package sched

import (
	"cmp"
	"slices"
)

// ────────────────────────────────────────────────────────────────────────────
// Serial Processing
// ────────────────────────────────────────────────────────────────────────────

// serialProcess executes the serial tick processing path.
//
// Unlike the parallel path, serial mode processes signals and effects inline:
// Emit/Publish closures immediately invoke the target logic's Think/Apply
// recursively, rather than buffering to intermediate collectors.
//
// Three internal recursive closures handle all dispatch:
//
//   - thinkSignal(ref, sig): wraps sig into a single-element Inbox, calls
//     Think on the target logic. Recursive: Think's Emit/Publish may trigger
//     further thinkSignal/applyOne calls inline.
//   - thinkTimer(ref): calls Think with an empty Inbox (timer-only activation).
//   - applyOne(ref, eff): wraps eff into a single-element Inbox, calls
//     Apply on the target logic. Apply's Emit may trigger thinkSignal inline.
//
// Initial frontier merge optimization:
//   - For each block, timer refs are copied to a local buffer and sorted.
//   - Signals from all source threads are collected into a flat buffer,
//     sorted by ref then Order().
//   - A merge-iteration over sorted timer refs and signal groups ensures
//     each logic is called at most once in the initial frontier:
//   - timer-only ref → Think(nil) via thinkTimer
//   - signal ref (with or without timer) → Think(batched signals) inline
//   - Cascading from Think/Apply still uses the recursive closures above,
//     processing one signal/effect at a time with immediate effect.
//
// This makes serial mode's initial frontier semantics consistent with
// parallel mode: each logic sees all its signals in a single Think call.
//
// Depth control:
//   - depth is tracked as a mutable stack variable, incremented before each
//     Think call and decremented after it returns.
//   - When depth >= maxDepth at thinkSignal entry, the signal is deferred
//     to the next tick via signalWrite[0].
//   - Apply does not increment depth: Think → Publish → Apply is atomic at
//     the same depth level.
//
// Timer registration uses blockToThread mapping to maintain consistency with
// parallel mode's thread-affinity semantics (last-write-wins within the same
// thread-local log entry).
//
// After serialProcess returns, caller must swap signal buffers to preserve
// deferred signals in signalRead for the next tick.
func (sc *Scheduler[W, S, E, L, ST]) serialProcess(world W, includeTimers bool, maxDepth int) {
	depth := 0
	bs := uint64(sc.meta.BlockSize)

	// ── Recursive closures ───────────────────────────────────────────────
	// Declared as variables so they can reference each other.
	// All closures capture depth by reference (single-threaded, stack-based
	// inc/dec tracks recursion naturally).
	var thinkSignal func(uint64, S)
	var thinkTimer func(uint64)
	var applyOne func(uint64, E)

	// stageRef tracks the refId of the logic currently executing Think/Apply.
	// Captured by reference in the WriteStage closures so that stage updates
	// are associated with the correct logic.
	var stageRef uint64

	// ThinkCtx and CommitCtx are created once and reused across all
	// recursive calls. Their Emit/Publish closures are set below.
	thinkCtx := &ThinkCtx[W, S, E, ST]{World: world}
	commitCtx := &CommitCtx[W, S, ST]{World: world}

	// applyOne finds the target logic and calls Apply with a single effect.
	// Apply does NOT increment depth — Think→Publish→Apply is one atomic
	// unit at the same depth level. Apply's Emit triggers thinkSignal,
	// which will increment depth on the next Think entry.
	applyOne = func(ref uint64, eff E) {
		logic, ok := sc.w.GetLogic(ref)
		if !ok {
			return
		}
		sc.promoteStages(world)
		prevStageRef := stageRef
		stageRef = ref
		logic.Apply(commitCtx, sliceInbox[E]{eff})
		sc.promoteStages(world)
		stageRef = prevStageRef
	}

	// thinkSignal finds the target logic and calls Think with a single signal.
	// On depth overflow, the signal is deferred to the next tick by pushing
	// it into signalWrite[0] (which is not read during serial processing).
	//
	// This closure is used for cascading signals from within Think/Apply.
	// The initial frontier uses the merge-iterate path below instead.
	thinkSignal = func(ref uint64, sig S) {
		if depth >= maxDepth {
			// Depth budget exhausted: defer signal to next tick.
			blockId := int(hash(ref, bs))
			sc.signalWrite[0].push(blockId, refVal[S]{ref, sig})
			return
		}
		logic, ok := sc.w.GetLogic(ref)
		if !ok {
			return
		}
		prevStageRef := stageRef
		stageRef = ref
		depth++
		delay := logic.Think(thinkCtx, sliceInbox[S]{sig})
		depth--
		sc.promoteStages(world)
		stageRef = prevStageRef
		if delay > 0 {
			blockId := int(hash(ref, bs))
			sc.timerWheel.set(sc.blockToThread[blockId], blockId, ref, delay)
		}
	}

	// thinkTimer calls Think with an empty Inbox (timer-only activation).
	// Used for timer-only refs in the merge-iterate path.
	thinkTimer = func(ref uint64) {
		if depth >= maxDepth {
			return // Budget exhausted (defensive; shouldn't happen for initial timers).
		}
		logic, ok := sc.w.GetLogic(ref)
		if !ok {
			return
		}
		prevStageRef := stageRef
		stageRef = ref
		depth++
		delay := logic.Think(thinkCtx, sliceInbox[S](nil))
		depth--
		sc.promoteStages(world)
		stageRef = prevStageRef
		if delay > 0 {
			blockId := int(hash(ref, bs))
			sc.timerWheel.set(sc.blockToThread[blockId], blockId, ref, delay)
		}
	}

	// Wire closures into contexts.
	thinkCtx.Emit = func(ref uint64, sig S) {
		sc.promoteStages(world)
		thinkSignal(ref, sig)
	}
	thinkCtx.Publish = func(ref uint64, eff E) {
		sc.promoteStages(world)
		applyOne(ref, eff)
	}
	thinkCtx.WriteStage = func(state ST) {
		sc.stageCollectors[0].Put(stageRef, state)
	}
	commitCtx.Emit = func(ref uint64, sig S) {
		sc.promoteStages(world)
		thinkSignal(ref, sig)
	}
	commitCtx.WriteStage = func(state ST) {
		sc.stageCollectors[0].Put(stageRef, state)
	}

	// ── Process initial frontier ─────────────────────────────────────────
	// For each block, collect timer refs and signals, sort both, then
	// merge-iterate to call each logic at most once.
	// Cascading from each Think call still uses the recursive closures above.
	//
	// Concurrency safety during iteration:
	//   - signalRead is only read, never written during serial processing.
	//   - Deferred signals go to signalWrite[0] (separate buffer).
	//   - Timer registrations go to thread-local logs (not the wheel itself),
	//     so timerWheel.get() results remain stable.
	c := sc.meta.Concurrency
	var flatBuf []refVal[S] // reused across blocks

	for blockId := range sc.meta.BlockSize {
		// 获取 timer refs 并原地排序。
		// 直接排序 IndexSet.Raw() 会破坏其 index map 一致性，
		// 但这是安全的：Think 阶段之后不会再读取当前 slot 的 epochSet
		// （merge 只写 future slot，advance 只推进 currentTime），
		// 等 wheel 转回此 slot 时 epoch 不匹配会触发 lazy Clear 重置。
		var timerRefs []uint64
		if includeTimers {
			timerRefs = sc.timerWheel.get(blockId)
			slices.Sort(timerRefs)
		}

		// Collect signals from all source threads.
		flatBuf = flatBuf[:0]
		for srcThread := range c {
			flatBuf = append(flatBuf, sc.signalRead[srcThread].get(blockId)...)
		}

		if len(timerRefs) == 0 && len(flatBuf) == 0 {
			continue
		}

		// Sort signals by ref then Order.
		if len(flatBuf) > 0 {
			slices.SortFunc(flatBuf, func(a, b refVal[S]) int {
				if c := cmp.Compare(a.ref, b.ref); c != 0 {
					return c
				}
				return cmp.Compare(a.val.Order(), b.val.Order())
			})
		}

		// Merge-iterate sorted timer refs and signal groups.
		// Each logic is called at most once in the initial frontier.
		ti := 0
		for start := 0; start < len(flatBuf); {
			ref := flatBuf[start].ref
			end := start + 1
			for end < len(flatBuf) && flatBuf[end].ref == ref {
				end++
			}

			// Timer-only refs with refId < current signal ref.
			for ti < len(timerRefs) && timerRefs[ti] < ref {
				thinkTimer(timerRefs[ti])
				ti++
			}

			// Timer ref matching signal ref → skip (merged into signal call).
			if ti < len(timerRefs) && timerRefs[ti] == ref {
				ti++
			}

			// Process signal group with depth tracking.
			// At the initial frontier depth is 0, so the overflow branch
			// is defensive only. Cascading depth is handled by thinkSignal.
			if depth >= maxDepth {
				// Defer all signals in this group to next tick.
				for i := start; i < end; i++ {
					sc.signalWrite[0].push(blockId, flatBuf[i])
				}
			} else if logic, ok := sc.w.GetLogic(ref); ok {
				prevStageRef := stageRef
				stageRef = ref
				depth++
				delay := logic.Think(thinkCtx, refValInbox[S](flatBuf[start:end]))
				depth--
				sc.promoteStages(world)
				stageRef = prevStageRef
				if delay > 0 {
					sc.timerWheel.set(sc.blockToThread[blockId], blockId, ref, delay)
				}
			}

			start = end
		}

		// Drain remaining timer-only refs after all signal groups.
		for ; ti < len(timerRefs); ti++ {
			thinkTimer(timerRefs[ti])
		}
	}
}

package engine

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
//   - applyOne(ref, eff): wraps eff into a single-element Arrangement, calls
//     Apply on the target logic. Apply's Emit may trigger thinkSignal inline.
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
func (sc *Scheduler[W, S, E, L]) serialProcess(world W, includeTimers bool, maxDepth int) {
	depth := 0
	bs := uint64(sc.meta.BlockSize)

	// ── Recursive closures ───────────────────────────────────────────────
	// Declared as variables so they can reference each other.
	// All closures capture depth by reference (single-threaded, stack-based
	// inc/dec tracks recursion naturally).
	var thinkSignal func(uint64, S)
	var thinkTimer func(uint64)
	var applyOne func(uint64, E)

	// ThinkCtx and CommitCtx are created once and reused across all
	// recursive calls. Their Emit/Publish closures are set below.
	thinkCtx := &ThinkCtx[W, S, E]{World: world}
	commitCtx := &CommitCtx[W, S]{World: world}

	// applyOne finds the target logic and calls Apply with a single effect.
	// Apply does NOT increment depth — Think→Publish→Apply is one atomic
	// unit at the same depth level. Apply's Emit triggers thinkSignal,
	// which will increment depth on the next Think entry.
	applyOne = func(ref uint64, eff E) {
		logic, ok := sc.w.GetLogic(ref)
		if !ok {
			return
		}
		logic.Apply(commitCtx, sliceArrangement[E]{eff})
	}

	// thinkSignal finds the target logic and calls Think with a single signal.
	// On depth overflow, the signal is deferred to the next tick by pushing
	// it into signalWrite[0] (which is not read during serial processing).
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
		depth++
		delay := logic.Think(thinkCtx, sliceInbox[S]{sig})
		depth--
		if delay > 0 {
			blockId := int(hash(ref, bs))
			sc.timerWheel.set(sc.blockToThread[blockId], blockId, ref, delay)
		}
	}

	// thinkTimer calls Think with an empty Inbox (timer-only activation).
	// Timer activations only occur at the initial frontier (depth==0),
	// so the depth guard is defensive only.
	thinkTimer = func(ref uint64) {
		if depth >= maxDepth {
			return // Budget exhausted (defensive; shouldn't happen for initial timers).
		}
		logic, ok := sc.w.GetLogic(ref)
		if !ok {
			return
		}
		depth++
		delay := logic.Think(thinkCtx, sliceInbox[S](nil))
		depth--
		if delay > 0 {
			blockId := int(hash(ref, bs))
			sc.timerWheel.set(sc.blockToThread[blockId], blockId, ref, delay)
		}
	}

	// Wire closures into contexts.
	thinkCtx.Emit = thinkSignal
	thinkCtx.Publish = applyOne
	commitCtx.Emit = thinkSignal

	// ── Process initial frontier ─────────────────────────────────────────
	// Iterate all blocks, consuming timer activations (first superstep only)
	// and signals from signalRead. Each item may trigger recursive cascading
	// via the closures above.
	//
	// Concurrency safety during iteration:
	//   - signalRead is only read, never written during serial processing.
	//   - Deferred signals go to signalWrite[0] (separate buffer).
	//   - Timer registrations go to thread-local logs (not the wheel itself),
	//     so timerWheel.get() results remain stable.
	c := sc.meta.Concurrency
	for blockId := range sc.meta.BlockSize {
		// Timer activations (first superstep only).
		if includeTimers {
			for _, refId := range sc.timerWheel.get(blockId) {
				thinkTimer(refId)
			}
		}
		// Signals from all source threads.
		for srcThread := range c {
			for _, rv := range sc.signalRead[srcThread].get(blockId) {
				thinkSignal(rv.ref, rv.val)
			}
		}
	}
}

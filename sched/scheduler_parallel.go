package sched

import (
	"cmp"
	"slices"
	"sync"
)

// ────────────────────────────────────────────────────────────────────────────
// Think Phase
// ────────────────────────────────────────────────────────────────────────────

// parallelThink 启动并行 Think 阶段。
// 每个 thread 遍历其负责的 block，消费 timer（首轮）和 signalRead，
// 产出 effect → effectCollectors，signal → signalWrite。
//
// TODO: 替换为预分配的 worker pool，避免每 superstep 创建 goroutine。
func (sc *Scheduler[W, S, E, L, WS]) parallelThink(world W, includeTimers bool) {
	var wg sync.WaitGroup
	for t := range sc.meta.Concurrency {
		wg.Add(1)
		go func(threadId int) {
			defer wg.Done()
			sc.thinkWorker(threadId, world, includeTimers)
		}(t)
	}
	wg.Wait()
}

// thinkWorker 是单个 thread 的 Think 执行逻辑。
//
// 对每个负责的 block：
//  1. Timer（首轮 superstep）：拷贝 wheel.get(blockId) 中的到期 refId 到本地
//     buffer 并排序。
//  2. Signal：跨所有 source thread 收集 signalRead[*][blockId] 到 flat buffer，
//     按 ref 排序后线性分组。
//  3. 归并遍历：将排好序的 timer refs 与 signal 分组做归并遍历，保证每个
//     logic 最多被调用一次 Think：
//     - 纯 timer ref（无同 ref signal）→ Think(ctx, emptyInbox)
//     - 有 signal 的 ref（无论是否同时有 timer）→ Think(ctx, signals)
//     排序替代了 map 分组，避免跨 block 的 map key 膨胀问题。
//  4. Think 返回 delay > 0 → timerWheel.set（thread-local write，无竞争）。
//  5. ctx.Publish → effectCollectors[threadId]（per-thread write，无竞争）。
//  6. ctx.Emit   → signalWrite[threadId]（per-thread write，无竞争）。
//
// 线程安全：
//   - getLogic 并发读（调用方保证无 tick 内写）
//   - signalRead[*] 在 Think 阶段只读（上轮 swap 后不再写入）
//   - effectCollectors[threadId] / signalWrite[threadId] 只有本 thread 写入
//   - timerWheel.threadBuf[threadId] 只有本 thread 写入
//   - thinkCollectBuf[threadId] 只有本 thread 读写（CacheLinePad 隔离）
func (sc *Scheduler[W, S, E, L, WS]) thinkWorker(threadId int, world W, includeTimers bool) {
	// thinkRef tracks the ref of the logic currently executing Think.
	// Captured by SetWatch closure so it can associate watch updates with
	// the correct logic without per-call closure allocation.
	var thinkRef uint64

	ctx := &ThinkCtx[W, S, E, WS]{
		World:   world,
		Emit:    sc.emitClosure(threadId),
		Publish: sc.publishClosure(threadId),
		SetWatch: func(ws WS) {
			sc.watchCollectors[threadId].buf = append(
				sc.watchCollectors[threadId].buf,
				RefWatch[WS]{thinkRef, ws},
			)
		},
	}

	c := sc.meta.Concurrency
	flatBuf := sc.thinkCollectBuf[threadId].buf

	for _, blockId := range sc.thinkBlocks[threadId] {
		// 1. 获取 timer refs 并原地排序。
		//    直接排序 IndexSet.Raw() 会破坏其 index map 一致性，
		//    但这是安全的：Think 阶段之后不会再读取当前 slot 的 epochSet
		//    （merge 只写 future slot，advance 只推进 currentTime），
		//    等 wheel 转回此 slot 时 epoch 不匹配会触发 lazy Clear 重置。
		var timerRefs []uint64
		if includeTimers {
			timerRefs = sc.timerWheel.get(blockId)
			slices.Sort(timerRefs)
		}

		// 2. 跨所有 source thread 收集 signal 到 flat buffer
		flatBuf = flatBuf[:0]
		for srcThread := range c {
			flatBuf = append(flatBuf, sc.signalRead[srcThread].get(blockId)...)
		}

		if len(timerRefs) == 0 && len(flatBuf) == 0 {
			continue
		}

		// 3. 按 ref 排序 signal，再按 Order 子排序
		if len(flatBuf) > 0 {
			slices.SortFunc(flatBuf, func(a, b refVal[S]) int {
				if c := cmp.Compare(a.ref, b.ref); c != 0 {
					return c
				}
				return cmp.Compare(a.val.Order(), b.val.Order())
			})
		}

		// 4. 归并遍历 timer refs 与 signal 分组。
		//    每个 logic 最多调用一次 Think。
		ti := 0
		for start := 0; start < len(flatBuf); {
			ref := flatBuf[start].ref
			end := start + 1
			for end < len(flatBuf) && flatBuf[end].ref == ref {
				end++
			}

			// 处理 refId < 当前 signal ref 的纯 timer refs
			for ti < len(timerRefs) && timerRefs[ti] < ref {
				tRef := timerRefs[ti]
				ti++
				if logic, ok := sc.w.GetLogic(tRef); ok {
					thinkRef = tRef
					delay := logic.Think(ctx, sliceInbox[S](nil))
					if delay > 0 {
						sc.timerWheel.set(threadId, blockId, tRef, delay)
					}
				}
			}

			// timer ref 与 signal ref 重合 → 跳过 timer（合并到 signal 调用）
			if ti < len(timerRefs) && timerRefs[ti] == ref {
				ti++
			}

			// 调用 Think，inbox 为本 ref 的所有 signals
			if logic, ok := sc.w.GetLogic(ref); ok {
				thinkRef = ref
				delay := logic.Think(ctx, refValInbox[S](flatBuf[start:end]))
				if delay > 0 {
					sc.timerWheel.set(threadId, blockId, ref, delay)
				}
			}
			start = end
		}

		// 处理 signal 遍历结束后剩余的纯 timer refs
		for ; ti < len(timerRefs); ti++ {
			tRef := timerRefs[ti]
			if logic, ok := sc.w.GetLogic(tRef); ok {
				thinkRef = tRef
				delay := logic.Think(ctx, sliceInbox[S](nil))
				if delay > 0 {
					sc.timerWheel.set(threadId, blockId, tRef, delay)
				}
			}
		}
	}

	// 写回以保留 grown capacity
	sc.thinkCollectBuf[threadId].buf = flatBuf
}

// publishClosure 返回 Think 阶段 thread 专用的 effect 发射闭包。
// effect 连同 targetRef 打包为 refVal[E]，按 hash(targetRef) % BlockSize 分桶
// 存入 effectCollectors[threadId]。
func (sc *Scheduler[W, S, E, L, WS]) publishClosure(threadId int) func(uint64, E) {
	collector := sc.effectCollectors[threadId]
	blockSize := uint64(sc.meta.BlockSize)
	return func(refId uint64, e E) {
		collector.push(int(hash(refId, blockSize)), refVal[E]{refId, e})
	}
}

// emitClosure 返回 Think/Apply 阶段 thread 专用的 signal 发射闭包。
// signal 连同 targetRef 打包为 refVal[S]，按 hash(targetRef) % BlockSize 分桶
// 存入 signalWrite[threadId]。
//
// 注意：闭包通过 sc.signalWrite[threadId] 间接访问当前 write buffer，
// 而非在创建时捕获 collector 指针。这保证 swap 后闭包仍写入正确的 buffer。
func (sc *Scheduler[W, S, E, L, WS]) emitClosure(threadId int) func(uint64, S) {
	blockSize := uint64(sc.meta.BlockSize)
	return func(refId uint64, sig S) {
		sc.signalWrite[threadId].push(int(hash(refId, blockSize)), refVal[S]{refId, sig})
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Apply Phase
// ────────────────────────────────────────────────────────────────────────────

// parallelApply 启动并行 Apply 阶段。
func (sc *Scheduler[W, S, E, L, WS]) parallelApply(world W) {
	var wg sync.WaitGroup
	for t := range sc.meta.Concurrency {
		if len(sc.applyBlocks[t]) == 0 {
			continue
		}
		wg.Add(1)
		go func(threadId int) {
			defer wg.Done()
			sc.applyWorker(threadId, world)
		}(t)
	}
	wg.Wait()
}

// applyWorker 是单个 thread 的 Apply 执行逻辑。
//
// 对分配到本 thread 的每个 block：
//  1. 跨所有 Think thread 收集 effectCollectors[*][blockId] 到 flat buffer
//  2. 按 ref 排序后线性分组，逐组调用 logic.Apply(commitCtx, effects)
//  3. Apply 产出的 signal → signalWrite[threadId]
//
// 线程安全：
//   - effectCollectors[*] 在 Think barrier 后只读
//   - 不同 Apply thread 处理不同 block → 不同 targetRef → 无写竞争
//   - signalWrite[threadId] 只有本 thread 写入
//   - applyCollectBuf[threadId] 只有本 thread 读写（CacheLinePad 隔离）
func (sc *Scheduler[W, S, E, L, WS]) applyWorker(threadId int, world W) {
	ctx := &CommitCtx[W, S, WS]{
		World: world,
		Emit:  sc.emitClosure(threadId),
	}

	flatBuf := sc.applyCollectBuf[threadId].buf
	c := sc.meta.Concurrency

	for _, blockId := range sc.applyBlocks[threadId] {
		// 跨所有 Think thread 收集 effect 到 flat buffer
		flatBuf = flatBuf[:0]
		for t := range c {
			flatBuf = append(flatBuf, sc.effectCollectors[t].get(blockId)...)
		}
		if len(flatBuf) == 0 {
			continue
		}

		// 按 ref 排序，线性分组调用 Apply
		slices.SortFunc(flatBuf, func(a, b refVal[E]) int {
			if c := cmp.Compare(a.ref, b.ref); c != 0 {
				return c
			}
			return cmp.Compare(a.val.Order(), b.val.Order())
		})
		for start := 0; start < len(flatBuf); {
			ref := flatBuf[start].ref
			end := start + 1
			for end < len(flatBuf) && flatBuf[end].ref == ref {
				end++
			}
			if logic, ok := sc.w.GetLogic(ref); ok {
				logic.Apply(ctx, refValInbox[E](flatBuf[start:end]))
			}
			start = end
		}
	}

	// 写回以保留 grown capacity
	sc.applyCollectBuf[threadId].buf = flatBuf
}

package par

import "github.com/legamerdc/game/lib"

// ────────────────────────────────────────────────────────────────────────────
// Configuration
// ────────────────────────────────────────────────────────────────────────────

// ScheduleMeta 包含 scheduler 运行时的全部可调参数。
// 零值字段在 NewScheduler 中会被替换为默认值。
type ScheduleMeta struct {
	// Think 阶段：frontier 超过此数时启用并发 Think（当前仅实现并行路径）
	ThinkConcurrencyThreshold int // default 500

	// 并发 worker 数（Think 和 Apply 共用同一组 threadId 空间）
	Concurrency int // default 5

	// 并发模式最大 superstep 轮次；超出后未消费的 signal 自动延迟到下一 tick
	MaxSupersteps int // default 3

	// timer wheel 槽位数
	TimerWheelSize int // default 200

	// effect/signal 分块数（质数，用于 hash(targetRef) % BlockSize 分桶）
	BlockSize int // default 137
}

// ────────────────────────────────────────────────────────────────────────────
// Scheduler
// ────────────────────────────────────────────────────────────────────────────

// Scheduler 是并行 tick 的调度运行时。
//
// 核心职责：
//  1. 管理 Think 激活：timer 到期、signal 触发、外部输入三种来源
//  2. 收集 Effect：Think 产出的 effect 按 targetRef 分 block 聚合后分配给 Apply
//  3. 传递 Signal：Think/Apply 产出的 signal 通过双缓冲传递给下一轮 superstep 的 Think
//  4. Superstep 循环：Think→Apply→swap 直到无工作或达到预算
//
// 并发模型：
//   - Think 阶段按 block 稳定分配到 thread（blockId % Concurrency），
//     跨 superstep/tick 一致，保证 timer wheel thread-local write 无冲突
//   - Apply 阶段按 effect 数量动态分配 block（LPT 近似均衡）
//   - 每个 thread 维护独立的 effect/signal collector，
//     cache-line 隔离，无锁无竞争
//
// 关于去重：
//
//	Scheduler 不保证同一 logic 在同一 superstep 只 Think 一次。
//	设计要求 Logic 自身处理重复激活（例如 timer + signal 同时到达时，
//	可能分多次传入 signal list，包含空 list 表示 timer 激活）。
//	这消除了 per-logic inbox 聚合、frontier 去重和 signal routing 的开销。
//
// 关于 Logic 查找：
//
//	Scheduler 不维护 logic 注册表。构造时注入 getLogic 函数，
//	由外部（如 World 的具体实现）负责 logic 的生命周期管理。
//	getLogic 在 Think/Apply 阶段被并发调用，调用方须保证并发读安全
//	（Go map 在无写时支持并发读）。
//
// 关于 L 的类型约束：
//
//	L 应为指针类型（如 *MyLogic），否则 Think/Apply 对 private/public state
//	的修改不会持久化。这是 Go 接口值语义的标准约束。
type Scheduler[W interface {
	World
	LogicProvider[L]
	StagePromoter
}, S SignalI, E EffectI, L Logic[W, S, E]] struct {
	meta ScheduleMeta

	// ── Logic 查找 ───────────────────────────────────────────────────
	// 由外部注入，在 Think/Apply 阶段并发调用。
	// 返回 (logic, true) 表示找到；(_, false) 表示不存在（静默丢弃）。
	w W

	// ── Timer Wheel ──────────────────────────────────────────────────
	// thread-local write（Think 阶段 set）+ block-sharded read（Think 阶段 get）。
	// merge/advance 在 tick 结束后单线程执行。
	timerWheel *timerWheel[uint64]

	// ── Block→Thread 稳定映射（初始化时固定）──────────────────────────
	// thinkBlocks[threadId] = 该 thread 负责的 blockId 列表
	// blockToThread[blockId] = 该 block 所属的 threadId（当前未使用，保留以备串行模式）
	// 保证同一 logic 跨 superstep/tick 始终在同一 thread 执行 Think，
	// 使 timer wheel 的 thread-local write 天然无冲突。
	thinkBlocks   [][]int
	blockToThread []int

	// ── Per-thread Effect Collectors ─────────────────────────────────
	// effectCollectors[threadId]: Think 阶段写入，Apply 阶段跨 thread 只读。
	// 每个 collector 有 CacheLinePad 隔离。
	effectCollectors []*blockCollector[refVal[E]]

	// ── 双缓冲 Signal Collectors ─────────────────────────────────────
	// signalRead[threadId]:  上一轮 superstep 的输出（或外部输入/延迟信号），当前 Think 消费。
	// signalWrite[threadId]: 当前 Think/Apply 的输出，下一轮 superstep 消费。
	//
	// 每轮 superstep 结束后 swap：signalRead ← signalWrite, clear signalWrite。
	// 超出 MaxSupersteps 后 signalRead 中的残余信号自动延迟到下一 tick。
	//
	// Think 和 Apply 共用 signalWrite（barrier 保证时序安全）：
	//   Think → barrier → Apply → barrier → swap
	signalRead  []*blockCollector[refVal[S]]
	signalWrite []*blockCollector[refVal[S]]

	// ── 外部输入缓冲 ────────────────────────────────────────────────
	// Emit() 追加到 pending，ProcessTick 开始时注入 signalRead。
	// 只在 tick 外部写入，tick 内单线程消费。
	pending []refVal[S]

	// ── Apply 阶段临时数据 ───────────────────────────────────────────
	applyBlocks [][]int // threadId → 当前 superstep 分配的 blockId 列表

	// ── Sort-based 分组缓冲 ─────────────────────────────────────────
	// thinkCollectBuf[threadId]: Think worker 收集当前 block 的 signal 到 flat buffer，
	// 按 ref 排序后线性分组调用 Think。每个 block 处理前截断到 0，跨 block 复用 capacity。
	// CacheLinePad 隔离保证并行写入无 false sharing。
	thinkCollectBuf []collectBuf[refVal[S]]

	// applyCollectBuf[threadId]: Apply worker 收集当前 block 的 effect 到 flat buffer，
	// 按 ref 排序后线性分组调用 Apply。每个 block 处理前截断到 0，跨 block 复用 capacity。
	applyCollectBuf []collectBuf[refVal[E]]

	// ── Per-thread Staged State Collectors ─────────────────────────────
	// stageCollectors[threadId]: Think/Apply 阶段 WriteStage 闭包写入。
	// 阶段 barrier 后由 promoteStages 串行提交给 World。
	stageCollectors []lib.IndexMap[stageKey, StagedState]
	stageCommitBuf  []RefStage

	// ── 预分配计算缓冲 ──────────────────────────────────────────────
	blockLoads  []blockLoad // computeApplyAssignment 用
	threadLoads []int       // computeApplyAssignment 用
}

// NewScheduler 创建 Scheduler 并预分配所有内部结构。
//
// getLogic 由外部注入，用于在 Think/Apply 阶段查找 Logic 实例。
// 调用方须保证 getLogic 在并发调用时安全（通常是底层 map 在 tick 内无写即可）。
//
// Think 阶段的 block→thread 映射在此固定，后续不可变。
func NewScheduler[W interface {
	World
	LogicProvider[L]
	StagePromoter
}, S SignalI, E EffectI, L Logic[W, S, E]](
	meta ScheduleMeta,
	w W,
) *Scheduler[W, S, E, L] {
	// 补齐默认值
	if meta.Concurrency <= 0 {
		meta.Concurrency = 5
	}
	if meta.BlockSize <= 0 {
		meta.BlockSize = 137
	}
	if meta.MaxSupersteps <= 0 {
		meta.MaxSupersteps = 3
	}
	if meta.TimerWheelSize <= 0 {
		meta.TimerWheelSize = 200
	}
	if meta.ThinkConcurrencyThreshold <= 0 {
		meta.ThinkConcurrencyThreshold = 500
	}

	c := meta.Concurrency
	bs := meta.BlockSize

	// 计算 Think 阶段的 block→thread 稳定映射
	// 策略：blockId % concurrency → threadId（均匀分配，质数 blockSize 保证各 thread 分到的块数差 ≤1）
	thinkBlocks := make([][]int, c)
	blockToThread := make([]int, bs)
	for b := range bs {
		t := b % c
		thinkBlocks[t] = append(thinkBlocks[t], b)
		blockToThread[b] = t
	}

	// 预分配 per-thread 结构
	effectCollectors := make([]*blockCollector[refVal[E]], c)
	signalRead := make([]*blockCollector[refVal[S]], c)
	signalWrite := make([]*blockCollector[refVal[S]], c)
	applyBlocks := make([][]int, c)
	thinkCollectBuf := make([]collectBuf[refVal[S]], c)
	applyCollectBuf := make([]collectBuf[refVal[E]], c)
	stageCollectors := make([]lib.IndexMap[stageKey, StagedState], c)
	for i := range c {
		effectCollectors[i] = newBlockCollector[refVal[E]](bs)
		signalRead[i] = newBlockCollector[refVal[S]](bs)
		signalWrite[i] = newBlockCollector[refVal[S]](bs)
		applyBlocks[i] = make([]int, 0, (bs/c)+1)
		stageCollectors[i].Init(bs / c)
	}

	return &Scheduler[W, S, E, L]{
		meta:             meta,
		w:                w,
		timerWheel:       newTimerWheel[uint64](meta.TimerWheelSize, bs, thinkBlocks),
		thinkBlocks:      thinkBlocks,
		blockToThread:    blockToThread,
		effectCollectors: effectCollectors,
		signalRead:       signalRead,
		signalWrite:      signalWrite,
		applyBlocks:      applyBlocks,
		thinkCollectBuf:  thinkCollectBuf,
		applyCollectBuf:  applyCollectBuf,
		stageCollectors:  stageCollectors,
		blockLoads:       make([]blockLoad, bs),
		threadLoads:      make([]int, c),
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 外部输入
// ────────────────────────────────────────────────────────────────────────────

// Emit 向指定 logic 注入外部信号（如网络输入）。
// 信号暂存在 pending 中，下次 ProcessTick 开始时注入 signalRead。
// 必须在 ProcessTick 外部调用（单线程）。
func (sc *Scheduler[W, S, E, L]) Emit(ref uint64, signal S) {
	sc.pending = append(sc.pending, refVal[S]{ref, signal})
}

// ────────────────────────────────────────────────────────────────────────────
// ProcessTick — 并行 tick 主入口
// ────────────────────────────────────────────────────────────────────────────

// ProcessTick 执行一个完整的 tick，自动选择并行或串行模式。
//
// 模式选择：
//   - 每轮 superstep 开始前统计待处理工作量（signal + timer 条目总数）
//   - workCount >= ThinkConcurrencyThreshold → 并行模式（superstep 循环）
//   - workCount <  ThinkConcurrencyThreshold → 串行模式（递归 inline）
//   - 串行模式是终态：一旦进入，不可能再回到并行模式
//
// 生命周期：
//  1. 注入外部输入到 signalRead
//  2. superstep 循环：每轮判断模式
//     - 并行：parallelThink → computeApplyAssignment → parallelApply → swap → reset
//     - 串行：serialProcess（递归 inline）→ swap → break
//  3. tick 结束：合并 timer 注册 → 推进 timer wheel
//  4. 溢出处理：signalRead 中的残余信号自动保留到下一 tick
func (sc *Scheduler[W, S, E, L]) ProcessTick() {
	// Phase 0: 注入外部输入
	sc.injectPending()

	// Superstep 循环
	firstSuperstep := true
	for round := range sc.meta.MaxSupersteps {
		workCount := sc.countWork(firstSuperstep)
		if workCount == 0 {
			break
		}

		if workCount >= sc.meta.ThinkConcurrencyThreshold {
			// ── Parallel path ────────────────────────────────────────
			sc.parallelThink(sc.w, firstSuperstep)
			firstSuperstep = false

			sc.promoteStages(sc.w)
			sc.computeApplyAssignment()
			sc.parallelApply(sc.w)
			sc.promoteStages(sc.w)

			// signalRead ← signalWrite（下一轮 Think 的输入）
			// signalWrite ← old signalRead（清空后作为下一轮的输出缓冲）
			sc.swapSignalBuffers()

			// effect 只在 Think 中产出、Apply 中消费，superstep 结束后即可清空。
			// timer wheel 的 thread-local log 不清空：跨 superstep 累积，tick 结束统一 merge。
			sc.resetEffectCollectors()
		} else {
			// ── Serial path ──────────────────────────────────────────
			// Serial mode processes inline: Think/Apply cascade via
			// recursive closures, no intermediate buffering.
			// Depth budget = remaining supersteps.
			maxDepth := sc.meta.MaxSupersteps - round
			sc.serialProcess(sc.w, firstSuperstep, maxDepth)

			// Deferred signals (depth overflow) were written to signalWrite.
			// Swap to preserve them in signalRead for the next tick.
			sc.swapSignalBuffers()
			break // Serial is terminal; cannot transition back to parallel.
		}
	}

	// ── Tick 结束 ─────────────────────────────────────────────────────

	// 合并所有 thread 本地的 timer 注册到全局 wheel。
	// 同一 logic 在多轮 superstep 中的最后一次 set() 覆盖前次（IndexMap.Put 语义）。
	sc.timerWheel.merge()

	// 推进 wheel 到下一 tick：清空 thread-local log，currentTime++。
	sc.timerWheel.advance()

	// 溢出处理：signalRead 中的残余信号（超 MaxSupersteps 未消费）自动保留。
	// 下一 tick 的 injectPending 不会清空 signalRead，Think 会继续消费它们。
}

// ────────────────────────────────────────────────────────────────────────────
// Pending & Work Detection
// ────────────────────────────────────────────────────────────────────────────

// injectPending 将外部输入从 pending 注入到 signalRead[0]。
// 使用固定的 threadId=0 作为外部输入的来源标识。
// 所有 Think thread 都会读取 signalRead[0]（跨 source thread 遍历），
// 因此外部信号能被正确路由到目标 block 对应的 Think thread。
func (sc *Scheduler[W, S, E, L]) injectPending() {
	blockSize := uint64(sc.meta.BlockSize)
	for _, rv := range sc.pending {
		blockId := int(hash(rv.ref, blockSize))
		sc.signalRead[0].push(blockId, rv)
	}
	clear(sc.pending)
	sc.pending = sc.pending[:0]
}

// countWork 统计当前 superstep 的待处理工作量（signal + timer 条目总数）。
//
// 返回值用于两个判断：
//   - == 0：无工作，终止 superstep 循环
//   - >= ThinkConcurrencyThreshold：选择并行模式，否则串行模式
//
// 当计数达到 ThinkConcurrencyThreshold 时提前返回（early exit），
// 因为超过阈值后精确计数对模式选择无意义。
//
//   - includeTimers=true（首轮 superstep）：统计 timer wheel + signalRead
//   - includeTimers=false（后续 superstep）：仅统计 signalRead
func (sc *Scheduler[W, S, E, L]) countWork(includeTimers bool) int {
	count := 0
	threshold := sc.meta.ThinkConcurrencyThreshold
	bs := sc.meta.BlockSize
	for _, c := range sc.signalRead {
		for b := range bs {
			count += len(c.get(b))
			if count >= threshold {
				return count
			}
		}
	}
	if includeTimers {
		for b := range bs {
			count += len(sc.timerWheel.get(b))
			if count >= threshold {
				return count
			}
		}
	}
	return count
}

// ────────────────────────────────────────────────────────────────────────────
// Buffer Management
// ────────────────────────────────────────────────────────────────────────────

// swapSignalBuffers 交换 signalRead 和 signalWrite，并清空新的 write buffer。
//
// swap 后：
//   - signalRead = 上一轮 Think/Apply 的输出 → 下一轮 Think 的输入
//   - signalWrite = 清空 → 下一轮 Think/Apply 的输出缓冲
//
// 如果 superstep 循环因 MaxSupersteps 终止，signalRead 中残余的信号
// 会自动保留到下一 tick（injectPending 不清空 signalRead）。
func (sc *Scheduler[W, S, E, L]) swapSignalBuffers() {
	sc.signalRead, sc.signalWrite = sc.signalWrite, sc.signalRead
	// 清空新的 write buffer（即旧的 read buffer，已被 Think 消费）
	for _, c := range sc.signalWrite {
		c.reset(collectorMaxRetain)
	}
}

// resetEffectCollectors 清空所有 thread 的 effect collector。
// effect 在 Think 中产出、Apply 中消费，superstep 结束后即可清空。
func (sc *Scheduler[W, S, E, L]) resetEffectCollectors() {
	for _, c := range sc.effectCollectors {
		c.reset(collectorMaxRetain)
	}
}

// promoteStages 将当前阶段各 thread 收集的 staged state 更新统一提交给 World。
//
// 在并行模式下，此方法在 Think barrier 之后、Apply 之前调用一次，
// 在 Apply barrier 之后、下一轮 Think 之前再调用一次。
//
// 串行模式下也复用同一 collector，并在 inline 阶段切换点提交。
func (sc *Scheduler[W, S, E, L]) promoteStages(world W) {
	sc.stageCommitBuf = sc.stageCommitBuf[:0]
	for t := range sc.meta.Concurrency {
		sc.stageCollectors[t].Iter(func(key stageKey, state StagedState) {
			sc.stageCommitBuf = append(sc.stageCommitBuf, RefStage{RefId: key.ref, Kind: key.kind, State: state})
		})
		sc.stageCollectors[t].Clear()
	}
	if len(sc.stageCommitBuf) == 0 {
		return
	}
	world.PromoteStages(sliceInbox[RefStage](sc.stageCommitBuf))
	clear(sc.stageCommitBuf)
}

type stageKey struct {
	ref  uint64
	kind StageKind
}

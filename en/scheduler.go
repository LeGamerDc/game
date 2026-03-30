package engine

import (
	"cmp"
	"slices"
	"sync"

	"golang.org/x/sys/cpu"
)

// ────────────────────────────────────────────────────────────────────────────
// Helper types
// ────────────────────────────────────────────────────────────────────────────

// refVal 将一个值与其目标 ref 打包在一起，用于 block-sharded collector。
// effect/signal 按 hash(targetRef) % blockSize 分桶存储时，需要保留
// targetRef 以便后续按 target 聚合（Apply）或按 target 分组（Think）。
type refVal[V any] struct {
	ref uint64
	val V
}

// sliceInbox 将 []S 适配为 Inbox[S] 接口。
// nil 或空 slice 产生 Len()==0，表示 timer-only 激活（无信号需消费）。
type sliceInbox[S SignalI] []S

func (s sliceInbox[S]) Len() int   { return len(s) }
func (s sliceInbox[S]) At(i int) S { return s[i] }

// sliceArrangement 将 []E 适配为 Arrangement[E] 接口。
type sliceArrangement[E EffectI] []E

func (s sliceArrangement[E]) Len() int   { return len(s) }
func (s sliceArrangement[E]) At(i int) E { return s[i] }

// refValInbox 将 []refVal[S] 适配为 Inbox[S] 接口。
// 用于 sort-based 分组后，将排序区间直接作为 Think 的输入，无需拷贝到独立 []S。
type refValInbox[S SignalI] []refVal[S]

func (r refValInbox[S]) Len() int   { return len(r) }
func (r refValInbox[S]) At(i int) S { return r[i].val }

// refValArrangement 将 []refVal[E] 适配为 Arrangement[E] 接口。
// 用于 sort-based 分组后，将排序区间直接作为 Apply 的输入，无需拷贝到独立 []E。
type refValArrangement[E EffectI] []refVal[E]

func (r refValArrangement[E]) Len() int   { return len(r) }
func (r refValArrangement[E]) At(i int) E { return r[i].val }

// collectBuf 是 per-thread 的排序/分组缓冲，用于 Think 和 Apply 阶段。
// 头部 CacheLinePad 隔离保证相邻 thread 的缓冲不共享 cache line。
type collectBuf[V any] struct {
	_   cpu.CacheLinePad
	buf []V
}

// blockLoad 记录单个 block 的 effect 负载，用于 Apply 阶段的 LPT 分配。
type blockLoad struct {
	blockId int
	count   int
}

// collectorMaxRetain 控制 blockCollector.reset 的保留上限：
// 单个 block 的 slice 长度超过此值时释放底层数组，防止偶发尖峰导致持久内存膨胀。
const collectorMaxRetain = 128

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
//	由外部（如 WorldView 的具体实现）负责 logic 的生命周期管理。
//	getLogic 在 Think/Apply 阶段被并发调用，调用方须保证并发读安全
//	（Go map 在无写时支持并发读）。
//
// 关于 L 的类型约束：
//
//	L 应为指针类型（如 *MyLogic），否则 Think/Apply 对 private/public state
//	的修改不会持久化。这是 Go 接口值语义的标准约束。
type Scheduler[W interface {
	WorldView
	LogicProvider[L]
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
	WorldView
	LogicProvider[L]
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
	for i := range c {
		effectCollectors[i] = newBlockCollector[refVal[E]](bs)
		signalRead[i] = newBlockCollector[refVal[S]](bs)
		signalWrite[i] = newBlockCollector[refVal[S]](bs)
		applyBlocks[i] = make([]int, 0, (bs/c)+1)
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

// ProcessTick 执行一个完整的并行 tick。
//
// 生命周期：
//  1. 注入外部输入到 signalRead
//  2. superstep 循环：Think→Apply→swap，直到无工作或超预算
//  3. tick 结束：合并 timer 注册 → 推进 timer wheel
//  4. 溢出处理：signalRead 中的残余信号自动保留到下一 tick（无需额外操作）
//
// 当前仅实现并行路径。串行模式将在后续版本中实现。
func (sc *Scheduler[W, S, E, L]) ProcessTick(world W) {
	// Phase 0: 注入外部输入
	sc.injectPending()

	// Superstep 循环
	firstSuperstep := true
	for round := range sc.meta.MaxSupersteps {
		_ = round
		if !sc.hasWork(firstSuperstep) {
			break
		}

		// ── Think Phase（并行）────────────────────────────────────────
		// 每个 thread 遍历其 block：
		//   - 首轮 superstep：消费 timer 到期 + signalRead
		//   - 后续 superstep：仅消费 signalRead（上轮 swap 后的 signalWrite）
		// 产出写入 effectCollectors + signalWrite
		sc.parallelThink(world, firstSuperstep)
		firstSuperstep = false

		// ── Apply Phase（并行，LPT 动态负载均衡）─────────────────────
		sc.computeApplyAssignment()
		sc.parallelApply(world)

		// ── Swap 信号缓冲 ────────────────────────────────────────────
		// signalRead ← signalWrite（下一轮 Think 的输入）
		// signalWrite ← old signalRead（清空后作为下一轮的输出缓冲）
		sc.swapSignalBuffers()

		// ── 清空 effect collectors ───────────────────────────────────
		// effect 只在 Think 中产出、Apply 中消费，superstep 结束后即可清空。
		// timer wheel 的 thread-local log 不清空：跨 superstep 累积，tick 结束统一 merge。
		sc.resetEffectCollectors()
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
	sc.pending = sc.pending[:0]
}

// hasWork 检查当前 superstep 是否有工作需要执行。
//
//   - includeTimers=true（首轮 superstep）：检查 timer wheel + signalRead
//   - includeTimers=false（后续 superstep）：仅检查 signalRead
func (sc *Scheduler[W, S, E, L]) hasWork(includeTimers bool) bool {
	// 检查 signalRead 是否有任何信号
	bs := sc.meta.BlockSize
	for _, c := range sc.signalRead {
		for b := range bs {
			if len(c.get(b)) > 0 {
				return true
			}
		}
	}
	// 检查 timer wheel 是否有到期条目（仅首轮）
	if includeTimers {
		for b := range bs {
			if len(sc.timerWheel.get(b)) > 0 {
				return true
			}
		}
	}
	return false
}

// ────────────────────────────────────────────────────────────────────────────
// Think Phase
// ────────────────────────────────────────────────────────────────────────────

// parallelThink 启动并行 Think 阶段。
// 每个 thread 遍历其负责的 block，消费 timer（首轮）和 signalRead，
// 产出 effect → effectCollectors，signal → signalWrite。
//
// TODO: 替换为预分配的 worker pool，避免每 superstep 创建 goroutine。
func (sc *Scheduler[W, S, E, L]) parallelThink(world W, includeTimers bool) {
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
//  1. Timer（首轮 superstep）：遍历 wheel.get(blockId) 中的到期 refId，
//     调用 Think(ctx, emptyInbox)。Logic 可能同时被 timer 和 signal 激活，
//     此时会有多次 Think 调用——设计上合法，Logic 自己处理去重。
//  2. Signal：跨所有 source thread 收集 signalRead[*][blockId] 到 flat buffer，
//     按 ref 排序后线性分组，逐组调用 Think(ctx, signals)。
//     排序替代了 map 分组，避免跨 block 的 map key 膨胀问题。
//  3. Think 返回 delay > 0 → timerWheel.set（thread-local write，无竞争）。
//  4. ctx.Publish → effectCollectors[threadId]（per-thread write，无竞争）。
//  5. ctx.Emit   → signalWrite[threadId]（per-thread write，无竞争）。
//
// 线程安全：
//   - getLogic 并发读（调用方保证无 tick 内写）
//   - signalRead[*] 在 Think 阶段只读（上轮 swap 后不再写入）
//   - effectCollectors[threadId] / signalWrite[threadId] 只有本 thread 写入
//   - timerWheel.threadBuf[threadId] 只有本 thread 写入
//   - thinkCollectBuf[threadId] 只有本 thread 读写（CacheLinePad 隔离）
func (sc *Scheduler[W, S, E, L]) thinkWorker(threadId int, world W, includeTimers bool) {
	ctx := &ThinkCtx[W, S, E]{
		World:   world,
		Emit:    sc.emitClosure(threadId),
		Publish: sc.publishClosure(threadId),
	}

	c := sc.meta.Concurrency
	flatBuf := sc.thinkCollectBuf[threadId].buf

	for _, blockId := range sc.thinkBlocks[threadId] {
		// 1. Timer 到期激活（首轮 superstep）
		if includeTimers {
			for _, refId := range sc.timerWheel.get(blockId) {
				logic, ok := sc.w.GetLogic(refId)
				if !ok {
					continue
				}
				delay := logic.Think(ctx, sliceInbox[S](nil))
				if delay > 0 {
					sc.timerWheel.set(threadId, blockId, refId, delay)
				}
			}
		}

		// 2. 跨所有 source thread 收集 signal 到 flat buffer
		flatBuf = flatBuf[:0]
		for srcThread := range c {
			flatBuf = append(flatBuf, sc.signalRead[srcThread].get(blockId)...)
		}
		if len(flatBuf) == 0 {
			continue
		}

		// 3. 按 ref 排序，线性分组调用 Think
		slices.SortFunc(flatBuf, func(a, b refVal[S]) int {
			return cmp.Compare(a.ref, b.ref)
		})
		for start := 0; start < len(flatBuf); {
			ref := flatBuf[start].ref
			end := start + 1
			for end < len(flatBuf) && flatBuf[end].ref == ref {
				end++
			}
			if logic, ok := sc.w.GetLogic(ref); ok {
				delay := logic.Think(ctx, refValInbox[S](flatBuf[start:end]))
				if delay > 0 {
					sc.timerWheel.set(threadId, blockId, ref, delay)
				}
			}
			start = end
		}
	}

	// 写回以保留 grown capacity
	sc.thinkCollectBuf[threadId].buf = flatBuf
}

// publishClosure 返回 Think 阶段 thread 专用的 effect 发射闭包。
// effect 连同 targetRef 打包为 refVal[E]，按 hash(targetRef) % BlockSize 分桶
// 存入 effectCollectors[threadId]。
func (sc *Scheduler[W, S, E, L]) publishClosure(threadId int) func(uint64, E) {
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
func (sc *Scheduler[W, S, E, L]) emitClosure(threadId int) func(uint64, S) {
	blockSize := uint64(sc.meta.BlockSize)
	return func(refId uint64, sig S) {
		sc.signalWrite[threadId].push(int(hash(refId, blockSize)), refVal[S]{refId, sig})
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Apply Phase
// ────────────────────────────────────────────────────────────────────────────

// computeApplyAssignment 根据每个 block 的 effect 总量，
// 使用 LPT（Longest Processing Time first）近似算法将 block 分配到 thread。
//
// LPT 算法：
//  1. 统计每个 block 跨所有 Think thread 的 effect 总量
//  2. 按 effect 数量降序排列
//  3. 依次将 block 分配给当前负载最低的 thread
//
// 这是经典多处理器调度的 (4/3 - 1/(3T)) 近似算法。
// 跳过 effect 数量为 0 的 block（无需 Apply）。
//
// 复杂度：O(B log B + B·T)，其中 B = BlockSize, T = Concurrency。
func (sc *Scheduler[W, S, E, L]) computeApplyAssignment() {
	c := sc.meta.Concurrency
	bs := sc.meta.BlockSize

	// 统计每个 block 的 effect 总量
	for b := range bs {
		sc.blockLoads[b].blockId = b
		sc.blockLoads[b].count = 0
		for t := range c {
			sc.blockLoads[b].count += len(sc.effectCollectors[t].get(b))
		}
	}

	// 按 effect 数量降序排列
	slices.SortFunc(sc.blockLoads, func(a, b blockLoad) int {
		return b.count - a.count // 降序
	})

	// LPT 分配
	clear(sc.threadLoads) // 归零
	for t := range c {
		sc.applyBlocks[t] = sc.applyBlocks[t][:0]
	}
	for _, bl := range sc.blockLoads {
		if bl.count == 0 {
			break // 已排序，后续都是 0
		}
		// 找当前负载最小的 thread
		minThread := 0
		for t := 1; t < c; t++ {
			if sc.threadLoads[t] < sc.threadLoads[minThread] {
				minThread = t
			}
		}
		sc.applyBlocks[minThread] = append(sc.applyBlocks[minThread], bl.blockId)
		sc.threadLoads[minThread] += bl.count
	}
}

// parallelApply 启动并行 Apply 阶段。
func (sc *Scheduler[W, S, E, L]) parallelApply(world W) {
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
func (sc *Scheduler[W, S, E, L]) applyWorker(threadId int, world W) {
	ctx := &CommitCtx[W, S]{
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
			return cmp.Compare(a.ref, b.ref)
		})
		for start := 0; start < len(flatBuf); {
			ref := flatBuf[start].ref
			end := start + 1
			for end < len(flatBuf) && flatBuf[end].ref == ref {
				end++
			}
			if logic, ok := sc.w.GetLogic(ref); ok {
				logic.Apply(ctx, refValArrangement[E](flatBuf[start:end]))
			}
			start = end
		}
	}

	// 写回以保留 grown capacity
	sc.applyCollectBuf[threadId].buf = flatBuf
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

// ────────────────────────────────────────────────────────────────────────────
// Hash
// ────────────────────────────────────────────────────────────────────────────

// hash 将 uint64 值通过整数散列后对 h 取模，产生均匀分布的桶号。
// 用于 refId → blockId 映射：hash(refId, blockSize)。
// 相比裸取模，整数散列对顺序 refId 的分布更均匀。
//
// 散列函数来源：https://gist.github.com/badboy/6267743
func hash(x, h uint64) uint64 {
	x = (^x) + (x << 18)
	x = x ^ (x >> 31)
	x = x * 21
	x = x ^ (x >> 11)
	x = x + (x << 6)
	x = x ^ (x >> 22)
	return x % h
}

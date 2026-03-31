package sched

import (
	"github.com/legamerdc/game/lib"
	"golang.org/x/sys/cpu"
)

const initialBlockTimerSlot = 16

// timerEntry 是 unified log 中的一条记录，携带 blockId 和 delay。
// 一个 timerId（logic ID）通过 RefId % BlockSize 确定性地映射到唯一一个 blockId，
// 因此 thread-local 缓冲区不需要按 block 分桶，用一个统一的 map 即可。
type timerEntry struct {
	blockId int
	delay   int64
}

// timerCollector 是单个 thread 的本地 timer 暂存区。
// 调用方应保证同一 timerId 在一个 tick 内始终落到同一个 thread，
// 这样覆盖写可以直接发生在本地 collector 中而无需同步。
//
// 与旧设计不同，这里用一个 unified log（IndexMap[V, timerEntry]）替换了
// 按 blockSize 预分配的 per-block IndexMap 数组。merge 只需遍历实际登记条目，
// 不再扫描空 block。
type timerCollector[V comparable] struct {
	_   cpu.CacheLinePad
	log lib.IndexMap[V, timerEntry] // timerId -> (blockId, delay)
}

// epochSet 是一个带 epoch 的 IndexSet。
// wheel 的每个 [slot][blockId] 单元用 epoch 标记其数据对应的绝对目标 tick。
// 当 epoch 与当前时间不匹配时，数据被视为过期（逻辑空）。
// 写入时若 epoch 过期则惰性 Clear 后更新 epoch，从而省去 advance 中的逐 block 清空。
//
// 正确性依据：在一轮 wheel 循环内（连续 wheelSize 个 tick），
// 每个物理 slot 只可能对应一个绝对目标 tick。delay 被 clamp 到 [1, wheelSize-1]，
// 所以不同 tick 的 merge 写入同一物理 slot 时，目标绝对 tick 总是相同的。
// 当 wheel 旋转满一圈后，旧 epoch ≠ 新 epoch，惰性 Clear 自动生效。
type epochSet[V comparable] struct {
	epoch int64
	set   lib.IndexSet[V]
}

// putAt 向 epochSet 写入一个值。若 epoch 不匹配则先惰性清空再写入。
func (es *epochSet[V]) putAt(v V, epoch int64) {
	if es.epoch != epoch {
		es.set.Clear()
		es.epoch = epoch
	}
	es.set.Put(v)
}

// rawAt 返回 epoch 匹配时的底层数据切片；不匹配则返回 nil。
func (es *epochSet[V]) rawAt(epoch int64) []V {
	if es.epoch != epoch {
		return nil
	}
	return es.set.Raw()
}

// timerWheel 是为 scheduler 服务的单层环形 timer wheel。
// 设计语义：
//  1. 按 block 分片，匹配 scheduler 的 block/shard 处理模型；
//  2. set 先写入 thread 本地 unified log，merge 时统一并入全局 wheel；
//  3. 同一 timerId 在同一 tick 内遵循"后写覆盖前写"；
//  4. get 只读取 currentTime 对应槽位，因此推荐调用顺序是：
//     先读取当前槽位 -> 消费到期 timer -> merge 本 tick 新注册 -> advance 进入下一 tick。
//
// 使用约束：
//   - 本结构本身不做并发保护；
//   - get / set 使用的 blockId 都是全局 blockId，且必须落在 [0, blockSize)；
//   - threadId 必须落在 [0, len(threadBuf))；
//   - time 语义是相对 currentTime 的 delay，而不是绝对时间。
//
// 性能设计：
//   - thread-local 使用 unified log（单个 IndexMap），merge 只遍历实际登记条目；
//   - wheel slot 使用 epoch-based lazy clear，advance 不做逐 block 清空。
type timerWheel[V comparable] struct {
	// config
	wheelSize int
	blockSize int

	// data
	currentTime int64
	wheel       [][]epochSet[V]     // slot -> blockId -> (epoch, timerId set)
	threadBuf   []timerCollector[V] // threadId -> unified log of timer entries
}

// newTimerWheel 创建一个按 block 和 thread 预分配的 timer wheel。
// threadBlocks 参数仅用于确定 thread 数量（len(threadBlocks)）；
// 由于 thread-local 改为 unified log，不再需要 per-thread 的 block 列表。
// 这里会为所有 wheel slot 完成 epochSet 初始化，
// 后续运行期尽量复用内部结构，避免每 tick 重新分配。
func newTimerWheel[V comparable](wheelSize, blockSize int, threadBlocks [][]int) *timerWheel[V] {
	tw := &timerWheel[V]{
		wheelSize: wheelSize,
		blockSize: blockSize,
		wheel:     make([][]epochSet[V], wheelSize),
		threadBuf: make([]timerCollector[V], len(threadBlocks)),
	}
	for i := range tw.wheel {
		tw.wheel[i] = make([]epochSet[V], blockSize)
		for j := range tw.wheel[i] {
			tw.wheel[i][j].set.Init(initialBlockTimerSlot)
		}
	}
	for i := range tw.threadBuf {
		tw.threadBuf[i].log.Init(initialBlockTimerSlot)
	}
	return tw
}

// set 在 thread 本地 unified log 中登记一个 timer。
// 这里的 blockId 直接使用全局 blockId，不做额外转换。
// 语义：
//   - time > 0：注册/覆盖该 timerId 的 delay 和 blockId；
//   - time <= 0：仅尝试取消当前 thread 本地 log 中尚未 merge 的登记。
//
// 注意：这里不会直接移除 wheel 中已经 merge 的旧 timer。
// 因此正确用法依赖 scheduler 的稳定 thread 亲和和 tick 边界流程：
// 同一 timerId 在一个 tick 内只通过其所属 thread 写入，
// 并且在 merge 之前完成本 tick 的覆盖或取消决策。
func (tw *timerWheel[V]) set(threadId int, blockId int, timerId V, time int64) {
	if time > 0 {
		tw.threadBuf[threadId].log.Put(timerId, timerEntry{blockId, time})
	} else if p, _ := tw.threadBuf[threadId].log.GetP(timerId); p > -1 {
		tw.threadBuf[threadId].log.Remove(p)
	}
}

// merge 把所有 thread 本地登记合并到全局 wheel。
// 只遍历每个 thread 的 unified log 中实际存在的条目，不扫描空 block。
// 合并后只保留"目标槽位中存在该 timerId"这一事实；
// delay 本身不会存回 wheel，因此 timer 到期后外层若需要更长延迟，
// 需要在被唤醒时重新计算并再次 set。
// 同一 block/slot 中使用 epochSet 去重，因此同一 timerId 即使被重复 merge，
// 目标槽位也只保留一份。
// wheel slot 使用 epoch-based lazy clear：写入时若 epoch 不匹配则自动清空旧数据。
func (tw *timerWheel[V]) merge() {
	lastOffset := int64(tw.wheelSize - 1)
	for threadId := range tw.threadBuf {
		tw.threadBuf[threadId].log.Iter(func(timerId V, e timerEntry) {
			targetTick := tw.currentTime + min(e.delay, lastOffset)
			targetSlot := targetTick % int64(tw.wheelSize)
			tw.wheel[targetSlot][e.blockId].putAt(timerId, targetTick)
		})
	}
}

// advance 清空所有 thread 本地 log，并把 currentTime 推进 1 tick。
// wheel slot 的清空由 epoch-based lazy clear 机制延迟到下次写入时自动完成，
// 因此 advance 本身不需要扫描 wheel。
//
// 这意味着：
//   - 当前槽位中的 timer 必须在 advance 前完成消费；
//   - merge 也应在 advance 前完成，否则本 tick 注册会被直接清掉；
//   - threadBuf 是"单 tick 暂存"，跨 tick 不保留。
func (tw *timerWheel[V]) advance() {
	for threadId := range tw.threadBuf {
		tw.threadBuf[threadId].log.Clear()
	}
	tw.currentTime++
}

// get 返回 currentTime 对应槽位、指定 blockId 下的全部到期 timerId。
// 使用 epoch 判断数据是否属于当前 tick：epoch 不匹配则返回 nil。
// 返回值直接引用内部存储：
//   - 调用方只能在当前消费阶段短暂使用；
//   - 不应跨 advance 保存；
//   - 不应修改返回切片内容。
func (tw *timerWheel[V]) get(blockId int) []V {
	timeSlot := tw.currentTime % int64(tw.wheelSize)
	return tw.wheel[timeSlot][blockId].rawAt(tw.currentTime)
}

package par

import (
	"slices"

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

// sliceInbox 将 []V 适配为 Inbox[V] 接口。
// nil 或空 slice 产生 Len()==0，表示 timer-only 激活（无信号需消费）。
// 同时用于 signal（Think）和 effect（Apply）的单元素或空 inbox。
type sliceInbox[V any] []V

func (s sliceInbox[V]) Len() int   { return len(s) }
func (s sliceInbox[V]) At(i int) V { return s[i] }

// refValInbox 将 []refVal[V] 适配为 Inbox[V] 接口。
// 用于 sort-based 分组后，将排序区间直接作为 Think/Apply 的输入，无需拷贝到独立 []V。
type refValInbox[V any] []refVal[V]

func (r refValInbox[V]) Len() int   { return len(r) }
func (r refValInbox[V]) At(i int) V { return r[i].val }

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
// Helper functions
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

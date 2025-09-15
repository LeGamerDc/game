package lib

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHeapMapRemove(t *testing.T) {
	var (
		h          HeapArrayMap[int, int, int]
		i, k, v, s int
	)
	h.Push(3, 3, 5)
	h.Push(1, 1, 2)
	h.Push(2, 2, 4)
	h.Push(4, 4, 1)
	h.Push(5, 5, 3)

	assert.True(t, h.check())

	i, v = h.Get(1)
	assert.Equal(t, 1, i)
	assert.Equal(t, 1, v)

	i, v = h.Get(2)
	assert.Equal(t, 2, i)
	assert.Equal(t, 2, v)
	h.Remove(i)
	assert.True(t, h.check())

	i, k, v, s = h.Top()
	assert.Equal(t, 3, i)
	assert.Equal(t, 4, k)
	assert.Equal(t, 4, v)
	assert.Equal(t, 1, s)
	h.Pop()

	assert.True(t, h.check())
}

func TestIndexMapRemove(t *testing.T) {
	var (
		h          HeapIndexMap[int, int, int]
		i, k, v, s int
	)
	h.Reserve(10)
	h.Push(3, 3, 5)
	h.Push(1, 1, 2)
	h.Push(2, 2, 4)
	h.Push(4, 4, 1)
	h.Push(5, 5, 3)

	assert.True(t, h.check())

	i, v = h.Get(1)
	assert.Equal(t, 1, i)
	assert.Equal(t, 1, v)

	i, v = h.Get(2)
	assert.Equal(t, 2, i)
	assert.Equal(t, 2, v)
	h.Remove(i)
	assert.True(t, h.check())

	i, k, v, s = h.Top()
	assert.Equal(t, 3, i)
	assert.Equal(t, 4, k)
	assert.Equal(t, 4, v)
	assert.Equal(t, 1, s)
	h.Pop()

	assert.True(t, h.check())
}

func TestHeapArrayMapFuzzy(t *testing.T) {
	n := 10000
	m := 1000
	var h HeapArrayMap[int, int, int]
	for b := 0; b < n; b++ {
		k := rand.N(m) + 1
		i, v := h.Get(k)
		s := rand.N(m) + 1
		if i >= 0 {
			assert.Equal(t, k, v)
			if rand.N(2) == 0 {
				h.Update(i, s)
			} else {
				h.Remove(i)
			}
		} else {
			h.Push(k, k, s)
		}
		assert.True(t, h.check())
	}
	fmt.Println(h.Size())
}

func TestHeapIndexMapFuzzy(t *testing.T) {
	n := 10000
	m := 1000
	var h HeapIndexMap[int, int, int]
	h.Reserve(1000)
	for b := 0; b < n; b++ {
		k := rand.N(m) + 1
		i, v := h.Get(k)
		s := rand.N(m) + 1
		if i >= 0 {
			assert.Equal(t, k, v)
			if rand.N(2) == 0 {
				h.Update(i, s)
			} else {
				h.Remove(i)
			}
		} else {
			h.Push(k, k, s)
		}
		assert.True(t, h.check())
	}
	fmt.Println(h.Size())
}

func TestHeapIndexMap_Filter(t *testing.T) {
	var h HeapIndexMap[int, int, int]
	h.Reserve(1000)
	for i := 0; i < 100; i++ {
		h.Push(i, i, 100-i)
	}
	h.Filter(func(i int) (keep bool) {
		return i%2 == 1
	})
	assert.Equal(t, 50, h.Size())
	h.Iter(func(i int) {
		assert.True(t, i%2 == 1)
	})
}

// TestHeapArrayMap 测试HeapArrayMap的所有功能
func TestHeapArrayMap(t *testing.T) {
	var m HeapArrayMap[string, int, string]

	// 测试空堆
	assert.Equal(t, 0, m.Size())

	// 测试添加元素
	m.Push("task1", "data1", 5)
	m.Push("task2", "data2", 2)
	m.Push("task3", "data3", 8)
	m.Push("task4", "data4", 1)

	assert.Equal(t, 4, m.Size())
	assert.True(t, m.check()) // 验证堆性质

	// 测试Top - 应该返回优先级最小的元素
	_, k, v, s := m.Top()
	assert.Equal(t, "task4", k)
	assert.Equal(t, "data4", v)
	assert.Equal(t, 1, s)

	// 测试Get
	i, v := m.Get("task2")
	assert.Equal(t, 1, i)
	assert.Equal(t, "data2", v)

	// 测试Update - 更新优先级
	i, _ = m.Get("task3")
	m.Update(i, 0) // 将task3的优先级设为0，应该成为最高优先级
	assert.True(t, m.check())

	_, k, _, s = m.Top()
	assert.Equal(t, "task3", k)
	assert.Equal(t, 0, s)

	// 测试Pop
	oldSize := m.Size()
	m.Pop()
	assert.Equal(t, oldSize-1, m.Size())
	assert.True(t, m.check())

	// 测试Remove
	i, _ = m.Get("task2")
	if i >= 0 {
		m.Remove(i)
		assert.True(t, m.check())
	}

	// 测试Iter
	count := 0
	m.Iter(func(k string, v string) bool {
		count++
		return false
	})
	assert.Equal(t, m.Size(), count)
}

// TestHeapProperty 测试堆性质的维护
func TestHeapProperty(t *testing.T) {
	var m HeapArrayMap[int, int, int]

	// 添加随机数据
	data := []int{15, 10, 20, 8, 25, 5, 12}
	for i, priority := range data {
		m.Push(i, i*10, priority)
	}

	// 验证堆性质
	assert.True(t, m.check())

	// 连续Pop应该按优先级顺序
	var poppedPriorities []int
	for m.Size() > 0 {
		_, _, _, s := m.Top()
		poppedPriorities = append(poppedPriorities, s)
		m.Pop()
		assert.True(t, m.check())
	}

	// 验证是否按升序排列（最小堆）
	assert.True(t, sort.IntsAreSorted(poppedPriorities))
}

// 覆盖：Push/Top/Pop 顺序、Update 上浮/下沉、Remove 根/中间/叶子、重复优先级

func TestHeapArrayMap_PopOrder(t *testing.T) {
	var h HeapArrayMap[int, int, int]
	for i := 0; i < 200; i++ {
		p := rand.N(1000)
		h.Push(i, i, p)
	}
	assert.True(t, h.check())

	var popped []int
	for h.Size() > 0 {
		_, _, _, s := h.Top()
		popped = append(popped, s)
		h.Pop()
		assert.True(t, h.check())
	}
	assert.True(t, sort.IntsAreSorted(popped))
}

func TestHeapArrayMap_UpdateUpDown(t *testing.T) {
	var h HeapArrayMap[string, int, string]
	h.Push("a", "A", 10)
	h.Push("b", "B", 20)
	h.Push("c", "C", 30)
	h.Push("d", "D", 40)
	assert.True(t, h.check())

	// 将 c 的优先级降低为 0，应当上浮为堆顶
	i, _ := h.Get("c")
	h.Update(i, 0)
	assert.True(t, h.check())
	_, k, _, s := h.Top()
	assert.Equal(t, "c", k)
	assert.Equal(t, 0, s)

	// 将 c 的优先级增大为 50，应当下沉，新的堆顶应为 a(10)
	h.Update(i, 50)
	assert.True(t, h.check())
	_, k, _, s = h.Top()
	assert.Equal(t, "a", k)
	assert.Equal(t, 10, s)
}

func TestHeapArrayMap_RemovePositions(t *testing.T) {
	var h HeapArrayMap[int, int, int]
	// 构建确定性的堆
	for i, p := range []int{50, 10, 30, 40, 20, 60, 70} { // 明确的结构
		h.Push(i, i, p)
	}
	assert.True(t, h.check())

	// 删除根节点
	rootIdx := int(h.h[0].i)
	rootKey := h.nk[rootIdx]
	h.Remove(rootIdx)
	assert.True(t, h.check())
	i, _ := h.Get(rootKey)
	assert.Equal(t, -1, i)

	// 删除一个叶子（当前堆最后一个元素对应的数组索引）
	leafHeapPos := len(h.h) - 1
	leafArrIdx := int(h.h[leafHeapPos].i)
	leafKey := h.nk[leafArrIdx]
	h.Remove(leafArrIdx)
	assert.True(t, h.check())
	i, _ = h.Get(leafKey)
	assert.Equal(t, -1, i)

	// 删除一个中间节点（选择当前堆顶的左孩子，如存在）
	if len(h.h) >= 3 { // 确保存在左孩子
		midArrIdx := int(h.h[1].i)
		midKey := h.nk[midArrIdx]
		h.Remove(midArrIdx)
		assert.True(t, h.check())
		i, _ = h.Get(midKey)
		assert.Equal(t, -1, i)
	}
}

func TestHeapArrayMap_DuplicatePriorities(t *testing.T) {
	var h HeapArrayMap[string, int, string]
	for i := 0; i < 10; i++ { // 插入多组相同优先级
		h.Push("a"+string(rune('0'+i)), "A", 5)
		h.Push("b"+string(rune('0'+i)), "B", 5)
	}
	assert.True(t, h.check())
	prev := -1
	for h.Size() > 0 {
		_, _, _, s := h.Top()
		if prev >= 0 {
			assert.LessOrEqual(t, prev, s)
		}
		prev = s
		h.Pop()
		assert.True(t, h.check())
	}
}

package lib

import (
	"math/rand/v2"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHeapIndexMap 测试HeapIndexMap的所有功能
func TestHeapIndexMap(t *testing.T) {
	var m HeapIndexMap[string, float64, int]
	m.Reserve(10)

	// 测试空堆
	assert.Equal(t, 0, m.Size())

	// 测试Put新元素
	m.Push("task1", 100, 5.5)
	m.Push("task2", 200, 2.1)
	m.Push("task3", 300, 8.8)
	m.Push("task4", 400, 1.0)

	assert.Equal(t, 4, m.Size())
	assert.True(t, m.check())

	// 测试Put更新现有元素
	m.Push("task2", 250, 0.5) // 更新task2的值和优先级
	assert.True(t, m.check())

	_, v := m.Get("task2")
	assert.Equal(t, 250, v)

	// 测试Top
	_, k, v, s := m.Top()
	assert.Equal(t, "task2", k) // task2现在优先级最高(0.5)
	assert.Equal(t, 250, v)
	assert.Equal(t, 0.5, s)

	// 测试GetP
	_, p := m.GetP("task1")
	assert.NotNil(t, p)
	*p = 150

	_, v = m.Get("task1")
	assert.Equal(t, 150, v)

	// 测试Update
	i, _ := m.Get("task3")
	m.Update(i, 0.1) // 更新task3优先级
	assert.True(t, m.check())

	// 测试Filter
	beforeSize := m.Size()
	m.Filter(func(v int) bool {
		return v >= 200 // 只保留值>=200的元素
	})
	assert.True(t, beforeSize >= m.Size())
	assert.True(t, m.check())

	// 验证Filter结果
	m.Iter(func(v int) {
		assert.GreaterOrEqual(t, v, 200)
	})

	// 测试Pop
	oldSize := m.Size()
	m.Pop()
	assert.Equal(t, oldSize-1, m.Size())
	assert.True(t, m.check())

	// 测试Remove
	if m.Size() > 0 {
		i, k, _, _ = m.Top()
		m.Remove(i)
		assert.True(t, m.check())

		// 验证元素确实被删除
		i, _ = m.Get(k)
		assert.Equal(t, -1, i)
	}
}

func TestHeapIndexMap_PopOrder(t *testing.T) {
	var h HeapIndexMap[int, int, int]
	h.Reserve(0)
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

func TestHeapIndexMap_UpdateExistingKey(t *testing.T) {
	var h HeapIndexMap[string, int, int]
	h.Reserve(0)
	h.Push("k1", 1, 5)
	assert.True(t, h.check())

	// Push 相同键应更新值并调整优先级
	h.Push("k1", 2, 1)
	assert.True(t, h.check())
	assert.Equal(t, 1, h.Size())
	_, k, v, s := h.Top()
	assert.Equal(t, "k1", k)
	assert.Equal(t, 2, v)
	assert.Equal(t, 1, s)
}

func TestHeapIndexMap_RemoveVariousPositions(t *testing.T) {
	var h HeapIndexMap[int, int, int]
	h.Reserve(0)
	for i, p := range []int{50, 10, 30, 40, 20, 60, 70} {
		h.Push(i, i, p)
	}
	assert.True(t, h.check())

	// 删除根
	rootArrIdx := int(h.h[0].i)
	rootKey := h.nk[rootArrIdx]
	h.Remove(rootArrIdx)
	assert.True(t, h.check())
	i, _ := h.Get(rootKey)
	assert.Equal(t, -1, i)

	// 删除叶
	leafHeapPos := len(h.h) - 1
	leafArrIdx := int(h.h[leafHeapPos].i)
	leafKey := h.nk[leafArrIdx]
	h.Remove(leafArrIdx)
	assert.True(t, h.check())
	i, _ = h.Get(leafKey)
	assert.Equal(t, -1, i)

	// 删除中间
	if len(h.h) >= 3 {
		midArrIdx := int(h.h[1].i)
		midKey := h.nk[midArrIdx]
		h.Remove(midArrIdx)
		assert.True(t, h.check())
		i, _ = h.Get(midKey)
		assert.Equal(t, -1, i)
	}
}

func TestHeapIndexMap_UpdateUpDown(t *testing.T) {
	var h HeapIndexMap[string, int, string]
	h.Reserve(0)
	h.Push("a", "A", 10)
	h.Push("b", "B", 20)
	h.Push("c", "C", 30)
	h.Push("d", "D", 40)
	assert.True(t, h.check())

	// 降低 c 的优先级到 0 上浮
	i, _ := h.Get("c")
	h.Update(i, 0)
	assert.True(t, h.check())
	_, k, _, s := h.Top()
	assert.Equal(t, "c", k)
	assert.Equal(t, 0, s)

	// 增大 c 的优先级到 50 下沉
	h.Update(i, 50)
	assert.True(t, h.check())
	_, k, _, s = h.Top()
	assert.Equal(t, "a", k)
	assert.Equal(t, 10, s)
}

package lib

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIndexSet 测试IndexSet的所有功能
func TestIndexSet(t *testing.T) {
	var s IndexSet[string]
	s.Init(5)

	// 测试空集合
	i := s.Has("nonexistent")
	assert.Equal(t, -1, i)

	// 测试Put新元素
	s.Put("key1")
	s.Put("key2")
	s.Put("key3")

	// 测试Has
	i = s.Has("key2")
	assert.Equal(t, 1, i)

	i = s.Has("key1")
	assert.Equal(t, 0, i)

	i = s.Has("key3")
	assert.Equal(t, 2, i)

	// 测试Put重复元素不会添加新条目
	s.Put("key2")
	i = s.Has("key2")
	assert.Equal(t, 1, i) // 索引不变

	count := 0
	s.Iter(func(k string) {
		count++
	})
	assert.Equal(t, 3, count) // 仍然只有3个元素

	// 测试Remove（swap-remove中间元素）
	s.Remove(1) // 移除key2，key3应被swap到位置1
	i = s.Has("key2")
	assert.Equal(t, -1, i)

	i = s.Has("key3")
	assert.Equal(t, 1, i) // key3被swap到了位置1

	i = s.Has("key1")
	assert.Equal(t, 0, i) // key1不受影响

	// 测试Iter
	keys := []string{}
	s.Iter(func(k string) {
		keys = append(keys, k)
	})
	assert.Len(t, keys, 2)
	assert.Contains(t, keys, "key1")
	assert.Contains(t, keys, "key3")
}

// TestIndexSetEdgeCases 测试IndexSet的边界情况
func TestIndexSetEdgeCases(t *testing.T) {
	var s IndexSet[int]
	s.Init(1)

	// 测试移除最后一个元素（不触发swap）
	s.Put(1)
	s.Remove(0)
	i := s.Has(1)
	assert.Equal(t, -1, i)

	// 测试多次Put同一个键
	s.Put(42)
	s.Put(42)
	s.Put(42)

	count := 0
	s.Iter(func(k int) {
		count++
		assert.Equal(t, 42, k)
	})
	assert.Equal(t, 1, count)
}

// TestIndexSetRemoveSwap 测试swap-remove的索引一致性
func TestIndexSetRemoveSwap(t *testing.T) {
	var s IndexSet[int]
	s.Init(5)

	s.Put(10)
	s.Put(20)
	s.Put(30)
	s.Put(40)
	s.Put(50)

	// 移除index=0的元素(10)，50应该被swap到位置0
	s.Remove(0)

	assert.Equal(t, -1, s.Has(10))
	assert.Equal(t, 0, s.Has(50)) // 50从位置4移到了位置0
	assert.Equal(t, 1, s.Has(20)) // 不受影响
	assert.Equal(t, 2, s.Has(30)) // 不受影响
	assert.Equal(t, 3, s.Has(40)) // 不受影响

	// 继续移除index=2的元素(30)，40应该被swap到位置2
	s.Remove(2)

	assert.Equal(t, -1, s.Has(30))
	assert.Equal(t, 0, s.Has(50))
	assert.Equal(t, 1, s.Has(20))
	assert.Equal(t, 2, s.Has(40)) // 40从位置3移到了位置2
}

// TestIndexSetRemoveLast 测试移除末尾元素不触发swap
func TestIndexSetRemoveLast(t *testing.T) {
	var s IndexSet[string]
	s.Init(3)

	s.Put("a")
	s.Put("b")
	s.Put("c")

	// 移除最后一个元素，不需要swap
	s.Remove(2)

	assert.Equal(t, -1, s.Has("c"))
	assert.Equal(t, 0, s.Has("a"))
	assert.Equal(t, 1, s.Has("b"))

	count := 0
	s.Iter(func(k string) {
		count++
	})
	assert.Equal(t, 2, count)
}

// TestIndexSetIterOrder 测试迭代按插入顺序进行
func TestIndexSetIterOrder(t *testing.T) {
	var s IndexSet[int]
	s.Init(5)

	s.Put(30)
	s.Put(10)
	s.Put(50)
	s.Put(20)
	s.Put(40)

	keys := []int{}
	s.Iter(func(k int) {
		keys = append(keys, k)
	})
	assert.Equal(t, []int{30, 10, 50, 20, 40}, keys)
}

// TestIndexSetPutAfterRemove 测试删除后再添加
func TestIndexSetPutAfterRemove(t *testing.T) {
	var s IndexSet[string]
	s.Init(3)

	s.Put("a")
	s.Put("b")
	s.Put("c")

	s.Remove(1) // 移除"b"，"c"swap到位置1

	// 重新添加"b"，应追加到末尾
	s.Put("b")
	i := s.Has("b")
	assert.Equal(t, 2, i) // "b"应在新的末尾位置

	keys := []string{}
	s.Iter(func(k string) {
		keys = append(keys, k)
	})
	assert.Equal(t, []string{"a", "c", "b"}, keys)
}

// TestIndexSetClear 测试IndexSet的Clear功能
func TestIndexSetClear(t *testing.T) {
	var s IndexSet[string]
	s.Init(4)

	s.Put("a")
	s.Put("b")
	s.Put("c")

	// 记录Clear前的容量
	capBefore := cap(s.nk)

	s.Clear()

	// 所有元素不可见
	assert.Equal(t, -1, s.Has("a"))
	assert.Equal(t, -1, s.Has("b"))
	assert.Equal(t, -1, s.Has("c"))

	// 长度归零
	count := 0
	s.Iter(func(k string) { count++ })
	assert.Equal(t, 0, count)

	// 容量保留，避免重新分配
	assert.Equal(t, capBefore, cap(s.nk))

	// Clear后可以正常复用
	s.Put("x")
	s.Put("y")

	assert.Equal(t, 0, s.Has("x"))
	assert.Equal(t, 1, s.Has("y"))
	assert.Equal(t, -1, s.Has("a")) // 旧元素依然不存在

	keys := []string{}
	s.Iter(func(k string) { keys = append(keys, k) })
	assert.Equal(t, []string{"x", "y"}, keys)
}

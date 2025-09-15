package lib

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIndexMap 测试IndexMap的所有功能
func TestIndexMap(t *testing.T) {
	var m IndexMap[string, int]
	m.Init(5)

	// 测试空映射
	i, v := m.Get("nonexistent")
	assert.Equal(t, -1, i)
	assert.Equal(t, 0, v)

	// 测试Put新元素
	m.Put("key1", 100)
	m.Put("key2", 200)
	m.Put("key3", 300)

	// 测试Get
	i, v = m.Get("key2")
	assert.Equal(t, 1, i)
	assert.Equal(t, 200, v)

	// 测试Put更新现有元素
	m.Put("key2", 250)
	i, v = m.Get("key2")
	assert.Equal(t, 1, i)
	assert.Equal(t, 250, v)

	// 测试GetP
	i, p := m.GetP("key1")
	assert.Equal(t, 0, i)
	assert.NotNil(t, p)
	assert.Equal(t, 100, *p)

	// 修改通过GetP获取的指针
	*p = 150
	_, v = m.Get("key1")
	assert.Equal(t, 150, v)

	// 测试Remove
	m.Remove(1) // 移除key2
	i, v = m.Get("key2")
	assert.Equal(t, -1, i)
	assert.Equal(t, 0, v)

	// 测试Iter
	values := []int{}
	m.Iter(func(v int) {
		values = append(values, v)
	})
	assert.Len(t, values, 2)
}

// TestIndexMapEdgeCases 测试IndexMap的边界情况
func TestIndexMapEdgeCases(t *testing.T) {
	var m IndexMap[int, string]
	m.Init(1)

	// 测试移除最后一个元素
	m.Put(1, "one")
	m.Remove(0)
	i, v := m.Get(1)
	assert.Equal(t, -1, i)
	assert.Equal(t, "", v)

	// 测试多次Put同一个键
	m.Put(1, "first")
	m.Put(1, "second") // 应该更新而不是添加新元素

	count := 0
	m.Iter(func(v string) {
		count++
		assert.Equal(t, "second", v)
	})
	assert.Equal(t, 1, count)
}

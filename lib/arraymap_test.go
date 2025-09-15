package lib

import (
	"fmt"
	"iter"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
)

const n = 20

func prepare(n int) iter.Seq2[int32, int32] {
	s := make([]int32, n)
	for i := range s {
		s[i] = int32(i)
	}
	rand.Shuffle(len(s), func(i, j int) { s[i], s[j] = s[j], s[i] })
	x := 0
	return func(y func(int32, int32) bool) {
		for x < n && y(s[x], s[x]) {
			x++
		}
	}
}

func TestGet(t *testing.T) {
	var m ArrayMap[int32, int32]
	for k, v := range prepare(n) {
		m.Put(k, v)
	}
	fmt.Println(m.nk)
	fmt.Println(m.nv)
}

func BenchmarkArrayMap_Get(b *testing.B) {
	var (
		m ArrayMap[int32, int32]
		x int
		y int32
	)
	for k, v := range prepare(n) {
		m.Put(k, v)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := int32(i % (n + 2))
		x, y = m.Get(k)
	}
	_, _ = x, y
}

func BenchmarkMap_Get(b *testing.B) {
	var (
		m = make(map[int32]int32)
		x int32
		y bool
	)
	for k, v := range prepare(n) {
		m[k] = v
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := int32(i % (n + 2))
		x, y = m[k]
	}
	_, _ = x, y
}

func BenchmarkIndexMap_Get(b *testing.B) {
	var (
		m IndexMap[int32, int32]
		x int
		y int32
	)
	m.Init(n)
	for k, v := range prepare(n) {
		m.Put(k, v)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := int32(i % (n + 2))
		x, y = m.Get(k)
	}
	_, _ = x, y
}

func BenchmarkArrayMap_Iter(b *testing.B) {
	var (
		m    ArrayMap[int32, int32]
		x, y int32
	)
	for k, v := range prepare(n) {
		m.Put(k, v)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Iter(func(k int32, v int32) bool {
			x, y = k, v
			return false
		})
	}
	_, _ = x, y
}

func BenchmarkMap_Iter(b *testing.B) {
	var (
		m    = make(map[int32]int32)
		x, y int32
	)
	for k, v := range prepare(n) {
		m[k] = v
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for k, v := range m {
			x, y = k, v
		}
	}
	_, _ = x, y
}

func BenchmarkIndexMap_Iter(b *testing.B) {
	var (
		m    IndexMap[int32, int32]
		x, y int32
	)
	m.Init(n)
	for k, v := range prepare(n) {
		m.Put(k, v)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Iter(func(v int32) {
			y = v
		})
	}
	_, _ = x, y
}

// TestArrayMap 测试ArrayMap的所有功能
func TestArrayMap(t *testing.T) {
	var m ArrayMap[string, int]

	// 测试空映射
	i, v := m.Get("nonexistent")
	assert.Equal(t, -1, i)
	assert.Equal(t, 0, v)

	// 测试添加元素
	m.Put("key1", 100)
	m.Put("key2", 200)
	m.Put("key3", 300)

	// 测试获取存在的元素
	i, v = m.Get("key2")
	assert.Equal(t, 1, i)
	assert.Equal(t, 200, v)

	// 测试GetP方法
	i, p := m.GetP("key1")
	assert.Equal(t, 0, i)
	assert.NotNil(t, p)
	assert.Equal(t, 100, *p)

	// 测试GetP不存在的键
	i, p = m.GetP("nonexistent")
	assert.Equal(t, -1, i)
	assert.Nil(t, p)

	// 测试Reserve
	m.Reserve(10)

	// 测试Remove - 移除中间元素
	m.Remove(1) // 移除key2
	i, v = m.Get("key2")
	assert.Equal(t, -1, i)
	assert.Equal(t, 0, v)

	// 测试移除后其他元素仍然可以访问
	i, v = m.Get("key1")
	assert.Equal(t, 0, i)
	assert.Equal(t, 100, v)

	i, v = m.Get("key3")
	assert.Equal(t, 1, i) // key3现在在索引1
	assert.Equal(t, 300, v)

	// 测试Iter方法
	keys := []string{}
	values := []int{}
	m.Iter(func(k string, v int) bool {
		keys = append(keys, k)
		values = append(values, v)
		return false // 不停止
	})
	assert.Len(t, keys, 2)
	assert.Len(t, values, 2)

	// 测试Iter提前停止
	count := 0
	m.Iter(func(k string, v int) bool {
		count++
		return count >= 1 // 只处理第一个元素
	})
	assert.Equal(t, 1, count)
}

// TestArrayMapEdgeCases 测试ArrayMap的边界情况
func TestArrayMapEdgeCases(t *testing.T) {
	var m ArrayMap[int, string]

	// 测试移除最后一个元素
	m.Put(1, "one")
	m.Remove(0)
	i, v := m.Get(1)
	assert.Equal(t, -1, i)
	assert.Equal(t, "", v)

	// 测试重复键（ArrayMap允许重复）
	m.Put(1, "first")
	m.Put(1, "second")

	i, v = m.Get(1) // 应该返回第一个匹配的
	assert.Equal(t, 0, i)
	assert.Equal(t, "first", v)
}

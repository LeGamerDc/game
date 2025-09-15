package lib

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
)

// BenchmarkComparison 性能对比基准测试
func BenchmarkComparison(b *testing.B) {
	const size = 512

	// 准备测试数据
	keys := make([]int, size)
	values := make([]string, size)
	for i := 0; i < size; i++ {
		keys[i] = rand.N(size * 2)
		values[i] = fmt.Sprintf("value_%d", i)
	}

	b.Run("ArrayMap_Get", func(b *testing.B) {
		var m ArrayMap[int, string]
		for i := 0; i < size; i++ {
			m.Put(keys[i], values[i])
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.Get(keys[i%size])
		}
	})

	b.Run("IndexMap_Get", func(b *testing.B) {
		var m IndexMap[int, string]
		m.Init(size)
		for i := 0; i < size; i++ {
			m.Put(keys[i], values[i])
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.Get(keys[i%size])
		}
	})

	b.Run("HeapArrayMap_Get", func(b *testing.B) {
		var m HeapArrayMap[int, float64, string]
		m.Reserve(size)
		for i := 0; i < size; i++ {
			m.Push(keys[i], values[i], float64(i))
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.Get(keys[i%size])
		}
	})

	b.Run("HeapIndexMap_Get", func(b *testing.B) {
		var m HeapIndexMap[int, float64, string]
		m.Reserve(size)
		for i := 0; i < size; i++ {
			m.Push(keys[i], values[i], float64(i))
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.Get(keys[i%size])
		}
	})
}

// TestDataStructureConsistency 测试数据结构的一致性
func TestDataStructureConsistency(t *testing.T) {
	// 使用相同的操作序列测试不同的数据结构
	operations := []struct {
		key   string
		value int
	}{
		{"a", 1}, {"b", 2}, {"c", 3}, {"d", 4}, {"e", 5},
	}

	// ArrayMap测试
	var am ArrayMap[string, int]
	for _, op := range operations {
		am.Put(op.key, op.value)
	}

	// IndexMap测试
	var im IndexMap[string, int]
	im.Init(len(operations))
	for _, op := range operations {
		im.Put(op.key, op.value)
	}

	// 验证两种数据结构的Get结果一致
	for _, op := range operations {
		_, v1 := am.Get(op.key)
		_, v2 := im.Get(op.key)
		assert.Equal(t, op.value, v1)
		assert.Equal(t, op.value, v2)
	}
}

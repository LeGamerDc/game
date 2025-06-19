package ds

import (
	"fmt"
	"iter"
	"math/rand/v2"
	"testing"
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
		m.Push(k, v)
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
		m.Push(k, v)
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
		m.Push(k, v)
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

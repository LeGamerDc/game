package ds

import (
	"cmp"
	"slices"
)

type index[S cmp.Ordered] struct {
	i  int
	sk S
}

type HeapArrayMap[K comparable, S cmp.Ordered, V any] struct {
	nk []K
	nv []V
	np []int32

	h []index[S]
}

func (m *HeapArrayMap[K, S, V]) Reserve(n int) {
	m.nk = slices.Grow(m.nk, n)
	m.nv = slices.Grow(m.nv, n)
	m.np = slices.Grow(m.np, n)
	m.h = slices.Grow(m.h, n)
}

func (m *HeapArrayMap[K, S, V]) Get(k K) (_ int, v V) {
	for i, ik := range m.nk {
		if ik == k {
			return i, m.nv[i]
		}
	}
	return -1, v
}

func (m *HeapArrayMap[K, S, V]) GetP(k K) (_ int, v *V) {
	for i, ik := range m.nk {
		if ik == k {
			return i, &m.nv[i]
		}
	}
	return -1, nil
}

func (m *HeapArrayMap[K, S, V]) Update(i int, s S) {
	p := m.np[i]
	m.h[p].sk = s
	m.np[i] = int32(m.fix(int(p)))
}

func (m *HeapArrayMap[K, S, V]) Remove(i int) {
	var zero V
	m.remove(int(m.np[i]))
	n := len(m.nk) - 1
	if n != i {
		m.nk[i], m.nv[i], m.np[i], m.nv[n] = m.nk[n], m.nv[n], m.np[n], zero
		m.h[m.np[i]].i = i
	}
	m.nk, m.nv, m.np = m.nk[:n], m.nv[:n], m.np[:n]
}

// Push 设置kv到SortedMap中，为了更好的性能，Push不检查重复K
func (m *HeapArrayMap[K, S, V]) Push(k K, v V, s S) {
	n := len(m.nk)
	m.nk = append(m.nk, k)
	m.nv = append(m.nv, v)
	m.np = append(m.np, int32(n))
	m.push(index[S]{i: n, sk: s})
}

func (m *HeapArrayMap[K, S, V]) Top() (i int, k K, v V, s S) {
	return int(m.h[0].i), m.nk[m.h[0].i], m.nv[m.h[0].i], m.h[0].sk
}

func (m *HeapArrayMap[K, S, V]) Pop() {
	m.Remove(int(m.h[0].i))
}

func (m *HeapArrayMap[K, S, V]) Size() int {
	return len(m.nk)
}

func (m *HeapArrayMap[K, S, V]) Iter(f func(k K, v V) (stop bool)) {
	for i, ik := range m.nk {
		if f(ik, m.nv[i]) {
			return
		}
	}
}

func (m *HeapArrayMap[K, S, V]) up(j int) int {
	for {
		i := (j - 1) / 2
		if i == j || m.h[i].sk <= m.h[j].sk {
			break
		}
		m.np[m.h[i].i], m.np[m.h[j].i] = int32(j), int32(i)
		m.h[i], m.h[j] = m.h[j], m.h[i]
		j = i
	}
	return j
}

func (m *HeapArrayMap[K, S, V]) down(i0 int, n int) int {
	var (
		i         = i0
		j, j1, j2 int
	)
	for {
		if j1 = 2*i + 1; j1 >= n {
			break
		}
		if j, j2 = j1, j1+1; j2 < n && m.h[j2].sk <= m.h[j].sk {
			j = j2
		}
		if m.h[i].sk <= m.h[j].sk {
			break
		}
		m.np[m.h[i].i], m.np[m.h[j].i] = int32(j), int32(i)
		m.h[i], m.h[j] = m.h[j], m.h[i]
		i = j
	}
	return i
}

func (m *HeapArrayMap[K, S, V]) fix(i int) (ni int) {
	if ni = m.down(i, len(m.h)); ni == i {
		return m.up(i)
	}
	return ni
}

func (m *HeapArrayMap[K, S, V]) push(x index[S]) int {
	m.h = append(m.h, x)
	return m.up(len(m.h) - 1)
}

func (m *HeapArrayMap[K, S, V]) pop() (x index[S]) {
	n := len(m.h) - 1
	m.np[m.h[n].i] = 0
	m.h[0], m.h[n] = m.h[n], m.h[0]
	m.down(0, n)
	m.h, x = m.h[:n], m.h[n]
	return
}

func (m *HeapArrayMap[K, S, V]) remove(i int) {
	n := len(m.h) - 1
	if n != i {
		m.np[m.h[n].i] = int32(i)
		m.h[i], m.h[n] = m.h[n], m.h[i]
		if ni := m.down(i, n); ni == i {
			m.up(i)
		}
	}
	m.h = m.h[:n]
}

func (m *HeapArrayMap[K, S, V]) check() bool {
	n := len(m.h)
	if len(m.nk) != n || len(m.nv) != n || len(m.np) != n {
		return false
	}
	for i := 0; i < n; i++ {
		if m.h[m.np[i]].i != i {
			return false
		}
	}
	return true
}

package ds

import "cmp"

type HeapIndexMap[K comparable, S cmp.Ordered, V any] struct {
	nk    []K
	nv    []V
	np    []int32
	index map[K]int32

	h []index[S]
}

func (m *HeapIndexMap[K, S, V]) Init(n int) {
	m.nk = make([]K, 0, n)
	m.nv = make([]V, 0, n)
	m.np = make([]int32, 0, n)
	m.index = make(map[K]int32)
	m.h = make([]index[S], 0, n)
}

func (m *HeapIndexMap[K, S, V]) Get(k K) (_ int, v V) {
	if i, ok := m.index[k]; ok {
		return int(i), m.nv[i]
	}
	return -1, v
}

func (m *HeapIndexMap[K, S, V]) GetP(k K) (_ int, v *V) {
	if i, ok := m.index[k]; ok {
		return int(i), &m.nv[i]
	}
	return -1, v
}

func (m *HeapIndexMap[K, S, V]) Update(i int, s S) {
	p := m.np[i]
	m.h[p].sk = s
	m.np[i] = int32(m.fix(int(p)))
}

func (m *HeapIndexMap[K, S, V]) Remove(i int) {
	var (
		zero V
		ok   = m.nk[i]
		n    = len(m.nk) - 1
	)
	m.remove(int(m.np[i]))
	if n != i {
		m.nk[i], m.nv[i], m.np[i], m.nv[n] = m.nk[n], m.nv[n], m.np[n], zero
		m.h[m.np[i]].i = i
		m.index[m.nk[i]] = int32(i)
	}
	delete(m.index, ok)
	m.nk, m.nv, m.np = m.nk[:n], m.nv[:n], m.np[:n]
}

func (m *HeapIndexMap[K, S, V]) Put(k K, v V, s S) {
	if i, ok := m.index[k]; ok {
		m.nv[i] = v
		m.Update(int(i), s)
		return
	}
	n := int32(len(m.nv))
	m.index[k] = n
	m.nk = append(m.nk, k)
	m.nv = append(m.nv, v)
	m.np = append(m.np, n)
	m.push(index[S]{i: int(n), sk: s})
}

func (m *HeapIndexMap[K, S, V]) Top() (i int, k K, v V, s S) {
	i = int(m.h[0].i)
	return i, m.nk[i], m.nv[i], m.h[0].sk
}

func (m *HeapIndexMap[K, S, V]) Pop() {
	m.Remove(int(m.h[0].i))
}

func (m *HeapIndexMap[K, S, V]) Size() int {
	return len(m.nk)
}

func (m *HeapIndexMap[K, S, V]) Iter(f func(V)) {
	for _, v := range m.nv {
		f(v)
	}
}

func (m *HeapIndexMap[K, S, V]) Filter(f func(V) (keep bool)) {
	n := len(m.nk) - 1
	for i := 0; i <= n; {
		if f(m.nv[i]) {
			i++
		} else {
			m.Remove(i)
			n--
		}
	}
}

func (m *HeapIndexMap[K, S, V]) up(j int) int {
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

func (m *HeapIndexMap[K, S, V]) down(i0 int, n int) int {
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

func (m *HeapIndexMap[K, S, V]) fix(i int) (ni int) {
	if ni = m.down(i, len(m.h)); ni == i {
		return m.up(i)
	}
	return ni
}

func (m *HeapIndexMap[K, S, V]) push(x index[S]) int {
	m.h = append(m.h, x)
	return m.up(len(m.h) - 1)
}

func (m *HeapIndexMap[K, S, V]) pop() (x index[S]) {
	n := len(m.h) - 1
	m.np[m.h[n].i] = 0
	m.h[0], m.h[n] = m.h[n], m.h[0]
	m.down(0, n)
	m.h, x = m.h[:n], m.h[n]
	return
}

func (m *HeapIndexMap[K, S, V]) remove(i int) {
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

func (m *HeapIndexMap[K, S, V]) check() bool {
	n := len(m.h)
	if len(m.nk) != n || len(m.nv) != n || len(m.np) != n || len(m.index) != n {
		return false
	}
	for i := int32(0); i < int32(n); i++ {
		if m.h[m.np[i]].i != int(i) {
			return false
		}
		if m.index[m.nk[i]] != i {
			return false
		}
	}
	return true
}

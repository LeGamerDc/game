package ds

import "slices"

type ArrayMap[K comparable, V any] struct {
	nk []K
	nv []V
}

func (m *ArrayMap[K, V]) Reserve(n int) {
	m.nk = slices.Grow(m.nk, n)
	m.nv = slices.Grow(m.nv, n)
}

func (m *ArrayMap[K, V]) Get(k K) (_ int, v V) {
	for i, ik := range m.nk {
		if ik == k {
			return i, m.nv[i]
		}
	}
	return -1, v
}

func (m *ArrayMap[K, V]) GetP(k K) (_ int, v *V) {
	for i, ik := range m.nk {
		if ik == k {
			return i, &m.nv[i]
		}
	}
	return -1, nil
}

// Push 设置kv到arrayMap中，为了更好的性能，Push不检查重复K
func (m *ArrayMap[K, V]) Push(k K, v V) {
	m.nk = append(m.nk, k)
	m.nv = append(m.nv, v)
}

func (m *ArrayMap[K, V]) Remove(i int) {
	var zero V
	n := len(m.nk) - 1
	if n != i {
		m.nk[i], m.nv[i], m.nv[n] = m.nk[n], m.nv[n], zero
	}
	m.nk, m.nv = m.nk[:n], m.nv[:n]
}

func (m *ArrayMap[K, V]) Iter(f func(K, V) (stop bool)) {
	for i, ik := range m.nk {
		if f(ik, m.nv[i]) {
			return
		}
	}
}

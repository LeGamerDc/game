package ds

type IndexMap[K comparable, V any] struct {
	index map[K]int32
	nk    []K
	nv    []V
}

func (m *IndexMap[K, V]) Init(n int) {
	m.index = make(map[K]int32)
	m.nk = make([]K, 0, n)
	m.nv = make([]V, 0, n)
}

func (m *IndexMap[K, V]) Get(k K) (_ int, v V) {
	if i, ok := m.index[k]; ok {
		return int(i), m.nv[i]
	}
	return -1, v
}

func (m *IndexMap[K, V]) GetP(k K) (_ int, v *V) {
	if i, ok := m.index[k]; ok {
		return int(i), &m.nv[i]
	}
	return -1, v
}

func (m *IndexMap[K, V]) Put(k K, v V) {
	if i, ok := m.index[k]; ok {
		m.nv[i] = v
		return
	}
	m.index[k] = int32(len(m.nv))
	m.nk = append(m.nk, k)
	m.nv = append(m.nv, v)
}

func (m *IndexMap[K, V]) Remove(i int) {
	var (
		zero V
		ok   = m.nk[i]
		n    = len(m.nv) - 1
	)
	if n != i {
		m.nk[i], m.nv[i], m.nv[n] = m.nk[n], m.nv[n], zero
		m.index[m.nk[i]] = int32(i)
	}
	delete(m.index, ok)
	m.nk, m.nv = m.nk[:n], m.nv[:n]
}

func (m *IndexMap[K, V]) Iter(f func(V)) {
	for _, v := range m.nv {
		f(v)
	}
}

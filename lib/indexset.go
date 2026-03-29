package lib

type IndexSet[K comparable] struct {
	index map[K]int32
	nk    []K
}

func (s *IndexSet[K]) Init(n int) {
	s.index = make(map[K]int32)
	s.nk = make([]K, 0, n)
}

func (s *IndexSet[K]) Clear() {
	clear(s.index)
	clear(s.nk)
	s.nk = s.nk[:0]
}

func (s *IndexSet[K]) Has(k K) int {
	if i, ok := s.index[k]; ok {
		return int(i)
	}
	return -1
}

func (s *IndexSet[K]) Put(k K) {
	if _, ok := s.index[k]; ok {
		return
	}
	s.index[k] = int32(len(s.nk))
	s.nk = append(s.nk, k)
}

func (s *IndexSet[K]) Remove(i int) {
	var (
		ok = s.nk[i]
		n  = len(s.nk) - 1
	)
	if n != i {
		s.nk[i] = s.nk[n]
		s.index[s.nk[i]] = int32(i)
	}
	delete(s.index, ok)
	s.nk = s.nk[:n]
}

func (s *IndexSet[K]) Iter(f func(K)) {
	for _, k := range s.nk {
		f(k)
	}
}

func (s *IndexSet[K]) Raw() []K {
	return s.nk
}

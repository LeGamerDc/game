package lib

// TypeMap 通过 id->下标 的映射，将 V 紧凑地存放在连续的 data 切片中，
// 避免直接使用 map[int64]V。Remove 时用最后一个元素填补空洞，保持 data 紧凑。
//
// ki 为 id->下标，ik 为下标->id（Remove 时据此修正被搬移元素的映射）。
//
// 指针失效约定：GetP/Place/Iter 返回的 *V 指向 data 内部，任何结构性修改
// （Insert/Place 新增 id、Remove、Clear）之后都不应再持有或使用此前取得的 *V——
// 扩容会搬迁整块 data，Remove 会改写并清零槽位，Clear 会清空整片。
//
// TypeMap 非并发安全，并发读写需调用方自行加锁。
type TypeMap[V any] struct {
	ki   map[int64]int64
	ik   []int64
	data []V
}

// Init 预分配容量，必须先调用
func (m *TypeMap[V]) Init(n int) {
	m.ki = make(map[int64]int64, n)
	m.ik = make([]int64, 0, n)
	m.data = make([]V, 0, n)
}

func (m *TypeMap[V]) Len() int { return len(m.data) }

func (m *TypeMap[V]) Clear() {
	clear(m.ki)
	clear(m.ik)
	clear(m.data)
	m.ik = m.ik[:0]
	m.data = m.data[:0]
}

// Insert 插入或更新 id 对应的值。
func (m *TypeMap[V]) Insert(id int64, v V) {
	if i, ok := m.ki[id]; ok {
		m.data[i] = v
		return
	}
	m.ki[id] = int64(len(m.data))
	m.ik = append(m.ik, id)
	m.data = append(m.data, v)
}

// Place 返回 id 对应槽位的指针，若不存在则创建零值槽位后返回。
func (m *TypeMap[V]) Place(id int64) *V {
	if i, ok := m.ki[id]; ok {
		return &m.data[i]
	}
	var zero V
	m.ki[id] = int64(len(m.data))
	m.ik = append(m.ik, id)
	m.data = append(m.data, zero)
	return &m.data[len(m.data)-1]
}

func (m *TypeMap[V]) Get(id int64) (v V, ok bool) {
	if i, ok := m.ki[id]; ok {
		return m.data[i], true
	}
	return v, false
}

func (m *TypeMap[V]) GetP(id int64) (*V, bool) {
	if i, ok := m.ki[id]; ok {
		return &m.data[i], true
	}
	return nil, false
}

// Remove 删除 id，返回是否存在。删除后把最后一个元素搬到空洞处，保持 data 紧凑。
func (m *TypeMap[V]) Remove(id int64) bool {
	i, ok := m.ki[id]
	if !ok {
		return false
	}
	var zero V
	n := int64(len(m.data) - 1)
	if i != n {
		last := m.ik[n]
		m.data[i] = m.data[n]
		m.ik[i] = last
		m.ki[last] = i
	}
	m.data[n] = zero
	m.data = m.data[:n]
	m.ik = m.ik[:n]
	delete(m.ki, id)
	return true
}

// Iter 按 data 顺序遍历，f 返回 true 时提前结束。
// 回调内只应读写传入的 *V，禁止做结构性修改（Insert/Place 新增 id、Remove、Clear），
// 否则可能越界 panic 或遍历错乱。
func (m *TypeMap[V]) Iter(f func(int64, *V) bool) {
	for i := range m.data {
		if f(m.ik[i], &m.data[i]) {
			return
		}
	}
}

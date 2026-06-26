package lib

import (
	"math/rand"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

// checkInvariant 校验 TypeMap 的内部不变量：
// 1. data 紧凑：len(data)==len(ik)==len(ki)
// 2. ki 与 ik 互为逆映射，且下标合法
func checkInvariant[V any](t *testing.T, m *TypeMap[V]) {
	t.Helper()
	assert.Equal(t, len(m.data), len(m.ik), "data 与 ik 长度应一致")
	assert.Equal(t, len(m.data), len(m.ki), "data 与 ki 数量应一致")
	for id, i := range m.ki {
		if assert.GreaterOrEqual(t, i, int64(0)) && assert.Less(t, i, int64(len(m.ik))) {
			assert.Equal(t, id, m.ik[i], "ki[id]=i 时应有 ik[i]=id")
		}
	}
}

// TestTypeMapBasic 测试基本的 Insert/Get/GetP 行为
func TestTypeMapBasic(t *testing.T) {
	var m TypeMap[int]

	m.Init(4)
	// 空映射
	v, ok := m.Get(1)
	assert.False(t, ok)
	assert.Equal(t, 0, v)
	p, ok := m.GetP(1)
	assert.False(t, ok)
	assert.Nil(t, p)
	assert.Equal(t, 0, m.Len())

	// Insert
	m.Insert(10, 100)
	m.Insert(20, 200)
	m.Insert(30, 300)
	assert.Equal(t, 3, m.Len())

	v, ok = m.Get(20)
	assert.True(t, ok)
	assert.Equal(t, 200, v)

	// Insert 更新已存在的 id，不新增元素
	m.Insert(20, 250)
	v, ok = m.Get(20)
	assert.True(t, ok)
	assert.Equal(t, 250, v)
	assert.Equal(t, 3, m.Len())

	// GetP 返回的指针可写
	p, ok = m.GetP(10)
	assert.True(t, ok)
	assert.NotNil(t, p)
	assert.Equal(t, 100, *p)
	*p = 150
	v, _ = m.Get(10)
	assert.Equal(t, 150, v)

	checkInvariant(t, &m)
}

// TestTypeMapPlace 测试 Place 的“取或建”语义
func TestTypeMapPlace(t *testing.T) {
	var m TypeMap[int]

	m.Init(4)
	// 不存在时创建零值槽位
	p := m.Place(1)
	assert.NotNil(t, p)
	assert.Equal(t, 0, *p)
	*p = 11
	assert.Equal(t, 1, m.Len())

	v, ok := m.Get(1)
	assert.True(t, ok)
	assert.Equal(t, 11, v)

	// 已存在时返回同一槽位
	p2 := m.Place(1)
	assert.Equal(t, 11, *p2)
	*p2 = 99
	v, _ = m.Get(1)
	assert.Equal(t, 99, v)
	assert.Equal(t, 1, m.Len())

	checkInvariant(t, &m)
}

// TestTypeMapRemoveSwap 重点测试 Remove 的 swap-remove：
// 删除中间元素后，最后一个元素被搬到空洞处，且其 id 映射被正确修正
func TestTypeMapRemoveSwap(t *testing.T) {
	var m TypeMap[int]
	m.Init(4)
	m.Insert(10, 100)
	m.Insert(20, 200)
	m.Insert(30, 300) // data: [100,200,300], ik: [10,20,30]

	// 删除中间的 20，末尾的 30 应被搬到下标 1
	assert.True(t, m.Remove(20))
	assert.Equal(t, 2, m.Len())

	// 20 不再存在
	_, ok := m.Get(20)
	assert.False(t, ok)

	// 30 仍可通过 id 访问，值不变
	v, ok := m.Get(30)
	assert.True(t, ok)
	assert.Equal(t, 300, v)

	// 30 被搬到了原本 20 的下标 1
	assert.Equal(t, int64(1), m.ki[30])
	assert.Equal(t, int64(30), m.ik[1])

	// 10 不受影响
	v, ok = m.Get(10)
	assert.True(t, ok)
	assert.Equal(t, 100, v)

	checkInvariant(t, &m)

	// 删除最后一个元素（i==n 分支）
	assert.True(t, m.Remove(30))
	assert.Equal(t, 1, m.Len())
	_, ok = m.Get(30)
	assert.False(t, ok)
	v, ok = m.Get(10)
	assert.True(t, ok)
	assert.Equal(t, 100, v)
	checkInvariant(t, &m)

	// 删除不存在的 id 返回 false
	assert.False(t, m.Remove(999))
	assert.False(t, m.Remove(30))

	// 删空
	assert.True(t, m.Remove(10))
	assert.Equal(t, 0, m.Len())
	checkInvariant(t, &m)
}

// TestTypeMapRemoveSingle 删除唯一元素
func TestTypeMapRemoveSingle(t *testing.T) {
	var m TypeMap[string]
	m.Init(4)

	m.Insert(1, "one")
	assert.True(t, m.Remove(1))
	assert.Equal(t, 0, m.Len())
	v, ok := m.Get(1)
	assert.False(t, ok)
	assert.Equal(t, "", v)
	checkInvariant(t, &m)

	// 删除后可继续复用
	m.Insert(2, "two")
	v, ok = m.Get(2)
	assert.True(t, ok)
	assert.Equal(t, "two", v)
	checkInvariant(t, &m)
}

// TestTypeMapIter 测试遍历与提前结束
func TestTypeMapIter(t *testing.T) {
	var m TypeMap[int]
	m.Init(4)

	m.Insert(1, 10)
	m.Insert(2, 20)
	m.Insert(3, 30)

	// 完整遍历，收集 id 与值
	got := map[int64]int{}
	m.Iter(func(id int64, v *int) bool {
		got[id] = *v
		return false
	})
	assert.Equal(t, map[int64]int{1: 10, 2: 20, 3: 30}, got)

	// 通过 Iter 暴露的指针可写
	m.Iter(func(_ int64, v *int) bool {
		*v += 1
		return false
	})
	v, _ := m.Get(2)
	assert.Equal(t, 21, v)

	// 提前结束：只访问一个元素
	count := 0
	m.Iter(func(_ int64, _ *int) bool {
		count++
		return true
	})
	assert.Equal(t, 1, count)
}

// TestTypeMapClear 测试 Clear 后状态归零且容量保留
func TestTypeMapClear(t *testing.T) {
	var m TypeMap[int]
	m.Init(8)
	m.Insert(1, 1)
	m.Insert(2, 2)
	m.Insert(3, 3)

	capBefore := cap(m.data)
	m.Clear()

	assert.Equal(t, 0, m.Len())
	_, ok := m.Get(1)
	assert.False(t, ok)
	assert.Equal(t, capBefore, cap(m.data), "Clear 应保留底层容量")
	checkInvariant(t, &m)

	// 复用
	m.Insert(5, 50)
	v, ok := m.Get(5)
	assert.True(t, ok)
	assert.Equal(t, 50, v)
	assert.Equal(t, int64(0), m.ki[5], "复用后下标应从 0 开始")
	checkInvariant(t, &m)
}

// TestTypeMapZeroValueUsable 零值（未 Init）可直接写入
func TestTypeMapZeroValueUsable(t *testing.T) {
	var m TypeMap[int]
	m.Init(4)

	m.Insert(1, 1)
	p := m.Place(2)
	*p = 2
	assert.Equal(t, 2, m.Len())
	checkInvariant(t, &m)
}

// TestTypeMapRemoveZeroesDeadSlot 验证删除/Clear 后底层数组被截断的尾部槽位被置零，
// 避免 V 含指针时悬挂引用（GC 安全）。直接覆盖 Remove 中 i==n 也置零的设计选择。
func TestTypeMapRemoveZeroesDeadSlot(t *testing.T) {
	// removeAndCheck 删除一个 id，并断言 [新len, 旧len) 区间的尾部槽位均为 nil
	removeAndCheck := func(t *testing.T, m *TypeMap[*int], id int64) {
		t.Helper()
		oldLen := m.Len()
		assert.True(t, m.Remove(id))
		dead := m.data[:oldLen] // 重新延展到截断前长度，访问已被丢弃的槽位
		for i := m.Len(); i < oldLen; i++ {
			assert.Nil(t, dead[i], "dead slot %d 应被置零", i)
		}
	}

	a, b, c := 1, 2, 3
	var m TypeMap[*int]
	m.Init(4)

	m.Insert(10, &a)
	m.Insert(20, &b)
	m.Insert(30, &c)

	removeAndCheck(t, &m, 20) // 删中间：末尾 &c 被搬走后原槽位应清零
	removeAndCheck(t, &m, 30) // 删末尾：i==n 分支
	removeAndCheck(t, &m, 10) // 删唯一剩余元素

	// Clear 后原 len 范围内的槽位应全部清零
	x, y := 7, 8
	m.Insert(1, &x)
	m.Insert(2, &y)
	n := m.Len()
	m.Clear()
	full := m.data[:n]
	for i := 0; i < n; i++ {
		assert.Nil(t, full[i], "Clear 后槽位 %d 应被置零", i)
	}
}

// TestTypeMapModel 用参照 map 做随机化模型测试，验证任意操作序列下不变量与值都正确
func TestTypeMapModel(t *testing.T) {
	var m TypeMap[int]
	m.Init(4)

	ref := map[int64]int{}
	r := rand.New(rand.NewSource(42))

	const ids = 50
	for step := 0; step < 20000; step++ {
		id := int64(r.Intn(ids))
		switch r.Intn(3) {
		case 0: // Insert
			val := r.Int()
			m.Insert(id, val)
			ref[id] = val
		case 1: // Place 后写值
			val := r.Int()
			*m.Place(id) = val
			ref[id] = val
		case 2: // Remove
			_, exist := ref[id]
			assert.Equal(t, exist, m.Remove(id))
			delete(ref, id)
		}

		// 每隔若干步做一次全量一致性校验
		if step%37 == 0 {
			assert.Equal(t, len(ref), m.Len())
			for k, want := range ref {
				v, ok := m.Get(k)
				assert.True(t, ok)
				assert.Equal(t, want, v)
			}
			checkInvariant(t, &m)
		}
	}

	// 末态：Iter 收集到的内容应与参照 map 完全一致
	got := map[int64]int{}
	m.Iter(func(id int64, v *int) bool {
		got[id] = *v
		return false
	})
	assert.Equal(t, ref, got)

	// data 紧凑：下标集合恰好是 [0, Len)
	idxs := make([]int, 0, len(m.ki))
	for _, i := range m.ki {
		idxs = append(idxs, int(i))
	}
	sort.Ints(idxs)
	for i := range idxs {
		assert.Equal(t, i, idxs[i], "下标应连续无空洞")
	}
}

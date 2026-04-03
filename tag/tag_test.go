package tag

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDBCompileHierarchy(t *testing.T) {
	db := NewDB()

	a := db.Compile("a")
	ab := db.Compile("a.b")
	abc := db.Compile("a.b.c")

	assert.NotEqual(t, int16(-1), a)
	assert.NotEqual(t, int16(-1), ab)
	assert.NotEqual(t, int16(-1), abc)

	assert.Equal(t, "a", db.String(a))
	assert.Equal(t, "a.b", db.String(ab))
	assert.Equal(t, "a.b.c", db.String(abc))

	assert.Equal(t, int16(-1), db.Parent(a))
	assert.Equal(t, a, db.Parent(ab))
	assert.Equal(t, ab, db.Parent(abc))
}

func TestTagAddHasAncestorClosure(t *testing.T) {
	db := NewDB()
	a := db.Compile("a")
	ab := db.Compile("a.b")
	abc := db.Compile("a.b.c")

	var tg Tag
	tg.AddTag(db, abc)

	// 具有 a.b.c 即视为具有 a.b 与 a
	assert.True(t, tg.HasTag(abc))
	assert.True(t, tg.HasTag(ab))
	assert.True(t, tg.HasTag(a))
}

func TestTagRemoveCount(t *testing.T) {
	db := NewDB()
	ab := db.Compile("a.b")

	var tg Tag
	tg.AddTag(db, ab)
	tg.AddTag(db, ab) // 计数 2

	// 第一次移除仍保留
	tg.RemoveTag(db, ab)
	assert.True(t, tg.HasTag(ab))

	// 第二次移除计数归零，缓存剔除
	tg.RemoveTag(db, ab)
	assert.False(t, tg.HasTag(ab))
}

func TestMatchAllNoneSome(t *testing.T) {
	db := NewDB()
	a := db.Compile("a")
	ab := db.Compile("a.b")
	abc := db.Compile("a.b.c")
	x := db.Compile("x")

	var tg Tag
	tg.AddTag(db, abc)

	// all 命中 + none 不命中 + some 命中
	q1, _ := NewQuery(db, []int16{ab}, []int16{x}, []int16{a})
	assert.True(t, tg.Match(q1))

	// all 中有未命中的 => false
	q2, _ := NewQuery(db, []int16{x}, nil, nil)
	assert.False(t, tg.Match(q2))

	// none 中有命中的 => false
	q3, _ := NewQuery(db, nil, []int16{a}, nil)
	assert.False(t, tg.Match(q3))

	// some 非空，全都未命中 => false
	q4, _ := NewQuery(db, nil, nil, []int16{x})
	assert.False(t, tg.Match(q4))

	// some 为空，且未触发前两条失败 => true
	q5, _ := NewQuery(db, []int16{a}, nil, nil)
	assert.True(t, tg.Match(q5))
}

func TestTagIsAncestor(t *testing.T) {
	db := NewDB()
	a := db.Compile("a")
	ab := db.Compile("a.b")
	abc := db.Compile("a.b.c")

	assert.True(t, db.IsAncestor(a, ab))
	assert.True(t, db.IsAncestor(a, abc))
	assert.True(t, db.IsAncestor(ab, abc))
	assert.False(t, db.IsAncestor(abc, ab))
	assert.False(t, db.IsAncestor(abc, a))
	assert.False(t, db.IsAncestor(ab, a))
}

func TestNewQueryNormalizesHierarchy(t *testing.T) {
	db := NewDB()
	a := db.Compile("a")
	ab := db.Compile("a.b")
	abc := db.Compile("a.b.c")
	x := db.Compile("x")
	xy := db.Compile("x.y")
	z := db.Compile("z")
	zy := db.Compile("z.y")

	q, ok := NewQuery(
		db,
		[]int16{a, ab, abc, ab},
		[]int16{zy, z, zy},
		[]int16{xy, x, xy},
	)

	assert.True(t, ok)
	assert.Equal(t, queryHasAll|queryHasNone|queryHasSome, q.kind)
	assert.Equal(t, []int16{abc, z, x}, q.tags)
	assert.Equal(t, uint16(1), q.allEnd)
	assert.Equal(t, uint16(2), q.noneEnd)
}

func TestNewQueryRejectsImpossibleQueries(t *testing.T) {
	db := NewDB()
	a := db.Compile("a")
	abc := db.Compile("a.b.c")

	_, ok1 := NewQuery(db, []int16{abc}, []int16{a}, nil)
	assert.False(t, ok1)

	_, ok2 := NewQuery(db, nil, []int16{a}, []int16{abc})
	assert.False(t, ok2)

	_, ok3 := NewQuery(nil, nil, nil, nil)
	assert.False(t, ok3)
}

func TestNewQueryDropsSomeSatisfiedByAll(t *testing.T) {
	db := NewDB()
	a := db.Compile("a")
	abc := db.Compile("a.b.c")
	x := db.Compile("x")

	q, ok := NewQuery(db, []int16{abc}, nil, []int16{a, x})

	assert.True(t, ok)
	assert.Equal(t, queryHasAll, q.kind)
	assert.Equal(t, []int16{abc}, q.tags)
	assert.Equal(t, uint16(1), q.allEnd)
	assert.Equal(t, uint16(1), q.noneEnd)
}

func TestNewQueryHandlesInvalidTags(t *testing.T) {
	db := NewDB()
	a := db.Compile("a")

	_, ok1 := NewQuery(db, []int16{-1}, nil, nil)
	assert.False(t, ok1)

	q2, ok2 := NewQuery(db, nil, []int16{-1}, []int16{a, -1})
	assert.True(t, ok2)
	assert.Equal(t, queryHasSome, q2.kind)
	assert.Equal(t, []int16{a}, q2.tags)
}

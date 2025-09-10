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
	q1 := Query{all: []int16{ab}, none: []int16{x}, some: []int16{a}}
	assert.True(t, tg.Match(q1))

	// all 中有未命中的 => false
	q2 := Query{all: []int16{x}}
	assert.False(t, tg.Match(q2))

	// none 中有命中的 => false
	q3 := Query{none: []int16{a}}
	assert.False(t, tg.Match(q3))

	// some 非空，全都未命中 => false
	q4 := Query{some: []int16{x}}
	assert.False(t, tg.Match(q4))

	// some 为空，且未触发前两条失败 => true
	q5 := Query{all: []int16{a}}
	assert.True(t, tg.Match(q5))
}

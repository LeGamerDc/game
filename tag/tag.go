package tag

import (
	"slices"

	"github.com/legamerdc/game/lib"
)

type Tag struct {
	count lib.ArrayMap[int16, int16]
	cache map[int16]struct{}
}

func (t *Tag) AddTag(d *DB, tag int16) {
	if i, pv := t.count.GetP(tag); i >= 0 {
		*pv++
		return
	}
	t.count.Put(tag, 1)
	t.rebuildCache(d)
}

func (t *Tag) RemoveTag(d *DB, tag int16) {
	if i, pv := t.count.GetP(tag); i >= 0 {
		*pv--
		if *pv <= 0 {
			t.count.Remove(i)
			t.rebuildCache(d)
		}
	}
}

func (t *Tag) rebuildCache(d *DB) {
	if t.cache == nil {
		t.cache = make(map[int16]struct{})
	} else {
		for i := range t.cache {
			delete(t.cache, i)
		}
	}
	t.count.Iter(func(tag int16, _ int16) bool {
		for tag != -1 {
			t.cache[tag] = struct{}{}
			tag = d.Parent(tag)
		}
		return false
	})
}

func (t *Tag) HasTag(tag int16) bool {
	_, ok := t.cache[tag]
	return ok
}

func (t *Tag) Match(q Query) bool {
	if slices.ContainsFunc(q.all, func(tag int16) bool {
		return !t.HasTag(tag)
	}) {
		return false
	}
	if slices.ContainsFunc(q.none, func(tag int16) bool {
		return t.HasTag(tag)
	}) {
		return false
	}
	if len(q.some) > 0 {
		return slices.ContainsFunc(q.some, func(tag int16) bool {
			return t.HasTag(tag)
		})
	}
	return true
}

type Query struct {
	all, none, some []int16
}

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
	switch q.kind {
	case 0:
		return true
	case queryHasAll:
		return t.matchAll(q.tags[:q.allEnd])
	case queryHasNone:
		return t.matchNone(q.tags[q.allEnd:q.noneEnd])
	case queryHasSome:
		return t.matchSome(q.tags[q.noneEnd:])
	case queryHasAll | queryHasNone:
		return t.matchAll(q.tags[:q.allEnd]) &&
			t.matchNone(q.tags[q.allEnd:q.noneEnd])
	case queryHasAll | queryHasSome:
		return t.matchAll(q.tags[:q.allEnd]) &&
			t.matchSome(q.tags[q.noneEnd:])
	case queryHasNone | queryHasSome:
		return t.matchNone(q.tags[q.allEnd:q.noneEnd]) &&
			t.matchSome(q.tags[q.noneEnd:])
	case queryHasAll | queryHasNone | queryHasSome:
		return t.matchAll(q.tags[:q.allEnd]) &&
			t.matchNone(q.tags[q.allEnd:q.noneEnd]) &&
			t.matchSome(q.tags[q.noneEnd:])
	}
	return true
}

func (t *Tag) matchAll(tags []int16) bool {
	return !slices.ContainsFunc(tags, func(tag int16) bool {
		return !t.HasTag(tag)
	})
}

func (t *Tag) matchNone(tags []int16) bool {
	return !slices.ContainsFunc(tags, func(tag int16) bool {
		return t.HasTag(tag)
	})
}

func (t *Tag) matchSome(tags []int16) bool {
	return slices.ContainsFunc(tags, func(tag int16) bool {
		return t.HasTag(tag)
	})
}

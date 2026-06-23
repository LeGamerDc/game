package tag

import (
	"github.com/legamerdc/game/lib"
)

// Key is the compact identifier of a hierarchical tag, assigned by DB.Build.
// The zero value (InvalidKey) means "no tag" / "no parent"; real ids start at 1,
// so a DB holds at most 65535 tags (Build errors past that).
type Key uint16

// InvalidKey is the sentinel for an absent tag or the parent of a root tag.
const InvalidKey Key = 0

// Tag is the tag set held by a single entity.
//
// It keeps two reference-counted maps:
//
//   - explicit: tag -> number of times it was explicitly granted. This is the
//     "exact" set (HasTagExact) and supports multiple grantors of the same tag
//     (a tag only disappears once every grantor revokes it).
//   - closure: tag -> number of distinct explicit tags whose ancestor chain
//     (or self) covers it. This is the "hierarchical" set (HasTag): holding
//     a.b.c makes HasTag(a.b) and HasTag(a) true.
//
// Both maps are maintained incrementally: AddTag/RemoveTag touch a single
// ancestor chain (O(depth)) on the 0<->1 transition of an explicit tag, instead
// of rebuilding the whole closure on every change.
//
// All operations on one Tag must use the same DB. Since a DB is immutable after
// Build, this just means "use your one dictionary": the closure is decremented
// by re-walking parent chains, so a DB with a different hierarchy would corrupt
// the counts.
type Tag struct {
	explicit lib.ArrayMap[Key, int16]
	closure  lib.ArrayMap[Key, int16]
}

// AddTag grants tag once. If it was already present, only its grant count is
// bumped and the closure is left untouched.
func (t *Tag) AddTag(d *DB, tag Key) {
	if !d.valid(tag) {
		return
	}
	if _, pv := t.explicit.GetP(tag); pv != nil {
		*pv++
		return
	}
	t.explicit.Put(tag, 1)
	// 0 -> 1 transition: add this tag's whole ancestor chain to the closure.
	for a := tag; a != InvalidKey; a = d.Parent(a) {
		if _, cv := t.closure.GetP(a); cv != nil {
			*cv++
		} else {
			t.closure.Put(a, 1)
		}
	}
}

// RemoveTag revokes one grant of tag. The tag (and its contribution to the
// closure) only disappears once the last grantor is removed.
func (t *Tag) RemoveTag(d *DB, tag Key) {
	i, pv := t.explicit.GetP(tag)
	if pv == nil {
		return
	}
	if *pv--; *pv > 0 {
		return
	}
	t.explicit.Remove(i)
	// 1 -> 0 transition: remove this tag's ancestor chain from the closure.
	for a := tag; a != InvalidKey; a = d.Parent(a) {
		if j, cv := t.closure.GetP(a); cv != nil {
			if *cv--; *cv <= 0 {
				t.closure.Remove(j)
			}
		}
	}
}

// HasTag reports whether tag is present hierarchically: tag itself or any of
// its descendants has been granted. {a.b.c}.HasTag(a) == true.
func (t *Tag) HasTag(tag Key) bool {
	i, _ := t.closure.Get(tag)
	return i >= 0
}

// HasTagExact reports whether tag was explicitly granted (ignoring the
// hierarchy). {a.b.c}.HasTagExact(a) == false.
func (t *Tag) HasTagExact(tag Key) bool {
	i, _ := t.explicit.Get(tag)
	return i >= 0
}

// Match evaluates a compiled Query against the tag set.
func (t *Tag) Match(q Query) bool {
	return t.eval(&q.root)
}

func (t *Tag) eval(e *Expr) bool {
	switch e.op {
	case opTrue:
		return true
	case opFalse:
		return false
	case opAllTags:
		return t.allMatch(e.tags, false)
	case opAllTagsExact:
		return t.allMatch(e.tags, true)
	case opAnyTags:
		return t.anyMatch(e.tags, false)
	case opAnyTagsExact:
		return t.anyMatch(e.tags, true)
	case opNoTags:
		return !t.anyMatch(e.tags, false)
	case opNoTagsExact:
		return !t.anyMatch(e.tags, true)
	case opAnd:
		for i := range e.exprs {
			if !t.eval(&e.exprs[i]) {
				return false
			}
		}
		return true
	case opOr:
		for i := range e.exprs {
			if t.eval(&e.exprs[i]) {
				return true
			}
		}
		return false
	case opNor:
		for i := range e.exprs {
			if t.eval(&e.exprs[i]) {
				return false
			}
		}
		return true
	}
	// Every defined op is handled above; reaching here is a programmer error
	// (e.g. a new op added without an eval arm). Fail loudly rather than letting
	// it silently match — note `false` would still flip to match under Nor.
	panic("tag: unhandled query op in eval")
}

func (t *Tag) has(tag Key, exact bool) bool {
	if exact {
		return t.HasTagExact(tag)
	}
	return t.HasTag(tag)
}

func (t *Tag) allMatch(tags []Key, exact bool) bool {
	for _, tag := range tags {
		if !t.has(tag, exact) {
			return false
		}
	}
	return true
}

func (t *Tag) anyMatch(tags []Key, exact bool) bool {
	for _, tag := range tags {
		if t.has(tag, exact) {
			return true
		}
	}
	return false
}

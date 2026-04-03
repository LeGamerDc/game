package tag

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── helpers ────────────────────────────────────────────────────────────────

// setupDB builds a shared hierarchy for most tests:
//
//	a ─ a.b ─ a.b.c
//	         ╰ a.b.d
//	x ─ x.y
//	z
func setupDB() (db *DB, a, ab, abc, abd, x, xy, z int16) {
	db = NewDB()
	a = db.Compile("a")
	ab = db.Compile("a.b")
	abc = db.Compile("a.b.c")
	abd = db.Compile("a.b.d")
	x = db.Compile("x")
	xy = db.Compile("x.y")
	z = db.Compile("z")
	return
}

func tagWith(db *DB, ids ...int16) *Tag {
	tg := &Tag{}
	for _, id := range ids {
		tg.AddTag(db, id)
	}
	return tg
}

// ── empty / trivial queries ────────────────────────────────────────────────

func TestQuery_EmptyMatchesEverything(t *testing.T) {
	db, _, _, abc, _, _, _, z := setupDB()

	q, ok := NewQuery(db, nil, nil, nil)
	assert.True(t, ok)
	assert.Equal(t, queryKind(0), q.kind)

	// Matches an entity with tags …
	tg := tagWith(db, abc, z)
	assert.True(t, tg.Match(q))

	// … and an entity with no tags at all.
	var empty Tag
	assert.True(t, empty.Match(q))
}

func TestQuery_NilDBIsImpossible(t *testing.T) {
	_, ok := NewQuery(nil, nil, nil, nil)
	assert.False(t, ok)
}

// ── all-only ───────────────────────────────────────────────────────────────

func TestQuery_AllSingle(t *testing.T) {
	db, a, ab, abc, _, _, _, _ := setupDB()

	q, _ := NewQuery(db, []int16{ab}, nil, nil)

	// Entity has a.b.c → cache contains {a, a.b, a.b.c} → matches all=[a.b].
	tg := tagWith(db, abc)
	assert.True(t, tg.Match(q))

	// Entity has only a → no a.b in cache.
	tg2 := tagWith(db, a)
	assert.False(t, tg2.Match(q))

	// Entity has a.b directly.
	tg3 := tagWith(db, ab)
	assert.True(t, tg3.Match(q))
}

func TestQuery_AllMultiple(t *testing.T) {
	db, _, _, abc, _, _, _, z := setupDB()

	q, _ := NewQuery(db, []int16{abc, z}, nil, nil)

	tg := tagWith(db, abc, z)
	assert.True(t, tg.Match(q))

	// Missing z.
	tg2 := tagWith(db, abc)
	assert.False(t, tg2.Match(q))

	// Missing abc.
	tg3 := tagWith(db, z)
	assert.False(t, tg3.Match(q))
}

// ── none-only ──────────────────────────────────────────────────────────────

func TestQuery_NoneSingle(t *testing.T) {
	db, a, _, abc, _, _, _, z := setupDB()

	// Forbid tag "a".
	q, _ := NewQuery(db, nil, []int16{a}, nil)

	// Entity has a.b.c → cache has a → fail.
	tg := tagWith(db, abc)
	assert.False(t, tg.Match(q))

	// Entity has only z → no a → pass.
	tg2 := tagWith(db, z)
	assert.True(t, tg2.Match(q))

	// Empty entity → pass.
	var empty Tag
	assert.True(t, empty.Match(q))
}

func TestQuery_NoneDoesNotImplyChildren(t *testing.T) {
	db, a, ab, _, _, _, _, _ := setupDB()

	// Forbid a.b — entity that only has "a" should still pass.
	q, _ := NewQuery(db, nil, []int16{ab}, nil)

	tg := tagWith(db, a)
	assert.True(t, tg.Match(q))
}

// ── some-only ──────────────────────────────────────────────────────────────

func TestQuery_SomeSingle(t *testing.T) {
	db, _, _, _, _, x, _, z := setupDB()

	q, _ := NewQuery(db, nil, nil, []int16{x})

	tg := tagWith(db, x)
	assert.True(t, tg.Match(q))

	tg2 := tagWith(db, z)
	assert.False(t, tg2.Match(q))
}

func TestQuery_SomeMultiple(t *testing.T) {
	db, _, _, _, _, x, _, z := setupDB()

	q, _ := NewQuery(db, nil, nil, []int16{x, z})

	// Has x.
	assert.True(t, tagWith(db, x).Match(q))
	// Has z.
	assert.True(t, tagWith(db, z).Match(q))
	// Has neither.
	var empty Tag
	assert.False(t, empty.Match(q))
}

func TestQuery_SomeMatchesViaAncestorClosure(t *testing.T) {
	db, a, _, abc, _, _, _, _ := setupDB()

	// some=[a] — entity with a.b.c should match because cache includes a.
	q, _ := NewQuery(db, nil, nil, []int16{a})
	tg := tagWith(db, abc)
	assert.True(t, tg.Match(q))
}

// ── two-section combos ─────────────────────────────────────────────────────

func TestQuery_AllAndNone(t *testing.T) {
	db, _, _, abc, _, x, _, z := setupDB()

	// Must have a.b.c, must not have x.
	q, _ := NewQuery(db, []int16{abc}, []int16{x}, nil)

	assert.True(t, tagWith(db, abc).Match(q))
	assert.True(t, tagWith(db, abc, z).Match(q))
	assert.False(t, tagWith(db, abc, x).Match(q))
	assert.False(t, tagWith(db, z).Match(q))
}

func TestQuery_AllAndSome(t *testing.T) {
	db, _, _, abc, _, x, _, z := setupDB()

	// Must have a.b.c, must have at least one of {x, z}.
	q, _ := NewQuery(db, []int16{abc}, nil, []int16{x, z})

	assert.True(t, tagWith(db, abc, x).Match(q))
	assert.True(t, tagWith(db, abc, z).Match(q))
	assert.False(t, tagWith(db, abc).Match(q))  // missing some
	assert.False(t, tagWith(db, x, z).Match(q)) // missing all
}

func TestQuery_NoneAndSome(t *testing.T) {
	db, _, _, abc, _, x, _, z := setupDB()

	// Must not have a.b.c, at least one of {x, z}.
	q, _ := NewQuery(db, nil, []int16{abc}, []int16{x, z})

	assert.True(t, tagWith(db, x).Match(q))
	assert.True(t, tagWith(db, z).Match(q))
	assert.False(t, tagWith(db, abc, x).Match(q)) // none violated
	assert.False(t, tagWith(db).Match(q))         // some not met (no tags)
}

// ── three-section combo ────────────────────────────────────────────────────

func TestQuery_AllNoneSome(t *testing.T) {
	db, _, ab, abc, abd, x, _, z := setupDB()

	// Must have a.b, must not have z, at least one of {x, a.b.d}.
	q, _ := NewQuery(db, []int16{ab}, []int16{z}, []int16{x, abd})

	assert.True(t, tagWith(db, abc, x).Match(q))     // all=a.b✓ none=z✓ some=x✓
	assert.True(t, tagWith(db, abd).Match(q))        // all=a.b✓ none=z✓ some=abd✓
	assert.False(t, tagWith(db, abc).Match(q))       // some not met
	assert.False(t, tagWith(db, abc, x, z).Match(q)) // none violated
	assert.False(t, tagWith(db, x).Match(q))         // all not met
}

// ── normalization: hierarchy dedup ─────────────────────────────────────────

func TestQuery_AllKeepsMostSpecific(t *testing.T) {
	db, a, ab, abc, _, _, _, _ := setupDB()

	// all=[a, a.b, a.b.c] → normalized to [a.b.c].
	q, _ := NewQuery(db, []int16{a, ab, abc}, nil, nil)

	assert.Equal(t, queryHasAll, q.kind)
	assert.Equal(t, []int16{abc}, q.tags)

	// Entity that has a.b but NOT a.b.c should fail.
	assert.False(t, tagWith(db, ab).Match(q))
	assert.True(t, tagWith(db, abc).Match(q))
}

func TestQuery_NoneKeepsBroadest(t *testing.T) {
	db, a, ab, abc, _, _, _, _ := setupDB()

	// none=[a, a.b, a.b.c] → normalized to [a].
	q, _ := NewQuery(db, nil, []int16{a, ab, abc}, nil)

	assert.Equal(t, queryHasNone, q.kind)
	assert.Equal(t, []int16{a}, q.tags)
}

func TestQuery_SomeKeepsBroadest(t *testing.T) {
	db, a, ab, abc, _, _, _, _ := setupDB()

	// some=[a, a.b, a.b.c] → normalized to [a].
	q, _ := NewQuery(db, nil, nil, []int16{a, ab, abc})

	assert.Equal(t, queryHasSome, q.kind)
	assert.Equal(t, []int16{a}, q.tags)
}

// ── normalization: cross-section ───────────────────────────────────────────

func TestQuery_AllNoneConflictImpossible(t *testing.T) {
	db, a, _, abc, _, _, _, _ := setupDB()

	// all=[a.b.c] none=[a] → having a.b.c implies a → impossible.
	_, ok := NewQuery(db, []int16{abc}, []int16{a}, nil)
	assert.False(t, ok)

	// Reversed: all=[a] none=[a] → same tag → impossible.
	_, ok = NewQuery(db, []int16{a}, []int16{a}, nil)
	assert.False(t, ok)
}

func TestQuery_AllNoneIndependentBranchesOK(t *testing.T) {
	db, _, _, abc, _, x, _, _ := setupDB()

	// all=[a.b.c] none=[x] → independent hierarchies → fine.
	_, ok := NewQuery(db, []int16{abc}, []int16{x}, nil)
	assert.True(t, ok)
}

func TestQuery_SomeSatisfiedByAllDropsEntireSection(t *testing.T) {
	db, a, _, abc, _, x, _, _ := setupDB()

	// all=[a.b.c] some=[a, x]
	// a is ancestor of a.b.c → a is guaranteed → entire some is true.
	q, ok := NewQuery(db, []int16{abc}, nil, []int16{a, x})

	assert.True(t, ok)
	assert.Equal(t, queryHasAll, q.kind) // some section dropped
	assert.Equal(t, []int16{abc}, q.tags)
}

func TestQuery_SomeBlockedByNoneImpossible(t *testing.T) {
	db, a, _, abc, _, _, _, _ := setupDB()

	// none=[a] some=[a.b.c] → having a.b.c implies a → violates none → blocked.
	_, ok := NewQuery(db, nil, []int16{a}, []int16{abc})
	assert.False(t, ok)
}

func TestQuery_SomePartiallyBlockedByNone(t *testing.T) {
	db, a, _, abc, _, x, _, _ := setupDB()

	// none=[a] some=[a.b.c, x] → a.b.c is blocked, x survives.
	q, ok := NewQuery(db, nil, []int16{a}, []int16{abc, x})

	assert.True(t, ok)
	assert.Equal(t, queryHasNone|queryHasSome, q.kind)

	// Must not have a, must have x.
	assert.True(t, tagWith(db, x).Match(q))
	assert.False(t, tagWith(db, abc).Match(q)) // none violated
}

// ── normalization: invalid tags ────────────────────────────────────────────

func TestQuery_InvalidTagInAllIsImpossible(t *testing.T) {
	db, _, _, _, _, _, _, _ := setupDB()

	_, ok := NewQuery(db, []int16{999}, nil, nil)
	assert.False(t, ok)
}

func TestQuery_InvalidTagsInNoneAndSomeAreDropped(t *testing.T) {
	db, a, _, _, _, _, _, _ := setupDB()

	q, ok := NewQuery(db, nil, []int16{-1, 999}, []int16{a, -1})
	assert.True(t, ok)
	// none is empty after filtering, some=[a].
	assert.Equal(t, queryHasSome, q.kind)
	assert.Equal(t, []int16{a}, q.tags)
}

func TestQuery_AllInvalidSomeBecomesNoSome(t *testing.T) {
	db, a, _, _, _, _, _, _ := setupDB()

	// some contains only invalid tags → becomes empty → no some clause.
	q, ok := NewQuery(db, []int16{a}, nil, []int16{-1})
	assert.True(t, ok)
	assert.Equal(t, queryHasAll, q.kind)
}

// ── normalization: dedup ───────────────────────────────────────────────────

func TestQuery_DuplicatesAreRemoved(t *testing.T) {
	db, _, _, abc, _, x, _, z := setupDB()

	q, _ := NewQuery(db, []int16{abc, abc, abc}, []int16{x, x}, []int16{z, z, z})

	assert.Equal(t, queryHasAll|queryHasNone|queryHasSome, q.kind)
	assert.Equal(t, []int16{abc, x, z}, q.tags)
	assert.Equal(t, uint16(1), q.allEnd)
	assert.Equal(t, uint16(2), q.noneEnd)
}

// ── match with multi-tag entities ──────────────────────────────────────────

func TestQuery_EntityWithMultipleTags(t *testing.T) {
	db, _, _, abc, abd, _, xy, z := setupDB()

	// Entity has a.b.c + x.y + z → cache = {a, a.b, a.b.c, x, x.y, z}.
	tg := tagWith(db, abc, xy, z)

	// all=[a.b.c, x.y] → ✓
	q1, _ := NewQuery(db, []int16{abc, xy}, nil, nil)
	assert.True(t, tg.Match(q1))

	// none=[a.b.d] → a.b.d not in cache → ✓
	q2, _ := NewQuery(db, nil, []int16{abd}, nil)
	assert.True(t, tg.Match(q2))

	// none=[z] → z in cache → ✗
	q3, _ := NewQuery(db, nil, []int16{z}, nil)
	assert.False(t, tg.Match(q3))

	// some=[a.b.d, z] → z in cache → ✓
	q4, _ := NewQuery(db, nil, nil, []int16{abd, z})
	assert.True(t, tg.Match(q4))
}

func TestQuery_EntityWithNoTags(t *testing.T) {
	db, a, _, _, _, _, _, _ := setupDB()

	var empty Tag

	// all → fail
	q1, _ := NewQuery(db, []int16{a}, nil, nil)
	assert.False(t, empty.Match(q1))
	// none → pass (nothing to violate)
	q2, _ := NewQuery(db, nil, []int16{a}, nil)
	assert.True(t, empty.Match(q2))
	// some → fail (nothing present)
	q3, _ := NewQuery(db, nil, nil, []int16{a})
	assert.False(t, empty.Match(q3))
	// empty query → pass
	q4, _ := NewQuery(db, nil, nil, nil)
	assert.True(t, empty.Match(q4))
}

// ── sibling branches ───────────────────────────────────────────────────────

func TestQuery_SiblingBranchesAreIndependent(t *testing.T) {
	db, _, _, abc, abd, _, _, _ := setupDB()

	// a.b.c and a.b.d share parent a.b but are independent leaves.
	tg := tagWith(db, abc)

	q1, _ := NewQuery(db, nil, []int16{abd}, nil)
	assert.True(t, tg.Match(q1)) // abd not in cache
	q2, _ := NewQuery(db, []int16{abd}, nil, nil)
	assert.False(t, tg.Match(q2)) // abd required but absent
}

// ── impossible query never matches ─────────────────────────────────────────

func TestQuery_ImpossibleNeverMatches(t *testing.T) {
	db, _, _, _, _, _, _, _ := setupDB()

	_, ok := NewQuery(db, []int16{-1}, nil, nil) // impossible
	assert.False(t, ok)
}

// ── caller slice is not mutated ────────────────────────────────────────────

func TestQuery_DoesNotMutateCallerSlices(t *testing.T) {
	db, a, ab, abc, _, x, xy, z := setupDB()

	allOrig := []int16{a, ab, abc}
	noneOrig := []int16{z, z}
	someOrig := []int16{xy, x, -1}

	allCopy := append([]int16(nil), allOrig...)
	noneCopy := append([]int16(nil), noneOrig...)
	someCopy := append([]int16(nil), someOrig...)

	_, _ = NewQuery(db, allOrig, noneOrig, someOrig)

	assert.Equal(t, allCopy, allOrig, "all slice was mutated")
	assert.Equal(t, noneCopy, noneOrig, "none slice was mutated")
	assert.Equal(t, someCopy, someOrig, "some slice was mutated")
}

// ── deep hierarchy ─────────────────────────────────────────────────────────

func TestQuery_DeepHierarchyNormalization(t *testing.T) {
	db := NewDB()
	l1 := db.Compile("l1")
	l2 := db.Compile("l1.l2")
	l3 := db.Compile("l1.l2.l3")
	l4 := db.Compile("l1.l2.l3.l4")
	l5 := db.Compile("l1.l2.l3.l4.l5")

	// all keeps deepest only.
	q, _ := NewQuery(db, []int16{l1, l2, l3, l4, l5}, nil, nil)
	assert.Equal(t, []int16{l5}, q.tags)

	// none keeps shallowest only.
	q2, _ := NewQuery(db, nil, []int16{l5, l4, l3, l2, l1}, nil)
	assert.Equal(t, []int16{l1}, q2.tags)

	// Match: entity with l5 has all ancestors.
	tg := tagWith(db, l5)
	assert.True(t, tg.Match(q))
	assert.False(t, tg.Match(q2)) // none=[l1] and cache has l1
}

// ── all + none on same leaf without ancestor relation ──────────────────────

func TestQuery_AllAndNoneSameLevelDifferentBranch(t *testing.T) {
	db, _, _, abc, abd, _, _, _ := setupDB()

	// Must have a.b.c, must not have a.b.d — these are siblings.
	q, ok := NewQuery(db, []int16{abc}, []int16{abd}, nil)
	assert.True(t, ok)

	assert.True(t, tagWith(db, abc).Match(q))
	assert.False(t, tagWith(db, abc, abd).Match(q))
	assert.False(t, tagWith(db, abd).Match(q))
}

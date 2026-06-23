package tag

import (
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// matchesNothing / matchesEverything probe a query against a representative
// spread of entities.
func matchesNothing(t *testing.T, db *DB, q Query, abc, x, z Key) {
	t.Helper()
	var empty Tag
	assert.False(t, empty.Match(q))
	assert.False(t, tagWith(db, abc).Match(q))
	assert.False(t, tagWith(db, x).Match(q))
	assert.False(t, tagWith(db, z).Match(q))
	assert.False(t, tagWith(db, abc, x, z).Match(q))
}

// ── convenience NewQuery: all / none / some ─────────────────────────────────

func TestQuery_EmptyMatchesEverything(t *testing.T) {
	db, _, _, abc, _, _, _, z := setupDB(t)
	q, err := NewQuery(db, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, opTrue, q.root.op)

	assert.True(t, tagWith(db, abc, z).Match(q))
	var empty Tag
	assert.True(t, empty.Match(q))
}

func TestQuery_NilDBIsError(t *testing.T) {
	_, err := NewQuery(nil, nil, nil, nil)
	assert.Error(t, err)
}

func TestQuery_ZeroValueMatchesNothing(t *testing.T) {
	db, _, _, abc, _, _, _, z := setupDB(t)
	var q Query // never constructed
	assert.Equal(t, opFalse, q.root.op)

	var empty Tag
	assert.False(t, empty.Match(q))
	assert.False(t, tagWith(db, abc, z).Match(q))
}

func TestQuery_AllNoneSome(t *testing.T) {
	db, a, ab, abc, abd, x, _, z := setupDB(t)

	// must have a.b, must not have z, at least one of {x, a.b.d}
	q, err := NewQuery(db, []Key{ab}, []Key{z}, []Key{x, abd})
	require.NoError(t, err)

	assert.True(t, tagWith(db, abc, x).Match(q))
	assert.True(t, tagWith(db, abd).Match(q))
	assert.False(t, tagWith(db, abc).Match(q), "some not met")
	assert.False(t, tagWith(db, abc, x, z).Match(q), "none violated")
	assert.False(t, tagWith(db, x).Match(q), "all not met")
	_ = a
}

func TestQuery_AllUsesHierarchy(t *testing.T) {
	db, a, ab, abc, _, _, _, _ := setupDB(t)
	q, _ := NewQuery(db, []Key{ab}, nil, nil)

	assert.True(t, tagWith(db, abc).Match(q), "a.b.c implies a.b")
	assert.True(t, tagWith(db, ab).Match(q))
	assert.False(t, tagWith(db, a).Match(q), "only parent a")
}

func TestQuery_NoneUsesHierarchy(t *testing.T) {
	db, a, ab, abc, _, _, _, z := setupDB(t)
	// forbid a → forbids every descendant too
	q, _ := NewQuery(db, nil, []Key{a}, nil)
	assert.False(t, tagWith(db, abc).Match(q))
	assert.True(t, tagWith(db, z).Match(q))

	// forbid a.b → an entity with only a still passes
	q2, _ := NewQuery(db, nil, []Key{ab}, nil)
	assert.True(t, tagWith(db, a).Match(q2))
}

func TestQuery_EmptySomeDoesNotForceFalse(t *testing.T) {
	db, _, ab, abc, _, _, _, _ := setupDB(t)
	// some=nil must be omitted, not treated as AnyTags([]) == false
	q, _ := NewQuery(db, []Key{ab}, nil, nil)
	assert.True(t, tagWith(db, abc).Match(q))
}

// ── B6: impossible queries never match (no construction error) ──────────────

func TestQuery_ImpossibleAllNoneMatchesNothing(t *testing.T) {
	db, a, _, abc, _, x, _, z := setupDB(t)
	// all=[a.b.c] none=[a]: a.b.c implies a → contradiction.
	q, err := NewQuery(db, []Key{abc}, []Key{a}, nil)
	require.NoError(t, err, "B6: impossible is not a build error")
	matchesNothing(t, db, q, abc, x, z)
}

func TestQuery_ImpossibleSameTagAllNone(t *testing.T) {
	db, a, _, abc, _, x, _, z := setupDB(t)
	q, err := NewQuery(db, []Key{a}, []Key{a}, nil)
	require.NoError(t, err)
	matchesNothing(t, db, q, abc, x, z)
}

func TestQuery_InvalidKeyInAllMatchesNothing(t *testing.T) {
	db, _, _, abc, _, x, _, z := setupDB(t)
	q, err := NewQuery(db, []Key{Key(9999)}, nil, nil)
	require.NoError(t, err)
	matchesNothing(t, db, q, abc, x, z)
}

func TestQuery_InvalidKeyInNoneIsHarmless(t *testing.T) {
	db, _, _, abc, _, _, _, _ := setupDB(t)
	// forbidding a non-existent tag never blocks anyone.
	q, err := NewQuery(db, nil, []Key{Key(9999)}, nil)
	require.NoError(t, err)
	assert.True(t, tagWith(db, abc).Match(q))
}

// ── leaf normalization ──────────────────────────────────────────────────────

func TestQuery_AllKeepsMostSpecific(t *testing.T) {
	db, a, ab, abc, _, _, _, _ := setupDB(t)
	q, _ := NewQueryExpr(db, AllTags(a, ab, abc))
	assert.Equal(t, opAllTags, q.root.op)
	assert.Equal(t, []Key{abc}, q.root.tags)

	assert.True(t, tagWith(db, abc).Match(q))
	assert.False(t, tagWith(db, ab).Match(q))
}

func TestQuery_NoneKeepsBroadest(t *testing.T) {
	db, a, ab, abc, _, _, _, _ := setupDB(t)
	q, _ := NewQueryExpr(db, NoTags(abc, ab, a))
	assert.Equal(t, opNoTags, q.root.op)
	assert.Equal(t, []Key{a}, q.root.tags)
}

func TestQuery_SomeKeepsBroadest(t *testing.T) {
	db, a, ab, abc, _, _, _, _ := setupDB(t)
	q, _ := NewQueryExpr(db, AnyTags(abc, ab, a))
	assert.Equal(t, opAnyTags, q.root.op)
	assert.Equal(t, []Key{a}, q.root.tags)
}

func TestQuery_Dedup(t *testing.T) {
	db, _, _, abc, _, _, _, _ := setupDB(t)
	q, _ := NewQueryExpr(db, AllTags(abc, abc, abc))
	assert.Equal(t, []Key{abc}, q.root.tags)
}

func TestQuery_DoesNotMutateInput(t *testing.T) {
	db, a, ab, abc, _, _, _, _ := setupDB(t)
	all := []Key{abc, ab, a}
	cp := slices.Clone(all)
	_, _ = NewQuery(db, all, nil, nil)
	assert.Equal(t, cp, all, "caller slice must be untouched")
}

// ── constant folding ────────────────────────────────────────────────────────

func TestQuery_EmptyLeafFolding(t *testing.T) {
	db, _, _, _, _, _, _, _ := setupDB(t)

	q1, _ := NewQueryExpr(db, AllTags())
	assert.Equal(t, opTrue, q1.root.op, "has all of nothing == true")

	q2, _ := NewQueryExpr(db, NoTags())
	assert.Equal(t, opTrue, q2.root.op, "has none of nothing == true")

	q3, _ := NewQueryExpr(db, AnyTags())
	assert.Equal(t, opFalse, q3.root.op, "has any of nothing == false")
}

func TestQuery_AndFoldsFalseChild(t *testing.T) {
	db, _, _, abc, x, _, _, z := setupDB(t)
	q, _ := NewQueryExpr(db, And(AllTags(abc), AnyTags() /* false */))
	assert.Equal(t, opFalse, q.root.op)
	matchesNothing(t, db, q, abc, x, z)
}

func TestQuery_OrFoldsTrueChild(t *testing.T) {
	db, _, _, abc, _, _, _, _ := setupDB(t)
	q, _ := NewQueryExpr(db, Or(AllTags(abc), AllTags() /* true */))
	assert.Equal(t, opTrue, q.root.op)
	var empty Tag
	assert.True(t, empty.Match(q))
}

func TestQuery_AndSingleChildUnwraps(t *testing.T) {
	db, _, _, abc, _, _, _, _ := setupDB(t)
	q, _ := NewQueryExpr(db, And(AllTags(abc)))
	assert.Equal(t, opAllTags, q.root.op, "single-child And unwraps to the child")
}

// ── nested expression trees ─────────────────────────────────────────────────

func TestQuery_OrOfAnds(t *testing.T) {
	db, _, _, abc, abd, x, _, z := setupDB(t)
	// (abc AND x) OR (abd AND z)
	q, _ := NewQueryExpr(db, Or(
		And(AllTags(abc), AllTags(x)),
		And(AllTags(abd), AllTags(z)),
	))
	assert.True(t, tagWith(db, abc, x).Match(q))
	assert.True(t, tagWith(db, abd, z).Match(q))
	assert.False(t, tagWith(db, abc, z).Match(q))
	assert.False(t, tagWith(db, abd, x).Match(q))
	assert.False(t, tagWith(db, abc).Match(q))
}

func TestQuery_Nor(t *testing.T) {
	db, _, _, abc, _, x, _, z := setupDB(t)
	// has none of {a.b.c, x}
	q, _ := NewQueryExpr(db, Nor(AllTags(abc), AllTags(x)))
	assert.True(t, tagWith(db, z).Match(q))
	var empty Tag
	assert.True(t, empty.Match(q))
	assert.False(t, tagWith(db, abc).Match(q))
	assert.False(t, tagWith(db, x).Match(q))
}

func TestQuery_NorSingleChildIsNegation(t *testing.T) {
	db, _, _, abc, _, _, _, z := setupDB(t)
	q, _ := NewQueryExpr(db, Nor(AllTags(abc)))
	assert.Equal(t, opNor, q.root.op, "single-child Nor stays a negation")
	assert.True(t, tagWith(db, z).Match(q))
	assert.False(t, tagWith(db, abc).Match(q))
}

func TestQuery_DeeplyNested(t *testing.T) {
	db, a, _, abc, abd, x, xy, z := setupDB(t)
	// has a  AND  (any of {x, z})  AND  not(abd)
	q, _ := NewQueryExpr(db, And(
		AllTags(a),
		Or(AllTags(x), AllTags(z)),
		Nor(AllTags(abd)),
	))
	assert.True(t, tagWith(db, abc, xy).Match(q))  // a✓ (x via xy)✓ not-abd✓
	assert.True(t, tagWith(db, abc, z).Match(q))    // a✓ z✓ not-abd✓
	assert.False(t, tagWith(db, abc).Match(q))      // missing x/z
	assert.False(t, tagWith(db, abd, x).Match(q))   // abd present
	assert.False(t, tagWith(db, x).Match(q))        // missing a
}

// ── exact leaves ────────────────────────────────────────────────────────────

func TestQuery_ExactIgnoresHierarchy(t *testing.T) {
	db, a, _, abc, _, _, _, _ := setupDB(t)
	q, _ := NewQueryExpr(db, AllTagsExact(a))
	assert.True(t, tagWith(db, a).Match(q), "explicit a matches")
	assert.False(t, tagWith(db, abc).Match(q), "a via closure does not")
}

func TestQuery_ExactNoCollapse(t *testing.T) {
	db, a, ab, abc, _, _, _, _ := setupDB(t)
	// Exact leaves keep both a and a.b (no most-specific collapse).
	q, _ := NewQueryExpr(db, AllTagsExact(a, ab))
	assert.Equal(t, opAllTagsExact, q.root.op)
	assert.Equal(t, []Key{a, ab}, q.root.tags)

	assert.True(t, tagWith(db, a, ab).Match(q))
	assert.False(t, tagWith(db, abc).Match(q), "no explicit a or a.b")
	assert.False(t, tagWith(db, a).Match(q), "missing exact a.b")
}

func TestQuery_NoTagsExact(t *testing.T) {
	db, a, _, abc, _, _, _, _ := setupDB(t)
	// forbid EXACT a; an entity holding a.b.c (a only via closure) is allowed.
	q, _ := NewQueryExpr(db, NoTagsExact(a))
	assert.True(t, tagWith(db, abc).Match(q))
	assert.False(t, tagWith(db, a).Match(q))
}

func TestQuery_AnyTagsExact(t *testing.T) {
	db, a, ab, abc, _, _, _, _ := setupDB(t)
	q, _ := NewQueryExpr(db, AnyTagsExact(a, ab))
	assert.True(t, tagWith(db, a).Match(q))
	assert.True(t, tagWith(db, ab).Match(q))
	assert.False(t, tagWith(db, abc).Match(q), "only closure, no exact a/a.b")
}

// ── sibling independence ─────────────────────────────────────────────────────

func TestQuery_SiblingBranchesIndependent(t *testing.T) {
	db, _, _, abc, abd, _, _, _ := setupDB(t)
	tg := tagWith(db, abc)

	q1, _ := NewQuery(db, nil, []Key{abd}, nil)
	assert.True(t, tg.Match(q1), "abd not present")
	q2, _ := NewQuery(db, []Key{abd}, nil, nil)
	assert.False(t, tg.Match(q2), "abd required but absent")
}

// TestQuery_NormalizationPreservesSemantics fuzzes random nested expression
// trees and asserts that matching the normalized Query agrees with evaluating
// the raw (un-normalized) tree — i.e. normalization never changes a result.
func TestQuery_NormalizationPreservesSemantics(t *testing.T) {
	db, keys := bigDB(t)
	rng := rand.New(rand.NewSource(7))

	var gen func(depth int) Expr
	gen = func(depth int) Expr {
		if depth <= 0 || rng.Intn(3) == 0 { // leaf
			ts := make([]Key, rng.Intn(3)+1)
			for i := range ts {
				ts[i] = keys[rng.Intn(len(keys))]
			}
			switch rng.Intn(6) {
			case 0:
				return AllTags(ts...)
			case 1:
				return AnyTags(ts...)
			case 2:
				return NoTags(ts...)
			case 3:
				return AllTagsExact(ts...)
			case 4:
				return AnyTagsExact(ts...)
			default:
				return NoTagsExact(ts...)
			}
		}
		kids := make([]Expr, rng.Intn(3)+1)
		for i := range kids {
			kids[i] = gen(depth - 1)
		}
		switch rng.Intn(3) {
		case 0:
			return And(kids...)
		case 1:
			return Or(kids...)
		default:
			return Nor(kids...)
		}
	}

	for trial := 0; trial < 400; trial++ {
		e := gen(3)
		q, err := NewQueryExpr(db, e)
		require.NoError(t, err)

		var tg Tag
		for i, n := 0, rng.Intn(6); i < n; i++ {
			tg.AddTag(db, keys[rng.Intn(len(keys))])
		}

		want := tg.eval(&e) // raw, un-normalized
		if got := tg.Match(q); got != want {
			t.Fatalf("trial %d: normalized Match=%v, raw eval=%v", trial, got, want)
		}
	}
}

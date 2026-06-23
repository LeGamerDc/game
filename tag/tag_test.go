package tag

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── closure / exact semantics ───────────────────────────────────────────────

func TestTag_HasTagAncestorClosure(t *testing.T) {
	db, a, ab, abc, _, _, _, _ := setupDB(t)
	var tg Tag
	tg.AddTag(db, abc)

	// Holding a.b.c implies a.b and a hierarchically.
	assert.True(t, tg.HasTag(abc))
	assert.True(t, tg.HasTag(ab))
	assert.True(t, tg.HasTag(a))
}

func TestTag_HasTagDoesNotImplyChildren(t *testing.T) {
	db, a, ab, _, _, _, _, _ := setupDB(t)
	var tg Tag
	tg.AddTag(db, a)

	// Holding the parent does not grant the child.
	assert.True(t, tg.HasTag(a))
	assert.False(t, tg.HasTag(ab))
}

func TestTag_HasTagExact(t *testing.T) {
	db, a, ab, abc, _, _, _, _ := setupDB(t)
	var tg Tag
	tg.AddTag(db, abc)

	assert.True(t, tg.HasTagExact(abc))
	assert.False(t, tg.HasTagExact(ab), "ancestor not exact")
	assert.False(t, tg.HasTagExact(a))
}

// ── reference counting ──────────────────────────────────────────────────────

func TestTag_RefcountedGrants(t *testing.T) {
	db, _, ab, _, _, _, _, _ := setupDB(t)
	var tg Tag
	tg.AddTag(db, ab)
	tg.AddTag(db, ab) // granted twice

	tg.RemoveTag(db, ab) // one grantor left
	assert.True(t, tg.HasTag(ab))
	assert.True(t, tg.HasTagExact(ab))

	tg.RemoveTag(db, ab) // last grantor gone
	assert.False(t, tg.HasTag(ab))
	assert.False(t, tg.HasTagExact(ab))
}

func TestTag_RemoveAbsentIsNoop(t *testing.T) {
	db, a, _, _, _, _, _, _ := setupDB(t)
	var tg Tag
	tg.RemoveTag(db, a) // must not panic or go negative
	assert.False(t, tg.HasTag(a))
	tg.AddTag(db, a)
	tg.RemoveTag(db, a)
	tg.RemoveTag(db, a) // extra remove
	assert.False(t, tg.HasTag(a))
}

func TestTag_AddInvalidKeyIgnored(t *testing.T) {
	db, _, _, _, _, _, _, _ := setupDB(t)
	var tg Tag
	tg.AddTag(db, InvalidKey)
	tg.AddTag(db, Key(9999))
	assert.False(t, tg.HasTag(InvalidKey))
	assert.False(t, tg.HasTagExact(InvalidKey))
	assert.Equal(t, 0, tg.explicit.Len())
}

// ── incremental closure maintenance ─────────────────────────────────────────

func TestTag_SharedAncestorsSurviveSiblingRemoval(t *testing.T) {
	db, a, ab, abc, abd, _, _, _ := setupDB(t)
	var tg Tag
	tg.AddTag(db, abc)
	tg.AddTag(db, abd)

	tg.RemoveTag(db, abc)
	assert.False(t, tg.HasTag(abc))
	assert.True(t, tg.HasTag(abd))
	assert.True(t, tg.HasTag(ab), "shared ancestor kept by abd")
	assert.True(t, tg.HasTag(a))

	tg.RemoveTag(db, abd)
	assert.False(t, tg.HasTag(abd))
	assert.False(t, tg.HasTag(ab))
	assert.False(t, tg.HasTag(a))
	assert.Equal(t, 0, tg.closure.Len(), "closure fully drained")
	assert.Equal(t, 0, tg.explicit.Len())
}

// TestTag_IncrementalMatchesBruteForce fuzzes add/remove and checks the
// incrementally-maintained closure/exact sets against a from-scratch recompute.
func TestTag_IncrementalMatchesBruteForce(t *testing.T) {
	db, keys := bigDB(t)
	rng := rand.New(rand.NewSource(1))

	var tg Tag
	ref := make(map[Key]int) // explicit tag -> grant count

	for step := 0; step < 1500; step++ {
		k := keys[rng.Intn(len(keys))]
		if rng.Intn(2) == 0 {
			tg.AddTag(db, k)
			ref[k]++
		} else {
			tg.RemoveTag(db, k)
			if ref[k] > 0 {
				ref[k]--
				if ref[k] == 0 {
					delete(ref, k)
				}
			}
		}

		// Recompute the expected closure COUNTS from ref (distinct explicit
		// descendants-or-self per node) — verifying counts, not just presence.
		wantClosure := make(map[Key]int)
		for e := range ref {
			for anc := e; anc != InvalidKey; anc = db.Parent(anc) {
				wantClosure[anc]++
			}
		}

		for _, q := range keys {
			ci, cv := tg.closure.Get(q)
			if want := wantClosure[q]; want == 0 {
				if ci >= 0 || tg.HasTag(q) {
					t.Fatalf("step %d: closure(%s) present, want absent", step, db.String(q))
				}
			} else if ci < 0 || int(cv) != want || !tg.HasTag(q) {
				t.Fatalf("step %d: closure(%s)=%d want %d", step, db.String(q), cv, want)
			}

			ei, ev := tg.explicit.Get(q)
			if want := ref[q]; want == 0 {
				if ei >= 0 || tg.HasTagExact(q) {
					t.Fatalf("step %d: explicit(%s) present, want absent", step, db.String(q))
				}
			} else if ei < 0 || int(ev) != want || !tg.HasTagExact(q) {
				t.Fatalf("step %d: explicit(%s)=%d want %d", step, db.String(q), ev, want)
			}
		}
	}
}

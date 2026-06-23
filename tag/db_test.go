package tag

import (
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── shared helpers ───────────────────────────────────────────────────────────

// setupDB builds the hierarchy used by most tests:
//
//	a ─ a.b ─ a.b.c
//	         ╰ a.b.d
//	x ─ x.y
//	z
func setupDB(t *testing.T) (db *DB, a, ab, abc, abd, x, xy, z Key) {
	t.Helper()
	db, err := Build(slices.Values([]string{"a.b.c", "a.b.d", "x.y", "z"}))
	require.NoError(t, err)
	mustLookup := func(s string) Key {
		id, ok := db.Lookup(s)
		require.True(t, ok, "tag %q should exist", s)
		return id
	}
	return db, mustLookup("a"), mustLookup("a.b"), mustLookup("a.b.c"),
		mustLookup("a.b.d"), mustLookup("x"), mustLookup("x.y"), mustLookup("z")
}

func tagWith(db *DB, ids ...Key) *Tag {
	tg := &Tag{}
	for _, id := range ids {
		tg.AddTag(db, id)
	}
	return tg
}

// ── Build ────────────────────────────────────────────────────────────────────

func TestBuild_AutoCreatesAncestors(t *testing.T) {
	db, err := Build(slices.Values([]string{"a.b.c"}))
	require.NoError(t, err)

	abc, ok := db.Lookup("a.b.c")
	require.True(t, ok)
	ab, ok := db.Lookup("a.b")
	require.True(t, ok, "ancestor a.b auto-created")
	a, ok := db.Lookup("a")
	require.True(t, ok, "ancestor a auto-created")

	assert.Equal(t, ab, db.Parent(abc))
	assert.Equal(t, a, db.Parent(ab))
	assert.Equal(t, InvalidKey, db.Parent(a))
	assert.Equal(t, "a.b", db.String(ab))
}

func TestBuild_CaseInsensitive(t *testing.T) {
	db, err := Build(slices.Values([]string{"A.B.C"}))
	require.NoError(t, err)

	id1, ok1 := db.Lookup("a.b.c")
	id2, ok2 := db.Lookup("A.B.C")
	assert.True(t, ok1)
	assert.True(t, ok2)
	assert.Equal(t, id1, id2)
	assert.Equal(t, "a.b.c", db.String(id1), "stored normalized (lowercase)")
}

func TestBuild_MalformedIsError(t *testing.T) {
	for _, bad := range []string{"", ".", "a.", ".a", "a..b", "a.b.", "..", "a...b"} {
		_, err := Build(slices.Values([]string{bad}))
		assert.Errorf(t, err, "expected error for %q", bad)
	}
}

func TestBuild_TooManyTagsIsError(t *testing.T) {
	// More than 65535 distinct tags overflows the uint16 Key space.
	_, err := Build(func(yield func(string) bool) {
		for i := 0; i < (1<<16)+1; i++ {
			if !yield(fmt.Sprintf("t%d", i)) {
				return
			}
		}
	})
	assert.Error(t, err)
}

func TestBuild_SharedPrefixesReused(t *testing.T) {
	db, err := Build(slices.Values([]string{"a.b.c", "a.b.d"}))
	require.NoError(t, err)

	ab, _ := db.Lookup("a.b")
	abc, _ := db.Lookup("a.b.c")
	abd, _ := db.Lookup("a.b.d")
	assert.Equal(t, ab, db.Parent(abc))
	assert.Equal(t, ab, db.Parent(abd))
}

func TestBuild_StableIdsRegardlessOfOrder(t *testing.T) {
	db1, err := Build(slices.Values([]string{"a.b", "x", "a.c"}))
	require.NoError(t, err)
	db2, err := Build(slices.Values([]string{"x", "a.c", "a.b"}))
	require.NoError(t, err)

	for _, s := range []string{"a", "a.b", "a.c", "x"} {
		id1, _ := db1.Lookup(s)
		id2, _ := db2.Lookup(s)
		assert.Equalf(t, id1, id2, "id for %q must be order-independent", s)
	}
}

func TestBuild_ParentIdAlwaysSmaller(t *testing.T) {
	db, a, ab, abc, abd, x, xy, _ := setupDB(t)
	for _, child := range []Key{ab, abc, abd, xy} {
		p := db.Parent(child)
		assert.Lessf(t, uint64(p), uint64(child), "parent of %s must have a smaller id", db.String(child))
	}
	_ = a
	_ = x
}

// ── DB queries ───────────────────────────────────────────────────────────────

func TestDB_IsAncestor(t *testing.T) {
	db, a, ab, abc, abd, x, _, _ := setupDB(t)

	assert.True(t, db.IsAncestor(a, abc))
	assert.True(t, db.IsAncestor(ab, abc))
	assert.True(t, db.IsAncestor(a, a), "ancestor-or-self includes self")
	assert.False(t, db.IsAncestor(abc, a))
	assert.False(t, db.IsAncestor(abc, abd), "siblings")
	assert.False(t, db.IsAncestor(x, abc), "independent roots")
	assert.False(t, db.IsAncestor(InvalidKey, abc))
}

func TestDB_LookupUnknown(t *testing.T) {
	db, _, _, _, _, _, _, _ := setupDB(t)
	_, ok := db.Lookup("does.not.exist")
	assert.False(t, ok)
}

func TestDB_InvalidKeyAccessors(t *testing.T) {
	db, _, _, _, _, _, _, _ := setupDB(t)
	assert.Equal(t, InvalidKey, db.Parent(InvalidKey))
	assert.Equal(t, "_", db.String(InvalidKey))
	assert.Equal(t, "_", db.String(Key(9999)))
	assert.False(t, db.valid(InvalidKey))
	assert.False(t, db.valid(Key(9999)))
}

// TestDB_ConcurrentReads exercises the immutable-after-Build guarantee under the
// race detector (go test -race).
func TestDB_ConcurrentReads(t *testing.T) {
	db, _, _, abc, _, _, _, _ := setupDB(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 2000; j++ {
				_ = db.IsAncestor(db.Parent(abc), abc)
				_, _ = db.Lookup("a.b")
				_ = db.String(abc)
			}
		}()
	}
	wg.Wait()
}

// bigDB builds a 3-level 6×6×6 forest and returns every id (leaves + ancestors).
func bigDB(t *testing.T) (*DB, []Key) {
	t.Helper()
	var tags []string
	for i := 0; i < 6; i++ {
		for j := 0; j < 6; j++ {
			for k := 0; k < 6; k++ {
				tags = append(tags, fmt.Sprintf("r%d.s%d.t%d", i, j, k))
			}
		}
	}
	db, err := Build(slices.Values(tags))
	require.NoError(t, err)

	var keys []Key
	add := func(s string) {
		id, ok := db.Lookup(s)
		require.True(t, ok)
		keys = append(keys, id)
	}
	for i := 0; i < 6; i++ {
		add(fmt.Sprintf("r%d", i))
		for j := 0; j < 6; j++ {
			add(fmt.Sprintf("r%d.s%d", i, j))
			for k := 0; k < 6; k++ {
				add(fmt.Sprintf("r%d.s%d.t%d", i, j, k))
			}
		}
	}
	return db, keys
}

package tag

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompile_ParentChain(t *testing.T) {
	b := NewDB()

	// a.b.c -> parents: a.b -> a -> -1
	idABC := b.Compile("a.b.c")
	idAB := b.Parent(idABC)
	assert.Equal(t, "a.b", b.String(idAB))

	idA := b.Parent(idAB)
	assert.Equal(t, "a", b.String(idA))
	assert.Equal(t, int16(-1), b.Parent(idA))

	// compiling an existing prefix should return the same id
	assert.Equal(t, idAB, b.Compile("a.b"))
}

func TestCompile_ReusesSharedPrefixes(t *testing.T) {
	b := NewDB()

	// Build first chain
	b.Compile("a.b.c")

	// Build second chain sharing the same prefix
	idABD := b.Compile("a.b.d")
	idAB := b.Compile("a.b")

	assert.Equal(t, idAB, b.Parent(idABD))
}

func TestCompile_MultipleRootsIndependent(t *testing.T) {
	b := NewDB()

	idAB := b.Compile("a.b")
	idXY := b.Compile("x.y")

	idA := b.Parent(idAB)
	assert.Equal(t, "a", b.String(idA))
	assert.Equal(t, int16(-1), b.Parent(idA))

	idX := b.Parent(idXY)
	assert.Equal(t, "x", b.String(idX))
	assert.Equal(t, int16(-1), b.Parent(idX))

	assert.NotEqual(t, idA, idX)
	assert.NotEqual(t, idAB, idXY)
}

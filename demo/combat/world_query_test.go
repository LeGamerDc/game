package combat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorldQueryEnemiesInRangeUsesStagedSummaries(t *testing.T) {
	w := NewWorld(testSerialMeta())
	source := NewUnit(0, Vec2{X: 0, Y: 0}, UnitStats{})
	nearEnemy := NewUnit(1, Vec2{X: 1, Y: 0}, UnitStats{})
	farEnemy := NewUnit(1, Vec2{X: 9, Y: 0}, UnitStats{})
	ally := NewUnit(0, Vec2{X: 1, Y: 1}, UnitStats{})

	sourceRef := w.AddUnit(source)
	nearRef := w.AddUnit(nearEnemy)
	w.AddUnit(farEnemy)
	w.AddUnit(ally)

	got := w.QueryEnemiesInRange(sourceRef, source.Pos, source.Team, 1.5)
	require.Len(t, got, 1)
	require.Equal(t, nearRef, got[0].Ref)
}

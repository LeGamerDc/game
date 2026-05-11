package scenario

import (
	"testing"

	"github.com/legamerdc/game/demo/combat"
	"github.com/stretchr/testify/require"
)

func TestGridWorldInitializesStableRefsPositionsAndTeams(t *testing.T) {
	w := NewGridWorld(GridConfig{Size: 3})
	units := w.Units()
	require.Len(t, units, 9)

	for i, u := range units {
		require.Equal(t, uint64(i+1), u.Id)
		x := i % 3
		y := i / 3
		require.Equal(t, combat.Vec2{X: float64(x), Y: float64(y)}, u.Pos)
		require.Equal(t, combat.Team((x+y)%2), u.Team)
	}
}

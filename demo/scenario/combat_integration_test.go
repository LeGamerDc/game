package scenario

import (
	"testing"

	"github.com/legamerdc/game/demo/combat"
	"github.com/legamerdc/game/sched"
	"github.com/stretchr/testify/require"
)

func TestGridCombatIntegrationWithDemoAbility(t *testing.T) {
	w := NewGridWorld(GridConfig{
		Size: 2,
		Stats: combat.UnitStats{
			Hp:              100,
			Attack:          5,
			AttackWindupSec: 0.25,
			ProjectileSpeed: 20,
		},
	})
	AddDemoAbilities(w)

	w.StepN(6)

	damaged := false
	for _, u := range w.Units() {
		summary, ok := w.UnitSummary(u.Id)
		require.True(t, ok)
		if summary.Hp < summary.MaxHp {
			damaged = true
			break
		}
	}
	require.True(t, damaged)
}

func TestGridCombatRunsOnParallelSchedulerPath(t *testing.T) {
	w := NewGridWorld(GridConfig{
		Size: 4,
		Meta: sched.ScheduleMeta{
			ThinkConcurrencyThreshold: 1,
			Concurrency:               2,
		},
		Stats: combat.UnitStats{
			Hp:              100,
			Attack:          8,
			AttackWindupSec: 0.25,
			ProjectileSpeed: 20,
		},
	})

	w.StepN(8)

	damaged := 0
	for _, u := range w.Units() {
		summary, ok := w.UnitSummary(u.Id)
		require.True(t, ok)
		if summary.Hp < summary.MaxHp {
			damaged++
		}
	}
	require.Positive(t, damaged)
}

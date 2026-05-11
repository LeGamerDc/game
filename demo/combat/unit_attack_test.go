package combat

import (
	"testing"

	"github.com/legamerdc/game/sched"
	"github.com/stretchr/testify/require"
)

func TestBasicAttackFiresProjectileDamageThroughScheduler(t *testing.T) {
	w := NewWorld(testSerialMeta())
	attacker := NewUnit(0, Vec2{X: 0, Y: 0}, UnitStats{Attack: 20, AttackWindupSec: 0.25, ProjectileSpeed: 12})
	target := NewUnit(1, Vec2{X: 1, Y: 0}, UnitStats{Hp: 100})
	w.AddUnit(attacker)
	targetRef := w.AddUnit(target)

	w.StepN(4)

	summary, ok := w.UnitSummary(targetRef)
	require.True(t, ok)
	require.Equal(t, 80.0, summary.Hp)
}

func TestDeathRevivesAfterEightSeconds(t *testing.T) {
	w := NewWorld(testSerialMeta())
	attacker := NewUnit(0, Vec2{X: 0, Y: 0}, UnitStats{Attack: 200, AttackWindupSec: 0.125, ProjectileSpeed: 100})
	target := NewUnit(1, Vec2{X: 1, Y: 0}, UnitStats{Hp: 100, ReviveDelaySec: 8})
	w.AddUnit(attacker)
	targetRef := w.AddUnit(target)

	w.StepN(4)
	dead, ok := w.UnitSummary(targetRef)
	require.True(t, ok)
	require.False(t, dead.Alive)
	require.Equal(t, 0.0, dead.Hp)

	w.StepN(int(8*TicksPerSecond) + 2)
	revived, ok := w.UnitSummary(targetRef)
	require.True(t, ok)
	require.True(t, revived.Alive)
	require.Equal(t, revived.MaxHp, revived.Hp)
}

func testSerialMeta() sched.ScheduleMeta {
	return sched.ScheduleMeta{ThinkConcurrencyThreshold: 100000}
}

package combat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAbilityQueueCommitsBeforeBasicAttack(t *testing.T) {
	w := NewWorld(testSerialMeta())
	caster := NewUnit(0, Vec2{X: 0, Y: 0}, UnitStats{Attack: 5})
	target := NewUnit(1, Vec2{X: 1, Y: 0}, UnitStats{Hp: 100})
	caster.AddAbility(&AbilityDef{ID: 7, CooldownSec: 10, Range: 5.1, RawDamage: 30})
	w.AddUnit(caster)
	targetRef := w.AddUnit(target)

	w.Step()

	summary, ok := w.UnitSummary(targetRef)
	require.True(t, ok)
	require.Equal(t, 70.0, summary.Hp)
}

func TestPassiveQueuesAbilityFromAttackFiredSignal(t *testing.T) {
	w := NewWorld(testSerialMeta())
	caster := NewUnit(0, Vec2{X: 0, Y: 0}, UnitStats{Attack: 1, AttackWindupSec: 0.125, ProjectileSpeed: 100})
	target := NewUnit(1, Vec2{X: 1, Y: 0}, UnitStats{Hp: 100})
	slot := caster.AddAbility(&AbilityDef{ID: 9, CooldownSec: 10, Range: 5.1, RawDamage: 25})
	caster.abilities.Slots[slot].CooldownReadyAtSec = 999
	caster.AddPassive(PassiveTrigger{On: SignalAttackFired, QueueSlot: slot, Chance: 1})
	w.AddUnit(caster)
	targetRef := w.AddUnit(target)

	w.StepN(3)

	summary, ok := w.UnitSummary(targetRef)
	require.True(t, ok)
	require.Equal(t, 74.0, summary.Hp)
}

package combat

import (
	"testing"

	"github.com/legamerdc/game/attr"
	"github.com/legamerdc/game/sched"
	"github.com/stretchr/testify/require"
)

func TestBuffModifierAppliesAndExpires(t *testing.T) {
	w := NewWorld(testSerialMeta())
	unit := NewUnit(0, Vec2{}, UnitStats{AttackSpeed: 1})
	ref := w.AddUnit(unit)

	unit.Apply(testCommitCtx(w), testEffectInbox{{
		K:         EffectApplyBuff,
		SourceRef: ref,
		Buff: &BuffDef{
			ID:          1,
			DurationSec: 0.25,
			Modifiers: []attr.ModifierTemplate{
				{Attr: DemoAttrKey_AttackSpeed, Op: attr.ModAdd, Value: 2},
			},
		},
		BuffInstanceID: 42,
	}})
	require.Equal(t, 3.0, unit.Attr(DemoAttrKey_AttackSpeed))

	w.StepN(3)

	require.Equal(t, 1.0, unit.Attr(DemoAttrKey_AttackSpeed))
}

type testEffectInbox []Effect

func (i testEffectInbox) Len() int { return len(i) }

func (i testEffectInbox) At(n int) Effect { return i[n] }

func testCommitCtx(w *World) *sched.CommitCtx[*World, Signal] {
	return &sched.CommitCtx[*World, Signal]{
		World: w,
		Emit: func(ref uint64, sig Signal) {
			w.Emit(ref, sig)
		},
		WriteStage: func(kind sched.StageKind, state sched.StagedState) {
			w.PromoteStages(testStageInbox{{RefId: 1, Kind: kind, State: state}})
		},
	}
}

type testStageInbox []sched.RefStage

func (i testStageInbox) Len() int { return len(i) }

func (i testStageInbox) At(n int) sched.RefStage { return i[n] }

package combat

import "github.com/legamerdc/game/sched"

const (
	EffectDamage sched.EffectKind = iota + 1
	EffectHeal
	EffectApplyBuff
	EffectRemoveBuff
	EffectRevive
	EffectModifyRevive
)

type Effect struct {
	K sched.EffectKind

	SourceRef uint64
	AbilityID uint32

	RawDamage float64
	Heal      float64

	Buff           *BuffDef
	BuffInstanceID uint64
	DurationSec    float64

	ReviveAtSec float64
}

func (e Effect) Kind() sched.EffectKind { return e.K }

func (e Effect) Order() int32 { return int32(e.K) }

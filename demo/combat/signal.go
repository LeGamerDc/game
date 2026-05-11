package combat

import "github.com/legamerdc/game/sched"

const (
	SignalExternalStart sched.SignalKind = iota + 1
	SignalAttackFired
	SignalSkillCommitted
	SignalDamageTaken
	SignalDamageDealt
	SignalInterrupt
	SignalDied
	SignalRevived
	SignalReviveChanged
	SignalBuffChanged
)

type Signal struct {
	K sched.SignalKind

	SourceRef uint64
	AbilityID uint32
	AtSec     float64
	Deadline  float64
	Amount    float64
}

func (s Signal) Kind() sched.SignalKind { return s.K }

func (s Signal) Order() int32 { return int32(s.K) }

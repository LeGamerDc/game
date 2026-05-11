package combat

import "github.com/legamerdc/game/sched"

type Impact struct {
	ImpactAtSec float64
	SourceRef   uint64
	TargetRef   uint64
	AbilityID   uint32
	RawDamage   float64
}

func (u *Unit) addPendingImpact(impact Impact) {
	u.pending = append(u.pending, impact)
}

func (u *Unit) processPendingImpacts(ctx *sched.ThinkCtx[*World, Signal, Effect], nowSec float64) {
	n := 0
	var due []Impact
	for _, impact := range u.pending {
		if impact.ImpactAtSec > nowSec {
			u.pending[n] = impact
			n++
			continue
		}
		due = append(due, impact)
	}
	clear(u.pending[n:])
	u.pending = u.pending[:n]

	for _, impact := range due {
		target, ok := ctx.World.UnitSummary(impact.TargetRef)
		if !ok || !target.Alive || !target.Targetable {
			continue
		}
		ctx.Publish(impact.TargetRef, Effect{
			K:         EffectDamage,
			SourceRef: impact.SourceRef,
			AbilityID: impact.AbilityID,
			RawDamage: impact.RawDamage,
		})
	}
}

func (u *Unit) nextPendingDeadline() float64 {
	var deadline float64
	for _, impact := range u.pending {
		deadline = minPositiveDeadline(deadline, impact.ImpactAtSec)
	}
	return deadline
}

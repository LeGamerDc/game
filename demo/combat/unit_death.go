package combat

import "github.com/legamerdc/game/sched"

type DeathController struct {
	ReviveAtSec float64
}

func (d *DeathController) Think(ctx *sched.ThinkCtx[*World, Signal, Effect], u *Unit, nowSec float64) {
	reviveAt := d.NextDeadline(u)
	if reviveAt > 0 && reviveAt <= nowSec {
		ctx.Publish(u.Id, Effect{K: EffectRevive, SourceRef: u.Id})
	}
}

func (d *DeathController) NextDeadline(u *Unit) float64 {
	if d.ReviveAtSec > 0 {
		return d.ReviveAtSec
	}
	return u.ReviveAtSec
}

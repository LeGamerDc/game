package combat

import "github.com/legamerdc/game/sched"

type AttackPhase uint8

const (
	AttackIdle AttackPhase = iota
	AttackWindup
	AttackCooldown
)

type BasicAttackController struct {
	Phase AttackPhase

	TargetRef uint64

	SearchExpireAtSec float64
	NextSearchAtSec   float64
	WindupEndAtSec    float64
	NextReadyAtSec    float64

	WindupSec       float64
	ProjectileSpeed float64
	TargetHoldSec   float64
	TargetSearchSec float64
}

func NewBasicAttackController(stats UnitStats) BasicAttackController {
	var c BasicAttackController
	c.Init(stats)
	return c
}

func (c *BasicAttackController) Init(stats UnitStats) {
	stats = stats.withDefaults()
	if c.WindupSec == 0 {
		c.WindupSec = stats.AttackWindupSec
	}
	if c.ProjectileSpeed == 0 {
		c.ProjectileSpeed = stats.ProjectileSpeed
	}
	if c.TargetHoldSec == 0 {
		c.TargetHoldSec = stats.TargetHoldSec
	}
	if c.TargetSearchSec == 0 {
		c.TargetSearchSec = stats.TargetSearchSec
	}
}

func (c *BasicAttackController) Reset() {
	c.Phase = AttackIdle
	c.TargetRef = 0
	c.SearchExpireAtSec = 0
	c.WindupEndAtSec = 0
}

func (c *BasicAttackController) Interrupt() {
	if c.Phase == AttackWindup {
		c.Phase = AttackCooldown
		c.WindupEndAtSec = 0
	}
}

func (c *BasicAttackController) InWindup() bool {
	return c.Phase == AttackWindup
}

func (c *BasicAttackController) Advance(ctx *sched.ThinkCtx[*World, Signal, Effect], u *Unit, nowSec float64) {
	if c.Phase == AttackWindup && c.WindupEndAtSec <= nowSec {
		c.fire(ctx, u, c.WindupEndAtSec)
	}
	if c.Phase == AttackCooldown {
		if nowSec >= c.SearchExpireAtSec || !c.validTarget(ctx.World, u) {
			c.Reset()
		}
	}
}

func (c *BasicAttackController) TryAct(ctx *sched.ThinkCtx[*World, Signal, Effect], u *Unit, nowSec float64) {
	if c.Phase == AttackWindup {
		return
	}
	if c.Phase == AttackCooldown {
		if nowSec >= c.NextReadyAtSec && c.validTarget(ctx.World, u) {
			c.startWindup(nowSec)
		}
		return
	}
	if nowSec < c.NextSearchAtSec {
		return
	}
	target, ok := c.pickTarget(ctx.World, u)
	if !ok {
		c.NextSearchAtSec = nowSec + c.TargetSearchSec
		return
	}
	c.TargetRef = target.Ref
	c.SearchExpireAtSec = nowSec + c.TargetHoldSec
	if nowSec >= c.NextReadyAtSec {
		c.startWindup(nowSec)
	}
}

func (c *BasicAttackController) NextDeadline() float64 {
	var deadline float64
	switch c.Phase {
	case AttackIdle:
		deadline = minPositiveDeadline(deadline, c.NextSearchAtSec)
	case AttackWindup:
		deadline = minPositiveDeadline(deadline, c.WindupEndAtSec)
	case AttackCooldown:
		deadline = minPositiveDeadline(deadline, c.NextReadyAtSec)
		deadline = minPositiveDeadline(deadline, c.SearchExpireAtSec)
	}
	return deadline
}

func (c *BasicAttackController) startWindup(nowSec float64) {
	c.Phase = AttackWindup
	c.WindupEndAtSec = nowSec + c.WindupSec
}

func (c *BasicAttackController) fire(ctx *sched.ThinkCtx[*World, Signal, Effect], u *Unit, fireAtSec float64) {
	fired := false
	target, ok := ctx.World.UnitSummary(c.TargetRef)
	if ok && target.Alive && target.Targetable {
		rawDamage := u.Attr(DemoAttrKey_Attack)
		impactAtSec := fireAtSec
		if c.ProjectileSpeed > 0 {
			impactAtSec += u.Pos.Distance(target.Pos) / c.ProjectileSpeed
		}
		u.addPendingImpact(Impact{
			ImpactAtSec: impactAtSec,
			SourceRef:   u.Id,
			TargetRef:   c.TargetRef,
			RawDamage:   rawDamage,
		})
		fired = true
	}
	attackSpeed := u.Attr(DemoAttrKey_AttackSpeed)
	if attackSpeed <= 0 {
		attackSpeed = 1
	}
	c.NextReadyAtSec = fireAtSec + 1/attackSpeed
	c.Phase = AttackCooldown
	c.WindupEndAtSec = 0
	if fired {
		ctx.Emit(u.Id, Signal{K: SignalAttackFired, SourceRef: u.Id, AtSec: fireAtSec})
	}
}

func (c *BasicAttackController) pickTarget(w *World, u *Unit) (UnitSummary, bool) {
	targets := w.QueryEnemiesInRange(u.Id, u.Pos, u.Team, u.Attr(DemoAttrKey_AttackRange))
	if len(targets) == 0 {
		return UnitSummary{}, false
	}
	return targets[u.rng.Intn(len(targets))], true
}

func (c *BasicAttackController) validTarget(w *World, u *Unit) bool {
	target, ok := w.UnitSummary(c.TargetRef)
	return ok && target.Alive && target.Targetable && target.Team != u.Team
}

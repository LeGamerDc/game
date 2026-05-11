package combat

import "github.com/legamerdc/game/sched"

type AbilityCommitFunc func(ctx *sched.ThinkCtx[*World, Signal, Effect], u *Unit, slot *AbilitySlot, nowSec float64) bool

type AbilityDef struct {
	ID   uint32
	Name string

	CooldownSec  float64
	PreCastSec   float64
	AfterCastSec float64

	Range           float64
	RawDamage       float64
	ProjectileSpeed float64

	OnCommit AbilityCommitFunc
}

func (def *AbilityDef) castRange(u *Unit) float64 {
	if def.Range > 0 {
		return def.Range
	}
	return u.Attr(DemoAttrKey_AttackRange)
}

func (def *AbilityDef) damage(u *Unit) float64 {
	if def.RawDamage > 0 {
		return def.RawDamage
	}
	return u.Attr(DemoAttrKey_Attack)
}

func defaultAbilityCommit(ctx *sched.ThinkCtx[*World, Signal, Effect], u *Unit, slot *AbilitySlot, nowSec float64) bool {
	def := slot.Def
	if def == nil {
		return false
	}
	target, ok := ctx.World.UnitSummary(slot.TargetRef)
	if !ok || !target.Alive || !target.Targetable {
		return false
	}

	rawDamage := def.damage(u)
	if def.ProjectileSpeed > 0 {
		u.addPendingImpact(Impact{
			ImpactAtSec: nowSec + u.Pos.Distance(target.Pos)/def.ProjectileSpeed,
			SourceRef:   u.Id,
			TargetRef:   target.Ref,
			AbilityID:   def.ID,
			RawDamage:   rawDamage,
		})
		return true
	}

	ctx.Publish(target.Ref, Effect{
		K:         EffectDamage,
		SourceRef: u.Id,
		AbilityID: def.ID,
		RawDamage: rawDamage,
	})
	return true
}

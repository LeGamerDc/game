package combat

import (
	"github.com/legamerdc/game/attr"
	"github.com/legamerdc/game/sched"
)

type BuffDef struct {
	ID   uint32
	Name string

	DurationSec float64
	Modifiers   []attr.ModifierTemplate

	PeriodSec    float64
	PeriodDamage float64
	PeriodHeal   float64
}

type BuffInstance struct {
	ID        uint64
	SourceRef uint64
	Def       *BuffDef

	Removing        bool
	ExpireAtSec     float64
	NextPeriodAtSec float64
}

type BuffTable struct {
	instances map[uint64]*BuffInstance
}

func (b *BuffTable) Init() {
	if b.instances == nil {
		b.instances = make(map[uint64]*BuffInstance)
	}
}

func (b *BuffTable) Apply(ctx *sched.CommitCtx[*World, Signal], u *Unit, e Effect, nowSec float64) {
	if e.Buff == nil {
		return
	}
	b.Init()
	id := e.BuffInstanceID
	if id == 0 {
		u.nextBuffID++
		id = u.nextBuffID
	}
	if _, exists := b.instances[id]; exists {
		b.Remove(ctx, u, id)
	}
	duration := e.DurationSec
	if duration <= 0 {
		duration = e.Buff.DurationSec
	}
	inst := &BuffInstance{
		ID:        id,
		SourceRef: e.SourceRef,
		Def:       e.Buff,
	}
	if duration > 0 {
		inst.ExpireAtSec = nowSec + duration
	}
	if e.Buff.PeriodSec > 0 {
		inst.NextPeriodAtSec = nowSec + e.Buff.PeriodSec
	}
	b.instances[id] = inst

	for _, tmpl := range e.Buff.Modifiers {
		u.Attrs.AddModifier(tmpl.Bind(id))
	}
	u.flushAttrs()
	ctx.Emit(u.Id, Signal{K: SignalBuffChanged, SourceRef: e.SourceRef, AtSec: nowSec})
}

func (b *BuffTable) Remove(ctx *sched.CommitCtx[*World, Signal], u *Unit, id uint64) {
	b.Init()
	if id == 0 {
		return
	}
	if _, ok := b.instances[id]; !ok {
		return
	}
	delete(b.instances, id)
	u.Attrs.RemoveModifiersBySource(id)
	u.flushAttrs()
	ctx.Emit(u.Id, Signal{K: SignalBuffChanged, AtSec: ctx.World.NowSec()})
}

func (b *BuffTable) Think(ctx *sched.ThinkCtx[*World, Signal, Effect], u *Unit, nowSec float64) {
	b.Init()
	var effects []Effect
	for id, inst := range b.instances {
		if inst.Removing {
			continue
		}
		if inst.ExpireAtSec > 0 && inst.ExpireAtSec <= nowSec {
			inst.Removing = true
			effects = append(effects, Effect{K: EffectRemoveBuff, SourceRef: inst.SourceRef, BuffInstanceID: id})
			continue
		}
		if inst.Def == nil || inst.Def.PeriodSec <= 0 || inst.NextPeriodAtSec <= 0 {
			continue
		}
		for inst.NextPeriodAtSec <= nowSec {
			if inst.Def.PeriodDamage > 0 {
				effects = append(effects, Effect{K: EffectDamage, SourceRef: inst.SourceRef, RawDamage: inst.Def.PeriodDamage})
			}
			if inst.Def.PeriodHeal > 0 {
				effects = append(effects, Effect{K: EffectHeal, SourceRef: inst.SourceRef, Heal: inst.Def.PeriodHeal})
			}
			inst.NextPeriodAtSec += inst.Def.PeriodSec
		}
	}
	for _, effect := range effects {
		ctx.Publish(u.Id, effect)
	}
}

func (b *BuffTable) NextDeadline() float64 {
	b.Init()
	var deadline float64
	for _, inst := range b.instances {
		if inst.Removing {
			continue
		}
		deadline = minPositiveDeadline(deadline, inst.ExpireAtSec)
		deadline = minPositiveDeadline(deadline, inst.NextPeriodAtSec)
	}
	return deadline
}

func (b *BuffTable) Len() int {
	b.Init()
	return len(b.instances)
}

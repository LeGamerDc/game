package combat

import (
	"math/rand"

	"github.com/legamerdc/game/attr"
	"github.com/legamerdc/game/sched"
)

type Unit struct {
	Id                uint64
	Team              Team
	Pos               Vec2
	Life              LifeState
	ReviveAtSec       float64
	PublicTagsVersion uint32
	Attrs             attr.Table

	stats UnitStats

	rng        *rand.Rand
	attack     BasicAttackController
	abilities  AbilitySystem
	buffs      BuffTable
	death      DeathController
	pending    []Impact
	nextBuffID uint64
}

func NewUnit(team Team, pos Vec2, stats UnitStats) *Unit {
	stats = stats.withDefaults()
	u := &Unit{
		Team:  team,
		Pos:   pos,
		Life:  LifeAlive,
		Attrs: NewAttributeTable(stats),
		stats: stats,
	}
	u.attack = NewBasicAttackController(stats)
	u.abilities.Init()
	u.buffs.Init()
	return u
}

func (u *Unit) ID() uint64 { return u.Id }

func (u *Unit) AddAbility(def *AbilityDef) int {
	return u.abilities.Add(def)
}

func (u *Unit) AddPassive(trigger PassiveTrigger) {
	u.abilities.AddPassive(trigger)
}

func (u *Unit) Think(ctx *sched.ThinkCtx[*World, Signal, Effect], inbox sched.Inbox[Signal]) int64 {
	nowSec := ctx.World.NowSec()
	u.consumeSignals(inbox, nowSec)
	u.processPendingImpacts(ctx, nowSec)
	u.buffs.Think(ctx, u, nowSec)

	if u.Life == LifeDead {
		u.death.Think(ctx, u, nowSec)
		return u.nextDelay(ctx.World.Now())
	}

	u.attack.Advance(ctx, u, nowSec)
	u.abilities.Advance(ctx, u, nowSec)
	if !u.abilities.BlocksAction() {
		if !u.abilities.TryStartQueued(ctx, u, nowSec) && !u.abilities.HasQueuedReady() {
			u.attack.TryAct(ctx, u, nowSec)
		}
	}

	return u.nextDelay(ctx.World.Now())
}

func (u *Unit) Apply(ctx *sched.CommitCtx[*World, Signal], inbox sched.Inbox[Effect]) {
	nowSec := ctx.World.NowSec()
	for i := range inbox.Len() {
		e := inbox.At(i)
		switch e.K {
		case EffectDamage:
			u.applyDamage(ctx, e, nowSec)
		case EffectHeal:
			u.applyHeal(e)
		case EffectApplyBuff:
			u.buffs.Apply(ctx, u, e, nowSec)
		case EffectRemoveBuff:
			u.buffs.Remove(ctx, u, e.BuffInstanceID)
		case EffectRevive:
			u.applyRevive(ctx, nowSec)
		case EffectModifyRevive:
			u.applyModifyRevive(ctx, e)
		}
	}
	u.flushAttrs()
	ctx.WriteStage(StageUnitSummary, u.UnitSummary())
	ctx.WriteStage(StageAttrSummary, u.AttrSummary())
}

func (u *Unit) initRuntime() {
	u.stats = u.stats.withDefaults()
	if u.rng == nil {
		seed := int64(u.Id)
		if seed == 0 {
			seed = 1
		}
		u.rng = rand.New(rand.NewSource(seed))
	}
	if u.nextBuffID == 0 {
		u.nextBuffID = u.Id << 32
	}
	u.attack.Init(u.stats)
	u.abilities.Init()
	u.buffs.Init()
}

func (u *Unit) consumeSignals(inbox sched.Inbox[Signal], nowSec float64) {
	for i := range inbox.Len() {
		sig := inbox.At(i)
		switch sig.K {
		case SignalDied:
			u.death.ReviveAtSec = sig.Deadline
			u.attack.Reset()
			u.abilities.Interrupt()
		case SignalRevived:
			u.death.ReviveAtSec = 0
		case SignalInterrupt:
			u.attack.Interrupt()
			u.abilities.Interrupt()
		}
		u.abilities.HandleSignal(u, sig, nowSec)
	}
}

func (u *Unit) applyDamage(ctx *sched.CommitCtx[*World, Signal], e Effect, nowSec float64) {
	if u.Life != LifeAlive || e.RawDamage <= 0 {
		return
	}
	defense := u.Attr(DemoAttrKey_Defense)
	finalDamage := e.RawDamage - defense
	if finalDamage < 0 {
		finalDamage = 0
	}
	if finalDamage == 0 {
		return
	}

	nextHp := u.Attr(DemoAttrKey_Hp) - finalDamage
	if nextHp < 0 {
		nextHp = 0
	}
	u.SetAttrCurrent(DemoAttrKey_Hp, nextHp)

	died := false
	if nextHp <= 0 && u.Life == LifeAlive {
		u.Life = LifeDead
		u.ReviveAtSec = nowSec + u.stats.ReviveDelaySec
		died = true
	}

	ctx.Emit(u.Id, Signal{K: SignalDamageTaken, SourceRef: e.SourceRef, AbilityID: e.AbilityID, AtSec: nowSec, Amount: finalDamage})
	if sched.IsNormalRef(e.SourceRef) && e.SourceRef != u.Id {
		ctx.Emit(e.SourceRef, Signal{K: SignalDamageDealt, SourceRef: u.Id, AbilityID: e.AbilityID, AtSec: nowSec, Amount: finalDamage})
	}
	if died {
		ctx.Emit(u.Id, Signal{K: SignalDied, AtSec: nowSec, Deadline: u.ReviveAtSec})
	}
}

func (u *Unit) applyHeal(e Effect) {
	if u.Life != LifeAlive || e.Heal <= 0 {
		return
	}
	maxHp := u.Attr(DemoAttrKey_MaxHp)
	nextHp := u.Attr(DemoAttrKey_Hp) + e.Heal
	if nextHp > maxHp {
		nextHp = maxHp
	}
	u.SetAttrCurrent(DemoAttrKey_Hp, nextHp)
}

func (u *Unit) applyRevive(ctx *sched.CommitCtx[*World, Signal], nowSec float64) {
	if u.Life != LifeDead {
		return
	}
	u.Life = LifeAlive
	u.ReviveAtSec = 0
	u.SetAttrCurrent(DemoAttrKey_Hp, u.Attr(DemoAttrKey_MaxHp))
	u.SetAttrCurrent(DemoAttrKey_Mana, u.Attr(DemoAttrKey_MaxMana))
	ctx.Emit(u.Id, Signal{K: SignalRevived, AtSec: nowSec})
}

func (u *Unit) applyModifyRevive(ctx *sched.CommitCtx[*World, Signal], e Effect) {
	if u.Life != LifeDead || e.ReviveAtSec <= 0 {
		return
	}
	u.ReviveAtSec = e.ReviveAtSec
	ctx.Emit(u.Id, Signal{K: SignalReviveChanged, Deadline: e.ReviveAtSec})
}

func (u *Unit) nextDelay(nowTick int64) int64 {
	deadline := u.nextDeadline()
	if deadline <= 0 {
		return 0
	}
	return DelayUntilTick(nowTick, deadline)
}

func (u *Unit) nextDeadline() float64 {
	var deadline float64
	deadline = minPositiveDeadline(deadline, u.nextPendingDeadline())
	deadline = minPositiveDeadline(deadline, u.buffs.NextDeadline())
	if u.Life == LifeDead {
		deadline = minPositiveDeadline(deadline, u.death.NextDeadline(u))
		return deadline
	}
	deadline = minPositiveDeadline(deadline, u.attack.NextDeadline())
	deadline = minPositiveDeadline(deadline, u.abilities.NextDeadline())
	return deadline
}

package combat

import "github.com/legamerdc/game/sched"

type AbilitySlotState uint8

const (
	AbilityNotReady AbilitySlotState = iota
	AbilityReady
	AbilityPreCast
	AbilityAfterCast
)

type AbilitySlot struct {
	Index int
	Def   *AbilityDef
	State AbilitySlotState

	Queued bool

	CooldownReadyAtSec float64
	PreCastEndAtSec    float64
	AfterCastEndAtSec  float64
	TargetRef          uint64
}

type AbilitySystem struct {
	Slots      []AbilitySlot
	ReadyQueue []int
	Passives   []PassiveTrigger

	active int
}

func (a *AbilitySystem) Init() {
	if a.active == 0 && len(a.Slots) == 0 {
		a.active = -1
	}
	if a.active < -1 {
		a.active = -1
	}
}

func (a *AbilitySystem) Add(def *AbilityDef) int {
	a.Init()
	idx := len(a.Slots)
	a.Slots = append(a.Slots, AbilitySlot{
		Index: idx,
		Def:   def,
		State: AbilityNotReady,
	})
	return idx
}

func (a *AbilitySystem) AddPassive(trigger PassiveTrigger) {
	a.Passives = append(a.Passives, trigger)
}

func (a *AbilitySystem) Advance(ctx *sched.ThinkCtx[*World, Signal, Effect], u *Unit, nowSec float64) {
	a.Init()
	a.advanceActive(ctx, u, nowSec)
	a.advanceCooldowns(nowSec)
}

func (a *AbilitySystem) TryStartQueued(ctx *sched.ThinkCtx[*World, Signal, Effect], u *Unit, nowSec float64) bool {
	a.Init()
	if a.active >= 0 {
		return false
	}
	for len(a.ReadyQueue) > 0 {
		slotIdx := a.ReadyQueue[0]
		copy(a.ReadyQueue, a.ReadyQueue[1:])
		a.ReadyQueue = a.ReadyQueue[:len(a.ReadyQueue)-1]
		if slotIdx < 0 || slotIdx >= len(a.Slots) {
			continue
		}
		slot := &a.Slots[slotIdx]
		slot.Queued = false
		if slot.State != AbilityReady || slot.Def == nil {
			continue
		}
		target, ok := a.pickTarget(ctx.World, u, slot.Def)
		if !ok {
			a.failSlot(slot, nowSec)
			continue
		}
		slot.TargetRef = target.Ref
		if slot.Def.PreCastSec > 0 {
			slot.State = AbilityPreCast
			slot.PreCastEndAtSec = nowSec + slot.Def.PreCastSec
			a.active = slotIdx
			return true
		}
		a.commitSlot(ctx, u, slot, nowSec)
		return true
	}
	return false
}

func (a *AbilitySystem) HasQueuedReady() bool {
	return len(a.ReadyQueue) > 0
}

func (a *AbilitySystem) BlocksAction() bool {
	if a.active < 0 || a.active >= len(a.Slots) {
		return false
	}
	state := a.Slots[a.active].State
	return state == AbilityPreCast || state == AbilityAfterCast
}

func (a *AbilitySystem) Interrupt() {
	if a.active < 0 || a.active >= len(a.Slots) {
		return
	}
	slot := &a.Slots[a.active]
	if slot.State == AbilityPreCast {
		slot.State = AbilityNotReady
		slot.CooldownReadyAtSec = slot.PreCastEndAtSec
		slot.PreCastEndAtSec = 0
		slot.TargetRef = 0
		a.active = -1
	}
}

func (a *AbilitySystem) NextDeadline() float64 {
	var deadline float64
	for i := range a.Slots {
		slot := &a.Slots[i]
		switch slot.State {
		case AbilityNotReady:
			deadline = minPositiveDeadline(deadline, slot.CooldownReadyAtSec)
		case AbilityPreCast:
			deadline = minPositiveDeadline(deadline, slot.PreCastEndAtSec)
		case AbilityAfterCast:
			deadline = minPositiveDeadline(deadline, slot.AfterCastEndAtSec)
		}
	}
	return deadline
}

func (a *AbilitySystem) QueueSlot(slotIdx int) bool {
	if slotIdx < 0 || slotIdx >= len(a.Slots) {
		return false
	}
	slot := &a.Slots[slotIdx]
	if slot.Queued {
		return false
	}
	if slot.State == AbilityPreCast || slot.State == AbilityAfterCast {
		return false
	}
	slot.State = AbilityReady
	slot.Queued = true
	a.ReadyQueue = append(a.ReadyQueue, slotIdx)
	return true
}

func (a *AbilitySystem) advanceActive(ctx *sched.ThinkCtx[*World, Signal, Effect], u *Unit, nowSec float64) {
	if a.active < 0 || a.active >= len(a.Slots) {
		a.active = -1
		return
	}
	slot := &a.Slots[a.active]
	switch slot.State {
	case AbilityPreCast:
		if slot.PreCastEndAtSec <= nowSec {
			a.commitSlot(ctx, u, slot, slot.PreCastEndAtSec)
		}
	case AbilityAfterCast:
		if slot.AfterCastEndAtSec <= nowSec {
			slot.State = AbilityNotReady
			slot.AfterCastEndAtSec = 0
			slot.TargetRef = 0
			a.active = -1
		}
	default:
		a.active = -1
	}
}

func (a *AbilitySystem) advanceCooldowns(nowSec float64) {
	for i := range a.Slots {
		slot := &a.Slots[i]
		if slot.State == AbilityNotReady && !slot.Queued && slot.CooldownReadyAtSec <= nowSec {
			a.QueueSlot(i)
		}
	}
}

func (a *AbilitySystem) commitSlot(ctx *sched.ThinkCtx[*World, Signal, Effect], u *Unit, slot *AbilitySlot, nowSec float64) {
	def := slot.Def
	if def == nil {
		a.failSlot(slot, nowSec)
		return
	}
	slot.CooldownReadyAtSec = nowSec + def.CooldownSec
	slot.PreCastEndAtSec = 0
	if def.AfterCastSec > 0 {
		slot.State = AbilityAfterCast
		slot.AfterCastEndAtSec = nowSec + def.AfterCastSec
		a.active = slot.Index
	} else {
		slot.State = AbilityNotReady
		a.active = -1
	}

	ok := false
	if def.OnCommit != nil {
		ok = def.OnCommit(ctx, u, slot, nowSec)
	} else {
		ok = defaultAbilityCommit(ctx, u, slot, nowSec)
	}
	if !ok {
		a.failSlot(slot, nowSec)
		return
	}

	if def.AfterCastSec > 0 {
		ctx.Emit(u.Id, Signal{K: SignalSkillCommitted, SourceRef: u.Id, AbilityID: def.ID, AtSec: nowSec})
		return
	}
	slot.TargetRef = 0
	ctx.Emit(u.Id, Signal{K: SignalSkillCommitted, SourceRef: u.Id, AbilityID: def.ID, AtSec: nowSec})
}

func (a *AbilitySystem) failSlot(slot *AbilitySlot, nowSec float64) {
	if slot.Def != nil {
		slot.CooldownReadyAtSec = nowSec + slot.Def.CooldownSec
	}
	slot.State = AbilityNotReady
	slot.Queued = false
	slot.TargetRef = 0
	slot.PreCastEndAtSec = 0
	slot.AfterCastEndAtSec = 0
	if a.active == slot.Index {
		a.active = -1
	}
}

func (a *AbilitySystem) pickTarget(w *World, u *Unit, def *AbilityDef) (UnitSummary, bool) {
	targets := w.QueryEnemiesInRange(u.Id, u.Pos, u.Team, def.castRange(u))
	if len(targets) == 0 {
		return UnitSummary{}, false
	}
	return targets[u.rng.Intn(len(targets))], true
}

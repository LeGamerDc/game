package scenario

import (
	"github.com/legamerdc/game/attr"
	"github.com/legamerdc/game/demo/combat"
	"github.com/legamerdc/game/sched"
)

func AddDemoAbilities(w *combat.World) {
	firebolt := &combat.AbilityDef{
		ID:              1001,
		Name:            "Firebolt",
		CooldownSec:     2,
		PreCastSec:      0.25,
		AfterCastSec:    0.125,
		Range:           5.1,
		RawDamage:       18,
		ProjectileSpeed: 16,
	}
	for _, u := range w.Units() {
		u.AddAbility(firebolt)
	}
}

func HasteBuff() *combat.BuffDef {
	return &combat.BuffDef{
		ID:          2001,
		Name:        "Haste",
		DurationSec: 2,
		Modifiers: []attr.ModifierTemplate{
			{Attr: combat.DemoAttrKey_AttackSpeed, Op: attr.ModAdd, Value: 1},
		},
	}
}

func AddBenchmarkAbilities(w *combat.World) {
	burn := benchmarkBurnBuff()
	arcBolt := &combat.AbilityDef{
		ID:          1101,
		Name:        "ArcBolt",
		CooldownSec: 0.125,
		Range:       4.25,
		RawDamage:   0.8,
	}
	cleave := &combat.AbilityDef{
		ID:          1102,
		Name:        "CleavePulse",
		CooldownSec: 0.25,
		Range:       3.25,
		RawDamage:   0.35,
		OnCommit:    benchmarkAreaDamageCommit(4),
	}
	burnMark := &combat.AbilityDef{
		ID:          1103,
		Name:        "BurnMark",
		CooldownSec: 0.5,
		Range:       4.25,
		OnCommit:    benchmarkApplyBuffCommit(burn, 3),
	}

	for _, u := range w.Units() {
		u.AddAbility(arcBolt)
		cleaveSlot := u.AddAbility(cleave)
		u.AddAbility(burnMark)
		u.AddPassive(combat.PassiveTrigger{
			On:          combat.SignalDamageDealt,
			QueueSlot:   cleaveSlot,
			Chance:      1,
			CooldownSec: 0.25,
		})
	}
}

func benchmarkBurnBuff() *combat.BuffDef {
	return &combat.BuffDef{
		ID:           2101,
		Name:         "BenchmarkBurn",
		DurationSec:  0.75,
		PeriodSec:    0.25,
		PeriodDamage: 0.2,
		Modifiers: []attr.ModifierTemplate{
			{Attr: combat.DemoAttrKey_Defense, Op: attr.ModAdd, Value: -0.05},
		},
	}
}

func benchmarkAreaDamageCommit(maxTargets int) combat.AbilityCommitFunc {
	return func(ctx *sched.ThinkCtx[*combat.World, combat.Signal, combat.Effect], u *combat.Unit, slot *combat.AbilitySlot, _ float64) bool {
		def := slot.Def
		targets := ctx.World.QueryEnemiesInRange(u.Id, u.Pos, u.Team, def.Range)
		if len(targets) == 0 {
			return false
		}
		forEachBenchmarkTarget(targets, u.Id, maxTargets, func(target combat.UnitSummary) {
			ctx.Publish(target.Ref, combat.Effect{
				K:         combat.EffectDamage,
				SourceRef: u.Id,
				AbilityID: def.ID,
				RawDamage: def.RawDamage,
			})
		})
		return true
	}
}

func benchmarkApplyBuffCommit(buff *combat.BuffDef, maxTargets int) combat.AbilityCommitFunc {
	return func(ctx *sched.ThinkCtx[*combat.World, combat.Signal, combat.Effect], u *combat.Unit, slot *combat.AbilitySlot, _ float64) bool {
		def := slot.Def
		targets := ctx.World.QueryEnemiesInRange(u.Id, u.Pos, u.Team, def.Range)
		if len(targets) == 0 {
			return false
		}
		forEachBenchmarkTarget(targets, u.Id+uint64(def.ID), maxTargets, func(target combat.UnitSummary) {
			ctx.Publish(target.Ref, combat.Effect{
				K:         combat.EffectApplyBuff,
				SourceRef: u.Id,
				AbilityID: def.ID,
				Buff:      buff,
			})
		})
		return true
	}
}

func forEachBenchmarkTarget(targets []combat.UnitSummary, seed uint64, maxTargets int, fn func(combat.UnitSummary)) {
	if maxTargets <= 0 || len(targets) <= maxTargets {
		for _, target := range targets {
			fn(target)
		}
		return
	}
	start := int(seed % uint64(len(targets)))
	for i := 0; i < maxTargets; i++ {
		fn(targets[(start+i)%len(targets)])
	}
}

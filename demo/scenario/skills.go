package scenario

import (
	"github.com/legamerdc/game/attr"
	"github.com/legamerdc/game/demo/combat"
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

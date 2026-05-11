package combat

import "github.com/legamerdc/game/attr"

func NewAttributeTable(stats UnitStats) attr.Table {
	stats = stats.withDefaults()

	var table attr.Table
	table.Init()
	table.Put(&DemoAttributeSet{
		Hp:          attr.Value{Base: stats.Hp, Current: stats.Hp},
		Mana:        attr.Value{Base: stats.Mana, Current: stats.Mana},
		MaxHp:       attr.Value{Base: stats.Hp, Current: stats.Hp},
		MaxMana:     attr.Value{Base: stats.Mana, Current: stats.Mana},
		Attack:      attr.Value{Base: stats.Attack, Current: stats.Attack},
		Defense:     attr.Value{Base: stats.Defense, Current: stats.Defense},
		AttackRange: attr.Value{Base: stats.AttackRange, Current: stats.AttackRange},
		AttackSpeed: attr.Value{Base: stats.AttackSpeed, Current: stats.AttackSpeed},
	})
	return table
}

func (u *Unit) Attr(key attr.Key) float64 {
	v, ok := u.Attrs.GetCurrent(key)
	if !ok {
		return 0
	}
	return v
}

func (u *Unit) SetAttrCurrent(key attr.Key, value float64) {
	u.Attrs.SetBase(key, value)
	u.Attrs.Flush()
}

func (u *Unit) flushAttrs() {
	u.Attrs.Flush()
	maxHp := u.Attr(DemoAttrKey_MaxHp)
	if hp := u.Attr(DemoAttrKey_Hp); hp > maxHp {
		u.SetAttrCurrent(DemoAttrKey_Hp, maxHp)
	}
	maxMana := u.Attr(DemoAttrKey_MaxMana)
	if mana := u.Attr(DemoAttrKey_Mana); mana > maxMana {
		u.SetAttrCurrent(DemoAttrKey_Mana, maxMana)
	}
}

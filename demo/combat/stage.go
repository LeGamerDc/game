package combat

import "github.com/legamerdc/game/sched"

const (
	StageUnitSummary sched.StageKind = iota + 1
	StageAttrSummary
)

type UnitSummary struct {
	Ref               uint64
	Team              Team
	Pos               Vec2
	Alive             bool
	Targetable        bool
	Hp, MaxHp         float64
	PublicTagsVersion uint32
}

type AttrSummary struct {
	Attack, Defense          float64
	AttackRange, AttackSpeed float64
	MaxHp, MaxMana           float64
}

func (u *Unit) UnitSummary() UnitSummary {
	return UnitSummary{
		Ref:               u.Id,
		Team:              u.Team,
		Pos:               u.Pos,
		Alive:             u.Life == LifeAlive,
		Targetable:        u.Life == LifeAlive,
		Hp:                u.Attr(DemoAttrKey_Hp),
		MaxHp:             u.Attr(DemoAttrKey_MaxHp),
		PublicTagsVersion: u.PublicTagsVersion,
	}
}

func (u *Unit) AttrSummary() AttrSummary {
	return AttrSummary{
		Attack:      u.Attr(DemoAttrKey_Attack),
		Defense:     u.Attr(DemoAttrKey_Defense),
		AttackRange: u.Attr(DemoAttrKey_AttackRange),
		AttackSpeed: u.Attr(DemoAttrKey_AttackSpeed),
		MaxHp:       u.Attr(DemoAttrKey_MaxHp),
		MaxMana:     u.Attr(DemoAttrKey_MaxMana),
	}
}

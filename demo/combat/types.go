package combat

import "math"

type Vec2 struct {
	X, Y float64
}

func (v Vec2) DistanceSquared(o Vec2) float64 {
	dx := v.X - o.X
	dy := v.Y - o.Y
	return dx*dx + dy*dy
}

func (v Vec2) Distance(o Vec2) float64 {
	return math.Sqrt(v.DistanceSquared(o))
}

type Team int32

type LifeState uint8

const (
	LifeAlive LifeState = iota
	LifeDead
)

type UnitStats struct {
	Hp, Mana        float64
	Attack, Defense float64
	AttackRange     float64
	AttackSpeed     float64
	ProjectileSpeed float64
	AttackWindupSec float64
	TargetHoldSec   float64
	TargetSearchSec float64
	ReviveDelaySec  float64
}

func (s UnitStats) withDefaults() UnitStats {
	if s.Hp <= 0 {
		s.Hp = 100
	}
	if s.Mana <= 0 {
		s.Mana = 100
	}
	if s.Attack <= 0 {
		s.Attack = 12
	}
	if s.AttackRange <= 0 {
		s.AttackRange = 5.1
	}
	if s.AttackSpeed <= 0 {
		s.AttackSpeed = 1
	}
	if s.ProjectileSpeed <= 0 {
		s.ProjectileSpeed = 12
	}
	if s.AttackWindupSec <= 0 {
		s.AttackWindupSec = 0.25
	}
	if s.TargetHoldSec <= 0 {
		s.TargetHoldSec = 5
	}
	if s.TargetSearchSec <= 0 {
		s.TargetSearchSec = 0.5
	}
	if s.ReviveDelaySec <= 0 {
		s.ReviveDelaySec = 8
	}
	return s
}

package demo

import (
	"github.com/legamerdc/game/attr"
	"github.com/legamerdc/game/lib"
	"github.com/legamerdc/game/sched"
	"github.com/legamerdc/game/tag"
)

var (
	_ sched.Logic[*World, *Signal, *Effect] = (*Unit)(nil)
)

type (
	Vector struct {
		X, Y float64
	}

	Unit struct {
		// public
		Id  uint64
		ASC attr.Table
		Tag tag.Tag
		Pos Vector

		// private
		Abilities lib.HeapArrayMap[uint32, int64, *Ability]
		Runnings  lib.HeapArrayMap[uint32, int64, *Running]
	}

	UnitPublic struct {
		ASC *attr.Table
		Tag *tag.Tag
	}

	Ability struct {
		spec *AbilitySpec
	}

	AbilitySpec struct{}

	Running struct {
		spec *RunningSpec
	}

	RunningSpec struct{}
)

func (g *Unit) Public() *UnitPublic {
	return &UnitPublic{
		ASC: &g.ASC,
		Tag: &g.Tag,
	}
}

func (g *Unit) ID() uint64 {
	return g.Id
}

func (g *Unit) Think(ctx *sched.ThinkCtx[*World, *Signal, *Effect], i sched.Inbox[*Signal]) int64 {
	return 0
}

func (g *Unit) Apply(ctx *sched.CommitCtx[*World, *Signal], i sched.Inbox[*Effect]) {

}

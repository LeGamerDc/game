package demo

import (
	"github.com/legamerdc/game/attr"
	"github.com/legamerdc/game/lib"
	"github.com/legamerdc/game/sched"
	"github.com/legamerdc/game/tag"
)

var (
	_ sched.Logic[*World, *Signal, *Effect] = (*GAS)(nil)
)

type (
	GAS struct {
		// public
		Id  uint64
		ASC attr.Table
		Tag tag.Tag

		// private
		Abilities lib.HeapArrayMap[uint32, int64, *Ability]
		Runnings  lib.HeapArrayMap[uint32, int64, *Running]
	}

	GASPublic struct {
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

func (g *GAS) Public() *GASPublic {
	return &GASPublic{
		ASC: &g.ASC,
		Tag: &g.Tag,
	}
}

func (g *GAS) ID() uint64 {
	return g.Id
}

func (g *GAS) Think(ctx *sched.ThinkCtx[*World, *Signal, *Effect], i sched.Inbox[*Signal]) int64 {
	return 0
}

func (g *GAS) Apply(ctx *sched.CommitCtx[*World, *Signal], i sched.Inbox[*Effect]) {

}

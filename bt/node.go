package bt

import "github.com/legamerdc/game/blackboard"

type (
	NodeType int32

	// EI 事件抽象
	EI interface {
		Kind() int32
	}

	Ctx interface {
		Now() int64
		Get(string) (blackboard.Field, bool)
		Set(string, blackboard.Field)
	}
)

const (
	Invalid NodeType = iota

	AlwaysSuccess
	AlwaysFail
	RepeatN
	UntilSuccess
	UntilFail
	AlwaysCheckGuard
)

type Node[C Ctx, E EI] struct {
	Type NodeType
}

func (n *Node[C, E]) Generate() {

}

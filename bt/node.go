package bt

import (
	"errors"
	"fmt"

	"github.com/legamerdc/game/blackboard"
)

type (
	NodeType int32

	CountMode int32

	TaskStatus int32

	// EI 事件抽象
	EI interface {
		Kind() int32
	}

	Ctx interface {
		Now() int64
		Get(string) (blackboard.Field, bool)
		Set(string, blackboard.Field)
		Del(string)
		Exec(string) (blackboard.Field, error)
	}
)

const (
	Invalid NodeType = iota

	TypeRevise
	TypeRepeat
	TypePostGuard
	TypeAlwaysGuard
	TypeGuard
	TypeTask

	TypeSequenceBranch
	TypeStochasticBranch
	TypeJoinBranch
)

const (
	MatchNone    CountMode = 0
	MatchSuccess           = 1
	MatchFail              = 2
	MatchAll               = MatchSuccess | MatchFail
)

// >= 1  : Running
// <= -2 : Fail
const (
	TaskRunning TaskStatus = 1
	TaskNew                = 0
	TaskSuccess            = -1
	TaskFail               = -2
)

var (
	errWrongChildCount = errors.New("wrong child count")
	fmtBadParam        = "bad param: %s"
)

type (
	TaskI[C Ctx, E EI] interface {
		// Execute Task执行逻辑，from标记Task在模拟栈中的弹出时机。
		// 首次运行时 from=TaskNew
		// 当子节点运行结束再次弹出Task时，from=TaskSuccess/TaskFail
		// 当Task本身是叶节点（包含重建栈的节点）时，当Task处于Running状态被反复执行时，from=TaskRunning
		Execute(c C, stk *TaskI[C, E], from TaskStatus) TaskStatus
		Parent() TaskI[C, E]
		SetParent(TaskI[C, E])
		OnComplete(cancel bool)
	}

	// EventTask 只有叶子节点（包括重建栈的节点）且可能处于TaskRunning状态才需要实现 OnEvent
	// 用于实时响应事件完成任务。
	EventTask[C Ctx, E EI] interface {
		// OnEvent 实时响应事件，如果响应后任务完成，则返回 TaskSuccess/TaskFail
		OnEvent(C, E) TaskStatus
	}

	// LeafTaskI 用户接入叶节点时需要实现的接口
	LeafTaskI[C Ctx, E EI] interface {
		// Execute 执行任务逻辑
		Execute(c C) TaskStatus
		// OnComplete 任务执行完毕后会调用OnComplete，用户可以在这里回收资源。
		// cancel 标记该任务是被取消的还是正常结束的。
		OnComplete(cancel bool)
		// OnEvent 事件驱动接口，用于处理外部事件。
		OnEvent(C, E) TaskStatus
	}

	TaskCreator[C Ctx, E EI] func(C) LeafTaskI[C, E]

	Guard[C Ctx] func(C) (blackboard.Field, error)

	Node[C Ctx, E EI] struct {
		Type      NodeType
		Children  []Node[C, E]
		MaxLoop   int32
		Require   int32
		CountMode CountMode

		Guard  func(C) (blackboard.Field, error)
		Task   TaskCreator[C, E]
		Revise func(TaskStatus) TaskStatus
	}
)

func (c CountMode) Count(success bool) bool {
	switch c {
	case MatchSuccess:
		return success
	case MatchFail:
		return !success
	case MatchAll:
		return true
	default:
		return false
	}
}
func (c CountMode) Require(complete, success int32) int32 {
	switch c {
	case MatchSuccess:
		return success
	case MatchFail:
		return complete - success
	case MatchAll:
		return complete
	default:
		return 0
	}
}

func (n *Node[C, E]) Check() error {
	switch n.Type {
	case TypeRevise:
		if len(n.Children) != 1 {
			return errWrongChildCount
		}
		if n.Revise == nil {
			return fmt.Errorf(fmtBadParam, "revise")
		}
	case TypeRepeat, TypePostGuard, TypeAlwaysGuard:
		if len(n.Children) != 1 {
			return errWrongChildCount
		}
	case TypeGuard, TypeTask:
		if len(n.Children) != 0 {
			return errWrongChildCount
		}
	case TypeSequenceBranch, TypeStochasticBranch, TypeJoinBranch:
		if len(n.Children) == 0 {
			return errWrongChildCount
		}
	default:
		return errors.New("unknown node type")
	}
	return nil
}

func (n *Node[C, E]) Generate(c C) TaskI[C, E] {
	switch n.Type {
	case TypeRevise:
		return &revise[C, E]{n: n}
	case TypeRepeat:
		return &repeat[C, E]{n: n}
	case TypePostGuard:
		return &postGuard[C, E]{n: n}
	case TypeAlwaysGuard:
		return &alwaysCheckGuard[C, E]{n: n}
	case TypeGuard:
		return &guard[C, E]{n: n}
	case TypeTask:
		return &task[C, E]{n: n}
	case TypeSequenceBranch:
		return &sequenceBranch[C, E]{n: n}
	case TypeStochasticBranch:
		return &stochasticBranch[C, E]{n: n}
	case TypeJoinBranch:
		return &joinBranch[C, E]{n: n}
	default:
		panic("unreachable")
	}
}

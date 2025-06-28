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
	TaskNew     TaskStatus = 0
	TaskSuccess TaskStatus = -1
	TaskFail    TaskStatus = -2
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
		OnComplete(c C, cancel bool)
	}

	// EventTask 只有叶子节点（包括重建栈的节点）且可能处于TaskRunning状态才需要实现 OnEvent
	// 用于实时响应事件完成任务。
	EventTask[C Ctx, E EI] interface {
		// OnEvent 实时响应事件，如果响应后任务完成，则返回 TaskSuccess/TaskFail，如果无法处理事件返回 TaskNew，
		// 如果事件处理后任务仍然处于Running状态，则返回一个正数表示预估bt应该在s后再次Update。
		OnEvent(C, E) TaskStatus
	}

	// LeafTaskI 用户接入叶节点时需要实现的接口
	LeafTaskI[C Ctx, E EI] interface {
		// Execute 执行任务逻辑
		Execute(c C) TaskStatus
		// OnComplete 任务执行完毕后会调用OnComplete，用户可以在这里回收资源。
		// cancel 标记该任务是被取消的还是正常结束的。
		OnComplete(c C, cancel bool)
		// OnEvent 事件驱动接口，用于处理外部事件。返回TaskNew表示无法处理这个信号，返回s>0的值表示任务仍然Running并预估bt应该在s后再次Update。
		OnEvent(C, E) TaskStatus
	}

	// TaskCreator 创建叶节点的函数抽象，如果创建失败返回 false，该节点会立即失败
	TaskCreator[C Ctx, E EI] func(C) (LeafTaskI[C, E], bool)

	// Guard 节点前置检查，如果检查失败节点不会执行，而是直接返回失败，如果检查报错也当做检查失败。
	Guard[C Ctx] func(C) bool

	Node[C Ctx, E EI] struct {
		Type      NodeType
		Children  []*Node[C, E]
		MaxLoop   int32
		Require   int32
		CountMode CountMode

		Guard  Guard[C]
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

func NewSuccess[C Ctx, E EI](g Guard[C], ch *Node[C, E]) *Node[C, E] {
	_assert(ch != nil)
	return &Node[C, E]{
		Type:     TypeRevise,
		Children: []*Node[C, E]{ch},
		Guard:    g,
		Revise:   _success,
	}
}

func NewFail[C Ctx, E EI](g Guard[C], ch *Node[C, E]) *Node[C, E] {
	_assert(ch != nil)
	return &Node[C, E]{
		Type:     TypeRevise,
		Children: []*Node[C, E]{ch},
		Guard:    g,
		Revise:   _fail,
	}
}

func NewInverter[C Ctx, E EI](g Guard[C], ch *Node[C, E]) *Node[C, E] {
	_assert(ch != nil)
	return &Node[C, E]{
		Type:     TypeRevise,
		Children: []*Node[C, E]{ch},
		Guard:    g,
		Revise:   _invert,
	}
}

// NewRepeatUntilNSuccess 持续运行子树，直到完成require次Success，若maxLoop后未完成则返回Fail
func NewRepeatUntilNSuccess[C Ctx, E EI](g Guard[C], require, maxLoop int32, ch *Node[C, E]) *Node[C, E] {
	_assert(ch != nil)
	return &Node[C, E]{
		Type:      TypeRepeat,
		Children:  []*Node[C, E]{ch},
		MaxLoop:   maxLoop,
		Require:   require,
		CountMode: MatchSuccess,
		Guard:     g,
	}
}

// NewPostGuard 运行子树后再check guard，并用guard的返回值充当结果
func NewPostGuard[C Ctx, E EI](g Guard[C], ch *Node[C, E]) *Node[C, E] {
	_assert(ch != nil)
	return &Node[C, E]{
		Type:     TypePostGuard,
		Children: []*Node[C, E]{ch},
		Guard:    g,
	}
}

// NewAlwaysGuard 行为树每次Update时都检查guard，若检测失败Cancel子树后返回
func NewAlwaysGuard[C Ctx, E EI](g Guard[C], ch *Node[C, E]) *Node[C, E] {
	_assert(ch != nil)
	return &Node[C, E]{
		Type:     TypeAlwaysGuard,
		Children: []*Node[C, E]{ch},
		Guard:    g,
	}
}

// NewGuard 运行用户指定逻辑的guard
func NewGuard[C Ctx, E EI](g Guard[C]) *Node[C, E] {
	return &Node[C, E]{
		Type:  TypeGuard,
		Guard: g,
	}
}

// NewTask 运行用户指定逻辑的leaf task
func NewTask[C Ctx, E EI](g Guard[C], task TaskCreator[C, E]) *Node[C, E] {
	_assert(task != nil)
	return &Node[C, E]{
		Type:  TypeTask,
		Guard: g,
		Task:  task,
	}
}

// NewSelector 遍历执行子树，发现成功提前退出并成功，全部失败算失败
func NewSelector[C Ctx, E EI](g Guard[C], shuffle bool, ch ...*Node[C, E]) *Node[C, E] {
	_assert(ch != nil)
	_assert(len(ch) > 0)
	for _, c := range ch {
		_assert(c != nil)
	}
	t := TypeSequenceBranch
	if shuffle {
		t = TypeStochasticBranch
	}
	return &Node[C, E]{
		Type:      t,
		Children:  ch,
		Require:   1,
		CountMode: MatchSuccess,
		Guard:     g,
		Revise:    _direct,
	}
}

// NewSelectorN 遍历执行子树，直到发现N个成功提前退出并成功，执行完毕则失败
func NewSelectorN[C Ctx, E EI](g Guard[C], n int32, shuffle bool, ch ...*Node[C, E]) *Node[C, E] {
	_assert(ch != nil)
	_assert(len(ch) > 0)
	_assert(n > 0 && n <= int32(len(ch)))
	for _, c := range ch {
		_assert(c != nil)
	}
	t := TypeSequenceBranch
	if shuffle {
		t = TypeStochasticBranch
	}
	return &Node[C, E]{
		Type:      t,
		Children:  ch,
		Require:   n,
		CountMode: MatchSuccess,
		Guard:     g,
		Revise:    _direct,
	}
}

// NewSequence 遍历执行子树，全部成功算成功，发现失败提前退出并失败
func NewSequence[C Ctx, E EI](g Guard[C], shuffle bool, ch ...*Node[C, E]) *Node[C, E] {
	_assert(ch != nil)
	_assert(len(ch) > 0)
	for _, c := range ch {
		_assert(c != nil)
	}
	t := TypeSequenceBranch
	if shuffle {
		t = TypeStochasticBranch
	}
	return &Node[C, E]{
		Type:      t,
		Children:  ch,
		Require:   1,
		CountMode: MatchFail,
		Guard:     g,
		Revise:    _invert,
	}
}

// NewParallel 同时执行所有子树，直到已经完成的子树状态按照mode满足require并提前结束Cancel剩余未结束子树返回成功，否则返回失败。
func NewParallel[C Ctx, E EI](g Guard[C], mode CountMode, require int32, ch ...*Node[C, E]) *Node[C, E] {
	_assert(ch != nil)
	_assert(len(ch) > 0)
	_assert(require > 0 && require <= int32(len(ch)))
	for _, c := range ch {
		_assert(c != nil)
	}
	return &Node[C, E]{
		Type:      TypeJoinBranch,
		Children:  ch,
		Require:   require,
		CountMode: mode,
		Guard:     g,
	}
}

// Check 用户自设参数时，调用Check检查参数是否合理
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
	case TypeSequenceBranch, TypeStochasticBranch:
		if len(n.Children) == 0 {
			return errWrongChildCount
		}
		if n.Revise == nil {
			return fmt.Errorf(fmtBadParam, "revise")
		}
	case TypeJoinBranch:
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
		return &alwaysGuard[C, E]{n: n}
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

func _assert(x bool) {
	if !x {
		panic("assertion")
	}
}

func _invert(x TaskStatus) TaskStatus {
	if x == TaskSuccess {
		return TaskFail
	}
	return TaskSuccess
}

func _success(_ TaskStatus) TaskStatus {
	return TaskSuccess
}

func _fail(_ TaskStatus) TaskStatus {
	return TaskFail
}

func _direct(x TaskStatus) TaskStatus {
	return x
}

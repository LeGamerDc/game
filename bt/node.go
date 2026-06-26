package bt

import (
	"errors"
	"fmt"
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
	}

	// Rand is the randomness used by the stochastic branches. It is satisfied
	// by *math/rand/v2.Rand. Inject it explicitly (instead of relying on a
	// global source) so stochastic order is reproducible for replay/lockstep.
	//
	// NOTE: the Rand is stored on the (potentially shared) Node. If one Node
	// tree is shared by many owners ticked concurrently, supply a Rand that is
	// safe for that sharing model, or build a per-owner tree with a per-owner
	// Rand. A *math/rand/v2.Rand is NOT safe for concurrent use.
	Rand interface {
		Shuffle(n int, swap func(i, j int))
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

	// Reactive composites re-evaluate their children from the first one on
	// every update, so a higher-priority child whose condition becomes true
	// can preempt a running lower-priority child.
	TypeReactiveSelector
	TypeReactiveSequence
)

const (
	MatchNone    CountMode = 0
	MatchSuccess           = 1
	MatchFail              = 2
	MatchAll               = MatchSuccess | MatchFail
)

// TaskStatus uses a compact encoding shared by node execution and event dispatch:
//   - >0: running, with the value as a relative delay hint before the next update.
//   - 0: internal "new child was pushed" marker, or "event was not handled".
//   - -1: success.
//   - <=-2: failure.
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
		// Execute 执行任务逻辑。必须返回 Running(>0) / TaskSuccess / TaskFail 之一，
		// 不允许返回 TaskNew(0)——它在引擎里表示「刚 push 了子节点」。叶节点若误返回
		// TaskNew，框架会防御性地当作 TaskFail 处理（见 task.Execute）。
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
		Require   int32 // sequence/selector/repeat threshold; parallel success threshold
		CountMode CountMode

		// Parallel (TypeJoinBranch) only:
		FailRequire int32 // number of failed children that makes the parallel fail
		FailFast    bool  // fail as soon as reaching Require successes is impossible

		Guard  Guard[C]
		Task   TaskCreator[C, E]
		Revise func(TaskStatus) TaskStatus
		Rand   Rand // stochastic branches only
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
	_assert(require > 0)
	_assert(maxLoop > 0)
	_assert(require <= maxLoop)
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

// newBranch 是顺序/随机分支节点的公共构造逻辑。
func newBranch[C Ctx, E EI](t NodeType, g Guard[C], require int32, mode CountMode, revise func(TaskStatus) TaskStatus, rng Rand, ch []*Node[C, E]) *Node[C, E] {
	_assert(len(ch) > 0)
	for _, c := range ch {
		_assert(c != nil)
	}
	return &Node[C, E]{
		Type:      t,
		Children:  ch,
		Require:   require,
		CountMode: mode,
		Guard:     g,
		Revise:    revise,
		Rand:      rng,
	}
}

// NewSelector 顺序遍历子树，发现一个成功就提前成功，全部失败算失败。
func NewSelector[C Ctx, E EI](g Guard[C], ch ...*Node[C, E]) *Node[C, E] {
	return newBranch(TypeSequenceBranch, g, 1, MatchSuccess, _direct, nil, ch)
}

// NewSelectorN 顺序遍历子树，累计 n 个成功就提前成功，遍历完仍不足则失败。
func NewSelectorN[C Ctx, E EI](g Guard[C], n int32, ch ...*Node[C, E]) *Node[C, E] {
	_assert(n > 0 && n <= int32(len(ch)))
	return newBranch(TypeSequenceBranch, g, n, MatchSuccess, _direct, nil, ch)
}

// NewSequence 顺序遍历子树，全部成功算成功，发现失败提前退出并失败。
func NewSequence[C Ctx, E EI](g Guard[C], ch ...*Node[C, E]) *Node[C, E] {
	return newBranch(TypeSequenceBranch, g, 1, MatchFail, _invert, nil, ch)
}

// NewStochasticSelector 与 NewSelector 相同，但首次访问前用注入的 rng 打乱子节点顺序。
func NewStochasticSelector[C Ctx, E EI](g Guard[C], rng Rand, ch ...*Node[C, E]) *Node[C, E] {
	_assert(rng != nil)
	return newBranch(TypeStochasticBranch, g, 1, MatchSuccess, _direct, rng, ch)
}

// NewStochasticSelectorN 与 NewSelectorN 相同，但首次访问前用注入的 rng 打乱子节点顺序。
func NewStochasticSelectorN[C Ctx, E EI](g Guard[C], n int32, rng Rand, ch ...*Node[C, E]) *Node[C, E] {
	_assert(rng != nil)
	_assert(n > 0 && n <= int32(len(ch)))
	return newBranch(TypeStochasticBranch, g, n, MatchSuccess, _direct, rng, ch)
}

// NewStochasticSequence 与 NewSequence 相同，但首次访问前用注入的 rng 打乱子节点顺序。
func NewStochasticSequence[C Ctx, E EI](g Guard[C], rng Rand, ch ...*Node[C, E]) *Node[C, E] {
	_assert(rng != nil)
	return newBranch(TypeStochasticBranch, g, 1, MatchFail, _invert, rng, ch)
}

// NewReactiveSelector 反应式选择器：子节点是一组「互斥备选方案」，按从左到右的优先级排列。
// 每次 update 都从第一个子节点重新评估：任一子节点成功则整体成功、全部失败则失败；当某个更高
// 优先级（更靠前）的子节点条件重新成立、变为成功/运行时，会抢占并 Cancel 正在运行的较低优先级子节点。
//
// 主要用途：按优先级切换行为（如「有威胁→逃跑；否则有敌→攻击；否则巡逻」）。抢占条件挂在高优先级
// 子节点自己的 guard 上（对应 Unreal BT 的 LowerPriority observer-abort），被抢占的低优先级节点
// 无需知道谁会抢占它，只需能被干净 Cancel。
//
// 约束：除当前运行的子节点外，靠前的子节点必须是「同步条件」（立即返回成功/失败的 Guard 或纯条件
// 子树）。若把多 tick 的动作放在靠前位置，它每轮会被重启并抢占后面的子节点，导致活锁。已完成的子节点
// 每个 tick 会被重新生成并重跑，因此条件子节点必须幂等、无副作用。注意：抢占在每次 update（Execute）
// 以及事件到来（OnEvent）时都会评估；离散调度下，靠前条件的变化必须能触发 update 或伴随事件，否则
// 要等到当前运行子节点的下次定时唤醒才会被发现。
func NewReactiveSelector[C Ctx, E EI](g Guard[C], ch ...*Node[C, E]) *Node[C, E] {
	_assert(len(ch) > 0)
	for _, c := range ch {
		_assert(c != nil)
	}
	return &Node[C, E]{
		Type:     TypeReactiveSelector,
		Children: ch,
		Guard:    g,
	}
}

// NewReactiveSequence 反应式序列器：子节点是一串「前置条件 + 动作」，全部成功才成功、任一失败则失败。
// 每次 update（及事件到来 OnEvent 时）都从第一个子节点重新评估，靠前的前置条件都会被重新检查；某个
// 前置条件不再成立时会立即 Cancel 正在运行的后续动作并返回失败。
//
// 主要用途：受持续前置条件守护的动作（如「在攻击范围内 && 有蓝 → 施法」，任一前置在施法途中失效就打断）。
// 典型形状是「N 个同步条件子节点 + 1 个末尾动作」。
//
// 与 AlwaysGuard 的关系：用单个条件守护单个动作时，二者几乎等价（都对应 Unreal BT 的 Self-abort），
// 此时 AlwaysGuard 更直接；ReactiveSequence 的额外价值是把多个前置条件表达为独立、可复用的 Guard 节点。
//
// 约束同 NewReactiveSelector：除末尾动作外，靠前子节点必须是同步条件，且幂等、无副作用。
func NewReactiveSequence[C Ctx, E EI](g Guard[C], ch ...*Node[C, E]) *Node[C, E] {
	_assert(len(ch) > 0)
	for _, c := range ch {
		_assert(c != nil)
	}
	return &Node[C, E]{
		Type:     TypeReactiveSequence,
		Children: ch,
		Guard:    g,
	}
}

// NewParallel 同时运行所有子树：
//   - 成功子树数达到 successRequire：立即成功，并 Cancel 仍在运行的子树；
//   - failRequire > 0 且失败子树数达到 failRequire：立即失败，并 Cancel 仍在运行的子树；
//     failRequire == 0 表示不设独立失败阈值（常见场景下只关心成功阈值即可）；
//   - failFast=true 时，一旦「已成功 + 仍在运行 < successRequire」（再也无法凑齐
//     successRequire 个成功）立即失败；
//   - 全部完成仍未达到 successRequire：失败。
//
// 常用组合（N 个子节点）：AND=successRequire N, failRequire 0, failFast true；
// OR=successRequire 1, failRequire 0, failFast false；需要独立失败阈值时才传 failRequire>0。
func NewParallel[C Ctx, E EI](g Guard[C], successRequire, failRequire int32, failFast bool, ch ...*Node[C, E]) *Node[C, E] {
	_assert(len(ch) > 0)
	_assert(successRequire > 0 && successRequire <= int32(len(ch)))
	_assert(failRequire >= 0 && failRequire <= int32(len(ch)))
	for _, c := range ch {
		_assert(c != nil)
	}
	return &Node[C, E]{
		Type:        TypeJoinBranch,
		Children:    ch,
		Require:     successRequire,
		FailRequire: failRequire,
		FailFast:    failFast,
		Guard:       g,
	}
}

// Check 用户自设参数时，调用Check检查当前节点参数是否合理。
// 它只检查当前节点，不递归检查子树；整棵树需要逐节点检查或未来单独的 tree validate API。
func (n *Node[C, E]) Check() error {
	switch n.Type {
	case TypeRevise:
		if len(n.Children) != 1 {
			return errWrongChildCount
		}
		if n.Revise == nil {
			return fmt.Errorf(fmtBadParam, "revise")
		}
	case TypeRepeat:
		if len(n.Children) != 1 {
			return errWrongChildCount
		}
		if n.Require <= 0 {
			return fmt.Errorf(fmtBadParam, "require")
		}
		if n.MaxLoop <= 0 {
			return fmt.Errorf(fmtBadParam, "maxLoop")
		}
		if n.Require > n.MaxLoop {
			return fmt.Errorf(fmtBadParam, "require")
		}
	case TypePostGuard, TypeAlwaysGuard:
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
		if n.Type == TypeStochasticBranch && n.Rand == nil {
			return fmt.Errorf(fmtBadParam, "rand")
		}
	case TypeReactiveSelector, TypeReactiveSequence:
		if len(n.Children) == 0 {
			return errWrongChildCount
		}
	case TypeJoinBranch:
		if len(n.Children) == 0 {
			return errWrongChildCount
		}
		l := int32(len(n.Children))
		if n.Require <= 0 || n.Require > l {
			return fmt.Errorf(fmtBadParam, "successRequire")
		}
		if n.FailRequire < 0 || n.FailRequire > l { // 0 = disabled
			return fmt.Errorf(fmtBadParam, "failRequire")
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
	case TypeReactiveSelector:
		return &reactiveBranch[C, E]{n: n, sequence: false}
	case TypeReactiveSequence:
		return &reactiveBranch[C, E]{n: n, sequence: true}
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

package bt

// sequenceBranch 遍历执行子树，直到 Require 次成功，如果遍历完所有子树都没满足则返回失败。
type sequenceBranch[C Ctx, E EI] struct {
	n          *Node[C, E]
	parent     TaskI[C, E]
	idx, count int32
}

func (x *sequenceBranch[C, E]) SetParent(parent TaskI[C, E]) {
	x.parent = parent
}

func (x *sequenceBranch[C, E]) Parent() TaskI[C, E] {
	return x.parent
}

func (x *sequenceBranch[C, E]) OnComplete(C, bool) {}

func (x *sequenceBranch[C, E]) Execute(c C, stk *TaskI[C, E], from TaskStatus) TaskStatus {
	if from == TaskNew {
		if s := checkGuard(x.n, c); s != TaskSuccess {
			return s
		}
		push(stk, x.n.Children[0].Generate(c))
		return TaskNew
	}
	x.idx++
	if x.n.CountMode.Count(from == TaskSuccess) {
		x.count++
	}
	if x.n.Require > 0 && x.count >= x.n.Require {
		return x.n.Revise(TaskSuccess)
	}
	if int(x.idx) >= len(x.n.Children) {
		return x.n.Revise(TaskFail)
	}
	push(stk, x.n.Children[x.idx].Generate(c))
	return TaskNew
}

// stochasticBranch 类似于sequenceBranch，但是在首次执行前打乱子节点，以达到随机遍历的效果。
type stochasticBranch[C Ctx, E EI] struct {
	n          *Node[C, E]
	parent     TaskI[C, E]
	idx, count int32
	order      []int32
}

func (x *stochasticBranch[C, E]) SetParent(parent TaskI[C, E]) {
	x.parent = parent
}

func (x *stochasticBranch[C, E]) Parent() TaskI[C, E] {
	return x.parent
}

func (x *stochasticBranch[C, E]) OnComplete(C, bool) {}

func (x *stochasticBranch[C, E]) Execute(c C, stk *TaskI[C, E], from TaskStatus) TaskStatus {
	if from == TaskNew {
		if s := checkGuard(x.n, c); s != TaskSuccess {
			return s
		}
		x.order = shuffleOrder(x.n.Rand, len(x.n.Children))
		push(stk, x.n.Children[x.order[0]].Generate(c))
		return TaskNew
	}
	x.idx++
	if x.n.CountMode.Count(from == TaskSuccess) {
		x.count++
	}
	if x.n.Require > 0 && x.count >= x.n.Require {
		return x.n.Revise(TaskSuccess)
	}
	if int(x.idx) >= len(x.n.Children) {
		return x.n.Revise(TaskFail)
	}
	push(stk, x.n.Children[x.order[x.idx]].Generate(c))
	return TaskNew
}

// joinBranch 所有的子树同时运行，直到满足Require或全部执行完成。
// 满足 Require 后剩下的仍然在Running 中的子树会被Cancel
type joinBranch[C Ctx, E EI] struct {
	n                 *Node[C, E]
	parent            TaskI[C, E]
	roots             []Root[C, E]
	tasks             []TaskStatus
	success, complete int32
}

func (x *joinBranch[C, E]) SetParent(parent TaskI[C, E]) {
	x.parent = parent
}

func (x *joinBranch[C, E]) Parent() TaskI[C, E] {
	return x.parent
}

func (x *joinBranch[C, E]) OnComplete(c C, cancel bool) {
	for i := range x.roots {
		if x.tasks[i] >= TaskRunning {
			x.roots[i].Cancel(c)
		}
	}
}

func (x *joinBranch[C, E]) OnEvent(c C, e E) TaskStatus {
	consumed := false
	for i := range x.roots {
		if x.tasks[i] >= TaskRunning {
			st := x.roots[i].OnEvent(c, e)
			if st == TaskNew {
				continue
			}
			consumed = true
			x.tasks[i] = st
			if st < TaskNew { // completed (success/fail)
				x.complete++
				if st == TaskSuccess {
					x.success++
				}
			}
		}
	}
	if !consumed {
		return TaskNew
	}
	return x.settle(x.runningNext())
}

func (x *joinBranch[C, E]) Execute(c C, stk *TaskI[C, E], from TaskStatus) TaskStatus {
	if from == TaskNew {
		if s := checkGuard(x.n, c); s != TaskSuccess {
			return s
		}
		l := len(x.n.Children)
		x.roots = make([]Root[C, E], l)
		x.tasks = make([]TaskStatus, l)
		for i := range l {
			x.roots[i].SetNode(x.n.Children[i])
		}
	}
	for i := range x.roots {
		if x.tasks[i] >= TaskNew { // New or Running
			x.tasks[i] = x.roots[i].Execute(c)
			if x.tasks[i] < TaskNew { // completed
				x.complete++
				if x.tasks[i] == TaskSuccess {
					x.success++
				}
			}
		}
	}
	return x.settle(x.runningNext())
}

// runningNext returns the minimum wake hint over still-running children, or
// TaskNew (0) if none are running.
func (x *joinBranch[C, E]) runningNext() TaskStatus {
	next := TaskNew
	for i := range x.tasks {
		if x.tasks[i] >= TaskRunning {
			if next == TaskNew {
				next = x.tasks[i]
			} else {
				next = min(next, x.tasks[i])
			}
		}
	}
	return next
}

// settle decides the parallel's aggregate status from the success/fail tallies.
// next is the minimum wake hint over running children (TaskNew if none).
func (x *joinBranch[C, E]) settle(next TaskStatus) TaskStatus {
	if x.success >= x.n.Require {
		return TaskSuccess
	}
	if x.n.FailRequire > 0 && x.complete-x.success >= x.n.FailRequire {
		return TaskFail
	}
	running := int32(len(x.roots)) - x.complete
	if running == 0 {
		return TaskFail // all done without enough successes
	}
	if x.n.FailFast && x.success+running < x.n.Require {
		return TaskFail // can no longer reach the success requirement
	}
	return next
}

func shuffleOrder(rng Rand, n int) []int32 {
	o := make([]int32, n)
	for i := range n {
		o[i] = int32(i)
	}
	rng.Shuffle(n, func(i, j int) {
		o[i], o[j] = o[j], o[i]
	})
	return o
}

// reactiveBranch re-evaluates its children from the first one on every update,
// so a higher-priority (earlier) child whose condition becomes true can preempt
// a running lower-priority child. Both entry points drive this: Execute does a
// full re-sweep, and OnEvent re-checks the strictly-higher-priority children
// before forwarding the event to the active child — so preemption (selector) and
// condition-abort (sequence) are event-driven, not merely tick-driven. Like
// joinBranch it owns one Root per child and is itself a stack-rebuilding leaf (so
// OnEvent reaches it). A completed child's Root auto-restarts (re-generates) on
// the next visit — that is the reactive contract, so condition children must be
// idempotent and side-effect free.
type reactiveBranch[C Ctx, E EI] struct {
	n        *Node[C, E]
	parent   TaskI[C, E]
	roots    []Root[C, E]
	sequence bool  // true: ReactiveSequence; false: ReactiveSelector
	active   int32 // index of the currently running child, or -1
}

func (x *reactiveBranch[C, E]) SetParent(parent TaskI[C, E]) { x.parent = parent }

func (x *reactiveBranch[C, E]) Parent() TaskI[C, E] { return x.parent }

func (x *reactiveBranch[C, E]) OnComplete(c C, _ bool) {
	for i := range x.roots {
		x.roots[i].Cancel(c)
	}
}

func (x *reactiveBranch[C, E]) Execute(c C, _ *TaskI[C, E], from TaskStatus) TaskStatus {
	if from == TaskNew {
		if s := checkGuard(x.n, c); s != TaskSuccess {
			return s
		}
		l := len(x.n.Children)
		x.roots = make([]Root[C, E], l)
		for i := range l {
			x.roots[i].SetNode(x.n.Children[i])
		}
		x.active = -1
	}
	// Re-tick from the first child on every update.
	return x.sweep(c, 0)
}

// sweep ticks children starting at index i until one becomes Running or the
// branch settles. Children before a Running child are re-evaluated (their Roots
// auto-restart), which is what enables higher-priority preemption.
func (x *reactiveBranch[C, E]) sweep(c C, i int32) TaskStatus {
	n := int32(len(x.roots))
	for ; i < n; i++ {
		if r := x.classify(c, i, x.roots[i].Execute(c)); r != TaskNew {
			return r
		}
	}
	x.active = -1
	if x.sequence {
		return TaskSuccess // all children succeeded
	}
	return TaskFail // selector: all children failed
}

// classify interprets child i's result st under the reactive policy. It returns
// the branch's running/terminal status, or TaskNew meaning "advance to the next
// child". When a child becomes the active running one, lower-priority leftovers
// are cancelled.
func (x *reactiveBranch[C, E]) classify(c C, i int32, st TaskStatus) TaskStatus {
	if st >= TaskRunning {
		x.haltFrom(c, i+1)
		x.active = i
		return st
	}
	if x.sequence {
		if st == TaskSuccess {
			return TaskNew // advance to next child
		}
		x.haltFrom(c, i+1)
		x.active = -1
		return TaskFail
	}
	// selector
	if st == TaskSuccess {
		x.haltFrom(c, i+1)
		x.active = -1
		return TaskSuccess
	}
	return TaskNew // failure: advance to next child
}

// haltFrom cancels the Roots in [i, len) that still hold an active stack.
func (x *reactiveBranch[C, E]) haltFrom(c C, i int32) {
	for ; int(i) < len(x.roots); i++ {
		x.roots[i].Cancel(c)
	}
}

// OnEvent makes preemption and condition-abort event-driven. It mirrors what an
// Execute sweep would decide, but lets the active child consume the event.
//
// Phase 1 re-evaluates the strictly-higher-priority children [0, active). These
// are dormant conditions: they hold no running leaf, so they cannot consume the
// event — instead we re-tick them. A flipped condition then preempts via the
// usual classify rules: in a selector a higher option that now runs/succeeds
// takes over (cancelling the active child); in a sequence an earlier precondition
// that now fails aborts the whole branch. Phase 2 forwards the event to the
// active child; if it completes, we advance through the remaining lower-priority
// children exactly like the Execute path.
//
// NOTE: preemption is only as timely as the events it rides on. Under a discrete
// (timer/event) scheduler, a higher-priority condition that flips with no
// accompanying event is not noticed until the active child's delay hint elapses
// and a full Execute runs — so such conditions must be event-backed. A continuous
// (every-frame) scheduler re-sweeps via Execute each frame regardless. Phase 1
// also reads the world as it is when the event arrives: if the active child's own
// Phase 2 OnEvent mutates a higher-priority condition, that flip is picked up on
// the next event/tick, not retroactively within this same call.
func (x *reactiveBranch[C, E]) OnEvent(c C, e E) TaskStatus {
	if x.active < 0 {
		return TaskNew
	}
	active := x.active
	// Phase 1: a higher-priority dormant child may preempt (selector) or abort
	// the branch (sequence) now that an event has changed the world.
	for i := int32(0); i < active; i++ {
		if r := x.classify(c, i, x.roots[i].Execute(c)); r != TaskNew {
			return r
		}
	}
	// Phase 2: no preemption — the active child consumes the event.
	st := x.roots[active].OnEvent(c, e)
	if st == TaskNew {
		return TaskNew // unhandled
	}
	if st >= TaskRunning {
		return st // still running, active unchanged
	}
	// Active child completed via the event: advance from it without re-running
	// it (classify on the completed result, then sweep the rest).
	if r := x.classify(c, active, st); r != TaskNew {
		return r
	}
	return x.sweep(c, active+1)
}

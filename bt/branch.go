package bt

import "math/rand/v2"

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
		x.order = _shuffle(len(x.n.Children))
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
	next := TaskNew
	for i := range x.roots {
		if x.tasks[i] >= TaskRunning {
			st := x.roots[i].OnEvent(c, e)
			if st >= TaskRunning {
				x.tasks[i] = st
				if next == TaskNew {
					next = x.tasks[i]
				} else {
					next = min(next, x.tasks[i])
				}
			} else if st != TaskNew {
				x.tasks[i] = st
				x.complete++
				if x.tasks[i] == TaskSuccess {
					x.success++
				}
			}
		}
	}
	if x.n.Require > 0 && x.n.CountMode.Require(x.complete, x.success) >= x.n.Require {
		return TaskSuccess
	}
	if x.complete >= int32(len(x.roots)) {
		return TaskFail
	}
	return next
}

func (x *joinBranch[C, E]) Execute(c C, stk *TaskI[C, E], from TaskStatus) TaskStatus {
	if from == TaskNew {
		if s := checkGuard(x.n, c); s != TaskSuccess {
			return s
		}
		l := len(x.n.Children)
		x.roots = make([]Root[C, E], l)
		x.tasks = make([]TaskStatus, l)
		for i := 0; i < l; i++ {
			x.roots[i].SetNode(x.n.Children[i])
		}
	}
	next := int32(-1)
	for i := range x.roots {
		if x.tasks[i] >= TaskNew { // New or Running
			x.tasks[i] = x.roots[i].Execute(c)
			if x.tasks[i] >= TaskRunning {
				if next == -1 {
					next = int32(x.tasks[i])
				} else {
					next = min(next, int32(x.tasks[i]))
				}
			} else {
				x.complete++
				if x.tasks[i] == TaskSuccess {
					x.success++
				}
			}
		}
	}
	if x.n.Require > 0 && x.n.CountMode.Require(x.complete, x.success) >= x.n.Require {
		return TaskSuccess
	}
	if x.complete >= int32(len(x.roots)) {
		return TaskFail
	}
	return TaskStatus(next)
}

func _shuffle(n int) []int32 {
	o := make([]int32, n)
	for i := 0; i < n; i++ {
		o[i] = int32(i)
	}
	rand.Shuffle(n, func(i, j int) {
		o[i], o[j] = o[j], o[i]
	})
	return o
}

package bt

/*
	decorator 文件下的所有 task 都最多只有一颗子树
*/

// revise 等待子节点执行完毕后，根据 f 函数修改执行结果。
type revise[C Ctx, E EI] struct {
	n      *Node[C, E]
	parent TaskI[C, E]
}

func (x *revise[C, E]) SetParent(n TaskI[C, E]) {
	x.parent = n
}

func (x *revise[C, E]) Parent() TaskI[C, E] {
	return x.parent
}

func (x *revise[C, E]) OnComplete(_ bool) {}

func (x *revise[C, E]) Execute(c C, stk *TaskI[C, E], from TaskStatus) TaskStatus {
	if from == TaskNew {
		if s := checkGuard(x.n, c); s != TaskSuccess {
			return s
		}
		push(stk, x.n.Children[0].Generate(c))
		return TaskNew
	}
	return x.n.Revise(from)
}

// repeat 重复执行子节点最多MaxLoop次，直到满足Require次成功。
type repeat[C Ctx, E EI] struct {
	n              *Node[C, E]
	parent         TaskI[C, E]
	curLoop, count int32
}

func (x *repeat[C, E]) SetParent(n TaskI[C, E]) {
	x.parent = n
}

func (x *repeat[C, E]) Parent() TaskI[C, E] {
	return x.parent
}

func (x *repeat[C, E]) OnComplete(_ bool) {}

func (x *repeat[C, E]) Execute(c C, stk *TaskI[C, E], from TaskStatus) TaskStatus {
	if from == TaskNew {
		if s := checkGuard(x.n, c); s != TaskSuccess {
			return s
		}
		push(stk, x.n.Children[0].Generate(c))
		return TaskNew
	}
	x.curLoop++
	if x.n.CountMode.Count(from == TaskSuccess) {
		x.count++
	}
	if x.n.Require > 0 && x.count >= x.n.Require {
		return TaskSuccess
	}
	if x.n.MaxLoop > 0 && x.curLoop >= x.n.MaxLoop {
		return TaskFail
	}
	push(stk, x.n.Children[0].Generate(c))
	return TaskNew
}

// postGuard 在执行子树完成后才checkGuard，并用checkGuard的结果替代子树的结果
type postGuard[C Ctx, E EI] struct {
	n      *Node[C, E]
	parent TaskI[C, E]
}

func (x *postGuard[C, E]) SetParent(n TaskI[C, E]) {
	x.parent = n
}

func (x *postGuard[C, E]) Parent() TaskI[C, E] {
	return x.parent
}

func (x *postGuard[C, E]) OnComplete(_ bool) {}

func (x *postGuard[C, E]) Execute(c C, stk *TaskI[C, E], from TaskStatus) TaskStatus {
	if from == TaskNew {
		push(stk, x.n.Children[0].Generate(c))
		return TaskNew
	}
	if s := checkGuard(x.n, c); s != TaskSuccess {
		return TaskFail
	}
	return TaskSuccess
}

// alwaysGuard 每次update时，都会检查guard是否通过
// alwaysGuard 会本地重建栈，因此他自己是一个leaf task
type alwaysGuard[C Ctx, E EI] struct {
	n      *Node[C, E]
	parent TaskI[C, E]
	r      Root[C, E]
}

func (x *alwaysGuard[C, E]) SetParent(n TaskI[C, E]) {
	x.parent = n
}

func (x *alwaysGuard[C, E]) Parent() TaskI[C, E] {
	return x.parent
}

func (x *alwaysGuard[C, E]) OnComplete(cancel bool) {
	if cancel {
		x.r.Cancel()
	}
}

func (x *alwaysGuard[C, E]) OnEvent(c C, e E) TaskStatus {
	if x.n.OnEvent != nil {
		// 如果alwaysGuard接受信号，并被信号打断，则直接退出任务。
		s := x.n.OnEvent(c, e)
		if s >= TaskNew {
			return x.r.OnEvent(c, e)
		}
		// 不是正常的Cancel流程，需要手动Cancel
		x.r.Cancel()
		return s
	}
	return x.r.OnEvent(c, e)
}

func (x *alwaysGuard[C, E]) Execute(c C, _ *TaskI[C, E], from TaskStatus) TaskStatus {
	if s := checkGuard(x.n, c); s != TaskSuccess {
		return s
	}
	if from == TaskNew {
		x.r.SetNode(x.n.Children[0])
	}
	return x.r.Execute(c)
}

// guard 一种leaf task，他只是单纯地执行一次checkGuard并返回
type guard[C Ctx, E EI] struct {
	n      *Node[C, E]
	parent TaskI[C, E]
}

func (x *guard[C, E]) SetParent(n TaskI[C, E]) {
	x.parent = n
}

func (x *guard[C, E]) Parent() TaskI[C, E] {
	return x.parent
}

func (x *guard[C, E]) OnComplete(_ bool) {}

func (x *guard[C, E]) Execute(c C, _ *TaskI[C, E], _ TaskStatus) TaskStatus {
	return checkGuard(x.n, c)
}

// task 是一种leaf task，用户传入 TaskCreator 决定 task 的逻辑
type task[C Ctx, E EI] struct {
	n      *Node[C, E]
	parent TaskI[C, E]
	tt     LeafTaskI[C, E]
}

func (x *task[C, E]) SetParent(n TaskI[C, E]) {
	x.parent = n
}

func (x *task[C, E]) Parent() TaskI[C, E] {
	return x.parent
}

func (x *task[C, E]) OnComplete(cancel bool) {
	if x.tt != nil {
		x.tt.OnComplete(cancel)
	}
}

func (x *task[C, E]) OnEvent(c C, e E) TaskStatus {
	return x.tt.OnEvent(c, e)
}

func (x *task[C, E]) Execute(c C, _ *TaskI[C, E], from TaskStatus) TaskStatus {
	if from == TaskNew {
		if s := checkGuard(x.n, c); s != TaskSuccess {
			return s
		}
		x.tt = x.n.Task(c)
	}
	return x.tt.Execute(c)
}

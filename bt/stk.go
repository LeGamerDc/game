package bt

// Root 行为树任务树的子树入口。
type Root[C Ctx, E EI] struct {
	stk TaskI[C, E]
	n   *Node[C, E]
}

func (r *Root[C, E]) SetNode(n *Node[C, E]) {
	r.n = n
}

func (r *Root[C, E]) Execute(c C) (next TaskStatus) {
	// 考虑到代码可读性，目前栈相关操作都包装在 push/top/pop 函数中，
	// 即使这些函数很简单，但因为泛型的原因，它们不能被内联，可能影响性能。
	// 后续稳定后考虑直接展开函数以提升性能。
	next = TaskRunning
	if top(&r.stk) == nil {
		next = TaskNew
		push(&r.stk, r.n.Generate(c))
	}
	for v := top(&r.stk); v != nil; v = top(&r.stk) {
		next = v.Execute(c, &r.stk, next)
		switch {
		case next >= TaskRunning:
			return next
		case next == TaskNew:
			// do nothing
		default: // success or fail
			pop(&r.stk)
			v.OnComplete(false)
		}
	}
	return
}

func (r *Root[C, E]) OnEvent(c C, e E) (next TaskStatus) {
	if v := top(&r.stk); v != nil {
		vv, ok := v.(EventTask[C, E])
		if !ok {
			return TaskNew
		}
		if next = vv.OnEvent(c, e); next >= TaskNew {
			// 叶节点处理后仍处于Running 或 无法处理 event
			return next
		}
		// 叶节点处理 event 后完成任务，转为正常执行
		pop(&r.stk)
		v.OnComplete(false)

		return r.Execute(c)
	}
	return TaskNew
}

func (r *Root[C, E]) Cancel() {
	for v := top(&r.stk); v != nil; v = top(&r.stk) {
		pop(&r.stk)
		v.OnComplete(true)
	}
}

func push[C Ctx, E EI](s *TaskI[C, E], t TaskI[C, E]) {
	t.SetParent(*s)
	*s = t
}

func top[C Ctx, E EI](s *TaskI[C, E]) TaskI[C, E] {
	return *s
}

func pop[C Ctx, E EI](s *TaskI[C, E]) TaskI[C, E] {
	v := *s
	*s = (*s).Parent()
	v.SetParent(nil)
	return v
}

func checkGuard[C Ctx, E EI](n *Node[C, E], c C) TaskStatus {
	if n.Guard == nil {
		return TaskSuccess
	}
	v, e := n.Guard(c)
	if e != nil {
		// TODO Log
		return TaskFail
	}
	if vv, found := v.Bool(); !(found && vv) {
		return TaskFail
	}
	return TaskSuccess
}

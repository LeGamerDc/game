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
	return r.execute(c, next)
}

func (r *Root[C, E]) execute(c C, next TaskStatus) TaskStatus {
	for v := top(&r.stk); v != nil; v = top(&r.stk) {
		next = v.Execute(c, &r.stk, next)
		switch {
		case next >= TaskRunning:
			// 叶节点处于 Running 状态，整个栈可以暂停运行了。
			return next
		case next == TaskNew:
			// 节点返回 TaskNew 表示刚刚 push 了一个新的节点到栈顶
		default:
			// 节点任务完成，从栈顶弹出，调用 OnComplete 清理资源
			pop(&r.stk)
			v.OnComplete(c, false)
		}
	}
	return next
}

func (r *Root[C, E]) OnEvent(c C, e E) (next TaskStatus) {
	if v := top(&r.stk); v != nil {
		vv, ok := v.(EventTask[C, E])
		if !ok {
			// 无法处理事件，返回TaskNew表示事件未处理
			return TaskNew
		}
		if next = vv.OnEvent(c, e); next >= TaskNew {
			// 叶节点处理后仍处于Running 或 无法处理 event
			return next
		}
		// 叶节点处理 event 后完成任务，转为正常执行
		pop(&r.stk)
		v.OnComplete(c, false)

		return r.execute(c, next)
	}
	return TaskNew
}

func (r *Root[C, E]) Cancel(c C) {
	// 从栈顶开始（沿着树的路径向上）调用 OnComplete 清理节点
	for v := top(&r.stk); v != nil; v = top(&r.stk) {
		pop(&r.stk)
		v.OnComplete(c, true)
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

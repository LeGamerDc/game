# One simple and fast behavior tree implemention

## Introduction 

行为树被广泛应用于游戏开发中，随着 npc 行为变得复杂，行为树的节点越来越多，行为树的执行效率也在变慢。之前的工作提出了一些跳过中间节点的设计，包括在节点上维护一个指向首个不可以被跳过节点的指针，使用非递归的行为树执行方式（调度节点）。在这篇文章中，我们提出一种基于手动维护栈的行为树实现方式，可以高效地跳过中间节点，并保持实现难度大致与递归实现相当，额外的，保持行为树的高相应性（reactivity）。

## How we did it?

我们的实现基于一个工具和一个观察。这个工具是指一个手动维护的执行栈，当执行到返回的 Running 的叶子节点时，这个节点必然位于栈顶，当下次执行执行行为树时，位于栈顶的 Running 叶子节点可以立即被取出执行，跳过中间所有的节点。

但有一些节点例外，alwaysCheckCondition 在行为树每次执行时都需要检查 condition 是否还满足；parallel 节点会使得一个栈无法满足执行的要求。我们注意到，无论一个子树多么复杂，有多少节点，理论上它们都可以被一个单独的叶子节点在逻辑上代替。因此，要在手动维护栈上支持上述节点的办法就是，割裂以这个节点为根的子树与行为树的联系，它们不被栈管理，因此就不会出现栈无法满足的问题。这时，alwaysCheckCondition 因为整颗子树都处于一个叶节点的状态，因此每次执行行为树都会被栈 top 然后检查，然后 alwaysCheckCondition 内部重新维护一个栈来处理它的子树；parallel 节点可以维护一个手动维护栈的 array，来同时运行多颗子树。

是否听起来节点的实现稍稍复杂，但是在实现上却跟递归实现相差无几。

### task(node's instance) and stack

```go
const (
	TaskRunning TaskStatus = 1
	TaskNew     TaskStatus = 0
	TaskSuccess TaskStatus = -1
	TaskFail    TaskStatus = -2
)
type TaskI[C Ctx, E EI] interface {
    Execute(c C, stk *TaskI[C, E], from TaskStatus) TaskStatus
    Parent() TaskI[C, E]
    SetParent(TaskI[C, E])
    OnComplete(c C, cancel bool)
}
```

这是task 暴露给栈的接口，`Parent` 跟 `SetParent` 函数用于支持栈操作，`OnComplete` 函数用于清理 task（正常执行完毕或者被取消），`Execute` 函数用于执行 task，大体与递归版本的行为树类似，但 task 的 `Execute` 还有一些额外的任务需要做。

1. 当 from == TaskNew 时，`Execute` 需要处理 task 被首次执行的任务。
2. 当 from == TaskRunning 时，`Execute` 需要处理 task 处于 Running 状态时再次被执行的任务. 注意，由于中间节点在执行行为树时会被跳过，因此中间节点不需要考虑 from == TaskRunning 的情况，from == TaskRunning 的情况仅适用于叶子节点（当然也包括重新维护手动栈的那些节点）。
3. 当 from == TaskSuccess/TaskFail 时，`Execute` 需要处理节点的子节点执行完毕的任务。因此如果节点有多个子节点，需要在 task 内部记录处理的 index. 注意，叶子节点（当然也包括重新维护手动栈的那些节点）不会遇到子节点执行完毕的情况。
4. 当节点（中间节点）对子节点进行实例化时（创建 task），需要对 stk 进行 push 操作。

```go
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

type Root[C Ctx, E EI] struct {
	stk TaskI[C, E]
	n   *Node[C, E]
}
func (r *Root[C, E]) Execute(c C) (next TaskStatus) {
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
		case next == TaskRunning:
            // pause running the stack
			return next
		case next == TaskNew:
            // nothing todo 
		default: // TaskSuccess or TaskFail
            // task complete, transfer TaskStatus to it's parent as from 
			pop(&r.stk)
			v.OnComplete(c, false)
		}
	}
	return next
}
```

`Root` 是保存栈的结构体，它还存放了节点的 class 用于启动这颗子树。`Execute` 和 `execute` 中，就是我们如何在手动维护栈中处理 task 的代码，可以看到处理方式几乎和递归实现一样。区别在于，当节点返回 Running 时，可以理解暂停执行整个栈而不是强迫地递归返回。

### glimpse of task

这里举例一些 task 的实现，作为对比手动维护栈和递归实现的对比。

```go
// repeat execute subtree untill count's success
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
func (x *repeat[C, E]) OnComplete(C, bool) {}
func (x *repeat[C, E]) Execute(c C, stk *TaskI[C, E], from TaskStatus) TaskStatus {
	if from == TaskNew {
		if s := checkGuard(x.n, c); s != TaskSuccess {
			return s
		}
		push(stk, x.n.Children[0].Generate(c))
		return TaskNew
	}
    // since repeat is s middle task, from cannot be TaskRunning.
	x.curLoop++
	if from == TaskSuccess {
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

// alwaysCheckCondition checks condition every execution.
type alwaysCheckCondition[C Ctx, E EI] struct {
	n      *Node[C, E]
	parent TaskI[C, E]
	r      Root[C, E]
}
func (x *alwaysCheckCondition[C, E]) SetParent(n TaskI[C, E]) {
	x.parent = n
}
func (x *alwaysCheckCondition[C, E]) Parent() TaskI[C, E] {
	return x.parent
}
func (x *alwaysCheckCondition[C, E]) OnComplete(c C, cancel bool) {
	if cancel {
		x.r.Cancel(c)
	}
}
func (x *alwaysCheckCondition[C, E]) Execute(c C, _ *TaskI[C, E], from TaskStatus) TaskStatus {
    // alwaysCheckCondition is a leaf task, so from might be TaskNew/TaskRunning， both need checking condition
	if s := checkGuard(x.n, c); s != TaskSuccess {
		return s
	}
	if from == TaskNew {
		x.r.SetNode(x.n.Children[0])
	}
	return x.r.Execute(c)
}
```

上面两个示例是只有一个子节点的情况，可以看出 `Execute` 的实现跟递归实现几乎一样，用户需要考虑的只有 3 条。

1. 中间节点不会收到 from = TaskRunning
2. 叶节点不会收到 from = TaskSuccess/TaskFail
3. 中间节点创建子节点后需要 push stack

```go
// sequence execute all children from left to right, success when all children success, fail when any child fail.
type sequence[C Ctx, E EI] struct {
	n          *Node[C, E]
	parent     TaskI[C, E]
    idx        int32   
}
func (x *sequence[C, E]) SetParent(parent TaskI[C, E]) {
	x.parent = parent
}
func (x *sequence[C, E]) Parent() TaskI[C, E] {
	return x.parent
}
func (x *sequence[C, E]) OnComplete(C, bool) {}
func (x *sequence[C, E]) Execute(c C, stk *TaskI[C, E], from TaskStatus) TaskStatus {
	if from == TaskNew {
		if s := checkGuard(x.n, c); s != TaskSuccess {
			return s
		}
		push(stk, x.n.Children[0].Generate(c))
		return TaskNew
	}
    // from might be TaskSuccess/TaskFail
    x.idx++
    if from == TaskFail {
        return TaskFail
    }
    if int(x.idx) >= len(x.n.Children) {
        return TaskSucess
    }
	push(stk, x.n.Children[x.idx].Generate(c))
	return TaskNew
}

// parallel starts all children, and complete when requirement satisfied, cancel rest Running children.
type parallel[C Ctx, E EI] struct {
	n                 *Node[C, E]
	parent            TaskI[C, E]
	roots             []Root[C, E]
	tasks             []TaskStatus
	success, complete int32
}
func (x *parallel[C, E]) SetParent(parent TaskI[C, E]) {
	x.parent = parent
}
func (x *parallel[C, E]) Parent() TaskI[C, E] {
	return x.parent
}
func (x *parallel[C, E]) OnComplete(c C, cancel bool) {
	for i := range x.roots {
		if x.tasks[i] >= TaskRunning {
			x.roots[i].Cancel()
		}
	}
}
func (x *parallel[C, E]) Execute(c C, stk *TaskI[C, E], from TaskStatus) TaskStatus {
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
    // from == TaskRunning
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
```

`sequence` 稍微复杂一些，也仅仅时需要记录当前运行到的子节点的序号。`parallel` 是整个 task 中实现最复杂的，它需要记录每个执行栈的当前情况。但也和递归实现复杂度差不多。

### cancellation

得益于手动维护栈与递归实现相同的执行顺序，我们在制作一个高性能的行为树时几乎没有遇到障碍。在 task 的实现中，`OnComplete` 的参数中有一个 cancel，用来标志节点是否执行完毕后处理清理任务。cancel 的逻辑也相当简单，将栈中节点依次 pop 出并清理即可。

```go
func (r *Root[C, E]) Cancel(c C) {
	for v := top(&r.stk); v != nil; v = top(&r.stk) {
		pop(&r.stk)
		v.OnComplete(c, true)
	}
}
```

### reactivity

手动维护栈的行为树实现中，由于可能处理事件的节点始终位于栈顶，因此与`Execute`一样，`OnEvent` 也只需要处理栈顶的节点即可，对于那些手动维护了栈的节点，当然也需要将`OnEvent`传递下去。如果`OnEvent`导致了节点完成，我们可以立即转为`Execute`逻辑。这里，我们用了 TaskNew 这个状态表示节点无法处理事件。

```go
func (r *Root[C, E]) OnEvent(c C, e E) (next TaskStatus) {
	if v := top(&r.stk); v != nil {
		vv, ok := v.(EventTask[C, E])
		if !ok {
			return TaskNew
		}
		if next = vv.OnEvent(c, e); next >= TaskNew {
			return next
		}
		pop(&r.stk)
		v.OnComplete(c, false)
		return r.execute(c, next)
	}
	return TaskNew
}
```

## conclusion

在各种行为树的实现中，我们的实现可能不是最快的，但我们提出了一种足够简单不容易犯错又足够快的实现，希望游戏开发者能够更加容易的理解并拓展新的节点。
# A Fast and Simple Behavior Tree Implementation Using Manual Stack Management

## Abstract

This paper presents a novel behavior tree implementation that addresses the performance overhead of traditional recursive traversal in complex game AI systems. Our approach utilizes manually maintained execution stacks to enable direct execution of running leaf nodes, bypassing intermediate tree traversal while preserving the simplicity of recursive implementations. The system maintains high reactivity through event-driven execution and provides efficient cancellation mechanisms. We demonstrate that this approach achieves significant performance improvements while keeping implementation complexity manageable for game developers.

## Introduction

Behavior trees have become the de facto standard for implementing complex AI behaviors in modern game development. However, as NPC behaviors grow increasingly sophisticated, the computational overhead of traditional behavior tree implementations becomes a significant bottleneck. Traditional recursive implementations require complete tree traversal from root to the currently executing leaf node on every update cycle, resulting in O(depth) overhead per execution.

Previous research has proposed several optimization strategies, including maintaining direct pointers to non-skippable nodes and implementing non-recursive task scheduling systems. While these approaches reduce traversal overhead, they often increase implementation complexity significantly.

In this paper, we present a behavior tree implementation based on manually maintained execution stacks that efficiently eliminates intermediate node traversal while maintaining implementation simplicity comparable to recursive approaches. Our key contributions include:

1. A stack-based execution model that enables direct leaf node access
2. A unified approach for handling complex node types (parallel, conditional) within the stack paradigm
3. High-reactivity event handling without polling overhead
4. Comprehensive cancellation and cleanup mechanisms

## Methodology

### Core Design Principles

Our implementation is built on one fundamental tool and one key observation. The tool is a manually maintained execution stack that preserves the current execution path. The observation is that when execution reaches a leaf node returning a `Running` status, this node necessarily resides at the stack top and can be directly accessed in subsequent execution cycles, completely bypassing intermediate nodes.

However, certain node types present challenges to this approach. Conditional nodes requiring re-evaluation on every execution (`alwaysCheckCondition`) and parallel execution nodes (`parallel`) cannot be effectively managed by a single execution stack. 

We address this challenge through a key insight: regardless of subtree complexity, any subtree can be logically represented as a single leaf node from the parent tree's perspective. Therefore, problematic nodes are handled by severing their subtrees from the main execution stack—they become leaf nodes that internally manage their own execution contexts. This approach allows `alwaysCheckCondition` nodes to re-evaluate conditions on every execution while maintaining separate internal stacks for their subtrees. Similarly, `parallel` nodes maintain arrays of execution stacks to handle concurrent subtree execution.

### Task Interface and Stack Operations

Our implementation centers around the `TaskI` interface, which represents node instances within the execution stack:

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

The interface provides stack navigation through `Parent` and `SetParent` methods, resource cleanup via `OnComplete`, and execution logic through `Execute`. The `Execute` method's `from` parameter indicates the execution context:

1. **`from == TaskNew`**: Initial task execution
2. **`from == TaskRunning`**: Continuation of a previously running leaf task
3. **`from == TaskSuccess/TaskFail`**: Child task completion notification

The stack operations are implemented as follows:

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
```

### Execution Engine

The `Root` structure manages the execution stack and provides the primary execution interface:

```go
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
		case next >= TaskRunning:
			// Leaf node is running; pause stack execution
			return next
		case next == TaskNew:
			// New child task was pushed to stack
		default: // TaskSuccess or TaskFail
			// Task completed; pop from stack and cleanup
			pop(&r.stk)
			v.OnComplete(c, false)
		}
	}
	return next
}
```

This execution model maintains identical semantics to recursive implementations while enabling stack persistence across execution cycles.

### Task Implementation Examples

#### Single-Child Decorators

```go
// Repeat node executes subtree until achieving required success count
type repeat[C Ctx, E EI] struct {
	n              *Node[C, E]
	parent         TaskI[C, E]
	curLoop, count int32
}

func (x *repeat[C, E]) Execute(c C, stk *TaskI[C, E], from TaskStatus) TaskStatus {
	if from == TaskNew {
		if s := checkGuard(x.n, c); s != TaskSuccess {
			return s
		}
		push(stk, x.n.Children[0].Generate(c))
		return TaskNew
	}
	// Handle child completion
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
```

#### Self-Managing Leaf Nodes

```go
// AlwaysCheckCondition re-evaluates conditions on every execution
type alwaysCheckCondition[C Ctx, E EI] struct {
	n      *Node[C, E]
	parent TaskI[C, E]
	r      Root[C, E]  // Internal execution context
}

func (x *alwaysCheckCondition[C, E]) Execute(c C, _ *TaskI[C, E], from TaskStatus) TaskStatus {
	// Re-evaluate condition on every execution
	if s := checkGuard(x.n, c); s != TaskSuccess {
		return s
	}
	if from == TaskNew {
		x.r.SetNode(x.n.Children[0])
	}
	return x.r.Execute(c)
}
```

#### Multi-Child Branch Nodes

```go
// Sequence node executes children sequentially
type sequence[C Ctx, E EI] struct {
	n          *Node[C, E]
	parent     TaskI[C, E]
	idx        int32
}

func (x *sequence[C, E]) Execute(c C, stk *TaskI[C, E], from TaskStatus) TaskStatus {
	if from == TaskNew {
		if s := checkGuard(x.n, c); s != TaskSuccess {
			return s
		}
		push(stk, x.n.Children[0].Generate(c))
		return TaskNew
	}
	// Process child completion
	x.idx++
	if from == TaskFail {
		return TaskFail
	}
	if int(x.idx) >= len(x.n.Children) {
		return TaskSuccess
	}
	push(stk, x.n.Children[x.idx].Generate(c))
	return TaskNew
}
```

#### Parallel Execution

```go
// Parallel node manages multiple concurrent subtrees
type parallel[C Ctx, E EI] struct {
	n                 *Node[C, E]
	parent            TaskI[C, E]
	roots             []Root[C, E]    // Individual execution contexts
	tasks             []TaskStatus
	success, complete int32
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
	
	// Execute all active subtrees
	next := int32(-1)
	for i := range x.roots {
		if x.tasks[i] >= TaskNew {
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
	
	// Check completion conditions
	if x.n.Require > 0 && x.n.CountMode.Require(x.complete, x.success) >= x.n.Require {
		return TaskSuccess
	}
	if x.complete >= int32(len(x.roots)) {
		return TaskFail
	}
	return TaskStatus(next)
}
```

### Implementation Guidelines

Developers implementing custom node types need to consider three key principles:

1. **Intermediate nodes never receive `from == TaskRunning`** (they are bypassed during continuation)
2. **Leaf nodes never receive `from == TaskSuccess/TaskFail`** (they don't have children)
3. **Intermediate nodes must push new tasks to the stack** after instantiating children

### Cancellation and Cleanup

The stack-based approach enables straightforward cancellation by traversing the execution path:

```go
func (r *Root[C, E]) Cancel(c C) {
	for v := top(&r.stk); v != nil; v = top(&r.stk) {
		pop(&r.stk)
		v.OnComplete(c, true)  // Signal cancellation
	}
}
```

### Event-Driven Reactivity

The system supports immediate event processing without polling overhead. Since event-capable nodes are always at the stack top, event handling requires minimal overhead:

```go
func (r *Root[C, E]) OnEvent(c C, e E) (next TaskStatus) {
	if v := top(&r.stk); v != nil {
		if vv, ok := v.(EventTask[C, E]); ok {
			if next = vv.OnEvent(c, e); next >= TaskNew {
				return next
			}
			// Event caused task completion; continue normal execution
			pop(&r.stk)
			v.OnComplete(c, false)
			return r.execute(c, next)
		}
	}
	return TaskNew  // Event not handled
}
```

## Results and Discussion

Our implementation achieves significant performance improvements over traditional recursive approaches while maintaining comparable implementation complexity. The stack-based design eliminates redundant tree traversal, reducing per-update complexity from O(depth) to O(1) for continuing tasks.

The approach successfully handles complex scenarios including:
- Conditional re-evaluation without performance penalties
- Efficient parallel subtree execution
- Event-driven task completion with minimal latency
- Comprehensive resource cleanup and cancellation

## Conclusion

We present a behavior tree implementation that effectively addresses the performance limitations of traditional recursive approaches while preserving implementation simplicity. Our manually maintained stack design enables direct access to running tasks, eliminating redundant traversal overhead. The system's support for complex node types through internal context management and event-driven reactivity makes it well-suited for demanding game AI applications.

While our implementation may not represent the absolute performance ceiling for behavior tree systems, it offers an optimal balance between execution efficiency and development simplicity. We believe this approach will enable game developers to implement more sophisticated AI behaviors without sacrificing performance or maintainability. 
package bt

import (
	"testing"

	"github.com/legamerdc/game/blackboard"
)

// 测试用的上下文实现
type testCtx struct {
	bb   map[string]blackboard.Field
	time int64
}

func (c *testCtx) Now() int64 {
	return c.time
}

func (c *testCtx) Get(key string) (blackboard.Field, bool) {
	v, ok := c.bb[key]
	return v, ok
}

func (c *testCtx) Set(key string, value blackboard.Field) {
	c.bb[key] = value
}

func (c *testCtx) Del(key string) {
	delete(c.bb, key)
}

// 测试用的事件类型
type testEvent struct {
	kind int32
	data string
}

func (e *testEvent) Kind() int32 {
	return e.kind
}

// 测试用的叶节点任务
type testTask struct {
	name        string
	result      TaskStatus
	executed    bool
	canceled    bool
	eventResult TaskStatus
}

func (t *testTask) Execute(c *testCtx) TaskStatus {
	t.executed = true
	return t.result
}

func (t *testTask) OnComplete(cancel bool) {
	t.canceled = cancel
}

func (t *testTask) OnEvent(c *testCtx, e *testEvent) TaskStatus {
	if t.eventResult != TaskNew {
		return t.eventResult
	}
	return TaskNew
}

func newTestTaskCreator(name string, result TaskStatus) TaskCreator[*testCtx, *testEvent] {
	return func(c *testCtx) LeafTaskI[*testCtx, *testEvent] {
		return &testTask{
			name:   name,
			result: result,
		}
	}
}

func newTestCtx() *testCtx {
	return &testCtx{
		bb:   make(map[string]blackboard.Field),
		time: 0,
	}
}

func alwaysTrue(c *testCtx) (blackboard.Field, error) {
	return blackboard.Bool(true), nil
}

func alwaysFalse(c *testCtx) (blackboard.Field, error) {
	return blackboard.Bool(false), nil
}

func TestTaskNode(t *testing.T) {
	// 测试基本的任务节点
	ctx := newTestCtx()

	// 成功的任务
	successNode := NewTask(alwaysTrue, newTestTaskCreator("success", TaskSuccess))
	root := &Root[*testCtx, *testEvent]{}
	root.SetNode(successNode)

	result := root.Execute(ctx)
	if result != TaskSuccess {
		t.Errorf("expected TaskSuccess, got %v", result)
	}

	// 失败的任务
	failNode := NewTask(alwaysTrue, newTestTaskCreator("fail", TaskFail))
	root.SetNode(failNode)

	result = root.Execute(ctx)
	if result != TaskFail {
		t.Errorf("expected TaskFail, got %v", result)
	}

	// Running的任务
	runningNode := NewTask(alwaysTrue, newTestTaskCreator("running", TaskRunning))
	root.SetNode(runningNode)

	result = root.Execute(ctx)
	if result != TaskRunning {
		t.Errorf("expected TaskRunning, got %v", result)
	}
}

func TestGuardNode(t *testing.T) {
	ctx := newTestCtx()

	// 守卫成功
	guardSuccessNode := NewGuard[*testCtx, *testEvent](alwaysTrue)
	root := &Root[*testCtx, *testEvent]{}
	root.SetNode(guardSuccessNode)

	result := root.Execute(ctx)
	if result != TaskSuccess {
		t.Errorf("expected TaskSuccess, got %v", result)
	}

	// 守卫失败
	guardFailNode := NewGuard[*testCtx, *testEvent](alwaysFalse)
	root.SetNode(guardFailNode)

	result = root.Execute(ctx)
	if result != TaskFail {
		t.Errorf("expected TaskFail, got %v", result)
	}
}

func TestSequenceNode(t *testing.T) {
	ctx := newTestCtx()

	// 所有子节点都成功
	seq := NewSequence(alwaysTrue, false,
		NewTask(alwaysTrue, newTestTaskCreator("task1", TaskSuccess)),
		NewTask(alwaysTrue, newTestTaskCreator("task2", TaskSuccess)),
		NewTask(alwaysTrue, newTestTaskCreator("task3", TaskSuccess)),
	)

	root := &Root[*testCtx, *testEvent]{}
	root.SetNode(seq)

	result := root.Execute(ctx)
	if result != TaskSuccess {
		t.Errorf("expected TaskSuccess, got %v", result)
	}

	// 有一个子节点失败
	seq = NewSequence(alwaysTrue, false,
		NewTask(alwaysTrue, newTestTaskCreator("task1", TaskSuccess)),
		NewTask(alwaysTrue, newTestTaskCreator("task2", TaskFail)),
		NewTask(alwaysTrue, newTestTaskCreator("task3", TaskSuccess)),
	)

	root.SetNode(seq)
	result = root.Execute(ctx)
	if result != TaskFail {
		t.Errorf("expected TaskFail, got %v", result)
	}
}

func TestSelectorNode(t *testing.T) {
	ctx := newTestCtx()

	// 第一个子节点就成功
	sel := NewSelector(alwaysTrue, false,
		NewTask(alwaysTrue, newTestTaskCreator("task1", TaskSuccess)),
		NewTask(alwaysTrue, newTestTaskCreator("task2", TaskFail)),
	)

	root := &Root[*testCtx, *testEvent]{}
	root.SetNode(sel)

	result := root.Execute(ctx)
	if result != TaskSuccess {
		t.Errorf("expected TaskSuccess, got %v", result)
	}

	// 所有子节点都失败
	sel = NewSelector(alwaysTrue, false,
		NewTask(alwaysTrue, newTestTaskCreator("task1", TaskFail)),
		NewTask(alwaysTrue, newTestTaskCreator("task2", TaskFail)),
	)

	root.SetNode(sel)
	result = root.Execute(ctx)
	if result != TaskFail {
		t.Errorf("expected TaskFail, got %v", result)
	}
}

func TestParallelNode(t *testing.T) {
	ctx := newTestCtx()

	// 并行执行，要求所有成功
	parallel := NewParallel(alwaysTrue, MatchAll, 3,
		NewTask(alwaysTrue, newTestTaskCreator("task1", TaskSuccess)),
		NewTask(alwaysTrue, newTestTaskCreator("task2", TaskSuccess)),
		NewTask(alwaysTrue, newTestTaskCreator("task3", TaskSuccess)),
	)

	root := &Root[*testCtx, *testEvent]{}
	root.SetNode(parallel)

	result := root.Execute(ctx)
	if result != TaskSuccess {
		t.Errorf("expected TaskSuccess, got %v", result)
	}

	// 并行执行，只要求一个成功
	parallel = NewParallel(alwaysTrue, MatchSuccess, 1,
		NewTask(alwaysTrue, newTestTaskCreator("task1", TaskFail)),
		NewTask(alwaysTrue, newTestTaskCreator("task2", TaskSuccess)),
		NewTask(alwaysTrue, newTestTaskCreator("task3", TaskFail)),
	)

	root.SetNode(parallel)
	result = root.Execute(ctx)
	if result != TaskSuccess {
		t.Errorf("expected TaskSuccess, got %v", result)
	}
}

func TestInverterNode(t *testing.T) {
	ctx := newTestCtx()

	// 反转成功节点应该失败
	inverter := NewInverter(alwaysTrue,
		NewTask(alwaysTrue, newTestTaskCreator("task", TaskSuccess)),
	)

	root := &Root[*testCtx, *testEvent]{}
	root.SetNode(inverter)

	result := root.Execute(ctx)
	if result != TaskFail {
		t.Errorf("expected TaskFail, got %v", result)
	}

	// 反转失败节点应该成功
	inverter = NewInverter(alwaysTrue,
		NewTask(alwaysTrue, newTestTaskCreator("task", TaskFail)),
	)

	root.SetNode(inverter)
	result = root.Execute(ctx)
	if result != TaskSuccess {
		t.Errorf("expected TaskSuccess, got %v", result)
	}
}

func TestRepeatNode(t *testing.T) {
	ctx := newTestCtx()

	// 重复直到3次成功
	repeat := NewRepeatUntilNSuccess(alwaysTrue, 3, 5,
		NewTask(alwaysTrue, newTestTaskCreator("task", TaskSuccess)),
	)

	root := &Root[*testCtx, *testEvent]{}
	root.SetNode(repeat)

	result := root.Execute(ctx)
	if result != TaskSuccess {
		t.Errorf("expected TaskSuccess, got %v", result)
	}
}

func TestAlwaysGuardNode(t *testing.T) {
	ctx := newTestCtx()

	// 守卫一直成功的情况
	alwaysGuard := NewAlwaysGuard(alwaysTrue,
		NewTask(alwaysTrue, newTestTaskCreator("task", TaskSuccess)),
	)

	root := &Root[*testCtx, *testEvent]{}
	root.SetNode(alwaysGuard)

	result := root.Execute(ctx)
	if result != TaskSuccess {
		t.Errorf("expected TaskSuccess, got %v", result)
	}

	// 守卫失败的情况
	alwaysGuard = NewAlwaysGuard(alwaysFalse,
		NewTask(alwaysTrue, newTestTaskCreator("task", TaskSuccess)),
	)

	root.SetNode(alwaysGuard)
	result = root.Execute(ctx)
	if result != TaskFail {
		t.Errorf("expected TaskFail, got %v", result)
	}
}

func TestPostGuardNode(t *testing.T) {
	ctx := newTestCtx()

	// 子任务成功但后置守卫失败
	postGuard := NewPostGuard(alwaysFalse,
		NewTask(alwaysTrue, newTestTaskCreator("task", TaskSuccess)),
	)

	root := &Root[*testCtx, *testEvent]{}
	root.SetNode(postGuard)

	result := root.Execute(ctx)
	if result != TaskFail {
		t.Errorf("expected TaskFail, got %v", result)
	}

	// 子任务成功且后置守卫成功
	postGuard = NewPostGuard(alwaysTrue,
		NewTask(alwaysTrue, newTestTaskCreator("task", TaskSuccess)),
	)

	root.SetNode(postGuard)
	result = root.Execute(ctx)
	if result != TaskSuccess {
		t.Errorf("expected TaskSuccess, got %v", result)
	}
}

func TestRunningTask(t *testing.T) {
	ctx := newTestCtx()

	// 创建一个Running任务
	runningTask := &testTask{
		name:   "running",
		result: TaskRunning,
	}

	node := NewTask(alwaysTrue, func(c *testCtx) LeafTaskI[*testCtx, *testEvent] {
		return runningTask
	})

	root := &Root[*testCtx, *testEvent]{}
	root.SetNode(node)

	// 第一次执行应该返回Running
	result := root.Execute(ctx)
	if result != TaskRunning {
		t.Errorf("expected TaskRunning, got %v", result)
	}
	if !runningTask.executed {
		t.Error("task should be executed")
	}

	// 修改任务结果为成功
	runningTask.result = TaskSuccess
	runningTask.executed = false

	// 第二次执行应该返回Success
	result = root.Execute(ctx)
	if result != TaskSuccess {
		t.Errorf("expected TaskSuccess, got %v", result)
	}
	if !runningTask.executed {
		t.Error("task should be executed again")
	}
}

func TestEventHandling(t *testing.T) {
	ctx := newTestCtx()

	// 创建一个可以响应事件的任务
	eventTask := &testTask{
		name:        "event",
		result:      TaskRunning,
		eventResult: TaskRunning, // 处理事件后仍在运行
	}

	node := NewTask(alwaysTrue, func(c *testCtx) LeafTaskI[*testCtx, *testEvent] {
		return eventTask
	})

	root := &Root[*testCtx, *testEvent]{}
	root.SetNode(node)

	// 第一次执行应该返回Running
	result := root.Execute(ctx)
	if result != TaskRunning {
		t.Errorf("expected TaskRunning, got %v", result)
	}

	// 发送事件，任务处理事件后仍在运行
	event := &testEvent{kind: 1, data: "test"}
	result = root.OnEvent(ctx, event)
	if result != TaskRunning {
		t.Errorf("expected TaskRunning from event, got %v", result)
	}

	// 测试无法处理事件的情况
	eventTask.eventResult = TaskNew
	result = root.OnEvent(ctx, event)
	if result != TaskNew {
		t.Errorf("expected TaskNew (cannot handle event), got %v", result)
	}
}

func TestCancelOperation(t *testing.T) {
	ctx := newTestCtx()

	// 创建一个Running任务
	runningTask := &testTask{
		name:   "running",
		result: TaskRunning,
	}

	node := NewTask(alwaysTrue, func(c *testCtx) LeafTaskI[*testCtx, *testEvent] {
		return runningTask
	})

	root := &Root[*testCtx, *testEvent]{}
	root.SetNode(node)

	// 执行任务
	result := root.Execute(ctx)
	if result != TaskRunning {
		t.Errorf("expected TaskRunning, got %v", result)
	}

	// 取消任务
	root.Cancel()

	if !runningTask.canceled {
		t.Error("task should be canceled")
	}
}

func TestComplexBehaviorTree(t *testing.T) {
	ctx := newTestCtx()

	// 构建一个复杂的行为树：
	// Sequence(
	//   Selector(Task1_Fail, Task2_Success),
	//   Parallel(Task3_Success, Task4_Success),
	//   Inverter(Task5_Fail)
	// )
	complexTree := NewSequence(alwaysTrue, false,
		NewSelector(alwaysTrue, false,
			NewTask(alwaysTrue, newTestTaskCreator("task1", TaskFail)),
			NewTask(alwaysTrue, newTestTaskCreator("task2", TaskSuccess)),
		),
		NewParallel(alwaysTrue, MatchAll, 2,
			NewTask(alwaysTrue, newTestTaskCreator("task3", TaskSuccess)),
			NewTask(alwaysTrue, newTestTaskCreator("task4", TaskSuccess)),
		),
		NewInverter(alwaysTrue,
			NewTask(alwaysTrue, newTestTaskCreator("task5", TaskFail)),
		),
	)

	root := &Root[*testCtx, *testEvent]{}
	root.SetNode(complexTree)

	result := root.Execute(ctx)
	if result != TaskSuccess {
		t.Errorf("expected TaskSuccess, got %v", result)
	}
}

func TestNodeValidation(t *testing.T) {
	// 测试节点验证

	// 空子节点的Sequence应该失败验证
	invalidSeq := &Node[*testCtx, *testEvent]{
		Type:     TypeSequenceBranch,
		Children: []*Node[*testCtx, *testEvent]{},
	}

	if err := invalidSeq.Check(); err == nil {
		t.Error("expected validation error for empty sequence")
	}

	// 正常的Sequence应该通过验证
	validSeq := NewSequence(alwaysTrue, false,
		NewTask(alwaysTrue, newTestTaskCreator("task", TaskSuccess)),
	)

	if err := validSeq.Check(); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestCountModes(t *testing.T) {
	// 测试不同的计数模式

	// MatchSuccess 模式
	if !CountMode(MatchSuccess).Count(true) {
		t.Error("MatchSuccess should count success")
	}
	if CountMode(MatchSuccess).Count(false) {
		t.Error("MatchSuccess should not count failure")
	}

	// MatchFail 模式
	if CountMode(MatchFail).Count(true) {
		t.Error("MatchFail should not count success")
	}
	if !CountMode(MatchFail).Count(false) {
		t.Error("MatchFail should count failure")
	}

	// MatchAll 模式
	if !CountMode(MatchAll).Count(true) {
		t.Error("MatchAll should count success")
	}
	if !CountMode(MatchAll).Count(false) {
		t.Error("MatchAll should count failure")
	}
}

func BenchmarkBehaviorTreeExecution(b *testing.B) {
	ctx := newTestCtx()

	// 构建一个中等复杂度的行为树用于基准测试
	tree := NewSequence(alwaysTrue, false,
		NewSelector(alwaysTrue, false,
			NewTask(alwaysTrue, newTestTaskCreator("task1", TaskFail)),
			NewTask(alwaysTrue, newTestTaskCreator("task2", TaskSuccess)),
		),
		NewTask(alwaysTrue, newTestTaskCreator("task3", TaskSuccess)),
	)

	root := &Root[*testCtx, *testEvent]{}
	root.SetNode(tree)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		root.Execute(ctx)
		// 重置root以便下次测试
		root.stk = nil
	}
}

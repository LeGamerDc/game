package bt

import (
	"fmt"
	"github.com/legamerdc/game/cc"
	"github.com/legamerdc/game/lib"
	"testing"

	"github.com/stretchr/testify/assert"
)

// 测试用的上下文实现
type testCtx struct {
	bb   map[string]lib.Field
	time int64
}

func (c *testCtx) Now() int64 {
	return c.time
}

func (c *testCtx) Get(key string) (lib.Field, bool) {
	v, ok := c.bb[key]
	return v, ok
}

func (c *testCtx) Set(key string, value lib.Field) {
	c.bb[key] = value
}

func (c *testCtx) Del(key string) {
	delete(c.bb, key)
}

func (c *testCtx) Exec(f string) (lib.Field, bool) {
	if f == "now" {
		return lib.Int64(c.time), true
	}
	return lib.Field{}, false
}

func (c *testCtx) GetInt64(key string) int64 {
	v, _ := c.Get(key)
	vv, _ := v.Int64()
	return vv
}

// 测试用的事件类型
type testEvent struct {
	kind int32
	data string
}

func (e *testEvent) Kind() int32 {
	return e.kind
}

var (
	_taskEnter       = "int d, wait, now; d = now()+wait"
	_taskExec        = "int d, now; now() >= d ? -1 : d - now()"
	_taskExit        = "int d; d = -1"
	_guardBeforeTime = "int now, p; p<=0?true:(now()<p)"

	_taskFuncEnter       = exec(cc.MustCompile[*testCtx](_taskEnter))
	_taskFuncExec        = execTask(cc.MustCompile[*testCtx](_taskExec))
	_taskFuncExit        = exec(cc.MustCompile[*testCtx](_taskExit))
	_guardFuncBeforeTime = execBool(cc.MustCompile[*testCtx](_guardBeforeTime))

	// 健康相关守卫
	_guardHealthBelow30 = "int health; health < 30"
	_guardHealthBelow80 = "int health; health < 80"

	// 敌人相关守卫
	_guardEnemyInSight  = "int enemy_distance; enemy_distance > 0 && enemy_distance <= 20"
	_guardEnemyTooClose = "int enemy_distance; enemy_distance > 0 && enemy_distance <= 3"
	_guardNoEnemy       = "int enemy_distance; enemy_distance <= 0"

	// 状态相关守卫
	_guardNotInCombat   = "bool in_combat; !in_combat"
	_guardInCombat      = "bool in_combat; in_combat"
	_guardAtDestination = "int distance_to_dest; distance_to_dest <= 1"

	// 技能相关守卫
	_guardSkillReady = "int skill_cooldown; skill_cooldown <= 0"
	_guardHasMana    = "int mana; mana >= 30"

	// 编译守卫函数
	_guardFuncHealthBelow30 = execBool(cc.MustCompile[*testCtx](_guardHealthBelow30))
	_guardFuncHealthBelow80 = execBool(cc.MustCompile[*testCtx](_guardHealthBelow80))
	_guardFuncEnemyInSight  = execBool(cc.MustCompile[*testCtx](_guardEnemyInSight))
	_guardFuncEnemyTooClose = execBool(cc.MustCompile[*testCtx](_guardEnemyTooClose))
	_guardFuncNoEnemy       = execBool(cc.MustCompile[*testCtx](_guardNoEnemy))
	_guardFuncNotInCombat   = execBool(cc.MustCompile[*testCtx](_guardNotInCombat))
	_guardFuncInCombat      = execBool(cc.MustCompile[*testCtx](_guardInCombat))
	_guardFuncAtDestination = execBool(cc.MustCompile[*testCtx](_guardAtDestination))
	_guardFuncSkillReady    = execBool(cc.MustCompile[*testCtx](_guardSkillReady))
	_guardFuncHasMana       = execBool(cc.MustCompile[*testCtx](_guardHasMana))
)

func execBool(f func(*testCtx) (lib.Field, error)) func(*testCtx) bool {
	return func(ctx *testCtx) bool {
		v, e := f(ctx)
		if e != nil {
			fmt.Println("execBool error", e)
			return false
		}
		si, ok := v.Bool()
		return si && ok
	}
}

func execTask(f func(*testCtx) (lib.Field, error)) func(*testCtx) TaskStatus {
	return func(ctx *testCtx) TaskStatus {
		v, e := f(ctx)
		if e != nil {
			fmt.Println("execTask error", e)
			return TaskFail
		}
		si, ok := v.Int64()
		if !ok {
			return TaskFail
		}
		return TaskStatus(si)
	}
}

func exec(f func(*testCtx) (lib.Field, error)) func(*testCtx) {
	return func(ctx *testCtx) {
		_, e := f(ctx)
		if e != nil {
			fmt.Println("exec error", e)
		}
	}
}

// waitTask 支持等待的任务
type waitTask struct {
	done, cancel   bool
	interruptEvent int32 // 支持通过事件打断，0表示不支持打断
}

func (w *waitTask) Execute(c *testCtx) TaskStatus {
	return _taskFuncExec(c)
}

func (w *waitTask) OnComplete(c *testCtx, cancel bool) {
	_taskFuncExit(c)
	w.cancel = cancel
	w.done = true
}

func (w *waitTask) OnEvent(c *testCtx, e *testEvent) TaskStatus {
	// 支持通过特定事件打断
	if w.interruptEvent > 0 && e.Kind() == w.interruptEvent {
		c.Set("interrupted", lib.Bool(true))
		return TaskSuccess
	}
	return TaskNew
}

func newWaitTaskCreator(wait int64) TaskCreator[*testCtx, *testEvent] {
	return func(ctx *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		ctx.Set("wait", lib.Int64(wait))
		_taskFuncEnter(ctx)
		return &waitTask{}, true
	}
}

func newInterruptibleWaitTaskCreator(wait int64, interruptEvent int32) TaskCreator[*testCtx, *testEvent] {
	return func(ctx *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		ctx.Set("wait", lib.Int64(wait))
		_taskFuncEnter(ctx)
		return &waitTask{interruptEvent: interruptEvent}, true
	}
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

func (t *testTask) OnComplete(c *testCtx, cancel bool) {
	t.canceled = cancel
}

func (t *testTask) OnEvent(c *testCtx, e *testEvent) TaskStatus {
	if t.eventResult != TaskNew {
		return t.eventResult
	}
	return TaskNew
}

func newTestTaskCreator(name string, result TaskStatus) TaskCreator[*testCtx, *testEvent] {
	return func(c *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		return &testTask{
			name:   name,
			result: result,
		}, true
	}
}

func newTestCtx() *testCtx {
	return &testCtx{
		bb:   make(map[string]lib.Field),
		time: 0,
	}
}

func successGuard(c *testCtx) bool {
	return true
}

func failGuard(c *testCtx) bool {
	return false
}

// 1. 各种 node 类型的Execute测试
func TestBasicNodeTypes(t *testing.T) {
	t.Run("Task Node Success", func(t *testing.T) {
		ctx := newTestCtx()
		node := NewTask(successGuard, newTestTaskCreator("success", TaskSuccess))
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(node)

		result := root.Execute(ctx)
		assert.Equal(t, TaskSuccess, result)
	})

	t.Run("Task Node Fail", func(t *testing.T) {
		ctx := newTestCtx()
		node := NewTask(successGuard, newTestTaskCreator("fail", TaskFail))
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(node)

		result := root.Execute(ctx)
		assert.Equal(t, TaskFail, result)
	})

	t.Run("Guard Success", func(t *testing.T) {
		ctx := newTestCtx()
		node := NewGuard[*testCtx, *testEvent](successGuard)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(node)

		result := root.Execute(ctx)
		assert.Equal(t, TaskSuccess, result)
	})

	t.Run("Guard Fail", func(t *testing.T) {
		ctx := newTestCtx()
		node := NewGuard[*testCtx, *testEvent](failGuard)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(node)

		result := root.Execute(ctx)
		assert.Equal(t, TaskFail, result)
	})

	t.Run("Sequence All Success", func(t *testing.T) {
		ctx := newTestCtx()
		seq := NewSequence(successGuard, false,
			NewTask(successGuard, newTestTaskCreator("task1", TaskSuccess)),
			NewTask(successGuard, newTestTaskCreator("task2", TaskSuccess)),
			NewTask(successGuard, newTestTaskCreator("task3", TaskSuccess)),
		)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(seq)

		result := root.Execute(ctx)
		assert.Equal(t, TaskSuccess, result)
	})

	t.Run("Sequence One Fail", func(t *testing.T) {
		ctx := newTestCtx()
		seq := NewSequence(successGuard, false,
			NewTask(successGuard, newTestTaskCreator("task1", TaskSuccess)),
			NewTask(successGuard, newTestTaskCreator("task2", TaskFail)),
			NewTask(successGuard, newTestTaskCreator("task3", TaskSuccess)),
		)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(seq)

		result := root.Execute(ctx)
		assert.Equal(t, TaskFail, result)
	})

	t.Run("Sequence All Success", func(t *testing.T) {
		ctx := newTestCtx()
		seq := NewSequence(successGuard, false,
			NewTask(successGuard, newTestTaskCreator("task1", TaskSuccess)),
			NewTask(successGuard, newTestTaskCreator("task2", TaskSuccess)),
		)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(seq)

		result := root.Execute(ctx)
		assert.Equal(t, TaskSuccess, result)
	})

	t.Run("Selector First Success", func(t *testing.T) {
		ctx := newTestCtx()
		sel := NewSelector(successGuard, false,
			NewTask(successGuard, newTestTaskCreator("task1", TaskSuccess)),
			NewTask(successGuard, newTestTaskCreator("task2", TaskFail)),
		)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(sel)

		result := root.Execute(ctx)
		assert.Equal(t, TaskSuccess, result)
	})

	t.Run("Selector All Fail", func(t *testing.T) {
		ctx := newTestCtx()
		sel := NewSelector(successGuard, false,
			NewTask(successGuard, newTestTaskCreator("task1", TaskFail)),
			NewTask(successGuard, newTestTaskCreator("task2", TaskFail)),
		)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(sel)

		result := root.Execute(ctx)
		assert.Equal(t, TaskFail, result)
	})

	t.Run("Parallel All Success", func(t *testing.T) {
		ctx := newTestCtx()
		parallel := NewParallel(successGuard, MatchAll, 3,
			NewTask(successGuard, newTestTaskCreator("task1", TaskSuccess)),
			NewTask(successGuard, newTestTaskCreator("task2", TaskSuccess)),
			NewTask(successGuard, newTestTaskCreator("task3", TaskSuccess)),
		)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(parallel)

		result := root.Execute(ctx)
		assert.Equal(t, TaskSuccess, result)
	})

	t.Run("Parallel One Success Required", func(t *testing.T) {
		ctx := newTestCtx()
		parallel := NewParallel(successGuard, MatchSuccess, 1,
			NewTask(successGuard, newTestTaskCreator("task1", TaskFail)),
			NewTask(successGuard, newTestTaskCreator("task2", TaskSuccess)),
			NewTask(successGuard, newTestTaskCreator("task3", TaskFail)),
		)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(parallel)

		result := root.Execute(ctx)
		assert.Equal(t, TaskSuccess, result)
	})

	t.Run("Inverter Success to Fail", func(t *testing.T) {
		ctx := newTestCtx()
		inverter := NewInverter(successGuard,
			NewTask(successGuard, newTestTaskCreator("task", TaskSuccess)),
		)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(inverter)

		result := root.Execute(ctx)
		assert.Equal(t, TaskFail, result)
	})

	t.Run("Inverter Fail to Success", func(t *testing.T) {
		ctx := newTestCtx()
		inverter := NewInverter(successGuard,
			NewTask(successGuard, newTestTaskCreator("task", TaskFail)),
		)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(inverter)

		result := root.Execute(ctx)
		assert.Equal(t, TaskSuccess, result)
	})

	t.Run("Repeat Until Success", func(t *testing.T) {
		ctx := newTestCtx()

		// 创建一个会失败2次然后成功的任务
		var attemptCount int
		taskCreator := func(ctx *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
			attemptCount++
			if attemptCount <= 2 {
				return &testTask{name: "retry", result: TaskFail}, true
			}
			return &testTask{name: "success", result: TaskSuccess}, true
		}

		repeat := NewRepeatUntilNSuccess(successGuard, 1, 5, // 需要1次成功，最多尝试5次
			NewTask(successGuard, taskCreator),
		)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(repeat)

		result := root.Execute(ctx)
		assert.Equal(t, TaskSuccess, result)
		assert.Equal(t, 3, attemptCount) // 验证确实尝试了3次
	})

	t.Run("Repeat Max Loop Exceeded", func(t *testing.T) {
		ctx := newTestCtx()

		// 创建一个总是失败的任务
		repeat := NewRepeatUntilNSuccess(successGuard, 1, 3, // 需要1次成功，最多尝试3次
			NewTask(successGuard, newTestTaskCreator("fail", TaskFail)),
		)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(repeat)

		result := root.Execute(ctx)
		assert.Equal(t, TaskFail, result) // 应该因为超过最大循环次数而失败
	})

	t.Run("AlwaysGuard Success", func(t *testing.T) {
		ctx := newTestCtx()
		alwaysGuard := NewAlwaysGuard(successGuard,
			NewTask(successGuard, newTestTaskCreator("task", TaskSuccess)),
		)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(alwaysGuard)

		result := root.Execute(ctx)
		assert.Equal(t, TaskSuccess, result)
	})

	t.Run("AlwaysGuard Fail", func(t *testing.T) {
		ctx := newTestCtx()
		alwaysGuard := NewAlwaysGuard(failGuard,
			NewTask(successGuard, newTestTaskCreator("task", TaskSuccess)),
		)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(alwaysGuard)

		result := root.Execute(ctx)
		assert.Equal(t, TaskFail, result)
	})

	t.Run("PostGuard Success", func(t *testing.T) {
		ctx := newTestCtx()
		postGuard := NewPostGuard(successGuard,
			NewTask(successGuard, newTestTaskCreator("task", TaskSuccess)),
		)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(postGuard)

		result := root.Execute(ctx)
		assert.Equal(t, TaskSuccess, result)
	})

	t.Run("PostGuard Fail", func(t *testing.T) {
		ctx := newTestCtx()
		postGuard := NewPostGuard(failGuard,
			NewTask(successGuard, newTestTaskCreator("task", TaskSuccess)),
		)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(postGuard)

		result := root.Execute(ctx)
		assert.Equal(t, TaskFail, result)
	})
}

// 2. 通过 waitTask beforeGuard 编写 2-3 层有运行中状态的行为树的测试
func TestRunningStateBehaviorTree(t *testing.T) {
	t.Run("Simple Wait Task", func(t *testing.T) {
		ctx := newTestCtx()
		n1 := NewTask(successGuard, newWaitTaskCreator(5))
		var r Root[*testCtx, *testEvent]
		r.SetNode(n1)

		// 第一次执行，应该返回等待5个时间单位
		assert.Equal(t, TaskStatus(5), r.Execute(ctx))

		// 时间推进到2，还需等待3个时间单位
		ctx.time = 2
		assert.Equal(t, TaskStatus(3), r.Execute(ctx))

		// 时间到达5，任务完成
		ctx.time = 5
		assert.Equal(t, TaskSuccess, r.Execute(ctx))
	})

	t.Run("Two Layer Running Tree", func(t *testing.T) {
		ctx := newTestCtx()

		// Sequence(Wait(5), Success)
		seq := NewSequence(successGuard, false,
			NewTask(successGuard, newWaitTaskCreator(5)),
			NewTask(successGuard, newTestTaskCreator("task2", TaskSuccess)),
		)
		var r Root[*testCtx, *testEvent]
		r.SetNode(seq)

		// 第一次执行，等待任务返回5
		assert.Equal(t, TaskStatus(5), r.Execute(ctx))

		// 时间推进但未完成
		ctx.time = 3
		assert.Equal(t, TaskStatus(2), r.Execute(ctx))

		// 等待任务完成，继续执行下一个任务
		ctx.time = 5
		assert.Equal(t, TaskSuccess, r.Execute(ctx))
	})

	t.Run("Three Layer Running Tree", func(t *testing.T) {
		ctx := newTestCtx()

		// Selector(Sequence(Wait(3), Success), Wait(10))
		sel := NewSelector(successGuard, false,
			NewSequence(successGuard, false,
				NewTask(successGuard, newWaitTaskCreator(3)),
				NewTask(successGuard, newTestTaskCreator("task1", TaskSuccess)),
			),
			NewTask(successGuard, newWaitTaskCreator(10)),
		)
		var r Root[*testCtx, *testEvent]
		r.SetNode(sel)

		// 第一次执行，内层等待任务返回3
		assert.Equal(t, TaskStatus(3), r.Execute(ctx))

		// 时间推进
		ctx.time = 2
		assert.Equal(t, TaskStatus(1), r.Execute(ctx))

		// 第一个sequence完成，整个selector成功
		ctx.time = 3
		assert.Equal(t, TaskSuccess, r.Execute(ctx))
	})

	t.Run("AlwaysGuard with Wait Task", func(t *testing.T) {
		ctx := newTestCtx()

		// 使用beforeTime守卫
		n1 := NewTask(successGuard, newWaitTaskCreator(5))
		n2 := NewAlwaysGuard(_guardFuncBeforeTime, n1)
		var r Root[*testCtx, *testEvent]
		r.SetNode(n2)

		// 设置p=-1，守卫应该通过
		ctx.Set("p", lib.Int64(-1))
		assert.Equal(t, TaskStatus(5), r.Execute(ctx))

		// 时间推进
		ctx.time = 2
		assert.Equal(t, TaskStatus(3), r.Execute(ctx))

		// 时间到3，设置p=3，守卫应该失败
		ctx.time = 3
		ctx.Set("p", lib.Int64(3))
		assert.Equal(t, TaskFail, r.Execute(ctx))

		// 重新设置p=-1，任务应该重新开始
		ctx.time = 4
		ctx.Set("p", lib.Int64(-1))
		assert.Equal(t, TaskStatus(5), r.Execute(ctx))
	})
}

// 3. 编写可能导致 Cancel 的行为树
func TestCancelBehavior(t *testing.T) {
	t.Run("AlwaysGuard Cancel", func(t *testing.T) {
		ctx := newTestCtx()

		// 创建一个可以被取消的等待任务
		waitTaskCreator := func(ctx *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
			ctx.Set("wait", lib.Int64(10))
			ctx.Set("task_started", lib.Bool(true))
			_taskFuncEnter(ctx)
			return &waitTask{}, true
		}

		n1 := NewTask(successGuard, waitTaskCreator)
		n2 := NewAlwaysGuard(_guardFuncBeforeTime, n1)
		var r Root[*testCtx, *testEvent]
		r.SetNode(n2)

		// 设置守卫条件，任务开始
		ctx.Set("p", lib.Int64(-1))
		result := r.Execute(ctx)
		assert.Equal(t, TaskStatus(10), result)
		started, _ := ctx.bb["task_started"].Bool()
		assert.True(t, started)

		// 时间推进，但守卫条件改变，导致取消
		ctx.time = 5
		ctx.Set("p", lib.Int64(3)) // 现在时间5>3，守卫失败
		result = r.Execute(ctx)
		assert.Equal(t, TaskFail, result)
	})

	t.Run("Parallel Early Exit Cancel", func(t *testing.T) {
		ctx := newTestCtx()

		// 创建可以跟踪取消状态的任务
		var task1 *testTask

		task1Creator := func(ctx *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
			task1 = &testTask{name: "task1", result: TaskSuccess}
			return task1, true
		}

		task2Creator := func(ctx *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
			ctx.Set("wait", lib.Int64(10))
			_taskFuncEnter(ctx)
			return &waitTask{}, true
		}

		task3Creator := func(ctx *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
			ctx.Set("wait", lib.Int64(15))
			_taskFuncEnter(ctx)
			return &waitTask{}, true
		}

		// 并行执行，只需要1个成功就退出
		parallel := NewParallel(successGuard, MatchSuccess, 1,
			NewTask(successGuard, task1Creator),
			NewTask(successGuard, task2Creator),
			NewTask(successGuard, task3Creator),
		)

		var r Root[*testCtx, *testEvent]
		r.SetNode(parallel)

		// 执行，task1立即成功，其他任务应该被取消
		result := r.Execute(ctx)
		assert.Equal(t, TaskSuccess, result)

		// 验证task1执行了
		assert.True(t, task1.executed)

		// 验证其他任务的清理状态 - 通过检查blackboard中的d值是否被重置
		d, exists := ctx.Get("d")
		if exists {
			dVal, _ := d.Int64()
			assert.Equal(t, int64(-1), dVal) // OnComplete 应该重置 d = -1
		}
	})
}

// 4. 让 waitTask 支持 OnEvent 打断行为树
func TestEventInterrupt(t *testing.T) {
	t.Run("Event_Interrupt_Wait_Task", func(t *testing.T) {
		ctx := newTestCtx()
		ctx.time = 0

		// 创建一个可被事件打断的等待任务
		waitTaskCreator := newInterruptibleWaitTaskCreator(10, 1) // 等待10秒，事件类型1可以打断
		node := NewTask(successGuard, waitTaskCreator)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(node)

		// 第一次执行 - 应该返回剩余等待时间10
		result := root.Execute(ctx)
		assert.Equal(t, TaskStatus(10), result)

		// 发送打断事件
		event := &testEvent{kind: 1, data: "interrupt"}
		eventResult := root.OnEvent(ctx, event)

		// 事件处理：waitTask返回TaskSuccess被弹出，然后r.Execute(c)重新开始
		// 重新开始会创建新的waitTask，所以返回等待时间10
		assert.Equal(t, TaskSuccess, eventResult)

		// 验证中断标记已设置
		interrupted, exists := ctx.Get("interrupted")
		assert.True(t, exists)
		interruptedVal, _ := interrupted.Bool()
		assert.True(t, interruptedVal)
	})

	t.Run("Event_No_Interrupt", func(t *testing.T) {
		ctx := newTestCtx()
		ctx.time = 0

		waitTaskCreator := newInterruptibleWaitTaskCreator(10, 1)
		node := NewTask(successGuard, waitTaskCreator)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(node)

		result := root.Execute(ctx)
		assert.Equal(t, TaskStatus(10), result)

		// 发送不匹配的事件
		event := &testEvent{kind: 2, data: "no interrupt"}
		eventResult := root.OnEvent(ctx, event)

		// 事件不匹配，waitTask.OnEvent返回TaskNew，不处理事件
		assert.Equal(t, TaskNew, eventResult)
	})

	t.Run("Complex_Tree_Event_Interrupt", func(t *testing.T) {
		ctx := newTestCtx()
		ctx.time = 0

		// 创建一个序列：成功任务 -> 可打断的等待任务 -> 成功任务
		seq := NewSequence(successGuard, false,
			NewTask(successGuard, newTestTaskCreator("task1", TaskSuccess)),
			NewTask(successGuard, newInterruptibleWaitTaskCreator(10, 1)),
			NewTask(successGuard, newTestTaskCreator("task3", TaskSuccess)),
		)
		root := &Root[*testCtx, *testEvent]{}
		root.SetNode(seq)

		// 第一次执行 - task1成功，开始执行waitTask，返回等待时间10
		result := root.Execute(ctx)
		assert.Equal(t, TaskStatus(10), result)

		// 发送打断事件
		event := &testEvent{kind: 1, data: "interrupt"}
		eventResult := root.OnEvent(ctx, event)

		assert.Equal(t, TaskSuccess, eventResult)

		// 验证中断标记已设置
		interrupted, exists := ctx.Get("interrupted")
		assert.True(t, exists)
		interruptedVal, _ := interrupted.Bool()
		assert.True(t, interruptedVal)
	})
}

// 5. 编写一个复杂的 7-8 层的NPC战士行为树
func TestComplexBehaviorTree(t *testing.T) {
	ctx := newTestCtx()
	setupNPCState(ctx)

	// 构建复杂的NPC战士行为树
	npcWarriorTree := NewSelector(successGuard, false,
		// Layer 2: 生命值过低时强制撤退 - 最高优先级
		NewSequence(successGuard, false,
			NewGuard[*testCtx, *testEvent](_guardFuncHealthBelow30),
			NewTask(successGuard, newRetreatTaskCreator()),
		),

		// Layer 2: 战斗序列 - 第二优先级
		NewSequence(successGuard, false,
			NewGuard[*testCtx, *testEvent](_guardFuncInCombat),
			NewSelector(successGuard, false,
				// Layer 4: 近距离战斗序列
				NewSequence(successGuard, false,
					NewGuard[*testCtx, *testEvent](_guardFuncEnemyTooClose),
					NewSelector(successGuard, false,
						// Layer 6: 技能攻击序列
						NewSequence(successGuard, false,
							NewGuard[*testCtx, *testEvent](_guardFuncSkillReady),
							NewGuard[*testCtx, *testEvent](_guardFuncHasMana),
							NewTask(successGuard, newSkillAttackTaskCreator()),
						),
						// Layer 6: 普通攻击
						NewTask(successGuard, newAttackTaskCreator()),
					),
				),
				// Layer 4: 追击序列
				NewSequence(successGuard, false,
					NewGuard[*testCtx, *testEvent](_guardFuncEnemyInSight),
					NewTask(successGuard, newChaseTaskCreator()),
				),
			),
		),

		// Layer 2: 生命值恢复序列 - 第三优先级
		NewSequence(successGuard, false,
			NewGuard[*testCtx, *testEvent](_guardFuncNotInCombat),
			NewGuard[*testCtx, *testEvent](_guardFuncHealthBelow80),
			NewTask(successGuard, newHealTaskCreator()),
		),

		// Layer 2: 巡逻序列 - 最低优先级
		NewSequence(successGuard, false,
			NewGuard[*testCtx, *testEvent](_guardFuncNotInCombat),
			NewGuard[*testCtx, *testEvent](_guardFuncNoEnemy),
			NewSelector(successGuard, false,
				// Layer 4: 到达目标点序列
				NewSequence(successGuard, false,
					NewGuard[*testCtx, *testEvent](_guardFuncAtDestination),
					NewTask(successGuard, newSearchTaskCreator()),
				),
				// Layer 4: 巡逻移动
				NewTask(successGuard, newPatrolTaskCreator()),
			),
		),
	)

	var r Root[*testCtx, *testEvent]
	r.SetNode(npcWarriorTree)

	// 测试各种场景
	t.Run("Scenario 1: 正常巡逻状态", func(t *testing.T) {
		r = Root[*testCtx, *testEvent]{}
		r.SetNode(npcWarriorTree)
		ctx = newTestCtx()
		setupNPCState(ctx)

		// 确保条件满足巡逻：不在战斗中，没有敌人
		ctx.Set("in_combat", lib.Bool(false))
		ctx.Set("enemy_distance", lib.Int64(0))
		ctx.Set("has_target", lib.Bool(false))

		// 第一次执行应该进入巡逻
		result := r.Execute(ctx)
		assert.Equal(t, TaskStatus(3), result) // 巡逻等待时间

		action, _ := ctx.Get("action")
		actionStr, _ := lib.TakeAny[string](&action)
		assert.Equal(t, "patrolling", actionStr)
	})

	t.Run("Scenario 2: 发现敌人并追击", func(t *testing.T) {
		r = Root[*testCtx, *testEvent]{}
		r.SetNode(npcWarriorTree)
		ctx = newTestCtx()
		setupNPCState(ctx)

		// 设置发现敌人的状态 - 敌人在视野内但不是很近
		ctx.Set("enemy_distance", lib.Int64(15)) // 敌人在视野内
		ctx.Set("in_combat", lib.Bool(true))
		ctx.Set("has_target", lib.Bool(true))

		result := r.Execute(ctx)
		assert.Equal(t, TaskStatus(2), result) // 追击等待时间

		action, _ := ctx.Get("action")
		actionStr, _ := lib.TakeAny[string](&action)
		assert.Equal(t, "chasing", actionStr)
	})
}

// 基准测试
func BenchmarkBehaviorTreeExecution(b *testing.B) {
	ctx := newTestCtx()

	// 构建一个中等复杂度的行为树用于基准测试
	tree := NewSequence(successGuard, false,
		NewSelector(successGuard, false,
			NewTask(successGuard, newTestTaskCreator("task1", TaskFail)),
			NewTask(successGuard, newTestTaskCreator("task2", TaskSuccess)),
		),
		NewTask(successGuard, newTestTaskCreator("task3", TaskSuccess)),
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

// 测试节点验证
func TestNodeValidation(t *testing.T) {
	// 测试空子节点的Sequence应该失败验证
	invalidSeq := &Node[*testCtx, *testEvent]{
		Type:     TypeSequenceBranch,
		Children: []*Node[*testCtx, *testEvent]{},
	}

	err := invalidSeq.Check()
	assert.Error(t, err)

	// 正常的Sequence应该通过验证
	validSeq := NewSequence(successGuard, false,
		NewTask(successGuard, newTestTaskCreator("task", TaskSuccess)),
	)

	err = validSeq.Check()
	assert.NoError(t, err)
}

// 测试计数模式
func TestCountModes(t *testing.T) {
	// MatchSuccess 模式
	assert.True(t, CountMode(MatchSuccess).Count(true))
	assert.False(t, CountMode(MatchSuccess).Count(false))

	// MatchFail 模式
	assert.False(t, CountMode(MatchFail).Count(true))
	assert.True(t, CountMode(MatchFail).Count(false))

	// MatchAll 模式
	assert.True(t, CountMode(MatchAll).Count(true))
	assert.True(t, CountMode(MatchAll).Count(false))
}

// 辅助函数：设置NPC初始状态
func setupNPCState(ctx *testCtx) {
	ctx.Set("health", lib.Int64(100))
	ctx.Set("mana", lib.Int64(100))
	ctx.Set("enemy_distance", lib.Int64(0))
	ctx.Set("in_combat", lib.Bool(false))
	ctx.Set("has_target", lib.Bool(false))
	ctx.Set("distance_to_dest", lib.Int64(5))
	ctx.Set("skill_cooldown", lib.Int64(0))
}

// 创建各种NPC行为任务
func newPatrolTaskCreator() TaskCreator[*testCtx, *testEvent] {
	return func(ctx *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		// 模拟巡逻移动，需要一定时间
		ctx.Set("wait", lib.Int64(3))
		ctx.Set("action", lib.Any("patrolling"))
		_taskFuncEnter(ctx)
		return &waitTask{}, true
	}
}

func newChaseTaskCreator() TaskCreator[*testCtx, *testEvent] {
	return func(ctx *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		// 模拟追击敌人
		ctx.Set("wait", lib.Int64(2))
		ctx.Set("action", lib.Any("chasing"))
		_taskFuncEnter(ctx)
		return &waitTask{}, true
	}
}

func newAttackTaskCreator() TaskCreator[*testCtx, *testEvent] {
	return func(ctx *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		// 模拟攻击动作
		ctx.Set("wait", lib.Int64(1))
		ctx.Set("action", lib.Any("attacking"))
		_taskFuncEnter(ctx)
		return &waitTask{interruptEvent: 2}, true // 可被反击事件打断
	}
}

func newSkillAttackTaskCreator() TaskCreator[*testCtx, *testEvent] {
	return func(ctx *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		// 模拟技能攻击
		ctx.Set("wait", lib.Int64(2))
		ctx.Set("action", lib.Any("skill_attack"))
		ctx.Set("skill_cooldown", lib.Int64(10))            // 设置技能冷却
		ctx.Set("mana", lib.Int64(ctx.GetInt64("mana")-30)) // 消耗法力
		_taskFuncEnter(ctx)
		return &waitTask{}, true
	}
}

func newHealTaskCreator() TaskCreator[*testCtx, *testEvent] {
	return func(ctx *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		// 模拟治疗
		ctx.Set("wait", lib.Int64(3))
		ctx.Set("action", lib.Any("healing"))
		currentHealth := ctx.GetInt64("health")
		ctx.Set("health", lib.Int64(currentHealth+30)) // 恢复生命值
		_taskFuncEnter(ctx)
		return &waitTask{}, true
	}
}

func newRetreatTaskCreator() TaskCreator[*testCtx, *testEvent] {
	return func(ctx *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		// 模拟撤退
		ctx.Set("wait", lib.Int64(2))
		ctx.Set("action", lib.Any("retreating"))
		ctx.Set("in_combat", lib.Bool(false)) // 脱离战斗
		_taskFuncEnter(ctx)
		return &waitTask{}, true
	}
}

func newSearchTaskCreator() TaskCreator[*testCtx, *testEvent] {
	return func(ctx *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		// 模拟搜索敌人
		ctx.Set("wait", lib.Int64(2))
		ctx.Set("action", lib.Any("searching"))
		_taskFuncEnter(ctx)
		return &waitTask{}, true
	}
}

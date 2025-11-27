# MonkeyBT 🐒

**基于手动栈实现的高性能行为树库**

MonkeyBT 是一个创新的 Go 行为树实现，通过手动栈管理避免了传统行为树每次从根节点遍历的性能开销。执行指针在树中上下移动，犹如猴子爬树，因此得名 MonkeyBT。

## ✨ 特性

- **🚀 高性能**：基于手动栈的内联执行，避免无效的树遍历
- **⚡ 实时响应**：支持叶节点实时响应外部事件，无需等待下次更新
- **🔄 智能更新**：支持离散更新机制，根据叶节点预估时间优化更新频率
- **🛡️ 优雅取消**：完善的取消机制，确保资源正确清理
- **🧩 灵活组合**：支持多种节点类型的灵活组合构建复杂行为
- **🔧 泛型支持**：完全的泛型实现，类型安全且易于扩展
- **🎯 最小约束**：Ctx 接口仅要求 `Now()` 方法，用户可自由定义黑板接口

## 📦 安装

```bash
go get github.com/legamerdc/game/bt
```

## 🚀 快速开始

### 基本概念

MonkeyBT 中的核心概念：

- **Context (Ctx)**：行为树的执行上下文，仅要求提供 `Now() int64` 方法
- **Event (EI)**：外部事件抽象，用于实时响应
- **Node**：行为树节点定义
- **Task**：具体的执行任务
- **Blackboard**：可选的数据共享机制，用户可自由定义接口

### 简单示例

```go
package main

import (
    "fmt"
    "github.com/legamerdc/game/bt"
    "github.com/legamerdc/game/bt/blackboard"
    "github.com/legamerdc/game/lib"
)

// 实现自定义上下文 - 只需实现 Now() 方法
// 黑板是可选的，用户可以根据需要自由设计
type GameContext struct {
    time  int64
    board *blackboard.Blackboard
}

func (c *GameContext) Now() int64 { return c.time }

// 实现自定义事件
type GameEvent struct{ kind int32 }
func (e GameEvent) Kind() int32 { return e.kind }

// 实现一个简单的攻击任务
type AttackTask struct{}

func (t *AttackTask) Execute(c *GameContext) bt.TaskStatus {
    fmt.Println("执行攻击")
    return bt.TaskSuccess
}

func (t *AttackTask) OnComplete(c *GameContext, cancel bool) {
    if cancel {
        fmt.Println("攻击被取消")
    } else {
        fmt.Println("攻击完成")
    }
}

func (t *AttackTask) OnEvent(c *GameContext, e GameEvent) bt.TaskStatus {
    return bt.TaskNew // 不处理事件
}

func main() {
    // 创建行为树
    root := bt.NewSequence[*GameContext, GameEvent](
        nil, // 无前置条件
        false, // 不随机
        bt.NewTask[*GameContext, GameEvent](
            func(c *GameContext) bool { return true }, // 总是通过的guard
            func(c *GameContext) (bt.LeafTaskI[*GameContext, GameEvent], bool) {
                return &AttackTask{}, true
            },
        ),
    )

    // 执行行为树
    var treeRoot bt.Root[*GameContext, GameEvent]
    treeRoot.SetNode(root)
    
    ctx := &GameContext{
        time:  1000,
        board: blackboard.New(),
    }
    status := treeRoot.Execute(ctx)
    fmt.Printf("执行结果: %d\n", status)
}
```

## 📚 核心概念详解

### 最小化的 Ctx 接口

MonkeyBT 的设计哲学是最小化接口约束。`Ctx` 接口仅要求一个方法：

```go
type Ctx interface {
    Now() int64
}
```

这种设计让用户可以自由定义自己的黑板接口，例如：

```go
// 方式一：基于字符串 key 的黑板
type StringKeyBoard interface {
    Get(key string) (lib.Field, bool)
    Set(key string, value lib.Field)
}

// 方式二：基于整数 ID 的高性能黑板
type IntKeyBoard interface {
    Get(id int) lib.Field
    Set(id int, value lib.Field)
}

// 方式三：强类型黑板
type TypedBoard interface {
    GetHealth() int
    SetHealth(v int)
    GetTarget() *Enemy
    SetTarget(e *Enemy)
}
```

### 默认 Blackboard 实现

我们在 `bt/blackboard` 包中提供了一个基于 `map[string]lib.Field` 的默认实现，适合快速原型开发：

```go
import (
    "github.com/legamerdc/game/bt/blackboard"
    "github.com/legamerdc/game/lib"
)

// 创建黑板
bb := blackboard.New()

// 设置值 - 使用 lib 包的类型构造函数
bb.Set("health", lib.Int64(100))
bb.Set("speed", lib.Float64(3.14))
bb.Set("alive", lib.Bool(true))
bb.Set("target", lib.Any(enemy))

// 类型安全的获取
health, ok := bb.GetInt64("health")
speed, ok := bb.GetFloat64("speed")
alive, ok := bb.GetBool("alive")

// 获取 Any 类型中的具体类型
target, ok := blackboard.GetAny[*Enemy](bb, "target")

// 其他操作
bb.Del("target")
bb.Has("health")
bb.Clear()
```

`lib.Field` 是一个高效的值类型，支持以下类型：
- `lib.Int32(v)` / `lib.Int64(v)` - 整数
- `lib.Float32(v)` / `lib.Float64(v)` - 浮点数
- `lib.Bool(v)` - 布尔值
- `lib.Any(v)` - 任意类型

### 行为树内联执行

传统行为树每次更新都需要从根节点遍历到正在运行的叶节点，这在大型行为树中会产生显著的性能开销。MonkeyBT 通过手动栈解决了这个问题：

1. **栈保持**：当叶节点处于 `Running` 状态时，执行栈保持不变
2. **直接访问**：下次更新时直接从栈顶的运行节点开始执行
3. **结构一致**：通过栈的 push/pop 操作保持与递归执行相同的语义

### 实时事件响应

MonkeyBT 支持叶节点实时响应外部事件，无需等待下一次行为树更新：

```go
// 实现事件响应的任务
type EventDrivenTask struct {
    waiting bool
    ctx     *GameContext
}

func (t *EventDrivenTask) Execute(c *GameContext) bt.TaskStatus {
    t.ctx = c
    t.waiting = true
    return bt.TaskRunning // 保持运行状态，等待事件
}

func (t *EventDrivenTask) OnEvent(c *GameContext, e GameEvent) bt.TaskStatus {
    if e.Kind() == 1 && t.waiting {
        t.waiting = false
        return bt.TaskSuccess // 事件处理完成
    }
    return bt.TaskNew // 无法处理该事件
}

func (t *EventDrivenTask) OnComplete(c *GameContext, cancel bool) {
    t.waiting = false
}
```

### 离散更新机制

通过叶节点预估下次执行时间，实现智能的更新调度：

```go
func (t *TimedTask) Execute(c *GameContext) bt.TaskStatus {
    if c.Now() >= t.deadline {
        return bt.TaskSuccess
    }
    // 返回正数表示预估的下次更新时间（时间差）
    return bt.TaskStatus(t.deadline - c.Now())
}

func (t *TimedTask) OnEvent(c *GameContext, e GameEvent) bt.TaskStatus {
    // 也可以在事件处理中返回预估时间
    return bt.TaskStatus(5) // 5个时间单位后再次更新
}
```

## 🏗️ 节点类型

### 装饰器节点

- **NewSuccess/NewFail/NewInverter**：修改子节点执行结果
- **NewRepeatUntilNSuccess**：重复执行直到成功N次
- **NewPostGuard**：子节点执行完成后进行条件检查
- **NewAlwaysGuard**：每次更新都检查条件
- **NewGuard**：简单的条件检查节点

```go
// 创建一个总是成功的装饰器
successNode := bt.NewSuccess(nil, childNode)

// 创建重复执行节点
repeatNode := bt.NewRepeatUntilNSuccess(nil, 3, 10, childNode) // 最多10次，成功3次
```

### 分支节点

- **NewSelector**：选择器，找到第一个成功的子节点
- **NewSelectorN**：选择器，需要N个子节点成功
- **NewSequence**：序列器，所有子节点都要成功
- **NewParallel**：并行执行多个子节点

```go
// 创建选择器
selector := bt.NewSelector(nil, false, task1, task2, task3)

// 创建序列器
sequence := bt.NewSequence(nil, false, task1, task2, task3)

// 创建并行节点
parallel := bt.NewParallel(nil, bt.MatchSuccess, 2, task1, task2, task3) // 需要2个成功
```

### 叶节点

- **NewTask**：用户自定义任务节点
- **NewGuard**：条件检查节点

## 🔧 高级特性

### Guard 条件检查

Guard 是节点的前置条件检查，只有通过检查的节点才会执行：

```go
// 创建带条件的任务
taskWithGuard := bt.NewTask(
    func(c *GameContext) bool {
        // 检查是否有足够的能量
        energy, ok := c.board.GetInt64("energy")
        return ok && energy > 10
    },
    taskCreator,
)
```

### 计数模式

分支节点支持不同的计数模式：

- `MatchSuccess`：只计算成功的子节点
- `MatchFail`：只计算失败的子节点  
- `MatchAll`：计算所有完成的子节点
- `MatchNone`：不进行计数

### 栈重建

某些节点（如 `alwaysGuard`、`joinBranch`）会重建执行栈来管理子树的执行，这使得它们能够：

1. 每次更新都被执行（对于需要持续检查的节点）
2. 同时管理多个子树的执行（对于并行节点）

## 🎯 完整示例：NPC 战士 AI

下面是一个完整的 NPC 战士行为树示例：

```go
package main

import (
    "fmt"
    "github.com/legamerdc/game/bt"
    "github.com/legamerdc/game/bt/blackboard"
    "github.com/legamerdc/game/lib"
)

// 游戏上下文
type GameContext struct {
    time  int64
    board *blackboard.Blackboard
}

func (c *GameContext) Now() int64 { return c.time }

// 游戏事件
type GameEvent struct{ kind int32 }
func (e GameEvent) Kind() int32 { return e.kind }

// 通用任务基类
type BaseTask struct {
    name string
}

func (t *BaseTask) OnComplete(c *GameContext, cancel bool) {
    if cancel {
        fmt.Printf("[%s] 被取消\n", t.name)
    }
}

func (t *BaseTask) OnEvent(c *GameContext, e GameEvent) bt.TaskStatus {
    return bt.TaskNew
}

// 攻击任务
type AttackTask struct{ BaseTask }

func (t *AttackTask) Execute(c *GameContext) bt.TaskStatus {
    v, _ := c.board.Get("target")
    target, _ := lib.TakeAny[string](&v)
    fmt.Printf("攻击目标: %s\n", target)
    return bt.TaskSuccess
}

// 追击任务
type ChaseTask struct{ BaseTask }

func (t *ChaseTask) Execute(c *GameContext) bt.TaskStatus {
    distance, _ := c.board.GetInt64("enemy_distance")
    if distance <= 3 {
        return bt.TaskSuccess
    }
    c.board.Set("enemy_distance", lib.Int64(distance-2))
    return bt.TaskStatus(1) // 1秒后继续
}

// 巡逻任务
type PatrolTask struct{ BaseTask }

func (t *PatrolTask) Execute(c *GameContext) bt.TaskStatus {
    fmt.Println("巡逻中...")
    return bt.TaskStatus(3) // 3秒后再检查
}

// 任务创建器
func newAttackTask(c *GameContext) (bt.LeafTaskI[*GameContext, GameEvent], bool) {
    return &AttackTask{BaseTask{name: "Attack"}}, true
}

func newChaseTask(c *GameContext) (bt.LeafTaskI[*GameContext, GameEvent], bool) {
    return &ChaseTask{BaseTask{name: "Chase"}}, true
}

func newPatrolTask(c *GameContext) (bt.LeafTaskI[*GameContext, GameEvent], bool) {
    return &PatrolTask{BaseTask{name: "Patrol"}}, true
}

// Guard 条件函数
func hasEnemy(c *GameContext) bool {
    distance, ok := c.board.GetInt64("enemy_distance")
    return ok && distance > 0
}

func enemyInRange(c *GameContext) bool {
    distance, ok := c.board.GetInt64("enemy_distance")
    return ok && distance <= 3
}

func main() {
    // 构建行为树
    tree := bt.NewSelector[*GameContext, GameEvent](nil, false,
        // 有敌人时的战斗行为
        bt.NewSequence[*GameContext, GameEvent](nil, false,
            bt.NewGuard[*GameContext, GameEvent](hasEnemy),
            bt.NewSelector[*GameContext, GameEvent](nil, false,
                // 敌人在攻击范围内则攻击
                bt.NewSequence[*GameContext, GameEvent](nil, false,
                    bt.NewGuard[*GameContext, GameEvent](enemyInRange),
                    bt.NewTask[*GameContext, GameEvent](nil, newAttackTask),
                ),
                // 否则追击
                bt.NewTask[*GameContext, GameEvent](nil, newChaseTask),
            ),
        ),
        // 没有敌人时巡逻
        bt.NewTask[*GameContext, GameEvent](nil, newPatrolTask),
    )

    // 创建上下文
    ctx := &GameContext{
        time:  0,
        board: blackboard.New(),
    }
    ctx.board.Set("enemy_distance", lib.Int64(10))
    ctx.board.Set("target", lib.Any("Goblin"))

    // 执行行为树
    var root bt.Root[*GameContext, GameEvent]
    root.SetNode(tree)

    // 模拟多帧更新
    for i := 0; i < 5; i++ {
        fmt.Printf("\n=== 帧 %d ===\n", i)
        status := root.Execute(ctx)
        fmt.Printf("状态: %d\n", status)
        
        if status <= bt.TaskFail {
            break
        }
        ctx.time++
    }
}
```

## 🎯 最佳实践

### 1. 合理使用 Guard

```go
// ✅ 好的实践：Guard 应该是轻量级的检查
func energyGuard(c *GameContext) bool {
    energy, _ := c.board.GetInt64("energy")
    return energy > 0
}

// ❌ 避免：在 Guard 中执行复杂逻辑
func badGuard(c *GameContext) bool {
    // 复杂的计算和副作用应该在 Task 中执行
    calculateComplexValue(c)
    return true
}
```

### 2. 适当使用事件响应

```go
// ✅ 适合事件响应：等待型任务
type WaitTask struct {
    targetTime int64
}

func (t *WaitTask) OnEvent(c *GameContext, e GameEvent) bt.TaskStatus {
    if c.Now() >= t.targetTime {
        return bt.TaskSuccess
    }
    return bt.TaskStatus(t.targetTime - c.Now()) // 返回剩余等待时间
}

// ❌ 不适合：频繁变化的任务
```

### 3. 优化性能

- 将频繁检查的条件放在 Guard 中
- 合理使用并行节点避免不必要的串行执行
- 对于长时间运行的任务，实现 OnEvent 以支持实时响应

## 📖 API 参考

### 核心接口

```go
// Ctx 是行为树的执行上下文接口
// 仅要求提供当前时间，黑板等功能由用户自行扩展
type Ctx interface {
    Now() int64
}

// EI 是事件接口
type EI interface {
    Kind() int32
}

// LeafTaskI 是用户实现的叶节点任务接口
type LeafTaskI[C Ctx, E EI] interface {
    Execute(c C) TaskStatus
    OnComplete(c C, cancel bool)
    OnEvent(C, E) TaskStatus
}
```

### 任务状态

```go
const (
    TaskRunning TaskStatus = 1   // 正在运行（或预估下次更新时间）
    TaskNew     TaskStatus = 0   // 新任务/无法处理
    TaskSuccess TaskStatus = -1  // 执行成功
    TaskFail    TaskStatus = -2  // 执行失败
)
```

### 节点构造函数

```go
// 装饰器节点
func NewSuccess[C Ctx, E EI](g Guard[C], ch *Node[C, E]) *Node[C, E]
func NewFail[C Ctx, E EI](g Guard[C], ch *Node[C, E]) *Node[C, E]
func NewInverter[C Ctx, E EI](g Guard[C], ch *Node[C, E]) *Node[C, E]
func NewRepeatUntilNSuccess[C Ctx, E EI](g Guard[C], require, maxLoop int32, ch *Node[C, E]) *Node[C, E]
func NewPostGuard[C Ctx, E EI](g Guard[C], ch *Node[C, E]) *Node[C, E]
func NewAlwaysGuard[C Ctx, E EI](g Guard[C], ch *Node[C, E]) *Node[C, E]
func NewGuard[C Ctx, E EI](g Guard[C]) *Node[C, E]

// 分支节点
func NewSelector[C Ctx, E EI](g Guard[C], shuffle bool, ch ...*Node[C, E]) *Node[C, E]
func NewSelectorN[C Ctx, E EI](g Guard[C], n int32, shuffle bool, ch ...*Node[C, E]) *Node[C, E]
func NewSequence[C Ctx, E EI](g Guard[C], shuffle bool, ch ...*Node[C, E]) *Node[C, E]
func NewParallel[C Ctx, E EI](g Guard[C], mode CountMode, require int32, ch ...*Node[C, E]) *Node[C, E]

// 叶节点
func NewTask[C Ctx, E EI](g Guard[C], task TaskCreator[C, E]) *Node[C, E]
```

## 🤝 贡献

欢迎提交 Issue 和 Pull Request 来改进 MonkeyBT！

## 📄 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

---

**MonkeyBT** - 让你的 AI 像猴子一样灵活地爬树！ 🐒🌳

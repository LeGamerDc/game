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

## 📦 安装

```bash
go get github.com/legamerdc/game/bt
```

## 🚀 快速开始

### 基本概念

MonkeyBT 中的核心概念：

- **Context (Ctx)**：行为树的执行上下文，提供时间、黑板数据访问等
- **Event (EI)**：外部事件抽象，用于实时响应
- **Node**：行为树节点定义
- **Task**：具体的执行任务

### 简单示例

```go
package main

import (
    "fmt"
    "github.com/legamerdc/game/bt"
    "github.com/legamerdc/game/blackboard"
)

// 实现自定义上下文
type GameContext struct {
    time int64
    board blackboard.Blackboard
}

func (c *GameContext) Now() int64 { return c.time }
func (c *GameContext) Get(key string) (blackboard.Field, bool) { return c.board.Get(key) }
func (c *GameContext) Set(key string, value blackboard.Field) { c.board.Set(key, value) }
func (c *GameContext) Del(key string) { c.board.Del(key) }

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
    
    ctx := &GameContext{time: 1000}
    status := treeRoot.Execute(ctx)
    fmt.Printf("执行结果: %d\n", status)
}
```

## 📚 核心概念详解

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
}

func (t *EventDrivenTask) Execute(c *GameContext) bt.TaskStatus {
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
```

### 离散更新机制

通过叶节点预估下次执行时间，实现智能的更新调度：

```go
func (t *TimedTask) OnEvent(c *GameContext, e GameEvent) bt.TaskStatus {
    // 返回正数表示预估的下次更新时间（秒）
    return bt.TaskStatus(5) // 5秒后再次更新
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
        energy, ok := c.Get("energy")
        return ok && energy.(int) > 10
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

## 🎯 最佳实践

### 1. 合理使用 Guard

```go
// ✅ 好的实践：Guard 应该是轻量级的检查
func energyGuard(c *GameContext) bool {
    energy, _ := c.Get("energy")
    return energy.(int) > 0
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
type Ctx interface {
    Now() int64
    Get(string) (blackboard.Field, bool)
    Set(string, blackboard.Field)
    Del(string)
}

type EI interface {
    Kind() int32
}

type LeafTaskI[C Ctx, E EI] interface {
    Execute(c C) TaskStatus
    OnComplete(c C, cancel bool)
    OnEvent(C, E) TaskStatus
}
```

### 任务状态

```go
const (
    TaskRunning TaskStatus = 1   // 正在运行
    TaskNew     TaskStatus = 0   // 新任务/无法处理
    TaskSuccess TaskStatus = -1  // 执行成功
    TaskFail    TaskStatus = -2  // 执行失败
)
```

## 🤝 贡献

欢迎提交 Issue 和 Pull Request 来改进 MonkeyBT！

## 📄 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

---

**MonkeyBT** - 让你的 AI 像猴子一样灵活地爬树！ 🐒🌳

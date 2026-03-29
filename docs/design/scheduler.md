# Scheduler Design

## 前置约定

- 本设计稿描述 Scheduler 的并发调度模型，不包含具体数据结构实现。
- Logic 接口定义以 `en/world.go` 为唯一权威来源。
- 并行 tick 整体设计见 `docs/design/parallel.md`。
- 设计稿与代码出现矛盾时，以代码为准。

## 设计目标

Scheduler 负责在每个 tick 内调度 Logic 的 Think/Apply 执行，核心职责：

1. **管理 Think 激活**：决定哪些 Logic 在当前 tick 需要 Think（timer 到期、signal 触发、外部输入）
2. **收集与分发 Effect**：Think 产出的 Effect 按目标 owner 分块聚合后交给 Apply
3. **收集与路由 Signal**：Think/Apply 产出的 Signal 路由到目标 inbox，触发后续 Think
4. **自动切换串行/并发模式**：根据工作量选择最优执行策略
5. **防护无限循环**：通过 superstep 轮次 / cascade depth 避免单 tick 内无限级联

## Meta 配置

```go
type ScheduleMeta struct {
    // Think 阶段：frontier 超过此数时启用并发 Think
    ThinkConcurrencyThreshold int // default: 500

    // 并发 worker 数
    Concurrency int // default: 5

    // 并发模式最大 superstep 轮次 / 串行模式最大 cascade depth
    MaxSupersteps int // default: 3

    // tick 时间预算（可选弹性上限）
    TickTimeBudget time.Duration

    // timer wheel 槽位数
    TimerWheelSize int // default: 200

    // effect 分块数（质数，用于 targetRef % BlockSize 分桶）
    BlockSize int // default: 137
}
```

## Tick 生命周期

```
Tick N 开始:
  1. Advance timer wheel → 到期的 logic 加入初始 frontier
  2. Drain deferred signals（上 tick 溢出）→ 目标 logic inbox 更新，加入 frontier
  3. Drain external inputs → 目标 logic inbox 更新，加入 frontier
  4. frontier = 所有 inbox 非空或被 timer 唤醒的 logic

  根据 frontier.Len() 选择执行模式：
  - >= ThinkConcurrencyThreshold → 并发模式
  - <  ThinkConcurrencyThreshold → 串行模式

  执行模式处理（详见下方各节）

Tick N 结束:
  - 合并所有 worker 的 timer 注册到全局 timer wheel
  - 如有溢出的 signal → defer to tick N+1
```

---

## 并发模式

### 总体流程

```
round = 0
while frontier.Len() > 0 && round < MaxSupersteps:

    ─── Think Phase (parallel) ───────────────────────
    将 frontier 按 RefId 分配到 WorkerCount 个 worker
    每个 worker 并行执行分配到的 logic.Think
    ─── barrier ──────────────────────────────────────

    ─── Apply Phase (parallel by block) ──────────────
    按 blockId 分配到 worker，每个 block 跨所有
    Think worker 收集 effect 并按 targetRef 聚合后 Apply
    World（RefWorld）作为普通 block 成员参与，内部串行
    ─── barrier ──────────────────────────────────────

    ─── Signal Routing (single-threaded) ─────────────
    合并所有 worker 的 signalBuf（Think + Apply 两阶段）
    路由到目标 logic inbox
    frontier = { logic | inbox 非空 }（IndexMap 去重）
    ──────────────────────────────────────────────────

    round++

    // 每轮独立判断模式切换
    if frontier.Len() < ThinkConcurrencyThreshold:
        切换到串行模式处理剩余 frontier
        串行 cascade depth 预算 = MaxSupersteps - round
        break

// 溢出处理
if frontier 仍非空: defer to tick N+1, log warning
```

### Think Phase 细节

**Worker 分配策略**：Logic 按 RefId 稳定分配到 worker（`RefId % WorkerCount`）。同一 Logic 在同一 tick 的不同 superstep 轮次中始终落在同一 worker。

**每个 worker 维护本地收集器（无锁、无竞争）**：

| 收集器 | 结构 | 说明 |
|--------|------|------|
| `effectBlocks[BlockSize]` | 按 `targetRef % BlockSize` 分块的 effect 列表 | 每个 block 内存放 `(targetRef, effect)` 对 |
| `signalBuf` | signal 列表 | 存放 `(targetRef, signal)` 对 |
| `timerRegs` | `map[logicID]delay` | 同一 logic 后注册覆盖先注册 |

**Think 执行**：

- `logic.Think(ctx, inbox)` 被调用
- `ctx.Publish(targetRef, effect)` → 写入当前 worker 的 `effectBlocks[targetRef % BlockSize]`
- `ctx.Emit(targetRef, signal)` → 写入当前 worker 的 `signalBuf`
- Think 返回 `delay > 0` → 写入当前 worker 的 `timerRegs[logicID] = delay`

**为什么 timerRegs 可以是 worker 本地的**：因为同一 Logic 始终在同一 worker 上执行 Think，即使跨 superstep 轮次也不会换 worker。所以 timerRegs 天然无冲突，tick 结束后统一合并到全局 timer wheel 即可。

### Apply Phase 细节

**无 Merge Phase**：不需要单独的 merge 步骤。Apply worker 直接跨所有 Think worker 读取对应 block 的 effect。

**Block 分配策略**：每个 blockId 分配到一个 Apply worker（如 `blockId % WorkerCount`）。

**处理流程**（对于 block B）：

1. 遍历所有 Think worker 的 `effectBlocks[B]`，按 `targetRef` 聚合
2. 对每个 target 调用 `target.Apply(commitCtx, effects)`
3. Apply 产出的 signal 写入 Apply worker 本地 `signalBuf`
4. 无 effect 的 block 直接跳过

**World Effect**：`RefWorld` 按 `RefWorld % BlockSize` 落入某个 block，作为该 block 的一个普通 target 参与 Apply。World 内部串行处理多个 world effect。因为 world 只有一个 owner（`RefWorld`），不存在并发问题。World Apply 与其他 entity Apply 天然并行。

**World Apply 并行安全性前提**：

- `CommitCtx.World` 是 frozen snapshot，非 live reference
- 每个 Apply 有独立的 signal 输出 buffer，无共享可变 signal 缓冲
- Despawn 只标记 registry（world-owned），不碰 entity public state
- 所有 Apply 的写入目标集合互不重叠（ownership 保证）

### Signal Routing 细节

barrier 后单线程执行：

1. 合并所有 worker 的 `signalBuf`（Think 阶段产出 + Apply 阶段产出）
2. 按 `targetRef` 路由到目标 logic 的 inbox
3. 构建下一轮 frontier：所有 inbox 非空的 logic（IndexMap 去重）

---

## 串行模式

### 核心特征

串行模式没有 superstep 概念，也不存在 frontier 管理（push to frontier）。所有 signal 和 effect 当场处理。通过 cascade depth 控制递归深度，避免无限级联。

### 执行模型

```
for each logic in initial_frontier:
    serial_cascade(logic, depth=0, maxDepth)

func serial_cascade(logic, depth, maxDepth):
    results = logic.Think(ctx, logic.inbox)

    // 立即 Apply
    for targetRef, effects in groupByTarget(results.effects):
        target = getLogic(targetRef)
        applyResults = target.Apply(commitCtx, effects)

        // Apply 产出的 signal → 立即级联
        for signal in applyResults.signals:
            signal.target.inbox.append(signal)
            if depth + 1 < maxDepth:
                serial_cascade(signal.target, depth + 1, maxDepth)
            else:
                defer signal.target to next tick

    // Think 产出的 signal → 立即级联
    for signal in results.signals:
        signal.target.inbox.append(signal)
        if depth + 1 < maxDepth:
            serial_cascade(signal.target, depth + 1, maxDepth)
        else:
            defer signal.target to next tick

    // Timer 注册
    if results.delay > 0:
        registerTimer(logic, results.delay)
```

### Cascade Depth 语义

- 初始 frontier 的 Think 在 `depth=0` 执行
- 它们的 signal/effect 触发的 Think 在 `depth=1` 执行
- 以此类推，每层因果链 depth + 1
- `depth >= maxDepth` 时截断：signal 留在 target inbox，target 延迟到下一 tick

Think → effect → Apply → signal → Think 这条链算 depth + 1（不是 +2）。从调度器视角，一次 Think 及其 effect 的 Apply 是一个原子操作单元。

### 接口兼容性

串行模式和并发模式在 Logic 接口层完全兼容。区别仅在于 `ThinkCtx` / `CommitCtx` 中 `Emit` / `Publish` 闭包的实现：

- **并发模式**：写入 worker 本地 buffer，等 barrier 后统一处理
- **串行模式**：立即路由 / 立即 Apply

Logic 实现无需感知当前执行模式。

### 语义差异及其可接受性

串行立即处理与并发模式的执行顺序不同。在游戏场景下这是可接受的：

- 游戏不保证同 tick 内事件的发生顺序
- Apply 按序处理 effect 但不依赖该顺序（任意顺序都是合法的）
- 不存在跨 owner 的同步可见性需求

---

## 模式切换

### 切换判断

每轮 superstep 独立判断执行模式。判断依据为当前 frontier 大小 vs `ThinkConcurrencyThreshold`。

### 切换方向

| 方向 | 时机 | 行为 |
|------|------|------|
| 并发 → 串行 | round N 结束后 frontier 缩小到阈值以下 | 剩余 frontier 用串行模式处理，cascade depth 预算 = `MaxSupersteps - round` |
| 串行 → 并发 | 不建议 | 如果初始 frontier 小（串行），signal 级联后变大，此时 tick 已在串行模式下完成大部分工作。无需切换。 |

---

## Timer Wheel

### 设计

- 单层环形数组，大小 = `TimerWheelSize`（default: 200）
- 每个 slot 是到期 logic ID 的列表
- 每个 logic 同时只有一个活跃 timer（覆盖语义）
- `delay > TimerWheelSize` → 放到最远 slot（slot = TimerWheelSize - 1），logic 被唤醒后 Think 重新注册剩余 delay
  - 这是结合游戏场景的 trick：amortized 1/200 的额外 Think 开销可忽略，避免了复杂的 overflow 设计
- `delay <= 0` → 不注册 timer（不再自动唤醒）

### 并发处理

- Think 阶段：timer 注册写入 worker 本地 `timerRegs`（因为同一 logic 始终在同一 worker，无冲突，后注册自然覆盖先注册）
- Tick 结束后：合并所有 worker 的 `timerRegs` 到全局 timer wheel
- Timer wheel 本身不需要并发安全

### Timer 覆盖语义

当 logic L 注册新 timer 时：

1. 如果 L 已有活跃 timer → 移除旧 timer，注册新 timer
2. 用 `map[logicID]slotIndex` 维护反向索引，支持 O(1) 覆盖

---

## 数据流总结

```
                    Think Phase                 Apply Phase
                    ─────────                   ───────────
Input:              inbox (signals)             effects (grouped by target)
Output:             effects[]                   signals[]
                    signals[]
                    timer registrations[]

Can modify:         private state               public state (own only)
Reads:              world snapshot              world snapshot
                    public state snapshot(*)

(*) 同一 superstep 内 Think 阶段 public state 静态；
    superstep 间 Apply 更新后对下一轮可见。

不允许:
  Apply 产出 effect（CommitCtx 无 Publish）
  Think/Apply 直接修改其他 owner 状态
```

## 关键不变量

1. 同一 Logic 在并发模式的同一 superstep 内只 Think 一次（frontier IndexMap 去重）
2. `CommitCtx.World` 是 frozen snapshot，非 live reference
3. World Apply 内部串行，与 Entity Apply 并行
4. Despawn 只标记 registry，不碰 entity public state
5. 每个 Apply 有独立的 signal 输出 buffer，无写竞争
6. 同一 logic 始终在同一 Think worker 上执行（RefId 亲和），保证 timerRegs 无冲突
7. Apply 按 block 并行，block 间完全独立，block 内按 targetRef 聚合

## 开放问题

1. **Block 粒度选择**：`BlockSize=137` 是否适合所有负载模式？是否需要动态调整？
2. **Think 返回 delay 的时间基准**：delay 是相对当前 tick 的偏移量？
3. **外部输入注入 API**：网络请求如何在 tick 开始前转化为 Signal 进入对应 Logic 的 inbox
4. **Logic 生命周期**：Init/Dispose 与 scheduler/timer wheel 的交互
5. **串行模式下同一 logic 被多条因果链触发**：depth 不同时取 min depth（给更大级联空间），还是允许重复 Think？
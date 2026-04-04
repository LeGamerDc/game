# Scheduler Design

## 前置约定

- 本设计稿描述 Scheduler 的调度模型，涵盖并发和串行两种执行模式。
- Logic 接口定义以 `sched/world.go` 为唯一权威来源。
- 并行 tick 整体设计（概念、所有权、数据分层）见 `docs/design/parallel.md`。
- 设计稿与代码出现矛盾时，以代码为准。
- 代码文件：`sched/scheduler.go`（核心调度循环）、`sched/scheduler_parallel.go`（并发路径）、`sched/scheduler_serial.go`（串行路径）、`sched/wheel.go`（timer wheel）、`sched/block_collector.go`（per-thread collector）、`sched/utils.go`（辅助类型）。

## 设计目标

Scheduler 负责在每个 tick 内调度 Logic 的 Think/Apply 执行，核心职责：

1. **管理 Think 激活**：决定哪些 Logic 在当前 tick 需要 Think（timer 到期、signal 触发、外部输入）
2. **收集与分发 Effect**：Think 产出的 Effect 按目标 owner 分块聚合后交给 Apply（并发模式），或即时调用 Apply（串行模式）
3. **传递 Signal**：Think/Apply 产出的 Signal 传递给下一轮 Think（并发模式通过双缓冲 swap，串行模式通过递归调用）
4. **自动选择执行模式**：每轮 superstep 根据工作量选择并发或串行
5. **防护无限循环**：并发模式通过 superstep 轮次限制，串行模式通过 cascade depth 限制

## Meta 配置

```
type ScheduleMeta struct {
    // Think concurrency threshold: workCount >= this value triggers parallel mode
    ThinkConcurrencyThreshold int // default: 500

    // Number of parallel workers (shared by Think and Apply phases)
    Concurrency int // default: 5

    // Max superstep rounds (parallel) / max cascade depth (serial)
    // Budget is shared: serial maxDepth = MaxSupersteps - completed parallel rounds
    MaxSupersteps int // default: 3

    // Timer wheel slot count
    TimerWheelSize int // default: 200

    // Block count for hash-based sharding (prime number)
    BlockSize int // default: 137
}
```

零值字段在 `NewScheduler` 中补齐默认值。

---

## ProcessTick 生命周期

```
ProcessTick(world):

  1. injectPending
     Emit() accumulated signals (pending) -> signalRead[0]
     hashed by targetRef % BlockSize into block buckets

  2. Superstep Loop  (round = 0 .. MaxSupersteps-1)
     firstSuperstep := true  // set to false after first parallel round
     workCount = countWork(includeTimers = firstSuperstep)
     workCount == 0 -> break

     if workCount >= ThinkConcurrencyThreshold:
       +-- Parallel Path --------------------------------+
       |  parallelThink(world, firstSuperstep)           |
       |  firstSuperstep = false                         |
       |  commitWatches(world)              // NEW       |
       |  computeApplyAssignment()          // LPT       |
       |  parallelApply(world)                           |
       |  swapSignalBuffers()                            |
       |  resetEffectCollectors()                        |
       +------------------------------------------------+
     else:
       +-- Serial Path ---------------------------------+
       |  serialProcess(world, includeTimers,            |
       |                MaxSupersteps - round)           |
       |  swapSignalBuffers()                            |
       |  break   // serial is terminal                  |
       +------------------------------------------------+

  3. Tick End
     timerWheel.merge()     // thread-local logs -> global wheel
     timerWheel.advance()   // clear thread logs, currentTime++

  4. Overflow
     signalRead residuals auto-preserved for next tick
     (injectPending does not clear signalRead)
```

### countWork

统计 `signalRead` 中的信号条目数加上 timer wheel 到期条目数（仅首轮 superstep）。达到 `ThinkConcurrencyThreshold` 时 early exit——超过阈值后精确计数对模式选择无意义。复杂度与旧 `hasWork` 相同：O(C×B)，实践中极低开销。

### 外部输入

`Emit(ref, signal)` 在 tick 外部调用，追加到 `pending` slice。`injectPending` 在 tick 开始时将 pending 按 `hash(ref) % BlockSize` 分桶注入 `signalRead[0]`（固定使用 threadId=0 作为外部输入的来源标识）。

### Logic 查找

Scheduler 不维护 logic 注册表。构造时注入 `W`（同时实现 `WorldView` + `LogicProvider[L]`），通过 `W.GetLogic(ref)` 查找 logic。getLogic 在 Think/Apply 阶段被并发调用，调用方须保证并发读安全。

---

## 并发模式

### Block-based 架构

所有 signal/effect 按 `hash(targetRef) % BlockSize` 映射到 block。Think 和 Apply 都以 block 为调度粒度：

- **blockCollector**：per-thread、per-block 的 `[]refVal[V]` 数组。`refVal` 将值与其 targetRef 打包，保留 ref 以便后续按 target 聚合。
- **CacheLinePad 隔离**：所有 per-thread 并发写入的结构体（`blockCollector`、`timerCollector`、`collectBuf`）头部有 `cpu.CacheLinePad`，不假定 cache line = 64（ARM 可达 128）。

### Think Phase

**Thread 分配**：`blockId % Concurrency → threadId`，初始化时固定，跨 superstep/tick 一致。同一 Logic（映射到同一 block）始终在同一 thread 上执行 Think，使 timer wheel 的 thread-local write 天然无冲突。

**thinkRef 追踪**：每个 thread 维护一个 `thinkRef` 变量，记录当前正在执行 Think 的 Logic refId。`SetWatch` 闭包捕获 `thinkRef` 以将 watch 更新关联到正确的 Logic，避免每次调用都创建新闭包。

**每个 thread 的执行流程**（对每个负责的 block）：

1. **Timer 激活**（首轮 superstep）：遍历 `timerWheel.get(blockId)` 中的到期 refId，设置 `thinkRef = refId`，调用 `Think(ctx, emptyInbox)`。
2. **Signal 消费**：跨所有 source thread 收集 `signalRead[*][blockId]` 到 per-thread flat buffer（`thinkCollectBuf`），按 ref 排序后线性分组，逐组设置 `thinkRef = ref` 并调用 `Think(ctx, signals)`。
3. **产出写入**：
   - `ctx.Publish(ref, eff)` → `effectCollectors[threadId]` 按 `hash(ref) % BlockSize` 分桶
   - `ctx.Emit(ref, sig)` → `signalWrite[threadId]` 按 `hash(ref) % BlockSize` 分桶
   - `ctx.SetWatch(ws)` → `watchCollectors[threadId]`，打包为 `RefWatch{thinkRef, ws}`
   - Think 返回 `delay > 0` → `timerWheel.set(threadId, blockId, ref, delay)`

**Sort-based 分组**（替代旧的 map-based 分组）：per-thread `collectBuf` 收集同一 block 的所有 signal 到 flat slice，`slices.SortFunc` 按 ref 排序后线性扫描分组。每个 block 处理前 `flatBuf[:0]` 重置，无跨 block 状态泄漏。`refValInbox` 适配器将 `[]refVal[S]` 子切片零拷贝适配为 `Inbox[S]` 接口。

**线程安全**：

- `signalRead[*]` 在 Think 阶段只读（上轮 swap 后不再写入）
- `effectCollectors[threadId]` / `signalWrite[threadId]` 只有本 thread 写入
- `watchCollectors[threadId]` 只有本 thread 写入（CacheLinePad 隔离）
- `timerWheel.threadBuf[threadId]` 只有本 thread 写入
- getLogic 并发读（调用方保证 tick 内无写）

### Apply Phase

**Block 分配**：LPT（Longest Processing Time first）近似算法——统计每个 block 跨所有 Think thread 的 effect 总量（`blockLoads`），按量降序排列，依次分配给当前负载最低的 thread。这是经典多处理器调度的 (4/3 - 1/(3T)) 近似算法。无 effect 的 block 跳过。

**CommitCtx 使用 WorldView**：Apply 阶段的 `CommitCtx.World` 类型为 `WorldView[WS]`（通过 `world.GetWorldView()` 获取），提供只读快照访问，包含 `WatchOf(ref)` 查询其他 Logic 的 watch 状态。

**每个 thread 的执行流程**（对分配到的每个 block）：

1. 跨所有 Think thread 收集 `effectCollectors[*][blockId]` 到 per-thread flat buffer（`applyCollectBuf`）
2. 按 ref 排序后线性分组（同 Think Phase 的 sort-based 分组策略）
3. 逐组调用 `logic.Apply(commitCtx, effects)`，`refValInbox` 适配器零拷贝传入
4. Apply 产出的 signal → `signalWrite[threadId]`

**World Effect**：`RefWorld` 按 `hash(RefWorld) % BlockSize` 落入某个 block，作为该 block 的普通 target 参与 Apply。World Apply 内部串行（只有一个 owner：`RefWorld`），与其他 entity Apply 天然并行。不需要独立的 world effect 阶段。

### Signal 传递：双缓冲 Swap

不存在显式的单线程 signal routing 阶段。Signal 传递通过双缓冲实现：

- `signalRead[threadId]`：当前 Think 消费的输入
- `signalWrite[threadId]`：当前 Think/Apply 的产出

每轮 superstep 结束后 swap：`signalRead ← signalWrite`（下一轮 Think 的输入），清空旧 signalRead 作为新 signalWrite。Think 和 Apply 共用 signalWrite（barrier 保证时序安全：Think → barrier → Apply → barrier → swap）。

超出 MaxSupersteps 后 signalRead 中的残余信号自动保留到下一 tick（injectPending 不清空 signalRead）。

### 去重

Scheduler 不保证同一 Logic 在同一 superstep 内只 Think 一次。同一 Logic 可能因为 timer + signal 同时到达或分布在不同 source thread 的 block 中而被多次激活。Logic 自身处理重复激活。这消除了 per-logic inbox 聚合、frontier 去重和 signal routing 的开销。

---

## 串行模式

### 核心设计：Truly Inline

串行模式不使用 blockCollector、不排序、不分组、不创建 goroutine。`ThinkCtx.Publish` / `ThinkCtx.Emit` / `CommitCtx.Emit` 闭包直接递归调用目标 Logic 的 Apply/Think，所有 signal 和 effect 当场处理。

### 三个递归闭包

`serialProcess` 入口内定义三个互相引用的闭包：

```
thinkSignal(ref, sig):
    if depth >= maxDepth:
        defer signal to signalWrite[0]  // overflow
        return
    logic = GetLogic(ref)
    depth++
    delay = logic.Think(thinkCtx, singleSignalInbox)
    depth--
    if delay > 0:
        blockId = hash(ref) % BlockSize
        timerWheel.set(blockToThread[blockId], blockId, ref, delay)

thinkTimer(ref):
    if depth >= maxDepth: return  // defensive; shouldn't happen for initial timers
    logic = GetLogic(ref)
    depth++
    delay = logic.Think(thinkCtx, emptyInbox)
    depth--
    if delay > 0: timerWheel.set(...)

applyOne(ref, eff):
    logic = GetLogic(ref)
    logic.Apply(commitCtx, singleEffectArrangement)
    // Apply does NOT increment depth
```

闭包接入 context：

- `thinkCtx.Emit = thinkSignal` — Think 产出 signal → 递归 Think
- `thinkCtx.Publish = applyOne` — Think 产出 effect → 立即 Apply
- `thinkCtx.SetWatch = func(ws)` — 立即通过 `world.CommitWatches` 提交（单线程无竞争）
- `commitCtx.Emit = thinkSignal` — Apply 产出 signal → 递归 Think

ThinkCtx 和 CommitCtx 各创建一次，跨所有递归调用复用。闭包捕获的 `depth` 和 `thinkRef` 是同一个栈变量，单线程下 inc/dec 自然匹配调用栈。串行模式下 `SetWatch` 立即生效，与 Apply 立即修改 public state 的语义一致。

### 初始 Frontier 处理

遍历所有 block，消费 timer（首轮 superstep）和 signalRead：

```
for blockId in 0..BlockSize:
    if includeTimers:
        for refId in timerWheel.get(blockId):
            thinkTimer(refId)
    for srcThread in 0..Concurrency:
        for rv in signalRead[srcThread].get(blockId):
            thinkSignal(rv.ref, rv.val)
```

迭代期间 signalRead 只读、signalWrite 只写（deferred signals），无冲突。Timer 新注册写入 thread-local log（不影响 wheel 本体），timerWheel.get() 结果稳定。

### Depth 追踪

- **栈变量 depth**：通过 inc/dec 自然匹配递归调用栈。不嵌入 signal/effect 值（`refVal` 结构体），避免膨胀 parallel 路径的 cache 效率。
- **Depth 语义**：Think → Publish → Apply 是同一 depth 层级的原子操作。只有进入 Think 时 `depth++`，Apply 不增加 depth。
- **Deferred signal 不携带 depth**：溢出信号写入 signalWrite 时不记录 depth，下一 tick 从 depth=0 重新开始。

### 溢出处理

`depth >= maxDepth` 时，信号写入 `signalWrite[0]`（serial 不读 signalWrite，无冲突）。`serialProcess` 返回后，`ProcessTick` 调用 `swapSignalBuffers()` 将 deferred 信号移入 signalRead，保留到下一 tick。

### Timer 一致性

串行模式使用 `blockToThread[blockId]` 映射写入正确的 thread-local log，而非固定 thread 0。保证同一 Logic 的 timer 在 parallel 和 serial 轮次中始终写入同一个 thread-local log，维持 last-write-wins 覆盖语义。

### 接口兼容性

Logic interface 在两种模式下完全相同。差异仅在 `ThinkCtx.Emit` / `ThinkCtx.Publish` / `ThinkCtx.SetWatch` / `CommitCtx.Emit` 闭包的实现：

- **并发模式**：写入 worker 本地 buffer，barrier 后统一处理（effect/signal/watch）
- **串行模式**：Emit/Publish 立即递归调用目标 Logic，SetWatch 立即提交

Logic 实现无需感知当前执行模式。

---

## 模式切换

### 判断机制

每轮 superstep 独立判断。`countWork(includeTimers)` 统计 signalRead + timer 的条目总数：

- `>= ThinkConcurrencyThreshold` → 并发模式
- `< ThinkConcurrencyThreshold` → 串行模式

### 切换方向

| Direction | Timing | Behavior |
|---|---|---|
| parallel → serial | round N 结束后工作量低于阈值 | 串行处理剩余，depth 预算 = MaxSupersteps - N |
| serial → parallel | 不允许 | 递归调用中无法判定工作量规模；串行是终态 |

---

## Timer Wheel

### 结构

- 单层环形数组，大小 = `TimerWheelSize`（default: 200）
- 按 block 分片：`wheel[slot][blockId]` → `epochSet[refId]`
- Thread-local unified log：`threadBuf[threadId].log` → `IndexMap[refId, timerEntry{blockId, delay}]`

### Epoch-based Lazy Clear

每个 `epochSet` 带 epoch 标记（目标绝对 tick）。写入（`putAt`）时若 epoch 不匹配则惰性清空旧数据再写入。读取（`rawAt`）时 epoch 不匹配返回 nil。`advance` 不需要逐 slot、逐 block 清空——epoch 不匹配自动视为空。

正确性依据：在一轮 wheel 循环内（连续 wheelSize 个 tick），每个物理 slot 只可能对应一个绝对目标 tick。delay 被 clamp 到 `[1, wheelSize-1]`，所以不同 tick 的 merge 写入同一物理 slot 时，目标绝对 tick 总是相同的。

### 生命周期

1. **set**（Think 阶段）：写入 thread-local unified log（`IndexMap.Put`）。同一 ref 后写覆盖前写。`delay <= 0` 仅取消 thread-local log 中未 merge 的登记。
2. **get**（Think 阶段，首轮 superstep）：读取 `wheel[currentTime % wheelSize][blockId]`，epoch 匹配时返回到期 ref 列表。返回值直接引用内部存储，不可跨 advance 保存。
3. **merge**（tick 结束）：遍历所有 thread log，将 `(ref, delay)` 写入 `wheel[targetSlot][blockId].putAt(ref, targetTick)`。只遍历实际存在的登记条目，不扫描空 block。
4. **advance**（tick 结束，merge 之后）：清空所有 thread log，`currentTime++`。

### 语义

- `delay > 0`：注册到 `currentTime + min(delay, wheelSize-1)` 对应的 slot
- `delay > wheelSize`：clamp 到最远 slot。被唤醒后 Think 重新注册剩余 delay（amortized 1/200 的额外 Think 开销可忽略）
- `delay <= 0`：不注册 timer，仅尝试取消 thread-local log 中尚未 merge 的登记
- 覆盖语义：同一 ref 同一 tick 内多次 set，最后一次 delay 值通过 IndexMap.Put 覆盖前次

---

## 数据流总结

```
                    Think Phase                 Apply Phase
                    ───────────                 ───────────
Input:              inbox (signals)             inbox (effects, grouped by target)
Output:             effects[]                   signals[]
                    signals[]
                    watch updates[]
                    timer registrations[]

Can modify:         private state               public state (own only)
Reads:              world snapshot (World)       world snapshot (WorldView)
                    WatchOf(ref)                WatchOf(ref)

Not allowed:
  Apply produce effect (CommitCtx has no Publish)
  Apply set watch (CommitCtx has no SetWatch)
  Think/Apply directly modify other owner's state
```

**Watch 更新流程**：

- **并发模式**：Think 阶段 `SetWatch(ws)` → per-thread `watchCollectors` → Think barrier 后 `commitWatches` flatten 并调用 `World.CommitWatches(Inbox[RefWatch[WS]])` → Apply 和下一轮 Think 通过 `WorldView.WatchOf(ref)` 读到更新后的 snapshot
- **串行模式**：`SetWatch(ws)` 立即调用 `world.CommitWatches`（单线程无竞争），后续 inline Think/Apply 立即可见

并发模式下，同一 superstep 内 Think 阶段 public state 静态（所有 Think 共享同一份 snapshot）；superstep 间 Apply 更新后对下一轮 Think 可见。

串行模式下，Apply 立即修改 public state，后续 inline Apply 可见变化。这是 truly inline 的自然结果（见"已知语义差异"）。

---

## 关键不变量

1. 调用方须保证 `World` 在并行 Think/Apply 期间不被并发修改（作为 caller contract，Scheduler 本身不创建 snapshot）
2. 同一 Logic 始终映射到同一 block（`hash(ref) % BlockSize`）和同一 Think thread（`blockId % Concurrency`）
3. 每个 Apply worker 处理不同 block → 不同 target → 无写竞争
4. Timer wheel thread-local log 与 Think thread 亲和绑定，无竞争（parallel 和 serial 模式均通过 `blockToThread` 映射保持一致）
5. Per-thread 并发写入结构体均有 CacheLinePad 隔离（头部 pad，不假定 cache line 尺寸）
6. 串行模式 depth 递增仅发生在 Think 入口，Apply 不增加 depth
7. Scheduler 不保证同一 Logic 在同一 superstep 内只 Think 一次——Logic 自身处理重复激活
8. Watch 更新遵循 BSP 一致性：并发模式下 per-thread 收集 → Think barrier 后批量提交 → Apply 和下一轮 Think 看到更新后的 snapshot；串行模式下立即生效
9. 默认无 watch：Logic 未调用 `SetWatch` 则不接收任何 signal（必须显式声明兴趣）

---

## 已知语义差异：串行 vs 并发

| Dimension | Parallel | Serial |
|---|---|---|
| Apply granularity | 同一 target 的多个 effect 批量传入一次 Apply（`refValInbox`） | 每个 effect 独立触发一次 Apply（`sliceInbox`） |
| Execution order | Think → barrier → commitWatches → Apply → barrier → swap | Think 中 Publish/Emit 立即触发（DFS recursive） |
| Same-logic multi-activation | 可能（timer + signal 同时到达） | 可能（多条信号、或 self-emit） |
| State visibility | Think 阶段 public state 静态；superstep 间 Apply 更新后可见 | Apply 立即修改 public state，后续 inline Apply 可见变化 |
| Watch update | 延迟提交：per-thread 收集 → Think barrier 后 `commitWatches` 批量提交 | 即时提交：`SetWatch` 直接调用 `world.CommitWatches`，后续 inline 调用立即可见 |

这些差异在游戏场景下可接受：游戏不保证同 tick 内事件的发生顺序，Effect/Apply 设计为顺序无关（任意顺序都是合法的）。Watch 更新的延迟 vs 即时差异与 public state 可见性差异一致——串行模式下一切都是 truly inline 的自然结果。

---

## Watch State

### 概述

WatchState 是 Logic 声明其感兴趣的 SignalKind 的机制，使发射方在 Emit 前可查询目标 Logic 的兴趣，实现发射端过滤。

### 接口设计

```
WatchState interface {
    Interest(SignalKind) bool
}
```

抽象实现——底层可以是 bitset、map、tree 等任何数据结构。框架只要求 `Interest` 方法的查询能力。

**WorldView 暴露查询**：`WatchOf(uint64) WS` 允许 Logic 在 Think/Apply 阶段查询任意 Logic 的 watch 状态，从而决定是否 Emit。

**SetWatch 在 ThinkCtx 上**：Logic 通过 `ctx.SetWatch(ws)` 在 Think 阶段声明兴趣。不通过 Think 返回值传递——避免零值歧义，且使 watch 更新成为可选操作。

**默认无 watch**：未调用 `SetWatch` 的 Logic 不接收任何 signal，必须显式声明兴趣。

### 并发模式：BSP 一致性延迟更新

并发模式遵循 BSP 一致性模型，watch 更新与 effect/signal 采用相同的 barrier 语义：

```
Think Phase (parallel)
  ├─ Logic.Think → ctx.SetWatch(ws)
  │   └─ watchCollectors[threadId] ← RefWatch{thinkRef, ws}
  ├─ (per-thread 并发写入，CacheLinePad 隔离)
  │
  Think Barrier ──────────────────────────
  │
  commitWatches:
  │  flatten watchCollectors[0..C] → watchCommitBuf
  │  world.CommitWatches(sliceInbox[RefWatch[WS]](watchCommitBuf))
  │  清空所有 watchCollectors
  │
  ├─ Apply Phase: WorldView.WatchOf(ref) 返回更新后的 snapshot
  └─ 下一轮 Think: WorldView.WatchOf(ref) 同样返回更新后的 snapshot
```

**关键数据结构**：

- `watchCollectors []collectBuf[RefWatch[WS]]`：per-thread 收集缓冲，CacheLinePad 隔离
- `watchCommitBuf []RefWatch[WS]`：预分配的 flatten 缓冲，避免每次 commitWatches 分配
- `RefWatch[WS]`：`{RefId uint64, WS WS}` 将 watch 更新与 Logic ref 关联
- `WatchCommitter` 接口：`CommitWatches(Inbox[RefWatch[WS]])`，由 World 实现

### 串行模式：即时更新

串行模式下 `SetWatch` 闭包立即调用 `world.CommitWatches`，打包单个 `RefWatch{thinkRef, ws}` 通过 `sliceInbox` 传入。单线程无竞争，与串行模式的 truly inline 语义一致（Apply 也立即修改 public state）。

### Scheduler 类型参数

Scheduler 现有 5 个类型参数：`Scheduler[W, S, E, L, WS]`

- `W`：`World[WS] + LogicProvider[L] + WatchCommitter[WS]`
- `S`：`SignalI`
- `E`：`EffectI`
- `L`：`Logic[W, S, E, WS]`
- `WS`：`WatchState`

### World vs WorldView 分层

- `WorldView[WS]`：只读快照接口，提供 `Now()`、`Version()`、`Round()`、`WatchOf(ref)`
- `World[WS]`：扩展 `WorldView[WS]`，增加 `GetWorldView() WorldView[WS]`
- ThinkCtx 持有 `World`（完整访问），CommitCtx 持有 `WorldView`（只读访问）
- Apply 阶段通过 `world.GetWorldView()` 获取 WorldView，确保 reducer 无法写穿 World

---

## 开放问题

1. **Block 粒度**：`BlockSize=137` 是否适合所有负载模式？是否需要动态调整？
2. **Think 返回 delay 的时间基准**：当前为相对当前 tick 的偏移量。
3. **外部输入注入 API**：网络请求如何在 tick 开始前转化为 Signal。
4. **Worker pool**：当前每 superstep 创建 goroutine，可替换为预分配 worker pool（代码中已标注 TODO）。
5. **TickStats / tracing / debug API**：是否需要扩展。
6. **World effect**：复用 `Logic` 接口是否足够清晰，还是应单独抽出 world reducer 接口。
7. **WatchState 实现选择**：默认提供 bitset 实现还是由用户自行实现？是否需要框架级标准实现。
# Memory

Last Updated: 2026-03-30

## Current Focus

- `scheduler_serial.go` 串行模式已完成实现并通过全部测试（含 race detector）。
- `ProcessTick` 已支持 parallel/serial 自动模式路由：每轮 superstep 根据 `countWork` vs `ThinkConcurrencyThreshold` 选择模式。
- 下一步：修复两个已确认的接口问题（Ref 空间歧义、Publish 不区分 Entity/World Effect）。

## Latest State

- `en/world.go` 是当前引擎接口讨论的权威入口。
- `WorldView` 目前已包含 `Now()/Version()/Round()` 三个只读观测接口。
- `en/scheduler.go` 核心调度器，包含 `ProcessTick`（双模式路由）、`countWork`、`injectPending`、`swapSignalBuffers`、`resetEffectCollectors`。
- `en/scheduler_parallel.go` 并行路径：`parallelThink`/`parallelApply`/`thinkWorker`/`applyWorker` + `emitClosure`/`publishClosure`。
- `en/scheduler_serial.go` 串行路径：`serialProcess` 通过三个递归闭包（`thinkSignal`/`thinkTimer`/`applyOne`）实现 truly inline 执行。
- `en/scheduler_test.go` 共 35 个测试（21 个 parallel + 14 个 serial），全部通过含 race detector。

### Scheduler 核心设计

- **getLogic 注入**：`NewScheduler` 接受 W（同时实现 `WorldView` + `LogicProvider[L]`），由外部负责 logic 生命周期管理。
- **双缓冲 Signal Collectors**：`signalRead`（消费）+ `signalWrite`（产出），superstep/serial 结束后 swap + clear。
- **无 per-logic 去重**：Scheduler 不保证同一 logic 在同一 superstep 只 Think 一次。Logic 自身处理重复激活。
- **外部输入**：`Emit()` → `pending` → `injectPending` 注入 `signalRead[0]`。
- **Sort-based 分组**（parallel only）：per-thread `collectBuf` flat buffer + sort by ref + 线性分组。
- **CacheLinePad 隔离**（parallel only）：`blockCollector`、`timerCollector`、`collectBuf` 头部 pad。

### ProcessTick 生命周期

```
injectPending → superstep 循环 {
  countWork(includeTimers) → workCount
  workCount == 0 → break
  workCount >= ThinkConcurrencyThreshold → parallel path:
    parallelThink → computeApplyAssignment → parallelApply → swap → reset
  workCount < ThinkConcurrencyThreshold → serial path:
    serialProcess(includeTimers, maxDepth=MaxSupersteps-round) → swap → break
} → merge timer wheel → advance timer wheel
```

### Serial 模式设计

- **Truly inline 执行**：`Publish`/`Emit` 闭包立即调用目标 logic 的 Apply/Think，不经过任何中间缓冲。
- **三个递归闭包**：
  - `thinkSignal(ref, sig)`：depth check → GetLogic → depth++ → Think(单信号 Inbox) → depth-- → timer set
  - `thinkTimer(ref)`：GetLogic → depth++ → Think(空 Inbox) → depth-- → timer set
  - `applyOne(ref, eff)`：GetLogic → Apply(单 effect Arrangement)，不增加 depth
- **Depth 追踪**：栈变量 `depth` 通过 inc/dec 自然匹配递归调用栈。不嵌入 refVal，不影响 parallel 路径的 cache 效率。
- **Depth 语义**：Think→Publish→Apply 是同一 depth 层级的原子操作；只有经过 Think 时 depth+1。
- **溢出处理**：`depth >= maxDepth` 时信号写入 `signalWrite[0]`（serial 不读 signalWrite），`ProcessTick` 结束时 swap 保留到下一 tick。
- **Timer 注册**：使用 `blockToThread[blockId]` 映射写入正确的 thread-local log，与 parallel 模式的 last-write-wins 语义一致。
- **零重量级基础设施**：不使用 blockCollector、不排序、不分组、不创建 goroutine。开销仅为递归函数调用 + 闭包创建（一次性）。
- **ThinkCtx/CommitCtx 复用**：创建一次，闭包捕获 depth 引用，跨所有递归调用复用。

### 模式切换

- `countWork` 替代了原 `hasWork`，返回 work item 总数（signal + timer），达到 threshold 时 early exit。
- 每轮 superstep 独立判断模式；串行是终态（break），不可回到并行。
- 串行 depth 预算 = `MaxSupersteps - 已完成并发轮次`。

### Timer Wheel

- 单层环形数组，大小 200。
- Unified Log + Epoch-based Lazy Clear。
- merge() O(actual_registrations)，advance() O(threads)。
- Serial 模式通过 `blockToThread` 映射使用正确的 thread-local log，保证与 parallel 模式的覆盖语义一致。

## Confirmed Decisions

### 协作流程

- 协作记忆统一在 `docs/memory/` 目录下，包含三个文件：`memory.md`、`tasks.md`、`todo.md`。

### Parallel Tick 接口审计结论

**需要修复的接口问题：**

1. **Ref 空间歧义**：`IsSerialRef(RefWorld) == true`，三类 ref 不互斥。需要明确互斥分区。
2. **Publish 不区分 Entity/World Effect**：需要拆分或加 domain 标记。

**确认不改的设计点（3-10）**：WorldView 极简、Signal/Effect source ref 用户管理、代数模型推迟、Budget/Meta 不进 Logic 接口、Apply→Emit 合法、Timer 冲突 Logic 处理、Think 激活类型不分类、Ack 内嵌 Think/private state。

### Serial 模式设计决策

- **Truly inline**（非 collect-then-cascade）：Publish/Emit 原地触发 Apply/Think，不做 Think 输出的中间收集。
- **Apply 粒度差异已确认接受**：serial 模式下 Apply 每次收到单个 effect（vs parallel 模式的批量 Arrangement）。这是 truly inline 的自然结果，用户确认可接受。
- **设计文档伪代码已过时**：`scheduler.md` 中的串行伪代码（collect-then-cascade）未经审查，以代码为准。
- **Logic 接口不变**：`thinkSignal`/`thinkTimer`/`applyOne` 是 scheduler 内部闭包，不改 Logic interface。Serial/parallel 对 Logic 实现完全透明。
- **Depth 用栈变量追踪**：不嵌入 signal/effect 值，避免膨胀 parallel 路径的 refVal 结构体。

### Logic 查找

- `getLogic` 由外部注入（通过 W 的 `LogicProvider[L]` 接口），Scheduler 不维护 logic 注册表。

### 去重

- Scheduler 不保证同一 logic 在同一 superstep 只 Think 一次。

### 双缓冲 Signal Collectors

- signalRead/signalWrite swap + clear。Think/Apply 共用 signalWrite（barrier 保证时序安全，或 serial 模式下不使用 signalWrite 除溢出）。

### Scheduler 并发模型

- Think 阈值 500、并发 worker 5、最多 3 轮 superstep。参数统一放入 `ScheduleMeta`。
- Block-based effect 收集，sort-based 分组替代 map。
- CacheLinePad 隔离规则。
- Think 阶段 `blockId % Concurrency → threadId`（稳定映射）。
- Apply 阶段 LPT 动态分配。
- World Effect 按 `hash(RefWorld) % BlockSize` 落入某个 block。

## Open Questions

- Logic 生命周期方法（Init/Dispose）是否需要加入接口。
- LogicMeta 如何暴露给调度器。
- ThinkCtx 函数引用可被 Logic 逃逸存储——Go 限制，只能靠规范。
- `engine.go` 中现有 GAS 模式与新并行模型的迁移隔离策略。
- 外部输入注入 API：网络请求如何在 tick 开始前转化为 Signal。
- Think 返回 delay 的时间基准：相对当前 tick 的偏移量？
- Block 粒度（137）是否适合所有负载模式。
- `TickStats` 是否需要扩展为 tracing/debug API。
- World effect 复用 `Logic` 接口是否足够清晰。
- Worker pool 替代每 superstep 创建 goroutine（TODO 已标注）。
- `docs/design/feedback.md` 应并入主设计文档还是保留。
- `docs/design/scheduler.md` 串行伪代码需要更新以反映 truly inline 设计。

## Relevant Files

- `AGENTS.md`
- `en/world.go`
- `en/scheduler.go`
- `en/scheduler_parallel.go`
- `en/scheduler_serial.go`
- `en/scheduler_test.go`
- `en/wheel.go`
- `en/wheel_test.go`
- `en/block_collector.go`
- `en/utils.go`
- `en/engine_bak.go`
- `docs/design/parallel.md`
- `docs/design/scheduler.md`
- `docs/design/feedback.md`
- `docs/references/parallel_theory.md`
- `docs/references/survey.md`

## Should

- 对任何算法议题可以创建 subagent 单独调研解决。
- 设计稿与代码出现矛盾时，以代码为准。

## Dont's

- 不要在 refVal 中嵌入 serial-only 的字段（如 depth），避免影响 parallel cache 效率。
# Memory

Last Updated: 2026-03-29

## Current Focus

- `en/scheduler.go` 并行 tick 调度器已完成重构：getLogic 注入、双缓冲 signal collectors、去除 per-logic 去重。
- 下一步：实现串行模式（cascade depth）、processTick 的模式路由（frontier < ThinkConcurrencyThreshold 时切换到串行）。
- 仍有两个已确认的接口问题待修复（Ref 空间歧义、Publish 不区分 Entity/World Effect）。

## Latest State

- `en/world.go` 是当前引擎接口讨论的权威入口。
- `WorldView` 目前已包含 `Now()/Version()/Round()` 三个只读观测接口。
- `en/scheduler.go` 已完成重构，核心设计：
  - **getLogic 注入**：`NewScheduler` 接受 `getLogic func(uint64)(L,bool)` 参数，由外部（如 WorldView 实现）负责 logic 生命周期管理。Scheduler 不再内部维护 logic 注册表。
  - **双缓冲 Signal Collectors**：`signalRead`（消费）+ `signalWrite`（产出），superstep 结束 swap + clear。Think/Apply 共用 signalWrite（barrier 保证时序安全）。溢出信号自动保留在 signalRead 中延迟到下一 tick。
  - **无 per-logic 去重**：Scheduler 不保证同一 logic 在同一 superstep 只 Think 一次。去除了 threadInboxes/threadFrontiers/pendingInbox/routeSignals/buildInitialFrontier/deferRemainingInboxes。Logic 自身处理重复激活。
  - **外部输入**：`Emit()` 追加到 `pending` slice，`injectPending` 在 tick 开始时注入 `signalRead[0]`。
  - **ProcessTick 生命周期**：injectPending → superstep 循环（parallelThink → computeApplyAssignment → parallelApply → swapSignalBuffers → resetEffectCollectors）→ merge/advance timer wheel
  - Think 阶段按 block 稳定分配到 thread（`blockId % Concurrency`，初始化时固定）
  - Apply 阶段使用 LPT（Longest Processing Time first）近似算法按 effect 数量动态分配 block
  - Timer wheel 集成：Think 产出的 delay → thread-local set → tick 结束 merge + advance
  - `signalGroupBufs[threadId]` 用于 Think worker 内部按 targetRef 分组 signal，遍历 signalRead block 时同一 block 可能包含多个 logic 的 signal，分组后逐组调 Think
- `en/scheduler_test.go` 包含测试覆盖全部核心路径，含 race detector 通过。
- `en/wheel.go` 已完成 Unified Log + Epoch-based Lazy Clear 重构。
- `en/block_collector.go` 提供 per-thread block-sharded collector，被 scheduler 用于 `refVal[E]` 和 `refVal[S]` 收集。
- `docs/design/parallel.md` 是 parallel tick 设计意图的主文档。
- `docs/design/scheduler.md` 是 Scheduler 并发调度模型设计文档。

## Confirmed Decisions

### 协作流程

- 协作记忆统一在 `docs/memory/` 目录下，包含三个文件：
  - `memory.md`：稳定上下文和当前状态
  - `tasks.md`：项目级任务注册表
  - `todo.md`：当前活跃任务的执行清单

### Parallel Tick 接口审计结论

以下结论来自对 `en/world.go` 与 `docs/design/parallel.md` 的深度审计及逐条讨论。

**需要修复的接口问题：**

1. **Ref 空间歧义**：`IsSerialRef(RefWorld) == true`，三类 ref（Normal / World / Serial）不互斥。缺少 `RefNone` 和 `IsValidRef`。需要明确互斥分区。
2. **Publish 不区分 Entity/World Effect**：`ThinkCtx.Publish` 用同一函数 + 同一类型参数覆盖 entity effect 和 world effect 两种语义，无编译期安全。需要拆分或加 domain 标记。

**确认不改的设计点：**

3. WorldView 保持极简。
4. Signal/Effect 的 source ref 是用户的事。
5. 代数模型推迟。
6. Budget/Meta 不进 Logic 接口。
7. Apply→Emit 自激活是合法场景。
8. Timer 冲突由 Logic 内部处理。
9. Think 激活类型不需要框架层分类。
10. Ack 内嵌在 Think / private state 中。

### Logic 查找

- `getLogic func(uint64)(L,bool)` 由外部注入，Scheduler 不在内部维护 logic 注册表。
- 原因：`WorldView.GetLogic` 因 Go 泛型类型不变性（type invariance）无法表达返回匹配类型参数的 Logic。循环类型依赖（WorldView → Logic[W,S,E] → WorldView）和只读语义冲突也无法解决。
- 调用方须保证 getLogic 在并发调用时安全（通常是底层 map 在 tick 内无写即可）。

### 去重

- Scheduler 不保证同一 logic 在同一 superstep 只 Think 一次。Logic 自身处理重复激活。
- 消除了 `threadInboxes`/`threadFrontiers`/`pendingInbox`/`routeSignals`/`buildInitialFrontier`/`deferRemainingInboxes`，大幅简化调度器内部状态。

### 双缓冲 Signal Collectors

- `signalRead`（消费）+ `signalWrite`（产出），superstep 结束 swap + clear。
- Think/Apply 共用 signalWrite（barrier 保证时序安全：Think → barrier → Apply → barrier → swap）。
- 溢出信号自动保留在 signalRead 中延迟到下一 tick（injectPending 不清空 signalRead）。
- 外部输入通过 `Emit()` → `pending` → `injectPending` 注入 `signalRead[0]`。

### Scheduler 并发模型

**并发控制**：
- Think 阈值 500 开启并发，并发 worker 数 5，每 tick 最多 3 轮 superstep。
- 参数统一放入 `ScheduleMeta` 结构体，零值字段在 NewScheduler 中补齐默认值。

**Block-based Effect 收集**：
- 按 `hash(targetRef) % BlockSize` 分块，collector 存储 `refVal[E]{ref, val}` 保留 targetRef。
- Apply 阶段按 blockId 分配 worker，跨所有 Think worker 读取对应 block，按 targetRef 聚合后 Apply。
- 聚合使用 per-thread `map[uint64][]E` 缓冲，处理完后截断 slice 到 0 长度保留 capacity。

**Block→Thread 映射**：
- Think 阶段：`blockId % Concurrency → threadId`，初始化时固定，跨 superstep/tick 一致。
- Apply 阶段：LPT（Longest Processing Time first）近似算法按 effect 数量动态分配 block。

**串行模式（设计已确认，代码待实现）**：
- 无 superstep 概念，无 frontier push。
- 深度优先递归，cascade depth 控制递归深度。
- 串行 cascade depth 预算 = `MaxSupersteps - 已完成并发轮次`。
- 通过 ThinkCtx/CommitCtx 闭包实现差异，Logic 接口层完全兼容。

**模式切换（设计已确认，代码待实现）**：
- 每轮 superstep 独立判断，frontier 缩小到阈值以下可切换到串行模式。

**World Effect**：
- RefWorld 按 `hash(RefWorld) % BlockSize` 落入某个 block，作为普通 target 参与 Apply。

### Timer Wheel

- 单层环形数组，大小 200。
- Unified Log + Epoch-based Lazy Clear 已完成重构。
- merge() O(actual_registrations)，advance() O(threads)。
- `delay > TimerWheelSize` clamp 到最远 slot。
- `delay <= 0` 仅取消 thread-local 未 merge 的登记。
- 接口签名和语义不变，6 个测试通过。

### Scheduler 当前实现约束

- Scheduler 以 `Logic.ID()` 作为 owner/ref 权威索引。
- `Scheduler.Emit` / pending 承担外部输入注入。溢出信号通过 signalRead 自动延迟到下一 tick。
- 非法 target ref / 未注册 logic 的 signal/effect 被静默丢弃。

## Open Questions

- Logic 生命周期方法（Init/Dispose）是否需要加入接口——尚未讨论。
- LogicMeta 如何暴露给调度器——设计文档有描述，接口未体现，尚未讨论。
- ThinkCtx 函数引用可被 Logic 逃逸存储——Go 语言限制，无法在接口层解决，只能靠规范和 review。
- `engine.go` 中现有 GAS 模式与新并行模型的迁移隔离策略——尚未讨论。
- 外部输入注入 API：网络请求如何在 tick 开始前转化为 Signal。
- Think 返回 delay 的时间基准：相对当前 tick 的偏移量？
- Block 粒度（137）是否适合所有负载模式。
- 串行模式下同一 logic 被多条因果链触发时的 depth 处理策略。
- `TickStats` 是否需要进一步扩展为 tracing/debug API。
- 当前 world effect 复用 `Logic` 接口是否足够清晰，还是应单独抽出 world reducer 接口。
- Worker pool 替代每 superstep 创建 goroutine（TODO 已标注在代码中）。
- `docs/design/feedback.md` 应并入主设计文档还是保留为评审记录。

## Relevant Files

- `AGENTS.md`
- `en/world.go`
- `en/scheduler.go`
- `en/scheduler_test.go`
- `en/wheel.go`
- `en/wheel_test.go`
- `en/block_collector.go`
- `en/engine_bak.go`
- `docs/design/parallel.md`
- `docs/design/scheduler.md`
- `docs/design/feedback.md`
- `docs/references/parallel_theory.md`
- `docs/references/survey.md`

## Should

- 对任何算法议题（如 timer wheel、effect/signal 收集器、数据结构选型），可以创建 subagent 单独调研解决，不需要在主对话中展开所有细节。

## Dont's

- （暂无，待用户补充）

## Maintenance Notes

- 这里保存稳定上下文和当前状态，不保存完整聊天转录。
- 项目级任务追踪放在 `docs/memory/tasks.md`。
- 当前任务执行清单放在 `docs/memory/todo.md`。
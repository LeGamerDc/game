# Memory

Last Updated: 2026-04-03

## Current Focus

- **GDC 投稿准备阶段**：先行工作分析与价值评估已完成，进入 benchmark + demo 阶段。
- **WatchState 机制已实现**：Logic 可声明感兴趣的 SignalKind，发射方可查询目标 watch 状态实现发射端过滤。并发模式 BSP 一致性延迟更新，串行模式即时更新。
- 先行工作对比分析完成：7 篇用户提供的论文 + 12+ 近年工作全部分析，产出综合覆盖矩阵和新颖性结论。
- GDC 发表价值评估完成：判定为 **Competitive**，目标系统填补了 GDC "服务器端 tick 并行化"的主题空白。
- 下一步 P0 任务：性能 Benchmark + 端到端 combat path demo。
- GDC 2027 (March 1-5, 2027) 的 Call for Submissions 预计 2026 年 7-9 月开放。
- **2015-2025 Prior Art & Novelty Analysis 已完成**：搜索 7 个方向、分析 12+ 工作，结论为无实质新颖性威胁。
- Signal/Effect 代数组合调研已完成，结论为"确认不做"。

## Latest State

- Scheduler 实现完成，代码在 `sched/scheduler*.go`，35 个测试全部通过。
- **WatchState 实现完成**：
  - `WatchState` 接口：`Interest(SignalKind) bool`，抽象底层实现（bitset/map/tree 等）
  - `WorldView[WS]` 暴露 `WatchOf(uint64) WS`，Logic 可在 Think/Apply 中查询其他 Logic 的 watch 状态
  - `World[WS]` 扩展 `WorldView[WS]`，增加 `GetWorldView() WorldView[WS]`；ThinkCtx 持有 `World`，CommitCtx 持有 `WorldView`
  - `ThinkCtx.SetWatch func(WS)`：Logic 在 Think 中声明兴趣，不通过返回值传递（避免零值歧义）
  - 并发模式：per-thread `watchCollectors` → Think barrier 后 `commitWatches` 批量提交（BSP 一致性）
  - 串行模式：`SetWatch` 立即调用 `world.CommitWatches`（truly inline 语义一致）
  - 默认无 watch：未调用 `SetWatch` 的 Logic 不接收 signal
  - `WatchCommitter` 接口：`CommitWatches(Inbox[RefWatch[WS]])`，由 World 实现批量 watch 提交
  - Arrangement 概念已移除：Apply 统一使用 `Inbox[E]`，`sliceInbox` 和 `refValInbox` 泛化为 `any` 约束
  - Scheduler 现有 5 个类型参数：`Scheduler[W, S, E, L, WS]`
- 适配性调研全部完成，适配分类指导手册已完成：`docs/design/adaptation_guide.md`
- 旧 GAS 模式代码已归档为 `en/engine_bak.go`（全部注释掉），与新模型无冲突。
- **GDC 先行工作分析已完成**，产出文件：
  - `docs/papers/novelty_and_value_analysis.md`：综合先行工作对比与 GDC 发表价值分析
  - `docs/references/prior_work_analysis.md`：7 篇先行论文逐篇深度分析
  - `docs/references/prior_art_novelty_analysis.md`：2015–2025 年间近年工作新颖性分析

### 新颖性分析核心结论

- **10 项核心特征中，F3（ownership）、F4（effect 代数）、F5（串/并自适应）、F9（107 条验证）、F10（适配方法论）在所有已知先行工作中完全无覆盖**
- 最高威胁论文：SynQuake 2010（2.5/5），stage-based + barrier 但无 ownership / effect 代数
- 需重点 position 的工作：Cordeiro 2007（BSP 概念先驱）、Redmond OOPSLA 2025（同期 ECS 形式化，不同技术路径）
- 核心方法论差异：先行工作全部依赖"检测冲突"（锁/TM），目标系统通过 ownership + effect 代数"结构性消除冲突"

### GDC 发表价值核心结论

- GDC 2027 track 已从 "Programming" 改名为 "Game & Production Technology"
- 服务器端 tick 并行化在 GDC 历史上是**主题空白**
- SpacetimeDB (GDC 2025) 是最近的服务器端并发架构演讲，但走数据库事务路径（互补非竞争）
- P0 差距：benchmark + 端到端 combat path demo

### 适配性调研核心结论（107 条逻辑链路）

- **107 条逻辑链路无一被判定为无法适配（0% 无法适配）**
- 经典游戏：53% 直接适配，47% 需轻度妥协
- OpMap 真实业务：37.7% 直接适配，62.3% 需要妥协改造（但均有成熟模式）
- C1（单 owner 提交）100% 触及且 100% 满足——ownership 模型与游戏逻辑天然结构高度一致
- C3（barrier 可见性）是最常见妥协来源，95%+ 属于可容忍延迟
- C5（串行域）经典技能 0% 触及，OpMap 仅 15% 触及且全为低频基础设施操作
- 所有妥协的本质都可归结为时序延迟（C3），在 30Hz+ tick rate 下对玩家不可感知

### 适配分类指导手册核心结构

六大分类（基于底层原理）：
- **A. Owner 闭环**：逻辑完全在单 owner 内，直接适配
- **B. 跨 Owner 写模式**：B1 单向投递 / B2 请求-响应 / B3 资源预留 / B4 扇出广播
- **C. 快照时序延迟**：C-0 无敏感 / C-1 可容忍 / C-2 裁决迁移 / C-3 需即时可见
- **D. 无序安全性**：D-0 天然可交换 / D-1 批量化 / D-2 确定性排序
- **E. 级联收敛性**：E-0 单跳 / E-1 浅链 / E-2 深链 / E-3 潜在无界
- **F. 全局序列化**：收归 World Apply 串行执行

### 框架改进建议（来自调研）

- **高优先级**：Effect 分类扫描工具（C6 两阶段扫描高频刚需）、空间查询 API（WorldView 需版本化空间索引）
- **中优先级**：标准化投射物 Logic 模板、CC 效果标准化、untargetable/invulnerable 状态标准化
- **低优先级**：Signal 链路追踪（debug/tracing）

## Confirmed Decisions

### 协作流程

- 协作记忆统一在 `docs/memory/` 目录下，包含三个文件：`memory.md`、`tasks.md`、`todo.md`。

### Prior Art Novelty Analysis 结论（2015-2025）

- **2015-2025 年间没有工作完整覆盖目标系统的核心设计**：搜索覆盖 12+ 学术/工业工作，无任何单一或组合工作覆盖 6 个核心特征中的 3 个或更多。
- **最需要 position 的工作**：Redmond et al. (OOPSLA 2025) "Core ECS" 形式化模型——同期工作但不同路径（component-type disjoint access vs owner+commutativity）；SpatialOS authority 模型——分布式场景的 owner 类似实践但无 BSP/effect 代数。
- **Cordeiro BSP Quake (2005/2012)** 是唯一的 BSP 游戏服务器前驱，2015 年后无后续深化，应定位为启发性前驱。
- **F4（typed effect commutativity）、F5（自适应串并行切换）、F6（107 条链路适配验证）在所有搜索范围内无任何覆盖**。
- **新颖性来自六个特征的组合**：BSP Think/Apply 两阶段 + owner-based 分区 + typed effect commutativity + 自适应串并行 + 大规模适配验证，在搜索覆盖的文献中是唯一的。
- 详见 `docs/references/prior_art_novelty_analysis.md`。

### 框架适配性调研结论

- **Ownership 模型与游戏逻辑天然结构高度一致**：107 条逻辑链路 100% 可以明确归属真相 owner。
- **串行域在核心战斗/战略逻辑中不被需要**：经典技能 0% 触及 C5，OpMap 仅基础设施操作触及。
- **所有妥协的本质都是时序延迟（C3）**：在 30Hz+ tick rate 下对玩家不可感知。
- **框架语义提示词方案可行**：`docs/references/scheduler_analysis_prompt.md` 可有效指导 agent 适配判定。
- **适配分类指导手册已产出**：`docs/design/adaptation_guide.md` 提供 6 大底层原理分类 + 5 步判定流程 + 10 种改造模式速查。

### Parallel Tick 接口审计结论（已全部关闭）

**已修复的接口问题：**

1. **RefNone / IsValidRef**：已实现 `RefNone uint64 = 0` 和 `IsValidRef()`。
2. **WorldView 增强**：已增加 `Version() uint32` 和 `Round() int32`。

**确认为有意设计：**

3. **IsSerialRef >= RefWorld**：有意设计。Serial ref 使用 >= RefWorld 的地址空间，RefWorld 本身也是 serial 的一种。三类 ref 不是互斥分区，而是 normal (< RefWorld) vs serial (>= RefWorld)，其中 RefWorld 是 serial 的特殊成员。
4. **Publish 不区分 Entity/World Effect**：确认不改。单一 `Publish(ref, effect)` 足够，路由完全由 target ref 决定。
5. **Signal/Effect 无 source ref**：确认由用户 payload 自行携带。
6. **Effect 代数元数据**：确认不需要。实践中 effect/signal 不可合并（每次 effect 必须独立处理），且"顺序无关"是容忍性而非严格一致性，shuffle 验证不适用。详见代数化调研结论。
7. **Think 激活类型不分类**：确认不改。Inbox 空 = timer/frontier，非空 = signal。
8. **Apply→Emit 合法**：已通过 MaxSupersteps（parallel）和 maxDepth（serial）防护。
9. **Apply 无返回值**：确认不改。
10. **ThinkCtx 函数引用可被逃逸 / WorldView 可被类型断言写穿**：Go 语言限制，靠规范约束。

**确认由用户逻辑处理：**

11. **Logic 生命周期（Init/Dispose）**：不需要框架级接口。Logic 首次被调用时内部状态机自行识别并 Init；Dispose 随 Unit lifecycle 自然发生，timer wheel 弹出找不到的 refId 即为已销毁。
12. **LogicMeta**：由 `ScheduleMeta` 统一管理，不需要 Logic 接口暴露 `Meta()` 方法。

### Serial 模式设计决策

- **Truly inline**（非 collect-then-cascade）：Publish/Emit 原地触发 Apply/Think，不做 Think 输出的中间收集。
- **Apply 粒度差异已确认接受**：serial 模式下 Apply 每次收到单个 effect（vs parallel 模式的批量 Inbox）。
- **Logic 接口不变**：Serial/parallel 对 Logic 实现完全透明。
- **Depth 用栈变量追踪**：不嵌入 signal/effect 值，避免膨胀 parallel 路径的 refVal 结构体。

### WatchState 设计决策

- **WatchState 接口**：`Interest(SignalKind) bool`——抽象实现，不规定底层数据结构。
- **WorldView 暴露 WatchOf**：`WatchOf(uint64) WS` 允许 Logic 在 Think/Apply 中查询目标 watch 状态。
- **World vs WorldView 分层**：`World[WS]` 扩展 `WorldView[WS]`，增加 `GetWorldView()`。ThinkCtx 持有 `World`（完整访问），CommitCtx 持有 `WorldView`（只读访问）。
- **SetWatch 在 ThinkCtx 上**：Logic 调用 `ctx.SetWatch(ws)` 声明兴趣。不通过 Think 返回值传递——避免零值歧义，使 watch 更新可选。
- **BSP 一致性延迟更新（并发）**：per-thread watchCollectors → Think barrier 后 commitWatches → Apply 和下一轮 Think 看到更新 snapshot。
- **即时更新（串行）**：`SetWatch` 立即调用 `world.CommitWatches`（与串行模式 truly inline 语义一致）。
- **默认无 watch**：未调用 `SetWatch` 的 Logic 不接收 signal（必须显式声明兴趣）。
- **WatchCommitter 接口**：`CommitWatches(Inbox[RefWatch[WS]])`——World 实现批量 watch 提交。
- **Arrangement 已移除**：Apply 统一使用 `Inbox[E]`。`sliceInbox` 和 `refValInbox` 约束从 `SignalI` 泛化为 `any`。
- **Scheduler 5 个类型参数**：`Scheduler[W, S, E, L, WS]`。

### Signal/Effect 代数化调研结论（已确认关闭）

- **Effect/Signal 代数组合（框架级预合并）确认不做**：覆盖 Unreal GAS、Overwatch ECS、SpacetimeDB、Bevy、Unity DOTS 等所有主流引擎/框架，无一做框架级 effect 合并。
- **Commutativity ≠ Mergeability**：交换律（顺序无关）和可合并性是完全不同的概念。Effect 可交换不意味着可合并——每次 effect 必须独立产出视觉反馈、独立触发后续效果、独立携带来源信息。
- **F4 "typed effect commutativity" 的精确含义**：不是数学严格交换律（不要求不同顺序产出完全相同结果），而是开发者和玩家对不同顺序的处理结果保持**容忍**。这是并发 scheduler 能工作的核心前提。
- **Shuffle 验证不适用**：因为"顺序无关"是容忍性而非一致性，不同顺序本就可能产出不同中间结果，shuffle 测试无法判定正确性。
- **WatchState 已覆盖最高价值优化**：发射端过滤远优于投递端合并。
- **调研产出文件**：`docs/references/signal_algebra_research.md`、`docs/references/signal_event_algebra_research.md`。

### Scheduler 并发模型

- Think 阈值 500、并发 worker 5、最多 3 轮 superstep。参数统一放入 `ScheduleMeta`。
- Block-based effect 收集，sort-based 分组替代 map。CacheLinePad 隔离。
- Think 阶段 `blockId % Concurrency → threadId`（稳定映射）。Apply 阶段 LPT 动态分配。
- `getLogic` 由外部注入（`LogicProvider[L]` 接口）。无 per-logic 去重。双缓冲 Signal Collectors。

## Open Questions

- **GDC 投稿**：benchmark 设计方案（entity 规模梯度、热点场景、对比基线选择）。
- **GDC 投稿**：端到端 combat path demo 的技能/buff/伤害选型。
- **GDC 投稿**：补充搜索已知盲点（中文学术文献、专利库、NetGames/FDG/I3D 会议）。
- **GDC 投稿**：检查 Redmond OOPSLA 2025 论文的 Related Work 引用链。
- 是否选取 1-2 个妥协技能（如 Meepo 联动死亡、Guardian Spirit 死亡替代）做端到端原型验证。
- 是否将适配性分析扩展到非战斗系统（交易、社交、副本机制）以验证 P5 资源交换模式。
- 空间查询 API 的版本化语义如何在 WorldView 中体现。
- 外部输入注入 API：网络请求如何在 tick 开始前转化为 Signal。
- Worker pool 替代每 superstep 创建 goroutine（代码中已有 TODO 标注）。
- Prior art 补充搜索：Redmond OOPSLA 2025 的 related work 引用链、NetGames/FDG/I3D 会议、中文学术数据库。
- WatchState 实现选择：默认提供 bitset 实现还是由用户自行实现？是否需要框架级标准实现。

## Relevant Files

- `AGENTS.md`
- `sched/world.go`
- `sched/scheduler.go`
- `sched/scheduler_parallel.go`
- `sched/scheduler_serial.go`
- `sched/scheduler_test.go`
- `sched/wheel.go`
- `sched/wheel_test.go`
- `sched/block_collector.go`
- `sched/utils.go`
- `docs/design/parallel.md`
- `docs/design/scheduler.md`
- `docs/design/adaptation_guide.md`（适配分类指导手册）
- `docs/references/scheduler_analysis_prompt.md`（框架语义提示词）
- `docs/papers/novelty_and_value_analysis.md`（综合先行工作对比与 GDC 价值分析）
- `docs/references/prior_work_analysis.md`（7 篇先行论文逐篇分析）
- `docs/references/prior_art_novelty_analysis.md`（2015–2025 近年工作新颖性分析）

## Should

- 对任何算法议题可以创建 subagent 单独调研解决。
- 设计稿与代码出现矛盾时，以代码为准。

## Dont's

- 不要在 refVal 中嵌入 serial-only 的字段（如 depth），避免影响 parallel cache 效率。

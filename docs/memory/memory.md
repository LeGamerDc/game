# Memory

Last Updated: 2026-04-24

## Current Focus

Scheduler StagedState 重设计已完成首版实现，等待 review。性能 demo 暂时让位。

## Latest State

- **StagedState 已实现首版**：`sched` 已移除 `WatchState` / `WatchOf` / `CommitWatches`，改为 `StagedState` 类型参数、`ctx.WriteStage(ST)`、`RefStage[ST]`、`StagePromoter[ST].PromoteStages(...)`。
- **WriteStage owner 安全**：`WriteStage` 不提供 ref 参数；scheduler 在 Think/Apply 调用前通过闭包捕获当前 owner ref（parallel: `thinkRef` / `applyRef`；serial: 可恢复的 `stageRef`），防止 Logic 写其他 owner 的 staged state。
- **Promote 实现**：每个 worker 使用一个 `IndexMap[uint64, ST]` 收集 staged state；阶段 barrier 后串行 flatten 并调用 `PromoteStages`。并发路径在 Think→Apply、Apply→下一轮 Think 两处 promote；串行路径在 inline 阶段切换点 promote。
- **闭包 benchmark 结果**：`WriteStage` 闭包捕获 mutable ref 本身约 0.92ns/op；通过 ctx 函数字段调用约 2.29ns/op，成本可接受。
- **Scheduler 已实现**：代码在 `sched/` 包，包含并发/串行双模式、自动切换、timer wheel、block-based effect 收集、LPT 负载均衡、StagedState 机制。`go test ./...` 通过。
- **Think 调用合并优化已完成**：`thinkWorker`（并行）和 `serialProcess`（串行）都通过归并遍历（merge-iteration）timer refs + signal flatBuf，保证每个 logic 在初始 frontier 中最多一次 Think 调用。Timer 是纯唤醒机制，被 signal 吸收；串行模式初始 frontier 也做了 signal 批量化，两种模式语义一致。
- **接口定义**：`sched/world.go`（Logic、ThinkCtx、CommitCtx、World、StagedState、StagePromoter、Inbox 等）。
- **Scheduler 5 个类型参数**：`Scheduler[W, S, E, L, ST]`。Logic 4 个参数 `Logic[W, S, E, ST]`（WorldView 已移除）。
- **设计文档已对齐**：`docs/design/parallel.md`（概念与理论）、`docs/design/scheduler.md`（实现级设计）。
- **scheduler.md 新增"计算分解约束"章节**：任何依赖双方状态的公式必须分解为 Source 端函数和 Target 端函数，由 Effect 数据连接。
- **ability_system.md 完成第二版修订**：统一 Buff/Running 为 thinkable Buff interface、澄清 Modifier 定位、新增 Effect 数据设计指导、17 个开放问题已列出。
- **博客初稿已完成**：`docs/papers/blog_parallel_tick.md`（gamedeveloper.com 投稿），待性能数据验证后再提交。
- **GAS 调研完成**：`docs/references/gas_survey.md` + `docs/tmp/research_*.md`（属性、效果、能力、Cues/Targeting 四篇）。
- **适配分类指导手册已完成**：`docs/design/adaptation_guide.md`（6 大分类 + 107 条逻辑链路验证）。
- **旧 GAS 模式代码已归档**：`sched/engine_bak.go`（全部注释掉）。

## Confirmed Decisions

### 核心模型

- **Logic = Owner**：调度单位是 Logic，内部子逻辑组合是实现私有事务。
- **Typed Effect/Signal**：不用 closure，所有副作用都是显式的 typed 数据。
- **World 是特殊 Owner**：RefWorld 参与同一套 Apply 流程，不需要独立阶段。
- **Effect 顺序无关（容忍性）**：不是数学严格交换律，而是玩家和开发者对不同顺序的处理结果保持容忍。

### Scheduler 实现

- Think 阈值 500、并发 worker 5、最多 3 轮 superstep（`ScheduleMeta` 管理）。
- Block-based effect 收集，sort-based 分组。CacheLinePad 隔离。
- Think 阶段 `blockId % Concurrency → threadId`（稳定映射）。Apply 阶段 LPT 动态分配。
- 串行模式 truly inline（递归闭包，非 collect-then-cascade）。
- 双缓冲 Signal Collectors。无 per-logic 去重。
- 每个 logic 每个 superstep 最多一次 Think 调用（归并遍历 timer+signal）。Timer 无数据，被 signal 吸收；纯 timer → Think(nil)，有 signal → Think(signals)。串行模式初始 frontier 也做信号批量化，与并行模式语义一致。
- StagedState：`WriteStage(ST)` 提交当前 owner 的阶段稳定状态，阶段边界 `PromoteStages` 串行提交；WatchState 移出 scheduler runtime，作为 framework 层 staged state 用例。
- **WorldView 已移除**：Think 和 Apply 阶段使用同一 W 类型（约束为 `World + LogicProvider + StagePromoter`）。原 WorldView 隔离价值极低（仅阻止调用 GetWorldView()），且无法通过 type parameter 注入自定义受限类型。移除后消除了 interface boxing 开销。

### GAS 设计决策

- **Buff 和 Running 统一**：统一为 thinkable Buff interface（ID/OnApply/OnRemove/OnStack/Think），由 BuffTable 管理。Running ability 实现为持续型 Buff，消除了独立的 Running 概念。
- **Modifier 是 AttrTable 内部的贡献记录**：不是独立实体。生命周期由 Buff.OnApply/OnRemove 管理（添加/移除 Modifier），AttrTable 负责聚合计算。
- **计算分解约束**：任何依赖双方状态的公式必须分解为 Source 端函数和 Target 端函数，由 Effect 数据连接。这是 parallel tick 的核心约束。
- **Effect 数据设计**：携带中间结果（如 rawDamage）+ 少量 source 参数（如 penetration、element），不携带 source 全部状态。Source 端在 Think 阶段计算并打包，Target 端在 Apply 阶段用自身状态完成最终计算。
- **Scheduler 协议层无需变更**：GAS 工作集中在 Logic 内部架构，不需要新的协议原语。所有 GAS 概念要么映射为 Effect/Signal 数据，要么是 Logic 内部的私有实现细节。
- **GAS 作为构建块**：AttrTable + BuffTable + AbilitySet + TagState 四个独立模块，由用户组装为 AbilitySystem，不是侵入式框架。
- **attr.toml 显式 field ID**：attr.toml 使用显式 field ID（`{ id = N, type = "instant"|"attribute" }`），不再依赖 list 顺序；生成的 Go 代码使用显式常量值而非 iota；struct 仍按 instant/attribute 分组排列。

### 已关闭的设计方向

- **Effect/Signal 代数组合（框架级预合并）**：确认不做。Commutativity ≠ Mergeability。
- **Shuffle 验证**：不适用，"顺序无关"是容忍性。
- **Per-logic LogicMeta**：由 ScheduleMeta 统一管理，不需要 Logic 接口暴露 Meta()。
- **Logic 生命周期（Init/Dispose）**：不需要框架级接口，Logic 自行管理。
- **WorldView 接口隔离**：已移除。Think/Apply 统一使用 W World，隔离由约束系统保证（Logic 内无法调用 GetLogic 等非 World 方法）。

## Open Questions

### Scheduler 层

- Framework 层具体如何用 `StagedState` 实现双缓冲 WatchState/订阅表：dirty mirror、全量 copy、结构共享三种策略尚未落定。
- 是否需要提供标准 StagedState helper（如 WatchBits、AOI membership、AttrSummary）仍未决定。
- 阶段稳定数据的通用抽象：除 WatchState 外，是否也覆盖订阅表、空间索引 membership、可见性/AOI、派生 public summary、dirty attribute projection。
- 空间查询 API：World 需提供版本化只读空间索引接口。
- 外部输入注入 API：网络请求如何在 tick 开始前转化为 Signal。
- Worker pool：替代每 superstep 创建 goroutine（代码中已有 TODO）。

### GAS 设计层

- Modifier Channel 数量：初版是否只支持单通道？（倾向单通道，预留扩展）
- AttrTable 的 Public State 暴露方式：World 如何读取 Current？（需与空间查询 API 一起设计）
- 死亡判定的位置：Apply Flush 中检测 HP<=0 → Emit 死亡 Signal？还是 Think 中？
- AbilitySystem 应该有多"薄"：编排器 vs 仅持有引用（倾向薄层编排器）
- Buff 的 Value 与 StackCount 的关系：固定线性 vs 自定义函数（倾向默认线性 + 可扩展）
- Buff 跨实体交互（荆棘反伤等）：Buff.Think 返回 action 列表由 Logic 转发 vs 扩展 BuffCtx 提供 Publish
- Buff 序列化/存档：类型注册表 + Buff ID → factory 映射

### GAS 实现层

- AttrTable 索引方式：int32 kind → dense array（属性数量通常 <50）
- BuffTable min-heap 实现：复用 `lib/` 还是参考 HeapIndexMap
- TagState 与 `tag/` 包的集成：层级匹配用 tag/，精确匹配用简单 map
- GAS 包的位置：`gas/` 顶层 vs `en/gas/`
- 泛型参数：AttrTable/BuffTable 具体类型 vs AbilitySystem 泛型化
- Stock Buff 的 PeriodicAction 回调签名

### 与其他系统的关系

- 弹道 Logic 模板：spawn/fly/collide/destroy 与 GAS 交互
- CC 效果标准化：Kind/Priority/Tenacity 体系
- 行为树（bt/）与 GAS 集成：NPC AI 如何调用 AbilitySet

## Relevant Files

- `AGENTS.md`
- `sched/world.go`
- `sched/scheduler.go`
- `sched/scheduler_parallel.go`
- `sched/scheduler_serial.go`
- `sched/scheduler_test.go`
- `sched/wheel.go`
- `sched/block_collector.go`
- `sched/utils.go`
- `docs/design/parallel.md`
- `docs/design/scheduler.md`
- `docs/design/adaptation_guide.md`
- `docs/design/ability_system.md`
- `docs/references/gas_survey.md`
- `docs/papers/blog_parallel_tick.md`
- `gas/attribute.go`
- `tools/mk_attr/main.go`
- `tools/mk_attr/main_test.go`
- `demo/cfg/attr.toml`
- `demo/Makefile`
- `demo/demo_attr.go`

## Should

- 对任何算法议题可以创建 subagent 单独调研解决。
- 设计稿与代码出现矛盾时，以代码为准。

## Dont's

- 不要在 refVal 中嵌入 serial-only 的字段（如 depth），避免影响 parallel cache 效率。

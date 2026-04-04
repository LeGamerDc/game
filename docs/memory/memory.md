# Memory

Last Updated: 2026-04-03

## Current Focus

Scheduler 设计与实现已完成，进入 **game ability system** 开发阶段（基于 Scheduler 框架实现技能/buff/伤害结算）。

## Latest State

- **Scheduler 已实现**：代码在 `sched/` 包，包含并发/串行双模式、自动切换、timer wheel、block-based effect 收集、LPT 负载均衡、WatchState 机制。35 个测试全部通过。
- **接口定义**：`sched/world.go`（Logic、ThinkCtx、CommitCtx、World/WorldView、WatchState、Inbox 等）。
- **Scheduler 5 个类型参数**：`Scheduler[W, S, E, L, WS]`。
- **设计文档已对齐**：`docs/design/parallel.md`（概念与理论）、`docs/design/scheduler.md`（实现级设计）。
- **适配分类指导手册已完成**：`docs/design/adaptation_guide.md`（6 大分类 + 107 条逻辑链路验证）。
- **GDC 先行工作分析已完成**：新颖性确认、价值评估为 Competitive，产出在 `docs/papers/` 和 `docs/references/`。
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
- WatchState：发射端过滤，BSP 一致性延迟更新（并发）/ 即时更新（串行）。

### 已关闭的设计方向

- **Effect/Signal 代数组合（框架级预合并）**：确认不做。Commutativity ≠ Mergeability。
- **Shuffle 验证**：不适用，"顺序无关"是容忍性。
- **Per-logic LogicMeta**：由 ScheduleMeta 统一管理，不需要 Logic 接口暴露 Meta()。
- **Logic 生命周期（Init/Dispose）**：不需要框架级接口，Logic 自行管理。

## Open Questions

- 空间查询 API：WorldView 需提供版本化只读空间索引接口。
- 外部输入注入 API：网络请求如何在 tick 开始前转化为 Signal。
- Worker pool：替代每 superstep 创建 goroutine（代码中已有 TODO）。
- WatchState 实现选择：默认提供 bitset 还是由用户自行实现。
- Game ability system 如何在 Logic 内部组织子逻辑（技能系统、buff 系统、伤害结算的内部架构）。

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

## Should

- 对任何算法议题可以创建 subagent 单独调研解决。
- 设计稿与代码出现矛盾时，以代码为准。

## Dont's

- 不要在 refVal 中嵌入 serial-only 的字段（如 depth），避免影响 parallel cache 效率。
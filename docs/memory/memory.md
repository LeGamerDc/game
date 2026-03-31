# Memory

Last Updated: 2025-07-27

## Current Focus

- Scheduler 开发已完成（parallel + serial 双模式），**框架适配性验证阶段已完成两轮调研 + 分类指导文档**。
- 两轮调研覆盖 107 条逻辑链路：30 条经典游戏技能（LOL/DOTA2/WOW）+ 77 条 OpMap 真实业务（8 个子系统）。
- 已产出适配分类指导手册 `docs/design/adaptation_guide.md`，基于底层原理（owner 闭环、跨 owner 写模式、快照时序、无序安全、级联收敛、全局序列化）建立 6 大分类 + 子分类，配合 5 步判定流程和改造模式速查表。
- 待决：是否选取 1-2 个妥协场景做端到端原型验证；是否进入实际代码改造阶段。

## Latest State

- Scheduler 实现完成，代码在 `en/scheduler*.go`，35 个测试全部通过。
- 适配性调研全部完成，产出文件：
  - `docs/references/scheduler_analysis_prompt.md`：框架语义提示词（可复用）
  - `docs/tmp/lol_skills_analysis.md`：LOL 10 个技能分析
  - `docs/tmp/dota2_skills_analysis.md`：DOTA2 10 个技能分析
  - `docs/tmp/wow_skills_analysis.md`：WOW 10 个技能分析
  - `docs/tmp/summary_analysis.md`：跨游戏总结分析
  - OpMap 真实业务分析（8 份子报告 + 1 份最终总结，位于外部目录）
- **适配分类指导手册已完成**：`docs/design/adaptation_guide.md`

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
- **低优先级**：per-logic budget（LogicMeta）、Signal 链路追踪（debug/tracing）

## Confirmed Decisions

### 协作流程

- 协作记忆统一在 `docs/memory/` 目录下，包含三个文件：`memory.md`、`tasks.md`、`todo.md`。

### 框架适配性调研结论

- **Ownership 模型与游戏逻辑天然结构高度一致**：107 条逻辑链路 100% 可以明确归属真相 owner。
- **串行域在核心战斗/战略逻辑中不被需要**：经典技能 0% 触及 C5，OpMap 仅基础设施操作触及。
- **所有妥协的本质都是时序延迟（C3）**：在 30Hz+ tick rate 下对玩家不可感知。
- **框架语义提示词方案可行**：`docs/references/scheduler_analysis_prompt.md` 可有效指导 agent 适配判定。
- **适配分类指导手册已产出**：`docs/design/adaptation_guide.md` 提供 6 大底层原理分类 + 5 步判定流程 + 10 种改造模式速查。

### Parallel Tick 接口审计结论

**需要修复的接口问题：**

1. **Ref 空间歧义**：`IsSerialRef(RefWorld) == true`，三类 ref 不互斥。需要明确互斥分区。
2. **Publish 不区分 Entity/World Effect**：需要拆分或加 domain 标记。

**确认不改的设计点（3-10）**：WorldView 极简、Signal/Effect source ref 用户管理、代数模型推迟、Budget/Meta 不进 Logic 接口、Apply→Emit 合法、Timer 冲突 Logic 处理、Think 激活类型不分类、Ack 内嵌 Think/private state。

### Serial 模式设计决策

- **Truly inline**（非 collect-then-cascade）：Publish/Emit 原地触发 Apply/Think，不做 Think 输出的中间收集。
- **Apply 粒度差异已确认接受**：serial 模式下 Apply 每次收到单个 effect（vs parallel 模式的批量 Arrangement）。
- **Logic 接口不变**：Serial/parallel 对 Logic 实现完全透明。
- **Depth 用栈变量追踪**：不嵌入 signal/effect 值，避免膨胀 parallel 路径的 refVal 结构体。

### Scheduler 并发模型

- Think 阈值 500、并发 worker 5、最多 3 轮 superstep。参数统一放入 `ScheduleMeta`。
- Block-based effect 收集，sort-based 分组替代 map。CacheLinePad 隔离。
- Think 阶段 `blockId % Concurrency → threadId`（稳定映射）。Apply 阶段 LPT 动态分配。
- `getLogic` 由外部注入（`LogicProvider[L]` 接口）。无 per-logic 去重。双缓冲 Signal Collectors。

## Open Questions

- 是否选取 1-2 个妥协技能（如 Meepo 联动死亡、Guardian Spirit 死亡替代）做端到端原型验证。
- 是否将适配性分析扩展到非战斗系统（交易、社交、副本机制）以验证 P5 资源交换模式。
- Effect 分类扫描工具的具体 API 设计（C6 两阶段扫描模式）。
- 空间查询 API 的版本化语义如何在 WorldView 中体现。
- Logic 生命周期方法（Init/Dispose）是否需要加入接口。
- LogicMeta 如何暴露给调度器。
- ThinkCtx 函数引用可被 Logic 逃逸存储——Go 限制，只能靠规范。
- 外部输入注入 API：网络请求如何在 tick 开始前转化为 Signal。
- Worker pool 替代每 superstep 创建 goroutine（TODO 已标注）。
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
- `docs/design/parallel.md`
- `docs/design/scheduler.md`
- `docs/design/adaptation_guide.md`（**适配分类指导手册**，6 大分类 + 判定流程 + 改造模式速查）
- `docs/references/scheduler_analysis_prompt.md`（框架语义提示词）
- `docs/tmp/lol_skills_analysis.md`（LOL 适配分析）
- `docs/tmp/dota2_skills_analysis.md`（DOTA2 适配分析）
- `docs/tmp/wow_skills_analysis.md`（WOW 适配分析）
- `docs/tmp/summary_analysis.md`（跨游戏总结分析）

## Should

- 对任何算法议题可以创建 subagent 单独调研解决。
- 设计稿与代码出现矛盾时，以代码为准。

## Dont's

- 不要在 refVal 中嵌入 serial-only 的字段（如 depth），避免影响 parallel cache 效率。
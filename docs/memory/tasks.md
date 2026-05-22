# Tasks

Last Updated: 2026-05-13

## Active

（无）

## Backlog

- [ ] Demo benchmark 后续分析：整理 benchstat/pprof 数据，定位 allocation 热点，并评估 worker pool 对并发路径的影响
- [ ] BT Game Developer 投稿准备：英文 Markdown 初稿与本地图像已完成，待审稿并决定是否补 benchmark/pprof 数据
- [ ] Demo combat 后续扩展：补充更复杂技能配置、投射物 Logic、公开 tag/CC、SerialRef 全局结算等非 MVP 能力
- [ ] gamedeveloper.com 博客投稿：初稿已完成（`docs/papers/blog_parallel_tick.md`），待性能数据后投稿
- [ ] GDC 投稿准备：先行工作分析与价值评估已完成，待 benchmark + demo
- [ ] 设计空间查询 API：World 需提供版本化只读空间索引接口
- [ ] 设计外部输入注入点：网络请求如何在 tick 开始前转化为 Signal
- [ ] 标准化投射物 Logic 模板：spawn/fly/collide/destroy 生命周期
- [ ] 标准化 CC 效果体系：CC Effect Kind + Apply 端状态机 + 优先级仲裁
- [ ] 替换 parallelThink/parallelApply 中每 superstep 创建 goroutine 为预分配 worker pool

## Blocked

（无）

## Done

- [x] Sched + demo 性能验证：新增 benchmark 技能组合与 `BenchmarkGridCombatScheduler`，完成 16x16/32x32/84x84 串行、parallel-4、parallel-8 初步对比；`go test ./...`、`go test -race ./demo/...`、三轮短 benchmark 通过 (2026-05-13)
- [x] BT Game Developer 投稿初稿与本地图像：新增 `docs/papers/bt_stack_runtime_submission.md`，引用 `docs/papers/assets/bt_stack_runtime/` 下 1 张 imagegen 头图与 7 张本地 PNG 技术图，正文按架构拆解稿处理性能边界 (2026-05-10)
- [x] BT Game Developer 投稿组织结构：新增 `docs/papers/bt_stack_runtime_article_outline.md`，确定 technical breakdown 结构、核心 claim、章节顺序、图示规划、伪代码控制策略和投稿前清单 (2026-05-10)
- [x] BT 投稿前 bug/设计缺陷修复：修复 `joinBranch.OnEvent` next wake 汇总，补 `NewRepeatUntilNSuccess` 参数校验，明确 `Node.Check` 浅校验、`TaskStatus` 编码与 `Root.SetNode` 空栈约束，`go test ./...` 通过 (2026-05-10)
- [x] BT Game Developer 投稿前审计：完成 `bt/` 实现 bug/设计漏洞、创新性、投稿适配度分析，产出 `bt/tmp_gamedeveloper_audit.md`；确认当前不宜直接投稿，需先修 P0 与补 benchmark (2026-05-10)
- [x] BT 手动栈 runtime 投稿核心设计：调研 prior art 后确认手动 traversal stack 本身已有相邻提出，但 `bt/` 可主张 stack-complete BT runtime 架构；新增 `docs/design/bt_stack_runtime_article.md` 并更新 `docs/INDEX.md` (2026-05-10)
- [x] Demo combat/ability MVP 实现：新增 `demo/combat` 与 `demo/scenario`，实现 Unit/World/Stage query、普通攻击、技能队列、被动触发、buff modifier、死亡复活和串行/并行集成测试，`go test ./...` 通过 (2026-05-09)
- [x] Demo combat/ability 框架设计稿：新增 `docs/design/demo_combat_framework.md`，沉淀 Unit、普通攻击、技能槽、被动、buff、死亡复活与 sched 接入边界 (2026-05-09)
- [x] Scheduler demo 接入文档：新增 `sched/integration.md`，沉淀 public/private data 访问模式、Effect/Signal/StagedState 接入边界与 SerialRef apply-only 语义 (2026-04-27)
- [x] GAS / Attribute 边界重构：删除 `game/` 内完整 `gas/` framework 草稿，新增 `attr/` runtime 与 `attr/cmd/mk_attr`，demo 属性改为 `attr.Value`，`go test ./...` 通过 (2026-04-25)
- [x] Scheduler StagedState 多域设计与实现：移除 `ST` 类型参数，改为 `StageKind` + `StagedState any` + `(ref, kind)` last-write-wins，新增多 kind 测试，`go test ./...` 通过 (2026-04-25)
- [x] Scheduler StagedState 重设计首版实现：WatchState 移出 runtime，新增 `WriteStage` / `PromoteStages`，并发/串行路径阶段 promote，闭包 benchmark 与 `go test ./...` 通过 (2026-04-24)
- [x] mk_attr 显式 field ID 改造 + demo Makefile：TOML 格式改为 { id, type }，代码生成使用显式 ID，demo Makefile gen-attr target，21 个测试通过 (2026-07-15)
- [x] Think 调用合并优化：thinkWorker/serialProcess 归并遍历 timer+signal，每个 logic 每个 superstep 最多一次 Think 调用；串行模式初始 frontier 信号批量化；44 个测试通过 (2026-04-08)
- [x] Scheduler 设计与实现：并发/串行双模式、自动切换、timer wheel、block-based 分组、LPT 负载均衡、WatchState、35 个测试通过 (2026-04-01)
- [x] tag.Query 编译态优化：构造期层级归一化、冗余消除、冲突检测；运行时单 slice + boundary + kind mask 分派 (2026-04-03)
- [x] Signal/Effect 代数化调研：确认不做框架级 effect 合并，澄清 F4 commutativity 为容忍性 (2026-04-03)
- [x] WatchState 机制实现：发射端过滤、BSP 一致性延迟更新、即时更新、Arrangement 移除 (2026-04-01)
- [x] 2015-2025 Prior Art & Novelty Analysis：12+ 工作分析，无实质新颖性威胁 (2025-07)
- [x] 适配性分类指导手册：6 大分类、107 条逻辑链路验证 (2025-07-28)
- [x] 经典游戏技能适配性调研：LOL/DOTA2/WOW 30 个技能分析 (2025-07-27)
- [x] Parallel tick 接口审计与逐条讨论：10 类问题全部关闭 (2025-07-27)
- [x] 串行模式实现：truly inline + depth 追踪 + 模式路由 (2026-03-30)
- [x] Scheduler 并行 tick 实现：ProcessTick + Think/Apply 并行 + LPT + signal routing (2026-03-29)
- [x] timerWheel 重构：Unified Log + Epoch-based Lazy Clear (2026-03-29)
- [x] Scheduler 并发模型设计：产出 docs/design/scheduler.md (2026-03-28)
- [x] 初始化协作记忆和 AGENTS.md (2025-03-27)

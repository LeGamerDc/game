# Tasks

Last Updated: 2026-04-03

## Active

- [ ] GDC 投稿准备：先行工作分析与价值评估（新颖性确认 + GDC 竞争力评估）
- [ ] 性能 Benchmark：串行 vs 并行 vs 自适应，不同 entity 数量扩展性曲线，热点 owner Apply 并行度
- [ ] 端到端 Combat Path Demo：至少一个完整技能→伤害→buff→死亡链路在框架内运行


## Backlog

- [ ] 设计空间查询 API：WorldView 需提供版本化只读空间索引接口（适配调研高优建议）
- [ ] 标准化投射物 Logic 模板：spawn/fly/collide/destroy 生命周期（适配调研中优建议）
- [ ] 标准化 CC 效果体系：一组 CC Effect Kind + Apply 端 CC 状态机 + 优先级仲裁（适配调研中优建议）
- [ ] 标准化 untargetable/invulnerable 状态 flag：确保 WorldView 查询正确过滤（适配调研中优建议）
- [ ] 选取 1-2 个妥协技能做端到端原型验证（如 Meepo 联动死亡、Guardian Spirit 死亡替代）
- [ ] 将适配性分析扩展到非战斗系统（交易、社交、副本机制）以验证 P5 资源交换模式
- [ ] 选取 1-2 个分类（如 B2 请求-响应、D-1 批量化）做端到端原型验证，确认适配指导手册的实操可行性
- [ ] 替换 parallelThink/parallelApply 中每 superstep 创建 goroutine 为预分配 worker pool
- [ ] 评估现有 timer wheel 的"剩余 delay 重注册"语义与更多边界测试
- [ ] 设计外部输入注入点：网络请求如何在 tick 开始前转化为 Signal 进入对应 Logic 的 inbox

## Blocked

(none)

- [ ] 手动补充搜索 Redmond OOPSLA 2025 的 related work 引用链、NetGames/FDG/I3D 会议论文、中文学术数据库

## Done

- [x] Signal/Effect 代数化调研：覆盖 Unreal GAS、Overwatch ECS、SpacetimeDB、Bevy 等主流引擎，确认无一做框架级 effect/signal 合并；澄清 F4 commutativity 含义为"容忍性"而非数学严格交换律；确认 Commutativity ≠ Mergeability；关闭代数合并方向。产出 `docs/references/signal_algebra_research.md`、`docs/references/signal_event_algebra_research.md` (2026-04-03)
- [x] WatchState 机制实现：Logic 声明感兴趣的 SignalKind，发射方可查询目标 watch 状态实现发射端过滤。`WatchState` 接口 + `WorldView.WatchOf` 查询 + `ThinkCtx.SetWatch` 声明 + BSP 一致性延迟更新（并发）/ 即时更新（串行）+ `WatchCommitter` 批量提交 + Arrangement 移除（Apply 统一使用 `Inbox[E]`）+ Scheduler 5 类型参数。代码文件：`sched/world.go`、`sched/scheduler.go`、`sched/scheduler_parallel.go`、`sched/scheduler_serial.go`、`sched/utils.go`、`sched/scheduler_test.go`（35 个测试全部通过） (2026-04-01)
- [x] 2015-2025 Prior Art & Novelty Analysis：搜索 7 个方向（游戏服务器并行化、ECS 并行执行、并行仿真、ownership 模型、Quake 后续、引擎并发架构、工业实践），分析 12+ 工作，结论：无实质新颖性威胁；Redmond OOPSLA 2025 和 SpatialOS 需重点 position；产出 `docs/references/prior_art_novelty_analysis.md` (2025-07)
- [x] 审计问题回顾与清理：10 个审计问题中 7 个已解决或确认不改，3 个确认为有意设计或已由现有机制覆盖；清理 Active/Backlog/Blocked 中的过时条目 (2026-03-31)
- [x] 适配性分类指导手册：基于 107 条逻辑链路（30 经典技能 + 77 OpMap 业务）提炼 6 大底层原理分类（A/B1-B4/C/D/E/F），产出 `docs/design/adaptation_guide.md` (2025-07-28)
- [x] 经典游戏技能适配性调研：分析 LOL/DOTA2/WOW 共 30 个技能，53% 直接适配、47% 需轻度妥协、0% 无法适配；生成框架语义分析提示词 `docs/references/scheduler_analysis_prompt.md` (2025-07-27)
- [x] 实现串行模式（scheduler_serial.go）及 ProcessTick 模式路由：truly inline 执行（thinkSignal/thinkTimer/applyOne 递归闭包）、栈变量 depth 追踪、countWork 替代 hasWork、parallel→serial 单向切换、blockToThread timer 一致性、14 个 serial 测试 + 21 个 parallel 测试全部通过含 race detector (2026-03-30)
- [x] Scheduler review 修复：sort-based 分组替代 map 分组（消除 signalGroupBufs/groupBufs 的无限膨胀），新增 refValInbox/refValArrangement 适配器和 collectBuf（CacheLinePad 隔离），26 个测试通过含 race detector (2026-03-30)
- [x] Scheduler 重构：getLogic 注入 + 双缓冲 signal collectors + 去除 per-logic 去重 (2026-03-29)
- [x] Scheduler 并行 tick 实现：ProcessTick superstep 循环、Think/Apply 并行、LPT 负载均衡、signal routing、timer wheel 集成 (2026-03-29)
- [x] timerWheel 重构：Unified Log + Epoch-based Lazy Clear (2026-03-29)
- [x] WorldView 增加 `Round()` 只读观测接口 (2026-03-28)
- [x] Scheduler 数据结构实现 + scheduler_test.go (2026-03-28)
- [x] Scheduler 并发模型设计：产出 `docs/design/scheduler.md` (2026-03-28)
- [x] 完成 `parallel.md` + `world.go` 的深度接口审计，产出 10 类问题 (2025-07-27)
- [x] 逐条讨论审计结果，区分"真问题 / 推迟 / 不改" (2025-07-27)
- [x] 初始化协作记忆和 AGENTS.md (2025-03-27)
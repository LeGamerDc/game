# Tasks

Last Updated: 2026-03-29

## Active

- [ ] 修复 Ref 空间歧义：`IsSerialRef(RefWorld) == true` 导致三类 ref 不互斥；补充 `RefNone` 常量和 `IsValidRef` 判断
- [ ] 解决 Publish 不区分 Entity/World Effect：考虑拆分为两个函数或在 `EffectI` 上增加 `TargetDomain` 方法做运行时校验
- [ ] 实现串行模式（cascade depth）及 processTick 模式路由：frontier < ThinkConcurrencyThreshold 时切换到串行路径，串行 cascade depth 预算 = MaxSupersteps - 已完成并发轮次

## Backlog

- [ ] 替换 parallelThink/parallelApply 中每 superstep 创建 goroutine 为预分配 worker pool
- [ ] 评估现有 timer wheel 的"剩余 delay 重注册"语义与更多边界测试
- [ ] 设计外部输入注入点：网络请求如何在 tick 开始前转化为 Signal 进入对应 Logic 的 inbox（调度器设计前置依赖）
- [ ] 讨论 Logic 生命周期：是否需要 Init/Dispose 接口，spawn/despawn 时的初始化和清理时机
- [ ] 讨论 LogicMeta 如何暴露给调度器：设计文档描述了 `max_effects_per_activation`、`priority`、`serial_only` 等元数据，但 `Logic` 接口没有 `Meta()` 方法
- [ ] 决定 `docs/design/feedback.md` 的归属：并入主设计文档还是保留为历史评审记录
- [ ] 考虑 `engine.go` 中 GAS 模式与新并行模型的迁移隔离策略

## Blocked

- [ ] Effect 代数模型设计 — 依赖调度器设计成型 + effect 类型调研完成

## Done

- [x] Scheduler 重构：getLogic 注入 + 双缓冲 signal collectors + 去除 per-logic 去重。消除 threadInboxes/threadFrontiers/pendingInbox/routeSignals/buildInitialFrontier/deferRemainingInboxes，改为 signalRead/signalWrite 双缓冲 swap + clear，溢出信号自动保留在 signalRead 延迟到下一 tick。Logic 查找改为外部注入 getLogic func(uint64)(L,bool)。Scheduler 不再保证同一 logic 在同一 superstep 只 Think 一次 (2026-03-29)
- [x] Scheduler 并行 tick 实现：ProcessTick superstep 循环、Think/Apply 并行、LPT 负载均衡、signal routing、timer wheel 集成、per-thread inbox 隔离、溢出延迟，19 个测试通过含 race detector (2026-03-29)
- [x] timerWheel 重构：Unified Log + Epoch-based Lazy Clear，merge O(registrations)、advance O(threads) (2026-03-29)
- [x] WorldView 增加 `Round()` 只读观测接口，用于 tracing/调试时标记 superstep 轮次 (2026-03-28)
- [x] Scheduler 数据结构实现：在 `en/schedule.go` 落地 serial/parallel runner、block-based effect 聚合、worker-local timerRegs、timer wheel、ready/inbox/frontier 管理，并补充 `en/schedule_test.go` (2026-03-28)
- [x] 明确 timer wheel 语义：不支持移除 wheel 中旧 timer；局部 cancel 仅作用于本 tick 未 merge 的 thread-local 注册；超长 delay 需要 clamp 到最远 slot (2026-03-29)
- [x] 优化 timer wheel 的 merge/advance：在 `newTimerWheel` 传入 thread 对应的 block 列表；每个 thread 的本地 collector 按全量 block 预分配 slot，set 直接使用全局 blockId，merge/advance 只遍历 thread 负责的 block (2026-03-29)
- [x] 为 `en/wheel.go` 补充 `wheel_test.go`，覆盖全局 blockId 写入、delay clamp、pre/post-merge cancel、advance 清理与同槽去重，并通过 `go test ./en` (2026-03-29)
- [x] Scheduler 并发模型设计：完成并发/串行双模式、block-based effect 收集、per-worker timer 注册、cascade depth、模式切换等设计，产出 `docs/design/scheduler.md` (2026-03-28)
- [x] 完成 `parallel.md` + `world.go` 的深度接口审计，产出 10 类问题 (2025-07-27)
- [x] 逐条讨论审计结果，区分"真问题 / 推迟 / 不改"，达成共识 (2025-07-27)
- [x] 在 `AGENTS.md` 补充项目结构、docs 规则、总结流程和 memory 维护约定 (2025-03-27)
- [x] 初始化 `docs/memory/memory.md` 和 `docs/memory/todo.md` (2025-03-27)
- [x] 重构 memory 目录结构：memory.md 移入 memory/，todo 拆分为 tasks.md + todo.md，AGENTS.md 增加读取规则 (2026-03-28)
# Tasks

Last Updated: 2026-03-30

## Active

- [ ] 修复 Ref 空间歧义：`IsSerialRef(RefWorld) == true` 导致三类 ref 不互斥；补充 `RefNone` 常量和 `IsValidRef` 判断
- [ ] 解决 Publish 不区分 Entity/World Effect：考虑拆分为两个函数或在 `EffectI` 上增加 `TargetDomain` 方法做运行时校验
- [ ] 更新 `docs/design/scheduler.md` 串行模式伪代码：当前伪代码描述 collect-then-cascade，需改写为 truly inline 设计以匹配代码实现

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
# Tasks

Last Updated: 2026-03-28

## Active

- [ ] 修复 Ref 空间歧义：`IsSerialRef(RefWorld) == true` 导致三类 ref 不互斥；补充 `RefNone` 常量和 `IsValidRef` 判断
- [ ] 解决 Publish 不区分 Entity/World Effect：考虑拆分为两个函数或在 `EffectI` 上增加 `TargetDomain` 方法做运行时校验
- [ ] 评估 WorldView 是否加 `Round() int`，用于 tracing/调试时标记 superstep 轮次

## Backlog

- [ ] 设计外部输入注入点：网络请求如何在 tick 开始前转化为 Signal 进入对应 Logic 的 inbox（调度器设计前置依赖）
- [ ] 讨论 Logic 生命周期：是否需要 Init/Dispose 接口，spawn/despawn 时的初始化和清理时机
- [ ] 讨论 LogicMeta 如何暴露给调度器：设计文档描述了 `max_effects_per_activation`、`priority`、`serial_only` 等元数据，但 `Logic` 接口没有 `Meta()` 方法
- [ ] 决定 `docs/design/feedback.md` 的归属：并入主设计文档还是保留为历史评审记录
- [ ] 考虑 `engine.go` 中 GAS 模式与新并行模型的迁移隔离策略

## Blocked

- [ ] Effect 代数模型设计 — 依赖调度器设计成型 + effect 类型调研完成

## Done

- [x] 完成 `parallel.md` + `world.go` 的深度接口审计，产出 10 类问题 (2025-07-27)
- [x] 逐条讨论审计结果，区分"真问题 / 推迟 / 不改"，达成共识 (2025-07-27)
- [x] 在 `AGENTS.md` 补充项目结构、docs 规则、总结流程和 memory 维护约定 (2025-03-27)
- [x] 初始化 `docs/memory/memory.md` 和 `docs/memory/todo.md` (2025-03-27)
- [x] 重构 memory 目录结构：memory.md 移入 memory/，todo 拆分为 tasks.md + todo.md，AGENTS.md 增加读取规则 (2026-03-28)
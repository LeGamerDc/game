# Tasks

Last Updated: 2026-04-08

## Active

- [ ] 性能 Demo 开发：验证 scheduler 并行性能，为博客投稿提供数据支撑
- [ ] 性能 Benchmark：串行 vs 并行 vs 自适应，不同 entity 数量扩展性曲线
- [ ] Game Ability System 设计：设计草稿第二版修订已完成，进入讨论确认阶段（确认设计方向 + 解决开放问题）

## Backlog

- [ ] gamedeveloper.com 博客投稿：初稿已完成（`docs/papers/blog_parallel_tick.md`），待性能数据后投稿
- [ ] 端到端 Combat Path Demo：至少一个完整技能→伤害→buff→死亡链路在框架内运行
- [ ] GDC 投稿准备：先行工作分析与价值评估已完成，待 benchmark + demo
- [ ] 设计空间查询 API：WorldView 需提供版本化只读空间索引接口
- [ ] 设计外部输入注入点：网络请求如何在 tick 开始前转化为 Signal
- [ ] 标准化投射物 Logic 模板：spawn/fly/collide/destroy 生命周期
- [ ] 标准化 CC 效果体系：CC Effect Kind + Apply 端状态机 + 优先级仲裁
- [ ] 替换 parallelThink/parallelApply 中每 superstep 创建 goroutine 为预分配 worker pool

## Blocked

（无）

## Done

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
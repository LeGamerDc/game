# Todo — 当前任务执行清单

Last Updated: 2026-03-29

当前任务：Scheduler 重构（getLogic 注入 + 双缓冲 signal + 去除 per-logic 去重） ✅ 已完成

- [x] 分析旧设计痛点：per-thread inbox map、threadFrontiers、pendingInbox、routeSignals、buildInitialFrontier、deferRemainingInboxes 复杂度高
- [x] 识别 Logic 查找问题：WorldView.GetLogic 因 Go 泛型类型不变性无法返回匹配类型参数的 Logic → 改为 getLogic func(uint64)(L,bool) 外部注入
- [x] 设计双缓冲 signal collectors：signalRead（消费）+ signalWrite（产出），superstep 结束 swap + clear
- [x] 确认去重策略：Scheduler 不保证同一 logic 在同一 superstep 只 Think 一次，Logic 自身处理重复激活
- [x] 移除旧数据结构：threadInboxes、threadFrontiers、pendingInbox、routeSignals、buildInitialFrontier、deferRemainingInboxes
- [x] 实现 NewScheduler 接受 getLogic 注入，移除内部 logics map 和 Register/Unregister
- [x] 实现双缓冲 signalRead/signalWrite 及 swapSignalBuffers
- [x] 实现 injectPending：外部输入从 pending 注入 signalRead[0]
- [x] 实现 signalGroupBufs：Think worker 内部按 targetRef 分组 signal 的复用缓冲
- [x] 实现溢出自动延迟：超 MaxSupersteps 后 signalRead 残余信号自动保留到下一 tick
- [x] 实现 ProcessTick 新流程：injectPending → superstep{Think→Apply→swap→resetEffects} → merge → advance
- [x] 编写测试覆盖全部核心路径，含 race detector 通过
- [x] 确认 `go build ./...` 和 `go test ./en/ -race` 全部通过
- [x] 更新 memory.md 和 tasks.md

## Notes

- **getLogic 注入**：WorldView.GetLogic 因 Go 泛型类型不变性（type invariance）无法表达返回匹配类型参数的 Logic。改为构造时注入 `getLogic func(uint64)(L,bool)`，调用方须保证并发读安全（Go map 无写时支持并发读）。
- **双缓冲 signal**：signalRead 是上轮输出 + 外部输入 + 延迟信号，signalWrite 是当前轮输出。swap 后角色互换。溢出时 signalRead 残余自动保留，无需 deferRemainingInboxes。
- **去除去重**：旧设计用 threadInboxes/threadFrontiers 跟踪已激活 logic 避免重复 Think。新设计不做去重，同一 logic 可能在同一 superstep 被多次 Think（不同 source block 的 signal 分别触发）。Logic 自身负责幂等或去重。
- **signalGroupBufs**：Think worker 遍历 signalRead 中某个 block 时，同一 block 可能包含多个 logic 的 signal，用 `map[uint64][]S` 按 targetRef 分组后逐组调 Think。
- **简化程度**：消除了 routeSignals 单线程瓶颈和 frontier 管理开销，整体控制流更简单。

## 下一步

- 实现串行模式（cascade depth）及 processTick 模式路由
- 替换 WaitGroup+goroutine 为预分配 worker pool（性能优化）
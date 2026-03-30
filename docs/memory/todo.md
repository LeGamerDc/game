# Todo — 当前任务执行清单

Last Updated: 2026-03-30

当前无活跃任务。

## 已完成任务：Serial 模式实现

- [x] 设计 serial 模式执行策略：truly inline（Publish/Emit 立即递归调用）vs collect-then-cascade → 确认 truly inline
- [x] 确认 depth 追踪方案：栈变量 inc/dec，不嵌入 refVal
- [x] 确认 Apply 粒度差异可接受：serial 单 effect vs parallel 批量 Arrangement
- [x] 确认 thinkSignal/thinkTimer/applyOne 为 scheduler 内部闭包，Logic interface 不变
- [x] 实现 `countWork` 替代 `hasWork`（返回 int，early exit at threshold）
- [x] 重写 `ProcessTick` 支持 parallel/serial 模式路由
- [x] 实现 `scheduler_serial.go`：`serialProcess` + 三个递归闭包
- [x] Timer 注册使用 `blockToThread` 映射保证与 parallel 模式一致
- [x] 溢出信号写入 `signalWrite[0]`，ProcessTick 结束时 swap 保留
- [x] 编写 14 个 serial 测试：基础信号、inline 执行顺序、信号级联、depth 限制、Apply→Emit 级联、timer 激活、timer 重注册、自发 effect、未注册 target、defer 到下 tick、自发 signal、空 tick、parallel→serial 切换、depth 预算共享
- [x] 全部 35 个测试通过（含 race detector）
- [x] 更新 memory.md 和 tasks.md

## Notes

- **truly inline 关键设计点**：Publish 立即触发 Apply（不等 Think 结束），Emit 立即触发 Think（递归）。Apply 不增加 depth，只有 Think 增加 depth。
- **countWork early exit**：达到 ThinkConcurrencyThreshold 后立即返回，不再精确计数——超过阈值后走 parallel 路径，精确数值无意义。
- **serial timer 一致性**：使用 `sc.blockToThread[blockId]` 而非固定 thread 0，保证同一 logic 的 timer 始终在同一 thread-local log 中覆盖写，避免与 parallel 轮次的注册冲突。
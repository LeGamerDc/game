# Todo — 当前任务执行清单

Last Updated: 2026-04-24

## 当前任务：Scheduler StagedState 重设计首版实现

### 讨论与确认

- [x] 读取 memory，确认原主线为性能 demo，当前切换到 scheduler 协议设计讨论
- [x] 读取 `sched/world.go`、并发/串行调度流程和现有 WatchState 文档
- [x] 形成对 `Logic.Sum` / `Summarize` 方向的设计判断
- [x] 根据新讨论修正判断：优先评估 Apply + double-buffered StagedState，不新增 Sum 阶段
- [x] 采纳用户命名：使用 `StagedState`，不新增 `Touch`
- [x] 编写并运行闭包捕获 mutable ref 的 microbenchmark

### 实现

- [x] 将 `WatchState` / `WatchOf` / `CommitWatches` 从 `sched` runtime 接口中剥离
- [x] 增加 `StagedState`、`RefStage`、`StagePromoter.PromoteStages`
- [x] 在 `ThinkCtx` / `CommitCtx` 增加 `WriteStage(ST)`
- [x] 使用 `Concurrency` 个 `IndexMap[uint64, ST]` 收集 `WriteStage`
- [x] 并发路径在 Think→Apply、Apply→下一轮 Think 两个阶段边界 promote
- [x] 串行路径在 inline 阶段切换点 promote，并恢复嵌套调用前的 `stageRef`
- [x] 更新 scheduler/parallel 设计文档和测试
- [x] 运行 `go test ./...`

### Notes

- 当前实现只负责收集和 promote staged state；framework 层的 `[2]State` 双缓冲、dirty mirror 或结构共享策略尚未实现。
- benchmark：direct ≈0.92ns/op，closure capture ref ≈0.92ns/op，ctx function field ≈2.29ns/op（Apple M5, Go 1.26.1）。
- 暂不需要 `Touch` 这类独立 API；“唤醒 Apply 但没有业务 effect”的场景可先用特殊 effect 表达，未来再考虑优化。

# Todo — 当前任务执行清单

Last Updated: 2026-04-27

## 当前任务：Scheduler demo 接入文档

### 执行步骤

- [x] 读取 `docs/memory/memory.md`、`tasks.md`、`todo.md`
- [x] 读取 `docs/INDEX.md`，定位 scheduler 相关设计文档
- [x] 读取 `sched/world.go`、并发/串行调度实现与 StagedState 逻辑
- [x] 读取 `docs/design/parallel.md` 与 `docs/design/scheduler.md`
- [x] 新增 `sched/integration.md`
- [x] 在文档中明确 public/private data 的 Think/Apply 访问差异
- [x] 在文档中明确 SerialRef 没有普通 Logic 实体、不可 Think、只能作为 Publish 后的 Apply 归并目标
- [x] 更新 `docs/memory/memory.md` 与 `docs/memory/tasks.md`

### Notes

- `sched/integration.md` 是 demo/agent 接入手册，不替代 `docs/design/scheduler.md` 的实现级设计。
- 后续 demo 如果要使用 SerialRef，需要在 world/framework 层提供 apply-only dispatch；不要把 SerialRef 混入普通实体表并让它被 signal/timer 激活。

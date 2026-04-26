# Todo — 当前任务执行清单

Last Updated: 2026-04-25

## 当前任务：GAS / Attribute 边界重评估

### 讨论与确认

- [x] 读取 `docs/memory/memory.md`、`tasks.md`、`todo.md`
- [x] 读取 `docs/INDEX.md`，定位 scheduler / GAS / attribute 相关文档
- [x] 读取 `sched/world.go` 与 StagedState 实现，确认当前单一 `ST` + per-ref last-write-wins 语义
- [x] 读取当前 `gas/`、`tools/mk_attr`、`demo/demo_attr.go`
- [x] 参考 `/Users/dongcheng/Project/legamerdc/unreal-gas-analysis` 的 Attribute / Modifier / ActiveGE 调研
- [x] 参考旧 `/Users/dongcheng/Project/legamerdc/gas` 的 Ability/Running/Buff 抽象
- [x] 用户确认：移除当前 `gas/` framework 实现、落地 `attr/` package、StagedState 改为多域 API
- [x] 实现 `sched.StageKind` + kind-keyed `WriteStage`
- [x] 新增 `attr/` runtime package
- [x] 迁移 `tools/mk_attr` 到 `attr/cmd/mk_attr`
- [x] 删除 `gas/` framework 草稿和旧 `tools/mk_attr`
- [x] 更新 demo attr 配置并重新生成 `demo/demo_attr.go`
- [x] 更新设计文档、memory 与 docs index
- [x] 运行 `go test ./...`

### 待确认方案

- [x] `game/` 不提供完整 GAS framework；Ability/Effect/Buff/Cost/Cooldown/Stacking 等放到 demo 业务层
- [x] 新增独立 Attribute package，承接 AttributeSet 生成、AttrKey、Base/Current、Modifier Aggregator
- [x] Scheduler StagedState 从单一 `ST` 改为 `(StageKind, any)` 多域 staged entry

### Notes

- `attr.Modifier.Source` 是 opaque `uint64`，不绑定 EffectHandle/Ability/Buff/Tag requirement。
- `mk_attr` 接受 `scalar` 与 `attribute`；`instant` 暂作为 deprecated scalar 兼容。demo 中 HP/Mana 已改为 `attribute`。
- `go test ./...` 通过。

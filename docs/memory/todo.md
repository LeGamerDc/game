# Todo — 当前任务执行清单

Last Updated: 2026-03-29

当前任务：timerWheel 重构 — Unified Log + Epoch-based Lazy Clear ✅ 已完成

- [x] 阅读 `docs/design/scheduler.md`，确认 timer wheel 的单层 ring + thread/block 分片设计意图
- [x] 审查 `en/wheel.go` 当前实现，并为接口补充使用语义与时序约束注释
- [x] 与当前实现对齐设计结论：暂不支持移除 wheel 中已 merge 的旧 timer；局部取消仅作用于本 tick、merge 前的 thread 本地登记
- [x] 修改 `en/wheel.go`：将 `delay > wheelSize` 的行为改为 clamp 到最远 slot，而不是直接取模
- [x] 修改 `en/wheel.go`：将 `blockId` 参数改为 `int`
- [x] 修改 `en/wheel.go`：移除冗余的 `threadSize` 字段
- [x] 确认优化方向：`newTimerWheel` 接收每个 `threadId` 对应的 `blockId` 列表，由 scheduler 提供稳定映射
- [x] 修改 `en/wheel.go`：让 `merge/advance` 只遍历各 thread 自己负责的 block 列表
- [x] 回退 `set` 的 local blockId 转换方案，恢复为直接使用全局 `blockId`
- [x] 确认全量 slot 方案：每个 thread 的 `timerCollector` 按全量 block 预分配 `slot`，只在 `merge/advance` 时遍历该 thread 的 block 列表
- [x] review `en/wheel.go`，确认新方案没有引入额外接口变化，且测试代码无需修改
- [x] 完成测试并确认 `go test ./en` 通过
- [x] 重构 timerCollector：将 `blocks []int` + `slot []IndexMap[V, int64]`（per-block 预分配）替换为单个 `log IndexMap[V, timerEntry]`（unified log）
- [x] 重构 wheel slot：将 `IndexSet[V]` 替换为 `epochSet[V]`（epoch + IndexSet，惰性清空）
- [x] 验证 merge() 从 O(Σ blocks_per_thread) 降为 O(actual_registrations)，advance() 从 O(blockSize + Σ blocks_per_thread) 降为 O(threads)
- [x] 确认接口（set/merge/advance/get 签名和语义）不变，全部 6 个现有测试通过，`go build ./...` 通过
- [x] 更新 memory，记录 unified log + epoch-based lazy clear 方案已确认并落地

## Notes

- 已确认：额外的旧 timer 激活可接受，因为 logic 会自行剔除无效激活；当前阶段不为"移除 wheel 中旧 timer"引入额外开销。
- 已确认：`set(time <= 0)` 的取消语义只针对 thread 本地、尚未 merge 的登记，不提供全局取消。
- 已确认：timer wheel 仅在 `en` package 内部使用，不做防呆式保护和额外运行时校验。
- 已确认：`blockId` 改为 `int` 合理；`threadSize` 字段可删除。
- （历史）曾采用 A 方案（per-thread 全量 block slot + thread→block 列表），已被 unified log 方案取代。
- **重构决策 — Unified Log**：timerCollector 不再按 block 分配 slot，改为持有单个 `log IndexMap[V, timerEntry]`，每次 `set` 直接写入 log；merge 只需遍历 log 中的实际注册条目，复杂度 O(actual_registrations)。
- **重构决策 — Epoch-based Lazy Clear**：wheel 的每个 slot 从 `IndexSet[V]` 改为 `epochSet[V]`（包含 epoch 计数器 + IndexSet）；advance 时只需递增 epoch 而非清空集合，实际清空延迟到下次 merge 写入该 slot 时按需执行；advance 复杂度从 O(blockSize + Σ blocks_per_thread) 降为 O(threads)。
- 重构后接口（set/merge/advance/get）签名和语义完全不变，6 个现有测试无需修改且全部通过。

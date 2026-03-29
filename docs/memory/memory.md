# Memory

Last Updated: 2026-03-29

## Current Focus

- `en/wheel.go` 的 timerWheel 重构（Unified Log + Epoch-based Lazy Clear）已完成，所有测试通过，`go build ./...` 通过。
- 已确认：timer wheel 不是通用定时器，而是服务 scheduler 的 thread-local write + block-sharded read 结构。
- 下一步：继续处理已确认的接口问题（Ref 空间、Publish 分离），并把 scheduler 侧的 thread→block 构造与 wheel 对接收口。

## Latest State

- `en/world.go` 是当前引擎接口讨论的权威入口。
- `WorldView` 目前已包含 `Now()/Version()/Round()` 三个只读观测接口。
- `en/schedule.go` 已实现 Scheduler 核心运行时。
- `en/wheel.go` 已完成 Unified Log + Epoch-based Lazy Clear 重构：timerCollector 使用单个 `log IndexMap[V, timerEntry]`（unified log）替代原先的 `blocks []int` + `slot []IndexMap[V, int64]`（per-block 预分配）；wheel 的 `IndexSet[V]` 替换为 `epochSet[V]`（epoch + IndexSet，惰性清空）；merge() 复杂度从 O(Σ blocks_per_thread) 降为 O(actual_registrations)，advance() 从 O(blockSize + Σ blocks_per_thread) 降为 O(threads)；接口签名和语义不变，现有测试无需修改。
- `docs/design/parallel.md` 是 parallel tick 设计意图的主文档。
- `docs/design/scheduler.md` 是 Scheduler 并发调度模型设计文档（新增）。
- timer wheel 相关结论已更新：不要求全局覆盖旧 timer；局部取消仅作用于尚未 merge 的本地登记；`delay > wheelSize` 需要 clamp 到最远槽位。
- timer wheel 已完成从 per-block 预分配方案到 Unified Log + Epoch-based Lazy Clear 的重构；unified log 消除了 thread 持有全量 block slot 的内存开销，epoch-based lazy clear 消除了 advance 时逐元素清空 wheel slot 的开销。
- `en/wheel_test.go` 已新增并通过测试，覆盖全局 block 写入、超长 delay clamp、pre-merge cancel、post-merge cancel 不清旧 timer、advance 清理、同槽去重等关键语义；重构后测试无需修改且仍通过（共 6 个测试）。
- `docs/design/feedback.md` 保存了上一轮审计的完整对话记录。

## Confirmed Decisions

### 协作流程

- 协作记忆统一在 `docs/memory/` 目录下，包含三个文件：
  - `memory.md`：稳定上下文和当前状态
  - `tasks.md`：项目级任务注册表
  - `todo.md`：当前活跃任务的执行清单

### Parallel Tick 接口审计结论

以下结论来自对 `en/world.go` 与 `docs/design/parallel.md` 的深度审计及逐条讨论。

**需要修复的接口问题：**

1. **Ref 空间歧义**：`IsSerialRef(RefWorld) == true`，三类 ref（Normal / World / Serial）不互斥。缺少 `RefNone` 和 `IsValidRef`。需要明确互斥分区。
2. **Publish 不区分 Entity/World Effect**：`ThinkCtx.Publish` 用同一函数 + 同一类型参数覆盖 entity effect 和 world effect 两种语义，无编译期安全。需要拆分或加 domain 标记。

**确认不改的设计点：**

3. WorldView 保持极简。
4. Signal/Effect 的 source ref 是用户的事。
5. 代数模型推迟。
6. Budget/Meta 不进 Logic 接口。
7. Apply→Emit 自激活是合法场景。
8. Timer 冲突由 Logic 内部处理。
9. Think 激活类型不需要框架层分类。
10. Ack 内嵌在 Think / private state 中。

### Scheduler 并发模型

以下结论来自 Scheduler 设计讨论。

**并发控制**：
- Think 阈值 500 开启并发，并发 worker 数 5，每 tick 最多 3 轮 superstep。
- 参数统一放入 `ScheduleMeta` 结构体。

**Block-based Effect 收集**：
- 不按 RefId 逐一收集，而是按 `targetRef % BlockSize`（如 137）分块。
- 消除了单独的 Merge Phase：Apply 阶段按 blockId 分配 worker，直接跨 Think worker 读取对应 block。
- Block 内按 targetRef 聚合后逐个 Apply。

**Worker 亲和性**：
- Think 阶段 Logic 按 `RefId % WorkerCount` 稳定分配到 worker。
- 同一 Logic 跨 superstep 轮次始终在同一 worker，因此 timerRegs 可以是 worker 本地 map，无锁无冲突。
- Tick 结束后统一合并 timerRegs 到全局 timer wheel。

**World Effect 并行**：
- World（RefWorld）作为普通 block 成员参与 Apply 阶段并发。
- World Apply 内部串行处理多个 world effect，与 entity Apply 并行。
- 安全前提：snapshot 隔离、独立 signal buffer、despawn 只标记不碰 entity state。

**串行模式**：
- 无 superstep 概念，无 frontier push。
- 所有 signal/effect 当场处理（立即 Apply、立即路由），深度优先递归。
- 用 cascade depth 控制递归深度，`depth >= MaxSupersteps` 时截断，signal 留到下一 tick。
- 语义差异（执行顺序不同于并发模式）对游戏场景可接受：tick 内事件顺序任意合法。
- 通过 ThinkCtx/CommitCtx 闭包实现差异，Logic 接口层完全兼容。

**Timer Wheel**：
- 单层环形数组，大小 200。
- 结构目标是适配 scheduler 的 thread/block 分片：Think 阶段 thread-local write，tick 消费阶段按 block 读取。
- 当前不要求“从 wheel 中移除旧 timer”这一全局覆盖语义；重复激活由 logic 自身在 Think 时剔除无效激活，额外一次激活成本可接受。
- `delay <= 0` 仅表示不新增/取消当前 thread 本地、尚未 merge 的登记，不承诺清除已经 merge 到 wheel 的旧条目。
- `delay > TimerWheelSize` 应 clamp 到最远 slot，logic 被唤醒后重新注册剩余 delay（amortized 额外 Think 开销可接受）。
- merge/advance 已通过 Unified Log + Epoch-based Lazy Clear 重构完成优化：
  - timerCollector 使用单个 `log IndexMap[V, timerEntry]` 统一记录所有登记，不再按 block 预分配 slot，消除了全量 block 内存开销。
  - wheel slot 使用 `epochSet[V]`（epoch + IndexSet），advance 时仅递增 epoch 实现惰性清空，不逐元素删除。
  - merge() 只遍历各 thread 的 log 中实际存在的登记条目，复杂度 O(actual_registrations)。
  - advance() 只需对每个 thread 递增 epoch 并重置 log，复杂度 O(threads)。
  - `set`/`merge`/`advance`/`get` 的签名和语义保持不变，现有测试无需修改。

**模式切换**：
- 每轮 superstep 独立判断，frontier 缩小到阈值以下可切换到串行模式。
- 串行 cascade depth 预算 = `MaxSupersteps - 已完成并发轮次`。

### Scheduler 当前实现约束

- Scheduler 以 `Logic.ID()` 作为 owner/ref 权威索引，world effect 也通过同一套 Apply 路径处理；若需要 world effect，调用方需要注册一个 `ID()==RefWorld` 的 world logic。
- `Scheduler.Emit` / ready set 既承担外部输入注入，也承担 tick 溢出后的 next-tick defer。
- 目前没有错误返回或日志 hook；非法 target ref / 未注册 logic 的 signal/effect 会被 runtime 统计为 dropped。

## Open Questions

- Logic 生命周期方法（Init/Dispose）是否需要加入接口——尚未讨论。
- LogicMeta 如何暴露给调度器——设计文档有描述，接口未体现，尚未讨论。
- ThinkCtx 函数引用可被 Logic 逃逸存储——Go 语言限制，无法在接口层解决，只能靠规范和 review。
- `engine.go` 中现有 GAS 模式与新并行模型的迁移隔离策略——尚未讨论。
- `docs/design/feedback.md` 应并入主设计文档还是保留为评审记录——上轮遗留。
- 外部输入注入 API：网络请求如何在 tick 开始前转化为 Signal。
- Think 返回 delay 的时间基准：相对当前 tick 的偏移量？
- Block 粒度（137）是否适合所有负载模式。
- 串行模式下同一 logic 被多条因果链触发时的 depth 处理策略。
- `TickStats` 是否需要进一步扩展为 tracing/debug API。
- 当前 world effect 复用 `Logic` 接口是否足够清晰，还是应单独抽出 world reducer 接口。
- 是否需要在 `schedule.go` 中继续收敛出显式的 block 分配辅助结构，以避免后续调用 timer wheel 时重复推导 thread/block 关系。

## Relevant Files

- `AGENTS.md`
- `en/world.go`
- `en/schedule.go`
- `en/wheel.go`
- `en/wheel_test.go`
- `en/schedule_test.go`
- `en/engine_bak.go`
- `docs/design/parallel.md`
- `docs/design/scheduler.md`
- `docs/design/feedback.md`
- `docs/references/parallel_theory.md`
- `docs/references/survey.md`
- `lib/indexmap.go`
- `lib/heapindexmap.go`

## Should

- 对任何算法议题（如 timer wheel、effect/signal 收集器、数据结构选型），可以创建 subagent 单独调研解决，不需要在主对话中展开所有细节。

## Dont's

- （暂无，待用户补充）

## Maintenance Notes

- 这里保存稳定上下文和当前状态，不保存完整聊天转录。
- 项目级任务追踪放在 `docs/memory/tasks.md`。
- 当前任务执行清单放在 `docs/memory/todo.md`。

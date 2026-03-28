# Memory

Last Updated: 2026-03-28

## Current Focus

- 完成了对 `en/world.go` + `docs/design/parallel.md` 的深度接口审计。
- 已逐条与用户讨论审计发现，区分了"真问题"与"不是当前该解决的问题"。
- 下一步：修复已确认的接口问题（Ref 空间、Publish 分离），然后进入调度器设计。

## Latest State

- `en/world.go` 是当前引擎接口讨论的权威入口。
- `docs/design/parallel.md` 是 parallel tick 设计意图的主文档。
- `docs/references/` 存放支撑设计讨论的理论和调研材料。
- `docs/design/feedback.md` 保存了上一轮审计的完整对话记录（含审计清单原文），待决定是否并入主文档。

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

3. **WorldView 保持极简**：框架不使用 WorldView 做路由或调度，空间/实体查询能力交给泛型具体类型扩展。可考虑加 `Round() int` 用于调试溯源，但不加游戏特定查询。
4. **Signal/Effect 的 source ref 是用户的事**：框架负责路由，不负责检查 payload 内容。用户在自己的实现里加 source 字段即可。
5. **代数模型推迟**：等调度器设计完成 + effect 类型调研后再回来设计 effect 代数元数据。
6. **Budget/Meta 不进 Logic 接口**：budget 是调度器的安全网，Logic 应写成不依赖 budget 也正确的代码。
7. **Apply→Emit 自激活是合法场景**：真实游戏会出现 signal 级联，框架通过 superstep 轮次限制兜底，不在并发模型层面禁止。
8. **Timer 冲突由 Logic 内部处理**：Logic 自己维护 `nextThinkTick`，过滤多余激活。框架不定义复杂的 timer 覆盖语义。
9. **Think 激活类型不需要框架层分类**：`inbox.Len()` + Logic 内部状态足以区分 timer 驱动 / signal 驱动 / 首次激活。
10. **Ack 内嵌在 Think / private state 中**：Think 做快速校验并将结果缓存到 private state，tick 结束后由外层排水到网络层。框架不需要 Ack 概念。

**新发现：**

11. **外部输入注入点**：网络请求如何转化为 Signal 进入 Logic inbox，是调度器 / tick runner 设计时需要定义的 API。当前接口未覆盖。

## Open Questions

- Logic 生命周期方法（Init/Dispose）是否需要加入接口——审计中提出，尚未讨论。
- LogicMeta 如何暴露给调度器——设计文档有描述，接口未体现，尚未讨论。
- ThinkCtx 函数引用可被 Logic 逃逸存储——Go 语言限制，无法在接口层解决，只能靠规范和 review。
- `engine.go` 中现有 GAS 模式与新并行模型的迁移隔离策略——尚未讨论。
- `docs/design/feedback.md` 应并入主设计文档还是保留为评审记录——上轮遗留。

## Relevant Files

- `AGENTS.md`
- `en/world.go`
- `en/engine.go`
- `docs/design/parallel.md`
- `docs/design/feedback.md`
- `docs/references/parallel_theory.md`
- `docs/references/survey.md`

## Should

- （暂无，待用户补充）

## Dont's

- （暂无，待用户补充）

## Maintenance Notes

- 这里保存稳定上下文和当前状态，不保存完整聊天转录。
- 项目级任务追踪放在 `docs/memory/tasks.md`。
- 当前任务执行清单放在 `docs/memory/todo.md`。

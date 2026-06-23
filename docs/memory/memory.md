# Memory

Last Updated: 2026-06-22

## Current Focus

新增**纯串行调度器** `sched/ser` 包：面向"开发/调试复杂度最低"的仅串行抽象，与并行 `sched/par` 并列、不复用其 Logic 接口。两个 scheduler 现已收拢到 `sched/` 下：`sched/par`（原 `sched` 包，并行 BSP）、`sched/ser`（纯串行）。设计与实现已落地并通过两轮 codex review（首轮抓到一个真 bug 已修）。下一步可选：接入手册（类似 `sched/par/integration.md`）、事件 slice 池化、连续溢出饥饿告警。注意 `demo/` 已在 commit `ed28f3b` 删除，本 memory 下方 demo 相关条目已过时。

## Latest State

- **纯串行调度器 `sched/ser` 已实现并 review**：`sched/ser/scheduler.go`（~294 行，核心 tick ~50 行）+ `sched/ser/scheduler_test.go`（24 测试，含随机 soak）。模型 = capped serial BSP + actor 风格 typed inbox：Unit 只有 `Think(ctx, events) int64` 单入口、直读 world/其他单位、改自己状态、`ctx.Post/Poke` 把事件交给 owner 处理；无 Apply/Stage、无 Effect/Signal 拆分。计时用 `lib.HeapIndexMap`（每 unit 一条目，Think 返回权威 deadline，`delay<=0` 取消 timer）；tick 拆成 ≤maxSteps(默认3) 个 superstep 波次（事件双缓冲，S→S+1），超出溢出到下个 tick 且 `Overflow()` 计数。确定性：每波按 ref 升序处理，不泄漏 map/heap 迭代序。设计稿见 `docs/design/serial_scheduler.md`。三轮 web 调研确认其等价于 serial Pregel/BSP + Bevy `Events<T>` 双缓冲 + actor message-cycle。`go test ./... && go vet` 通过。
- **Sched + demo 性能验证首轮完成**：`demo/scenario/skills.go` 新增 `AddBenchmarkAbilities`，包含低 CD 单体、群体伤害、短周期 burn buff 与 `SignalDamageDealt` 被动追击；`demo/scenario/benchmark_test.go` 新增 `BenchmarkGridCombatScheduler`，固定 16x16 / 32x32 / 84x84 grid，对比强制串行、parallel-4、parallel-8；84x84 用作约 7000 单位档位（7056 units）。
- **Demo 当前验证结果**：`GOCACHE=/private/tmp/game-go-build-cache go test ./...` 通过；`GOCACHE=/private/tmp/game-go-build-cache go test -race ./demo/...` 通过；`GOCACHE=/private/tmp/game-go-build-cache go test -bench=BenchmarkGridCombatScheduler -benchmem -run=^$ -benchtime=500ms -count=3 ./demo/scenario` 通过。
- **初步 benchmark 趋势**：Apple M5 上 16x16 串行约 2.81-2.83 ms/tick，parallel-4 约 0.85-0.88 ms/tick，parallel-8 约 0.84 ms/tick；32x32 串行约 12.9-13.1 ms/tick，parallel-4 约 3.25-3.26 ms/tick，parallel-8 约 2.83-2.85 ms/tick；84x84 串行约 123-125 ms/tick，parallel-4 约 26.7-26.8 ms/tick，parallel-8 约 23.6-24.2 ms/tick。并发路径已有清晰收益，但 alloc/op 仍高，后续适合用 pprof/benchstat 深挖。literal 7000x7000 会创建 4900 万 units，当前不应直接跑。
- **BT Game Developer 投稿正文与图像已产出**：新增 `docs/papers/bt_stack_runtime_submission.md`，约 2385 英文词，使用 1 张 imagegen 头图与 7 张本地生成的 16:9 PNG 技术图；正文明确不宣称首次提出 traversal stack、不宣称完整替代全局 reactive BT、不在无对照 benchmark 前写性能倍数。
- **BT 投稿前首批修复已完成**：修复 `joinBranch.OnEvent` 遗漏未消费事件子树 next wake 的问题；`NewRepeatUntilNSuccess` 构造函数与 `TypeRepeat.Check` 拒绝 `require <= 0`、`maxLoop <= 0`、`require > maxLoop`；`Root.SetNode` 断言只能在空栈时调用；补充覆盖测试。`GOCACHE=/private/tmp/game-go-build-cache go test ./...` 通过。
- **BT Game Developer 投稿组织稿已新增**：`docs/papers/bt_stack_runtime_article_outline.md` 根据官方 Blogging Guidelines 与本地风格调研，确定文章不写成 README/论文，而写成 2200-3000 词 technical breakdown；主线为 active path continuation stack，正文用 6-8 张图解释 root tick 对比、短路径恢复、AlwaysGuard sub-root、parallel child roots、event dispatch、discrete wake 与 cancel unwind，最多保留两段极简代码。
- **BT 投稿前审计已完成**：`bt/tmp_gamedeveloper_audit.md` 记录了实现机制、测试/benchmark 证据、明显 bug、创新性边界和 Game Developer 文章定位。两个已知 P0 已修复；当前仍需补传统 root tick 对照 benchmark、allocation/pprof 解释与文章图示后再投稿。
- **BT 手动栈投稿核心设计已沉淀**：`docs/design/bt_stack_runtime_article.md` 记录新颖性边界与文章主 claim。调研确认 AltDevBlog 2011 已提出 data-oriented traversal stack，因此“手动栈 short path”不能单独作为无人提出的 claim；但可以把 `bt/` 定位为 stack-complete BT runtime：普通 composite 是栈帧，AlwaysGuard 是外层 frame + 内部 sub-root，parallel 是外层 frame + 多个子 root，event/discrete wake/cancel 都落在 active continuation 上。
- **Demo combat 框架设计稿已新增**：`docs/design/demo_combat_framework.md` 以 `sched/integration.md` 为约束，提出 Unit=Owner、World staged query、0.125s tick/连续秒 deadline、普通攻击前摇/弹道延迟、技能 ready queue、被动触发、buff modifier、死亡 8s 复活和 package/file 布局。
- **Demo combat MVP 已实现**：`demo/combat` 包含 time、Effect/Signal、World、Stage summary、空间查询、Unit、普通攻击、技能队列、被动触发、buff、死亡复活和生成属性；`demo/scenario` 包含 n x n grid、demo 技能配置、runner 和集成测试。
- **实现期设计调整已记录**：`demo/update.md` 记录了串行 scheduler 递归 `Emit` / `Publish` 的业务接入规则：发出 self signal 或可能回流 source 的 effect 前，必须先推进 owner-local 状态，pending 队列必须先移除/推进再 publish。
- **Demo 属性生成目标已迁移**：`demo/cfg/attr.toml` 新增 `AttackRange` / `AttackSpeed`，`demo/Makefile gen-attr` 输出到 `demo/combat/demo_attr.go`（package `combat`），旧根目录 `demo/demo_attr.go` 已删除。
- **旧 demo 根 package 已移除**：早期草稿 `demo/gas.go` / `demo/world.go` 已删除，避免与 `demo/combat` 新实现并存两套语义。
- **验证通过**：`make -C demo gen-attr` 与 `GOCACHE=/private/tmp/game-go-build-cache go test ./...` 均通过。
- **Demo package 拆分已确认**：框架和场景都在 `demo/` 下，但使用子 package 分层；`demo/combat` 放战斗框架，`demo/scenario` 放具体 n x n 场景、测试技能配置、runner 和集成测试，依赖方向为 scenario -> combat。
- **StagedState 已改为多域 API**：`sched` 使用 `StageKind int32` + `StagedState any`，`ctx.WriteStage(kind, state)`，`RefStage{RefId, Kind, State}`，`StagePromoter.PromoteStages(Inbox[RefStage])`。Scheduler 不再有 `ST` 类型参数。
- **Scheduler 接入文档已新增**：`sched/integration.md` 作为 demo/agent 接入手册，明确 Logic=Owner、public/private data 分层、Think/Apply 访问矩阵、Effect/Signal 分工、StagedState 接入方式、World 接入清单与常见误用检查表。
- **WriteStage owner 安全**：`WriteStage` 不提供 ref 参数；scheduler 在 Think/Apply 调用前通过闭包捕获当前 owner ref（parallel: `thinkRef` / `applyRef`；serial: 可恢复的 `stageRef`），防止 Logic 写其他 owner 的 staged state；`StageKind` 只区分同 owner 的 staged domain。
- **Promote 实现**：每个 worker 使用一个 `IndexMap[stageKey, StagedState]` 收集 staged state；`stageKey=(ref, kind)`，同 owner+kind 同阶段 last-write-wins。阶段 barrier 后串行 flatten 并调用 `PromoteStages`。
- **闭包 benchmark 结果**：`WriteStage` 闭包捕获 mutable ref 本身约 0.92ns/op；通过 ctx 函数字段调用约 2.29ns/op，成本可接受。
- **Scheduler 已实现**：代码在 `sched/` 包，包含并发/串行双模式、自动切换、timer wheel、block-based effect 收集、LPT 负载均衡、StagedState 机制。`go test ./...` 通过。
- **Think 调用合并优化已完成**：`thinkWorker`（并行）和 `serialProcess`（串行）都通过归并遍历（merge-iteration）timer refs + signal flatBuf，保证每个 logic 在初始 frontier 中最多一次 Think 调用。Timer 是纯唤醒机制，被 signal 吸收；串行模式初始 frontier 也做了 signal 批量化，两种模式语义一致。
- **接口定义**：`sched/world.go`（Logic、ThinkCtx、CommitCtx、World、StageKind、StagedState、StagePromoter、Inbox 等）。
- **Scheduler 4 个类型参数**：`Scheduler[W, S, E, L]`。Logic 3 个参数 `Logic[W, S, E]`（WorldView 与 `ST` 均已移除）。
- **设计文档已对齐**：`docs/design/parallel.md`（概念与理论）、`docs/design/scheduler.md`（实现级设计）。
- **scheduler.md 新增"计算分解约束"章节**：任何依赖双方状态的公式必须分解为 Source 端函数和 Target 端函数，由 Effect 数据连接。
- **`gas/` framework 已移除**：上一轮未提交的 `AbilitySystem`、`TagState`、`AbilitySet`、`ActiveEffectTable` 等草稿已删除；完整 GAS 未来放 demo 业务层。
- **`attr/` package 已新增**：`attr.Value`、`attr.Map`、`attr.Table`、`attr.Modifier`、Unreal-style Add/Mul/Div/Override channel aggregation、可选 Attribute hooks（PreBase/PreCurrent/PostCurrent）已实现。
- **mk_attr 已迁移**：旧 `tools/mk_attr` 删除，新路径为 `attr/cmd/mk_attr`；生成代码导入 `github.com/legamerdc/game/attr`。`demo/Makefile` 已更新。
- **demo 属性已再生成**：`demo/cfg/attr.toml` 中 HP/Mana 改为 `attribute`，生成的 `demo/demo_attr.go` 所有字段使用 `attr.Value`。
- **验证通过**：`go test ./...` 通过。
- **ability_system.md 完成第二版修订**：统一 Buff/Running 为 thinkable Buff interface、澄清 Modifier 定位、新增 Effect 数据设计指导、17 个开放问题已列出。
- **博客初稿已完成**：`docs/papers/blog_parallel_tick.md`（gamedeveloper.com 投稿），待性能数据验证后再提交。
- **GAS 调研完成**：`docs/references/gas_survey.md` + `docs/tmp/research_*.md`（属性、效果、能力、Cues/Targeting 四篇）。
- **适配分类指导手册已完成**：`docs/design/adaptation_guide.md`（6 大分类 + 107 条逻辑链路验证）。
- **旧 GAS 模式代码已归档**：`sched/engine_bak.go`（全部注释掉）。

## Confirmed Decisions

### BT runtime

- `TaskStatus > 0` 表示 running 且值为相对 delay hint，不是绝对时间点。
- `TaskNew == 0` 复用为内部 push-new-child 标记与 event 未处理标记；调用方需要按上下文理解。
- `Node.Check` 是当前节点浅校验，不递归检查整棵子树；整树校验若需要应作为单独 API 设计。
- `Root.SetNode` 仅用于空栈 root 的初始化/替换；运行中替换必须先 cancel 或等待当前栈结束。

### 核心模型

- **Logic = Owner**：调度单位是 Logic，内部子逻辑组合是实现私有事务。
- **Typed Effect/Signal**：不用 closure，所有副作用都是显式的 typed 数据。
- **World 是特殊 Owner**：RefWorld 参与同一套 Apply 流程，不需要独立阶段。
- **Effect 顺序无关（容忍性）**：不是数学严格交换律，而是玩家和开发者对不同顺序的处理结果保持容忍。

### Scheduler 实现

- Think 阈值 500、并发 worker 5、最多 3 轮 superstep（`ScheduleMeta` 管理）。
- Block-based effect 收集，sort-based 分组。CacheLinePad 隔离。
- Think 阶段 `blockId % Concurrency → threadId`（稳定映射）。Apply 阶段 LPT 动态分配。
- 串行模式 truly inline（递归闭包，非 collect-then-cascade）。
- 双缓冲 Signal Collectors。无 per-logic 去重。
- 每个 logic 每个 superstep 最多一次 Think 调用（归并遍历 timer+signal）。Timer 无数据，被 signal 吸收；纯 timer → Think(nil)，有 signal → Think(signals)。串行模式初始 frontier 也做信号批量化，与并行模式语义一致。
- StagedState：`WriteStage(StageKind, StagedState)` 提交当前 owner 某个 staged domain 的阶段稳定状态，阶段边界 `PromoteStages` 串行提交；WatchState 移出 scheduler runtime，作为 framework 层 staged state 用例。
- Demo 接入采用更严格的数据分层约束：private data 由 Think 读写，public data 由 Apply 作为权威提交点；Think 只读取 public 的稳定视图，public summary 通过 StagedState/World 查询暴露。
- SerialRef 是 apply-only 的串行归并目标：没有普通 Logic 实体，不进入 Think/timer/signal；可以通过 `Publish(serialRef, effect)` 被归纳到一起 Apply。`RefWorld` 需先于一般 SerialRef 单独识别。
- **WorldView 已移除**：Think 和 Apply 阶段使用同一 W 类型（约束为 `World + LogicProvider + StagePromoter`）。原 WorldView 隔离价值极低（仅阻止调用 GetWorldView()），且无法通过 type parameter 注入自定义受限类型。移除后消除了 interface boxing 开销。

### Attribute / GAS 边界决策

- **GAS 不作为 `game/` 基础框架实现**：Ability、Effect、Buff、Cooldown、Cost、Stacking policy、Tag requirement 等都与具体业务耦合，未来优先在 demo 中和业务逻辑一起实现。
- **Attribute 是独立基础 package**：`attr/` 提供 AttributeSet 生成、AttrKey、Base/Current、Modifier Aggregator 等基础能力；生成器位于 `attr/cmd/mk_attr`。
- **Modifier 是 Attribute 聚合层的贡献记录**：Modifier/Channel/Op/Aggregator 是基础设施；其 Source 是 opaque `uint64`，stack 规则、tag 条件、effect 生命周期不绑定到 `attr`。
- **计算分解约束**：任何依赖双方状态的公式必须分解为 Source 端函数和 Target 端函数，由 Effect 数据连接。这是 parallel tick 的核心约束。
- **Effect 数据设计**：携带中间结果（如 rawDamage）+ 少量 source 参数（如 penetration、element），不携带 source 全部状态。Source 端在 Think 阶段计算并打包，Target 端在 Apply 阶段用自身状态完成最终计算。
- **attr.toml 显式 field ID**：attr.toml 使用显式 field ID（`{ id = N, type = "scalar"|"attribute" }`；`instant` 暂兼容为 deprecated scalar），不再依赖 list 顺序；生成的 Go 代码使用显式常量值而非 iota。

### Demo combat 结构决策

- `demo/combat` 是战斗框架 package，包含 Unit、World、Effect、Signal、Ability、Buff、Stage summary、属性 helper 和生成的 `demo_attr.go`。
- `demo/scenario` 是具体 demo 场景 package，包含 n x n 初始化包装、测试技能/buff 配置、runner 和端到端测试；依赖 `demo/combat`，`demo/combat` 不反向依赖 scenario。
- 根 `demo/` 尽量只保留 `Makefile`、`cfg/attr.toml`、prompt/说明类材料；旧 `demo/gas.go` / `demo/world.go` 已由 `demo/combat` 新实现取代并删除。
- MVP 采用设计稿默认语义：普通攻击 fire 后即可让技能队列接管；技能 CD 从 cast commit 开始；弹道命中只检查目标仍存在且可受击，不重新检查距离；`AttackRange` / `AttackSpeed` 是 attribute，`ProjectileSpeed` 暂为配置；每 Unit 使用基于 ref 的 deterministic RNG。
- 串行模式下业务代码必须考虑 `Emit` / `Publish` inline 递归：在 self signal 或可能回流 source 的 effect 发出前，先提交本 owner 私有/公开状态；到期队列先移除或推进 deadline，再发布 effect，避免递归 Think 重复处理。
- MVP 的 n x n 创建是 tick 外部 scenario 初始化，暂未把 spawn/despawn 建模为 `RefWorld` effect；运行时创建/销毁需要后续补 World/SerialRef apply-only dispatch。

### 已关闭的设计方向

- **Effect/Signal 代数组合（框架级预合并）**：确认不做。Commutativity ≠ Mergeability。
- **Shuffle 验证**：不适用，"顺序无关"是容忍性。
- **Per-logic LogicMeta**：由 ScheduleMeta 统一管理，不需要 Logic 接口暴露 Meta()。
- **Logic 生命周期（Init/Dispose）**：不需要框架级接口，Logic 自行管理。
- **WorldView 接口隔离**：已移除。Think/Apply 统一使用 W World，隔离由约束系统保证（Logic 内无法调用 GetLogic 等非 World 方法）。

## Open Questions

### BT 投稿与实现

- BT 投稿是否要在提交前补对照 benchmark：传统 root tick、memory composite、reactive guard 频繁重评估、事件唤醒路径和 allocation/pprof 都尚未补齐；当前英文稿已避免性能倍数宣称，可作为架构拆解稿继续审阅。
- BT 投稿下一步是审阅 `docs/papers/bt_stack_runtime_submission.md` 的语气、图示、标题和对 prior art 的边界表达，再决定是否补 benchmark 数据。

### Scheduler 层

- Framework 层具体如何用多域 `StagedState` 实现双缓冲 WatchState/订阅表：dirty mirror、全量 copy、结构共享三种策略尚未落定。
- 是否需要提供标准 StagedState helper（如 WatchBits、AOI membership、AttrSummary）仍未决定。
- 阶段稳定数据的通用抽象：除 WatchState 外，是否也覆盖订阅表、空间索引 membership、可见性/AOI、派生 public summary、dirty attribute projection。
- 空间查询 API：World 需提供版本化只读空间索引接口。
- 外部输入注入 API：网络请求如何在 tick 开始前转化为 Signal。
- Worker pool：替代每 superstep 创建 goroutine（代码中已有 TODO）。
- SerialRef 的 apply-only dispatch 需要在 demo/framework 层明确落地；当前普通 Apply 调用仍通过 `LogicProvider.GetLogic(ref)`，接入时不能把 SerialRef 混作可 Think 的普通实体。

### Attribute 设计层

- `instant` field type 目前仅兼容为 deprecated scalar；未来是否完全删除兼容尚未决定。
- AttrTable/Aggregator 索引方式：当前 map[AttrKey][]Modifier 可用，但长期可考虑按 generated field count 做 dense storage 与 lazy aggregator。

### 与其他系统的关系

- 弹道 Logic 模板：spawn/fly/collide/destroy 与 GAS 交互
- CC 效果标准化：Kind/Priority/Tenacity 体系
- 行为树（bt/）与 GAS 集成：NPC AI 如何调用 AbilitySet

### Demo combat 后续增强

- Projectile 是否需要升级为独立 Logic 模板：spawn/fly/collide/destroy 生命周期尚未实现。
- `ProjectileSpeed` 是否需要成为 attribute 以支持 buff 修改；MVP 暂保持为普通攻击/技能配置。
- 公开 tag / CC / cost / charge / stack policy 等更完整 GAS-like 能力尚未进入 MVP。
- SerialRef apply-only dispatch 在 demo 层还未落地；MVP 暂不使用 SerialRef。
- 运行时 spawn/despawn 还未走 `RefWorld` effect；当前 grid 初始化发生在 tick 前。

## Relevant Files

- `AGENTS.md`
- `bt/tmp_gamedeveloper_audit.md`
- `docs/design/bt_stack_runtime_article.md`
- `docs/papers/bt_stack_runtime_article_outline.md`
- `docs/papers/bt_stack_runtime_submission.md`
- `docs/papers/assets/bt_stack_runtime/`
- `bt/node.go`
- `bt/branch.go`
- `bt/decorator.go`
- `bt/stk.go`
- `bt/node_test.go`
- `bt/README.md`
- `bt/blackboard/blackboard.go`
- `docs/references/gamedeveloper_style_guide.md`
- `sched/par/world.go`
- `sched/par/scheduler.go`
- `sched/par/scheduler_parallel.go`
- `sched/par/scheduler_serial.go`
- `sched/par/scheduler_test.go`
- `sched/par/integration.md`
- `sched/par/wheel.go`
- `sched/par/block_collector.go`
- `sched/par/utils.go`
- `sched/ser/scheduler.go`
- `sched/ser/scheduler_test.go`
- `docs/design/serial_scheduler.md`
- `docs/design/parallel.md`
- `docs/design/scheduler.md`
- `docs/design/demo_combat_framework.md`
- `docs/design/adaptation_guide.md`
- `docs/design/ability_system.md`
- `docs/references/gas_survey.md`
- `/Users/dongcheng/Project/legamerdc/unreal-gas-analysis`
- `/Users/dongcheng/Project/legamerdc/gas`
- `docs/papers/blog_parallel_tick.md`
- `attr/attribute.go`
- `attr/modifier.go`
- `attr/attribute_test.go`
- `attr/cmd/mk_attr/main.go`
- `attr/cmd/mk_attr/main_test.go`
- `demo/cfg/attr.toml`
- `demo/Makefile`
- `demo/update.md`
- `demo/combat/demo_attr.go`
- `demo/combat/world.go`
- `demo/combat/unit.go`
- `demo/combat/unit_attack.go`
- `demo/combat/ability.go`
- `demo/combat/ability_slot.go`
- `demo/combat/passive.go`
- `demo/combat/buff.go`
- `demo/combat/unit_death.go`
- `demo/combat/*_test.go`
- `demo/scenario/grid.go`
- `demo/scenario/skills.go`
- `demo/scenario/benchmark_test.go`
- `demo/scenario/runner.go`
- `demo/scenario/*_test.go`
- `demo/prompt.md`

## Should

- 对任何算法议题可以创建 subagent 单独调研解决。
- 设计稿与代码出现矛盾时，以代码为准。

## Dont's

- 不要在 refVal 中嵌入 serial-only 的字段（如 depth），避免影响 parallel cache 效率。

## User

[@parallel.md](file:///Users/dongcheng/Project/legamerdc/game/docs/design/parallel.md) [@world.go](file:///Users/dongcheng/Project/legamerdc/game/en/world.go) 
结合设计和接口设计，深度思考一下这个parallel 设计是否存在漏洞

## Assistant





<thinking>
The user wants me to deeply analyze the parallel tick design document and its corresponding interface definitions in `world.go` to identify potential design flaws, gaps, or vulnerabilities.

Let me carefully analyze both documents and think about potential issues.

Let me first understand the design:

1. **Core model**: BSP-like parallel tick execution with Logic = Owner
2. **Tick pipeline**: Think (parallel, read snapshot) → Apply (parallel by owner) → World Effect (serial)
3. **Data layers**: World state, Logic public state, Logic private state
4. **Effect/Signal**: Typed, not closures

Now let me look at the interface in `world.go`:

```go


type (
	EffectKind int32
	SignalKind int32
)

const RefWorld uint64 = 1 << 63

func IsWorldRef(r uint64) bool { return r == RefWorld }
func IsSerialRef(r uint64) bool { return r >= RefWorld }
func IsNormalRef(r uint64) bool { return r < RefWorld }
```

I'm examining the reference system and basic type definitions. The code establishes a reference hierarchy where the high bit distinguishes world/serial references from normal ones, and I can see the `WorldView` interface only exposes the current tick timestamp, while `SignalI` and `EffectI` are minimal interfaces that just return their kind identifiers.

Now I'm noticing a potential design issue with the reference space partitioning. The `IsSerialRef` check returns true for anything >= 1<<63, which includes `RefWorld` itself, so there's actually overlap between what's considered a serial ref and what's a world ref. This means the distinction isn't as clean as it appears—the world ref is technically also matching the serial ref predicate, and the order of checks matters when determining what type of reference you're dealing with.

There's also no validation for ref = 0, so it's unclear whether that's a valid reference or should be treated as nil.

Beyond that, I'm seeing another gap: the `ThinkCtx.Publish` interface lets any Logic publish effects to any target, including `RefWorld`, but the design doc clearly separates entity effects (parallelized by owner) from world effects (single-threaded). The type system doesn't enforce this distinction, so a Logic could accidentally publish a world effect during the Think phase without the framework having a clear way to catch or route it correctly.

There's also the awkwardness that Think doesn't have direct access to its own ref—it has to call `ID()` and then pass that to `Publish`, which works but leaves room for stale references to slip through.

And then there's the signal emission from Apply: since Apply can emit signals that become the next frontier, and those signals trigger Think phases that produce effects, there's a potential for cycles where Logic A's Apply emits to B, B's Think sends effects back to A, and A's Apply emits again—creating an infinite loop risk.

The design mentions budget controls to prevent this, but the Logic itself has no visibility into those limits, so the framework has to be extremely careful about enforcement.

Neither Think nor Apply have error handling—there's no way to report failures or invalid states back through the interface, and Apply can't signal partial failures when processing effects.

WorldView is also pretty minimal, exposing only the current timestamp.  The design doc expects Think to access world snapshots and run spatial queries, but the interface doesn't guarantee any of that—it's all left to concrete implementations, which means there's no compile-time safety or standard way to enforce read-only access to these capabilities.

The Publish function treats entity and world effects identically even though they route differently (parallel vs. serial), so the framework has to inspect effect targets at runtime to determine where they go, losing type safety in the process.

Inbox and Arrangement also lack sender context—there's no way to know the source ref or which round a signal or effect came from.

The design doc mentions embedding causal IDs in signal payloads, but that shifts the burden onto every signal type instead of having the framework wrap them with metadata.

Logic has no lifecycle hooks—no Init, OnSpawn, OnDespawn, or way to signal removal—even though the design doc talks about spawn/despawn through world effects.

Think's return value is ambiguous: it's supposed to indicate the next wakeup interval, but what happens if a Logic receives a signal and already has a pending timer? Does the new return value override the old one, or do they coexist? The semantics aren't clear.

The serial ref range check is problematic because RefWorld sits at exactly 1<<63, which is also the boundary for serial refs. This creates a collision where the world ref and serial ref space overlap, when they should be cleanly separated—world ref at 1<<63, serial refs above that, and normal refs below.

There's no API support for the pending-reservation pattern mentioned in the design docs, which is supposed to handle cross-owner transactions by freezing a count layer and confirming or rolling back in later rounds.

The ThinkCtx pointer could be captured and stored by a Logic implementation, allowing Emit and Publish to be called outside the Think phase from goroutines, which would violate the concurrency model.

The Logic interface is missing a method to expose LogicMeta, even though the design doc has a whole section on declarative metadata. The Apply method also lacks context about which round it's executing in, how many times it's been activated, or remaining budget. Additionally, Emit and Publish don't return errors, so there's no feedback if a Logic publishes to an invalid ref or exceeds its budget—the framework might silently drop effects without the caller knowing.

There's a potential race condition when Think modifies private state in parallel mode, since the framework needs to guarantee exclusive access to each Logic's state, but this guarantee isn't explicit in the interface. Finally, both Signal and Effect types have Kind() methods, but there's nothing preventing a developer from accidentally implementing both interfaces on the same type, creating confusion.

Now I'm examining the ref categorization logic more carefully. The code defines three categories—normal refs, world ref, and serial refs—but the implementation shows that world ref actually satisfies the serial ref condition, making them not mutually exclusive as the design doc suggests. Additionally, zero is treated as a valid normal ref, but it's likely meant to represent a nil or invalid ref, which creates ambiguity about what constitutes a legitimate Logic reference.

Looking at the broader architecture, I notice the CommitCtx correctly prevents Apply from calling Publish through its type signature, but it does expose Emit, which allows Apply to trigger signals that feed into the next round's frontier. This creates a multi-round execution model where Apply can indirectly spawn more Think executions and effects, but the interface doesn't provide safeguards against potential misuse or infinite loops.

There's also an ergonomic gap with Inbox and Arrangement being indexed by integer with no idiomatic way to iterate them in Go—no Range method or iterator pattern. More critically, the snapshot consistency guarantee relies entirely on convention rather than enforcement. Since W is constrained to the WorldView interface, the concrete implementation could mutate data through its methods, and there's nothing preventing a buggy WorldView from breaking the frozen snapshot assumption that Think depends on.

One more thing: if a Logic's Think method panics while running in a goroutine, there's no recovery mechanism in the interface, which could crash the entire server. The framework would need to handle this, but the interface doesn't account for it.
</thinking>

我仔细阅读了设计文档和接口定义，以下是深度分析。让我先检查一下项目中是否有其他相关文件来辅助理解。

**Tool Call: Search files for regex `parallel_theory`**
Status: Completed

Found 3 matches:

## Matches in game/docs/design/parallel.md

### # Parallel Tick Design › ## 前置约定 › L8-10
```
- 相关理论抽象与术语沉淀见 [../references/parallel_theory.md](../references/parallel_theory.md)

## Goal
```

## Matches in game/docs/references/survey.md

### # 并行 Tick 模型：数据分类与结构调研 › L10-11
```
理论来源包括 Actor Model、BSP/Pregel、Apache Beam、CRDT、Unity ECS、Orleans 等，完整引用见 [parallel_theory.md](./parallel_theory.md)。

```

## Matches in game/docs/references/think.md

### L1-2
```
两路 subagent 和文献都指向同一个结论：这套框架不该围绕“HP/MP/体力”这类业务字段建模，而该围绕“消息如何作用于状态”建模。详细调研我已经存到 parallel_theory.md，设计稿也已挂上引用：parallel.md。

```


**Tool Call: 2 matches**
Status: Completed

file:///Users/dongcheng/Project/legamerdc/game/en/engine.go

file:///Users/dongcheng/Project/legamerdc/game/en/world.go

**Tool Call: Read file `en/engine.go`**
Status: Completed

```game/en/engine.go
package engine

import "github.com/legamerdc/game/lib"

const (
	EventKindGameEvent EventKind = iota
	EventKindGEUpdate
	EventKindTagUpdate
	EventKindCast
)

type (
	EventKind int32
	WI        interface {
		Now() int64
	}

	UI any

	EI interface {
		Kind() EventKind
		Id() int32
	}

	Running[W WI, U UI] interface {
		Id() int32
		Update(W, U, *GAS[W, U])
		OnEvent(W, U, EI)
	}

	GAS[W WI, U UI] struct {
		Runnings lib.HeapIndexMap[int32, int64, Running[W, U]]
		Watcher  EventWatcher[W, U]
	}

	EventWatcher[W WI, U UI] struct {
		EventMap     map[int32]*lib.ArrayMap[int64, Listener[W, U]] // EventId
		GEUpdateMap  map[int32]*lib.ArrayMap[int64, Listener[W, U]] // GeId
		TagUpdateMap map[int32]*lib.ArrayMap[int64, Listener[W, U]] // TagId
		CastMap      map[int32]*lib.ArrayMap[int64, Listener[W, U]] // SkillId
	}

	Listener[W WI, U UI] struct {
		Id       int64
		Listener func(W, U, EI)
	}
)

```


**Tool Call: 4 matches**
Status: Completed

file:///Users/dongcheng/Project/legamerdc/game/docs/design/parallel.md

file:///Users/dongcheng/Project/legamerdc/game/docs/references/parallel_theory.md

file:///Users/dongcheng/Project/legamerdc/game/docs/references/survey.md

file:///Users/dongcheng/Project/legamerdc/game/docs/references/think.md

**Tool Call: Read file `docs/references/parallel_theory.md`**
Status: Completed

```game/docs/references/parallel_theory.md
# Parallel Simulation Theory Notes

## Scope

这份笔记不是讨论 MMORPG 具体玩法设计，而是回答两个框架问题:

1. 从数据层面上，`effect` / `signal` / `logic reaction` 有哪些基础类别
2. 为了支撑并行 tick / superstep，`world` 和 `logic` 应该长成什么 shape

目标是提炼出足够稳定的理论骨架，供后续接口设计复用。

## Primary Source Anchors

- [Actor Model of Computation: Scalable Robust Information Systems](https://arxiv.org/abs/1008.1459)
- [Introduction to Actors, Akka docs](https://doc.akka.io/libraries/akka-core/current/general/actors.html)
- [Behaviors as finite state machines, Akka docs](https://doc.akka.io/libraries/akka-core/current/typed/fsm.html)
- [Time, Clocks, and the Ordering of Events in a Distributed System](https://lamport.org/pubs/time-clocks.pdf)
- [Pregel: A System for Large-Scale Graph Processing](https://research.google/pubs/pregel-a-system-for-large-scale-graph-processing-2/)
- [Large-scale graph computing at Google](https://research.google/blog/large-scale-graph-computing-at-google/)
- [Basics of the Beam model](https://beam.apache.org/documentation/basics/)
- [Apache Beam Timer API](https://beam.apache.org/releases/javadoc/current/org/apache/beam/sdk/state/Timer.html)
- [Convergent and Commutative Replicated Data Types](https://pages.lip6.fr/Marc.Shapiro/papers/Comprehensive-CRDTs-RR7506-2011-01.pdf)
- [Unity ECS features in detail](https://docs.unity.cn/Packages/com.unity.entities%400.0/manual/ecs_in_detail.html)
- [Orleans benefits](https://learn.microsoft.com/en-us/dotnet/orleans/benefits)
- [Grain identity, Orleans docs](https://learn.microsoft.com/en-us/dotnet/orleans/grains/grain-identity)

## Theoretical Starting Point

对框架来说，最稳的抽象不是“HP/MP/体力/能量”这类业务字段，而是:

- 一个输入是请求、事实、通知还是时间触发
- 一个 effect 对状态的作用是否可交换、可结合、可幂等
- 一个变化归谁裁决、何时变得可见、是否需要后续确认

换句话说，框架应该先描述“消息如何作用于状态”，而不是“状态里放什么数值”。

## Data-Layer Taxonomy

### 1. Query / Snapshot Read

含义:

- 只读观察
- 不产生提交
- 只依赖当前 round 的 snapshot

接口含义:

- `think` 必须通过 query/snapshot 读取 world
- query 不应偷偷携带副作用
- query 与 effect 必须严格分离

理论来源:

- Lamport 的 happened-before 说明并发系统缺少天然全序，因此“看见什么”必须由清晰的可见性边界定义
- BSP / Pregel 强调 local compute 读取的是当前 superstep 的输入，而不是别人的中途写入

### 2. Command / Intent

含义:

- 对某个 owner 发出的请求
- 请求本身不等于事实
- 允许被拒绝、延迟、重试、拆分

例子抽象:

- “尝试开始一段行为”
- “请求占用一个资源”
- “请求对目标发起一次作用”

接口含义:

- `think` 最常产出的就是 intent
- intent 必须带 target owner / domain
- intent 通常需要配套 `Outcome`

理论来源:

- Actor 模型把跨边界交互建模为 message
- Orleans 的 grain identity / location transparency 强调消息只面向逻辑 identity，而不是物理对象指针

### 3. Event / Fact

含义:

- 已经发生的事实
- 不再等待批准
- 天然适合订阅、审计、回放、触发后续逻辑

接口含义:

- fact 不应再被当成待审批的 command
- fact 建议不可变
- fact 可以进入 signal router 做 fan-out

理论来源:

- Event-sourcing / pub-sub 的核心区分就是 “command asks, event tells”

### 4. Delta / Contribution

含义:

- 对目标状态的增量贡献
- 可以在一个 owner 上聚合 / reduce

常见代数形状:

- `sum`
- `max`
- `min`
- `set-add`
- `set-remove`
- `or` / `and`

接口含义:

- 这是默认并行 lane 最核心的 effect 类别
- 这类 effect 应显式声明 algebra
- 允许在 reduce 前不读取当前值

理论来源:

- CRDT 的核心结论是: 若状态满足 join-semilattice 或并发操作可交换，则无需细粒度同步也能得到合理合并
- Beam 的 `CombiningState`、`BagState`、`SetState` 也体现了“先积累、后归约”的接口思想

### 5. Patch / Replacement

含义:

- 对局部字段进行替换、覆盖或条件更新
- 往往不是天然可交换

接口含义:

- 必须带冲突策略或 guard
- 推荐限制为 `replace-if(predicate)`
- 若没有清晰 guard，通常不应进入默认无序 lane

理论来源:

- Actor mailbox 与 Lamport partial order 都说明跨发送者的天然顺序并不稳固
- 因此 replace 类 effect 如果没有 guard，语义会非常脆弱

### 6. Outcome / Ack / Nack / Confirm / Rollback

含义:

- 对 command/intention 的结果反馈
- 用于跨 owner 协调

接口含义:

- 只要存在“先申请，后确认”的协议，就必须有 outcome 类消息
- 如果规则需要 source 根据 target 反馈修改资源，最好显式使用 reservation + outcome，而不是隐式同步回调

理论来源:

- Actor / FSM 理论都天然适合把协议拆成 request-response
- Akka FSM 明确把行为看作有限状态机，收到不同事件后进入不同状态

### 7. Notification / Signal

含义:

- 告诉订阅者“有变化了”或“某类事实发生了”
- 不一定携带完整 payload

接口含义:

- signal 适合做解耦和 fan-out
- signal 可以进入 inbox 批量消费
- signal 不应被滥用为任意同步调用

理论来源:

- pub-sub / semantic subscription
- Actor 中“消息到 mailbox，再由 receiver 决定如何处理”的边界

### 8. Schedule / Timer

含义:

- 把工作推迟到未来的 tick / round / timestamp

接口含义:

- schedule 必须是一等公民
- timer 至少应区分 simulation time 与 processing time
- 同一 logic 的 future wakeup 应该数据化，而不是靠闭包或协程栈悬挂

理论来源:

- Beam 明确区分 state 与 timer，并提供 per-key timer callback
- FSM 理论天然包含 timeout / delayed transition

### 9. Structural Change

含义:

- spawn / despawn
- add/remove component
- subscribe/unsubscribe
- join/leave group

接口含义:

- 结构变更不应与普通数值 delta 混在一起
- 推荐通过 world command buffer / world effect 统一提交
- 推荐只在 barrier 后生效

理论来源:

- Unity ECS 使用 `EntityCommandBuffer` 延迟结构变更
- 结构变更往往会影响索引、内存布局、迭代器有效性

### 10. Barrier / Reduce Result

含义:

- 一轮 superstep 的汇总结果
- 决定下一轮看见什么、哪些 signal 可被消费

接口含义:

- barrier 是语义边界，不只是调度细节
- world snapshot、effect commit、signal routing 最好围绕 barrier 组织

理论来源:

- Valiant BSP 与 Pregel 都把 `compute -> communicate -> barrier` 作为主干

## Reaction Taxonomy

从数据层看，logic 处理 signal/intention 后，反应模式大致只有少数几种:

- `consume and transition`
- `accumulate / reduce`
- `forward / route`
- `defer / reschedule`
- `compensate / cancel`
- `ignore / drop`
- `escalate to serial island`

这里最关键的不是业务内容，而是:

- 是否只修改 private state
- 是否产出 owner-local delta
- 是否发起跨 owner 协议
- 是否需要 future wakeup

## Algebra Matters More Than Payload

并行框架真正需要知道的不是 payload 里装的是“血”还是“蓝”，而是这个 payload 的代数性质:

- 是否 commutative
- 是否 associative
- 是否 idempotent
- 是否需要 current-value guard
- 是否需要 exclusivity
- 是否需要 causal link

建议 effect 类型带这些元数据，而不是只带一个 payload struct。

一个务实接口可以长成:

- `kind`
- `domain`
- `target`
- `algebra`
- `guard_kind`
- `causal_id`
- `deadline`
- `priority`

其中 `algebra` 比业务字段更重要，因为它决定了:

- 能否并行 reduce
- 能否合批
- 能否重试
- 能否随机打乱顺序做测试

## World Shape

对并行 tick 模型来说，`world` 最好不是“一个可到处读写的大对象”，而是四层东西:

### 1. Snapshot / Query Surface

作用:

- 提供当前 round 的只读观察
- 暴露空间索引、实体索引、队伍索引等查询接口

要求:

- 只读
- 稳定于当前 round
- 支持按 owner / component / spatial 范围批量查询

### 2. Authoritative Stores

作用:

- 保存 entity public state
- 保存 world-level public state

要求:

- 不允许在 `think` 阶段被直接改写
- 只能经由 effect / world effect reducer 提交

### 3. Derived Indexes

作用:

- spatial grid
- team/faction membership
- visibility / neighborhood caches

要求:

- 应视为 committed state 的派生物
- barrier 后更新
- 不暴露脏中间态

### 4. Scheduling Surfaces

作用:

- active frontier
- timer wheel / future-event list
- signal mailboxes
- world command buffer

要求:

- 允许 `think` / `effect` 追加未来工作
- 但不允许同步重入执行

## Logic Shape

最适合 parallel design 的 `logic` 不是一个能直接抓全局对象图的脚本对象，而是:

- 有稳定 identity
- 绑定一个 owner domain
- 拥有 private mutable state
- 拥有 typed inbox
- 每次激活都消耗一批输入并产出一批 typed output

更接近:

- actor
- FSM
- Pregel vertex program

而不是:

- 任意共享内存对象
- 可同步回调世界任意角落的脚本

## Recommended Logic Contract

### Input

- round snapshot
- logic private state
- inbox batch
- scheduler metadata

### Output

- private state delta
- entity effects
- world effects
- emitted facts/signals
- schedules / timers
- optional outcomes

### Forbidden in Default Lane

- 直接写别的 entity public state
- 直接写 world public state
- 依赖同步 callback 链
- 依赖跨 owner 即时可见的新状态
- 未声明归约语义的 unordered updates

## Why the Hybrid Shape Fits

把 `world` 做成数据面，把 `logic` 做成 actor/FSM，把 scheduler 做成 BSP engine，有几个理论上的好处:

- Actor 提供 owner-local mutation 与 mailbox 边界
- BSP/Pregel 提供 round / barrier / frontier 的执行骨架
- ECS 提供 query-friendly data layout 与延迟结构变更
- Beam 提供 per-key state/timer 思路
- CRDT 提供“什么样的 effect 可以无序合并”的数学语言

这几个理论不是互斥的，它们正好分别解释:

- 谁拥有状态
- 何时可见
- 如何路由
- 如何延迟
- 哪些更新能并行归约

## Interface Implications

如果把上面的理论压成框架接口，最值得优先固化的不是业务字段，而是以下元信息。

### `EffectKind`

至少应描述:

- target domain: `self/entity/world`
- algebra: `sum/max/min/set-add/set-remove/replace-if/exclusive/serial`
- delivery: `same-round/next-round/future-tick`
- idempotence
- guard requirement

### `SignalKind`

至少应描述:

- semantic kind: `fact/notification/outcome/timer`
- routing scope: `self/watchers/world/subscription-query`
- payload schema
- causal relation

### `LogicMeta`

至少应描述:

- owner domain
- subscribed signal kinds
- max effects/signals per activation
- max reschedules per tick
- allow same-tick reenter or not
- serial-island requirement or not

### `WorldView`

至少应描述:

- snapshot identity / version
- spatial/entity/group query APIs
- no direct mutation

## Working Conclusion

框架层的核心问题不是“支持哪些 RPG 数值系统”，而是:

- 支持哪些状态转移代数
- 支持哪些消息语义
- 支持哪些 owner 边界
- 支持哪些时间/可见性边界

只要这些抽象定得对，HP/MP/体力/怒气/能量都只是 payload，不会反过来决定框架形状。

```


**Tool Call: Read file `docs/references/survey.md`**
Status: Completed

```game/docs/references/survey.md
SUCCESS: File outline retrieved. This file is too large to read all at once, so the outline below shows the file's structure with line numbers.

IMPORTANT: Do NOT retry this call without line numbers - you will get the same outline.
Instead, use the line numbers below to read specific sections by calling this tool again with start_line and end_line parameters.

# File outline for /Users/dongcheng/Project/legamerdc/game/docs/references/survey.md

# 并行 Tick 模型：数据分类与结构调研 [L1-435]
 ## 一、消息的九种基础类别 [L14-120]
  ### 1. Query（只读查询） [L18-29]
  ### 2. Command / Intent（意图请求） [L29-40]
  ### 3. Event / Fact（已发生的事实） [L40-51]
  ### 4. Delta / Contribution（增量贡献）——最重要的 effect 类别 [L51-64]
  ### 5. Patch / Replacement（条件替换） [L64-74]
  ### 6. Outcome（结果反馈） [L74-84]
  ### 7. Signal / Notification（通知信号） [L84-95]
  ### 8. Schedule / Timer（定时调度） [L95-106]
  ### 9. Structural Change（结构变更） [L106-120]
 ## 二、Effect 的代数性质——决定能否并行的关键 [L120-158]
  ### 代数类型一览 [L124-137]
  ### 核心原则 [L137-145]
  ### 实际例子：为什么代数比 payload 重要 [L145-158]
 ## 三、Logic 的反应模式 [L158-176]
 ## 四、World 应该是什么 Shape [L176-241]
  ### 层 1：Snapshot / Query Surface（快照查询面） [L180-196]
  ### 层 2：Authoritative Stores（权威状态存储） [L196-209]
  ### 层 3：Derived Indexes（派生索引） [L209-225]
  ### 层 4：Scheduling Surfaces（调度面） [L225-241]
 ## 五、Logic 应该是什么 Shape [L241-301]
  ### Logic 的固定结构 [L245-258]
  ### Logic 的统一入口 [L258-274]
  ### Logic 更像什么 [L274-286]
  ### Logic 的声明式元数据 [L286-301]
 ## 六、整体架构拼图 [L301-353]
 ## 七、需要接受的妥协 [L353-389]
  ### 妥协 1：不支持跨 owner 的同 round 原子事务 [L357-363]
  ### 妥协 2：成功语义锚定在单 owner [L363-369]
  ### 妥协 3：同 round 只能看到 barrier 前的世界 [L369-375]
  ### 妥协 4：同 tick 完成是尽力而为 [L375-381]
  ### 妥协 5：极少数强顺序玩法进入 serial island [L381-389]
 ## 八、可操作的结论 [L389-435]
  ### 框架需要优先定义的四个核心类型 [L391-418]
  ### 设计 effect 时的检查清单 [L418-432]
  ### 下一步建议 [L432-435]

Showing symbols 1-37 (total symbols: 37)


NEXT STEPS: To read a specific symbol's implementation, call read_file with the same path plus start_line and end_line from the outline above.
For example, to read a function shown as [L100-150], use start_line: 100 and end_line: 150.
```


## Assistant

Now I have a comprehensive understanding of the design and interface. Let me provide a deep analysis.

---

# Parallel Tick 设计深度审计

我从**接口契约 vs 设计意图的一致性**、**并发安全保证的完整性**、**运行时的可操作性**三个维度，逐条分析设计中存在的漏洞和风险。

---

## 一、Ref 空间设计存在歧义和缺陷

### 1.1 `IsSerialRef` 与 `RefWorld` 存在集合包含关系

```game/en/world.go#L9-L15
const RefWorld uint64 = 1 << 63

func IsWorldRef(r uint64) bool { return r == RefWorld }

func IsSerialRef(r uint64) bool { return r >= RefWorld }

func IsNormalRef(r uint64) bool { return r < RefWorld }
```

设计文档声明 ref 分为三类：**普通 Logic ref**、**World ref**、**Serial ref**，暗示三者是互斥分区。但代码中 `IsSerialRef(RefWorld) == true`，即 World ref 被 Serial ref 包含了。

这导致：
- 如果调度器先用 `IsSerialRef` 判断，World effect 会被当成 serial island logic 处理
- 如果路由代码写成 `if IsSerialRef { ... } else if IsWorldRef { ... }`，World ref 永远走不到第二个分支
- 判断顺序变成隐式约定，容易在不同开发者的代码中出现不一致

**建议**：要么用位标记明确区分（比如高两位 `00=normal`, `01=serial`, `10=world`），要么让 `IsSerialRef` 排除 World ref：

```/dev/null/fix.go#L1-1
func IsSerialRef(r uint64) bool { return r > RefWorld }
```

### 1.2 缺少 Nil Ref / Invalid Ref 定义

`IsNormalRef(0) == true`，但 ref=0 几乎一定应该是"无效/空"引用。没有 `RefNone` 常量和 `IsValidRef` 判断，意味着：
- `Emit(0, signal)` 和 `Publish(0, effect)` 不会在类型层面被拦截
- 框架路由时需要额外处理 0 值边界，但接口没有提供任何约定

---

## 二、类型系统未能区分 Entity Effect 与 World Effect

### 2.1 Publish 用同一函数签名覆盖两种语义

```game/en/world.go#L39-L44
	ThinkCtx[W WorldView, S SignalI, E EffectI] struct {
		World   W
		Emit    func(uint64, S)
		Publish func(uint64, E)
	}
```

设计文档明确区分了两种 effect 路径：
- **Entity effect**：并行，按目标 Logic owner 分桶
- **World effect**：串行，由 world owner 处理（spawn/despawn/area trigger）

但接口中 `Publish(uint64, E)` 只有一个，类型参数 `E EffectI` 也只有一个。这意味着：
- 同一个 `E` 类型既要承载 `DamageEffect`（Entity effect），又要承载 `SpawnNpcEffect`（World effect），区分完全靠 target ref 值
- **没有编译期保证**：Logic 可以 `Publish(RefWorld, DamageEffect{})` —— 把一个 entity effect 投递给 world，或者反过来
- 框架必须在运行时做 `(ref, effect kind)` 的合法性校验，但接口没有任何 hook 来声明"这个 effect kind 只能投递给 normal ref / world ref"

**建议**：要么拆成两个函数（`Publish` + `PublishWorld`），要么在 `EffectI` 接口上增加 `TargetDomain() Domain` 方法让框架做运行时校验。

---

## 三、WorldView 过于单薄，snapshot 只读语义无法在类型层面保证

### 3.1 WorldView 只有 `Now()`

```game/en/world.go#L18-L20
	WorldView interface {
		Now() int64
	}
```

设计文档要求 Think 能做空间查询、可见性查询、附近目标列表等操作，`parallel_theory.md` 更是明确要求 WorldView 提供 `spatial/entity/group query APIs`。但接口约束只有一个 `Now()`。

虽然泛型参数 `W WorldView` 可以被具体类型扩展，但这意味着：
- 框架对 WorldView 的能力边界毫无感知
- 具体类型可以暗地里加入任意 mutable 方法，打破 snapshot 只读语义
- 不同 Logic 实现者可能对 WorldView 的能力做出不同假设

### 3.2 没有 snapshot 版本标识

`parallel_theory.md` 明确建议 WorldView 应有 `snapshot identity / version`，但当前接口没有这个信息。这导致：
- 无法在运行时检测 Logic 是否意外读取了非当前 round 的数据
- 无法在日志/调试中追踪 "这个 Think 看到的是哪一轮的世界"

---

## 四、Effect 和 Signal 缺少必要的元信息

### 4.1 不携带 source ref

```game/en/world.go#L22-L28
	SignalI interface {
		Kind() SignalKind
	}

	EffectI interface {
		Kind() EffectKind
	}
```

`Inbox[S]` 和 `Arrangement[E]` 提供的 signal/effect 都只有 payload，**没有 source ref**。但在实际游戏逻辑中，Apply 几乎总是需要知道"谁打了我"：
- 伤害来源归属（用于击杀计数、仇恨列表）
- Signal 因果链追踪
- 死亡反馈给攻击者

如果由每个 effect payload 自己携带 source，就会出现：
- 每个 effect struct 都要手动加 `Source uint64` 字段
- 没有统一约束，某些 effect 可能忘记加
- 框架无法统一审计因果链

**建议**：框架层包装一层 `Envelope[T]{ Source uint64, RoundID int, Payload T }`，让 Inbox/Arrangement 返回的是 envelope 而不是裸 payload。

### 4.2 EffectI 缺少代数元数据

`parallel_theory.md` 花了大量篇幅论证 `algebra` 比 `payload` 更重要，建议 effect 至少带 `kind, domain, target, algebra, guard_kind, causal_id, deadline, priority`。设计文档也专门提到"未来可选: effect 代数元数据"。

但当前 `EffectI` 只有 `Kind() EffectKind`。虽然文档说"当前版本中框架不需要知道代数性质"，但这个决定的风险在于：**一旦无序安全检测需要 shuffle 验证，框架连哪些 effect 是同类都分不清楚**。至少应该保留一个 `Algebra() AlgebraKind` 的扩展点。

---

## 五、Apply 能 Emit Signal 导致的多轮级联风险

### 5.1 Signal 乒乓与预算不可见

```game/en/world.go#L48-L51
	CommitCtx[W WorldView, S SignalI] struct {
		World W
		Emit  func(uint64, S)
	}
```

设计文档允许 Apply 通过 `Emit` 向任意 Logic 发送 signal。这些 signal 成为下一轮的 frontier，触发新的 Think → Apply → Emit 循环。

风险场景：
1. Logic A 的 Apply 处理伤害 effect → Emit "HP Changed" signal 给 Logic B
2. Logic B 的 Think 触发 buff 反弹 → Publish effect 给 Logic A
3. Logic A 的 Apply 处理反弹 → Emit "HP Changed" 给 Logic B
4. 循环……

设计文档提到了 budget 机制（`max_rounds_per_tick` 等），但：
- **Logic 对 budget 完全不可见**：Think/Apply 没有任何方式知道当前是第几轮、还剩多少预算
- **Apply 的 Emit 没有计数约束暴露给实现者**：元数据 `max_signals_per_activation` 在设计文档中提及，但接口完全没有体现
- 当 budget 超限时，框架的行为（丢弃？延迟？告警？）对 Logic 不透明

### 5.2 Apply 的 Emit 目标无限制

Apply 可以 `Emit(anyRef, signal)`，包括 `Emit(RefWorld, signal)` 或 `Emit(self, signal)`。设计文档说"Logic 必须要么终止，要么显式等待未来事件。不允许无条件的 same-tick 自激活闭环"，但接口层面没有任何机制阻止 `Apply → Emit(self) → Think → Apply → Emit(self)` 的自激活死循环。

---

## 六、Think 返回值语义不完备

```game/en/world.go#L55-L58
	Logic[W WorldView, S SignalI, E EffectI] interface {
		ID() uint64
		Think(*ThinkCtx[W, S, E], Inbox[S]) int64
		Apply(*CommitCtx[W, S], Arrangement[E])
	}
```

### 6.1 定时器语义与 signal 激活冲突

`Think` 返回 `int64` 表示下次自动苏醒间隔。但一个 Logic 可能同时因为以下原因被激活：
- 上一轮遗留的 signal
- 定时器到期
- 来自网络的输入

问题：
- 如果 Logic 已经有一个 pending timer（比如 5 ticks 后苏醒），中间因 signal 被激活并返回了新的间隔 3，**旧 timer 是被覆盖还是保留**？接口没有定义
- 如果 Logic 被 signal 激活但不想改变已有 timer，**应该返回什么值**？返回 0（不调度）会取消已有 timer 吗？返回负值呢？

### 6.2 Think 无法区分激活原因

Think 收到一个 `Inbox[S]`，但当 Inbox 为空时，激活原因可能是定时器、也可能是 tick 初始 frontier（网络输入）。Logic 无法区分自己为什么被唤醒，这在某些 FSM 场景下会导致逻辑歧义。

---

## 七、Logic 生命周期缺失

### 7.1 无 Init / Dispose 接口

`Logic` 接口只有 `ID()`, `Think()`, `Apply()`。但：
- Logic 被 spawn 时如何初始化？（比如加载配置、设置初始状态）
- Logic 被 despawn 时如何清理？（比如释放外部资源、发送告别 signal）
- 如果 spawn 是通过 World Effect 实现的，新 Logic 第一次执行前的初始化时机在哪里？

### 7.2 无 Meta 方法

设计文档用了一整节描述 `LogicMeta`（max_effects_per_activation、priority、cost_hint、serial_only 等），但接口上 `Logic` 没有 `Meta() LogicMeta` 方法。这些元数据的获取方式完全不在接口契约中。

---

## 八、并发安全的"君子协定"问题

### 8.1 ThinkCtx 的函数引用可被逃逸

`ThinkCtx` 是个 struct，里面的 `Emit` 和 `Publish` 是 `func` 值。Go 语言无法阻止 Logic 在 Think 中将这些函数存储到自己的私有状态中，然后在 Think 返回后的其他时机调用：

```/dev/null/bad.go#L1-L6
func (l *MyLogic) Think(ctx *ThinkCtx[...], inbox Inbox[...]) int64 {
    l.savedPublish = ctx.Publish  // 逃逸！
    go func() {
        l.savedPublish(someRef, someEffect)  // Think 结束后异步调用
    }()
}
```

这会完全摧毁并发模型。接口层面无法阻止，只能靠代码规范和 review。

### 8.2 WorldView 的具体类型可能暴露 mutable 方法

`W WorldView` 的 constraint 只要求 `Now() int64`。但如果具体类型是一个指针类型并且有修改方法（比如 `(*ConcreteWorld).SetHP()`），Logic 可以通过类型断言绕过只读约束：

```/dev/null/bad2.go#L1-L4
func (l *MyLogic) Think(ctx *ThinkCtx[*ConcreteWorld, ...], inbox ...) int64 {
    ctx.World.SetHP(targetID, 0) // 直接改 world 状态！
    // ...
}
```

框架无法用 Go 的类型系统阻止这种行为。

---

## 九、Apply 没有返回值 —— 错误吞没

`Apply(*CommitCtx[W, S], Arrangement[E])` 没有返回值。如果 Apply 阶段遇到非法 effect（比如对已死亡目标施加 buff）或者内部错误，无法向框架报告。这意味着：
- 框架不知道 Apply 是否成功
- 无法实现 Outcome/Ack/Nack 协议（`parallel_theory.md` 明确提到了这个需求）
- 调试时无法从框架层面追踪哪些 effect 被成功处理、哪些被丢弃

---

## 十、现有 engine.go 的 GAS 模式与新模型存在冲突

```game/en/engine.go#L15-L25
	Running[W WI, U UI] interface {
		Id() int32
		Update(W, U, *GAS[W, U])
		OnEvent(W, U, EI)
	}

	GAS[W WI, U UI] struct {
		Runnings lib.HeapIndexMap[int32, int64, Running[W, U]]
		Watcher  EventWatcher[W, U]
	}
```

现有 `GAS` 模式中：
- `Running.Update()` 直接接收 `*GAS`（可变引用），可以在内部添加/移除 Running、注册/注销 Watcher —— 这是 **同步回调链** 模式
- `EventWatcher` 的 `Listener` 是 `func(W, U, EI)` —— 这正是设计文档明确反对的 **closure + 同步回调**
- `OnEvent` 没有经过 signal routing，是直接同步触发

这意味着从 GAS 迁移到并行模型需要大量重构，而且两套接口(`world.go` 和 `engine.go`)目前共存在同一个 package 中，没有明确的迁移边界。

---

## 总结：按严重性排序的漏洞清单

| 严重性 | 问题 | 影响 |
|--------|------|------|
| 🔴 高 | Publish 不区分 Entity/World Effect | 运行时路由错误、无编译期安全 |
| 🔴 高 | ThinkCtx 函数可被逃逸存储 | 并发模型被绕过 |
| 🔴 高 | WorldView 只读约束仅为约定 | 可通过类型断言写穿 snapshot |
| 🟠 中 | Ref 空间 Serial/World 有重叠 | 路由歧义、判断顺序依赖 |
| 🟠 中 | Effect/Signal 无 source ref | 因果链断裂、业务逻辑冗余 |
| 🟠 中 | Think 返回值的 timer 语义未定义 | 定时器覆盖/保留不确定 |
| 🟠 中 | Apply 可 Emit 导致自激活死循环 | budget 机制是唯一防线但对 Logic 不透明 |
| 🟠 中 | Apply 无返回值 | 错误吞没、无法实现 outcome 协议 |
| 🟡 低 | WorldView 过于单薄 | 扩展完全依赖具体类型，框架失去约束能力 |
| 🟡 低 | 缺少 Nil Ref 定义 | 0 值 ref 的边界行为未定义 |
| 🟡 低 | Logic 缺少生命周期方法和 Meta | 与设计文档的 LogicMeta 章节脱节 |
| 🟡 低 | 现有 GAS 模式未做迁移隔离 | 两套模型共存一个 package 易混淆 |

---

最核心的一个判断：**当前接口的保护力度远低于设计文档的表述力度**。设计文档描述了一个非常严谨的 ownership + typed effect + barrier 模型，但 `world.go` 的接口留下了太多可以"绕过去"的口子。Go 的类型系统确实有其局限性，但至少可以在以下方面加强：

1. **拆分 `Publish` 为 entity/world 两条路径**（或在 EffectI 上加 domain 标记）
2. **给 Effect/Signal 套 Envelope 包装**，统一携带 source、round、causal_id
3. **明确 Ref 空间的互斥分区**，加上 `RefNone` 和 `IsValidRef`
4. **给 Logic 加 `Meta()` 和生命周期方法**
5. **Think 的定时器语义**需要明确文档化（覆盖 vs 最小值 vs 独立管理）

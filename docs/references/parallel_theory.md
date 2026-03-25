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

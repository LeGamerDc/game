两路 subagent 和文献都指向同一个结论：这套框架不该围绕“HP/MP/体力”这类业务字段建模，而该围绕“消息如何作用于状态”建模。详细调研我已经存到 parallel_theory.md，设计稿也已挂上引用：parallel.md。

1. effect / signal 应该怎么从理论上分类

最有用的不是按业务分，而是按 4 个维度分：

语义角色：query、command/intent、event/fact、delta、patch、outcome、signal/notification、schedule/timer、structural change
代数性质：commutative、associative、idempotent、guarded、exclusive
可见性时间：same-round、next-round、future-tick
作用域：self、entity、world
这意味着框架里的核心类型不该先问“payload 是不是血量”，而该先问：

这是请求还是事实
这是可归约增量还是条件替换
它归谁裁决
它什么时候可见
它能不能在无序输入下安全合并
所以我现在更倾向于把 effect 理解成“会导致 owner 状态提交的 typed state transition”，把 signal 理解成“驱动 logic 继续运行的 typed delivery item”。
在默认并行 lane 里，最应该鼓励的是 delta 类 effect，因为它们天然适合 reduce。patch、exclusive、reservation/outcome 这类就要更谨慎，要么带 guard，要么走显式协议，要么进 serial island。

2. logic / world 应该长什么 shape

最合适的是一个 hybrid：

world 是数据面，不是任意脚本执行环境
logic 是 actor/FSM 式的小状态机
scheduler 是 BSP/superstep 引擎
也就是：

WorldView
当前 round 的只读 snapshot
空间索引、实体索引、组队/阵营索引等 query API
Authoritative Stores
entity public state
world public state
Derived Indexes
由 committed state 派生，barrier 后更新
Scheduling Surfaces
active frontier
timer wheel / future events
signal inboxes
world command buffer
而 logic 最好固定成这个形状：

logic id
owner domain
private state
typed inbox
subscriptions
meta/budget
一个统一入口，类似 think(snapshot, inbox_batch) -> outputs
outputs 再统一产出：

private state delta
entity effects
world effects
emitted facts/signals
schedules/timers
optional outcomes
所以本质上，world 提供“看”和“挂号”，logic 负责“想”和“发消息”，effect reducer 负责“提交”。这正好把 Actor 的 ownership、Pregel/BSP 的 superstep、ECS 的 data layout、Beam 的 state/timer、CRDT 的 merge algebra 拼在一起了。

如果继续往下推进，下一步最值钱的不是再谈玩法例子，而是把这几个框架类型定出来：

EffectKind
SignalKind
LogicMeta
WorldView
我建议下一轮就直接把这四个类型收成一版 Go 接口草图。

主要来源

Actor Model of Computation
Lamport 1978: Time, Clocks, and the Ordering of Events
Pregel
Apache Beam model
CRDT survey
Unity ECS features in detail
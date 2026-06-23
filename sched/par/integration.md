# Scheduler 接入文档

本文面向 demo 或上层 framework 的开发者，目标是让接入方不必完整阅读 `sched` 实现，也能正确理解 Scheduler 的概念边界、数据分层和最小接入方式。

如果本文与代码不一致，以 `sched/world.go` 的接口定义和 `sched/` 下当前实现为准。更完整的设计背景见 `docs/design/parallel.md` 与 `docs/design/scheduler.md`。

---

## 1. 最小心智模型

Scheduler 不是通用任务系统，也不是锁/事务框架。它要求业务逻辑显式拆成两类阶段：

```text
Think Phase                Apply Phase
===========                ===========
read stable views          receive effects grouped by target
mutate private state       mutate own public state
publish typed effects      emit typed signals
emit typed signals         write staged public views
write staged views
return next wakeup
```

核心约束是 **Logic = Owner**：

- Scheduler 调度的最小单位是 `Logic`。
- `Effect` 的目标 ref 表示某个 owner。
- 同一个 owner 的 public state 只能由它自己的 `Apply` 提交。
- 其他 owner 不能直接改它，只能 `Publish(targetRef, effect)`。
- `Signal` 用来激活目标 owner 的下一次 `Think`，不是直接改状态。

因此，接入 demo 时优先问两个问题：

1. 这段数据归哪个 owner 拥有？
2. 这次变化应该在 Think 改 private data，还是在 Apply 改 public data？

只要这两个问题没有回答清楚，就不要急着写代码。

---

## 2. Ref 与 Owner 分类

`sched` 统一使用 `uint64` ref 标识投递目标。

| 类型 | 判定 | 含义 | 可被 Think | 可被 Apply |
|------|------|------|------------|------------|
| NormalRef | `IsNormalRef(ref)` | 普通业务 Logic，例如 unit / projectile / trigger | 可以 | 可以 |
| RefWorld | `IsWorldRef(ref)` | world 这个特殊 owner | 不作为普通实体 Think | 可以 |
| SerialRef | `IsSerialRef(ref)` 且不是 `RefWorld` | 串行域 / 全局归并目标 | 不可以 | 可以作为 effect 聚合目标 |
| RefNone | `RefNone` | 无效 ref | 不可以 | 不可以 |

注意：当前 `IsSerialRef(ref)` 的实现是 `ref >= RefWorld`，因此 `RefWorld` 也满足这个判断。接入代码需要先判断 `IsWorldRef(ref)`，再处理其他 serial refs。

### NormalRef

NormalRef 对应真实存在的 `Logic` 实例。它可以：

- 被外部输入或其他 Logic 的 `Emit` 激活 Think。
- 在 Think 中 `Publish` effect 给其他 owner。
- 在 Apply 中处理发给自己的 effects。
- 通过 `WriteStage` 提交自己的 staged views。
- 通过 Think 返回 delay 注册自己的下一次 timer wakeup。

### RefWorld

World 是特殊 owner，不是模型外的第三阶段。对 world 的操作也应建模为 effect：

- spawn / despawn
- entity registry 更新
- 全局队伍、副本、场景索引更新
- 其他必须由 world 统一裁决的 shared state

业务 Logic 不应在 Think 中直接改 world shared state，而是 `Publish(RefWorld, effect)`，由 world owner 的 Apply/reducer 处理。

### SerialRef

SerialRef 表示显式串行域，适合极少数不能自然拆成 owner-local Apply 的操作。

它的关键边界：

- SerialRef **没有普通 Logic 实体**。
- SerialRef **不可能 Think**。
- 不要对 SerialRef 调用外部 `Scheduler.Emit`。
- 不要让 SerialRef 进入 timer wheel。
- 可以从 Think 中 `Publish(serialRef, effect)`，让同一个 serial ref 下的 effects 被归纳到一起 Apply。

换句话说，SerialRef 是 apply-only 的归并目标，不是可被 AI、timer、signal 驱动的业务对象。

当前 `sched` 的普通 Apply 调用路径仍以 `LogicProvider.GetLogic(ref)` 为入口；如果 demo 要使用 SerialRef，需要在 world/framework 层提供清晰的 apply-only dispatch，而不是把它混入普通实体表并当成可 Think 的 Logic 使用。如果暂时没有这层 dispatch，demo MVP 应先避免引入 SerialRef。

---

## 3. Public / Private Data 必须先分清

Scheduler 能并行的前提不是 goroutine，而是数据访问模式被限制住了。

接入 demo 时建议采用下面这个更严格的子集：

| 数据层 | 示例 | Think 阶段 | Apply 阶段 | 对其他 owner 可见 |
|--------|------|------------|------------|-------------------|
| Logic private data | AI 状态、施法读条、技能 CD、buff 内部计时、行为树栈 | 可读写 | 通常不写 | 不可见 |
| Logic public data | HP/MP、位置、阵营、死亡状态、公开 tags、公开 buffs | 只读稳定视图；不要直接改 | 可读写，作为权威提交点 | 通过 world/staged view 可见 |
| World public data | entity registry、空间索引、场景/队伍索引 | 只读稳定视图 | 由 `RefWorld` / serial reducer 提交 | 通过 world 查询可见 |
| Staged public view | WatchBits、AOI membership、AttrSummary、public summary | `WriteStage` 提交当前 owner 的 view | `WriteStage` 提交当前 owner 的 view | Promote 后可被查询 |

### Private data

Private data 是当前 Logic 的内部运行记忆。Think 可以直接修改它，因为同一个 Logic 的 Think 由 owner 边界保护，不会有其他 owner 同时写它。

典型例子：

- AI 下一步意图
- 当前正在释放的技能阶段
- 本地冷却、读条、buff timer
- 行为树运行栈
- 只影响本 Logic 后续决策的缓存

Private data 不应该被其他 owner 查询。如果某个 private 结果需要影响别人，必须变成：

- effect payload
- signal payload
- staged public view
- 或由 Apply 提交后的 public data

### Public data

Public data 是别的 owner 可以观察、或可以通过 effect 影响的权威状态。接入 demo 时，应把它视为 **Apply-owned data**：

- Think 可以基于稳定视图做决策，但不要直接写 public data。
- Apply 是 public data 的权威提交点。
- 自己改自己的 HP/MP/Tag 这类 public data，也建议走 self effect，让所有 public mutation 共享同一套 reducer。

Go 代码层面无法完全阻止 Logic 在 Think 里直接写字段，但 demo 层应尽量遵守这个约束。否则后续 demo 很容易变成“Think 一边改共享事实，一边又 Publish effect”，使并行语义失去可解释性。

### Staged public view

StagedState 不是 private data，也不是额外的共享可写状态。它是“阶段稳定视图”的提交机制。

常见用途：

- 把 Apply 后的 public state 摘要投射给下一轮 Think 查询。
- 维护 WatchBits / 订阅摘要。
- 维护 AOI membership / 可见性摘要。
- 维护 attribute summary，让其他 owner 只读少量公开信息。

`WriteStage(kind, state)` 没有 ref 参数，Scheduler 会自动把它绑定到当前执行 owner。接入方不能写其他 owner 的 staged view。

---

## 4. Think 阶段接入规则

`Think(ctx, inbox) int64` 的职责是处理输入 signal、推进 private state、产出 typed effect/signal，并返回下一次自动唤醒时间。

Think 可以做：

- 读取 `ctx.World` 暴露的稳定查询。
- 读取自己的 private data。
- 修改自己的 private data。
- 消费 `inbox` 中的 signals。
- `ctx.Publish(targetRef, effect)` 给目标 owner。
- `ctx.Emit(targetRef, signal)` 激活目标 owner 后续 Think。
- `ctx.WriteStage(kind, state)` 提交当前 owner 的 staged view。
- 返回 `delay > 0` 注册自己的下一次 timer wakeup。

Think 不应该做：

- 直接写其他 owner 的任何状态。
- 直接写 world shared state。
- 直接写自己的 public data。
- 让其他 Logic 同步执行。
- 给别的 Logic 注册 timer。
- 向 SerialRef `Emit` signal。

### Signal inbox

Signal 是 Think 的输入，不是 Apply 的输入。它适合表达“请重新思考”或“某件事发生了，你可以在下一轮处理”：

- 玩家输入
- AI 感知变化
- Apply 后产生的事件通知
- 目标状态变化后的反应触发

并发模式下，同一目标 ref 的 signals 会按 ref 分组并传给一次 Think。串行模式的初始 frontier 也会把同一 ref 的 signals 批量传入一次 Think；串行 cascade 中新产生的 signal 会 inline 递归触发，通常是单条 signal。

因此 Logic 必须同时支持：

- `inbox.Len() == 0`：timer-only wakeup。
- `inbox.Len() == 1`：单个 signal。
- `inbox.Len() > 1`：同一轮聚合到的多个 signals。

---

## 5. Apply 阶段接入规则

`Apply(ctx, effects)` 的职责是把发给当前 owner 的 effects 提交到本 owner 的 public data，并按需发出 signals / staged views。

Apply 可以做：

- 读取 `ctx.World` 暴露的稳定查询。
- 读取和修改当前 owner 的 public data。
- 读取实现 public reducer 所需的本 owner 内部表，例如 buff/modifier/attribute 聚合表。
- 消费 `effects` 中发给自己的 typed effects。
- `ctx.Emit(targetRef, signal)` 通知后续 Think。
- `ctx.WriteStage(kind, state)` 提交当前 owner 的 staged view。

Apply 不可以做：

- `Publish` 新 effect。`CommitCtx` 没有 `Publish`。
- 直接写其他 owner 的状态。
- 写其他 owner 的 staged view。
- 依赖 effects 的精确到达顺序才能保持语义正确。

### Effect inbox

并发模式下，同一 target ref 的多个 effects 会被分组后一次传入 Apply。串行模式下，`Publish` 会 inline 触发 Apply，通常一次只传入一个 effect。

因此 Apply 必须同时支持：

- 单 effect。
- 多 effect batch。
- 任意合法顺序。

`EffectI.Order()` 只提供同 ref 内的排序键，适合做低风险的稳定展示或局部优先级；不要把核心正确性建立在全局顺序上。玩法语义应当对同一轮内的不同处理顺序保持容忍。

---

## 6. Effect / Signal 的分工

| 类型 | 由谁发出 | 投递到哪里 | 消费阶段 | 典型语义 |
|------|----------|------------|----------|----------|
| Effect | Think | owner ref / RefWorld / SerialRef | Apply | 改变目标 public state 的 intent |
| Signal | Think 或 Apply | NormalRef | Think | 激活目标重新决策或反应 |
| StagedState | Think 或 Apply | 当前 owner，隐式绑定 | PromoteStages | 发布阶段稳定查询视图 |

### Effect 应该携带什么

Effect payload 应携带 source 端已经计算好的中间结果，以及 target Apply 必须知道的少量 metadata。

推荐：

- `sourceRef`
- `rawDamage`
- `element`
- `critFlags`
- `sourceLevel`
- `penetration`
- `abilityId`
- `causalId`

避免：

- closure
- 指向 source Logic 的指针
- source 全量属性快照
- 需要 target 回调 source 才能完成的同步协议

跨 source/target 的公式必须拆成两段：

```text
payload     = source_fn(source_private, source_public, target_stable_view)
finalResult = target_fn(payload, target_current_public)
```

例如伤害：

```text
Think(source):
  rawDamage = attackPower * skillMultiplier
  Publish(target, DamageEffect{Source: self, RawDamage: rawDamage, Element: Fire})

Apply(target):
  finalDamage = reduceByDefense(effect.RawDamage, target.CurrentDefense)
  target.HP -= finalDamage
  WriteStage(StageAttrSummary, target.AttrSummary())
```

### Signal 应该携带什么

Signal 更像 notification / wakeup。它应该足够小，且不承担 public mutation：

- `DamageTakenSignal`
- `DeathSignal`
- `TargetLostSignal`
- `CommandSignal`
- `BuffExpiredSignal`

如果某个消息会直接改变目标 public state，它应该是 Effect，不是 Signal。

---

## 7. StagedState 接入方式

World 必须实现：

```go
type StagePromoter interface {
    PromoteStages(Inbox[RefStage])
}
```

Scheduler 在阶段边界调用 `PromoteStages`：

- 并发模式：Think barrier 后、Apply barrier 后。
- 串行模式：inline Emit/Publish/Think/Apply 边界。

同一 owner + `StageKind` 在同一阶段多次 `WriteStage` 时，last-write-wins。

### StageKind 建议

demo 可以先定义少量明确的 staged domains：

```go
const (
    StagePublicSummary sched.StageKind = iota + 1
    StageAttrSummary
    StageAOIMembership
    StageWatchBits
)
```

每个 domain 独立维护，不要把所有 staged data 塞进一个巨大结构。这样可以避免 demo 早期为了方便查询而把 private data 一起泄露出去。

### PromoteStages 示例

```go
func (w *World) PromoteStages(inbox sched.Inbox[sched.RefStage]) {
    for i := 0; i < inbox.Len(); i++ {
        rs := inbox.At(i)
        switch rs.Kind {
        case StagePublicSummary:
            w.publicSummaries[rs.RefId] = rs.State.(PublicSummary)
        case StageAttrSummary:
            w.attrSummaries[rs.RefId] = rs.State.(AttrSummary)
        }
    }
}
```

`PromoteStages` 是串行调用，可以更新 world/framework 里的版本化查询表。它不应该反过来执行业务 Logic，也不应该产生新的 Effect/Signal。

---

## 8. World 接入清单

World 需要同时满足三个接口：

```go
type World interface {
    Now() int64
    Version() uint32
    Round() int32
}

type LogicProvider[L any] interface {
    GetLogic(uint64) (L, bool)
}

type StagePromoter interface {
    PromoteStages(Inbox[RefStage])
}
```

接入要求：

- `GetLogic` 在并发 Think/Apply 期间必须并发读安全。
- `L` 应是指针类型，例如 `*Unit`，否则 Logic 内部状态修改不会持久化。
- tick 中不要并发增删普通 Logic map；spawn/despawn 应通过 world effect 在 Apply 阶段归并。
- `Now/Version/Round` 应表达稳定 tick 观测，不要在单个 Think 调用中变化。
- world 查询方法应返回只读视图或值拷贝，不要把可写内部结构暴露给 Think。

最小构造：

```go
type Unit struct {
    id uint64
    // private data
    ai       AIState
    cooldown CooldownTable

    // public data, Apply-owned
    hp   int
    pos  Vec2
    tags TagState
}

func (u *Unit) ID() uint64 { return u.id }

func (u *Unit) Think(ctx *sched.ThinkCtx[*World, Signal, Effect], inbox sched.Inbox[Signal]) int64 {
    // consume signals, update private state, publish effects
    return u.cooldown.NextWakeup(ctx.World.Now())
}

func (u *Unit) Apply(ctx *sched.CommitCtx[*World, Signal], effects sched.Inbox[Effect]) {
    // reduce effects into public state
    ctx.WriteStage(StagePublicSummary, u.PublicSummary())
}

type World struct {
    units map[uint64]*Unit
    // staged read models
    publicSummaries map[uint64]PublicSummary
}

func (w *World) GetLogic(ref uint64) (*Unit, bool) {
    u, ok := w.units[ref]
    return u, ok
}
```

---

## 9. Demo 开发顺序建议

建议按下面顺序接入，而不是先写完整玩法系统：

1. 定义 ref 规划：普通 unit/projectile refs、`RefWorld` effects、是否暂缓 SerialRef。
2. 给每个 Logic 类型画出 public/private data 表。
3. 定义最小 `Signal` 和 `Effect` 类型，先覆盖移动、伤害、死亡三条链路。
4. 实现 World 的只读查询和 `PromoteStages`。
5. 实现一个 Unit Logic：Think 处理输入、Apply 处理 damage/heal。
6. 用 self effect 替代 Think 直接改 public data。
7. 加入 StageKind：至少有 public summary / attr summary。
8. 最后再引入复杂系统：buff、ability、projectile、AOI、serial islands。

---

## 10. 常见误用检查表

提交 demo 代码前，快速检查：

- [ ] 是否所有跨 owner 状态修改都变成了 typed Effect？
- [ ] 是否所有 Signal 都只用于 Think 激活，而不是直接改 public state？
- [ ] Think 是否只修改 private data 或 staged view？
- [ ] Apply 是否是 public data 的权威提交点？
- [ ] Effect payload 是否没有 closure、没有 Logic 指针、没有 source 全量快照？
- [ ] Apply 是否能处理单 effect 和多 effect batch？
- [ ] 同一轮 effect 顺序变化时，玩法结果是否仍可接受？
- [ ] `WriteStage` 是否只发布 public/staged summary，没有泄露 private data？
- [ ] `GetLogic` 是否并发读安全？
- [ ] SerialRef 是否没有被注册成可 Think 的普通实体？
- [ ] SerialRef 是否只通过 Publish 进入 apply-only 归并路径？

如果某条规则很难满足，通常说明这段逻辑需要重新决定 truth owner，或拆成多轮 signal/effect 协议。

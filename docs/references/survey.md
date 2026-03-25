# 并行 Tick 模型：数据分类与结构调研

基于 [parallel.md](../design/parallel.md) 的并发设计，本文回答两个框架层面的问题：

1. `effect` / `signal` / `think 的反应` 在数据层面应该有哪些基础类别？
2. `world` 和 `logic` 应该是什么 shape 才能支撑并行 tick？

核心结论先放在前面：**框架不该围绕"HP/MP/怒气"等业务字段建模，而该围绕"消息如何作用于状态"建模。** 只要消息的代数性质和归属边界定对了，任何具体数值系统都只是 payload。

理论来源包括 Actor Model、BSP/Pregel、Apache Beam、CRDT、Unity ECS、Orleans 等，完整引用见 [parallel_theory.md](./parallel_theory.md)。

---

## 一、消息的九种基础类别

parallel.md 中 `think` 产出的东西和系统内部流转的消息，从数据语义上可以归为九类。这不是按"伤害/治疗/移动"的业务维度分的，而是按**消息对状态的作用方式**分的。

### 1. Query（只读查询）

**含义**：读取世界状态，不产生任何写入。

**游戏举例**：
- 法师 AI 在 think 阶段查"半径 10 米内有没有敌人"
- 治疗逻辑查"队友中谁的 HP 百分比最低"
- 触发器查"某个区域内有多少玩家"

**框架要求**：think 阶段的所有读取都必须走 query，读到的是当前 round 的冻结快照，不是别人正在写的中间状态。这是整个并行模型成立的前提——所有 think 看到的世界是同一份。

### 2. Command / Intent（意图请求）

**含义**：对某个 owner 发出的"我想做某事"的请求。请求 ≠ 事实，它可以被拒绝、延迟、部分生效。

**游戏举例**：
- `CastIntent{source: 玩家A, skill: 火球, target: 怪物B}` —— 玩家想释放技能，但还没真正释放
- `MoveIntent{entity: 怪物B, destination: (10,20)}` —— 怪物想移动到某位置，但可能被控制技能阻止
- `LootIntent{player: 玩家A, item: 掉落物X}` —— 玩家想拾取，但可能被别人抢先

**框架要求**：Intent 是 think 最常产出的东西。它必须带明确的 target owner，因为"谁来裁决这个请求"决定了它会被路由到哪里。Intent 通常需要配套一个 Outcome（见第 6 条）来告知发起方结果。

### 3. Event / Fact（已发生的事实）

**含义**：已经发生、不可撤回的事实。它不再等待审批，只用于通知。

**游戏举例**：
- `EntityDied{entity: 怪物B, killer: 玩家A}` —— 怪物已经死了，这是事实
- `BuffExpired{entity: 玩家A, buff: 护盾}` —— buff 已经自然到期
- `ZoneEntered{entity: 玩家A, zone: 副本入口}` —— 玩家已经进入区域

**框架要求**：Fact 是不可变的，它不该被当成待审批的 command。Fact 的主要用途是进入 signal router 做 fan-out——比如"怪物死亡"这个事实需要通知任务系统、掉落系统、成就系统等多个订阅者。这就是 parallel.md 中 signal 机制的核心用途。

### 4. Delta / Contribution（增量贡献）——最重要的 effect 类别

**含义**：对目标状态的一份增量贡献，可以和其他同类贡献无序合并。

**游戏举例**：
- `DamageDelta{target: 怪物B, amount: -150}` —— 对怪物造成 150 点伤害（sum 语义，多个伤害可以直接加和）
- `HealDelta{target: 玩家A, amount: +80}` —— 治疗 80 点（sum 语义）
- `ThreatDelta{target: 怪物B, source: 玩家A, amount: +500}` —— 增加 500 仇恨（sum 语义）
- `AddTag{target: 玩家A, tag: "in_combat"}` —— 添加战斗标记（set-add 语义，多个人给你打战斗标记，结果一样）
- `RefreshBuff{target: 玩家A, buff: 点燃, duration: max(当前, 5s)}` —— 刷新 buff 持续时间（max 语义）

**框架要求**：**这是默认并行 lane 里最核心、最鼓励使用的 effect 类型。** 因为 delta 类 effect 天然满足 parallel.md 中"同一 round 同一 target 的 effect 视为无序集合"的要求。具体的代数类型见下文第二节。

### 5. Patch / Replacement（条件替换）

**含义**：对某个字段做替换或覆盖。它不像 delta 那样天然可以无序合并——两个人同时设置不同的移动目标，谁赢？

**游戏举例**：
- `SetMoveTarget{target: 怪物B, position: (10,20), guard: 怪物B未死亡}` —— 设置移动目标，但前提是怪物还活着
- `SetState{target: 玩家A, state: "stunned", guard: 当前状态 != "immune"}` —— 设置眩晕状态，但前提是没有免疫

**框架要求**：Patch 类 effect 必须带 guard（前提条件），否则在并行环境下语义不稳定。如果两个 effect 要 patch 同一字段且没有 guard，结果取决于执行顺序——这正是并行模型要避免的。建议限制为 `replace-if(predicate)` 形式。如果连 guard 都无法表达清楚，就应该进入 serial island。

### 6. Outcome（结果反馈）

**含义**：对之前某个 command/intent 的结果通知——成功、失败、部分成功、需要重试。

**游戏举例**：
- 玩家 A 发出 `CastIntent`，技能系统在 effect 阶段裁决后产出 `CastOutcome{success: true, cost_consumed: true}` —— 告诉玩家 A 的后续逻辑"技能释放成功了"
- 玩家 A 发出 `LootIntent`，掉落系统裁决后产出 `LootOutcome{success: false, reason: "already_taken"}` —— 告诉玩家 A "东西被别人拿了"

**框架要求**：只要存在"先申请、后确认"的玩法，就需要 Outcome。这对应 parallel.md 中的妥协 2——"成功语义必须锚定在单 owner 上"。如果"技能是否成功"需要等 target 的反馈才能决定，就形成了跨 owner 事务，需要用 reservation + outcome 协议来处理（而不是同步回调）。

### 7. Signal / Notification（通知信号）

**含义**：告诉订阅者"某类事情发生了"，驱动后续逻辑运行。Signal 是连接各轮 think 的纽带。

**游戏举例**：
- effect 提交了伤害后，产出 `DamageDealt{source, target, amount}` signal → 触发 target 身上的"受击反弹"buff 在下一轮 think
- 怪物死亡事实产出 `EntityDied` signal → 触发任务系统检查击杀计数、掉落系统生成掉落物、成就系统检查成就条件
- 玩家进入区域产出 `ZoneEntered` signal → 触发副本脚本开始计时

**框架要求**：Signal 进入 inbox，在下一轮（或下一 tick）被消费，而不是在产出的瞬间同步回调。这是 parallel.md 中"事件监听数据化"的核心机制，它保证了 owner 边界不被打穿。

### 8. Schedule / Timer（定时调度）

**含义**：把工作推迟到未来某个时间点。

**游戏举例**：
- `ScheduleThink{logic: 怪物B的AI, tick: current+3}` —— 怪物 B 的 AI 3 个 tick 后再次运行
- `ScheduleThink{logic: 玩家A的buff_护盾, tick: current+100}` —— 护盾 buff 100 tick 后过期检查
- `ScheduleThink{logic: 刷怪点X, tick: current+600}` —— 刷怪点 10 秒后刷新（假设 60 tick/s）

**框架要求**：Schedule 必须是框架的一等公民，不能用闭包或协程栈来悬挂。它应该数据化——存在 timer wheel 里，到期时进入 frontier，而不是某个 goroutine 在 sleep。这样才能支持回放、持久化、迁移。

### 9. Structural Change（结构变更）

**含义**：改变世界的拓扑结构，而不只是修改某个实体的数值。

**游戏举例**：
- `SpawnNpc{template: 骷髅兵, position: (10,20), owner: 刷怪点X}` —— 在世界中创建一个新实体
- `DespawnEntity{entity: 怪物B}` —— 从世界中移除一个实体
- `JoinGroup{entity: 玩家A, group: 队伍1}` —— 加入队伍（修改索引关系）
- `AddComponent{entity: 玩家A, component: 骑乘状态}` —— 给实体动态添加组件

**框架要求**：结构变更必须通过 world effect 统一提交，在 barrier 后才生效（类似 Unity ECS 的 `EntityCommandBuffer`）。不能在 think 阶段直接创建实体并立刻可见——否则别的 think 可能看到一个"创建了一半"的实体。

---

## 二、Effect 的代数性质——决定能否并行的关键

parallel.md 已经指出"同一 round、同一 target 的 effect 是无序集合"。但不是所有 effect 都能安全地无序处理——**能不能并行，取决于 effect 的代数性质，而不是它的业务含义。**

### 代数类型一览

| 代数类型 | 含义 | 游戏举例 | 能否无序合并 |
|---------|------|---------|------------|
| **sum** | 可加和 | 伤害 -150、治疗 +80、仇恨 +500 | 能。`a+b = b+a` |
| **max** | 取最大值 | 刷新 buff 持续时间取最长、取最高威胁值 | 能。`max(a,b) = max(b,a)` |
| **min** | 取最小值 | 减速取最慢速度、冷却取最短时间 | 能。`min(a,b) = min(b,a)` |
| **set-add** | 集合添加 | 添加 tag、添加 buff 到列表 | 能。集合并集满足交换律 |
| **set-remove** | 集合移除 | 移除 tag、移除 debuff | 能。但和 set-add 混合时需注意顺序 |
| **or / and** | 布尔运算 | 是否进入战斗状态（任意一个攻击就算）、是否满足所有条件 | 能 |
| **replace-if** | 带条件的替换 | "若未眩晕则设为眩晕"、"若未死亡则设置移动目标" | 有限制。guard 之间不能冲突 |
| **exclusive** | 互斥，同 round 最多一个生效 | 同时只能有一个控制技能生效 | 需要 tie-break 规则（如按优先级） |

### 核心原则

**如果一个 effect 的代数性质是 sum/max/min/set-add/or/and，它就可以安全地放在默认并行 lane 里。** 框架在 reduce 时不需要关心顺序，多个 effect 合并的结果是确定的。

**如果一个 effect 是 replace-if 或 exclusive，它需要额外约束**（guard 或 tie-break 规则），否则不能进入并行 lane。

**如果一个 effect 连上述分类都无法归入，它就不应该走默认并行路径**，而应该进入 serial island。

### 实际例子：为什么代数比 payload 重要

假设两个技能同时命中怪物 B：

- 火球：`DamageDelta{amount: -100}`（sum）
- 冰霜：`DamageDelta{amount: -80}`（sum）+ `SetState{state: "frozen", guard: not immune}`（replace-if）

伤害部分是 sum，无论先算火球还是冰霜，总伤害都是 -180，安全。

冰冻部分是 replace-if，它带了 guard "目标没有免疫"。如果同时有个"净化"effect 移除了免疫状态，那冰冻的 guard 检查结果取决于谁先执行——这就不安全了。解决方案：要么把净化和冰冻拆到不同 round，要么把它们归入 serial island。

---

## 三、Logic 的反应模式

当一个 logic 被激活（收到 signal、timer 到期、收到玩家输入），它的反应从数据层面看只有几种模式：

| 反应模式 | 含义 | 举例 |
|---------|------|------|
| **consume & transition** | 消费输入，推进状态机 | buff 收到"持续时间到期"signal，从 active 转为 expired |
| **accumulate / reduce** | 积累输入，产出聚合结果 | 伤害结算逻辑收集本轮所有伤害 delta，算出最终伤害值 |
| **forward / route** | 转发给别的 logic | 受击事件转发给反伤 buff、格挡判定逻辑 |
| **defer / reschedule** | 推迟到未来 | AI 决策后发现目标不在范围内，重新调度 3 tick 后再查 |
| **compensate / cancel** | 回滚或取消之前的请求 | reservation 超时未确认，自动释放冻结的资源 |
| **ignore / drop** | 忽略不相关的输入 | buff 已经过期，收到的后续 signal 直接丢弃 |
| **escalate to serial island** | 升级到串行域 | 检测到需要跨 owner 原子操作，进入 serial island 处理 |

关键不在于具体业务内容，而在于：这个反应**是否只改 private state**、**是否产出 owner-local 的 delta**、**是否需要跨 owner 协议**、**是否需要 future wakeup**。这四个问题决定了它能不能安全地并行。

---

## 四、World 应该是什么 Shape

parallel.md 已经把状态分成了 world state / entity public state / logic private state 三层。从并行模型的角度，world 整体应该呈现为**四个功能面**，而不是"一个可以到处读写的大对象"：

### 层 1：Snapshot / Query Surface（快照查询面）

**作用**：给 think 阶段提供只读的世界视图。

**包含**：
- 空间索引（"10 米内有哪些敌人"）
- 实体索引（"ID=123 的实体的公开状态"）
- 关系索引（"玩家 A 的队友有哪些"、"怪物 B 属于哪个阵营"）

**关键约束**：
- 完全只读
- 同一 round 内所有 think 共享同一份快照
- 不暴露任何写接口

**游戏举例**：AI think 阶段通过 `world.NearbyEnemies(pos, radius)` 查找附近敌人，这个查询在整个 round 内返回值一致，即使别的 think 已经"决定"杀死某个敌人——那个死亡要等 effect 提交后才会在下一轮的快照中体现。

### 层 2：Authoritative Stores（权威状态存储）

**作用**：保存 entity public state 和 world-level public state 的真实值。

**包含**：
- 每个实体的公开状态（HP、位置、朝向、buff 列表等）
- 世界级状态（刷怪点配置、全局事件标记、副本进度等）

**关键约束**：
- think 阶段不允许直接写入
- 只能通过 effect reducer 在 barrier 后提交
- 提交完成后生成新版本的 snapshot

### 层 3：Derived Indexes（派生索引）

**作用**：由 committed state 自动派生，barrier 后更新。

**包含**：
- 空间网格（位置变了 → 重建空间索引）
- 队伍/阵营成员关系（组队变了 → 更新成员索引）
- 可见性缓存（位置变了 → 更新谁能看到谁）

**关键约束**：
- 不能暴露中间脏状态
- 只在 barrier 后根据最新 committed state 重算
- think 阶段读到的永远是上一轮 barrier 后的版本

**游戏举例**：玩家从 (0,0) 移动到 (10,10)，在移动 effect 提交前，所有 think 查空间索引时仍然认为玩家在 (0,0)。只有下一轮的 think 才会看到玩家在 (10,10)。

### 层 4：Scheduling Surfaces（调度面）

**作用**：管理"什么 logic 什么时候该运行"。

**包含**：
- Active frontier（当前 round 要运行的 think 列表）
- Timer wheel（未来某 tick 要触发的定时器）
- Signal inboxes（每个 logic 的信号收件箱）
- World command buffer（待提交的 world effect 缓冲）

**关键约束**：
- think / effect 可以往里追加未来工作（发 signal、注册 timer）
- 但追加的工作不会在当前阶段同步执行——只会进入下一轮的 frontier

---

## 五、Logic 应该是什么 Shape

并行模型要求 logic 不能是"能抓全局对象图的脚本"，而应该是一个**有明确边界的小状态机**。

### Logic 的固定结构

```
Logic {
    id:             唯一标识（如 "player_123_skill_fireball"）
    owner_domain:   归属域（self / entity / world）
    private_state:  私有可变状态（只有自己能读写）
    inbox:          待消费的信号/事件批次
    subscriptions:  订阅了哪些类型的 signal
    meta:           元数据（预算、优先级、是否允许同 tick 重入等）
}
```

### Logic 的统一入口

每个 logic 的执行入口只有一个：

```
think(snapshot, inbox_batch) → outputs
```

其中 outputs 包含：
- private state 的变更
- entity effects（DamageDelta、AddBuff 等）
- world effects（SpawnNpc、DespawnEntity 等）
- signals（DamageDealt、EntityDied 等事实通知）
- schedules（未来 tick 的定时唤醒）
- outcomes（对之前某个 intent 的结果反馈）

### Logic 更像什么

更像：
- **Actor**：有自己的 mailbox（inbox），只处理自己收到的消息，不直接操作别人的状态
- **有限状态机（FSM）**：有明确的状态、明确的转移条件、有界的行为
- **Pregel vertex program**：读取当前 superstep 的消息，产出给下一 superstep 的消息

不像：
- 能随意访问全局变量的脚本
- 能同步调用别的 logic 的函数
- 能在运行过程中直接修改共享内存

### Logic 的声明式元数据

每个 logic 应该带一组元数据，用于调度器预算控制和静态检查：

| 元数据 | 含义 | 举例 |
|-------|------|------|
| `max_effects_per_activation` | 单次激活最多产出多少个 effect | 技能逻辑：10；AI 逻辑：5 |
| `max_signals_per_activation` | 单次激活最多产出多少个 signal | 伤害结算：3；副本脚本：20 |
| `max_reschedules_per_tick` | 同一 tick 内最多被重新调度几次 | buff 逻辑：2；AI 逻辑：1 |
| `allow_same_tick_reenter` | 是否允许同 tick 内多次进入 | 普通 buff：true；技能释放：false |
| `priority` | 调度优先级 | 玩家输入响应：高；环境刷新：低 |
| `serial_island` | 是否需要进入串行域 | 剧情脚本：true；普通战斗逻辑：false |

---

## 六、整体架构拼图

把上面的分析拼在一起，框架的整体 shape 是这样的：

```
┌─────────────────────────────────────────────────────────────┐
│                        Tick Pipeline                         │
│                                                              │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐               │
│  │ Frontier  │───▶│  Think   │───▶│  Output  │               │
│  │ (待执行   │    │ (并行读   │    │ (typed   │               │
│  │  logic   │    │  snapshot │    │  effect/ │               │
│  │  列表)    │    │  + 写     │    │  signal/ │               │
│  │          │    │  private) │    │  sched)  │               │
│  └──────────┘    └──────────┘    └────┬─────┘               │
│                                       │                      │
│                          ┌────────────┼────────────┐         │
│                          ▼            ▼            ▼         │
│                    ┌──────────┐ ┌──────────┐ ┌──────────┐   │
│                    │ Entity   │ │  World   │ │  Signal  │   │
│                    │ Effect   │ │  Effect  │ │  Router  │   │
│                    │ Reduce   │ │ (serial) │ │          │   │
│                    │ (并行,   │ │          │ │          │   │
│                    │ 按owner  │ │          │ │          │   │
│                    │ 分桶)    │ │          │ │          │   │
│                    └────┬─────┘ └────┬─────┘ └────┬─────┘   │
│                         │            │            │          │
│                         ▼            ▼            ▼          │
│                    ┌─────────────────────────────────────┐   │
│                    │           Barrier                    │   │
│                    │  提交新状态 → 更新 snapshot →         │   │
│                    │  更新派生索引 → 生成 next frontier    │   │
│                    └─────────────────────────────────────┘   │
│                         │                                    │
│                         ▼                                    │
│                  next frontier 非空？                         │
│                  是 → 回到 Think    否 → Tick 结束            │
└─────────────────────────────────────────────────────────────┘
```

这个架构本质上是在一个地方拼了五个理论模型的优点：

| 理论 | 提供了什么 | 在框架中体现为 |
|------|----------|--------------|
| **Actor Model** | owner-local mutation + mailbox 边界 | logic 只改自己的 private state，通过 inbox 接收消息 |
| **BSP / Pregel** | round / barrier / frontier 执行骨架 | 每轮 think → effect → barrier 的 superstep 循环 |
| **ECS** | query-friendly data layout + 延迟结构变更 | world 的 snapshot query 面 + EntityCommandBuffer 式的结构变更 |
| **Apache Beam** | per-key state/timer 模型 | logic 的 private state + timer wheel 调度 |
| **CRDT** | "什么更新能无序合并"的数学语言 | effect 的代数性质声明（sum/max/set-add 等） |

---

## 七、需要接受的妥协

parallel.md 已经列出了五条妥协，这里从理论角度解释**为什么这些妥协是必要的**，以及如何用上述分类来应对：

### 妥协 1：不支持跨 owner 的同 round 原子事务

**理论原因**：Actor 模型的核心假设就是每个 actor 只管自己的状态。要跨 actor 做原子操作，必须引入分布式事务协议，这会破坏并行性。

**应对**：把跨 owner 需求拆成 intent → outcome 协议。例如"交易"不是"同时扣 A 的金币加 B 的物品"，而是"A 发出 TradeIntent → 交易系统裁决 → 分别给 A 和 B 发 outcome"。

### 妥协 2：成功语义锚定在单 owner

**理论原因**：如果"成功"的定义依赖多个 owner 的状态，就形成了分布式一致性问题，没有简单解。

**应对**：用 command/intent + outcome 模式。"技能是否命中"由 target owner 裁决；source 在发出 intent 时就先扣资源（consume-on-cast），不依赖 target 反馈。

### 妥协 3：同 round 只能看到 barrier 前的世界

**理论原因**：BSP 模型的核心就是 superstep 内不可见其他计算的中间结果。这是并行 think 能安全运行的前提。

**应对**：接受 1 round 延迟。如果确实需要"A 的结果立刻影响 B 的决策"，拆成两轮——第一轮 A 产出 effect，barrier 后 B 在第二轮看到新状态。

### 妥协 4：同 tick 完成是尽力而为

**理论原因**：无限追加 next-round work 等价于无限循环风险。budget 机制是防止单 tick 膨胀的必要手段。

**应对**：logic 声明 `max_reschedules_per_tick`，超出 budget 的工作延迟到下一 tick。

### 妥协 5：极少数强顺序玩法进入 serial island

**理论原因**：有些逻辑天然不满足交换律（比如严格顺序的剧情演出），强行并行化只会引入 bug。

**应对**：serial island 作为显式的逃生门，logic 通过 `serial_island: true` 声明自己需要串行执行。这应该是**可见的、可统计的例外**，而不是默认路径。

---

## 八、可操作的结论

### 框架需要优先定义的四个核心类型

1. **EffectKind**：每个 effect 类型必须声明
   - target domain（self / entity / world）
   - algebra（sum / max / min / set-add / set-remove / replace-if / exclusive / serial）
   - delivery timing（same-round / next-round / future-tick）
   - 是否幂等
   - 是否需要 guard

2. **SignalKind**：每个 signal 类型必须声明
   - 语义角色（fact / notification / outcome / timer）
   - 路由范围（self / watchers / world / subscription-query）
   - payload schema
   - 因果关系（是哪个 effect 产出的）

3. **LogicMeta**：每个 logic 必须声明
   - owner domain
   - 订阅的 signal 类型
   - 预算限制（max effects、max signals、max reschedules）
   - 是否允许同 tick 重入
   - 是否需要 serial island

4. **WorldView**：快照查询接口必须声明
   - snapshot 版本号
   - 可用的查询 API（空间/实体/关系）
   - 不暴露任何写接口

### 设计 effect 时的检查清单

为每个新 effect 类型回答以下问题：

1. 它是请求（intent）还是已成事实（delta/fact）？
2. 它的目标 owner 是谁？
3. 它的代数类型是什么？（sum? max? replace-if?）
4. 同一 round 内多个实例无序合并，结果是否确定？
5. 它是否需要读取目标的当前值才能计算？（如果是，考虑加 guard）
6. 它是否需要配套 outcome？
7. 它是否可以幂等重试？

如果第 4 题的答案是"不确定"，这个 effect 就不能进入默认并行 lane。

### 下一步建议

把上述四个核心类型落实为 Go 接口草图，然后用 parallel.md 中建议的最小闭环（DamageEffect + AddBuffEffect + RemoveBuffEffect + ScheduleThink + EmitSignal + SpawnNpcEffect）来验证。

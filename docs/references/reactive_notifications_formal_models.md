# Formal Models for Reactive Notifications in Parallel Game Simulations

## Scope

本文档调研与本项目 Think/Apply 并行 tick 模型直接相关的形式化和半形式化理论模型。
焦点不在通用分布式系统，而是 **面向游戏/实时仿真的并行调度器中，如何正确、高效地管理 reactive notification（signal / effect / subscription）**。

与 `parallel_theory.md` 的区分：该文件聚焦数据分层与代数性质；本文件聚焦 **通知传播的时序模型、正确性论证和订阅机制的形式化处理**。

---

## 1. BSP Event Models and Inter-Process Notifications

### 1.1 BSP 形式化回顾

Valiant (1990) 定义的 BSP 模型把计算组织为 superstep 序列，每个 superstep 三个阶段：

1. **Local computation**：每个 processor 使用本地内存独立计算
2. **Global communication**：point-to-point 消息通过 routing network 发送
3. **Barrier synchronization**：所有 processor 完成后才进入下一个 superstep

形式化成本模型：

```
T_superstep = w + g·h + l

其中：
  w = max over all processors of local work (flops)
  h = max over all processors of messages sent or received (h-relation)
  g = communication cost per data word (gap parameter)
  l = barrier synchronization latency

T_total = Σ_i (w_i + g·h_i + l)
```

### 1.2 BSP 中 "消息" 的形式处理

BSP 对消息有一个关键的时序约束：

> **A message sent in superstep s is delivered to the destination processor
> at the beginning of superstep s+1.**

这不是实现细节，而是模型的核心语义保证。它意味着：

- **No mid-step visibility**：一个 processor 在 superstep 内看不到其他 processor 的中间状态
- **Deterministic delivery**：消息集合在 barrier 时刻被原子性地 "翻转" 为可见
- **No message ordering within a superstep**：同一 superstep 内到达同一 processor 的多条消息之间没有定义顺序

这三条性质直接映射到本项目的设计：

| BSP 概念 | 本项目对应 | 语义 |
|----------|-----------|------|
| Superstep | Think → Apply → barrier 一轮 | 计算-通信-同步的完整周期 |
| Local computation | Think Phase | 基于只读 snapshot 的并行计算 |
| Communication | Effect publish + Signal emit | 产出 typed effect 和 signal |
| Barrier | Superstep 结束时的 swap | signalRead ← signalWrite, 新一轮可见 |
| h-relation | blockCollector 的 per-thread per-block 写入 | 通信量的衡量 |

### 1.3 BSP 的扩展：Pregel 和 Agent-based Models

**Pregel (Malewicz et al., 2010)** 在 BSP 基础上增加了 vertex-centric 编程模型：

- 每个 vertex 是一个 logic unit（对应本项目的 Logic）
- vertex 在 superstep 中接收上一轮的消息、执行计算、发送新消息
- vertex 可以 "vote to halt"，在收到新消息时被重新激活

这个 "vote to halt + reactivation" 模式与本项目的 timer + signal frontier 机制高度同构：

```
Pregel:
  vertex.active = false (vote to halt)
  → receives message → vertex.active = true

本项目:
  Logic 无 pending signal/timer → 不进入 Think
  → 收到 signal 或 timer 到期 → 进入下一轮 Think 的 frontier
```

**Agent-based BSP 扩展 (Generalizing BSP, EPFL)**：

将 BSP processor 替换为 agent，每个 agent 有：
- 独立的 logical clock
- mailbox（缓冲 incoming messages）
- per-round 语义：round t 结束时 clock 递增到 t+1

Agent 模型允许 `send(B, m, k)` ——发送消息 m 给 agent B，scheduled 在 k ticks 后到达。
这与本项目 timer + signal 的组合能力对应。

### 1.4 对本项目的启示

1. **BSP 的消息语义天然支持 "同 round 不可见" 约束**——这正是本项目 `妥协 3` 的理论依据
2. **h-relation 参数可以作为自动串并行切换的理论依据**：当 h 很小（通信量低）时，barrier 开销相对高，串行更划算
3. **Pregel 的 vote-to-halt 证明了 "按需激活" 在 BSP 框架内是良定义的**——不需要每轮 tick 所有 Logic 都参与

---

## 2. Reactive Systems Formalization

### 2.1 Signal vs Effect in FRP

Functional Reactive Programming (Elliott & Hudak, ICFP 1997) 定义了两个核心抽象：

**Signal (Behavior)**：
```
Signal a = Time → a
```
Signal 是一个随时间变化的值。在任意时刻 t，`signal(t)` 返回当前值。
Signal 是连续的、始终存在的。

**Event**：
```
Event a = [(Time, a)]
```
Event 是一个离散的时间-值对序列。Event 只在特定时刻 "发生"。

在现代 reactive 框架（如 JavaScript Signals Proposal, TC39 2024）中，这演化为三层：

| 概念 | FRP 对应 | 语义 | 本项目对应 |
|------|----------|------|-----------|
| `Signal.State` | Behavior (source) | 可变的 reactive 值，是依赖图的根 | Logic private/public state |
| `Signal.Computed` | Behavior (derived) | 从其他 signal 自动派生的值，lazy + memoized | World derived indexes |
| `Effect` | Event handler | 当依赖变化时执行的副作用 | Apply phase 中的 effect reducer |

**关键区分**：

- **Signal** 是 "值的持续存在"——你可以在任何时候 query 它
- **Effect** 是 "状态转移的请求"——它在特定时刻发生，改变某个 signal 的值

本项目中的 signal（小写，指 Emit 产出的通知）更接近 FRP 的 Event 而非 Signal：
它是离散的、有时间戳的、在特定 round 到达 inbox 的消息。

本项目中的 effect 是 typed 的状态转移请求，语义上对应 FRP effect 的 "action" 部分，
但增加了 target owner 和代数元数据。

### 2.2 Pull vs Push in Concurrent Systems

并发系统中的信息流可以分为两种基本模式：

**Pull (Polling / Query)**：
- Consumer 主动请求数据
- 数据在请求时刻计算或返回
- Latency = polling interval / 2 (average)
- 适合：变化频率低、consumer 数量少、需要精确控制 timing

**Push (Notification)**：
- Producer 在数据变化时主动通知 consumer
- Consumer 被动接收
- Latency ≈ 0 (bounded by delivery mechanism)
- 适合：变化频率高、consumer 数量多、需要及时响应

**Push-Pull Hybrid (FRP 的核心贡献)**：

FRP 的 push-pull 模型（Reactive & Etage libraries on Hackage）：
- **Push half**：外部事件被 push 到系统，消费者立即得知
- **Pull half**：纯定义的流（如固定时间表上的事件列表）按需构造，类似 lazy list

本项目的 Think/Apply 模型是一个 **batch-push + snapshot-pull hybrid**：

```
Pull 部分：
  Think 阶段通过 WorldView 做 snapshot query
  → 这是 pull：Logic 主动查询 world state
  → snapshot 保证时点一致性

Push 部分：
  Signal/Effect 通过 Emit/Publish 发送到 target
  → 这是 push：源 Logic 主动推送到目标 Logic 的 inbox
  → 但实际交付延迟到 barrier 后（batch-push）

Batch-push 的形式化：
  在 superstep s 内：
    push(source, target, message) → pending_buffer[target].append(message)
  在 barrier：
    ∀ target: inbox[target] ← pending_buffer[target]
    clear(pending_buffer)
```

这种 batch-push 在形式上等价于 BSP 的 superstep 消息语义，
但比纯 push 多了一层 buffering，使得同一 round 内的所有 push 被原子化地交付。

### 2.3 Pub-Sub in Deterministic Simulations

标准 pub-sub 模型的核心定义：

```
Publisher：不知道 subscriber 身份，只向 topic/channel 发送消息
Subscriber：注册对 topic/channel 的兴趣，由 broker 投递
Broker：管理订阅关系，执行消息路由
```

**确定性仿真对 pub-sub 的特殊要求**：

标准 pub-sub 允许消息乱序、重复、丢失（at-most-once / at-least-once / exactly-once 是配置项）。
确定性仿真要求更强的约束：

1. **Deterministic delivery order**：同一组输入必须产生同一组输出。
   如果 pub-sub 的路由顺序依赖线程调度，仿真将不可重放。

2. **Snapshot-consistent subscriptions**：订阅列表的变更不应在 superstep 中途生效。
   否则同一 round 内，先执行的 publisher 看到旧订阅列表，后执行的看到新列表。

3. **Bounded fan-out**：无限 fan-out 会破坏 superstep 的 h-relation 成本模型。

本项目当前的设计选择（Logic 自己查订阅者列表并逐个 Emit）实际上回避了 broker 层的确定性问题，
代价是 fan-out 逻辑分散在业务代码中。

**形式化的确定性 pub-sub 方案**：

方案 A：Subscription snapshot（与 world snapshot 同步冻结）

```
在 barrier 时刻：
  subscription_snapshot ← current_subscriptions
  冻结到下一个 barrier

Think 阶段的 fan-out：
  targets = subscription_snapshot.query(topic)
  for target in sorted(targets):  // 排序保证确定性
    emit(target, signal)
```

方案 B：Deferred subscription（订阅变更作为 structural change 延迟到 barrier 后生效）

```
Think 阶段：
  subscribe(topic, self) → pending_sub_changes.append(Subscribe(topic, self))

Barrier 阶段：
  apply pending_sub_changes to subscription_store
  subscription_store 在下一个 superstep 可见
```

方案 B 与本项目的 structural change 处理方式一致（spawn/despawn 也是 barrier 后生效）。

---

## 3. Think/Apply Phase Decomposition: Formal Treatment

### 3.1 Two-Phase Commit 的游戏仿真适配

经典 Two-Phase Commit (2PC) in distributed databases：

```
Phase 1 (Prepare / Vote):
  Coordinator 询问所有 participants 是否可以提交
  Participants 回复 yes/no

Phase 2 (Commit / Abort):
  如果全部 yes → Coordinator 发送 commit
  如果任一 no → Coordinator 发送 abort
```

Think/Apply 不是 2PC 的直接实例，但有结构同构性：

| 2PC 概念 | Think/Apply 对应 | 差异 |
|----------|-----------------|------|
| Prepare | Think：计算意图，产出 effect | Think 不做 "是否可提交" 的投票，而是无条件产出 intent |
| Vote | 隐式：effect 的 typed algebra 保证无冲突 | 无需显式投票，因为 ownership 消除了写竞争 |
| Commit | Apply：effect 被 target owner 消费并提交 | Apply 可以拒绝 effect（作为 owner 的裁决权） |
| Abort | 不支持跨 owner 原子回滚 | 这是 `妥协 1` 的理论根源 |

**更准确的类比是 "Read-Compute-Write" 三阶段模型**（并行数据库中的 Aria protocol）：

Aria (DCC, 2020) 的 deterministic concurrency control：

```
Execution phase:
  所有 transaction 按确定顺序执行，但不直接提交
  write set 记录在 global write set 中

Commit phase:
  如果两个 transaction 访问同一行且至少一个是写：
    ID 更小的提交，更大的 abort

  通过后进入 commit
```

本项目的 Think/Apply 比 Aria 更简单，因为 ownership 消除了冲突检测的需要：

```
Think phase (= Aria execution):
  所有 Logic 并行执行，读 snapshot，写 effect buffer
  不存在 "两个 Logic 写同一行" 的情况（effect target 是 owner，owner 唯一）

Apply phase (= Aria commit):
  每个 owner 消费发给自己的 effect，串行执行
  不存在 abort（owner 自己决定如何 reduce）
```

### 3.2 正确性论证

**定理（非正式）：Think/Apply 两阶段执行在 ownership 约束下是无数据竞争的。**

证明思路：

```
定义：
  S = {L_1, L_2, ..., L_n} 为所有 Logic 实例
  W = world snapshot（只读，在 barrier 时刻冻结）
  PS(L_i) = Logic L_i 的 private state
  EB = effect buffer（append-only，per-thread per-block 隔离）

Think Phase 的读写集：
  L_i.Think 读取：W (只读), PS(L_i) (仅自己的 private state), inbox(L_i) (仅自己的 inbox)
  L_i.Think 写入：PS(L_i) (仅自己的 private state), EB[thread_i][block(target)] (per-thread 隔离)

对任意 i ≠ j：
  L_i.Think 的写集 ∩ L_j.Think 的读集 = ∅
  原因：
    1. PS(L_i) ∩ PS(L_j) = ∅（ownership 保证）
    2. W 是只读的
    3. EB[thread_i] ∩ EB[thread_j] = ∅（per-thread 隔离）
    4. inbox(L_i) ∩ inbox(L_j) = ∅（per-owner 隔离）

  ∴ Think Phase 无数据竞争 □

Apply Phase 的读写集：
  L_i.Apply 读取：W (只读), effects_for(L_i) (已聚合的 effect 集合)
  L_i.Apply 写入：public_state(L_i) (仅自己的公共状态), signal_buffer[thread_i]

对任意 i ≠ j：
  L_i.Apply 的写集 ∩ L_j.Apply 的读集 = ∅
  原因：
    1. public_state(L_i) ∩ public_state(L_j) = ∅（ownership 保证）
    2. effects_for(L_i) ∩ effects_for(L_j) = ∅（按 target 分组）
    3. signal_buffer 是 per-thread append-only

  ∴ Apply Phase 无数据竞争 □
```

**关键前提**：正确性完全依赖 ownership 的唯一性。如果允许一个 entity 被多个 Logic 写，证明立即失败。

这与 Core ECS (Kuper et al., 2025) 的结论一致：

> "A system that does not obtain entities from component values and has only a
> singleton query vector is deterministic under schedule conc(−), and two
> sub-schedules that write to disjoint sets of component labels are deterministic
> under schedule (−∥−)."

即：只要写集不相交，并行调度是确定性的。

### 3.3 形式化定义 "什么属于哪个阶段"

**Think 阶段的形式化边界**：

```
Think(L_i, W, inbox) → (PS'(L_i), effects[], signals[])

前置条件：
  W 是 frozen snapshot
  inbox 是上一轮 barrier 后交付的 signal 集合

后置条件：
  PS'(L_i) 是 L_i 的新 private state
  effects[] 中每个 effect 必须有 target_ref ≠ ∅
  signals[] 中每个 signal 必须有 target_ref ≠ ∅
  未修改 W
  未修改任何 PS(L_j) where j ≠ i
  未修改任何 public_state(L_k) for any k
```

**Apply 阶段的形式化边界**：

```
Apply(L_i, W, effects_for_i[]) → (public_state'(L_i), signals[])

前置条件：
  W 是 frozen snapshot（同一个 barrier 内与 Think 使用的是同一份）
  effects_for_i[] 是所有 target = L_i 的 effect 集合（无序）

后置条件：
  public_state'(L_i) 是 L_i 的新公共状态
  signals[] 中每个 signal 必须有 target_ref ≠ ∅
  未修改 W
  未修改任何 public_state(L_j) where j ≠ i
  effects_for_i[] 的处理顺序不影响最终结果（无序安全）
```

**什么属于 Think**：
- 需要读取 world snapshot 的查询（spatial query, entity lookup）
- 需要读取 private state 的决策（AI, behavior tree, cooldown check）
- 产出跨 owner intent（damage, heal, buff request）
- 产出 self-targeting effect（internal state sync）
- 产出 signal（notification to others）
- 设置 timer（future wakeup）

**什么属于 Apply**：
- 把收到的 effect 归约到 public state（HP -= total_damage, apply buff stack）
- 产出 fact signal（"I took damage", "I died"）
- 不应产出新的 effect（避免 Apply 退化为第二个 Think）

**什么不属于任何一个阶段（属于 Barrier）**：
- Snapshot 刷新
- Subscription store 更新
- Structural changes（spawn/despawn）
- Signal buffer swap
- Timer wheel advance

---

## 4. Event Commutativity and Ordering

### 4.1 形式化定义

**定义（Effect Commutativity）**：

```
两个 effect e_1, e_2 在状态 s 上 commute，当且仅当：

  apply(apply(s, e_1), e_2) = apply(apply(s, e_2), e_1)

即：无论先应用 e_1 还是 e_2，最终状态相同。
```

**定义（Effect Associativity）**：

```
三个 effect e_1, e_2, e_3 的归约是 associative，当且仅当：

  apply(apply(apply(s, e_1), e_2), e_3) = apply(apply(s, apply_batch(e_1, e_2)), e_3)

即：分组方式不影响最终结果。
```

**定义（Effect Idempotency）**：

```
Effect e 是 idempotent，当且仅当：

  apply(apply(s, e), e) = apply(s, e)

即：重复应用不改变结果。
```

### 4.2 游戏 Effect 的交换性分析

| Effect 类型 | 交换性 | 结合性 | 幂等性 | 分析 |
|-------------|--------|--------|--------|------|
| Damage (sum) | ✅ | ✅ | ❌ | `HP -= 10; HP -= 20` = `HP -= 20; HP -= 10`。但 `HP -= 10; HP -= 10` ≠ `HP -= 10` |
| Heal (sum, clamped) | ✅ | ✅ | ❌ | `HP += 10; HP += 20` = `HP += 20; HP += 10`。Clamp 到 max HP 不影响交换性 |
| Buff duration (max) | ✅ | ✅ | ✅ | `duration = max(duration, 5); max(duration, 3)` 交换、幂等 |
| Buff stack count (sum) | ✅ | ✅ | ❌ | 同 damage 分析 |
| Buff stack count (max) | ✅ | ✅ | ✅ | 同 duration 分析 |
| Set flag (or) | ✅ | ✅ | ✅ | `is_stunned |= true; is_stunned |= true` 交换、幂等 |
| Clear flag (and) | ✅ | ✅ | ✅ | 同 set flag |
| Position replace | ❌ | ❌ | ✅ | `pos = A; pos = B` ≠ `pos = B; pos = A`。需要 guard 或 priority |
| Inventory add (set-add) | ✅ | ✅ | ✅ | `items ∪= {sword}; items ∪= {shield}` 交换、幂等 |
| Inventory remove (set-remove) | ❌* | - | ✅ | 与 add 不交换：`add X; remove X` ≠ `remove X; add X` |
| Conditional replace | ❌ | ❌ | 看 guard | `replace-if(HP > 50, ...)` 依赖当前值，不交换 |

**不交换的 effect 处理策略**：

1. **改代数**：把 `position replace` 改为 `position delta (sum)`，交换性恢复
2. **加 guard**：`replace-if(version == expected)` 把冲突变成可检测的
3. **拆 round**：第一轮申请，第二轮确认（reservation protocol）
4. **进 serial island**：强制串行执行

### 4.3 CRDT 作为游戏状态模型

**State-based CRDT (CvRDT)**：

```
定义：状态空间 S 上的偏序 ≤ 构成 join-semilattice。
merge(s_1, s_2) = s_1 ⊔ s_2 （least upper bound）

性质：
  Commutativity: s_1 ⊔ s_2 = s_2 ⊔ s_1
  Associativity: (s_1 ⊔ s_2) ⊔ s_3 = s_1 ⊔ (s_2 ⊔ s_3)
  Idempotence:   s_1 ⊔ s_1 = s_1

→ 给定相同的状态集合 {s_1, ..., s_n}，任何 merge 顺序都收敛到同一结果。
```

**Operation-based CRDT (CmRDT)**：

```
定义：每个 update 操作分为两步：
  prepare(op, local_state) → message
  effect(message, state) → state'

要求：对并发操作，effect 必须交换：
  effect(m_1, effect(m_2, s)) = effect(m_2, effect(m_1, s))
```

**CRDT 与 Think/Apply 的映射**：

| CRDT 概念 | Think/Apply 对应 |
|-----------|-----------------|
| Replica | Logic（每个 Logic 是一个 "replica" of game state it owns） |
| Update operation | Think 产出的 effect |
| prepare phase | Think：根据 snapshot 计算 intent，产出 typed effect |
| effect phase | Apply：把 effect 应用到 owner 的 public state |
| Merge | Apply 中的 reduce（对同一 owner 的多个 effect 进行归约） |
| Commutativity requirement | 本项目的 "effect 无序安全" 约束 |

**关键差异**：

CRDT 设计用于 **最终一致性**——允许 replica 在任意时刻状态不同，只要最终收敛。
本项目要求 **每 tick 结束时状态一致**——所有 effect 在同一 barrier 内被处理完毕。

这意味着本项目实际上比 CRDT 更强：

```
CRDT:  ∀ permutation π of effects: final_state is eventually the same
本项目: ∀ permutation π of effects: final_state is immediately the same (within same Apply)
```

但交换性要求是相同的。CRDT 的代数工具（semilattice, commutative monoid）可以直接复用。

### 4.4 可交换 Effect 的代数结构

总结适用于游戏的标准代数结构：

**Commutative Monoid (sum/count)**：
```
(S, ⊕, 0) where a ⊕ b = b ⊕ a and (a ⊕ b) ⊕ c = a ⊕ (b ⊕ c)
示例：damage sum, heal sum, stack count, resource delta
```

**Join-Semilattice (max/min/set-union)**：
```
(S, ⊔) where ⊔ is commutative, associative, idempotent
示例：buff duration (max), debuff floor (min), known set (union)
```

**Boolean Algebra (flags)**：
```
(Bool, ∨, ∧, ¬, true, false)
示例：is_stunned, is_visible, has_been_hit
```

**Last-Writer-Wins Register (LWW-Register)**：
```
state = (value, timestamp)
merge(a, b) = argmax(a.ts, b.ts)  // 注意：需要总序，不适合纯并行
```

LWW-Register 在并行仿真中有问题：如果两个 effect 有相同时间戳，需要额外的 tiebreaker。
本项目应避免使用 LWW-Register 作为默认代数，而是优先使用 commutative monoid 或 semilattice。

---

## 5. Subscription/Watch Mechanisms in Parallel Contexts

### 5.1 问题定义

在并行 tick 模型中，"订阅" 面临的核心挑战：

```
Logic A 在 Think 阶段订阅了 "entity B 的 HP 变化事件"。
问：这个订阅在当前 round 内生效吗？

如果立即生效：
  - 本 round 内其他 Logic 对 B 造成的 damage 会在 Apply 阶段产出 signal
  - 但 A 是在 Think 阶段注册的订阅，Apply 阶段是否应该查看新的订阅列表？
  - 如果是，同一 round 内不同 Logic 的 Think 执行顺序会影响 A 是否收到 signal
  → 破坏确定性

如果延迟到下一 round：
  - 确定性保证
  - 但需要额外一轮延迟
```

### 5.2 Frozen vs Live Subscriptions

**定义（Frozen Subscription）**：

```
subscription_snapshot 在 barrier 时刻冻结。
整个 superstep 内，所有 fan-out 查询使用同一份 frozen subscription_snapshot。
订阅变更（subscribe/unsubscribe）作为 structural change 延迟到下一个 barrier。
```

**定义（Live Subscription）**：

```
subscription 在 Think/Apply 过程中实时更新。
fan-out 查询使用最新的 subscription 列表。
```

**正确性分析**：

| 属性 | Frozen | Live |
|------|--------|------|
| 确定性 | ✅ 保证 | ❌ 依赖执行顺序 |
| 延迟 | 1 round | 0 round |
| 实现复杂度 | 低（与 world snapshot 同步） | 高（需要并发安全的 subscription store） |
| 适合场景 | 默认 | 串行域 |

**推荐策略**：Frozen subscription 作为默认模式，与 BSP 语义一致。

### 5.3 动态订阅的形式化处理

**Subscription 作为 World State 的一部分**：

```
World state W 包含：
  entities: Map[Ref, EntityState]
  subscriptions: Map[Topic, Set[Ref]]  // 谁订阅了什么
  ...

subscription 变更是 structural change：
  Subscribe(topic, ref) → pending_structural_changes
  Unsubscribe(topic, ref) → pending_structural_changes

在 barrier：
  apply(W.subscriptions, pending_structural_changes) → W'.subscriptions
```

**Subscription Query in Think**：

```
Think 阶段需要 fan-out 时：
  targets = W.subscriptions[topic]  // 使用 frozen snapshot
  for target in deterministic_order(targets):
    emit(target, signal)
```

`deterministic_order` 必须是确定性的（如按 ref 值排序），否则不同线程上的 emit 顺序可能不同。
（在当前实现中，emit 是 append-only 到 per-thread buffer，最终由 barrier 交付，顺序不影响语义——
但如果 future 版本中 signal 有优先级或顺序敏感的语义，排序就变得必要。）

### 5.4 Subscription Consistency Models

**强一致性（Serializable）**：
所有操作（subscribe, unsubscribe, query_subscribers, emit）可以排列成一个合法的串行执行序列。
→ 对并行 tick 模型来说太强，会导致大量同步。

**Snapshot 一致性（适合本项目）**：
每个 superstep 看到的订阅列表是某个确定时刻（上一个 barrier）的快照。
superstep 内的订阅变更在下一个 barrier 生效。
→ 等价于 BSP 的消息可见性语义。

**最终一致性（适合跨服/分布式场景）**：
订阅变更最终会传播到所有节点，但不保证 "何时"。
→ 不适合单服确定性仿真，但适合跨服事件总线。

### 5.5 Subscription 元数据的并行安全

如果 subscription store 本身是一个 CRDT：

```
SubscriptionSet = GSet[Ref]  // Grow-only set: 只能添加订阅者，永远不会被自动删除

问题：如何支持 unsubscribe？

方案 1：OR-Set CRDT
  add(ref) 和 remove(ref) 并发时，add wins
  适合 "偏向保持订阅" 的语义

方案 2：2P-Set (Two-Phase Set)
  add_set: GSet, remove_set: GSet
  element ∈ result ⟺ element ∈ add_set ∧ element ∉ remove_set
  一旦 unsubscribe，永远不能再 subscribe
  → 不适合游戏（玩家可能反复 subscribe/unsubscribe）

方案 3：LWW-Element-Set
  每个 element 有 (add_timestamp, remove_timestamp)
  add wins if add_ts > remove_ts
  → 需要可靠的时间戳（在单服确定性仿真中 = round number）
```

**本项目推荐方案**：不需要 CRDT。单服环境下，subscription store 在 barrier 时刻被串行更新，
不存在并发冲突。CRDT 的价值在跨服场景下才体现。

---

## 6. Parallel Discrete Event Simulation (PDES) 的经验

### 6.1 Conservative vs Optimistic Synchronization

PDES 领域的两种经典并行策略：

**Conservative (Chandy-Misra-Bryant, 1977-1979)**：
- 只有当确认不会收到更早的消息时，才处理当前最早的消息
- 无需回滚，但可能导致死锁（需要 null messages 或 lookahead）
- 保证因果正确性

**Optimistic (Time Warp, Jefferson 1985)**：
- 乐观地并行执行，如果因果错误则回滚（rollback + anti-messages）
- 需要 Global Virtual Time (GVT) 计算来回收状态
- 高并行度，但回滚开销大

**本项目选择的是 conservative BSP 风格**：
- barrier 充当 "确认点"——在 barrier 之前所有消息已知
- 无需回滚（Think 只读 snapshot，Apply 只写自己）
- 无需 GVT 计算

### 6.2 PDES 在游戏中的经验教训

来自 Parallel Discrete Event Simulation: The Making of a Field (WSC 2017)：

> "Game Entities are very much LPs. They interact with nearby Entities,
> and need to be mapped to processors that contain others they tightly
> communicate with. Migration is a big deal and message routing is a challenge."

> "One unique challenge is that game developers think in time steps,
> because they need to render a graphics frame with up to date positions
> and states of Entities. But it is more efficient to model things using
> discrete events."

关键经验：

1. **Interest Management (IM)**：DIS/HLA 仿真中使用 pub-sub + 地理网格过滤，只把消息发给 "感兴趣" 的实体。
   这正是 subscription + spatial filtering 的军事仿真实践。

2. **Time Warp 在实时游戏中很少适用**：因为游戏有 frame deadline，不能无限回滚。
   BSP/barrier-based 的固定步长更适合游戏。

3. **Deterministic ordering of messages is critical**：
   > "Explicit, deterministic ordering to message timestamps was a later
   > important addition."

---

## 7. Core ECS: Formal Concurrency Analysis

### 7.1 Core ECS (Kuper et al., 2025)

这是目前唯一已知的 ECS 模式形式化工作，与本项目高度相关。

**核心贡献**：

1. 定义了 Core ECS 核心演算（denotational semantics）
2. 定义了四种调度模式：
   - `seq(−)`：顺序执行
   - `−o9−`：顺序组合
   - `conc(−)`：并行执行同一 schedule 中的所有 system
   - `−∥−`：并行执行两个独立的 sub-schedule
3. 证明了确定性并行的充分条件

**关键定理**：

> **Schedule Safety implies Schedule Determinism.**
>
> A schedule z is safe at state c if for all concurrent mutations m_1, m_2
> in z, infl_c(m_1) ∩ infl_c(m_2) = ∅ (influence sets are disjoint).
>
> If z is safe at c, then z is deterministic at c.

翻译成本项目语言：

```
如果 Think(L_i) 和 Think(L_j) 的 "影响集" 不相交（它们写的状态不重叠），
那么并行执行它们是确定性的。

影响集 = {(entity, component) | Think 会写入的 (entity, component) 对}

在本项目中，ownership 保证：
  影响集(Think(L_i)) = {(L_i, private_state)} ∪ {effect_buffer[thread_i]}
  影响集(Think(L_j)) = {(L_j, private_state)} ∪ {effect_buffer[thread_j]}

  当 i ≠ j 时：
  影响集(Think(L_i)) ∩ 影响集(Think(L_j)) = ∅
```

### 7.2 The Essence of ECS (SAC 2026)

**补充贡献**：

- 引入 archetype-based 内存模型的形式化
- 定义了 "wave" 概念：scheduler 将 system 组织为 wave 序列，wave 内并行，wave 间串行
- 引入 type-directed conflict detection：编译时通过类型系统检测读写冲突

```
Wave 与本项目 superstep 的关系：

ECS wave:
  [System_A, System_B] → sync() → [System_C, System_D] → sync()
  wave 内并行，wave 间 barrier

本项目 superstep:
  Think(all active Logics) → barrier → Apply(all targets) → barrier → next round
  superstep 内 Think 并行，barrier 后 Apply 并行
```

---

## 8. 综合：形式模型到 Think/Apply 调度器的映射

### 8.1 模型对照表

| 理论模型 | 核心贡献 | 本项目采用的部分 | 本项目不采用的部分 |
|----------|---------|-----------------|-------------------|
| **BSP** (Valiant 1990) | superstep + barrier + h-relation | superstep 结构、barrier 语义、消息延迟到下一步可见 | g/l 成本参数（未用于自动调优） |
| **Pregel** (Google 2010) | vertex-centric BSP + vote-to-halt | Logic = vertex、按需激活、消息到 inbox | combiner（框架级 effect 预合并，保留为 future） |
| **FRP** (Elliott 1997) | Signal + Event + push-pull | Signal 作为通知、snapshot query 作为 pull | 连续时间语义（游戏是离散 tick） |
| **CRDT** (Shapiro 2011) | 交换性 + semilattice → 无冲突收敛 | effect 代数性质分析、无序安全约束 | 最终一致性（本项目要求 per-barrier 一致） |
| **Core ECS** (Kuper 2025) | schedule safety → determinism | ownership-based 无竞争证明 | query vector 形式化（本项目不使用 archetype ECS） |
| **PDES Conservative** | 因果安全的并行事件处理 | barrier 作为确认点、无需回滚 | null messages、lookahead（本项目不需要） |
| **PDES Optimistic** | 乐观并行 + 回滚 | 不采用 | Time Warp、rollback、GVT 计算 |
| **2PC** | prepare-vote-commit 协议 | Think=prepare, Apply=commit 的结构同构 | 分布式 abort（本项目不支持跨 owner 回滚） |
| **Aria DCC** | 确定性并发控制 | 确定性排序、execution→commit 两阶段 | conflict detection + abort（ownership 消除了冲突） |
| **HLA/DIS IM** | Interest Management pub-sub | subscription + spatial filtering 理念 | 完整 HLA 协议栈 |

### 8.2 本项目的理论定位

用一句话概括本项目模型的理论身份：

> **一个 ownership-partitioned、BSP-structured、CRDT-constrained 的
> deterministic parallel game tick scheduler，
> 其中 Logic 是 actor/vertex，Effect 是 commutative message，
> Signal 是 deferred notification，Barrier 是 superstep boundary。**

### 8.3 理论给出的 design checklist

以下是从形式化模型直接推导的设计检查项：

**从 BSP 推导**：
- [ ] 每个 superstep 的消息只在下一个 barrier 后可见
- [ ] Think 不应看到其他 Logic 的中间写入
- [ ] barrier 开销应与通信量 h 成正比（用于自动串并行切换）

**从 CRDT 推导**：
- [ ] 每种 effect 应声明代数性质（commutative? associative? idempotent?）
- [ ] 不交换的 effect 必须有显式的冲突处理策略
- [ ] 测试时可以 shuffle effect 顺序验证交换性

**从 Core ECS 推导**：
- [ ] 并行 schedule 的正确性依赖 "影响集不相交"
- [ ] ownership 是保证影响集不相交的最简单机制
- [ ] 如果引入共享可写状态，必须重新证明 safety

**从 PDES 推导**：
- [ ] 固定步长 barrier 比乐观并行更适合实时游戏
- [ ] Interest Management 可以大幅减少 h-relation

**从 FRP/Subscription 推导**：
- [ ] Subscription 变更应作为 structural change 延迟到 barrier 后生效
- [ ] fan-out 查询应基于 frozen subscription snapshot
- [ ] 显式 Emit 比隐式 pub-sub 更容易保证确定性

---

## 9. Open Research Questions

### 9.1 Effect Algebra Verification

**问题**：如何在编译时或测试时自动验证 effect 的交换性？

**可能方向**：
- Property-based testing：对每种 effect 类型，随机生成两个 effect，验证 `apply(s, e1, e2) == apply(s, e2, e1)`
- 类型系统标注：effect 声明 `algebra: Commutative` 时，编译时检查 Apply 实现是否满足
- 静态分析：分析 Apply 函数的读写模式，推断交换性

### 9.2 Optimal Superstep Granularity

**问题**：MaxSupersteps 设为多少最优？太少导致 signal 延迟到下一 tick，太多导致 barrier 开销。

**可能方向**：
- BSP cost model：`T = Σ (w_i + g·h_i + l)`，最小化 T 对 superstep 数量的导数
- 运行时自适应：根据过去 N tick 的 h_i 和 w_i 自动调整

### 9.3 Subscription as First-Class Framework Primitive

**问题**：当前 Logic 自己管理订阅者列表并逐个 Emit。是否应该把 subscription 提升为框架原语？

**利弊**：
- 利：减少样板代码、框架可做 fan-out 优化、subscription snapshot 自动管理
- 弊：增加框架复杂度、subscription 语义需要标准化、可能限制业务灵活性

**建议**：暂不提升，但保留 subscription store 的接口扩展点（对应 `parallel.md` 中的 "未来可选"）。

---

## References

### 直接引用

1. Valiant, L.G. (1990). "A Bridging Model for Parallel Computation." Communications of the ACM, 33(8).
2. Malewicz, G. et al. (2010). "Pregel: A System for Large-Scale Graph Processing." SIGMOD.
3. Elliott, C. & Hudak, P. (1997). "Functional Reactive Animation." ICFP.
4. Shapiro, M. et al. (2011). "Conflict-free Replicated Data Types." SSS.
5. Shapiro, M. et al. (2011). "A Comprehensive Study of Convergent and Commutative Replicated Data Types." INRIA TR 7506.
6. Kuper, L. et al. (2025). "Exploring the Theory and Practice of Concurrency in the Entity-Component-System Pattern." arXiv:2508.15264.
7. Boyang Chen et al. (2026). "The Essence of Entity Component System." SAC.
8. Jefferson, D.R. (1985). "Virtual Time." ACM TOPLAS, 7(3).
9. Chandy, K.M. & Misra, J. (1979). "Distributed Simulation: A Case Study in Design and Verification of Distributed Programs." IEEE TSE.
10. Fujimoto, R.M. (1990). "Parallel Discrete Event Simulation." Communications of the ACM, 33(10).
11. Bryant, R.E., Chandy, K.M., Jefferson, D.R. et al. (2017). "Parallel Discrete Event Simulation: The Making of a Field." WSC.
12. TC39 Signals Proposal (2024). https://github.com/tc39/proposal-signals

### 补充参考

13. Lamport, L. (1978). "Time, Clocks, and the Ordering of Events in a Distributed System." Communications of the ACM.
14. Hewitt, C. (2010). "Actor Model of Computation: Scalable Robust Information Systems." arXiv:1008.1459.
15. Almeida, P.S. et al. (2023). "Approaches to Conflict-free Replicated Data Types." arXiv:2310.18220.
16. Generalizing Bulk-Synchronous Parallel Processing for Data Science. EPFL Technical Report.
17. Ruoyu Sun (2019). "Game Networking Demystified, Part II: Deterministic." Blog post.
18. Glenn Fiedler. "Floating Point Determinism." Gaffer On Games.
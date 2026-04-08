# GAS (Gameplay Ability System) 调研总结

> 基于 Unreal Engine GAS 及其他方案的调研，聚焦于与我们 Scheduler 框架的对接分析。
> 调研原始数据见 `docs/tmp/research_*.md`。

Last Updated: 2025-07

---

## 1. 调研目标

我们已有一个并行 tick Scheduler（`sched/`），以及一个早期的参考 GAS 实现（外部 `gas/` 仓库）。本次调研的目标不是全面翻译 Unreal GAS，而是回答一个核心问题：

> **Unreal GAS（及其他成熟方案）中，有哪些概念维度是参考 GAS 缺失的，且这些缺失的概念会影响 GAS ↔ Scheduler 协议层设计？**

---

## 2. Scheduler 关键约束回顾

| 约束 | 说明 |
|------|------|
| Think 只读 | Logic.Think 只能读取 WorldView（快照），不能修改任何 public state |
| Apply 自裁决 | Logic.Apply 只能修改自身 public state，接收来自其他 Logic 的 Effect |
| Typed Effect | 所有跨 owner 写入必须是显式的 typed 数据结构（非 closure） |
| Signal 触发 Think | 跨 owner 事件通知通过 Signal 投递，触发 target 的 Think |
| WatchState 过滤 | Logic 必须显式声明 Signal 兴趣，否则不接收任何 Signal |
| Timer Wheel | 支持定时唤醒 Logic.Think，delay > 0 时注册 |
| BSP 一致性 | 并发模式下 Think 阶段 public state 静态，Apply 更新后下一轮可见 |
| Effect 顺序无关 | 多个 Effect 同时到达同一 target，处理结果应对顺序不敏感（容忍性） |
| Logic = Owner | 调度单位是 Logic，内部子逻辑组合是实现私有事务 |

---

## 3. Unreal GAS 核心概念

### 3.1 概念关系图

```
                  ┌──────────────────────────────────────────────┐
                  │        AbilitySystemComponent (ASC)          │
                  │                                              │
                  │  ┌────────────┐  ┌────────────┐  ┌────────┐  │
                  │  │ Abilities  │  │ Attributes │  │  Tags  │  │
                  │  │ (GA list)  │  │ (AttrSet)  │  │(Owned) │  │
                  │  └──────┬─────┘  └─────▲──────┘  └──▲─────┘  │
                  │         │              │             │       │
                  │    cost/│         modifies      grant/revoke │
                  │  applies│              │             │       │
                  │         ▼              │             │       │
                  │  ┌─────────────────────┴─────────────┴─────┐ │
                  │  │     GameplayEffect (GE)                 │ │
                  │  │      - Instant / Duration / Infinite    │ │
                  │  │      - Modifiers (Add/Mul/Div/Override) │ │
                  │  │      - Stacking / Period / Conditions   │ │
                  │  └─────────────────────────────────────────┘ │
                  │                                              │
                  │  ┌──────────────┐  ┌──────────────────────┐  │
                  │  │ ActiveGEs    │  │ GameplayCue (client) │  │
                  │  │ (tracking)   │  │ VFX / SFX / UI       │  │
                  │  └──────────────┘  └──────────────────────┘  │
                  └──────────────────────────────────────────────┘
```

### 3.2 概念通用性分级

| 概念 | 通用性 | 我们是否需要 |
|------|--------|-------------|
| Attributes（属性） | ★★★ 通用必需 | 是 |
| GameplayTags（层级标签） | ★★★ 通用必需 | 已有 `tag/` 包 |
| GameplayEffect（效果） | ★★★ 通用必需 | 是，对应 typed Effect |
| GameplayAbility（技能） | ★★★ 通用必需 | 是 |
| Modifier 聚合 | ★★☆ 高度推荐 | 是 |
| Stacking 策略 | ★★☆ 高度推荐 | 是 |
| Cost / Cooldown | ★★☆ 高度推荐 | 是 |
| TargetData（目标数据） | ★★☆ 高度推荐 | 概念需要，形式不同 |
| Meta Attributes | ★★☆ 设计模式 | 推荐 |
| ExecutionCalculation | ★★★ 通用必需 | 对应 Apply 端计算 |
| EffectContext | ★★☆ 概念通用 | 对应 Effect 携带的上下文 |
| AbilityTask | ★☆☆ Unreal 特色 | 不需要（用 Running 替代） |
| GameplayCue | ★☆☆ 客户端专用 | 不需要 Cue 子系统 |
| Prediction / Rollback | ★☆☆ Unreal 特有 | 不需要 |
| ASC Replication Mode | ★☆☆ Unreal 特有 | 不需要 |

---

## 4. 关键概念详解与 Scheduler 映射

### 4.1 属性系统 (Attributes)

**Unreal 做法**：
- **Base Value**：被 Instant GE 永久修改的累积值
- **Current Value**：Base + 所有活跃 Duration/Infinite Modifier 整体重算的派生值
- Buff 的"可撤销性"通过 Base/Current 分离自动实现——移除 GE 时 Modifier 消失，Current 自动回退
- 使用 Aggregator 管理每个属性的 Modifier 列表，Dirty 标记驱动惰性重算

**Modifier 聚合公式**：

```
CurrentValue = ((BaseValue + Σ Additive) × Π Multiplicative) / Π Division
```

- 固定顺序：Override → Add → Multiply → Divide
- **同类操作加法聚合**：两个 +50% = +100%，不是 ×1.5×1.5
- 可选多 Channel 实现跨层乘法聚合

**Scheduler 映射**：

| 属性类别 | 建议归属 | 理由 |
|---------|---------|------|
| Base Value | Owner Private State | 只有 owner 的 Apply 才修改 |
| Modifier 列表 | Owner Private State | Buff 增删是 Apply 内部事务 |
| Current Value | Public State（快照） | 其他 Logic Think 需要读取（AI 决策、技能判断） |
| Meta Attribute（如 Damage） | Owner Private State | 临时计算中间量，外部不需要 |

**关键洞察**：Aggregator 模型天然满足"Effect 顺序无关"——Current 是从 Base + 全量 Modifiers 整体重算的，不依赖 Effect 到达顺序。这比我们要求的"容忍性"更强，是**结构性保证的交换性**。

**与参考 GAS 的差距**：参考 GAS 的 Buff 系统没有 Base/Current 分离，没有 Aggregator，Modifier 与属性值的关系不够系统化。

---

### 4.2 GameplayEffect 持续类型

**Unreal 做法**：

| 类型 | 行为 | 修改目标 | 可撤销 |
|------|------|---------|--------|
| Instant | 立即执行并消失 | Base Value | 否 |
| Duration | 持续一段时间后自动移除 | Current Value（via Modifier） | 是 |
| Infinite | 永久直到手动移除 | Current Value（via Modifier） | 是 |

**Periodic Effects**：Duration/Infinite GE 可附加 Period，每隔固定时间执行一次 Instant 语义操作。典型场景：DOT、HOT。

**Scheduler 映射**：

| UE 概念 | 我们的对应 | 驱动机制 |
|---------|-----------|---------|
| Instant GE | typed Effect | Think 产出 → Apply 消费 |
| Duration GE（简单 buff） | Apply 端 BuffList + Timer Wheel | Timer 到期唤醒 → 清理 |
| Duration GE（复杂效果） | Running（独立逻辑实体） | Timer Wheel 周期唤醒 |
| Infinite GE | BuffList（Expire=0 表示永久） | 手动 Remove |
| Periodic GE | Running + Timer Wheel | 周期唤醒产出 Effect |

**设计建议**：用 `Expire` 字段（0=Infinite）统一 Duration 和 Infinite，减少概念数量。

---

### 4.3 Stacking 模型

**Unreal 做法**：

| 维度 | 选项 |
|------|------|
| Stacking Type | None（独立实例）/ AggregateBySource / AggregateByTarget |
| Stack Limit | 最大堆叠层数 |
| Duration Refresh | 刷新 / 不刷新 |
| Period Reset | 重置周期 / 不重置 |
| Expiration | 清除所有层 / 移除一层 / 刷新持续时间 |
| Overflow | 拒绝 / 触发额外 Effect |

**与参考 GAS 的对比**：

| 参考 GAS | Unreal GAS | 差距 |
|----------|-----------|------|
| BuffStackGreater | — | Unreal 没有直接对应，可视为 AggregateBySource + 取最大值 |
| BuffStackLonger | Duration Refresh=true | 概念接近 |
| BuffStackSeparate | None（独立实例） | 概念一致 |
| 缺失 | AggregateByTarget | 多来源共享堆叠计数 |
| 缺失 | Stack Limit | 堆叠上限 |
| 缺失 | Overflow 处理 | 上限溢出触发额外效果 |
| 缺失 | Expiration Policy | 到期时对层数的处理 |

**Scheduler 映射**：Stacking 完全在 Apply 端处理，不需要 Think 参与。Apply 接收 BuffEffect → 查找 BuffList → 按策略处理堆叠 → 更新 Modifier → 重算 Current。

---

### 4.4 能力激活与 GameplayTags

**Unreal 做法**：

GameplayAbility 激活时的检查链：
1. `CanActivateAbility` — Tag 条件 + 通用检查
2. `CheckCost` — 资源是否足够
3. `CheckCooldown` — 是否在冷却中
4. `ActivateAbility` — 真正执行
5. `CommitAbility`（可选延迟）— 扣资源 + 上 Cooldown

Tag 在 Ability 上的配置：

| Tag 类型 | 作用 |
|---------|------|
| AbilityTags | 标识这个能力属于什么分类 |
| CancelAbilitiesWithTag | 激活时取消带有这些 Tag 的其他能力 |
| BlockAbilitiesWithTag | 激活期间阻止带有这些 Tag 的能力激活 |
| ActivationRequiredTags | 激活前提：owner 必须拥有这些 Tag |
| ActivationBlockedTags | 激活阻止：owner 拥有这些 Tag 时不能激活 |

**关键发现：激活检查是纯 owner 本地操作**。

`CanActivateAbility` 检查的所有 Tag 条件都基于 owner 自身的 OwnedTags 和 BlockedAbilityTags。不需要实时查询其他实体的状态。

**Scheduler 映射**：
- Tag 检查 → Think 阶段本地完成（读自身 private state）
- Cost 检查 → Think 阶段本地完成（读自身 Attribute）
- Cooldown 检查 → Think 阶段本地完成（读自身 Tag 或 timer 状态）
- CommitAbility → Think 阶段修改 private state + 注册 Cooldown timer
- **完全不需要跨 owner 协调**

**与参考 GAS 的差距**：参考 GAS 完全没有 Tag-based 激活条件、技能互斥/阻断。我们的 `tag/` 包已覆盖 GAS 的 Tag 需求（层级匹配、Has/HasAny/HasAll），且有编译态优化。

---

### 4.5 技能打断/取消

分析为三个场景：

**场景 A：自身能力间的互斥（纯本地）**

角色释放能力 A → 取消自己的能力 B。

- 纯本地逻辑，在 Think 阶段内部处理
- AbilitySystem 检查 CancelAbilitiesWithTag，直接取消匹配的能力
- **不需要 Signal 或 Effect**

**场景 B：跨实体打断（如眩晕）**

攻击者 X 对目标 Y 施放眩晕。

```
X.Think(): 产出 StunEffect{target: Y, duration: 3s}
                        │
                   Effect 投递
                        │
                        ▼
Y.Apply(): 收到 StunEffect
           → 裁决：是否免疫？减少持续时间？
           → 接受：Grant Tag "Status.CC.Stun"
           → 副作用：取消被 Stun 阻断的能力（本地处理）
```

- **跨实体打断是 Effect 语义**（状态修改请求），不是 Signal 语义
- 目标在 Apply 阶段裁决是否接受
- 能力取消是裁决结果的副作用

**场景 C：事件触发（如死亡爆炸被动）**

X 击杀 Y → Y 触发被动能力。

- **Signal 语义**：X 发出 KillSignal，Y 收到后 Think 阶段激活被动
- Y 的 WatchState 声明 Interest(KillSignal)

---

### 4.6 Targeting（目标选择）

**Unreal 做法**：
- TargetData：标准化的目标信息多态容器（HitResult、Actor 列表、位置等）
- 两种时机：即时确定 vs 延迟确定（通过 AbilityTask 等待）
- Snapshot 机制：Effect 创建时快照 Source 属性，Apply 时读 Target 最新状态

**与 Scheduler M10 模式的对比**：

| 维度 | Unreal GAS | 我们的 M10（攻方快照 + 守方裁决） |
|------|-----------|-------------------------------|
| Source 属性 | 创建 GESpec 时快照 | Think 阶段读自身 → 编入 Effect |
| Target 属性 | Apply 时读最新 | Apply 阶段读自身最新状态 |
| 结算方 | 目标 ASC 执行 | 目标 Logic.Apply |
| 时延容忍 | Snapshot 冻结，容忍 1 tick | BSP 快照，容忍 superstep 延迟 |

**核心一致性**：两者都是"攻方快照 + 守方裁决"模式，天然契合。

**Selector 与 WorldView 的冲突**：参考 GAS 的 Selector 直接遍历 `w.units`（可变引用）。在 Scheduler 中，Think 阶段只能通过 WorldView 读取只读快照。目标选择应通过 WorldView 提供的空间查询接口完成，返回 ref ID 列表而非可变指针。

---

### 4.7 Gameplay Cues（表现层通知）

**Unreal 做法**：Gameplay Cues 是纯客户端表现层通知（VFX、SFX、UI），通过 GameplayTag 路由。

**对纯服务器框架的结论**：
- 不需要 Cue 子系统
- 服务器需要告诉客户端"发生了什么"时，可以在 Effect/Signal 结构体中携带 CueHint 字段
- 客户端自行根据 CueHint 播放表现——这是客户端的职责，不是框架的
- **不影响 GAS ↔ Scheduler 协议设计**

---

## 5. 参考 GAS 缺失的概念维度

### 5.1 影响协议设计的缺失

| 缺失概念 | 影响 | 优先级 |
|---------|------|--------|
| **Base/Current 属性分离** | 决定 Buff 的可撤销性和 Modifier 管线的架构。Current 应作为 Public State 暴露。 | 高 |
| **Modifier 聚合管线** | 决定多个 Buff 如何组合影响同一属性。聚合公式保证顺序无关性。 | 高 |
| **Tag-based 激活条件** | 决定 Ability 激活检查的完整性。确认为纯本地操作，不影响协议。 | 中 |
| **技能互斥/打断** | 本地互斥不影响协议；跨实体打断确认为 Effect 语义。 | 中 |
| **Effect 持续类型分化** | Instant vs Duration 的区分影响 Effect 的生命周期管理方式。 | 高 |
| **Stacking 策略系统化** | 参考 GAS 的 4 种堆叠过于简单，需要扩展。但 Stacking 完全在 Apply 端，不影响协议层。 | 低 |
| **Meta Attribute 模式** | 伤害计算解耦——将 "Damage" 作为中间值，分离造成/承受逻辑。影响 Effect 的数据设计。 | 中 |
| **EffectContext** | Effect 应携带的上下文信息（Source 快照、命中信息等）。影响 Effect 结构体设计。 | 中 |

### 5.2 不影响协议设计的缺失

| 缺失概念 | 为什么不影响协议 |
|---------|----------------|
| Cost/Cooldown 系统化 | 纯 owner 私有事务，Think 阶段本地处理 |
| Stacking 细节策略 | 完全在 Apply 端，是 Logic 内部实现 |
| AbilityTask | 用 Running + Timer Wheel 替代，是 Logic 内部组织方式 |
| Gameplay Cues | 不需要，或作为 Effect 的附加字段 |
| Prediction/Rollback | 纯服务器框架不需要 |

---

## 6. GAS ↔ Scheduler 协议层核心结论

### 6.1 ASC 映射为 Logic 内部子系统

```
┌────────────────────────────────────────────────────────────┐
│                     Logic (= Owner)                        │
│                                                            │
│  ┌──────────────────────────────────────────────────────┐  │
│  │              AbilitySystem (internal subsystem)      │  │
│  │                                                      │  │
│  │  ┌───────────┐  ┌───────────┐  ┌─────────────────┐   │  │
│  │  │ Abilities │  │   Tags    │  │   Attributes    │   │  │
│  │  │           │  │ (Owned)   │  │ (Base/Current)  │   │  │
│  │  │ Cast()    │  │ (Blocked) │  │ Modifier Lists  │   │  │
│  │  │ CanUse()  │  │           │  │                 │   │  │
│  │  └───────────┘  └───────────┘  └─────────────────┘   │  │
│  │                                                      │  │
│  │  ┌──────────────────────────────────────────────┐    │  │
│  │  │ ActiveEffects (Duration/Infinite tracking)   │    │  │
│  │  └──────────────────────────────────────────────┘    │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                            │
│  Think(ctx, inbox):                                        │
│    - Read WorldView (snapshot)                             │
│    - Process Signals from inbox                            │
│    - AbilitySystem: check activations, tick timers         │
│    -   CanActivateAbility (local tag/cost/cd check)        │
│    -   Emit Signals / Publish Effects                      │
│    - Return next wakeup delay                              │
│                                                            │
│  Apply(ctx, inbox):                                        │
│    - Receive Effects from other Logics                     │
│    - AbilitySystem: apply incoming effects                 │
│    -   Modify Attributes (Base for Instant, Modifier       │
│    -     registration for Duration)                        │
│    -   Grant/Remove Tags                                   │
│    -   Handle stacking                                     │
│    -   Cancel blocked abilities (local side-effect)        │
│    -   Recompute Current Values                            │
│    -   Update Public State snapshot                        │
│    - Emit triggered Signals if needed                      │
└────────────────────────────────────────────────────────────┘
```

### 6.2 协议层不需要变化

**核心发现：现有 Scheduler 的 Logic/Signal/Effect/WatchState 四元协议足以支撑完整的 GAS**。

- Ability 激活 → Think 本地决策 + Effect/Signal 产出
- 属性修改 → Effect（跨实体）或 private state 修改（自身）
- 持续效果 → Apply 端 BuffList + Timer Wheel
- 技能打断 → Effect（跨实体）或 Think 本地处理（自身）
- 事件触发 → Signal + WatchState
- 目标选择 → WorldView 空间查询（Think 阶段）

**GAS 的所有概念要么映射为 Effect/Signal 数据，要么是 Logic 内部的私有实现细节。不需要新的协议原语。**

### 6.3 Modifier 管线在 Apply 端执行

```
Think (Source)                        Apply (Target)
┌──────────────────────┐              ┌──────────────────────────────┐
│ Read Target snapshot │              │ 1. Receive Effect            │
│ Decide: apply buff   │  ─Effect──>  │ 2. Register Modifier (Dur)   │
│ Build Effect with    │              │    or modify Base (Instant)  │
│   source snapshot    │              │ 3. Handle stacking           │
│                      │              │ 4. Recompute CurrentValue    │
│                      │              │    = f(Base, AllModifiers)   │
│                      │              │ 5. Pre/Post hooks            │
│                      │              │ 6. Update public snapshot    │
└──────────────────────┘              └──────────────────────────────┘
```

### 6.4 Effect 顺序无关性的结构性保证

Aggregator 模型天然满足——Current 是从 Base + 全量 Modifier 整体重算的：

```
Same tick, 3 Effects arrive at target in any order:
  Effect A: Add +10 to Attack
  Effect B: Multiply 1.2 to Attack
  Effect C: Add +5 to Attack

After all Applied:
  Modifier list = [Add: +10, +5] [Multiply: 1.2]
  CurrentValue = (Base + 10 + 5) * 1.2

Result identical regardless of ordering.
```

**边界情况**：Instant Effect 之间可能有条件依赖（如"HP < 30 时触发护盾"）。处理方式：Apply 内部按 Effect Kind 排序——状态变更类先于条件检查类（对应 adaptation_guide D-2 策略）。

---

## 7. 下一步设计方向

基于调研结论，GAS 设计的核心工作在 **Logic 内部架构**，而非 Scheduler 协议层。建议的设计重点：

### 7.1 属性系统

- 引入 Base/Current 分离
- 设计 Modifier 聚合管线（Add/Multiply/Override，固定聚合顺序）
- Modifier 按 Channel/Layer 分组（至少 2 层：装备 vs 技能）
- Aggregator dirty 标记 + 惰性重算
- Meta Attribute 模式用于伤害计算解耦

### 7.2 Effect 体系

- Instant Effect：修改 Base，fire-and-forget
- Duration/Infinite Effect（统一用 Expire 字段，0=Infinite）：注册 Modifier + Timer
- Periodic Effect：Running + Timer Wheel 周期唤醒
- EffectContext：携带 Source 快照、命中信息等上下文
- Stacking 策略：配置化（Type/Limit/Refresh/Reset/Expiration/Overflow）

### 7.3 能力系统

- 激活检查：Tag 条件 + Cost + Cooldown，纯本地
- 技能互斥/打断：CancelTags / BlockTags，纯本地
- 跨实体打断：Effect 语义，Apply 端裁决
- CommitAbility 两阶段（可选）：先激活探测，再扣资源
- Cooldown：Timer Wheel 直接管理，比 Duration GE + Tag 更轻量

### 7.4 内部组织

- AbilitySystem 作为 Logic 的内部子系统
- Abilities / Tags / Attributes / ActiveEffects 作为子系统的组成部分
- Running 用于需要独立 Think 逻辑的复杂效果
- 简单属性 buff 用 Apply 端 BuffList 直接管理

---

## 8. 参考来源

### 官方文档
- [UE5 Gameplay Ability System](https://docs.unrealengine.com/5.0/en-US/gameplay-ability-system-for-unreal-engine/)
- [UE5 Gameplay Attributes and Gameplay Effects](https://docs.unrealengine.com/5.0/en-US/gameplay-attributes-and-gameplay-effects-for-the-gameplay-ability-system-in-unreal-engine/)

### 社区文档
- [GASDocumentation (tranek)](https://github.com/tranek/GASDocumentation) — 最全面的社区文档
- [GAS Companion Plugin Documentation](https://gascompanion.github.io/)

### 内部参考
- `sched/world.go` — Scheduler 接口定义
- `docs/design/scheduler.md` — Scheduler 设计文档
- `docs/design/adaptation_guide.md` — 适配性分类指导手册
- `docs/tmp/research_attributes.md` — 属性系统调研原始数据
- `docs/tmp/research_effects.md` — Effect 系统调研原始数据
- `docs/tmp/research_abilities.md` — 能力激活调研原始数据
- `docs/tmp/research_cues_targeting_others.md` — Cues/Targeting/其他方案调研原始数据

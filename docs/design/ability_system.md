# Game Ability System 设计草稿

> 本文档是 GAS 在 Scheduler 框架上的设计草稿，描述设计意图而非最终实现。
> 基于 `docs/references/gas_survey.md` 的调研结论。
>
> **状态更新（2026-04-25）**：完整 GAS framework 不再计划作为 `game/` 的基础 package 落地。当前已将可复用的 Attribute / Modifier 聚合抽到 `attr/`；Ability、Effect、Buff、Cooldown、Cost、Stacking、Tag requirement 等业务强相关部分后续优先在 demo 层实现。本文件保留为设计参考和 Unreal GAS 映射资料。

Last Updated: 2026-04-25

---

## 目录

1. [设计目标](#1-设计目标)
2. [与 Scheduler 的关系](#2-与-scheduler-的关系)
3. [核心概念总览](#3-核心概念总览)
4. [属性系统 (Attributes)](#4-属性系统-attributes)
5. [Buff 系统](#5-buff-系统)
6. [能力系统 (Abilities)](#6-能力系统-abilities)
7. [Logic 内部组织](#7-logic-内部组织)
8. [端到端场景走查](#8-端到端场景走查)
9. [接口草案](#9-接口草案)
10. [开放问题](#10-开放问题)

---

## 1. 设计目标

1. **Scheduler 原生**：GAS 的所有行为都在 Logic.Think / Logic.Apply 内运行，不引入新的调度原语。
2. **构建块而非框架**：提供 `AttrTable`、`BuffTable`、`AbilitySet` 等可组合的构建块，用户在自己的 Logic 中组装，而非继承一个"GAS 框架"。Buff 是 `interface` 而非纯数据记录——框架提供常见的 stock 实现，用户可自由实现自己的 Buff 类型。
3. **顺序无关性的结构性保证**：属性计算采用 Aggregator 模型——Current 从 Base + 全量 Modifier 整体重算，不依赖 Effect 到达顺序。
4. **数据驱动**：Buff 堆叠策略、Modifier 运算、技能激活条件等尽量以配置数据表达，减少硬编码。
5. **轻量**：服务器侧框架，不包含客户端表现层（Gameplay Cue）、预测回滚等概念。

### 非目标

- 不定义具体的伤害公式——伤害计算是游戏层逻辑，由用户在 Apply 中编写。
- 不提供可视化编辑器或蓝图集成。
- 不处理网络同步——那是框架外层的职责。

---

## 2. 与 Scheduler 的关系

### 2.1 核心结论

> **现有 Scheduler 的 Logic / Signal / Effect / StagedState 协议足以支撑 GAS。不需要新的协议原语。**

GAS 的所有概念，要么映射为 Effect/Signal 的数据内容，要么是 Logic 内部的私有实现细节。

### 2.2 概念映射表

| GAS 概念 | Scheduler 映射 | 位置 |
|----------|----------------|------|
| AbilitySystem（能力容器） | Logic 内部子系统 | Logic 私有状态 |
| Ability（技能） | Logic.Think 中调度 | Logic 私有状态 |
| Buff（持续效果，thinkable） | Logic 内部 BuffTable，Buff interface 实例 | Logic 私有状态 |
| Attribute（属性） | Logic 内部 AttrTable（Base/Modifier=私有，Current=可通过 World/staged view 暴露） | Logic 状态 |
| Instant Effect（伤害/治疗） | Scheduler typed Effect | 协议层 |
| Buff Effect（施加/移除 buff） | Scheduler typed Effect | 协议层 |
| CC Effect（眩晕/沉默等） | Scheduler typed Effect（Grant Tag + Duration） | 协议层 |
| Gameplay Event（击杀通知等） | Scheduler Signal | 协议层 |
| 目标选择 | World/staged view 空间查询（Think 阶段只读） | 协议层 |
| 技能冷却 | Logic 内部 timer 或 BuffTable min-heap | Logic 私有状态 |
| Tag 状态 | `tag/` 包（Logic 私有持有） | Logic 私有状态 |

### 2.3 Think / Apply 职责划分

```
Think phase (read-only world, may modify own private state)
+-- Process inbox Signals (event triggers)
+-- Check ability activation (Tag/Cost/Cooldown, local only)
+-- Target selection (World/staged view read-only query)
+-- Produce Effects (Publish) and Signals (Emit)
+-- Self-maintenance: BuffTable.Tick (drive Buff.Think), recompute Current
+-- Return next wakeup time (min of all internal timers)

Apply phase (receive Effects, modify own state)
+-- Process Effects from other Logics
|   +-- Instant Effect -> modify Base, recompute Current
|   +-- Buff Effect -> BuffTable.Add (factory creates Buff instance)
|   +-- CC Effect -> Grant Tag, register duration
+-- Apply-side side-effects
|   +-- Tag change -> check ability block/cancel (local)
|   +-- Attribute reaches zero -> death check
|   +-- Emit reactive Signals (e.g. death notification)
+-- Update Public State snapshot
```

### 2.4 状态修改时机

Think 和 Apply 都可以修改 Logic 自身状态。区别在于：

- **Think 修改**：自维护行为（Buff.Think 执行、cooldown 到期、属性重算）。这些变化在当前 superstep 的 World/staged view 中**不可见**（BSP 语义），下一轮才可见。
- **Apply 修改**：响应外部 Effect（受到伤害、被施加 buff）。同样下一轮可见。

两者对同一 Logic 不会并发执行（Think → barrier → Apply），无竞争。

### 2.5 计算分解约束

在并行 tick 架构中，没有任何一个阶段能同时访问 Source 和 Target 的完整最新状态。任何依赖双方状态的计算公式，必须可分解为 Source 端函数和 Target 端函数，由 Effect 数据连接：

```
payload     = f_source(source.state, target.snapshot)   // Think phase
finalResult = f_target(payload, target.currentState)     // Apply phase
```

这意味着 Effect 的数据设计至关重要——它是连接 Think/Apply 信息割裂的唯一桥梁。

详细的可见性矩阵、分解示例和设计指导参见 `docs/design/scheduler.md` "计算分解约束" 章节。

### 2.6 Effect 数据设计指导

基于计算分解约束，Effect 应携带的数据遵循以下模式：

| 数据类别 | 说明 | 示例 |
|----------|------|------|
| **中间计算结果** | Source 端已完成的计算，Target 端继续处理 | rawDamage, healAmount |
| **不可预计算的 Source 参数** | Target 端公式需要但无法由 Source 预算的原始值 | sourceLevel, penetration |
| **元信息** | 影响 Target 端处理分支的标记 | element type, flags (blockable/reflectable), effect source ref |
| **不包含** | Source 的全部属性快照 | ~~fullAttrSnapshot~~ |

**原则**：Effect 是"Source 端计算的输出 + Target 端计算的输入"，而非 Source 状态的完整镜像。Source 端应尽可能完成自己能做的计算，只将中间结果和 Target 端必需的少量参数打包进 Effect。

---

## 3. 核心概念总览

```
+-------------------------------------------------------------+
|                    Logic (= Owner)                          |
|                                                             |
|  +--------------------------------------------------------+ |
|  |               AbilitySystem (subsystem)                | |
|  |                                                        | |
|  |  +--------------+  +----------+  +------------------+  | |
|  |  |  AbilitySet  |  |   Tags   |  |    AttrTable     |  | |
|  |  |              |  |  (Owned) |  | (Base / Current) |  | |
|  |  |  ability_1   |  |  (Block) |  |  HP, MP, ATK ... |  | |
|  |  |  ability_2   |  |          |  |  Modifier lists  |  | |
|  |  |  ...         |  |          |  |                  |  | |
|  |  +--------------+  +----------+  +------------------+  | |
|  |                                                        | |
|  |  +--------------------------------------------------+  | |
|  |  |              BuffTable                           |  | |
|  |  |  [ModifierBuff: +ATK 20%, thinkable (expiry)]   |  | |
|  |  |  [PeriodicBuff: DOT 5/s, thinkable (periodic)]  |  | |
|  |  |  [TagBuff: stun, thinkable (expiry)]             |  | |
|  |  |  [Custom: user-defined Buff interface impl]      |  | |
|  |  +--------------------------------------------------+  | |
|  +--------------------------------------------------------+ |
|                                                             |
|  Think(ctx, inbox) int64                                    |
|  Apply(ctx, inbox)                                          |
+-------------------------------------------------------------+
```

四个构建块：

| 构建块 | 职责 | 核心数据 |
|--------|------|---------|
| **AttrTable** | 属性管理 + Modifier 聚合 | Base/Current/Modifier list per attribute |
| **BuffTable** | 持续效果管理（thinkable），统一 modifier buff / DOT / HOT / CC / 被动触发等 | Buff interface 实例 + min-heap by next wakeup |
| **AbilitySet** | 技能注册 + 激活检查 + Cooldown | Ability list + cooldown timers |
| **Tags** | 状态标记 + 条件查询 | OwnedTags + BlockedAbilityTags（复用 `tag/` 包） |

---

## 4. 属性系统 (Attributes)

### 4.1 Base / Current 分离

每个属性包含两个值：

| 字段 | 含义 | 修改方 |
|------|------|--------|
| **Base** | 永久值，被 Instant Effect 修改 | Apply（外部 Effect）或 Think（自消耗） |
| **Current** | 运行时值，从 Base + 全量 Modifier 重算 | 任何 Modifier 变化后自动重算 |

Current 可通过 World/staged view 暴露给其他 Logic 读取（如 AI 读取 target HP 做决策）。

```
Buff.OnApply:  register Modifier to AttrTable -> Current recomputed (Base + mods)
Buff.OnRemove: remove Modifier from AttrTable -> Current restored (back to no-buff state)
Instant Effect (e.g. damage): Base -= damage -> Current recomputed
```

### 4.2 Modifier 模型

Modifier 是 **AttrTable 内部的贡献记录**，不是独立存在的概念。每个 Modifier 记录了某个 Buff 对某个属性的修改：

```
Modifier {
    Source   uint32     // buff instance ID, used for batch cleanup
    Op       ModOp      // Add / Multiply / Override
    Value    float64    // modification amount
    Channel  int8       // aggregation channel (optional, single-channel for v1)
}
```

Modifier 的生命周期完全由 Buff 管理：

- **Buff.OnApply** 调用 `AttrTable.AddModifier()` 注册 Modifier
- **Buff.OnRemove** 调用 `AttrTable.RemoveModifiersBySource()` 批量清理
- Buff 不存在 → 对应 Modifier 不存在（不变量）

**ModOp 类型**：

| Op | 语义 | 聚合方式 |
|----|------|---------|
| Add | 加法修正 | Σ value |
| Multiply | 百分比修正 | 1 + Σ value（同通道内加法聚合，避免百分比爆炸） |
| Override | 强制覆盖 | 优先级最高者生效 |

### 4.3 聚合公式

单通道聚合：

```
Current = (Base + Sum(Add.values)) * (1 + Sum(Mul.values))
```

如有 Override modifier 存在，直接取 Override 值，忽略其他。

**多通道聚合**（可选扩展）：

```
Channel 0: result_0 = (Base     + Sum(Add_0)) * (1 + Sum(Mul_0))
Channel 1: result_1 = (result_0 + Sum(Add_1)) * (1 + Sum(Mul_1))
...
Final Current = result_N
```

通道之间串行传递，同通道内加法聚合。典型用途：装备加成（Channel 0）和技能加成（Channel 1）之间的乘法关系。

**初版建议**：单通道，未来按需扩展多通道。

### 4.4 Dirty 标记与惰性重算

```
AddModifier / RemoveModifier -> mark attribute dirty
Reading Current:
  if dirty -> recompute -> clear dirty -> return
  else     -> return cached
```

批量处理多个 Effect 时，每个 Effect 只标记 dirty，最后统一重算一次，避免冗余计算。

### 4.5 Clamp 与 联动属性

某些属性有约束关系：

- `HP` 不能超过 `MaxHP`，不能低于 0
- `MP` 不能超过 `MaxMP`

这通过属性的 Clamp 配置实现：

```
AttrDef {
    Kind     AttrKind
    Min      *AttrKind   // optional lower bound attr (e.g. 0 or another attr)
    Max      *AttrKind   // optional upper bound attr (e.g. MaxHP)
    MinConst float64     // fixed lower bound (default -Inf)
    MaxConst float64     // fixed upper bound (default +Inf)
}
```

Clamp 在 Recompute 末尾执行。

### 4.6 Meta Attribute 模式

**Meta Attribute** 是临时计算属性，不暴露为 Public State：

```
Scenario: damage calculation
1. Apply receives DamageEffect{rawDamage: 100, element: Fire}
2. Compute: finalDamage = rawDamage * (1 - fireResist) * armorReduction
3. Modify: HP.Base -= finalDamage
4. Check: HP.Current <= 0 -> death
```

Meta Attribute（如 rawDamage）只在 Apply 内部的计算流程中存在，不注册到 AttrTable、不暴露给 World/staged view。用户在 Apply 的计算函数中自行使用局部变量即可。

GAS 库不需要为 Meta Attribute 提供专门机制——这是用户在 Apply 回调中的自由代码。

### 4.7 Modifier 定位说明

Modifier 不是独立的系统组件，而是 AttrTable 内部的记录结构。它在整个数据流中的位置如下：

```
Source.Think                        Target.Apply
+-----------+                      +------------------------------+
| Produce   |   Effect (typed)     | Receive Effect               |
| Effect    | -------------------> |   |                          |
+-----------+                      |   v                          |
                                   | BuffTable.Add(def, buff)     |
                                   |   |                          |
                                   |   v                          |
                                   | buff.OnApply(ctx):           |
                                   |   |                          |
                                   |   +-> AttrTable.AddModifier  |
                                   |   +-> TagState.Grant         |
                                   |   +-> return first Think t   |
                                   |                              |
                                   +------------------------------+

Target.Think (timer wakeup)
+------------------------------+
| BuffTable.Tick(now):         |
|   |                          |
|   v                          |
| buff.Think(ctx):             |
|   +-> periodic action        |
|   +-> return next wakeup / <0 to self-remove
|                              |
| If Think returns <0:         |
|   buff.OnRemove(ctx):        |
|     +-> AttrTable.RemoveMods |
|     +-> TagState.Revoke      |
+------------------------------+
```

关系链：**Effect → BuffTable.Add → Buff.OnApply → Modifier registered in AttrTable → Attribute recomputed**

---

## 5. Buff 系统

### 5.1 Buff 的本质（修订）

Buff 是 Logic 内部的 **thinkable 子逻辑**。它统一了旧设计中"固定参数 Buff"和"Running（复杂子逻辑）"的概念——所有 BuffTable 管理的条目都是实现了 `Buff` interface 的实例，拥有自己的定时驱动能力。

**关键定位**：

- Buff **不是** Scheduler 的 Logic（没有自己的 ref ID，不被 Scheduler 直接调度）
- Buff 是挂在 owner Logic 上的 **子逻辑**，由 BuffTable 在 owner 的 Think 阶段代为驱动
- Buff 的 Think 可以包含任意复杂的逻辑，但只影响 owner 自身状态

```
Source.Think: Publish(target, ApplyBuffEffect{buffID, params...})
                          |
                     Effect delivery
                          |
                          v
Target.Apply: Receive ApplyBuffEffect
              -> Factory creates Buff instance (implements Buff interface)
              -> BuffTable.Add(def, buff, source, duration)
              -> buff.OnApply(ctx): register Modifiers, grant Tags
              -> heap registers next wakeup time

Target.Think (timer wakeup):
              -> BuffTable.Tick(now)
              -> buff.Think(ctx): periodic action, return next wakeup
```

### 5.2 Buff Interface

```go
// Buff 是 BuffTable 管理的持续效果实例。
// 每个 Buff 实例都是一个 thinkable 子逻辑，由 BuffTable.Tick 在 owner 的 Think 阶段驱动。
type Buff interface {
    ID() int32

    // OnApply 在 buff 首次施加时调用。
    // 典型操作：注册 Modifier 到 AttrTable、Grant Tag。
    // 返回首次 Think 的时间戳（绝对时间），<0 表示无需 Think（纯被动 buff）。
    OnApply(ctx BuffCtx) int64

    // OnRemove 在 buff 被移除时调用（过期、驱散、自行结束）。
    // 必须清理 OnApply 中注册的所有 Modifier 和 Tag。
    OnRemove(ctx BuffCtx)

    // OnStack 在堆叠层数变化时调用（新堆叠施加或一层过期）。
    // 典型操作：更新 Modifier 值。
    // 返回新的 Think 时间戳（可用于刷新过期），<0 表示不改变现有 schedule。
    OnStack(ctx BuffCtx) int64

    // Think 在定时唤醒时调用（由 BuffTable.Tick 驱动）。
    // 典型操作：DOT 扣血、HOT 加血、检查过期、条件判断。
    // 返回下次 Think 的时间戳（绝对时间），<0 表示 buff 自行结束，BuffTable 将自动移除。
    Think(ctx BuffCtx) int64
}

// BuffCtx 提供 Buff 回调所需的上下文。
type BuffCtx struct {
    Now    int64       // 当前 tick 时间
    Attrs  *AttrTable  // owner 的属性表（可读写）
    Tags   *TagState   // owner 的 tag 状态（可读写）
}
```

### 5.3 Stock 实现

框架提供常见的 Buff 实现，覆盖大部分典型场景。用户无需为简单 buff 编写自定义代码：

| Stock 实现 | 用途 | OnApply 行为 | Think 行为 |
|-----------|------|-------------|-----------|
| **ModifierBuff** | 属性修正 buff（+ATK, +移速） | 注册 Modifier | 检查过期，无其他逻辑 |
| **PeriodicBuff** | DOT / HOT | 可选注册 Modifier/Tag | 每个 period 执行一次 action |
| **TagBuff** | 状态标记（眩晕、沉默、免疫） | Grant Tag | 检查过期 |
| **ShieldBuff** | 伤害吸收护盾 | 注册吸收量 | 检查过期，吸收量耗尽时返回 <0 自行结束 |
| **自定义** | 任何逻辑 | 用户实现 | 用户实现 Buff 接口 |

**Stock 实现由工厂函数创建**。以 ModifierBuff 为例：

```go
// NewModifierBuff 创建一个属性修正 buff。
// mods 是要注册的 Modifier 列表（Attr + Op + Value）。
// expireAt 是过期时间戳，0 = 永久。
func NewModifierBuff(id int32, instID uint32, mods []ModTemplate, expireAt int64) Buff
```

### 5.4 Buff 类型定义（配置数据）

BuffDef 只管堆叠策略，不管 Buff 的内部逻辑。Buff 的行为由 interface 实现决定，BuffDef 只描述"当多个同类 Buff 施加时如何处理"：

```
BuffDef {
    ID              int32
    StackType       StackType    // None / BySource / ByTarget
    StackLimit      int32        // max stack count, 0 = unlimited
    DurationRefresh bool         // new stack refreshes duration?
    ExpirationPolicy ExpPolicy   // on expiry: ClearAll / RemoveOneStack / RefreshDuration
}
```

**BuffDef + 工厂函数 = Buff 实例**。在 Apply 端处理 ApplyBuffEffect 时：

```
1. Look up BuffDef by buffID
2. Call factory function to create Buff instance (carries effect params)
3. BuffTable.Add(def, buff, source, duration)
4. BuffTable handles stacking per BuffDef policy
```

### 5.5 Stacking 策略

**StackType**：

| 类型 | 行为 | 典型场景 |
|------|------|---------|
| **None** | 每次施加创建独立实例 | 独立计时的 DOT |
| **BySource** | 同一来源只保留一个实例，层数累加 | 英雄联盟被动叠层 |
| **ByTarget** | 所有来源共享一个实例，层数累加 | 全局 debuff 标记 |

**施加流程**：

```
Receive ApplyBuffEffect:

1. Look up BuffDef (by buffID)
2. Find existing instance by StackType:
   - None     -> no lookup, always create new
   - BySource -> find same buffID + same source
   - ByTarget -> find same buffID (ignore source)

3a. No existing -> create new instance
    - Factory creates Buff impl
    - buff.OnApply(ctx): register Modifiers, grant Tags, return first Think time
    - StackCount = 1
    - Register in heap

3b. Found existing -> stacking
    - StackCount += 1 (capped by StackLimit)
    - if StackCount > StackLimit -> overflow handling
    - if DurationRefresh -> refresh expiry
    - buff.OnStack(ctx): update Modifier values
    - Update heap entry

4. Mark related attributes dirty
5. Update next wakeup time
```

### 5.6 Buff 过期与移除

**Think 驱动自移除**：当 `Buff.Think()` 返回 `<0` 时，BuffTable 自动移除该 Buff 实例。这是 Buff 结束生命的主要方式——stock 实现在检测到过期时返回 `<0`。

```
BuffTable.Tick(now):
  for each buff whose next wakeup <= now:
    result = buff.Think(ctx)
    if result < 0:
      buff.OnRemove(ctx)   // cleanup: remove Modifiers, revoke Tags
      remove from instances and heap
    else:
      update heap with new wakeup time = result
```

**ExpirationPolicy**（应用于 stock 实现的过期逻辑）：

| 策略 | 行为 |
|------|------|
| ClearAll | 移除整个 buff 实例（Think 返回 <0） |
| RemoveOneStack | StackCount -= 1，如果 StackCount == 0 则移除 |
| RefreshDuration | 重置过期时间（永不真正过期，需外部驱散） |

**移除清理**（由 Buff.OnRemove 负责）：

```
buff.OnRemove(ctx):
  1. AttrTable.RemoveModifiersBySource(instanceID) -- remove all Modifiers
  2. TagState.Revoke(grantedTags...)                -- remove all Tags
  3. (related attributes auto-marked dirty)
```

**主动移除**（驱散效果）：

```
Source.Think: Publish(target, RemoveBuffEffect{buffID: 42})
Target.Apply: BuffTable.Remove(42) -> buff.OnRemove(ctx) -> cleanup
```

### 5.7 Timer 管理

BuffTable 内部使用 **min-heap** 按 Buff 实例的 next wakeup 时间排序。每个 Buff 实例在 heap 中有一个条目，其 deadline 为 `Buff.OnApply` 或 `Buff.Think` 最近一次返回的时间戳。

```go
func (bt *BuffTable) NextWakeup() int64 {
    if bt.heap.Len() == 0 {
        return 0  // no wakeup needed
    }
    return bt.heap.Peek().deadline
}
```

Logic.Think 返回的 wakeup delay = min(AbilitySet.NextWakeup(), BuffTable.NextWakeup(), ...)。

不需要 Think 的 Buff（OnApply 返回 <0 的纯被动 buff）不进入 heap。这些 Buff 只能通过外部 `BuffTable.Remove()` 移除。

### 5.8 BuffTable 结构

```go
type buffEntry struct {
    buff       Buff        // interface instance
    def        *BuffDef    // stacking policy
    instanceID uint32      // unique instance ID within this table
    source     uint64      // caster ref
    stackCount int32       // current stacks
}

type BuffTable struct {
    instances  []buffEntry     // all active buff instances
    heap       minHeap         // sorted by next wakeup time
    defs       map[int32]*BuffDef // stacking policy configs, keyed by buff type ID
    attrs      *AttrTable      // reference
    tags       *TagState       // reference
    nextInstID uint32          // instance ID allocator
}

func NewBuffTable(attrs *AttrTable, tags *TagState) *BuffTable

// Tick drives all due Buff.Think calls. Called during owner's Think phase.
func (bt *BuffTable) Tick(now int64)

// Add creates or stacks a buff instance. Called during owner's Apply phase.
func (bt *BuffTable) Add(def *BuffDef, buff Buff, source uint64, duration int64)

// Remove removes a buff by type ID, calling OnRemove for cleanup.
func (bt *BuffTable) Remove(buffID int32)

// NextWakeup returns the earliest wakeup time across all buff instances.
func (bt *BuffTable) NextWakeup() int64

// Query
func (bt *BuffTable) Has(buffID int32) bool
func (bt *BuffTable) StackCount(buffID int32) int32
```

### 5.9 Buff vs 独立 Logic

| 维度 | Buff | 独立 Logic |
|------|------|-----------|
| 状态归属 | owner 的私有子逻辑 | 自己的 ref ID，独立调度 |
| Think 复杂度 | 任意（但只影响 owner） | 任意 |
| 跨实体交互 | 不直接产出 Effect/Signal | 可自由 Emit/Publish |
| 世界存在感 | 无（挂在 owner 上） | 有（位置、碰撞等） |
| 典型场景 | 属性 buff、DOT、CC、护盾、被动 | 弹道、召唤物、光环中心、陷阱 |

**判定口诀**：**如果它只影响 owner 自身 → Buff。如果它需要在世界中独立存在或主动与其他实体交互 → 独立 Logic。**

边界案例指导：

- **光环 buff**（对周围单位施加效果）：光环中心 = 独立 Logic（需要位置、周期扫描周围实体、产出 Effect）。被光环影响的增益 = target 上的 Buff。
- **荆棘反伤**（受伤时反弹伤害）：参见第 10 节开放问题。
- **弹道附带 debuff**：弹道 = 独立 Logic。命中后施加的 debuff = target 上的 Buff。

---

## 6. 能力系统 (Abilities)

### 6.1 Ability 的职责

Ability 代表一个可激活的技能/动作。它的核心职责：

1. **判断能否激活**（CanActivate）
2. **执行激活**（Activate）：产出 Effect/Signal
3. **管理冷却**（Cooldown）
4. **响应事件**（OnEvent）：被动技能

### 6.2 Ability 接口

```go
type AbilityI interface {
    ID()           int32
    CanActivate()  bool        // check activation conditions
    Activate(ctx)              // execute activation
    Cancel()                   // cancel (e.g. interrupted)
    IsActive()     bool        // is currently executing
}
```

### 6.3 激活检查链

```
CanActivate:
  1. Tag condition check (local only)
     - RequiredTags:  owner must have these Tags
     - BlockedTags:   cannot activate if owner has these Tags
     - Check AbilitySet.BlockedAbilityTags for this ability's tag

  2. Cost check (local only)
     - Read resource attribute from AttrTable (MP, rage, etc.)
     - Check Current >= cost

  3. Cooldown check (local only)
     - Check cooldown timer has expired

  4. Custom conditions (user extension)
     - e.g. requires specific equipment, must be grounded, etc.
```

**所有检查都基于 owner 私有状态，在 Think 阶段本地完成，不需要跨 Logic 协调。**

这是 GAS 设计的一个核心优势：通过把"别人对我的影响"编码为"我自己的 Tag/Attribute 状态变化"（Apply 阶段由 Effect 驱动），将跨实体交互转化为本地状态查询。

### 6.4 Cost & Cooldown

**Cost**（资源消耗）：

```
Activate:
  1. CanActivate already confirmed
  2. Deduct resource: AttrTable.ModifyBase(MP, -cost)
  3. Produce Effects/Signals
```

Cost 扣除发生在 Think 阶段（修改自身 private state）。如果需要"先激活，稍后再扣"（如需要瞄准确认），可以拆分为两阶段：

```
Two-phase mode (optional):
  Phase 1: Activate -> enter pre-activation state, no cost
  Phase 2: Commit   -> confirm activation, deduct cost, start Cooldown
  Cancel:  before Commit, can cancel without cost
```

**Cooldown**（冷却）：

冷却用 Logic 内部的 timer 管理，不需要用 Duration GE + Tag 这样重的机制：

```
Activate -> Commit:
  cooldownExpire = now + cooldownDuration
  Register to AbilitySet timer (or share BuffTable's min-heap)

CanActivate:
  now >= cooldownExpire -> cooldown ended
```

Cooldown 可选择通过 Tag 标记（`Cooldown.Ability.Fireball`），让外部效果可以查询/修改冷却状态（如"冷却缩减" buff）。

### 6.5 Tag-Based 能力互斥与打断

**本地互斥（同一 Logic 内部）**：

```
Ability A config:
  CancelAbilitiesWithTag: ["Ability.Channel"]
  BlockAbilitiesWithTag:  ["Ability.Channel"]

When activating Ability A:
  1. AbilitySet iterates all active abilities
  2. If an active ability's tag matches CancelAbilitiesWithTag -> call its Cancel()
  3. Inject BlockAbilitiesWithTag into BlockedAbilityTags
  4. When Ability A ends, remove those tags from BlockedAbilityTags
```

纯本地操作，在 Think 阶段完成。

**跨实体打断（如 CC 效果）**：

```
Attacker.Think: Publish(target, StunEffect{duration: 3s})

Target.Apply:
  1. Receive StunEffect
  2. Adjudicate: check CC immunity (tag query), reduce duration (tenacity attr)
  3. Accept: BuffTable.Add(StunDef, NewTagBuff(..., "Status.CC.Stun"), ...)
     -> TagBuff.OnApply(): Grant Tag "Status.CC.Stun", return expiry time
  4. Tag change side-effect: iterate active abilities, Cancel all blocked by Stun

Target next Think:
  - CanActivate(any): BlockedTags contains CC.Stun -> rejected
  - StunBuff expires -> Think returns <0 -> OnRemove revokes Tag -> normal resumes
```

**关键设计**：跨实体打断是 **Effect 语义**（状态修改请求），不是 Signal 语义。目标在 Apply 端裁决是否接受，能力取消是裁决的本地副作用。

### 6.6 被动技能（Event-Driven Abilities）

某些 ability 不需要主动激活，而是响应事件自动触发：

```
Ability "Death Explosion":
  TriggerSignal: KillSignal  (when owner is killed)
  OnTrigger: -> Publish(area_targets, DamageEffect{...})

Integration:
  owner's framework StagedState exposes WatchBits for KillSignal
  Logic.Think receives KillSignal -> AbilitySet.OnEvent(KillSignal)
  -> matching passive ability auto-Activates
```

---

## 7. Logic 内部组织

### 7.1 AbilitySystem 作为组装容器

AbilitySystem 不是一个万能的 God Object，而是一个**薄层组装器**，持有并协调四个构建块：

```go
type AbilitySystem struct {
    Attrs     *AttrTable
    Buffs     *BuffTable
    Abilities *AbilitySet
    Tags      *TagState
}
```

用户的 Logic 实现持有一个 AbilitySystem 实例，在 Think/Apply 中调用其方法：

```go
type MyUnit struct {
    id   uint64
    gas  AbilitySystem
    // ... other game state
}

func (u *MyUnit) Think(ctx *ThinkCtx[...], inbox Inbox[MySignal]) int64 {
    // 1. Process Signals
    for i := 0; i < inbox.Len(); i++ {
        u.gas.OnSignal(inbox.At(i))
    }

    // 2. Self-maintenance: drive Buff.Think, recompute attributes
    u.gas.Tick(ctx.World.Now())

    // 3. Ability decisions (AI or player command)
    if u.wantCast(skillID) {
        if u.gas.CanActivate(skillID) {
            u.gas.Activate(skillID, ctx)  // internally Publish/Emit
        }
    }

    // 4. Return next wakeup
    return u.gas.NextWakeup(ctx.World.Now())
}

func (u *MyUnit) Apply(ctx *CommitCtx[...], inbox Inbox[MyEffect]) {
    // Process each Effect
    for i := 0; i < inbox.Len(); i++ {
        u.gas.ApplyEffect(inbox.At(i), ctx)
    }

    // Flush: recompute all dirty attributes + handle side-effects
    u.gas.Flush(ctx)
}
```

### 7.2 Effect 分发

用户定义自己的 Effect 类型（实现 `sched.EffectI`）。GAS 不限制 Effect 的具体类型，用户在 `ApplyEffect` 中做分发：

```go
func (gas *AbilitySystem) ApplyEffect(e MyEffect, ctx *CommitCtx[...]) {
    switch e.Kind() {
    case EffectDamage:
        gas.applyDamage(e.RawDamage, e.Element, e.SourceLevel)
    case EffectHeal:
        gas.applyHeal(e.HealAmount)
    case EffectApplyBuff:
        buff := e.Factory(gas.Buffs.nextInstID)  // factory creates Buff impl
        gas.Buffs.Add(e.Def, buff, e.Source, e.Duration)
    case EffectRemoveBuff:
        gas.Buffs.Remove(e.BuffID)
    case EffectCC:
        gas.applyCC(e.CCType, e.Duration, e.Tenacity)
    }
}
```

这保持了 GAS 作为构建块的灵活性——用户完全控制 Effect 的类型定义和分发逻辑。

### 7.3 Effect 数据设计（Source 快照修订）

Effect 的数据结构应遵循 2.6 节的设计指导和 `docs/design/scheduler.md` 的计算分解约束。核心原则是 **Effect 携带中间结果，不是 Source 的全部属性快照**。

推荐的 Effect 数据模式：

```go
type MyEffect struct {
    kind     EffectKind
    target   uint64          // Scheduler routes this
    order    int32           // Effect ordering key

    // -- Intermediate results (source-side computation output) --
    rawDamage float64        // baseDmg * spellPower (already computed)
    healAmount float64

    // -- Non-precomputable source params (target needs for its formula) --
    sourceLevel int32
    penetration float64

    // -- Metadata --
    element  ElementType     // fire, ice, etc.
    flags    EffectFlags     // blockable, reflectable, etc.
    sourceRef uint64         // who cast this

    // -- Buff application --
    buffDef  *BuffDef
    factory  func(instID uint32) Buff  // creates Buff impl with captured params
    duration int64
}
```

与旧设计中 `SourceSnapshot` 的区别：

| 旧设计（SourceSnapshot） | 新设计（中间结果） |
|-------------------------|-------------------|
| 携带 SpellPower, CritChance, Level... | 携带 rawDamage（已算好）, sourceLevel（无法预算） |
| Target 端从快照读取 Source 属性做计算 | Target 端只用中间结果 + 自身最新状态做计算 |
| Source 状态泄漏到 Effect 中 | Effect 只包含计算必需的最小数据 |

这对应 **攻方快照 + 守方裁决** 模式（adaptation guide M10），但更精确地界定了"快照"的内容边界。

---

## 8. 端到端场景走查

### 8.1 场景 A：法师释放火球

```
Tick N:

  Mage.Think (Timer wakeup or player command):
    1. AbilitySet.CanActivate(Fireball)
       -> Tag check: no BlockedTags -> OK
       -> Cost check: MP.Current >= 30 -> OK
       -> Cooldown check: now >= cdExpire -> OK
    2. AbilitySet.Activate(Fireball)
       -> MP.Base -= 30 (cost deduct, private state)
       -> cdExpire = now + 5s (register cooldown timer)
    3. Target selection: World.SpatialQuery(pos, range=10)
       -> returns [targetRef]
    4. ctx.Publish(targetRef, DamageEffect{
         rawDamage: 50 * mage.attrs.Current(SpellPower) / 100,
         element: Fire,
         sourceRef: mage.id,
       })
    5. return min(cdExpire - now, gas.NextWakeup())

  [Effect routed to target]

  Target.Apply:
    1. Receive DamageEffect
    2. Compute: finalDamage = rawDamage * (1 - target.fireResist)
    3. HP.Base -= finalDamage -> mark dirty
    4. Flush: HP.Recompute() -> HP.Current = clamp(HP.Base + mods, 0, MaxHP)
    5. if HP.Current <= 0 -> ctx.Emit(mage, KillSignal{victim: target})
```

### 8.2 场景 B：施加 DOT（thinkable Buff）

```
Tick N:

  Warlock.Think:
    -> Publish(target, ApplyBuffEffect{
        buffID: PoisonDOT,
        source: warlock.ID(),
        duration: 5000,       // 5s
        damagePerPeriod: 10,
        factory: func(instID) Buff {
            return NewPeriodicBuff(PoisonDOT, instID, 1000, 10, ...)
        },
      })

  Target.Apply:
    -> BuffTable.Add(PoisonDOT):
      -> BuffDef lookup: StackType=BySource
      -> No existing from this source -> create new instance
      -> Factory creates PeriodicBuff instance
      -> buff.OnApply(ctx):
         Grant Tag "Status.DOT.Poison"
         return now + 1000 (first Think at now+1s)
      -> heap registers wakeup at now+1s

Tick N+1 (timer wakeup at first Think time):

  Target.Think:
    -> BuffTable.Tick(now):
      -> PoisonBuff.Think(ctx):
         HP.Base -= 10, mark dirty
         return now + 1000 (next Think at now+1s)
      -> heap updated

Tick N+5 (buff decides to expire):

  Target.Think:
    -> BuffTable.Tick(now):
      -> PoisonBuff.Think(ctx):
         elapsed >= duration -> return -1 (self-remove)
      -> buff.OnRemove(ctx):
         Revoke Tag "Status.DOT.Poison"
      -> instance removed from table and heap
```

### 8.3 场景 C：眩晕打断吟唱

```
Tick N: Target is channeling (Ability "Fireball" in channeling state)

  Warrior.Think:
    -> Publish(target, CCEffect{
        ccType: Stun,
        duration: 2000,
      })

  Target.Apply:
    1. Receive CCEffect
    2. Adjudicate: check Tag "Status.Immune.CC" -> not present, accept
    3. BuffTable.Add(StunDef, NewTagBuff(..., "Status.CC.Stun", 2000)):
       -> TagBuff.OnApply(ctx):
          Grant Tag "Status.CC.Stun"
          return now + 2000 (expiry time)
    4. Tag change side-effect:
       -> AbilitySet detects "Status.CC.Stun" added
       -> Fireball.IsActive() && Fireball's BlockedByTags contains CC.Stun
       -> Fireball.Cancel() -> terminate channeling

  Target subsequent Think:
    -> CanActivate(any): BlockedTags contains CC.Stun -> rejected
    -> 2s later: TagBuff.Think(ctx): expired -> return -1
    -> TagBuff.OnRemove(ctx): Revoke Tag -> normal resumes
```

### 8.4 场景 D：属性 Buff 堆叠

```
Tick N: Bard casts +20% AttackPower buff on Target (ByTarget stacking, limit=3)

  Bard.Think:
    -> Publish(target, ApplyBuffEffect{
        buffID: WarCry,
        factory: func(instID) Buff {
            return NewModifierBuff(WarCry, instID,
                []ModTemplate{{ATK, Multiply, 0.2}}, now+10s)
        },
        duration: 10s,
      })

  Target.Apply:
    -> BuffTable.Add(WarCry):
      -> BuffDef: StackType=ByTarget, StackLimit=3, DurationRefresh=true
      -> No existing -> create, StackCount=1
      -> buff.OnApply(ctx):
         AddModifier(ATK, Modifier{Source: instID, Op: Multiply, Value: 0.2})
         return now + 10s (expiry)
      -> ATK.dirty = true
    -> Flush:
      -> ATK.Current = (ATK.Base + Sum(Add)) * (1 + 0.2) = ATK.Base * 1.2

Tick N+2: Another Bard also casts WarCry

  Target.Apply:
    -> BuffTable.Add(WarCry):
      -> StackType=ByTarget -> found existing instance
      -> StackCount: 1 -> 2 (< limit 3)
      -> DurationRefresh=true -> refresh expiry to now+10s
      -> buff.OnStack(ctx):
         Update Modifier: Value = 0.2 * 2 = 0.4
         return now + 10s (refreshed)
      -> ATK.dirty = true
    -> Flush:
      -> ATK.Current = ATK.Base * (1 + 0.4) = ATK.Base * 1.4

Tick N+10: WarCry expires

  Target.Think:
    -> BuffTable.Tick(now):
      -> ModifierBuff.Think(ctx): expired -> return -1
      -> ModifierBuff.OnRemove(ctx):
         RemoveModifiersBySource(instID)
      -> ATK.dirty = true
      -> ATK.Current = ATK.Base * 1.0 (restored to no-buff state)
```

---

## 9. 接口草案

> 以下是 Go 代码草案，展示核心类型和方法签名。实际实现可能调整。

### 9.1 属性系统

```go
// ---- Type Definitions ----

type AttrKind int32
type ModOp int8

const (
    ModAdd      ModOp = iota
    ModMultiply
    ModOverride
)

type Modifier struct {
    Source  uint32   // associated buff instance ID
    Op      ModOp
    Value   float64
}

// AttrDef describes the static configuration of an attribute type.
type AttrDef struct {
    Kind     AttrKind
    ClampMin float64  // fixed lower bound (default math.Inf(-1))
    ClampMax float64  // fixed upper bound (default math.Inf(1))
    MaxAttr  AttrKind // dynamic upper bound attr (e.g. HP capped by MaxHP), 0 = unused
}

// ---- AttrTable ----

// AttrTable manages a set of attributes' Base / Current / Modifiers.
type AttrTable struct {
    defs   []AttrDef
    base   []float64
    cur    []float64
    mods   [][]Modifier  // mods[attrIndex] = modifier list for this attribute
    dirty  []bool
}

func NewAttrTable(defs []AttrDef) *AttrTable

// Read
func (t *AttrTable) Base(kind AttrKind) float64
func (t *AttrTable) Current(kind AttrKind) float64  // lazy recompute

// Direct Base modification (Instant Effect)
func (t *AttrTable) ModifyBase(kind AttrKind, delta float64)
func (t *AttrTable) SetBase(kind AttrKind, value float64)

// Modifier management (called by Buff.OnApply / Buff.OnRemove)
func (t *AttrTable) AddModifier(kind AttrKind, mod Modifier)
func (t *AttrTable) RemoveModifiersBySource(source uint32)  // batch cleanup when Buff is removed

// Recompute all dirty attributes
func (t *AttrTable) RecomputeAll()
```

### 9.2 Buff 系统

```go
// ---- Buff Interface ----

type Buff interface {
    ID() int32
    OnApply(ctx BuffCtx) int64   // first application, return first Think time (<0 = no Think)
    OnRemove(ctx BuffCtx)         // cleanup on removal
    OnStack(ctx BuffCtx) int64    // stack count changed, return new Think time (<0 = no change)
    Think(ctx BuffCtx) int64      // timer-driven, return next Think time (<0 = self-remove)
}

type BuffCtx struct {
    Now    int64
    Attrs  *AttrTable
    Tags   *TagState
}

// ---- Stacking Configuration ----

type StackType int8

const (
    StackNone     StackType = iota  // independent instances
    StackBySource                    // aggregate by source
    StackByTarget                    // aggregate by target (all sources share)
)

type ExpPolicy int8

const (
    ExpClearAll       ExpPolicy = iota // remove entire buff
    ExpRemoveOneStack                  // remove one stack
    ExpRefreshDuration                 // refresh duration (never truly expires)
)

type BuffDef struct {
    ID              int32
    StackType       StackType
    StackLimit      int32
    DurationRefresh bool
    ExpirationPolicy ExpPolicy
}

// ---- Stock Implementations ----

type ModTemplate struct {
    Attr     AttrKind
    Op       ModOp
    Value    float64   // per-stack value, effective = Value * StackCount
}

// NewModifierBuff creates a buff that registers attribute modifiers.
func NewModifierBuff(id int32, instID uint32, mods []ModTemplate, expireAt int64) Buff

// NewPeriodicBuff creates a buff that executes an action every period.
// action is called each period with BuffCtx; the buff self-removes after expireAt.
func NewPeriodicBuff(id int32, instID uint32, period int64, expireAt int64, action func(BuffCtx)) Buff

// NewTagBuff creates a buff that grants a tag for a duration.
func NewTagBuff(id int32, instID uint32, tagKey tag.Key, expireAt int64) Buff

// NewShieldBuff creates a damage-absorbing shield buff.
// absorbAmount is consumed by external damage application; self-removes when depleted.
func NewShieldBuff(id int32, instID uint32, absorbAmount float64, expireAt int64) Buff

// ---- BuffTable ----

type buffEntry struct {
    buff       Buff
    def        *BuffDef
    instanceID uint32
    source     uint64
    stackCount int32
}

type BuffTable struct {
    instances  []buffEntry
    heap       minHeap         // sorted by next wakeup time
    defs       map[int32]*BuffDef
    attrs      *AttrTable
    tags       *TagState
    nextInstID uint32
}

func NewBuffTable(attrs *AttrTable, tags *TagState) *BuffTable

// Tick drives all due Buff.Think calls (Think phase).
func (bt *BuffTable) Tick(now int64)

// Add creates or stacks a buff instance (Apply phase).
func (bt *BuffTable) Add(def *BuffDef, buff Buff, source uint64, duration int64)

// Remove removes a buff by type ID, calling OnRemove.
func (bt *BuffTable) Remove(buffID int32)

// RemoveByIDAndSource removes a specific source's buff instance.
func (bt *BuffTable) RemoveByIDAndSource(buffID int32, source uint64)

// NextWakeup returns the earliest wakeup time.
func (bt *BuffTable) NextWakeup() int64

// Query
func (bt *BuffTable) Has(buffID int32) bool
func (bt *BuffTable) StackCount(buffID int32) int32
```

### 9.3 能力系统

```go
type AbilityState int8

const (
    AbilityInactive AbilityState = iota
    AbilityActive
    AbilityCooldown
)

// AbilityDef: static configuration of an ability.
type AbilityDef struct {
    ID             int32
    CostAttr       AttrKind     // resource attribute to consume
    CostValue      float64      // consumption amount
    Cooldown       int64        // cooldown duration
    RequiredTags   []tag.Key    // tags required for activation
    BlockedTags    []tag.Key    // tags that block activation
    CancelTags     []tag.Key    // cancel other abilities with these tags on activation
    BlockTags      []tag.Key    // block abilities with these tags while active
    AbilityTag     tag.Key      // this ability's own tag (for being cancelled/blocked by others)
}

// AbilityInstance: runtime ability state.
type AbilityInstance struct {
    def       *AbilityDef
    state     AbilityState
    cdExpire  int64           // cooldown end time
    // users may embed custom fields
}

// ---- AbilitySet ----

type AbilitySet struct {
    abilities []AbilityInstance
    tags      *TagState        // reference
    attrs     *AttrTable       // reference
}

func NewAbilitySet(tags *TagState, attrs *AttrTable) *AbilitySet

func (as *AbilitySet) Register(def *AbilityDef)
func (as *AbilitySet) CanActivate(id int32, now int64) bool
func (as *AbilitySet) Activate(id int32, now int64) bool  // CanActivate + commit
func (as *AbilitySet) Cancel(id int32)
func (as *AbilitySet) IsActive(id int32) bool

// OnTagChanged: check if any active abilities should be cancelled.
func (as *AbilitySet) OnTagChanged(added, removed []tag.Key)

// NextWakeup: earliest cooldown expiry.
func (as *AbilitySet) NextWakeup() int64
```

### 9.4 Tag 状态

```go
// TagState wraps tag/ package, providing reference-counted tag management.
// Multiple buffs can grant the same tag; the tag only disappears when all grantors are removed.
type TagState struct {
    owned   map[tag.Key]int32  // tag -> reference count
    blocked map[tag.Key]int32  // blocked ability tags -> reference count
}

func (ts *TagState) Grant(k tag.Key)    // refcount++
func (ts *TagState) Revoke(k tag.Key)   // refcount--, remove if 0
func (ts *TagState) Has(k tag.Key) bool // refcount > 0
func (ts *TagState) HasAny(keys []tag.Key) bool
func (ts *TagState) HasAll(keys []tag.Key) bool

func (ts *TagState) Block(k tag.Key)
func (ts *TagState) Unblock(k tag.Key)
func (ts *TagState) IsBlocked(k tag.Key) bool
```

### 9.5 AbilitySystem 组装

```go
// AbilitySystem assembles four building blocks, providing unified Think/Apply interface.
type AbilitySystem struct {
    Attrs     *AttrTable
    Buffs     *BuffTable
    Abilities *AbilitySet
    Tags      *TagState
}

func NewAbilitySystem(attrDefs []AttrDef) *AbilitySystem

// Think phase: drive BuffTable.Tick + return nearest wakeup time.
func (gas *AbilitySystem) Tick(now int64)

// Ability activation
func (gas *AbilitySystem) CanActivate(id int32, now int64) bool
func (gas *AbilitySystem) Activate(id int32, now int64) bool

// Next wakeup time
func (gas *AbilitySystem) NextWakeup(now int64) int64
```

---

## 10. 开放问题

### 10.1 设计层面

| # | 问题 | 当前倾向 | 讨论点 |
|---|------|---------|--------|
| 1 | **Modifier Channel 数量**：初版是否只支持单通道？ | 初版单通道，预留扩展接口 | 多通道的实际需求在初期可能不明确 |
| 2 | **AttrTable 的 Public State 暴露方式**：World/staged view 如何读取 Current？ | 用户的 World 或 staged view 实现从 Logic 的 AttrTable 读取 | 需要与空间查询 API 一起设计 |
| 3 | **死亡判定的位置**：在 Apply 的 Flush 中？还是在 Think 中？ | Apply Flush 中检测 HP<=0，Emit 死亡 Signal | 需要明确死亡的 Signal/Effect 语义 |
| 4 | **AbilitySystem 应该有多"薄"**：是一个编排器还是仅仅持有引用？ | 薄层编排器，提供 Tick/Flush 但不限制用户的 Effect 定义 | 过厚会限制灵活性，过薄会让用户重复编写粘合代码 |
| 5 | **Buff 的 Value 与 StackCount 的关系**：固定 `value * stackCount`？还是允许自定义函数？ | 默认线性，允许用户传入自定义计算函数 | 某些游戏的堆叠有递减收益（如第 N 层只有 50% 效果） |
| 6 | **Buff 跨实体交互**：荆棘反伤等需要对攻击者产出 Effect 的 Buff 如何实现？ | 方案 A：Buff.Think 返回 action 描述列表，由 owner Logic 的 Think 转发为 Effect；方案 B：扩展 BuffCtx 提供 Publish 能力 | 方案 A 更纯粹（Buff 不直接接触 Scheduler 协议），方案 B 更方便 |
| 7 | **Buff 序列化/存档**：interface 类型的 Buff 如何序列化？ | 类型注册表 + Buff ID → factory 映射，序列化 Buff 的参数数据而非 interface 本身 | 需要约定 Buff 参数的序列化格式 |

### 10.2 实现层面

| # | 问题 | 备注 |
|---|------|------|
| 8 | **AttrTable 的索引方式**：int32 kind → 数组下标？还是 map？ | 属性数量通常 <50，dense array 更高效 |
| 9 | **BuffTable 的 min-heap 实现**：复用 `lib/` 中的数据结构？还是参考 GAS 的 HeapIndexMap？ | 需要支持 Update（修改 deadline）和 Remove（按 instanceID） |
| 10 | **TagState 与 tag/ 包的集成**：直接使用 tag.Tag？还是简化为 map[int32]int32？ | 需要层级匹配时用 tag/，只需精确匹配时用简单 map |
| 11 | **GAS 包的位置**：放 `gas/` 顶层？还是 `en/gas/`？ | 已倾向不放入 `game/` 基础库；完整 GAS 放 demo 层，基础库只保留 `attr/` |
| 12 | **泛型参数**：GAS 构建块是否需要泛型？ | AttrTable/BuffTable 可以是具体类型，AbilitySystem 可能需要泛型以适配用户的 Effect/Signal 类型 |
| 13 | **Stock Buff 的 PeriodicAction 回调签名**：action 函数是否需要返回值（如产出的 Effect 描述）？ | 与问题 6 相关——如果 Buff 只允许修改 owner，action 签名简单；如果允许跨实体交互，需要更丰富的返回类型 |

### 10.3 与其他系统的关系

| # | 问题 | 备注 |
|---|------|------|
| 14 | **空间查询 API**：World/Framework 的版本化空间索引接口影响 Target 选择的设计 | 目前 scheduler 只要求 Now()/Version()/Round()，空间索引与 WatchOf 应由上层 staged view 提供 |
| 15 | **弹道 Logic 模板**：弹道作为独立 Logic，其 spawn/fly/collide/destroy 生命周期与 GAS 的交互方式 | 在 tasks.md 中已有 backlog 条目 |
| 16 | **CC 效果标准化**：CC 的 Kind/Priority/Tenacity 体系 | 在 tasks.md 中已有 backlog 条目 |
| 17 | **行为树 (bt/) 与 GAS 的集成**：NPC AI 如何通过 AbilitySet 释放技能 | 行为树节点调用 AbilitySet.CanActivate/Activate |

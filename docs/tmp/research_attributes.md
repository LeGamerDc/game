# Unreal GAS 属性系统与 Modifier Pipeline 调研笔记

Last Updated: 2025-07-29

---

## 1. Unreal GAS 属性系统概述

### 1.1 核心概念

Unreal 的 Gameplay Ability System (GAS) 中，**Attribute（属性）** 是描述 Actor 当前状态的浮点值，例如 Health、MaxHealth、MovementSpeed、DamageResistance 等。属性通过 **AttributeSet** 组织，挂载在 **AbilitySystemComponent (ASC)** 上。

关键数据结构：

```text
FGameplayAttributeData
├── BaseValue    // 基础值：所有 Instant GE 修改的累积结果
└── CurrentValue // 当前值：BaseValue 经过所有活跃 Duration/Infinite GE Modifier 计算后的输出
```

### 1.2 Base Value vs Current Value

这是 GAS 属性系统最核心的二元模型：

| 概念 | 含义 | 谁修改它 | 类比 |
|------|------|----------|------|
| **Base Value** | 属性的"永久"值，是计算的输入 | Instant GE（直接修改）、Execution Calculation | 数据库中的原始记录 |
| **Current Value** | 属性的"运行时"值，是计算的输出 | 由引擎自动从 Base + 活跃 Modifiers 计算得出 | 视图层的派生值 |

**计算关系**：

```text
CurrentValue = f(BaseValue, ActiveModifiers_from_Duration/Infinite_GEs)
```

关键行为：
- **Instant GE** → 修改 BaseValue，然后重算 CurrentValue
- **Duration/Infinite GE** → 不改 BaseValue，只在 Modifier 列表中注册，参与 CurrentValue 的重算
- 当 Duration GE 过期或被移除 → 对应 Modifier 被移除 → CurrentValue 自动回退
- 这意味着 **Buff/Debuff 的"可撤销性"是通过 Base/Current 分离自动实现的**

**示例**：

```text
场景：角色 MaxHealth = 100 (Base)，装备一个 +20% MaxHealth 的 Infinite GE

Base  = 100 （不变）
Current = 100 * 1.2 = 120

卸下装备（移除 GE）：
Base  = 100 （不变）
Current = 100 （回到 Base，因为没有 Modifier 了）
```

### 1.3 AttributeSet 与 AbilitySystemComponent 的关系

```text
Actor
└── AbilitySystemComponent (ASC)
    ├── AttributeSet: HealthSet
    │   ├── Health
    │   ├── MaxHealth
    │   └── Damage (meta attribute)
    ├── AttributeSet: CombatSet
    │   ├── AttackPower
    │   ├── DamageResistance
    │   └── CritChance
    └── ActiveGameplayEffectsContainer
        ├── [GE_Spec_1: +20% MaxHealth, Infinite]
        ├── [GE_Spec_2: +10 AttackPower, Duration 30s]
        └── [GE_Spec_3: -50 Health, Instant → 已执行，不在容器中]
```

关键设计点：
- **一个 Actor 只有一个 ASC**（官方强烈建议）
- **AttributeSet 可以有多个**，按职责拆分（HealthSet、CombatSet、MovementSet 等）
- 多个 AttributeSet 共享同一个 ASC 的 ActiveGameplayEffectsContainer
- AttributeSet 既是数据容器，也是属性修改的**拦截器**（通过 Pre/Post 回调）

### 1.4 Meta Attribute 模式

GAS 中一个重要的实践模式是 **Meta Attribute**（元属性）：

```text
典型流程（Lyra/Fortnite 的 Damage 实现）：
1. GameplayEffect 携带 Execution Calculation
2. Execution 计算出最终伤害值，写入 Damage（meta attribute）
3. AttributeSet::PostGameplayEffectExecute() 检测到 Damage 被设置
4. 手动执行：Health = clamp(Health - Damage, 0, MaxHealth)
5. 重置 Damage = 0
```

Meta Attribute 的价值：
- 复杂计算（暴击、闪避、护甲减免、伤害类型）集中在 Execution Calculation 中
- AttributeSet 只做最终的 clamp 和副作用触发（如死亡判定）
- 解耦了"伤害公式"和"属性修改"

---

## 2. Modifier Pipeline 详解

### 2.1 Modifier 的基本操作

每个 GameplayEffect 可以携带 0 到多个 Modifier，每个 Modifier 指定：

| 字段 | 说明 |
|------|------|
| Attribute | 目标属性（如 Health, MaxHealth） |
| ModifierOp | 操作类型：Add / Multiply / Divide / Override |
| Magnitude | 数值来源（固定值 / ScalableFloat / SetByCaller / MMC / AttributeBased） |

### 2.2 单 Channel 聚合公式

**这是 GAS 属性计算的核心公式**，位于 `FAggregatorModChannel::EvaluateWithBase()`：

```text
CurrentValue = ((BaseValue + SumOfAdditives) * SumOfMultiplicatives) / SumOfDivisors
```

其中：
- `SumOfAdditives` = 所有 Add Modifier 值的直接求和
- `SumOfMultiplicatives` = 1.0 + Σ(each_multiply_mod - 1.0) = 1.0 + Σ(bias)
- `SumOfDivisors` = 1.0 + Σ(each_divide_mod - 1.0) = 1.0 + Σ(bias)
- Override 存在时，优先取最后应用的 Override 值作为起点

**计算顺序固定为：Override → Add → Multiply → Divide**

**关键细节：同类操作是"加法聚合"而非"乘法聚合"**：

```text
两个 Multiply 1.5 (+50%) 的效果：
  NOT: 1.5 * 1.5 = 2.25  （乘法聚合）
  YES: 1.0 + (0.5) + (0.5) = 2.0  （加法聚合，即 +100%）

这是有意设计：防止百分比 buff 堆叠过于爆炸。
但也因此，很多开发者觉得不够直观。
```

### 2.3 Modifier Evaluation Channels（多通道聚合）

为解决单一公式不够灵活的问题，GAS 提供了 **Mod Evaluation Channels** 机制（opt-in）：

```text
                    Channel 0          Channel 1          Channel 2
                   (Weapons)        (Equipment)       (Abilities)
                 ┌───────────┐    ┌───────────┐    ┌───────────┐
BaseValue ──────>│ Add/Mul/  │───>│ Add/Mul/  │───>│ Add/Mul/  │───> Final CurrentValue
                 │ Div/Over  │    │ Div/Over  │    │ Div/Over  │
                 └───────────┘    └───────────┘    └───────────┘
                   result_0          result_1          result_2
                 = f(Base)        = f(result_0)     = f(result_1)
```

每个 Channel 内部使用标准聚合公式，Channel 之间是**串行链式**传递：
- Channel 0 的输出作为 Channel 1 的 BaseValue
- Channel 1 的输出作为 Channel 2 的 BaseValue
- 最多支持 10 个 Channel

这使得：
- 同一 Channel 的同类 Modifier 仍然加法聚合（被"收拢"）
- 不同 Channel 的 Multiply Modifier 变成**乘法聚合**（相互"放大"）
- 设计师可以精确控制"武器加成"和"天赋加成"之间是相加还是相乘

**示例**：

```text
BaseValue = 100
Channel 0 (Weapons): Multiply 1.2 (+20%)   → 100 * 1.2 = 120
Channel 1 (Talents): Multiply 1.3 (+30%)   → 120 * 1.3 = 156

对比单 Channel: 100 * (1.0 + 0.2 + 0.3) = 150
```

### 2.4 Pre/Post 回调管线

AttributeSet 提供了一组回调函数，形成完整的修改拦截管线：

```text
GameplayEffect 尝试修改属性
        │
        ▼
┌─ PreGameplayEffectExecute() ─────────────────────────────┐
│  可以拒绝或修改提议的修改                                    │
│  只对 Instant / Periodic 执行的 GE 触发                     │
│  适合：伤害减免计算、免疫检查                                │
└──────────────────────────────────────────────────────────┘
        │
        ▼
┌─ PreAttributeBaseChange() ────────────────────────────────┐
│  Base Value 即将被修改时触发                                 │
│  适合：clamp Base Value（如 Health 不低于 0）                │
└──────────────────────────────────────────────────────────┘
        │
        ▼
     [Base Value 被修改]
        │
        ▼
┌─ PreAttributeChange() ────────────────────────────────────┐
│  Current Value 即将被修改时触发（包括 Base 变化导致的重算）     │
│  适合：防止 CurrentValue 超出范围                            │
│  注意：这里的修改不会回溯 Base                               │
└──────────────────────────────────────────────────────────┘
        │
        ▼
     [Current Value 被更新]
        │
        ▼
┌─ PostGameplayEffectExecute() ─────────────────────────────┐
│  Base Value 已被修改后触发                                   │
│  只对 Instant / Periodic 执行的 GE 触发                     │
│  适合：响应性逻辑（死亡判定、Meta Attribute 消费、UI 通知）     │
└──────────────────────────────────────────────────────────┘
        │
        ▼
┌─ PostAttributeChange() ──────────────────────────────────┐
│  属性值确实改变后触发                                        │
│  适合：级联约束（MaxHealth 改变后 re-clamp Health）           │
└──────────────────────────────────────────────────────────┘
```

### 2.5 Aggregator 与 Dirty 机制

GAS 内部为每个被 Modifier 影响的属性维护一个 **FAggregator**：

```text
FAggregator (per attribute)
├── BaseValue: float
├── ModChannels[]: FAggregatorModChannel
│   └── Mods[Add]: []  // 所有 Add modifier
│   └── Mods[Mul]: []  // 所有 Multiply modifier
│   └── Mods[Div]: []  // 所有 Divide modifier
│   └── Mods[Ovr]: []  // 所有 Override modifier
├── OnDirty: delegate  // 当 modifier 增删时触发重算
└── EvaluateWithBase() → float  // 执行公式计算
```

工作流程：
1. Duration/Infinite GE 被 Applied → Modifier 注册到 Aggregator 的对应 Channel/Op 列表
2. Aggregator 标记为 Dirty
3. OnDirty 触发 → `InternalUpdateNumericalAttribute()` → 重新 EvaluateWithBase
4. 新的 CurrentValue 写入 FGameplayAttributeData::CurrentValue
5. GE 被移除 → Modifier 从列表中删除 → 再次 Dirty → 重算

**这是一个纯函数式计算**：CurrentValue 完全由 BaseValue + 活跃 Modifier 列表决定，不依赖修改顺序。

### 2.6 GameplayEffect 的三种修改路径

```text
路径 1: Modifier（声明式）
  - GE 蓝图中配置 Attribute + Op + Magnitude
  - 引擎自动管理 Aggregator，自动重算
  - 支持预测（客户端预测）
  - 适合简单的加减乘除

路径 2: Execution Calculation（程序式）
  - C++ 类，可读取多个 Source/Target 属性
  - 可执行任意复杂公式
  - 输出 OutputModifier（仍然是 Attribute + Op + Value）
  - 只在 Instant / Periodic GE 中触发
  - 不支持客户端预测
  - 适合：伤害公式、治疗公式

路径 3: Modifier Magnitude Calculation (MMC)
  - 为单个 Modifier 提供动态数值
  - 可引用其他属性、Tags 等
  - 支持预测
  - 适合：基于等级/属性的动态倍率
```

### 2.7 Stacking 与 Modifier 的交互

GAS 的 Stacking 在 GE 层级管理（不在 Modifier 层级）：

| Stacking 策略 | 行为 | Modifier 影响 |
|--------------|------|--------------|
| Aggregate by Source | 同一 Source 的重复施加 → 增加 StackCount，不同 Source 独立 | Modifier magnitude 可随 StackCount 缩放 |
| Aggregate by Target | 无论 Source，Target 上只保持一个 Stack | 单一 Modifier 实例，magnitude 随 StackCount 变化 |
| 无 Stacking | 每次施加创建独立 GE 实例 | 各自独立的 Modifier，各自独立参与聚合 |

Stacking 与 Modifier 聚合的关系：
- StackCount 影响的是单个 GE 的 Modifier Magnitude
- 多个独立 GE 的 Modifier 仍然通过标准聚合公式合并

---

## 3. 与 Scheduler 对接的分析要点

### 3.1 Attribute 的 Public/Private 归属

**UE GAS 的假设**：所有属性修改都发生在**拥有 ASC 的 Actor 上**，由单线程同步执行。

**我们的 Scheduler 约束**：
- Think 阶段只读快照（WorldView）
- Apply 阶段修改自身 public state
- 跨实体写入通过 Effect 投递

**分析**：

| 属性类别 | 建议归属 | 理由 |
|---------|---------|------|
| Base Value | **Owner Private State** | 只有 owner 的 Apply 阶段才会修改它 |
| Active Modifier 列表 | **Owner Private State** | Buff 的增删是 owner Apply 阶段的内部事务 |
| Current Value (派生值) | **Public State（快照）** | 其他 entity 的 Think 需要读取（如 AI 决策读 target HP）|
| Meta Attribute (Damage 等) | **Owner Private State** | 临时计算中间量，外部不需要看到 |

**关键洞察**：GAS 的 Base/Current 分离天然适配 Scheduler 的 private/public 分离：
- Base + Modifiers 是 private 的"源数据"
- Current 是 public 的"对外视图"
- Apply 阶段修改 private state 后，更新 public state 的 CurrentValue 快照

### 3.2 Modifier Pipeline 应在哪个阶段执行

**Think 阶段**：
- 构建 Effect 意图（"我要对 target 施加 +20% AttackPower buff"）
- 读取 target 的 CurrentValue 快照做决策
- **不执行 Modifier 聚合计算**

**Apply 阶段**（推荐执行 Modifier Pipeline）：
- 接收 Effect → 解析为 Modifier 增删操作
- 更新 Active Modifier 列表（private state）
- 重算 CurrentValue = f(BaseValue, Modifiers)
- Pre/Post 回调在此阶段同步执行
- 更新 public state 快照

```text
Think (Source)                    Apply (Target)
┌─────────────────────┐          ┌──────────────────────────────────┐
│ 读取 Target 快照     │          │ 1. 接收 BuffEffect               │
│ 决策：施加 Buff      │ ──Effect──> │ 2. 注册 Modifier 到 Aggregator   │
│ 产出 BuffEffect     │          │ 3. PreGameplayEffectExecute()    │
│                     │          │ 4. 修改 BaseValue (if Instant)   │
│                     │          │ 5. 重算 CurrentValue             │
│                     │          │ 6. PostGameplayEffectExecute()   │
│                     │          │ 7. 更新 Public State 快照         │
└─────────────────────┘          └──────────────────────────────────┘
```

**Instant GE vs Duration GE 的 Scheduler 适配**：

| GE 类型 | UE 行为 | Scheduler 适配 |
|---------|---------|---------------|
| Instant | 直接改 Base，不留记录 | Apply 阶段直接修改 Base，重算 Current |
| Duration/Infinite | 注册 Modifier，不改 Base | Apply 阶段注册到 Modifier 列表，重算 Current |
| Periodic | 每个 Period 执行一次 Instant | Timer Wheel 触发 → 自发 Effect → Apply 处理 |

### 3.3 多个 Effect 同时修改同一属性：顺序无关适配

**GAS 的天然交换性**：

GAS 的 Aggregator 模型天然满足"顺序无关"——因为 CurrentValue 是从 Base + 所有 Active Modifiers **整体重算**的：

```text
场景：同一 tick 内 3 个 Effect 到达 target：
  Effect A: Add +10 to Attack
  Effect B: Multiply 1.2 to Attack
  Effect C: Add +5 to Attack

无论 A、B、C 以任何顺序被 Apply 处理：
  最终 Modifier 列表 = [Add: +10, +5] [Multiply: 1.2]
  CurrentValue = (Base + 10 + 5) * 1.2

结果完全一致。顺序无关是 Aggregator 模型的结构性保证。
```

**这与我们的 F4 "Effect 顺序无关（容忍性）" 原则完美契合**，而且比"容忍"更强——这里是数学上严格的交换性。

**但需注意的边界情况**：

1. **Instant GE 之间有依赖**：如果 Effect A 是 "Health -= 50" (Instant)，Effect B 是 "If Health < 30, apply Shield" (Instant with condition)，则 A 和 B 的顺序会影响 Shield 是否触发。
   - **GAS 的做法**：Instant GE 按到达顺序逐个执行，不保证跨 Actor 的全局顺序。
   - **我们的做法**：Apply 阶段可以对 Effect 按 Kind 排序（适配指南 D-2 策略），确保状态变更类先于条件检查类。

2. **Pre/Post 回调可能产生副作用**：PostGameplayEffectExecute 中的逻辑（如死亡判定）对执行顺序敏感。
   - **我们的做法**：Apply 阶段内部可以分两步——先执行所有 Modifier 注册/Base 修改，再统一触发 Post 回调。

3. **Override 冲突**：两个 Override Modifier 同时存在，GAS 取"最后应用的"。
   - **我们的做法**：如果多个 Override Effect 同 tick 到达，需要定义优先级规则（如按 Effect 优先级字段、或按 Source 优先级）。

### 3.4 架构映射建议

```text
UE GAS 概念                    → Scheduler 映射

AbilitySystemComponent         → Logic 内部的 AbilitySubsystem
AttributeSet                   → Logic 的 struct 字段（按职责分组）
FGameplayAttributeData          → { Base float64, Current float64 }
FAggregator                     → 每个属性一个 modifierList + recompute()
GameplayEffect (Duration)       → BuffEffect：携带 Modifier 定义 + Duration
GameplayEffect (Instant)        → DamageEffect / HealEffect：直接修改 Base
GameplayEffectSpec              → Effect 实例（携带 Source 快照、Magnitude 等）
Modifier                       → { Attr, Op(Add/Mul/Div/Override), Value }
Execution Calculation           → Apply 阶段的自定义计算函数
PreGameplayEffectExecute        → Apply 阶段的 pre-hook（可拒绝 Effect）
PostGameplayEffectExecute       → Apply 阶段的 post-hook（死亡判定等）
Mod Evaluation Channel          → Modifier 的 priority/layer 字段
Meta Attribute                  → Effect 中的临时计算字段（不暴露到 Public State）
```

### 3.5 对现有 Buff 系统的升级方向

我们当前的简单 Buff 系统支持 4 种组合模式（None/Add/Percent/Magnify）和 4 种堆叠策略。对照 GAS，建议的演进方向：

| 现有概念 | GAS 对应 | 建议改进 |
|---------|---------|---------|
| None | Override | 保留 |
| Add | Add (Additive) | 保留，语义一致 |
| Percent | Multiply (Additive aggregation) | 与 GAS 一致：同类百分比加法聚合 |
| Magnify | 多 Channel Multiply | 引入 Channel/Layer 概念实现乘法聚合 |
| 堆叠策略 | GE-level Stacking | 保留，但明确 Stacking 影响的是单 GE 的 Magnitude，不是 Aggregator |

**新增建议**：
- 引入 Base/Current 分离，让 Duration buff 自动可撤销
- 引入 Pre/Post Apply Hook，支持拦截和副作用
- 引入 Meta Attribute 模式，将伤害公式从属性系统中解耦
- 考虑 Modifier Channel（至少 2-3 层），解决"装备加成 vs 技能加成"的聚合控制需求

---

## 4. 参考链接

### 官方文档
- [Gameplay Attributes and Attribute Sets (Epic)](https://dev.epicgames.com/documentation/en-us/unreal-engine/gameplay-attributes-and-attribute-sets-for-the-gameplay-ability-system-in-unreal-engine)
- [Gameplay Effects (Epic)](https://dev.epicgames.com/documentation/en-us/unreal-engine/gameplay-effects-for-the-gameplay-ability-system-in-unreal-engine)
- [GAS Best Practices for Setup (Epic, Zhi Kang Shao)](https://dev.epicgames.com/community/learning/tutorials/DPpd/unreal-engine-gameplay-ability-system-best-practices-for-setup)
- [Modifier Evaluation Channels (Epic, Zhi Kang Shao)](https://dev.epicgames.com/community/learning/tutorials/JG2a/unreal-engine-gameplay-ability-system-modifier-evaluation-channels)

### 社区文档
- [tranek/GASDocumentation (GitHub)](https://github.com/tranek/GASDocumentation) — 社区公认的最全面 GAS 文档
- [GASDocumentation 中文翻译](https://github.com/BillEliot/GASDocumentation_Chinese)
- [Gameplay Ability System Course Project (DevinSherry)](https://forums.unrealengine.com/t/gameplay-ability-system-course-project-development-blog/1419542)

### 源码关键位置
- `FAggregatorModChannel::EvaluateWithBase()` — 单 Channel 聚合公式
- `FAggregatorModChannelContainer::EvaluateWithBase()` — 多 Channel 串行聚合
- `FActiveGameplayEffectsContainer::InternalUpdateNumericalAttribute()` — Dirty 触发重算
- `UAttributeSet::PreAttributeBaseChange()` / `PreAttributeChange()` — 修改前拦截
- `UAttributeSet::PostGameplayEffectExecute()` — 修改后回调

### 实践参考
- [A Gameplay Framework with GAS based on Risk of Rain 2](https://www.vitorcantao.com/post/gas-gameplay-framework/) — 良好的 GAS 实战架构参考
- [How to Properly Implement GAS in UE5 (Deep Haul Game)](https://deephaulgame.com/blog/2026/03/how-to-properly-implement-gas-in-ue5-the-setup-guide-i-wish-i-had.html)
- [Stack Overflow: GAS Aggregator 详解](https://stackoverflow.com/questions/52916274/unreal-gas-influence-of-the-gameplayeffect-aggregator-on-gameplay-attribute-val)
# UE5 GameplayEffect 系统调研笔记

> 调研目标：为 Go 游戏服务器框架的 GAS 设计提供参考，重点关注 Duration、Period、Stacking 模型及其与 Scheduler（Think/Apply + Timer Wheel）的映射关系。

---

## 1. GameplayEffect 持续类型

UE5 的 GameplayEffect (GE) 有三种 Duration Policy：**Instant**、**Has Duration**、**Infinite**。

### 1.1 Instant

- **行为**：立即执行，不会进入 Active Gameplay Effects Container（ASC 的活跃效果列表）。
- **对属性的影响**：修改 Attribute 的 **BaseValue**（永久性修改）。
- **生命周期**：Apply 即 Execute，没有 Handle，无法被追踪或移除。
- **典型场景**：
  - 伤害（HP -= damage）
  - 治疗（HP += heal）
  - 属性初始化（设置角色初始 HP/MaxHP）
  - 经验值/金币获取
- **GameplayCue 事件**：触发 `Execute`（一次性视觉/音效反馈）。
- **Tag 行为**：**不能** Grant Tags（因为没有持续期）。
- **关键设计点**：Instant GE 本质上是一个 "立即执行的函数调用"，修改完 BaseValue 就消失。

### 1.2 Has Duration

- **行为**：添加到 Active Gameplay Effects Container，持续指定时长后自动移除。
- **对属性的影响**：修改 Attribute 的 **CurrentValue**（临时性修改）。移除时效果自动回退。
- **Attribute 双值模型**：
  - `BaseValue`：永久值，只被 Instant GE 和 Periodic GE 修改。
  - `CurrentValue`：= BaseValue + 所有活跃 Duration/Infinite GE 的 Modifier 聚合结果。
  - 当 Duration GE 移除时，其 Modifier 从聚合中移除，CurrentValue 自动回到正确值。
- **典型场景**：
  - 移速 buff（+50% 移速持续 5 秒）
  - 护盾（临时增加护甲值 10 秒）
  - 冷却（Cooldown GE 本质上就是 Duration GE + Cooldown Tag）
  - 短时控制效果（眩晕 3 秒）
- **GameplayCue 事件**：`Add`（添加时）+ `Remove`（移除时）。
- **Tag 行为**：可以 Grant Tags，移除时 Tags 自动移除。

### 1.3 Infinite

- **行为**：与 Has Duration 几乎相同，但 **没有预设的到期时间**，必须手动移除或由条件触发移除。
- **对属性的影响**：同 Has Duration，修改 CurrentValue。
- **典型场景**：
  - 装备属性加成（穿上装备 → Apply Infinite GE，脱下 → Remove）
  - 光环 / 区域效果（进入区域 Apply，离开 Remove）
  - 被动技能效果
  - 派生属性（用 Infinite GE + Attribute Based Modifier 实现属性间依赖关系）
- **GameplayCue 事件**：同 Has Duration。
- **本质**：Infinite = "Duration 为 ∞ 的 Has Duration"。

### 1.4 三种类型对比

```
+----------+------------+----------------+----------+--------+-----------+
| Policy   | BaseValue  | CurrentValue   | 进入 ASC | 自动   | Grant     |
|          | 修改       | 修改           | 容器     | 过期   | Tags      |
+----------+------------+----------------+----------+--------+-----------+
| Instant  | Yes        | No (直接改Base)| No       | N/A    | No        |
| Duration | No         | Yes            | Yes      | Yes    | Yes       |
| Infinite | No         | Yes            | Yes      | No     | Yes       |
+----------+------------+----------------+----------+--------+-----------+
```

---

## 2. 周期性效果机制 (Periodic Effects)

### 2.1 基本概念

Duration 和 Infinite GE 可以设置一个 **Period**（周期），使效果每隔固定时间执行一次。

- 设置 Period 后，GE 在每个周期到达时执行一次，**等同于一次 Instant GE 的执行**。
- 因此 Periodic GE 修改的是 **BaseValue**（永久修改），而不是 CurrentValue。
- 这与普通 Duration GE（修改 CurrentValue，移除时回退）形成对比。

### 2.2 语义

```
Periodic GE = 一个 "容器" + 内嵌的周期性 Instant 执行

Duration GE (Period=2s, Duration=10s):
  t=0:  GE Added，执行第一次 (修改 BaseValue)
  t=2:  Period tick → Execute (修改 BaseValue)
  t=4:  Period tick → Execute (修改 BaseValue)
  t=6:  Period tick → Execute (修改 BaseValue)
  t=8:  Period tick → Execute (修改 BaseValue)
  t=10: GE Removed (Duration 到期)
  总共执行 5 次
```

### 2.3 设计要点

- **Period 首次执行**：可以配置是否在 Apply 时立即执行第一次（`bExecutePeriodicEffectOnApplication`）。
- **Period 与 Stacking 交互**：Stack 新增时可以选择是否 Reset Period（`StackPeriodResetPolicy`）。
- **Periodic GE 不可预测**（Prediction）：客户端不能预测周期性执行，只能由服务器驱动。
- **Execution Calculation**：只能用于 Instant 和 Periodic GE（因为两者都是 "Execute" 语义）。
- **GameplayCue**：每次 Period tick 触发 `Executed` 事件（不是 Add/Remove）。

### 2.4 典型场景

| 场景 | Duration | Period | 效果 |
|------|----------|--------|------|
| 中毒 DOT | 10 秒 | 2 秒 | 每 2 秒扣一次 HP（BaseValue），10 秒后毒效果消失，但已扣的 HP 不恢复 |
| 持续回血 HOT | 15 秒 | 3 秒 | 每 3 秒加一次 HP |
| 自然法力恢复 | Infinite | 1 秒 | 每秒恢复法力，直到被移除 |
| 光环伤害 | Infinite | 0.5 秒 | 每 0.5 秒对范围内敌人造成伤害 |

### 2.5 与非 Periodic Duration GE 的关键区别

```
非 Periodic Duration GE (移速 +50%, 10秒):
  → 修改 CurrentValue，移除时自动回退

Periodic Duration GE (每 2 秒扣 10 HP, 10秒):
  → 每次 tick 修改 BaseValue，移除时已造成的伤害不会回退
  → 但 GE 本身作为容器被移除后，不再有新的 tick
```

---

## 3. Stacking 模型详解

### 3.1 Stacking Type（堆叠类型）

UE5 提供三种 Stacking 类型：

| 枚举值 | 含义 | 说明 |
|--------|------|------|
| `None` | 不堆叠 | 每次 Apply 创建独立实例，各自有独立计时器。这是默认行为。 |
| `AggregateBySource` | 按来源聚合 | 每个 Source ASC 在 Target 上维护各自的堆栈。不同来源互不影响。 |
| `AggregateByTarget` | 按目标聚合 | Target 上只有一个堆栈实例，无论来源是谁。所有 Source 共享同一个 Stack Count。 |

**注意**：Stacking 只对 Duration 和 Infinite GE 有效，Instant GE 无需堆叠。

#### None 模式（默认）

- 每次 Apply 产生一个独立的 ActiveGameplayEffect 实例。
- 各实例独立计时、独立到期。
- 查询 "stack count" 会返回所有同类实例的总数。
- **适用**：需要独立追踪每个效果来源和剩余时间的场景。

#### AggregateBySource

- 同一个 Source 对同一个 Target 重复 Apply 同一种 GE → 累加 Stack Count。
- 不同 Source 各自维护独立的堆栈。
- **适用**：每个施法者对目标的效果独立追踪，如 "A 的毒叠了 3 层，B 的毒叠了 2 层"。

#### AggregateByTarget

- 无论来源，Target 上同一种 GE 只有一个堆栈实例。
- 任何 Source 的 Apply 都会增加共享的 Stack Count。
- **适用**：目标上的效果统一管理，如 "护甲层数"、"流血层数" 不区分来源。

### 3.2 Stack Limit

- `StackLimitCount`：最大堆叠层数。为 0 表示无限制。
- 达到上限后继续 Apply → 触发 Overflow 逻辑。

### 3.3 Stack Duration Refresh Policy（时长刷新策略）

当新的一层 Stack 被成功 Apply 时，已有堆栈的剩余时长如何处理：

| 策略 | 行为 |
|------|------|
| `RefreshOnSuccessfulApplication` | 每次成功添加新 Stack 时，**重置**整个效果的剩余时长为初始时长。 |
| `NeverRefresh` | 新 Stack 添加不影响剩余时长，效果按原始计时到期。 |

### 3.4 Stack Period Reset Policy（周期重置策略）

当新的一层 Stack 被成功 Apply 时，Periodic 效果的下一次 tick 计时如何处理：

| 策略 | 行为 |
|------|------|
| `ResetOnSuccessfulApplication` | 重置 Period 计时器，从头开始计算下一次 tick。 |
| `NeverReset` | 不影响 Period 计时，继续当前的周期倒计时。 |

### 3.5 Stack Expiration Policy（到期策略）

当效果的 Duration 到期时，堆栈如何处理：

| 策略 | 行为 |
|------|------|
| `ClearEntireStack` | 整个堆栈全部清除，效果完全移除。 |
| `RemoveSingleStackAndRefreshDuration` | 移除一层 Stack，剩余层数刷新时长继续生效。重复直到 Stack Count 归零。 |
| `RefreshDuration` | 时长到期时不移除任何 Stack，直接刷新时长继续（实际上效果永不到期，除非手动移除或 Stack Count 因其他原因归零）。 |

### 3.6 Overflow 处理

当 Stack Count 已达 `StackLimitCount`，再次尝试 Apply：

| 配置项 | 行为 |
|--------|------|
| `OverflowEffects` | 溢出时额外 Apply 的 GE 列表（例如毒素累积满后触发一个伤害爆发 GE） |
| `bDenyOverflowApplication` | 如果为 true，溢出时拒绝 Apply（不刷新时长和上下文） |
| `bClearStackOnOverflow` | 如果为 true，溢出时清除整个堆栈 |

**Overflow 典型场景**：毒素蓄积 → 达到阈值后溢出 → 触发一个 DOT 效果，同时清空毒素层数。

### 3.7 与我们参考实现的对比

我们的 `docs/references/reactive_notifications_formal_models.md` 中提到了几种 Buff Stack 策略：

| 我们的概念 | UE5 对应 | 说明 |
|-----------|----------|------|
| `BuffStackGreater`（取最大值） | Stacking=None + AggregatorEvaluateMetaData（只取最大负 Modifier） | UE5 的 Paragon 做法：多个同类减速各自独立存在，但聚合时只取最强的一个生效。通过 `OnAttributeAggregatorCreated` 配置。 |
| `BuffStackLonger`（取最长时长） | 无直接对应；可用 CustomApplicationRequirement 实现 | 需要自定义逻辑：比较新旧效果时长，保留更长的。 |
| `BuffStackSeparate`（独立叠加） | Stacking=None（默认行为） | 每个实例独立，各自计时。 |
| 层数叠加 | AggregateByTarget + StackLimitCount | 统一管理层数，Modifier 值 = 基础值 × Stack Count。 |
| 按来源独立追踪 | AggregateBySource | 区分不同来源的叠加层数。 |

**关键差异**：
- UE5 的 Stacking 是 GE 配置级别的（数据驱动），不需要运行时代码区分。
- 我们的 BuffStackGreater/Longer 语义在 UE5 中需要组合使用多个机制实现。
- UE5 的 AggregatorEvaluateMetaData 提供了"多个效果同时存在，但只有最强的生效"的能力，这是一个独立于 Stacking 的概念。

---

## 4. Effect 生命周期

### 4.1 创建与 Apply

```
GameplayEffect (Class/Blueprint, 不可变资产)
        |
        v
GameplayEffectSpec (运行时实例化数据)
  - 引用 GE Class
  - Level, Source/Target 信息
  - SetByCaller 值 (TMap<GameplayTag, float>)
  - 捕获的 Attribute 快照
  - 动态 Tags
        |
        v
ApplyGameplayEffectSpecToSelf()
        |
        +--[Instant]--> Execute → 修改 BaseValue → 结束（不追踪）
        |
        +--[Duration/Infinite]--> 创建 FActiveGameplayEffect
                                    → 加入 ActiveGameplayEffects 容器
                                    → 开始计时 / 周期执行
```

### 4.2 添加 (Application)

1. **检查 Application Tag Requirements**：Target 必须满足 Tag 条件。
2. **检查 Immunity**：Target 可能通过 Immunity Tags 阻止特定 GE。
3. **检查 CustomApplicationRequirement**：自定义 C++ 逻辑判断是否允许 Apply。
4. **检查 ChanceToApply**：概率性 Apply。
5. **Stacking 判断**：
   - 如果 Stacking=None → 创建新实例。
   - 如果 AggregateBySource/Target → 查找已有匹配堆栈 → 增加 Stack Count 或创建新堆栈。
   - 如果已达 StackLimit → 触发 Overflow 逻辑。
6. **执行 Modifiers**：将 Modifier 注册到 Attribute Aggregator。
7. **Grant Tags**：将 Granted Tags 添加到 Target ASC。
8. **触发 Remove 逻辑**：按 "Remove Gameplay Effects with Tags" 配置移除冲突效果。
9. **Grant Abilities**：如果配置了 Granted Abilities，授予对应的 GameplayAbility。
10. **触发 GameplayCue**：Add 事件。

### 4.3 移除 (Removal)

移除方式有多种：

| 触发方式 | 说明 |
|---------|------|
| Duration 自然到期 | 计时器到期自动移除 |
| 手动调用 `RemoveActiveGameplayEffect` | 代码主动移除（通过 Handle 或查询条件） |
| Tag 条件不满足 | Ongoing Tag Requirements 不满足时效果暂时关闭（不移除）；Target Tag Requirements 配置了 Removal 条件时实际移除 |
| 被其他 GE 移除 | 新 Apply 的 GE 配置了 "Remove Gameplay Effects with Tags" |
| Stack Count 归零 | StackExpirationPolicy = RemoveSingleStackAndRefreshDuration 循环到最后一层时 |
| Overflow Clear | bClearStackOnOverflow = true 时 |

移除时发生的操作：
1. 撤销 Modifier（从 Attribute Aggregator 中移除），CurrentValue 自动回退。
2. 移除 Granted Tags。
3. 根据 Removal Policy 处理 Granted Abilities（立即取消 / 允许结束 / 保留）。
4. 触发 GameplayCue Remove 事件。
5. 可选地 Apply `PrematureExpirationEffectClasses`（非正常到期时）或 `RoutineExpirationEffectClasses`（正常到期时）。

### 4.4 Ongoing Tag Requirements（条件性开关）

- Duration/Infinite GE 可以配置 Ongoing Tag Requirements。
- 当 Target 的 Tag 状态不满足条件时，效果 **暂时关闭**（Modifier 移除，Granted Tags 移除），但效果本身 **不从容器中移除**。
- 当条件重新满足时，效果 **重新开启**（Modifier 重新注册，Granted Tags 重新添加）。
- 这是一个非常灵活的条件系统，例如 "只在持有武器时生效的加成"。

### 4.5 生命周期回调

```
PreAttributeChange(Attribute, NewValue)
  → CurrentValue 变化前，用于 clamp
  → 不触发游戏逻辑

PreGameplayEffectExecute(Data)
  → BaseValue 被 Instant/Periodic GE 修改前
  → 可以拒绝或修改提议的变更

PostGameplayEffectExecute(Data)
  → BaseValue 修改后
  → 适合做：死亡判定、经验分配、伤害数字显示
  → 最终 clamp 值的推荐位置

OnActiveGameplayEffectAdded
  → Duration/Infinite GE 被添加时（委托）

OnAnyGameplayEffectRemoved
  → 任何 Duration/Infinite GE 被移除时（委托）
```

---

## 5. 与 Scheduler 的映射分析

### 5.1 核心约束回顾

我们的 Scheduler 模型：
- **Think 阶段**：Logic 只读 WorldView，产出 typed Effect 和 Signal。
- **Apply 阶段**：Owner 接收 Effect 修改自身状态。
- **Timer Wheel**：支持定时唤醒 Logic.Think。
- **Running**：持续运行的运行时实体（类似持续效果）。
- **跨实体通信**：Signal（触发 Think）+ Effect（触发 Apply）。

### 5.2 Instant GE → 我们的 typed Effect

映射关系最直接。

```
UE5:  Ability → Apply Instant GE → BaseValue 修改

Ours: Logic.Think() → 产出 DamageEffect{Target, Value}
      → Apply 阶段 Target.Apply(DamageEffect) → 修改 HP
```

- Instant GE 在 UE5 中就是 "fire and forget"，等同于我们的 typed Effect。
- 语义完全匹配：产出一个数据、立即消费、不追踪生命周期。
- **结论：Instant GE = 我们的 Effect。直接对应。**

### 5.3 Duration/Infinite GE → Running + Timer Wheel 驱动

这是最需要设计的部分。

```
UE5:  Apply Duration GE (10s, 移速+50%)
      → 进入 Active GE 容器
      → 持续影响 CurrentValue
      → 10s 后自动移除

Ours: 方案 A - Apply 端维护
      Logic.Think() → 产出 ApplyBuffEffect{Kind: SpeedBuff, Duration: 10s, Value: +50%}
      → Apply 阶段 Owner 将 Buff 加入内部 BuffList
      → Timer Wheel 注册 10s 后唤醒 Owner
      → 10s 后 Owner.Think() 检查并清理过期 Buff

      方案 B - Running 概念
      Logic.Think() → Spawn 一个 SpeedBuffRunning
      → Running 持续存在，在其 Think() 中检查到期
      → 到期时产出 RemoveBuffEffect → Owner.Apply() 移除 Buff
```

**分析**：

| 维度 | 方案 A (Apply 端管理) | 方案 B (Running) |
|------|----------------------|-----------------|
| 实现位置 | Owner 内部逻辑 | 独立 Running 实体 |
| 计时机制 | Timer Wheel 唤醒 Owner | Timer Wheel 唤醒 Running |
| Modifier 管理 | Owner.Apply() 内部 BuffList | Running 存在时 Owner 从 BuffList 读 |
| 跨实体交互 | 不需要（Owner 自管理） | Running → Owner 需要 Effect/Signal |
| 复杂度 | 低，适合简单 buff | 高，适合需要独立逻辑的效果 |
| Ongoing Conditions | Owner.Think() 中检查 | Running.Think() 中检查 |

**建议**：
- **简单的属性 Modifier 型 Buff**（移速加成、攻击力加成）→ 方案 A，Owner Apply 端直接管理 BuffList。
- **需要独立行为逻辑的效果**（持续追踪目标的光环、需要条件判断的复杂效果）→ 方案 B，使用 Running。

### 5.4 Periodic GE → Timer Wheel 驱动

```
UE5:  Apply Periodic GE (Period=2s, Duration=10s, -10 HP per tick)
      → 每 2s 执行一次 Instant 效果

Ours: 
      方案 A: Owner Apply 端实现
      Logic.Think() → 产出 ApplyDOTEffect{Period: 2s, Duration: 10s, Damage: 10}
      → Owner.Apply() 注册到内部 DOT 列表 + Timer Wheel 每 2s 唤醒
      → 每次唤醒时 Owner.Think() 生成 DamageEffect 对自己
      → 10s 后清除 DOT 条目

      方案 B: Running 实现
      → Spawn DOTRunning, 2s Timer tick
      → 每次 tick DOTRunning.Think() → 产出 DamageEffect → Target.Apply()
      → 10s 后 DOTRunning 销毁
```

**分析**：Periodic GE 在 UE5 中本质是 "容器 + 定时触发 Instant Execute"。
- 如果使用 Timer Wheel，自然对应为周期性唤醒。
- Running 方案更清晰：DOT Running 自己管理周期和生命周期。
- **建议：Periodic Effect 优先考虑 Running + Timer Wheel 方案**，因为：
  - 周期执行需要独立的时间追踪
  - DOT/HOT 可能需要独立的 Source 信息（谁施加的）
  - Stacking 场景下每个 Source 的 Periodic 需要独立计时

### 5.5 Stacking → Apply 端逻辑

Stacking 是 **Apply 端** 的职责：

```
Owner.Apply(ApplyBuffEffect):
  1. 查找 BuffList 中是否已有同类型 Buff
  2. 根据 StackingType 决定：
     a. None → 添加新独立实例
     b. AggregateBySource → 按 Source 查找，找到则累加 StackCount
     c. AggregateByTarget → 全局查找，找到则累加 StackCount
  3. 检查 StackLimit → 处理 Overflow
  4. 根据 DurationRefreshPolicy 决定是否重置计时器
  5. 根据 PeriodResetPolicy 决定是否重置周期计时器
  6. 更新 Modifier 聚合值
```

**关键设计点**：
- Stacking 逻辑完全在 Apply 阶段执行，不需要 Think 阶段参与。
- StackCount 影响 Modifier 的最终值（通常为 BaseValue × StackCount）。
- Overflow 处理可以产出新的 Effect（类似 UE5 的 OverflowEffects）。

### 5.6 Ongoing Tag Requirements → WatchState + Signal

UE5 的 Ongoing Tag Requirements（条件性开关效果）映射：

```
UE5: Buff 只在 Target 持有 "HasWeapon" Tag 时生效

Ours:
  - Owner 的 Tag 变化通过 WatchState 广播
  - 关注 Tag 变化的 Buff 通过 Signal 通知 Owner 重新评估
  - 或者简单地在 Owner.Think() 中每 tick 检查条件
    （因为 Buff 是 Owner 内部管理的，不需要跨实体通信）
```

### 5.7 映射总结

```
+---------------------+---------------------------+------------------------+
| UE5 概念            | 我们的对应                 | 驱动机制               |
+---------------------+---------------------------+------------------------+
| Instant GE          | typed Effect              | Think → Apply          |
| Duration GE         | BuffList (Apply 端) 或    | Timer Wheel 唤醒       |
|                     | Running (独立实体)         |                        |
| Infinite GE         | BuffList (无到期) 或      | 手动 Remove            |
|                     | Running (持续存在)         |                        |
| Periodic GE         | Running + Timer tick      | Timer Wheel 周期唤醒   |
| Stacking            | Apply 端 BuffList 逻辑    | Apply 阶段处理         |
| Modifier 聚合       | Apply 端 Attribute 计算   | Apply 阶段计算         |
| Ongoing Conditions  | WatchState / Think 检查   | Signal 或 tick 检查    |
| GameplayCue         | 不适用(客户端表现层)       | N/A                    |
| GE Spec             | typed Effect 数据 + 元数据| Effect 结构体          |
| Active GE Container | Owner 内部 BuffList       | Owner 私有状态         |
+---------------------+---------------------------+------------------------+
```

### 5.8 设计建议

1. **Effect 类型分层**：
   - `InstantEffect`：立即消费型（伤害、治疗），对应 Instant GE。
   - `BuffEffect`：请求添加/移除/刷新 Buff 的 Effect，Apply 端处理堆叠和生命周期。
   - 不需要 Duration/Infinite 的区分 — 我们用 `Expire` 字段（0 表示 Infinite）统一。

2. **Buff 管理在 Owner Apply 端**：
   - BuffList 是 Owner 的私有状态，不需要跨实体可见。
   - Modifier 聚合在 Apply 端计算，每 tick 重新计算 CurrentValue = Base + Aggregated Modifiers。
   - Timer Wheel 管理 Buff 到期和 Periodic tick。

3. **Stacking 作为 BuffList 的配置**：
   - 每种 Buff 类型定义自己的 Stacking 策略（配置数据，不是运行时代码）。
   - Apply 端根据策略处理 Stack 逻辑。

4. **复杂效果升级为 Running**：
   - 当 Buff 需要独立的 Think 逻辑时（如追踪目标、条件判断、与其他实体交互），升级为 Running。
   - Running 的 Think 产出 Effect 来修改 Owner 状态。

---

## 6. 参考链接

### 官方文档
- [Gameplay Effects - Epic Official Docs](https://dev.epicgames.com/documentation/en-us/unreal-engine/gameplay-effects-for-the-gameplay-ability-system-in-unreal-engine)
- [Your First 60 Minutes with GAS](https://dev.epicgames.com/community/learning/tutorials/8Xn9/unreal-engine-epic-for-indies-your-first-60-minutes-with-gameplay-ability-system)
- [GAS Best Practices for Setup](https://dev.epicgames.com/community/learning/tutorials/DPpd/unreal-engine-gameplay-ability-system-best-practices-for-setup)

### 社区文档
- [tranek/GASDocumentation](https://github.com/tranek/GASDocumentation) — 最全面的 GAS 第三方文档，Epic 官方推荐
- [Making Sense of Gameplay Effect Durations (Quod Soler)](https://www.quodsoler.com/blog/making-sense-of-gameplay-effect-durations) — Duration Policy 的通俗解释
- [GAS Companion API - GameplayEffect Template](https://gascompanion.github.io/api/Templates/GSCTemplate_GameplayEffectDefinition/) — 所有 GE 属性的完整列表和说明

### API 参考
- [UGameplayEffect API](https://dev.epicgames.com/documentation/en-us/unreal-engine/API/Plugins/GameplayAbilities/UGameplayEffect)
- [EGameplayEffectStackingType](http://dev.epicgames.com/documentation/en-us/unreal-engine/python-api/class/GameplayEffectStackingType?application_version=5.7) — None / AggregateBySource / AggregateByTarget
- [EGameplayEffectStackingPeriodPolicy](https://dev.epicgames.com/documentation/en-us/unreal-engine/API/Plugins/GameplayAbilities/EGameplayEffectStackingPeriodPol-) — ResetOnSuccessfulApplication / NeverReset

### 论坛讨论
- [Turn-Based GAS Duration Discussion](https://forums.unrealengine.com/t/how-to-think-about-time-based-aspects-of-gameplay-ability-system-in-terms-of-turn-based-instead/451100) — 将 GAS 时间机制适配到非实时系统的讨论
- [Stacking Duration Effects with Individual Timers](https://www.reddit.com/r/UnrealEngine5/comments/1ajgr2w/need_help_with_gas_duration_effects/) — 独立计时的堆叠效果实现
- [Dave Ratti Q&A (Epic Engineer)](https://epicgames.ent.box.com/s/m1egifkxv3he3u3xezb9hzbgroxyhx89) — GAS 架构师的设计思路和未来方向
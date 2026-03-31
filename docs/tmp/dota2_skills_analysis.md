# DOTA2 技能/机制 × 并行 Tick 调度框架适配分析

> 分析目标：判断 10 个代表性 DOTA2 技能/机制能否适配到基于 ownership 的并行 tick 调度框架。
>
> 框架权威接口：`en/world.go`
>
> 框架设计文档：`docs/design/parallel.md`

---

## 目录

1. [Invoker - Invoke 系统](#1-invoker祈求者---invoke-系统)
2. [Rubick - Spell Steal](#2-rubick拉比克---spell-steal法术窃取)
3. [Faceless Void - Chronosphere](#3-faceless-void虚空假面---chronosphere时间结界)
4. [Pudge - Meat Hook](#4-pudge帕吉---meat-hook肉钩)
5. [Oracle - False Promise](#5-oracle神谕者---false-promise虚妄之诺)
6. [Morphling - Attribute Shift](#6-morphling变体精灵---attribute-shift属性转换)
7. [Meepo - Divided We Stand](#7-meepo米波---divided-we-stand个体分裂)
8. [Io - Tether](#8-io艾欧---tether羁绊)
9. [Phoenix - Supernova](#9-phoenix凤凰---supernova超新星)
10. [Techies - Proximity Mines](#10-techies工程师---proximity-mines感应地雷)
11. [统计摘要](#统计摘要)

---

## 1. Invoker（祈求者） - Invoke 系统

**描述**：Invoker 拥有三个球技能（Quas/Wex/Exort），每个可升级至 7 级。当前激活的三个球的组合决定了 Invoke 可以合成的法术（共 10 种）。同时，当前激活的球会提供持续性属性加成（如 Quas 提供生命恢复，Wex 提供攻速/移速，Exort 提供攻击力）。切换球会实时改变属性加成。

**适配判定**：✅ 直接适配

**Owner 归属**：Invoker 自身 Logic（单一 owner）

**执行流程映射**：

- **Think 阶段**：
  - 读取 private state 中的当前球组合（如 QQW）、各球等级、已合成的两个技能槽
  - 当收到玩家输入 Signal（切球/Invoke）时，在 private state 中更新球组合或替换技能槽
  - 计算新的属性加成值，产出 Effect 修改自身 public state
  - 如果释放已合成的技能，按正常技能流程产出对应 Effect
- **Effect 产出**：
  - `AttributeModifyEffect` → 自身（更新球提供的属性加成）
  - 具体法术的 Effect → 对应目标（如 Sun Strike 对区域目标的伤害 Effect）
- **Apply 阶段**：
  - 自身 Apply 接收 `AttributeModifyEffect`，更新 public state 中的属性加成
- **Signal 产出**：
  - 切球/Invoke 操作可产出 `OrbChangedSignal` 供 UI/音效等外围系统消费
- **Timer 使用**：
  - Invoke CD 管理（private state 中的 CD 计时器）
  - 球属性加成可以是持续 buff，通过 Think 返回 delay 定期刷新（或直接在切球时一次性更新）

**触及的约束**：

- **C1**：完美契合。所有状态变更（球组合、技能槽、属性加成）都归属 Invoker 单一 owner
- **C6**：Invoker 不太可能同时收到冲突的自身属性 Effect，无序安全天然满足

**适配模式**：**P1 单 owner 状态机** — 球组合和技能槽完全在 private state 运行，属性加成通过 Effect→Apply 更新 public state

---

## 2. Rubick（拉比克） - Spell Steal（法术窃取）

**描述**：Rubick 对敌方英雄释放 Spell Steal，偷取目标最近一次使用的主动技能。偷取后 Rubick 可以使用该技能（保留原始等级和大部分行为），直到偷取另一个技能将其替换。被偷取的技能会继承 Rubick 自身的施法动画和绿色视觉效果。

**适配判定**：⚠️ 需要妥协

**Owner 归属**：

- 偷取行为的真相 owner：Rubick Logic
- 被偷取技能的"最近施放记录"：目标英雄 Logic 的 public state

**执行流程映射**：

- **Think 阶段**：
  - Rubick Think 读取世界快照中目标英雄的 public state，获取 `lastCastAbilityID` 字段
  - 将该技能 ID 写入 Rubick private state 的"被偷技能槽"
  - 初始化被偷技能的 private state（CD、等级等），从目标的 public state 中读取技能等级
- **Effect 产出**：
  - `StealCompleteEffect` → 自身（确认技能槽更新，更新 public state 中的可用技能列表）
  - 被偷技能释放时，按正常技能流程产出对应 Effect
- **Apply 阶段**：
  - Rubick Apply 处理 `StealCompleteEffect`，更新 public state（对外展示当前拥有的被偷技能）
- **Signal 产出**：
  - `SpellStolenSignal` → 目标（通知被偷，可用于触发目标的反应逻辑或 UI 表现）
  - `AbilitySlotChangedSignal` → 自身（下轮 Think 可据此做后续处理）
- **Timer 使用**：
  - Spell Steal 自身 CD
  - 被偷技能的 CD 管理

**触及的约束**：

- **C3 barrier 可见性**：Rubick Think 读取目标的 `lastCastAbilityID` 是快照数据。如果目标在同一 tick 内又释放了新技能，Rubick 偷到的可能是"上一 tick 的最后技能"而非"本 tick 刚释放的技能"。这与 DOTA2 原版行为可能有微妙差异（原版是即时可见的）
- **C1 单 owner 提交**：被偷技能的等级信息需要从目标 public state 读取（快照读取，合法）。Rubick 不修改目标状态，只读取，满足单 owner 约束
- **C7 无去重**：Rubick 被多次激活时需要在 private state 中做幂等保护（如检查是否已偷取过该技能）

**所需改造**：

1. **目标英雄需要在 public state 中维护 `lastCastAbilityID` + `lastCastAbilityLevel`**：这是一个设计约定，要求所有英雄 Logic 在释放技能时将此信息写入 public state。这是 Apply 的一个额外字段更新，成本极低
2. **接受一个 tick 的信息延迟**：由于 C3，Rubick 偷到的是快照中的"最近技能"，如果目标在同一 tick 释放了新技能，需要等下一 tick 才能偷到最新的。实际体验影响极小（一个 tick 通常是 33ms），DOTA2 原版也有类似的时序窗口
3. **被偷技能的行为复制**：Rubick 内部需要能实例化任意技能的 Logic 行为。这是玩法层面的实现复杂度（技能工厂/模板），与调度框架无关

---

## 3. Faceless Void（虚空假面） - Chronosphere（时间结界）

**描述**：创建一个球形区域（AoE），区域内的所有单位（包括队友英雄，但不包括 Faceless Void 自己）被暂停（stunned），无法行动、攻击或施法。持续数秒后消失。Faceless Void 在区域内获得额外移速。该技能还会暂停区域内的投射物。

**适配判定**：⚠️ 需要妥协

**Owner 归属**：

- Chronosphere 区域：作为独立 Logic（独立 owner），或归属 World owner
- 受影响单位的"被暂停"状态：各自 owner 的 public state
- Faceless Void 的免疫：Void 自身 owner 的 private/public state

**执行流程映射**：

- **Think 阶段**：
  - Faceless Void Think 决定释放 Chronosphere，读取世界快照确定施法位置
  - 产出 `CreateZoneEffect` → World owner（请求在世界中注册一个持续性区域实体）
  - 或者：直接创建 Chronosphere 作为独立 Logic，向 World 注册
- **Effect 产出**：
  - `CreateZoneEffect` → World owner（注册区域）
  - Chronosphere Logic 每 tick Think：读快照做空间查询，找到区域内所有单位 ref
  - 对区域内每个非自身单位：`ChronoStunEffect` → 各目标 owner
  - 对 Faceless Void：`ChronoHasteEffect` → Void owner（增加移速）
  - 区域内投射物：`ChronoFreezeProjectileEffect` → 各投射物 Logic
- **Apply 阶段**：
  - 各目标 Apply 处理 `ChronoStunEffect`，在 public state 中设置"被 Chrono 暂停"状态
  - 被暂停的单位在后续 Think 中检查自身暂停状态，跳过决策逻辑
  - Void Apply 处理 `ChronoHasteEffect`，更新移速
  - 投射物 Logic Apply 处理冻结 Effect，暂停自身运动
- **Signal 产出**：
  - `ChronoFieldCreatedSignal` → 所有受影响单位（通知进入时间结界）
  - Chronosphere 结束时：`ChronoFieldEndedSignal` → 所有受影响单位（解除暂停）
- **Timer 使用**：
  - Chronosphere Logic 使用 timer 管理持续时间，到期后自销毁
  - 每 tick 唤醒进行区域内单位检测

**触及的约束**：

- **C1 单 owner 提交**："被暂停"状态由各目标 owner 自己的 Apply 处理，满足单 owner 约束。Chronosphere 不直接修改目标状态，只发送 Effect
- **C3 barrier 可见性**：Chronosphere 创建后，区域信息需要等到下一个 barrier 才对其他 Logic 可见。第一 tick 的 Think 中其他单位可能还不知道自己被暂停。实际影响：暂停可能延迟一轮 superstep 或一个 tick 生效
- **C6 Effect 无序安全**：目标同时收到来自其他来源的 Effect（如治疗、其他控制）与 `ChronoStunEffect`，Apply 必须正确处理这些无序组合。暂停类 Effect 通常作为最高优先级的状态覆盖，无序安全
- **C4 same-tick 尽力而为**：创建区域→查询单位→发送暂停 Effect→目标 Apply 生效，这个链条需要多轮 superstep。在 3 轮 budget 内完全可以收敛（创建 round 1 → 查询+Effect round 2 → Apply round 2/3）

**所需改造**：

1. **Chronosphere 作为独立 Logic**（推荐 P2 模式）：区域效果实体化为独立 owner，拥有自己的 Think/Apply 生命周期。每 tick Think 做空间查询、发送 Effect
2. **"被暂停"判定由目标 owner 负责**：目标 Apply 收到 `ChronoStunEffect` 后自行判断是否生效（检查自身免疫、不可控制等），符合 C1
3. **投射物暂停**：如果投射物是独立 Logic（P2），直接发送冻结 Effect 即可。如果投射物不是独立 Logic，需要改造为独立 Logic 或由 World owner 统一管理
4. **接受生效延迟**：暂停可能比原版延迟 1 轮 superstep（约 1/3 tick），实际游戏体验几乎无感知差异

---

## 4. Pudge（帕吉） - Meat Hook（肉钩）

**描述**：Pudge 向目标方向发射一个钩子投射物。钩子沿直线飞行，命中第一个单位后将其拉向 Pudge 当前位置，对敌方单位造成纯粹伤害。钩子可以钩到敌人和队友（队友不受伤害）。被钩的目标在拉动期间失去行动控制。钩子飞行途中 Pudge 可以移动，目标会被拉到 Pudge 的实时位置。

**适配判定**：⚠️ 需要妥协

**Owner 归属**：

- 钩子投射物：独立 Logic（P2 模式，独立 owner）
- Pudge 本体：Pudge Logic（owner）
- 被钩目标：目标 Logic（owner）

**执行流程映射**：

- **Think 阶段**：
  - Pudge Think 决定释放 Meat Hook，确定方向和速度
  - 产出 `CreateProjectileEffect` → World owner（创建钩子投射物 Logic）
  - 钩子 Logic 每 tick Think：读快照获取自身位置，做空间查询检测碰撞
  - 命中时：钩子 Think 产出 `HookHitEffect` → 目标 owner（附带伤害数值 + 拉动标记）
  - 钩子 Think 产出 `HookConnectedSignal` → Pudge owner（通知命中，开始拉动阶段）
  - 拉动阶段：钩子 Logic 每 tick Think 读取 Pudge public state 中的位置，计算拉动路径，产出 `ForceMoveEffect` → 目标 owner
- **Effect 产出**：
  - `CreateProjectileEffect` → World（注册投射物）
  - `HookHitEffect`（含伤害 + 控制标记）→ 目标
  - `ForceMoveEffect`（含目标位置）→ 目标（每 tick 持续拉动）
  - `HookRetractSignal` → Pudge（钩子收回完毕的通知）
- **Apply 阶段**：
  - 目标 Apply 处理 `HookHitEffect`：
    - 扣减 HP（如果是敌方）
    - 设置"被拉动"状态到 public state（禁止自主移动）
  - 目标 Apply 处理 `ForceMoveEffect`：更新自身位置
  - Pudge Apply：无特殊处理，正常移动
- **Signal 产出**：
  - `HookConnectedSignal` → Pudge（通知命中，Pudge 可据此调整行为）
  - `HookLandedSignal` → 目标（拉动完成后通知目标恢复控制）
  - `ProjectileDestroyedSignal` → World（钩子实体销毁）
- **Timer 使用**：
  - 钩子飞行：每 tick 唤醒（delay=1）
  - 拉动阶段：每 tick 唤醒更新目标位置
  - Meat Hook CD

**触及的约束**：

- **C1 单 owner 提交**：目标的位置更新由目标自己的 Apply 处理。钩子 Logic 只发送 `ForceMoveEffect`，不直接修改目标位置。满足 C1
- **C3 barrier 可见性**：钩子读取 Pudge 位置是快照数据。如果 Pudge 在同一 tick 移动了，钩子读到的是上一 barrier 的位置。实际效果：拉动目标位置会有约一个 tick 的位置追踪延迟。对于 33ms/tick 的游戏，几乎不可感知
- **C6 Effect 无序安全**：目标可能同时收到 `ForceMoveEffect` 和来自其他来源的移动/控制 Effect。Apply 需要做控制优先级仲裁（如：被钩拉动 > 普通移动命令）。只要 Apply 中有明确的优先级规则，任意 Effect 顺序都会产生相同结果
- **C4 same-tick 尽力而为**：创建投射物→碰撞检测→发送命中 Effect→目标 Apply，可能跨越多轮 superstep。首次命中可能延迟到第二个 tick 生效（取决于投射物创建时机和碰撞检测时序）

**所需改造**：

1. **钩子必须作为独立 Logic（P2）**：投射物实体化，拥有独立生命周期
2. **拉动阶段的位置更新通过 Effect**：每 tick 钩子 Logic 读 Pudge 快照位置 → 计算拉动目标点 → `ForceMoveEffect` → 目标 Apply 更新位置。不能直接操作目标坐标
3. **控制优先级仲裁在目标 Apply 中实现**：目标 Apply 需要知道"被钩拉动"的优先级高于普通移动命令
4. **接受一个 tick 的位置追踪延迟**

---

## 5. Oracle（神谕者） - False Promise（虚妄之诺）

**描述**：对一个友方单位施放，持续时间内目标不会死亡（HP 不会降到 1 以下）。期间目标受到的所有伤害和治疗都被记录但不立即结算（伤害照常扣血但不致死，治疗正常生效）。实际上原版行为是：伤害和治疗正常结算，但 HP 不会降至 1 以下；结束时所有治疗翻倍，然后结算净 HP 变化。如果净结果为正，目标回血；如果为负，目标受到对应伤害（可致死）。

**适配判定**：⚠️ 需要妥协

**Owner 归属**：

- False Promise buff：目标英雄 Logic 的 private/public state 中的 buff
- 真相 owner：目标英雄（因为 HP 变化由目标 Apply 裁决）
- 辅助追踪 owner：可以是 Oracle Logic（追踪是否施放了 FP）或目标 Logic 自身

**执行流程映射**：

- **Think 阶段**：
  - Oracle Think 决定释放 False Promise → 产出 `ApplyBuffEffect` → 目标 owner
  - 目标 Think 在后续 tick 中检测自身 buff 状态，正常进行决策
- **Effect 产出**：
  - `ApplyBuffEffect(FalsePromise, duration)` → 目标
- **Apply 阶段**：
  - 目标 Apply 收到 `ApplyBuffEffect`，在 public state 中添加 FalsePromise buff，初始化追踪计数器：`totalHealingReceived = 0`
  - FP 持续期间，目标 Apply 处理所有伤害/治疗 Effect 时：
    - 正常结算伤害，但 clamp HP ≥ 1（不致死）
    - 记录所有治疗量到 `totalHealingReceived`（累加）
    - 伤害正常扣除 HP（仍然 clamp ≥ 1）
  - FP 结束时（通过 timer 触发的 Think → 自身 Effect）：
    - 计算 `bonusHeal = totalHealingReceived`（翻倍治疗 = 额外加一倍）
    - 如果 `bonusHeal > 0`，对自身施加治疗
    - HP 自然结算（如果 clamp 期间累计了大量伤害，HP 可能已经是 1，翻倍治疗可能不够抵消）
- **Signal 产出**：
  - `FalsePromiseAppliedSignal` → Oracle（通知施放成功，用于 UI 反馈）
  - `FalsePromiseEndedSignal` → Oracle + 目标（结算完毕通知）
- **Timer 使用**：
  - FP buff 持续时间 timer（目标 Logic 设置 delay，到期触发结算）

**触及的约束**：

- **C1 单 owner 提交**：所有 HP 相关裁决由目标 owner 的 Apply 处理。Oracle 只发送一个 buff Effect，不参与后续 HP 结算。完美满足 C1
- **C6 Effect 无序安全**：FP 期间目标同时收到多个伤害/治疗 Effect，Apply 必须对每个都执行 clamp 和追踪逻辑。由于 clamp（HP ≥ 1）和累加追踪都是可交换操作，无序安全
- **C3 barrier 可见性**：FP buff 在目标 Apply 中设置后，其他 Logic 下一个 barrier 才能看到。影响极小

**所需改造**：

1. **HP 结算逻辑增强**：目标 Apply 处理伤害/治疗时需检查是否存在 FalsePromise buff，如果是则执行特殊逻辑（clamp + 累计追踪）。这是 Apply 内部的 buff 系统增强，框架层面无改动
2. **治疗追踪作为 buff private data**：`totalHealingReceived` 存储在 buff 的 state 中（属于目标 Logic private state 的 buff 子结构）
3. **结算时机**：通过 timer 触发目标 Think，Think 产出 `FalsePromiseSettlementEffect` → 自身 Apply，在 Apply 中完成最终 HP 结算并移除 buff

---

## 6. Morphling（变体精灵） - Attribute Shift（属性转换）

**描述**：Morphling 可以持续将力量属性点转化为敏捷属性点（或反之），每次转换一定量。力量影响 HP 上限和 HP 回复，敏捷影响护甲和攻击速度。转换过程中 HP 上限实时变化，当前 HP 按比例缩放（保持 HP 百分比不变）。这是一个 toggle 技能，开启后每 tick 持续转换直到手动关闭或属性耗尽。

**适配判定**：✅ 直接适配

**Owner 归属**：Morphling Logic（单一 owner）

**执行流程映射**：

- **Think 阶段**：
  - Morphling Think 检查 private state 中的 Attribute Shift 状态（开启/关闭、方向：STR→AGI 或 AGI→STR）
  - 如果开启：计算本 tick 的转换量（如每 tick 转换 N 点属性）
  - 在 private state 中更新属性分配（减少一个属性，增加另一个）
  - 产出 `AttributeChangeEffect` → 自身（携带新的 STR/AGI 值、新 HP 上限、HP 按比例缩放后的值）
  - 返回 delay=1（每 tick 唤醒，持续转换）
- **Effect 产出**：
  - `AttributeChangeEffect` → 自身（更新 public state 中的属性、HP 上限、当前 HP）
- **Apply 阶段**：
  - 自身 Apply 处理 `AttributeChangeEffect`：
    - 更新 public state 中的 STR/AGI 值
    - 更新 HP 上限
    - 按比例缩放当前 HP（保持百分比）
    - 更新派生属性（护甲、攻速、HP 回复等）
- **Signal 产出**：
  - `AttributeChangedSignal` → 自身（可选，触发 UI 更新或其他被动效果检查）
- **Timer 使用**：
  - Think 返回 delay=1，每 tick 唤醒以持续执行属性转换
  - 收到关闭指令或属性耗尽时停止返回 delay（不再自动唤醒）

**触及的约束**：

- **C1**：完美契合。所有属性变更都是 Morphling 自己的状态
- **C6**：自身 Effect 不会有外部冲突。如果同时收到外部 buff 修改属性的 Effect，Apply 中的属性计算应使用"基础属性 + 所有 modifier"的通用模式，天然无序安全
- **C7**：每 tick 通过 timer 唤醒一次，不存在重复激活问题

**适配模式**：**P1 单 owner 状态机** — 属性转换完全在 private state + 自身 Apply 中闭环运行

---

## 7. Meepo（米波） - Divided We Stand（个体分裂）

**描述**：Meepo 在学习终极技能后获得额外的分身（最多 4 个分身 + 本体 = 5 个 Meepo）。每个分身是一个完整的可控制单位，有独立的 HP、位置、技能 CD，可以独立移动、攻击和使用技能。关键约束：**任一 Meepo（本体或分身）死亡，所有 Meepo 都会死亡**。分身共享本体的经验和等级。

**适配判定**：⚠️ 需要妥协

**Owner 归属**：

- 每个 Meepo（本体 + 分身）：各自独立的 Logic（独立 owner）
- 死亡联动：需要跨 owner 协调

**执行流程映射**：

- **Think 阶段**：
  - 各 Meepo Logic 独立 Think：读取玩家输入信号（每个分身有独立操控）、读取世界快照进行 AI/决策
  - 独立产出各自的攻击/技能/移动 Effect
- **Effect 产出**：
  - 各自的攻击/技能 Effect → 各自的目标
  - 各自收到的伤害 Effect 由各自 Apply 处理
- **Apply 阶段**：
  - 各 Meepo Apply 独立处理收到的 Effect，更新自身 HP、位置等
  - **死亡检测**：某个 Meepo Apply 中 HP ≤ 0 时，产出 `MeepoDeathSignal` → 所有其他 Meepo Logic + World owner
- **Signal 产出**：
  - `MeepoDeathSignal` → 所有 Meepo（触发联动死亡）
  - 收到 `MeepoDeathSignal` 的 Meepo 在下轮 Think 中产出 `SelfKillEffect` → 自身 Apply
- **Timer 使用**：
  - 各 Meepo 独立的技能 CD、状态 timer
  - 分身创建/销毁通过 World owner 管理

**触及的约束**：

- **C1 单 owner 提交**：每个 Meepo 的 HP 由自己 Apply 裁决，满足 C1。死亡联动通过 Signal 传播，不是跨 owner 原子事务
- **C4 same-tick 尽力而为**：Meepo A 死亡 → Signal → Meepo B/C/D Think → 自杀 Effect → Apply。这个链条需要 2-3 轮 superstep。在 3 轮 budget 内可以收敛，但如果死亡发生在最后一轮 superstep，联动死亡可能延迟到下一 tick
- **C3 barrier 可见性**：Meepo A 死亡后其 public state 更新需等 barrier 后可见。其他 Meepo 可能在不知道 A 已死的情况下继续行动一个 superstep。通过 Signal 而非状态轮询来传播死亡信息可以缓解这个问题

**所需改造**：

1. **每个 Meepo 作为独立 Logic**：本体和分身各自有独立 owner，满足独立控制的需求
2. **死亡联动通过 Signal 协议**：Apply 检测到死亡时 Emit `MeepoDeathSignal` → 所有其他 Meepo。收到 Signal 的 Meepo 在 Think 中自杀。接受可能延迟一个 tick 的联动死亡
3. **共享经验/等级通过 Signal 同步**：本体获得经验时发送 `ExpGainSignal` → 所有分身。分身 Think 中更新自身等级。可能延迟一个 tick，但经验/等级的延迟几乎不影响游戏体验
4. **需要一个 Meepo Group 协调器**（可选）：可以作为一个无实体的 Logic，负责管理分身创建/销毁和联动规则。也可以将这些责任放在本体 Logic 中

---

## 8. Io（艾欧） - Tether（羁绊）

**描述**：Io 链接一个友方单位，链接期间：(1) Io 会被拉向链接目标（如果距离超过阈值），(2) Io 受到的治疗会按百分比共享给链接目标，(3) Io 受到的减速效果会传递给链接区域内的敌人（或根据版本不同有差异），(4) 如果链接断开（距离过远），Tether 结束。链接期间 Io 获得移速加成。

**适配判定**：⚠️ 需要妥协

**Owner 归属**：

- Tether buff/状态：Io Logic 的 private state（链接状态）+ Io public state（对外可见的链接标记）
- 治疗共享的裁决：需要拆分 — Io 的治疗由 Io Apply 处理，共享治疗由目标 Apply 处理
- 移速/位置调整：各自 owner

**执行流程映射**：

- **Think 阶段**：
  - Io Think 检查 private state 中的 Tether 状态（是否激活、链接目标 ref）
  - 读取快照中目标位置，计算距离
  - 如果距离 > 阈值但未超过断裂距离：产出 `ForceMoveSelfEffect` → 自身（拉向目标）
  - 如果距离 > 断裂距离：在 private state 中标记 Tether 结束
  - 返回 delay=1（每 tick 唤醒，持续维护链接状态）
- **Effect 产出**：
  - `ForceMoveSelfEffect` → 自身（拉向目标位置）
  - 建立链接时：`TetherLinkEffect` → 目标（通知被链接，可选）
- **Apply 阶段**：
  - Io Apply 处理治疗 Effect 时：
    - 正常结算自身治疗
    - 检查是否有 Tether buff，如果有：产出 `SharedHealEffect` → 链接目标（Emit Signal 在 Apply 中合法，但这里是 Effect，需要用 Signal 触发下一轮再发 Effect，或改为 Signal 传递治疗信息）
  - **改造方案**：Io Apply 处理治疗 Effect 后，Emit `HealReceivedSignal(amount)` → 自身。下一轮 Io Think 收到该 Signal，如果 Tether 激活，产出 `SharedHealEffect` → 目标
- **Signal 产出**：
  - `HealReceivedSignal` → 自身（Apply 中 Emit，记录本轮收到的治疗量）
  - `TetherBrokenSignal` → 目标（链接断开通知）
- **Timer 使用**：
  - delay=1 每 tick 唤醒检查距离和链接状态

**触及的约束**：

- **C1 单 owner 提交**：Io 的治疗由 Io Apply 处理，共享治疗作为 Effect 发送给目标由目标 Apply 处理。满足 C1
- **C3 barrier 可见性**：Io 读取目标位置是快照。如果目标在同一 tick 移动了，Io 的距离计算基于上一 barrier 位置。实际影响：链接断裂判定可能延迟一个 tick
- **C4 same-tick 尽力而为**：治疗共享链条：Io Apply 收到治疗 → Emit Signal → 下轮 Think → 产出 `SharedHealEffect` → 目标 Apply。需要 2 轮 superstep。如果治疗发生在最后一轮 superstep，共享治疗延迟到下一 tick
- **C6 Effect 无序安全**：Io 可能同时收到多个治疗 Effect，每个都应独立触发共享治疗。只要 Apply 对每个治疗 Effect 都 Emit 一个 `HealReceivedSignal`，累计效果等价于求和，无序安全

**所需改造**：

1. **治疗共享改为两阶段**：Io Apply 处理治疗 → Emit Signal → Think 产出 Effect → 目标 Apply。接受 1-2 轮 superstep 延迟
2. **距离检查基于快照**：接受一个 tick 的位置延迟
3. **Apply 中 Emit Signal 是合法的**（框架已确认 `CommitCtx` 提供 `Emit` 方法），所以 Io Apply 可以在处理治疗时直接 Emit Signal

---

## 9. Phoenix（凤凰） - Supernova（超新星）

**描述**：Phoenix 变成一颗蛋（Supernova），持续期间 Phoenix 无法行动。蛋有独立的 HP，敌人可以攻击蛋。如果蛋在持续时间结束前被摧毁（HP ≤ 0），Phoenix 死亡。如果蛋存活到持续时间结束，Phoenix 满血满蓝复活，并对周围敌人造成大量伤害和眩晕。蛋的持续时间内，Phoenix 周围会持续造成 DPS。

**适配判定**：✅ 直接适配

**Owner 归属**：Phoenix Logic（单一 owner）— 蛋是 Phoenix 的状态变化，不需要独立 owner

**执行流程映射**：

- **Think 阶段**：
  - Phoenix Think 决定释放 Supernova：
    - 在 private state 中切换状态机到 "EGG" 状态
    - 记录蛋的 HP（独立血量池）、持续时间剩余
    - 产出 `SelfTransformEffect` → 自身（切换为蛋形态，对外表现无法被选为普通攻击/技能目标，但蛋可被攻击）
  - 蛋状态期间的 Think：
    - 跳过正常决策逻辑（无法行动）
    - 读快照做空间查询，对范围内敌人产出 `SunrayDamageEffect`（持续 DPS）
    - 检查 private state 中蛋的剩余时间
    - 返回 delay=1
- **Effect 产出**：
  - `SelfTransformEffect` → 自身（进入蛋形态）
  - `SunrayDamageEffect` → 范围内敌人（持续 DPS）
  - 蛋存活结束时：
    - `FullRestoreEffect` → 自身（满血满蓝）
    - `SupernovaBurstDamageEffect` → 范围内敌人
    - `SupernovaStunEffect` → 范围内敌人
  - 蛋被摧毁时：
    - `SelfKillEffect` → 自身
- **Apply 阶段**：
  - Phoenix Apply 处理蛋受到的攻击 Effect：扣减蛋 HP（蛋 HP 作为 public state 的一部分）
  - 蛋 HP ≤ 0 时：在 Apply 中 Emit `EggDestroyedSignal` → 自身
  - Phoenix Think 收到 `EggDestroyedSignal`：产出 `SelfKillEffect` → 自身
  - Phoenix Apply 处理 `FullRestoreEffect`：恢复满血满蓝，切换回正常形态
- **Signal 产出**：
  - `EggDestroyedSignal` → 自身（蛋被摧毁）
  - `SupernovaEndedSignal` → 自身（蛋存活结束，触发爆炸逻辑）
- **Timer 使用**：
  - delay=1 每 tick 唤醒进行持续 DPS 和持续时间倒计时
  - 蛋持续时间结束的精确 timer（可直接用 private state 中的计数器 + delay=1 实现）

**触及的约束**：

- **C1**：完美契合。Phoenix 的蛋 HP、形态切换、复活/死亡都由 Phoenix 自身 Apply 裁决
- **C6**：蛋可能同时收到多个攻击 Effect，Apply 中逐一扣减蛋 HP，加法可交换，无序安全
- **C4**：蛋被摧毁 → Signal → Think → 自杀 Effect → Apply，需要 2 轮 superstep。在 budget 内可收敛

**适配模式**：**P1 单 owner 状态机** — Phoenix 在蛋状态和正常状态之间切换，完全由 private state 驱动

---

## 10. Techies（工程师） - Proximity Mines（感应地雷）

**描述**：Techies 在地面放置一颗隐形地雷。地雷有一个感应半径，当敌方单位进入范围后，经过短暂延迟（约 0.5 秒）后引爆，对范围内敌人造成魔法伤害。地雷放置后有短暂激活时间（约 1.75 秒），激活前不会触发。地雷有独立 HP，可以被攻击摧毁。地雷数量无上限。

**适配判定**：✅ 直接适配

**Owner 归属**：每颗地雷作为独立 Logic（P2 模式，独立 owner）

**执行流程映射**：

- **Think 阶段**：
  - Techies Think 决定放置地雷：产出 `CreateMineEffect` → World owner（创建地雷 Logic 实体）
  - 地雷 Logic Think：
    - 激活前阶段（private state 计时器）：什么都不做，返回 delay 等待激活
    - 激活后：读取世界快照，做空间查询检测范围内是否有敌方单位
    - 检测到敌人：在 private state 中标记"已触发"，开始引爆延迟倒计时
    - 引爆延迟结束：对范围内所有敌方单位产出 `MineExplosionDamageEffect` → 各目标
    - 产出 `DestroyEntityEffect` → World owner（销毁自身实体）
- **Effect 产出**：
  - `CreateMineEffect` → World（注册地雷实体）
  - `MineExplosionDamageEffect` → 范围内各敌方单位
  - `DestroyEntityEffect` → World（销毁地雷实体）
- **Apply 阶段**：
  - 各目标 Apply 处理 `MineExplosionDamageEffect`：扣减 HP
  - 地雷 Apply 处理攻击 Effect（地雷被攻击）：扣减地雷 HP
  - 地雷 HP ≤ 0：Emit `MineDestroyedSignal` → 自身 + Techies（通知拆除），下轮 Think 自销毁
- **Signal 产出**：
  - `MineDestroyedSignal` → Techies + World（地雷被摧毁通知）
  - `MineTriggeredSignal` → Techies（可选，通知地雷被触发）
- **Timer 使用**：
  - 激活延迟 timer（放置后等待激活）
  - 引爆延迟 timer（触发后等待引爆）
  - 可以直接用 Think 返回的 delay 实现两个阶段的计时

**触及的约束**：

- **C1**：完美契合。地雷是独立 owner，各目标 HP 由各自 Apply 裁决
- **C6**：目标可能同时被多颗地雷命中，多个 `MineExplosionDamageEffect` 无序到达 Apply。伤害是加法，无序安全
- **C3**：地雷读取快照做空间查询，可能有一个 tick 的检测延迟。对于 0.5 秒的引爆延迟来说，一个 tick 的检测误差可以忽略
- **C7**：地雷 Logic 可能在同一 superstep 被多次激活（如同时收到攻击 Signal 和 timer 唤醒）。Think 中需做幂等检查（如已标记"已触发"则跳过）

**适配模式**：**P2 投射物/延迟效果** — 地雷实体化为独立 Logic

---

## 统计摘要

### 适配结果统计

| 判定 | 数量 | 技能 |
|------|------|------|
| ✅ 直接适配 | 4 | Invoker Invoke、Morphling Attribute Shift、Phoenix Supernova、Techies Proximity Mines |
| ⚠️ 需要妥协 | 6 | Rubick Spell Steal、Faceless Void Chronosphere、Pudge Meat Hook、Oracle False Promise、Meepo Divided We Stand、Io Tether |
| ❌ 无法适配 | 0 | — |

### 约束触及频次

| 约束 | 触及次数 | 说明 |
|------|----------|------|
| C1 单 owner 提交 | 10/10 | 所有技能都涉及，全部满足 |
| C3 barrier 可见性 | 6/10 | 最常触及的妥协来源，所有跨 owner 交互都涉及快照延迟 |
| C6 Effect 无序安全 | 8/10 | 大多数技能涉及多 Effect 场景，需 Apply 逻辑保证可交换性 |
| C4 same-tick 尽力而为 | 5/10 | 多轮 superstep 链条场景，大多在 3 轮 budget 内可收敛 |
| C7 无去重 | 3/10 | 涉及 Signal 驱动的高频激活场景 |
| C2 成功语义锚定 | 0/10 | 这 10 个技能无需"跨 owner 成功反馈决定资源消费"的语义 |
| C5 串行域 | 0/10 | 无技能需要进入串行域 |

### 适配模式使用

| 模式 | 使用次数 | 技能 |
|------|----------|------|
| P1 单 owner 状态机 | 4 | Invoker、Morphling、Phoenix、Oracle（目标侧 buff） |
| P2 投射物/延迟效果 | 4 | Chronosphere、Meat Hook、Proximity Mines、Meepo 分身 |
| P3 被动触发 | 2 | Io（Apply 中治疗检测）、Oracle（FP buff 修改 Apply 行为） |
| P4 全局规则裁决 | 2 | Chronosphere（区域注册）、Techies（地雷注册） |
| P5 资源交换 | 0 | — |
| P6 链式技能/combo | 0 | — |

### 核心结论

1. **无技能完全无法适配**：10 个技能覆盖了 DOTA2 中高复杂度的典型代表（组合技、技能偷取、全局控制场、投射物位移、延迟结算、持续属性变换、多单位联动、状态共享链接、形态切换、陷阱地雷），全部可以在框架内实现

2. **最常见的妥协是时序延迟**：6 个⚠️技能的核心妥协都是接受 1 个 tick 或 1-2 轮 superstep 的延迟。在 30Hz tick rate 下（约 33ms/tick），这种延迟对玩家体验几乎不可感知

3. **C1（单 owner 提交）是框架最强的设计约束，但也是最易满足的**：只需将"谁负责裁决什么"想清楚，所有技能都可以分解为 owner-local 操作。DOTA2 的技能设计天然适合这个模型——伤害由 target 裁决，资源消耗由 source 裁决

4. **C6（Effect 无序安全）需要 Apply 实现者注意**：伤害（加法可交换）、控制（优先级仲裁）、buff（独立叠加）都有明确的无序安全策略。只要 Apply 遵循这些模式，无序安全可以系统性保证

5. **独立 Logic（P2）是处理复杂空间效果的万能模式**：投射物、区域效果、陷阱、分身——只要实体化为独立 owner，就天然获得并行安全性和生命周期管理能力

6. **Meepo 死亡联动是最接近框架极限的案例**：它要求跨多个 owner 的"全部死亡"原子语义。框架通过 Signal 协议将其改写为"尽力 same-tick 传播"，接受极端情况下一个 tick 的延迟。这是一个合理的语义软化
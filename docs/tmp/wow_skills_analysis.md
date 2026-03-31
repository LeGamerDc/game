# WoW 技能/机制 × 并行 Tick 调度框架适配分析

> 分析框架：基于 ownership 的并行 tick 调度器（`en/world.go` 接口权威）
> 分析日期：2025-07
> 核心约束参考：C1–C7，典型模式参考：P1–P6

---

## 目录

1. [牧师 - 守护之魂](#1-牧师priest---守护之魂guardian-spirit)
2. [圣骑士 - 圣盾术](#2-圣骑士paladin---圣盾术divine-shield)
3. [盗贼 - 影舞](#3-盗贼rogue---影舞shadow-dance)
4. [猎人 - 误导](#4-猎人hunter---误导misdirection)
5. [法师 - 寒冰屏障](#5-法师mage---寒冰屏障ice-block)
6. [术士 - 灵魂石](#6-术士warlock---灵魂石soulstone)
7. [德鲁伊 - 形态切换系统](#7-德鲁伊druid---形态切换系统)
8. [死亡骑士 - 符文系统](#8-死亡骑士death-knight---符文系统)
9. [战士 - 法术反射](#9-战士warrior---法术反射spell-reflection)
10. [萨满 - 图腾系统](#10-萨满shaman---图腾系统)
11. [统计摘要](#统计摘要)

---

## 1. 牧师（Priest） - 守护之魂（Guardian Spirit）

**描述**：对目标施放守护之魂（buff），持续 10 秒。若目标 HP 降至致死阈值，消耗 buff 改为恢复一定百分比 HP 而非死亡，同时触发施法者的 CD。

**适配判定**：⚠️ 需要妥协

**Owner 归属**：**目标（target）** 是死亡/存活的真相 owner。施法者（caster）拥有 CD 状态。

**执行流程映射**：

- **Think 阶段**：
  - Caster Think：施放时产出 `ApplyGuardianSpirit` Effect 投递给 target；CD 写入 private state。
  - Target Think：无特殊逻辑（buff 状态在 Apply 侧管理）。
- **Effect 产出**：
  - `ApplyGuardianSpirit { caster_ref, heal_pct, duration }` → target
- **Apply 阶段**：
  - Target Apply 处理所有 Effect（包括伤害）时执行"死亡判定"：
    1. 累计本轮所有伤害 Effect。
    2. 若 HP 将归零且存在 Guardian Spirit buff → 取消死亡，恢复 `heal_pct` HP，消耗 buff。
    3. 产出 `GuardianSpiritConsumed` Signal → caster（通知 CD 开始）。
  - 若 buff 自然过期（timer 到期）→ 移除 buff，产出 `GuardianSpiritExpired` Signal → caster。
- **Signal 产出**：
  - `GuardianSpiritConsumed { target_ref }` → caster（下轮 Think 收到后可更新 UI/内部状态）
  - `GuardianSpiritExpired { target_ref }` → caster
- **Timer 使用**：
  - Target：buff 持续时间 timer（10 秒 = N ticks），到期移除 buff。
  - Caster：CD timer（private state 管理）。

**触及的约束**：

| 约束 | 分析 |
|------|------|
| **C1 单 owner 提交** | 死亡判定归 target owner，CD 归 caster owner。两个真相各自独立，符合 C1。 |
| **C2 成功语义锚定** | 关键问题：caster 需要知道"buff 是否被消耗"才能决定 CD 策略。但 caster 无法在同一 tick 获知 target 的 Apply 结果。改为 **cast-and-forget**：buff 施放即开始 CD（consume-on-cast），或分拆为"触发时 Signal 通知 caster 开始 CD"。 |
| **C3 barrier 可见性** | Target 的 HP 变化（被救活）要到下一 barrier 后对外可见。同 tick 内其他 Logic 可能仍认为 target 已死。 |
| **C6 Effect 无序安全** | Target Apply 收到伤害 + Guardian Spirit 两类 Effect 无序。Apply 必须先聚合所有伤害，再判定是否触发 Guardian Spirit。实现为"先收集，再决算"模式。 |

**所需改造**：

1. **CD 触发时机改写**（C2 应对）：WoW 原版中"buff 触发时 CD 才开始"。在本框架中有两种策略：
   - **方案 A（推荐）**：cast-on-use CD。施放瞬间 CD 即开始（略微偏离原版语义，但避免跨 owner 依赖）。
   - **方案 B**：延迟 CD。Target Apply 消耗 buff 后通过 Signal 通知 caster，caster 下轮 Think 开始 CD。代价是 CD 开始时间延迟 1 superstep / 1 tick（C4），但功能正确。
2. **Apply 阶段"先聚合再决算"**（C6 应对）：Target Apply 不能逐条处理 Effect，需要先扫描所有伤害 Effect 求和，再判定是否触发守护之魂。

---

## 2. 圣骑士（Paladin） - 圣盾术（Divine Shield）

**描述**：激活后获得 8 秒无敌，免疫所有伤害和控制效果，但自身攻击力降低 50%。

**适配判定**：✅ 直接适配

**Owner 归属**：**自身（self）** 是唯一真相 owner。无敌状态、攻击力修正都是 self 的 public state。

**执行流程映射**：

- **Think 阶段**：
  - Self Think：决定施放 → 设置 private state `divine_shield_active = true`，CD 写入 private state，返回 timer delay = 持续时间 ticks。
  - 不需要产出 Effect 给其他 owner（纯自身状态变更通过 self-Effect 或直接在 Apply 提交）。
  - 产出 `DivineShieldEffect { activate: true }` → self（自发自收）。
- **Effect 产出**：
  - `DivineShieldEffect` → self（Publish to self）
- **Apply 阶段**：
  - Self Apply：
    1. 处理 `DivineShieldEffect` → 在 public state 设置 `immune_all = true`，`attack_modifier = 0.5`。
    2. 处理所有伤害/控制 Effect → 检查 `immune_all` flag，全部丢弃（过滤为 0）。
  - 注意：由于 C6 Effect 无序，Apply 必须**先处理 DivineShieldEffect 再处理伤害**，或以"声明式终态"方式实现（先扫描是否有 DivineShield，有则标记免疫后再处理其余）。
- **Signal 产出**：无必要（纯 self 状态变更）。
- **Timer 使用**：
  - Self：持续时间 timer（8 秒），到期时 Think 被激活，产出取消 Effect 恢复正常状态。

**触及的约束**：

| 约束 | 分析 |
|------|------|
| **C1 单 owner 提交** | 完全在 self owner 内闭环，完美符合。 |
| **C6 Effect 无序安全** | Apply 需保证"免疫判定"先于"伤害处理"。采用**两阶段扫描**：第一遍扫描是否有 DivineShield 激活，第二遍处理伤害（有免疫则丢弃）。 |

**所需改造**：无。典型 P1 单 owner 状态机 + C6 两阶段扫描模式。

---

## 3. 盗贼（Rogue） - 影舞（Shadow Dance）

**描述**：激活后进入特殊"影舞"状态 6 秒，期间可使用潜行专用技能（如伏击、偷袭），且受到攻击不会打断潜行状态。

**适配判定**：✅ 直接适配

**Owner 归属**：**自身（self）** 是唯一真相 owner。"影舞状态"是 self 的 private/public state。

**执行流程映射**：

- **Think 阶段**：
  - Self Think：决定施放 → private state 标记 `shadow_dance_active = true`，记录剩余持续时间。
  - 在 shadow_dance_active 期间，Think 的技能选择逻辑解锁潜行技能集（如伏击、偷袭），正常产出对应 Effect 给 target。
  - 返回 timer delay = 持续时间 ticks。
- **Effect 产出**：
  - 状态切换：`ShadowDanceEffect { activate }` → self
  - 技能效果：正常攻击 Effect → target（与普通技能相同）
- **Apply 阶段**：
  - Self Apply：
    1. 处理 `ShadowDanceEffect` → 更新 public state `stealth_mode = shadow_dance`（告知外界自己处于潜行态）。
    2. 处理收到的攻击 Effect → 正常扣血，但**不清除潜行标记**（影舞特性：受击不脱潜行）。
- **Signal 产出**：无必要。
- **Timer 使用**：
  - Self：持续时间 timer，到期 Think 激活后取消影舞状态。

**触及的约束**：

| 约束 | 分析 |
|------|------|
| **C1 单 owner 提交** | 完全自身闭环。 |
| **C3 barrier 可见性** | 影舞激活后，其他 Logic 下一 barrier 才能看到 stealth 状态变化。同 tick 内敌方 Think 可能仍以"非潜行"状态做决策（如选择目标）。这在游戏逻辑上可接受——影舞激活的"突然性"本身就意味着对手来不及反应。 |

**所需改造**：无。经典 P1 单 owner 状态机 + P6 combo 管理。

---

## 4. 猎人（Hunter） - 误导（Misdirection）

**描述**：对友方目标施放，持续 8 秒。期间猎人产生的仇恨（threat）转移给指定目标（通常是坦克）。

**适配判定**：⚠️ 需要妥协

**Owner 归属**：**仇恨表（threat table）** 是关键真相。仇恨表归属存在设计选择：

- **方案 A（推荐）**：仇恨表归 boss/怪物 owner（怪物是仇恨表的真相 owner）。
- **方案 B**：仇恨表归 World owner（全局裁决）。

**执行流程映射**（方案 A）：

- **Think 阶段**：
  - Hunter Think：施放误导 → private state 记录 `misdirect_target = tank_ref`，`misdirect_remaining = N ticks`。
  - 后续每次 Think 产出伤害 Effect 时，**在 Effect 中标注 threat 归属**：`DamageEffect { source: hunter, threat_credit: tank_ref, ... }` → boss。
- **Effect 产出**：
  - `DamageEffect { source: hunter, damage, threat_credit: tank_ref }` → boss
- **Apply 阶段**：
  - Boss Apply：处理 `DamageEffect` → 扣血，更新仇恨表时将 threat 计入 `threat_credit`（tank）而非 `source`（hunter）。
- **Signal 产出**：
  - Boss Apply 可选产出 `ThreatUpdateSignal` → hunter/tank（用于 UI 更新，非必需）。
- **Timer 使用**：
  - Hunter：误导持续时间 timer，到期清除 `misdirect_target`。

**触及的约束**：

| 约束 | 分析 |
|------|------|
| **C1 单 owner 提交** | 仇恨表归 boss owner，hunter 只提供"意图标注"，boss 自行决定是否采纳。符合 C1。 |
| **C2 成功语义** | Hunter 无法确认 threat 是否真的被转移（boss 可能有免疫 threat redirect 的能力）。采用 fire-and-forget 语义。 |
| **C6 Effect 无序安全** | Boss 收到多个带不同 `threat_credit` 的 DamageEffect，每条独立处理 → 天然无序安全。 |

**所需改造**：

1. **DamageEffect 扩展**：需要在伤害 Effect 中增加 `threat_credit` 字段，让 boss Apply 知道 threat 应该归谁。这是 Effect 类型的设计扩展而非框架改造。
2. **语义微调**：原版 WoW 中误导有"3 次攻击"限制。在本框架中需由 hunter 的 Think private state 自行计数，计满后停止标注 `threat_credit`。

---

## 5. 法师（Mage） - 寒冰屏障（Ice Block）

**描述**：立即进入免疫一切状态，清除所有 debuff，持续期间无法移动、攻击或施法。

**适配判定**：✅ 直接适配

**Owner 归属**：**自身（self）** 是唯一真相 owner。

**执行流程映射**：

- **Think 阶段**：
  - Self Think：决定施放 → private state `ice_block_active = true`，CD 写入。
  - 产出 `IceBlockEffect { activate: true }` → self。
  - 返回 timer delay = 持续时间 ticks。
  - 在 ice_block_active 期间，Think 不产出任何攻击/移动 Effect（自我行为锁定）。
- **Effect 产出**：
  - `IceBlockEffect` → self
- **Apply 阶段**：
  - Self Apply：
    1. 扫描所有 Effect，识别 `IceBlockEffect`。
    2. 若激活：清除 public state 中所有 debuff，设置 `immune_all = true`，`action_locked = true`。
    3. 丢弃本轮所有伤害/控制类 Effect。
  - 同圣盾术的两阶段扫描模式。
- **Signal 产出**：无必要。
- **Timer 使用**：
  - Self：持续时间 timer，到期 Think 激活后解除冰箱。

**触及的约束**：

| 约束 | 分析 |
|------|------|
| **C1 单 owner 提交** | 纯 self 闭环。 |
| **C6 Effect 无序安全** | 同圣盾术：两阶段扫描（先识别 IceBlock，再处理其余）。 |
| **C3 barrier 可见性** | debuff 清除后对外可见有 1 barrier 延迟。无功能影响——debuff 源方在下一 tick 才能观测到 debuff 消失。 |

**所需改造**：无。与圣盾术同构，P1 模式。

---

## 6. 术士（Warlock） - 灵魂石（Soulstone）

**描述**：预先对目标使用灵魂石（buff），若目标死亡，可选择在原地复活（恢复一定 HP/MP）。

**适配判定**：⚠️ 需要妥协

**Owner 归属**：**目标（target）** 是死亡/复活的真相 owner。术士（caster）拥有灵魂石 CD/消耗。

**执行流程映射**：

- **Think 阶段**：
  - Caster Think：施放灵魂石 → 产出 `SoulstoneEffect { caster_ref, resurrect_hp_pct }` → target。
- **Effect 产出**：
  - `SoulstoneEffect` → target（注册 buff）
- **Apply 阶段**：
  - Target Apply：收到 `SoulstoneEffect` → 在 public state 注册 `soulstone_buff`。
  - Target 死亡时（HP ≤ 0 判定）：检查 `soulstone_buff` → 如果存在，进入"待复活"状态而非真正死亡。
  - "待复活"状态 → **下一 tick** target Think 被 timer 激活，产出 `ResurrectSelfEffect` → self。
  - Target Apply：处理 `ResurrectSelfEffect` → 恢复 HP/MP，清除 soulstone_buff。
- **Signal 产出**：
  - Target Apply 产出 `SoulstoneConsumedSignal` → caster（通知消耗）。
  - Target Apply 产出 `PlayerResurrectedSignal` → World（通知全局：entity 复活）。
- **Timer 使用**：
  - Target："待复活"状态时 timer 1 tick（或更长，模拟"选择复活"延迟）。
  - Caster：灵魂石 CD timer。

**触及的约束**：

| 约束 | 分析 |
|------|------|
| **C1 单 owner 提交** | 死亡/复活判定归 target，CD 归 caster。各自独立。 |
| **C2 成功语义** | 术士无法同 tick 确认灵魂石是否成功注册。采用 consume-on-cast：施放即消耗材料/CD，不依赖 target 确认。 |
| **C3 barrier 可见性** | 复活不是即时对外可见的——target 从"死亡"到"复活"需经过至少 1 tick 的 barrier。 |
| **C4 same-tick** | 死亡 → 检查灵魂石 → 复活是多步流程，不保证同 tick 完成。实际设计为至少 2 tick（死亡判定 + 复活提交），可接受。 |

**所需改造**：

1. **"玩家选择"机制**：WoW 中灵魂石复活是可选的（弹出对话框）。在框架中改写为：
   - 自动复活（AI 场景），或
   - 外部输入 Signal（玩家客户端输入注入），target Think 收到玩家确认 Signal 后才执行复活。
2. **死亡判定拆分**：target Apply 中 HP ≤ 0 不立即标记为"已死亡"（entity 销毁），而是进入"待复活窗口"状态。这需要 death 系统支持"死亡 ≠ 销毁"的两阶段设计。

---

## 7. 德鲁伊（Druid） - 形态切换系统

**描述**：在人形/熊/猫/鸟/旅行等形态间切换，每个形态有独立的技能栏和部分属性（如熊形态增加护甲和生命值，猫形态增加暴击）。

**适配判定**：✅ 直接适配

**Owner 归属**：**自身（self）** 是唯一真相 owner。形态是 private/public state 的一部分。

**执行流程映射**：

- **Think 阶段**：
  - Self Think：决定切换形态 → private state 更新 `current_form`。
  - Think 内部的技能选择逻辑根据 `current_form` 切换可用技能集（P6 模式）。
  - 产出 `ShapeshiftEffect { new_form }` → self。
- **Effect 产出**：
  - `ShapeshiftEffect { new_form: bear/cat/bird/travel }` → self
- **Apply 阶段**：
  - Self Apply：处理 `ShapeshiftEffect` → 更新 public state：
    - 切换属性修正（护甲/生命/暴击等）。
    - 更新外观标记（供其他 Logic 读取）。
    - 清除与新形态不兼容的 buff/debuff（如变形解控）。
- **Signal 产出**：可选 `FormChangedSignal` → 附近 Logic（用于 AI 重新评估威胁等）。
- **Timer 使用**：无特殊 timer 需求（形态切换是即时的，无持续时间限制）。

**触及的约束**：

| 约束 | 分析 |
|------|------|
| **C1 单 owner 提交** | 纯 self 闭环。 |
| **C3 barrier 可见性** | 形态切换后属性变化需 barrier 后可见。同 tick 内敌方可能仍按旧属性计算。游戏上可接受（切换是 GCD 内的即时动作）。 |
| **C6 Effect 无序安全** | 若同 tick 收到 ShapeshiftEffect + 伤害 Effect，Apply 需定义顺序。两阶段扫描：先处理 Shapeshift（确定新形态属性），再应用伤害（用新属性计算减伤等）。 |

**所需改造**：无。经典 P1 单 owner 状态机。形态 = private state enum，属性表 = 按形态索引的配置数据。

---

## 8. 死亡骑士（Death Knight） - 符文系统

**描述**：6 个符文（2 血/2 冰/2 邪），使用后进入 CD（约 10 秒），独立恢复。部分技能消耗特定类型符文，部分天赋可将符文转换为"死亡符文"（万能符文）。

**适配判定**：✅ 直接适配

**Owner 归属**：**自身（self）** 是唯一真相 owner。符文系统完全是 private state。

**执行流程映射**：

- **Think 阶段**：
  - Self Think：
    1. 检查 6 个符文的 CD 状态（private state `rune[6].ready_at`）。
    2. 根据可用符文决定可施放技能。
    3. 施放技能时消耗对应符文（标记 `rune[i].ready_at = now + rune_cd`）。
    4. 产出技能 Effect → target。
    5. 返回 timer delay = min(所有符文剩余 CD)（确保有符文恢复时被激活）。
- **Effect 产出**：
  - 技能对应的 `DamageEffect`/`DebuffEffect` → target（与普通技能相同）
- **Apply 阶段**：
  - Self Apply：无特殊处理（符文状态在 Think 阶段 private state 管理）。
  - Target Apply：正常处理伤害/debuff。
- **Signal 产出**：无必要。
- **Timer 使用**：
  - Self：返回 delay 对应下一个符文恢复时间，确保及时激活。
  - 多个符文独立 CD → Think 计算最近的恢复时间作为 delay。

**触及的约束**：

| 约束 | 分析 |
|------|------|
| **C1 单 owner 提交** | 符文系统完全在 self private state，无跨 owner 依赖。 |
| **C7 无 per-logic 去重** | 如果同一 superstep 内 DK 被多次激活（多个 Signal），每次 Think 都会读取同一 private state。由于符文消耗写入 private state 是幂等的（检查 rune 是否 ready 再消耗），多次激活不会导致重复消耗，但可能产出重复 Effect。DK Logic 需自行管理"本 tick 是否已决策"标记。 |

**所需改造**：无。P1 单 owner 状态机的教科书案例。符文 CD 管理 = 6 个独立 timer 在 private state 中的数组。

---

## 9. 战士（Warrior） - 法术反射（Spell Reflection）

**描述**：激活后获得短暂窗口（约 5 秒），将下一个指向自己的法术反射回施法者，对施法者造成原始效果。

**适配判定**：⚠️ 需要妥协

**Owner 归属**：**战士（self）** 是反射状态的真相 owner。但"将效果作用于施法者"涉及跨 owner 交互。

**执行流程映射**：

- **Think 阶段**：
  - Warrior Think：决定施放 → private state `spell_reflect_active = true`，返回 timer delay = 持续时间 ticks。
  - 产出 `SpellReflectEffect { activate }` → self。
- **Effect 产出**：
  - `SpellReflectEffect` → self（激活/取消反射盾）
- **Apply 阶段**：
  - Warrior Apply：
    1. 扫描所有 Effect。若存在 `SpellReflectEffect(activate)` → 设置 `reflecting = true`。
    2. 扫描是否有法术类 Effect（`SpellDamageEffect` / `SpellDebuffEffect`）：
       - 若 `reflecting == true` 且存在法术 Effect → **消费反射状态**（`reflecting = false`），产出 `ReflectedSpellEffect { original_effect, target: original_caster }` Signal → caster（将效果"弹回"）。
       - 由于 Effect 无序（C6），若同 tick 有多个法术，Apply 只反射其中一个（取第一个扫描到的，或按优先级选择），其余正常处理。
    3. 非法术 Effect 正常处理。
- **Signal 产出**：
  - `ReflectedSpellSignal { reflected_damage, debuff_type, ... }` → original caster
  - Caster 下轮 Think 收到 Signal → 产出 `SelfDamageEffect` → self（对自己造成反射伤害）。
- **Timer 使用**：
  - Warrior：反射持续窗口 timer，到期未反射则消失。

**触及的约束**：

| 约束 | 分析 |
|------|------|
| **C1 单 owner 提交** | Warrior 的 Apply 只修改自己的状态（消费反射 buff）。反射伤害通过 Signal → caster Think → caster self-Effect 路径实现，每一步都是各自 owner 的合法操作。 |
| **C3 barrier 可见性** | 反射效果不是即时生效的：warrior Apply 发出 Signal → caster 下轮 Think 收到 → caster Apply 扣血。至少延迟 1 superstep。 |
| **C4 same-tick** | 法术飞来 → 反射 → 对 caster 生效，这是一个 3 步链：warrior Apply → Signal → caster Think → caster Apply。在 3 轮 superstep 预算内可能完成（取决于 timing），但不保证。 |
| **C6 Effect 无序安全** | 多个法术同 tick 到达 warrior：Apply 需定义"反射哪一个"的规则（如按 EffectKind 优先级，或取最高伤害）。 |

**所需改造**：

1. **反射路径延迟**（C3/C4 应对）：接受反射效果有 1-2 superstep 延迟。在快节奏战斗中几乎不可感知（1 superstep ≈ 同 tick 内的第二轮）。
2. **多法术到达策略**（C6 应对）：定义反射优先级规则。建议：反射伤害最高的法术，其余正常受击。
3. **Signal → self-Effect 模式**：caster 收到 `ReflectedSpellSignal` 后，在 Think 中产出对自己的 `DamageEffect`。这确保 caster 的 HP 变更仍由 caster 自己的 Apply 执行（符合 C1）。

---

## 10. 萨满（Shaman） - 图腾系统

**描述**：放置图腾作为独立实体（有 HP，可被攻击摧毁），提供范围光环（如抗性图腾）、脉冲治疗（治疗之泉图腾）、减速场（地缚图腾）等持续效果。

**适配判定**：✅ 直接适配

**Owner 归属**：**每个图腾是独立的 Logic（独立 owner）**（P2 投射物/延迟效果模式）。萨满（caster）是创建者。

**执行流程映射**：

- **Think 阶段**：
  - Shaman Think：决定放置图腾 → 产出 `SpawnTotemEffect { totem_type, position }` → World owner。
  - Totem Think（每 tick / 每 N tick 激活）：
    - 读 WorldView 快照获取范围内 entity 列表（空间索引查询）。
    - 根据图腾类型产出对应 Effect：
      - 治疗图腾：`HealEffect` → 范围内友方。
      - 减速图腾：`SlowDebuffEffect` → 范围内敌方。
      - 抗性图腾：`ResistanceBuffEffect` → 范围内友方。
    - 返回 timer delay = pulse_interval（如 2 秒 = M ticks）。
- **Effect 产出**：
  - `SpawnTotemEffect` → World（entity 注册）
  - `HealEffect` / `SlowDebuffEffect` / `ResistanceBuffEffect` → 范围内各 target
- **Apply 阶段**：
  - World Apply：处理 `SpawnTotemEffect` → 在 entity 注册表中创建 totem entity，注册为新 Logic。
  - Target Apply：正常处理 heal/debuff/buff Effect。
  - Totem Apply：处理收到的 `DamageEffect`（图腾可被攻击） → 扣 HP，HP ≤ 0 时标记销毁。
- **Signal 产出**：
  - Totem Apply（HP ≤ 0）→ `TotemDestroyedSignal` → World（注销 entity）。
  - Totem Apply（HP ≤ 0）→ `TotemDestroyedSignal` → Shaman（通知创建者）。
- **Timer 使用**：
  - Totem：脉冲间隔 timer（治疗/减速等每 N tick 触发一次）。
  - Totem：持续时间 timer（图腾总持续时间，如 120 秒，到期自毁）。

**触及的约束**：

| 约束 | 分析 |
|------|------|
| **C1 单 owner 提交** | 图腾是独立 owner，自行管理 HP 和脉冲逻辑。Shaman 只负责创建。 |
| **C3 barrier 可见性** | 图腾 Think 读快照中的 entity 位置做范围判定。新进入范围的 entity 需 barrier 后才可见 → 可能漏一轮脉冲。可接受（WoW 原版图腾脉冲也有延迟）。 |
| **C4 same-tick** | 图腾创建 → 首次脉冲需至少 2 tick（World Apply 注册 → 下 tick 图腾 Think 激活）。可接受。 |
| **C6 Effect 无序安全** | 图腾 Apply 收到多个攻击 Effect → 累加伤害，与普通 entity 一致。 |

**所需改造**：无。P2 投射物/延迟效果模式的标准应用。图腾 = 独立 Logic + 定时脉冲 + 空间查询。

---

## 统计摘要

### 适配判定统计

| 判定 | 数量 | 技能 |
|------|------|------|
| ✅ 直接适配 | **6** | 圣盾术、影舞、形态切换、符文系统、寒冰屏障、图腾系统 |
| ⚠️ 需要妥协 | **4** | 守护之魂、误导、灵魂石、法术反射 |
| ❌ 无法适配 | **0** | — |

### 触及约束频次

| 约束 | 触及次数 | 说明 |
|------|----------|------|
| **C1 单 owner 提交** | 10/10 | 所有技能都需要明确 owner 归属，全部满足 |
| **C2 成功语义锚定** | 3/10 | 守护之魂（CD 触发）、误导（threat 确认）、灵魂石（buff 注册确认） |
| **C3 barrier 可见性** | 7/10 | 大多数涉及状态切换的技能都有 barrier 延迟，但都可接受 |
| **C4 same-tick 尽力而为** | 3/10 | 灵魂石（死亡→复活链）、法术反射（反射链）、图腾（创建→首脉冲） |
| **C5 强顺序串行域** | 0/10 | 所有技能都不需要进入串行域 |
| **C6 Effect 无序安全** | 7/10 | 涉及"免疫+伤害同时到达"的都需两阶段扫描模式 |
| **C7 无 per-logic 去重** | 1/10 | 符文系统（重复激活需自行管理） |

### 使用的适配模式频次

| 模式 | 使用次数 | 技能 |
|------|----------|------|
| **P1 单 owner 状态机** | 8/10 | 圣盾术、影舞、寒冰屏障、形态切换、符文系统、法术反射、守护之魂(部分)、灵魂石(部分) |
| **P2 投射物/延迟效果** | 1/10 | 图腾系统 |
| **P3 被动触发** | 2/10 | 守护之魂（target 被动触发）、法术反射（target Apply 检查被动） |
| **P4 全局规则裁决** | 1/10 | 图腾系统（World 注册 entity） |
| **P5 资源交换** | 0/10 | — |
| **P6 链式/combo** | 2/10 | 影舞（解锁潜行技能集）、形态切换（切换技能栏） |

### 核心发现

1. **零无法适配**：所有 10 个 WoW 技能均可在本框架中实现，无一需要突破框架约束。
2. **C6 两阶段扫描是高频模式**：任何涉及"免疫/状态切换 + 伤害同 tick 到达"的场景，Apply 都需要先扫描状态变更 Effect，再处理伤害 Effect。建议在框架层面提供 Effect 分类扫描的工具函数。
3. **跨 owner 交互一律走 Effect→Signal 链**：守护之魂、误导、法术反射、灵魂石都遵循"source 提供意图 Effect → target Apply 决策 → Signal 反馈 source"的标准模式。这验证了 C1 + P3 的组合威力。
4. **consume-on-cast 是 C2 的通用解法**：当"施放是否成功"依赖 target 状态时，统一改为 cast-and-forget，通过 Signal 异步获取结果。
5. **延迟可接受**：所有 C3/C4 引入的 1-2 tick / superstep 延迟在游戏体验上都可接受。WoW 原版的服务器 tick 约 400ms，本框架的 superstep 粒度更细，延迟感知更低。
6. **P1 占绝对主导**：80% 的技能核心逻辑可在单 owner private state 内完成，验证了"ownership-centric"设计的有效性。
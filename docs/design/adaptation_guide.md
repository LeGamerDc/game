# Scheduler 适配性分类指导手册

> 基于 107 条逻辑链路（30 条经典游戏技能 + 77 条 OpMap 真实业务）的深度分析，提炼的底层原理分类。
>
> **目标读者**：开发者 / AI Agent，在面对一段游戏逻辑时，快速判定它属于哪类适配模式、需要多大改造成本、以及怎么做。

Last Updated: 2025-07-27

---

## 如何使用本文档

面对一段待适配的游戏逻辑时，**按顺序回答以下五个问题**：

```
Q1. 这段逻辑的"真相 owner"是谁？能否归属到单一 owner？
     → 判定属于 A 类（可归属）还是需要进入 Q2

Q2. 它需要修改其他 owner 的状态吗？修改模式是什么？
     → 判定属于 B1–B4 的哪个子类

Q3. 它对读取其他 owner 状态的新鲜度有什么要求？
     → 判定是否需要时序妥协（C 类）

Q4. 多个同类 Effect 同时到达同一 owner 时，处理结果是否依赖顺序？
     → 判定是否需要无序安全改造（D 类）

Q5. 它是否存在级联反应链？链路深度是否有界？
     → 判定是否存在收敛性风险（E 类）
```

一段逻辑可能同时命中多个分类——这很正常。分类之间不互斥，而是叠加的。最终适配方案是各命中分类的应对策略的组合。

---

## 分类总览

```
                        +--------------------------+
                        |  A. Owner 闭环           |
                        |  (Single-Owner Closure)  |
                        +-----------+--------------+
                                    |
                   逻辑是否跨 owner 写？
                        YES         |  NO → 直接适配 ✅
                         |          |
            +------------+----------+
            |                       |
  +---------v----------+  +--------v----------+
  | B. 跨 Owner 写模式  |  | C. 快照时序延迟     |
  | (Cross-Owner Write)|  | (Snapshot Delay)  |
  +----+----+----+-----+  +---------+---------+
       |    |    |    |              |
      B1   B2   B3   B4    读新鲜度是否够？
                                    |
                        +-----------+-----------+
                        |                       |
               +--------v--------+    +---------v--------+
               | D. 无序安全性     |    | E. 级联收敛性     |
               | (Commutativity) |    | (Cascade Depth)  |
               +-----------------+    +------------------+
```

---

## A. Owner 闭环（Single-Owner Closure）

### 底层原理

逻辑的全部状态读写都在**同一个 owner** 内部完成——读写 private state、产出 Effect 给自身、Timer 驱动状态转移——不需要任何跨 owner 交互。这是框架最理想的适配模式，也是实践中占比最高的模式。

### 识别特征

- 所有读写的数据都属于同一个实体（如技能 CD、行为树栈、内部计数器）
- 对外只有"通知"性质的输出（可表达为 Effect/Signal），不依赖外部反馈
- 逻辑的"成功/失败"判定不依赖任何其他 owner 的状态

### 适配策略

**直接适配。** 状态机运行在 private state，Think 驱动状态转换，Apply 提交 public state 变更，Timer 驱动持续/周期行为。

### 典型案例

| 来源 | 案例 | 说明 |
|------|------|------|
| LOL | 亚索 Q 斩钢闪叠层 | combo 计数、CD 计时完全在 source private state |
| DOTA2 | Morphling 属性转换 | 连续每 tick 重分配 AGI/STR，纯自身数值操作 |
| WOW | DK 符文系统 | 符文充能/消耗/冷却全在自身状态机 |
| OpMap | ModSkill 技能状态机 | CD → 前摇 → 释放 → 后摇，完全在 MFChar private state |
| OpMap | 行为树事件系统 | events 队列 → ExecBT → 树切换，全在 NPC private state |
| OpMap | Troop 生命周期 | 创建/更新/删除都是 Square owner 的内部操作 |
| OpMap | 技能条件/打断检查 | GetCondVal 只读自身状态，CheckInterrupt 只写自身状态 |
| OpMap | SkillSettleInTickEnd | 怒气累加完全在 target 自身，加法 + 上限 clamp |

### 占比

- 经典游戏：P1 模式触及 73%（22/30）的技能
- OpMap：29/77（37.7%）完全闭环，另有大量逻辑的核心决策部分是闭环的

### 判定口诀

> **如果你能画出一个不跨越任何实体边界的状态机图，它就是 A 类。**

---

## B. 跨 Owner 写模式（Cross-Owner Write Patterns）

### 底层原理

逻辑需要修改**不属于自己的** owner 的状态。这是 ownership 模型的核心约束（C1：单 owner 提交），也是大多数适配改造工作的来源。根据跨 owner 写的**拓扑结构**和**事务性要求**，分为四个子类。

---

### B1. 单向意图投递（Unidirectional Intent）

#### 底层原理

Source 只需要**表达意图**（"我要对你造成 100 点伤害"），不关心 target 的处理结果。Target 在自己的 Apply 中独立裁决最终效果。没有反馈回路。

#### 识别特征

- Source Think 产出 Effect，投递给 target
- Source 不等待 target 的处理结果
- Effect 的"成功"语义锚定在 source（consume-on-cast：释放即扣 CD/资源，不管命中）
- Target Apply 可以独立裁决（格挡、免疫、吸收）

#### 适配策略

**轻度改造。** 将原来的"直接调用 target 方法"改为"Think 产出 typed Effect → target Apply 处理"。

```
Before:  source.skillDamage(target, dmg)     // 直接跨 owner 写
After:   source Think → Publish(DamageEffect{target, dmg})
         target Apply → handle DamageEffect   // target 自行裁决
```

#### 典型案例

| 来源 | 案例 | 说明 |
|------|------|------|
| LOL | Ashe R 全图飞行命中 | 投射物 Logic Think 检测碰撞，产出 StunEffect 给 target |
| DOTA2 | Techies 感应地雷 | 地雷 Logic 检测范围 → DamageEffect → target Apply |
| WOW | 圣盾术 Divine Shield | 自身 Apply 设置 immune flag，外部 DamageEffect 被 Apply 拒绝 |
| OpMap | MultiMoveStop | Player Think → StopMoveEffect → Square Apply |
| OpMap | 建筑升级/取消 | 玩家 Think 验证 → UpgradeEffect → 建筑 Apply 裁决 |
| OpMap | Wind 风场推力 | WindChar Think 计算推力 → PushEffect → target Apply |

#### 占比

这是最常见的跨 owner 写模式。OpMap 中约 60% 的"需要妥协"逻辑链路属于此类。

#### 判定口诀

> **如果 source "打了就跑"、不等回复，它就是 B1。**

---

### B2. 请求-响应协调（Request-Response）

#### 底层原理

Source 发出意图后，需要等待 target 的**反馈**才能完成自身状态转换。形成 "Source Think → Effect → Target Apply → Signal → Source Think" 的往返链路。

#### 识别特征

- Source 的后续行为取决于 target 的处理结果
- 存在明确的 "Effect → Signal 反馈" 回路
- Source 在发出 Effect 后进入"等待状态"，收到 Signal 后才继续

#### 适配策略

**中度改造。** 需要 2 轮 superstep 完成一次交互。Source Think 发出 Effect → barrier → Target Apply 处理并 Emit Signal → barrier → Source 下一轮 Think 消费 Signal。

关键设计点：Source 在等待期间需要处理"响应未到"的情况（超时/默认行为）。

```
Tick N, Superstep 1:  source Think → Publish(RequestEffect) → target
Tick N, Superstep 2:  target Apply → Emit(ResponseSignal) → source
Tick N, Superstep 3:  source Think → consume ResponseSignal → continue
```

如果 3 轮 superstep 不够，溢出到 Tick N+1，延迟约 16–33ms，对玩家通常不可感知。

#### 典型案例

| 来源 | 案例 | 说明 |
|------|------|------|
| LOL | 锤石 Q 连接 → 二段位移 | Source 命中后等待"连接建立"Signal，再决定是否二段飞过去 |
| DOTA2 | Meepo 联动死亡 | 一个分身死亡 → Signal 通知所有分身 → 各自在 Think 中处理死亡 |
| WOW | 守护之魂死亡替代 | Target Apply 检测致死伤害 → 消耗 buff → Signal 回复 source "已触发" |
| WOW | 法术反射 | Target Apply 检查被动 → 拒绝 Effect → Signal 反馈 source "被反射" |
| OpMap | 集结加入（EnterTroop） | Square → RequestEnterRally Effect → 载体 Apply 裁决 → Signal 确认 |
| OpMap | 伤害结算反馈 | Target Apply 处理 DamageEffect → DamageResultSignal → Source 更新仇恨 |

#### 占比

约 15-20% 的跨 owner 交互需要请求-响应模式。

#### 判定口诀

> **如果 source 发完请求后还在"等回信"，它就是 B2。**

---

### B3. 资源预留协议（Reservation Protocol）

#### 底层原理

操作需要**两个或多个 owner 同时成功**才有意义——典型场景是资源交换："A 给 B 100 金币，B 给 A 一把剑"。任何一方失败都需要全部回滚。

框架不支持跨 owner 原子事务，因此需要通过**冻结 → 确认/回滚**的异步协议模拟原子性。

#### 识别特征

- 操作的"成功"依赖多个 owner 各自状态的联合满足
- 任何一方失败需要其他方回滚
- 存在中间的"冻结/锁定"状态

#### 适配策略

**需要设计专用协议。** 三阶段异步流程：

```
Phase 1 — Freeze:
  发起方 Think → 冻结自身资源（private state）
                 → Publish(ReservationEffect) → 协调方 (World)

Phase 2 — Validate:
  协调方 Apply → 验证双方条件是否满足
              → Emit(ConfirmSignal) 或 Emit(RejectSignal)

Phase 3 — Commit/Rollback:
  各参与方 Think → 收到 ConfirmSignal → 正式提交（Apply 中移除冻结、完成交换）
                  或 收到 RejectSignal → 解冻回滚
```

关键设计点：超时处理（协调方未响应时自动回滚）、部分成功处理（N 方参与时 M 方成功的语义）。

#### 典型案例

| 来源 | 案例 | 说明 |
|------|------|------|
| OpMap | DispatchTroop 调兵 | Player 冻结 TroopSlot → World 创建 Square → Player 确认扣减 |
| OpMap | 建造个人建筑 | Player 资源预扣（跨服务已完成）→ World 创建 Char → 失败则回滚 |
| 理论 | 物品交易 | 双方冻结道具/金币 → World 验证 → 同时确认或同时回滚 |
| 理论 | 能量转移 | Source 冻结能量 → Target 确认接收容量 → 双方提交 |

#### 占比

- 经典游戏技能：P5 模式在 30 个技能中 0% 触及——核心战斗不需要原子资源交换
- OpMap：约 5-8% 的逻辑涉及创建/转移操作（调兵、建筑建造），当前多已采用类似 consume-on-cast 的简化模式

#### 判定口诀

> **如果操作是"要么全做、要么全不做"跨多个 owner，它就是 B3。**

#### 重要提示

在实践中，**很多看似 B3 的场景可以降级为 B1（consume-on-cast）**。例如"技能释放扣资源"看似需要"命中才扣"的原子语义，但改为"释放即扣、不管命中"后就变成了单 owner 闭环操作。**优先考虑降级**——只有当双方确实需要联合原子性时才使用 B3。

---

### B4. 扇出广播与级联清理（Fan-out Broadcast）

#### 底层原理

一个事件触发后，需要通知**大量其他 owner** 做出反应——典型场景是死亡清理："目标死亡 → 通知所有攻击者停止追击、通知仇恨系统移除条目、通知集结系统清理、通知视野系统更新"。

这里的核心问题不是单次跨 owner 写（那是 B1），而是**扇出的广度和级联的深度**。

#### 识别特征

- 一个 owner 的状态变更需要"广播"给 N 个不特定的其他 owner
- 各接收方的反应是独立的（彼此之间不依赖）
- 当前代码中表现为"遍历列表，逐个调用方法"的模式

#### 适配策略

**批量 Signal 广播。** 源 owner 在 Apply 中产出一批 Signal，每个 Signal 发送给对应的目标 owner。目标在下一轮 Think 中**各自独立处理**。

```
Dead Square Apply:
  → Emit(TargetDiedSignal) → Attacker_1
  → Emit(TargetDiedSignal) → Attacker_2
  → ...
  → Emit(TargetDiedSignal) → Attacker_N
  → Publish(RemoveCharEffect) → World

Next superstep:
  Each Attacker_i Think: handle TargetDiedSignal → clear target, re-select enemy
  World Apply: handle RemoveCharEffect → update spatial index
```

关键设计点：广播不要求所有接收方在同一 tick 完成处理。攻击者晚 1 tick 才重新选敌（~33ms），玩家完全不可感知。

#### 典型案例

| 来源 | 案例 | 说明 |
|------|------|------|
| DOTA2 | Chronosphere 时间结界 | Faceless Void 产出 FreezeEffect 给区域内所有敌方单位 |
| WOW | 猎人误导（Misdirection） | 仇恨转移信号广播给所有相关仇恨列表持有者 |
| OpMap | Square 死亡清理 | notAliveHandleAttackers → 逐个通知攻击者 → 改为 Signal 广播 |
| OpMap | 取消集结 CancelRally | 遣散所有参与者部队 → StopAndIdle Effect 批量投递 |
| OpMap | 迁城 evacuateClear | 清理追击、集结、警报 → 拆为多个 Signal 给各 owner |

#### 占比

OpMap 中约 15-20% 的"需要妥协"逻辑属于此类。它们的改造模式高度统一。

#### 判定口诀

> **如果代码中有 `for range targets { target.DoSomething() }`，它就是 B4。**

---

## C. 快照时序延迟（Snapshot Temporal Delay）

### 底层原理

在并行 Think 阶段，所有 owner 读取的是 **barrier 前的快照**（world snapshot），而不是实时状态。这意味着 owner A 在当前 superstep 的 Apply 中修改的状态，owner B 在**同一个 superstep** 的 Think 中看不到——要到**下一个 superstep 或下一个 tick** 才可见。

这是并行框架的根本性约束（C3），不可消除，只能通过设计将其影响降至不可感知。

### 识别特征

- 逻辑在决策时需要读取**其他 owner** 的状态（位置、HP、buff、仇恨列表…）
- 逻辑的正确性对读取状态的"新鲜度"有一定要求

### 严重程度分级

#### C-0：无时序敏感（No Temporal Sensitivity）

读取的是**静态或极低频变化**的数据——地形网格、兵种配置表、阵营关系规则。快照与实时完全等价。

> **案例**：CheckUncrossBuilding 碰撞检查（读静态地形）、CanAttack 关系判断（读联盟 ID）
>
> **处理**：无需任何处理。

#### C-1：可容忍时序延迟（Tolerable Delay）

读取的数据在 tick 间会变化，但 1 tick（~16-33ms）的延迟对**玩家体验无可感知影响**。这是最常见的情况。

> **案例**：
> - RVO 避障读取邻居位置（邻居上一帧的位置 vs 当前帧，差距 < 1mm）
> - 仇恨选敌读取目标存活状态（目标可能已死亡但延迟 1 tick 才感知）
> - NPC AI 读取追击目标位置（1 tick 位置偏差对追击行为无影响）
> - DOTA2 Io Tether 治疗同步（延迟 1 tick 的治疗对玩家不可感知）
> - LOL Yi Q 不可选取窗口（进入/退出延迟 1 superstep）
>
> **处理**：直接使用 snapshot 读取，不做额外处理。在设计文档中标注"容忍 1 tick 延迟"。

#### C-2：需要 Apply 端裁决（Apply-Side Adjudication）

读取的数据用于**最终裁决**，但裁决权可以转移到**数据所在的 owner**。这是解决 C3 延迟的核心模式——**谁拥有真相，谁做最终决策**。

> **案例**：
> - 伤害计算需要读取 target 的 buff/护甲 → 伤害公式拆分，防御部分在 target Apply 计算
> - 集结加入需要检查载体容量 → 容量检查在载体 Apply 中执行
> - 建造需要检查位置是否被占 → World Apply 中二次验证
> - WOW 法术反射 → target Apply 检查被动状态，决定是否反射
>
> **处理**：将最终裁决逻辑从 source Think 迁移到 target Apply。Source Think 只构建意图快照（如 AttackTroop 快照），target Apply 基于自身**最新状态**做最终计算。

#### C-3：需要同 tick 即时可见（Same-Tick Visibility Required）

逻辑要求一个 Apply 的结果在**同一 tick 的后续阶段**立即可见——例如"创建 MFChar 后，同一 tick 的 fightPhase 需要遍历到它"。

> **案例**：
> - inputPhasePrepare 创建 MFChar → fightPhasePrepare 需要遍历它
> - 领土 BFS Refresh → 刷新结果需要在同一 tick 的视野计算中可见
>
> **处理方案**：
> - 方案 A：将这些操作收归同一个 owner（如 World）的 Apply 中串行执行，确保 Apply 内部的顺序
> - 方案 B：标记为 serial island，牺牲并行性换取即时可见性
> - 方案 C：接受延迟（如果延迟影响仅为视觉表现差 1 帧）

### 统计数据

- 107 条逻辑中约 56 条（52%）涉及跨 owner 读取
- 其中 95%+ 属于 C-0 或 C-1（不可感知延迟），无需特殊处理
- C-2 约占 3-5%，需要将裁决权迁移到数据 owner
- C-3 < 2%，通常为实体注册/反注册等基础设施操作

### 判定口诀

> **问自己："如果对方的状态延迟一帧才被我看到，游戏行为会有玩家可感知的差异吗？"如果答案是"不会"，那它就不是问题。**

---

## D. 无序安全性（Effect Commutativity）

### 底层原理

同一个 owner 在同一个 superstep 中可能收到**多个同类 Effect**，这些 Effect 以**无序集合**的形式到达 Apply。如果 Apply 的最终结果**依赖 Effect 的处理顺序**，就违反了框架约束 C6（Effect 无序安全 / 交换律）。

核心问题：给定 Effect 集合 {E1, E2, E3}，无论按什么顺序处理，Apply 之后 owner 的状态是否相同？

### 严重程度分级

#### D-0：天然可交换（Naturally Commutative）

Effect 的处理是**纯加法、纯减法、或幂等操作**，任意顺序结果完全相同。

> **案例**：
> - 伤害值累加：HP -= dmg1; HP -= dmg2 = HP -= dmg2; HP -= dmg1 ✓
> - 怒气值累加：Rage += val1; Rage += val2 顺序无关 ✓
> - 仇恨值增加：Hatred += h1; Hatred += h2 顺序无关 ✓
> - 集结标记（BeAtkMap insert）：map[id1]=true; map[id2]=true 顺序无关 ✓
> - 风场推力向量叠加：Force += vec1; Force += vec2 向量加法满足交换律 ✓
> - 建筑升级/取消：状态机保证只有第一个成功，后续被拒——结果无关顺序 ✓
>
> **处理**：无需改造。

#### D-1：可批量化为可交换（Batch-then-Apply）

单个 Effect 的处理不满足交换律，但可以通过**先收集全部 Effect、再统一处理**的方式实现等效的顺序无关。

> **案例**：
> - 护盾吸收（有限量）：先到的伤害先被护盾吸收，后到的打到 HP 上——顺序敏感！
>   → **改造**：Apply 先累计总伤害，再一次性与护盾对比，溢出部分扣 HP
> - 格挡次数限制：先到的攻击被格挡，后到的穿过——顺序敏感！
>   → **改造**：Apply 统计本轮总攻击次数，与格挡次数上限对比，按比例分配格挡
> - 兵力递减影响后续伤害（兵少了护甲降低）：先到的伤害扣兵后影响后续伤害公式
>   → **改造**：Apply 用开始时的兵力快照计算所有伤害，统一扣血
>
> **处理**：Apply 实现改为 "scan → accumulate → commit" 三步骤。

#### D-2：需要确定性排序（Deterministic Ordering）

Effect 之间存在**语义优先级**——某些 Effect 必须先于其他 Effect 生效。但只要 Apply 内部的排序规则是确定性的，仍然满足"任意输入排列 → 相同输出"。

> **案例**：
> - 免疫 Effect vs 伤害 Effect：免疫必须先生效，才能正确拒绝伤害
>   → **改造**：Apply 两阶段扫描（先扫描状态变更类 Effect → 再处理伤害类 Effect）
> - 死亡替代 buff vs 致死伤害：死亡替代必须先注册，才能拦截致死判定
>   → **改造**：Apply 中按 Effect Kind 排序：状态变更 > 伤害 > 通知
> - 形态切换 vs 属性修正：形态切换改变基础属性后，属性修正在新基础上计算
>   → **改造**：Apply 按阶段处理：形态切换 → 属性重算 → 伤害/Buff
>
> **处理**：在 Apply 中定义 Effect 类型优先级表，按优先级排序后处理。这不违反交换律——排序后的处理顺序对任意输入排列都相同。

**这正是框架改进建议中"Effect 分类扫描工具"（C6 两阶段扫描）的需求来源。**

### 统计数据

- D-0 占绝大多数（>80% 的跨 owner 交互）
- D-1 主要出现在伤害结算（护盾、格挡、兵力递减），约 10%
- D-2 主要出现在 buff/免疫/死亡替代场景，约 5-10%
- 没有发现完全不可交换的场景（0%）

### 判定口诀

> **在纸上交换两个 Effect 的处理顺序，看最终 state 是否相同。如果不同，思考是否可以通过"先汇总再统一处理"消除差异。**

---

## E. 级联收敛性（Cascade Convergence）

### 底层原理

一个 Effect/Signal 触发目标的 Apply/Think 后，可能产生新的 Effect/Signal，再触发下一轮处理——形成级联链路。框架对每个 tick 的 superstep 轮数有上限（默认 MaxSupersteps=3），超出部分延迟到下一 tick。

核心问题：级联链路能否在有限 superstep 内收敛？不收敛意味着逻辑在一个 tick 内无法完成，需要跨 tick 延续。

### 严重程度分级

#### E-0：单跳（Single Hop）

Effect 到达 target Apply 后，不产生新的跨 owner 效果。链路深度 = 1。

> **案例**：伤害 Effect → target 扣 HP（结束）。移动命令 → Square 更新位置（结束）。

#### E-1：有界浅链（Bounded Shallow Chain）

链路深度 2-3，可在 MaxSupersteps 内收敛。

> **案例**：
> - 攻击链：Source Think → DamageEffect → Target Apply → DamageResultSignal → Source Think（2 跳）
> - 集结出发：Castle Think → SpawnEffect → World Apply → CreatedSignal → Castle Think（2 跳）
> - Rally 所有权转移：Castle → World → 新 Square（2 跳）

#### E-2：有界深链（Bounded Deep Chain）

链路深度可能 > 3，需要跨 tick 但总步数有界。

> **案例**：
> - Buff 级联触发：Buff A 添加 → 触发被动 → 产生 Buff B → 触发另一个被动 → …
>   统计表明现有 buff 链路最大递归深度约 2-3 层，但理论上可更深
> - 死亡 → 追击清理 → 重新选敌 → 新追击 的全链路可能跨 2-3 tick
>
> **处理**：
> - 验证最大链路深度，确保在合理 tick 数内收敛
> - 超出 MaxSupersteps 的部分自然延迟到下一 tick，通常不影响游戏体验
> - 考虑在关键路径上设计"截断点"——如果链路太长，在某一层停下，剩余部分由 Timer 驱动延迟处理

#### E-3：潜在无界循环（Potentially Unbounded）

存在 A → B → A 的循环 Signal 路径，如果没有终止条件可能无限循环。

> **案例**：
> - 理论场景：A 受伤 → 反伤给 B → B 受伤 → 反伤给 A → A 受伤 → …
>
> **处理**：
> - 在 Logic 层面设计收敛保证：反伤不再触发反伤（深度检查）
> - 利用 MaxSupersteps 自然截断：超出的 Signal 延迟到下一 tick，随着伤害递减自然收敛
> - 设计中标记 `[CASCADE-RISK]`，在测试中重点覆盖

### 统计数据

- E-0 和 E-1 占 90%+
- E-2 主要出现在 buff 系统和死亡清理链路
- E-3 在实际分析的 107 条链路中未出现，但理论上存在（反伤链、治疗链）

### 判定口诀

> **追踪 Signal/Effect 的传播路径：它最多要跳几次才能"安静下来"？跳数 ≤ 3 就是安全的。**

---

## F. 全局序列化需求（Global Serialization）

### 底层原理

某些操作**不可拆分为多个 owner 的独立决策**，必须在全局层面串行执行。框架通过 serial island / World Apply 内串行来支持这类场景。

### 识别特征

- 操作涉及全局不可分割的数据结构（如领土网格的 BFS 洪泛）
- 操作有严格的内部步骤顺序且步骤间有数据依赖
- 结果需要在同一 tick 内对所有 owner 可见

### 典型案例

| 案例 | 原因 | 频率 |
|------|------|------|
| 领土 BFS Refresh | 全局网格洪泛，不可拆分 | 低（建筑变更时） |
| 实体注册/注销（AddChar/RemoveChar） | World 注册表原子性 | 中（每 tick 少量） |
| MFChar 创建/删除链 | CopyExt → NewChar → Trigger → Clone → Remove 有严格顺序 | 中 |
| BossRoom 阶段切换 | 全局可见性要求 | 极低 |
| 迁城 Evacuate → Land | 必须先消失再出现 | 极低 |

### 适配策略

**收归 World Apply 串行执行。** 这些操作天然低频（每 tick 只有少量实体创建/销毁），串行开销极小。

核心原则：**串行域是安全兜底，不是性能陷阱**——只要高频操作（AI 决策、移动、技能、伤害）保持并行，低频的全局串行操作不会成为瓶颈。

### 统计数据

- 经典游戏技能：C5（串行域）触及率 = **0%**——核心战斗逻辑完全不需要
- OpMap：约 12 条逻辑（15%）触及 C5，全部为基础设施操作（实体管理、领土、建筑），无核心战斗路径

### 判定口诀

> **如果操作是"改世界的骨架"（注册表、空间索引、领土网格）而不是"改实体的血肉"（HP、位置、buff），它很可能需要串行。但这没关系——它频率很低。**

---

## 快速判定流程图

下面是面对一段具体游戏逻辑时的完整判定流程：

```
Step 1: 确定 Owner
  "这段逻辑的最终裁决权归谁？"
  → 能找到唯一 owner → 继续
  → 找不到 / 需要多个 owner 联合裁决 → 考虑 B3 或 F

Step 2: 检查状态访问边界
  "它只读写自己的状态吗？"
  → 是 → A 类，直接适配 ✅
  → 否 → 继续

Step 3: 分析跨 owner 写模式
  "它怎么修改别人的状态？"
  → 单向投递，不等回复 → B1
  → 投递后等回复 → B2
  → 需要多方原子成功 → B3（优先考虑降级为 B1 + consume-on-cast）
  → 广播给多个 target → B4

Step 4: 评估读取新鲜度
  "它读取别人状态时，容忍多大延迟？"
  → 静态数据 / 容忍 1 tick → C-0/C-1，无需处理
  → 需要最新值做裁决 → C-2，将裁决权迁移到数据 owner
  → 必须同 tick 可见 → C-3，考虑串行

Step 5: 检查 Effect 无序安全
  "多个同类 Effect 到达同一 owner 时，处理顺序影响结果吗？"
  → 不影响（加法、幂等） → D-0，无需改造
  → 影响，但可以先汇总再统一处理 → D-1
  → 影响，但可以按类型优先级排序消除 → D-2

Step 6: 评估级联深度
  "Effect/Signal 链最多跳几次？"
  → ≤ 1 → E-0
  → 2-3 → E-1，在 MaxSupersteps 内收敛
  → > 3 但有界 → E-2，部分跨 tick，可接受
  → 存在循环 → E-3，需要在 Logic 层面设计截断

Step 7: 检查是否需要全局串行
  "操作是否修改不可拆分的全局数据结构？"
  → 是 → F 类，收归 World Apply
  → 否 → 已完成判定
```

---

## 附录 A：改造模式速查表

| 模式 ID | 模式名称 | 适用分类 | 改造成本 | 一句话描述 |
|---------|---------|---------|---------|----------|
| M1 | Effect 化 | B1, B4 | 低 | 直接调用 → Publish typed Effect |
| M2 | 裁决权迁移 | C-2 | 中 | 将判定逻辑从 source Think 移到 target Apply |
| M3 | consume-on-cast | B3→B1 降级 | 低 | 发起即消耗，不等确认 |
| M4 | 批量累加 | D-1 | 中 | 先收集所有 Effect，再统一处理 |
| M5 | 两阶段扫描 | D-2 | 中 | Apply 按 Effect 类型分优先级处理 |
| M6 | Signal 广播 | B4 | 低 | Apply 产出批量 Signal 通知相关 owner |
| M7 | Timer 替代轮询 | A | 低 | 全局遍历 → 各 owner 自注册 Timer |
| M8 | Reservation 协议 | B3 | 高 | 冻结 → 验证 → 提交/回滚 三阶段 |
| M9 | Serial island | F | 低 | 标记为串行域，在 World Apply 中执行 |
| M10 | 攻方快照 + 守方裁决 | C-2 + D-1 | 高 | Effect 携带攻方快照，守方 Apply 基于最新状态计算 |

---

## 附录 B：从 107 条链路提炼的分类统计

### 经典游戏技能（30 条）

| 分类 | 触及数 | 占比 | 典型案例 |
|------|--------|------|----------|
| A (Owner 闭环) | 16 | 53% | Yasuo Q, Morphling Shift, DK Runes |
| B1 (单向投递) | 10 | 33% | Ashe R, Techies Mines |
| B2 (请求-响应) | 4 | 13% | Thresh Q, Meepo Death, Guardian Spirit |
| B3 (资源预留) | 0 | 0% | — |
| C-1 (可容忍延迟) | 21 | 70% | Yi Q, Chronosphere, Tether |
| D-0 (天然可交换) | 15 | 50% | 所有伤害累加 |
| D-2 (确定性排序) | 5 | 17% | 免疫 + 伤害 |
| F (全局串行) | 0 | 0% | — |

### OpMap 真实业务（77 条）

| 分类 | 触及数 | 占比 | 典型案例 |
|------|--------|------|----------|
| A (Owner 闭环) | 29 | 38% | ModSkill, BT Events, Troop |
| B1 (单向投递) | ~25 | 32% | MoveStop, BuildingUpgrade, Wind |
| B2 (请求-响应) | ~10 | 13% | ReinforceRally, DamageResult |
| B3 (资源预留) | ~4 | 5% | DispatchTroop, BuildPersonalBuilding |
| B4 (扇出广播) | ~9 | 12% | Death cleanup, CancelRally, Evacuate |
| C-1 (可容忍延迟) | ~35 | 45% | RVO, Hatred, AI |
| C-2 (裁决迁移) | ~5 | 6% | skill_attack, EnterTroop |
| D-1 (批量化) | ~8 | 10% | Shield absorb, Block count |
| D-2 (排序) | ~5 | 6% | Buff priority, Death replacement |
| E-2 (深链) | ~3 | 4% | Buff cascade, Death chain |
| F (串行) | ~12 | 16% | Territory BFS, AddChar/RemoveChar |

---

## 附录 C：反模式识别（不可适配的信号）

以下模式是**真正的红旗**——如果逻辑强依赖这些模式且无法改写，则可能无法适配（但在 107 条分析中未出现过）：

| 反模式 | 描述 | 为什么不行 |
|--------|------|----------|
| 跨 owner 原子事务 + 不允许任何穿插 | 必须 A 和 B **同一瞬间**同时变更，中间不允许任何其他 Effect 发生 | 框架无跨 owner 锁，Effect 是无序集合 |
| 强因果 read-your-write 跨 owner | A 写了一个值，B 必须在**同一 superstep** 读到新值并做决策 | barrier 语义使得写后读延迟至少 1 superstep |
| 无界递归级联 + 必须同 tick 收敛 | A→B→A→B→… 无终止条件，且必须在 1 tick 内完成 | MaxSupersteps 有上限 |
| 全局锁 + 高频竞争 | 每 tick 数百个 owner 竞争同一把全局锁 | 框架设计目标是消除全局锁 |

**在实践中**，这些反模式几乎不出现在游戏逻辑中。107 条链路的分析证明了这一点：**0% 无法适配**。原因在于游戏逻辑天然具有 ownership 结构——每条规则总能找到一个明确的真相 owner。

---

## 附录 D：决策树示例

### 示例 1：技能伤害结算

```
Q1: Owner 是谁？
    → 伤害的"真相"：target 裁决最终扣血量（target 知道自己有没有护盾、免疫）
    → 但攻击发起方是 source

Q2: 跨 owner 写吗？
    → 是。Source 需要修改 target 的 HP。
    → 模式：B1（单向意图投递）。Source 不等 target 确认"我扣成功了没"。

Q3: 新鲜度要求？
    → Source 构建攻方快照时，需要读取 target 的护甲/buff —— 但改为 C-2：
      由 target Apply 基于自身最新状态计算防御部分。
    → 改造模式 M10（攻方快照 + 守方裁决）。

Q4: 多个伤害同时到达？
    → 护盾吸收是 D-1（先累计总伤害再对比护盾）。
    → 兵力递减是 D-1（用快照兵力计算所有伤害再统一扣血）。

Q5: 级联深度？
    → 伤害 → 反伤 → 再伤害？理论上 E-3，实践中反伤不再触发反伤，E-1。

结论：B1 + C-2 + D-1 + E-1。需要中度改造，但有成熟模式。
```

### 示例 2：NPC 行为树 AI

```
Q1: Owner 是谁？
    → NPC 自身。行为树栈、变量、决策完全在 NPC private state。

Q2: 跨 owner 写吗？
    → Think 中不写。产出 Effect（MoveIntent、AttackRequest）投递给 target。
    → 模式：A（核心决策是闭环）+ B1（对外输出是单向投递）。

Q3: 新鲜度？
    → 读取敌人位置、仇恨列表 —— C-1（1 tick 延迟对 NPC AI 无感知影响）。

Q4: 无序安全？
    → NPC 每 tick 只产生一个 MoveIntent + 一个 AttackRequest，不存在多 Effect。

Q5: 级联？
    → 无。行为树决策是自包含的。

结论：A + B1 + C-1。直接适配或极低改造成本。数百 NPC 可完全并行。
```

### 示例 3：迁城（CastleMove）

```
Q1: Owner 是谁？
    → 迁城决策：Player。消失/出现：World（因为涉及全局注册表）。

Q2: 跨 owner 写？
    → Player → World（EvacuateEffect）→ World → World（LandEffect）
    → 模式：B1（Player → World）+ F（World 内串行执行）

Q3: 新鲜度？
    → Evacuate 和 Land 分两步（已有跨 RPC 设计），C-0。

Q4: 无序安全？
    → 同一 tick 不会有两次迁城请求到同一 Castle，D-0。

Q5: 级联？
    → Evacuate → 清理追击/集结/警报 → 通知联盟 = B4 扇出广播。
    → 深度约 2-3，E-1/E-2，部分跨 tick。

Q6: 全局串行？
    → RemoveChar / AddChar / 格子变更 = F（World Apply 串行）。

结论：B1 + B4 + E-2 + F。需要中度改造。但迁城频率极低，串行开销可忽略。
```

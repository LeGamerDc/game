# LOL 技能并行 Tick 框架适配分析

> 本文档分析 10 个 League of Legends 技能在 ownership-based 并行 tick 调度框架下的适配情况。
> 框架接口权威参考：`en/world.go`；设计文档参考：`docs/design/parallel.md`。

---

## 1. 盲僧（Lee Sin）- 天音波/回音击（Q）

**描述**：两段技能。Q1 发射音波投射物，命中敌方单位后标记目标；Q2 再次激活，盲僧飞向被标记目标并造成伤害。

**适配判定**：✅ 直接适配

**Owner 归属**：
- Q1/Q2 状态机：Lee Sin Logic（source owner）
- 音波投射物：独立 Logic（投射物 owner）
- 标记状态：存储在 source private state

**执行流程映射**：

- **Think 阶段**：
  - Q1 释放：source Think 检查技能 CD（private state），消耗资源，创建投射物 spawn 请求（Effect → World owner），进入"Q1 已释放"状态，设置 timer 作为 Q2 窗口超时。
  - 投射物 Think：每 tick 读 WorldView 做碰撞检测，命中时 Publish DamageEffect → target owner，Emit HitSignal → source owner。
  - Q2 释放：source Think 收到 HitSignal 后记录标记目标 ref（private state）；玩家再次按 Q 时（外部输入 Signal），Think 产出 DashEffect → self（位移意图）+ DamageEffect → target。
- **Effect 产出**：
  - `SpawnProjectileEffect` → World owner
  - `DamageEffect` → target owner（Q1 命中 + Q2 到达）
  - `DashEffect` → source owner（Q2 位移）
- **Apply 阶段**：
  - World Apply：处理 SpawnProjectileEffect，注册投射物 entity。
  - Target Apply：处理 DamageEffect，扣减 HP。
  - Source Apply：处理 DashEffect，更新位置。
- **Signal 产出**：
  - 投射物命中 → `ProjectileHitSignal` → source owner（通知标记成功）
  - Target Apply 处理伤害后 → `DamageTakenSignal`（可选，用于被动触发）
- **Timer 使用**：Q2 窗口超时（约 3 秒），超时后 private state 重置为 Q1 可用状态。

**触及的约束**：
- **C1**：Q1 命中标记存 source private state，不依赖 target 状态 → 单 owner 真相，无问题。
- **C2**：consume-on-cast，Q1 释放即扣资源进 CD，命中与否不影响资源消耗 → 天然契合。
- **C3**：投射物碰撞读 snapshot，命中结果下一 barrier 后可见 → 可接受（投射物飞行本身跨多 tick）。

**所需改造**：无。

---

## 2. 亚索（Yasuo）- 斩钢闪（Q）

**描述**：普通形态为短距直线刺击，每次命中叠加一层（最多 2 层），叠满 3 层后下次 Q 变为旋风击飞（龙卷风），击中敌人附加击飞效果。层数有时间窗口，超时重置。

**适配判定**：✅ 直接适配

**Owner 归属**：Yasuo Logic（source owner）完全拥有技能状态机和层数。

**执行流程映射**：

- **Think 阶段**：
  - 读 private state 中的 Q 层数和 CD。
  - 层数 < 3：产出近战范围内的 DamageEffect → 命中目标（基于 WorldView 空间查询）。
  - 层数 = 3：产出龙卷风投射物 spawn 请求（Effect → World），或直接对锥形区域目标产出 DamageEffect + AirborneEffect。
  - 更新 private state：层数 +1 或重置、重置层数超时 timer。
- **Effect 产出**：
  - `DamageEffect` → target owner(s)
  - `AirborneEffect`（击飞 CC）→ target owner(s)（仅第 3 层）
  - 若旋风为投射物形态：`SpawnProjectileEffect` → World owner
- **Apply 阶段**：
  - Target Apply：处理 DamageEffect 扣 HP；处理 AirborneEffect 施加击飞状态（CC）。
- **Signal 产出**：
  - Target Apply 可 Emit `DamageTakenSignal` → 相关监听者。
  - 命中 Signal → source（用于层数确认，但 consume-on-cast 模式下可省略）。
- **Timer 使用**：
  - Q CD timer（短 CD，约 1.33 秒受攻速影响）。
  - 层数超时 timer（约 6 秒无命中则重置层数）。

**触及的约束**：
- **C1**：层数完全在 source private state → 单 owner 真相。
- **C2**：层数叠加采用 consume-on-cast 模式——释放即叠层，不依赖命中反馈。如果需要"命中才叠层"，则需要 HitSignal 回传，下一轮 Think 更新层数（**P3** 模式），延迟可接受。
- **C6**：多个 target 同时收到 AirborneEffect + DamageEffect，Apply 无序处理安全（各自独立作用于不同属性）。

**所需改造**：无。

---

## 3. 锤石（Thresh）- 死亡判决（Q）

**描述**：投掷钩锁，命中第一个敌方单位后将其拉向锤石（两次小拖拽），期间可再次激活飞向被钩目标。

**适配判定**：✅ 直接适配

**Owner 归属**：
- 钩锁投射物：独立 Logic（投射物 owner）
- 技能状态机 + 再激活：Thresh Logic（source owner）
- 被钩拉拽状态：target owner（在自己的 Apply 中施加 CC）

**执行流程映射**：

- **Think 阶段**：
  - Q 释放：source Think 消耗资源，Publish SpawnProjectileEffect → World，进入"Q1 飞行中"状态。
  - 投射物 Think：每 tick 碰撞检测，命中后 Publish StunEffect + PullEffect → target，Emit HitSignal → source。
  - Q2 激活：source 收到 HitSignal + 玩家再次按 Q 输入 → Think 产出 DashEffect → self。
  - 拖拽 tick：source Think 可周期性 Publish PullEffect → target（两次小拖拽，用 timer 控制间隔）。
- **Effect 产出**：
  - `SpawnProjectileEffect` → World owner
  - `StunEffect` → target owner（定身）
  - `PullEffect` → target owner（位移拖拽）
  - `DashEffect` → source owner（Q2 飞向目标）
- **Apply 阶段**：
  - Target Apply：施加定身 CC，处理 PullEffect 更新位置。
  - Source Apply：处理 DashEffect 更新位置。
- **Signal 产出**：
  - `ProjectileHitSignal` → source（标记命中，开放 Q2 窗口）
  - `CCAppliedSignal` → source（可选，确认 CC 生效）
- **Timer 使用**：
  - Q2 窗口超时 timer。
  - 两次拖拽间隔 timer（投射物/source 定时 Think 产出连续 PullEffect）。

**触及的约束**：
- **C1**：拖拽位移由 target Apply 执行（target 是位置的真相 owner） → 正确。
- **C3**：钩锁命中后 source 下一轮才知道 → 短暂延迟，实际游戏中感知不到（投射物本身就有飞行时间）。
- **C6**：target 如果同时收到多个 PullEffect（极端情况），Apply 按矢量叠加或覆盖处理即可。

**所需改造**：无。

---

## 4. 卡特琳娜（Katarina）- 贪婪利刃（被动）

**描述**：击杀或助攻后，所有技能冷却时间立即刷新（重置为 0）。

**适配判定**：✅ 直接适配

**Owner 归属**：Katarina Logic（source owner）。CD 是 private state，完全自主可控。

**执行流程映射**：

- **Think 阶段**：
  - 正常 tick：Think 中检查 Inbox 是否包含 `KillSignal` 或 `AssistSignal`。
  - 若收到：将 private state 中所有技能 CD 重置为 0。
  - 继续正常技能决策逻辑。
- **Effect 产出**：无特殊 Effect（被动本身不产出 Effect，只修改 private state）。
- **Apply 阶段**：无特殊处理（CD 是 private state，不经过 Apply）。
- **Signal 产出**：
  - `CDResetSignal` → self（可选，用于 UI/日志通知）。
- **Timer 使用**：无额外 timer 需求。

**触及的约束**：
- **C1**：CD 属于 private state，只有 owner 自己的 Think 可修改 → 完美契合。
- **C3**：击杀/助攻 Signal 来源：
  - 方案 A：target Apply 处死时 Emit `DeathSignal` → World owner；World Apply 计算助攻列表，Emit `KillSignal`/`AssistSignal` → 相关 owner。下一轮 Think 刷新 CD。
  - 方案 B：target Apply 直接 Emit `KillCreditSignal` → 击杀者和助攻者（如果 target 维护了伤害来源列表）。
  - 两种方案都有 1-2 轮 superstep 延迟，在实际游戏中可接受（击杀结算本身跨帧）。
- **C7**：同一 superstep 收到多个 KillSignal（多杀），CD 重置是幂等操作 → 安全。

**所需改造**：无。击杀/助攻归属系统需要能产出对应 Signal，但这是基础设施层面的需求，不是卡特被动的特殊改造。

---

## 5. 寒冰射手（Ashe）- 鹰击长空（R）

**描述**：发射全图飞行的魔法水晶箭，命中第一个敌方英雄造成伤害并眩晕，眩晕时长与箭矢飞行距离成正比（最短 1 秒，最长 3.5 秒）。

**适配判定**：✅ 直接适配

**Owner 归属**：
- 箭矢投射物：独立 Logic（投射物 owner）
- 技能释放：Ashe Logic（source owner）

**执行流程映射**：

- **Think 阶段**：
  - 释放：source Think 消耗资源 + 进 CD，Publish SpawnProjectileEffect → World。投射物初始化时记录发射原点坐标（投射物 private state）。
  - 投射物 Think：每 tick 更新位置（private state），读 WorldView 碰撞检测。命中时计算飞行距离（当前位置 - 发射原点），据此算出眩晕时长，Publish StunEffect(duration) + DamageEffect → target，Emit HitSignal → source，请求 World 销毁自身。
- **Effect 产出**：
  - `SpawnProjectileEffect` → World owner（含方向、速度、发射原点）
  - `DamageEffect` → target owner
  - `StunEffect(duration)` → target owner（duration 由飞行距离决定）
  - `DestroyEntityEffect` → World owner（投射物命中后销毁）
- **Apply 阶段**：
  - Target Apply：处理 DamageEffect 扣 HP，处理 StunEffect 施加眩晕 CC（duration 已编码在 Effect 中）。
  - World Apply：处理投射物 spawn 和 destroy。
- **Signal 产出**：
  - `ProjectileHitSignal` → source（可选，用于后续逻辑如触发被动）
- **Timer 使用**：
  - 投射物不需要外部 timer，靠每 tick Think 自驱动（返回 delay=1 持续飞行）。
  - 可设最大飞行时间 timer 作为安全网（超远距离自动销毁）。

**触及的约束**：
- **C1**：眩晕时长计算完全在投射物 Think 中完成（读自己 private state 的发射原点 + 当前位置），不依赖任何跨 owner 查询 → 单 owner 真相。
- **C2**：consume-on-cast，释放即扣资源进 CD，箭矢命中与否不影响 → 天然契合。
- **C3**：投射物碰撞检测基于 WorldView snapshot → 极端情况下目标可能在 snapshot 拍摄后闪现走了，但这是所有投射物的共性问题，可接受。

**所需改造**：无。

---

## 6. 剑圣（Master Yi）- 阿尔法突袭（Q）

**描述**：Master Yi 进入不可选取（untargetable）状态，在最多 4 个目标间弹跳攻击，每次弹跳造成伤害，结束后出现在最后一个目标旁边。技能期间 Yi 从地图上"消失"。

**适配判定**：⚠️ 需要妥协

**Owner 归属**：
- 技能状态机 + 弹跳序列：Master Yi Logic（source owner）
- untargetable 状态：source public state（由 source Apply 设置）

**执行流程映射**：

- **Think 阶段**：
  - 释放：source Think 消耗资源 + 进 CD，Publish `SetUntargetableEffect` → self，选择弹跳目标列表（读 WorldView 空间查询），存入 private state。设置 timer 控制弹跳节奏。
  - 弹跳过程：每次 timer 触发 Think，从 private state 取出下一个目标，Publish DamageEffect → 当前弹跳目标。
  - 结束：最后一次弹跳后，Publish `ClearUntargetableEffect` + `TeleportEffect`（位置更新）→ self。
- **Effect 产出**：
  - `SetUntargetableEffect` → self（进入不可选取）
  - `DamageEffect` → target owner(s)（每次弹跳一个）
  - `ClearUntargetableEffect` → self（结束时恢复可选取）
  - `TeleportEffect` → self（出现在最后目标旁）
- **Apply 阶段**：
  - Source Apply：处理 untargetable 状态切换（public state），处理位置更新。
  - Target Apply：处理 DamageEffect 扣 HP。
- **Signal 产出**：
  - 各 target 的 `DamageTakenSignal`（用于被动/仇恨等）
- **Timer 使用**：
  - 弹跳间隔 timer（每跳约 0.231 秒 → 约 tick 级别间隔）。
  - 总持续时间安全 timer。

**触及的约束**：
- **C3**：弹跳期间目标可能死亡或移出范围。source Think 读 snapshot 决定目标，但目标可能在 barrier 后才显示为已死亡。
  - **应对**：弹跳目标列表在释放时预选，但每跳时重新验证目标存活状态（读 snapshot）。如果目标已死，跳过该目标或选择新目标。
- **C1**：untargetable 是 source public state，只有 source Apply 能改 → 正确。但其他 Logic 的 Think 读 snapshot 可能在设置 untargetable 之前拍摄的 snapshot，导致该 tick 内仍尝试对 Yi 释放技能。
  - **应对**：target 技能命中 Yi 时，Yi 的 Apply 因 untargetable 状态直接丢弃/无效化该 Effect → Effect 无序安全（**C6**）。

**所需改造**：
- 弹跳目标选择需要容错逻辑：每跳前重新验证 snapshot 中目标有效性（alive + targetable）。
- untargetable 检查需要在 Apply 端做防御：即使收到 DamageEffect，如果 public state 已是 untargetable 则丢弃。但注意：**这个检查在 target Apply 端** → Yi 的 untargetable 是 source public state，其他 owner 的 Think 通过 WorldView 读到 Yi 的 public state。如果 snapshot 滞后一个 barrier，存在极短窗口的误命中，由 Yi 自身 Apply 拒绝即可（Yi 不处理该伤害 Effect 即可——但伤害 Effect 是发给 target 的，Yi 是 source）。

  实际上更准确的模型：**其他英雄尝试攻击 Yi 时，在 Think 阶段读 snapshot 看到 Yi 已是 untargetable，直接不选择 Yi 作为目标**。极端窗口（同 tick 刚变 untargetable）的"幽灵命中"在 LOL 原版中同样存在，可接受。

---

## 7. 莫甘娜（Morgana）- 黑暗禁锢（R）

**描述**：莫甘娜标记周围一定范围内的所有敌方英雄并施加减速，建立锁链连接。3 秒后，仍在锁链范围内的敌人被眩晕并受到第二段伤害。敌人如果在 3 秒内走出范围则锁链断裂，不受第二段效果。

**适配判定**：⚠️ 需要妥协

**Owner 归属**：
- R 技能状态机 + 锁链追踪：Morgana Logic（source owner）
- 减速/眩晕 CC：target owner(s)

**执行流程映射**：

- **Think 阶段**：
  - R 释放：source Think 读 WorldView 空间查询范围内敌人，记录初始标记目标列表（private state），Publish SlowEffect → 各目标，Publish DamageEffect → 各目标（第一段伤害）。设置 timer = 3 秒。
  - 3 秒后 timer 触发 Think：重新读 WorldView 获取各标记目标当前位置，计算与 Morgana 的距离。仍在范围内的目标：Publish StunEffect + DamageEffect → target。超出范围的目标：不产出 Effect（锁链断裂）。
- **Effect 产出**：
  - `SlowEffect` → target owner(s)（第一阶段减速）
  - `DamageEffect` → target owner(s)（两阶段各一次）
  - `StunEffect` → target owner(s)（第二阶段眩晕，仅范围内目标）
- **Apply 阶段**：
  - Target Apply：处理 SlowEffect 施加减速，处理 DamageEffect 扣 HP，处理 StunEffect 施加眩晕。
- **Signal 产出**：
  - `ChainBreakSignal` → source（可选，用于 UI 反馈）
  - `DamageTakenSignal` → 相关监听者
- **Timer 使用**：
  - 3 秒延迟 timer 用于第二阶段判定（核心机制）。

**触及的约束**：
- **C3**：第二阶段判定时读 snapshot 获取目标位置 → snapshot 可能滞后一个 barrier。在极端边缘情况下（目标恰好在 barrier 窗口内走出/走入范围），判定可能有微小偏差。
  - **应对**：这是所有基于距离判定的技能的共性问题。LOL 本身的判定也有 server tick 精度限制，可接受。适当扩大/缩小判定半径做容差即可。
- **C1**："仍在范围内"的判定在 source Think 中完成（读 snapshot 中 target 位置 vs 自己位置） → 单 owner 决策，正确。
- **C6**：多个目标同时收到 StunEffect，各自 Apply 独立处理 → 无序安全。

**所需改造**：
- 第二阶段判定依赖"Morgana 自身当前位置" → Morgana 的位置是 source public state，Think 中可通过 WorldView 读取自己的 public state snapshot。需确认框架支持 owner 在 Think 中读取自己的 public state（通过 WorldView 而非 private state）。
  - 如果不支持，可将 Morgana 释放 R 时的位置缓存在 private state，或在每次 Think 中通过某种机制同步自身位置到 private state。实际上 Think 通过 `WorldView` 读取世界快照（包含所有 entity 的 public state），所以 Morgana Think 读自己的 public state 位置是标准操作 → **无需额外改造**。

---

## 8. 劫（Zed）- 禁奥义·瞬狱影杀阵（R）

**描述**：劫变为不可选取状态冲向目标，标记目标（死兆星印记），3 秒内劫及其影子对目标造成的所有伤害被记录，3 秒后引爆印记，造成记录伤害的一定百分比作为额外伤害。

**适配判定**：⚠️ 需要妥协

**Owner 归属**：
- R 技能状态机 + 伤害累积记录：需要仔细分析 → **方案选择见下文**
- 死兆星印记：target public state 或 source private state

**执行流程映射**：

**方案 A：伤害记录存 source private state（推荐）**

- **Think 阶段**：
  - R 释放：source Think 消耗资源 + 进 CD，Publish `DashEffect` → self + `SetUntargetableEffect` → self。记录标记目标 ref + 初始化伤害累积器 = 0（private state）。设置 timer = 3 秒。
  - 每次对标记目标造成伤害时：source Think Publish DamageEffect → target，**同时**在 private state 累积伤害值。
  - 3 秒后 timer 触发 Think：读取累积伤害值，计算额外伤害，Publish `DeathMarkDetonateEffect(bonus_damage)` → target。
- **Effect 产出**：
  - `DashEffect` → self（冲刺到目标身后）
  - `SetUntargetableEffect` / `ClearUntargetableEffect` → self
  - `DamageEffect` → target（常规伤害，多次）
  - `DeathMarkDetonateEffect` → target（3 秒后的额外伤害）
- **Apply 阶段**：
  - Target Apply：处理各种 DamageEffect 扣 HP。
  - Source Apply：处理位移、untargetable 状态切换。
- **Signal 产出**：
  - `DeathMarkAppliedSignal` → target（可选，用于 UI 显示印记）
  - `DamageTakenSignal` → 相关监听者
- **Timer 使用**：
  - 3 秒印记引爆 timer（核心机制）。
  - 短暂 untargetable 窗口 timer（R 冲刺期间）。

**触及的约束**：
- **C1**：伤害累积存 source private state → 单 owner 真相。但有一个微妙问题：**劫的影子（W 技能产生的分身）也可以对标记目标造成伤害**，这些伤害也需要累积。
  - **应对**：影子作为独立 Logic 或 source 的子逻辑。如果是子逻辑（P1 模式），伤害发出时 source 自然知道并累积。如果是独立 Logic（P2 模式），影子命中时 Emit `ShadowDamageSignal` → source，source 下一轮 Think 累积。
  - 跨 owner 累积有 1 轮 superstep 延迟 → 对 3 秒窗口来说完全可以忽略。
- **C2**：伤害累积是"先记录后结算"，不涉及跨 owner 资源交换 → 安全。
- **C3**：引爆时刻读 snapshot 看目标状态 → 目标可能已死亡，此时引爆无意义，target Apply 不处理即可。

**所需改造**：
- 影子（W）如果作为独立 Logic，需要 Signal 协议将伤害值回报给 source，用于累积。
- 替代方案：将影子造成的伤害也走 source 的 Think 发出（影子作为 source 的子状态机），避免跨 owner 通信 → **推荐此方案**，直接适用 **P1**。

---

## 9. 女枪（Miss Fortune）- 弹雨（R）

**描述**：Miss Fortune 原地引导（channeling），持续约 3 秒，对锥形区域造成多段伤害（约 12-14 波）。引导期间不能移动或使用其他技能，被打断则提前结束。

**适配判定**：✅ 直接适配

**Owner 归属**：Miss Fortune Logic（source owner）

**执行流程映射**：

- **Think 阶段**：
  - R 释放：source Think 消耗资源 + 进 CD，进入"引导中"状态（private state），记录引导方向和剩余波数。设置 timer = 每波间隔（约 0.25 秒）。
  - 每波 timer 触发 Think：
    - 检查 Inbox 是否有 `InterruptSignal`（被打断）。如有 → 结束引导，清除状态。
    - 读 WorldView 空间查询锥形区域内的敌人，Publish DamageEffect → 各目标。
    - 剩余波数 -1。波数归零 → 结束引导。
    - 返回 timer = 下一波间隔。
- **Effect 产出**：
  - `DamageEffect` → 锥形区域内各 target owner（每波独立产出）
- **Apply 阶段**：
  - Target Apply：处理 DamageEffect 扣 HP。
  - Source Apply：如果收到 CC Effect（眩晕/击飞等），施加 CC 状态 → 下一轮 Think 检测到 CC 状态时中断引导。
- **Signal 产出**：
  - `DamageTakenSignal` → 相关监听者（每波每目标）
  - 打断来源：敌方 CC Effect → source Apply 施加 CC → source Apply Emit `CCAppliedSignal` → self → 下一轮 Think 中断引导。
- **Timer 使用**：
  - 每波伤害间隔 timer（核心驱动机制）。
  - 最大引导时间安全 timer。

**触及的约束**：
- **C1**：引导状态完全在 source private state → 单 owner 真相。
- **C3**：每波目标选择基于 snapshot → 目标可能在 barrier 后移入/移出锥形区域，精度为 tick 级别，可接受。
- **C6**：同一 target 同一波只收到一个 DamageEffect → 无序问题不存在。多波之间是跨 tick 的 → 时序天然保证。
- **C4**：引导被打断的信号链：敌方 Think → CC Effect → source Apply → InterruptSignal → source Think 中断。需要至少 2 轮 superstep。如果跨 tick，可能多输出一波伤害。
  - **应对**：可接受。LOL 原版中也存在"被 CC 后多打出一发"的边界情况（server tick 精度问题）。在本框架 superstep 模型下，如果在同 tick 内完成则可及时中断。

**所需改造**：无。

---

## 10. 炸弹人（Ziggs）- 短导引信（W）

**描述**：放置一个炸弹，延迟后爆炸或再次激活提前引爆。爆炸效果：
- 炸飞区域内的敌人（位移 + 微量伤害）
- 如果 Ziggs 自己在爆炸范围内，炸飞自己（用于跳跃地形）
- 特殊交互：可以对低血量防御塔使用，直接处决（拆塔）

**适配判定**：⚠️ 需要妥协

**Owner 归属**：
- W 炸弹：独立 Logic（投射物/陷阱 owner）或 source 子状态机
- 拆塔判定：需要读 target（塔）HP → 真相归属需分析

**执行流程映射**：

- **Think 阶段**：
  - W 释放：source Think 消耗资源 + 进 CD，Publish SpawnTrapEffect → World。记录炸弹 ref（private state）。设置自动引爆 timer。
  - 再次激活/timer 触发：炸弹 Think 引爆 → 读 WorldView 空间查询爆炸范围内的单位。
    - 敌方单位：Publish KnockbackEffect + DamageEffect → 各 target。
    - Ziggs 自身在范围内：Publish KnockbackEffect → source（自我炸飞跳跃）。
    - 防御塔且 HP 低于阈值：Publish `ExecuteTowerEffect` → 塔 owner。
- **Effect 产出**：
  - `SpawnTrapEffect` → World owner
  - `DamageEffect` → target owner(s)
  - `KnockbackEffect` → target owner(s) + 可能 source owner
  - `ExecuteTowerEffect` → 塔 owner（特殊处决）
  - `DestroyEntityEffect` → World owner（炸弹自毁）
- **Apply 阶段**：
  - Target Apply：处理 KnockbackEffect 更新位置 + DamageEffect 扣 HP。
  - Source Apply：处理 KnockbackEffect 更新自身位置（跳跃）。
  - 塔 Apply：处理 ExecuteTowerEffect → 如果当前 HP 确实低于阈值则销毁，否则忽略。
  - World Apply：处理 spawn/destroy。
- **Signal 产出**：
  - `TowerDestroyedSignal` → World / 相关监听者（塔被拆除）
- **Timer 使用**：
  - 炸弹自动引爆 timer（约 4 秒）。
  - 再次激活通过外部输入 Signal → source Think → Emit DetonateSignal → 炸弹 Think。

**触及的约束**：
- **C3**：拆塔判定 — 炸弹 Think 读 snapshot 中塔的 HP 来决定是否可以处决。**snapshot 可能滞后**：塔在当前 tick 被其他 source 打到阈值以下，但 snapshot 中尚未反映。
  - **应对**：将处决判定移至**塔 Apply 端**——炸弹 Think 总是 Publish `ExecuteTowerEffect` → 塔，塔 Apply 检查自己当前 HP 是否低于阈值，满足则执行销毁。这样真相 owner 是塔自身 → **符合 C1**。
  - 但如果塔 HP 高于阈值，`ExecuteTowerEffect` 被忽略，此时 W 应该仅造成普通伤害 + 击退效果。解决方案：**同时发送** `ExecuteTowerEffect` 和 `DamageEffect + KnockbackEffect` → 塔 Apply 内部决定走哪条路径（处决 or 普通伤害）。
- **C1**：位移（KnockbackEffect）由各 target 自己的 Apply 处理 → 正确。Ziggs 自己的 Apply 处理自我弹飞 → 正确。
- **C6**：多个目标同时收到 KnockbackEffect，各自独立处理 → 无序安全。

**所需改造**：
- 拆塔处决逻辑从"source 判定"改为"target（塔）Apply 端判定"。炸弹 Think 始终发送 `TowerInteractionEffect`（包含处决信息），塔 Apply 内部根据当前 HP 决定是执行处决还是普通伤害。这避免了 **C3** snapshot 滞后问题。
- 再次激活（提前引爆）的通信链：玩家输入 → source Think → DetonateSignal → 炸弹 Think → 引爆 Effect。需要 2 轮 superstep，在同 tick 内通常可完成（**C4** 尽力而为足够）。

---

## 统计摘要

| # | 英雄 | 技能 | 判定 | 主要模式 | 关键约束 |
|---|------|------|------|----------|----------|
| 1 | 盲僧 Lee Sin | Q 天音波/回音击 | ✅ 直接适配 | P1+P2 | C1, C2, C3 |
| 2 | 亚索 Yasuo | Q 斩钢闪 | ✅ 直接适配 | P1 | C1, C2, C6 |
| 3 | 锤石 Thresh | Q 死亡判决 | ✅ 直接适配 | P1+P2 | C1, C3, C6 |
| 4 | 卡特琳娜 Katarina | 被动 贪婪利刃 | ✅ 直接适配 | P1+P3 | C1, C3, C7 |
| 5 | 寒冰 Ashe | R 鹰击长空 | ✅ 直接适配 | P2 | C1, C2, C3 |
| 6 | 剑圣 Master Yi | Q 阿尔法突袭 | ⚠️ 需要妥协 | P1+P2 | C1, C3, C6 |
| 7 | 莫甘娜 Morgana | R 黑暗禁锢 | ⚠️ 需要妥协 | P1 | C1, C3, C6 |
| 8 | 劫 Zed | R 瞬狱影杀阵 | ⚠️ 需要妥协 | P1+P6 | C1, C2, C3 |
| 9 | 女枪 Miss Fortune | R 弹雨 | ✅ 直接适配 | P1 | C1, C3, C4, C6 |
| 10 | 炸弹人 Ziggs | W 短导引信 | ⚠️ 需要妥协 | P1+P2 | C1, C3, C4, C6 |

### 汇总

| 判定 | 数量 | 技能 |
|------|------|------|
| ✅ 直接适配 | 6 | Lee Sin Q, Yasuo Q, Thresh Q, Katarina 被动, Ashe R, MF R |
| ⚠️ 需要妥协 | 4 | Master Yi Q, Morgana R, Zed R, Ziggs W |
| ❌ 无法适配 | 0 | — |

### 关键发现

1. **无一技能被判定为无法适配**。所有 10 个技能都可以在框架内实现，差异仅在于是否需要额外的设计模式适配。

2. **最常用的模式**：
   - **P1（单 owner 状态机）**：10/10 技能使用，是绝对基础模式。
   - **P2（投射物/延迟效果独立 Logic）**：5/10 技能使用（Lee Sin Q, Thresh Q, Ashe R, Master Yi Q 的弹跳可建模为此, Ziggs W）。
   - **P3（被动触发）**：1/10 直接使用（Katarina 被动），但 Signal 驱动的反馈机制在多个技能中隐式使用。
   - **P6（链式技能/combo）**：1/10 直接使用（Zed R 的伤害累积 + 引爆）。

3. **最常触及的约束**：
   - **C1（单 owner 提交）**：10/10 — 每个技能都需要明确真相 owner。
   - **C3（barrier 可见性）**：8/10 — 几乎所有涉及跨 owner 交互的技能都需要考虑 snapshot 滞后。
   - **C6（Effect 无序安全）**：5/10 — 多目标技能或复合 Effect 需要确保 Apply 端无序安全。

4. **妥协技能的共性**：
   - **状态窗口问题**（C3）：untargetable、距离判定、HP 阈值判定等依赖最新状态的逻辑，在 snapshot 模型下有极短窗口的不精确。所有妥协都可通过"将判定移至真相 owner Apply 端"来解决。
   - **跨 Logic 累积**：Zed R 的伤害累积涉及影子 Logic 的伤害回报，推荐将影子建模为 source 子状态机（P1）而非独立 Logic 来避免通信开销。

5. **Timer 是核心驱动力**：10/10 技能都使用 timer（CD 管理、延迟效果、引导节奏、状态窗口超时），验证了 timer wheel 作为基础设施的必要性。

6. **consume-on-cast 是默认最佳实践**：所有资源消耗（MP、CD）都采用释放即扣模式，完美契合 C2 约束，无需跨 owner 事务。

### 对框架的建议

- **空间查询 API**：多个技能依赖 Think 阶段的空间查询（范围内敌人、锥形区域、碰撞检测）。`WorldView` 需要提供高效的只读空间索引接口。
- **CC 系统标准化**：眩晕、击飞、减速、击退等 CC 效果在多个技能中出现，建议标准化为一组 Effect Kind + Apply 端 CC 状态机。
- **投射物生命周期**：5 个技能涉及投射物，建议提供标准化的投射物 Logic 模板（spawn/fly/collide/destroy），减少重复代码。
- **untargetable / invulnerable 状态**：作为 public state flag，需要在框架层面标准化，确保所有 Think 的 WorldView 查询能正确过滤。
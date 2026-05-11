# Demo Combat / Ability Framework 设计稿

> 本文是 `demo/` 战斗与技能框架的实现前设计。它描述下一步代码应如何组织，不包含实现代码。
>
> 如果本文与当前代码不一致，以 `sched/world.go`、`sched/integration.md` 和后续实际代码为准。

Last Updated: 2026-05-09

---

## 1. 设计目标

本 demo 的目标不是复刻完整 GAS，而是用一个足够具体的战斗场景展示 `sched` 的正确接入方式：

- `Unit` 是主要 `Logic = Owner`。
- 跨 Unit 状态修改全部走 typed `Effect`。
- `Signal` 只用于唤醒 Unit 的 Think 和驱动被动/事件反应。
- Unit 的公开事实由 Apply 提交，再通过 `StagedState` 投射为 World 查询视图。
- 普通攻击、技能、buff 各自维护自己的下一次 Think 时间，Unit 汇总后返回最小 delay。
- 测试文件与框架文件分离；框架文件按职责拆分，避免把 Unit/技能/buff/世界查询塞进单个文件。

初版暂不引入 `SerialRef`。如果后续需要全局结算或排行榜归并，再单独设计 apply-only dispatch。

---

## 2. 与 Scheduler 的映射

### 2.1 Owner

| Owner | Ref 类型 | 责任 |
|------|----------|------|
| Unit | NormalRef | 战斗实体、普通攻击、技能槽、buff、死亡/复活 |
| World | RefWorld | 创建 n x n Unit、维护注册表、维护 staged query read model |

Projectile 暂不作为独立 Logic。普通攻击/技能产生的延迟命中先由 source Unit 私有 pending-impact 队列持有，到期后由 source Think `Publish(target, DamageEffect)`。这样可以先把 demo 主线聚焦在 Unit/Effect/Signal/StagedState 上。未来若要展示投射物生命周期，再引入 Projectile Logic。

### 2.2 Public / Private 数据边界

| 数据 | 归属 | Think | Apply | 对其他 Unit 可见方式 |
|------|------|-------|-------|----------------------|
| HP/Mana/MaxHP/Attack/Defense/AttackRange/AttackSpeed | Unit public | 只读当前 owner 稳定值 | 权威修改 | `StageUnitSummary` / `StageAttrSummary` |
| Pos/Team/LifeState/PublicTags | Unit public | 只读稳定值 | 权威修改 | `StageUnitSummary` |
| 普攻状态、索敌窗口、pending impacts | Unit private | 读写 | 通常不写 | 不暴露 |
| 技能槽状态、ready queue、当前施法阶段 | Unit private | 读写 | 通常不写，必要时通过 self signal 协调 | 不暴露 |
| Buff 实例和内部计时 | Unit private | 驱动计时、产生 self/cross-owner effect | Apply 提交公开 modifier/tag 生命周期 | 只暴露投射后的属性/tag |
| World 查询索引 | World staged read model | 只读查询 | `PromoteStages` 串行更新 | World query API |

原则：Think 可以推进 owner 的私有运行状态；任何会改变公开事实的动作都应变成 self/cross-owner `Effect`，由 Apply 提交。比如 buff 到期时，Think 不直接删除公开 modifier，而是发布 self `RemoveBuffEffect`，Apply 再清理 modifier/tag 并写 staged summary。

---

## 3. 时间模型

游戏配置使用秒，Scheduler timer 使用 tick delay。

```text
TickSeconds = 0.125
TicksPerSecond = 8
World.Now() = 当前 tick 序号，Think/Apply 中保持稳定
NowSec = float64(World.Now()) * TickSeconds
```

事件使用连续时间戳 `deadlineSec` 保存，发生在第一个 `tickTime >= deadlineSec` 的 tick。转换为 Scheduler delay 时使用向上取整：

```text
delayTicks = ceil((deadlineSec - NowSec) / TickSeconds)
```

Unit.Think 先循环处理所有 `deadlineSec <= NowSec` 的内部事件，再把下一个未来 deadline 转成 `delayTicks` 返回。`delay <= 0` 在 Scheduler 中表示不自动唤醒，所以框架不依赖 `0` delay 触发同 tick 重入。

为了避免累积误差，冷却和弹道时间从原始动作时间戳计算，而不是从实际处理 tick 的时间计算：

```text
attackFireAt      = windupEndAt
nextAttackReadyAt = attackFireAt + 1 / attackSpeedAtFire
impactAt          = attackFireAt + distance / projectileSpeed
skillCooldownAt   = castCommitAt + cooldownSeconds
```

如果 `windupEndAt` 落在两个 tick 之间，攻击会在后一个 tick 被处理，但 `nextAttackReadyAt` 仍从 `windupEndAt` 计算。

---

## 4. World 与 Staged 查询

### 4.1 n x n 初始化

World 创建 `n * n` 个 Unit，位置为：

```text
Pos = (float64(x), float64(y))
Team = (x + y) % 2
spacing = 1.0
```

Unit ref 应稳定可复现，建议由 grid index 编码或从 1 开始顺序分配，保持 `< sched.RefWorld`。

### 4.2 StageKind

初版建议：

| StageKind | State | 用途 |
|-----------|-------|------|
| `StageUnitSummary` | `UnitSummary` | 目标选择、死亡/阵营/位置查询 |
| `StageAttrSummary` | `AttrSummary` | 公开属性查询，后续可与 UnitSummary 合并或拆分 |

`UnitSummary` 至少包含：

```text
Ref, Team, Pos, Alive, Targetable, Hp, MaxHp, PublicTagsVersion
```

`AttrSummary` 至少包含：

```text
Attack, Defense, AttackRange, AttackSpeed, MaxHp, MaxMana
```

### 4.3 空间查询

目标选择通过 World 稳定查询完成，不直接读取目标 Unit 指针：

```text
QueryEnemiesInRange(sourceRef, pos, team, radius) -> []UnitSummary / []ref
```

因为 demo 初始是规则网格，初版可以维护一个格子索引，按 `ceil(radius)` 扫描邻近格子，再用平方距离过滤。这样 n 较大时不会退化成所有 Unit 互扫。

`PromoteStages` 串行更新 summary map 和空间索引。Think 期间 World query 只返回值拷贝或只读 summary，不暴露可写内部结构。

---

## 5. Unit 结构拆分

Unit 不是一个大而全的脚本，而是几个 owner-local 子系统的组合：

```text
Unit
+-- public
|   +-- id, pos, team, lifeState
|   +-- attrs attr.Table
|   +-- public tags / flags
|
+-- private
    +-- rng
    +-- BasicAttackController
    +-- AbilitySystem
    +-- BuffTable
    +-- DeathController
    +-- pending impact heap
```

Unit.Think 的职责是 orchestration：

1. 消费 signals，更新私有反应状态。
2. 推进 pending impacts，到期则 publish damage/heal/effect。
3. 推进 buff timers，到期则 publish self/cross-owner effects。
4. 推进技能 cooldown/passive triggers，把 ready slot 放入施法队列。
5. 如果当前普通攻击或技能施法阶段到期，执行阶段转换。
6. 如果空闲且施法队列非空，尝试释放下一个技能。
7. 如果施法队列为空，进入普通攻击行为。
8. 汇总普通攻击、技能、buff、pending impact、死亡复活等子系统的 next deadline，返回最小 delay。

Unit.Apply 的职责是 reducer：

1. 消费 Damage/Heal/Buff/Revive 等 effects。
2. 修改 public attrs/tags/lifeState。
3. 触发死亡、受击、命中、技能事件对应的 signals。
4. flush attribute modifier 聚合。
5. 写 `StageUnitSummary` / `StageAttrSummary`。

---

## 6. 普通攻击

### 6.1 状态

普通攻击作为 Unit 内部子系统，不作为技能槽：

```text
BasicAttackController
+-- phase: Idle | Windup | Cooldown
+-- targetRef
+-- searchExpireAtSec
+-- windupEndAtSec
+-- nextReadyAtSec
```

攻击范围默认 `5.1`，但最终读取 `AttackRange` 当前值，允许 buff/技能修改。攻击力读取 `Attack`，攻击速度读取 `AttackSpeed`。

### 6.2 行为

```text
Idle:
  query enemies in range
  random pick one target
  targetRef = picked
  searchExpireAt = now + 5.0
  if attack ready: enter Windup

Windup:
  interruptible
  when windupEndAt <= now:
    validate target using staged summary
    create pending impact
    nextReadyAt = windupEndAt + attackInterval
    phase = Cooldown

Cooldown:
  if searchExpireAt <= now or target invalid:
    phase = Idle
  else if nextReadyAt <= now:
    enter Windup against current target
```

“当前普通攻击结束后才能释放队列技能”在本文中解释为：如果普通攻击已经进入 Windup，则等到本次 attack fire 之后再开始技能；攻击进入 Cooldown 后，技能队列可以抢占普通行为。这个解释需要你确认，因为它会影响技能能否穿插在普攻 CD 内。

### 6.3 延迟伤害

攻击 fire 时不直接伤害目标，而是把命中事件放入 source Unit 的 pending impact heap：

```text
Impact
+-- impactAtSec = fireAtSec + distance / projectileSpeed
+-- sourceRef
+-- targetRef
+-- abilityOrAttackID
+-- rawDamage
+-- metadata
```

pending impact 到期后，source Think `Publish(targetRef, DamageEffect)`。DamageEffect 携带 source 端已经计算出的 rawDamage 和少量元信息；target Apply 用自己的 Defense、护盾、减伤等计算最终伤害。

---

## 7. 技能系统

### 7.1 技能槽

Unit 最多 8 个技能槽，每个槽挂一个 AbilityDef，允许多个槽挂同一个 Def。

```text
AbilitySlot
+-- slotIndex
+-- abilityID
+-- state: NotReady | Ready | PreCast | AfterCast
+-- queued bool
+-- cooldownReadyAtSec
+-- passive runtime data
+-- charges / custom counters
```

槽是运行时状态单位，AbilityDef 是共享配置。重复技能也必须拥有独立槽状态、CD、充能和 queued 标记。

### 7.2 Ready Queue

技能就绪后进入 Unit 的 FIFO 施法队列：

```text
ReadyQueue []slotIndex
```

入队规则：

- 同一 slot 已 queued 时不重复入队。
- CD 型技能到 `cooldownReadyAtSec` 后置为 Ready 并入队。
- 释放型被动技能收到事件 signal 后，在 Think 中做条件判断；通过后可把配置的目标 slot 入队。
- 纯数值被动不入队，表现为 modifier/buff/tag 对属性的持续影响。

出队规则：

- 当前技能 PreCast/AfterCast 未结束时不取下一个。
- 普通攻击 Windup 未 fire 时不取技能，除非后续 AbilityDef 显式声明可打断普攻前摇。
- 出队时再次检查释放条件；失败则调用 `OnFail`，清掉 Ready/queued，继续尝试下一个。
- 队列排空后才恢复普通攻击行为。

### 7.3 施法阶段

技能状态流：

```text
NotReady -> Ready -> PreCast -> AfterCast -> NotReady
              |        |
              |        +-- interrupted/fail -> OnFail -> NotReady
              +-- condition fail -> OnFail -> NotReady
```

默认语义：

- PreCast 是可打断阶段。
- Cast commit 发生在 PreCast 结束时。
- 成功 commit 后立刻产生技能 effects / pending impacts，并进入 AfterCast。
- CD 默认从 `castCommitAtSec` 开始计算，避免因为 tick 对齐产生累计误差。
- AfterCast 阻塞后续技能和普通行为，但默认不可被普通攻击打断。

如果某个技能需要“按下即进 CD”或“失败也消耗 CD”，由 AbilityDef 的 cost/cooldown/fail policy 扩展，不放进初版全局规则。

### 7.4 被动技能

被动技能分两类：

| 类型 | 建模 |
|------|------|
| 纯数值影响型 | 初始化或 buff 生命周期中注册 modifier/tag，不进 ready queue |
| 释放型被动 | 订阅 Unit signals，在 Think 中判断概率/充能/条件，通过后让目标 slot Ready 并入队 |

触发事件统一走 Signal：

- 主动攻击：source Think 在 attack fire 时 emit self `AttackFiredSignal`
- 收到攻击：target Apply 处理 DamageEffect 后 emit self `DamageTakenSignal`
- 释放技能：source Think 在 cast commit 时 emit self `SkillCommittedSignal`
- 命中/造成伤害：target Apply 可以 emit source `DamageDealtSignal`

这样被动触发始终进入目标 Unit 的 Think，不在 Apply 中直接执行复杂逻辑。

---

## 8. Buff / Debuff

Buff 是 Unit owner-local 的 thinkable 子逻辑，但不是 Scheduler Logic。

```text
BuffTable
+-- instances by instanceID
+-- heap by nextThinkAtSec
+-- stack policy data
```

公共影响通过 Effect 提交：

- ApplyBuffEffect：target Apply 创建 buff instance，调用 OnApply，注册 modifier/tag。
- RemoveBuffEffect：target Apply 调用 OnRemove，清理 modifier/tag。
- Buff timer 到期：target Think 发布 self RemoveBuffEffect。
- Periodic DOT/HOT：target Think 到期后发布 self DamageEffect/HealEffect。

这个模型让 buff 的计时在 Think 中推进，但 HP、属性 current、公开 tag 的权威变更仍集中在 Apply。

攻击范围、攻击力、攻击速度、血量上限都走 `attr.Table` modifier。后续实现前需要扩展 `demo/cfg/attr.toml`，至少补充：

```text
AttackRange
AttackSpeed
```

ProjectileSpeed 可以先作为普通攻击/技能配置，不一定做成属性；如果需要被 buff 修改，再加入 attribute。

---

## 9. 死亡与复活

死亡是 public state：

```text
LifeState = Alive | Dead
ReviveAtSec
```

流程：

```text
DamageEffect Apply:
  HP -= finalDamage
  if HP <= 0 and Alive:
    LifeState = Dead
    ReviveAtSec = now + 8.0
    Emit(self, DiedSignal{ReviveAtSec})
    WriteStage(...)

DiedSignal Think:
  DeathController records reviveAt
  return delay to reviveAt

Revive deadline Think:
  Publish(self, ReviveEffect)

ReviveEffect Apply:
  LifeState = Alive
  HP = MaxHP
  Mana = MaxMana
  clear death-only flags if needed
  Emit(self, RevivedSignal)
  WriteStage(...)
```

能加速复活的技能/效果通过 `ModifyReviveEffect` 修改 `ReviveAtSec`，Apply 后 emit self signal 让 DeathController 重新计算 wakeup。

Dead Unit 默认不执行普通攻击和主动技能，但仍处理 buff/death timers、被动清理和 ReviveEffect。

---

## 10. Effect / Signal 草案

### Effects

| Kind | Target Apply 行为 |
|------|-------------------|
| Damage | 计算最终伤害，扣 HP，触发受击/死亡 signals |
| Heal | 回复 HP，受 MaxHP clamp |
| ApplyBuff | 创建/堆叠 buff，注册 modifier/tag |
| RemoveBuff | 移除 buff，清理 modifier/tag |
| Revive | 满血满蓝复活 |
| ModifyRevive | 调整死亡状态下的 reviveAt |

Effect payload 不携带 source Unit 指针，只携带 `SourceRef`、`AbilityID/AttackID`、`RawDamage`、`Element/Flags` 等数据。

### Signals

| Kind | Target Think 行为 |
|------|-------------------|
| ExternalStart / Command | 初始化或外部驱动 |
| AttackFired | 释放型被动的主动攻击触发 |
| SkillCommitted | 技能联动触发 |
| DamageTaken | 受击被动触发 |
| DamageDealt | 命中/造成伤害被动触发 |
| Interrupt | 尝试打断当前 Windup/PreCast |
| Died / Revived | 更新私有控制器状态 |
| ReviveChanged | 重算死亡复活 wakeup |

Signal 只唤醒 Think；如果需要改变 HP、buff、tag，必须转成 Effect。

---

## 11. Package 设计与文件布局

虽然框架、场景、测试代码和测试技能配置都放在 `demo/` 目录下，但不建议都放在同一个 Go package。Go 的 package 边界可以帮助 demo 保持两层语义：

- `demo/combat`：战斗框架 package，只表达可复用的 Unit/Ability/Buff/Effect/Signal/World 接入方式。
- `demo/scenario`：具体 demo 场景 package，放 n x n 初始化参数、测试技能配置、集成测试和 demo runner。

依赖方向固定为：

```text
demo/scenario  ──imports──>  demo/combat
demo/combat    ──imports──>  sched / attr / tag / lib
```

`demo/combat` 不反向依赖 `demo/scenario`，也不读取具体测试技能配置。这样框架代码展示的是“如何接入 sched”，场景代码展示的是“如何用框架拼出玩法案例”。

### 11.1 `demo/combat` 框架包

建议 package name 使用 `combat`。框架代码：

| 文件 | 内容 |
|------|------|
| `demo/combat/time.go` | tick/秒转换、deadline -> delay helper |
| `demo/combat/types.go` | Vec2、Team、LifeState、常量 |
| `demo/combat/effect.go` | Effect kind、payload、Order |
| `demo/combat/signal.go` | Signal kind、payload、Order |
| `demo/combat/world.go` | World 主结构、scheduler 创建、Step、GetLogic |
| `demo/combat/world_init.go` | 通用 Unit 创建 API；n x n 可由 scenario 调用或包装 |
| `demo/combat/world_query.go` | staged summary 查询、空间查询 |
| `demo/combat/stage.go` | StageKind、UnitSummary、AttrSummary、PromoteStages |
| `demo/combat/unit.go` | Unit struct、Think/Apply orchestration |
| `demo/combat/unit_attack.go` | BasicAttackController |
| `demo/combat/unit_death.go` | DeathController |
| `demo/combat/ability.go` | AbilityDef、Ability interface / callbacks |
| `demo/combat/ability_slot.go` | AbilitySlot、ReadyQueue、CD readiness |
| `demo/combat/passive.go` | passive trigger runtime |
| `demo/combat/buff.go` | Buff interface、BuffTable、stock buff skeleton |
| `demo/combat/attr_helpers.go` | demo attrs 初始化、summary、clamp/flush helper |
| `demo/combat/demo_attr.go` | `mk_attr` 生成的 demo 属性代码，归属 combat package |

框架内部测试可以放在同目录 `_test.go`：

| 文件 | 覆盖 |
|------|------|
| `demo/combat/time_test.go` | deadline/tick 转换、无累计误差 |
| `demo/combat/world_query_test.go` | range 5.1、敌我过滤、死亡过滤 |
| `demo/combat/unit_attack_test.go` | 索敌、前摇、打断、弹道延迟伤害 |
| `demo/combat/ability_queue_test.go` | CD ready 入队、重复技能槽、失败丢弃 |
| `demo/combat/passive_test.go` | 主动攻击/受击/释放技能触发 |
| `demo/combat/buff_test.go` | modifier 生效/过期、think delay |
| `demo/combat/death_test.go` | 死亡、8s 复活、复活加速 |

这类测试验证框架规则，可以使用 `package combat` 直接测内部状态；如果要约束公开 API，也可以另建 `package combat_test` 的黑盒测试文件。

### 11.2 `demo/scenario` 场景包

建议 package name 使用 `scenario`。这里放具体示例、测试技能配置和端到端验证：

| 文件/目录 | 内容 |
|-----------|------|
| `demo/scenario/grid.go` | 创建 n x n Unit，位置间距 1.0，`(x + y) % 2` 分阵营 |
| `demo/scenario/skills.go` | demo 技能配置与构造函数；可以包含重复槽配置 |
| `demo/scenario/buffs.go` | demo buff/debuff 配置 |
| `demo/scenario/runner.go` | 性能 demo 或手动运行入口 |
| `demo/scenario/testdata/` | 如果技能配置改为 TOML/JSON/YAML，测试配置放这里 |
| `demo/scenario/grid_test.go` | n x n 位置、阵营、ref 稳定性 |
| `demo/scenario/combat_integration_test.go` | 多 Unit tick 端到端 |
| `demo/scenario/skill_integration_test.go` | 具体技能配置触发链路 |

如果技能配置会被多个 scenario 复用，可以再拆一个 `demo/combatcfg` package；但初版建议先放 `demo/scenario`，等配置变多后再拆，避免过早抽象。

### 11.3 根 `demo/` 目录

根 `demo/` 目录尽量只保留轻量入口和工具文件：

| 文件/目录 | 内容 |
|-----------|------|
| `demo/Makefile` | 生成属性、运行测试/benchmark |
| `demo/cfg/attr.toml` | 属性生成配置；生成目标改到 `demo/combat/demo_attr.go` |
| `demo/prompt.md` | 当前需求草稿或 agent 输入材料 |

现有 `demo/gas.go` 和 `demo/world.go` 更像早期草稿。实现阶段建议迁入 `demo/combat` 后按职责拆开，而不是继续在根 package 扩张。

---

## 12. 实现顺序建议

1. 时间 helper 与 World Step：保证 `World.Now()`、scheduler delay、0.125s tick 对齐。
2. Stage summary 与 World query：先让 Unit 能只读查询敌人。
3. Unit public/private 结构与 n x n 初始化。
4. Effect/Signal 基础类型：Damage、Heal、Died、Interrupt。
5. 普通攻击闭环：索敌 5s、前摇、弹道延迟、伤害 Apply。
6. 死亡/复活闭环：8s 满血满蓝复活。
7. AbilitySlot + ReadyQueue：只做 CD 型技能和 OnFail。
8. Passive trigger：释放型被动把目标 slot 入队。
9. BuffTable + modifier：属性影响攻击范围/攻击力/攻速/MaxHP。
10. 再填充具体技能配置和更复杂效果。

---

## 13. 待确认问题

1. “当前普通攻击结束后”是否指 attack fire 后即可释放技能，还是必须等普通攻击 CD 完全结束？
2. 技能 CD 是从 PreCast 开始、Cast commit、还是 AfterCast 结束开始？本文默认从 Cast commit 开始。
3. 普通攻击弹道命中时，如果目标已死亡/复活/移出范围，是否仍然命中？本文建议初版只检查目标仍存在且可受击，不重新检查距离。
4. AttackRange / AttackSpeed 是否立即加入 `demo/cfg/attr.toml`，ProjectileSpeed 是否也需要可被 buff 修改？
5. 被动触发的随机概率是否要求全局可复现？本文建议每 Unit 私有 deterministic RNG，避免共享随机源。

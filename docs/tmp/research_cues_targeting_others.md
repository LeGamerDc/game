# 调研笔记：Gameplay Cues、Targeting System 与其他 GAS-like 系统

> 调研目的：为 Go 语言并行 tick 游戏服务器框架的 GAS 设计提供参考
> 调研日期：2026-07

---

## 目录

1. [Gameplay Cues 概述](#1-gameplay-cues-概述)
2. [Targeting System](#2-targeting-system)
3. [其他 GAS-like 系统简要对比](#3-其他-gas-like-系统简要对比)
4. [通用 GAS 概念地图](#4-通用-gas-概念地图)
5. [参考链接](#5-参考链接)

---

## 1. Gameplay Cues 概述

### 1.1 概念与用途

Gameplay Cues（GC）是 Unreal GAS 中专门用于**表现层通知**的机制，负责触发粒子特效、音效、相机震动等**非 Gameplay 逻辑**的视觉/听觉反馈。

核心特征：
- **纯表现层**：GameplayCues 不应修改任何 Gameplay 状态（Attributes、Tags 等），只产出视觉/音效反馈
- **基于 GameplayTag 路由**：所有 Cue 必须关联一个以 `GameplayCue.` 为前缀的 GameplayTag（如 `GameplayCue.ElectricalSparks`），由 GameplayCueManager 统一管理分发
- **网络高效**：通过 unreliable NetMulticast RPC 广播，可以 batch 多个 Cue 到单个 RPC；也支持纯本地执行以完全避免网络开销
- **不可靠性**：Cues 本质上是 unreliable 的，不适合驱动任何影响 Gameplay 的逻辑

### 1.2 两类 GameplayCueNotify

| 类型 | 响应事件 | 绑定的 GE 类型 | 实例化方式 | 典型用途 |
|------|----------|---------------|-----------|----------|
| `GameplayCueNotify_Static` | `Execute` | Instant / Periodic | 无实例（CDO） | 一次性效果：击中火花、伤害数字飘字 |
| `GameplayCueNotify_Actor` | `Add` / `Remove` | Duration / Infinite | 生成 Actor 实例 | 持续效果：燃烧粒子、增益光环、循环音效 |

### 1.3 四个生命周期事件

| 事件 | 时机 | 用途 |
|------|------|------|
| `OnActive` | GC 被激活时 | 起始爆发效果（爆炸、闪光），迟加入玩家不会看到 |
| `WhileActive` | GC 活跃期间（含迟加入） | 持续效果（地面火焰、光环），迟加入玩家也能看到 |
| `Removed` | GC 被移除时 | 清理 OnActive 和 WhileActive 添加的效果 |
| `Executed` | 即时执行 | 仅用于 Instant GE 触发的一次性效果 |

### 1.4 与 GameplayEffects / Abilities 的关联

触发路径有两条：

**路径 A：通过 GameplayEffect 自动触发**
- 在 GE 的 Cue 配置中关联 GameplayCueTag
- Instant GE → 触发 `Execute`
- Duration/Infinite GE → 应用时触发 `Add`（OnActive + WhileActive），移除时触发 `Remove`
- Periodic GE 每次 tick 触发 `Execute`
- 通过 GE 触发的 Cue 携带完整的 GameplayEffectContext（Instigator、HitResult、Magnitude 等）

**路径 B：从 Ability / ASC 手动触发**
- 直接调用 `ASC->ExecuteGameplayCue()` / `AddGameplayCue()` / `RemoveGameplayCue()`
- 需要手动填充 `FGameplayCueParameters`
- 可以做成 Local 版本（不走网络 RPC），常用于投射物碰撞等高频场景

### 1.5 可靠性分析

Gameplay Cues 的可靠性取决于触发方式和 ASC Replication Mode：

- **通过 GE 触发**：`WhileActive` 和 `OnRemove` 是可靠的（通过 GE 复制实现）；`OnActive` 使用 unreliable multicast
- **手动触发**：`OnActive` 和 `WhileActive` 都是 unreliable multicast；只有 `OnRemove` 对 autonomous proxy 可靠
- **结论**：如果需要"可靠" Cue，应通过 GE 触发并使用 `WhileActive`

### 1.6 对纯服务器框架的启示

**核心结论：纯服务器框架不需要 Gameplay Cue 子系统。**

理由分析：
1. Cues 的本质是**客户端表现层同步**（VFX、SFX、UI 反馈），服务器不渲染
2. 在 Unreal 中，Cues 依赖 unreliable NetMulticast RPC，本质上是一种网络优化——把「什么表现该播」的决策权给服务器
3. 对于我们的服务器框架，Gameplay 事件（伤害、Buff 应用/移除等）本身已通过 Effect/Signal 协议传递

**建议方案**：将 Cue 的概念映射为 **Effect 或 Signal 中的表现提示（Presentation Hint）**：

| Unreal Cue 模式 | 我们框架的对应方案 |
|---|---|
| Execute (Instant) | Effect 中附带 CueTag 字段 |
| | → 客户端收到 Effect 同步后自行播放 |
| Add/Remove (持续) | Buff/状态变化本身就是 Signal/Effect |
| | → 客户端 observe 状态变化，自行管理表现生命周期 |
| Local Cue | 客户端本地逻辑，完全不经过服务器 |

关键设计点：
- 服务器侧 Effect 可以携带一个可选的 `CueHint` 结构体（包含 Tag、位置、方向、强度等），作为 Effect 的附属数据
- 客户端接收到 Effect 后，根据 CueHint 自行决定播放什么表现
- 这比 Unreal 的 Cue 系统更简洁：不需要 GameplayCueManager、不需要 Cue 的注册/路由/batch 机制
- 与 Scheduler 的 Effect 收集管线天然兼容——CueHint 只是 Effect 结构体中的一个字段

---

## 2. Targeting System

### 2.1 Unreal GAS 的 Targeting 设计

#### 2.1.1 核心概念

**TargetData（`FGameplayAbilityTargetData`）**
- 多态数据结构，用于在网络上传递目标信息
- 可以包含：AActor 引用、FHitResult、位置/方向/原点等
- 通过 `FGameplayAbilityTargetDataHandle`（内含 TArray<FGameplayAbilityTargetData*>）传递
- 支持自定义子类携带任意额外数据（如弹药 ID、自定义伤害参数）
- 可以存入 GameplayEffectContext，在整个 GE 管线中传递

**TargetActor（`AGameplayAbilityTargetActor`）**
- 世界中的 Actor，负责可视化瞄准过程并从世界中采集目标信息
- 通过 `WaitTargetData` AbilityTask 生成和管理
- 内置多种实现：SingleLineTrace、GroundTrace、Radius 等
- 每帧 Tick 执行 trace/overlap 并更新当前目标
- 确认后将结果转为 TargetData 返回给 Ability

**TargetData Filter（`FGameplayTargetDataFilter`）**
- 过滤器，在 Trace/Overlap 结果中筛选有效目标
- 内置支持：过滤施法者自身、限定目标类型
- 可自定义子类 `FilterPassesForActor` 实现复杂过滤（如友敌判断、状态检查）

**Reticle（`AGameplayAbilityWorldReticle`）**
- 可选的瞄准指示器 Actor，用于在有效目标上显示 UI

#### 2.1.2 确认模式

| 确认类型 | 行为 |
|----------|------|
| `Instant` | 立即生成 TargetData，无用户输入，TargetActor 立即销毁 |
| `UserConfirmed` | 等待玩家按确认键或调用 `TargetConfirm()` |
| `Custom` | Ability 自行决定何时调用 `ConfirmTaskByInstanceName()` |
| `CustomMulti` | 同 Custom 但不结束 AbilityTask，可多次产出 TargetData |

#### 2.1.3 网络模型

TargetActor 的 `ShouldProduceTargetDataOnServer` 属性控制两种模式：

- **false（默认）**：客户端做 trace，通过 RPC（`CallServerSetReplicatedTargetData`）把 TargetData 发给服务器
  - 优点：客户端体验流畅，瞄准零延迟
  - 缺点：需要服务端验证防作弊
  
- **true**：客户端仅发送确认信号，服务器自己做 trace 产出 TargetData
  - 优点：天然防作弊
  - 缺点：客户端可能出现预测偏差

### 2.2 目标选择时机：激活时 vs 延迟确定

Unreal GAS 同时支持两种模式：

**即时确定（Instant Targeting）**
- Ability 激活时立即执行 trace/overlap 获取目标
- 常见于：瞬发技能、普攻、hitscan 武器
- 在 `GameplayEffectContainers`（Epic ActionRPG 示例）中，targeting 在容器应用时即时完成，在 CDO 上执行，不生成 Actor
- 可以与 Ability Batching 配合，将激活 + TargetData + 结束三个 RPC 合并为一个

**延迟确定（Deferred Targeting）**
- 使用 `WaitTargetData` AbilityTask，Ability 进入等待状态
- 玩家在世界中选择/确认目标后，TargetData 才返回给 Ability
- 常见于：范围指示器技能（LOL 的 Veigar W）、多段释放技能
- 利用 AbilityTask 的异步特性，Ability 在等待期间不阻塞其他逻辑

**关键洞察**：Unreal 的设计并不强制 Ability 在激活时就确定目标。Target 选择是 Ability 执行流中的一个**异步步骤**，可以在任意时刻发生。这对我们 Scheduler 的设计有启示——Target 选择可以分为「发起请求」（Think 阶段）和「确认/应用」（Apply 阶段）两个分离的步骤。

### 2.3 时序问题："施法时有效，结算时失效"

这是所有 GAS 系统面临的经典问题。Unreal GAS 的处理策略：

#### 2.3.1 Snapshot（快照）机制

GAS 在 Attribute Based Modifier 中有明确的 **Snapshot** 概念：

| Snapshot | Source | 采集时机 | 自动更新 |
|----------|--------|---------|---------|
| Yes | Source | GESpec 创建时 | 否 |
| Yes | Target | GESpec 应用时 | 否 |
| No | Source | GESpec 应用时 | 是（Duration/Infinite GE） |
| No | Target | GESpec 应用时 | 是（Duration/Infinite GE） |

- 攻方属性（如攻击力）通常在 GESpec **创建时快照**
- 守方属性（如护甲）通常在 GESpec **应用时采集**（不快照），确保使用最新值

#### 2.3.2 投射物场景

投射物是时序问题的典型案例：
1. 施放技能时创建 `GameplayEffectSpec`，此时**快照**施法者的属性（攻击力、暴击率等）
2. GESpec 传递给投射物 Actor
3. 投射物飞行一段时间后命中目标
4. 命中时**应用** GESpec，此时**实时采集**目标的属性（护甲等）
5. 如果目标已死亡/消失，GE 应用失败（目标 ASC 不存在或 Tag 条件不满足）

#### 2.3.3 Lag Compensation（延迟补偿）

对于"目标已移动"的问题，Unreal 主要依赖：
- **Server Rewind**：服务器回溯到客户端开火时刻的世界状态，验证命中（Fortnite/Lyra 使用此方案）
- **"Favor the Attacker"** 原则：攻击方看到什么就命中什么，被打方偶尔被"穿墙打中"是可接受的
- 这与我们框架无关——我们是纯服务器侧逻辑，不涉及客户端预测

#### 2.3.4 与我们 M10 模式的对比

我们 Scheduler 的 **M10（攻方快照 + 守方裁决）**模式与 Unreal GAS 的 Snapshot 机制高度一致：

```
Unreal GAS 模式:
    Source Think:  创建 GESpec，快照 Source 属性
    Network:       GESpec 通过投射物/直接传递到 Target
    Target Apply:  Target 的 ASC 应用 GESpec
                   → ExecutionCalculation 读取快照的 Source 属性
                   → 实时采集 Target 属性（护甲等）
                   → 计算最终伤害
                   → PostGameplayEffectExecute 处理（护盾吸收、死亡判定）

我们的 M10 模式:
    Source Think:  构建 DamageEffect，快照攻方属性（攻击力、暴击率等）
    Signal/Effect: DamageEffect 投递到 Target 的 Inbox
    Target Apply:  Target 读取 Effect 中的攻方快照
                   → 基于自身最新状态（护甲、buff）计算实际伤害
                   → 修改自身 HP
                   → 产出后续 Signal（死亡通知、反伤等）
```

**关键对应关系**：
| Unreal GAS | 我们的框架 | 说明 |
|------------|-----------|------|
| GameplayEffectSpec | 携带快照的 typed Effect | 攻方快照数据载体 |
| Snapshot=true for Source | Think 阶段从 WorldView 读取自身属性打包 | 快照时机 |
| ExecutionCalculation | Target 的 Apply 逻辑 | 守方裁决计算 |
| PostGameplayEffectExecute | Apply 内的后处理 | 级联效果处理 |

**与 Unreal 的差异**：
1. Unreal 的 Snapshot 选择是**每个 Modifier 独立配置**的（粒度细），我们是**整体快照**
2. Unreal 支持 non-snapshot（Duration GE 自动更新），我们的 Effect 是一次性投递，没有"持续跟踪"语义
3. Unreal 的 ExecutionCalculation 能同时修改多个 Attribute，我们的 Apply 也可以
4. Unreal 不预测伤害（Epic 明确推荐不预测），我们天然也不需要——纯服务器框架

### 2.4 Selector 与 WorldView 的冲突

当前参考实现中 Selector 在 Cast 时直接遍历 `world.units` 的问题：

**问题**：Think 阶段只能通过 WorldView 读取世界状态，不能直接访问 world.units

**解决方案**（受 Unreal Targeting 启发）：

1. **WorldView 提供空间查询接口**：
   - `WorldView.UnitsInRange(center, radius) → []UnitSnapshot`
   - `WorldView.UnitsWithTag(tag) → []UnitSnapshot`
   - `WorldView.RayCast(origin, direction, distance) → []HitResult`
   - 返回的是**只读快照**，符合 Think 阶段的约束

2. **两阶段目标选择**：
   - Think：通过 WorldView 查询候选目标列表，将目标 ID 列表打包到 Effect 中
   - Apply：Target 收到 Effect 后验证自身是否仍然有效（是否存活、是否在范围内等）
   - 这就是 M10 的守方裁决模式

3. **对应 Unreal 的概念**：
   - 我们的 WorldView 空间查询 ≈ TargetActor 的 trace/overlap
   - 我们的 Effect 中的目标 ID 列表 ≈ TargetData
   - Target Apply 中的验证 ≈ GE Application Tag Requirements

---

## 3. 其他 GAS-like 系统简要对比

### 3.1 Unity 社区的 GAS 移植

Unity 没有官方 GAS，但社区有多个开源实现：

#### sjai013/unity-gameplay-ability-system（已归档）
- MIT 协议，高度模仿 Unreal GAS 的术语和架构
- 实现了三大核心：Gameplay Abilities、Gameplay Effects、Attributes
- 基于 Unity ScriptableObject 做数据驱动
- 不再维护，但代码结构清晰，适合学习 GAS 概念的 Unity 映射

#### h2v9696/UnityGAS
- 仍在活跃开发（v1.1.1，2025年6月），MIT 协议
- 完整实现 GAS 核心：ASC、AttributeSet、GE（Instant/Duration/Infinite）、GA
- 尚未实现 Gameplay Cue 和多人支持
- 使用 Unity Test Framework 做了充分测试

#### felipeggrod/GASify
- Asset Store 付费版 + GitHub 免费版
- 支持 Mirror 网络框架集成
- 包含：Attributes、Tags、Effects、Modifiers、Abilities
- 有在线文档站

#### 共同特征
- 所有 Unity GAS 移植都保留了 Unreal GAS 的核心概念：ASC、Attributes、GE、GA、GameplayTags
- 但**都大幅简化了网络复制**——Unity 的网络模型与 Unreal 差异很大，Prediction 几乎都没有实现
- **GameplayCues 几乎都没实现**——进一步证实 Cue 是 Unreal 特有的客户端表现层机制

### 3.2 ECS 架构中的 Ability System

#### Iron Marines Invasion（Ironhide Games）
- 使用自研 ECS 架构
- 将 Ability 建模为 `AbilitiesComponent`（包含 `List<Ability>` 的组件），而非每个 Ability 一个 Component
- 每个 Ability 是一个数据结构体：name、cooldown、isReady、isRunning
- System 层面处理 Ability 的更新和执行
- **与我们的框架相关**：ECS 中 Ability 也是"组件上的数据 + 系统中的逻辑"，与我们 Logic 内部组织技能的思路一致

#### LOL 2.0 概念设计讨论
- 社区讨论将 LOL 的技能系统用 ECS 重构
- 将技能拆分为：AbilityComponent（数据）+ 各种 System（HealingSystem、DamageSystem、ControlSystem）
- 指出 ECS 天然适合并行化和跨平台优化
- 能力偷取（Sylas R）在 ECS 中只需复制 AbilityComponent

#### Unity DOTS/ECS + 确定性
- Unity ECS 的 Job System 默认保证确定性执行
- 社区讨论中反复出现"网络同步层需要确定性，表现层不需要"的分层思路
- 使用多 World 概念分离确定性仿真和非确定性渲染
- **与我们的框架高度相关**：我们的 Think/Apply 分离本质上就是确定性仿真层

### 3.3 Entity-Component-Worker（SpatialOS / Improbable）

SpatialOS 提出的 Entity-Component-Worker 架构是与我们最接近的设计思路：

- **Entity**：游戏对象，无行为
- **Component**：数据容器，描述实体状态
- **Worker**：分布式系统中的处理单元，每个 Worker 负责模拟一部分实体的一部分组件
- 关键创新：取代 ECS 中的 System，用 Worker 实现分布式处理
- 支持百万级实体、千级 Worker、百级服务器的大规模模拟

**与我们框架的对比**：

| SpatialOS ECW | 我们的框架 | 说明 |
|---------------|-----------|------|
| Worker | Scheduler Worker/Goroutine | 并行处理单元 |
| Component 的 authority 分配 | Logic 的 Owner 模型 | 谁负责更新哪些数据 |
| Worker 间通信 | Signal/Effect 协议 | 跨实体交互 |
| 空间分区 (chunking) | Block-based 分组 | 并行粒度管理 |

差异：SpatialOS 关注**空间分区和多服务器水平扩展**，我们关注**单服务器内的并行 tick**。但两者在"Worker 间不能直接共享状态、必须通过协议通信"这一核心约束上完全一致。

### 3.4 Medium 文章：Designing a Flexible Ability System

一篇设计灵活技能系统的文章提出了有意思的架构分层：

1. **CastChecker 链**：验证技能是否可以释放（资源够不够、冷却好没有、状态允许不允许）
2. **SkillCastRequest**：抽象执行请求，解耦"技能做什么"和"如何发起"
3. **Skill = CastChecker + SkillCastRequest**：绑定验证和执行

```
interface Skill {
    string name;
    SkillCastRequest request;
    CastChecker checker;
    bool cast() { return checker.check(); }
}
```

**与我们框架的映射**：
- CastChecker → Think 阶段的 pre-condition 检查（通过 WorldView 读取状态）
- SkillCastRequest → 产出的 Effect（携带执行参数）
- Skill.cast() → Think 中的技能决策逻辑

### 3.5 有无"并行 tick + Effect/Signal 协议"类似思路的 GAS？

**调研结论：目前没有发现与我们完全一致的公开设计。**

最接近的系统：
1. **SpatialOS ECW**：分布式 Worker 间通过组件读写通信，但没有 Think/Apply 两阶段模型
2. **Unity DOTS + Networking**：确定性执行 + 多 World 分层，但 Ability System 不是 DOTS 原生的
3. **各种 ECS Ability 实现**：关注数据驱动和组件化，但没有并行 tick 的约束考量
4. **Unreal GAS 本身**：单线程执行，Prediction 是客户端技术（不是服务器并行）

我们的设计独特之处：
- **服务器侧并行 tick**（不是客户端预测）
- **Think 阶段只读快照 + Apply 阶段状态修改**的严格分离
- **Signal/Effect 作为唯一跨实体通信手段**

这种组合在公开的游戏架构资料中确实是新颖的，与我们之前的 Prior Art 分析结论一致。

---

## 4. 通用 GAS 概念地图

### 4.1 Unreal GAS 核心概念关系图

```
                        ┌──────────────────────────────────────────────────┐
                        │          AbilitySystemComponent (ASC)            │
                        │    ┌─────────────────────────────────────────┐   │
                        │    │ ActivatableAbilities (FGameplayAbility  │   │
                        │    │  SpecContainer)                        │   │
                        │    │   ┌─────────────┐                      │   │
                        │    │   │ GameplayAbil-│──uses──►AbilityTasks │   │
                        │    │   │ ity (GA)     │         (async work) │   │
                        │    │   └──────┬───┬──┘                      │   │
                        │    └──────────┼───┼─────────────────────────┘   │
                        │              │   │                              │
                        │         cost │   │ applies                     │
                        │              ▼   ▼                              │
                        │    ┌─────────────────┐   modifies   ┌────────┐ │
                        │    │ GameplayEffect   │────────────►│Attribu-│ │
                        │    │ (GE)             │             │teSet   │ │
                        │    │  - Instant       │             │        │ │
                        │    │  - Duration      │◄──defined───│ Health │ │
                        │    │  - Infinite      │    by       │ Mana   │ │
                        │    └────────┬─────────┘             │ etc.   │ │
                        │             │                       └────────┘ │
                        │        triggers                                │
                        │             ▼                                   │
                        │    ┌─────────────────┐                         │
                        │    │ GameplayCue (GC) │  ◄── cosmetic only     │
                        │    │  VFX / SFX / UI  │                        │
                        │    └─────────────────┘                         │
                        │                                                │
                        │    ┌─────────────────┐                         │
                        │    │ GameplayTags     │  ◄── state labeling    │
                        │    │  hierarchical    │      used everywhere   │
                        │    └─────────────────┘                         │
                        └──────────────────────────────────────────────────┘

  Targeting Pipeline:
      GA ──► WaitTargetData(AbilityTask) ──► TargetActor ──► TargetData
                                                │                │
                                           trace/overlap    FHitResult
                                           in world         + Actor refs
                                                │                │
                                                ▼                ▼
                                         TargetDataFilter   GESpec.Context
```

### 4.2 概念分类：通用 vs Unreal 特有

| 概念 | 通用性 | 说明 |
|------|--------|------|
| **Attributes**（属性） | ★★★ 通用必需 | 任何 GAS 都需要可修改的数值属性系统 |
| **GameplayTags**（层级标签） | ★★★ 通用必需 | 状态标记、条件匹配、分类系统——我们已有 `tag/` 包 |
| **GameplayEffect**（效果） | ★★★ 通用必需 | 修改属性的数据驱动容器，核心机制 |
| **GameplayAbility**（技能） | ★★★ 通用必需 | 封装动作/技能逻辑的执行单元 |
| **Modifier 聚合**（加法/乘法/覆盖） | ★★☆ 高度推荐 | 多个效果作用于同一属性时的聚合公式 |
| **TargetData**（目标数据） | ★★☆ 高度推荐 | 标准化的目标信息传递结构 |
| **Cost / Cooldown GE** | ★★☆ 高度推荐 | 技能消耗和冷却的标准化处理 |
| **Stacking**（效果堆叠） | ★★☆ 高度推荐 | Duration/Infinite 效果的堆叠策略 |
| **AbilityTask**（异步任务） | ★☆☆ Unreal 特色 | 依赖 UObject 生命周期和 Blueprint 可视化，我们用 goroutine/channel |
| **GameplayCue**（表现通知） | ★☆☆ Unreal 特有 | 客户端表现层同步机制，纯服务器框架不需要 |
| **Prediction / Rollback** | ★☆☆ Unreal 特有 | 客户端预测和回滚，纯服务器框架不需要 |
| **ASC Replication Mode** | ★☆☆ Unreal 特有 | Full/Mixed/Minimal 复制策略，与 Unreal 网络层强耦合 |
| **TargetActor / Reticle** | ★☆☆ Unreal 特有 | 客户端可视化瞄准 Actor，服务器框架不需要 |
| **GESpec / EffectContext** | ★★☆ 概念通用 | 运行时效果实例 + 上下文（施法者、命中信息），名字不同但概念通用 |
| **Meta Attributes** | ★★☆ 设计模式 | "伤害"作为中间值，分离"造成多少"和"如何分配"的逻辑 |
| **Execution Calculation** | ★★★ 通用必需 | 复杂伤害/效果计算逻辑，对应我们 Apply 中的计算 |

### 4.3 对服务器侧并行 tick 框架最值得借鉴的概念

**第一梯队（核心必需）**：

1. **Attributes + Modifier 聚合模型**
   - BaseValue / CurrentValue 分离
   - `((Base + Additive) * Multiplicative) / Division` 聚合公式
   - 与 Duration/Infinite Effect 的自动关联

2. **GameplayEffect 的三种持续类型**
   - Instant：永久修改 BaseValue
   - Duration：临时修改 CurrentValue，到期自动移除
   - Infinite：临时修改 CurrentValue，需手动移除
   - 这直接映射到我们 Timer Wheel 的管理策略

3. **Snapshot 机制 → 攻方快照 + 守方裁决**
   - Effect 创建时快照 Source 属性
   - Effect 应用时读取 Target 最新属性
   - 天然适配 Think/Apply 两阶段模型

4. **GameplayTags 作为统一的状态/条件系统**
   - 效果的应用条件（Application Tag Requirements）
   - 技能的激活/阻止条件
   - 效果的持续条件（Ongoing Tag Requirements）
   - 免疫系统

**第二梯队（高度推荐）**：

5. **Meta Attributes 设计模式**
   - 将"Damage"作为临时中间属性，而非直接修改 Health
   - 分离「造成伤害」和「承受伤害」的逻辑
   - 支持护盾吸收、伤害类型分流等复杂场景

6. **ExecutionCalculation 的 Source/Target 属性采集模式**
   - 声明式地定义需要采集哪些属性
   - 是否快照由配置决定
   - 对应我们 Effect 中应该携带哪些数据

7. **Cost/Cooldown 作为特殊 GE 的设计**
   - 消耗用 Instant GE 表达
   - 冷却用 Duration GE + Tag 表达
   - 统一的 GE 管线处理所有这些

8. **GameplayEffectContext（效果上下文）**
   - 记录「谁」「用什么」「在哪里」「对谁」施加了效果
   - 可子类化携带自定义数据
   - 在整个效果管线中传递

**第三梯队（参考但需大幅适配）**：

9. **Stacking 策略**
   - Aggregate by Source vs Aggregate by Target
   - 堆叠上限、持续时间刷新、周期重置
   - 需要在我们的 Effect Apply 中实现对应的堆叠逻辑

10. **TargetData 作为标准化数据结构**
    - 不是 TargetActor 那套客户端瞄准机制
    - 而是**目标信息的多态容器**概念——可以携带 HitResult、Actor 列表、位置等
    - 对应我们 Effect 中的目标描述部分

---

## 5. 参考链接

### 核心文档
- [tranek/GASDocumentation](https://github.com/tranek/GASDocumentation) — 最全面的社区 GAS 文档（5.7k stars），本调研的主要信息源
- [Epic 官方 GAS 文档](https://dev.epicgames.com/documentation/en-us/unreal-engine/gameplay-ability-system-for-unreal-engine) — 官方概述
- [Gameplay Effects 官方文档](https://dev.epicgames.com/documentation/en-us/unreal-engine/gameplay-effects-for-the-gameplay-ability-system-in-unreal-engine) — GE 详细说明（含 Cue 配置表）
- [Understanding the Unreal Engine GAS](https://dev.epicgames.com/documentation/en-us/unreal-engine/understanding-the-unreal-engine-gameplay-ability-system) — 官方概念分解

### Gameplay Cues
- [Gameplay Cues 教程 - Epic Dev Community](https://dev.epicgames.com/community/learning/tutorials/Zmk3/unreal-engine-gameplay-cues) — Cue 设置教程
- [Devtricks: GAS Truth](https://vorixo.github.io/devtricks/gas/) — 社区深度分析文章，含 Cue 的 unreliable 本质讨论

### Targeting
- [Target Data Filters in GAS - fp12's Blog](https://fp12.github.io/blog/2020/08/10/target-data-filters-in-gas) — TargetData 过滤器详解
- [Target Data in Gameplay Abilities - The Games Dev](https://www.thegames.dev/?p=242) — TargetData 自定义实践
- [UAbilityTask_WaitTargetData API](https://dev.epicgames.com/documentation/en-us/unreal-engine/API/Plugins/GameplayAbilities/UAbilityTask_WaitTargetData) — 官方 API

### Unity GAS 替代方案
- [sjai013/unity-gameplay-ability-system](https://github.com/sjai013/unity-gameplay-ability-system) — Unity GAS 移植（已归档，MIT）
- [h2v9696/UnityGAS](https://github.com/h2v9696/UnityGAS) — 活跃的 Unity GAS 实现
- [GASify](https://github.com/felipeggrod/gasify) — Unity Asset Store 的 GAS 实现

### ECS 与 Ability System
- [Entity-Component-Worker Architecture - GameDeveloper](https://www.gamedeveloper.com/programming/the-entity-component-worker-architecture-and-its-use-on-massive-online-games) — SpatialOS 的 ECW 架构
- [Design decisions when building games using ECS](https://arielcoppes.dev/2023/07/13/design-decisions-when-building-games-using-ecs.html) — Iron Marines 的 ECS 技能系统实践
- [Designing a Flexible Ability System - Medium](https://medium.com/@galiullinnikolai/designing-a-flexible-ability-system-for-games-1e2ba31beee1) — 通用技能系统设计模式

### 网络 / 延迟补偿
- [Lag Compensation - Gabriel Gambetta](https://www.gabrielgambetta.com/lag-compensation.html) — 经典的延迟补偿系列文章
- [SnapNet: Lag Compensation in UE5](https://snapnet.dev/blog/performing-lag-compensation-in-unreal-engine-5/) — UE5 中的 Server Rewind 实现
- [Unity Netcode: Dealing with Latency](https://docs.unity3d.com/Packages/com.unity.netcode.gameobjects@2.7/manual/learn/dealing-with-latency.html) — Unity 延迟处理方案

### 内部参考
- `docs/design/adaptation_guide.md` — M10（攻方快照 + 守方裁决）模式定义
- `sched/world.go` — WorldView 接口定义（空间查询 API 待设计）

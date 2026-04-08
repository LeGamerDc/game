# GAS (Gameplay Ability System) 调研笔记

> 调研目标：理解 UE GAS 中 Ability 激活、Tag 控制、打断/互斥、ASC 职责，并分析与我们 Scheduler 框架的映射关系。

---

## 1. GameplayAbility 激活流程

### 1.1 激活调用链

GAS 中技能激活的核心调用链：

```
ASC::TryActivateAbility(Handle)
  → ASC::InternalTryActivateAbility(Handle)
      → GA::CanActivateAbility()           // ← 所有前置检查
          → GA::DoesAbilitySatisfyTagRequirements()
              → CheckForRequired lambda     // 检查 Required Tags
              → CheckForBlocked lambda      // 检查 Blocked Tags
          → GA::CheckCooldown()             // Cooldown GE 是否过期
          → GA::CheckCost()                 // 资源是否足够
      → GA::PreActivate()                   // 设置 bIsActive, bIsCancelable
          → ASC::ApplyAbilityBlockAndCancelTags()  // 应用 Block/Cancel Tags
      → GA::CallActivateAbility()           // 进入用户逻辑
          → GA::ActivateAbility()           // 用户重写点
```

### 1.2 CanActivateAbility 检查链

`CanActivateAbility()` 是激活的守门人，按顺序执行以下检查：

| 检查项 | 说明 | 失败 Tag |
|--------|------|----------|
| **Tag Requirements** | 通过 `DoesAbilitySatisfyTagRequirements()` 检查 | `ActivateFailTagsBlockedTag` / `ActivateFailTagsMissingTag` |
| **Cooldown** | `CheckCooldown()` 检查 Cooldown GE 是否仍在生效 | `ActivateFailCooldownTag` |
| **Cost** | `CheckCost()` 检查 Attribute 是否足够支付 | `ActivateFailCostTag` |
| **Is Dead** | Actor 是否已死亡 | `ActivateFailIsDeadTag` |
| **Networking** | 网络权限检查 | `ActivateFailNetworkingTag` |

所有检查失败时会通过 `ASC::NotifyAbilityFailed()` 回调通知，携带对应的失败原因 Tag。

### 1.3 CommitAbility：两阶段设计

GAS 的激活采用**两阶段设计**：

1. **CanActivateAbility → ActivateAbility**：能力被"激活"，开始执行逻辑，此时 Block/Cancel Tags 已经生效。
2. **CommitAbility**：在 ActivateAbility 内部由用户代码主动调用，真正扣除 Cost 和应用 Cooldown。

```
CommitAbility()
  → CommitCheck()        // 再次检查 Cost + Cooldown（因为激活后可能状态已变）
  → CommitExecute()      // 真正扣除 Cost、应用 Cooldown GE
```

这意味着能力可以在 ActivateAbility 阶段做一些预检（如环境探测、射线检测），如果条件不满足就直接 `EndAbility()` 退出，而不会扣资源。但要注意：PreActivate 阶段的 Block/Cancel Tags 已经生效了——即使最终没有 Commit，其他能力可能已经被短暂阻断或取消。

### 1.4 Cost 实现

- Cost 通过一个 **GameplayEffect** 定义（`CostGameplayEffectClass`）。
- 该 GE 的 Modifier 是 **Additive**（减少 Attribute，如扣除 Mana）。
- `CheckCost()` 通过 `FActiveGameplayEffectsContainer::CanApplyAttributeModifiers()` 预检：模拟应用 Modifier，检查结果是否会使 Attribute 降至合法范围以下。
- `CommitExecute()` 真正 Apply 这个 GE。

### 1.5 Cooldown 实现

- Cooldown 也通过一个 **GameplayEffect** 实现（`CooldownGameplayEffectClass`）。
- 该 GE 的 Duration Policy = **Has Duration**（或 Infinite）。
- GE 必须通过 `UTargetTagsGameplayEffectComponent` 给 target Actor 授予一个 Cooldown Tag（如 `Cooldown.Ability.Fireball`）。
- `CheckCooldown()` 的逻辑：检查 ASC 上是否存在与 Cooldown GE 匹配的 GrantedTags → 若存在，说明仍在冷却中。
- Cooldown 持续时间可以通过 Scalable Float、SetByCaller、Curve Table 等方式配置。

### 1.6 Ability Activation Policy

GAS 原生提供的激活方式：

| 方式 | 说明 |
|------|------|
| `TryActivateAbility(Handle)` | 通过 AbilitySpecHandle 激活 |
| `TryActivateAbilityByClass(Class)` | 通过 GA 类激活（缺乏灵活性，不推荐） |
| `TryActivateAbilitiesByTag(TagContainer)` | 通过 Tag 匹配激活（推荐，支持多态） |
| **Ability Trigger** | 配置 Trigger Tags，当 GameplayEvent 被发送到 ASC 时自动激活 |
| **Input Binding** | 绑定到输入动作，按下时激活（Lyra 扩展了 WhileInputActive 等策略） |

Lyra 项目扩展的 Activation Policy：
- `OnInputTriggered`：按下时激活
- `WhileInputActive`：持续按住时保持激活
- `OnSpawn`：授予时自动激活（被动技能）

---

## 2. GameplayTags 在 GAS 中的角色

### 2.1 Tag 的本质

GameplayTags 是 UE 中的**层级化标签系统**：
- 以 `.` 分隔的层级结构，如 `Ability.Skill.Fire`、`Status.CC.Stun`
- 支持层级匹配：拥有 `Damage.Magic.Fire` 的实体会匹配 `Damage.Magic` 和 `Damage` 的查询
- 存储在 `FGameplayTagContainer` 中，支持 `HasTag`、`HasAny`、`HasAll` 等查询

### 2.2 Ability 上的 Tag 配置

每个 GameplayAbility 的 CDO（Class Default Object）上有以下 Tag 配置区块：

| Tag 容器 | 作用 | 检查时机 |
|-----------|------|----------|
| **Ability Tags** | 标识该能力自身的标签（如 `Ability.Skill.Fireball`） | 被其他能力的 Block/Cancel 匹配 |
| **Cancel Abilities With Tag** | 当该能力激活时，取消所有 Ability Tags 匹配的正在执行的能力 | PreActivate 阶段 |
| **Block Abilities With Tag** | 当该能力激活期间，阻止所有 Ability Tags 匹配的能力被激活 | 持续生效直到 EndAbility |
| **Activation Owned Tags** | 能力激活期间赋予 owner ASC 的 Tags | PreActivate → EndAbility |
| **Activation Required Tags** | owner ASC 必须拥有这些 Tags 才能激活该能力 | CanActivateAbility |
| **Activation Blocked Tags** | owner ASC 拥有任一这些 Tags 时阻止激活 | CanActivateAbility |
| **Source Required Tags** | Source（触发者）必须拥有的 Tags | CanActivateAbility |
| **Source Blocked Tags** | Source 拥有则阻止激活 | CanActivateAbility |
| **Target Required Tags** | Target 必须拥有的 Tags | CanActivateAbility |
| **Target Blocked Tags** | Target 拥有则阻止激活 | CanActivateAbility |

### 2.3 Tag 激活检查的核心逻辑

`DoesAbilitySatisfyTagRequirements()` 的伪代码：

```
func DoesAbilitySatisfyTagRequirements(asc, sourceTags, targetTags):
    // 1. 检查该能力的 AbilityTags 是否被 ASC 当前 BlockedAbilityTags 阻断
    if asc.BlockedAbilityTags.HasAny(ability.AbilityTags):
        return blocked

    // 2. 检查 Activation Required/Blocked Tags
    if not asc.OwnedTags.HasAll(ability.ActivationRequiredTags):
        return missing
    if asc.OwnedTags.HasAny(ability.ActivationBlockedTags):
        return blocked

    // 3. 检查 Source Required/Blocked Tags
    // 4. 检查 Target Required/Blocked Tags
    // ... 同理
    return ok
```

关键点：**所有 Tag 检查都是基于 ASC 的 OwnedTags 和 BlockedAbilityTags 进行的，这是 owner 私有状态**。Source/Target Tags 在 ability 激活时传入，不需要主动查询其他实体的实时状态。

### 2.4 GameplayEffect 中的 Tag

GameplayEffect 通过 GEComponent 与 Tags 交互：

| GEComponent | 作用 |
|-------------|------|
| `UTargetTagsGameplayEffectComponent` | 将 Tags 授予 (Grant) 给 target ASC（GE 移除时 Tags 也被移除） |
| `UBlockAbilityTagsGameplayEffectComponent` | 在 GE 存续期间阻止匹配 Tags 的能力激活 |
| `UTargetTagRequirementsGameplayEffectComponent` | GE 的应用/持续条件：target 必须满足特定 Tag 要求 |
| `UAssetTagsGameplayEffectComponent` | GE 资产自身的标签（不转移到 Actor） |
| `UImmunityGameplayEffectComponent` | 基于 Tag 实现免疫，阻止特定 GE 的应用 |
| `URemoveOtherGameplayEffectComponent` | 基于条件移除其他活跃 GE |

典型流程：眩晕技能 → Apply GE(Duration=3s) → Grant Tag `Status.CC.Stun` → 目标 ASC 拥有该 Tag → 目标的移动能力因 `Activation Blocked Tags` 包含 `Status.CC` 而被阻断。

### 2.5 与我们 tag 包的对应性

| UE GameplayTags 特性 | 我们的 tag 包 | 对应情况 |
|----------------------|---------------|----------|
| 层级结构（`.` 分隔） | `DB.Compile("a.b.c")` 自动建立层级 | ✅ 完全对应 |
| `HasTag` 单标签查询 | `Tag.HasTag(id)` | ✅ 完全对应 |
| `HasAny` / `HasAll` | `Tag.Match(Query)` 的 Some/All 语义 | ✅ 完全对应 |
| 层级匹配（拥有子 Tag 自动匹配祖先） | `Tag.rebuildCache` 在 AddTag 时构建祖先闭包 | ✅ 完全对应 |
| 引用计数（多个 GE 授予同一 Tag） | `Tag.count` 使用 ArrayMap 引用计数 | ✅ 完全对应 |
| GE 移除时自动移除 Granted Tags | `Tag.RemoveTag` 递减计数，归零时移除 | ✅ 可直接支持 |
| `FGameplayTagContainer`（Tag 集合） | `Tag` 结构体本身 | ✅ 对应 |
| Query 编译态优化 | `NewQuery` 支持冗余消除、冲突检测 | ✅ 我们更强（编译态优化） |
| Blocked Tags 查询（反向匹配） | `Query` 的 None 语义 | ✅ 对应 |

**结论：我们的 tag 包在功能上完全覆盖 GAS 中 GameplayTags 的核心用途，且在编译态优化方面更优。可以直接用作 GAS 中 Tag 检查的基础设施。**

---

## 3. 能力打断/取消机制

### 3.1 Cancel vs End

GAS 区分两种结束方式：

- **EndAbility**：正常结束。能力逻辑执行完毕后主动调用。
- **CancelAbility**：被打断取消。可以是自身主动取消，也可以是外部强制取消。

两者都会触发 `OnEndAbility` 回调，通过 `bWasCancelled` 参数区分。

### 3.2 Tag-Based 自动取消

当能力 A 激活时，GAS 自动处理：

```
PreActivate():
    ASC.ApplyAbilityBlockAndCancelTags(
        A.AbilityTags,
        A.BlockAbilitiesWithTag,    // 添加到 ASC.BlockedAbilityTags
        A.CancelAbilitiesWithTag    // 立即取消匹配的活跃能力
    )
```

具体流程：
1. 遍历 ASC 上所有活跃能力
2. 对每个活跃能力 B，检查 B.AbilityTags 是否与 A.CancelAbilitiesWithTag 有交集
3. 若有交集且 B.CanBeCanceled()，调用 B.CancelAbility()

Block 的工作方式类似但方向相反：
1. A 激活时，A.BlockAbilitiesWithTag 被加入 ASC.BlockedAbilityTags（引用计数）
2. 后续任何能力 C 尝试激活时，CanActivateAbility 会检查 C.AbilityTags 是否被 BlockedAbilityTags 匹配
3. A 结束时，Block Tags 的引用计数递减

### 3.3 AbilityTask 的取消传播

当能力被取消时，清理机制：

```
GA::EndAbility(bWasCancelled):
    // 遍历所有活跃的 AbilityTasks，逆序清理
    for task in ActiveTasks (reverse):
        task.TaskOwnerEnded()
            → task.OnDestroy()                        // Task 自身清理
            → GameplayTasksComponent.OnTaskDeactivated()  // 从组件注销

    ActiveTasks.Reset()

    // 移除 Activation Owned Tags
    // 移除 Block Tags（从 ASC.BlockedAbilityTags 中递减）
```

关键点：
- AbilityTask 可通过 `EndTask()` 自行终止，也可等待所属 Ability 结束时被自动终止
- Task 的终止回调可用于清理资源（停止动画、移除特效等）
- Task 通过 Delegate（C++）或 Output Pin（Blueprint）影响执行流

### 3.4 外部事件触发的打断

典型场景——控制技能打断正在施法的目标：

```
1. 攻击者释放眩晕技能
2. 攻击者的能力逻辑创建 GameplayEffectSpec（Stun GE）
3. 通过 ASC::ApplyGameplayEffectSpecToTarget() 应用到目标的 ASC
4. 目标的 Stun GE 生效：
   a. Grant Tag: Status.CC.Stun → 添加到目标 ASC.OwnedTags
   b. Block Ability Tags: Ability.* → 阻止目标激活新能力
5. 目标 ASC 检查所有活跃能力：
   - 如果活跃能力配置了 "当拥有 Status.CC.Stun 时取消" → 取消该能力
   - 这通常通过 AbilityTask (WaitGameplayTagAdd) 实现：
     能力在执行中监听特定 Tag 的添加，收到后自行 CancelAbility
```

也可以通过 `ASC::CancelAbilities(WithTags)` 从外部代码直接取消匹配的能力。

### 3.5 Gameplay Tag Relationship Mapping

Lyra 项目引入了一个更集中的管理方式：`AbilityTagRelationshipMapping`（DataAsset）。

它将 Block/Cancel 关系从每个能力的 CDO 中提取出来，集中到一个数据资产中：

```
struct AbilityTagRelationship:
    AbilityTag:              Tag     // 触发能力的标签
    AbilityTagsToBlock:      Tags    // 该能力激活时阻断的标签
    AbilityTagsToCancel:     Tags    // 该能力激活时取消的标签
    ActivationRequiredTags:  Tags    // 额外的激活条件
    ActivationBlockedTags:   Tags    // 额外的激活阻断条件
```

在 ASC 中重写 `GetAdditionalActivationTagRequirements()` 来注入这些额外规则。这使得能力间的互斥关系可以在一个地方统一查看和管理。

---

## 4. AbilitySystemComponent (ASC) 的职责

### 4.1 ASC 的核心角色

ASC 是 GAS 的**中央调度器和状态容器**，附着在 Actor 上，是一个"大管家"：

| 职责域 | 具体功能 |
|--------|----------|
| **能力管理** | GiveAbility / ClearAbility / TryActivateAbility / CancelAbility |
| **活跃能力追踪** | 维护 ActivatableAbilities 列表，追踪每个能力的状态 |
| **Effect 管理** | ApplyGameplayEffect / RemoveActiveGameplayEffect |
| **活跃 Effect 容器** | ActiveGameplayEffects：所有 Duration/Infinite GE 的运行时状态 |
| **Attribute 宿主** | 持有 AttributeSet，管理 Attribute 的 Base/Current 值 |
| **Tag 状态** | OwnedGameplayTags（当前拥有的所有 Tags，来自 GE 的 Grant 和 Loose Tags） |
| **Block 状态** | BlockedAbilityTags（当前被阻断的 Ability Tags，来自活跃能力的 BlockAbilitiesWithTag） |
| **Gameplay Cue** | 触发和管理视觉/音效表现 |
| **网络复制** | 能力状态、Effect、Attribute 的网络同步 |
| **事件路由** | HandleGameplayEvent → 将事件分发给匹配的能力 |

### 4.2 ASC 的状态构成

```
ASC 的运行时状态:
├── ActivatableAbilities[]       // 已授予的能力列表（AbilitySpec）
│   └── 每项包含: GA 类, Handle, IsActive, SourceObject, Level...
├── ActiveGameplayEffects[]      // 活跃的持续性 GE
│   └── 每项包含: GESpec, StartTime, Duration, Stacks, GrantedTags...
├── AttributeSets[]              // Attribute 集合
│   └── 每项包含: float 值（Base + Current）
├── OwnedGameplayTags           // 当前拥有的所有 Tags（聚合）
├── BlockedAbilityTags          // 当前被 block 的 Ability Tags（引用计数）
└── GameplayTagEventCallbacks   // Tag 变化的监听注册
```

### 4.3 Owner vs Avatar

GAS 区分两个 Actor 角色：
- **Owner**：拥有 ASC 的 Actor（可以是 PlayerState、Controller）
- **Avatar**：执行能力的物理 Actor（通常是 Character/Pawn）

这个区分主要是为了 UE 的网络复制需求。在我们的无网络框架中，两者合一即可。

### 4.4 ASC 的职责边界

ASC **负责**：
- 能力的生命周期管理（授予、激活、结束、取消）
- Tag 状态的维护和查询
- Effect 的应用和 Attribute 的修改
- 能力间的互斥/阻断裁决

ASC **不负责**：
- 能力的具体游戏逻辑（由 GA 子类实现）
- 输入系统的绑定（由外部代码完成）
- 空间查询、碰撞检测（由游戏逻辑层完成）
- 动画播放的细节（通过 AbilityTask 委托）

---

## 5. 与 Scheduler 的映射分析

### 5.1 ASC ↔ Logic 的映射

| GAS 概念 | Scheduler 概念 | 映射方式 |
|----------|----------------|----------|
| ASC（能力容器） | Logic 内部子系统 | ASC 的功能作为 Logic 的私有状态和子逻辑组织 |
| GA（GameplayAbility） | Logic 内部的能力实例 | 由 Logic 在 Think 中调度，产出 Effect/Signal |
| GE（GameplayEffect） | Typed Effect | 跨实体的状态修改请求 |
| Gameplay Event | Signal | 跨实体的事件通知 |
| Attribute | Logic 的内部状态字段 | 在 Apply 中被 Effect 修改 |
| OwnedTags | Logic 的内部 `tag.Tag` 字段 | 私有状态，Think/Apply 均可读取 |
| BlockedAbilityTags | Logic 的内部 `tag.Tag` 字段 | 由活跃能力管理 |

**核心映射**：ASC 不是一个独立的 Logic，而是每个 Logic 内部的子系统。一个 Logic（如角色）内部持有自己的 AbilitySystem，管理自己的能力、Tag 状态、Attribute。

### 5.2 Tag-Based 激活检查：纯 Owner 私有状态

**结论：Tag-based 激活检查几乎完全是 owner 私有状态检查，不需要跨 owner 信息。**

分析：
- `CanActivateAbility` 检查的所有 Tag 条件（Required/Blocked）都基于 **owner ASC 的 OwnedTags 和 BlockedAbilityTags**。
- Source Tags 和 Target Tags 虽然涉及其他实体，但它们是在**触发激活时作为参数传入**的，不需要实时读取其他实体的状态。
- 在 Scheduler 模型中，这意味着：
  - Think 阶段读取 World 快照获取 Source/Target 信息（如果需要）
  - 但大部分激活检查只看自己的 Tags → **完全在 Think 本地完成**
  - Cost 检查看自己的 Attribute → **完全在 Think 本地完成**
  - Cooldown 检查看自己的 Tags → **完全在 Think 本地完成**

这与 Scheduler 的设计非常契合：Think 阶段只读取世界快照 + 自身状态，就足以完成所有激活决策。

### 5.3 技能打断/取消的语义分析

打断/取消存在两种场景，对应不同的 Scheduler 语义：

#### 场景 A：自身能力间的互斥（纯本地）

角色自己释放能力 A，导致取消自己的能力 B。

- **语义**：纯本地逻辑，不涉及跨 entity。
- **映射**：在 Logic 的 Think 阶段内部处理。能力 A 激活时，Logic 内部的 AbilitySystem 检查 Cancel Tags，直接取消能力 B。
- **不需要 Signal 或 Effect**。

#### 场景 B：跨实体的打断（如眩晕）

攻击者 X 对目标 Y 施放眩晕。

- **攻击侧（X 的 Think）**：
  - 决定释放眩晕技能
  - 产出 Effect：`StunEffect { target: Y, duration: 3s }`
  - 或者产出 Signal：`StunSignal { target: Y }` + Effect：`ApplyStunGE { target: Y }`

- **目标侧（Y 的 Apply）**：
  - 收到 StunEffect
  - 裁决：是否免疫？是否有减少持续时间的 Attribute？
  - 若接受：Apply GE → Grant Tag `Status.CC.Stun` → 更新自己的 OwnedTags
  - 取消当前被 Stun 阻断的能力（本地处理）

- **结论**：跨实体打断是 **Effect 语义**（状态修改请求），不是 Signal 语义。
  - Effect 携带 "施加什么状态" 的声明
  - 目标在 Apply 阶段裁决是否接受、如何修改
  - 目标本地的能力取消是裁决结果的副作用

#### 场景 C：事件触发的能力激活

X 打死 Y，Y 触发"死亡爆炸"被动能力。

- **语义**：Signal。X 发出"击杀" Signal，Y 收到后在 Think 阶段激活被动能力。
- **映射**：Y 的 WatchState 声明 Interest(`KillSignal`)，X 通过 Emit 发出 Signal。

### 5.4 映射总结

```
┌──────────────────────────────────────────────────────┐
│                    Logic (= Owner)                    │
│  ┌─────────────────────────────────────────────────┐  │
│  │              AbilitySystem (内部子系统)            │  │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────────┐  │  │
│  │  │ Abilities │  │ Tags     │  │ Attributes   │  │  │
│  │  │ (GA list) │  │ (Owned)  │  │ (HP/MP/...)  │  │  │
│  │  │           │  │ (Blocked)│  │              │  │  │
│  │  └──────────┘  └──────────┘  └──────────────┘  │  │
│  │  ┌──────────────────────────────────────────┐   │  │
│  │  │ ActiveEffects (Duration GEs tracking)    │   │  │
│  │  └──────────────────────────────────────────┘   │  │
│  └─────────────────────────────────────────────────┘  │
│                                                        │
│  Think(ctx, inbox):                                    │
│    - Read world snapshot                               │
│    - Read inbox (Signals from other Logics)             │
│    - AbilitySystem.ProcessPendingActivations()          │
│    -   -> CanActivateAbility (local tag/cost/cd check)  │
│    -   -> Emit Signals, Publish Effects                 │
│                                                        │
│  Apply(ctx, inbox):                                    │
│    - Receive Effects from other Logics                  │
│    - AbilitySystem.ApplyIncomingEffects(inbox)           │
│    -   -> Modify Attributes                             │
│    -   -> Grant/Remove Tags                             │
│    -   -> Cancel blocked abilities (local)              │
│    -   -> Tick duration GEs (decrement, expire)         │
└──────────────────────────────────────────────────────┘
```

### 5.5 设计启示

1. **Tag 系统可直接复用**：我们的 `tag` 包完全覆盖 GAS 的 Tag 需求。每个 Logic 内部持有 `tag.Tag` 实例即可。

2. **激活检查是纯本地的**：不需要跨 entity 协调，完全在 Think 阶段完成。这是 GAS 设计的一个优秀特性——通过把"别人对我的影响"编码为"我自己的 Tag 状态变化"（Apply 阶段由 Effect 完成），将跨实体交互转化为本地状态查询。

3. **能力互斥/打断分两层**：
   - **本地层**：同一 Logic 内部的能力互斥，在 Think 中解决
   - **跨实体层**：通过 Effect 修改目标的 Tag 状态，目标在 Apply 中裁决后本地处理打断

4. **Cooldown 可以用 TimerWheel**：GAS 用 Duration GE + Tag 实现 Cooldown。我们已有 TimerWheel，可以直接用 timer 到期移除 Cooldown Tag，比 GE 的方式更轻量。

5. **Cost 检查是 Attribute 的预检**：扣除 Attribute 之前先检查余量是否足够。在我们的模型中，Attribute 是 Logic 私有状态，Think 阶段直接读取判断即可。

6. **CommitAbility 的两阶段模式值得借鉴**：先激活（可以做预检、环境探测），再 Commit（真正扣资源）。这在"技能需要瞄准/确认"的场景中很有用。

---

## 6. 参考链接

### 官方文档
- [Gameplay Ability System Overview](https://dev.epicgames.com/documentation/en-us/unreal-engine/understanding-the-unreal-engine-gameplay-ability-system)
- [Gameplay Effects Documentation](https://dev.epicgames.com/documentation/en-us/unreal-engine/gameplay-effects-for-the-gameplay-ability-system-in-unreal-engine)
- [GAS Best Practices for Setup](https://dev.epicgames.com/community/learning/tutorials/DPpd/unreal-engine-gameplay-ability-system-best-practices-for-setup)
- [Your First 60 Minutes with GAS](https://dev.epicgames.com/community/learning/tutorials/8Xn9/unreal-engine-epic-for-indies-your-first-60-minutes-with-gameplay-ability-system)

### 社区资源
- [tranek/GASDocumentation](https://github.com/tranek/GASDocumentation) — 最全面的社区文档，Epic 官方推荐
- [Devtricks: The truth of GAS](https://vorixo.github.io/devtricks/gas/) — 优缺点分析，Light Dart 能力完整解析
- [GameDev Pensieve: Gameplay Ability](https://www.gamedevpensieve.com/engines/unreal/unreal_plugin/unreal_plugin_gameplay-ability) — 简洁的 API 速查
- [Gameplay Ability - hzFishy's Notes](https://notes.hzfishy.fr/Unreal-Engine/GAS/Types/Gameplay-Ability) — 激活链路源码分析
- [Mastering Gameplay Ability (yuewu.dev)](https://www.yuewu.dev/en/wiki/NpEQIOqYrAhp-pPhrSQUY) — 能力实例管理详解

### 源码级分析
- [StackOverflow: When is a GA considered executed](https://stackoverflow.com/questions/52797736/unreal-gas-when-a-gameplayability-is-considered-as-executed) — PreActivate/CommitAbility 生命周期分析
- [StackOverflow: Why GA failed to activate](https://stackoverflow.com/questions/53108639/how-to-determine-why-a-gameplayability-failed-to-activate) — 失败原因 Tag 机制
- [UE Forum: Tag Relationship Mapping](https://forums.unrealengine.com/t/gameplay-ability-system-course-project-development-blog/1419542) — Lyra 风格集中化 Tag 关系管理
- [UE Forum: Block Abilities With Parent Tags](https://forums.unrealengine.com/t/ue5-5-4-gas-gas-block-abilities-with-tag-not-working-with-parent-tags/2553134) — Tag 层级匹配在 Block 中的行为

### 学术/会议
- [GAS: From Programming Framework to Designer's Tool (PDF)](https://blog.cnam.fr/medias/fichier/ue-gas-from-programming-framework-to-deisgner-s-tool_1643896090840-pdf) — GAS 架构概述论文
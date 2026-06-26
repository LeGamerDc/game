# MonkeyBT 行为树评估、优化与本轮改进记录

> 评估对象：`bt/`（核心运行时）+ 相关调度集成 `sched/`
> 评估方法：① 逐文件精读源码（`node.go` / `branch.go` / `decorator.go` / `stk.go` / `blackboard`）；② 编写探针/回归测试验证关键行为；③ 对照成熟实现（BehaviorTree.CPP v3/v4、Unreal Engine BT、py_trees、经典游戏 AI 文献）；④ 交叉比对项目自身设计文档。
> 本文已结合作者反馈更新：标注「✅ 本轮已实现」的项目已落地并配回归测试；「ℹ️ 澄清/撤回」为经反馈确认不成立或不强调的条目。

---

## 0. 结论速览（TL;DR）

**核心执行模型是优秀且自洽的**：把「当前活跃路径」做成一等的 continuation 栈（manual stack），Node（只读定义）与 Task（运行态）分离，使得「叶子挂起后从叶子恢复、事件路由到活跃任务、取消即栈展开、由树告诉调度器下次何时唤醒」被统一在一个模型里。

| 级别 | 项目 | 结论 |
|------|------|------|
| ✅ 本轮已实现 | 反应式优先级抢占 | 新增 `NewReactiveSelector` / `NewReactiveSequence`，支持高优先级分支抢占运行中的低优先级分支 |
| ✅ 本轮已实现 | 并行阈值/快速失败 | `NewParallel` 改为独立的 `successRequire` / `failRequire` + `failFast` |
| ✅ 本轮已实现 | 确定性随机 | `NewStochastic*` 构造时注入 `Rand`，移除全局 `math/rand` 依赖 |
| 🟠 仍开放 | 常用装饰器 | 缺 `Timeout` / `Cooldown` / `RateLimit` / `Retry` / `Delay` |
| 🟠 仍开放 | 调试工具链 | 无可视化 / trace / 运行时 introspection |
| 🟡 仍开放 | 性能 | 子树激活分配无对象池；泛型栈操作未内联；缺基准 |
| 🟡 仍开放 | 子树参数化 | 指针可复用子树，但无端口重映射/命名空间 |
| ℹ️ 澄清/撤回 | int32 delay「溢出」 | **非 bug**：返回值是 ms 级相对 delay，游戏中不可能出现 >2³¹ 的等待 |
| ℹ️ 澄清/撤回 | 调度边界丢失终态 | **撤回**：`Root.Execute` 返回值并非直接用作 `Think` 返回值，原推理不成立 |
| ℹ️ 文档即可 | `Guard` 语义 | `Guard` 是一次性前置检查，等价于其他 BT 的 condition+反应需用 `AlwaysGuard`；文档已述，无需强化 |

---

## 1. 设计亮点（保留）

1. **Node / Task 分离的 flyweight 模型**（`node.go` `Node` 只读，`Generate` 产出运行态 `Task`）。`Node` 运行期只读 → 一棵 Node 树可被大量 agent 共享；**子树可通过指针复用**（每次 `Generate` 产生独立 Task）。
2. **manual stack / continuation**（`stk.go`）。叶子 Running 时整条活跃路径保留，下一 tick 从栈顶叶子恢复，避免「每 tick 从 root 重新遍历」。`Execute(c, stk, from)` 用 `from` 区分进入时机，语义清晰。
3. **离散唤醒 + 事件驱动融合**。正数 `TaskStatus` = 相对 delay 提示；`OnEvent` 让外部事件提前打断等待。
4. **取消即栈展开**（`stk.go` `Cancel`）。`OnComplete(c, cancel)` 统一清理钩子。
5. **并行 = frame + 多 sub-root**（`branch.go` `joinBranch`），事件可转发并正确聚合唤醒时间。
6. **`cc` 表达式编译器**（`cc/`）：类型检查过的 guard/condition DSL，近原生性能。
7. **最小 `Ctx` 约束**，黑板由用户自定义。

---

## 2. 与成熟实现的横向对照（已更新本轮改进）

| 维度 | BehaviorTree.CPP v3/v4 | Unreal Engine BT | py_trees | **MonkeyBT** |
|------|------------------------|------------------|----------|--------------|
| 恢复模型 | tick 从 root | 事件驱动 | tick 从 root | **manual stack，从叶子恢复** ✅ |
| 反应式优先级抢占 | `ReactiveSequence`/`ReactiveFallback` | Observer Aborts | `memory=False` | **`ReactiveSelector`/`ReactiveSequence`** ✅ 本轮新增 |
| 记忆 vs 反应 | 两套都有 | abort 配置 | `memory` 开关 | **记忆型（Selector/Sequence）+ 反应型（Reactive*）** ✅ |
| 并行策略 | success/failure 双阈值 | 完成策略 | 多策略 | **successRequire + failRequire + failFast** ✅ 本轮重构 |
| 装饰器 | 丰富（Timeout/Retry/Cooldown/...） | 丰富 | 丰富 | Revise/Repeat/PostGuard/AlwaysGuard/Guard，**仍缺 Timeout/Cooldown/Retry/Delay** 🟠 |
| 随机 | 用户可控 | 引擎 RNG | 用户可控 | **注入 `Rand`** ✅ 本轮改 |
| 黑板 | typed ports + remapping | Blackboard keys | namespaces + 访问追踪 | 用户自定义；默认 `map`，**无 ports/remapping** 🟠 |
| 调试/可视化 | Groot | 行为树调试器 | 可视化/activity stream | **无** 🟠 |
| 表达式/脚本 | v4 scripting | Blackboard 条件 | Python 原生 | **`cc` 编译表达式** ✅ |

---

## 3. 本轮已实现的改进（含实现要点与回归测试）

### 3.1 ✅ 反应式优先级抢占：`ReactiveSelector` / `ReactiveSequence`

**背景**：原设计的 manual-stack 只有 `alwaysGuard`（向下中止自己的子树），无法表达「高优先级分支条件变 true 时抢占正在运行的低优先级分支」——这是战斗 AI 最常见诉求（巡逻中发现敌人立即转战斗）。

**实现**（`bt/branch.go` `reactiveBranch`，`bt/node.go` `NewReactiveSelector` / `NewReactiveSequence`）：
- 反应式分支像 `joinBranch`/`alwaysGuard` 一样是「重建栈的叶子」，内部为每个子节点持有一个 `Root`（per-child sub-root）。
- **每次 update 都从第 0 个子节点重新评估**（`sweep`）：恢复正在运行的子节点、重新生成并重评已完成的（条件）子节点。
  - ReactiveSelector：任一子节点成功 → 成功；某子节点 Running → 取消其后较低优先级子节点并返回 Running；全部失败 → 失败。
  - ReactiveSequence：任一子节点失败 → 失败（并取消正在运行的后续子节点）；Running → 返回；全部成功 → 成功。
- 当更靠前（更高优先级）的子节点重新变为 Running/Success 时，`classify` 调用 `haltFrom(i+1)` **取消正在运行的低优先级子树**，实现抢占。
- `OnEvent` 同样**事件驱动地评估抢占/中止**：先重评严格更高优先级的子节点 `[0, active)`（Phase 1，复用 `classify`/`haltFrom`）——Selector 中某高优先级分支变为可运行/成功，或 Sequence 中某前置条件失败，都会即时抢占/中止并取消当前运行子树；无抢占时再把事件转发给当前运行子节点（Phase 2），若其因事件完成则从其后继续 sweep。这样抢占与前置中止不再只在 tick 时发生，也能由事件即时触发（无需改动 `Guard` 接口）。
  - 注意：抢占只与它所搭载的事件一样及时。离散调度下，不伴随事件的高优先级条件要等当前子节点的 delay 提示到期、下一次 `Execute` 才被发现，故此类条件应「事件化」；连续 tick 调度每帧 `Execute` 重扫则不受影响。

**语义注意（已写入 godoc）**：反应式分支每 tick 会重新生成并重跑「已完成的」子节点，因此条件子节点必须**幂等、无副作用**（与 BT.CPP ReactiveSequence 相同的约束）。

**回归测试**（`bt/reactive_test.go`、`bt/reactive_onevent_test.go`）：
- `TestReactiveSelector_Preempt`：g 变 true 时高优先级分支（tick）抢占、低优先级被 `canceled`。
- `TestReactiveSequence_AbortOnConditionLost`：前置条件丢失时（tick）中止正在运行的动作。
- `TestReactiveSelector_EventInterrupt`：事件转发给运行子节点并正确结算。
- `TestReactiveSelector_EventPreemptsToHigherSuccess` / `...ToHigherRunning`：事件驱动抢占（高分支同步成功 / 变为运行）。
- `TestReactiveSequence_EventAbortsOnPreconditionLost`：事件驱动的前置中止。
- `TestReactiveSelector_EventPreemptionBeatsActiveCompletion`：抢占（Phase 1）优先于当前子节点借事件完成（Phase 2）。
- `TestReactiveSelector_LowerPriorityFlipDoesNotPreemptOnEvent`：只重评 `[0, active)`，低优先级就绪不抢占。
- `TestReactiveBranch_CancelPropagatesToActiveChild` / `...HitsActiveChildExactlyOnce`：拆除时当前子节点恰好被 `cancel=true` 取消一次。
- `TestReactiveSelector_ResumesActiveChildAcrossTicksWithoutRegen`：跨 tick 恢复（而非重建）当前运行子节点。

### 3.2 ✅ 并行阈值重构：`successRequire` / `failRequire` / `failFast`

**实现**（`bt/node.go` `NewParallel`，`bt/branch.go` `joinBranch.settle`）。新签名：
```go
func NewParallel[C, E](g Guard[C], successRequire, failRequire int32, failFast bool, ch ...*Node[C, E]) *Node[C, E]
```
结算逻辑（每次推进所有运行中子树后调用一次 `settle`）：
1. `success >= successRequire` → 成功，Cancel 其余；
2. `failRequire > 0 && fail >= failRequire` → 失败，Cancel 其余；
3. `running == 0`（全部完成仍不足）→ 失败；
4. `failFast && success + running < successRequire` → 失败（再也无法凑齐 successRequire）。

**`failRequire == 0 = 不设失败阈值`**（ergonomics 修正）：强制每个调用方都设一个失败阈值是负担，
而多数场景只关心成功阈值。`0` 个失败触发失败本就无意义，故用 `0` 作「禁用」哨兵语义清晰。
常用组合：AND = `(N, 0, failFast=true)`；OR = `(1, 0, false)`；M 选成功 = `(M, 0, true)`；
仅在需要「独立失败阈值」时才传 `failRequire > 0`（如 `TestParallel_FailCount`）。

> ⚠️ **failFast 边界确认点**：需求里写的是「`success + 剩余running <= successCnt` 立即失败」，但按「无法达成」语义这会**偏早一格**——例如 `successCnt=1`、还剩 1 个 running 时（`success(0)+running(1)=1 <= 1`）就会失败，可那唯一的 running 仍可能成功。因此本实现取**严格不可达边界 `success + running < successRequire`**（剩 0 个可成功才失败）。若你确实想要「无冗余即放弃」的更激进策略（`<=`），把 `branch.go` 中该判断改成 `<=` 即可（一字符）。请确认采用哪种。

**回归测试**（`bt/reactive_test.go`）：`TestParallel_FailFast`（不可达即失败并取消运行子树）、`TestParallel_NoFailFastKeepsRunning`、`TestParallel_FailCount`（独立失败阈值）。
旧 `CountMode` 不再用于并行（仅 Selector/Sequence/Repeat 内部使用）。

### 3.3 ✅ 确定性随机：注入 `Rand`

**实现**（`bt/node.go` `Rand` 接口 + `NewStochasticSelector/Sequence/SelectorN`，`bt/branch.go` `shuffleOrder`）：
- 新增最小接口 `Rand interface{ Shuffle(n int, swap func(i, j int)) }`（`*math/rand/v2.Rand` 直接满足）。
- 随机分支构造时**强制注入** `rng`（`_assert(rng != nil)`，`Check` 亦校验），`stochasticBranch` 用它打乱顺序，移除全局 `math/rand/v2` 依赖。
- 原 `NewSelector/NewSequence` 的 `shuffle bool` 形参被移除，拆分为「顺序」构造函数与 `NewStochastic*` 构造函数，语义更显式。

**注意（已写入 godoc）**：`Rand` 存放在（可共享的）`Node` 上。若一棵 Node 树被多个 owner 并发 tick，需提供对该共享模型安全的 `Rand`，或为每个 owner 构造独立树 + 独立 `Rand`（`*math/rand/v2.Rand` 非并发安全）。

**回归测试**：`TestStochastic_DeterministicWithSeed`（同种子 → 同访问顺序）。

---

## 4. 仍开放的设计/功能项（建议后续迭代）

### 4.1 🟠 缺常用装饰器
- `Timeout`：子树超 T 未完成则取消并失败（用 `Now()` 即可实现）。
- `Cooldown` / `RateLimit`：成功后 T 内不再执行 / 高频条件节流。
- `Retry(n)`：失败重试 n 次（与按成功计数的 `RepeatUntilNSuccess` 语义不同）。
- `Delay`：进入后等待 T 再跑子树。

### 4.2 🟠 调试与工程化工具链（对「游戏用」很关键）
- 运行时 introspection：导出当前活跃栈路径（调试「AI 现在在干嘛」）。
- trace / 事件日志 / 断点（类似 Groot、py_trees tree-watcher）。
- 离线可视化导出（DOT/JSON）；递归整树 `Validate()`（当前 `Check()` 只校验单节点）。

### 4.3 🟡 性能
- **子树激活分配**：每次 `Generate` 分配 Task；`joinBranch`/`reactiveBranch` 分配 `roots`；`stochastic` 分配 `order`。上千 agent、频繁重启子树时是 GC 压力。建议为 Task 引入 `sync.Pool`/freelist，切片按子节点数预分配复用；区分「长驻树」与「频繁重启子树」。
- 泛型 `push/top/pop` 不内联（`stk.go` 注释自认）；建议基准确认是否手工展开。
- 默认黑板 `map[string]Field` 高频读写不如整型 key/数组；为热点 agent 提供紧凑实现。
- **缺基准**：建议矩阵——deep-running-leaf 恢复 vs root-tick baseline；事件唤醒延迟；并行/反应式唤醒聚合；alloc/op。先有数字再坐实「高性能」主张。

### 4.4 🟡 子树参数化
- 指针复用子树虽可共享，但共享同一黑板键空间。可考虑一等 `SubTree` 引用 + 端口重映射，或类型化黑板端口（typed ports）/命名空间（py_trees 风格）。

### 4.5 🟡 其余次要项
- `reactiveBranch`/`joinBranch` 的反应式语义会**每 tick 重生成已完成子节点**——这是有意设计，但需在用户文档显著提示（避免把有副作用的动作放进反应式分支早段）。
- `task.OnEvent` 未对 `tt == nil` 兜底（当前调用路径安全，属防御性增强）。
- 单次 `Execute` 内无工作量预算：大 `MaxLoop` + 瞬时子节点 / 超大 sequence 会在一次调用内同步跑完，可能帧尖刺；可加「每 tick 最多推进 N 个节点」的可选预算。

---

## 5. 已澄清 / 撤回的条目（经作者反馈）

- **int32 delay「溢出」——非 bug**。`TaskStatus` 正数是 ms 级**相对 delay**（不是时间戳），游戏中不可能出现 >2³¹（约 24.8 天）的单次等待，故无需改 int64。已从问题列表移除。
- **「终态在调度边界被抹平」——撤回**。该推理基于「`Root.Execute` 返回值直接作为调度器 `Think` 返回值」的错误假设；实际接入层另有处理，原结论不成立。
- **`Guard` vs `AlwaysGuard` 语义——文档即可**。`Guard` 是一次性前置检查；需要「每 tick 复检并可中止」的（其他 BT 里的 condition/observer）语义应使用 `AlwaysGuard`。现有文档已描述，不再强调。
- **不建议手工构造 `Node`**。请统一使用 `NewXxx` 构造函数（已内置参数 `_assert` 与 `Check` 校验：并行的 `successRequire/failRequire` 范围、随机分支的 `rng != nil` 等）。因此「手工构造绕过 `Check`」一类问题不作为缺陷处理。

---

## 6. 优先级路线图（更新）

**本轮已完成**：反应式抢占（Reactive*）、并行双阈值 + failFast、随机注入 `Rand`。

**P1（下一步，常用能力/可用性）**
- [ ] 装饰器补全：`Timeout` / `Cooldown` / `Retry` / `Delay`。
- [ ] 运行时 introspection / trace（调试 AI 必需）。
- [ ] 递归 `Validate()` 整树校验。

**P2（性能/工程化）**
- [ ] Task 对象池、`joinBranch`/`reactiveBranch` 切片复用、热路径内联。
- [ ] SubTree 端口重映射 / 类型化黑板端口。
- [ ] 基准套件，坐实「高性能」主张。

---

## 附录：回归测试位置

本轮改进的行为均已落为回归测试，便于持续验证：

- `bt/reactive_test.go`
  - `TestReactiveSelector_Preempt` —— 高优先级抢占，低优先级被取消。
  - `TestReactiveSequence_AbortOnConditionLost` —— 前置条件丢失时中止运行动作。
  - `TestReactiveSelector_EventInterrupt` —— 事件转发与结算。
  - `TestParallel_FailFast` / `TestParallel_NoFailFastKeepsRunning` / `TestParallel_FailCount` / `TestParallel_FailRequireDisabled` —— 并行阈值语义（含 `failRequire=0` 禁用）。
  - `TestParallel_FailFastEqualityKeepsRunning` —— 锁定 failFast 严格 `<` 边界（等号仍可达，继续运行）。
  - `TestReactiveSequence_EventAdvancesActive` / `TestReactiveSelector_EventFailAdvances` —— 事件完成/失败后 active 推进。
  - `TestStochastic_DeterministicWithSeed` —— 注入 `Rand` 的可复现性。

> 上述 3 个边界测试由 Codex review 建议补充；Codex 结论为「未发现确定 bug」，并认可 failFast 采用严格 `<` 边界。
- `bt/node_test.go` —— 原有节点/运行态/取消/事件用例已迁移到新构造函数签名，保持通过。

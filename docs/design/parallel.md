# Parallel Tick Design

## 前置约定

- 本设计稿只表达设计意图与约束，不包含代码
- 接口定义以 `en/world.go` 为唯一权威来源
- 设计稿与代码出现矛盾时，以代码为准
- 相关理论抽象与术语沉淀见 [../references/parallel_theory.md](../references/parallel_theory.md)

## Goal

在保留 tick 驱动、可预测、易调试这些 MMORPG 服务器优点的前提下，把单线程串行 game loop 演进为一个可以按负载自动切换串行/并行的受限非确定性并发执行模型。

这个模型更接近 BSP (Bulk Synchronous Parallel) / actor message passing:

- Think 是基于同一份只读快照的并行计算
- Effect 是按目标 owner 聚合提交、但不同 owner 之间并行提交
- 每一轮之间有明确 barrier

## 核心概念: Logic = Owner

整个并行模型的基石是 ownership。本设计中 Logic 与 Owner 是同一个东西:

- 每个 Logic 实例就是一个独立的 owner
- Logic 内部可以组合任意多个子逻辑（技能系统、buff 系统、行为树等），类似 GAS 模式
- 内部子逻辑如何组织、调度、复用，完全是 Logic 实现的私有事务
- 对外（对调度器、对其他 Logic），一个 Logic 表现为单一的、不可分割的参与者

这意味着:

- 调度器调度的单位是 Logic
- Effect 投递的目标是 Logic 的 ref
- 每个 Logic 的 Think 和 Apply 内部不存在跨 owner 的并发问题
- 框架不需要知道 Logic 内部有几个技能在跑、几个 buff 在倒计时

## 核心判断

### 1. 先把"谁能改什么"限定清楚

真正能让这个模型成立的不是 Think/Effect 这两个名字，而是 ownership:

- Think 阶段:
  - 可以读 world / entity snapshot
  - 可以修改当前 Logic 的私有状态（含所有内部子逻辑的状态）
  - 不应该直接修改任何共享状态
- Effect 阶段（Apply）:
  - 只允许修改当前 Logic 自己拥有的公共状态
  - 同一个 round 内，同一个目标 Logic 的所有 effect 由该 Logic 自己的 Apply 处理
  - 默认语义下，这些 effect 的顺序不重要，可以视为无序集合
- World effect 阶段:
  - 只允许修改 world 级共享状态
  - world 本身也被视为一个特殊的 owner

因为 Logic = Owner，所以不存在"一个 entity 上跑多个 logic 导致 think 阶段写竞争"的问题。Logic 内部的子逻辑协调由 Logic 自己的实现保证，框架不干预。

如果 Logic 内部的某个子逻辑想修改本 Logic 的公共状态（如 HP），有两种选择:

- 在 Think 内部直接改（因为 Logic 就是 Owner，不存在并发竞争）
- 发一个 target=self 的 effect，走 Apply 统一处理

两种方式都合法，由 Logic 实现者根据场景选择。

### 2. 副作用必须是 typed effect，不是 closure

closure 方案短期实现快，但长期代价很高:

- 不容易做聚合、合并和无序 reduce
- 捕获上下文太隐式，不利于审计和回放
- 不利于网络同步、持久化、录制/replay
- alloc 和逃逸分析压力通常更差
- 调试时很难看出"到底发生了什么"

Think 的输出必须是显式的 typed effect 和 typed signal 记录，例如伤害、移动、加 buff、生成 NPC 等。

### 3. 事件监听应当数据化，不要在 effect 里直接回调

更稳妥的做法是:

1. Apply 处理 effect 时产出 typed signal
2. Signal router 根据投递目标把 signal 放入对应 Logic 的 inbox
3. Inbox 在本 tick 下一轮或未来 tick 变成新的 think frontier

这样做好处是:

- 保持 owner 边界清晰
- 避免 Apply 过程中出现任意重入
- 便于合并多个 signal
- 便于做预算控制和调试

## 数据分层

状态分成三层:

### 1. World state

全局唯一共享状态，例如:

- entity registry
- 空间索引
- 队伍/场景/副本索引
- 掉落池、刷怪点、导航网格等全局数据

### 2. Logic public state

Logic 拥有的、对其他 Logic 可见的状态，例如:

- HP/MP
- 位置、朝向、速度
- 阵营、死亡状态
- 公共 buff/debuff
- 可见 combat tag

### 3. Logic private state

Logic 独占的内部状态，包括所有子逻辑的状态，例如:

- 技能 channel 进度、CD 计时
- buff 的叠层内部计数
- 行为树运行栈
- 触发器的冷却和局部记忆
- 任何内部子逻辑的运行上下文

并发安全的关键是:

- Think 改 private state（含内部子逻辑状态）
- Apply 改 public state（处理收到的 effect）
- World effect 改 world state

## Tick 执行模型

每个 tick 不是单次 Think → Apply，而是多轮 superstep，直到当前 tick 工作队列清空或者达到预算上限。

### Tick pipeline

1. 收集 tick 初始 frontier: 网络输入、定时器到期、上一 tick 留下的 deferred signal/task
2. 如果 frontier 很小，走串行 runner；否则走并行 runner
3. 开始 round loop
4. 为当前 round 固定只读 snapshot
5. 并行执行所有 Think
6. 合并输出: entity effects、world effects、signals、future schedules、next-round think requests
7. 按目标 Logic 分桶并行执行 Apply
8. 执行 world effect（单线程，world 作为特殊 owner）
9. 路由 signals，生成 next frontier
10. 如果 next frontier 非空且未超 budget，则进入下一轮；否则结束 tick

### Budget

- max_rounds_per_tick
- max_effects_per_logic_per_tick
- max_generated_tasks_per_logic_per_tick
- max_world_effects_per_tick
- tick_time_budget

超限时不要继续追求同 tick 完成，应明确降级: 延后到下个 tick、丢弃低优先级事件、记录告警。

## World Effect

建议把 world 视为特殊 owner，并在每轮 Apply 之后执行 world effect:

- Entity effect: 并行，按 Logic owner 分桶提交
- World effect: 单线程，按 world owner 提交
- Signal routing: 基于最新已提交状态产生 next frontier

好处:

- spawn/despawn/area trigger 等可以在同 tick 下一轮可见
- 语义统一: world 只是另一个 owner
- 不必把大量机制强行推迟到下 tick

如果某类 world 操作非常重，可以单独归为 end-of-tick maintenance。

## 自动切换串行/并行

pipeline 语义只有一套，只是 runner 不同: serial runner 和 parallel runner。

切换依据可以来自:

- 当前 frontier 大小
- effect target 去重后的 owner 数量
- world effect 数量
- 最近 N tick 的平均耗时
- 当前线程池繁忙度

注意不要只看 think 个数——如果 100 个 think 最终都只打到 2 个 target，effect 阶段并行度仍然很差。

## ThinkCtx 设计意图

关键目标不是给逻辑足够自由，而是只暴露不会破坏并发模型的自由。

Think 应该看到的:

- 只读 world snapshot（具体 query 能力由泛型实例化时的类型提供）
- Emit: 向目标 ref 发送 signal
- Publish: 向目标 ref 发送 effect

Think 返回下次自动苏醒的 tick 间隔，只控制自己，不替别人挂定时器。跨 owner 交互统一通过 effect/signal 完成。

不应暴露:

- 直接写其他 Logic 的状态
- 直接创建新 entity 并立刻可见（应通过 world effect）
- 在 Think 内同步触发别的 Logic 执行
- 给其他 Logic 直接计划 future think

## CommitCtx 设计意图

Apply（对应 CommitCtx）应该看到的:

- 只读 world snapshot
- Emit: 向目标 ref 发送 signal（用于通知后续逻辑）

Apply 不应:

- 再次发 effect（否则 Apply 会退化成第二套脚本执行环境）
- 直接写其他 Logic 的状态

Apply 应当小而稳定，主要负责把 effect 提交到自己的公共状态。

## Effect 的顺序语义

同一 round、同一目标 Logic 收到的是一个 effect multiset。逻辑必须把这些 effect 视为无序输入。

如果某个交互天然不是无序安全的，有三种处理方式:

1. 改玩法语义，适配无序归约（优先）
2. 拆成多轮/多 tick 的显式协议（其次）
3. 进入特殊的串行域 serial island（最后手段）

### 未来可选: effect 代数元数据

当前版本中，effect 的归约逻辑完全由 Logic 的 Apply 实现者自己负责。框架不需要知道 effect 的代数性质。

未来如果需要以下能力，可以考虑为 effect 类型补充代数元数据（sum/max/min/set-add/replace-if/exclusive 等）:

- 测试时 shuffle 验证: 对同一批 effect 随机打乱顺序跑两次 Apply，检查结果是否一致，用于发现隐式顺序依赖
- 调度器预合并: 框架在 Apply 前对同类 effect 做聚合，减少 Apply 工作量

这不是当前的必需项，作为优化和检测的扩展点保留。

## Signal 的设计边界

当前模型中，signal 发送方式为 Emit(target_ref, signal)，目标是显式指定的。

这意味着:

- Routing 不需要框架理解 signal 语义: 框架只负责按 target ref 投递到 inbox
- Fan-out 由 Logic 自己负责: 如果需要通知多个关注者，Logic 在 Think 里查出关注者列表并逐个 Emit
- 优先级: 所有 signal 统一进 inbox，统一在下一轮被消费

### 未来可选: signal 语义类型与 scope

如果未来出现以下需求，可以考虑为 signal 补充语义分类（fact/notification/outcome/timer）和 routing scope（self/watchers/world/subscription-query）:

- 框架级 pub-sub: Logic 订阅某类事件，框架自动做 fan-out 分发
- 同 tick 加急投递: 某类 signal 需要比其他 signal 更早到达
- Replay/审计工具需要区分因果链

当前这些场景都可以通过 Logic 自己查订阅者 Emit、signal payload 携带 causal_id 等方式解决。作为扩展点保留。

## Ref 体系

系统统一使用 uint64 ref 标识所有 owner:

- 普通 Logic ref: 标识一个具体的 Logic 实例
- World ref: 特殊保留值，标识 world 这个特殊 owner
- Serial ref: 标识需要进入串行域处理的 owner

ref 的分区由位标记区分，具体定义见代码。

## 关于 snapshot 成本

Think 依赖冻结世界，但不要把"冻结"理解成每轮复制整个地图。

更可行的实现方向:

- World/Logic public state 使用版本化只读视图
- Round 内所有 Think 共享同一份 snapshot handle
- Apply/world effect 在 barrier 后提交新版本

冻结的是"观察语义"，不一定是"物理复制"。

## 适合这个模型的逻辑

最适合迁移的逻辑:

- 技能判定
- buff/talent 触发
- 伤害结算
- AI 决策
- 定时器驱动的状态机

不适合直接裸迁移的逻辑:

- 大量依赖即时全局副作用可见性的脚本
- 依赖深层同步回调链的老代码
- "一边改状态一边广播再被别的系统立刻读取"的隐式耦合逻辑

这些逻辑需要先改造成: 读 snapshot → 产出 intent → 在 Apply 阶段生效。

## 覆盖范围与妥协

这个模型可以覆盖绝大多数 MMORPG 服务器逻辑，但不能在保持系统简单的前提下原样覆盖所有既有语义。

### 妥协 1: 不支持跨 owner 的同 round 原子事务

系统默认只支持单 owner 提交。每个玩法规则必须有一个真相 owner，真相 owner 负责最终裁决自己的状态变化，其他 owner 的结果通过 signal/后续 round/补偿来体现。

例如:

- 技能释放、资源扣减、进入 CD，通常归 source owner 裁决
- 受击、躲避、格挡、死亡，通常归 target owner 裁决

### 妥协 2: 成功语义必须锚定在单 owner 上

像"技能成功释放才扣 1 层计数"这种规则，如果"成功"依赖 target 在 Apply 阶段的反馈，那么它本质上是跨 owner 事务。

建议优先改写为:

- consume-on-cast: 开始释放时就扣层数，命中与否不返还
- consume-on-launch: 生成投射物时扣层数，躲避/格挡只影响后续效果
- pending-reservation: 先冻结一层计数，后续 round 再确认或回滚（轻量两阶段协议）

默认不支持"跨 owner 反馈决定 source 资源是否消费"的同步语义。如必须支持，只能通过 reservation + confirm/rollback 协议实现。

### 妥协 3: 同 round 内只能看到 barrier 前的世界

Think 内读取的是 snapshot，而不是别人刚刚写进去的最新状态。没有 read-your-write-across-owner，没有同步回调链。新状态只在 barrier 后变得可见。

### 妥协 4: same tick 完成不是承诺，只是尽力而为

如果某条逻辑链需要很多轮 signal 往返，系统不承诺它一定在当前 tick 收敛。同 tick 多轮是优化，不是语义保证。一旦超出 budget，剩余工作顺延到后续 tick。

### 妥协 5: 极少数强顺序玩法进入串行域

保留一个显式的 serial island 概念，用来承载:

- 必须依赖严格顺序的剧情脚本
- 必须依赖跨多个 owner 的原子裁决
- 很难改造成 signal 协议的遗留系统

但这应该是有成本、可见、可统计的例外路径，而不是默认路径。

## LogicMeta: 声明式元数据

建议每个 Logic 实现都带一组声明式元数据，供调度器和检测工具使用。

当前建议的候选字段（具体字段需要进一步协商）:

- max_effects_per_activation: 限制单次 Think 最多扇出多少 effect
- max_signals_per_activation: 限制单次 Think 最多扇出多少 signal
- allow_same_tick_reenter: 是否允许同 tick 内被新 signal 再次激活
- priority: ready queue 内的调度提示
- cost_hint: 给调度器的粗粒度成本估计，决定更适合串行还是并行 lane
- serial_only: 明确声明该 Logic 只能进入 serial lane

这些元数据的作用:

- 调度器预算控制
- 日志和 profiling
- 启动期校验
- 线上告警

## 逻辑合同与检测

### 逻辑合同

每个 Logic 都应满足:

- Think 只能读 snapshot、改 private state、产出 typed effect/signal
- Apply 只能改自己的 public state、产出 signal
- 不能同步触发别的 Logic 执行
- 每次激活都必须是有界的

### 无限循环防护

建议三层防护:

1. 运行时 budget: max_rounds_per_tick、max_generated_tasks_per_logic_per_tick、max_signal_chain_depth
2. 状态机约束: phase 有限、某些 phase 只能单向推进、反复进入同一 phase 必须伴随计数下降或 deadline 接近
3. Tracing/报警: 记录 signal 链路、记录同 tick 内 Logic 激活次数、记录最热点的 owner/Logic 组合

Logic 必须要么终止，要么显式等待未来事件。不允许无条件的 same-tick 自激活闭环。

### 无序安全检测

如果系统接受同一 round 内 effect 任意顺序，那么就必须反过来检查玩法是否违反这个假设。

测试环境应主动对同一 Logic 的 effect 输入随机 shuffle。如果同一组输入在扰动下出现不可接受的玩法分歧，说明该逻辑实际上并不满足无序安全合同。

## 需要尽早验证的风险

### 1. 语义风险

最容易出问题的不是性能，而是设计者误以为可以随意修改共享状态。public/private state 的拆分必须从第一天就落实。

### 2. 调度风险

如果 current tick 允许无限追加 next-round think，很容易出现某些 tick 极端膨胀。

### 3. 热点风险

如果大量 effect 都集中到少数 boss/tower/objective 上，并行度会在 Apply 阶段塌缩到热点 owner。

### 4. 快照风险

空间查询、可见性、附近目标列表这类索引如果没有版本化语义，Think 看到的世界会不一致。

## 建议的最小落地版本

不要一开始就把整个服务器迁过去，建议先拿一个最小闭环验证:

1. 单地图、单场景
2. 只迁移 combat path
3. 只支持: DamageEffect、AddBuffEffect、RemoveBuffEffect、ScheduleThink、EmitSignal
4. World effect 先只支持 SpawnNpcEffect
5. 加上 tracing、budget 统计和无序执行压力测试

如果这个最小闭环能跑稳，再扩展移动、AI、召唤、投射物。

## 当前结论

核心工程选择:

1. Logic = Owner，内部组合是实现私有事务
2. Typed effect，不是 closure
3. World 是特殊 owner，不是模型外的例外
4. 监听/触发改成 signal routing，不是同步回调
5. 从第一天就定义 tick budget
6. Effect 代数元数据和 signal 语义分类作为未来优化/检测的扩展点保留
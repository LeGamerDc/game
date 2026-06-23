# 纯串行调度器（serial package）设计稿

> 定位：`sched/` 之外的**仅支持串行**的轻量调度抽象，目标是把"开发/调试复杂度"压到最低。
> 代码权威：`serial/scheduler.go`、`serial/scheduler_test.go`。本文与代码冲突时以代码为准。

## 1. 为什么再做一个串行版

`sched/` 是支持并发的 BSP 调度器，对 Logic 能力和世界做了完整建模；它内置的 `scheduler_serial.go`
只是"并发框架下单位较少时省掉并发开销"的快路径，**承担的复杂度和并行版一致**（Apply/Commit/Stage、
block 分片、signal 双缓冲、递归 cascade、depth 预算……）。

大多数玩法的复杂度 × 单位数并不需要并行。这时我们想要一个**为理解而生**的纯串行抽象：
Logic 的 `Think` 里可以直接读 world / 其他单位，不再需要理解 Effect/Apply/Stage。

## 2. 核心模型

三个概念，单线程语义：

```text
Unit   每个单位只有一个入口 Think(ctx, events) -> delay
World  调用方注入的注册表(ref->unit) + 业务查询；调度器不持有单位存储
Event  跨单位交互的唯一抽象：一条 typed 消息，投到目标，由目标在自己的 Think 里处理
```

`Think` 内可以：

- 直接读 `ctx.World` 与其他单位（同步、无竞争）；
- 改**自己**的状态；
- `ctx.Post(ref, event)` / `ctx.Poke(ref)`：把事件/纯唤醒交给目标（由目标自己处理）；
- 返回自身下一次唤醒的相对 tick 数（`delay <= 0` 表示休眠）。

`Think` **不可以**直接改别的单位的状态——跨单位修改一律表达成 Event，由 owner 的 Think 落地。
这样每个单位都是自己状态和"下次唤醒时间"的唯一权威。

相比 `sched` 的塌缩：

| sched（并行/两相） | serial（串行/单相） |
|---|---|
| `Publish(effect)` + `Apply` | 直接改自己 / `Post(event)` 交给 owner |
| `Emit(signal)` 激活 Think | `Post`/`Poke` 同 tick 唤醒目标 |
| `WriteStage`/`PromoteStages` 双缓冲视图 | 无；Think 直读 live 状态 |
| Effect / Signal 两种消息 | 单一 Event |
| superstep + depth 预算 | superstep 波次 + maxSteps 上限 |

## 3. 不变式（理解与调试的锚点）

> **任何单位的状态只在它自己 Think 时改变；任何观察都看到各单位"上一次 Think"的结果。**

由此：

- 跨单位写一律走 Event → 没有 Apply 相、没有 owner 之外的写者。
- Think 直读 live 状态 → 没有 Stage/Promote。
- 一波（superstep）内单位逐个执行；事件**双缓冲**：第 S 波 Post 的事件第 S+1 波才可见
  （处理前先快照各单位 inbox，处理中产生的 Post 进下一波的 inbox）。
- **不丢事件**：每次 `Post`/`Emit` 都同时把目标排进消费它的那一波；inbox 非空 ⟹ 该 ref 必被调度。
  超过 maxSteps 仍未消化的，连同 inbox 一起溢出到下个 tick（不是丢弃）。

注意一个有意为之的语义代价：波内逐个执行 ⇒ 后处理的单位看到先处理单位"本波已改完的自身状态"，
**不是并行 BSP 那种波初快照一致**。这是串行的顺序观察，确定但与并行不逐位相同——与 sched
"顺序无关是容忍性"的设计哲学一致，串行只是又一种被容忍的合法顺序。

## 4. 计时：一个单位一个定时器

每个单位在最小堆（`lib.HeapIndexMap`，按 ref 索引、按 deadline 排序）里**最多一个条目**。

- `Think` 的返回值是该单位**权威的**下次 deadline（不管这次是被 timer 还是 event 唤醒，
  都要返回它内部多个 deadline 的 min）：`delay > 0` 覆盖式写堆；`delay <= 0` **取消**已有堆条目，
  使单位真正休眠（若被 event 提前唤醒后想睡，旧 timer 必须被清掉，否则会被错误唤醒）。
- `Schedule(ref, delay)`（外部 bootstrap）是 **min-merge**：只会把唤醒提前，不会推后。
- 这样"其他单位频繁访问触发 nextThink 改变"导致的 churn 不存在：始终一条目、O(log n) 更新、无 stale。

`delay > 0` 只能安排**严格未来**的 tick；同 tick 内的再思考只能通过 `Post`/`Poke`（走波次），不走 timer。

## 5. 一个 tick 的处理

```text
Tick(now):
  1) seed 初始 frontier：
       - 从堆 pop 出所有 deadline <= now 的单位
       - 加入 pending（外部 Emit 输入 + 上个 tick 溢出 carry）
       去重后作为第 0 波
  2) superstep 级联，最多 maxSteps 波：
       sort(frontier)                      # 按 ref 升序，确定性
       snapshot 各 ref 的 inbox -> batch    # 双缓冲：本波只看快照
       for ref in frontier:
           delay = unit.Think(ctx, batch[ref])
           if delay > 0: heap.upsert(ref, now+delay)   # 安排未来 timer
           # 本波 Post 进 next 波
       frontier = next
  3) 溢出：>maxSteps 仍在队列里的，markPending 留到下个 tick（inbox 保留），Overflow() 计数
  now++
```

- 同 tick 内的跨单位反应通过波次实现（最多 maxSteps 跳）。
- 溢出单位下个 tick 进 **第 0 波**（最先处理）——反饥饿，不是推到队尾。
- `Overflow()` 持续增长 ⇒ 存在跑飞的反馈环（如 A↔B 每波互相 Post），是可观测指标而非 hang。

## 6. API 一览

```go
type Unit[W, E any] interface {
    ID() uint64
    Think(ctx *Ctx[W, E], events []E) (delay int64)
}

type Ctx[W, E any] struct {
    World W
    Now   int64
    // Post(ref, ev) 投事件；Poke(ref) 无 payload 唤醒；都是同 tick 下一波处理
}

func NewScheduler[W, E any, U Unit[W,E]](world W, lookup func(uint64)(U,bool), maxSteps int) *Scheduler
func (*Scheduler) Tick()
func (*Scheduler) Now() int64
func (*Scheduler) Schedule(ref, delay int64)   // bootstrap，min-merge
func (*Scheduler) Emit(ref, ev)                // tick 外注入外部输入
func (*Scheduler) Remove(ref)                  // 完整反调度：清 timer + inbox + pending/next 排队激活
func (*Scheduler) Overflow() int               // 诊断：被推迟到后续 tick 的累计次数
```

注册表由调用方（World）注入：`lookup` 通常就是 `world.GetUnit`。spawn/despawn 与权威单位表完全在
调用方，调度器只持有堆 + inbox + 波次缓冲。

## 7. 与典型工程的关系（调研结论）

本设计不是凭空发明，而是几个成熟模式的串行组合：

- **Serial Pregel / BSP**：消息从 superstep S 在 S+1 跨 barrier 投递；顶点 vote-to-halt、被消息重新激活。
  ——对应我们的 heap-deadline 休眠 + event 唤醒，以及 S→S+1 双缓冲。Pregel 系统本身就提供
  *单机串行 runner* 用于测试/仿真，所以"串行 BSP + 消息传递"是被承认的构造；我们的增量是
  **per-tick 的 superstep 上限 + 溢出到下个 tick**（经典 Pregel 跑到不动点、不设上限）。
- **Bevy ECS `Events<T>`**：双缓冲事件、按帧 swap。我们借其"读前快照、跨波可见"的双缓冲思路，
  但**规避了它的已知坑**——Bevy 在 buffer 轮换时会静默丢弃未读事件；我们靠"Post 必同时调度目标 +
  溢出保留 inbox"保证不丢。
- **Actor message-cycle**：消息投到目标、由目标顺序处理自己的 mailbox、owner 独占状态；用显式排序
  得到确定序。我们的"按 ref 升序处理 frontier + owner 在自己 Think 里处理事件"即此。

参考：Game Programming Patterns《Event Queue》《Double Buffer》；Pregel/BSP；Bevy Cheat Book — Events；
Quake `nextthink` / Source `OnTakeDamage`（事件投到目标、owner 处理）。

## 8. 常见误用检查表

- [ ] 是否在 Think 里直接改了别的单位的状态？（应改成 `Post(event)`）
- [ ] `Think` 是否每次都返回**当前真实**的最近 deadline？（被 event 唤醒时也要返回，否则会丢自己的 timer）
- [ ] 是否想要"同 tick 立即反应"却用了 timer 返回值？（同 tick 反应只能靠 `Post`/`Poke`）
- [ ] 是否依赖"攻击者当场知道目标死没死"这类即时反馈？（本模型是 fire-and-forget，回执要建模成另一条 Event）
- [ ] despawn 是否同时做了 `Remove(ref)` + 从注册表删除？
- [ ] `Overflow()` 是否在持续增长？（排查 A↔B 反馈环）

## 9. 未决 / 后续

- 事件 slice 目前每波丢弃后由 GC 回收；单位量大时可加 free-list 池化（当前刻意保持简单）。
- 是否需要 per-unit 的"连续溢出 N 次"饥饿告警计数（目前只有全局 `Overflow()`）。
- 是否提供 batch 版 `Tick(n)` 或固定步长驱动 helper。
- 与 demo / 上层 framework 的接入手册（类似 `sched/integration.md`）待补。

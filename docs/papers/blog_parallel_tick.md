# No Locks, No Transactions: Parallelizing an MMO Server Tick with Structural Ownership

## The Thousand-Player Problem

Picture a castle siege in your favorite MMO. Eight hundred players converge on a single zone — warriors clash at the gate, mages rain fire from the walls, healers scramble to keep raid groups alive. On the client, your GPU handles this beautifully across dozens of cores. On the server? All that game logic — every damage calculation, every buff tick, every cooldown check — runs on a single thread.

This is the dirty secret of MMO server architecture. Twenty years of engine-level parallelism have given us multi-threaded rendering, physics, and animation. But the game logic tick loop — the thing that actually *decides what happens* — remains stubbornly single-threaded in virtually every shipped title. Overwatch's ECS runs gameplay on one thread. So does Destiny's. So does yours, probably.

The industry's answer has been **scale out**: split the world into spatial regions, run each on a separate process, stitch them together with networking. SpatialOS pioneered this approach. SpacetimeDB (presented at GDC 2025) treats the whole backend as a database with ACID transactions. These are legitimate solutions, but they come with real costs — cross-region latency spikes when players walk across boundaries, complex authority handoff protocols, and fundamental throughput limits when hundreds of entities crowd into the same spatial cell.

Meanwhile, your 16-core server sits at 6% CPU utilization during that castle siege, because 15 cores have nothing to do.

We asked a different question: **what if we scaled up instead?** What if we could use all those idle cores to parallelize the tick loop itself — not the rendering, not the physics, but the actual game logic?

The academic community tried this in the mid-2000s. They hit a wall. Lock-based parallelization of Quake showed 35% lock overhead. Transactional memory approaches suffered high abort rates. By 2012, the research line went dormant.

We think we've found a way through. No locks, no transactions, no rollbacks. The key insight is embarrassingly simple: **don't detect conflicts at runtime — make them structurally impossible.**

<!-- [PICTURE: A simple "before/after" comparison diagram.
Left side: "Traditional" — single tall bar labeled "1 core: all game logic", with 15 grey bars labeled "idle".
Right side: "Ours" — multiple shorter bars across cores, all active, labeled "Think" and "Apply" phases.
Caption: "Same tick, same logic, all cores working."] -->

## The Design: Ownership Makes Parallelism Free

The entire design rests on one rule: **every piece of mutable game state has exactly one owner, and only the owner can write to it.**

A player character owns its HP, position, mana, and cooldowns. An NPC owns its AI state and threat table. A fireball projectile owns its flight path and remaining lifetime. We call each of these owning entities a **Logic** — the fundamental unit of parallelism.

This rule alone eliminates write conflicts. If two threads are running two different Logics, they are *by construction* writing to disjoint memory. No locks needed. No compare-and-swap. No contention.

But if everyone can only write to their own state, how does a fireball deal damage to a player? This is where the two-phase execution model comes in.

### Think and Apply

Each tick, every active Logic goes through two phases:

**Think** — the Logic reads a frozen snapshot of the world and decides what it *wants* to do. A fireball checks its trajectory against the spatial index and decides it wants to deal 50 fire damage to Player B. But it doesn't touch Player B's HP. Instead, it produces a typed **Effect** — an explicit data packet: `DamageEffect{target: PlayerB, element: Fire, rawDamage: 50, penetration: 12}`.

**Apply** — each Logic receives all the Effects that targeted it and commits them to its own state. Player B receives the `DamageEffect`, checks its own fire resistance, applies armor reduction, and updates its own HP. Player B is the truth owner of its own HP — only it gets to decide the final number.

<!-- [PICTURE: A two-phase diagram showing the Think/Apply split:

Top row (Think phase):  Three boxes "Mage A", "Fireball", "Warrior B" each reading from a shared "World Snapshot" (arrows pointing down from snapshot to each box). Fireball produces a "DamageEffect → B" arrow pointing to a collection area.

Bottom row (Apply phase): "Warrior B" box receives the DamageEffect, applies its own armor/resistance, updates its HP. The HP change is shown as writing to "B's Public State".

A barrier line separates Think and Apply.
Caption: "Think reads and produces intents. Apply owns the truth and commits state."] -->

This is essentially a **computation decomposition constraint**: any formula that depends on both the attacker's and the defender's state must be split into two functions. The attacker's Think computes `f_source(myStats) → payload`, and the defender's Apply computes `f_target(payload, myDefenses) → finalResult`. The Effect data is the bridge.

```game/docs/papers/blog_parallel_tick.md#L1-5
// During Think — the mage computes what it can
effect := DamageEffect{
    RawDamage:   baseDmg * (1 + spellPower*0.4),
    Penetration: mage.ArcanePen,
    Element:     Fire,
}
```

```game/docs/papers/blog_parallel_tick.md#L6-11
// During Apply — the target decides what actually happens
func (p *Player) Apply(ctx, effects) {
    for _, e := range effects {
        finalDmg := e.RawDamage * (1 - p.Resist(e.Element)) * (1 - max(0, p.Armor-e.Penetration)*0.01)
        p.HP -= finalDmg
    }
}
```

### Signals and Supersteps

Effects modify state. But sometimes a Logic needs to *notify* another Logic without modifying its state — "hey, I just died" or "the buff you gave me expired." That's what **Signals** are for. Signals are typed data packets routed to a target Logic's inbox, consumed in the *next* round of Think.

A single tick can contain multiple rounds of Think→Apply→Signal propagation. We call each round a **Superstep**, borrowing from Valiant's Bulk Synchronous Parallel model. The loop repeats until no new work is produced, or we hit a budget cap (default: 3 supersteps). Any leftover signals spill into the next tick — the system is self-regulating rather than unbounded.

<!-- [PICTURE: A horizontal timeline showing one tick containing multiple supersteps:

Tick N:
  [Superstep 0: Think → Barrier → Apply → Barrier]
  [Superstep 1: Think → Barrier → Apply → Barrier]
  [Superstep 2: Think → Barrier → Apply → Barrier]  ← budget exhausted
  [Timer Merge] [Advance]
Tick N+1:
  [Inject overflow signals + external input]
  [Superstep 0: ...]

Caption: "Multiple Think/Apply rounds per tick. Overflow is gracefully deferred."] -->

### Why Typed Data, Not Closures

Every Effect and Signal is an explicit, typed struct — never a closure or callback. This is a deliberate constraint:

- **Debuggable**: you can log every Effect that flew between entities this tick
- **Replayable**: record Effects to reproduce exact server state
- **Network-friendly**: Effects can be serialized and forwarded to clients for prediction reconciliation
- **Aggregatable**: the Apply phase can sort, filter, and batch-process Effects by kind (e.g., process all immunity effects before damage)

## Seeing It in Action

Let's walk through a few scenarios to see how real game logic maps to this model. These are deliberately simplified — the point is to show the *patterns*, not to spec a shipping game.

### Scenario 1: The Fireball (Owner Closure)

A mage casts Fireball. The mage's Think checks cooldown and mana (both private state), deducts the cost, starts the cooldown timer, and spawns a Fireball Logic. The Fireball is now its own owner — its Think handles flight each tick, its Apply handles being interrupted (e.g., by a wind effect). On collision, it publishes a `DamageEffect` to the target.

Everything the mage does is **self-contained** — cooldowns, mana, cast animation state are all private. The fireball is a new independent entity. The damage is a one-way intent: fire and forget. This covers roughly **53% of real game abilities** in our analysis.

### Scenario 2: The War Horn (Fan-Out Broadcast)

A commander activates War Horn: +20% damage to all allies within 30 meters. The commander's Think queries the spatial index (read-only), finds 12 allied entities in range, and publishes a `WarHornBuffEffect` to each of them. Twelve Apply calls run in parallel — each ally adds the buff to its own buff table, recalculates its damage modifier, done.

This is a **fan-out broadcast** — one Think, N independent Applys. The 12 allies don't interact with each other, so their Applys are embarrassingly parallel.

### Scenario 3: The Spell Parry (Request-Response)

A knight has an active "Spell Parry" buff. A sorcerer's chain lightning targets the knight. Here's where it gets interesting:

- **Superstep 0, Think**: Sorcerer publishes `LightningEffect → Knight`
- **Superstep 0, Apply**: Knight receives the effect. Its Apply checks the parry buff, consumes it, *negates* the damage, and emits a `ParriedSignal → Sorcerer`
- **Superstep 1, Think**: Sorcerer receives the `ParriedSignal`. Its Think publishes a `ReflectedDamageEffect → Sorcerer` (self-targeted)
- **Superstep 1, Apply**: Sorcerer's Apply deals the reflected damage to itself

This took **2 supersteps** — about 1ms of wall time. The player sees "PARRIED!" with no perceptible delay. But notice what happened: the knight decided whether the parry succeeded (it owns its buff state), and the sorcerer decided how much reflected damage it took (it owns its HP). Each owner controlled its own truth.

### Scenario 4: The Death Pact (The Hard Case)

Two warlocks form a Death Pact: if either dies, both die. Warlock A takes lethal damage.

- **Superstep 0, Apply**: Warlock A's Apply processes the damage, HP drops to 0. It emits `DeathPactSignal → Warlock B`
- **Superstep 1, Think**: Warlock B receives the signal. Its Think publishes `LethalEffect → self`
- **Superstep 1, Apply**: Warlock B kills itself

There is a **1-superstep window** where Warlock A is dead but Warlock B is still alive. At 30Hz tick rate with 3 supersteps per tick, this window is roughly 1ms — completely imperceptible to any player. But it exists. And acknowledging it honestly leads us to the next section.

**So... what's the catch?**

## The Price You Pay

This design doesn't come free. There are real guarantees you give up in exchange for parallelism. If you're evaluating whether this model fits your game, these are the constraints you need to understand.

### 1. No Cross-Owner Atomic Transactions

You cannot atomically check Player A's inventory and deduct Player B's gold in the same superstep. Every gameplay rule must have a **single truth owner**. In practice, this means adopting a "consume-on-cast" pattern: the caster deducts mana when it *starts* casting, not when the spell *hits*. If the target dodges, the mana is still gone. This matches what most shipped games already do — it's the rare game that refunds resources on miss.

### 2. Think Sees a Snapshot, Not Live State

During Think, you read a frozen snapshot of the world. If Player B just died in someone else's Apply, your Think doesn't know yet — you'll see B's pre-death state. This sounds scary, but in our analysis of 107 real game logic chains across League of Legends, DOTA 2, and World of Warcraft ability mechanics, **95%+ of cross-entity reads tolerate 1-superstep staleness** with no gameplay impact. The remaining cases are handled by moving the decision to the target's Apply, where it has the freshest state.

### 3. Effect Order Is Not Guaranteed

If 10 damage effects arrive at the same target in the same superstep, the order they're processed is not deterministic. For additive effects (damage, healing, resource changes), this is mathematically irrelevant — addition is commutative. Over **80% of game effects are naturally order-independent**. For the rest (e.g., "apply immunity before processing damage"), the Apply function scans effects by category: process immunity effects first, then damage. This is a simple two-phase scan, not a complex ordering system.

### 4. You Must Rethink Cross-Entity Interactions

The biggest adaptation cost isn't technical — it's mental. You can no longer write `target.TakeDamage(50)` as a direct function call. Instead, you publish a `DamageEffect` and let the target handle it. This is a paradigm shift from imperative "I do things to you" to declarative "I declare my intent, you decide the outcome."

In our adaptation study, we classified 107 logic chains from real games and production business logic into a formal taxonomy. The results:

| Category | Description | Frequency |
|---|---|---|
| **Owner Closure** | All reads/writes within one entity | ~53% |
| **Fire-and-Forget** | One-way Effect, no response needed | ~33% |
| **Request-Response** | Effect + Signal round-trip (2-3 supersteps) | ~13% |
| **Global Serial** | Must run single-threaded (entity registry, etc.) | ~0% of combat |

Zero out of 107 chains were unadaptable. The framework provides an escape hatch — an explicit serial mode for logic that truly can't be parallelized — but in practice, combat logic never needs it.

### How Do I Know If My Logic Fits?

We built a formal decision tree for this. When you're staring at a piece of game logic and wondering "can this run in parallel?", walk through these steps:

```
Step 1: Identify the Owner
  "Who has final authority over this state change?"
  → Single owner found → continue
  → Multiple owners must agree atomically → Reservation Protocol (rare) or Serial

Step 2: Check State Boundaries
  "Does this logic only read/write its own state?"
  → Yes → Owner Closure. Direct fit, zero adaptation. ✅
  → No  → continue

Step 3: Classify the Cross-Owner Write
  "How does it modify someone else's state?"
  → Fire-and-forget, no response needed      → B1: Unidirectional Intent
  → Sends effect, waits for a response signal → B2: Request-Response (2-3 supersteps)
  → Needs multi-party atomic success          → B3: Reservation (try to downgrade to B1)
  → Broadcasts to many targets                → B4: Fan-out Broadcast

Step 4: Assess Read Freshness
  "When reading another entity's state, how much staleness is okay?"
  → Static data / tolerates 1 tick delay → C-0/C-1: no action needed
  → Needs latest value for adjudication  → C-2: move the decision to the data owner's Apply
  → Must be visible same-tick            → C-3: consider serial (extremely rare)

Step 5: Check Effect Order Safety
  "If multiple effects of the same kind arrive, does processing order matter?"
  → No (additive, idempotent)                   → D-0: naturally commutative
  → Yes, but can collect-then-batch              → D-1: batch accumulation
  → Yes, but can sort by category/priority       → D-2: two-phase scan

Step 6: Evaluate Cascade Depth
  "How many Effect/Signal hops does this chain need?"
  → ≤ 1 hop   → E-0: single superstep
  → 2-3 hops  → E-1: converges within MaxSupersteps budget
  → > 3, bounded → E-2: may spill across ticks, usually acceptable
  → Circular   → E-3: design a cutoff in Logic (theoretical — never seen in practice)

Step 7: Global Serialization Check
  "Does this modify an indivisible global data structure?"
  → Yes → Category F: run in World's Apply (serial island)
  → No  → classification complete ✅
```

<!-- [PICTURE: A flowchart version of the 7-step decision tree above, with color-coded terminal nodes:
- Green: Owner Closure (direct fit)
- Blue: B1/B4 (light adaptation)
- Yellow: B2/C-2/D-1/D-2 (moderate adaptation)
- Orange: B3/E-2 (heavy adaptation, rare)
- Red: F (serial island, non-combat only)
Caption: "The adaptation decision tree. Green and blue cover ~86% of real game logic."] -->

We also catalogued a toolkit of **named adaptation patterns** for the common cases:

| Pattern | Applies To | Cost | One-Liner |
|---|---|---|---|
| **Effect-ify** | B1, B4 | Low | Replace direct call with `Publish(target, TypedEffect)` |
| **Move Adjudication** | C-2 | Medium | Shift the decision from source's Think to target's Apply |
| **Consume-on-Cast** | B3→B1 | Low | Deduct resources at cast time, never wait for hit confirmation |
| **Batch Accumulate** | D-1 | Medium | Collect all Effects first, process as a batch |
| **Two-Phase Scan** | D-2 | Medium | Process immunity/shield effects before damage effects |
| **Signal Broadcast** | B4 | Low | Apply emits Signals to fan out notifications |
| **Timer Replace Poll** | A | Low | Replace global scans with per-owner self-registered timers |
| **Attacker Snapshot + Defender Adjudication** | C-2 + D-1 | High | Effect carries source's pre-computed payload; target Apply uses own latest state |

When we ran 107 real logic chains (30 classic game skills from three major titles + 77 production business logic paths) through this decision tree, here's what came out:

| Category | Game Skills (30) | Production Logic (77) |
|---|---|---|
| **A — Owner Closure** | 53% | 38% |
| **B1 — Fire-and-Forget** | 33% | 32% |
| **B2 — Request-Response** | 13% | 13% |
| **B3 — Reservation** | 0% | 5% |
| **F — Global Serial** | 0% of combat | 16% (infra only) |
| **Unadaptable** | **0%** | **0%** |

You give up synchronous cross-entity certainty. **In return, you get every core on your server working for you.**

## Under the Hood: Engineering for Zero Contention

The design principles above could be implemented many ways. Here's how we built it to ensure the hot path — the inner loop of Think and Apply workers — contains **no synchronization primitives whatsoever**.

### Block-Based, Thread-Local Everything

Every entity ID is hashed to a fixed **block** (we use 137 blocks — a prime, for even distribution). Each worker thread owns a private **block collector**: a simple `[][]Effect` indexed by block ID.

```game/sched/block_collector.go#L11-22
type blockCollector[V any] struct {
    _      cpu.CacheLinePad
    blocks [][]V
}

func (c *blockCollector[V]) push(blockId int, v V) {
    c.blocks[blockId] = append(c.blocks[blockId], v)
}
```

When a Think closure calls `Publish(targetRef, effect)`, the effect lands in `effectCollectors[myThreadId]`. Thread 0 writes to collector 0, thread 1 to collector 1 — never the same memory. The `CacheLinePad` field at the struct head prevents **false sharing**: adjacent threads' collectors won't share a CPU cache line, even on ARM processors with 128-byte lines.

The result: during the entire Think phase, there is not a single lock, atomic operation, or shared write. Every thread is scribbling into its own private scratch space.

### Sort, Don't Map

When the Apply phase begins, we need to group effects by target entity. The naive approach is `map[uint64][]Effect` — but maps mean heap allocation, poor cache locality, and GC pressure.

Instead, we flatten all effects for a block into a contiguous buffer, **sort by target ref**, and linearly scan for group boundaries:

```game/sched/scheduler_parallel.go#L82-95
slices.SortFunc(flatBuf, func(a, b refVal[E]) int {
    return cmp.Compare(a.ref, b.ref)
})
for start := 0; start < len(flatBuf); {
    ref := flatBuf[start].ref
    end := start + 1
    for end < len(flatBuf) && flatBuf[end].ref == ref {
        end++
    }
    logic.Apply(ctx, flatBuf[start:end]) // zero-copy sub-slice
    start = end
}
```

This is a textbook sort-group-process pattern. The flat buffer is reused across blocks within the same tick — truncated to `[:0]` per block, preserving grown capacity. Zero allocation on the hot path.

<!-- [PICTURE: A visual comparison of two approaches side by side.
Left: "Map-based grouping" — scattered boxes with pointers, labeled "heap alloc per key, pointer chasing, GC pressure"
Right: "Sort-based grouping" — one contiguous array, sorted by color/ref, with bracket annotations showing groups. Labeled "cache-friendly, zero-alloc, linear scan"
Caption: "Sort-based grouping turns pointer-chasing into a linear memory scan."] -->

### LPT Load Balancing for Apply

Think blocks use a static mapping (`blockId % numThreads`) because timer state needs thread affinity. But Apply blocks are assigned **dynamically each superstep** using LPT (Longest Processing Time first): count effects per block, sort descending, greedily assign each block to the least-loaded thread. This is the classic multiprocessor scheduling approximation — simple, no allocation, and it prevents stragglers from uneven effect distribution.

### Automatic Serial Fallback

Not every superstep needs parallelism. If the work count drops below a threshold (default: 500 active entities), the scheduler switches to **truly inline serial mode**: Think directly invokes Apply via recursive closures, Apply directly triggers the next Think — a single-threaded DFS with zero goroutine overhead, zero buffer management, zero barriers.

```game/sched/scheduler.go#L252-283
if workCount >= sc.meta.ThinkConcurrencyThreshold {
    // Parallel: goroutines + barriers + block sharding
    sc.parallelThink(world, firstSuperstep)
    sc.commitWatches(world)
    sc.computeApplyAssignment()
    sc.parallelApply(world)
    sc.swapSignalBuffers()
} else {
    // Serial: recursive closures, inline execution
    sc.serialProcess(world, firstSuperstep, maxDepth)
    sc.swapSignalBuffers()
    break // Serial is terminal within a tick
}
```

The switch is automatic and per-superstep. A tick might start parallel (800 entities active in round 0), then cascade down to serial (40 entities in round 2). You don't choose a mode — the system picks the right one based on actual workload.

## What We Don't Have Yet

I want to be upfront about what's missing.

We don't have production benchmarks yet — no "X times faster" graph to show you. The scheduler passes 35 tests covering parallel/serial switching, superstep convergence, timer wheel behavior, and edge cases. But real performance numbers on real game logic are still ahead of us.

We also don't have a shipped game running on this. The adaptation study (107 logic chains, 0% unadaptable) gives us confidence the model covers real gameplay, but confidence is not proof.

What we *do* have is a clean implementation in Go with a well-defined interface, a formal adaptation methodology, and an honest accounting of the tradeoffs. The code is open source.

## Takeaways

If you're building an MMO server and you've hit the single-threaded tick ceiling, here's what we learned:

1. **Ownership is the key, not clever synchronization.** If you can answer "who owns this data?" for every piece of mutable state, parallelism falls out naturally. No locks needed.

2. **Split computation, not data.** The Think/Apply split isn't about moving data around — it's about decomposing *formulas* so that each side only writes to memory it owns.

3. **Typed effects over direct mutation.** Replacing `target.TakeDamage(50)` with `Publish(target, DamageEffect{50})` feels bureaucratic at first. It pays for itself in debuggability, replayability, and — oh yeah — parallelism.

4. **Perfection is the enemy of parallelism.** Accept 1-superstep staleness. Accept non-deterministic effect ordering. Accept that the Death Pact has a 1ms window. These "imperfections" are invisible to players and they're what makes the whole thing work.

5. **Let the system choose.** Don't force developers to pick "parallel or serial." Measure the workload, switch automatically, degrade gracefully.

The game industry has spent two decades parallelizing everything *around* game logic. We think it's time to parallelize the logic itself.
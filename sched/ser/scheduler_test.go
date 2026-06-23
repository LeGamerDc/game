package ser

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ────────────────────────────────────────────────────────────────────────────
// Test fixtures
// ────────────────────────────────────────────────────────────────────────────

type evKind int

const (
	evDamage evKind = iota
	evPing
)

type event struct {
	kind evKind
	from uint64
	val  int
}

// world is a minimal injected registry + shared scratch for assertions.
type world struct {
	units map[uint64]*unit
	order []uint64 // global processing order, for determinism tests
}

func newWorld() *world { return &world{units: map[uint64]*unit{}} }

func (w *world) get(ref uint64) (*unit, bool) {
	u, ok := w.units[ref]
	return u, ok
}

func (w *world) add(u *unit) *unit {
	w.units[u.id] = u
	return u
}

// unit is a programmable test logic. behave fully controls Think when set;
// otherwise the unit simply records the call and self-reschedules by period.
type unit struct {
	id      uint64
	period  int64 // self-reschedule delay; <=0 means one-shot
	hp      int
	lastNow int64

	thinkTicks  []int64
	thinkEvents [][]event

	behave func(u *unit, ctx *Ctx[*world, event], events []event) int64
}

func (u *unit) ID() uint64 { return u.id }

func (u *unit) Think(ctx *Ctx[*world, event], events []event) int64 {
	u.thinkTicks = append(u.thinkTicks, ctx.Now)
	cp := append([]event(nil), events...)
	u.thinkEvents = append(u.thinkEvents, cp)
	ctx.World.order = append(ctx.World.order, u.id)
	if u.behave != nil {
		return u.behave(u, ctx, events)
	}
	return u.period
}

func newSched(w *world, maxSteps int) *Scheduler[*world, event, *unit] {
	return NewScheduler[*world, event, *unit](w, w.get, maxSteps)
}

// ────────────────────────────────────────────────────────────────────────────
// Timer scheduling
// ────────────────────────────────────────────────────────────────────────────

func TestPeriodicTimer(t *testing.T) {
	w := newWorld()
	a := w.add(&unit{id: 1, period: 3})
	sc := newSched(w, 0)
	sc.Schedule(a.id, 0)

	for range 10 {
		sc.Tick()
	}

	assert.Equal(t, []int64{0, 3, 6, 9}, a.thinkTicks)
	assert.EqualValues(t, 10, sc.Now())
}

func TestOneShotSleeps(t *testing.T) {
	w := newWorld()
	a := w.add(&unit{id: 1, period: 0}) // returns 0 -> sleep after first think
	sc := newSched(w, 0)
	sc.Schedule(a.id, 0)

	for range 5 {
		sc.Tick()
	}

	assert.Equal(t, []int64{0}, a.thinkTicks)
}

func TestScheduleDelay(t *testing.T) {
	w := newWorld()
	a := w.add(&unit{id: 1, period: 0})
	sc := newSched(w, 0)
	sc.Schedule(a.id, 4)

	for range 6 {
		sc.Tick()
	}

	assert.Equal(t, []int64{4}, a.thinkTicks)
}

// Schedule is min-merge: only the earliest pending wakeup survives, and there is
// always exactly one heap entry per unit (no churn, no stale entries).
func TestScheduleMinMerge(t *testing.T) {
	w := newWorld()
	a := w.add(&unit{id: 1, period: 0})
	sc := newSched(w, 0)

	sc.Schedule(a.id, 5)
	sc.Schedule(a.id, 2) // earlier -> wins
	sc.Schedule(a.id, 8) // later -> ignored
	require.EqualValues(t, 1, sc.heap.Size())

	for range 6 {
		sc.Tick()
	}

	assert.Equal(t, []int64{2}, a.thinkTicks)
}

// A unit that maintains an absolute cadence must keep its timer even when an
// event wakes it off-schedule. This documents the "Think returns the true next
// deadline every call" contract.
func TestEventWakeDoesNotCorruptCadence(t *testing.T) {
	w := newWorld()
	var nextCadence int64 = 4
	a := w.add(&unit{id: 1})
	a.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
		if ctx.Now >= nextCadence {
			nextCadence += 4
		}
		return nextCadence - ctx.Now
	}
	b := w.add(&unit{id: 2})
	b.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
		if ctx.Now == 2 {
			ctx.Post(1, event{kind: evPing}) // wake A off-cadence at tick 2
		}
		return 0
	}

	sc := newSched(w, 0)
	sc.Schedule(a.id, 4)
	sc.Schedule(b.id, 2)

	for range 12 {
		sc.Tick()
	}

	// A fires on cadence at 4, 8; the off-cadence wake at tick 2 does not shift
	// the cadence forward.
	assert.Equal(t, []int64{2, 4, 8}, a.thinkTicks)
}

// An event-driven wake that returns "sleep" must CANCEL a previously scheduled
// future timer — the return value is authoritative.
func TestEventWakeCanCancelFutureTimer(t *testing.T) {
	w := newWorld()
	a := w.add(&unit{id: 1})
	a.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
		if len(events) > 0 {
			return 0 // event woke us and we decide to sleep for good
		}
		return 10 // first (timer) think arms a far timer
	}
	b := w.add(&unit{id: 2})
	b.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
		if ctx.Now == 2 {
			ctx.Post(1, event{kind: evPing})
		}
		return 0
	}
	sc := newSched(w, 0)
	sc.Schedule(1, 0) // A thinks at tick 0, arms timer at 10
	sc.Schedule(2, 2) // B pokes A at tick 2; A sleeps

	for range 15 {
		sc.Tick()
	}

	// A thinks at tick 0 (timer) and tick 2 (event), then never again — the
	// stale timer at tick 10 was cancelled.
	assert.Equal(t, []int64{0, 2}, a.thinkTicks)
	assert.Zero(t, sc.heap.Size())
}

// Same cancellation must work within a single tick: a self-post re-think that
// returns "sleep" cancels the timer the first wave armed.
func TestSelfPostCanCancelTimerWithinSameTick(t *testing.T) {
	w := newWorld()
	a := w.add(&unit{id: 1})
	a.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
		if len(u.thinkTicks) == 1 {
			ctx.Post(1, event{kind: evPing}) // self-post -> wave 1
			return 10                        // arm a timer the second wave will cancel
		}
		return 0
	}
	sc := newSched(w, 0)
	sc.Schedule(1, 0)

	sc.Tick() // wave 0 arms 10, wave 1 cancels
	assert.Equal(t, []int64{0, 0}, a.thinkTicks)
	assert.Zero(t, sc.heap.Size())

	for range 15 {
		sc.Tick()
	}
	assert.Equal(t, []int64{0, 0}, a.thinkTicks) // never woken by the stale timer
}

// ────────────────────────────────────────────────────────────────────────────
// Events & supersteps
// ────────────────────────────────────────────────────────────────────────────

func TestPostHandledSameTick(t *testing.T) {
	w := newWorld()
	a := w.add(&unit{id: 1})
	a.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
		ctx.Post(2, event{kind: evPing, from: 1})
		return 0
	}
	b := w.add(&unit{id: 2, period: 0})
	sc := newSched(w, 0)
	sc.Schedule(a.id, 0)

	sc.Tick() // single tick

	require.Equal(t, []int64{0}, b.thinkTicks) // B reacted in the SAME tick
	require.Len(t, b.thinkEvents, 1)
	require.Len(t, b.thinkEvents[0], 1)
	assert.Equal(t, evPing, b.thinkEvents[0][0].kind)
}

func TestCascadeWithinBudget(t *testing.T) {
	w := newWorld()
	mk := func(id, target uint64) *unit {
		u := &unit{id: id}
		u.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
			if target != 0 {
				ctx.Post(target, event{from: id})
			}
			return 0
		}
		return w.add(u)
	}
	mk(1, 2) // A -> B
	mk(2, 3) // B -> C
	c := mk(3, 0)

	sc := newSched(w, 3)
	sc.Schedule(1, 0)
	sc.Tick()

	assert.Equal(t, []int64{0}, c.thinkTicks) // 3-hop chain resolves in one tick
	assert.Zero(t, sc.Overflow())
}

func TestCascadeOverflowSpillsToNextTick(t *testing.T) {
	w := newWorld()
	mk := func(id, target uint64) *unit {
		u := &unit{id: id}
		u.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
			if target != 0 {
				ctx.Post(target, event{from: id})
			}
			return 0
		}
		return w.add(u)
	}
	mk(1, 2) // A -> B (step 0)
	mk(2, 3) // B -> C (step 1)
	mk(3, 4) // C -> D (step 2), D queued but budget exhausted
	d := mk(4, 0)

	sc := newSched(w, 3)
	sc.Schedule(1, 0)

	sc.Tick() // tick 0: A,B,C run; D deferred
	assert.Empty(t, d.thinkTicks)
	assert.Equal(t, 1, sc.Overflow())

	sc.Tick() // tick 1: D runs
	assert.Equal(t, []int64{1}, d.thinkTicks)
	// The event that spilled is NOT lost: D still receives C's event next tick.
	require.Len(t, d.thinkEvents, 1)
	require.Len(t, d.thinkEvents[0], 1)
	assert.EqualValues(t, 3, d.thinkEvents[0][0].from)
}

// When several sources fan out onto the SAME overflow target, the spilled events
// are neither dropped nor duplicated: next tick the target sees all of them in
// exactly one Think.
func TestOverflowFanoutNoDropNoDuplicate(t *testing.T) {
	w := newWorld()
	// Three sources each drive a 2-hop chain so their final post to the shared
	// target T lands in wave 3 (the overflow wave) of tick 0.
	chain := func(a, b, target uint64) {
		ua := &unit{id: a}
		ua.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
			ctx.Post(b, event{from: a})
			return 0
		}
		ub := &unit{id: b}
		ub.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
			ctx.Post(target, event{from: b})
			return 0
		}
		w.add(ua)
		w.add(ub)
	}
	chain(1, 2, 100)
	chain(3, 4, 100)
	chain(5, 6, 100)
	target := w.add(&unit{id: 100, period: 0})

	sc := newSched(w, 2) // budget 2: wave0 sources, wave1 mids, target spills
	for _, id := range []uint64{1, 3, 5} {
		sc.Schedule(id, 0)
	}

	sc.Tick() // wave0: 1,3,5; wave1: 2,4,6 -> all post T; budget=2 -> T spills
	assert.Empty(t, target.thinkTicks)
	assert.Equal(t, 1, sc.Overflow()) // exactly one ref (T) deferred, despite 3 events

	sc.Tick()
	require.Equal(t, []int64{1}, target.thinkTicks) // exactly one Think
	assert.Len(t, target.thinkEvents[0], 3)         // all three events, no dup, no drop
}

func TestEventBatching(t *testing.T) {
	w := newWorld()
	post := func(id, target uint64) *unit {
		u := &unit{id: id}
		u.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
			ctx.Post(target, event{from: id})
			return 0
		}
		return w.add(u)
	}
	post(1, 3) // A -> C
	post(2, 3) // B -> C
	c := w.add(&unit{id: 3, period: 0})

	sc := newSched(w, 0)
	sc.Schedule(1, 0)
	sc.Schedule(2, 0)
	sc.Tick()

	require.Equal(t, []int64{0}, c.thinkTicks)
	// Both posts land in ONE Think call for C.
	require.Len(t, c.thinkEvents, 1)
	assert.Len(t, c.thinkEvents[0], 2)
}

func TestPoke(t *testing.T) {
	w := newWorld()
	a := w.add(&unit{id: 1})
	a.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
		ctx.Poke(2)
		return 0
	}
	b := w.add(&unit{id: 2, period: 0})
	sc := newSched(w, 0)
	sc.Schedule(1, 0)
	sc.Tick()

	require.Equal(t, []int64{0}, b.thinkTicks) // woken same tick
	require.Len(t, b.thinkEvents, 1)
	assert.Empty(t, b.thinkEvents[0]) // no payload
}

func TestSelfPostNextWave(t *testing.T) {
	w := newWorld()
	a := w.add(&unit{id: 1})
	a.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
		// Re-arm itself once via a self-post; without a guard this would loop,
		// so only post on the very first think.
		if len(u.thinkTicks) == 1 {
			ctx.Post(1, event{kind: evPing})
		}
		return 0
	}
	sc := newSched(w, 0)
	sc.Schedule(1, 0)
	sc.Tick()

	// Think at step 0 (timer) and step 1 (self-post), both in tick 0.
	assert.Equal(t, []int64{0, 0}, a.thinkTicks)
	require.Len(t, a.thinkEvents, 2)
	assert.Empty(t, a.thinkEvents[0])
	assert.Len(t, a.thinkEvents[1], 1)
}

// ────────────────────────────────────────────────────────────────────────────
// External input, ordering, removal
// ────────────────────────────────────────────────────────────────────────────

func TestEmitExternalInput(t *testing.T) {
	w := newWorld()
	a := w.add(&unit{id: 1, period: 0})
	sc := newSched(w, 0)

	sc.Emit(a.id, event{kind: evPing, val: 42})
	assert.Empty(t, a.thinkTicks) // not processed until a tick runs

	sc.Tick()

	require.Equal(t, []int64{0}, a.thinkTicks)
	require.Len(t, a.thinkEvents[0], 1)
	assert.Equal(t, 42, a.thinkEvents[0][0].val)
}

func TestEmitDedupesRef(t *testing.T) {
	w := newWorld()
	a := w.add(&unit{id: 1, period: 0})
	sc := newSched(w, 0)

	sc.Emit(a.id, event{val: 1})
	sc.Emit(a.id, event{val: 2})
	sc.Tick()

	require.Len(t, a.thinkTicks, 1) // one Think, both events batched
	assert.Len(t, a.thinkEvents[0], 2)
}

func TestDeterministicOrderWithinWave(t *testing.T) {
	w := newWorld()
	// Insertion / id order deliberately scrambled.
	w.add(&unit{id: 3, period: 0})
	w.add(&unit{id: 1, period: 0})
	w.add(&unit{id: 2, period: 0})
	sc := newSched(w, 0)
	sc.Schedule(3, 0)
	sc.Schedule(1, 0)
	sc.Schedule(2, 0)

	sc.Tick()

	assert.Equal(t, []uint64{1, 2, 3}, w.order) // ascending ref order
}

func TestRemovedUnitSkipped(t *testing.T) {
	w := newWorld()
	a := w.add(&unit{id: 1})
	a.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
		// Despawn B from the world and still post to it: must be skipped safely.
		delete(ctx.World.units, 2)
		ctx.Post(2, event{from: 1})
		return 0
	}
	b := w.add(&unit{id: 2, period: 0})
	sc := newSched(w, 0)
	sc.Schedule(1, 0)

	assert.NotPanics(t, func() { sc.Tick() })
	assert.Empty(t, b.thinkTicks)
}

func TestRemoveCancelsTimer(t *testing.T) {
	w := newWorld()
	a := w.add(&unit{id: 1, period: 2})
	sc := newSched(w, 0)
	sc.Schedule(1, 0)

	sc.Tick() // think at 0, reschedules to 2
	sc.Remove(1)
	delete(w.units, 1)

	for range 5 {
		sc.Tick()
	}
	assert.Equal(t, []int64{0}, a.thinkTicks) // never thinks again
	assert.Zero(t, sc.heap.Size())
}

// Remove must also drop a pending (emitted-but-not-yet-run) activation, even if
// the unit stays alive in the registry — otherwise it would get a spurious
// empty Think next tick.
func TestRemoveDropsPendingActivation(t *testing.T) {
	w := newWorld()
	a := w.add(&unit{id: 1, period: 0})
	sc := newSched(w, 0)

	sc.Emit(1, event{kind: evPing})
	sc.Remove(1) // unit stays in w.units (deactivate, not despawn)

	for range 3 {
		sc.Tick()
	}
	assert.Empty(t, a.thinkTicks) // never thinks: the pending activation was dropped
}

// Contract pin: Remove does NOT retract a ref already in the current wave's
// frontier. The documented way to despawn mid-Think is to delete from the
// registry (lookup fails). This locks the intended semantics.
func TestRemoveDuringWaveDoesNotCancelCurrentWave(t *testing.T) {
	w := newWorld()
	var sc *Scheduler[*world, event, *unit]
	a := w.add(&unit{id: 1})
	a.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
		sc.Remove(2) // B (id 2) is later in this same already-sorted wave
		return 0
	}
	b := w.add(&unit{id: 2, period: 0})
	sc = newSched(w, 0)
	sc.Schedule(1, 0)
	sc.Schedule(2, 0)

	sc.Tick()
	// Scheduler-only Remove can't pull B from the current wave; B still thinks.
	assert.Equal(t, []int64{0}, b.thinkTicks)

	// Whereas deleting from the registry mid-wave DOES skip the current wave.
	w2 := newWorld()
	var sc2 *Scheduler[*world, event, *unit]
	a2 := w2.add(&unit{id: 1})
	a2.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
		delete(ctx.World.units, 2)
		return 0
	}
	b2 := w2.add(&unit{id: 2, period: 0})
	sc2 = newSched(w2, 0)
	sc2.Schedule(1, 0)
	sc2.Schedule(2, 0)

	sc2.Tick()
	assert.Empty(t, b2.thinkTicks) // lookup fails -> skipped this wave and forever
}

// Same scenario run twice yields the identical processing order — no map/heap
// iteration order leaks into execution.
func TestRepeatableOrder(t *testing.T) {
	run := func() []uint64 {
		w := newWorld()
		for _, id := range []uint64{7, 3, 9, 1, 5} {
			u := &unit{id: id}
			u.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
				// Each fan-outs to two others on the first wave to exercise cascades.
				if len(u.thinkTicks) == 1 {
					ctx.Post(u.id^1, event{from: u.id})
					ctx.Poke(u.id + 2)
				}
				return 0
			}
			w.add(u)
		}
		sc := newSched(w, 0)
		for _, id := range []uint64{7, 3, 9, 1, 5} {
			sc.Schedule(id, 0)
		}
		for range 3 {
			sc.Tick()
		}
		return w.order
	}
	assert.Equal(t, run(), run())
}

// Randomised soak test: many units with mixed periodic / reactive / fan-out
// behaviour, asserting the scheduler's invariants hold over many ticks.
func TestSoakInvariants(t *testing.T) {
	const n = 200
	rng := rand.New(rand.NewSource(20260622))

	w := newWorld()
	live := map[uint64]bool{}
	for id := uint64(1); id <= n; id++ {
		u := &unit{id: id}
		u.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
			// Invariant: a unit never sees time go backwards.
			require.GreaterOrEqual(t, ctx.Now, u.lastNow)
			u.lastNow = ctx.Now
			// Randomly poke / post a few neighbours to exercise cascades.
			for k := 0; k < rng.Intn(3); k++ {
				tgt := uint64(rng.Intn(n) + 1)
				if rng.Intn(2) == 0 {
					ctx.Post(tgt, event{from: id})
				} else {
					ctx.Poke(tgt)
				}
			}
			// Mix of periodic and one-shot.
			if rng.Intn(4) == 0 {
				return 0
			}
			return int64(rng.Intn(5) + 1)
		}
		w.add(u)
		live[id] = true
	}

	sc := newSched(w, 3)
	for id := uint64(1); id <= n; id++ {
		sc.Schedule(id, int64(rng.Intn(4)))
	}

	prevOverflow := 0
	for tick := int64(0); tick < 500; tick++ {
		require.Equal(t, tick, sc.Now())
		require.NotPanics(t, func() { sc.Tick() })
		// Overflow only ever grows.
		require.GreaterOrEqual(t, sc.Overflow(), prevOverflow)
		prevOverflow = sc.Overflow()
		// One heap entry per unit at most: never exceed the live population.
		require.LessOrEqual(t, sc.heap.Size(), len(live))
		// Core invariant: any unit still holding queued events after a tick must
		// be scheduled to drain them next tick (overflow carry => pending). No
		// inbox is ever orphaned, so no event is silently lost.
		for ref, evs := range sc.inbox {
			if len(evs) > 0 {
				require.GreaterOrEqual(t, sc.pending.Has(ref), 0,
					"unit %d has queued events but is not scheduled", ref)
			}
		}
	}
	assert.EqualValues(t, 500, sc.Now())
}

// ────────────────────────────────────────────────────────────────────────────
// Integration: a tiny combat that terminates
// ────────────────────────────────────────────────────────────────────────────

func TestMiniCombatTerminates(t *testing.T) {
	w := newWorld()
	// Two fighters trade blows every tick until one dies; the killer keeps the
	// authority over its own HP by handling DamageEvents in its own Think.
	mkFighter := func(id, target uint64, hp int) *unit {
		u := &unit{id: id, hp: hp}
		u.behave = func(u *unit, ctx *Ctx[*world, event], events []event) int64 {
			for _, e := range events {
				if e.kind == evDamage {
					u.hp -= e.val
				}
			}
			if u.hp <= 0 {
				return 0 // dead: stop thinking
			}
			// Attack the target if it is still alive.
			if _, ok := ctx.World.get(target); ok {
				ctx.Post(target, event{kind: evDamage, from: id, val: 7})
			}
			return 1 // think again next tick
		}
		return w.add(u)
	}
	a := mkFighter(1, 2, 30)
	b := mkFighter(2, 1, 30)

	sc := newSched(w, 0)
	sc.Schedule(1, 0)
	sc.Schedule(2, 0)

	for range 100 {
		sc.Tick()
		if a.hp <= 0 || b.hp <= 0 {
			break
		}
	}

	// Someone died, HP is consistent, and the sim did not run away.
	assert.True(t, a.hp <= 0 || b.hp <= 0, "one fighter should die")
	assert.Less(t, sc.Now(), int64(100))
}

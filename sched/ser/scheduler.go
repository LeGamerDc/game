// Package ser implements a single-threaded, easy-to-understand tick scheduler.
//
// It is a deliberately simpler sibling of the parallel scheduler in sched/par.
// Where par splits work into Think/Apply phases, buckets effects by owner, and
// double-buffers signals to survive a BSP barrier, ser collapses all of that
// into ONE programming model that only makes sense single-threaded:
//
//   - A unit's only entry point is Think. Inside Think a unit may read the world
//     and other units directly (synchronous, race-free), mutate its OWN state,
//     and decide its OWN next wakeup.
//   - Cross-unit interaction is a single typed Event posted to the target. The
//     TARGET handles it in its own Think, so every unit stays the sole authority
//     over its state and its next wakeup time. There is no Apply phase, no
//     StagedState, and no Effect/Signal split.
//
// Timing model:
//
//   - Each unit has at most one scheduled wakeup, held in a min-heap keyed by ref
//     and ordered by deadline (the unit's "nextThink"). Think returns the unit's
//     authoritative next delay; the scheduler overwrites the single heap entry
//     with it (or cancels it when the unit sleeps). Re-scheduling is therefore
//     O(log n) with no stale entries — the churn that a per-deadline timer would
//     suffer simply cannot happen.
//   - A tick is processed as bounded "supersteps" (waves). Events posted during
//     superstep S are handled in superstep S+1 of the SAME tick, so cross-unit
//     reactions land same-tick. After maxSteps waves any still-pending work
//     spills to the next tick (see Overflow), which both bounds the per-tick cost
//     and turns runaway feedback loops into an observable metric instead of a
//     hang.
//
// Determinism: within a wave units are processed in ascending ref order, and a
// unit's state changes only when it itself thinks. Observing a unit always
// yields its last-thought state; an event posted during a wave is invisible
// until the next wave. The execution order does not depend on heap internals,
// map iteration, or post order — the frontier is sorted before every wave.
//
// Relation to prior art: this is essentially a single-threaded Pregel/BSP loop
// (messages from superstep S delivered in S+1 across a barrier; a vertex votes
// to halt and is reactivated by a message) with a per-tick superstep cap, where
// the barrier is realized by a Bevy-Events-style double-buffered inbox and the
// "a unit only mutates its own state, in response to messages it receives" rule
// is the actor model. A key invariant lifted from those systems' failure modes:
// every Post/Emit also schedules its target into the wave that consumes it, so —
// unlike Bevy's swap-and-clear buffers — an event is never silently dropped; it
// either runs this tick or spills (with its inbox intact) to the next.
package ser

import (
	"slices"

	"github.com/legamerdc/game/lib"
)

const defaultMaxSteps = 3

// Unit is a serial logic unit owned by exactly one scheduler.
//
// Think is invoked when the unit is woken by (a) its own timer (the delay it
// last returned elapsed), (b) events posted to it, or (c) external input. The
// events slice holds every event delivered since the unit last thought (empty
// for a pure timer wake). Think must return the delay, in ticks relative to the
// current tick, until the unit's next self-scheduled wakeup; delay <= 0 means
// "sleep" — no timer is registered and any prior timer is cancelled, so only a
// future event/poke will wake it.
//
// Think MUST return the unit's true next deadline every time it is called,
// regardless of why it was woken, because the return value is authoritative: it
// overwrites (or clears) the unit's single heap entry. A unit that wants a fixed
// cadence independent of event wakeups should track an absolute next-tick and
// return next-tick minus Now.
//
// A unit must never mutate another unit's state directly; express cross-unit
// state changes as events via ctx.Post so the target's own Think applies them.
type Unit[W any, E any] interface {
	ID() uint64
	Think(ctx *Ctx[W, E], events []E) (delay int64)
}

// Ctx is handed to Unit.Think. It is created once per scheduler and reused
// across every call, so it allocates nothing on the hot path.
type Ctx[W any, E any] struct {
	// World is the injected world handle for read-only queries and for looking
	// up other units (e.g. ctx.World.GetUnit(ref)).
	World W
	// Now is the current tick.
	Now int64

	post func(ref uint64, ev E)
	poke func(ref uint64)
}

// Post delivers an event to ref. The target handles it in its own Think during
// the next superstep of the current tick (or the next tick if the per-tick
// superstep budget is exhausted). Posting to an unknown ref is a no-op once the
// scheduler fails to resolve it.
func (c *Ctx[W, E]) Post(ref uint64, ev E) { c.post(ref, ev) }

// Poke wakes ref with no payload; it will Think with an empty event slice,
// scheduled exactly like Post (next superstep, same tick). Use it for "please
// reconsider" notifications that carry no data.
func (c *Ctx[W, E]) Poke(ref uint64) { c.poke(ref) }

// Scheduler drives units serially. It owns timer scheduling (a min-heap keyed by
// ref, one entry per unit) and per-unit event inboxes. It does NOT own unit
// storage: units are resolved through the injected lookup, so spawn/despawn and
// the authoritative registry live entirely in the caller's world.
type Scheduler[W any, E any, U Unit[W, E]] struct {
	lookup func(ref uint64) (U, bool)

	now      int64
	maxSteps int

	heap  lib.HeapIndexMap[uint64, int64, int64] // ref -> deadline; one entry per unit
	inbox map[uint64][]E                         // ref -> events queued for its next Think

	// superstep frontiers (reused across ticks)
	frontier []uint64             // refs to think in the current wave (sorted before use)
	batch    [][]E                // frontier[i]'s snapshot events, taken before any Think runs
	next     lib.IndexSet[uint64] // refs queued for the next wave (deduped)

	// refs to seed at the start of the next tick: external input plus overflow
	pending lib.IndexSet[uint64]

	overflow int // cumulative refs deferred to a later tick by the superstep cap

	ctx *Ctx[W, E]
}

// NewScheduler builds a scheduler. world is passed through to every Think via
// ctx.World; lookup resolves a ref to its unit (typically world.GetUnit, with U
// usually a pointer type so unit state mutations persist). A maxSteps <= 0
// selects the default budget of 3 supersteps per tick.
func NewScheduler[W any, E any, U Unit[W, E]](
	world W,
	lookup func(ref uint64) (U, bool),
	maxSteps int,
) *Scheduler[W, E, U] {
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}
	sc := &Scheduler[W, E, U]{
		lookup:   lookup,
		maxSteps: maxSteps,
		inbox:    make(map[uint64][]E),
	}
	sc.heap.Reserve(64)
	sc.next.Init(64)
	sc.pending.Init(64)
	sc.ctx = &Ctx[W, E]{World: world}
	sc.ctx.post = sc.post
	sc.ctx.poke = sc.poke
	return sc
}

// Now returns the current tick. It advances by one after every Tick.
func (sc *Scheduler[W, E, U]) Now() int64 { return sc.now }

// Overflow returns the cumulative number of refs that were deferred to a later
// tick because a single tick's event cascade exceeded maxSteps. A steadily
// growing value indicates a runaway feedback loop (e.g. two units that keep
// posting to each other every wave).
func (sc *Scheduler[W, E, U]) Overflow() int { return sc.overflow }

// Schedule requests ref to Think after delay ticks (relative to Now); delay <= 0
// schedules it for the next tick. Min-merge: it never pushes an existing wakeup
// later, only earlier. Use it to bootstrap a unit; afterwards units schedule
// themselves through Think's return value.
func (sc *Scheduler[W, E, U]) Schedule(ref uint64, delay int64) {
	if delay < 0 {
		delay = 0
	}
	sc.wakeAt(ref, sc.now+delay)
}

// Emit injects an external event to ref from outside a tick (e.g. network
// input). ref will Think on the next Tick. Call it between ticks; inside Think
// use ctx.Post instead.
func (sc *Scheduler[W, E, U]) Emit(ref uint64, ev E) {
	sc.inbox[ref] = append(sc.inbox[ref], ev)
	sc.pending.Put(ref)
}

// Remove unschedules ref: it drops the timer, any queued events, and any
// pending or next-wave activation, so ref will not be woken again until it is
// re-scheduled, emitted, or posted to. It does NOT touch the caller's registry,
// so a complete despawn is Remove plus deleting the unit from the world.
//
// Remove is intended for use between ticks. It does not retract a ref that is
// already in the CURRENT wave's frontier: if called from within a Think for a
// unit still pending in this wave, that unit will finish the wave (and may even
// re-arm itself by returning delay > 0). To despawn mid-Think, delete the unit
// from the registry so lookup fails — that skips both the remaining current-wave
// Think and every future wake.
func (sc *Scheduler[W, E, U]) Remove(ref uint64) {
	if i, _ := sc.heap.Get(ref); i >= 0 {
		sc.heap.Remove(i)
	}
	delete(sc.inbox, ref)
	if i := sc.pending.Has(ref); i >= 0 {
		sc.pending.Remove(i)
	}
	if i := sc.next.Has(ref); i >= 0 {
		sc.next.Remove(i)
	}
}

func (sc *Scheduler[W, E, U]) wakeAt(ref uint64, at int64) {
	if i, cur := sc.heap.Get(ref); i >= 0 && cur <= at {
		return // already scheduled at or before `at`
	}
	sc.heap.Push(ref, at, at)
}

func (sc *Scheduler[W, E, U]) post(ref uint64, ev E) {
	sc.inbox[ref] = append(sc.inbox[ref], ev)
	sc.next.Put(ref)
}

func (sc *Scheduler[W, E, U]) poke(ref uint64) {
	sc.next.Put(ref)
}

// Tick advances the clock by one and runs all due work. Timer-due units and
// pending external input form the initial frontier; events then cascade through
// up to maxSteps supersteps within this same tick. Cascades deeper than maxSteps
// spill to the next tick.
func (sc *Scheduler[W, E, U]) Tick() {
	now := sc.now
	sc.ctx.Now = now

	// 1) Seed the initial frontier from due timers + pending external input.
	sc.next.Clear()
	for sc.heap.Size() > 0 {
		_, ref, _, deadline := sc.heap.Top()
		if deadline > now {
			break
		}
		sc.heap.Pop()
		sc.next.Put(ref)
	}
	for _, ref := range sc.pending.Raw() {
		sc.next.Put(ref)
	}
	sc.pending.Clear()
	sc.frontier = append(sc.frontier[:0], sc.next.Raw()...)

	// 2) Superstep cascade.
	for step := 0; step < sc.maxSteps && len(sc.frontier) > 0; step++ {
		// Deterministic order within a wave, independent of heap/post order.
		slices.Sort(sc.frontier)

		// Snapshot every frontier unit's events BEFORE any Think runs, so events
		// posted during this wave are seen only in the next one.
		sc.batch = sc.batch[:0]
		for _, ref := range sc.frontier {
			ev := sc.inbox[ref]
			delete(sc.inbox, ref)
			sc.batch = append(sc.batch, ev)
		}

		sc.next.Clear()
		for i, ref := range sc.frontier {
			u, ok := sc.lookup(ref)
			if !ok {
				continue // unit despawned; its snapshot events are dropped
			}
			// Think's return value is the unit's authoritative next deadline, so
			// it overwrites the single heap entry (unlike the external Schedule,
			// which is min-merge). delay > 0 schedules a strictly future tick;
			// delay <= 0 cancels any existing timer so the unit truly sleeps;
			// same-tick re-think only happens via Post/Poke.
			if delay := u.Think(sc.ctx, sc.batch[i]); delay > 0 {
				at := now + delay
				sc.heap.Push(ref, at, at)
			} else if hi, _ := sc.heap.Get(ref); hi >= 0 {
				sc.heap.Remove(hi)
			}
		}
		clear(sc.batch) // release event-slice references for GC
		sc.frontier = append(sc.frontier[:0], sc.next.Raw()...)
	}

	// 3) Overflow: anything still queued after the cap spills to the next tick.
	// Their events remain in inbox; they are re-seeded from pending.
	if len(sc.frontier) > 0 {
		sc.overflow += len(sc.frontier)
		for _, ref := range sc.frontier {
			sc.pending.Put(ref)
		}
	}
	sc.frontier = sc.frontier[:0]

	sc.now++
}

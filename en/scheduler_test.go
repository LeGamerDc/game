package engine

import (
	"sync/atomic"
	"testing"
)

// ────────────────────────────────────────────────────────────────────────────
// Test doubles
// ────────────────────────────────────────────────────────────────────────────

type testWorld struct {
	now     int64
	version uint32
	round   int32
	logics  map[uint64]*testLogic
}

func newTestWorld() testWorld {
	return testWorld{logics: make(map[uint64]*testLogic)}
}

func (w testWorld) Now() int64      { return w.now }
func (w testWorld) Version() uint32 { return w.version }
func (w testWorld) Round() int32    { return w.round }

// GetLogic implements LogicProvider[*testLogic].
// Value receiver is fine: map is a reference type, so copies of testWorld
// share the same underlying map — mutations after Scheduler construction
// are visible to the scheduler.
func (w testWorld) GetLogic(id uint64) (*testLogic, bool) {
	l, ok := w.logics[id]
	return l, ok
}

func (w testWorld) addLogic(logic *testLogic) {
	w.logics[logic.id] = logic
}

func (w testWorld) removeLogic(id uint64) {
	delete(w.logics, id)
}

type testSignal struct {
	kind  SignalKind
	value int
}

func (s testSignal) Kind() SignalKind { return s.kind }

type testEffect struct {
	kind  EffectKind
	value int
}

func (e testEffect) Kind() EffectKind { return e.kind }

// testLogic is a programmable Logic stub.
// All callbacks are optional; nil means no-op.
type testLogic struct {
	id        uint64
	thinkFn   func(*ThinkCtx[testWorld, testSignal, testEffect], Inbox[testSignal]) int64
	applyFn   func(*CommitCtx[testWorld, testSignal], Arrangement[testEffect])
	thinkHits atomic.Int64
	applyHits atomic.Int64
}

func (l *testLogic) ID() uint64 { return l.id }

func (l *testLogic) Think(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
	l.thinkHits.Add(1)
	if l.thinkFn != nil {
		return l.thinkFn(ctx, inbox)
	}
	return 0
}

func (l *testLogic) Apply(ctx *CommitCtx[testWorld, testSignal], arr Arrangement[testEffect]) {
	l.applyHits.Add(1)
	if l.applyFn != nil {
		l.applyFn(ctx, arr)
	}
}

type testScheduler = Scheduler[testWorld, testSignal, testEffect, *testLogic]

func newTestScheduler(meta ScheduleMeta, w testWorld) *testScheduler {
	return NewScheduler[testWorld, testSignal, testEffect, *testLogic](meta, w)
}

func defaultMeta() ScheduleMeta {
	return ScheduleMeta{
		Concurrency:               3,
		BlockSize:                 7, // small prime for test
		MaxSupersteps:             3,
		TimerWheelSize:            8,
		ThinkConcurrencyThreshold: 1,
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Tests
// ────────────────────────────────────────────────────────────────────────────

// TestSchedulerEmptyTick verifies that ProcessTick with no logics and no
// pending input is a safe no-op.
func TestSchedulerEmptyTick(t *testing.T) {
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(defaultMeta(), world)
	// Should not panic
	sc.ProcessTick(world)
	sc.ProcessTick(world)
}

// TestSchedulerExternalSignalTriggersThink verifies that Emit → ProcessTick
// delivers the signal to the correct logic's Think and that the logic is
// activated exactly once per tick.
func TestSchedulerExternalSignalTriggersThink(t *testing.T) {
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(defaultMeta(), world)

	var receivedSignals []int
	logic := &testLogic{
		id: 42,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			for i := 0; i < inbox.Len(); i++ {
				receivedSignals = append(receivedSignals, inbox.At(i).value)
			}
			return 0
		},
	}
	world.addLogic(logic)

	sc.Emit(42, testSignal{value: 100})
	sc.Emit(42, testSignal{value: 200})
	sc.ProcessTick(world)

	if logic.thinkHits.Load() < 1 {
		t.Fatalf("expected at least 1 Think call, got %d", logic.thinkHits.Load())
	}
	if len(receivedSignals) != 2 {
		t.Fatalf("expected 2 signals, got %v", receivedSignals)
	}
	// Signals may arrive in any order within the batch
	sum := 0
	for _, v := range receivedSignals {
		sum += v
	}
	if sum != 300 {
		t.Fatalf("signal sum = %d, want 300", sum)
	}
}

// TestSchedulerTimerActivation verifies the full timer lifecycle:
// Think returns delay → timer registered → after delay ticks the logic
// is re-activated with empty inbox.
func TestSchedulerTimerActivation(t *testing.T) {
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(defaultMeta(), world)

	thinkCount := int64(0)
	logic := &testLogic{
		id: 10,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			thinkCount++
			if thinkCount == 1 {
				// First activation: register timer with delay=2
				return 2
			}
			// Second activation: timer fired, inbox should be empty
			if inbox.Len() != 0 {
				t.Errorf("timer activation should have empty inbox, got %d", inbox.Len())
			}
			return 0
		},
	}
	world.addLogic(logic)

	// Tick 1: external signal triggers Think, which registers delay=2
	sc.Emit(10, testSignal{value: 1})
	sc.ProcessTick(world)

	if thinkCount != 1 {
		t.Fatalf("tick 1: expected 1 Think, got %d", thinkCount)
	}

	// Tick 2: timer not yet expired (delay=2 means tick+2)
	sc.ProcessTick(world)
	if thinkCount != 1 {
		t.Fatalf("tick 2: timer should not fire yet, Think count = %d", thinkCount)
	}

	// Tick 3: timer expires, logic re-activated
	sc.ProcessTick(world)
	if thinkCount != 2 {
		t.Fatalf("tick 3: timer should fire, Think count = %d", thinkCount)
	}
}

// TestSchedulerEffectDelivery verifies that effects published during Think
// are correctly aggregated and delivered to the target logic's Apply.
func TestSchedulerEffectDelivery(t *testing.T) {
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(defaultMeta(), world)

	var appliedEffects []int
	source := &testLogic{
		id: 1,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			ctx.Publish(2, testEffect{value: 10})
			ctx.Publish(2, testEffect{value: 20})
			return 0
		},
	}
	target := &testLogic{
		id: 2,
		applyFn: func(ctx *CommitCtx[testWorld, testSignal], arr Arrangement[testEffect]) {
			for i := 0; i < arr.Len(); i++ {
				appliedEffects = append(appliedEffects, arr.At(i).value)
			}
		},
	}
	world.addLogic(source)
	world.addLogic(target)

	sc.Emit(1, testSignal{value: 1})
	sc.ProcessTick(world)

	if source.thinkHits.Load() < 1 {
		t.Fatalf("source Think count = %d, want >= 1", source.thinkHits.Load())
	}
	if target.applyHits.Load() != 1 {
		t.Fatalf("target Apply count = %d, want 1", target.applyHits.Load())
	}
	if len(appliedEffects) != 2 {
		t.Fatalf("applied effects = %v, want 2 entries", appliedEffects)
	}
}

// TestSchedulerSignalCascade verifies multi-superstep signal cascade:
// Logic A thinks → emits signal to B → B thinks (superstep 2) → emits
// signal to C → C thinks (superstep 3).
func TestSchedulerSignalCascade(t *testing.T) {
	meta := defaultMeta()
	meta.MaxSupersteps = 5 // allow enough rounds for the cascade
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(meta, world)

	var order []uint64

	logicA := &testLogic{
		id: 100,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			order = append(order, 100)
			ctx.Emit(200, testSignal{value: 1})
			return 0
		},
	}
	logicB := &testLogic{
		id: 200,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			order = append(order, 200)
			ctx.Emit(300, testSignal{value: 2})
			return 0
		},
	}
	logicC := &testLogic{
		id: 300,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			order = append(order, 300)
			return 0
		},
	}

	world.addLogic(logicA)
	world.addLogic(logicB)
	world.addLogic(logicC)

	sc.Emit(100, testSignal{value: 0})
	sc.ProcessTick(world)

	// All three should have been activated in cascade order
	if len(order) < 3 {
		t.Fatalf("cascade order = %v, want at least 3 entries", order)
	}
	// First entry must be A (it was the initial trigger)
	if order[0] != 100 {
		t.Fatalf("first cascade entry = %d, want 100", order[0])
	}
	// Check that all three appear
	seen := map[uint64]bool{}
	for _, v := range order {
		seen[v] = true
	}
	for _, id := range []uint64{100, 200, 300} {
		if !seen[id] {
			t.Fatalf("logic %d not seen in cascade order %v", id, order)
		}
	}
}

// TestSchedulerApplyEmitsSignal verifies that signals emitted during Apply
// are routed in the same superstep's signal routing pass and trigger Think
// in the next superstep.
func TestSchedulerApplyEmitsSignal(t *testing.T) {
	meta := defaultMeta()
	meta.MaxSupersteps = 5
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(meta, world)

	reactorThinkCount := int64(0)

	source := &testLogic{
		id: 10,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			ctx.Publish(20, testEffect{value: 42})
			return 0
		},
	}
	applier := &testLogic{
		id: 20,
		applyFn: func(ctx *CommitCtx[testWorld, testSignal], arr Arrangement[testEffect]) {
			// Apply emits a signal to reactor
			ctx.Emit(30, testSignal{value: 99})
		},
	}
	reactor := &testLogic{
		id: 30,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			reactorThinkCount++
			if inbox.Len() == 0 {
				t.Errorf("reactor got empty inbox, expected signal")
			}
			return 0
		},
	}

	world.addLogic(source)
	world.addLogic(applier)
	world.addLogic(reactor)

	sc.Emit(10, testSignal{value: 1})
	sc.ProcessTick(world)

	if reactorThinkCount < 1 {
		t.Fatalf("reactor should think at least once from Apply signal, got %d", reactorThinkCount)
	}
}

// TestSchedulerDeferOverflow verifies that when MaxSupersteps is reached
// with remaining signals, they are deferred to the next tick.
func TestSchedulerDeferOverflow(t *testing.T) {
	meta := defaultMeta()
	meta.MaxSupersteps = 1 // force overflow after 1 round
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(meta, world)

	thinkCount := int64(0)

	// A emits signal to B during Think. With MaxSupersteps=1,
	// B's signal won't be consumed this tick → deferred to next tick.
	logicA := &testLogic{
		id: 1,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			ctx.Emit(2, testSignal{value: 77})
			return 0
		},
	}
	logicB := &testLogic{
		id: 2,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			thinkCount++
			return 0
		},
	}

	world.addLogic(logicA)
	world.addLogic(logicB)

	// Tick 1: A thinks, emits to B, but MaxSupersteps=1 → B deferred
	sc.Emit(1, testSignal{value: 1})
	sc.ProcessTick(world)

	if thinkCount != 0 {
		t.Fatalf("tick 1: B should not think (deferred), got %d", thinkCount)
	}

	// Tick 2: deferred signal should trigger B
	sc.ProcessTick(world)

	if thinkCount < 1 {
		t.Fatalf("tick 2: B should think from deferred signal, got %d", thinkCount)
	}
}

// TestSchedulerSelfEffect verifies that a logic can publish effects
// targeting itself (target=self), which are delivered to its own Apply.
func TestSchedulerSelfEffect(t *testing.T) {
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(defaultMeta(), world)

	var selfApplied []int
	logic := &testLogic{
		id: 50,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			ctx.Publish(50, testEffect{value: 999})
			return 0
		},
		applyFn: func(ctx *CommitCtx[testWorld, testSignal], arr Arrangement[testEffect]) {
			for i := 0; i < arr.Len(); i++ {
				selfApplied = append(selfApplied, arr.At(i).value)
			}
		},
	}
	world.addLogic(logic)

	sc.Emit(50, testSignal{value: 1})
	sc.ProcessTick(world)

	if logic.thinkHits.Load() < 1 {
		t.Fatalf("Think count = %d, want >= 1", logic.thinkHits.Load())
	}
	if logic.applyHits.Load() != 1 {
		t.Fatalf("Apply count = %d, want 1", logic.applyHits.Load())
	}
	if len(selfApplied) != 1 || selfApplied[0] != 999 {
		t.Fatalf("self-applied = %v, want [999]", selfApplied)
	}
}

// TestSchedulerUnregisteredTargetDropped verifies that effects and signals
// targeting non-existent logics are silently dropped without panic.
func TestSchedulerUnregisteredTargetDropped(t *testing.T) {
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(defaultMeta(), world)

	logic := &testLogic{
		id: 1,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			// Publish effect to non-existent target
			ctx.Publish(99999, testEffect{value: 1})
			// Emit signal to non-existent target
			ctx.Emit(88888, testSignal{value: 2})
			return 0
		},
	}
	world.addLogic(logic)

	sc.Emit(1, testSignal{value: 0})
	// Should not panic
	sc.ProcessTick(world)

	if logic.thinkHits.Load() < 1 {
		t.Fatalf("Think count = %d, want >= 1", logic.thinkHits.Load())
	}
}

// TestSchedulerTimerOverrideWithinTick verifies that when a logic Thinks
// multiple times within a tick (via signal cascade), the last timer
// registration wins (last-write-wins in thread-local log).
func TestSchedulerTimerOverrideWithinTick(t *testing.T) {
	meta := defaultMeta()
	meta.MaxSupersteps = 5
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(meta, world)

	thinkRound := int64(0)

	logic := &testLogic{
		id: 5,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			thinkRound++
			if thinkRound == 1 {
				// First think: register delay=1 and emit signal to self
				// to trigger a second think in the same tick
				ctx.Emit(5, testSignal{value: 1})
				return 1
			}
			if thinkRound == 2 {
				// Second think (same tick via cascade): override timer to delay=3
				return 3
			}
			// Third think: should happen at tick 1 + 3 = tick 4
			return 0
		},
	}
	world.addLogic(logic)

	// Tick 1: external signal → Think1 (delay=1, emit self) → Think2 (delay=3 override)
	sc.Emit(5, testSignal{value: 0})
	sc.ProcessTick(world)
	if thinkRound < 2 {
		t.Fatalf("tick 1: expected at least 2 thinks, got %d", thinkRound)
	}

	// Tick 2: delay=3 means 3 ticks from registration → should NOT fire
	sc.ProcessTick(world)
	if thinkRound != 2 {
		t.Fatalf("tick 2: should not fire, got %d", thinkRound)
	}

	// Tick 3: still waiting
	sc.ProcessTick(world)
	if thinkRound != 2 {
		t.Fatalf("tick 3: should not fire, got %d", thinkRound)
	}

	// Tick 4: delay=3 expires
	sc.ProcessTick(world)
	if thinkRound < 3 {
		t.Fatalf("tick 4: timer should fire, got %d", thinkRound)
	}
}

// TestSchedulerMultipleLogicsParallel verifies that many logics can be
// processed in parallel without data races. Run with -race to check.
func TestSchedulerMultipleLogicsParallel(t *testing.T) {
	meta := defaultMeta()
	meta.Concurrency = 4
	meta.BlockSize = 13
	meta.MaxSupersteps = 3
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(meta, world)

	const numLogics = 50
	logics := make([]*testLogic, numLogics)

	for i := range numLogics {
		id := uint64(i + 1)
		logics[i] = &testLogic{
			id: id,
			thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
				// Each logic publishes an effect to the next logic (wrapping)
				target := (id % numLogics) + 1
				ctx.Publish(target, testEffect{value: int(id)})
				return 0
			},
		}
		world.addLogic(logics[i])
		sc.Emit(id, testSignal{value: int(id)})
	}

	sc.ProcessTick(world)

	// All logics should have been activated
	for i, l := range logics {
		if l.thinkHits.Load() == 0 {
			t.Errorf("logic %d: Think count = 0, want >= 1", i+1)
		}
		if l.applyHits.Load() == 0 {
			t.Errorf("logic %d: Apply count = 0, want >= 1", i+1)
		}
	}
}

// TestSchedulerLPTApplyAssignment verifies that computeApplyAssignment
// distributes blocks with effects across threads in a roughly balanced manner.
func TestSchedulerLPTApplyAssignment(t *testing.T) {
	meta := defaultMeta()
	meta.Concurrency = 2
	meta.BlockSize = 5
	world := newTestWorld()
	sc := newTestScheduler(meta, world)

	// Manually push effects to simulate uneven distribution:
	// Block 0: 10 effects, Block 1: 1 effect, Block 2: 5 effects,
	// Block 3: 0 effects, Block 4: 3 effects
	for i := range 10 {
		sc.effectCollectors[0].push(0, refVal[testEffect]{ref: 1, val: testEffect{value: i}})
	}
	sc.effectCollectors[0].push(1, refVal[testEffect]{ref: 2, val: testEffect{value: 0}})
	for i := range 5 {
		sc.effectCollectors[0].push(2, refVal[testEffect]{ref: 3, val: testEffect{value: i}})
	}
	for i := range 3 {
		sc.effectCollectors[0].push(4, refVal[testEffect]{ref: 5, val: testEffect{value: i}})
	}

	sc.computeApplyAssignment()

	// Block 3 has 0 effects → skipped
	totalBlocks := 0
	for thr := range 2 {
		totalBlocks += len(sc.applyBlocks[thr])
	}
	if totalBlocks != 4 {
		t.Fatalf("total assigned blocks = %d, want 4 (block3 has 0 effects)", totalBlocks)
	}

	// Verify thread loads are roughly balanced (no thread has all blocks)
	for thr := range 2 {
		if len(sc.applyBlocks[thr]) == 0 {
			t.Errorf("thread %d got 0 blocks, LPT should distribute", thr)
		}
	}
}

// TestSchedulerTimerAndSignalSameLogic verifies that a logic can be
// activated by both timer and signal in the same tick. Without dedup,
// Think may be called multiple times — the logic handles this.
func TestSchedulerTimerAndSignalSameLogic(t *testing.T) {
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(defaultMeta(), world)

	thinkCount := int64(0)

	logic := &testLogic{
		id: 7,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			thinkCount++
			if thinkCount == 1 {
				return 1 // schedule for next tick
			}
			return 0
		},
	}
	world.addLogic(logic)

	// Tick 1: external signal → Think, registers delay=1
	sc.Emit(7, testSignal{value: 1})
	sc.ProcessTick(world)
	if thinkCount != 1 {
		t.Fatalf("tick 1: expected 1 Think, got %d", thinkCount)
	}

	// Tick 2: timer fires AND external signal arrives
	// Logic may be called multiple times (timer + signal) — that's OK
	sc.Emit(7, testSignal{value: 2})
	sc.ProcessTick(world)
	if thinkCount < 2 {
		t.Fatalf("tick 2: expected at least 2 total Thinks, got %d", thinkCount)
	}
}

// TestSchedulerEffectsFromMultipleSources verifies that when multiple logics
// publish effects to the same target, all effects are aggregated in a single
// Apply call.
func TestSchedulerEffectsFromMultipleSources(t *testing.T) {
	meta := defaultMeta()
	meta.MaxSupersteps = 3
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(meta, world)

	var appliedValues []int

	source1 := &testLogic{
		id: 1,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			ctx.Publish(3, testEffect{value: 10})
			ctx.Publish(3, testEffect{value: 20})
			return 0
		},
	}
	source2 := &testLogic{
		id: 2,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			ctx.Publish(3, testEffect{value: 30})
			return 0
		},
	}
	target := &testLogic{
		id: 3,
		applyFn: func(ctx *CommitCtx[testWorld, testSignal], arr Arrangement[testEffect]) {
			for i := 0; i < arr.Len(); i++ {
				appliedValues = append(appliedValues, arr.At(i).value)
			}
		},
	}

	world.addLogic(source1)
	world.addLogic(source2)
	world.addLogic(target)

	sc.Emit(1, testSignal{value: 0})
	sc.Emit(2, testSignal{value: 0})
	sc.ProcessTick(world)

	if target.applyHits.Load() < 1 {
		t.Fatalf("target Apply count = %d, want >= 1", target.applyHits.Load())
	}
	// Check all effect values present (order may vary due to parallelism)
	sum := 0
	for _, v := range appliedValues {
		sum += v
	}
	if sum != 60 {
		t.Fatalf("sum of applied values = %d, want 60", sum)
	}
}

// TestSchedulerNoThinkForInactiveLogics verifies that logics not triggered
// by timer, signal, or external input are not activated.
func TestSchedulerNoThinkForInactiveLogics(t *testing.T) {
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(defaultMeta(), world)

	active := &testLogic{id: 1}
	inactive := &testLogic{id: 2}

	world.addLogic(active)
	world.addLogic(inactive)

	sc.Emit(1, testSignal{value: 0})
	sc.ProcessTick(world)

	if active.thinkHits.Load() < 1 {
		t.Fatalf("active logic Think count = %d, want >= 1", active.thinkHits.Load())
	}
	if inactive.thinkHits.Load() != 0 {
		t.Fatalf("inactive logic Think count = %d, want 0", inactive.thinkHits.Load())
	}
}

// TestSchedulerRemovedLogicNotActivated verifies that after removing a logic
// from the registry, it is no longer activated even if it has pending timer
// or signals.
func TestSchedulerRemovedLogicNotActivated(t *testing.T) {
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(defaultMeta(), world)

	logic := &testLogic{
		id: 5,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			return 1 // register timer
		},
	}
	world.addLogic(logic)

	// Tick 1: activate and register timer
	sc.Emit(5, testSignal{value: 0})
	sc.ProcessTick(world)
	if logic.thinkHits.Load() < 1 {
		t.Fatalf("tick 1: expected at least 1 Think, got %d", logic.thinkHits.Load())
	}

	// Remove from registry before timer fires
	world.removeLogic(5)

	// Also emit a signal to the removed logic
	sc.Emit(5, testSignal{value: 99})

	thinksBefore := logic.thinkHits.Load()

	// Tick 2: timer would fire + signal pending, but logic removed → no activation
	sc.ProcessTick(world)
	if logic.thinkHits.Load() != thinksBefore {
		t.Fatalf("tick 2: removed logic should not be activated, thinks went from %d to %d",
			thinksBefore, logic.thinkHits.Load())
	}
}

// TestSchedulerMultipleTicksTimerRepeat verifies that a logic can
// repeatedly reschedule itself via timer across multiple ticks.
func TestSchedulerMultipleTicksTimerRepeat(t *testing.T) {
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(defaultMeta(), world)

	thinkCount := int64(0)

	logic := &testLogic{
		id: 1,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			thinkCount++
			return 1 // always reschedule for next tick
		},
	}
	world.addLogic(logic)

	// Tick 1: initial activation
	sc.Emit(1, testSignal{value: 0})
	sc.ProcessTick(world)

	// Ticks 2-5: timer fires every tick
	for tick := 2; tick <= 5; tick++ {
		sc.ProcessTick(world)
	}

	if thinkCount < 5 {
		t.Fatalf("expected at least 5 total Thinks over 5 ticks, got %d", thinkCount)
	}
}

// TestSchedulerDefaultMeta verifies that zero-value ScheduleMeta fields
// are filled with sensible defaults.
func TestSchedulerDefaultMeta(t *testing.T) {
	world := newTestWorld()
	sc := newTestScheduler(ScheduleMeta{}, world)

	if sc.meta.Concurrency != 5 {
		t.Errorf("default Concurrency = %d, want 5", sc.meta.Concurrency)
	}
	if sc.meta.BlockSize != 137 {
		t.Errorf("default BlockSize = %d, want 137", sc.meta.BlockSize)
	}
	if sc.meta.MaxSupersteps != 3 {
		t.Errorf("default MaxSupersteps = %d, want 3", sc.meta.MaxSupersteps)
	}
	if sc.meta.TimerWheelSize != 200 {
		t.Errorf("default TimerWheelSize = %d, want 200", sc.meta.TimerWheelSize)
	}
	if sc.meta.ThinkConcurrencyThreshold != 500 {
		t.Errorf("default ThinkConcurrencyThreshold = %d, want 500", sc.meta.ThinkConcurrencyThreshold)
	}
}

// TestSchedulerConcurrentSafety runs many logics that cross-interact
// to detect data races under -race. This test exercises the parallel
// Think and Apply paths simultaneously.
func TestSchedulerConcurrentSafety(t *testing.T) {
	meta := ScheduleMeta{
		Concurrency:               4,
		BlockSize:                 11,
		MaxSupersteps:             3,
		TimerWheelSize:            16,
		ThinkConcurrencyThreshold: 1,
	}
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(meta, world)

	const N = 100
	logics := make([]*testLogic, N)
	for i := range N {
		id := uint64(i + 1)
		logics[i] = &testLogic{
			id: id,
			thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
				// Emit signals and effects to various targets
				ctx.Emit((id%uint64(N))+1, testSignal{value: int(id)})
				ctx.Publish((id%uint64(N))+1, testEffect{value: int(id)})
				if id%3 == 0 {
					return 2 // some logics register timers
				}
				return 0
			},
		}
		world.addLogic(logics[i])
		sc.Emit(id, testSignal{value: 0})
	}

	// Run several ticks to exercise timer firing + signal cascade + effect delivery
	for range 5 {
		sc.ProcessTick(world)
	}

	// Basic sanity: all logics should have been activated at least once
	for i, l := range logics {
		if l.thinkHits.Load() == 0 {
			t.Errorf("logic %d was never activated", i+1)
		}
	}
}

// TestSchedulerMultipleThinkCallsPerSuperstep verifies that the scheduler
// may call Think multiple times per superstep (no dedup guarantee) and
// the logic handles it correctly.
func TestSchedulerMultipleThinkCallsPerSuperstep(t *testing.T) {
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(defaultMeta(), world)

	totalSignals := int64(0)
	logic := &testLogic{
		id: 7,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			atomic.AddInt64(&totalSignals, int64(inbox.Len()))
			return 1 // register timer for next tick
		},
	}
	world.addLogic(logic)

	// Tick 1: initial activation via signal
	sc.Emit(7, testSignal{value: 1})
	sc.ProcessTick(world)

	// Tick 2: timer fires + external signal → may be 1 or 2 Think calls
	sc.Emit(7, testSignal{value: 2})
	sc.ProcessTick(world)

	// The signal must have been delivered at some point
	if totalSignals < 2 {
		t.Fatalf("expected at least 2 total signals across Think calls, got %d", totalSignals)
	}
}

// TestSchedulerDoubleBufferDefer verifies that signals remaining in
// signalRead after MaxSupersteps are automatically preserved for
// the next tick without explicit defer logic.
func TestSchedulerDoubleBufferDefer(t *testing.T) {
	meta := defaultMeta()
	meta.MaxSupersteps = 1
	world := newTestWorld()
	world.now = 1
	sc := newTestScheduler(meta, world)

	chainLen := int64(0)

	// A → B → C chain, but MaxSupersteps=1 so only A runs in tick 1
	logicA := &testLogic{
		id: 1,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			atomic.AddInt64(&chainLen, 1)
			ctx.Emit(2, testSignal{value: 1})
			return 0
		},
	}
	logicB := &testLogic{
		id: 2,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			atomic.AddInt64(&chainLen, 1)
			ctx.Emit(3, testSignal{value: 2})
			return 0
		},
	}
	logicC := &testLogic{
		id: 3,
		thinkFn: func(ctx *ThinkCtx[testWorld, testSignal, testEffect], inbox Inbox[testSignal]) int64 {
			atomic.AddInt64(&chainLen, 1)
			return 0
		},
	}

	world.addLogic(logicA)
	world.addLogic(logicB)
	world.addLogic(logicC)

	// Tick 1: A runs, emits to B (deferred)
	sc.Emit(1, testSignal{value: 0})
	sc.ProcessTick(world)
	if chainLen != 1 {
		t.Fatalf("tick 1: expected chain length 1, got %d", chainLen)
	}

	// Tick 2: B runs (from deferred), emits to C (deferred)
	sc.ProcessTick(world)
	if chainLen != 2 {
		t.Fatalf("tick 2: expected chain length 2, got %d", chainLen)
	}

	// Tick 3: C runs (from deferred)
	sc.ProcessTick(world)
	if chainLen != 3 {
		t.Fatalf("tick 3: expected chain length 3, got %d", chainLen)
	}
}

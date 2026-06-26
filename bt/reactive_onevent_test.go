package bt

import (
	"testing"

	"github.com/legamerdc/game/lib"
	"github.com/stretchr/testify/assert"
)

// These tests pin the intended behavior of reactiveBranch across its three entry
// points — Execute (full re-sweep), OnEvent (event-driven preemption/abort), and
// Cancel/OnComplete (teardown). The reuse helpers evtLeaf/funcLeaf live in
// reactive_test.go / review_test.go.

// ---------------------------------------------------------------------------
// OnEvent — preemption (selector) is event-driven.
// ---------------------------------------------------------------------------

// A higher-priority option that succeeds synchronously once its guard flips
// preempts the running lower-priority action on the event itself.
func TestReactiveSelector_EventPreemptsToHigherSuccess(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("g", lib.Bool(false))

	var low *evtLeaf
	lowCreator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		low = &evtLeaf{wantKind: 99, delay: TaskStatus(1000)}
		return low, true
	}
	tree := NewReactiveSelector(successGuard,
		NewTask(boolGuard("g"), newTestTaskCreator("high", TaskSuccess)), // dormant, guarded
		NewTask(successGuard, lowCreator),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(1000), r.Execute(ctx)) // g=false -> high fails -> low runs (active=1)

	ctx.Set("g", lib.Bool(true))
	got := r.OnEvent(ctx, &testEvent{kind: 1}) // kind 1; low only handles 99
	assert.Equal(t, TaskSuccess, got, "higher option now succeeds -> selector succeeds on the event")
	assert.True(t, low.canceled, "running low-priority action is preempted/cancelled")
}

// A higher-priority option that becomes Running once its guard flips takes over:
// the selector returns the high option's hint, cancels the low action, and the
// high option is the new active child (a later event completes it).
func TestReactiveSelector_EventPreemptsToHigherRunning(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("g", lib.Bool(false))

	var high, low *evtLeaf
	highCreator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		high = &evtLeaf{wantKind: 7, delay: TaskStatus(50)}
		return high, true
	}
	lowCreator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		low = &evtLeaf{wantKind: 99, delay: TaskStatus(1000)}
		return low, true
	}
	tree := NewReactiveSelector(successGuard,
		NewTask(boolGuard("g"), highCreator),
		NewTask(successGuard, lowCreator),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(1000), r.Execute(ctx)) // active=1 (low)

	ctx.Set("g", lib.Bool(true))
	assert.Equal(t, TaskStatus(50), r.OnEvent(ctx, &testEvent{kind: 1}),
		"high becomes the running active child; its hint is returned")
	assert.True(t, low.canceled, "low is preempted")
	assert.NotNil(t, high)
	assert.True(t, high.started)

	// active is now high (index 0); its own event completes it.
	assert.Equal(t, TaskSuccess, r.OnEvent(ctx, &testEvent{kind: 7}))
	assert.True(t, high.done && !high.canceled, "high completes normally via its own event")
}

// Preemption (Phase 1) takes priority over completing the active child (Phase 2):
// when one event would both complete the active child and flip a higher-priority
// condition, the higher-priority branch wins and the active child is cancelled.
func TestReactiveSelector_EventPreemptionBeatsActiveCompletion(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("g", lib.Bool(false))

	var low *evtLeaf
	lowCreator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		low = &evtLeaf{wantKind: 5, delay: TaskStatus(1000)} // would complete on kind 5
		return low, true
	}
	tree := NewReactiveSelector(successGuard,
		NewTask(boolGuard("g"), newTestTaskCreator("high", TaskSuccess)),
		NewTask(successGuard, lowCreator),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(1000), r.Execute(ctx)) // active=1 (low)

	ctx.Set("g", lib.Bool(true))
	got := r.OnEvent(ctx, &testEvent{kind: 5}) // kind 5 would complete low, but g flipped too
	assert.Equal(t, TaskSuccess, got, "preemption is evaluated before the active child consumes the event")
	assert.True(t, low.canceled, "low is cancelled by preemption, not completed by its own event")
}

// Only the strictly-higher-priority children [0, active) are re-checked on an
// event; a LOWER-priority condition flipping must not preempt the active child.
func TestReactiveSelector_LowerPriorityFlipDoesNotPreemptOnEvent(t *testing.T) {
	ctx := newTestCtx()

	var high *evtLeaf
	highCreator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		high = &evtLeaf{wantKind: 99, delay: TaskStatus(1000)} // running, won't handle kind 1
		return high, true
	}
	tree := NewReactiveSelector(successGuard,
		NewTask(successGuard, highCreator),                                // active=0
		NewTask(boolGuard("low_g"), newTestTaskCreator("x", TaskSuccess)), // lower priority
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	ctx.Set("low_g", lib.Bool(false))
	assert.Equal(t, TaskStatus(1000), r.Execute(ctx)) // high runs (active=0)

	ctx.Set("low_g", lib.Bool(true)) // a lower-priority option becomes ready
	got := r.OnEvent(ctx, &testEvent{kind: 1})
	assert.Equal(t, TaskNew, got, "lower-priority readiness must not preempt; event unhandled")
	assert.False(t, high.canceled, "active high-priority child keeps running")
}

// ---------------------------------------------------------------------------
// OnEvent — condition-abort (sequence) is event-driven.
// ---------------------------------------------------------------------------

// A ReactiveSequence aborts the running action on the event that makes an earlier
// precondition false (previously this only happened on the next tick).
func TestReactiveSequence_EventAbortsOnPreconditionLost(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("ok", lib.Bool(true))

	var act *evtLeaf
	actCreator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		act = &evtLeaf{wantKind: 99, delay: TaskStatus(500)}
		return act, true
	}
	tree := NewReactiveSequence(successGuard,
		NewGuard[*testCtx, *testEvent](boolGuard("ok")),
		NewTask(successGuard, actCreator),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(500), r.Execute(ctx)) // ok=true -> action runs (active=1)

	ctx.Set("ok", lib.Bool(false))
	got := r.OnEvent(ctx, &testEvent{kind: 1}) // kind 1; action only handles 99
	assert.Equal(t, TaskFail, got, "lost precondition aborts the sequence on the event")
	assert.True(t, act.canceled, "running action is cancelled when its precondition is lost")
}

// When preconditions still hold, the event is forwarded to the active action and
// completing it advances the sequence (Phase 1 re-check is harmless here).
func TestReactiveSequence_EventCompletesActionWhenPreconditionsHold(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("ok", lib.Bool(true))

	var act *evtLeaf
	actCreator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		act = &evtLeaf{wantKind: 1, delay: TaskStatus(500)}
		return act, true
	}
	tree := NewReactiveSequence(successGuard,
		NewGuard[*testCtx, *testEvent](boolGuard("ok")),
		NewTask(successGuard, actCreator),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(500), r.Execute(ctx))
	got := r.OnEvent(ctx, &testEvent{kind: 1}) // precondition holds; action handles kind 1
	assert.Equal(t, TaskSuccess, got, "sequence completes after its last action completes via the event")
	assert.True(t, act.done && !act.canceled, "action completed normally, not cancelled")
}

// ---------------------------------------------------------------------------
// OnEvent — no-preemption forwarding and unhandled events.
// ---------------------------------------------------------------------------

// With no higher-priority condition flipped, an event the active child cannot
// handle returns TaskNew and leaves the active child running untouched.
func TestReactive_EventUnhandledKeepsActive(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("g", lib.Bool(false))

	var low *evtLeaf
	lowCreator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		low = &evtLeaf{wantKind: 99, delay: TaskStatus(1000)}
		return low, true
	}
	tree := NewReactiveSelector(successGuard,
		NewTask(boolGuard("g"), newTestTaskCreator("high", TaskSuccess)), // stays dormant (g=false)
		NewTask(successGuard, lowCreator),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(1000), r.Execute(ctx)) // active=1 (low)

	got := r.OnEvent(ctx, &testEvent{kind: 1}) // higher cond still false; low only handles 99
	assert.Equal(t, TaskNew, got, "no preemption and active cannot handle -> unhandled")
	assert.False(t, low.canceled, "active child is not disturbed by the re-check")
	assert.False(t, low.done, "active child keeps running")
}

// ---------------------------------------------------------------------------
// Execute — the active child is resumed, not regenerated, across ticks.
// ---------------------------------------------------------------------------

func TestReactiveSelector_ResumesActiveChildAcrossTicksWithoutRegen(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("g", lib.Bool(false))

	lowCreates := 0
	lowCreator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		lowCreates++
		return &funcLeaf{exec: func() TaskStatus { return TaskStatus(10) }}, true
	}
	tree := NewReactiveSelector(successGuard,
		NewTask(boolGuard("g"), newTestTaskCreator("high", TaskSuccess)), // re-evaluated each tick
		NewTask(successGuard, lowCreator),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	for i := 0; i < 3; i++ {
		assert.Equal(t, TaskStatus(10), r.Execute(ctx))
	}
	assert.Equal(t, 1, lowCreates,
		"active child is resumed across ticks (regenerated only after it ends or is preempted)")
}

// ---------------------------------------------------------------------------
// Cancel / OnComplete — teardown propagates to the active child exactly once.
// ---------------------------------------------------------------------------

// Tearing down a reactive branch (here via an enclosing AlwaysGuard losing its
// condition) cancels the active child with cancel=true.
func TestReactiveBranch_CancelPropagatesToActiveChild(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("ok", lib.Bool(true))

	var leaf *evtLeaf
	leafCreator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		leaf = &evtLeaf{wantKind: 99, delay: TaskStatus(100)}
		return leaf, true
	}
	tree := NewAlwaysGuard(boolGuard("ok"),
		NewReactiveSelector(successGuard, NewTask(successGuard, leafCreator)))
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(100), r.Execute(ctx))
	assert.True(t, leaf.started)

	ctx.Set("ok", lib.Bool(false))
	assert.Equal(t, TaskFail, r.Execute(ctx))
	assert.True(t, leaf.canceled, "active child of a torn-down reactive branch must see cancel=true")
}

// Teardown calls the active child's OnComplete exactly once (no double-cancel,
// no missed cleanup).
func TestReactiveBranch_CancelHitsActiveChildExactlyOnce(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("ok", lib.Bool(true))

	completes, cancels := 0, 0
	leafCreator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		return &funcLeaf{
			exec: func() TaskStatus { return TaskStatus(100) },
			onComplete: func(cancel bool) {
				completes++
				if cancel {
					cancels++
				}
			},
		}, true
	}
	tree := NewAlwaysGuard(boolGuard("ok"),
		NewReactiveSelector(successGuard, NewTask(successGuard, leafCreator)))
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(100), r.Execute(ctx))
	ctx.Set("ok", lib.Bool(false))
	assert.Equal(t, TaskFail, r.Execute(ctx))
	assert.Equal(t, 1, completes, "active child OnComplete called exactly once")
	assert.Equal(t, 1, cancels, "and exactly once with cancel=true (no double-cancel)")
}

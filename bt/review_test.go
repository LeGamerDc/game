package bt

import (
	"testing"

	"github.com/legamerdc/game/lib"
	"github.com/stretchr/testify/assert"
)

// evtLeaf is a controllable leaf: it reports `delay` while running and completes
// with TaskSuccess when it receives an event of kind `wantKind`.
type evtLeaf struct {
	wantKind int32
	delay    TaskStatus
	started  bool
	done     bool
	canceled bool
}

func (l *evtLeaf) Execute(_ *testCtx) TaskStatus { l.started = true; return l.delay }
func (l *evtLeaf) OnComplete(_ *testCtx, cancel bool) {
	l.done = true
	l.canceled = cancel
}
func (l *evtLeaf) OnEvent(_ *testCtx, e *testEvent) TaskStatus {
	if l.wantKind > 0 && e.Kind() == l.wantKind {
		return TaskSuccess
	}
	return TaskNew
}

// funcLeaf runs an arbitrary closure each Execute; used to model stateful actions.
type funcLeaf struct {
	exec       func() TaskStatus
	onComplete func(cancel bool)
}

func (l *funcLeaf) Execute(_ *testCtx) TaskStatus { return l.exec() }
func (l *funcLeaf) OnComplete(_ *testCtx, cancel bool) {
	if l.onComplete != nil {
		l.onComplete(cancel)
	}
}
func (l *funcLeaf) OnEvent(_ *testCtx, _ *testEvent) TaskStatus { return TaskNew }

// ---------------------------------------------------------------------------
// Finding A (FIXED): reactive preemption is now event-driven as well as
// tick-driven. OnEvent re-checks the strictly-higher-priority children before
// forwarding the event to the active child, so an event that flips a
// higher-priority condition preempts on the spot instead of waiting for the
// active child's (possibly large, see Finding B) delay hint to elapse.
// ---------------------------------------------------------------------------
func TestReactive_EventRechecksPrioritiesAndPreempts(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("g", lib.Bool(false))

	var action *evtLeaf
	actCreator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		action = &evtLeaf{wantKind: 99, delay: TaskStatus(1000)} // only handles kind 99
		return action, true
	}

	tree := NewReactiveSelector(successGuard,
		NewSequence(successGuard,
			NewGuard[*testCtx, *testEvent](boolGuard("g")),
			NewTask(successGuard, newTestTaskCreator("high", TaskSuccess)),
		),
		NewTask(successGuard, actCreator),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	// g=false -> high branch fails its guard -> low action runs, reporting 1000.
	assert.Equal(t, TaskStatus(1000), r.Execute(ctx))
	assert.NotNil(t, action)

	// g flips true and an UNRELATED event (kind 1; the action only handles 99)
	// arrives. Phase 1 of OnEvent re-checks the now-true higher-priority branch,
	// which succeeds synchronously and preempts the running action immediately.
	ctx.Set("g", lib.Bool(true))
	got := r.OnEvent(ctx, &testEvent{kind: 1})

	assert.Equal(t, TaskSuccess, got, "event re-evaluates higher-priority conditions and preempts on the spot")
	assert.True(t, action.canceled, "low-priority action is preempted by the event-driven re-check")
}

// Finding B: Execute propagates the active child's raw delay hint (no clamp).
// Between ticks, preemption therefore relies on events (see Finding A, now
// fixed); the hint only bounds how long a NON-event-backed higher-priority
// condition can go unnoticed under a delay-driven scheduler.
func TestReactive_ReturnsActiveChildDelayHint(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("g", lib.Bool(false))

	actCreator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		return &evtLeaf{wantKind: 99, delay: TaskStatus(5000)}, true
	}
	tree := NewReactiveSelector(successGuard,
		NewSequence(successGuard,
			NewGuard[*testCtx, *testEvent](boolGuard("g")),
			NewTask(successGuard, newTestTaskCreator("high", TaskSuccess)),
		),
		NewTask(successGuard, actCreator),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	// The reactive selector returns the action's 5000ms hint verbatim — the
	// scheduler would not re-tick (and re-check g) for 5000ms.
	assert.Equal(t, TaskStatus(5000), r.Execute(ctx),
		"reactive branch returns the active child's delay hint, capping preemption responsiveness")
}

// Finding C: a ReactiveSequence/Selector whose EARLIER child is a multi-tick
// action (not a synchronous condition) livelocks — every cycle the earlier
// action restarts and preempts the later child, which therefore never finishes.
// This is the classic reactive-composite constraint (shared with BT.CPP) but it
// is neither enforced nor detected here.
func TestReactiveSequence_EarlierMultiTickActionLivelocks(t *testing.T) {
	ctx := newTestCtx()

	// A: 2-tick action (Running once, then Success); regenerated each cycle.
	aCreator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		ticks := 0
		return &funcLeaf{exec: func() TaskStatus {
			if ticks == 0 {
				ticks++
				return TaskStatus(1) // running
			}
			return TaskSuccess
		}}, true
	}
	// B: an action that wants to run to completion but keeps being preempted.
	bStarts, bCancels := 0, 0
	bCreator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		return &funcLeaf{
			exec: func() TaskStatus { bStarts++; return TaskStatus(1) },
			onComplete: func(cancel bool) {
				if cancel {
					bCancels++
				}
			},
		}, true
	}

	tree := NewReactiveSequence(successGuard,
		NewTask(successGuard, aCreator),
		NewTask(successGuard, bCreator),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	for i := 0; i < 12; i++ {
		r.Execute(ctx)
	}
	assert.Greater(t, bCancels, 1,
		"B is repeatedly preempted/cancelled (livelock): earlier reactive children must be synchronous conditions")
}

// Finding D (semantic note, not a defect): a parallel broadcasts an event to ALL
// running children, so multiple children can complete from a single event even
// when fewer successes are required.
func TestParallel_EventBroadcastsToAllRunning(t *testing.T) {
	ctx := newTestCtx()
	var l1, l2 *evtLeaf
	c1 := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		l1 = &evtLeaf{wantKind: 5, delay: TaskStatus(100)}
		return l1, true
	}
	c2 := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		l2 = &evtLeaf{wantKind: 5, delay: TaskStatus(100)}
		return l2, true
	}
	par := NewParallel(successGuard, 1, 0, false, // only ONE success required
		NewTask(successGuard, c1),
		NewTask(successGuard, c2),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(par)

	assert.Equal(t, TaskStatus(100), r.Execute(ctx))
	assert.Equal(t, TaskSuccess, r.OnEvent(ctx, &testEvent{kind: 5}))
	// Both children consumed the same event and completed normally (not cancelled),
	// even though only one success was needed.
	assert.True(t, l1.done && !l1.canceled, "child 1 completed via the broadcast event")
	assert.True(t, l2.done && !l2.canceled, "child 2 also completed via the same event (broadcast)")
}

// ---------------------------------------------------------------------------
// Positive regression tests — confirm behaviors that MUST hold.
// ---------------------------------------------------------------------------

// An event reaches a running leaf that sits below a Sequence (the leaf, not the
// sequence, is at the top of the stack), and completing it resumes the sequence.
func TestSequence_EventCompletesRunningLeaf(t *testing.T) {
	ctx := newTestCtx()
	var leaf *evtLeaf
	creator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		leaf = &evtLeaf{wantKind: 7, delay: TaskStatus(30)}
		return leaf, true
	}
	tree := NewSequence(successGuard,
		NewTask(successGuard, newTestTaskCreator("a", TaskSuccess)),
		NewTask(successGuard, creator),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(30), r.Execute(ctx)) // a succeeds, b runs
	assert.Equal(t, TaskSuccess, r.OnEvent(ctx, &testEvent{kind: 7}))
	assert.True(t, leaf.done && !leaf.canceled)
}

// Events forward through nested parallels (two levels deep) to the inner leaf.
func TestNestedParallel_EventForwarding(t *testing.T) {
	ctx := newTestCtx()
	var leaf *evtLeaf
	creator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		leaf = &evtLeaf{wantKind: 3, delay: TaskStatus(40)}
		return leaf, true
	}
	inner := NewParallel(successGuard, 1, 0, false, NewTask(successGuard, creator))
	outer := NewParallel(successGuard, 1, 0, false, inner)
	var r Root[*testCtx, *testEvent]
	r.SetNode(outer)

	assert.Equal(t, TaskStatus(40), r.Execute(ctx))
	assert.Equal(t, TaskSuccess, r.OnEvent(ctx, &testEvent{kind: 3}))
	assert.True(t, leaf.done && !leaf.canceled)
}

// When AlwaysGuard loses its condition mid-run, the still-running inner task must
// be cancelled with cancel=true (resource cleanup must see the abort).
func TestAlwaysGuard_InnerTaskCancelFlagOnGuardLoss(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("ok", lib.Bool(true))
	var leaf *evtLeaf
	creator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		leaf = &evtLeaf{wantKind: 99, delay: TaskStatus(50)}
		return leaf, true
	}
	tree := NewAlwaysGuard(boolGuard("ok"), NewTask(successGuard, creator))
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(50), r.Execute(ctx))
	assert.True(t, leaf.started)

	ctx.Set("ok", lib.Bool(false))
	assert.Equal(t, TaskFail, r.Execute(ctx))
	assert.True(t, leaf.canceled, "inner task must see cancel=true when AlwaysGuard aborts it")
}

// Inverter (revise) must pass a running child's hint through untouched, then
// invert the terminal result when the child completes via an event.
func TestInverter_RunningChildThenInvert(t *testing.T) {
	ctx := newTestCtx()
	var leaf *evtLeaf
	creator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		leaf = &evtLeaf{wantKind: 1, delay: TaskStatus(20)}
		return leaf, true
	}
	tree := NewInverter(successGuard, NewTask(successGuard, creator))
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(20), r.Execute(ctx)) // child running; inverter passes hint through
	assert.Equal(t, TaskFail, r.OnEvent(ctx, &testEvent{kind: 1}))
}

// PostGuard must run its child first (no pre-check), then use the guard as the
// result — discarding the child's own result.
func TestPostGuard_WaitsForRunningChildThenChecksGuard(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("ok", lib.Bool(true))
	var leaf *evtLeaf
	creator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		leaf = &evtLeaf{wantKind: 2, delay: TaskStatus(15)}
		return leaf, true
	}
	tree := NewPostGuard(boolGuard("ok"), NewTask(successGuard, creator))
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(15), r.Execute(ctx)) // child running; guard not yet evaluated
	assert.Equal(t, TaskSuccess, r.OnEvent(ctx, &testEvent{kind: 2}))
}

func TestPostGuard_IgnoresChildResult(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("ok", lib.Bool(true))
	// Child FAILS, but PostGuard returns success because the guard is true.
	tree := NewPostGuard(boolGuard("ok"),
		NewTask(successGuard, newTestTaskCreator("f", TaskFail)))
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)
	assert.Equal(t, TaskSuccess, r.Execute(ctx))
}

// Repeat must regenerate its child each iteration and correctly thread
// event-driven completions across iterations.
func TestRepeat_RegeneratesAcrossEventCompletions(t *testing.T) {
	ctx := newTestCtx()
	iter := 0
	creator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		iter++
		return &evtLeaf{wantKind: 1, delay: TaskStatus(10)}, true
	}
	tree := NewRepeatUntilNSuccess(successGuard, 2, 5, NewTask(successGuard, creator))
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(10), r.Execute(ctx))                      // iter 1 running
	assert.Equal(t, TaskStatus(10), r.OnEvent(ctx, &testEvent{kind: 1})) // iter1 done -> iter2 running
	assert.Equal(t, TaskSuccess, r.OnEvent(ctx, &testEvent{kind: 1}))    // iter2 done -> 2 successes
	assert.Equal(t, 2, iter)
}

// SelectorN counting boundary: threshold met exactly at the last child.
func TestSelectorN_ThresholdAtLastChild(t *testing.T) {
	ctx := newTestCtx()
	sel := NewSelectorN(successGuard, 2,
		NewTask(successGuard, newTestTaskCreator("f", TaskFail)),
		NewTask(successGuard, newTestTaskCreator("s1", TaskSuccess)),
		NewTask(successGuard, newTestTaskCreator("s2", TaskSuccess)),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(sel)
	assert.Equal(t, TaskSuccess, r.Execute(ctx))
}

func TestSelectorN_NotEnoughSuccesses(t *testing.T) {
	ctx := newTestCtx()
	sel := NewSelectorN(successGuard, 2,
		NewTask(successGuard, newTestTaskCreator("f1", TaskFail)),
		NewTask(successGuard, newTestTaskCreator("s", TaskSuccess)),
		NewTask(successGuard, newTestTaskCreator("f2", TaskFail)),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(sel)
	assert.Equal(t, TaskFail, r.Execute(ctx))
}

// Contract: a leaf's Execute must return Running(>0) / TaskSuccess / TaskFail,
// never TaskNew(0) — the engine reads TaskNew as "a child was pushed". task
// defensively converts an illegal TaskNew into TaskFail, so a misbehaving leaf is
// created once and fails its own subtree (and still gets OnComplete) instead of
// re-spinning the engine forever.
func TestLeaf_ReturningTaskNewFailsDefensively(t *testing.T) {
	ctx := newTestCtx()
	creates, completed := 0, 0
	creator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		creates++
		return &funcLeaf{
			exec:       func() TaskStatus { return TaskNew }, // illegal leaf return
			onComplete: func(_ bool) { completed++ },
		}, true
	}
	var r Root[*testCtx, *testEvent]
	r.SetNode(NewTask(successGuard, creator))

	assert.Equal(t, TaskFail, r.Execute(ctx), "illegal TaskNew return is converted to failure")
	assert.Equal(t, 1, creates, "leaf is created once, not re-spun")
	assert.Equal(t, 1, completed, "leaf still receives OnComplete (no leak)")
}

// A creator returning (nil, true) must not panic; task treats a nil leaf as a
// creation failure.
func TestTask_NilLeafFromCreatorFailsWithoutPanic(t *testing.T) {
	ctx := newTestCtx()
	creator := func(_ *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		return nil, true // contract violation: nil leaf reported as success
	}
	var r Root[*testCtx, *testEvent]
	r.SetNode(NewTask(successGuard, creator))
	assert.NotPanics(t, func() {
		assert.Equal(t, TaskFail, r.Execute(ctx))
	})
}

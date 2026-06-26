package bt

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/legamerdc/game/lib"
	"github.com/stretchr/testify/assert"
)

func boolGuard(key string) Guard[*testCtx] {
	return func(c *testCtx) bool {
		v, ok := c.Get(key)
		if !ok {
			return false
		}
		b, _ := v.Bool()
		return b
	}
}

// A higher-priority reactive branch preempts a running lower-priority branch
// once its condition becomes true.
func TestReactiveSelector_Preempt(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("g", lib.Bool(false))

	var high, low *testTask
	highCreator := func(c *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		high = &testTask{name: "high", result: TaskStatus(7)}
		return high, true
	}
	lowCreator := func(c *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		low = &testTask{name: "low", result: TaskStatus(10)}
		return low, true
	}

	tree := NewReactiveSelector(successGuard,
		NewSequence(successGuard,
			NewGuard[*testCtx, *testEvent](boolGuard("g")),
			NewTask(successGuard, highCreator),
		),
		NewTask(successGuard, lowCreator),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	// g=false -> high branch fails its guard -> low runs.
	assert.Equal(t, TaskStatus(10), r.Execute(ctx))
	assert.NotNil(t, low)
	assert.True(t, low.executed)
	assert.Nil(t, high)

	// g becomes true -> next tick re-evaluates from the top and preempts low.
	ctx.Set("g", lib.Bool(true))
	assert.Equal(t, TaskStatus(7), r.Execute(ctx))
	assert.NotNil(t, high)
	assert.True(t, high.executed)
	assert.True(t, low.canceled, "low must be aborted when high preempts")
}

// A reactive sequence aborts the running action when an earlier condition child
// stops being satisfied.
func TestReactiveSequence_AbortOnConditionLost(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("ok", lib.Bool(true))

	var act *testTask
	actCreator := func(c *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		act = &testTask{name: "act", result: TaskStatus(5)}
		return act, true
	}

	tree := NewReactiveSequence(successGuard,
		NewGuard[*testCtx, *testEvent](boolGuard("ok")),
		NewTask(successGuard, actCreator),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(5), r.Execute(ctx))
	assert.NotNil(t, act)
	assert.True(t, act.executed)

	ctx.Set("ok", lib.Bool(false))
	assert.Equal(t, TaskFail, r.Execute(ctx))
	assert.True(t, act.canceled, "running action must be aborted when guard re-check fails")
}

// A reactive branch forwards events to its running child and settles when the
// child completes via the event.
func TestReactiveSelector_EventInterrupt(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("g", lib.Bool(false))

	tree := NewReactiveSelector(successGuard,
		NewSequence(successGuard,
			NewGuard[*testCtx, *testEvent](boolGuard("g")),
			NewTask(successGuard, newTestTaskCreator("high", TaskSuccess)),
		),
		NewTask(successGuard, newInterruptibleWaitTaskCreator(10, 1)),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(10), r.Execute(ctx))
	assert.Equal(t, TaskSuccess, r.OnEvent(ctx, &testEvent{kind: 1}))

	interrupted, ok := ctx.Get("interrupted")
	assert.True(t, ok)
	v, _ := interrupted.Bool()
	assert.True(t, v)
}

// failFast: when reaching successRequire becomes impossible, fail immediately
// and cancel the still-running siblings.
func TestParallel_FailFast(t *testing.T) {
	ctx := newTestCtx()

	var r1, r2 *testTask
	failCreator := newTestTaskCreator("fail", TaskFail)
	run1 := func(c *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		r1 = &testTask{name: "r1", result: TaskStatus(5)}
		return r1, true
	}
	run2 := func(c *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		r2 = &testTask{name: "r2", result: TaskStatus(5)}
		return r2, true
	}

	par := NewParallel(successGuard, 3, 0, true, // need 3 successes, no fail threshold
		NewTask(successGuard, failCreator), // one fails -> only 2 can succeed
		NewTask(successGuard, run1),
		NewTask(successGuard, run2),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(par)

	assert.Equal(t, TaskFail, r.Execute(ctx))
	assert.True(t, r1.canceled, "running sibling cancelled on fail-fast")
	assert.True(t, r2.canceled)
}

// Without failFast the same configuration keeps the survivors running.
func TestParallel_NoFailFastKeepsRunning(t *testing.T) {
	ctx := newTestCtx()
	run := func(c *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		return &testTask{name: "run", result: TaskStatus(5)}, true
	}
	par := NewParallel(successGuard, 3, 0, false,
		NewTask(successGuard, newTestTaskCreator("fail", TaskFail)),
		NewTask(successGuard, run),
		NewTask(successGuard, run),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(par)
	assert.Equal(t, TaskStatus(5), r.Execute(ctx))
}

// failRequire=0 disables the independent failure threshold: a failed child does
// not trigger an early failure; the parallel only fails when all complete (or,
// with failFast, when success becomes unreachable).
func TestParallel_FailRequireDisabled(t *testing.T) {
	ctx := newTestCtx()
	run := func(c *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		return &testTask{name: "run", result: TaskStatus(5)}, true
	}
	par := NewParallel(successGuard, 2, 0, false, // no fail threshold, no failFast
		NewTask(successGuard, newTestTaskCreator("f", TaskFail)),
		NewTask(successGuard, run),
		NewTask(successGuard, run),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(par)
	assert.Equal(t, TaskStatus(5), r.Execute(ctx), "a failed child must not fail the parallel when failRequire=0")
}

// failRequire is an independent failure threshold.
func TestParallel_FailCount(t *testing.T) {
	ctx := newTestCtx()
	par := NewParallel(successGuard, 3, 2, false, // succeed at 3, but fail at 2 failures
		NewTask(successGuard, newTestTaskCreator("f1", TaskFail)),
		NewTask(successGuard, newTestTaskCreator("f2", TaskFail)),
		NewTask(successGuard, newTestTaskCreator("s1", TaskSuccess)),
		NewTask(successGuard, newTestTaskCreator("s2", TaskSuccess)),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(par)
	assert.Equal(t, TaskFail, r.Execute(ctx))
}

// failFast equality boundary: success + running == successRequire is still
// achievable, so the parallel must keep running (locks the strict `<`, not `<=`).
func TestParallel_FailFastEqualityKeepsRunning(t *testing.T) {
	ctx := newTestCtx()
	run := func(c *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		return &testTask{name: "run", result: TaskStatus(5)}, true
	}
	// successRequire=2: one success + one still running => 1+1 == 2 (achievable).
	par := NewParallel(successGuard, 2, 0, true,
		NewTask(successGuard, newTestTaskCreator("s", TaskSuccess)),
		NewTask(successGuard, run),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(par)
	assert.Equal(t, TaskStatus(5), r.Execute(ctx), "must keep running at the equality boundary")
}

// A ReactiveSequence whose active child completes via an event advances to the
// next child and updates the active index to it.
func TestReactiveSequence_EventAdvancesActive(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("ok", lib.Bool(true))

	var b *testTask
	bCreator := func(c *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		b = &testTask{name: "b", result: TaskStatus(5)}
		return b, true
	}
	tree := NewReactiveSequence(successGuard,
		NewGuard[*testCtx, *testEvent](boolGuard("ok")),
		NewTask(successGuard, newInterruptibleWaitTaskCreator(10, 1)), // waitA: kind 1 -> success
		NewTask(successGuard, bCreator),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(10), r.Execute(ctx)) // waitA running (active=1), b not started
	assert.Nil(t, b)

	// Event completes waitA -> sequence advances to b which becomes running.
	assert.Equal(t, TaskStatus(5), r.OnEvent(ctx, &testEvent{kind: 1}))
	assert.NotNil(t, b)
	assert.True(t, b.executed)
}

// A ReactiveSelector whose active child fails via an event continues scanning
// to the next child.
func TestReactiveSelector_EventFailAdvances(t *testing.T) {
	ctx := newTestCtx()

	var low *testTask
	highCreator := func(c *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		return &testTask{name: "high", result: TaskStatus(10), eventResult: TaskFail}, true
	}
	lowCreator := func(c *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
		low = &testTask{name: "low", result: TaskStatus(5)}
		return low, true
	}
	tree := NewReactiveSelector(successGuard,
		NewTask(successGuard, highCreator),
		NewTask(successGuard, lowCreator),
	)
	var r Root[*testCtx, *testEvent]
	r.SetNode(tree)

	assert.Equal(t, TaskStatus(10), r.Execute(ctx)) // high running (active=0), low not started
	assert.Nil(t, low)

	// Event fails high -> selector advances to low which becomes running.
	assert.Equal(t, TaskStatus(5), r.OnEvent(ctx, &testEvent{kind: 9}))
	assert.NotNil(t, low)
	assert.True(t, low.executed)
}

// Stochastic ordering is reproducible when the same seeded Rand is injected.
func TestStochastic_DeterministicWithSeed(t *testing.T) {
	run := func(seed uint64) string {
		ctx := newTestCtx()
		var order []string
		mk := func(name string) TaskCreator[*testCtx, *testEvent] {
			return func(c *testCtx) (LeafTaskI[*testCtx, *testEvent], bool) {
				order = append(order, name)
				return &testTask{name: name, result: TaskFail}, true
			}
		}
		rng := rand.New(rand.NewPCG(seed, seed))
		tree := NewStochasticSelector(successGuard, rng,
			NewTask(successGuard, mk("a")),
			NewTask(successGuard, mk("b")),
			NewTask(successGuard, mk("c")),
			NewTask(successGuard, mk("d")),
		)
		var r Root[*testCtx, *testEvent]
		r.SetNode(tree)
		r.Execute(ctx)
		return strings.Join(order, ",")
	}
	assert.Equal(t, run(42), run(42), "same seed must yield the same visit order")
	// All four children are visited (all fail), in some order.
	assert.Len(t, strings.Split(run(7), ","), 4)
}

package sched

import (
	"slices"
	"testing"
)

func TestTimerWheelSetUsesGlobalBlockID(t *testing.T) {
	tw := newTimerWheel[string](8, 4, [][]int{
		{1, 3},
		{0, 2},
	})

	tw.set(0, 3, "a", 1)
	tw.set(1, 2, "b", 1)
	tw.merge()

	if got := tw.get(3); len(got) != 0 {
		t.Fatalf("current slot block 3 timers = %v, want empty before advance", got)
	}
	if got := tw.get(2); len(got) != 0 {
		t.Fatalf("current slot block 2 timers = %v, want empty before advance", got)
	}

	tw.advance()

	if got := tw.get(3); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("block 3 timers = %v, want [a]", got)
	}
	if got := tw.get(2); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("block 2 timers = %v, want [b]", got)
	}
	if got := tw.get(1); len(got) != 0 {
		t.Fatalf("block 1 timers = %v, want empty", got)
	}
	if got := tw.get(0); len(got) != 0 {
		t.Fatalf("block 0 timers = %v, want empty", got)
	}
}

func TestTimerWheelClampDelayToLastSlot(t *testing.T) {
	tw := newTimerWheel[string](4, 2, [][]int{
		{0, 1},
	})

	tw.set(0, 1, "far", 10)
	tw.merge()

	for i := range 3 {
		if got := tw.get(1); len(got) != 0 {
			t.Fatalf("tick %d block 1 timers = %v, want empty before last slot", i, got)
		}
		tw.advance()
	}

	if got := tw.get(1); !slices.Equal(got, []string{"far"}) {
		t.Fatalf("last slot timers = %v, want [far]", got)
	}
}

func TestTimerWheelCancelBeforeMergeOnlyAffectsThreadLocalBuffer(t *testing.T) {
	tw := newTimerWheel[string](8, 2, [][]int{
		{0, 1},
	})

	tw.set(0, 1, "cancelled", 1)
	tw.set(0, 1, "cancelled", 0)
	tw.merge()
	tw.advance()

	if got := tw.get(1); len(got) != 0 {
		t.Fatalf("cancelled timer still triggered after pre-merge cancel: %v", got)
	}
}

func TestTimerWheelCancelDoesNotRemoveMergedTimer(t *testing.T) {
	tw := newTimerWheel[string](8, 2, [][]int{
		{0, 1},
	})

	tw.set(0, 1, "persist", 1)
	tw.merge()

	tw.set(0, 1, "persist", 0)
	tw.advance()

	if got := tw.get(1); !slices.Equal(got, []string{"persist"}) {
		t.Fatalf("merged timer should still trigger after local cancel, got %v", got)
	}
}

func TestTimerWheelAdvanceClearsCurrentSlotAndThreadLocalBuffer(t *testing.T) {
	tw := newTimerWheel[string](8, 2, [][]int{
		{0, 1},
	})

	tw.set(0, 0, "now", 0)
	tw.set(0, 1, "next", 1)
	tw.merge()

	if got := tw.get(0); len(got) != 0 {
		t.Fatalf("delay=0 should not register timer, got %v", got)
	}
	if got := tw.get(1); len(got) != 0 {
		t.Fatalf("current slot unexpectedly contains future timer: %v", got)
	}

	tw.advance()

	if got := tw.get(1); !slices.Equal(got, []string{"next"}) {
		t.Fatalf("next slot timers = %v, want [next]", got)
	}

	tw.advance()

	if got := tw.get(1); len(got) != 0 {
		t.Fatalf("slot should be cleared after advance, got %v", got)
	}
}

func TestTimerWheelDedupWithinSameSlotAndBlock(t *testing.T) {
	tw := newTimerWheel[string](8, 2, [][]int{
		{1},
		{1},
	})

	tw.set(0, 1, "dup", 1)
	tw.set(1, 1, "dup", 1)
	tw.merge()
	tw.advance()

	if got := tw.get(1); !slices.Equal(got, []string{"dup"}) {
		t.Fatalf("dedup result = %v, want [dup]", got)
	}
}

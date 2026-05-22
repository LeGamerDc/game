package scenario

import (
	"fmt"
	"testing"

	"github.com/legamerdc/game/demo/combat"
	"github.com/legamerdc/game/sched"
)

func BenchmarkGridCombatScheduler(b *testing.B) {
	maxInt := int(^uint(0) >> 1)
	sizes := []int{16, 32, 84}
	modes := []struct {
		name string
		meta sched.ScheduleMeta
	}{
		{
			name: "serial",
			meta: sched.ScheduleMeta{
				ThinkConcurrencyThreshold: maxInt,
				Concurrency:               1,
			},
		},
		{
			name: "parallel-4",
			meta: sched.ScheduleMeta{
				ThinkConcurrencyThreshold: 1,
				Concurrency:               4,
			},
		},
		{
			name: "parallel-8",
			meta: sched.ScheduleMeta{
				ThinkConcurrencyThreshold: 1,
				Concurrency:               8,
			},
		},
	}

	for _, size := range sizes {
		for _, mode := range modes {
			b.Run(fmt.Sprintf("%dx%d/%s", size, size, mode.name), func(b *testing.B) {
				w := newBenchmarkWorld(size, mode.meta)
				w.StepN(24)

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					w.Step()
				}
				b.StopTimer()
				b.ReportMetric(float64(size*size), "units/tick")

				requireBenchmarkCombatStillActive(b, w)
			})
		}
	}
}

func newBenchmarkWorld(size int, meta sched.ScheduleMeta) *combat.World {
	w := NewGridWorld(GridConfig{
		Size:    size,
		Spacing: 1,
		Meta:    meta,
		Stats: combat.UnitStats{
			Hp:              1_000_000_000,
			Mana:            1_000_000,
			Attack:          0.4,
			Defense:         0,
			AttackRange:     4.25,
			AttackSpeed:     4,
			ProjectileSpeed: 64,
			AttackWindupSec: 0.125,
			TargetHoldSec:   2,
			TargetSearchSec: 0.125,
			ReviveDelaySec:  8,
		},
	})
	AddBenchmarkAbilities(w)
	return w
}

func requireBenchmarkCombatStillActive(b *testing.B, w *combat.World) {
	b.Helper()

	damaged := 0
	for _, u := range w.Units() {
		summary, ok := w.UnitSummary(u.Id)
		if !ok {
			b.Fatalf("missing unit summary for ref %d", u.Id)
		}
		if summary.Alive && summary.Hp < summary.MaxHp {
			damaged++
		}
	}
	if damaged == 0 {
		b.Fatal("benchmark scenario produced no damage")
	}
}

package main

import (
	"github.com/legamerdc/game/blackboard"
	"github.com/legamerdc/game/calc"
	"testing"
)

func BenchmarkCalc(b *testing.B) {
	kv := Kv{m: make(map[string]blackboard.Field)}
	kv.m["power_x"] = blackboard.Int64(3000)
	kv.m["power_y"] = blackboard.Int64(3000)
	f, _ := calc.Compile[*Kv](test)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = f(&kv)
	}
}

func code(m *Kv) {
	vx, ox := m.Get("power_x")
	vy, oy := m.Get("power_y")
	if ox && oy {
		fx, o1 := vx.Float64()
		fy, o2 := vy.Float64()
		if o1 && o2 {
			m.SetFloat64("power", fx*0.95+fy*1.25)
		}
	}
}

func BenchmarkCode(b *testing.B) {
	kv := Kv{m: make(map[string]blackboard.Field)}
	kv.m["power_x"] = blackboard.Int64(3000)
	kv.m["power_y"] = blackboard.Int64(3000)
	for i := 0; i < b.N; i++ {
		code(&kv)
	}
}

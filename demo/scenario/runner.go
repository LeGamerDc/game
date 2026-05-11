package scenario

import "github.com/legamerdc/game/demo/combat"

func RunTicks(w *combat.World, ticks int) {
	w.StepN(ticks)
}

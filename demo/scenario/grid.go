package scenario

import (
	"github.com/legamerdc/game/demo/combat"
	"github.com/legamerdc/game/sched"
)

type GridConfig struct {
	Size    int
	Spacing float64
	Stats   combat.UnitStats
	Meta    sched.ScheduleMeta
}

func NewGridWorld(cfg GridConfig) *combat.World {
	if cfg.Size <= 0 {
		cfg.Size = 1
	}
	if cfg.Spacing <= 0 {
		cfg.Spacing = 1
	}
	w := combat.NewWorld(cfg.Meta)
	for y := range cfg.Size {
		for x := range cfg.Size {
			team := combat.Team((x + y) % 2)
			pos := combat.Vec2{X: float64(x) * cfg.Spacing, Y: float64(y) * cfg.Spacing}
			w.AddUnit(combat.NewUnit(team, pos, cfg.Stats))
		}
	}
	return w
}

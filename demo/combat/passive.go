package combat

import "github.com/legamerdc/game/sched"

type PassiveTrigger struct {
	On          sched.SignalKind
	QueueSlot   int
	Chance      float64
	CooldownSec float64

	nextReadyAtSec float64
}

func (a *AbilitySystem) HandleSignal(u *Unit, sig Signal, nowSec float64) {
	for i := range a.Passives {
		trigger := &a.Passives[i]
		if trigger.On != sig.K {
			continue
		}
		if trigger.nextReadyAtSec > nowSec {
			continue
		}
		if trigger.Chance > 0 && trigger.Chance < 1 && u.rng.Float64() >= trigger.Chance {
			continue
		}
		if trigger.Chance <= 0 {
			continue
		}
		if a.QueueSlot(trigger.QueueSlot) && trigger.CooldownSec > 0 {
			trigger.nextReadyAtSec = nowSec + trigger.CooldownSec
		}
	}
}

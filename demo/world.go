package demo

import "github.com/legamerdc/game/sched"

var _ sched.World = (*World)(nil)

type (
	World struct {
		now     int64
		version uint32
		round   int32
	}

	Signal struct{}

	Effect struct{}
)

func (s *Signal) Kind() sched.SignalKind { return 0 }

func (s *Signal) Order() int32 { return 0 }

func (e *Effect) Kind() sched.EffectKind { return 0 }

func (e *Effect) Order() int32 { return 0 }

func (w *World) Now() int64 {
	return w.now
}

func (w *World) Version() uint32 {
	return w.version
}

func (w *World) Round() int32 {
	return w.round
}

func (w *World) PromoteStages(i sched.Inbox[sched.RefStage]) {}

func (w *World) GetLogic(uint64) (*GAS, bool) {

}

func Init() {
	var w World
	sc := sched.NewScheduler(sched.ScheduleMeta{}, &w)
	sc.ProcessTick()
}

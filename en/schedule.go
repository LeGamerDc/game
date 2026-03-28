package engine

type (
	Scheduler[W WorldView, S SignalI, E EffectI, L Logic[W, S, E]] struct {
	}
)

func (s *Scheduler[W, S, E, L]) think() {

}

func (s *Scheduler[W, S, E, L]) apply() {}

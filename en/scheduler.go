package engine

type (
	ScheduleMeta struct {
		ThinkConcurrencyThreshold int
		Concurrency               int // default 5
		MaxSupersteps             int
		TimerWheelSize            int
		BlockSize                 int // default Concurrency * 10
	}

	Scheduler[W WorldView, S SignalI, E EffectI, L Logic[W, S, E]] struct {
		meta ScheduleMeta

		// data
		timerWheel *timerWheel[uint64]
		// cache
		effectCollector []*blockCollector[E] // threadId -> blockId -> []E
		signalCollector []*blockCollector[S] // threadId -> blockId -> []S
	}
)

func (s *Scheduler[W, S, E, L]) publishFunc(threadId int32) func(uint64, E) {
	collector := s.effectCollector[threadId]
	blockSize := uint64(s.meta.BlockSize)
	return func(refId uint64, e E) {
		collector.push(int(hash(refId, blockSize)), e)
	}
}

func (s *Scheduler[W, S, E, L]) emitFunc(threadId int32) func(uint64, S) {
	collector := s.signalCollector[threadId]
	blockSize := uint64(s.meta.BlockSize)
	return func(refId uint64, e S) {
		collector.push(int(hash(refId, blockSize)), e)
	}
}

func (s *Scheduler[W, S, E, L]) think() {}

func (s *Scheduler[W, S, E, L]) apply() {}

// https://gist.github.com/badboy/6267743
func hash(x, h uint64) uint64 {
	x = (^x) + (x << 18)
	x = x ^ (x >> 31)
	x = x * 21
	x = x ^ (x >> 11)
	x = x + (x << 6)
	x = x ^ (x >> 22)
	return x % h
}

package engine

type (
	EffectKind int32
	SignalKind int32
)

const RefWorld uint64 = 1 << 63

func IsWorldRef(r uint64) bool { return r == RefWorld }

func IsSerialRef(r uint64) bool { return r >= RefWorld }

func IsNormalRef(r uint64) bool { return r < RefWorld }

type (
	WorldView interface {
		Now() int64
	}

	SignalI interface {
		Kind() SignalKind
	}

	EffectI interface {
		Kind() EffectKind
	}

	Inbox[S SignalI] interface {
		Len() int
		At(i int) S
	}

	Arrangement[E EffectI] interface {
		Len() int
		At(i int) E
	}

	// ThinkCtx intentionally exposes only read access to world state plus
	// targeted effect/signal outputs. Public/entity/world writes must go
	// through effect commit.
	ThinkCtx[W WorldView, S SignalI, E EffectI] struct {
		World   W
		Emit    func(uint64, S)
		Publish func(uint64, E)
	}

	// CommitCtx is used by owner-local reducers after effects are bucketed
	// by target ref. Reducers may mutate only their own authoritative state.
	CommitCtx[W WorldView, S SignalI] struct {
		World W
		Emit  func(uint64, S)
	}

	Logic[W WorldView, S SignalI, E EffectI] interface {
		ID() uint64
		// Think returns the next self wakeup interval in ticks.
		// A non-positive result means no automatic reschedule.
		Think(*ThinkCtx[W, S, E], Inbox[S]) int64
		Apply(*CommitCtx[W, S], Arrangement[E])
	}
)

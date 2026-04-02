package sched

type (
	EffectKind int32
	SignalKind int32
)

const (
	RefWorld uint64 = 1 << 63
	RefNone  uint64 = 0
)

func IsWorldRef(r uint64) bool { return r == RefWorld }

func IsSerialRef(r uint64) bool { return r >= RefWorld }

func IsNormalRef(r uint64) bool { return r < RefWorld }

func IsValidRef(r uint64) bool { return r != RefNone }

type (
	WatchState interface {
		Interest(SignalKind) bool
	}

	WorldView[WS WatchState] interface {
		Now() int64
		Version() uint32
		Round() int32

		WatchOf(uint64) WS
	}

	World[WS WatchState] interface {
		WorldView[WS]

		GetWorldView() WorldView[WS]
	}

	SignalI interface {
		Kind() SignalKind
		Order() int32 // 同 ref 内排序键，小值优先。不需要排序时返回 0。
	}

	EffectI interface {
		Kind() EffectKind
		Order() int32 // 同 ref 内排序键，小值优先。不需要排序时返回 0。
	}

	Inbox[S any] interface {
		Len() int
		At(i int) S
	}

	// ThinkCtx intentionally exposes only read access to world state plus
	// targeted effect/signal outputs. Public/entity/world writes must go
	// through effect commit.
	ThinkCtx[W World[WS], S SignalI, E EffectI, WS WatchState] struct {
		World    W
		Emit     func(uint64, S)
		Publish  func(uint64, E)
		SetWatch func(WS) // Declare signal interest; applied at Think barrier (parallel) or immediately (serial).
	}

	// CommitCtx is used by owner-local reducers after effects are bucketed
	// by target ref. Reducers may mutate only their own authoritative state.
	CommitCtx[W WorldView[WS], S SignalI, WS WatchState] struct {
		World W
		Emit  func(uint64, S)
	}

	Logic[W World[WS], S SignalI, E EffectI, WS WatchState] interface {
		ID() uint64
		// Think returns the next self wakeup interval in ticks.
		// A non-positive result means no automatic reschedule.
		Think(*ThinkCtx[W, S, E, WS], Inbox[S]) int64
		Apply(*CommitCtx[WorldView[WS], S, WS], Inbox[E])
	}

	LogicProvider[L any] interface {
		GetLogic(uint64) (L, bool)
	}

	RefWatch[WS WatchState] struct {
		RefId uint64
		WS    WS
	}

	WatchCommitter[WS WatchState] interface {
		CommitWatches(Inbox[RefWatch[WS]])
	}
)

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
	StageKind int32

	StagedState any

	World interface {
		Now() int64
		Version() uint32
		Round() int32
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
	ThinkCtx[W World, S SignalI, E EffectI] struct {
		World      W
		Emit       func(uint64, S)
		Publish    func(uint64, E)
		WriteStage func(StageKind, StagedState)
	}

	// CommitCtx is used by owner-local reducers after effects are bucketed
	// by target ref. Reducers may mutate only their own authoritative state.
	CommitCtx[W World, S SignalI] struct {
		World      W
		Emit       func(uint64, S)
		WriteStage func(StageKind, StagedState)
	}

	Logic[W World, S SignalI, E EffectI] interface {
		ID() uint64
		// Think returns the next self wakeup interval in ticks.
		// A non-positive result means no automatic reschedule.
		Think(*ThinkCtx[W, S, E], Inbox[S]) int64
		Apply(*CommitCtx[W, S], Inbox[E])
	}

	LogicProvider[L any] interface {
		GetLogic(uint64) (L, bool)
	}

	RefStage struct {
		RefId uint64
		Kind  StageKind
		State StagedState
	}

	StagePromoter interface {
		PromoteStages(Inbox[RefStage])
	}
)

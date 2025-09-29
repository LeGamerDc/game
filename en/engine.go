package engine

import "github.com/legamerdc/game/lib"

const (
	EventKindGameEvent EventKind = iota
	EventKindGEUpdate
	EventKindTagUpdate
	EventKindCast
)

type (
	EventKind int32
	WI        interface {
		Now() int64
	}

	UI interface{}

	EI interface {
		Kind() EventKind
		Id() int32
	}

	Running[W WI, U UI] interface {
		Id() int32
		Update(W, U, *GAS[W, U])
		OnEvent(W, U, EI)
	}

	GAS[W WI, U UI] struct {
		Runnings lib.HeapIndexMap[int32, int64, Running[W, U]]
		Watcher  EventWatcher[W, U]
	}

	EventWatcher[W WI, U UI] struct {
		EventMap     map[int32]*lib.ArrayMap[int64, Listener[W, U]] // EventId
		GEUpdateMap  map[int32]*lib.ArrayMap[int64, Listener[W, U]] // GeId
		TagUpdateMap map[int32]*lib.ArrayMap[int64, Listener[W, U]] // TagId
		CastMap      map[int32]*lib.ArrayMap[int64, Listener[W, U]] // SkillId
	}

	Listener[W WI, U UI] struct {
		Id       int64
		Listener func(W, U, EI)
	}
)

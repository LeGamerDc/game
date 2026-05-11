package combat

import (
	"math"
	"slices"

	"github.com/legamerdc/game/sched"
)

var (
	_ sched.World                         = (*World)(nil)
	_ sched.LogicProvider[*Unit]          = (*World)(nil)
	_ sched.StagePromoter                 = (*World)(nil)
	_ sched.Logic[*World, Signal, Effect] = (*Unit)(nil)
)

type World struct {
	now     int64
	version uint32
	round   int32

	nextRef uint64
	units   map[uint64]*Unit

	scheduler *sched.Scheduler[*World, Signal, Effect, *Unit]

	unitSummaries map[uint64]UnitSummary
	attrSummaries map[uint64]AttrSummary
	cellByRef     map[uint64]cellKey
	refsByCell    map[cellKey][]uint64
}

func NewWorld(meta sched.ScheduleMeta) *World {
	w := &World{
		nextRef:       1,
		units:         make(map[uint64]*Unit),
		unitSummaries: make(map[uint64]UnitSummary),
		attrSummaries: make(map[uint64]AttrSummary),
		cellByRef:     make(map[uint64]cellKey),
		refsByCell:    make(map[cellKey][]uint64),
	}
	w.scheduler = sched.NewScheduler[*World, Signal, Effect, *Unit](meta, w)
	return w
}

func (w *World) Now() int64 { return w.now }

func (w *World) NowSec() float64 { return TickToSeconds(w.now) }

func (w *World) Version() uint32 { return w.version }

func (w *World) Round() int32 { return w.round }

func (w *World) Step() {
	w.scheduler.ProcessTick()
	w.now++
	w.version++
	w.round = 0
}

func (w *World) StepN(n int) {
	for range n {
		w.Step()
	}
}

func (w *World) Emit(ref uint64, sig Signal) {
	w.scheduler.Emit(ref, sig)
}

func (w *World) AddUnit(u *Unit) uint64 {
	if u.Id == 0 {
		u.Id = w.nextRef
		w.nextRef++
	} else if u.Id >= w.nextRef {
		w.nextRef = u.Id + 1
	}
	u.initRuntime()
	w.units[u.Id] = u
	w.promoteUnitSummary(u.UnitSummary())
	w.attrSummaries[u.Id] = u.AttrSummary()
	w.scheduler.Emit(u.Id, Signal{K: SignalExternalStart, AtSec: w.NowSec()})
	return u.Id
}

func (w *World) Unit(ref uint64) (*Unit, bool) {
	u, ok := w.units[ref]
	return u, ok
}

func (w *World) Units() []*Unit {
	out := make([]*Unit, 0, len(w.units))
	for _, u := range w.units {
		out = append(out, u)
	}
	slices.SortFunc(out, func(a, b *Unit) int {
		if a.Id < b.Id {
			return -1
		}
		if a.Id > b.Id {
			return 1
		}
		return 0
	})
	return out
}

func (w *World) GetLogic(ref uint64) (*Unit, bool) {
	u, ok := w.units[ref]
	return u, ok
}

func (w *World) PromoteStages(inbox sched.Inbox[sched.RefStage]) {
	for i := range inbox.Len() {
		stage := inbox.At(i)
		switch stage.Kind {
		case StageUnitSummary:
			if summary, ok := stage.State.(UnitSummary); ok {
				w.promoteUnitSummary(summary)
			}
		case StageAttrSummary:
			if summary, ok := stage.State.(AttrSummary); ok {
				w.attrSummaries[stage.RefId] = summary
			}
		}
	}
}

func (w *World) UnitSummary(ref uint64) (UnitSummary, bool) {
	s, ok := w.unitSummaries[ref]
	return s, ok
}

func (w *World) AttrSummary(ref uint64) (AttrSummary, bool) {
	s, ok := w.attrSummaries[ref]
	return s, ok
}

func (w *World) QueryEnemiesInRange(sourceRef uint64, pos Vec2, team Team, radius float64) []UnitSummary {
	if radius <= 0 {
		return nil
	}

	minX := int(math.Floor(pos.X - radius))
	maxX := int(math.Floor(pos.X + radius))
	minY := int(math.Floor(pos.Y - radius))
	maxY := int(math.Floor(pos.Y + radius))
	radiusSq := radius * radius

	out := make([]UnitSummary, 0, 8)
	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			for _, ref := range w.refsByCell[cellKey{X: x, Y: y}] {
				if ref == sourceRef {
					continue
				}
				s := w.unitSummaries[ref]
				if !s.Alive || !s.Targetable || s.Team == team {
					continue
				}
				if pos.DistanceSquared(s.Pos) <= radiusSq {
					out = append(out, s)
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b UnitSummary) int {
		if a.Ref < b.Ref {
			return -1
		}
		if a.Ref > b.Ref {
			return 1
		}
		return 0
	})
	return out
}

func (w *World) promoteUnitSummary(summary UnitSummary) {
	if oldCell, ok := w.cellByRef[summary.Ref]; ok {
		newCell := cellFor(summary.Pos)
		if oldCell != newCell {
			w.removeFromCell(oldCell, summary.Ref)
			w.refsByCell[newCell] = append(w.refsByCell[newCell], summary.Ref)
			w.cellByRef[summary.Ref] = newCell
		}
	} else {
		cell := cellFor(summary.Pos)
		w.refsByCell[cell] = append(w.refsByCell[cell], summary.Ref)
		w.cellByRef[summary.Ref] = cell
	}
	w.unitSummaries[summary.Ref] = summary
}

func (w *World) removeFromCell(cell cellKey, ref uint64) {
	refs := w.refsByCell[cell]
	for i, r := range refs {
		if r == ref {
			copy(refs[i:], refs[i+1:])
			refs = refs[:len(refs)-1]
			break
		}
	}
	if len(refs) == 0 {
		delete(w.refsByCell, cell)
		return
	}
	w.refsByCell[cell] = refs
}

type cellKey struct {
	X, Y int
}

func cellFor(pos Vec2) cellKey {
	return cellKey{X: int(math.Floor(pos.X)), Y: int(math.Floor(pos.Y))}
}

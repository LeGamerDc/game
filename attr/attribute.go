package attr

import "github.com/legamerdc/game/lib"

// Key encodes a generated attribute set id and field id.
type Key = uint32

// Value holds the persistent base and computed current value of an attribute.
// Base is changed by owner-local logic. Current is derived from Base plus the
// active Modifier list managed by Table.
type Value struct {
	Base, Current float64
}

// KeySetID extracts the set id from a global attribute key.
func KeySetID(key Key) uint32 { return key >> 16 }

// KeyField extracts the field id from a global attribute key.
func KeyField(key Key) uint16 { return uint16(key & 0xFFFF) }

// MakeKey constructs a global attribute key from a set id and field id.
func MakeKey(setID uint32, field uint16) Key {
	return (setID << 16) | uint32(field)
}

// AttributeSet is implemented by generated attribute set structs.
//
// The dynamic accessors are intended for generic systems such as modifier
// aggregation, expression evaluation and staged public-state projection. Hot
// gameplay code can use generated typed accessors directly.
type AttributeSet interface {
	SetID() uint32
	FieldCount() uint16

	GetCurrent(field uint16) (float64, bool)
	GetBase(field uint16) (float64, bool)

	SetBase(field uint16, v float64) bool
	SetCurrent(field uint16, v float64) bool

	Dirty() uint64
	ClearDirty()
}

// Hooks is an optional interface for generated or hand-written AttributeSets.
// It mirrors the useful, non-business-specific part of Unreal GAS's attribute
// hook pipeline: clamp or normalize values before they are written, and observe
// numerical changes after they have happened. Gameplay consequences such as
// death, rewards, or signal emission should stay in the owning Logic.
type Hooks interface {
	PreBaseChange(field uint16, next float64) float64
	PreCurrentChange(field uint16, next float64) float64
	PostCurrentChange(field uint16, old, next float64)
}

// Map stores AttributeSets keyed by SetID.
type Map struct {
	sets lib.ArrayMap[uint32, AttributeSet]
}

func (m *Map) Len() int { return m.sets.Len() }

func (m *Map) Get(id uint32) AttributeSet {
	if i, v := m.sets.Get(id); i >= 0 {
		return v
	}
	return nil
}

func (m *Map) Put(set AttributeSet) {
	id := set.SetID()
	if _, p := m.sets.GetP(id); p != nil {
		*p = set
		return
	}
	m.sets.Put(id, set)
}

func (m *Map) Remove(id uint32) bool {
	if i, _ := m.sets.Get(id); i >= 0 {
		m.sets.Remove(i)
		return true
	}
	return false
}

func (m *Map) Reserve(n int) { m.sets.Reserve(n) }

func (m *Map) Clear() { m.sets.Clear() }

func (m *Map) GetBase(key Key) (float64, bool) {
	set := m.Get(KeySetID(key))
	if set == nil {
		return 0, false
	}
	return set.GetBase(KeyField(key))
}

func (m *Map) GetCurrent(key Key) (float64, bool) {
	set := m.Get(KeySetID(key))
	if set == nil {
		return 0, false
	}
	return set.GetCurrent(KeyField(key))
}

func (m *Map) SetBase(key Key, v float64) bool {
	set := m.Get(KeySetID(key))
	if set == nil {
		return false
	}
	field := KeyField(key)
	if hooks, ok := set.(Hooks); ok {
		v = hooks.PreBaseChange(field, v)
	}
	return set.SetBase(field, v)
}

func (m *Map) SetCurrent(key Key, v float64) bool {
	set := m.Get(KeySetID(key))
	if set == nil {
		return false
	}
	field := KeyField(key)
	old, _ := set.GetCurrent(field)
	if hooks, ok := set.(Hooks); ok {
		v = hooks.PreCurrentChange(field, v)
		if old == v {
			return true
		}
		if !set.SetCurrent(field, v) {
			return false
		}
		hooks.PostCurrentChange(field, old, v)
		return true
	}
	return set.SetCurrent(field, v)
}

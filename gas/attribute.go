package gas

import "github.com/legamerdc/game/lib"

// ---------------------------------------------------------------------------
// Value types
// ---------------------------------------------------------------------------

// AttributeValue holds a base value and a computed current value.
// Base is modified by Instant Effects; Current is derived from Base + Modifiers.
type AttributeValue struct {
	Base, Current float64
}

// ---------------------------------------------------------------------------
// AttrKey encoding: upper 16 bits = SetID, lower 16 bits = FieldIndex
// ---------------------------------------------------------------------------

// AttrKeySetID extracts the SetID from a global AttrKey.
func AttrKeySetID(key uint32) uint32 { return key >> 16 }

// AttrKeyField extracts the field index from a global AttrKey.
func AttrKeyField(key uint32) uint16 { return uint16(key & 0xFFFF) }

// MakeAttrKey constructs a global AttrKey from a SetID and field index.
func MakeAttrKey(setID uint32, field uint16) uint32 {
	return (setID << 16) | uint32(field)
}

// ---------------------------------------------------------------------------
// AttributeSet interface
// ---------------------------------------------------------------------------

// AttributeSet is the interface that all generated attribute-set types implement.
// It provides generic (dynamic) field access used by the modifier system,
// expression evaluator and change-notification routing.
//
// For the hot path, callers should use the typed Bind function
// (e.g. GetDemoAttrs) to obtain the concrete *XxxAttributeSet and call
// its typed accessor methods directly.
type AttributeSet interface {
	// SetID returns the unique identifier for this attribute set type.
	SetID() uint32

	// FieldCount returns the total number of fields in this set.
	FieldCount() uint16

	// GetCurrent returns the effective value of a field.
	//   InstantValue  → the value itself
	//   AttributeValue → Current
	GetCurrent(field uint16) (float64, bool)

	// GetBase returns the base value of a field.
	//   InstantValue  → the value itself (same as GetCurrent)
	//   AttributeValue → Base
	GetBase(field uint16) (float64, bool)

	// SetBase sets the base value and marks the field dirty.
	SetBase(field uint16, v float64) bool

	// SetCurrent sets the current value and marks the field dirty.
	SetCurrent(field uint16, v float64) bool

	// Dirty returns the dirty bitmask (one bit per field).
	Dirty() uint64

	// ClearDirty resets the dirty bitmask to zero.
	ClearDirty()
}

// ---------------------------------------------------------------------------
// AttrMap – dynamic array map for AttributeSets
// ---------------------------------------------------------------------------

// AttrMap is a small array-based map that stores AttributeSets keyed by SetID.
// It wraps lib.ArrayMap and adds upsert semantics for Put.
type AttrMap struct {
	sets lib.ArrayMap[uint32, AttributeSet]
}

// Len returns the number of registered sets.
func (m *AttrMap) Len() int { return m.sets.Len() }

// Get returns the AttributeSet with the given SetID, or nil.
func (m *AttrMap) Get(id uint32) AttributeSet {
	if i, v := m.sets.Get(id); i >= 0 {
		return v
	}
	return nil
}

// Put registers (or replaces) an AttributeSet. The SetID is taken from
// set.SetID().
func (m *AttrMap) Put(set AttributeSet) {
	id := set.SetID()
	if _, p := m.sets.GetP(id); p != nil {
		*p = set
		return
	}
	m.sets.Put(id, set)
}

// Remove removes the set with the given id. Returns true if found.
func (m *AttrMap) Remove(id uint32) bool {
	if i, _ := m.sets.Get(id); i >= 0 {
		m.sets.Remove(i)
		return true
	}
	return false
}

// Reserve pre-allocates capacity for n sets.
func (m *AttrMap) Reserve(n int) { m.sets.Reserve(n) }

// Clear removes all registered sets.
func (m *AttrMap) Clear() { m.sets.Clear() }

// ---------------------------------------------------------------------------
// AttrMap – shared attribute access by global AttrKey
// ---------------------------------------------------------------------------

// GetBase reads the base value of an attribute by its global AttrKey.
func (m *AttrMap) GetBase(key uint32) (float64, bool) {
	set := m.Get(AttrKeySetID(key))
	if set == nil {
		return 0, false
	}
	return set.GetBase(AttrKeyField(key))
}

// GetCurrent reads the current (effective) value of an attribute by its global AttrKey.
func (m *AttrMap) GetCurrent(key uint32) (float64, bool) {
	set := m.Get(AttrKeySetID(key))
	if set == nil {
		return 0, false
	}
	return set.GetCurrent(AttrKeyField(key))
}

// SetBase writes the base value of an attribute by its global AttrKey.
func (m *AttrMap) SetBase(key uint32, v float64) bool {
	set := m.Get(AttrKeySetID(key))
	if set == nil {
		return false
	}
	return set.SetBase(AttrKeyField(key), v)
}

// SetCurrent writes the current value of an attribute by its global AttrKey.
func (m *AttrMap) SetCurrent(key uint32, v float64) bool {
	set := m.Get(AttrKeySetID(key))
	if set == nil {
		return false
	}
	return set.SetCurrent(AttrKeyField(key), v)
}

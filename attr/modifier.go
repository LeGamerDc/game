package attr

import "slices"

// ModOp describes how a Modifier contributes to an attribute channel.
type ModOp uint8

const (
	ModAdd ModOp = iota
	// ModMul uses Unreal GAS-style bias=1 aggregation. For example, two
	// modifiers with Value 1.2 evaluate to 1.4, not 1.44.
	ModMul
	// ModDiv also uses bias=1 aggregation.
	ModDiv
	ModOverride
)

// Modifier is a pure attribute-aggregation contribution. Source is an opaque
// stable id owned by the caller, commonly an active effect handle in a game
// layer, but attr does not interpret it.
type Modifier struct {
	Source   uint64
	Attr     Key
	Op       ModOp
	Channel  uint8
	Priority int16
	Value    float64
}

type ModifierTemplate struct {
	Attr     Key
	Op       ModOp
	Channel  uint8
	Priority int16
	Value    float64
}

func (t ModifierTemplate) Bind(source uint64) Modifier {
	return Modifier{
		Source:   source,
		Attr:     t.Attr,
		Op:       t.Op,
		Channel:  t.Channel,
		Priority: t.Priority,
		Value:    t.Value,
	}
}

// Table owns attribute values plus their active modifier lists.
type Table struct {
	Values Map

	mods  map[Key][]Modifier
	dirty map[Key]struct{}
}

func (t *Table) Init() {
	if t.mods == nil {
		t.mods = make(map[Key][]Modifier)
	}
	if t.dirty == nil {
		t.dirty = make(map[Key]struct{})
	}
}

func (t *Table) Put(set AttributeSet) { t.Values.Put(set) }

func (t *Table) GetSet(id uint32) AttributeSet { return t.Values.Get(id) }

func (t *Table) GetBase(key Key) (float64, bool) { return t.Values.GetBase(key) }

func (t *Table) GetCurrent(key Key) (float64, bool) { return t.Values.GetCurrent(key) }

func (t *Table) SetBase(key Key, value float64) bool {
	if !t.Values.SetBase(key, value) {
		return false
	}
	t.MarkDirty(key)
	return true
}

func (t *Table) ModifyBase(key Key, delta float64) bool {
	base, ok := t.GetBase(key)
	if !ok {
		return false
	}
	return t.SetBase(key, base+delta)
}

func (t *Table) AddModifier(mod Modifier) {
	t.Init()
	t.mods[mod.Attr] = append(t.mods[mod.Attr], mod)
	t.MarkDirty(mod.Attr)
}

func (t *Table) AddModifiers(mods []Modifier) {
	for _, mod := range mods {
		t.AddModifier(mod)
	}
}

func (t *Table) UpdateModifiersBySource(source uint64, mods []Modifier) {
	t.RemoveModifiersBySource(source)
	for _, mod := range mods {
		mod.Source = source
		t.AddModifier(mod)
	}
}

func (t *Table) RemoveModifiersBySource(source uint64) int {
	t.Init()
	removed := 0
	for key, mods := range t.mods {
		n := 0
		for _, mod := range mods {
			if mod.Source == source {
				removed++
				continue
			}
			mods[n] = mod
			n++
		}
		if n != len(mods) {
			clear(mods[n:])
			if n == 0 {
				delete(t.mods, key)
			} else {
				t.mods[key] = mods[:n]
			}
			t.MarkDirty(key)
		}
	}
	return removed
}

func (t *Table) Modifiers(key Key, dst []Modifier) []Modifier {
	t.Init()
	dst = append(dst, t.mods[key]...)
	return dst
}

func (t *Table) MarkDirty(key Key) {
	t.Init()
	t.dirty[key] = struct{}{}
}

func (t *Table) Flush() int {
	t.Init()
	changed := 0
	for key := range t.dirty {
		if t.recompute(key) {
			changed++
		}
		delete(t.dirty, key)
	}
	return changed
}

func (t *Table) RecomputeAll() int {
	t.Init()
	changed := 0
	t.Values.sets.Iter(func(_ uint32, set AttributeSet) bool {
		setID := set.SetID()
		for field := uint16(0); field < set.FieldCount(); field++ {
			if t.recompute(MakeKey(setID, field)) {
				changed++
			}
		}
		return false
	})
	clear(t.dirty)
	return changed
}

func (t *Table) ClearDirty() {
	t.Init()
	clear(t.dirty)
	t.Values.sets.Iter(func(_ uint32, set AttributeSet) bool {
		set.ClearDirty()
		return false
	})
}

func (t *Table) recompute(key Key) bool {
	set := t.Values.Get(KeySetID(key))
	if set == nil {
		return false
	}
	field := KeyField(key)
	base, ok := set.GetBase(field)
	if !ok {
		return false
	}
	next := Eval(base, t.mods[key])
	cur, _ := set.GetCurrent(field)
	if hooks, ok := set.(Hooks); ok {
		next = hooks.PreCurrentChange(field, next)
	}
	if cur == next {
		return false
	}
	if !set.SetCurrent(field, next) {
		return false
	}
	if hooks, ok := set.(Hooks); ok {
		hooks.PostCurrentChange(field, cur, next)
	}
	return true
}

// Eval returns the current value produced by applying mods to base.
func Eval(base float64, mods []Modifier) float64 {
	if len(mods) == 0 {
		return base
	}

	var channels []uint8
	var used [256]bool
	var agg [256]channelMods

	for _, mod := range mods {
		ch := mod.Channel
		if !used[ch] {
			used[ch] = true
			channels = append(channels, ch)
			agg[ch].mul = 1
			agg[ch].div = 1
		}
		c := &agg[ch]
		switch mod.Op {
		case ModAdd:
			c.add += mod.Value
		case ModMul:
			c.mul += mod.Value - 1
		case ModDiv:
			c.div += mod.Value - 1
		case ModOverride:
			if !c.hasOverride || betterOverride(mod, c.override) {
				c.hasOverride = true
				c.override = mod
			}
		}
	}

	slices.Sort(channels)

	out := base
	for _, ch := range channels {
		c := agg[ch]
		if c.hasOverride {
			out = c.override.Value
			continue
		}
		out = (out + c.add) * c.mul
		if c.div != 0 {
			out /= c.div
		}
	}
	return out
}

type channelMods struct {
	add         float64
	mul         float64
	div         float64
	hasOverride bool
	override    Modifier
}

func betterOverride(a, b Modifier) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.Source != b.Source {
		return a.Source < b.Source
	}
	return a.Value < b.Value
}

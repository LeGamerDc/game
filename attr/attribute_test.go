package attr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testSetID uint32 = 1

	testFieldHP uint16 = iota
	testFieldAttack
	testFieldCount
)

var (
	testAttrHP     = MakeKey(testSetID, testFieldHP)
	testAttrAttack = MakeKey(testSetID, testFieldAttack)
)

type testSet struct {
	dirty   uint64
	base    [testFieldCount]float64
	current [testFieldCount]float64
	changes int
}

func (s *testSet) SetID() uint32      { return testSetID }
func (s *testSet) FieldCount() uint16 { return testFieldCount }
func (s *testSet) Dirty() uint64      { return s.dirty }
func (s *testSet) ClearDirty()        { s.dirty = 0 }

func (s *testSet) GetCurrent(field uint16) (float64, bool) {
	if field >= testFieldCount {
		return 0, false
	}
	return s.current[field], true
}

func (s *testSet) GetBase(field uint16) (float64, bool) {
	if field >= testFieldCount {
		return 0, false
	}
	return s.base[field], true
}

func (s *testSet) SetBase(field uint16, v float64) bool {
	if field >= testFieldCount {
		return false
	}
	s.base[field] = v
	s.dirty |= 1 << field
	return true
}

func (s *testSet) SetCurrent(field uint16, v float64) bool {
	if field >= testFieldCount {
		return false
	}
	s.current[field] = v
	s.dirty |= 1 << field
	return true
}

func (s *testSet) PreBaseChange(field uint16, next float64) float64 {
	if field == testFieldHP && next < 0 {
		return 0
	}
	return next
}

func (s *testSet) PreCurrentChange(field uint16, next float64) float64 {
	if field == testFieldHP && next < 0 {
		return 0
	}
	return next
}

func (s *testSet) PostCurrentChange(uint16, float64, float64) {
	s.changes++
}

func TestTableAggregatesModifiersDeterministically(t *testing.T) {
	var attrs Table
	attrs.Init()
	attrs.Put(&testSet{})
	require.True(t, attrs.SetBase(testAttrAttack, 100))
	require.Equal(t, 1, attrs.Flush())

	attrs.AddModifier(Modifier{Source: 2, Attr: testAttrAttack, Op: ModMul, Value: 1.2})
	attrs.AddModifier(Modifier{Source: 1, Attr: testAttrAttack, Op: ModAdd, Value: 10})
	require.Equal(t, 1, attrs.Flush())
	cur, ok := attrs.GetCurrent(testAttrAttack)
	require.True(t, ok)
	require.InEpsilon(t, 132, cur, 0.0001)

	attrs.AddModifier(Modifier{Source: 3, Attr: testAttrAttack, Op: ModMul, Value: 1.2})
	require.Equal(t, 1, attrs.Flush())
	cur, _ = attrs.GetCurrent(testAttrAttack)
	require.InEpsilon(t, 154, cur, 0.0001)

	attrs.AddModifier(Modifier{Source: 9, Attr: testAttrAttack, Op: ModOverride, Priority: 10, Value: 50})
	attrs.AddModifier(Modifier{Source: 8, Attr: testAttrAttack, Op: ModOverride, Priority: 1, Value: 70})
	require.Equal(t, 1, attrs.Flush())
	cur, _ = attrs.GetCurrent(testAttrAttack)
	require.Equal(t, 50.0, cur)

	require.Equal(t, 1, attrs.RemoveModifiersBySource(9))
	require.Equal(t, 1, attrs.Flush())
	cur, _ = attrs.GetCurrent(testAttrAttack)
	require.Equal(t, 70.0, cur)
}

func TestTableCallsAttributeHooks(t *testing.T) {
	set := &testSet{}
	var attrs Table
	attrs.Init()
	attrs.Put(set)

	require.True(t, attrs.SetBase(testAttrHP, -10))
	require.Equal(t, 0, attrs.Flush())

	base, _ := attrs.GetBase(testAttrHP)
	cur, _ := attrs.GetCurrent(testAttrHP)
	require.Equal(t, 0.0, base)
	require.Equal(t, 0.0, cur)
	require.Equal(t, 0, set.changes)

	attrs.AddModifier(Modifier{Source: 1, Attr: testAttrHP, Op: ModAdd, Value: -5})
	require.Equal(t, 0, attrs.Flush())
	cur, _ = attrs.GetCurrent(testAttrHP)
	require.Equal(t, 0.0, cur)
}

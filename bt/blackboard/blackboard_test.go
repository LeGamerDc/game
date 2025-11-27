package blackboard

import (
	"testing"

	"github.com/legamerdc/game/lib"
	"github.com/stretchr/testify/assert"
)

func TestBlackboard_Basic(t *testing.T) {
	bb := New()

	// 测试 Set 和 Get
	bb.Set("name", lib.Any("warrior"))
	v, ok := bb.Get("name")
	assert.True(t, ok)
	name, ok := lib.TakeAny[string](&v)
	assert.True(t, ok)
	assert.Equal(t, "warrior", name)

	// 测试不存在的 key
	_, ok = bb.Get("nonexistent")
	assert.False(t, ok)

	// 测试 Has
	assert.True(t, bb.Has("name"))
	assert.False(t, bb.Has("nonexistent"))

	// 测试 Del
	bb.Del("name")
	assert.False(t, bb.Has("name"))

	// 测试 Len
	bb.Set("a", lib.Int32(1))
	bb.Set("b", lib.Int32(2))
	assert.Equal(t, 2, bb.Len())

	// 测试 Clear
	bb.Clear()
	assert.Equal(t, 0, bb.Len())
}

func TestBlackboard_TypedGetters(t *testing.T) {
	bb := New()

	// 测试 GetInt32
	bb.Set("health", lib.Int32(100))
	health, ok := bb.GetInt32("health")
	assert.True(t, ok)
	assert.Equal(t, int32(100), health)

	// 测试 GetInt64
	bb.Set("time", lib.Int64(1234567890))
	time, ok := bb.GetInt64("time")
	assert.True(t, ok)
	assert.Equal(t, int64(1234567890), time)

	// 测试 GetFloat64
	bb.Set("speed", lib.Float64(3.14))
	speed, ok := bb.GetFloat64("speed")
	assert.True(t, ok)
	assert.InDelta(t, 3.14, speed, 0.001)

	// 测试 GetFloat32
	bb.Set("rate", lib.Float32(1.5))
	rate, ok := bb.GetFloat32("rate")
	assert.True(t, ok)
	assert.InDelta(t, 1.5, rate, 0.001)

	// 测试 GetBool
	bb.Set("alive", lib.Bool(true))
	alive, ok := bb.GetBool("alive")
	assert.True(t, ok)
	assert.True(t, alive)

	// 测试类型不匹配
	bb.Set("action", lib.Any("attack"))
	_, ok = bb.GetInt32("action")
	assert.False(t, ok)
}

func TestBlackboard_GetAny(t *testing.T) {
	bb := New()

	type Enemy struct {
		ID   int
		Name string
	}

	enemy := Enemy{ID: 1, Name: "Goblin"}
	bb.Set("target", lib.Any(enemy))

	// 测试 GetAny
	result, ok := GetAny[Enemy](bb, "target")
	assert.True(t, ok)
	assert.Equal(t, enemy, result)

	// 测试类型不匹配
	_, ok = GetAny[string](bb, "target")
	assert.False(t, ok)

	// 测试不存在的 key
	_, ok = GetAny[Enemy](bb, "nonexistent")
	assert.False(t, ok)
}

func TestBlackboard_Keys(t *testing.T) {
	bb := New()
	bb.Set("a", lib.Int32(1))
	bb.Set("b", lib.Int32(2))
	bb.Set("c", lib.Int32(3))

	keys := bb.Keys()
	assert.Len(t, keys, 3)
	assert.Contains(t, keys, "a")
	assert.Contains(t, keys, "b")
	assert.Contains(t, keys, "c")
}

func TestBlackboard_TypeConversion(t *testing.T) {
	bb := New()

	// int32 -> int64
	bb.Set("int32val", lib.Int32(42))
	v64, ok := bb.GetInt64("int32val")
	assert.True(t, ok)
	assert.Equal(t, int64(42), v64)

	// int64 -> float64
	bb.Set("int64val", lib.Int64(100))
	f, ok := bb.GetFloat64("int64val")
	assert.True(t, ok)
	assert.InDelta(t, 100.0, f, 0.001)

	// float32 -> float64
	bb.Set("float32val", lib.Float32(1.5))
	f, ok = bb.GetFloat64("float32val")
	assert.True(t, ok)
	assert.InDelta(t, 1.5, f, 0.001)

	// int32 -> bool
	bb.Set("truthy", lib.Int32(1))
	b, ok := bb.GetBool("truthy")
	assert.True(t, ok)
	assert.True(t, b)

	bb.Set("falsy", lib.Int32(0))
	b, ok = bb.GetBool("falsy")
	assert.True(t, ok)
	assert.False(t, b)
}

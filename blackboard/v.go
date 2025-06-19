package blackboard

import "math"

type Kind int32

const (
	KindAny Kind = iota
	KindInt32
	KindInt64
	KindFloat32
	KindFloat64
	KindBool
)

// Field 主要做数值计算，也可以用来存储 any
type Field struct {
	kind Kind
	vi   int64
	va   any
}

func Any(v any) Field {
	return Field{
		kind: KindAny,
		va:   v,
	}
}

func Int32(v int32) Field {
	return Field{
		kind: KindInt32,
		vi:   int64(v),
	}
}

func Int64(v int64) Field {
	return Field{
		kind: KindInt64,
		vi:   v,
	}
}

func Float32(v float32) Field {
	return Field{
		kind: KindFloat32,
		vi:   int64(math.Float32bits(v)),
	}
}

func Float64(v float64) Field {
	return Field{
		kind: KindFloat64,
		vi:   int64(math.Float64bits(v)),
	}
}

func Bool(v bool) Field {
	var i int64
	if v {
		i = 1
	}
	return Field{
		kind: KindBool,
		vi:   i,
	}
}

func TakeAny[T any](f *Field) (v T, ok bool) {
	if f.kind == KindAny {
		v, ok = f.va.(T)
	}
	return
}

func (f Field) Int32() (int32, bool) {
	if f.kind == KindInt32 {
		return int32(f.vi), true
	}
	return 0, false
}

func (f Field) Int64() (int64, bool) {
	if f.kind == KindInt64 || f.kind == KindInt32 {
		return f.vi, true
	}
	return 0, false
}

func (f Field) Float32() (float32, bool) {
	switch f.kind {
	case KindFloat32:
		return math.Float32frombits(uint32(f.vi)), true
	case KindInt32, KindInt64:
		return float32(f.vi), true
	default:
		return 0, false
	}
}

func (f Field) Float64() (float64, bool) {
	switch f.kind {
	case KindFloat64:
		return math.Float64frombits(uint64(f.vi)), true
	case KindFloat32:
		return float64(math.Float32frombits(uint32(f.vi))), true
	case KindInt32, KindInt64:
		return float64(f.vi), true
	default:
		return 0, false
	}
}

func (f Field) Bool() (bool, bool) {
	switch f.kind {
	case KindBool, KindInt32, KindInt64:
		return f.vi != 0, true
	default:
		return false, false
	}
}

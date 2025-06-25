package calc

import (
	"testing"

	"github.com/legamerdc/game/blackboard"

	"github.com/stretchr/testify/assert"
)

func TestParse(t *testing.T) {
	var e error

	_, e = parse("(x+y)*z")
	assert.Nil(t, e)

	_, e = parse("x >= y + 3")
	assert.Nil(t, e)

	_, e = parse("5 + (-x)")
	assert.Nil(t, e)

	_, e = parse("x % 7 <= 2")
	assert.Nil(t, e)

	_, e = parse("x < 5 || y > 3")
	assert.Nil(t, e)

	_, e = parse("x < 5 ? y+2 : z+3")
	assert.Nil(t, e)

	_, e = parse("x = y*21")
	assert.Nil(t, e)

	_, e = parse("int x")
	assert.Nil(t, e)

	_, e = parse("float y,z")
	assert.Nil(t, e)

	_, e = parse("bool x,z; float h; int p, q")
	assert.Nil(t, e)

	_, e = parse("int x,y,z; z = (x+3)*(y+2); z % 2 == 0")
	assert.Nil(t, e)

	_, e = parse("x, y = y, x")
	assert.NotNil(t, e)

	_, e = parse("x & y")
	assert.NotNil(t, e)

	_, e = parse("pp x,y,z")
	assert.NotNil(t, e)

	_, e = parse("x !=< y")
	assert.NotNil(t, e)
}

// MockKv 实现 Ctx 接口用于测试
type MockKv struct {
	data map[string]blackboard.Field
}

func NewMockKv() *MockKv {
	return &MockKv{
		data: make(map[string]blackboard.Field),
	}
}

func (m *MockKv) Get(key string) (blackboard.Field, bool) {
	v, ok := m.data[key]
	return v, ok
}

func (m *MockKv) Set(key string, v blackboard.Field) {
	m.data[key] = v
}

func (m *MockKv) Del(key string) {
	delete(m.data, key)
}

func (m *MockKv) Exec(key string) (v blackboard.Field, ok bool) {
	v1, ok1 := m.getInt64("_1")
	if !ok1 {
		return
	}
	return blackboard.Int64(v1 * v1), true
}

func (m *MockKv) getInt64(key string) (int64, bool) {
	v1, ok1 := m.Get(key)
	if !ok1 {
		return 0, false
	}
	vv, ok2 := v1.Int64()
	return vv, ok2
}

func (m *MockKv) SetInt64(key string, value int64) {
	m.data[key] = blackboard.Int64(value)
}

func (m *MockKv) SetFloat64(key string, value float64) {
	m.data[key] = blackboard.Float64(value)
}

func (m *MockKv) SetBool(key string, value bool) {
	m.data[key] = blackboard.Bool(value)
}

func TestCompile(t *testing.T) {
	// 测试简单算术表达式
	t.Run("简单算术运算", func(t *testing.T) {
		compiledFunc, err := Compile[*MockKv]("int x, y; x + y")
		assert.Nil(t, err)
		assert.NotNil(t, compiledFunc)

		kv := NewMockKv()
		kv.SetInt64("x", 10)
		kv.SetInt64("y", 5)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(15), v)
	})

	// 测试浮点数运算
	t.Run("浮点数运算", func(t *testing.T) {
		compiledFunc, err := Compile[*MockKv]("float x, y; x * y")
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetFloat64("x", 3.14)
		kv.SetFloat64("y", 2.0)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Float64()
		assert.True(t, ok)
		assert.InDelta(t, 6.28, v, 0.001)
	})

	// 测试逻辑运算
	t.Run("逻辑运算", func(t *testing.T) {
		compiledFunc, err := Compile[*MockKv]("int x, y; x > 5 && y < 10")
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetInt64("x", 8)
		kv.SetInt64("y", 3)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Bool()
		assert.True(t, ok)
		assert.True(t, v)
	})

	// 测试三目运算符
	t.Run("三目运算符", func(t *testing.T) {
		compiledFunc, err := Compile[*MockKv]("int x; x > 0 ? x : -x")
		assert.Nil(t, err)

		// 测试正数情况
		kv := NewMockKv()
		kv.SetInt64("x", 5)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(5), v)

		// 测试负数情况
		kv.SetInt64("x", -3)
		result, err = compiledFunc(kv)
		assert.Nil(t, err)
		v, ok = result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(3), v)
	})

	// 测试变量赋值
	t.Run("变量赋值", func(t *testing.T) {
		compiledFunc, err := Compile[*MockKv]("int x; x = 42")
		assert.Nil(t, err)

		kv := NewMockKv()
		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(42), v)

		// 验证变量已被设置
		storedValue, exists := kv.Get("x")
		assert.True(t, exists)
		storedInt, ok := storedValue.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(42), storedInt)
	})

	// 测试复杂表达式
	t.Run("复杂表达式", func(t *testing.T) {
		compiledFunc, err := Compile[*MockKv]("int x, y, z, w; (x + y) * z - w")
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetInt64("x", 2)
		kv.SetInt64("y", 3)
		kv.SetInt64("z", 4)
		kv.SetInt64("w", 5)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(15), v) // (2+3)*4-5 = 15
	})

	// 测试取模运算
	t.Run("取模运算", func(t *testing.T) {
		compiledFunc, err := Compile[*MockKv]("int x, y; x % y")
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetInt64("x", 17)
		kv.SetInt64("y", 5)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(2), v)
	})

	// 测试整数幂运算
	t.Run("整数幂运算", func(t *testing.T) {
		compiledFunc, err := Compile[*MockKv]("int x, y; x ^ y")
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetInt64("x", 2)
		kv.SetInt64("y", 3)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(8), v)
	})

	// 测试浮点数幂运算
	t.Run("浮点数幂运算", func(t *testing.T) {
		compiledFunc, err := Compile[*MockKv]("float x, y; x ^ y")
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetFloat64("x", 2.0)
		kv.SetFloat64("y", 3.0)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Float64()
		assert.True(t, ok)
		assert.InDelta(t, 8.0, v, 0.001) // 2^3 = 8 (Power)
	})

	// 测试一元运算符
	t.Run("一元运算符", func(t *testing.T) {
		// 测试负号
		compiledFunc, err := Compile[*MockKv]("int x; -x")
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetInt64("x", 5)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(-5), v)

		// 测试逻辑非
		compiledFunc2, err := Compile[*MockKv]("bool flag; !flag")
		assert.Nil(t, err)

		kv.SetBool("flag", true)
		result, err = compiledFunc2(kv)
		assert.Nil(t, err)
		b, ok := result.Bool()
		assert.True(t, ok)
		assert.False(t, b)
	})

	// 测试布尔常量
	t.Run("布尔常量", func(t *testing.T) {
		compiledFunc, err := Compile[*MockKv]("true")
		assert.Nil(t, err)

		kv := NewMockKv()
		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Bool()
		assert.True(t, ok)
		assert.True(t, v)
	})

	// 测试数字常量
	t.Run("数字常量", func(t *testing.T) {
		compiledFunc, err := Compile[*MockKv]("3.14159")
		assert.Nil(t, err)

		kv := NewMockKv()
		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Float64()
		assert.True(t, ok)
		assert.InDelta(t, 3.14159, v, 0.00001)
	})

	// 测试函数调用
	t.Run("函数调用", func(t *testing.T) {
		f, e := Compile[*MockKv]("int x, ff, _1; _1=x; ff()+2*x+1")
		assert.Nil(t, e)

		kv := NewMockKv()
		kv.SetInt64("x", 2)
		v, e1 := f(kv)
		assert.Nil(t, e1)
		vv, ok := v.Int64()
		assert.True(t, ok)
		assert.InDelta(t, 9.0, vv, 0.00001)

		_, ok = kv.Get("_1")
		assert.False(t, ok)
	})
}

func TestCompileErrors(t *testing.T) {
	// 测试语法错误
	t.Run("语法错误", func(t *testing.T) {
		_, err := Compile[*MockKv]("x & y")
		assert.NotNil(t, err)

		_, err = Compile[*MockKv]("x !=< y")
		assert.NotNil(t, err)
	})

	// 测试类型错误
	t.Run("类型错误", func(t *testing.T) {
		_, err := Compile[*MockKv]("pp x")
		assert.NotNil(t, err)
	})

	// 测试未声明变量
	t.Run("运行时变量未找到", func(t *testing.T) {
		compiledFunc, err := Compile[*MockKv]("int x, y; x + y")
		assert.Nil(t, err)

		kv := NewMockKv()
		// 不设置 x 和 y
		_, err = compiledFunc(kv)
		assert.NotNil(t, err) // 应该产生运行时错误
	})
}

func BenchmarkCompile(b *testing.B) {
	kv := NewMockKv()
	kv.SetInt64("power_x", 3000)
	kv.SetInt64("power_y", 3000)
	compiledFunc, _ := Compile[*MockKv](`
	float power, power_x, power_y;
	power = power_x * 0.95 + power_y * 1.25;
	power > 3000`)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = compiledFunc(kv)
	}
}

func code(m *MockKv) (bool, bool) {
	vx, ox := m.Get("power_x")
	vy, oy := m.Get("power_y")
	if ox && oy {
		fx, o1 := vx.Float64()
		fy, o2 := vy.Float64()
		if o1 && o2 {
			m.SetFloat64("power", fx*0.95+fy*1.25)
			vp, o3 := m.Get("power")
			if !o3 {
				return false, false
			}
			fp, o4 := vp.Float64()
			if !o4 {
				return false, false
			}
			return fp > 3000, true
		}
	}
	return false, false
}

func BenchmarkCode(b *testing.B) {
	kv := NewMockKv()
	kv.SetInt64("power_x", 3000)
	kv.SetInt64("power_y", 3000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		code(kv)
	}
}

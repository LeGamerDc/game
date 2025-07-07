package cc

import (
	"fmt"
	"testing"

	"github.com/legamerdc/game/lib"

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
	data map[string]lib.Field
}

func NewMockKv() *MockKv {
	return &MockKv{
		data: make(map[string]lib.Field),
	}
}

func (m *MockKv) Get(key string) (lib.Field, bool) {
	v, ok := m.data[key]
	return v, ok
}

func (m *MockKv) Set(key string, v lib.Field) {
	m.data[key] = v
}

func (m *MockKv) Del(key string) {
	delete(m.data, key)
}

func (m *MockKv) Exec(key string) (v lib.Field, ok bool) {
	v1, ok1 := m.getInt64("_1")
	if !ok1 {
		return
	}
	return lib.Int64(v1 * v1), true
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
	m.data[key] = lib.Int64(value)
}

func (m *MockKv) SetFloat64(key string, value float64) {
	m.data[key] = lib.Float64(value)
}

func (m *MockKv) SetBool(key string, value bool) {
	m.data[key] = lib.Bool(value)
}

func s2s(s string) string {
	return s
}

func TestCompile(t *testing.T) {
	// 测试简单算术表达式
	t.Run("简单算术运算", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv]("int x, y; x + y", s2s)
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
		compiledFunc, err := Compile[string, *MockKv]("float x, y; x * y", s2s)
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
		compiledFunc, err := Compile[string, *MockKv]("int x, y; x > 5 && y < 10", s2s)
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
		compiledFunc, err := Compile[string, *MockKv]("int x; x > 0 ? x : -x", s2s)
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
		compiledFunc, err := Compile[string, *MockKv]("int x; x = 42", s2s)
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
		compiledFunc, err := Compile[string, *MockKv]("int x, y, z, w; (x + y) * z - w", s2s)
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
		compiledFunc, err := Compile[string, *MockKv]("int x, y; x % y", s2s)
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
		compiledFunc, err := Compile[string, *MockKv]("int x, y; x ^ y", s2s)
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
		compiledFunc, err := Compile[string, *MockKv]("float x, y; x ^ y", s2s)
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
		compiledFunc, err := Compile[string, *MockKv]("int x; -x", s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetInt64("x", 5)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(-5), v)

		// 测试逻辑非
		compiledFunc2, err := Compile[string, *MockKv]("bool flag; !flag", s2s)
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
		compiledFunc, err := Compile[string, *MockKv]("true", s2s)
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
		compiledFunc, err := Compile[string, *MockKv]("3.14159", s2s)
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
		f, e := Compile[string, *MockKv]("int x, ff, _1; _1=x; ff()+2*x+1", s2s)
		assert.Nil(t, e)

		kv := NewMockKv()
		kv.SetInt64("x", 2)
		v, e1 := f(kv)
		assert.Nil(t, e1)
		vv, ok := v.Int64()
		assert.True(t, ok)
		assert.InDelta(t, 9.0, vv, 0.00001)
	})

	// 测试复杂的运算符优先级
	t.Run("运算符优先级", func(t *testing.T) {
		// 测试幂运算优先级
		compiledFunc, err := Compile[string, *MockKv]("int x, y, z; x + y ^ z", s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetInt64("x", 2)
		kv.SetInt64("y", 3)
		kv.SetInt64("z", 2)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(11), v) // 2 + 3^2 = 2 + 9 = 11

		// 测试复杂的混合运算
		compiledFunc2, err := Compile[string, *MockKv]("int a, b, c, d; a * b + c * d", s2s)
		assert.Nil(t, err)

		kv.SetInt64("a", 2)
		kv.SetInt64("b", 3)
		kv.SetInt64("c", 4)
		kv.SetInt64("d", 5)

		result, err = compiledFunc2(kv)
		assert.Nil(t, err)
		v, ok = result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(26), v) // 2*3 + 4*5 = 6 + 20 = 26
	})

	// 测试嵌套三目运算符
	t.Run("嵌套三目运算符", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv]("int x, y, z; x > 0 ? (y > 0 ? x + y : x - y) : (z > 0 ? z : 0)", s2s)
		assert.Nil(t, err)

		kv := NewMockKv()

		// 测试 x > 0 && y > 0 的情况
		kv.SetInt64("x", 5)
		kv.SetInt64("y", 3)
		kv.SetInt64("z", 2)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(8), v) // 5 + 3 = 8

		// 测试 x > 0 && y <= 0 的情况
		kv.SetInt64("y", -3)
		result, err = compiledFunc(kv)
		assert.Nil(t, err)
		v, ok = result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(8), v) // 5 - (-3) = 8

		// 测试 x <= 0 && z > 0 的情况
		kv.SetInt64("x", -5)
		kv.SetInt64("z", 7)
		result, err = compiledFunc(kv)
		assert.Nil(t, err)
		v, ok = result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(7), v) // z = 7
	})

	// 测试复杂的逻辑表达式
	t.Run("复杂逻辑表达式", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv]("int x, y, z; float a, b; (x > 5 && y < 10) || (a >= 3.14 && b <= 2.71)", s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetInt64("x", 8)
		kv.SetInt64("y", 3)
		kv.SetFloat64("a", 2.5)
		kv.SetFloat64("b", 3.0)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Bool()
		assert.True(t, ok)
		assert.True(t, v) // 第一个条件为真：8 > 5 && 3 < 10

		// 测试第二个条件为真的情况
		kv.SetInt64("x", 3)
		kv.SetInt64("y", 15)
		kv.SetFloat64("a", 3.5)
		kv.SetFloat64("b", 2.0)

		result, err = compiledFunc(kv)
		assert.Nil(t, err)
		v, ok = result.Bool()
		assert.True(t, ok)
		assert.True(t, v) // 第二个条件为真：3.5 >= 3.14 && 2.0 <= 2.71
	})

	// 测试多重赋值和复杂表达式
	t.Run("多重赋值和复杂表达式", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv]("int x, y, z, result; float ratio; x = 10; y = 5; z = x * y; ratio = z / 25.0; result = ratio > 1.5 ? z + 10 : z - 10", s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(60), v) // ratio = 50/25 = 2.0 > 1.5, so 50 + 10 = 60

		// 验证中间变量
		storedZ, exists := kv.Get("z")
		assert.True(t, exists)
		zVal, ok := storedZ.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(50), zVal)

		storedRatio, exists := kv.Get("ratio")
		assert.True(t, exists)
		ratioVal, ok := storedRatio.Float64()
		assert.True(t, ok)
		assert.InDelta(t, 2.0, ratioVal, 0.001)
	})

	// 测试浮点数精度和边界情况
	t.Run("浮点数精度处理", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv]("float x, y, z; x = 0.1; y = 0.2; z = x + y; z > 0.29 && z < 0.31", s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Bool()
		assert.True(t, ok)
		assert.True(t, v) // 0.1 + 0.2 应该在 0.29 到 0.31 之间

		// 测试更复杂的浮点数运算
		compiledFunc2, err := Compile[string, *MockKv]("float a, b, c; a = 3.14159; b = 2.71828; c = a ^ 2 + b ^ 2; c > 15 && c < 18", s2s)
		assert.Nil(t, err)

		result, err = compiledFunc2(kv)
		assert.Nil(t, err)
		v, ok = result.Bool()
		assert.True(t, ok)
		assert.True(t, v) // π² + e² ≈ 9.87 + 7.39 = 17.26
	})

	// 测试类型混合运算
	t.Run("类型混合运算", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv]("int x; float y; bool flag; x = 5; y = 3.14; flag = x > y; flag", s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Bool()
		assert.True(t, ok)
		assert.True(t, v) // 5 > 3.14 为真

		// 测试另一种混合类型情况
		compiledFunc2, err := Compile[string, *MockKv]("int count; float rate; count = 10; rate = 0.8; count * rate", s2s)
		assert.Nil(t, err)

		result, err = compiledFunc2(kv)
		assert.Nil(t, err)
		fVal, ok := result.Float64()
		assert.True(t, ok)
		assert.InDelta(t, 8.0, fVal, 0.001) // 10 * 0.8 = 8.0
	})

	// 测试深层嵌套表达式
	t.Run("深层嵌套表达式", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv]("int a, b, c, d; ((a + b) * (c - d)) / ((a - b) + (c + d))", s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetInt64("a", 8)
		kv.SetInt64("b", 2)
		kv.SetInt64("c", 6)
		kv.SetInt64("d", 4)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Int64()
		assert.True(t, ok)
		// ((8+2) * (6-4)) / ((8-2) + (6+4)) = (10 * 2) / (6 + 10) = 20 / 16 = 1
		assert.Equal(t, int64(1), v)
	})

	// 测试布尔逻辑的短路求值
	t.Run("布尔短路求值", func(t *testing.T) {
		// 测试 && 短路 - 修改为不使用内联赋值
		compiledFunc, err := Compile[string, *MockKv]("bool flag1; int x; flag1 = false; x = 0; flag1 && x > 0", s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Bool()
		assert.True(t, ok)
		assert.False(t, v) // flag1 是 false，所以整个表达式是 false

		// 验证 x 被设置为 0
		xVal, exists := kv.Get("x")
		assert.True(t, exists)
		xInt, ok := xVal.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(0), xInt)

		// 测试 || 短路
		compiledFunc2, err := Compile[string, *MockKv]("bool flag1; int y; flag1 = true; y = 0; flag1 || y > 0", s2s)
		assert.Nil(t, err)

		result, err = compiledFunc2(kv)
		assert.Nil(t, err)
		v, ok = result.Bool()
		assert.True(t, ok)
		assert.True(t, v) // flag1 是 true，所以整个表达式是 true

		// 验证 y 被设置为 0
		yVal, exists := kv.Get("y")
		assert.True(t, exists)
		yInt, ok := yVal.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(0), yInt)

		// 测试更复杂的短路情况
		compiledFunc3, err := Compile[string, *MockKv]("bool a, b; int count; a = false; b = true; count = 10; a && b && count > 5", s2s)
		assert.Nil(t, err)

		result, err = compiledFunc3(kv)
		assert.Nil(t, err)
		v, ok = result.Bool()
		assert.True(t, ok)
		assert.False(t, v) // a 是 false，短路后整个表达式是 false
	})

	// 测试幂运算的右结合性
	t.Run("幂运算的右结合性", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv]("int base, exp1, exp2; float result; base = 2; exp1 = 3; exp2 = 2; result = base ^ exp1 ^ exp2; result > 250", s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetInt64("base", 2)
		kv.SetInt64("exp1", 3)
		kv.SetInt64("exp2", 2)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Bool()
		assert.True(t, ok)
		assert.True(t, v) // 2^(3^2) = 2^9 = 512 > 250

		// 验证结果
		storedResult, exists := kv.Get("result")
		assert.True(t, exists)
		resultVal, ok := storedResult.Float64()
		assert.True(t, ok)
		assert.InDelta(t, 512.0, resultVal, 0.001)
	})

	// 测试边界值
	t.Run("边界值", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv]("int x, y; x = 10; y = 0; x != 0 && y == 0", s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Bool()
		assert.True(t, ok)
		assert.True(t, v) // 10 != 0 && 0 == 0 为真

		// 测试很大的数值
		compiledFunc2, err := Compile[string, *MockKv]("int large; large = 999999; large > 500000", s2s)
		assert.Nil(t, err)

		result, err = compiledFunc2(kv)
		assert.Nil(t, err)
		v, ok = result.Bool()
		assert.True(t, ok)
		assert.True(t, v)
	})

	// 测试复杂的游戏逻辑场景
	t.Run("游戏逻辑场景", func(t *testing.T) {
		// 模拟游戏中的技能冷却和资源消耗判断
		compiledFunc, err := Compile[string, *MockKv](`
			int hp, mp, max_hp, max_mp, skill_cost, cooldown_time, current_time;
			float hp_ratio, mp_ratio;
			bool can_cast_skill;
			
			hp_ratio = hp / max_hp;
			mp_ratio = mp / max_mp;
			can_cast_skill = (mp >= skill_cost) && (current_time >= cooldown_time) && (hp_ratio > 0.3);
			
			can_cast_skill && (hp_ratio < 0.8 || mp_ratio > 0.9)
		`, s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetInt64("hp", 60)
		kv.SetInt64("max_hp", 100)
		kv.SetInt64("mp", 80)
		kv.SetInt64("max_mp", 100)
		kv.SetInt64("skill_cost", 50)
		kv.SetInt64("cooldown_time", 10)
		kv.SetInt64("current_time", 15)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Bool()
		assert.True(t, ok)
		assert.True(t, v) // 应该可以释放技能：mp够用，冷却时间到了，hp>30%，且hp<80%

		// 测试不满足条件的情况
		kv.SetInt64("mp", 30) // mp不够
		result, err = compiledFunc(kv)
		assert.Nil(t, err)
		v, ok = result.Bool()
		assert.True(t, ok)
		assert.False(t, v) // 不能释放技能
	})

	// 测试长表达式
	t.Run("长表达式", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv]("float x, y, z, result; x = 1.5; y = 2.5; z = 3.5; result = (x + y) * z - (x * y) / z + x ^ 2 - y ^ 2", s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Float64()
		assert.True(t, ok)
		// (1.5 + 2.5) * 3.5 - (1.5 * 2.5) / 3.5 + 1.5^2 - 2.5^2
		// = 4 * 3.5 - 3.75 / 3.5 + 2.25 - 6.25
		// = 14 - 1.071 + 2.25 - 6.25
		// ≈ 8.929
		assert.InDelta(t, 8.929, v, 0.01)
	})

	// 测试除零错误
	t.Run("除零错误", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv]("int x, y; x / y", s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetInt64("x", 10)
		kv.SetInt64("y", 0)

		// 验证除零会导致panic
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Logf("除法除零正确触发panic: %v", r)
				} else {
					t.Error("期望除零会触发panic，但没有")
				}
			}()
			_, _ = compiledFunc(kv)
		}()

		// 取模除零
		compiledFunc2, err := Compile[string, *MockKv]("int x, y; x % y", s2s)
		assert.Nil(t, err)

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Logf("取模除零正确触发panic: %v", r)
				} else {
					t.Error("期望除零会触发panic，但没有")
				}
			}()
			_, _ = compiledFunc2(kv)
		}()

		// 测试非零情况应该正常工作
		kv.SetInt64("y", 3)
		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(3), v) // 10 / 3 = 3
	})
}

func TestCompileErrors(t *testing.T) {
	// 测试语法错误
	t.Run("语法错误", func(t *testing.T) {
		_, err := Compile[string, *MockKv]("x & y", s2s)
		assert.NotNil(t, err)

		_, err = Compile[string, *MockKv]("x !=< y", s2s)
		assert.NotNil(t, err)
	})

	// 测试类型错误
	t.Run("类型错误", func(t *testing.T) {
		_, err := Compile[string, *MockKv]("pp x", s2s)
		assert.NotNil(t, err)
	})

	// 测试未声明变量
	t.Run("运行时变量未找到", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv]("int x, y; x + y", s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		// 不设置 x 和 y
		_, err = compiledFunc(kv)
		assert.NotNil(t, err) // 应该产生运行时错误
	})

	// 测试类型不匹配的复杂情况
	t.Run("复杂类型不匹配", func(t *testing.T) {
		// 测试布尔值与数字进行算术运算
		_, err := Compile[string, *MockKv]("bool flag; int x; flag + x", s2s)
		assert.NotNil(t, err)

		// 测试浮点数与布尔值比较
		_, err = Compile[string, *MockKv]("float x; bool flag; x == flag", s2s)
		assert.NotNil(t, err)

		// 测试对布尔值进行算术操作
		_, err = Compile[string, *MockKv]("bool a, b; a * b", s2s)
		assert.NotNil(t, err)
	})

	// 测试不支持的运算符组合
	t.Run("不支持的运算符组合", func(t *testing.T) {
		// 测试对字符串进行操作（不支持的类型）
		_, err := Compile[string, *MockKv]("string x", s2s)
		assert.NotNil(t, err)

		// 测试不完整的三目运算符
		_, err = Compile[string, *MockKv]("int x; x > 0 ? 1", s2s)
		assert.NotNil(t, err)

		// 测试括号不匹配
		_, err = Compile[string, *MockKv]("int x; (x + 1", s2s)
		assert.NotNil(t, err)
	})

	// 测试变量作用域和重复声明
	t.Run("变量声明错误", func(t *testing.T) {
		// 测试未声明变量的使用
		_, err := Compile[string, *MockKv]("x + y", s2s)
		assert.NotNil(t, err)

		// 测试对未声明变量的赋值
		_, err = Compile[string, *MockKv]("x = 5", s2s)
		assert.NotNil(t, err)
	})

	// 测试数值边界和格式错误
	t.Run("数值格式错误", func(t *testing.T) {
		// 测试非法的数字格式
		_, err := Compile[string, *MockKv]("int x; x = 3.14.159", s2s)
		assert.NotNil(t, err)

		// 测试空表达式
		_, err = Compile[string, *MockKv]("", s2s)
		assert.NotNil(t, err)

		// 测试只有类型声明没有表达式
		compiledFunc, err := Compile[string, *MockKv]("int x, y", s2s)
		assert.Nil(t, err) // 类型声明本身是合法的

		kv := NewMockKv()
		_, err = compiledFunc(kv)
		assert.Nil(t, err) // 只有类型声明的程序应该能正常运行
	})

	// 测试逻辑运算的类型约束
	t.Run("逻辑运算类型约束", func(t *testing.T) {
		// 测试对浮点数使用逻辑非
		_, err := Compile[string, *MockKv]("float x; !x", s2s)
		assert.NotNil(t, err)

		// 测试浮点数的逻辑运算
		_, err = Compile[string, *MockKv]("float x, y; x && y", s2s)
		assert.NotNil(t, err)

		// 测试对布尔值使用算术运算符
		_, err = Compile[string, *MockKv]("bool x; +x", s2s)
		assert.NotNil(t, err)
	})

	// 测试幂运算的特殊情况
	t.Run("幂运算边界情况", func(t *testing.T) {
		// 正常情况应该可以编译
		compiledFunc, err := Compile[string, *MockKv]("int x, y; x ^ y", s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetInt64("x", 0)
		kv.SetInt64("y", 0)

		// 0^0 的情况
		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		v, ok := result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(1), v) // 0^0 通常定义为 1

		// 负数的幂运算
		kv.SetInt64("x", -2)
		kv.SetInt64("y", 3)
		result, err = compiledFunc(kv)
		assert.Nil(t, err)
		v, ok = result.Int64()
		assert.True(t, ok)
		assert.Equal(t, int64(-8), v) // (-2)^3 = -8
	})
}

func BenchmarkCompile(b *testing.B) {
	kv := NewMockKv()
	kv.SetInt64("power_x", 3000)
	kv.SetInt64("power_y", 3000)
	compiledFunc, _ := Compile[string, *MockKv](`
	float power, power_x, power_y;
	power = power_x * 0.95 + power_y * 1.25;
	power > 3000`, s2s)

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

// 基准测试：复杂的数学表达式
func BenchmarkComplexMath(b *testing.B) {
	kv := NewMockKv()
	kv.SetInt64("a", 5)
	kv.SetInt64("b", 3)
	kv.SetInt64("c", 7)
	kv.SetFloat64("pi", 3.14159)
	kv.SetFloat64("e", 2.71828)

	compiledFunc, _ := Compile[string, *MockKv](`
		int a, b, c;
		float pi, e, result;
		result = (a + b) * c ^ 2 - (pi * e) / (a - b) + c * pi;
		result > 100
	`, s2s)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = compiledFunc(kv)
	}
}

// 基准测试：深层嵌套表达式
func BenchmarkDeepNesting(b *testing.B) {
	kv := NewMockKv()
	for i := 1; i <= 10; i++ {
		kv.SetInt64(fmt.Sprintf("x%d", i), int64(i))
	}

	compiledFunc, _ := Compile[string, *MockKv](`
		int x1, x2, x3, x4, x5, x6, x7, x8, x9, x10;
		((x1 + x2) * (x3 - x4)) + ((x5 * x6) / (x7 + x8)) - ((x9 ^ x10) % (x1 + x5))
	`, s2s)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = compiledFunc(kv)
	}
}

// 基准测试：逻辑表达式
func BenchmarkComplexLogic(b *testing.B) {
	kv := NewMockKv()
	kv.SetInt64("hp", 80)
	kv.SetInt64("max_hp", 100)
	kv.SetInt64("mp", 60)
	kv.SetInt64("max_mp", 100)
	kv.SetInt64("level", 25)
	kv.SetInt64("enemy_level", 30)
	kv.SetFloat64("distance", 150.5)

	compiledFunc, _ := Compile[string, *MockKv](`
		int hp, max_hp, mp, max_mp, level, enemy_level;
		float distance, hp_ratio, mp_ratio;
		hp_ratio = hp / max_hp;
		mp_ratio = mp / max_mp;
		(hp_ratio > 0.5 && mp_ratio > 0.3) && 
		(level > enemy_level - 10) && 
		(distance < 200.0) &&
		((hp > 50 && mp > 30) || (level > 20))
	`, s2s)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = compiledFunc(kv)
	}
}

// 测试极端复杂的表达式场景
func TestExtremeComplexity(t *testing.T) {
	t.Run("AI决策树模拟", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv](`
			int player_hp, player_mp, player_level, player_attack, player_defense;
			int enemy_hp, enemy_mp, enemy_level, enemy_attack, enemy_defense;
			float distance, player_speed, enemy_speed;
			bool has_potion, has_spell, is_boss;
			
			float hp_ratio, mp_ratio, level_diff, attack_ratio, defense_ratio;
			bool should_attack, should_defend, should_flee, should_heal;
			
			hp_ratio = player_hp / 100.0;
			mp_ratio = player_mp / 100.0;
			level_diff = player_level - enemy_level;
			attack_ratio = player_attack / enemy_attack;
			defense_ratio = player_defense / enemy_defense;
			
			should_heal = (hp_ratio < 0.3) && has_potion;
			should_flee = (hp_ratio < 0.2) || (level_diff < -5 && !is_boss);
			should_defend = (enemy_attack > player_defense * 1.5) && (hp_ratio < 0.6);
			should_attack = !should_heal && !should_flee && !should_defend && 
			               (mp_ratio > 0.2 || distance < 50.0) && 
			               (attack_ratio > 0.8 || has_spell);
			
			should_attack ? 1 : (should_defend ? 2 : (should_heal ? 3 : 4))
		`, s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		// 设置游戏状态
		kv.SetInt64("player_hp", 70)
		kv.SetInt64("player_mp", 80)
		kv.SetInt64("player_level", 25)
		kv.SetInt64("player_attack", 150)
		kv.SetInt64("player_defense", 120)
		kv.SetInt64("enemy_hp", 100)
		kv.SetInt64("enemy_mp", 60)
		kv.SetInt64("enemy_level", 22)
		kv.SetInt64("enemy_attack", 130)
		kv.SetInt64("enemy_defense", 100)
		kv.SetFloat64("distance", 45.0)
		kv.SetFloat64("player_speed", 10.5)
		kv.SetFloat64("enemy_speed", 8.2)
		kv.SetBool("has_potion", true)
		kv.SetBool("has_spell", true)
		kv.SetBool("is_boss", false)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		decision, ok := result.Int64()
		assert.True(t, ok)
		assert.True(t, decision >= 1 && decision <= 4) // 1=攻击, 2=防御, 3=治疗, 4=逃跑

		t.Logf("AI决策结果: %d (1=攻击, 2=防御, 3=治疗, 4=逃跑)", decision)
	})

	t.Run("物理引擎计算", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv](`
			float x, y, vx, vy, ax, ay, mass, force, dt;
			float gravity, friction, drag, wind_x, wind_y;
			
			gravity = 9.8;
			friction = 0.1;
			drag = 0.02;
			dt = 0.016;
			
			ax = (force * x / mass) + wind_x - (vx * drag) - (vx * friction);
			ay = (force * y / mass) + wind_y - (vy * drag) - (vy * friction) - gravity;
			
			vx = vx + ax * dt;
			vy = vy + ay * dt;
			
			x = x + vx * dt;
			y = y + vy * dt;
			
			(x ^ 2 + y ^ 2) < 10000
		`, s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetFloat64("x", 0.0)
		kv.SetFloat64("y", 100.0)
		kv.SetFloat64("vx", 50.0)
		kv.SetFloat64("vy", 0.0)
		kv.SetFloat64("mass", 10.0)
		kv.SetFloat64("force", 100.0)
		kv.SetFloat64("wind_x", 2.0)
		kv.SetFloat64("wind_y", -1.0)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		inBounds, ok := result.Bool()
		assert.True(t, ok)

		// 验证物理计算后的位置
		newX, exists := kv.Get("x")
		assert.True(t, exists)
		newY, exists := kv.Get("y")
		assert.True(t, exists)

		x, _ := newX.Float64()
		y, _ := newY.Float64()
		t.Logf("物体新位置: (%.2f, %.2f), 在边界内: %v", x, y, inBounds)
	})

	t.Run("统计学计算", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv](`
			float x1, x2, x3, x4, x5, x6, x7, x8, x9, x10;
			float mean, variance, std_dev, sum, sum_sq;
			int n;
			
			n = 10;
			sum = x1 + x2 + x3 + x4 + x5 + x6 + x7 + x8 + x9 + x10;
			mean = sum / n;
			
			sum_sq = (x1 - mean) ^ 2 + (x2 - mean) ^ 2 + (x3 - mean) ^ 2 + 
			         (x4 - mean) ^ 2 + (x5 - mean) ^ 2 + (x6 - mean) ^ 2 + 
			         (x7 - mean) ^ 2 + (x8 - mean) ^ 2 + (x9 - mean) ^ 2 + 
			         (x10 - mean) ^ 2;
			
			variance = sum_sq / (n - 1);
			std_dev = variance ^ 0.5;
			
			std_dev < 5.0
		`, s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		// 设置一组样本数据
		values := []float64{1.2, 2.5, 3.1, 4.8, 5.3, 6.7, 7.2, 8.9, 9.1, 10.4}
		for i, val := range values {
			kv.SetFloat64(fmt.Sprintf("x%d", i+1), val)
		}

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		lowVariance, ok := result.Bool()
		assert.True(t, ok)

		// 验证计算结果
		mean, exists := kv.Get("mean")
		assert.True(t, exists)
		stdDev, exists := kv.Get("std_dev")
		assert.True(t, exists)

		meanVal, _ := mean.Float64()
		stdVal, _ := stdDev.Float64()
		t.Logf("样本均值: %.2f, 标准差: %.2f, 低方差: %v", meanVal, stdVal, lowVariance)
	})

	t.Run("经济模型计算", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv](`
			float price, demand, supply, elasticity, tax_rate, inflation;
			float cost, profit, revenue, market_share, competition_factor;
			bool is_luxury, is_essential, has_monopoly;
			
			float demand_factor, supply_factor, price_adjustment;
			float final_price, expected_profit, market_viability;
			
			demand_factor = is_essential ? 1.2 : (is_luxury ? 0.8 : 1.0);
			supply_factor = has_monopoly ? 1.5 : (1.0 - competition_factor);
			
			price_adjustment = (demand / supply) * demand_factor * supply_factor;
			price_adjustment = price_adjustment * (1.0 + inflation) * (1.0 + tax_rate);
			
			final_price = price * price_adjustment;
			revenue = final_price * demand * market_share;
			expected_profit = revenue - (cost * demand);
			
			market_viability = (expected_profit / revenue) * (elasticity > -1.0 ? 1.0 : 0.5);
			
			market_viability > 0.15
		`, s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetFloat64("price", 100.0)
		kv.SetFloat64("demand", 1000.0)
		kv.SetFloat64("supply", 800.0)
		kv.SetFloat64("elasticity", -0.8)
		kv.SetFloat64("tax_rate", 0.15)
		kv.SetFloat64("inflation", 0.03)
		kv.SetFloat64("cost", 60.0)
		kv.SetFloat64("market_share", 0.25)
		kv.SetFloat64("competition_factor", 0.3)
		kv.SetBool("is_luxury", false)
		kv.SetBool("is_essential", true)
		kv.SetBool("has_monopoly", false)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		viable, ok := result.Bool()
		assert.True(t, ok)

		finalPrice, exists := kv.Get("final_price")
		assert.True(t, exists)
		expectedProfit, exists := kv.Get("expected_profit")
		assert.True(t, exists)
		marketViability, exists := kv.Get("market_viability")
		assert.True(t, exists)

		price, _ := finalPrice.Float64()
		profit, _ := expectedProfit.Float64()
		viability, _ := marketViability.Float64()

		t.Logf("最终价格: %.2f, 预期利润: %.2f, 市场可行性: %.3f, 投资建议: %v",
			price, profit, viability, viable)
	})
}

func TestLargeExpression(t *testing.T) {
	// 测试大型表达式
	t.Run("大型表达式", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv](`
			int player_hp, player_mp, player_level, player_attack, player_defense;
			int enemy_hp, enemy_mp, enemy_level, enemy_attack, enemy_defense;
			float distance, player_speed, enemy_speed;
			bool has_potion, has_spell, is_boss;
			
			float hp_ratio, mp_ratio, level_diff, attack_ratio, defense_ratio;
			bool should_attack, should_defend, should_flee, should_heal;
			
			hp_ratio = player_hp / 100.0;
			mp_ratio = player_mp / 100.0;
			level_diff = player_level - enemy_level;
			attack_ratio = player_attack / enemy_attack;
			defense_ratio = player_defense / enemy_defense;
			
			should_heal = (hp_ratio < 0.3) && has_potion;
			should_flee = (hp_ratio < 0.2) || (level_diff < -5 && !is_boss);
			should_defend = (enemy_attack > player_defense * 1.5) && (hp_ratio < 0.6);
			should_attack = !should_heal && !should_flee && !should_defend && 
			               (mp_ratio > 0.2 || distance < 50.0) && 
			               (attack_ratio > 0.8 || has_spell);
			
			should_attack ? 1 : (should_defend ? 2 : (should_heal ? 3 : 4))
		`, s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		// 设置游戏状态
		kv.SetInt64("player_hp", 70)
		kv.SetInt64("player_mp", 80)
		kv.SetInt64("player_level", 25)
		kv.SetInt64("player_attack", 150)
		kv.SetInt64("player_defense", 120)
		kv.SetInt64("enemy_hp", 100)
		kv.SetInt64("enemy_mp", 60)
		kv.SetInt64("enemy_level", 22)
		kv.SetInt64("enemy_attack", 130)
		kv.SetInt64("enemy_defense", 100)
		kv.SetFloat64("distance", 45.0)
		kv.SetFloat64("player_speed", 10.5)
		kv.SetFloat64("enemy_speed", 8.2)
		kv.SetBool("has_potion", true)
		kv.SetBool("has_spell", true)
		kv.SetBool("is_boss", false)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		decision, ok := result.Int64()
		assert.True(t, ok)
		assert.True(t, decision >= 1 && decision <= 4) // 1=攻击, 2=防御, 3=治疗, 4=逃跑

		t.Logf("AI决策结果: %d (1=攻击, 2=防御, 3=治疗, 4=逃跑)", decision)
	})

	// 测试物理仿真
	t.Run("物理仿真", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv](`
			float x, y, vx, vy, ax, ay, mass, force, dt;
			float gravity, friction, drag, wind_x, wind_y;
			
			gravity = 9.8;
			friction = 0.1;
			drag = 0.02;
			dt = 0.016;
			
			ax = (force * x / mass) + wind_x - (vx * drag) - (vx * friction);
			ay = (force * y / mass) + wind_y - (vy * drag) - (vy * friction) - gravity;
			
			vx = vx + ax * dt;
			vy = vy + ay * dt;
			
			x = x + vx * dt;
			y = y + vy * dt;
			
			(x ^ 2 + y ^ 2) < 10000
		`, s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetFloat64("x", 0.0)
		kv.SetFloat64("y", 100.0)
		kv.SetFloat64("vx", 50.0)
		kv.SetFloat64("vy", 0.0)
		kv.SetFloat64("mass", 10.0)
		kv.SetFloat64("force", 100.0)
		kv.SetFloat64("wind_x", 2.0)
		kv.SetFloat64("wind_y", -1.0)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		inBounds, ok := result.Bool()
		assert.True(t, ok)

		// 验证物理计算后的位置
		newX, exists := kv.Get("x")
		assert.True(t, exists)
		newY, exists := kv.Get("y")
		assert.True(t, exists)

		x, _ := newX.Float64()
		y, _ := newY.Float64()
		t.Logf("物体新位置: (%.2f, %.2f), 在边界内: %v", x, y, inBounds)
	})

	t.Run("统计计算", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv](`
			float x1, x2, x3, x4, x5, x6, x7, x8, x9, x10;
			float mean, variance, std_dev, sum, sum_sq;
			int n;
			
			n = 10;
			sum = x1 + x2 + x3 + x4 + x5 + x6 + x7 + x8 + x9 + x10;
			mean = sum / n;
			
			sum_sq = (x1 - mean) ^ 2 + (x2 - mean) ^ 2 + (x3 - mean) ^ 2 + 
			         (x4 - mean) ^ 2 + (x5 - mean) ^ 2 + (x6 - mean) ^ 2 + 
			         (x7 - mean) ^ 2 + (x8 - mean) ^ 2 + (x9 - mean) ^ 2 + 
			         (x10 - mean) ^ 2;
			
			variance = sum_sq / (n - 1);
			std_dev = variance ^ 0.5;
			
			std_dev < 5.0
		`, s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		// 设置一组样本数据
		values := []float64{1.2, 2.5, 3.1, 4.8, 5.3, 6.7, 7.2, 8.9, 9.1, 10.4}
		for i, val := range values {
			kv.SetFloat64(fmt.Sprintf("x%d", i+1), val)
		}

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		lowVariance, ok := result.Bool()
		assert.True(t, ok)

		// 验证计算结果
		mean, exists := kv.Get("mean")
		assert.True(t, exists)
		stdDev, exists := kv.Get("std_dev")
		assert.True(t, exists)

		meanVal, _ := mean.Float64()
		stdVal, _ := stdDev.Float64()
		t.Logf("样本均值: %.2f, 标准差: %.2f, 低方差: %v", meanVal, stdVal, lowVariance)
	})

	t.Run("经济模型计算", func(t *testing.T) {
		compiledFunc, err := Compile[string, *MockKv](`
			float price, demand, supply, elasticity, tax_rate, inflation;
			float cost, profit, revenue, market_share, competition_factor;
			bool is_luxury, is_essential, has_monopoly;
			
			float demand_factor, supply_factor, price_adjustment;
			float final_price, expected_profit, market_viability;
			
			demand_factor = is_essential ? 1.2 : (is_luxury ? 0.8 : 1.0);
			supply_factor = has_monopoly ? 1.5 : (1.0 - competition_factor);
			
			price_adjustment = (demand / supply) * demand_factor * supply_factor;
			price_adjustment = price_adjustment * (1.0 + inflation) * (1.0 + tax_rate);
			
			final_price = price * price_adjustment;
			revenue = final_price * demand * market_share;
			expected_profit = revenue - (cost * demand);
			
			market_viability = (expected_profit / revenue) * (elasticity > -1.0 ? 1.0 : 0.5);
			
			market_viability > 0.15
		`, s2s)
		assert.Nil(t, err)

		kv := NewMockKv()
		kv.SetFloat64("price", 100.0)
		kv.SetFloat64("demand", 1000.0)
		kv.SetFloat64("supply", 800.0)
		kv.SetFloat64("elasticity", -0.8)
		kv.SetFloat64("tax_rate", 0.15)
		kv.SetFloat64("inflation", 0.03)
		kv.SetFloat64("cost", 60.0)
		kv.SetFloat64("market_share", 0.25)
		kv.SetFloat64("competition_factor", 0.3)
		kv.SetBool("is_luxury", false)
		kv.SetBool("is_essential", true)
		kv.SetBool("has_monopoly", false)

		result, err := compiledFunc(kv)
		assert.Nil(t, err)
		viable, ok := result.Bool()
		assert.True(t, ok)

		finalPrice, exists := kv.Get("final_price")
		assert.True(t, exists)
		expectedProfit, exists := kv.Get("expected_profit")
		assert.True(t, exists)
		marketViability, exists := kv.Get("market_viability")
		assert.True(t, exists)

		price, _ := finalPrice.Float64()
		profit, _ := expectedProfit.Float64()
		viability, _ := marketViability.Float64()

		t.Logf("最终价格: %.2f, 预期利润: %.2f, 市场可行性: %.3f, 投资建议: %v",
			price, profit, viability, viable)
	})
}

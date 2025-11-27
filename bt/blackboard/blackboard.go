// Package blackboard 提供了一个基于 map 的默认 Blackboard 实现
// 用于在行为树节点之间共享数据。
package blackboard

import "github.com/legamerdc/game/lib"

// Blackboard 是一个基于 map[string]lib.Field 的默认黑板实现
// 适用于快速原型开发。行为树串行执行，无需加锁。
type Blackboard struct {
	data map[string]lib.Field
}

// New 创建一个新的 Blackboard 实例
func New() *Blackboard {
	return &Blackboard{
		data: make(map[string]lib.Field),
	}
}

// Get 获取指定 key 的值
func (b *Blackboard) Get(key string) (lib.Field, bool) {
	v, ok := b.data[key]
	return v, ok
}

// Set 设置指定 key 的值
func (b *Blackboard) Set(key string, value lib.Field) {
	b.data[key] = value
}

// Del 删除指定 key
func (b *Blackboard) Del(key string) {
	delete(b.data, key)
}

// Has 检查是否存在指定 key
func (b *Blackboard) Has(key string) bool {
	_, ok := b.data[key]
	return ok
}

// Clear 清空所有数据
func (b *Blackboard) Clear() {
	b.data = make(map[string]lib.Field)
}

// Keys 返回所有的 key
func (b *Blackboard) Keys() []string {
	keys := make([]string, 0, len(b.data))
	for k := range b.data {
		keys = append(keys, k)
	}
	return keys
}

// Len 返回黑板中的条目数量
func (b *Blackboard) Len() int {
	return len(b.data)
}

// GetInt32 获取 int32 类型的值
func (b *Blackboard) GetInt32(key string) (int32, bool) {
	v, ok := b.Get(key)
	if !ok {
		return 0, false
	}
	return v.Int32()
}

// GetInt64 获取 int64 类型的值
func (b *Blackboard) GetInt64(key string) (int64, bool) {
	v, ok := b.Get(key)
	if !ok {
		return 0, false
	}
	return v.Int64()
}

// GetFloat32 获取 float32 类型的值
func (b *Blackboard) GetFloat32(key string) (float32, bool) {
	v, ok := b.Get(key)
	if !ok {
		return 0, false
	}
	return v.Float32()
}

// GetFloat64 获取 float64 类型的值
func (b *Blackboard) GetFloat64(key string) (float64, bool) {
	v, ok := b.Get(key)
	if !ok {
		return 0, false
	}
	return v.Float64()
}

// GetBool 获取 bool 类型的值
func (b *Blackboard) GetBool(key string) (bool, bool) {
	v, ok := b.Get(key)
	if !ok {
		return false, false
	}
	return v.Bool()
}

// GetAny 使用泛型获取 Any 类型中的指定类型值
func GetAny[T any](b *Blackboard, key string) (T, bool) {
	v, ok := b.Get(key)
	if !ok {
		var zero T
		return zero, false
	}
	return lib.TakeAny[T](&v)
}

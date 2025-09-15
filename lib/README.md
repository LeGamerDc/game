# Lib 数据结构库

这是一个高性能的Go语言数据结构库，提供了四种不同特性的映射数据结构，每种都针对特定的使用场景进行了优化。

## 数据结构概览

### 1. ArrayMap[K, V] - 简单数组映射

基于两个并行数组实现的简单键值映射，保持插入顺序。

**特点:**
- 线性查找，时间复杂度 O(n)
- 内存紧凑，缓存友好
- 保持插入顺序
- 适用于小数据集（< 100个元素）

**主要方法:**
```go
func (m *ArrayMap[K, V]) Get(k K) (int, V)        // 获取键对应的值
func (m *ArrayMap[K, V]) Push(k K, v V)           // 添加键值对（不检查重复）
func (m *ArrayMap[K, V]) Remove(i int)            // 移除指定索引的元素
func (m *ArrayMap[K, V]) Iter(f func(K, V) bool)  // 遍历所有元素
```

### 2. IndexMap[K, V] - 索引映射

结合内置map和数组的优势，既能快速查找又保持插入顺序。

**特点:**
- 快速查找，时间复杂度 O(1)
- 保持插入顺序
- 支持键的重复检查和更新
- 适用于需要频繁查找的场景

**主要方法:**
```go
func (m *IndexMap[K, V]) Get(k K) (int, V)        // O(1)查找
func (m *IndexMap[K, V]) Put(k K, v V)            // 插入或更新键值对
func (m *IndexMap[K, V]) Remove(i int)            // 移除指定索引的元素
func (m *IndexMap[K, V]) Iter(f func(V))          // 按顺序遍历值
```

### 3. HeapArrayMap[K, S, V] - 堆数组映射

基于数组实现的优先队列映射，支持按排序键进行堆排序。

**特点:**
- 支持优先级排序（最小堆）
- 线性查找键，时间复杂度 O(n)
- 支持动态更新优先级
- 适用于优先队列场景

**主要方法:**
```go
func (m *HeapArrayMap[K, S, V]) Get(k K) (_ int, v V)
func (m *HeapArrayMap[K, S, V]) GetP(k K) (_ int, v *V)
func (m *HeapArrayMap[K, S, V]) Update(i int, s S)
func (m *HeapArrayMap[K, S, V]) Remove(i int)
func (m *HeapArrayMap[K, S, V]) Push(k K, v V, s S)
func (m *HeapArrayMap[K, S, V]) Top() (int, K, V, S)
func (m *HeapArrayMap[K, S, V]) Pop()
func (m *HeapArrayMap[K, S, V]) Iter(f func(k K, v V) (stop bool))
```

### 4. HeapIndexMap[K, S, V] - 堆索引映射

终极数据结构，结合了快速查找和优先队列的所有优势。

**特点:**
- 快速查找，时间复杂度 O(1)
- 支持优先级排序（最小堆）
- 支持键的重复检查和更新
- 最全面的功能，适用于复杂场景

**主要方法:**
```go
func (m *HeapIndexMap[K, S, V]) Push(k K, v V, s S)
func (m *HeapIndexMap[K, S, V]) Get(k K) (int, V)
func (m *HeapIndexMap[K, S, V]) GetP(k K) (_ int, v *V)
func (m *HeapIndexMap[K, S, V]) Update(i int, s S)
func (m *HeapIndexMap[K, S, V]) Remove(i int)
func (m *HeapIndexMap[K, S, V]) Top() (int, K, V, S)
func (m *HeapIndexMap[K, S, V]) Pop()
func (m *HeapIndexMap[K, S, V]) Iter(f func(V))
func (m *HeapIndexMap[K, S, V]) Filter(f func(V) bool)

```

## 性能对比

| 数据结构 | 查找 | 插入 | 删除 | 内存占用 | 适用场景 |
|---------|------|------|------|----------|----------|
| ArrayMap | O(n) | O(1) | O(1) | 最少 | 小数据集，简单场景 |
| IndexMap | O(1) | O(1) | O(1) | 中等 | 需要快速查找和顺序 |
| HeapArrayMap | O(n) | O(log n) | O(log n) | 中等 | 优先队列，小数据集 |
| HeapIndexMap | O(1) | O(log n) | O(log n) | 最多 | 优先队列，大数据集 |

## 使用建议

1. **ArrayMap**: 当数据量小（<100）且不需要频繁查找时使用
2. **IndexMap**: 当需要快速查找且要保持插入顺序时使用
3. **HeapArrayMap**: 当需要优先队列但数据量不大时使用
4. **HeapIndexMap**: 当需要优先队列且要支持快速查找时使用

## 类型参数说明

- `K comparable`: 键的类型，必须可比较
- `V any`: 值的类型，可以是任意类型
- `S cmp.Ordered`: 排序键的类型，必须支持排序比较（如int, float64, string等）

## 示例用法

```go
// 简单映射
var am ArrayMap[string, int]
am.Push("key1", 100)

// 索引映射  
var im IndexMap[string, int]
im.Init(10)
im.Put("key1", 100)

// 优先队列映射
var ham HeapArrayMap[string, float64, int]
ham.Push("task1", 100, 1.5)  // 值100，优先级1.5

// 全功能映射
var him HeapIndexMap[string, float64, int]
him.Init(10)
him.Put("task1", 100, 1.5)
```

所有数据结构都支持泛型，提供类型安全和高性能的数据操作。

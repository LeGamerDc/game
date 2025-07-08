# Calc 数值表达式动态运行库分享

## 背景

在游戏开发中，特别是在技能系统和行为树中，我们常常和策划合作，通过配置的方式来驱动一些逻辑。配置中常常出现一些数值表达式（读变量，写变量，变量表达式例如 `NeedFlee = Hp / MaxHp < 0.35` ）。

在实现这些数值表达式时，一般会选择动态解析表达式执行或者生成代码执行：

- **动态解析表达式**：每次执行时都需要解析表达式语法树，性能不友好
- **生成代码执行**：虽然性能优秀，但阻碍了配置动态化的开发流程

这里，我们引入 **calc 数值表达式动态运行库**，用接近生成代码的性能实现动态化解析执行表达式的功能。

## 接口设计

### 基本使用

```go
f, _ := Compile[*MockKv]("int x, y, z; x + y ^ z")
kv := NewMockKv()
kv.SetInt64("x", 2)
kv.SetInt64("y", 3)
kv.SetInt64("z", 2) 
v, _ := f(kv)
fmt.Println(v.Int64()) // 2 + 3^2 = 2 + 9 = 11
```

### 核心概念

用户需要将一个数值表达式编译成一个函数 `func(Ctx) (Field, error)`，然后还需要一个支持 Set/Get 变量的黑板。这个编译后的函数就可以通过黑板提取变量值并计算了，函数的返回值取决于最后一个表达式的返回值。

### 黑板接口

```go
type Ctx[K any] interface {
    Get(K) (lib.Field, bool)
    Set(K, lib.Field)
    Exec(string, ...lib.Field) (lib.Field, bool)
}
```

- **Get/Set**：对变量的读取和设置，变量使用 Field 类型，支持类型自动转换
- **Exec**：函数调用接口，支持可变参数传递
- **泛型K**：支持用户自选Key类型，提供更好的类型安全性

Ctx 交给用户自己实现，这样可以嵌入各种已有的工程。

## 支持的语法

### 语法规则

```
PROGRAM: STATEMENT [; STATEMENT]
STATEMENT:
    VAR_DEFINE |
    ASSIGNMENT |
    EXPR
VAR_DEFINE: TYPE VAR_LIST
ASSIGNMENT: TOKEN = EXPR
EXPR:
    UNARY_EXPR |
    BINARY_EXPR |
    TERNARY_EXPR |
    FUNC_CALL |
    IDENT |
    IDENT!
UNARY_EXPR: OP EXPR
BINARY_EXPR: EXPR1 OP EXPR2
TERNARY_EXPR: EXPR ? EXPR1 : EXPR2
FUNC_CALL: IDENT() | IDENT(EXPR_LIST)
EXPR_LIST: EXPR [, EXPR]
OP: + | - | ! | ^ | * | / | % | || | && | == | != | < | <= | > | >=
TYPE: int | float | bool 
```

### 运算符优先级

从高到低：
1. `!`（逻辑非）、`+`/`-`（一元）
2. `^`（幂运算）
3. `*`、`/`、`%`
4. `+`、`-`（二元）
5. `<`、`<=`、`>`、`>=`
6. `==`、`!=`
7. `&&`（逻辑与）
8. `||`（逻辑或）
9. `?:`（三目运算符）

### 示例

```go
// 简单运算
f, _ := Compile[*MockKv]("int x,y,z; (x+y)*z")

// 赋值操作
f, _ := Compile[*MockKv]("float x,y,z; z=x*y; z > 9")

// 逻辑运算
f, _ := Compile[*MockKv]("float hp, hp_max, dist; (hp / hp_max > 0.35) && dist < 190")

// 三元运算符
f, _ := Compile[*MockKv]("int x; x > 0 ? x : -x")

// 函数调用
f, _ := Compile[*MockKv]("int x; x = 5; sqrt(x * x + 16)")  // 调用sqrt函数，参数为x*x+16

// 变量读取模式
f, _ := Compile[*MockKv]("int x, y!; x + y")  // x不存在时使用0，y不存在时报错
```

## 核心原理

### 1. 表达式树

Compile 首先把 `x + y ^ z` 解析为一颗表达式树：

```
     +
   /   \
  x     ^
       / \
      y   z
```

然后将表达式树生成一个函数（编译）。表达式解析使用的是 **yacc(语法分析)+lex(词法分析)** 的技术。

### 2. 编译流程

表达式树转函数的过程：

1. **词法分析**：将输入字符串分解为 token
2. **语法分析**：使用 yacc 生成抽象语法树
3. **变量分析**：提取变量声明信息
4. **类型推断**：自动推断表达式类型
5. **类型传播**：进行类型兼容性检查
6. **代码生成**：生成可执行的 Go 函数

核心思想是根据表达式树的每个节点，递归地生成对应的计算函数，最终组合成一个完整的执行函数。

### 3. 类型系统

#### 为什么需要类型系统？

1. **性能优化**：避免所有的函数都以 float64 计算，针对不同类型生成最优的计算代码
2. **编译期错误检测**：在编译期就发现表达式存在的类型问题

#### 类型规则

为了减轻使用者的负担，我们使用非常简单的类型系统（int, float, bool）：

- **int** 类型：可以自动转换为 float 或在比较运算中转换为 bool
- **float** 类型：浮点数运算
- **bool** 类型：逻辑运算结果

#### 类型推断示例

```go
// 编译期错误检测
bool x; float y; x == y  // 会在编译期报错，类型不兼容

// 自动类型推断
int x; float y; x + y    // 结果会自动推断为 float 类型
```

### 4. 变量存储

Ctx 的定义提供了完整的变量操作能力：

```go
type Ctx[K any] interface {
    Get(K) (lib.Field, bool)             // 获取变量
    Set(K, lib.Field)                    // 设置变量
    Exec(string, ...lib.Field) (lib.Field, bool) // 函数调用
}
```

其中 Field 类型支持类型自动转换，K是泛型参数允许用户自选Key类型，用户可以根据实际需要实现 Ctx 接口。

**变量读取模式**：
- `x` - 安全读取：如果变量不存在，使用对应类型的零值（int:0, float:0.0, bool:false）
- `x!` - 强制读取：如果变量不存在，抛出运行时错误

### 5. 函数调用支持

Ctx 中的 `Exec` 接口用来调用函数，现在支持直接传递参数：

**函数调用语法**：
- `func()` - 无参数函数调用
- `func(expr1, expr2, ...)` - 带参数函数调用

**参数传递机制**：
- 函数参数会被计算后作为可变参数直接传递给 `Exec(string, ...lib.Field)` 接口
- 函数实现者可以直接使用传入的参数，无需通过临时变量

```go
f, _ := Compile[*MockKv]("int x; x = 5; sqrt(x * x + 16)") 
// sqrt函数会接收到参数值41 (5*5+16)
kv := NewMockKv()
v, _ := f(kv)
fmt.Println(v.Float64()) // sqrt(41) ≈ 6.4
```

## 性能优化

### 编译时优化

1. **类型特化**：针对不同的类型组合生成专门的计算函数
2. **常量折叠**：编译期计算常量表达式
3. **函数内联**：对小函数进行内联优化
4. **无用代码消除**：去除不会影响结果的计算

### 运行时优化

1. **避免动态解析**：表达式只解析一次，后续重复执行编译后的函数
2. **类型转换优化**：减少不必要的类型转换
3. **内存池**：复用临时变量存储，减少GC压力

### 性能基准

通过基准测试，接近手写代码的性能。

## 思路来源

思路主要来源于字节的 **sonic 库**。sonic 是一个高性能的 Go JSON 库，它在 Unmarshal 时：

1. 分析目标结构体，按字段展开成一颗树
2. 根据这棵树生成一个专用的 Unmarshal 函数
3. 反复使用这个函数解析 JSON，性能非常高
4. 中间没有任何动态代码

### 适用条件

这种技术适用于满足以下条件的场景：

1. **树状特征**：功能具有树的特征，可以根据节点递归地生成函数
2. **单一入口**：最终只有一个函数入口（像行为树这样多入口的就不适用）
3. **预计算价值**：耗时的逻辑可以在编译期计算好，这样才有优化意义

### 其他应用场景

- **配置驱动的规则引擎**
- **数学表达式计算器**
- **条件判断系统**
- **数据转换和验证**

## 实际应用

### 游戏技能系统

```go
// 技能释放条件判断
f, _ := Compile[*GameCtx](`
    int mp, mp_cost, cooldown, current_time;
    float hp_ratio;
    bool can_use_skill;
    
    hp_ratio = hp / max_hp;
    can_use_skill = (mp >= mp_cost) && 
                   (current_time >= cooldown) && 
                   (hp_ratio > 0.3);
    can_use_skill
`)
```

### 行为树条件节点

```go
// NPC 逃跑判断
f, _ := Compile[*AICtx](`
    float hp_ratio, distance_to_enemy;
    bool should_flee;
    
    hp_ratio = current_hp / max_hp;
    should_flee = (hp_ratio < 0.35) || (distance_to_enemy < 50);
    should_flee
`)
```

## 总结

calc 库通过编译时优化和类型特化，在保持动态性的同时实现了接近静态代码的性能。它特别适用于：

- 需要频繁执行的数值计算
- 配置驱动的游戏逻辑
- 复杂的条件判断系统

通过这种预编译的方式，我们既获得了脚本的灵活性，又保持了原生代码的性能，是一个很好的性能与开发效率的平衡点。

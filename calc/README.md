# Calc 表达式解析编译器

Calc 是一个基于 Go 语言的表达式解析编译器，支持算术运算、逻辑运算、变量声明和赋值等功能。它使用 yacc 语法解析器来构建抽象语法树（AST），并通过类型推断和编译优化生成可执行的函数。

## 主要功能

- **表达式解析**：支持复杂的数学和逻辑表达式
- **变量声明**：支持 `int`、`float`、`bool` 三种基本数据类型
- **变量赋值**：支持变量赋值操作
- **类型推断**：自动进行类型推断和转换
- **编译优化**：将表达式编译为高效的可执行函数
- **错误检查**：完整的语法和类型错误检查

## 支持的语法

### 数据类型
- `int`：整数类型
- `float`：浮点数类型  
- `bool`：布尔类型（true/false）

### 变量声明
```go
int x               // 声明单个整数变量
float y, z          // 声明多个浮点数变量
bool flag           // 声明布尔变量
bool x, z; float h; int p, q  // 混合声明
```

### 算术运算符
- `+`：加法
- `-`：减法
- `*`：乘法
- `/`：除法
- `%`：取模（仅支持整数）
- `^`：幂运算

### 比较运算符
- `==`：等于
- `!=`：不等于
- `<`：小于
- `<=`：小于等于
- `>`：大于
- `>=`：大于等于

### 逻辑运算符
- `&&`：逻辑与
- `||`：逻辑或
- `!`：逻辑非

### 一元运算符
- `+`：正号
- `-`：负号
- `!`：逻辑非

### 三目运算符
```go
condition ? value_if_true : value_if_false
```

### 赋值运算符
```go
x = expression
```

### 运算符优先级（从高到低）
1. `!`（逻辑非）、`+`/`-`（一元）
2. `^`（幂运算）
3. `*`、`/`、`%`
4. `+`、`-`（二元）
5. `<`、`<=`、`>`、`>=`
6. `==`、`!=`
7. `&&`（逻辑与）
8. `||`（逻辑或）
9. `?:`（三目运算符）

## 使用方法

### 基本使用
```go
package main

import (
    "fmt"
	
    "github.com/legamerdc/game/blackboard"
)

// MockKv 实现 Kv 接口用于测试
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

func (m *MockKv) SetInt64(key string, value int64) {
	m.data[key] = blackboard.Int64(value)
}

func (m *MockKv) SetFloat64(key string, value float64) {
	m.data[key] = blackboard.Float64(value)
}

func (m *MockKv) SetBool(key string, value bool) {
	m.data[key] = blackboard.Bool(value)
}

func main() {
    // 编译表达式
    compiledFunc, err := calc.Compile[*MockKv]("x + y * 2")
    if err != nil {
        panic(err)
    }
    
    // 创建变量存储
    bb := NewMockKv()
    bb.SetInt64("x", 10)
    bb.SetInt64("y", 5)
    
    // 执行表达式
    result, err := compiledFunc(bb)
    if err != nil {
        panic(err)
    }
    
    fmt.Println(result.Int64()) // 输出: 20
}
```

### 复杂表达式示例
```go
// 算术表达式
"(x + y) * z"
"x ^ 2 + y ^ 2"

// 逻辑表达式  
"x > 0 && y < 10"
"flag || (x == y)"

// 三目运算符
"x > 0 ? x : -x"
"score >= 60 ? true : false"

// 变量声明和赋值
"int x, y; x = 10; y = x * 2"
"float pi; pi = 3.14159"
"bool result; result = x > y"

// 混合表达式
"int x, y, z; z = (x + 3) * (y + 2); z % 2 == 0"
```

## 错误处理

库提供了完整的错误检查机制：

### 语法错误
```go
// 不支持的操作符
"x & y"     // 错误：不支持位运算
"x !=< y"   // 错误：非法的比较运算符
```

### 类型错误
```go
// 类型不匹配
"true + 5"      // 错误：布尔值不能参与算术运算
"x && 3.14"     // 错误：数字不能参与逻辑运算
```

### 变量错误
```go
// 未声明的变量类型
"pp x, y, z"    // 错误：未知的变量类型
```

## 内部实现

### 编译流程
1. **词法分析**：将输入字符串分解为 token
2. **语法分析**：使用 yacc 生成抽象语法树
3. **变量分析**：提取变量声明信息
4. **类型推断**：自动推断表达式类型
5. **类型传播**：进行类型兼容性检查
6. **代码生成**：生成可执行的 Go 函数

### 核心文件说明
- `g.y`：yacc 语法定义文件
- `y.go`：yacc 生成的解析器代码
- `compiler.go`：编译器核心实现
- `infect.go`：类型推断和传播
- `dfs.go`：语法树遍历工具
- `compiler_test.go`：测试用例

## 注意事项

1. **类型安全**：所有类型转换都是显式的，避免了隐式类型转换的问题
2. **性能优化**：表达式被编译为原生 Go 函数，执行效率高
3. **内存管理**：使用 blackboard 模式管理变量，避免内存泄漏
4. **错误友好**：提供详细的错误信息，便于调试

## 依赖项

- `github.com/legamerdc/game/blackboard`：变量存储和管理
- `github.com/stretchr/testify/assert`：测试断言库

## 许可证

本项目采用开源许可证，具体请参考项目根目录的 LICENSE 文件。 
# Calc 表达式解析编译器

Calc 是一个基于 Go 语言的表达式解析编译器，支持算术运算、逻辑运算、变量声明和赋值等功能。它使用 yacc 语法解析器来构建抽象语法树（AST），并通过类型推断和编译优化生成可执行的函数。

Calc 是一个动态数值表达式编译器，将一段数值表达式代码编译成一个闭包函数，在支持类似动态脚本的灵活性的同时，提供接近原生代码的性能。

```go
f, _ := Compile("int hp,hp_max; hp / hp_max < 0.35")
v, _ := f()
v.Bool() // hp / hp_max < 0.35 ?
```

## 主要功能

- **yacc 解析表达式**：支持一元、二元、三元操作符，括号表达式，赋值
- **变量声明**：用户可以设定读取、存储黑板变量的类型，Calc会根据提供的类型生成性能最高的执行程序
- **类型推断**：Calc 会自动推断剩余部分的类型，如果发现冲突如 `bool x;float y;x==y` 会编译器报错。类型推断会尽量保证生成高性能程序
- **编译检测**：编译器将检查语法错误，类型错误，避免后续执行浪费运行时cpu
- **编译优化**：Calc 做了大量编译优化，保证运行期性能。

## 语法介绍

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
    TERNARY_EXPR
UNARY_EXPR: OP EXPR
BINARY_EXPR: EXPR1 OP EXPR2
TERNARY_EXPR: EXPR ? EXPR1 : EXPR2
OP: + | - | ! | ^ | * | / | % | || | && | == | != | < | <= | > | >=
TYPE: int | float | bool 
```

介绍语法时，可以参考上述描述文件结合来看

- 整个程序由多个语句组成，之间用;隔开，程序的返回值由最后一个语句决定。
- 语句可以分为类型定义、赋值、表达式
- 类型定义为类型定义符加上,隔开的变量名字。如 "int x", "float x,y,z"
- 赋值为变量名字 = 表达式，赋值的变量会被存储回黑板中。例如 "x = y+3"
- 表达式为一元、二元、三元操作符递归组成。例如 "!(x || y)", "(x>3) && (y<7)", "x>3 ? y : (z+3)%7"
- 支持的类型包括 int, float, bool。其中 int 在结合时可以根据情况转换为 float 或 bool。
- 表达式中可以自由嵌入括号，提升执行顺序。
- 表达式中如果读取了变量，用户需要保证黑板中存在变量，否则会报错找不到变量。

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

## 示例

```go
blackboard := NewMockKv()

// 简单运算
blackboard.SetInt64(x, 1)
blackboard.SetInt64(y, 2)
blackboard.SetInt64(z, 3)
f, _ := Compile("int x,y,z; (x+y)*z")
v, _ := f()
v.Int64() // 9

// 赋值
blackboard.SetFloat64(x, 2.5)
blackboard.SetFloat64(y, 4)
f, _ := Compile("float x,y,z; z=x*y; z > 9")
v, _ = f()
v.Bool() // true 10>9
fz, _ := blackboard.Get("z")
z, _ := fz.Float64() // 10.0

// 三元操作
f, _ := Compile("float x,y,z; z = x > 3 ? y : y+2; z > 5")

// bool 计算
f, _ := Compile("float hp, hp_max, dist; (hp / hp_max > 0.35) && dist < 190")
```

错误语法举例：

```
x & y       // 不支持的操作符
x != < y    // 无效操作
int x, y; x + y + z     // 没有找到变量类型定义
true + 3.15 // 错误的类型计算
string x, y // 不支持的类型
```

## 实现原理

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
- `compiler_test.go`：测试用例

## 注意事项

1. **类型安全**：所有类型转换都是显式的，避免了隐式类型转换的问题
2. **性能优化**：表达式被编译为原生 Go 函数，执行效率高
3. **内存管理**：使用 blackboard 模式管理变量，避免内存泄漏
4. **错误友好**：提供详细的错误信息，便于调试

## 许可证

本项目采用开源许可证，具体请参考项目根目录的 LICENSE 文件。 
%{
package calc

import (
    "fmt"
    "strconv"

)

// 语法树节点类型
type NodeType int
const (
    NodeProgram NodeType = iota
    NodeVarDecl
    NodeAssign
    NodeExpr
    NodeBinOp
    NodeUnaryOp
    NodeTernary
    NodeIdent
    NodeNumber
    NodeBool
)

// 语法树节点
type Node struct {
    Type     NodeType
    Target   exprType
    Token    string
    Children []*Node
}

// 全局变量存储解析结果
var parseResult *Node
%}

%union {
    node    *Node
    str     string
    num     float64
    bool    bool
}

%token <str> IDENT
%token <str> NUMBER
%token <bool> BOOLEAN
%token INT FLOAT BOOL
%token TRUE FALSE
%token ASSIGN
%token SEMICOLON
%token COMMA
%token LPAREN RPAREN
%token QUESTION COLON
%token PLUS MINUS MULTIPLY DIVIDE MOD POWER
%token AND OR NOT
%token LT LE GT GE EQ NE

%type <node> program
%type <node> statement_list
%type <node> statement
%type <node> var_decl
%type <str> var_list
%type <node> assignment
%type <node> expr
%type <node> ternary_expr
%type <node> logical_or_expr
%type <node> logical_and_expr
%type <node> equality_expr
%type <node> relational_expr
%type <node> additive_expr
%type <node> multiplicative_expr
%type <node> power_expr
%type <node> unary_expr
%type <node> primary_expr
%type <str> type_name

%right QUESTION COLON
%left OR
%left AND
%left EQ NE
%left LT LE GT GE
%left PLUS MINUS
%left MULTIPLY DIVIDE MOD
%right POWER
%right NOT UMINUS UPLUS

%%

program:
    statement_list
    {
        if $1.Type == NodeProgram {
            $$ = $1
        } else {
            $$ = &Node{Type: NodeProgram, Children: []*Node{$1}}
        }
        parseResult = $$
    }
;

statement_list:
    statement
    {
        $$ = $1
    }
|   statement_list SEMICOLON statement
    {
        // 如果第一个节点不是NodeProgram类型，创建一个包装节点
        if $1.Type != NodeProgram {
            $$ = &Node{Type: NodeProgram, Children: []*Node{$1, $3}}
        } else {
            $1.Children = append($1.Children, $3)
            $$ = $1
        }
    }
;

statement:
    var_decl        { $$ = $1 }
|   assignment      { $$ = $1 }
|   expr            { $$ = $1 }
;

var_decl:
    type_name var_list
    {
        $$ = &Node{
            Type: NodeVarDecl,
            Token: $1 + ":" + $2,
        }
    }
;

var_list:
    IDENT
    {
        $$ = $1
    }
|   var_list COMMA IDENT
    {
        $$ = $1 + "," + $3
    }
;

type_name:
    INT     { $$ = "int" }
|   FLOAT   { $$ = "float" }
|   BOOL    { $$ = "bool" }
;

assignment:
    IDENT ASSIGN expr
    {
        $$ = &Node{
            Type: NodeAssign,
            Token: $1,
            Children: []*Node{$3},
        }
    }
;

expr:
    ternary_expr    { $$ = $1 }
;

ternary_expr:
    logical_or_expr
    {
        $$ = $1
    }
|   logical_or_expr QUESTION expr COLON ternary_expr
    {
        $$ = &Node{
            Type: NodeTernary,
            Token: "?:",
            Children: []*Node{$1, $3, $5},
        }
    }
;

logical_or_expr:
    logical_and_expr
    {
        $$ = $1
    }
|   logical_or_expr OR logical_and_expr
    {
        $$ = &Node{
            Type: NodeBinOp,
            Token: "||",
            Children: []*Node{$1, $3},
        }
    }
;

logical_and_expr:
    equality_expr
    {
        $$ = $1
    }
|   logical_and_expr AND equality_expr
    {
        $$ = &Node{
            Type: NodeBinOp,
            Token: "&&",
            Children: []*Node{$1, $3},
        }
    }
;

equality_expr:
    relational_expr
    {
        $$ = $1
    }
|   equality_expr EQ relational_expr
    {
        $$ = &Node{
            Type: NodeBinOp,
            Token: "==",
            Children: []*Node{$1, $3},
        }
    }
|   equality_expr NE relational_expr
    {
        $$ = &Node{
            Type: NodeBinOp,
            Token: "!=",
            Children: []*Node{$1, $3},
        }
    }
;

relational_expr:
    additive_expr
    {
        $$ = $1
    }
|   relational_expr LT additive_expr
    {
        $$ = &Node{
            Type: NodeBinOp,
            Token: "<",
            Children: []*Node{$1, $3},
        }
    }
|   relational_expr LE additive_expr
    {
        $$ = &Node{
            Type: NodeBinOp,
            Token: "<=",
            Children: []*Node{$1, $3},
        }
    }
|   relational_expr GT additive_expr
    {
        $$ = &Node{
            Type: NodeBinOp,
            Token: ">",
            Children: []*Node{$1, $3},
        }
    }
|   relational_expr GE additive_expr
    {
        $$ = &Node{
            Type: NodeBinOp,
            Token: ">=",
            Children: []*Node{$1, $3},
        }
    }
;

additive_expr:
    multiplicative_expr
    {
        $$ = $1
    }
|   additive_expr PLUS multiplicative_expr
    {
        $$ = &Node{
            Type: NodeBinOp,
            Token: "+",
            Children: []*Node{$1, $3},
        }
    }
|   additive_expr MINUS multiplicative_expr
    {
        $$ = &Node{
            Type: NodeBinOp,
            Token: "-",
            Children: []*Node{$1, $3},
        }
    }
;

multiplicative_expr:
    power_expr
    {
        $$ = $1
    }
|   multiplicative_expr MULTIPLY power_expr
    {
        $$ = &Node{
            Type: NodeBinOp,
            Token: "*",
            Children: []*Node{$1, $3},
        }
    }
|   multiplicative_expr DIVIDE power_expr
    {
        $$ = &Node{
            Type: NodeBinOp,
            Token: "/",
            Children: []*Node{$1, $3},
        }
    }
|   multiplicative_expr MOD power_expr
    {
        $$ = &Node{
            Type: NodeBinOp,
            Token: "%",
            Children: []*Node{$1, $3},
        }
    }
;

power_expr:
    unary_expr
    {
        $$ = $1
    }
|   unary_expr POWER power_expr
    {
        $$ = &Node{
            Type: NodeBinOp,
            Token: "^",
            Children: []*Node{$1, $3},
        }
    }
;

unary_expr:
    primary_expr
    {
        $$ = $1
    }
|   PLUS unary_expr %prec UPLUS
    {
        $$ = &Node{
            Type: NodeUnaryOp,
            Token: "+",
            Children: []*Node{$2},
        }
    }
|   MINUS unary_expr %prec UMINUS
    {
        $$ = &Node{
            Type: NodeUnaryOp,
            Token: "-",
            Children: []*Node{$2},
        }
    }
|   NOT unary_expr
    {
        $$ = &Node{
            Type: NodeUnaryOp,
            Token: "!",
            Children: []*Node{$2},
        }
    }
;

primary_expr:
    IDENT
    {
        $$ = &Node{
            Type: NodeIdent,
            Token: $1,
        }
    }
|   NUMBER
    {
        $$ = &Node{
            Type: NodeNumber,
            Token: $1,
        }
    }
|   TRUE
    {
        $$ = &Node{
            Type: NodeBool,
            Token: "true",
        }
    }
|   FALSE
    {
        $$ = &Node{
            Type: NodeBool,
            Token: "false",
        }
    }
|   LPAREN expr RPAREN
    {
        $$ = $2
    }
;

%%

// 词法分析器接口
type Lexer interface {
    Lex(lval *yySymType) int
    Error(s string)
}

// 简单的词法分析器实现
type SimpleLexer struct {
    input string
    pos   int
    line  int
}

func NewLexer(input string) *SimpleLexer {
    return &SimpleLexer{input: input, pos: 0, line: 1}
}

func (l *SimpleLexer) Error(s string) {
    fmt.Printf("语法错误 (行 %d): %s\n", l.line, s)
}

func (l *SimpleLexer) Lex(lval *yySymType) int {
    for l.pos < len(l.input) {
        ch := l.input[l.pos]
        
        // 跳过空白字符
        if ch == ' ' || ch == '\t' || ch == '\r' {
            l.pos++
            continue
        }
        
        if ch == '\n' {
            l.line++
            l.pos++
            continue
        }
        
        // 识别各种token
        switch ch {
        case ';':
            l.pos++
            return SEMICOLON
        case '=':
            if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
                l.pos += 2
                return EQ
            }
            l.pos++
            return ASSIGN
        case '(':
            l.pos++
            return LPAREN
        case ')':
            l.pos++
            return RPAREN
        case '+':
            l.pos++
            return PLUS
        case '-':
            l.pos++
            return MINUS
        case '*':
            l.pos++
            return MULTIPLY
        case '/':
            l.pos++
            return DIVIDE
        case '%':
            l.pos++
            return MOD
        case '^':
            l.pos++
            return POWER
        case ',':
            l.pos++
            return COMMA
        case '?':
            l.pos++
            return QUESTION
        case ':':
            l.pos++
            return COLON
        case '!':
            if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
                l.pos += 2
                return NE
            }
            l.pos++
            return NOT
        case '<':
            if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
                l.pos += 2
                return LE
            }
            l.pos++
            return LT
        case '>':
            if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
                l.pos += 2
                return GE
            }
            l.pos++
            return GT
        case '&':
            if l.pos+1 < len(l.input) && l.input[l.pos+1] == '&' {
                l.pos += 2
                return AND
            }
            // 单独的 & 字符是非法的，跳过并报告错误
            l.Error(fmt.Sprintf("unexpected character '&' at position %d", l.pos))
            l.pos++
            continue
        case '|':
            if l.pos+1 < len(l.input) && l.input[l.pos+1] == '|' {
                l.pos += 2
                return OR
            }
            // 单独的 | 字符是非法的，跳过并报告错误
            l.Error(fmt.Sprintf("unexpected character '|' at position %d", l.pos))
            l.pos++
            continue
        }
        
        // 识别数字（包括以点开头的小数）
        if (ch >= '0' && ch <= '9') || 
           (ch == '.' && l.pos+1 < len(l.input) && l.input[l.pos+1] >= '0' && l.input[l.pos+1] <= '9') {
            return l.lexNumber(lval)
        }
        
        // 识别标识符和关键字
        if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' {
            return l.lexIdent(lval)
        }
        
        // 未识别的字符，报告错误并跳过
        l.Error(fmt.Sprintf("unexpected character '%c' at position %d", ch, l.pos))
        l.pos++
    }
    return 0 // EOF
}

func (l *SimpleLexer) lexNumber(lval *yySymType) int {
    start := l.pos
    hasDot := false
    hasDigit := false
    
    for l.pos < len(l.input) {
        ch := l.input[l.pos]
        if ch >= '0' && ch <= '9' {
            hasDigit = true
            l.pos++
        } else if ch == '.' && !hasDot {
            hasDot = true
            l.pos++
        } else {
            break
        }
    }
    
    numStr := l.input[start:l.pos]
    
    // 检查是否是有效的数字格式
    if numStr == "" || numStr == "." || !hasDigit {
        // 回退位置，将其作为未识别字符处理
        l.pos = start + 1
        l.Error(fmt.Sprintf("invalid number format '%s' at position %d", 
                          l.input[start:l.pos], start))
        return l.Lex(lval)  // 递归调用继续处理
    }
    
    if _, err := strconv.ParseFloat(numStr, 64); err == nil {
        lval.str = numStr
        return NUMBER
    }
    
    // 如果数字格式无效，回退并报错
    l.pos = start + 1
    l.Error(fmt.Sprintf("invalid number format '%s' at position %d", numStr, start))
    return l.Lex(lval)  // 递归调用继续处理
}

func (l *SimpleLexer) lexIdent(lval *yySymType) int {
    start := l.pos
    
    for l.pos < len(l.input) {
        ch := l.input[l.pos]
        if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || 
           (ch >= '0' && ch <= '9') || ch == '_' {
            l.pos++
        } else {
            break
        }
    }
    
    ident := l.input[start:l.pos]
    lval.str = ident
    
    // 检查关键字
    switch ident {
    case "int":
        return INT
    case "float":
        return FLOAT
    case "bool":
        return BOOL
    case "true":
        lval.bool = true
        return TRUE
    case "false":
        lval.bool = false
        return FALSE
    default:
        return IDENT
    }
}

// 解析函数
func parse(input string) (*Node, error) {
    lexer := NewLexer(input)
    if yyParse(lexer) != 0 {
        return nil, fmt.Errorf("解析失败")
    }
    return parseResult, nil
}

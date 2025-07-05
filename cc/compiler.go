package cc

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/legamerdc/game/lib"
)

var (
	fmtWrongVarType = "wrong variable type: %s"
	fmtVariableType = "variable undefined: %s"
	fmtKeyMiss      = "key not set: %s"
	fmtConstFormat  = "number ill format: %s"
	fmtIllFunc      = "ill func: %s"
)

type (
	exprType int32

	Ctx interface {
		Get(string) (lib.Field, bool)
		Set(string, lib.Field)
		Exec(string) (lib.Field, bool)
	}
)

const (
	exprUnknown exprType = iota
	exprInt
	exprFloat
	exprBool
)

func MustCompile[B Ctx](code string) func(kv B) (lib.Field, error) {
	f, e := Compile[B](code)
	if e != nil {
		panic(e)
	}
	return f
}

func Compile[B Ctx](code string) (f func(kv B) (lib.Field, error), e error) {
	var (
		n *Node
		m map[string]exprType
	)
	if n, e = parse(code); e != nil {
		return nil, e
	}
	//if len(n.Children) != 1 {
	//	return nil, errors.New("invalid expression")
	//}
	//n = n.Children[0]
	//fmt.Println(dfs(n, 0))
	if m, e = n.phaseVar(); e != nil {
		return nil, e
	}
	if _, e = n.phaseInfectUp(m); e != nil {
		return nil, e
	}
	if e = n.phaseInfectDown(m, 0); e != nil {
		return nil, e
	}
	return compile[B](n, m)
}

func compile[B Ctx](n *Node, m map[string]exprType) (f func(B) (lib.Field, error), e error) {
	switch n.Type {
	case NodeProgram:
		fs := make([]func(B) (lib.Field, error), 0, len(n.Children)+1)
		for _, x := range n.Children {
			if x.Type != NodeVarDecl {
				if f, e = compile[B](x, m); e != nil {
					return nil, e
				}
				fs = append(fs, f)
			}
		}
		return _inline(fs), nil
	case NodeAssign:
		return compileAssign[B](n, m)
	case NodeUnaryOp:
		return compileUnary[B](n, m)
	case NodeBinOp:
		return compileBinary[B](n, m)
	case NodeTernary:
		return compileTernary[B](n, m)
	case NodeIdent:
		return compileIdent[B](n, m)
	case NodeTryIdent:
		return compileTryIdent[B](n, m)
	case NodeFunc:
		return compileFunc[B](n, m)
	case NodeNumber:
		return compileNumber[B](n, m)
	case NodeBool:
		return compileBool[B](n, m)
	default:
	}
	panic("unreachable")
}

func compileAssign[B Ctx](n *Node, m map[string]exprType) (fr func(B) (lib.Field, error), e error) {
	var f func(B) (lib.Field, error)
	if f, e = compile[B](n.Children[0], m); e != nil {
		return nil, e
	}
	token := n.Token
	switch n.Target {
	case exprInt:
		return func(b B) (v lib.Field, e error) {
			if v, e = f(b); e != nil {
				return
			}
			vv, _ := v.Int64()
			b.Set(token, lib.Int64(vv))
			return v, nil
		}, nil
	case exprFloat:
		return func(b B) (v lib.Field, e error) {
			if v, e = f(b); e != nil {
				return
			}
			vv, _ := v.Float64()
			b.Set(token, lib.Float64(vv))
			return v, nil
		}, nil
	case exprBool:
		return func(b B) (v lib.Field, e error) {
			if v, e = f(b); e != nil {
				return
			}
			vv, _ := v.Bool()
			b.Set(token, lib.Bool(vv))
			return v, nil
		}, nil
	default:
		panic("unreachable")
	}
}

func compileUnary[B Ctx](n *Node, m map[string]exprType) (fr func(B) (lib.Field, error), e error) {
	var f func(B) (lib.Field, error)
	if f, e = compile[B](n.Children[0], m); e != nil {
		return nil, e
	}
	switch n.Token {
	case "+":
		return f, nil
	case "-":
		if n.Target == exprFloat {
			return func(b B) (v lib.Field, e error) {
				if v, e = f(b); e != nil {
					return
				}
				vv, _ := v.Float64()
				return lib.Float64(-vv), nil
			}, nil
		}
		return func(b B) (v lib.Field, e error) {
			if v, e = f(b); e != nil {
				return
			}
			vv, _ := v.Int64()
			return lib.Int64(-vv), nil
		}, nil
	case "!":
		return func(b B) (v lib.Field, e error) {
			if v, e = f(b); e != nil {
				return
			}
			vv, _ := v.Bool()
			return lib.Bool(!vv), nil
		}, nil
	default:
		panic("unreachable")
	}
}

func compileBinary[B Ctx](n *Node, m map[string]exprType) (func(B) (lib.Field, error), error) {
	f0, e0 := compile[B](n.Children[0], m)
	f1, e1 := compile[B](n.Children[1], m)
	if e := errors.Join(e0, e1); e != nil {
		return nil, e
	}
	switch n.Token {
	case "==", "!=":
		if n.Children[0].Target == exprBool {
			op := binBool(n.Token)
			return func(b B) (v lib.Field, e error) {
				v0, e0 := f0(b)
				v1, e1 := f1(b)
				if e = errors.Join(e0, e1); e != nil {
					return
				}
				vv0, _ := v0.Bool()
				vv1, _ := v1.Bool()
				return op(vv0, vv1), nil
			}, nil
		}
		fallthrough
	case "^", "+", "-", "*", "/", "%", "<", "<=", ">", ">=":
		if n.Children[0].Target == exprInt {
			op := binInt(n.Token)
			return func(b B) (v lib.Field, e error) {
				v0, e0 := f0(b)
				v1, e1 := f1(b)
				if e = errors.Join(e0, e1); e != nil {
					return
				}
				vv0, _ := v0.Int64()
				vv1, _ := v1.Int64()
				return op(vv0, vv1), nil
			}, nil
		}
		if n.Children[0].Target == exprFloat {
			op := binFloat(n.Token)
			return func(b B) (v lib.Field, e error) {
				v0, e0 := f0(b)
				v1, e1 := f1(b)
				if e = errors.Join(e0, e1); e != nil {
					return
				}
				vv0, _ := v0.Float64()
				vv1, _ := v1.Float64()
				return op(vv0, vv1), nil
			}, nil
		}
	case "||":
		return func(b B) (v lib.Field, e error) {
			v0, e0 := f0(b)
			if e0 != nil {
				return v, e0
			}
			vv0, _ := v0.Bool()
			if vv0 {
				return v0, nil
			}
			v1, e1 := f1(b)
			if e1 != nil {
				return v, e1
			}
			return v1, nil
		}, nil
	case "&&":
		return func(b B) (v lib.Field, e error) {
			v0, e0 := f0(b)
			if e0 != nil {
				return v, e0
			}
			vv0, _ := v0.Bool()
			if !vv0 {
				return v0, nil
			}
			v1, e1 := f1(b)
			if e1 != nil {
				return v, e1
			}
			return v1, nil
		}, nil
	default:
	}
	panic("unreachable")
}

func compileTernary[B Ctx](n *Node, m map[string]exprType) (func(B) (lib.Field, error), error) {
	f0, e0 := compile[B](n.Children[0], m)
	f1, e1 := compile[B](n.Children[1], m)
	f2, e2 := compile[B](n.Children[2], m)
	if e := errors.Join(e0, e1, e2); e != nil {
		return nil, e
	}
	return func(b B) (v lib.Field, e error) {
		v0, e0 := f0(b)
		if e0 != nil {
			return v, e0
		}
		if vv0, _ := v0.Bool(); vv0 {
			return f1(b)
		}
		return f2(b)
	}, nil
}

func compileFunc[B Ctx](n *Node, _ map[string]exprType) (func(B) (lib.Field, error), error) {
	token := n.Token
	return func(b B) (v lib.Field, e error) {
		v0, ok := b.Exec(token)
		if !ok {
			return v, fmt.Errorf(fmtIllFunc, token)
		}
		return v0, nil
	}, nil
}

var (
	zeroInt   = lib.Int64(0)
	zeroFloat = lib.Float64(0)
	zeroBool  = lib.Bool(false)
)

func compileTryIdent[B Ctx](n *Node, m map[string]exprType) (func(B) (lib.Field, error), error) {
	token := n.Token
	var zero lib.Field
	switch m[token] {
	case exprInt:
		zero = zeroInt
	case exprFloat:
		zero = zeroFloat
	case exprBool:
		zero = zeroBool
	default:
		panic("unreachable")
	}
	return func(b B) (v lib.Field, e error) {
		v0, ok := b.Get(token)
		if !ok {
			return zero, nil
		}
		return v0, nil
	}, nil
}

func compileIdent[B Ctx](n *Node, _ map[string]exprType) (func(B) (lib.Field, error), error) {
	token := n.Token
	return func(b B) (v lib.Field, e error) {
		v0, ok := b.Get(token)
		if !ok {
			return v, fmt.Errorf(fmtKeyMiss, token)
		}
		return v0, nil
	}, nil
}

func compileNumber[B Ctx](n *Node, _ map[string]exprType) (func(B) (lib.Field, error), error) {
	f, e := strconv.ParseFloat(n.Token, 64)
	if e != nil {
		return nil, fmt.Errorf(fmtConstFormat, n.Token)
	}
	var v lib.Field
	switch n.Target {
	case exprInt:
		v = lib.Int64(int64(f))
	case exprFloat:
		v = lib.Float64(f)
	case exprBool:
		v = lib.Bool(int64(f) != 0)
	default:
		panic("unreachable")
	}
	return func(b B) (lib.Field, error) {
		return v, nil
	}, nil
}

func compileBool[B Ctx](n *Node, _ map[string]exprType) (func(B) (lib.Field, error), error) {
	f, e := strconv.ParseBool(n.Token)
	if e != nil {
		return nil, fmt.Errorf(fmtConstFormat, n.Token)
	}
	v := lib.Bool(f)
	return func(b B) (lib.Field, error) {
		return v, nil
	}, nil
}

func binInt(op string) func(a, b int64) lib.Field {
	switch op {
	case "^":
		return func(a, b int64) lib.Field {
			return lib.Int64(_ipower(a, b))
		}
	case "+":
		return func(a, b int64) lib.Field {
			return lib.Int64(a + b)
		}
	case "-":
		return func(a, b int64) lib.Field {
			return lib.Int64(a - b)
		}
	case "*":
		return func(a, b int64) lib.Field {
			return lib.Int64(a * b)
		}
	case "/":
		return func(a, b int64) lib.Field {
			return lib.Int64(a / b)
		}
	case "%":
		return func(a, b int64) lib.Field {
			return lib.Int64(a % b)
		}
	case "==":
		return func(a, b int64) lib.Field {
			return lib.Bool(a == b)
		}
	case "!=":
		return func(a, b int64) lib.Field {
			return lib.Bool(a != b)
		}
	case "<":
		return func(a, b int64) lib.Field {
			return lib.Bool(a < b)
		}
	case "<=":
		return func(a, b int64) lib.Field {
			return lib.Bool(a <= b)
		}
	case ">":
		return func(a, b int64) lib.Field {
			return lib.Bool(a > b)
		}
	case ">=":
		return func(a, b int64) lib.Field {
			return lib.Bool(a >= b)
		}
	default:
		panic("unreachable")
	}
}

func binFloat(op string) func(a float64, b float64) lib.Field {
	switch op {
	case "^":
		return func(a, b float64) lib.Field {
			return lib.Float64(math.Pow(a, b))
		}
	case "+":
		return func(a, b float64) lib.Field {
			return lib.Float64(a + b)
		}
	case "-":
		return func(a, b float64) lib.Field {
			return lib.Float64(a - b)
		}
	case "*":
		return func(a, b float64) lib.Field {
			return lib.Float64(a * b)
		}
	case "/":
		return func(a, b float64) lib.Field {
			return lib.Float64(a / b)
		}
	case "==":
		return func(a, b float64) lib.Field {
			return lib.Bool(a == b)
		}
	case "!=":
		return func(a, b float64) lib.Field {
			return lib.Bool(a != b)
		}
	case "<":
		return func(a, b float64) lib.Field {
			return lib.Bool(a < b)
		}
	case "<=":
		return func(a, b float64) lib.Field {
			return lib.Bool(a <= b)
		}
	case ">":
		return func(a, b float64) lib.Field {
			return lib.Bool(a > b)
		}
	case ">=":
		return func(a, b float64) lib.Field {
			return lib.Bool(a >= b)
		}
	default:
		panic("unreachable")
	}
}

func binBool(op string) func(a, b bool) lib.Field {
	switch op {
	case "==":
		return func(a, b bool) lib.Field {
			return lib.Bool(a == b)
		}
	case "!=":
		return func(a, b bool) lib.Field {
			return lib.Bool(a != b)
		}
	default:
		panic("unreachable")
	}
}

func _ipower(a, b int64) int64 {
	var c int64 = 1
	for b > 0 {
		if b&1 != 0 {
			c *= a
		}
		a *= a
		b >>= 1
	}
	return c
}

func _inline[I, O any](fs []func(I) (O, error)) func(I) (O, error) {
	// 对常见的参数数量[0-5]做内联，加快函数调用
	switch l := len(fs); l {
	case 0:
		return func(I) (o O, e error) {
			// 这里蕴含O类型0值是未初始化值
			return
		}
	case 1:
		f0 := fs[0]
		return func(i I) (o O, e error) {
			return f0(i)
		}
	case 2:
		f0, f1 := fs[0], fs[1]
		return func(i I) (o O, e error) {
			_, e0 := f0(i)
			o1, e1 := f1(i)
			if e = errors.Join(e0, e1); e != nil {
				return
			}
			return o1, nil
		}
	case 3:
		f0, f1, f2 := fs[0], fs[1], fs[2]
		return func(i I) (o O, e error) {
			_, e0 := f0(i)
			_, e1 := f1(i)
			o2, e2 := f2(i)
			if e = errors.Join(e0, e1, e2); e != nil {
				return
			}
			return o2, nil
		}
	case 4:
		f0, f1, f2, f3 := fs[0], fs[1], fs[2], fs[3]
		return func(i I) (o O, e error) {
			_, e0 := f0(i)
			_, e1 := f1(i)
			_, e2 := f2(i)
			o3, e3 := f3(i)
			if e = errors.Join(e0, e1, e2, e3); e != nil {
				return
			}
			return o3, nil
		}
	case 5:
		f0, f1, f2, f3, f4 := fs[0], fs[1], fs[2], fs[3], fs[4]
		return func(i I) (o O, e error) {
			_, e0 := f0(i)
			_, e1 := f1(i)
			_, e2 := f2(i)
			_, e3 := f3(i)
			o4, e4 := f4(i)
			if e = errors.Join(e0, e1, e2, e3, e4); e != nil {
				return
			}
			return o4, nil
		}
	default:
		return func(i I) (o O, e error) {
			for _, f := range fs {
				if o, e = f(i); e != nil {
					return
				}
			}
			return
		}
	}
}

func _string2type(s string) (exprType, error) {
	switch s {
	case "int":
		return exprInt, nil
	case "float":
		return exprFloat, nil
	case "bool":
		return exprBool, nil
	}
	return -1, fmt.Errorf(fmtWrongVarType, s)
}

func _tmpKeys(m map[string]exprType) (keys []string) {
	for k := range m {
		if strings.HasPrefix(k, "_") {
			keys = append(keys, k)
		}
	}
	return
}

func _after[B any](f, after func(B) (lib.Field, error)) func(B) (lib.Field, error) {
	return func(b B) (v lib.Field, e error) {
		if v, e = f(b); e != nil {
			return
		}
		_, _ = after(b)
		return v, nil
	}
}

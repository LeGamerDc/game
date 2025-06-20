package calc

import (
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/legamerdc/game/blackboard"
)

var (
	fmtWrongVarType = "wrong variable type: %s"
	fmtVariableType = "variable type unknown: %s"
	fmtKeyMiss      = "key miss: %s"
	fmtConstFormat  = "const format: %s"
)

type (
	exprType int32

	Kv interface {
		Get(string) (blackboard.Field, bool)
		SetInt64(string, int64)
		SetFloat64(string, float64)
		SetBool(string, bool)
	}
)

const (
	exprUnknown exprType = iota
	exprInt
	exprFloat
	exprBool
)

func Compile[B Kv](code string) (f func(kv B) (blackboard.Field, error), e error) {
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

func compile[B Kv](n *Node, m map[string]exprType) (f func(B) (blackboard.Field, error), e error) {
	switch n.Type {
	case NodeProgram:
		var fs []func(B) (blackboard.Field, error)
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
	case NodeNumber:
		return compileNumber[B](n, m)
	case NodeBool:
		return compileBool[B](n, m)
	default:
	}
	panic("unreachable")
}

func compileAssign[B Kv](n *Node, m map[string]exprType) (fr func(B) (blackboard.Field, error), e error) {
	var f func(B) (blackboard.Field, error)
	if f, e = compile[B](n.Children[0], m); e != nil {
		return nil, e
	}
	token := n.Token
	switch n.Target {
	case exprInt:
		return func(b B) (v blackboard.Field, e error) {
			if v, e = f(b); e != nil {
				return
			}
			vv, _ := v.Int64()
			b.SetInt64(token, vv)
			return v, nil
		}, nil
	case exprFloat:
		return func(b B) (v blackboard.Field, e error) {
			if v, e = f(b); e != nil {
				return
			}
			vv, _ := v.Float64()
			b.SetFloat64(token, vv)
			return v, nil
		}, nil
	case exprBool:
		return func(b B) (v blackboard.Field, e error) {
			if v, e = f(b); e != nil {
				return
			}
			vv, _ := v.Bool()
			b.SetBool(token, vv)
			return v, nil
		}, nil
	default:
		panic("unreachable")
	}
}

func compileUnary[B Kv](n *Node, m map[string]exprType) (fr func(B) (blackboard.Field, error), e error) {
	var f func(B) (blackboard.Field, error)
	if f, e = compile[B](n.Children[0], m); e != nil {
		return nil, e
	}
	switch n.Token {
	case "+":
		return f, nil
	case "-":
		if n.Target == exprFloat {
			return func(b B) (v blackboard.Field, e error) {
				if v, e = f(b); e != nil {
					return
				}
				vv, _ := v.Float64()
				return blackboard.Float64(-vv), nil
			}, nil
		}
		return func(b B) (v blackboard.Field, e error) {
			if v, e = f(b); e != nil {
				return
			}
			vv, _ := v.Int64()
			return blackboard.Int64(-vv), nil
		}, nil
	case "!":
		return func(b B) (v blackboard.Field, e error) {
			if v, e = f(b); e != nil {
				return
			}
			vv, _ := v.Bool()
			return blackboard.Bool(!vv), nil
		}, nil
	default:
		panic("unreachable")
	}
}

func compileBinary[B Kv](n *Node, m map[string]exprType) (func(B) (blackboard.Field, error), error) {
	f0, e0 := compile[B](n.Children[0], m)
	f1, e1 := compile[B](n.Children[1], m)
	if e := errors.Join(e0, e1); e != nil {
		return nil, e
	}
	switch n.Token {
	case "^", "+", "-", "*", "/", "%", "==", "!=", "<", "<=", ">", ">=":
		if n.Children[0].Target == exprInt {
			op := binInt(n.Token)
			return func(b B) (v blackboard.Field, e error) {
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
			return func(b B) (v blackboard.Field, e error) {
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
		return func(b B) (v blackboard.Field, e error) {
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
		return func(b B) (v blackboard.Field, e error) {
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

func compileTernary[B Kv](n *Node, m map[string]exprType) (func(B) (blackboard.Field, error), error) {
	f0, e0 := compile[B](n.Children[0], m)
	f1, e1 := compile[B](n.Children[1], m)
	f2, e2 := compile[B](n.Children[2], m)
	if e := errors.Join(e0, e1, e2); e != nil {
		return nil, e
	}
	return func(b B) (v blackboard.Field, e error) {
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

func compileIdent[B Kv](n *Node, m map[string]exprType) (func(B) (blackboard.Field, error), error) {
	token := n.Token
	return func(b B) (v blackboard.Field, e error) {
		v0, ok := b.Get(token)
		if !ok {
			return v, fmt.Errorf(fmtKeyMiss, token)
		}
		return v0, nil
	}, nil
}

func compileNumber[B Kv](n *Node, m map[string]exprType) (func(B) (blackboard.Field, error), error) {
	f, e := strconv.ParseFloat(n.Token, 64)
	if e != nil {
		return nil, fmt.Errorf(fmtConstFormat, n.Token)
	}
	var v blackboard.Field
	switch n.Target {
	case exprInt:
		v = blackboard.Int64(int64(f))
	case exprFloat:
		v = blackboard.Float64(f)
	case exprBool:
		v = blackboard.Bool(int64(f) != 0)
	default:
		panic("unreachable")
	}
	return func(b B) (blackboard.Field, error) {
		return v, nil
	}, nil
}

func compileBool[B Kv](n *Node, m map[string]exprType) (func(B) (blackboard.Field, error), error) {
	f, e := strconv.ParseBool(n.Token)
	if e != nil {
		return nil, fmt.Errorf(fmtConstFormat, n.Token)
	}
	v := blackboard.Bool(f)
	return func(b B) (blackboard.Field, error) {
		return v, nil
	}, nil
}

func binInt(op string) func(a, b int64) blackboard.Field {
	switch op {
	case "^":
		return func(a, b int64) blackboard.Field {
			return blackboard.Int64(_ipower(a, b))
		}
	case "+":
		return func(a, b int64) blackboard.Field {
			return blackboard.Int64(a + b)
		}
	case "-":
		return func(a, b int64) blackboard.Field {
			return blackboard.Int64(a - b)
		}
	case "*":
		return func(a, b int64) blackboard.Field {
			return blackboard.Int64(a * b)
		}
	case "/":
		return func(a, b int64) blackboard.Field {
			return blackboard.Int64(a / b)
		}
	case "%":
		return func(a, b int64) blackboard.Field {
			return blackboard.Int64(a % b)
		}
	case "==":
		return func(a, b int64) blackboard.Field {
			return blackboard.Bool(a == b)
		}
	case "!=":
		return func(a, b int64) blackboard.Field {
			return blackboard.Bool(a != b)
		}
	case "<":
		return func(a, b int64) blackboard.Field {
			return blackboard.Bool(a < b)
		}
	case "<=":
		return func(a, b int64) blackboard.Field {
			return blackboard.Bool(a <= b)
		}
	case ">":
		return func(a, b int64) blackboard.Field {
			return blackboard.Bool(a > b)
		}
	case ">=":
		return func(a, b int64) blackboard.Field {
			return blackboard.Bool(a >= b)
		}
	default:
		panic("unreachable")
	}
}

func binFloat(op string) func(a float64, b float64) blackboard.Field {
	switch op {
	case "^":
		return func(a, b float64) blackboard.Field {
			return blackboard.Float64(math.Pow(a, b))
		}
	case "+":
		return func(a, b float64) blackboard.Field {
			return blackboard.Float64(a + b)
		}
	case "-":
		return func(a, b float64) blackboard.Field {
			return blackboard.Float64(a - b)
		}
	case "*":
		return func(a, b float64) blackboard.Field {
			return blackboard.Float64(a * b)
		}
	case "/":
		return func(a, b float64) blackboard.Field {
			return blackboard.Float64(a / b)
		}
	case "==":
		return func(a, b float64) blackboard.Field {
			return blackboard.Bool(a == b)
		}
	case "!=":
		return func(a, b float64) blackboard.Field {
			return blackboard.Bool(a != b)
		}
	case "<":
		return func(a, b float64) blackboard.Field {
			return blackboard.Bool(a < b)
		}
	case "<=":
		return func(a, b float64) blackboard.Field {
			return blackboard.Bool(a <= b)
		}
	case ">":
		return func(a, b float64) blackboard.Field {
			return blackboard.Bool(a > b)
		}
	case ">=":
		return func(a, b float64) blackboard.Field {
			return blackboard.Bool(a >= b)
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
	// 对常见的参数数量[0-4]做内联，加快函数调用
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

package cc

import (
	"errors"
	"fmt"
	"strings"
)

func (n *Node) phaseVar() (m map[string]exprType, e error) {
	if n.Type != NodeProgram {
		return nil, nil
	}
	m = make(map[string]exprType)
	for _, x := range n.Children {
		if x.Type == NodeVarDecl {
			if e = x.varType(m); e != nil {
				return nil, e
			}
		}
	}
	return m, nil
}

func (n *Node) varType(m map[string]exprType) error {
	var (
		idx int
		et  exprType
		e   error
	)
	if idx = strings.IndexByte(n.Token, ':'); idx == -1 {
		return fmt.Errorf(fmtWrongVarType, n.Token)
	}
	if et, e = _string2type(n.Token[:idx]); e != nil {
		return fmt.Errorf(fmtWrongVarType, n.Token)
	}
	vs := strings.Split(n.Token[idx+1:], ",")
	for _, v := range vs {
		m[v] = et
	}
	return nil
}

func (n *Node) phaseInfectDown(m map[string]exprType, down exprType) (e error) {
	var ok bool
	switch n.Type {
	case NodeProgram:
		for _, x := range n.Children {
			if e = x.phaseInfectDown(m, 0); e != nil {
				return e
			}
		}
		return nil
	case NodeVarDecl:
		return
	case NodeAssign:
		return n.Children[0].phaseInfectDown(m, n.Target)
	case NodeUnaryOp:
		if n.Target, ok = _infect(n.Target, down); !ok {
			return fmt.Errorf(fmtWrongVarType, n.Token)
		}
		return n.Children[0].phaseInfectDown(m, n.Target)
	case NodeBinOp:
		switch n.Token {
		case "^", "+", "-", "*", "/":
			if n.Target, ok = _infect(n.Target, down); !ok {
				return fmt.Errorf(fmtWrongVarType, n.Token)
			}
			return errors.Join(n.Children[0].phaseInfectDown(m, n.Target), n.Children[1].phaseInfectDown(m, n.Target))
		case "==", "!=", "<", "<=", ">", ">=":
			if n.Target, ok = _infect(n.Target, down); !ok {
				return fmt.Errorf(fmtWrongVarType, n.Token)
			}
			l, r := n.Children[0].Target, n.Children[1].Target
			if (l == exprBool && r == exprFloat) || (l == exprFloat && r == exprBool) {
				return fmt.Errorf(fmtWrongVarType, n.Token)
			}
			target := exprInt
			if l == exprFloat || r == exprFloat {
				target = exprFloat
			}
			if l == exprBool || r == exprBool {
				target = exprBool
			}
			return errors.Join(n.Children[0].phaseInfectDown(m, target), n.Children[1].phaseInfectDown(m, target))
		case "||", "&&":
			return errors.Join(n.Children[0].phaseInfectDown(m, exprBool), n.Children[1].phaseInfectDown(m, exprBool))
		case "%":
			return errors.Join(n.Children[0].phaseInfectDown(m, exprInt), n.Children[1].phaseInfectDown(m, exprInt))
		}
	case NodeTernary:
		if n.Target, ok = _infect(n.Target, down); !ok {
			return fmt.Errorf(fmtWrongVarType, n.Token)
		}
		return errors.Join(n.Children[1].phaseInfectDown(m, n.Target), n.Children[2].phaseInfectDown(m, n.Target))
	case NodeFunc:
		for _, x := range n.Children {
			if e = x.phaseInfectDown(m, 0); e != nil {
				return e
			}
		}
		fallthrough
	case NodeIdent, NodeTryIdent, NodeNumber, NodeBool:
		if n.Target, ok = _infect(n.Target, down); !ok {
			return fmt.Errorf(fmtWrongVarType, n.Token)
		}
		return nil
	default:
	}
	panic(fmt.Sprintf("unknown node type: %d", n.Type))
}

func (n *Node) phaseInfectUp(m map[string]exprType) (up exprType, e error) {
	switch n.Type {
	case NodeProgram:
		for _, x := range n.Children {
			if _, e = x.phaseInfectUp(m); e != nil {
				return 0, e
			}
		}
		return 0, nil
	case NodeVarDecl:
		return
	case NodeAssign:
		et, ok := m[n.Token]
		if !ok {
			return 0, fmt.Errorf(fmtWrongVarType, n.Token)
		}
		_, e = n.Children[0].phaseInfectUp(m)
		n.Target = et
		return et, e
	case NodeUnaryOp:
		switch n.Token {
		case "+", "-":
			if up, e = n.Children[0].phaseInfectUp(m); e != nil {
				return 0, e
			}
			if up == exprBool {
				return 0, fmt.Errorf(fmtWrongVarType, n.Token)
			}
			n.Target = up
			return up, nil
		case "!":
			if up, e = n.Children[0].phaseInfectUp(m); e != nil {
				return 0, e
			}
			if up == exprFloat {
				return 0, fmt.Errorf(fmtWrongVarType, n.Token)
			}
			n.Target = exprBool
			return exprBool, nil
		}
	case NodeBinOp:
		switch n.Token {
		case "^", "*", "/", "+", "-":
			up0, e0 := n.Children[0].phaseInfectUp(m)
			up1, e1 := n.Children[1].phaseInfectUp(m)
			if e = errors.Join(e0, e1); e != nil {
				return 0, e
			}
			if up0 == exprBool || up1 == exprBool {
				return 0, fmt.Errorf(fmtWrongVarType, n.Token)
			}
			up = exprInt
			if up0 == exprFloat || up1 == exprFloat {
				up = exprFloat
			}
			n.Target = up
			return up, nil
		case "==", "!=":
			l, e0 := n.Children[0].phaseInfectUp(m)
			r, e1 := n.Children[1].phaseInfectUp(m)
			if e = errors.Join(e0, e1); e != nil {
				return 0, e
			}
			if (l == exprBool && r == exprFloat) || (l == exprFloat && r == exprBool) {
				return 0, fmt.Errorf(fmtWrongVarType, n.Token)
			}
			n.Target = exprBool
			return exprBool, nil
		case "<", "<=", ">", ">=":
			up0, e0 := n.Children[0].phaseInfectUp(m)
			up1, e1 := n.Children[1].phaseInfectUp(m)
			if e = errors.Join(e0, e1); e != nil {
				return 0, e
			}
			if up0 == exprBool || up1 == exprBool {
				return 0, fmt.Errorf(fmtWrongVarType, n.Token)
			}
			n.Target = exprBool
			return exprBool, nil
		case "||", "&&":
			up0, e0 := n.Children[0].phaseInfectUp(m)
			up1, e1 := n.Children[1].phaseInfectUp(m)
			if e = errors.Join(e0, e1); e != nil {
				return 0, e
			}
			if up0 == exprFloat || up1 == exprFloat {
				return 0, fmt.Errorf(fmtWrongVarType, n.Token)
			}
			n.Target = exprBool
			return exprBool, nil
		case "%":
			up0, e0 := n.Children[0].phaseInfectUp(m)
			up1, e1 := n.Children[1].phaseInfectUp(m)
			if e = errors.Join(e0, e1); e != nil {
				return 0, e
			}
			if !(up0 == exprInt && up1 == exprInt) {
				return 0, fmt.Errorf(fmtWrongVarType, n.Token)
			}
			n.Target = exprInt
			return exprInt, nil
		}
	case NodeTernary:
		up0, e0 := n.Children[0].phaseInfectUp(m)
		up1, e1 := n.Children[1].phaseInfectUp(m)
		up2, e2 := n.Children[2].phaseInfectUp(m)
		if e = errors.Join(e0, e1, e2); e != nil {
			return 0, e
		}
		if up0 != exprBool {
			return 0, fmt.Errorf(fmtWrongVarType, n.Token)
		}
		if up1 == exprInt {
			n.Target = up2
			return up2, nil
		}
		if up2 == exprInt {
			n.Target = up1
			return up1, nil
		}
		if up1 == up2 {
			n.Target = up1
			return up1, nil
		}
		return 0, fmt.Errorf(fmtWrongVarType, n.Token)
	case NodeFunc:
		for _, x := range n.Children {
			if _, e = x.phaseInfectUp(m); e != nil {
				return 0, e
			}
		}
		fallthrough
	case NodeIdent, NodeTryIdent:
		et, ok := m[n.Token]
		if !ok {
			return 0, fmt.Errorf(fmtVariableType, n.Token)
		}
		n.Target = et
		return et, nil
	case NodeNumber:
		n.Target = exprInt
		if strings.Contains(n.Token, ".") {
			n.Target = exprFloat
		}
		return n.Target, nil
	case NodeBool:
		n.Target = exprBool
		return exprBool, nil
	default:
	}
	panic(fmt.Sprintf("unknown node type: %d", n.Type))
}

func _infect(now, down exprType) (exprType, bool) {
	switch down {
	case exprUnknown:
		return now, true
	case exprFloat:
		if now == exprBool {
			return 0, false
		}
		return exprFloat, true
	case exprBool:
		if now == exprFloat {
			return 0, false
		}
		return exprBool, true
	case exprInt:
		if now != exprInt {
			return 0, false
		}
		return exprInt, true
	}
	panic("unreachable")
}

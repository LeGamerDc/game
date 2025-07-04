package cc

import (
	"fmt"
	"strconv"
)

func dfs(n *Node, i int) (s string) {
	idx := strconv.Itoa(i)
	switch n.Type {
	case NodeProgram:
		for p, x := range n.Children {
			if p == 0 {
				s = dfs(x, i+1)
			} else {
				s += ";" + dfs(x, i+1)
			}
		}
		return idx + "[" + s + "]"
	case NodeVarDecl:
		return idx + n.Token
	case NodeAssign:
		return idx + n.Token + "=" + dfs(n.Children[0], i+1)
	case NodeUnaryOp:
		return idx + n.Token + dfs(n.Children[0], i+1)
	case NodeBinOp:
		return idx + dfs(n.Children[0], i+1) + n.Token + dfs(n.Children[1], i+1)
	case NodeTernary:
		return idx + dfs(n.Children[0], i+1) + "?" + dfs(n.Children[1], i+1) + ":" + dfs(n.Children[2], i+1)
	case NodeIdent, NodeNumber, NodeBool:
		return idx + n.Token
	default:
	}
	fmt.Println("unknown node type", n.Type)
	panic("unreachable")
}

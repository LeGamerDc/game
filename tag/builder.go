package tag

import (
	"strings"
)

type DB struct {
	s2i    map[string]int16
	i2s    []string
	pi     []int16
	curIdx int16
}

func NewDB() *DB {
	return &DB{
		s2i: make(map[string]int16),
		i2s: make([]string, 0, 1024),
		pi:  make([]int16, 0, 1024),
	}
}

func (b *DB) Parent(i int16) int16 {
	if i < 0 || int(i) >= len(b.pi) {
		return -1
	}
	return b.pi[i]
}

func (b *DB) IsAncestor(p, i int16) bool {
	for i != -1 {
		if i == p {
			return true
		}
		i = b.pi[i]
	}
	return false
}

func (b *DB) String(i int16) string {
	if i < 0 || int(i) >= len(b.pi) {
		return "_"
	}
	return b.i2s[i]
}

// Compile 将"."分割的字符串按前缀注册 Builder 中，并建立字符串和 int16 的双向映射。
// 并记录字符串的 parent(最长前缀的 id)。
func (b *DB) Compile(s string) int16 {
	var (
		i, pi int16 = 0, -1
		start int
		ss    string
		ok    bool
	)
	for {
		if idx := strings.IndexByte(s[start:], '.'); idx >= 0 {
			ss, start = s[:start+idx], start+idx+1
			if i, ok = b.s2i[ss]; !ok {
				i = b.curIdx
				b.curIdx++
				b.s2i[ss] = i
				b.i2s = append(b.i2s, ss)
				b.pi = append(b.pi, pi)
			}
			pi = i
			continue
		}
		if i, ok = b.s2i[s]; !ok {
			i = b.curIdx
			b.curIdx++
			b.s2i[s] = i
			b.i2s = append(b.i2s, s)
			b.pi = append(b.pi, pi)
		}
		return i
	}
}

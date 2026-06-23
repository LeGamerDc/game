package tag

import (
	"fmt"
	"iter"
	"slices"
	"strings"
)

// DB is the immutable tag dictionary. It is built once from the full set of
// (possibly) used tags via Build and is read-only afterwards, which makes it
// safe to share across goroutines without synchronization.
//
// Tags are dot-separated hierarchical strings ("a.b.c"). Build folds case,
// validates the syntax, expands every ancestor prefix, sorts the result and
// assigns dense ids starting at 1 (id 0 == InvalidKey). The sorted assignment
// makes ids deterministic for a given input set: two processes that Build from
// the same tag set get identical ids.
type DB struct {
	s2i map[string]Key // normalized string -> id
	i2s []string       // id -> string (index 0 is the invalid sentinel)
	pi  []Key          // id -> parent id  (InvalidKey for roots)
}

// Build constructs an immutable DB from a stream of tag strings. The stream is
// an iter.Seq so callers never have to materialize a large slice of all tags.
//
// Each tag is lowercased and validated; ancestor prefixes are created
// automatically (registering "a.b.c" also registers "a.b" and "a"). A malformed
// tag (empty, leading/trailing dot, or an empty "a..b" segment) makes Build
// fail. After Build the dictionary is closed: there is no way to add new tags.
func Build(tags iter.Seq[string]) (*DB, error) {
	set := make(map[string]struct{})
	for raw := range tags {
		s, err := normalize(raw)
		if err != nil {
			return nil, err
		}
		// Register s and every ancestor prefix.
		for i := 0; i <= len(s); i++ {
			if i == len(s) || s[i] == '.' {
				set[s[:i]] = struct{}{}
			}
		}
	}

	sorted := make([]string, 0, len(set))
	for s := range set {
		sorted = append(sorted, s)
	}
	slices.Sort(sorted)

	// ids run 1..len(sorted); a Key is uint16, so the dictionary caps at 65535.
	if len(sorted) > 0xFFFF {
		return nil, fmt.Errorf("tag: too many tags (%d), max is 65535", len(sorted))
	}

	d := &DB{
		s2i: make(map[string]Key, len(sorted)),
		i2s: make([]string, len(sorted)+1),
		pi:  make([]Key, len(sorted)+1),
	}
	for idx, s := range sorted {
		id := Key(idx + 1)
		d.s2i[s] = id
		d.i2s[id] = s
	}
	// Resolve parents. A proper prefix always sorts before the full string, so
	// the parent's id is always strictly smaller — no cycles are possible.
	for idx, s := range sorted {
		id := Key(idx + 1)
		if dot := strings.LastIndexByte(s, '.'); dot >= 0 {
			d.pi[id] = d.s2i[s[:dot]]
		}
	}
	return d, nil
}

// normalize lowercases and validates a tag string.
func normalize(s string) (string, error) {
	s = strings.ToLower(s)
	if s == "" {
		return "", fmt.Errorf("tag: empty tag string")
	}
	if s[0] == '.' || s[len(s)-1] == '.' {
		return "", fmt.Errorf("tag: leading/trailing dot in %q", s)
	}
	if strings.Contains(s, "..") {
		return "", fmt.Errorf("tag: empty segment in %q", s)
	}
	return s, nil
}

// Lookup resolves a tag string (case-insensitively) to its id. The bool reports
// whether the tag exists in the dictionary.
func (d *DB) Lookup(s string) (Key, bool) {
	id, ok := d.s2i[strings.ToLower(s)]
	return id, ok
}

func (d *DB) valid(i Key) bool {
	return i != InvalidKey && int(i) < len(d.pi)
}

// Parent returns the parent id of i, or InvalidKey if i is a root or invalid.
func (d *DB) Parent(i Key) Key {
	if !d.valid(i) {
		return InvalidKey
	}
	return d.pi[i]
}

// IsAncestor reports whether p is an ancestor of i or equal to i.
func (d *DB) IsAncestor(p, i Key) bool {
	if !d.valid(p) {
		return false
	}
	for d.valid(i) {
		if i == p {
			return true
		}
		i = d.pi[i]
	}
	return false
}

// String returns the full dotted string of i, or "_" if i is invalid.
func (d *DB) String(i Key) string {
	if !d.valid(i) {
		return "_"
	}
	return d.i2s[i]
}

package tag

import (
	"errors"
	"slices"
)

// exprOp is the operator of an Expr node.
type exprOp uint8

const (
	// Constants. opFalse is the zero value on purpose: an uninitialized Expr
	// (hence a zero-value Query) matches nothing, which fails safe.
	opFalse exprOp = iota
	opTrue

	// Leaf operators (operate on a set of tags).
	opAllTags      // entity has all of the tags (hierarchical)
	opAnyTags      // entity has at least one of the tags (hierarchical)
	opNoTags       // entity has none of the tags (hierarchical)
	opAllTagsExact // ... matched exactly (ignoring the hierarchy)
	opAnyTagsExact
	opNoTagsExact

	// Compound operators (operate on a set of sub-expressions).
	opAnd // all sub-expressions match
	opOr  // any sub-expression matches
	opNor // no sub-expression matches
)

// Expr is a node of a tag query expression tree. Leaf nodes carry a tag set;
// compound nodes carry child expressions. Build one with the constructor
// helpers (AllTags, NoTags, And, Or, ...) and compile it with NewQueryExpr.
type Expr struct {
	op    exprOp
	tags  []Key
	exprs []Expr
}

// Leaf constructors (hierarchical).
func AllTags(tags ...Key) Expr { return Expr{op: opAllTags, tags: tags} }
func AnyTags(tags ...Key) Expr { return Expr{op: opAnyTags, tags: tags} }
func NoTags(tags ...Key) Expr  { return Expr{op: opNoTags, tags: tags} }

// Leaf constructors (exact).
func AllTagsExact(tags ...Key) Expr { return Expr{op: opAllTagsExact, tags: tags} }
func AnyTagsExact(tags ...Key) Expr { return Expr{op: opAnyTagsExact, tags: tags} }
func NoTagsExact(tags ...Key) Expr  { return Expr{op: opNoTagsExact, tags: tags} }

// Compound constructors.
func And(exprs ...Expr) Expr { return Expr{op: opAnd, exprs: exprs} }
func Or(exprs ...Expr) Expr  { return Expr{op: opOr, exprs: exprs} }
func Nor(exprs ...Expr) Expr { return Expr{op: opNor, exprs: exprs} }

// Query is a compiled, normalized tag query, ready to be matched repeatedly.
// Build one with NewQuery or NewQueryExpr; the zero value matches nothing.
type Query struct {
	root Expr
}

var errNilDB = errors.New("tag: nil DB")

// NewQueryExpr compiles an arbitrary expression tree into a Query.
//
// Normalization performs only cheap, always-valid rewrites:
//   - per-leaf dedup and hierarchy collapse (most-specific for All, broadest
//     for Any/None) — skipped for Exact leaves, where ancestors/descendants are
//     independent;
//   - constant folding of empty leaves and of And/Or/Nor over constant children.
//
// It deliberately does NOT try to prove a query unsatisfiable across siblings.
// An impossible query is not an error: it simply never matches (its root folds
// to — or evaluates as — false). The only error is a structural one (nil DB).
func NewQueryExpr(d *DB, root Expr) (Query, error) {
	if d == nil {
		return Query{}, errNilDB
	}
	return Query{root: normalizeExpr(d, root)}, nil
}

// NewQuery is a convenience wrapper for the common "required / forbidden /
// any-of" shape, equivalent to And(AllTags(all), NoTags(none), AnyTags(some))
// with empty clauses omitted. An empty all/none/some clause does not
// participate; an all-empty query matches everything.
func NewQuery(d *DB, all, none, some []Key) (Query, error) {
	var children []Expr
	if len(all) > 0 {
		children = append(children, AllTags(all...))
	}
	if len(none) > 0 {
		children = append(children, NoTags(none...))
	}
	if len(some) > 0 {
		children = append(children, AnyTags(some...))
	}
	return NewQueryExpr(d, And(children...))
}

// ── normalization ────────────────────────────────────────────────────────────

func normalizeExpr(d *DB, e Expr) Expr {
	switch e.op {
	case opAllTags, opAnyTags, opNoTags:
		tags := dedupKeys(slices.Clone(e.tags))
		tags = collapse(d, e.op, tags)
		return foldLeaf(e.op, tags)
	case opAllTagsExact, opAnyTagsExact, opNoTagsExact:
		// Exact leaves get dedup only: an ancestor and a descendant are
		// independent under exact matching, so neither implies the other.
		tags := dedupKeys(slices.Clone(e.tags))
		return foldLeaf(e.op, tags)
	case opAnd, opOr, opNor:
		kids := make([]Expr, 0, len(e.exprs))
		for i := range e.exprs {
			kids = append(kids, normalizeExpr(d, e.exprs[i]))
		}
		return foldCompound(e.op, kids)
	default: // opTrue, opFalse
		return e
	}
}

// collapse removes hierarchically-redundant tags from a leaf set.
func collapse(d *DB, op exprOp, tags []Key) []Key {
	switch op {
	case opAllTags:
		return keepMostSpecific(d, tags)
	case opAnyTags, opNoTags:
		return keepBroadest(d, tags)
	}
	return tags
}

// foldLeaf turns an empty leaf into the appropriate constant.
func foldLeaf(op exprOp, tags []Key) Expr {
	if len(tags) == 0 {
		switch op {
		case opAllTags, opAllTagsExact, opNoTags, opNoTagsExact:
			// "has all of nothing" and "has none of nothing" are vacuously true.
			return Expr{op: opTrue}
		case opAnyTags, opAnyTagsExact:
			// "has any of nothing" is false.
			return Expr{op: opFalse}
		}
	}
	return Expr{op: op, tags: tags}
}

// foldCompound applies boolean identities once the children are normalized.
func foldCompound(op exprOp, kids []Expr) Expr {
	switch op {
	case opAnd:
		var out []Expr
		for _, k := range kids {
			switch k.op {
			case opFalse:
				return Expr{op: opFalse}
			case opTrue:
				// drop
			default:
				out = append(out, k)
			}
		}
		switch len(out) {
		case 0:
			return Expr{op: opTrue}
		case 1:
			return out[0]
		default:
			return Expr{op: opAnd, exprs: out}
		}
	case opOr:
		var out []Expr
		for _, k := range kids {
			switch k.op {
			case opTrue:
				return Expr{op: opTrue}
			case opFalse:
				// drop
			default:
				out = append(out, k)
			}
		}
		switch len(out) {
		case 0:
			return Expr{op: opFalse}
		case 1:
			return out[0]
		default:
			return Expr{op: opOr, exprs: out}
		}
	case opNor:
		var out []Expr
		for _, k := range kids {
			switch k.op {
			case opTrue:
				// Nor is true only when every child is false.
				return Expr{op: opFalse}
			case opFalse:
				// drop
			default:
				out = append(out, k)
			}
		}
		if len(out) == 0 {
			return Expr{op: opTrue}
		}
		// A single-child Nor is "not child" and cannot be collapsed further.
		return Expr{op: opNor, exprs: out}
	}
	return Expr{op: op, exprs: kids}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// dedupKeys sorts then compacts duplicates (in place on the supplied slice).
func dedupKeys(tags []Key) []Key {
	slices.Sort(tags)
	return slices.Compact(tags)
}

// keepMostSpecific removes tag x when another tag y in the set is more specific
// (x is an ancestor of y): requiring y already implies x via ancestor closure.
func keepMostSpecific(d *DB, tags []Key) []Key {
	j := 0
	for i, x := range tags {
		redundant := false
		for k, y := range tags {
			if i != k && d.IsAncestor(x, y) {
				redundant = true
				break
			}
		}
		if !redundant {
			tags[j] = x
			j++
		}
	}
	return tags[:j]
}

// keepBroadest removes tag x when another tag y in the set is broader (y is an
// ancestor of x). Used for none (an ancestor ban subsumes a descendant ban) and
// some (an ancestor match is easier to satisfy).
func keepBroadest(d *DB, tags []Key) []Key {
	j := 0
	for i, x := range tags {
		redundant := false
		for k, y := range tags {
			if i != k && d.IsAncestor(y, x) {
				redundant = true
				break
			}
		}
		if !redundant {
			tags[j] = x
			j++
		}
	}
	return tags[:j]
}

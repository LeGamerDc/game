package tag

import (
	"slices"
)

type queryKind uint8

const (
	queryHasAll queryKind = 1 << iota
	queryHasNone
	queryHasSome
)

type Query struct {
	tags            []int16
	allEnd, noneEnd uint16
	kind            queryKind
}

// NewQuery constructs a Query with compile-time normalization:
//
//  1. Invalid tags: all contains invalid → impossible; none/some drop invalid.
//  2. Dedup each section.
//  3. Hierarchy normalization:
//     - all:  keep most specific (remove ancestors — they are implied).
//     - none: keep broadest (remove descendants — ancestor ban is stronger).
//     - some: keep broadest (remove descendants — ancestor is easier to satisfy).
//  4. Cross-section optimization:
//     - all ∩ none conflict: having an all-tag implies its ancestors; if any
//     none-tag is such an ancestor → impossible.
//     - some satisfied by all: if any some-tag is an ancestor of an all-tag,
//     the some clause is trivially true → drop entire some.
//     - none blocks some: remove some-tags whose presence would violate none;
//     if all some-tags are blocked → impossible.
func NewQuery(d *DB, all, none, some []int16) (Query, bool) {
	var q Query
	if d == nil {
		return q, false
	}

	// ── Phase 1: validate ──────────────────────────────────────────────
	// all: any invalid tag makes the query unsatisfiable.
	for _, t := range all {
		if !d.validTag(t) {
			return q, false
		}
	}

	// Work on copies so we never mutate the caller's slices.
	allW := dedupTags(slices.Clone(all))
	noneW := dedupTags(filterValidTags(d, slices.Clone(none)))
	someW := dedupTags(filterValidTags(d, slices.Clone(some)))

	// ── Phase 2: hierarchy normalization ────────────────────────────────
	allW = keepMostSpecific(d, allW)
	noneW = keepBroadest(d, noneW)
	someW = keepBroadest(d, someW)

	// ── Phase 3: cross-section checks ──────────────────────────────────

	// all ∩ none conflict: if a none-tag is an ancestor-or-equal of any
	// all-tag, having that all-tag would put the none-tag into the cache
	// via ancestor closure → contradiction.
	for _, a := range allW {
		for _, n := range noneW {
			if d.IsAncestor(n, a) {
				return q, false
			}
		}
	}

	// some satisfied by all: some requires "at least one present".  If any
	// some-tag is an ancestor-or-equal of an all-tag, having that all-tag
	// guarantees the some-tag via ancestor closure → entire some is trivially
	// satisfied.
	if len(someW) > 0 {
		for _, s := range someW {
			for _, a := range allW {
				if d.IsAncestor(s, a) {
					someW = someW[:0]
					goto buildQuery
				}
			}
		}
	}

	// none blocks some: a some-tag whose ancestor (or itself) appears in none
	// can never be present without violating none.  Remove such entries; if
	// nothing survives the some clause is unsatisfiable.
	if len(someW) > 0 && len(noneW) > 0 {
		j := 0
		for _, s := range someW {
			blocked := false
			for _, n := range noneW {
				if d.IsAncestor(n, s) {
					blocked = true
					break
				}
			}
			if !blocked {
				someW[j] = s
				j++
			}
		}
		someW = someW[:j]
		if j == 0 {
			return q, false
		}
	}

buildQuery:
	// ── Phase 4: pack into compact layout ──────────────────────────────
	q.tags = make([]int16, 0, len(allW)+len(noneW)+len(someW))
	q.tags = append(q.tags, allW...)
	q.allEnd = uint16(len(q.tags))
	q.tags = append(q.tags, noneW...)
	q.noneEnd = uint16(len(q.tags))
	q.tags = append(q.tags, someW...)

	if len(allW) > 0 {
		q.kind |= queryHasAll
	}
	if len(noneW) > 0 {
		q.kind |= queryHasNone
	}
	if len(someW) > 0 {
		q.kind |= queryHasSome
	}
	return q, true
}

// ── helpers ────────────────────────────────────────────────────────────────

// filterValidTags removes tags that are not registered in the DB (in-place).
func filterValidTags(d *DB, tags []int16) []int16 {
	j := 0
	for _, t := range tags {
		if d.validTag(t) {
			tags[j] = t
			j++
		}
	}
	return tags[:j]
}

// dedupTags sorts then compacts duplicates (in-place).
func dedupTags(tags []int16) []int16 {
	slices.Sort(tags)
	return slices.Compact(tags)
}

// keepMostSpecific removes tag x if another tag y in the set is more specific
// (i.e. x is an ancestor of y), because requiring y already implies x via
// ancestor closure.
func keepMostSpecific(d *DB, tags []int16) []int16 {
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

// keepBroadest removes tag x if another tag y in the set is broader (i.e. y is
// an ancestor of x).  Used for none (ancestor ban subsumes descendant ban) and
// some (ancestor match is easier to satisfy).
func keepBroadest(d *DB, tags []int16) []int16 {
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

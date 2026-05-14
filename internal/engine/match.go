package engine

import (
	"fmt"
	"math/bits"
)

// Matching turns a Position's leg slice into a name->Leg binding that satisfies
// a Rule's declared leg slots. The matcher itself is dumb about formulas; once
// it returns a binding, rulebook.Evaluate handles constraint evaluation and
// formula compilation. Separated from rulebook.go so the hot-path optimization
// (bitmask candidate sets, popcount slot ordering, singleton fast-path, DFS
// fallback) lives on its own and stays easy to grok.

// tryMatch dispatches by rule shape. The two shapes today:
//   - legs_pattern: "all_options" — open-ended option-only multi-leg rules
//     (butterflies, condors, generic_limited_risk_combo).
//   - legs: [...] — explicit slot list with fixed cardinality.
func (rb *Rulebook) tryMatch(pos Position, rule Rule) (map[string]Leg, bool) {
	if rule.Match.LegsPattern == "all_options" {
		if len(pos.Legs) == 0 {
			return nil, false
		}
		bound := map[string]Leg{}
		for i, l := range pos.Legs {
			if l.Kind != OptionKind {
				return nil, false
			}
			bound[fmt.Sprintf("L%d", i)] = l
		}
		if len(bound) < rule.Match.MinLegs {
			return nil, false
		}
		if rule.Match.MaxLegs > 0 && len(bound) > rule.Match.MaxLegs {
			return nil, false
		}
		return bound, true
	}

	if len(rule.Match.Legs) != len(pos.Legs) {
		return nil, false
	}
	return bindSlots(pos.Legs, rule.Match.Legs)
}

// maxSlots is the hard cap on legs/slots per rule. 16 lets the candidate sets
// fit in a uint16 bitmask; the manual never goes above 8 (boxes/iron condors).
const maxSlots = 16

// bindSlots assigns each declared slot to a distinct position leg such that
// every slot's attribute predicate (side/kind/option_type/venue) is satisfied.
//
// Hot-path optimizations vs. the original O(n!) backtracker:
//
//  1. Candidate sets are uint16 bitmasks stored in a fixed [maxSlots]uint16
//     array — zero heap allocations during matching itself.
//  2. Slots are visited most-constrained-first (fewest candidates) via an
//     in-place insertion sort over a fixed [maxSlots]int order array.
//  3. Singleton-disjoint fast-path: if every slot has exactly one candidate
//     and the singletons are pairwise distinct, we assign directly without
//     descending into DFS. Every current rule in the rulebook hits this path
//     because every slot pattern is unique (e.g. {side:long,kind:option} vs
//     {side:short,kind:option}), giving O(n·m) total work where n=slots,
//     m=legs.
//  4. DFS iterates remaining candidates by popping the lowest set bit
//     (`cand & -cand`) instead of scanning a slice. Used-leg state is the
//     same uint16 mask, intersected with each slot's candidate set so we
//     never re-test legs that already failed the slot predicate.
//
// The recursion itself allocates nothing; only the final success map is
// allocated, sized exactly to len(slots).
func bindSlots(legs []Leg, slots []LegSlot) (map[string]Leg, bool) {
	n := len(slots)
	if n == 0 || n > maxSlots || len(legs) > maxSlots {
		return nil, false
	}

	// 1. Build candidate bitmask per slot.
	var candidates [maxSlots]uint16
	for si, slot := range slots {
		var mask uint16
		for li, leg := range legs {
			if slotMatches(slot, leg) {
				mask |= uint16(1) << uint(li)
			}
		}
		if mask == 0 {
			return nil, false
		}
		candidates[si] = mask
	}

	// 2. Sort slot visit order by ascending candidate-count (popcount). Tiny
	// n, no need for sort.Slice — straight-line insertion sort is faster and
	// keeps the recursion allocation-free.
	var order [maxSlots]int
	for i := range n {
		order[i] = i
	}
	for i := 1; i < n; i++ {
		oi := order[i]
		pi := popcount16(candidates[oi])
		j := i - 1
		for j >= 0 && popcount16(candidates[order[j]]) > pi {
			order[j+1] = order[j]
			j--
		}
		order[j+1] = oi
	}

	// 3. Singleton-disjoint fast-path. Every existing rule hits this.
	var union uint16
	allSingleton := true
	for i := range n {
		c := candidates[order[i]]
		if c&(c-1) != 0 { // more than one bit set → not a singleton
			allSingleton = false
			break
		}
		if union&c != 0 { // collision with an earlier singleton
			allSingleton = false
			break
		}
		union |= c
	}
	if allSingleton {
		bound := make(map[string]Leg, n)
		for si, slot := range slots {
			bound[slot.Name] = legs[bits.TrailingZeros16(candidates[si])]
		}
		return bound, true
	}

	// 4. General case: DFS with bitmask state. assign[si] = legIndex.
	var assign [maxSlots]int
	var dfs func(depth int, used uint16) bool
	dfs = func(depth int, used uint16) bool {
		if depth == n {
			return true
		}
		si := order[depth]
		// Try candidates not already used; iterate bits low→high.
		remaining := candidates[si] & ^used
		for remaining != 0 {
			bit := remaining & -remaining // lowest set bit
			remaining ^= bit
			assign[si] = bits.TrailingZeros16(bit)
			if dfs(depth+1, used|bit) {
				return true
			}
		}
		return false
	}
	if !dfs(0, 0) {
		return nil, false
	}
	bound := make(map[string]Leg, n)
	for si, slot := range slots {
		bound[slot.Name] = legs[assign[si]]
	}
	return bound, true
}

// BindSlotsAll is the exported wrapper around bindSlotsAll for the Layer-0.5
// optimizer (which lives in a sibling internal package and cannot reach the
// unexported function). Semantics, determinism guarantees, and preconditions
// are identical to bindSlotsAll's documentation immediately below.
func BindSlotsAll(legs []Leg, slots []LegSlot) [][]int {
	return bindSlotsAll(legs, slots)
}

// bindSlotsAll enumerates every assignment of slot → leg index that satisfies
// each slot's attribute predicate (side/kind/option_type/venue). Unlike
// bindSlots — which short-circuits on the singleton-disjoint fast-path and
// returns the first DFS leaf — bindSlotsAll always walks the full DFS and
// returns every complete assignment. Used by the Layer-0.5 optimizer's
// candidate enumerator, where "the first binding" isn't enough: e.g. a
// vertical_spread template against four long calls + four short calls has 16
// distinct slot assignments and the optimizer must score each.
//
// Each entry in the returned slice is a []int of length len(slots), where
// element si is the leg index bound to slots[si] (i.e. iteration order matches
// the slot declaration order, NOT the popcount-sorted visit order used
// internally by bindSlots). Output enumeration order is deterministic: slots
// are visited in declaration order and candidate legs are tried low-index
// first. An empty return ([] / nil) means no assignment satisfies the slots.
//
// Precondition: the rule does not use legs_pattern. Callers should restrict
// bindSlotsAll to fixed-slot rules; behavior for catch-all rules is undefined
// (and OptimizerTargets excludes them by default policy).
func bindSlotsAll(legs []Leg, slots []LegSlot) [][]int {
	n := len(slots)
	if n == 0 || n > maxSlots || len(legs) > maxSlots {
		return nil
	}

	// Per-slot candidate bitmasks. Same construction as bindSlots.
	var candidates [maxSlots]uint16
	for si, slot := range slots {
		var mask uint16
		for li, leg := range legs {
			if slotMatches(slot, leg) {
				mask |= uint16(1) << uint(li)
			}
		}
		if mask == 0 {
			return nil
		}
		candidates[si] = mask
	}

	// Walk slots in declaration order. Iterating low bit → high bit yields a
	// deterministic, lex-ascending enumeration on (assign[0], assign[1], ...).
	var out [][]int
	var assign [maxSlots]int
	var dfs func(depth int, used uint16)
	dfs = func(depth int, used uint16) {
		if depth == n {
			leaf := make([]int, n)
			copy(leaf, assign[:n])
			out = append(out, leaf)
			return
		}
		remaining := candidates[depth] & ^used
		for remaining != 0 {
			bit := remaining & -remaining
			remaining ^= bit
			assign[depth] = bits.TrailingZeros16(bit)
			dfs(depth+1, used|bit)
		}
	}
	dfs(0, 0)
	return out
}

func popcount16(x uint16) int { return bits.OnesCount16(x) }

func slotMatches(s LegSlot, l Leg) bool {
	if s.Side != "" && Side(s.Side) != l.Side {
		return false
	}
	if s.Kind != "" && Kind(s.Kind) != l.Kind {
		return false
	}
	if s.OptionType != "" && s.OptionType != l.OptionType {
		return false
	}
	if s.Venue != "" && s.Venue != l.Venue {
		return false
	}
	return true
}

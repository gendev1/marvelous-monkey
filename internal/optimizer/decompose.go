// Branch-and-bound search core for the Layer-0.5 spread optimizer. The search
// walks every (ruleID, assignment) candidate at every state, races the result
// against a residual-only completion, and keeps the cheapest decomposition
// per the four-level tiebreaker in less(). All work is memoized by
// State.Key() so equivalent sub-states share work across branches.
//
// Lower bound is fixed at 0 — strategies REDUCE margin vs. the naked sum, so
// any naked-sum admissible bound would over-prune optimal branches. See
// docs/architecture/test.md §"B&B core" and TestLowerBoundDocumented.
package optimizer

import (
	"math"
	"sort"
	"strings"

	"margincalc/internal/engine"
)

// decompose returns the cheapest Decomposition for state per less(). Memo is
// keyed by State.Key(); residualErrs records the strongest residual error
// encountered at each state (used at the root when every branch yields +Inf).
//
// A returned err is a hard engine error from EvaluateRule — callers
// (Optimize) propagate it. A returned Decomposition with TotalRequirement ==
// +Inf means no template branch nor residual completion satisfied the state.
//
// Memoization is correct because the optimization is purely a function of
// State (the remaining inventory) and the immutable BucketFacts — neither
// the prefix path nor accumulated cost feeds back into the recursion.
func (o *Optimizer) decompose(state State, facts BucketFacts, memo map[string]Decomposition, residualErrs map[string]error) (Decomposition, error) {
	if len(state.Legs) == 0 {
		return Decomposition{
			TotalRequirement:  0,
			AttributionsByLeg: map[LegID][]Attribution{},
		}, nil
	}
	key := state.Key()
	if cached, ok := memo[key]; ok {
		return cached, nil
	}
	// Mark state as in-flight with +Inf so cycles (none expected — every
	// branch strictly shrinks the state — but cheap insurance) terminate.
	memo[key] = Decomposition{TotalRequirement: math.Inf(+1)}

	best := Decomposition{TotalRequirement: math.Inf(+1)}

	// 1. Race the residual-only completion at every node so a cheaper naked
	// score can outscore template branches. A residual failure does NOT
	// short-circuit branch search: stash the strongest residual error
	// keyed by state for the +Inf-root partial-output path.
	resDecomp, resErr := o.scoreAllResidual(state, facts)
	if resErr == nil {
		if less(resDecomp, best) {
			best = resDecomp
		}
	} else {
		recordResidualError(residualErrs, key, resErr)
	}

	// 2. Branch over every (ruleID, assignment) the rulebook lets us pick.
	for _, ruleID := range o.rb.OptimizerTargets() {
		assignments := o.enumerateAssignments(ruleID, state.Legs)
		for _, assignment := range assignments {
			plan, ok, err := consumptionFor(ruleID, assignment, facts)
			if err != nil {
				return Decomposition{}, err
			}
			if !ok {
				continue
			}
			pos, slots := buildSubPosition(ruleID, assignment, plan, facts)
			res, ok, err := o.rb.EvaluateRule(pos, ruleID, o.accountType, o.phase)
			if err != nil {
				return Decomposition{}, err
			}
			if !ok {
				continue
			}
			sub := SubPosition{
				StrategyID: ruleID,
				Slots:      slots,
				Result:     res,
			}
			nextState := applyConsumption(state, assignment, plan)
			tail, err := o.decompose(nextState, facts, memo, residualErrs)
			if err != nil {
				return Decomposition{}, err
			}
			if math.IsInf(tail.TotalRequirement, +1) {
				continue
			}
			combined := combine(sub, tail)
			if less(combined, best) {
				best = combined
			}
		}
	}

	memo[key] = best
	return best, nil
}

// recordResidualError keeps the strongest residual error per state per
// compareResidualErr. Used so Optimize can return a meaningful diagnostic
// when the root state has no successful branch at all.
func recordResidualError(out map[string]error, key string, err error) {
	if existing, ok := out[key]; ok {
		out[key] = pickStronger(existing, err)
		return
	}
	out[key] = err
}

// enumerateAssignments converts engine.BindSlotsAll's []int leg-index
// assignments into slot-name → WorkingLeg maps suitable for consumptionFor /
// buildSubPosition. Defensive filter: assignments where any bound slot has
// OpenQty / OpenShares ≤ epsQty are skipped (buildState should already have
// pruned such legs but the optimizer is the one place that can't afford a
// silent off-by-one).
func (o *Optimizer) enumerateAssignments(ruleID string, legs []WorkingLeg) []map[string]WorkingLeg {
	rule, ok := o.rb.RuleByID(ruleID)
	if !ok {
		return nil
	}
	if len(rule.Match.Legs) == 0 {
		// legs_pattern catch-alls (only generic_limited_risk_combo today)
		// are excluded from OptimizerTargets, but defend in depth.
		return nil
	}
	engineLegs := make([]engine.Leg, len(legs))
	for i, wl := range legs {
		engineLegs[i] = wl.Leg
	}
	raw := engine.BindSlotsAll(engineLegs, rule.Match.Legs)
	if len(raw) == 0 {
		return nil
	}
	out := make([]map[string]WorkingLeg, 0, len(raw))
	for _, idxs := range raw {
		assignment := make(map[string]WorkingLeg, len(idxs))
		valid := true
		for si, slot := range rule.Match.Legs {
			wl := legs[idxs[si]]
			if wl.OpenQty <= epsQty && wl.OpenShares <= epsQty {
				valid = false
				break
			}
			assignment[slot.Name] = wl
		}
		if !valid {
			continue
		}
		out = append(out, assignment)
	}
	return out
}

// combine prepends sub to tail's SubPositions, shifts tail's Attributions by
// +1 to track the new index of the prepended sub, and adds sub's own
// attribution at index 0. Total requirement adds (sub-positions are
// independent — engine returns each rule's own Result.Requirement).
func combine(sub SubPosition, tail Decomposition) Decomposition {
	subs := make([]SubPosition, 0, len(tail.SubPositions)+1)
	subs = append(subs, sub)
	subs = append(subs, tail.SubPositions...)

	attrs := make(map[LegID][]Attribution, len(tail.AttributionsByLeg)+len(sub.Slots))
	for _, name := range sortedSlotNamesAssign(sub.Slots) {
		sa := sub.Slots[name]
		attrs[sa.OriginalLegID] = append(attrs[sa.OriginalLegID], Attribution{
			SubPositionIdx: 0,
			SlotName:       name,
			ConsumedQty:    sa.ConsumedQty,
			ConsumedShares: sa.ConsumedShares,
			Reason:         "template " + sub.StrategyID,
		})
	}
	for legID, list := range tail.AttributionsByLeg {
		for _, a := range list {
			a.SubPositionIdx++
			attrs[legID] = append(attrs[legID], a)
		}
	}
	return Decomposition{
		SubPositions:      subs,
		AttributionsByLeg: attrs,
		TotalRequirement:  sub.Result.Requirement + tail.TotalRequirement,
	}
}

// less is the four-level tiebreaker from docs/architecture/test.md
// §"Tiebreakers": (1) lower TotalRequirement; (2) fewer SubPositions; (3)
// lex-smaller sorted-by-StrategyID list; (4) lex-smaller sorted-by-OriginalLegID
// flattened slot keys. Used both for memo update comparisons and for final
// selection inside decompose. A +Inf TotalRequirement is "infinitely worse"
// than any finite value so candidates with no completion never win.
func less(a, b Decomposition) bool {
	if a.TotalRequirement != b.TotalRequirement {
		return a.TotalRequirement < b.TotalRequirement
	}
	if len(a.SubPositions) != len(b.SubPositions) {
		return len(a.SubPositions) < len(b.SubPositions)
	}
	aSids := strategyIDsSorted(a)
	bSids := strategyIDsSorted(b)
	if cmp := compareStringSlices(aSids, bSids); cmp != 0 {
		return cmp < 0
	}
	aLegs := slotLegIDsSorted(a)
	bLegs := slotLegIDsSorted(b)
	return compareStringSlices(aLegs, bLegs) < 0
}

func strategyIDsSorted(d Decomposition) []string {
	ids := make([]string, len(d.SubPositions))
	for i, sp := range d.SubPositions {
		ids[i] = sp.StrategyID
	}
	sort.Strings(ids)
	return ids
}

func slotLegIDsSorted(d Decomposition) []string {
	ids := make([]string, 0, len(d.SubPositions)*2)
	for _, sp := range d.SubPositions {
		for _, sa := range sp.Slots {
			ids = append(ids, string(sa.OriginalLegID))
		}
	}
	sort.Strings(ids)
	return ids
}

func compareStringSlices(a, b []string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if c := strings.Compare(a[i], b[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

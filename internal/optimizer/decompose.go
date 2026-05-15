package optimizer

import (
	"errors"
	"math"
	"sort"
	"strings"

	"margincalc/internal/engine"
)

// isResidualSoftErr reports whether err is a residual-completion soft
// failure (ErrNoNakedRule / ErrStockResidualUnsupported). Soft errors mean
// "this state has no completion via the residual path" — they may dead-end
// a single branch without invalidating the whole decomposition, so callers
// at deeper recursion levels skip-and-continue rather than propagate. A
// hard error (CEL eval, configuration drift) always propagates because
// the optimizer cannot reason past it.
func isResidualSoftErr(err error) bool {
	var nr *ErrNoNakedRule
	var sr *ErrStockResidualUnsupported
	return err != nil && (errors.As(err, &nr) || errors.As(err, &sr))
}

// errorDecomposition wraps a hard error into a Decomposition so it can
// propagate through memoized recursion as a single return value. Any
// SubPositions accumulated before the failure (notably partial residual
// completion) ride along inside `partial`, honoring the optimizer's
// partial-output contract.
func errorDecomposition(err error, partial Decomposition) Decomposition {
	partial.err = err
	return partial
}

// IsError reports whether d is a propagated error carrier from
// errorDecomposition. The embedded SubPositions are still meaningful — they
// represent the partial decomposition seen before the failure.
func (d Decomposition) IsError() bool { return d.err != nil }

// Err returns the embedded error if d is an error carrier, otherwise nil.
func (d Decomposition) Err() error { return d.err }

// less is the canonical tiebreak ordering between two candidate
// Decompositions. Returns true when a strictly precedes b. The chain:
//
//  1. Smaller TotalRequirement wins.
//  2. Fewer SubPositions wins (prefer the simpler decomposition).
//  3. Lex-smaller sorted rule-ID list wins.
//  4. Lex-smaller sorted leg-ID assignment list wins.
//
// The float compare uses an absolute tolerance — exact float equality after
// an EvaluateRule path is rare even for "equivalent" decompositions.
func less(a, b Decomposition) bool {
	if math.Abs(a.TotalRequirement-b.TotalRequirement) > 1e-9 {
		return a.TotalRequirement < b.TotalRequirement
	}
	if len(a.SubPositions) != len(b.SubPositions) {
		return len(a.SubPositions) < len(b.SubPositions)
	}
	ar := strings.Join(sortedRuleIDs(a), ",")
	br := strings.Join(sortedRuleIDs(b), ",")
	if c := strings.Compare(ar, br); c != 0 {
		return c < 0
	}
	al := strings.Join(sortedAssignments(a), "|")
	bl := strings.Join(sortedAssignments(b), "|")
	return strings.Compare(al, bl) < 0
}

func sortedRuleIDs(d Decomposition) []string {
	ids := make([]string, len(d.SubPositions))
	for i, sp := range d.SubPositions {
		ids[i] = sp.StrategyID
	}
	sort.Strings(ids)
	return ids
}

// sortedAssignments serializes each sub-position as "strategyID:slot=legID,..."
// and returns the lex-sorted list. Preserving the slot→leg mapping (not just
// the multiset of leg IDs) is what the documented tiebreak chain step 4
// requires: two decompositions that pick the same legs into different slots
// can yield different downstream Attributions, so they must not compare equal.
func sortedAssignments(d Decomposition) []string {
	out := make([]string, 0, len(d.SubPositions))
	for _, sp := range d.SubPositions {
		slots := make([]string, len(sp.Slots))
		for i, sa := range sp.Slots {
			slots[i] = sa.Slot + "=" + string(sa.LegID)
		}
		out = append(out, sp.StrategyID+":"+strings.Join(slots, ","))
	}
	sort.Strings(out)
	return out
}

// combine prepends the (ruleID, assignment, plan, res) sub-position to the
// recursive `sub` decomposition and sums requirements. The new sub-position
// is recorded with slots in declared-slot order; combined with the recursive
// sub-positions that preserve their own slot order, the resulting list is
// byte-deterministic across reruns on the same input.
func combine(rb *engine.Rulebook, ruleID string, assignment map[string]WorkingLeg, plan ConsumptionPlan, res engine.Result, sub Decomposition) Decomposition {
	sp := newSubPositionRecord(rb, ruleID, assignment, plan, res)
	subs := make([]SubPosition, 0, 1+len(sub.SubPositions))
	subs = append(subs, sp)
	subs = append(subs, sub.SubPositions...)
	return Decomposition{
		SubPositions:     subs,
		TotalRequirement: res.Requirement + sub.TotalRequirement,
	}
}

// newSubPositionRecord builds the SubPosition for one matched rule. Slot
// order in SubPosition.Slots follows the rule's declared slot order so the
// downstream Attribution view is stable.
func newSubPositionRecord(rb *engine.Rulebook, ruleID string, assignment map[string]WorkingLeg, plan ConsumptionPlan, res engine.Result) SubPosition {
	rule, ok := rb.RuleByID(ruleID)
	var names []string
	if ok {
		names = make([]string, 0, len(rule.Match.Legs))
		for _, slot := range rule.Match.Legs {
			names = append(names, slot.Name)
		}
	} else {
		// Defensive: fall back to sorted assignment keys if the rule vanished.
		// In practice rb.RuleByID is the same handle that drove enumerateAssignments,
		// so this branch is unreachable today.
		for n := range assignment {
			names = append(names, n)
		}
		sort.Strings(names)
	}
	slots := make([]SlotAssignment, 0, len(names))
	for _, name := range names {
		wl, ok := assignment[name]
		if !ok {
			continue
		}
		c := plan.Slots[name]
		slots = append(slots, SlotAssignment{
			Slot:       name,
			LegID:      wl.ID,
			QtyUsed:    c.Qty,
			SharesUsed: c.Shares,
		})
	}
	return SubPosition{StrategyID: ruleID, Slots: slots, Result: res}
}

// buildSubPosition assembles the engine.Position to score for a candidate
// (ruleID, assignment, plan). Legs appear in the rule's declared slot order
// with Qty replaced by the planned consumption — every other leg field
// (K, P, Style, Underlying, etc.) carries through from the working leg.
func buildSubPosition(rb *engine.Rulebook, ruleID string, assignment map[string]WorkingLeg, plan ConsumptionPlan, facts BucketFacts) engine.Position {
	rule, ok := rb.RuleByID(ruleID)
	if !ok {
		return engine.Position{}
	}
	legs := make([]engine.Leg, 0, len(rule.Match.Legs))
	for _, slot := range rule.Match.Legs {
		wl, ok := assignment[slot.Name]
		if !ok {
			continue
		}
		leg := wl.Leg
		c := plan.Slots[slot.Name]
		if c.Qty > 0 {
			leg.Qty = c.Qty
		}
		if c.Shares > 0 {
			// Stock-coverage sub-positions consume only the coverage portion
			// of the leg's shares. Dollar-denominated stock fields (notably
			// ShortSaleProceeds) must scale proportionally so the rule's
			// formula sees a consistent (shares, proceeds) pair — sale_price
			// is per-share and stays as-is. Ratio uses the working leg's
			// original Leg.Shares, which applyConsumption leaves untouched
			// across partial-consumption rounds, so the ratio remains
			// correct even when the same stock leg is sliced multiple times.
			if leg.Kind == engine.StockKind && wl.Leg.Shares > 0 {
				ratio := c.Shares / wl.Leg.Shares
				leg.ShortSaleProceeds = wl.Leg.ShortSaleProceeds * ratio
			}
			leg.Shares = c.Shares
		}
		legs = append(legs, leg)
	}
	return engine.Position{
		Legs:                    legs,
		U:                       facts.U,
		Class:                   facts.Class,
		Lev:                     facts.Lev,
		UnderlyingIsEquityBased: facts.UnderlyingIsEquityBased,
	}
}

// decomposeStats lets tests assert memoization behavior without exporting
// the search internals. Nil is the production path; a non-nil pointer
// records (1) total decompose entries and (2) memo hits. The two together
// are sufficient to verify "same state hits memo on second visit": a memo
// hit increments Hits without increasing the effective work.
type decomposeStats struct {
	Calls int
	Hits  int
}

// decompose is the branch-and-bound core. For each state it enumerates
// every (optimizer-target rule, valid assignment, viable consumption plan)
// triple, scores the slice via EvaluateRule, recurses on the remainder,
// and keeps the best decomposition under `less`. Residual-only completion
// is always considered as a baseline; together they guarantee that any
// state which can be scored at all is scored by the call.
//
// Memoization: the per-state Key() identifies a node uniquely; once a state
// is scored, every revisit returns the cached Decomposition. This bounds
// total work by the number of distinct reachable states, which for the
// option-only families considered here is small.
//
// Error semantics: a hard engine error short-circuits via errorDecomposition.
// A residual-only failure (no B&B candidate fit AND scoreAllResidual
// returned an error) returns the strongest residual error plus whatever
// partial sub-positions were scored before the failure — matching the
// partial-output contract enforced for the residual-only path in #72.
//
// Lower bound: 0 in v1. The naked-sum is NOT admissible (a vertical
// requirement is much smaller than the sum of its two naked legs), so a
// "skip if currentCost + sum > best" prune would be wrong. Documented here
// so a future tightening doesn't accidentally regress.
func (o *Optimizer) decompose(s State, facts BucketFacts, memo map[string]Decomposition, stats *decomposeStats) Decomposition {
	o.nodes++
	if stats != nil {
		stats.Calls++
	}
	key := s.Key()
	if d, ok := memo[key]; ok {
		if stats != nil {
			stats.Hits++
		}
		return d
	}
	if len(s.Legs) == 0 {
		zero := Decomposition{}
		memo[key] = zero
		return zero
	}

	var best Decomposition
	haveBest := false

	for _, ruleID := range o.rb.OptimizerTargets() {
		for _, assignment := range enumerateAssignments(o.rb, ruleID, s.Legs) {
			plan, ok, err := consumptionFor(ruleID, assignment, facts)
			if err != nil {
				return errorDecomposition(err, Decomposition{})
			}
			if !ok {
				continue
			}
			slicedPos := buildSubPosition(o.rb, ruleID, assignment, plan, facts)
			res, fit, err := o.rb.EvaluateRule(slicedPos, ruleID, facts.AccountType, facts.Phase)
			if err != nil {
				return errorDecomposition(err, Decomposition{})
			}
			if !fit {
				continue
			}
			consumed := applyConsumption(s, assignment, plan)
			sub := o.decompose(consumed, facts, memo, stats)
			if sub.IsError() && !isResidualSoftErr(sub.err) {
				// Hard error (CEL/configuration) propagates so the caller
				// hears about the most actionable failure.
				return sub
			}
			combined := combine(o.rb, ruleID, assignment, plan, res, sub)
			// A soft residual error in the recursion (typically a stock
			// residual that no further template can absorb) is propagated
			// up the chain alongside the partial decomposition — the parent
			// sub-position is still recorded so callers see the most
			// complete attribution possible.
			if sub.IsError() {
				combined.err = sub.err
			}
			if !haveBest || isBetterCandidate(combined, best) {
				best = combined
				haveBest = true
			}
		}
	}

	residual, rerr := o.scoreAllResidual(s, facts)
	if rerr == nil {
		if !haveBest || isBetterCandidate(residual, best) {
			best = residual
		}
	} else {
		residualCarrier := errorDecomposition(rerr, residual)
		if !haveBest {
			// No B&B branch fit and residual returned an error. Carry the
			// partial residual decomposition (it may contain successful sub-
			// scores) so the caller can surface them alongside the error.
			memo[key] = residualCarrier
			return residualCarrier
		}
		// haveBest: if the best B&B branch itself carries a soft residual
		// error, the residual baseline is also a valid candidate — compare
		// them so the cheaper partial decomposition wins.
		if best.IsError() && isBetterCandidate(residualCarrier, best) {
			best = residualCarrier
		}
	}

	memo[key] = best
	return best
}

// isBetterCandidate ranks a above b. An error-free decomposition always beats
// an error-flagged one (a complete, scored decomposition is strictly better
// than a partial one that leaves residual unsupported). When both candidates
// carry an error, the one with the smaller unattributed-share count wins —
// i.e. the partial decomposition that managed to consume more state ranks
// higher than a baseline that left more state stranded. Final tiebreak is
// the documented `less` order.
func isBetterCandidate(a, b Decomposition) bool {
	aErr := a.IsError()
	bErr := b.IsError()
	if aErr != bErr {
		return !aErr
	}
	if aErr {
		aRem := residualSharesInErr(a.err)
		bRem := residualSharesInErr(b.err)
		if aRem != bRem {
			return aRem < bRem
		}
	}
	return less(a, b)
}

// residualSharesInErr extracts the unattributed share count from a residual
// error. ErrStockResidualUnsupported.OpenShares is the canonical signal;
// other error kinds carry no share count and return 0 (treated as "tied" so
// `less` decides).
func residualSharesInErr(err error) float64 {
	var s *ErrStockResidualUnsupported
	if errors.As(err, &s) {
		return s.OpenShares
	}
	return 0
}

package optimizer

import (
	"fmt"

	"margincalc/internal/engine"
)

// residualCandidates is the deterministic sequence of single-leg "naked" rule
// IDs residualOptionRule walks per docs/architecture/test.md §"Residual option
// handling". Order is intentional: short-dated long options short-circuit
// before the venue-specific long-dated branches; short calls/puts come last.
var residualCandidates = []string{
	"long_option_short_dated",
	"long_option_long_dated_listed",
	"long_option_long_dated_otc",
	"short_call_uncovered",
	"short_put_uncovered",
}

// residualOptionRule scores wl as a naked single-leg sub-position. It walks
// residualCandidates in order and returns the first SubPosition whose
// EvaluateRule call returns (Result, true, nil).
//
// Returns:
//   - SubPosition, nil               — first candidate that binds.
//   - SubPosition{}, *ErrNoNakedRule — every candidate returned (_, false, nil).
//   - SubPosition{}, err             — a real engine error (CEL eval / lookup);
//     propagated untouched per §"Strongest-residual-error priority" item 1.
func (o *Optimizer) residualOptionRule(wl WorkingLeg, facts BucketFacts) (SubPosition, error) {
	// Clone the leg with Qty pinned to OpenQty so the formula sees the actual
	// open size (not whatever Qty was in the originally-supplied Leg).
	scoredLeg := wl.Leg
	scoredLeg.Qty = wl.OpenQty

	pos := engine.Position{
		Legs:                    []engine.Leg{scoredLeg},
		U:                       facts.U,
		Class:                   facts.Class,
		Lev:                     facts.Lev,
		UnderlyingIsEquityBased: facts.UnderlyingIsEquityBased,
	}

	for _, ruleID := range residualCandidates {
		res, ok, err := o.rb.EvaluateRule(pos, ruleID, o.accountType, o.phase)
		if err != nil {
			return SubPosition{}, fmt.Errorf("optimizer: EvaluateRule(%s) for leg %q: %w", ruleID, string(wl.ID), err)
		}
		if !ok {
			continue
		}
		sub := SubPosition{
			StrategyID: ruleID,
			Slots: map[string]SlotAssignment{
				// All naked candidates declare a single slot named "opt".
				"opt": {
					OriginalLegID:  wl.ID,
					Leg:            scoredLeg,
					ConsumedQty:    wl.OpenQty,
					ConsumedShares: 0,
				},
			},
			Result: res,
		}
		return sub, nil
	}
	return SubPosition{}, &ErrNoNakedRule{LegID: wl.ID, Leg: wl.Leg}
}

// scoreAllResidual is the PR-2 entry into per-leg residual scoring. Legs are
// processed in State.Legs order (already sorted by LegID), so iteration is
// deterministic and no map iteration occurs while building output.
//
// Stock-like legs (OpenShares > eps) emit *ErrStockResidualUnsupported.
// Option legs are sent through residualOptionRule. All errors are collected;
// successful sub-positions accumulate into the partial Decomposition. After
// the pass, if any errors were collected, the strongest one (per
// compareResidualErr) is returned alongside the partial Decomposition.
func (o *Optimizer) scoreAllResidual(state State, facts BucketFacts) (Decomposition, error) {
	subs := make([]SubPosition, 0, len(state.Legs))
	attrs := make(map[LegID][]Attribution, len(state.Legs))
	var strongest error
	var total float64

	for _, wl := range state.Legs {
		if wl.OpenShares > epsQty {
			stockErr := &ErrStockResidualUnsupported{LegID: wl.ID, OpenShares: wl.OpenShares}
			strongest = pickStronger(strongest, stockErr)
			continue
		}
		sub, err := o.residualOptionRule(wl, facts)
		if err != nil {
			strongest = pickStronger(strongest, err)
			continue
		}
		idx := len(subs)
		subs = append(subs, sub)
		total += sub.Result.Requirement
		recordAttribution(attrs, sub, idx)
	}

	decomp := Decomposition{
		SubPositions:      subs,
		AttributionsByLeg: attrs,
		TotalRequirement:  total,
	}
	return decomp, strongest
}

// pickStronger returns whichever of a, b is the stronger residual error per
// compareResidualErr. If exactly one is nil, the non-nil one wins.
func pickStronger(a, b error) error {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if compareResidualErr(a, b) >= 0 {
		return a
	}
	return b
}

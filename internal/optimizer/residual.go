package optimizer

import (
	"margincalc/internal/engine"
)

// nakedCandidateRules is the deterministic sequence of rule IDs that residual
// completion tries for an option leg with OpenQty > 0. Order is load-bearing:
// the first candidate that EvaluateRule reports as a clean match wins, which
// is exactly the production dispatch order in cboe_baseline.yaml. Keeping it
// hard-coded here (rather than re-deriving from the rulebook) makes the
// residual contract explicit — naked completion is a closed sequence, not a
// general scan, and a new naked sink rule must be added here intentionally.
var nakedCandidateRules = []string{
	"long_option_short_dated",
	"long_option_long_dated_listed",
	"long_option_long_dated_otc",
	"short_call_uncovered",
	"short_put_uncovered",
}

// residualOptionRule scores a single working option leg against the naked-rule
// sequence. Returns the first SubPosition that EvaluateRule reports as a
// clean match. If every candidate cleanly declines (no match / constraint /
// requires false), returns *ErrNoNakedRule. A hard engine error from any
// candidate aborts the search and is returned as-is so it can outrank weaker
// residual errors via compareResidualErr.
func residualOptionRule(rb *engine.Rulebook, wl WorkingLeg, facts BucketFacts) (SubPosition, error) {
	leg := wl.Leg
	leg.Qty = wl.OpenQty
	pos := engine.Position{
		Legs:                    []engine.Leg{leg},
		U:                       facts.U,
		Class:                   facts.Class,
		Lev:                     facts.Lev,
		UnderlyingIsEquityBased: facts.UnderlyingIsEquityBased,
	}
	for _, ruleID := range nakedCandidateRules {
		res, ok, err := rb.EvaluateRule(pos, ruleID, facts.AccountType, facts.Phase)
		if err != nil {
			return SubPosition{}, err
		}
		if !ok {
			continue
		}
		slotName := residualSlotName(rb, ruleID)
		return SubPosition{
			StrategyID: ruleID,
			Slots: []SlotAssignment{{
				Slot:    slotName,
				LegID:   wl.ID,
				QtyUsed: wl.OpenQty,
			}},
			Result: res,
		}, nil
	}
	return SubPosition{}, &ErrNoNakedRule{LegID: wl.ID, Leg: wl.Leg}
}

// residualSlotName looks up the single slot name a naked rule declares so the
// SubPosition records the same identifier the rule's match block did. Falls
// back to "opt" — the convention used by every naked rule in
// cboe_baseline.yaml — if the rule is missing, has no slots, or uses a
// legs_pattern (none of which apply to nakedCandidateRules today, but we
// don't want a future rulebook-wide refactor to crash residual scoring).
func residualSlotName(rb *engine.Rulebook, ruleID string) string {
	rule, ok := rb.RuleByID(ruleID)
	if !ok || len(rule.Match.Legs) == 0 {
		return "opt"
	}
	return rule.Match.Legs[0].Name
}

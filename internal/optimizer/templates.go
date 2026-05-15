package optimizer

import (
	"margincalc/internal/engine"
)

// enumerateAssignments returns every valid (slot → working-leg) binding for
// `ruleID` against the State's working legs. Built on engine.BindSlotsAll so
// the optimizer reuses the engine's tested slot-matcher; this function only
// adapts WorkingLeg → engine.Leg (with Qty := OpenQty) and inverts the index
// rows BindSlotsAll returns into per-slot maps the rest of the optimizer
// consumes.
//
// Returns nil when the rule has no fixed slot list (legs_pattern catch-all is
// not an optimizer target in this PR's set) or when the matcher rejects every
// candidate. Constraint and requires checks are intentionally deferred to
// EvaluateRule at scoring time — running them here would duplicate the
// rulebook's logic without saving search work in any meaningful way.
func enumerateAssignments(rb *engine.Rulebook, ruleID string, legs []WorkingLeg) []map[string]WorkingLeg {
	rule, ok := rb.RuleByID(ruleID)
	if !ok {
		return nil
	}
	if rule.Match.LegsPattern != "" || len(rule.Match.Legs) == 0 {
		return nil
	}
	rawLegs := make([]engine.Leg, len(legs))
	for i, wl := range legs {
		l := wl.Leg
		l.Qty = wl.OpenQty
		rawLegs[i] = l
	}
	rows := engine.BindSlotsAll(rawLegs, rule.Match.Legs)
	if len(rows) == 0 {
		return nil
	}
	out := make([]map[string]WorkingLeg, 0, len(rows))
	for _, row := range rows {
		m := make(map[string]WorkingLeg, len(rule.Match.Legs))
		for si, slot := range rule.Match.Legs {
			m[slot.Name] = legs[row[si]]
		}
		out = append(out, m)
	}
	return out
}

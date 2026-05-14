package optimizer

import "math"

// ConsumedAmount is the amount drawn from one slot's input WorkingLeg by a
// single consumption plan. Exactly one of Qty / Shares is populated for every
// slot in v1: option slots use Qty (contracts), stock-like slots use Shares.
// Both populated would be malformed; the matcher / requires-block prevent
// option ↔ stock cross-binding upstream.
type ConsumedAmount struct {
	Qty    float64
	Shares float64
}

// ConsumptionPlan records, for one (ruleID, assignment) pair, how much of
// each slot's input leg the resulting sub-position will consume. PerSlot is
// keyed by rule slot name (matching engine LegSlot.Name).
type ConsumptionPlan struct {
	PerSlot map[string]ConsumedAmount
}

// floorEps rounds x down to the nearest integer with an epsQty tolerance so
// that pure-arithmetic equalities like 5.0000000003 → 5 instead of 5 → 5.
func floorEps(x float64) float64 {
	return math.Floor(x + epsQty)
}

// consumptionFor returns the deliverable-units consumption plan for the
// (ruleID, assignment) pair, or (_, false, nil) if the rule cannot bind at
// any positive quantity (e.g. a slot's mult is non-positive, or the
// deliverable units round below 1). PR-3 covers the option-only families:
// vertical_spread, short_strangle_or_straddle, long_box_spread,
// short_box_spread. Every other ruleID returns (_, false, nil) so Optimize
// silently skips templates whose recipes land in PR-4 (stock coverage) or
// PR-5 (ETF notional). An unknown ruleID is not an error path here — the
// optimizer only calls consumptionFor with ruleIDs from
// Rulebook.OptimizerTargets(), which is rulebook-controlled.
//
// Deliverable-units formula (docs/architecture/test.md §"Rounding & quantity
// policy", §"Rule-aware consumption"): units := min over slots of
// (openQty_i * mult_i); consumedQty_i := units / mult_i. For boxes /
// strangles where engine `requires` enforces uniform mult across slots, this
// collapses to consumedQty_i = min(OpenQty).
func consumptionFor(ruleID string, assignment map[string]WorkingLeg, _ BucketFacts) (ConsumptionPlan, bool, error) {
	switch ruleID {
	case "vertical_spread",
		"short_strangle_or_straddle",
		"long_box_spread",
		"short_box_spread":
		return optionDeliverableUnits(assignment)
	default:
		// Stock-coverage (PR-4) and ETF (PR-5) recipes land later — until
		// then, every other rule template is silently skipped so Optimize
		// keeps progressing with whatever templates / residuals do fit.
		return ConsumptionPlan{}, false, nil
	}
}

// optionDeliverableUnits applies the deliverable-units formula across an
// option-only assignment. Returns (_, false, nil) if any slot has Mult <= 0
// or if the deliverable rounds to fewer than 1 unit; otherwise returns a
// plan with the per-slot ConsumedQty set to floor_eps(units / mult_i).
func optionDeliverableUnits(assignment map[string]WorkingLeg) (ConsumptionPlan, bool, error) {
	if len(assignment) == 0 {
		return ConsumptionPlan{}, false, nil
	}
	minDeliverable := math.Inf(+1)
	for _, wl := range assignment {
		if wl.Leg.Mult <= 0 {
			return ConsumptionPlan{}, false, nil
		}
		if wl.OpenQty <= epsQty {
			return ConsumptionPlan{}, false, nil
		}
		d := wl.OpenQty * wl.Leg.Mult
		if d < minDeliverable {
			minDeliverable = d
		}
	}
	units := floorEps(minDeliverable)
	if units < 1 {
		return ConsumptionPlan{}, false, nil
	}
	plan := ConsumptionPlan{PerSlot: make(map[string]ConsumedAmount, len(assignment))}
	for name, wl := range assignment {
		consumed := floorEps(units / wl.Leg.Mult)
		if consumed < 1 {
			return ConsumptionPlan{}, false, nil
		}
		plan.PerSlot[name] = ConsumedAmount{Qty: consumed}
	}
	return plan, true, nil
}

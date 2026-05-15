package optimizer

import (
	"math"

	"margincalc/internal/engine"
)

// consumptionEps is the rounding tolerance for the deliverable-units snap. A
// candidate consumption is rejected only when it would yield less than a
// whole contract (consumedQty < 1 - eps) so float drift on near-1 inputs
// doesn't spuriously kill a viable branch.
const consumptionEps = 1e-9

// ConsumedAmount is the per-slot consumption record: how much option quantity
// (and/or how many shares) of the underlying working leg is attributed to
// this sub-position.
type ConsumedAmount struct {
	Qty    float64
	Shares float64
}

// ConsumptionPlan is the full per-rule consumption plan keyed by slot name.
type ConsumptionPlan struct {
	Slots map[string]ConsumedAmount
}

// floor_eps is the canonical float-snapping helper used by the
// deliverable-units math. The eps absorbs the small additive drift that
// shows up after a subtraction like (10 - 5) producing 4.999999999... .
//
//revive:disable-next-line:var-naming helper named per the issue spec
func floor_eps(x float64) float64 { return math.Floor(x + consumptionEps) }

// optionOnlyConsumption is the deliverable-units recipe for an all-option
// strategy. Given the assignment and the list of relevant slot names, it
// returns the ConsumptionPlan that uses the largest whole-unit slice across
// those slots:
//
//	units      = min_i (openQty_i * mult_i), snapped down so units/mult_i is
//	             a non-negative integer for every i.
//	consumedQty_i = units / mult_i
//
// Returns ok=false when any slot is missing, non-option, has non-positive
// mult, or when the snap yields consumedQty < 1 for any slot. The latter
// branch is the "this template can't slice a whole contract here" signal —
// the optimizer must skip, not error.
func optionOnlyConsumption(assignment map[string]WorkingLeg, slots []string) (ConsumptionPlan, bool) {
	if len(slots) == 0 {
		return ConsumptionPlan{}, false
	}
	for _, slot := range slots {
		wl, ok := assignment[slot]
		if !ok {
			return ConsumptionPlan{}, false
		}
		if wl.Leg.Kind != engine.OptionKind {
			return ConsumptionPlan{}, false
		}
		if !(wl.Leg.Mult > 0) || !(wl.OpenQty > 0) {
			return ConsumptionPlan{}, false
		}
	}
	units := math.Inf(1)
	for _, slot := range slots {
		wl := assignment[slot]
		u := wl.OpenQty * wl.Leg.Mult
		if u < units {
			units = u
		}
	}
	// Snap units down to a fixed point so units / mult_i is a whole number
	// for *every* slot. A single pass is not sufficient when multipliers
	// differ: lowering units to fit slot j can leave it indivisible by an
	// earlier slot i. We loop until no further snap is needed; with units
	// strictly decreasing by at least one mult per change, the loop is
	// O(slots * max(units/min_mult)) in the worst case and trivial for the
	// uniform-mult case that dominates production.
	for {
		changed := false
		for _, slot := range slots {
			wl := assignment[slot]
			snapped := floor_eps(units/wl.Leg.Mult) * wl.Leg.Mult
			if snapped+consumptionEps < units {
				units = snapped
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	plan := ConsumptionPlan{Slots: make(map[string]ConsumedAmount, len(slots))}
	for _, slot := range slots {
		wl := assignment[slot]
		qty := units / wl.Leg.Mult
		if qty < 1-consumptionEps {
			return ConsumptionPlan{}, false
		}
		plan.Slots[slot] = ConsumedAmount{Qty: qty}
	}
	return plan, true
}

// consumptionFor dispatches by rule ID to the appropriate consumption recipe.
// In this PR only the option-only families (vertical, strangle/straddle,
// long/short box) are wired up; rules that need stock/ETF coverage return
// ok=false (no error) so the optimizer cleanly skips them until issues 6/7
// add the corresponding recipes.
func consumptionFor(ruleID string, assignment map[string]WorkingLeg, _ BucketFacts) (ConsumptionPlan, bool, error) {
	switch ruleID {
	case "vertical_spread":
		plan, ok := optionOnlyConsumption(assignment, []string{"long_leg", "short_leg"})
		return plan, ok, nil
	case "short_strangle_or_straddle":
		plan, ok := optionOnlyConsumption(assignment, []string{"sp", "sc"})
		return plan, ok, nil
	case "long_box_spread", "short_box_spread":
		plan, ok := optionOnlyConsumption(assignment, []string{"bc", "bp", "sp", "sc"})
		return plan, ok, nil
	default:
		return ConsumptionPlan{}, false, nil
	}
}

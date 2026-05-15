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
// Option-only families slice quantity by deliverable units; stock-coverage
// families consume only the coverage portion of shares (n * mult) and leave
// the residual stock in state. ETF-coverage rules remain ok=false until the
// notional-coverage recipe lands.
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
	case "covered_call":
		plan, ok := singleOptionStockConsumption(assignment, "sc", "ls")
		return plan, ok, nil
	case "protective_put":
		plan, ok := singleOptionStockConsumption(assignment, "lp", "ls")
		return plan, ok, nil
	case "long_call_short_stock":
		plan, ok := singleOptionStockConsumption(assignment, "lc", "ss")
		return plan, ok, nil
	case "short_put_short_stock":
		plan, ok := singleOptionStockConsumption(assignment, "sp", "ss")
		return plan, ok, nil
	case "collar":
		plan, ok := dualOptionStockConsumption(assignment, "lp", "sc", "ls")
		return plan, ok, nil
	case "conversion":
		plan, ok := dualOptionStockConsumption(assignment, "lp", "sc", "ls")
		return plan, ok, nil
	case "reverse_conversion":
		plan, ok := dualOptionStockConsumption(assignment, "lc", "sp", "ss")
		return plan, ok, nil
	default:
		return ConsumptionPlan{}, false, nil
	}
}

// singleOptionStockConsumption handles the 1-option + 1-stock coverage shape
// (covered_call, protective_put, long_call_short_stock, short_put_short_stock).
//
//	n = min(opt.OpenQty, floor_eps(stock.OpenShares / opt.mult))
//
// Consumes n contracts of the option and n*opt.mult shares of stock. The
// residual stock stays in state for the next branch — only the coverage
// portion is consumed. Returns ok=false when n < 1 (not enough shares for a
// single contract's worth of coverage) so the optimizer cleanly skips the
// branch.
func singleOptionStockConsumption(assignment map[string]WorkingLeg, optSlot, stockSlot string) (ConsumptionPlan, bool) {
	opt, ok := assignment[optSlot]
	if !ok || opt.Leg.Kind != engine.OptionKind || !(opt.Leg.Mult > 0) || !(opt.OpenQty > 0) {
		return ConsumptionPlan{}, false
	}
	stock, ok := assignment[stockSlot]
	if !ok || stock.Leg.Kind != engine.StockKind || !(stock.OpenShares > 0) {
		return ConsumptionPlan{}, false
	}
	maxByShares := floor_eps(stock.OpenShares / opt.Leg.Mult)
	n := math.Min(opt.OpenQty, maxByShares)
	if n < 1-consumptionEps {
		return ConsumptionPlan{}, false
	}
	return ConsumptionPlan{Slots: map[string]ConsumedAmount{
		optSlot:   {Qty: n},
		stockSlot: {Shares: n * opt.Leg.Mult},
	}}, true
}

// dualOptionStockConsumption handles the 2-option + 1-stock shape (collar,
// conversion, reverse_conversion). The coverage divisor is the larger of the
// two option mults — that's the conservative requirement that satisfies
// both legs at once. Mixed mults (e.g. mini-options) are handled here.
//
//	mult = max(optA.mult, optB.mult)
//	n    = min(optA.OpenQty, optB.OpenQty, floor_eps(stock.OpenShares / mult))
func dualOptionStockConsumption(assignment map[string]WorkingLeg, optASlot, optBSlot, stockSlot string) (ConsumptionPlan, bool) {
	optA, ok := assignment[optASlot]
	if !ok || optA.Leg.Kind != engine.OptionKind || !(optA.Leg.Mult > 0) || !(optA.OpenQty > 0) {
		return ConsumptionPlan{}, false
	}
	optB, ok := assignment[optBSlot]
	if !ok || optB.Leg.Kind != engine.OptionKind || !(optB.Leg.Mult > 0) || !(optB.OpenQty > 0) {
		return ConsumptionPlan{}, false
	}
	stock, ok := assignment[stockSlot]
	if !ok || stock.Leg.Kind != engine.StockKind || !(stock.OpenShares > 0) {
		return ConsumptionPlan{}, false
	}
	mult := math.Max(optA.Leg.Mult, optB.Leg.Mult)
	maxByShares := floor_eps(stock.OpenShares / mult)
	n := math.Min(math.Min(optA.OpenQty, optB.OpenQty), maxByShares)
	if n < 1-consumptionEps {
		return ConsumptionPlan{}, false
	}
	return ConsumptionPlan{Slots: map[string]ConsumedAmount{
		optASlot:  {Qty: n},
		optBSlot:  {Qty: n},
		stockSlot: {Shares: n * mult},
	}}, true
}

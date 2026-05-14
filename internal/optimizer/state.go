package optimizer

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// epsQty is the rounding / pruning epsilon used for both OpenQty and
// OpenShares throughout the optimizer. Kept consistent with the rounding
// policy spelled out in docs/architecture/test.md §"Rounding & quantity
// policy".
const epsQty = 1e-9

// State is the optimizer's working snapshot of remaining open inventory. In
// this PR only buildState and State.Key are used; later PRs add
// applyConsumption and memoization on top of Key.
type State struct {
	// Legs is sorted by LegID. Only entries with OpenQty > eps OR
	// OpenShares > eps are retained; sub-eps entries are pruned at
	// construction time.
	Legs []WorkingLeg
}

// buildState canonicalizes the caller's input legs into a State: validates the
// "exactly one of OpenQty/OpenShares > 0" rule, drops sub-epsilon entries, and
// sorts by LegID for determinism.
func buildState(legs []WorkingLeg) (State, error) {
	out := make([]WorkingLeg, 0, len(legs))
	for _, wl := range legs {
		// Negative open inventory is malformed input, not a "not-live" leg
		// — silently dropping it would mask broken upstream accounting and
		// understate decomposition totals. The > epsQty live-checks below
		// would otherwise let negatives fall through into the skip branch.
		if wl.OpenQty < -epsQty || wl.OpenShares < -epsQty {
			return State{}, fmt.Errorf(
				"invalid WorkingLeg %q: OpenQty/OpenShares must be >= 0 (got qty=%g shares=%g)",
				string(wl.ID), wl.OpenQty, wl.OpenShares,
			)
		}
		qtyLive := wl.OpenQty > epsQty
		sharesLive := wl.OpenShares > epsQty
		if qtyLive && sharesLive {
			return State{}, fmt.Errorf("invalid WorkingLeg %q: exactly one of OpenQty/OpenShares must be > 0 (got qty=%g shares=%g)",
				string(wl.ID), wl.OpenQty, wl.OpenShares)
		}
		if !qtyLive && !sharesLive {
			continue
		}
		out = append(out, wl)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return State{Legs: out}, nil
}

// applyConsumption returns a new State with the consumption plan's amounts
// subtracted from the matching input legs. Lookup is by LegID — never by
// engine.Leg struct identity, per §"Optimizer types". Legs whose remaining
// OpenQty AND OpenShares fall to ≤ epsQty are dropped so the memo key (and
// recursion fanout) shrinks each step. The resulting Legs slice is a fresh
// allocation and remains sorted by LegID since state.Legs was already
// LegID-sorted by buildState and we walk it in that order.
func applyConsumption(state State, assignment map[string]WorkingLeg, plan ConsumptionPlan) State {
	consumed := make(map[LegID]ConsumedAmount, len(assignment))
	for name, wl := range assignment {
		amt := plan.PerSlot[name]
		acc := consumed[wl.ID]
		acc.Qty += amt.Qty
		acc.Shares += amt.Shares
		consumed[wl.ID] = acc
	}
	out := make([]WorkingLeg, 0, len(state.Legs))
	for _, wl := range state.Legs {
		if c, ok := consumed[wl.ID]; ok {
			wl.OpenQty -= c.Qty
			wl.OpenShares -= c.Shares
		}
		if wl.OpenQty > epsQty || wl.OpenShares > epsQty {
			out = append(out, wl)
		}
	}
	return State{Legs: out}
}

// roundEps rounds x to the nearest 1e-9 to keep Key() stable across
// arithmetic with tiny floating-point drift.
func roundEps(x float64) float64 {
	return math.Round(x/epsQty) * epsQty
}

// Key returns a stable hash-suitable string identifying this State. Entries
// are pre-sorted by LegID (by buildState), so iteration here is deterministic
// without re-sorting. Format per leg: "legID:qty:shares"; entries joined by
// '|'. Quantities are rounded to epsQty so memoization keys survive
// nanoscale-noise differences between equivalent consumption paths.
func (s State) Key() string {
	parts := make([]string, len(s.Legs))
	for i, wl := range s.Legs {
		parts[i] = string(wl.ID) + ":" +
			strconv.FormatFloat(roundEps(wl.OpenQty), 'f', -1, 64) + ":" +
			strconv.FormatFloat(roundEps(wl.OpenShares), 'f', -1, 64)
	}
	return strings.Join(parts, "|")
}

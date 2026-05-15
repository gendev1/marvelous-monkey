package optimizer

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// State is the optimizer's per-decomposition input: the still-unconsumed
// working legs, sorted by LegID, with only strictly positive Open* values
// retained. State is the memo key for branch-and-bound: two States with equal
// Key() values produce equal Decompositions, so the search never repeats a
// node it has already scored.
type State struct {
	Legs []WorkingLeg
}

// newState normalizes a slice of working legs into the State form: drop any
// fully-consumed entry, sort by LegID for deterministic Key()s. It does NOT
// validate the OpenQty/OpenShares invariant — Optimize handles that up-front.
func newState(legs []WorkingLeg) State {
	out := make([]WorkingLeg, 0, len(legs))
	for _, wl := range legs {
		if wl.OpenQty <= 0 && wl.OpenShares <= 0 {
			continue
		}
		out = append(out, wl)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return State{Legs: out}
}

// stateKeyEps is the rounding granularity for OpenQty / OpenShares in Key().
// Two states whose float quantities differ below this threshold collapse to
// the same memo entry — keeps fmt %g from producing distinct keys for
// values that differ only in the last ULP after a subtract.
const stateKeyEps = 1e-9

func roundEps(v float64) float64 {
	return math.Round(v/stateKeyEps) * stateKeyEps
}

// Key returns the memoization key for s: "legID:openQty:openShares" entries
// for every leg, joined by '|'. Quantities are rounded to stateKeyEps before
// formatting so floating-point drift from repeated subtraction doesn't split
// what should be the same state across multiple memo entries.
func (s State) Key() string {
	if len(s.Legs) == 0 {
		return ""
	}
	parts := make([]string, len(s.Legs))
	for i, wl := range s.Legs {
		parts[i] = fmt.Sprintf("%s:%g:%g", wl.ID, roundEps(wl.OpenQty), roundEps(wl.OpenShares))
	}
	return strings.Join(parts, "|")
}

// applyConsumption returns the State that results from subtracting the
// assignment's planned consumption from s. Legs whose remaining Open* fall to
// (effectively) zero are dropped. The input state is not mutated.
func applyConsumption(s State, assignment map[string]WorkingLeg, plan ConsumptionPlan) State {
	deltaQty := map[LegID]float64{}
	deltaShares := map[LegID]float64{}
	for slot, wl := range assignment {
		c := plan.Slots[slot]
		deltaQty[wl.ID] += c.Qty
		deltaShares[wl.ID] += c.Shares
	}
	next := make([]WorkingLeg, 0, len(s.Legs))
	for _, wl := range s.Legs {
		rem := wl
		rem.OpenQty -= deltaQty[wl.ID]
		rem.OpenShares -= deltaShares[wl.ID]
		if rem.OpenQty < stateKeyEps {
			rem.OpenQty = 0
		}
		if rem.OpenShares < stateKeyEps {
			rem.OpenShares = 0
		}
		if rem.OpenQty <= 0 && rem.OpenShares <= 0 {
			continue
		}
		next = append(next, rem)
	}
	return State{Legs: next}
}

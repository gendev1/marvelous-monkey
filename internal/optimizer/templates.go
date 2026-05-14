package optimizer

import (
	"sort"

	"margincalc/internal/engine"
)

// buildSubPosition turns a (ruleID, assignment, plan) triple into the
// engine.Position the rulebook will score, plus the slot-name → SlotAssignment
// map the optimizer attaches to its SubPosition output.
//
// Per-leg cloning is by value so the original WorkingLeg.Leg is never
// mutated; only Qty (option contracts) is overwritten with the consumed
// amount. PR-3 only touches option slots; PR-4 (stock coverage) will write
// Shares onto stock-kind clones via the same path.
//
// Slot iteration is in slot-name alphabetical order so the resulting
// engine.Position.Legs slice is deterministic across runs (Go map iteration
// order otherwise leaks into the output). The engine matcher does not care
// about leg order — bindSlots binds by attribute predicate — but a
// deterministic position keeps EvaluateRule reproducible.
func buildSubPosition(_ string, assignment map[string]WorkingLeg, plan ConsumptionPlan, facts BucketFacts) (engine.Position, map[string]SlotAssignment) {
	slotNames := sortedSlotNames(assignment)
	legs := make([]engine.Leg, 0, len(slotNames))
	slots := make(map[string]SlotAssignment, len(slotNames))
	for _, name := range slotNames {
		wl := assignment[name]
		amt := plan.PerSlot[name]
		legClone := wl.Leg
		legClone.Qty = amt.Qty
		legs = append(legs, legClone)
		slots[name] = SlotAssignment{
			OriginalLegID:  wl.ID,
			Leg:            legClone,
			ConsumedQty:    amt.Qty,
			ConsumedShares: amt.Shares,
		}
	}
	pos := engine.Position{
		U:                       facts.U,
		Class:                   facts.Class,
		Lev:                     facts.Lev,
		UnderlyingIsEquityBased: facts.UnderlyingIsEquityBased,
		Legs:                    legs,
	}
	return pos, slots
}

// sortedSlotNames returns the slot-name keys of m in lex-ascending order.
// Centralized so buildSubPosition / decompose share one ordering convention.
func sortedSlotNames(m map[string]WorkingLeg) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// sortedSlotNamesAssign mirrors sortedSlotNames for SlotAssignment maps.
func sortedSlotNamesAssign(m map[string]SlotAssignment) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

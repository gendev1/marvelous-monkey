package optimizer

import "sort"

// recordAttribution writes one Attribution per slot of sub into out, keyed by
// SlotAssignment.OriginalLegID. Slots are walked in slot-name alphabetical
// order so the appended Attributions are deterministic across runs even
// though sub.Slots is a map. This is the only place output is built from
// SubPosition slot data — §"Optimizer types" forbids comparing engine.Leg by
// struct identity, so attribution always reads OriginalLegID.
func recordAttribution(out map[LegID][]Attribution, sub SubPosition, idx int) {
	slotNames := make([]string, 0, len(sub.Slots))
	for name := range sub.Slots {
		slotNames = append(slotNames, name)
	}
	sort.Strings(slotNames)
	for _, name := range slotNames {
		sa := sub.Slots[name]
		out[sa.OriginalLegID] = append(out[sa.OriginalLegID], Attribution{
			SubPositionIdx: idx,
			SlotName:       name,
			ConsumedQty:    sa.ConsumedQty,
			ConsumedShares: sa.ConsumedShares,
			Reason:         "residual naked option via " + sub.StrategyID,
		})
	}
}

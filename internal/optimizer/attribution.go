package optimizer

// buildAttributions inverts a list of SubPositions into a per-leg view: each
// LegID maps to the (sub-position index, slot name, quantity used) triples
// that consumed it. Returns nil for an empty input so callers can `len(...)`
// it without distinguishing "no subs" from "no attributions" — the two are
// equivalent.
//
// The slice for a given LegID preserves sub-position order; within one
// sub-position the slot order matches SubPosition.Slots. Combined with the
// deterministic Optimize iteration, this makes Attributions byte-identical
// across reruns on the same input.
func buildAttributions(subs []SubPosition) map[LegID][]Attribution {
	if len(subs) == 0 {
		return nil
	}
	out := map[LegID][]Attribution{}
	for i, sp := range subs {
		for _, sa := range sp.Slots {
			out[sa.LegID] = append(out[sa.LegID], Attribution{
				SubIndex:   i,
				Slot:       sa.Slot,
				QtyUsed:    sa.QtyUsed,
				SharesUsed: sa.SharesUsed,
			})
		}
	}
	return out
}

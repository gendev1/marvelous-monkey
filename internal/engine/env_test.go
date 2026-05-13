package engine

import (
	"testing"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// legsValFromMap builds the CEL-shaped legs value that mpl(legs) consumes.
// Each inner map's keys must match the fields legView reads (kind/side/
// option_type/K/qty/mult), or the entry is silently treated as a
// zero-valued option.
func legsValFromMap(legs map[string]map[string]any) ref.Val {
	native := map[string]any{}
	for name, leg := range legs {
		native[name] = leg
	}
	return types.DefaultTypeAdapter.NativeToValue(native)
}

// A put ratio backspread written upside-down: Long 1 K=100 put, Short 2 K=80
// puts. Net signed put quantity is -1, so the payoff slope below the lowest
// strike is positive and the worst loss occurs at U=0, NOT at any strike.
//
// Strikes-only sampling (the old behavior) sees:
//
//	U=100: long 0, shorts 0                       →     0
//	U=80:  long +20×100=+2000, shorts 0           → +2000
//
// and would return MPL=0. The true worst case at U=0 is:
//
//	long +100×100 = +10000, shorts -2×80×100 = -16000   → -6000
//
// so MPL must be 6000.
func TestMaxPotentialLoss_putRatioSamplesZero(t *testing.T) {
	legs := legsValFromMap(map[string]map[string]any{
		"lp": {
			"kind": "option", "side": "long", "option_type": "put",
			"K": 100.0, "qty": 1.0, "mult": 100.0,
		},
		"sp": {
			"kind": "option", "side": "short", "option_type": "put",
			"K": 80.0, "qty": 2.0, "mult": 100.0,
		},
	})
	got := maxPotentialLoss(legs)
	assertClose(t, "put ratio MPL must sample U=0", got, 6000.0)
}

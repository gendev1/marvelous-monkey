package engine

import (
	"strings"
	"testing"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// legsVal wraps a Go map[string]Leg as the CEL value shape that bindings
// receive. It uses the rulebook's env adapter so the inner values are
// wrapped exactly as they would be in production (per-leg nativeObj whose
// Value() returns the underlying Leg) — see the comments next to unwrapLeg.
func legsVal(t *testing.T, legs map[string]Leg) ref.Val {
	t.Helper()
	adapter := loadRB(t).env.CELTypeAdapter()
	native := make(map[string]any, len(legs))
	for name, leg := range legs {
		native[name] = leg
	}
	return adapter.NativeToValue(native)
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
	legs := legsVal(t, map[string]Leg{
		"lp": {Kind: OptionKind, Side: Long, OptionType: "put", K: 100, Qty: 1, Mult: 100},
		"sp": {Kind: OptionKind, Side: Short, OptionType: "put", K: 80, Qty: 2, Mult: 100},
	})
	got := maxPotentialLoss(legs)
	d, ok := got.(types.Double)
	if !ok {
		t.Fatalf("maxPotentialLoss returned %T (%v), want types.Double", got, got)
	}
	assertClose(t, "put ratio MPL must sample U=0", float64(d), 6000.0)
}

// unwrapLeg round-trips every field that any binding reads off a Leg.
func TestUnwrapLeg_Success(t *testing.T) {
	in := Leg{
		Side: Short, Kind: OptionKind, OptionType: "call",
		K: 100, P: 2.5, P0: 2.0, Qty: 3, Mult: 100,
		Style: "american", Venue: "listed",
		KEquivalent: 110.0, Shares: 0, Price: 0,
	}
	adapter := loadRB(t).env.CELTypeAdapter()
	v := adapter.NativeToValue(in)
	got, errVal := unwrapLeg(v)
	if errVal != nil {
		t.Fatalf("unwrapLeg: %v", errVal.Value())
	}
	if got != in {
		t.Fatalf("unwrapLeg round-trip mismatch:\n  got  %+v\n  want %+v", got, in)
	}
}

// unwrapLeg accepts a *Leg too — cel-go may hand back either shape depending
// on how the activation supplied the value. Both paths must succeed.
func TestUnwrapLeg_AcceptsPointer(t *testing.T) {
	in := &Leg{Side: Long, Kind: OptionKind, OptionType: "put", K: 80, Qty: 1, Mult: 100}
	adapter := loadRB(t).env.CELTypeAdapter()
	v := adapter.NativeToValue(in)
	got, errVal := unwrapLeg(v)
	if errVal != nil {
		t.Fatalf("unwrapLeg: %v", errVal.Value())
	}
	if got != *in {
		t.Fatalf("unwrapLeg pointer round-trip mismatch:\n  got  %+v\n  want %+v", got, *in)
	}
}

// A non-Leg ref.Val must produce a CEL error with the documented substring —
// not a zero-valued Leg. Mirrors rate() error semantics per CLAUDE.md
// "Required Rules" (no silent zero-fallback).
func TestUnwrapLeg_NonLegErrors(t *testing.T) {
	_, errVal := unwrapLeg(types.String("nope"))
	if errVal == nil {
		t.Fatal("unwrapLeg(types.String) returned no error, want CEL error")
	}
	msg, _ := errVal.Value().(error)
	if msg == nil || !strings.Contains(msg.Error(), "leg unwrap failed") {
		t.Fatalf("unwrapLeg error %q does not contain %q", errVal.Value(), "leg unwrap failed")
	}
}

// Feeding maxPotentialLoss a non-map value must surface the unwrap error,
// not return 0. This is the eval-time defense against a future regression
// that wires a binding to the wrong type.
func TestMaxPotentialLoss_unwrapErrorPropagates(t *testing.T) {
	got := maxPotentialLoss(types.String("not a map"))
	if _, ok := got.(types.Double); ok {
		t.Fatalf("got %v (a Double), want CEL error", got)
	}
	err, _ := got.Value().(error)
	if err == nil || !strings.Contains(err.Error(), "legs unwrap failed") {
		t.Fatalf("maxPotentialLoss error %v missing substring %q", got.Value(), "legs unwrap failed")
	}
}

// Same defense, but the failure point is the per-leg unwrap inside the
// iterator rather than the outer map. We hand a real map whose value is a
// types.String — the inner unwrapLeg must catch it.
func TestMaxPotentialLoss_innerUnwrapErrorPropagates(t *testing.T) {
	adapter := loadRB(t).env.CELTypeAdapter()
	v := adapter.NativeToValue(map[string]any{"x": "not a leg"})
	got := maxPotentialLoss(v)
	if _, ok := got.(types.Double); ok {
		t.Fatalf("got %v (a Double), want CEL error", got)
	}
	err, _ := got.Value().(error)
	if err == nil || !strings.Contains(err.Error(), "leg unwrap failed") {
		t.Fatalf("inner unwrap error %v missing substring %q", got.Value(), "leg unwrap failed")
	}
}

// Empty legs map: forEachLeg invokes the callback zero times and reports no
// error; downstream accumulators return their zero element.
func TestForEachLeg_emptyMapIsNoop(t *testing.T) {
	legs := legsVal(t, map[string]Leg{})
	calls := 0
	if err := forEachLeg(legs, func(Leg) bool { calls++; return true }); err != nil {
		t.Fatalf("forEachLeg on empty map: %v", err.Value())
	}
	if calls != 0 {
		t.Fatalf("forEachLeg invoked callback %d times on empty map, want 0", calls)
	}

	if got := maxPotentialLoss(legs); got != types.Double(0) {
		t.Fatalf("maxPotentialLoss on empty map: got %v, want 0", got)
	}
	if got := sumPremiums(legs, "P", Long); got != types.Double(0) {
		t.Fatalf("sumPremiums(P, long) on empty map: got %v, want 0", got)
	}
	if got := isLimitedRisk(legs); got != types.Bool(true) {
		// Vacuously: no non-option legs, zero net call exposure ≥ 0.
		t.Fatalf("isLimitedRisk on empty map: got %v, want true", got)
	}
}

// forEachLeg walks every leg in the map exactly once, in some order, with
// the typed Leg shape — strikes 50/60/70 must all be observed.
func TestForEachLeg_visitsEveryTypedLeg(t *testing.T) {
	legs := legsVal(t, map[string]Leg{
		"a": {Kind: OptionKind, Side: Long, OptionType: "call", K: 50, Qty: 1, Mult: 100},
		"b": {Kind: OptionKind, Side: Short, OptionType: "call", K: 60, Qty: 1, Mult: 100},
		"c": {Kind: OptionKind, Side: Short, OptionType: "call", K: 70, Qty: 1, Mult: 100},
	})
	seen := map[float64]int{}
	if err := forEachLeg(legs, func(l Leg) bool {
		seen[l.K]++
		return true
	}); err != nil {
		t.Fatalf("forEachLeg: %v", err.Value())
	}
	for _, K := range []float64{50, 60, 70} {
		if seen[K] != 1 {
			t.Errorf("strike %g visited %d times, want 1", K, seen[K])
		}
	}
}

// An unknown premium field name is a YAML/CEL typo, not a zero. sumPremiums
// must surface it as a CEL error — same fail-loud contract as rate().
func TestSumPremiums_unknownFieldErrors(t *testing.T) {
	legs := legsVal(t, map[string]Leg{
		"lp": {Kind: OptionKind, Side: Long, OptionType: "put", K: 100, P: 4.0, Qty: 1, Mult: 100},
	})
	got := sumPremiums(legs, "Pnope", Long)
	if _, ok := got.(types.Double); ok {
		t.Fatalf("got %v (a Double), want CEL error", got)
	}
	err, _ := got.Value().(error)
	if err == nil || !strings.Contains(err.Error(), "unknown premium field") {
		t.Fatalf("sumPremiums error %v missing substring %q", got.Value(), "unknown premium field")
	}
}

// sumPremiums sums on the typed-leg path for both Long and Short and reads
// the named field correctly.
func TestSumPremiums_typedLeg(t *testing.T) {
	legs := legsVal(t, map[string]Leg{
		"lp": {Kind: OptionKind, Side: Long, OptionType: "put", K: 100, P: 4.0, P0: 3.5, Qty: 1, Mult: 100},
		"sc": {Kind: OptionKind, Side: Short, OptionType: "call", K: 110, P: 2.0, P0: 1.5, Qty: 2, Mult: 100},
		// non-option legs must be ignored by sumPremiums
		"ss": {Kind: StockKind, Side: Short, Shares: 100},
	})
	if got := sumPremiums(legs, "P", Long); got != types.Double(400.0) {
		t.Errorf("long P sum: got %v, want 400", got)
	}
	if got := sumPremiums(legs, "P0", Short); got != types.Double(300.0) {
		t.Errorf("short P0 sum: got %v, want 300", got)
	}
}

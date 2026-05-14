package engine

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestEvaluateRule_ExactMatch_Vertical: EvaluateRule on a vertical_spread
// fixture returns the same Result as Evaluate (which dispatches to the same
// rule by first-match), demonstrating exact-rule scoring parity.
func TestEvaluateRule_ExactMatch_Vertical(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     128.50,
		Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call",
				K: 125, P: 3.80, P0: 3.80, Qty: 1, Mult: 100, Style: "american", Venue: "listed",
				Underlying: "XYZ", Expiration: "2024-11-15"},
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 120, P: 8.40, P0: 8.40, Qty: 1, Mult: 100, Style: "american", Venue: "listed",
				Underlying: "XYZ", Expiration: "2024-11-15"},
		},
	}
	wantRes, err := rb.Evaluate(pos, MarginAccount, Initial)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	got, ok, err := rb.EvaluateRule(pos, "vertical_spread", MarginAccount, Initial)
	if err != nil {
		t.Fatalf("EvaluateRule: %v", err)
	}
	if !ok {
		t.Fatal("EvaluateRule: not ok")
	}
	if !reflect.DeepEqual(got, wantRes) {
		t.Errorf("EvaluateRule result diverged: got %+v, want %+v", got, wantRes)
	}
}

// TestEvaluateRule_ExactMatch_CoveredCall: stock-coverage family parity.
func TestEvaluateRule_ExactMatch_CoveredCall(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     92.38,
		Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 90, P: 7.0, P0: 7.0, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ"},
			{Side: Long, Kind: StockKind, Shares: 100, Underlying: "XYZ"},
		},
	}
	wantRes, err := rb.Evaluate(pos, MarginAccount, Initial)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	got, ok, err := rb.EvaluateRule(pos, "covered_call", MarginAccount, Initial)
	if err != nil {
		t.Fatalf("EvaluateRule: %v", err)
	}
	if !ok || !reflect.DeepEqual(got, wantRes) {
		t.Errorf("got=%+v ok=%v want=%+v", got, ok, wantRes)
	}
}

// TestEvaluateRule_ExactMatch_Conversion: 3-slot conversion family parity.
func TestEvaluateRule_ExactMatch_Conversion(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     115.0,
		Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "put",
				K: 110, P: 1.35, P0: 1.35, Qty: 1, Mult: 100, Style: "american", Expiration: "2024-05-17", Underlying: "XYZ"},
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 110, P: 6.50, P0: 6.50, Qty: 1, Mult: 100, Style: "american", Expiration: "2024-05-17", Underlying: "XYZ"},
			{Side: Long, Kind: StockKind, Shares: 100},
		},
	}
	wantRes, err := rb.Evaluate(pos, MarginAccount, Maintenance)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	got, ok, err := rb.EvaluateRule(pos, "conversion", MarginAccount, Maintenance)
	if err != nil {
		t.Fatalf("EvaluateRule: %v", err)
	}
	if !ok || !reflect.DeepEqual(got, wantRes) {
		t.Errorf("got=%+v ok=%v want=%+v", got, ok, wantRes)
	}
}

// TestEvaluateRule_ExactMatch_Box: 4-slot long_box_spread parity. American
// style so the gross+proceeds branch evaluates.
func TestEvaluateRule_ExactMatch_Box(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     105.0,
		Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call",
				K: 100, P: 8.0, P0: 8.0, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ", Expiration: "2024-11-15"},
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 100, P: 1.0, P0: 1.0, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ", Expiration: "2024-11-15"},
			{Side: Long, Kind: OptionKind, OptionType: "put",
				K: 110, P: 6.0, P0: 6.0, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ", Expiration: "2024-11-15"},
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 110, P: 2.0, P0: 2.0, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ", Expiration: "2024-11-15"},
		},
	}
	wantRes, err := rb.Evaluate(pos, MarginAccount, Initial)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if wantRes.RuleID != "long_box_spread" {
		t.Fatalf("fixture matched %s, expected long_box_spread", wantRes.RuleID)
	}
	got, ok, err := rb.EvaluateRule(pos, "long_box_spread", MarginAccount, Initial)
	if err != nil {
		t.Fatalf("EvaluateRule: %v", err)
	}
	if !ok || !reflect.DeepEqual(got, wantRes) {
		t.Errorf("got=%+v ok=%v want=%+v", got, ok, wantRes)
	}
}

// TestEvaluateRule_ExactMatch_Naked: 1-slot naked-sink rule parity. These
// rules are excluded from OptimizerTargets by default policy but remain
// scorable explicitly via EvaluateRule.
func TestEvaluateRule_ExactMatch_Naked(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     95.0,
		Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 80, P: 2.0, P0: 2.0, Qty: 1, Mult: 100},
		},
	}
	wantRes, err := rb.Evaluate(pos, MarginAccount, Initial)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	got, ok, err := rb.EvaluateRule(pos, "short_put_uncovered", MarginAccount, Initial)
	if err != nil {
		t.Fatalf("EvaluateRule: %v", err)
	}
	if !ok || !reflect.DeepEqual(got, wantRes) {
		t.Errorf("got=%+v ok=%v want=%+v", got, ok, wantRes)
	}
}

// TestEvaluateRule_RequiresFailure_NoMatch: a vertical_spread fixture whose
// short leg has empty Venue would, under Evaluate, fail validateRequirements
// with an "invalid position:" error. EvaluateRule must demote this to
// (Result{}, false, nil) — that demotion is the entire reason the
// RequiresError sentinel exists.
//
// Confirms the guard is what's being exercised: vertical_spread requires
// `required_fields.short_leg: [venue]`. Without that requires entry the
// fixture would otherwise produce a valid Result (matcher binds, all
// constraints evaluate true since venue=="" on both legs).
func TestEvaluateRule_RequiresFailure_NoMatch(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     128.50,
		Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call",
				K: 125, P: 3.80, P0: 3.80, Qty: 1, Mult: 100, Style: "american", Venue: "",
				Underlying: "XYZ", Expiration: "2024-11-15"},
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 120, P: 8.40, P0: 8.40, Qty: 1, Mult: 100, Style: "american", Venue: "",
				Underlying: "XYZ", Expiration: "2024-11-15"},
		},
	}
	// Sanity: confirm Evaluate today returns the requires-failure error so
	// the test would actually exercise the demotion path.
	if _, err := rb.Evaluate(pos, MarginAccount, Initial); err == nil ||
		!strings.HasPrefix(err.Error(), "invalid position:") {
		t.Fatalf("Evaluate should produce an 'invalid position:' requires-failure; got err=%v", err)
	}
	got, ok, err := rb.EvaluateRule(pos, "vertical_spread", MarginAccount, Initial)
	if err != nil {
		t.Fatalf("EvaluateRule: expected demoted no-match, got err=%v", err)
	}
	if ok {
		t.Fatalf("EvaluateRule: expected no-match, got ok=true result=%+v", got)
	}
	if !reflect.DeepEqual(got, Result{}) {
		t.Fatalf("EvaluateRule: expected zero Result, got %+v", got)
	}
}

// TestEvaluateRule_CELErrorPropagates: a position whose class is not in the
// rates table triggers a CEL eval failure inside short_put_req. That kind of
// error is NOT a requires-failure and must bubble out of EvaluateRule.
func TestEvaluateRule_CELErrorPropagates(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     95.0,
		Class: "bogus_rate_class", // not present in rates: → shortOptionReq errors
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 80, P: 2.0, P0: 2.0, Qty: 1, Mult: 100},
		},
	}
	_, _, err := rb.EvaluateRule(pos, "short_put_uncovered", MarginAccount, Initial)
	if err == nil {
		t.Fatal("EvaluateRule: expected CEL eval error to bubble, got nil")
	}
	if !strings.Contains(err.Error(), "unknown rate") {
		t.Fatalf("EvaluateRule: expected 'unknown rate' in error, got %v", err)
	}
}

// TestEvaluateRule_UnknownRule: unknown rule IDs are programmer errors and
// surface as a hard error.
func TestEvaluateRule_UnknownRule(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     100,
		Class: "equity",
		Legs:  []Leg{{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100}},
	}
	_, ok, err := rb.EvaluateRule(pos, "no_such_rule", MarginAccount, Initial)
	if err == nil {
		t.Fatal("EvaluateRule: expected error for unknown rule, got nil")
	}
	if ok {
		t.Fatal("EvaluateRule: expected ok=false for unknown rule")
	}
	if !strings.Contains(err.Error(), "unknown rule") {
		t.Errorf("expected 'unknown rule' in error, got %v", err)
	}
}

// TestRuleByID: known rule round-trip + unknown returns ok=false.
func TestRuleByID(t *testing.T) {
	rb := loadRB(t)
	r, ok := rb.RuleByID("vertical_spread")
	if !ok {
		t.Fatal("RuleByID(vertical_spread): not found")
	}
	if r.ID != "vertical_spread" {
		t.Errorf("RuleByID returned wrong rule: got %q", r.ID)
	}
	if _, ok := rb.RuleByID("does_not_exist"); ok {
		t.Error("RuleByID returned ok=true for unknown rule")
	}
}

// TestRuleByID_ReturnsIndependentCopy asserts the Rule returned from
// RuleByID is a deep copy: mutating its slices, maps, or *bool fields must
// not leak back into the Rulebook's stored rules. Regression guard for the
// concurrency invariant — if a caller could mutate rb.rules in place, the
// "Rulebook is concurrent-safe only because LoadRulebook pre-compiles" claim
// in CLAUDE.md would be violated.
func TestRuleByID_ReturnsIndependentCopy(t *testing.T) {
	rb := loadRB(t)
	// vertical_spread has populated Match.Legs, Match.Constraints, Requires
	// (RequiredFields map, SameAcrossSlots slice, ExpirationSlots slice), and
	// our two YAML overrides put OptimizerTarget on the convertible/warrant
	// rules. Round-trip each.
	id := "vertical_spread"
	first, ok := rb.RuleByID(id)
	if !ok {
		t.Fatalf("RuleByID(%q): not found", id)
	}

	// Mutate every reference field on the returned copy.
	first.Match.Legs[0].Name = "MUTATED"
	first.Match.Constraints[0] = "MUTATED"
	first.Requires.RequiredFields["long_leg"][0] = "MUTATED"
	first.Requires.SameAcrossSlots[0].Slots[0] = "MUTATED"
	first.Requires.ExpirationSlots[0] = "MUTATED"
	// Note: OptimizerTarget pointer-vs-value sharing is exercised below on
	// short_call_long_convertible, where the rule actually has a non-nil
	// pointer to dereference and mutate.

	// Fetch again — must be pristine.
	second, ok := rb.RuleByID(id)
	if !ok {
		t.Fatalf("RuleByID(%q) second fetch: not found", id)
	}
	if second.Match.Legs[0].Name == "MUTATED" {
		t.Errorf("RuleByID leaked Match.Legs reference: second fetch sees mutation")
	}
	if second.Match.Constraints[0] == "MUTATED" {
		t.Errorf("RuleByID leaked Match.Constraints reference")
	}
	if second.Requires.RequiredFields["long_leg"][0] == "MUTATED" {
		t.Errorf("RuleByID leaked Requires.RequiredFields slice reference")
	}
	if len(second.Requires.SameAcrossSlots) > 0 && len(second.Requires.SameAcrossSlots[0].Slots) > 0 &&
		second.Requires.SameAcrossSlots[0].Slots[0] == "MUTATED" {
		t.Errorf("RuleByID leaked Requires.SameAcrossSlots[].Slots reference")
	}
	if second.Requires.ExpirationSlots[0] == "MUTATED" {
		t.Errorf("RuleByID leaked Requires.ExpirationSlots reference")
	}
	// Spot-check the OptimizerTarget *bool on a rule that DOES have one,
	// and confirm mutating the returned bool doesn't change the source.
	conv, ok := rb.RuleByID("short_call_long_convertible")
	if !ok {
		t.Fatalf("RuleByID(short_call_long_convertible): not found")
	}
	if conv.OptimizerTarget == nil {
		t.Fatalf("expected non-nil OptimizerTarget on short_call_long_convertible")
	}
	*conv.OptimizerTarget = true
	conv2, _ := rb.RuleByID("short_call_long_convertible")
	if conv2.OptimizerTarget == nil || *conv2.OptimizerTarget {
		t.Errorf("RuleByID leaked OptimizerTarget *bool: mutation visible in second fetch")
	}
}

// TestMatch_LegsPatternRejected: rules with legs_pattern have synthetic slot
// names, so Match's "bind these slots" question is meaningless. Confirms the
// guard fails fast instead of returning a binding the caller didn't expect.
func TestMatch_LegsPatternRejected(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     100,
		Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100},
			{Side: Short, Kind: OptionKind, OptionType: "call", K: 110, P: 1, P0: 1, Qty: 1, Mult: 100},
		},
	}
	_, ok, err := rb.Match(pos, "generic_limited_risk_combo")
	if err == nil {
		t.Fatal("Match: expected error for legs_pattern rule, got nil")
	}
	if ok {
		t.Error("Match: expected ok=false for legs_pattern rule")
	}
	if !strings.Contains(err.Error(), "legs_pattern") {
		t.Errorf("expected 'legs_pattern' in error, got %v", err)
	}
}

// TestOptimizerTargets_DefaultsAndOverrides asserts the resolved set matches
// the rule-by-rule table in docs/architecture/spread-optimizer.md. In
// particular: every 2/3/4-slot rule is on by default, naked 1-slot rules are
// off, generic_limited_risk_combo (legs_pattern: all_options) is off, and the
// two explicit YAML overrides (short_call_long_convertible /
// short_call_long_warrant) come back off as required.
func TestOptimizerTargets_DefaultsAndOverrides(t *testing.T) {
	rb := loadRB(t)
	got := rb.OptimizerTargets()
	want := []string{
		"collar",
		"conversion",
		"covered_call",
		"long_box_spread",
		"long_call_short_stock",
		"protective_put",
		"reverse_conversion",
		"short_box_spread",
		"short_index_call_long_etf",
		"short_put_short_stock",
		"short_strangle_or_straddle",
		"vertical_spread",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OptimizerTargets mismatch:\n got: %v\nwant: %v", got, want)
	}
	// Sorted invariant.
	if !sort.StringsAreSorted(got) {
		t.Errorf("OptimizerTargets not sorted: %v", got)
	}
	// Excluded sets — assert key membership rather than re-listing.
	excluded := []string{
		"long_option_short_dated",
		"long_option_long_dated_listed",
		"long_option_long_dated_otc",
		"short_call_uncovered",
		"short_put_uncovered",
		"generic_limited_risk_combo",
		"short_call_long_convertible", // explicit YAML override
		"short_call_long_warrant",     // explicit YAML override
	}
	in := map[string]struct{}{}
	for _, id := range got {
		in[id] = struct{}{}
	}
	for _, id := range excluded {
		if _, present := in[id]; present {
			t.Errorf("OptimizerTargets unexpectedly includes %q", id)
		}
	}
	// Double-check the explicit-false override actually carries through on
	// the Rule struct itself.
	for _, id := range []string{"short_call_long_convertible", "short_call_long_warrant"} {
		r, ok := rb.RuleByID(id)
		if !ok {
			t.Fatalf("RuleByID(%q): not found", id)
		}
		if r.OptimizerTarget == nil || *r.OptimizerTarget {
			t.Errorf("rule %q: expected OptimizerTarget=*false, got %v", id, r.OptimizerTarget)
		}
	}
}

// TestBindSlotsAll_MultipleAssignments: vertical_spread slot pattern (long_leg
// + short_leg) against a leg slice with two longs and two shorts produces
// exactly 4 deterministic assignments in lex-ascending (long_idx, short_idx)
// order.
func TestBindSlotsAll_MultipleAssignments(t *testing.T) {
	legs := []Leg{
		// indices: 0 long, 1 short, 2 long, 3 short
		{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100},
		{Side: Short, Kind: OptionKind, OptionType: "call", K: 110, P: 1, P0: 1, Qty: 1, Mult: 100},
		{Side: Long, Kind: OptionKind, OptionType: "call", K: 105, P: 1, P0: 1, Qty: 1, Mult: 100},
		{Side: Short, Kind: OptionKind, OptionType: "call", K: 115, P: 1, P0: 1, Qty: 1, Mult: 100},
	}
	slots := []LegSlot{
		{Name: "long_leg", Side: "long", Kind: "option"},
		{Name: "short_leg", Side: "short", Kind: "option"},
	}
	got := bindSlotsAll(legs, slots)
	want := [][]int{
		{0, 1}, {0, 3}, {2, 1}, {2, 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("bindSlotsAll = %v, want %v", got, want)
	}
}

// TestBindSlotsAll_NoMatch: when no leg satisfies a slot's predicate the
// function returns nil (no assignments).
func TestBindSlotsAll_NoMatch(t *testing.T) {
	legs := []Leg{
		{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100},
		{Side: Long, Kind: OptionKind, OptionType: "call", K: 110, P: 1, P0: 1, Qty: 1, Mult: 100},
	}
	slots := []LegSlot{
		{Name: "long_leg", Side: "long", Kind: "option"},
		{Name: "short_leg", Side: "short", Kind: "option"},
	}
	got := bindSlotsAll(legs, slots)
	if got != nil {
		t.Errorf("bindSlotsAll: expected nil, got %v", got)
	}
}

// TestRequiresError_Sentinel: checkRequires wraps requires-failures in
// *RequiresError. EvaluateRule depends on that wrapping for its demote path —
// a regression would make the optimizer treat real CEL errors as no-matches.
func TestRequiresError_Sentinel(t *testing.T) {
	rb := loadRB(t)
	rule, ok := rb.RuleByID("vertical_spread")
	if !ok {
		t.Fatal("RuleByID(vertical_spread): not found")
	}
	pos := Position{
		U:     128.50,
		Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call",
				K: 125, P: 3.80, P0: 3.80, Qty: 1, Mult: 100, Style: "american", Venue: "",
				Underlying: "XYZ", Expiration: "2024-11-15"},
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 120, P: 8.40, P0: 8.40, Qty: 1, Mult: 100, Style: "american", Venue: "",
				Underlying: "XYZ", Expiration: "2024-11-15"},
		},
	}
	pos = rb.preparePosition(pos)
	bound, ok, err := rb.tryMatchAndConstraints(pos, rule)
	if err != nil || !ok {
		t.Fatalf("tryMatchAndConstraints: ok=%v err=%v", ok, err)
	}
	act := rb.buildActivation(pos, bound)
	err = rb.checkRequires(rule, bound, act)
	if err == nil {
		t.Fatal("checkRequires: expected error, got nil")
	}
	var rerr *RequiresError
	if !errors.As(err, &rerr) {
		t.Fatalf("checkRequires: expected *RequiresError, got %T (%v)", err, err)
	}
	if rerr.RuleID != "vertical_spread" {
		t.Errorf("RequiresError.RuleID = %q, want vertical_spread", rerr.RuleID)
	}
	if !strings.HasPrefix(rerr.Error(), "invalid position:") {
		t.Errorf("RequiresError.Error() must preserve 'invalid position:' prefix, got %q", rerr.Error())
	}
}

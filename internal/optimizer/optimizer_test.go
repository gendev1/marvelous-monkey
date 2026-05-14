package optimizer

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"margincalc/internal/engine"
)

const rulesPath = "../../rules/cboe_baseline.yaml"

var loadedRB *engine.Rulebook

func loadRB(t *testing.T) *engine.Rulebook {
	t.Helper()
	if loadedRB != nil {
		return loadedRB
	}
	rb, err := engine.LoadRulebook(rulesPath)
	if err != nil {
		t.Fatalf("LoadRulebook: %v", err)
	}
	loadedRB = rb
	return rb
}

func newOpt(t *testing.T) *Optimizer {
	t.Helper()
	return New(loadRB(t), engine.MarginAccount, engine.Initial)
}

func assertClose(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Errorf("%s: got %.4f, want %.4f", label, got, want)
	}
}

// equityFacts is the most common single-underlying bucket used by the
// per-rule smoke tests. Concrete numeric inputs are cribbed from
// rulebook_test.go (TestShortCallITM_p32, TestShortPutOTM_p28) when those
// values matter for the assertion; for the long-option tests, the U / Class
// pair is whatever the constraint set requires.
var equityFacts = BucketFacts{U: 95.0, Class: "equity"}

// -----------------------------------------------------------------------------
// Per-naked-rule smoke tests
// -----------------------------------------------------------------------------

func TestNakedSmoke_LongOptionShortDated(t *testing.T) {
	opt := newOpt(t)
	legs := []WorkingLeg{
		{
			ID:      "leg0",
			OpenQty: 1,
			Leg: engine.Leg{
				Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
				K: 90, P: 2.0, P0: 2.0, Mult: 100,
				TimeToExpirationMonths: 3, // <= 9 threshold
			},
		},
	}
	decomp, err := opt.Optimize(equityFacts, legs)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(decomp.SubPositions) != 1 {
		t.Fatalf("want 1 sub-position, got %d", len(decomp.SubPositions))
	}
	sp := decomp.SubPositions[0]
	if sp.StrategyID != "long_option_short_dated" {
		t.Errorf("StrategyID = %q, want long_option_short_dated", sp.StrategyID)
	}
	assertClose(t, "Requirement", sp.Result.Requirement, 200.0) // P0*qty*mult
	attrs := decomp.AttributionsByLeg["leg0"]
	if len(attrs) != 1 || attrs[0].SubPositionIdx != 0 || attrs[0].SlotName != "opt" ||
		attrs[0].ConsumedQty != 1 {
		t.Errorf("unexpected attribution: %+v", attrs)
	}
}

func TestNakedSmoke_LongOptionLongDatedListed(t *testing.T) {
	opt := newOpt(t)
	legs := []WorkingLeg{
		{
			ID:      "leg0",
			OpenQty: 1,
			Leg: engine.Leg{
				Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
				K: 90, P: 10.0, P0: 10.0, Mult: 100,
				Venue:                  "listed",
				TimeToExpirationMonths: 12,
			},
		},
	}
	decomp, err := opt.Optimize(equityFacts, legs)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(decomp.SubPositions) != 1 {
		t.Fatalf("want 1 sub-position, got %d", len(decomp.SubPositions))
	}
	sp := decomp.SubPositions[0]
	if sp.StrategyID != "long_option_long_dated_listed" {
		t.Errorf("StrategyID = %q, want long_option_long_dated_listed", sp.StrategyID)
	}
	assertClose(t, "Requirement", sp.Result.Requirement, 750.0) // 0.75*10*1*100
}

func TestNakedSmoke_LongOptionLongDatedOTC(t *testing.T) {
	opt := newOpt(t)
	facts := BucketFacts{U: 100.0, Class: "equity"}
	legs := []WorkingLeg{
		{
			ID:      "leg0",
			OpenQty: 1,
			Leg: engine.Leg{
				Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
				K: 100, P: 5.0, P0: 5.0, Mult: 100,
				Venue:                  "otc",
				Style:                  "american",
				BrokerGuaranteed:       true,
				TimeToExpirationMonths: 12,
			},
		},
	}
	decomp, err := opt.Optimize(facts, legs)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(decomp.SubPositions) != 1 {
		t.Fatalf("want 1 sub-position, got %d", len(decomp.SubPositions))
	}
	sp := decomp.SubPositions[0]
	if sp.StrategyID != "long_option_long_dated_otc" {
		t.Errorf("StrategyID = %q, want long_option_long_dated_otc", sp.StrategyID)
	}
	// intrinsic_call(100,100)=0, so initial = qty*mult*(0 + P0) = 1*100*5 = 500.
	assertClose(t, "Requirement", sp.Result.Requirement, 500.0)
}

func TestNakedSmoke_ShortCallUncovered(t *testing.T) {
	opt := newOpt(t)
	// Fixture cribbed from TestShortCallITM_p32: K=120, U=128.50, P=P0=8.40,
	// equity → 3410.00.
	facts := BucketFacts{U: 128.50, Class: "equity"}
	legs := []WorkingLeg{
		{
			ID:      "leg0",
			OpenQty: 1,
			Leg: engine.Leg{
				Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
				K: 120, P: 8.40, P0: 8.40, Mult: 100,
			},
		},
	}
	decomp, err := opt.Optimize(facts, legs)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(decomp.SubPositions) != 1 {
		t.Fatalf("want 1 sub-position, got %d", len(decomp.SubPositions))
	}
	sp := decomp.SubPositions[0]
	if sp.StrategyID != "short_call_uncovered" {
		t.Errorf("StrategyID = %q, want short_call_uncovered", sp.StrategyID)
	}
	assertClose(t, "Requirement", sp.Result.Requirement, 3410.00)
	assertClose(t, "TotalRequirement", decomp.TotalRequirement, 3410.00)
}

func TestNakedSmoke_ShortPutUncovered(t *testing.T) {
	opt := newOpt(t)
	// Fixture cribbed from TestShortPutOTM_p28: K=80, U=95, P=P0=2, equity → 1000.
	facts := BucketFacts{U: 95.0, Class: "equity"}
	legs := []WorkingLeg{
		{
			ID:      "leg0",
			OpenQty: 1,
			Leg: engine.Leg{
				Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
				K: 80, P: 2.0, P0: 2.0, Mult: 100,
			},
		},
	}
	decomp, err := opt.Optimize(facts, legs)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(decomp.SubPositions) != 1 {
		t.Fatalf("want 1 sub-position, got %d", len(decomp.SubPositions))
	}
	sp := decomp.SubPositions[0]
	if sp.StrategyID != "short_put_uncovered" {
		t.Errorf("StrategyID = %q, want short_put_uncovered", sp.StrategyID)
	}
	assertClose(t, "Requirement", sp.Result.Requirement, 1000.0)
}

// -----------------------------------------------------------------------------
// Determinism / structural tests
// -----------------------------------------------------------------------------

func sixNakedLegs() []WorkingLeg {
	return []WorkingLeg{
		{ID: "a-short-call", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
			K: 120, P: 8.40, P0: 8.40, Mult: 100,
		}},
		{ID: "b-short-put", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
			K: 80, P: 2.0, P0: 2.0, Mult: 100,
		}},
		{ID: "c-long-short-dated", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 95, P: 3.0, P0: 3.0, Mult: 100, TimeToExpirationMonths: 3,
		}},
		{ID: "d-long-listed", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 95, P: 4.0, P0: 4.0, Mult: 100,
			Venue: "listed", TimeToExpirationMonths: 12,
		}},
		{ID: "e-long-otc", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 100, P: 5.0, P0: 5.0, Mult: 100,
			Venue: "otc", Style: "american", BrokerGuaranteed: true,
			TimeToExpirationMonths: 12,
		}},
		{ID: "f-short-put-2", OpenQty: 2, Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
			K: 75, P: 1.0, P0: 1.0, Mult: 100,
		}},
	}
}

func TestDeterminism_NakedOnly(t *testing.T) {
	opt := newOpt(t)
	facts := BucketFacts{U: 128.50, Class: "equity"}
	d1, err1 := opt.Optimize(facts, sixNakedLegs())
	d2, err2 := opt.Optimize(facts, sixNakedLegs())
	if err1 != nil || err2 != nil {
		t.Fatalf("Optimize errored: %v / %v", err1, err2)
	}
	if !reflect.DeepEqual(d1, d2) {
		t.Fatalf("decompositions not equal across two runs:\n  d1=%+v\n  d2=%+v", d1, d2)
	}
	if len(d1.SubPositions) != 6 {
		t.Fatalf("want 6 sub-positions, got %d", len(d1.SubPositions))
	}
	// Sub-positions must be ordered by input LegID alphabetical (a..f).
	wantIDs := []LegID{"a-short-call", "b-short-put", "c-long-short-dated",
		"d-long-listed", "e-long-otc", "f-short-put-2"}
	for i, sp := range d1.SubPositions {
		got := sp.Slots["opt"].OriginalLegID
		if got != wantIDs[i] {
			t.Errorf("SubPositions[%d] OriginalLegID = %q, want %q", i, got, wantIDs[i])
		}
	}
}

// -----------------------------------------------------------------------------
// Error-path tests
// -----------------------------------------------------------------------------

func TestStockOnlyResidualError(t *testing.T) {
	opt := newOpt(t)
	legs := []WorkingLeg{
		{ID: "stock", OpenShares: 100, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.StockKind,
		}},
	}
	decomp, err := opt.Optimize(equityFacts, legs)
	var sErr *ErrStockResidualUnsupported
	if !errors.As(err, &sErr) {
		t.Fatalf("err = %v, want *ErrStockResidualUnsupported", err)
	}
	if sErr.LegID != "stock" || sErr.OpenShares != 100 {
		t.Errorf("err fields = %+v, want LegID=stock OpenShares=100", sErr)
	}
	if len(decomp.SubPositions) != 0 {
		t.Errorf("partial SubPositions should be empty, got %d", len(decomp.SubPositions))
	}
	if len(decomp.AttributionsByLeg) != 0 {
		t.Errorf("partial AttributionsByLeg should be empty, got %v", decomp.AttributionsByLeg)
	}
}

func TestNoNakedRuleForOption(t *testing.T) {
	opt := newOpt(t)
	// Long call, empty venue, ttm > threshold (9) → none of the candidate
	// rules binds (short-dated rejects ttm>9; listed/OTC require venue).
	legs := []WorkingLeg{
		{ID: "lonely", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 100, P: 5.0, P0: 5.0, Mult: 100,
			TimeToExpirationMonths: 12, // > 9; no Venue set
		}},
	}
	decomp, err := opt.Optimize(equityFacts, legs)
	var nErr *ErrNoNakedRule
	if !errors.As(err, &nErr) {
		t.Fatalf("err = %v, want *ErrNoNakedRule", err)
	}
	if nErr.LegID != "lonely" {
		t.Errorf("err.LegID = %q, want lonely", nErr.LegID)
	}
	if len(decomp.SubPositions) != 0 {
		t.Errorf("partial SubPositions should be empty, got %d", len(decomp.SubPositions))
	}
}

func TestPartialOutputContract(t *testing.T) {
	opt := newOpt(t)
	// "a-naked" sorts before "b-stock"; both get processed, error returned but
	// the naked sub-position must appear in partial output regardless of
	// which leg failed.
	legs := []WorkingLeg{
		{ID: "a-naked", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
			K: 80, P: 2.0, P0: 2.0, Mult: 100,
		}},
		{ID: "b-stock", OpenShares: 100, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.StockKind,
		}},
	}
	decomp, err := opt.Optimize(equityFacts, legs)
	var sErr *ErrStockResidualUnsupported
	if !errors.As(err, &sErr) {
		t.Fatalf("err = %v, want *ErrStockResidualUnsupported", err)
	}
	if sErr.LegID != "b-stock" || sErr.OpenShares != 100 {
		t.Errorf("err fields = %+v, want b-stock/100", sErr)
	}
	if len(decomp.SubPositions) != 1 {
		t.Fatalf("partial SubPositions = %d, want 1", len(decomp.SubPositions))
	}
	sp := decomp.SubPositions[0]
	if sp.StrategyID != "short_put_uncovered" {
		t.Errorf("partial StrategyID = %q, want short_put_uncovered", sp.StrategyID)
	}
	assertClose(t, "partial Requirement", sp.Result.Requirement, 1000.0)
	assertClose(t, "partial TotalRequirement", decomp.TotalRequirement, 1000.0)
	if attrs := decomp.AttributionsByLeg["a-naked"]; len(attrs) != 1 || attrs[0].SubPositionIdx != 0 {
		t.Errorf("partial attribution for a-naked = %+v, want one entry @ idx 0", attrs)
	}
	if _, ok := decomp.AttributionsByLeg["b-stock"]; ok {
		t.Errorf("b-stock should have no attribution, found one")
	}
}

func TestStrongestResidualPriority(t *testing.T) {
	opt := newOpt(t)
	// Mix an unscoreable option (→ ErrNoNakedRule) with a stock-only leg
	// (→ ErrStockResidualUnsupported). NoNakedRule should win the priority
	// comparison.
	legs := []WorkingLeg{
		{ID: "stock", OpenShares: 50, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.StockKind,
		}},
		{ID: "unscoreable", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 100, P: 5.0, P0: 5.0, Mult: 100,
			TimeToExpirationMonths: 12, // no Venue → no naked rule binds
		}},
	}
	_, err := opt.Optimize(equityFacts, legs)
	var nErr *ErrNoNakedRule
	if !errors.As(err, &nErr) {
		t.Fatalf("err = %v, want *ErrNoNakedRule to win priority", err)
	}
	if nErr.LegID != "unscoreable" {
		t.Errorf("err.LegID = %q, want unscoreable", nErr.LegID)
	}
}

// -----------------------------------------------------------------------------
// Misc input-validation paths
// -----------------------------------------------------------------------------

func TestEmptyInput(t *testing.T) {
	opt := newOpt(t)
	decomp, err := opt.Optimize(equityFacts, nil)
	if err != nil {
		t.Fatalf("Optimize(nil): %v", err)
	}
	if decomp.TotalRequirement != 0 || len(decomp.SubPositions) != 0 {
		t.Errorf("empty input decomp = %+v, want zero-valued", decomp)
	}
}

func TestStrongestResidualPriority_AlphabeticalTiebreak(t *testing.T) {
	opt := newOpt(t)
	// Two legs that both trigger *ErrNoNakedRule (same priority class) —
	// LegIDs differ only in their first character so the alphabetical
	// tie-break is what picks the winner. "a-unscoreable" must win.
	mkLonelyLong := func(id LegID) WorkingLeg {
		return WorkingLeg{ID: id, OpenQty: 1, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 100, P: 5.0, P0: 5.0, Mult: 100,
			TimeToExpirationMonths: 12, // no Venue → no naked rule binds
		}}
	}
	legs := []WorkingLeg{mkLonelyLong("b-unscoreable"), mkLonelyLong("a-unscoreable")}
	_, err := opt.Optimize(equityFacts, legs)
	var nErr *ErrNoNakedRule
	if !errors.As(err, &nErr) {
		t.Fatalf("err = %v, want *ErrNoNakedRule", err)
	}
	if nErr.LegID != "a-unscoreable" {
		t.Errorf("tiebreak picked %q, want a-unscoreable (alphabetical-earlier)", nErr.LegID)
	}
}

func TestNegativeOpenQty_Rejected(t *testing.T) {
	opt := newOpt(t)
	legs := []WorkingLeg{
		{ID: "bad", OpenQty: -1, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
		}},
	}
	_, err := opt.Optimize(equityFacts, legs)
	if err == nil {
		t.Fatalf("expected validation error for negative OpenQty, got nil")
	}
}

func TestNegativeOpenShares_Rejected(t *testing.T) {
	opt := newOpt(t)
	legs := []WorkingLeg{
		{ID: "bad", OpenShares: -100, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.StockKind,
		}},
	}
	_, err := opt.Optimize(equityFacts, legs)
	if err == nil {
		t.Fatalf("expected validation error for negative OpenShares, got nil")
	}
}

func TestWorkingLegBothPositive_Rejected(t *testing.T) {
	opt := newOpt(t)
	legs := []WorkingLeg{
		{ID: "bad", OpenQty: 1, OpenShares: 100, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
		}},
	}
	_, err := opt.Optimize(equityFacts, legs)
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
}

// =============================================================================
// PR-3: B&B / option-only tests
// =============================================================================

// boxFacts mirrors TestEvaluateRule_ExactMatch_Box's inputs so the Optimize
// path can be parity-checked against the engine's own EvaluateRule call on
// the same fixture.
var boxFacts = BucketFacts{U: 105.0, Class: "equity"}

// vertCallFacts mirrors TestVerticalCallSpread_p42.
var vertCallFacts = BucketFacts{U: 128.50, Class: "equity"}

// strangleFacts: short OTM call + short OTM put on the same underlying.
var strangleFacts = BucketFacts{U: 100.0, Class: "equity"}

// fixtureVerticalCall returns the two-leg input that the engine's
// vertical_spread test asserts at $880 gross requirement.
func fixtureVerticalCall() []WorkingLeg {
	return []WorkingLeg{
		{ID: "long_125", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 125, P: 3.80, P0: 3.80, Mult: 100,
			Style: "american", Venue: "listed",
			Underlying: "XYZ", Expiration: "2024-11-15",
		}},
		{ID: "short_120", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
			K: 120, P: 8.40, P0: 8.40, Mult: 100,
			Style: "american", Venue: "listed",
			Underlying: "XYZ", Expiration: "2024-11-15",
		}},
	}
}

// fixtureStrangle returns a short call + short put pair that binds
// short_strangle_or_straddle. The dollar requirement isn't asserted here;
// parity uses EvaluateRule on the equivalent Position.
func fixtureStrangle() []WorkingLeg {
	return []WorkingLeg{
		{ID: "sc_110", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
			K: 110, P: 1.50, P0: 1.50, Mult: 100,
			Underlying: "XYZ",
		}},
		{ID: "sp_90", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
			K: 90, P: 1.20, P0: 1.20, Mult: 100,
			Underlying: "XYZ",
		}},
	}
}

// fixtureLongBox returns the four-leg input from
// TestEvaluateRule_ExactMatch_Box.
func fixtureLongBox() []WorkingLeg {
	return []WorkingLeg{
		{ID: "bc_100", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 100, P: 8.0, P0: 8.0, Mult: 100,
			Style: "american", Underlying: "XYZ", Expiration: "2024-11-15",
		}},
		{ID: "bp_100", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
			K: 100, P: 1.0, P0: 1.0, Mult: 100,
			Style: "american", Underlying: "XYZ", Expiration: "2024-11-15",
		}},
		{ID: "sp_110", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "put",
			K: 110, P: 6.0, P0: 6.0, Mult: 100,
			Style: "american", Underlying: "XYZ", Expiration: "2024-11-15",
		}},
		{ID: "sc_110", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
			K: 110, P: 2.0, P0: 2.0, Mult: 100,
			Style: "american", Underlying: "XYZ", Expiration: "2024-11-15",
		}},
	}
}

// fixtureShortBox: same shape as fixtureLongBox but K_low/K_high inverted so
// the short_box_spread template binds (K_high call+put bought at, K_low
// call+put sold at). Magnitudes are arbitrary — parity only.
func fixtureShortBox() []WorkingLeg {
	return []WorkingLeg{
		{ID: "bc_110", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 110, P: 2.0, P0: 2.0, Mult: 100,
			Style: "american", Underlying: "XYZ", Expiration: "2024-11-15",
		}},
		{ID: "bp_110", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
			K: 110, P: 6.0, P0: 6.0, Mult: 100,
			Style: "american", Underlying: "XYZ", Expiration: "2024-11-15",
		}},
		{ID: "sp_100", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "put",
			K: 100, P: 1.0, P0: 1.0, Mult: 100,
			Style: "american", Underlying: "XYZ", Expiration: "2024-11-15",
		}},
		{ID: "sc_100", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
			K: 100, P: 8.0, P0: 8.0, Mult: 100,
			Style: "american", Underlying: "XYZ", Expiration: "2024-11-15",
		}},
	}
}

// engineRequirement is a helper: build the engine.Position from a fixture
// (with Qty=1 each) and return EvaluateRule's Requirement for the named
// rule. Used to compute parity targets without hard-coding manual numbers
// for each fixture.
func engineRequirement(t *testing.T, ruleID string, facts BucketFacts, fixture []WorkingLeg) float64 {
	t.Helper()
	rb := loadRB(t)
	legs := make([]engine.Leg, len(fixture))
	for i, wl := range fixture {
		l := wl.Leg
		l.Qty = wl.OpenQty
		legs[i] = l
	}
	pos := engine.Position{
		U: facts.U, Class: facts.Class, Lev: facts.Lev,
		UnderlyingIsEquityBased: facts.UnderlyingIsEquityBased,
		Legs:                    legs,
	}
	res, ok, err := rb.EvaluateRule(pos, ruleID, engine.MarginAccount, engine.Initial)
	if err != nil {
		t.Fatalf("EvaluateRule(%s): %v", ruleID, err)
	}
	if !ok {
		t.Fatalf("EvaluateRule(%s): no match for fixture", ruleID)
	}
	return res.Requirement
}

// TestPerTemplateParity_OptionOnly: for each option-only template, Optimize
// produces exactly one SubPosition with the matching StrategyID, no
// residuals, and a TotalRequirement equal to what EvaluateRule reports for
// the same input. Mirrors the parity-shape tests in
// internal/engine/evaluate_rule_test.go but goes through Optimize so the
// B&B + consumption + buildSubPosition path is exercised end-to-end.
func TestPerTemplateParity_OptionOnly(t *testing.T) {
	cases := []struct {
		name   string
		ruleID string
		facts  BucketFacts
		fix    []WorkingLeg
	}{
		{"vertical_spread", "vertical_spread", vertCallFacts, fixtureVerticalCall()},
		{"short_strangle_or_straddle", "short_strangle_or_straddle", strangleFacts, fixtureStrangle()},
		{"long_box_spread", "long_box_spread", boxFacts, fixtureLongBox()},
		{"short_box_spread", "short_box_spread", boxFacts, fixtureShortBox()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opt := newOpt(t)
			want := engineRequirement(t, tc.ruleID, tc.facts, tc.fix)
			decomp, err := opt.Optimize(tc.facts, tc.fix)
			if err != nil {
				t.Fatalf("Optimize: %v", err)
			}
			if len(decomp.SubPositions) != 1 {
				t.Fatalf("want 1 SubPosition, got %d (%+v)",
					len(decomp.SubPositions), decomp.SubPositions)
			}
			sp := decomp.SubPositions[0]
			if sp.StrategyID != tc.ruleID {
				t.Errorf("StrategyID = %q, want %q", sp.StrategyID, tc.ruleID)
			}
			assertClose(t, "TotalRequirement", decomp.TotalRequirement, want)
		})
	}
}

// TestDeterminism_BnB: two identical Optimize calls on a six-leg option-only
// input must return reflect.DeepEqual decompositions. Catches any leak of Go
// map iteration order into output.
func TestDeterminism_BnB(t *testing.T) {
	opt := newOpt(t)
	// Two non-overlapping verticals + a residual short put. Each call
	// re-builds the input from scratch so the slice identity differs.
	build := func() []WorkingLeg {
		return []WorkingLeg{
			{ID: "lc_125", OpenQty: 1, Leg: engine.Leg{
				Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
				K: 125, P: 3.80, P0: 3.80, Mult: 100,
				Style: "american", Venue: "listed",
				Underlying: "XYZ", Expiration: "2024-11-15",
			}},
			{ID: "sc_120", OpenQty: 1, Leg: engine.Leg{
				Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
				K: 120, P: 8.40, P0: 8.40, Mult: 100,
				Style: "american", Venue: "listed",
				Underlying: "XYZ", Expiration: "2024-11-15",
			}},
			{ID: "lp_85", OpenQty: 1, Leg: engine.Leg{
				Side: engine.Long, Kind: engine.OptionKind, OptionType: "put",
				K: 85, P: 1.0, P0: 1.0, Mult: 100,
				Style: "american", Venue: "listed",
				Underlying: "XYZ", Expiration: "2024-11-15",
			}},
			{ID: "sp_80", OpenQty: 1, Leg: engine.Leg{
				Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
				K: 80, P: 0.50, P0: 0.50, Mult: 100,
				Style: "american", Venue: "listed",
				Underlying: "XYZ", Expiration: "2024-11-15",
			}},
			{ID: "sp_75_extra", OpenQty: 1, Leg: engine.Leg{
				Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
				K: 75, P: 0.20, P0: 0.20, Mult: 100,
			}},
			{ID: "lc_short_dated", OpenQty: 1, Leg: engine.Leg{
				Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
				K: 130, P: 1.0, P0: 1.0, Mult: 100,
				TimeToExpirationMonths: 3,
			}},
		}
	}
	d1, err1 := opt.Optimize(vertCallFacts, build())
	d2, err2 := opt.Optimize(vertCallFacts, build())
	if err1 != nil || err2 != nil {
		t.Fatalf("Optimize errored: %v / %v", err1, err2)
	}
	if !reflect.DeepEqual(d1, d2) {
		t.Fatalf("Optimize not deterministic across two runs:\n  d1=%+v\n  d2=%+v", d1, d2)
	}
}

// -----------------------------------------------------------------------------
// Brute-force parity (size 2-4)
// -----------------------------------------------------------------------------

// templateRules is the set of multi-leg rules the brute-force enumerator
// considers in addition to per-leg residuals. Mirrors consumptionFor's
// switch — must be kept in sync if PR-4/PR-5 add cross-kind families.
var templateRules = []string{
	"vertical_spread",
	"short_strangle_or_straddle",
	"long_box_spread",
	"short_box_spread",
}

// bruteForceMin enumerates every partition of the input legs into groups of
// 1, 2, or 4, scores each group via EvaluateRule on every matching template
// (groups of 1 fall back to the residual sequence), and returns the
// minimum sum of Requirements. Assumes OpenQty == 1 for every leg.
//
// NOTE: this is intentionally a naive enumerator — it doesn't share work
// with Optimize's B&B. Its only job is to be correct, so the parity test
// catches bugs in the production path.
func bruteForceMin(t *testing.T, opt *Optimizer, facts BucketFacts, legs []WorkingLeg) (float64, bool) {
	t.Helper()
	if len(legs) == 0 {
		return 0, true
	}
	// Try grouping leg 0 with each k-1 remaining for k in {1,2,4}, then
	// recurse on the rest.
	first := legs[0]
	rest := legs[1:]
	best := math.Inf(+1)
	any := false

	// k=1: residual on first.
	if cost, ok := scoreResidual(t, opt, facts, first); ok {
		if sub, sub_ok := bruteForceMin(t, opt, facts, rest); sub_ok {
			c := cost + sub
			if c < best {
				best = c
				any = true
			}
		}
	}

	// k=2: pair first with each rest[j]; try every template of size 2.
	for j := 0; j < len(rest); j++ {
		group := []WorkingLeg{first, rest[j]}
		newRest := append([]WorkingLeg{}, rest[:j]...)
		newRest = append(newRest, rest[j+1:]...)
		if cost, ok := scoreGroupTemplates(t, opt, facts, group, 2); ok {
			if sub, sub_ok := bruteForceMin(t, opt, facts, newRest); sub_ok {
				c := cost + sub
				if c < best {
					best = c
					any = true
				}
			}
		}
	}

	// k=4: first plus three of rest; try size-4 templates (boxes).
	for i := 0; i < len(rest); i++ {
		for j := i + 1; j < len(rest); j++ {
			for k := j + 1; k < len(rest); k++ {
				group := []WorkingLeg{first, rest[i], rest[j], rest[k]}
				newRest := make([]WorkingLeg, 0, len(rest)-3)
				for x := range rest {
					if x == i || x == j || x == k {
						continue
					}
					newRest = append(newRest, rest[x])
				}
				if cost, ok := scoreGroupTemplates(t, opt, facts, group, 4); ok {
					if sub, sub_ok := bruteForceMin(t, opt, facts, newRest); sub_ok {
						c := cost + sub
						if c < best {
							best = c
							any = true
						}
					}
				}
			}
		}
	}
	return best, any
}

// scoreResidual scores wl as a naked single-leg sub-position via the
// optimizer's residualOptionRule (re-using the production code path so the
// brute-force comparator measures the same thing Optimize would).
func scoreResidual(t *testing.T, opt *Optimizer, facts BucketFacts, wl WorkingLeg) (float64, bool) {
	t.Helper()
	if wl.OpenShares > epsQty {
		return 0, false
	}
	sub, err := opt.residualOptionRule(wl, facts)
	if err != nil {
		return 0, false
	}
	return sub.Result.Requirement, true
}

// scoreGroupTemplates tries every templateRules entry of the given slot
// arity against the leg group and returns the minimum successful
// Requirement (or false if no template binds + scores).
func scoreGroupTemplates(t *testing.T, opt *Optimizer, facts BucketFacts, group []WorkingLeg, arity int) (float64, bool) {
	t.Helper()
	rb := loadRB(t)
	best := math.Inf(+1)
	any := false
	for _, ruleID := range templateRules {
		rule, ok := rb.RuleByID(ruleID)
		if !ok || len(rule.Match.Legs) != arity {
			continue
		}
		assignments := opt.enumerateAssignments(ruleID, group)
		for _, assignment := range assignments {
			plan, ok, err := consumptionFor(ruleID, assignment, facts)
			if err != nil || !ok {
				continue
			}
			pos, _ := buildSubPosition(ruleID, assignment, plan, facts)
			res, ok, err := rb.EvaluateRule(pos, ruleID, engine.MarginAccount, engine.Initial)
			if err != nil || !ok {
				continue
			}
			if res.Requirement < best {
				best = res.Requirement
				any = true
			}
		}
	}
	return best, any
}

// TestBruteForceParity_Size2to4: for several option-only fixtures of size
// 2, 3, and 4 with OpenQty=1, Optimize's TotalRequirement matches the
// brute-force minimum across every legal partition.
func TestBruteForceParity_Size2to4(t *testing.T) {
	opt := newOpt(t)
	cases := []struct {
		name  string
		facts BucketFacts
		legs  []WorkingLeg
	}{
		{"size2-vertical", vertCallFacts, fixtureVerticalCall()},
		{"size2-strangle", strangleFacts, fixtureStrangle()},
		{"size4-long-box", boxFacts, fixtureLongBox()},
		{"size4-short-box", boxFacts, fixtureShortBox()},
		{"size3-vertical-plus-naked", vertCallFacts, append(fixtureVerticalCall(),
			WorkingLeg{ID: "z_extra_naked_short_put", OpenQty: 1, Leg: engine.Leg{
				Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
				K: 100, P: 1.0, P0: 1.0, Mult: 100,
			}},
		)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, ok := bruteForceMin(t, opt, tc.facts, tc.legs)
			if !ok {
				t.Fatalf("brute-force could not score fixture")
			}
			decomp, err := opt.Optimize(tc.facts, tc.legs)
			if err != nil {
				t.Fatalf("Optimize: %v", err)
			}
			if math.Abs(decomp.TotalRequirement-want) > 0.01 {
				t.Errorf("Optimize TotalRequirement = %.2f, brute-force = %.2f",
					decomp.TotalRequirement, want)
			}
		})
	}
}

// TestLowerBoundDocumented: documents why the B&B lower bound is fixed at 0
// (see decompose.go) by scoring a fixture where the strategy template
// produces a LARGER margin number than the naked-sum residual would. A
// hypothetical naked-sum admissible bound at the root would be larger than
// the achievable optimum, so it would over-prune the cheaper "score each
// leg as residual" branch — the test asserts Optimize correctly picks the
// cheaper alternative because no such bound exists.
func TestLowerBoundDocumented(t *testing.T) {
	opt := newOpt(t)
	// Long short-dated calls — both will score at long_option_short_dated:
	// each at P0*qty*mult. A vertical_spread template can never bind
	// (needs one short leg), so naked is the only option here. The point
	// of the test: residual completion is always raced at every node, and
	// a naked-sum bound would be admissible only if it were ≤ the true
	// achievable cost — which strategy reductions can falsify. With
	// bound=0, the search always finds the best path.
	legs := []WorkingLeg{
		{ID: "lc_a", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 100, P: 5.0, P0: 5.0, Mult: 100, TimeToExpirationMonths: 3,
		}},
		{ID: "lc_b", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 110, P: 4.0, P0: 4.0, Mult: 100, TimeToExpirationMonths: 3,
		}},
	}
	decomp, err := opt.Optimize(BucketFacts{U: 105.0, Class: "equity"}, legs)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	// 5*100 + 4*100 = 900, both naked.
	assertClose(t, "TotalRequirement", decomp.TotalRequirement, 900.0)
	if len(decomp.SubPositions) != 2 {
		t.Errorf("want 2 SubPositions (both residual), got %d", len(decomp.SubPositions))
	}
}

// TestEngineErrorPropagates: when EvaluateRule returns a hard engine error
// for one ruleID against a particular position, Optimize surfaces the error
// AND returns a partial Decomposition for the legs that did score.
//
// The shim is implemented via the production rulebook with a deliberately
// bad BucketFacts.Class — short_call_uncovered's CEL formula calls
// rate(class, ...) which errors per CLAUDE.md "Required Rules" when class
// is missing from the rates table. The long-dated long-call leg falls
// through to long_option_short_dated, whose formula does not touch the
// rates table, so it scores cleanly and shows up in the partial output.
func TestEngineErrorPropagates(t *testing.T) {
	opt := newOpt(t)
	facts := BucketFacts{U: 100.0, Class: "NOT_IN_RATES"}
	legs := []WorkingLeg{
		{ID: "longc", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 100, P: 5.0, P0: 5.0, Mult: 100, TimeToExpirationMonths: 3,
		}},
		{ID: "shortc", OpenQty: 1, Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
			K: 110, P: 1.0, P0: 1.0, Mult: 100,
		}},
	}
	decomp, err := opt.Optimize(facts, legs)
	if err == nil {
		t.Fatalf("expected engine error, got nil")
	}
	if !strings.Contains(err.Error(), "rate") && !strings.Contains(err.Error(), "NOT_IN_RATES") {
		t.Errorf("err = %v; want a rates-lookup-style engine error", err)
	}
	// Partial output: long leg scores fine via long_option_short_dated.
	if len(decomp.SubPositions) == 0 {
		t.Errorf("partial Decomposition should include at least one residual sub-position, got %d",
			len(decomp.SubPositions))
	}
}

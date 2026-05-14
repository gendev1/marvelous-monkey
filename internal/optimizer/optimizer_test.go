package optimizer

import (
	"errors"
	"math"
	"reflect"
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

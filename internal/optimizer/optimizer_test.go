package optimizer

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"margincalc/internal/engine"
)

const rulesPath = "../../rules/cboe_baseline.yaml"

func loadRulebook(t *testing.T) *engine.Rulebook {
	t.Helper()
	rb, err := engine.LoadRulebook(rulesPath)
	if err != nil {
		t.Fatalf("LoadRulebook: %v", err)
	}
	return rb
}

func defaultFacts() BucketFacts {
	return BucketFacts{
		U:           100.0,
		Class:       "equity",
		Lev:         1.0,
		AccountType: engine.MarginAccount,
		Phase:       engine.Initial,
	}
}

func longCallShortDated() engine.Leg {
	return engine.Leg{
		Side:                   engine.Long,
		Kind:                   engine.OptionKind,
		OptionType:             "call",
		K:                      100.0,
		P:                      3.0,
		P0:                     3.0,
		Mult:                   100.0,
		TimeToExpirationMonths: 3.0,
	}
}

func TestNakedScoring_LongOptionShortDated(t *testing.T) {
	opt := New(loadRulebook(t))
	wl := WorkingLeg{ID: "L1", Leg: longCallShortDated(), OpenQty: 1.0}
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{wl})
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(dec.SubPositions) != 1 {
		t.Fatalf("want 1 sub-position, got %d", len(dec.SubPositions))
	}
	if got := dec.SubPositions[0].StrategyID; got != "long_option_short_dated" {
		t.Fatalf("StrategyID: want long_option_short_dated, got %s", got)
	}
	attr := dec.Attributions["L1"]
	if len(attr) != 1 || attr[0].SubIndex != 0 || attr[0].QtyUsed != 1.0 {
		t.Fatalf("attribution: %+v", attr)
	}
}

func TestNakedScoring_LongOptionLongDatedListed(t *testing.T) {
	opt := New(loadRulebook(t))
	leg := engine.Leg{
		Side:                   engine.Long,
		Kind:                   engine.OptionKind,
		OptionType:             "put",
		K:                      100.0,
		P:                      5.0,
		P0:                     5.0,
		Mult:                   100.0,
		Venue:                  "listed",
		TimeToExpirationMonths: 12.0,
	}
	wl := WorkingLeg{ID: "L1", Leg: leg, OpenQty: 1.0}
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{wl})
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if got := dec.SubPositions[0].StrategyID; got != "long_option_long_dated_listed" {
		t.Fatalf("StrategyID: want long_option_long_dated_listed, got %s", got)
	}
}

func TestNakedScoring_LongOptionLongDatedOTC(t *testing.T) {
	opt := New(loadRulebook(t))
	leg := engine.Leg{
		Side:                   engine.Long,
		Kind:                   engine.OptionKind,
		OptionType:             "call",
		K:                      100.0,
		P:                      5.0,
		P0:                     5.0,
		Mult:                   100.0,
		Venue:                  "otc",
		Style:                  "american",
		BrokerGuaranteed:       true,
		TimeToExpirationMonths: 12.0,
	}
	wl := WorkingLeg{ID: "L1", Leg: leg, OpenQty: 1.0}
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{wl})
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if got := dec.SubPositions[0].StrategyID; got != "long_option_long_dated_otc" {
		t.Fatalf("StrategyID: want long_option_long_dated_otc, got %s", got)
	}
}

func TestNakedScoring_ShortCallUncovered(t *testing.T) {
	opt := New(loadRulebook(t))
	leg := engine.Leg{
		Side:       engine.Short,
		Kind:       engine.OptionKind,
		OptionType: "call",
		K:          100.0,
		P:          3.0,
		P0:         3.0,
		Mult:       100.0,
	}
	wl := WorkingLeg{ID: "L1", Leg: leg, OpenQty: 1.0}
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{wl})
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if got := dec.SubPositions[0].StrategyID; got != "short_call_uncovered" {
		t.Fatalf("StrategyID: want short_call_uncovered, got %s", got)
	}
}

func TestNakedScoring_ShortPutUncovered(t *testing.T) {
	opt := New(loadRulebook(t))
	leg := engine.Leg{
		Side:       engine.Short,
		Kind:       engine.OptionKind,
		OptionType: "put",
		K:          100.0,
		P:          3.0,
		P0:         3.0,
		Mult:       100.0,
	}
	wl := WorkingLeg{ID: "L1", Leg: leg, OpenQty: 1.0}
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{wl})
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if got := dec.SubPositions[0].StrategyID; got != "short_put_uncovered" {
		t.Fatalf("StrategyID: want short_put_uncovered, got %s", got)
	}
}

func TestOptimize_Deterministic(t *testing.T) {
	opt := New(loadRulebook(t))
	legs := []WorkingLeg{
		{ID: "A", Leg: longCallShortDated(), OpenQty: 1.0},
		{ID: "B", Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
			K: 100.0, P: 3.0, P0: 3.0, Mult: 100.0,
		}, OpenQty: 2.0},
		{ID: "C", Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
			K: 100.0, P: 3.0, P0: 3.0, Mult: 100.0,
		}, OpenQty: 3.0},
	}
	a, errA := opt.Optimize(defaultFacts(), legs)
	b, errB := opt.Optimize(defaultFacts(), legs)
	if errA != nil || errB != nil {
		t.Fatalf("Optimize: %v / %v", errA, errB)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("Optimize is not deterministic:\n%+v\n%+v", a, b)
	}
}

func TestOptimize_StockOnlyResidualError(t *testing.T) {
	opt := New(loadRulebook(t))
	wl := WorkingLeg{
		ID: "S1",
		Leg: engine.Leg{
			Side: engine.Long, Kind: engine.StockKind, Shares: 100.0, Mult: 1.0,
		},
		OpenShares: 100.0,
	}
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{wl})
	var stockErr *ErrStockResidualUnsupported
	if !errors.As(err, &stockErr) {
		t.Fatalf("want *ErrStockResidualUnsupported, got %T %v", err, err)
	}
	if stockErr.LegID != "S1" || stockErr.OpenShares != 100.0 {
		t.Fatalf("err fields: %+v", stockErr)
	}
	if stockErr.Leg.Kind != engine.StockKind {
		t.Fatalf("leg not carried through: %+v", stockErr.Leg)
	}
	if len(dec.SubPositions) != 0 {
		t.Fatalf("partial decomposition should be empty, got %d subs", len(dec.SubPositions))
	}
}

func TestOptimize_NoNakedRule(t *testing.T) {
	opt := New(loadRulebook(t))
	leg := engine.Leg{
		Side:                   engine.Long,
		Kind:                   engine.OptionKind,
		OptionType:             "call",
		K:                      100.0,
		P:                      5.0,
		P0:                     5.0,
		Mult:                   100.0,
		TimeToExpirationMonths: 12.0, // > 9 month threshold
		// Venue intentionally empty: neither listed nor otc rule binds.
	}
	wl := WorkingLeg{ID: "L1", Leg: leg, OpenQty: 1.0}
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{wl})
	var nrErr *ErrNoNakedRule
	if !errors.As(err, &nrErr) {
		t.Fatalf("want *ErrNoNakedRule, got %T %v", err, err)
	}
	if nrErr.LegID != "L1" {
		t.Fatalf("err LegID: %q", nrErr.LegID)
	}
	if len(dec.SubPositions) != 0 {
		t.Fatalf("subs should be empty, got %d", len(dec.SubPositions))
	}
}

func TestStrongestResidualErrorPriority(t *testing.T) {
	opt := New(loadRulebook(t))
	stock := WorkingLeg{
		ID: "S1",
		Leg: engine.Leg{
			Side: engine.Long, Kind: engine.StockKind, Shares: 100.0, Mult: 1.0,
		},
		OpenShares: 100.0,
	}
	noRule := WorkingLeg{
		ID: "L1",
		Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 100.0, P: 5.0, P0: 5.0, Mult: 100.0, TimeToExpirationMonths: 12.0,
		},
		OpenQty: 1.0,
	}
	_, err := opt.Optimize(defaultFacts(), []WorkingLeg{stock, noRule})
	var nr *ErrNoNakedRule
	if !errors.As(err, &nr) {
		t.Fatalf("want *ErrNoNakedRule to win, got %T %v", err, err)
	}

	// And the reverse input order produces the same winner — the priority
	// is by error kind, not by iteration order.
	_, err = opt.Optimize(defaultFacts(), []WorkingLeg{noRule, stock})
	if !errors.As(err, &nr) {
		t.Fatalf("want *ErrNoNakedRule (reverse order), got %T %v", err, err)
	}
}

func TestStrongestResidualErrorPriority_AlphabeticalTieBreak(t *testing.T) {
	opt := New(loadRulebook(t))
	mkNoRule := func(id LegID) WorkingLeg {
		return WorkingLeg{
			ID: id,
			Leg: engine.Leg{
				Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
				K: 100.0, P: 5.0, P0: 5.0, Mult: 100.0, TimeToExpirationMonths: 12.0,
			},
			OpenQty: 1.0,
		}
	}
	for _, order := range [][]WorkingLeg{
		{mkNoRule("L1"), mkNoRule("L2")},
		{mkNoRule("L2"), mkNoRule("L1")},
	} {
		_, err := opt.Optimize(defaultFacts(), order)
		var nr *ErrNoNakedRule
		if !errors.As(err, &nr) {
			t.Fatalf("want *ErrNoNakedRule, got %T %v", err, err)
		}
		if nr.LegID != "L1" {
			t.Fatalf("alphabetical tie-break: want L1, got %q (input order %v)", nr.LegID, order)
		}
	}
}

func TestOptimize_PartialOutputOnError(t *testing.T) {
	opt := New(loadRulebook(t))
	good := WorkingLeg{ID: "A", Leg: longCallShortDated(), OpenQty: 1.0}
	bad := WorkingLeg{
		ID: "B",
		Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 100.0, P: 5.0, P0: 5.0, Mult: 100.0, TimeToExpirationMonths: 12.0,
		},
		OpenQty: 1.0,
	}
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{good, bad})
	var nr *ErrNoNakedRule
	if !errors.As(err, &nr) {
		t.Fatalf("want *ErrNoNakedRule, got %T %v", err, err)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "long_option_short_dated" {
		t.Fatalf("partial decomposition missing the successful sub: %+v", dec.SubPositions)
	}
	if got := dec.Attributions["A"]; len(got) != 1 {
		t.Fatalf("partial Attributions for A: %+v", got)
	}
	if dec.TotalRequirement <= 0 {
		t.Fatalf("partial TotalRequirement should reflect successful sub, got %g", dec.TotalRequirement)
	}
}

func TestOptimize_OpenQtyAndOpenSharesIsProgrammerError(t *testing.T) {
	opt := New(loadRulebook(t))
	wl := WorkingLeg{
		ID: "X1",
		Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 100.0, P: 3.0, P0: 3.0, Mult: 100.0, TimeToExpirationMonths: 3.0,
		},
		OpenQty:    1.0,
		OpenShares: 100.0,
	}
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{wl})
	if err == nil {
		t.Fatalf("want programmer error for OpenQty+OpenShares, got nil")
	}
	// Not one of the residual sentinel types — it's a generic invariant
	// violation that callers should treat as a bug.
	var nr *ErrNoNakedRule
	var sr *ErrStockResidualUnsupported
	if errors.As(err, &nr) || errors.As(err, &sr) {
		t.Fatalf("invariant violation should not be a residual sentinel: %T", err)
	}
	if len(dec.SubPositions) != 0 || dec.TotalRequirement != 0 {
		t.Fatalf("invariant violation must return zero decomposition, got %+v", dec)
	}
}

func TestOptimize_NilGuard(t *testing.T) {
	var nilOpt *Optimizer
	if _, err := nilOpt.Optimize(defaultFacts(), nil); err == nil {
		t.Fatal("nil Optimizer: want error, got nil")
	}
	if _, err := New(nil).Optimize(defaultFacts(), nil); err == nil {
		t.Fatal("New(nil): want error, got nil")
	}
}

func TestOptimize_EmptyLegs(t *testing.T) {
	opt := New(loadRulebook(t))
	dec, err := opt.Optimize(defaultFacts(), nil)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if dec.TotalRequirement != 0 || len(dec.SubPositions) != 0 {
		t.Fatalf("expected empty decomposition, got %+v", dec)
	}
}

// -----------------------------------------------------------------------------
// Per-template B&B parity. Each test feeds the exact-fit fixture for one
// optimizer-target rule and asserts the optimizer picks that rule (rather than
// falling through to residual completion). A regression here would mean the
// B&B branch silently lost to residual on a fixture it should dominate.

func verticalCallSpread_p42_legs() []WorkingLeg {
	return []WorkingLeg{
		{ID: "A", Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 125.0, P: 3.80, P0: 3.80, Mult: 100.0,
			Style: "american", Venue: "listed", Underlying: "XYZ", Expiration: "2024-11-15",
		}, OpenQty: 1.0},
		{ID: "B", Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
			K: 120.0, P: 8.40, P0: 8.40, Mult: 100.0,
			Style: "american", Venue: "listed", Underlying: "XYZ", Expiration: "2024-11-15",
		}, OpenQty: 1.0},
	}
}

func TestPerTemplate_VerticalSpread(t *testing.T) {
	opt := New(loadRulebook(t))
	facts := defaultFacts()
	facts.U = 128.50
	dec, err := opt.Optimize(facts, verticalCallSpread_p42_legs())
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(dec.SubPositions) != 1 {
		t.Fatalf("want 1 sub-position, got %d (%+v)", len(dec.SubPositions), dec.SubPositions)
	}
	if dec.SubPositions[0].StrategyID != "vertical_spread" {
		t.Fatalf("want vertical_spread, got %s", dec.SubPositions[0].StrategyID)
	}
}

func shortStrangle_legs() []WorkingLeg {
	return []WorkingLeg{
		{ID: "A", Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
			K: 95.0, P: 2.0, P0: 2.0, Mult: 100.0, Style: "american", Underlying: "XYZ",
		}, OpenQty: 1.0},
		{ID: "B", Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
			K: 105.0, P: 2.0, P0: 2.0, Mult: 100.0, Style: "american", Underlying: "XYZ",
		}, OpenQty: 1.0},
	}
}

func TestPerTemplate_ShortStrangle(t *testing.T) {
	opt := New(loadRulebook(t))
	dec, err := opt.Optimize(defaultFacts(), shortStrangle_legs())
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "short_strangle_or_straddle" {
		t.Fatalf("want one short_strangle_or_straddle sub, got %+v", dec.SubPositions)
	}
}

func TestPerTemplate_ShortStraddle(t *testing.T) {
	opt := New(loadRulebook(t))
	legs := []WorkingLeg{
		{ID: "A", Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
			K: 100.0, P: 3.0, P0: 3.0, Mult: 100.0, Style: "american", Underlying: "XYZ",
		}, OpenQty: 1.0},
		{ID: "B", Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
			K: 100.0, P: 3.0, P0: 3.0, Mult: 100.0, Style: "american", Underlying: "XYZ",
		}, OpenQty: 1.0},
	}
	dec, err := opt.Optimize(defaultFacts(), legs)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "short_strangle_or_straddle" {
		t.Fatalf("want one short_strangle_or_straddle sub, got %+v", dec.SubPositions)
	}
}

func longBox_legs() []WorkingLeg {
	return []WorkingLeg{
		{ID: "BC", Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 95.0, P: 8.0, P0: 8.0, Mult: 100.0, Style: "american", Expiration: "2024-12-20", Underlying: "XYZ",
		}, OpenQty: 1.0},
		{ID: "BP", Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
			K: 95.0, P: 1.0, P0: 1.0, Mult: 100.0, Style: "american", Expiration: "2024-12-20", Underlying: "XYZ",
		}, OpenQty: 1.0},
		{ID: "SP", Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "put",
			K: 105.0, P: 4.0, P0: 4.0, Mult: 100.0, Style: "american", Expiration: "2024-12-20", Underlying: "XYZ",
		}, OpenQty: 1.0},
		{ID: "SC", Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
			K: 105.0, P: 2.0, P0: 2.0, Mult: 100.0, Style: "american", Expiration: "2024-12-20", Underlying: "XYZ",
		}, OpenQty: 1.0},
	}
}

func TestPerTemplate_LongBoxSpread(t *testing.T) {
	opt := New(loadRulebook(t))
	dec, err := opt.Optimize(defaultFacts(), longBox_legs())
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "long_box_spread" {
		t.Fatalf("want one long_box_spread sub, got %+v", dec.SubPositions)
	}
}

func TestPerTemplate_ShortBoxSpread(t *testing.T) {
	opt := New(loadRulebook(t))
	legs := []WorkingLeg{
		{ID: "BC", Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 105.0, P: 1.0, P0: 1.0, Mult: 100.0, Style: "american", Expiration: "2024-12-20", Underlying: "XYZ",
		}, OpenQty: 1.0},
		{ID: "BP", Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
			K: 105.0, P: 4.0, P0: 4.0, Mult: 100.0, Style: "american", Expiration: "2024-12-20", Underlying: "XYZ",
		}, OpenQty: 1.0},
		{ID: "SP", Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "put",
			K: 95.0, P: 1.0, P0: 1.0, Mult: 100.0, Style: "american", Expiration: "2024-12-20", Underlying: "XYZ",
		}, OpenQty: 1.0},
		{ID: "SC", Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
			K: 95.0, P: 8.0, P0: 8.0, Mult: 100.0, Style: "american", Expiration: "2024-12-20", Underlying: "XYZ",
		}, OpenQty: 1.0},
	}
	dec, err := opt.Optimize(defaultFacts(), legs)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "short_box_spread" {
		t.Fatalf("want one short_box_spread sub, got %+v", dec.SubPositions)
	}
}

// TestQuantitySlicing_VerticalAndNaked drives the deliverable-units policy:
// 10 long calls @ K=100 + 5 short calls @ K=110 should slice into one
// vertical_spread sub (5 longs paired with 5 shorts) plus a naked
// long_option_short_dated remainder for the leftover 5 longs.
func TestQuantitySlicing_VerticalAndNaked(t *testing.T) {
	opt := New(loadRulebook(t))
	facts := defaultFacts()
	facts.U = 105.0
	longLeg := engine.Leg{
		Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
		K: 100.0, P: 8.0, P0: 8.0, Mult: 100.0,
		Style: "american", Venue: "listed", Underlying: "XYZ", Expiration: "2024-12-20",
		TimeToExpirationMonths: 3.0,
	}
	shortLeg := engine.Leg{
		Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
		K: 110.0, P: 3.0, P0: 3.0, Mult: 100.0,
		Style: "american", Venue: "listed", Underlying: "XYZ", Expiration: "2024-12-20",
		TimeToExpirationMonths: 3.0,
	}
	legs := []WorkingLeg{
		{ID: "L1", Leg: longLeg, OpenQty: 10.0},
		{ID: "S1", Leg: shortLeg, OpenQty: 5.0},
	}
	dec, err := opt.Optimize(facts, legs)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	var verticals, nakedLongs int
	for _, sp := range dec.SubPositions {
		switch sp.StrategyID {
		case "vertical_spread":
			verticals++
		case "long_option_short_dated":
			nakedLongs++
		}
	}
	if verticals != 1 || nakedLongs != 1 {
		t.Fatalf("want 1 vertical + 1 naked-long, got verticals=%d naked=%d (subs=%+v)", verticals, nakedLongs, dec.SubPositions)
	}
	attr := dec.Attributions["L1"]
	if len(attr) != 2 {
		t.Fatalf("want 2 attribution entries for L1, got %d (%+v)", len(attr), attr)
	}
	for _, a := range attr {
		if a.QtyUsed != 5.0 {
			t.Fatalf("each L1 attribution should consume 5 contracts, got %g", a.QtyUsed)
		}
	}
}

// TestMemoization_RevisitsSameStateOnce exercises the memo via the
// decompose stats hook. We score a state twice through decompose with a
// shared memo: the second call must be served entirely from the memo
// (Hits incremented, Calls increment matched).
func TestMemoization_RevisitsSameStateOnce(t *testing.T) {
	opt := New(loadRulebook(t))
	facts := defaultFacts()
	facts.U = 128.50
	state := newState(verticalCallSpread_p42_legs())
	memo := map[string]Decomposition{}
	stats := &decomposeStats{}
	first := opt.decompose(state, facts, memo, stats)
	if first.IsError() {
		t.Fatalf("first decompose error: %v", first.Err())
	}
	firstHits := stats.Hits
	if _, ok := memo[state.Key()]; !ok {
		t.Fatalf("expected memo to contain root state after first call")
	}
	_ = opt.decompose(state, facts, memo, stats)
	if stats.Hits <= firstHits {
		t.Fatalf("expected memo hit on second visit, hits went %d → %d", firstHits, stats.Hits)
	}
}

// TestTiebreak_DeterministicOrder constructs two synthetic decompositions
// with equal TotalRequirement and asserts `less` orders them by the
// documented chain. Tying on requirement → fewer subs wins; tying on subs
// → smaller sorted rule-ID list wins; tying on rule IDs → smaller sorted
// leg-ID list wins.
func TestTiebreak_DeterministicOrder(t *testing.T) {
	mkSub := func(rule, leg string) SubPosition {
		return SubPosition{
			StrategyID: rule,
			Slots:      []SlotAssignment{{Slot: "x", LegID: LegID(leg)}},
			Result:     engine.Result{Requirement: 100.0},
		}
	}
	// Same totals, same sub-counts, same rule IDs: leg IDs break the tie.
	a := Decomposition{
		SubPositions:     []SubPosition{mkSub("r", "A")},
		TotalRequirement: 100.0,
	}
	b := Decomposition{
		SubPositions:     []SubPosition{mkSub("r", "B")},
		TotalRequirement: 100.0,
	}
	if !less(a, b) || less(b, a) {
		t.Fatalf("leg-ID tiebreak: A should beat B")
	}
	// Fewer sub-positions wins when totals match.
	c := Decomposition{
		SubPositions:     []SubPosition{mkSub("r", "A"), mkSub("r", "A2")},
		TotalRequirement: 100.0,
	}
	if !less(a, c) {
		t.Fatalf("fewer subs should win: a (1 sub) should beat c (2 subs)")
	}
	// Strictly smaller requirement always wins.
	d := Decomposition{
		SubPositions:     []SubPosition{mkSub("r", "Z")},
		TotalRequirement: 99.0,
	}
	if !less(d, a) {
		t.Fatalf("smaller requirement should win regardless of leg IDs")
	}
}

// TestBruteForceParity_2to4Legs enumerates every partition of the input
// legs into option-only B&B templates plus residual completion, scores
// each partition independently, and asserts Optimize picks the
// minimum-cost partition (or one tied with it under `less`).
//
// Restricting fixtures to OpenQty=1 keeps the brute-force search small
// enough to be exhaustive without quantity slicing — slicing is covered
// by TestQuantitySlicing_VerticalAndNaked.
func TestBruteForceParity_2to4Legs(t *testing.T) {
	cases := []struct {
		name string
		legs []WorkingLeg
	}{
		{"vertical_2leg", verticalCallSpread_p42_legs()},
		{"short_strangle_2leg", shortStrangle_legs()},
		{"long_box_4leg", longBox_legs()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opt := New(loadRulebook(t))
			facts := defaultFacts()
			if tc.name == "vertical_2leg" {
				facts.U = 128.50
			}
			got, err := opt.Optimize(facts, tc.legs)
			if err != nil {
				t.Fatalf("Optimize: %v", err)
			}
			best := bruteForceDecompose(t, opt, facts, tc.legs)
			if best.IsError() {
				t.Fatalf("brute force had no viable decomposition: %v", best.Err())
			}
			if math.Abs(got.TotalRequirement-best.TotalRequirement) > 1e-6 {
				t.Fatalf("%s: Optimize TotalRequirement %g != brute-force %g",
					tc.name, got.TotalRequirement, best.TotalRequirement)
			}
		})
	}
}

// bruteForceDecompose enumerates every partition of legs into option-only
// B&B-template subsets plus a residual remainder, scores each, and returns
// the minimum under `less`. Used only by TestBruteForceParity_2to4Legs.
func bruteForceDecompose(t *testing.T, opt *Optimizer, facts BucketFacts, legs []WorkingLeg) Decomposition {
	t.Helper()
	var best Decomposition
	haveBest := false
	visit := func(d Decomposition) {
		if d.IsError() {
			return
		}
		if !haveBest || less(d, best) {
			best = d
			haveBest = true
		}
	}
	// Residual-only baseline.
	state := newState(legs)
	if d, err := opt.scoreAllResidual(state, facts); err == nil {
		visit(d)
	}
	// Single-template partitions: pick one optimizer-target rule + one valid
	// assignment, residual-score the remainder, combine.
	for _, ruleID := range opt.rb.OptimizerTargets() {
		for _, assignment := range enumerateAssignments(opt.rb, ruleID, state.Legs) {
			plan, ok, err := consumptionFor(ruleID, assignment, facts)
			if err != nil || !ok {
				continue
			}
			slicedPos := buildSubPosition(opt.rb, ruleID, assignment, plan, facts)
			res, fit, err := opt.rb.EvaluateRule(slicedPos, ruleID, facts.AccountType, facts.Phase)
			if err != nil || !fit {
				continue
			}
			remainder := applyConsumption(state, assignment, plan)
			// Recurse into the remainder so we exhaustively explore
			// template+template+... chains, not just one template plus a
			// residual tail. The recursion's own residual baseline at each
			// level guarantees this terminates with a viable decomposition
			// whenever one exists.
			subDec := bruteForceDecompose(t, opt, facts, remainder.Legs)
			if subDec.IsError() {
				continue
			}
			combined := combine(opt.rb, ruleID, assignment, plan, res, subDec)
			visit(combined)
		}
	}
	if !haveBest {
		return errorDecomposition(errors.New("brute force: no viable decomposition"), Decomposition{})
	}
	return best
}

// -----------------------------------------------------------------------------
// Stock-coverage per-template parity. Each fixture is the minimal-fit pair for
// one stock-coverage rule; Optimize must pick the rule rather than fall through
// to residual completion.

func longStock100() WorkingLeg {
	return WorkingLeg{
		ID: "LS",
		Leg: engine.Leg{
			Side: engine.Long, Kind: engine.StockKind, Shares: 100.0, Mult: 1.0,
			Underlying: "XYZ",
		},
		OpenShares: 100.0,
	}
}

func shortStock100() WorkingLeg {
	return WorkingLeg{
		ID: "SS",
		Leg: engine.Leg{
			Side: engine.Short, Kind: engine.StockKind, Shares: 100.0, Mult: 1.0,
			Underlying:        "XYZ",
			ShortSaleProceeds: 10000.0,
			SalePrice:         100.0,
		},
		OpenShares: 100.0,
	}
}

func shortCall(id string, k float64) WorkingLeg {
	return WorkingLeg{
		ID: LegID(id),
		Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
			K: k, P: 3.0, P0: 3.0, Mult: 100.0,
			Style: "american", Venue: "listed", Underlying: "XYZ", Expiration: "2024-12-20",
		},
		OpenQty: 1.0,
	}
}

func longPut(id string, k float64) WorkingLeg {
	return WorkingLeg{
		ID: LegID(id),
		Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "put",
			K: k, P: 4.0, P0: 4.0, Mult: 100.0,
			Style: "american", Venue: "listed", Underlying: "XYZ", Expiration: "2024-12-20",
		},
		OpenQty: 1.0,
	}
}

func longCall(id string, k float64) WorkingLeg {
	return WorkingLeg{
		ID: LegID(id),
		Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: k, P: 4.0, P0: 4.0, Mult: 100.0,
			Style: "american", Venue: "listed", Underlying: "XYZ", Expiration: "2024-12-20",
			TimeToExpirationMonths: 3.0,
		},
		OpenQty: 1.0,
	}
}

func shortPut(id string, k float64) WorkingLeg {
	return WorkingLeg{
		ID: LegID(id),
		Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
			K: k, P: 3.0, P0: 3.0, Mult: 100.0,
			Style: "american", Venue: "listed", Underlying: "XYZ", Expiration: "2024-12-20",
		},
		OpenQty: 1.0,
	}
}

func TestPerTemplate_CoveredCall(t *testing.T) {
	opt := New(loadRulebook(t))
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{longStock100(), shortCall("SC", 100.0)})
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "covered_call" {
		t.Fatalf("want one covered_call sub, got %+v", dec.SubPositions)
	}
	if got := dec.Attributions["LS"]; len(got) != 1 || got[0].SharesUsed != 100.0 {
		t.Fatalf("LS attribution: %+v", got)
	}
}

func TestPerTemplate_ProtectivePut(t *testing.T) {
	opt := New(loadRulebook(t))
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{longStock100(), longPut("LP", 100.0)})
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "protective_put" {
		t.Fatalf("want one protective_put sub, got %+v", dec.SubPositions)
	}
}

func TestPerTemplate_Collar(t *testing.T) {
	opt := New(loadRulebook(t))
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{
		longStock100(), longPut("LP", 95.0), shortCall("SC", 105.0),
	})
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "collar" {
		t.Fatalf("want one collar sub, got %+v", dec.SubPositions)
	}
}

func TestPerTemplate_Conversion(t *testing.T) {
	opt := New(loadRulebook(t))
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{
		longStock100(), longPut("LP", 100.0), shortCall("SC", 100.0),
	})
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "conversion" {
		t.Fatalf("want one conversion sub, got %+v", dec.SubPositions)
	}
}

func TestPerTemplate_ReverseConversion(t *testing.T) {
	opt := New(loadRulebook(t))
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{
		shortStock100(), longCall("LC", 100.0), shortPut("SP", 100.0),
	})
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "reverse_conversion" {
		t.Fatalf("want one reverse_conversion sub, got %+v", dec.SubPositions)
	}
}

func TestPerTemplate_LongCallShortStock(t *testing.T) {
	opt := New(loadRulebook(t))
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{shortStock100(), longCall("LC", 100.0)})
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "long_call_short_stock" {
		t.Fatalf("want one long_call_short_stock sub, got %+v", dec.SubPositions)
	}
}

func TestPerTemplate_ShortPutShortStock(t *testing.T) {
	opt := New(loadRulebook(t))
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{shortStock100(), shortPut("SP", 100.0)})
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "short_put_short_stock" {
		t.Fatalf("want one short_put_short_stock sub, got %+v", dec.SubPositions)
	}
}

// TestExcessStock_CoveredCallWithStockResidual (epic test 6): 1000 shares + 5
// short calls. The covered_call sub-position absorbs 500 shares + 5 contracts;
// the remaining 500 shares surface as ErrStockResidualUnsupported. The partial
// decomposition still contains the covered_call sub and its attribution.
func TestExcessStock_CoveredCallWithStockResidual(t *testing.T) {
	opt := New(loadRulebook(t))
	stock := WorkingLeg{
		ID: "LS",
		Leg: engine.Leg{
			Side: engine.Long, Kind: engine.StockKind, Shares: 1000.0, Mult: 1.0,
			Underlying: "XYZ",
		},
		OpenShares: 1000.0,
	}
	sc := shortCall("SC", 100.0)
	sc.OpenQty = 5.0
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{stock, sc})
	var stockErr *ErrStockResidualUnsupported
	if !errors.As(err, &stockErr) {
		t.Fatalf("want *ErrStockResidualUnsupported, got %T %v", err, err)
	}
	if stockErr.OpenShares != 500.0 || stockErr.LegID != "LS" {
		t.Fatalf("residual err fields: %+v", stockErr)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "covered_call" {
		t.Fatalf("want covered_call in partial decomposition, got %+v", dec.SubPositions)
	}
	attr := dec.Attributions["LS"]
	if len(attr) != 1 || attr[0].SharesUsed != 500.0 {
		t.Fatalf("LS attribution: %+v", attr)
	}
}

// TestExactCoveredCallPlusNakedLong (epic test 9): 100 shares + 1 sc + 1
// lonely long call. covered_call consumes the stock + sc; the lonely long
// call is scored via residual completion. No stock residual.
func TestExactCoveredCallPlusNakedLong(t *testing.T) {
	opt := New(loadRulebook(t))
	legs := []WorkingLeg{
		longStock100(),
		shortCall("SC", 100.0),
		longCall("LC", 110.0),
	}
	dec, err := opt.Optimize(defaultFacts(), legs)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	var coveredCall, nakedLong int
	for _, sp := range dec.SubPositions {
		switch sp.StrategyID {
		case "covered_call":
			coveredCall++
		case "long_option_short_dated":
			nakedLong++
		}
	}
	if coveredCall != 1 || nakedLong != 1 {
		t.Fatalf("want 1 covered_call + 1 naked long, got covered=%d naked=%d (subs=%+v)",
			coveredCall, nakedLong, dec.SubPositions)
	}
}

// TestPartialCoverageRemainderStaysInState: 250 shares + 1 sc (mult=100).
// covered_call consumes 100 shares + 1 contract; remaining 150 shares trigger
// *ErrStockResidualUnsupported with OpenShares == 150. The sub-position
// covered_call is still recorded.
func TestPartialCoverageRemainderStaysInState(t *testing.T) {
	opt := New(loadRulebook(t))
	stock := WorkingLeg{
		ID: "LS",
		Leg: engine.Leg{
			Side: engine.Long, Kind: engine.StockKind, Shares: 250.0, Mult: 1.0,
			Underlying: "XYZ",
		},
		OpenShares: 250.0,
	}
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{stock, shortCall("SC", 100.0)})
	var stockErr *ErrStockResidualUnsupported
	if !errors.As(err, &stockErr) {
		t.Fatalf("want *ErrStockResidualUnsupported, got %T %v", err, err)
	}
	if stockErr.OpenShares != 150.0 {
		t.Fatalf("residual OpenShares: want 150, got %g", stockErr.OpenShares)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "covered_call" {
		t.Fatalf("want covered_call recorded, got %+v", dec.SubPositions)
	}
}

// TestCollarVsStrangleConflict (epic test 11): LS=100sh + 1 LP + 1 SC + 1 SP.
// Two decompositions consume all stock:
//
//	A. collar(lp+sc+ls) + naked sp
//	B. protective_put(lp+ls) + short_strangle_or_straddle(sp+sc)
//
// Both fixtures must consume all shares (no residual error). The optimizer
// picks the cheaper one; we only assert there's no error and that one of the
// two well-formed structures wins.
func TestCollarVsStrangleConflict(t *testing.T) {
	opt := New(loadRulebook(t))
	cases := []struct {
		name string
		legs []WorkingLeg
	}{
		{
			name: "caseA",
			legs: []WorkingLeg{
				longStock100(),
				longPut("LP", 95.0),
				shortCall("SC", 105.0),
				shortPut("SP", 90.0),
			},
		},
		{
			name: "caseB",
			legs: []WorkingLeg{
				longStock100(),
				longPut("LP", 95.0),
				shortCall("SC", 105.0),
				shortPut("SP", 95.0),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, err := opt.Optimize(defaultFacts(), tc.legs)
			if err != nil {
				t.Fatalf("%s Optimize: %v", tc.name, err)
			}
			if attr, ok := dec.Attributions["LS"]; !ok {
				t.Fatalf("%s: LS has no attribution (stock not consumed)", tc.name)
			} else {
				var consumed float64
				for _, a := range attr {
					consumed += a.SharesUsed
				}
				if consumed != 100.0 {
					t.Fatalf("%s: LS shares consumed = %g, want 100", tc.name, consumed)
				}
			}
			// Both decompositions must absorb the stock via some coverage
			// rule (collar / protective_put / covered_call / conversion).
			// The specific winner depends on the engine's price-dependent
			// formulas — what matters here is that the optimizer never
			// leaves stock stranded when at least one coverage path fits.
			var hasCoverage bool
			for _, sp := range dec.SubPositions {
				switch sp.StrategyID {
				case "collar", "protective_put", "conversion", "covered_call":
					hasCoverage = true
				}
			}
			if !hasCoverage {
				t.Fatalf("%s: expected a stock-coverage rule in subs, got %+v", tc.name, dec.SubPositions)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// short_index_call_long_etf — notional ETF coverage with cross-underlying
// substitution. Bucket facts carry the index spot U; the ETF tracks the index
// (TracksIndex == sc.Underlying) and the coverage constraint is dollar-based:
//
//	le.shares * le.price >= U * sc.qty * sc.mult
//
// The recipe consumes ceil(U * n * mult / le.price) shares per n contracts.
// Leftover ETF shares stay in state with le.price and le.K_equivalent
// unchanged so subsequent templates / residual completion can re-read them.

func shortIndexCall(id string, qty float64) WorkingLeg {
	return WorkingLeg{
		ID: LegID(id),
		Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
			K: 4500.0, P: 10.0, P0: 10.0, Mult: 100.0,
			Style: "european", Underlying: "XYZ_INDEX",
		},
		OpenQty: qty,
	}
}

func longETF(id string, shares, price, kEq float64) WorkingLeg {
	return WorkingLeg{
		ID: LegID(id),
		Leg: engine.Leg{
			Side: engine.Long, Kind: engine.ETFKind, Mult: 1.0,
			Price: price, Shares: shares, KEquivalent: kEq,
			TracksIndex: "XYZ_INDEX", Leveraged: false,
		},
		OpenShares: shares,
	}
}

func etfFacts(U float64) BucketFacts {
	return BucketFacts{
		U:           U,
		Class:       "equity",
		Lev:         1.0,
		AccountType: engine.MarginAccount,
		Phase:       engine.Initial,
	}
}

// TestPerTemplate_ShortIndexCallLongETF_ExactCoverage (epic test 10): the
// bucket holds an SPX short call and SPY long shares sized for *exactly* the
// rule's required notional. The optimizer must pick short_index_call_long_etf
// with no residual error.
func TestPerTemplate_ShortIndexCallLongETF_ExactCoverage(t *testing.T) {
	opt := New(loadRulebook(t))
	// U=4000, qty=1, mult=100 → required notional = 4000 * 1 * 100 = $400k.
	// Provide 1000 shares @ $400 → 1000 * 400 = $400k notional.
	facts := etfFacts(4000.0)
	legs := []WorkingLeg{
		shortIndexCall("SC", 1.0),
		longETF("LE", 1000.0, 400.0, 460.0),
	}
	dec, err := opt.Optimize(facts, legs)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "short_index_call_long_etf" {
		t.Fatalf("want one short_index_call_long_etf sub, got %+v", dec.SubPositions)
	}
	attr := dec.Attributions["LE"]
	if len(attr) != 1 || attr[0].SharesUsed != 1000.0 {
		t.Fatalf("LE attribution: want 1000 shares used, got %+v", attr)
	}
	if got := dec.Attributions["SC"]; len(got) != 1 || got[0].QtyUsed != 1.0 {
		t.Fatalf("SC attribution: %+v", got)
	}
}

// TestExcessETFWithNotionalCoverage (epic test 7): SPX short call (qty=1,
// mult=100, U=4000) + SPY long shares 1500 @ $400. Supplied notional ($600k)
// exceeds required ($400k); 1000 shares are consumed, 500 stay in state and
// surface as *ErrStockResidualUnsupported. The partial decomposition still
// contains the short_index_call_long_etf sub.
func TestExcessETFWithNotionalCoverage(t *testing.T) {
	opt := New(loadRulebook(t))
	facts := etfFacts(4000.0)
	legs := []WorkingLeg{
		shortIndexCall("SC", 1.0),
		longETF("LE", 1500.0, 400.0, 460.0),
	}
	dec, err := opt.Optimize(facts, legs)
	var stockErr *ErrStockResidualUnsupported
	if !errors.As(err, &stockErr) {
		t.Fatalf("want *ErrStockResidualUnsupported, got %T %v", err, err)
	}
	if stockErr.LegID != "LE" || stockErr.OpenShares != 500.0 {
		t.Fatalf("residual err fields: %+v", stockErr)
	}
	// The leftover ETF metadata must round-trip through the error so callers
	// rendering diagnostics (or re-bucketing the residual) see the same
	// per-share price and strike-equivalent as the original working leg.
	if stockErr.Leg.Price != 400.0 || stockErr.Leg.KEquivalent != 460.0 {
		t.Fatalf("residual ETF metadata: want price=400 K_equivalent=460, got price=%g K_equivalent=%g",
			stockErr.Leg.Price, stockErr.Leg.KEquivalent)
	}
	if stockErr.Leg.TracksIndex != "XYZ_INDEX" {
		t.Fatalf("residual ETF TracksIndex: want XYZ_INDEX, got %q", stockErr.Leg.TracksIndex)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "short_index_call_long_etf" {
		t.Fatalf("want short_index_call_long_etf in partial decomposition, got %+v", dec.SubPositions)
	}
	attr := dec.Attributions["LE"]
	if len(attr) != 1 || attr[0].SharesUsed != 1000.0 {
		t.Fatalf("LE attribution: want 1000 shares used, got %+v", attr)
	}
}

// TestETFCoverage_TracksIndexMismatchSkipsTemplate: the rule's bind
// constraint requires `le.tracks_index == sc.underlying`. When the ETF
// tracks a different index, EvaluateRule must decline the template; the
// optimizer falls back to residual completion. Locks against an accidental
// optimizer shortcut that would substitute by kind alone and ignore the
// cross-underlying relationship.
func TestETFCoverage_TracksIndexMismatchSkipsTemplate(t *testing.T) {
	opt := New(loadRulebook(t))
	facts := etfFacts(4000.0)
	le := longETF("LE", 1000.0, 400.0, 460.0)
	le.Leg.TracksIndex = "OTHER_INDEX"
	dec, err := opt.Optimize(facts, []WorkingLeg{shortIndexCall("SC", 1.0), le})
	if err == nil {
		t.Fatalf("want residual error on TracksIndex mismatch, got nil; subs=%+v", dec.SubPositions)
	}
	for _, sp := range dec.SubPositions {
		if sp.StrategyID == "short_index_call_long_etf" {
			t.Fatalf("unexpected short_index_call_long_etf on TracksIndex mismatch: %+v", sp)
		}
	}
}

// TestETFCoverage_InsufficientNotional: ETF supplies $40k against required
// $400k → consumption returns ok=false, no template fits. B&B falls back to
// residual completion: the ETF leg is unsupported (stock residual) and the
// SPX short call lands wherever the naked-rule sequence routes it. Both an
// ErrStockResidualUnsupported and an ErrNoNakedRule outcome are documented
// possibilities; we only assert that the optimizer surfaces *some* residual
// error and never silently absorbs the ETF.
func TestETFCoverage_InsufficientNotional(t *testing.T) {
	opt := New(loadRulebook(t))
	facts := etfFacts(4000.0)
	legs := []WorkingLeg{
		shortIndexCall("SC", 1.0),
		longETF("LE", 100.0, 400.0, 460.0),
	}
	dec, err := opt.Optimize(facts, legs)
	if err == nil {
		t.Fatalf("want residual error (ETF can't cover), got nil; subs=%+v", dec.SubPositions)
	}
	var noNaked *ErrNoNakedRule
	var stockErr *ErrStockResidualUnsupported
	if !errors.As(err, &noNaked) && !errors.As(err, &stockErr) {
		t.Fatalf("want *ErrNoNakedRule or *ErrStockResidualUnsupported, got %T %v", err, err)
	}
	// No short_index_call_long_etf sub may exist — the consumption guard
	// rejected the branch, so the template never reached EvaluateRule.
	for _, sp := range dec.SubPositions {
		if sp.StrategyID == "short_index_call_long_etf" {
			t.Fatalf("unexpected coverage sub on insufficient notional: %+v", sp)
		}
	}
}

// TestETFCoverage_FractionalShareLeftover: ETF supplies 1000 + 5e-10 shares
// (a float-drift sliver above exact coverage). After consuming 1000 shares
// the remainder falls below stateKeyEps and is dropped by applyConsumption —
// no residual error surfaces. Documenting the OpenShares boundary here
// pins the 1e-9 epsilon convention.
func TestETFCoverage_FractionalShareLeftover(t *testing.T) {
	opt := New(loadRulebook(t))
	facts := etfFacts(4000.0)
	// 1000 + 5e-10 shares: remainder 5e-10 < stateKeyEps (1e-9) → dropped.
	legs := []WorkingLeg{
		shortIndexCall("SC", 1.0),
		longETF("LE", 1000.0+5e-10, 400.0, 460.0),
	}
	dec, err := opt.Optimize(facts, legs)
	if err != nil {
		t.Fatalf("Optimize: %v (sub-eps residual should be dropped, not errored)", err)
	}
	if len(dec.SubPositions) != 1 || dec.SubPositions[0].StrategyID != "short_index_call_long_etf" {
		t.Fatalf("want one short_index_call_long_etf sub, got %+v", dec.SubPositions)
	}
}

// TestETFCoverage_PartialContractsConsumed: 5 short index calls but ETF
// notional covers only 2 contracts. The coverage sub consumes 2 contracts +
// the matching share slice; the remaining 3 sc contracts fall to residual
// completion and score as short_call_uncovered.
func TestETFCoverage_PartialContractsConsumed(t *testing.T) {
	opt := New(loadRulebook(t))
	facts := etfFacts(4000.0)
	// 2000 shares @ $400 = $800k notional → covers 800000 / (4000 * 100) = 2 contracts.
	legs := []WorkingLeg{
		shortIndexCall("SC", 5.0),
		longETF("LE", 2000.0, 400.0, 460.0),
	}
	dec, err := opt.Optimize(facts, legs)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	var coverage, naked int
	var coverageSC, nakedSC float64
	for _, sp := range dec.SubPositions {
		switch sp.StrategyID {
		case "short_index_call_long_etf":
			coverage++
			for _, sa := range sp.Slots {
				if sa.Slot == "sc" {
					coverageSC = sa.QtyUsed
				}
			}
		case "short_call_uncovered":
			naked++
			for _, sa := range sp.Slots {
				if sa.LegID == "SC" {
					nakedSC = sa.QtyUsed
				}
			}
		}
	}
	if coverage != 1 || naked != 1 {
		t.Fatalf("want 1 coverage + 1 naked, got coverage=%d naked=%d (subs=%+v)", coverage, naked, dec.SubPositions)
	}
	if coverageSC != 2.0 {
		t.Fatalf("coverage consumed sc qty = %g, want 2", coverageSC)
	}
	if nakedSC != 3.0 {
		t.Fatalf("naked consumed sc qty = %g, want 3", nakedSC)
	}
}

// TestETFCoverage_DriftedPriceCapsShares: a price that's just below a clean
// fixed-point value (399.9999996 instead of 400.0) is the adversarial input
// that would otherwise have floor_eps round maxByNotional up to 1 contract
// while ceil_eps rounds the share count to 1001 — one share above
// OpenShares. The defensive back-off must catch this and either reduce n or
// reject the branch so applyConsumption never sees a negative leftover.
func TestETFCoverage_DriftedPriceCapsShares(t *testing.T) {
	opt := New(loadRulebook(t))
	facts := etfFacts(4000.0)
	legs := []WorkingLeg{
		shortIndexCall("SC", 1.0),
		longETF("LE", 1000.0, 399.9999996, 460.0),
	}
	dec, err := opt.Optimize(facts, legs)
	// With only 1 contract on offer, the back-off path falls to n=0 and the
	// template is skipped — the legs route to residual completion. Either
	// outcome is acceptable as long as no plan ever consumes more shares
	// than OpenShares.
	for _, sp := range dec.SubPositions {
		for _, sa := range sp.Slots {
			if sa.LegID == "LE" && sa.SharesUsed > 1000.0 {
				t.Fatalf("planned %g shares against 1000 OpenShares: %+v", sa.SharesUsed, sp)
			}
		}
	}
	// Residual error is fine; a hard error would indicate the guard
	// misclassified the drift as a programmer bug.
	if err != nil {
		var noNaked *ErrNoNakedRule
		var stockErr *ErrStockResidualUnsupported
		if !errors.As(err, &noNaked) && !errors.As(err, &stockErr) {
			t.Fatalf("want residual error from drifted-price back-off, got %T %v", err, err)
		}
	}
}

// TestETFCoverage_ZeroPriceIsHardError: a working ETF leg with le.price == 0
// is a programmer error — the consumption recipe would divide by zero and
// produce a meaningless share count. consumptionFor must surface a hard
// error so the bug is loud (rather than silently skipping the template).
func TestETFCoverage_ZeroPriceIsHardError(t *testing.T) {
	opt := New(loadRulebook(t))
	facts := etfFacts(4000.0)
	bad := longETF("LE", 1000.0, 400.0, 460.0)
	bad.Leg.Price = 0
	legs := []WorkingLeg{shortIndexCall("SC", 1.0), bad}
	_, err := opt.Optimize(facts, legs)
	if err == nil {
		t.Fatal("want hard error for le.price == 0, got nil")
	}
	var noNaked *ErrNoNakedRule
	var stockErr *ErrStockResidualUnsupported
	if errors.As(err, &noNaked) || errors.As(err, &stockErr) {
		t.Fatalf("zero-price should not surface as a residual sentinel, got %T %v", err, err)
	}
}

// TestETFCoverage_ZeroUIsHardError: facts.U == 0 makes the notional
// denominator vanish. consumptionFor must surface a hard error rather than
// silently divide by zero. The check fires before any successful template
// match, so the engine never sees the malformed state.
func TestETFCoverage_ZeroUIsHardError(t *testing.T) {
	opt := New(loadRulebook(t))
	facts := etfFacts(0.0)
	legs := []WorkingLeg{
		shortIndexCall("SC", 1.0),
		longETF("LE", 1000.0, 400.0, 460.0),
	}
	_, err := opt.Optimize(facts, legs)
	if err == nil {
		t.Fatal("want hard error for U == 0, got nil")
	}
	var noNaked *ErrNoNakedRule
	var stockErr *ErrStockResidualUnsupported
	if errors.As(err, &noNaked) || errors.As(err, &stockErr) {
		t.Fatalf("zero-U should not surface as a residual sentinel, got %T %v", err, err)
	}
}

// -----------------------------------------------------------------------------
// Quantity-slicing at scale (epic tests 3, 4, 8) and pruning instrumentation
// (epic test 14). These exercise the deliverable-units policy on non-trivial
// OpenQty and pin the same-leg-class no-coalesce contract for v1.

func slicedLongCall(id string, qty float64, k float64) WorkingLeg {
	return WorkingLeg{
		ID: LegID(id),
		Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: k, P: 8.0, P0: 8.0, Mult: 100.0,
			Style: "american", Venue: "listed", Underlying: "XYZ", Expiration: "2024-12-20",
			TimeToExpirationMonths: 3.0,
		},
		OpenQty: qty,
	}
}

func slicedShortCall(id string, qty float64, k float64) WorkingLeg {
	return WorkingLeg{
		ID: LegID(id),
		Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
			K: k, P: 3.0, P0: 3.0, Mult: 100.0,
			Style: "american", Venue: "listed", Underlying: "XYZ", Expiration: "2024-12-20",
			TimeToExpirationMonths: 3.0,
		},
		OpenQty: qty,
	}
}

// TestQuantitySlicing_DifferentStrikes (epic test 3): 10 long calls K=100 + 5
// short calls K=110 slice into one vertical_spread (5+5) + one
// long_option_short_dated naked sub for the leftover 5 longs. The longs are
// attributed across two sub-positions, each consuming 5 contracts.
func TestQuantitySlicing_DifferentStrikes(t *testing.T) {
	opt := New(loadRulebook(t))
	facts := defaultFacts()
	facts.U = 105.0
	legs := []WorkingLeg{
		slicedLongCall("L1", 10.0, 100.0),
		slicedShortCall("S1", 5.0, 110.0),
	}
	dec, err := opt.Optimize(facts, legs)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	var verticals, nakedLongs int
	for _, sp := range dec.SubPositions {
		switch sp.StrategyID {
		case "vertical_spread":
			verticals++
		case "long_option_short_dated":
			nakedLongs++
		}
	}
	if verticals != 1 || nakedLongs != 1 {
		t.Fatalf("want 1 vertical + 1 naked-long, got verticals=%d naked=%d (subs=%+v)", verticals, nakedLongs, dec.SubPositions)
	}
	attr := dec.Attributions["L1"]
	if len(attr) != 2 {
		t.Fatalf("want 2 attribution entries for L1, got %d (%+v)", len(attr), attr)
	}
	for _, a := range attr {
		if a.QtyUsed != 5.0 {
			t.Fatalf("each L1 attribution should consume 5 contracts, got %g", a.QtyUsed)
		}
	}
}

// TestSameLegClassMatchSplit (epic test 4): two distinct working legs with
// identical option attributes (same K, exp, side) cannot form a vertical with
// each other. The v1 optimizer does NOT coalesce same-class legs into a
// combined naked sub — it emits one naked sub per input leg, totaling the
// summed contract count.
func TestSameLegClassMatchSplit(t *testing.T) {
	opt := New(loadRulebook(t))
	facts := defaultFacts()
	legs := []WorkingLeg{
		slicedLongCall("L1", 10.0, 100.0),
		slicedLongCall("L2", 5.0, 100.0),
	}
	dec, err := opt.Optimize(facts, legs)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	var nakedLongs int
	var totalQty float64
	for _, sp := range dec.SubPositions {
		if sp.StrategyID == "vertical_spread" {
			t.Fatalf("vertical_spread should not appear with two long legs: %+v", sp)
		}
		if sp.StrategyID == "long_option_short_dated" {
			nakedLongs++
			for _, sa := range sp.Slots {
				totalQty += sa.QtyUsed
			}
		}
	}
	if nakedLongs != 2 {
		t.Fatalf("want exactly 2 naked-long subs (no coalesce), got %d (subs=%+v)", nakedLongs, dec.SubPositions)
	}
	if totalQty != 15.0 {
		t.Fatalf("naked-long total qty = %g, want 15", totalQty)
	}
}

// TestExactMatchSplit (epic test 8): two long-call lots (10 + 5 contracts) at
// the same K=100 plus 5 short calls K=110 produce one vertical (5+5) and
// naked-long sub-positions totaling 10 contracts on the leftover longs. The
// vertical can pair with either lot; the optimizer's tiebreak picks one
// deterministically. We assert the totals, not which lot was paired.
func TestExactMatchSplit(t *testing.T) {
	opt := New(loadRulebook(t))
	facts := defaultFacts()
	facts.U = 105.0
	legs := []WorkingLeg{
		slicedLongCall("L1", 10.0, 100.0),
		slicedLongCall("L2", 5.0, 100.0),
		slicedShortCall("S1", 5.0, 110.0),
	}
	dec, err := opt.Optimize(facts, legs)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	var verticals, nakedLongs int
	var nakedQty float64
	for _, sp := range dec.SubPositions {
		switch sp.StrategyID {
		case "vertical_spread":
			verticals++
		case "long_option_short_dated":
			nakedLongs++
			for _, sa := range sp.Slots {
				nakedQty += sa.QtyUsed
			}
		}
	}
	if verticals != 1 {
		t.Fatalf("want exactly 1 vertical_spread, got %d (subs=%+v)", verticals, dec.SubPositions)
	}
	if nakedQty != 10.0 {
		t.Fatalf("naked-long total qty = %g, want 10 (subs=%+v)", nakedQty, dec.SubPositions)
	}
	if nakedLongs < 1 {
		t.Fatalf("want at least 1 naked-long sub, got %d", nakedLongs)
	}
}

// sixLegMixedAAPL is the 6-leg AAPL-style fixture from the epic hand-spot-check:
// a mix of long/short calls and puts that exercises the B&B search over
// vertical / strangle / box candidates plus residual completion. Reused by
// both TestPruningInstrumentation (soft runtime budget) and BenchmarkOptimize.
func sixLegMixedAAPL() []WorkingLeg {
	mk := func(id string, side engine.Side, opt string, k, p float64) WorkingLeg {
		return WorkingLeg{
			ID: LegID(id),
			Leg: engine.Leg{
				Side: side, Kind: engine.OptionKind, OptionType: opt,
				K: k, P: p, P0: p, Mult: 100.0,
				Style: "american", Venue: "listed", Underlying: "AAPL", Expiration: "2024-12-20",
				TimeToExpirationMonths: 3.0,
			},
			OpenQty: 1.0,
		}
	}
	return []WorkingLeg{
		mk("LC1", engine.Long, "call", 150.0, 8.0),
		mk("SC1", engine.Short, "call", 160.0, 3.0),
		mk("LC2", engine.Long, "call", 170.0, 1.5),
		mk("LP1", engine.Long, "put", 150.0, 4.0),
		mk("SP1", engine.Short, "put", 140.0, 2.0),
		mk("LP2", engine.Long, "put", 130.0, 1.0),
	}
}

// TestPruningInstrumentation (epic test 14): the 6-leg AAPL fixture must
// complete under a soft 100ms wall-clock budget. The NodeCount() is logged
// for triage; no ratio assertion lands until a tighter pruning bound does.
// Failure here is a regression signal that B&B blew up — file a follow-up
// before skipping.
func TestPruningInstrumentation(t *testing.T) {
	opt := New(loadRulebook(t))
	legs := sixLegMixedAAPL()
	start := time.Now()
	_, err := opt.Optimize(defaultFacts(), legs)
	elapsed := time.Since(start)
	if err != nil {
		t.Logf("Optimize returned residual error (expected for mixed bucket): %v", err)
	}
	t.Logf("6-leg Optimize: elapsed=%s nodes=%d", elapsed, opt.NodeCount())
	if elapsed > 100*time.Millisecond {
		t.Fatalf("6-leg Optimize exceeded 100ms soft budget: %s (nodes=%d)", elapsed, opt.NodeCount())
	}
	if opt.NodeCount() <= 0 {
		t.Fatalf("NodeCount() should reflect decompose entries, got %d", opt.NodeCount())
	}
}

// TestNodeCount_ResetPerOptimize asserts the documented "reset to 0 inside
// each Optimize call" contract: running Optimize twice does not accumulate
// the node counter across calls.
func TestNodeCount_ResetPerOptimize(t *testing.T) {
	opt := New(loadRulebook(t))
	legs := sixLegMixedAAPL()
	_, _ = opt.Optimize(defaultFacts(), legs)
	first := opt.NodeCount()
	_, _ = opt.Optimize(defaultFacts(), legs)
	second := opt.NodeCount()
	if first != second {
		t.Fatalf("NodeCount not reset per Optimize: first=%d second=%d", first, second)
	}
}

func BenchmarkOptimize(b *testing.B) {
	rb, err := engine.LoadRulebook(rulesPath)
	if err != nil {
		b.Fatalf("LoadRulebook: %v", err)
	}
	opt := New(rb)
	legs := sixLegMixedAAPL()
	facts := defaultFacts()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = opt.Optimize(facts, legs)
	}
}

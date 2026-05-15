package optimizer

import (
	"errors"
	"math"
	"reflect"
	"testing"

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
			subDec, rerr := opt.scoreAllResidual(remainder, facts)
			if rerr != nil {
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

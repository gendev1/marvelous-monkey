package optimizer

import (
	"errors"
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
		U:           100,
		Class:       "equity",
		Lev:         1,
		AccountType: engine.MarginAccount,
		Phase:       engine.Initial,
	}
}

func longCallShortDated() engine.Leg {
	return engine.Leg{
		Side:                   engine.Long,
		Kind:                   engine.OptionKind,
		OptionType:             "call",
		K:                      100,
		P:                      3,
		P0:                     3,
		Mult:                   100,
		TimeToExpirationMonths: 3,
	}
}

func TestNakedScoring_LongOptionShortDated(t *testing.T) {
	opt := New(loadRulebook(t))
	wl := WorkingLeg{ID: "L1", Leg: longCallShortDated(), OpenQty: 1}
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
	if len(attr) != 1 || attr[0].SubIndex != 0 || attr[0].QtyUsed != 1 {
		t.Fatalf("attribution: %+v", attr)
	}
}

func TestNakedScoring_LongOptionLongDatedListed(t *testing.T) {
	opt := New(loadRulebook(t))
	leg := engine.Leg{
		Side:                   engine.Long,
		Kind:                   engine.OptionKind,
		OptionType:             "put",
		K:                      100,
		P:                      5,
		P0:                     5,
		Mult:                   100,
		Venue:                  "listed",
		TimeToExpirationMonths: 12,
	}
	wl := WorkingLeg{ID: "L1", Leg: leg, OpenQty: 1}
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
		K:                      100,
		P:                      5,
		P0:                     5,
		Mult:                   100,
		Venue:                  "otc",
		Style:                  "american",
		BrokerGuaranteed:       true,
		TimeToExpirationMonths: 12,
	}
	wl := WorkingLeg{ID: "L1", Leg: leg, OpenQty: 1}
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
		K:          100,
		P:          3,
		P0:         3,
		Mult:       100,
	}
	wl := WorkingLeg{ID: "L1", Leg: leg, OpenQty: 1}
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
		K:          100,
		P:          3,
		P0:         3,
		Mult:       100,
	}
	wl := WorkingLeg{ID: "L1", Leg: leg, OpenQty: 1}
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
		{ID: "A", Leg: longCallShortDated(), OpenQty: 1},
		{ID: "B", Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
			K: 100, P: 3, P0: 3, Mult: 100,
		}, OpenQty: 2},
		{ID: "C", Leg: engine.Leg{
			Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
			K: 100, P: 3, P0: 3, Mult: 100,
		}, OpenQty: 3},
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
			Side: engine.Long, Kind: engine.StockKind, Shares: 100, Mult: 1,
		},
		OpenShares: 100,
	}
	dec, err := opt.Optimize(defaultFacts(), []WorkingLeg{wl})
	var stockErr *ErrStockResidualUnsupported
	if !errors.As(err, &stockErr) {
		t.Fatalf("want *ErrStockResidualUnsupported, got %T %v", err, err)
	}
	if stockErr.LegID != "S1" || stockErr.OpenShares != 100 {
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
		K:                      100,
		P:                      5,
		P0:                     5,
		Mult:                   100,
		TimeToExpirationMonths: 12, // > 9 month threshold
		// Venue intentionally empty: neither listed nor otc rule binds.
	}
	wl := WorkingLeg{ID: "L1", Leg: leg, OpenQty: 1}
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
			Side: engine.Long, Kind: engine.StockKind, Shares: 100, Mult: 1,
		},
		OpenShares: 100,
	}
	noRule := WorkingLeg{
		ID: "L1",
		Leg: engine.Leg{
			Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
			K: 100, P: 5, P0: 5, Mult: 100, TimeToExpirationMonths: 12,
		},
		OpenQty: 1,
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

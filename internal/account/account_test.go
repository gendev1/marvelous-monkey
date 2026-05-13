package account

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"margincalc/internal/engine"
)

func longStockPosition() AccountPosition {
	return AccountPosition{
		ID: "p1",
		Position: engine.Position{
			U:     100,
			Class: "equity",
			Legs: []engine.Leg{{
				Side:   engine.Long,
				Kind:   engine.StockKind,
				Shares: 100,
			}},
		},
	}
}

func minimalAccount() Account {
	return Account{
		ID:          "acct-1",
		AccountType: engine.MarginAccount,
		Phase:       engine.Initial,
		Positions:   []AccountPosition{longStockPosition()},
	}
}

func assertInvalidAccount(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.HasPrefix(err.Error(), "invalid account:") {
		t.Fatalf("expected error prefixed with %q, got: %v", "invalid account:", err)
	}
}

func TestValidate_acceptsMinimal(t *testing.T) {
	if err := validate(minimalAccount()); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidate_rejectsMissingID(t *testing.T) {
	a := minimalAccount()
	a.ID = ""
	assertInvalidAccount(t, validate(a))
}

func TestValidate_rejectsBadAccountType(t *testing.T) {
	a := minimalAccount()
	a.AccountType = engine.AccountType("savings")
	assertInvalidAccount(t, validate(a))
}

func TestValidate_rejectsBadPhase(t *testing.T) {
	a := minimalAccount()
	a.Phase = engine.Phase("intraday")
	assertInvalidAccount(t, validate(a))
}

func TestValidate_rejectsNaNInf(t *testing.T) {
	fields := []struct {
		name string
		set  func(*Account, float64)
	}{
		{"SODEquity", func(a *Account, v float64) { a.SODEquity = v }},
		{"CashBalance", func(a *Account, v float64) { a.CashBalance = v }},
		{"PnL", func(a *Account, v float64) { a.PnL = v }},
		{"DepositsWithdrawals", func(a *Account, v float64) { a.DepositsWithdrawals = v }},
	}
	bads := []struct {
		name string
		val  float64
	}{
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
	}
	for _, f := range fields {
		for _, b := range bads {
			t.Run(f.name+"/"+b.name, func(t *testing.T) {
				a := minimalAccount()
				f.set(&a, b.val)
				assertInvalidAccount(t, validate(a))
			})
		}
	}
}

func TestValidate_rejectsDuplicatePositionIDs(t *testing.T) {
	a := minimalAccount()
	dup := longStockPosition()
	dup.ID = "p1"
	a.Positions = append(a.Positions, dup)
	assertInvalidAccount(t, validate(a))

	a2 := minimalAccount()
	a2.Positions[0].ID = ""
	empty := longStockPosition()
	empty.ID = ""
	a2.Positions = append(a2.Positions, empty)
	if err := validate(a2); err != nil {
		t.Fatalf("two empty position IDs should pass, got %v", err)
	}
}

func optionPosition(p, qty float64) AccountPosition {
	return AccountPosition{
		ID: "opt",
		Position: engine.Position{
			U:     100,
			Class: "equity",
			Legs: []engine.Leg{{
				Side:       engine.Long,
				Kind:       engine.OptionKind,
				OptionType: "call",
				P:          p,
				Qty:        qty,
				K:          100,
			}},
		},
	}
}

func TestValidate_rejectsBadOptionLeg(t *testing.T) {
	t.Run("P=-1 fails", func(t *testing.T) {
		a := minimalAccount()
		a.Positions = []AccountPosition{optionPosition(-1, 1)}
		assertInvalidAccount(t, validate(a))
	})
	t.Run("Qty=0 fails", func(t *testing.T) {
		a := minimalAccount()
		a.Positions = []AccountPosition{optionPosition(1, 0)}
		assertInvalidAccount(t, validate(a))
	})
	t.Run("P=0 passes", func(t *testing.T) {
		a := minimalAccount()
		a.Positions = []AccountPosition{optionPosition(0, 1)}
		if err := validate(a); err != nil {
			t.Fatalf("zero-priced option should pass, got %v", err)
		}
	})
	t.Run("P>0 Qty>0 passes", func(t *testing.T) {
		a := minimalAccount()
		a.Positions = []AccountPosition{optionPosition(1.25, 2)}
		if err := validate(a); err != nil {
			t.Fatalf("expected pass, got %v", err)
		}
	})
}

func TestValidate_rejectsBadStockLeg(t *testing.T) {
	t.Run("Shares=0 fails", func(t *testing.T) {
		a := minimalAccount()
		a.Positions[0].Position.Legs[0].Shares = 0
		assertInvalidAccount(t, validate(a))
	})
	t.Run("U=0 fails", func(t *testing.T) {
		a := minimalAccount()
		a.Positions[0].Position.U = 0
		assertInvalidAccount(t, validate(a))
	})
}

func stockLikePosition(kind engine.Kind, price, shares float64) AccountPosition {
	return AccountPosition{
		ID: string(kind),
		Position: engine.Position{
			U:     100,
			Class: "equity",
			Legs: []engine.Leg{{
				Side:   engine.Long,
				Kind:   kind,
				Price:  price,
				Shares: shares,
			}},
		},
	}
}

func TestValidate_rejectsBadStockLikeLeg(t *testing.T) {
	kinds := []engine.Kind{engine.ETFKind, engine.ETNKind, engine.ConvertibleKind, engine.WarrantKind}
	for _, k := range kinds {
		t.Run(string(k)+"/Price=0", func(t *testing.T) {
			a := minimalAccount()
			a.Positions = []AccountPosition{stockLikePosition(k, 0, 10)}
			assertInvalidAccount(t, validate(a))
		})
		t.Run(string(k)+"/Shares=0", func(t *testing.T) {
			a := minimalAccount()
			a.Positions = []AccountPosition{stockLikePosition(k, 50, 0)}
			assertInvalidAccount(t, validate(a))
		})
	}
}

func TestValidate_rejectsNonFiniteLegInputs(t *testing.T) {
	nonFinite := []struct {
		name string
		val  float64
	}{
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
	}
	for _, nf := range nonFinite {
		t.Run("option/P/"+nf.name, func(t *testing.T) {
			a := minimalAccount()
			a.Positions = []AccountPosition{optionPosition(nf.val, 1)}
			assertInvalidAccount(t, validate(a))
		})
		t.Run("option/Qty/"+nf.name, func(t *testing.T) {
			a := minimalAccount()
			a.Positions = []AccountPosition{optionPosition(1, nf.val)}
			assertInvalidAccount(t, validate(a))
		})
		t.Run("stock/Shares/"+nf.name, func(t *testing.T) {
			a := minimalAccount()
			a.Positions[0].Position.Legs[0].Shares = nf.val
			assertInvalidAccount(t, validate(a))
		})
		t.Run("stock/U/"+nf.name, func(t *testing.T) {
			a := minimalAccount()
			a.Positions[0].Position.U = nf.val
			assertInvalidAccount(t, validate(a))
		})
		t.Run("etf/Price/"+nf.name, func(t *testing.T) {
			a := minimalAccount()
			a.Positions = []AccountPosition{stockLikePosition(engine.ETFKind, nf.val, 10)}
			assertInvalidAccount(t, validate(a))
		})
		t.Run("etf/Shares/"+nf.name, func(t *testing.T) {
			a := minimalAccount()
			a.Positions = []AccountPosition{stockLikePosition(engine.ETFKind, 50, nf.val)}
			assertInvalidAccount(t, validate(a))
		})
	}
}

func TestValidate_rejectsUnknownKind(t *testing.T) {
	a := minimalAccount()
	a.Positions[0].Position.Legs[0].Kind = engine.Kind("future")
	err := validate(a)
	assertInvalidAccount(t, err)
	for _, k := range []engine.Kind{
		engine.OptionKind, engine.StockKind, engine.ETFKind,
		engine.ETNKind, engine.ConvertibleKind, engine.WarrantKind,
	} {
		if !strings.Contains(err.Error(), string(k)) {
			t.Fatalf("error %q should list recognized kind %q", err.Error(), k)
		}
	}
}

func TestValidate_rejectsUnknownSide(t *testing.T) {
	a := minimalAccount()
	a.Positions[0].Position.Legs[0].Side = engine.Side("neutral")
	assertInvalidAccount(t, validate(a))
}

func optionLeg(side engine.Side, p, qty, mult float64) engine.Leg {
	return engine.Leg{
		Side:       side,
		Kind:       engine.OptionKind,
		OptionType: "call",
		P:          p,
		Qty:        qty,
		Mult:       mult,
		K:          100,
	}
}

func TestLegMarketValue_longCallOption(t *testing.T) {
	got := legMarketValue(optionLeg(engine.Long, 5, 2, 100), 0)
	if got != 1000 {
		t.Fatalf("want 1000, got %v", got)
	}
}

func TestLegMarketValue_shortPutOption(t *testing.T) {
	got := legMarketValue(optionLeg(engine.Short, 5, 2, 100), 0)
	if got != 1000 {
		t.Fatalf("short leg MV should be positive magnitude; want 1000, got %v", got)
	}
}

func TestLegMarketValue_defaultOptionMultiplier(t *testing.T) {
	got := legMarketValue(optionLeg(engine.Long, 2, 1, 0), 0)
	if got != 200 {
		t.Fatalf("Mult==0 must default to 100; want 200, got %v", got)
	}
}

func TestLegMarketValue_explicitNonStandardMult(t *testing.T) {
	got := legMarketValue(optionLeg(engine.Long, 2, 1, 10), 0)
	if got != 20 {
		t.Fatalf("want 20 (mini-option Mult=10 honored), got %v", got)
	}
}

func TestLegMarketValue_longStock(t *testing.T) {
	leg := engine.Leg{Side: engine.Long, Kind: engine.StockKind, Shares: 100}
	got := legMarketValue(leg, 150)
	if got != 15000 {
		t.Fatalf("want 15000, got %v", got)
	}
}

func TestLegMarketValue_stockLikeBuckets(t *testing.T) {
	kinds := []engine.Kind{engine.ETFKind, engine.ETNKind, engine.ConvertibleKind, engine.WarrantKind}
	for _, k := range kinds {
		t.Run(string(k), func(t *testing.T) {
			leg := engine.Leg{Side: engine.Long, Kind: k, Price: 50, Shares: 10}
			got := legMarketValue(leg, 0)
			if got != 500 {
				t.Fatalf("want 500, got %v", got)
			}
		})
	}
}

func TestAccumulate_longStockBucket(t *testing.T) {
	var s AccountSnapshot
	leg := engine.Leg{Side: engine.Long, Kind: engine.StockKind, Shares: 100}
	accumulate(&s, leg, 150)
	if s.LMVStock != 15000 {
		t.Fatalf("LMVStock want 15000, got %v", s.LMVStock)
	}
	if s.LMVOption != 0 || s.SMVStock != 0 || s.SMVOption != 0 {
		t.Fatalf("other buckets should be zero: %+v", s)
	}
}

func TestAccumulate_shortStockBucket(t *testing.T) {
	var s AccountSnapshot
	leg := engine.Leg{Side: engine.Short, Kind: engine.StockKind, Shares: 100}
	accumulate(&s, leg, 150)
	if s.SMVStock != 15000 {
		t.Fatalf("SMVStock want 15000 (positive magnitude), got %v", s.SMVStock)
	}
	if s.LMVStock != 0 || s.LMVOption != 0 || s.SMVOption != 0 {
		t.Fatalf("other buckets should be zero: %+v", s)
	}
}

func TestAccumulate_longShortOptionBuckets(t *testing.T) {
	var s AccountSnapshot
	accumulate(&s, optionLeg(engine.Long, 5, 2, 100), 0)
	if s.LMVOption != 1000 {
		t.Fatalf("LMVOption want 1000, got %v", s.LMVOption)
	}
	if s.SMVOption != 0 || s.LMVStock != 0 || s.SMVStock != 0 {
		t.Fatalf("long-call should not contaminate other buckets: %+v", s)
	}

	var s2 AccountSnapshot
	put := optionLeg(engine.Short, 3, 4, 100)
	put.OptionType = "put"
	accumulate(&s2, put, 0)
	if s2.SMVOption != 1200 {
		t.Fatalf("SMVOption want 1200 positive, got %v", s2.SMVOption)
	}
	if s2.LMVOption != 0 || s2.LMVStock != 0 || s2.SMVStock != 0 {
		t.Fatalf("short-put should not contaminate other buckets: %+v", s2)
	}
}

func TestAccumulate_stockLikeKindsRollIntoStockBuckets(t *testing.T) {
	kinds := []engine.Kind{engine.ETFKind, engine.ETNKind, engine.ConvertibleKind, engine.WarrantKind}
	for _, k := range kinds {
		t.Run("long/"+string(k), func(t *testing.T) {
			var s AccountSnapshot
			accumulate(&s, engine.Leg{Side: engine.Long, Kind: k, Price: 50, Shares: 10}, 0)
			if s.LMVStock != 500 {
				t.Fatalf("LMVStock want 500, got %v", s.LMVStock)
			}
			if s.LMVOption != 0 || s.SMVStock != 0 || s.SMVOption != 0 {
				t.Fatalf("stock-like long should only touch LMVStock: %+v", s)
			}
		})
		t.Run("short/"+string(k), func(t *testing.T) {
			var s AccountSnapshot
			accumulate(&s, engine.Leg{Side: engine.Short, Kind: k, Price: 50, Shares: 10}, 0)
			if s.SMVStock != 500 {
				t.Fatalf("SMVStock want 500 positive, got %v", s.SMVStock)
			}
			if s.LMVOption != 0 || s.LMVStock != 0 || s.SMVOption != 0 {
				t.Fatalf("stock-like short should only touch SMVStock: %+v", s)
			}
		})
	}
}

func TestAccumulate_multipleLegsSum(t *testing.T) {
	var s AccountSnapshot
	leg1 := engine.Leg{Side: engine.Long, Kind: engine.StockKind, Shares: 100}
	leg2 := engine.Leg{Side: engine.Long, Kind: engine.StockKind, Shares: 50}
	accumulate(&s, leg1, 150)
	accumulate(&s, leg2, 200)
	if s.LMVStock != 15000+10000 {
		t.Fatalf("LMVStock want 25000, got %v", s.LMVStock)
	}
}

func emptyAccount() Account {
	return Account{
		ID:          "acct-empty",
		AccountType: engine.MarginAccount,
		Phase:       engine.Initial,
	}
}

func TestAggregate_emptyAccount(t *testing.T) {
	a := emptyAccount()
	a.SODEquity = 50_000
	a.PnL = 1_000
	a.DepositsWithdrawals = -500

	snap, err := Aggregate(a, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.LMVStock != 0 || snap.LMVOption != 0 || snap.SMVStock != 0 || snap.SMVOption != 0 {
		t.Fatalf("MV buckets must be zero: %+v", snap)
	}
	if snap.NetMV != 0 || snap.GrossExposure != 0 {
		t.Fatalf("NetMV / GrossExposure want 0, got %v / %v", snap.NetMV, snap.GrossExposure)
	}
	if snap.CurrentEquity != 50_500 {
		t.Fatalf("CurrentEquity want 50500, got %v", snap.CurrentEquity)
	}
}

func TestAggregate_currentEquityFromSODAndPnL(t *testing.T) {
	a := emptyAccount()
	a.SODEquity = 100_000
	a.PnL = 2_500
	a.DepositsWithdrawals = -1_000

	snap, err := Aggregate(a, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.CurrentEquity != 101_500 {
		t.Fatalf("CurrentEquity want 101500, got %v", snap.CurrentEquity)
	}
}

func TestAggregate_adjustedBalance(t *testing.T) {
	a := emptyAccount()
	a.SODEquity = 100_000

	longStock := longStockPosition()
	longStock.ID = "long-stock"
	shortCall := AccountPosition{
		ID: "short-call",
		Position: engine.Position{
			U:     100,
			Class: "equity",
			Legs:  []engine.Leg{optionLeg(engine.Short, 5, 2, 100)},
		},
	}
	a.Positions = []AccountPosition{longStock, shortCall}

	snap, err := Aggregate(a, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantLMVStock := 100.0 * 100.0
	wantSMVOption := 5.0 * 2.0 * 100.0
	if snap.LMVStock != wantLMVStock {
		t.Fatalf("LMVStock want %v, got %v", wantLMVStock, snap.LMVStock)
	}
	if snap.SMVOption != wantSMVOption {
		t.Fatalf("SMVOption want %v, got %v", wantSMVOption, snap.SMVOption)
	}
	wantAdj := snap.CurrentEquity - (snap.LMVStock + snap.LMVOption) + snap.SMVOption
	if snap.AdjustedBalance != wantAdj {
		t.Fatalf("AdjustedBalance want %v, got %v", wantAdj, snap.AdjustedBalance)
	}
	if wantAdj != 100_000-10_000+1_000 {
		t.Fatalf("hand-computed AdjustedBalance want 91000, got %v", wantAdj)
	}
}

func threePermittedAccount() (Account, []PositionEvaluation) {
	a := emptyAccount()
	a.SODEquity = 100_000
	p1 := longStockPosition()
	p1.ID = "p1"
	p2 := longStockPosition()
	p2.ID = "p2"
	p3 := longStockPosition()
	p3.ID = "p3"
	a.Positions = []AccountPosition{p1, p2, p3}
	evals := []PositionEvaluation{
		{PositionID: "p1", Result: engine.Result{Permitted: true, Requirement: 1_000, CashCall: 250}},
		{PositionID: "p2", Result: engine.Result{Permitted: true, Requirement: 2_500, CashCall: 500}},
		{PositionID: "p3", Result: engine.Result{Permitted: true, Requirement: 500, CashCall: 100}},
	}
	return a, evals
}

func TestAggregate_totalRequirementAndCashCall(t *testing.T) {
	a, evals := threePermittedAccount()
	snap, err := Aggregate(a, evals)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.TotalRequirement != 4_000 {
		t.Fatalf("TotalRequirement want 4000, got %v", snap.TotalRequirement)
	}
	if snap.TotalCashCall != 850 {
		t.Fatalf("TotalCashCall want 850, got %v", snap.TotalCashCall)
	}
}

func TestAggregate_depositRequirementsAggregated(t *testing.T) {
	a := emptyAccount()
	a.SODEquity = 100_000
	p1 := longStockPosition()
	p1.ID = "p1"
	p2 := longStockPosition()
	p2.ID = "p2"
	p3 := longStockPosition()
	p3.ID = "p3"
	p4 := longStockPosition()
	p4.ID = "p4"
	a.Positions = []AccountPosition{p1, p2, p3, p4}

	evals := []PositionEvaluation{
		{PositionID: "p1", Result: engine.Result{Permitted: true, Requirement: 100, DepositKind: "cash_or_escrow"}},
		{PositionID: "p2", Result: engine.Result{Permitted: true, Requirement: 200, DepositKind: "cash_or_escrow"}},
		{PositionID: "p3", Result: engine.Result{Permitted: true, Requirement: 400, DepositKind: "underlying_or_escrow"}},
		{PositionID: "p4", Result: engine.Result{Permitted: true, Requirement: 50}},
	}
	snap, err := Aggregate(a, evals)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]float64{
		"cash_or_escrow":       300,
		"underlying_or_escrow": 400,
	}
	if !reflect.DeepEqual(snap.DepositRequirements, want) {
		t.Fatalf("DepositRequirements want %v, got %v", want, snap.DepositRequirements)
	}
	if snap.TotalRequirement != 750 {
		t.Fatalf("TotalRequirement want 750, got %v", snap.TotalRequirement)
	}
}

func TestAggregate_equityRatio(t *testing.T) {
	a := emptyAccount()
	a.SODEquity = 100_000
	p := longStockPosition()
	p.ID = "p1"
	a.Positions = []AccountPosition{p}
	evals := []PositionEvaluation{{
		PositionID: "p1",
		Result:     engine.Result{Permitted: true, Requirement: 20_000},
	}}
	snap, err := Aggregate(a, evals)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.EquityRatio != 0.2 {
		t.Fatalf("EquityRatio want 0.2, got %v", snap.EquityRatio)
	}
}

func TestAggregate_zeroOrNegativeEquity(t *testing.T) {
	for name, sod := range map[string]float64{"zero": 0, "negative": -1_000} {
		t.Run(name, func(t *testing.T) {
			a := emptyAccount()
			a.SODEquity = sod
			p := longStockPosition()
			p.ID = "p1"
			a.Positions = []AccountPosition{p}
			evals := []PositionEvaluation{{
				PositionID: "p1",
				Result:     engine.Result{Permitted: true, Requirement: 5_000},
			}}
			snap, err := Aggregate(a, evals)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if snap.StockLeverage != 0 || snap.GrossLeverage != 0 || snap.EquityRatio != 0 {
				t.Fatalf("zero-equity guard violated: leverage=%v/%v ratio=%v",
					snap.StockLeverage, snap.GrossLeverage, snap.EquityRatio)
			}
			if math.IsInf(snap.EquityRatio, 0) || math.IsNaN(snap.EquityRatio) {
				t.Fatalf("EquityRatio must not be Inf/NaN, got %v", snap.EquityRatio)
			}
			if len(snap.Warnings) == 0 {
				t.Fatalf("expected zero-equity Warning, got none")
			}
		})
	}
}

func TestAggregate_permittedFalseIsViolation(t *testing.T) {
	a := emptyAccount()
	a.SODEquity = 100_000
	p := longStockPosition()
	p.ID = "p1"
	a.Positions = []AccountPosition{p}
	evals := []PositionEvaluation{{
		PositionID: "p1",
		Result:     engine.Result{Permitted: false, Requirement: 500},
	}}
	snap, err := Aggregate(a, evals)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snap.Violations) != 1 || snap.Violations[0].PositionID != "p1" {
		t.Fatalf("expected one violation for p1, got %+v", snap.Violations)
	}
	if snap.TotalRequirement != 0 {
		t.Fatalf("violation must not feed totals; TotalRequirement=%v", snap.TotalRequirement)
	}
}

func TestAggregate_noRuleFlagPreserved(t *testing.T) {
	a := emptyAccount()
	a.SODEquity = 100_000
	p := longStockPosition()
	p.ID = "p1"
	a.Positions = []AccountPosition{p}
	evals := []PositionEvaluation{{PositionID: "p1", NoRule: true}}
	snap, err := Aggregate(a, evals)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snap.Evaluations) != 1 || !snap.Evaluations[0].NoRule {
		t.Fatalf("NoRule flag not preserved: %+v", snap.Evaluations)
	}
	if len(snap.Violations) != 0 || len(snap.Errors) != 0 {
		t.Fatalf("NoRule eval should not appear in Violations/Errors: %+v / %+v",
			snap.Violations, snap.Errors)
	}
	if snap.TotalRequirement != 0 {
		t.Fatalf("NoRule must not feed totals; got %v", snap.TotalRequirement)
	}
}

func TestAggregate_errorEvalCaptured(t *testing.T) {
	a := emptyAccount()
	a.SODEquity = 100_000
	p := longStockPosition()
	p.ID = "p1"
	a.Positions = []AccountPosition{p}
	evals := []PositionEvaluation{{PositionID: "p1", Error: errors.New("boom")}}
	snap, err := Aggregate(a, evals)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snap.Errors) != 1 {
		t.Fatalf("expected one error entry, got %+v", snap.Errors)
	}
	if snap.LMVStock != 10_000 {
		t.Fatalf("MV must contribute even when eval errored; LMVStock=%v", snap.LMVStock)
	}
	if snap.TotalRequirement != 0 {
		t.Fatalf("errored eval must not feed totals; got %v", snap.TotalRequirement)
	}
}

func cloneAccount(a Account) Account {
	c := a
	c.Positions = append([]AccountPosition(nil), a.Positions...)
	for i := range c.Positions {
		c.Positions[i].Position.Legs = append([]engine.Leg(nil), a.Positions[i].Position.Legs...)
	}
	return c
}

func TestAggregate_doesNotMutatePositions(t *testing.T) {
	a, evals := threePermittedAccount()
	clone := cloneAccount(a)
	if _, err := Aggregate(a, evals); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(a, clone) {
		t.Fatalf("Aggregate mutated input account.\nwant: %+v\ngot:  %+v", clone, a)
	}
}

func TestAggregate_rejectsEmptyEvalID(t *testing.T) {
	a := minimalAccount()
	evals := []PositionEvaluation{{Result: engine.Result{Permitted: true, Requirement: 1}}}
	_, err := Aggregate(a, evals)
	if err == nil {
		t.Fatalf("expected error for empty eval position id")
	}
	if !strings.HasPrefix(err.Error(), "invalid account:") {
		t.Fatalf("expected invalid-account prefix, got %v", err)
	}
}

func TestAggregate_rejectsExtraEvalIDs(t *testing.T) {
	a := minimalAccount()
	evals := []PositionEvaluation{{PositionID: "ghost"}}
	snap, err := Aggregate(a, evals)
	if err == nil {
		t.Fatalf("expected error, got snapshot %+v", snap)
	}
	if !strings.HasPrefix(err.Error(), "invalid account:") {
		t.Fatalf("expected invalid-account prefix, got %v", err)
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("error should name the unknown id, got %v", err)
	}
}

func TestAggregate_rejectsDuplicateEvalIDs(t *testing.T) {
	a := minimalAccount()
	evals := []PositionEvaluation{
		{PositionID: "p1", Result: engine.Result{Permitted: true, Requirement: 1}},
		{PositionID: "p1", Result: engine.Result{Permitted: true, Requirement: 2}},
	}
	_, err := Aggregate(a, evals)
	if err == nil {
		t.Fatalf("expected error for duplicate position id")
	}
	if !strings.HasPrefix(err.Error(), "invalid account:") {
		t.Fatalf("expected invalid-account prefix, got %v", err)
	}
}

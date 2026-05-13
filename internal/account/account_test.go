package account

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"margincalc/internal/engine"
)

func assertClose(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Errorf("%s: got %.4f, want %.4f", label, got, want)
	}
}

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

// ---------------------------------------------------------------------------
// AggregateWithRulebook + classifyEvaluation
// ---------------------------------------------------------------------------

// rbCache mirrors the cache pattern in internal/engine/rulebook_test.go so
// the YAML is parsed/compiled once across all account-package tests.
var rbCache *engine.Rulebook

func loadRulebook(t *testing.T) *engine.Rulebook {
	t.Helper()
	if rbCache != nil {
		return rbCache
	}
	x, err := engine.LoadRulebook("../../rules/cboe_baseline.yaml")
	if err != nil {
		t.Fatalf("LoadRulebook: %v", err)
	}
	rbCache = x
	return rbCache
}

func TestClassifyEvaluation_permitted(t *testing.T) {
	res := engine.Result{Permitted: true, Requirement: 1000}
	got := classifyEvaluation("p1", res, nil)
	if got.Violation || got.NoRule || got.Error != nil {
		t.Fatalf("expected clean permitted, got %+v", got)
	}
	if got.Result.Requirement != 1000 {
		t.Fatalf("Result not carried through, got %+v", got.Result)
	}
	if got.PositionID != "p1" {
		t.Fatalf("PositionID lost, got %q", got.PositionID)
	}
}

func TestClassifyEvaluation_notPermittedIsViolation(t *testing.T) {
	res := engine.Result{Permitted: false, RuleID: "house_block"}
	got := classifyEvaluation("p2", res, nil)
	if !got.Violation {
		t.Fatalf("expected Violation=true, got %+v", got)
	}
	if got.NoRule || got.Error != nil {
		t.Fatalf("expected NoRule=false, Error=nil; got %+v", got)
	}
	if got.PositionID != "p2" {
		t.Fatalf("PositionID lost, got %q", got.PositionID)
	}
}

func TestClassifyEvaluation_noRuleIsCaptured(t *testing.T) {
	err := fmt.Errorf("no rule matched position with 2 legs")
	got := classifyEvaluation("p3", engine.Result{}, err)
	if !got.NoRule {
		t.Fatalf("expected NoRule=true, got %+v", got)
	}
	if got.Error != nil {
		t.Fatalf("expected Error=nil, got %v", got.Error)
	}
	if got.Result != (engine.Result{}) {
		t.Fatalf("expected zero Result, got %+v", got.Result)
	}
	if got.PositionID != "p3" {
		t.Fatalf("PositionID lost, got %q", got.PositionID)
	}
}

func TestClassifyEvaluation_validationErrorIsError(t *testing.T) {
	err := fmt.Errorf("invalid position: leg 0 K must be > 0, got 0")
	got := classifyEvaluation("p4", engine.Result{}, err)
	if got.Error == nil {
		t.Fatalf("expected Error set, got %+v", got)
	}
	if got.NoRule {
		t.Fatalf("validation error must not flag NoRule, got %+v", got)
	}
	if got.PositionID != "p4" {
		t.Fatalf("PositionID lost, got %q", got.PositionID)
	}
}

func TestClassifyEvaluation_otherErrorIsError(t *testing.T) {
	err := fmt.Errorf("unexpected CEL failure: divide by zero")
	got := classifyEvaluation("p5", engine.Result{}, err)
	if got.Error == nil {
		t.Fatalf("expected Error set, got %+v", got)
	}
	if got.NoRule {
		t.Fatalf("non-no-match error must not flag NoRule, got %+v", got)
	}
	if got.PositionID != "p5" {
		t.Fatalf("PositionID lost, got %q", got.PositionID)
	}
}

func longStockOnlyPosition() AccountPosition {
	return AccountPosition{
		ID: "stock-only",
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

func shortPutOTMPosition() AccountPosition {
	return AccountPosition{
		ID: "short-put",
		Position: engine.Position{
			U:     95.0,
			Class: "equity",
			Legs: []engine.Leg{{
				Side: engine.Short, Kind: engine.OptionKind, OptionType: "put",
				K: 80, P: 2.0, P0: 2.0, Qty: 1, Mult: 100,
			}},
		},
	}
}

func verticalCallSpreadPosition() AccountPosition {
	return AccountPosition{
		ID: "vert-call",
		Position: engine.Position{
			U:     128.50,
			Class: "equity",
			Legs: []engine.Leg{
				{Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
					K: 125, P: 3.80, P0: 3.80, Qty: 1, Mult: 100,
					Style: "american", Venue: "listed",
					Underlying: "XYZ", Expiration: "2024-11-15"},
				{Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
					K: 120, P: 8.40, P0: 8.40, Qty: 1, Mult: 100,
					Style: "american", Venue: "listed",
					Underlying: "XYZ", Expiration: "2024-11-15"},
			},
		},
	}
}

// engineMalformedPosition passes account-level validation (option fields P>=0,
// Qty>0 satisfied) but fails engine.validateRuleInputs because the rule that
// binds (long_option_short_dated) requires legs.opt.time_to_expiration_months
// > 0, which is left at zero here. The resulting error string starts with
// "invalid position:" and lands in Errors per the classifier contract.
func engineMalformedPosition() AccountPosition {
	return AccountPosition{
		ID: "malformed-long-opt",
		Position: engine.Position{
			U:     100,
			Class: "equity",
			Legs: []engine.Leg{{
				Side: engine.Long, Kind: engine.OptionKind, OptionType: "call",
				K: 100, P: 1.0, P0: 1.0, Qty: 1, Mult: 100,
				// TimeToExpirationMonths intentionally 0
			}},
		},
	}
}

func TestAggregateWithRulebook_marginAccountMixedPositions(t *testing.T) {
	rb := loadRulebook(t)
	a := Account{
		ID:          "acct-mixed",
		AccountType: engine.MarginAccount,
		Phase:       engine.Initial,
		SODEquity:   100000,
		CashBalance: 100000,
		Positions: []AccountPosition{
			longStockOnlyPosition(),
			shortPutOTMPosition(),
			verticalCallSpreadPosition(),
			engineMalformedPosition(),
		},
	}

	snap, err := AggregateWithRulebook(rb, a)
	if err != nil {
		t.Fatalf("AggregateWithRulebook: %v", err)
	}

	if len(snap.Evaluations) != 4 {
		t.Fatalf("Evaluations want 4 entries in declaration order, got %d", len(snap.Evaluations))
	}
	wantIDs := []string{"stock-only", "short-put", "vert-call", "malformed-long-opt"}
	for i, w := range wantIDs {
		if snap.Evaluations[i].PositionID != w {
			t.Fatalf("Evaluations[%d].PositionID want %q, got %q", i, w, snap.Evaluations[i].PositionID)
		}
	}
	if !snap.Evaluations[0].NoRule {
		t.Fatalf("stock-only must classify as NoRule, got %+v", snap.Evaluations[0])
	}
	if snap.Evaluations[1].Error != nil || snap.Evaluations[1].NoRule || snap.Evaluations[1].Violation {
		t.Fatalf("short-put must be a clean permitted evaluation, got %+v", snap.Evaluations[1])
	}
	if snap.Evaluations[2].Error != nil || snap.Evaluations[2].NoRule || snap.Evaluations[2].Violation {
		t.Fatalf("vert-call must be a clean permitted evaluation, got %+v", snap.Evaluations[2])
	}
	if snap.Evaluations[3].Error == nil {
		t.Fatalf("malformed-long-opt must carry an Error, got %+v", snap.Evaluations[3])
	}

	if len(snap.Errors) != 1 || snap.Errors[0].PositionID != "malformed-long-opt" {
		t.Fatalf("Errors want exactly malformed-long-opt, got %+v", snap.Errors)
	}
	if !strings.HasPrefix(snap.Errors[0].Error.Error(), "invalid position:") {
		t.Fatalf("Error message should be an engine validation error, got %v", snap.Errors[0].Error)
	}
	if len(snap.Violations) != 0 {
		t.Fatalf("Violations should be empty, got %+v", snap.Violations)
	}

	// Manual numbers from existing engine tests (TestShortPutOTM_p28,
	// TestVerticalCallSpread_p42). Positions 1 and 4 contribute zero
	// requirement; position 4 still contributes MV because account-level
	// validation passed (partial-output preservation).
	assertClose(t, "TotalRequirement", snap.TotalRequirement, 1000+880)
	assertClose(t, "TotalCashCall", snap.TotalCashCall, 800+40)

	// MV buckets:
	//   stock-only:   long stock 100 shares @ U=100  → LMVStock += 10000
	//   short-put:    short put P=2 qty=1 mult=100   → SMVOption += 200
	//   vert-call:    long  call P=3.80              → LMVOption += 380
	//                 short call P=8.40              → SMVOption += 840
	//   malformed:    long  call P=1 qty=1 mult=100  → LMVOption += 100
	assertClose(t, "LMVStock", snap.LMVStock, 10000)
	assertClose(t, "LMVOption", snap.LMVOption, 380+100)
	assertClose(t, "SMVOption", snap.SMVOption, 200+840)
	assertClose(t, "SMVStock", snap.SMVStock, 0)
}

func TestAggregateWithRulebook_errorDoesNotShortCircuit(t *testing.T) {
	rb := loadRulebook(t)
	a := Account{
		ID:          "acct-partial",
		AccountType: engine.MarginAccount,
		Phase:       engine.Initial,
		SODEquity:   50000,
		Positions: []AccountPosition{
			shortPutOTMPosition(),
			engineMalformedPosition(),
		},
	}

	snap, err := AggregateWithRulebook(rb, a)
	if err != nil {
		t.Fatalf("AggregateWithRulebook must not surface per-position errors as function error, got %v", err)
	}
	if len(snap.Errors) != 1 || snap.Errors[0].PositionID != "malformed-long-opt" {
		t.Fatalf("malformed position must land in Errors, got %+v", snap.Errors)
	}
	// Permitted position's MV and requirement still appear.
	assertClose(t, "TotalRequirement", snap.TotalRequirement, 1000)
	assertClose(t, "TotalCashCall", snap.TotalCashCall, 800)
	// Both legs contribute MV: the permitted short put (200) and the
	// erroring long option (100).
	assertClose(t, "SMVOption", snap.SMVOption, 200)
	assertClose(t, "LMVOption", snap.LMVOption, 100)
}

func TestAggregateWithRulebook_nilRulebook(t *testing.T) {
	a := minimalAccount()
	snap, err := AggregateWithRulebook(nil, a)
	if err == nil {
		t.Fatalf("expected error for nil rulebook")
	}
	if !strings.HasPrefix(err.Error(), "invalid account:") {
		t.Fatalf("expected invalid-account prefix, got %v", err)
	}
	if !strings.Contains(err.Error(), "nil rulebook") {
		t.Fatalf("error should name the nil rulebook, got %v", err)
	}
	if snap.AccountID != "" {
		t.Fatalf("expected empty snapshot, got %+v", snap)
	}
}

func TestAggregateWithRulebook_validationFailureReturnsError(t *testing.T) {
	rb := loadRulebook(t)
	a := minimalAccount()
	a.ID = "" // fail validate()
	snap, err := AggregateWithRulebook(rb, a)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.HasPrefix(err.Error(), "invalid account:") {
		t.Fatalf("expected invalid-account prefix, got %v", err)
	}
	if snap.AccountID != "" || len(snap.Evaluations) != 0 {
		t.Fatalf("expected empty snapshot, got %+v", snap)
	}
}

package account

import (
	"math"
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
}

func TestValidate_rejectsEmptyPositionID(t *testing.T) {
	a := minimalAccount()
	a.Positions[0].ID = ""
	err := validate(a)
	assertInvalidAccount(t, err)
	if !strings.Contains(err.Error(), "position[0] ID is required") {
		t.Fatalf("expected index-specific error for first position, got %v", err)
	}

	a2 := minimalAccount()
	empty := longStockPosition()
	empty.ID = ""
	a2.Positions = append(a2.Positions, empty)
	err = validate(a2)
	assertInvalidAccount(t, err)
	if !strings.Contains(err.Error(), "position[1] ID is required") {
		t.Fatalf("expected index-specific error for appended position, got %v", err)
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

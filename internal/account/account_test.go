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
	if !strings.HasPrefix(err.Error(), "invalid account") {
		t.Fatalf("expected error prefixed with %q, got: %v", "invalid account", err)
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

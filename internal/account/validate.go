package account

import (
	"fmt"
	"math"

	"margincalc/internal/engine"
)

// recognizedKinds is the canonical set from internal/engine/types.go:12-19.
// Listed here so error messages can echo it back to the caller.
var recognizedKinds = []engine.Kind{
	engine.OptionKind,
	engine.StockKind,
	engine.ETFKind,
	engine.ETNKind,
	engine.ConvertibleKind,
	engine.WarrantKind,
}

// validate enforces account-level shape and per-leg market-value-input shape.
// Margin-rule input shape is engine.validateRuleInputs's job
// (internal/engine/rulebook.go:460) and is intentionally not duplicated here.
//
// Every error starts with the literal prefix "invalid account:" so callers
// can string-classify, mirroring the engine's "invalid position:" convention.
func validate(account Account) error {
	if account.ID == "" {
		return fmt.Errorf("invalid account: ID is required")
	}

	switch account.AccountType {
	case engine.CashAccount, engine.MarginAccount:
	default:
		return fmt.Errorf("invalid account: id=%q account_type=%q not in {%q, %q}",
			account.ID, account.AccountType, engine.CashAccount, engine.MarginAccount)
	}

	switch account.Phase {
	case engine.Initial, engine.Maintenance:
	default:
		return fmt.Errorf("invalid account: id=%q phase=%q not in {%q, %q}",
			account.ID, account.Phase, engine.Initial, engine.Maintenance)
	}

	for _, f := range []struct {
		name string
		val  float64
	}{
		{"sod_equity", account.SODEquity},
		{"cash_balance", account.CashBalance},
		{"pnl", account.PnL},
		{"deposits_withdrawals", account.DepositsWithdrawals},
	} {
		if !isFinite(f.val) {
			return fmt.Errorf("invalid account: id=%q %s is not finite", account.ID, f.name)
		}
	}

	seen := make(map[string]struct{}, len(account.Positions))
	for i, p := range account.Positions {
		if p.ID == "" {
			return fmt.Errorf("invalid account: id=%q position[%d] ID is required", account.ID, i)
		}
		if _, dup := seen[p.ID]; dup {
			return fmt.Errorf("invalid account: id=%q duplicate position id %q", account.ID, p.ID)
		}
		seen[p.ID] = struct{}{}
	}

	for i, p := range account.Positions {
		if err := validatePosition(account.ID, i, p); err != nil {
			return err
		}
	}

	return nil
}

func validatePosition(accountID string, idx int, p AccountPosition) error {
	for j, leg := range p.Position.Legs {
		if err := validateLeg(accountID, posLabel(p, idx), j, leg, p.Position); err != nil {
			return err
		}
	}
	return nil
}

func posLabel(p AccountPosition, idx int) string {
	if p.ID != "" {
		return fmt.Sprintf("position %q", p.ID)
	}
	return fmt.Sprintf("position[%d]", idx)
}

// validateLeg enforces MV-input invariants per kind:
//   - side ∈ {long, short}; kind ∈ recognizedKinds
//   - option: finite P >= 0, finite Qty > 0
//   - stock: finite Shares > 0, finite pos.U > 0
//   - ETF/ETN/convertible/warrant: finite Price > 0, finite Shares > 0
//
// Non-finite values are rejected before inequality checks because NaN
// compares false against any bound and would otherwise slip through.
// Margin-rule input shape (K, P0, Style, Expiration, etc.) is intentionally
// left to engine.validateRuleInputs.
func validateLeg(accountID, posLabel string, j int, leg engine.Leg, pos engine.Position) error {
	switch leg.Side {
	case engine.Long, engine.Short:
	default:
		return fmt.Errorf("invalid account: id=%q %s leg[%d] side=%q not in {%q, %q}",
			accountID, posLabel, j, leg.Side, engine.Long, engine.Short)
	}

	if !isRecognizedKind(leg.Kind) {
		return fmt.Errorf("invalid account: id=%q %s leg[%d] kind=%q not in %v",
			accountID, posLabel, j, leg.Kind, recognizedKinds)
	}

	switch leg.Kind {
	case engine.OptionKind:
		if !isFinite(leg.Mult) {
			return fmt.Errorf("invalid account: id=%q %s leg[%d] option mult=%g is not finite",
				accountID, posLabel, j, leg.Mult)
		}
		if leg.Mult < 0 {
			return fmt.Errorf("invalid account: id=%q %s leg[%d] option mult=%g must be >= 0",
				accountID, posLabel, j, leg.Mult)
		}
		if !isFinite(leg.P) {
			return fmt.Errorf("invalid account: id=%q %s leg[%d] option P=%g is not finite",
				accountID, posLabel, j, leg.P)
		}
		if leg.P < 0 {
			return fmt.Errorf("invalid account: id=%q %s leg[%d] option P=%g must be >= 0",
				accountID, posLabel, j, leg.P)
		}
		if !isFinite(leg.Qty) {
			return fmt.Errorf("invalid account: id=%q %s leg[%d] option qty=%g is not finite",
				accountID, posLabel, j, leg.Qty)
		}
		if leg.Qty <= 0 {
			return fmt.Errorf("invalid account: id=%q %s leg[%d] option qty=%g must be > 0",
				accountID, posLabel, j, leg.Qty)
		}
	case engine.StockKind:
		if !isFinite(leg.Shares) {
			return fmt.Errorf("invalid account: id=%q %s leg[%d] stock shares=%g is not finite",
				accountID, posLabel, j, leg.Shares)
		}
		if leg.Shares <= 0 {
			return fmt.Errorf("invalid account: id=%q %s leg[%d] stock shares=%g must be > 0",
				accountID, posLabel, j, leg.Shares)
		}
		if !isFinite(pos.U) {
			return fmt.Errorf("invalid account: id=%q %s leg[%d] stock position U=%g is not finite",
				accountID, posLabel, j, pos.U)
		}
		if pos.U <= 0 {
			return fmt.Errorf("invalid account: id=%q %s leg[%d] stock position U=%g must be > 0",
				accountID, posLabel, j, pos.U)
		}
	case engine.ETFKind, engine.ETNKind, engine.ConvertibleKind, engine.WarrantKind:
		if !isFinite(leg.Price) {
			return fmt.Errorf("invalid account: id=%q %s leg[%d] %s price=%g is not finite",
				accountID, posLabel, j, leg.Kind, leg.Price)
		}
		if leg.Price <= 0 {
			return fmt.Errorf("invalid account: id=%q %s leg[%d] %s price=%g must be > 0",
				accountID, posLabel, j, leg.Kind, leg.Price)
		}
		if !isFinite(leg.Shares) {
			return fmt.Errorf("invalid account: id=%q %s leg[%d] %s shares=%g is not finite",
				accountID, posLabel, j, leg.Kind, leg.Shares)
		}
		if leg.Shares <= 0 {
			return fmt.Errorf("invalid account: id=%q %s leg[%d] %s shares=%g must be > 0",
				accountID, posLabel, j, leg.Kind, leg.Shares)
		}
	}

	return nil
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func isRecognizedKind(k engine.Kind) bool {
	for _, rk := range recognizedKinds {
		if k == rk {
			return true
		}
	}
	return false
}

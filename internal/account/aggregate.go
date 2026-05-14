package account

import (
	"fmt"
	"math"
	"strings"

	"margincalc/internal/engine"
)

// AggregateWithRulebook evaluates each position in account against rb,
// classifies the engine return into Evaluations / Violations / Errors
// (with NoRule distinguished via the "no rule matched" substring contract
// the engine emits at internal/engine/rulebook.go's Evaluate), and reduces
// to Aggregate. Per-position errors do not short-circuit aggregation:
// market-value buckets are still accumulated for every leg that passes
// account-level validation, and the per-position error lands in Errors.
func AggregateWithRulebook(rb *engine.Rulebook, account Account) (AccountSnapshot, error) {
	if rb == nil {
		return AccountSnapshot{}, fmt.Errorf("invalid account: nil rulebook")
	}
	if err := validate(account); err != nil {
		return AccountSnapshot{}, err
	}
	evals := make([]PositionEvaluation, 0, len(account.Positions))
	for _, pos := range account.Positions {
		result, err := rb.Evaluate(pos.Position, account.AccountType, account.Phase)
		evals = append(evals, classifyEvaluation(pos.ID, result, err))
	}
	return Aggregate(account, evals)
}

// classifyEvaluation converts the engine's (Result, error) return into a
// PositionEvaluation with the correct flags. Only the "no rule matched"
// substring is special-cased into NoRule; every other error (validation
// failures prefixed "invalid position:", CEL eval failures, etc.) lands
// in Error. The classifier reads only the error string — it does not
// type-assert or unwrap. The engine's own assertion at
// internal/engine/rulebook_test.go is the firewall against a future
// rewording of the no-match message.
func classifyEvaluation(positionID string, result engine.Result, err error) PositionEvaluation {
	if err == nil {
		return PositionEvaluation{
			PositionID: positionID,
			Result:     result,
			Violation:  !result.Permitted,
		}
	}
	if strings.Contains(err.Error(), "no rule matched") {
		return PositionEvaluation{PositionID: positionID, NoRule: true}
	}
	return PositionEvaluation{PositionID: positionID, Error: err}
}

// Aggregate builds an AccountSnapshot from a validated Account and a slice
// of caller-supplied PositionEvaluation results. It performs no rulebook
// evaluation itself — classification of NoRule / Violation / Error and the
// engine.Result values must already be populated by the caller. The
// rulebook-driven sibling AggregateWithRulebook wraps this function after
// running rb.Evaluate per position.
//
// Determinism: Aggregate never mutates account.Positions or any Leg. The
// returned snapshot copies scalar fields by value; Evaluations / Violations
// / Errors are fresh slices.
func Aggregate(account Account, evals []PositionEvaluation) (AccountSnapshot, error) {
	if err := validate(account); err != nil {
		return AccountSnapshot{}, err
	}

	knownIDs := make(map[string]struct{}, len(account.Positions))
	for _, p := range account.Positions {
		if p.ID != "" {
			knownIDs[p.ID] = struct{}{}
		}
	}

	evalsByID := make(map[string]PositionEvaluation, len(evals))
	for _, e := range evals {
		if e.PositionID == "" {
			return AccountSnapshot{}, fmt.Errorf("invalid account: evaluation with empty position id")
		}
		if _, dup := evalsByID[e.PositionID]; dup {
			return AccountSnapshot{}, fmt.Errorf("invalid account: duplicate evaluation for position id %q", e.PositionID)
		}
		if _, ok := knownIDs[e.PositionID]; !ok {
			return AccountSnapshot{}, fmt.Errorf("invalid account: evaluation for unknown position id %q", e.PositionID)
		}
		evalsByID[e.PositionID] = e
	}

	snap := AccountSnapshot{
		AccountID:           account.ID,
		AccountType:         account.AccountType,
		Phase:               account.Phase,
		AsOf:                account.AsOf,
		Currency:            account.Currency,
		SODEquity:           account.SODEquity,
		CashBalance:         account.CashBalance,
		PnL:                 account.PnL,
		DepositsWithdrawals: account.DepositsWithdrawals,
	}

	for _, pos := range account.Positions {
		for _, leg := range pos.Position.Legs {
			accumulate(&snap, leg, pos.Position.U)
		}

		eval, hasEval := evalsByID[pos.ID]
		if !hasEval {
			snap.Evaluations = append(snap.Evaluations, PositionEvaluation{PositionID: pos.ID})
			continue
		}
		snap.Evaluations = append(snap.Evaluations, eval)

		switch {
		case eval.Error != nil:
			snap.Errors = append(snap.Errors, eval)
		case eval.NoRule:
			// Preserved in Evaluations only; no totals.
		case !eval.Result.Permitted:
			snap.Violations = append(snap.Violations, eval)
		default:
			snap.TotalRequirement += eval.Result.Requirement
			snap.TotalCashCall += eval.Result.CashCall
			if kind := eval.Result.DepositKind; kind != "" {
				if snap.DepositRequirements == nil {
					snap.DepositRequirements = make(map[string]float64)
				}
				snap.DepositRequirements[kind] += eval.Result.Requirement
			}
		}
	}

	snap.CurrentEquity = account.SODEquity + account.PnL + account.DepositsWithdrawals
	snap.NetMV = snap.LMVStock + snap.LMVOption - snap.SMVStock - snap.SMVOption
	snap.GrossExposure = snap.LMVStock + snap.LMVOption + snap.SMVStock + snap.SMVOption
	snap.AdjustedBalance = snap.CurrentEquity - (snap.LMVStock + snap.LMVOption) + snap.SMVStock + snap.SMVOption

	switch {
	case snap.CurrentEquity <= 0:
		snap.StockLeverage = 0
		snap.GrossLeverage = 0
		snap.EquityRatio = 0
		snap.Warnings = append(snap.Warnings,
			fmt.Sprintf("current_equity=%g <= 0; leverage and equity_ratio set to 0", snap.CurrentEquity))
	case snap.CurrentEquity < 1.0:
		snap.StockLeverage = math.Inf(1)
		snap.GrossLeverage = math.Inf(1)
		snap.EquityRatio = math.Inf(1)
		snap.Warnings = append(snap.Warnings,
			fmt.Sprintf("current_equity=%g < 1.0; leverage and equity_ratio set to +Inf", snap.CurrentEquity))
	default:
		snap.StockLeverage = (snap.LMVStock + snap.SMVStock) / snap.CurrentEquity
		snap.GrossLeverage = snap.GrossExposure / snap.CurrentEquity
		snap.EquityRatio = snap.TotalRequirement / snap.CurrentEquity
	}

	return snap, nil
}

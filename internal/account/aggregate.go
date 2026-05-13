package account

import "fmt"

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
			continue
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
	snap.AdjustedBalance = snap.CurrentEquity - (snap.LMVStock + snap.LMVOption) + snap.SMVOption

	if snap.CurrentEquity <= 0 {
		snap.StockLeverage = 0
		snap.GrossLeverage = 0
		snap.EquityRatio = 0
		snap.Warnings = append(snap.Warnings,
			fmt.Sprintf("current_equity=%g <= 0; leverage and equity_ratio set to 0", snap.CurrentEquity))
	} else {
		snap.StockLeverage = (snap.LMVStock + snap.SMVStock) / snap.CurrentEquity
		snap.GrossLeverage = snap.GrossExposure / snap.CurrentEquity
		snap.EquityRatio = snap.TotalRequirement / snap.CurrentEquity
	}

	return snap, nil
}

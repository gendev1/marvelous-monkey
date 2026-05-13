package overlay

import (
	"fmt"
	"math"
	"slices"

	"margincalc/internal/account"

	"github.com/google/cel-go/cel"
)

// Evaluate runs the overlay Rulebook against an account snapshot and
// reference data and returns the attributed HouseRequirement. It does
// not mutate acct or snap.
//
// Position-scope modes add / max / floor / block are supported. Group
// scope is supported only for mode=block in this issue; non-block
// group-scope rules are skipped (their evaluation lands with the rest
// of the group-scope path). Account- and symbol-scope rules are also
// skipped. Option positions are skipped per the position-scope issue.
func (e *Engine) Evaluate(
	acct account.Account,
	snap account.AccountSnapshot,
	ref ReferenceData,
) (HouseRequirement, error) {
	out := HouseRequirement{
		AccountID:           acct.ID,
		AsOf:                acct.AsOf,
		Currency:            acct.Currency,
		BaselineRequirement: snap.TotalRequirement,
		BaselineCashCall:    snap.TotalCashCall,
		HouseRequirement:    snap.TotalRequirement,
		HouseCashCall:       snap.TotalCashCall,
		Excess:              snap.CurrentEquity - snap.TotalRequirement,
		Snapshot:            snap,
	}
	rb := e.Rulebook
	if rb != nil {
		out.Audit.OverlayRulebookHash = rb.OverlayRulebookHash
	}
	if rb == nil || len(rb.rules) == 0 {
		return out, nil
	}

	posByID := make(map[string]account.AccountPosition, len(acct.Positions))
	for _, p := range acct.Positions {
		posByID[p.ID] = p
	}

	// stockLikes collects each stock-like position's facts so the
	// group-scope pass can reuse them without re-walking legs.
	var stockLikes []stockLikePos

	for _, eval := range snap.Evaluations {
		if eval.Error != nil {
			continue
		}
		p, ok := posByID[eval.PositionID]
		if !ok {
			continue
		}
		facts := derivePositionFacts(p, eval)
		if !facts.hasStockLike {
			continue
		}
		stockLikes = append(stockLikes, stockLikePos{positionID: p.ID, facts: facts})

		sec, secOK := lookupSecurity(ref, facts.primarySymbol, facts.primaryVenue)
		secMissing := !secOK

		side := sideToken(facts)
		errorViolationEmitted := false
		for _, rule := range rb.rules {
			if rule.Scope != "position" {
				continue
			}
			if !appliesMatches(rule.Applies, acct, sec, side) {
				continue
			}
			// D3: missing-ref + error policy → emit a violation for
			// this rule and skip evaluating it. The position-level
			// warning that would normally cover this case is
			// suppressed below in favor of the violation.
			if secMissing && rule.OnMissingReference == "error" {
				out.Violations = append(out.Violations, HouseViolation{
					RuleID:     rule.ID,
					Scope:      rule.Scope,
					PositionID: p.ID,
					Symbol:     facts.symbol,
					Message: fmt.Sprintf(
						"reference data missing for %s@%s; rule %q requires reference data",
						facts.primarySymbol, facts.primaryVenue, rule.ID,
					),
				})
				errorViolationEmitted = true
				continue
			}

			activation := map[string]any{
				"account":   accountActivation(acct, snap),
				"position":  facts.activation(),
				"security":  securityActivation(sec),
				"constants": rb.constants,
			}
			matched, err := evalBool(rule.whenProg, activation)
			if err != nil {
				out.Warnings = append(out.Warnings,
					fmt.Sprintf("rule %q when eval error on position %s: %v", rule.ID, p.ID, err))
				continue
			}
			if !matched {
				continue
			}

			if rule.Mode == "block" {
				var amount float64
				if rule.formulaProg != nil {
					a, err := evalNumber(rule.formulaProg, activation)
					if err != nil {
						out.Warnings = append(out.Warnings,
							fmt.Sprintf("rule %q formula skipped on position %s: %v", rule.ID, p.ID, err))
						continue
					}
					amount = a
				}
				comp := composeBlockComponent(rule, p.ID, "", facts, amount)
				out.Components = append(out.Components, comp)
				out.Violations = append(out.Violations, HouseViolation{
					RuleID:     rule.ID,
					Scope:      rule.Scope,
					PositionID: p.ID,
					Symbol:     facts.symbol,
					Message:    blockMessage(rule, "position"),
				})
				continue
			}

			amount, err := evalNumber(rule.formulaProg, activation)
			if err != nil {
				out.Warnings = append(out.Warnings,
					fmt.Sprintf("rule %q formula skipped on position %s: %v", rule.ID, p.ID, err))
				continue
			}
			comp := composeComponent(rule, p.ID, facts, amount)
			out.Components = append(out.Components, comp)
			if comp.Applied {
				out.HouseRequirement += comp.Delta
				out.HouseCashCall += comp.Delta
			}
		}

		// Per D3: the missing-ref warning is emitted at most once per
		// position, and it is suppressed when an `error`-policy
		// violation already covers the same condition.
		if secMissing && !errorViolationEmitted {
			out.Warnings = append(out.Warnings,
				fmt.Sprintf("reference data missing for %s@%s; defaulted instrument_kind=stock",
					facts.primarySymbol, facts.primaryVenue))
		}
	}

	// Group-scope block pass. Only block-mode rules are evaluated here;
	// add/max/floor at group scope land with the rest of the group path.
	for _, rule := range rb.rules {
		if rule.Scope != "group" || rule.Mode != "block" {
			continue
		}
		if !groupAppliesMatches(rule.Applies, acct) {
			continue
		}
		groups := groupFactsByRule(rule, stockLikes)
		for _, gf := range groups {
			activation := map[string]any{
				"account":   accountActivation(acct, snap),
				"group":     gf.activation(),
				"position":  emptyFactMap(),
				"security":  emptyFactMap(),
				"constants": rb.constants,
			}
			matched, err := evalBool(rule.whenProg, activation)
			if err != nil {
				out.Warnings = append(out.Warnings,
					fmt.Sprintf("rule %q when eval error on group %s: %v", rule.ID, gf.key, err))
				continue
			}
			if !matched {
				continue
			}
			var amount float64
			if rule.formulaProg != nil {
				a, err := evalNumber(rule.formulaProg, activation)
				if err != nil {
					out.Warnings = append(out.Warnings,
						fmt.Sprintf("rule %q formula skipped on group %s: %v", rule.ID, gf.key, err))
					continue
				}
				amount = a
			}
			comp := composeGroupBlockComponent(rule, gf, amount)
			out.Components = append(out.Components, comp)
			out.Violations = append(out.Violations, HouseViolation{
				RuleID:   rule.ID,
				Scope:    rule.Scope,
				GroupKey: gf.key,
				Message:  blockMessage(rule, "group"),
			})
		}
	}

	out.Excess = snap.CurrentEquity - out.HouseRequirement
	return out, nil
}

// appliesMatches enforces each list-typed applies filter. An empty list
// means "no filter, all values pass". A position with shares on both
// sides simultaneously is treated as a non-match when applies.sides is
// set — combo decomposition is out of scope this issue.
func appliesMatches(spec AppliesSpec, acct account.Account, sec SecurityFacts, side string) bool {
	if len(spec.AccountTypes) > 0 && !contains(spec.AccountTypes, string(acct.AccountType)) {
		return false
	}
	if len(spec.Phases) > 0 && !contains(spec.Phases, string(acct.Phase)) {
		return false
	}
	if len(spec.InstrumentKinds) > 0 && !contains(spec.InstrumentKinds, sec.InstrumentKind) {
		return false
	}
	if len(spec.Sides) > 0 {
		if side == "" {
			return false
		}
		if !contains(spec.Sides, side) {
			return false
		}
	}
	return true
}

// groupAppliesMatches enforces the subset of applies filters that make
// sense at group scope: account type and phase. Instrument-kind and
// side filters are per-position; group-scope block rules don't carry
// them.
func groupAppliesMatches(spec AppliesSpec, acct account.Account) bool {
	if len(spec.AccountTypes) > 0 && !contains(spec.AccountTypes, string(acct.AccountType)) {
		return false
	}
	if len(spec.Phases) > 0 && !contains(spec.Phases, string(acct.Phase)) {
		return false
	}
	return true
}

func contains(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}

// lookupSecurity returns (facts, true) when ReferenceData has a row for
// the given symbol/venue. On miss it returns a synthetic SecurityFacts
// with InstrumentKind = "stock" and the symbol/venue filled per D3.
func lookupSecurity(ref ReferenceData, symbol, venue string) (SecurityFacts, bool) {
	key := SecKey{Symbol: symbol, Venue: venue}
	if ref.Securities != nil {
		if sec, ok := ref.Securities[key]; ok {
			if sec.InstrumentKind == "" {
				sec.InstrumentKind = "stock"
			}
			return sec, true
		}
	}
	return SecurityFacts{
		Symbol:         symbol,
		Venue:          venue,
		InstrumentKind: "stock",
	}, false
}

func accountActivation(acct account.Account, snap account.AccountSnapshot) map[string]any {
	return map[string]any{
		"id":                 acct.ID,
		"account_type":       string(acct.AccountType),
		"phase":              string(acct.Phase),
		"currency":           acct.Currency,
		"current_equity":     snap.CurrentEquity,
		"sod_equity":         snap.SODEquity,
		"cash_balance":       snap.CashBalance,
		"adjusted_balance":   snap.AdjustedBalance,
		"total_requirement":  snap.TotalRequirement,
		"total_cash_call":    snap.TotalCashCall,
		"gross_exposure":     snap.GrossExposure,
		"net_market_value":   snap.NetMV,
		"long_market_value":  snap.LMVStock + snap.LMVOption,
		"short_market_value": snap.SMVStock + snap.SMVOption,
		"stock_leverage":     snap.StockLeverage,
		"gross_leverage":     snap.GrossLeverage,
	}
}

func securityActivation(sec SecurityFacts) map[string]any {
	return map[string]any{
		"symbol":             sec.Symbol,
		"venue":              sec.Venue,
		"instrument_kind":    sec.InstrumentKind,
		"underlying":         sec.Underlying,
		"issuer_id":          sec.IssuerID,
		"sector":             sec.Sector,
		"industry":           sec.Industry,
		"gics_sub_industry":  sec.GICSSubIndustry,
		"last_price":         sec.LastPrice,
		"adv_20":             sec.ADV20,
		"median_volume_20":   sec.MedianVolume20,
		"market_cap":         sec.MarketCap,
		"shares_outstanding": sec.SharesOutstanding,
		"volatility_30d":     sec.Volatility30D,
		"hard_to_borrow":     sec.HardToBorrow,
		"borrow_rate":        sec.BorrowRate,
		"marginable":         sec.Marginable,
		"leveraged_factor":   sec.LeveragedFactor,
	}
}

func emptyFactMap() map[string]any {
	return map[string]any{}
}

func evalBool(prog cel.Program, activation map[string]any) (bool, error) {
	out, _, err := prog.Eval(activation)
	if err != nil {
		return false, err
	}
	v, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("when did not return bool")
	}
	return v, nil
}

func evalNumber(prog cel.Program, activation map[string]any) (float64, error) {
	if prog == nil {
		return 0, fmt.Errorf("no formula program")
	}
	out, _, err := prog.Eval(activation)
	if err != nil {
		return 0, err
	}
	var v float64
	switch x := out.Value().(type) {
	case float64:
		v = x
	case int64:
		v = float64(x)
	case int:
		v = float64(x)
	default:
		return 0, fmt.Errorf("formula did not return a number, got %T", out.Value())
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("formula produced non-finite value")
	}
	return v, nil
}

// composeComponent fills the HouseComponent for one matched rule. Mode
// semantics:
//
//   - add  : Delta = OverlayAmount, always Applied.
//   - max  : Delta = max(0, OverlayAmount - BaselineAmount), Applied
//     when Delta > 0.
//   - floor: numerically identical to max; Mode records "floor" for
//     audit attribution.
//
// BaselineAmount on max/floor is the position's baseline requirement
// from the snapshot evaluation (0 when none).
func composeComponent(rule overlayRule, positionID string, facts positionFacts, amount float64) HouseComponent {
	comp := HouseComponent{
		RuleID:        rule.ID,
		Scope:         rule.Scope,
		Mode:          rule.Mode,
		Basis:         rule.Basis,
		PositionID:    positionID,
		Symbol:        facts.symbol,
		OverlayAmount: amount,
		Formula:       rule.Formula,
		Reason:        rule.Reason,
		Evidence: map[string]float64{
			"position.long_market_value":    facts.longMV,
			"position.short_market_value":   facts.shortMV,
			"position.gross_market_value":   facts.grossMV,
			"position.net_market_value":     facts.netMV,
			"position.long_shares":          facts.longShares,
			"position.short_shares":         facts.shortShares,
			"position.baseline_requirement": facts.baselineReq,
			"overlay.amount":                amount,
		},
	}
	switch rule.Mode {
	case "add":
		comp.Delta = amount
		comp.Applied = true
	case "max", "floor":
		comp.BaselineAmount = facts.baselineReq
		delta := amount - facts.baselineReq
		if delta < 0 {
			delta = 0
		}
		comp.Delta = delta
		comp.Applied = delta > 0
	}
	return comp
}

// composeBlockComponent builds the HouseComponent for a position-scope
// block-mode match. Block components carry Delta = 0 and Applied = true
// so the audit log records the match without moving the requirement.
// BaselineAmount is informational. OverlayAmount records the formula
// value when the rule carried one (zero otherwise).
func composeBlockComponent(rule overlayRule, positionID, groupKey string, facts positionFacts, amount float64) HouseComponent {
	return HouseComponent{
		RuleID:         rule.ID,
		Scope:          rule.Scope,
		Mode:           rule.Mode,
		Basis:          rule.Basis,
		PositionID:     positionID,
		Symbol:         facts.symbol,
		GroupKey:       groupKey,
		BaselineAmount: facts.baselineReq,
		OverlayAmount:  amount,
		Delta:          0,
		Applied:        true,
		Formula:        rule.Formula,
		Reason:         rule.Reason,
		Evidence: map[string]float64{
			"position.long_market_value":    facts.longMV,
			"position.short_market_value":   facts.shortMV,
			"position.gross_market_value":   facts.grossMV,
			"position.net_market_value":     facts.netMV,
			"position.long_shares":          facts.longShares,
			"position.short_shares":         facts.shortShares,
			"position.baseline_requirement": facts.baselineReq,
			"overlay.amount":                amount,
		},
	}
}

// composeGroupBlockComponent builds the HouseComponent for a
// group-scope block match. GroupKey is set; PositionID and Symbol are
// left empty so consumers can distinguish group attributions.
func composeGroupBlockComponent(rule overlayRule, gf groupFacts, amount float64) HouseComponent {
	return HouseComponent{
		RuleID:        rule.ID,
		Scope:         rule.Scope,
		Mode:          rule.Mode,
		Basis:         rule.Basis,
		GroupKey:      gf.key,
		OverlayAmount: amount,
		Delta:         0,
		Applied:       true,
		Formula:       rule.Formula,
		Reason:        rule.Reason,
		Evidence: map[string]float64{
			"group.gross_market_value": gf.grossMV,
			"group.long_market_value":  gf.longMV,
			"group.short_market_value": gf.shortMV,
			"group.position_count":     float64(gf.positionCount),
			"overlay.amount":           amount,
		},
	}
}

// blockMessage returns the user-facing violation message for a
// block-mode match: rule.Reason if set, otherwise a generated default.
func blockMessage(rule overlayRule, scope string) string {
	if rule.Reason != "" {
		return rule.Reason
	}
	return fmt.Sprintf("%s blocked by rule %q", scope, rule.ID)
}

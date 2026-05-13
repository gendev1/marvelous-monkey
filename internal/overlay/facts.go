package overlay

import (
	"margincalc/internal/account"
	"margincalc/internal/engine"
)

// positionFacts is the per-position fact namespace exposed to CEL rules
// under `position.*`. Numeric fields are float64 to match CEL's strict
// double typing; string fields (id, symbol) ride along for evidence and
// future scope tests.
//
// Combo positions — a position with both long and short shares
// simultaneously — are decomposed at the account layer; this issue
// treats them as out of scope and computes both buckets honestly so a
// future PR can decide whether to bind such a position to "long" or
// "short" applies.sides filtering. TODO(#43-followup): combo
// decomposition policy lands with group scope.
type positionFacts struct {
	id            string
	symbol        string
	longShares    float64
	shortShares   float64
	longMV        float64
	shortMV       float64
	netMV         float64
	grossMV       float64
	baselineReq   float64
	baselineCash  float64
	primaryVenue  string
	primarySymbol string
	hasStockLike  bool
}

// derivePositionFacts walks a position's legs and returns the
// per-position fact map used by both the applies-matrix filter and the
// CEL activation. Option legs are excluded (option positions are out
// of scope this issue). For multi-leg stock positions every stock-like
// leg's MV magnitude is summed into the side-appropriate bucket.
//
// Per D4: the primary symbol is leg.Underlying when set on a stock-
// like leg, falling back to the empty string. The venue is taken from
// the first stock-like leg.
func derivePositionFacts(p account.AccountPosition, eval account.PositionEvaluation) positionFacts {
	facts := positionFacts{
		id:           p.ID,
		baselineReq:  eval.Result.Requirement,
		baselineCash: eval.Result.CashCall,
	}
	pos := p.Position
	for _, leg := range pos.Legs {
		if leg.Kind == engine.OptionKind {
			continue
		}
		mv := stockLikeMV(leg, pos.U)
		if !facts.hasStockLike {
			facts.primaryVenue = leg.Venue
			facts.primarySymbol = leg.Underlying
			facts.hasStockLike = true
		}
		if leg.Side == engine.Short {
			facts.shortShares += leg.Shares
			facts.shortMV += mv
		} else {
			facts.longShares += leg.Shares
			facts.longMV += mv
		}
	}
	facts.grossMV = facts.longMV + facts.shortMV
	facts.netMV = facts.longMV - facts.shortMV
	facts.symbol = facts.primarySymbol
	return facts
}

// stockLikeMV mirrors account/market_value.go's legMarketValue but is
// inlined here so the overlay package does not reach into the account
// package's unexported helper. The two stay in sync via the test
// `TestFacts_StockMVMatchesAccountLayer` in evaluate_test.go.
func stockLikeMV(leg engine.Leg, u float64) float64 {
	switch leg.Kind {
	case engine.StockKind:
		return u * leg.Shares
	case engine.ETFKind, engine.ETNKind, engine.ConvertibleKind, engine.WarrantKind:
		return leg.Price * leg.Shares
	}
	return 0
}

// sideToken returns the canonical applies.sides token for a position
// based on its derived facts. A position with shares on both sides
// returns "" so the caller can apply the documented combo policy
// (skip applies.sides filtering for now).
func sideToken(f positionFacts) string {
	long := f.longShares > 0
	short := f.shortShares > 0
	switch {
	case long && !short:
		return "long"
	case short && !long:
		return "short"
	default:
		return ""
	}
}

// factsActivation builds the `position` namespace for CEL evaluation.
func (f positionFacts) activation() map[string]any {
	return map[string]any{
		"id":                   f.id,
		"symbol":               f.symbol,
		"long_market_value":    f.longMV,
		"short_market_value":   f.shortMV,
		"gross_market_value":   f.grossMV,
		"net_market_value":     f.netMV,
		"long_shares":          f.longShares,
		"short_shares":         f.shortShares,
		"baseline_requirement": f.baselineReq,
	}
}

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
		if !isStockLikeKind(leg.Kind) {
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

// isStockLikeKind is the closed set of leg kinds the overlay's
// position-scope evaluator recognizes. Filtering on this set (rather
// than only on `!= OptionKind`) means a future leg kind added to the
// engine cannot accidentally start triggering position-scope rules
// here before the overlay's facts/activation surface has been
// reviewed for it.
func isStockLikeKind(kind engine.Kind) bool {
	switch kind {
	case engine.StockKind, engine.ETFKind, engine.ETNKind, engine.ConvertibleKind, engine.WarrantKind:
		return true
	default:
		return false
	}
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

// groupFacts is the per-group fact namespace exposed to CEL rules under
// `group.*`. Built from the stock-like positions sharing a group key
// (the `group_by` field on a group-scope rule, either "underlying" or
// "symbol"). Group-scope evaluation lands incrementally; this issue
// uses it only for block-mode rules.
type groupFacts struct {
	key           string
	longMV        float64
	shortMV       float64
	grossMV       float64
	netMV         float64
	positionCount int
}

// activation builds the `group` namespace for CEL evaluation.
func (g groupFacts) activation() map[string]any {
	return map[string]any{
		"key":                g.key,
		"long_market_value":  g.longMV,
		"short_market_value": g.shortMV,
		"gross_market_value": g.grossMV,
		"net_market_value":   g.netMV,
		"position_count":     int64(g.positionCount),
	}
}

// stockLikePos pairs a position ID with its derived facts so the
// group-scope pass can reuse the work done by the position-scope pass.
type stockLikePos struct {
	positionID string
	facts      positionFacts
}

// groupFactsByRule partitions the stock-like positions into groups
// keyed by the rule's GroupBy attribute and returns the per-group
// aggregates in deterministic key order. A position with no key for the
// chosen attribute (empty underlying/symbol) is skipped — a group with
// no identity cannot be referenced by a rule's audit trail.
func groupFactsByRule(rule overlayRule, positions []stockLikePos) []groupFacts {
	bucket := map[string]*groupFacts{}
	var order []string
	for _, sp := range positions {
		var key string
		switch rule.GroupBy {
		case "underlying":
			key = sp.facts.primarySymbol
		case "symbol":
			key = sp.facts.primarySymbol
		default:
			continue
		}
		if key == "" {
			continue
		}
		gf, ok := bucket[key]
		if !ok {
			gf = &groupFacts{key: key}
			bucket[key] = gf
			order = append(order, key)
		}
		gf.longMV += sp.facts.longMV
		gf.shortMV += sp.facts.shortMV
		gf.grossMV += sp.facts.grossMV
		gf.netMV += sp.facts.netMV
		gf.positionCount++
	}
	out := make([]groupFacts, 0, len(order))
	for _, k := range order {
		out = append(out, *bucket[k])
	}
	return out
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

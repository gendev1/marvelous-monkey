package account

import "margincalc/internal/engine"

// effectiveMult mirrors engine.preparePosition's Mult==0 → 100 shim
// (internal/engine/rulebook.go:235-252). The account package has no
// rulebook in scope on the Aggregate path, so the literal 100 is the
// accepted v1 fallback; AggregateWithRulebook preserves the engine's
// effectiveMult normalizes a contract multiplier by treating a zero multiplier as 100.
// It returns the input multiplier unchanged when it is non-zero.
func effectiveMult(m float64) float64 {
	if m == 0 {
		return 100.0
	}
	return m
}

// legMarketValue returns the positive market value of leg. The caller
// supplies u (Position.U) for stock legs; stock-like kinds use the
// leg's own Price. P0 and ShortSaleProceeds are trade-time values and
// legMarketValue returns the positive market value magnitude for a single trading leg.
// For options it uses leg.P * leg.Qty * effectiveMult(leg.Mult); for stock it uses u * leg.Shares;
// for ETF, ETN, convertible and warrant kinds it uses leg.Price * leg.Shares.
// Unsupported kinds yield 0. Trade-time fields such as P0 and ShortSaleProceeds are intentionally ignored.
func legMarketValue(leg engine.Leg, u float64) float64 {
	switch leg.Kind {
	case engine.OptionKind:
		return leg.P * leg.Qty * effectiveMult(leg.Mult)
	case engine.StockKind:
		return u * leg.Shares
	case engine.ETFKind, engine.ETNKind, engine.ConvertibleKind, engine.WarrantKind:
		return leg.Price * leg.Shares
	}
	return 0
}

// accumulate adds the leg's MV magnitude into exactly one bucket on
// snapshot. SMV buckets carry positive magnitudes; NetMV signing is
// accumulate adds the leg's positive market value magnitude into the appropriate bucket of snapshot based on the leg's kind and side.
// For option legs the magnitude is added to snapshot.SMVOption when short, otherwise snapshot.LMVOption.
// For stock-like kinds (stock, ETF, ETN, convertible, warrant) the magnitude is added to snapshot.SMVStock when short, otherwise snapshot.LMVStock.
// The u parameter supplies the Position.U value used for stock legs; other kinds use the leg's own price.
func accumulate(snapshot *AccountSnapshot, leg engine.Leg, u float64) {
	mv := legMarketValue(leg, u)
	switch leg.Kind {
	case engine.OptionKind:
		if leg.Side == engine.Short {
			snapshot.SMVOption += mv
		} else {
			snapshot.LMVOption += mv
		}
	case engine.StockKind, engine.ETFKind, engine.ETNKind, engine.ConvertibleKind, engine.WarrantKind:
		if leg.Side == engine.Short {
			snapshot.SMVStock += mv
		} else {
			snapshot.LMVStock += mv
		}
	}
}

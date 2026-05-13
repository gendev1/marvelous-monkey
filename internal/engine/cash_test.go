package engine

import "testing"

// Cash-account coverage tests. The CBOE manual treats cash accounts via
// deposit (cash, escrow, or shares) rather than a margin formula for most
// short-option positions; these tests cover the cases where the engine
// surfaces a USD-equivalent number alongside the deposit_kind, and where
// it refuses outright (Permitted=false).

// Short put in a cash account: manual requires aggregate exercise price on
// deposit (K*qty*mult). Premium received is surfaced as proceeds; cash_call
// = aggregate strike - premium = the net deposit beyond the credit.
func TestShortPutCash_aggregateStrike(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U: 95.0, Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 80, P: 2.0, P0: 2.0, Qty: 1, Mult: 100},
		},
	}
	res := mustEvaluate(t, rb, pos, CashAccount, Initial)
	if res.RuleID != "short_put_uncovered" {
		t.Errorf("matched %s, want short_put_uncovered", res.RuleID)
	}
	assertClose(t, "cash short put gross", res.Requirement, 8000.00)
	assertClose(t, "cash short put proceeds", res.AppliedProceeds, 200.00)
	assertClose(t, "cash short put net", res.CashCall, 7800.00)
	if res.DepositKind != "cash_or_escrow" {
		t.Errorf("deposit_kind=%q, want cash_or_escrow", res.DepositKind)
	}
}

// Short call in a cash account: deposit is shares (underlying-or-escrow); the
// number we surface is the USD-equivalent of those shares (U*qty*mult).
func TestShortCallCash_underlyingValue(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U: 50.0, Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 55, P: 1.0, P0: 1.0, Qty: 2, Mult: 100},
		},
	}
	res := mustEvaluate(t, rb, pos, CashAccount, Initial)
	assertClose(t, "cash short call gross", res.Requirement, 50.0*2*100)
	assertClose(t, "cash short call proceeds", res.AppliedProceeds, 1.0*2*100)
	if res.DepositKind != "underlying_or_escrow" {
		t.Errorf("deposit_kind=%q, want underlying_or_escrow", res.DepositKind)
	}
}

// Short strangle in cash: must be refused (different collateral kinds).
func TestShortStrangleCash_refused(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U: 100.0, Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 95, P: 1.5, P0: 1.5, Qty: 1, Mult: 100, Underlying: "X"},
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 105, P: 1.5, P0: 1.5, Qty: 1, Mult: 100, Underlying: "X"},
		},
	}
	res := mustEvaluate(t, rb, pos, CashAccount, Initial)
	if res.RuleID != "short_strangle_or_straddle" {
		t.Errorf("matched %s", res.RuleID)
	}
	if res.Permitted {
		t.Errorf("cash strangle should not be permitted")
	}
}

// Cash-secured vertical call spread: max loss on deposit + long premium,
// with short premium as proceeds.
func TestVerticalCallSpreadCash_p42(t *testing.T) {
	rb := loadRB(t)
	// Long Nov 60 call @ 12, Short Nov 70 call @ 4, U=72.
	// MPL of a long debit call spread is 0 (can only profit at expiry).
	// Long premium=$1200, short premium=$400.
	// cash gross = mpl + long_prem = 0 + 1200 = 1200. proceeds = 400.
	// cash_call = 800.
	pos := Position{
		U: 72.0, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call",
				K: 60, P: 12.0, P0: 12.0, Qty: 1, Mult: 100,
				Style: "american", Venue: "listed", Underlying: "X", Expiration: "2026-11-20"},
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 70, P: 4.0, P0: 4.0, Qty: 1, Mult: 100,
				Style: "american", Venue: "listed", Underlying: "X", Expiration: "2026-11-20"},
		},
	}
	res := mustEvaluate(t, rb, pos, CashAccount, Initial)
	if res.RuleID != "vertical_spread" {
		t.Errorf("matched %s, want vertical_spread", res.RuleID)
	}
	assertClose(t, "cash vertical call spread gross", res.Requirement, 1200.00)
	assertClose(t, "cash vertical call spread proceeds", res.AppliedProceeds, 400.00)
	assertClose(t, "cash vertical call spread net", res.CashCall, 800.00)
}

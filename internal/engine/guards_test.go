package engine

import "testing"

// Guard tests cover the constraint predicates that prevent a rule from
// matching even when the leg shape would otherwise bind. These are the
// hardest cases to get right: silent over-matching produces a confidently
// wrong number, which is worse than refusing.
//
// Two guards live in the rulebook today:
//   - long_option_long_dated_listed restricts to equity / equity-ETF /
//     index / vol-index classes (ETNs and other classes refuse).
//   - generic_limited_risk_combo requires is_limited_risk(legs) so that
//     unbounded ratio spreads don't silently get an MPL computed only at
//     existing strikes.

// Equity-class long-dated listed option gets the manual's 75% loan-value path.
func TestLongDatedListed_equity_75pct(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U: 100.0, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call",
				K: 100, P: 12.0, P0: 10.0, Qty: 1, Mult: 100,
				Style: "american", Venue: "listed",
				TimeToExpirationMonths: 18.0},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	if res.RuleID != "long_option_long_dated_listed" {
		t.Fatalf("matched %s, want long_option_long_dated_listed", res.RuleID)
	}
	assertClose(t, "long-dated listed equity initial", res.Requirement, 0.75*12.0*1*100)
}

// ETN-class long-dated listed option: manual does not extend 75% to ETNs
// (debt instrument, not equity-based). Rule must NOT match; no other rule
// covers this either, so Evaluate returns a no-match error.
func TestLongDatedListed_etnRefused(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U: 30.0, Class: "etn_broad",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call",
				K: 30, P: 4.0, P0: 3.5, Qty: 1, Mult: 100,
				Style: "american", Venue: "listed",
				TimeToExpirationMonths: 12.0},
		},
	}
	mustReject(t, rb, pos, MarginAccount, Initial)
}

// Guard test: a 3-leg net-short-call structure (long 50C, short 60C, short 70C)
// is unbounded as U → ∞, but its loss evaluated only at strikes {50,60,70} is
// finite — so without the is_limited_risk guard, generic_limited_risk_combo
// would silently match and return a wrong (finite) MPL. With the guard it must
// NOT match — and since no specific rule handles ratio spreads either,
// Evaluate returns a no-match error. That refusal is the desired behaviour.
func TestRatioSpread_isLimitedRiskGuardRejects(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     58.0,
		Class: "equity",
		Lev:   1.0,
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call",
				K: 50, P: 9.0, P0: 9.0, Qty: 1, Mult: 100, Style: "american"},
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 60, P: 2.0, P0: 2.0, Qty: 1, Mult: 100, Style: "american"},
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 70, P: 0.5, P0: 0.5, Qty: 1, Mult: 100, Style: "american"},
		},
	}
	mustReject(t, rb, pos, MarginAccount, Initial)
}

// Net-short PUT structure that IS bounded at U=0: long 1 put K=100,
// short 2 puts K=80, plus a long call K=130 to satisfy min_legs:3.
// Previously rejected because the old guard summed both call AND put exposure;
// puts are bounded by U=0 (mpl now samples that point), so only net call
// exposure can be truly unbounded. Net call exposure here is +100 ≥ 0, so the
// guard must accept and generic_limited_risk_combo must match.
//
// At the sample points:
//
//	U=0:   long put +10000, short puts -16000, long call 0   → -6000
//	U=80:  long put +2000, short puts 0, long call 0         → +2000
//	U=100: 0
//	U=130: 0
//
// MPL = 6000. With premiums zero, Requirement = MPL + 0 = 6000.
func TestBoundedPutRatio_isLimitedRiskGuardAccepts(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     90.0,
		Class: "equity",
		Lev:   1.0,
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "put",
				K: 100, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ"},
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 80, Qty: 2, Mult: 100, Style: "american", Underlying: "XYZ"},
			{Side: Long, Kind: OptionKind, OptionType: "call",
				K: 130, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ"},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	if res.RuleID != "generic_limited_risk_combo" {
		t.Errorf("matched %s, want generic_limited_risk_combo", res.RuleID)
	}
	assertClose(t, "bounded put ratio (gross)", res.Requirement, 6000.00)
}

// Mismatched-multiplier call spread: long 1 call K=100 mult=10 (mini),
// short 1 call K=110 mult=100 (standard). Raw quantity nets to zero so the
// old guard (summing qty only) accepted, but qty*mult nets to -90 — at
// U → ∞ the short standard loses 100× per unit while the long mini gains
// only 10× per unit, so the structure is truly unbounded. The third leg
// (long put K=90) is inert: it satisfies min_legs:3 and doesn't change call
// exposure. New guard must reject.
func TestMismatchedMultiplierCallSpread_isLimitedRiskGuardRejects(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     105.0,
		Class: "equity",
		Lev:   1.0,
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call",
				K: 100, Qty: 1, Mult: 10, Style: "american"},
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 110, Qty: 1, Mult: 100, Style: "american"},
			{Side: Long, Kind: OptionKind, OptionType: "put",
				K: 90, Qty: 1, Mult: 100, Style: "american"},
		},
	}
	mustReject(t, rb, pos, MarginAccount, Initial)
}

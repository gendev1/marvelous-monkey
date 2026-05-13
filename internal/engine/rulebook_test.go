package engine

import (
	"math"
	"testing"
)

const rulesPath = "../../rules/cboe_baseline.yaml"

// Helper: load once, share across tests.
var rb *Rulebook

func loadRB(t *testing.T) *Rulebook {
	t.Helper()
	if rb != nil {
		return rb
	}
	x, err := LoadRulebook(rulesPath)
	if err != nil {
		t.Fatalf("LoadRulebook: %v", err)
	}
	rb = x
	return rb
}

func assertClose(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Errorf("%s: got %.4f, want %.4f", label, got, want)
	}
}

// mustEvaluate runs Evaluate and cross-checks with EvaluateAll that *exactly*
// one rule matched. Length>1 means rule declaration order is silently shadowing
// one rule with another — production callers wouldn't notice, but tests should.
// Every fixture in this package goes through this helper so the safety net
// stays populated as new rules are added.
func mustEvaluate(t *testing.T, rb *Rulebook, pos Position, acct AccountType, phase Phase) Result {
	t.Helper()
	res, err := rb.Evaluate(pos, acct, phase)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	all, err := rb.EvaluateAll(pos, acct, phase)
	if err != nil {
		t.Fatalf("EvaluateAll: %v", err)
	}
	if len(all) != 1 {
		ids := make([]string, len(all))
		for i, r := range all {
			ids[i] = r.RuleID
		}
		t.Fatalf("rule dispatch ambiguous: %d rules matched (%v); Evaluate picked %s", len(all), ids, res.RuleID)
	}
	return res
}

// mustReject asserts the position matches no rule: Evaluate returns the
// no-match error AND EvaluateAll returns an empty slice. Use in guard tests.
func mustReject(t *testing.T, rb *Rulebook, pos Position, acct AccountType, phase Phase) {
	t.Helper()
	if _, err := rb.Evaluate(pos, acct, phase); err == nil {
		t.Fatalf("expected no-match, but Evaluate succeeded")
	}
	all, err := rb.EvaluateAll(pos, acct, phase)
	if err != nil {
		t.Fatalf("EvaluateAll: %v", err)
	}
	if len(all) != 0 {
		ids := make([]string, len(all))
		for i, r := range all {
			ids[i] = r.RuleID
		}
		t.Fatalf("expected no-match, but %d rules matched: %v", len(all), ids)
	}
}

// p.28: Short 1 Sep 80 put at 2.00, underlying at 95 (OTM)
// Expected: $1,000 (minimum formula binds: 200 + 800)
func TestShortPutOTM_p28(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     95.0,
		Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 80, P: 2.0, P0: 2.0, Qty: 1, Mult: 100},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	if res.RuleID != "short_put_uncovered" {
		t.Errorf("matched %s, want short_put_uncovered", res.RuleID)
	}
	assertClose(t, "p28 short put OTM initial", res.Requirement, 1000.00)
}

// p.28: Short 1 Jan 20 put at 1.50, underlying at 19.50 (ITM)
// Expected: $540 (basic formula binds: 150 + 390)
func TestShortPutITM_p28(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     19.50,
		Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 20, P: 1.50, P0: 1.50, Qty: 1, Mult: 100},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	assertClose(t, "p28 short put ITM initial", res.Requirement, 540.00)
}

// p.32: Short 1 Nov 120 call at 8.40, underlying at 128.50 (ITM)
// Expected: $3,410 (basic formula: 840 + 2570)
func TestShortCallITM_p32(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     128.50,
		Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 120, P: 8.40, P0: 8.40, Qty: 1, Mult: 100},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	if res.RuleID != "short_call_uncovered" {
		t.Errorf("matched %s, want short_call_uncovered", res.RuleID)
	}
	assertClose(t, "p32 short call ITM initial", res.Requirement, 3410.00)
}

// p.32: Short 1 Feb 30 call at .05, underlying at 26.38 (OTM)
// Expected: $268.80 (minimum formula: 5 + 263.80)
func TestShortCallOTM_p32(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     26.38,
		Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 30, P: 0.05, P0: 0.05, Qty: 1, Mult: 100},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	assertClose(t, "p32 short call OTM initial", res.Requirement, 268.80)
}

// p.29: Leveraged ETF short put. K=725, U=970, P=3, lev=2.0 (OTM)
// Expected: $14,800 (minimum formula at 20% strike binds)
func TestLeveragedETFShortPut_p29(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     970.0,
		Class: "etf_narrow",
		Lev:   2.0,
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 725, P: 3.0, P0: 3.0, Qty: 1, Mult: 100},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	assertClose(t, "p29 leveraged ETF short put", res.Requirement, 14800.00)
}

// p.30: Broad-based ETF short put. K=410, U=445.35, P=.10 (OTM)
// Expected: $4,110 (uses 15%/10%; minimum binds at 10% × strike)
func TestBroadETFShortPut_p30(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     445.35,
		Class: "etf_broad",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 410, P: 0.10, P0: 0.10, Qty: 1, Mult: 100},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	assertClose(t, "p30 broad-ETF short put", res.Requirement, 4110.00)
}

// p.34: Broad-based index short call ITM. K=430, U=433.35, P=8.70
// Expected: $7,370.25 (basic at 15%)
func TestBroadIndexShortCallITM_p34(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     433.35,
		Class: "broad_based_index",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 430, P: 8.70, P0: 8.70, Qty: 1, Mult: 100},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	assertClose(t, "p34 broad index short call ITM", res.Requirement, 7370.25)
}

// p.47: Covered call. Long 100 @ 92.38, Short Dec 90 call.
// Initial: 0.50 × 92.38 × 100 = $4,619
func TestCoveredCallInitial_p47(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     92.38,
		Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 90, P: 7.0, P0: 7.0, Qty: 1, Mult: 100, Style: "american"},
			{Side: Long, Kind: StockKind, Shares: 100},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	if res.RuleID != "covered_call" {
		t.Errorf("matched %s, want covered_call", res.RuleID)
	}
	assertClose(t, "p47 covered call initial", res.Requirement, 4619.00)
}

// p.47: Short put + short stock. Short 100 @ 255, Short Nov 250 put at 3
// Maintenance: 100% × 255 × 100 + max(5×100, 30%×255×100) = 25500 + 7650 = 33150
// Plus put-ITM excess: max(0, 250-255)×100 = 0
// Total: 33,150
func TestShortPutShortStockMaintenance_p47(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     255.0,
		Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 250, P: 3.0, P0: 3.0, Qty: 1, Mult: 100, Style: "american"},
			{Side: Short, Kind: StockKind, Shares: 100, ShortSaleProceeds: 25500, SalePrice: 255},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Maintenance)
	if res.RuleID != "short_put_short_stock" {
		t.Errorf("matched %s, want short_put_short_stock", res.RuleID)
	}
	assertClose(t, "p47 short put + short stock maintenance", res.Requirement, 33150.00)
}

// p.59: Conversion maintenance. Long 100 XYZ @ 115, Short May 110 call, Long May 110 put.
// Maintenance: 10% × 110 × 100 = $1,100
func TestConversionMaintenance_p59(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     115.0,
		Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "put",
				K: 110, P: 1.35, P0: 1.35, Qty: 1, Mult: 100, Style: "american", Expiration: "2024-05-17", Underlying: "XYZ"},
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 110, P: 6.50, P0: 6.50, Qty: 1, Mult: 100, Style: "american", Expiration: "2024-05-17", Underlying: "XYZ"},
			{Side: Long, Kind: StockKind, Shares: 100},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Maintenance)
	if res.RuleID != "conversion" {
		t.Errorf("matched %s, want conversion", res.RuleID)
	}
	assertClose(t, "p59 conversion maintenance", res.Requirement, 1100.00)
}

// p.60: Reverse conversion maintenance. Short 100 XYZ @ 115, Long May 110 call, Short May 110 put.
// Maintenance: 1.10 × 110 × 100 + max(0, 110-115)×100 = 12,100 + 0 = $12,100
func TestReverseConversionMaintenance_p60(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     115.0,
		Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call",
				K: 110, P: 6.50, P0: 6.50, Qty: 1, Mult: 100, Style: "american", Expiration: "2024-05-17", Underlying: "XYZ"},
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 110, P: 1.35, P0: 1.35, Qty: 1, Mult: 100, Style: "american", Expiration: "2024-05-17", Underlying: "XYZ"},
			{Side: Short, Kind: StockKind, Shares: 100, ShortSaleProceeds: 11500, SalePrice: 115},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Maintenance)
	if res.RuleID != "reverse_conversion" {
		t.Errorf("matched %s, want reverse_conversion", res.RuleID)
	}
	assertClose(t, "p60 reverse conversion maintenance", res.Requirement, 12100.00)
}

// p.61: Reverse conversion ITM put.
// Short 100 @ 71.90, Long Dec 75 call, Short Dec 75 put.
// Maintenance: 1.10 × 75 × 100 + max(0, 75-71.90)×100 = 8,250 + 310 = $8,560
func TestReverseConversionITMPut_p61(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     71.90,
		Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call",
				K: 75, P: 0.50, P0: 0.50, Qty: 1, Mult: 100, Style: "american", Expiration: "2024-12-20", Underlying: "XYZ"},
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 75, P: 4.0, P0: 4.0, Qty: 1, Mult: 100, Style: "american", Expiration: "2024-12-20", Underlying: "XYZ"},
			{Side: Short, Kind: StockKind, Shares: 100, ShortSaleProceeds: 7190, SalePrice: 71.90},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Maintenance)
	assertClose(t, "p61 reverse conversion ITM put maintenance", res.Requirement, 8560.00)
}

// p.61: Collar maintenance. Long 100 @ 31.75, Long Dec 30 put, Short Dec 35 call.
// Maintenance: min((10% × 30 + max(0, 30-31.75)) × 100, 25% × 35 × 100)
//
//	         = min(300 + 0, 875) = $475? Wait, p.61 says:
//	a) (10% × 30) + 1.75 × 100 = $475   ← That's 10%×30 + max(0, 30-31.75) = 3 + 0 = 3 × 100 = 300?
//
// Re-reading manual p.61:
//
//	"(10% × 30) + 1.75 × 100 = $475"
//	This is actually [(10% × 30) + 1.75] × 100 — but max(0, 30-31.75) = 0 since put is OTM.
//
// Wait: the manual writes "(10% × 30) + 1.75" = 3 + 1.75 = 4.75 × 100 = $475
// But the OTM amount for a put with K=30, U=31.75 is max(0, K-U) = max(0, -1.75) = 0.
// So the 1.75 in the manual is the AMOUNT BY WHICH UNDERLYING EXCEEDS PUT STRIKE — that's call-OTM-like.
// Looking again at the Collar maintenance rule on p.19:
//
//	"lower of: 1) 10% of the put exercise price plus any put out-of-the-money amount, or
//	          2) 25% of the call exercise price"
//
// "put out-of-the-money amount" for a long put: U > K means put is OTM, amount = U - K.
// So OTM-put-amount = max(0, U - K) = max(0, 31.75 - 30) = 1.75. ✓
//
// My current YAML uses `max(0.0, legs.lp.K - U)` which is ITM amount, not OTM!
// That's a BUG. Need to fix: should be max(0, U - K) for "put OTM amount".
//
// Expected: min((0.10 × 30 + max(0, 31.75 - 30)) × 100, 0.25 × 35 × 100)
//
//	= min((3 + 1.75) × 100, 8.75 × 100)
//	= min(475, 875) = $475
func TestCollarMaintenance_p61(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     31.75,
		Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "put",
				K: 30, P: 0.40, P0: 0.40, Qty: 1, Mult: 100, Style: "american", Expiration: "2024-12-20", Underlying: "XYZ"},
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 35, P: 0.20, P0: 0.20, Qty: 1, Mult: 100, Style: "american", Expiration: "2024-12-20", Underlying: "XYZ"},
			{Side: Long, Kind: StockKind, Shares: 100},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Maintenance)
	if res.RuleID != "collar" {
		t.Errorf("matched %s, want collar", res.RuleID)
	}
	assertClose(t, "p61 collar maintenance", res.Requirement, 475.00)
}

// p.58: Protective put maintenance. Long 100 XYZ @ 103.50, Long Nov 95 put.
// Maintenance: min((10% × 95 + max(0, 103.50 - 95)) × 100, 25% × 103.50 × 100)
//
//	= min((9.50 + 8.50) × 100, 2587.50)
//	= min(1800, 2587.50) = $1,800
//
// Same bug as collar: put-OTM amount is max(0, U - K) when underlying is ABOVE strike.
func TestProtectivePutMaintenance_p58(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     103.50,
		Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "put",
				K: 95, P: 1.0, P0: 1.0, Qty: 1, Mult: 100, Style: "american"},
			{Side: Long, Kind: StockKind, Shares: 100},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Maintenance)
	assertClose(t, "p58 protective put maintenance", res.Requirement, 1800.00)
}

// p.42: Vertical call spread. Long Nov 125 call @ 3.80, Short Nov 120 call @ 8.40, U=128.50
// MPL = 500 (max loss at U=120 is short ITM 0, at U=125 short -500 long 0; net -500)
// Margin = min(short_uncov_req, mpl) + long_paid - short_proceeds
//
//	= min(3410, 500) + 380 - 840 = 500 + 380 - 840 = $40
//
// Wait, manual says margin requirement is $880, SMA debit $40 = "880 - 840" — the $880 includes long paid in full.
// Let's compute: MPL = $500. long P0=3.80 → 380 in. short proceeds = 840.
// margin_initial = min(3410, 500) + 380 - 840 = 500 + 380 - 840 = 40
// But manual says $880. Reading again:
//
//	"Margin Requirement: $500 + $380 = $880"
//	"SMA Debit or Margin Call: $880 - $840 = $40"
//
// So manual's "margin requirement" is MPL + long-cost = $880, then short proceeds applied = $40 cash needed.
// Our formula gives the NET (cash needed). To match manual's "$880", we'd compute MPL + long-cost without subtracting short proceeds.
// Decision: our requirement = "minimum cash to put on" = MPL + long_paid - short_proceeds = $40.
// We accept that diverges from manual's display convention. The economic requirement is what matters.
// For the test, validate net cash requirement = $40.
func TestVerticalCallSpread_p42(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     128.50,
		Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call",
				K: 125, P: 3.80, P0: 3.80, Qty: 1, Mult: 100, Style: "american", Venue: "listed",
				Underlying: "XYZ", Expiration: "2024-11-15"},
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 120, P: 8.40, P0: 8.40, Qty: 1, Mult: 100, Style: "american", Venue: "listed",
				Underlying: "XYZ", Expiration: "2024-11-15"},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	if res.RuleID != "vertical_spread" {
		t.Errorf("matched %s, want vertical_spread", res.RuleID)
	}
	// Manual p.42: Margin Requirement = $880, short proceeds applied = $840, SMA debit = $40.
	assertClose(t, "p42 vertical call spread (gross)", res.Requirement, 880.00)
	assertClose(t, "p42 vertical call spread (proceeds)", res.AppliedProceeds, 840.00)
	assertClose(t, "p42 vertical call spread (cash call)", res.CashCall, 40.00)
}

// p.39: Vertical put spread. Long Nov 250 put @ 3, Short Nov 240 put @ .95, U=255
// MPL = max loss at any strike = 0 (long strike higher than short; long protects).
// At U=240: long 250 put intrinsic=10, short 240 put 0 → +1000-0 = +1000
// At U=250: long 0, short 0 → 0
// MPL = 0.
// Manual: "No loss... Margin = $300, SMA = 300 - 95 = 205"
// Our: min(short_uncov_req, 0) + 300 - 95 = 0 + 300 - 95 = $205
func TestVerticalPutSpread_p39(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     255.0,
		Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "put",
				K: 250, P: 3.0, P0: 3.0, Qty: 1, Mult: 100, Style: "american", Venue: "listed",
				Underlying: "XYZ", Expiration: "2024-11-15"},
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 240, P: 0.95, P0: 0.95, Qty: 1, Mult: 100, Style: "american", Venue: "listed",
				Underlying: "XYZ", Expiration: "2024-11-15"},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	// Manual p.39: Margin Requirement = $300, short proceeds = $95, SMA debit = $205.
	assertClose(t, "p39 vertical put spread (gross)", res.Requirement, 300.00)
	assertClose(t, "p39 vertical put spread (proceeds)", res.AppliedProceeds, 95.00)
	assertClose(t, "p39 vertical put spread (cash call)", res.CashCall, 205.00)
}

// p.52: Long butterfly puts. Long Nov 540 put @ 5.60, Short 2x Nov 550 puts @ 7.20, Long Nov 555 put @ 9.80.
// MPL = $500 (worst at U=540: -2000 short, +1500 long-555, 0 long-540 = -500).
// Margin requirement (manual): $2,040; SMA = $600 after applying $1,440 short proceeds.
// Net cash = MPL + long_premiums - short_premiums
//
//	= 500 + (560 + 980) - (2 × 720) = 500 + 1540 - 1440 = $600 ✓
func TestLongButterflyPuts_p52(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     550.0,
		Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "put",
				K: 540, P: 5.60, P0: 5.60, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ"},
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 550, P: 7.20, P0: 7.20, Qty: 2, Mult: 100, Style: "american", Underlying: "XYZ"},
			{Side: Long, Kind: OptionKind, OptionType: "put",
				K: 555, P: 9.80, P0: 9.80, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ"},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	if res.RuleID != "generic_limited_risk_combo" {
		t.Errorf("matched %s, want generic_limited_risk_combo", res.RuleID)
	}
	// Manual p.52: gross = $2,040, short proceeds = $1,440, cash needed = $600.
	assertClose(t, "p52 long butterfly puts (gross)", res.Requirement, 2040.00)
	assertClose(t, "p52 long butterfly puts (proceeds)", res.AppliedProceeds, 1440.00)
	assertClose(t, "p52 long butterfly puts (cash call)", res.CashCall, 600.00)
}

// p.56: Short iron butterfly. Long Oct 16 put @ .10, Short Oct 20 put @ .20,
// Short Oct 20 call @ 7, Long Oct 24 call @ 4. U=26.75
// MPL: at U=16 short 20-put -400; at U=24 short 20-call -400 (and long 24 call 0). So MPL = 400.
// Net cash = 400 + (10 + 400) - (20 + 700) = 400 + 410 - 720 = $90
func TestShortIronButterfly_p56(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     26.75,
		Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "put",
				K: 16, P: 0.10, P0: 0.10, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ"},
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 20, P: 0.20, P0: 0.20, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ"},
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 20, P: 7.0, P0: 7.0, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ"},
			{Side: Long, Kind: OptionKind, OptionType: "call",
				K: 24, P: 4.0, P0: 4.0, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ"},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	// gross = MPL + long premiums = 400 + (10+400) = 810
	// proceeds = short premiums = 20 + 700 = 720; cash call = 810 - 720 = 90
	assertClose(t, "p56 short iron butterfly (gross)", res.Requirement, 810.00)
	assertClose(t, "p56 short iron butterfly (proceeds)", res.AppliedProceeds, 720.00)
	assertClose(t, "p56 short iron butterfly (cash call)", res.CashCall, 90.00)
}

// -----------------------------------------------------------------------------
// p.14 — Short Call + Long Marginable Convertible (formula-only; no worked
// example in the manual, expected values derived by applying the stated rule.)
// -----------------------------------------------------------------------------
// Setup: Long 100 XYZ convertible @ $80, short 1 XYZ Mar 90 call @ $3.
// Margin initial:    50% * 80 * 100         = $4,000.00
// Margin maintenance: 25% * min(80, 90) * 100 = $2,000.00
// Cash initial:      80 * 100               = $8,000.00 (pay-in-full)
func TestShortCallLongConvertible_margin_p14(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     80.0,
		Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 90, P: 3.0, P0: 3.0, Qty: 1, Mult: 100},
			{Side: Long, Kind: ConvertibleKind,
				Price: 80.0, Shares: 100, KEquivalent: 90.0},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	if res.RuleID != "short_call_long_convertible" {
		t.Errorf("matched %s, want short_call_long_convertible", res.RuleID)
	}
	assertClose(t, "p14 SC+LConv margin initial", res.Requirement, 4000.00)

	res = mustEvaluate(t, rb, pos, MarginAccount, Maintenance)
	assertClose(t, "p14 SC+LConv margin maintenance", res.Requirement, 2000.00)

	res = mustEvaluate(t, rb, pos, CashAccount, Initial)
	assertClose(t, "p14 SC+LConv cash initial", res.Requirement, 8000.00)
}

// p.14 — Maintenance value cap: when convertible market value exceeds the call
// exercise price, the 25% is applied to the *strike*, not the market value.
// Setup: convertible @ $100, call strike $90 → 25% * 90 * 100 = $2,250.
func TestShortCallLongConvertible_maintenanceCap_p14(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     100.0,
		Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 90, P: 12.0, P0: 12.0, Qty: 1, Mult: 100},
			{Side: Long, Kind: ConvertibleKind,
				Price: 100.0, Shares: 100, KEquivalent: 90.0},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Maintenance)
	assertClose(t, "p14 SC+LConv maintenance cap binds", res.Requirement, 2250.00)
}

// -----------------------------------------------------------------------------
// p.14 — Short Call + Long Marginable Stock Warrant (formula-only).
// Setup: Long 100 XYZ warrants @ $4, exercise $50;
//
//	Short 1 XYZ Mar 45 call @ $2.
//
// Margin initial = maintenance:
//
//	100% * 4 * 100  + max(0, 50 - 45) * 100  = 400 + 500 = $900.00
//
// Cash account: not permitted.
// -----------------------------------------------------------------------------
func TestShortCallLongWarrant_margin_p14(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     46.0,
		Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 45, P: 2.0, P0: 2.0, Qty: 1, Mult: 100},
			{Side: Long, Kind: WarrantKind,
				Price: 4.0, Shares: 100, KEquivalent: 50.0},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	if res.RuleID != "short_call_long_warrant" {
		t.Errorf("matched %s, want short_call_long_warrant", res.RuleID)
	}
	assertClose(t, "p14 SC+LW margin initial", res.Requirement, 900.00)

	res = mustEvaluate(t, rb, pos, MarginAccount, Maintenance)
	assertClose(t, "p14 SC+LW margin maintenance", res.Requirement, 900.00)

	res = mustEvaluate(t, rb, pos, CashAccount, Initial)
	if res.Permitted {
		t.Errorf("cash account should be not-permitted, got Permitted=true")
	}
}

// Warrant maintenance value cap: warrant market value $60, call strike $45,
// warrant exercise $50.
//
//	min(60, 45)*100 + max(0, 50-45)*100 = 4,500 + 500 = $5,000.
func TestShortCallLongWarrant_marketCap_p14(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     58.0,
		Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 45, P: 15.0, P0: 15.0, Qty: 1, Mult: 100},
			{Side: Long, Kind: WarrantKind,
				Price: 60.0, Shares: 100, KEquivalent: 50.0},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Maintenance)
	assertClose(t, "p14 SC+LW maintenance cap binds", res.Requirement, 5000.00)
}

// -----------------------------------------------------------------------------
// p.14 — Short Index Call + Long ETF tracking that index (formula-only; no
// worked numeric example in the manual, expected values derived by applying
// the stated rule). K_equivalent is the strike-equivalent on the *ETF* leg
// — it is a property of the ETF position, not the index option — so both the
// formula and the rule-level validator key it off `le`.
// -----------------------------------------------------------------------------
// Setup: Long 100 XYZ_ETF @ $450, short 1 XYZ_INDEX Mar 4500 call @ $10.
// ETF tracks the index, KEquivalent on the ETF leg = $460.
// Margin initial:     50% * 450 * 100             = $22,500.00
// Margin maintenance: 25% * min(450, 460) * 100   = $11,250.00
// Proceeds (initial = maintenance, since P0 == P): 10 * 1 * 100 = $1,000.00
// CashCall initial = 22,500 - 1,000 = $21,500.00
// CashCall maintenance = 11,250 - 1,000 = $10,250.00
func TestShortIndexCallLongETF_margin_p14(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     450.0,
		Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 4500, P: 10.0, P0: 10.0, Qty: 1, Mult: 100,
				Underlying: "XYZ_INDEX"},
			{Side: Long, Kind: ETFKind,
				Price: 450.0, Shares: 100, KEquivalent: 460.0,
				TracksIndex: "XYZ_INDEX", Leveraged: false},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Initial)
	if res.RuleID != "short_index_call_long_etf" {
		t.Errorf("matched %s, want short_index_call_long_etf", res.RuleID)
	}
	assertClose(t, "p14 SIC+LETF margin initial (req)", res.Requirement, 22500.00)
	assertClose(t, "p14 SIC+LETF margin initial (proceeds)", res.AppliedProceeds, 1000.00)
	assertClose(t, "p14 SIC+LETF margin initial (cash call)", res.CashCall, 21500.00)

	res = mustEvaluate(t, rb, pos, MarginAccount, Maintenance)
	assertClose(t, "p14 SIC+LETF margin maintenance (req)", res.Requirement, 11250.00)
	assertClose(t, "p14 SIC+LETF margin maintenance (proceeds)", res.AppliedProceeds, 1000.00)
	assertClose(t, "p14 SIC+LETF margin maintenance (cash call)", res.CashCall, 10250.00)
}

// Maintenance value cap: when ETF market value exceeds the strike-equivalent,
// the 25% is applied to KEquivalent, not the market price.
// Setup: ETF @ $500, KEquivalent $460 → 0.25 * 460 * 100 = $11,500.
func TestShortIndexCallLongETF_maintenanceCap_p14(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U:     500.0,
		Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 4600, P: 15.0, P0: 15.0, Qty: 1, Mult: 100,
				Underlying: "XYZ_INDEX"},
			{Side: Long, Kind: ETFKind,
				Price: 500.0, Shares: 100, KEquivalent: 460.0,
				TracksIndex: "XYZ_INDEX", Leveraged: false},
		},
	}
	res := mustEvaluate(t, rb, pos, MarginAccount, Maintenance)
	assertClose(t, "p14 SIC+LETF maintenance cap binds (req)", res.Requirement, 11500.00)
	// Proceeds = sc.P * qty * mult = 15 * 1 * 100 = $1,500.00
	assertClose(t, "p14 SIC+LETF maintenance cap binds (proceeds)", res.AppliedProceeds, 1500.00)
	assertClose(t, "p14 SIC+LETF maintenance cap binds (cash call)", res.CashCall, 10000.00)
}

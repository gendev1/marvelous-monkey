package overlay

import (
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"margincalc/internal/account"
	"margincalc/internal/engine"
)

// --- helpers -----------------------------------------------------------------

// loadRules compiles an inline overlay rulebook from YAML for use in
// these tests. Writing to a temp file keeps the test surface aligned
// with the production loader path.
func loadRules(t *testing.T, yamlBody string) *Rulebook {
	t.Helper()
	path := writeTemp(t, "rules.yaml", yamlBody)
	rb, err := LoadRulebook(path)
	if err != nil {
		t.Fatalf("LoadRulebook: %v", err)
	}
	return rb
}

func stockPosition(id, symbol string, side engine.Side, shares, u float64) account.AccountPosition {
	return account.AccountPosition{
		ID: id,
		Position: engine.Position{
			U:     u,
			Class: "equity",
			Lev:   1,
			Legs: []engine.Leg{{
				Side:       side,
				Kind:       engine.StockKind,
				Shares:     shares,
				Underlying: symbol,
				Venue:      "listed",
				Mult:       1,
			}},
		},
	}
}

func baseAccount(positions ...account.AccountPosition) account.Account {
	return account.Account{
		ID:          "ACCT1",
		AccountType: engine.MarginAccount,
		Phase:       engine.Maintenance,
		AsOf:        time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		Currency:    "USD",
		SODEquity:   100000,
		CashBalance: 50000,
		Positions:   positions,
	}
}

func baseSnapshot(acct account.Account, evals []account.PositionEvaluation) account.AccountSnapshot {
	snap := account.AccountSnapshot{
		AccountID:        acct.ID,
		AccountType:      acct.AccountType,
		Phase:            acct.Phase,
		AsOf:             acct.AsOf,
		Currency:         acct.Currency,
		SODEquity:        acct.SODEquity,
		CashBalance:      acct.CashBalance,
		CurrentEquity:    100000,
		Evaluations:      evals,
		TotalRequirement: 0,
		TotalCashCall:    0,
	}
	for _, e := range evals {
		snap.TotalRequirement += e.Result.Requirement
		snap.TotalCashCall += e.Result.CashCall
	}
	return snap
}

// --- tests -------------------------------------------------------------------

func TestEvaluate_NoRules_PassthroughBaseline(t *testing.T) {
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{
		PositionID: "p1",
		Result:     engine.Result{Requirement: 3000, CashCall: 3000},
	}})
	snap.TotalRequirement = 3000
	snap.TotalCashCall = 3000

	e := &Engine{}
	out, err := e.Evaluate(acct, snap, ReferenceData{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if out.HouseRequirement != 3000 {
		t.Errorf("HouseRequirement = %v, want 3000", out.HouseRequirement)
	}
	if out.BaselineRequirement != 3000 {
		t.Errorf("BaselineRequirement = %v, want 3000", out.BaselineRequirement)
	}
	if len(out.Components) != 0 {
		t.Errorf("Components = %d, want 0", len(out.Components))
	}
	if len(out.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", out.Warnings)
	}
	if out.Currency != "USD" {
		t.Errorf("Currency = %q, want USD", out.Currency)
	}
}

func TestEvaluate_AddMode_AccumulatesDelta(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: pct_addon
    scope: position
    mode: add
    basis: market_value
    formula: "position.long_market_value * 0.10"
`)
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150) // LMV = 15000
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{
		PositionID: "p1",
		Result:     engine.Result{Requirement: 7500, CashCall: 7500},
	}})

	e := &Engine{Rulebook: rb}
	out, err := e.Evaluate(acct, snap, ReferenceData{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got, want := out.HouseRequirement, 7500.0+1500.0; got != want {
		t.Errorf("HouseRequirement = %v, want %v", got, want)
	}
	if len(out.Components) != 1 {
		t.Fatalf("Components = %d, want 1", len(out.Components))
	}
	c := out.Components[0]
	if !c.Applied || c.Delta != 1500 || c.Mode != "add" {
		t.Errorf("component = %+v", c)
	}
}

func TestEvaluate_MaxMode_PositiveDeltaOnly(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: max_below_baseline
    scope: position
    mode: max
    basis: market_value
    formula: "100.0"
`)
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{
		PositionID: "p1",
		Result:     engine.Result{Requirement: 1000, CashCall: 1000},
	}})

	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if out.HouseRequirement != 1000 {
		t.Errorf("HouseRequirement = %v, want 1000", out.HouseRequirement)
	}
	if len(out.Components) != 1 {
		t.Fatalf("Components = %d", len(out.Components))
	}
	c := out.Components[0]
	if c.Applied || c.Delta != 0 {
		t.Errorf("component should not apply: %+v", c)
	}
}

func TestEvaluate_MaxMode_AppliesShortfall(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: max_above_baseline
    scope: position
    mode: max
    basis: market_value
    formula: "2500.0"
`)
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{
		PositionID: "p1",
		Result:     engine.Result{Requirement: 1000, CashCall: 1000},
	}})

	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if got, want := out.HouseRequirement, 1000.0+1500.0; got != want {
		t.Errorf("HouseRequirement = %v, want %v", got, want)
	}
	c := out.Components[0]
	if !c.Applied || c.Delta != 1500 || c.BaselineAmount != 1000 || c.OverlayAmount != 2500 {
		t.Errorf("component = %+v", c)
	}
}

func TestEvaluate_FloorMode_BehavesLikeMaxButCarriesFloorAttribution(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: per_share_floor
    scope: position
    mode: floor
    basis: shares
    formula: "position.long_shares * 5.0"
`)
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{
		PositionID: "p1",
		Result:     engine.Result{Requirement: 100, CashCall: 100},
	}})

	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if got, want := out.HouseRequirement, 100.0+(500.0-100.0); got != want {
		t.Errorf("HouseRequirement = %v, want %v", got, want)
	}
	c := out.Components[0]
	if c.Mode != "floor" {
		t.Errorf("Mode = %q, want floor", c.Mode)
	}
	if c.Delta != 400 {
		t.Errorf("Delta = %v, want 400", c.Delta)
	}
}

func TestEvaluate_AppliesMatrix_AccountTypeFilter(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: margin_only
    scope: position
    mode: add
    formula: "10.0"
    applies:
      account_types: [cash]
`)
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p) // margin
	snap := baseSnapshot(acct, []account.PositionEvaluation{{
		PositionID: "p1",
		Result:     engine.Result{Requirement: 0},
	}})
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if len(out.Components) != 0 {
		t.Errorf("rule should not have fired for margin account; components=%v", out.Components)
	}
}

func TestEvaluate_AppliesMatrix_InstrumentKindFilter_ETFOnly(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: etf_only
    scope: position
    mode: add
    formula: "100.0"
    applies:
      instrument_kinds: [etf]
`)
	p := stockPosition("p1", "SPY", engine.Long, 100, 400)
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{
		PositionID: "p1",
		Result:     engine.Result{Requirement: 0},
	}})

	// Reference data missing → defaults to "stock"; rule does NOT fire.
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if len(out.Components) != 0 {
		t.Errorf("etf-only rule fired with no ref data: %v", out.Components)
	}
	if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "reference data missing") {
		t.Errorf("expected missing-ref-data warning, got %v", out.Warnings)
	}

	// Reference data present with InstrumentKind=etf → fires.
	ref := ReferenceData{Securities: map[SecKey]SecurityFacts{
		{Symbol: "SPY", Venue: "listed"}: {Symbol: "SPY", Venue: "listed", InstrumentKind: "etf"},
	}}
	out2, _ := e.Evaluate(acct, snap, ref)
	if len(out2.Components) != 1 || out2.Components[0].Delta != 100 {
		t.Errorf("etf rule should fire; components=%v", out2.Components)
	}
	if len(out2.Warnings) != 0 {
		t.Errorf("no warnings expected when ref data present, got %v", out2.Warnings)
	}
}

func TestEvaluate_MissingReferenceData_DefaultsToStockAndWarns(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: stock_addon
    scope: position
    mode: add
    formula: "10.0"
    applies:
      instrument_kinds: [stock]
`)
	p := stockPosition("p1", "XYZ", engine.Long, 10, 5)
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{PositionID: "p1"}})
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "XYZ@listed") {
		t.Errorf("expected XYZ@listed warning, got %v", out.Warnings)
	}
	if len(out.Components) != 1 {
		t.Errorf("stock rule should fire on defaulted instrument kind; components=%v", out.Components)
	}
}

func TestEvaluate_NaNFromFormula_WarnsAndSkips(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: divide_by_zero
    scope: position
    mode: add
    formula: "0.0 / 0.0"
`)
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{PositionID: "p1"}})
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if len(out.Components) != 0 {
		t.Errorf("Components = %v, want none on NaN formula", out.Components)
	}
	// Must have a warning naming the rule.
	found := false
	for _, w := range out.Warnings {
		if strings.Contains(w, "divide_by_zero") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning naming divide_by_zero, got %v", out.Warnings)
	}
}

func TestEvaluate_InputsNotMutated(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: r1
    scope: position
    mode: add
    formula: "position.long_market_value * 0.05"
`)
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{PositionID: "p1"}})

	acctCopy := deepCopyAccount(acct)
	snapCopy := deepCopySnapshot(snap)

	e := &Engine{Rulebook: rb}
	if _, err := e.Evaluate(acct, snap, ReferenceData{}); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !reflect.DeepEqual(acct, acctCopy) {
		t.Errorf("Account was mutated")
	}
	if !reflect.DeepEqual(snap, snapCopy) {
		t.Errorf("Snapshot was mutated")
	}
}

func TestEvaluate_BaselineFieldsPopulated(t *testing.T) {
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{
		PositionID: "p1",
		Result:     engine.Result{Requirement: 4242, CashCall: 4000},
	}})

	e := &Engine{}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if out.BaselineRequirement != snap.TotalRequirement {
		t.Errorf("BaselineRequirement = %v, want %v", out.BaselineRequirement, snap.TotalRequirement)
	}
	if out.BaselineCashCall != snap.TotalCashCall {
		t.Errorf("BaselineCashCall = %v, want %v", out.BaselineCashCall, snap.TotalCashCall)
	}
	if out.Currency != acct.Currency {
		t.Errorf("Currency = %q, want %q", out.Currency, acct.Currency)
	}
}

func TestEvaluate_NonFinite_Inf_WarnsAndSkips(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: divide_by_zero_pos
    scope: position
    mode: add
    formula: "1.0 / 0.0"
`)
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{PositionID: "p1"}})
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if len(out.Components) != 0 {
		t.Errorf("Inf formula should not produce a component, got %v", out.Components)
	}
	if math.IsInf(out.HouseRequirement, 0) {
		t.Errorf("HouseRequirement should not be Inf")
	}
}

func TestEvaluate_NonPositionScopeIgnored(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: account_floor
    scope: account
    mode: floor
    basis: account_equity
    formula: "2000.0"
`)
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{
		PositionID: "p1",
		Result:     engine.Result{Requirement: 100, CashCall: 100},
	}})
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if len(out.Components) != 0 {
		t.Errorf("non-position rule should be skipped this issue: %v", out.Components)
	}
	if out.HouseRequirement != 100 {
		t.Errorf("HouseRequirement = %v, want 100", out.HouseRequirement)
	}
}

func TestEvaluate_NonStockLikeLegs_Ignored(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: addon
    scope: position
    mode: add
    formula: "100.0"
`)
	// An option-only position: not stock-like, so position-scope rules
	// must not fire and no reference-data warning should be emitted.
	optPos := account.AccountPosition{
		ID: "p1",
		Position: engine.Position{
			U:     150,
			Class: "equity",
			Lev:   1,
			Legs: []engine.Leg{{
				Side:       engine.Long,
				Kind:       engine.OptionKind,
				OptionType: "call",
				K:          150,
				P:          5,
				Qty:        1,
				Mult:       100,
				Underlying: "AAPL",
				Venue:      "listed",
			}},
		},
	}
	acct := baseAccount(optPos)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{
		PositionID: "p1",
		Result:     engine.Result{Requirement: 500, CashCall: 500},
	}})
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if len(out.Components) != 0 {
		t.Errorf("non-stock-like position produced components: %v", out.Components)
	}
	if len(out.Warnings) != 0 {
		t.Errorf("non-stock-like position should not emit ref-data warnings, got %v", out.Warnings)
	}
	if out.HouseRequirement != snap.TotalRequirement {
		t.Errorf("HouseRequirement = %v, want %v (baseline passthrough)", out.HouseRequirement, snap.TotalRequirement)
	}
}

func TestEvaluate_EvaluationError_SkipsPosition(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: addon
    scope: position
    mode: add
    formula: "100.0"
`)
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{
		PositionID: "p1",
		Error:      errFakeEvalErr,
	}})
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if len(out.Components) != 0 {
		t.Errorf("error eval should skip overlay, got components=%v", out.Components)
	}
}

var errFakeEvalErr = &fakeErr{msg: "engine error"}

type fakeErr struct{ msg string }

func (f *fakeErr) Error() string { return f.msg }

// --- group-scope tests -------------------------------------------------------

// groupAddRulebook fires `group.gross_market_value > 20000` for any
// bucket and adds 10% of the group's gross MV. Used by the
// firing-group-only test.
const groupAddYAML = `schema_version: "1"
rules:
  - id: concentration_addon
    scope: group
    group_by: underlying
    mode: add
    basis: group_gross_mv
    when: "group.gross_market_value > 20000.0"
    formula: "group.gross_market_value * 0.10"
`

func TestEvaluate_GroupAdd_AccumulatesDeltaForFiringGroupOnly(t *testing.T) {
	rb := loadRules(t, groupAddYAML)
	// AAPL: 200 * 150 = 30,000 (fires)
	// MSFT: 100 * 100 = 10,000 (doesn't fire)
	// GOOG: 50 * 200  = 10,000 (doesn't fire)
	p1 := stockPosition("p1", "AAPL", engine.Long, 200, 150)
	p2 := stockPosition("p2", "MSFT", engine.Long, 100, 100)
	p3 := stockPosition("p3", "GOOG", engine.Long, 50, 200)
	acct := baseAccount(p1, p2, p3)
	snap := baseSnapshot(acct, []account.PositionEvaluation{
		{PositionID: "p1", Result: engine.Result{Requirement: 1000}},
		{PositionID: "p2", Result: engine.Result{Requirement: 500}},
		{PositionID: "p3", Result: engine.Result{Requirement: 500}},
	})

	e := &Engine{Rulebook: rb}
	out, err := e.Evaluate(acct, snap, ReferenceData{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(out.Components) != 1 {
		t.Fatalf("Components = %d, want 1; got %+v", len(out.Components), out.Components)
	}
	c := out.Components[0]
	if c.Scope != "group" || c.GroupKey != "AAPL" || c.Symbol != "AAPL" || c.PositionID != "" {
		t.Errorf("component identity = %+v", c)
	}
	if c.Delta != 3000 || c.OverlayAmount != 3000 || !c.Applied {
		t.Errorf("Delta/OverlayAmount/Applied = %v/%v/%v", c.Delta, c.OverlayAmount, c.Applied)
	}
	want := snap.TotalRequirement + 3000
	if out.HouseRequirement != want {
		t.Errorf("HouseRequirement = %v, want %v", out.HouseRequirement, want)
	}
}

func TestEvaluate_GroupMax_BaselineIsSumOfPerPositionRequirements_D2(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: group_floor_5k
    scope: group
    group_by: underlying
    mode: max
    basis: group_gross_mv
    formula: "5000.0"
`)
	// Three AAPL positions; Layer 1 baselines sum to 4000.
	p1 := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	p2 := stockPosition("p2", "AAPL", engine.Long, 100, 150)
	p3 := stockPosition("p3", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p1, p2, p3)
	snap := baseSnapshot(acct, []account.PositionEvaluation{
		{PositionID: "p1", Result: engine.Result{Requirement: 1500}},
		{PositionID: "p2", Result: engine.Result{Requirement: 1500}},
		{PositionID: "p3", Result: engine.Result{Requirement: 1000}},
	})

	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if len(out.Components) != 1 {
		t.Fatalf("Components = %d, want 1; got %+v", len(out.Components), out.Components)
	}
	c := out.Components[0]
	if c.BaselineAmount != 4000 || c.OverlayAmount != 5000 || c.Delta != 1000 || !c.Applied {
		t.Errorf("D2 baseline composition wrong: %+v", c)
	}
	// Per-position baselines must be in Evidence for audit.
	for _, id := range []string{"p1", "p2", "p3"} {
		if _, ok := c.Evidence["baseline."+id]; !ok {
			t.Errorf("Evidence missing baseline.%s; have %v", id, c.Evidence)
		}
	}
	if out.HouseRequirement != snap.TotalRequirement+1000 {
		t.Errorf("HouseRequirement = %v, want %v", out.HouseRequirement, snap.TotalRequirement+1000)
	}
}

func TestEvaluate_GroupMax_OverlayBelowBaseline_NoDelta(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: group_floor_3k
    scope: group
    group_by: underlying
    mode: max
    basis: group_gross_mv
    formula: "3000.0"
`)
	p1 := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	p2 := stockPosition("p2", "AAPL", engine.Long, 100, 150)
	p3 := stockPosition("p3", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p1, p2, p3)
	snap := baseSnapshot(acct, []account.PositionEvaluation{
		{PositionID: "p1", Result: engine.Result{Requirement: 1500}},
		{PositionID: "p2", Result: engine.Result{Requirement: 1500}},
		{PositionID: "p3", Result: engine.Result{Requirement: 1000}},
	})

	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if len(out.Components) != 1 {
		t.Fatalf("Components = %d", len(out.Components))
	}
	c := out.Components[0]
	if c.Applied || c.Delta != 0 {
		t.Errorf("expected non-applied component, got %+v", c)
	}
	if out.HouseRequirement != snap.TotalRequirement {
		t.Errorf("HouseRequirement should equal baseline, got %v want %v",
			out.HouseRequirement, snap.TotalRequirement)
	}
}

func TestEvaluate_GroupBy_Underlying_BucketsCorrectly(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: per_underlying_addon
    scope: group
    group_by: underlying
    mode: add
    formula: "group.position_count * 100.0"
`)
	// Two AAPL, one MSFT → two groups, count 2 and 1.
	p1 := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	p2 := stockPosition("p2", "AAPL", engine.Long, 50, 150)
	p3 := stockPosition("p3", "MSFT", engine.Long, 100, 100)
	acct := baseAccount(p1, p2, p3)
	snap := baseSnapshot(acct, []account.PositionEvaluation{
		{PositionID: "p1"},
		{PositionID: "p2"},
		{PositionID: "p3"},
	})

	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if len(out.Components) != 2 {
		t.Fatalf("Components = %d, want 2; got %+v", len(out.Components), out.Components)
	}
	byKey := map[string]HouseComponent{}
	for _, c := range out.Components {
		byKey[c.GroupKey] = c
	}
	if byKey["AAPL"].Delta != 200 {
		t.Errorf("AAPL delta = %v, want 200", byKey["AAPL"].Delta)
	}
	if byKey["MSFT"].Delta != 100 {
		t.Errorf("MSFT delta = %v, want 100", byKey["MSFT"].Delta)
	}
}

func TestEvaluate_GroupBy_Symbol_BucketsCorrectly(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: per_symbol_addon
    scope: group
    group_by: symbol
    mode: add
    formula: "group.gross_market_value * 0.01"
`)
	p1 := stockPosition("p1", "AAPL", engine.Long, 100, 150) // 15000
	p2 := stockPosition("p2", "AAPL", engine.Long, 100, 150) // 15000
	p3 := stockPosition("p3", "MSFT", engine.Long, 100, 100) // 10000
	acct := baseAccount(p1, p2, p3)
	snap := baseSnapshot(acct, []account.PositionEvaluation{
		{PositionID: "p1"}, {PositionID: "p2"}, {PositionID: "p3"},
	})
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if len(out.Components) != 2 {
		t.Fatalf("Components = %d, want 2", len(out.Components))
	}
	byKey := map[string]HouseComponent{}
	for _, c := range out.Components {
		byKey[c.GroupKey] = c
	}
	if byKey["AAPL"].Delta != 300 {
		t.Errorf("AAPL = %v, want 300", byKey["AAPL"].Delta)
	}
	if byKey["MSFT"].Delta != 100 {
		t.Errorf("MSFT = %v, want 100", byKey["MSFT"].Delta)
	}
}

func TestEvaluate_GroupOrdering_DeterministicAcrossRuns(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: per_underlying_addon
    scope: group
    group_by: underlying
    mode: add
    formula: "1.0"
`)
	// Several positions across many symbols to exercise map iteration.
	positions := []account.AccountPosition{
		stockPosition("p1", "ZZZZ", engine.Long, 1, 10),
		stockPosition("p2", "AAPL", engine.Long, 1, 10),
		stockPosition("p3", "MSFT", engine.Long, 1, 10),
		stockPosition("p4", "GOOG", engine.Long, 1, 10),
		stockPosition("p5", "TSLA", engine.Long, 1, 10),
		stockPosition("p6", "NVDA", engine.Long, 1, 10),
		stockPosition("p7", "BBBB", engine.Long, 1, 10),
	}
	acct := baseAccount(positions...)
	evals := make([]account.PositionEvaluation, len(positions))
	for i, p := range positions {
		evals[i] = account.PositionEvaluation{PositionID: p.ID}
	}
	snap := baseSnapshot(acct, evals)
	e := &Engine{Rulebook: rb}

	out1, _ := e.Evaluate(acct, snap, ReferenceData{})
	out2, _ := e.Evaluate(acct, snap, ReferenceData{})
	if len(out1.Components) != len(positions) {
		t.Fatalf("got %d components, want %d", len(out1.Components), len(positions))
	}
	keys1 := make([]string, len(out1.Components))
	keys2 := make([]string, len(out2.Components))
	for i := range out1.Components {
		keys1[i] = out1.Components[i].GroupKey
		keys2[i] = out2.Components[i].GroupKey
	}
	if !reflect.DeepEqual(keys1, keys2) {
		t.Errorf("ordering not deterministic:\n run1=%v\n run2=%v", keys1, keys2)
	}
	if !sort.StringsAreSorted(keys1) {
		t.Errorf("group keys not byte-sorted: %v", keys1)
	}
}

func TestEvaluate_GroupSkipsWhenAllPositionsFilteredByAppliesMatrix(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: etf_only_group
    scope: group
    group_by: underlying
    mode: add
    formula: "1.0"
    applies:
      instrument_kinds: [etf]
`)
	// Stock-only position; ETF filter excludes it from the bucket.
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{PositionID: "p1"}})
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if len(out.Components) != 0 {
		t.Errorf("expected no components when applies excludes all positions, got %v", out.Components)
	}
}

func TestEvaluate_GroupMax_FloorMode_CarriesFloorAttribution(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: group_floor
    scope: group
    group_by: underlying
    mode: floor
    basis: group_gross_mv
    formula: "5000.0"
`)
	p1 := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	p2 := stockPosition("p2", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p1, p2)
	snap := baseSnapshot(acct, []account.PositionEvaluation{
		{PositionID: "p1", Result: engine.Result{Requirement: 1000}},
		{PositionID: "p2", Result: engine.Result{Requirement: 1000}},
	})
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if len(out.Components) != 1 {
		t.Fatalf("Components = %d", len(out.Components))
	}
	c := out.Components[0]
	if c.Mode != "floor" {
		t.Errorf("Mode = %q, want floor", c.Mode)
	}
	if c.BaselineAmount != 2000 || c.Delta != 3000 || !c.Applied {
		t.Errorf("component = %+v", c)
	}
}

// --- deep copy helpers -------------------------------------------------------

func deepCopyAccount(a account.Account) account.Account {
	out := a
	out.Positions = make([]account.AccountPosition, len(a.Positions))
	for i, p := range a.Positions {
		cp := p
		cp.Position.Legs = append([]engine.Leg(nil), p.Position.Legs...)
		out.Positions[i] = cp
	}
	return out
}

func deepCopySnapshot(s account.AccountSnapshot) account.AccountSnapshot {
	out := s
	out.Evaluations = append([]account.PositionEvaluation(nil), s.Evaluations...)
	out.Violations = append([]account.PositionEvaluation(nil), s.Violations...)
	out.Errors = append([]account.PositionEvaluation(nil), s.Errors...)
	out.Warnings = append([]string(nil), s.Warnings...)
	if s.DepositRequirements != nil {
		out.DepositRequirements = make(map[string]float64, len(s.DepositRequirements))
		for k, v := range s.DepositRequirements {
			out.DepositRequirements[k] = v
		}
	}
	return out
}

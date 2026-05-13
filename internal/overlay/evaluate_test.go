package overlay

import (
	"math"
	"reflect"
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

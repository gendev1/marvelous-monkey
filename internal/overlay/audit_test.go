package overlay

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"margincalc/internal/account"
	"margincalc/internal/engine"
)

func TestEvaluate_AuditTrail_OverlayRulebookHashPopulated(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: addon
    scope: position
    mode: add
    formula: "1.0"
`)
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{PositionID: "p1"}})

	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if out.Audit.OverlayRulebookHash == "" {
		t.Errorf("OverlayRulebookHash empty")
	}
	if out.Audit.OverlayRulebookHash != rb.OverlayRulebookHash {
		t.Errorf("OverlayRulebookHash = %q, want %q", out.Audit.OverlayRulebookHash, rb.OverlayRulebookHash)
	}
	// Baseline hash is intentionally deferred for this PR; assert
	// empty so the deferral is enforced and not silently regressed.
	if out.Audit.BaselineRulebookHash != "" {
		t.Errorf("BaselineRulebookHash = %q, want \"\" (deferred per #46 follow-up)",
			out.Audit.BaselineRulebookHash)
	}
}

func TestEvaluate_AuditTrail_EntryPerRulePerTarget(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: r1
    scope: position
    mode: add
    formula: "1.0"
  - id: r2
    scope: position
    mode: add
    formula: "2.0"
`)
	p1 := stockPosition("p1", "AAPL", engine.Long, 1, 10)
	p2 := stockPosition("p2", "MSFT", engine.Long, 1, 10)
	p3 := stockPosition("p3", "GOOG", engine.Long, 1, 10)
	acct := baseAccount(p1, p2, p3)
	snap := baseSnapshot(acct, []account.PositionEvaluation{
		{PositionID: "p1"}, {PositionID: "p2"}, {PositionID: "p3"},
	})
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if got, want := len(out.Audit.Entries), 6; got != want {
		t.Fatalf("Audit.Entries = %d, want %d", got, want)
	}
	// Outer rule, inner target order.
	wantOrder := []struct{ rule, pos string }{
		{"r1", "p1"}, {"r1", "p2"}, {"r1", "p3"},
		{"r2", "p1"}, {"r2", "p2"}, {"r2", "p3"},
	}
	for i, w := range wantOrder {
		ae := out.Audit.Entries[i]
		if ae.RuleID != w.rule || ae.PositionID != w.pos {
			t.Errorf("entry[%d] = (%s,%s), want (%s,%s)", i, ae.RuleID, ae.PositionID, w.rule, w.pos)
		}
	}
}

func TestEvaluate_AuditTrail_MatchedAndAppliedFlags(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: when_false
    scope: position
    mode: add
    when: "false"
    formula: "1.0"
  - id: max_no_shortfall
    scope: position
    mode: max
    formula: "100.0"
  - id: block_match
    scope: position
    mode: block
    when: "true"
    formula: "42.0"
`)
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{
		PositionID: "p1",
		Result:     engine.Result{Requirement: 1000},
	}})
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})

	byID := map[string]AuditEntry{}
	for _, ae := range out.Audit.Entries {
		byID[ae.RuleID] = ae
	}
	if ae := byID["when_false"]; ae.Matched || ae.Applied {
		t.Errorf("when_false Matched/Applied = %v/%v, want false/false", ae.Matched, ae.Applied)
	}
	if ae := byID["max_no_shortfall"]; !ae.Matched || ae.Applied {
		t.Errorf("max_no_shortfall Matched/Applied = %v/%v, want true/false", ae.Matched, ae.Applied)
	}
	if ae := byID["max_no_shortfall"]; ae.Amount != 100 || ae.Delta != 0 {
		t.Errorf("max_no_shortfall Amount/Delta = %v/%v, want 100/0", ae.Amount, ae.Delta)
	}
	if ae := byID["block_match"]; !ae.Matched || !ae.Applied {
		t.Errorf("block_match Matched/Applied = %v/%v, want true/true", ae.Matched, ae.Applied)
	}
}

func TestEvaluate_AuditTrail_InputsCaptureFormulaActivation(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
constants:
  pct: 0.10
rules:
  - id: covered_call_like
    scope: position
    mode: add
    formula: "position.long_market_value * constants.pct"
`)
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150) // LMV=15000
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{
		PositionID: "p1",
		Result:     engine.Result{Requirement: 0},
	}})
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})

	if len(out.Audit.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(out.Audit.Entries))
	}
	in := out.Audit.Entries[0].Inputs
	if in == nil {
		t.Fatal("Inputs is nil")
	}
	if got := in["position.long_market_value"]; got != 15000 {
		t.Errorf("Inputs[position.long_market_value] = %v, want 15000", got)
	}
	if got := in["constants.pct"]; got != 0.10 {
		t.Errorf("Inputs[constants.pct] = %v, want 0.10", got)
	}
	if _, ok := in["account.current_equity"]; !ok {
		t.Errorf("Inputs missing account.current_equity")
	}
}

func TestEvaluate_AuditTrail_BlockEntryAppliedTrue_D1(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: block_concentration
    scope: position
    mode: block
    when: "position.long_market_value > 0.0"
    formula: "position.long_market_value"
`)
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150) // LMV=15000
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{
		PositionID: "p1",
		Result:     engine.Result{Requirement: 1000},
	}})
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})

	if len(out.Audit.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(out.Audit.Entries))
	}
	ae := out.Audit.Entries[0]
	if !ae.Matched || !ae.Applied {
		t.Errorf("block entry Matched/Applied = %v/%v, want true/true", ae.Matched, ae.Applied)
	}
	if ae.Delta != 0 {
		t.Errorf("block entry Delta = %v, want 0", ae.Delta)
	}
	if ae.Amount != 15000 {
		t.Errorf("block entry Amount = %v, want 15000", ae.Amount)
	}
	// Block must NOT affect HouseRequirement.
	if out.HouseRequirement != 1000 {
		t.Errorf("HouseRequirement = %v, want 1000 (baseline; block does not add)", out.HouseRequirement)
	}
}

func TestEvaluate_AuditTrail_GroupEntriesInSortedKeyOrder(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: per_underlying
    scope: group
    group_by: underlying
    mode: add
    formula: "1.0"
`)
	positions := []account.AccountPosition{
		stockPosition("p1", "ZZZZ", engine.Long, 1, 10),
		stockPosition("p2", "AAPL", engine.Long, 1, 10),
		stockPosition("p3", "MSFT", engine.Long, 1, 10),
	}
	acct := baseAccount(positions...)
	evals := make([]account.PositionEvaluation, len(positions))
	for i, p := range positions {
		evals[i] = account.PositionEvaluation{PositionID: p.ID}
	}
	snap := baseSnapshot(acct, evals)
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})
	if len(out.Audit.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(out.Audit.Entries))
	}
	wantKeys := []string{"AAPL", "MSFT", "ZZZZ"}
	for i, w := range wantKeys {
		if out.Audit.Entries[i].GroupKey != w {
			t.Errorf("entry[%d].GroupKey = %q, want %q", i, out.Audit.Entries[i].GroupKey, w)
		}
	}
}

func TestEvaluate_Determinism_ByteIdenticalAcrossTwoRuns(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
constants:
  pct: 0.10
rules:
  - id: pos_addon
    scope: position
    mode: add
    formula: "position.long_market_value * constants.pct"
  - id: per_underlying
    scope: group
    group_by: underlying
    mode: add
    formula: "group.gross_market_value * 0.01"
`)
	positions := []account.AccountPosition{
		stockPosition("p1", "ZZZZ", engine.Long, 10, 10),
		stockPosition("p2", "AAPL", engine.Long, 10, 10),
		stockPosition("p3", "MSFT", engine.Long, 10, 10),
		stockPosition("p4", "AAPL", engine.Long, 5, 10),
	}
	acct := baseAccount(positions...)
	evals := make([]account.PositionEvaluation, len(positions))
	for i, p := range positions {
		evals[i] = account.PositionEvaluation{
			PositionID: p.ID,
			Result:     engine.Result{Requirement: 100, CashCall: 100},
		}
	}
	snap := baseSnapshot(acct, evals)
	e := &Engine{Rulebook: rb}

	out1, err := e.Evaluate(acct, snap, ReferenceData{})
	if err != nil {
		t.Fatalf("Evaluate #1: %v", err)
	}
	out2, err := e.Evaluate(acct, snap, ReferenceData{})
	if err != nil {
		t.Fatalf("Evaluate #2: %v", err)
	}
	if !reflect.DeepEqual(out1, out2) {
		t.Fatalf("HouseRequirement not byte-identical across two runs")
	}
	b1, err := json.Marshal(out1)
	if err != nil {
		t.Fatalf("json.Marshal out1: %v", err)
	}
	b2, err := json.Marshal(out2)
	if err != nil {
		t.Fatalf("json.Marshal out2: %v", err)
	}
	if !bytes.Equal(b1, b2) {
		t.Errorf("JSON bytes not byte-identical:\n%s\n%s", b1, b2)
	}
}

func TestEvaluate_JSONRoundTrip_D6(t *testing.T) {
	rb := loadRules(t, `schema_version: "1"
rules:
  - id: addon
    scope: position
    mode: add
    formula: "position.long_market_value * 0.10"
`)
	p := stockPosition("p1", "AAPL", engine.Long, 100, 150)
	acct := baseAccount(p)
	snap := baseSnapshot(acct, []account.PositionEvaluation{{
		PositionID: "p1",
		Result:     engine.Result{Requirement: 1000, CashCall: 1000},
	}})
	e := &Engine{Rulebook: rb}
	out, _ := e.Evaluate(acct, snap, ReferenceData{})

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got HouseRequirement
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(out, got) {
		t.Errorf("HouseRequirement did not round-trip through JSON\n want: %#v\n got: %#v", out, got)
	}
}

package overlay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestLoad_ValidExampleYAML(t *testing.T) {
	rb, err := LoadRulebook("testdata/minimal.yaml")
	if err != nil {
		t.Fatalf("LoadRulebook: %v", err)
	}
	if got := rb.ruleCount(); got != 3 {
		t.Fatalf("rule count = %d, want 3", got)
	}
	if rb.OverlayRulebookHash == "" {
		t.Errorf("OverlayRulebookHash is empty")
	}
	// Sorted by priority asc: 10, 20, 30.
	want := []string{"small_account_floor", "concentrated_position_addon", "leveraged_etf_group_cap"}
	for i, r := range rb.rules {
		if r.ID != want[i] {
			t.Errorf("rules[%d].ID = %q, want %q", i, r.ID, want[i])
		}
		if r.whenProg == nil {
			t.Errorf("rules[%d].whenProg is nil (lazy compile?)", i)
		}
		if r.formulaProg == nil {
			t.Errorf("rules[%d].formulaProg is nil", i)
		}
	}
}

func TestLoad_DuplicateRuleIDsAcrossFiles_Rejected(t *testing.T) {
	a := writeTemp(t, "a.yaml", `schema_version: "1"
rules:
  - {id: dupe, scope: account, mode: add, formula: "1.0"}
`)
	b := writeTemp(t, "b.yaml", `schema_version: "1"
rules:
  - {id: dupe, scope: account, mode: add, formula: "2.0"}
`)
	_, err := LoadRulebook(a, b)
	assertLoaderError(t, err, "duplicate rule id")
}

func TestLoad_UnknownYAMLField_Rejected(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
rules:
  - id: r1
    scope: account
    mode: add
    formula: "1.0"
    priorty: 10
`)
	_, err := LoadRulebook(p)
	assertLoaderError(t, err, "priorty")
}

func TestLoad_UnknownScope_Rejected(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
rules:
  - {id: r1, scope: planet, mode: add, formula: "1.0"}
`)
	_, err := LoadRulebook(p)
	assertLoaderError(t, err, "scope")
}

func TestLoad_UnknownMode_Rejected(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
rules:
  - {id: r1, scope: account, mode: bogus, formula: "1.0"}
`)
	_, err := LoadRulebook(p)
	assertLoaderError(t, err, "mode")
}

func TestLoad_UnknownBasis_Rejected(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
rules:
  - {id: r1, scope: account, mode: add, basis: gut_feeling, formula: "1.0"}
`)
	_, err := LoadRulebook(p)
	assertLoaderError(t, err, "basis")
}

func TestLoad_UnknownAccountType_Rejected(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
rules:
  - id: r1
    scope: account
    mode: add
    formula: "1.0"
    applies:
      account_types: [tax_deferred]
`)
	_, err := LoadRulebook(p)
	assertLoaderError(t, err, "account_types")
}

func TestLoad_UnknownPhase_Rejected(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
rules:
  - id: r1
    scope: account
    mode: add
    formula: "1.0"
    applies:
      phases: [overnight]
`)
	_, err := LoadRulebook(p)
	assertLoaderError(t, err, "phases")
}

func TestLoad_UnknownInstrumentKind_Rejected(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
rules:
  - id: r1
    scope: account
    mode: add
    formula: "1.0"
    applies:
      instrument_kinds: [crypto]
`)
	_, err := LoadRulebook(p)
	assertLoaderError(t, err, "instrument_kinds")
}

func TestLoad_UnknownSide_Rejected(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
rules:
  - id: r1
    scope: account
    mode: add
    formula: "1.0"
    applies:
      sides: [neutral]
`)
	_, err := LoadRulebook(p)
	assertLoaderError(t, err, "sides")
}

func TestLoad_GroupScopeRequiresGroupBy_Rejected(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
rules:
  - {id: r1, scope: group, mode: add, formula: "1.0"}
`)
	_, err := LoadRulebook(p)
	assertLoaderError(t, err, "group_by")
}

func TestLoad_WhenReturnsNonBool_Rejected(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
rules:
  - {id: r1, scope: account, mode: add, formula: "1.0", when: "5"}
`)
	_, err := LoadRulebook(p)
	assertLoaderError(t, err, "when must return bool")
}

func TestLoad_FormulaReturnsBool_Rejected(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
rules:
  - {id: r1, scope: account, mode: add, formula: "true"}
`)
	_, err := LoadRulebook(p)
	assertLoaderError(t, err, "formula must return a number")
}

func TestLoad_FormulaReturnsString_Rejected(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
rules:
  - {id: r1, scope: account, mode: add, formula: "\"hi\""}
`)
	_, err := LoadRulebook(p)
	assertLoaderError(t, err, "formula must return a number")
}

func TestLoad_IntegerConstantsNormalizedToFloat64(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
constants:
  threshold: 2000
rules:
  - {id: r1, scope: account, mode: add, formula: "constants.threshold"}
`)
	rb, err := LoadRulebook(p)
	if err != nil {
		t.Fatalf("LoadRulebook: %v", err)
	}
	v, ok := rb.constants["threshold"]
	if !ok {
		t.Fatalf("threshold missing from constants")
	}
	if _, isFloat := v.(float64); !isFloat {
		t.Fatalf("threshold = %v (%T), want float64", v, v)
	}
}

func TestLoad_DeterministicOrdering_EqualPriorities(t *testing.T) {
	// Equal priorities — tie-break must be (fileIndex, declIndex, id).
	a := writeTemp(t, "a.yaml", `schema_version: "1"
rules:
  - {id: zeta, priority: 5, scope: account, mode: add, formula: "1.0"}
  - {id: alpha, priority: 5, scope: account, mode: add, formula: "1.0"}
`)
	b := writeTemp(t, "b.yaml", `schema_version: "1"
rules:
  - {id: beta, priority: 5, scope: account, mode: add, formula: "1.0"}
`)
	rb, err := LoadRulebook(a, b)
	if err != nil {
		t.Fatalf("LoadRulebook: %v", err)
	}
	want := []string{"zeta", "alpha", "beta"}
	for i, r := range rb.rules {
		if r.ID != want[i] {
			t.Errorf("rules[%d].ID = %q, want %q", i, r.ID, want[i])
		}
	}
}

func TestLoad_OverlayRulebookHashChangesWhenBytesChange(t *testing.T) {
	a := writeTemp(t, "a.yaml", `schema_version: "1"
rules:
  - {id: r1, scope: account, mode: add, formula: "1.0"}
`)
	rbA, err := LoadRulebook(a)
	if err != nil {
		t.Fatalf("load A: %v", err)
	}
	b := writeTemp(t, "b.yaml", `schema_version: "1"
rules:
  - {id: r1, scope: account, mode: add, formula: "2.0"}
`)
	rbB, err := LoadRulebook(b)
	if err != nil {
		t.Fatalf("load B: %v", err)
	}
	if rbA.OverlayRulebookHash == rbB.OverlayRulebookHash {
		t.Errorf("hash unchanged: %q == %q", rbA.OverlayRulebookHash, rbB.OverlayRulebookHash)
	}
}

func TestLoad_OnMissingReferenceDefaultsToWarn(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
rules:
  - {id: r1, scope: account, mode: add, formula: "1.0"}
`)
	rb, err := LoadRulebook(p)
	if err != nil {
		t.Fatalf("LoadRulebook: %v", err)
	}
	if got := rb.rules[0].OnMissingReference; got != "warn" {
		t.Errorf("OnMissingReference = %q, want %q", got, "warn")
	}
}

func TestLoad_WhenOmittedDefaultsToTrue(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
rules:
  - {id: r1, scope: account, mode: add, formula: "1.0"}
`)
	rb, err := LoadRulebook(p)
	if err != nil {
		t.Fatalf("LoadRulebook: %v", err)
	}
	if rb.rules[0].whenProg == nil {
		t.Errorf("whenProg is nil for rule with omitted when")
	}
}

func TestLoad_FormulaOmittedOnNonBlockMode_Rejected(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
rules:
  - {id: r1, scope: account, mode: add}
`)
	_, err := LoadRulebook(p)
	assertLoaderError(t, err, "requires a formula")
}

func TestLoad_BlockModeMayOmitFormula(t *testing.T) {
	p := writeTemp(t, "x.yaml", `schema_version: "1"
rules:
  - {id: r1, scope: account, mode: block, when: "account.current_equity < 0"}
`)
	rb, err := LoadRulebook(p)
	if err != nil {
		t.Fatalf("LoadRulebook: %v", err)
	}
	if rb.rules[0].formulaProg != nil {
		t.Errorf("formulaProg unexpectedly populated for block-mode rule without formula")
	}
}

func TestLoad_ConstantCollisionAcrossFiles_Rejected(t *testing.T) {
	a := writeTemp(t, "a.yaml", `schema_version: "1"
constants:
  k: 1
rules: []
`)
	b := writeTemp(t, "b.yaml", `schema_version: "1"
constants:
  k: 2
rules: []
`)
	_, err := LoadRulebook(a, b)
	assertLoaderError(t, err, "constant")
}

func TestLoad_EmptyYAMLFileAccepted(t *testing.T) {
	empty := writeTemp(t, "empty.yaml", "")
	full := writeTemp(t, "full.yaml", `schema_version: "1"
rules:
  - {id: r1, scope: account, mode: add, formula: "1.0"}
`)
	rb, err := LoadRulebook(empty, full)
	if err != nil {
		t.Fatalf("LoadRulebook: %v", err)
	}
	if rb.ruleCount() != 1 {
		t.Errorf("rule count = %d, want 1", rb.ruleCount())
	}
	if rb.OverlayRulebookHash == "" {
		t.Errorf("hash is empty even though an empty file was loaded")
	}
}

func assertLoaderError(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", substr)
	}
	if !strings.HasPrefix(err.Error(), "invalid overlay rulebook:") {
		t.Errorf("error %q does not start with 'invalid overlay rulebook:'", err.Error())
	}
	if !strings.Contains(err.Error(), substr) {
		t.Errorf("error %q does not contain %q", err.Error(), substr)
	}
}

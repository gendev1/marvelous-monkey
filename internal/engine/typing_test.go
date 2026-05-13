package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestLegObjectTypeName pins the cel-go-derived ObjectType string for
// engine.Leg. cel-go composes it as
// `simplePkgAlias(reflect.Type.PkgPath()) + "." + Name()` (see
// ext/native.go: convertToCelType, simplePkgAlias). If this package is
// renamed or moved, the constant in env.go and the variable declaration
// for `legs` must move in lockstep — this test is the load-bearing canary.
func TestLegObjectTypeName(t *testing.T) {
	got := reflect.TypeOf(Leg{}).String()
	if got != legObjectTypeName {
		t.Fatalf("reflect.TypeOf(Leg{}).String() = %q, legObjectTypeName = %q — keep them in sync (see env.go)", got, legObjectTypeName)
	}
}

// TestLoadRulebook_unknownLegField asserts that a typoed leg-field access
// inside a formula is rejected at load time, not silently zero-evaluated at
// runtime. `kk` is not a tag on engine.Leg; with legs declared as
// map<string, engine.Leg> the CEL type-checker rejects it.
func TestLoadRulebook_unknownLegField(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: r_typo_formula
    match:
      legs:
        - { name: opt, side: long, kind: option }
    formulas:
      margin:
        initial: "0.25 * legs.opt.kk"
        maintenance: "0.0"
`)
	assertRulebookError(t, err, "kk")
	assertRulebookError(t, err, "r_typo_formula")
}

// TestLoadRulebook_unknownLegFieldInConstraint asserts that a typoed leg-field
// access inside a match.constraint is rejected at load time (per
// rulebook.go's constraint-compile wrapper).
func TestLoadRulebook_unknownLegFieldInConstraint(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: r_typo_constraint
    match:
      legs:
        - { name: opt, side: long, kind: option }
      constraints:
        - "legs.opt.styel == 'american'"
    formulas:
      margin:
        initial: "0.0"
        maintenance: "0.0"
`)
	assertRulebookError(t, err, "styel")
	assertRulebookError(t, err, "r_typo_constraint")
}

// TestLeg_zeroValuedNumericFieldAccessible verifies that ext.ParseStructTag
// is rename-only and does not honor the `omitempty` directive on numeric
// fields. A leg with K == 0 must still expose `K` reading as 0.0 — otherwise
// every long-stock or zero-strike fixture would fail to type-check. This
// uses an inline rulebook so the assertion is direct: a formula that reads
// `legs.s.K` on a long-stock leg must compile *and* evaluate to 0.
func TestLeg_zeroValuedNumericFieldAccessible(t *testing.T) {
	path := writeTempRulebook(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: r_zero_k
    match:
      legs:
        - { name: s, side: long, kind: stock }
    formulas:
      margin:
        initial: "legs.s.K"
        maintenance: "legs.s.K"
`)
	rb, err := LoadRulebook(path)
	if err != nil {
		t.Fatalf("load rulebook: %v", err)
	}
	pos := Position{
		U: 100, Class: "equity",
		Legs: []Leg{{Side: Long, Kind: StockKind, Shares: 100}},
	}
	res, err := rb.Evaluate(pos, MarginAccount, Initial)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Requirement != 0 {
		t.Fatalf("requirement = %v, want 0 (zero-valued K should read as 0.0)", res.Requirement)
	}
}

// TestLoadRulebook_mplWrongArgFails asserts that calling mpl() with a
// non-legs argument is rejected at load time by cel-go's overload resolver,
// not silently zero-evaluated at runtime. The tightened signature
// `mpl(map<string, engine.Leg>) → double` makes a string argument a type
// mismatch the compiler can name.
func TestLoadRulebook_mplWrongArgFails(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: r_mpl_bad
    match:
      legs:
        - { name: opt, side: long, kind: option }
    formulas:
      margin:
        initial: 'mpl("bad")'
        maintenance: "0.0"
`)
	assertRulebookError(t, err, "found no matching overload for 'mpl'")
	assertRulebookError(t, err, "r_mpl_bad")
}

// TestLoadRulebook_isLimitedRiskWrongArgFails covers the map-arg case for
// the bool-returning predicate: a string where map<string, engine.Leg> is
// expected must be a load-time overload mismatch.
func TestLoadRulebook_isLimitedRiskWrongArgFails(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: r_ilr_bad
    match:
      legs:
        - { name: opt, side: long, kind: option }
      constraints:
        - 'is_limited_risk("bad")'
    formulas:
      margin:
        initial: "0.0"
        maintenance: "0.0"
`)
	assertRulebookError(t, err, "found no matching overload for 'is_limited_risk'")
	assertRulebookError(t, err, "r_ilr_bad")
}

// TestLoadRulebook_sumShortPremiumsWrongArgFails covers the two-arg map
// case so a typoed first argument (string vs legs map) is caught at load.
func TestLoadRulebook_sumShortPremiumsWrongArgFails(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: r_ssp_bad
    match:
      legs:
        - { name: opt, side: short, kind: option }
    formulas:
      margin:
        initial: 'sum_short_premiums("bad", "P0")'
        maintenance: "0.0"
`)
	assertRulebookError(t, err, "found no matching overload for 'sum_short_premiums'")
	assertRulebookError(t, err, "r_ssp_bad")
}

// TestLoadRulebook_shortCallReqWrongArgFails covers the single-leg first-arg
// case. With short_call_req declared as
// `(engine.Leg, double, string, double, double) → double`, a string in the
// first position must fail overload resolution at load time. (short_put_req
// shares the declaration shape — one representative test stands in for both.)
func TestLoadRulebook_shortCallReqWrongArgFails(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: r_scr_bad
    match:
      legs:
        - { name: sc, side: short, kind: option, option_type: call }
    formulas:
      margin:
        initial: 'short_call_req("bad", U, class, lev, 0.0)'
        maintenance: "0.0"
`)
	assertRulebookError(t, err, "found no matching overload for 'short_call_req'")
	assertRulebookError(t, err, "r_scr_bad")
}

// TestLoadRulebook_baselineCompilesCleanly is the smoke test guarding the
// signature tightening: if any baseline-rule callsite (e.g. mpl(legs),
// is_limited_risk(legs), short_call_req(legs.sc, ...)) silently resolves to a
// different overload after the change, this load fails. Pairs with the
// negative tests above.
func TestLoadRulebook_baselineCompilesCleanly(t *testing.T) {
	if _, err := LoadRulebook("../../rules/cboe_baseline.yaml"); err != nil {
		t.Fatalf("baseline rulebook should load cleanly: %v", err)
	}
}

func writeTempRulebook(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rules.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp rulebook: %v", err)
	}
	return path
}

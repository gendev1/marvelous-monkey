package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustValidationError asserts that rb.Evaluate (AND rb.EvaluateAll) reject
// pos with a validation error — i.e. an error whose message starts with the
// "invalid position" prefix that recon's classifier relies on to distinguish
// validation failures from "no rule matched". Both code paths run the same
// validation, so we exercise both here rather than risk one path drifting.
func mustValidationError(t *testing.T, rb *Rulebook, pos Position) {
	t.Helper()
	_, err := rb.Evaluate(pos, MarginAccount, Initial)
	if err == nil {
		t.Fatalf("Evaluate: expected validation error, got nil")
	}
	if !strings.HasPrefix(err.Error(), "invalid position") {
		t.Fatalf("Evaluate: error %q lacks 'invalid position' prefix", err.Error())
	}
	if _, err := rb.EvaluateAll(pos, MarginAccount, Initial); err == nil {
		t.Fatalf("EvaluateAll: expected validation error, got nil")
	}
}

// Sound baseline used as a starting point in tests below: one well-formed
// short put. Mutating helpers shave off exactly one required field so the
// failure mode under test is the only thing that differs from a valid case.
func validShortPut() Position {
	return Position{
		U:     95.0,
		Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 80, P: 2.0, P0: 2.0, Qty: 1, Mult: 100},
		},
	}
}

func TestValidate_missingQty(t *testing.T) {
	rb := loadRB(t)
	pos := validShortPut()
	pos.Legs[0].Qty = 0
	mustValidationError(t, rb, pos)
}

func TestValidate_missingStrike(t *testing.T) {
	rb := loadRB(t)
	pos := validShortPut()
	pos.Legs[0].K = 0
	mustValidationError(t, rb, pos)
}

func TestValidate_invalidSide(t *testing.T) {
	rb := loadRB(t)
	pos := validShortPut()
	pos.Legs[0].Side = Side("buy")
	mustValidationError(t, rb, pos)
}

func TestValidate_invalidKind(t *testing.T) {
	rb := loadRB(t)
	pos := validShortPut()
	pos.Legs[0].Kind = Kind("future")
	mustValidationError(t, rb, pos)
}

func TestValidate_emptyClass(t *testing.T) {
	rb := loadRB(t)
	pos := validShortPut()
	pos.Class = ""
	mustValidationError(t, rb, pos)
}

func TestValidate_nonPositiveU(t *testing.T) {
	rb := loadRB(t)
	pos := validShortPut()
	pos.U = 0
	mustValidationError(t, rb, pos)
}

func TestValidate_missingOptionType(t *testing.T) {
	rb := loadRB(t)
	pos := validShortPut()
	pos.Legs[0].OptionType = ""
	mustValidationError(t, rb, pos)
}

func TestValidate_negativeMult(t *testing.T) {
	rb := loadRB(t)
	pos := validShortPut()
	// preparePosition only defaults Mult==0 to 100; a negative slips through
	// without this check and silently sign-flips every qty*mult formula.
	pos.Legs[0].Mult = -100
	mustValidationError(t, rb, pos)
}

func TestValidate_negativePremium(t *testing.T) {
	rb := loadRB(t)
	pos := validShortPut()
	pos.Legs[0].P = -1.0
	mustValidationError(t, rb, pos)
}

func TestValidate_stockMissingShares(t *testing.T) {
	rb := loadRB(t)
	// Covered call shape minus stock shares.
	pos := Position{
		U: 92.38, Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 90, P: 7.0, P0: 7.0, Qty: 1, Mult: 100, Style: "american"},
			{Side: Long, Kind: StockKind, Shares: 0},
		},
	}
	mustValidationError(t, rb, pos)
}

// Convertible leg without a price — the rule formula is
// `0.50 * legs.conv.price * legs.conv.shares` and would silently return 0
// (Requirement=0) without this check, which is the confidently-wrong number
// validation exists to prevent.
func TestValidate_convertibleMissingPrice(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U: 80.0, Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 90, P: 3.0, P0: 3.0, Qty: 1, Mult: 100},
			{Side: Long, Kind: ConvertibleKind,
				Price: 0, Shares: 100, KEquivalent: 90.0},
		},
	}
	mustValidationError(t, rb, pos)
}

func TestValidate_warrantMissingPrice(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U: 46.0, Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 45, P: 2.0, P0: 2.0, Qty: 1, Mult: 100},
			{Side: Long, Kind: WarrantKind,
				Price: 0, Shares: 100, KEquivalent: 50.0},
		},
	}
	mustValidationError(t, rb, pos)
}

// Warrant K_equivalent is the exercise price referenced by
// `max(0, K_equivalent - sc.K)`. Zero K_equivalent makes that term silently
// drop out, hiding a wrong economic input behind a still-positive number.
func TestValidate_warrantMissingKEquivalent(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U: 46.0, Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 45, P: 2.0, P0: 2.0, Qty: 1, Mult: 100},
			{Side: Long, Kind: WarrantKind,
				Price: 4.0, Shares: 100, KEquivalent: 0},
		},
	}
	mustValidationError(t, rb, pos)
}

// Empty Legs is NOT a validation error — Item 2.4 of the plan requires this
// to fall through to no-match and be bucketed as NO_RULE by recon. If a
// future change makes empty legs a validation error, recon's NO_RULE bucket
// for vendor positions with no leg rows breaks.
func TestValidate_emptyLegsNotError(t *testing.T) {
	rb := loadRB(t)
	pos := Position{U: 95.0, Class: "equity"}
	_, err := rb.Evaluate(pos, MarginAccount, Initial)
	if err == nil {
		t.Fatalf("expected no-match error for empty legs, got nil")
	}
	if strings.HasPrefix(err.Error(), "invalid position") {
		t.Fatalf("empty legs must be no-match, not validation error: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "no rule matched") {
		t.Fatalf("empty legs must surface 'no rule matched', got %q", err.Error())
	}
}

func validVerticalSpread() Position {
	return Position{
		U: 128.50, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call",
				K: 125, P: 3.80, P0: 3.80, Qty: 1, Mult: 100,
				Style: "american", Venue: "listed", Underlying: "XYZ", Expiration: "2024-11-15"},
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 120, P: 8.40, P0: 8.40, Qty: 1, Mult: 100,
				Style: "american", Venue: "listed", Underlying: "XYZ", Expiration: "2024-11-15"},
		},
	}
}

func TestRuleInputValidation_verticalBlankUnderlying(t *testing.T) {
	rb := loadRB(t)
	pos := validVerticalSpread()
	pos.Legs[0].Underlying = ""
	pos.Legs[1].Underlying = ""
	mustValidationError(t, rb, pos)
}

func TestRuleInputValidation_verticalBlankExpiration(t *testing.T) {
	rb := loadRB(t)
	pos := validVerticalSpread()
	pos.Legs[0].Expiration = ""
	pos.Legs[1].Expiration = ""
	mustValidationError(t, rb, pos)
}

func TestRuleInputValidation_verticalBlankVenue(t *testing.T) {
	rb := loadRB(t)
	pos := validVerticalSpread()
	pos.Legs[0].Venue = ""
	pos.Legs[1].Venue = ""
	mustValidationError(t, rb, pos)
}

func TestRuleInputValidation_genericMixedUnderlyings(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U: 550.0, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "put",
				K: 540, P: 5.60, P0: 5.60, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ"},
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 550, P: 7.20, P0: 7.20, Qty: 2, Mult: 100, Style: "american", Underlying: "ABC"},
			{Side: Long, Kind: OptionKind, OptionType: "put",
				K: 555, P: 9.80, P0: 9.80, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ"},
		},
	}
	mustValidationError(t, rb, pos)
}

func TestRuleInputValidation_shortPutShortStockMissingShortSaleProceeds(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U: 255.0, Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "put",
				K: 250, P: 3.0, P0: 3.0, Qty: 1, Mult: 100, Style: "american"},
			{Side: Short, Kind: StockKind, Shares: 100, SalePrice: 255},
		},
	}
	mustValidationError(t, rb, pos)
}

// -----------------------------------------------------------------------------
// Rulebook schema validation. Each test writes a minimal YAML stub exhibiting
// exactly one failure mode, loads it, and asserts the "invalid rulebook" prefix
// — chosen so future structured-error callers can string-distinguish from
// CEL compile failures further down LoadRulebook.

// loadRulebookFromYAML writes content to a temp file and loads it. Returns
// the load error; the caller asserts on its shape. Each test is intentionally
// self-contained — small enough that copy-paste beats parameterization.
func loadRulebookFromYAML(t *testing.T, content string) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rules.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp rulebook: %v", err)
	}
	_, err := LoadRulebook(path)
	return err
}

func assertRulebookError(t *testing.T, err error, wantSubstr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.HasPrefix(err.Error(), "invalid rulebook") {
		t.Fatalf("error %q lacks 'invalid rulebook' prefix", err.Error())
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Fatalf("error %q does not contain %q", err.Error(), wantSubstr)
	}
}

func TestRulebookValidation_missingSchemaVersion(t *testing.T) {
	err := loadRulebookFromYAML(t, `
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: r1
    match:
      legs:
        - { name: a, side: long, kind: option }
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`)
	assertRulebookError(t, err, "schema_version")
}

func TestRulebookValidation_duplicateRuleIDs(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: dup
    match:
      legs:
        - { name: a, side: long,  kind: option }
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
  - id: dup
    match:
      legs:
        - { name: b, side: short, kind: option }
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`)
	assertRulebookError(t, err, `duplicate rule id "dup"`)
}

func TestRulebookValidation_unknownLegsPattern(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: r1
    match:
      legs_pattern: any_options
      min_legs: 1
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`)
	assertRulebookError(t, err, "legs_pattern")
}

func TestRulebookValidation_bothLegsAndPattern(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: r1
    match:
      legs_pattern: all_options
      legs:
        - { name: a, side: long, kind: option }
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`)
	assertRulebookError(t, err, "both match.legs and legs_pattern")
}

func TestRulebookValidation_duplicateSlotNames(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: r1
    match:
      legs:
        - { name: a, side: long,  kind: option }
        - { name: a, side: short, kind: option }
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`)
	assertRulebookError(t, err, `duplicate slot name "a"`)
}

func TestRulebookValidation_duplicateSlotSignatures(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: r1
    match:
      legs:
        - { name: a, side: long, kind: option }
        - { name: b, side: long, kind: option }
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`)
	assertRulebookError(t, err, "share attribute signature")
}

func TestRulebookValidation_ruleWithNoOutput(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: r1
    match:
      legs:
        - { name: a, side: long, kind: option }
`)
	assertRulebookError(t, err, "no formula, permitted, or deposit_kind")
}

func TestRulebookValidation_emptyRuleID(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: ""
    match:
      legs:
        - { name: a, side: long, kind: option }
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`)
	assertRulebookError(t, err, "id is empty")
}

func TestRulebookValidation_emptySlotName(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: r1
    match:
      legs:
        - { name: "", side: long, kind: option }
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`)
	assertRulebookError(t, err, "name is empty")
}

// A formula whose CEL output is bool/string/null silently became 0 under the
// old asFloat path. celNumber must now surface that as an error at eval time.
// Using `1 == 1` instead of the literal `true` so the failure is unambiguously
// an eval-time type mismatch, not a compile-time identifier lookup miss.
func TestEvaluate_nonNumericFormulaFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.yaml")
	yaml := `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: bool_formula
    match:
      legs:
        - { name: a, side: long, kind: option }
    formulas:
      margin:
        initial:     "1 == 1"
        maintenance: "0.0"
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write temp rulebook: %v", err)
	}
	rb, err := LoadRulebook(path)
	if err != nil {
		t.Fatalf("LoadRulebook: %v", err)
	}
	pos := Position{
		U: 100.0, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call",
				K: 100, P: 1.0, P0: 1.0, Qty: 1, Mult: 100},
		},
	}
	_, err = rb.Evaluate(pos, MarginAccount, Initial)
	if err == nil {
		t.Fatal("expected error for non-numeric formula, got nil")
	}
	if !strings.Contains(err.Error(), "formula must return a number") {
		t.Fatalf("error %q lacks expected substring 'formula must return a number'", err.Error())
	}
}

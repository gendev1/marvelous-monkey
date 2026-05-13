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

// short_index_call_long_etf K_equivalent lives on the ETF leg (`le`), not on
// the short index call (`sc`). The maintenance formula reads
// `min(le.price, le.K_equivalent)` — a zero KEquivalent on the ETF leg would
// collapse that minimum to 0 and produce a confidently-wrong $0 requirement,
// which is the failure mode validateRuleInputs exists to catch.
func TestRuleInputValidation_shortIndexCallLongETFMissingKEquivalent(t *testing.T) {
	rb := loadRB(t)
	pos := Position{
		U: 450.0, Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call",
				K: 4500, P: 10.0, P0: 10.0, Qty: 1, Mult: 100,
				Underlying: "XYZ_INDEX"},
			{Side: Long, Kind: ETFKind,
				Price: 450.0, Shares: 100, KEquivalent: 0,
				TracksIndex: "XYZ_INDEX", Leveraged: false},
		},
	}
	_, err := rb.Evaluate(pos, MarginAccount, Initial)
	if err == nil {
		t.Fatalf("expected validation error for le.KEquivalent==0, got nil")
	}
	if !strings.HasPrefix(err.Error(), "invalid position") {
		t.Fatalf("error %q lacks 'invalid position' prefix", err.Error())
	}
	if !strings.Contains(err.Error(), "le") || !strings.Contains(err.Error(), "K_equivalent") {
		t.Fatalf("error %q should point at slot 'le' and field 'K_equivalent'", err.Error())
	}
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

// A formula whose CEL output is bool/string/null used to compile cleanly and
// fail only at Evaluate time via the celNumber guard. The assertion has been
// moved to load: LoadRulebook now rejects with "formula must return a number"
// and the wrapping "invalid rulebook" / formula-label context so the rule and
// formula key are pinpointed in the error chain.
func TestLoadRulebook_nonNumericFormulaFails(t *testing.T) {
	err := loadRulebookFromYAML(t, `
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
`)
	assertRulebookError(t, err, "formula must return a number")
	if !strings.Contains(err.Error(), "margin.initial") {
		t.Fatalf("error %q lacks formula label 'margin.initial'", err.Error())
	}
}

// A string-typed formula is the other shape that used to slip through compile
// and fail only at celNumber. Asserted at load now.
func TestLoadRulebook_stringFormulaFails(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: str_formula
    match:
      legs:
        - { name: a, side: long, kind: option }
    formulas:
      margin:
        initial:     "'hi'"
        maintenance: "0.0"
`)
	assertRulebookError(t, err, "formula must return a number")
}

// Proceeds formulas (initial_proceeds / maintenance_proceeds) go through the
// same compile path as initial / maintenance, so they must be type-asserted
// too. Without coverage here, a regression that drops them from formulaExprs()
// would still pass the other formula tests.
func TestLoadRulebook_proceedsNonNumericRejects(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: bool_proceeds
    match:
      legs:
        - { name: a, side: long, kind: option }
    formulas:
      margin:
        initial:           "0.0"
        maintenance:       "0.0"
        initial_proceeds:  "1 == 1"
`)
	assertRulebookError(t, err, "formula must return a number")
	if !strings.Contains(err.Error(), "margin.initial_proceeds") {
		t.Fatalf("error %q lacks formula label 'margin.initial_proceeds'", err.Error())
	}
}

// Double-typed formulas (the common case) must continue to load cleanly.
func TestLoadRulebook_doubleFormulaLoads(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: dbl_formula
    match:
      legs:
        - { name: a, side: long, kind: option }
    formulas:
      margin:
        initial:     "0.25 * legs.a.K"
        maintenance: "0.0"
`)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// Int-typed formulas must load: CEL Int is one of the accepted numeric output
// types, and celNumber handles the runtime conversion to float64.
func TestLoadRulebook_intFormulaLoads(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: int_formula
    match:
      legs:
        - { name: a, side: long, kind: option }
    formulas:
      margin:
        initial:     "42"
        maintenance: "0.0"
`)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// long_box_spread-shaped conditional: both branches return Double but cel-go's
// checker is sometimes conservative through conditionals and may report
// DynType. The lenient allowlist must accept this shape — defense-in-depth
// at eval time is still provided by celNumber.
func TestLoadRulebook_dynConditionalFormulaLoads(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: cond_formula
    match:
      legs_pattern: all_options
      min_legs: 2
    formulas:
      margin:
        initial:     "legs.size() > 0 ? 0.50 * U : mpl(legs) + 0.0"
        maintenance: "0.0"
`)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// A permitted:false rule with empty initial/maintenance must keep loading.
// The empty-expr skip at rulebook.go:114 means the new assertion never sees
// an empty string and never spuriously rejects.
func TestLoadRulebook_permittedFalseEmptyFormulasLoad(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: not_permitted
    match:
      legs:
        - { name: a, side: long, kind: option }
    formulas:
      cash:
        permitted: false
`)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// A constraint that returns a non-bool (e.g. `1 + 1`) must NOT be rejected at
// load in this issue — constraint-bool assertion is the sibling issue's scope.
// This proves the kind discriminator is wired and not over-eager. The sibling
// issue will flip this expectation.
func TestLoadRulebook_nonBoolConstraintNotYetRejected(t *testing.T) {
	err := loadRulebookFromYAML(t, `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: int_constraint
    match:
      legs:
        - { name: a, side: long, kind: option }
      constraints:
        - "1 + 1"
    formulas:
      margin:
        initial:     "0.0"
        maintenance: "0.0"
`)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

package engine

import (
	"math"
	"strings"
	"testing"
)

// Tests exercise the runtime interpreter for RequireSpec one primitive at a
// time using minimal inline rulebooks. Each negative case asserts the
// "invalid position:" prefix and names the offending slot/field; the prefix
// is the contract callers (notably recon) rely on to distinguish validation
// failures from no-match outcomes.

func loadRBFromYAML(t *testing.T, content string) *Rulebook {
	t.Helper()
	path := writeTempRulebook(t, content)
	rb, err := LoadRulebook(path)
	if err != nil {
		t.Fatalf("LoadRulebook: %v", err)
	}
	return rb
}

func assertInvalidPosition(t *testing.T, err error, wantSubstrs ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.HasPrefix(err.Error(), "invalid position:") {
		t.Fatalf("error %q lacks 'invalid position:' prefix", err.Error())
	}
	for _, s := range wantSubstrs {
		if !strings.Contains(err.Error(), s) {
			t.Fatalf("error %q does not contain %q", err.Error(), s)
		}
	}
}

// Two-slot vertical-spread-like rule. required_fields exercises the
// blank-string rejection path.
const yamlRequiredFields = `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: req_required_fields
    match:
      legs:
        - { name: long_leg, side: long, kind: option }
        - { name: short_leg, side: short, kind: option }
    requires:
      required_fields:
        long_leg: [underlying]
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`

func TestRequires_RequiredFields_BlankRejected(t *testing.T) {
	rb := loadRBFromYAML(t, yamlRequiredFields)
	pos := Position{
		U: 100, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100, Underlying: ""},
			{Side: Short, Kind: OptionKind, OptionType: "call", K: 110, P: 1, P0: 1, Qty: 1, Mult: 100, Underlying: "X"},
		},
	}
	_, err := rb.Evaluate(pos, MarginAccount, Initial)
	assertInvalidPosition(t, err, "req_required_fields", "legs.long_leg.underlying")
}

func TestRequires_RequiredFields_AllPresentAccepts(t *testing.T) {
	rb := loadRBFromYAML(t, yamlRequiredFields)
	pos := Position{
		U: 100, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100, Underlying: "X"},
			{Side: Short, Kind: OptionKind, OptionType: "call", K: 110, P: 1, P0: 1, Qty: 1, Mult: 100, Underlying: "X"},
		},
	}
	if _, err := rb.Evaluate(pos, MarginAccount, Initial); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

// positive_fields: use time_to_expiration_months because it's an unvalidated
// numeric field — validatePosition would otherwise pre-reject zeros/NaN on
// fields it owns (qty/K/mult/...).
const yamlPositiveFields = `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: req_positive_fields
    match:
      legs:
        - { name: opt, side: long, kind: option }
    requires:
      positive_fields:
        opt: [time_to_expiration_months]
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`

func TestRequires_PositiveFields_ZeroRejected(t *testing.T) {
	rb := loadRBFromYAML(t, yamlPositiveFields)
	pos := Position{
		U: 100, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100, TimeToExpirationMonths: 0},
		},
	}
	_, err := rb.Evaluate(pos, MarginAccount, Initial)
	assertInvalidPosition(t, err, "req_positive_fields", "legs.opt.time_to_expiration_months > 0")
}

func TestRequires_PositiveFields_NaNRejected(t *testing.T) {
	rb := loadRBFromYAML(t, yamlPositiveFields)
	pos := Position{
		U: 100, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100, TimeToExpirationMonths: math.NaN()},
		},
	}
	_, err := rb.Evaluate(pos, MarginAccount, Initial)
	assertInvalidPosition(t, err, "req_positive_fields", "legs.opt.time_to_expiration_months")
}

const yamlExpirationSlots = `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: req_expiration
    match:
      legs:
        - { name: opt, side: long, kind: option }
    requires:
      expiration_slots: [opt]
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`

func TestRequires_ExpirationSlots_MalformedRejected(t *testing.T) {
	rb := loadRBFromYAML(t, yamlExpirationSlots)
	pos := Position{
		U: 100, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100, Expiration: "not-a-date"},
		},
	}
	_, err := rb.Evaluate(pos, MarginAccount, Initial)
	assertInvalidPosition(t, err, "req_expiration", "legs.opt.expiration", "YYYY-MM-DD")
}

func TestRequires_ExpirationSlots_BlankRejected(t *testing.T) {
	rb := loadRBFromYAML(t, yamlExpirationSlots)
	pos := Position{
		U: 100, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100},
		},
	}
	_, err := rb.Evaluate(pos, MarginAccount, Initial)
	assertInvalidPosition(t, err, "req_expiration", "legs.opt.expiration")
}

const yamlSameAcross = `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: req_same_across
    match:
      legs:
        - { name: a, side: long, kind: option }
        - { name: b, side: short, kind: option }
    requires:
      same_across_slots:
        - { field: underlying, slots: [a, b] }
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`

func TestRequires_SameAcrossSlots_MismatchRejected(t *testing.T) {
	rb := loadRBFromYAML(t, yamlSameAcross)
	pos := Position{
		U: 100, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100, Underlying: "X"},
			{Side: Short, Kind: OptionKind, OptionType: "call", K: 110, P: 1, P0: 1, Qty: 1, Mult: 100, Underlying: "Y"},
		},
	}
	_, err := rb.Evaluate(pos, MarginAccount, Initial)
	assertInvalidPosition(t, err, "req_same_across", "underlying")
}

func TestRequires_SameAcrossSlots_BlankRejected(t *testing.T) {
	rb := loadRBFromYAML(t, yamlSameAcross)
	pos := Position{
		U: 100, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100, Underlying: ""},
			{Side: Short, Kind: OptionKind, OptionType: "call", K: 110, P: 1, P0: 1, Qty: 1, Mult: 100, Underlying: ""},
		},
	}
	_, err := rb.Evaluate(pos, MarginAccount, Initial)
	assertInvalidPosition(t, err, "req_same_across", "legs.a.underlying")
}

const yamlSameContractSize = `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: req_same_contract
    match:
      legs:
        - { name: a, side: long, kind: option }
        - { name: b, side: short, kind: option }
    requires:
      same_contract_size:
        - [a, b]
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`

func TestRequires_SameContractSize_MismatchRejected(t *testing.T) {
	rb := loadRBFromYAML(t, yamlSameContractSize)
	pos := Position{
		U: 100, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100},
			{Side: Short, Kind: OptionKind, OptionType: "call", K: 110, P: 1, P0: 1, Qty: 2, Mult: 100},
		},
	}
	_, err := rb.Evaluate(pos, MarginAccount, Initial)
	assertInvalidPosition(t, err, "req_same_contract", "qty*mult")
}

const yamlMinFields = `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: req_min_fields
    match:
      legs:
        - { name: sc, side: short, kind: option }
        - { name: ls, side: long,  kind: stock }
    requires:
      min_fields:
        - { slot: ls, field: shares, gte: "legs.sc.qty * legs.sc.mult" }
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`

func TestRequires_MinFields_BelowRejected(t *testing.T) {
	rb := loadRBFromYAML(t, yamlMinFields)
	pos := Position{
		U: 100, Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100},
			{Side: Long, Kind: StockKind, Shares: 99, Mult: 1},
		},
	}
	_, err := rb.Evaluate(pos, MarginAccount, Initial)
	assertInvalidPosition(t, err, "req_min_fields", "legs.ls.shares", "got 99", "need 100")
}

// min_fields gte that resolves to NaN must surface as an "invalid position:"
// error, not slip through as a passing comparison. Feed a NaN-valued leg
// field into the gte expression to force celNumber's non-finite guard.
const yamlMinFieldsNaN = `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: req_min_fields_nan
    match:
      legs:
        - { name: opt, side: long, kind: option }
    requires:
      min_fields:
        - { slot: opt, field: K, gte: "legs.opt.time_to_expiration_months" }
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`

func TestRequires_MinFields_NaNFromExpressionRejected(t *testing.T) {
	rb := loadRBFromYAML(t, yamlMinFieldsNaN)
	pos := Position{
		U: 100, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100, TimeToExpirationMonths: math.NaN()},
		},
	}
	_, err := rb.Evaluate(pos, MarginAccount, Initial)
	assertInvalidPosition(t, err, "req_min_fields_nan")
}

// all_slots binds across every leg matched by an `all_options` rule. Use the
// matcher's bound-leg names (L0/L1) for the error-message assertion.
const yamlAllSlotsRequired = `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: req_all_required
    match:
      legs_pattern: all_options
      min_legs: 1
    requires:
      all_slots:
        required_fields: [underlying]
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`

func TestRequires_AllSlots_RequiredFieldRejected(t *testing.T) {
	rb := loadRBFromYAML(t, yamlAllSlotsRequired)
	pos := Position{
		U: 100, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100, Underlying: ""},
		},
	}
	_, err := rb.Evaluate(pos, MarginAccount, Initial)
	assertInvalidPosition(t, err, "req_all_required", "underlying")
}

const yamlAllSlotsSame = `
schema_version: "1"
rates:
  equity: { base_pct: 0.20, min_pct: 0.10 }
rules:
  - id: req_all_same
    match:
      legs_pattern: all_options
      min_legs: 2
    requires:
      all_slots:
        same_field: underlying
    formulas:
      margin: { initial: "0.0", maintenance: "0.0" }
`

func TestRequires_AllSlots_SameFieldMismatchRejected(t *testing.T) {
	rb := loadRBFromYAML(t, yamlAllSlotsSame)
	pos := Position{
		U: 100, Class: "equity",
		Legs: []Leg{
			{Side: Long, Kind: OptionKind, OptionType: "call", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100, Underlying: "X"},
			{Side: Long, Kind: OptionKind, OptionType: "put", K: 100, P: 1, P0: 1, Qty: 1, Mult: 100, Underlying: "Y"},
		},
	}
	_, err := rb.Evaluate(pos, MarginAccount, Initial)
	assertInvalidPosition(t, err, "req_all_same", "underlying")
}

// TestRequires_EmptySpec confirms the interpreter is a no-op on rules with
// no requires block — load the live cboe_baseline (no requires today) and
// pick one fixture per migration batch to prove the wiring didn't regress.
func TestRequires_DualPathSamePass(t *testing.T) {
	rb := loadRB(t)
	// Short put OTM (p.28) — single-slot rule path.
	pos1 := Position{
		U: 95.0, Class: "equity",
		Legs: []Leg{{Side: Short, Kind: OptionKind, OptionType: "put", K: 80, P: 2.0, P0: 2.0, Qty: 1, Mult: 100}},
	}
	res1 := mustEvaluate(t, rb, pos1, MarginAccount, Initial)
	assertClose(t, "p28 short put OTM via requires path", res1.Requirement, 1000.00)

	// Covered call (p.47) — multi-slot rule path with validateRuleInputs.
	pos2 := Position{
		U: 92.38, Class: "equity",
		Legs: []Leg{
			{Side: Short, Kind: OptionKind, OptionType: "call", K: 90, P: 7.0, P0: 7.0, Qty: 1, Mult: 100, Style: "american", Underlying: "XYZ"},
			{Side: Long, Kind: StockKind, Shares: 100, Underlying: "XYZ"},
		},
	}
	res2 := mustEvaluate(t, rb, pos2, MarginAccount, Initial)
	assertClose(t, "p47 covered call via requires path", res2.Requirement, 4619.00)
}

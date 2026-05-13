package overlay

import (
	"time"

	"margincalc/internal/account"

	"github.com/google/cel-go/cel"
)

// Engine evaluates an overlay Rulebook against an account snapshot and
// reference data, producing a HouseRequirement. A nil Rulebook makes
// Evaluate a passthrough that emits the baseline numbers and no
// components.
type Engine struct {
	Rulebook *Rulebook
}

// Rulebook is the compiled, in-memory representation of the overlay
// rule set. Loaded from one or more YAML files via LoadRulebook. Safe
// for concurrent reads once LoadRulebook returns — every CEL program is
// compiled eagerly so there are no lazy-write paths.
//
// Note on currency (D4/D5): the loader emits no per-rule currency
// filter and does not error on rules that omit currency. Currency is
// implicit from Account.Currency at evaluation time.
type Rulebook struct {
	// OverlayRulebookHash is a SHA-256 over the concatenated bytes of
	// the loaded YAML files (in input order, with a separator byte
	// between files). Surfaced into HouseRequirement.Audit by issue 6.
	OverlayRulebookHash string

	constants map[string]any
	rules     []overlayRule // sorted deterministic order
}

// CompiledRules returns the rules in deterministic evaluation order:
// (priority asc, fileIndex asc, declIndex asc, id asc). Exported on the
// package's unexported type via the in-package test surface; callers
// outside overlay/ use the engine to consume rulebooks.
func (rb *Rulebook) ruleCount() int { return len(rb.rules) }

// overlayRule is the loaded + compiled representation of a single
// overlay rule. Fields prefixed lowercase are populated by LoadRulebook
// and treated as read-only after load.
type overlayRule struct {
	ID                 string
	Priority           int
	Scope              string
	GroupBy            string
	Applies            AppliesSpec
	When               string
	Mode               string
	Basis              string
	Formula            string
	Reason             string
	OnMissingReference string

	// fileIndex is the position of the source file in the input list
	// to LoadRulebook; declIndex is the rule's position within that
	// file. Together with priority and id they form the sort key.
	fileIndex int
	declIndex int

	whenProg    cel.Program
	formulaProg cel.Program // nil when mode == "block" and formula is empty
}

// AppliesSpec captures the optional applicability filters on a rule.
// All fields are inclusion lists; an empty list means "no filter, all
// values match". Currencies is reserved for future use (see D4/D5);
// loader does not validate its contents today.
type AppliesSpec struct {
	AccountTypes    []string `yaml:"account_types,omitempty"`
	Phases          []string `yaml:"phases,omitempty"`
	InstrumentKinds []string `yaml:"instrument_kinds,omitempty"`
	Sides           []string `yaml:"sides,omitempty"`
	Currencies      []string `yaml:"currencies,omitempty"`
}

// HouseRequirement is the customer-facing house number with full
// attribution: the baseline from Layer 2, the applied overlay
// components, any violations, and an audit trail. The embedded Snapshot
// preserves Layer-2 inputs so downstream consumers do not need to keep
// it side-by-side.
type HouseRequirement struct {
	AccountID string
	AsOf      time.Time
	Currency  string

	BaselineRequirement float64 // snap.TotalRequirement at evaluation time.
	BaselineCashCall    float64 // snap.TotalCashCall at evaluation time.

	HouseRequirement float64
	HouseCashCall    float64
	Excess           float64

	Components []HouseComponent
	Violations []HouseViolation
	Warnings   []string
	Audit      AuditTrail

	Snapshot account.AccountSnapshot
}

// HouseComponent is a single attributed adjustment from one overlay
// rule. Mode is one of four string values:
//
//   - "add"   — add OverlayAmount to the house requirement.
//   - "max"   — raise the scope's requirement to at least OverlayAmount
//     (only positive delta over baseline applies).
//   - "floor" — numerically equivalent to "max"; reserved name for
//     account minimums and per-share floors.
//   - "block" — emit both this component (with Delta = 0 and
//     Applied = true so the audit trail records the match) and a
//     HouseViolation. Any formula output is recorded for inspection
//     but is not added to HouseRequirement.
//
// Evidence is map[string]float64 — auditable without schema lock-in,
// mirroring engine.Result's plain-map design.
type HouseComponent struct {
	RuleID     string
	Scope      string // "account" | "position" | "symbol" | "group"
	Mode       string // "add" | "max" | "floor" | "block"
	Basis      string // "market_value" | "shares" | "group_gross_mv" | "account_equity"
	PositionID string
	Symbol     string
	GroupKey   string

	BaselineAmount float64
	OverlayAmount  float64
	Delta          float64
	Applied        bool

	Formula  string
	Reason   string
	Evidence map[string]float64
}

// HouseViolation is the customer-facing policy-block record paired with
// a "block"-mode HouseComponent. It carries the rule identity and a
// human-readable message; the dollar-level audit lives on the
// component.
type HouseViolation struct {
	RuleID     string
	Scope      string
	PositionID string
	Symbol     string
	GroupKey   string
	Message    string
}

// SecKey identifies a security in ReferenceData. Venue mirrors
// engine.Leg.Venue ("listed" | "otc") so the same symbol traded on
// different venues can carry distinct facts. Constructed by callers;
// SecKey is exported on purpose.
type SecKey struct {
	Symbol string
	Venue  string
}

// String renders the key as "SYMBOL@VENUE" for log lines and audit
// inputs.
func (k SecKey) String() string {
	return k.Symbol + "@" + k.Venue
}

// ReferenceData is the overlay engine's read-only side input: per
// security facts keyed by SecKey. A nil Securities map is permitted at
// the type level — typical Go zero-value rules apply, so a lookup just
// returns the zero SecurityFacts. Loader-level validation policy lives
// in the loader issue.
type ReferenceData struct {
	Securities map[SecKey]SecurityFacts
}

// SecurityFacts is the per-security reference row. Symbol and Venue
// mirror the SecKey under which the value is stored so a SecurityFacts
// value taken out of the map still carries its own identifier; loader
// validation enforces consistency. An empty Venue is permitted at the
// type level. InstrumentKind defaults to "stock" when missing in the
// upstream feed (the loader applies the default — this type holds
// whatever value the loader sets).
type SecurityFacts struct {
	Symbol          string
	Venue           string
	InstrumentKind  string // "stock" | "etf" | "etn" | "leveraged_etf" | "adr"
	Underlying      string
	IssuerID        string
	Sector          string
	Industry        string
	GICSSubIndustry string

	LastPrice         float64
	ADV20             float64
	MedianVolume20    float64
	MarketCap         float64
	SharesOutstanding float64

	Volatility30D   float64
	HardToBorrow    bool
	BorrowRate      float64
	Marginable      bool
	LeveragedFactor float64
}

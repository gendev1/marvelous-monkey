package overlay

import (
	"errors"
	"time"

	"margincalc/internal/account"
)

// errNotImplemented is the sentinel returned by stubs in this skeleton
// PR. Subsequent issues replace it with real behavior; tests assert on
// this value as a regression target.
var errNotImplemented = errors.New("overlay: not implemented")

// Engine evaluates an overlay Rulebook against an account snapshot and
// reference data, producing a HouseRequirement. The zero value is not
// useful; construct an Engine with a loaded Rulebook before calling
// Evaluate. Behavior is filled in by later issues; this skeleton only
// declares the type surface.
type Engine struct {
	rulebook *Rulebook
}

// Rulebook is the compiled, in-memory representation of the overlay
// rule set. Loaded from one or more YAML files via LoadRulebook. The
// zero value is empty and contains no rules.
type Rulebook struct{}

// LoadRulebook reads one or more overlay YAML files, validates them,
// and returns a compiled Rulebook. In this skeleton it returns an empty
// Rulebook and no error; the real loader lands in a later issue.
func LoadRulebook(paths ...string) (*Rulebook, error) {
	return &Rulebook{}, nil
}

// Evaluate runs every overlay rule against the snapshot and returns the
// attributed HouseRequirement. It must not mutate acct or snap. In this
// skeleton it returns a zero HouseRequirement and the not-implemented
// sentinel so callers can wire against the API now.
func (e *Engine) Evaluate(
	acct account.Account,
	snap account.AccountSnapshot,
	ref ReferenceData,
) (HouseRequirement, error) {
	return HouseRequirement{}, errNotImplemented
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

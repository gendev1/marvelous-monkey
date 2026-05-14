// Package optimizer implements the Layer-0.5 spread-optimizer scaffold.
//
// This file declares the public surface (Optimizer, BucketFacts, WorkingLeg,
// Decomposition, SubPosition, SlotAssignment, Attribution, LegID) plus the
// residual-error sentinels and their priority comparator. Branch-and-bound,
// rule-aware consumption, and memoization land in subsequent PRs; in this PR
// Optimize delegates to scoreAllResidual.
//
// See docs/architecture/test.md §"Optimizer types", §"Residual option
// handling", §"Partial-output contract", and §"Strongest-residual-error
// priority".
package optimizer

import (
	"fmt"
	"strings"

	"margincalc/internal/engine"
)

// LegID is the caller-supplied stable identifier for an input WorkingLeg.
// Attribution and state hashing key on LegID — never on engine.Leg struct
// identity — so input slices may be cloned, reordered, or rebuilt across
// calls.
type LegID string

// BucketFacts carries the single-underlying context shared by every
// WorkingLeg in one Optimize call.
type BucketFacts struct {
	U                       float64
	Class                   string
	Lev                     float64 // 0 → engine defaults to 1
	UnderlyingIsEquityBased bool
}

// WorkingLeg is one input leg plus its remaining open size. Exactly one of
// OpenQty / OpenShares must be > 0 (option contracts vs. stock-like shares);
// a leg with both populated is malformed input.
type WorkingLeg struct {
	ID         LegID
	Leg        engine.Leg
	OpenQty    float64 // option contracts remaining
	OpenShares float64 // stock-like shares remaining
}

// Optimizer is the entry point for decomposition. It is safe to share across
// goroutines once constructed; the underlying *engine.Rulebook is concurrent-
// safe by design.
type Optimizer struct {
	rb          *engine.Rulebook
	accountType engine.AccountType
	phase       engine.Phase
}

// New constructs an Optimizer bound to a rulebook + (account, phase) pair.
func New(rb *engine.Rulebook, at engine.AccountType, ph engine.Phase) *Optimizer {
	return &Optimizer{rb: rb, accountType: at, phase: ph}
}

// Decomposition is the result of one Optimize call. When the returned error
// is non-nil, SubPositions / AttributionsByLeg / TotalRequirement still
// describe every leg that did score successfully — scoring is not aborted at
// the first failing leg; all input legs are visited, errors are collected,
// and the strongest one (per compareResidualErr) is returned alongside this
// partial Decomposition (see §"Partial-output contract").
type Decomposition struct {
	SubPositions      []SubPosition
	AttributionsByLeg map[LegID][]Attribution
	TotalRequirement  float64
}

// SubPosition is one rule's worth of scored output. Slots map slot names to
// the legs (cloned with consumed amounts) the matcher bound for them.
type SubPosition struct {
	StrategyID string
	Slots      map[string]SlotAssignment
	Result     engine.Result
}

// SlotAssignment records which input leg fed which slot, and how much of its
// open amount this sub-position consumed.
type SlotAssignment struct {
	OriginalLegID  LegID
	Leg            engine.Leg // clone with Qty / Shares set to consumed amount
	ConsumedQty    float64    // 0 if stock-like slot
	ConsumedShares float64    // 0 if option slot
}

// Attribution is the per-input-leg view of how that leg was decomposed across
// sub-positions. Reason is a deterministic string; LLM-narrated reasons are
// explicitly out of scope for v1.
type Attribution struct {
	SubPositionIdx int
	SlotName       string
	ConsumedQty    float64
	ConsumedShares float64
	Reason         string
}

// ErrStockResidualUnsupported is returned for any leg with OpenShares > eps
// that the optimizer cannot consume into a strategy template. v1 has no
// standalone naked-stock rule, so any unconsumed stock surfaces here.
type ErrStockResidualUnsupported struct {
	LegID      LegID
	OpenShares float64
}

func (e *ErrStockResidualUnsupported) Error() string {
	return fmt.Sprintf("stock residual unsupported: leg %q has %g unconsumed shares", string(e.LegID), e.OpenShares)
}

// ErrNoNakedRule is returned when residualOptionRule walks the entire naked
// candidate sequence without any rule binding (e.g. a long OTC option with an
// empty venue cannot bind any listed/OTC long-dated rule).
type ErrNoNakedRule struct {
	LegID LegID
	Leg   engine.Leg
}

func (e *ErrNoNakedRule) Error() string {
	return fmt.Sprintf("no naked rule binds leg %q (side=%s kind=%s option_type=%s)",
		string(e.LegID), e.Leg.Side, e.Leg.Kind, e.Leg.OptionType)
}

// residualErrPriority assigns the priority ordering described in
// §"Strongest-residual-error priority": hard engine error (3) > ErrNoNakedRule
// (2) > ErrStockResidualUnsupported (1). nil is 0.
func residualErrPriority(err error) int {
	if err == nil {
		return 0
	}
	switch err.(type) {
	case *ErrStockResidualUnsupported:
		return 1
	case *ErrNoNakedRule:
		return 2
	default:
		return 3
	}
}

// residualErrLegID extracts the LegID a residual error refers to for
// alphabetical tiebreaking. Returns "" for hard engine errors (which lack a
// LegID); the comparator falls back to error-message order in that case.
func residualErrLegID(err error) LegID {
	switch e := err.(type) {
	case *ErrStockResidualUnsupported:
		return e.LegID
	case *ErrNoNakedRule:
		return e.LegID
	default:
		return ""
	}
}

// compareResidualErr returns a positive value when a is the "stronger"
// residual error per §"Strongest-residual-error priority", negative when b is
// stronger, and 0 on a true tie. Priority is hard engine error > ErrNoNakedRule
// > ErrStockResidualUnsupported; ties are broken by LegID alphabetical, then
// by Error() string for hard engine errors with no LegID.
//
// Callers pick the "strongest" by retaining whichever side this returns >= 0
// for, i.e. argmax.
func compareResidualErr(a, b error) int {
	pa, pb := residualErrPriority(a), residualErrPriority(b)
	if pa != pb {
		return pa - pb
	}
	if pa == 0 {
		return 0
	}
	// Tie-break: alphabetical-earlier LegID wins per
	// §"Strongest-residual-error priority". `pickStronger` uses argmax (a
	// wins on >= 0), so we must invert strings.Compare so the lex-smaller
	// LegID produces the positive value.
	if cmp := strings.Compare(string(residualErrLegID(a)), string(residualErrLegID(b))); cmp != 0 {
		return -cmp
	}
	// Both have the same priority and same LegID (or both empty for hard
	// engine errors). Fall back to Error() text for total ordering — again
	// inverted so the alphabetically-earlier message wins under argmax.
	return -strings.Compare(a.Error(), b.Error())
}

// Optimize decomposes the input bucket into recognized strategies.
//
// In this PR every leg is scored as a residual (naked-option rule per leg, or
// ErrStockResidualUnsupported for stock-like legs). B&B branching lands in
// PR-3.
//
// Contract:
//   - empty input → Decomposition{TotalRequirement: 0}, nil.
//   - WorkingLeg with both OpenQty > eps and OpenShares > eps is malformed
//     input — returns a validation error and the empty Decomposition.
//   - On success, returns a complete Decomposition with all legs attributed.
//   - On error, returns a partial Decomposition holding every leg that did
//     score successfully (scoring is not aborted at the first failing leg —
//     all legs are visited, errors are collected, and the strongest one per
//     compareResidualErr is returned).
func (o *Optimizer) Optimize(facts BucketFacts, legs []WorkingLeg) (Decomposition, error) {
	state, err := buildState(legs)
	if err != nil {
		return Decomposition{}, err
	}
	if len(state.Legs) == 0 {
		return Decomposition{TotalRequirement: 0}, nil
	}
	return o.scoreAllResidual(state, facts)
}

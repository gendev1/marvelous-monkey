// Package optimizer is the spread-aware decomposition layer (Layer 0.5) that
// sits between raw working positions and the engine's strategy-rule dispatch.
//
// This file is the v1 scaffold described in issue #72: the public types, error
// shapes, the residual-only Optimize entry point, and the strongest-error
// priority. Branch-and-bound decomposition, consumptionFor, and quantity
// slicing land in subsequent issues — Optimize here only walks the residual
// completion path so a working leg with OpenQty > 0 can be scored against the
// deterministic naked-rule sequence.
package optimizer

import (
	"errors"
	"fmt"
	"strings"

	"margincalc/internal/engine"
)

// LegID identifies a working leg within a bucket. The optimizer treats it as
// an opaque string for attribution and error diagnostics; callers (account
// aggregation, recon) are responsible for choosing IDs that are stable across
// reruns so deterministic output is meaningful.
type LegID string

// WorkingLeg is one row of the optimizer's input: a leg shape paired with the
// open quantity (option contracts) or open shares not yet consumed by an
// already-attributed sub-position. Exactly one of OpenQty / OpenShares is
// expected to be > 0 for any given leg; both > 0 is a programmer error and
// surfaced by Optimize.
type WorkingLeg struct {
	ID         LegID
	Leg        engine.Leg
	OpenQty    float64
	OpenShares float64
}

// BucketFacts carries the position-level inputs the engine needs but that the
// optimizer treats as constants for the duration of one Optimize call.
type BucketFacts struct {
	U                       float64
	Class                   string
	Lev                     float64
	UnderlyingIsEquityBased bool
	AccountType             engine.AccountType
	Phase                   engine.Phase
}

// SlotAssignment records which working leg filled a named slot of a strategy
// rule, and how much of its open quantity was consumed.
type SlotAssignment struct {
	Slot       string
	LegID      LegID
	QtyUsed    float64
	SharesUsed float64
}

// SubPosition is one scored decomposition: a strategy rule plus the leg
// assignments that produced its Result.
type SubPosition struct {
	StrategyID string
	Slots      []SlotAssignment
	Result     engine.Result
}

// Attribution maps a working leg back to the sub-positions it participated in.
// Useful downstream for explaining why a leg's quantity was split a particular
// way and for driving per-leg cash-call disclosures.
type Attribution struct {
	SubIndex   int
	Slot       string
	QtyUsed    float64
	SharesUsed float64
}

// Decomposition is the optimizer's output: the chosen sub-positions, the
// per-leg attributions that point back into them, and the summed Requirement.
//
// On error returns the sub-positions accumulated before the failure are still
// present (partial-output contract); callers can inspect them for diagnostics.
type Decomposition struct {
	SubPositions     []SubPosition
	Attributions     map[LegID][]Attribution
	TotalRequirement float64

	// err carries a propagated error through the memoized recursion. Unexported
	// so external callers always interact through the (Decomposition, error)
	// return of Optimize; IsError/Err expose it for the few internal callers
	// (combine, the test harness) that need to introspect a carrier.
	err error
}

// ErrNoNakedRule is the residual-completion failure mode: a working option
// leg with OpenQty > 0 did not match any of the deterministic naked-rule
// candidates. The leg is carried for caller-side diagnostics (logging, recon
// classification).
type ErrNoNakedRule struct {
	LegID LegID
	Leg   engine.Leg
}

func (e *ErrNoNakedRule) Error() string {
	return fmt.Sprintf("optimizer: no naked rule matched residual leg %q", string(e.LegID))
}

// ErrStockResidualUnsupported signals that residual completion was asked to
// score open shares — there is no naked-stock sink rule yet. The shares count
// and original leg are carried so callers can render a meaningful diagnostic
// without re-deriving them from the input.
type ErrStockResidualUnsupported struct {
	LegID      LegID
	OpenShares float64
	Leg        engine.Leg
}

func (e *ErrStockResidualUnsupported) Error() string {
	return fmt.Sprintf("optimizer: stock residual unsupported for leg %q (%.0f shares)", string(e.LegID), e.OpenShares)
}

// Optimizer evaluates working legs against a Rulebook. It is read-only with
// respect to the rulebook and safe for concurrent use to the same extent
// engine.Rulebook is.
type Optimizer struct {
	rb    *engine.Rulebook
	nodes int
}

// New constructs an Optimizer bound to a loaded rulebook.
func New(rb *engine.Rulebook) *Optimizer {
	return &Optimizer{rb: rb}
}

// NodeCount returns the number of decompose entries during the most recent
// Optimize call. Exposed for instrumentation tests (epic test 14) so future
// pruning work can detect regressions in the search size. Reset to 0 at the
// top of each Optimize invocation.
func (o *Optimizer) NodeCount() int {
	if o == nil {
		return 0
	}
	return o.nodes
}

// Optimize runs the branch-and-bound decomposition over the working legs:
// every state is scored against every (optimizer-target rule, valid
// assignment, viable consumption plan) candidate; the recursive remainder
// is scored the same way; residual-only completion is always considered as
// a baseline. The cheapest decomposition under the documented tiebreak
// (see less) wins. Memoization on State.Key() keeps total work proportional
// to the number of distinct reachable states.
//
// Residual-only behavior is preserved end-to-end: a state with no viable
// B&B branch falls through to residualOptionRule for every leg, and the
// strongest-priority residual error (see compareResidualErr) wins. The
// returned Decomposition carries whatever sub-positions were scored before
// the failure (partial-output contract).
//
// An empty legs slice returns Decomposition{} with TotalRequirement=0 and no
// error. A leg with both OpenQty and OpenShares > 0 violates the input
// invariant and returns a programmer-error err.
func (o *Optimizer) Optimize(facts BucketFacts, legs []WorkingLeg) (Decomposition, error) {
	// Defensive: a nil Optimizer or a New(nil) construction would otherwise
	// panic deep inside residualOptionRule when the rulebook is dereferenced.
	// Surface a clear error at the entry point instead.
	if o == nil || o.rb == nil {
		return Decomposition{}, fmt.Errorf("optimizer: nil Optimizer or rulebook (constructed with New(nil)?)")
	}
	// Validate the input invariants up-front so the early return on a violation
	// doesn't leave Decomposition partially populated (no Attributions, no
	// TotalRequirement) — an inconsistent state would defeat the
	// partial-output contract and confuse callers.
	//
	// Duplicate LegIDs are rejected because the B&B path keys memoization,
	// consumption deltas, and Attributions by LegID — two working legs sharing
	// an ID would silently merge their open quantity into one bucket and
	// misattribute the result.
	seen := make(map[LegID]struct{}, len(legs))
	for _, wl := range legs {
		if _, dup := seen[wl.ID]; dup {
			return Decomposition{}, fmt.Errorf("optimizer: duplicate LegID %q (invariant violation)", string(wl.ID))
		}
		seen[wl.ID] = struct{}{}
		if wl.OpenQty > 0 && wl.OpenShares > 0 {
			return Decomposition{}, fmt.Errorf("optimizer: leg %q has both OpenQty (%g) and OpenShares (%g) > 0 (invariant violation)",
				string(wl.ID), wl.OpenQty, wl.OpenShares)
		}
	}
	o.nodes = 0
	state := newState(legs)
	memo := map[string]Decomposition{}
	result := o.decompose(state, facts, memo, nil)
	if result.IsError() {
		err := result.err
		result.err = nil
		result.Attributions = buildAttributions(result.SubPositions)
		return result, err
	}
	result.Attributions = buildAttributions(result.SubPositions)
	return result, nil
}

// scoreAllResidual scores every working leg in s independently via
// residualOptionRule (or rejects shares as unsupported in this PR). It is
// the residual-only completion path called from decompose; the strongest
// residual error wins and any successfully-scored sub-positions ride along
// in the returned Decomposition.
func (o *Optimizer) scoreAllResidual(s State, facts BucketFacts) (Decomposition, error) {
	dec := Decomposition{}
	var strongest error
	for _, wl := range s.Legs {
		if wl.OpenShares > 0 {
			candidate := &ErrStockResidualUnsupported{LegID: wl.ID, OpenShares: wl.OpenShares, Leg: wl.Leg}
			strongest = takeStronger(strongest, candidate)
			continue
		}
		if wl.OpenQty <= 0 {
			continue
		}
		sub, err := residualOptionRule(o.rb, wl, facts)
		if err != nil {
			strongest = takeStronger(strongest, err)
			continue
		}
		dec.SubPositions = append(dec.SubPositions, sub)
	}
	for _, sp := range dec.SubPositions {
		dec.TotalRequirement += sp.Result.Requirement
	}
	return dec, strongest
}

// takeStronger returns whichever of (current, candidate) ranks higher under
// compareResidualErr. nil is treated as the weakest possible.
func takeStronger(current, candidate error) error {
	if current == nil {
		return candidate
	}
	if compareResidualErr(candidate, current) > 0 {
		return candidate
	}
	return current
}

// compareResidualErr orders residual-completion errors by severity:
//
//	hard engine error  >  *ErrNoNakedRule  >  *ErrStockResidualUnsupported
//
// The stronger error wins so the caller hears about the most actionable
// failure first (a CEL/configuration bug shouldn't be hidden by a downstream
// "no rule" miss). Within the same kind, ties break by the alphabetically
// smallest LegID — both directions are deterministic, but smaller-first
// matches how callers naturally enumerate leg IDs and keeps the chosen
// diagnostic stable when input order changes.
//
// Returns +1 when a is stronger, -1 when b is stronger, 0 when equal.
func compareResidualErr(a, b error) int {
	ra := residualErrRank(a)
	rb := residualErrRank(b)
	if ra != rb {
		if ra > rb {
			return 1
		}
		return -1
	}
	// Smaller LegID is "stronger" within a tie. strings.Compare(b, a) is
	// positive iff a < b, which is exactly the "a wins" condition.
	return strings.Compare(residualErrLegID(b), residualErrLegID(a))
}

func residualErrRank(err error) int {
	if err == nil {
		return 0
	}
	var n *ErrNoNakedRule
	if errors.As(err, &n) {
		return 2
	}
	var s *ErrStockResidualUnsupported
	if errors.As(err, &s) {
		return 1
	}
	return 3 // hard engine / configuration error
}

func residualErrLegID(err error) string {
	var n *ErrNoNakedRule
	if errors.As(err, &n) {
		return string(n.LegID)
	}
	var s *ErrStockResidualUnsupported
	if errors.As(err, &s) {
		return string(s.LegID)
	}
	return ""
}

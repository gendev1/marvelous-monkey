// Package account is the aggregation layer above internal/engine. It turns
// an Account plus a slice of AccountPosition into an AccountSnapshot with
// MV buckets, equity, exposure, leverage, and per-position margin
// requirements. v1 is rulebook-driven and vendor-free.
package account

import (
	"time"

	"margincalc/internal/engine"
)

// Account is the per-customer input to the aggregator. Numeric scalars are
// float64 to match the engine's CEL-strict-double typing.
type Account struct {
	ID                  string
	AccountType         engine.AccountType
	Phase               engine.Phase
	AsOf                time.Time
	Currency            string
	SODEquity           float64
	CashBalance         float64
	PnL                 float64
	DepositsWithdrawals float64
	Positions           []AccountPosition
}

// AccountPosition wraps engine.Position with a stable identifier so position
// identity can travel into PositionEvaluation.PositionID and into error /
// violation reports without polluting the engine's input shape.
type AccountPosition struct {
	ID       string
	Position engine.Position
}

// PositionEvaluation is the per-position outcome of running the engine.
// NoRule and Violation are first-class classifications distinct from Error
// (see epic §"Data / API Model").
type PositionEvaluation struct {
	PositionID string
	Result     engine.Result
	Error      error
	NoRule     bool
	Violation  bool
}

// AccountSnapshot is the aggregator's deterministic output. SMV fields carry
// positive magnitudes per the resolved sign convention.
type AccountSnapshot struct {
	AccountID   string
	AccountType engine.AccountType
	Phase       engine.Phase
	AsOf        time.Time
	Currency    string

	LMVStock  float64
	LMVOption float64
	SMVStock  float64
	SMVOption float64

	NetMV         float64
	GrossExposure float64

	SODEquity           float64
	CashBalance         float64
	PnL                 float64
	DepositsWithdrawals float64
	CurrentEquity       float64
	AdjustedBalance     float64

	TotalRequirement    float64
	TotalCashCall       float64
	StockLeverage       float64
	GrossLeverage       float64
	EquityRatio         float64
	DepositRequirements map[string]float64

	Evaluations []PositionEvaluation
	Violations  []PositionEvaluation
	Errors      []PositionEvaluation

	// Warnings holds account-level non-error notes (e.g. the zero-equity
	// guard message). Distinct from Errors so consumers don't confuse a
	// degenerate but valid snapshot with a per-position evaluation failure.
	Warnings []string
}

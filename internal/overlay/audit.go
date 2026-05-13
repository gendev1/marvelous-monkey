package overlay

// AuditTrail is the deterministic record of an overlay evaluation:
// rulebook hashes plus one AuditEntry per rule considered (matched or
// not). Consumers persist it alongside HouseRequirement for vendor
// reconciliation and customer-facing display.
type AuditTrail struct {
	BaselineRulebookHash string
	OverlayRulebookHash  string
	Entries              []AuditEntry
}

// AuditEntry is the per-rule audit row. Matched records whether the
// rule's scope and `when` predicate fired; Applied records whether the
// resulting delta actually moved the house requirement (e.g. a "max"
// rule whose formula amount fell below baseline is Matched but not
// Applied). Inputs carries the named numeric values the formula saw so
// the row is reproducible without re-running CEL.
type AuditEntry struct {
	RuleID     string
	Priority   int
	Scope      string
	PositionID string
	Symbol     string
	GroupKey   string

	Matched bool
	Applied bool
	Mode    string
	Formula string
	Amount  float64
	Delta   float64

	Inputs map[string]float64
}

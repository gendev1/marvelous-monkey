package overlay

// AuditTrail is the deterministic record of an overlay evaluation:
// rulebook hashes plus one AuditEntry per rule considered against every
// scope-target (matched or not). Consumers persist it alongside
// HouseRequirement for vendor reconciliation and customer-facing
// display. Entry ordering follows the rulebook's deterministic rule
// order with targets ordered by snapshot position order (position
// scope) or byte-sorted group key (group scope).
type AuditTrail struct {
	BaselineRulebookHash string
	OverlayRulebookHash  string
	Entries              []AuditEntry
}

// AuditEntry is the per-(rule, target) audit row. Matched records
// whether the rule's applies-matrix and `when` predicate fired against
// the target; Applied records whether the resulting delta actually
// moved the house requirement (e.g. a "max" rule whose formula amount
// fell below baseline is Matched but not Applied). Inputs carries the
// named numeric values the activation exposed so the row is
// reproducible without re-running CEL.
//
// All fields are concrete numeric / string / bool / map types so the
// row JSON-serializes without custom marshalers (D6).
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

// newAuditEntry seeds the identity fields of an AuditEntry from the
// rule. Target identifiers (PositionID, Symbol, GroupKey) are filled
// in by the caller because they vary per scope.
func newAuditEntry(rule overlayRule) AuditEntry {
	return AuditEntry{
		RuleID:   rule.ID,
		Priority: rule.Priority,
		Scope:    rule.Scope,
		Mode:     rule.Mode,
		Formula:  rule.Formula,
	}
}

// collectNumericInputs flattens an activation map into the
// "namespace.field" -> float64 form used by AuditEntry.Inputs. Non-
// numeric leaf values (strings, bools) are skipped because Inputs is
// constrained to numerics for D6 JSON friendliness.
func collectNumericInputs(activation map[string]any) map[string]float64 {
	out := map[string]float64{}
	for ns, val := range activation {
		nested, ok := val.(map[string]any)
		if !ok {
			continue
		}
		for k, v := range nested {
			f, ok := toFloat64(v)
			if !ok {
				continue
			}
			out[ns+"."+k] = f
		}
	}
	return out
}

// toFloat64 narrows the activation leaf type union to float64. Mirrors
// the conversions the engine layer does for YAML constants so int and
// uint variants surface as floats in the audit trail without losing
// precision for typical sizes.
func toFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	}
	return 0, false
}

package overlay

import "fmt"

// Closed enums shared between the loader and (future) the engine. Kept
// package-level so the future evaluator reuses the exact sets the
// loader validated against.
var (
	validScopes = map[string]struct{}{
		"account":  {},
		"position": {},
		"symbol":   {},
		"group":    {},
	}
	validModes = map[string]struct{}{
		"add":   {},
		"max":   {},
		"floor": {},
		"block": {},
	}
	// validBases accepts the canonical short tokens documented in the
	// epic plus the long-form variants that appear in canonical YAML.
	// Both are kept legal so authors can pick the more readable form
	// in any given file; the evaluator treats short/long aliases as
	// identical.
	validBases = map[string]struct{}{
		"market_value":             {},
		"shares":                   {},
		"group_gross_mv":           {},
		"account_equity":           {},
		"position_market_value":    {}, // alias for market_value at position scope
		"group_gross_market_value": {}, // alias for group_gross_mv
	}
	validAccountTypes = map[string]struct{}{
		"cash":   {},
		"margin": {},
	}
	validPhases = map[string]struct{}{
		"initial":     {},
		"maintenance": {},
	}
	validInstrumentKinds = map[string]struct{}{
		"stock":         {},
		"etf":           {},
		"etn":           {},
		"leveraged_etf": {},
		"adr":           {},
		"option":        {},
	}
	validSides = map[string]struct{}{
		"long":  {},
		"short": {},
	}
	validOnMissingRef = map[string]struct{}{
		"warn":  {},
		"error": {},
	}
	validGroupBy = map[string]struct{}{
		"underlying": {},
		"symbol":     {},
	}
)

// validateRawRule runs structural checks on a single parsed rule. CEL
// compile checks live in compileRule (rulebook.go) so they can run
// after the env is built. Errors are prefixed "invalid overlay
// rulebook:" per the acceptance criteria; the rule id and source path
// are included so a malformed multi-file load remains diagnosable.
func validateRawRule(path string, idx int, r rawRule) error {
	if r.ID == "" {
		return fmt.Errorf("invalid overlay rulebook: %s rule[%d] id is empty", path, idx)
	}
	if r.Scope == "" {
		return fmt.Errorf("invalid overlay rulebook: rule %q scope is required", r.ID)
	}
	if _, ok := validScopes[r.Scope]; !ok {
		return fmt.Errorf("invalid overlay rulebook: rule %q scope %q is not one of account/position/symbol/group", r.ID, r.Scope)
	}
	if r.Mode == "" {
		return fmt.Errorf("invalid overlay rulebook: rule %q mode is required", r.ID)
	}
	if _, ok := validModes[r.Mode]; !ok {
		return fmt.Errorf("invalid overlay rulebook: rule %q mode %q is not one of add/max/floor/block", r.ID, r.Mode)
	}
	if r.Basis != "" {
		if _, ok := validBases[r.Basis]; !ok {
			return fmt.Errorf("invalid overlay rulebook: rule %q basis %q is not one of market_value/shares/group_gross_mv/account_equity/position_market_value/group_gross_market_value", r.ID, r.Basis)
		}
	}
	if err := checkEnumList("rule "+r.ID+" applies.account_types", r.Applies.AccountTypes, validAccountTypes); err != nil {
		return err
	}
	if err := checkEnumList("rule "+r.ID+" applies.phases", r.Applies.Phases, validPhases); err != nil {
		return err
	}
	if err := checkEnumList("rule "+r.ID+" applies.instrument_kinds", r.Applies.InstrumentKinds, validInstrumentKinds); err != nil {
		return err
	}
	if err := checkEnumList("rule "+r.ID+" applies.sides", r.Applies.Sides, validSides); err != nil {
		return err
	}
	if _, ok := validOnMissingRef[r.OnMissingReference]; !ok {
		return fmt.Errorf("invalid overlay rulebook: rule %q on_missing_reference %q is not one of warn/error", r.ID, r.OnMissingReference)
	}
	if r.Scope == "group" {
		if r.GroupBy == "" {
			return fmt.Errorf("invalid overlay rulebook: rule %q has scope=group but no group_by", r.ID)
		}
		if _, ok := validGroupBy[r.GroupBy]; !ok {
			return fmt.Errorf("invalid overlay rulebook: rule %q group_by %q is not one of underlying/symbol", r.ID, r.GroupBy)
		}
	} else if r.GroupBy != "" {
		return fmt.Errorf("invalid overlay rulebook: rule %q has group_by but scope is %q (expected group)", r.ID, r.Scope)
	}
	// Mode "block" doesn't need a numeric formula — its purpose is the
	// violation record. Every other mode does: a "max" or "add" with
	// no formula has no overlay amount to apply.
	if r.Mode != "block" && r.Formula == "" {
		return fmt.Errorf("invalid overlay rulebook: rule %q mode %q requires a formula", r.ID, r.Mode)
	}
	return nil
}

func checkEnumList(label string, values []string, allowed map[string]struct{}) error {
	for _, v := range values {
		if _, ok := allowed[v]; !ok {
			return fmt.Errorf("invalid overlay rulebook: %s has unknown value %q", label, v)
		}
	}
	return nil
}

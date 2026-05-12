package margincalc

import (
	"fmt"
	"os"

	"github.com/google/cel-go/cel"
	"gopkg.in/yaml.v3"
)

// --- YAML schema mirrors the rules file ---

type LegSlot struct {
	Name       string `yaml:"name"`
	Side       string `yaml:"side,omitempty"`
	Kind       string `yaml:"kind,omitempty"`
	OptionType string `yaml:"option_type,omitempty"`
	Venue      string `yaml:"venue,omitempty"`
}

type MatchSpec struct {
	Legs        []LegSlot `yaml:"legs,omitempty"`
	LegsPattern string    `yaml:"legs_pattern,omitempty"`
	MinLegs     int       `yaml:"min_legs,omitempty"`
	MaxLegs     int       `yaml:"max_legs,omitempty"`
	Constraints []string  `yaml:"constraints,omitempty"`
}

type FormulaBlock struct {
	Initial             string `yaml:"initial,omitempty"`
	Maintenance         string `yaml:"maintenance,omitempty"`
	InitialProceeds     string `yaml:"initial_proceeds,omitempty"`
	MaintenanceProceeds string `yaml:"maintenance_proceeds,omitempty"`
	Permitted           *bool  `yaml:"permitted,omitempty"`
	DepositKind         string `yaml:"deposit_kind,omitempty"`
}

type RuleFormulas struct {
	Cash   FormulaBlock `yaml:"cash,omitempty"`
	Margin FormulaBlock `yaml:"margin,omitempty"`
}

type Rule struct {
	ID       string       `yaml:"id"`
	Match    MatchSpec    `yaml:"match"`
	Formulas RuleFormulas `yaml:"formulas"`
}

type rawRulebook struct {
	SchemaVersion string                                 `yaml:"schema_version"`
	Rates         map[string]map[string]float64          `yaml:"rates"`
	Rules         []Rule                                 `yaml:"rules"`
	Constants     map[string]any                         `yaml:"constants,omitempty"`
}

// Rulebook is the loaded, compiled rule set. Safe for concurrent reads.
type Rulebook struct {
	rates    map[string]map[string]float64
	rules    []Rule
	env      *cel.Env
	progs    map[string]cel.Program // cache: rule_id + "/" + key → compiled Program
}

func LoadRulebook(path string) (*Rulebook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rules: %w", err)
	}
	var raw rawRulebook
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse rules: %w", err)
	}
	env, err := buildEnv(raw.Rates)
	if err != nil {
		return nil, fmt.Errorf("build CEL env: %w", err)
	}
	rb := &Rulebook{
		rates: raw.Rates,
		rules: raw.Rules,
		env:   env,
		progs: map[string]cel.Program{},
	}
	// Pre-compile every formula and constraint for fail-fast + cache.
	for _, r := range raw.Rules {
		for _, c := range r.Match.Constraints {
			if _, err := rb.compile(r.ID+"/c:"+c, c); err != nil {
				return nil, fmt.Errorf("rule %s constraint %q: %w", r.ID, c, err)
			}
		}
		for label, expr := range formulaExprs(r) {
			if expr == "" {
				continue
			}
			if _, err := rb.compile(r.ID+"/"+label, expr); err != nil {
				return nil, fmt.Errorf("rule %s formula %s: %w", r.ID, label, err)
			}
		}
	}
	return rb, nil
}

func formulaExprs(r Rule) map[string]string {
	return map[string]string{
		"cash.initial":                r.Formulas.Cash.Initial,
		"cash.maintenance":            r.Formulas.Cash.Maintenance,
		"cash.initial_proceeds":       r.Formulas.Cash.InitialProceeds,
		"cash.maintenance_proceeds":   r.Formulas.Cash.MaintenanceProceeds,
		"margin.initial":              r.Formulas.Margin.Initial,
		"margin.maintenance":          r.Formulas.Margin.Maintenance,
		"margin.initial_proceeds":     r.Formulas.Margin.InitialProceeds,
		"margin.maintenance_proceeds": r.Formulas.Margin.MaintenanceProceeds,
	}
}

func (rb *Rulebook) compile(key, expr string) (cel.Program, error) {
	if prog, ok := rb.progs[key]; ok {
		return prog, nil
	}
	ast, iss := rb.env.Compile(expr)
	if iss.Err() != nil {
		return nil, iss.Err()
	}
	prog, err := rb.env.Program(ast)
	if err != nil {
		return nil, err
	}
	rb.progs[key] = prog
	return prog, nil
}

// Evaluate finds the first rule that matches `pos` and returns the requirement
// for (accountType, phase). Returns an error if no rule matches.
func (rb *Rulebook) Evaluate(pos Position, accountType AccountType, phase Phase) (Result, error) {
	if pos.Lev == 0 {
		pos.Lev = 1.0
	}
	for i := range pos.Legs {
		if pos.Legs[i].Mult == 0 {
			pos.Legs[i].Mult = 100.0
		}
	}

	for _, rule := range rb.rules {
		bound, ok := rb.tryMatch(pos, rule)
		if !ok {
			continue
		}
		// Build CEL activation
		legsMap := map[string]any{}
		for name, leg := range bound {
			legsMap[name] = leg.toMap()
		}
		activation := map[string]any{
			"U":     pos.U,
			"class": pos.Class,
			"lev":   pos.Lev,
			"legs":  legsMap,
		}

		// Check constraints. An evaluation *error* is surfaced (likely a bug
		// in the rule); a clean `false` simply demotes to "doesn't match".
		constraintsHold := true
		for _, c := range rule.Match.Constraints {
			prog, err := rb.compile(rule.ID+"/c:"+c, c)
			if err != nil {
				return Result{}, fmt.Errorf("compile constraint %s %q: %w", rule.ID, c, err)
			}
			out, _, err := prog.Eval(activation)
			if err != nil {
				return Result{}, fmt.Errorf("eval constraint %s %q: %w", rule.ID, c, err)
			}
			if b, ok := out.Value().(bool); !ok || !b {
				constraintsHold = false
				break
			}
		}
		if !constraintsHold {
			continue
		}

		// Pull the right formula block
		var block FormulaBlock
		var keyPrefix string
		switch accountType {
		case CashAccount:
			block = rule.Formulas.Cash
			keyPrefix = "cash"
		case MarginAccount:
			block = rule.Formulas.Margin
			keyPrefix = "margin"
		}

		formulaKey := keyPrefix + "." + string(phase)

		// Non-numeric outcomes
		if block.Permitted != nil && !*block.Permitted {
			return Result{RuleID: rule.ID, FormulaKey: formulaKey, AccountType: string(accountType), Phase: string(phase), Permitted: false}, nil
		}

		var expr, proceedsExpr, proceedsKey string
		switch phase {
		case Initial:
			expr = block.Initial
			proceedsExpr = block.InitialProceeds
			proceedsKey = keyPrefix + ".initial_proceeds"
		case Maintenance:
			expr = block.Maintenance
			proceedsExpr = block.MaintenanceProceeds
			proceedsKey = keyPrefix + ".maintenance_proceeds"
		}
		// If only a deposit_kind is given (no numeric formula), return the
		// deposit-only result. Otherwise compute the number AND attach the
		// deposit_kind — both are meaningful: the number is the USD-equivalent
		// cash deposit, the kind describes acceptable forms of that deposit.
		if expr == "" {
			if block.DepositKind != "" {
				return Result{RuleID: rule.ID, FormulaKey: formulaKey, AccountType: string(accountType), Phase: string(phase), Permitted: true, DepositKind: block.DepositKind}, nil
			}
			return Result{}, fmt.Errorf("rule %s has no %s formula", rule.ID, formulaKey)
		}

		prog, err := rb.compile(rule.ID+"/"+formulaKey, expr)
		if err != nil {
			return Result{}, fmt.Errorf("compile %s: %w", rule.ID, err)
		}
		out, _, err := prog.Eval(activation)
		if err != nil {
			return Result{}, fmt.Errorf("eval %s: %w", rule.ID, err)
		}
		// Use asFloat so an integer-typed CEL result (e.g. literal `0`) is
		// accepted instead of panicking on a failed float64 type assertion.
		req := asFloat(out)

		// Proceeds default to 0 when no expression is supplied.
		var proceeds float64
		if proceedsExpr != "" {
			pprog, err := rb.compile(rule.ID+"/"+proceedsKey, proceedsExpr)
			if err != nil {
				return Result{}, fmt.Errorf("compile %s %s: %w", rule.ID, proceedsKey, err)
			}
			pout, _, err := pprog.Eval(activation)
			if err != nil {
				return Result{}, fmt.Errorf("eval %s %s: %w", rule.ID, proceedsKey, err)
			}
			proceeds = asFloat(pout)
		}

		return Result{
			RuleID:          rule.ID,
			FormulaKey:      formulaKey,
			AccountType:     string(accountType),
			Phase:           string(phase),
			Requirement:     req,
			AppliedProceeds: proceeds,
			CashCall:        req - proceeds,
			Permitted:       true,
			DepositKind:     block.DepositKind,
		}, nil
	}
	return Result{}, fmt.Errorf("no rule matched position with %d legs", len(pos.Legs))
}

// --- matching ---

func (rb *Rulebook) tryMatch(pos Position, rule Rule) (map[string]Leg, bool) {
	if rule.Match.LegsPattern == "all_options" {
		bound := map[string]Leg{}
		for i, l := range pos.Legs {
			if l.Kind != OptionKind {
				return nil, false
			}
			bound[fmt.Sprintf("L%d", i)] = l
		}
		if len(bound) < rule.Match.MinLegs {
			return nil, false
		}
		if rule.Match.MaxLegs > 0 && len(bound) > rule.Match.MaxLegs {
			return nil, false
		}
		return bound, true
	}

	if len(rule.Match.Legs) != len(pos.Legs) {
		return nil, false
	}
	bound, ok := bindPermute(pos.Legs, rule.Match.Legs, map[string]Leg{}, make([]bool, len(pos.Legs)))
	return bound, ok
}

func bindPermute(legs []Leg, slots []LegSlot, bound map[string]Leg, used []bool) (map[string]Leg, bool) {
	if len(slots) == 0 {
		// Ensure all position legs were used
		for _, u := range used {
			if !u {
				return nil, false
			}
		}
		return bound, true
	}
	slot := slots[0]
	for i, l := range legs {
		if used[i] {
			continue
		}
		if !slotMatches(slot, l) {
			continue
		}
		newBound := make(map[string]Leg, len(bound)+1)
		for k, v := range bound {
			newBound[k] = v
		}
		newBound[slot.Name] = l
		newUsed := make([]bool, len(used))
		copy(newUsed, used)
		newUsed[i] = true
		if result, ok := bindPermute(legs, slots[1:], newBound, newUsed); ok {
			return result, true
		}
	}
	return nil, false
}

func slotMatches(s LegSlot, l Leg) bool {
	if s.Side != "" && Side(s.Side) != l.Side {
		return false
	}
	if s.Kind != "" && Kind(s.Kind) != l.Kind {
		return false
	}
	if s.OptionType != "" && s.OptionType != l.OptionType {
		return false
	}
	if s.Venue != "" && s.Venue != l.Venue {
		return false
	}
	return true
}

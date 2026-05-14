package engine

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"time"

	"github.com/google/cel-go/cel"
	"gopkg.in/yaml.v3"
)

// RequiresError marks a `requires:` guard failure as distinct from a CEL/
// lookup/configuration error. Callers (notably the optimizer scoring path)
// use errors.As to demote a guard failure to "rule doesn't apply" while
// still propagating real evaluation errors.
type RequiresError struct{ Msg string }

func (e *RequiresError) Error() string { return e.Msg }

func requiresErrorf(format string, args ...any) *RequiresError {
	return &RequiresError{Msg: fmt.Sprintf(format, args...)}
}

// wrapRequires lifts a plain error from a requires-helper into a
// *RequiresError so callers can identify guard failures with errors.As.
// It is a no-op if err already wraps a *RequiresError or is nil.
func wrapRequires(err error) error {
	if err == nil {
		return nil
	}
	var re *RequiresError
	if errors.As(err, &re) {
		return err
	}
	return &RequiresError{Msg: err.Error()}
}

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
	Requires RequireSpec  `yaml:"requires,omitempty"`
	Formulas RuleFormulas `yaml:"formulas"`
	// OptimizerTarget marks whether this rule is a valid decomposition template
	// for the spread optimizer (Layer 0.5). Nil means "use the default policy"
	// (see ruleIsDefaultOptimizerTarget); a non-nil value is an explicit YAML
	// override that wins over the default — including for legs_pattern rules
	// where an author may want to opt the catch-all back in.
	OptimizerTarget *bool `yaml:"optimizer_target,omitempty"`
}

// ruleIsDefaultOptimizerTarget resolves whether a rule should be considered an
// optimizer decomposition target. An explicit YAML `optimizer_target` always
// wins; otherwise the default excludes the `all_options` catch-all and any
// 1-slot rule (naked sinks like `long_option_*` / `short_*_uncovered`).
func ruleIsDefaultOptimizerTarget(r Rule) bool {
	if r.OptimizerTarget != nil {
		return *r.OptimizerTarget
	}
	if r.Match.LegsPattern != "" {
		return false
	}
	return len(r.Match.Legs) >= 2
}

// RequireSpec is the declarative pre-formula validation block. Each rule may
// declare which slot/field combinations must be present, positive, equal across
// slots, or above a CEL-expressed lower bound. The schema is structurally
// validated at load time (see validateRequires); a future issue wires a runtime
// interpreter that produces "invalid position:" errors before formula eval.
type RequireSpec struct {
	RequiredFields   map[string][]string   `yaml:"required_fields,omitempty"`
	PositiveFields   map[string][]string   `yaml:"positive_fields,omitempty"`
	ExpirationSlots  []string              `yaml:"expiration_slots,omitempty"`
	SameAcrossSlots  []SameAcrossSlotsSpec `yaml:"same_across_slots,omitempty"`
	SameContractSize [][]string            `yaml:"same_contract_size,omitempty"`
	MinFields        []MinFieldSpec        `yaml:"min_fields,omitempty"`
	AllSlots         *AllSlotsSpec         `yaml:"all_slots,omitempty"`
}

type SameAcrossSlotsSpec struct {
	Field string   `yaml:"field"`
	Slots []string `yaml:"slots"`
}

// MinFieldSpec asserts legs.<slot>.<field> >= <gte>. GTE compiles against the
// same CEL environment as match.constraints — legs/U/class/lev/constants are
// in scope. The load-time validator only checks that it parses to a numeric
// type; runtime-type assertions on the bound value live in the future
// interpreter.
type MinFieldSpec struct {
	Slot  string `yaml:"slot"`
	Field string `yaml:"field"`
	GTE   string `yaml:"gte"`
}

// AllSlotsSpec is the only requires shape valid under legs_pattern: all_options.
// Slots aren't named there, so checks apply to every bound leg.
type AllSlotsSpec struct {
	RequiredFields []string `yaml:"required_fields,omitempty"`
	SameField      string   `yaml:"same_field,omitempty"`
}

// Closed whitelists shared between the load-time validator and (issue 2) the
// runtime interpreter. Kept package-level so the field accessor work in the
// next issue can reuse the exact same sets without re-deriving them.
var (
	requireStringFields = map[string]struct{}{
		"underlying":   {},
		"style":        {},
		"venue":        {},
		"settle_style": {},
		"tracks_index": {},
	}
	requireNumericFields = map[string]struct{}{
		"qty":                       {},
		"mult":                      {},
		"K":                         {},
		"price":                     {},
		"time_to_expiration_months": {},
		"shares":                    {},
		"short_sale_proceeds":       {},
		"sale_price":                {},
		"K_equivalent":              {},
	}
)

type rawRulebook struct {
	SchemaVersion string                        `yaml:"schema_version"`
	Source        any                           `yaml:"source,omitempty"`
	Rates         map[string]map[string]float64 `yaml:"rates"`
	Rules         []Rule                        `yaml:"rules"`
	Constants     map[string]any                `yaml:"constants,omitempty"`
}

// Rulebook is the loaded, compiled rule set. Safe for concurrent reads once
// LoadRulebook returns — that call pre-compiles every formula and constraint
// into `progs`, so Evaluate only does map lookups. Do not call compile()
// concurrently with Evaluate from outside this file: it writes to `progs`.
type Rulebook struct {
	rates     map[string]map[string]float64
	constants map[string]any
	rules     []Rule
	env       *cel.Env
	progs     map[string]cel.Program // cache: rule_id + "/" + key → compiled Program
}

func LoadRulebook(path string) (*Rulebook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rules: %w", err)
	}
	var raw rawRulebook
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse rules: %w", err)
	}
	if err := validateRawRulebook(raw); err != nil {
		return nil, err
	}
	// Normalize numeric constants to float64 so CEL double arithmetic works
	// regardless of whether the YAML wrote `9` (int) or `9.0` (float).
	constants := map[string]any{}
	for k, v := range raw.Constants {
		switch x := v.(type) {
		case int:
			constants[k] = float64(x)
		case int64:
			constants[k] = float64(x)
		case int32:
			constants[k] = float64(x)
		case uint:
			constants[k] = float64(x)
		case uint32:
			constants[k] = float64(x)
		case uint64:
			constants[k] = float64(x)
		case float32:
			constants[k] = float64(x)
		default:
			constants[k] = v
		}
	}
	env, err := buildEnv(raw.Rates)
	if err != nil {
		return nil, fmt.Errorf("build CEL env: %w", err)
	}
	rb := &Rulebook{
		rates:     raw.Rates,
		constants: constants,
		rules:     raw.Rules,
		env:       env,
		progs:     map[string]cel.Program{},
	}
	// Pre-compile every formula and constraint for fail-fast + cache.
	for _, r := range raw.Rules {
		for _, c := range r.Match.Constraints {
			if _, err := rb.compile(r.ID+"/c:"+c, c, kindConstraint); err != nil {
				return nil, fmt.Errorf("invalid rulebook: rule %s constraint %q: %w", r.ID, c, err)
			}
		}
		for _, mf := range r.Requires.MinFields {
			// Include the gte expression in the key so two entries sharing
			// (slot, field) but differing on gte don't collide in rb.progs.
			key := r.ID + "/req:gte:" + mf.Slot + "." + mf.Field + ":" + mf.GTE
			if _, err := rb.compile(key, mf.GTE, kindFormula); err != nil {
				return nil, fmt.Errorf("invalid rulebook: rule %s min_fields legs.%s.%s gte %q: %w", r.ID, mf.Slot, mf.Field, mf.GTE, err)
			}
		}
		// Sort labels so error wrapping is deterministic when multiple
		// formulas would fail — Go map iteration is randomized.
		exprs := formulaExprs(r)
		labels := make([]string, 0, len(exprs))
		for label := range exprs {
			labels = append(labels, label)
		}
		sort.Strings(labels)
		for _, label := range labels {
			expr := exprs[label]
			if expr == "" {
				continue
			}
			if _, err := rb.compile(r.ID+"/"+label, expr, kindFormula); err != nil {
				return nil, fmt.Errorf("invalid rulebook: rule %s formula %s: %w", r.ID, label, err)
			}
		}
	}
	return rb, nil
}

// exprKind distinguishes formula expressions (must return a number) from
// constraint expressions (must return a bool — enforced in a sibling issue).
// The discriminator is plumbed through compile() so the load-time type
// assertions stay co-located with env.Compile.
type exprKind int

const (
	kindFormula exprKind = iota
	kindConstraint
)

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

// lookupProg returns a pre-compiled program by cache key. It does NOT write to
// rb.progs; calling it at Evaluate time keeps Rulebook concurrent-safe per the
// CLAUDE.md invariant. Panics if the key is missing — LoadRulebook is
// responsible for pre-compiling every formula, constraint, and min_fields gte
// expression, so a miss here is a programming error (cache key drift).
func (rb *Rulebook) lookupProg(key string) cel.Program {
	prog, ok := rb.progs[key]
	if !ok {
		panic(fmt.Sprintf("engine: program %q not pre-compiled; LoadRulebook must populate every cache key before Evaluate", key))
	}
	return prog
}

func (rb *Rulebook) compile(key, expr string, kind exprKind) (cel.Program, error) {
	if prog, ok := rb.progs[key]; ok {
		return prog, nil
	}
	ast, iss := rb.env.Compile(expr)
	if iss.Err() != nil {
		return nil, iss.Err()
	}
	switch kind {
	case kindFormula:
		// Accept Double, Int, and DynType: conditional formulas whose branches
		// both return Double can still be reported as DynType by cel-go's
		// checker (see long_box_spread margin.initial). The eval-time
		// celNumber guard stays as defense-in-depth for DynType slip-through.
		outType := ast.OutputType()
		if !outType.IsExactType(cel.DoubleType) &&
			!outType.IsExactType(cel.IntType) &&
			!outType.IsExactType(cel.DynType) {
			return nil, fmt.Errorf("formula must return a number, got %s", outType)
		}
	case kindConstraint:
		// Strictly require Bool. DynType is intentionally rejected: no real
		// constraint needs leniency today, and tolerating it would re-enable
		// the silent-no-match failure mode (out.Value().(bool) returning
		// ok=false at eval) that this assertion exists to eliminate.
		outType := ast.OutputType()
		if !outType.IsExactType(cel.BoolType) {
			return nil, fmt.Errorf("constraint must return bool, got %s", outType)
		}
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
	pos = rb.preparePosition(pos)
	if err := validatePosition(pos); err != nil {
		return Result{}, err
	}
	for _, rule := range rb.rules {
		res, ok, err := rb.evaluateOne(pos, rule, accountType, phase)
		if err != nil {
			return Result{}, err
		}
		if ok {
			return res, nil
		}
	}
	return Result{}, fmt.Errorf("no rule matched position with %d legs", len(pos.Legs))
}

// EvaluateAll returns the Result for every rule whose match binds AND whose
// constraints all hold for `pos`, in YAML declaration order. Length > 1 means
// rule order is silently shadowing one rule with another in Evaluate; length 0
// means Evaluate would return a no-match error.
//
// Production callers should use Evaluate. EvaluateAll exists to surface
// dispatch ambiguity in tests (assert exactly one match per fixture) and as
// a debug aid when reconciliation produces a surprising number.
func (rb *Rulebook) EvaluateAll(pos Position, accountType AccountType, phase Phase) ([]Result, error) {
	pos = rb.preparePosition(pos)
	if err := validatePosition(pos); err != nil {
		return nil, err
	}
	var matches []Result
	for _, rule := range rb.rules {
		res, ok, err := rb.evaluateOne(pos, rule, accountType, phase)
		if err != nil {
			return nil, err
		}
		if ok {
			matches = append(matches, res)
		}
	}
	return matches, nil
}

// preparePosition applies position-level defaults: Lev=1 if zero, and
// `default_contract_multiplier` (or 100) for any leg with Mult==0. Returns a
// Position with a freshly-copied Legs slice so the caller's slice is never
// written to.
func (rb *Rulebook) preparePosition(pos Position) Position {
	if pos.Lev == 0 {
		pos.Lev = 1.0
	}
	defaultMult := 100.0
	if v, ok := rb.constants["default_contract_multiplier"].(float64); ok {
		defaultMult = v
	}
	legs := make([]Leg, len(pos.Legs))
	copy(legs, pos.Legs)
	for i := range legs {
		if legs[i].Mult == 0 {
			legs[i].Mult = defaultMult
		}
	}
	pos.Legs = legs
	return pos
}

// validatePosition checks universal invariants after preparePosition has
// defaulted Lev and Mult. All errors start with "invalid position" so callers
// (notably recon) can string-distinguish them from the "no rule matched"
// no-match path — validation must NOT be silently bucketed as NO_RULE.
//
// Empty Legs slice is intentionally NOT a validation error: the documented
// behaviour for vendor-supplied positions with no legs is to fall through
// to no-match and be classified as NO_RULE downstream.
func validatePosition(pos Position) error {
	if !isFinite(pos.U) || pos.U <= 0 {
		return fmt.Errorf("invalid position: U must be > 0, got %g", pos.U)
	}
	if pos.Class == "" {
		return fmt.Errorf("invalid position: class is required")
	}
	if !isFinite(pos.Lev) || pos.Lev <= 0 {
		return fmt.Errorf("invalid position: lev must be > 0, got %g", pos.Lev)
	}
	for i, l := range pos.Legs {
		if err := validateLeg(i, l); err != nil {
			return err
		}
	}
	return nil
}

func validateLeg(i int, l Leg) error {
	switch l.Side {
	case Long, Short:
	default:
		return fmt.Errorf("invalid position: leg %d side must be 'long' or 'short', got %q", i, string(l.Side))
	}
	switch l.Kind {
	case OptionKind, StockKind, ETFKind, ETNKind, ConvertibleKind, WarrantKind:
	default:
		return fmt.Errorf("invalid position: leg %d kind %q is not one of option/stock/etf/etn/convertible/warrant", i, string(l.Kind))
	}
	if !isFinite(l.Mult) || l.Mult <= 0 {
		return fmt.Errorf("invalid position: leg %d mult must be > 0, got %g", i, l.Mult)
	}
	if l.Kind == OptionKind {
		if l.OptionType != "put" && l.OptionType != "call" {
			return fmt.Errorf("invalid position: leg %d option_type must be 'put' or 'call', got %q", i, l.OptionType)
		}
		if !isFinite(l.Qty) || l.Qty <= 0 {
			return fmt.Errorf("invalid position: leg %d qty must be > 0, got %g", i, l.Qty)
		}
		if !isFinite(l.K) || l.K <= 0 {
			return fmt.Errorf("invalid position: leg %d K must be > 0, got %g", i, l.K)
		}
		if !isFinite(l.P) || l.P < 0 {
			return fmt.Errorf("invalid position: leg %d P must be >= 0, got %g", i, l.P)
		}
		if !isFinite(l.P0) || l.P0 < 0 {
			return fmt.Errorf("invalid position: leg %d P0 must be >= 0, got %g", i, l.P0)
		}
		return nil
	}
	// stock-like (stock/etf/etn/convertible/warrant): every kind needs a
	// positive share count. Convertible/warrant additionally carry their own
	// price (and the warrant exercise) as type-intrinsic fields — a leg
	// without them isn't a valid instance of the type, regardless of which
	// rule binds it. ETF/ETN price stays unchecked here: only one rule reads
	// it (short_index_call_long_etf) and rule-level required-field validation
	// is the right home for that — see roadmap item #1.
	if !isFinite(l.Shares) || l.Shares <= 0 {
		return fmt.Errorf("invalid position: leg %d shares must be > 0, got %g", i, l.Shares)
	}
	switch l.Kind {
	case ConvertibleKind:
		if !isFinite(l.Price) || l.Price <= 0 {
			return fmt.Errorf("invalid position: leg %d convertible price must be > 0, got %g", i, l.Price)
		}
	case WarrantKind:
		if !isFinite(l.Price) || l.Price <= 0 {
			return fmt.Errorf("invalid position: leg %d warrant price must be > 0, got %g", i, l.Price)
		}
		if !isFinite(l.KEquivalent) || l.KEquivalent <= 0 {
			return fmt.Errorf("invalid position: leg %d warrant K_equivalent must be > 0, got %g", i, l.KEquivalent)
		}
	}
	return nil
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// validateRawRulebook enforces structural invariants on a freshly-parsed
// rulebook before it's compiled into CEL programs. All errors start with
// "invalid rulebook" so callers can string-distinguish from CEL compile
// failures further down in LoadRulebook. The checks here are the ones that
// would otherwise manifest as: duplicate map keys silently overwriting compiled
// programs (rule ID collisions); legs_pattern typos silently falling into
// fixed-slot behavior; or ambiguous slot signatures where bindSlots returns
// a non-deterministic binding (see the matcher's "uniquely-attributed"
// invariant in match.go).
func validateRawRulebook(raw rawRulebook) error {
	if raw.SchemaVersion == "" {
		return fmt.Errorf("invalid rulebook: schema_version is required")
	}
	seenID := map[string]struct{}{}
	for i, r := range raw.Rules {
		if r.ID == "" {
			return fmt.Errorf("invalid rulebook: rule[%d] id is empty", i)
		}
		if _, dup := seenID[r.ID]; dup {
			return fmt.Errorf("invalid rulebook: duplicate rule id %q", r.ID)
		}
		seenID[r.ID] = struct{}{}
		if err := validateRule(r); err != nil {
			return err
		}
	}
	return nil
}

func validateRule(r Rule) error {
	if r.Match.LegsPattern != "" && len(r.Match.Legs) > 0 {
		return fmt.Errorf("invalid rulebook: rule %q sets both match.legs and legs_pattern (pick one)", r.ID)
	}
	switch r.Match.LegsPattern {
	case "", "all_options":
	default:
		return fmt.Errorf("invalid rulebook: rule %q legs_pattern %q is not recognized (allowed: %q, %q)",
			r.ID, r.Match.LegsPattern, "", "all_options")
	}
	if r.Match.LegsPattern == "" {
		if err := validateSlots(r); err != nil {
			return err
		}
	}
	if err := validateRequires(r); err != nil {
		return err
	}
	if !hasAnyOutput(r) {
		return fmt.Errorf("invalid rulebook: rule %q has no formula, permitted, or deposit_kind in either cash or margin block", r.ID)
	}
	return nil
}

// validateRequires structurally checks rule.Requires against the rule's slot
// declarations and the closed field whitelist. CEL compilation of min_fields
// gte expressions happens in LoadRulebook after the env exists; this function
// only checks shape so it can run before env build.
//
// Note on gte return-type checking: we assert numeric output via the existing
// kindFormula compile path. A gte that parses but returns a non-numeric type
// is rejected here. Runtime-type assertions on the *bound value* of legs.x.f
// are out of scope for this issue (defer to the interpreter in #34).
func validateRequires(r Rule) error {
	spec := r.Requires
	isAll := r.Match.LegsPattern == "all_options"

	slotNames := map[string]struct{}{}
	for _, s := range r.Match.Legs {
		slotNames[s.Name] = struct{}{}
	}

	checkSlot := func(where, slot string) error {
		if isAll {
			return fmt.Errorf("invalid rulebook: rule %q requires.%s references slot %q but legs_pattern is all_options (use all_slots)", r.ID, where, slot)
		}
		if _, ok := slotNames[slot]; !ok {
			return fmt.Errorf("invalid rulebook: rule %q requires.%s references unknown slot %q", r.ID, where, slot)
		}
		return nil
	}

	checkField := func(where, field, expected string) error {
		_, isStr := requireStringFields[field]
		_, isNum := requireNumericFields[field]
		switch expected {
		case "string":
			if !isStr {
				if isNum {
					return fmt.Errorf("invalid rulebook: rule %q requires.%s field %q is numeric, expected a string field", r.ID, where, field)
				}
				return fmt.Errorf("invalid rulebook: rule %q requires.%s field %q is not a known string field", r.ID, where, field)
			}
		case "numeric":
			if !isNum {
				if isStr {
					return fmt.Errorf("invalid rulebook: rule %q requires.%s field %q is a string field, expected a numeric field", r.ID, where, field)
				}
				return fmt.Errorf("invalid rulebook: rule %q requires.%s field %q is not a known numeric field", r.ID, where, field)
			}
		case "any":
			if !isStr && !isNum {
				return fmt.Errorf("invalid rulebook: rule %q requires.%s field %q is not a known leg field", r.ID, where, field)
			}
		}
		return nil
	}

	if spec.AllSlots != nil && !isAll {
		return fmt.Errorf("invalid rulebook: rule %q has requires.all_slots but legs_pattern is not all_options", r.ID)
	}

	for slot, fields := range spec.RequiredFields {
		if err := checkSlot("required_fields", slot); err != nil {
			return err
		}
		for _, f := range fields {
			if err := checkField("required_fields", f, "any"); err != nil {
				return err
			}
		}
	}

	for slot, fields := range spec.PositiveFields {
		if err := checkSlot("positive_fields", slot); err != nil {
			return err
		}
		for _, f := range fields {
			if err := checkField("positive_fields", f, "numeric"); err != nil {
				return err
			}
		}
	}

	for _, slot := range spec.ExpirationSlots {
		if err := checkSlot("expiration_slots", slot); err != nil {
			return err
		}
	}

	for _, sa := range spec.SameAcrossSlots {
		if sa.Field == "" {
			return fmt.Errorf("invalid rulebook: rule %q same_across_slots entry has empty field", r.ID)
		}
		if err := checkField("same_across_slots", sa.Field, "string"); err != nil {
			return err
		}
		if len(sa.Slots) < 2 {
			return fmt.Errorf("invalid rulebook: rule %q same_across_slots field %q requires at least two slots, got %d", r.ID, sa.Field, len(sa.Slots))
		}
		for _, s := range sa.Slots {
			if err := checkSlot("same_across_slots", s); err != nil {
				return err
			}
		}
	}

	for i, group := range spec.SameContractSize {
		if len(group) < 2 {
			return fmt.Errorf("invalid rulebook: rule %q same_contract_size group %d requires at least two slots, got %d", r.ID, i, len(group))
		}
		for _, s := range group {
			if err := checkSlot("same_contract_size", s); err != nil {
				return err
			}
		}
	}

	for _, mf := range spec.MinFields {
		if err := checkSlot("min_fields", mf.Slot); err != nil {
			return err
		}
		if err := checkField("min_fields", mf.Field, "numeric"); err != nil {
			return err
		}
		if mf.GTE == "" {
			return fmt.Errorf("invalid rulebook: rule %q min_fields entry for legs.%s.%s has empty gte", r.ID, mf.Slot, mf.Field)
		}
	}

	if spec.AllSlots != nil {
		for _, f := range spec.AllSlots.RequiredFields {
			if err := checkField("all_slots.required_fields", f, "any"); err != nil {
				return err
			}
		}
		if spec.AllSlots.SameField != "" {
			if err := checkField("all_slots.same_field", spec.AllSlots.SameField, "string"); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateSlots(r Rule) error {
	seenName := map[string]struct{}{}
	type slotSig struct{ side, kind, optionType, venue string }
	seenSig := map[slotSig]string{}
	for i, s := range r.Match.Legs {
		if s.Name == "" {
			return fmt.Errorf("invalid rulebook: rule %q slot[%d] name is empty", r.ID, i)
		}
		if _, dup := seenName[s.Name]; dup {
			return fmt.Errorf("invalid rulebook: rule %q has duplicate slot name %q", r.ID, s.Name)
		}
		seenName[s.Name] = struct{}{}
		// Signature collision: two slots with identical (side,kind,option_type,
		// venue) tuples make bindSlots produce a non-deterministic assignment
		// — constraints would only see one of them and silently accept a wrong
		// binding. Until constraint-aware matching exists, reject at load.
		sig := slotSig{s.Side, s.Kind, s.OptionType, s.Venue}
		if other, dup := seenSig[sig]; dup {
			return fmt.Errorf("invalid rulebook: rule %q slots %q and %q share attribute signature (side=%q,kind=%q,option_type=%q,venue=%q)",
				r.ID, other, s.Name, s.Side, s.Kind, s.OptionType, s.Venue)
		}
		seenSig[sig] = s.Name
	}
	return nil
}

func hasAnyOutput(r Rule) bool {
	for _, b := range []FormulaBlock{r.Formulas.Cash, r.Formulas.Margin} {
		if b.Initial != "" || b.Maintenance != "" ||
			b.InitialProceeds != "" || b.MaintenanceProceeds != "" ||
			b.Permitted != nil || b.DepositKind != "" {
			return true
		}
	}
	return false
}

func requirePositive(ruleID, slot, field string, value float64) error {
	if !isFinite(value) || value <= 0 {
		return fmt.Errorf("invalid position: rule %s requires legs.%s.%s > 0, got %g", ruleID, slot, field, value)
	}
	return nil
}

func requireSameContractSize(ruleID string, bound map[string]Leg, slots ...string) error {
	var want float64
	for i, slot := range slots {
		size := bound[slot].Qty * bound[slot].Mult
		if !isFinite(size) || size <= 0 {
			return fmt.Errorf("invalid position: rule %s requires legs.%s.qty * legs.%s.mult > 0, got %g", ruleID, slot, slot, size)
		}
		if i == 0 {
			want = size
			continue
		}
		if size != want {
			return fmt.Errorf("invalid position: rule %s requires matching qty*mult across slots %v, got %g and %g", ruleID, slots, want, size)
		}
	}
	return nil
}

func requireExpirationSlots(ruleID string, bound map[string]Leg, slots ...string) error {
	var want string
	for i, slot := range slots {
		exp := bound[slot].Expiration
		if exp == "" {
			return fmt.Errorf("invalid position: rule %s requires legs.%s.expiration", ruleID, slot)
		}
		if _, err := time.Parse("2006-01-02", exp); err != nil {
			return fmt.Errorf("invalid position: rule %s requires legs.%s.expiration as YYYY-MM-DD, got %q", ruleID, slot, exp)
		}
		if i == 0 {
			want = exp
			continue
		}
		if exp != want {
			return fmt.Errorf("invalid position: slot %q has expiration %q, expected %q", slot, exp, want)
		}
	}
	return nil
}

func requireSameStringField(ruleID string, bound map[string]Leg, field string, slots ...string) error {
	var want string
	for i, slot := range slots {
		value, err := legStringField(bound[slot], field)
		if err != nil {
			return err
		}
		if value == "" {
			return fmt.Errorf("invalid position: rule %s requires legs.%s.%s", ruleID, slot, field)
		}
		if i == 0 {
			want = value
			continue
		}
		if value != want {
			return fmt.Errorf("invalid position: rule %s requires matching %s across slots %v, got %q and %q", ruleID, field, slots, want, value)
		}
	}
	return nil
}

func legStringField(l Leg, field string) (string, error) {
	switch field {
	case "underlying":
		return l.Underlying, nil
	case "style":
		return l.Style, nil
	case "venue":
		return l.Venue, nil
	case "settle_style":
		return l.SettleStyle, nil
	case "tracks_index":
		return l.TracksIndex, nil
	default:
		return "", fmt.Errorf("invalid position: unknown string field %q", field)
	}
}

// legNumberField mirrors legStringField for the numeric whitelist shared with
// the requires schema validator. Unknown fields surface as "invalid position:"
// — load-time validation should have rejected the rulebook before we get here,
// so reaching this branch means a code/whitelist drift.
func legNumberField(l Leg, field string) (float64, error) {
	switch field {
	case "qty":
		return l.Qty, nil
	case "mult":
		return l.Mult, nil
	case "K":
		return l.K, nil
	case "price":
		return l.Price, nil
	case "time_to_expiration_months":
		return l.TimeToExpirationMonths, nil
	case "shares":
		return l.Shares, nil
	case "short_sale_proceeds":
		return l.ShortSaleProceeds, nil
	case "sale_price":
		return l.SalePrice, nil
	case "K_equivalent":
		return l.KEquivalent, nil
	default:
		return 0, fmt.Errorf("invalid position: unknown numeric field %q", field)
	}
}

// fieldIsPresent reports whether `field` on l has a usable value. "Present"
// matches requirePositive's zero-as-missing convention so the requires
// interpreter and the legacy Go validators agree on what blank means: a
// numeric field is present iff finite and non-zero; a string field is present
// iff non-empty. Unknown fields error.
func fieldIsPresent(l Leg, field string) (bool, error) {
	if _, ok := requireStringFields[field]; ok {
		v, err := legStringField(l, field)
		if err != nil {
			return false, err
		}
		return v != "", nil
	}
	if _, ok := requireNumericFields[field]; ok {
		v, err := legNumberField(l, field)
		if err != nil {
			return false, err
		}
		return isFinite(v) && v != 0, nil
	}
	return false, fmt.Errorf("invalid position: unknown leg field %q", field)
}

// validateRequirements is the runtime interpreter for RequireSpec — the sole
// rule-shape validator after the YAML-`requires` migration retired the
// rule-ID-keyed Go switch. Errors use the "invalid position:" prefix so
// callers cannot distinguish requirement failures from universal position
// validation by error shape. Read-only on bound and activation — Rulebook is
// concurrent-safe and the interpreter must not mutate either.
func (rb *Rulebook) validateRequirements(ruleID string, spec RequireSpec, bound map[string]Leg, activation map[string]any) error {
	checkSlotBound := func(slot string) error {
		if _, ok := bound[slot]; !ok {
			return fmt.Errorf("invalid rulebook: rule %s requires slot %q not present in bound legs", ruleID, slot)
		}
		return nil
	}

	for _, slot := range sortedStringKeys(spec.RequiredFields) {
		if err := checkSlotBound(slot); err != nil {
			return err
		}
		leg := bound[slot]
		for _, field := range spec.RequiredFields[slot] {
			present, err := fieldIsPresent(leg, field)
			if err != nil {
				return wrapRequires(err)
			}
			if !present {
				return requiresErrorf("invalid position: rule %s requires legs.%s.%s", ruleID, slot, field)
			}
		}
	}

	for _, slot := range sortedStringKeys(spec.PositiveFields) {
		if err := checkSlotBound(slot); err != nil {
			return err
		}
		leg := bound[slot]
		for _, field := range spec.PositiveFields[slot] {
			v, err := legNumberField(leg, field)
			if err != nil {
				return wrapRequires(err)
			}
			if err := requirePositive(ruleID, slot, field, v); err != nil {
				return wrapRequires(err)
			}
		}
	}

	if len(spec.ExpirationSlots) > 0 {
		for _, slot := range spec.ExpirationSlots {
			if err := checkSlotBound(slot); err != nil {
				return err
			}
		}
		if err := requireExpirationSlots(ruleID, bound, spec.ExpirationSlots...); err != nil {
			return wrapRequires(err)
		}
	}

	for _, sa := range spec.SameAcrossSlots {
		for _, slot := range sa.Slots {
			if err := checkSlotBound(slot); err != nil {
				return err
			}
		}
		if err := requireSameStringField(ruleID, bound, sa.Field, sa.Slots...); err != nil {
			return wrapRequires(err)
		}
	}

	for _, group := range spec.SameContractSize {
		for _, slot := range group {
			if err := checkSlotBound(slot); err != nil {
				return err
			}
		}
		if err := requireSameContractSize(ruleID, bound, group...); err != nil {
			return wrapRequires(err)
		}
	}

	for _, mf := range spec.MinFields {
		if err := checkSlotBound(mf.Slot); err != nil {
			return err
		}
		got, err := legNumberField(bound[mf.Slot], mf.Field)
		if err != nil {
			return wrapRequires(err)
		}
		// LoadRulebook pre-compiles every min_fields gte expression into
		// rb.progs; lookupProg is read-only so Evaluate stays concurrent-safe.
		key := ruleID + "/req:gte:" + mf.Slot + "." + mf.Field + ":" + mf.GTE
		prog := rb.lookupProg(key)
		out, _, eerr := prog.Eval(activation)
		if eerr != nil {
			return fmt.Errorf("invalid position: rule %s min_fields legs.%s.%s gte %q: %w", ruleID, mf.Slot, mf.Field, mf.GTE, eerr)
		}
		need, nerr := celNumber(out)
		if nerr != nil {
			return fmt.Errorf("invalid position: rule %s min_fields legs.%s.%s gte %q: %w", ruleID, mf.Slot, mf.Field, mf.GTE, nerr)
		}
		if !isFinite(got) || got < need {
			return requiresErrorf("invalid position: rule %s requires legs.%s.%s >= %s (got %g, need %g)", ruleID, mf.Slot, mf.Field, mf.GTE, got, need)
		}
	}

	if spec.AllSlots != nil {
		slots := sortedSlotKeys(bound)
		for _, slot := range slots {
			leg := bound[slot]
			for _, field := range spec.AllSlots.RequiredFields {
				present, err := fieldIsPresent(leg, field)
				if err != nil {
					return wrapRequires(err)
				}
				if !present {
					return requiresErrorf("invalid position: rule %s requires legs.%s.%s", ruleID, slot, field)
				}
			}
		}
		if spec.AllSlots.SameField != "" {
			field := spec.AllSlots.SameField
			var want string
			for _, slot := range slots {
				v, err := legStringField(bound[slot], field)
				if err != nil {
					return wrapRequires(err)
				}
				if v == "" {
					return requiresErrorf("invalid position: rule %s requires legs.%s.%s", ruleID, slot, field)
				}
				if want == "" {
					want = v
					continue
				}
				if v != want {
					return requiresErrorf("invalid position: rule %s requires one %s for mpl(legs), got %q and %q", ruleID, field, want, v)
				}
			}
		}
	}

	return nil
}

func sortedStringKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedSlotKeys(m map[string]Leg) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// buildActivation assembles the CEL activation map shared by constraint,
// requires, and formula evaluation. Centralized so the three factored
// helpers stay in lockstep on which top-level names are exposed.
func (rb *Rulebook) buildActivation(pos Position, bound map[string]Leg) map[string]any {
	return map[string]any{
		"U":         pos.U,
		"class":     pos.Class,
		"lev":       pos.Lev,
		"legs":      bound,
		"constants": rb.constants,
	}
}

// tryMatchAndConstraints runs slot binding and the constraint CEL pass.
// Returns:
//   - (bound, true, nil)  binding succeeded and every constraint held.
//   - (nil,   false, nil) bindSlots refused, or a constraint evaluated
//     cleanly to false — the rule does not apply.
//   - (nil,   false, err) a CEL evaluation error occurred (rule bug).
func (rb *Rulebook) tryMatchAndConstraints(pos Position, rule Rule) (map[string]Leg, bool, error) {
	bound, ok := rb.tryMatch(pos, rule)
	if !ok {
		return nil, false, nil
	}
	activation := rb.buildActivation(pos, bound)
	for _, c := range rule.Match.Constraints {
		prog := rb.lookupProg(rule.ID + "/c:" + c)
		out, _, err := prog.Eval(activation)
		if err != nil {
			return nil, false, fmt.Errorf("eval constraint %s %q: %w", rule.ID, c, err)
		}
		if b, ok := out.Value().(bool); !ok || !b {
			return nil, false, nil
		}
	}
	return bound, true, nil
}

// checkRequires walks rule.Requires against the bound legs. Returns nil on
// pass, a *RequiresError for any guard failure (callers may demote with
// errors.As), and a generic error only for CEL/lookup failures inside a
// requires primitive (currently the min_fields gte expression).
func (rb *Rulebook) checkRequires(rule Rule, bound map[string]Leg, activation map[string]any) error {
	return rb.validateRequirements(rule.ID, rule.Requires, bound, activation)
}

// evalFormulas runs the Requirement, AppliedProceeds, and CashCall formulas
// for the (accountType, phase) cell. Errors propagate as plain errors.
func (rb *Rulebook) evalFormulas(pos Position, rule Rule, bound map[string]Leg, accountType AccountType, phase Phase) (Result, error) {
	activation := rb.buildActivation(pos, bound)

	var block FormulaBlock
	var keyPrefix string
	switch accountType {
	case CashAccount:
		block = rule.Formulas.Cash
		keyPrefix = "cash"
	case MarginAccount:
		block = rule.Formulas.Margin
		keyPrefix = "margin"
	default:
		return Result{}, fmt.Errorf("unknown account type %q", string(accountType))
	}
	switch phase {
	case Initial, Maintenance:
	default:
		return Result{}, fmt.Errorf("unknown phase %q", string(phase))
	}
	formulaKey := keyPrefix + "." + string(phase)

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
	// deposit_kind-only (no numeric formula): return the kind. Otherwise we
	// compute the number AND attach the kind — both are meaningful.
	if expr == "" {
		if block.DepositKind != "" {
			return Result{RuleID: rule.ID, FormulaKey: formulaKey, AccountType: string(accountType), Phase: string(phase), Permitted: true, DepositKind: block.DepositKind}, nil
		}
		return Result{}, fmt.Errorf("rule %s has no %s formula", rule.ID, formulaKey)
	}

	prog := rb.lookupProg(rule.ID + "/" + formulaKey)
	out, _, err := prog.Eval(activation)
	if err != nil {
		return Result{}, fmt.Errorf("eval %s: %w", rule.ID, err)
	}
	req, err := celNumber(out)
	if err != nil {
		return Result{}, fmt.Errorf("eval %s %s: %w", rule.ID, formulaKey, err)
	}

	var proceeds float64
	if proceedsExpr != "" {
		pprog := rb.lookupProg(rule.ID + "/" + proceedsKey)
		pout, _, err := pprog.Eval(activation)
		if err != nil {
			return Result{}, fmt.Errorf("eval %s %s: %w", rule.ID, proceedsKey, err)
		}
		proceeds, err = celNumber(pout)
		if err != nil {
			return Result{}, fmt.Errorf("eval %s %s: %w", rule.ID, proceedsKey, err)
		}
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

// evaluateOne applies a single rule to an already-prepared Position. Thin
// composer over tryMatchAndConstraints, checkRequires, and evalFormulas.
// Returns:
//   - (res, true, nil)  the rule matched, constraints held, and a result was
//     computed (numeric, permitted=false, or deposit-kind-only).
//   - (_,  false, nil)  the rule does not apply: either bindSlots refused, or
//     a constraint evaluated cleanly to false.
//   - (_,  false, err)  CEL compile/eval failure, requires-guard failure
//     (typed *RequiresError), or rulebook configuration error.
func (rb *Rulebook) evaluateOne(pos Position, rule Rule, accountType AccountType, phase Phase) (Result, bool, error) {
	bound, ok, err := rb.tryMatchAndConstraints(pos, rule)
	if err != nil || !ok {
		return Result{}, ok, err
	}
	activation := rb.buildActivation(pos, bound)
	if err := rb.checkRequires(rule, bound, activation); err != nil {
		return Result{}, false, err
	}
	res, err := rb.evalFormulas(pos, rule, bound, accountType, phase)
	if err != nil {
		return Result{}, false, err
	}
	return res, true, nil
}

package engine

import (
	"fmt"
	"os"
	"sort"
	"time"

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
	SchemaVersion string                        `yaml:"schema_version"`
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
	if err := yaml.Unmarshal(data, &raw); err != nil {
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

func (rb *Rulebook) compile(key, expr string, kind exprKind) (cel.Program, error) {
	if prog, ok := rb.progs[key]; ok {
		return prog, nil
	}
	ast, iss := rb.env.Compile(expr)
	if iss.Err() != nil {
		return nil, iss.Err()
	}
	if kind == kindFormula {
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
	if pos.U <= 0 {
		return fmt.Errorf("invalid position: U must be > 0, got %g", pos.U)
	}
	if pos.Class == "" {
		return fmt.Errorf("invalid position: class is required")
	}
	if pos.Lev <= 0 {
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
	if l.Mult <= 0 {
		return fmt.Errorf("invalid position: leg %d mult must be > 0, got %g", i, l.Mult)
	}
	if l.Kind == OptionKind {
		if l.OptionType != "put" && l.OptionType != "call" {
			return fmt.Errorf("invalid position: leg %d option_type must be 'put' or 'call', got %q", i, l.OptionType)
		}
		if l.Qty <= 0 {
			return fmt.Errorf("invalid position: leg %d qty must be > 0, got %g", i, l.Qty)
		}
		if l.K <= 0 {
			return fmt.Errorf("invalid position: leg %d K must be > 0, got %g", i, l.K)
		}
		if l.P < 0 {
			return fmt.Errorf("invalid position: leg %d P must be >= 0, got %g", i, l.P)
		}
		if l.P0 < 0 {
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
	if l.Shares <= 0 {
		return fmt.Errorf("invalid position: leg %d shares must be > 0, got %g", i, l.Shares)
	}
	switch l.Kind {
	case ConvertibleKind:
		if l.Price <= 0 {
			return fmt.Errorf("invalid position: leg %d convertible price must be > 0, got %g", i, l.Price)
		}
	case WarrantKind:
		if l.Price <= 0 {
			return fmt.Errorf("invalid position: leg %d warrant price must be > 0, got %g", i, l.Price)
		}
		if l.KEquivalent <= 0 {
			return fmt.Errorf("invalid position: leg %d warrant K_equivalent must be > 0, got %g", i, l.KEquivalent)
		}
	}
	return nil
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
	if !hasAnyOutput(r) {
		return fmt.Errorf("invalid rulebook: rule %q has no formula, permitted, or deposit_kind in either cash or margin block", r.ID)
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

// validateRuleInputs checks fields that only become required once a specific
// rule shape has bound. Universal validation cannot require these globally:
// e.g. a naked short option doesn't need expiration, but a vertical spread
// does because the rule compares expirations and would otherwise accept
// "" <= "". These errors are validation failures, not no-match outcomes.
//
// Design choice: the rule-shape helpers used here (requireSameStringField,
// requireExpirationSlots, requireSingleUnderlying) remain in Go rather than
// migrating to CEL match.constraints. The CEL-typing epic audit walked all
// checks reachable from this function and classified them as still needed:
// none are made redundant by a typed Leg. Two structural reasons keep them
// Go-side:
//
//  1. Blank-string equality. requireSameStringField rejects two legs whose
//     underlying / expiration / venue fields are both "". In CEL,
//     legs.a.underlying == legs.b.underlying is true for two blank strings, so
//     a constraint phrased that way silently passes on under-specified input.
//     The Go check treats blank as a distinct "missing" state, which CEL's
//     value-equality model cannot express without a parallel "is-set" channel.
//
//  2. Cross-leg uniqueness on all_options patterns. requireSingleUnderlying
//     runs against generic_limited_risk_combo, whose legs_pattern: all_options
//     binds an arbitrary number of legs. CEL constraints address slots by name
//     (legs.a, legs.b, ...), so a constraint cannot iterate the bound set to
//     assert "every leg shares one underlying".
//
// requireExpirationSlots stays Go-side for a related but narrower reason: it
// validates that expiration fields are present and parseable dates before CEL
// constraints compare them as strings. CEL can compare strings, but it does
// not know that "" or "not-a-date" are invalid expirations.
//
// The four "new failing" cases surfaced by the audit (vertical blank
// underlying / blank expiration / blank venue, generic mixed underlyings) are
// already covered by TestRuleInputValidation_* — see the epic's Out-of-scope
// note for the rejected migration to CEL constraints.
func validateRuleInputs(ruleID string, bound map[string]Leg) error {
	switch ruleID {
	case "long_option_short_dated", "long_option_long_dated_listed", "long_option_long_dated_otc":
		return requirePositive(ruleID, "opt", "time_to_expiration_months", bound["opt"].TimeToExpirationMonths)
	case "short_strangle_or_straddle":
		return requireSameUnderlying(ruleID, bound, "sp", "sc")
	case "vertical_spread":
		if err := requireSameUnderlying(ruleID, bound, "long_leg", "short_leg"); err != nil {
			return err
		}
		if err := requireSameStringField(ruleID, bound, "style", "long_leg", "short_leg"); err != nil {
			return err
		}
		if err := requireSameStringField(ruleID, bound, "venue", "long_leg", "short_leg"); err != nil {
			return err
		}
		return requireExpirationSlots(ruleID, bound, "long_leg", "short_leg")
	case "long_box_spread":
		if err := requireExpirationSlots(ruleID, bound, "bc", "bp", "sp", "sc"); err != nil {
			return err
		}
		return requireSameStringField(ruleID, bound, "style", "bc", "bp", "sp", "sc")
	case "short_box_spread":
		return requireExpirationSlots(ruleID, bound, "bc", "sc")
	case "short_put_short_stock":
		return requirePositive(ruleID, "ss", "short_sale_proceeds", bound["ss"].ShortSaleProceeds)
	case "short_index_call_long_etf":
		if err := requireNonEmpty(ruleID, "sc", "underlying", bound["sc"].Underlying); err != nil {
			return err
		}
		if err := requireNonEmpty(ruleID, "le", "tracks_index", bound["le"].TracksIndex); err != nil {
			return err
		}
		if err := requirePositive(ruleID, "le", "price", bound["le"].Price); err != nil {
			return err
		}
		return requirePositive(ruleID, "sc", "K_equivalent", bound["sc"].KEquivalent)
	case "protective_put":
		return requireNonEmpty(ruleID, "lp", "style", bound["lp"].Style)
	case "long_call_short_stock":
		if err := requireNonEmpty(ruleID, "lc", "style", bound["lc"].Style); err != nil {
			return err
		}
		return requirePositive(ruleID, "ss", "short_sale_proceeds", bound["ss"].ShortSaleProceeds)
	case "conversion":
		if err := requireSameUnderlying(ruleID, bound, "lp", "sc"); err != nil {
			return err
		}
		if err := requireExpirationSlots(ruleID, bound, "lp", "sc"); err != nil {
			return err
		}
		return requireSameStringField(ruleID, bound, "style", "lp", "sc")
	case "reverse_conversion":
		if err := requireSameUnderlying(ruleID, bound, "lc", "sp"); err != nil {
			return err
		}
		if err := requireExpirationSlots(ruleID, bound, "lc", "sp"); err != nil {
			return err
		}
		if err := requireSameStringField(ruleID, bound, "style", "lc", "sp"); err != nil {
			return err
		}
		if err := requirePositive(ruleID, "ss", "short_sale_proceeds", bound["ss"].ShortSaleProceeds); err != nil {
			return err
		}
		return requirePositive(ruleID, "ss", "sale_price", bound["ss"].SalePrice)
	case "collar":
		if err := requireSameUnderlying(ruleID, bound, "lp", "sc"); err != nil {
			return err
		}
		if err := requireExpirationSlots(ruleID, bound, "lp", "sc"); err != nil {
			return err
		}
		return requireSameStringField(ruleID, bound, "style", "lp", "sc")
	case "generic_limited_risk_combo":
		return requireSingleUnderlying(ruleID, bound)
	default:
		return nil
	}
}

func requireNonEmpty(ruleID, slot, field, value string) error {
	if value == "" {
		return fmt.Errorf("invalid position: rule %s requires legs.%s.%s", ruleID, slot, field)
	}
	return nil
}

func requirePositive(ruleID, slot, field string, value float64) error {
	if value <= 0 {
		return fmt.Errorf("invalid position: rule %s requires legs.%s.%s > 0, got %g", ruleID, slot, field, value)
	}
	return nil
}

func requireExpirationSlots(ruleID string, bound map[string]Leg, slots ...string) error {
	for _, slot := range slots {
		exp := bound[slot].Expiration
		if exp == "" {
			return fmt.Errorf("invalid position: rule %s requires legs.%s.expiration", ruleID, slot)
		}
		if _, err := time.Parse("2006-01-02", exp); err != nil {
			return fmt.Errorf("invalid position: rule %s requires legs.%s.expiration as YYYY-MM-DD, got %q", ruleID, slot, exp)
		}
	}
	return nil
}

func requireSameUnderlying(ruleID string, bound map[string]Leg, slots ...string) error {
	return requireSameStringField(ruleID, bound, "underlying", slots...)
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

func requireSingleUnderlying(ruleID string, bound map[string]Leg) error {
	var want string
	for slot, leg := range bound {
		if leg.Underlying == "" {
			return fmt.Errorf("invalid position: rule %s requires legs.%s.underlying", ruleID, slot)
		}
		if want == "" {
			want = leg.Underlying
			continue
		}
		if leg.Underlying != want {
			return fmt.Errorf("invalid position: rule %s requires one underlying for mpl(legs), got %q and %q", ruleID, want, leg.Underlying)
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
	default:
		return "", fmt.Errorf("invalid position: unknown string field %q", field)
	}
}

// evaluateOne applies a single rule to an already-prepared Position.
// Returns:
//   - (res, true, nil)  the rule matched, constraints held, and a result was
//     computed (numeric, permitted=false, or deposit-kind-only).
//   - (_,  false, nil)  the rule does not apply: either bindSlots refused, or
//     a constraint evaluated cleanly to false.
//   - (_,  false, err)  CEL compile/eval failure or rulebook configuration
//     error (no formula AND no deposit_kind for the requested key).
func (rb *Rulebook) evaluateOne(pos Position, rule Rule, accountType AccountType, phase Phase) (Result, bool, error) {
	bound, ok := rb.tryMatch(pos, rule)
	if !ok {
		return Result{}, false, nil
	}
	legsMap := map[string]any{}
	for name, leg := range bound {
		// Pass the Leg struct directly so the NativeTypes adapter wraps it
		// via reflection; CEL field access uses reflect.Value.FieldByName and
		// would panic on a map[string]any (see env.go legObjectTypeName).
		legsMap[name] = leg
	}
	activation := map[string]any{
		"U":         pos.U,
		"class":     pos.Class,
		"lev":       pos.Lev,
		"legs":      legsMap,
		"constants": rb.constants,
	}
	// Constraints. A clean `false` demotes to "doesn't match"; a CEL eval
	// error is surfaced (likely a rule bug).
	for _, c := range rule.Match.Constraints {
		prog, err := rb.compile(rule.ID+"/c:"+c, c, kindConstraint)
		if err != nil {
			return Result{}, false, fmt.Errorf("compile constraint %s %q: %w", rule.ID, c, err)
		}
		out, _, err := prog.Eval(activation)
		if err != nil {
			return Result{}, false, fmt.Errorf("eval constraint %s %q: %w", rule.ID, c, err)
		}
		if b, ok := out.Value().(bool); !ok || !b {
			return Result{}, false, nil
		}
	}
	if err := validateRuleInputs(rule.ID, bound); err != nil {
		return Result{}, false, err
	}

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
		return Result{}, false, fmt.Errorf("unknown account type %q", string(accountType))
	}
	formulaKey := keyPrefix + "." + string(phase)

	if block.Permitted != nil && !*block.Permitted {
		return Result{RuleID: rule.ID, FormulaKey: formulaKey, AccountType: string(accountType), Phase: string(phase), Permitted: false}, true, nil
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
			return Result{RuleID: rule.ID, FormulaKey: formulaKey, AccountType: string(accountType), Phase: string(phase), Permitted: true, DepositKind: block.DepositKind}, true, nil
		}
		return Result{}, false, fmt.Errorf("rule %s has no %s formula", rule.ID, formulaKey)
	}

	prog, err := rb.compile(rule.ID+"/"+formulaKey, expr, kindFormula)
	if err != nil {
		return Result{}, false, fmt.Errorf("compile %s: %w", rule.ID, err)
	}
	out, _, err := prog.Eval(activation)
	if err != nil {
		return Result{}, false, fmt.Errorf("eval %s: %w", rule.ID, err)
	}
	req, err := celNumber(out)
	if err != nil {
		return Result{}, false, fmt.Errorf("eval %s %s: %w", rule.ID, formulaKey, err)
	}

	var proceeds float64
	if proceedsExpr != "" {
		pprog, err := rb.compile(rule.ID+"/"+proceedsKey, proceedsExpr, kindFormula)
		if err != nil {
			return Result{}, false, fmt.Errorf("compile %s %s: %w", rule.ID, proceedsKey, err)
		}
		pout, _, err := pprog.Eval(activation)
		if err != nil {
			return Result{}, false, fmt.Errorf("eval %s %s: %w", rule.ID, proceedsKey, err)
		}
		proceeds, err = celNumber(pout)
		if err != nil {
			return Result{}, false, fmt.Errorf("eval %s %s: %w", rule.ID, proceedsKey, err)
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
	}, true, nil
}

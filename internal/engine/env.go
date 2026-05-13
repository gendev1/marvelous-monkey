package engine

import (
	"fmt"
	"math"
	"reflect"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/ext"
)

// legObjectTypeName is the cel-go-derived ObjectType string for engine.Leg.
// cel-go composes it as `simplePkgAlias(reflect.Type.PkgPath()) + "." + Name()`
// (see ext/native.go: convertToCelType, simplePkgAlias), so for the module path
// `margincalc/internal/engine` the result is `engine.Leg`. If this package is
// renamed or moved (e.g. promoted to `pkg/margincalc/`), LoadRulebook will fail
// at startup because the variable declaration below won't match the registered
// type — update this constant and the moved-package's ObjectType in lockstep.
// See issue #5.
const legObjectTypeName = "engine.Leg"

// buildEnv constructs a CEL environment exposing the variables and custom
// functions referenced by the rules YAML. The rates table is captured so
// rate(class, field) can resolve. The `constants` variable is declared
// here but its value is bound per-evaluation via the activation map (see
// Rulebook.Evaluate), letting rule expressions reference named thresholds
// (e.g. `constants.long_option_loan_value_threshold_months`) instead of
// hard-coding magic numbers.
func buildEnv(rates map[string]map[string]float64) (*cel.Env, error) {
	// Register engine.Leg as a CEL native type so typoed field accesses
	// (e.g. legs.opt.kk) are rejected at compile time rather than silently
	// reading as a zero-valued dyn at runtime. Field names are taken from
	// the `json:` struct tags on Leg, which already match every formula in
	// rules/*.yaml. The activation in evaluateOne (rulebook.go) passes Leg
	// structs directly so cel-go's reflective field access lands on the
	// real struct, not a map[string]any (which would panic with
	// "FieldByName on map Value"). The bindings below unwrap to a typed
	// engine.Leg via unwrapLeg — see issue #6.
	legType := cel.ObjectType(legObjectTypeName)
	legsType := cel.MapType(cel.StringType, legType)

	return cel.NewEnv(
		ext.NativeTypes(reflect.TypeOf(Leg{}), ext.ParseStructTag("json")),
		// --- variables ---
		cel.Variable("U", cel.DoubleType),
		cel.Variable("class", cel.StringType),
		cel.Variable("lev", cel.DoubleType),
		cel.Variable("legs", legsType),
		cel.Variable("constants", cel.MapType(cel.StringType, cel.DynType)),

		// --- max / min (CEL stdlib has no max/min) ---
		cel.Function("max",
			cel.Overload("max_double_double",
				[]*cel.Type{cel.DoubleType, cel.DoubleType},
				cel.DoubleType,
				cel.BinaryBinding(func(a, b ref.Val) ref.Val {
					return types.Double(math.Max(asFloat(a), asFloat(b)))
				}))),
		cel.Function("min",
			cel.Overload("min_double_double",
				[]*cel.Type{cel.DoubleType, cel.DoubleType},
				cel.DoubleType,
				cel.BinaryBinding(func(a, b ref.Val) ref.Val {
					return types.Double(math.Min(asFloat(a), asFloat(b)))
				}))),

		// --- rate(class, field) → double ---
		cel.Function("rate",
			cel.Overload("rate_string_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.DoubleType,
				cel.BinaryBinding(func(a, b ref.Val) ref.Val {
					cls := asString(a)
					fld := asString(b)
					if t, ok := rates[cls]; ok {
						if v, ok := t[fld]; ok {
							return types.Double(v)
						}
					}
					return types.NewErr("unknown rate %s.%s", cls, fld)
				}))),

		// --- intrinsic_call(K, U), intrinsic_put(K, U) → double ---
		cel.Function("intrinsic_call",
			cel.Overload("intrinsic_call_double_double",
				[]*cel.Type{cel.DoubleType, cel.DoubleType},
				cel.DoubleType,
				cel.BinaryBinding(func(K, U ref.Val) ref.Val {
					return types.Double(math.Max(0, asFloat(U)-asFloat(K)))
				}))),
		cel.Function("intrinsic_put",
			cel.Overload("intrinsic_put_double_double",
				[]*cel.Type{cel.DoubleType, cel.DoubleType},
				cel.DoubleType,
				cel.BinaryBinding(func(K, U ref.Val) ref.Val {
					return types.Double(math.Max(0, asFloat(K)-asFloat(U)))
				}))),

		// --- short_call_req(leg, U, class, lev, p) → double ---
		// Returns the FULL USD requirement (qty * mult already applied).
		cel.Function("short_call_req",
			cel.Overload("short_call_req_overload",
				[]*cel.Type{legType, cel.DoubleType, cel.StringType, cel.DoubleType, cel.DoubleType},
				cel.DoubleType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					return shortOptionReq(rates, args /*isCall=*/, true)
				}))),
		cel.Function("short_put_req",
			cel.Overload("short_put_req_overload",
				[]*cel.Type{legType, cel.DoubleType, cel.StringType, cel.DoubleType, cel.DoubleType},
				cel.DoubleType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					return shortOptionReq(rates, args /*isCall=*/, false)
				}))),

		// --- mpl(legs) → double ---
		cel.Function("mpl",
			cel.Overload("mpl_legs",
				[]*cel.Type{legsType},
				cel.DoubleType,
				cel.UnaryBinding(func(v ref.Val) ref.Val {
					return maxPotentialLoss(v)
				}))),

		// --- is_limited_risk(legs) → bool ---
		// True iff the option-only payoff is bounded below at both tails
		// (U → ∞ and U → 0). Stock/etf/etn legs make the position unbounded
		// for this predicate's purposes — those have their own dedicated rules.
		cel.Function("is_limited_risk",
			cel.Overload("is_limited_risk_legs",
				[]*cel.Type{legsType},
				cel.BoolType,
				cel.UnaryBinding(func(v ref.Val) ref.Val {
					return isLimitedRisk(v)
				}))),

		// --- sum_long_premiums(legs, field), sum_short_premiums(legs, field) ---
		cel.Function("sum_long_premiums",
			cel.Overload("sum_long_premiums_overload",
				[]*cel.Type{legsType, cel.StringType},
				cel.DoubleType,
				cel.BinaryBinding(func(legsV, fieldV ref.Val) ref.Val {
					return sumPremiums(legsV, asString(fieldV), Long)
				}))),
		cel.Function("sum_short_premiums",
			cel.Overload("sum_short_premiums_overload",
				[]*cel.Type{legsType, cel.StringType},
				cel.DoubleType,
				cel.BinaryBinding(func(legsV, fieldV ref.Val) ref.Val {
					return sumPremiums(legsV, asString(fieldV), Short)
				}))),
	)
}

// shortOptionReq implements the per-position USD requirement for an uncovered
// short option. args = [leg, U, class, lev, p]. Returns a CEL error if the
// class is unknown or if the leg cannot be unwrapped — silently zeroing here
// previously masked typos in `class` and would now mask a typed-leg miswire.
func shortOptionReq(rates map[string]map[string]float64, args []ref.Val, isCall bool) ref.Val {
	leg, errVal := unwrapLeg(args[0])
	if errVal != nil {
		return errVal
	}
	U := asFloat(args[1])
	cls := asString(args[2])
	lev := asFloat(args[3])
	p := asFloat(args[4])

	t, ok := rates[cls]
	if !ok {
		return types.NewErr("unknown rate class %q", cls)
	}
	base, ok := t["base_pct"]
	if !ok {
		return types.NewErr("rate class %q missing base_pct", cls)
	}
	minPct, ok := t["min_pct"]
	if !ok {
		return types.NewErr("rate class %q missing min_pct", cls)
	}

	K := leg.K
	qty := leg.Qty
	mult := leg.Mult

	var basic, minRule float64
	if isCall {
		basic = base*lev*U - math.Max(0, K-U)
		minRule = minPct * lev * U
	} else {
		basic = base*lev*U - math.Max(0, U-K)
		minRule = minPct * lev * K
	}
	return types.Double(qty * mult * (p + math.Max(basic, minRule)))
}

// forEachLeg invokes fn for every leg in the map, unwrapping each map value
// to a typed engine.Leg. If unwrap fails, iteration stops and the resulting
// *types.Err is returned; callers must surface it as the binding's result
// (mirrors rate() — no silent zero-fallback per CLAUDE.md "Required Rules").
// If fn returns false, iteration stops early with no error.
func forEachLeg(legsVal ref.Val, fn func(Leg) bool) ref.Val {
	legsMap, ok := legsVal.(traits.Mapper)
	if !ok {
		return types.NewErr("legs unwrap failed: expected map, got %T", legsVal)
	}
	it := legsMap.Iterator()
	for it.HasNext() == types.Bool(true) {
		k := it.Next()
		leg, errVal := unwrapLeg(legsMap.Get(k))
		if errVal != nil {
			return errVal
		}
		if !fn(leg) {
			return nil
		}
	}
	return nil
}

// maxPotentialLoss walks every option leg in the map, enumerates the sample
// points where the piecewise-linear payoff can hit its minimum — each distinct
// strike plus the downside tail U=0 — and returns the largest net loss as a
// non-negative USD amount. U=0 is required because for net-short-put
// structures the slope below the lowest strike is positive, so the worst loss
// occurs at the floor of the underlying's domain, not at any strike.
func maxPotentialLoss(legsVal ref.Val) ref.Val {
	var opts []Leg
	if err := forEachLeg(legsVal, func(l Leg) bool {
		if l.Kind == OptionKind {
			opts = append(opts, l)
		}
		return true
	}); err != nil {
		return err
	}
	if len(opts) == 0 {
		return types.Double(0)
	}
	sampleSet := map[float64]struct{}{0: {}}
	for _, o := range opts {
		sampleSet[o.K] = struct{}{}
	}
	minPnL := math.Inf(+1)
	for U := range sampleSet {
		if pnl := payoffAt(opts, U); pnl < minPnL {
			minPnL = pnl
		}
	}
	if minPnL >= 0 {
		return types.Double(0)
	}
	return types.Double(-minPnL)
}

// payoffAt is the signed P&L of an option-only position at underlying price U.
// Longs add, shorts subtract; intrinsic value is the usual max(0, ...).
func payoffAt(opts []Leg, U float64) float64 {
	pnl := 0.0
	for _, o := range opts {
		var intrinsic float64
		if o.OptionType == "call" {
			intrinsic = math.Max(0, U-o.K)
		} else {
			intrinsic = math.Max(0, o.K-U)
		}
		sign := 1.0
		if o.Side == Short {
			sign = -1.0
		}
		pnl += sign * o.Qty * o.Mult * intrinsic
	}
	return pnl
}

// isLimitedRisk returns true iff an option-only structure has bounded loss
// at the upside tail. Only call exposure can make the loss truly unbounded:
//   - U → ∞: each uncovered short call loses without bound; so net signed
//     call exposure must be ≥ 0. Exposure is summed as qty*mult so mixed
//     multipliers (e.g. 1 mini call mult=10 vs 1 standard call mult=100)
//     don't appear hedged when they aren't.
//   - U → 0: puts have a floor — every put's contribution is capped at K, so
//     even a net-short-put structure has finite worst-case loss. mpl() now
//     samples U=0 explicitly, so the actual number stays correct.
//   - any non-option leg (stock/etf/etn) — those belong to specific rules,
//     not this catch-all
func isLimitedRisk(legsVal ref.Val) ref.Val {
	netCallExposure := 0.0
	limited := true
	if err := forEachLeg(legsVal, func(l Leg) bool {
		if l.Kind != OptionKind {
			limited = false
			return false // short-circuit
		}
		if l.OptionType == "call" {
			sign := 1.0
			if l.Side == Short {
				sign = -1.0
			}
			netCallExposure += sign * l.Qty * l.Mult
		}
		return true
	}); err != nil {
		return err
	}
	return types.Bool(limited && netCallExposure >= 0)
}

// sumPremiums adds qty*mult*<field> for every option leg on `side`. The
// only premium fields the rulebook passes are `P` and `P0` (see
// rules/cboe_baseline.yaml); any other name is a CEL/YAML typo and surfaces
// as a hard error rather than a silent zero — same fail-loud contract as
// rate() per CLAUDE.md "Required Rules".
func sumPremiums(legsVal ref.Val, field string, side Side) ref.Val {
	var premium func(Leg) float64
	switch field {
	case "P":
		premium = func(l Leg) float64 { return l.P }
	case "P0":
		premium = func(l Leg) float64 { return l.P0 }
	default:
		return types.NewErr("sum_premiums: unknown premium field %q (want \"P\" or \"P0\")", field)
	}
	total := 0.0
	if err := forEachLeg(legsVal, func(l Leg) bool {
		if l.Kind != OptionKind || l.Side != side {
			return true
		}
		total += premium(l) * l.Qty * l.Mult
		return true
	}); err != nil {
		return err
	}
	return types.Double(total)
}

// --- helpers for unwrapping ref.Val ---

// unwrapLeg extracts a typed engine.Leg from the value cel-go hands a binding
// for a `map<string, engine.Leg>` entry. Under ext.NativeTypes the wrapped
// value is a *nativeObj whose Value() returns the underlying Go value — which
// can be either Leg or *Leg depending on how the activation supplied it. We
// accept both; anything else is a hard error rather than a zero-valued Leg.
// Mirrors rate() at env.go: no silent fallback per CLAUDE.md "Required Rules".
func unwrapLeg(v ref.Val) (Leg, ref.Val) {
	if v == nil {
		return Leg{}, types.NewErr("leg unwrap failed: got nil ref.Val")
	}
	switch x := v.Value().(type) {
	case Leg:
		return x, nil
	case *Leg:
		if x == nil {
			return Leg{}, types.NewErr("leg unwrap failed: got nil *Leg")
		}
		return *x, nil
	default:
		return Leg{}, types.NewErr("leg unwrap failed: got %T", v.Value())
	}
}

// asFloat is a forgiving extractor used on the *inside* of CEL helpers where
// the value's source is a known-good numeric arg (e.g. unwrapping the U arg
// of short_call_req). It returns 0 for anything non-numeric. DO NOT use it
// to unwrap a formula's overall return value — use celNumber for that so a
// formula like `initial: "true"` doesn't silently become 0.
func asFloat(v ref.Val) float64 {
	switch x := v.(type) {
	case types.Double:
		return float64(x)
	case types.Int:
		return float64(x)
	}
	if d, ok := v.Value().(float64); ok {
		return d
	}
	return 0
}

// celNumber is the strict counterpart to asFloat for unwrapping a CEL
// formula's top-level return value. A formula that returns bool/string/null
// is a rule bug, not a 0 — silently zeroing would be the worst kind of
// confidently-wrong margin number, so report it as an error instead. The
// rulebook validator already catches the rule shape; this is the eval-time
// safety net for dyn-typed expressions whose output type the compiler
// couldn't pin (e.g., a conditional with mixed-typed branches).
func celNumber(v ref.Val) (float64, error) {
	switch x := v.(type) {
	case types.Double:
		return float64(x), nil
	case types.Int:
		return float64(x), nil
	}
	return 0, fmt.Errorf("formula must return a number, got %s (value: %v)", v.Type().TypeName(), v.Value())
}

func asString(v ref.Val) string {
	if s, ok := v.(types.String); ok {
		return string(s)
	}
	if s, ok := v.Value().(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v.Value())
}

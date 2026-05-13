package engine

import (
	"fmt"
	"math"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// buildEnv constructs a CEL environment exposing the variables and custom
// functions referenced by the rules YAML. The rates table is captured so
// rate(class, field) can resolve. The `constants` variable is declared
// here but its value is bound per-evaluation via the activation map (see
// Rulebook.Evaluate), letting rule expressions reference named thresholds
// (e.g. `constants.long_option_loan_value_threshold_months`) instead of
// hard-coding magic numbers.
func buildEnv(rates map[string]map[string]float64) (*cel.Env, error) {
	legType := cel.MapType(cel.StringType, cel.DynType)
	legsType := cel.MapType(cel.StringType, legType)

	return cel.NewEnv(
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
					return shortOptionReq(rates, args, /*isCall=*/ true)
				}))),
		cel.Function("short_put_req",
			cel.Overload("short_put_req_overload",
				[]*cel.Type{legType, cel.DoubleType, cel.StringType, cel.DoubleType, cel.DoubleType},
				cel.DoubleType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					return shortOptionReq(rates, args, /*isCall=*/ false)
				}))),

		// --- mpl(legs) → double ---
		cel.Function("mpl",
			cel.Overload("mpl_legs",
				[]*cel.Type{legsType},
				cel.DoubleType,
				cel.UnaryBinding(func(v ref.Val) ref.Val {
					return types.Double(maxPotentialLoss(v))
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
					return types.Bool(isLimitedRisk(v))
				}))),

		// --- sum_long_premiums(legs, field), sum_short_premiums(legs, field) ---
		cel.Function("sum_long_premiums",
			cel.Overload("sum_long_premiums_overload",
				[]*cel.Type{legsType, cel.StringType},
				cel.DoubleType,
				cel.BinaryBinding(func(legsV, fieldV ref.Val) ref.Val {
					return types.Double(sumPremiums(legsV, asString(fieldV), Long))
				}))),
		cel.Function("sum_short_premiums",
			cel.Overload("sum_short_premiums_overload",
				[]*cel.Type{legsType, cel.StringType},
				cel.DoubleType,
				cel.BinaryBinding(func(legsV, fieldV ref.Val) ref.Val {
					return types.Double(sumPremiums(legsV, asString(fieldV), Short))
				}))),
	)
}

// shortOptionReq implements the per-position USD requirement for an uncovered
// short option. args = [leg, U, class, lev, p]. Returns a CEL error if the
// class is unknown — silently zeroing here previously masked typos in `class`.
func shortOptionReq(rates map[string]map[string]float64, args []ref.Val, isCall bool) ref.Val {
	leg := args[0].(traits.Mapper)
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

	K := mapFloat(leg, "K")
	qty := mapFloat(leg, "qty")
	mult := mapFloat(leg, "mult")

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

// legView is the projection of a CEL leg map that the iterating helpers care
// about. Centralized so the map-field reads happen in exactly one place.
type legView struct {
	kind       string
	side       Side
	optionType string
	K, qty, mu float64
}

// forEachLeg invokes fn for every leg in the map (option or otherwise). The
// raw mapper is passed alongside the projection so callers that need extra
// fields (e.g. a custom premium field name) don't have to re-implement
// iteration. If fn returns false, iteration stops early.
func forEachLeg(legsVal ref.Val, fn func(legView, traits.Mapper) bool) {
	legsMap := legsVal.(traits.Mapper)
	it := legsMap.Iterator()
	for it.HasNext() == types.Bool(true) {
		k := it.Next()
		legM, ok := legsMap.Get(k).(traits.Mapper)
		if !ok {
			continue
		}
		l := legView{
			kind:       mapString(legM, "kind"),
			side:       Side(mapString(legM, "side")),
			optionType: mapString(legM, "option_type"),
			K:          mapFloat(legM, "K"),
			qty:        mapFloat(legM, "qty"),
			mu:         mapFloat(legM, "mult"),
		}
		if !fn(l, legM) {
			return
		}
	}
}

// maxPotentialLoss walks every option leg in the map, enumerates distinct
// strikes, evaluates each leg's payoff at each strike, and returns the
// largest net loss as a non-negative USD amount.
func maxPotentialLoss(legsVal ref.Val) float64 {
	var opts []legView
	forEachLeg(legsVal, func(l legView, _ traits.Mapper) bool {
		if l.kind == "option" {
			opts = append(opts, l)
		}
		return true
	})
	if len(opts) == 0 {
		return 0
	}
	strikeSet := map[float64]struct{}{}
	for _, o := range opts {
		strikeSet[o.K] = struct{}{}
	}
	minPnL := math.Inf(+1)
	for U := range strikeSet {
		pnl := 0.0
		for _, o := range opts {
			var intrinsic float64
			if o.optionType == "call" {
				intrinsic = math.Max(0, U-o.K)
			} else {
				intrinsic = math.Max(0, o.K-U)
			}
			sign := 1.0
			if o.side == Short {
				sign = -1.0
			}
			pnl += sign * o.qty * o.mu * intrinsic
		}
		if pnl < minPnL {
			minPnL = pnl
		}
	}
	if minPnL >= 0 {
		return 0
	}
	return -minPnL
}

// isLimitedRisk returns true iff an option-only structure has bounded loss
// in both underlying tails. An options-only payoff diverges when:
//   - net short call quantity > 0 (U → ∞: each uncovered short call loses ∞)
//   - net short put quantity > 0 (U → 0: each uncovered short put loses K)
//     — a single naked short put IS bounded (loss caps at qty*mult*K), but a
//     ratio put spread with more shorts than longs is unbounded relative to
//     the long protection; mpl() would only sample existing strikes and miss
//     the U→0 corner. Reject the whole structure here rather than try to
//     distinguish "bounded but large" from "unbounded".
//   - any non-option leg (stock/etf/etn) — those belong to specific rules,
//     not this catch-all
func isLimitedRisk(legsVal ref.Val) bool {
	netCallQty, netPutQty := 0.0, 0.0
	limited := true
	forEachLeg(legsVal, func(l legView, _ traits.Mapper) bool {
		if l.kind != "option" {
			limited = false
			return false // short-circuit
		}
		sign := 1.0
		if l.side == Short {
			sign = -1.0
		}
		switch l.optionType {
		case "call":
			netCallQty += sign * l.qty
		case "put":
			netPutQty += sign * l.qty
		}
		return true
	})
	return limited && netCallQty >= 0 && netPutQty >= 0
}

func sumPremiums(legsVal ref.Val, field string, side Side) float64 {
	total := 0.0
	forEachLeg(legsVal, func(l legView, legM traits.Mapper) bool {
		if l.kind != "option" || l.side != side {
			return true
		}
		total += mapFloat(legM, field) * l.qty * l.mu
		return true
	})
	return total
}

// --- helpers for unwrapping ref.Val ---

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

func asString(v ref.Val) string {
	if s, ok := v.(types.String); ok {
		return string(s)
	}
	if s, ok := v.Value().(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v.Value())
}

func mapFloat(m traits.Mapper, key string) float64 {
	v := m.Get(types.String(key))
	return asFloat(v)
}

func mapString(m traits.Mapper, key string) string {
	v := m.Get(types.String(key))
	return asString(v)
}

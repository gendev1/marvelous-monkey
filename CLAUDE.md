# margincalc

Programmable margin calculator. A CEL-based rule engine (`internal/engine/`) evaluates Cboe / FINRA strategy rules from YAML (`rules/`); a CSV-driven reconciliation harness (`internal/recon/`, `cmd/recon/`) diffs engine output against a vendor's numbers.

## Environment

- Go `1.23` standalone module (path `margincalc`). Run all commands from the module root.
- Deps: `github.com/google/cel-go`, `gopkg.in/yaml.v3`.

## Codebase-Specific Conventions

- The engine is package-private under `internal/`. When this needs to be an SDK, add a thin `pkg/margincalc/` facade that re-exports from `internal/engine/`.
- Numeric leg/position fields are `float64` throughout — CEL is strict-double-typed. YAML integers in the `constants:` block are normalized to float64 at load.
- New CEL primitives go in `internal/engine/env.go` via `cel.Function`. Walk legs through `forEachLeg(legsVal, fn)` — do not re-implement that boilerplate.
- Use named constants from the YAML `constants` block (e.g. `constants.long_option_loan_value_threshold_months`) instead of literal numbers when one exists.
- Rule, function, and type-level invariants live as inline doc comments next to the thing they constrain (e.g. `Result` semantics in `types.go`, concurrency in `Rulebook`'s godoc, the `is_limited_risk` guard in `cboe_baseline.yaml` above `generic_limited_risk_combo`). Read those before changing the surrounding code.

## Required Rules

- **Rule order in YAML is load-bearing.** `Evaluate` returns the *first* rule whose match binds and whose constraints all hold. Specific strategies must appear **above** `generic_limited_risk_combo`.
- **`generic_limited_risk_combo` is the only rule using `legs_pattern: all_options`** and is gated by `is_limited_risk(legs)`. Don't weaken that guard — ratio spreads can otherwise look bounded at the existing strikes while being unbounded at `U → ∞`.
- **CEL functions that depend on a `rates` key must error on miss**, not silently zero-fallback (see `shortOptionReq`). Silent zero hides typos in `class`.
- **`Rulebook` is concurrent-safe only because `LoadRulebook` pre-compiles every formula + constraint.** Don't add lazy-compile paths without a mutex.
- **`bindSlots` slot patterns within one rule must be uniquely-attributed** by (side, kind, option_type, venue) so the singleton-disjoint fast-path applies and the matcher returns a deterministic binding. If two slots share an attribute pattern, DFS still binds *some* assignment — but constraints will only see the first one.

## Adding a rule

1. Add the entry to `rules/cboe_baseline.yaml` (or `rules/house_rules.yaml`) **above** `generic_limited_risk_combo`. Declare slots in `match.legs` and predicates in `match.constraints`.
2. Add a test in `internal/engine/rulebook_test.go` against a known-good number — Cboe manual page, FINRA Rule 4210 example, or broker output. Assert all three of `Requirement`, `AppliedProceeds`, `CashCall` where the source provides them. Names follow `Test<Strategy>_p<page>` so the manual page stays greppable.

## Tests and Validation

Engine tests load `rules/cboe_baseline.yaml` via a relative path — run from the module root, not from inside `internal/engine/`.

```sh
go test ./...                                       # full suite (engine + recon)
go test -race ./...                                 # before any concurrency-sensitive change
go test ./internal/engine -run TestShortPutOTM_p28  # single test by name regex
go test -bench=BindSlots -benchmem ./internal/engine
go vet ./...
```

## Out of scope

Per the README: FLEX options, PDT/day-trading, portfolio margin (SPAN-style), European calendar-spread broker overrides, European long box in a cash account. The `short_call_long_convertible` and `short_call_long_warrant` rules have no worked Cboe examples — treat their outputs as draft until reviewed.

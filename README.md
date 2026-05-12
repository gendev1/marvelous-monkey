# margincalc

Strategy-based margin calculator. Rules live in `cboe_margin_rules.yaml` as CEL expressions; the Go module here is the evaluator and pattern-matcher.

## Architecture

```
cboe_margin_rules.yaml      CEL formulas + match patterns + rates
        │
        ▼
rulebook.go     loads YAML, compiles each formula via cel-go, caches Program
        │
        ▼
match.go        binds position legs → named slots (one-to-one + constraint check)
        │
        ▼
env.go          cel.Env with custom funcs: max, min, rate, intrinsic_*,
                short_call_req, short_put_req, mpl, sum_*_premiums
        │
        ▼
Evaluate(pos, accountType, phase) → Result
```

Rules are tried in declaration order. First rule whose match-pattern binds and whose CEL constraints all pass, wins. Specific strategies (covered_call, conversion, vertical_spread, ...) come before the `generic_limited_risk_combo` catch-all which handles butterflies/condors/iron-anything via the max-potential-loss algorithm.

## Why CEL

- `cel-go` gives us a typed, sandboxed evaluator with sub-millisecond compile + microsecond eval per formula. Programs are cached at load.
- Formulas are real expressions, not strings shaped like expressions. Syntax errors fail at `LoadRulebook`, not at runtime per-position.
- Same rules can drive a Java or Python service later without rewriting the rule file (cel-java, cel-python both available).
- Custom domain functions (`mpl`, `rate`, `short_*_req`) plug in via `cel.Function` and stay outside the rule file — keeps the YAML focused on policy, not algorithm.

## Usage

```go
rb, err := margincalc.LoadRulebook("cboe_margin_rules.yaml")
if err != nil { log.Fatal(err) }

pos := margincalc.Position{
    U: 128.50, Class: "equity",
    Legs: []margincalc.Leg{
        {Side: margincalc.Short, Kind: margincalc.OptionKind, OptionType: "call",
         K: 120, P0: 8.40, P: 8.40, Qty: 1, Mult: 100},
    },
}
res, _ := rb.Evaluate(pos, margincalc.MarginAccount, margincalc.Initial)
// res.RuleID = "short_call_uncovered"
// res.Requirement = 3410.00
```

## Result semantics

Every numeric evaluation returns three quantities, matching the Cboe Manual's distinction between gross requirement and SMA debit:

- `Result.Requirement` — the manual's **"Margin Requirement"** (gross, before short proceeds are applied).
- `Result.AppliedProceeds` — short-option premium credit received when putting the trade on.
- `Result.CashCall` — `Requirement - AppliedProceeds`; the manual's **"SMA Debit / Cash Call"** (net cash the customer must deposit).

Rules in YAML express these as `initial` / `maintenance` (gross) plus `initial_proceeds` / `maintenance_proceeds` (credit). Long-only positions can omit the proceeds expressions; they default to 0.

For cash-account-only strategies the rule may return `Permitted: false` instead of a number. For short uncovered options in a cash account the rule returns BOTH a USD-equivalent number AND `DepositKind: "cash_or_escrow"` / `"underlying_or_escrow"` — the number is the cash-equivalent deposit (aggregate strike for puts, underlying market value for calls), the kind is the authoritative statement of acceptable collateral forms.

Multi-leg cash coverage tracks the manual where it gives an unambiguous rule:

- Verticals, short boxes, and any limited-risk multi-leg combo: cash requirement = `mpl(legs) + long premium`, with short premium as `AppliedProceeds`.
- Long boxes: no cash block — the European style depends on the loan-value mechanism (margin-only) and splitting by style is left until a concrete case appears.
- Short strangles/straddles and short-stock-bearing structures: `Permitted: false` in cash (different leg collaterals can't be satisfied by one deposit).

## What's covered

22 worked / derived examples from the Cboe Margin Manual (Nov 2021) reproduce exactly. Strategies:

- Long option (≤9mo, listed >9mo, OTC >9mo)
- Short call / short put uncovered (incl. leveraged ETF/ETN)
- Short strangle/straddle
- Vertical spread (put/call)
- Long & short box spreads (incl. European loan-value path)
- Short put + short stock
- Covered call
- Short index call + long ETF
- Protective put
- Long call + short stock
- Conversion / Reverse conversion / Collar
- Short call + long marginable convertible (manual p.14)
- Short call + long marginable stock warrant (manual p.14)
- Generic limited-risk combo (butterflies, condors, iron variants)

The convertible and warrant cases have no worked examples in the Cboe manual
— their expected values were derived by applying the formula text directly and
are best treated as draft until your PM / risk reviews them.

## What's not covered

- FLEX options (largely follow conventional rules; broker overrides matter more)
- Day-trading / PDT (out of manual scope)
- Portfolio margin / risk-based margining
- European calendar spread broker overrides (manual flags these as broker-specific)

## Adding a rule

1. Add the entry to `cboe_margin_rules.yaml`. Match section declares leg slots + CEL constraint predicates. Formula section is CEL strings keyed by:
   - `cash.initial`, `cash.maintenance`, `margin.initial`, `margin.maintenance` — gross requirements.
   - `cash.initial_proceeds`, `cash.maintenance_proceeds`, `margin.initial_proceeds`, `margin.maintenance_proceeds` — short-option credit (omit if 0).
2. If you need a new primitive, register it in `env.go` via `cel.Function`.
3. Add a test in `rulebook_test.go` against a known-good number — from the Cboe manual, from FINRA Rule 4210 examples, or from the broker's clearing system. Where possible assert all three of `Requirement`, `AppliedProceeds`, `CashCall`.

## Running

```sh
go test ./...
```

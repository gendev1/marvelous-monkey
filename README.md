# margincalc

A programmable margin calculator. Strategy-based Reg T today; account aggregator and risk-based layer designed for, not yet built.

The project owns three things:
1. **An engine** — CEL-based rule evaluator with custom domain functions (`mpl`, `is_limited_risk`, `short_call_req`, …).
2. **A rule set** — Cboe Margin Manual encoded as machine-readable YAML; house rules can replace or extend it.
3. **A reconciliation harness** — CSV-in / CSV-out diff tool that compares engine output against an existing vendor.

For the full plan see [`roadmap.html`](roadmap.html). For where current work fits, the layout below.

## Layout

Canonical Go module layout — binaries in `cmd/`, implementation in `internal/`, data files in their own dirs at the root.

```
margincalc/
├── README.md                  this file
├── roadmap.html               the plan (open in browser)
├── go.mod / go.sum
│
├── cmd/
│   └── recon/main.go          CLI for the reconciliation harness
│
├── internal/
│   ├── engine/                package engine (the core)
│   │   ├── types.go           Position, Leg, Result, Side, Kind, …
│   │   ├── env.go             CEL environment + custom functions
│   │   ├── match.go           leg-slot binding (bitmask matcher)
│   │   ├── rulebook.go        LoadRulebook + Evaluate
│   │   ├── rulebook_test.go   strategy / cash / guard tests
│   │   └── bench_test.go      throughput benchmarks
│   └── recon/                 package recon (CSV diff vs vendor)
│       ├── recon.go
│       ├── recon_test.go
│       └── testdata/
│
├── rules/
│   ├── cboe_baseline.yaml     regulatory baseline rule set
│   └── house_rules.example.yaml   schema template for the firm's rules
│
└── docs/
    └── Margin_Manual.pdf      source document for cboe_baseline.yaml
```

Everything under `internal/` is module-private — Go won't let any package outside this module import it. That keeps the surface area honest while we're iterating. When the firm needs to expose this as an SDK to another internal service, a thin facade in `pkg/margincalc/` re-exports the relevant types from `internal/engine/`.

## The engine

Position-level, single-strategy. Takes a `Position`, returns a `Result` with three numbers per the manual's distinction:

| Field | Meaning |
|---|---|
| `Requirement` | gross "Margin Requirement" (matches the manual's column) |
| `AppliedProceeds` | short-option premium credit |
| `CashCall` | net cash the customer must deposit = Requirement − AppliedProceeds |

Plus `DepositKind` for short-option cash-account positions where the deposit is shares or escrow, not just dollars.

```go
import "margincalc/internal/engine"

rb, _ := engine.LoadRulebook("rules/cboe_baseline.yaml")

pos := engine.Position{
    U: 128.50, Class: "equity",
    Legs: []engine.Leg{{
        Side: engine.Short, Kind: engine.OptionKind, OptionType: "call",
        K: 120, P0: 8.40, P: 8.40, Qty: 1, Mult: 100,
    }},
}
res, _ := rb.Evaluate(pos, engine.MarginAccount, engine.Initial)
// res.RuleID = "short_call_uncovered"
// res.Requirement = 3410.00 ; res.AppliedProceeds = 840.00 ; res.CashCall = 2570.00
```

### Coverage

22 worked / derived examples from the Cboe Margin Manual (Nov 2021) reproduce exactly. Strategies:

- Long option (≤9mo, listed >9mo equity-class only, OTC >9mo)
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
- Generic limited-risk combo (butterflies, condors, iron variants) — guarded by `is_limited_risk` so unbounded ratio spreads don't silently get a fake finite number

### Not covered (and why)

- **FLEX options** — broker overrides matter more than the universal manual rules.
- **Day trading / PDT** — separate FINRA regime.
- **Portfolio margin / risk-based margining** — see roadmap items 6–8.
- **European calendar spread broker overrides** — manual flags these as broker-specific.
- **European long box in a cash account** — relies on loan-value mechanism (margin-only).

## The rule set

`rules/cboe_baseline.yaml` is the regulatory baseline. Your firm's actual production rules are stricter and live (today) inside the vendor; encode them in `rules/house_rules.yaml` using the schema in `rules/house_rules.example.yaml` as you discover them via reconciliation.

### Adding a rule

1. Add the entry to the appropriate rules file. The `match` section declares leg slots + CEL constraint predicates. The `formulas` section is CEL strings keyed by:
   - `cash.initial`, `cash.maintenance`, `margin.initial`, `margin.maintenance` — gross requirements
   - `cash.initial_proceeds`, `cash.maintenance_proceeds`, `margin.initial_proceeds`, `margin.maintenance_proceeds` — short-option credit (omit if 0)
   - `cash.deposit_kind` / `margin.deposit_kind` — coexists with numeric formulas; describes acceptable collateral form
2. If you need a new primitive, register it in `env.go` via `cel.Function`.
3. Add a test in `rulebook_test.go` against a known-good number — from the Cboe manual, from FINRA Rule 4210 examples, or from the broker's clearing system. Assert all three of `Requirement`, `AppliedProceeds`, `CashCall` where the source provides them.

## The reconciliation harness

```sh
go run ./cmd/recon \
  -rules rules/cboe_baseline.yaml \
  -positions positions.csv \
  -legs legs.csv \
  -out diff.csv \
  -tolerance 0.01
```

Produces a summary (MATCH / DIFF subdivided by size / NO_RULE / ERROR) plus a per-position `diff.csv`. Sample CSVs in `internal/recon/testdata/`.

The DIFF rows clustered by `rule_id` are the firm's house policy made visible. Use them to populate `rules/house_rules.yaml` over time.

## Future work — where it lands

The roadmap items in `roadmap.html` map to this layout as:

| Item | Where it lives |
|---|---|
| 02. Account aggregator (LMV/SMV/Equity) | `internal/account/` |
| 04. Rule-set versioning & audit | extend `internal/engine/` + new `internal/audit/` |
| 05. House Policy overlays | `internal/overlay/` (post-rule modifiers) |
| 06. Risk-shock engine | `internal/shock/` (consumes Position + delta from engine) |
| 07. Universal Spread Rule | `internal/decomp/` (runs before per-position evaluation) |
| 08. Full TIMS / pricing | `internal/tims/` (separate engine selectable by `Margin Type`) |

Each gets its own CLI binary in `cmd/<name>/` when it earns one (e.g. `cmd/aggregate/` for the account roll-up). Nothing above changes `internal/engine/` or `internal/recon/`. They sit on top.

## Running

```sh
go test ./...        # full suite (engine + recon)
go test -bench=.     # benchmarks
```

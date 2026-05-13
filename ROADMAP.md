# margincalc · status

**main** `54c02e5` · updated 2026-05-13

Strategy-based RegT engine. Three-layer architecture. Vendor-free.

## Snapshot

| Tests | Cboe examples | Rules | Layers | Active | Backlog |
|------:|--------------:|------:|-------:|-------:|--------:|
| 104   | 22            | 20    | 3      | 0      | 5       |

## Three layers

### Layer 1 · Per-position regulatory engine — **ready** (96%)

Reg T strategy match. CEL rules from YAML, per-position formula evaluation.

- [x] 20 strategy rules in `rules/cboe_baseline.yaml`
- [x] Typed `engine.Leg` end-to-end (NativeTypes)
- [x] Load-time overload-mismatch checks
- [x] Per-leg `validateLeg` invariants
- [x] `mpl` samples U=0; signed `qty*mult`; bounded ratio + mismatched-mult tests
- [x] `cmd/recon` per-position CSV diff harness

*92 tests · `internal/engine`*

### Layer 2 · Account container + aggregation — **ready** (~90%)

Positions → Account → vendor-comparable account state (LMV / SMV / equity / adjusted-balance).

- [x] `Account` / `AccountPosition` / `AccountSnapshot`
- [x] `PositionEvaluation` with `NoRule` / `Violation` as first-class outcomes
- [x] `validate(account)` — shape + per-leg market-value validation
- [x] `Aggregate(account) → AccountSnapshot` (+ `AggregateWithRulebook`)
- [x] `DepositRequirements` rollup by kind
- [ ] Account-level reconciliation harness — deferred pending vendor API contract

*12 tests · `internal/account`*

### Layer 3 · House / broker overlay — **not started** (~5%)

Firm-specific add-ons applied on top of L1 + L2 outputs. The number the customer actually has to deposit.

- [x] `rules/house_rules.example.yaml` — schema sketch
- [ ] Multi-file rulebook loader (deterministic precedence)
- [ ] Vol / low-price floor / market-cap / ADV add-ons
- [ ] Single-name / sector concentration limits
- [ ] `HouseRequirement` output shape
- [ ] House-overlay reconciliation

*0 tests · not yet packaged*

## Active

_None — Layer 2 aggregator landed; next active item picks up from the backlog once sequencing is confirmed._

## Done · recently landed

| #  | Item                                                                                                                                                                                                            | Layer   | Status |
|---:|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|---------|--------|
| 03 | **`Aggregate(account) → AccountSnapshot`** — `Aggregate` + `AggregateWithRulebook` shipped in `internal/account/`. Sterling-named LMV/SMV/equity/adjusted-balance fields; `DepositRequirements` rollup by kind. | Layer 2 | done   |
| 01 | **Engine correctness hardening** — `mpl` samples U=0, signed `qty*mult`, bounded put-ratio + mismatched-mult tests, typed Leg, load-time overload-mismatch, `validateLeg` invariants — all on main.            | Layer 1 | done   |

## Backlog · sequenced, not yet started

| #  | Item                                                                                                                                                                                                | Layer    | Status |
|---:|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------|--------|
| 05 | **Rule-set versioning + audit trail** — Stamp `Result` with the rulebook SHA; persist inputs/outputs to a sqlite/parquet log.                                                                       | Layer 1  | later  |
| 06 | **House rulebook composition** — Multi-file loader with deterministic precedence. `house_rules.example.yaml` schema already landed.                                                                 | Layer 1  | later  |
| 07 | **House / broker overlay add-ons** — Vol, low-price floor, market-cap, ADV / liquidity, concentration. Consumes L1 `Result` + L2 `AccountSnapshot`, emits `HouseRequirement`.                       | Layer 3  | later  |
| 08 | **Risk-shock engine ("poor-man's PM")** — Delta-based ±20% / ±50%, ±3σ, single-name 50%, worst-case + 5% liquidity haircut. TIMS-shaped number without a vol surface.                                | Layer 1+ | later  |
| 09 | **Universal Spread Rule** — Cross-position 2-leg pair-up; sum requirements per Sterling. Prereq for portfolio-level RegT.                                                                          | Layer 1  | later  |

## Deferred · explicitly parked

| #  | Item                                                                                                                                                                                                | Layer   | Status   |
|---:|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|---------|----------|
| 04 | **Account-level reconciliation — deferred pending vendor API contract** — Extend `cmd/recon` to ingest vendor account-level rows and diff against `AccountSnapshot`. Current per-position harness is forgiving by design; account-level diffing waits on a stable vendor API contract before the schema is worth pinning down.                                                                                          | Layer 2 | deferred |
| 02 | **Reconciliation v2: fail loudly on bad data** — Header / enum / `NaN` / duplicate-ID validation. Only needed before nightly vendor runs; current forgiving harness is fine for spot checks.       | L1 tool | deferred |
| 10 | **Full TIMS / pricing layer** — OCC scenario grid, vol surface, binomial American pricing. Separate project. Wait until L1 + L2 + L3 reconcile against the vendor.                                  | Layer 1 | deferred |

## Out of scope

- **FLEX option exception handling** — broker-policy-driven; not in CBOE universal scope.
- **European long box cash treatment** — relies on loan-value mechanism (margin-account only); no clean cash formula.
- **Day-trading / PDT** — separate FINRA rule, separate engine surface.
- **Horizon / Pin / VaR views** — consumers of the same underlying state, not new engines.
- **SPAN / RBH_BD / RBH_MM / EXEC** — separate Layer-1 engine implementations; not on the path for this project.

## Sequencing

Engine correctness (#01) and the Layer 2 aggregator (#03) are done. The middle items (#05 – #09) only become fully verifiable once vendor account numbers can be diffed against `AccountSnapshot` — but account-level reconciliation (#04) is parked until a vendor API contract exists, so unblocking happens through the contract conversation rather than the codebase. Recon hardening (#02) gets revisited when nightly vendor runs are actually on the table — until then the current forgiving harness is fine for spot checks.

---

*Generated 2026-05-13 · main @ `54c02e5` · living doc — edit freely as the project advances.*

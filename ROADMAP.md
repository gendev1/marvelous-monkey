# margincalc · status

**main** `54c02e5` · updated 2026-05-14

Strategy-based RegT engine. Three-layer architecture. Vendor-free.

## Snapshot

| Tests | Cboe examples | Rules | Layers | Active | Backlog |
|------:|--------------:|------:|-------:|-------:|--------:|
| 273   | 22            | 20    | 4 (+L0.5 shipped) | 0 | 8 |

## Three layers

### Layer 1 · Per-position regulatory engine — **ready** (96%)

Reg T strategy match. CEL rules from YAML, per-position formula evaluation.

- [x] 20 strategy rules in `rules/cboe_baseline.yaml`
- [x] Typed `engine.Leg` end-to-end (NativeTypes)
- [x] Load-time overload-mismatch checks
- [x] Per-leg `validateLeg` invariants
- [x] `mpl` samples U=0; signed `qty*mult`; bounded ratio + mismatched-mult tests
- [x] `cmd/recon` per-position CSV diff harness

*143 tests · `internal/engine`*

### Layer 2 · Account container + aggregation — **ready** (~90%)

Positions → Account → vendor-comparable account state (LMV / SMV / equity / adjusted-balance).

- [x] `Account` / `AccountPosition` / `AccountSnapshot`
- [x] `PositionEvaluation` with `NoRule` / `Violation` as first-class outcomes
- [x] `validate(account)` — shape + per-leg market-value validation
- [x] `Aggregate(account, evals) → AccountSnapshot` (+ `AggregateWithRulebook(rb, account)`)
- [x] `DepositRequirements` rollup by kind
- [ ] Account-level reconciliation harness — deferred pending vendor API contract

*50 tests · `internal/account`*

### Layer 3 · House / broker overlay — **MVP shipped** (~85%)

Firm-specific add-ons applied on top of L1 + L2 outputs. The number the customer actually has to deposit.

- [x] `rules/house_rules.notes.md` — schema sketch
- [x] `HouseRequirement` output shape
- [x] `ReferenceData` / security facts input model
- [x] `rules/house_overlay.example.yaml` — account-aware overlay schema
- [x] Overlay rulebook loader (deterministic precedence + CEL compile)
- [x] Low-price long/short floors
- [ ] Symbol/account/group percentage overrides
- [x] Single-name concentration add-on
- [x] Component-level audit trail
- [ ] House-overlay reconciliation

*74 tests · `internal/overlay`*

## Active

_None — Layer 3 overlay MVP landed; next sequenced work is in the backlog below._

## Done · recently landed

| #  | Item                                                                                                                                                                                                            | Layer   | Status |
|---:|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|---------|--------|
| 13 | **Layer 0.5 — spread optimizer** — Branch-and-bound decomposition of arbitrary multi-leg portfolios into recognized strategies, with per-leg attribution. **Shipped.** See `internal/optimizer/`. Gating capability for margincalc-as-a-product. | Layer 0.5 | done |
| 12 | **Cross-layer review pass** — Closed `compile()` write-during-`Evaluate` race (all CEL programs pre-compiled in `LoadRulebook`; eval-time `lookupProg` panics on miss); `requireExpirationSlots` now actually compares dates; strict YAML decode with `KnownFields(true)`; `AdjustedBalance` adds back `SMVStock`; recon row-numbers preserved through blank-line skips; required-float guard on `qty`/`mult`; overlay `validScopes` truthful; group-scope `on_missing_reference: error` honored; missing `requires` blocks on four uncovered rules; gofmt CI gate. | Cross-layer | done   |
| 07 | **House / broker overlay MVP** — `internal/overlay` package ships `HouseRequirement` from `AccountSnapshot` + reference data, with low-price floors, single-name concentration, component-level audit trail, and an `EvaluateHouse` Layer 1→2→3 wrapper. Example overlay at `rules/house_overlay.example.yaml`. See `docs/epics/layer3-house-overlay-plan.md`. | Layer 3 | done   |
| 03 | **`Aggregate(account, evals) → AccountSnapshot`** — `Aggregate` + `AggregateWithRulebook` shipped in `internal/account/`. Sterling-named LMV/SMV/equity/adjusted-balance fields; `DepositRequirements` rollup by kind. | Layer 2 | done   |
| 01 | **Engine correctness hardening** — `mpl` samples U=0, signed `qty*mult`, bounded put-ratio + mismatched-mult tests, typed Leg, load-time overload-mismatch, `validateLeg` invariants — all on main.            | Layer 1 | done   |

## Backlog · sequenced, not yet started

| #  | Item                                                                                                                                                                                                | Layer    | Status |
|---:|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------|--------|
| 05 | **Rule-set versioning + audit trail** — Stamp `Result` with the rulebook SHA; persist inputs/outputs to a sqlite/parquet log.                                                                       | Layer 1  | later  |
| 06A | **Layer 1 house rulebook composition** — Multi-file loader with deterministic precedence for strategy/rate overrides. `house_rules.notes.md` schema already landed.                             | Layer 1  | later  |
| 06B | **Layer 3 overlay rulebook loader** — Account-aware overlay loader with deterministic precedence, typed scope/mode validation, and CEL `when`/`formula` precompile.                                  | Layer 3  | part of #07 |
| 07A | **Deferred rich overlay dimensions** — Volatility buckets, market-cap, ADV / liquidity, sector concentration, HTB, exposure fees. Requires stable vendor/reference data evidence.                    | Layer 3  | later  |
| 08 | **Risk-shock engine ("poor-man's PM")** — Delta-based ±20% / ±50%, ±3σ, single-name 50%, worst-case + 5% liquidity haircut. TIMS-shaped number without a vol surface.                                | Layer 1+ | later  |
| 09 | **Universal Spread Rule** — Cross-position 2-leg pair-up; sum requirements per Sterling. Prereq for portfolio-level RegT.                                                                          | Layer 1  | later  |
| 11 | **Multi-regime margin router** — Add `internal/margin` normalized account result types and `internal/regime` selector/orchestrator so Reg T, house overlay, and future TIMS/SPAN/shock engines share one account-level contract. See `docs/epics/multi-regime-margin-plan.md`. | Cross-layer | later |

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

Engine correctness (#01), the Layer 2 aggregator (#03), the Layer 3 house-overlay MVP (#07), and the cross-layer review-fix pass (#12) are done — `internal/overlay` ships `HouseRequirement`, reference data, low-price floors, single-name concentration, and component attribution with an audit trail; the engine's concurrent-`Evaluate` invariant is now actually enforced. Remaining Layer 3 work is symbol/account/group percentage overrides and house-overlay reconciliation. The gating capability for margincalc-as-a-product — **Layer 0.5, the spread optimizer (#13)** — is shipped: `internal/optimizer.Optimize` branch-and-bounds arbitrary leg portfolios into recognized strategies with per-leg attribution, so callers no longer have to pre-classify. The multi-regime router (#11) lands alongside or after L0.5 so Reg T, house overlay, and future engines share one account-level result contract. Account-level reconciliation (#04) stays parked until a vendor API contract exists. Recon hardening (#02) gets revisited when nightly vendor runs are actually on the table.

---

*Generated 2026-05-14 · main @ `54c02e5` · living doc — edit freely as the project advances.*

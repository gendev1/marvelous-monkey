# Issue #43: Implement position-scope evaluator with `add`, `max`, `floor` modes

> GitHub: https://github.com/gendev1/marvelous-monkey/issues/43
> Branch: `issue-43-implement-position-scope-evaluator-with`
> Repo: `margincalc`

---


Epic: #49 — complexity **[5]**.

## Dependencies

- #42 — `Rulebook` must hold compiled CEL programs and ordered rules.

## Summary

Implement `Engine.Evaluate` for `scope: position` rules with modes `add`, `max`, and `floor`. Produces `HouseComponent` entries and the running `HouseRequirement` totals. No group, block, or audit-hash work — those land in later issues.

## Relevant Resolved Decisions

- **D3** — When `ReferenceData.Securities[SecKey{symbol, venue}]` is missing for a position, default `security.instrument_kind = "stock"` and append a `Warnings` entry like `"reference data missing for AAPL@listed; defaulted instrument_kind=stock"`. Rules whose `applies.instrument_kinds` excludes `"stock"` will not fire. Do NOT treat the missing data itself as a violation in this issue — issue 5 owns the `on_missing_reference: error` path.
- **D4** — Reference-data lookup uses `SecKey{Symbol: pos.Underlying or pos.Symbol, Venue: pos.Leg.Venue}`. Document which field (`Symbol` vs `Underlying`) the lookup uses per position kind.
- **D5** — No currency filter applied. Account currency flows into the audit only.

## Context

Position scope is the most common — low-price floors, per-position percentage overrides, leveraged-ETF multipliers. Getting this right validates the entire evaluation pipeline before group scope adds complexity.

## Files to Touch

- `internal/overlay/evaluate.go` — new. `Engine.Evaluate`, per-position iteration, applies-matrix filter, CEL activation construction, mode-specific composition (`add`, `max`, `floor`).
- `internal/overlay/facts.go` — new. Per-position fact derivation (`long_market_value`, `short_market_value`, `gross_market_value`, `net_market_value`, `long_shares`, `short_shares`) from `account.AccountPosition` plus its matching `PositionEvaluation`.
- `internal/overlay/evaluate_test.go` — new.

## Approach

1. **Snapshot embed.** First line of `Evaluate`: `out := HouseRequirement{Snapshot: snap, AccountID: acct.ID, AsOf: acct.AsOf, Currency: acct.Currency, BaselineRequirement: snap.TotalRequirement, BaselineCashCall: snap.TotalCashCall}`. Pure copy; do not mutate the inputs.
2. **Iterate snap.Evaluations in stable order** (already deterministic from `account.Aggregate`). For each `(position, evaluation)`:
   - Resolve `SecKey` from the position's primary leg: `SecKey{Symbol: leg.Underlying, Venue: leg.Venue}` for stock positions; option positions are out of scope this issue (skip with no error).
   - Look up `SecurityFacts`. If missing, build a synthetic one with `InstrumentKind = "stock"` and `Symbol`, `Venue` filled. Append a `Warnings` entry.
   - Derive per-position facts (`facts.go`).
3. **Per-rule application.** Walk `rb.orderedRules`. Skip rules whose `scope != "position"` — group and account scopes are not in this issue.
4. **Applies matrix.** For each position-scope rule, check `applies` filters against the position and account. Skip when:
   - `applies.account_types` set and `acct.AccountType` not in it.
   - `applies.phases` set and `acct.Phase` not in it.
   - `applies.instrument_kinds` set and `security.InstrumentKind` not in it.
   - `applies.sides` set and the position's side not in it (long = long_shares > 0; short = short_shares > 0; positions with both sides simultaneously are out of scope this issue — comment but do not handle).
5. **CEL activation.** Build `map[string]any` with: `account` (Go struct converted to map with snapshot totals + currency), `position` (facts map), `security` (SecurityFacts mirror), `constants` (rulebook constants). Eval `when`; if not truthy, continue.
6. **Formula eval.** Eval rule's compiled formula. Coerce to `float64`. Reject NaN/Inf with a `Warnings` entry naming the rule — do not crash and do not silently treat as zero.
7. **Compose by mode** (write the `HouseComponent` for each fired rule before moving on):
   - `add`: `Delta = formulaValue`. Always `Applied = true`.
   - `max`: `BaselineAmount = perPositionBaseline` (sum of `evaluation.Result.Requirement` for this position; if absent, use `0`). `OverlayAmount = formulaValue`. `Delta = max(0, OverlayAmount - BaselineAmount)`. `Applied = Delta > 0`.
   - `floor`: same numeric behavior as `max`; component records `Mode = "floor"` for audit attribution.
8. **Accumulate.** `out.HouseRequirement = out.BaselineRequirement + sum(applied delta)`. Same for `HouseCashCall`. `out.Excess = snap.CurrentEquity - out.HouseRequirement`.
9. **Component identity.** Set `PositionID = position.ID`, `Symbol = leg.Underlying`, `Scope = "position"`, `Basis` from rule, `Formula = rule.formula`, `Reason = rule.reason`, `Evidence` map with the values that went into the formula (e.g. `{"position.long_market_value": 12345.0, "constants.low_price_requirement_pct": 1.0}`). The evidence map is allowed to be approximate this issue — exhaustive evidence collection is part of issue 6.

## Test Plan

`internal/overlay/evaluate_test.go`:

- `TestEvaluate_NoRules_PassthroughBaseline` — empty rulebook → `HouseRequirement == BaselineRequirement`, no components, no warnings.
- `TestEvaluate_AddMode_AccumulatesDelta`.
- `TestEvaluate_MaxMode_PositiveDeltaOnly` — overlay below baseline produces `Applied = false`, `Delta = 0`.
- `TestEvaluate_MaxMode_AppliesShortfall` — overlay above baseline, only the positive shortfall is added.
- `TestEvaluate_FloorMode_BehavesLikeMaxButCarriesFloorAttribution`.
- `TestEvaluate_AppliesMatrix_AccountTypeFilter`.
- `TestEvaluate_AppliesMatrix_InstrumentKindFilter_ETFOnly` — rule requires `[etf]`; with reference data present and `InstrumentKind = "etf"`, fires. With missing ref data (defaults to "stock"), does not fire.
- `TestEvaluate_MissingReferenceData_DefaultsToStockAndWarns`.
- `TestEvaluate_NaNFromFormula_WarnsAndSkips`.
- `TestEvaluate_InputsNotMutated` — deep-equal `acct` and `snap` before and after.
- `TestEvaluate_BaselineFieldsPopulated` — `out.BaselineRequirement == snap.TotalRequirement` regardless of rule activity.

## Acceptance Criteria

- `Engine.Evaluate` produces correct `HouseRequirement` for the three position modes against the test cases above.
- No mutation of `acct` or `snap`.
- Missing reference data does not error; it falls back and warns per D3.
- `out.Currency == acct.Currency`.
- All position-scope tests pass; group and block tests stay out (their issues haven't landed).

## Edge Cases

- Position with both long and short shares simultaneously (combo positions decomposed at the account layer): out of scope this issue. Document with a `TODO` comment naming the future PR.
- Rule whose `when` evaluates to `false`: contributes nothing — no component, no audit entry. Issue 6 records "matched but not applied" entries; this issue does not.
- `evaluation.Error != nil` for a position: skip overlay evaluation for that position; the engine error is the higher-priority signal.

## Required Verification

- `gofmt -w internal/overlay/`
- `go test ./internal/overlay`
- `go vet ./...`

## Out of Scope

- Group scope (issue 4).
- `block` mode and `on_missing_reference: error` policy (issue 5).
- Audit trail population beyond best-effort evidence (issue 6).
- Option positions.


---

## Repo Context

This is a Go module for a programmable margin calculator.

Core areas:

- `internal/engine` — CEL/YAML RegT rule engine.
- `internal/recon` — current CSV reconciliation harness.
- `internal/account` — planned account aggregation layer.
- `rules/` — Cboe baseline and house-rule examples.
- `cmd/` — CLI entry points.

Required conventions:

- Run commands from the repo root.
- Preserve invariants in `CLAUDE.md`.
- Rule order in YAML is load-bearing.
- Add behavioral tests for behavioral changes.
- Do not weaken CEL strictness, validation, or rulebook fail-fast behavior.
- Keep PR scope limited to this issue.

## Required Verification

Run before committing:

```sh
gofmt -w <changed-go-files>
go test ./...
go vet ./...
```

If `go vet ./...` reports an existing unrelated issue, document it in the PR body and still include `go test ./...`.

## Completion Instructions

1. Implement the issue end-to-end.
2. Run required verification.
3. Commit with a concise message.
4. Push the branch.
5. Open a PR with `Fixes #43` in the body.
6. Report the PR URL.

Do not amend unrelated commits. Do not force-push unless explicitly asked.

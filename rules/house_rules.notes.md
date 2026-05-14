<!--
DOCUMENTATION ONLY — NOT LOADABLE.

This file is a schema sketch for the firm's eventual Layer-1 (engine)
house rulebook. It is *not* parsed by any loader today:

  - `internal/engine.LoadRulebook` accepts a single YAML file shaped
    like `rules/cboe_baseline.yaml` (schema_version: "1", top-level
    `rates:`, `constants:`, `rules:`). The example below uses
    schema_version "3.0" and a `source:` map, both of which the engine
    parser rejects under `KnownFields(true)`.
  - `internal/overlay.LoadRulebook` is the Layer-3 overlay loader and
    has a different shape entirely (no `match.legs`, no
    `formulas.margin.*`). See `rules/house_overlay.example.yaml` for a
    working overlay example that integration tests load as-is.

Once issue 06A ("Layer 1 house rulebook composition" — multi-file loader
with deterministic precedence) lands, this file should be either:
  (a) split into a working `house_rules.example.yaml` whose shape
      matches whatever the multi-file engine loader accepts, or
  (b) folded into `rules/cboe_baseline.yaml`'s schema docs.

Until then this file is preserved as a design sketch.
-->

# House Rules — Schema Sketch (not loadable)

This file shows the schema the firm's actual margin rules should use
once multi-file engine loading (ROADMAP item 06A) lands. It is **not**
loaded by any current loader; see the HTML comment block at the top of
this file for the reasons.

## Why a separate file from `cboe_baseline.yaml`

- CBOE rules are the regulatory baseline (free, public, in
  `Margin_Manual.pdf`).
- The firm's actual rules are stricter and live (today) inside the
  current vendor (Sterling, etc.) — undocumented externally.
- Reconciling vendor output against `cboe_baseline.yaml` surfaces every
  divergence; each divergence is a house rule. Move them into a real
  `rules/house_rules.yaml` as they become known.

## Engine selection (future)

The Layer-1 rulebook loader will accept multiple files in priority
order — house rules win, CBOE fills the gap. For now,
`engine.LoadRulebook` takes one file; point it at `house_rules.yaml`
when ready to switch.

## Schema sketch

```yaml
schema_version: "3.0"

source:
  document: "<firm's internal margin policy document or vendor config>"
  publisher: "<firm name>"
  version_date: "<YYYY-MM-DD>"

# Rate tables can override the CBOE defaults. Any class omitted here
# falls back to cboe_baseline.yaml when multi-file loading is wired up.
rates:
  # Example: firm charges 25% base on equity (vs CBOE's 20%).
  # equity: { base_pct: 0.25, min_pct: 0.10 }

constants:
  # Firm-specific thresholds go here. Example:
  # pdt_min_equity_usd: 25000
  # pm_eligibility_min_equity_usd: 150000

# Rules in declaration order; first match wins.
rules:
  # -------------------------------------------------------------------
  # Example: firm's short-call rule charges 30% instead of CBOE's 20%.
  # Replace this whole block with real rules from the PM.
  # -------------------------------------------------------------------
  # - id: house_short_call_uncovered
  #   match:
  #     legs:
  #       - { name: opt, side: short, kind: option, option_type: call }
  #   formulas:
  #     margin:
  #       initial: |
  #         legs.opt.qty * legs.opt.mult * (
  #           legs.opt.P0
  #           + max(
  #               0.30 * lev * U - max(0.0, legs.opt.K - U),
  #               0.15 * lev * U
  #             )
  #         )
  #       maintenance: |
  #         legs.opt.qty * legs.opt.mult * (
  #           legs.opt.P
  #           + max(
  #               0.30 * lev * U - max(0.0, legs.opt.K - U),
  #               0.15 * lev * U
  #             )
  #         )
  #       initial_proceeds:     "legs.opt.P0 * legs.opt.qty * legs.opt.mult"
  #       maintenance_proceeds: "legs.opt.P  * legs.opt.qty * legs.opt.mult"
```

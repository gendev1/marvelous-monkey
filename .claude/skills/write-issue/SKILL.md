---
name: write-issue
description: Expand a margincalc epic's issue headings into PR-sized child issue files under docs/epics/<slug>/. Each child issue should be independently reviewable and suitable for one PR.
---

# write-issue

This skill expands an epic into implementation-ready child issues.

Correct harness model:

```text
Epic = coordination container
Child issue = one PR-sized implementation slice
One child issue file = one future GitHub child issue
start-issue = one child issue -> one branch/worktree/PR
```

Do not collapse an epic into one issue. Do not make giant issues.

## Input

Read:

```text
docs/epics/<slug>/epic.md
docs/architecture/decisions/<slug>.md   (if it exists — locked decisions from grill-with-docs)
```

For every bullet in `## Issues`, write:

```text
docs/epics/<slug>/<issue-slug>.md
```

If issue files already exist, ask whether to overwrite, skip, or abort.

## Feeding locked decisions into issues

If `docs/architecture/decisions/<slug>.md` exists, it is the source of truth for architectural decisions on this epic. Do not re-derive design choices from the epic body — read the decisions doc first and let it shape every issue.

For each child issue:

1. Identify which `D<n>` decisions in the decisions doc are load-bearing for this issue. A decision is load-bearing if the implementation would change shape depending on which alternative was picked.
2. Reference them explicitly in the issue body under a `## Decisions` section (between `## Context` and `## Files to Touch`):

   ```markdown
   ## Decisions

   This issue implements:
   - [D2 — `max` at `scope: group` floors over sum-of-per-position-baselines](../../architecture/decisions/<slug>.md#d2--max-at-scope-group-floors-over-sum-of-per-position-baselines)
   - [D3 — Instrument-kind classification lives on `SecurityFacts`](../../architecture/decisions/<slug>.md#d3--instrument-kind-classification-lives-on-securityfacts-not-engineleg)

   Do not re-litigate; if you believe a decision needs revisiting, surface it in the PR body and stop.
   ```

3. Use the decisions to constrain `## Approach` and `## Files to Touch`. If D3 says instrument-kind lives on `SecurityFacts`, the issue's approach must not extend `engine.Leg` with a sub-kind field — even if the user's plain-text plan was ambiguous.

If a child issue would need a decision that isn't yet locked in the decisions doc, **stop and tell the user**. Either run `/grill-with-docs` to lock it, or accept that the issue carries an unresolved question (which usually means it isn't ready to ship).

## Shared research

Inspect the epic and relevant files once before expanding issues.

Use `.claude/agents` if useful:

- `codebase-locator`
- `codebase-pattern-finder`
- `codebase-analyzer`

Focus on:

- existing Go package layout
- current tests
- rulebook/rule-evaluation patterns
- validation/error conventions
- docs/plans relevant to the epic

## Issue file format

```markdown
# <Issue Title>

Epic: [<Epic Title>](epic.md) — complexity **[N]**.

## Dependencies

None.

## Summary

One sentence describing what this PR-sized issue ships.

## Context

Why this slice matters and what existing code it touches.

## Files to Touch

- `path/file.go` — new/modify and why.

## Approach

Implementation narrative. Include data flow, error handling, and design decisions.

## Test Plan

Tests-first plan. Name test files and concrete cases.

## Acceptance Criteria

- Observable condition that must be true.

## Edge Cases

- Important failure/boundary behavior.

## Required Verification

- `gofmt -w <changed-go-files>`
- `go test ./...`
- `go vet ./...` if code changed

## Out of Scope

- Work explicitly excluded from this PR.
```

If dependencies exist, format them as:

```markdown
## Dependencies

- `<sibling-issue-slug>` — concrete artifact needed before this issue can start.
```

`graduate-epic` rewrites sibling slugs to GitHub `#N` references, and `start-issue` enforces them.

## Margincalc-specific guidance

For engine/CEL/rulebook issues:

- Preserve `Requirement`, `AppliedProceeds`, and `CashCall` semantics.
- Preserve first-match rule order.
- Add negative tests for validation/error behavior.
- Do not weaken CEL strictness.

For account aggregation issues:

- Reference `account-aggregator-plan.md`.
- Keep vendor API reconciliation out of scope unless explicitly requested.
- Lock down LMV/SMV sign conventions.
- Keep each issue small: types, market value, equity formulas, engine integration, validation, docs.

For reconciliation/vendor issues:

- If vendor API contract is unknown, write a discovery/design issue, not implementation.
- Do not expand CSV-specific account reconciliation unless the user asks.

## PR sizing rules

Each issue should fit one focused PR.

Split if it touches too many concerns:

- data model + formulas + engine integration + CLI + docs = too large
- one formula group + tests = good
- one validation layer + tests = good

## Style

- Use repo-relative paths.
- Be specific enough that `start-issue` can hand the file to an implementation session.
- No npm/frontend/Drizzle assumptions.

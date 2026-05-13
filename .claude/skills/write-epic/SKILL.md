---
name: write-epic
description: Create a margincalc engineering epic under docs/epics/<slug>/epic.md from a roadmap item, implementation plan, or feature idea. The epic is a coordination container that must split work into many PR-sized child issues.
---

# write-epic

This skill creates an epic for margincalc.

Correct harness model:

```text
Epic = coordination container
Issues = PR-sized implementation slices
start-issue = one child issue -> one worktree -> one PR
```

An epic must not be scoped as one giant PR.

## Inputs

Use the user's prompt plus relevant local docs:

- `roadmap.html`
- `README.md`
- `CLAUDE.md`
- `cel-strictness-plan.md`
- `fixes-1-2-plan.md`
- `account-aggregator-plan.md`
- any other plan in repo root

If the requested scope is ambiguous, propose 2-3 possible epics and ask which one to draft.

## Research

Before writing, inspect current code and docs. Use `.claude/agents` where useful:

- `codebase-locator` to find files.
- `codebase-pattern-finder` to find existing implementation/test patterns.
- `codebase-analyzer` to explain current code paths.

Keep research repo-specific:

- Go packages under `internal/`
- CLI packages under `cmd/`
- rulebooks under `rules/`
- markdown plans and roadmap
- tests under `internal/**`

No React, npm, Drizzle, browser, or frontend assumptions.

## Output

Create:

```text
docs/epics/<slug>/epic.md
```

Create the directory if missing. Ask before overwriting.

## Epic format

```markdown
# <Epic Title>

## Deliverable

One sentence describing the shipped system capability.

## Context

Why this matters in margincalc. Link to relevant roadmap/plan files.

## Current Codebase State

What already exists, with paths.

## Scope

- Concrete in-scope deliverables.

## Out of Scope

- Explicit exclusions.

## Architecture

Small Mermaid diagram if useful.

## Data / API Model

Go structs, function signatures, rulebook fields, CLI shape, or API boundary.

## Validation and Test Strategy

How correctness will be proven.

## Issues

- **[N] Issue heading** — one-line context
  - Depends on: <sibling-slug> if there is a hard dependency

## Open Questions

- Questions that must be answered before or during implementation.
```

## Issue slicing rules

Make issues PR-sized. Prefer 3-8 child issues over one large issue.

Good slices:

- Add types and sign conventions.
- Implement one validation layer.
- Implement one formula group.
- Add integration with existing engine.
- Add docs after behavior exists.

Bad slices:

- "Implement account aggregator" as one issue.
- "Fix CEL strictness" as one issue if it includes parser, helpers, rulebook validation, and docs.

Complexity:

- `1` trivial
- `2` small
- `3` straightforward
- `5` meaningful single PR
- `8` large but still reviewable
- `13` too large; split before writing issues

## Style

- Concrete, testable, Go-specific.
- Use repo paths.
- Keep vendor API/reconciliation work out unless the vendor contract is known.
- Do not create implementation tasks inside the epic; child issues carry implementation detail.

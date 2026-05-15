---
name: grill-with-docs
description: Stress-test an epic plan by grilling open questions one-by-one, locking each decision into docs/architecture/decisions/<epic-slug>.md as it crystallises. Use after /write-epic and before /graduate-epic when an epic still has unresolved architectural questions.
---

# grill-with-docs

This skill resolves the open architectural questions in a margincalc epic by interviewing the user one question at a time, then writing the locked decisions into `docs/architecture/decisions/<epic-slug>.md` in the format already established by `layer3-house-overlay.md` and `declarative-rule-preconditions.md`.

It exists because `/write-epic` and `/write-issue` produce planning artifacts that often carry an `## Open questions` section. Those questions need locking before `/graduate-epic` cuts GitHub issues — otherwise sibling issues reference decisions that don't exist yet, and implementation drifts from the original plan.

## When to use

- The user says "grill me", "let's lock the open questions", "stress-test this plan", or similar.
- After `/write-epic` produced an epic with an `## Open questions` section.
- Before `/graduate-epic` — locked decisions get referenced in issue bodies and the umbrella description.

## When NOT to use

- The epic has no open questions and no fuzzy areas.
- The user is in implementation mode (decisions already locked, just shipping PRs).
- There's no epic file yet — run `/write-epic` first.

## Inputs

- Path to the epic file (typically `docs/epics/<slug>/epic.md`) OR the epic slug.
- If neither is given, list `docs/epics/*/epic.md` and ask which one.

## What to do

This skill does **two equally weighted things**:

1. **Spar with and educate the user** as decisions get made — push back on weak reasoning, surface tradeoffs they missed, teach the underlying concept when relevant. The goal is not just to capture a decision but to make sure the user understands *why* the locked decision is right (and what would have made another option correct under different constraints).
2. **Document decisions properly** — write each locked decision into `docs/architecture/decisions/<epic-slug>.md` in the established format, inline as it crystallises (don't batch).

Both happen in the same loop. Skipping #1 produces well-documented decisions the user can't defend three months from now. Skipping #2 produces good thinking that evaporates.

Interview the user relentlessly about every open question in the epic until each one is locked. Walk down the question list one-by-one, resolving dependencies between decisions as you go. For each question, provide your **recommended answer** with reasoning, then wait for the user to accept, redirect, or push back.

Ask one question at a time. Wait for feedback before moving to the next.

If a question can be answered by reading the codebase (existing types, function signatures, current behavior), read first and propose a grounded answer rather than a hypothetical one.

### Spar-and-educate mode

For each open question, your job is to be a sharper colleague — not a polite assistant.

- **Push back on weak reasoning.** If the user picks an option for a shallow reason ("A sounds easier"), challenge it. "Easier today, but it locks you out of multi-currency support — is that the tradeoff you want?" Don't accept the first answer if it doesn't survive a real prod.
- **Teach the concept underneath.** If the question turns on a concept the user hasn't met before (CRDTs, BCNF, LSM trees, idempotency tokens, etc.), pause and explain it briefly before asking which option they want. The decision should rest on understanding, not vibes.
- **Surface what they missed.** If there's a third option they haven't considered, name it. If a constraint they stated rules out their preferred option, point it out: "you said the override store needs SQL-style search, which means a key-value store like Redis can't be the primary backend even though it would simplify writes."
- **Steelman alternatives.** When you recommend option A, also state the strongest case for B. The user should reject B knowingly, not because you under-sold it.
- **Force concrete scenarios.** Vague questions stay vague. Invent a specific portfolio, a specific account state, a specific override conflict — make the user say what should happen. The answer becomes part of the locked decision.
- **Call out fuzzy language immediately.** If they say "the engine handles it," ask which engine — `engine.Rulebook`, `account.Aggregate`, or `overlay.Engine`? Margincalc is a layered system; loose layer naming wastes everyone's time downstream.

Educate by pointing at things the user can verify: "Look at how `forEachLeg` works in `internal/engine/env.go` — that's why this option requires a mutex change." Don't lecture in the abstract.

The user explicitly wants to learn from this skill, not just to capture decisions through it. If a session ends with the decisions doc updated but the user feels no smarter, the skill failed at half its job.

### How to structure each round

For each open question:

1. **Restate the question** in your own words so misinterpretations surface early.
2. **Lay out the alternatives** explicitly (typically 2-3 options labeled A / B / C).
3. **Recommend one** with the reasoning — what tradeoff you're picking and why.
4. **Ask** which the user prefers, or if they want to introduce a new option.
5. **On confirmation**, immediately write the decision into the decisions doc (don't batch).

### Sharpen fuzzy language

When the user uses vague or overloaded terms, propose precise canonical terms before continuing. Example: if they say "the engine handles it," ask whether they mean `engine.Rulebook`, `account.Aggregate`, or `overlay.Engine` — those are different things. Margincalc is a layered system and slop in layer naming wastes everyone's time downstream.

### Cross-reference with code

If the user states how something works, verify with `Grep` or `Read` before writing a decision that depends on it. If you find a contradiction, surface it immediately: "you said `engine.Leg` carries instrument-kind, but it doesn't — `overlay.SecurityFacts.InstrumentKind` does. Which is the right thing to depend on?"

### Discuss concrete scenarios

When a decision has subtle edge cases, invent a scenario that probes the boundary. Example for the layer3 D2 (`max` at group scope): "what happens when the group has zero per-position baseline — does `max` floor to the formula value, or to zero?" Force the user to be precise; the answer becomes part of the locked decision.

## Decisions file format

Write to `docs/architecture/decisions/<epic-slug>.md` using the format already established by the two existing files in that directory. Read one of them before writing if you're unsure.

Required structure:

```markdown
# Decisions — <epic title>

Resolved architectural decisions for the <epic title> epic ([#<umbrella issue>](<umbrella URL>)).

These decisions are locked. Future modifications must explicitly supersede them, not silently override.

## Context

<one paragraph: what the epic is, why these questions surfaced>

## D1 — <one-line decision summary>

**Decision:** <the actual decision in 1-3 sentences>

**Rationale:**
- <bullet points — why this over the alternatives>

**Impact on code:**
- <bullet points — what this means for the implementation>

## D2 — <next>

...

## Why these are locked

<the standard "re-litigating produces churn" boilerplate, see existing files>

## See also

<links to relevant code, rule files, umbrella issue>
```

The umbrella issue link can be omitted if `/graduate-epic` hasn't run yet — leave a `TODO(graduate-epic)` placeholder and the user will fill it after graduation.

Create the file lazily — only after the first decision is locked. Don't pre-create it with empty sections.

## When to offer creating an ADR-style decision

Only lock a decision into the file when all three are true:

1. **Hard to reverse** — changing your mind later is costly (rule shape, file format, type signature).
2. **Surprising without context** — a future reader will wonder "why this way?"
3. **The result of a real trade-off** — there were genuine alternatives and one was picked for specific reasons.

If a question doesn't meet all three, resolve it inline in conversation but don't write it to the decisions file. Examples that don't belong:
- Variable naming
- Whether to use `int` or `int32` (compiler-checked, easy to change)
- Test file organization (covered by existing conventions)

## After all questions are locked

When every open question has a corresponding D-section in the decisions file:

1. Tell the user: "All open questions locked. Decisions written to `docs/architecture/decisions/<slug>.md`."
2. Suggest the next step: `/write-issue` (if issue files don't exist yet) or `/graduate-epic` (if they do).
3. Remind them that issue bodies should reference the decisions doc — `/write-issue` will pick it up automatically if the file exists at the conventional path.

## Style

- One question at a time. Always.
- Recommend before asking. Don't dump 4 options without a recommendation.
- Lock decisions inline as they happen. Don't batch.
- Skip the glossary/CONTEXT.md pattern from generic versions of this skill — margincalc uses CLAUDE.md plus the architecture docs for that purpose.

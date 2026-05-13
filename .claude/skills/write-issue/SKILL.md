---
name: write-issue
description: Expand every issue heading in an epic into a full issue file. Reads the Issues list from docs/epics/<epic>/epic.md (migrating legacy flat docs/epics/<epic>.md if found), researches the codebase once via the codebase-locator / codebase-analyzer / codebase-pattern-finder agents, then fans out parallel write agents to produce one focused markdown file per issue at docs/epics/<epic>/<issue-slug>.md. Each issue file includes a mermaid architecture diagram, acceptance criteria, edge cases, files to touch, data model deltas, an approach narrative, and a TDD-ordered task list (tests-first; task details themselves are produced by a separate write-task skill). Use when the user has an epic drafted and wants its issues expanded into implementable units; fits the planning → epic → issues → tasks workflow with TDD as mandatory.
---

# write-issue

You take an epic's Issues list and produce one full issue file per item. Each issue file is the planning unit a *task-level* session reads to understand its slice of the epic; the implementation session itself reads task files, not issue files. The issue exists for the human planner and for the `write-task` skill that comes after.

This skill operates in **batch mode** with **parallel fan-out**: one invocation walks every issue in the named epic, runs a single shared codebase-research pass, then spawns parallel write agents — one per issue — to produce all files concurrently. Use `write-task` to drill from issue → task.

## When to use

- The user has a drafted epic at `docs/epics/<slug>/epic.md` (or the legacy `docs/epics/<slug>.md`) and wants its issues expanded.
- The user names an epic and asks for issues ("expand issues in `scaffold-auth-and-db`", "write the issues for chat capture").
- The user is mid-planning-session and wants to drive the epic → issue → task pipeline forward.

Do not implement anything. This skill produces planning markdown only. Do not expand task details — that's `write-task`.

## Inputs

The user names an epic. Resolve it as:

1. Direct slug: `scaffold-auth-and-db` → look for `docs/epics/scaffold-auth-and-db/epic.md`, then legacy `docs/epics/scaffold-auth-and-db.md`.
2. Tagline / topic ("chat capture", "auth"): list `docs/epics/`, propose 1–2 matching epics, confirm before proceeding.
3. Filter to a single issue ("just the [5] callback issue"): expand only the matching item from the epic's Issues list; still fan out (one agent), still update epic backlinks.

If the epic file is missing, stop and tell the user — don't invent an epic.

## File layout & migration

Canonical layout:

```
docs/epics/<epic-slug>/
├── epic.md              ← the epic doc
├── <issue-slug>.md      ← one per issue
└── ...
```

**Legacy migration.** If the epic is still a flat file (`docs/epics/<slug>.md`) from before `write-epic` was updated, migrate it before doing anything else:

1. Create directory `docs/epics/<slug>/`.
2. Move `docs/epics/<slug>.md` → `docs/epics/<slug>/epic.md`.
3. Continue with issue generation.

Subsequent invocations find the directory already in place; just write issue files alongside `epic.md`.

**Conflict handling.** If any target issue files already exist, list them and ask the user: overwrite, skip-existing, or abort. Do not silently overwrite.

After writing all issues, the parent flow updates `epic.md` Issues section so each bullet links to its issue file (`- **[3] [Wire intent classifier](wire-intent-classifier.md)** — ...`). Preserve original ordering, complexity, and context line; only wrap the heading text in a link. This is a serial step performed by the parent, not by the parallel write agents.

## Codebase research (one shared pass)

Run **one** comprehensive research pass per invocation, not one per issue. Findings get compressed into a summary and passed to every parallel write agent.

**Phase 1 (parallel):**

- **codebase-locator** — frame the prompt to span the epic's full scope ("Locate every file touching <epic area>: <concrete keywords from the epic's Description and Architecture sections>"). Ask it to report absences explicitly so greenfield areas are visible.
- **codebase-pattern-finder** — ask for conventions the new code should mirror across the epic's scope (route shape, DB table shape, server/browser module split, test scaffolding, etc.). Run alongside the locator.

**Phase 2 (sequential, only if locator found relevant files):**

- **codebase-analyzer** — pass the locator's relevant paths and ask how those pieces currently work. Skip entirely for greenfield epics.

**Persist findings as `docs/epics/<slug>/research.md`** — a real planning artifact alongside `epic.md`. The fan-out agents read this file instead of receiving a summary inline; that keeps each agent's prompt small and gives the human planner an auditable record of what was learned.

Use this structure:

```markdown
# Research: <epic title>

_Generated for `write-issue` on <YYYY-MM-DD>. Sources: codebase-locator, codebase-pattern-finder, codebase-analyzer (skip line if not run)._

## Codebase state

One short paragraph: is the epic's area greenfield, partially built, or fully present? Anchor with concrete file counts.

## Files in this area

Bulleted list of existing files relevant to the epic, each with a one-line role description. Group by sub-area (auth / db / routes / lib). Mark absences explicitly under a separate **Absent** subsection (e.g., "No `.env.example`", "No `app/db/` directory").

## Conventions to mirror

Bulleted list of patterns the new code must follow, each with a `file:line` reference. Cover at minimum: route file shape, path alias (`~/*` vs `@/*`), module split (server-only suffix, client/browser split), styling, type-generation. Quote a small snippet per pattern when it clarifies.

## How existing pieces work

(Omit this section if greenfield.) Per-component breakdowns from `codebase-analyzer`: entry points, key functions, data flow, configuration. File:line refs throughout.

## Test scaffolding

What test framework, if any, is wired up (vitest / playwright / supertest / none). Where test files live. How to add a new test. If none, note the gap — TDD is mandatory so the first issue in the epic likely needs to wire test infra before anything else.

## Gotchas

Bulleted list of non-obvious constraints discovered: cookie wiring quirks, RR7 typegen ordering, migration apply path, env-var exposure rules, version-specific API shapes. Each gotcha should be actionable, not just descriptive.
```

If `research.md` already exists from a prior run, ask the user before overwriting (offer: refresh from agents / reuse existing / abort). Default to refresh — codebases drift.

Pass only the **file path** (`docs/epics/<slug>/research.md`) to each fan-out agent. Do not embed the file contents in agent prompts.

## TDD scope — behavior, not declarations

TDD is mandatory for **behavior**, never for **declarations**. A declaration is anything TypeScript or the framework already validates without your code running. Tests on declarations are tautological — they assert what the compiler already enforces — and they're the single biggest waste pattern in TDD-mandated agent work.

**Exempt from tests, acceptance criteria, and edge cases:**

- Type/interface definitions, type aliases, generics
- Drizzle table definitions (the table shape itself — *not* migrations, *not* query helpers)
- Static config files: `drizzle.config.ts`, `vite.config.ts`, `tsconfig.json`, `components.json`, env templates
- Constants, enums, re-exports, barrel files
- Mermaid diagrams, markdown, docs

**Banned tests (concrete examples):**

- "Test that the `User` type accepts a string for `email`" — TS does this at compile time.
- "Test that the schema allows empty string in `display_name`" — the type already says so.
- "Test that `COLORS.primary === '#abc'`" — testing the file's own literal.
- "Test that `drizzle.config.ts` exports an object" — config loads or it doesn't; the build is the test.

**Still tested:** anything with runtime behavior — functions, queries, migrations (DB side effects), route handlers, validators with rules (lowercase email, max length), transformations, error paths, RLS policies.

**Decision heuristic:** "Would a future change to this code break something a TS error / build failure wouldn't catch?" Yes → test it. No → skip and say so in Approach → Test strategy.

**For declaration-only issues:**

- **Acceptance criteria** collapses to a single bullet: "File exists; `npm run typecheck` and `npm run build` both pass."
- **Edge cases** becomes "None — declaration only."
- **Tasks** have no test step; sizing reflects implementation only.
- **Approach → Test strategy** says "No tests — declaration only" with a one-line justification.

If an issue mixes declaration work with behavior (e.g., schema + a query helper), split it: the declaration is one task with no tests, the helper is another task with TDD.

## Issue file structure

Each issue file contains these sections, in order. Sections marked *optional* may be omitted only when the rule allows.

### 1. Title (H1)

Slugified heading from the epic's Issues list, in human-readable form. e.g. epic line `**[3] Wire intent classifier (heuristic only)**` → title `Wire intent classifier (heuristic only)`, slug `wire-intent-classifier.md`.

### 2. Epic

One-line backlink: `Epic: [<epic title>](epic.md) — complexity **[N]** in the epic's Issues list.`

### 3. Dependencies

Mandatory section. Lists sibling issues in the same epic that **must** be shipped before this issue can start. The downstream `start-issue` skill parses this section and refuses to create a worktree if any listed dependency is still open on GitHub.

Format:

```
## Dependencies

- `define-blood-work-tables` — this issue modifies `app/db/schema/safe-values.ts`, which that issue creates.
- `wire-intent-classifier` — tagger call depends on intent routing being live.
```

Rules:

- Reference siblings by their **slug only** (no `.md` extension, no path). `graduate-epic` rewrites these to GitHub `#N` refs in pass 3, and `start-issue` reads the rewritten `#N` to check state.
- One bullet per dependency. Include a short justification — what concrete artifact this issue needs from the sibling. "Cannot start without X" not "logically follows X".
- If the epic's Issues list included a `Depends on:` sub-bullet for this issue, copy those slugs in (and add justifications). Don't drop any.
- If there are no hard dependencies, write the single line `None.` — do not omit the section. The empty-but-present form is what `start-issue` matches on to confirm the section was considered.
- Only list **hard** dependencies (compile/runtime/data needs). Don't list siblings that are merely thematically related or that you'd "rather" had shipped first.

### 4. Summary

One sentence. What this issue ships. Concrete and observable.

### 5. Description

2–3 sentences. Why this issue exists, what it unblocks within the epic, which spec sections it derives from. Cite spec sections inline (`§5.2`, `§7.2`).

### 6. Architecture

A mermaid diagram. **Mandatory unless the issue genuinely touches a single file with no flow** — diagrams are how a task session orients itself before reading any code. Show the components/data/control flow the issue introduces. Scope: this issue only, not the whole epic.

Diagram type choice (same conventions as `write-epic`):

- `flowchart` — components and data flow
- `sequenceDiagram` — request lifecycles, agent runs, cookie handoffs, OAuth dances
- `erDiagram` — table additions or significant column work

Distinguish existing vs new via `classDef` (existing = dashed grey, new = filled). Source the "existing" set from the codebase research summary.

If you genuinely skip the diagram, write one sentence in this section explaining why (e.g. "Single-file config change; no flow to draw").

### 7. Files to touch

Bulleted list of files. Annotate each as `new` or `modify`. Source from the codebase research summary.

```
- `app/db/client.ts` — **new**
- `app/db/schema.ts` — **modify** (add `users` table)
- `package.json` — **modify** (declare `drizzle-orm`, `postgres`)
- `drizzle.config.ts` — **new** (repo root)
- `app/db/client.test.ts` — **new** (TDD: written before client.ts per task ordering)
```

For non-code artifacts (SQL migration, env-config change, Supabase dashboard step), describe the artifact explicitly.

### 8. Data model deltas (optional)

Schema fragments this issue introduces or modifies. Pull verbatim from `docs/spec.md §5.2` where applicable. Omit the section entirely if the issue doesn't touch persistence.

### 9. Approach

Narrative — 2–6 short paragraphs or a tight bulleted list. Covers:

- **Design choices** the issue requires (where state lives, sync vs async, error path strategy). Name the choice the issue is making and the alternative discarded.
- **APIs / patterns to use**, citing pattern-finder findings (`route file shape from app/routes/home.tsx`, `cookie callbacks per @supabase/ssr docs`).
- **Test strategy** — what level of test covers each acceptance criterion (unit / integration / e2e), what fixtures are needed, what gets mocked vs hit real. TDD is mandatory in this codebase, so this section is not optional.
- **Gotchas / non-obvious constraints** discovered during research (`@supabase/ssr requires response-side header writes via the action's headers object — not via response interceptors`).

This section is the bulk of the issue's value. A future task session can lean on it instead of re-deriving the design.

### 10. Acceptance criteria

Numbered list. Each criterion is a **testable assertion** the issue must satisfy, written as `Given ... When ... Then ...` or as a direct invariant. Every criterion must be expressible as an automated test — that's the gate for whether it belongs here. See **TDD scope** above for the declaration-only exemption (collapses this section to a single build/typecheck bullet).

```
1. Given a valid `DATABASE_URL` in `.env`, when `import { db } from "~/db/client"` is evaluated, then it exports a Drizzle instance whose `.execute(sql\`select 1\`)` returns `[{ "?column?": 1 }]`.
2. Given a missing `DATABASE_URL`, when the client module loads, then it throws with a clear "DATABASE_URL is required" message (not a generic undefined error).
3. Given the `users` table migration is applied, when an unauthenticated `select * from users` runs, then RLS returns zero rows.
```

Each criterion in this list maps to at least one task in section 12. The mapping is implicit (don't number-link them) but the coverage must be complete.

### 11. Edge cases

Bulleted list of failure modes, weird inputs, race conditions, and degraded-environment behaviors the implementation must handle. Each edge case implies at least one test (often a negative test).

```
- Magic-link code reused after a successful exchange → second call returns an "already used" error; loader redirects to /auth/login with a flash message.
- `DATABASE_URL` points at a DB without the `users` table (fresh Supabase project) → migration runner produces a clear failure, not a silent crash on first request.
- Two concurrent magic-link callbacks for the same user on a new account → `users` upsert is idempotent; only one row exists afterward (`on conflict do nothing` or `on conflict update`).
- Cookie set in `auth.callback` is read by a load on `/` mounted in the same response → header writes propagate (regression risk in `@supabase/ssr` cookie wiring).
```

If after thinking through the issue you genuinely cannot identify edge cases, write "None obvious — flag any encountered during implementation." (This should be rare; most issues have at least 2–3.)

### 12. Tasks (TDD-ordered)

Bulleted list. Same format as the epic's Issues list — complexity in brackets, imperative heading, one-line context. **Do not expand task details** — `write-task` handles that.

**TDD ordering is mandatory for behavior-bearing tasks.** Each behavior task names a behavior, not a file. Within each task, the first step (handled later by `write-task`) is writing a failing test for the behavior; only after the test exists does the implementation. The task's complexity score covers test + implementation + refactor for that behavior.

**Declaration-only tasks** (per **TDD scope** above — type definitions, table shapes, config files, constants) skip the test step entirely. Name the task by the artifact (`Define users table in schema.ts`) and size for implementation only. Don't pretend-test a declaration to satisfy the TDD rule.

Order tasks so that for any pair where task B depends on task A's behavior, A comes first. Prefix parallelizable tasks with `‖`.

```
- **[1] Drizzle config is loadable and points at the schema** — `drizzle.config.ts` at repo root; test: `npx drizzle-kit generate --dry-run` exits 0
- **[2] DB module exports a working Drizzle client** — `app/db/client.ts`; test: round-trip `select 1` via the exported `db` returns 1
- **[2] DB client fails loudly on missing `DATABASE_URL`** — same module; test: setting `DATABASE_URL=""` throws the documented error
- **[3] `users` table exists with self-row RLS** — migration + policy; test: insert as user A, query as user B, expect 0 rows
- **‖ [1] `.env.example` lists every required key** — test: every key referenced in `app/lib/*.server.ts` appears in `.env.example`
```

Task complexity scale (Fibonacci — narrower than the epic's because tasks are smaller):

- **1** — one behavior with one or two assertions; trivial implementation. Minutes.
- **2** — one behavior with a few assertions; one design decision. Under an hour.
- **3** — a sub-feature inside the issue with multiple assertions. A sitting.
- **5** — likely too big — split into two behaviors before starting.

If any task is ≥ 5, flag it inline and propose a split.

### 13. Out of scope

Bulleted list of things adjacent to this issue that *don't* belong in it. Often points at sibling issues in the same epic or at later epics. Helps the task session resist drift.

### 14. Open questions (optional)

Decisions the issue can't make on its own. Pull from the epic's Open questions where relevant, or surface new ones discovered during research. Omit the section if there are none.

## Parallel execution (fan-out)

After the shared research pass and conflict check, **spawn parallel write agents — one per issue — in a single tool-use block**. The fan-out is the heart of this skill's speed:

1. **Compose a small per-issue prompt** for each item in the Issues list. The prompt contains:
   - Path to the epic file (`docs/epics/<slug>/epic.md`) — agent reads for context.
   - Path to the research file (`docs/epics/<slug>/research.md`) — agent reads for codebase grounding (file paths, conventions, gotchas).
   - The exact issue bullet line from the epic, with complexity, heading, context.
   - Path the agent must write to: `docs/epics/<slug>/<issue-slug>.md`.
   - Instruction to read `.claude/skills/write-issue/SKILL.md` for the issue structure spec, then write the file using exactly that structure.
   - Instruction: do not modify any other file, do not re-run codebase agents (use `research.md` instead), do not run code, do not spawn further agents.

2. **Use the `general-purpose` agent type** for each fan-out call (it has Write access; the codebase-* agents are read-only).

3. **Place all N Agent calls in a single assistant message** so they run concurrently. With 12 issues, this is 12 parallel agents — wall-clock ≈ max(per-issue time), not sum.

4. **Do not perform shared-state writes inside the agents.** Each agent writes exactly one file, the file for its own issue. The parent flow (not the agents) updates `epic.md` backlinks after all agents return.

5. **Collect results.** Each agent returns a confirmation (path written, plus a one-line summary). If any agent fails, surface the failure with the issue bullet so the user can re-run that one.

If the user requested filter-to-single-issue, still use this same pattern with one agent — same code path, one slot used.

## Process

1. **Resolve the epic.** Find its `epic.md` (or legacy `<slug>.md`). If absent, stop.
2. **Migrate if needed.** If legacy flat file, move it to `docs/epics/<slug>/epic.md` before any other writes.
3. **Read the epic in full.** Capture the Issues list verbatim — each item's complexity, heading, and context.
4. **Research the codebase once and persist it.** Run locator + pattern-finder in parallel; run analyzer only if locator found relevant files. Frame prompts in terms of the *whole epic's scope*. Synthesize findings into `docs/epics/<slug>/research.md` using the structure in the *Codebase research* section. If `research.md` already exists, prompt before overwriting.
5. **Check for conflicts.** If any target issue files already exist, list them and ask the user how to proceed before drafting.
6. **Fan out.** Spawn one `general-purpose` agent per issue in a single tool-use block. Each agent reads `epic.md` and `research.md` for context, then writes its own issue file using the structure above.
7. **Update the epic's `epic.md`.** Edit the Issues section so each bullet wraps its heading in a markdown link to its issue file. Keep ordering, complexity, and context line intact.
8. **Report back:** issue files created (with paths), total task count across all issues, sum of task complexities, any agents that failed, and any open questions surfaced.

## Style

- Concrete > vague. Use the spec's exact field names, table names, route paths, role names.
- Single-user solo build — no "we will", "the team will", "stakeholders".
- No estimates in time units. Complexity is Fibonacci.
- TDD is mandatory: every acceptance criterion is testable, every task names a behavior, tests precede implementation within each task.
- No Risks / Mitigations / Stakeholders / Owners sections. Summary + Acceptance criteria + Edge cases + Out of scope + Open questions cover this.
- Each issue file stands alone — assume the reader has the parent epic open in another tab, but don't make them flip back for basic context.
- No emojis unless the spec uses them.

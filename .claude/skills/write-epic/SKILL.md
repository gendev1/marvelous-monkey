---
name: write-epic
description: Convert a section of docs/spec.md into an epic file at docs/epics/<slug>.md. Use when the user asks to scaffold/plan/draft an epic, or wants to break a piece of the spec into a planning unit. Produces headings-only issues sized by Fibonacci complexity — do not write full issue details (a separate skill handles that).
---

# write-epic

You produce one epic file. The epic is the planning unit between the spec and individual issues/tasks in this user's workflow (planning session → epic → issues → tasks; fresh implementation session per task). Implementation sessions read the issues/tasks files; they do not read the epic. The epic exists for the human planner.

## When to use

- The user asks for an epic ("write the epic for visit prep", "draft an epic for chat capture", "epic for v0 admin CRUDs").
- The user points at a section of `docs/spec.md` and wants it scoped into a planning unit.
- The user wants to start the planning side of the session-per-task workflow.

Do not use to implement anything. This skill produces planning markdown only.

## Inputs

Resolve the target epic from the user's prompt:

- **Feature name** ("visit prep", "chat capture") → locate the corresponding spec section(s).
- **Spec reference** ("§5", "§7.2 visit_prep", "data model") → use that section as the spine.
- **Build-order step** ("step 5 of §11", "the chat surface step") → epic = that step plus any tightly coupled following steps. Don't span across version boundaries (v0 → v0.5 → v1) unless the user asks.

If the user's intent is ambiguous (multiple spec sections match, or the scope could reasonably be one epic or three), surface 2–3 candidate epics with one-line summaries and ask which they want. Don't guess silently.

## Codebase research (required before drafting)

The codebase is not a blank slate — parts of the spec may already be partially implemented. Before drafting, use the three subagents in `.claude/agents/` to ground the epic in current reality, so you don't propose work that's already done and so new issues follow established patterns.

Spawn agents via the Agent tool. Phase 1 runs in parallel; phase 2 depends on phase 1's locator output, so it runs after.

**Phase 1 (parallel):**

- **codebase-locator** — find every file/dir relevant to the epic's area. Frame the prompt in concrete terms: "Locate files related to authentication (magic link, session handling)." Don't ask it to analyze; only locate.
- **codebase-pattern-finder** — find existing patterns the epic's code should mirror (route-handler shape, Drizzle table conventions, component file layout, error handling, RLS policies). Run alongside the locator.

**Phase 2 (sequential, after locator returns):**

- **codebase-analyzer** — only invoke if the locator found files relevant to the epic. Pass it the specific paths and ask how that code currently works. Skip the analyzer entirely for greenfield epics where the locator returns "no implementation found."

When the project is essentially greenfield in the epic's area, note that explicitly in the epic's **Description** ("No prior implementation; this epic is greenfield.") and don't fabricate prior art.

## Output

One file: `docs/epics/<verb-led-slug>/epic.md`.

- Each epic lives in its own directory. The epic doc itself is always named `epic.md`. Sibling issue files (created later by `write-issue`) live alongside it as `<issue-slug>.md`.
- Slugs are kebab-case, verb-led — an epic is something you _do_: `wire-chat-capture`, `ship-visit-prep`, `ingest-lab-pdfs`. Not `chat-capture` or `visit-prep-system`.
- Create `docs/epics/` and `docs/epics/<slug>/` if they don't exist.
- If `docs/epics/<slug>/epic.md` already exists, ask before overwriting.
- Legacy flat files (`docs/epics/<slug>.md`) from earlier runs: do not silently migrate them here — `write-issue` handles the migration when it runs against them.

## Epic structure

Every epic file contains these sections, in this order. Sections marked _optional_ may be omitted only when the rule allows.

### 1. Title (H1)

The slug as a human-readable title. Optional one-line tagline in italics underneath.

### 2. Deliverable

One sentence. The thing the epic ships when done. Concrete and observable, not aspirational.

- Good: "User can type into the chat input and a parsed event-card returns within 3s."
- Bad: "Chat works." / "Visit prep is ready."

### 3. Expectations from the user

Bulleted prerequisites the user must complete _before_ starting work on the epic. Not time-bound — these are state conditions, not deadlines.

Examples of what belongs here:

- Infra provisioned: "Hetzner CCX23 reachable via SSH."
- Credentials configured: "`ANTHROPIC_API_KEY` in `.env`; `DATABASE_URL` points at the Supabase project."
- External resources: "Supabase project created; magic-link auth tested."
- Decisions resolved from `§13 Open questions`: "Tagger sync-vs-async decided (currently `§13.1`)."
- Other epics completed: "Epic `wire-chat-capture` shipped."

**Use the codebase research to subtract prereqs that are already in place.** If `drizzle.config.ts` exists, don't list "set up Drizzle." If `app/db/schema.ts` already defines a table, don't list "add migrations infra." Only list what the user actually still has to do.

If there are no prerequisites, write "None."

### 4. Description

2–4 sentences. What the epic is for, why it exists, what it unblocks next. Cite the spec sections it derives from inline (e.g., "Implements `§7.2 visit_prep` and the `agent_runs` writes from `§5.2`.").

### 5. Architecture

A mermaid diagram showing the components / flows / data introduced or modified by this epic. Scoped to what the epic touches — not the whole system.

Choose the right diagram type:

- `flowchart` — components and data flow
- `sequenceDiagram` — agent runs, request lifecycles, streaming pipelines
- `erDiagram` — new tables or significant column additions

**Distinguish existing components from new ones.** Use `classDef` in mermaid so the planner can see at a glance what's already built vs what this epic adds:

```
classDef existing fill:#eee,stroke:#888,stroke-dasharray:5 5;
classDef new fill:#dff,stroke:#066;
class AuthRoute,DrizzleSchema existing;
class ChatStream,TaggerCall new;
```

Source the "existing" set from the codebase-locator's findings. If the area is fully greenfield, skip the `classDef` and show only new components.

If the diagram would be trivial (one box, no edges), skip it and write a one-sentence description instead.

### 6. Data models

Schemas this epic introduces or modifies. Pull verbatim from `docs/spec.md §5.2` (Drizzle definitions). Show only the relevant tables/columns; do not dump the entire schema.

Format options:

- Quote the relevant Drizzle table block from the spec.
- For new fields on an existing table, show the diff-like fragment with a comment marking what's new.
- For changes to `structured` JSON shapes, show the TypeScript shape.

**Cross-reference against the codebase.** If the locator/analyzer found an existing schema file, annotate which tables/columns are already implemented vs still to add:

```
users   ✓ in app/db/schema.ts
events  ✗ new
states  ✗ new — depends on users
```

If the epic doesn't touch persistence, write: "None — this epic doesn't introduce or modify persistence."

### 7. Issues

Bulleted list. Each item is: complexity score, heading, one-line context. **Do not expand into full details** — a separate skill handles that.

Format:

```
- **[3] Wire intent classifier (heuristic only)** — regex/keyword router for log vs query; no LLM fallback yet
- **[5] Implement tagger Sonnet call** — single-call structured output, returns `structured` + `child_events`
  - Depends on: `wire-intent-classifier`
- **[8] SSE streaming endpoint for chat responses** — POST /api/chat, forwards text + tool results to the client
  - Depends on: `implement-tagger-sonnet-call`
- **‖ [2] Event-card React component** — renders parsed event with edit affordance; parallelizable with the SSE work
```

**Dependencies (optional sub-bullet).** When an issue can only proceed after a sibling issue in the same epic ships, add a `Depends on:` sub-bullet listing sibling slug(s). This is a hint at the epic level; `write-issue` promotes it into a mandatory `## Dependencies` section inside each issue file, and `graduate-epic` rewrites the slugs to GitHub `#N` refs so `start-issue` can enforce them. Only list **hard** dependencies (the issue cannot start without the sibling's deliverable existing); don't list mere "logically related" siblings.

Complexity scale (Fibonacci — no time units):

- **1** — trivial; minutes (config tweak, single migration line)
- **2** — under an hour (one focused function, well-understood)
- **3** — one sitting; no surprises (a CRUD route, a wired component)
- **5** — a sitting or two; real design choices to make
- **8** — multi-sitting; needs careful design before coding
- **13** — should probably be split before starting — call this out explicitly

Conventions:

- Order issues in recommended implementation order.
- Prefix parallelizable issues with `‖`.
- Heading is imperative and specific. "Wire intent classifier" not "Intent classification".
- One line of context is enough to make the issue findable later. The full content is the next skill's job.
- **Cite the pattern to mirror** when the codebase-pattern-finder surfaced one — e.g., "...follows the route-handler pattern at `app/routes/auth.ts`". Keeps the implementation session aligned without re-deriving conventions.
- **Drop issues for work already shipped.** If the codebase-analyzer shows a piece is already implemented, omit the issue (or, if it's worth noting, list it as a completed reference under Out of scope, not in Issues).

### 8. Out of scope

Bulleted list of things a reader might reasonably think belong in this epic but don't. Be specific; cite `§2 Non-goals` or other epics where relevant. Also list work that the codebase already has so a planner doesn't re-scope it.

- "Voice input — handled in epic `add-voice-capture`."
- "Whisper transcription — v0.5+, not this epic."
- "Push notifications — anti-goal per `§8`."
- "Drizzle scaffolding — already in `app/db/`, see `drizzle.config.ts`."

If you can't think of anything that might be confused as in-scope, write "None obvious."

### 9. Open questions (optional)

Bulleted list of unresolved decisions blocking or shaping the epic. Pull from `§13` of the spec where relevant; surface new ones discovered while scoping. Omit the section if there are none.

## Process

1. **Understand the target.** Read the relevant `docs/spec.md` section(s) plus `§13` (open questions) and `§5.2` (schema, if data models are involved). Read existing files in `docs/epics/` for naming consistency.
2. **Research the codebase.** Run the agent passes described in _Codebase research_ above — locator + pattern-finder in parallel, then analyzer if there's anything to analyze. Capture: which files already exist in the epic's area, which conventions to mirror, what's already wired up.
3. **Resolve scope.** If still ambiguous after research, ask the user before drafting.
4. **Draft each section.** Pull verbatim from the spec where possible — especially data models and field names. Use research findings to (a) subtract prereqs that are already done, (b) drop issues for already-shipped work, (c) cite patterns the new code should mirror, (d) distinguish existing from new components in the architecture diagram.
5. **Write** the file to `docs/epics/<slug>/epic.md`.
6. **Report back:** file path, deliverable line, issue count, total complexity sum. Also note: "Found `<N>` existing files in this area; `<M>` issues skipped because already shipped" (or "Greenfield — no prior implementation").

## Style

- Concrete > vague. Use the spec's exact field names (`occurred_at`, `state_snapshot`, `structured`), table names, route paths, and role names (`tagger`, `visit_prep`).
- Single-user solo build — no "we will", "the team will", "stakeholders".
- No estimates in time units. Complexity is Fibonacci.
- No Risks / Mitigations / Stakeholders / Owners / Acceptance Criteria sections. The Deliverable line + Out of scope + Open questions cover this.
- No emojis unless the spec itself uses them.

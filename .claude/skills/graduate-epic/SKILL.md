---
name: graduate-epic
description: Promote a fully-planned epic from local markdown to GitHub Issues. Reads docs/epics/<slug>/ (epic.md + research.md + N issue files), creates one GitHub Issue per issue file with sibling cross-references resolved to #N refs, creates an umbrella issue holding the epic narrative with a checklist of all child issues, then prompts to delete the local directory once GitHub becomes the source of truth. Use when the user says graduate / promote / publish / push an epic, names an epic as ready to leave planning, or asks to convert local planning markdown into GitHub Issues. Single-user, gh-CLI based, idempotent on retry via a tiny state file.
---

# graduate-epic

You promote one epic's planning markdown to GitHub Issues, then delete the local files. The skill is the bridge between the planning workflow (local markdown) and the implementation workflow (GitHub Issues are the source of truth; implementation sessions read them).

The epic is a coordination container, not an implementation unit. Graduation creates one umbrella tracker issue plus many child issues. The umbrella issue is never implemented directly; each child issue should be small enough to become one readable PR.

This skill **mutates external state** — it creates GitHub Issues that can't be cleanly undone. Run preflight checks, two-pass for cross-refs, and never delete local files until every GitHub creation has succeeded.

## When to use

- User asks to graduate / promote / publish an epic ("graduate `account-aggregator` to github").
- User says planning is done for an epic and wants to move on to implementation.
- User explicitly says "push the epic to github" or "create issues from this epic".

Do not use this skill mid-planning. The epic and every issue must already be drafted as local files.

## Inputs

User names the epic. Resolve via `docs/epics/<slug>/`. If the directory or `epic.md` is missing, stop.

If a `.graduated.json` state file exists in the directory, this is a retry — read it to know which issues are already created and skip those.

## Preflight (stop on any failure)

Run these checks before touching GitHub. Any failure → print the specific fix and stop.

1. **Working tree clean for the epic dir.** `git status --short docs/epics/<slug>/` — if there are uncommitted modifications, warn and ask before continuing (the user might be mid-edit; graduating now would publish unfinished content).
2. **GitHub remote configured.** `git remote get-url origin` returns a `github.com` URL. If missing, tell the user to run `gh repo create` or add a remote.
3. **gh CLI authenticated.** `gh auth status` exits 0. If not, prompt them to `gh auth login`.
4. **Epic structure valid.** `docs/epics/<slug>/epic.md` exists; at least one `<issue-slug>.md` sibling exists.
5. **Read the epic.md Issues section.** Parse out each issue's complexity, heading, context, and the corresponding `<issue-slug>.md` filename. Confirm every referenced filename exists on disk; warn if there's a mismatch (issue listed but file missing, or file present but not listed).

## Conversion model

| Local artifact | GitHub artifact |
| --- | --- |
| `<issue-slug>.md` | One PR-sized GitHub child Issue. Title = issue file's H1. Body = issue file content minus the H1. Labeled `epic:<slug>`. |
| `epic.md` | One **umbrella** tracker Issue. Title = epic title. Body = epic.md content with the Issues section rewritten to a checklist of `#N` child refs. Labeled `epic:<slug>` and `umbrella`. Pinned (`gh issue pin`). Do not implement this issue directly. |
| `research.md` | Not graduated. Internal scratch; gets deleted with the rest. |
| Tasks inside issue files | Stay inline as part of the issue body (checklist or sub-list — preserved as-written from the issue file). They are not separate GitHub Issues. |
| Cross-issue refs in issue bodies (`[market-value-buckets](market-value-buckets.md)`) | Rewritten to `#N` after pass 1 captures issue numbers. |

**Labels (auto-create idempotently):**

- `epic:<slug>` — one per graduated epic. Color: pick any consistent one (e.g., `#0e8a16`).
- `umbrella` — only on the umbrella issue. Color: `#5319e7`.

Use `gh label create ... || true` so re-runs don't error on existing labels.

## Two-pass GitHub creation

Cross-references between issue files can only be resolved after every child issue has been assigned a number. So creation is two passes.

**Pass 1 — create children.** For each `<issue-slug>.md` (in the order they appear in epic.md's Issues list):

1. Read the file.
2. Strip the leading H1 → use it as the title.
3. Body = remaining file content. **Leave cross-issue markdown links as-is for now** — they get rewritten in pass 3.
4. `gh issue create --title "<title>" --body-file <tempfile> --label "epic:<slug>"`.
5. Capture the issue number from the URL `gh` prints.
6. Persist `{slug: '<issue-slug>', number: N, url: '...'}` to `docs/epics/<slug>/.graduated.json` immediately after each success. This is the resumption file.

If a `.graduated.json` already lists an entry, skip the creation (idempotent retry).

**Pass 2 — create umbrella.**

1. Read `epic.md`. Strip H1 for title.
2. Find the `## Issues` (or numbered `## 7. Issues`) section. Replace its bullet list with a checklist where each item references the child by `#N`:

   ```
   - [ ] #42 — **[1] Define account snapshot types** — account, position, and balance input structs
   - [ ] #43 — **[2] Implement market value buckets** — LMV, SMV, equity, and adjusted balance calculations
   ```

   Preserve complexity, heading, context line, and the `‖` parallelization marker if present.
3. `gh issue create --title "<epic title>" --body-file <tempfile> --label "epic:<slug>,umbrella"`.
4. `gh issue pin <number>` to pin it.
5. Append the umbrella entry to `.graduated.json`.

**Pass 3 — rewrite cross-refs.** For each child issue whose body contained markdown links to sibling issue files (`(market-value-buckets.md)`, `[some-issue](some-issue.md)`, `Depends on: define-account-types.md`) or bare sibling slugs in the **`## Dependencies`** section:

1. Build a `slug → #N` map from `.graduated.json`.
2. Rewrite each link: `[anything](some-slug.md)` → `#N`, and bare `some-slug.md` mentions → `#N`. The text-side of the link can be dropped; GitHub auto-renders `#N` with the issue's title.
3. **In the `## Dependencies` section** specifically, each bullet starts with a bare slug in backticks: `` - `define-users-schema` — justification ``. Rewrite the backticked slug to `#N` (drop the backticks): `- #N — justification`. The downstream `start-issue` skill parses this exact `- #N` form to enforce dependencies. If a Dependencies bullet's slug is **not** in the `slug → #N` map (i.e., the named sibling isn't part of this epic), leave it as-is and surface a warning — that's a planning error the user should see.
4. `gh issue edit <child-number> --body-file <updated-tempfile>` only for issues whose body actually changed (skip the rewrite for issues with no sibling refs).

## Confirmation & cleanup

After all three passes succeed, print a summary:

```
✓ Created umbrella issue: <URL>  (#54)
✓ Created 12 child issues: #42–#53
✓ Cross-refs resolved in 4 issues
✓ Labels: epic:account-aggregator, umbrella

Local files at docs/epics/account-aggregator/ are now redundant with GitHub.
Delete them? [y/N]
```

If user confirms:

1. `rm -rf docs/epics/<slug>/` (the whole directory: epic.md + research.md + issue files + .graduated.json).
2. Print: "Deleted. Remember to commit: `git add -A docs/epics && git commit -m 'graduate <slug> to github'`."

If user declines:

- Leave the directory in place including `.graduated.json`. Print: "Local files preserved. Re-invoke `graduate-epic <slug>` with `--cleanup-only` to delete later. The state file ensures no double-creation."

Do **not** auto-commit. The user controls when the deletion lands in git history.

## Failure handling

- **Partial pass-1 failure** (some children created, then rate limit / network error): `.graduated.json` already records the successes. Re-invoking the skill picks up where it left off — skip already-created issues, continue from the next.
- **Pass 2 fails (umbrella creation):** all children exist but no umbrella. Re-invoke; the skill detects children in `.graduated.json` and only retries the umbrella + cross-refs.
- **Pass 3 fails (cross-ref rewrite):** umbrella exists; some child bodies are stale. Re-invoke; skill detects which children need re-editing by re-running the rewrite check and only patches those.
- **Any creation succeeded but the user wants to abort:** they can manually `gh issue close <N> --reason "not planned"` for each created issue and `rm docs/epics/<slug>/.graduated.json` to reset. Don't add an `--undo` flag; closing issues is GitHub's job, not this skill's.

## Cleanup-only mode

If the user invokes `graduate-epic <slug> --cleanup-only`:

- Read `.graduated.json` to confirm GitHub artifacts exist.
- Spot-check one issue URL with `gh issue view <N>` to confirm it's reachable.
- Then prompt + delete as in the confirmation flow above.

If `.graduated.json` is missing, refuse — there's no record of GitHub creation and deleting would lose the planning content.

## Process

1. **Resolve the epic.** Find `docs/epics/<slug>/epic.md`. If absent, stop.
2. **Preflight.** Run all five checks. Stop on any failure with a specific fix message.
3. **Plan.** Print the conversion summary ("Will create 12 child issues + 1 umbrella, labels `epic:<slug>` and `umbrella`. Proceed? [y/N]") and wait. This is the single creation-side confirmation; cleanup gets its own later.
4. **Pass 1: children.** Create each child issue, persist state per success.
5. **Pass 2: umbrella.** Create + pin the umbrella with rewritten Issues checklist.
6. **Pass 3: cross-refs.** Rewrite + edit any child bodies that link to siblings.
7. **Confirm cleanup.** Print summary, ask, then delete or preserve.
8. **Report:** URLs of every created issue (with the umbrella first), label names, and the cleanup outcome.

## Style

- Use `gh issue create --body-file <tempfile>` (not `--body "string"`) to preserve markdown formatting and avoid shell-escaping problems. Write the body to a temp file in `/tmp/`, pass the path, delete after.
- Don't try to be cute with `gh api` if `gh issue create` does the job. Stick to `gh` subcommands wherever possible.
- All gh commands run against the current repo's remote — don't accept a `--repo` override (would let a user accidentally publish to the wrong repo).
- Confirm-prompt copy is plain and direct: "Delete? [y/N]" — not "Are you sure you'd like to proceed with deletion?".
- Don't graduate more than one epic per invocation. Single epic, single mental model.

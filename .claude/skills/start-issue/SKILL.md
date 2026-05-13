---
name: start-issue
description: Pick a graduated GitHub issue, set up a git worktree + branch for it, write a self-contained TASK.md briefing inside that worktree, and print the copy-paste command to launch the implementation session. The launched session reads TASK.md, implements end-to-end, runs typecheck + tests, commits, pushes, and opens a PR with `Fixes #N` so merging the PR auto-closes the issue. Use when the user is ready to start work on a graduated issue ("start #42", "implement issue 42", "kick off the wire-drizzle issue", "pick up the next issue"). Single-user, gh-CLI based, one issue → one worktree → one PR.
---

# start-issue

You take one open child GitHub issue from this repo and stage everything needed for an implementation session: a fresh worktree on a dedicated branch, a `TASK.md` briefing containing the issue body plus the end-of-run instructions, and a copy-paste launch command. You do **not** launch the implementation session yourself — you set up, print the command, stop.

## When to use

- User names a GitHub issue number / URL and asks to start work ("start #42", "implement issue 42", "kick off `wire-drizzle-client`").
- User asks "what's next" after an epic was graduated and wants to begin.
- User explicitly says "set up a worktree for this issue."

Do not use to:

- Plan or rewrite the issue — that's done before graduation.
- Implement code in this session — this skill only sets up the worktree.
- Work on an umbrella issue directly. If the user points at an umbrella, list its open child issues and ask which to start.

## Inputs

Accept any of:

- Plain issue number: `42`
- `gh` URL: `https://github.com/<owner>/<repo>/issues/42`
- Short ref: `#42`

Resolve to the canonical issue number.

## Preflight (stop on any failure)

1. **`gh` authenticated:** `gh auth status` exits 0. If not, tell the user to run `gh auth login`.
2. **Inside a git repo with a GitHub remote:** `git remote get-url origin` returns a `github.com` URL.
3. **`~/wt/<repo-name>/` exists or can be created** — the worktree script handles this with `mkdir -p`, but warn if the home directory layout is unusual.
4. **Issue exists and is open.** `gh issue view <N> --json number,title,body,labels,state,url`. If state is not `OPEN`, stop and tell the user.
5. **Issue is not an umbrella.** Check labels for `umbrella`. If present, fetch the umbrella body, parse its `- [ ] #N` checklist for open children, and ask the user which child issue to start instead.
6. **Dependencies satisfied.** Parse the issue body for a `## Dependencies` section. If absent, warn (the issue predates the dependency convention) but continue. If present:
   - If the section's only content is `None.`, dependencies are trivially satisfied — continue.
   - Otherwise, extract every `#N` reference (one per bullet, in the `- #N — justification` form produced by `graduate-epic` pass 3). For each, run `gh issue view <N> --json number,title,state,stateReason`. A dependency is **satisfied** iff state is `CLOSED` *and* stateReason is `COMPLETED` (not `NOT_PLANNED`). State `OPEN` or stateReason `NOT_PLANNED` = unsatisfied.
   - If any dependency is unsatisfied, **stop**. Print a clear block listing every unsatisfied dependency with its number, title, state, and URL — e.g.:
     ```
     ✗ Cannot start issue #42: 1 of 2 dependencies not yet shipped.
       - #38 "Define blood-work tables" — OPEN
         https://github.com/<owner>/<repo>/issues/38
     Ship the dependency first (or, if you've intentionally re-ordered, edit the
     `## Dependencies` section of issue #42 to drop the stale reference).
     ```
     Do not create the worktree. Do not write `TASK.md`. Do not print the launch command. Exit.
   - If a dependency `#N` itself does not exist (`gh issue view` fails with not-found), treat it as unsatisfied with a "not found" note — same stop behavior.
   - Parsing rule: only the `## Dependencies` section's `- #N` bullets are checked. References to `#N` elsewhere in the issue body (Description, Edge cases, Out of scope) are explanatory and do not gate anything.
7. **Working tree clean on the main repo.** Optional but recommended — warn if `git status --short` is non-empty, since `.claude/` may be edited and copied as-is into the worktree.

## Slug derivation

From the issue title, derive a kebab-case slug for the branch / worktree name:

1. Lowercase the title.
2. Drop any leading `[N] ` or markdown emphasis markers (`**`, `*`, etc.) the planning workflow may have inserted.
3. Replace non-alphanumeric runs with `-`.
4. Collapse repeated `-`, trim leading/trailing.
5. Truncate to ~40 chars at a word boundary.

Examples:

- `Wire Drizzle config and DB client` → `wire-drizzle-config-and-db-client`
- `[3] Wire intent classifier (heuristic only)` → `wire-intent-classifier-heuristic-only` → truncate to `wire-intent-classifier-heuristic`

Final branch / worktree name: `issue-<N>-<slug>`. Example: `issue-42-wire-drizzle-config-and-db`.

## Worktree setup

Run the helper script:

```
.claude/skills/start-issue/scripts/create_worktree.sh <issue-number> <slug>
```

The script:

- Creates `~/wt/<repo-name>/issue-<N>-<slug>/` as a git worktree.
- Creates branch `issue-<N>-<slug>` off `origin/HEAD` (falls back to `main` / `master`).
- If the branch already exists (resuming after a crash), attaches the worktree to the existing branch.
- Copies `.claude/` into the worktree so this skill, the planning skills, and the agents travel with it.
- Runs `npm install` if `package.json` is present.

Output convention (first stdout line):

- `CREATED` — new branch + worktree
- `REUSED-BRANCH` — existing branch, new worktree attached
- `EXISTS` — worktree already present (script does nothing)

Second line is the absolute worktree path. If the script exits non-zero, surface its stderr to the user and stop.

## TASK.md

Write a single file at `<worktree-path>/TASK.md` with the structure below. This file is the implementation session's entire briefing — make it self-contained. Do not include the launch command (printed to terminal instead).

```markdown
# Issue #<N>: <issue title>

> [GitHub issue](<issue url>) · branch `issue-<N>-<slug>` · labels: <comma-separated>

---

<issue body verbatim — the umbrella's child issues already include Acceptance criteria, Edge cases, Files to touch, Tasks, etc. Do not re-summarize.>

---

## When implementation is complete

You are running in a worktree at `~/wt/<repo-name>/issue-<N>-<slug>/` on branch `issue-<N>-<slug>`. When every task and acceptance criterion above is satisfied:

1. **Verify locally.** Run `npm run typecheck` and `npm test` (skip `npm test` only if the issue is declaration-only per TDD scope). Both must exit 0. If they don't, fix and re-run before continuing.
2. **Commit.** `git add -A && git commit` with a message that summarizes the change in one line, followed by a blank line and short body if needed. Match the project's existing commit style (read `git log --oneline -10` to see it).
3. **Push.** `git push -u origin issue-<N>-<slug>`.
4. **Open the PR.**
   - `gh pr create --base main --title "<concise title, often the issue title>" --body <see below>`
   - PR body MUST include `Fixes #<N>` on its own line so merging auto-closes the issue.
   - PR body should also include: a one-paragraph summary of what was done, and a brief "How to verify" section pulled from the issue's Acceptance criteria.
5. **Report.** Print the PR URL as the final message. The human merges manually.

Do not switch branches, do not amend across previous commits, do not force-push. If something goes wrong (failing tests you can't fix, unclear acceptance criterion), stop and surface the question instead of guessing.
```

Substitute the placeholders with concrete values when writing the file. The implementation session reads this verbatim and follows it.

## Launch the implementation session

After the worktree is created and TASK.md is written, spawn a detached tmux session that runs the implementation session — no terminal-juggling, no copy-paste. Run:

```
.claude/skills/start-issue/scripts/launch_session.sh <issue-number> <absolute worktree path>
```

The script does `tmux new-session -d -s issue-<N> -c <worktree> "claude 'Read TASK.md and implement it end-to-end.'"`. The session is detached (runs in the background); each issue gets its own named session so parallel work doesn't collide. The user attaches with `tmux attach -t issue-<N>` from any terminal — including from another tab in their current one — and detaches with `Ctrl-b d`.

If the script returns exit code 2, a session named `issue-<N>` already exists (resuming a previous attempt). Surface the attach command — don't clobber a running session.

If the script returns 1, tmux isn't installed or the worktree path is wrong; surface stderr.

After launching, print this confirmation block:

```
✓ Worktree:  <absolute worktree path>
✓ Branch:    issue-<N>-<slug>
✓ Base:      <base branch>
✓ TASK.md:   <worktree path>/TASK.md
✓ Status:    <CREATED | REUSED-BRANCH | EXISTS>
✓ Launched:  tmux session 'issue-<N>' — implementation running in background.

Attach:  tmux attach -t issue-<N>
List:    tmux ls

When the session finishes you'll get a PR URL inside the tmux window. Merging the PR auto-closes issue #<N>.
```

## Failure modes

- **Issue is closed:** stop. Tell the user; suggest reopening if intentional.
- **Issue is umbrella:** stop. List open children with their numbers and titles; ask which to start.
- **Unsatisfied dependencies:** stop per preflight step 6. Do not create a worktree. The user either ships the dependency first or edits the issue's `## Dependencies` section if the order is intentionally being inverted. To bypass on purpose (rare), the user can pass `--force` — this skill should accept that flag, log a warning naming every unsatisfied dep, and proceed.
- **Branch exists, worktree doesn't:** the script attaches a worktree to the existing branch. The implementation session continues from the prior tip. Note this in the output ("Status: REUSED-BRANCH — resuming from existing commits").
- **Worktree already exists:** the script returns `EXISTS`. Skip the npm install (already done), still re-write `TASK.md` (issue body may have updated since the previous attempt), print the launch command.
- **`npm install` fails:** the script exits non-zero. Surface its output. The worktree may be in a half-set-up state — instruct the user to either remove it (`git worktree remove <path> --force`) and re-run, or fix the npm issue manually.
- **TASK.md already exists with edits:** ask before overwriting. The user may have hand-edited it.

## Removing a worktree later

Not this skill's job, but for completeness — after a PR merges:

```
git worktree remove ~/wt/<repo-name>/issue-<N>-<slug>
git branch -d issue-<N>-<slug>   # -D if it wasn't merged
```

## Style

- One issue per invocation. Never set up multiple worktrees in one run.
- Don't touch `main` or any other branch in the main checkout. The script does everything in the new worktree path.
- Auto-launch the implementation session via the tmux launcher script. The detached tmux session is a separate process, which is what keeps the implementing session's context isolated — terminal-agnostic, survives terminal restarts, and parallel issues coexist as separate named sessions.
- Never push to `main`, never open a PR from this skill itself. The implementation session does the PR. This skill ends at the launch command.

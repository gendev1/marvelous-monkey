---
name: start-issue
description: Start one PR-sized GitHub child issue by creating/reusing a git worktree, writing a margincalc-specific TASK.md, and launching a detached tmux Claude implementation session. Use when the user says "start #N", "implement issue N", or "pick up this issue". Refuses umbrella issues.
---

# start-issue

This skill starts exactly one implementation issue.

Correct harness model:

```text
Epic = coordination container
Umbrella GitHub issue = progress tracker
Child GitHub issue = one PR-sized implementation slice
One started child issue = one worktree + one branch + one PR
```

Do not implement umbrella issues. If the user points at an umbrella issue, list its open child issues and ask which child issue to start.

## Preconditions

Stop if any check fails:

1. `gh auth status` succeeds.
2. Current repo has a GitHub remote.
3. Requested issue exists and is open:

   ```sh
   gh issue view <N> --json number,title,body,labels,state,url
   ```

4. Issue does not have label `umbrella`.
5. Dependencies are satisfied. In the issue body, parse `## Dependencies`; every `- #N` dependency must be closed with `stateReason=COMPLETED`.
6. Warn if the main working tree is dirty. This matters because `.claude/` is copied into the worktree.

## Branch and worktree

Derive a branch slug from the issue title:

1. Lowercase.
2. Drop leading `[N]`.
3. Replace non-alphanumeric runs with `-`.
4. Collapse duplicate dashes.
5. Trim leading/trailing dashes.
6. Truncate to roughly 40 chars.

Branch/worktree name:

```text
issue-<N>-<slug>
```

Run:

```sh
.claude/skills/start-issue/scripts/create_worktree.sh <N> <slug>
```

The script creates/reuses:

```text
~/wt/<repo-name>/issue-<N>-<slug>/
```

## TASK.md

Write `<worktree>/TASK.md`:

```markdown
# Issue #<N>: <issue title>

> GitHub: <issue URL>
> Branch: `issue-<N>-<slug>`
> Repo: `margincalc`

---

<issue body verbatim>

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
5. Open a PR with `Fixes #<N>` in the body.
6. Report the PR URL.
7. Stop after reporting the PR URL.

Do not run blocking PR watcher scripts from inside the implementation session. Review follow-up is
handled by the operator or a separate continuation session.

Do not amend unrelated commits. Do not force-push unless explicitly asked.
```

## Launch

Run:

```sh
.claude/skills/start-issue/scripts/launch_session.sh <N> <absolute-worktree-path>
```

Print:

```text
Worktree: <path>
Branch:   issue-<N>-<slug>
TASK.md:  <path>/TASK.md
Session:  issue-<N>

Attach: tmux attach -t issue-<N>
List:   tmux ls
```

## Important

- This repo is not a JS app. Do not run `npm install`, `npm test`, or `npm run typecheck`.
- Docs-only issues still run `go test ./...` unless explicitly impossible.
- Vendor/API work must not guess API shape. If the issue lacks vendor contract details, stop and ask.

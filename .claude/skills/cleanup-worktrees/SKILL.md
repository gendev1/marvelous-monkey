---
name: cleanup-worktrees
description: Remove git worktrees and local branches whose pull requests are merged on GitHub, and kill the matching tmux sessions. Use when the user asks to clean up worktrees, prune merged branches, tidy up after PRs land, or remove stale per-issue sessions. Targets ~/wt/<repo>/issue-N-* worktrees created by start-issue; leaves the main worktree, the current session's worktree, open-PR worktrees, and <repo>/.claude/worktrees/* harness worktrees alone.
---

# cleanup-worktrees

Remove git worktrees + local branches + tmux sessions for issues whose PRs have been merged on GitHub. Idempotent — safe to re-run.

## What gets cleaned

Any worktree whose branch is the `headRefName` of a **merged** PR (closed with `mergedAt != null` in `gh pr list --state closed`).

For each match:

1. Kill the matching tmux session (derive name from `issue-N-<slug>` → `issue-N`).
2. `git worktree remove --force <path>` — `--force` is needed because `start-issue` leaves `TASK.md` and `.claude/worktrees/` untracked in the worktree.
3. `git branch -d <branch>` — safe delete; refuses if the branch isn't fully merged into HEAD or the remote tracking ref.

## What this never touches

- The main working tree (line 1 of `git worktree list`).
- The current session's worktree (`git rev-parse --show-toplevel`).
- Worktrees for branches with an **open** PR.
- Worktrees under `<repo>/.claude/worktrees/<name>` — those are managed by the Claude Code harness, not by `start-issue`.

## Workflow

1. **Gather in parallel** (one Bash call each):
   - `git -C <main-repo> worktree list`
   - `gh pr list --state closed --limit 50 --json number,title,headRefName,mergedAt`
   - `gh pr list --state open --limit 50 --json headRefName`
   - `tmux ls`

2. **Cross-reference**: candidate = worktree whose branch is in the merged set, not in the open set, and outside the protected paths.

3. **Safety-check each candidate**: `git -C <path> status --porcelain`. Acceptable untracked entries: `TASK.md`, `.claude/worktrees/`. Anything else — surface to the user and skip that worktree.

4. **Execute** (independent removals can run in parallel):
   - `tmux kill-session -t <session>` for each candidate that has a live session.
   - `git worktree remove --force <path>` for each candidate.
   - `git branch -d <branch>` for each candidate's branch.

5. **Verify**: re-run `git worktree list` and `tmux ls`; print the final state.

## Ambiguity — ask the user

Use `AskUserQuestion` (options: remove worktree + branch / remove worktree only / leave alone) when:

- Worktree path is outside `~/wt/<repo>/` and not under `<repo>/.claude/worktrees/` (e.g. hand-created `/tmp/...` paths).
- Branch name doesn't match the `issue-N-<slug>` convention but its PR is merged.
- `git branch -d` refuses (branch not fully merged into HEAD); the worktree is gone but the branch lingers.
- A worktree at HEAD of main with an unfamiliar branch name and no associated PR — could be an idle Claude harness session or stale state.

Default to **leave alone** for anything ambiguous.

## Summary output

End with a short report in this shape:

```
Removed:
  - <worktree-path>  (PR #N merged)
  ...
Kept:
  - main, current session, <open-PR worktrees>, .claude/worktrees/*
Asked about:
  - <ambiguous-path>: <user-decision>
```

## Notes

- `git branch -d` accepts branches merged via origin's tracking ref even if the local HEAD hasn't been updated yet (you'll see a warning like `"merged to refs/remotes/origin/<branch>, but not yet merged to HEAD"` and the delete proceeds). That's the expected path right after a PR merges on GitHub but before the local main is pulled.
- Don't delete remote branches as part of this cleanup. GitHub typically deletes the head branch on merge if the repo has "auto-delete head branches" on; if not, that's a separate request.
- Don't `rm -rf` worktree paths directly. Always use `git worktree remove` so git's metadata stays consistent.

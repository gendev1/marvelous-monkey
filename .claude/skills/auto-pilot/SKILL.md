---
name: auto-pilot
description: Continuously dispatch unblocked GitHub child issues from graduated epics into per-issue worktrees/tmux Claude sessions, then clean up merged issue worktrees and fast-forward main. Use when the user asks to automate the issue queue, run the next available issue, or keep an epic moving without manually choosing each unblocked child issue.
---

# auto-pilot

This skill runs the local issue orchestrator:

```sh
node scripts/auto-pilot.mjs
```

It is the automation layer above:

- `graduate-epic` - creates umbrella + child GitHub issues.
- `start-issue` - one child issue -> one branch/worktree/tmux session/PR.
- `cleanup-worktrees` - removes merged issue worktrees.

## Operating model

The autopilot loop:

1. Finds merged PRs whose branch matches `issue-N-*`.
2. Removes their matching `~/wt/<repo>/issue-N-*` worktree if it has no non-harness local changes.
3. Deletes the local branch with `git branch -d`.
4. Runs `git pull --ff-only` on the main checkout when the checkout is clean and currently on the default branch.
5. Lists open GitHub issues with an `epic:*` label.
6. Skips umbrella issues.
7. Parses each child issue's `## Dependencies` section for `- #N`.
8. Starts the first unblocked child issues up to `MAX_PARALLEL`.

The implementation sessions still own implementation, tests, PR creation, and review-comment fixes. The autopilot does not merge PRs.

## Commands

Run one planning pass without mutating anything:

```sh
node scripts/auto-pilot.mjs --once --dry-run --verbose
```

Run one real pass:

```sh
node scripts/auto-pilot.mjs --once
```

Run continuously:

```sh
MAX_PARALLEL=1 POLL_INTERVAL_MS=60000 node scripts/auto-pilot.mjs
```

## Configuration

- `MAX_PARALLEL` - max issue worktrees kept in flight. Default: `1`.
- `POLL_INTERVAL_MS` - loop delay. Default: `60000`.
- `EPIC_LABEL_PREFIX` - watched issue-label prefix. Default: `epic:`.
- `WORKTREE_ROOT` - parent directory for repo worktrees. Default: `~/wt`.
- `GH_REPO` - optional `owner/repo` override; normally auto-detected.
- `AUTO_PULL_MAIN=0` - disables the automatic `git pull --ff-only`.

## Safety boundaries

- Never implements umbrella issues.
- Never starts a child issue while any dependency is still open or closed as not-planned.
- Never starts a duplicate issue if an issue worktree, `issue-N` tmux session, or open PR already exists.
- Never removes the main checkout.
- Never removes a worktree with non-harness local changes.
- Never force-deletes local branches.
- Never auto-merges PRs.

## Preconditions

Stop if any check fails:

1. `gh auth status` succeeds.
2. `tmux` is installed.
3. The repo has a GitHub remote.
4. `.claude/skills/start-issue/scripts/create_worktree.sh` exists.
5. `.claude/skills/start-issue/scripts/launch_session.sh` exists.

## Notes

The issue sessions launched by this skill receive a `TASK.md` equivalent to the `start-issue`
skill's task file. Each session is instructed to implement the issue, open a PR, report the PR URL,
and stop. Blocking PR watcher scripts should not run inside the implementation session.

# Scripts

## `auto-pilot.mjs`

`auto-pilot.mjs` is a local orchestrator for the Claude issue workflow in this repo.

It automates the repeatable loop:

1. Find open GitHub child issues with an `epic:*` label.
2. Skip umbrella issues.
3. Parse `## Dependencies` for `- #N` blockers.
4. Start unblocked issues in `~/wt/<repo>/issue-N-*` git worktrees.
5. Launch one detached `tmux` Claude session per started issue.
6. Clean up worktrees after their PRs are merged.
7. Fast-forward the main checkout when safe.

It does **not** merge PRs.

### Preconditions

Required tools:

```sh
gh auth status
node --version
tmux -V
claude --version
```

The workflow also expects the `start-issue` scripts to exist:

```text
.claude/skills/start-issue/scripts/create_worktree.sh
.claude/skills/start-issue/scripts/launch_session.sh
```

### Dry Run

Run one pass without mutating worktrees, branches, or tmux sessions:

```sh
node scripts/auto-pilot.mjs --once --dry-run --verbose
```

Use this first after authenticating `gh`.

### Run Once

Run one real cleanup/dispatch pass:

```sh
node scripts/auto-pilot.mjs --once
```

### Run Continuously

Run as a simple long-lived local process:

```sh
MAX_PARALLEL=1 POLL_INTERVAL_MS=60000 node scripts/auto-pilot.mjs
```

Attach to an issue session with:

```sh
tmux attach -t issue-<N>
```

List active sessions:

```sh
tmux ls
```

### Configuration

Environment variables:

- `MAX_PARALLEL` - max issue worktrees in flight. Default: `1`.
- `POLL_INTERVAL_MS` - delay between loop ticks. Default: `60000`.
- `EPIC_LABEL_PREFIX` - watched issue label prefix. Default: `epic:`.
- `WORKTREE_ROOT` - parent directory for repo worktrees. Default: `~/wt`.
- `GH_REPO` - optional `owner/repo` override. Usually auto-detected.
- `AUTO_PULL_MAIN=0` - disables automatic `git pull --ff-only`.

### Safety Boundaries

The script is intentionally conservative:

- It never starts issues labeled `umbrella`.
- It never starts an issue whose dependencies are not closed as completed.
- It never starts duplicate work if a matching worktree, tmux session, or open PR exists.
- It never removes the main checkout.
- It never removes a worktree with non-harness local changes.
- It deletes local branches with `git branch -d`, not force delete.
- It never merges PRs.

### Current Limitations

- Issue discovery currently uses all open issues with labels matching `epic:*`; use one epic at a time or narrow `EPIC_LABEL_PREFIX`.
- There is no durable scheduler state file yet; state is reconstructed from GitHub, git worktrees, and tmux.
- Failed starts are logged and retried on the next poll tick, but there is no exponential backoff yet.
- The script assumes the GitHub issue body is already good enough to become `TASK.md`.

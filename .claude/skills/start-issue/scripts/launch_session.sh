#!/usr/bin/env bash
# Spawn a detached tmux session that runs `claude <prompt>` inside <worktree>.
# Usage: launch_session.sh <issue-number> <worktree-path> [<prompt>]
#
# Session name: issue-<N>. Attach from any terminal with:
#   tmux attach -t issue-<N>
# List all in-flight implementation sessions:
#   tmux ls
#
# If a session named issue-<N> already exists, this script refuses to clobber
# it — print attach instructions and exit non-zero. The caller (start-issue
# skill) surfaces the message.

set -euo pipefail

N="${1:?issue number required}"
WT="${2:?worktree path required}"
PROMPT="${3:-Read TASK.md and implement it end-to-end.}"

if [[ ! -d "$WT" ]]; then
  echo "✗ worktree not found: $WT" >&2
  exit 1
fi

if ! command -v tmux >/dev/null 2>&1; then
  echo "✗ tmux not found on PATH — install via 'brew install tmux'" >&2
  exit 1
fi

SESSION="issue-$N"

if tmux has-session -t="$SESSION" 2>/dev/null; then
  echo "✗ tmux session '$SESSION' already exists. Attach with:" >&2
  echo "    tmux attach -t $SESSION" >&2
  echo "  Or kill it first:  tmux kill-session -t $SESSION" >&2
  exit 2
fi

tmux new-session -d -s "$SESSION" -c "$WT" "claude '$PROMPT'"

echo "✓ tmux session '$SESSION' started in $WT"
echo "  Attach:  tmux attach -t $SESSION"
echo "  List:    tmux ls"

#!/bin/bash
# create_worktree.sh — create a git worktree for a single GitHub issue.
#
# Usage: ./create_worktree.sh <issue-number> <slug>
#
# Behavior:
#   - Worktree path: $HOME/wt/<repo-name>/issue-<N>-<slug>
#   - Branch name:   issue-<N>-<slug>
#   - Creates branch from origin/<default-branch> (falls back to main).
#   - If the branch already exists, attaches the worktree to it (resumable).
#   - Copies .claude/ into the worktree so skills/agents travel with it.
#   - Runs `npm install` if package.json is present.
#
# Output (stdout):
#   First line: CREATED or EXISTS or REUSED-BRANCH
#   Second line: absolute path to the worktree
#
# Errors exit non-zero with a message on stderr.

set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "Usage: $0 <issue-number> <slug>" >&2
  exit 64
fi

ISSUE_NUMBER="$1"
SLUG="$2"

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  echo "Not in a git repository." >&2
  exit 1
}
REPO_NAME="$(basename "${REPO_ROOT}")"

BRANCH="issue-${ISSUE_NUMBER}-${SLUG}"
WORKTREE_BASE="${HOME}/wt/${REPO_NAME}"
WORKTREE_PATH="${WORKTREE_BASE}/${BRANCH}"

mkdir -p "${WORKTREE_BASE}"

# If worktree already mounted, just report and exit.
if [[ -d "${WORKTREE_PATH}" ]]; then
  echo "EXISTS"
  echo "${WORKTREE_PATH}"
  exit 0
fi

# Resolve base branch (origin/HEAD -> name, fall back to main).
BASE_BRANCH=""
if git symbolic-ref --quiet refs/remotes/origin/HEAD >/dev/null 2>&1; then
  BASE_BRANCH="$(git symbolic-ref refs/remotes/origin/HEAD | sed 's@^refs/remotes/origin/@@')"
fi
if [[ -z "${BASE_BRANCH}" ]]; then
  if git show-ref --verify --quiet refs/heads/main; then
    BASE_BRANCH="main"
  elif git show-ref --verify --quiet refs/heads/master; then
    BASE_BRANCH="master"
  else
    echo "Could not resolve base branch (no origin/HEAD, no main, no master)." >&2
    exit 1
  fi
fi

# Branch already exists? attach worktree to it; otherwise create from base.
if git show-ref --verify --quiet "refs/heads/${BRANCH}"; then
  git worktree add "${WORKTREE_PATH}" "${BRANCH}" >/dev/null
  STATUS="REUSED-BRANCH"
else
  git worktree add -b "${BRANCH}" "${WORKTREE_PATH}" "${BASE_BRANCH}" >/dev/null
  STATUS="CREATED"
fi

# Copy .claude/ so skills, agents, and settings travel with the worktree.
if [[ -d "${REPO_ROOT}/.claude" ]]; then
  cp -R "${REPO_ROOT}/.claude" "${WORKTREE_PATH}/"
fi

# Install dependencies if a JS project.
if [[ -f "${WORKTREE_PATH}/package.json" ]]; then
  (cd "${WORKTREE_PATH}" && npm install --silent --no-audit --no-fund) || {
    echo "npm install failed in ${WORKTREE_PATH}." >&2
    exit 1
  }
fi

echo "${STATUS}"
echo "${WORKTREE_PATH}"

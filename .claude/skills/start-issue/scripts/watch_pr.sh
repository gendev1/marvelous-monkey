#!/usr/bin/env bash
# watch_pr.sh
#
# Poll the open PR for the current branch for new review comments.
# Blocks until unread comments arrive, prints them, and exits 0.
# Re-run after fixing to wait for the next round of review.
#
# Usage: watch_pr.sh [--interval <seconds>]

set -euo pipefail

INTERVAL=30
while [[ $# -gt 0 ]]; do
  case $1 in
    --interval) INTERVAL=$2; shift 2 ;;
    *) echo "Unknown arg: $1" >&2; exit 1 ;;
  esac
done

# ── Locate the PR ────────────────────────────────────────────────────────────

BRANCH=$(git rev-parse --abbrev-ref HEAD)
PR_JSON=$(gh pr list --head "$BRANCH" --state open --json number,url,headRefName --jq '.[0]' 2>/dev/null || true)

if [[ -z "$PR_JSON" || "$PR_JSON" == "null" ]]; then
  echo "❌  No open PR found for branch '$BRANCH'." >&2
  echo "    Push your branch and open a PR first, then re-run this script." >&2
  exit 1
fi

PR_NUM=$(echo "$PR_JSON" | jq -r '.number')
PR_URL=$(echo "$PR_JSON" | jq -r '.url')

# ── State file (tracks which comment IDs we've already surfaced) ──────────────

STATE_DIR="${TMPDIR:-/tmp}/watch_pr"
mkdir -p "$STATE_DIR"
SEEN_FILE="$STATE_DIR/seen_${PR_NUM}"
touch "$SEEN_FILE"

echo "👀  Watching PR #${PR_NUM} for review comments (${INTERVAL}s poll)."
echo "    ${PR_URL}"
echo "    Ctrl-C to stop."
echo ""

# ── Helpers ──────────────────────────────────────────────────────────────────

fetch_inline_comments() {
  # Line-level review comments
  gh api "repos/{owner}/{repo}/pulls/${PR_NUM}/comments" \
    --paginate \
    --jq '.[] | {
      kind: "inline",
      id: (.id | tostring),
      author: .user.login,
      path: .path,
      line: (.line // .original_line // 0),
      body: .body,
      diff_hunk: .diff_hunk
    }' 2>/dev/null || true
}

fetch_review_bodies() {
  # PR-level review bodies (CHANGES_REQUESTED or COMMENTED with non-empty body)
  gh pr view "$PR_NUM" --json reviews \
    --jq '.reviews[]
      | select(.state == "CHANGES_REQUESTED" or .state == "COMMENTED")
      | select(.body != "")
      | {
          kind: "review",
          id: (.submittedAt),
          author: .author.login,
          path: "",
          line: 0,
          body: .body,
          diff_hunk: ""
    }' 2>/dev/null || true
}

fetch_pr_status() {
  gh pr view "$PR_NUM" --json state,mergedAt,reviewDecision \
    --jq '{state, mergedAt, reviewDecision}' 2>/dev/null || true
}

print_comment() {
  local kind="$1" author="$2" path="$3" line="$4" body="$5" diff_hunk="$6"

  echo "┌─────────────────────────────────────────────────────────────────"
  if [[ "$kind" == "inline" ]]; then
    echo "│ 📍 ${author}  →  ${path}:${line}"
    if [[ -n "$diff_hunk" ]]; then
      echo "│"
      echo "$diff_hunk" | head -6 | sed 's/^/│   /'
      echo "│"
    fi
  else
    echo "│ 📝 ${author}  (PR-level comment)"
  fi
  echo "│"
  echo "$body" | fold -sw 72 | sed 's/^/│   /'
  echo "└─────────────────────────────────────────────────────────────────"
  echo ""
}

# ── Poll loop ─────────────────────────────────────────────────────────────────

while true; do
  STATUS_JSON="$(fetch_pr_status)"
  if [[ -n "$STATUS_JSON" && "$STATUS_JSON" != "null" ]]; then
    PR_STATE="$(echo "$STATUS_JSON" | jq -r '.state // ""')"
    MERGED_AT="$(echo "$STATUS_JSON" | jq -r '.mergedAt // ""')"
    REVIEW_DECISION="$(echo "$STATUS_JSON" | jq -r '.reviewDecision // ""')"

    if [[ "$PR_STATE" == "MERGED" || -n "$MERGED_AT" ]]; then
      echo "✅  PR #${PR_NUM} has been merged."
      exit 0
    fi

    if [[ "$PR_STATE" == "CLOSED" ]]; then
      echo "ℹ️  PR #${PR_NUM} is closed."
      exit 0
    fi

    if [[ "$REVIEW_DECISION" == "APPROVED" ]]; then
      echo "✅  PR #${PR_NUM} is approved."
      exit 0
    fi
  fi

  NEW_FOUND=0

  while IFS= read -r raw; do
    [[ -z "$raw" ]] && continue
    ID=$(echo "$raw" | jq -r '.id')
    if ! grep -qxF "$ID" "$SEEN_FILE"; then
      echo "$ID" >> "$SEEN_FILE"
      KIND=$(echo "$raw" | jq -r '.kind')
      AUTHOR=$(echo "$raw" | jq -r '.author')
      PATH_=$(echo "$raw" | jq -r '.path')
      LINE=$(echo "$raw" | jq -r '.line')
      BODY=$(echo "$raw" | jq -r '.body')
      HUNK=$(echo "$raw" | jq -r '.diff_hunk')
      print_comment "$KIND" "$AUTHOR" "$PATH_" "$LINE" "$BODY" "$HUNK"
      NEW_FOUND=1
    fi
  done < <(fetch_inline_comments; fetch_review_bodies)

  if [[ $NEW_FOUND -eq 1 ]]; then
    echo "═══════════════════════════════════════════════════════════════════"
    echo "  Fix the issues above, commit, push, then re-run watch_pr.sh."
    echo "═══════════════════════════════════════════════════════════════════"
    exit 0
  fi

  sleep "$INTERVAL"
done

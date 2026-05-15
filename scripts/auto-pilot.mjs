#!/usr/bin/env node
/**
 * DAG-aware GitHub issue autopilot for the local Claude worktree workflow.
 *
 * It intentionally does not merge PRs. It dispatches unblocked child issues,
 * lets each issue session implement/review/fix, then cleans up after the PR is
 * merged and fast-forwards the main checkout when safe.
 */

import { execFile as execFileCb } from 'node:child_process';
import { existsSync, writeFileSync } from 'node:fs';
import { dirname, isAbsolute, join, resolve as resolvePath } from 'node:path';
import { fileURLToPath } from 'node:url';
import { homedir } from 'node:os';
import { promisify } from 'node:util';
import { setTimeout as sleep } from 'node:timers/promises';

const execFile = promisify(execFileCb);
const SCRIPT_DIR = dirname(fileURLToPath(import.meta.url));
const REPO_HINT = resolvePath(SCRIPT_DIR, '..');

const rawArgs = process.argv.slice(2);
const ARGS = new Set(rawArgs);

const CFG = {
  once: ARGS.has('--once'),
  dryRun: ARGS.has('--dry-run'),
  verbose: ARGS.has('--verbose'),
  maxParallel: numberEnv('MAX_PARALLEL', 1),
  pollIntervalMs: numberEnv('POLL_INTERVAL_MS', 60_000),
  epicLabelPrefix: process.env.EPIC_LABEL_PREFIX || 'epic:',
  worktreeRoot: process.env.WORKTREE_ROOT || join(homedir(), 'wt'),
  repoOverride: process.env.GH_REPO || '',
  autoPullMain: process.env.AUTO_PULL_MAIN !== '0',
};

function numberEnv(name, fallback) {
  const raw = process.env[name];
  if (!raw) return fallback;
  const parsed = Number.parseInt(raw, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function log(level, ...parts) {
  if (level === 'debug' && !CFG.verbose) return;
  console.log(`[${new Date().toISOString()}] [${level}]`, ...parts);
}

const info = (...parts) => log('info', ...parts);
const warn = (...parts) => log('warn', ...parts);
const debug = (...parts) => log('debug', ...parts);

async function run(file, args = [], options = {}) {
  debug('$', file, ...args);
  if (CFG.dryRun && options.mutates) {
    info('(dry-run) would run:', [file, ...args].join(' '));
    return '';
  }
  try {
    const result = await execFile(file, args, {
      cwd: options.cwd || CFG.repoRoot || REPO_HINT,
      env: { ...process.env, ...(options.env || {}) },
      maxBuffer: 32 * 1024 * 1024,
    });
    if (result.stderr.trim()) debug(result.stderr.trim());
    return result.stdout;
  } catch (error) {
    const message = [
      `${file} ${args.join(' ')}`.trim(),
      error.stderr?.trim(),
      error.stdout?.trim(),
      error.message,
    ].filter(Boolean).join('\n');
    throw new Error(message);
  }
}

async function json(file, args = [], options = {}) {
  const out = await run(file, args, options);
  return out.trim() ? JSON.parse(out) : null;
}

async function preflight() {
  CFG.repoRoot = (await run('git', ['rev-parse', '--show-toplevel'], { cwd: REPO_HINT })).trim();
  await run('gh', ['auth', 'status']);
  await run('tmux', ['-V']);

  const repo = CFG.repoOverride
    ? { nameWithOwner: CFG.repoOverride }
    : await json('gh', ['repo', 'view', '--json', 'nameWithOwner']);
  CFG.ghRepo = repo.nameWithOwner;
  CFG.repoName = CFG.ghRepo.split('/').at(-1);
  CFG.worktreeBase = join(CFG.worktreeRoot, CFG.repoName);
  CFG.defaultBranch = await defaultBranch();

  const createScript = join(CFG.repoRoot, '.claude/skills/start-issue/scripts/create_worktree.sh');
  const launchScript = join(CFG.repoRoot, '.claude/skills/start-issue/scripts/launch_session.sh');
  if (!existsSync(createScript)) throw new Error(`missing ${createScript}`);
  if (!existsSync(launchScript)) throw new Error(`missing ${launchScript}`);
  CFG.createScript = createScript;
  CFG.launchScript = launchScript;

  info(`repo: ${CFG.ghRepo}`);
  info(`repo root: ${CFG.repoRoot}`);
  info(`worktree base: ${CFG.worktreeBase}`);
  info(`default branch: ${CFG.defaultBranch}`);
  info(`max parallel issues: ${CFG.maxParallel}`);
  info(`poll interval: ${CFG.pollIntervalMs}ms`);
  if (CFG.dryRun) info('dry run: no mutating commands will execute');
}

async function defaultBranch() {
  try {
    const ref = (await run('git', ['symbolic-ref', 'refs/remotes/origin/HEAD'])).trim();
    return ref.replace('refs/remotes/origin/', '');
  } catch {
    return 'main';
  }
}

function hasLabel(item, labelName) {
  return (item.labels || []).some((label) => label.name === labelName);
}

function hasLabelPrefix(item, prefix) {
  return (item.labels || []).some((label) => label.name.startsWith(prefix));
}

async function listOpenChildIssues() {
  const issues = await json('gh', [
    'issue', 'list',
    '--state', 'open',
    '--limit', '200',
    '--json', 'number,title,body,labels,url',
  ]);
  return issues
    .filter((issue) => hasLabelPrefix(issue, CFG.epicLabelPrefix))
    .filter((issue) => !hasLabel(issue, 'umbrella'))
    .sort((a, b) => a.number - b.number);
}

function parseDependencies(body) {
  if (!body) return [];
  const lines = body.split(/\r?\n/);
  const start = lines.findIndex((line) => /^##\s+Dependencies\s*$/i.test(line.trim()));
  if (start === -1) return [];

  const deps = [];
  for (const line of lines.slice(start + 1)) {
    const trimmed = line.trim();
    if (/^##\s+/.test(trimmed)) break;
    const match = trimmed.match(/^-\s+#(\d+)\b/);
    if (match) deps.push(Number.parseInt(match[1], 10));
  }
  return deps;
}

async function dependencyCompleted(issueNumber) {
  const issue = await json('gh', [
    'issue', 'view', String(issueNumber),
    '--json', 'number,state,stateReason',
  ]);
  return issue.state === 'CLOSED' && issue.stateReason === 'COMPLETED';
}

async function dependenciesSatisfied(issue) {
  const deps = parseDependencies(issue.body);
  if (deps.length === 0) return true;
  for (const dep of deps) {
    if (!(await dependencyCompleted(dep))) {
      debug(`#${issue.number} blocked by #${dep}`);
      return false;
    }
  }
  return true;
}

async function listWorktrees() {
  const out = await run('git', ['worktree', 'list', '--porcelain']);
  const worktrees = [];
  let current = null;

  for (const line of out.split(/\r?\n/)) {
    if (line.startsWith('worktree ')) {
      if (current) worktrees.push(current);
      current = { path: line.slice('worktree '.length), branch: '' };
    } else if (current && line.startsWith('branch refs/heads/')) {
      current.branch = line.slice('branch refs/heads/'.length);
    }
  }
  if (current) worktrees.push(current);
  return worktrees;
}

async function listIssueWorktrees() {
  const base = ensureTrailingSlash(CFG.worktreeBase);
  return (await listWorktrees()).filter((wt) =>
    wt.path.startsWith(base) && /^issue-\d+-/.test(wt.branch || basename(wt.path))
  );
}

function basename(path) {
  return path.split('/').filter(Boolean).at(-1) || path;
}

function ensureTrailingSlash(path) {
  return path.endsWith('/') ? path : `${path}/`;
}

async function tmuxSessionExists(issueNumber) {
  try {
    await run('tmux', ['has-session', '-t', `issue-${issueNumber}`]);
    return true;
  } catch {
    return false;
  }
}

async function openPRForIssue(issueNumber) {
  const prs = await json('gh', [
    'pr', 'list',
    '--state', 'open',
    '--limit', '100',
    '--json', 'number,headRefName',
  ]);
  return prs.find((pr) => pr.headRefName.startsWith(`issue-${issueNumber}-`)) || null;
}

async function issueAlreadyInFlight(issueNumber) {
  const worktrees = await listIssueWorktrees();
  if (worktrees.some((wt) => wt.branch.startsWith(`issue-${issueNumber}-`))) return true;
  if (await tmuxSessionExists(issueNumber)) return true;
  return Boolean(await openPRForIssue(issueNumber));
}

function slugifyIssueTitle(title) {
  return title
    .toLowerCase()
    .replace(/^\[\d+\]\s*/, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '')
    .slice(0, 40)
    .replace(/-$/g, '') || 'issue';
}

async function issueDetails(issueNumber) {
  return json('gh', [
    'issue', 'view', String(issueNumber),
    '--json', 'number,title,body,labels,state,url',
  ]);
}

async function startIssue(issue) {
  const detail = await issueDetails(issue.number);
  if (detail.state !== 'OPEN') return false;
  if (hasLabel(detail, 'umbrella')) return false;
  if (!(await dependenciesSatisfied(detail))) return false;

  const slug = slugifyIssueTitle(detail.title);
  info(`starting #${detail.number}: ${detail.title}`);
  if (CFG.dryRun) {
    info(`(dry-run) would create worktree/branch issue-${detail.number}-${slug}`);
    return true;
  }

  const created = await run(CFG.createScript, [String(detail.number), slug], {
    cwd: CFG.repoRoot,
    mutates: true,
  });
  const [, worktreePathRaw] = created.trim().split(/\r?\n/);
  const worktreePath = worktreePathRaw?.trim();
  if (!worktreePath || !isAbsolute(worktreePath)) {
    throw new Error(`create_worktree.sh returned an invalid path:\n${created}`);
  }

  writeFileSync(join(worktreePath, 'TASK.md'), taskMarkdown(detail, slug), 'utf8');
  const prompt = [
    'Read TASK.md and implement it end-to-end.',
    'Open a PR with Fixes in the body.',
    'Stop after reporting the PR URL.',
  ].join(' ');

  await run(CFG.launchScript, [String(detail.number), worktreePath, prompt], {
    cwd: CFG.repoRoot,
    mutates: true,
  });
  info(`launched issue-${detail.number} in ${worktreePath}`);
  return true;
}

function taskMarkdown(issue, slug) {
  return `# Issue #${issue.number}: ${issue.title}

> GitHub: ${issue.url}
> Branch: \`issue-${issue.number}-${slug}\`
> Repo: \`margincalc\`

---

${issue.body || ''}

---

## Repo Context

This is a Go module for a programmable margin calculator.

Core areas:

- \`internal/engine\` - CEL/YAML RegT rule engine.
- \`internal/recon\` - current CSV reconciliation harness.
- \`internal/account\` - account aggregation layer.
- \`rules/\` - Cboe baseline and house-rule examples.
- \`cmd/\` - CLI entry points.

Required conventions:

- Run commands from the repo root.
- Preserve invariants in \`CLAUDE.md\`.
- Rule order in YAML is load-bearing.
- Add behavioral tests for behavioral changes.
- Do not weaken CEL strictness, validation, or rulebook fail-fast behavior.
- Keep PR scope limited to this issue.

## Required Verification

Run before committing:

\`\`\`sh
gofmt -w <changed-go-files>
go test ./...
go vet ./...
\`\`\`

If \`go vet ./...\` reports an existing unrelated issue, document it in the PR body and still include \`go test ./...\`.

## Completion Instructions

1. Implement the issue end-to-end.
2. Run required verification.
3. Commit with a concise message.
4. Push the branch.
5. Open a PR with \`Fixes #${issue.number}\` in the body.
6. Report the PR URL.
7. Stop after reporting the PR URL.

Do not run blocking PR watcher scripts from inside the implementation session. Review follow-up is handled by the operator or a separate continuation session.

Do not amend unrelated commits. Do not force-push unless explicitly asked.
`;
}

async function listMergedPRs() {
  const prs = await json('gh', [
    'pr', 'list',
    '--state', 'merged',
    '--limit', '100',
    '--json', 'number,headRefName,mergedAt',
  ]);
  return prs.filter((pr) => /^issue-\d+-/.test(pr.headRefName || ''));
}

function acceptableCleanupStatus(status) {
  const lines = status.split(/\r?\n/).filter(Boolean);
  return lines.every((line) => {
    const path = line.slice(3);
    return path === 'TASK.md' || path.startsWith('.claude/');
  });
}

async function cleanupMergedPR(pr) {
  const worktrees = await listIssueWorktrees();
  const wt = worktrees.find((candidate) => candidate.branch === pr.headRefName);
  if (!wt) return false;

  const status = await run('git', ['-C', wt.path, 'status', '--porcelain']);
  if (!acceptableCleanupStatus(status)) {
    warn(`skipping cleanup for ${wt.path}; it has non-harness changes`);
    return false;
  }

  info(`cleaning merged PR #${pr.number}: ${wt.branch}`);
  if (CFG.dryRun) {
    info(`(dry-run) would remove ${wt.path} and delete ${wt.branch}`);
    return true;
  }

  const issueMatch = wt.branch.match(/^issue-(\d+)-/);
  if (issueMatch) {
    try {
      await run('tmux', ['kill-session', '-t', `issue-${issueMatch[1]}`], { mutates: true });
    } catch {
      debug(`tmux session issue-${issueMatch[1]} was not running`);
    }
  }

  await run('git', ['worktree', 'remove', '--force', wt.path], { cwd: CFG.repoRoot, mutates: true });
  try {
    await run('git', ['branch', '-d', wt.branch], { cwd: CFG.repoRoot, mutates: true });
  } catch (error) {
    warn(`local branch ${wt.branch} was not deleted: ${error.message}`);
  }
  return true;
}

async function pullMainIfSafe() {
  if (!CFG.autoPullMain) return;
  const current = (await run('git', ['rev-parse', '--abbrev-ref', 'HEAD'], { cwd: CFG.repoRoot })).trim();
  if (current !== CFG.defaultBranch) {
    debug(`not pulling ${CFG.defaultBranch}; current branch is ${current}`);
    return;
  }
  const status = await run('git', ['status', '--porcelain'], { cwd: CFG.repoRoot });
  if (status.trim()) {
    warn(`not pulling ${CFG.defaultBranch}; main checkout is dirty`);
    return;
  }
  info(`fast-forwarding ${CFG.defaultBranch}`);
  await run('git', ['pull', '--ff-only'], { cwd: CFG.repoRoot, mutates: true });
}

async function cleanupMergedWork() {
  let cleaned = 0;
  for (const pr of await listMergedPRs()) {
    if (await cleanupMergedPR(pr)) cleaned++;
  }
  if (cleaned > 0) await pullMainIfSafe();
}

async function dispatchUnblockedIssues() {
  const active = (await listIssueWorktrees()).length;
  let slots = Math.max(0, CFG.maxParallel - active);
  if (slots === 0) {
    debug(`capacity full: ${active}/${CFG.maxParallel}`);
    return;
  }

  for (const issue of await listOpenChildIssues()) {
    if (slots === 0) break;
    if (await issueAlreadyInFlight(issue.number)) {
      debug(`#${issue.number} already in flight`);
      continue;
    }
    if (!(await dependenciesSatisfied(issue))) continue;
    try {
      if (await startIssue(issue)) slots--;
    } catch (error) {
      warn(`failed to start #${issue.number}: ${error.message}`);
    }
  }
}

async function tick() {
  await cleanupMergedWork();
  await dispatchUnblockedIssues();
}

let stopRequested = false;
for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => {
    info(`received ${signal}; stopping after current tick`);
    stopRequested = true;
  });
}

async function main() {
  try {
    await preflight();
    do {
      await tick();
      if (CFG.once || stopRequested) break;
      await sleep(CFG.pollIntervalMs);
    } while (!stopRequested);
    info('exiting cleanly');
  } catch (error) {
    console.error(`[${new Date().toISOString()}] [error]`, error.message);
    process.exitCode = 1;
  }
}

main();

---
description: Bootstrap the current repo for atomic-claude use. Audits .gitignore entries, docs/ layout, and presence of CLAUDE.md. Proposes only what's missing — never overwrites. No commits.
---

You set up the repo for atomic-claude conventions. Detect first, propose second, apply only what the user confirms.

## Pre-flight

1. Verify inside a git repo: `git rev-parse --is-inside-work-tree 2>/dev/null`.
2. If not a git repo, prompt via `AskUserQuestion`:
    ```
    Not a git repo. Initialize one?
    - Yes, run git init
    - No, stop
    ```
    On Yes: `git init`. On No: refuse and stop.

## Step 1 — Audit

Inspect the repo. Build this status table:

| Convention | Present? | Status |
|-----------|----------|--------|
| `.gitignore` exists | check `test -f .gitignore` | exists / missing |
| `.gitignore` has `tmp/` | grep `^tmp/?$` | yes / no |
| `.gitignore` has `.claude/.scratchpad/` | grep `^\.claude/\.scratchpad/?$` | yes / no |
| `.gitignore` has `.worktrees/` | grep `^\.worktrees/?$` | yes / no |
| `CLAUDE.md` at repo root | `test -f CLAUDE.md` | exists / missing |
| `docs/` directory | `test -d docs` | exists / missing |
| `docs/spec/` directory | `test -d docs/spec` | exists / missing |
| `docs/design/` directory | `test -d docs/design` | exists / missing |
| `README.md` at repo root | `test -f README.md` | exists / missing |
| `atomic` binary on PATH | `command -v atomic` | found / missing |
| `.claude/hooks/session-start-reminders.sh` exists | `test -f .claude/hooks/session-start-reminders.sh` | exists / missing |
| `SessionStart` hook registered in `.claude/settings.json` | parse `.claude/settings.json` (JWCC tolerated) and look for a `SessionStart` entry whose `hooks[].command` value contains `session-start-reminders.sh` (the absolute path written by `atomic hooks install`) | registered / missing |
| `.claude/project/deterministic-signals.md` | `test -f .claude/project/deterministic-signals.md` | exists / missing |
| `CLAUDE.md` references signals files | `test -f CLAUDE.md && grep -qF '@.claude/project/deterministic-signals.md' CLAUDE.md && grep -qF '@.claude/project/inferred-signals.md' CLAUDE.md` (if `test -f CLAUDE.md` fails → n/a) | yes / no / n/a |

Classify the repo:

- **fresh** — none of the conventions present (or only an empty `.gitignore`).
- **partial** — some present, some missing.
- **complete** — all present.

Print the audit table and the classification.

## Step 2 — Propose

For each missing item, propose an action. Skip items already present.

| Missing item | Proposed action |
|--------------|----------------|
| `.gitignore` doesn't exist | Create with three lines: `tmp/`, `.claude/.scratchpad/`, `.worktrees/`. |
| `.gitignore` exists but missing `tmp/` | Append `tmp/`. |
| `.gitignore` exists but missing `.claude/.scratchpad/` | Append `.claude/.scratchpad/`. |
| `.gitignore` exists but missing `.worktrees/` | Append `.worktrees/`. |
| `CLAUDE.md` missing | Write the atomic starter template (see below). |
| `docs/spec/` missing | Create directory + `docs/spec/.gitkeep` (so git tracks it before any content lands). |
| `docs/design/` missing | Create directory + `docs/design/.gitkeep`. |
| `README.md` missing | Offer to scaffold a minimal starter. If user declines, skip — don't push it. |
| `atomic` binary missing | Print: `curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh \| bash`. Setup does not run the install — user runs the curl. |
| Script + registration missing, binary present | Run `atomic hooks install`. |
| Script + registration missing, binary missing | Write `.claude/hooks/session-start-reminders.sh` as the fallback script manually AND manually add the `SessionStart` hook entry to `.claude/settings.json`. |
| Script present, registration missing | Run `atomic hooks install` (idempotent — rewrites script with canonical content and adds the settings entry). |
| `deterministic-signals.md` missing but `atomic` present | Print: "Run `/initialize-signals` to generate project signals." (follow-up only; setup does not invoke it). |
| `CLAUDE.md` exists but missing either `@-ref` | Append the `## Project signals (auto-loaded)` section (see Signals subsection in Step 4). Skip this row when `CLAUDE.md` is missing — the starter template row handles that case. |

Present the proposed actions as a numbered list:

```
Proposed actions:
  [1] Append tmp/, .claude/.scratchpad/, .worktrees/ to .gitignore
  [2] Create CLAUDE.md from atomic template
  [3] Create docs/spec/.gitkeep
  [4] Create docs/design/.gitkeep
  [5] Scaffold README.md (optional — say "skip 5" to leave it)
```

If the repo is **complete**, print `repo already atomic-ready. nothing to do.` and stop.

## Step 3 — Confirm

Prompt user:

```
Apply which actions? Type indices, "all", or "none".
Examples: `1 3 5`  |  `all`  |  `none`  |  `1-3 5`  |  `all except 5`

Your selection:
```

Parse the input the same way `/git-cleanup` does (space- or comma-separated, ranges, `all`, `none`, `all except N` excluded list).

Validate each index against the proposed list. Unknown index → re-prompt.

## Step 4 — Apply

For each confirmed action, in order:

### `.gitignore`

- If file missing: write a fresh one with the three lines.
- If file exists: read it. For each missing entry, append a new line (preserve trailing newline). NEVER modify existing entries. NEVER reorder.

```bash
# Example append (one entry, idempotent):
grep -qxF 'tmp/' .gitignore || echo 'tmp/' >> .gitignore
```

### `CLAUDE.md`

- Refuse to overwrite if file exists. (Audit already gated this — defensive double-check.)
- Write the template from this command (see below).

### `docs/spec/` and `docs/design/`

```bash
mkdir -p docs/spec docs/design
touch docs/spec/.gitkeep docs/design/.gitkeep
```

### `README.md`

- Refuse to overwrite if file exists.
- Write a minimal scaffold: title (repo dir name), one-line pitch placeholder, "Install / Usage / License" placeholder headings. Tell the user it's a stub and they should expand it.

### Signals

Apply in this order, only for the confirmed actions:

**Binary missing** — Print the install command only; do not execute it:

```
Install the atomic binary:
  curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash
```

**Signals files missing (binary present)** — Print the follow-up command; do not invoke it:

```
Run /initialize-signals to generate project signals.
```

**`CLAUDE.md` missing `@-refs`** — Append to the existing `CLAUDE.md`:

```bash
if test -f CLAUDE.md && ! { grep -qF '@.claude/project/deterministic-signals.md' CLAUDE.md && grep -qF '@.claude/project/inferred-signals.md' CLAUDE.md; }; then
  cat >> CLAUDE.md << 'EOF'


## Project signals (auto-loaded)


@.claude/project/deterministic-signals.md
@.claude/project/inferred-signals.md
EOF
fi
```

Idempotent: only appends when `CLAUDE.md` exists AND at least one `@-ref` is missing. Refuses silently otherwise.

## Step 5 — Report

Final state:

```
Applied:
  ✓ .gitignore updated: added tmp/, .claude/.scratchpad/, .worktrees/
  ✓ CLAUDE.md created (atomic template — edit it with project-specific context)
  ✓ docs/spec/ + docs/design/ created with .gitkeep

Skipped:
  • README.md (you said no)

Next steps:
  - Edit CLAUDE.md to capture this project's meaningful context.
  - Run /atomic-plan to start your first design or spec.
  - Commit when ready: /commit-only.
```

Delete no scratch (this command writes no scratchpad).

## CLAUDE.md starter template

Use this exactly when creating `CLAUDE.md`. Tabs and blank-line spacing preserved.

```markdown
# CLAUDE.md


## Principles


- Think before coding. State assumptions. Ask, don't guess.
- Simplicity first. Minimum code. No abstractions for single-use code.
- Surgical changes. Touch only what's needed. Match existing style.
- Read before you write. Check exports, callers, shared utilities.
- Tests verify intent. A test that still passes when business logic changes is wrong.
- Fail loud. "Completed" is wrong if anything was skipped.


## Where things live


- **Working memory** (LLM-only, gitignored): `.claude/.scratchpad/<YYYY-MM-DD>-<desc>/` — used by `/subagent-implementation`. Holds `BRIEF.md` + `STATE.md`. Deleted on task completion.
- **Durable docs** (committed):
  - `docs/design/<topic>.md` — design rationale, alternatives, brainstorming. Written via `/atomic-plan`.
  - `docs/spec/<topic>.md` — implementation contract for an approved feature. Written via `/atomic-plan`. Canonical source for `/subagent-implementation`.
- **Worktrees** (gitignored): `.worktrees/<branch-name>/` — created via `/worktree-start`.
- **Throwaway** (gitignored): `tmp/` — ad-hoc experiments, scratch scripts, one-off test files.


## Project-specific


<!--
Replace this section with what's meaningful for THIS repo. Examples:

- Build / test / lint commands the agent should know
- Architectural patterns the codebase uses
- Gotchas or non-obvious conventions
- Deployment targets, env vars, infra notes

Keep it lean. If the agent can read the code and figure it out, leave it out.
-->


## Workflow (atomic)


1. Plan: `/atomic-plan` → `docs/design/<topic>.md` or `docs/spec/<topic>.md` (human-approved).
2. Implement: `/subagent-implementation` (reads spec, runs implement→review loop).
3. Ship: `/commit-only`, `/commit-and-pr`, `/pr-only`, `/merge-to-main`, `/squash-and-merge`, etc.
4. Sync docs: `/documentation`.
5. Clean up: `/git-cleanup`.
```

## Rules

- Never overwrite an existing file. The audit + the apply step both gate this.
- Never modify existing `.gitignore` entries. Only append missing ones.
- Never `git add` or commit. The user owns when to commit setup changes.
- Idempotent — running the command twice on a fresh-then-bootstrapped repo should report "already complete" the second time.
- If a step fails partway through (e.g. permission denied on `mkdir`), report which step failed and stop. Don't continue silently.

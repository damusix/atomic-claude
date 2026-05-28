---
description: Bootstrap the current repo for atomic-claude use. Audits .gitignore entries, docs/ layout, and presence of CLAUDE.md. Proposes only what's missing — never overwrites. No commits.
---

You set up the repo for atomic-claude conventions. Detect first, propose second, apply only what the user confirms.

<workflow>

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
| `.gitignore` has `.claude/project/.deterministic-signals.prev.md` | grep the pattern | yes / no |
| `CLAUDE.md` at repo root | `test -f CLAUDE.md` | exists / missing |
| `docs/` directory | `test -d docs` | exists / missing |
| `docs/spec/` directory | `test -d docs/spec` | exists / missing |
| `docs/design/` directory | `test -d docs/design` | exists / missing |
| `README.md` at repo root | `test -f README.md` | exists / missing |
| `atomic` binary on PATH | `command -v atomic` | found / missing |
| `.claude/hooks/session-start-reminders.sh` exists | `test -f .claude/hooks/session-start-reminders.sh` | exists / missing |
| `SessionStart` hook registered in `.claude/settings.json` | parse `.claude/settings.json` (JWCC tolerated) and look for a `SessionStart` entry whose `hooks[].command` value contains `session-start-reminders.sh` (the absolute path written by `atomic hooks install`) | registered / missing |
| `.claude/project/deterministic-signals.md` | `test -f .claude/project/deterministic-signals.md` | exists / missing |
| `CLAUDE.md` references signals.md | `test -f CLAUDE.md && grep -qF '@.claude/project/signals.md' CLAUDE.md` (if `test -f CLAUDE.md` fails → n/a). Only `signals.md` is `@-ref`'d — `deterministic-signals.md` is too large for context. | yes / no / n/a |
| `.signalsignore` at repo root | `test -f .signalsignore` | exists / missing |
| `.claude/project/signals-steering.md` | `test -f .claude/project/signals-steering.md` | exists / missing |

Classify the repo:

- **fresh** — none of the conventions present (or only an empty `.gitignore`).
- **partial** — some present, some missing.
- **complete** — all present.

Print the audit table and the classification.

## Step 2 — Propose

For each missing item, propose an action. Skip items already present.

| Missing item | Proposed action |
|--------------|----------------|
| `.gitignore` doesn't exist | Create with: `tmp/`, `.claude/.scratchpad/`, `.worktrees/`, `.claude/project/.deterministic-signals.prev.md`. |
| `.gitignore` exists but missing `tmp/` | Append `tmp/`. |
| `.gitignore` exists but missing `.claude/.scratchpad/` | Append `.claude/.scratchpad/`. |
| `.gitignore` exists but missing `.worktrees/` | Append `.worktrees/`. |
| `.gitignore` exists but missing `.claude/project/.deterministic-signals.prev.md` | Append `.claude/project/.deterministic-signals.prev.md`. |
| `CLAUDE.md` missing | Run the survey procedure (see "CLAUDE.md survey" in Step 4). Seed every section with an agent guess from signals/README/code; user edits the guess. |
| `docs/spec/` missing | Create directory + `docs/spec/.gitkeep` (so git tracks it before any content lands). |
| `docs/design/` missing | Create directory + `docs/design/.gitkeep`. |
| `README.md` missing | Offer to scaffold a minimal starter. If user declines, skip — don't push it. |
| `atomic` binary missing | Print: `curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh \| bash`. Setup does not run the install — user runs the curl. |
| Script + registration missing, binary present | Run `atomic hooks install`. |
| Script + registration missing, binary missing | Write `.claude/hooks/session-start-reminders.sh` as the fallback script manually AND manually add the `SessionStart` hook entry to `.claude/settings.json`. |
| Script present, registration missing | Run `atomic hooks install` (idempotent — rewrites script with canonical content and adds the settings entry). |
| `deterministic-signals.md` missing but `atomic` present | Print: "Run `/refresh-signals` to generate project signals." (follow-up only; setup does not invoke it). |
| `CLAUDE.md` exists but missing either `@-ref` | Append the `## Project signals (auto-loaded)` section (see Signals subsection in Step 4). Skip this row when `CLAUDE.md` is missing — the starter template row handles that case. |
| `.signalsignore` missing | Create `.signalsignore` with commented explanation (see `.signalsignore` subsection in Step 4). Never overwrite if it exists. |
| `.claude/project/signals-steering.md` missing | Create `.claude/project/signals-steering.md` with commented explanation (see `signals-steering.md` subsection in Step 4). Never overwrite if it exists. |

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
- If file exists: read it. For each missing entry, append a new line (preserve trailing newline). Append only — preserve existing entries and their order.

```bash
# Example append (one entry, idempotent):
grep -qxF 'tmp/' .gitignore || echo 'tmp/' >> .gitignore
```

### `.signalsignore`

Refuse to overwrite if file exists (audit already gated this — defensive double-check).

Write the file only when `.signalsignore` is absent:

```bash
if ! test -f .signalsignore; then
  cat > .signalsignore << 'EOF'
# .signalsignore
#
# Augments .gitignore for the signals scan. Gitignored paths are
# already excluded automatically. This file is for TRACKED paths
# you want excluded from signals or flagged as generated.
#
# Two modes:
#   plain glob  → fully excluded from scan (not in tree at all)
#   + prefix    → appears in tree with [generated] flag (inferrer skips)
#
# One glob per line. Blank lines and # comments ignored.
#
# Examples:
#   third_party/**     ← committed but excluded from signals
#   fixtures/**        ← committed but excluded from signals
#   +dist/**           ← in tree, flagged [generated]
#   +*.pb.go           ← in tree, flagged [generated]
EOF
fi
```

### `signals-steering.md`

Refuse to overwrite if file exists (audit already gated this — defensive double-check).

Write the file only when `.claude/project/signals-steering.md` is absent:

```bash
if ! test -f .claude/project/signals-steering.md; then
  mkdir -p .claude/project
  cat > .claude/project/signals-steering.md << 'EOF'
# Signals steering
#
# User-provided hints for the signals inferrer. When this file exists,
# the inferrer reads it before writing signals.md and treats its
# content as ground truth — steering wins over detection when they
# conflict. Delete sections you don't need.
#
# ## Framework
# NestJS monorepo (not plain Express)
#
# ## Domains
# - src/billing/ and src/payments/ are one domain ("payments")
# - src/internal-tools/ is scratch code — not a real domain
#
# ## Build
# - Build: pnpm turbo build
# - Test: pnpm test:ci (not pnpm test — that runs watch mode)
#
# ## Ignore for domains
# - vendor/
# - generated/
EOF
fi
```

### `CLAUDE.md` survey

Refuse to overwrite if file exists (audit already gated this — defensive double-check).

**Seed every section with content from the original.** Every section is seeded with an agent guess; the user edits the guess. The point of project `CLAUDE.md` is durable intent, scope, tribal knowledge, rules, processes, and external references — content global `~/.claude/CLAUDE.md` cannot carry and project signals cannot infer. Do not duplicate global principles, "where things live", or the canonical workflow. Those load globally.

**Inputs the agent reads to form guesses** (in order, stop when enough signal):

1. `.claude/project/deterministic-signals.md` and `signals.md` if present.
2. `README.md`.
3. Top-level manifest files (`package.json`, `go.mod`, `pyproject.toml`, `Cargo.toml`, etc.) for purpose / language / domain hints.
4. `.github/workflows/`, `Makefile`, release scripts for processes.
5. Recent `git log --oneline -50` for commit style and rule signals.
6. `rg -n 'HACK|FIXME|XXX|WORKAROUND'` for tribal-knowledge candidates.

**Survey loop.** Walk the six sections below in order. For each:

1. Form the guess from the documented source.
2. If the guess is **non-empty** (the source returned real content), present it and ask `[a]ccept / [e]dit`. Skip is NOT offered — the agent already found durable signal, so the section gets written.
3. If the guess is **empty** (the source returned nothing actionable), present the fallback placeholder and ask `[a]ccept / [e]dit / [s]kip`. Skip writes the placeholder as the section body.

Accept → use as-is. Edit → user supplies replacement text. Skip (empty-guess path only) → render the one-line honest placeholder. Never an HTML comment. Never blank.

| # | Section | Guess source | If nothing inferable |
|---|---------|--------------|----------------------|
| 1 | **What this is** | First README paragraph + manifest `description` field + dominant language | "One-line purpose. Who uses it, who maintains it." prompt, asked of user |
| 2 | **Scope boundary** | Platform support comments (`claude.local.md`-style "macOS+Linux only"), CI matrix, language exclusions | Ask user explicitly: "What is this for? What is it deliberately NOT for?" |
| 3 | **Tribal knowledge** | `HACK`/`FIXME`/`XXX`/`WORKAROUND` comments with surrounding context; non-standard directory layout | "No surprising patterns detected. Add gotchas as they surface." |
| 4 | **Project rules** | Commit-message style from recent git log, lint config, pre-commit hooks, CI gates | "No repo-specific rules detected beyond global defaults." |
| 5 | **Processes** | `Makefile` targets, `.github/workflows/*.yml` job names, release scripts, `CONTRIBUTING.md` | "No release / rollback / on-call processes detected." |
| 6 | **External references** | URLs scraped from `README.md` matching Linear/Notion/Slack/Grafana/Sentry/Datadog domains | "No external references detected. Add Linear/Notion/Slack/dashboards as they arise." |

**Render.** Assemble the accepted/edited content into this skeleton, then write to `CLAUDE.md`:

````markdown
# CLAUDE.md


## What this is


<§1 content>


## Scope boundary


<§2 content>


## Tribal knowledge


<§3 content>


## Project rules


<§4 content>


## Processes


<§5 content>


## External references


<§6 content>


<atomic-signals>

## Project signals (auto-loaded)


@.claude/project/signals.md

</atomic-signals>
````

The `<atomic-signals>` block is appended unconditionally — even if signals haven't been scanned yet, the `@-ref` is forward-compatible (Claude tolerates missing `@-ref` targets). The tag makes the block swappable on refresh without touching user content. Only `signals.md` (the compact router) is `@-ref`'d. `deterministic-signals.md` is NOT — it can be thousands of lines on large repos and would blow up context. `signals-steering.md` is also NOT `@-ref`'d — it is read only during inference by the `atomic-signals-inferrer` agent.

**Content that belongs in the global file, not the project file:** These live globally already — duplicating them noise-pollutes the project file:

- Principles ("Think before coding", "Simplicity first", etc.)
- "Where things live" (scratchpad / docs/design / docs/spec / worktrees)
- Canonical workflow steps (Plan → Implement → Ship → Sync docs)
- Subagent roster
- Slash command catalog

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
Run /refresh-signals to generate project signals.
```

**`CLAUDE.md` missing `@-ref`** — Append to the existing `CLAUDE.md`:

```bash
if test -f CLAUDE.md && ! grep -qF '@.claude/project/signals.md' CLAUDE.md; then
  cat >> CLAUDE.md << 'EOF'

<atomic-signals>

## Project signals (auto-loaded)


@.claude/project/signals.md

</atomic-signals>
EOF
fi
```

Idempotent: only appends when `CLAUDE.md` exists AND `@-ref` is missing. Refuses silently otherwise.

## Step 5 — Report

Final state:

```
Applied:
  ✓ .gitignore updated: added tmp/, .claude/.scratchpad/, .worktrees/
  ✓ CLAUDE.md created via survey (N sections accepted, M edited, K skipped)
  ✓ docs/spec/ + docs/design/ created with .gitkeep

Skipped:
  • README.md (you said no)

Next steps:
  - Revisit CLAUDE.md as tribal knowledge accrues — skipped sections in particular.
  - Run /atomic-plan to start your first design or spec.
  - Commit when ready: /commit-only.
```

Delete no scratch (this command writes no scratchpad).

</workflow>

<constraints>

## Rules

- Never overwrite an existing file. The audit + the apply step both gate this.
- Never modify existing `.gitignore` entries. Only append missing ones.
- Never `git add` or commit. The user owns when to commit setup changes.
- Idempotent — running the command twice on a fresh-then-bootstrapped repo should report "already complete" the second time.
- If a step fails partway through (e.g. permission denied on `mkdir`), report which step failed and stop. Don't continue silently.

</constraints>

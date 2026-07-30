---
description: Bootstrap the current repo for atomic-claude use. Audits .gitignore entries, docs/ layout, and presence of CLAUDE.md. Proposes only what's missing — never overwrites. No commits.
---

You set up the repo for atomic-claude conventions. Detect first, propose second, apply only what the user confirms.

<workflow>

## Pre-flight

1. Run these two checks in parallel:
   - `git rev-parse --is-inside-work-tree 2>/dev/null`
   - `test -d wiki && echo wiki-present || echo wiki-absent`

2. If NOT in a git repo:
   - **`wiki/` directory present** → realm root confirmed. Set `WIKI_SCOPE=realm`;
     skip the init prompt and proceed to Scope detection.
   - **No `wiki/` directory** → prompt via `AskUserQuestion`:
     ```
     Not a git repo. Initialize one?
     - Yes, run git init
     - No, stop
     ```
     On Yes: `git init`. On No: refuse and stop.

## Scope detection

Determine `WIKI_SCOPE` (skip if pre-flight already set it to `realm`):

| git repo? | `wiki/` present? | `WIKI_SCOPE` |
|-----------|-----------------|-------------|
| No        | Yes             | `realm` (set in pre-flight) |
| Yes       | No              | `repo` |
| Yes       | Yes             | Ask user (see below) |

**Ambiguous case** — git repo with a `wiki/` directory present. Prompt via
`AskUserQuestion`:

```
A wiki/ directory exists inside this git repo. Which scope applies?
- realm — this directory is a realm root (wiki/ is the attached wiki repo)
- repo  — this is a single git repo with its own wiki/ storage
- unsure / cancel — treat as repo
```

Accept `realm` or `repo` as typed input or a button choice. If the user cancels,
answers ambiguously, or chooses "unsure / cancel", default to `repo` and note
`"wiki-type defaulted to repo (wiki/ present inside git repo — user did not confirm realm)"` in the Step 5 output.

`WIKI_SCOPE` is now set to either `repo` or `realm`. It is surfaced in the
Step 5 report only — the wiki pipeline (`/refresh-wiki`) writes the
`<wiki-type>` marker itself, into the wiki index it generates.

## Step 1 — Audit

Inspect the repo. Build this status table:

| Convention | Present? | Status |
|-----------|----------|--------|
| `.gitignore` exists | check `test -f .gitignore` | exists / missing |
| `.gitignore` has `tmp/` | grep `^tmp/?$` | yes / no |
| `.gitignore` has `.claude/.scratchpad/` | grep `^\.claude/\.scratchpad/?$` | yes / no |
| `.claude/worktrees/` ignored | `git check-ignore -q .claude/worktrees/probe` (rule may live in nested `.claude/.gitignore`) | yes / no |
| `CLAUDE.md` at repo root | `test -f CLAUDE.md` | exists / missing |
| `docs/` directory | `test -d docs` | exists / missing |
| `docs/spec/` directory | `test -d docs/spec` | exists / missing |
| `docs/design/` directory | `test -d docs/design` | exists / missing |
| `README.md` at repo root | `test -f README.md` | exists / missing |
| `atomic` binary on PATH | `command -v atomic` | found / missing |
| `SessionStart` hook registered in `.claude/settings.json` | parse `.claude/settings.json` (JWCC tolerated) and look for a `SessionStart` entry whose `hooks[].command` value is `atomic hooks session-start` (the inline command written by `atomic hooks install`) | registered / missing |
| `docs/wiki/index.md` | `test -f docs/wiki/index.md` | exists / missing |
| Signals `@-ref` wired | present in ANY of `claude.local.md`, `CLAUDE.local.md`, `CLAUDE.md` — check each with `grep -qF '@docs/wiki/index.md' <file>` (mirrors the `atomic-wiki-inferrer` search order); n/a only when none of the three files exist. Only `docs/wiki/index.md` (the compact router) is `@-ref`'d — `docs/wiki/scan.md` is too large for context on big repos and is read by the inferrer on demand. | yes / no / n/a |
| `.signalsignore` at repo root | `test -f .signalsignore` | exists / missing |
| `docs/wiki/CLAUDE.md` | `test -f docs/wiki/CLAUDE.md` | exists / missing |

Classify the repo:

- **fresh** — none of the conventions present (or only an empty `.gitignore`).
- **partial** — some present, some missing.
- **complete** — all present.

Print the audit table and the classification.

## Step 2 — Propose

For each missing item, propose an action. Skip items already present.

| Missing item | Proposed action |
|--------------|----------------|
| `.gitignore` missing, or any of `tmp/` / `.claude/.scratchpad/` / `.claude/worktrees/` not ignored, `atomic` binary present | Run `atomic repo init` — one idempotent call covers all three (creates `.gitignore` if absent) plus the `.claude/.scratchpad/` + `.claude/.atomic-index/` + `.claude/worktrees/` rules in nested `.claude/.gitignore` and the `.claude/.scratchpad/` + `.claude/project/` dirs. |
| `.gitignore` doesn't exist, `atomic` binary absent | Create with: `tmp/`, `.claude/.scratchpad/`, `.claude/worktrees/`. |
| `.gitignore` exists but missing `tmp/`, `atomic` binary absent | Append `tmp/`. |
| `.gitignore` exists but missing `.claude/.scratchpad/`, `atomic` binary absent | Append `.claude/.scratchpad/`. |
| `.claude/worktrees/` not ignored, `atomic` binary absent | Append `.claude/worktrees/` to root `.gitignore`. |
| `CLAUDE.md` missing | Run the survey procedure (see "CLAUDE.md survey" in Step 4). Seed every section with an agent guess from signals/README/code; user edits the guess. |
| `docs/spec/` missing | Create directory + `docs/spec/.gitkeep` (so git tracks it before any content lands). |
| `docs/design/` missing | Create directory + `docs/design/.gitkeep`. |
| `README.md` missing | Offer to scaffold a minimal starter. If user declines, skip — don't push it. |
| `atomic` binary missing | Print: `curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh \| bash`. Setup does not run the install — user runs the curl. |
| Registration missing, binary present | Run `atomic hooks install`. |
| Registration missing, binary missing | Manually add a `SessionStart` entry to `.claude/settings.json` whose `hooks[].command` is `atomic hooks session-start`. |
| Legacy wrapper-script registration present | Run `atomic hooks install` (migrates to the inline command and deletes the stale `session-start-reminders.sh` script). |
| `docs/wiki/index.md` missing but `atomic` present | Print: "Run `/refresh-wiki` to generate project signals." (follow-up only; setup does not invoke it). |
| `CLAUDE.md` exists but the `@-ref` is missing | Append the `## Project signals (auto-loaded)` section (see Signals subsection in Step 4). Skip this row when `CLAUDE.md` is missing — the starter template row handles that case. |
| `.signalsignore` missing | Create `.signalsignore` with commented explanation (see `.signalsignore` subsection in Step 4). Never overwrite if it exists. |
| `docs/wiki/CLAUDE.md` missing | Create `docs/wiki/CLAUDE.md` via `atomic wiki init --scope repo` (see `docs/wiki/CLAUDE.md` subsection in Step 4). Never overwrite if it exists. |

Present the proposed actions as a numbered list:

```
Proposed actions:
  [1] Run atomic repo init (scaffolds tmp/, .claude/.scratchpad/, .claude/worktrees/ ignore rules)
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

- **`atomic` binary present:** run `atomic repo init`. It creates `.gitignore` if missing and appends `tmp/` when not yet effective, plus (via nested `.claude/.gitignore`) `.claude/.scratchpad/`, `.claude/.atomic-index/`, and `.claude/worktrees/`. Idempotent — re-running once everything is in place is a no-op.
- **`atomic` binary absent:** fall back to manual append. If file missing: write a fresh one with the three lines. If file exists: read it. For each missing entry, append a new line (preserve trailing newline). Append only — preserve existing entries and their order.

```bash
# Manual fallback: append one entry, idempotent:
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

### `docs/wiki/CLAUDE.md`

```bash
atomic wiki init --scope repo
```

This is idempotent: it writes `docs/wiki/CLAUDE.md` with the default scaffold if the file does not exist, and no-ops silently if it already exists. On creation the command prints `created <path>` on stdout — relay that line, followed by `(edit to steer the inferrer).`

### `CLAUDE.md` survey

Refuse to overwrite if file exists (audit already gated this — defensive double-check).

**Seed every section with content from the original.** Every section is seeded with an agent guess; the user edits the guess. The point of project `CLAUDE.md` is durable intent, scope, tribal knowledge, rules, processes, and external references — content global `~/.claude/CLAUDE.md` cannot carry and project signals cannot infer. Do not duplicate global principles, "where things live", or the canonical workflow. Those load globally.

**Inputs the agent reads to form guesses** (in order, stop when enough signal):

1. `docs/wiki/index.md` and `docs/wiki/scan.md` if present.
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


@docs/wiki/index.md

</atomic-signals>
````

The `<atomic-signals>` block is appended unconditionally — even if signals haven't been scanned yet, the `@-ref` is forward-compatible (Claude tolerates missing `@-ref` targets). The tag makes the block swappable on refresh without touching user content. Only `docs/wiki/index.md` (the compact router) is `@-ref`'d. `docs/wiki/scan.md` is NOT — it can be thousands of lines on large repos and would blow up context. `docs/wiki/CLAUDE.md` is also NOT `@-ref`'d — it is read only during inference by the `atomic-wiki-inferrer` agent.

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
Run /refresh-wiki to generate project signals.
```

**`CLAUDE.md` missing `@-ref`** — Append to the existing `CLAUDE.md`, but only when
the ref is missing from all three candidate files (`claude.local.md`,
`CLAUDE.local.md`, `CLAUDE.md`) — a ref already present in the project-local
file counts as wired, mirroring the `atomic-wiki-inferrer` search order:

```bash
if grep -qF '@docs/wiki/index.md' claude.local.md 2>/dev/null || \
   grep -qF '@docs/wiki/index.md' CLAUDE.local.md 2>/dev/null || \
   grep -qF '@docs/wiki/index.md' CLAUDE.md 2>/dev/null; then
  : # already wired somewhere — nothing to do
elif test -f CLAUDE.md; then
  cat >> CLAUDE.md << 'EOF'

<atomic-signals>

## Project signals (auto-loaded)


@docs/wiki/index.md

</atomic-signals>
EOF
fi
```

Idempotent: only appends when `CLAUDE.md` exists AND the `@-ref` is missing from
all three candidate files. Refuses silently otherwise.

## Step 5 — Report

Final state:

```
Applied:
  ✓ .gitignore updated: added tmp/, .claude/.scratchpad/, .claude/worktrees/
  ✓ CLAUDE.md created via survey (N sections accepted, M edited, K skipped)
  ✓ docs/spec/ + docs/design/ created with .gitkeep
  • scope detected: repo (marker is written by /refresh-wiki into docs/wiki/index.md)

Skipped:
  • README.md (you said no)

Next steps:
  - Revisit CLAUDE.md as tribal knowledge accrues — skipped sections in particular.
  - Run /atomic-plan to start your first design or spec.
  - Commit when ready: /commit.
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

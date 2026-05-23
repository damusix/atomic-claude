# atomic-setup

## Goal

Bootstrap a fresh repo for atomic-claude use without polluting the project's `CLAUDE.md` with content the global `~/.claude/CLAUDE.md` already carries. Project `CLAUDE.md` must capture only what is durable, project-specific, and not inferable from signals: intent, scope boundary, tribal knowledge, project rules, processes, external references.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Survey procedure replaces the old starter-template dump | `commands/atomic-setup.md` | Manual: running `/atomic-setup` in a fresh repo walks the six-section survey, presents agent guesses, accepts user edits, writes a project-specific `CLAUDE.md` with zero global-principle duplication. |
| 2 | Render skeleton contains exactly six durable sections + signals `@-ref` block | `commands/atomic-setup.md` | Inspect output: headings are `What this is` / `Scope boundary` / `Tribal knowledge` / `Project rules` / `Processes` / `External references` / `Project signals (auto-loaded)`. No `Principles`, no `Where things live`, no `Workflow`. |
| 3 | Skip is conditional on empty guess; non-empty guesses force `[a]ccept / [e]dit` only | `commands/atomic-setup.md` | Run survey against a repo with real signal (Makefile targets, README URLs): skip option is NOT offered for §5 / §6. Run against a barren repo: skip IS offered, and produces one-line "No X detected" placeholder. |
| 4 | Guess sources documented per section | `commands/atomic-setup.md` | The Step 4 `CLAUDE.md survey` subsection lists guess sources for each of the six sections. |

## Problem statement

The prior starter template hard-coded a copy of the global `CLAUDE.md` (Principles, Where things live, Workflow). Every fresh project repo started with ~50 lines of content already auto-loaded from `~/.claude/CLAUDE.md`. The duplication noise-polluted project context, masked the actual project-specific signal, and signaled to future contributors that project `CLAUDE.md` is a generic boilerplate to ignore rather than a durable knowledge artifact.

Project `CLAUDE.md` exists to carry what global cannot:

- **Intent / direction** — what this project is trying to be, the durable north star.
- **Scope boundary** — what it is for, what it is deliberately NOT for.
- **Tribal knowledge** — gotchas, historical decisions, incidents that shaped design.
- **Project rules** — repo-specific overrides or extensions to global defaults.
- **Processes** — release, rollback, on-call, env setup quirks.
- **External references** — Linear / Notion / Slack / dashboards specific to this repo.

Signals capture the *shape* of the project (languages, structure, build commands). They cannot capture *intent* or *tribal knowledge*. That is the project `CLAUDE.md` niche.

## Non-goals

- Not a survey of the user's preferences (those belong in global memory or `atomic config`).
- Not a replacement for `docs/design/*` or `docs/spec/*` — those are per-feature workspaces.
- Not interactive past the initial bootstrap. `/atomic-setup` is one-shot; subsequent edits to `CLAUDE.md` are manual or via other verbs.
- Does not write an empty scaffold under any circumstance. The agent always guesses; the user always edits.

## Survey contract

Six sections, walked in order. For each:

1. Agent forms a guess from the documented guess source.
2. Agent presents the guess and prompts the user:
   - **Non-empty guess** (source returned real content) → prompt `[a]ccept / [e]dit` only. Skip is NOT offered — the agent already found durable signal, the section gets written.
   - **Empty guess** (source returned nothing actionable) → prompt `[a]ccept / [e]dit / [s]kip`, where the presented "guess" is the fallback placeholder.
3. `[a]ccept` → guess (or placeholder) used as section body.
4. `[e]dit` → user-supplied text used as section body.
5. `[s]kip` (empty-guess path only) → one-line honest placeholder used as section body. Never blank. Never an HTML comment.

The conditional-skip rule prevents users from accidentally discarding real signal the agent successfully inferred — every concrete finding lands in the file unless the user explicitly edits it out.

### Guess sources

| Section | Primary source | Secondary source | Skip placeholder |
|---------|----------------|------------------|------------------|
| What this is | README first paragraph + manifest `description` | Top-level dominant language + repo dir name | Ask user explicitly; do not skip — at minimum the agent prompts for one sentence |
| Scope boundary | Platform-support comments in repo (e.g. "macOS+Linux only"), CI matrix, language exclusions | Manifest `engines` / `os` fields | Ask user explicitly; do not skip |
| Tribal knowledge | `rg -n 'HACK\|FIXME\|XXX\|WORKAROUND'` with surrounding context | Non-standard directory layout, unusual structure | "No surprising patterns detected. Add gotchas as they surface." |
| Project rules | Recent `git log --oneline -50` commit-message style + lint config + pre-commit hooks + CI gates | `CONTRIBUTING.md` if present | "No repo-specific rules detected beyond global defaults." |
| Processes | `Makefile` targets, `.github/workflows/*.yml` job names, release scripts | `CONTRIBUTING.md`, `RUNBOOK.md`, `docs/runbook/` | "No release / rollback / on-call processes detected." |
| External references | URLs in `README.md` matching Linear / Notion / Slack / Grafana / Sentry / Datadog domains | URLs in `package.json` / `pyproject.toml` `urls` field | "No external references detected. Add Linear/Notion/Slack/dashboards as they arise." |

### Forbidden content in the rendered file

The renderer MUST NOT include any of the following — these load from global and duplicating them noise-pollutes the project file:

- Principles section
- "Where things live" section
- Canonical workflow (Plan → Implement → Ship → Sync docs)
- Subagent roster
- Slash command catalog

If a survey reviewer detects any of the above in a freshly-rendered `CLAUDE.md`, the implementation is non-conformant.

## Render skeleton

```markdown
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


## Project signals (auto-loaded)


@.claude/project/deterministic-signals.md
@.claude/project/signals.md
```

The trailing `## Project signals (auto-loaded)` block is appended unconditionally — even when signals have not yet been scanned, the `@-ref` is forward-compatible (Claude tolerates missing `@-ref` targets).

## Existing `CLAUDE.md` (no overwrite)

When `CLAUDE.md` already exists, the survey does not run. The audit step gates on `test -f CLAUDE.md`. If the file is present but missing the `@-ref` block, only the `## Project signals (auto-loaded)` block is appended (existing behavior preserved). The body is never touched.

## Rules

- Never overwrite an existing `CLAUDE.md`.
- Never render a blank section. Always either an accepted guess, a user edit, or an honest one-line placeholder.
- Never include forbidden content (principles, where-things-live, workflow, subagent roster, command catalog).
- Survey is interactive — runs once on greenfield, never re-runs.

## Change log

### 2026-05-23 — Initial spec

**What changed:** First spec written for `/atomic-setup`. Captures the survey-driven `CLAUDE.md` creation contract.

**Why:** Prior starter template at `commands/atomic-setup.md:188-236` hard-coded a duplicate of global `CLAUDE.md` content (Principles, Where things live, Workflow). Project `CLAUDE.md` files created via `/atomic-setup` were generic boilerplate, not durable project knowledge. User flagged that project `CLAUDE.md` should carry intent / tribal knowledge / direction / rules / processes — content signals cannot infer and global does not own.

### 2026-05-23 — Correction: stale signals @-ref in CLAUDE.md scaffold

**Correction:** The `## Project signals (auto-loaded)` scaffold block wrote `@.claude/project/inferred-signals.md` but the signals router renamed the file to `signals.md`. Corrected to `@.claude/project/signals.md`. This is load-bearing — freshly bootstrapped repos would get a broken `@-ref` pointing to a file that never exists.

### 2026-05-23 — Conditional skip

**What changed:** `[s]kip` is now offered only when the section's guess is empty. When the guess source returned real content, the prompt narrows to `[a]ccept / [e]dit`.

**Why:** Dry-run against a fresh fixture (Makefile with `release:` + `rollback:` targets, README with Notion + Slack references) surfaced the defect: skip was offered even when the agent had real signal, letting the user accidentally discard durable findings in favor of a "No X detected" placeholder. Skip exists to handle the genuinely-empty case, not to nullify successful inference.

**Superseded:** Prior contract offered `[a]ccept / [e]dit / [s]kip` unconditionally for every section.

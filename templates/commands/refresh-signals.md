---
description: Refresh project signals on demand (initializes on first run). Re-runs atomic signals scan and dispatches the inferrer to update both deterministic and inferred signals files. Use when you want a deliberate refresh rather than waiting for the atomic-signals skill to auto-fire on natural language.
---

## Step 1 — Pre-flight

```bash
git rev-parse --is-inside-work-tree
```

If not inside a git repo, stop:

```
not a git repo. refresh-signals requires a git repository.
```

```bash
command -v atomic
```

If `atomic` is not on `$PATH`, stop:

```
atomic binary not found on $PATH.
install: curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash
then re-run /refresh-signals.
```

## Step 2 — Bootstrap check

```bash
test -f .claude/project/deterministic-signals.md
```

If the file does not exist, this is a first-time initialization. Print:

```
no existing signals found. initializing from scratch.
```

Continue to Step 3 (scan) — do not stop.

## Step 3 — Scan

Run:

```bash
atomic signals scan
```

This writes `.claude/project/deterministic-signals.md`. On completion, print the section headers:

```bash
grep '^## ' .claude/project/deterministic-signals.md
```

## Step 4 — Dispatch inferrer

Invoke the `atomic-signals` skill. It owns: dispatch of `atomic-signals-inferrer` for the signals router file, and verification that `CLAUDE.md` `@`-references both signals files.

If signals files are missing from `CLAUDE.md` (first-time run), the skill handles wiring — see Step 5.

## Step 5 — CLAUDE.md wiring (first-time only)

The `atomic-signals` skill checks whether `@-refs` are present in `CLAUDE.md` (or `claude.local.md` / `CLAUDE.local.md`). If already wired, nothing happens. If missing, the skill wires them.

If no `CLAUDE.md` exists at all, ask via `AskUserQuestion`:

```
No CLAUDE.md found. Create a starter with signals @-refs?
- Yes (writes minimal starter with @-refs)
- No, skip
```

On "Yes": write a minimal `CLAUDE.md` at repo root containing only:

```markdown
## Project signals (auto-loaded)

@.claude/project/deterministic-signals.md
@.claude/project/signals.md
```

On "No, skip": continue without creating.

## Step 6 — Report

Print final state:

```
signals <refreshed | initialized>.

  deterministic: .claude/project/deterministic-signals.md
  signals:       .claude/project/signals.md
  CLAUDE.md:     <updated with @-refs | unchanged (already wired) | not created (skipped)>

suggested next step:
  git add .claude/project/deterministic-signals.md .claude/project/signals.md
  (and CLAUDE.md if modified)
  then: /commit-only
```

Use "initialized" if Step 2 found no existing signals; "refreshed" otherwise.

---

## Steering

If `.claude/project/signals-steering.md` exists, the inferrer reads it as authoritative user hints. Use it to correct misdetected frameworks, override domain groupings, specify exact build/test commands, or exclude paths from domain classification. Steering wins over inference when they conflict.

Example:

```markdown
## Framework
NestJS monorepo (not plain Express)

## Domains
- src/billing/ and src/payments/ are one domain ("payments")
- src/internal-tools/ is scratch code — not a real domain

## Build
- Build: pnpm turbo build
- Test: pnpm test:ci (not pnpm test — that runs watch mode)
```

---

## Rules

- Stop on pre-flight failure. Never continue past a missing git repo or missing binary.
- Idempotent. Safe to run repeatedly — first run initializes, subsequent runs refresh.
- Never modify `CLAUDE.md` without confirmation when creating it from scratch. Appending `@-refs` to an existing `CLAUDE.md` is handled by the skill without prompting (it's non-destructive).
- Never commit. The user stages and commits the regenerated files.
- The skill keeps `@-refs` to signals present in `CLAUDE.md` so future sessions auto-load them. That wiring is part of refresh, not a separate concern.

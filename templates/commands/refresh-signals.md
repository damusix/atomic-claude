---
description: Refresh project signals on demand. Re-runs atomic signals scan and dispatches the inferrer to update both deterministic and inferred signals files. Use when you want a deliberate refresh rather than waiting for the atomic-signals skill to auto-fire on natural language.
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

If the file does not exist, signals were never initialized. Stop and redirect:

```
no signals to refresh — .claude/project/deterministic-signals.md missing.
run /initialize-signals first to bootstrap.
```

## Step 3 — Invoke the skill

Invoke the `atomic-signals` skill. It owns the full refresh flow: `atomic signals scan` for the deterministic file, dispatch of `atomic-signals-inferrer` for the inferred file, and verification that `CLAUDE.md` already `@`-references both.

## Step 4 — Report

After the skill completes, print:

```
signals refreshed.

  deterministic: .claude/project/deterministic-signals.md
  inferred:      .claude/project/inferred-signals.md

suggested next step:
  git add .claude/project/deterministic-signals.md .claude/project/inferred-signals.md
  then: /commit-only
```

---

## Rules

- Stop on pre-flight failure. Never continue past a missing git repo or missing binary.
- Refuse if signals were never initialized. `/refresh-signals` is for updating existing signals, not bootstrapping — that's `/initialize-signals`'s job.
- Delegate the actual work to the `atomic-signals` skill. This command is a thin, explicit-only entry point; the skill owns scan + inferrer + CLAUDE.md wiring.
- The skill keeps `@-refs` to signals present in `CLAUDE.md` so future sessions auto-load them. That wiring is part of refresh, not a separate concern — without it the snapshot exists but Claude never reads it.
- Never commit. The user stages and commits the regenerated files.

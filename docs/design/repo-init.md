# Design: `atomic repo init` — deterministic `.claude/` scaffolding

GitHub issue: #125. Two coupled concerns: (1) `.claude/` filesystem setup is scattered across
command prose and runs lazily on first use — deterministic work code should own; (2) this repo's
root `.gitignore` carries fifteen `.claude/*` lines (eight negations across five groups) that
belong in a nested `.claude/.gitignore`.

## Problem

- No verb guarantees `.claude/.scratchpad/` exists in a cold repo; the first command that needs it
  (`/subagent-implementation`, `/remind-me`, `/subagent-diagnose`) does its own `mkdir` and its own
  "verify gitignored, append if missing" prose. Each site re-derives the same layout contract, and
  an LLM performs deterministic filesystem work.
- Root `.gitignore` mixes repo concerns with `.claude/` concerns. A nested `.claude/.gitignore`
  scopes the rules to the directory they govern and guarantees ignore-before-negate ordering.

## Shape

New verb: `atomic repo init`. New internal package: `atomic/internal/repoinit` (mirrors
`dockerinit` wiring: internal package → `main.go` dispatch → `cliusage` entry).

The verb is **idempotent and non-destructive**: it creates what is missing, never rewrites or
removes anything a user wrote, and running it twice is a no-op.

What it guarantees:

| Item | Mechanism |
|------|-----------|
| `.claude/.scratchpad/` exists | `mkdir -p` |
| `.claude/project/` exists | `mkdir -p` |
| `.claude/.scratchpad/` ignored | managed rule `/.scratchpad/` in `.claude/.gitignore` |
| `.claude/.atomic-index/` ignored | managed rule `/.atomic-index/` in `.claude/.gitignore` |
| `tmp/` ignored | root `.gitignore` line `tmp/` |
| `.claude/worktrees/` ignored | managed rule `/worktrees/` in `.claude/.gitignore` |

## Key decisions

**Effect-based idempotency, line-based append.** "Is this rule needed?" is answered by
`git check-ignore -q <probe path>` — not by literal line matching. A repo whose root `.gitignore`
already carries `tmp/*` (this repo) or `.claude/.scratchpad/` (repos bootstrapped by the old
`/setup-wiki` audit) must not receive duplicate or conflicting lines. Only when the effect is
missing does init append the literal managed line. When git is unavailable (no binary, not a work
tree), degrade to literal line-presence checks — named exception: the deterministic path (git)
may be absent, so a weaker fallback is deliberate.

**Minimal managed rule set.** The nested `.claude/.gitignore` scaffold carries only rules the
atomic ecosystem itself owns: `/.scratchpad/` and `/.atomic-index/`. It deliberately does NOT
manage `settings.local.json` — that file is Claude Code's concern, and in repos that deliberately
track it (this one does, via a `!` negation) an appended ignore rule would flip the negation
(last match wins). Same reasoning excludes `agents/`, `commands/`, `skills/`: user repos may
commit project-local artifacts there.

**Nested file created only when needed.** If every managed rule is already effective and both
directories exist, init changes nothing — it does not create an empty `.claude/.gitignore` for
show. Output reports each item as `created` or `ok`.

**Init never commits.** The caller (a command template, or the user) owns the commit, matching
`atomic wiki init` / `atomic docker init`.

**No new doctor check.** Issue scope asks for existing gates to stay green, not for new
diagnostics. A repo that never ran init keeps working — every consumer retains a degradation path.

## This repo's migration (dogfood)

One-time, by hand (not by the verb): move the fifteen `.claude/*` lines from root `.gitignore`
into `.claude/.gitignore`, translated to be relative to `.claude/` and anchored with a leading `/`
so depth semantics are preserved exactly. The fifteen lines sit in three spans of the root file —
`.claude/.scratchpad/` alone (the root-resident `.worktrees/` line follows it), thirteen contiguous
lines, and a separate `.claude/.atomic-index/` line further down:

```
/.scratchpad/
/agents/
/commands/*
!/commands/triage-issues.md
/output-styles/
/skills/*
!/skills/atomic-cli-contrib/
!/skills/atomic-cli-contrib/**
!/skills/atomic-release-ci/
!/skills/atomic-release-ci/**
/rules/*
!/rules/authoring/
!/rules/authoring/**
!/settings.local.json
/.atomic-index/
```

Negation ordering is preserved (ignore `X/*`, then negate the child dir, then its contents — the
structure git requires for re-inclusion). Verification: `git status --porcelain` identical before
and after, and a `git check-ignore` matrix over representative paths (ignored: `.claude/agents/x`,
`.claude/commands/other.md`, `.claude/skills/other/x`, `.claude/rules/other/x`,
`.claude/.scratchpad/x`, `.claude/.atomic-index/x`; not ignored: `.claude/commands/triage-issues.md`,
`.claude/skills/atomic-cli-contrib/SKILL.md`, `.claude/rules/authoring/axioms.md`,
`.claude/.gitignore` itself) produces identical results.

## Template strip

Once init owns the layout, command templates stop doing ad-hoc gitignore setup. The rule:

- **Removed from templates:** all "verify X is gitignored, append if missing" prose and standalone
  layout-mkdir steps. Each such site instead runs `atomic repo init` best-effort
  (`command -v atomic` guard; silent skip when absent).
- **Retained in templates:** `mkdir -p` of the task-specific leaf directory a command is about to
  write (`.claude/.scratchpad/<date>-<topic>/`, `reminders/`). This is the degradation path for
  repos where the binary is absent — named exception, kept deliberately.

Sites: `subagent-implementation.md` (gitignore-verify step), `subagent-diagnose.md` (two
gitignore-verify steps), `setup-wiki.md` (gitignore audit rows delegate to init),
`templates/shared/worktree-setup.md` (prefer init; keep check-ignore + append + commit fallback,
since that partial must commit the change it makes).

## Alternatives considered

- **Extend `/setup-wiki` (slash command) instead of a binary verb** — rejected: keeps the work
  in LLM prose; the issue's motivation is exactly to move it to code.
- **`atomic claude init`** — rejected: the `claude` namespace operates on `~/.claude`
  (user-global); this verb is repo-scoped. `repo` is a new, accurate namespace.
- **Managed block with markers in `.gitignore`** (like the `<wikis>` block) — rejected: overkill
  for append-only guarantees; effect-based checks make ownership markers unnecessary.

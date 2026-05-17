---
description: One-shot bootstrap for a project that has never had signals generated. Runs atomic signals scan, dispatches the inferrer, and wires claude.md @-refs. Idempotent — delegates to the atomic-signals skill if already initialized.
---

## Step 1 — Pre-flight

```bash
git rev-parse --is-inside-work-tree
```

If not inside a git repo, stop:

```
not a git repo. initialize-signals requires a git repository.
```

```bash
command -v atomic
```

If `atomic` is not on `$PATH`, stop:

```
atomic binary not found on $PATH.
install: curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash
then re-run /initialize-signals.
```

## Step 2 — Idempotent check

```bash
test -f .claude/project/deterministic-signals.md
```

If the file exists, print:

```
signals already initialized. running refresh instead.
```

Then invoke the `atomic-signals` skill and stop. Do not continue with the steps below.

## Step 3 — Scan

Run:

```bash
atomic signals scan
```

This writes `.claude/project/deterministic-signals.md`. On completion, print the section headers from the resulting file (not full content):

```bash
grep '^## ' .claude/project/deterministic-signals.md
```

## Step 4 — Dispatch inferrer

Spawn the `atomic-signals-inferrer` subagent via the `Agent` tool. Wait for completion before proceeding.

Prompt to pass:

```
First-run inference. .claude/project/deterministic-signals.md has just been written by `atomic signals scan`.
No prior inferred-signals.md exists. Read the deterministic signals file end-to-end and write .claude/project/inferred-signals.md from scratch.
Follow the output schema in your system prompt. Every claim must cite evidence. Risks / unknowns must be non-empty.
```

## Step 5 — claude.md handling

Detect the project's `claude.md`:

```bash
test -f claude.md
```

**File exists:**

Check whether both `@-refs` are already present:

```bash
grep -q '@\.claude/project/deterministic-signals\.md' claude.md && \
  grep -q '@\.claude/project/inferred-signals\.md' claude.md
```

If both refs are found (exit 0), print:

```
claude.md already references both signals files. no changes needed.
```

Skip the rest of this step.

Otherwise, print the section that would be appended:

```
--- proposed append to claude.md ---

## Project signals (auto-loaded)

@.claude/project/deterministic-signals.md
@.claude/project/inferred-signals.md
---
```

Ask via `AskUserQuestion`:

```
Append the auto-load section to claude.md?
- Yes
- Show me the diff again
- No, skip
```

On "Yes": append the section exactly as shown above (preceded by two blank lines).
On "Show me the diff again": reprint the proposed append and re-ask.
On "No, skip": continue without modifying `claude.md`.

**File missing:**

Ask via `AskUserQuestion`:

```
No claude.md found. Create a starter?
- Yes (writes minimal starter with @-refs)
- No, skip
```

On "Yes": write a minimal starter `claude.md` at repo root containing only the auto-load section (heading `## Project signals (auto-loaded)` plus the two `@-refs`). The user can run `/atomic-setup` later for a fuller scaffold — `/initialize-signals` only writes what it owns.
On "No, skip": continue.

## Step 6 — Report

Print final state and commit suggestion:

```
signals initialized.

  deterministic: .claude/project/deterministic-signals.md
  inferred:      .claude/project/inferred-signals.md
  claude.md:     <updated with @-refs | unchanged (skipped) | unchanged (already wired)>

suggested next step:
  git add .claude/project/deterministic-signals.md .claude/project/inferred-signals.md
  (and claude.md if modified)
  then: /commit-only
```

---

## Rules

- Stop on pre-flight failure. Never continue past a missing git repo or missing binary.
- Idempotent. If `.claude/project/deterministic-signals.md` exists, always delegate to the skill — never re-run scan or overwrite.
- Never modify `claude.md` without the `AskUserQuestion` confirm. The confirm is required even if the diff is trivial.
- If `claude.md` already contains both `@-refs`, take no action and say so — do not re-append.
- Never commit. The user owns when to stage and commit the resulting files.
- Print `atomic signals scan` output headers, not full content — the file can be large.

---
description: Refresh project signals on demand (initializes on first run). Dispatches the atomic-signals-inferrer agent to scan, infer, and wire signals files.
---

<workflow>

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

Set `first_run=true`. Continue to Step 3.

## Step 3 — Steering file check

```bash
test -f .claude/project/signals-steering.md
```

If the file does not exist, create it with the default scaffold:

```bash
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
```

Print: `created .claude/project/signals-steering.md (edit to steer the inferrer).`

If the file already exists, read its contents for the dispatch prompt.

## Step 4 — Code-intel index lifecycle

Ensure the code-intel index is current before dispatching the inferrer, so domain clustering can use real dependency edges rather than filename heuristics alone.

- If `atomic` is not on `$PATH` (already checked in Step 1 above): skip this step silently and proceed. The inferrer degrades to heuristic-only clustering.
- If `.claude/.atomic-index/atomic.db` **exists** (warm index): run `atomic code sync` to bring it up to date with the current working tree. This is incremental and cheap. On sync error, print a warning and proceed degraded — a stale index is still useful and never blocks the refresh.
- If `.claude/.atomic-index/atomic.db` **does not exist** (cold — no index yet): ask via `AskUserQuestion`:

  ```
  No code index found. Build one now so signals can use the real dependency graph?
  First index can take seconds to minutes depending on repo size.
  ```

  Options:
  - "Yes, build index" → run `atomic code index`. On completion, continue to Step 5.
  - "No, use heuristics" → proceed to Step 5 without an index.

  The offer fires each run until accepted. The decline is not persisted — memory-of-decline is a deferred follow-up out of scope here.

A missing or declined index never blocks the refresh. The inferrer uses filename/path heuristics in that case and produces valid signals — the index is an enhancement, not a requirement.

## Step 5 — Dispatch agent

Dispatch the `atomic-signals-inferrer` agent via the `Agent` tool. Build the prompt:

```
mode: interactive
first_run: <true if Step 2 found no existing signals, false otherwise>

<steering>
<contents of signals-steering.md, if it exists and is not all comments>
</steering>
```

Wait for the agent to complete. As its final action the agent runs `atomic signals linkify`, so the written signals files render path citations as navigable relative markdown links (idempotent; not `@-refs`). No separate invocation is needed here.

## Step 6 — Surface concerns

If the agent returned a `## Concerns` table (judgment observations found during inference), present them to the user as a numbered list. Ask via `AskUserQuestion`: "The signals scan found N potential issues. Create follow-ups for any?" with options:

- "All" — create follow-ups for every concern via `atomic followups add`
- "Pick" — print the indexed list, accept space/comma-separated indices, create only those
- "Skip" — discard, no follow-ups created

## Step 7 — No CLAUDE.md at all

If no `CLAUDE.md` exists after the agent runs (the agent could not wire the `@-ref`), ask via `AskUserQuestion`:

```
No CLAUDE.md found. Create a starter with signals @-ref?
- Yes (writes minimal starter with @-ref)
- No, skip
```

On "Yes": write a minimal `CLAUDE.md` at repo root containing only:

```markdown
<atomic-signals>

## Project signals (auto-loaded)

@.claude/project/signals.md

</atomic-signals>
```

On "No, skip": continue without creating.

## Step 8 — Report

Print final state:

```
signals <refreshed | initialized>.

  deterministic: .claude/project/deterministic-signals.md
  signals:       .claude/project/signals.md
  CLAUDE.md:     <updated with @-refs | unchanged (already wired) | not created (skipped)>

suggested next step:
  git add .claude/project/deterministic-signals.md .claude/project/signals.md
  (and CLAUDE.md if modified)
  then: /commit
```

Use "initialized" if Step 2 found no existing signals; "refreshed" otherwise.

</workflow>

<constraints>

## Rules

- Stop on pre-flight failure. Never continue past a missing git repo or missing binary. **Why:** the agent depends on both — proceeding produces broken output.
- Idempotent. Safe to run repeatedly — first run initializes, subsequent runs refresh.
- Never commit. The user stages and commits the regenerated files. **Why:** signals refresh is a side effect of the user's work — they decide when to commit.

</constraints>

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

---
description: Orchestrate implement→review subagent loop until task complete. Reads the approved spec, writes a thin brief to .claude/.scratchpad/, dispatches fresh-context subagents, loops until reviewer signs off, commits per green iteration, then updates repo docs.
---

You are the **orchestrator**. The user has given you a task. You will NOT implement it yourself. You drive a loop of fresh-context subagents until the task is done, then update documentation.

<workflow>

## Phase 0 — Understand

1. Read the user's task. If anything is genuinely ambiguous and would block work, ask consolidated questions. Otherwise proceed.
2. **Dispatch `atomic-investigator` first** to locate the relevant surface area — files, call sites, existing tests, conventions. Haiku-backed and read-only, so it's cheap. Pass a focused brief (e.g. `Map src/auth/. Find: token generation, validation, refresh. Report file:line table.`). When a code-intel index is warm, tell the investigator to lead with `atomic code explore "<surface-area query>"` (one shot returns the relevant symbols, files, and relationships) before targeted verbs or `sg`/`grep` — it carries the `agent-code-intel` partial, but the brief should name the area to explore. Use its `file:line — what` table as your scoping evidence — do NOT duplicate the search yourself in the main context. The investigator's job is to spend Haiku tokens so Sonnet builder/reviewer dispatches start with a precise target.
3. Read only the files the investigator flagged that you need to make scoping decisions (spec gate, agent choice, iteration breakdown). Do NOT start implementing.

Skip the investigator only when the task names exact files and there's nothing to locate (e.g. "fix the typo in README.md line 42"). When in doubt, dispatch it — its cost is trivial compared to a misaimed builder.

## Spec gate

Derive the topic slug from the task: short kebab-case (e.g. `oauth-refresh`, `user-search-perf`).

Check for `docs/spec/<topic>.md`:

- **Spec exists** → use it as the canonical brief source. Skip to the Worktree gate.
- **No spec, task is <30 min of obvious work** → proceed inline. State the assumption: `no spec; proceeding inline because task is small/obvious.` Skip to the Worktree gate.
- **No spec, task is non-trivial** → refuse. Tell user: `Run /atomic-plan first. I need an approved spec at docs/spec/<topic>.md before launching the implementation loop.` Stop.

Bar for "non-trivial": touches ≥3 files, introduces new architectural patterns, or has any ambiguity about success criteria. When in doubt, require the spec.

**Currency gate — the spec body must reflect the current decision before any dispatch.** Subagents read the spec body verbatim as ground truth (the `BRIEF.md` points them straight at it), so stale content makes them build the wrong thing. If a decision in this conversation has superseded any part of the spec — a cut feature, a changed checkpoint, a dropped success criterion — **update the spec body first** (rewrite the affected sections, log the change per the `CLAUDE.md` spec rule), then build the brief from the corrected spec. Never paper over a superseded spec section with brief wording; fix the source. Test before dispatching: could a fresh subagent reading only the spec body build something a later decision already cut? If yes, fix the spec, not the brief.

## Worktree gate

<worktree-setup>

## Detect existing isolation

Run in parallel:

```bash
GIT_DIR=$(cd "$(git rev-parse --git-dir)" 2>/dev/null && pwd -P)
GIT_COMMON=$(cd "$(git rev-parse --git-common-dir)" 2>/dev/null && pwd -P)
SUPERPROJECT=$(git rev-parse --show-superproject-working-tree 2>/dev/null)
CURRENT_BRANCH=$(git branch --show-current 2>/dev/null)
```

Submodule guard: if `--show-superproject-working-tree` returns a non-empty path, treat as a normal repo — not a worktree.

If `$GIT_DIR != $GIT_COMMON` (and not a submodule) → already in a worktree. Print:

```
already isolated at <pwd> on branch <branch>. Skipping creation.
```

Continue in place with the current working tree. Skip all steps below.

## Decide whether to create (ask-if-unspecified / auto-create)

**Interactive mode (ask-if-unspecified):** if the caller has not already decided, ask via `AskUserQuestion`:

```
Significant work ahead. Use an isolated worktree?
- Yes, new branch → create .worktrees/<derived-name>/
- No, work in place
```

On `No`: continue in place. Skip all steps below.

**Hands-off mode (auto-create):** skip the question and proceed to branch resolution.

## Resolve the branch name

The branch name is passed by the caller (e.g. a topic slug derived from the spec or task). It must match `^[a-z0-9][a-z0-9/-]*$`. If no name is available, derive one: kebab-case slug of the first ~6 words of the task description.

## Verify .worktrees/ is gitignored

```bash
git check-ignore -q .worktrees
```

If exit code is non-zero (not ignored):

- Append `.worktrees/` to `.gitignore` (create at repo root if missing).
- Invoke the `atomic-commit` skill.
- Stage `.gitignore` explicitly by path.
- Commit with message `chore: gitignore .worktrees/`.

## Carry forward an in-context spec or design (optional)

A worktree branches from `HEAD`. Uncommitted spec or design files in the source working tree do not follow — if the implementer subagent reads `docs/spec/<topic>.md` from the worktree and the file isn't there, the loop fails before iteration 1.

Detect carry-over candidates:

- A spec path was passed by the caller, and `git status --porcelain -- <path>` reports it as untracked or modified.
- The current conversation produced a `docs/spec/*.md` or `docs/design/*.md` that is untracked or modified, and its basename matches or is closely related to the branch name.

For each candidate (interactive mode only — skip silently in hands-off mode): ask via `AskUserQuestion`:

```
Spec `<path>` is uncommitted. Commit it before creating the worktree so
the branch carries it forward?
```

Options: `commit now (recommended)` / `skip`. On `commit now`:

- Invoke the `atomic-commit` skill for the message.
- Stage the file explicitly by path. Do not `git add -A`.
- Commit on the current branch (typically `main`).

In hands-off mode: if a spec candidate is found, commit it automatically without prompting (the caller authorized autonomy).

Only `docs/spec/` and `docs/design/` files qualify here.

## Verify branch does not already exist

```bash
git rev-parse --verify <branch>
```

If this succeeds (exit 0), refuse:

```
branch <name> already exists. pick a different name or checkout existing.
```

Stop.

## Create the worktree

```bash
git worktree add .worktrees/<branch> -b <branch>
```

If this fails with a permission or sandbox error, print:

```
sandbox blocked worktree creation. working in place.
```

Continue in place — do not run setup or tests.

## Auto-detect and run setup

Run all detection from inside `.worktrees/<branch>/`. Check files in this order:

- `pnpm-lock.yaml` exists alongside `package.json` → `pnpm install`
- `yarn.lock` exists alongside `package.json` → `yarn install`
- `package.json` exists → `npm install`
- `Cargo.toml` exists → `cargo build`
- `requirements.txt` exists → `pip install -r requirements.txt`
- `poetry.lock` exists alongside `pyproject.toml` → `poetry install`
- `pyproject.toml` exists → `pip install -e .`
- `go.mod` exists → `go mod download`
- None matched → skip setup, note as skipped

If the setup command fails with a network or permission error, note `setup skipped (sandboxed or no network)` and continue.

## Run baseline tests

Detect the test command from inside `.worktrees/<branch>/`:

- `pnpm-lock.yaml` + `package.json` with `test` script → `pnpm test`
- `yarn.lock` + `package.json` with `test` script → `yarn test`
- `package.json` with `test` script → `npm test`
- `Cargo.toml` → `cargo test`
- `pytest.ini` present, or `pyproject.toml` contains `[tool.pytest` → `pytest`
- `go.mod` → `go test ./...`
- None matched → skip, note as skipped

If tests fail: in interactive mode, list each failure, then ask whether to proceed or investigate before continuing. In hands-off mode, list failures in `STATE.md` and proceed (the reviewer will catch regressions).

## Report

```
Worktree: .worktrees/<branch>/
Branch:   <branch>
Setup:    <command run> | skipped (no manifest) | skipped (sandboxed)
Baseline: <N> tests pass | <N> failures | skipped
Ready.
```

</worktree-setup>

For tasks classified as obviously small in the Spec gate, skip the worktree question entirely.

## Code-intel index lifecycle

Before writing the brief, ensure the code-intel index is fresh so subagents can query the dependency graph during this task.

Check whether the index DB exists:

```bash
test -f .claude/.atomic-index/atomic.db
```

- **Warm (DB exists):** run `atomic code sync` to bring the index up to date with the current working tree. Skip silently if `atomic` is absent or errors.
- **Cold (no DB):** offer to build the index via `AskUserQuestion`:

    ```
    No code-intel index found (.claude/.atomic-index/atomic.db). Build one now so builders
    and reviewers can use the dependency graph for this task? First index can take
    seconds to a few minutes depending on repo size.
    - Yes — run `atomic code index`
    - No — proceed; subagents fall back to sg/grep
    ```

    On Yes: run `atomic code index`. On No (or if `atomic` is absent, or if indexing errors): proceed without the index — subagents degrade to sg/grep automatically.

The index lifecycle is orchestrator-owned. Subagents never trigger indexing. A missing or declined index never blocks the loop.

## Phase 1 — Write brief to `$SCRATCH`

Pick the working dir: `.claude/.scratchpad/<YYYY-MM-DD>-<topic>/`. Use today's date.

```bash
SCRATCH=".claude/.scratchpad/$(date +%Y-%m-%d)-<topic>"
mkdir -p "$SCRATCH"
```

`.claude/.scratchpad/` must be gitignored — verify, add if missing.

Write two files inside `$SCRATCH`:

### `$SCRATCH/BRIEF.md`

Thin orchestrator-curated brief. Contents:

```markdown
# Brief: <topic>

**Spec:** `docs/spec/<topic>.md` (canonical source — read this first)

**Iteration scope (this turn):** <which checkpoint from the spec>

**Reviewer feedback to address:** <findings from prior iteration, or "N/A — first iteration">

**Success criteria for this iteration:**
- <criterion>
- <criterion>

**Base SHA for diff:** <git rev-parse HEAD>
```

Refreshed each iteration — overwrite, don't append.

### `$SCRATCH/STATE.md`

Append-only iteration log:

```markdown
# State: <topic>

## Iteration 1 — <date>
- Implementer: <one-line summary>
- Reviewer: <verdict + key findings>
- Decisions: <anything load-bearing>
- Commit: <sha or "deferred">

## Iteration 2 — <date>
...
```

### `$SCRATCH/FOLLOWUPS.md`

Ledger of non-blocking reviewer findings (🟡 risk / 🔵 nit / ❓ question) — anything that didn't block the iteration's PASS but is worth a deliberate decision before final ship. Append after every reviewer pass that returns findings; do NOT discard them just because the verdict was PASS.

Initialize on first iteration with this structure:

```markdown
# Follow-ups: <topic>

Non-blocking findings carried across iterations. At finalization (Phase 3): review with the user, decide what to fix in a polish pass, what to defer to a tracked issue, what to drop.

---

## 🟡 risks

### F-1 — <one-line title>

`<path:line>`

<problem + suggested fix in 1-3 sentences>

Origin: iteration <N> reviewer.

## 🔵 nits

### F-N — <title>
...
```

Numbering is sequential across all severities (F-1, F-2, F-3...). When a follow-up gets closed in a later iteration, mark `*(closed iter N — <commit-sha>)*` next to its title and keep the entry for traceability — don't delete it.

That's it. No GOAL.md, no CONTEXT.md, no PLAN.md — the spec at `docs/spec/<topic>.md` IS those. The scratchpad is a thin handoff plus a deliberate-decision ledger, not a duplicate.

## Phase 2 — Implement → Review → Commit loop

Repeat until reviewer signs off or a stop condition fires. Two stop conditions:

- **Stuck-fix escalation** (Step C): after 2 consecutive `CHANGES_REQUESTED` rounds on the same blocking signal → surface `/pressure-test` and `atomic-strategist` RCA options; wait for user choice before looping.
- **6-iteration soft-stop**: at 6 iterations regardless of signal state → ask user before continuing.

### Step A — Dispatch implementer (fresh context)

Pick the agent based on iteration scope:

- **`atomic-implementer (mode: surgical)`** when scope touches ≤2 files and is mechanically obvious (typo, single-fn rewrite, rename, single-callsite fix).
- **`atomic-implementer (mode: feature)`** for feature checkpoints — one cohesive slice, however many files.
- **`general-purpose`** as fallback if neither fits.

Build the implementer prompt by reading `commands/_templates/implementer-prompt.md` and substituting:

| Placeholder | Value |
|-------------|-------|
| `{SCRATCH_PATH}` | absolute path to `$SCRATCH` |
| `{SPEC_PATH}` | absolute path to `docs/spec/<topic>.md` (or `"no spec — inline brief in BRIEF.md"`) |
| `{ITERATION_SCOPE}` | this iteration's scope from BRIEF |
| `{REVIEWER_FEEDBACK}` | findings from STATE.md (or `"N/A — first iteration"`) |
| `{BASE_SHA}` | current HEAD SHA before this iteration |

Dispatch via `Agent` tool with `subagent_type: "atomic-implementer"` and include `mode: feature` or `mode: surgical` in the prompt.

### Step B — Dispatch reviewer (fresh context)

Use `subagent_type: "atomic-reviewer"`.

Build the reviewer prompt from `commands/_templates/reviewer-prompt.md`, substituting:

| Placeholder | Value |
|-------------|-------|
| `{SCRATCH_PATH}` | absolute path to `$SCRATCH` |
| `{SPEC_PATH}` | absolute path to `docs/spec/<topic>.md` |
| `{BASE_SHA}` | HEAD before this iteration |
| `{HEAD_SHA}` | current HEAD after implementer's work |

### Step C — Orchestrator triages

- Parse reviewer's verdict line: `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED`.
- Update `STATE.md` with iteration number, implementer summary, reviewer findings, next-iteration focus.
- **Harvest non-blocking findings** (🟡 / 🔵 / ❓ that the reviewer let through to PASS, or anything in CHANGES_REQUESTED's set that the next iteration is NOT going to address) into `FOLLOWUPS.md` as new `F-N` entries. Cite `path:line`, severity emoji, problem, suggested fix, origin iteration. Don't drop them; they exist for a reason and the user reviews the ledger before ship.
- If implementer reported `BLOCKED` or `NEEDS_CONTEXT` → stop loop and surface to user.
- If `PASS` → continue to Step D.
- If `CHANGES_REQUESTED` → run the stuck-fix check below before looping.

**Stuck-fix escalation (loop default — fires automatically when the condition is met).**

After each `CHANGES_REQUESTED`, compare the current iteration's blocking signal (the primary 🔴 finding or the dominant failing criterion) against the prior iteration's blocking signal recorded in `STATE.md`. A signal is "unchanged" when the same criterion, test, or finding category appears in both `STATE.md` entries — it does not need to be a verbatim string match. What matters is the underlying root cause, not the surface wording: if the same root failure persists across rounds even when the reviewer leads with a different 🔴 or phrases it differently, treat the signal as unchanged. If two consecutive `CHANGES_REQUESTED` rounds carry the same unchanged blocking signal on the same checkpoint:

1. **Surface the escalation block** to the user before looping again. Print exactly this block (substituting the topic slug):

    ```
    STUCK: 2 rounds on the same failing signal without progress.

    Before another wrap-and-retry iteration, consider escalating to root-cause analysis:

    Option A — pressure-test the spec:
      /pressure-test @docs/spec/<topic>.md

    Option B — dispatch atomic-strategist (opus, read-only) for cross-cutting RCA:
      "Dispatch atomic-strategist: review STATE.md and the last two reviewer verdicts.
       Identify why the same signal keeps failing and whether the spec or approach needs revision."

    Option C — continue the loop anyway:
      Type "continue" to run another iteration without escalating.

    These are offers, not gates.
    ```

2. **Wait for user input** via `AskUserQuestion` with three choices: `continue loop`, `run /pressure-test`, `dispatch atomic-strategist`.
3. **Never auto-dispatch** `atomic-strategist` or auto-invoke `/pressure-test` — both are user-driven (axiom 3: expensive/opus; the user opts in). The orchestrator surfaces the block and waits.
4. After user chooses, record the choice in `STATE.md` under the current iteration's `Decisions:` line.
5. If the user chooses `continue loop` → loop back to Step A as normal.
6. If the user chooses `dispatch atomic-strategist` → dispatch `atomic-strategist` (read-only) with a prompt summarizing the task context, the repeated signal, and the last two iteration findings from `STATE.md`. Incorporate any strategic recommendation into the next `BRIEF.md` before looping. The strategist dispatch does NOT consume a loop iteration — it is a diagnosis step.

This check is **reset** when the blocking signal changes (a different finding category blocks, or the checkpoint advances). It fires again only if the new signal stalls for two rounds.

**6-iteration soft-stop.** When the iteration count reaches 6 (regardless of stuck status), pause and ask the user before continuing — use the same `AskUserQuestion` mechanic. The stuck escalation and the 6-iteration soft-stop are complementary: stuck fires early on repeated signals; the soft-stop is the outer bound. If the stuck escalation has already fired and the user chose to continue, that counts toward the 6-iteration total.

After the stuck check (or if the signal changed and no escalation fires), loop back to Step A with the blocking findings (🔴, plus any 🟡 the orchestrator chooses to address now) as the implementer's focus. Anything not addressed next iteration stays in `FOLLOWUPS.md`.

### Step D — Commit the green iteration

After each PASS, commit before the next iteration:

1. Invoke `atomic-commit` skill for message format.
2. Stage only the files the implementer touched (explicit paths from the implementer's `## Did` section). No `-A`.
3. Commit via HEREDOC. Conventional Commits format. No AI bylines.
4. Record the commit SHA in STATE.md under the iteration's `Commit:` line.
5. If the code-intel index exists (`.claude/.atomic-index/atomic.db`), run `atomic code sync` so the next iteration's reviewer queries the just-committed working-tree state. Skip silently if absent or errors — a failed sync never blocks the next iteration.

Skip Step D only if the iteration produced zero behavior change (pure investigation, no diff). State that explicitly in STATE.md.

This makes each iteration bisectable. The next iteration's reviewer diffs against the prior commit, not the merge base — cleaner reviews, easier rollback if something goes wrong later.

## Phase 3 — Finalize

Once reviewer says `PASS` and there are no more checkpoints in the spec to ship:

1. Run the full test/typecheck/lint/build suite yourself (orchestrator) to confirm green. Do NOT trust subagent claims at the finish line — invoke the `atomic-verify` skill here, which is exactly this gate. When the change touched `docs/spec/**`, `docs/design/**`, or bundled artifacts, also run `atomic validate spec` and `atomic validate config` as part of this verification (skip silently if `atomic` is not on PATH).
2. **Surface `FOLLOWUPS.md` to the user.** Read it, list every open `F-N` entry, and ask the user what to do with each. Four dispositions:

    - **`fix-now`** — run another iteration to address it.
    - **`defer`** — promote to a project-level entry under `.claude/project/followups/<id>.md` (committed, durable, auto-loaded into future sessions via the regenerated `INDEX.md`). The entry survives scratchpad deletion. Optionally chain to `/remind-me <duration> <text>` so the user gets surfaced via `/follow-up` later.
    - **`issue`** — file as a tracked GitHub issue via `/report-issue`.
    - **`drop`** — discard. State the reason in the implementation log so the audit trail explains why it wasn't worth keeping.

    Don't auto-decide; this is the deliberate-decision gate the ledger exists for.

    **`defer` mechanics.** When promoting an F-N entry to project-level via `atomic followups add`:

    1. Compute args from the FOLLOWUPS.md entry:
       - `--id` = `<topic-slug>-F-<N>` (topic-slug from the spec/design file path; N from existing count in `.claude/project/followups/`)
       - `--title` = short one-line description from the F-N header
       - `--severity` = `risk` | `nit` | `question` (map from 🟡/🔵/❓)
       - `--origin` = `"docs/spec/<topic>.md, iter <N> reviewer (CP-<X>)"`
       - `--file` = `<path:line>` from the entry (optional)
    2. Pipe the entry body to `atomic followups add` via stdin:

        ```bash
        printf '%s' "<entry body text>" | atomic followups add \
            --id "<topic-slug>-F-<N>" \
            --title "<short title>" \
            --severity <risk|nit|question> \
            --origin "<origin>" \
            --file "<path:line>" \
            --body -
        ```

       `atomic followups add` validates the id, writes `.claude/project/followups/<id>.md` with correct frontmatter, and regenerates `INDEX.md`. No LLM-authored frontmatter — the command owns that surface.
    3. On exit 1 from `atomic followups add` (e.g. duplicate id): surface the error to the user and prompt for a different id suffix. Retry once. If still failing, fall back to asking the user to run `atomic followups add` manually with a chosen id.
    4. Stage and commit:

        ```bash
        git add .claude/project/followups/<id>.md .claude/project/followups/INDEX.md
        git commit -m "docs(followups): defer <id>"
        ```
3. **Write an implementation log to the spec.** Append (or create) an `## Implementation log` section at the END of `docs/spec/<topic>.md`. This is the durable record someone reads in 6 months when they ask "what did we ship?", "where did this come from?", or "what's still open?". Format:

    ```markdown
    ## Implementation log

    ### <version-or-status> — <date>

    Built across N iterations of /subagent-implementation. Commits (chronological):

    - `<sha>` — CP-1 <one-line>
    - `<sha>` — CP-2 <one-line>
    - ...

    **Out-of-scope work performed during this build:**
    - <what + why it ended up in scope> (or "none")

    **Unforeseens — surprises that emerged during implementation:**
    - <surprise + how it was handled> (or "none")

    **Deferred items still open:**
    - <link to FOLLOWUPS triage decisions, tracked issues, or "none"> 
    ```

    Pull commit SHAs from `STATE.md`. Pull out-of-scope and unforeseens from `STATE.md` decision lines and from any iteration where the implementer's report flagged scope drift or surprise. Pull deferred items from `FOLLOWUPS.md`'s Queued section and the user's disposition answers from step 2. Keep entries tight — one line each. The log is a navigation aid, not a narrative.

    If the spec is dead (e.g. user decided not to ship the feature), still write the log with the status as `abandoned — <date>` and one line on why.

4. Update repo documentation by invoking `/documentation` — it handles `README.md`, `CLAUDE.md`, `docs/spec/`, `docs/design/`.
5. Delete `$SCRATCH` (the task's dated dir) — only after the user has signed off on the FOLLOWUPS triage AND the implementation log is written. Other dated dirs from prior runs are not your concern.
6. Report to the user: what shipped, which iterations + commit SHAs, what was verified, what FOLLOWUPS were dispositioned, what's left (if anything). Mirror what you just wrote to the spec — they should match.

    **Documentation advisory.** If `## Documentation surfaces` exists in CLAUDE instructions and the implemented changes touch files matching any surface's "Covers" column, append to the next-steps suggestions:

    ```
    /documentation — N doc surfaces may be stale
    ```

    One line, advisory only. Not a gate — the user decides whether to address docs now or later.

Do NOT push, merge, or open a PR. The user picks how to ship (`/commit pr`, `/commit merge`, `/commit squash merge`, etc.) when ready.

</workflow>

<constraints>

## Rules

- Parent orchestrator does NOT write implementation code. Only goal docs, state updates, commits per PASS, final docs, final verification.
- Every subagent invocation is fresh context. The scratchpad brief is the only handoff. If the brief is bad, the loop is bad — invest in it.
- Reviewer and implementer are separate agents. Never the same one. Never combine roles.
- If the same blocking signal repeats across two consecutive `CHANGES_REQUESTED` rounds, the stuck-fix escalation in Step C fires automatically — surface `/pressure-test` and `atomic-strategist` RCA options to the user. Do not silently loop again without surfacing this.
- Subagent output is the tool result. Summarize it to the user in 1-3 lines per iteration; don't dump full transcripts.
- Templates live in `commands/_templates/`. If they're missing, the loop can't start — surface that error rather than inlining prompts.

</constraints>

---
description: Multi-agent failure-investigation orchestrator. Parallel to /subagent-implementation — same scratchpad + investigator + builder/surgeon + reviewer + FOLLOWUPS pattern — but starts from a failure, not a spec. Two modes share one loop: ci (failed CI run is the brief seed) and bug (freeform symptom paragraph is the brief seed).
---

You are the **orchestrator**. The user has invoked `/subagent-diagnose`. You will NOT implement the fix yourself. You drive a loop of fresh-context subagents until the failure is resolved, then archive the scratchpad and triage follow-ups.

Spec: `docs/spec/subagent-diagnose.md` — canonical contract. Read it if anything below is ambiguous.

<workflow>

## Phase 0 — Context capture (mode-specific)

### Parse mode

First positional arg (`$ARGUMENTS`) must be `ci` or `bug`. Anything else → refuse:

<example>
```
usage: /subagent-diagnose <mode> [args]
  ci  [<run-id>|<branch>|<pr#>|<workflow.yml>]  — defaults to latest failed run on current branch
  bug "<freeform symptom>"                       — symptom is required; quotes recommended
```

Stop. Do not proceed until mode is valid.
</example>

---

### `ci` mode — Phase 0 steps

| Step | Action |
|------|--------|
| 0.1 | Resolve argument to a run ID. If no arg given: `gh run list --status failure --branch <current-branch> --limit 1 --json databaseId,name,headSha,createdAt`. Refuse if no failed run found: `no failed run found on branch <branch>. provide a run-id, pr#, or workflow.yml as argument.` |
| 0.2 | Capture into `BRIEF.md` source-pointer section: branch, head SHA, base SHA (`git merge-base HEAD origin/main`), workflow name, failed step name (from `gh run view <id> --json jobs`), failure timestamp, provider URL. |
| 0.3 | Topic: `<YYYY-MM-DD>-diagnose-ci-<run-id>`. Set `SCRATCH=".claude/.scratchpad/<topic>"`. **Concurrent-run guard:** if `$SCRATCH` already exists, refuse: `scratchpad <path> already exists; rm -rf it or pick a different topic suffix.` Stop. Per axiom 3, no silent overwrite. Otherwise `mkdir -p "$SCRATCH"`. Verify `.claude/.scratchpad/` is gitignored (add `**/.scratchpad/` to `.gitignore` if missing). |
| 0.4 | Dispatch a `general-purpose` subagent (`model: haiku`, foreground, read-only) with this brief: `Fetch full logs for CI run <id>, failed step "<step-name>". Write to <SCRATCH>/CONTEXT.md. If logs exceed 64KB, truncate with footer: "[truncated, full log at <provider-url>]". Extract the primary failing assertion / panic / error line and append as a trailing YAML key: top_level_error: "<exact error string>"`. |
| 0.5 | Read `CONTEXT.md`. Copy `top_level_error` value into `STATE.md` as `## Iteration 0 — baseline` entry: `top_level_error: <value>` + `normalized_hash: <sha256 of normalized string, first 12 chars>`. |

---

### `bug` mode — Phase 0 steps

| Step | Action |
|------|--------|
| 0.1 | Slug: kebab-case from first ~6 words of the symptom arg (e.g. `"user login fails with 500"` → `user-login-fails-with-500`). Topic: `<YYYY-MM-DD>-diagnose-bug-<slug>`. Set `SCRATCH=".claude/.scratchpad/<topic>"`. **Concurrent-run guard:** if `$SCRATCH` already exists, refuse: `scratchpad <path> already exists; rm -rf it or pick a different topic suffix.` Stop. Per axiom 3, no silent overwrite. Otherwise `mkdir -p "$SCRATCH"`. Verify `.claude/.scratchpad/` is gitignored. |
| 0.2 | Single `AskUserQuestion` block. For each of the four context fields not already answered by the symptom: **repro steps**, **expected vs actual behavior**, **environment fingerprint** (OS, runtime versions, branch, dirty/clean working tree), **what's been tried**. Skip fields the symptom paragraph already answers. |
| 0.3 | Write `CONTEXT.md` with four stable headings (`## Repro`, `## Expected vs actual`, `## Environment`, `## Already tried`). Append trailing YAML key `top_level_error:` — use a paste-able error string if the brief or answers contain one, else `<none — behavioral bug>`. |
| 0.4 | Auto-capture: if suspected paths are inferable from the brief, run `git log --oneline -20 -- <paths>` and append output as `## Recent commits` to `CONTEXT.md`. Skip silently if no paths inferable. |
| 0.5 | Write `BRIEF.md` source-pointer section pointing at `CONTEXT.md`. The brief is canonical — no external spec exists. |

---

## Scratchpad layout

| Path | Contents |
|------|----------|
| `$SCRATCH/BRIEF.md` | Pointer to source, current iteration scope, reviewer feedback rollup |
| `$SCRATCH/STATE.md` | Append-only iteration log; one entry per Phase 2→3 cycle + iteration-0 baseline |
| `$SCRATCH/FOLLOWUPS.md` | Non-blocking findings carried across iterations; dispositioned at Phase 4 |
| `$SCRATCH/CONTEXT.md` | Phase 0 capture (logs for `ci`, repro + symptom map for `bug`) |

`$SCRATCH` = `.claude/.scratchpad/<YYYY-MM-DD>-<mode-suffix>`.

## Phase 1 — Investigator pass

Write the full `BRIEF.md` before dispatching (refreshed each iteration — orchestrator overwrites, not appends; Phase 0 source-pointer section is restored each time). Include: source pointer, failure summary from `CONTEXT.md`, head SHA, base SHA, suspected surface area (if known from Phase 0), iteration scope, reviewer feedback (`"N/A — first iteration"` on first pass).

**Brief verbosity discipline:** every fact the next agent needs lives in the brief — log excerpts, file:line refs, base SHA, what's been tried, hypotheses, prior reviewer feedback. A short brief that forces re-discovery is a false economy. Tokens spent on a verbose brief are tokens saved on subagent re-search.

Dispatch:

- `subagent_type: "atomic-investigator"`
- Prompt: `Read $SCRATCH/BRIEF.md and $SCRATCH/CONTEXT.md. Map the suspect surface — files, call sites, functions, test coverage. Return a file:line — what table as your output.` When a code-intel index is warm, seed the brief with the failing symbol or top-level error from `CONTEXT.md` and tell the investigator to lead with `atomic code explore "<that failure context>"` to scope the failure neighborhood in one shot, plus `atomic code callers <failing-fn>` for blast radius, before grepping cold.

Orchestrator appends the investigator's output to `BRIEF.md` as `## Phase 1 — surface map`.

**Cohesion classification (orchestrator, not agent).** After reading the investigator's surface map, classify the work:

| Classification | Signal | → Agent |
|---------------|--------|---------|
| `tight` | Single logical change, ≤2 files would suffice | `atomic-implementer (mode: surgical)` |
| `loose` | Multi-file, multi-concern | `atomic-implementer (mode: feature)` |

Record classification in `STATE.md` under `## Iteration 1`. If the surgical dispatch returns `OUT OF SCOPE: needs N files. Split: ...`, re-dispatch the same scope to `atomic-implementer (mode: feature)` immediately — do not loop on refusal.

## Phase 2 — Implementation

Build the implementer prompt from `commands/_templates/implementer-prompt.md`, substituting:

| Placeholder | Value |
|-------------|-------|
| `{SCRATCH_PATH}` | Absolute path to `$SCRATCH` |
| `{SPEC_PATH}` | `"no spec — brief is BRIEF.md + CONTEXT.md"` |
| `{ITERATION_SCOPE}` | This iteration's scope (fix the failure described in CONTEXT.md; failing test first) |
| `{REVIEWER_FEEDBACK}` | Findings from STATE.md (or `"N/A — first iteration"`) |
| `{BASE_SHA}` | `git rev-parse HEAD` before this iteration |

Dispatch via `Agent` tool with `subagent_type: "atomic-implementer"` and include `mode: surgical` or `mode: feature` in the prompt per the cohesion classification.

TDD discipline applies: failing test that reproduces the bug must be written first, then the fix. The agent's signal block is the evidence.

**Commit ownership: orchestrator commits, not the agent.** After PASS:

1. Invoke `atomic-commit` skill for message format.
2. Stage only files from the implementer's `## Did` section — explicit paths, no `-A`.
3. Commit via HEREDOC. Conventional Commits format. No AI bylines.
4. Record commit SHA in `STATE.md` under the iteration's `Commit:` line.

## Phase 3 — Reviewer pass

Build the reviewer prompt from `commands/_templates/reviewer-prompt.md`, substituting:

| Placeholder | Value |
|-------------|-------|
| `{SCRATCH_PATH}` | Absolute path to `$SCRATCH` |
| `{SPEC_PATH}` | `"no spec — brief is BRIEF.md + CONTEXT.md"` |
| `{BASE_SHA}` | HEAD before this iteration's implementer ran |
| `{HEAD_SHA}` | `git rev-parse HEAD` after implementer's work |

Dispatch `subagent_type: "atomic-reviewer"`.

Reviewer emits `## Spec compliance` + `## Code quality` + signals block + exactly one of `VERDICT: PASS` / `VERDICT: CHANGES_REQUESTED`.

**Orchestrator triage after each verdict:**

- Parse `VERDICT:` line.
- Update `STATE.md`: iteration number, implementer summary, reviewer findings, next-iteration focus.
- **Harvest non-blocking findings** (🟡 / 🔵 / ❓ that didn't block PASS, or CHANGES_REQUESTED items not addressed next iteration) into `FOLLOWUPS.md` as `F-N` entries. Cite `path:line`, severity emoji, problem, suggested fix, origin iteration. Don't self-censor non-blockers.
- **Same-failure check:** after each CHANGES_REQUESTED, extract the normalized top-level error from `STATE.md`'s `top_level_error:` entries across iterations (originally written at Phase 0 into `CONTEXT.md`) and compare to prior iterations (see § Iteration cap + bail-out). If three consecutive normalized matches → bail.
- `CHANGES_REQUESTED` → overwrite `BRIEF.md` reviewer-feedback section with blocking findings, increment iteration counter in `STATE.md`, loop back to Phase 2.
- `PASS` → proceed to Phase 4.
- Implementer `BLOCKED` or `NEEDS_CONTEXT` → stop loop, surface to user.

## Iteration cap + bail-out

- **Default hard stop:** 5 iterations of Phase 2→3.
- **User override (axiom 2 — memory-first):** read user memory key `diagnose iteration cap` at Phase 1. Falls back to 5 if absent. Cap is `min(memory-override, 5)`.
- **Same-failure early bail:** if three consecutive iterations produce the same normalized top-level error → bail before the hard stop.
- **Bail behavior:** retain `$SCRATCH` in place (do not archive). Print a summary of iterations tried + final reviewer verdict. Then surface the Stuck-fix escalation block defined in this section and wait for user input — do NOT auto-open a PR comment or post anywhere.

**Stuck-fix escalation block (surfaced on bail, never auto-invoked).**

When the same-failure bail fires, print this block (substituting the actual error slug and topic):

```
BAIL: 3 consecutive iterations hit the same failure ("<normalized-error-slug>"). The loop cannot make forward progress.

Before abandoning or retrying from scratch, consider:

Option A — pressure-test the approach:
  /pressure-test @$SCRATCH/CONTEXT.md   (primary — the captured failure context is the input)
  or, if a spec exists for the affected area: /pressure-test @docs/spec/<topic>.md

Option B — dispatch atomic-strategist (opus, read-only) for cross-cutting RCA:
  "Dispatch atomic-strategist: review $SCRATCH/CONTEXT.md, $SCRATCH/STATE.md, and
   the last three reviewer verdicts. Identify whether the failure has a root cause
   the current approach cannot reach, and recommend a revised approach."

Option C — abort and retain scratchpad for manual inspection.
```

Then `AskUserQuestion` with three choices: `dispatch atomic-strategist`, `run /pressure-test`, `abort`. Never auto-dispatch either option — the user opts in (axiom 3: opus is expensive; `/pressure-test` may mutate the spec).

### Same-failure normalization

Before comparing top-level error strings across iterations, apply in order:

1. Strip `:\d+(:\d+)?` line/column suffixes.
2. Replace absolute paths with basename (`/a/b/foo.go` → `foo.go`).
3. Strip ISO timestamps and `\d{2}:\d{2}:\d{2}` clock times.
4. Strip hex addresses (`0x[0-9a-fA-F]+`).
5. Strip test-runner durations (`\d+(\.\d+)?(ms|s|µs|ns)\b`).
6. Collapse runs of whitespace to single space; trim.

Two normalized strings equal → "same failure". Store hash (first 12 chars of sha256) + first 200 chars of raw error in `STATE.md` per iteration.

## FOLLOWUPS handling

- Reviewer findings tagged non-blocking (🟡 / 🔵 / ❓ that don't block PASS, or any finding the next iteration won't address) are appended to `FOLLOWUPS.md` by the orchestrator after each reviewer pass — even on PASS verdicts.
- Carried across iterations. Reviewer may re-affirm or signal prior findings closed; **orchestrator** marks resolved entries `*(closed iter N — <sha>)*` in `FOLLOWUPS.md` — do not delete the entry.
- At Phase 4, present per-item to user. Dispositions per item:
    - **`close`** — discard; state reason in implementation log.
    - **`defer`** — shell out to `atomic followups add` to promote to `.claude/project/followups/<id>.md` with `--origin` pointing at this run. Optionally chain to `/remind-me`.
    - **`convert-to-spec`** — invoke `/atomic-plan` with the entry as the brief.

## Concurrent runs

Topic dir includes mode + per-mode unique suffix (run-id for `ci`, slug for `bug`). If the topic dir already exists when creating it → refuse (per concurrent-run guard above). No silent overwrite. `--resume` is YAGNI.

## Phase 4 — Verification + teardown (mode-specific body, shared teardown)

### `ci` mode

| Step | Action |
|------|--------|
| 4.1 | Push the fix commit if not yet pushed. Confirm with user first (per axiom 3 — push is visible to others). |
| 4.2 | Dispatch a `general-purpose` subagent (`model: haiku`, `run_in_background: true`) with brief: `Watch CI for branch <branch> (commit <sha>) until terminal state. Report: run ID, conclusion (success/failure/cancelled/timed-out), failing step + 1-3 line error excerpt on failure. Cap at 10 minutes. Read-only — do not rerun or cancel.` |
| 4.3 | Return control to user immediately. Print: archive path, fix commit SHA, background watcher launched. |
| 4.4 | When watcher completes — do **not** auto-relaunch on failure. Surface the new failure ID and instruct user to re-invoke `/subagent-diagnose ci <new-run-id>`. Prevents infinite loops on flaky infrastructure. |

### `bug` mode

| Step | Action |
|------|--------|
| 4.1 | Foreground orchestrator runs the repro from `CONTEXT.md ## Repro` against the committed fix. Shell-executable repros: run via Bash. Manual repros (UI, third-party service): prompt user to run and report result via `AskUserQuestion`. |
| 4.2 | If repro passes (bug no longer reproduces): proceed to teardown (archive). |
| 4.3 | If repro still fails: do **not** archive. Print: `fix landed in commit <sha> but repro still fails. scratchpad retained at <path>. reviewer signed PASS based on the regression test; the test may not match the real repro.` Then `AskUserQuestion`: continue iterating (Phase 2) / accept and archive / abort. |
| 4.4 | No background dispatch. Bug verification is synchronous. |

### Shared teardown (both modes)

After verification:

1. **FOLLOWUPS disposition.** Present `FOLLOWUPS.md` ledger to user per-item (see § FOLLOWUPS handling).
2. **Archive on success.** `mkdir -p .claude/.scratchpad/.archive/` then `mv "$SCRATCH" ".claude/.scratchpad/.archive/<topic>/"`. Verify: archive dir exists, original dir does not.
3. **Retain on bail.** Do not archive. Leave `$SCRATCH` in place for resume inspection.
4. **Report to user:** what was fixed, commit SHA(s), iterations run, signals verified, FOLLOWUPS dispositioned, what's left (if any).

Do NOT push, merge, or open a PR. User picks the ship verb when ready.

</workflow>

<constraints>

## Rules

- Orchestrator does NOT write implementation code. Only goal docs, state updates, commits per PASS, archive, and triage.
- Every subagent dispatch is fresh context. The scratchpad brief is the only handoff. Invest in it — a verbose brief is cheaper than a misaimed iteration.
- Reviewer and implementer are separate agents. Never the same. Never combined.
- Mode subcommand is required. Never proceed without a valid `ci` or `bug` mode.
- Never auto-relaunch on CI re-watch failure (ci-mode step 4.4). Hard rule — prevents infinite loops on flaky infra.
- If the same normalized top-level error repeats across three consecutive iterations, the same-failure bail fires and surfaces the stuck-fix escalation block (see § Iteration cap + bail-out) — do not silently loop past the bail.
- Subagent output is the tool result. Summarize to the user in 1-3 lines per iteration; don't dump full transcripts.
- Templates live in `commands/_templates/`. If missing, stop: `implementer/reviewer prompt template not found at commands/_templates/<file>. cannot proceed.`

</constraints>

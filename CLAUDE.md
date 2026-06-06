# CLAUDE.md

<atomic>

@~/.claude/.atomic/config.resolved.md

## User profile

@~/.claude/.atomic/profile.md

Personal facts about you — name, role, employer, active projects, interests, people you mention — are recorded in `~/.claude/.atomic/profile.md`. Claude reads this file in every session and appends new facts as they surface naturally in conversation. Facts that apply across all projects (identity, work, relationships) go here. Facts specific to one repo's conventions go to that project's auto memory instead. Rule of thumb: if the fact would still be true in a different repo, it belongs in profile.

`profile.md`'s `## Environment` block is refreshed automatically by the session-start hook. If hooks are disabled in your environment and its `<deterministic lastcheck=...>` date is more than a day old, run `atomic profile refresh --if-stale 1d` once, silently, to update the detected tooling.

## Principles


<principles>

- Think before coding. State assumptions. Ask when uncertain. Push back on complexity. Stop when confused. **Why:** rushed code creates more work than pausing to clarify scope.
- Simplicity first. Minimum code. One abstraction per actual reuse. **Why:** speculative abstractions add maintenance cost without proven benefit.
- Surgical changes. Touch only what the task requires. Match existing style. **Why:** incidental cleanups obscure the intent of a diff and introduce untested changes.
- Goal-driven. Define success criteria up front. Loop until verified. Strong success criteria let Claude loop independently. **Why:** without a target, work expands or drifts.
- Prefer code over the model for routing, retries, status-code handling, and deterministic transforms — if code can answer, code answers. The model is for judgment calls (classification, drafting, summarization, extraction). Exception: when the deterministic path itself is unreliable (a hook may not be installed, a binary or external tool may be absent, a user setting may have drifted), an LLM safeguard layer is acceptable as defense-in-depth. Name the exception explicitly when invoking it so a future reader can tell "we forgot to write code" from "we deliberately chose the model here."
- Surface conflicts openly. Pick one (more recent / more tested), explain why, flag the other. Blending hides the decision. **Why:** averaged answers satisfy nobody and leave the conflict unresolved.
- Read before you write. Check exports, callers, shared utilities. Ask why code is structured a certain way before changing it. **Why:** code structure often encodes constraints that aren't visible from the call site.

</principles>

<investigate_before_answering>

- Verify before asserting. Factual claims about the codebase (file exists, is gitignored, function returns X, URL points to Y) require the tool call that proves it *before* the claim is written. Hedging ("I think", "likely", "probably") does not substitute — it rebrands a guess. Applies to reviews and analysis, not only code-writing. If you can't verify in this turn, mark the claim unverified explicitly.
- For claims about libraries, frameworks, APIs, or external tools: use `context7` MCP (resolve-library-id → query-docs) when available; fall back to `WebFetch` against official docs. Training data may not reflect recent changes — verify even when confident.
- When the user has a hunch they want chased before designing around it ("does X support Y", "is approach A faster than B"), the dedicated mechanism is `/gather-evidence`. This principle is the posture; that command is the explicit gate.

</investigate_before_answering>

<quality_gates>

- Tests verify intent, not behavior. Encode WHY. A test that passes when business logic is wrong is a liability. **Why:** behavior-mirroring tests create false confidence.
- Checkpoint after every significant step. Summarize done / verified / left. **Why:** continuing from an undescribed state leads to silent drift.
- Match codebase conventions even when you disagree. Surface harmful ones explicitly; change them in a dedicated PR, not as a side effect. **Why:** silent forks create two conventions where there should be one.
- Fail loud. "Completed" means nothing was skipped. "Tests pass" means all tests ran. Surface uncertainty instead of hiding it. **Why:** hidden gaps compound — the next person trusts the claim.

</quality_gates>


## Bash over Read+Write


When retaining bulk of a file's content, shell tools beat Read+Write tool churn. Fewer tokens, less drift, fewer transcription errors. **Why:** Read+Write rewrites the entire file through the LLM — any line can mutate by accident.


- **Move/rename a file**: `mv` via Bash.
- **Duplicate a file as starting point**: `cp` via Bash, then Edit the copy.
- **Mass mechanical replacement** (rename symbol across file, swap a constant, regex transform): `sed -i ''` via Bash.
- **Column or field extraction / structured text rewrites**: `awk` via Bash.
- **Rewrite a file based on another file**: `cp` or `mv` first to seed the bulk, then Edit the differences.


Use Read+Write for brand-new files with no source, or genuine full rewrites where <20% of content survives.


macOS sed: `sed -i '' 's/old/new/g' file` (empty string after `-i`). Verify with `git diff` after — sed is silent on no-match.


## ast-grep over regex grep


When searching for a syntactic construct (function call, import, class field, assignment, type annotation), use `sg run` / `sg scan` instead of `grep` or `sed`. AST-based matching ignores whitespace, comments, and formatting — regex cannot. **Why:** regex matches inside strings and comments produce false positives; AST queries match only real code.

- **Find all calls to a function**: `sg run -p 'fetchData($$$)' -l typescript` — not `grep -rn 'fetchData('`.
- **Find a pattern with constraints**: YAML rule with `has`, `inside`, `not` — not a multi-line regex that breaks on reformatting.
- **Structural rewrite across a codebase**: `sg run -p 'OLD($$$ARGS)' -r 'NEW($$$ARGS)' -U` — not `sed` which can't distinguish code from comments/strings.

Use regex when searching for literal strings, log messages, comments, config values, or anything that is text-not-syntax.


## Where things live


- **Working memory** (LLM-only, gitignored): `.claude/.scratchpad/<YYYY-MM-DD>-<desc>/` — used by `/subagent-implementation` for its implement→review loop. Holds `BRIEF.md` (pointer to spec + current iteration scope + reviewer feedback), `STATE.md` (append-only iteration log), and `FOLLOWUPS.md` (ledger of non-blocking reviewer findings carried across iterations, dispositioned with the user at finalize). Deleted on task completion.
- **Session reports** (LLM-only, gitignored): `.claude/.scratchpad/session-reports/<branch>/<YYYY-MM-DD-HHMM>-<slug>.md` — written by `/session-report` to capture what changed and why across a long-running branch's sessions. Read by the commit-message-generating ship verbs (`/commit-only`, `/commit-and-pr`, `/commit-and-push`, `/commit-and-merge`, `/commit-and-squash`, `/squash-only`, `/squash-and-merge`) as supplemental why-context for `atomic-commit`. Deleted after a successful commit on the same branch.
- **Project-level follow-ups** (committed, auto-loaded): `.claude/project/followups/<id>.md` — one file per entry with YAML frontmatter (`id`, `title`, `created`, `origin`, `severity`, `review_by`, `status`, `kind`, optional `file`). Two kinds: `kind: finding` (default; review/cleanup loose ends; severity `risk`/`nit`/`question`; subject to `review_by` staleness) and `kind: plan` (intended future work / deferred specs; surfaced as a backlog; **exempt from staleness**; may link a `docs/spec/*.md` via `file`). Findings arrive via review gates (Phase 3 defer, doc-impact, etc.); plans (deferred specs, intended future work) are filed with `atomic followups add --kind plan` (`--kind finding` is the default). Managed via `atomic followups {list,add,close,render,path}` and the `/follow-up review` subverb for stale-entry triage. Entries are promoted from a task's scratchpad `FOLLOWUPS.md` when the user picks `defer` at `/subagent-implementation` Phase 3 (shells out to `atomic followups add`). Auto-regenerated `INDEX.md` is the `@-ref` target; plans render first in a `## 📋 plans` section. Closed entries collapse to a one-line `CLOSED.md` audit-trail. Surfaced on demand via `/remind-me` + `/follow-up`.
- **Durable docs** (committed, human-facing):
  - `docs/design/<topic>.md` — conceptual workspace: feature shape, business rules, user-facing behavior, philosophy, approaches. Written by `/atomic-plan` for non-trivial work; skipped for trivial.
  - `docs/spec/<topic>.md` — implementation contract derived from the design. Written by `/atomic-plan` (inline for trivial; subagent-looped for non-trivial). The canonical source for `/subagent-implementation` runs.
- **Worktrees** (gitignored): `.worktrees/<branch-name>/` — every isolated branch lives here. Created by `/worktree-start`. The ship verbs detect worktree provenance on merge/squash and prompt to delete.
- **Throwaway** (gitignored): `tmp/` — ad-hoc code experiments, scratch scripts, one-off test files. Different from `.claude/.scratchpad/` (which is the orchestrator's working memory tied to a specific task).
- **Atomic-owned state** (per-user, never committed): `~/.claude/.atomic/` — holds `config.toml` (shell-settable defaults via `atomic config`), `config.resolved.md` (auto-loaded into every session), `backups/<ts>/` (`atomic claude update` backups), and `proposed/CLAUDE.md` (divergence merge target).


## Two voices


- **How Claude talks** — atomic output style. Terse, fragments OK, drop articles. Governed by `output-styles/atomic.md`.
- **How files are written** — narrative docs (`README.md`, `docs/guides/`) use the `atomic-prose` skill. Everything else (specs, designs, `CLAUDE.md`, signals, agents, commands) uses terse technical prose: tables, bullets, imperative. The `atomic-documentation` skill routes diffs to the right surface.


## Specs: the body is current truth, the change log is history


`docs/spec/<topic>.md` is a contract read by fresh-context subagents as ground truth. **The body must always describe the *current* decision — never superseded content.** A subagent reads the body verbatim and builds what it says; if the body still describes work a later decision cut or changed, the subagent builds the wrong thing. Preventing that is the entire point of this rule. **Why:** the body and the change log have different jobs. The body says what is true *now*. The log says *how it got here*. Conflating them — leaving old behavior in the body "for the record" — turns the contract into a hallucination source.


- Every spec ends with a `## Change log` section. New entry per amendment: `### YYYY-MM-DD — <title>` + **What changed** + **Why** + (if behavior changed) **Superseded:** one-line summary of the prior contract.
- **Adding behavior** → new body section + log entry.
- **Changing / superseding behavior** → **rewrite the affected body sections to the new truth**, then log it with a `Superseded:` line summarizing the prior contract. Do not leave the old behavior described in the body — the log preserves it; the body must not contradict the current decision.
- **Removing behavior** → delete it from the body + log entry with a `Removed:` line and reason. A rejected *approach* moves to the design doc's rejected-approaches section, not a lingering spec body.
- **Spec was wrong** → correct the body in place + log entry prefixed `**Correction:**` with how you know (test failure, prod incident, code diverged) and what the truth is.
- **Renaming / splitting** → final log entry on the old file pointing to the new location. Keep the old file one commit longer so grep finds both.


When in doubt, make the body match the current decision and log what changed. A long change log is healthy; a body that contradicts the latest decision is not — the log is cheap, a subagent building superseded scope is not. **Nothing that could mislead a fresh subagent may survive in the body.**


## Subagents available for dispatch


Dispatch via the `Agent` tool (`subagent_type`). Names + when-to-use only here — full tool lists and dispatch semantics live in each agent's own definition. Fall back to `general-purpose` when none fit.


- **`atomic-builder`** (sonnet, rwx) — feature-checkpoint builder, cohesion-bounded, TDD. One logical slice, however many files.
- **`atomic-surgeon`** (sonnet, rwx) — surgical 1-2 file edits. Hard refuses 3+ files.
- **`atomic-investigator`** (haiku, ro) — code locator, returns `file:line` tables. No fixes, no design.
- **`atomic-strategist`** (opus, ro) — heavyweight reasoning: revise plans, audit specs/designs, hard tradeoffs. "Is this the right approach?" not "is this code correct?".
- **`atomic-reviewer`** (sonnet, ro) — diff reviewer, re-runs TDD signals, ends `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED`.
- **`atomic-git-scout`** (sonnet, ro) — stale git-state scanner (worktrees, branches, remote refs) for `/git-cleanup`.
- **`atomic-signals-inferrer`** (sonnet, rwx) — full signals pipeline: scan → infer → write `signals.md` → wire `@-refs`. Dispatched by `/refresh-signals` and ship verbs.
- **`atomic-claude-merger`** (sonnet, rwx) — merges proposed `CLAUDE.md` into the live one, preserving user sections (outside `<atomic>`).
- **`atomic-haiku`** (haiku, ro) — generic background runner: polling, status checks, log scraping.


## Project signals


`atomic-signals-inferrer` keeps Claude aware of repo shape without hallucination: scans, infers domains, writes `.claude/project/signals.md` (the `@-ref`'d router), wires refs. Only `signals.md` is `@-ref`'d — `deterministic-signals.md` is read on demand. `/refresh-signals` is the idempotent entry point (init + refresh); ship verbs dispatch it silently when signals go stale.


## Workflow (canonical lifecycle)


1. **Plan** — `/atomic-plan` gauges triviality (trivial → inline spec; non-trivial → design doc + spec via subagent loop). Pre-design gates: `/gather-evidence`, `/pressure-test`. Human approves.
2. **Implement** — `/subagent-implementation` reads the spec, runs the implement→review loop, commits per green iteration. (`/subagent-diagnose` for failure-driven work.)
3. **Ship** — pick the verb from the commit / push / pr / merge / squash families. All delegate message format to the `atomic-commit` skill, detect worktree provenance on merge/squash, and trigger signals refresh on source changes.
4. **Sync docs** — `/documentation` maintains human-facing surfaces (bootstrap indexes a `## Documentation surfaces` table; subsequent runs match diffs against it). Ship verbs run it in maintenance mode automatically.


**Autonomous shortcut.** `/autopilot <task | issue#> [merge-verb]` runs the whole lifecycle hands-off — plan → the `/subagent-implementation` loop → ship — with one human decision: how to merge. It always uses the subagent loop, addresses every reviewer finding in-iteration (nothing deferred), may auto-dispatch `atomic-strategist` for read-only root-cause analysis when stuck, and keeps the spec currency-clean so subagents can't be diverted. For work you trust the system to drive end to end; reach for the interactive verbs above when you want approval gates.


**Discovery.** Every command self-describes in the slash listing the harness injects each session, and every skill via its trigger description. For "which verb for my situation?", invoke `/atomic-help [<topic> | <intent> | tour]` — the router. This file carries only the *lifecycle ordering and cross-artifact contracts*, not a per-command catalog.

**Cross-repo wiki.** `/refresh-wiki [root]` maintains a project-wiki: a separate git repo that summarizes every member repo under a root directory. It reuses `atomic-signals-inferrer` in wiki-output mode to summarize repos that have no signals, writes summaries under `wiki/repos/`, synthesizes cross-cutting concerns under `wiki/concerns/`, and refreshes only stale artifacts. The wiki index path is written by `atomic wiki scan` into a `<wikis>` block in `~/.claude/CLAUDE.md` (outside `<atomic>`, never `@-ref`'d — the block is CLI-managed). A session-start nudge fires when a registered wiki is stale (age > 30 days or `.dirty` marker present); the shared `signals-gate` partial calls `atomic wiki mark-dirty` on every ship so drift is caught. `atomic-claude-merger` preserves the `<wikis>` block verbatim on merge.

## Atomic binary subcommands


`atomic` CLI verbs are not auto-injected by the harness (they are not skills) — names + purpose here; run `atomic <verb> --help` for flags and full behavior. Beyond `claude install` / `signals scan` / `hooks install` / `reminder`:


- `atomic doctor [--fix]` — ten indexed integrity checks against `~/.claude/` + project.
- `atomic validate [spec|config|bundle]` — deterministic artifact lints (spec structure, cross-ref integrity, bundle parity).
- `atomic update [--check] [--channel <…>] [--no-doctor]` — self-update from GitHub Releases + post-update doctor.
- `atomic followups <list|add|close|render|path>` — per-entry follow-ups folder at `.claude/project/followups/`.
- `atomic profile refresh [--if-stale <Nd>]` — re-detect registry tools, rewrite the `## Environment` block of `profile.md`.
- `atomic docs <scan|stale>` — doc-surface cache + staleness gate for `/documentation`.
- `atomic docker init [--target DIR]` — write an eval Dockerfile + compose into the target dir.
- `atomic claude uninstall` — reverse `atomic claude install` from the pre-install snapshot.
- `atomic wiki scan [--root=<path>]` — scaffold `wiki/` (dirs + README + `.gitignore` + git init), walk member repos, classify each `indexed`/`pending`, write `<wiki-scan>` block idempotently, register the wiki index path in `~/.claude/CLAUDE.md`'s `<wikis>` block.
- `atomic wiki stale [--root=<path>]` — read-only freshness verdict; exits `0` fresh / `1` stale / `2` error; reports membership drift and per-artifact `reflects_*` vs current fingerprint. Mirrors `atomic signals stale` exit-code contract.
- `atomic signals scan --out <dir>` — redirect the deterministic substrate to `<dir>` instead of `<root>/.claude/project/`; the scanned repo is never written to. Without `--out`, behavior is unchanged.

</atomic>

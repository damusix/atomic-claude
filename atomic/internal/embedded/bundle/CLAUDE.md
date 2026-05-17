# CLAUDE.md


## Principles


- Think before coding. State assumptions. Ask, don't guess. Push back on complexity. Stop when confused.
- Simplicity first. Minimum code. No speculation. No abstractions for single-use code.
- Surgical changes. Touch only what's needed. Don't improve adjacent code. Match existing style.
- Goal-driven. Define success criteria up front. Loop until verified. Strong success criteria let Claude loop independently.
- Use the model for judgment calls only (classification, drafting, summarization, extraction). Never for routing, retries, status-code handling, or deterministic transforms. If code can answer, code answers.
- Surface conflicts, don't average them. Pick one (more recent / more tested), explain why, flag the other. Never blend.
- Read before you write. Check exports, callers, shared utilities. If unsure why code is structured a certain way, ask.
- Verify before asserting. Factual claims about the codebase (file exists, is gitignored, function returns X, URL points to Y) require the tool call that proves it *before* the claim is written. Hedging ("I think", "likely", "probably") does not substitute — it rebrands a guess. Applies to reviews and analysis, not only code-writing. If you can't verify in this turn, mark the claim unverified explicitly; don't ship it as fact.
- Tests verify intent, not behavior. Encode WHY. A test that can't fail when business logic changes is wrong.
- Checkpoint after every significant step. Summarize done / verified / left. Don't continue from a state you can't describe.
- Match codebase conventions even if you disagree. Surface harmful ones; don't fork silently.
- Fail loud. "Completed" is wrong if anything was skipped. "Tests pass" is wrong if any were skipped. Surface uncertainty, don't hide it.


## Bash over Read+Write


When retaining bulk of a file's content, shell tools beat Read+Write tool churn. Fewer tokens, less drift, fewer transcription errors.


- **Move/rename a file**: `mv` via Bash. Never Read + Write to new path + delete old.
- **Duplicate a file as starting point**: `cp` via Bash, then Edit the copy. Never Read source + Write target.
- **Mass mechanical replacement** (rename symbol across file, swap a constant, regex transform): `sed -i ''` via Bash. Never Read + Write the whole file for a find/replace.
- **Column or field extraction / structured text rewrites**: `awk` via Bash.
- **Rewrite a file based on another file**: `cp` or `mv` first to seed the bulk, then Edit the differences. Never Read source + Write target from scratch.


When Read+Write is correct: brand-new file with no source, or genuine full rewrite where <20% of content survives.


macOS sed: `sed -i '' 's/old/new/g' file` (empty string after `-i`). Verify with `git diff` after — sed is silent on no-match.


## Design axioms


Enduring principles for the atomic-claude system. Apply when adding new commands, skills, or agents.


1. **Cohesion-bounded scope, not file-count-bounded.** Feature-slice agents accept many files when they form one logical unit; surgical agents hard-cap on file count.
2. **Memory over config.** Variable thresholds and user preferences live in auto-memory, not config files.
3. **Destructive ops require explicit per-item confirm.** Default to report-only. Never auto-act on data-losing operations.
4. **Plain-text indexed selection over multi-select UI.** For N-item lists, print a numbered list and accept typed input (`1 3 5`, `all`, `none`).
5. **Skills auto-fire on triggers; commands are explicit-only.** If a description has to forbid auto-firing, convert the skill to a command.


## Where things live


- **Working memory** (LLM-only, gitignored): `.claude/.scratchpad/<YYYY-MM-DD>-<desc>/` — used by `/subagent-implementation` for its implement→review loop. Holds `BRIEF.md` (pointer to spec + current iteration scope + reviewer feedback), `STATE.md` (append-only iteration log), and `FOLLOWUPS.md` (ledger of non-blocking reviewer findings carried across iterations, dispositioned with the user at finalize). Deleted on task completion.
- **Project-level follow-ups** (committed, auto-loaded): `.claude/project/followups.md` — entries promoted from a task's scratchpad `FOLLOWUPS.md` when the user picks the `defer` disposition at Phase 3. Cross-task durability for non-blocking findings the user explicitly chose to revisit later. Auto-loaded via `@-ref` (same search order as signals: `claude.local.md` → `CLAUDE.local.md` → `claude.md` → `CLAUDE.md`). Surfaced on demand via `/remind-me` + `/follow-up` for time-boxed reviews.
- **Durable docs** (committed, human-facing):
  - `docs/design/<topic>.md` — design rationale, alternatives considered, brainstorming. Written collaboratively via `/atomic-plan` when classified as design.
  - `docs/spec/<topic>.md` — implementation contract for an approved feature. Written collaboratively via `/atomic-plan` when classified as spec. The canonical source for `/subagent-implementation` runs.
- **Worktrees** (gitignored): `.worktrees/<branch-name>/` — every isolated branch lives here. Created by `/worktree-start`. The ship verbs detect worktree provenance on merge/squash and prompt to delete.
- **Throwaway** (gitignored): `tmp/` — ad-hoc code experiments, scratch scripts, one-off test files. Different from `.claude/.scratchpad/` (which is the orchestrator's working memory tied to a specific task).


## Three doc voices, three surfaces


Documentation in this system follows three distinct voices. Picking the wrong voice for a surface is an invisible bug — readers either drown in narrative where they wanted a table, or hit a wall of fragments where they wanted explanation.


- **Atomic style** — Claude's *TUI replies to the user*. Terse, fragments OK, drop articles. Governed by `output-styles/atomic.md`.
- **`atomic-prose` skill** — *enduring narrative docs*: `README.md`, `docs/guides/`, CHANGELOG narrative entries. Clear, specific, active-voice technical paragraphs. No em dashes, no marketing jargon, no throat-clearing, no AI-tell phrases. Auto-fires when drafting or editing those surfaces; invoked by `/documentation` when it touches README or a guide.
- **Spec / design voice** — `docs/spec/<topic>.md` and `docs/design/<topic>.md`. **Tables, diagrams, terse bullets first.** Prose only where a contract truly needs sentences (Goal, Problem statement, Rationale). Brevity dominates: these files are re-read often by both humans and agents and live or die by token cost. Enforced by `/atomic-plan` (see `commands/atomic-plan.md`).


Never apply `atomic-prose` to specs or design docs. Never let spec/design terseness creep into the README or guides.


## Spec files are append-mostly


`docs/spec/<topic>.md` is a contract. Editing it in place destroys the original intent and the reason it was written. Treat specs as **append-mostly** so the audit trail survives.


- Every spec ends with a `## Change log` section. New entry per amendment: `### YYYY-MM-DD — <title>` + **What changed** + **Why** + (if behavior changed) **Superseded:** one-line summary of prior contract.
- **Adding behavior** → new body section + log entry.
- **Changing behavior** → edit body + log entry with `Superseded:` line preserving prior contract.
- **Removing behavior** → delete from body + log entry with `Removed:` line and reason.
- **Spec was wrong** → correct body in place + log entry prefixed `**Correction:**` with how you know (test failure, prod incident, code diverged) and what the truth is. Only case where the body mutates without an additive section.
- **Renaming / splitting** → final log entry on the old file pointing to the new location. Keep the old file one commit longer so grep finds both.


When in doubt, append. A spec with a 10-entry change log is healthier than one rewritten 10 times with no trace.


## Subagents available for dispatch


Dispatch via the `Agent` tool with the corresponding `subagent_type`. Fall back to `general-purpose` only when none fit.


- **`atomic-builder`** (sonnet, tools: Read/Edit/Write/Grep/Glob/Bash) — feature-checkpoint builder. Cohesion-bounded: may touch many files when they form one logical slice (controller + service + DTO + entity + test for one endpoint). Writes TDD: failing test first, then implementation. Reports the atomic quality signal block. Refuses cross-cutting or architecturally ambiguous work.
- **`atomic-surgeon`** (sonnet, tools: Read/Edit/Write/Grep/Glob/Bash) — surgical 1-2 file edits. Typo fixes, single-function rewrites, mechanical renames, single-callsite bug fixes. Hard refuses 3+ file scope. Same TDD discipline and signal-block reporting as the builder.
- **`atomic-investigator`** (haiku, read-only) — code locator. Returns `file:line — what` tables. Refuses to write code, suggest fixes, or design. Dispatched as the first pass in `/subagent-implementation` Phase 0 to scope the surface area before Sonnet builder/reviewer turns.
- **`atomic-reviewer`** (sonnet, tools: Read/Grep/Bash) — diff reviewer. Verifies TDD signals were actually run (re-runs typecheck/tests itself, spot-checks new tests). Emits `## Spec compliance` + `## Code quality` subsections plus the signals block, ending with exactly one of `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED`.
- **`atomic-git-scout`** (sonnet, tools: Read/Grep/Glob/Bash) — read-only scanner for stale git state (worktrees, branches, optional remote tracking refs). Classifies cleanup candidates (`remove` / `delete` / `prune` / `ask` / `flag` / `skip`) and returns an indexed report for `/git-cleanup`. Never mutates state.
- **`atomic-signals-inferrer`** (sonnet, tools: Read/Write/Edit/Grep/Glob) — reads `.claude/project/deterministic-signals.md` and writes `inferred-signals.md`. Incremental on subsequent runs (updates only sections the diff touches). Dispatched by the `atomic-signals` skill; never modifies files outside `.claude/project/`.
- **`atomic-claude-merger`** (sonnet, tools: Read/Edit/Write/Bash) — merges `~/.claude/CLAUDE.md.atomic-proposed` into the live `~/.claude/CLAUDE.md`. Preserves user-authored sections, replaces atomic-owned ones, backs up the prior file. Dispatched by `/atomic-claude-merge`.
- **`atomic-haiku`** (haiku, tools: Read/Grep/Glob/Bash) — generic background runner for polling, status checks, log scraping, structured reporting. Read-only by default. Used by `/watch-ci`; available for any task too lightweight for Sonnet.


## Project signals (skill + agent + command)


The signals workflow keeps Claude aware of the current shape of a project without hallucination. Three artifacts compose it:


- **`atomic-signals`** (skill) — auto-fires on "regenerate signals", "scan the project", "refresh project context", "what's in this repo", "rescan". Runs `atomic signals scan` to write `.claude/project/deterministic-signals.md`, dispatches `atomic-signals-inferrer` to write `inferred-signals.md`, then ensures both files are `@`-referenced in the project's `claude.md`. Falls back to a tree-only markdown scan if the binary is absent.
- **`atomic-signals-inferrer`** (agent) — reads `deterministic-signals.md` and writes `inferred-signals.md`. Incremental: in subsequent runs it reads only the diff between scans and updates only the dependent sections. Never modifies files outside `.claude/project/`.
- **`/initialize-signals`** (command) — one-shot bootstrap for a project that has never had signals generated. Interactive, idempotent. Stops if `atomic` binary is missing.
- **`/refresh-signals`** (command) — deliberate on-demand refresh of existing signals. Refuses to run if signals were never initialized (use `/initialize-signals` instead). Delegates to the `atomic-signals` skill.

Full spec: `docs/spec/signals-workflow.md`.


## Workflow (canonical lifecycle)


1. **Plan** — `/atomic-plan` collaboratively writes to `docs/design/` or `docs/spec/`. Human approves.
2. **Implement** — `/subagent-implementation` reads the spec, runs the implement→review loop, commits per green iteration.
3. **Ship** — pick the right verb:
    - `/commit-only` — stage + commit, nothing else.
    - `/commit-and-pr` — commit + push + open PR.
    - `/pr-only` — open PR for existing commits.
    - `/merge-to-main` — merge branch into base, no squash.
    - `/commit-and-merge` — commit pending + merge.
    - `/squash-only` — squash branch commits into one, no merge.
    - `/squash-and-merge` — squash + merge to base in one shot.
    - `/commit-and-squash` — commit pending + squash branch history.
4. **Sync docs** — `/documentation` updates `README.md`, `claude.md`, `docs/spec/`, `docs/design/` after significant change.


Other commands: `/atomic-setup` (bootstrap a repo for atomic conventions — gitignore, docs/ layout, starter claude.md), `/report-issue` (open a GitHub issue against the user's current repo), `/report-issue-with-atomic` (open a GitHub issue against the atomic-claude repo itself — bugs/feature requests with the installed config, not the user's current project), `/worktree-start <branch>` (create isolated `.worktrees/<branch>/`), `/git-cleanup [<name>]` (scan stale git state — worktrees, branches, optional remote — via `atomic-git-scout`; confirm before deleting anything), `/atomic-compress <file>` (compress prose file into atomic style), `/initialize-signals` (one-shot bootstrap of project signals), `/refresh-signals` (deliberate re-scan of existing signals), `/review-branch` (dispatch `atomic-reviewer` once on `<base>..HEAD` for a pre-PR / pre-merge branch review — no orchestration loop, no spec required), `/undo-commit` (soft-undo the last commit; refuses if HEAD is a merge commit, the initial commit, or already pushed), `/watch-ci [<branch>|<pr#>|<run-id>|<workflow.yml>]` (spawn background Haiku subagent to watch CI — provider auto-detected from signals: GitHub Actions, GitLab CI, CircleCI, Jenkins, Buildkite, Bitbucket, Azure), `/remind-me <duration> <text>` (schedule a reminder via cron; degrades to file-only without `CronCreate`), `/follow-up [due <id>]` (review pending reminders — reminders surface three ways: cron fires `/follow-up due <id>`, session-start hook injects pending items at session open, and `/follow-up` on demand), `/atomic-claude-merge` (merge `~/.claude/CLAUDE.md.atomic-proposed` produced by `atomic claude install/update` into the live `~/.claude/CLAUDE.md` via the `atomic-claude-merger` agent — preserves user sections, replaces atomic-owned ones, backs up prior CLAUDE.md under `~/.claude/.atomic-backups/<ts>/`).

Atomic binary subcommands beyond `claude install` / `signals scan` / `hooks install` / `reminder` / `update`: `atomic docker init [--target DIR] [--force]` writes a Dockerfile + docker-compose.yml + entrypoint into the target dir (default `./atomic-docker/`) so users can evaluate atomic-claude on their own projects without cloning this repo. Mirror of the contributor Docker setup at the repo root (see `## Evaluations` in README.md).

# CLAUDE.md


## Principles


- Think before coding. State assumptions. Ask, don't guess. Push back on complexity. Stop when confused.
- Simplicity first. Minimum code. No speculation. No abstractions for single-use code.
- Surgical changes. Touch only what's needed. Don't improve adjacent code. Match existing style.
- Goal-driven. Define success criteria up front. Loop until verified.
- Use the model for judgment calls only (classification, drafting, summarization, extraction). Never for routing, retries, status-code handling, or deterministic transforms. If code can answer, code answers.
- Surface conflicts, don't average them. Pick one (more recent / more tested), explain why, flag the other. Never blend.
- Read before you write. Check exports, callers, shared utilities. If unsure why code is structured a certain way, ask.
- Tests verify intent, not behavior. Encode WHY. A test that can't fail when business logic changes is wrong.
- Checkpoint after every significant step. Summarize done / verified / left. Don't continue from a state you can't describe.
- Match codebase conventions even if you disagree. Surface harmful ones; don't fork silently.
- Fail loud. "Completed" is wrong if anything was skipped. Surface uncertainty, don't hide it.


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
- **Durable docs** (committed, human-facing):
  - `docs/design/<topic>.md` — design rationale, alternatives considered, brainstorming. Written collaboratively via `/atomic-plan` when classified as design.
  - `docs/spec/<topic>.md` — implementation contract for an approved feature. Written collaboratively via `/atomic-plan` when classified as spec. The canonical source for `/subagent-implementation` runs.
- **Worktrees** (gitignored): `.worktrees/<branch-name>/` — every isolated branch lives here. Created by `/worktree-start`. The ship verbs detect worktree provenance on merge/squash and prompt to delete.
- **Throwaway** (gitignored): `tmp/` — ad-hoc code experiments, scratch scripts, one-off test files. Different from `.claude/.scratchpad/` (which is the orchestrator's working memory tied to a specific task).


## Subagents available for dispatch


Dispatch via the `Agent` tool with the corresponding `subagent_type`. Fall back to `general-purpose` only when none fit.


- **`atomic-builder`** (sonnet, tools: Read/Edit/Write/Grep/Glob/Bash) — feature-checkpoint builder. Cohesion-bounded: may touch many files when they form one logical slice (controller + service + DTO + entity + test for one endpoint). Writes TDD: failing test first, then implementation. Reports the atomic quality signal block. Refuses cross-cutting or architecturally ambiguous work.
- **`atomic-surgeon`** (sonnet, tools: Read/Edit/Write/Grep/Glob/Bash) — surgical 1-2 file edits. Typo fixes, single-function rewrites, mechanical renames, single-callsite bug fixes. Hard refuses 3+ file scope. Same TDD discipline and signal-block reporting as the builder.
- **`atomic-investigator`** (haiku, read-only) — code locator. Returns `file:line — what` tables. Refuses to write code, suggest fixes, or design.
- **`atomic-reviewer`** (sonnet, tools: Read/Grep/Bash) — diff reviewer. Verifies TDD signals were actually run (re-runs typecheck/tests itself, spot-checks new tests). Emits `## Spec compliance` + `## Code quality` subsections plus the signals block, ending with exactly one of `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED`.
- **`atomic-git-scout`** (sonnet, tools: Read/Grep/Glob/Bash) — read-only scanner for stale git state (worktrees, branches, optional remote tracking refs). Classifies cleanup candidates (`remove` / `delete` / `prune` / `ask` / `flag` / `skip`) and returns an indexed report for `/git-cleanup`. Never mutates state.


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


Other commands: `/atomic-setup` (bootstrap a repo for atomic conventions — gitignore, docs/ layout, starter claude.md), `/report-issue` (open a GitHub issue), `/worktree-start <branch>` (create isolated `.worktrees/<branch>/`), `/git-cleanup [<name>]` (scan stale git state — worktrees, branches, optional remote — via `atomic-git-scout`; confirm before deleting anything), `/atomic-compress <file>` (compress prose file into atomic style), `/initialize-signals` (one-shot bootstrap of project signals), `/refresh-signals` (deliberate re-scan of existing signals), `/watch-ci [<branch>|<pr#>|<run-id>|<workflow.yml>]` (spawn background Haiku subagent to watch CI — provider auto-detected from signals: GitHub Actions, GitLab CI, CircleCI, Jenkins, Buildkite, Bitbucket, Azure), `/remind-me <duration> <text>` (schedule a reminder via cron; degrades to file-only without `CronCreate`), `/follow-up [due <id>]` (review pending reminders — reminders surface three ways: cron fires `/follow-up due <id>`, session-start hook injects pending items at session open, and `/follow-up` on demand), `/atomic-claude-merge` (merge `~/.claude/CLAUDE.md.atomic-proposed` produced by `atomic claude install/update` into the live `~/.claude/CLAUDE.md` via the `atomic-claude-merger` agent — preserves user sections, replaces atomic-owned ones, backs up prior CLAUDE.md under `~/.claude/.atomic-backups/<ts>/`).


## Project signals (auto-loaded)

@.claude/project/deterministic-signals.md
@.claude/project/inferred-signals.md

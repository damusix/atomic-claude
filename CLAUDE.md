# CLAUDE.md

@~/.claude/.atomic/config.resolved.md

## Principles


- Think before coding. State assumptions. Ask, don't guess. Push back on complexity. Stop when confused.
- Simplicity first. Minimum code. No speculation. No abstractions for single-use code.
- Surgical changes. Touch only what's needed. Don't improve adjacent code. Match existing style.
- Goal-driven. Define success criteria up front. Loop until verified. Strong success criteria let Claude loop independently.
- Prefer code over the model for routing, retries, status-code handling, and deterministic transforms — if code can answer, code answers. The model is for judgment calls (classification, drafting, summarization, extraction). Exception: when the deterministic path itself is unreliable (a hook may not be installed, a binary or external tool may be absent, a user setting may have drifted), an LLM safeguard layer is acceptable as defense-in-depth. Name the exception explicitly when invoking it so a future reader can tell "we forgot to write code" from "we deliberately chose the model here."
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


## Where things live


- **Working memory** (LLM-only, gitignored): `.claude/.scratchpad/<YYYY-MM-DD>-<desc>/` — used by `/subagent-implementation` for its implement→review loop. Holds `BRIEF.md` (pointer to spec + current iteration scope + reviewer feedback), `STATE.md` (append-only iteration log), and `FOLLOWUPS.md` (ledger of non-blocking reviewer findings carried across iterations, dispositioned with the user at finalize). Deleted on task completion.
- **Session reports** (LLM-only, gitignored): `.claude/.scratchpad/session-reports/<branch>/<YYYY-MM-DD-HHMM>-<slug>.md` — written by `/session-report` to capture what changed and why across a long-running branch's sessions. Read by the commit-message-generating ship verbs (`/commit-only`, `/commit-and-pr`, `/commit-and-push`, `/commit-and-merge`, `/commit-and-squash`, `/squash-only`, `/squash-and-merge`) as supplemental why-context for `atomic-commit`. Deleted after a successful commit on the same branch. Full spec: `docs/spec/session-report.md`.
- **Project-level follow-ups** (committed, auto-loaded): `.claude/project/followups/<id>.md` — one file per entry with YAML frontmatter (`id`, `title`, `created`, `origin`, `severity`, `review_by`, `status`, optional `file`). Auto-regenerated `INDEX.md` is the `@-ref` target (same search order as signals: `claude.local.md` → `CLAUDE.local.md` → `CLAUDE.md` → `CLAUDE.md`). Closed entries collapse to a one-line `CLOSED.md` audit-trail. Managed via `atomic followups {list,add,close,render,migrate,path}` and the `/follow-up review` subverb for stale-entry triage. Entries are promoted from a task's scratchpad `FOLLOWUPS.md` when the user picks `defer` at `/subagent-implementation` Phase 3 (shells out to `atomic followups add`). Surfaced on demand via `/remind-me` + `/follow-up`.
- **Durable docs** (committed, human-facing):
  - `docs/design/<topic>.md` — conceptual workspace: feature shape, business rules, user-facing behavior, philosophy, approaches. Written by `/atomic-plan` for non-trivial work; skipped for trivial.
  - `docs/spec/<topic>.md` — implementation contract derived from the design. Written by `/atomic-plan` (inline for trivial; subagent-looped for non-trivial). The canonical source for `/subagent-implementation` runs.
- **Worktrees** (gitignored): `.worktrees/<branch-name>/` — every isolated branch lives here. Created by `/worktree-start`. The ship verbs detect worktree provenance on merge/squash and prompt to delete.
- **Throwaway** (gitignored): `tmp/` — ad-hoc code experiments, scratch scripts, one-off test files. Different from `.claude/.scratchpad/` (which is the orchestrator's working memory tied to a specific task).
- **Atomic-owned state** (per-user, never committed): `~/.claude/.atomic/` — holds `config.toml` (shell-settable defaults via `atomic config`), `config.resolved.md` (auto-loaded into every session), `backups/<ts>/` (`atomic claude update` backups), and `proposed/CLAUDE.md` (divergence merge target).


## Four doc voices, four surfaces


- **Atomic style** — Claude's TUI replies. Governed by `output-styles/atomic.md`.
- **`atomic-prose` skill** — enduring narrative docs: `README.md`, `docs/guides/`, CHANGELOG narrative entries.
- **Spec/design voice** — `docs/spec/*`, `docs/design/*`. Tables, diagrams, terse bullets. Append-mostly for specs.
- **LLM-reference voice** — `CLAUDE.md`, `claude.local.md`, `.claude/project/*-signals.md`. Technical-imperative, conventions and dispatch contracts only, never tutorial or narrative.

Diff-signal → surface routing lives in the `atomic-documentation` skill. Invoke `/documentation` to apply, or let ship verbs fire it automatically on staged diffs.


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
- **`atomic-strategist`** (opus, tools: Read/Grep/Glob/Bash) — heavyweight reasoning agent for revising plans, auditing specs/designs, and reasoning through hard problems. Read-only. Surfaces hidden assumptions, names tradeoffs, recommends approaches with explicit confidence. Does not implement, does not gate diffs, does not locate code. Dispatch when the question is "is this the right approach?" not "is this code correct?".
- **`atomic-reviewer`** (sonnet, tools: Read/Grep/Bash) — diff reviewer. Verifies TDD signals were actually run (re-runs typecheck/tests itself, spot-checks new tests). Emits `## Spec compliance` + `## Code quality` subsections plus the signals block, ending with exactly one of `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED`.
- **`atomic-git-scout`** (sonnet, tools: Read/Grep/Glob/Bash) — read-only scanner for stale git state (worktrees, branches, optional remote tracking refs). Classifies cleanup candidates (`remove` / `delete` / `prune` / `ask` / `flag` / `skip`) and returns an indexed report for `/git-cleanup`. Never mutates state.
- **`atomic-signals-inferrer`** (sonnet, tools: Read/Write/Edit/Grep/Glob/Agent) — reads `.claude/project/deterministic-signals.md` and writes `signals.md` (the router). On large repos, dispatches sub-agents per domain to write `signals/<domain>/` files, runs reviewer per domain file, wires cross-domain refs. On small repos, writes everything directly into `signals.md`. Dispatched by the `atomic-signals` skill; never modifies files outside `.claude/project/`.
- **`atomic-claude-merger`** (sonnet, tools: Read/Edit/Write/Bash) — merges `~/.claude/.atomic/proposed/CLAUDE.md` into the live `~/.claude/CLAUDE.md`. Preserves user-authored sections, replaces atomic-owned ones, backs up the prior file. Dispatched by `/atomic-claude-merge`.
- **`atomic-haiku`** (haiku, tools: Read/Grep/Glob/Bash) — generic background runner for polling, status checks, log scraping, structured reporting. Read-only by default. Used by `/watch-ci`; available for any task too lightweight for Sonnet.


## Project signals (skill + agent + command)


The signals workflow keeps Claude aware of the current shape of a project without hallucination. Three artifacts compose it:


- **`atomic-signals`** (skill) — auto-fires on "regenerate signals", "scan the project", "refresh project context", "what's in this repo", "rescan". Runs `atomic signals scan` to write `.claude/project/deterministic-signals.md`, dispatches `atomic-signals-inferrer` to write `signals.md`, then ensures both files are `@`-referenced in the project's `CLAUDE.md`. Falls back to a tree-only markdown scan if the binary is absent.
- **`atomic-signals-inferrer`** (agent) — reads `deterministic-signals.md` and writes `signals.md` (the router). On large repos, dispatches sub-agents per domain to write domain files, runs reviewer per domain file, wires cross-domain refs. On small repos, writes everything into `signals.md` directly. Never modifies files outside `.claude/project/`.
- **`/initialize-signals`** (command) — one-shot bootstrap for a project that has never had signals generated. Interactive, idempotent. Stops if `atomic` binary is missing.
- **`/refresh-signals`** (command) — deliberate on-demand refresh of existing signals. Refuses to run if signals were never initialized (use `/initialize-signals` instead). Delegates to the `atomic-signals` skill.

Full spec: `docs/spec/signals-workflow.md`.


## Workflow (canonical lifecycle)


1. **Plan** — `/atomic-plan` gauges triviality. Trivial → inline spec. Non-trivial → design doc + spec authored via subagent loop (`atomic-builder` writes, `atomic-reviewer` checks alignment in spec-mode). Optionally grounds via `atomic-investigator` and consults `atomic-strategist` on hard tradeoffs. Human approves.
2. **Implement** — `/subagent-implementation` reads the spec, runs the implement→review loop, commits per green iteration.
3. **Ship** — pick the right verb:
    - `/commit-only` — stage + commit, nothing else.
    - `/commit-and-push` — commit + push (no PR, no merge). Trunk-based counterpart to `/commit-and-pr`.
    - `/commit-and-pr` — commit + push + open PR.
    - `/push-only` — push existing commits (no commit, no PR). Trunk-based counterpart to `/pr-only`.
    - `/pr-only` — open PR for existing commits.
    - `/merge-to-main` — merge branch into base, no squash.
    - `/commit-and-merge` — commit pending + merge.
    - `/squash-only` — squash branch commits into one, no merge.
    - `/squash-and-merge` — squash + merge to base in one shot.
    - `/commit-and-squash` — commit pending + squash branch history.
4. **Sync docs** — `/documentation` runs a diff-scoped doc-impact pass via the `atomic-documentation` skill (per-surface edit / skip / continue). Ship verbs fire the same skill on the staged diff automatically.


## Other commands


**Routing / planning**


- `/atomic-help [<topic> | <freeform intent>]` — routing assistant for a lost user. Reads git state, classifies intent, recommends one next verb. No menus, never executes.
- `/pressure-test [<topic> | @<path-to.md>]` — Socratic challenger session. Pressure-tests assumptions, surfaces contradictions, forces fuzzy maybes into yes/no through questions only. No artifacts. Pairs with `/atomic-plan` as a pre-approval gate.
- `/review-branch` — dispatches `atomic-reviewer` once on `<base>..HEAD` for a pre-PR / pre-merge branch review. No orchestration loop, no spec required.
- `/subagent-diagnose <ci|bug> [args]` — multi-agent failure-investigation orchestrator. `ci` mode pulls a failed CI run's logs and drives a fix loop; `bug` mode starts from a freeform symptom. Same scratchpad + investigator + builder/surgeon + reviewer loop as `/subagent-implementation`.


**Repo bootstrap**


- `/atomic-setup` — bootstrap a repo for atomic conventions: gitignore, `docs/` layout, starter `CLAUDE.md`.
- `/initialize-signals` — one-shot bootstrap of project signals.
- `/refresh-signals` — deliberate re-scan of existing signals.
- `/worktree-start <branch>` — create isolated `.worktrees/<branch>/`.


**Maintenance**


- `/git-cleanup [<name>]` — scan stale git state (worktrees, branches, optional remote) via `atomic-git-scout`. Confirm before deleting anything.
- `/undo-commit` — soft-undo the last commit. Refuses if HEAD is a merge commit, the initial commit, or already pushed.
- `/atomic-claude-merge` — merge `~/.claude/.atomic/proposed/CLAUDE.md` produced by `atomic claude install/update` into the live `~/.claude/CLAUDE.md` via the `atomic-claude-merger` agent. Preserves user sections, replaces atomic-owned ones, backs up prior `CLAUDE.md` under `~/.claude/.atomic/backups/<ts>/`.
- `/atomic-compress <file>` — compress prose file into atomic style.


**Session memory / reminders**


- `/session-report [<slug>]` — capture what changed and why for the current branch's session. Writes to `.claude/.scratchpad/session-reports/<branch>/`. Read and deleted by the next commit-message-generating ship verb.
- `/remind-me <duration> <text>` — schedule a reminder via cron. Degrades to file-only without `CronCreate`.
- `/follow-up [due <id> | review]` — review pending reminders. Surface paths: cron fires `/follow-up due <id>`, session-start hook injects pending items at session open, `/follow-up` on demand. `/follow-up review` triages stale `.claude/project/followups/` entries with per-item `extend|close|promote|skip` disposition.


**Observability / reporting**


- `/watch-ci [<branch>|<pr#>|<run-id>|<workflow.yml>]` — spawn background Haiku subagent to watch CI. Provider auto-detected from signals: GitHub Actions, GitLab CI, CircleCI, Jenkins, Buildkite, Bitbucket, Azure.
- `/report-issue` — open a GitHub issue against the user's current repo.
- `/report-issue-with-atomic` — open a GitHub issue against the atomic-claude repo itself. Bugs / feature requests with the installed config, not the user's current project.


## Atomic binary subcommands


Beyond `claude install` / `signals scan` / `hooks install` / `reminder` / `update`:


- `atomic docker init [--target DIR] [--force]` — writes a Dockerfile + docker-compose.yml + entrypoint into the target dir (default `./atomic-docker/`) so users can evaluate atomic-claude on their own projects without cloning this repo. Mirror of the contributor Docker setup at the repo root (see `## Evaluations` in README.md).
- `atomic doctor [--fix] [--json] [--only <cat[,...]>] [--skip <cat[,...]>] [--stale-days N] [--verbose]` — runs nine indexed integrity checks (install, hooks, signals, refs, manifest, followups, memory, binary, config) against `~/.claude/` and the current project. Exits 0 (PASS or only WARN/SKIP), 1 (any FAIL), 2 (usage error). `--fix` prompts per item to apply repairs. Spec: `docs/spec/atomic-doctor.md`.
- `atomic validate [spec|config|bundle] [paths...]` — deterministic lints against the repo's artifacts: spec markdown structure (S0/S1/S5/S6), cross-reference integrity in CLAUDE.md / commands / agents / skills (C1/C3/C5/C7/C9), bundle parity against the embedded manifest. No args → whole-repo run. `--json` for machine output, `--suggest` for structural template hints. Exit 1 on any FAIL, 2 on internal error.
- `atomic update [--check] [--channel <stable|prerelease>] [--no-doctor]` — self-updates the binary from GitHub Releases (SHA256-verified). After a successful binary swap, runs `doctor.Run` with `signals` and `binary` skipped, prints FAIL lines only (silent on healthy). Update success preserved unconditionally — doctor outcome (including panics) never changes exit code. Disable per-invocation with `--no-doctor` or durably via `update.run_doctor = false` in `~/.claude/.atomic/config.toml`. Precedence: flag > config > default (`true`). Spec: `docs/spec/atomic-update-doctor.md`.
- `atomic followups <list|add|close|render|migrate|path>` — manages the per-entry follow-ups folder at `.claude/project/followups/`. `list [--stale] [--json]` enumerates open entries; `add --id <id> --title <t> --severity <s> --origin <o> [--file <f>] [--body -]` writes a new entry (deterministic frontmatter, LLM-free); `close <id> [--reason <r>]` appends to `CLOSED.md` and deletes the entry file; `render` regenerates `INDEX.md`; `migrate` one-shot converts the legacy `followups.md` into the folder layout; `path` prints the absolute folder path. Spec: `docs/spec/follow-ups-folder.md`.
- `atomic claude uninstall` — reverses `atomic claude install`. Reads `~/.claude/.atomic/pre-install/manifest.json` (exit 1 if missing — no snapshot, no uninstall), computes a restore plan (files to restore, files to delete, files needing LLM merge), and outputs a structured prompt to stdout. Claude receives the prompt, confirms the plan with the user, LLM-merges `settings.json` and `CLAUDE.md` if they were modified post-install, restores pre-install files, deletes atomic-only artifacts, removes `~/.claude/.atomic/`, and prints the binary removal instruction. TTY-aware: when run interactively outside a Claude session, prints a hint to run inside Claude Code instead. Binary is never removed by the CLI. Spec: `docs/spec/uninstall.md`.

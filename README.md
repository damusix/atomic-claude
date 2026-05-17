# Atomic Claude

```
            _.-^^---....,,--
        _--                  --_
      <      Atomic Claude       >
       \._                    _./
          ```--. . , ; .--'''
                | |   |
             .-=||  | |=-.
             `-=#$%&%$#=-'
                | ;  :|
       _____.,-#%&$@%#&#~,._____
```

A Claude Code configuration that trades narrative for signal. Less filler in Claude's responses, an opinionated workflow from idea to merged PR, and a clear split between *skills* that fire automatically (TDD, commits, verification, debug, review) and *commands* you invoke explicitly.

Atomic Claude opts in to discipline rather than imposing it everywhere. Skills auto-fire only where the trigger is well-bounded; everything else is a slash command you reach for on purpose. Inspired by [caveman](https://github.com/JuliusBrussee/caveman) and [superpowers](https://github.com/obra/superpowers); see [Credits](#credits) for what each one did right and where atomic-claude diverges.

Use it in personal projects or at work. Licensed [MIT](LICENSE). Still evolving: APIs and behaviors may shift between releases, but breaking changes are flagged in the changelog and ship verbs are designed to be safe to interrupt.

**Why choose Atomic Claude?**

- **Faster decision making.** Claude's TUI replies skip narrative scaffolding and put the technical substance first.
  - Less prose to skim, more signal per line. A six-word answer beats a three-sentence one when both say the same thing.
  - Diagrams over prose when they carry the load (3+ entities with relationships, ordered interactions, state transitions). A flowchart beats three paragraphs.
- **Project-state awareness.** The `atomic` binary scans your repo and writes signals files Claude loads on every session.
  - Knows your build, test, and lint commands instead of guessing.
  - Knows your framework, conventions, and package manager (`npm` vs `pnpm` vs `yarn`).
  - No hallucinated `npm run` scripts, no invented `make` targets.
- **Work survives across sessions.** `/clear` your context, close your laptop, take a week off: rerun the same command and pick up where you left off.
  - Specs are append-mostly with a `## Change log` of every amendment.
  - `/subagent-implementation` keeps `BRIEF.md`, `STATE.md`, and `FOLLOWUPS.md` in `.claude/.scratchpad/`, so the next run reads what's done, what's next, and which non-blocking findings need a disposition.
  - `/remind-me <duration> <text>` schedules a cron-fired reminder.
  - `/follow-up` lists pending items at session open or on demand.
- **Operational tooling.**
  - `/watch-ci`: background CI observation that survives `/clear`.
  - `/git-cleanup`: scan stale worktrees, branches, and (optionally) remote tracking refs; per-item confirm.
  - `/worktree-start`: isolated worktree with auto-detected `npm install` / `cargo build` / etc. plus a baseline test run.
  - `/atomic-setup`: bootstrap a fresh repo for atomic conventions (gitignore, docs/ layout, starter CLAUDE.md).
  - `/atomic-claude-merge`: merge `~/.claude/CLAUDE.md` updates with your local edits.

**Hooks are optional.** Install registers a `SessionStart` hook by default so pending reminders surface at session open, but the system is designed to work without it. If corporate policy blocks `settings.json` modifications, run `atomic claude install --no-hooks`. The commands, skills, agents, output style, signals workflow, and the full implement → review loop all work unchanged. You lose automatic reminder surfacing (use `/follow-up` manually instead).


## Install

Two commands. The first lays down the `atomic` binary; the second wires everything else up.

```bash
curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash
atomic claude install
```

`atomic claude install` does three things in one shot:

1. Lays the artifact bundle (CLAUDE.md, agents, commands, skills, output-styles, rules) into `~/.claude/`. SHA256-idempotent; backs up any changed files under `~/.claude/.atomic-backups/<timestamp>/`.
2. Registers the `SessionStart` hook in `~/.claude/settings.json` so pending reminders surface when you open a Claude Code session.
3. Prints the two manual steps it **can't** automate (see the next section).

Flags: `--target ./.claude` for project-scoped install, `--dry-run` to preview, `--no-hooks` to skip hook registration (use if you manage hooks yourself).

Full prerequisites, Windows/WSL2 notes, manual install, and build-from-source: [docs/guides/install.md](docs/guides/install.md). To try it in a throwaway Docker container first: [docs/guides/evaluations.md](docs/guides/evaluations.md).


## Activate the output style (opt-in)

Installing the bundle drops `output-styles/atomic.md` into `~/.claude/output-styles/` but does **not** turn it on. Claude Code treats output styles as a user preference, not a config the bundle can flip on your behalf. Opt in explicitly:

1. Open Claude Code in the repo where you want to evaluate it.
2. Run `/config` and select **Output style**.
3. Pick **Atomic** from the menu.

`/config` saves your choice to that project's `.claude/settings.local.json` as `"outputStyle": "Atomic"`. To apply it everywhere instead, edit `~/.claude/settings.json` directly with the same key. There is no global menu option.

Notes:

- **Takes effect on the next session**, not the current one. Claude Code fixes the system prompt at session start to keep prompt caching warm.
- **Tone-only layer.** Most of what makes the repo feel atomic comes from `CLAUDE.md` + skills, not the output style. Skip activation and the commands and skills still work; you lose extra prose compression.
- **Additive, not a replacement.** The file sets `keep-coding-instructions: true`, so selecting Atomic preserves Claude Code's default engineering guidance (scope, comments, verification, security) and stacks atomic's tone rules on top.
- **Switch intensity mid-session** by saying "atomic lite", "atomic full" (default), or "atomic ultra". Runtime instruction, not a settings change.

Full details: [docs/reference/output-style.md](docs/reference/output-style.md).


## How to use this system

The canonical flow takes a feature from "I want to build X" to "X is shipped." Five steps, each with a clear handoff.


### 1. `/initialize-signals`: teach Claude what your repo looks like

Run this **once per repo**, the first time you open Claude Code in it. The `atomic` binary scans the project and writes two files:

- `.claude/project/deterministic-signals.md`: facts (directory tree, manifests, lockfiles, language LOC).
- `.claude/project/inferred-signals.md`: interpretation (framework detection, build/test/lint commands, architectural style, conventions).

Both files are auto-loaded into every Claude Code session via `@`-refs in `CLAUDE.md`. Claude knows your repo's shape without you having to explain it each session, and won't hallucinate commands that don't exist.

After the first run, signals refresh on their own. The `atomic-signals` skill auto-fires on phrases like "rescan the project" and runs silently when `/commit-only` detects source-tree changes. Use `/refresh-signals` to force a re-scan.

**Expect:** a few seconds. Two new committed files under `.claude/project/`. A reference added to your `CLAUDE.md`.


### 2. `/atomic-plan`: plan the feature together

This is the **human approval gate**. You describe what you want to build. Claude asks clarifying questions, surfaces tradeoffs, and writes a checkpoint table to one of two places:

- `docs/design/<topic>.md`: when you're still in brainstorm/rationale mode. Alternatives considered, open questions.
- `docs/spec/<topic>.md`: when the design is settled and ready to implement. Each checkpoint is a discrete unit of work with success criteria.

The spec is the contract that step 4 will follow autonomously. Get it right here so you can step away later. Specs are **append-mostly**: every amendment adds a dated entry to a `## Change log` section, so the original intent and the reason for every later change stay in the file. See `CLAUDE.md` → "Spec files are append-mostly" for the full rule.

**Expect:** a conversation, not a one-shot. Mermaid diagrams allowed. Nothing gets implemented until you approve.


### 3. `/worktree-start <branch-name>`: isolate the work

Creates `.worktrees/<branch-name>/` with a fresh branch checked out. Auto-detects your project setup (`npm install`, `cargo build`, `pip install`, `go mod download`), runs a baseline test to confirm green, and reports ready.

Why bother: the next step runs autonomously and can take a while. The worktree lets it run without disrupting whatever's checked out in your main working tree, so you can keep editing in the main checkout while Claude implements in parallel.

**Expect:** ~30 seconds for setup + baseline test. Skipped if you're already inside `.worktrees/*`.


### 4. `/subagent-implementation`: let Claude build it

The autonomous part. The orchestrator reads `docs/spec/<topic>.md`, writes a thin `BRIEF.md` + `STATE.md` + `FOLLOWUPS.md` to `.claude/.scratchpad/`, then drives an implement → review loop:

1. `atomic-builder` (or `atomic-surgeon` for small edits) writes a failing test first, then the implementation, then runs typecheck / tests / build / lint.
2. `atomic-reviewer` runs against the diff in a fresh context. Verifies the quality signals were executed, not claimed. Emits findings plus `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED`.
3. On `PASS`: commit, move to next checkpoint. On `CHANGES_REQUESTED`: feedback goes back to the builder, loop continues.

Non-blocking findings (🟡 risks, 🔵 nits, ❓ questions) accumulate in `FOLLOWUPS.md` across iterations. At finalize, you disposition each one: fix now, file an issue, or drop. Nothing gets swept under the rug because the iteration passed.

**Expect:** runs until the spec is satisfied. Commits land per green checkpoint. Step in any time to redirect.


### 5. `/commit-and-pr`: ship it

Commits any leftover pending changes (message format via the `atomic-commit` skill), pushes the branch, and opens a PR via `gh`. The PR body is drafted via `atomic-review` so the tone matches.

Eight ship verbs cover the combinations of "commit pending work" × "tidy history" × "open PR" × "merge to base". Pick the one that matches where you're heading:

| Verb | Commit pending? | Squash? | PR? | Merge to base? |
|------|-----------------|---------|-----|----------------|
| `/commit-only` | yes | | | |
| `/commit-and-pr` | yes | | yes | |
| `/commit-and-squash` | yes | yes | | |
| `/commit-and-merge` | yes | | | yes |
| `/pr-only` | | | yes | |
| `/squash-only` | | yes | | |
| `/squash-and-merge` | | yes | | yes |
| `/merge-to-main` | | | | yes |

Shared behavior across the family:

- Commit messages delegate to the `atomic-commit` skill; PR bodies to `atomic-review`.
- **Signals stay fresh automatically.** `/commit-only` invokes the `atomic-signals` skill (silent mode) when the staged diff touches source files. `/merge-to-main`, `/squash-only`, and `/squash-and-merge` run a post-op refresh as defense in depth against history that bypassed `/commit-only`. The compound `commit-and-*` verbs inherit the refresh by delegating to `/commit-only`. Only `/pr-only` skips it (working tree must already be clean). You rarely need to run `/refresh-signals` by hand.
- Merge and squash verbs invoke `atomic-verify` before touching base, re-run tests on the merged tip, and prompt to delete the worktree if the branch came from `.worktrees/`.
- On significant changes (new file, removed file, public-API change, dependency change), each verb prints a one-line reminder to run `/documentation` so README, `CLAUDE.md`, and `docs/spec/` stay in sync.

**Expect:** a PR link in your terminal. Tests pass before anything touches the base branch.


## What's in here

| Path | What it is | Reference |
|------|------------|-----------|
| `output-styles/atomic.md` | Strips ceremony from Claude's TUI replies. Three intensity levels (lite / full / ultra). | [docs/reference/output-style.md](docs/reference/output-style.md) |
| `commands/` | Slash commands for planning, implementation, shipping, repo hygiene. | [docs/reference/commands.md](docs/reference/commands.md) |
| `skills/` | Discipline skills that auto-fire on matching phrases. | [docs/reference/skills.md](docs/reference/skills.md) |
| `agents/` | Subagents the orchestrator dispatches. | [docs/reference/agents.md](docs/reference/agents.md) |
| `rules/` | Path-scoped instructions, glob-gated via `paths:` frontmatter. Load only when matching files enter context. Starter rules for TypeScript and Python. | |
| `CLAUDE.md` | Global instructions installed at `~/.claude/CLAUDE.md`. | |


## Design philosophy

Five enduring axioms shape the system: cohesion-bounded scope, memory over config, explicit confirm for destructive ops, plain-text indexed selection, skills auto-fire vs commands explicit. Read [`.claude/docs/axioms.md`](.claude/docs/axioms.md) before adding new artifacts.


## Where things live

| Path | Purpose |
|------|---------|
| `docs/spec/<topic>.md` | Implementation contracts; what `/subagent-implementation` follows. |
| `docs/design/<topic>.md` | Rationale, alternatives, open questions. |
| `docs/guides/` | Install, evaluations, contributing. |
| `docs/reference/` | Commands, skills, agents, output-style, workflow, signals, conventions. |
| `.worktrees/<branch>/` | Isolated worktree per feature branch. Gitignored. |
| `.claude/.scratchpad/<date>-<desc>/` | Working memory for `/subagent-implementation`. LLM-only, gitignored, ephemeral. |
| `.claude/rules/<lang>/*.md` | Path-scoped instructions, glob-gated. |
| `tmp/` | Throwaway experiments, ad-hoc test scripts. |


## Documentation

| Topic | Path |
|-------|------|
| Install (prereqs, WSL2, manual, source) | [docs/guides/install.md](docs/guides/install.md) |
| Docker evaluation environment | [docs/guides/evaluations.md](docs/guides/evaluations.md) |
| Contributing (link-local.sh, dogfood loop) | [docs/guides/contributing.md](docs/guides/contributing.md) |
| Lifecycle + ship verbs | [docs/reference/workflow.md](docs/reference/workflow.md) |
| Commands reference | [docs/reference/commands.md](docs/reference/commands.md) |
| Skills reference | [docs/reference/skills.md](docs/reference/skills.md) |
| Agents reference | [docs/reference/agents.md](docs/reference/agents.md) |
| Output style + intensity levels | [docs/reference/output-style.md](docs/reference/output-style.md) |
| Signals workflow | [docs/reference/signals-workflow.md](docs/reference/signals-workflow.md) |
| Conventions | [docs/reference/conventions.md](docs/reference/conventions.md) |
| Implementation specs | [docs/spec/](docs/spec/) |


## Credits

Atomic Claude wouldn't exist without two projects that do their thing well. Credit where it's due.

**[caveman](https://github.com/JuliusBrussee/caveman)** (Julius Brussee). 61k stars, and earned. Caveman pioneered the compressed-output pattern for Claude Code and proved you can ship ~65% token savings without sacrificing technical accuracy. The intensity-level naming (lite / full / ultra) atomic-claude uses comes straight from there. Install it if its style fits you. Why this repo exists alongside it: caveman's voice is ooga-booga by design, and I wanted something that read more like a colleague. Full sentences when they help, diagrams and code blocks where they communicate better than prose, terse only where terseness wins.

**[superpowers](https://github.com/obra/superpowers)** (Jesse Vincent / obra). The most comprehensive skill toolkit for Claude Code I've used. The TDD discipline, verification-before-completion, subagent-driven development, and worktree workflows that atomic-claude leans on are all superpowers territory. It's the right answer for a lot of workflows. Why this repo exists alongside it: superpowers leans hard on auto-firing skills by design. `brainstorming` will kick in and start drafting a design spec on a single offhand comment. That's the intended UX, and for some flows it's perfect; for what I wanted, it was overbearing. Atomic-claude keeps the same disciplines but moves most of them into explicit slash commands you reach for on purpose.

**[stop-slop](https://github.com/hardikpandya/stop-slop)** (Hardik Pandya, MIT). The rule set behind `atomic-prose`. Stop-slop is a focused skill for removing predictable AI patterns from prose (throat-clearing, em dashes, marketing jargon, false agency). Atomic Claude's `atomic-prose` skill adapts those rules for developer documentation: kept the anti-marketing core, the active-voice requirement, and the no-em-dash rule; dropped essay-targeted guidance (manufactured profundity, performative sincerity); added a boundary against the atomic TUI style and a "keep some narrative" rule so doc prose does not collapse into telegraphic fragments. If you write blog posts or essays as well as docs, run stop-slop for the broader rule set.

Both projects are worth running on their own terms. If atomic-claude's tradeoffs don't fit your style, those two might.


## Comparison with caveman and superpowers

The table groups capabilities by what they do, not where they sit in each project's structure.

| Capability | Atomic Claude | Superpowers | Caveman |
|------------|---------------|-------------|---------|
| Output compression / tone | `output-styles/atomic.md` (lite / full / ultra) | — | `/caveman` (lite / full / ultra / wenyan) |
| TDD enforcement | `atomic-tdd` skill | `test-driven-development` skill | — |
| Verify before claiming done | `atomic-verify` skill | `verification-before-completion` skill | — |
| Systematic debugging | `atomic-debug` skill | `systematic-debugging` skill | — |
| Commit-message format | `atomic-commit` skill | — | `/caveman-commit` |
| Code-review tone | `atomic-review` skill | `requesting-code-review`, `receiving-code-review` | `/caveman-review` |
| Narrative-doc voice (README, guides) | `atomic-prose` skill | — | — |
| Brainstorm / plan | `/atomic-plan` (one verb, picks design vs spec) | `brainstorming`, `writing-plans` (split) | — |
| Execute a plan | `/subagent-implementation` | `executing-plans`, `subagent-driven-development` | — |
| Parallel subagents | `atomic-builder` / `atomic-surgeon` / `atomic-investigator` / `atomic-reviewer` | `dispatching-parallel-agents` skill | `cavecrew-*` (investigator / builder / reviewer) |
| Worktree isolation | `/worktree-start` | `using-git-worktrees` skill | — |
| Ship a branch | `/commit-only`, `/commit-and-pr`, `/merge-to-main`, `/squash-and-merge`, … (7 verbs) | `finishing-a-development-branch` skill | — |
| Compress a markdown file | `/atomic-compress <file>` | — | `/caveman-compress <file>` |
| Project signal scanning | `/initialize-signals` + `atomic` binary + `atomic-signals` skill | — | — |
| Cron-backed reminders | `/remind-me`, `/follow-up` | — | — |
| CI observation | `/watch-ci` | — | — |
| Stale git cleanup | `/git-cleanup` + `atomic-git-scout` agent | — | — |
| Bootstrap a fresh repo | `/atomic-setup` | — | — |
| Token usage stats | — | — | `/caveman-stats` |
| MCP middleware compression | — | — | `caveman-shrink` (npm) |
| Meta: write a new skill | — | `writing-skills` skill | — |

A few honest notes:

- **Atomic borrows visibly from both.** The intensity-level naming (lite / full / ultra) is straight from caveman. The skills + agents split, TDD discipline, verification gate, and worktree workflow are superpowers territory.
- **Atomic adds project-state awareness and a durable workflow.** Signals scanning, the `atomic` binary, reminders, CI watching, git cleanup, and the spec → implement → review loop with a `FOLLOWUPS.md` ledger are atomic-specific.
- **Atomic is more opinionated about explicit vs implicit.** Superpowers leans on auto-firing skills (it's the design point); atomic uses skills sparingly (6 of them) and pushes most behavior into explicit slash commands. Caveman is mixed: `/caveman` is a command but also auto-activates per session for supported agents.


## License

[MIT](LICENSE). Use it in personal projects or at work. No warranty, no liability; standard MIT terms apply.

Contributions welcome. See [docs/guides/contributing.md](docs/guides/contributing.md) for the dogfood workflow.


## Status

Still evolving. Commands, agents, and skills may change between releases. Breaking changes are called out in commit messages and the changelog. The ship verbs (`/commit-only`, `/merge-to-main`, `/squash-and-merge`, etc.) are designed to be safe to interrupt and easy to undo. When in doubt, lean on `git reflog`.

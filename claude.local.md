# claude.local.md

Project-local context for working **on** this repo. Not copied anywhere — read by Claude only when the cwd is this repo.


## What this repo is

A holistic Claude Code configuration. The artifacts here (`CLAUDE.md`, `commands/`, `agents/`, `skills/`, `output-styles/`) are designed as one coherent system — atomic output style, an opinionated command set, a small subagent roster, and discipline skills that interlock. Not a grab-bag; everything is meant to compose.

Replaces (for the author) heavier toolkits like superpowers and caveman. Personal config, no stability guarantee.


## Platform support

Target macOS and Linux only. Drop Windows-specific review findings, Windows-only test paths (`os.PathSeparator` probes), and Windows compatibility gates. `.goreleaser.yaml` may still produce Windows binaries — that's fine, but correctness on Windows is not a concern.


## File roles (this repo specifically)

| File | Role | Destination |
|------|------|-------------|
| `CLAUDE.md` | Global instructions. Gets copied to `~/.claude/CLAUDE.md`. Affects every workspace, not just this repo. | `~/.claude/CLAUDE.md` |
| `claude.local.md` | This file. Project-local context for editing this repo. Gitignored. | Stays here, cwd-scoped. |
| `CLAUDE.md` | The committed project instructions for anyone working in this repo. Mirrors `CLAUDE.md` content because this repo *is* the config source. | Repo root, committed. |
| `README.md` | Human-facing overview of what the config does and how to install it. | Repo root, committed. |
| `commands/*.md` | **Rendered** slash command definitions. Edit `templates/commands/<verb>.md` (and `templates/shared/<flow>.md` for cross-verb partials); `make render` regenerates these. Copied to `~/.claude/commands/` by `atomic claude install`. | `~/.claude/commands/` |
| `templates/commands/<verb>.md` | Source of truth for command files. Either a single `{{ template "<flow>" . }}` directive plus verb-specific orchestration, or a self-contained body if no partial applies. | Renders to `commands/<verb>.md`. |
| `templates/shared/<name>.md` | Reusable partials composed by command templates via `{{ template "<name>" . }}`. Two-level taxonomy: big partials (`commit-flow`, `pr-flow`, `merge-flow`, `squash-flow`, `push-flow`) + small partials (`doc-impact`, `doc-impact-why`, `signals-gate`, `base-resolution`, `worktree-cleanup-prompt`). | Not copied directly; consumed at render time. |
| `agents/*.md` | Subagent definitions. Copied to `~/.claude/agents/`. | `~/.claude/agents/` |
| `skills/*/SKILL.md` | Discipline skills. Copied to `~/.claude/skills/`. | `~/.claude/skills/` |
| `output-styles/*.md` | Output style definitions. Copied to `~/.claude/output-styles/`. | `~/.claude/output-styles/` |
| `rules/<lang>/*.md` | Path-scoped topic rules. `paths:` frontmatter globs against filetypes (e.g. `**/*.{ts,tsx}`, `**/*.py`) so the rule only loads when Claude touches a matching file. Currently: `typescript/`, `python/`. Expand with more languages or topic subdirs as needed. | `~/.claude/rules/` (via `atomic claude install`) |


## Reference docs (load every session — longform data, placed before behavioral rules)


### Design axioms

@.claude/docs/axioms.md

Decisions that emerged from this work and shouldn't be re-litigated each session: cohesion-bounded scope, memory > config, destructive-ops explicit confirm, plain-text indexed selection, skills auto-fire vs commands explicit. Read before adding new commands, skills, or agents.


### Agent configuration reference

@.claude/docs/agent-config.md

How Claude Code agents, skills, commands, and output styles are defined — frontmatter shapes, tool restrictions, model selection, dispatch semantics. Consult before editing any artifact in `agents/`, `skills/`, `commands/`, or `output-styles/`.


### Claude Code upstream docs

@.claude/docs/claude-code-references.md

URL index for official Claude Code documentation: agents, sub-agents, skills, commands, hooks, hooks-guide, tools-reference, worktrees, scheduled-tasks, headless. Fetch via WebFetch when verifying semantics — these URLs are the source of truth, not the local snapshots in `agent-config.md`.


### Prompting best practices

@.claude/docs/prompting-best-practices.md

Anthropic's official prompt engineering guide, distilled for this repo's artifact authoring. Covers Opus 4.7 behavioral notes, XML structuring, positive framing, parallel tool calls, thinking guidance, and agentic patterns. Consult before editing agents, skills, or commands.


### Project signals (auto-loaded)

@.claude/project/signals.md


### Project follow-ups (auto-loaded)

@.claude/project/followups/INDEX.md


## Coherence rules (when editing here)

- Treat the four artifact types (commands, agents, skills, output-styles) as one system. A change to one often demands a matching change to the others.
- `CLAUDE.md` is the global contract. Adding a command/agent/skill that other artifacts reference? Update `CLAUDE.md` so every workspace knows it exists.
- `README.md` is the public-facing index. New artifact, removed artifact, or renamed verb → update the tables.
- Atomic output style applies to Claude's TUI replies, not to the files in this repo. Command/agent/skill prose stays in normal English so it reads cleanly when installed.
- Skill triggers, agent dispatch criteria, and command behaviors must not contradict each other. If `/atomic-plan` says it writes to `docs/spec/` and an agent expects `docs/specs/`, that's a bug.


## Adding a new artifact (mandatory checklist)


This is the **invisible-feature prevention checklist**. A new artifact is not "done" until every applicable row is updated. Skipping a row means the feature exists in code but nobody — user, agent, or future-you — knows it exists.


Run this whenever you add, rename, or remove a command / agent / skill / output-style / rule. Do not batch across artifacts — finish the checklist for one before starting the next.

<mandatory_checklist>

| # | Surface | When to update | What to write |
|---|---------|----------------|---------------|
| 1 | The artifact file itself | Always | `agents/atomic-*.md`, `templates/commands/<verb>.md` (NEVER `commands/<verb>.md` directly — that's rendered), `skills/<name>/SKILL.md`, `output-styles/atomic-*.md`, or `rules/<lang>/*.md`. Use `atomic-` prefix for custom artifacts. For new commands, also run `make render` to materialize `commands/<verb>.md`. |
| 2 | `CLAUDE.md` | Always — this is the global contract bundled into every install | Add to the relevant section: "Subagents available for dispatch" (agents), "Workflow" + "Other commands" (commands), "Project signals" or similar (skills), naming conventions (output styles/rules). |
| 3 | `CLAUDE.md` | Always — it mirrors `CLAUDE.md` for this repo's committed instructions | Same edit as `CLAUDE.md`. These two files must stay synchronized. |
| 4 | `README.md` | Always — public-facing index | Add to the matching table (commands table, agents table, skills table). Keep one-line descriptions. |
| 5 | `docs/spec/<topic>.md` | If the artifact has non-trivial behavior or cross-references | Write or extend the spec. Required for anything dispatched by another artifact or that mutates state. **Amending an existing spec: see "Spec amendment rule" below — never silently overwrite the original.** |
| 6 | Cross-references in other artifacts | If this artifact is invoked by, or invokes, another | Wire both directions. Example: a new skill invoked by `/commit-only` requires editing the command to call it AND the skill to declare itself as called from there. |
| 7 | Bundle inclusion (`atomic/internal/bundlemirror/mirror.go`) | Only if you introduce a **new artifact kind** (not a new file of an existing kind) | Add the inclusion rule. Existing kinds (`agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`) auto-include matching files. |
| 8 | Signals refresh | After adding the file | Run `/refresh-signals` (or let `/commit-only` auto-fire `atomic-signals`) so `.claude/project/deterministic-signals.md` and `signals.md` reflect the new file. |
| 9 | `claude.local.md` (this file) | Only if the artifact changes project-local conventions (e.g. new `@-ref` location, new bundle rule) | Edit the relevant section. |

</mandatory_checklist>

**Verification before commit.** Grep for the new artifact name across the repo. Every place it is *referenced from* should also reference it *back* where appropriate. A skill mentioned only in its own SKILL.md is an invisible skill.


<build_pipeline>

## Embedded bundle: regenerate before every commit


The `atomic` binary embeds the artifact bundle at build time via `go:embed`. Source of truth is the repo root (`agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, `CLAUDE.md`). The mirrored copies under `atomic/internal/embedded/bundle/` and the snapshot at `atomic/internal/embedded/manifest.go` are both **tracked**, not gitignored. CI guards parity with `git diff --exit-code` after `go generate`. Forget to regenerate → CI fails the "Verify bundle is committed" step.


**Hard rule: any commit that touches a source artifact must include the regenerated bundle in the same commit.** Source artifacts = anything under `agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, or `CLAUDE.md` at the repo root. Pure changes to `atomic/`, `docs/`, `.claude/`, `README.md`, or other non-bundle paths do NOT need regen.


**How to regenerate.** From repo root: `make -C atomic bundle`. Outputs `atomic/internal/embedded/bundle/**` + `atomic/internal/embedded/manifest.go`. Stage everything under `atomic/internal/embedded/`, include in the same commit. Do not split the regen into a follow-up commit unless CI already caught the gap.


**Pre-commit hook handles this automatically.** `.githooks/pre-commit` (installed via `make hooks`, which sets `core.hooksPath=.githooks`) has three stages: (1) `make render` when any `templates/` file is staged, re-staging `commands/`; (2) `make bundle` when any source artifact is staged (`agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, `CLAUDE.md`), re-staging the embedded bundle; (3) `atomic followups render` when any followups entry file (other than INDEX.md) is staged, re-staging `INDEX.md` (degrades to WARN if `atomic` binary absent). Render runs before bundle since bundle reads what render wrote. If you commit without the hook installed, the regen is your responsibility — CI fails the "Verify render is committed" and "Verify bundle is committed" steps on drift.


**`atomic hooks` vs git hooks — different systems.** `atomic hooks install` registers a Claude Code session-start hook (injects pending reminders into context). That has nothing to do with the build pipeline. Bundle and render parity are enforced by CI; the git pre-commit hook in `.githooks/` is the local convenience layer.


## Templates: regenerate before every commit


`commands/` is fully generated from `templates/` via `make render`. Sources are `templates/commands/<verb>.md` (per-verb orchestration) and `templates/shared/<name>.md` (reusable partials). The rendered `commands/` files are **tracked**, not gitignored — same pattern as the embedded bundle.

**Hard rule: any commit that touches a template must include the re-rendered `commands/` outputs in the same commit.** Editing `commands/<verb>.md` directly is overwritten on the next render; the contributor skill `.claude/skills/atomic-cli-contrib/SKILL.md` §10 spells out the rule.

**How to regenerate.** From repo root: `make render`. Outputs flow to `commands/<verb>.md`. Stage everything under `commands/`, include in the same commit. The pre-commit hook automates this when templates are staged.

**Orphan rule.** `commands/<verb>.md` without a matching `templates/commands/<verb>.md` causes `make render` to halt with a non-zero exit and an error that names both remediation paths (create the template OR `rm` the orphan output). Adding a new command means creating the template file under `templates/commands/`, never directly in `commands/`.

**Three-stage pipeline.** Render runs before bundle. `make render` writes `commands/`; `make bundle` reads `commands/` to update the embedded bundle; `atomic followups render` regenerates `INDEX.md`. CI runs two drift gates (`make render && git diff --exit-code` then `make bundle && git diff --exit-code`); the pre-commit hook chains all three stages.

</build_pipeline>


## Spec amendment rule (`docs/spec/<topic>.md`)


Specs are the canonical contract for a feature. Editing one in place destroys the original intent and the reason it was written that way — future readers (human or agent) can't tell what shifted or why. Treat specs as **append-mostly**.


**Every spec file must have a `## Change log` section at the bottom.** When amending, append a new dated entry; do not delete prior entries. The log is the audit trail.


- **Adding behavior.** Add a new section to the spec body describing the new behavior. Append a change-log entry: `### YYYY-MM-DD — <short title>` with a one-paragraph **What changed** + **Why** (the trigger: bug, user feedback, axiom shift, downstream artifact requirement).
- **Changing behavior.** Edit the spec body to reflect the new behavior. In the change-log entry, include a **Superseded** line quoting (or summarizing) the prior contract so the old intent isn't lost. Format: `Superseded: <one-line summary of what the spec used to say>`.
- **Removing behavior.** Delete the section from the body. In the change-log entry, include a **Removed** line with what was removed and why. If the removal is reversible (feature parked, not killed), say so.
- **Correcting a factually wrong spec.** Edit the body in place. Append a change-log entry with `**Correction:**` prefix explaining what was wrong, how you know it was wrong (test failure, prod incident, code already diverged), and what the truth is. Corrections are the *only* case where the body changes without an additive section — and even then the log records the delta.
- **Renaming or splitting a spec file.** The old file gets a final change-log entry pointing to the new location: `Moved to: docs/spec/<new>.md` or `Split into: docs/spec/<a>.md + docs/spec/<b>.md`. Keep the old file one commit longer so grep finds both.


**Change-log entry template:**

<example>

```markdown
### 2026-05-17 — <short title>

**What changed:** <one paragraph>

**Why:** <trigger — bug, feedback, axiom, dependency>

**Superseded:** <if applicable, one line on prior contract>
```

</example>


**When in doubt, append.** A spec with a 10-entry change log is healthier than a spec that was rewritten 10 times with no trace. The log is cheap; the lost context is not.


## Cross-artifact wiring rules (mandatory for cohesion)


These rules exist because this repo is meant to be installed into *user repositories* — not just dogfooded here. Cohesion is the product. When a user runs `/commit-only` in their own repo, they expect signals to refresh and docs to stay current without typing five commands.


- **Ship verbs must trigger signals refresh on source-tree changes.** The commit/squash/merge/PR family (`/commit-only`, `/commit-and-pr`, `/commit-and-merge`, `/commit-and-squash`, `/merge-to-main`, `/squash-only`, `/squash-and-merge`, `/pr-only`) must invoke the `atomic-signals` skill (silent mode) whenever the staged diff touches source files. If a ship verb does not do this, the user's project signals go stale — invisible drift.
- **Ship verbs must remind the user to run `/documentation` after significant changes.** "Significant" = new file, removed file, public-API change, dependency change. Surface a one-line prompt at the end of the verb. The skill is interactive and user-driven (axiom 3: destructive ops explicit confirm; doc rewrites are close enough).
- **Symmetry within a command family.** The commit/squash/merge family must agree on shared concerns: message format (all delegate to `atomic-commit` skill), worktree detection (all detect on merge/squash and prompt to delete), signals refresh trigger (above). If you change one verb's behavior on a shared concern, change all of them.
- **Skills that are invoked by commands must declare it.** A skill's description should mention "invoked by /foo, /bar" so the trigger surface is inspectable. Reverse holds: a command that invokes a skill must name it in the command file. No silent dependencies.
- **Agents dispatched by commands must be listed in `CLAUDE.md` → "Subagents available for dispatch"**. The command file should also name the `subagent_type`. Dispatch is a public contract.
- **When in doubt, write the spec first.** `docs/spec/<topic>.md` is the canonical source for any cross-artifact contract. If two artifacts reference the same flow and the spec doesn't exist, write it before adding the second reference.


**Why these rules apply to user repos, not just this one.** Users install these artifacts and rely on the cohesion. A user's `/commit-only` that forgets to refresh signals leaves *their* Claude session with a stale project map. The bug is invisible to us but real to them. Treat every wiring rule as a contract the user has implicitly accepted by installing.


## Signals `@-ref` must stay wired (in this repo: `claude.local.md`)


Only `signals.md` (the compact router) is `@-ref`'d. `deterministic-signals.md` is NOT — it can be thousands of lines on large repos and would blow up context. The inferrer reads it on demand; sessions do not need it. `signals-steering.md` is also NOT `@-ref`'d — the skill reads it at dispatch time and passes it to the inferrer.


**In this repo specifically**, the ref lives in `claude.local.md` (this file) — not in `CLAUDE.md`. Reason: `CLAUDE.md` is the bundle source (gets installed as every user's global `~/.claude/CLAUDE.md`), so project-specific paths there would leak into every install. `claude.local.md` is gitignored, project-local, and still auto-loaded by Claude Code when cwd is this repo. That's the correct home for the project-scoped `@`-ref.


- The `atomic-signals` skill checks for `@.claude/project/signals.md` in `claude.local.md` / `CLAUDE.local.md` first, then `CLAUDE.md`. If present in ANY of them, it skips wiring. The skill's search order is the contract.
- For most repos, the ref ends up in `CLAUDE.md` (one file, no separation). For this repo and any other config-source repos, it lives in `claude.local.md`. Both are valid.
- If you fork the layout (e.g. moving refs into a separate `@`-included file), update the skill's search order in lockstep.


## Documentation surfaces

| Path | Covers | Voice |
|------|--------|-------|
| `README.md` | project overview, install, commands, agents, skills | atomic-prose |
| `docs/guides/install.md` | installation, updating, uninstalling | atomic-prose |
| `docs/guides/contributing.md` | contributing, build pipeline, testing | atomic-prose |
| `docs/guides/evaluations.md` | Docker eval environment, testing setup | atomic-prose |
| `docs/reference/workflow.md` | plan, implement, diagnose, ship lifecycle | atomic-prose |
| `docs/reference/commands.md` | command reference table | atomic-prose |
| `docs/reference/agents.md` | agent reference table | atomic-prose |
| `docs/reference/skills.md` | skills reference table | atomic-prose |
| `docs/reference/signals-workflow.md` | signals scan, infer, wire pipeline | atomic-prose |
| `docs/reference/output-style.md` | atomic output style reference | atomic-prose |
| `CLAUDE.md` | global contract, agent/command/skill registry | llm-reference |


## Naming

- All custom artifacts use the `atomic-` prefix (`atomic-builder`, `atomic-tdd`, `atomic-commit`, etc.) so they're easy to spot among third-party installs.
- Slash commands are imperative verbs (`/commit-only`, `/merge-to-main`, `/worktree-start`).


## Install (for this repo's artifacts)

No install script yet. Manual: copy each top-level directory into `~/.claude/`, restart Claude Code. A future `/install` or Makefile target is on the table.

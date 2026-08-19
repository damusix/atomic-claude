# Commands

Commands are explicit actions you invoke with a slash. They never auto-fire — you reach for them on purpose.


## Planning

| Command | What it does |
|---------|-------------|
| `/atomic-plan` | Produce a spec for the work ahead. Small tasks get an inline checkpoint table; larger work gets a design doc and a derived spec. Nothing is implemented until you approve. |
| `/gather-evidence` | Chase a hunch through primary sources before sinking a planning session into it. Pulls evidence from context7, official docs, source code, ast-grep, and run-it experiments. Returns `SUPPORTED` / `UNSUPPORTED` / `MIXED` / `INCONCLUSIVE` with cited evidence trail. Hearsay (blog posts, forum opinions) cannot produce `SUPPORTED`. |
| `/pressure-test` | Challenge a design decision before committing to it. Asks hard questions, surfaces contradictions, and forces fuzzy maybes into yes or no. Pairs well with `/atomic-plan` as a pre-approval gate. |
| `/challenge-swarm` | Attack a written design, spec, or proposal from 3-6 isolated expert lenses running in parallel. Profiles the artifact first (who can reach it, whose data, what being wrong costs) and seats lenses from a ~30-lens catalog — engineering, data/ML, business, finance, communication, delivery — only where a cited stake exists; a reflexive pick with no stake is benched out loud. Reports a contradiction map: where the lenses conflict, where they independently agree, and what they all assumed without checking. Post-design gate before implementation. |


## Implementation

| Command | What it does |
|---------|-------------|
| `/autopilot` | Run the whole lifecycle hands-off: plan, the implement-then-review loop, and ship, from a task description or an issue number. Fixes every reviewer finding as it goes, dispatches a read-only strategist for root-cause analysis when stuck, and asks just one thing — how to merge. Pass a merge verb (e.g. `/autopilot 29 commit squash merge`) to skip even that. |
| `/subagent-implementation` | Run the implement-then-review loop from an approved spec. Creates an isolated worktree on request (asks if unspecified). Builder writes code, reviewer checks it, passing checkpoints get committed. |
| `/quick-fix` | Run the implement-then-review loop without a spec, worktree, or finalize ceremony. For a fix with a known cause and one obvious approach; escalates to `/subagent-diagnose`, `/atomic-plan`, or `/subagent-implementation` if the assumption breaks. |
| `/subagent-diagnose` | Investigate and fix a failure. `ci` mode starts from a failed CI run; `bug` mode starts from a description. Same loop as implementation. |


## Shipping

All ship commands delegate commit messages to the `atomic-git-discipline` skill.

| Command | What it does |
|---------|-------------|
| `/commit` | Stage and commit, then ask how far to ship — or skip the prompt by passing a token: `push`, `pr`, `merge`, `squash`, or `squash merge`. With no pending changes and commits ahead of base, skips straight to the ship step. |
| `/undo-commit` | Soft-undo the last commit. Refuses merge commits, initial commits, and already-pushed commits. |


## Code review

| Command | What it does |
|---------|-------------|
| `/review-branch` | One-shot code review of the current branch against base. No orchestration loop, no spec required. |
| `/documentation` | Keep project docs in sync with code changes. First run bootstraps: scans for markdown files, you pick which to track as indexed surfaces. Subsequent runs match diffs against tracked surfaces and walk you through each (edit, skip, later, remind). Ship verbs run the same check automatically during commit flow. |


## Project setup

| Command | What it does |
|---------|-------------|
| `/setup-wiki` | Bootstrap a repo for atomic conventions. Audits `.gitignore`, `docs/` layout, and `CLAUDE.md`. Proposes only what is missing — never overwrites. |
| `/refresh-wiki` | Refresh the project wiki; scope is auto-detected. Repo scope: generate or update the `docs/wiki/` pages that teach Claude your repo's shape. Realm scope: run `atomic wiki scan` to classify member repos (scaffolding included), refresh stale or pending artifacts, and synthesize capture-bucket material into `wiki/knowledge/` pages. On first run in a realm with no `<wiki-buckets>` block, prompts to register capture folders; a blank response records the decline so the offer never re-fires. After repo summaries, dispatches `atomic-wiki-inferrer` in bucket-synthesis mode for each bucket with a non-empty diff; code stamps `sources:` frontmatter via `atomic wiki stamp --knowledge`. Prints a per-artifact disposition and commits the wiki automatically when done — its git history is the changelog. Idempotent. |


## Maintenance

| Command | What it does |
|---------|-------------|
| `/git-cleanup` | Scan for stale worktrees, branches, and optionally remote tracking refs. Shows a report and asks before deleting anything. |
| `/watch-ci` | Spawn a background agent to monitor CI for the current branch. Reports back when it finishes. |
| `/remind-me` | Schedule a reminder (e.g. `/remind-me 2h check deploy`). Fires via cron for durations under an hour, via Routines for an hour or more; degrades to a file-only reminder surfaced at session start when neither is available. |
| `/follow-up` | Review pending reminders. Also used to triage stale project follow-ups with `/follow-up review`. |
| `/session-report` | Capture what changed and why during this session. Read by the next ship command for commit message context, then deleted. |
| `/retrospective-learning` | Session retrospective. Mines session history and the current conversation for friction signals, cross-references against installed artifacts, and walks proposed improvements one at a time. Persists a run log so later runs detect drift on past accepts. |


## Utilities

| Command | What it does |
|---------|-------------|
| `/atomic-help` | When you are not sure what to do next. Reads git state, figures out where you are, and recommends one action. `/atomic-help tour` runs a four-stage guided walkthrough of the whole system; bare invocation offers the tour automatically on fresh repos. |
| `/report-issue` | Open a GitHub issue against your current repo. |
| `/report-issue-with-atomic` | Open a GitHub issue against the atomic-claude repo itself. |



## Binary subcommands

`atomic` verbs are not slash commands, so the harness never lists them in the slash menu. Each family self-describes: `atomic <family> --help` prints every verb with its flags, and `atomic <family> <verb> --help` prints one verb's contract. That output is the source of truth, which is why this page points at it rather than copying it.

| Family | What it covers | Reference |
|--------|----------------|-----------|
| `atomic code` | Build and query the symbol graph — where a symbol is defined, what calls it, what breaks if it changes. Also serves the graph over MCP. | [Code intelligence](/reference/code-intel) |
| `atomic wiki` | Scan and maintain the cross-repo wiki, and register capture buckets that feed its knowledge layer. | [Wiki workflow](/reference/realm-wiki) |
| `atomic bus` | Message between concurrent Claude Code sessions over named rooms, and operate a room from outside it. | [Bus](/reference/bus) |
| `atomic repl` | Drive a named Python or Node interpreter session that survives across separate Bash calls. | [REPL](/reference/repl) |
| `atomic serve` | Serve the wiki and code graph as a browsable site. Read-only, localhost by default. | [Serve](/reference/serve) |
| `atomic doctor` · `validate` · `update` · `migrate` | Check the install, validate artifacts, self-update against a verified checksum, apply versioned migrations. | [Install](/guides/install) |

Run `atomic --help` for the full family list.

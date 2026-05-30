# Commands

Commands are explicit actions you invoke with a slash. They never auto-fire — you reach for them on purpose.


## Planning

| Command | What it does |
|---------|-------------|
| `/atomic-plan` | Produce a spec for the work ahead. Small tasks get an inline checkpoint table; larger work gets a design doc and a derived spec. Nothing is implemented until you approve. |
| `/gather-evidence` | Chase a hunch through primary sources before sinking a planning session into it. Pulls evidence from context7, official docs, source code, ast-grep, and run-it experiments. Returns `SUPPORTED` / `UNSUPPORTED` / `MIXED` / `INCONCLUSIVE` with cited evidence trail. Hearsay (blog posts, forum opinions) cannot produce `SUPPORTED`. |
| `/pressure-test` | Challenge a design decision before committing to it. Asks hard questions, surfaces contradictions, and forces fuzzy maybes into yes or no. Pairs well with `/atomic-plan` as a pre-approval gate. |


## Implementation

| Command | What it does |
|---------|-------------|
| `/autopilot` | Run the whole lifecycle hands-off: plan, the implement-then-review loop, and ship, from a task description or an issue number. Fixes every reviewer finding as it goes, dispatches a read-only strategist for root-cause analysis when stuck, and asks just one thing — how to merge. Pass a merge verb (e.g. `/autopilot 29 squash-and-merge`) to skip even that. |
| `/subagent-implementation` | Run the implement-then-review loop from an approved spec. Builder writes code, reviewer checks it, passing checkpoints get committed. |
| `/subagent-diagnose` | Investigate and fix a failure. `ci` mode starts from a failed CI run; `bug` mode starts from a description. Same loop as implementation. |
| `/worktree-start` | Create an isolated worktree at `.worktrees/<name>/` with its own branch. Detects your project setup (npm, cargo, pip, go) and runs a baseline test. |


## Shipping

All ship commands delegate commit messages to the `atomic-commit` skill.

| Command | What it does |
|---------|-------------|
| `/commit-only` | Stage and commit. Nothing else. |
| `/commit-and-push` | Commit, then push. No PR, no merge. |
| `/commit-and-pr` | Commit, push, and open a PR via `gh`. |
| `/commit-and-merge` | Commit, then merge into the base branch. |
| `/commit-and-squash` | Commit, then squash all branch commits into one. |
| `/push-only` | Push existing commits. No new commit, no PR. |
| `/pr-only` | Open a PR for commits that already exist. |
| `/squash-only` | Squash all branch commits into one. No merge. |
| `/squash-and-merge` | Squash into one commit and merge to base. |
| `/merge-to-main` | Merge the current branch into base. No squash. |
| `/undo-commit` | Soft-undo the last commit. Refuses merge commits, initial commits, and already-pushed commits. |


## Code review

| Command | What it does |
|---------|-------------|
| `/review-branch` | One-shot code review of the current branch against base. No orchestration loop, no spec required. |
| `/documentation` | Keep project docs in sync with code changes. First run bootstraps: scans for markdown files, you pick which to track as indexed surfaces. Subsequent runs match diffs against tracked surfaces and walk you through each (edit, skip, later, remind). Ship verbs run the same check automatically during commit flow. |


## Project setup

| Command | What it does |
|---------|-------------|
| `/atomic-setup` | Bootstrap a repo for atomic conventions. Audits `.gitignore`, `docs/` layout, and `CLAUDE.md`. Proposes only what is missing — never overwrites. |
| `/refresh-signals` | Scan the project and generate (or update) the signals files that teach Claude your repo's shape. Idempotent. |


## Maintenance

| Command | What it does |
|---------|-------------|
| `/git-cleanup` | Scan for stale worktrees, branches, and optionally remote tracking refs. Shows a report and asks before deleting anything. |
| `/watch-ci` | Spawn a background agent to monitor CI for the current branch. Reports back when it finishes. |
| `/remind-me` | Schedule a reminder (e.g. `/remind-me 2h check deploy`). Creates a cron-fired follow-up. |
| `/follow-up` | Review pending reminders. Also used to triage stale project follow-ups with `/follow-up review`. |
| `/session-report` | Capture what changed and why during this session. Read by the next ship command for commit message context, then deleted. |
| `/atomic-improve` | Session retrospective. Mines session history and the current conversation for friction signals, cross-references against installed artifacts, and walks proposed improvements one at a time. Persists a run log so later runs detect drift on past accepts. |


## Utilities

| Command | What it does |
|---------|-------------|
| `/atomic-help` | When you are not sure what to do next. Reads git state, figures out where you are, and recommends one action. `/atomic-help tour` runs a four-stage guided walkthrough of the whole system; bare invocation offers the tour automatically on fresh repos. |
| `/atomic-claude-merge` | Reconcile your `~/.claude/CLAUDE.md` with updates from `atomic claude install`. Keeps your instructions, deduplicates conflicts. |
| `/report-issue` | Open a GitHub issue against your current repo. |
| `/report-issue-with-atomic` | Open a GitHub issue against the atomic-claude repo itself. |

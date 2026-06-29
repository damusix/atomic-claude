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
| `/autopilot` | Run the whole lifecycle hands-off: plan, the implement-then-review loop, and ship, from a task description or an issue number. Fixes every reviewer finding as it goes, dispatches a read-only strategist for root-cause analysis when stuck, and asks just one thing — how to merge. Pass a merge verb (e.g. `/autopilot 29 commit squash merge`) to skip even that. |
| `/subagent-implementation` | Run the implement-then-review loop from an approved spec. Creates an isolated worktree on request (asks if unspecified). Builder writes code, reviewer checks it, passing checkpoints get committed. |
| `/subagent-diagnose` | Investigate and fix a failure. `ci` mode starts from a failed CI run; `bug` mode starts from a description. Same loop as implementation. |


## Shipping

All ship commands delegate commit messages to the `atomic-commit` skill.

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
| `/atomic-setup` | Bootstrap a repo for atomic conventions. Audits `.gitignore`, `docs/` layout, and `CLAUDE.md`. Proposes only what is missing — never overwrites. |
| `/refresh-wiki` | Scan the project and generate (or update) the signals files that teach Claude your repo's shape. Idempotent. |
| `/refresh-wiki` | Maintain a cross-repo wiki. Runs `atomic wiki scan` to classify member repos, refreshes stale or pending artifacts, and synthesizes capture-bucket material into `wiki/knowledge/` pages. On first run in a realm with no `<wiki-buckets>` block, prompts to register capture folders; a blank response records the decline so the offer never re-fires. After repo summaries, dispatches `atomic-wiki-inferrer` in bucket-synthesis mode for each bucket with a non-empty diff; code stamps `sources:` frontmatter via `atomic wiki stamp --knowledge`. Prints a per-artifact disposition and offers a commit when done. Run `atomic wiki scan` first to scaffold the wiki directory. |


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
| `/report-issue` | Open a GitHub issue against your current repo. |
| `/report-issue-with-atomic` | Open a GitHub issue against the atomic-claude repo itself. |


## Binary subcommands (`atomic serve`)

`atomic serve` starts a local read-only HTTP server for exploring a wiki realm and code-intel index in the browser. No write operations; localhost only. Run `atomic serve --help` for full usage. See [serve reference](/reference/serve) for the full view and route list.

| Subcommand | What it does |
|---------|-------------|
| `atomic serve [path] [--port N] [--open]` | Start the server. `path` defaults to `cwd`; scope (realm / member / repo) is resolved automatically. `--port 0` picks a free port. `--open` opens the browser. Shuts down cleanly on SIGINT. |


## Binary subcommands (`atomic wiki`)

The `atomic wiki` subcommand manages the cross-repo wiki and capture buckets. Most of these are called by `/refresh-wiki`, but the `bucket` verbs are also useful on their own. Run `atomic wiki --help` for full usage.

| Subcommand | What it does |
|---------|-------------|
| `atomic wiki scan [--root=<path>]` | Scaffold the wiki directory, classify member repos, write the managed `<wiki-scan>` block in `index.md`, and register the wiki globally. Idempotent — re-running regenerates only the managed block. |
| `atomic wiki stale [--root=<path>]` | Read-only freshness verdict. Reports `DRIFT`/`STALE` lines for repos and concerns, plus `STALE bucket <name>` for capture folders with a non-empty diff. Exits `0` fresh, `1` stale, `2` error. |
| `atomic wiki linkify --root=<path>` | Render inline path citations across summaries, concerns, knowledge pages, and the index into file-relative markdown links. Deterministic, idempotent, no model. |
| `atomic wiki bucket add <name>` | Register a capture folder at the realm root. Creates `<name>/index.md` (purpose stub), `wiki/.buckets/<name>/` (manifest dir), and splices a `<bucket>` entry into the `<wiki-buckets>` block in `wiki/index.md`. On first add in a realm, also writes a `## Capture surfaces` section to the realm `CLAUDE.md`. Refuses if `<name>` is `wiki` or the bucket is already registered. |
| `atomic wiki bucket list` | Print one line per registered bucket: name, path, baseline file count, and `pending` or `fresh` status. Exits `0` even when no buckets are registered. |
| `atomic wiki bucket diff <name>` | Read-only diff of the capture folder against its baseline. Prints `new <path>`, `changed <path>`, or `removed <path>` per changed file. Exits `0` when the diff is empty, `1` when any line is emitted. |
| `atomic wiki bucket promote <name>` | Advance the baseline after successful synthesis: recomputes the SHA-256 manifest, rotates `baseline→previous`, sets new manifest as `baseline`. After promote, `diff` exits `0`. |


## Binary subcommands (`atomic code`)

The `atomic code` subcommand provides a code-intelligence index and query engine. When a project has been indexed, `atomic-investigator`, `atomic-reviewer`, and `atomic-wiki-inferrer` query the symbol graph automatically; every consumer falls back to `sg`/`grep` when the index is absent. `atomic doctor` check 11 reports index health. Run `atomic code --help` for full usage.

| Subcommand | What it does |
|---------|-------------|
| `atomic code index` | Index all source files in the project root. Creates `.claude/.atomic-index/atomic.db` and adds the path to `.gitignore`. |
| `atomic code sync` | Incrementally re-index only files that changed since the last run. |
| `atomic code status [--json]` | Show index status: initialized state, file/node/edge counts, last-indexed timestamp, pending changes. `--json` emits the appendix-N shape. |
| `atomic code search <query> [--json] [--limit N]` | Full-text + fuzzy search over indexed nodes by name, kind, or language. |
| `atomic code callers <symbol> [--depth N] [--json]` | Find all callers of a symbol up to N hops. |
| `atomic code callees <symbol> [--depth N] [--json]` | Find all callees of a symbol up to N hops. |
| `atomic code impact <symbol> [--depth N] [--json]` | Find the impact radius of a symbol — all nodes reachable through call/import edges. |
| `atomic code node <symbol> [--file path] [--line N] [--json]` | Show detailed node info for a symbol. `--file` and `--line` disambiguate overloads. |
| `atomic code files [pattern] [--json]` | List all indexed files. Optional pattern filters by path substring. |
| `atomic code affected [--depth N] [--test-glob pattern] [--stdin] [--json] [paths...]` | BFS over the dependency graph from changed files; returns test files transitively affected. |
| `atomic code explore <query> [--json]` | Gather relevant context for a natural-language query; returns markdown or structured JSON. |
| `atomic code mcp` | Start an MCP server exposing the code graph as tools (`atomic_code_explore`, `atomic_code_search`, `atomic_code_node`, `atomic_code_callers`, `atomic_code_callees`, `atomic_code_impact`, `atomic_code_status`, `atomic_code_files`). Subagents do not need MCP — they shell out directly. MCP is opt-in for the interactive session; register manually in `.mcp.json`. See [code-intel MCP setup](/guides/code-intel-mcp). |

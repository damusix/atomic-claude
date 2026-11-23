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
| `/autopilot` | Plan, implement, and ship a task or issue. Decide without routine prompts, consult a read-only strategist when stuck, and default to a PR. Merge only on request. |
| `/implement` | Implement a task or spec. A straightforward known-cause fix needs no spec. Work directly or delegate, review each checkpoint independently, and commit on PASS. |


## Shipping

Ask to commit, push, open a PR, squash, or merge. `atomic-git-discipline` supplies the safeguards and message format.


## Code review

| Command | What it does |
|---------|-------------|
| `/review-branch` | One-shot code review of the current branch against base. No orchestration loop, no spec required. Reviews a diff; for code nobody is changing, see `/deslop` under Maintenance. |
| `/documentation` | Index project documentation or walk affected pages with the user. `/implement` uses the scan and task diff to maintain affected pages before final review. |


## Project setup

| Command | What it does |
|---------|-------------|
| `/setup-wiki` | Check the project steering file, wiki, and state directories. Propose only missing setup and preserve existing content. |
| `/refresh-wiki` | Manually update a repository's `docs/wiki/` pages, source links, and path-scoped steering. Use `atomic-wiki` for realm capture buckets. |


## Maintenance

| Command | What it does |
|---------|-------------|
| `/deslop` | Audit the current change, a PR, or an explicitly selected path or repo. Apply safe, behavior-preserving fixes only when asked. |
| `/cleanup` | Remove selected temporary files, artifacts, or worktrees. Defaults to the session and current worktree; broader local or remote scans are opt-in. |
| `/follow-up` | Record an item in the resolved tracker or ledger. Use `/follow-up review` to triage stale ledger entries. |
| `/retro [focus]` | Review this session and propose improvements for approval. Past sessions and checks of earlier decisions are opt-in. See [Retro](/reference/retro). |


## Utilities

| Command | What it does |
|---------|-------------|
| `/atomic-help` | Recommends one next action or asks a clarifying question. Use `/atomic-help tour` for a walkthrough. |
| `/report-issue` | File a bug, enhancement, or question in the resolved issue tracker or local ledger. |
| `/report-issue-with-atomic` | Open a GitHub issue against the atomic-claude repo itself. |



## Binary subcommands

`atomic` verbs are not slash commands, so the harness never lists them in the slash menu. Each family self-describes: `atomic <family> --help` prints every verb with its flags, and `atomic <family> <verb> --help` prints one verb's contract. That output is the source of truth, which is why this page points at it rather than copying it.

| Family | What it covers | Reference |
|--------|----------------|-----------|
| `atomic install` · `atomic harness` | Enroll a harness instance and converge it. `atomic harness list\|status\|enroll\|adopt\|repair\|diff\|recover\|uninstall` covers the target lifecycle; `atomic harness rules status\|sync` covers per-target rule delivery. Enrollment is explicit — discovery never enrolls. | [Install](/guides/install) |
| `atomic config` | Read and write `~/.atomic/config.toml`. `atomic config agents --harness claude\|omp\|codex\|all` sets per-agent model and effort for each harness, then converges every enrolled harness it edited. | [Agents](/reference/agents#model-and-effort) |
| `atomic state` | Manage this repository's state root, `.atomic` by default. `atomic state adopt [--dir\|--clear]` selects a root on the harness-neutral resolution ladder; `atomic state migrate` moves Atomic's state out of legacy `.claude` (into `.atomic` by default) and stages the tracked renames. | [Concepts](/reference/concepts#state-root-resolution-order) |
| `atomic workflow` | `atomic workflow resolve [--json]` prints the resolved source-control, issue-tracker, and worktree preferences for this directory; `atomic workflow set <key> <value> [--level profile\|realm\|repo]` writes one preference line. | [Concepts](/reference/concepts#workflow-preferences) |
| `atomic code` | Build and query the symbol graph — where a symbol is defined, what calls it, what breaks if it changes. Also serves the graph over MCP. | [Code intelligence](/reference/code-intel) |
| `atomic wiki` | Scan and maintain the cross-repo wiki, and register capture buckets whose docs it indexes. | [Wiki workflow](/reference/realm-wiki) |
| `atomic realm` | Publish, receive, review, and link selected realm paths without sharing member repositories. Verbs: `share`, `sync`, `join`, `link`, `status`, `unlink`. | [CLI contract](/reference/realm-wiki#realm-sharing-cli) · [Sharing guide](/guides/realm-sharing) |
| `atomic bus` | Message between concurrent agent sessions over named rooms, and operate a room from outside it. | [Bus](/reference/bus) |
| `atomic repl` | Drive a named Python or Node interpreter session that survives across separate Bash calls. | [REPL](/reference/repl) |
| `atomic retro` | `atomic retro extract` exports numbered session history for optional cross-session review. | [Retro](/reference/retro) |
| `atomic docs` | `atomic docs index` rebuilds the `<bucket-docs>` listing in a directory's `index.md` from its files' frontmatter; `--check` fails CI when it lags. | [Docs index](/reference/docs-index) |
| `atomic serve` | Serve the wiki and code graph as a browsable site. Read-only, localhost by default. | [Serve](/reference/serve) |
| `atomic scratchpad` | Create, look up, list, and archive slug-keyed work bundles — the shared shape behind every implement-loop scratchpad. | [Conventions](/reference/conventions) |
| `atomic doctor` · `validate` · `update` · `migrate` | Check the install and every enrolled target (26 categories), lint spec / config / bundle / artifact citations / canonical-corpus projections, self-update against a verified checksum and reconverge enrolled targets, apply versioned migrations. | [Install](/guides/install) |

Run `atomic --help` for the full family list.

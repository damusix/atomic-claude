<script setup>
import SessionPlayer from '../../.vitepress/theme/SessionPlayer.vue'
import { FLOW } from '../../.vitepress/theme/flow-script'
</script>

# Concepts


Key ideas behind Atomic Claude, explained plainly. This page covers the *why* — for detailed usage, follow the links to the reference pages.

You may know the broader idea as *loop engineering*: designing the system that finds work, hands it to the agent, checks the result, and records state, rather than prompting by hand. The concepts below are the parts of that loop.


## How it flows


Here is what a real session looks like — adding a Stripe webhook endpoint to a NestJS app. Play it, or jump between the steps:

<SessionPlayer :session="FLOW" />

Every concept below plays a role in that flow. Signals gave Claude the project map. Evidence-gathering settled an assumption before planning around it. The spec kept implementation on track. TDD fired during each implementer checkpoint. Session reports preserved the why. Ship commands handled signals, docs, and the commit message. Follow-ups caught what was deferred.


## The atomic binary


`atomic` is a standalone Go binary, the deterministic layer beneath everything else. The model is good at judgment and bad at reproducibly scanning a tree, computing a checksum, or managing a scheduled job — so those are code's job. The binary does them and hands Claude facts it can trust.

- **Deterministic signals** — scans the filesystem (tree, manifests, languages, lockfiles) into a reproducible facts file that grounds everything Claude infers about your repo.
- **Code intelligence** — builds and queries the symbol graph (below).
- **Self-update and health** — `atomic update` swaps the binary against a verified checksum; `atomic doctor` and `atomic validate` check the install.
- **Config and state** — `~/.claude/.atomic/config.toml`, follow-ups, install/uninstall, and the user profile.

Everything below is either produced by this binary or grounded by what it produces. Run `atomic --help` for the full surface.


## Code intelligence


`atomic code index` parses every source file with tree-sitter into a symbol graph at `.claude/.atomic-index/atomic.db` — what calls what, what imports what, and where every symbol is defined, across 31 languages, with no compiler or language server. Claude queries that graph instead of grepping for structure, and the implementation agents check it for blast radius before they edit. It is optional: every consumer falls back to `sg` and `grep` when no index is present.

- `atomic code explore "<question>"` — reach for this first: a bundled digest of the relevant symbols, files, and how they relate, in one shot.
- `atomic code callers <symbol>` / `callees <symbol>` — what calls it, what it calls.
- `atomic code impact <symbol>` — the blast radius of changing it.
- `atomic code sync` — keep the index current (ship verbs and `/refresh-signals` do this when it is warm).
- `atomic code mcp` — expose the graph to your interactive session as MCP tools.

See the [code-intel reference](/reference/code-intel) for the full verb list and lifecycle, and the [MCP guide](/guides/code-intel-mcp).


## Signals


Signals are context engineering — a wiki for one repo. The project's context is curated once and kept as an artifact, instead of re-derived from scratch every session.

You could hand-maintain a `CLAUDE.md`, but the odds you keep it current are slim: you add a service, rename a package, swap Jest for Vitest, and forget. Signals are baked into the workflow instead. `/refresh-signals` scans the repo, the ship verbs refresh it on every commit, and the inference is grounded by the code-intel graph and the actual file diff, not guesswork. You front-load compressed context once — and again only when the repo changes — instead of paying for it on every request.

Signals fix:

- Hallucinated build and test commands — invented `npm run` scripts, fake `make` targets.
- Wrong guesses about your framework, stack, and architecture.
- Re-explaining your project layout at the start of every session.
- A hand-written `CLAUDE.md` that silently drifts out of date.

A scan writes two files: **deterministic signals** (filesystem facts — tree, manifests, languages — reproducible) and **inferred signals** (the meaning on top — framework, commands, a domain map). Claude loads the inferred file before it reads your code; the deterministic file stays out of context and is read on demand. See [signals workflow](/reference/signals-workflow).


## Wikis


Signals map one repo; a wiki maps how a *realm* of repos relate — the shared libraries, the contracts one repo owns and another consumes, the patterns duplicated across a folder of services. A wiki is a portable, git-initialized knowledge base at `<root>/wiki/` for one such realm (most people keep three to five, one per realm). `/refresh-wiki` scans the root, points at member repos that already have signals, summarizes the ones that should not carry signals — open-source dependencies — without writing into them, and synthesizes the realm's cross-cutting concerns with cited evidence. Registered wikis live in a `<wikis>` block in your user-level `~/.claude/CLAUDE.md`, so every Claude session, in any repo, knows they exist.

- `/refresh-wiki` — scan the realm; refresh repo summaries and shared concerns.
- `atomic wiki bucket add <dir>` — register loose material (research, raw dumps, ticket exports) as a capture bucket; refresh synthesizes it into `wiki/knowledge/` pages.
- `atomic wiki stale` — report membership drift and stale content.
- `atomic serve` — browse the realm as a typed, navigable graph in the browser.

See the [knowledge base guide](/guides/knowledge-base) and [wiki workflow](/reference/wiki-workflow).


## Output style


Claude is verbose by default: "Sure! I'd be happy to help," hedging with "perhaps," narrating what it is about to do and then what it just did. Fine for one question; across a working session the filler costs tokens, slows you down, and buries the answer.

Atomic's output style strips the scaffolding — same information, easier to follow. A bug explanation that was a paragraph becomes two sentences and a code block; multi-part answers use tables, trees, and ASCII flows so structure carries the meaning. Security warnings and irreversible-action confirmations always revert to full prose.

This is the most optional part of Atomic Claude — everything else works without it. See [output style](/reference/output-style) for details and examples.


## Planning


Turning an idea into a spec the build loop can follow.


### Plan


`/atomic-plan` gauges how big a task is. Trivial work gets an inline spec and goes straight to building. Non-trivial work gets a design doc and a spec, written through a subagent loop, with your approval before any code. The two gates below feed it.


### Gather evidence and pressure-test


Two optional gates before a plan hardens. `/gather-evidence` chases a hunch — "does this library support that," "is approach A faster than B" — against primary sources and returns a verdict, so you do not design a whole session around an assumption that falls apart on contact. `/pressure-test` does the opposite job: it attacks a design's assumptions to find the weak ones before they reach a spec.


### Specs and design docs


Planning produces two kinds of durable document. **Design docs** (`docs/design/`) are the thinking space — concepts, rules, approaches weighed, the *why*; trivial tasks skip them. **Specs** (`docs/spec/`) are the contract the subagents build from: checkpoints, success criteria, the plan.

A spec has two parts. The body states what is true *now* — a subagent reads it as ground truth, so it must always reflect the current decision, never superseded scope. The change log records how the contract got there: when a decision changes the spec, you rewrite the affected body and add a dated entry saying what changed, why, and what it used to say. History is preserved without ever leaving the body stale.


## Implementation


`/subagent-implementation` runs the implement-then-review loop against a spec, committing each green checkpoint. The pieces below make that loop safe and resumable.


### Worktrees


A worktree is a second checkout of the same repo, on its own branch, in a different directory — git supports it natively. The implement loop and `/autopilot` create one at `.worktrees/<feature>/` automatically, run a baseline test, and build there, so your main checkout stays clean with no stashing or branch juggling. On merge or squash, `/commit` notices the branch came from a worktree and offers to clean it up.


### Scratchpad


`.claude/.scratchpad/` is Claude's working memory during a task — gitignored, not for human eyes. It holds the brief (what to build now), a state log (what happened each iteration), and a follow-ups ledger. This is how the loop survives context compaction: the agent forgets between runs, the file does not. When the task is done, the scratchpad is deleted.


### Subagents


Claude Code can spawn agents that run in a fresh context with their own prompts; Atomic defines a roster, each scoped to one job. The core split is the [evaluator-optimizer pattern](https://www.anthropic.com/engineering/building-effective-agents): one agent writes, a separate one critiques. A single agent grading its own work is too generous; a reviewer with fresh eyes that re-runs the tests catches what the author talked itself into. Scope is constrained too — the implementer's surgical mode refuses to touch more than two files, and the strategist is read-only. See [agents](/reference/agents) for the roster.


### Ship verbs


Atomic ships through one `/commit` verb: run it bare and it stages, commits, then asks how far to go; pass a token to skip the prompt — `/commit push`, `/commit pr`, `/commit merge`, `/commit squash`, `/commit squash merge`. One verb, escalated by intent. The reason it exists instead of a plain "commit and push" is what happens around the git operation:

- A run that produces a commit refreshes signals and checks for stale docs automatically.
- A run that does not produce a commit checks staleness and asks before proceeding.
- The merge tokens run verification and tests on the merged result first.
- Every run writes the commit message from the diff via the `atomic-commit` skill.

See [commands](/reference/commands) for the full token set.


### Session reports


Long or scattered work loses its *why*. You come back tomorrow and Claude has no memory of yesterday's choices; or you are running three terminals on one branch toward a big PR (not recommended, but it happens) and no single session has the whole picture. `/session-report` writes a timestamped, branch-scoped snapshot of what changed and why. The next commit on that branch folds those reports into the message, then deletes them. They are not for you to read — they are for Claude to read when it writes your commit.


## Follow-ups and reminders


Two ways to park something for later. **Reminders** are time-based: `/remind-me check the deploy in 30 minutes` schedules a job that surfaces in your session when it fires, or at the start of your next one. **Follow-ups** are decision-based: non-blocking things the reviewer flags during implementation collect in a ledger, and at the end you fix, defer, or drop each. Deferred ones persist until you close them with `/follow-up review`.


## Skills vs commands


Both shape Claude's behavior; they trigger differently. **Skills** fire automatically on matching language — say "let's implement the auth module" and `atomic-tdd` activates without you asking. They are the *how*: always-on discipline. **Commands** fire only when you type the slash — `/atomic-plan`, `/subagent-implementation`, `/commit`. They are the *when*: workflows you start on purpose. A command can invoke a skill (every ship verb uses `atomic-commit`); a skill never invokes a command. See [skills](/reference/skills) and [commands](/reference/commands).


## Documentation


Code changes break docs silently — an endpoint renamed, a config field gone, a diagram showing a component that no longer exists. Nothing fails; months later someone acts on something wrong. `/documentation` treats docs the way signals treats project context: scan, track, and prompt on drift.

- **Bootstrap** — the first run scans for markdown and lets you pick which surfaces to track into a `## Documentation surfaces` table in your CLAUDE.md.
- **Authoring** — run `/documentation` to compare recent changes against tracked surfaces and walk the stale ones with you, one at a time.
- **Maintenance** — ship verbs run the same check on the staged diff automatically, silent unless something is stale.


## Your work profile


Claude reads `~/.claude/.atomic/profile.md` at the start of every session — personal facts that hold across repos: name, role, employer, active projects, interests, and people you work with. Install seeds the `## Environment` section from your machine (git identity, OS, tooling versions); the rest fills in as facts surface in conversation. Volatility tags (`<stable>`, `<volatile>`, `<deterministic>`) tell Claude how eagerly to flag a contradiction, and `/atomic-improve` resolves drift with your sign-off.

The routing rule: anything still true in a different repo belongs in the profile; repo-specific conventions go to that project's signals instead.

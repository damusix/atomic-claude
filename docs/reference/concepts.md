# Concepts

Key ideas behind Atomic Claude, explained plainly. This page covers the *why* — for detailed usage, follow the links to reference pages.

You may know the broader idea as *loop engineering*: designing the system that finds work, hands it to the agent, checks the result, and records state, rather than prompting by hand. The concepts below are the parts of that loop.


## How it flows

Here is what a real session looks like — adding a new API endpoint to a project:

```
you:    /refresh-signals
        → Claude scans the repo, learns it's a NestJS app with
          Prisma, Jest, and a docs/ folder

you:    /gather-evidence does the Stripe Node SDK expose
        a built-in webhook signature verifier?
        → Claude pulls context7 docs for stripe-node, finds
          `Stripe.webhooks.constructEvent`, confirms tolerance
          window and replay-protection behavior. VERDICT:
          SUPPORTED. Now you know the hunch holds — no need
          to design HMAC-by-hand.

you:    /atomic-plan I need a POST /api/webhooks endpoint that
        validates Stripe signatures and queues events
        → Claude writes a spec: controller, service, DTO,
          queue integration, signature validation, retry
          logic. You review, decide retry is out of scope
          for now, and approve without it.

you:    /subagent-implementation
        → Implementer agent implements each checkpoint. atomic-tdd
          fires on each one — failing test first, then code.
          Reviewer agent re-runs tests independently and checks
          against the spec. Non-blocking findings accumulate
          in a ledger.
        → At the end, you review deferred findings: fix one,
          defer two as follow-ups, drop the rest.

you:    /session-report
        → Captures why you chose HMAC over the Stripe SDK,
          why you skipped retry logic (follow-up), and what
          the reviewer flagged.

you:    /commit pr
        → Signals refresh (the new controller is now in the
          project map). Doc-impact check runs (new endpoint
          needs API docs — you update them). Commit message
          synthesized from the diff + session report. PR opens.

you:    /remind-me check if the webhook PR got reviewed by thursday
        → Schedules a reminder. Thursday morning, it surfaces
          at the start of your next session.

        — two days later —

        → Reminder fires: "check if the webhook PR got reviewed"

you:    /follow-up review
        → The retry logic follow-up surfaces. You promote it
          to a GitHub issue.
```

Every concept below plays a role in that flow. Signals gave Claude the project map. Evidence-gathering settled an assumption before planning around it. The spec kept implementation on track. TDD fired during each implementer checkpoint. Session reports preserved the why. Ship commands handled signals, docs, and the commit message. Follow-ups caught what was deferred.


## Signals

Signals are context engineering: the curated knowledge Claude works from, kept as an artifact instead of re-derived each session. Claude does not know your project. Every session starts fresh — no idea what framework you use, what your build command is, or how your code is organized. Without help, it guesses. Those guesses show up as hallucinated `npm run` scripts, invented `make` targets, and wrong assumptions about your architecture.

You might maintain a `CLAUDE.md` by hand, and that helps — but it inevitably drifts. You add a new service, rename a package, switch from Jest to Vitest, and forget to update the instructions. Now Claude is working from a stale map.

Signals fix this. Running `/refresh-signals` scans your repo and produces two markdown files:

- **Deterministic signals** — facts from the filesystem: directory tree, manifests, languages, lockfile presence. Reproducible and idempotent — running the scan twice produces the same output.
- **Inferred signals** — meaning derived from those facts: framework, build/test/lint commands, domain boundaries, conventions.

Claude loads the inferred signals at the start of every session, before it reads your code, so it knows what it is looking at before you ask your first question. The deterministic file is the substrate underneath: on a large repo it can run thousands of lines, so it stays out of session context and the inference step reads it on demand.

Signals auto-refresh when you commit through the ship commands, so they stay current without you thinking about it. See [signals workflow](/reference/signals-workflow) for the full mechanism.


## Code intelligence

Signals describe the shape of your project from the outside: directories, manifests, framework names. The code-intelligence index goes one level deeper. `atomic code index` parses every source file with tree-sitter and builds a symbol graph stored at `.claude/.atomic-index/atomic.db`. The graph records what calls what, what imports what, and where every symbol is defined, across 31 languages and with no compiler or language server required.

The highest-value query is `atomic code explore "<natural-language question>"`. It returns a bundled context digest, the relevant symbols and files and the relationships between them, in one shot. Reach for it first when scoping unfamiliar code. Once it points you at a symbol, the targeted verbs drill into that symbol: `callers` lists everything that calls it, `callees` lists what it calls, and `impact` reports the blast radius of changing it.

Agents that read code use the graph on their own. The investigator leads with `explore` to scope a surface, the reviewer checks `impact` before flagging a risky change, and the signals inferrer corroborates domain boundaries with real call edges instead of filename guesses. Every one of them falls back to `sg` and `grep` when no index is present, so indexing is optional and never required. Build the index once with `atomic code index`, keep it current with `atomic code sync` (ship commands and `/refresh-signals` do this for you when the index is warm), and expose it to your interactive session as MCP tools with `atomic code mcp`. See the [code-intel reference](/reference/code-intel) for the full verb list and lifecycle, and the [MCP guide](/guides/code-intel-mcp) for registration.


## Wikis

Signals describe one repo's internals. Nothing describes how a set of repos *relate* — the shared libraries, the contracts one repo owns and another consumes, the patterns duplicated across a folder of services. If you work across several repos in one realm (your client projects, a set of open-source libraries, everything personal), that cross-cutting knowledge lives only in your head, and a fresh Claude session rediscovers it one repo at a time with no map of the whole.

A wiki fixes this. It is a portable, git-initialized knowledge base for one realm of repos, living at `<root>/wiki/`, where `<root>` is a folder that contains repositories. Most people keep three to five, one per realm. The mental model is one level up from signals: **signals map one repo; a wiki maps how a realm's repos relate.** The command parallel is exact — `/refresh-signals` is to one repo what `/refresh-wiki` is to a realm.

Running `/refresh-wiki` scans the root for member repos (any directory with a `.git`) and sorts each into one of three states:

- **`indexed`** — the repo already has signals. The wiki points at them and cites the path; it never copies signals in, so there is one source of truth.
- **`summarized`** — the repo has no signals. The wiki writes its own summary of the repo, reading it without ever writing into it. This is the right state for repos that should never carry committed signals, like open-source dependencies.
- **`pending`** — found in a fresh scan, not yet summarized. The refresh pass resolves it, either by offering to add signals to the repo or by summarizing it into the wiki.

A repo with no signals is not a defect to fix — it is a fork between repo-owned and wiki-owned knowledge. On top of the per-repo picture, the refresh pass synthesizes the cross-cutting concerns across the realm and writes them up with cited evidence.

The wiki does not guarantee freshness the way signals do — it sits outside any single repo's lifecycle, so it cannot ride a commit. Instead a cheap nudge keeps it honest: the session-start hook flags a wiki that has gone untouched too long or has changes pending, and shipping from any member repo marks its wiki dirty. Acting on the nudge is the only heavy step, and it clears both signals. See [wiki workflow](/reference/wiki-workflow) for the full mechanism.


## Session reports

Long-running work across multiple sessions loses context. You come back tomorrow, Claude has no memory of why you made the choices you made yesterday.

Or: you are working in three terminals on the same branch, about to make a large PR. Not recommended, but it happens — we all work in different ways. Each terminal has its own session context. When it is time to commit, nobody — including you — has the full picture of what happened and why across all those sessions.

Session reports are opt-in snapshots that capture what changed and why. Run `/session-report` before you switch context, and it writes a timestamped markdown file scoped to the current branch.

The next time you commit on that branch, the ship command reads those reports and folds their context into the commit message. After a successful commit, the reports are deleted — they served their purpose.

Session reports are not for you to read. They are for Claude to read when it writes your commit message, so the "why" does not get lost between sessions or terminals.


## Follow-ups and reminders

Two mechanisms for things you want to deal with later.

**Reminders** are time-based. `/remind-me check the deploy in 30 minutes` creates a scheduled job that surfaces the reminder at the specified time. If you are in a Claude session when it fires, it appears in your conversation. If not, it gets picked up at the start of your next session.

**Follow-ups** are decision-based. During implementation, the reviewer sometimes flags things that are not blocking — risks worth tracking, nits to address later, open questions. These get collected in a ledger. At the end of the implementation, you decide what to do with each one: fix it now, defer it as a project follow-up, or drop it.

Deferred follow-ups persist until you explicitly close them via `/follow-up review`. The difference: reminders are alarms with a time. Follow-ups are decisions you parked.


## Documentation maintenance

Code changes break docs. An endpoint gets renamed, a config field disappears, an architecture diagram shows a component that no longer exists. The drift is silent — nothing fails, nothing warns. Months later someone reads the docs and acts on information that is wrong.

`/documentation` treats docs the same way signals treats project context: scan, track, and prompt when something drifts.

**Bootstrap.** The first time you run `/documentation`, it scans your repo for markdown files and presents what it found. You pick which surfaces to track — architecture docs, API references, ERDs, guides. Those go into a `## Documentation surfaces` table in your CLAUDE.md. This is the index that everything else reads from.

**Authoring mode.** Run `/documentation` explicitly when you want to check what needs updating. It compares your recent changes against tracked surfaces, classifies each as stale or incomplete, and walks you through them one at a time: edit the doc, skip it, defer it as a follow-up, or set a reminder. For domains with no docs at all, it suggests creating a new surface.

**Maintenance mode.** Ship commands that produce a commit run the same check automatically — but scoped to just the staged diff, and only for stale or incomplete surfaces (never suggesting new pages during commit flow). If nothing is stale, it passes silently. If something is, you get the same edit/skip/later/remind prompt before the commit lands.

The `atomic-documentation` skill is the engine that classifies surfaces. The `/documentation` command is the interface you interact with. The `atomic docs scan` and `atomic docs stale` binary subcommands handle the deterministic scanning underneath.


## Why clearer output?

Claude is verbose by default. It opens with "Sure! I'd be happy to help." It hedges with "perhaps" and "it seems like." It explains what it is about to do, does it, then summarizes what it just did. For a single question, this is fine. Across a working session with dozens of exchanges, the filler adds up — it costs tokens, slows you down, and buries the answer.

Atomic's output style strips this scaffolding. Clarity is the goal: the same information, easier to follow. A bug explanation that took a paragraph becomes two sentences and a code block. Multi-part answers use tables, trees, and ASCII flows so structure carries the meaning. Security warnings and irreversible-action confirmations always revert to full prose.

The output style is the most optional part of Atomic Claude. Everything else — skills, commands, agents, signals — works without it.

See [output style](/reference/output-style) for details and examples.


## Worktrees

A worktree is a second checkout of the same repo in a different directory, on its own branch. Git supports this natively. Atomic Claude uses worktrees to isolate feature work from your main checkout.

Worktree creation happens automatically when `/subagent-implementation` or `/autopilot` runs via the worktree-setup partial — it creates `.worktrees/my-feature/` with a new branch, detects your project setup, and runs a baseline test before work begins.

Why this matters: you keep your main branch clean in the original checkout while building a feature in the worktree. No stashing, no juggling branches. When the feature is done and you merge or squash, `/commit` detects that the branch came from a worktree and offers to clean it up.


## Subagents

Claude Code can spawn specialized agents that run in a fresh context with their own system prompts. Atomic Claude defines a roster of these, each scoped to one job.

The split is the [evaluator-optimizer pattern](https://www.anthropic.com/engineering/building-effective-agents) from Anthropic's *Building Effective Agents*: one model generates, a separate one critiques. It exists because a single agent doing everything produces worse results — the author is too generous grading its own work. An implementer writes code without second-guessing itself. A reviewer catches what the implementer missed because it has fresh eyes and re-runs the evidence. An investigator maps the codebase cheaply on Haiku before the more expensive Sonnet agents start writing.

The separation also lets you constrain scope. The implementer's surgical mode hard-refuses changes beyond two files, so it cannot accidentally rewrite half your codebase. A strategist that is read-only cannot introduce bugs while reasoning about architecture.

See [agents](/reference/agents) for the full roster and their scopes.


## Specs and design docs

Atomic Claude writes two kinds of durable documents during planning:

**Design docs** (`docs/design/`) are the thinking space. Concepts, business rules, approaches considered, tradeoffs weighed. They capture *why* you chose this direction. Optional — trivial tasks skip this.

**Specs** (`docs/spec/`) are the contract. Checkpoints, success criteria, the implementation plan the subagents will follow. This is what `/subagent-implementation` reads to know what to build.

A spec has two parts with two jobs. The body states what is true now, the contract a subagent reads as ground truth and builds from. The change log records how the contract got there. When a decision changes the spec, you rewrite the affected body to the new truth and add a dated change log entry that records what changed, why, and what the spec used to say. The body always reflects the current decision, never superseded scope, because a subagent that reads stale body text builds the wrong thing. The change log carries the history so the original intent is never lost, but it never excuses leaving the body out of date.


## The scratchpad

`.claude/.scratchpad/` is Claude's working memory during a task. It is gitignored and not meant for human consumption.

During `/subagent-implementation`, the scratchpad holds a brief (what to build now), a state log (what happened each iteration), and a follow-ups ledger (non-blocking findings that accumulate). The scratchpad is how the implement-then-review loop survives context compaction. The agent forgets between runs; the file does not — Claude does not need to remember what happened, it reads the file. When the task is done, the scratchpad is deleted.


## Ship verbs

Atomic Claude ships code through a single `/commit` verb. Run it bare and it stages, commits, then asks how far to go; pass an escalation token to skip the prompt — `/commit push`, `/commit pr`, `/commit merge`, `/commit squash`, or `/commit squash merge`. One verb, escalated by intent. See [commands](/reference/commands) for the full set of escalation tokens.

The reason this exists — instead of just asking Claude to "commit and push" — is what happens *around* the git operation:

- A run that produces a commit refreshes signals and checks for stale documentation automatically
- A run that does not produce a commit checks for staleness and asks you before proceeding
- The merge tokens run verification and tests on the merged result before completing
- Every run generates the commit message from the diff via the `atomic-commit` skill

You could do all of this manually. `/commit` makes it the default so nothing slips through when you are moving fast.


## The atomic binary

`atomic` is a standalone Go binary that handles things Claude cannot do on its own — or that should not depend on the model:

- **Signals scanning** — reads the filesystem and produces the deterministic signals file. Intentionally done by code so the output is reproducible and fast.
- **Self-update** — fetches the latest release, verifies its checksum, replaces the binary.
- **Health checks** — `atomic doctor` runs a suite of integrity checks against your install. `atomic validate` lints specs and cross-references.
- **Config/state** — manages `~/.claude/.atomic/config.toml`, follow-ups, install/uninstall.

The binary exists because some operations need to be deterministic, fast, and runnable outside a Claude session. Scanning a repo's tree structure, computing SHA checksums, and managing scheduled jobs are all better done by code than by asking a model.


## User profile

Claude reads `~/.claude/.atomic/profile.md` at the start of every session, alongside `config.resolved.md`. The file records personal facts about you: name, role, employer, active projects, interests, and people you work with. These are organized into six fixed sections.

Install creates the file and fills in the `## Environment` section from your local env (git name and email, OS, architecture, CPU count). The other sections start empty. Claude appends new facts to the matching section as they come up naturally in conversation: you mention a coworker, Claude notes it; you say you switched jobs, Claude appends the new role below the old one.

The schema:

```
# User profile

## Identity
<stable>
- Name: ...
- Location: ...
- Native language: ...
</stable>

## Work
<volatile>
- Employer: ...
- Role: ...
- Team: ...
</volatile>

## Active projects
<volatile>
- ...
</volatile>

## Interests
<stable>
- ...
- Communication style: ...
</stable>

## People mentioned
<volatile>
- Alice (coworker) — owns billing service
</volatile>

## Environment
<deterministic>
- Git user.name: ...
- Git user.email: ...
- OS: ...
- Arch: ...
- CPU count: ...
</deterministic>
```

XML volatility tags tell Claude how to treat contradictions:

| Tag | Meaning | When drift surfaces |
|-----|---------|---------------------|
| `<stable>` | Rarely changes — Identity, Interests | Only on strong signal |
| `<volatile>` | Changes routinely — Work, Projects, People | Early, on any contradiction |
| `<deterministic>` | Captured from env at install | Never flagged for drift |

**Routing rule.** Facts that would still be true in a different repo go in the profile. Facts specific to one repo's conventions go to that project's auto memory instead. Communication style preferences (terse, verbose, no emoji) are personal facts and belong in `## Interests` under `<stable>`.

**Drift review.** Claude appends new facts but never removes old ones; both the old and new line are retained. Contradictions are resolved through `/atomic-improve`, which surfaces a profile drift finding category during its history scan. You accept, modify, or skip each finding. The `<deterministic>` section is excluded from drift detection entirely.

**Environment refresh.** The `## Environment` section is owned by `atomic profile refresh`, which re-detects your dev tooling on demand and rewrites the block wholesale. The registry covers the common dev tools across seven categories: language runtimes (node, python, go, rust, …), package managers (npm, pip, cargo, …), version managers (nvm, pyenv, asdf, …), containers (docker, kubectl, …), monorepo tools (nx, turbo), CLI tools (jq, gh, rg, …), and cloud CLIs (aws, gcloud, az, …). Each entry records the active binary version and its source class (system, homebrew, version-manager, or other). Shell info (`$SHELL`, oh-my-zsh/prezto/starship) is also captured. The `<deterministic>` tag gains a `lastcheck=YYYY-MM-DD` attribute stamped on every refresh.

`atomic profile refresh` — unconditional refresh. `atomic profile refresh --if-stale 7d` — no-op if `lastcheck` is within 7 days, full refresh otherwise. The session-start hook fires `--if-stale 7d` automatically on every Claude Code session open so the env stays current during active use. `atomic doctor` warns when `lastcheck` is absent or older than 30 days.

**Doctor check.** `atomic doctor` reports WARN when `@~/.claude/.atomic/profile.md` is absent from all three candidate files (`~/.claude/CLAUDE.md`, `~/.claude/claude.local.md`, `~/.claude/CLAUDE.local.md`), when the file itself does not exist on disk, or when `lastcheck` is absent or older than 30 days. `atomic doctor --fix` prompts to create the stub or insert the ref, per-item.

**Uninstall.** `atomic claude uninstall` preserves the file. It is user data generated after install and has no pre-install counterpart.


## Skills vs commands

Both are instructions that shape Claude's behavior, but they trigger differently:

**Skills** fire automatically when Claude encounters matching language. You say "let's implement the auth module" and `atomic-tdd` activates — you did not invoke it. You say "looks good, ready to merge" and `atomic-verify` runs a verification check. Skills are the *how* — they enforce discipline without you remembering to ask.

**Commands** fire only when you type the slash. `/atomic-plan`, `/subagent-implementation`, `/commit` — these are explicit actions you reach for on purpose. Commands are the *when* — they orchestrate workflows at the moment you choose.

The split exists because some behaviors should always be on (TDD, verification, debugging discipline) and others should only run when requested (planning, implementing, shipping). A skill that required explicit invocation would get forgotten. A command that auto-fired would be disruptive.

A command can invoke a skill — all ship commands use `atomic-commit` for message format. But a skill never invokes a command. The user decides when to start a workflow; the skill decides how to execute within it.

See [skills](/reference/skills) and [commands](/reference/commands) for full listings.

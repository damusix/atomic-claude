# Concepts

Key ideas behind Atomic Claude, explained plainly. This page covers the *why* — for detailed usage, follow the links to reference pages.


## How it flows

Here is what a real session looks like — adding a new API endpoint to a project:

```
you:    /refresh-signals
        → Claude scans the repo, learns it's a NestJS app with
          Prisma, Jest, and a docs/ folder

you:    /atomic-plan I need a POST /api/webhooks endpoint that
        validates Stripe signatures and queues events
        → Claude writes a spec: controller, service, DTO,
          queue integration, signature validation, retry
          logic. You review, decide retry is out of scope
          for now, and approve without it.

you:    /subagent-implementation
        → Builder agent implements each checkpoint. atomic-tdd
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

you:    /commit-and-pr
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

Every concept below plays a role in that flow. Signals gave Claude the project map. The spec kept implementation on track. TDD fired during each builder checkpoint. Session reports preserved the why. Ship commands handled signals, docs, and the commit message. Follow-ups caught what was deferred.


## Signals

Claude does not know your project. Every session starts fresh — no idea what framework you use, what your build command is, or how your code is organized. Without help, it guesses. Those guesses show up as hallucinated `npm run` scripts, invented `make` targets, and wrong assumptions about your architecture.

You might maintain a `CLAUDE.md` by hand, and that helps — but it inevitably drifts. You add a new service, rename a package, switch from Jest to Vitest, and forget to update the instructions. Now Claude is working from a stale map.

Signals fix this. Running `/refresh-signals` scans your repo and produces two markdown files that Claude reads at the start of every session:

- **Deterministic signals** — facts from the filesystem: directory tree, manifests, languages, lockfile presence. Reproducible and idempotent — running the scan twice produces the same output.
- **Inferred signals** — meaning derived from those facts: framework, build/test/lint commands, domain boundaries, conventions.

Claude reads these files before it reads your code. It knows what it is looking at before you ask your first question.

Signals auto-refresh when you commit through the ship commands, so they stay current without you thinking about it. See [signals workflow](/reference/signals-workflow) for the full mechanism.


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


## Why compressed output?

Claude is verbose by default. It opens with "Sure! I'd be happy to help." It hedges with "perhaps" and "it seems like." It explains what it is about to do, does it, then summarizes what it just did. For a single question, this is fine. Across a working session with dozens of exchanges, the filler adds up — it costs tokens, slows you down, and buries the answer.

Atomic's output style strips this scaffolding. The result is the same information in fewer tokens, faster to scan. A bug explanation that took a paragraph becomes two sentences and a code block. Three intensity levels let you dial it mid-session. Security warnings and irreversible-action confirmations always revert to full prose regardless of intensity.

The output style is the most optional part of Atomic Claude. Everything else — skills, commands, agents, signals — works without it.

See [output style](/reference/output-style) for intensity levels and examples.


## Worktrees

A worktree is a second checkout of the same repo in a different directory, on its own branch. Git supports this natively. Atomic Claude uses worktrees to isolate feature work from your main checkout.

`/worktree-start my-feature` creates `.worktrees/my-feature/` with a new branch, detects your project setup, and runs a baseline test to make sure the isolated copy is healthy before you start working.

Why this matters: you keep your main branch clean in the original checkout while building a feature in the worktree. No stashing, no juggling branches. When the feature is done and you merge or squash, the ship command detects that the branch came from a worktree and offers to clean it up.


## Subagents

Claude Code can spawn specialized agents that run in a fresh context with their own system prompts. Atomic Claude defines a roster of these, each scoped to one job.

The split exists because a single agent doing everything produces worse results. A builder writes code without second-guessing itself. A reviewer catches what the builder missed because it has fresh eyes and re-runs the evidence. An investigator maps the codebase cheaply on Haiku before the more expensive Sonnet agents start writing.

The separation also lets you constrain scope. A surgeon that hard-refuses 3+ file changes cannot accidentally rewrite half your codebase. A strategist that is read-only cannot introduce bugs while reasoning about architecture.

See [agents](/reference/agents) for the full roster and their scopes.


## Specs and design docs

Atomic Claude writes two kinds of durable documents during planning:

**Design docs** (`docs/design/`) are the thinking space. Concepts, business rules, approaches considered, tradeoffs weighed. They capture *why* you chose this direction. Optional — trivial tasks skip this.

**Specs** (`docs/spec/`) are the contract. Checkpoints, success criteria, the implementation plan the subagents will follow. This is what `/subagent-implementation` reads to know what to build.

Specs are append-only. When the contract changes, you add a dated change log entry that records what changed, why, and what the spec used to say. The original intent survives so future readers can trace how the design evolved. Editing a spec in place destroys that history — the same way force-pushing destroys commit history.


## The scratchpad

`.claude/.scratchpad/` is Claude's working memory during a task. It is gitignored and not meant for human consumption.

During `/subagent-implementation`, the scratchpad holds a brief (what to build now), a state log (what happened each iteration), and a follow-ups ledger (non-blocking findings that accumulate). The scratchpad is how the implement-then-review loop survives context compaction — Claude does not need to remember what happened, it reads the file. When the task is done, the scratchpad is deleted.


## Ship verbs

Atomic Claude has ten commands for shipping code. Each is a specific combination of commit, push, squash, PR, and merge — you pick the one that matches your intent. See [commands](/reference/commands) for the matrix.

The reason these exist — instead of just asking Claude to "commit and push" — is what happens *around* the git operation:

- Commands that produce a commit refresh signals and check for stale documentation automatically
- Commands that do not produce a commit check for staleness and ask you before proceeding
- Merge commands run verification and tests on the merged result before completing
- All commands generate commit messages from the diff via the `atomic-commit` skill

You could do all of this manually. The ship verbs make it the default so nothing slips through when you are moving fast.


## The atomic binary

`atomic` is a standalone Go binary that handles things Claude cannot do on its own — or that should not depend on the model:

- **Signals scanning** — reads the filesystem and produces the deterministic signals file. Intentionally done by code so the output is reproducible and fast.
- **Self-update** — fetches the latest release, verifies its checksum, replaces the binary.
- **Health checks** — `atomic doctor` runs nine integrity checks against your install. `atomic validate` lints specs and cross-references.
- **Config/state** — manages `~/.claude/.atomic/config.toml`, follow-ups, install/uninstall.

The binary exists because some operations need to be deterministic, fast, and runnable outside a Claude session. Scanning a repo's tree structure, computing SHA checksums, and managing scheduled jobs are all better done by code than by asking a model.


## Skills vs commands

Both are instructions that shape Claude's behavior, but they trigger differently:

**Skills** fire automatically when Claude encounters matching language. You say "let's implement the auth module" and `atomic-tdd` activates — you did not invoke it. You say "looks good, ready to merge" and `atomic-verify` runs a verification check. Skills are the *how* — they enforce discipline without you remembering to ask.

**Commands** fire only when you type the slash. `/atomic-plan`, `/subagent-implementation`, `/commit-and-pr` — these are explicit actions you reach for on purpose. Commands are the *when* — they orchestrate workflows at the moment you choose.

The split exists because some behaviors should always be on (TDD, verification, debugging discipline) and others should only run when requested (planning, implementing, shipping). A skill that required explicit invocation would get forgotten. A command that auto-fired would be disruptive.

A command can invoke a skill — all ship commands use `atomic-commit` for message format. But a skill never invokes a command. The user decides when to start a workflow; the skill decides how to execute within it.

See [skills](/reference/skills) and [commands](/reference/commands) for full listings.

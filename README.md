<h1 align="center">Atomic Claude</h1>

<p align="center">
 <img src="./assets/atomic-claude.png" alt="Atomic Claude" />
</p>

<p align="center">
 <strong>An opinionated Claude Code configuration. Onboard Claude once: it maps your repo, ships features from issue to merged PR on autopilot, and sharpens its own setup from how you work.</strong>
</p>

<p align="center">
 <em>Stop re-explaining your repo to Claude every session.</em>
</p>

<p align="center">
 <a href="docs/guides/install.md">Install</a> &bull;
 <a href="docs/reference/concepts.md">Concepts</a> &bull;
 <a href="docs/reference/workflow.md">Workflow</a> &bull;
 <a href="docs/reference/commands.md">Commands</a> &bull;
 <a href="docs/reference/skills.md">Skills</a> &bull;
 <a href="docs/reference/agents.md">Agents</a> &bull;
 <a href="docs/credits.md">Credits</a>
</p>

<p align="center">
 <img src="https://img.shields.io/badge/status-evolving-orange" alt="Status" />
 <a href="https://github.com/damusix/atomic-claude/releases/latest"><img src="https://img.shields.io/github/v/release/damusix/atomic-claude?label=latest" alt="Latest Release" /></a>
 <a href="./LICENSE"><img src="https://img.shields.io/github/license/damusix/atomic-claude" alt="License" /></a>
</p>

> **Still evolving.** Commands, agents, and skills may shift between releases. Breaking changes are flagged in the changelog.


## 🌟 Highlights

- **Repo-aware from the first message.** One scan builds a standing map of your codebase that Claude reads before your code, so it stops inventing `npm` scripts.
- **A queryable map of your code.** A tree-sitter symbol graph across 29 languages and 15 web frameworks answers callers, call sites, and blast radius, no compiler required.
- **SQL is a first-class language in the graph.** Procedures, views, and foreign keys across Postgres, MySQL, and T-SQL, read from your `.sql` files with no database connection.
- **Issue to merged PR, hands-off.** `/autopilot` plans, tests first, reviews its own diff, and ships. Your only decision is how to merge.
- **A config that learns from you.** It mines your corrections for friction and edits its own skills and rules, only with your say-so.
- **Replies with structure.** Tables, trees, and ASCII flows replace walls of prose when they explain faster, filler cut.
- **One install, adopt incrementally.** Every layer is optional, from clearer replies up to full autopilot.


## ℹ️ Overview

Atomic Claude is a configuration you install into Claude Code once. Mechanically it is plain markdown files copied into `~/.claude/` (commands, agents, skills, rules) plus one standalone Go binary: no daemon, no API proxy, every file readable before you trust it. By default Claude starts every session blind to your project: it doesn't know your framework, your build command, or how your code is laid out, so it guesses, and you correct the same guesses again and again.

This replaces that with a Claude that knows your repo before it reads your code, takes a feature from issue to merged PR on its own, and refines its own rules from where it last tripped you up. Clearer replies come with it. One install, and you adopt as much or as little as helps.


## 🚀 Usage

Everything below is opt-in. The pieces compose into one lifecycle, and you can run it stage by stage or hand it off whole.

### The workflow, end to end

Fresh-context subagents drive each stage. The builder writes a failing test before any code; the reviewer re-runs tests and gates the diff against the spec; work commits per green checkpoint.

```
plan ........ /atomic-plan writes a design doc + a checkpoint spec
implement ... atomic-builder: failing test first, then the code
review ...... atomic-reviewer: re-run tests, gate against the spec
ship ........ a commit / push / squash / PR / merge verb
```

Run it gated, stage by stage (`/gather-evidence` → `/atomic-plan` → `/subagent-implementation` → a ship verb), or hand the whole loop to `/autopilot`. → [workflow](docs/reference/workflow.md)

### Orient Claude in a new repo

`/atomic-setup` audits conventions, `/refresh-signals` teaches Claude the repo's shape, deterministic facts plus inferred meaning:

```text
❯ /atomic-setup

⏺ Audit

  ┌─────────────────────────────────────┬────────────────────────────────────┐
  │             Convention              │              Status                │
  ├─────────────────────────────────────┼────────────────────────────────────┤
  │ .gitignore has .claude/.scratchpad/ │ ✗                                  │
  │ .gitignore has .worktrees/          │ ✗                                  │
  │ docs/spec/                          │ ✗                                  │
  │ docs/design/                        │ ✗                                  │
  │ Signals files                       │ ✗                                  │
  │ CLAUDE.md @-refs                    │ ✗                                  │
  └─────────────────────────────────────┴────────────────────────────────────┘

  Proposed actions

  [1] Append .claude/.scratchpad/ and .worktrees/ to .gitignore
  [2] Create docs/spec/.gitkeep + docs/design/.gitkeep
  [3] Append signals @-refs to CLAUDE.md

  Apply which actions? → All (1-3)

  ✓ Applied. Next: /refresh-signals

❯ /refresh-signals

⏺ atomic signals scan
  ⎿  Tree · Manifests · Languages → .claude/project/deterministic-signals.md

⏺ atomic-signals-inferrer
  ⎿  Framework: NestJS · package manager: pnpm
  ⎿  Build: pnpm build · test: pnpm test · lint: pnpm lint
  ⎿  → .claude/project/signals.md

  signals initialized.
```

Claude reads that model before your code; ship commands refresh it as the source tree changes. → [signals](docs/reference/signals-workflow.md)

### Query your code's structure

`atomic code index` parses your repo into a symbol graph using tree-sitter. Claude then queries structure instead of grepping for it:

```text
atomic code explore "how does token refresh work"
   → the relevant symbols, files, and call relationships,
     gathered into one context digest.

atomic code impact validateToken
   → every caller that breaks if you change it, transitively.
```

Atomic indexes SQL as a first-class language: `.sql` files join the graph alongside your application code, so Claude can answer which procedures read a table, what a view depends on, or where a foreign key points, across Postgres, MySQL, and T-SQL, with no database connection. Most code tools treat SQL as plain text.

Agents reach for the graph when an index is present and fall back to grep when it isn't. → [code-intel](docs/reference/code-intel.md)

### Hand off the whole feature

`/autopilot` takes a task description or a GitHub issue number and runs the entire lifecycle on its own:

```text
/autopilot 142 squash-and-merge

   → Reads issue #142. Writes a spec: controller, service, DTO,
     queue, signature validation.
   → Worktree-isolated. Builder implements each checkpoint;
     atomic-tdd fires — failing test first, then code.
   → Reviewer re-runs tests and gates against the spec. Every
     finding, blocking or not, gets fixed in-iteration.
   → Stuck twice on the same error? It dispatches a read-only
     strategist for root-cause analysis, then keeps going.
   → Squashes, merges, closes the issue.
```

One decision is yours, how to merge. Everything else runs unattended. → [workflow](docs/reference/workflow.md)

### The rest, at a glance

| Capability | What it gives you | Docs |
|---|---|---|
| **Cross-repo wikis** | `/refresh-wiki` maps a realm of repos and the concerns they share, summarizing the ones it doesn't own without touching them. | [wiki](docs/reference/wiki-workflow.md) |
| **Self-sharpening config** | `/atomic-improve` mines your session history for repeated corrections and proposes one-at-a-time fixes to your own skills and rules. | [concepts](docs/reference/concepts.md) |
| **Output style** | Multi-part answers shaped as tables, trees, and ASCII flows, filler cut. The most optional piece. | [output-style](docs/reference/output-style.md) |
| **Discipline skills** | Seven that auto-fire on natural language: TDD, verify, debug, commit, review, prose, doc-routing. | [skills](docs/reference/skills.md) |
| **Git commands** | Ten verbs across commit / push / squash / PR / merge-to-base, plus CI watch, branch cleanup, worktrees, reminders. | [commands](docs/reference/commands.md) |
| **Persistent profile** | `~/.claude/.atomic/profile.md`: who you are plus auto-detected dev tooling, read every session, refreshed on a staleness check. | [concepts](docs/reference/concepts.md) |

### And the replies take the right shape

A paragraph is one instrument, not the only one. When an answer has parts that sequence, compare, or nest, the output style reaches for the form that shows that structure directly. Same question, same model, same facts, default Claude Code vs. Atomic Claude.

A sequence becomes a flow:

**Default Claude Code:**
```
When a request arrives it first passes through the rate limiter, which checks
the client IP against a sliding window. If that allows it, the authentication
middleware reads the bearer token from the Authorization header and verifies
its signature and expiry. With a valid token, the authorization layer loads the
user's roles and checks them against the route's required permissions before
the handler finally runs.
```

**Atomic Claude:**
```
request
  → rate limit ... sliding window per IP
  → authn ........ verify bearer: signature + expiry
  → authz ........ load roles, check route perms
  → handler
```

A comparison becomes a table:

**Default Claude Code:**
```
If you just want to record work locally, use /commit-only, which stages and
commits without pushing. When you also want it on the remote, /commit-and-push
does both. To open a pull request as well, reach for /commit-and-pr, which
commits, pushes, and opens the PR. And to merge straight into the base branch,
/commit-and-merge handles the whole path.
```

**Atomic Claude:**
```
verb               push  PR   merge
────────────────   ────  ───  ─────
/commit-only        no   no    no
/commit-and-push    yes  no    no
/commit-and-pr      yes  yes   no
/commit-and-merge   yes   –    yes
```

Same facts every time. The shape does the explaining.


## 🪜 Pick your depth

Lost? Run `/atomic-help` in any repo — it reads your git state and names one next command. `/atomic-help tour` walks the whole system in four stages. Otherwise:

| # | Adopt | Do this |
|---|-------|---------|
| 1 | Structured replies | Install, activate the output style via `/config`. Everything else is optional. |
| 2 | A repo explorer | `/atomic-setup` + `/refresh-signals`. Claude stops hallucinating build commands. |
| 3 | A symbol-aware assistant | `atomic code index`, then `atomic code explore "<question>"` returns a digest of symbols, files, and call edges in one query. |
| 4 | The full loop, or autopilot | Read the [workflow reference](docs/reference/workflow.md). |


## ⬇️ Installation

Two commands. The first downloads the `atomic` binary (macOS / Linux / WSL2):

```bash
curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash
```

The second wires up the artifact bundle into `~/.claude/`:

```bash
atomic claude install
```

Activate the output style with `/config` → Output style → Atomic.

Then get the most from it: run `/refresh-signals` in each repo so Claude learns its shape, and `/refresh-wiki` over a folder of related repos for a cross-repo map. If your organization allows Claude Code hooks, `atomic hooks install` wires up profile refresh, pending reminders, and staleness nudges.

For prereqs, flags, existing `~/.claude/CLAUDE.md` handling, updates, Docker evaluation, and uninstall: [docs/guides/install.md](docs/guides/install.md).


## 💭 Contributing & feedback

Atomic Claude dogfoods itself: the root artifacts are both the live config and the bundle source. Bugs and ideas are welcome via [Issues](https://github.com/damusix/atomic-claude/issues). To work on the config, see [docs/guides/contributing.md](docs/guides/contributing.md).


## 📖 Further reading

| Topic | Link |
|-------|------|
| Workflow lifecycle | [docs/reference/workflow.md](docs/reference/workflow.md) |
| Commands | [docs/reference/commands.md](docs/reference/commands.md) |
| Skills | [docs/reference/skills.md](docs/reference/skills.md) |
| Agents | [docs/reference/agents.md](docs/reference/agents.md) |
| Output style | [docs/reference/output-style.md](docs/reference/output-style.md) |
| Signals workflow | [docs/reference/signals-workflow.md](docs/reference/signals-workflow.md) |
| Wiki workflow | [docs/reference/wiki-workflow.md](docs/reference/wiki-workflow.md) |
| Code intelligence | [docs/reference/code-intel.md](docs/reference/code-intel.md) |
| Code-intel MCP setup | [docs/guides/code-intel-mcp.md](docs/guides/code-intel-mcp.md) |
| Concepts (how it flows) | [docs/reference/concepts.md](docs/reference/concepts.md) |
| Conventions | [docs/reference/conventions.md](docs/reference/conventions.md) |
| Install / update / uninstall | [docs/guides/install.md](docs/guides/install.md) |
| Docker evaluation | [docs/guides/evaluations.md](docs/guides/evaluations.md) |
| Contributing | [docs/guides/contributing.md](docs/guides/contributing.md) |
| Credits | [docs/credits.md](docs/credits.md) |
| Specs | [docs/spec/](docs/spec/) |


## License

[MIT](LICENSE)

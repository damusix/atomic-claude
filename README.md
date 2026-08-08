<h1 align="center">Atomic Claude</h1>

<p align="center">
 <img src="./assets/atomic-claude.png" alt="Atomic Claude" />
</p>

<p align="center">
 <strong>Code graphs, wikis, and research-backed agentic coding. Deterministic tools for nondeterministic workflows, in one opinionated Claude Code config.</strong>
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
- **A queryable map of your code.** A tree-sitter symbol graph across 31 languages and 23 web frameworks answers callers, call sites, and blast radius, no compiler required.
- **SQL is a first-class language in the graph.** Procedures, views, foreign keys, and lineage across Postgres, MySQL, T-SQL, and Snowflake — plus dbt models, `ref()`/`source()` lineage, and macros — read from your `.sql` files with no database connection.
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

Fresh-context subagents drive each stage as a maker/checker split (Anthropic's evaluator-optimizer pattern): the implementer writes a failing test before any code; the reviewer re-runs tests and gates the diff against the spec; work commits per green checkpoint.

```
plan ........ /atomic-plan writes a design doc + a checkpoint spec
implement ... atomic-implementer: failing test first, then the code
review ...... atomic-reviewer: re-run tests, gate against the spec
ship ........ a commit / push / squash / PR / merge verb
```

Run it gated, stage by stage (`/gather-evidence` → `/atomic-plan` → `/subagent-implementation` → a ship verb), or hand the whole loop to `/autopilot`. → [workflow](docs/reference/workflow.md)

### Orient Claude in a new repo

`/setup-wiki` audits conventions, `/refresh-wiki` teaches Claude the repo's shape, deterministic facts plus inferred meaning:

```text
❯ /setup-wiki

⏺ Audit

  ┌─────────────────────────────────────┬────────────────────────────────────┐
  │             Convention              │              Status                │
  ├─────────────────────────────────────┼────────────────────────────────────┤
  │ .gitignore has .claude/.scratchpad/ │ ✗                                  │
  │ .claude/worktrees/ ignored          │ ✗                                  │
  │ docs/spec/                          │ ✗                                  │
  │ docs/design/                        │ ✗                                  │
  │ Signals files                       │ ✗                                  │
  │ CLAUDE.md @-refs                    │ ✗                                  │
  └─────────────────────────────────────┴────────────────────────────────────┘

  Proposed actions

  [1] Run atomic repo init (scaffolds .claude/ dirs + ignore rules)
  [2] Create docs/spec/.gitkeep + docs/design/.gitkeep
  [3] Append signals @-refs to CLAUDE.md

  Apply which actions? → All (1-3)

  ✓ Applied. Next: /refresh-wiki

❯ /refresh-wiki

⏺ atomic signals scan
  ⎿  Tree · Manifests · Languages → docs/wiki/scan.md

⏺ atomic-wiki-inferrer
  ⎿  Framework: NestJS · package manager: pnpm
  ⎿  Build: pnpm build · test: pnpm test · lint: pnpm lint
  ⎿  → docs/wiki/index.md

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

Atomic indexes SQL as a first-class language: `.sql` files join the graph alongside your application code, so Claude can answer which procedures read a table, what a view depends on, or where a foreign key points, across Postgres, MySQL, T-SQL, and Snowflake. It also follows the dbt DAG — `ref()`/`source()` lineage and macros across your models — with no database connection. Most code tools treat SQL as plain text.

Agents reach for the graph when an index is present and fall back to grep when it isn't. At a wiki realm root, `atomic code index` indexes every member repo into per-repo dbs under `<realm>/.atomic/`; query verbs fan out across all members and group results under `[<key>]` headers (`--only`/`--exclude` to filter). Nothing is written into any member repo. → [code-intel](docs/reference/code-intel.md)

### Hand off the whole feature

`/autopilot` takes a task description or a GitHub issue number and runs the entire lifecycle on its own:

```text
/autopilot 142 commit squash merge

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
| **Wiki realm browser** | `atomic serve [path] [--port N] [--open]` starts a local read-only HTTP server that renders the wiki realm (or a single repo) as a navigable, Obsidian-style graph: a page view with a live right rail (this-page graph, out/in links, frontmatter Properties with `resource:` as a link), a whole-system graph toggle with OKF node-type coloring and a type legend/filter, a code-file modal (highlighted source + code intelligence), an `md\|code` search box, and federated code search. Bundle-relative `/path.md` links resolve as in-shell navigable routes. Binds 127.0.0.1 by default (`--host 0.0.0.0` opts into read-only LAN exposure), no auth, no write operations. | [serve](docs/reference/serve.md) |
| **Bus chat page** | `atomic serve`'s `/bus` page operates `atomic bus` rooms from the browser: room list, live transcript, an `@` mention composer with addressee chips, halt/resume, and each member's Claude Code session rendered in a paginated rail modal. Loopback-only, regardless of `--host`. | [serve](docs/reference/serve.md) · [bus](docs/reference/bus.md) |
| **Cross-repo wikis** | `/refresh-wiki` maps a realm of repos and the concerns they share, summarizing the ones it doesn't own without touching them. Wiki pages are OKF-aligned: concern and knowledge pages carry `type:` + `description:` frontmatter; the realm `index.md` `## Members` section lists each member with a description. Capture buckets (`atomic wiki bucket add/list/diff/promote`) let you register loose material folders at the realm root; `/refresh-wiki` synthesizes them into topic-keyed `wiki/knowledge/` pages with SHA-256 provenance tracking. `atomic code index` at the realm root layers in a federated symbol graph — query verbs fan out across member repos, nothing written into members. | [wiki](docs/reference/wiki-workflow.md) · [code-intel](docs/reference/code-intel.md) |
| **Bucket doc management** | Capture-bucket docs carry a six-key frontmatter contract (`title`, `type`, `description`, `tags`, `status`, `created`); `atomic wiki bucket doc <bucket> <slug>` scaffolds a topic file (`--router` for one that outgrows a single file), `atomic wiki bucket skill <bucket>` scaffolds a per-bucket authoring skill, and `atomic wiki bucket index` rebuilds the bucket and realm listing regions from that frontmatter, work `atomic wiki scan` already covers as part of its own pass. | [wiki](docs/reference/wiki-workflow.md) |
| **Self-sharpening config** | `/retrospective-learning` mines your session history for repeated corrections and proposes one-at-a-time fixes to your own skills and rules. | [concepts](docs/reference/concepts.md) |
| **Output style** | Multi-part answers shaped as tables, trees, and ASCII flows, filler cut. The most optional piece. | [output-style](docs/reference/output-style.md) |
| **Discipline skills** | Ten that auto-fire on natural language: TDD, verify, debug, commit, review, prose, doc-routing, wiki/bucket routing, visual-options, bus messaging. | [skills](docs/reference/skills.md) |
| **Quick fixes** | `/quick-fix <task>` runs the implement-review loop without a spec, for a fix with a known cause and one obvious approach. | [workflow](docs/reference/workflow.md) |
| **Inter-session messaging** | `atomic bus` lets concurrent Claude Code sessions on one machine message each other over named rooms: `join` a room, address a peer by name or send a room-wide FYI, watch or halt a room as the operator. The daemon auto-spawns on first use; `bus start \| stop \| restart` control it explicitly, with no idle shutdown. | [bus](docs/reference/bus.md) |
| **Persistent REPL sessions** | `atomic repl start --name <s> --lang py\|js` spawns a named interpreter an agent drives across separate Bash calls; `eval` runs code with state surviving between calls, `list`/`status --all` find a session anywhere on the machine, `stop` ends it. Sessions self-terminate after an idle window; eight fixed exit codes route agent remedies. | [repl](docs/reference/repl.md) |
| **Git commands** | `/commit [push\|pr\|merge\|squash\|squash merge]` covers all ship paths from one verb; ask-don't-enumerate. Plus CI watch, branch cleanup, worktrees, reminders. | [commands](docs/reference/commands.md) |
| **Persistent profile** | `~/.atomic/profile.md`: who you are plus auto-detected dev tooling, read every session, refreshed on a staleness check. | [concepts](docs/reference/concepts.md) |
| **Multi-harness paths** | The binary detects which coding agent launched it and keeps repo-local state under that agent's directory: `.claude/` for Claude Code, `.pi/` for the pi agent, `ATOMIC_HARNESS=<name>` for anything else. Per-user state lives harness-neutral at `~/.atomic/`; the pi harness reads its `[pi.agents]` overrides via `atomic config resolve`. | [concepts](docs/reference/concepts.md) |

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
If you just want to record work locally, use /commit, which stages and commits
without pushing. When you also want it on the remote, pass the push token.
To open a pull request as well, reach for the pr token, which commits, pushes,
and opens the PR. And to merge straight into the base branch, the merge token
handles the whole path.
```

**Atomic Claude:**
```
/commit               — commit only (asks next step interactively)
/commit push          — commit + push
/commit pr            — commit + push + PR
/commit merge         — commit + merge to base
/commit squash merge  — commit + squash + merge
```

Same facts every time. The shape does the explaining.


## 🪜 Pick your depth

Lost? Run `/atomic-help` in any repo — it reads your git state and names one next command. `/atomic-help tour` walks the whole system in four stages. Otherwise:

| # | Adopt | Do this |
|---|-------|---------|
| 1 | Structured replies | Install, activate the output style via `/config`. Everything else is optional. |
| 2 | A repo explorer | `/setup-wiki` + `/refresh-wiki`. Claude stops hallucinating build commands. |
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

Then get the most from it: run `/refresh-wiki` in each repo so Claude learns its shape, and at a realm root to build a cross-repo knowledge map. Session-start hooks — profile refresh, pending reminders, staleness nudges — are registered by `atomic claude install` unless you pass `--no-hooks`; run `atomic hooks install` to add them later.

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
| Bus (inter-session messaging) | [docs/reference/bus.md](docs/reference/bus.md) |
| Code intelligence | [docs/reference/code-intel.md](docs/reference/code-intel.md) |
| Code-intel MCP setup | [docs/guides/code-intel-mcp.md](docs/guides/code-intel-mcp.md) |
| Concepts (how it flows) | [docs/reference/concepts.md](docs/reference/concepts.md) |
| Conventions | [docs/reference/conventions.md](docs/reference/conventions.md) |
| Install / update / uninstall | [docs/guides/install.md](docs/guides/install.md) |
| Docker evaluation | [docs/guides/evaluations.md](docs/guides/evaluations.md) |
| Contributing | [docs/guides/contributing.md](docs/guides/contributing.md) |
| Credits | [docs/credits.md](docs/credits.md) |
| Specs | [docs/spec/](docs/spec/) |
| Anthropic patterns behind it | [Building Effective Agents](https://www.anthropic.com/engineering/building-effective-agents), [Effective context engineering](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents) |


## License

[MIT](LICENSE)

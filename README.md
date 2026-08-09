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
 <img src="https://img.shields.io/badge/status-stable-brightgreen" alt="Status" />
 <a href="https://github.com/damusix/atomic-claude/releases/latest"><img src="https://img.shields.io/github/v/release/damusix/atomic-claude?label=latest" alt="Latest Release" /></a>
 <a href="./LICENSE"><img src="https://img.shields.io/github/license/damusix/atomic-claude" alt="License" /></a>
</p>


## 🌟 Features

| Feature | What it does |
|---|---|
| **Repo-aware sessions** | One scan builds a standing map of your codebase that Claude reads before your code, so it stops inventing `npm` scripts. |
| **Code graph** | A tree-sitter symbol graph across 31 languages and 23 web frameworks answers callers, call sites, and blast radius, no compiler required. |
| **SQL in the graph** | Procedures, views, foreign keys, and lineage across Postgres, MySQL, T-SQL, and Snowflake, plus dbt models and macros, read from `.sql` files with no database connection. |
| **Autopilot** | `/autopilot` takes an issue to a merged PR: plans, tests first, reviews its own diff, ships. Your only decision is how to merge. |
| **Self-sharpening config** | `/retrospective-learning` mines your corrections for friction and edits its own skills and rules, only with your say-so. |
| **Structured replies** | Tables, trees, and ASCII flows replace walls of prose when they explain faster. |
| **Incremental adoption** | One install; every layer is optional, from clearer replies up to full autopilot. |


## 🚀 Usage

Everything below is opt-in and composes into one lifecycle. Lost? `/atomic-help` reads your git state and names one next command; `/atomic-help tour` walks the whole system.

### The workflow

Fresh-context subagents drive each stage as a maker/checker split (Anthropic's evaluator-optimizer pattern): the implementer writes a failing test before any code; the reviewer re-runs tests and gates the diff against the spec; work commits per green checkpoint.

```
plan ........ /atomic-plan writes a design doc + a checkpoint spec
implement ... atomic-implementer: failing test first, then the code
review ...... atomic-reviewer: re-run tests, gate against the spec
ship ........ a commit / push / squash / PR / merge verb
```

Run it gated, stage by stage (`/gather-evidence` → `/atomic-plan` → `/subagent-implementation` → a ship verb), or hand the whole loop to `/autopilot <task|issue#> [merge-verb]`, which runs it unattended and leaves you one decision: how to merge. `/quick-fix` runs the same loop without a spec, for a fix with a known cause and one obvious approach. → [workflow](docs/reference/workflow.md)

### Wikis

Wikis are how Claude learns a codebase once instead of every session. Two scopes:

- **Repo wiki**: `docs/wiki/` inside one repository. Build and framework signals, a domain map, cross-cutting notes.
- **Realm wiki**: its own git repo at the root of a folder of repositories. Per-repo summaries, shared concerns, and capture buckets for loose notes, research, and tickets.

The scopes compose. A realm summarizes the repos under it without writing into them, and a member repo that keeps its own wiki is linked rather than re-summarized. Claude loads whichever scope the session sits in: a realm session sees the cross-repo picture, a repo session sees that repo's map. `atomic code index` layers a symbol graph onto either scope; at a realm root it indexes every member, and queries fan out across them.

Two commands drive it. `/setup-wiki` audits a repo's conventions (ignore rules, docs layout, CLAUDE.md wiring) and proposes only what's missing. `/refresh-wiki` builds or refreshes the wiki at either scope: a deterministic scan captures the facts, an inference pass writes the summaries, and ship verbs mark the wiki dirty as the source tree changes, so a session-start nudge tells you when a refresh is due. → [wiki](docs/reference/wiki-workflow.md) · [signals](docs/reference/signals-workflow.md)

### The rest

| Tool | One line | Docs |
|---|---|---|
| `atomic code` | `index` parses the repo into the symbol graph; `explore`, `callers`, `impact` answer structure questions Claude would otherwise grep for. | [code-intel](docs/reference/code-intel.md) |
| `atomic serve` | Read-only localhost browser for wikis and the code graph, plus a loopback-only `/bus` page for operating bus rooms. | [serve](docs/reference/serve.md) |
| `atomic bus` | Named-room messaging between concurrent Claude Code sessions on one machine: address a peer, send a room-wide FYI, watch or halt a room. | [bus](docs/reference/bus.md) |
| `atomic repl` | Named Python or Node interpreter sessions that hold state across separate Bash calls; idle sessions self-terminate. | [repl](docs/reference/repl.md) |
| `/commit` | One verb covers commit, push, PR, squash, and merge; siblings watch CI, clean up branches, and set reminders. | [commands](docs/reference/commands.md) |
| Discipline skills | Ten that auto-fire on natural language: TDD, verify, debug, commit, review, prose, doc-routing, wiki routing, visual options, bus messaging. | [skills](docs/reference/skills.md) |
| Profile | `~/.atomic/profile.md`: who you are plus auto-detected tooling, read every session. | [concepts](docs/reference/concepts.md) |
| Multi-harness | Repo state lives under the launching agent's directory (`.claude/`, `.pi/`, `ATOMIC_HARNESS`); user state stays harness-neutral at `~/.atomic/`. | [concepts](docs/reference/concepts.md) |


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
| REPL (persistent interpreter sessions) | [docs/reference/repl.md](docs/reference/repl.md) |
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

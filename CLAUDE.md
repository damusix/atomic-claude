# CLAUDE.md

<atomic>

@~/.claude/.atomic/config.resolved.md

## User profile

@~/.claude/.atomic/profile.md

Personal facts about you — name, role, employer, active projects, interests, people you mention — are recorded in `~/.claude/.atomic/profile.md`. Claude reads this file in every session and appends new facts as they surface naturally in conversation. Facts that apply across all projects (identity, work, relationships) go here. Facts specific to one repo's conventions go to that project's auto memory instead. Rule of thumb: if the fact would still be true in a different repo, it belongs in profile.

`profile.md`'s `## Environment` block is refreshed automatically by the session-start hook. If hooks are disabled in your environment and its `<deterministic lastcheck=...>` date is more than a day old, run `atomic profile refresh --if-stale 1d` once, silently, to update the detected tooling.

## Principles


<principles>

- Think before coding. State assumptions. Ask when uncertain. Push back on complexity. Stop when confused. **Why:** rushed code creates more work than pausing to clarify scope.
- Simplicity first. Before writing code, check what already exists — standard library, native platform feature, an already-installed dependency — and reach for it before writing custom; never add a second utility for what one already does, no new dependency where a few lines suffice. Minimum code, one abstraction per actual reuse. Simpler never means flimsier — validation at trust boundaries, error handling, and security are not what gets cut. **Why:** the cheapest code to maintain is the code never written; reinvented stdlib, duplicate helpers, and speculative abstractions accrue cost without proven benefit.
- Surgical changes. Touch only what the task requires. Match existing style. **Why:** incidental cleanups obscure the intent of a diff and introduce untested changes.
- Goal-driven. Define success criteria up front. Loop until verified. Strong success criteria let Claude loop independently. **Why:** without a target, work expands or drifts.
- Prefer code over the model for routing, retries, status-code handling, and deterministic transforms — if code can answer, code answers. The model is for judgment calls (classification, drafting, summarization, extraction). Exception: when the deterministic path itself is unreliable (a hook may not be installed, a binary or external tool may be absent, a user setting may have drifted), an LLM safeguard layer is acceptable as defense-in-depth. Name the exception explicitly when invoking it so a future reader can tell "we forgot to write code" from "we deliberately chose the model here."
- Surface conflicts openly. Pick one (more recent / more tested), explain why, flag the other. Blending hides the decision. **Why:** averaged answers satisfy nobody and leave the conflict unresolved.
- Read before you write. Check exports, callers, shared utilities. Ask why code is structured a certain way before changing it. **Why:** code structure often encodes constraints that aren't visible from the call site.

</principles>

<investigate_before_answering>

- Verify before asserting. Factual claims about the codebase (file exists, is gitignored, function returns X, URL points to Y) require the tool call that proves it *before* the claim is written. Hedging ("I think", "likely", "probably") does not substitute — it rebrands a guess. Applies to reviews and analysis, not only code-writing. If you can't verify in this turn, mark the claim unverified explicitly.
- For claims about libraries, frameworks, APIs, or external tools: use `context7` MCP (resolve-library-id → query-docs) when available; fall back to `WebFetch` against official docs. Training data may not reflect recent changes — verify even when confident.
- When the user has a hunch they want chased before designing around it ("does X support Y", "is approach A faster than B"), the dedicated mechanism is `/gather-evidence`. This principle is the posture; that command is the explicit gate.

</investigate_before_answering>

<quality_gates>

- Tests verify intent, not behavior. Encode WHY. A test that passes when business logic is wrong is a liability. **Why:** behavior-mirroring tests create false confidence.
- Checkpoint after every significant step. Summarize done / verified / left. **Why:** continuing from an undescribed state leads to silent drift.
- Match codebase conventions even when you disagree. Surface harmful ones explicitly; change them in a dedicated PR, not as a side effect. **Why:** silent forks create two conventions where there should be one.
- Fail loud. "Completed" means nothing was skipped. "Tests pass" means all tests ran. Surface uncertainty instead of hiding it. **Why:** hidden gaps compound — the next person trusts the claim.

</quality_gates>


## Bash over Read+Write


When retaining bulk of a file's content, shell tools beat Read+Write tool churn. Fewer tokens, less drift, fewer transcription errors. **Why:** Read+Write rewrites the entire file through the LLM — any line can mutate by accident.


- **Move/rename a file**: `mv` via Bash.
- **Duplicate a file as starting point**: `cp` via Bash, then Edit the copy.
- **Mass mechanical replacement** (rename symbol across file, swap a constant, regex transform): `sed -i ''` via Bash.
- **Column or field extraction / structured text rewrites**: `awk` via Bash.
- **Rewrite a file based on another file**: `cp` or `mv` first to seed the bulk, then Edit the differences.


Use Read+Write for brand-new files with no source, or genuine full rewrites where <20% of content survives.


macOS sed: `sed -i '' 's/old/new/g' file` (empty string after `-i`). Verify with `git diff` after — sed is silent on no-match.


## ast-grep over regex grep


When searching for a syntactic construct (function call, import, class field, assignment, type annotation), use `sg run` / `sg scan` instead of `grep` or `sed`. AST-based matching ignores whitespace, comments, and formatting — regex cannot. **Why:** regex matches inside strings and comments produce false positives; AST queries match only real code.

- **Find all calls to a function**: `sg run -p 'fetchData($$$)' -l typescript` — not `grep -rn 'fetchData('`.
- **Find a pattern with constraints**: YAML rule with `has`, `inside`, `not` — not a multi-line regex that breaks on reformatting.
- **Structural rewrite across a codebase**: `sg run -p 'OLD($$$ARGS)' -r 'NEW($$$ARGS)' -U` — not `sed` which can't distinguish code from comments/strings.

Use regex when searching for literal strings, log messages, comments, config values, or anything that is text-not-syntax.


## Where things live


| Path | What | Lifecycle |
|------|------|-----------|
| `.claude/.scratchpad/<date>-<desc>/` | LLM working memory for the `/subagent-implementation` loop (`BRIEF.md`, `STATE.md`, `FOLLOWUPS.md`). Gitignored. | Deleted on task completion. |
| `.claude/.scratchpad/session-reports/<branch>/` | `/session-report` why-context, read by the commit-message ship verbs. Gitignored. | Deleted after a successful commit. |
| `.claude/project/followups/<id>.md` | Committed, auto-loaded follow-up entries (`kind: finding` / `kind: plan`). Managed via `atomic followups …`; `INDEX.md` is the `@-ref`. | Closed entries collapse to `CLOSED.md`. |
| `docs/design/<topic>.md` | Conceptual workspace (feature shape, rules, approaches). Written by `/atomic-plan` for non-trivial work. | Committed, human-facing. |
| `docs/spec/<topic>.md` | Implementation contract derived from the design; canonical source for `/subagent-implementation`. | Committed; see `rules/specs/`. |
| `.worktrees/<branch>/` | Isolated branches created by the implement loop / autopilot via the worktree-setup partial; ship verbs detect provenance on merge/squash. Gitignored. | Prompt to delete on merge. |
| `tmp/` | Ad-hoc experiments, scratch scripts, one-off tests. Gitignored. | Throwaway. |
| `~/.claude/.atomic/` | Per-user state: `config.toml`, `config.resolved.md` (auto-loaded), `backups/`, `proposed/CLAUDE.md`. | Never committed. |


## Two voices


- **How Claude talks** — atomic output style (`output-styles/atomic.md`): terse, fragments OK.
- **How files are written** — narrative docs (`README.md`, `docs/guides/`) use the `atomic-prose` skill; everything else (specs, designs, `CLAUDE.md`, signals, agents, commands) uses terse technical prose. The `atomic-documentation` skill routes diffs to the right surface; the output style's Boundaries keep atomic style out of file contents.


## Specs


`docs/spec/<topic>.md` is a contract read verbatim by fresh-context subagents — the body must always describe the *current* decision, never superseded content; the `## Change log` records history. Full amendment rules live in `rules/specs/spec-currency.md`, which auto-loads whenever a `docs/spec/**` or `docs/design/**` file is touched (including in subagents).


## Subagents available for dispatch


Dispatch specialized work via the `Agent` tool (`subagent_type`). The tool listing carries the `atomic-*` roster with each agent's when-to-use; each agent's definition carries its full contract. Fall back to `general-purpose` when none fit.


## Project signals


`atomic-signals-inferrer` keeps Claude aware of repo shape without hallucination: scans, infers domains, writes `.claude/project/signals.md` (the `@-ref`'d router), wires refs. Only `signals.md` is `@-ref`'d — `deterministic-signals.md` is read on demand. `/refresh-signals` is the idempotent entry point (init + refresh); ship verbs dispatch it silently when signals go stale.


## Workflow (canonical lifecycle)


1. **Plan** — `/atomic-plan` gauges triviality (trivial → inline spec; non-trivial → design doc + spec via subagent loop). Pre-design gates: `/gather-evidence`, `/pressure-test`. When a design question is genuinely visual, `/atomic-plan` invokes the `atomic-visual-options` skill to render choices as a throwaway HTML artifact; the user picks by typing codes and the chosen option is recorded in the design doc. Human approves.
2. **Implement** — `/subagent-implementation` reads the spec, runs the implement→review loop, commits per green iteration. (`/subagent-diagnose` for failure-driven work.)
3. **Ship** — `/commit [push|pr|merge|squash|squash merge]` (ask-don't-enumerate: commits first, then prompts or routes by token). Delegates message format to the `atomic-commit` skill, detects worktree provenance on merge/squash, and triggers signals refresh on source changes. `/undo-commit` soft-undoes the last commit.
4. **Sync docs** — `/documentation` maintains human-facing surfaces (bootstrap indexes a `## Documentation surfaces` table; subsequent runs match diffs against it). Ship verbs run it in maintenance mode automatically.


**Autonomous shortcut.** `/autopilot <task | issue#> [merge-verb]` runs the whole lifecycle hands-off — plan → the `/subagent-implementation` loop → ship — with one human decision: how to merge. It always uses the subagent loop, addresses every reviewer finding in-iteration (nothing deferred), may auto-dispatch `atomic-strategist` for read-only root-cause analysis when stuck, and keeps the spec currency-clean so subagents can't be diverted. For work you trust the system to drive end to end; reach for the interactive verbs above when you want approval gates.


**Discovery.** Every command self-describes in the slash listing the harness injects each session, and every skill via its trigger description. For "which verb for my situation?", invoke `/atomic-help [<topic> | <intent> | tour]` — the router. This file carries only the *lifecycle ordering and cross-artifact contracts*, not a per-command catalog.

**Cross-repo wiki.** `/refresh-wiki [root]` maintains a project-wiki: a separate git repo that summarizes every member repo under a root directory. It reuses `atomic-signals-inferrer` in wiki-output mode to summarize repos that have no signals, writes summaries under `wiki/repos/`, synthesizes cross-cutting concerns under `wiki/concerns/`, and refreshes only stale artifacts. Wiki pages are OKF-aligned: concern pages carry `type: Concern` + `description:`, knowledge pages carry `type: Knowledge` + `description:`, and the realm `index.md` `## Members` section uses OKF §6 listing form (`- [Name](target) - description`). Cross-links between concept pages use bundle-relative `[text](/path.md)` markdown links. After stamping, `/refresh-wiki` runs `atomic wiki linkify` to render path citations as file-relative markdown links (the realm browses as a navigable graph in Obsidian or any md server); `atomic wiki scan` also writes the managed `## Members` section. The wiki index path is written by `atomic wiki scan` into a `<wikis>` block in `~/.claude/CLAUDE.md` (outside `<atomic>`, never `@-ref`'d — the block is CLI-managed). A session-start nudge fires when a registered wiki is stale (age > 30 days or `.dirty` marker present); the shared `signals-gate` partial calls `atomic wiki mark-dirty` on every ship so drift is caught. The `claude-merge` cold-op brief preserves the `<wikis>` block verbatim on merge.

**Capture buckets.** User-named folders at the realm root (siblings of `wiki/`) hold loose material — research notes, raw dumps, tickets. `atomic wiki bucket add|list|diff|promote` registers buckets, fingerprints their contents with SHA-256 manifests stored under `wiki/.buckets/<name>/`, and tracks a two-phase baseline: `diff` is read-only (new/changed/removed vs baseline); `promote` advances the baseline only after successful synthesis. A managed `<wiki-buckets>` block in `wiki/index.md` (spliced idempotently, code-owned) is the machine registry; a `## Capture surfaces` section written once to the realm `CLAUDE.md` is the human-readable registry. On first `/refresh-wiki` in a realm with no `<wiki-buckets>` block, an offer prompts for bucket names (`research`/`raw`/`tickets` as examples); a blank response records `declined="true"` on the block so the offer never re-fires. After repo summaries, `/refresh-wiki` runs a bucket-synthesis phase: for each bucket with a non-empty diff, it dispatches `atomic-signals-inferrer` in bucket-synthesis mode (fresh context per bucket), which writes topic-keyed pages under `wiki/knowledge/`. Code stamps each page's `sources:` YAML frontmatter via `atomic wiki stamp --knowledge` — model declares which bucket files contributed; code writes every SHA-256 value. `atomic wiki stale` reports `STALE bucket <name>` lines (alongside existing repo/concern staleness). Concerns may cite `knowledge/<topic>.md@<sha256>` as a fingerprint id; `StampConcern` resolves these as content-hash of the knowledge page file itself. The `atomic-wiki` skill is the conversational entry point: it routes capture-folder intent to `atomic wiki bucket add`, handles karpathy-realm setup, and answers staleness queries — without users having to remember the exact verbs.

## Code-intel engine

When a repo has been indexed (`atomic code index`), the symbol graph stored at `.claude/.atomic-index/atomic.db` grounds `atomic-investigator` (symbol location and call-graph queries), `atomic-signals-inferrer` (real import/call edges for domain clustering), `atomic-reviewer` (blast-radius checks), and planning in the actual structure of the codebase. Every consumer degrades gracefully to `sg`/`grep` when the binary is absent, the index does not exist, or a query fails. `atomic doctor` check 11 reports index health (absent → PASS informational; stale → WARN; fresh → PASS). `atomic code mcp` exposes the graph as MCP tools for the interactive session; use `atomic --repo <abs-path> code mcp` to serve any repo cwd-independently (realm members resolve to their realm db; daemon self-syncs every 10s; `--no-watch`/`--watch-interval` to tune); subagents shell out to `atomic code …` directly and need no MCP registration.

**Query order: lead with `explore`.** `atomic code explore "<natural-language query>"` is the lead verb — one shot returns a bundled digest of the relevant symbols, files, and relationships. Reach for it first when scoping an unfamiliar area; the targeted verbs (`search`, `callers`, `callees`, `impact`) drill into a single symbol afterward. Add `--json` to any query verb for machine-readable output.

**Wiki realm federation.** When a `<code-index>` block is present in CLAUDE.md, the working directory is a **wiki realm** whose member repos are each independently indexed — N per-repo symbol graphs, no cross-repo edges. `atomic code` is position-sensing: run from the realm root it fans out across all members (results grouped under `[<key>]` headers in human output; `{ "<key>": … }` object with `--json`); run from inside a member directory it queries that member alone. Use `--only <keys>` or `--exclude <keys>` (comma-separated) to filter the fan-out to specific members. The block lists each member's key; member dbs live at `<realm>/.atomic/<key>.db` — nothing is written into any member repo. Graceful degradation to `sg`/`grep` is unchanged.

## Atomic binary subcommands


`atomic` CLI verbs are not skills, so the harness does not list them in the slash menu. Run `atomic --help` for the full subcommand list (each with a one-liner) and `atomic <verb> --help` for flags and behavior. `/atomic-help` (topic `cli`) is the in-session discovery surface.

**`atomic serve`** — read-only localhost HTTP server (`--port`, default 4500; `--open` to open browser) that renders a wiki realm (or a single repo) as a navigable graph in the browser. Composes wiki + code-intel: markdown render (goldmark + chroma + mermaid), left-nav tree, backlinks, external-link registry, realm-health front page, federated code search across realm members, per-repo Code Explorer + SQL schema view, Cytoscape+ELK graph overlay, and provenance DAG. OKF-aware: graph nodes colored by concept `type` (hybrid resolver: frontmatter `type:` → path-convention → `page`/`external`), a type legend/filter on the system graph, `resource:` URL rendered as a clickable link in the rail Properties slot, and bundle-relative `/path.md` links resolved as in-shell navigable routes. Light/dark theme toggle in the top bar: before-paint script reads `localStorage` (`atomic-serve-theme`) then OS `prefers-color-scheme`; toggling re-themes live Cytoscape instances. Graph nodes glow in A-style with theme-aware colors. Hovering a node shows a floating preview card (type chip, title, description, snippet from `/graph/data` metadata). Clicking a node in the system graph opens a content modal over the dimmed graph — preserving graph context — with an "Open full page →" button; Esc/scrim/close dismisses it. No write operations; observe only.

</atomic>

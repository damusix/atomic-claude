# CLAUDE.md

Project-local context for working **on** this repo. Not copied anywhere — read by Claude only when the cwd is this repo. The contract that ships to users is `context/CLAUDE.md`, a different file.


## What this repo is

A holistic Claude Code configuration. Everything that installs lives under `context/`: `CLAUDE.md`, `commands/`, `agents/`, `skills/`, `rules/`, `output-styles/`. They are designed as one coherent system — atomic output style, an opinionated command set, a small subagent roster, and discipline skills that interlock. Not a grab-bag; everything is meant to compose.

Replaces (for the author) heavier toolkits like superpowers and caveman. Personal config, no stability guarantee.


## Platform support

Target macOS and Linux only. Drop Windows-specific review findings, Windows-only test paths (`os.PathSeparator` probes), and Windows compatibility gates. `.goreleaser.yaml` may still produce Windows binaries — that's fine, but correctness on Windows is not a concern.


## File roles (this repo specifically)

| File | Role | Destination |
|------|------|-------------|
| `context/CLAUDE.md` | Single source of truth. Two roles in one file: (a) the global contract that ships as every user's `~/.claude/CLAUDE.md` on install, and (b) this repo's own project instructions when working *on* atomic-claude. Since it no longer sits at the repo root, Claude Code does not auto-load it here — the `@context/CLAUDE.md` ref below pulls it in. Not to be confused with the root `CLAUDE.md`, which is the row below. | `context/`, committed → `~/.claude/CLAUDE.md` on install |
| `CLAUDE.md` (this file) | Project-local overlay for this repo *only*. Build pipeline rules, doc reference paths, mandatory checklist, design axioms. Auto-loads at the repo root, and pulls in `context/CLAUDE.md` by `@`-ref. Do NOT duplicate `context/CLAUDE.md` content here — both load into context, duplication = wasted tokens. | Repo root, committed, never installed. |
| `README.md` | Human-facing overview of what the config does and how to install it. | Repo root, committed. |
| `context/commands/*.md` | Slash command definitions, committed in source form. May carry `{{ template "<flow>" . }}` directives resolved against `context/_partials/`; expansion happens on the way into the embedded bundle, never back into `context/`. Copied to `~/.claude/commands/` by `atomic claude install`. | `~/.claude/commands/` |
| `context/agents/*.md` | Subagent definitions, same contract as commands. Every agent composes at least `agent-atomic-voice`. | `~/.claude/agents/` |
| `context/_partials/<name>.md` | Reusable blocks composed by command AND agent sources via `{{ template "<name>" . }}`. One shared pool — a partial defined once is callable from either kind. Never installs: the mirror walks only the artifact kind directories, and `_partials/` is not one of them. The full inventory and the per-artifact composition tables live in `docs/wiki/bundle.md` — read them there rather than keeping a second copy here, which is what let this row drift. One rule that is not derivable from the files themselves: `agent-yagni` is kept **verbatim in sync** with the Simplicity-first (YAGNI) ladder in `context/CLAUDE.md`'s Principles block, and `CLAUDE.md` is not expanded, so nothing enforces the match. | Not copied; consumed at build time. |
| `context/skills/*/SKILL.md` | Discipline skills. Copied to `~/.claude/skills/`. | `~/.claude/skills/` |
| `context/output-styles/*.md` | Output style definitions. Copied to `~/.claude/output-styles/`. | `~/.claude/output-styles/` |
| `context/rules/<topic>/*.md` | **Shipped** path-scoped topic rules. `paths:` frontmatter globs (e.g. `**/*.{ts,tsx}`, `docs/spec/**`) so the rule only loads when Claude touches a matching file — auto-loads into subagents too (verified). Currently: `typescript/`, `python/` (language style), `specs/spec-currency.md` (spec body-is-truth, globs `docs/spec/**` + `docs/design/**`). | `~/.claude/rules/` (via `atomic claude install`) |
| `.claude/rules/<topic>/*.md` | **Repo-only** path-scoped rules — committed (gitignore-negated) but NOT bundled, so they never ship to users. `.claude/rules/authoring/` holds the contributor artifact-authoring references (`agent-config`, `prompting`, `claude-code-refs`, `axioms`) — glob the artifact sources (`context/**`) so they auto-load only when editing an artifact, instead of `@`-ref'ing every session. | Stays in repo; never installed. |


## Reference docs


**Artifact-authoring references** — frontmatter/dispatch semantics (`.claude/rules/authoring/agent-config.md`), Anthropic prompting patterns (`.claude/rules/authoring/prompting.md`), upstream Claude Code doc URLs (`.claude/rules/authoring/claude-code-refs.md`), and atomic's design axioms (`.claude/rules/authoring/axioms.md`) — are now **path-scoped rules**. They auto-load only when an artifact source is touched (anything under `context/`), in the main agent and in subagents, instead of `@`-ref'ing every session. Read before adding or editing any command / agent / skill / output-style / rule.


These stay auto-loaded every session (project-specific, compact):

### Global contract (auto-loaded)

`context/CLAUDE.md` is the file that installs as every user's `~/.claude/CLAUDE.md`. It no longer sits at the repo root, so Claude Code will not pick it up on its own — this ref is what keeps the repo dogfooding its own contract.

@context/CLAUDE.md


### Project signals (auto-loaded)

@docs/wiki/index.md


### Project follow-ups (auto-loaded)

@.claude/project/followups/INDEX.md


## Coherence rules (when editing here)

- Treat the five artifact types (commands, agents, skills, output-styles, rules) as one system. A change to one often demands a matching change to the others.
- `CLAUDE.md` is the global contract. Adding a command/agent/skill that other artifacts reference? Update `CLAUDE.md` so every workspace knows it exists.
- `README.md` is the public-facing index. New artifact, removed artifact, or renamed verb → update the tables.
- Atomic output style applies to Claude's TUI replies, not to the files in this repo. Command/agent/skill prose stays in normal English so it reads cleanly when installed.
- Skill triggers, agent dispatch criteria, and command behaviors must not contradict each other. If `/atomic-plan` says it writes to `docs/spec/` and an agent expects `docs/specs/`, that's a bug.


## Adding a new artifact (mandatory checklist)


This is the **invisible-feature prevention checklist**. A new artifact is not "done" until every applicable row is updated. Skipping a row means the feature exists in code but nobody — user, agent, or future-you — knows it exists.


Run this whenever you add, rename, or remove a command / agent / skill / output-style / rule. Do not batch across artifacts — finish the checklist for one before starting the next.

<mandatory_checklist>

| # | Surface | When to update | What to write |
|---|---------|----------------|---------------|
| 1 | The artifact file itself | Always | `context/agents/atomic-*.md`, `context/commands/<verb>.md`, `context/skills/<name>/SKILL.md`, `context/output-styles/atomic-*.md`, or `context/rules/<lang>/*.md` — one file, no separate template. Use the `atomic-` prefix for custom artifacts. |
| 2 | `CLAUDE.md` | Always — single source of truth | This is both (a) the global contract that ships as every user's `~/.claude/CLAUDE.md` on install, and (b) the committed project instructions when working *on* atomic-claude. One file, both roles. Agents and skills are surfaced by the harness each session (agent roster + skill trigger descriptions), so they need no CLAUDE.md registry section — keep each artifact's own description accurate instead. Commands go only into the `## Workflow` lifecycle ordering (the per-command catalog was removed; discovery is via the harness slash listing + `/atomic-help`); naming conventions cover output styles/rules. |
| 3 | `CLAUDE.md` (root, this file) | Only when the new artifact changes *project-local* conventions for this repo specifically (e.g. new bundle path, new build step, new file role) | Edit the relevant section. This file never installs. Do NOT duplicate the global registration here — the root file is for repo-specific overlays only, not for mirroring `context/CLAUDE.md`. Both load into context when cwd is this repo, so duplication = wasted tokens. |
| 4 | `README.md` | Always — public-facing index | Add to the matching table in `docs/reference/commands.md` (or agents/skills equivalent). Keep one-line descriptions. |
| 5 | `docs/spec/<topic>.md` | If the artifact has non-trivial behavior or cross-references | Write or extend the spec. Required for anything dispatched by another artifact or that mutates state. **Amending an existing spec: see "Spec amendment rule" below — never silently overwrite the original.** |
| 6 | Cross-references in other artifacts | If this artifact is invoked by, or invokes, another | Wire both directions. Example: a new skill invoked by `/commit` requires editing the command to call it AND the skill to declare itself as called from there. |
| 7 | **`/atomic-help` topic table + tour** ⚠ | **Always** — every artifact a user might type, install, or run. Non-negotiable. | Edit `context/commands/atomic-help.md`. Add / remove / rename the row in the right category sub-table (Lifecycle / Ship matrix / State & context / Maintenance & utilities / Reference). Material lifecycle or maintenance change → also update the matching tour stage (Stage 2 lifecycle / Stage 3 state files / Stage 4 maintenance). **Read the full contract in `<help_router_contract>` below before skipping any sub-rule.** |
| 8 | Bundle inclusion (`atomic/internal/bundlemirror/mirror.go`) | Only if you introduce a **new artifact kind** (not a new file of an existing kind) | Add the inclusion rule. Existing kinds (`context/agents/`, `context/commands/`, `context/skills/`, `context/output-styles/`, `context/rules/`) auto-include matching files. |
| 9 | Signals refresh | After adding the file | Run `/refresh-signals` (or let ship verbs dispatch `atomic-signals-inferrer` in silent mode) so `docs/wiki/scan.md` and `docs/wiki/index.md` reflect the new file. |

</mandatory_checklist>

**Verification before commit.** Grep for the new artifact name across the repo. Every place it is *referenced from* should also reference it *back* where appropriate. A skill mentioned only in its own SKILL.md is an invisible skill.


<build_pipeline>

## Embedded bundle: a build artifact, never committed


The `atomic` binary embeds `context/` at build time via `go:embed`. `go:embed` cannot name a parent directory (`../../../context` is a pattern-syntax error) and will not follow a symlink (`cannot embed irregular file`), so the tree is mirrored into `atomic/internal/embedded/bundle/` before it can be embedded.


**That mirror, and the `manifest.go` beside it, are gitignored.** They are outputs, not sources. Nothing that produces a shipped binary depends on them being in git: `make build`, `make test`, and `make vet` all declare the `bundle` target as a prerequisite; CI runs `go generate ./...`; goreleaser runs the same in its `before` hook. There is no bundle drift gate, because there is nothing committed to drift from.


**How to regenerate.** From the repo root: `make -C atomic bundle`. Nothing to stage afterwards.


**Failure mode to recognize.** A bare `go build` or `go test` on a fresh clone, bypassing `make`, fails with `pattern bundle: no matching files found`. That is the mirror being absent, not a broken tree — run `make -C atomic bundle`.


**Pre-commit hook.** `.githooks/pre-commit` (installed via `make hooks`, which sets `core.hooksPath=.githooks`) has two stages, each firing only when a staged path touches its own inputs: (1) `atomic followups render` when any followups entry file (other than INDEX.md) is staged, re-staging `INDEX.md` (degrades to WARN if `atomic` binary absent); (2) `make frontend` when `atomic/internal/serve/frontend/**` outside `dist/` is staged, re-staging `frontend/dist/`. There is no render or bundle stage — neither produces a committed file.


**`atomic hooks` vs git hooks — different systems.** `atomic hooks install` registers a Claude Code session-start hook (injects pending reminders into context). That has nothing to do with the build pipeline. Render parity is enforced by CI; the git pre-commit hook in `.githooks/` is the local convenience layer.


## Shared partials


A command or agent source may compose a reusable block with `{{ template "<name>" . }}`, resolved against `context/_partials/`. Both kinds draw from one pool. Expansion happens inside `make bundle`, on the way into the embedded bundle — nothing is ever written back into `context/`, so an artifact exists in exactly one place and a partial edit reaches every consumer on the next build.


**Only commands and agents expand.** Skills, rules, output styles, and `context/CLAUDE.md` are copied byte-for-byte, so a literal `{{` in their prose is safe. A directive naming a partial that does not exist fails the build rather than shipping an artifact with a hole in it.


**Adding an artifact is one file.** `context/commands/<verb>.md` or `context/agents/<name>.md` — there is no second location to keep in sync, and no orphan rule, because there is no separate output to orphan.


**Pipeline order.** One generation step: `make bundle` expands `context/` into `atomic/internal/embedded/`. Alongside it, `atomic followups render` regenerates `INDEX.md` and `make frontend` rebuilds `serve/frontend/dist`. CI has no render or bundle drift gate — neither output is committed.


</build_pipeline>


## Spec amendment rule (`docs/spec/<topic>.md`)


The canonical spec-currency contract lives in **`context/rules/specs/spec-currency.md`** — a path-scoped rule that auto-loads (into the main agent *and* subagents) whenever a `docs/spec/**` or `docs/design/**` file is touched. It covers: body-is-current-truth, the `## Change log` discipline, the per-amendment rules (add / change-supersede / remove / correct / rename), and the change-log entry template. Read that rule rather than duplicating it here.

Repo-specific note: in this repo specs are also read verbatim by fresh-context subagents in `/subagent-implementation` and authored via the `/atomic-plan` spec loop, so currency is load-bearing — a superseded body section gets built. The planning and autopilot commands brief spec-writing subagents to follow the rule explicitly in addition to the auto-load.


## Cross-artifact wiring rules (mandatory for cohesion)


These rules exist because this repo is meant to be installed into *user repositories* — not just dogfooded here. Cohesion is the product. When a user runs `/commit` in their own repo, they expect signals to refresh and docs to stay current without typing five commands.


- **The implementation phase owns signals refresh; ship verbs are the ad-hoc fallback.** The primary refresh point is the end of an implementation phase: `/subagent-implementation` (Phase 3 finalize) and `/autopilot` (Phase 4) dispatch `atomic-signals-inferrer` once over the loop's SHA range (`changed_range: <loop-base>..HEAD`), committed as `chore(signals)`. The ship verbs (`/commit` and its merge/squash flows, via the `signals-gate` partial) refresh only when the user is **ad-hoc committing a real code change**: the gate skips docs-only commits (every staged path under `docs/` or a top-level `README*`/`CHANGELOG*`/`LICENSE*`-class file) and relies on the content-based `atomic signals stale` exit code as the coordinator — a fresh stored file (because the implementation phase already refreshed) returns exit 0, so the gate is a no-op, avoiding a redundant dispatch. If neither path refreshes, the user's project signals go stale — invisible drift. Contract: `docs/spec/signals-refresh-timing.md` (child of `docs/spec/signals-workflow.md`).
- **Ship verbs must remind the user to run `/documentation` after significant changes.** "Significant" = new file, removed file, public-API change, dependency change. Surface a one-line prompt at the end of the verb. The skill is interactive and user-driven (axiom 3: destructive ops explicit confirm; doc rewrites are close enough).
- **Symmetry within a command family.** The commit/squash/merge family must agree on shared concerns: message format (all delegate to `atomic-git-discipline` skill), worktree detection (all detect on merge/squash and prompt to delete), signals refresh trigger (above). If you change one verb's behavior on a shared concern, change all of them.
- **Skills that are invoked by commands must declare it.** A skill's description should mention "invoked by /foo, /bar" so the trigger surface is inspectable. Reverse holds: a command that invokes a skill must name it in the command file. No silent dependencies.
- **Agents dispatched by commands must name the `subagent_type` in the command file**, and the agent's own `description` must carry an accurate when-to-use (that's what the harness surfaces in the session roster — there is no CLAUDE.md agent registry). Dispatch is a public contract.
- **When in doubt, write the spec first.** `docs/spec/<topic>.md` is the canonical source for any cross-artifact contract. If two artifacts reference the same flow and the spec doesn't exist, write it before adding the second reference.


**Why these rules apply to user repos, not just this one.** Users install these artifacts and rely on the cohesion. A user's `/commit` that forgets to refresh signals leaves *their* Claude session with a stale project map. The bug is invisible to us but real to them. Treat every wiring rule as a contract the user has implicitly accepted by installing.


## ⚠ Help router coverage rule (`/atomic-help`) — CRITICAL


<help_router_contract>

**Hard rule. Non-negotiable. Failing this rule ships invisible features.**

**Why this is critical:** `/atomic-help` is the canonical onboarding map and the only discoverability surface for new users. It is a routing layer — not duplicated docs — so nothing automated detects drift. A command that exists but is unmentioned in help may as well not exist; users typing `/atomic-help` or `/atomic-help tour` will never find it. Every artifact add / remove / rename has a corresponding help update, and that update is part of the same change, not a follow-up.

**Triggering events.** Every one of these requires a `context/commands/atomic-help.md` edit before commit:

- Adding any command, agent, skill, output-style, or rule.
- Removing any of the above.
- Renaming any of the above.
- Adding a new user-runnable `atomic <verb>` binary subcommand.
- Changing what an existing surface does, in a way that would alter its one-line description.
- Reshaping the canonical lifecycle, state-file layout, or maintenance surface (touches the tour stages).

**Sub-rules (all hard, no exceptions):**

- **Every committed slash command must appear in at least one `/atomic-help` topic row.** Primary verb for its own topic, or named alternative in another topic's output. A command not discoverable through help is invisible to new users.
- **Every committed agent and skill must be reachable through a topic.** Agents → topic `agents`. Skills → topic `skills`. Surfacing the roster suffices — individual agents/skills do not each need their own topic, but the roster must stay accurate.
- **Tour stages mirror the documented surface.** Stage 1 (surfaces) names the five composing layers (output style, skills, commands, agents, binary). Stage 2 (lifecycle) lists the canonical plan → implement → ship → docs verbs. Stage 3 (state files) enumerates where things live (signals, scratchpad, session reports, follow-ups, worktrees, design/spec). Stage 4 (maintenance) covers doctor / validate / update / cleanup / ci / report. Adding a new artifact in one of those zones means updating the matching stage in `context/commands/atomic-help.md` alongside the topic table.
- **Renames update both the topic row and every freeform-intent example.** Run `grep -n '/<old-verb>' context/commands/atomic-help.md` after a rename — must return zero matches.
- **Removals delete the topic row, any freeform-intent example, and any tour-stage mention.** No dangling pointers.
- **Binary subcommands surfaced to users count as commands for this rule.** New `atomic <verb>` that a user runs directly → mention it under the `binary` / `cli` topic (or whichever maintenance / setup topic fits). Internal subcommands invoked only by other artifacts do not.
- **Final pass before commit (mandatory).** Open `context/commands/atomic-help.md`, scan every category table and all four tour stages, ask: *"Would a new user typing `/atomic-help` discover the change I just made?"* If no, fix `context/commands/atomic-help.md` and stage it. This is the gate — do not commit without it.

**Reshape-don't-cram clause.** If the topic taxonomy becomes the wrong shape (categories overflow, a stage gets bloated past ~15 lines, a topic table grows past one screen), reshape it. The point of the router is discoverability, not exhaustiveness; a help command nobody can scan is worse than one missing one verb. When in doubt, split a category or promote a sub-topic into its own row.

**Verification command (run before committing any artifact change):**

```bash
# Every committed slash command should have at least one mention in atomic-help.
for cmd in context/commands/*.md; do
  verb=$(basename "$cmd" .md)
  [ "$verb" = "atomic-help" ] && continue
  grep -q "/$verb" context/commands/atomic-help.md || echo "MISSING: /$verb"
done
```

Zero `MISSING:` lines = pass. Any output = blocker.

</help_router_contract>


## Signals `@-ref` must stay wired (in this repo: the root `CLAUDE.md`)


Only `signals.md` (the compact router) is `@-ref`'d. `deterministic-signals.md` is NOT — it can be thousands of lines on large repos and would blow up context. The inferrer reads it on demand; sessions do not need it. `docs/wiki/CLAUDE.md` (steering) is also NOT `@-ref`'d — it lazy-loads as nested memory whenever Claude reads a file under `docs/wiki/`, which is exactly when the inferrer operates.


**In this repo specifically**, the ref lives in the root `CLAUDE.md` (this file) — not in `context/CLAUDE.md`. Reason: `context/CLAUDE.md` is the bundle source (it installs as every user's global `~/.claude/CLAUDE.md`), so project-specific paths there would leak into every install. The root file never installs, and Claude Code auto-loads it when cwd is this repo. That's the correct home for the project-scoped `@`-ref.


- The `atomic-signals-inferrer` agent checks for `@docs/wiki/index.md` in `claude.local.md` / `CLAUDE.local.md` first, then `CLAUDE.md`. If present in ANY of them, it skips wiring. The agent's search order is the contract.
- For most repos, the ref ends up in `CLAUDE.md` (one file, no separation) — which is also where it lives here, now that the shipped contract has moved to `context/CLAUDE.md` and freed the root name.
- If you fork the layout (e.g. moving refs into a separate `@`-included file), update the agent's search order in lockstep.


## Documentation surfaces

| Path | Covers | Voice |
|------|--------|-------|
| `README.md` | project overview, install, commands, agents, skills | atomic-writing |
| `docs/guides/install.md` | installation, updating, uninstalling | atomic-writing |
| `docs/guides/getting-started.md` | first-session quickstart, output style, repo setup, first task, updating | atomic-writing |
| `docs/guides/knowledge-base.md` | karpathy realm, capture surfaces (research/raw/tickets), fingerprint synthesis to wiki/knowledge | atomic-writing |
| `docs/guides/contributing.md` | contributing, build pipeline, testing | atomic-writing |
| `docs/guides/evaluations.md` | Docker eval environment, testing setup | atomic-writing |
| `docs/guides/code-intel-mcp.md` | code-intel MCP server setup, .mcp.json, tools | atomic-writing |
| `docs/reference/workflow.md` | plan, implement, diagnose, ship lifecycle | atomic-writing |
| `docs/reference/commands.md` | command reference table | atomic-writing |
| `docs/reference/agents.md` | agent reference table | atomic-writing |
| `docs/reference/skills.md` | skills reference table | atomic-writing |
| `docs/reference/signals-workflow.md` | signals scan, infer, wire pipeline | atomic-writing |
| `docs/reference/code-intel.md` | code-intel engine: verbs, index, lifecycle, workflow integration | atomic-writing |
| `docs/reference/output-style.md` | atomic output style reference | atomic-writing |
| `docs/reference/concepts.md` | how atomic-claude fits together, the atomic binary, code intelligence flow | atomic-writing |
| `docs/reference/conventions.md` | atomic style scope, CLAUDE.md hygiene, commit bylines, scratchpad/tmp rules, worktree auto-detect | atomic-writing |
| `docs/reference/serve.md` | `atomic serve` usage, scope resolution, browsing wiki + code graph locally | atomic-writing |
| `docs/reference/wiki-workflow.md` | wiki setup, repo/realm scope, wiki verbs (scan/stale/linkify/bucket) | atomic-writing |
| `docs/reference/bus.md` | `atomic bus` room model, addressed vs FYI, envelope, daemon lifecycle, exit codes, operator verbs | atomic-writing |
| `docs/reference/repl.md` | `atomic repl` persistent interpreter sessions, scope model, six verbs, exit codes, idle_timeout config | atomic-writing |
| `docs/reference/atomic-toml.md` | repo-scoped `.claude/atomic.toml`: scope marker, code-index ignore globs, repl idle_timeout, lenient load contract | atomic-writing |
| `docs/credits.md` | inspirations, prior-art credits | atomic-writing |
| `docs/index.md` | VitePress site homepage, feature highlights, tagline | atomic-writing |
| `context/CLAUDE.md` | global contract, agent/command/skill registry | atomic-writing |


## Research notes (`docs/research/`)


Point-in-time research and decision records: a problem investigated, the experiment that settled it,
and the agreed plan. Committed, human-facing, `atomic-writing` voice like every other file. Mermaid allowed (it is a `docs/` file).

These are **not** maintained documentation surfaces — keep them out of the `## Documentation surfaces`
table above so the `atomic-documentation` maintenance flow never flags them as stale. A research note
reflects what was true when written; it is an audit trail, not a living doc. Supersede a finding with a
new note (or a short `Status:`/update line) rather than silently rewriting the original. Distinct from
`docs/design/` (conceptual workspace for a feature about to be built) and `docs/spec/` (implementation
contract) — research notes capture *why a path was chosen*, including paths not taken.

Current notes:

- `docs/research/tsbinding-vendor-on-demand.md` — why `atomic code index` **used to** OOM (committed
  generated `parser.c` indexed), the experiment proving vendor-on-demand viable, recovered upstream pins,
  and the now-implemented fix. **RESOLVED:** `tsbinding/src/` is gitignored / vendor-on-demand, and the
  indexer respects `.gitignore` (`git ls-files --exclude-standard`), so indexing is safe — do not skip
  it citing OOM. (Latent edge: the non-git `walkDirFallback` does not skip `src/`.)


## VitePress docs site (public, not bundled)


The `.vitepress/` theme, `docs/index.md` landing page, and `package.json` are the public docs site only — **not** part of the Go build or the embedded bundle. Editing them does NOT require `make bundle`, and neither does `README.md` — the bundle mirror ships only what is under `context/`. Build/verify with `npm run docs:build` (~1.2s; harmless `@vueuse/core` `#__PURE__` Rollup warnings are expected).


### Home feature-card icons — use an icon font, not inline SVG


The landing page (`docs/index.md`) feature cards use **Font Awesome 7 Free (solid)** glyphs, set per-card in frontmatter as a YAML `\uXXXX` escape (e.g. `icon: "\\uF0E7"` for `bolt`).


- **Why not inline SVG.** VitePress (2.0.0-alpha.17) HTML-escapes frontmatter values before the `v-html` icon slot renders them, so `icon: '<svg>…</svg>'` ships as visible escaped text (`&lt;svg&gt;`). A font glyph is a single PUA codepoint — plain text, nothing to escape — so it survives.
- **Why a font over `{ src }` image icons.** The glyph inherits `--vp-c-brand-1` via `currentColor`, so it flips amber (light) / gold (dark) for free. An `<img>` SVG can't theme-react without baked-in color or light/dark variants.
- **Wiring.** `.vitepress/theme/index.ts` imports `@fortawesome/fontawesome-free/css/solid.min.css` (gives the `@font-face`; Vite self-hosts `fa-solid-900.woff2`, no CDN). `.vitepress/theme/custom.css` binds `.VPFeature .icon` to `font-family: "Font Awesome 7 Free"; font-weight: 900`. FA is a `devDependency`.
- **Picking glyphs.** Read exact codepoints from `node_modules/@fortawesome/fontawesome-free/metadata/icon-families.json` (`unicode` field; confirm `familyStylesByLicense.free` includes `{classic, solid}`) — never guess the hex. Lucide has no official font, so it is not an option for this approach.


## Release-please conventional commit types — hard rules


This repo uses [release-please](https://github.com/googleapis/release-please) to generate the changelog and tag releases. Its default `changelog-sections` config **filters out** several conventional-commit types. Anything filtered ships invisibly — it lands in `git log` but never appears in the release notes for `CHANGELOG.md` or the GitHub release body.


**Visible in changelog:** `feat:`, `fix:`, `perf:`. The `!` marker on any of these (e.g. `feat!:`, `fix!:`) also triggers a major version bump and adds a `BREAKING CHANGES` section.


**Filtered (invisible by default):** `refactor:`, `chore:`, `docs:`, `test:`, `style:`, `build:`, `ci:`, `revert:`. release-please drops these from the rendered changelog even though they still contribute to the diff between versions.


**Implication: choose the commit type by user-visible impact, not by code-shape.**


- New behavior, new commands, new artifacts → `feat:`
- Bug fix → `fix:`
- Breaking change of any kind (removed command, renamed flag, schema migration, behavior incompatibility) → `fix!:` (preferred) or `feat!:`. The `!` is what makes it visible AND bumps semver to major.
- Pure code restructure that ships zero user-visible delta → `refactor:` is honest but accept it will not appear in the release log
- Non-user-visible cleanup (lint, formatting, doc-only updates that don't ship) → `chore:` / `style:` / `docs:` as appropriate, accept invisibility


**When bundling many concerns into one commit, the type applies to the whole commit.** A single commit that adds a feature AND breaks a contract AND does cleanup must be labeled by the highest-impact concern. Default: if the commit removes/renames anything user-touchable, use `fix!:`. If it adds new behavior without breaking anything, use `feat:`. Never `refactor:` for a commit that ships new commands, new agents, or new skills — that work disappears from the changelog.


**Real example from this repo.** Commit `55d98a7 refactor: collapse signals/voice/axiom-2 architecture, add /atomic-improve and /gather-evidence` shipped 94 files including two new commands, one removed command (`/atomic-compress`), a removed skill (`atomic-signals` → consumed by an agent), and a renamed verb (`/initialize-signals` → `/refresh-signals`). All of it was invisible in v1.10.0's changelog because the `refactor:` prefix was filtered. The fix required a history rewrite to relabel the commits. Avoid this by labeling at commit-write time.


When the release-please **branch or PR CI** breaks (stale-based branch re-failing things already fixed on main, missing changelog work, drift gates), use the `atomic-release-ci` skill — it encodes the diagnosis and the per-cause fix. See "Contributor-only skills" below.


## Contributor-only skills


These live under `.claude/skills/`, auto-load for sessions in this repo, and are **never bundled or installed** (`atomic/internal/bundlemirror/mirror.go` ships only `context/skills/atomic-*/`). Each needs an explicit negation pair in `.gitignore` (the `.claude/skills/*` line ignores the dir by default).


| Skill | Fires on | Purpose |
|-------|----------|---------|
| `atomic-cli-contrib` | "add a CLI subcommand", "add a doctor check", "render templates", "edit commands/" | Conventions for editing the `atomic` Go CLI and command artifacts (prompt layer, testable seams, render/bundle pipeline). |
| `atomic-release-ci` | "release-please CI is failing", "release branch is out of date", "release PR is red", "fix the release CI" | Diagnose + fix broken release-please branch/PR CI. Cross-references the release-please commit-type rules above (cause 3 = commit-type mislabel). |


## Contributor-only commands


These live under `.claude/commands/`, load as project-scoped slash commands for sessions in this repo, and are **never bundled or installed** — they are not under `context/` and never go through `make bundle`. Each needs an explicit negation pair in `.gitignore` (the `.claude/commands/*` line ignores the dir by default; siblings there are broken symlinks to the user's global install and stay ignored). Because they are repo-local, the global artifact checklist (CLAUDE.md registry, `/atomic-help` router, README, docs, signals) does **not** apply — the command file's own frontmatter description is the discovery surface, and the harness lists it in the slash menu.


| Command | Purpose |
|---------|---------|
| `/triage-issues` | GitHub issue triage for this repo: suggest labels per issue (you approve), and a two-stage staleness lifecycle on issues waiting on the reporter — nudge (@-mention + `stale` label) at 14 days idle, close at ~30. Deterministic staleness math in `gh`+`jq`; label suggestion + comment drafting are the model's judgment; nothing applied without explicit OK. Labels `windows` / `research` / `stale` were created on `damusix/atomic-claude` to support it. |


## Naming

- All custom artifacts use the `atomic-` prefix (`atomic-implementer`, `atomic-tdd`, `atomic-git-discipline`, etc.) so they're easy to spot among third-party installs.
- Slash commands are imperative verbs (`/commit`, `/undo-commit`, `/atomic-plan`).


## Install (for this repo's artifacts)

Install like any user: `curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash` fetches the binary, then `atomic claude install` writes the artifact bundle into `~/.claude/`. To test local artifact changes, build the dev binary (`make -C atomic build`, outputs `bin/atomic`) and run `bin/atomic claude install`. Decision record: `docs/research/install-sh-decision.md` (issue #127 — install.sh stays; the old manual-copy guidance is retired).

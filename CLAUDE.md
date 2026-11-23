# CLAUDE.md

Project-local context for working **on** this repo. Not copied anywhere — read by Claude only when the cwd is this repo. The contract that ships to users is `context/AGENTS.md`, a different file.


## What this repo is

A holistic Claude Code configuration. Everything that installs lives under `context/`: `AGENTS.md`, `commands/`, `agents/`, `skills/`, `rules/`, `output-styles/`. They are designed as one coherent system — atomic output style, an opinionated command set, a small subagent roster, and discipline skills that interlock. Not a grab-bag; everything is meant to compose.

Replaces (for the author) heavier toolkits like superpowers and caveman. Personal config, no stability guarantee.


## Platform support

Target macOS and Linux only. Drop Windows-specific review findings, Windows-only test paths (`os.PathSeparator` probes), and Windows compatibility gates. `.goreleaser.yaml` may still produce Windows binaries — that's fine, but correctness on Windows is not a concern.


## File roles (this repo specifically)

| File | Role | Destination |
|------|------|-------------|
| `context/AGENTS.md` | Single source of truth. The sole authored global contract, wrapped in the `<atomic>` managed block. It carries a whole-line `@~/.atomic/profile.md` import, which the OMP projection keeps because OMP expands it natively. The Claude adapter renders the source directly into `~/.claude/CLAUDE.md`; other harnesses project it natively. Since it no longer sits at the repo root, Claude Code does not auto-load it here — the `@context/AGENTS.md` ref below pulls it in. Not to be confused with the root `CLAUDE.md`, which is the row below. | `context/`, committed → `~/.claude/CLAUDE.md` on install |
| `CLAUDE.md` (this file) | Project-local overlay for this repo *only*. Build pipeline rules, doc reference paths, mandatory checklist, design axioms. Auto-loads at the repo root, and pulls in `context/AGENTS.md` by `@`-ref. Do NOT duplicate `context/AGENTS.md` content here — both load into context, duplication = wasted tokens. | Repo root, committed, never installed. |
| `README.md` | Human-facing overview of what the config does and how to install it. | Repo root, committed. |
| `context/commands/*.md` | Slash command definitions, committed in source form. May carry `{{ template "<flow>" . }}` directives resolved against `context/_partials/`; expansion happens on the way into the embedded bundle, never back into `context/`. Copied to `~/.claude/commands/` by `atomic claude install`. | `~/.claude/commands/` |
| `context/agents/*.md` | Subagent definitions, same contract as commands. Every agent composes at least `agent-atomic-voice`. | `~/.claude/agents/` |
| `context/_partials/<name>.md` | Reusable blocks composed by command AND agent sources via `{{ template "<name>" . }}`. One shared pool — a partial defined once is callable from either kind. Never installs: `templaterender.LoadPartials` reads it as a pool of `{{ define }}` blocks, and artifact enumeration (`artifacts.Load` in [`catalog.go`](atomic/internal/artifacts/catalog.go)) never walks it as an artifact source. The full inventory and the per-artifact composition tables live in `docs/wiki/bundle.md` — read them there rather than keeping a second copy here, which is what let this row drift. One rule that is not derivable from the files themselves: `agent-yagni` is kept **verbatim in sync** with the Simplicity-first (YAGNI) ladder in `context/AGENTS.md`'s Principles block; nothing enforces the match. | Not copied; consumed at build time. |
| `context/skills/*/SKILL.md` | Discipline skills. Copied to `~/.claude/skills/`. | `~/.claude/skills/` |
| `context/output-styles/*.md` | Output style definitions. Copied to `~/.claude/output-styles/`. | `~/.claude/output-styles/` |
| `context/rules/<topic>/*.md` | **Shipped** path-scoped topic rules. `paths:` frontmatter globs (e.g. `**/*.{ts,tsx}`, `docs/spec/**`) so the rule only loads when Claude touches a matching file — auto-loads into subagents too (verified). Currently: `typescript/`, `python/` (language style), `specs/spec-currency.md` (spec body-is-truth, globs `docs/spec/**` + `docs/design/**`). | `~/.claude/rules/` (via `atomic claude install`) |
| `.claude/rules/<topic>/*.md` | **Repo-only** path-scoped rules — committed (gitignore-negated) but NOT bundled, so they never ship to users. `.claude/rules/authoring/` holds the contributor artifact-authoring references (`agent-config`, `prompting`, `claude-code-refs`, `axioms`) — glob the artifact sources (`context/**`) so they auto-load only when editing an artifact, instead of `@`-ref'ing every session. `.atomic/rules/wiki/<domain>.md` is `/refresh-wiki` output — one pointer card per wiki domain, pipeline-owned and regenerated every refresh; never hand-edit. Claude loads the cards through the committed `.claude/rules/atomic -> ../../.atomic/rules` symlink, as `.claude/rules/atomic/wiki/<domain>.md`. | Stays in repo; never installed. |


## Reference docs


**Artifact-authoring references** — frontmatter/dispatch semantics (`.claude/rules/authoring/agent-config.md`), Anthropic prompting patterns (`.claude/rules/authoring/prompting.md`), upstream Claude Code doc URLs (`.claude/rules/authoring/claude-code-refs.md`), and atomic's design axioms (`.claude/rules/authoring/axioms.md`) — are now **path-scoped rules**. They auto-load only when an artifact source is touched (anything under `context/`), in the main agent and in subagents, instead of `@`-ref'ing every session. Read before adding or editing any command / agent / skill / output-style / rule.


These stay auto-loaded every session (project-specific, compact):

### Global contract (auto-loaded)

`context/AGENTS.md` is the file the Claude adapter renders as every user's `~/.claude/CLAUDE.md`. It no longer sits at the repo root, so Claude Code will not pick it up on its own — this ref is what keeps the repo dogfooding its own contract.

@context/AGENTS.md


### Project signals (auto-loaded)

@docs/wiki/index.md


### Project follow-ups (auto-loaded)

@.atomic/project/followups/INDEX.md


## Coherence rules (when editing here)

- Treat the five artifact types (commands, agents, skills, output-styles, rules) as one system. A change to one often demands a matching change to the others.
- `context/AGENTS.md` is the global contract. Adding a command/agent/skill that other artifacts reference? Update `context/AGENTS.md` so every workspace knows it exists.
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
| 2 | `context/AGENTS.md` | Always — single source of truth | The sole authored global contract, wrapped in the `<atomic>` managed block and projected into each harness's native steering file on install (Claude's `~/.claude/CLAUDE.md`). This repo's project-local overlay is the root `CLAUDE.md` (row 3), a separate file. Agents and skills are surfaced by the harness each session (agent roster + skill trigger descriptions), so they need no registry section — keep each artifact's own description accurate instead. Commands go only into the `## Workflow` lifecycle ordering (the per-command catalog was removed; discovery is via the harness slash listing + `/atomic-help`); naming conventions cover output styles/rules. |
| 3 | `CLAUDE.md` (root, this file) | Only when the new artifact changes *project-local* conventions for this repo specifically (e.g. new bundle path, new build step, new file role) | Edit the relevant section. This file never installs. Do NOT duplicate the global registration here — the root file is for repo-specific overlays only, not for mirroring `context/AGENTS.md`. Both load into context when cwd is this repo, so duplication = wasted tokens. |
| 4 | `README.md` | Always — public-facing index | Add to the matching table in `docs/reference/commands.md` (or agents/skills equivalent). Keep one-line descriptions. |
| 5 | `docs/spec/<topic>.md` | If the artifact has non-trivial behavior or cross-references | Write or extend the spec. Required for anything dispatched by another artifact or that mutates state. **Amending an existing spec: see "Spec amendment rule" below — never silently overwrite the original.** |
| 6 | Cross-references in other artifacts | If this artifact invokes another or is invoked by one | Update the caller and the callee. |
| 7 | Discoverability | User-facing additions, removals, renames, or behavior changes | Update the authoritative reference. Update `/atomic-help` only if its routing or tour changes. |
| 8 | Bundle inclusion (`atomic/internal/bundlespec/bundlespec.go` + `atomic/internal/artifacts/catalog.go`) | Only if you introduce a **new artifact kind** (not a new file of an existing kind) | Add the predicate and its walk in `artifacts.Load`, plus a Claude target mapping in `atomic/internal/bundlemirror/mirror.go`. Existing kinds (`context/agents/`, `context/commands/`, `context/skills/`, `context/output-styles/`, `context/rules/`, and the singleton `context/AGENTS.md` steering source) auto-include matching files. |
| 9 | Doc impact and repo scan | Before final audit | Diff a scratch scan and the task's source changes, update affected docs, and accept the scan only after review. |

</mandatory_checklist>

**Verification before commit.** Grep for the new artifact name across the repo. Every place it is *referenced from* should also reference it *back* where appropriate. A skill mentioned only in its own SKILL.md is an invisible skill.


<build_pipeline>

## Embedded bundle: a build artifact, never committed


The `atomic` binary embeds `context/` at build time via `go:embed`. `go:embed` cannot name a parent directory (`../../../context` is a pattern-syntax error) and will not follow a symlink (`cannot embed irregular file`), so the tree is mirrored into `atomic/internal/embedded/bundle/` before it can be embedded.


**That mirror, and the `manifest.go` beside it, are gitignored.** They are outputs, not sources. Nothing that produces a shipped binary depends on them being in git: `make build`, `make test`, and `make vet` all declare the `bundle` target as a prerequisite; CI runs `go generate ./...`; goreleaser runs the same in its `before` hook. There is no bundle drift gate, because there is nothing committed to drift from.


**How to regenerate.** From the repo root: `make -C atomic bundle`. Nothing to stage afterwards. The `atomic serve` React app follows the same rule: `make -C atomic frontend` builds `atomic/internal/serve/frontend/dist/`, also gitignored, also a prerequisite of `build`/`test`/`vet`, also never staged.


**Failure mode to recognize.** A bare `go build` or `go test` on a fresh clone, bypassing `make`, fails with `pattern bundle: no matching files found` (or `pattern all:frontend/dist: no matching files found`). That is the mirror or the frontend build being absent, not a broken tree — run `make -C atomic bundle frontend`.


**Pre-commit hook.** `.githooks/pre-commit` (installed via `make hooks`, which sets `core.hooksPath=.githooks`) has one stage: `atomic followups render` when any followups entry file (other than INDEX.md) is staged, re-staging `INDEX.md` (degrades to WARN if `atomic` binary absent). There is no render, bundle, or frontend stage — none of those produces a committed file.


**`atomic hooks` vs git hooks — different systems.** `atomic hooks install` registers a Claude Code session-start hook (injects orientation and workflow preferences into context). That has nothing to do with the build pipeline. Render parity is enforced by CI; the git pre-commit hook in `.githooks/` is the local convenience layer.


## Shared partials


A command, agent, or the global steering source may compose a reusable block with `{{ template "<name>" . }}`, resolved against `context/_partials/`. Every expanding kind draws from one pool. Expansion happens inside `make bundle`, on the way into the embedded bundle — nothing is ever written back into `context/`, so an artifact exists in exactly one place and a partial edit reaches every consumer on the next build.


**Commands, agents, and the global steering source expand.** Skills, rules, and output styles are copied byte-for-byte, so a literal `{{` in their prose is safe. A directive naming a partial that does not exist fails the build rather than shipping an artifact with a hole in it.


**Adding an artifact is one file.** `context/commands/<verb>.md` or `context/agents/<name>.md` — there is no second location to keep in sync, and no orphan rule, because there is no separate output to orphan.


**Pipeline order.** Two generation steps, both gitignored outputs and both prerequisites of `make build|test|vet`: `make bundle` expands `context/` into `atomic/internal/embedded/`, and `make frontend` builds the `atomic serve` React app into `atomic/internal/serve/frontend/dist/`. `go generate ./...` runs both (CI and goreleaser use that form). Alongside them, `atomic followups render` regenerates `INDEX.md`, the one generated file that is committed. CI has no drift gate on the bundle or on `dist/`.


</build_pipeline>


## Spec amendment rule (`docs/spec/<topic>.md`)


The canonical spec-currency contract lives in **`context/rules/specs/spec-currency.md`** — a path-scoped rule that auto-loads (into the main agent *and* subagents) whenever a `docs/spec/**` or `docs/design/**` file is touched. It covers: body-is-current-truth, the `## Change log` discipline, the per-amendment rules (add / change-supersede / remove / correct / rename), and the change-log entry template. Read that rule rather than duplicating it here.

`/implement` passes the current spec to delegated writers. `/atomic-plan` and `/autopilot` brief spec writers to follow the currency rule.


## Cross-artifact wiring rules (mandatory for cohesion)


Installed workflows must keep project signals and docs current when shipping.


- **Preserve the accepted scan until docs pass review.** Follow `docs/spec/signals-refresh-timing.md` for implementation and ad-hoc commits. Keep realm capture separate.
- **Skills that are invoked by commands must declare it.** A skill's description should mention "invoked by /foo, /bar" so the trigger surface is inspectable. Reverse holds: a command that invokes a skill must name it in the command file. No silent dependencies.
- **Agents dispatched by commands must name the `subagent_type` in the command file**, and the agent's own `description` must carry an accurate when-to-use (that's what the harness surfaces in the session roster — there is no CLAUDE.md agent registry). Dispatch is a public contract.
- **When in doubt, write the spec first.** `docs/spec/<topic>.md` is the canonical source for any cross-artifact contract. If two artifacts reference the same flow and the spec doesn't exist, write it before adding the second reference.


Apply these shipping rules in user repositories as well as here.


## Help and discoverability


`/atomic-help` recommends one next action with one reason. The short tour runs only on request. Keep catalogs and exact syntax in their authoritative references, not in the router.

Before committing a user-facing artifact change, check its entry in the relevant commands, agents, skills, or configuration reference. For a CLI change, check the CLI help and its reference. For a rename or removal, remove stale pointers from those references and the router. Change the router only when a recommendation or the tour would otherwise be wrong.


## Signals `@-ref` must stay wired (in this repo: the root `CLAUDE.md`)


Only `docs/wiki/index.md` (the compact router) is `@-ref`'d. `docs/wiki/scan.md` is NOT — it can be thousands of lines on large repos and would blow up context. The inferrer reads it on demand; sessions do not need it. `docs/wiki/CLAUDE.md` (steering) is also NOT `@-ref`'d — it lazy-loads as nested memory whenever Claude reads a file under `docs/wiki/`, which is exactly when the inferrer operates.


**In this repo specifically**, the ref lives in the root `CLAUDE.md` (this file) — not in `context/AGENTS.md`. Reason: `context/AGENTS.md` is the bundle source (it installs as every user's global `~/.claude/CLAUDE.md`), so project-specific paths there would leak into every install. The root file never installs, and Claude Code auto-loads it when cwd is this repo. That's the correct home for the project-scoped `@`-ref.


- The `atomic-wiki-inferrer` agent checks for `@docs/wiki/index.md` in `claude.local.md` / `CLAUDE.local.md` first, then `CLAUDE.md`. If present in ANY of them, it skips wiring. The agent's search order is the contract.
- For most repos, the ref ends up in `CLAUDE.md` (one file, no separation) — which is also where it lives here, now that the shipped contract has moved to `context/AGENTS.md` and freed the root name.
- If you fork the layout (e.g. moving refs into a separate `@`-included file), update the agent's search order in lockstep.


## Documentation surfaces

| Path | Covers | Voice |
|------|--------|-------|
| `README.md` | project overview, install, commands, agents, skills | atomic-writing |
| `docs/guides/install.md` | installation, updating, uninstalling | atomic-writing |
| `docs/guides/getting-started.md` | first-session quickstart, output style, repo setup, first task, updating | atomic-writing |
| `docs/guides/knowledge-base.md` | karpathy realm, capture surfaces (research items with `01_raw/` stages, history captures, tickets), per-folder bucket indexes | atomic-writing |
| `docs/guides/contributing.md` | contributing, build pipeline, testing | atomic-writing |
| `docs/guides/evaluations.md` | Docker eval environment, testing setup | atomic-writing |
| `docs/guides/code-intel-mcp.md` | code-intel MCP server setup, .mcp.json, tools | atomic-writing |
| `docs/reference/workflow.md` | plan, implement, diagnose, ship lifecycle | atomic-writing |
| `docs/reference/commands.md` | command reference table | atomic-writing |
| `docs/reference/agents.md` | agent reference table | atomic-writing |
| `docs/reference/skills.md` | skills reference table | atomic-writing |
| `docs/reference/repo-wiki.md` | repo-scope wiki: scan, infer, wire pipeline, steering, `.signalsignore`, pointer rules | atomic-writing |
| `docs/reference/code-intel.md` | code-intel engine: verbs, index, lifecycle, workflow integration | atomic-writing |
| `docs/reference/output-style.md` | atomic output style reference | atomic-writing |
| `docs/reference/concepts.md` | how atomic-claude fits together, the atomic binary, code intelligence flow | atomic-writing |
| `docs/reference/conventions.md` | atomic style scope, CLAUDE.md hygiene, commit bylines, scratchpad/tmp rules, worktree auto-detect | atomic-writing |
| `docs/reference/serve.md` | `atomic serve` usage, scope resolution, browsing wiki + code graph locally | atomic-writing |
| `docs/reference/realm-wiki.md` | realm-scope wiki: setup, repo states, registry, membership drift, capture buckets | atomic-writing |
| `docs/reference/bus.md` | `atomic bus` room model, addressed vs FYI, envelope, daemon lifecycle, exit codes, operator verbs | atomic-writing |
| `docs/reference/repl.md` | `atomic repl` persistent interpreter sessions, scope model, six verbs, exit codes, idle_timeout config | atomic-writing |
| `docs/reference/retro.md` | `atomic retro extract`: flags, defaults, output shape, file:line convention, dropped rows, exit codes | atomic-writing |
| `docs/reference/docs-index.md` | `atomic docs index`: `<bucket-docs>` region for any directory, `--check`, exit codes, frontmatter keys read | atomic-writing |
| `docs/reference/atomic-toml.md` | repo-scoped `.atomic/atomic.toml`: scope marker, code-index ignore globs, comments max_lines, repl idle_timeout, realm repos, steering mode, lenient load contract | atomic-writing |
| `docs/credits.md` | inspirations, prior-art credits | atomic-writing |
| `docs/index.md` | VitePress site homepage, feature highlights, tagline | atomic-writing |
| `context/AGENTS.md` | global contract, agent/command/skill registry | atomic-writing |

**Ownership:** where two pages could carry the same fact, the owner below carries it and the sibling points. A doc edit that would restate an owned fact on a sibling page becomes a link instead.

- `docs/guides/install.md` owns the update flags, the background staging mechanism, and the CLAUDE.md merge flow.
- `docs/guides/getting-started.md` owns the first-session walkthrough (output style, repo setup, first task); `docs/reference/workflow.md` §0 compresses and points.
- `docs/reference/concepts.md` owns harness detection and the state-path resolution order.
- `docs/reference/code-intel.md` owns index setup and the `atomic code` verbs; guides show one invocation and point.
- `docs/reference/atomic-toml.md` owns the `[scan]`, `[code]`, `[comments]`, `[repl]`, `[realm]`, and `[steering]` key semantics; `repo-wiki.md` and `code-intel.md` show their own table and point, and `realm-wiki.md` points.


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


**The package path in `release-please-config.json` must stay `"."`.** Release-please counts only commits that touch files under the package path. It was `"atomic"` for a long time and looked fine only because the embedded bundle mirror under `atomic/internal/embedded/` was committed, so every artifact commit touched `atomic/` as a side effect. Once that mirror became gitignored (2026-08-16), `feat:` commits to `context/`, `docs/`, or `marketing/` stopped counting and a release full of features was computed as a patch. The product is the whole repo — `context/` ships inside the binary — so the package is the repo root; `changelog-path` keeps the log at `atomic/CHANGELOG.md`.


**Visible in changelog:** `feat:`, `fix:`, `perf:`. The `!` marker on any of these (e.g. `feat!:`, `fix!:`) also triggers a major version bump and adds a `BREAKING CHANGES` section.


**Filtered (invisible by default):** `refactor:`, `chore:`, `docs:`, `test:`, `style:`, `build:`, `ci:`, `revert:`. release-please drops these from the rendered changelog even though they still contribute to the diff between versions.


**Implication: choose the commit type by user-visible impact, not by code-shape.**


- New behavior, new commands, new artifacts → `feat:`
- Bug fix → `fix:`
- Breaking change of any kind (removed command, renamed flag, schema migration, behavior incompatibility) → `fix!:` (preferred) or `feat!:`. The `!` is what makes it visible AND bumps semver to major.
- Pure code restructure that ships zero user-visible delta → `refactor:` is honest but accept it will not appear in the release log
- Non-user-visible cleanup (lint, formatting, doc-only updates that don't ship) → `chore:` / `style:` / `docs:` as appropriate, accept invisibility


**On a squash merge the PR title is the commit, so it carries the type.** Merging with `--merge` keeps every commit on the branch and release-please reads their subjects, which is why this repo's history of merge-commit PRs never had to think about titles. `gh pr merge --squash` inverts it: the PR title becomes the only subject on `next`, and the branch's own `feat:`/`fix:` lines survive only as body bullets that release-please does not parse. A prose title there ships the whole PR invisibly — no failure, no warning, just a release that omits it. PR #235 landed that way and contributed nothing to `6.7.0-next.1`; the repair was a force-push of `next` to rewrite the subject. Before squash-merging, check the title and `gh pr edit --title` it into Conventional Commit form.

**When bundling many concerns into one commit, the type applies to the whole commit.** A single commit that adds a feature AND breaks a contract AND does cleanup must be labeled by the highest-impact concern. Default: if the commit removes/renames anything user-touchable, use `fix!:`. If it adds new behavior without breaking anything, use `feat:`. Never `refactor:` for a commit that ships new commands, new agents, or new skills — that work disappears from the changelog.


**Real example from this repo.** Commit `55d98a7 refactor: collapse signals/voice/axiom-2 architecture, add /atomic-improve and /gather-evidence` shipped 94 files including two new commands, one removed command (`/atomic-compress`), a removed skill (`atomic-signals` → consumed by an agent), and a renamed verb (`/initialize-signals` → `/refresh-signals`). All of it was invisible in v1.10.0's changelog because the `refactor:` prefix was filtered. The fix required a history rewrite to relabel the commits. Avoid this by labeling at commit-write time.


When the release-please **branch or PR CI** breaks (stale-based branch re-failing things already fixed on main, missing changelog work, drift gates), use the `atomic-release-ci` skill — it encodes the diagnosis and the per-cause fix. See "Contributor-only skills" below.


## Contributor-only skills


These live under `.claude/skills/`, auto-load for sessions in this repo, and are **never bundled or installed** (`MatchesSkillDir` in `atomic/internal/bundlespec/bundlespec.go` admits only `context/skills/atomic-*/`, so `.claude/skills/` never enters the corpus). Each needs an explicit negation pair in `.gitignore` (the `.claude/skills/*` line ignores the dir by default).


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
- Slash commands are imperative verbs (`/implement`, `/atomic-plan`).


## Install (for this repo's artifacts)

Install like any user: `curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash` fetches the binary, then `atomic claude install` writes the artifact bundle into `~/.claude/`. To test local artifact changes, build the dev binary (`make -C atomic build`, outputs `bin/atomic`) and run `bin/atomic claude install`. Decision record: `docs/research/install-sh-decision.md` (issue #127 — install.sh stays; the old manual-copy guidance is retired).

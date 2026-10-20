# Project wikis


## Goal


Ship a cross-repository knowledge layer in one release: deterministic CLI verbs `atomic wiki scan [--root=<path>]` (scaffold + scan + classify + register) and `atomic wiki stale [--root=<path>]` (read-only membership-drift report); an `atomic signals scan --out <dir>` redirect so the no-signals explorer never writes into a source repo; and a `/refresh-wiki` command that reuses `atomic-wiki-inferrer` (wiki-output mode) to summarize no-signals repos and re-authors the existing summaries and concerns the user selects. Nothing nudges, marks, or fingerprints a realm wiki.

Design: `docs/design/wiki.md`.


## Non-goals


- A new agent. The no-signals explorer is `atomic-wiki-inferrer` in wiki mode.
- Copying signals into the wiki (pointer only; summarize only when absent).
- A CLI mechanism to attach out-of-tree repos (cross-realm links are LLM-curated via the registry).
- Monorepo sub-package detection (a repo is a `.git` boundary).
- An in-file change log in the wiki (the wiki repo's git history is the change log).
- `--full` self-containment (summarizing `indexed` repos).
- Any freshness tracking for a realm wiki: no session-start nudge, no ship-time marker, no fingerprints in artifact frontmatter.


## Success criteria


### `atomic wiki scan`

- [ ] `atomic wiki scan` with no flag scaffolds `./wiki/` from cwd; `--root=<path>` uses `<path>`. The positional slot is reserved for verbs (`scan`, `stale`); `--root` is a flag.
- [ ] Scaffold creates `wiki/index.md`, `wiki/README.md`, `wiki/repos/`, `wiki/concerns/`, and runs `git init` in `wiki/` (skipped if already a git repo).
- [ ] Members = directories with a `.git` entry (dir or worktree file) found by recursing the root, or only the roots matched by `[realm] repos` in `<root>/.claude/atomic.toml` when that list is non-empty; recursion stops at each repo boundary at or below the walk's starting directory (a declared root inside a member repo is still walked, unless another declared root contains it); the root itself is never a member even when it is a git repo; junk dirs (`node_modules`, `dist`, `build`, `target`, `vendor`, `.worktrees`, `tmp`, `.git`) and the `wiki/` dir are skipped. A bad `[realm] repos` entry warns on stderr and is skipped; the scan still exits 0. `scan` classifies by signals-presence only and never reads git `HEAD`.
- [ ] Each member is classified `indexed` (has `.claude/project/signals.md`), `summarized` (no signals, but a summary exists on disk at `wiki/repos/<name>.md` or `wiki/repos/<name>/` containing at least one `.md`), or `pending` (neither) and recorded in a `<wiki-scan>` block with `path`, `status`, optional `signals`/`summary`. The open tag carries `root` and `generated` (date from injected clock). Membership + status only — no fingerprints in the block.
- [ ] A member already recorded `summarized` whose summary file still exists keeps `summarized` on re-scan; otherwise status is re-derived (signals → `indexed`, then summary-on-disk → `summarized`, else `pending`). Disk discovery makes `summarized` reachable without a prior entry: `/refresh-wiki` writes summaries after its initial scan, and the next `atomic wiki scan` picks them up.
- [ ] Re-running regenerates ONLY the `<wiki-scan>` block; a diff of `index.md` outside the block and of every file under `repos/`/`concerns/` is empty.
- [ ] The `index.md` path is written to a `<wikis>` block in `~/.claude/CLAUDE.md`. Three insertion cases: block present (add line iff absent, dedup by normalized path), block absent (append after `</atomic>`, or EOF when none), file absent (create). A registry write never alters the `<atomic>` block (diff outside `<wikis>` is empty).
- [ ] `scan` declares `scope = "realm"` in `<root>/.claude/atomic.toml`, creating the file or inserting the key when absent. A file already declaring another scope, or one that fails to parse, is left byte-identical with a warning on stderr; the scan still registers the wiki, prints the handoff, and exits 0.
- [ ] If `<root>/wiki/` exists but `index.md` is absent or lacks a `<wiki-scan>` marker, `scan` refuses with a non-zero exit and a message naming the path.
- [ ] `scan` prints a stdout handoff: summary (`<N> repos · <M> indexed · <K> pending`), per-repo list (`<status> <path> [→ signals path]`), `NEXT STEPS` naming each `pending` repo. Labels stable (orientation for `/refresh-wiki`).
- [ ] `scan` writes a managed `## Members` linked section into `index.md` between `<!-- wiki-members:start -->` / `<!-- wiki-members:end -->` markers, spliced idempotently like the `<wiki-scan>` block (content outside the markers untouched): `indexed` → link to `../<repo>/.claude/project/signals.md`; `summarized` → link to the recorded summary path (`repos/<repo>.md`, or `repos/<repo>/` for a domain-split summary); `pending` → link to `../<repo>/`. The realm is browsable from `index.md` after a deterministic scan, no LLM pass required.

### `atomic wiki linkify`

- [ ] `atomic wiki linkify --root=<path>` is a user-runnable verb that renders path citations in wiki artifacts into file-relative markdown links: each `repos/<repo>(/<domain>).md` with base = `<realm>/<repo>` (repo read from the summary's `repo:` frontmatter); each `concerns/*.md` and the `index.md` narrative with base = realm root. Idempotent; fenced code blocks untouched; a `[text](path)` link is not an `@-ref`. Surfaced in `/atomic-help`.
- [ ] `/refresh-wiki` runs a closing `atomic wiki scan` and then `atomic wiki linkify` after summaries and concerns are written, before the disposition report and the wiki commit.

### `atomic signals scan --out`

- [ ] `atomic signals scan --out <dir>` writes the deterministic substrate to `<dir>` instead of `<root>/.claude/project/`; the scanned repo is never written to. Without `--out`, behavior is unchanged (existing signals tests stay green).

### `atomic wiki stale`

- [ ] `atomic wiki stale [--root=<path>]` is read-only and exits `0` no drift / `1` drift / `2` error.
- [ ] Reports membership/status drift only (re-walk vs the `<wiki-scan>` block). It reads no frontmatter, hashes no files, and runs no git.
- [ ] stdout uses literal line prefixes: `DRIFT added <path>`, `DRIFT removed <path>`, `DRIFT status <path> <old>→<new>`. Exit 1 iff any line emitted.

### `/refresh-wiki` + inferrer wiki mode

- [ ] `/refresh-wiki [root]` follows the realm flow in `docs/spec/realm-wiki-drop-refresh.md` (criterion 10): scan; the one-time bucket offer and placeholder fill (`docs/spec/wiki-buckets.md`); the numbered pending-repo `/refresh-wiki` offer (axiom 4), with unselected pending repos summarized by the inferrer in wiki mode; a numbered list of existing summaries and concerns from which only the selected ones are re-authored (`all`, `none` accepted); a closing `atomic wiki scan`; `atomic wiki linkify`; a per-artifact disposition (`NEW` / `RE-AUTHORED` / `SKIPPED (not selected)` / `PLACEHOLDER (unfilled)`); and an automatic commit in the wiki repo.
- [ ] `atomic-wiki-inferrer` wiki-output mode (caller-provided `target_repo` + `wiki_dir`): obtains the substrate via `atomic signals scan --out <tmp>` (never writing into `target_repo`), infers, writes the summary ONLY under `wiki_dir/repos/<repo>(/<domain>)`, skips @-ref wiring. Large repos domain-split; small repos single file. Reviewer-verified prompt constraints, not unit-tested.
- [ ] The inferrer's default (non-wiki) mode is unchanged: existing signals tests stay green; reviewer confirms default-mode steps unchanged except for the additive branch.
- [ ] If exactly one of `target_repo` / `wiki_dir` is supplied, the inferrer fails loud — refuses and names the missing arg rather than proceeding in default mode (prompt-level guard; the command always passes both, and no Go dispatch code exists, so reviewer-verified).

### Checklist + gates

- [ ] `make render` produces `commands/refresh-wiki.md` + re-renders `agents/atomic-wiki-inferrer.md`, no orphan; `make render && git diff --exit-code` clean.
- [ ] `make -C atomic bundle && git diff --exit-code` clean.
- [ ] `/atomic-help` discovers `/refresh-wiki` (topic row), the binary verbs `atomic wiki scan` / `atomic wiki stale` and `atomic signals scan --out` (cli topic), and the wiki feature (tour stage); help-coverage reports no `MISSING:`.
- [ ] Bundle-source `CLAUDE.md` documents the feature but ships NO live `<wikis>` entries (grep returns none).
- [ ] `go test ./...` green; `go vet ./...` clean; `gofmt -l .` empty (from `atomic/`).


## Approaches


Adapted from `docs/design/wiki.md`.

| # | Decision | Chosen |
|---|----------|--------|
| 1 | Registry | `<wikis>` block in `~/.claude/CLAUDE.md`, not `@-ref`'d |
| 2 | No-signals explorer | `atomic-wiki-inferrer` wiki mode, fed by `signals scan --out` (substrate outside repo), output into the wiki, no new agent |
| 3 | index.md | two-zone single file: `<wiki-scan>` block + narrative |
| 4 | Concern synthesis | `/refresh-wiki` orchestrator; per-repo summaries by the inferrer |
| 5 | Cross-realm refs | LLM-curated, linked via the registry — no CLI membership |
| 6 | Knowledge routing | pointer to in-repo signals; summary only when absent |
| 7 | Registry write | CLI splices `<wikis>` directly into CLAUDE.md |
| 8 | Freshness | none tracked; `atomic wiki stale` reports membership drift, and the user picks which artifacts to re-author |


## Recommendation


New `atomic/internal/wiki/` package mirroring `internal/signals/` (Options + injectable clock, idempotent body-compare write, shared repo-discovery walk for `scan`/`stale`; worktree-aware `os.Lstat` `.git` detection; block writes via the profile `## Environment` splice pattern). `atomic signals scan` gains `--out <dir>`. `/refresh-wiki` + the inferrer wiki mode are rendered artifacts; both auto-bundle.


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | **wiki core**: discovery walk, classify, scaffold (dirs + README + git init), `<wiki-scan>` block idempotent narrative-preserving write, `summarized`-preservation, collision refusal | `atomic/internal/wiki/*.go` + tests | `go test ./internal/wiki/...` on fixtures (repos w/ & w/o signals, nested non-repo, junk dirs, root-is-repo excluded); re-run narrative diff-empty; `summarized` survives; both collision sub-cases error naming the path |
| 2 | **registry + scan wiring**: `<wikis>` writer (3 cases + dedup + `<atomic>` untouched), `atomic wiki scan` dispatch + seam, stdout handoff | `atomic/internal/wiki/registry*.go`, `cmd/atomic/main.go` | `go test ./...`: idempotent registry, file/block creation cases, diff outside `<wikis>` empty, handoff with stable labels |
| 3 | **`atomic signals scan --out`**: `--out` redirect (substrate outside repo, default unchanged) | `atomic/internal/signals/`, `cmd/atomic/main.go` + tests | `go test ./...`: `--out` writes outside repo + repo untouched; no-flag default unchanged (existing signals tests green) |
| 4 | **`atomic wiki stale` membership drift**: re-walk vs the `<wiki-scan>` block; literal-prefix report + exit 0/1/2 | `atomic/internal/wiki/stale*.go`, `cmd/atomic/main.go` | `go test ./...`: no drift→0; added/removed member→`DRIFT added`/`DRIFT removed` + 1; pending→indexed flip→`DRIFT status`; missing wiki dir→2 |
| 7 | **inferrer wiki mode**: caller-context branch — substrate via `signals scan --out`, write to `wiki_dir/repos/<repo>(/<domain>)`, skip @-ref wiring, never touch target repo, fail-loud partial args | `context/agents/atomic-wiki-inferrer.md` | body has the wiki-mode branch (substrate redirect, wiki output, skip wiring, don't-touch-target, fail-loud); `go test ./...` stays green; reviewer confirms default-mode unchanged |
| 8 | **/refresh-wiki command**: scan, numbered pending-repo offer, inferrer dispatch, numbered re-author selection, closing scan, linkify, disposition output, automatic wiki commit | `context/commands/refresh-wiki.md` | `description` present; body names `atomic wiki scan`, both numbered selections, inferrer wiki-mode dispatch, linkify, disposition output, the wiki commit; an unselected artifact is reported `SKIPPED (not selected)` and left unmodified |
| 9 | **contracts + discovery + docs**: `CLAUDE.md` feature + `<wikis>` contract + inferrer role; `/atomic-help` rows (`/refresh-wiki`, `atomic wiki scan`/`stale`, `signals scan --out`) + tour; merger-template preserve note; `README.md`; `docs/reference/commands.md` | `CLAUDE.md`, `templates/commands/atomic-help.md`→render, `templates/agents/atomic-claude-merger.md`→render, `README.md`, `docs/reference/commands.md` | help-coverage no `MISSING:`; `grep` for `/refresh-wiki`, `atomic wiki scan`, and `atomic wiki stale` in the help template all hit; no live `<wikis>` path in `CLAUDE.md`; `npm run docs:build` clean |
| 10 | **bundle + signals** | `atomic/internal/embedded/**`, `.claude/project/signals*` | `make -C atomic bundle && git diff --exit-code` clean; `/refresh-wiki` → `signals.md` lists a `wiki` domain |


## Agent roles


Three agent types run a refresh, and which type authors a page is a contract rather than a convenience, because the harness has no way to enforce it.

| Agent | Role | Why this type |
|-------|------|---------------|
| `atomic-wiki-inferrer` | Orchestrates: scan, classify, dispatch, assemble the router, wire the `@`-ref. Authors no page. | Holds `Agent`. Its isolated context keeps `scan.md`, which runs to thousands of lines, out of the caller's. |
| `atomic-wiki-writer` | Authors one page per dispatch, from the source paths it is given. | Declares `skills: [atomic-writing]`, so the reading order, the diagram rules, and the voice load as context. Holds no `Agent`, so it cannot fan out. |
| `atomic-reviewer` | Gates each written page against the source and the page shape. | Already the repo's review gate; `skills:` carries `atomic-writing` for the shape checks. |

**Every dispatch names its `subagent_type` explicitly.** Frontmatter grants tools, not agent types: an agent holding `Agent` can spawn any registered type, and an omitted `subagent_type` falls back to `general-purpose`, which declares no `skills:` frontmatter. That fallback fails open — it produces a page that looks finished and skipped the contract entirely. Naming the type is the only guard available.

## Deterministic CLI contract


**Repo discovery (shared `scan`/`stale`).** Without a non-empty `[realm] repos` (next paragraph), recurse `<root>`'s children (never the root). Member iff `os.Lstat(<dir>/.git)` succeeds (dir or worktree file). On a member: record, don't descend. On a non-member: descend unless base name in the skip set or it's the `wiki/` dir. Sort for stable output.

**Declared roots (`[realm] repos`).** When `<root>/.claude/atomic.toml` sets a non-empty `[realm] repos`, discovery walks only the directories its entries match instead of `<root>`'s children; each matched directory is walked the same way, so a matched repo is itself a member. Each entry is a root-relative path expanded as a doublestar glob over the root; wildcards never descend into a symbolic link, and every hit that is or passes through one is skipped with a warning. A matched root nested in another is dropped, and a match that is `wiki/`, under it, or has a skip-set segment is dropped silently. A `"."` match walks the whole root. A matched directory inside a member repo is still a root unless another match contains it, so a repo nested below it becomes a member. Unset key, absent file, `repos = []`, or a read, parse, or decode error walk the whole root. Problems are warnings, never failures: an entry that matches nothing, is empty, is absolute or leaves the root, or is an invalid pattern is skipped; a hit that is a file, a symbolic link, a path through one, or unreadable is skipped, and the entry's directory hits are still walked; `repos = []`, a read, parse, or decode error, and an unknown `realm.*` key also warn. When every entry is skipped, there are no members. `scan` prints warnings as `atomic wiki scan: warning: <msg>` and `stale` as `atomic wiki stale: warning: <msg>`, both on stderr; neither changes stdout nor the exit code. A recorded member outside the declared roots is reported by `stale` as `DRIFT removed <path>`. User-facing semantics and the warning table: `docs/reference/atomic-toml.md`.

**Classification.** Precedence: prior `summarized` + summary file present → keep `summarized`; else `indexed` iff `<repo>/.claude/project/signals.md` exists; else `summarized` iff a summary exists on disk (`wiki/repos/<name>.md`, or `wiki/repos/<name>/` containing at least one `.md`, where `<name>` is the member's base name); else `pending`. Signals win over a leftover summary — a repo that graduates to signals stays `indexed`. `scan` never reads `HEAD`.

**`## Members` linked section.** Literal `<!-- wiki-members:start -->` / `<!-- wiki-members:end -->` boundary in `wiki/index.md`. One link per member, derived from its `<wiki-scan>` status: `indexed` → `../<repo>/.claude/project/signals.md`; `summarized` → the recorded `summary` path (`repos/<repo>.md` or `repos/<repo>/`); `pending` → `../<repo>/`. Spliced idempotently like the `<wiki-scan>` block; content outside the markers untouched. Written by `atomic wiki scan` so the realm is browsable after a deterministic pass, no LLM required.

**`atomic wiki linkify` (code).** Renders inline-code path citations that resolve on disk into file-relative markdown links. Bases per artifact: `repos/<repo>(/<domain>).md` → `<realm>/<repo>` (from the summary's `repo:` frontmatter); `concerns/*.md` and `index.md` narrative → realm root. Idempotent (skips already-linked tokens); never touches fenced code blocks or frontmatter. A `[text](path)` link is not an `@-ref`. User-runnable and surfaced in `/atomic-help`.

**`<wiki-scan>` block.** Literal `<wiki-scan ...>` / `</wiki-scan>` boundary. Open attrs `root`, `generated` (injected clock — no wall-clock reads). One `<repo .../>` per member: `path`, `status`, optional `signals`/`summary`. No fingerprints. Target `wiki/index.md`; idempotent in-place; content outside untouched. `generated` records the last scan; nothing reads it as a freshness signal.

**`<wikis>` block.** `~/.claude/CLAUDE.md`, literal `<wikis>`/`</wikis>`, each tag alone on its line. Every reader and the writer match the tags as whole lines only, because the installed `<atomic>` block names `<wikis>` inline in prose before the real block. One `- <abs index.md path>` per wiki. Present → add iff absent (dedup by normalized path = `filepath.Abs` then `filepath.Clean`, no symlink resolution); absent → append after `</atomic>` (or EOF); file absent → create. `<atomic>` never touched.

**`atomic signals scan --out <dir>`.** Writes the deterministic substrate to `<dir>` instead of `<root>/.claude/project/`. With `--out`, the scanned repo is never written to. Without it, unchanged.

**`atomic wiki stale`.** Read-only. Exit `0`/`1`/`2`. Per-finding literal prefix: `DRIFT added <path>`, `DRIFT removed <path>`, `DRIFT status <path> <old>→<new>`. Exit 1 iff any line. Missing `wiki/`, missing `index.md`, or no `<wiki-scan>` block → exit 2.

**Root collision.** `<root>/wiki/` exists but `index.md` absent or lacks the marker → refuse, non-zero exit, message names the path.


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Huge-tree recursion (home as root) | med | Stop at repo boundary + skip set (walk only; see `**` row) + start from children; README notes `root` is a project container. |
| `**` in `[realm] repos` reads the whole subtree | med | Accepted: glob expansion runs before the repo-boundary and skip-set filters; the reference recommends fixed-depth `*`. |
| `scan --out` regresses normal signals | med | `--out` is additive (default path unchanged); existing signals tests gate; checkpoint 3 covers it. |
| Inferrer (wiki mode) writes into target repo | low | Substrate goes to `--out` (outside repo); mode-scoped don't-touch rule; reviewer checks. No write path into the repo remains. |
| A summary or concern goes out of date unnoticed | med | Accepted: nothing tracks freshness. `/refresh-wiki` lists every existing artifact so the user can pick what to re-author. |
| `<wikis>` clobbered by `atomic claude update` | low | Auto-preserved outside `<atomic>`; checkpoint 9 notes it in the merger template. |
| Bundle-source `CLAUDE.md` ships a live `<wikis>` entry | low | Runtime-written to the installed CLAUDE.md only; checkpoint 9 grep-verifies. |
| Re-run drops `summarized` → `pending` | med | Re-classification preserves `summarized` when the summary exists; checkpoint 1 covers it. |


## Change log


### 2026-10-18 — Discovery honors `[realm] repos`

**What changed:** Repo discovery for `scan` and `stale` walks only the directories matched by `[realm] repos` in the realm root's `.claude/atomic.toml` when the list is non-empty, and reports bad entries as stderr warnings without failing. The discovery success criterion and the Deterministic CLI contract now describe the declared roots, and a Risks row records that `**` expansion reads the whole subtree below its prefix.

**Why:** Issue #300. A git repo inside a capture bucket or notes folder became a member, and a realm laid out as `agency/`, `experiments/`, and one folder per company could not be described.

**Superseded:** Discovery always recursed every child of the realm root.

### 2026-10-18 — Removed: realm wiki freshness tracking

**What changed:** `atomic wiki stale` reports membership drift only. `/refresh-wiki` re-authors only the existing summaries and concerns the user picks from a numbered list and commits the wiki automatically; the non-goal that ruled out auto-committing is dropped to match. `/refresh-wiki` no longer runs `atomic wiki stale`, because it would compare against the block its own scan had just rewritten. Scan no longer writes `wiki/.gitignore`. Checkpoints 5 and 6 (staleness primitives, forcing-function wiring) are gone; the remaining checkpoints keep their numbers so the implementation log still matches. Approaches row 8 now records that no freshness is tracked.

**Why:** Realm wikis no longer need refreshing; the session-start nudge repeated every session without producing refreshes.

**Removed:** `atomic wiki stamp` and `reflects_*` fingerprints, `STALE summary` / `STALE concern` lines, `wiki.CheckStaleness`, `atomic wiki mark-dirty`, the `.dirty` marker and its `.gitignore`, and the `.dirty` clear; the closing scan stays, now before linkify. See `docs/spec/realm-wiki-drop-refresh.md`.

### 2026-09-30 — Scan writes the realm scope marker

**What changed:** `atomic wiki scan` now declares `scope = "realm"` at the root through the same `EnsureScopeMarker` path `atomic wiki init --scope realm` uses. A root already marked with another scope, or a marker file that fails to parse, is warned about and left unchanged; the scan continues.

**Why:** Bug. Scan is the documented realm bootstrap, but only `wiki init --scope realm` wrote the marker and no artifact calls it, so realms set up through `/refresh-wiki` or the `atomic-wiki` skill had none. Two things broke without it. `atomic repl` takes realm membership only from the marker, so realm sessions were invisible from member repos. And `atomic repo init` at an unmarked, non-git realm root wrote `scope = "repo"` there, a marker that then outranks git for every member repo lacking its own; with the realm marker present, `repo init` refuses at the realm root instead. Scan warns rather than failing because it is a re-runnable bootstrap that `/refresh-wiki` stops on, and the marker is not required for anything else it does.


### 2026-09-18 — `<wikis>` reader matches whole-line tags

**What changed:** The `<wikis>` block contract now states that readers match the tags as whole lines, as the writer already did. `ReadWikiIndexPaths`, which backs `mark-dirty`, `CheckStaleness`, `atomic where`, the doctor, and the code-intel realm resolver, now uses the same line-anchored parser as `RegisterWiki`.

**Why:** Issue #263. The reader took the first substring match of `<wikis>`, which is the prose mention inside the installed `<atomic>` block. Every bullet between that line and the real `</wikis>` became a registry entry. `` `atomic serve`: ... `` has no `/`, so `filepath.Abs` resolved it against the process cwd, and `mark-dirty` treated cwd as a wiki directory and its parent as the realm root. Every ship verb then wrote `.dirty` into whatever repo it ran in and returned before reaching the real entries, so registered wikis were never marked.

**Correction:** The CP9 implementation-log line below says `<wikis>` detection was hardened. Only the writer was; the reader kept the substring scan until this change.

### 2026-08-16 — Page authoring moves from `general-purpose` to `atomic-wiki-writer`

**What changed:** A new `## Agent roles` section names the three agent types a refresh dispatches. Per-domain page authoring moves from `general-purpose` to the new `atomic-wiki-writer`, and `atomic-wiki-inferrer` is described as an orchestrator that authors nothing.

**Why:** `general-purpose` declares no `skills:` frontmatter, so the page contract and the voice rules reached the agent that actually wrote pages only as a request inside the dispatch prompt ("invoke the `atomic-writing` skill"), while the orchestrator that never wrote a page held the skill in frontmatter. The wiring was inverted. Frontmatter cannot restrict which types an agent spawns, and an omitted `subagent_type` falls back to `general-purpose`, so naming the type explicitly on every dispatch is the only available guard.

**Superseded:** the prior rationale in `skills/atomic-wiki/references/repo.md` Step 4, which chose `general-purpose` on the grounds that `atomic-implementer` is scoped to code. That reasoning was correct about `atomic-implementer` and wrong to conclude `general-purpose` was the remaining option; the gap it identified is what the new agent fills.

### 2026-06-12 — `/refresh-wiki` extended with bucket synthesis phase

**What changed:** The `/refresh-wiki` success criterion now references the extended bucket phase order (scan → stale → bucket offer → placeholder fill → repo summaries → bucket synthesis → concern re-auth → linkify → dirty clear → commit offer) and points to `docs/spec/wiki-buckets.md` for the full contract. Disposition output gains `SYNTHESIZED` and `PLACEHOLDER (unfilled)` states alongside the existing `NEW` / `RE-AUTHORED` / `SKIPPED (fresh)`.

**Why:** The bucket synthesis phase (CP6 of wiki-buckets) ships behavior — the spec body must describe the current state, not the prior incremental-only flow. Pointer to `docs/spec/wiki-buckets.md` avoids duplicating the detailed phase contract that lives there.

**Superseded:** "Refreshes INCREMENTALLY (scan → stale → repo summaries → concerns → linkify → dirty clear → commit offer) with disposition `NEW` / `RE-AUTHORED` / `SKIPPED (fresh)`."


### 2026-06-11 — Classification discovers summaries on disk

**What changed:** `classifyMembers` gained a disk-discovery rule: a member with no signals but a summary on disk (`wiki/repos/<name>.md`, or `wiki/repos/<name>/` containing at least one `.md`) is classified `summarized` with the `summary` attribute set. Precedence: prior-`summarized` preservation → signals (`indexed`) → summary-on-disk (`summarized`) → `pending`. `memberLinkTarget` now links `summarized` members to the recorded `summary` path, so domain-split summaries link to `repos/<repo>/` instead of a nonexistent `repos/<repo>.md`.

**Why:** Bug — `summarized` was unreachable. Nothing ever wrote the status initially: scan only *preserved* a prior `summarized` entry, and `/refresh-wiki`'s closing re-scan could not see freshly written summaries. Summarized repos stayed `pending` forever and the `## Members` section linked to the repo directory instead of the wiki page, defeating the point of summarizing.

**Superseded:** "Each member is classified `indexed` or `pending`; `summarized` only survives via preservation" — derivation is now three-way.


### 2026-06-06 — Link navigability: `## Members` section + `atomic wiki linkify`

**What changed:** Added two behaviors. (1) `atomic wiki scan` now writes a managed `## Members` linked section into `wiki/index.md` (markers `<!-- wiki-members:start -->` / `<!-- wiki-members:end -->`), spliced idempotently like the `<wiki-scan>` block: `indexed` → `../<repo>/.claude/project/signals.md`, `summarized` → `repos/<repo>.md`, `pending` → `../<repo>/`. The realm is browsable from `index.md` after a deterministic scan. (2) A new user-runnable verb `atomic wiki linkify --root=<path>` renders path citations in `repos/**` (base = each summary's `repo:` frontmatter dir), `concerns/*.md`, and the `index.md` narrative (base = realm root) into file-relative markdown links. `/refresh-wiki` runs it after stamping (Step 6), so it never disturbs `reflects_*` fingerprints or `atomic wiki stale` verdicts. Full contract: `docs/spec/signals-wiki-linkify.md`.

**Why:** Plain backtick paths are inert in Obsidian, a markdown server, or GitHub. Linkifying makes the realm navigable as a graph. Relative-prefix computation is a deterministic transform — code's job, not the model's. A `[text](path)` link is not an `@-ref`. `linkify` is user-runnable (surfaced in `/atomic-help`), unlike the internal `stamp` / `mark-dirty` helpers.


### 2026-06-06 — Conform checkpoint table to validator schema

**Correction:** The `## Checkpoints` table shipped with six columns (`# | Checkpoint | Files/areas | Agent | Est. | Verifies`), but `atomic validate spec` rule S5 requires the canonical four-column header `| # | Checkpoint | Files/areas | Verifies |`. CI caught it: `FAIL  S5   docs/spec/wiki.md:97  ## Checkpoints table header mismatch`. The spec was drafted from the `/atomic-plan` template (which emits the six-column form) and shipped without `atomic validate` having run, so the deviation slipped through. Dropped the `Agent` and `Est.` columns; the build was already complete, so those were historical dispatch hints, not contract. No checkpoint, file/area, or verification changed.


## Implementation log


### Shipped — 2026-06-06


Built across 10 checkpoints via `/autopilot` → the `/subagent-implementation` loop on branch `feat/project-wiki`. Commits (chronological):

- `71444e3` — CP1 wiki-core package (discovery, scaffold, `<wiki-scan>` block, collision, summarized-preservation)
- `4b59005` — CP2 `atomic wiki scan` + `<wikis>` registry writer
- `0d14f5c` — CP3 `signals scan --out` + `atomic wiki stamp` code fingerprinter
- `513f441` — CP4 `atomic wiki stale` comparator
- `ae3e6c5` — CP5 `CheckStaleness` + `atomic wiki mark-dirty` primitives
- `8895bdb` — CP6 forcing-function wiring (session-start nudge + ship-time `mark-dirty`)
- `ee85475` — CP7 inferrer wiki-output mode
- `acde091` — CP8 `/refresh-wiki` orchestration command
- `4228d30` — CP9 discovery + docs (CLAUDE.md, `/atomic-help`, merger note, README, docs/reference) + hardened `<wikis>` detection
- (this commit) — CP10 implementation log; bundle + render parity confirmed

**Out-of-scope work performed during the build:**
- Hardened `RegisterWiki` to line-anchored `<wikis>` detection (folded into CP9). The original substring scan would have false-matched the documentation mention of the literal tag in the installed `~/.claude/CLAUDE.md` and written the user's registry entry inside `<atomic>`, where `atomic claude update` clobbers it. Caught by the CP9 reviewer; fixed in-iteration with a regression test.
- Bundle regenerated per artifact-touching commit (CP6–CP9) rather than once at CP10, per the build-pipeline hard rule ("any commit touching a source artifact includes its regenerated bundle"). CP10 is therefore a parity confirmation, not a bundle commit.

**Unforeseens:**
- CP3 reviewer reported an "all cited ids unresolvable → crash" 🔴 whose described mechanism did not actually exist (a nil `[]any` still matches the `[]any` type-switch arm and emits an empty sequence). The defensive empty-slice initializer + an all-unresolvable regression test were applied regardless.
- Isolation used a feature-branch-checked-out-as-worktree rather than a fresh `/worktree-start`, because the spec/design/follow-up were uncommitted on `main` and a fresh worktree from HEAD would have stranded them.

**Deferred items still open:** none. The scratchpad FOLLOWUPS ledger is empty — every reviewer finding (blocking and non-blocking) was addressed in-iteration per autopilot.

**End-to-end verification:** built the binary and ran `atomic wiki scan` on a fixture (with `HOME` redirected to a temp dir, never touching the real `~/.claude`) — correct `N repos · M indexed · K pending` handoff, scaffold, `<wiki-scan>` block, and `<wikis>` registration; `atomic wiki stale` returned fresh (exit 0); junk dirs skipped. Full Go suite green; `go vet` + `gofmt` clean; render + bundle parity clean; `/atomic-help` coverage complete (zero `MISSING`); VitePress docs build clean.

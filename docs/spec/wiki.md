# Project wikis


## Goal


Ship a cross-repository knowledge layer in one release: deterministic CLI verbs `atomic wiki scan [--root=<path>]` (scaffold + scan + classify + register) and `atomic wiki stale [--root=<path>]` (read-only freshness verdict); an `atomic signals scan --out <dir>` redirect so the no-signals explorer never writes into a source repo; a `/refresh-wiki` command that reuses `atomic-signals-inferrer` (wiki-output mode) to summarize no-signals repos, synthesizes cross-cutting concerns, and refreshes incrementally; and a cheap forcing function (session-start neglect nudge + ship-time drift marker) so the wiki does not rot unnoticed. Fingerprints are written by code, never the model.

Design: `docs/design/wiki.md`.


## Non-goals


- A new agent. The no-signals explorer is `atomic-signals-inferrer` in wiki mode.
- Copying signals into the wiki (pointer only; summarize only when absent).
- A CLI mechanism to attach out-of-tree repos (cross-realm links are LLM-curated via the registry).
- Monorepo sub-package detection (a repo is a `.git` boundary).
- An in-file change log in the wiki (the wiki repo's git history is the change log).
- `--full` self-containment (summarizing `indexed` repos); auto-committing the wiki; git-per-repo staleness sweeps at session start.


## Success criteria


### `atomic wiki scan`

- [ ] `atomic wiki scan` with no flag scaffolds `./wiki/` from cwd; `--root=<path>` uses `<path>`. The positional slot is reserved for verbs (`scan`, `stale`); `--root` is a flag.
- [ ] Scaffold creates `wiki/index.md`, `wiki/README.md`, `wiki/repos/`, `wiki/concerns/`, `wiki/.gitignore` (ignoring the `.dirty` marker), and runs `git init` in `wiki/` (skipped if already a git repo).
- [ ] Members = directories with a `.git` entry (dir or worktree file) found by recursing the root; recursion stops at each repo boundary; the root itself is never a member even when it is a git repo; junk dirs (`node_modules`, `dist`, `build`, `target`, `vendor`, `.worktrees`, `tmp`, `.git`) and the `wiki/` dir are skipped. `scan` classifies by signals-presence only and never reads git `HEAD`.
- [ ] Each member is classified `indexed` (has `.claude/project/signals.md`), `summarized` (no signals, but a summary exists on disk at `wiki/repos/<name>.md` or `wiki/repos/<name>/` containing at least one `.md`), or `pending` (neither) and recorded in a `<wiki-scan>` block with `path`, `status`, optional `signals`/`summary`. The open tag carries `root` and `generated` (date from injected clock). Membership + status only — no fingerprints in the block.
- [ ] A member already recorded `summarized` whose summary file still exists keeps `summarized` on re-scan; otherwise status is re-derived (signals → `indexed`, then summary-on-disk → `summarized`, else `pending`). Disk discovery makes `summarized` reachable without a prior entry: `/refresh-wiki` writes summaries after its initial scan, and the closing re-scan picks them up.
- [ ] Re-running regenerates ONLY the `<wiki-scan>` block; a diff of `index.md` outside the block and of every file under `repos/`/`concerns/` is empty.
- [ ] The `index.md` path is written to a `<wikis>` block in `~/.claude/CLAUDE.md`. Three insertion cases: block present (add line iff absent, dedup by normalized path), block absent (append after `</atomic>`, or EOF when none), file absent (create). A registry write never alters the `<atomic>` block (diff outside `<wikis>` is empty).
- [ ] If `<root>/wiki/` exists but `index.md` is absent or lacks a `<wiki-scan>` marker, `scan` refuses with a non-zero exit and a message naming the path.
- [ ] `scan` prints a stdout handoff: summary (`<N> repos · <M> indexed · <K> pending`), per-repo list (`<status> <path> [→ signals path]`), `NEXT STEPS` naming each `pending` repo. Labels stable (orientation for `/refresh-wiki`; the incremental pass is driven by `atomic wiki stale`).
- [ ] `scan` writes a managed `## Members` linked section into `index.md` between `<!-- wiki-members:start -->` / `<!-- wiki-members:end -->` markers, spliced idempotently like the `<wiki-scan>` block (content outside the markers untouched): `indexed` → link to `../<repo>/.claude/project/signals.md`; `summarized` → link to the recorded summary path (`repos/<repo>.md`, or `repos/<repo>/` for a domain-split summary); `pending` → link to `../<repo>/`. The realm is browsable from `index.md` after a deterministic scan, no LLM pass required.

### `atomic wiki linkify`

- [ ] `atomic wiki linkify --root=<path>` is a user-runnable verb that renders path citations in wiki artifacts into file-relative markdown links: each `repos/<repo>(/<domain>).md` with base = `<realm>/<repo>` (repo read from the summary's `repo:` frontmatter); each `concerns/*.md` and the `index.md` narrative with base = realm root. Idempotent; fenced code blocks untouched; a `[text](path)` link is not an `@-ref`. (Unlike `stamp`/`mark-dirty`, which are internal, `linkify` is surfaced in `/atomic-help`.)
- [ ] `/refresh-wiki` runs `atomic wiki linkify` after summaries/concerns are (re)authored and stamped, before the `.dirty`-clear and commit offer. Running after `atomic wiki stamp` means it never alters `reflects_*` frontmatter, so it does not affect `atomic wiki stale` verdicts (staleness is HEAD/hash-based, not body-based).

### `atomic signals scan --out` + code fingerprint stamp

- [ ] `atomic signals scan --out <dir>` writes the deterministic substrate to `<dir>` instead of `<root>/.claude/project/`; the scanned repo is never written to. Without `--out`, behavior is unchanged (existing signals tests stay green).
- [ ] `atomic wiki stamp` (a CLI helper) computes `git rev-parse HEAD` and writes/updates `reflects_rev` in a summary's YAML frontmatter. For a concern, the command passes the cited-repo ids it got from the model as args (e.g. `--cites repoA,repoB`); the helper resolves each to a fingerprint (HEAD SHA for `summarized` repos, `.claude/project/signals.md` content hash for `indexed`) and writes the `reflects:` list. The model supplies which repos are cited; code computes and writes every fingerprint value. An unresolvable cited repo is skipped, not crashed on. (`stamp` and `mark-dirty` are internal helpers invoked by `/refresh-wiki` and the ship flow — not surfaced in `/atomic-help`.)

### `atomic wiki stale`

- [ ] `atomic wiki stale [--root=<path>]` is read-only and exits `0` fresh / `1` stale / `2` error, mirroring `atomic signals stale`.
- [ ] Reports membership/status drift (re-walk vs the block) AND per-artifact content drift: for each `repos/<repo>(/<domain>).md`, current `git rev-parse HEAD` vs `reflects_rev`; for each `concerns/<concern>.md`, each `reflects:` entry vs the referenced repo's current fingerprint.
- [ ] A missing/unparseable `reflects_*` counts as stale (fail-safe) — never a panic or exit 2. A repo with no commits (no `HEAD`) is always-needs-summary, not an error.
- [ ] stdout uses literal line prefixes: `DRIFT added <path>`, `DRIFT removed <path>`, `DRIFT status <path> <old>→<new>`, `STALE summary <path>`, `STALE concern <path> (<repo>)`. Exit 1 iff any line emitted.

### Forcing function

- [ ] **Neglect (hook).** A cheap `wiki.CheckStaleness` runs at session start: parse `<wikis>` from `~/.claude/CLAUDE.md`, and per wiki read the `<wiki-scan>` `generated` date and stat the `.dirty` marker — stats + small reads, ZERO git spawns. `CheckStaleness` takes an injected exec runner + clock (the same seam pattern as `internal/signals/`) so a test asserts it spawns no git. Emit a one-line nudge per wiki where age > threshold OR marker present. Threshold defaults to 30 days, read from memory (axiom 2 — a conversational nudge; graduate to config only if it must be shell-settable). Wired best-effort into the session-start hook (like profile refresh — never blocks or fails the session).
- [ ] **Drift (ship boundary).** `atomic wiki mark-dirty` (a CLI helper) reads `<wikis>`, checks whether cwd is under any registered wiki root (normalized path-prefix, no git), and if so `touch`es that wiki's `.dirty`. The shared `signals-gate` partial — composed by every ship verb — invokes it after the signals refresh. A no-op when cwd is under no registered root, so it's cheap on every ship.
- [ ] **Heal/clear.** `/refresh-wiki` clears `.dirty` and re-runs `atomic wiki scan` (bumping `generated`) only after a fully completed refresh (scan + stale + incremental all succeed); an aborted or partial run leaves `.dirty` set so the nudge persists.

### `/refresh-wiki` + inferrer wiki mode

- [ ] `/refresh-wiki [root]` runs `atomic wiki scan`, then `atomic wiki stale`, then refreshes INCREMENTALLY following the extended phase order: (1) scan, (2) stale check, (3) one-time bucket creation offer (fires when no `<wiki-buckets>` block exists; decline recorded as `declined="true"` attribute), (4) placeholder fill for new buckets, (5) stale check result parse, (6) pending-repo `/refresh-signals` offer, (7) repo summaries (pending/stale repos via inferrer wiki mode + stamp), (8) bucket synthesis (non-empty-diff buckets via inferrer bucket-synthesis mode + stamp knowledge pages + promote on success), (9) stale concern re-auth + stamp, (10) `atomic wiki linkify`, (11) disposition output, (12) conditional `.dirty` clear + `generated` bump, (13) commit offer. (The count of 13 phases includes all judgment/offer surfaces — bucket offer, placeholder fill, pending-repo offer, and disposition — not only data-transformation phases. For the data-phase-only ordering, see `docs/spec/wiki-buckets.md` § `/refresh-wiki` phase order.) Full bucket phase contract: `docs/spec/wiki-buckets.md` (§ `/refresh-wiki` bucket integration + Deterministic CLI contract §`/refresh-wiki` phase order). Pending repos presented as numbered list (axiom 4); unselected pending repos go to inferrer wiki mode. It re-authors only the stale-flagged/pending artifacts and preserves the rest; prints a per-artifact disposition (`NEW` / `RE-AUTHORED` / `SKIPPED (fresh)` / `SYNTHESIZED` / `PLACEHOLDER (unfilled)`); clears `.dirty` only on full completion.
- [ ] `atomic-signals-inferrer` wiki-output mode (caller-provided `target_repo` + `wiki_dir`): obtains the substrate via `atomic signals scan --out <tmp>` (never writing into `target_repo`), infers, writes the summary ONLY under `wiki_dir/repos/<repo>(/<domain>)`, skips @-ref wiring. It does NOT write the fingerprint — the code stamp step does. Large repos domain-split; small repos single file. Reviewer-verified prompt constraints, not unit-tested.
- [ ] The inferrer's default (non-wiki) mode is unchanged: existing signals tests stay green; reviewer confirms default-mode steps unchanged except for the additive branch.
- [ ] If exactly one of `target_repo` / `wiki_dir` is supplied, the inferrer fails loud — refuses and names the missing arg rather than proceeding in default mode (prompt-level guard; the command always passes both, and no Go dispatch code exists, so reviewer-verified).

### Checklist + gates

- [ ] `make render` produces `commands/refresh-wiki.md` + re-renders `agents/atomic-signals-inferrer.md`, no orphan; `make render && git diff --exit-code` clean.
- [ ] `make -C atomic bundle && git diff --exit-code` clean.
- [ ] `/atomic-help` discovers `/refresh-wiki` (topic row), the binary verbs `atomic wiki scan` / `atomic wiki stale` and `atomic signals scan --out` (cli topic), and the wiki feature (tour stage); help-coverage reports no `MISSING:`.
- [ ] Bundle-source `CLAUDE.md` documents the feature but ships NO live `<wikis>` entries (grep returns none).
- [ ] `go test ./...` green; `go vet ./...` clean; `gofmt -l .` empty (from `atomic/`).


## Approaches


Copied from `docs/design/wiki.md`.

| # | Decision | Chosen |
|---|----------|--------|
| 1 | Registry | `<wikis>` block in `~/.claude/CLAUDE.md`, not `@-ref`'d |
| 2 | No-signals explorer | `atomic-signals-inferrer` wiki mode, fed by `signals scan --out` (substrate outside repo), output into the wiki, no new agent |
| 3 | index.md | two-zone single file: `<wiki-scan>` block + narrative |
| 4 | Concern synthesis | `/refresh-wiki` orchestrator; per-repo summaries by the inferrer |
| 5 | Cross-realm refs | LLM-curated, linked via the registry — no CLI membership |
| 6 | Knowledge routing | pointer to in-repo signals; summary only when absent |
| 7 | Registry write | CLI splices `<wikis>` directly into CLAUDE.md |
| 8 | Fingerprint store | artifact frontmatter, written by CODE (not the block, not the model) |
| 9 | Staleness detection | deterministic comparator; model only re-judges flagged concerns |
| 10 | Forcing function | cheap neglect timer (hook) + cheap drift marker (ship boundary) → one self-clearing nudge |


## Recommendation


New `atomic/internal/wiki/` package mirroring `internal/signals/` (Options + injectable clock, idempotent body-compare write, shared repo-discovery walk for `scan`/`stale`; worktree-aware `os.Lstat` `.git` detection; block writes via the profile `## Environment` splice pattern). `atomic signals scan` gains `--out <dir>`. A deterministic stamp helper writes `reflects_*` from `git rev-parse HEAD` / signals-hash. `wiki.CheckStaleness(claudeHome)` (no git) backs the session-start nudge; a cheap path-prefix marker step backs ship-time drift. `/refresh-wiki` + the inferrer wiki mode are rendered artifacts; both auto-bundle.


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | **wiki core**: discovery walk, classify, scaffold (dirs + README + `.gitignore` + git init), `<wiki-scan>` block idempotent narrative-preserving write, `summarized`-preservation, collision refusal | `atomic/internal/wiki/*.go` + tests | `go test ./internal/wiki/...` on fixtures (repos w/ & w/o signals, nested non-repo, junk dirs, root-is-repo excluded); re-run narrative diff-empty; `summarized` survives; both collision sub-cases error naming the path |
| 2 | **registry + scan wiring**: `<wikis>` writer (3 cases + dedup + `<atomic>` untouched), `atomic wiki scan` dispatch + seam, stdout handoff | `atomic/internal/wiki/registry*.go`, `cmd/atomic/main.go` | `go test ./...`: idempotent registry, file/block creation cases, diff outside `<wikis>` empty, handoff with stable labels |
| 3 | **`atomic signals scan --out` + `atomic wiki stamp`**: `--out` redirect (substrate outside repo, default unchanged); `stamp` CLI writes `reflects_*` (HEAD SHA / signals-hash; concern cited-repos passed as `--cites` and resolved by code) | `atomic/internal/signals/`, `atomic/internal/wiki/stamp*.go`, `cmd/atomic/main.go` + tests | `go test ./...`: `--out` writes outside repo + repo untouched; no-flag default unchanged (existing signals tests green); `stamp` writes correct `reflects_rev`/`reflects`; unresolvable cited repo skipped not crashed |
| 4 | **`atomic wiki stale` comparator**: membership/status diff; parse `reflects_*`; compute current fingerprints; literal-prefix report + exit 0/1/2 | `atomic/internal/wiki/stale*.go`, `cmd/atomic/main.go` | `go test ./...`: fresh→0; moved HEAD→1 naming stale summary; signals.md changed (HEAD unchanged)→citing concern stale (content-hash path); pending→indexed flip→status drift; missing/garbled `reflects_*`→stale; no-`HEAD`→handled; error→2; literal `DRIFT`/`STALE` prefixes |
| 5 | **staleness primitives**: `wiki.CheckStaleness` (injected runner+clock seam; reads `generated`-age + `.dirty`, no git) + `atomic wiki mark-dirty` CLI (path-prefix check → touch `.dirty`) | `atomic/internal/wiki/staleness*.go`, `cmd/atomic/main.go` + tests | `go test ./...`: `CheckStaleness` nudges on old `generated` OR marker, silent when fresh, spawns no git (asserted via the injected runner); `mark-dirty` touches `.dirty` only when cwd under a registered root, no-op otherwise |
| 6 | **forcing-function wiring**: session-start hook calls `CheckStaleness` best-effort (never errors the session); the shared `signals-gate` partial calls `atomic wiki mark-dirty` after the signals refresh | `atomic/internal/hooks/`, `templates/shared/signals-gate.md` → render | `go test ./internal/hooks/...` (hook swallows wiki errors, stays best-effort); `make render` clean; `signals-gate` invokes `atomic wiki mark-dirty`; ship verbs inherit it |
| 7 | **inferrer wiki mode**: caller-context branch — substrate via `signals scan --out`, write to `wiki_dir/repos/<repo>(/<domain>)`, skip @-ref wiring, never touch target repo, defer SHA-stamping to code, fail-loud partial args | `templates/agents/atomic-signals-inferrer.md` → render | `make render` no orphan + diff clean; body has the wiki-mode branch (substrate redirect, wiki output, skip wiring, don't-touch-target, defers stamping to code, fail-loud); `go test ./...` stays green; reviewer confirms default-mode unchanged |
| 8 | **/refresh-wiki command**: scan→stale→incremental (re-author flagged/pending, preserve fresh), numbered `/refresh-signals` offer, inferrer dispatch, `atomic wiki stamp` invocation, disposition output, `.dirty` clear (only on full completion), commit offer | `templates/commands/refresh-wiki.md` → render | `make render` no orphan + diff clean; `description` present; body names `atomic wiki scan`/`stale`, the numbered offer, inferrer wiki-mode dispatch, `atomic wiki stamp`, incremental skip-fresh, disposition output, conditional `.dirty` clear, commit offer; det/judgment halves fenced; manual fixture run shows a fresh artifact `SKIPPED (fresh)` and unmodified |
| 9 | **contracts + discovery + docs**: `CLAUDE.md` feature + `<wikis>` contract + forcing function + inferrer role; `/atomic-help` rows (`/refresh-wiki`, `atomic wiki scan`/`stale`, `signals scan --out`) + tour; merger-template preserve note; `README.md`; `docs/reference/commands.md` | `CLAUDE.md`, `templates/commands/atomic-help.md`→render, `templates/agents/atomic-claude-merger.md`→render, `README.md`, `docs/reference/commands.md` | help-coverage no `MISSING:`; `grep` for `/refresh-wiki`, `atomic wiki scan`, and `atomic wiki stale` in the help template all hit; no live `<wikis>` path in `CLAUDE.md`; `npm run docs:build` clean |
| 10 | **bundle + signals** | `atomic/internal/embedded/**`, `.claude/project/signals*` | `make -C atomic bundle && git diff --exit-code` clean; `/refresh-signals` → `signals.md` lists a `wiki` domain |


## Deterministic CLI contract


**Repo discovery (shared `scan`/`stale`).** Recurse `<root>`'s children (never the root). Member iff `os.Lstat(<dir>/.git)` succeeds (dir or worktree file). On a member: record, don't descend. On a non-member: descend unless base name in the skip set or it's the `wiki/` dir. Sort for stable output.

**Classification.** Precedence: prior `summarized` + summary file present → keep `summarized`; else `indexed` iff `<repo>/.claude/project/signals.md` exists; else `summarized` iff a summary exists on disk (`wiki/repos/<name>.md`, or `wiki/repos/<name>/` containing at least one `.md`, where `<name>` is the member's base name); else `pending`. Signals win over a leftover summary — a repo that graduates to signals stays `indexed`. `scan` never reads `HEAD`.

**`## Members` linked section.** Literal `<!-- wiki-members:start -->` / `<!-- wiki-members:end -->` boundary in `wiki/index.md`. One link per member, derived from its `<wiki-scan>` status: `indexed` → `../<repo>/.claude/project/signals.md`; `summarized` → the recorded `summary` path (`repos/<repo>.md` or `repos/<repo>/`); `pending` → `../<repo>/`. Spliced idempotently like the `<wiki-scan>` block; content outside the markers untouched. Written by `atomic wiki scan` so the realm is browsable after a deterministic pass, no LLM required.

**`atomic wiki linkify` (code).** Renders inline-code path citations that resolve on disk into file-relative markdown links. Bases per artifact: `repos/<repo>(/<domain>).md` → `<realm>/<repo>` (from the summary's `repo:` frontmatter); `concerns/*.md` and `index.md` narrative → realm root. Idempotent (skips already-linked tokens); never touches fenced code blocks or `reflects_*` frontmatter. Runs after `atomic wiki stamp` in `/refresh-wiki`, so it cannot affect `atomic wiki stale` verdicts. A `[text](path)` link is not an `@-ref`. User-runnable and surfaced in `/atomic-help` (unlike `stamp`/`mark-dirty`).

**`<wiki-scan>` block.** Literal `<wiki-scan ...>` / `</wiki-scan>` boundary. Open attrs `root`, `generated` (injected clock — no wall-clock reads). One `<repo .../>` per member: `path`, `status`, optional `signals`/`summary`. No fingerprints. Target `wiki/index.md`; idempotent in-place; content outside untouched. `generated` doubles as the neglect baseline read by the hook.

**`<wikis>` block.** `~/.claude/CLAUDE.md`, literal `<wikis>`/`</wikis>`. One `- <abs index.md path>` per wiki. Present → add iff absent (dedup by normalized path = `filepath.Abs` then `filepath.Clean`, no symlink resolution); absent → append after `</atomic>` (or EOF); file absent → create. `<atomic>` never touched.

**`atomic signals scan --out <dir>`.** Writes the deterministic substrate to `<dir>` instead of `<root>/.claude/project/`. With `--out`, the scanned repo is never written to. Without it, unchanged.

**Fingerprint stamp (`atomic wiki stamp`, code).** Deterministic: `reflects_rev` = `git rev-parse HEAD` of the summarized repo, written into the summary's YAML frontmatter. For a concern, the command passes the cited-repo ids (`--cites repoA,repoB`); `stamp` resolves each to `<repo>@<fingerprint>` — HEAD SHA (`summarized`) or `signals.md` content hash (`indexed`) — and writes the `reflects:` list. The model supplies which repos are cited; code computes + writes every fingerprint value. Unresolvable cited repo → skipped. `stamp` and `mark-dirty` are internal (not surfaced in `/atomic-help`).

**`atomic wiki stale`.** Read-only. Exit `0`/`1`/`2`. Per-finding literal prefix: `DRIFT added <path>`, `DRIFT removed <path>`, `DRIFT status <path> <old>→<new>`, `STALE summary <path>`, `STALE concern <path> (<repo>)`. Exit 1 iff any line. No `HEAD` → always-needs-summary; missing/garbled `reflects_*` → stale (never crash).

**Forcing function.** `.dirty` marker = `<root>/wiki/.dirty` (gitignored via scaffolded `wiki/.gitignore`); per-checkout/local by design (drift since the last *local* refresh). `wiki.CheckStaleness(claudeHome, runner, clock)` (injected runner+clock seam): parse `<wikis>`, per wiki read `generated` + stat `.dirty`, emit a nudge if age > the memory-default threshold (30 days) OR marker present; spawns no git. `atomic wiki mark-dirty`: read `<wikis>`, normalized path-prefix check (cwd under a registered root) → `touch` that wiki's `.dirty`; invoked by the shared `signals-gate` partial on every ship (no-op when cwd is under no wiki). `/refresh-wiki` clears `.dirty` and re-runs `scan` (bumping `generated`) only after a fully completed refresh — a partial/aborted run leaves the marker set.

**Root collision.** `<root>/wiki/` exists but `index.md` absent or lacks the marker → refuse, non-zero exit, message names the path.


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Session-start hook too slow | low | `CheckStaleness` is stats + small reads, zero git spawns; checkpoint 5 asserts no exec. Hook is best-effort and never blocks the session. |
| Huge-tree recursion (home as root) | med | Stop at repo boundary + skip set + start from children; README notes `root` is a project container. |
| `scan --out` or stamp regresses normal signals | med | `--out` is additive (default path unchanged); existing signals tests gate; checkpoint 3 covers it. |
| Inferrer (wiki mode) writes into target repo | low | Substrate goes to `--out` (outside repo); mode-scoped don't-touch rule; reviewer checks. No write path into the repo remains. |
| Model mis-records a fingerprint | low | Eliminated — fingerprints are code-written; the model only supplies cited-repo ids. |
| Fingerprint misses uncommitted changes | med | Wiki reflects committed state by contract; `reflects_dirty` is an optional hint. |
| `.dirty` marker committed by accident | low | Scaffold writes `wiki/.gitignore` ignoring it; checkpoint 1 verifies the gitignore. |
| `reflects` over-flags a multi-repo concern | low | Conservative by design; model decides during re-synthesis. |
| `<wikis>` clobbered by `atomic claude update` | low | Auto-preserved outside `<atomic>`; checkpoint 8 notes it in the merger template. |
| Bundle-source `CLAUDE.md` ships a live `<wikis>` entry | low | Runtime-written to the installed CLAUDE.md only; checkpoint 8 grep-verifies. |
| Re-run drops `summarized` → `pending` | med | Re-classification preserves `summarized` when the summary exists; checkpoint 1 covers it. |


## Change log


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

# Wiki pointer rules


## Goal

Every repo-scope wiki refresh emits one path-scoped pointer card per domain to `.claude/rules/wiki/<domain>.md`, so touching any file in a domain injects a link to that domain's wiki page and its related docs, in the main session and in subagents, without loading wiki content by default.


## Non-goals

- Realm-scope wikis. This spec targets the repo-scope pipeline (`context/skills/atomic-wiki/references/repo.md`) only.
- Go-side generation or a persisted machine-readable domain classification.
- Doctor validation of rule-card files. Deferred to a follow-up (Checkpoint 3).
- Auto-loading wiki page content — cards link, they never inline domain-page prose.


## Success criteria

- [ ] `context/skills/atomic-wiki/references/repo.md` documents a new Step 7b (after Step 7 index assembly, before Step 8 `@-ref` wiring) whose scope has three branches: if `.claude/rules/wiki/` is absent or holds no cards, this is a bootstrap run — before emitting, run Step 3's domain partition for every domain in the router table (classification only, no writer dispatch) so each domain has a `<source_paths>` block, then emit a card for every domain regardless of the run's `scope`, with no fallback to the router table's Start-here directory (an unavailable `<source_paths>` is a pipeline error to report, not a guess); otherwise, scope follows the run's `scope`: full rewrites every domain's card; incremental rewrites only the cards of the domains re-dispatched in that run, leaving a non-re-dispatched domain's card untouched with its persisted `paths:` standing until that domain's next re-dispatch. All three branches delete any card whose domain is absent from the current router table.
- [ ] The new step's card contract text specifies `paths:` frontmatter derived from the Step 4 `<source_paths>` block only (`dir` → `dir/**`, a file path stays a file). The partition behind `<source_paths>` is disjoint: each path belongs to exactly one primary domain, the domain that owns the file's behavior, not every domain that couples to it, so a `dir/**` glob belongs to at most one card. A page's `## Where it lives` table and `## Coupling` section are never glob sources. The tie-break for a path whose behavior spans domains is a three-rung ladder: (1) the domain whose router "Start here" directory is the longest path prefix of the file wins (mechanical, covering every file under a Start-here tree); (2) otherwise the domain whose page's `## How it works` section describes the file's behavior; (3) otherwise the domain with the narrower `source_paths` set. Rung 1 is deterministic; rungs 2 and 3 are judgment, so byte-identical partitions across refreshes are guaranteed only for files under a Start-here tree. No exclusion globs, since `paths:` documents no negation syntax.
- [ ] The card contract places the generated marker as a YAML comment inside the frontmatter block, never in the body — the body injects into context on every matching read and the harness strips frontmatter from injection.
- [ ] The card contract specifies a typed pointer index: a one-line domain description identical to the router table's, including any inline code spans it carries (e.g. `~/.claude`), then one labeled block per non-empty category with its links nested beneath it, two spaces in, one `- link` per line — `Map:` with exactly one link to `docs/wiki/<domain>.md` and still in nested form, plus `Contracts:` (`docs/spec/`), `References:` (`docs/reference/`), `Guides:` (`docs/guides/`), `Research:` (`docs/research/`), `Designs:` (`docs/design/`), and `Related:` as catch-all for linked docs outside those families, in that order. Candidates come from the domain page's own doc links and doc surfaces the project's CLAUDE.md couples to the domain (a wiring rule or contract citation naming a `docs/` file for that domain's flow); category is assigned by path family; every link is existence-verified on disk before inclusion.
- [ ] The card contract fixes the closing line as a verbatim literal — `Consult the map before changing behavior here. Behavior changes stale the pages above. Renames or removals stale mentions beyond them: grep the old name across docs/ before shipping.` — one physical line, and pins the rest of the body to deterministic text (description verbatim from the router table including inline code spans, links sorted by path within each category in `LC_ALL=C` byte order) so an unchanged domain regenerates a byte-identical card. Body budget ≤ 12 links across the seven category blocks, which is 24 lines after frontmatter at the ceiling: a fixed five-line skeleton plus one label line per non-empty category and one line per link.
- [ ] The new step documents the `git check-ignore -v .claude/rules/wiki` probe and, when ignored, appending the negation pair `!/rules/wiki/` and `!/rules/wiki/**` to the ignore file `git check-ignore -v` names as the match. The edit is named in the interactive Step 9 report only; silent mode produces no output, per the pipeline's existing rule.
- [ ] The scoped-writes widening lands at all three touchpoints, in matching wording: `context/agents/atomic-wiki-inferrer.md` frontmatter `description:`, the agent's `<constraints>` section line, which names `.claude/rules/wiki/` (per-domain pointer cards) and an ignore file's rules/wiki negation append as writable alongside the active wiki root, and the scoped-writes sentence in `context/skills/atomic-wiki/references/repo.md`'s Rules section. The criterion fails if the `<constraints>` line still states the narrow scope (`grep -c 'rules/wiki' context/agents/atomic-wiki-inferrer.md` ≥ 2, one hit in `description:`, one in `<constraints>`).
- [ ] `context/_partials/signals-gate.md` step 3 stages `.claude/rules/wiki/` AND any ignore file the refresh modified, detected mechanically (`git status --short -- .gitignore .claude/.gitignore`) with no dependency on inferrer output — in the same `git add` invocation (or an adjacent one) that stages `docs/wiki/*.md` after a silent refresh. Falsifiable: a silent refresh that edits an ignore file leaves no unstaged ignore-file diff after the gate's `git add` (`git diff --name-only` excludes it).
- [ ] In interactive mode, Step 9's one-line summary gains cards emitted, cards deleted, and any ignore-file edit, and `context/commands/refresh-wiki.md`'s R8 report step names all three in its printed summary. Silent mode returns nothing, consistent with Step 9's existing rule; nothing downstream depends on inferrer output.
- [ ] Staging symmetry: `context/commands/subagent-implementation.md`'s finalize step and `context/commands/autopilot.md`'s Phase 4 step both stage `.claude/rules/wiki/`, guarded on the directory existing, AND any ignore file `git status --short -- .gitignore .claude/.gitignore` reports modified, alongside `docs/wiki/*.md`, in the same wording `context/_partials/signals-gate.md` step 3 uses. Falsifiable: `grep -n 'rules/wiki' context/commands/subagent-implementation.md context/commands/autopilot.md` finds the guarded staging in both.
- [ ] `docs/reference/repo-wiki.md` carries a new section documenting the pointer-rules layer: what a card contains, where it lives, and that it regenerates every refresh.
- [ ] A follow-up entry exists at `.claude/project/followups/wiki-rules-doctor-validation.md` (kind: plan) recording the deferred doctor check for rule-card files, and is listed in `.claude/project/followups/INDEX.md` after `atomic followups render`.
- [ ] Dogfood: running `/refresh-wiki` in this repo after implementation produces `.claude/rules/wiki/*.md`, one file per domain in `docs/wiki/index.md`'s `## Domains` table (11 today), and `.claude/.gitignore` gains the `!/rules/wiki/` / `!/rules/wiki/**` negation pair, both committed (`git ls-files .claude/rules/wiki/ | wc -l` matches the domain count; `git show HEAD:.claude/.gitignore | grep -c 'rules/wiki'` = 2).


## Approach

Wiki pipeline emits pointer cards as a new orchestration step (Approach A) — see `docs/design/wiki-pointer-rules.md`.


## Change tree

```
context/skills/atomic-wiki/references/repo.md ... M  (new Step 7b: emit rule cards; bootstrap runs Step 3 partition for every domain)
context/agents/atomic-wiki-inferrer.md ......... M  (description + <constraints>: widen scoped-writes)
context/_partials/signals-gate.md .............. M  (stage .claude/rules/wiki/)
context/commands/refresh-wiki.md ............... M  (report cards emitted/deleted/ignore-edit)
context/commands/subagent-implementation.md .... M  (finalize staging: mirror signals-gate step 3)
context/commands/autopilot.md .................. M  (Phase 4 staging: mirror signals-gate step 3)
docs/reference/repo-wiki.md .................... M  (new pointer-rules section)
.claude/project/followups/wiki-rules-doctor-validation.md . A  (new follow-up entry, kind: plan)
.claude/project/followups/INDEX.md ............. M  (rendered)
.claude/rules/wiki/<domain>.md ................. A  (dogfood output, 11 files, pipeline-generated)
.claude/.gitignore .............................. M  (dogfood: negation pair for rules/wiki)
```


## Outline

```
context/skills/atomic-wiki/references/repo.md
  Step 7b — Emit wiki pointer rules
    scope-aware rewrite — bootstrap (rules dir absent/empty): run Step 3's domain partition for every router domain first (classification only), then every card regardless of scope, no Start-here fallback; otherwise full: every card, incremental: re-dispatched domains only, non-re-dispatched cards untouched; all branches delete stale cards
    card contract — frontmatter globs from Step 4 source_paths only (disjoint partition, no exclusion; three-rung tie-break for spanning paths: Start-here prefix match, else How it works ownership, else narrower path set, rung 1 deterministic) + YAML-comment marker, description verbatim, typed pointer index (every collection nested one entry per line, frontmatter and body alike; Map single by construction; Contracts/References/Guides/Research/Designs/Related hold as many links as the domain has, sourced from the domain page and CLAUDE.md domain couplings, category by path family, links sorted), fixed-literal closing line, body budget in links
    git check-ignore probe and negation-pair append
    report — cards emitted, cards deleted, ignore-file edit added to Step 9's interactive summary line; silent mode stays silent
  Rules (existing section)
    scoped-writes sentence — widened to include .claude/rules/wiki/

context/agents/atomic-wiki-inferrer.md
  description frontmatter — widened scoped-writes clause
  <constraints> section — scoped-writes line widened to include .claude/rules/wiki/

context/_partials/signals-gate.md
  step 3 — git add extended to .claude/rules/wiki/ and any ignore file git status shows modified

context/commands/refresh-wiki.md
  repo-scope report step — cards emitted/deleted, ignore-file edit line

context/commands/subagent-implementation.md
  finalize signals-refresh staging — extended to .claude/rules/wiki/ and any ignore file git status shows modified

context/commands/autopilot.md
  Phase 4 signals-refresh staging — extended to .claude/rules/wiki/ and any ignore file git status shows modified

docs/reference/repo-wiki.md
  Pointer rules (new ## section) — card shape, location, regeneration, read/write payoff

.claude/project/followups/wiki-rules-doctor-validation.md
  entry — deferred doctor validation of rule-card files
```


## Flows

**Flow: pointer-card emission (Step 7b, every refresh)**

1. `atomic-wiki-inferrer` finishes Step 7 (`docs/wiki/index.md` assembled, every domain page and its links final).
2. If `.claude/rules/wiki/` is absent or holds no cards (bootstrap run), the inferrer first runs Step 3's domain partition for every domain in the router table (classification only, no writer dispatch), so every domain has a `<source_paths>` block before any card is built — there is no Start-here fallback, and an unavailable `<source_paths>` after this partition is a pipeline error to report. For each domain in scope (every domain on a bootstrap or full run; the re-dispatched domains only on an incremental run once cards already exist — a non-re-dispatched domain's card is left untouched), the inferrer builds a card: `paths:` globs from that domain's disjoint Step 4 `<source_paths>` block, the YAML-comment marker in frontmatter, the router's one-line description, the typed pointer index (`Map:` with exactly one link to `docs/wiki/<domain>.md`; `Contracts:`/`References:`/`Guides:`/`Research:`/`Designs:`/`Related:` blocks, each holding its links nested one per line and present only when non-empty, candidates pulled from the domain page's own linked docs and from doc surfaces the project's CLAUDE.md couples to the domain, category by path family, each link checked to exist on disk with missing targets dropped, links sorted by path), and the fixed-literal closing line.
3. The inferrer probes `git check-ignore -v .claude/rules/wiki`. If it reports a match, the inferrer appends `!/rules/wiki/` and `!/rules/wiki/**` to the matched ignore file.
4. The inferrer writes `.claude/rules/wiki/<domain>.md` for every in-scope domain and deletes any existing card file whose domain is absent from the current router table.
5. In interactive mode, Step 9's summary line reports cards emitted, cards deleted, and any ignore-file edit. In silent mode the inferrer returns nothing.
6. Step 8 (`@-ref` wiring) proceeds unchanged.

**Flow: read-path payoff**

1. A session (main or subagent) opens or edits a file under a domain's `source_paths`.
2. Claude Code's `paths:` glob match injects that domain's pointer card into context.
3. The card names `docs/wiki/<domain>.md` and its related docs; the session reads the map on demand instead of guessing or re-deriving domain facts.

**Flow: write-path payoff / ship-time staging**

1. A ship verb or the implementation loop runs a silent signals refresh (`signals-gate` partial).
2. After the refresh, the gate stages `docs/wiki/*.md`, `.claude/rules/wiki/`, and any ignore file `git status --short -- .gitignore .claude/.gitignore` shows modified, in the same commit.
3. The committed diff carries the domain page changes, any regenerated pointer cards, and any ignore-file edit together, so a reviewer sees them as one unit and no ignore-file edit is left unstaged.

**Flow: ignore-file remediation (first run in a repo that ignores `/rules/*`)**

1. Step 7b probes `git check-ignore -v .claude/rules/wiki` before writing.
2. The probe reports a match against `.claude/.gitignore`'s `/rules/*` line.
3. The inferrer appends `!/rules/wiki/` and `!/rules/wiki/**` to `.claude/.gitignore`.
4. The refresh report (interactive) names the ignore-file edit; the `signals-gate` partial (silent) detects the modified ignore file via `git status` and stages it alongside `.claude/rules/wiki/` so it isn't a silent, unstaged side effect.


## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Document Step 7b (scope-aware card emission contract, globs from Step 4 source_paths, fixed-literal closing line, interactive-only report line) in the repo pipeline reference and widen the inferrer's scoped-writes contract (frontmatter `description:` + `<constraints>` section) to match | `context/skills/atomic-wiki/references/repo.md`, `context/agents/atomic-wiki-inferrer.md` | atomic-implementer (mode: feature) | 2 | `grep -n 'Step 7b' context/skills/atomic-wiki/references/repo.md` finds the new step; `grep -c 'rules/wiki' context/agents/atomic-wiki-inferrer.md` ≥ 2 (description + `<constraints>`); `sed -n '/^## Rules/,$p' context/skills/atomic-wiki/references/repo.md \| grep -c 'rules/wiki'` ≥ 1 (Rules section, not Step 7b's own prose) |
| 2 | Wire the ship-path: stage `.claude/rules/wiki/` AND any modified ignore file (mechanical `git status` check) in `signals-gate`, report cards emitted/deleted/ignore-edit in `/refresh-wiki` R8 | `context/_partials/signals-gate.md`, `context/commands/refresh-wiki.md` | atomic-implementer (mode: surgical) | 2 | `grep -n 'rules/wiki' context/_partials/signals-gate.md` finds the extended `git add`; the same step stages any ignore file `git status --short -- .gitignore .claude/.gitignore` reports modified; `grep -n 'rules/wiki\|card' context/commands/refresh-wiki.md` finds the report line |
| 3 | Document the pointer-rules layer in the repo-wiki reference page; file the deferred doctor-validation follow-up | `docs/reference/repo-wiki.md`, `.claude/project/followups/wiki-rules-doctor-validation.md` (new entry), `.claude/project/followups/INDEX.md` (mechanical `atomic followups render` output) | atomic-implementer (mode: feature) | 2-3 | `grep -n '^## ' docs/reference/repo-wiki.md` shows a new pointer-rules section; `atomic followups list` (or `INDEX.md` after render) includes `wiki-rules-doctor-validation` |
| 4 | Dogfood: run `/refresh-wiki` in this repo, confirm cards emit for all 11 domains and the ignore negation lands, commit | `.claude/rules/wiki/*.md`, `.claude/.gitignore` | orchestrator — main session dispatches `atomic-wiki-inferrer` via `/refresh-wiki`, after Checkpoints 1-3 land | ~12 | `ls .claude/rules/wiki/*.md \| wc -l` matches the domain count in `docs/wiki/index.md`; `grep -c 'rules/wiki' .claude/.gitignore` = 2; `git status --short .claude/rules/wiki .claude/.gitignore` clean after commit |


## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Agent-authored `paths:` globs drift from a domain's real `source_paths` between refreshes | med | Card is fully pipeline-owned and rewritten every refresh; doctor validation follow-up (Checkpoint 3) closes the gap later |
| A domain page's doc links include paths that don't exist on disk, producing a dead pointer-index link | low | Step 7b existence-checks every candidate link before inclusion and drops missing ones |
| `.claude/.gitignore` negation append runs on every refresh instead of once, causing duplicate lines | med | Step 7b's `git check-ignore -v` probe only appends when the path currently resolves as ignored; once the negation lands, the probe reports "not ignored" and skips the append |
| Regenerated cards differ in wording between refreshes, adding diff noise to every ship commit | med | Every body line is deterministic: router sentence verbatim, fixed-literal closing line, links sorted by path; an unchanged domain yields a byte-identical card |
| The three-rung tie-break misassigns an ambiguous shared file to the wrong domain | low | Rung 1 (Start-here prefix match) covers most files mechanically; a misassignment via rungs 2-3 self-corrects on the domain's next re-dispatch, which recomputes `<source_paths>` and rewrites the card |


## Change log

### 2026-09-02 — Collections nest one entry per line; category labels pluralized

**What changed:** Every collection in a card is now a label on its own line with its entries nested beneath it, two spaces in. That covers `paths:`, which becomes a YAML block sequence, and each pointer category, whose links were previously comma-separated after a single label. Category labels are uniformly plural, so `Reference:` became `References:` and `Design:` became `Designs:`; `Map:` keeps its single link but takes the nested form so every block reads alike. The body budget is restated in links rather than lines: at most twelve links across the seven category blocks, a 24-line ceiling from the `5 + categories + links` shape.

**Why:** a card is rewritten whenever its domain is re-dispatched, so its diff is read far more often than the card itself. A flow-style `paths:` array rewraps when one glob is added, and the reviewer sees the whole line rewritten rather than the one path that changed — observed on `workflow.md`, whose 27 globs redrew entirely to record a single added partial. Nested style makes that a one-line insertion.

**Superseded:** the typed pointer index was one line per category with links comma-separated on it, labels mixed singular and plural, `paths:` a single-line flow array, and the body capped at 12 lines.

### 2026-09-02 — Description verbatim includes inline code; links byte-sorted

**What changed:** The typed pointer index's description line now pins "verbatim" to include the router table's inline code spans (e.g. `~/.claude`), not just its wording. The per-category link sort now pins the collation: `LC_ALL=C` byte order.

**Why:** Three shipped cards (`bundle.md`, `code-intel.md`, `config.md`) stripped inline backticks from the router description, and `wiki.md`'s `Contracts:` line was not byte-sorted; both left the byte-identity guarantee the card contract makes undefined at the character level, so two refreshes could legitimately disagree.

**Superseded:** "Description verbatim from the router table" and "links sorted by path" without a collation.

### 2026-09-02 — Disjoint card partition replaces "gets both cards"

**What changed:** The glob criterion no longer allows a path to land in two domains' `<source_paths>`. The Step 3 partition backing `<source_paths>` is disjoint: each path belongs to exactly one primary domain, the domain that owns the file's behavior, not every domain that merely couples to it. A page's `## Where it lives` table and `## Coupling` section are never glob sources. A shared file's tie-break is a three-rung ladder: the domain whose router `Start here` directory is the longest path prefix of the file wins (deterministic); otherwise the domain whose page's `## How it works` section describes the file's behavior; otherwise the domain with the narrower `<source_paths>` set. Only rung 1 guarantees byte-identical partitions across runs.

**Why:** A dogfood bootstrap run derived card globs from each page's `## Where it lives` table (coupling paths) instead of the Step 3 partition, so `context/commands/refresh-wiki.md` matched 4 cards and `atomic/internal/cliusage/cliusage.go` matched 3: overlap the design intended to be rare turned out routine for `context/`.

**Superseded:** "A path present in two domains' `source_paths` gets both cards" as the resolution for shared files; the reason a card can't exclude another's files (`paths:` documents no negation syntax) still holds, but disjointness must now be achieved at partition time rather than accepted as overlap.

### 2026-09-02 — Staging symmetry across ship paths; bootstrap partitions before emitting

**What changed:** `context/commands/subagent-implementation.md`'s finalize staging and `context/commands/autopilot.md`'s Phase 4 staging now also stage `.claude/rules/wiki/` (guarded on the directory existing) and any ignore file `git status --short -- .gitignore .claude/.gitignore` reports modified, alongside `docs/wiki/*.md` — matching `context/_partials/signals-gate.md` step 3's wording. Step 7b's bootstrap branch now runs Step 3's domain partition for every router-table domain (classification only, no writer dispatch) before emitting cards, so each domain has a `<source_paths>` block to derive globs from; the Start-here directory is not a fallback, and an unavailable `<source_paths>` after the partition is a pipeline error to report. On a non-bootstrap incremental run, cards for domains not re-dispatched stay untouched, their persisted `paths:` standing until that domain's next re-dispatch.

**Why:** A CP4 dogfood run found the two command-file staging points committed `chore(signals)` with only `docs/wiki/*.md`, leaving cards and ignore-file edits from those refresh paths uncommitted; the same run's bootstrap emission fell back to the router's Start-here directory for domains not re-dispatched (no `<source_paths>` computed in incremental mode), producing under-scoped cards (e.g. the `signals` card carried only `atomic/internal/signals/**`).

**Superseded:** The two command files' staging step as `git add docs/wiki/*.md` alone. The bootstrap branch as "emit every domain's card" without first ensuring every domain has a `<source_paths>` block — it previously assumed one was already available, which incremental mode does not guarantee.

### 2026-09-01 — Bootstrap branch for a pre-cards wiki

**What changed:** Step 7b's scope gains a bootstrap branch: when `.claude/rules/wiki/` is absent or holds no cards, emit a card for every domain in the router table regardless of the run's `scope`. The full/incremental branch applies only once cards already exist. All branches keep deleting any card whose domain is absent from the current router table.

**Why:** A repo whose wiki predates pointer cards runs its next refresh incrementally (per Step 2b's decision tree), so the prior two-branch rule gave it cards only for the domains that happened to be re-dispatched, leaving the rest cardless indefinitely. The bootstrap branch forces a full first emission.

**Superseded:** The two-branch scope rule (full: every card; incremental: re-dispatched domains only) as the complete rule; it now applies only after a repo's first card set exists.

### 2026-09-01 — Silent-mode carrier, incremental scope, deterministic body

**What changed:** Step 7b's scope now follows the run's `scope` (full: every card; incremental: re-dispatched domains only; both delete stale cards). The ignore-file edit is no longer reported through the concerns channel; `signals-gate` detects a modified ignore file mechanically via `git status` and stages it, and Step 7b's counts appear only in the interactive Step 9 summary. Globs derive from the Step 4 `<source_paths>` block only, overlap yields two cards, and the exclusion tie-break is gone. The closing line is a fixed literal, links are sorted, and the body budget counts blank lines. Change tree, Outline, Flows, Checkpoints, and Risks updated together.

**Why:** Review against `references/repo.md` found Step 6b concerns are discarded and Step 9 emits nothing in silent mode, so the gate had no carrier for the ignore-file path in exactly the mode it needed one; incremental refreshes recompute `source_paths` only for re-dispatched domains, so a full rewrite had nothing to build untouched cards from; `paths:` documents no negation syntax, so the exclusion clause was unimplementable; and agent-authored wording would churn every ship commit.

**Superseded:** Full rewrite of the rules dir on every run; Step 6b-channel output contract consumed by `signals-gate`; primary-domain-wins tie-break with exclusion globs; closing line described rather than fixed.


### 2026-09-01 — Typed pointer index; marker moved to frontmatter

**What changed:** The flat `Related:` bucket (capped at 5) became a typed pointer index — `Map:` single by construction, then multi-valued `Contracts:`/`Reference:`/`Guides:`/`Research:`/`Design:` lines by path family with `Related:` as catch-all; the 12-line body budget replaces the numeric link cap as the governor. The generated marker moved from a body HTML comment to a YAML comment inside frontmatter.

**Why:** The card body injects into context on every matching read, so a body marker is per-injection noise while frontmatter is stripped from injection. The flat bucket hid doc-surface types and imposed an arbitrary single-value/5-value shape; the doc taxonomy already exists, so the index carries it.

**Superseded:** Body-comment marker; single flat `Related:` list capped at 5.


### 2026-09-01 — CLAUDE.md as a Related-link source; rename-nudge in the closing line

**What changed:** `Related:` link selection now draws from two sources — the domain page's own doc links AND doc surfaces the project's CLAUDE.md couples to the domain. The card's single closing line now also carries a rename-nudge: renames or removals in the domain stale mentions beyond its own pages, so grep the old name across `docs/` before shipping. Success criteria, Outline, and the emission Flow updated together.

**Why:** A live drift incident — a renamed agent left stale references in another workstream's spec, session memory, and CLAUDE.md — showed the card must propagate updates across every documentation form coupled to a domain, and that renames are the drift class the update-nudge alone missed.


## Implementation log

### shipped — 2026-09-02

Built across 9 iterations of /subagent-implementation (4 checkpoints, 3 dogfood refresh runs, 1 audit), squashed into one commit before merge. Steps, in order:

- CP-1 Step 7b card contract in the pipeline reference; inferrer scoped-writes widened at three touchpoints
- CP-2 ship-path wiring: signals-gate stages cards + git-status-detected ignore edits; refresh-wiki R8 report line
- CP-3 pointer-rules section in docs/reference/repo-wiki.md; doctor-validation follow-up filed; guard form fixed
- bootstrap branch: a pre-cards wiki emits every domain on its first refresh
- CP-4 dogfood run 1: first 11 cards, .claude/.gitignore negation pair
- staging symmetry in subagent-implementation + autopilot; bootstrap runs the Step 3 partition first
- dogfood run 2: partition-derived globs
- disjoint partition with three-rung tie-break; signals-refresh-timing.md path correction; voice alignment
- dogfood run 3: disjoint cards (refresh-wiki.md → workflow, cliusage.go → doctor)
- documentation pass and this log
- audit remediation: log history restored, card text pinned, help router and file-roles updated
- silent signals refresh after finalize

**Out-of-scope work performed during this build:**

- `docs/spec/signals-refresh-timing.md` pre-relocation paths corrected: the same stale-reference drift class this feature targets, found by the iteration-6 implementer and dispositioned fix-now.
- `context/_partials/signals-gate.md` punctuation aligned so the three staging sites are byte-identical.

**Unforeseens — surprises that emerged during implementation:**

- Silent-mode refreshes return nothing, so the gate could not learn an ignore-file path from the inferrer; staging became mechanical (`git status`) instead.
- Incremental refreshes recompute `source_paths` only for re-dispatched domains, so the first emission needed a bootstrap branch that partitions every domain first; the first dogfood run fell back to Start-here directories and produced under-scoped cards.
- The Step 3 partition was not disjoint in practice (coupling paths from `## Where it lives`), so `refresh-wiki.md` fired 4 cards; the contract now requires a disjoint partition with a deterministic first-rung tie-break.
- An unguarded `git add -A .claude/rules/wiki/` exits 128 when the dir is absent, which would abort every silent ship refresh in a wiki with no domain files.

**Deferred items still open:**

- `.claude/project/followups/wiki-rules-doctor-validation.md` — doctor validation of card files between refreshes.
- `.claude/project/followups/claude-md-domain-content-to-rules.md` — relocating domain-scoped CLAUDE.md content into path-scoped rules.
- Rung 2/3 tie-break assignments are judgment calls; a domain's next full re-dispatch self-corrects them (disclosed in Risks).

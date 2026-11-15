# Documentation as maintenance


## Goal

Replace the hardcoded artifact-index-updating behavior of `/documentation` and `atomic-documentation` with a discovery-based documentation maintenance system that helps users keep human-facing project docs (architecture, ERDs, business rules, API references, flowcharts) in sync with code changes. The skill does the work — users decide when.


## Non-goals

- Replacing `atomic-writing` (voice and tone for file contents stays there).
- Auto-committing documentation changes (edits are staged; user commits via ship verb).
- Bootstrapping docs from zero for undocumented projects (that's `/atomic-plan` territory).
- Non-markdown doc formats (Confluence, Notion, Google Docs).
- Enforcing a specific docs directory structure.
- "Missing" detection during commit flow (authoring mode only).


## Success criteria


### Binary

- [ ] `atomic docs scan` deterministically walks doc directories, extracts H1 + first 3 H2s per `.md` file, writes `<state-root>/project/doc-surfaces.md` (`.atomic` by default; `config.ProjectDir`). Respects `.signalsignore` exclude globs. The cache is git-ignored through `atomic repo init`'s managed rule `/project/doc-surfaces.md` (`docs/spec/repo-init.md`).
- [ ] `atomic docs stale` compares cache mtime against latest doc-file mtime. Exit 0 = fresh, exit 1 = stale.

### Authoring mode (`/documentation` explicit)

- [ ] Bootstrap: on first run (no `## Documentation surfaces` in CLAUDE instructions), scans, presents discovered surfaces as numbered list, user picks which to index, writes table to committed CLAUDE.md. Creates CLAUDE.md if it doesn't exist.
- [ ] Unindexed detection: subsequent runs compare scan results against indexed table, offer to add new unindexed docs.
- [ ] Stale/incomplete classification: matches diff range against indexed surfaces, classifies each as stale or incomplete based on what the diff changed vs what the doc covers.
- [ ] Missing classification: for domains with 5+ files and no corresponding doc surface, suggests creating a new page. Only fires in authoring mode, never commit flow.
- [ ] Content generation: when user picks "Yes" on stale/incomplete, skill opens the file and makes the edit (ERD updates, flow diagram fixes, prose changes, field table rows). When user picks "New" on missing, skill generates full page draft with Mermaid diagrams. Verified by reviewer pass on representative fixture diffs.
- [ ] `--discover` flag triggers a re-scan of the doc tree and presents unindexed docs for addition to the surfaces table.

### Maintenance mode (ship verb commit flow)

- [ ] Reads `## Documentation surfaces` from CLAUDE instructions. If missing → skips silently (no error, no prompt).
- [ ] Matches staged diff against "Covers" column via LLM judgment within the existing commit-flow turn (no separate dispatch). The table is already in CLAUDE.md context. User steers via Skip for false positives. Presents Yes/Later/Skip per stale/incomplete surface.
- [ ] "Yes" → skill edits the file + stages. "Later" → records one follow-up through the `followup-sink` partial: an issue in the resolved issue tracker, or a local ledger entry when the tracker is `filesystem` or cannot take it (contract: `docs/spec/workflow-preferences.md`). The prompt shows the sink preview under the options, so choosing "Later" is the go-ahead and no second prompt follows. "Skip" → no action.
- [ ] Ignores surface entries with `impact_type: missing` — maintenance mode never suggests new pages.
- [ ] When no surfaces table exists, ship verbs print a recurring hint every commit: `no documentation surfaces indexed. run /documentation to set up.` Then proceed without blocking. Hint stops appearing once the user bootstraps.

### Integration

- [ ] `/subagent-implementation` Phase 3 next-steps: if implemented changes affect indexed surfaces, includes `/documentation` in the next-steps list with count of potentially stale surfaces.

### Skill

- [ ] All rows in the existing `## Surface routing` hardcoded table removed. Skill reads only from the project's indexed `## Documentation surfaces` table.
- [ ] Skill's YAML output contract adds `impact_type` field (`stale | incomplete | missing`) to each surface entry. Parser rule added: surface entries with unknown fields are accepted; only `path` and `voice` are required.
- [ ] Generated docs follow output quality standards from design doc: short intro, tables, Mermaid with captions, plain language, no LLM-tell, no code-restating.

### Migration

- [ ] This repo's `claude.local.md` gains a `## Documentation surfaces` table replacing the hardcoded routing.
- [ ] Bundle regenerated. CLAUDE.md, README.md updated.


## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | `atomic docs scan` + `atomic docs stale` | `atomic/internal/docs/` (new pkg, ~4 files; atomic-builder) | Tests: scan finds .md files in test fixtures, extracts headings, writes cache. Stale returns correct exit. Respects .signalsignore. |
| 2 | CLI wiring for `docs scan` and `docs stale` | `atomic/cmd/atomic/main.go`, `main_test.go` (~2 files; atomic-surgeon) | Tests: subcommands dispatch correctly. Usage text updated. |
| 3 | Skill rewrite: generic `atomic-documentation` | `skills/atomic-documentation/SKILL.md` (atomic-surgeon) | Grep: no hardcoded routing rows remain. Two modes documented. `impact_type` in output contract. Parser rule for unknown fields added. |
| 4 | Command rewrite: `/documentation` bootstrap + authoring | `templates/commands/documentation.md` (atomic-builder) | Grep: bootstrap flow present. `--discover` flag documented. Authoring mode walks stale/incomplete/missing with Yes/Later/Skip. Renders via `make render`. |
| 5 | Ship verb doc-impact partial: maintenance mode | `templates/shared/doc-impact.md` (atomic-surgeon) | Grep: reads surfaces table, Yes/Later/Skip options, ignores `missing` entries. One-time hint when no table exists. Renders via `make render`. |
| 6 | `/subagent-implementation` advisory line | `templates/commands/subagent-implementation.md` (atomic-surgeon) | Grep: Phase 3 next-steps includes `/documentation` with stale-surface count. |
| 7 | Atomic repo migration: surfaces table to `claude.local.md` | `claude.local.md` (atomic-surgeon) | Grep: `## Documentation surfaces` table present with correct paths. |
| 8 | CLAUDE.md + README.md + bundle regen | `CLAUDE.md`, `README.md`, `make render`, `make bundle` (atomic-builder) | CI: render + bundle drift gates pass. CLAUDE.md documents `atomic docs scan/stale` and bootstrap. README tables updated. |


## Checkpoint details


### CP 1 — `atomic/internal/docs/` package

New package mirroring `atomic/internal/signals/`. Reuses `mdparse.Sections()` for heading extraction.

Exported scan entry point accepts a repo root path. Options struct for configurability (exclude globs, scanned directory names). Exported stale check returns a sentinel error distinguishable from "not stale."

Scan behavior:

1. Read `.signalsignore` via the same function `signals` uses (export `readSignalsIgnore` or duplicate the ~25-line function).
2. Walk directories matching: `docs/`, `doc/`, `documentation/`, `wiki/`, `ADR/`, `adr/`, `decisions/`. Also any `**/README.md`.
3. For each `.md` file: read, call `mdparse.Sections()`, extract H1 (title) + first 3 H2s (scope). Build one-line summary: `path — H1 / H2a / H2b / H2c`.
4. Write `<state-root>/project/doc-surfaces.md` with header + last-scanned timestamp + one line per surface.

Stale behavior: compare cache file mtime against latest mtime of any `.md` file in scanned directories. Return stale error or nil.

**Test fixtures**: `atomic/internal/docs/testdata/` with a small doc tree (3-5 .md files with headings, a `.signalsignore` that excludes one). Tests verify: correct heading extraction, signalsignore exclusion, cache file written with expected content, stale detection after touching a doc file.


### CP 2 — CLI wiring

Add `docs` to the top-level verb dispatch switch in `main.go` (same pattern as `signals`, `doctor`, `followups`). Add a `runDocs` dispatcher for subcommands: `scan`, `stale`. Update usage text to list `docs scan` and `docs stale`.


### CP 3 — Skill rewrite

Remove the entire `## Surface routing` table (the hardcoded diff-signal → surface mapping). Keep the `## Two voices` taxonomy table — that's voice guidance, not routing. Replace with:

- Read `## Documentation surfaces` table from CLAUDE instructions (search order: `claude.local.md` / `CLAUDE.local.md` → `CLAUDE.md`). If no table found → return empty surfaces (clean degradation).
- Two modes section: maintenance (stale/incomplete only, commit flow) vs authoring (full pipeline, explicit `/documentation`). Maintenance mode never emits surface entries with `impact_type: missing`.
- Updated output contract: add `impact_type: stale | incomplete | missing` to each surface entry. Add parser rule: "Surface entries with unknown fields are accepted; only `path` and `voice` are required."
- Content generation instructions: when user picks "Yes", skill opens the file and makes the edit. ERDs, flowcharts, field tables, prose — the skill does the work. Mermaid generated directly with one-sentence captions.
- Output quality section: reference design doc standards (short intro, tables, plain language, no LLM-tell, no code-restating).
- Override section stays (user can still declare custom surfaces) but reframed as "the primary mechanism is the indexed table; overrides sharpen or exclude."


### CP 4 — Command rewrite

Replace current `/documentation` with:

**Bootstrap mode** (no `## Documentation surfaces` in CLAUDE instructions):

1. Run `atomic docs scan`.
2. Read the cache file.
3. Present discovered surfaces as numbered list (axiom 4).
4. User picks: `all | 1 3 5 | none`.
5. For selected: optionally annotate domain coverage (or accept heading-derived suggestion).
6. Write `## Documentation surfaces` table to committed CLAUDE.md. Create the file if it doesn't exist.

**Authoring mode** (table exists, explicit `/documentation`):

1. Check for unindexed docs → offer to add.
2. Read diff range (user-supplied or `<base>..HEAD`).
3. Match diff against surfaces.
4. Classify: stale / incomplete / missing (missing only for modules with 5+ files).
5. Walk surfaces: Yes / Later / Skip (and New for missing).
6. For "Yes" and "New" → skill generates content, stages files.
7. For "Later" → record through the `followup-sink` partial with id `doc-<slug>-<short-hash>`, title `update <path> — <one-line reason>`, severity `nit`, kind `finding`, origin `/documentation authoring`. The sink preview prints under the Yes / Later / Skip options.
8. Summary.

Flags preserved: `--dry-run`, `--print-template`. Add: `--discover` (re-scan and offer to update the surfaces table, even when a table already exists).


### CP 5 — Ship verb doc-impact partial

Rewrite `templates/shared/doc-impact.md`:

1. Read `## Documentation surfaces` from CLAUDE instructions. If missing → print hint (`no documentation surfaces indexed. run /documentation to set up.`) and skip. Hint recurs every commit until the user bootstraps. No error, no blocking.
2. Match `git diff --cached` against "Covers" column.
3. Ignore any surface entry with `impact_type: missing` — maintenance mode never suggests new pages.
4. For each stale/incomplete match: present with reason + proposed change.
5. Options: `[y] Yes` / `[l] Later` / `[s] Skip`, with the sink preview of the follow-up "Later" would record printed under them.
6. Yes → invoke `atomic-documentation` skill on that surface. Skill edits + stages.
7. Later → record through the `followup-sink` partial with id `doc-<slugified-path>-<short-hash>`, title `update <path> — <reason>`, severity `nit`, kind `finding`, origin `ship-verb doc-impact`. The sink creates an issue in the resolved issue tracker, or a local ledger entry when the tracker is `filesystem` or cannot take it.
8. Skip → no action.

If no surfaces matched → proceed silently (same as today's "no doc impact" path).


### CP 6 — Implementation advisory

In `templates/commands/subagent-implementation.md` Phase 3, after the implementation summary and before the ship-verb suggestions, add:

If the implemented changes affect indexed doc surfaces (read `## Documentation surfaces` from CLAUDE instructions, match checkpoint files against "Covers" column), append to the next-steps list:

```
/documentation — N doc surfaces may be stale
```

One line. Advisory only. Not a gate.


### CP 7 — Atomic repo migration

Add to `claude.local.md`:

```markdown
## Documentation surfaces

| Path | Covers | Voice |
|------|--------|-------|
| `README.md` | project overview, install, commands, agents, skills | atomic-writing |
| `docs/guides/install.md` | installation, updating, uninstalling | atomic-writing |
| `docs/guides/contributing.md` | contributing, build pipeline, testing | atomic-writing |
| `docs/guides/evaluations.md` | Docker eval environment, testing setup | atomic-writing |
| `docs/reference/workflow.md` | plan, implement, diagnose, ship lifecycle | atomic-writing |
| `docs/reference/commands.md` | command reference table | atomic-writing |
| `docs/reference/agents.md` | agent reference table | atomic-writing |
| `docs/reference/skills.md` | skills reference table | atomic-writing |
| `docs/reference/signals-workflow.md` | signals scan, infer, wire pipeline | atomic-writing |
| `docs/reference/output-style.md` | atomic output style reference | atomic-writing |
| `CLAUDE.md` | global contract, agent/command/skill registry | atomic-writing |
```


### CP 8 — Cross-references and bundle

- `CLAUDE.md`: add `atomic docs scan` and `atomic docs stale` to "Atomic binary subcommands". Update `/documentation` description to mention bootstrap flow. Update "Two voices" to reference the surfaces table mechanism.
- `README.md`: update `/documentation` row in commands table. Add note about bootstrap behavior.
- `make render` + `make bundle`.
- `/refresh-wiki`.


## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| `mdparse.Sections()` doesn't handle all heading styles in user docs (Setext, non-standard) | Low | `mdparse` already handles ATX headings via goldmark; Setext detected by `IsATXOnly()`. Test with diverse fixtures. |
| Ship verb latency increase from reading surfaces table | Low | Table is already in CLAUDE.md context — no extra file read. Matching is LLM judgment within existing turn, not a separate dispatch. |
| Users forget to run `/documentation` bootstrap | Med | Ship verbs print a recurring hint every commit when no surfaces table exists: `no documentation surfaces indexed. run /documentation to set up.` Stops once bootstrapped. Degrades cleanly — no surfaces = no doc-impact checks. |
| `readSignalsIgnore` is unexported | Low | Either export it or duplicate the ~25-line function. Both are cheap. |
| Large surfaces table in CLAUDE.md wastes tokens | Low | Typical project has 5-20 doc pages. Table is ~3-5 lines per surface. Even 20 surfaces = ~60 lines = ~200 tokens. |
| Follow-up integration adds complexity to doc-impact partial | Med | "Later" composes the shared `followup-sink` partial; doc-impact and `/documentation` add no tracker logic of their own. |
| "Later" opens a hosted issue the user expected to stay local | Med | The sink preview names the tracker before the choice, its result line names the tracker used, and `issue-tracker: filesystem` keeps follow-ups local. |


## Change log

### 2026-11-12 — The doc-surfaces cache is ignored

**What changed:** The scan criterion states the cache is git-ignored through `atomic repo init`'s managed rule `/project/doc-surfaces.md`.

**Why:** the scan regenerates the cache with a timestamp on every run and nobody edits it, so a tracked copy churns every commit.

**Superseded:** the cache had no ignore rule and was left to be committed.

### 2026-11-11 — "Later" records through the workflow sink

**What changed:** The maintenance-mode criterion, authoring step 7, doc-impact step 7, and the follow-up risk rows record "Later" through the `followup-sink` partial: an issue in the resolved issue tracker, else a local ledger entry. The sink preview prints under the Yes / Later / Skip options, so the choice is the only prompt.

**Why:** `docs/spec/workflow-preferences.md` resolves an issue tracker per repository and makes the local ledger its `filesystem` fallback.

**Superseded:** "Later" always ran `atomic followups add`.

### 2026-11-11 — State root defaults to .atomic

**What changed:** The scan success criterion and scan step 4 name the cache as `<state-root>/project/doc-surfaces.md`, with `.atomic` as the default state root.

**Why:** `docs/spec/omp-plugin-compatibility.md` moves repository state to the `.atomic` default, with a resolution ladder and `atomic state migrate`.

**Superseded:** the cache was `.claude/project/doc-surfaces.md`.

**Correction:** the scan criterion said the cache path is covered by `.gitignore`, and that sentence is removed. `atomic/internal/repoinit/repoinit.go:73-95` ignores only `/.scratchpad/`, `/.atomic-index/`, and `/worktrees/` under the state root, no ignore rule names `doc-surfaces`, and `git check-ignore .atomic/project/doc-surfaces.md` exits 1.

### 2026-11-11 — Reminders removed

**What changed:** Authoring mode and the ship-verb doc-impact partial offer Yes / Later / Skip. The success criteria, Checkpoints 4 and 5, and the follow-up risk row follow.

**Why:** `docs/spec/workflow-preferences.md` drops the reminder feature.

**Removed:** the "Remind" option and its `/remind-me` flow.

### 2026-10-19 — Artifacts assume atomic is installed

**What changed:** Bootstrap mode runs `atomic docs scan` with no Bash fallback.

**Why:** Issue #298: artifacts ship inside and install through the `atomic` binary, so the binary is always present when they run. See `docs/spec/artifacts-mermaid-flows.md`.

**Superseded:** bootstrap fell back to `find docs/ doc/ documentation/ wiki/ ADR/ adr/ decisions/ -name '*.md'` when the binary was absent.

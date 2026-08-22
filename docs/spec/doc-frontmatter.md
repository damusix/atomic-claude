# Doc frontmatter contract


## Goal


Every `docs/design/<topic>.md` and `docs/spec/<topic>.md` carries a YAML frontmatter block
(`type`, `description`, `domain`, `parent`, `status`) that code can parse, so family
membership and shipped state are queryable without prose grep. `atomic validate spec` enforces
the block; a migration step backfills every pre-existing file in this repo.


## Non-goals


- No `feature:` key. Family membership is the `parent` chain, resolved at read time.
- No changes to `docs/wiki/*.md` or bucket-doc frontmatter; both already carry their own
  contract and are untouched here.
- No wiki-pipeline role for design/spec files (`context/skills/atomic-wiki/references/repo.md`
  Steps 3/4/5). That is phase 2 of `docs/design/doc-consolidation.md`.
- No consolidation verb, lineage table, or spec retirement. That is phase 3.
- No automatic `status` derivation from checkpoint completion. `status: shipped` is stamped
  only by the finalize phase of `/subagent-implementation` and `/autopilot`.


## Success criteria


- [ ] `atomic template spec` emits a file whose frontmatter block carries `type: Spec` and
      passes `atomic validate spec` with no other content beyond placeholders; `atomic
      template design-doc` emits a file whose block carries `type: Design`, has no
      `## Checkpoints` section, and passes the same command.
- [ ] `atomic validate spec` discovers `docs/spec/*.md` and `docs/design/*.md`, strips the
      leading frontmatter block from every discovered file before any rule runs, and offsets
      every finding's `Line` to match the un-stripped file. S0, S1, S5, and S6 run only on a
      file whose `type` is `Spec`; the frontmatter rule runs on every discovered file
      regardless of `type`.
- [ ] A design or spec file missing the block, missing a required key, carrying an invalid
      `type`/`status` enum value, or whose `parent` does not resolve to an existing file fails
      validation. `domain` is required only on a family root, a file with no `parent` key; a
      file that carries `parent` may omit `domain` and inherits it at read time.
- [ ] `atomic migrate --repo <path>` at target `1.3.0` adds the block to every
      `docs/design/*.md` and `docs/spec/*.md` file that lacks one; a file that already has the
      block is left byte-identical; re-running the step is a no-op.
- [ ] The finalize phase of `/subagent-implementation` and `/autopilot` stamps
      `status: shipped` and `shipped_sha` on every spec the task shipped, in the same commit as
      the signals refresh.
- [ ] Every pre-existing `docs/design/*.md` and `docs/spec/*.md` file in this repo carries the block; files whose `domain` could not be
      derived are hand-filled, none left without the rest of the block.


## Approach


Approach F: extend the OKF keys already on `docs/wiki/*.md` and bucket docs (`type`,
`description`, `status`) with `domain` and `parent`; see `docs/design/doc-consolidation.md`.


## Change tree


```
atomic/internal/doctemplate/templates/
├── spec.md ................................ M  (frontmatter block)
├── design-doc.md ........................... M  (frontmatter block)
atomic/internal/doctemplate/doctemplate_test.go  M  (block round-trips through Get)
atomic/internal/validate/
├── spec.go ................................. M  (discover both dirs, strip+offset,
│                                                  type-gated S0/S1/S5/S6, new S7 rule)
├── dispatch.go .............................. M  (isSpecPath and the whole-repo glob cover
│                                                  docs/design/*.md too)
├── spec_test.go ............................ M  (S7 cases, type-gating cases)
└── testdata/spec/
    ├── pass/S7/ ............................. A  (valid block fixtures, Spec and Design)
    ├── pass/design-no-checkpoints/ .......... A  (type: Design, no Checkpoints, passes whole)
    └── fail/S7/ .............................. A  (missing/invalid/broken-parent fixtures)
atomic/internal/migrate/
├── steps_docfrontmatter.go ................. A  (1.3.0 backfill step)
└── steps_docfrontmatter_test.go ............. A  (idempotence + derivation cases)
context/rules/specs/spec-currency.md ......... M  (frontmatter contract paragraph)
context/commands/
├── atomic-plan.md .......................... M  (planner fills the block)
├── subagent-implementation.md .............. M  (finalize stamps status+sha)
└── autopilot.md ............................ M  (finalize stamps status+sha)
docs/reference/conventions.md ................ M  (frontmatter contract row)
docs/design/*.md (every file) ................ M  (backfill)
docs/spec/*.md (every file) .................. M  (backfill)
```


## Outline


```
atomic/internal/validate/spec.go
  discovery glob — docs/spec/*.md and docs/design/*.md under the repo root
  RunSpecRules — strips the leading frontmatter block before any rule, offsets every
    finding's Line by the block's line height, always runs S7, runs S0/S1/S5/S6 only
    when the block's type is Spec
  S7 — frontmatter-block rule: presence, required keys, type/status enum values,
    domain required only when parent is absent, parent path resolution

atomic/internal/validate/dispatch.go
  isSpecPath — matches docs/spec/*.md and docs/design/*.md
  runWholeRepo — globs both directories before calling RunSpecRules

atomic/internal/migrate/steps_docfrontmatter.go
  docFrontmatterBackfill — 1.3.0 repo-scope step registered via init()
    deriveType — docs/design -> Design, docs/spec -> Spec
    deriveDescription — H1 text as a sentence
    deriveParent — prose markers (Parent spec:, Child of, Umbrella:, Continues, Grandparent)
    deriveDomain — filename prefix table; unresolved atomic-* left for hand fill

context/commands/subagent-implementation.md
  Phase 3 finalize — stamp status: shipped + shipped_sha on shipped specs, same commit
    as the signals refresh

context/commands/autopilot.md
  Phase 4 — same stamping step

context/rules/specs/spec-currency.md
  Frontmatter contract — the five keys, enum values, domain required only on family roots

docs/reference/conventions.md
  Doc frontmatter — human-facing description of the block and its keys
```


## Flows


**Flow: validating a frontmattered spec or design doc**

1. `atomic validate spec` with no path discovers `docs/spec/*.md` and `docs/design/*.md`
   under the repo root; given an explicit path of either kind, it validates that file alone
2. for each discovered file, `RunSpecRules` parses a leading frontmatter block via
   `frontmatter.Parse`; the block's line height is recorded and the remaining body is what
   any further rule runs against
3. S7 checks the parsed block: `type`, `description`, `status` present with valid values;
   `domain` is required only when `parent` is absent; a `parent` value must resolve to an
   existing file on disk
4. when the block's `type` is `Spec`, S0, S1, S5, and S6 also run against the stripped body;
   a `Design`-typed file, which carries no `## Checkpoints` section, skips them and is judged
   on S7 alone
5. every finding's `Line` is offset by the stripped block height before being returned, so
   it still points at the right line in the original file

**Flow: repo backfill migration**

1. `atomic migrate --repo <path>` runs at target `1.3.0`
2. the step globs `docs/design/*.md` and `docs/spec/*.md` under the repo root
3. for each file, `frontmatter.Parse` runs first; a file that already returns a non-nil
   `meta` is left untouched
4. otherwise: `type` from the containing directory, `description` from the H1, `parent` from
   the first matching prose marker, `domain` from the filename-prefix table; `status:
   shipped` by default
5. `frontmatter.Emit` writes the block back; a file whose `domain` could not be derived is
   written without that key and its path is collected into the step's summary output

**Flow: finalize stamping**

1. `/subagent-implementation` Phase 3 (or `/autopilot` Phase 4) reaches the signals-refresh
   step after the implementation loop's checkpoints are all green
2. for every `docs/spec/*.md` file the task's change set touched, the step parses its
   frontmatter, sets `status: shipped` and `shipped_sha` to the commit about to be made, and
   emits the file back, the same parse -> mutate -> emit shape as `wiki/stamp.go`'s
   `updateFrontmatterKey`
3. the stamped files are staged into the same `chore(signals)` commit as the refresh, not a
   separate commit


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Frontmatter block on both templates; planner and spec-currency instruct filling it | `atomic/internal/doctemplate/templates/spec.md`, `design-doc.md`, `doctemplate_test.go`, `context/rules/specs/spec-currency.md`, `context/commands/atomic-plan.md` | atomic-implementer (mode: feature) | ~5 | `atomic template spec` / `atomic template design-doc` output carries the block; `doctemplate_test.go` asserts it round-trips |
| 2 | Validator discovers `docs/spec/*.md` and `docs/design/*.md`, strips the block before any rule with line offset, gates S0/S1/S5/S6 to `type: Spec`, and adds S7 for presence/keys/enums/domain-conditional/parent | `atomic/internal/validate/spec.go`, `dispatch.go`, `spec_test.go` (inline fixtures gain the block; the golden test against `docs/spec/atomic-validate.md` keeps asserting zero FAILs), `docs/spec/atomic-validate.md` (gains the block here, ahead of the backfill, so the golden test stays green), `testdata/spec/pass/S7/`, `testdata/spec/pass/design-no-checkpoints/`, `testdata/spec/fail/S7/` | atomic-implementer (mode: feature) | ~7 | `go test ./internal/validate/...` green; a frontmattered spec no longer fails S0/S1; a `type: Design` fixture with no `## Checkpoints` passes; S7 fails on missing block, bad enum, dangling parent |
| 3 | Backfill migration step at `1.3.0`, idempotent, derives the five keys | `atomic/internal/migrate/steps_docfrontmatter.go`, `steps_docfrontmatter_test.go` | atomic-implementer (mode: feature) | ~2 | `go test ./internal/migrate/...` green; running the step twice on the same tree produces no second diff |
| 4 | Finalize phase stamps `status: shipped` + `shipped_sha` in the signals-refresh commit | `context/commands/subagent-implementation.md`, `context/commands/autopilot.md` | atomic-implementer (mode: surgical) | 2 | both commands describe the stamping step landing in the same commit as `chore(signals)`; `make -C atomic bundle` clean |
| 5 | Backfill every pre-existing spec and design file in this repo; hand-fill ambiguous `atomic-*` domains; document the contract | `docs/spec/*.md`, `docs/design/*.md`, `docs/reference/conventions.md` | atomic-implementer (mode: feature) | every spec and design file plus 1 | `atomic validate spec` reports 0 S7 FAILs across `docs/spec/` and `docs/design/`; every backfilled file's `domain` set or listed as a known gap in the commit message |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| `mdparse.IsATXOnly` treats a frontmatter closing `---` under `description: x` as a Setext underline, failing S0 on every frontmattered spec | high | strip the block before any `mdparse` call, not just before S0, in checkpoint 2 |
| Prose-marker parent derivation (`Parent spec:`, `Child of`, ...) misparses a file whose prose uses one of those phrases outside a family reference | med | the migration step lists every derived `parent` value in its summary output for a human skim before the backfill commit lands |
| The backfill touches every spec and design file in one PR, inflating the diff and colliding with any spec mid-edit on another branch | med | run the backfill last (checkpoint 5), after validator and migration land, so it is a single mechanical commit reviewable by diff shape alone |
| Domain derivation table misses an `atomic-*` prefix not yet catalogued | low | files with an undetermined domain keep the rest of the block and are surfaced in the step's summary, never silently dropped |


## Change log

<!-- Populated on first amendment after the spec is approved. Do not log drafting/refinement turns. -->

# atomic validate


## Goal


`atomic validate` lints repo artifacts for structural and referential integrity. Deterministic, fast, exit-1 on FAIL. Designed to run on a changed-file set in pre-commit or CI without LLM judgment.


v1 ships a tight, load-bearing rule subset (8 rules) that catches the actual invisible-feature bugs the validator exists for. Additional rules promote to v1.1 once the rule surface, output shape, and FAIL rate on real commits are observed.


## Non-goals


- Style or voice linting (markdownlint covers that).
- LLM-based review (`atomic-reviewer` agent's job).
- Running or executing artifacts.
- Auto-fixing content errors (human authorship boundary; `--suggest` prints *structural* templates only — empty section headings + skeletons, never name suggestions or fuzzy "did you mean").
- Resolving third-party skill/agent names installed in `~/.claude/` but not bundled. If users legitimately depend on third-party refs, add a future C-rule with an explicit allowlist (e.g. `config.thirdPartySkills` in `claude.local.md`) — do NOT implicitly peek into `~/.claude/`.
- Pre-commit hook wiring (`atomic hooks install --pre-commit`) — defer until v1 stabilizes and FAIL rate on real commits is known.
- Validating the bundled `CLAUDE.md` snapshot's refs against the *bundled* agent/skill set (orthogonal to working-tree validation). Deferred to v1.1.


## v1.1 deferrals (not in v1)


- D-rules (design validator) — D1–D6.
- S2, S3, S4, S7 (spec structure beyond H1 + Checkpoints + Change log).
- S8 (TODO/TBD scan) — if revived, scope to `docs/spec/` only; design docs legitimately carry open questions.
- C2 (reverse registry direction), C4 (skill-name extraction from prose), C6 (`/atomic-<name>` claim without `user-invocable`), C8 (dup skill names).
- F-rules (followups validator) — F1–F5.
- Bundle-snapshot ref validation (run C1/C5 against bundled `CLAUDE.md` too).
- `--format sarif` output (current `--json` reserves `schema_version: 1` for forward compatibility).


## Success criteria


- [ ] `atomic validate` (no args) runs all v1 validators on the whole repo from repo root.
- [ ] `atomic validate <paths...>` routes each path to the appropriate validator by location.
- [ ] `atomic validate spec|config|bundle [paths...]` runs the named validator only.
- [ ] Exit code: `0` on all PASS or only WARN; `1` on any FAIL; `2` on validator-internal error.
- [ ] Findings emitted one per line, indexed, rule-ID-tagged.
- [ ] Summary line ends each run: `<P> PASS, <W> WARN, <F> FAIL. exit <N>.`
- [ ] `--json` emits `{schema_version: 1, findings: [...], summary: {...}}`.
- [ ] `--suggest` prints structural templates for content-level FAILs without editing files. No name suggestions, no fuzzy matching.
- [ ] All validator code lives in `atomic/internal/validate/`; bundle-parity computation in `atomic/internal/manifestcheck/`; bundle inclusion predicates in `atomic/internal/bundlespec/`.
- [ ] `bundlespec/` is imported by both `bundlemirror/mirror.go` (build) and `manifestcheck/` (runtime) — single source of truth.
- [ ] Name resolution checks the working tree (`skills/*/SKILL.md`, `agents/*.md`), never `~/.claude/`.
- [ ] Markdown parsing uses `goldmark` AST (not regex); refs inside fenced/indented code blocks are NOT extracted.
- [ ] Project-root detection walks up for `.git`, treating it as **either a directory OR a file** (worktree case: `.git` is a file pointing at the main repo).
- [ ] Symlinks are resolved and deduped during tree walk (this repo's `scripts/link-local.sh` symlinks `.claude/` ↔ root dirs — must not double-count or loop).
- [ ] Canonical casing: `CLAUDE.md` (uppercase), `claude.local.md` (lowercase per existing repo). C5 resolution is case-sensitive. Document and FAIL on the wrong case.
- [ ] Test coverage: each v1 rule has at least one PASS and one FAIL fixture in `atomic/internal/validate/testdata/`.
- [ ] Soft perf budget: `atomic validate` on this repo completes in **<500ms** on a modern machine. Future rule additions fit within this envelope.
- [ ] CI integration is real: `.github/workflows/ci.yml` runs `atomic validate` and the step fails on exit 1.


## Subcommands (v1)


| Subcommand | Validates | Sources |
|------------|-----------|---------|
| `atomic validate spec [paths...]` | Spec structure | `docs/spec/*.md` |
| `atomic validate config` | Cross-reference integrity | `CLAUDE.md` + `agents/` + `commands/` + `skills/` + `claude.local.md` |
| `atomic validate bundle` | Bundle parity | `agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, `CLAUDE.md` ↔ `atomic/internal/embedded/` |
| `atomic validate` | All of the above | Whole repo |


v1.1 adds: `atomic validate design`, `atomic validate followups`.


Path-aware dispatch: `atomic validate docs/spec/foo.md other/path.md` routes the first to `spec`, ignores (with WARN) the second.


## v1 rules


### Spec validator


| ID | Rule | Severity |
|----|------|----------|
| S0 | File uses ATX headings (`# `, `## `) only — Setext (`===`/`---` underlines) rejected | FAIL |
| S1 | File starts with `# <title>` (H1) | FAIL |
| S5 | Has `## Checkpoints` section with table header `\| # \| Checkpoint \| Files/areas \| Verifies \|` | FAIL |
| S6 | Has `## Change log` section (may be empty under heading) | FAIL |


S0 exists because `mdparse` only handles ATX correctly; silent mis-parsing of Setext docs would produce wrong section bracketing. Loud rejection beats silent wrong.


### Config validator


| ID | Rule | Severity |
|----|------|----------|
| C1 | Every agent named in `CLAUDE.md` "Subagents available for dispatch" exists at `agents/<name>.md` | FAIL |
| C3 | Every `subagent_type: "<name>"` in `commands/*.md` resolves to `agents/<name>.md` or a built-in (`general-purpose`, `Explore`, `Plan`) | FAIL |
| C5 | Every `@-ref` in `CLAUDE.md`, `claude.local.md`, `CLAUDE.local.md` resolves to an existing path (case-sensitive) | FAIL |
| C7 | No duplicate `name:` across `agents/*.md` | FAIL |
| C9 | Files in `agents/`, `skills/`, `output-styles/` without the `atomic-` prefix (when not `_templates/` or similar known-skip dirs) | WARN |


C9 catches the silent-bundle-exclusion typo class (`agents/atomic_builder.md`, `agents/AtomicBuilder.md`) — the file exists but bundle inclusion misses it.


### Bundle validator


Single check (shared with future `atomic doctor` via `atomic/internal/manifestcheck/`):


- Compute expected bundle contents by walking working tree with `bundlespec.Matches(path) bool` predicates.
- Diff against committed `atomic/internal/embedded/bundle/` + `manifest.go`.
- FAIL on any diff. Print up to 5 differing paths.
- Resolve and dedupe symlinks before comparison.


No `go generate` invocation — pure read-and-compare. CI's existing `git diff --exit-code` gate stays canonical; this is the dev-loop counterpart.


## Output format


Default (human):


```
atomic validate config — referential integrity

[1] FAIL  C3  commands/foo.md:42  subagent_type "bar" — no agents/bar.md
[2] FAIL  C5  CLAUDE.md:118       @-ref .claude/missing.md does not resolve
[3] WARN  C9  agents/AtomicBuilder.md missing atomic- prefix; will not bundle

0 PASS, 1 WARN, 2 FAIL. exit 1.
```


JSON (`--json`):


```json
{
  "schema_version": 1,
  "findings": [
    {"index": 1, "severity": "FAIL", "rule": "C3", "path": "commands/foo.md", "line": 42, "message": "subagent_type \"bar\" — no agents/bar.md"}
  ],
  "summary": {"pass": 0, "warn": 1, "fail": 2, "exit": 1}
}
```


`schema_version: 1` is the forward-compat hedge — additional `--format` options (SARIF, etc.) ship as siblings, never replacements.


`--suggest` prints structural templates only for content-FAIL rules. Example for S5:


```
Suggestion for S5 in docs/spec/foo.md (insert before ## Change log):

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 |            |             |          |
```


Never suggests names, never fuzzy-matches against existing artifacts. The author writes the content; the tool only shapes the container.


## Exit codes


| Code | Meaning |
|------|---------|
| `0` | All PASS, or only WARN findings |
| `1` | One or more FAIL findings |
| `2` | Validator internal error (e.g. unreadable file, parse crash) |


## Package layout


| Path | Role |
|------|------|
| `atomic/internal/validate/` | Subcommand dispatch, rule runners, fixture-backed tests |
| `atomic/internal/validate/spec.go` | S0, S1, S5, S6 rules |
| `atomic/internal/validate/config.go` | C1, C3, C5, C7, C9 rules |
| `atomic/internal/validate/output.go` | Human + JSON formatters, `--suggest` structural templates |
| `atomic/internal/validate/testdata/` | PASS / FAIL fixtures per rule |
| `atomic/internal/bundlespec/` | Pure predicate package: `Matches(path) bool`. Thin leaf — small, no exported types beyond predicates. Imported by `bundlemirror/mirror.go` (build) and `manifestcheck/` (runtime) |
| `atomic/internal/manifestcheck/` | Bundle-parity diff: walks tree using `bundlespec`, compares to committed `embedded/` snapshot |
| `atomic/internal/mdparse/` | goldmark wrapper: section bracketing (group nodes by H2), table-by-header lookup, AST inline ref extraction (CodeSpan + Link, skips FencedCodeBlock/CodeBlock subtrees) |
| `atomic/cmd/atomic/main.go` | Wire `validate` subcommand under root |


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Scaffold `atomic validate` subcommand + flag parsing (`--json`, `--suggest`) | `atomic/cmd/atomic/main.go`, `atomic/internal/validate/validate.go` | `atomic validate --help` prints v1 subcommands; `go test ./internal/validate -run TestDispatch` |
| 2 | Extract bundle inclusion predicates into `bundlespec/`; rewire `bundlemirror/mirror.go` to import it. No behavior change | `atomic/internal/bundlespec/`, `atomic/internal/bundlemirror/mirror.go` | `make -C atomic bundle` produces byte-identical output; `go generate ./... && git diff --exit-code` clean |
| 3 | `manifestcheck/` package + `validate bundle` wiring. Symlink-aware tree walk (resolve + dedupe by inode/realpath) | `atomic/internal/manifestcheck/`, `atomic/internal/validate/bundle.go` | Tamper a bundle file → FAIL; clean tree → PASS; symlinked dir does not double-count |
| 4 | `mdparse/` package: goldmark + extension.Table wrapper. Section bracketing by H2-walking state. AST inline-ref extractor (skips fenced/indented code subtrees). Reject Setext loud. Add goldmark + table extension to `atomic/go.mod` | `atomic/internal/mdparse/`, `atomic/go.mod`, `atomic/go.sum` | Golden tests cover: ATX-only enforcement, sections bracketed across indented code + nested lists, refs inside fenced blocks NOT extracted |
| 5 | Spec validator (S0, S1, S5, S6) using `mdparse` | `atomic/internal/validate/spec.go`, `testdata/spec/{pass,fail}/*.md` | Per-rule PASS+FAIL fixture; `validate spec` on real `docs/spec/*.md` passes |
| 6 | Config validator (C1, C3, C5, C7, C9). Project-root via `.git` walk-up treating it as **file OR directory** (worktree case). Case-sensitive ref resolution | `atomic/internal/validate/config.go`, `testdata/config/*` | Synthetic repo fixtures per rule; `validate config` clean on this repo; worktree-from-`.worktrees/<branch>/` resolves to worktree root; case-mismatch `claude.local.md` vs `Claude.Local.md` → FAIL |
| 7 | Output formatters: human, `--json` (with `schema_version: 1`), `--suggest` structural templates only | `atomic/internal/validate/output.go` | Golden-file tests per format; `--suggest` never emits name strings from existing artifacts |
| 8 | Path-aware dispatch + whole-repo run + perf gate | `atomic/internal/validate/dispatch.go` | `atomic validate <mixed paths>` routes correctly; `atomic validate` on this repo completes in <500ms (measured by test) |
| 9 | Wire into `CLAUDE.md` (subcommand listing), `CLAUDE.md`, `README.md`, signals refresh, **and `.github/workflows/ci.yml`** as a real CI step | `CLAUDE.md`, `CLAUDE.md`, `README.md`, `.github/workflows/ci.yml` | Cross-artifact checklist green; CI fails on a synthetic broken `@-ref`; `atomic validate` clean on this repo's tip |


## Risks


| ID | Risk | Likelihood | Mitigation |
|----|------|-----------|-----------|
| R1 | Markdown section / table parser brittle on heading style variants, indented code, or non-standard tables | med | Use [`goldmark`](https://github.com/yuin/goldmark) + `extension.Table`. Walk AST tracking "current H2" to bracket sections (siblings under root — pattern from [`go.abhg.dev/goldmark/toc`](https://pkg.go.dev/go.abhg.dev/goldmark/toc)). Match tables structurally via `ast.Table`/`TableHeader`. S0 rule rejects Setext loud rather than silently mis-parsing |
| R2 | Ref extraction misses non-standard quoting OR false-positives inside fenced code examples | med | AST-walk `ast.CodeSpan` + `ast.Link` only; skip `ast.FencedCodeBlock` / `ast.CodeBlock` subtrees. Borrow ref-resolution model from [`felixgeelhaar/cclint`](https://github.com/felixgeelhaar/cclint) (TS prior art): project-root via `.git` walk-up, on-disk existence, circular detection, ≤5-hop |
| R3 | `--json` schema instability breaks CI consumers | low | Ship `{schema_version: 1, ...}` (matches ruff / golangci-lint convention). SARIF deferred to v1.1 as `--format sarif` if external CI integration is requested. Bump `schema_version` only on breaking changes |
| R4 | Bundle inclusion rules drift between `bundlemirror/mirror.go` and `manifestcheck/` | high | Extract predicates into `atomic/internal/bundlespec/` — both sides import. Predicates are pure functions, no state, no version field (rules change via code review, not version bumps). If predicates ever start *changing semantics*, revisit and add versioning then |
| R5 | Config validator FAILs on legitimate in-flight artifacts during PR review | med | Resolve names against working tree only; never read `~/.claude/`. C9 stays WARN. Project-root via `.git` walk-up treating `.git` as **directory OR file** (in a git worktree, `.git` is a file pointing at the main repo's worktree dir — hand-rolled walk-up logic must handle both) |
| R6 | Symlink loops or double-counting in `manifestcheck` tree walk — `scripts/link-local.sh` symlinks `.claude/` ↔ root dirs in this repo | high | Resolve symlinks via `filepath.EvalSymlinks` and dedupe by realpath before comparison. Test fixture must include a symlinked dir to prevent regression |
| R7 | Case sensitivity differences between macOS (insensitive) and Linux/CI (sensitive) cause silent breakage | med | Canonical casing pinned: `CLAUDE.md` (upper), `claude.local.md` (lower). C5 ref resolution is byte-exact, case-sensitive. Wrong case → FAIL. Document the canonical set in spec and `CLAUDE.md` |
| R8 | Perf regression as rules accumulate | low | <500ms soft budget on this repo, asserted in checkpoint 8 test. New rules must demonstrate they fit |


## Change log


<!-- Drafting/refinement edits before approval are not logged. First entry is at v1 ship. -->


## Implementation log


### v1 — 2026-05-17


Built across 10 iterations of `/subagent-implementation` (9 spec checkpoints + 1 inserted dogfood cleanup + 1 polish pass). Commits, chronological:


- `007bfaa` — CP-1 scaffold `validate` subcommand + `--json` / `--suggest` flag parsing.
- `841a1b2` — CP-2 extract bundle inclusion predicates into `bundlespec/`.
- `0547486` — CP-3 `manifestcheck/` package + `validate bundle` wiring; symlink-aware tree walk.
- `f4358ca` — CP-4 `mdparse/` goldmark wrapper (sections, ATX-only, table-by-header, inline refs).
- `fbea6f3` — CP-5 spec rules S0/S1/S5/S6, JSON output, flag-after-subcommand fix.
- `5609e64` — CP-5.5 (inserted) conform 7 pre-existing specs to S5/S6.
- `d0655a0` — CP-6 config rules C1/C3/C5/C7/C9.
- `803bb9a` — CP-7 unified output formatters across all subcommands.
- `0ad2cb0` — CP-8 whole-repo + path-aware dispatch; perf-budget test.
- `4aa3ed8` — CP-9 wire into `CLAUDE.md`, `README.md`, `.github/workflows/ci.yml`.
- `91b6c8c` — polish pass closing F-6, F-11, F-15, F-17, F-19.


**Out-of-scope work performed during this build:**


- **CP-5.5 (spec-cleanup iteration, not in original spec)** — after CP-5 landed, dogfood surfaced 9 real defects in pre-existing `docs/spec/` files (missing Change log sections, legacy `| CP | Lands |` table headers). User chose to modernize the legacy specs rather than relax the new S5 rule. 7 specs amended in place via append-mostly convention.


**Unforeseens — surprises that emerged during implementation:**


- **`IsATXOnly` line-prescan was buggy** — CP-4 chose line prescan over goldmark AST for Setext detection because goldmark Heading nodes don't differentiate Setext from ATX. The initial prescan didn't track fenced code-block state, so `---` inside YAML or markdown code blocks triggered false S0 positives on 3 of 7 dogfood specs. Caught at CP-5 round 2 review; round 2 surgery added fence-tracking state machine. Spec R1 had warned about exactly this failure mode.
- **CRLF Setext false-negative** — CP-4 round 1 reviewer caught that `bytes.TrimRight(line, " \t")` did not strip `\r`, so CRLF files containing Setext silently passed S0. Round 2 fixed.
- **Bundle integration tests' embedded-manifest mismatch** — synthetic-tempdir bundle tests cannot cleanly assert `validate bundle` exit code because the binary's embedded manifest is generated from the real repo, not the test fixture. Documented as deferred follow-up `atomic-validate-F-8`.


**Deferred items still open** (promoted to `.claude/project/followups.md` with topic-prefixed ids):


- `atomic-validate-F-5` — `MatchesSkillDir` doc/test on file-vs-dir contract.
- `atomic-validate-F-7` — `manifestcheck` symlink-loop test placed outside walked dirs (vacuous).
- `atomic-validate-F-8` — bundle integration tests accept both exit codes.
- `atomic-validate-F-10` — bundle test seeding duplicated across packages.
- `atomic-validate-F-12` — `FindTableByHeader` line-number depends on goldmark version-specific behavior.
- `atomic-validate-F-13` — empty ATX heading falls back to `startLine = 1`.
- `atomic-validate-F-14` — indented-code Setext test passes vacuously.
- `atomic-validate-F-16` — `session-report.md` Checkpoints section placement loose.


Closed during the build (dropped from ledger; commits cover them): F-1 (flag-after-subcommand), F-2 (flag test strengthened), F-3 (no-subcommand message), F-4 (`IsClaudeMd` doc → consumer), F-6 (`manifestcheck` uses `IsClaudeMd`), F-9 (bundle output rewired through formatter), F-11 (drop unused parseAST return), F-15 (rewrite misleading comment), F-17 (TextSegments callsite docs), F-18 (rename stale test), F-19 (`lineOfMatch` documentation strengthened).

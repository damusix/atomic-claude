# Embedded SQL language expansion

## Goal

Extend embedded-SQL extraction (SQL in host-language string literals → table/column/edge nodes with `Provenance: "embedded"`) from the 4 shipped host languages (Go, Python, TS, TSX) to the 16 remaining engine languages: C, C++, C#, Java, JavaScript, Kotlin, Lua, Luau, Objective-C, Pascal, PHP, Ruby, Rust, Scala, Swift, Dart. Done via one config-driven generic tree-sitter harvester plus a per-language node-kind config table — not 16 bespoke files.

## Non-goals

- Re-architecting the 4 shipped harvesters (Go scanner, Python docstring-aware walk, TS/TSX walk). They stay as-is; unifying them onto the generic harvester is a deliberate future boundary.
- New SQL parsing — the `standalone` SQL extractor, `IsSQLLiteral` gate, and `ExtractEmbeddedSQL` entry point are reused verbatim.
- Multi-fragment / concatenated queries (`"SELECT " + cols`) — accepted false negative (unchanged).
- Vue/Svelte SFC inline staleness (`followup-hardening-f-4`).
- JSX/Liquid/XML/YAML/Twig host extraction. JSX folds into the JavaScript grammar; the rest are not general-purpose code hosts.
- Perfect Shape-2 trailing-delimiter stripping (see Known boundaries).

## Success criteria

For **each** of the 16 languages, an orchestrator-level (real index) test proves:

- [ ] A source file with `CREATE TABLE` inside a string literal of that language's primary form is indexed with at least one `table` node attributed to that file, with correct file-absolute `StartLine`/`EndLine`.
- [ ] DML (`SELECT`/`INSERT`/`UPDATE`/`DELETE`) inside a string literal produces `UnresolvedReference` entries owned by the enclosing host function/method node, or the file node when no containing function exists.
- [ ] All embedded edges and unresolved refs carry `Provenance: "embedded"`, queryable via `GetEdgesByProvenance("embedded")`.

Cross-language criteria:

- [ ] Languages with a secondary string form covered in their config (C++ raw strings, C# verbatim + interpolated, Java text blocks, Kotlin/Scala/Swift triple-quoted, Lua long brackets `[[…]]`, PHP/Ruby heredocs, Rust raw strings) extract SQL from that form too — proven by at least one test per language that uses the form most likely to carry SQL (heredoc/raw/triple-quoted where idiomatic).
- [ ] Interpolated string where the **table target** is an interpolation segment (e.g. Ruby `"SELECT a FROM #{t}"`, Dart `"… FROM $t"`, Scala `s"… FROM $t"`, C# `$"… FROM {t}"`, Swift `"… FROM \(t)"`, PHP `"… FROM $t"`, JS `` `… FROM ${t}` ``) produces zero refs from that literal (interpolation → `?` placeholder → no identifier after FROM). Proven for at least the interpolation-bearing languages (C#, Kotlin, PHP, Ruby, Scala, Swift, Dart, JavaScript).
- [ ] Interpolated string where the table is a plain identifier and only a **value** is interpolated (e.g. Ruby `"SELECT a FROM users WHERE id = #{id}"`) emits a normal reference edge to `users`. Proven for at least one interpolation-bearing language.
- [ ] Prose strings (`"choose an item from the dropdown"`) fail the gate and produce zero nodes/edges. (Gate is unchanged; one regression test on a new-language file suffices.)
- [ ] The corpus/admission harness covers at least one new language and the zero-resolved-phantom-edge bar holds: `GetEdgesByProvenance("embedded")` resolves only to nodes that exist.
- [ ] The 4 shipped languages (Go/Python/TS/TSX) behave identically — all their existing tests pass unchanged.
- [ ] `standaloneExts` routing (`.sql`/`.ddl`/`.pgsql`/`.mysql`) is unchanged.
- [ ] The new-language host-ext set is **derived** from `extToLanguage` + the config table (single source), not a second hand-maintained ext list.
- [ ] `go test ./...`, `go vet ./...`, `gofmt -l .` clean after each checkpoint.

## Grammar node-kind config (ground truth — probed)

Canonical config table. `extraction.Lang` key → `{StringKinds, ContentKinds, InterpKinds}`. Probed against the live wazero/tree-sitter grammars; do not alter without re-probing.

| extraction.Lang | StringKinds | ContentKinds | InterpKinds |
|-----------------|-------------|--------------|-------------|
| LangC | `string_literal` | `string_content` | — |
| LangCpp | `string_literal`, `raw_string_literal` | `string_content`, `raw_string_content` | — |
| LangCSharp | `string_literal`, `interpolated_string_expression`, `verbatim_string_literal` | `string_literal_content`, `string_content` | `interpolation` |
| LangJava | `string_literal` | `string_fragment`, `multiline_string_fragment` | — |
| LangJavaScript | `string`, `template_string` | `string_fragment` | `template_substitution` |
| LangKotlin | `string_literal` | `string_content` | `interpolated_identifier`, `interpolation` |
| LangLua | `string` | *(none — Shape 2)* | — |
| LangLuau | `string` | `string_content` | — |
| LangObjC | `string_literal` | `string_content` | — |
| LangPascal | `literalString` | *(none — Shape 2)* | — |
| LangPHP | `encapsed_string`, `heredoc` | `string_content` | `variable_name` |
| LangRuby | `string`, `heredoc_body` | `string_content`, `heredoc_content` | `interpolation` |
| LangRust | `string_literal`, `raw_string_literal` | `string_content` | — |
| LangScala | `string`, `interpolated_string` | *(none — Shape 2)* | `interpolation` |
| LangSwift | `line_string_literal`, `multi_line_string_literal` | `line_str_text`, `multi_line_str_text` | `interpolated_expression` |
| LangDart | `string_literal` | *(none — Shape 2)* | `template_substitution` |

## Harvester algorithm (the contract)

`HarvestEmbeddedLiterals(ctx, inst, src, lang, cfg) -> ([]EmbeddedSpan, error)` where `EmbeddedSpan{Text string; StartLine int; EndLine int}` mirrors `standalone.StringLiteralSpan` field-for-field. The harvester lives in package `extraction`, which cannot import `standalone` (`standalone` imports `extraction` — cycle); the CP2 indexer adapter converts `EmbeddedSpan` → `standalone.StringLiteralSpan`, exactly as the existing `PythonLiteralSpan`/`TSLiteralSpan` adapters do.

1. `inst.SetLanguage(lang)`, parse, build line-offset table (reuse `buildLineOffsets` + the `pyByteToLine` binary search — already in `extraction`).
2. DFS the named tree. At a node whose kind ∈ `cfg.StringKinds`: harvest it, do **not** recurse into it. Otherwise recurse.
3. Harvest one string node:
   - Collect, by source-order descendant walk, the set of descendants whose kind ∈ `ContentKinds` (content) and ∈ `InterpKinds` (interpolation).
   - **Shape 1** (≥1 content descendant): walk descendants in source order; content → append its source text; interpolation → append `"?"`; join.
   - **Shape 2** (no content descendant): take the node's own source text; for each interpolation descendant, in **descending** byte order, splice `"?"` over its byte range (relative to node start); then strip a leading run and trailing run of delimiter-alphabet chars `" ' \` @ [ ] =`.
   - Return `nil` for an empty result (skip).
   - `StartLine`/`EndLine` = file-absolute 1-based lines of the node's start/end bytes.
4. Best-effort: a `Kind()`/byte error on any node skips that subtree, never aborts the file (matches existing harvesters).

`EmbeddedLiteralConfig.StringKinds/ContentKinds/InterpKinds` are `map[string]bool` (O(1) membership — per user preference for set lookups over slice scans).

## Approaches

| # | Approach | Sketch | Cost | Risk |
|---|----------|--------|------|------|
| A | One generic config-driven harvester + per-Lang node-kind config table; new langs only; existing 4 untouched | Reuse proven 16×; single walk; config is the only per-lang surface | med | Shape-2 trailing-delim over-trim (bounded, documented) |
| B | 16 bespoke `<lang>_literals.go` files (clone python/ts per language) | Follows existing pattern literally | high | 16× duplication; 16× drift surface; rejected |
| C | Unify all 20 langs (incl. Go/Python/TS/TSX) onto the generic harvester | Cleanest end state | high | Regresses tested Python docstring + Go scanner paths; out of scope |

## Recommendation

Approach A. The Python/TS harvesters are provably the same walk modulo three node-kind sets (design doc table); 16 more consumers make the generic harvester reuse-justified. Existing tested paths stay untouched (surgical). Node kinds are probed ground truth, not guesses. Host-ext set derived from the existing `extToLanguage` authority + config table — no second ext list (the `embedded-sql-ext-list-dup` lesson).

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|---------------|-------|------------|----------|
| 1 | Generic harvester engine | `extraction/embedded_literals.go` (NEW), `extraction/embedded_literals_test.go` (NEW) | atomic-builder | 2 | `HarvestEmbeddedLiterals` + `EmbeddedLiteralConfig`; unit tests parsing real fixtures proving all four modes: content-child (C/Java), content-child+interp (C#/Ruby), inline (Lua/Pascal), inline+interp (Dart/Scala); delimiter stripping; file-absolute lines; f-string/template interpolation → `?` |
| 2 | Config table + orchestrator wiring | `indexer/embedded_literals_config.go` (NEW), `indexer/embedded_sql_postpass.go`, `indexer/orchestrator.go` | atomic-builder | 3 | All 16 configs present; `init()` derives new host exts from `extToLanguage`+config (single source, no second ext list); generic harvester dispatched for new langs; go/py/ts/tsx registry entries unchanged; smoke e2e for one language per mode (e.g. Java, C#, Lua, Dart) producing embedded table node + ref with `Provenance: "embedded"` |
| 3 | Per-language end-to-end coverage | `indexer/embedded_sql_lang_test.go` (NEW) | atomic-builder | 1-2 | One orchestrator-level test per remaining language (C, C++, Kotlin, Luau, ObjC, PHP, Ruby, Rust, Scala, Swift, Pascal, JavaScript): DDL → table node (file-absolute lines), DML → owned ref, `Provenance: "embedded"`; secondary forms (raw/heredoc/triple-quoted/long-bracket) where idiomatic; interpolated-table-target → zero refs and interpolated-value → real ref for the interpolation-bearing langs; one prose-rejection test |
| 4 | Corpus / admission harness | `scripts/code-eval/embedded-sql-eval.sh`, `cmd/embedded-sql-admission/main.go` (if needed) | atomic-builder | 1-2 | Harness exercises at least one new language; zero-resolved-phantom-edge assertion holds across the expanded language set; admission count reported; existing two-condition guard (>0 nodes AND ≥1 embedded edge) preserved |
| 5 | Docs + signals + impl log | `docs/reference/code-intel.md`, `docs/spec/embedded-sql-language-expansion.md` (impl log), `.claude/project/signals.md` | atomic-builder | 2-3 | code-intel reference notes embedded-SQL now covers 20 languages; spec impl log written; signals refreshed |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Probed node kind wrong for an untested grammar form | Low | CP3 per-language e2e is the proof; a wrong kind shows as a missing node and fails the test in-loop |
| Shape-2 trailing-delimiter over-trim mangles a DML tail | Low | Bounded: only clips after the table ref; cannot invent an identifier → zero-phantom bar holds; documented boundary |
| Kotlin `${expr}` interpolation kind differs from probed `$ident` | Low | `interpolation` included defensively; degrades to gate-handled text; corpus run backstops |
| Heredoc node (PHP/Ruby) harvested twice (outer + inner node both matched) | Med | StringKinds chosen so only the body-bearing node matches; recursion stops at a StringKinds node; CP1/CP3 tests assert distinct, non-duplicated nodes |
| Owner-node attribution wrong for languages where functions nest differently | Low | `findOwnerNode` is language-agnostic (narrowest span containment); CP3 asserts ownership per language |
| New ext accidentally double-routed (host post-pass + standalone) | Low | Post-pass runs only for tree-sitter-extracted host files; `standaloneExts` unchanged; CP2 asserts no `.sql` regression |

## Known boundaries

- **Shape-2 trailing delimiters.** A DML literal in an inline-content grammar (Lua/Pascal/Dart/Scala/C#-verbatim) that ends in an embedded SQL quoted string may have its trailing quote clipped by delimiter stripping. The table reference (after `FROM`) is already captured; the clip only affects trailing tokens and never creates a phantom edge. Accepted, same class as concatenated-query false negatives.
- **Multi-fragment queries.** Unchanged accepted false negative.
- **Go/Python/TS/TSX** keep their bespoke harvesters; this expansion does not touch them.

## Change log

<!-- Populated on first post-approval amendment. -->

## Implementation log

### Shipped — 2026-06-10

Built via `/autopilot` (the `/subagent-implementation` loop), 8 iterations (each checkpoint + its reviewer-finding fix pass). Branch `embedded-sql-extraction`, stacked on the prior embedded-SQL + followup-hardening work. Commits (chronological, pre-squash):

- `8306ca0` — plan (design + spec)
- `8f33a60` — CP1 generic config-driven harvester engine (`HarvestEmbeddedLiterals`, Shape 1/Shape 2) + unit tests; reviewer fixes folded in (exact assertions, stop-at-leaf, delimiter alphabet)
- `78b8bb4` — CP2 16-language node-kind config table + orchestrator wiring (host exts derived from `extToLanguage`, single source) + 4 smoke e2e; reviewer fixes folded in (exact line assertions, init-ordering comment)
- `5a5623d` — CP3 per-language e2e for the 12 remaining languages + secondary forms + interpolation + prose rejection; reviewer fixes folded in (7 `FromNodeID` ownership assertions)
- `04e8f3a` — CP4 multilang fixture corpus + CI-durable zero-phantom integrity test + shell-harness `multilang` corpus id; reviewer nit folded in (dangling-dump completeness)
- CP5 — docs (`docs/reference/code-intel.md`) + this implementation log

**Out-of-scope work performed during this build:**

- Corrected a stale `docs/reference/code-intel.md` line that still named the `offsetResult` mechanism (removed in the followup-hardening batch) for embedded line numbers. Minor doc-accuracy fix made while updating the same section.

**Unforeseens — surprises that emerged during implementation:**

- Import cycle: `standalone` imports `extraction`, so the harvester (in `extraction`) cannot return `standalone.StringLiteralSpan`. Resolved with `extraction.EmbeddedSpan` (field-for-field mirror), converted at the CP2 indexer adapter — exactly how the existing `PythonLiteralSpan`/`TSLiteralSpan` types already work. Spec body updated to match.
- Grammar line attribution: Ruby `<<~SQL` heredocs report the node start at the first content line, and Swift `"""` multi-line literals at the first content line, not at the opener line. File-absolute and correct; tests assert the exact (content) line.
- Per-grammar string shapes split three ways (content-child, inline, heredoc), unified into two harvest branches (content-descendant reconstruction vs. inline text with byte-range interpolation substitution + delimiter stripping). Node kinds were probed from the live grammars rather than guessed.

**Deferred items still open:** none — every reviewer finding (blocking and non-blocking) was addressed in-iteration per the autopilot contract. The Go/Python/TS/TSX bespoke harvesters were deliberately left on their existing tested paths (a documented non-goal, not a deferral); unifying them onto the generic harvester remains available as future work.

**Squashed to 19b4782 — 2026-06-10.** Branch re-squashed (original feature + followup-hardening + the 16-language expansion) into a single commit; per-iteration SHAs above are historical and unreachable from any branch.

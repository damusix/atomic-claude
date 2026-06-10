# Followup hardening batch (pre-expansion)

## Goal

Close all 9 open followups blocking the embedded-SQL language expansion. After this batch: the shared embedded-SQL core is correct (node IDs are file-absolute, DML gate is tighter, harness cannot pass vacuously), the ext-list duplication is eliminated, weak test assertions are exact, and the mdlink/gorilla nits are resolved.

## Non-goals

- The embedded-SQL language expansion itself (separate plan).
- Other open ledger items: doctor findings, signals-router findings, install findings — orthogonal to this batch.
- Vue/Svelte node-ID staleness — pre-existing, same root cause as the collision fix (node IDs minted from relative lines, then shifted via inline `contentLineOffset` without ID regen) but a separate code path, out of scope; a future pass can apply the same padding trick to the SFC extractors.
- Adding a real gorilla/mux app to the eval corpus.

## Success criteria

- [ ] `go test ./...` green from `atomic/` (no new failures, no skipped tests).
- [ ] `go vet ./...` clean.
- [ ] `gofmt -l .` returns no output.
- [ ] `go build ./...` succeeds.
- [ ] Node-ID collision: a test with two same-named DDL literals at different lines produces two nodes with distinct IDs. If either same-named DDL node is dropped (replaced via `INSERT OR REPLACE`), the test fails.
- [ ] Multiline-DDL offset: `TestExtractEmbeddedSQL_MultiLineDDLOffset` uses exact-equality assertions on `StartLine`; a doubled offset does not pass.
- [ ] LineOffset node-ID: `TestExtractEmbeddedSQL_LineOffset` asserts node IDs in addition to line numbers; the new assertion would fail if `offsetResult` were re-introduced on the embedded path.
- [ ] Harness vacuous-pass: `embedded-sql-eval.sh` exits non-zero when total node count is 0 or `EMBEDDED_COUNT` is 0 on the known-non-empty `atomic/` corpus.
- [ ] DML gate: `UPDATE` prose without a `SET` token is rejected; real `UPDATE … SET …` is admitted.
- [ ] Ext-list single source: `resolution/pipeline.go` and `indexer/orchestrator.go` both consume an exported ext constant from the standalone package; a parity test fails if the two consumers diverge.
- [ ] Eval-tool nits: dead `report.txt` echo removed from `embedded-sql-eval.sh`; skipped-file count printed and non-zero exits non-zero from the admission tool; README documents single/triple-quoted harvester limitation.
- [ ] Gorilla stale comment: `resolution/frameworks/golang.go:492` no longer contains "Only consider if the window doesn't have a newline…"; `extractGorillaMethods` behavior is unchanged.
- [ ] mdlink fence: a 4-backtick outer fence containing a 3-backtick inner block is linkified correctly; inner fence lines are not treated as block boundaries. Fence closes only on a same-character run at least as long as the opener.
- [ ] All 9 followups closed via `atomic followups close`.

## Approaches

Node-ID collision only — the one finding with real design divergence.

| # | Approach | Pros | Cons |
|---|----------|------|------|
| A | Pad `startLine-1` newlines before calling the SQL extractor | No ID remap; fixes node IDs, ref IDs, and edge lines in one move; deletes the embedded `offsetResult` call; zero changes to `sql.go` | Allocates a padded string copy per admitted literal (few literals in practice — 126 over 22,847 scanned in the `atomic/` corpus); column numbers stay literal-relative (already true today) |
| B | Post-hoc ID regen + old→new edge/ref remap in `offsetResult` | Could fix Vue/Svelte latent staleness in the same pass | Cannot re-derive the per-call-site name input to `GenerateNodeID` — name varies: `name` at `standalone.go:82`, `qname` at `sql.go:389`, `policyName` at `sql.go:891`; fragile against every future extractor change; most code of the three options |
| C | Thread a `lineBase` parameter through `sql.go` | Explicit, no allocation | ~16 `GenerateNodeID` call sites in `sql.go` touched; invasive for the same end result as A |

## Recommendation

Approach A. The padding is applied once at `ExtractEmbeddedSQL` entry, behind the admission gate, so only admitted literals pay the allocation. The root cause of post-hoc regen being unsafe is that the name input to `GenerateNodeID` varies per call site — `name` at `standalone.go:82`, `qname` at `sql.go:389`, `policyName` at `sql.go:891` — so a remap function would have to re-derive which string fed each node's hash, which is fragile. Approach A avoids this entirely.

After CP1 the embedded path stops calling `offsetResult`. `offsetResult` then has zero production callers — the Vue/Svelte SFC extractors apply their own `contentLineOffset` inline (`standalone.go`) and never called it. `offsetResult` (and the CP3 ref-ID regen inside it) becomes dead code; CP6 removes it. The Vue/Svelte path has the same latent node-ID staleness via its inline `contentLineOffset`, but that is a separate code path and stays out of scope (see Non-goals).

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|-----------|---------|
| CP1 | Node-ID collision fix | `extraction/standalone/embedded_sql.go` — pad `startLine-1` newlines before extraction, stop calling `offsetResult` on the embedded path; `extraction/standalone/embedded_sql_test.go` — collision test + updated LineOffset/MultilineDDLOffset tests | atomic-builder | 2 | Collision test: two same-named DDL nodes at different lines have distinct IDs; removing either node causes the test to fail. `TestExtractEmbeddedSQL_LineOffset` asserts node IDs. `TestExtractEmbeddedSQL_MultiLineDDLOffset` uses exact-equality on `StartLine`. |
| CP2 | DML gate tightening | `extraction/standalone/embedded_sql.go` — when the DML start verb is `UPDATE`, additionally require `SET` present in the literal; `extraction/standalone/embedded_sql_test.go` — UPDATE-prose FP test (no SET) + UPDATE-with-SET admit test | atomic-builder | 1–2 | `UPDATE available: version %s` rejected; `UPDATE users SET name = $1 WHERE id = $2` admitted. |
| CP3 | Harness vacuous-pass guard | `scripts/code-eval/embedded-sql-eval.sh` — require total node count > 0 AND `EMBEDDED_COUNT` ≥ 1 before issuing PASS | atomic-surgeon | 1 | Script exits non-zero when run against an empty index or when `EMBEDDED_COUNT` is 0. |
| CP4 | Ext-list single source | `extraction/standalone/` — export a canonical ext constant/set; `indexer/orchestrator.go` — consume exported constant; `resolution/pipeline.go` — consume exported constant; add parity test | atomic-builder | 3–4 | Parity test fails if either consumer diverges from the exported source. |
| CP5 | mdlink nested-fence fix | `atomic/internal/mdlink/mdlink.go` — track opener length and character; a fence closes only on a same-character run at least as long as the opener (CommonMark); `atomic/internal/mdlink/mdlink_test.go` — nested-fence test | atomic-builder | 2 | A 4-backtick outer fence containing a 3-backtick inner block: inner lines are linkified correctly, not treated as fence boundaries. Existing fence tests continue to pass. |
| CP6 | Eval-tool nits + gorilla comment + dead-code cleanup + close followups | `scripts/code-eval/embedded-sql-eval.sh` — drop dead `report.txt` echo; `scripts/code-eval/` admission tool `main.go` — count skipped entries and print a `SKIPPED_COUNT` line so the undercount is no longer silent; keep exit 0 on the normal path (non-zero stays reserved for the existing fatal walk-error case), because `eval.sh` (line 191) degrades its step-3 admission-surface output on ANY non-zero admission exit; `scripts/code-eval/README.md` — document single/triple-quoted harvester limitation; `resolution/frameworks/golang.go:492` — remove stale comment "Only consider if the window doesn't have a newline…" (fix for `gorilla-multiline-methods` already landed in commit `045e5ce`; only the contradicting comment remains); `extraction/standalone/standalone.go` — remove dead `offsetResult` (zero callers after CP1; verify no test caller first) plus any import left unused; `extraction/standalone/embedded_sql.go` + `embedded_sql_test.go` — reword stale `offsetResult` comments to the newline-padding mechanism; finally close all 9 followups via `atomic followups close <id>` | atomic-surgeon | 5–6 | Stale comment absent from `golang.go:492`. Skipped-file output visible when files are unreadable. README has harvester-limitation note. `offsetResult` removed, `go build ./...` + `go test ./...` green. No `offsetResult` references remain. Admission tool prints `SKIPPED_COUNT` and still exits 0 on the normal corpus run (eval.sh step 3 still shows the admission surface). All 9 followup ids absent from `.claude/project/followups/INDEX.md` open list, present in `CLOSED.md` (closed by the orchestrator at finalization). |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Newline padding changes column-relative semantics | Low | Columns are already literal-relative today (not file-absolute); padding adds newlines before the text so column offsets within each line are unchanged. |
| Ext-list refactor misses a third consumer | Low | `grep -rn "standaloneExts\|isStandaloneSQLExt"` before and after the change; the parity test catches divergence at runtime. |
| mdlink fence-length change regresses existing fence tests | Low | Run `mdlink_test.go` — any regression surfaces immediately. The existing tests use 3-backtick fences, which are unaffected: a 3-backtick fence still closes on a 3-backtick run. |
| Gorilla comment removal touches live logic by mistake | Very low | The comment is on a standalone line at `golang.go:492`; `extractGorillaMethods` call is on `:494`. Surgical deletion of one line; verify with `go test ./...`. |

## Implementation log

### Shipped — 2026-06-10

Built across 6 checkpoints of /subagent-implementation (stacked on the embedded-sql-extraction branch). Commits (chronological):

- `3206e69` — plan (design + spec)
- `217091f` — CP1 embedded node IDs file-absolute via newline padding (+ collision/offset/node-ID tests)
- `4c74ec9` — spec amend: offsetResult dead after CP1
- `9101b71` — CP2 embedded UPDATE requires SET
- `aa1e51d` — CP3 harness guarded against vacuous PASS
- `b70fb7b` — CP4 SQL ext list single-sourced (SQLExtensions/IsSQLExt) + parity test
- `6539c7a` — CP5 mdlink CommonMark fence-length close (nested + tilde)
- `52fcec7` — spec amend: CP6 admission-tool exit behavior
- `0c27275` — CP6 remove dead offsetResult + tidy eval tooling (gorilla comment, SKIPPED_COUNT, README, dedupe)

**Out-of-scope work performed during this build:**
- Removed dead `offsetResult` + reworded its stale comments (F-1/F-2/F-3) — surfaced by the CP1 reviewer; folded into CP6.
- Routed a third SQL-ext consumer (`standalone.NewRegistry`) through the canonical source in CP4 — the parity goal required it.
- Deduped a redundant CP4 test subtest (F-5) in CP6.

**Unforeseens — surprises that emerged during implementation:**
- gorilla-multiline-methods was already fixed in `045e5ce`; only a stale contradicting comment remained (CP6 removed it). No re-implementation.
- `offsetResult` had zero callers once the embedded path stopped using it (Vue/Svelte inline `contentLineOffset`, never called it) — contradicted the spec's premise; spec amended, dead code removed.
- The spec's original "admission tool exits non-zero if any file skipped" would have hidden eval.sh's step-3 admission surface; corrected to SKIPPED_COUNT + exit 0.

**Deferred items still open:**
- `followup-hardening-f-4` (project followup) — Vue/Svelte node-ID staleness via inline `contentLineOffset`, a separate code path (non-goal here). Fix: apply the same padding trick to the SFC extractors.

**Squashed to b439d18 — 2026-06-10.** Per-iteration SHAs above are historical (unreachable from any branch).

## Change log

<!-- new entries prepended here -->

### 2026-06-10 — CP6 admission-tool exit behavior corrected

Pre-CP6 review of `eval.sh` (line 191) found it degrades its step-3 admission-surface output on ANY non-zero admission-tool exit. The original CP6 criterion "exit non-zero if any file skipped" would therefore hide the admission surface whenever any entry is skipped (always, given skipDirs).

- Changed: CP6 now prints a `SKIPPED_COUNT` line and keeps exit 0 on the normal path; non-zero stays reserved for the existing fatal walk-error case.
- Superseded: "exit non-zero if any skipped" (harmful — conflicts with the harness the CP3 guard validated).

### 2026-06-10 — CP1 discovery: offsetResult is dead code

CP1 (commit `217091f`) implemented the newline-padding fix. The reviewer verified that after removing the embedded path's `offsetResult` call, `offsetResult` has zero production callers: the Vue/Svelte SFC extractors apply `contentLineOffset` inline and never called it.

- Changed: Recommendation no longer claims "offsetResult retains its behavior for Vue/Svelte." It now states offsetResult becomes dead code after CP1 and CP6 removes it.
- Changed: CP6 scope extended to remove dead `offsetResult` (+ stale-comment rewording in the CP1 files). Est. files 3–4 → 5–6.
- Changed: Non-goals clarifies the Vue/Svelte path uses inline `contentLineOffset`, not `offsetResult`.
- Superseded: prior text asserting Vue/Svelte are offsetResult consumers (factually wrong; they inline their offset).

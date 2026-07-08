# Graphignore: repo-scoped ignore globs for the code-intel index

## Goal


A committed `.claude/atomic.toml` with `[code] ignore = ["<glob>", ...]` excludes matching files from the code-intel index at discovery time, and files that become ignored are pruned from an existing index on the next `atomic code index` / `sync`.

## Non-goals


- Negation patterns (`!pattern`).
- Per-user (uncommitted) ignore overlay.
- CLI flags for ad-hoc ignore globs.
- Ignore support for `atomic signals scan` / wiki scan.
- Serve UI toggle for ignored files.
- Windows path semantics.

## Success criteria


- [x] `.claude/atomic.toml` containing `[code]` with `ignore = ["atomic/internal/serve/assets/vendor/**"]` causes `atomic code index` to produce zero nodes from files under that directory.
- [x] A file present in an existing index that matches a newly added ignore pattern is removed (nodes, edges, unresolved refs, file row) by the next `Sync` or `IndexAll` — no new prune mechanism; the existing `pruneDeleted` handles it because the discovery list no longer contains the file.
- [x] Patterns without `/` match basenames at any depth (`*.min.js` excludes `a/b/lib.min.js`); patterns with `/` are doublestar full-path matches against repo-relative slash paths; leading `./` is stripped; a trailing-slash-only pattern (`vendor/`) matches nothing — directories are excluded with `dir/**` (matcher table test covers all four).
- [x] `IndexPaths` (explicit subset) skips ignored paths.
- [x] `ScanFiles` (exported; used by framework extraction) returns the filtered list.
- [x] Missing or empty config file → discovery output identical to today (verified by test).
- [x] Malformed TOML or an invalid glob pattern → indexing proceeds unfiltered and the CLI prints one warning line to stderr; unknown keys in the file → warning only.
- [x] `atomic code status` reports the count of active ignore patterns and the config path when patterns are loaded.
- [x] Doctor validates `.claude/atomic.toml` when present in the current repo — as its own repo-scoped check category (following the code-index check precedent, not folded into the user-config category): parse errors, unknown keys, and invalid glob patterns each produce a WARN with detail; absent file → no finding.
- [x] Discoverability wiring: `templates/shared/agent-code-intel.md` names the config so agents can self-configure ("hide vendor from my code graph"); `templates/commands/atomic-help.md` mentions it in the code/cli topic; `CLAUDE.md` code-intel section gains a one-line clause; `docs/reference/code-intel.md` documents the file format and semantics. `make render` and `make -C atomic bundle` outputs committed with zero drift; the `/atomic-help` MISSING-scan passes.
- [x] Dogfood: this repo commits `.claude/atomic.toml` ignoring `atomic/internal/serve/assets/vendor/**` (with a `.gitignore` negation pair if `.claude/*` rules would swallow it); after a fresh worktree re-index, `atomic code files` lists no path under `atomic/internal/serve/assets/vendor/`.

## Approach


Repo-scoped `[code] ignore` globs in `.claude/atomic.toml`, filtered at `scanFiles` so `pruneDeleted` removes newly-ignored files for free; matching via `github.com/bmatcuk/doublestar/v4` — see `docs/design/graphignore.md`.

## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Repo config loader + ignore matcher: a loader for `<projectRoot>/.claude/atomic.toml` mirroring the existing lenient `config.Load` pattern (missing file → empty, unknown keys → warn, malformed → error value the caller degrades on), plus a matcher honoring the semantics above; add doublestar dep | `atomic/internal/config/` (new `repo.go` + test), `atomic/go.mod` | atomic-implementer (mode: feature) | ~4 | `go test ./internal/config/`: parse, missing-file, unknown-key warn, malformed error, matcher table test (`**`, basename, `./` strip, trailing-slash no-match, invalid pattern) |
| 2 | Indexer wiring: filter `scanFiles` output and `IndexPaths` input through the matcher; engine loads repo config once per indexer boot; CLI warning on degraded (malformed) config; `code status` ignore-pattern line | `atomic/internal/codeintel/indexer/orchestrator.go`, `engine/engine.go`, `cli/code.go` + tests | atomic-implementer (mode: feature) | ~6 | `go test ./internal/codeintel/...`: ignored file skipped on IndexAll; previously indexed file pruned on Sync after pattern added; IndexPaths skips ignored; absent config → unchanged output |
| 3 | Doctor validation of `.claude/atomic.toml` as a new repo-scoped check category (mirrors the code-index check's shape in `checks_code_index.go`) | `atomic/internal/doctor/` (new check + test) | atomic-implementer (mode: surgical) | 2 | `go test ./internal/doctor/`: malformed → WARN, invalid glob → WARN, unknown key → WARN, absent → OK |
| 4 | Discoverability + dogfood: agent-code-intel partial line, atomic-help mention, CLAUDE.md clause, `docs/reference/code-intel.md` section, this repo's `.claude/atomic.toml` (+ gitignore negation if needed), render + bundle | `templates/shared/agent-code-intel.md`, `templates/commands/atomic-help.md`, `CLAUDE.md`, `docs/reference/code-intel.md`, `.claude/atomic.toml`, `.gitignore`, rendered `agents/`+`commands/`, `atomic/internal/embedded/` | atomic-implementer (mode: feature) | ~12 | `make render` + `make -C atomic bundle` then `git diff --exit-code` clean; atomic-help MISSING-scan zero; `git check-ignore .claude/atomic.toml` exits non-zero |

## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| doublestar semantics diverge from gitignore expectations (no negation, no dir-slash) | med | Document exact semantics in `docs/reference/code-intel.md`; keep v1 exclude-only |
| User repos gitignore `.claude/*`, silently swallowing the committed config | med | Reference docs call out the negation pair; dogfood commit proves the pattern in this repo |
| Filtering `ScanFiles` changes framework extraction output unexpectedly | low | Intended — consistency criterion; covered by absent-config no-change test |
| New dep (doublestar) | low | v4.10.0, MIT, zero transitive deps; verified on module proxy |
| Realm mode: one member's config bleeding into another's index | low | Engines root per member (`NewWithDBPath(projectRoot, …)`); the loader takes the engine root, so isolation holds by construction — covered by the loader's projectRoot-scoped tests |

## Change log


### 2026-07-08 — implementation log

**What changed:** All four checkpoints shipped on branch `graphignore`: CP1 `18650bf` (config loader + `IgnoreMatcher`, doublestar v4.10.0), CP2 `4e0482b` (discovery filtering in IndexAll/Sync/IndexPaths, `ScanFiles` free-fn → method, engine per-boot config load, CLI warn + `code status` line), CP3 `9ee51f5` (doctor category 13 `repo-config`), CP4 `9c54b1c` (agent-code-intel partial, atomic-help clause + count-free doctor phrasing, CLAUDE.md clause, `docs/reference/code-intel.md` section, dogfood `.claude/atomic.toml`), signals `69d2662`. Every success criterion verified — dogfood confirmed end-to-end in a live worktree index (vendor files indexed: 4 → 0 after `atomic code sync`; `atomic doctor --only 13` PASS; `atomic code status` reports 1 pattern). Follow-up filed for a pre-existing gap discovered en route: `doctor-spec-missing-migrate-row`.

**Why:** De-noise the code graph and index (~3.8k vendor symbols in this repo) with a committed, repo-scoped exclusion the whole team inherits.

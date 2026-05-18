# Project follow-ups


Non-blocking findings and deferred decisions, promoted from per-task scratchpad `FOLLOWUPS.md` ledgers. Each entry carries an `Origin:` line so future sessions know where the item came from. Closed entries are deleted — `git log` is the audit trail.


Auto-loaded into every session via `@-ref` in `claude.local.md` (or `claude.md` for repos without a local file).


---


## 🟡 risks



### atomic-doctor-F-1 — `bundlemirror.Run` double-reads files via path reconstruction


`atomic/internal/bundlemirror/mirror.go:196-216`


After the CP-2 refactor, `Run` calls `enumerate` to get `[]embedded.Artifact`, then reconstructs paths via `filepath.Join(repoRoot, filepath.FromSlash(ea.Target))` inside `mirrorFile`, re-reading each file. Harmless today (all targets are repoRoot-relative by construction) but creates a hidden contract between `enumerate` output and `Run` consumption. If any future walker rule produces a target that doesn't map cleanly back under `repoRoot`, `Run` silently breaks. Document the contract, or have `enumerate` return both the artifact + the source path so `Run` doesn't reconstruct.


Origin: docs/spec/atomic-doctor.md, iter 3 reviewer (CP-2). Deferred to project followups at Phase 3 finalize 2026-05-17.


### atomic-doctor-F-2 — `gitToplevel` called 3× per doctor run


`atomic/internal/doctor/checks_manifest.go:38`, `checks_refs.go`, plus `repodev.go` `IsRepoDev`


Three call sites each spawn `git rev-parse --show-toplevel` per doctor run. Latency nit (~20-30ms total wasted); minor correctness surface if cwd-relative symlinks change between calls. Thread the resolved toplevel through Run, or hoist to a Run-level cache passed via `Opts`.


Origin: docs/spec/atomic-doctor.md, iter 5 + iter 6 reviewers (CP-3 + CP-4). Deferred to project followups at Phase 3 finalize 2026-05-17.


### atomic-doctor-F-3 — Repair seam globals are exported `Set*` mutators


`atomic/internal/doctor/fix.go:41-98`


`installRepairFn`, `hooksRepairFn`, `manifestRepairFn`, `isRepoDevFn`, `repoRootFn` are package-level globals with exported `SetXxxFn` mutators for tests. Works today because tests don't `t.Parallel()`; would race if they did. Consider repackaging as a `Repairer` struct with injected fields, or move the `Set*` helpers to an unexported test-only file.


Origin: docs/spec/atomic-doctor.md, iter 9 reviewer (CP-7). Deferred to project followups at Phase 3 finalize 2026-05-17.


### atomic-doctor-F-4 — `defaultManifestRepair` does not stream `make` output


`atomic/internal/doctor/fix_impls.go:54`


Uses `CombinedOutput()` then discards on success — user sees `$ make -C atomic bundle` with no confirmation of what regenerated. Forward to the repair's `io.Writer` for transparency. Also: `cmd.Stdout = nil` + `cmd.Stderr = nil` before `CombinedOutput` are redundant.


Origin: docs/spec/atomic-doctor.md, iter 9 reviewer (CP-7). Deferred to project followups at Phase 3 finalize 2026-05-17.

### atomic-validate-F-5 — `MatchesSkillDir` predicate lacks file-vs-dir contract documentation


`atomic/internal/bundlespec/bundlespec.go` (predicate) / `bundlespec_test.go` (tests)


Predicate matches name strings with `atomic-` prefix regardless of file vs directory — the caller in `bundlemirror/mirror.go` gates on `IsDir()`. A future consumer reading the predicate alone cannot tell that file-named strings would also match. Add a doc comment to the predicate stating the caller-must-gate-on-IsDir contract, and add a test case demonstrating the loose match (e.g. `MatchesSkillDir("atomic-foo.md") == true`) to encode the design intent.


Origin: `docs/spec/atomic-validate.md`, iteration 2 reviewer.


### atomic-validate-F-7 — `manifestcheck` symlink-loop test placed outside walked source dirs (vacuous)


`atomic/internal/manifestcheck/manifestcheck_test.go:213`


`TestCheck_SymlinkLoop` puts the loop at `root/loopdir/self -> root/loopdir`, which the walker never descends into (not under `agents/`/`commands/`/`skills/`/`output-styles/`/`rules/`). The loop-detection code paths at lines 173-179 and 231-237 are not exercised. Move the symlink fixture inside e.g. `rules/` to actually exercise dedup.


Origin: `docs/spec/atomic-validate.md`, iteration 3 reviewer.


### atomic-validate-F-8 — Bundle integration tests accept both exit 0 and 1 (no contract verification)


`atomic/internal/validate/validate_test.go:137-143, 193-197`


`TestDispatch_BundleCleanTree` and `TestDispatch_BundleTamperedTree` assert `code != 0 && code != 1` — accept either. Cannot detect contract regressions. Reason: the embedded manifest baked into the test binary mismatches the synthetic tempdir tree, so both exit codes are "valid" from the test's perspective. Fix: inject a manifest into `RunBundleCheckAt` (cleaner — testability hook), or delete these tests and rely on `CheckFromEntries` unit tests + the manual `./bin/atomic validate bundle` smoke.


Origin: `docs/spec/atomic-validate.md`, iteration 3 reviewer.


### atomic-validate-F-12 — `mdparse.FindTableByHeader` line-number relies on `TableCell.Lines()` non-empty


`atomic/internal/mdparse/mdparse.go:253-255`


`lineNumber` comes from `hdr.FirstChild().Lines().At(0).Start`. If a goldmark version returns empty `TableCell.Lines()`, line stays 0 despite `found = true`. Add a fallback via `tbl.Lines()` or `hdr.Lines()`, or document the version assumption.


Origin: `docs/spec/atomic-validate.md`, iteration 4 reviewer.


### atomic-validate-F-13 — Empty ATX heading falls back to `startLine = 1`


`atomic/internal/mdparse/mdparse.go:119-126`


`## ` (heading marker with no content) → `h.Lines().Len() == 0` → `startLine = 1`. Silent wrong line for non-first empties. Not a blocker for prose spec files but is valid CommonMark.


Origin: `docs/spec/atomic-validate.md`, iteration 4 reviewer.


### atomic-validate-F-14 — Indented-code Setext test passes vacuously


`atomic/internal/mdparse/mdparse_test.go:217` (`TestIsATXOnly_SetextInsideIndentedCodeBlockReturnsTrue`)


Test passes regardless of the fence-tracking fix because `    ---` starts with a space and `isSetextUnderline` returns false at `trimmed[0] == ' '` before any fence logic runs. Doesn't exercise the new code path. Strengthen by also asserting against a less-trivially-non-matching indented form, OR drop the test as it tests pre-existing behavior.


Origin: `docs/spec/atomic-validate.md`, iteration 5 round 2 reviewer.


### atomic-validate-F-16 — `session-report.md` Checkpoints section placement loose


`docs/spec/session-report.md`


Cleanup pass placed the new `## Checkpoints` section at the end (just before `## Change log`) rather than immediately after `## Goal`. Functionally fine — validator passes — but reads oddly when the spec is read top-to-bottom.


Origin: `docs/spec/atomic-validate.md`, iteration 5.5 round 1 reviewer.



## 🔵 nits


### atomic-validate-F-10 — Bundle test repo-seeding loop duplicated across packages


`atomic/internal/validate/validate_test.go:94-143`


`TestDispatch_BundleCleanTree` duplicates the synthetic-repo seeding loop from `manifestcheck_test.go`. The two are in different packages so sharing is non-trivial, but at minimum a comment pointing at the canonical fixture would help. If `atomic-validate-F-8` resolves by deleting these tests, this closes automatically.


Origin: `docs/spec/atomic-validate.md`, iteration 3 reviewer.


### F-1 — Encode skill trigger boundary in atomic-tdd and atomic-debug descriptions


No file:line — descriptions live in `skills/atomic-tdd/SKILL.md` and `skills/atomic-debug/SKILL.md`.


The two skills can both auto-fire on phrases like "let's fix the broken X" — `atomic-debug` matches "broken", `atomic-tdd` matches "fix". A word-order precedence rule would be brittle. Proposed approach: encode the boundary in each skill's description itself, so the model routes correctly without an explicit rule. `atomic-tdd` description should say "NEW behavior only; for existing-broken-thing fixes, atomic-debug owns that." `atomic-debug` should say the reciprocal. The model reads both descriptions when picking, so sharp boundaries beat ordering.


Open question: does this actually work in practice, or do we see misroutes? Decision deferred pending real-world routing observations.


Origin: chat session 2026-05-17 audit review, deferred at user's request pending evidence of misrouting.


### F-2 — Design and decide on `atomic doctor` CLI subcommand *(closed 2026-05-17 — dbe2a53)*


Design exists at `docs/design/atomic-doctor.md`. Open questions in the design doc:


- Should it run automatically on `atomic update` post-install?
- Severity threshold for stale signals — file-defined or memory-configured?
- How to detect "repo-dev only" cleanly for the bundle manifest check?
- Failure mode when `~/.claude/` doesn't exist at all?


Next step when revisiting: promote design → `docs/spec/atomic-doctor.md` and feed into `/subagent-implementation`. Cohesion bundle: implementation lives in new package `atomic/internal/doctor/`, shares manifest parity check with `atomic-validate`.


Origin: chat session 2026-05-17 system improvement discussion, deferred to explore later. Closed by atomic-doctor branch (commits `ba5992f`..`dbe2a53`): design promoted to spec, all 4 open questions resolved (opt-in only, `--stale-days` flag, bundlemirror dir heuristic, exit-0 short-circuit), 8 checks + repair mode shipped.


### F-3 — Design and decide on `atomic validate` CLI subcommand


Design exists at `docs/design/atomic-validate.md`. Open questions in the design doc:


- Share code with `atomic doctor` for the bundle-parity check? (Yes — extract to `atomic/internal/manifestcheck/`.)
- Resolve third-party skill names installed in `~/.claude/skills/` but not bundled? (Probably no — focus on project's own artifacts.)
- Handle in-flight skills referenced by commands in the same PR? (Resolve against working tree, not `~/.claude/`.)
- `--suggest` flag that prints templates without editing files?
- Pre-commit hook integration via `atomic hooks install --pre-commit`?


Next step when revisiting: promote design → `docs/spec/atomic-validate.md`. Closely coupled with F-2 — both share `manifestcheck` substrate.


Origin: chat session 2026-05-17 system improvement discussion, deferred to explore later.


### F-4 — Spec the post-merge / post-squash signals-refresh integration


`docs/spec/signals-workflow.md` contracts the `/commit-only` integration (skill auto-fires pre-commit when source-tree changes). It does NOT cover the post-op defense-in-depth refresh now implemented in `/merge-to-main`, `/squash-and-merge`, and (as of this session) `/squash-only`. The pattern is in code but unspecced — future contributors editing those verbs have no canonical contract to follow.


Scope when revisiting:


- Add a "Post-merge / post-squash integration" section to `docs/spec/signals-workflow.md` describing the three-step probe (`command -v atomic` → `atomic signals stale` → invoke skill non-interactively) and the follow-up-commit convention (`chore(signals): refresh after <op> of <feature>`).
- Document the rationale: branch commits may have skipped `/commit-only` (manual `git commit`, rebased history, external PR squash) so the merge/squash target is the last guaranteed gate.
- Decide whether `/pr-only` should join the family (currently exempt — working tree must be clean, so by construction nothing to refresh). Probably leave exempt; document the reasoning.
- Append a `## Change log` entry per the spec amendment rule when landing.


Origin: chat session 2026-05-17 follow-on to fixing `/squash-only` signals-refresh gap. Pre-existing spec gap, not introduced by the fix.


## ❓ questions


(none)

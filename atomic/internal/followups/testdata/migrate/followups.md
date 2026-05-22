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


### install-output-style-F-1 — `prompt.Confirm` default-value plumbing is untested


`atomic/internal/prompt/prompt_test.go:28-50` + `prompt.go:54`


Both Confirm tests stub the runner entirely; neither exercises the path where `def` reaches `defaultRunConfirm` and survives huh's form init. The `result = def` line could be deleted with no test failure. Hard to test cleanly without a real TTY. Revisit when adding any integration harness for huh, or when huh ships a `.Default(bool)` builder we can rely on directly.


Origin: install-output-style branch (abandoned 2026-05-21), iter 1 reviewer. Logged here because the prompt package was salvaged forward.


### install-output-style-F-2 — huh PointerAccessor read-timing risk on huh upgrade


`atomic/internal/prompt/prompt.go:44-58`


`huh.NewConfirm()` has no explicit `.Default(bool)` builder. Current code sets `result = def` after `Value(&result)` and before `form.Run()`, relying on huh's `PointerAccessor` reading through the pointer at render time. If a future huh version resets bound values during form init, every confirm silently becomes `false`. huh's minor version is pinned in `go.mod`; revisit on every huh upgrade.


Origin: install-output-style branch (abandoned 2026-05-21), iter 1 reviewer. Logged here because the prompt package was salvaged forward.


### install-output-style-F-5 — `defaultRunConfirm` huh-abort mapping is test-dead


`atomic/internal/prompt/prompt.go:61-63` + `atomic/internal/doctor/stdin_prompter.go:42-44`


The `if errors.Is(err, huh.ErrUserAborted) { return false, ErrAborted }` line and the doctor-side `prompt.ErrAborted → DecisionAbort` translation cannot be reached without a real TTY. Tests cover the layers above (stubbed `runConfirm`, `Repair` loop respecting `DecisionAbort`) but not the actual mapping. Deleting either line would compile and pass `go test`. Structural gap — add an integration harness or a `huh`-side stub once the testability story for `huh` improves.


Origin: install-output-style branch (abandoned 2026-05-21), polish-pass reviewer. Logged here because the abort-handling code was salvaged forward.




## 🔵 nits


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

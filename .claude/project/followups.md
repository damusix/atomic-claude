# Project follow-ups


Non-blocking findings and deferred decisions, promoted from per-task scratchpad `FOLLOWUPS.md` ledgers. Each entry carries an `Origin:` line so future sessions know where the item came from. Closed entries stay (marked `*(closed <date> — <sha>)*`) — the audit trail is the point.


Auto-loaded into every session via `@-ref` in `claude.local.md` (or `claude.md` for repos without a local file).


---


## 🟡 risks


### cron-workflow-v2-F-1 — `SetDue` double-reads + double-parses the reminder file


`atomic/internal/reminder/reminder.go:153–165`


`SetDue` calls `findByID` (which reads + parses the file to return body), then immediately calls `os.ReadFile` + `frontmatter.Parse` on the same path again to get `meta`. Two reads, two parses per `set-due` call. No correctness issue (no concurrent writer), but unnecessary I/O. Fix: extend `findByID` to return meta alongside body, or restructure so the parse from the lookup is reused.


Origin: `docs/spec/cron-workflow.md`, cron-workflow-v2 iter 1 reviewer (CP-1). Deferred at finalize 2026-05-17.


## 🔵 nits


### F-1 — Encode skill trigger boundary in atomic-tdd and atomic-debug descriptions


No file:line — descriptions live in `skills/atomic-tdd/SKILL.md` and `skills/atomic-debug/SKILL.md`.


The two skills can both auto-fire on phrases like "let's fix the broken X" — `atomic-debug` matches "broken", `atomic-tdd` matches "fix". A word-order precedence rule would be brittle. Proposed approach: encode the boundary in each skill's description itself, so the model routes correctly without an explicit rule. `atomic-tdd` description should say "NEW behavior only; for existing-broken-thing fixes, atomic-debug owns that." `atomic-debug` should say the reciprocal. The model reads both descriptions when picking, so sharp boundaries beat ordering.


Open question: does this actually work in practice, or do we see misroutes? Decision deferred pending real-world routing observations.


Origin: chat session 2026-05-17 audit review, deferred at user's request pending evidence of misrouting.


### F-2 — Design and decide on `atomic doctor` CLI subcommand


Design exists at `docs/design/atomic-doctor.md`. Open questions in the design doc:


- Should it run automatically on `atomic update` post-install?
- Severity threshold for stale signals — file-defined or memory-configured?
- How to detect "repo-dev only" cleanly for the bundle manifest check?
- Failure mode when `~/.claude/` doesn't exist at all?


Next step when revisiting: promote design → `docs/spec/atomic-doctor.md` and feed into `/subagent-implementation`. Cohesion bundle: implementation lives in new package `atomic/internal/doctor/`, shares manifest parity check with `atomic-validate`.


Origin: chat session 2026-05-17 system improvement discussion, deferred to explore later.


### F-3 — Design and decide on `atomic validate` CLI subcommand


Design exists at `docs/design/atomic-validate.md`. Open questions in the design doc:


- Share code with `atomic doctor` for the bundle-parity check? (Yes — extract to `atomic/internal/manifestcheck/`.)
- Resolve third-party skill names installed in `~/.claude/skills/` but not bundled? (Probably no — focus on project's own artifacts.)
- Handle in-flight skills referenced by commands in the same PR? (Resolve against working tree, not `~/.claude/`.)
- `--suggest` flag that prints templates without editing files?
- Pre-commit hook integration via `atomic hooks install --pre-commit`?


Next step when revisiting: promote design → `docs/spec/atomic-validate.md`. Closely coupled with F-2 — both share `manifestcheck` substrate.


Origin: chat session 2026-05-17 system improvement discussion, deferred to explore later.


### F-4 — Design `/diagnose-ci` orchestrator command


Parallel to `/subagent-implementation` but dedicated to CI failure remediation. Three-phase loop:


1. **Foreground orchestrator** captures context (branch, failed run ID, head SHA) and writes a verbose `$SCRATCH/CI-BRIEF.md` covering the failure: which workflow, which step, error excerpt, suspected files from the log, base SHA. Verbosity is a hard rule — the parent transfers everything so the next agent does no re-discovery.
2. **`atomic-haiku` dispatch** pulls full logs into `$SCRATCH/CI-LOGS.md`.
3. **`atomic-builder` (or `atomic-surgeon`) dispatch** reads brief + logs + spec gate, proposes fix, writes test, commits. Same review loop as `/subagent-implementation`. Same `FOLLOWUPS.md` ledger.
4. **Re-watch** post-commit: dispatch `atomic-haiku` to watch the next CI run. Loop until green or hard stop.


Spec-worthy because the brief-verbosity discipline + multi-agent handoff needs codifying. Open questions:


- Scratchpad layout: same `<YYYY-MM-DD>-<topic>/` as `/subagent-implementation`, or separate `<YYYY-MM-DD>-ci-<run-id>/`?
- How to detect "same failure repeats" vs "new failure" across iterations? Compare top-level error string?
- Hard stop after N iterations (3? 5?) before bailing to the user?
- Should the orchestrator open a PR comment summarizing what was tried if it bails?


Origin: chat session 2026-05-17 system improvement discussion, deferred to explore later.


### F-5 — Design `/diagnose-bug` orchestrator command


Heavy-debug counterpart to the `atomic-debug` skill. Skill stays for fast in-context hypothesis loops; `/diagnose-bug` is the orchestrated command for bugs that span sessions or need investigator + builder + reviewer with persistent scratchpad context. Naming: `/diagnose-bug` (not `/diagnose`) so it's explicitly distinct from `/diagnose-ci`.


Same scratchpad-backed pattern as `/diagnose-ci` and `/subagent-implementation`:


1. Foreground orchestrator writes `$SCRATCH/BUG-BRIEF.md` — symptom, reproduction steps, suspected surface, environment, recent commits, what's already been tried.
2. Dispatch `atomic-investigator` to map the suspect surface.
3. Dispatch `atomic-builder`/`atomic-surgeon` with the brief + investigator output to form hypothesis, write test that captures the bug, fix, commit.
4. Loop on `CHANGES_REQUESTED`.


Wait on `/diagnose-ci` to land first so the orchestrator pattern is proven before generalizing. Likely shares >80% of the scratchpad and review-loop logic.


Open questions:


- Does this collapse into `/subagent-implementation` with a "diagnose mode" flag, or stay separate? Separate keeps the contract sharp; flag avoids duplication. Lean separate.
- Required spec at `docs/spec/<topic>.md`, or freeform bug brief? Lean freeform — bug reports rarely have specs.
- Same FOLLOWUPS.md ledger and Phase 3 disposition flow as `/subagent-implementation`? Yes — consistent surface.


Origin: chat session 2026-05-17 system improvement discussion, deferred to explore later. Naming clarified ("diagnose-bug" not "diagnose") to differentiate from `/diagnose-ci`.


### F-6 — Spec the post-merge / post-squash signals-refresh integration


`docs/spec/signals-workflow.md` contracts the `/commit-only` integration (skill auto-fires pre-commit when source-tree changes). It does NOT cover the post-op defense-in-depth refresh now implemented in `/merge-to-main`, `/squash-and-merge`, and (as of this session) `/squash-only`. The pattern is in code but unspecced — future contributors editing those verbs have no canonical contract to follow.


Scope when revisiting:


- Add a "Post-merge / post-squash integration" section to `docs/spec/signals-workflow.md` describing the three-step probe (`command -v atomic` → `atomic signals stale` → invoke skill non-interactively) and the follow-up-commit convention (`chore(signals): refresh after <op> of <feature>`).
- Document the rationale: branch commits may have skipped `/commit-only` (manual `git commit`, rebased history, external PR squash) so the merge/squash target is the last guaranteed gate.
- Decide whether `/pr-only` should join the family (currently exempt — working tree must be clean, so by construction nothing to refresh). Probably leave exempt; document the reasoning.
- Append a `## Change log` entry per the spec amendment rule when landing.


Origin: chat session 2026-05-17 follow-on to fixing `/squash-only` signals-refresh gap. Pre-existing spec gap, not introduced by the fix.


### cron-workflow-v2-F-2 — `orderedKVs` helper used only by `SetDue`; `Add` builds key list inline


`atomic/internal/reminder/reminder.go:187–209`


`Add` and `SetDue` both need to write the same canonical frontmatter key order (`id`, `created`, `due`, `transport`). `SetDue` uses the `orderedKVs` helper; `Add` inlines its own key construction. If a future field is added to the spec, the two paths can diverge silently. Fix: consolidate to one canonical ordering function used by both paths.


Origin: `docs/spec/cron-workflow.md`, cron-workflow-v2 iter 1 reviewer (CP-1). Deferred at finalize 2026-05-17.


### cron-workflow-v2-F-4 — `addReminderWithDue` test helper conflates two concerns


`atomic/internal/hooks/hooks_test.go:289–320`


`addReminderWithDue(t, root, body, dueDaysOffset, createdDaysAgo)` sets the `due:` field and optionally backdates `created:`. The two parameters interact in untested ways (no test calls both non-zero simultaneously) and the helper's purpose is opaque at the call site. Fix: split into `addReminderWithDue(due)` + `backdateCreated(id)` for clarity.


Origin: `docs/spec/cron-workflow.md`, cron-workflow-v2 iter 3 reviewer (CP-2). Deferred at finalize 2026-05-17.


### cron-workflow-v2-F-6 — `buildAdditionalContextFromRows` and `buildBodyFromPastDue` are near-duplicates


`atomic/internal/hooks/hooks.go:151–182`


After CP-2 iter 4 refactor, `buildAdditionalContextFromRows` (used by `SessionStartText`) and `buildBodyFromPastDue` (used by `SessionStart`) are structurally identical except the former runs `filterPastDue` on its input first. Trivial collapse: `buildAdditionalContextFromRows` should call `filterPastDue` then delegate to `buildBodyFromPastDue`. Zero behavior change; just removes duplication.


Origin: `docs/spec/cron-workflow.md`, cron-workflow-v2 iter 4 reviewer (CP-2). Deferred at finalize 2026-05-17.


### cron-workflow-v2-F-7 — `systemMessage` count has grammar bug ("1 reminders pending")


`atomic/internal/hooks/hooks.go:71`


The format string `"%d reminders pending"` always pluralizes "reminders" — produces "1 reminders pending" when the count is 1. Cosmetic; the prior code had the same bug since `len(rows)` rarely hit 1, but now `len(pastDue) == 1` is a common case. Fix: pluralize correctly (`fmt.Sprintf` with conditional, or a tiny `pluralize` helper).


Origin: `docs/spec/cron-workflow.md`, cron-workflow-v2 iter 4 reviewer (CP-2). Deferred at finalize 2026-05-17.


## ❓ questions


(none)


---


## Closed


(none — first entries)

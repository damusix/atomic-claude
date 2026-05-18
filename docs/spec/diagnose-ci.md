# /diagnose-ci


## Goal


CI failure remediation orchestrator. User invokes with a failed CI run reference; orchestrator pulls logs, scopes the suspect surface, drives an implement-review loop that produces a fix + regression test, commits, then re-watches CI until terminal.


## Non-goals


- Auto-firing on CI failure. User-invoked slash command only (axiom 5).
- Replacing `/watch-ci`. The Phase 0 log pull and Phase 4 re-watch reuse `atomic-haiku` directly; this command does not delegate to `/watch-ci`.
- Multi-provider abstraction beyond what `atomic-haiku` already auto-detects from project signals (GitHub Actions, GitLab CI, CircleCI, Jenkins, Buildkite, Bitbucket, Azure).
- A `--resume` flag for interrupted runs (YAGNI until a real second-hit forces the case).


## Success criteria


- [ ] Given a failed CI run, command writes `.claude/.scratchpad/<YYYY-MM-DD>-ci-<run-id>/CONTEXT.md` containing the failing step's log output, truncated at 64KB with `[truncated, full log at <provider-url>]` footer if exceeded.
- [ ] Orchestrator classifies cohesion as `tight` or `loose` after reading the investigator's surface map, then dispatches `atomic-surgeon` (tight) or `atomic-builder` (loose). On surgeon-refusal (>2 files), falls back to builder.
- [ ] Loop bails at min(memory-override-cap, 5) iterations or on three consecutive normalized-same failures, whichever first. `STATE.md` records the bail reason.
- [ ] On reviewer PASS, orchestrator commits fix + test, then `mv`s scratchpad into `.claude/.scratchpad/.archive/<topic>/`. Archive dir exists; original dir does not.
- [ ] Phase 4 re-watch dispatches `atomic-haiku` with `run_in_background: true`. Command returns before CI terminal state; watcher reports back independently.
- [ ] On bail, scratchpad retained in place; user gets iteration summary + final reviewer verdict.
- [ ] Phase 4 does **not** auto-relaunch `/diagnose-ci` on watcher-reported failure. User must re-invoke.


## Phase 0 — context capture


Invocation: `/diagnose-ci [<branch>|<pr#>|<run-id>|<workflow.yml>]`. Same arg shapes as `/watch-ci`; orchestrator resolves to a single failed run ID.


| Step | Action |
|------|--------|
| 0.1 | Resolve argument to a failed run ID via provider CLI (e.g. `gh run list --status failure --limit 1` if no arg). Refuse if no failed run found. |
| 0.2 | Capture branch, head SHA, base SHA, workflow name, failed step name, failure timestamp into `BRIEF.md` source-pointer section. |
| 0.3 | Topic suffix: `ci-<run-id>` (e.g. `2026-05-17-ci-9821334512`). Per shared-substrate "Concurrent runs", refuse if dir exists. |
| 0.4 | Dispatch `atomic-haiku` (read-only) with brief: "fetch full logs for run `<id>`, step `<name>`. Write to `CONTEXT.md`. Extract failing assertion / panic / error line as `top_level_error:` trailing key." |
| 0.5 | Orchestrator reads `CONTEXT.md`, copies `top_level_error` into `STATE.md` as iteration-0 baseline. |


## Shared loop (Phases 1-4)


> **Engine:** `docs/spec/_engine/diagnose-loop.md` defines the scratchpad layout, the Phase 1–3 agent sequence (investigator → builder/surgeon → reviewer), brief-verbosity discipline, iteration cap (with memory override), same-failure normalization, FOLLOWUPS disposition, archive-on-success teardown, and the concurrent-runs refusal. Read it once; every concrete behavior referenced below comes from there.



## Phase 4 verification (CI-specific)


After scratchpad teardown and FOLLOWUPS disposition, **before** archiving:


| Step | Action |
|------|--------|
| 4.1 | Push the fix commit if not yet pushed (orchestrator confirms with user per axiom 3 — push is visible to others). |
| 4.2 | Dispatch `atomic-haiku` in background (`run_in_background: true`) with brief: "watch CI for branch `<branch>` until terminal. Report run ID + conclusion when done." |
| 4.3 | Orchestrator returns control to user with: scratchpad archive path, fix commit SHA, background watcher ID. Notifies on watcher completion. |
| 4.4 | If watcher reports failure: do **not** auto-relaunch `/diagnose-ci`. Surface the new failure ID and let the user re-invoke. Prevents infinite loops on infrastructurally flaky tests. |


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Argument parsing + run ID resolution | `commands/diagnose-ci.md` (new) | Failed run lookup works for `gh` provider; refuses cleanly on no-failure |
| 2 | Phase 0 log capture via `atomic-haiku` dispatch | `commands/diagnose-ci.md` Phase 0 section | `CONTEXT.md` written with logs (truncated to 64KB) + `top_level_error:` trailing key |
| 3 | Phase 1–3 loop integration | `commands/diagnose-ci.md` — link to engine | Orchestrator classifies cohesion from investigator surface map; surgeon-vs-builder dispatched accordingly; surgeon-refusal falls back to builder; loop honors memory-override cap + normalized-same-failure bail |
| 4 | Phase 4 background re-watch | `commands/diagnose-ci.md` Phase 4 section | `atomic-haiku` runs `run_in_background: true`; command returns before terminal state; no auto-relaunch on failure |
| 5 | Scratchpad archive + bail-retention | engine-defined; verified by checkpoint 3 | `.claude/.scratchpad/.archive/<topic>/` exists on PASS; in-place retention on bail |
| 6 | Wiring: `CLAUDE.md`, `CLAUDE.md`, `README.md`, `commands/diagnose-ci.md` | per `claude.local.md` invisible-feature checklist | grep finds `/diagnose-ci` in all four surfaces |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Provider detection wrong → log fetch fails | medium | Reuse `/watch-ci` provider-detection logic; refuse with clear message if no provider matched in signals |
| Phase 4 watcher hangs (CI cancelled, infra down, run >10m) | medium | `atomic-haiku` has its own ~10-min cap; on cap-hit the watcher reports `terminal: unknown` and exits. Orchestrator does not block on it. |
| Run-ID collision across providers (numeric IDs reused) | low | Topic suffix derives from provider+run-id when ambiguous; defer until a real collision is hit |
| Auto-relaunch loop on flaky tests | medium | Hard rule in Phase 4.4: never auto-relaunch on watcher-reported failure |
| Engine evolves under this consumer | low | Engine file has its own `## Change log`; this spec is invalidated only on a breaking engine change, which would warrant amending this spec's body too |


## Change log


<!-- Populated on first amendment after this spec is approved. Initial draft (2026-05-17) is not an amendment. -->

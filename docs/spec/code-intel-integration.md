# Code-intel integration

## Goal

Wire the built `atomic code` engine into the atomic artifact system so disposable subagents query the symbol graph (with graceful fallback to grep) and orchestrator commands keep the index fresh — without context-precious parents querying inline. Done = the keystone agents and lifecycle commands use the index when present, every consumer degrades cleanly when it is absent, a doctor check reports index health, and the engine is discoverable through help/docs.

## Non-goals

- MCP server registration as a shipped feature. Public docs only (manual `.mcp.json` opt-in). No helper command, no auto-register.
- Auto-indexing on session start.
- A new `atomic code stale` CLI verb (doctor calls a Go helper; orchestrators use `code sync`).
- Any change to the engine's index/query internals. Pure consumption.
- Windows correctness (per repo platform policy).

## Success criteria

- [ ] A shared partial `templates/shared/agent-code-intel.md` exists, stating: index-present → use the `code search/callers/callees/impact` verbs with machine-parseable output for location/relationship questions; bounded queries (one symbol, no full-graph dump); and the degradation contract (binary absent / no DB / failed query → fall back to sg/grep).
- [ ] `agent-code-intel` is composed into `agent-implementer-workflow` (reaching builder + surgeon), `atomic-investigator`, `atomic-reviewer`, `atomic-haiku`, `atomic-wiki-inferrer` — verifiable in the rendered `agents/*.md`.
- [ ] `agent-search-tooling` carries a one-line bridge naming code-intel as the top search tier when an index is present.
- [ ] `atomic-investigator` uses `code search/callers/callees/impact` for location/relationship questions when an index exists, and falls back to sg/grep otherwise.
- [ ] `atomic-wiki-inferrer` queries real import/call edges to inform domain clustering when an index exists, degrading to filename heuristics otherwise.
- [ ] `/refresh-wiki` ensures the index is fresh before dispatching the inferrer: `code sync` when warm; auto-run `code index` when cold (no prompt — indexing is harmless and idempotent); proceed degraded on index error or binary absence.
- [ ] `/refresh-wiki` syncs/indexes each member repo best-effort before summarizing, and degrades to summary-without-graph when a repo has no index.
- [ ] `/subagent-implementation` and `/autopilot` index/sync once at task start and run `code sync` after each builder commit so the reviewer queries current working-tree state; both degrade when unavailable.
- [ ] `/atomic-plan` delegates structural exploration to the investigator (which now carries code-intel); the main agent issues no inline `code` queries.
- [ ] `/documentation` delegates the changed-symbol `code impact` sweep to a subagent (investigator/haiku); the parent issues no inline sweep.
- [ ] `/gather-evidence` adds `atomic code callers` / `atomic code impact` as an example to the Tier-1 source-quality row for codebase claims (alongside ast-grep on the actual repo).
- [ ] A new doctor check reports index health: no DB → PASS informational; DB present + stale → WARN; DB present + fresh → PASS; never FAIL. Appended to the suite (new stable index, never renumber existing). `go test ./internal/doctor/...` green. The doctor-check count string in `/atomic-help` (currently "10 integrity checks") is incremented to match.
- [ ] `/atomic-help`: doctor-check count and the code-intel surface reflect the integration (topic rows + relevant tour stage). Help-router verification command emits zero `MISSING:` lines.
- [ ] A public docs page documents manual project-scoped MCP registration (`.mcp.json` snippet for `atomic code mcp`); README/docs reference tables and `CLAUDE.md` reflect the new partial + doctor check.
- [ ] `make render` and `make -C atomic bundle` are clean (no diff) after each checkpoint; `go vet` + `gofmt` clean for Go changes.

## Approaches

| # | Approach | Sketch | Cost | Risk |
|---|----------|--------|------|------|
| A | Overload `agent-search-tooling` | add code-intel tier + graph verbs to the existing partial | low | muddies single-responsibility; reviewer/haiku/signals-inferrer don't carry it |
| B | Dedicated `agent-code-intel` partial + bridge | new partial composed where relevant; one-line bridge in search-tooling | med | one more partial in the pool |

## Recommendation

**B.** Single-responsibility per partial matches the repo's partial philosophy. Graph verbs (`callers`/`callees`/`impact`) are novel capabilities with no grep equivalent, so folding them into a search-tool-selection partial is a category mismatch. Composing the new partial only where relevant keeps reviewer/haiku/signals-inferrer from inheriting the full grep/sg paragraph they don't need. Evidence: `agent-search-tooling` is composed inside `agent-implementer-workflow` (so builder/surgeon get the bridge automatically); investigator composes it directly at line 25; reviewer/haiku/signals-inferrer compose only `agent-atomic-voice` today, so they take `agent-code-intel` as a clean new directive. Full rationale in `docs/design/code-intel-integration.md`.

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | `agent-code-intel` partial + keystone investigator + search-tooling bridge | `templates/shared/agent-code-intel.md` (new), `templates/shared/agent-search-tooling.md`, `templates/agents/atomic-investigator.md`, rendered `agents/atomic-investigator.md` | atomic-builder | ~4 | `make render` clean; rendered investigator carries code-intel guidance + degradation; bridge line present |
| 2 | Compose into implementer + reviewer + haiku | `templates/shared/agent-implementer-workflow.md`, `templates/agents/atomic-reviewer.md`, `templates/agents/atomic-haiku.md`, rendered `agents/{atomic-builder,atomic-surgeon,atomic-reviewer,atomic-haiku}.md` | atomic-builder | ~7 | rendered builder/surgeon/reviewer/haiku carry the partial; bounded-query + degradation present |
| 3 | Signals: inferrer query + `/refresh-wiki` lifecycle | `templates/agents/atomic-wiki-inferrer.md`, `templates/commands/refresh-wiki.md`, rendered outputs | atomic-builder | ~4 | inferrer uses import/call edges w/ degradation; refresh-signals syncs-if-warm / auto-indexes-if-cold (no prompt) before dispatch |
| 4 | `/refresh-wiki` cross-repo index | `templates/commands/refresh-wiki.md`, rendered output | atomic-builder | ~2 | per-repo best-effort sync/index step; degrades to summary-without-graph |
| 5 | Orchestrator lifecycle: `/subagent-implementation` + `/autopilot` | `templates/commands/subagent-implementation.md`, `templates/commands/autopilot.md`, rendered outputs | atomic-builder | ~4 | sync-if-warm at task start + sync-per-iteration (after each builder commit) steps. Cold-start is uniform: both auto-index best-effort at task start with no prompt (indexing is harmless and idempotent), degrading to grep on error or when the index/binary is unavailable. |
| 6 | Parent-delegates: `/atomic-plan` + `/documentation` + `/gather-evidence` | `templates/commands/atomic-plan.md`, `templates/commands/documentation.md`, `templates/commands/gather-evidence.md`, rendered outputs | atomic-builder | ~6 | plan/doc delegate to subagent (no inline query); gather-evidence names code-intel Tier-1 |
| 7 | Doctor index-freshness check | `atomic/internal/codeintel/...` (read-only status helper, mirroring how `checks_signals.go` calls `signals.Stale`), new doctor check file + test, doctor suite registration | atomic-builder | ~4 | `go test ./internal/doctor/...` green; PASS-absent / WARN-stale / PASS-fresh; never FAIL; doctor check appended (existing indices unchanged) |
| 8 | Discoverability + docs + MCP page | `templates/commands/atomic-help.md`, `CLAUDE.md`, `README.md`, `docs/reference/*.md`, new MCP docs page | atomic-builder | ~6 | help-router verification zero `MISSING:`; docs build; bundle parity; "10 integrity checks" string in atomic-help bumped to "11" |

**Per-checkpoint build parity (applies to every CP above).** Any checkpoint touching `templates/` re-renders and verifies `make render` clean; any touching a source artifact (`templates/` outputs under `agents/`/`commands/`, `CLAUDE.md`, `rules/`) verifies `make -C atomic bundle` clean. The Go-only CP7 verifies `go vet ./...` + `gofmt -l` clean instead. No CP is green with render or bundle drift.

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Doctor check WARNs on every repo that never opted into code-intel (noise) | med | absence = PASS informational, never WARN; only WARN when a DB exists and is stale |
| Subagent hard-depends on the index and errors when absent | med | degradation contract stated in the shared partial + verified per CP; fallback to sg/grep is the default path, query is the enhancement |
| Cold-start `code index` ambushes the user (slow) on a hot path | med | only orchestrators trigger indexing; `/refresh-wiki`, `/subagent-implementation`, and `/autopilot` all auto-index best-effort with no prompt, printing a one-line "first run may take a while" notice and degrading on error; session-start auto-index is a non-goal |
| Bundle/render drift slips into a commit | low | each CP verifies `make render` + `make -C atomic bundle` clean; pre-commit hook chains both |
| `/refresh-wiki` cross-repo indexing is slow on large realms | med | best-effort, sync-if-warm, index-only-on-opt-in, degrade to summary-without-graph |
| Parent agent still queries inline despite the principle | low | plan/documentation checkpoints assert "no inline query"; reviewer checks the rendered command text |

## Implementation log

### v1 — 2026-06-08

Built across 8 checkpoints via `/autopilot` (merge-verb `commit-only`). Commits (chronological):

- `6fee1d0` — planning docs (design + spec)
- `0a8a591` — CP1 agent-code-intel partial + investigator graph-locator + search-tooling bridge
- `1ca4687` — CP2 code-intel into builder/surgeon (via implementer-workflow) + reviewer + haiku
- `a9d39c3` — CP3 signals inferrer graph clustering + /refresh-wiki index lifecycle
- `77da2cc` — CP4 /refresh-wiki per-repo best-effort index grounding
- `06f21a7` — CP5 orchestrator index lifecycle (/subagent-implementation + /autopilot)
- `5abcbf8` — CP6 /atomic-plan + /documentation delegate; /gather-evidence Tier-1
- `809660a` — CP7 doctor code-index freshness check (category 11) + engine.IndexPath
- `fd41a27` — CP8 help-router + CLAUDE.md + docs reference + public MCP guide

**Out-of-scope work performed during this build:**
- Spec amendment mid-run: CP5 cold-start behavior split by command (interactive offers, autopilot auto-indexes no-prompt) — see change log. Forced by the discovery that autopilot cannot prompt mid-run.

**Unforeseens — surprises that emerged during implementation:**
- Builders initially moved the orchestrator's `.claude/.scratchpad/` dir into `tmp/trash/`, misreading the "discard scratch" hygiene line. Recovered the dir; hardened every later builder/surgeon brief to state the scratchpad is orchestrator memory, not scratch.
- The CP2 reviewer flagged uncommitted rendered/bundle outputs as "render drift" (false positive — that is the expected pre-commit state). Corrected the parity-check method in every later reviewer brief (parity bug only if `make render` produces NEW changes beyond the working tree).
- Orchestrator Phase-4 verification first ran in the main checkout (no codeintel on `main`) instead of the worktree; caught and re-run in the worktree's `atomic/`.

**Deferred items still open:**
- None. Autopilot addressed every reviewer finding in-iteration; the scratchpad FOLLOWUPS ledger ended empty.
- Obsolete (2026-06-24): the cold-start index *offer* was removed in favor of uniform no-prompt auto-index, so memory-of-decline no longer applies — there is no decline to remember.

## Change log

### 2026-06-24 — Cold-start indexing unified to no-prompt auto-index

Removed the cold-start index *offer*. `/refresh-wiki` and `/subagent-implementation` previously prompted via `AskUserQuestion` before building a cold index; both now run `atomic code index` directly with no prompt, matching `/autopilot`. Rationale: indexing is cheap, idempotent, and non-destructive — there is nothing to ask, and a one-line "first run may take a while" notice covers the only cost (latency). **Superseded:** the 2026-06-07 CP5 split (interactive commands offer, autopilot auto-indexes) and the original "cold-start is an explicit offer, never silent" rule — all three commands now auto-index uniformly. Body amended: the `/refresh-wiki` success criterion, the CP3 and CP5 checkpoint cells, and the cold-start-ambush risk row; the memory-of-decline deferred item is marked obsolete. Aligns with the auto-index policy in `CLAUDE.md`.

### 2026-06-07 — CP5 cold-start behavior split by command

Clarified CP5: the "cold-start is an explicit offer, never silent" rule applies to `/subagent-implementation` (interactive). `/autopilot` cannot prompt mid-run (its only human decision is the ship gate), so on a cold index it auto-indexes best-effort at task start without prompting, degrading to grep on error. Discovered during implementation — autopilot runs in a fresh worktree (always cold), so a no-prompt path is required for it to use code-intel at all; auto-indexing is non-destructive and the user already granted autonomy. **Superseded:** the prior single rule "cold-start is an explicit offer, never silent auto-index" applied uniformly to both commands.

**Squashed to 84aeb5d — 2026-06-08.** Per-iteration SHAs above are historical (unreachable from any branch).

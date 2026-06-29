---
type: Domain
description: Scan → infer → wire: project context generation pipeline; implementation phase owns refresh (C3/C4); ship verb is docs-only-skipped fallback (C2).
---

# signals

## What it does

The project signals workflow: deterministic project snapshot generation (`atomic signals scan`), LLM-driven inference (`atomic-wiki-inferrer`), router file management (`docs/wiki/index.md`), and @-ref wiring. Only `docs/wiki/index.md` is auto-loaded by Claude sessions. `docs/wiki/scan.md` is read on demand by the inferrer only.

## Artifacts

- [`agents/atomic-wiki-inferrer.md`](../../agents/atomic-wiki-inferrer.md) — full signals pipeline: runs `atomic signals scan`, reads `docs/wiki/scan.md`, infers domain structure, dispatches sub-agents per domain, runs reviewer per domain file, wires cross-domain refs, writes `docs/wiki/index.md`, wires `@-ref`. Carries `agent-code-intel` partial — queries real import/call edges when index present to inform domain clustering; degrades to filename heuristics when absent. Accepts `changed_range: <from-sha>..<to-sha>` caller arg: derives changed-paths set from `git diff --name-only <from-sha>..<to-sha>` unioned with uncommitted changes instead of the prev/current snapshot diff; whole-repo scan still runs; ignored in wiki-output and bucket-synthesis modes. Never modifies files outside [`docs/wiki/`](..). Dispatched by `/refresh-wiki` (interactive), the `/subagent-implementation` finalize step (silent, `changed_range`-scoped), `/autopilot` pre-ship step (silent, `changed_range`-scoped), and ship verbs as ad-hoc fallback (silent mode via signals-gate partial).
- [`commands/refresh-wiki.md`](../../commands/refresh-wiki.md) — `/refresh-wiki` idempotent entry point for both first-run init and subsequent refreshes. Pre-flight checks: git repo presence, [`atomic`](../../atomic) binary on PATH. **Code-intel lifecycle**: runs `atomic code sync` if index is warm; offers `atomic code index` if cold (offer fires each run until accepted — memory-of-decline is a deferred follow-up); proceeds degraded on decline or absence. Dispatches `atomic-wiki-inferrer` agent. Wires `@-ref` (only `docs/wiki/index.md`; `docs/wiki/scan.md` is NOT @-ref'd). Reports status.

## CLI code

- [`atomic/internal/signals/signals.go`](../../atomic/internal/signals/signals.go) — top-level scan orchestrator. Writes [`docs/wiki/scan.md`](scan.md). Keeps `tmp/.scan.prev.md` for diff. Entry point for `atomic signals scan`. `resolveScanOptions` returns a cloned `*Options` — the caller's value is never mutated. `assembleBody` reads each file once via the pre-populated tree entries; there is no second file-read pass.
- [`atomic/internal/signals/tree.go`](../../atomic/internal/signals/tree.go) — directory tree walker. `max_depth` default 3 (shell-settable via `output.signals.max_depth`). Beyond cutoff: folder name + child count + folder SHA, no contents.
- [`atomic/internal/signals/languages.go`](../../atomic/internal/signals/languages.go) — extension-to-language map. 51 extensions, 26 languages. Deterministic — map is the single source of truth, no content sniffing.
- [`atomic/internal/signals/manifests.go`](../../atomic/internal/signals/manifests.go) — package manifest scanner (go.mod, package.json, Cargo.toml, etc.).
- [`atomic/internal/signals/diff.go`](../../atomic/internal/signals/diff.go) — `atomic signals diff`: delegates to `git diff` in a git repo, falls back to unix `diff` against `tmp/.scan.prev.md` outside one. Enables incremental mode for the inferrer.
- [`atomic/internal/doctor/checks_signals.go`](../../atomic/internal/doctor/checks_signals.go) — doctor check index 3 (`signals`). Verifies [`docs/wiki/scan.md`](scan.md) exists and is not stale.
- [`atomic/internal/doctor/checks_refs.go`](../../atomic/internal/doctor/checks_refs.go) — doctor check index 4 (`refs`). Searches for `@docs/wiki/index.md` in candidate files in order: [`claude.local.md`](../../claude.local.md), [`CLAUDE.local.md`](../../CLAUDE.local.md), [`CLAUDE.md`](../../CLAUDE.md), [`claude.md`](../../claude.md). Only `docs/wiki/index.md` is checked — `docs/wiki/scan.md` is not an @-ref target. Severity: FAIL.

## Docs

- [`docs/spec/signals-workflow.md`](../../docs/spec/signals-workflow.md) — end-to-end lifecycle: scan → inference → @-ref wiring. Canonical for the `atomic-wiki-inferrer` agent and `/refresh-wiki` command. Covers: files produced, `atomic signals stale` gate (with docs-only guard), inferrer dispatch, @-ref wiring rules (only `signals.md`), fallback flow, ship verb integration contract.
- [`docs/spec/signals-refresh-timing.md`](../../docs/spec/signals-refresh-timing.md) — child of `signals-workflow.md`; contracts *when* the refresh fires and *how* the inferrer is scoped. C1: inferrer `changed_range` arg. C2: signals-gate docs-only guard (skips before staleness check when every staged path is documentation). C3: `/subagent-implementation` finalize refresh (once at finalize, `changed_range`-scoped, committed as `chore(signals)`). C4: `/autopilot` pre-ship refresh. C5: documentation surfaces updated.
- [`docs/spec/signals-router.md`](../../docs/spec/signals-router.md) — router file shape, domain file layout, inferrer behavior, incremental vs full mode, budget model (split trigger at ~1k lines / ~5k tokens), naming continuity rule.
- [`docs/spec/signals-project-detection.md`](../../docs/spec/signals-project-detection.md) — project detection heuristics used by the tree walker to locate the primary source root.
- [`docs/spec/code-intel-integration.md`](../../docs/spec/code-intel-integration.md) — integration spec covering `/refresh-wiki` code-intel lifecycle contract (sync-if-warm / offer-if-cold / degrade), inferrer code-intel query behavior.
- [`docs/reference/signals-workflow.md`](../../docs/reference/signals-workflow.md) — user-facing reference: when refresh fires (implementation phase owns it; ship verb is the ad-hoc fallback for docs-only-skipped commits).
- [`docs/design/signals-refresh-timing.md`](../../docs/design/signals-refresh-timing.md) — design rationale for moving the primary refresh from commit-time to implementation-phase finalize; explains why `atomic signals stale` is the coordinator (no marker file); docs-only classification; SHA-range scoping (no Go change needed).
- [`docs/design/signals-router.md`](../../docs/design/signals-router.md) — design rationale for the router shape: why flat eager-load fails at scale, domain partitioning approach, change detection via git per-path SHAs, incremental refresh design.

## Coupling

- **→ bundle**: `atomic-wiki-inferrer` agent ships in the bundle via `agents/atomic-*.md` bundlespec rule. Changes to the agent file require `make bundle`.
- **→ doctor**: `checks_refs.go` checks for `@docs/wiki/index.md`. Changes to which @-ref is required must be reflected in both `checks_refs.go` and the `signalsRef` const.
- **→ config**: `output.signals.max_depth` config key is read by [`atomic/internal/signals/tree.go`](../../atomic/internal/signals/tree.go). Config schema changes propagate to signals behavior.
- **→ workflow**: the `/subagent-implementation` finalize step and `/autopilot` pre-ship step dispatch `atomic-wiki-inferrer` in silent mode with `changed_range: <loop-base>..HEAD`, committed as `chore(signals): refresh after <topic>`. Ship verbs dispatch the inferrer as an ad-hoc fallback only when `atomic signals stale` exits 1 and the staged set is not docs-only (docs-only commits skip before the staleness check). If the agent's interface or output contract changes, the finalize templates and signals-gate partial (bundle domain) must be updated.

# signals

## What it does

The project signals workflow: deterministic project snapshot generation (`atomic signals scan`), LLM-driven inference (`atomic-signals-inferrer`), router file management (`signals.md`), and @-ref wiring. Only `signals.md` is auto-loaded by Claude sessions. `deterministic-signals.md` is read on demand by the inferrer only.

## Artifacts

- [`agents/atomic-signals-inferrer.md`](../../../agents/atomic-signals-inferrer.md) — full signals pipeline: runs `atomic signals scan`, reads `deterministic-signals.md`, infers domain structure, dispatches sub-agents per domain, runs reviewer per domain file, wires cross-domain refs, writes `signals.md`, wires `@-ref`. Now carries `agent-code-intel` partial — queries real import/call edges when index present to inform domain clustering; degrades to filename heuristics when absent. Never modifies files outside [`.claude/project/`](..). Dispatched by `/refresh-signals` (interactive) and ship verbs (silent mode via signals-gate partial).
- [`commands/refresh-signals.md`](../../../commands/refresh-signals.md) — `/refresh-signals` idempotent entry point for both first-run init and subsequent refreshes. Pre-flight checks: git repo presence, [`atomic`](../../../atomic) binary on PATH. Creates [`.claude/project/signals-steering.md`](../signals-steering.md) scaffold if missing. **Code-intel lifecycle**: runs `atomic code sync` if index is warm; offers `atomic code index` if cold (offer fires each run until accepted — memory-of-decline is a deferred follow-up); proceeds degraded on decline or absence. Dispatches `atomic-signals-inferrer` agent. Wires `@-ref` (only `signals.md`; `deterministic-signals.md` is NOT @-ref'd). Reports status.

## CLI code

- [`atomic/internal/signals/signals.go`](../../../atomic/internal/signals/signals.go) — top-level scan orchestrator. Writes [`.claude/project/deterministic-signals.md`](../deterministic-signals.md). Keeps `.deterministic-signals.prev.md` for diff. Entry point for `atomic signals scan`. `resolveScanOptions` returns a cloned `*Options` — the caller's value is never mutated. `assembleBody` reads each file once via the pre-populated tree entries; there is no second file-read pass.
- [`atomic/internal/signals/tree.go`](../../../atomic/internal/signals/tree.go) — directory tree walker. `max_depth` default 3 (shell-settable via `output.signals.max_depth`). Beyond cutoff: folder name + child count + folder SHA, no contents.
- [`atomic/internal/signals/languages.go`](../../../atomic/internal/signals/languages.go) — extension-to-language map. 51 extensions, 26 languages. Deterministic — map is the single source of truth, no content sniffing.
- [`atomic/internal/signals/manifests.go`](../../../atomic/internal/signals/manifests.go) — package manifest scanner (go.mod, package.json, Cargo.toml, etc.).
- [`atomic/internal/signals/diff.go`](../../../atomic/internal/signals/diff.go) — `atomic signals diff`: delegates to `git diff` in a git repo, falls back to unix `diff` against `.deterministic-signals.prev.md` outside one. Enables incremental mode for the inferrer.
- [`atomic/internal/doctor/checks_signals.go`](../../../atomic/internal/doctor/checks_signals.go) — doctor check index 3 (`signals`). Verifies [`.claude/project/deterministic-signals.md`](../deterministic-signals.md) exists and is not stale.
- [`atomic/internal/doctor/checks_refs.go`](../../../atomic/internal/doctor/checks_refs.go) — doctor check index 4 (`refs`). Searches for `@.claude/project/signals.md` in candidate files in order: [`claude.local.md`](../../../claude.local.md), [`CLAUDE.local.md`](../../../CLAUDE.local.md), [`CLAUDE.md`](../../../CLAUDE.md), [`claude.md`](../../../claude.md). Only `signals.md` is checked — `deterministic-signals.md` is no longer an @-ref target (fixed in hash `477404b`). Severity: FAIL.

## Docs

- [`docs/spec/signals-workflow.md`](../../../docs/spec/signals-workflow.md) — end-to-end lifecycle: scan → inference → @-ref wiring. Canonical for the `atomic-signals-inferrer` agent and `/refresh-signals` command. Covers: files produced, `atomic signals stale` gate, inferrer dispatch, @-ref wiring rules (only `signals.md`), fallback flow, ship verb integration contract.
- [`docs/spec/signals-router.md`](../../../docs/spec/signals-router.md) — router file shape, domain file layout, inferrer behavior, incremental vs full mode, budget model (split trigger at ~1k lines / ~5k tokens), naming continuity rule.
- [`docs/spec/signals-project-detection.md`](../../../docs/spec/signals-project-detection.md) — project detection heuristics used by the tree walker to locate the primary source root.
- [`docs/spec/code-intel-integration.md`](../../../docs/spec/code-intel-integration.md) — integration spec covering `/refresh-signals` code-intel lifecycle contract (sync-if-warm / offer-if-cold / degrade), inferrer code-intel query behavior.
- [`docs/reference/signals-workflow.md`](../../../docs/reference/signals-workflow.md) — thin pointer (11L) to the spec. User-facing entry point.
- [`docs/design/signals-router.md`](../../../docs/design/signals-router.md) — design rationale for the router shape: why flat eager-load fails at scale, domain partitioning approach, change detection via git per-path SHAs, incremental refresh design.

## Coupling

- **→ bundle**: `atomic-signals-inferrer` agent ships in the bundle via `agents/atomic-*.md` bundlespec rule. Changes to the agent file require `make bundle`.
- **→ doctor**: `checks_refs.go` checks for `@.claude/project/signals.md`. Changes to which @-ref is required must be reflected in both `checks_refs.go` and the `signalsRef` const.
- **→ config**: `output.signals.max_depth` config key is read by [`atomic/internal/signals/tree.go`](../../../atomic/internal/signals/tree.go). Config schema changes propagate to signals behavior.
- **→ workflow**: ship verbs dispatch `atomic-signals-inferrer` in silent mode (via signals-gate partial) after staged source changes. If the agent's interface or output contract changes, ship verb templates (bundle domain) must be updated.

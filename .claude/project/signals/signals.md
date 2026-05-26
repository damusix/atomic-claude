# signals

## What it does

The project signals workflow: deterministic project snapshot generation (`atomic signals scan`), LLM-driven inference (`atomic-signals-inferrer`), router file management (`signals.md`), and @-ref wiring. Only `signals.md` is auto-loaded by Claude sessions. `deterministic-signals.md` is read on demand by the inferrer only.

## Artifacts

- `skills/atomic-signals/SKILL.md` — auto-fires on "regenerate signals", "scan the project", "refresh project context", "what's in this repo", "rescan". Runs `atomic signals scan`, dispatches `atomic-signals-inferrer`, ensures @-refs are wired in the project CLAUDE.md.
- `agents/atomic-signals-inferrer.md` — reads `deterministic-signals.md`, writes `signals.md` (the router). On large repos, dispatches sub-agents per domain, runs reviewer per domain file, wires cross-domain refs. Never modifies files outside `.claude/project/`.
- `commands/refresh-signals.md` — `/refresh-signals` idempotent entry point for both first-run init and subsequent refreshes. Pre-flight checks: git repo presence, `atomic` binary on PATH. Creates `.claude/project/signals-steering.md` scaffold if missing. Runs `atomic signals scan`, dispatches skill, wires `@-ref` (only `signals.md`; `deterministic-signals.md` is NOT @-ref'd). Reports status.

## CLI code

- `atomic/internal/signals/signals.go` — top-level scan orchestrator. Writes `.claude/project/deterministic-signals.md`. Keeps `.deterministic-signals.prev.md` for diff. Entry point for `atomic signals scan`.
- `atomic/internal/signals/tree.go` — directory tree walker. `max_depth` default 3 (shell-settable via `output.signals.max_depth`). Beyond cutoff: folder name + child count + folder SHA, no contents.
- `atomic/internal/signals/languages.go` — extension-to-language map. 51 extensions, 26 languages. Deterministic — map is the single source of truth, no content sniffing.
- `atomic/internal/signals/manifests.go` — package manifest scanner (go.mod, package.json, Cargo.toml, etc.).
- `atomic/internal/signals/diff.go` — `atomic signals diff`: delegates to `git diff` in a git repo, falls back to unix `diff` against `.deterministic-signals.prev.md` outside one. Enables incremental mode for the inferrer.
- `atomic/internal/doctor/checks_signals.go` — doctor check index 3 (`signals`). Verifies `.claude/project/deterministic-signals.md` exists and is not stale.
- `atomic/internal/doctor/checks_refs.go` — doctor check index 4 (`refs`). Searches for `@.claude/project/signals.md` in candidate files in order: `claude.local.md`, `CLAUDE.local.md`, `CLAUDE.md`, `claude.md`. Only `signals.md` is checked — `deterministic-signals.md` is no longer an @-ref target (fixed in hash `477404b`). Severity: FAIL.

## Docs

- `docs/spec/signals-workflow.md` — end-to-end lifecycle: scan → inference → @-ref wiring. Canonical for the `atomic-signals` skill and `/refresh-signals` command. Covers: files produced, `atomic signals stale` gate, staleness check in skill, inferrer dispatch, @-ref wiring rules (only `signals.md`), fallback flow, `/commit-only` integration contract.
- `docs/spec/signals-router.md` — router file shape, domain file layout, inferrer behavior, incremental vs full mode, budget model (split trigger at ~1k lines / ~5k tokens), naming continuity rule.
- `docs/spec/signals-project-detection.md` — project detection heuristics used by the tree walker to locate the primary source root.
- `docs/reference/signals-workflow.md` — thin pointer (11L) to the spec. User-facing entry point.
- `docs/design/signals-router.md` — design rationale for the router shape: why flat eager-load fails at scale, domain partitioning approach, change detection via git per-path SHAs, incremental refresh design.

## Coupling

- **→ bundle**: `atomic-signals-inferrer` agent ships in the bundle via `agents/atomic-*.md` bundlespec rule. Changes to the agent file require `make bundle`.
- **→ bundle**: `atomic-signals` skill ships in the bundle via `skills/atomic-*/` bundlespec rule. Changes to `skills/atomic-signals/SKILL.md` require `make render` then `make bundle` (render is not needed here but bundle is).
- **→ doctor**: `checks_refs.go` checks for `@.claude/project/signals.md`. The prior bug (checking for `inferred-signals.md`) is resolved. Changes to which @-ref is required must be reflected in both `checks_refs.go` and the `signalsRef` const.
- **→ config**: `output.signals.max_depth` config key is read by `atomic/internal/signals/tree.go`. Config schema changes propagate to signals behavior.
- **→ workflow**: ship verbs invoke the `atomic-signals` skill silently after staged source changes. If the skill's silent-mode contract or output format changes, ship verb templates (bundle domain) must be updated.

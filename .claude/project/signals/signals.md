# signals

The project signals workflow: deterministic project snapshot generation, LLM-driven inference, router file management, and @-ref wiring.

## Artifacts

- `skills/atomic-signals/SKILL.md` — auto-fires on "regenerate signals", "scan the project", "refresh project context", "what's in this repo", "rescan". Runs `atomic signals scan`, dispatches `atomic-signals-inferrer`, ensures @-refs are wired in the project CLAUDE.md.
- `agents/atomic-signals-inferrer.md` — reads `deterministic-signals.md`, writes `signals.md` (the router). On large repos, dispatches sub-agents per domain, runs reviewer per domain file, wires cross-domain refs. Never modifies files outside `.claude/project/`.
- `commands/initialize-signals.md` — `/initialize-signals` one-shot bootstrap for projects that have never had signals generated. Interactive, idempotent. Stops if `atomic` binary missing.
- `commands/refresh-signals.md` — `/refresh-signals` deliberate on-demand refresh. Refuses to run if signals were never initialized. Delegates to the `atomic-signals` skill.

## CLI code

- `atomic/internal/signals/signals.go` — top-level scan orchestrator. Writes `.claude/project/deterministic-signals.md`. Keeps `.deterministic-signals.prev.md` for diff. Entry point for `atomic signals scan`.
- `atomic/internal/signals/tree.go` — directory tree walker. `max_depth` default 3 (shell-settable via `output.signals.max_depth`). Beyond cutoff: folder name + child count + folder SHA, no contents.
- `atomic/internal/signals/languages.go` — extension-to-language map. 51 extensions, 26 languages. Deterministic — map is the single source of truth, no content sniffing.
- `atomic/internal/signals/manifests.go` — package manifest scanner (go.mod, package.json, Cargo.toml, etc.).
- `atomic/internal/signals/diff.go` — git diff integration. Uses `git diff --name-only <prev>..HEAD` to compute changed paths set; falls back to mtime comparison outside git repos.
- `atomic/internal/doctor/checks_signals.go` — doctor check index 3 (`signals`). Verifies `.claude/project/deterministic-signals.md` exists and is not stale.
- `atomic/internal/doctor/checks_refs.go` — doctor check index 4 (`refs`). Reads `claude.local.md` / `CLAUDE.md` / `CLAUDE.local.md` verifying `@.claude/project/` refs are wired. **Known bug**: currently looks for `@.claude/project/inferred-signals.md` — does not accept `signals.md`. Fails on migrated projects. Fix: add `signals.md` alongside `inferred-signals.md` as accepted ref target in `checks_refs.go`.

## Docs

- `docs/spec/signals-workflow.md` — end-to-end lifecycle: scan → inference → @-ref wiring. Canonical for the `atomic-signals` skill and `/initialize-signals` / `/refresh-signals` commands.
- `docs/spec/signals-router.md` — router file shape, domain file layout, inferrer behavior, incremental vs full mode, budget model (split trigger at ~1k lines / ~5k tokens), naming continuity rule.
- `docs/spec/signals-project-detection.md` — project detection heuristics used by the tree walker to locate the primary source root.
- `docs/reference/signals-workflow.md` — thin pointer (11L) to the spec. User-facing entry point.
- `docs/design/signals-router.md` — design rationale for the router shape: why flat eager-load fails at scale, domain partitioning approach, change detection via git per-path SHAs, incremental refresh design.

## Coupling

- **→ bundle**: `atomic-signals-inferrer` agent ships in the bundle via `agents/atomic-*.md` bundlespec rule. Changes to the agent file require `make bundle`.
- **→ bundle**: `atomic-signals` skill ships in the bundle via `skills/atomic-*/` bundlespec rule. Changes to `skills/atomic-signals/SKILL.md` require `make render` then `make bundle` (render is not needed here but bundle is).
- **→ doctor**: `checks_refs.go` (doctor domain) has a known bug where it fails on projects using `signals.md` instead of the old `inferred-signals.md`. Fix touches doctor domain.
- **→ config**: `output.signals.max_depth` config key is read by `atomic/internal/signals/tree.go`. Config schema changes propagate to signals behavior.
- **→ workflow**: ship verbs invoke the `atomic-signals` skill after staged changes. If signals skill changes its trigger surface or output contract, ship verb templates (bundle domain) must be updated in lockstep.

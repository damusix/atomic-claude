# bundle

## What it does

Template rendering → bundle embedding → install/uninstall into `~/.claude/`. Human-authored markdown in `templates/` and `agents/`/`skills/` is rendered by `make render` (commands) and embedded by `make bundle` into the Go binary. `atomic claude install` copies the embedded bundle to `~/.claude/`.

## Artifacts

**Source (human edit surface — never generate these directly):**

- `templates/commands/*.md` — 31 per-verb command source files. Edit surface for slash commands.
- `templates/shared/*.md` — 10 reusable partials composed via `{{ template "<name>" . }}`. Big partials: `commit-flow`, `pr-flow`, `merge-flow`, `squash-flow`, `push-flow`. Small partials: `doc-impact`, `doc-impact-why`, `signals-gate`, `base-resolution`, `worktree-cleanup-prompt`.

**Rendered/generated (never edit directly):**

- `commands/*.md` — 31 rendered slash command files (`/initialize-signals` removed; `/refresh-signals` is now the single idempotent entry point). Generated from `templates/` by `make render`. Includes `commands/_templates/implementer-prompt.md` and `commands/_templates/reviewer-prompt.md` (runtime prompt partials consumed by orchestrator commands).

**Bundle inputs (the distributed artifact set):**

- `agents/` — 9 subagent definitions (`atomic-*.md`). All ship via `agents/atomic-*.md` bundlespec rule.
- `skills/` — 8 skill directories (`atomic-*/SKILL.md`). Full directory subtree bundled per `atomic-` prefix dir.
- `output-styles/` — 1 output style (`atomic.md`).
- `rules/` — 2 path-scoped topic rules (`python/style.md`, `typescript/style.md`).
- `CLAUDE.md` — global instructions bundled directly. Installed to `~/.claude/CLAUDE.md`.

## CLI code

- `atomic/internal/bundlespec/` — pure inclusion predicate functions (`MatchesAgent`, `MatchesSkillDir`, `MatchesOutputStyle`, `MatchesCommand`, `MatchesRule`, `IsClaudeMd`). Single source of truth for all inclusion decisions. Both `bundlemirror` (build-time) and `manifestcheck` (runtime) import this package — changing a predicate here propagates to both.
- `atomic/internal/bundlemirror/mirror.go` — build-time walker. Reads repo root, calls `bundlespec` predicates, copies matching artifacts into `atomic/internal/embedded/bundle/`, writes `manifest.go`. Commands walk is `filepath.WalkDir` (recursive) so `commands/_templates/` is included. Called by `atomic/cmd/bundle-mirror/main.go` via `go generate`.
- `atomic/internal/embedded/` — holds `//go:embed all:bundle` FS (`bundle.go`) and generated `Manifest()` slice (`manifest.go`). `all:` prefix required — without it, `commands/_templates/` (underscore prefix) would be silently absent. Never edit `bundle/` or `manifest.go` by hand.
- `atomic/cmd/bundle-mirror/main.go` — build-time entrypoint for `bundlemirror.Run`. Called by `go generate ./...`.
- `atomic/internal/templaterender/` — Go `text/template` renderer. Reads `templates/commands/*.md` (per-verb) and `templates/shared/*.md` (partials). Writes `commands/*.md`. Orphan guard: any `commands/<verb>.md` without a matching `templates/commands/<verb>.md` halts render with non-zero exit.
- `atomic/cmd/render-templates/main.go` — build-time entrypoint for `templaterender.Render`. Called by `make render`.
- `atomic/internal/claudeinstall/install.go` — install/update/diff/list verbs. SHA256-based idempotency. Backs up changed files to `~/.claude/.atomic/backups/<ts>/`. Writes proposed merge to `~/.claude/.atomic/proposed/CLAUDE.md` when installed `CLAUDE.md` diverges. Pre-creates `config.resolved.md` on every `Apply`. Special-cases `CLAUDE.md` as merge-required (never silently overwrites user customizations).
- `atomic/internal/manifestcheck/` — runtime bundle validator. Calls `bundlespec` predicates to verify the embedded manifest matches expectations. Used by doctor check index 5 (`manifest`).

## Docs

- `docs/spec/artifact-templates.md` — render pipeline contract: engine behavior, orphan rule, partial taxonomy, pipeline order.
- `docs/spec/install-workflow.md` — `atomic claude install/update` flow, SHA256 idempotency, backup strategy, CLAUDE.md merge-required guard.
- `docs/spec/atomic-binary.md` — master spec for all `atomic` CLI subcommands (690L). Governs `atomic claude install/update/diff/list`.
- `docs/guides/contributing.md` — contributor workflow: `make render` before `make bundle`, pre-commit hook, artifact additions checklist.
- `docs/guides/install.md` — user-facing install walkthrough.
- `docs/design/artifact-templates.md` — design rationale for the template rendering system.

**Pre-commit hook (`.githooks/pre-commit`)** — three-stage pipeline:

1. If `templates/` files staged → `make render`, re-stages `commands/`.
2. If any source artifact staged (`agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, `CLAUDE.md`) → `make bundle`, re-stages `atomic/internal/embedded/bundle/` and `manifest.go`.
3. If `.claude/project/followups/` files staged (excluding INDEX.md) → `atomic followups render`, re-stages INDEX.md.

Render fires before bundle (bundle reads what render wrote). Install via `make hooks`; uninstall via `make hooks-uninstall`.

## Coupling

- **→ signals**: `commands/` output is read by `bundlemirror` downstream of the template render step. Pipeline order is load-bearing: `make render` must precede `make bundle`.
- **→ workflow**: ship verb commands (in `commands/`) are rendered from `templates/commands/` + `templates/shared/` partials. Editing any ship verb flow requires editing the template source, not the rendered output.
- **→ doctor**: `atomic/internal/manifestcheck/` (used by doctor check 5) imports `bundlespec`. Changing a bundlespec predicate affects which items fail the manifest check.
- **→ config**: `claudeinstall.Apply` pre-creates `~/.claude/.atomic/config.resolved.md`. Config domain owns the content of that file; bundle domain writes it as a side effect of install.
- **→ docs-meta**: `CLAUDE.md` (bundle input) must stay synchronized with `CLAUDE.md` (the committed project instructions). Adding a new artifact requires updating both.

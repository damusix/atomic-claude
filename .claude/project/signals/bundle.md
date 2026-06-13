# bundle

## What it does

Template rendering → bundle embedding → install/uninstall into `~/.claude/`. Human-authored markdown in [`templates/commands/`](../../../templates/commands) and [`templates/agents/`](../../../templates/agents) is rendered by `make render` into [`commands/`](../../../commands) and [`agents/`](../../../agents); `make bundle` embeds the outputs plus [`skills/`](../../../skills), [`output-styles/`](../../../output-styles), [`rules/`](../../../rules), and [`CLAUDE.md`](../../../CLAUDE.md) into the Go binary. `atomic claude install` copies the embedded bundle to `~/.claude/`.

## Artifacts

**Source (human edit surface — never generate these directly):**

- `templates/commands/*.md` — 23 per-verb command source files. Edit surface for slash commands.
- `templates/agents/*.md` — 5 agent source files. Edit surface for subagent definitions. Mirrors the commands template pattern.
- `templates/shared/*.md` — 19 reusable partials composed via `{{ template "<name>" . }}`. Big command partials: `commit-flow`, `pr-flow`, `merge-flow`, `squash-flow`, `push-flow`. Small command partials: `doc-impact`, `signals-gate`, `base-resolution`, `worktree-cleanup-prompt`, `worktree-setup`, `git-safety`, `staleness-check`. Agent partials: `agent-tdd-signals` (TDD + quality-signals steps), `agent-signals-output` (output block format), `agent-shared-rules` (style/git/error-quoting constraints), `agent-search-tooling` (grep/glob/sg tool-selection rule; one-line bridge names code-intel as top search tier when index present), `agent-implementer-workflow` (shared `<workflow>` block for `atomic-implementer.md`, composes `agent-search-tooling` + `agent-tdd-signals` + `agent-code-intel`), `agent-atomic-voice` (response-voice rule injected into all 5 agent templates — instructs subagent to return findings only, no preamble or recap), `agent-code-intel` (`atomic code` verb guidance with graceful degradation contract; composed into `atomic-implementer.md` via `agent-implementer-workflow`, and directly into `atomic-investigator.md`, `atomic-reviewer.md`, and `atomic-signals-inferrer.md`).

**Rendered/generated (never edit directly):**

- `commands/*.md` — 22 rendered slash command files. Generated from [`templates/commands/`](../../../templates/commands) + [`templates/shared/`](../../../templates/shared) by `make render`.  Includes [`commands/_templates/implementer-prompt.md`](../../../commands/_templates/implementer-prompt.md) and [`commands/_templates/reviewer-prompt.md`](../../../commands/_templates/reviewer-prompt.md) (runtime prompt partials consumed by orchestrator commands).
- `agents/*.md` — 5 rendered subagent definition files. Generated from [`templates/agents/`](../../../templates/agents) + [`templates/shared/`](../../../templates/shared) by `make render`. `atomic-implementer.md` pulls `{{ template "agent-implementer-workflow" . }}` (which composes `agent-search-tooling` + `agent-tdd-signals` + `agent-code-intel`) plus `{{ template "agent-signals-output" . }}` and `{{ template "agent-shared-rules" . }}`. `atomic-investigator.md`, `atomic-reviewer.md`, and `atomic-signals-inferrer.md` each include `agent-code-intel` directly.

**Bundle inputs (the distributed artifact set):**

- [`agents/`](../../../agents) — 5 subagent definitions (`atomic-*.md`), now rendered outputs. All ship via `agents/atomic-*.md` bundlespec rule.
- [`skills/`](../../../skills) — 8 skill directories (`atomic-*/SKILL.md`). Full directory subtree bundled per `atomic-` prefix dir.
- [`output-styles/`](../../../output-styles) — 1 output style (`atomic.md`).
- [`rules/`](../../../rules) — 3 path-scoped topic rules (`python/style.md`, `typescript/style.md`, `specs/spec-currency.md`). `specs/spec-currency.md` has `paths: ["docs/spec/**/*.md", "docs/design/**/*.md"]` — auto-loads when touching spec or design files; enforces "body is current truth, change log is history".
- [`CLAUDE.md`](../../../CLAUDE.md) — global instructions bundled directly. Installed to `~/.claude/CLAUDE.md`. Atomic-owned content is bounded by `<atomic>...</atomic>` tags; everything outside those tags is user-owned. `atomicblock.go` uses this boundary during install to swap the block in place.

## CLI code

- [`atomic/internal/bundlespec/`](../../../atomic/internal/bundlespec) — pure inclusion predicate functions (`MatchesAgent`, `MatchesSkillDir`, `MatchesOutputStyle`, `MatchesCommand`, `MatchesRule`, `IsClaudeMd`). Single source of truth for all inclusion decisions. Both `bundlemirror` (build-time) and `manifestcheck` (runtime) import this package — changing a predicate here propagates to both.
- [`atomic/internal/bundlemirror/mirror.go`](../../../atomic/internal/bundlemirror/mirror.go) — build-time walker. Reads repo root, calls `bundlespec` predicates, copies matching artifacts into [`atomic/internal/embedded/bundle/`](../../../atomic/internal/embedded/bundle), writes `manifest.go`. Commands walk is `filepath.WalkDir` (recursive) so [`commands/_templates/`](../../../commands/_templates) is included. Called by [`atomic/cmd/bundle-mirror/main.go`](../../../atomic/cmd/bundle-mirror/main.go) via `go generate`.
- [`atomic/internal/embedded/`](../../../atomic/internal/embedded) — holds `//go:embed all:bundle` FS (`bundle.go`) and generated `Manifest()` slice (`manifest.go`). `all:` prefix required — without it, [`commands/_templates/`](../../../commands/_templates) (underscore prefix) would be silently absent. Never edit `bundle/` or `manifest.go` by hand.
- [`atomic/cmd/bundle-mirror/main.go`](../../../atomic/cmd/bundle-mirror/main.go) — build-time entrypoint for `bundlemirror.Run`. Called by `go generate ./...`.
- [`atomic/internal/templaterender/`](../../../atomic/internal/templaterender) — Go `text/template` renderer. Iterates `renderedKinds = ["commands", "agents"]`; for each kind renders `templates/<kind>/*.md` (using shared partials from [`templates/shared/`](../../../templates/shared)) into `<outDir>/<kind>/*.md`. Single shared-partial pool is cloned per file, so all partials (command and agent) are available to any template. Orphan guard: any `<kind>/<name>.md` without a matching `templates/<kind>/<name>.md` halts render with non-zero exit naming both remediation paths (create template OR `rm` output).
- [`atomic/cmd/render-templates/main.go`](../../../atomic/cmd/render-templates/main.go) — build-time entrypoint for `templaterender.Render`. Called by `make render`.
- [`atomic/internal/claudeinstall/atomicblock.go`](../../../atomic/internal/claudeinstall/atomicblock.go) — `<atomic>...</atomic>` block parser. `atomicBlockBounds` returns byte offsets of the single block (line-anchored tag matching). `replaceAtomicBlock` swaps the block in place, preserving user content outside it byte-for-byte. `atomicBlocksEqual` compares blocks across two files. Ambiguous shapes (no tags, unclosed, multiple blocks) return `!ok`, triggering fallback to the LLM merge path.
- [`atomic/internal/claudeinstall/install.go`](../../../atomic/internal/claudeinstall/install.go) — install/update/diff/list verbs. SHA256-based idempotency. Backs up changed files to `~/.claude/.atomic/backups/<ts>/`. `ActionBlockReplaced` (new kind): CLAUDE.md only — when both on-disk and embedded carry a parseable `<atomic>` block and the blocks differ, `replaceAtomicBlock` swaps the block in place; user content outside the block is preserved byte-for-byte; no proposed-file is written. `ActionMergeRequired` fires only when no parseable block exists (pre-tag installs, malformed tags); the install output directs the user to run `atomic prompt claude-merge` in a Claude Code session. Pre-creates `config.resolved.md` on every `Apply`. Calls `profile.CaptureEnv` + `profile.RenderStub` to write `~/.claude/.atomic/profile.md` on first install (skips if file already exists).
- [`atomic/internal/profile/profile.go`](../../../atomic/internal/profile/profile.go) — `CaptureEnv()` reads `git config --global user.name/user.email` + `runtime.GOOS/GOARCH/NumCPU`. `RenderStub(Env)` returns the initial `profile.md` content with six sections tagged `<stable>`, `<volatile>`, or `<deterministic>`. Called by `claudeinstall` at install time and by [`atomic/internal/claudeinstall/uninstall.go`](../../../atomic/internal/claudeinstall/uninstall.go) (uninstall removes profile.md). Git failures produce empty strings — install is not aborted.
- [`atomic/internal/manifestcheck/`](../../../atomic/internal/manifestcheck) — runtime bundle validator. Calls `bundlespec` predicates to verify the embedded manifest matches expectations. Used by doctor check index 5 (`manifest`).

## Docs

- [`docs/spec/artifact-templates.md`](../../../docs/spec/artifact-templates.md) — render pipeline contract: engine behavior, orphan rule, partial taxonomy, pipeline order.
- [`docs/spec/install-workflow.md`](../../../docs/spec/install-workflow.md) — `atomic claude install/update` flow, SHA256 idempotency, backup strategy, CLAUDE.md merge-required guard.
- [`docs/spec/atomic-binary.md`](../../../docs/spec/atomic-binary.md) — master spec for all [`atomic`](../../../atomic) CLI subcommands (690L). Governs `atomic claude install/update/diff/list`.
- [`docs/guides/contributing.md`](../../../docs/guides/contributing.md) — contributor workflow: `make render` before `make bundle`, pre-commit hook, artifact additions checklist.
- [`docs/guides/install.md`](../../../docs/guides/install.md) — user-facing install walkthrough.
- [`docs/design/artifact-templates.md`](../../../docs/design/artifact-templates.md) — design rationale for the template rendering system.

**Pre-commit hook ([`.githooks/pre-commit`](../../../.githooks/pre-commit))** — three-stage pipeline:

1. If [`templates/`](../../../templates) files staged → `make render`, re-stages [`commands/`](../../../commands) **and** [`agents/`](../../../agents) outputs.
2. If any source artifact staged ([`agents/`](../../../agents), [`commands/`](../../../commands), [`skills/`](../../../skills), [`output-styles/`](../../../output-styles), [`rules/`](../../../rules), [`CLAUDE.md`](../../../CLAUDE.md)) → `make bundle`, re-stages [`atomic/internal/embedded/bundle/`](../../../atomic/internal/embedded/bundle) and `manifest.go`.
3. If [`.claude/project/followups/`](../followups) files staged (excluding INDEX.md) → `atomic followups render`, re-stages INDEX.md.

Render fires before bundle (bundle reads what render wrote). Install via `make hooks`; uninstall via `make hooks-uninstall`.

## Coupling

- **→ signals**: [`commands/`](../../../commands) output is read by `bundlemirror` downstream of the template render step. Pipeline order is load-bearing: `make render` must precede `make bundle`.
- **→ workflow**: ship verb commands (in [`commands/`](../../../commands)) are rendered from [`templates/commands/`](../../../templates/commands) + [`templates/shared/`](../../../templates/shared) partials. Editing any ship verb flow requires editing the template source, not the rendered output.
- **→ doctor**: [`atomic/internal/manifestcheck/`](../../../atomic/internal/manifestcheck) (used by doctor check 5) imports `bundlespec`. Changing a bundlespec predicate affects which items fail the manifest check.
- **→ config**: `claudeinstall.Apply` pre-creates `~/.claude/.atomic/config.resolved.md`. Config domain owns the content of that file; bundle domain writes it as a side effect of install.
- **→ docs-meta**: [`CLAUDE.md`](../../../CLAUDE.md) (bundle input) must stay synchronized with [`CLAUDE.md`](../../../CLAUDE.md) (the committed project instructions). Adding a new artifact requires updating both.
- **→ doctor**: [`atomic/internal/profile/`](../../../atomic/internal/profile) is exercised by doctor category 10 (`checks_profile.go`). Changes to `CaptureEnv` or `RenderStub` (profile package, this domain) affect what doctor checks as "profile wired".

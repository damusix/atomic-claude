---
generated_at: 2026-05-25T01:11:45Z
source: .claude/project/deterministic-signals.md
---

# Project signals

## Framework & runtime

- **Go 1.23**, single module at `atomic/go.mod` (`module github.com/damusix/atomic-claude/atomic`). CLI binary named `atomic`.
- **External Go deps**: `gopkg.in/yaml.v3` (YAML frontmatter), `github.com/tailscale/hujson` (lenient JSON for hooks/settings), `github.com/pelletier/go-toml/v2` (TOML config), `github.com/charmbracelet/huh` (interactive TUI prompts).
- **Go embed**: `//go:embed all:bundle` in `atomic/internal/embedded/bundle.go`. `all:` prefix required to include `commands/_templates/` (underscore prefix skipped by plain `bundle` glob).
- **goreleaser**: multi-platform release — `linux/darwin x amd64/arm64`. CGO disabled. Version from `internal/version` via ldflags.
- **release-please**: automated changelog + tag on main push.
- **VitePress docs site**: `package.json` (`name=atomic-claude-docs`), `package-lock.json`, `.vitepress/config.mts`. Scripts: `docs:build`, `docs:dev`, `docs:preview`. Deployed via `.github/workflows/docs.yml`. Not part of the Go build.
- No web framework. No database.

## Build / test / lint

All commands run from `atomic/` (CI: `working-directory: ./atomic`):

| Purpose | Command | Source |
|---------|---------|--------|
| Render command templates | `make render` (root) or `make -C atomic render` | `atomic/Makefile`, CI "Verify render is committed" |
| Regenerate embedded bundle | `go generate ./...` → `make -C atomic bundle` | `atomic/Makefile` target `bundle`, CI "Verify bundle is committed" |
| Run tests | `go test ./...` | `atomic/Makefile` target `test` |
| Vet | `go vet ./...` | `atomic/Makefile` target `vet` |
| Format check | `gofmt -l .` | `atomic/Makefile` target `fmt` |
| Build binary | `go build -o ../bin/atomic ./cmd/atomic` | `atomic/Makefile` target `build` |
| Tidy deps | `go mod tidy` | `atomic/Makefile` target `tidy` |
| Release | `goreleaser release --clean` | `.github/workflows/release.yml` (on `v*` tag) |

CI gates: (1) `make render && git diff --exit-code` — stale `commands/` fails. (2) `make bundle && git diff --exit-code` — stale `manifest.go` fails. Pipeline order: render must run before bundle.

## Language breakdown

| Language | LOC | Files | % |
|----------|-----|-------|---|
| Go | 27417 | 124 | 48% |
| Markdown | 26285 | 227 | 46% |
| JSON | 2552 | 5 | 4% |
| Shell | 269 | 3 | 0% |
| YAML | 212 | 6 | 0% |
| TypeScript | 100 | 1 | 0% |
| Python | 30 | 1 | 0% |

## DevOps & CI

- CI: GitHub Actions (`.github/workflows/`). Four workflows: `ci.yml` (test/vet/fmt/bundle-drift gates), `docs.yml` (VitePress site build/deploy), `release-please.yml` (changelog automation), `release.yml` (goreleaser on tag).
- Release: goreleaser produces multi-arch binaries. release-please manages version + tag.
- Pre-commit hook (`.githooks/pre-commit`): three-stage render→bundle→followups chain. Install via `make hooks`.

## Domains

Each domain groups ALL files across ALL layers (artifacts + CLI code + docs) for one feature concern. Read a domain file when you're working on that feature end-to-end.

| Domain | Repo paths | One-liner | Detail |
|--------|------------|-----------|--------|
| signals | `skills/atomic-signals/`, `agents/atomic-signals-inferrer.md`, `commands/refresh-signals.md`, `atomic/internal/signals/`, `atomic/internal/doctor/checks_signals.go`, `atomic/internal/doctor/checks_refs.go`, `docs/spec/signals-*.md` | Scan → infer → wire: project context generation pipeline | .claude/project/signals/signals.md |
| bundle | `templates/`, `commands/`, `agents/`, `skills/`, `output-styles/`, `rules/`, `CLAUDE.md`, `atomic/internal/bundlespec/`, `atomic/internal/bundlemirror/`, `atomic/internal/embedded/`, `atomic/internal/templaterender/`, `atomic/internal/claudeinstall/` (install + snapshot + uninstall), `atomic/cmd/bundle-mirror/`, `atomic/cmd/render-templates/`, `docs/spec/uninstall.md`, `docs/design/uninstall.md` | Template render → bundle embed → install/uninstall into ~/.claude | .claude/project/signals/bundle.md |
| doctor | `atomic/internal/doctor/`, `atomic/internal/validate/`, `atomic/internal/manifestcheck/`, `atomic/internal/updatedoctor/`, `docs/spec/atomic-doctor.md`, `docs/spec/atomic-validate.md`, `docs/spec/atomic-update-doctor.md` | 9-check integrity suite + static validation + post-update auto-fire | .claude/project/signals/doctor.md |
| workflow | `commands/atomic-plan.md`, `commands/subagent-implementation.md`, `commands/subagent-diagnose.md`, ship verbs (`commands/commit-*.md`, `commands/push-only.md`, etc.), `commands/_templates/`, `agents/atomic-builder.md`, `agents/atomic-surgeon.md`, `agents/atomic-reviewer.md`, `agents/atomic-investigator.md`, `agents/atomic-strategist.md`, `skills/atomic-tdd/`, `skills/atomic-verify/`, `skills/atomic-commit/`, `skills/atomic-review/`, `skills/atomic-debug/` | Plan → implement → review → ship lifecycle | .claude/project/signals/workflow.md |
| config | `commands/follow-up.md`, `commands/remind-me.md`, `commands/git-cleanup.md`, `commands/watch-ci.md`, `commands/atomic-claude-merge.md`, `agents/atomic-git-scout.md`, `agents/atomic-haiku.md`, `agents/atomic-claude-merger.md`, `atomic/internal/config/`, `atomic/internal/hooks/`, `atomic/internal/reminder/`, `atomic/internal/followups/` (migrate.go removed — migration completed), `atomic/internal/prompt/`, `atomic/internal/selfupdate/` | User config, state dir, session hooks, reminders, follow-ups, self-update | .claude/project/signals/config.md |
| docs-meta | `output-styles/atomic.md`, `skills/atomic-documentation/`, `skills/atomic-prose/`, `commands/documentation.md`, `commands/atomic-compress.md`, `.claude/docs/axioms.md`, `.claude/docs/agent-config.md`, `docs/spec/documentation-skill-split.md` | Four-voice taxonomy, surface routing, prose style, design axioms | .claude/project/signals/docs-meta.md |

## Cross-cutting

**Deterministic substrate**: `.claude/project/deterministic-signals.md` — written by `atomic signals scan`. Never edit by hand.

**@-ref wiring**: for this repo, `@-refs` to both signals files live in `claude.local.md` (not `CLAUDE.md` — `CLAUDE.md` is the bundle source and must not carry project-specific paths). For most user repos, refs live in `CLAUDE.md` directly.

**Doctor refs check (doctor ↔ signals) — fixed**: `atomic/internal/doctor/checks_refs.go` (hash `37203fd`) now correctly looks for `@.claude/project/deterministic-signals.md` and `@.claude/project/signals.md`. The prior bug (looking for `inferred-signals.md`) is resolved.

**`go:embed all:bundle` requirement (bundle)**: `commands/_templates/` starts with `_` — excluded by the default embed glob. `all:bundle` overrides this. Any new underscore-prefixed directory under `embedded/bundle/` needs this same consideration.

**Pipeline order is load-bearing (bundle)**: `make render` must precede `make bundle`. The pre-commit hook stages 1 and 2 enforce this order. CI runs the same two drift gates in order. Running only `make bundle` after editing a template embeds stale command outputs.

**Domain partitioning basis**: domains are vertical slices by feature concern, not horizontal layers by file type. Each domain file answers: "if I'm working on X, what artifacts, what CLI code, and what docs do I need?" This replaces the prior horizontal split (artifacts / cli / docs).

**Artifact additions checklist**: adding any new command/agent/skill requires updating the artifact file, `CLAUDE.md`, `CLAUDE.md`, `README.md`, relevant `docs/reference/` tables, `docs/spec/<topic>.md` if non-trivial, cross-references in other artifacts, running `make render` and `make bundle`, and `/refresh-signals`. See `claude.local.md` for the full checklist with per-row guidance.

**`/initialize-signals` removed**: `commands/initialize-signals.md` and `templates/commands/initialize-signals.md` are gone. `/refresh-signals` is now the single idempotent entry point for both first-run init and subsequent refreshes (commands count: 32 → 31).

**Uninstall feature added**: `atomic/internal/claudeinstall/` grew `snapshot.go`, `snapshot_internal_test.go`, `snapshot_test.go`, `uninstall.go`, `uninstall_test.go`. Spec at `docs/spec/uninstall.md`, design at `docs/design/uninstall.md`. Implements `atomic claude uninstall` — reads pre-install snapshot, computes restore plan, LLM-merges modified files.

**VitePress docs site**: not part of the Go build or embedded bundle. `package.json` / `.vitepress/config.mts` are purely for the public docs site. `docs.yml` workflow deploys it. `docs/public/logo.png` is the site logo (same blob as `assets/atomic-claude.png`). JSON LOC jumped from 21 → 2552 entirely due to `package-lock.json`.

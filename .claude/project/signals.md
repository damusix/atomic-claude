---
generated_at: 2026-06-06T23:23:51Z
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
| Markdown | 40573 | 276 | 49% |
| Go | 38258 | 155 | 46% |
| JSON | 2295 | 5 | 2% |
| CSS | 691 | 1 | 0% |
| Shell | 269 | 3 | 0% |
| YAML | 218 | 6 | 0% |
| TypeScript | 205 | 3 | 0% |
| Vue | 183 | 1 | 0% |

## DevOps & CI

- CI: GitHub Actions (`.github/workflows/`). Four workflows: `ci.yml` (test/vet/fmt/bundle-drift gates), `docs.yml` (VitePress site build/deploy), `release-please.yml` (changelog automation), `release.yml` (goreleaser on tag).
- Release: goreleaser produces multi-arch binaries. release-please manages version + tag.
- Pre-commit hook (`.githooks/pre-commit`): three-stage render→bundle→followups chain. Install via `make hooks`.

## Domains

Each domain groups ALL files across ALL layers (artifacts + CLI code + docs) for one feature concern. Read a domain file when you're working on that feature end-to-end.

| Domain | Repo paths | One-liner | Detail |
|--------|------------|-----------|--------|
| signals | `agents/atomic-signals-inferrer.md`, `commands/refresh-signals.md`, `atomic/internal/signals/`, `atomic/internal/doctor/checks_signals.go`, `atomic/internal/doctor/checks_refs.go`, `docs/spec/signals-*.md` | Scan → infer → wire: project context generation pipeline | .claude/project/signals/signals.md |
| bundle | `templates/commands/`, `templates/agents/`, `templates/shared/`, `commands/`, `agents/`, `skills/`, `output-styles/`, `rules/`, `CLAUDE.md`, `atomic/internal/bundlespec/`, `atomic/internal/bundlemirror/`, `atomic/internal/embedded/`, `atomic/internal/templaterender/`, `atomic/internal/claudeinstall/` (install + snapshot + uninstall), `atomic/cmd/bundle-mirror/`, `atomic/cmd/render-templates/`, `docs/spec/uninstall.md`, `docs/design/uninstall.md` | Template render (commands + agents) → bundle embed → install/uninstall into ~/.claude | .claude/project/signals/bundle.md |
| doctor | `atomic/internal/doctor/`, `atomic/internal/validate/`, `atomic/internal/manifestcheck/`, `atomic/internal/updatedoctor/`, `atomic/internal/profile/`, `docs/spec/atomic-doctor.md`, `docs/spec/atomic-validate.md`, `docs/spec/atomic-update-doctor.md`, `docs/spec/user-profile.md`, `docs/design/user-profile.md` | 10-check integrity suite + static validation + post-update auto-fire + user profile | .claude/project/signals/doctor.md |
| workflow | `commands/autopilot.md`, `commands/atomic-plan.md`, `commands/gather-evidence.md`, `commands/subagent-implementation.md`, `commands/subagent-diagnose.md`, `commands/atomic-setup.md`, `commands/atomic-improve.md`, ship verbs (`commands/commit-*.md`, `commands/push-only.md`, etc.), `commands/_templates/`, `agents/atomic-builder.md`, `agents/atomic-surgeon.md`, `agents/atomic-reviewer.md`, `agents/atomic-investigator.md`, `agents/atomic-strategist.md`, `skills/atomic-tdd/`, `skills/atomic-verify/`, `skills/atomic-commit/`, `skills/atomic-review/`, `skills/atomic-debug/` | Plan → gather-evidence → implement → review → ship → retrospective lifecycle; `/autopilot` runs it hands-off | .claude/project/signals/workflow.md |
| config | `commands/follow-up.md`, `commands/remind-me.md`, `commands/git-cleanup.md`, `commands/watch-ci.md`, `commands/atomic-claude-merge.md`, `agents/atomic-git-scout.md`, `agents/atomic-haiku.md`, `agents/atomic-claude-merger.md`, `atomic/internal/config/`, `atomic/internal/hooks/`, `atomic/internal/reminder/`, `atomic/internal/followups/`, `atomic/internal/prompt/`, `atomic/internal/selfupdate/` | User config, state dir (profile.md, config.toml), session hooks, reminders, follow-ups, self-update | .claude/project/signals/config.md |
| docs-meta | `output-styles/atomic.md`, `skills/atomic-documentation/`, `skills/atomic-prose/`, `commands/documentation.md`, `.claude/docs/axioms.md`, `.claude/docs/agent-config.md`, `docs/spec/documentation-skill-split.md`, `docs/spec/documentation-as-maintenance.md` | Two-voice taxonomy, diff-driven surface routing, prose style, design axioms | .claude/project/signals/docs-meta.md |
| wiki | `commands/refresh-wiki.md`, `atomic/internal/wiki/`, `docs/spec/wiki.md`, `docs/design/wiki.md`, `docs/reference/wiki-workflow.md`, `docs/reference/concepts.md` (Wikis section), `docs/credits.md` | Cross-repo knowledge layer: scan → stale → incremental LLM refresh; ship-time dirty marker; session-start nudge | .claude/project/signals/wiki.md |

## Cross-cutting

**Deterministic substrate**: `.claude/project/deterministic-signals.md` — written by `atomic signals scan`. Never edit by hand.

**@-ref wiring**: for this repo, `@.claude/project/signals.md` lives in `claude.local.md` (not `CLAUDE.md` — `CLAUDE.md` is the bundle source and must not carry project-specific paths). Only `signals.md` is `@-ref`'d. `deterministic-signals.md` is NOT `@-ref`'d — it can be thousands of lines on large repos and would blow up context. The inferrer reads it when needed; sessions do not.

**Doctor refs check updated**: `atomic/internal/doctor/checks_refs.go` (hash `477404b`) now checks only for `@.claude/project/signals.md`. Prior contract (requiring both `deterministic-signals.md` and `signals.md`) is superseded per spec change 2026-05-26.

**Doctor hooks scope bug fixed**: `checks_hooks.go` `checkHooks` now passes `$HOME` as scopeRoot to `RunCheckHooksWith` — not `~/.claude`. Passing `~/.claude` caused `hooks.IsInstalled` to look for `~/.claude/.claude/settings.json` (double `.claude` segment). `RunCheckHooksWith(scopeRoot)` is exported for tests; `checks_hooks_internal_test.go` added for package-internal test coverage.

**Hooks inline-command migration**: `atomic/internal/hooks/hooks.go` `Install` now registers the literal string `atomic hooks session-start` directly in `settings.json`'s `SessionStart` hook array — no wrapper script written. Legacy installs that registered `~/.claude/hooks/session-start-reminders.sh` are detected via `IsInstalled` `drifted=true` return and migrated away by `migrateLegacy` on the next `Install` call. `legacyScriptName` / `legacyHooksSubdir` constants retained solely for this migration path.

**`go:embed all:bundle` requirement (bundle)**: `commands/_templates/` starts with `_` — excluded by the default embed glob. `all:bundle` overrides this. Any new underscore-prefixed directory under `embedded/bundle/` needs this same consideration.

**Pipeline order is load-bearing (bundle)**: `make render` must precede `make bundle`. The pre-commit hook stages 1 and 2 enforce this order; stage 1 re-stages both `commands/` and `agents/` after render. CI runs the same two drift gates in order. Running only `make bundle` after editing a template embeds stale command or agent outputs.

**Agent template partial pipeline**: `templates/agents/` now mirrors `templates/commands/` — agent source files are rendered, not directly edited. Five agent-specific partials live in `templates/shared/`: `agent-tdd-signals` (TDD + quality signals workflow steps), `agent-signals-output` (output block format), `agent-shared-rules` (style/git/error-quoting constraints), `agent-search-tooling` (grep/glob/sg tool-selection rule), `agent-implementer-workflow` (shared `<workflow>` block for builder/surgeon, composes `agent-search-tooling` + `agent-tdd-signals`). `atomic-builder.md` and `atomic-surgeon.md` compose all five via `agent-implementer-workflow`. `make render` produces `agents/*.md`; `make bundle` embeds them. Editing `agents/*.md` directly is overwritten on next render — edit `templates/agents/*.md` instead.

**Domain partitioning basis**: domains are vertical slices by feature concern, not horizontal layers by file type. Each domain file answers: "if I'm working on X, what artifacts, what CLI code, and what docs do I need?"

**Artifact additions checklist**: adding any new command/agent/skill requires updating the artifact file, `CLAUDE.md`, `CLAUDE.md`, `README.md`, relevant `docs/reference/` tables, `docs/spec/<topic>.md` if non-trivial, cross-references in other artifacts, running `make render` and `make bundle`, and `/refresh-signals`. See `claude.local.md` for the full checklist with per-row guidance.

**`/refresh-signals` is the single idempotent entry point**: `/initialize-signals` was removed (commit 4011b30). `/refresh-signals` handles both first-run init and subsequent refreshes. The `atomic-signals` skill was removed and absorbed into `atomic-signals-inferrer` — the agent now owns the full pipeline (scan + infer + wire).

**`atomic signals stale` exit-code contract**: returns `(StaleInfo, error)`. Exit 0 = fresh; exit 1 = stale (stdout has imperative evidence lines); exit 2 = hard error (binary absent, scan failed, etc.). `signals diff` and `atomic update --check` use the same check-family exit convention. Spec: `docs/spec/atomic-binary.md`.

**Uninstall feature**: `atomic/internal/claudeinstall/` contains `snapshot.go`, `snapshot_internal_test.go`, `snapshot_test.go`, `uninstall.go`, `uninstall_test.go`. Spec at `docs/spec/uninstall.md`, design at `docs/design/uninstall.md`. Implements `atomic claude uninstall` — reads pre-install snapshot, computes restore plan, LLM-merges modified files.

**VitePress docs site**: not part of the Go build or embedded bundle. `package.json` / `.vitepress/config.mts` are purely for the public docs site. `docs.yml` workflow deploys it.

**Typed follow-ups (config domain)**: `atomic/internal/followups/entry.go` defines `Kind` type (`KindFinding`/`KindPlan`). Missing `kind` parses as `KindFinding` (back-compat — existing 19 entries unchanged). `--severity` optional when `--kind plan`. `Render` places `## 📋 plans` first; plans are exempt from staleness. Plans are filed via `atomic followups add --kind plan`. Spec: `docs/spec/typed-followups.md`.

**Wiki feature (wiki domain)**: `atomic/internal/wiki/` implements `atomic wiki scan` (scaffold + classify + `<wiki-scan>` block + `<wikis>` registry), `atomic wiki stale` (membership drift + per-artifact fingerprint drift), `atomic wiki stamp` (writes `reflects_rev`/`reflects:` fingerprints into YAML frontmatter — code-only, model never writes fingerprints), and `atomic wiki mark-dirty` (touches `.dirty` on ship). `/refresh-wiki` dispatches `atomic-signals-inferrer` in wiki-output mode using `atomic signals scan --out <tmp>` to avoid writing into the target repo. The `signals-gate` partial (shared across all ship verbs) invokes `atomic wiki mark-dirty` after signals refresh. Session-start hook calls `wiki.CheckStaleness` best-effort with 30-day default (reads from memory per axiom 2). `atomic-claude-merger` preserves the `<wikis>` block verbatim on merge.

**`atomic-documentation` skill is two-mode**: maintenance mode (fires during ship verbs — flags stale/incomplete only) and authoring mode (`/documentation` — full discovery, gap detection, content generation). `atomic docs scan` / `atomic docs stale` are binary subcommands that support it.

**Stuck-fix escalation + suppression awareness (workflow domain)**: `/subagent-implementation` Step C now carries a loop-default stuck-fix escalator — after 2 consecutive `CHANGES_REQUESTED` rounds on the same root-cause blocking signal, surfaces `/pressure-test @<spec>` + `atomic-strategist` RCA offer; never auto-dispatched (axiom 3). `/subagent-diagnose` same-failure bail (3 consecutive normalized errors) enriched with the same RCA options. `atomic-reviewer` (`commands/_templates/reviewer-prompt.md`, `templates/agents/atomic-reviewer.md`) gains Step 5 suppression-pattern check: flags error-catching-without-investigation (🟡 default, 🔴 on 2+ repeat of same error). Spec: `docs/spec/stuck-fix-escalation.md`. Design: `docs/design/stuck-fix-escalation.md`. Closes issue #29. `atomic-strategist`'s "stuck or repeatedly failing review" dispatch trigger now has a wired caller.

**User profile v2**: `atomic/internal/profile/` contains `profile.go`, `detect.go`, `render.go`, `refresh.go` (+ tests). `detect.go` — detection registry of 57 `RegistryEntry` entries across 7 `ToolCategory` values (language-runtime, version-manager, package-manager, container, monorepo, cli, cloud); `DetectAll(DetectOptions)` runs parallel detection; `DetectShell(ShellEnvOptions)` captures shell env. `render.go` — `RenderEnvironmentSection(Env, []ToolResult, ShellResult, date)` produces the `## Environment` block stamped with `<deterministic lastcheck=YYYY-MM-DD>`; `RewriteEnvironmentSection(content, section)` splices it in-place. `refresh.go` — `Refresh(claudeHome, date)` rewrites profile.md atomically (write-tmp-then-rename); `RefreshIfStale(claudeHome, today, days)` is a no-op when `lastcheck` is within the window; `IsStale(lastcheck, today, days)` and `ParseLastcheck(content)` are the staleness primitives. `atomic profile refresh [--if-stale <Nd>]` subcommand dispatched from `main.go:profileAction`. Session-start hook (`atomic/internal/hooks/hooks.go`) fires `profile.RefreshIfStale` best-effort with 7-day window via injectable `ProfileRefresh` seam. Doctor category 10 (`checks_profile.go`) gained a third leg: `lastcheck` freshness check against `profileStaleDays = 30`. v1-format files (no `lastcheck`) trigger WARN directing user to run `atomic profile refresh`. `config.ProfilePath` / `config.ProfileRelPath` in `atomic/internal/config/paths.go` remain the canonical path derivation. Spec: `docs/spec/user-profile.md`. Design: `docs/design/user-profile.md`.

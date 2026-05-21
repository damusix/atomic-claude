---
generated_at: 2026-05-21T07:00:00Z
source: .claude/project/deterministic-signals.md
---

# Inferred signals

## Framework / runtime

- **Go 1.23**, single module at `atomic/go.mod` (`module github.com/damusix/atomic-claude/atomic`). Runtime target: CLI binary named `atomic`.
- **External Go dependencies**: `gopkg.in/yaml.v3 v3.0.1` (YAML frontmatter parsing, `atomic/internal/frontmatter/`), `github.com/tailscale/hujson` (lenient JSON for hooks config, `atomic/internal/hooks/hooks_hujson.go`), `github.com/pelletier/go-toml/v2` (TOML config read/write, `atomic/internal/config/`).
- **Go embed** (`//go:embed bundle`, `atomic/internal/embedded/bundle.go`): artifact bundle is embedded at build time via `go:generate`.
- **goreleaser** (`.goreleaser.yaml`): multi-platform release pipeline producing `linux/darwin × amd64/arm64` binaries. macOS/Linux only — Windows out of scope (`claude.local.md`). CGO disabled. Version injected via ldflags from `internal/version`.
- **release-please** (`.github/workflows/release-please.yml`, `release-please-config.json`, `release-please-manifest.json`): automated changelog and tag generation on main.
- No web framework. No database. No runtime dependencies outside the Go standard library and the three listed third-party modules.

## Build / test / lint commands

All commands run from `atomic/` (CI sets `working-directory: ./atomic`):

| Purpose | Command | Source |
|---------|---------|--------|
| Regenerate embedded bundle | `go generate ./...` (calls `cmd/bundle-mirror`) | `atomic/Makefile` target `bundle`, `.github/workflows/ci.yml` |
| Run tests | `go test ./...` | `atomic/Makefile` target `test`, `ci.yml` step `Test` |
| Vet | `go vet ./...` | `atomic/Makefile` target `vet`, `ci.yml` step `Vet` |
| Format check | `gofmt -l .` | `atomic/Makefile` target `fmt`, `ci.yml` step `Format check` |
| Build binary | `go build -o ../bin/atomic ./cmd/atomic` | `atomic/Makefile` target `build` |
| Tidy deps | `go mod tidy` | `atomic/Makefile` target `tidy` |
| Release | `goreleaser release --clean` (triggered on `v*` tag push) | `.github/workflows/release.yml` |

CI gate: `go generate ./...` followed by `git diff --exit-code` enforces that the committed `manifest.go` matches the generated bundle — a stale manifest is a CI failure (`.github/workflows/ci.yml` step "Verify bundle is committed").

## Architectural style

**Dual-product repo**: a Go CLI binary (`atomic/`) co-located with the Claude Code configuration artifacts it manages (`agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, `CLAUDE.md`).

The repo has two logical layers:

1. **Source-of-truth artifacts** (root: `agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, `CLAUDE.md`) — human-authored markdown files that are both the live dogfood config for this repo and the bundle input.
2. **Go CLI** (`atomic/`) — reads those artifacts, embeds them at build time, and provides subcommands to install/update them into `~/.claude`, scan project signals, manage reminders, manage hooks, and self-update.

Internal package layout inside `atomic/internal/`:

- `bundlemirror/` — build-time mirror: walks repo root, copies matching artifacts into `embedded/bundle/`, writes `manifest.go`
- `embedded/` — holds the `go:embed`-ed `bundle/` FS and the generated `Manifest()` slice; bundle is 51 total items
- `claudeinstall/` — install/update/diff/list verbs; SHA256-based idempotency; backs up changed files under `~/.claude/.atomic/backups/<timestamp>/`; writes proposed merge to `~/.claude/.atomic/proposed/CLAUDE.md`; pre-creates `~/.claude/.atomic/config.resolved.md` on every `Apply`; special-cases `CLAUDE.md` as merge-required
- `config/` — TOML-backed config (`~/.claude/.atomic/config.toml`); v1 schema has one key: `output.intensity` (`lite|full|ultra`, default `full`); lenient load (unknown keys → warnings, not errors); atomic write via `os.Rename`; renders resolved values to `~/.claude/.atomic/config.resolved.md` (markdown snapshot `@`-ref'd from bundled `CLAUDE.md`); Levenshtein typo-suggestion on unknown key names; uses `github.com/pelletier/go-toml/v2`
- `signals/` — tree walker, manifest scanner, language counter; writes `.claude/project/deterministic-signals.md`; keeps `.deterministic-signals.prev.md` for diff; uses `git diff` when in a git repo, falls back to `diff(1)`
- `hooks/` — session-start hook payload generation and install/uninstall
- `reminder/` — file-based reminder CRUD
- `selfupdate/` — GitHub Releases API lookup, SHA256-verified binary download, atomic replace via `os.Rename` with cross-filesystem fallback
- `repoctx/`, `frontmatter/`, `ids/`, `version/` — support utilities

Not a monorepo (single go.mod). Not a library (no exported packages intended for external import). Not a web app. macOS/Linux only. This is a **CLI tool with an embedded configuration bundle**.

Commands: 32 top-level `.md` files. Agents roster: 9. `atomic/internal/` has 18 subdirectories. `doctor/` grew to 35 files (new: `checks_config.go`, `checks_config_test.go` — category #9 in the doctor registry). `docs/spec/` grew to 12 files: added `atomic-state-and-config.md`, `install-output-style.md`. `docs/design/` grew to 4 files: added `atomic-state-and-config.md`. `docs/credits.md` added at `docs/` root. `assets/atomic-claude.png` added (logo/image asset, not bundled).

## Conventions detected

**Test layout**: tests are co-located with implementation (`*_test.go` alongside source, e.g. `signals_test.go` next to `signals.go`). One integration test lives separately at `atomic/test/install_sh_test.go`.

**Source layout**: standard Go flat package layout — no `internal/pkg/` nesting. Each subdomain is one package directly under `internal/`. Exception: `doctor/` (35 files) is significantly larger than all other packages (next largest: `validate/` at 14, `config/` at 8, `signals/` at 6). It stays flat within the package but uses file-level subdivision (`checks_<domain>.go`, `fix.go`, `fix_impls.go`, `format.go`, `exit.go`, `shortcircuit.go`) rather than sub-packages.

**Bundle inclusion rules** (from `atomic/internal/bundlemirror/mirror.go`):

- Agents: `agents/atomic-*.md` (prefix filter, files only)
- Skills: `skills/atomic-*/` full directory subtree (prefix filter on dir name; requires `SKILL.md` present; all files under the dir are bundled, not just `SKILL.md`)
- Output styles: `output-styles/atomic*.md` (prefix filter, files only)
- Commands: every top-level `commands/*.md` — no allowlist; subdirectories (e.g. `commands/_templates/`) are skipped
- Rules: all `rules/**/*.md` (recursive)
- `CLAUDE.md` (bundled directly; no rename step — `bundlemirror` reads `CLAUDE.md` at repo root as-is)

**Naming convention**: all custom Claude Code artifacts use the `atomic-` prefix (`atomic-builder`, `atomic-tdd`, `atomic-prose`, etc.).

**Skill surface split**: two distinct style scopes exist in the skill layer. `output-styles/atomic.md` governs TUI replies to the user (terse, telegraphic). `skills/atomic-prose/SKILL.md` governs prose Claude writes *into* files (README, `docs/design/`, `docs/spec/` prose sections, guides). They do not conflict — they apply to different output surfaces. `atomic-prose` auto-fires on phrases like "draft the README", "write the docs", "improve this prose", "edit the guide", and is also invoked by `/documentation` and `/atomic-plan` (prose sections only).

**Lint config**: no `.golangci.yml` or `golangci-lint` found; CI uses `go vet` + `gofmt -l` only.

**Markdown conventions**: CLAUDE.md and project docs follow atomic output style (double newline after headings, 4-space code blocks) — enforced by `output-styles/atomic.md`.

**`bundle/` directory is tracked in git**: both `atomic/internal/embedded/bundle/` files and `manifest.go` are committed. The pre-commit hook (`.githooks/pre-commit`) regenerates the bundle via `make -C atomic bundle` when staged source artifacts change and then re-stages `atomic/internal/embedded/bundle` and `manifest.go` automatically. CI's "Verify bundle is committed" step (`go generate ./... && git diff --exit-code`) remains the canonical gate. Install via `make hooks` (sets `core.hooksPath=.githooks`); undo via `make hooks-uninstall` (`atomic/Makefile` and root `Makefile` both expose these targets).

## Domains

- **Artifact distribution**: `atomic claude install/update/diff/list` — installs the embedded bundle into `~/.claude`.
- **Project signals**: `atomic signals scan/show/stale/diff` — deterministic project snapshot for Claude context injection.
- **Session hooks**: `atomic hooks session-start/install/uninstall` — injects pending-reminders context at session open.
- **Reminders**: `atomic reminder add/list/show/rm` — file-based reminder CRUD stored in repo root.
- **Self-update**: `atomic update [--check] [--channel]` — GitHub Releases-backed binary update with SHA256 verification and background check on every command invocation.
- **Integrity checks**: `atomic/internal/doctor/` — 9-check health-check suite (`install`, `hooks`, `signals`, `refs`, `manifest`, `followups`, `memory`, `binary`, `config`). Implements `atomic doctor [--fix]`; exit 0 (PASS/WARN/SKIP), 1 (FAIL), 2 (usage error). Full spec at `docs/spec/atomic-doctor.md`. Category 9 (`config`): verifies `config.toml` parses, no unknown keys, `config.resolved.md` matches re-render; repair re-renders `config.resolved.md`.
- **Claude Code config authoring**: the root-level `agents/`, `commands/`, `skills/`, `output-styles/`, `rules/` directories are the authoritative source for the artifact bundle.
- **Prose discipline**: `skills/atomic-prose/` — voice and tone rules for human-readable developer documentation written into files. Distinct from the atomic TUI output style. Invoked by `/documentation`, `/atomic-plan` (design and prose sections), and auto-fires on documentation-editing phrases.
- **Issue filing (atomic repo)**: `commands/report-issue-with-atomic.md` — `/report-issue-with-atomic` targets `damusix/atomic-claude` specifically. Sibling to `/report-issue` (which targets the user's current repo). Entry point for bugs/feature requests about the installed config itself.
- **Branch review**: `commands/review-branch.md` — `/review-branch` dispatches `atomic-reviewer` once on `<base>..HEAD` for a pre-PR / pre-merge review pass; no orchestration loop, no spec required.
- **Commit undo**: `commands/undo-commit.md` — `/undo-commit` soft-resets the last commit; refuses on merge commits, initial commit, and already-pushed HEAD.
- **Trunk-based ship verbs**: `commands/commit-and-push.md` (`/commit-and-push`) and `commands/push-only.md` (`/push-only`) — trunk-based counterparts to `/commit-and-pr` and `/pr-only`. Push directly to the current branch without opening a PR. Intended for trunk workflows where PRs are bypassed.
- **Session reports**: `commands/session-report.md` — `/session-report` writes a timestamped working-memory note to `.claude/.scratchpad/session-reports/<branch>/` capturing what changed and why in the current session. Ship verbs read all reports for the current branch before commit-message synthesis, then delete them after a successful commit. Opt-in only; does not auto-fire. Full spec at `docs/spec/session-report.md`.
- **Workflow routing**: `commands/atomic-help.md` — `/atomic-help` is a routing assistant for disoriented users. Reads git state (branch, ahead/behind, dirty, scratchpad presence, spec presence), classifies user intent (empty args = state-driven recommendation; topic keyword = focused pointer; freeform = one-verb classification), and recommends the single next action. Never executes. Atomic style output only.
- **Design pressure-testing**: `commands/pressure-test.md` — `/pressure-test [<topic> | @<path>]` enters a Socratic challenger session. Questions only; no code, no specs, no agents. Explicit-only (never auto-fires). Pairs with `/atomic-plan` as pre-approval gate. Persists settled decisions in-conversation; no disk writes.
- **Failure investigation**: `commands/subagent-diagnose.md` — `/subagent-diagnose <ci|bug>` is a multi-agent orchestrator for failure-root-cause loops. `ci` mode seeds from a failed GitHub Actions run (`gh run`); `bug` mode seeds from a freeform symptom. Same scratchpad (`BRIEF.md`, `STATE.md`, `FOLLOWUPS.md`, `CONTEXT.md`), investigator + builder/surgeon + reviewer chain, and FOLLOWUPS disposition as `/subagent-implementation`. Hard bail at 5 iterations (user-memory-configurable) or 3 consecutive same-failure iterations. `ci` mode spawns `atomic-haiku` background watcher after fix commit. Full spec at `docs/spec/subagent-diagnose.md`.
- **User config**: `atomic config get|set|unset|list|path` — TOML-backed user config stored at `~/.claude/.atomic/config.toml`. Resolved values rendered to `~/.claude/.atomic/config.resolved.md` and `@`-ref'd from bundled `CLAUDE.md` so every Claude session sees current config without hooks. v1 schema: `output.intensity` (`lite|full|ultra`). `atomic/internal/config/` implements load/validate/render/persist; `atomic/internal/doctor/checks_config.go` enforces integrity. Spec: `docs/spec/atomic-state-and-config.md`.
- **State directory consolidation**: `~/.claude/.atomic/` is now the canonical atomic-owned state root. Paths: `config.toml` (user config), `config.resolved.md` (rendered snapshot), `backups/<ts>/` (install backups), `proposed/CLAUDE.md` (merge target). Supersedes `~/.claude/.atomic-backups/` and `~/.claude/CLAUDE.md.atomic-proposed` (old paths orphaned; no migration).
- **Design docs**: `docs/design/` holds `atomic-doctor.md`, `atomic-validate.md`, `atomic-state-and-config.md`, and `diagnose-orchestrators.md` — design rationale for shipped and planned features.

## Cross-references

- `atomic/internal/bundlemirror/mirror.go` is the sole source of bundle inclusion logic; no `CommandAllowlist` exists. Every top-level `commands/*.md` ships. `commands/_templates/` subdirectory is explicitly skipped (loop skips dirs). Adding a new top-level command file is sufficient to include it in the bundle. `commands/atomic-help.md`, `commands/pressure-test.md`, and `commands/subagent-diagnose.md` all auto-bundle via this rule — no mirror.go change needed. Bundle is now 51 items.
- `agents/atomic-strategist.md` ships in the bundle automatically via the `agents/atomic-*.md` inclusion rule. No bundle config change needed.
- `atomic/internal/doctor/` (35 files) implements `atomic doctor`. Consumed by `atomic/cmd/atomic/main.go`. Shares `manifestcheck` with `atomic/internal/validate/`. The new `checks_config.go` imports `atomic/internal/config` directly — `config` package is a shared dependency between `doctor` and the `atomic config` CLI commands.
- `atomic/internal/config/` (8 files) is consumed by `atomic/cmd/atomic/main.go` (CLI dispatch for `atomic config get|set|unset|list|path`), `atomic/internal/claudeinstall/install.go` (pre-creates `config.resolved.md` on `Apply`), and `atomic/internal/doctor/checks_config.go` (integrity check). The `paths.go` functions (`TOMLPath`, `ResolvedPath`, `BackupDir`, `ProposedCLAUDEMD`) are the canonical source for all `~/.claude/.atomic/` path derivation — all three consumers call these rather than constructing paths independently.
- `skills/atomic-prose/SKILL.md` ships in the bundle automatically: the `skills/atomic-*/` inclusion rule picks up all subdirs matching the `atomic-` prefix that contain a `SKILL.md`. No bundle config change needed when adding a new `atomic-`-prefixed skill directory.
- `atomic/internal/embedded/manifest.go` is generated by `go generate`; editing it by hand is explicitly forbidden (file header: "Code generated by cmd/bundle-mirror. DO NOT EDIT.").
- `.claude/project/deterministic-signals.md` is written by `atomic signals scan`; `inferred-signals.md` (this file) is written by the `atomic-signals-inferrer` agent. Both are `@`-referenced in `claude.local.md` (not `CLAUDE.md` — `CLAUDE.md` is the bundle source and must not carry project-specific paths).
- `.githooks/pre-commit` wires bundle regeneration into the commit flow. Triggers on staged changes to `agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, or `CLAUDE.md`. Re-stages bundle outputs automatically. Installed via `make hooks`; reverted via `make hooks-uninstall` (root `Makefile`, not `atomic/Makefile`).
- `commands/session-report.md` → `atomic-commit` skill: ship verbs read `.claude/.scratchpad/session-reports/<branch>/*.md` in chronological order and pass the concatenated content to `atomic-commit` as supplemental *why* context before message synthesis. Delete the branch's reports dir after a successful commit. Exempt verbs (no message synthesis): `/pr-only`, `/push-only`, `/merge-to-main`. Full integration contract in `docs/spec/session-report.md § Ship-verb integration`.
- `commands/subagent-diagnose.md` reuses `commands/_templates/implementer-prompt.md` and `commands/_templates/reviewer-prompt.md` — same templates as `/subagent-implementation`. The orchestrator halts with an error message if the templates are missing. `docs/spec/subagent-diagnose.md` is the canonical contract.
- `commands/atomic-help.md` consumes git state via Bash (`git`, `gh`) and consults scratchpad presence and `docs/spec/` presence to drive routing decisions. Pure read-only; dispatches nothing.
- `scripts/link-local.sh` — the only shell script besides `install.sh`; likely creates symlinks between root artifacts and `.claude/` for dogfooding. Not read in this run; content unverified.

## Security boundaries

- **Self-update binary replacement**: `selfupdate.Apply()` downloads a release archive, verifies SHA256 against `checksums.txt` from the same release, then atomically replaces the running binary via `os.Rename`. No signature verification beyond SHA256 (no GPG, no Sigstore). The checksums file itself is fetched from the same GitHub Release — a compromised release would provide matching checksums.
- **Artifact install**: `claudeinstall.Apply()` writes files from the embedded FS to `~/.claude`. No signature check on the embedded bundle; trust derives from the binary itself.
- **`CLAUDE.md` merge-required guard**: if `~/.claude/CLAUDE.md` differs from the bundled version, `claudeinstall` writes to `~/.claude/.atomic/proposed/CLAUDE.md` instead of overwriting, preventing silent replacement of user-customized global instructions. Old path (`~/.claude/CLAUDE.md.atomic-proposed`) is no longer written.
- **No network access in signals or hooks**: `signals`, `hooks`, and `reminder` subcommands are local-only. Only `selfupdate` and `claude install/update` make outbound calls.
- **CGO disabled** (`.goreleaser.yaml` `CGO_ENABLED=0`): no native code in the binary; no shared library attack surface.

## Risks / unknowns

- **`scripts/link-local.sh` not read**: purpose and targets unverified. If it symlinks `.claude/` subdirs to root dirs, edits to root artifacts would immediately affect the dogfood config without a `go generate` step — but this creates a divergence between the live config and the committed manifest. Read the script to confirm.
- **SHA256-only self-update integrity**: no GPG or Sigstore signature verification. The checksums file is fetched from the same GitHub Release as the binary, meaning a compromised release token or release account could serve a malicious binary with matching checksums. Acceptable for a personal config tool; worth noting for any broader distribution.
- **`atomic/test/install_sh_test.go` scope**: this test exercises `install.sh` but the file was not read. It is unknown whether it mocks the GitHub API or makes live network calls, which would make it flaky in offline/CI environments without credentials.
- **`release-please-config.json` / `release-please-manifest.json` not read**: release-please versioning strategy (component vs. root) not confirmed. The `.goreleaser.yaml` injects `version.Version` from a single `atomic/internal/version/version.go` — unclear how release-please and goreleaser coordinate the version value.
- **TypeScript file (100 LOC, 1 file)**: the deterministic scan reports 1 TypeScript file but the tree shows no `.ts` files at root level. Location not confirmed; likely inside `atomic/internal/signals/testdata/` (7 total items reported there). Requires `ls` or Read of that subtree to verify.
- **Python file (30 LOC, 1 file)**: same as TypeScript — 1 Python file detected but not located in the visible tree. Likely inside `atomic/internal/signals/testdata/`. Requires verification.
- **`atomic-claude-merger` agent and `/atomic-claude-merge` command**: the spec (`docs/spec/atomic-state-and-config.md` success criterion) states these must reference the new proposed path (`~/.claude/.atomic/proposed/CLAUDE.md`). Whether the agent and command files were updated to match was not verified in this incremental run — only `atomic/internal/claudeinstall/install.go` and `atomic/internal/config/paths.go` were read.

---
generated_at: 2026-05-17T08:40:00Z
source: .claude/project/deterministic-signals.md
---

# Inferred signals

## Framework / runtime

- **Go 1.23**, single module at `atomic/go.mod` (`module github.com/damusix/atomic-claude/atomic`). Runtime target: CLI binary named `atomic`.
- **External Go dependencies**: `gopkg.in/yaml.v3 v3.0.1` (YAML frontmatter parsing, used in `atomic/internal/frontmatter/`), `github.com/tailscale/hujson` (lenient JSON for hooks config, `atomic/internal/hooks/hooks_hujson.go`).
- **Go embed** (`//go:embed bundle`, `atomic/internal/embedded/bundle.go`): artifact bundle is embedded at build time via `go:generate`.
- **goreleaser** (`.goreleaser.yaml`): multi-platform release pipeline producing `linux/darwin/windows × amd64/arm64` binaries (Windows arm64 excluded). CGO disabled. Version injected via ldflags from `internal/version`.
- **release-please** (`.github/workflows/release-please.yml`, `release-please-config.json`, `release-please-manifest.json`): automated changelog and tag generation on main.
- No web framework. No database. No runtime dependencies outside the Go standard library and the two listed third-party modules.

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

**Dual-product repo**: a Go CLI binary (`atomic/`) co-located with the Claude Code configuration artifacts it manages (`agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, `claude.md`).

The repo has two logical layers:

1. **Source-of-truth artifacts** (root: `agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, `claude.md`) — human-authored markdown files that are both the live dogfood config for this repo and the bundle input.
2. **Go CLI** (`atomic/`) — reads those artifacts, embeds them at build time, and provides subcommands to install/update them into `~/.claude`, scan project signals, manage reminders, manage hooks, and self-update.

Internal package layout inside `atomic/internal/`:

- `bundlemirror/` — build-time mirror: walks repo root, copies matching artifacts into `embedded/bundle/`, writes `manifest.go`
- `embedded/` — holds the `go:embed`-ed `bundle/` FS and the generated `Manifest()` slice; bundle grew from 30 to 36 total items as skills now ship as full subtrees and all top-level commands ship
- `claudeinstall/` — install/update/diff/list verbs; SHA256-based idempotency; backs up changed files under `.atomic-backups/<timestamp>/`; special-cases `CLAUDE.md` as merge-required
- `signals/` — tree walker, manifest scanner, language counter; writes `.claude/project/deterministic-signals.md`; keeps `.deterministic-signals.prev.md` for diff; uses `git diff` when in a git repo, falls back to `diff(1)`
- `hooks/` — session-start hook payload generation and install/uninstall
- `reminder/` — file-based reminder CRUD
- `selfupdate/` — GitHub Releases API lookup, SHA256-verified binary download, atomic replace via `os.Rename` with cross-filesystem fallback
- `repoctx/`, `frontmatter/`, `ids/`, `version/` — support utilities

Not a monorepo (single go.mod). Not a library (no exported packages intended for external import). Not a web app. This is a **CLI tool with an embedded configuration bundle**.

Commands grew from 20 to 21 entries with the addition of `commands/refresh-signals.md`.

## Conventions detected

**Test layout**: tests are co-located with implementation (`*_test.go` alongside source, e.g. `signals_test.go` next to `signals.go`). One integration test lives separately at `atomic/test/install_sh_test.go`.

**Source layout**: standard Go flat package layout — no `internal/pkg/` nesting. Each subdomain is one package directly under `internal/`.

**Bundle inclusion rules** (from `atomic/internal/bundlemirror/mirror.go`):

- Agents: `agents/atomic-*.md` (prefix filter, files only)
- Skills: `skills/atomic-*/` full directory subtree (prefix filter on dir name; requires `SKILL.md` present; all files under the dir are bundled, not just `SKILL.md`)
- Output styles: `output-styles/atomic*.md` (prefix filter, files only)
- Commands: every top-level `commands/*.md` — no allowlist; subdirectories (e.g. `commands/_templates/`) are skipped
- Rules: all `rules/**/*.md` (recursive)
- `claude.md` → `CLAUDE.md`

**Naming convention**: all custom Claude Code artifacts use the `atomic-` prefix (`atomic-builder`, `atomic-tdd`, etc.).

**Lint config**: no `.golangci.yml` or `golangci-lint` found; CI uses `go vet` + `gofmt -l` only.

**Markdown conventions**: CLAUDE.md and project docs follow atomic output style (double newline after headings, 4-space code blocks) — enforced by `output-styles/atomic.md`.

**`bundle/` directory is gitignored**: it is generated at build time by `go generate ./...` (`cmd/bundle-mirror`). The manifest snapshot (`manifest.go`) is committed; the actual file copies are not. CI verifies the committed manifest matches what `go generate` would produce.

## Domains

- **Artifact distribution**: `atomic claude install/update/diff/list` — installs the embedded bundle into `~/.claude`.
- **Project signals**: `atomic signals scan/show/stale/diff` — deterministic project snapshot for Claude context injection.
- **Session hooks**: `atomic hooks session-start/install/uninstall` — injects pending-reminders context at session open.
- **Reminders**: `atomic reminder add/list/show/rm` — file-based reminder CRUD stored in repo root.
- **Self-update**: `atomic update [--check] [--channel]` — GitHub Releases-backed binary update with SHA256 verification and background check on every command invocation.
- **Claude Code config authoring**: the root-level `agents/`, `commands/`, `skills/`, `output-styles/`, `rules/` directories are the authoritative source for the artifact bundle.

## Cross-references

- `atomic/internal/bundlemirror/mirror.go` is the sole source of bundle inclusion logic; no `CommandAllowlist` exists. Every top-level `commands/*.md` ships. `commands/_templates/` subdirectory is explicitly skipped (loop skips dirs). Adding a new top-level command file is sufficient to include it in the bundle.
- `atomic/internal/embedded/manifest.go` is generated by `go generate`; editing it by hand is explicitly forbidden (file header: "Code generated by cmd/bundle-mirror. DO NOT EDIT.").
- `.claude/project/deterministic-signals.md` is written by `atomic signals scan`; `inferred-signals.md` (this file) is written by the `atomic-signals-inferrer` agent. Both are referenced by the repo's `claude.md` via `@` imports.
- `scripts/link-local.sh` — the only shell script besides `install.sh`; likely creates symlinks between root artifacts and `.claude/` for dogfooding. Not read in this run; content unverified.

## Security boundaries

- **Self-update binary replacement**: `selfupdate.Apply()` downloads a release archive, verifies SHA256 against `checksums.txt` from the same release, then atomically replaces the running binary via `os.Rename`. No signature verification beyond SHA256 (no GPG, no Sigstore). The checksums file itself is fetched from the same GitHub Release — a compromised release would provide matching checksums.
- **Artifact install**: `claudeinstall.Apply()` writes files from the embedded FS to `~/.claude`. No signature check on the embedded bundle; trust derives from the binary itself.
- **`CLAUDE.md` merge-required guard**: if `~/.claude/CLAUDE.md` differs from the bundled version, `claudeinstall` writes a `.atomic-proposed` file instead of overwriting, preventing silent replacement of user-customized global instructions.
- **No network access in signals or hooks**: `signals`, `hooks`, and `reminder` subcommands are local-only. Only `selfupdate` and `claude install/update` make outbound calls.
- **CGO disabled** (`.goreleaser.yaml` `CGO_ENABLED=0`): no native code in the binary; no shared library attack surface.

## Risks / unknowns

- **`scripts/link-local.sh` not read**: purpose and targets unverified. If it symlinks `.claude/` subdirs to root dirs, edits to root artifacts would immediately affect the dogfood config without a `go generate` step — but this creates a divergence between the live config and the committed manifest. Read the script to confirm.
- **SHA256-only self-update integrity**: no GPG or Sigstore signature verification. The checksums file is fetched from the same GitHub Release as the binary, meaning a compromised release token or release account could serve a malicious binary with matching checksums. Acceptable for a personal config tool; worth noting for any broader distribution.
- **`atomic/test/install_sh_test.go` scope**: this test exercises `install.sh` but the file was not read. It is unknown whether it mocks the GitHub API or makes live network calls, which would make it flaky in offline/CI environments without credentials.
- **`release-please-config.json` / `release-please-manifest.json` not read**: release-please versioning strategy (component vs. root) not confirmed. The `.goreleaser.yaml` injects `version.Version` from a single `atomic/internal/version/version.go` — unclear how release-please and goreleaser coordinate the version value.
- **TypeScript file (100 LOC, 1 file)**: the deterministic scan reports 1 TypeScript file but the tree shows no `.ts` files at root level. Location not confirmed; likely inside `atomic/internal/signals/testdata/` (7 total items reported there). Requires `ls` or Read of that subtree to verify.
- **Python file (30 LOC, 1 file)**: same as TypeScript — 1 Python file detected but not located in the visible tree. Likely inside `atomic/internal/signals/testdata/`. Requires verification.

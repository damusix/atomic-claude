---
generated_at: 2026-05-17T07:20:10Z
source: .claude/project/deterministic-signals.md
---

# Inferred signals

## Framework / runtime

- Go 1.23 — `atomic/go.mod`: `go 1.23`, module `github.com/damusix/atomic-claude/atomic`
- `gopkg.in/yaml.v3` — YAML parsing; `atomic/go.mod` `require` block
- `github.com/tailscale/hujson` — HJson (relaxed JSON) parsing; `atomic/go.mod` `require` block, used in `atomic/internal/hooks/hooks_hujson.go`
- No Node.js / npm runtime detected — `TypeScript: 100 LOC` in `Languages` section but no `package.json` manifest cited; likely type-rule files only (`rules/typescript/style.md`)
- Shell: `install.sh` + `scripts/link-local.sh` — installation and local-link helpers

## Build / test / lint commands

- Build: inferred `go build ./...` — standard Go module layout under `atomic/`; `atomic/Makefile` present but not read (not cited as a manifest)
- Test: inferred `go test ./...` — test files present at `atomic/cmd/atomic/main_test.go`, `atomic/cmd/bundle-mirror/main_test.go`, `atomic/internal/*/`*`_test.go`, `atomic/test/install_sh_test.go`
- No `package.json` scripts, no `pyproject.toml` scripts — no Node or Python build commands available
- `atomic/Makefile` — likely defines canonical build/test/lint targets; not read (not cited in `Manifests` section); consult before assuming raw `go` invocations

## Architectural style

- Hybrid repo: Claude Code configuration artifact store (Markdown-first) + a compiled Go CLI binary (`atomic/`)
- Go CLI is structured as a multi-command binary: `atomic/cmd/atomic/` (main CLI) + `atomic/cmd/bundle-mirror/` (bundle tool), consistent with `cobra`-style or manual sub-command dispatch
- Internal packages under `atomic/internal/` follow Go's encapsulation convention: `signals/`, `bundlemirror/`, `claudeinstall/`, `frontmatter/`, `hooks/`, `selfupdate/`, `reminder/`, `repoctx/`, `ids/`, `version/`
- Configuration artifacts (`agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`) are flat Markdown files — no build step, consumed directly by Claude Code
- `atomic/internal/embedded/bundle/` embeds artifact bundle at compile time (evident from `bundle.go` + `manifest.go` alongside a `bundle/` subdir with 30 total items)
- CI/CD: `.github/workflows/` contains `ci.yml`, `release-please.yml`, `release.yml` — automated release pipeline via Release Please

## Conventions detected

- Test layout: co-located `_test.go` files next to production code throughout `atomic/internal/`; integration test at `atomic/test/install_sh_test.go` (separate `test/` dir for shell-level tests)
- Source layout: `atomic/cmd/<binary>/` for entry points; `atomic/internal/<pkg>/` for private packages; standard Go module conventions
- Spec-driven development: `docs/spec/` holds implementation contracts (`signals-workflow.md`, `atomic-binary.md`, `install-workflow.md`, `cron-workflow.md`, `signals-project-detection.md`)
- Lint config: no explicit linter config file visible in `Tree`; Go toolchain default linting assumed
- Skill/agent/command artifact naming: `atomic-` prefix throughout (`atomic-builder.md`, `atomic-commit/SKILL.md`, etc.)
- Rules scoped by language: `rules/python/style.md`, `rules/typescript/style.md` — path-scoped Claude instructions
- Changelog managed by Release Please: `atomic/CHANGELOG.md` + `release-please-config.json` + `release-please-manifest.json`

## Risks / unknowns

- `atomic/Makefile` not cited as a manifest and not read — actual build/test/lint targets unknown; inferred commands may differ from canonical ones
- `TypeScript: 100 LOC` in `Languages` with no `package.json` in `Manifests` — TypeScript source is likely just `rules/typescript/style.md` prose, but cannot confirm without reading it; no TS toolchain is configured
- `Python: 30 LOC` similarly has no `pyproject.toml` — likely `rules/python/style.md` prose only, but unconfirmed
- `atomic/internal/embedded/bundle/` has 30 total items but only counted as `6 subitems` in the collapsed tree — embedded artifact contents not enumerable from the tree alone
- No `.goreleaser.yaml` content read — release build matrix, CGO flags, binary targets, and archive formats unknown
- `atomic/internal/selfupdate/` package exists but update mechanism (GitHub Releases? custom endpoint?) not verifiable from signals alone
- `.claude/settings.local.json` listed in tree but not read — may contain tool permissions or MCP server config that affects local developer workflow

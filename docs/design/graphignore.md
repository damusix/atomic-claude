# Graphignore: repo-scoped ignore globs for the code-intel index

## Problem


Vendored and minified files enter the symbol graph and dominate it. In this repo, `atomic/internal/serve/assets/vendor/` contributes ~3.8k symbols (mermaid.min.js ~3,510, cytoscape ~317) — noise in the code graph view, `atomic code search`, and `explore` digests. These files are committed (not gitignored), so the `git ls-files` discovery path correctly picks them up; there is currently no way to tell the indexer "this is code, but not *our* code."

The fix must be repo-scoped and committed: every contributor and every fresh worktree index should exclude the same files without per-user setup.

## Goals / Non-goals


Goals:

- A committed, repo-scoped config file that lists ignore globs for the code-intel indexer.
- Ignored files never enter the index at discovery time (not filtered at query time).
- Files already indexed that become ignored are pruned on the next `index`/`sync`.
- Zero behavior change when the config file is absent.
- The feature is discoverable: agents can self-configure it, `/atomic-help` and reference docs name it, doctor validates it.

Non-goals:

- Negation patterns (`!pattern`) — v1 is exclude-only.
- A per-user (uncommitted) ignore overlay.
- CLI flags to pass ignore globs ad hoc.
- Ignore support for the wiki/signals scan (`atomic signals scan` has its own scope rules).
- A serve UI toggle to show/hide ignored files.
- Windows path semantics (repo targets macOS/Linux).

## Approaches


| # | Approach | Pros | Cons |
|---|----------|------|------|
| A | `.claude/atomic.toml` with `[code] ignore = [globs]` | Committed + repo-scoped; TOML gives a natural home for future repo-level keys; mirrors the existing user-scoped `config.toml` loader shape | New config surface to validate (doctor) |
| B | `.atomicignore` gitignore-style file at repo root | Familiar format | Another dotfile at root; gitignore semantics (negation, dir-slash) are a large contract to honor; no room for non-ignore keys later |
| C | Hardcoded heuristics (`*.min.js`, `vendor/` always skipped) | Zero config | Wrong for repos that *want* vendor indexed; invisible magic; unfixable per-repo |
| D | Index everything, filter at query/serve time | No indexer change | DB stays bloated (extraction cost, disk, resolution passes over noise); every query surface needs the filter |

## Recommendation


**A.** The user explicitly asked for "a graphignore in an atomic config toml," and it composes best: discovery-time filtering means `pruneDeleted` — which already builds its membership set from the same `scanFiles` list (`atomic/internal/codeintel/indexer/orchestrator.go:288`) — removes newly-ignored files for free, with no second prune mechanism. TOML parsing reuses the `pelletier/go-toml/v2` dependency and the lenient unknown-key-warning pattern already established in `atomic/internal/config/config.go`.

Glob matching uses `github.com/bmatcuk/doublestar/v4` (v4.10.0 verified on the module proxy; MIT, zero transitive deps). The stdlib `path.Match` has no `**` support, and hand-rolling correct `**` semantics is more than a few lines; doublestar is the community-standard matcher.

## Matching semantics


Patterns are slash-separated and matched against repo-relative paths:

- Pattern **containing `/`** → full-path doublestar match (`atomic/internal/serve/assets/vendor/**`).
- Pattern **without `/`** → basename match at any depth (`*.min.js` matches `a/b/c.min.js`), mirroring the gitignore mental model.
- Leading `./` is stripped from patterns before matching.
- No negation, no trailing-slash dir-only form — a directory is excluded with `dir/**`.

Failure behavior is lenient, matching the user-config precedent: missing file → no filtering; unknown keys → warning; malformed TOML or invalid pattern → indexing proceeds **unfiltered** with a CLI warning, and doctor reports the problem.

## Where it applies


All discovery funnels through `scanFiles` (used by `IndexAll`, `Sync`, and the exported `ScanFiles` that `engine.ExtractFrameworkNodes` calls), so filtering there covers the CLI verbs, the MCP daemon's sync loop, and framework extraction consistently. `IndexPaths` (explicit subset, used by `engine.IndexFiles`) is filtered separately since it bypasses `scanFiles`. In a wiki realm, each member repo's engine roots at that member, so each member reads its own `.claude/atomic.toml` — no realm-level aggregation needed.

## Open questions


None.

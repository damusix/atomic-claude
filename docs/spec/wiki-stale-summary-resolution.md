# Spec: wiki stale summary repo resolution


Fixes [#157](https://github.com/damusix/atomic-claude/issues/157). `atomic wiki stale`
summary mode reports a summary stale even when its `reflects_rev` equals the member
repo's HEAD, because it reconstructs the member's directory from the summary file's
own location on disk instead of resolving it from the scan block.


## Problem


`Stale` derives the member directory like this:

    rel, err := filepath.Rel(reposDir, fp)
    repoName := strings.SplitN(rel, string(filepath.Separator), 2)[0]
    repoDir := filepath.Join(root, repoName)

Two independent defects live in those two lines.

1. **`.md` is never stripped.** A flat summary `repos/<name>.md` has no path separator,
   so `SplitN` returns the whole basename. `repoDir` becomes `<root>/<name>.md` — a
   file, not a git worktree.

2. **The member's parent path is discarded.** Only the first path component under
   `repos/` survives, so a member registered at `packages/gamma` resolves to
   `<root>/gamma`, which does not exist.

`gitRevParseHead(repoDir)` then fails on the missing path and the fail-safe branch
emits `STALE summary` before the `reflects_rev` comparison is ever reached. The
comparison is dead code for every shape except one.


## Shape matrix


Two variables decide whether resolution succeeds: where the member sits relative to
the realm root, and how its summary is laid out.

| Member path | Summary | Shape | Before | After |
|---|---|---|---|---|
| `alpha` | `repos/alpha.md` | root + flat | STALE (false) | fresh |
| `beta` | `repos/beta/*.md` | root + domain-split | fresh | fresh |
| `packages/gamma` | `repos/gamma/*.md` | nested + domain-split | STALE (false) | fresh |
| `packages/delta` | `repos/delta.md` | nested + flat | STALE (false) | fresh |

Only root + domain-split passes today. It is the control: the fix must keep it green
while turning the other three green.


## Approach


Resolve each summary file back to its owning `Member` and use `Member.Path` — the
value `wiki scan` recorded and the only authoritative statement of where the repo
lives. Never infer a filesystem path from an artifact's location.

`classified` is already in scope in `Stale`, and `Member` already carries both
`Path` and `SummaryPath` (the `summary=` attribute, `repos/<name>.md` or
`repos/<name>/`). No new parsing, no new struct field, no signature change.

Resolution order for a summary file at wiki-relative path `p`:

1. **Claimed match.** A member whose `SummaryPath` equals `p` (flat form) or whose
   `SummaryPath` is the directory prefix of `p` (split form). This is the
   authoritative path and matches what `scan` wrote.

2. **Base-name fallback.** No member claims `p`, but exactly one member's
   `filepath.Base(Path)` equals the summary's stem — `<name>` from `repos/<name>.md`
   or `repos/<name>/<domain>.md`. This is the exact inverse of `discoverSummary`,
   which names summaries by `filepath.Base(rel)`.

   The fallback exists because `classifyMembers` rule 2 outranks rule 3: a member
   that has graduated to `indexed` keeps an empty `SummaryPath` even when a summary
   file is still on disk. The classifier comments state that a leftover summary must
   not demote a graduated repo; without this fallback such a summary would resolve to
   nothing and report a false STALE — the same class of bug this spec removes.

3. **Unresolved → `STALE summary`.** No member claims it and the base name matches
   zero or more than one member. Preserves today's fail-safe posture: an orphaned or
   ambiguous summary is reported, never silently passed.

Once resolved, `repoDir = filepath.Join(root, member.Path)` and the existing
`reflects_rev` comparison runs unchanged.


## Out of scope


**Base-name collisions.** `discoverSummary` names summaries by member base name, so
two members at `a/tool` and `b/tool` both map to `repos/tool.md`. That ambiguity is a
property of the naming convention, not of this fix. Step 3 handles it deterministically
(report stale rather than guess); resolving the convention itself is separate work.

**Concern and bucket modes.** Untouched. Bucket staleness resolves paths from the
registered `<bucket path="...">` attribute and has no equivalent failure mode; concern
mode compares recorded fingerprints, not reconstructed paths.


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| C1 | Resolver helper | `atomic/internal/wiki/stale.go`, `atomic/internal/wiki/stale_internal_test.go` | atomic-implementer (mode: surgical) | 2 | A function maps a wiki-relative summary path plus `[]Member` to a member path, implementing the three-step order above. Unit tests cover flat claimed, split claimed, nested member claimed, base-name fallback for an `indexed` member with a leftover summary, zero-match unresolved, and two-member ambiguity unresolved. |
| C2 | `Stale` uses the resolver | `atomic/internal/wiki/stale.go` | atomic-implementer (mode: surgical) | 1 | The `SplitN`/`filepath.Join(root, repoName)` derivation is gone; `repoDir` comes from the resolved member path. Unresolved summaries still emit `STALE summary`. |
| C3 | Shape-matrix regression tests | `atomic/internal/wiki/stale_test.go` | atomic-implementer (mode: surgical) | 1 | A test builds a realm with all four rows of the shape matrix, stamps every summary to its member HEAD, and asserts `Stale` returns fresh with no `STALE summary` lines. A companion test moves one member's HEAD and asserts only that member's summary is reported. |
| C4 | Fixture parity | `~/projects/sample-realm` (external fixture, not in this repo) | orchestrator | 0 | `atomic wiki stale` against the seeded sample realm exits 0 with no output, and `scripts/check-157.sh` there exits 0. |


## Verification


- `go test ./internal/wiki/` green, including the new shape-matrix cases.
- Full `go test ./...` green — no regression in `serve`, which consumes wiki exports.
- `scripts/check-157.sh` in `~/projects/sample-realm` exits 0.
- The three previously-failing live realms (`spt`, `alonso-network`, `hapi`) report no
  false `STALE summary` lines.


## Change log


- 2026-07-25 — Written. Fixes #157: summary mode resolved the member directory from
  the summary file's path, hitting two defects (unstripped `.md`, discarded parent
  path) that made the `reflects_rev` comparison unreachable for three of four shapes.

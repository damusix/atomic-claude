# atomic where — position orientation verb

## Goal

A new `atomic where [--json]` top-level verb reports, in one call, a cwd's
position across three independent axes: repo-scope wiki presence
(`docs/wiki/index.md` found by walking up to the nearest `.git`), realm-scope
position (none / root / member / orphaned, relative to any `<wikis>`-registered
realm), and code-index scope (reusing `codeintel/realm.Resolve` unmodified).
The report composes all three side by side rather than collapsing them into
one enum, so the composite case — a realm member that also carries its own
repo-scope wiki — is visible.

Agents get a documented convention to run `atomic where` for orientation
before wiki/realm-scoped work, and the session-start hook surfaces one
interesting-only nudge line when the position is non-trivial.

## Non-goals

- No refactor of the `isUnder` duplication across `wiki` and `codeintel/realm`.
  A third private copy inside the new package is acceptable.
- No caching or daemon layer — detection is a handful of stat calls per
  invocation.
- No change to the `<wikis>` registry format or to `codeintel/realm`'s
  existing `Scope` semantics — both are consumed as-is.
- No interactive prompts, no write operations — `atomic where` is a read-only
  reporting verb.
- No watch/live mode.
- No PASS/WARN/FAIL severity — the report is descriptive, not a health check
  (contrast with `atomic doctor`).
- No `docs/reference/*.md` update as a checkpoint — the next `/documentation`
  maintenance pass picks up the new verb on a later commit that touches it.
- No dedicated `atomic where` paragraph in `CLAUDE.md`'s `## Atomic binary
  subcommands` block — that treatment is reserved for surfaces on the scale
  of `atomic serve` (a whole new UI); `atomic where` is discoverable the same
  way `atomic doctor` is, via `atomic --help` and the `/atomic-help` `cli` topic.

## Success criteria

- [ ] `atomic where` run from a plain repo with no `docs/wiki/`, no realm
      registration, and no code index reports all three axes as absent/none.
- [ ] `atomic where` run from a repo with `docs/wiki/index.md` present (at
      cwd or an ancestor, stopping at the first `.git` boundary) reports
      repo-scope wiki found, with the resolved path.
- [ ] `atomic where` run from a directory that is a realm root (per the
      `<wikis>` registry) reports realm scope = root; run from inside a
      registered member's subtree reports realm scope = member; run from
      inside a realm root but outside any registered member reports realm
      scope = orphaned.
- [ ] `atomic where` run from a directory that is simultaneously a realm
      member and carries its own `docs/wiki/index.md` reports both facts
      together — composite state is visible in one invocation, not hidden by
      collapsing to a single enum.
- [ ] Code-index scope in the report reflects `codeintel/realm.Resolve`'s
      result unmodified, and appears unconditionally as one of the report's
      top-level fields (not gated behind a flag).
- [ ] `atomic where --json` emits the same information as machine-readable
      JSON.
- [ ] The upward walk for repo-scope detection performs no `git` subprocess
      spawns — pure filesystem stats, matching the zero-git-spawn contract
      already established in `wiki/staleness.go`.
- [ ] `atomic where` is registered in the root Cobra command, has a
      `cliusage.go` entry, and is covered by `TestDeriveCommandsGolden` (and
      the Cobra-metadata cross-check, if the verb's args/flags are asserted
      there) — both pass.
- [ ] The session-start hook appends one orientation nudge line when the
      resolved position is not the plain no-wiki/no-realm case, and appends
      nothing when it is — matching the hook's existing silent-unless-relevant
      behavior (see `wikiNudges` / `buildBody` precedent).
- [ ] A shared agent-prompt partial documents the convention: agents run
      `atomic where` for orientation before wiki/realm-scoped work, with
      graceful degradation when the binary is absent — mirroring
      `agent-code-intel`'s existing "lead with `atomic code explore`"
      convention. Composed into the same agents that already receive
      `agent-code-intel` (or a documented subset, if scope differs).
- [ ] `templates/commands/atomic-help.md`'s `binary`/`cli` topic row mentions
      `atomic where [--json]`.
- [ ] `templates/commands/atomic-help.md`'s Stage 4 tour block (the maintenance
      code fence) lists `atomic where [--json]` alongside the other read-only
      orientation/maintenance verbs already there (`atomic doctor`, `atomic
      code explore`, `atomic wiki stale`, `atomic profile refresh`), matching
      their existing per-verb line shape (verb + one-line description).
- [ ] The MISSING-scan verification
      (`for cmd in commands/*.md; do verb=$(basename "$cmd" .md); [ "$verb" = "atomic-help" ] && continue; grep -q "/$verb" templates/commands/atomic-help.md || echo "MISSING: /$verb"; done`)
      returns zero lines — `atomic where` is a binary verb, not a slash
      command, so it is exempt from this scan by construction, but the scan
      must still pass after this work lands.
- [ ] `go test ./...`, `go vet ./...`, `gofmt -l .` clean under `atomic/`.
- [ ] `make render && git diff --exit-code` clean; `make -C atomic bundle && git diff --exit-code` clean.

## Approach

New `internal/where` package composing three existing/new detectors behind
one `atomic where` verb — see
[docs/design/atomic-where.md](../design/atomic-where.md) § Recommendation
(Approach A).

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | `internal/where` package (new git-boundary upward walk for repo-scope wiki detection, reuse of `wiki.ReadWikiIndexPaths` for realm roots + `wiki.ReadScanMembers` for realm member paths — the wiki's own `<wiki-scan>` registry, distinct from `codeintel/realm`'s separate `code.toml`, which code-index scope reads via `realm.Resolve` unmodified — plus a local `isUnder` copy, for realm-scope root/member/orphaned/none, reuse of `codeintel/realm.Resolve` unmodified for code-index scope, one composed report type) + `atomic where [--json]` CLI verb, Cobra registration mirroring `buildDoctorCmd()` (`atomic/cmd/atomic/main.go:760-779`), `cliusage.go` entry (top-level single-token `Path`, pattern at `atomic/internal/cliusage/cliusage.go:110` `["doctor"]` / `:380` `["serve"]`), `main_test.go` golden-fixture wiring (`atomic/cmd/atomic/main_test.go:149-171` `TestDeriveCommandsGolden`, `:80-124` `cp3WantMeta`), package tests | new `atomic/internal/where/` (detector + tests), `atomic/cmd/atomic/main.go` (`buildWhereCmd()` + registration alongside `main.go:186-218`), `atomic/internal/cliusage/cliusage.go`, `atomic/cmd/atomic/main_test.go` | atomic-implementer (feature) | 6 | All repo-scope/realm-scope/composite/code-index/`--json` success criteria; zero-git-spawn constraint; `go test ./...`, `go vet`, `gofmt -l` clean; `TestDeriveCommandsGolden` passes |
| 2 | Session-start hook integration: one new orientation-nudge source, interesting-only suppression | `atomic/internal/hooks/hooks.go` (`buildBody`, `:100-160` `SessionStart`, `:165-175` `SessionStartText` — new nudge source alongside `checkWikiStaleness` at `:62-75`), new/extended hook test coverage | atomic-implementer (surgical) | 2-3 | Nudge appears only for non-trivial position, matching `wikiNudges` precedent; existing hook tests still pass |
| 3 | Agent-prompt convention (shared partial telling agents to run `atomic where` for orientation, mirroring `templates/shared/agent-code-intel.md`) + `/atomic-help` `binary`/`cli` topic-row mention + render/bundle regen | `templates/shared/agent-code-intel.md` or a new `templates/shared/agent-where.md` partial + the agent templates composing it (grep `agent-code-intel` across `templates/agents/` and `templates/shared/` for the full current composition set — includes at least `templates/agents/atomic-reviewer.md:160`, `atomic-investigator.md:21`, `atomic-wiki-inferrer.md:77`, and `atomic-implementer.md` indirectly via `templates/shared/agent-implementer-workflow.md:14`), `templates/commands/atomic-help.md` (`binary`/`cli` topic row, ~line 125, and Stage 4 tour block, ~line 211-231), rendered `commands/atomic-help.md` + `agents/*.md` (via `make render`), `atomic/internal/embedded/bundle/**` (via `make -C atomic bundle`) | atomic-implementer (feature) | 5-6 | Agent partial success criterion; topic-row success criterion; Stage 4 tour-line success criterion; MISSING-scan zero lines; `make render` + `make -C atomic bundle` parity clean |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| `cliusage.go`'s static table and the live Cobra tree diverge, breaking `TestDeriveCommandsGolden` | High (fires if either is edited alone) | CP1 updates `cliusage.go`, `buildWhereCmd()`, and `main_test.go`'s golden fixture in the same commit; run `go test ./...` before proceeding |
| Upward walk false-positives against an unrelated ancestor's `docs/wiki/` when cwd is deeply nested with no `.git` in reach (e.g. run outside any repo) | Med | Stop the walk at the first `.git` found, or at filesystem root if none — same boundary rule the design doc specifies |
| Third `isUnder` copy invites a future "just consolidate them" temptation mid-implementation | Low | Non-goals explicitly permits the third copy; reviewer should not flag it as a defect |
| Session-start hook nudge line becomes noisy if "interesting" is defined too broadly (e.g. fires for every repo-scope-wiki-only cwd, which is the common case) | Med | CP2 tests both the suppressed case (plain no-wiki/no-realm) and at least one surfaced case explicitly |
| `agent-where` guidance duplicates rather than complements `agent-code-intel`'s degradation language, producing redundant partials | Low | CP3 checks whether extending `agent-code-intel` (single partial, two concerns) reads more cohesively than a new sibling partial before committing to either |

## Change log

<!-- empty on creation; first entry on first post-approval amendment -->

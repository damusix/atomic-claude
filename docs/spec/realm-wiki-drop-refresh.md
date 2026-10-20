# Realm wiki: drop the refresh requirement

## Approach

A realm wiki no longer needs refreshing. Every mechanism whose only job was to force or track a refresh is deleted, along with bucket synthesis into `wiki/knowledge/`. Decisions are recorded in GitHub issue damusix/atomic-claude#301; this spec is the build contract.

## Goal

- Nothing nudges, marks, or fingerprints a realm wiki.
- Bucket synthesis is gone. Buckets keep `add`, `list`, `index`, `doc`, and `skill`; per-folder indexes are their only derived output.
- `repos/` summaries and `concerns/` stay, with nothing tracking their freshness. An explicit `/refresh-wiki` on a realm writes summaries and re-authors existing concerns; new concerns are written by hand.
- `atomic wiki stale` reports membership drift only.

## Non-goals

- No migration or cleanup of leftover state on existing realms (`wiki/.dirty`, `wiki/.buckets/`, `wiki/knowledge/`, `reflects_*` frontmatter, the `.dirty` line in `wiki/.gitignore`). They become inert files nothing reads.
- No change to the `<wikis>` registry or to `atomic where`.
- No change to repo-scope signals (`atomic signals stale`, `docs/wiki/`) or to the realm wiki-output pipeline (W1-W7) beyond removing its stamp step.
- No change to `atomic wiki scan`'s `<wiki-scan generated="...">` attribute; it stays as a record of the last scan.
- No new verb, flag, or config key.
- `docs/design/*.md` files are left as they are (design workspace, not a contract). `docs/wiki/*.md` is pipeline output and is refreshed by the signals step, not hand-edited.

## Success criteria

1. `atomic wiki mark-dirty`, `atomic wiki stamp`, `atomic wiki bucket diff`, and `atomic wiki bucket promote` are unknown verbs: exit 1 with `unknown verb`, and none appears in `atomic wiki --help`, `atomic wiki bucket --help`, or `cliusage`.
2. The session-start hook emits no `## Wiki staleness` section under any condition; `wiki.CheckStaleness` and `wiki.MarkDirty` no longer exist. `wiki.ReadWikiIndexPaths` stays (used by `where` and `codeintel/realm`).
3. `atomic wiki stale` emits only `DRIFT added|removed|status` lines. It reads no frontmatter, hashes no files, and runs no git. Exit codes keep their meaning: 0 no drift, 1 drift, 2 hard error.
4. `atomic wiki bucket add <name>` creates no `wiki/.buckets/` directory. It still validates the name and refuses a double register, now by checking the `<wiki-buckets>` registry in `wiki/index.md`.
5. `atomic wiki bucket list` prints one `<name>\t<path>` line per registered bucket, exit 0, with no file counts and no pending/fresh status.
6. `atomic wiki scan` writes no `wiki/.gitignore`; a fresh scan scaffolds `index.md`, `README.md`, `CLAUDE.md`, `repos/`, `concerns/`.
7. The bucket manifest engine (`WalkBucket`, `BucketDiff`, `BucketDiffReadOnly`, `PromoteBucket`, manifest read/parse/diff helpers), the stamp engine (`StampSummary`, `StampConcern`, `StampKnowledge`, `resolveFingerprint`), and the exports wrapping them (`FileSHA256`, `ResolveFingerprint`) are deleted, with their tests.
8. `atomic serve` no longer builds a provenance DAG, injects provenance edges into the graph, lists a Knowledge nav group, classifies a `knowledge` node type, or badges buckets as stale. `/api/status` reports wiki membership drift only (`staleRepos`, `allFresh`); `staleConcerns`, `staleBuckets`, and `bucketDiffKeys` are gone from the Go type, the JSON, and the frontend.
9. No shipped artifact under `context/` calls `atomic wiki mark-dirty`, `atomic wiki stamp`, `bucket diff`, or `bucket promote`, names `wiki/knowledge/`, `wiki/.buckets/`, `reflects_rev`, `reflects:`, the neglect nudge, or the `.dirty` marker, or dispatches the inferrer in bucket-synthesis mode.
10. `/refresh-wiki` on a realm: scans, writes summaries for pending repos, then offers a numbered list of existing summaries and concerns and re-authors only the ones the user picks (`all` and `none` accepted). It ends with a closing `atomic wiki scan` (so `index.md` records the repos the run summarized or Step 6 promoted), linkify, the disposition report, and the automatic wiki commit; there is no `.dirty` clear and no `atomic wiki stale` call (the scan has just rewritten the block it compares against).
11. `docs/reference/realm-wiki.md`, `docs/reference/commands.md`, `docs/guides/knowledge-base.md`, `docs/reference/concepts.md`, `docs/reference/serve.md`, `README.md`, and the `/atomic-help` rows describe the current behavior and mention none of the removed pieces, except one note in `docs/reference/realm-wiki.md` naming the inert leftovers an existing realm may still hold. Every `docs/spec/*.md` whose body describes a removed piece has that body content removed and a `## Change log` entry with a **Removed:** line pointing here.
12. `make -C atomic bundle frontend`, `go vet ./...`, `gofmt -l .`, `go test ./...`, and the frontend test suite all pass; the help-router coverage loop prints no `MISSING:` line.

## Change tree

```
atomic/
  cmd/atomic/
    cmd_wiki.go                      M  drop stamp, bucket diff/promote subcommands; reword shorts
    main_test.go                     M  drop references to removed verbs
  internal/
    cliusage/cliusage.go             M  drop stamp, mark-dirty, bucket diff/promote entries; reword bucket add/list
    hooks/
      hooks.go                       M  drop wiki staleness seam, constant, section
      hooks_test.go                  M  drop staleness tests
    wiki/
      action.go                      M  drop stamp, mark-dirty, bucket diff/promote handlers; reshape bucket list
      bucket.go                      M  keep validateBucketName + RegisterBucket (registry-backed); drop manifest engine
      bucket_registry.go             M  RegisterBucket duplicate check reads <wiki-buckets> (if moved here)
      exports.go                     D
      stale.go                       M  membership drift only
      staleness.go                   M  keep ReadWikiIndexPaths; drop CheckStaleness, MarkDirty, helpers
      stamp.go                       D
      wiki.go                        M  scaffold writes no .gitignore; README structure lines
      *_test.go                      M/D  stamp_test, stamp_knowledge_test, stale_buckets_test deleted; others trimmed
    serve/
      provenance.go                  D
      provenance_test.go             D
      graphoverlay.go                M  drop provenance injection
      graphcache.go                  M  drop provenance injection
      graph.go                       M  drop knowledge node type
      nav.go                         M  drop Knowledge group, bucket stale badge, BucketDiffs
      api_handlers.go                M  StalenessFn returns members only
      stale.go                       M  parse DRIFT lines only
      staleness_cache.go             M  Members set only
      health.go                      M  WikiStaleResult = StaleRepos only
      frontend/src/pages/Status.tsx  M  membership drift only
      frontend/src/components/...    M  drop knowledge group/node styling and copy
      frontend/public/*.js           M  drop provenance edge styling if present
      *_test.go, *.test.tsx          M  follow the above
context/
  _partials/signals-gate.md          M  drop mark-dirty step
  _partials/loop-finalize.md         M  drop mark-dirty call
  commands/refresh-wiki.md           M  realm flow per criterion 10
  commands/atomic-help.md            M  wiki rows, state-file tour, binary rows
  agents/atomic-wiki-inferrer.md     M  drop bucket-synthesis mode and args
  agents/atomic-wiki-writer.md       M  drop fingerprint rule
  skills/atomic-wiki/SKILL.md        M  drop manifest, stamp, knowledge guidance
  skills/atomic-wiki/references/realm.md  M  drop bucket-synthesis mode, stamp notes
docs/
  spec/realm-wiki-drop-refresh.md    A  this spec
  spec/*.md                          M  Removed change-log entries where bodies describe removed pieces
  reference/realm-wiki.md            M
  reference/commands.md              M
  reference/concepts.md              M
  reference/serve.md                 M
  guides/knowledge-base.md           M
README.md                            M
```

## Outline

```
atomic/internal/wiki/staleness.go
  ReadWikiIndexPaths — unchanged; the only survivor
atomic/internal/wiki/stale.go
  Stale — membership drift (added/removed/status) from the recorded <wiki-scan> block vs a live walk
  resolveSummaryMember, summaryStem — deleted if nothing else uses them
  parseBlockMembers — unchanged
atomic/internal/wiki/bucket.go
  validateBucketName — unchanged
  RegisterBucket — validates, refuses a name already in the <wiki-buckets> registry; creates nothing
atomic/internal/wiki/action.go
  wikiAction — verbs: scan, stale, linkify, bucket, init
  wikiBucketAction — verbs: add, list, doc, skill, index
  wikiBucketListAction — name<TAB>path per entry
atomic/internal/hooks/hooks.go
  buildBody — orientation, then reminders
atomic/internal/serve/stale.go
  staleSets — Members only
  parseStaleLines — DRIFT lines only
atomic/internal/serve/health.go
  WikiStaleResult — StaleRepos
  apiWikiStatus — staleRepos, allFresh
atomic/internal/serve/nav.go
  buildRealmNavGroupsJSON — Repos, Concerns, Buckets, External; bucket items carry no Stale
context/commands/refresh-wiki.md
  Realm Step 3 bucket offer — buckets described as indexed capture folders
  Realm Step 5 early stop — with nothing pending, created, or offerable, skips to the wiki commit
  Realm Step 7 repo pass — 7a pending summaries (NEW); 7b re-author selection over existing summaries and concerns
  Realm Step 8 record and linkify — closing atomic wiki scan, then linkify
  Rules — no fingerprint, promote, or .dirty rules
  How the pass works — rewritten to the explicit-selection model
```

## Flows

**Session start on a machine with registered realm wikis**

1. Claude Code runs the session-start hook.
2. The hook builds orientation and reminder sections and emits them; no wiki staleness section is computed or printed.

**Explicit `/refresh-wiki` on a realm**

1. The command runs `atomic wiki scan --root <root>` and records members and statuses.
2. Pending repos the user does not route to repo-scope `/refresh-wiki` get a summary from `atomic-wiki-inferrer` (wiki-output mode); no stamp follows.
3. The command lists existing `repos/` summaries and `concerns/` files as a numbered list; the user types a selection; the selected ones are re-authored.
4. It re-runs `atomic wiki scan --root <root>` to record the new summaries, runs `atomic wiki linkify --root <root>`, prints the disposition report, and commits the wiki repo.

**Registering a bucket**

1. The user runs `atomic wiki bucket add <name>`.
2. The name is validated and checked against the `<wiki-buckets>` registry; a duplicate exits 1.
3. The bucket folder and its `index.md` stub are created, the registry entry is spliced into `wiki/index.md`, and the realm `CLAUDE.md` capture-surfaces section is written. No manifest directory is created.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Go removal | `internal/wiki`, `cmd/atomic`, `internal/cliusage`, `internal/hooks`, `internal/serve` Go files, and their tests | Criteria 1-7 and the Go half of criterion 8; `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` pass |
| 2 | Serve frontend | `internal/serve/frontend/` | The frontend half of criterion 8; `bun test` and `bun run build.ts` pass |
| 3 | Shipped artifacts | `context/` | Criteria 9 and 10; help-router coverage loop clean; `atomic validate config` passes |
| 4 | Docs | reference pages, guide, README, spec change logs | Criterion 11 |

## Risks

- Deleting `exports.go` breaks `internal/serve` at compile time, which is why checkpoint 1 carries every Go file.
- Tests across `internal/wiki` share fixtures that write `.buckets/` or `reflects_*`; trimming them can silently drop coverage of surviving behavior (bucket add, registry splice, scan). Keep those assertions; delete only assertions about removed behavior.
- `docs/spec/` has many specs touching buckets and staleness; a missed body section leaves a contract describing deleted behavior.

## Change log

### 2026-10-18 — Initial spec

**What changed:** Spec written from issue #301.

**Why:** Realm wiki nudges repeated every session without producing refreshes.

### 2026-10-18 — Leftovers note allowed

**What changed:** Criterion 11 allows one note in `docs/reference/realm-wiki.md` that names the inert leftovers (`wiki/.dirty`, `wiki/.buckets/`, `wiki/knowledge/`) on existing realms; the checkpoint table uses the repo's standard columns.

**Why:** Users upgrading an existing realm need to know which files nothing reads any more; `atomic validate spec` S5 requires the standard checkpoint columns.

### 2026-10-18 — No drift check inside realm /refresh-wiki

**What changed:** Criterion 10 and the realm flow no longer run `atomic wiki stale`. It stays a manual verb.

**Why:** The command runs it right after its own `atomic wiki scan`, which has just rewritten the block it compares against, so it could only print nothing.

**Superseded:** `/refresh-wiki` printed `DRIFT` lines from `atomic wiki stale` as information.

### 2026-10-18 — Closing scan restored

**What changed:** Criterion 10 and the realm flow end with a closing `atomic wiki scan` before linkify.

**Why:** Without it, the committed `index.md` lists repos the run just summarized (or promoted to `indexed`) as `pending`, and `atomic wiki stale` reports them as `DRIFT status` until the next scan.

**Superseded:** The realm flow ended with no second scan.

### 2026-10-18 — Correction: concerns are created by hand

**What changed:** The Goal says `/refresh-wiki` re-authors existing concerns and new concerns are written by hand.

**Why:** The realm flow's re-author step only offers existing `concerns/*.md` files; no step creates one, and none is added.

**Superseded:** The Goal said concerns were written only by an explicit `/refresh-wiki`.

## Implementation log

### shipped — 2026-10-18

Built with /autopilot across 4 checkpoints and 12 implement-review iterations, then squashed into one commit for the PR (per-iteration SHAs do not survive the squash):

- CP-1 Go removal across `internal/wiki`, `cmd/atomic`, `cliusage`, `hooks`, and `serve`
- CP-2 serve frontend: membership-drift Status page, knowledge and provenance UI removed
- CP-3 shipped artifacts: realm `/refresh-wiki` selection flow, partials, wiki agents and skill, `/atomic-help`
- CP-4 docs and 20 spec change logs; audit fixes

**Out-of-scope work performed during this build:**

- `parseStaleLines` keyed `DRIFT status` lines by their `a→b` token, and `/api/status` listed nested members twice; both fixed because the stale path was being rewritten anyway.
- The dead `{"wiki","stamp"}` `--repo` exemption in `cmd/atomic/rootflags.go` removed with the verb.
- Follow-up `wiki-fingerprint-legacy-signals-path` closed; the code it tracked was deleted.

**Unforeseens — surprises that emerged during implementation:**

- A drift check inside realm `/refresh-wiki` could only print nothing, since it ran right after the command's own scan; dropped.
- Dropping the closing scan left newly summarized repos recorded as `pending`; the closing scan was restored before linkify.

**Deferred items still open:**

- none

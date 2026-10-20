# Buckets: an index at every level

## Approach

A bucket becomes a folder tree with an `index.md` in every directory, each holding a code-generated `<bucket-docs>` listing of its direct children. Items scaffolded by `atomic wiki bucket doc` are folders with numbered stage folders, starting at `01_raw/`. Decisions are recorded in GitHub issue damusix/atomic-claude#299, modelled on Interpretable Context Methodology (folder structure as routing); this spec is the build contract.

## Goal

- An agent entering a bucket reads its root `index.md` and descends one `index.md` at a time, loading only the layers it needs.
- Every directory in a bucket carries an `index.md` whose `<bucket-docs>` region lists that directory's direct children: its markdown files and its subdirectories, with title and description from frontmatter.
- Numbered stage folders (`01_raw`, `02_findings`, `10_publish`) list in numeric order.
- `atomic wiki bucket doc <bucket> <slug>` scaffolds `<slug>/index.md` and `<slug>/01_raw/index.md`.
- Raw material and research live in one bucket: each item is a slug folder, and `01_raw/` is its collection stage. The shipped docs and examples describe one `research` bucket, not separate `raw` and `research` buckets.

## Non-goals

- No new verb, flag, or config key. `--router` is removed from `bucket doc`.
- No tickets bucket.
- No change to `atomic docs index`'s listing rules: it keeps indexing exactly the directories it is given, one level, with its router and orphan rules. Through the shared writer it now also skips unchanged writes and keeps a rewritten file's mode.
- No model pass over buckets; indexing is code.
- No edit to any user file. Atomic writes only the `<bucket-docs>` region of each `index.md`, creates an `index.md` where none exists, and writes the scaffold files `bucket doc` creates.
- No migration of existing flat buckets: their `<slug>.md` files and subdirectories stay where they are and gain indexes on the next rebuild.

## Success criteria

1. `atomic wiki bucket index [<bucket>]` and `atomic wiki scan` walk every directory of each bucket, recursively, and splice a `<bucket-docs>` region into each directory's `index.md`, creating the file (region only) when absent. Directories skipped: those in the wiki `skipDirs` set, and any whose name starts with `.`. Their subtrees are not walked.
2. A directory's listing has one entry per direct child: each `.md` file other than `index.md`, and each walked subdirectory. Non-markdown files, dot-prefixed files (`._notes.md`), and OS junk (`.DS_Store`, `Thumbs.db`) are not listed.
3. A file entry takes its title, description, tags, and indexed flag from the file, by the existing ladders. A subdirectory entry takes them from its `index.md` by the same ladders, falling back to the directory name for the title; it links to `<name>/index.md`. A subdirectory whose `index.md` has no recognized frontmatter key lists under `### Unindexed`.
4. Entries sort by name, except that a name starting with a run of digits sorts by that number first: names with a numeric prefix come before names without one, ordered by the prefix's integer value, ties broken by full name. `9_x` sorts before `10_x`.
5. A flat bucket listing a `<slug>.md` beside a `<slug>/` directory lists both as separate entries; no router collapse and no orphan flag apply inside buckets.
6. Text outside every `<bucket-docs>` region survives a rebuild byte-for-byte. A second bucket rebuild with no file changes writes no file (no content change, no mtime change), neither a bucket `index.md` nor the realm `wiki/index.md`. `atomic wiki scan`'s own `<wiki-scan>` and member-list splices still rewrite `wiki/index.md` on every scan. A rewritten `index.md` keeps its file mode; a created one is `0644`. The same mode rule holds for every file `atomic wiki` writes through `writeFileAtomic`. An unpaired `<bucket-docs>` tag in one directory warns, leaves that file untouched, and does not stop the walk of its siblings, its subdirectories, or other buckets.
7. `atomic wiki bucket index` prints one `<name>: N indexed, M unindexed` line per bucket, where the counts sum the entries across every index in that bucket's tree.
8. `atomic wiki bucket doc <bucket> <slug>` validates the slug (`[a-z0-9-]+`, and not a name the walk skips such as `build` or `node_modules`), refuses when `<bucket>/<slug>` already exists as a file or directory, then writes `<slug>/index.md` (six-key frontmatter, `created` stamped, H1, a lede comment describing the stage layout) and `<slug>/01_raw/index.md` (frontmatter with `title`, `description`, `created`), then prints the path of `<slug>/index.md`, then rebuilds the bucket's indexes (a hard rebuild error exits 1 after the path is printed). `--router` is an unrecognized flag (exit 2). The `bucket-router-claude.md` template is deleted.
9. `atomic wiki bucket add <name>` on a folder that already holds material rebuilds that bucket's indexes before exiting, so every level is indexed on registration. Its stub and registry splice are unchanged. The `## Capture surfaces` bullet it writes in the realm `CLAUDE.md` names `<bucket>/index.md`, and the realm `<wiki-bucket-list>` links each present bucket as `<bucket>/index.md` (a missing bucket keeps its folder link and `(missing)` flag).
10. The per-bucket skill scaffold (`bucket skill`) describes the slug-folder and numbered-stage layout, not the single-file and router shapes.
11. `cliusage`, the cobra `Short` strings, and `/atomic-help` describe `bucket doc` and `bucket index` as above, with no `--router`. The help-router coverage loop prints no `MISSING:` line.
12. `docs/reference/realm-wiki.md`, `docs/guides/knowledge-base.md`, `docs/reference/docs-index.md`, `docs/reference/concepts.md`, `README.md`, the `atomic-wiki` skill and its `references/realm.md`, and `/refresh-wiki`'s bucket offer describe the merged research bucket, the stage layout, and per-level indexes, and mention no `--router` and no separate `raw` bucket. `docs/spec/bucket-doc-management.md` has its router, orphan, and one-level-walk body content rewritten or removed, with a change-log entry pointing here.
13. `make -C atomic bundle frontend`, `go vet ./...`, `gofmt -l .`, and `go test ./...` pass.

## Change tree

```
atomic/
  cmd/atomic/
    cmd_wiki.go                         M  bucket doc drops --router; doc/index Short strings
    main_test.go                        M  usage table rows follow cliusage
  internal/
    cliusage/cliusage.go                M  bucket doc flags/description; bucket index description
    wiki/
      bucketindex.go                    M  child walk, numeric sort, tree rebuild, skip unchanged writes
      bucketindex_test.go               M  tree, sort, dir entries, idempotence, unpaired isolation
      bucketdoc.go                      M  slug-folder scaffold; routerScaffold deleted
      bucketdoc_test.go                 M  follows scaffold
      action.go                         M  bucket doc args, bucket add rebuild, bucket index counts, reportTreeErrors
      action_test.go                    M  parseBucketDocArgs tests deleted; reportTreeErrors
      bucket_cli_test.go                M  follows action
      bucket_doc_cli_test.go            M  follows action
      bucket_registry.go                M  capture-surfaces bullet names <bucket>/index.md; section-scoped dedupe
      registry.go                       M  writeFileAtomic keeps mode, new files 0644; registry, bucket, and init writes
      registry_test.go                  M  writeFileAtomic mode tests
      wiki.go                           M  Scan reports tree errors per directory
      wiki_test.go                      M  realm bucket list link
      templates/bucket-doc.md           M  slug index template
      templates/bucket-stage.md         A  01_raw index template
      templates/bucket-router-claude.md D
      templates/bucket-skill.md         M  stage layout
context/
  commands/atomic-help.md               M  bucket doc|index row
  commands/refresh-wiki.md              M  bucket offer examples
  skills/atomic-wiki/SKILL.md           M  bucket doc/index lines, bucket name examples
  skills/atomic-wiki/references/realm.md M per-level index description
CLAUDE.md                               M  knowledge-base row in Documentation surfaces
docs/
  spec/bucket-icm-structure.md          A  this spec
  spec/bucket-doc-management.md         M  router/orphan body content removed, change log
  spec/wiki-buckets.md                  M  bucket name examples, bucket add criterion, change log
  spec/wiki-bucket-arg-hardening.md     M  parseBucketDocArgs and --router removed, change log
  reference/realm-wiki.md               M
  reference/docs-index.md               M  listing rules differ from buckets; do not point it at a bucket
  reference/concepts.md                 M
  guides/knowledge-base.md              M
README.md                               M
```

## Outline

```
atomic/internal/wiki/bucketindex.go
  BucketTopic — gains Dir: a subdirectory entry linking to <name>/index.md
  walkBucketTopics — unchanged; atomic docs index only
  walkDirChildren — one directory's direct .md files and walkable subdirectories as entries
  childLess — numeric-prefix-aware name order
  readEntryMeta — title, description, tags, indexed flag from one file; a missing file reads as empty
  listEntry — renders Dir entries as [title](name/index.md)
  TopicCounts — indexed and unindexed totals, summed across a bucket tree
  indexOneDir — splice one index.md from a given entry list; writes only when changed
  IndexDir — atomic docs index entry point; one level, walkBucketTopics
  RebuildBucketTree — every walkable directory under a bucket; summed counts, one error per failing directory
  rebuildRealmBucketList — realm <wiki-bucket-list> splice; writes only when changed
  renderBucketList — links each present bucket as <bucket>/index.md
  RebuildAllBucketIndexes — per bucket via RebuildBucketTree, then the realm list
atomic/internal/wiki/registry.go
  writeFileAtomic — keeps an existing file's mode, gives a new file 0644; used by the registry, bucket, and init writes
atomic/internal/wiki/bucket_registry.go
  writeCaptureSurfacesSection — bullet names <bucket>/index.md; a bullet for the bare folder or index.md in the section counts as present
atomic/internal/wiki/wiki.go
  Scan — reports each bucket tree error through reportTreeErrors
atomic/internal/wiki/bucketdoc.go
  ScaffoldBucketDoc — slug folder, index.md, 01_raw/index.md; refuses an existing slug path
  ScaffoldBucketSkill — unchanged
atomic/internal/wiki/action.go
  wikiBucketDocAction — no --router; rebuilds the bucket after scaffolding
  wikiBucketAddAction — rebuilds the new bucket's indexes after the stub
  printBucketIndexCounts — counts from RebuildBucketTree's summed result
  reportTreeErrors — prints each tree error; only an unpaired region is a warning
atomic/internal/wiki/templates/
  bucket-doc.md — slug index: frontmatter, H1, stage-layout lede
  bucket-stage.md — 01_raw index: title, description, created
  bucket-skill.md — Doc shape section describes slug folders and stages
```

## Flows

**Scaffolding an item**

1. The user runs `atomic wiki bucket doc research seo-audit`.
2. The bucket resolves from the `<wiki-buckets>` registry; the slug is validated; `research/seo-audit` must not exist.
3. `research/seo-audit/index.md` and `research/seo-audit/01_raw/index.md` are written from the embedded templates.
4. The command prints the path of `research/seo-audit/index.md`.
5. The `research` bucket's indexes are rebuilt: the root lists `seo-audit`, `seo-audit/index.md` lists `01_raw`, and `01_raw/index.md` gets an empty listing.

**Rebuilding indexes**

1. `atomic wiki bucket index` (or `atomic wiki scan`) reads the `<wiki-buckets>` registry.
2. For each bucket, every walkable directory is visited top-down. Its direct children are read, sorted, rendered, and spliced into its `index.md`; the file is written only if the result differs.
3. A directory whose `index.md` holds an unpaired tag is reported and skipped; the walk continues.
4. The realm `<wiki-bucket-list>` region in `wiki/index.md` is rebuilt, and the file is written only if the result differs.

**Registering an existing folder**

1. The user runs `atomic wiki bucket add notes` on a `notes/` folder that already holds files and subfolders.
2. The stub `index.md` is written if absent, the registry and capture-surfaces entries are written.
3. The bucket's indexes are rebuilt, so every subfolder gains an `index.md`; no existing user file changes.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Tree indexer | `bucketindex.go`, `action.go` (index, add, counts), tests | Criteria 1-7, 9; `go test ./internal/wiki/... ./cmd/...` |
| 2 | Scaffold | `bucketdoc.go`, `action.go` (doc), templates, `cliusage.go`, `cmd_wiki.go`, `main_test.go`, tests | Criteria 8, 10, the CLI half of 11; full `go test ./...` |
| 3 | Artifacts and docs | `context/`, `docs/`, `README.md` | Criteria 11-12; help-router loop clean; `atomic validate config` and `atomic validate spec` pass |

## Risks

- `IndexDir` is shared with `atomic docs index`; changing its walker would silently change that verb. The tree walk uses its own child walker and leaves `IndexDir`'s behavior intact.
- A bucket holding a code checkout or asset tree gains an `index.md` in every non-skipped directory. Dot-prefixed and `skipDirs` directories are excluded to bound this.
- Tests in `bucket_cli_test.go` and `bucketindex_test.go` assert router and orphan output for buckets; those assertions change meaning rather than disappear, so they are rewritten, not deleted.

## Change log

### 2026-10-18 — Initial spec

**What changed:** Spec written from issue #299.

**Why:** A bucket's index described one directory level, so an agent entering a deep bucket had no map of each folder.

### 2026-10-18 — Dot-prefixed files excluded

**What changed:** Criterion 2 excludes dot-prefixed files from a bucket listing, as dot-prefixed directories already were.

**Why:** Review found macOS AppleDouble files (`._notes.md`) listed, with their binary content read as frontmatter.

### 2026-10-18 — Outline: wrapper and walk helper dropped

**What changed:** The outline no longer lists `isWalkableDir` or `RebuildBucketIndex`; `RebuildBucketTree` returns one error per failing directory and is called directly.

**Why:** Per-directory errors let `bucket add` and single-bucket `bucket index` tell an unpaired region from a hard failure; the wrapper had no production caller left.

**Superseded:** `RebuildBucketIndex` wrapped `RebuildBucketTree`, and the tree walk returned one joined error.

### 2026-10-18 — File mode kept on rewrite

**What changed:** Criterion 6 requires a rewritten `index.md` to keep its mode and a created one to be `0644`.

**Why:** Review found the atomic write left every rewritten file at `0600`, and the tree walk now rewrites every `index.md` in a bucket.

### 2026-10-18 — Slugs the walk skips are rejected

**What changed:** Criterion 8 rejects a slug that names a skipped directory.

**Why:** Review found `bucket doc research build` scaffolded a folder the tree walk never lists or indexes.

### 2026-10-18 — Correction: path printed before the rebuild

**What changed:** Criterion 8 and the scaffolding flow print the path before rebuilding the bucket's indexes.

**Why:** Review found a hard rebuild error left the scaffold on disk with no path reported; the command now prints the path first and still exits 1 on that error.

**Correction:** The body said the rebuild ran before the print.

### 2026-10-18 — Correction: realm list write, change tree, docs index non-goal

**What changed:** Criterion 6 and the rebuild flow cover the realm `wiki/index.md`, which is now written only when its `<wiki-bucket-list>` region changes, and state that the mode rule holds for every file written through `writeFileAtomic`. The change tree and outline add `registry.go`, `registry_test.go`, `wiki.go`, `action_test.go`, `bucket_registry.go`, `reportTreeErrors`, `TopicCounts`, `readEntryMeta`, `rebuildRealmBucketList`, and the two amended sibling specs. The `atomic docs index` non-goal is narrowed to its listing rules.

**Why:** Audit found a no-op rebuild rewrote `wiki/index.md` (same content, new mtime), and the tree and outline left out files the change touched. `atomic docs index` shares `indexOneDir` and `writeFileAtomic`, so it now also skips unchanged writes and keeps file mode.

**Correction:** The body said there was no change to `atomic docs index` at all, and criterion 6 held only for the bucket files in practice.

### 2026-10-18 — Correction: scope of the no-write and mode rules

**What changed:** Criterion 6's no-write rule is scoped to the bucket rebuild, and the `writeFileAtomic` lines name the writes that use it.

**Why:** Review found `atomic wiki scan` rewrites `wiki/index.md` through its own `<wiki-scan>` and member-list splices, and several wiki writers use `os.WriteFile`, not `writeFileAtomic`.

**Correction:** The body said no rebuild writes `wiki/index.md` and that every wiki write goes through `writeFileAtomic`.

### 2026-10-18 — Bucket entry points name index.md

**What changed:** Criterion 9 adds that the realm `CLAUDE.md` capture-surfaces bullet names `<bucket>/index.md` and the realm `<wiki-bucket-list>` links each present bucket's `index.md`. The duplicate check matches the exact backticked path, either form, inside the section only, so a bucket whose path prefixes another's still gets its bullet.

**Why:** Every other listing links a folder's `index.md`; the two realm-level entry points still named the bare folder.

**Superseded:** The bullet named the bucket folder and the realm list linked it.

## Implementation log

### shipped — 2026-10-18

Built across 13 implement-review iterations of /autopilot, then squashed into one commit on the branch. Pre-squash commits (chronological); the squashed commit was later amended so the capture-surfaces bullet and realm bucket list link `<bucket>/index.md`:

- `21c00722` — spec
- `2752fbed` — CP1 tree indexer, write-on-change, mode-keeping atomic write
- `2deae3bd` — CP2 slug-folder scaffold, `--router` removed
- `39b8c61a` — CP3 docs and artifacts; slugs naming skipped folders rejected
- `1545f8c8` — audit fixes: realm list written only on change, doc corrections

**Out-of-scope work performed during this build:**

- `writeFileAtomic` keeps file modes: the tree walk now rewrites every bucket `index.md`, and the old write left each at `0600`.
- `docs/spec/wiki-buckets.md` and `docs/spec/wiki-bucket-arg-hardening.md` amended: their bodies described the removed router shape.

**Unforeseens — surprises that emerged during implementation:**

- An errors.Join result hid hard I/O errors behind an unpaired-region warning; the tree walk now returns one error per directory.
- `os.Mkdir` replaced a check-then-create race in the scaffold so its rollback only removes a folder the call created.

**Deferred items still open:**

- none

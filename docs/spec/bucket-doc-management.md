# Bucket doc management


## Goal


Bucket contents become deterministically indexed at two levels — the realm `wiki/index.md` lists its buckets with descriptions, and each bucket's `index.md` lists its own docs with title, description, and tags — all derived from frontmatter by code, never authored by the model. Two scaffold verbs (`atomic wiki bucket doc`, `atomic wiki bucket skill`) make the frontmatter contract and the per-bucket authoring rules cheap to produce.

Child spec of `docs/spec/wiki-buckets.md`. Design: `docs/design/bucket-doc-management.md`.


## Non-goals


- No rejection or quarantine of frontmatter-free files. Files without frontmatter are listed under an `### Unindexed` sub-heading with a derived description; capture stays frictionless.
- No change to manifest or staleness granularity. `WalkBucket`, `BucketDiff`, `PromoteBucket`, and the `STALE bucket <name>` line are untouched.
- No new `atomic wiki stale` line type for unindexed or un-frontmattered docs. The stale exit-code contract is unchanged.
- No per-bucket skill in the install bundle. `atomic claude install`, `bundlemirror`, and the embedded manifest never carry realm-local skills.
- No central `tags` or `type` vocabulary, and no validation of `status` values. All three stay producer-defined.
- No doc body generation. The scaffold stops after the H1 lede placeholder.
- No rewriting of existing bucket `index.md` content. The `<bucket-docs>` region is appended on next index rebuild; prose already there is preserved untouched. (The `## Members` delimiter migration below is scoped to `wiki/index.md` only.)


## Success criteria


### Frontmatter contract

- [ ] Bucket docs carry six recognized keys. Writer per key is fixed: `created` is written by code at scaffold time; `title`, `type`, `description`, `tags`, `status` are model-written.
- [ ] Every key is optional at index time. A doc with no frontmatter at all indexes without error.
- [ ] Title resolution ladder: frontmatter `title` → first H1 in the body → filename stem (kebab-case preserved verbatim).
- [ ] Description resolution ladder: frontmatter `description` → first prose line of the body, reusing the existing `DeriveMemberDescription` semantics (skips headings, blockquotes, tags, tables, list items; strips links/emphasis/backticks; rejects lines containing `" | "`; truncates at 120 chars) → empty.
- [ ] Tags resolution: frontmatter `tags` as a YAML list of strings. No fallback — absent means no tags rendered. A `tags` value that is a bare string is read as a single-element list; any other shape is ignored.

### Topic walk

- [ ] The index walk is per **topic**, not per file. `<bucket>/<slug>.md` is one topic.
- [ ] A directory `<bucket>/<slug>/` sitting beside `<bucket>/<slug>.md` marks that topic a **router**; the subtree contributes no separate index entries and the entry is flagged as a router with its descendant `.md` count.
- [ ] A directory `<bucket>/<slug>/` with **no** sibling `<slug>.md` is reported as an orphan subtree entry — listed, flagged, never silently dropped.
- [ ] The walk covers the bucket root and one directory level for router detection; `.md` files nested deeper than a router subtree root are never promoted to top-level topics.
- [ ] The bucket's own `index.md` is excluded from its topic list. OS junk (`.DS_Store`, `Thumbs.db`) and `skipDirs` members are excluded, matching `WalkBucket`.
- [ ] Non-`.md` files at the bucket root are excluded from the topic list (they are still hashed by the manifest walk — the two granularities stay independent).

### Managed-region primitive

- [ ] One shared primitive owns every generated region. A region is an XML pseudo-tag pair on its own lines; the code-owned content sits between them and includes the region's `##` heading.
- [ ] Content outside the tag pair is preserved byte-for-byte, whatever the user typed — prose between entries, extra headings, or text after the region.
- [ ] Region body is emitted as: open tag, blank line, content, blank line, close tag. The blank lines are required — under CommonMark an open tag alone on a line starts an HTML block that runs to the first blank line, so a listing written flush against its tags renders as raw text instead of a list.
- [ ] Absent region → appended at EOF, prior content preserved. Present and well-formed → body replaced wholesale.
- [ ] Unpaired tag (open without close, or close without open) → the file is left **untouched**. The verb reports the region unmanageable and exits non-zero; `atomic wiki scan` warns and continues with other work. No content is ever truncated to EOF.
- [ ] Rendering is idempotent — a rebuild with no content change produces a byte-identical file.

### Bucket-level index (`<bucket>/index.md`)

- [ ] A `<bucket-docs>` region holds a code-generated `## Docs` heading plus the topic listing. The `## Conventions` section and all other user prose sit outside it and are preserved.
- [ ] Listing entries are OKF §6 form: `- [<title>](<relpath>) - <description>`, with ` · tags: a, b` appended when tags exist and ` · router (<N> docs)` appended for routers.
- [ ] When no description is derivable the entry is link-only — `- [<title>](<relpath>)` with no trailing separator, matching `buildMembersSection`'s existing OKF §6 link-only form.
- [ ] Entries are sorted by relative path.
- [ ] Docs with zero recognized frontmatter render under an `### Unindexed` sub-heading inside the region, using the same title and description ladders.

### Realm-level index (`wiki/index.md`)

- [ ] A `<wiki-bucket-list>` region holds a code-generated `## Buckets` heading plus one entry per registered bucket as `- [<name>](<link>) - <description>`.
- [ ] `<link>` is relative to `wiki/index.md` (`../<name>` for a realm-root bucket), matching how `atomic wiki linkify` emits file-relative links — the absolute path stays in the `<wiki-buckets>` block, which `stale` and `serve` resolve from any cwd.
- [ ] Entries are sorted by bucket name.
- [ ] Bucket description is derived from `<bucket>/index.md` via the same ladder as doc descriptions.
- [ ] The `<wiki-buckets>` block is **unchanged** — no new attributes, no schema change. It stays the parse-back registry; `<wiki-bucket-list>` is the presentation.
- [ ] A registered bucket whose directory is missing renders link-only with a `(missing)` marker rather than failing the scan.

### `## Members` delimiter migration

- [ ] `buildMembersSection` emits a `<wiki-member-list>` region through the shared primitive instead of the `<!-- wiki-members:start -->` / `<!-- wiki-members:end -->` comment pair, so one file no longer carries two delimiter syntaxes.
- [ ] A `wiki/index.md` carrying the legacy comment markers has that whole region rewritten to the XML form on the next `atomic wiki scan`. The migration is idempotent and runs once; content outside the region is preserved.
- [ ] Legacy-marker detection is retained only for the migration path — no new writes ever emit comment markers.
- [ ] The pre-existing EOF-truncation behavior on a missing end marker is removed; the unpaired-tag rule above governs instead.

### Commands

- [ ] `atomic wiki bucket doc <bucket> <slug> [--router]` writes `<bucket>/<slug>.md` from the embedded scaffold with `created` stamped to the current date.
- [ ] The command refuses when the target file exists, exits non-zero, and names the path. It never overwrites.
- [ ] `--router` additionally creates `<bucket>/<slug>/` and a `<slug>/CLAUDE.md` stub; both are skipped if present.
- [ ] `<slug>` is validated against `[a-z0-9-]+`; a non-conforming slug is rejected with the pattern named, matching the knowledge-topic-name discipline already in `wiki-buckets`.
- [ ] An unregistered `<bucket>` is rejected, listing the registered bucket names.
- [ ] `atomic wiki bucket skill <bucket>` writes `<realm-root>/.claude/skills/<bucket>-management/SKILL.md` from the embedded scaffold, pre-filled with the bucket name and the purpose line read from `<bucket>/index.md`. No-op when the file exists.
- [ ] `atomic wiki bucket index [<bucket>]` rebuilds the `<bucket-docs>` region for one bucket, or every registered bucket when the argument is omitted, and rebuilds the realm `<wiki-bucket-list>` region.
- [ ] `atomic wiki bucket index` reports the per-bucket indexed and unindexed counts on stdout.
- [ ] `atomic wiki scan` rebuilds both index levels as part of its existing pass.
- [ ] All three verbs are registered in `cliusage.go` so `atomic validate artifacts` (rule A1) accepts citations of them.

### Wiring and gates

- [ ] `/refresh-wiki` runs the index rebuild in its deterministic phase, before the commit offer.
- [ ] The `atomic-wiki` skill and `skills/atomic-wiki/references/realm.md` describe the frontmatter contract so bucket-synthesis reads it rather than re-deriving.
- [ ] `/atomic-help` carries the three new verbs in its `cli` topic row (help-router coverage rule).
- [ ] `docs/spec/wiki-buckets.md` gains a change-log entry naming this child spec.
- [ ] `make render` and `make -C atomic bundle` are clean; `go test ./...`, `go vet ./...`, `gofmt -l .` clean.


## Approach


Per-topic walk with frontmatter-derived listings spliced into XML-delimited managed regions, plus two placement verbs reusing the `doctemplate` embed idiom — see `docs/design/bucket-doc-management.md`.


## Change tree


    atomic/internal/wiki/
    ├── managedregion.go ............ A  (shared XML-region splice primitive)
    ├── managedregion_test.go ....... A
    ├── bucketindex.go .............. A  (topic walk, ladders, both region builders)
    ├── bucketindex_test.go ......... A
    ├── bucketdoc.go ................ A  (doc + skill scaffold placement)
    ├── bucketdoc_test.go ........... A
    ├── templates/
    │   ├── bucket-doc.md ........... A  (embedded doc scaffold)
    │   ├── bucket-router-claude.md . A  (embedded router-subtree CLAUDE.md stub)
    │   └── bucket-skill.md ......... A  (embedded per-bucket SKILL.md scaffold)
    ├── bucket_registry.go .......... M  (createBucketIndexStub: frontmatter + empty region)
    └── wiki.go ..................... M  (Scan calls the rebuild; members migrated to the
                                          shared primitive; legacy-marker migration)
    atomic/cmd/atomic/main.go ....... M  (buildWikiCmd: 3 thin bucket children via addBucketSub)
    atomic/cmd/atomic/main_test.go .. M  (cp3WantMeta bucket entries; TestCP3CobraMetadata)
    atomic/internal/wiki/action.go .. M  (wikiBucketAction switch + 3 wikiBucket*Action fns)
    atomic/internal/cliusage/cliusage.go  M  (three surface entries)
    templates/commands/refresh-wiki.md .. M  (deterministic index-rebuild step)
    templates/commands/atomic-help.md ... M  (cli topic row)
    commands/refresh-wiki.md ............ M  (rendered)
    commands/atomic-help.md ............. M  (rendered)
    skills/atomic-wiki/SKILL.md ......... M  (new verbs)
    skills/atomic-wiki/references/realm.md  M  (frontmatter contract for synthesis)
    CLAUDE.md ........................... M  (one clause in the wiki paragraph)
    README.md ........................... M  (feature-table row)
    docs/reference/wiki-workflow.md ..... M  (bucket doc authoring section)
    docs/spec/wiki-buckets.md ........... M  (change-log entry -> child spec)
    atomic/internal/embedded/ ........... M  (regenerated bundle + manifest)


## Outline


    atomic/internal/wiki/managedregion.go
      managedRegion — one region's identity: tag name, and the content the caller wants inside
      spliceManagedRegion — idempotent replace of a tag-delimited region, outside preserved
        findRegion — locate the open/close pair, reporting absent, well-formed, or unpaired
      renderManagedRegion — wrap content as open tag, blank line, content, blank line, close tag
      errUnpairedRegion — sentinel the callers surface as "region unmanageable", never truncate

    atomic/internal/wiki/bucketindex.go
      BucketTopic — one indexed topic: path, title, description, tags, router flag, child count, indexed flag
      walkBucketTopics — bucket dir -> sorted topic list, router collapse, orphan-subtree flagging
      readTopicMeta — one topic file -> BucketTopic via the title/description/tags ladders
        deriveTitle — frontmatter title, then first H1, then filename stem
        deriveTags — frontmatter tags list, tolerating a bare-string value
      renderBucketDocs — topic list -> ## Docs heading plus indexed and unindexed groups
      renderBucketList — registered buckets -> ## Buckets heading plus one entry each
      listEntry — one OKF §6 line, link-only when no description is derivable
      RebuildBucketIndex — one bucket end to end: walk, render, splice
      RebuildAllBucketIndexes — every registered bucket, then the realm region

    atomic/internal/wiki/bucketdoc.go
      ScaffoldBucketDoc — validate slug and bucket, refuse collision, stamp created, write topic file
        routerScaffold — create the sibling subtree dir and its CLAUDE.md stub
      ScaffoldBucketSkill — write the realm-root per-bucket SKILL.md when absent
      bucketPurposeLine — read a bucket index.md purpose line for skill pre-fill
      validateSlug — enforce the kebab-case topic-name pattern

    atomic/internal/wiki/bucket_registry.go
      createBucketIndexStub — reshaped: emit OKF frontmatter and an empty bucket-docs block

    atomic/internal/wiki/wiki.go
      Scan — additionally rebuilds both index levels
      buildMembersSection — emits through the shared primitive as a wiki-member-list region
      migrateLegacyMemberMarkers — one-shot rewrite of the comment-delimited region to XML

    atomic/cmd/atomic/main.go
      buildWikiCmd — registers doc, skill, index as thin bucket children via addBucketSub

    atomic/internal/wiki/action.go
      wikiBucketAction — verb switch gains doc, skill, index cases
      wikiBucketDocAction — CLI arg parse + exit codes for the doc verb, calls ScaffoldBucketDoc
      wikiBucketSkillAction — CLI arg parse + exit codes for the skill verb, calls ScaffoldBucketSkill
      wikiBucketIndexAction — optional-bucket-arg parse, calls RebuildBucketIndex/RebuildAllBucketIndexes

    atomic/internal/wiki/templates/bucket-doc.md
      frontmatter — six keys, created pre-stamped, others placeholdered
      H1 and lede — title placeholder plus one-paragraph lede placeholder
      guidance comment — doc-shape convention, stripped by the author

    atomic/internal/wiki/templates/bucket-skill.md
      frontmatter — skill name and trigger description
      Bucket purpose — pre-filled from the bucket index.md
      Doc shape — simple versus router convention
      Frontmatter contract — the six keys and who writes each
      Bucket-specific rules — placeholder for the user's own authoring rules

    docs/reference/wiki-workflow.md
      Authoring bucket docs — the contract, the verbs, the doc-shape convention


## Flows


    Flow 1: scaffold a topic
    1. user runs `atomic wiki bucket doc research seo`
    2. code resolves the realm root, reads the <wiki-buckets> registry, confirms `research` is registered
    3. code validates the slug against [a-z0-9-]+ and computes research/seo.md
    4. target exists -> exit non-zero naming the path, write nothing
    5. target absent -> render the embedded scaffold with created stamped to today, write the file
    6. --router given -> also create research/seo/ and research/seo/CLAUDE.md, skipping either if present

    Flow 2: rebuild the two indexes
    1. `atomic wiki scan` (or `atomic wiki bucket index [<name>]`) resolves the realm root
    2. for each registered bucket: walk topics, collapsing <slug>/ under its <slug>.md router
    3. for each topic: read frontmatter, apply the title/description/tags ladders, mark indexed or unindexed
    4. render the listing and splice the bucket index.md <bucket-docs> region, prose preserved
    5. an unpaired <bucket-docs> tag -> leave that file untouched, report it unmanageable, continue
    6. read each bucket index.md description via the same ladder
    7. render and splice the <wiki-bucket-list> region in wiki/index.md
    8. wiki/index.md still carrying legacy member comment markers -> rewrite that region as
       <wiki-member-list> in the same pass, once, idempotently
    9. a missing bucket directory yields a link-only (missing) entry rather than an error

    Flow 3: scaffold a per-bucket skill
    1. user runs `atomic wiki bucket skill experiments`
    2. code confirms registration and reads the purpose line from experiments/index.md
    3. target <realm-root>/.claude/skills/experiments-management/SKILL.md exists -> no-op, report and exit 0
    4. target absent -> create parent dirs, render the scaffold with name and purpose pre-filled, write
    5. code prints the path plus a one-line note that the skill loads for sessions started at the realm root


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Shared managed-region primitive | `atomic/internal/wiki/managedregion.go` (+test) | atomic-implementer (mode: feature) | ~2 | Unit tests: absent/well-formed/unpaired paths, blank-line wrapping, byte-for-byte outside preservation, idempotent re-splice, no EOF truncation |
| 2 | Topic walk + ladders + `<bucket-docs>` | `bucketindex.go` (+test) | atomic-implementer (mode: feature) | ~2 | Unit tests: router collapse, orphan subtree, three-step ladders, link-only entries, prose survives arbitrary user edits |
| 3 | `<wiki-bucket-list>` + members migration + `Scan` wiring | `bucketindex.go`, `wiki.go` (+tests) | atomic-implementer (mode: feature) | ~3 | Unit tests: region splice, missing-bucket entry, `<wiki-buckets>` untouched, legacy-marker migration runs once and is idempotent |
| 4 | Scaffold verbs + embedded templates | `bucketdoc.go`, `templates/*.md`, `bucket_registry.go` (+test) | atomic-implementer (mode: feature) | ~6 | Unit tests: collision refusal, slug validation, router variant, skill no-op, `created` stamp |
| 5 | CLI wiring | `main.go` (addBucketSub children), `action.go` (wikiBucketAction switch + 3 action fns), `main_test.go` (cp3WantMeta), `cliusage.go` | atomic-implementer (mode: feature) | ~4 | `TestCP3CobraMetadata` green with new children; `atomic validate artifacts` clean |
| 6 | Artifacts + docs + render/bundle | `templates/commands/`, `commands/`, `skills/atomic-wiki/`, `CLAUDE.md`, `README.md`, `docs/reference/wiki-workflow.md`, `docs/spec/wiki-buckets.md`, `atomic/internal/embedded/` | atomic-implementer (mode: feature) | ~14 | `make render` + `make bundle` diff-clean; `/atomic-help` MISSING-scan clean; full test suite green |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Two walk granularities (per-topic index vs per-file manifest) read as a bug and get "fixed" into one | med | Package doc comment in `bucketindex.go` states the divergence and why; spec non-goal says the manifest walk is untouched |
| Splicing corrupts a user's hand-written bucket `index.md` | med | Region replace is tag-bounded with byte-for-byte preservation outside; an unpaired tag leaves the file untouched rather than guessing; covered by a test that types prose between entries, after the region, and inside the region, then asserts only the region body moved |
| Members migration runs against a realm mid-edit and loses content | low | Migration reuses the same primitive: locate the legacy region, replace only between its bounds, preserve outside; an unpaired legacy marker is left untouched and reported, not repaired |
| Listing renders as raw text because the blank-line rule is dropped | med | The blank lines are emitted by `renderManagedRegion`, not by callers, so no call site can omit them; a rendering test asserts the wrapper shape |
| Realm root is not a git repo, so the per-bucket skill never loads | med | Open question in the design flags it as unverified; checkpoint 3 verifies empirically before the guide text promises the behavior; symlink-into-`~/.claude/skills/` documented as the fallback |
| `atomic wiki scan` slows on a large raw bucket | low | Index walk reads frontmatter only (bounded head read), not whole files; manifest walk already hashes every file, so the added cost is strictly smaller than the existing pass |
| Users expect frontmatter to be enforced and are surprised by silent `### Unindexed` entries | low | The group is a visible heading in the bucket index, not a silent drop; `atomic wiki bucket index` reports the unindexed count on stdout |
| Per-bucket skill name collides with a bundled `atomic-*` skill | low | Name is `<bucket>-management`; the reserved bucket name is `wiki`, and no bundled skill uses the `-management` suffix |


## Change log

<!-- Populated on first amendment after the spec is approved. Do not log drafting/refinement turns. -->


## Implementation log


### Delivered — 2026-07-24

Built across 6 checkpoints (10 implement→review iterations) of /subagent-implementation. Commits (chronological):

- `578f605` — CP1 managed-region splice primitive (`managedregion.go`): tag-delimited regions, byte-for-byte outside preservation, unpaired-tag leaves file untouched (no EOF truncation).
- `e033293` — CP2 per-topic bucket walk + title/description/tags ladders + `<bucket-docs>` render/splice (`bucketindex.go`).
- `bd696a9` — CP3 realm `<wiki-bucket-list>` + `RebuildAllBucketIndexes` + `## Members` migrated to `<wiki-member-list>` + `Scan` wiring. Added `spliceRegionAt` (interior-region boundary normalization) after an atomic-strategist RCA.
- `68df7b0` — CP4 `ScaffoldBucketDoc`/`ScaffoldBucketSkill` + 3 go:embed templates + OKF `createBucketIndexStub` reshape.
- `a807edc` — CP5 CLI wiring: `atomic wiki bucket doc|skill|index` (thin cobra children → `wikiBucketAction` switch → action fns; cliusage entries).
- `17eeb77` — CP6 discoverability + docs (atomic-wiki skill, realm.md frontmatter contract, /atomic-help cli topic, refresh-wiki note, CLAUDE.md, README, wiki-workflow.md, wiki-buckets change-log) + render/bundle regen.
- `b12de4c` — Finalize polish: fence-aware title derivation, single-bucket index no cross-walk, one file read per topic, reversed-order test, spliceRegionAt precondition doc.

**Out-of-scope work performed during this build:**

- `## Members` delimiter migration to `<wiki-member-list>` — user-approved before the loop (it touches shipped code + rewrites existing installs' `wiki/index.md` once); folded into CP3.
- `spliceRegionAt` boundary primitive — not in the original spec; introduced in CP3 after three review rounds on migration whitespace, per an atomic-strategist RCA. Centralizes LEAD/TRAIL/EOF normalization so migration stops hand-rolling newlines.

**Unforeseens — surprises that emerged during implementation:**

- CP3 members migration failed review 3× on the same class (boundary whitespace: reorder → trailing blank → leading+EOF). Stuck-fix escalation fired; the RCA found the root cause (no tested op normalized both boundaries of an interior region) and predicted a silent data-loss case (prose between heading and marker deleted) the per-round reviews had not reached. Redesign closed the full LEAD×TRAIL matrix + 4 preempt cases in one pass.
- Spec's CLI Outline named `runBucketDoc` handlers in `main.go`; the real codebase uses a thin-cobra → `dispatch` → `wikiBucketAction` switch pattern with logic in `action.go`. Corrected the spec before dispatch (`725995b`).
- CP6 change-log insertion accidentally deleted the `## Implementation log` heading in `wiki-buckets.md`; reviewer caught it, orchestrator restored it (final-docs scope).

**Follow-up dispositions (from FOLLOWUPS triage):**

- Fixed this session (`b12de4c`): F-1 reversed-order unpaired-region test, F-2 double file read per topic, F-3 `deriveTitle` code-fence awareness, F-4 `spliceRegionAt` precondition doc, F-7 single-bucket index cross-walk / double-warning.
- Dropped (reason): F-5 TOCTOU collision — inherited `Lstat`-then-write convention, unrealistic for a single-user CLI. F-6 `--root` parse duplication — dedup would touch 4 call sites for 1 new caller. F-8 skill double-`Lstat` — benign redundant stat, documented.
- Pre-existing unrelated: `internal/doctor` `TestRepairPlan_configWARN_fixable` fails on this machine (reads real `~/.atomic/config.toml`); tracked in `.claude/project/followups/doctor-config-test-reads-real-home.md`, not touched by this branch.

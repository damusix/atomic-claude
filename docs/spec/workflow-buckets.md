# Workflow buckets and touch-driven bucket refresh


## Goal


A realm bucket declares its type in its `index.md` frontmatter. A workflow bucket gets a scaffolded ICM layout: numbered stage folders, each with a `CLAUDE.md` contract and an `output/` folder whose run artifacts never make the bucket stale. Every registered bucket gets its authoring skill. In a session started at the realm root, an agent that edits a bucket is told during that session that `/refresh-wiki bucket <name>` refreshes it.

This extends `docs/spec/wiki-buckets.md` (manifest, diff, promote, stale, synthesis) and `docs/spec/bucket-doc-management.md` (managed regions, bucket index, scaffold verbs). Both contracts hold unchanged except where this spec names a change.


## Non-goals


- Running a workflow. Atomic scaffolds and indexes stages; the agent and the human execute them.
- Synthesizing run deliverables (`<NN_stage>/output/`) into `wiki/knowledge/`.
- Blocking a tool call or a turn until a bucket is refreshed.
- A user-scope hook. The bucket-touch hook is never written to `~/.claude/settings.json`.
- A verb that converts a capture bucket into a workflow bucket. Editing `type:` by hand is the only path.
- A doctor check for the bucket-touch hook. `IsInstalled` and `atomic doctor` keep checking the session-start hook only.
- Any change to the realm `CLAUDE.md` `## Capture surfaces` section.
- Any change to `atomic docs index` output.


## Success criteria


**Bucket type**

- [ ] The type is read from the `type` key in the bucket root `index.md` frontmatter. `Workflow`, compared case-insensitively, is a workflow bucket.
- [ ] Any other value, a missing key, no frontmatter block, unparseable frontmatter, or a missing `index.md` is a capture bucket, with no error.
- [ ] For existing buckets (the stub writes `type: Bucket`), `bucket diff`, `atomic wiki stale`, and `<bucket-docs>` output is byte-identical to before this change.

**Manifest membership**

- [ ] One exported wiki predicate decides whether a bucket-relative path is in the bucket's manifest. `WalkBucket` and the bucket-touch hook both use it, and no second filter exists.
- [ ] The predicate excludes the bucket-root `index.md`, any path inside a `skipDirs` subtree, and `osJunk` base names, for both bucket types.
- [ ] A stage folder is a top-level bucket directory named with two digits, `_`, and a slug that passes the existing bucket-doc slug validation (`01_research`).
- [ ] For a workflow bucket, the predicate also excludes every path under `<stage>/output/`. Every other path stays in the manifest, including the rest of each stage folder and any other `output/` folder (`references/output/`, `output/` at the bucket root, `01_research/notes/output/`).
- [ ] For a capture bucket, `01_x/output/a.md` stays in the manifest.
- [ ] `bucket diff`, `bucket promote`, `bucket list`, `atomic wiki stale`, `atomic serve` health, and `/refresh-wiki` Step 8 all inherit the exclusion through `WalkBucket`. Example: after a promote, writing a file under a workflow stage's `output/` leaves `atomic wiki stale` at exit 0.

**Bucket skill**

- [ ] The capture and workflow skill templates do not copy the bucket's purpose text. Each one points at `<bucket>/index.md` as the purpose source.
- [ ] Both templates tell the agent to run `/refresh-wiki bucket <name>` on a bucket it edited before reporting the work done.
- [ ] The workflow template describes the stage protocol:
    - read the stage's `CLAUDE.md`
    - read the previous stage's `output/`
    - write only to the current stage's `output/`
    - stop for human review between stages
    - add stages with `atomic wiki bucket stage`
- [ ] `atomic wiki bucket skill <bucket>` picks the template from the bucket's type.

**`atomic wiki bucket add --type`**

- [ ] `--type workflow` (case-insensitive) writes `type: Workflow` in the new `index.md` stub, creates `<bucket>/references/`, and scaffolds the workflow skill.
- [ ] `--type capture`, or no flag, behaves as it does today and also scaffolds the capture skill.
- [ ] Any other `--type` value exits 2 with a message naming the accepted values, and writes nothing.
- [ ] If `<realm-root>/.claude/skills/<bucket>-management/SKILL.md` already exists, `bucket add` for a new bucket leaves it byte-identical.

**`atomic wiki bucket stage <bucket> <slug>`**

- [ ] Creates `<bucket>/NN_<slug>/CLAUDE.md` from an embedded stage-contract template with `## Inputs`, `## Process`, and `## Outputs` sections. Also creates an empty `<bucket>/NN_<slug>/output/`.
- [ ] `NN` is one more than the highest existing stage number, zero-padded to two digits. The first stage is `01`. Gaps are never filled and existing stages are never renumbered.
- [ ] Exits 1, names the cause, and writes nothing when:
    - the bucket is not registered
    - the bucket is a capture bucket
    - the slug fails validation
    - a stage folder with that slug already exists under any number
    - the next number would go past `99`
- [ ] A flag or usage error exits 2. These exit codes match `bucket doc` and `bucket skill`.
- [ ] Prints the created stage path on stdout.

**Bucket index regions**

- [ ] A workflow bucket's `index.md` carries a code-generated `<bucket-stages>` managed region. Under a `## Stages` heading it lists every stage folder in number order, each linking to that stage's `CLAUDE.md`.
- [ ] `atomic wiki bucket index` and `atomic wiki scan` rebuild the region with the managed-region primitive from `docs/spec/bucket-doc-management.md`. Unpaired tags leave the file untouched, and a rebuild with no change writes identical bytes.
- [ ] When a workflow bucket is rebuilt, stage folders and `references/` do not appear in its `<bucket-docs>` region. The exclusion lives on the bucket rebuild path only, not in the shared topic walk.
- [ ] For a directory whose `index.md` says `type: Workflow` and contains `01_x/`, `atomic docs index` output is byte-identical to before this change.
- [ ] A capture bucket's `index.md` never gains a `<bucket-stages>` region.

**Upgrade path through `atomic wiki scan`**

- [ ] `atomic wiki scan` scaffolds the skill for every registered bucket that has none, using the bucket's type. It never overwrites an existing skill file.
- [ ] `atomic wiki scan` ensures the realm bucket-touch hook when the realm has at least one registered bucket. It does not create or modify `.claude/settings.json` for a realm with none.
- [ ] `atomic wiki bucket add` ensures the realm-scope marker (`config.EnsureScopeMarker`) alongside the hook, so the hook resolves the realm in a realm last scanned by a binary that predates the marker.

**`atomic hooks bucket-touch`**

- [ ] Reads one Claude Code `PostToolUse` JSON payload on stdin. It uses `session_id`, `agent_id` when present, `cwd`, and `tool_input.file_path` (or `tool_input.notebook_path`).
- [ ] Resolves the realm root from `cwd` with the existing realm-scope marker resolver, `config.FindScopeRoot(cwd, "realm")`, the same one `where.resolveRealmScope` uses. Then it matches the edited path against that realm's registered buckets.
- [ ] When the path is in a registered bucket's manifest, prints one JSON object: `{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"..."}}`. The text states, as a fact rather than an instruction, that the file is in bucket `<name>` and that `/refresh-wiki bucket <name>` refreshes it.
- [ ] Prints nothing in any of these cases:
    - the path is outside every registered bucket
    - `cwd` is outside a realm
    - the path is a bucket-root `index.md`
    - the path is inside a `skipDirs` subtree
    - the path is an `osJunk` file
    - the path is under a workflow stage's `output/`
    - the payload has no path field
- [ ] Reports a bucket at most once per actor per refresh cycle. The actor is `agent_id` when the payload has one, else `session_id`. After a report, further edits by the same actor to the same bucket print nothing until `bucket promote` writes a baseline newer than that actor's marker.
- [ ] A subagent edit (payload carries `agent_id`) does not suppress the main thread's first reminder for the same bucket in the same session.
- [ ] Markers live under per-user state in `~/.atomic/`, never inside the realm or `wiki/`.
- [ ] Every failure exits 0 with empty stdout: bad JSON, an unreadable registry, an unwritable marker directory. The handler spawns no subprocess.
- [ ] Has a `cliusage` entry and a cobra child under `atomic hooks`.

**Realm hook registration**

- [ ] The bucket-touch hook is registered only in `<realm-root>/.claude/settings.json`. The entry is under `PostToolUse` with matcher `Edit|Write|NotebookEdit` exactly (no anchors or regex syntax) and runs `atomic hooks bucket-touch`.
- [ ] A new leaf settings-writer package under `atomic/internal/` registers or removes one command under any event and matcher.
    - Both `hooks` and `wiki` import it, and it imports neither.
    - `wiki` does not import `hooks`, and `go build ./...` has no import cycle.
- [ ] The leaf package's registration lookup and its unregister path work for any event; neither assumes `SessionStart`.
- [ ] `claudesettings` holds the only implementation of settings read, write, register, and unregister. The hooks package keeps `readSettingsHujson`, `writeSettingsHujson`, `registerInSettings`, and `unregisterFromSettings` as thin delegates to it, so `hooks_hujson_test.go` and `outputstyle.go` compile and pass unmodified.
- [ ] The bucket-touch entry (event `PostToolUse`, matcher `Edit|Write|NotebookEdit`, command `atomic hooks bucket-touch`) is defined once, in the wiki package, by an ensure function and a remove function for the realm hook entry. `bucket add`, `atomic wiki scan`, `hooks.InstallRealm`, and `hooks.UninstallRealm` all call those two functions and none of them restates the entry.
- [ ] Existing session-start behavior is unchanged: `hooks.Install`, `hooks.Uninstall`, and `IsInstalled` act on `SessionStart` as before, and the existing `atomic/internal/hooks` tests pass unmodified.
- [ ] `bucket add` installs the hook by calling the wiki package's realm-hook ensure function from inside the wiki package. `cmd_wiki.go` does not re-parse arguments to do it.
- [ ] Installation is idempotent: repeat runs leave exactly one entry. Every other key, hook, and comment in the file survives the hujson round-trip.
- [ ] When the install fails during `bucket add` or `atomic wiki scan` (malformed or read-only `settings.json`), the verb prints a warning on stderr and its exit code is unchanged.
- [ ] `atomic hooks install --scope realm` and `atomic hooks uninstall --scope realm` add or remove only the bucket-touch `PostToolUse` entry in `<realm-root>/.claude/settings.json`.
    - The realm root comes from the realm-scope marker, so both work from the realm root or from inside a member repo.
    - With no realm, the verb exits non-zero and names the missing realm.
    - Neither touches `SessionStart` or the output-style key, and neither runs the legacy-script migration.
- [ ] `~/.claude/settings.json` never gains the bucket-touch entry, whatever path triggers the install.

**`/refresh-wiki bucket <name...>`**

- [ ] `/refresh-wiki bucket <name> [<name>...]` resolves the realm from cwd and checks every name against the `<wiki-buckets>` registry. If any name is unknown, it stops before doing any work, with an error naming each unknown bucket.
- [ ] For each named bucket it rebuilds the bucket index and runs Step 8 for that bucket only (diff, inferrer in bucket-synthesis mode, stamp, promote). It then runs the Step 13 wiki commit.
- [ ] It skips:
    - the repo pass and the concern pass
    - the one-time bucket offer and the placeholder fill
    - Step 12 (it leaves `wiki/.dirty` untouched)

**Documentation and discovery**

- [ ] Each feature is documented in the files assigned to it:

    | Feature | Documented in |
    |---------|---------------|
    | Bucket types, workflow layout, `bucket add --type`, `bucket stage` | `docs/reference/realm-wiki.md`, `docs/guides/knowledge-base.md`, `context/skills/atomic-wiki/SKILL.md`, `docs/wiki/wiki.md` |
    | Workflow bucket as synthesis input (stage `CLAUDE.md` and `references/` in, `output/` out) | `context/skills/atomic-wiki/references/realm.md` |
    | `/refresh-wiki bucket <name...>` | `context/commands/refresh-wiki.md`, `docs/reference/commands.md`, `docs/reference/realm-wiki.md`, `context/skills/atomic-wiki/SKILL.md` |
    | `atomic hooks bucket-touch` and realm hook scope | `docs/reference/realm-wiki.md`, `docs/wiki/config.md`, `docs/wiki/wiki.md` |
    | `atomic hooks install/uninstall --scope realm` | `docs/guides/install.md`, `docs/wiki/config.md`, `cliusage.go` descriptions for `hooks install` and `hooks uninstall` |
    | Parent-spec body lines this spec supersedes, each with a dated Change log entry carrying a **Superseded:** line | `docs/spec/bucket-doc-management.md` (the orphan-subtree "never silently dropped" criterion; the skill "pre-filled with the purpose line" criterion), `docs/spec/wiki-buckets.md` (the two manifest-exclusion lines naming only `index.md` and OS junk) |

- [ ] `docs/reference/realm-wiki.md` covers four behaviors:
    - the hook fires only in sessions started at the realm root
    - `atomic wiki scan` reinstalls the hook after `hooks uninstall --scope realm` while the realm still has buckets
    - `atomic wiki scan` re-creates a bucket skill the user deleted. To opt out, keep the skill file (emptied or rewritten) or unregister the bucket
    - hand-editing `type:` on a bucket with existing stage `output/` files makes those files show as removed in the next diff
- [ ] `context/commands/atomic-help.md` mentions `atomic wiki bucket stage`, `bucket add --type`, `/refresh-wiki bucket`, `atomic hooks bucket-touch`, and `hooks install --scope realm`. The help-router check loop in the root `CLAUDE.md` prints no `MISSING:` line.
- [ ] The parent-spec rewrites land in checkpoint 7, after the code that makes them true, so neither parent spec describes unbuilt behavior.
- [ ] `make -C atomic test vet` is green. `atomic validate spec`, `atomic validate config`, and `atomic validate artifacts` report 0 FAIL.


## Approach


Type marker in bucket `index.md` frontmatter, factory-only manifest through one shared membership predicate, and a non-blocking realm-scope `PostToolUse` hook paired with a scoped `/refresh-wiki bucket` mode; see `docs/design/workflow-buckets.md`.


## Change tree


```
atomic/internal/claudesettings/ ....... A  (leaf: register/remove one command under event+matcher)
├── claudesettings.go ................. A
└── claudesettings_test.go ............ A
atomic/internal/wiki/
├── buckettype.go ..................... A  (bucket type from index.md frontmatter; stage-dir test)
├── buckettype_test.go ................ A
├── bucket.go ......................... M  (manifest-membership predicate; WalkBucket uses it)
├── bucket_test.go .................... M
├── bucket_registry.go ................ M  (createBucketIndexStub: type value)
├── bucketindex.go .................... M  (<bucket-stages>; workflow-only exclusions on bucket rebuild)
├── bucketindex_test.go ............... M
├── bucketdoc.go ...................... M  (skill template by type; ScaffoldBucketStage; bucketPurposeLine removed)
├── bucketdoc_test.go ................. M  (TestBucketPurposeLine_* removed)
├── realmhook.go ...................... A  (bucket-touch entry: ensure / remove via claudesettings)
├── realmhook_test.go ................. A
├── wiki.go ........................... M  (Scan: missing-skill scaffold, realm hook ensure)
├── action.go ......................... M  (bucket add --type + hook install; bucket stage; usage)
├── bucket_cli_test.go ................ M  (add installs hook; stage verb; warn-on-install-failure)
├── scan_members_test.go .............. M  (scan with zero buckets leaves settings absent; skill backfill)
└── templates/
    ├── bucket-skill.md ............... M  (purpose points at index.md; refresh-before-done)
    ├── bucket-skill-workflow.md ...... A  (workflow variant: stage protocol)
    └── bucket-stage.md ............... A  (stage CLAUDE.md: Inputs / Process / Outputs)
atomic/internal/hooks/
├── buckettouch.go .................... A  (PostToolUse handler)
├── buckettouch_test.go ............... A
├── hooks.go .......................... M  (InstallRealm / UninstallRealm call wiki's realm-hook functions)
├── hooks_test.go ..................... M  (new tests: realm install/uninstall touch only the bucket-touch entry)
└── hooks_hujson.go ................... M  (settings read/write/register/unregister become delegates)
atomic/cmd/atomic/
├── cmd_wiki.go ....................... M  (stage child; --type flag metadata)
├── cmd_hooks.go ...................... M  (bucket-touch child; --scope realm)
├── cmd_hooks_test.go ................. A  (--scope realm: resolves from a member, errors with no realm)
└── main_test.go ...................... M  (cobra metadata for new children)
atomic/internal/cliusage/cliusage.go .. M  (bucket stage, add --type, hooks bucket-touch, --scope realm)
context/commands/refresh-wiki.md ...... M  (bucket <name...> scoped mode)
context/commands/atomic-help.md ....... M  (wiki + cli rows; tour state-file stage)
context/skills/atomic-wiki/SKILL.md ... M  (workflow bucket, stage, scoped refresh routes)
context/skills/atomic-wiki/references/realm.md  M  (B1: workflow bucket synthesis input)
docs/reference/realm-wiki.md .......... M  (bucket types, stages, touch hook, scoped refresh)
docs/reference/commands.md ............ M  (/refresh-wiki row: bucket mode)
docs/guides/knowledge-base.md ......... M  (workflow buckets)
docs/guides/install.md ................ M  (hooks --scope realm)
docs/wiki/wiki.md ..................... M  (verb table, manifest predicate, bucket types)
docs/wiki/config.md ................... M  (hooks package, claudesettings seam, realm install)
docs/spec/wiki-buckets.md ............. M  (manifest-exclusion lines rewritten; Superseded log entry)
docs/spec/bucket-doc-management.md .... M  (orphan + skill-purpose lines rewritten; Superseded log entry)
```


## Outline


```
atomic/internal/claudesettings/claudesettings.go
  Register — add one command under an event + matcher, idempotent, hujson-preserving
  Unregister — remove that command from an event, drop empty containers
  IsRegistered — lookup under any event

atomic/internal/wiki/buckettype.go
  BucketType — capture or workflow
  ReadBucketType — type from <bucket>/index.md frontmatter, capture on any miss
  isStageDir — top-level NN_<slug> name test

atomic/internal/wiki/bucket.go
  InBucketManifest — exported membership predicate (root index.md, skipDirs, osJunk, stage output/)
  WalkBucket — filters through InBucketManifest

atomic/internal/wiki/bucket_registry.go
  createBucketIndexStub — writes the type value for the requested bucket type

atomic/internal/wiki/bucketindex.go
  <bucket-stages> renderer — ordered stage list
  RebuildBucketIndex — workflow buckets: splice <bucket-stages>, drop stage dirs and references/ from <bucket-docs>

atomic/internal/wiki/bucketdoc.go
  ScaffoldBucketSkill — template chosen by bucket type, no purpose copy
  bucketPurposeLine — removed, with TestBucketPurposeLine_ReadsDescription and TestBucketPurposeLine_EmptyWhenNoDescription
  ScaffoldBucketStage — next NN, stage CLAUDE.md, output/

atomic/internal/wiki/realmhook.go
  EnsureRealmHook — the one bucket-touch entry definition, registered in <realm-root>/.claude/settings.json
  RemoveRealmHook — removes that entry only

atomic/internal/wiki/wiki.go
  Scan — scaffold missing bucket skills; ensure realm hook when ≥1 bucket, warn on failure

atomic/internal/wiki/action.go
  wikiBucketAddAction — --type, references/, skill scaffold, realm hook install with warn-on-failure
  wikiBucketStageAction — stage verb, exit 1 refusals, exit 2 usage

atomic/internal/wiki/templates/
  bucket-skill.md — purpose pointer, refresh-before-done
  bucket-skill-workflow.md — purpose pointer, stage protocol, refresh-before-done
  bucket-stage.md — Inputs / Process / Outputs

atomic/internal/hooks/buckettouch.go
  BucketTouch — stdin payload to optional additionalContext JSON; never errors outward
  actor marker — agent_id else session_id, per bucket, under ~/.atomic/, compared to baseline mtime

atomic/internal/hooks/hooks.go
  InstallRealm / UninstallRealm — call wiki's EnsureRealmHook / RemoveRealmHook; no SessionStart, legacy, or output-style work

atomic/internal/hooks/hooks_hujson.go
  readSettingsHujson / writeSettingsHujson — thin delegates to claudesettings
  registerInSettings / unregisterFromSettings — thin delegates, SessionStart behavior unchanged

atomic/cmd/atomic/cmd_wiki.go
  bucket stage child; add --type flag metadata

atomic/cmd/atomic/cmd_hooks.go
  bucket-touch child
  --scope realm — realm root via realm-scope marker, error when absent

atomic/internal/cliusage/cliusage.go
  wiki bucket stage — new entry
  wiki bucket add — --type flag
  hooks bucket-touch — new entry
  hooks install / uninstall — --scope realm in descriptions

context/commands/refresh-wiki.md
  Step 0 — bucket <name...> argument detection
  Bucket scope — validate names, index rebuild, Step 8 per bucket, Step 13 commit, no Step 12

context/commands/atomic-help.md
  wiki topic row — bucket types, stage verb, scoped refresh
  cli topic rows — bucket stage, add --type, hooks bucket-touch, --scope realm
  Tour Stage 3 — workflow bucket layout line

context/skills/atomic-wiki/SKILL.md
  Bucket creation route — --type workflow
  Workflow stage route — atomic wiki bucket stage
  Scoped refresh — /refresh-wiki bucket <name>

context/skills/atomic-wiki/references/realm.md
  B1 — workflow bucket: stage CLAUDE.md and references/ are the input, output/ never is

docs/reference/realm-wiki.md
  Capture buckets — bucket types, workflow layout, stage verb
  Touch hook — realm-root scope, scan reinstall after uninstall, Bash edits
  Skill backfill — scan re-creates a deleted skill; opt out by keeping the file or unregistering the bucket
  Scoped refresh — /refresh-wiki bucket
  Converting by hand — type: edit and the removed-files diff

docs/reference/commands.md
  /refresh-wiki row — bucket <name...> mode

docs/guides/knowledge-base.md
  Workflow buckets — when to use, layout, stage verb

docs/guides/install.md
  Session-start hook paragraph — --scope realm and what it touches

docs/wiki/wiki.md
  Verb table — bucket add --type, bucket stage
  Bucket lifecycle — InBucketManifest exclusions, bucket types

docs/wiki/config.md
  Hooks — bucket-touch handler, claudesettings seam, realm install contract

docs/spec/wiki-buckets.md
  Manifest exclusion lines — rewritten to include workflow stage output/
  Change log — dated entry with Superseded line, pointing to docs/spec/workflow-buckets.md

docs/spec/bucket-doc-management.md
  Orphan-subtree criterion — workflow stage dirs and references/ excluded on the bucket rebuild path
  bucket skill criterion — skill points at index.md instead of copying the purpose line
  Change log — dated entry with Superseded line, pointing to docs/spec/workflow-buckets.md
```


## Flows


**Flow: scaffold a workflow bucket**

1. user runs `atomic wiki bucket add pipeline --type workflow` at the realm root
2. `bucket add` does five things:
    - registers the bucket
    - writes `pipeline/index.md` with `type: Workflow`
    - creates `pipeline/references/`
    - writes the `## Capture surfaces` bullet, as it does today
    - scaffolds `.claude/skills/pipeline-management/SKILL.md` from the workflow template
3. `bucket add` registers the bucket-touch `PostToolUse` entry in `<realm-root>/.claude/settings.json` through the wiki package's realm-hook ensure function. A failure there prints a warning and leaves the exit code at 0.
4. `atomic wiki bucket stage pipeline research` creates `pipeline/01_research/CLAUDE.md` and `pipeline/01_research/output/`. A second call with `script` creates `02_script/`.
5. `atomic wiki bucket index pipeline`, or the next `atomic wiki scan`, writes the `<bucket-stages>` region listing `01_research` and `02_script`.

**Flow: workflow run stays fresh**

1. after a promote, an agent writes `pipeline/01_research/output/draft.md`
2. `InBucketManifest` rejects the path, so `atomic wiki stale` and `bucket diff pipeline` both exit 0, and the hook prints nothing
3. the agent edits `pipeline/01_research/CLAUDE.md`, and `atomic wiki stale` now prints `STALE bucket pipeline`

**Flow: touch reminder and scoped refresh**

1. in a session started at the realm root, the main thread edits `research/seo.md` with `Edit`
2. Claude Code runs `atomic hooks bucket-touch` with the `PostToolUse` payload
3. the handler:
    - resolves the realm through the realm-scope marker
    - matches bucket `research`
    - confirms the path is in the manifest
    - finds no marker for this `session_id` newer than the baseline
    - writes the marker and prints the `additionalContext` JSON
4. Claude Code shows the statement to the agent next to the tool result. A second main-thread edit to `research/` prints nothing.
5. a subagent edit to `research/` carries an `agent_id`, so it gets its own marker and its own first reminder
6. the agent runs `/refresh-wiki bucket research`, which:
    - validates the names
    - rebuilds the `research` index
    - runs Step 8: diff, inferrer, stamp, promote
    - commits `wiki/` (Step 13) and leaves `wiki/.dirty` untouched
7. promote writes a baseline newer than every marker, so the next edit to `research/` is reported again

**Flow: existing realm picks up skills and the hook**

1. a realm whose buckets predate this change runs `/refresh-wiki`
2. Step 2's `atomic wiki scan` scaffolds a skill for every registered bucket missing one. It then ensures the bucket-touch entry in `<realm-root>/.claude/settings.json`, leaving other settings intact.

**Flow: realm-scope hook install from a member repo**

1. user runs `atomic hooks uninstall --scope realm` inside a member repo
2. the realm-scope marker resolves the realm root
3. only the bucket-touch `PostToolUse` entry is removed. `SessionStart` and the output-style key stay as they were.


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Bucket type and manifest-membership predicate | `atomic/internal/wiki/buckettype.go`, `bucket.go` (+tests) | atomic-implementer (mode: feature) | ~4 | Unit tests: every type-resolution case; predicate rejects root `index.md`, a `skipDirs` path, `osJunk`, and workflow stage `output/`; `references/output/` and nested `output/` kept; capture fixture walk byte-identical; stale exit 0 after an `output/` write |
| 2 | Leaf `claudesettings` package, hooks delegates, wiki realm-hook entry | `atomic/internal/claudesettings/` (+test), `hooks_hujson.go`, `atomic/internal/wiki/realmhook.go` (+test) | atomic-implementer (mode: feature) | ~5 | New package tests: register/unregister under `PostToolUse` + matcher, idempotent, foreign hooks and comments preserved, read-only skip; `realmhook_test.go`: one entry after repeated ensures, remove leaves `SessionStart`; existing `atomic/internal/hooks` tests (incl. `hooks_hujson_test.go`) pass unmodified; `go build ./...` cycle-free |
| 3 | `bucket add --type`, typed skill templates, `scan` skill backfill and hook ensure | `action.go`, `wiki.go`, `bucket_registry.go`, `bucketdoc.go`, `templates/bucket-skill*.md`, `cmd_wiki.go`, `cliusage.go`, `main_test.go` (+wiki tests) | atomic-implementer (mode: feature) | ~11 | `bucket_cli_test.go`: workflow stub + `references/` + workflow skill; capture default; bad `--type` writes nothing; existing skill byte-identical; add installs hook; malformed settings warns with exit 0. `scan_members_test.go`: zero buckets leaves settings absent; missing skill backfilled, existing kept. Cobra metadata test green |
| 4 | `bucket stage` verb and workflow index regions | `bucketdoc.go`, `bucketindex.go`, `action.go`, `templates/bucket-stage.md`, `cmd_wiki.go`, `cliusage.go`, `main_test.go` (+tests) | atomic-implementer (mode: feature) | ~8 | Unit tests: NN numbering with gaps; each exit-1 refusal and exit-2 usage error; idempotent `<bucket-stages>` rebuild; stage dirs and `references/` absent from workflow `<bucket-docs>`; `atomic docs index` byte-identical on a `type: Workflow` dir; no region on capture buckets |
| 5 | `atomic hooks bucket-touch` handler and `--scope realm` | `atomic/internal/hooks/buckettouch.go`, `hooks.go`, `cmd_hooks.go`, `cliusage.go`, `main_test.go` (+tests) | atomic-implementer (mode: feature) | ~6 | Table tests over stdin payloads: match; no match; outside realm; root `index.md`; `skipDirs` path; workflow `output/`; missing path; malformed JSON (exit 0, empty stdout); per-actor suppression; subagent edit leaves main-thread reminder intact; re-arm after promote; markers only under a temp `~/.atomic/`. `hooks_test.go`: `InstallRealm`/`UninstallRealm` touch only the bucket-touch entry; `cmd_hooks_test.go`: `--scope realm` from a member resolves the realm root, no realm exits non-zero |
| 6 | `/refresh-wiki bucket` scoped mode and skill routes | `context/commands/refresh-wiki.md`, `context/skills/atomic-wiki/SKILL.md`, `context/skills/atomic-wiki/references/realm.md` | atomic-implementer (mode: feature) | ~3 | `atomic validate artifacts` 0 FAIL; reviewer walks the scoped mode against the `/refresh-wiki bucket` criteria (unknown-name stop, skipped passes, no Step 12, Step 13 commit); `make -C atomic bundle test` green |
| 7 | Docs, help router, wiki pages, parent-spec supersessions | `context/commands/atomic-help.md`, `docs/reference/realm-wiki.md`, `docs/reference/commands.md`, `docs/guides/knowledge-base.md`, `docs/guides/install.md`, `docs/wiki/wiki.md`, `docs/wiki/config.md`, `docs/spec/wiki-buckets.md`, `docs/spec/bucket-doc-management.md` | atomic-implementer (mode: feature) | ~9 | Help-router loop prints no `MISSING:`; every feature row in the documentation table found by `grep` in its assigned files; the four superseded parent-spec lines no longer state the old contract and each parent spec has a dated Change log entry with a **Superseded:** line; `npm run docs:build` green; `atomic validate spec` 0 FAIL |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Claude Code reads `.claude/settings.json` only from the primary cwd, so a session started inside a member repo gets no reminder when it edits a realm bucket | high | Accepted scope. The session-start staleness nudge and `atomic wiki stale` still report the drift. `docs/reference/realm-wiki.md` states the realm-root-only scope |
| Edits made through `Bash` (`sed`, heredocs, `mv`) do not match the `PostToolUse` matcher | med | The manifest still records them and `atomic wiki stale` reports `STALE bucket`. The skill's refresh-before-done rule applies whatever tool made the edit |
| `wiki` installing the hook by importing `hooks` creates an import cycle | high | Both packages import the leaf `claudesettings` package; a success criterion forbids `wiki` importing `hooks`; `go build ./...` gates it |
| Moving the SessionStart writer onto `claudesettings` changes session-start install behavior | med | The existing hooks tests must pass unmodified; `IsInstalled` and doctor stay pinned to `SessionStart` |
| The hook runs on every `Edit`/`Write`/`NotebookEdit` in a realm-root session and adds latency | med | No subprocess. It exits early when `cwd` resolves to no realm or the path is outside the realm root, before reading the registry. Any error exits 0 with no output |
| Hook text shaped as an instruction trips prompt-injection defenses | low | The message is a factual statement. The instruction lives in the bucket skill |
| `atomic hooks uninstall --scope realm` is undone by the next `atomic wiki scan` while the realm still has buckets | med | Documented in `docs/reference/realm-wiki.md`. Opting out of the hook while keeping buckets is not supported |
| `atomic wiki scan` re-creates a bucket skill the user deleted on purpose | med | Documented in `docs/reference/realm-wiki.md`: keep the skill file (scan never overwrites it) or unregister the bucket |
| Session markers accumulate under `~/.atomic/` | low | Markers are zero-byte, one per actor per touched bucket |
| Hand-editing `type: Workflow` onto a bucket with existing `NN_x/output/` files shrinks the manifest, so the next diff lists those files as removed | low | Removed files cost nothing to synthesize. Documented in `docs/reference/realm-wiki.md` |


## Change log

<!-- Populated on first amendment after the spec is approved. Do not log drafting/refinement turns. -->

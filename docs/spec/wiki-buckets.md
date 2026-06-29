# Wiki capture buckets


## Goal


Ship zero-dependency bucket tooling for capture folders at the realm root: `atomic wiki bucket add | list | diff | promote` (Go, porting the fingerprint-script semantics); one staleness surface (`atomic wiki stale` reports bucket pending work alongside existing repo/concern staleness); `/refresh-wiki` offers bucket creation once per realm and runs bucket synthesis as a new phase; knowledge pages under `wiki/knowledge/` get the same fingerprint-based staleness story as repo summaries.

Design: `docs/design/wiki-buckets.md`.


## Non-goals


- Wiki/scan refuse guard — ships separately, before this work.
- Auto-retraction of knowledge when a source file is removed — v1 reports `removed`; human/model decides.
- Per-bucket slash commands (`/wiki-from-research` etc.) — synthesis is one generic phase.
- Hardcoded bucket names or per-name behavior — `research`/`raw`/`tickets` are prompt examples only.
- Any relationship to `atomic followups` — buckets are arbitrary user folders with no shared tooling or taxonomy.
- Versioning capture content — buckets are working material; only the wiki repo is versioned.
- Binary-file synthesis — PDFs and images are hashed but cannot be read by the model; v1 relies on the user describing them in the bucket `index.md`.


## Concepts


| Concept | Definition |
|---------|-----------|
| Bucket | User-named folder at the **realm root** (sibling of `wiki/`). Registered, fingerprinted, synthesized. Never nested inside `wiki/`. |
| Manifest | SHA-256 snapshot of bucket content files: `current`, `baseline`, `previous`. Lives at `wiki/.buckets/<name>/`. Baseline = "what the wiki has consumed" (wiki-side state, versioned with the wiki repo). |
| Two-phase contract | `diff` = read-only work list (current vs baseline: new/changed/removed). `promote` = rotate baseline→previous, current→baseline. Run only after successful synthesis; re-running `diff` without `promote` never loses the work list. |
| Registry | Machine: managed `<wiki-buckets>` block in `wiki/index.md` (code-owned, spliced like `<wiki-scan>`). Narrative: `## Capture surfaces` section in realm `CLAUDE.md`, written once on first `bucket add`. |
| Bucket index | `<bucket>/index.md` — purpose line + `## Conventions` block. Load-bearing: the only place a bucket's meaning exists; synthesis reads it as context. |
| Knowledge page | `wiki/knowledge/<topic>.md` — topic-keyed, not bucket-keyed. Multiple buckets' files about the same topic merge into one page. Provenance lives in `sources:` frontmatter (`<bucket>/<file>@<sha256>`), stamped by code. |
| Citation DAG | capture → knowledge → concerns. Concerns may cite `knowledge/<topic>.md@<sha256>` (new resolver id type; example: `knowledge/vendor-x.md@a3f9…`); never bucket files directly. One synthesis boundary, one staleness path per fact. |


## Business rules


- Code computes, writes, and compares every fingerprint. The model only declares which sources and citations apply. Same axiom as `atomic wiki stamp` today (`atomic/internal/wiki/stamp.go:44-56`).
- Baseline advances only on `promote` after successful synthesis. A failed or aborted synthesis leaves the work list intact.
- Manifest content files exclude infrastructure: the bucket's own `index.md` and OS junk (`.DS_Store`, `Thumbs.db`). Manifests are never hand-edited.
- Bucket names are user-defined. The only reserved name is `wiki`.
- The bucket-creation offer fires once per realm: `/refresh-wiki` on a realm with no `<wiki-buckets>` block prompts with `research`/`raw`/`tickets` as examples; a decline is recorded in the block so the offer never re-nags. `atomic wiki bucket add` works any time after.
- `removed` files are reported in the diff and surfaced during synthesis; nothing is auto-deleted from `wiki/knowledge/`.
- Phase order inside `/refresh-wiki` is upstream-before-downstream: repo summaries → bucket synthesis (knowledge) → concern staleness recheck against fresh hashes → concern re-author → stamp-as-written → linkify. One pass converges; no fixpoint loop.
- Bucket synthesis is dispatched to `atomic-wiki-inferrer` in a new bucket-synthesis mode — fresh context per bucket (raw dumps can be large), consistent with repo summaries in wiki mode.


## Success criteria


### `atomic wiki bucket add`

- [ ] `atomic wiki bucket add <name>` registers a new bucket: creates `<realm-root>/<name>/index.md` (stub with purpose line + `## Conventions` placeholder), creates `wiki/.buckets/<name>/` (empty manifest dir), adds a `<bucket name="<name>" path="<abs-path>"/>` entry to the `<wiki-buckets>` block in `wiki/index.md` (spliced idempotently, like `<wiki-scan>`).
- [ ] On the first `bucket add` in a realm (no existing `## Capture surfaces` section in the realm `CLAUDE.md`), writes the `## Capture surfaces` section to realm `CLAUDE.md` listing the bucket path with a `<!-- describe what this bucket is for -->` purpose placeholder per bucket. If realm `CLAUDE.md` does not exist, `bucket add` creates it containing only the section; if it exists, the section is appended and all content outside it is preserved byte-for-byte. Subsequent adds append a bucket bullet to the section rather than overwriting it.
- [ ] If the `<wiki-buckets>` block carries a `declined="true"` attribute (the user previously declined the creation offer), `bucket add` removes the attribute as part of registering the new bucket. The offer does not re-fire after this.
- [ ] Refuses with a non-zero exit and a message if `<name>` equals `wiki` (reserved) or if the bucket is already registered in `<wiki-buckets>`.

### `atomic wiki bucket list`

- [ ] `atomic wiki bucket list [--root=<path>]` prints one line per registered bucket: `<name>  <abs-path>  <baseline-file-count> files  (<pending|fresh>)`, where `pending` means the diff work list is non-empty. When a bucket has no baseline manifest yet (never promoted), the count field prints `(no baseline)` in place of the number and every content file counts as pending.
- [ ] Exits 0 even when no buckets are registered (prints nothing).

### `atomic wiki bucket diff`

- [ ] `atomic wiki bucket diff <name> [--root=<path>]` is read-only. Prints one line per content-file change: `new <relpath>`, `changed <relpath>`, `removed <relpath>`. `new` = present in current, absent in baseline. `changed` = SHA-256 differs. `removed` = absent in current, present in baseline.
- [ ] When baseline is empty (no prior `promote`), all content files are `new`.
- [ ] Exits 0 when the diff is empty (bucket is in sync with baseline); exits 1 when any line is emitted.
- [ ] Content files exclude `index.md` and files matching the OS-junk ignore set (`.DS_Store`, `Thumbs.db`).
- [ ] Every `diff` invocation writes the computed `current` manifest to `wiki/.buckets/<name>/current` on disk. `current` is a debugging artifact only — it is never read as state by `diff`, `promote`, or `stale`; `baseline` and `previous` are the authoritative state.

### `atomic wiki bucket promote`

- [ ] `atomic wiki bucket promote <name> [--root=<path>]` recomputes `current` live (writing it to `wiki/.buckets/<name>/current`), then rotates `baseline`→`previous` and `current`→`baseline`. After promote, `diff` emits nothing (exits 0).
- [ ] `promote` on a bucket with no prior baseline (first run) treats the rotation as a no-op for `previous` and sets `baseline` from `current`. The bucket is marked in-sync after the operation.
- [ ] Refuses with a non-zero exit and a message if the bucket is not registered.

### Manifest format

- [ ] `wiki/.buckets/<name>/current` is a sorted, newline-delimited list of `<relpath>\t<sha256>` lines. `wiki/.buckets/<name>/baseline` and `previous` have the same format. Generated by code from a deterministic walk of `<realm-root>/<name>/`.
- [ ] The manifest walk sorts paths lexicographically for stable output; does not recurse into subdirectories named `.git`, `node_modules`, or other skip-set dirs.

### `atomic wiki stale` extension

- [ ] `atomic wiki stale` (existing verb, `atomic/internal/wiki/stale.go`) additionally reports bucket staleness: one `STALE bucket <name>` line per bucket whose `diff` is non-empty. Existing `DRIFT`/`STALE` lines for repos and concerns are unchanged.
- [ ] Exit code contract is unchanged: exits 1 iff any line (now including `STALE bucket`) is emitted; exits 2 on hard error.

### Knowledge page stamping

- [ ] `atomic wiki stamp --knowledge <path> --sources <bucket>/<file>@<sha256>[,…]` writes/updates the `sources:` YAML list in the knowledge page's frontmatter. Each entry is formatted as `<bucket>/<relpath>@<sha256hex>`. The model supplies which source files contributed; code writes the `sources:` value.
- [ ] For concern staleness, a concern that cites `knowledge/<topic>.md` (wiki-root-relative, including `.md`; example: `knowledge/vendor-x.md@a3f9…`) resolves the fingerprint as the SHA-256 of the knowledge page file itself (content hash), not a git HEAD. `StampConcern` (`atomic/internal/wiki/stamp.go:43`) gains this third id-type branch: if the cited id matches `knowledge/<topic>.md@<sha256>`, compute the file hash. Id format mirrors `SummaryPath` storage (`repos/<name>.md`).
- [ ] `atomic wiki stale` reports `STALE concern <path> (knowledge/<topic>.md)` when the stored hash for a knowledge-page citation diverges from the current file hash.

### `/refresh-wiki` bucket integration

- [ ] On a realm with no `<wiki-buckets>` block, `/refresh-wiki` prompts once (axiom 4 plain-text offer) with `research`/`raw`/`tickets` as examples. User may name buckets to create; a blank/skip input records a `declined="true"` attribute on the `<wiki-buckets>` block. Offer never re-fires when the block already exists (whether populated or declined).
- [ ] After creating buckets from the offer, `/refresh-wiki` instructs the model to fill in the narrative the code stubs cannot write: ask the user (or infer from their offer answers) what each bucket is for, then replace the purpose placeholders in the realm `CLAUDE.md` `## Capture surfaces` section and write the bucket's `index.md` purpose line + `## Conventions` block to match. Code writes structure; the model writes meaning. A bucket left with its placeholder is reported in the disposition output so it isn't silently forgotten.
- [ ] Bucket synthesis phase fires after repo summaries and before concern re-auth: for each bucket with a non-empty diff, dispatch `atomic-wiki-inferrer` in bucket-synthesis mode (fresh context per bucket, bucket `index.md` + changed files as context). Synthesizer writes or updates `wiki/knowledge/<topic>.md` page(s). Code stamps the knowledge pages after synthesis via `atomic wiki stamp --knowledge`.
- [ ] `/refresh-wiki` runs `atomic wiki bucket promote <name>` for each bucket whose synthesis completed successfully. A failed synthesis leaves the bucket un-promoted so the diff persists.
- [ ] Fresh buckets (empty diff) are reported as `SKIPPED (fresh)` in the disposition output, consistent with the existing per-artifact disposition reporting.

### `atomic-wiki-inferrer` bucket-synthesis mode

- [ ] Bucket-synthesis mode is a new caller-context branch in the inferrer. Trigger: `bucket_name` + `bucket_path` + `wiki_dir` all supplied. The inferrer reads the bucket's `index.md` (conventions context) + the files listed in the diff as content context, synthesizes `wiki/knowledge/<topic>.md` page(s), and writes them under `wiki_dir/knowledge/`. It never touches the bucket folder, never writes fingerprints (code stamps after), and never modifies `wiki/index.md`.
- [ ] If `bucket_name` or `bucket_path` is supplied (bucket intent shown) while any of the three args (`bucket_name`, `bucket_path`, `wiki_dir`) is missing, the inferrer fails loud — refuses and names the missing arg(s). `wiki_dir` alone shows no bucket intent (it is shared with wiki-output mode) and never triggers this guard.
- [ ] Default (non-wiki) and wiki-output modes are unchanged. Existing signals tests stay green; reviewer confirms bucket-synthesis branch is additive only.

### Checklist + gates

- [ ] `make render` produces updated `commands/refresh-wiki.md` and `agents/atomic-wiki-inferrer.md`, no orphan; `make render && git diff --exit-code` clean. Note: the render and bundle drift gates are **final** gates (run once at the end, not per-checkpoint); per-checkpoint Verifies reference only the files that checkpoint touches.
- [ ] `make -C atomic bundle && git diff --exit-code` clean.
- [ ] `/atomic-help` discovers the new `atomic wiki bucket` verbs (cli topic) and the bucket-synthesis mode (tour stage if applicable); help-coverage reports no `MISSING:`.
- [ ] `cliusage.go` (`atomic/internal/cliusage/cliusage.go`) has entries for the four user-runnable verbs: `wiki bucket add`, `wiki bucket list`, `wiki bucket diff`, `wiki bucket promote` and their flags; `atomic validate artifacts` passes. `atomic wiki stamp --knowledge` is an internal helper (not user-runnable), consistent with `docs/spec/wiki.md` declaring `stamp` and `mark-dirty` internal — no cliusage entry required.
- [ ] `go test ./...` green; `go vet ./...` clean; `gofmt -l .` empty (from `atomic/`).


## Conversational entry point (`atomic-wiki` skill)


The `atomic-wiki` skill (`skills/atomic-wiki/SKILL.md`) is the conversational routing layer for wiki and bucket operations. It is a bundled auto-firing skill, not a command.

**Trigger surface:** fires on capture-intent phrases ("I want a place for notes/tickets/research", "add a bucket"), karpathy-realm setup ("set up a wiki for my projects"), and wiki query/staleness phrases ("what does my wiki know about X", "is my wiki stale"). The trigger description in the skill frontmatter is the canonical list.

**Realm resolution:** reads the `<wikis>` block in `~/.claude/CLAUDE.md` (CLI-managed, outside `<atomic>`). cwd under a realm root → that realm is active. No registered realm + setup intent → `atomic wiki scan --root <path>` bootstraps.

**Bucket creation contract:** when capture intent is detected and the cwd is under a registered realm, the skill routes to `atomic wiki bucket add <name> --root <realm>` — never a bare `mkdir`. After the binary creates the structure, the skill drives the meaning-fill: asks what the bucket is for, replaces the `<!-- describe what this bucket is for -->` placeholder in realm `CLAUDE.md` `## Capture surfaces`, and writes the bucket `index.md` purpose line + `## Conventions`. Code writes structure; model writes meaning.

**Karpathy-wiki setup:** `atomic wiki scan --root` scaffolds, user names buckets (examples: `research`, `raw`, `tickets`), `atomic wiki bucket add` registers each, then `/refresh-wiki` runs the synthesis pass.

**Query/staleness:** "what does my wiki know" → read `wiki/index.md` + `knowledge/` + `repos/`; "is it stale" → `atomic wiki stale --root` (exit 0 fresh / exit 1 stale with lines / exit 2 error).

**Degradation:** `atomic` binary absent → say so, point at `/refresh-wiki` and `docs/reference/wiki-workflow.md`; never hand-build the structure.

**Relationship to /refresh-wiki:** this skill is the conversational entry point; `/refresh-wiki` is the synthesis engine. The skill is not invoked by other commands; it complements but does not call `/refresh-wiki`.


## Approaches


| # | Approach | Pros | Cons |
|---|----------|------|------|
| A | Status quo: per-workspace zx script + prompt doc | No atomic changes | Runtime dep (bun/zx) per realm; manual setup; conventions drift; script copies fork |
| B | Go verbs (`atomic wiki bucket …`) + inferrer synthesis phase in `/refresh-wiki` | Zero deps; one staleness surface; reuses stamp/stale/registry patterns verbatim; semantics stay user-side | New verb family + agent mode to maintain |
| C | Per-bucket slash commands (`/wiki-from-research` …) | Mirrors original setup | Hardcodes bucket semantics into artifacts; N commands for N buckets; contradicts "no semantics in code" |
| D | Bucket-keyed output (`knowledge/<bucket>/`) | Trivial provenance | Defeats synthesis — same topic split across buckets never merges; wiki becomes a mirror of capture |


## Recommendation


**B**, with topic-keyed knowledge output (rejecting D) and provenance via `sources:` frontmatter. Existing machinery to extend: `StampConcern`/`resolveFingerprint` (`atomic/internal/wiki/stamp.go:43-80`) needs one new id-type branch (knowledge page → content hash); the `<wiki-scan>` splice pattern (`atomic/internal/wiki/wiki.go`) is the registry model to copy for `<wiki-buckets>`; `atomic wiki stale` (`atomic/internal/wiki/stale.go`) already parses `reflects:` lists and extends cleanly for bucket diff checks. The fingerprint script's two-phase promote semantics port 1:1 to Go.

Manifest location: `wiki/.buckets/<name>/` over `<bucket>/.fingerprints/` — baseline answers "what has the wiki consumed", which is wiki state. Storing it in the wiki repo makes the wiki self-describing (clone it, know exactly what it reflects) and keeps capture folders pure content.


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | **Bucket manifest core**: SHA-256 content walk (exclude `index.md` + OS junk + skip-set dirs), sorted `current` generation, `baseline`/`previous` rotation (`promote`), two-phase diff (new/changed/removed), `wiki/.buckets/<name>/` dir scaffolding | `atomic/internal/wiki/bucket*.go` + tests | `go test ./internal/wiki/...`: empty-baseline→all-new; promote→diff-empty; changed file detected; removed file detected; OS junk excluded; `index.md` excluded; `<name>=wiki` refused; double-register refused; promote on unregistered refused |
| 2 | **`atomic wiki bucket` CLI verbs**: `add`, `list`, `diff`, `promote` dispatch; `<wiki-buckets>` block splice in `wiki/index.md` (idempotent, spliced like `<wiki-scan>`); `## Capture surfaces` write to realm `CLAUDE.md` on first `add`; `cliusage.go` entries for all four verbs | `atomic/internal/wiki/bucket*.go`, `atomic/cmd/atomic/main.go`, `atomic/internal/cliusage/cliusage.go` + tests | `go test ./...`: idempotent block splice, CLAUDE.md section written once, `list` pending/fresh status, `diff` exit 0/1, `promote` rotation; `atomic validate artifacts` passes (no unknown verbs/flags) |
| 3 | **`atomic wiki stale` bucket extension**: `Stale` (`atomic/internal/wiki/stale.go`) extended to read `<wiki-buckets>` block and run diff per bucket; emits `STALE bucket <name>` lines; exit-code contract unchanged | `atomic/internal/wiki/stale.go` + tests | `go test ./internal/wiki/...`: fresh bucket→no line emitted; pending bucket→`STALE bucket <name>` line + exit 1; hard error (missing wiki dir)→exit 2; existing DRIFT/STALE repo/concern lines unaffected |
| 4 | **Knowledge page stamping**: `atomic wiki stamp --knowledge <path> --sources <bucket>/<file>@<sha256>[,…]` writes `sources:` frontmatter; `StampConcern` gains knowledge-page id-type branch (content hash; id format `knowledge/<topic>.md@<sha256>`); `atomic wiki stale` reports `STALE concern <path> (knowledge/<topic>.md)` on hash divergence; topic-name validation (kebab-case `[a-z0-9-]+\.md`) | `atomic/internal/wiki/stamp.go`, `atomic/internal/wiki/stale.go` + tests | `go test ./...`: `sources:` written with correct entries; re-stamp is idempotent; knowledge-citation staleness detected on file change; non-conforming topic name skipped with notice; `atomic validate artifacts` passes (stamp is internal — no cliusage entry) |
| 5 | **Inferrer bucket-synthesis mode**: new `bucket_name`/`bucket_path`/`wiki_dir` caller-context branch; reads bucket `index.md` + diff files as context; writes `wiki/knowledge/<topic>.md`; never touches bucket folder; defers stamping to code; fail-loud on partial args | `templates/agents/atomic-wiki-inferrer.md` → render | `make render` no orphan + diff clean; body has bucket-synthesis branch (conventions-context load, writes under `wiki_dir/knowledge/`, skip fingerprinting, fail-loud on missing args); default and wiki modes unchanged; `go test ./...` green |
| 6 | **`/refresh-wiki` bucket integration**: one-time creation offer (with decline recording in `<wiki-buckets>`); bucket synthesis phase after repo summaries; per-bucket inferrer dispatch; `atomic wiki stamp --knowledge` invocation; `atomic wiki bucket promote` on success; `SKIPPED (fresh)` for empty-diff buckets; `.dirty` clear only on full completion; same commit amends `docs/spec/wiki.md`'s `/refresh-wiki` criterion to reference the extended bucket phase order, with a dated `## Change log` entry per `rules/specs/spec-currency.md` (amending wiki.md earlier would make it describe unbuilt behavior; the amendment ships with the behavior change) | `templates/commands/refresh-wiki.md` → render, `docs/spec/wiki.md` | `make render` no orphan + diff clean; body names offer flow (declined attr), bucket-synthesis phase order, inferrer dispatch, stamp invocation, conditional promote, disposition output, conditional `.dirty` clear; manual run shows fresh bucket `SKIPPED (fresh)`; `docs/spec/wiki.md` `/refresh-wiki` criterion references bucket phase and `## Change log` entry present |
| 7 | **Contracts + discovery + docs**: `CLAUDE.md` bucket concept + `<wiki-buckets>` block contract + knowledge-page fingerprint; `/atomic-help` rows for `atomic wiki bucket add/list/diff/promote`; `README.md` and `docs/reference/commands.md`; `docs/reference/wiki-workflow.md` bucket phase | `CLAUDE.md`, `templates/commands/atomic-help.md`→render, `README.md`, `docs/reference/commands.md`, `docs/reference/wiki-workflow.md` | help-coverage no `MISSING:`; grep for `wiki bucket` in help template hits; no live `<wiki-buckets>` paths in `CLAUDE.md`; `npm run docs:build` clean |
| 8 | **Bundle + signals** | `atomic/internal/embedded/**`, `.claude/project/signals*` | `make -C atomic bundle && git diff --exit-code` clean; `/refresh-wiki` → `signals.md` wiki domain updated to include bucket files |


## Deterministic CLI contract


**Manifest walk.** Recurse `<realm-root>/<name>/` (not `wiki/.buckets/`). Skip dirs named in the existing `skipDirs` set (`atomic/internal/wiki/wiki.go:21-30`). Exclude `index.md` at the bucket root and files matching the OS-junk ignore set (`.DS_Store`, `Thumbs.db`). Hash each file with SHA-256. Sort paths lexicographically. Emit `<relpath>\t<sha256hex>` per line.

**Diff logic.** Walk current tree live. Write the result to `wiki/.buckets/<name>/current` on disk (debugging artifact; never read back as state by diff, promote, or stale). Parse `baseline` (empty string if file absent). Classify: present in current, absent in baseline → `new`; present in both, SHA differs → `changed`; absent in current, present in baseline → `removed`. Emit in path-sorted order. `baseline` and `previous` are the authoritative state; `current` is a side-effect artifact only.

**Promote rotation.** Promote recomputes the walk live and writes `current`, then rotates: `baseline`→`previous`, freshly computed manifest→`baseline`. It never reads a previously written `current` file as input. If `baseline` absent (first promote): skip the `previous` write, set `baseline` from the fresh manifest only.

**`<wiki-buckets>` block.** Literal `<wiki-buckets>`/`</wiki-buckets>` boundaries in `wiki/index.md`. One `<bucket name="<name>" path="<abs-path>"/>` per registered bucket. Optional `declined="true"` attribute on the open tag (no children) when the user declined the creation offer. Spliced idempotently (content outside the block untouched), following the `<wiki-scan>` block pattern in `atomic/internal/wiki/wiki.go`.

**`## Capture surfaces` in realm `CLAUDE.md`.** Written once on first `bucket add` — creating the file when it does not exist, appending the section when it does (content outside the section preserved byte-for-byte). Each bucket bullet carries a `<!-- describe what this bucket is for -->` purpose placeholder; code writes the structure, the `/refresh-wiki` offer flow drives the model to replace placeholders with real purpose descriptions (and matching bucket `index.md` conventions). Subsequent adds append a bullet for the new bucket path. The section is never removed by code. The upward CLAUDE.md walk makes it visible to member-repo sessions.

**Knowledge page `sources:` frontmatter.** YAML list key, values `<bucket>/<relpath>@<sha256hex>`. Written by `atomic wiki stamp --knowledge <path> --sources <entries>`. The model supplies which bucket files contributed to the page; code writes every fingerprint value.

**Knowledge-page citation fingerprint.** `StampConcern` (`atomic/internal/wiki/stamp.go:43`) id-type dispatch: if id matches `knowledge/<topic>.md` → SHA-256 of `wiki/knowledge/<topic>.md` file content. Existing `indexed` (signals hash) and `summarized` (git HEAD) branches unchanged. Id format is wiki-root-relative and always includes `.md` (example: `knowledge/vendor-x.md@a3f9…`), mirroring how `SummaryPath` stores `repos/<name>.md`.

**Knowledge topic name safety.** Knowledge topic file names are kebab-case matching `[a-z0-9-]+\.md` (example: `vendor-x.md`, `auth-patterns.md`). The synthesis brief requires `atomic-wiki-inferrer` in bucket-synthesis mode to emit conforming names. `atomic wiki stamp --knowledge` rejects (skips with a notice) any knowledge path whose filename does not match the pattern. Code validates; model conforms.

**Bucket-synthesis mode dispatch guard.** Bucket intent = `bucket_name` or `bucket_path` supplied. With bucket intent, all three of `bucket_name`, `bucket_path`, `wiki_dir` must be present — otherwise the inferrer refuses and names the missing arg(s). Without bucket intent the guard never fires: `wiki_dir` alone is a legitimate wiki-output-mode arg (shared key), and no bucket args at all falls through to existing mode detection.

**`atomic wiki stale` bucket lines.** Literal prefix `STALE bucket <name>`. One line per bucket with a non-empty diff. Emitted after existing `DRIFT`/`STALE` repo/concern lines, before exit-code computation (so exit-1 logic is unchanged: exit 1 iff any line).

**`/refresh-wiki` phase order.** (1) `atomic wiki scan` (membership). (2) `atomic wiki stale` (work list). (3) Repo summaries (pending/stale repos via inferrer wiki mode). (4) Bucket synthesis (non-empty-diff buckets via inferrer bucket-synthesis mode; stamp knowledge pages; promote on success). (5) Concern staleness recheck against fresh hashes. (6) Concern re-author. (7) `atomic wiki stamp` for concerns. (8) `atomic wiki linkify`. (9) Clear `.dirty` + bump `generated` (only on full completion). (10) Commit offer.


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Large raw-dump buckets slow synthesis | med | Fresh context per bucket (not per file); `removed` files excluded from context; binary files skipped (user-described in `index.md`) |
| `promote` run before synthesis completes | low | `/refresh-wiki` only promotes on per-bucket synthesis success; `promote` is user-runnable (explicit command, axiom 3) |
| Knowledge topic name collisions across buckets | low | Topic-keyed output is intentional — same topic in two buckets merges into one page; provenance tracked in `sources:` |
| `wiki/.buckets/` accidentally committed | low | `wiki/.gitignore` (scaffolded in wiki CP1) already gitignores `.dirty`; extend it to NOT ignore `.buckets/` — manifests are wiki state, should be versioned |
| `<wiki-buckets>` block clobbered by `atomic claude update` | low | Block is outside `<atomic>` in `wiki/index.md` (not in `CLAUDE.md`), so `atomic claude update`'s block-replace logic does not touch it |
| Realm `CLAUDE.md` write crosses git boundary | low | Walk stops at the realm root by design; the write targets the realm-root `CLAUDE.md`, not any member-repo file |
| `declined` attribute prevents legitimate future use | low | `atomic wiki bucket add` still works after decline — it adds the bucket and removes the `declined` attribute |


## Change log


### 2026-06-12 — Correction: bucket dispatch guard scoped to bucket intent

**What changed:** The bucket-synthesis dispatch guard fired on "any one of the three args present" — but `wiki_dir` is shared with wiki-output mode, so a legitimate wiki-output dispatch (`target_repo` + `wiki_dir`) would have tripped the bucket guard. Guard rewritten: it fires only when `bucket_name` or `bucket_path` shows bucket intent; `wiki_dir` alone never triggers it.

**Why:** Correction — CP5 code reviewer caught the conflict between the guard as written and the existing wiki-output mode contract.

**Superseded:** "If any of the three are missing while any one is present, refuse."

### 2026-06-12 — Correction: promote recomputes the walk live

**What changed:** The Deterministic CLI contract's Promote rotation paragraph said `cp current baseline` (reading the previously written `current` file). Rewritten: promote recomputes the manifest walk live, writes `current`, then rotates `baseline`→`previous` and fresh manifest→`baseline`; a previously written `current` is never read as input.

**Why:** Correction — internal spec conflict caught by the CP1 code reviewer: the `bucket promote` success criterion already said "recomputes `current` live" while the contract paragraph said copy-on-disk. Recompute-live matches the reference fingerprint script (snapshot always precedes rotation within one invocation) and removes the stale-`current` hazard.

**Superseded:** "Promote rotation = `cp baseline previous; cp current baseline`."

### 2026-06-12 — Adding behavior: `atomic-wiki` conversational entry point skill

**What changed:** Added the `atomic-wiki` bundled skill (`skills/atomic-wiki/SKILL.md`) as the conversational routing layer for wiki and bucket operations. The skill fires on capture-intent phrases, karpathy-realm setup intent, and wiki query/staleness phrases. It routes bucket creation to `atomic wiki bucket add` (never bare mkdir), handles realm resolution via the `<wikis>` block, drives meaning-fill after the binary scaffolds, and routes staleness queries to `atomic wiki stale`. A new `## Conversational entry point` section was added to this spec documenting the skill's contract.

**Why:** Capture-intent phrases ("I want a space for tickets") had no deterministic route to `atomic wiki bucket add`. Without the skill the harness would mkdir a bare folder, bypassing the manifest system. Skills are the harness's intent-routing mechanism; the description is the trigger surface.

## Implementation log


Delivered 2026-06-12 on branch `wiki-buckets` (autopilot run; 8 checkpoints + 1 verification fix round):

- `57cab82` — CP1 manifest core (walk/diff/promote + spec/design committed)
- `d92e72b` — CP2 CLI verbs + `<wiki-buckets>` registry + `## Capture surfaces`
- `02fadc0` — CP3 `atomic wiki stale` bucket lines
- `ac252db` — CP4 knowledge stamping + citation staleness
- `32f6ad6` — CP5 inferrer bucket-synthesis mode
- `d7b9100` — CP6 `/refresh-wiki` offer + synthesis phase; `docs/spec/wiki.md` amended
- `16362b8` — CP7 CLAUDE.md contract, help router, README, reference docs
- `e435fea` — CP8 signals refresh
- `4460f66` — e2e verification fixes: space-form `--root` + unknown-arg refusal, line-anchored `## Capture surfaces` matching with EOF fallback, list status field on never-promoted buckets

**End-to-end verification:** full Go suite green (3 pre-existing `internal/hooks` failures are a filed environment gap, not this feature); `go vet` + `gofmt` clean; render + bundle parity clean; `atomic validate` 0 FAIL; `/atomic-help` MISSING-scan zero; VitePress docs build clean; live binary smoke in a temp realm (HOME redirected): scan → bucket add → diff exit 1 → list `(no baseline) (pending)` → `STALE bucket` → promote → diff exit 0 → list `1 files (fresh)` → stale exit 0; no writes outside the realm.

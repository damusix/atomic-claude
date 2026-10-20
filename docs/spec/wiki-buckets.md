# Wiki capture buckets


## Goal


Zero-dependency bucket tooling for capture folders at the realm root: `atomic wiki bucket add | list` register and enumerate buckets; `/refresh-wiki` offers bucket creation once per realm; the `atomic-wiki` skill routes capture intent to `bucket add`. A bucket's per-folder doc index (`docs/spec/bucket-doc-management.md`) is the only output the wiki derives from it.

Design: `docs/design/wiki-buckets.md`.


## Non-goals


- Wiki/scan refuse guard — ships separately.
- Per-bucket slash commands (`/wiki-from-research` etc.).
- Hardcoded bucket names or per-name behavior. `research`, `notes`, and `tickets` are prompt examples only.
- Any relationship to `atomic followups` — buckets are arbitrary user folders with no shared tooling or taxonomy.
- Versioning capture content — buckets are working material; only the wiki repo is versioned.
- Any model pass over bucket content, and any fingerprint, manifest, or freshness state for a bucket.


## Concepts


| Concept | Definition |
|---------|-----------|
| Bucket | User-named folder at the **realm root** (sibling of `wiki/`). Registered and indexed. Never nested inside `wiki/`. |
| Registry | Machine: managed `<wiki-buckets>` block in `wiki/index.md` (code-owned, spliced like `<wiki-scan>`). Narrative: `## Capture surfaces` section in realm `CLAUDE.md`, written on first `bucket add`. |
| Bucket index | `<bucket>/index.md` — purpose line + `## Conventions` block, plus the code-generated `<bucket-docs>` listing. The only place a bucket's meaning exists. |


## Business rules


- Bucket names are user-defined. The only reserved name is `wiki`.
- The bucket-creation offer fires once per realm: `/refresh-wiki` on a realm with no `<wiki-buckets>` block prompts with `research`/`notes`/`tickets` as examples; a decline is recorded in the block so the offer never re-nags. `atomic wiki bucket add` works any time after.
- Code writes structure (folder, index stub, registry entry, `## Capture surfaces` bullet); the model writes meaning (purpose line, conventions).


## Success criteria


### `atomic wiki bucket add`

- [ ] `atomic wiki bucket add <name>` registers a new bucket: creates `<realm-root>/<name>/` and its `index.md` (stub with purpose line + `## Conventions` placeholder, left untouched when one exists), and adds a `<bucket name="<name>" path="<abs-path>"/>` entry to the `<wiki-buckets>` block in `wiki/index.md` (spliced idempotently, like `<wiki-scan>`). It creates nothing under `wiki/` besides that entry. It then rebuilds the bucket's indexes, so every folder the walk reaches holds an `index.md` (`docs/spec/bucket-icm-structure.md` criterion 9).
- [ ] On the first `bucket add` in a realm (no existing `## Capture surfaces` section in the realm `CLAUDE.md`), writes the `## Capture surfaces` section to realm `CLAUDE.md` listing the bucket path with a `<!-- describe what this bucket is for -->` purpose placeholder per bucket. If realm `CLAUDE.md` does not exist, `bucket add` creates it containing only the section; if it exists, the section is appended and all content outside it is preserved byte-for-byte. Subsequent adds append a bucket bullet to the section rather than overwriting it.
- [ ] If the `<wiki-buckets>` block carries a `declined="true"` attribute (the user previously declined the creation offer), `bucket add` removes the attribute as part of registering the new bucket. The offer does not re-fire after this.
- [ ] Refuses with exit 1 and a message if `<name>` fails validation (`docs/spec/wiki-bucket-arg-hardening.md`), equals `wiki` (reserved), or is already registered in the `<wiki-buckets>` block.

### `atomic wiki bucket list`

- [ ] `atomic wiki bucket list [--root=<path>]` prints one `<name>\t<abs-path>` line per registered bucket, read from the `<wiki-buckets>` block.
- [ ] Exits 0 even when no buckets are registered (prints nothing).

### `/refresh-wiki` bucket integration

- [ ] On a realm with no `<wiki-buckets>` block, `/refresh-wiki` prompts once (axiom 4 plain-text offer) with `research`/`notes`/`tickets` as examples. User may name buckets to create; a blank/skip input records a `declined="true"` attribute on the `<wiki-buckets>` block. Offer never re-fires when the block already exists (whether populated or declined).
- [ ] After creating buckets from the offer, `/refresh-wiki` instructs the model to fill in the narrative the code stubs cannot write: ask the user (or infer from their offer answers) what each bucket is for, then replace the purpose placeholders in the realm `CLAUDE.md` `## Capture surfaces` section and write the bucket's `index.md` purpose line + `## Conventions` block to match. A bucket left with its placeholder is reported in the disposition output as `PLACEHOLDER (unfilled)` so it isn't silently forgotten.
- [ ] `/refresh-wiki` runs no inference pass over bucket content.

### Checklist + gates

- [ ] `/atomic-help` discovers the `atomic wiki bucket` verbs (cli topic); help-coverage reports no `MISSING:`.
- [ ] `cliusage.go` (`atomic/internal/cliusage/cliusage.go`) has entries for `wiki bucket add` and `wiki bucket list` and their flags; `atomic validate artifacts` passes.
- [ ] `go test ./...` green; `go vet ./...` clean; `gofmt -l .` empty (from `atomic/`).


## Conversational entry point (`atomic-wiki` skill)


The `atomic-wiki` skill (`skills/atomic-wiki/SKILL.md`) is the conversational routing layer for wiki and bucket operations. It is a bundled auto-firing skill, not a command.

**Trigger surface:** fires on capture-intent phrases ("I want a place for notes/tickets/research", "add a bucket"), karpathy-realm setup ("set up a wiki for my projects"), and wiki query/staleness phrases ("what does my wiki know about X", "is my wiki stale"). The trigger description in the skill frontmatter is the canonical list.

**Realm resolution:** reads the `<wikis>` block in `~/.claude/CLAUDE.md` (CLI-managed, outside `<atomic>`). cwd under a realm root → that realm is active. No registered realm + setup intent → `atomic wiki scan --root <path>` bootstraps.

**Bucket creation contract:** when capture intent is detected and the cwd is under a registered realm, the skill routes to `atomic wiki bucket add <name> --root <realm>` — never a bare `mkdir`. After the binary creates the structure, the skill drives the meaning-fill: asks what the bucket is for, replaces the `<!-- describe what this bucket is for -->` placeholder in realm `CLAUDE.md` `## Capture surfaces`, and writes the bucket `index.md` purpose line + `## Conventions`. Code writes structure; model writes meaning.

**Karpathy-wiki setup:** `atomic wiki scan --root` scaffolds, user names buckets (examples: `research`, `notes`, `tickets`), `atomic wiki bucket add` registers each, then `/refresh-wiki` writes the repo summaries and concerns.

**Query/staleness:** "what does my wiki know" → read `wiki/index.md`, `repos/`, `concerns/`, and each registered bucket's `index.md`; "is it stale" → `atomic wiki stale --root`, which reports membership drift only (exit 0 none / exit 1 `DRIFT` lines / exit 2 error).

**Relationship to /refresh-wiki:** this skill is the conversational entry point; `/refresh-wiki` writes the repo summaries and concerns. The skill is not invoked by other commands; it complements but does not call `/refresh-wiki`.


## Approach


Go verbs under `atomic wiki bucket`, with the registry spliced into `wiki/index.md` following the `<wiki-scan>` block pattern in `atomic/internal/wiki/wiki.go`, and bucket semantics kept user-side in each bucket's `index.md`.


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 2 | **`atomic wiki bucket` CLI verbs**: `add`, `list` dispatch; `<wiki-buckets>` block splice in `wiki/index.md` (idempotent, spliced like `<wiki-scan>`); `## Capture surfaces` write to realm `CLAUDE.md` on first `add`; `cliusage.go` entries | `atomic/internal/wiki/bucket*.go`, `atomic/internal/wiki/action.go`, `atomic/internal/cliusage/cliusage.go` + tests | `go test ./...`: idempotent block splice, CLAUDE.md section written once, `list` prints `name<TAB>path`, `<name>=wiki` refused, double-register refused; `atomic validate artifacts` passes |
| 6 | **`/refresh-wiki` bucket offer**: one-time creation offer with decline recording in `<wiki-buckets>`; placeholder fill; `PLACEHOLDER (unfilled)` disposition | `context/commands/refresh-wiki.md` | body names the offer flow (declined attr), placeholder fill, and disposition output |
| 7 | **Contracts + discovery + docs**: `/atomic-help` rows for `atomic wiki bucket add/list`; `README.md`, `docs/reference/commands.md`, `docs/reference/realm-wiki.md` | `context/commands/atomic-help.md`, `README.md`, `docs/reference/*.md` | help-coverage no `MISSING:`; `npm run docs:build` clean |


## Deterministic CLI contract


**`<wiki-buckets>` block.** Literal `<wiki-buckets>`/`</wiki-buckets>` boundaries in `wiki/index.md`. One `<bucket name="<name>" path="<abs-path>"/>` per registered bucket. Optional `declined="true"` attribute on the open tag (no children) when the user declined the creation offer. Spliced idempotently (content outside the block untouched), following the `<wiki-scan>` block pattern in `atomic/internal/wiki/wiki.go`. The block is the authority for "is this bucket registered".

**`## Capture surfaces` in realm `CLAUDE.md`.** Written once on first `bucket add` — creating the file when it does not exist, appending the section when it does (content outside the section preserved byte-for-byte). Each bucket bullet carries a `<!-- describe what this bucket is for -->` purpose placeholder; code writes the structure, the `/refresh-wiki` offer flow drives the model to replace placeholders with real purpose descriptions (and matching bucket `index.md` conventions). Subsequent adds append a bullet for the new bucket path. The section is never removed by code. The upward CLAUDE.md walk makes it visible to member-repo sessions.


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|------------|
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

### 2026-07-24 — Adding behavior: bucket doc management (child spec)

**What changed:** Added `docs/spec/bucket-doc-management.md` as a child spec of this one, covering: a six-key bucket-doc frontmatter contract (`title`, `type`, `description`, `tags`, `status`, `created`); a two-level deterministic index (a `<bucket-docs>` region per bucket `index.md`, a `<wiki-bucket-list>` region in the realm `wiki/index.md`), both spliced through one shared managed-region primitive; and two scaffold verbs, `atomic wiki bucket doc <bucket> <slug> [--router]` and `atomic wiki bucket skill <bucket>`, plus `atomic wiki bucket index [<bucket>]` to rebuild the index regions on demand. `atomic wiki scan` now rebuilds both index levels as part of its existing pass. The `## Members` region in `wiki/index.md` was also migrated from the legacy `<!-- wiki-members:start/end -->` comment markers to the same XML managed-region form, for one delimiter syntax across the file.

**Why:** Bucket content (`wiki/knowledge/`) had no bucket-facing index of its own — a bucket's docs were discoverable only by listing the directory. This child spec makes the frontmatter contract and the two listing regions cheap to produce and keep current, without touching manifest or staleness granularity (both unchanged, per the child spec's non-goals).

### 2026-10-18 — Removed: bucket manifests, synthesis, and staleness

**What changed:** Body rewritten to the surviving surface: `bucket add` (no manifest directory; duplicate check reads the `<wiki-buckets>` block) and `bucket list` (`name<TAB>path`), the one-time offer, and the `atomic-wiki` skill. Approaches and Recommendation collapsed to a one-line `## Approach`; checkpoints reduced to the surviving work, keeping their numbers so the implementation log still matches.

**Why:** Realm wikis no longer need refreshing; every mechanism whose job was to track or force a refresh is deleted.

**Removed:** `bucket diff`, `bucket promote`, the SHA-256 manifests under the wiki, `STALE bucket` lines, knowledge pages and their `sources:` stamping, knowledge-page concern citations, the inferrer bucket-synthesis mode, and the `/refresh-wiki` synthesis phase order. See `docs/spec/realm-wiki-drop-refresh.md`.

### 2026-10-18 — Bucket name examples: `research`, `notes`, `tickets`

**What changed:** The offer and setup examples name `research`, `notes`, and `tickets`.

**Why:** Raw material now lives in each research item's `01_raw/` stage, so a separate `raw` bucket is no longer the example to give; `notes` takes its place. `tickets` stays an example. See `docs/spec/bucket-icm-structure.md`.

**Superseded:** examples `research`, `raw`, `tickets`.

### 2026-10-18 — `bucket add` rebuilds the bucket's indexes

**What changed:** The `bucket add` criterion now includes the index rebuild that gives every walked folder of the new bucket an `index.md`.

**Why:** Audit found the criterion listed the stub, registry, and capture-surfaces writes but not the rebuild, which `docs/spec/bucket-icm-structure.md` criterion 9 requires.

**Correction:** The criterion omitted the index rebuild that `bucket add` performs.

### 2026-10-19 — Artifacts assume atomic is installed

**What changed:** The `atomic-wiki` skill section no longer has a degradation rule for a missing binary.

**Why:** Issue #298: artifacts ship inside and install through the `atomic` binary, so the binary is always present when they run. See `docs/spec/artifacts-mermaid-flows.md`.

**Superseded:** with the binary absent, the skill said so and pointed at `/refresh-wiki` and `docs/reference/realm-wiki.md`.


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

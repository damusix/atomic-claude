# Spec: install workflow (Claude steering projection and CLAUDE.md merge)


The authored global Atomic contract is `context/AGENTS.md`. The Claude adapter renders it directly into `~/.claude/CLAUDE.md`: there is no user-level `AGENTS.md` artifact and no global loader. The multi-harness architecture that produces this projection is [`omp-plugin-compatibility.md`](./omp-plugin-compatibility.md); this spec covers the Claude install surface that carries it and the one merge case still left to the model.

`atomic claude install` and `atomic claude update` write the embedded artifact bundle mechanically and treat the global steering file as block-aware: the `<atomic>...</atomic>` block is Atomic-owned, everything outside it is user-owned. The install/update orchestration and the sibling verbs (`list`, `diff`, `uninstall`) live in [`atomic-binary.md`](./atomic-binary.md); this spec owns the `CLAUDE.md` comparison, the proposed-file fallback, and the cold-op merge brief that resolves it.


## Global steering projection


`context/AGENTS.md` is the sole authored global Atomic contract. The manifest maps it to the Claude target `CLAUDE.md`, so the adapter renders the contract's own bytes straight into the harness's user-level file — no importer file at user scope and no path referring to one.

The rendered document carries exactly one `<atomic>...</atomic>` block. That block is Atomic-owned; user prose around it is out of scope, and a difference outside the block never registers as drift for install, update, or `diff`.


## Artifact bundle install (block-aware `CLAUDE.md`)


`atomic claude install` and `atomic claude update` plan each embedded artifact against its on-disk target, applying any configured agent `model`/`effort` overrides first so the planned bytes match what is written. Every artifact except `CLAUDE.md` is compared and written whole, and a changed file is backed up under `~/.atomic/backups/<timestamp>/` before the write. `CLAUDE.md` takes one of two paths.


### Block path


When the embedded source and the on-disk file each carry exactly one parseable `<atomic>` block — line-anchored tags, with no missing, unclosed, or duplicate tag:

- Equal blocks → `unchanged`. No write, no proposed file, and user content outside the block does not register as drift.
- Different blocks → back up the whole file under `~/.atomic/backups/<timestamp>/CLAUDE.md`, then splice the embedded block over the on-disk block byte-for-byte, preserving every byte outside it. Reported as `block replaced`.

User edits inside the block are overwritten; the backup is the recovery path. Atomic-owned content is a versioned contract, and silently preserving divergent edits inside it would leave the user running a patched version they cannot diff against upstream.


### No parseable block


When the on-disk `CLAUDE.md` has no parseable block — a pre-tag install, unclosed or duplicate tags — code cannot draw the ownership boundary safely, so the binary writes the embedded content to `~/.atomic/proposed/CLAUDE.md` and reports the target as `merge required (proposed at <path>)`. It never overwrites the live file.

The install summary prints the next step: in a Claude Code session, run `atomic prompt claude-merge` to merge the config. The binary never spawns Claude and never merges on its own — the user runs the merge on their own schedule.

A first-time install, with no on-disk `CLAUDE.md` at all, writes the embedded content directly: no proposed file, no merge step.


## Cold-op merge brief (`atomic prompt claude-merge`)


`atomic prompt` emits built-in cold-op briefs to stdout, one per name (`git-cleanup`, `claude-merge`, `implementer`, `reviewer`). `atomic prompt claude-merge` emits the brief that merges `~/.atomic/proposed/CLAUDE.md` into `~/.claude/CLAUDE.md`; the user pastes it into a Claude Code session, which runs it as a generic subagent task.

The brief's contract:

- Replace the `<atomic>...</atomic>` block with the proposed block verbatim; Atomic always wins inside its boundary.
- Preserve everything outside the block byte-for-byte, and preserve any `<wikis>...</wikis>` block at its original position.
- Write the result to `~/.claude/CLAUDE.md.atomic-merged` and never modify the live file. The dispatcher — the user's interactive session — presents the report, requires explicit acceptance, and applies the staging file only on accept.
- Report the block action, the preserved user content, and the apply command.

The brief is a built-in asset of the binary, so the merge surface ships in the same embedded bundle as the rest of the install. The former merge slash command and its dedicated agent were removed; the cold-op is now a binary-emitted prompt.


## Scope steering loader pairs


Repository and nested-realm Claude steering is a loader pair: authored guidance in `AGENTS.md` plus an adjacent thin `CLAUDE.md` whose managed-block body is `@AGENTS.md`. The import sits as a top-level markdown paragraph bracketed by blank lines so Claude's memory parser resolves it; an import placed immediately inside the block's tags would be swallowed by the HTML block those tags open and deliver nothing.

Both files carry managed blocks: the owned block is spliced into the observed bytes and unowned prose survives byte-for-byte. A scope whose directory resolves — symlinks included — to the Claude native root is refused, because the global projection owns that file and a second loader there would deliver the contract twice.


## Milestone A adoption


Under Milestone A a legacy Claude install converges through the generic adoption engine rather than through the `claude` verb: `atomic install --harness claude` and `atomic harness adopt|repair` acquire the single lifecycle lock, recover unresolved journals oldest-first, batch older-version replace-or-leave-unowned decisions, preserve the write-once legacy pre-install snapshot, and import `profile.md`/`wikis.md` into `~/.atomic` once while the authority is absent. Canonical contract: [`omp-plugin-compatibility.md`](./omp-plugin-compatibility.md).


## Success criteria


- A first-time install on a machine with no `~/.claude/CLAUDE.md` writes the rendered global contract directly; no proposed file and no merge step.
- `atomic claude update` against a `CLAUDE.md` carrying exactly one parseable `<atomic>` block updates the block in place, or no-ops when the blocks are equal; no proposed file is written and user content outside the block survives byte-for-byte.
- `atomic claude update` against a `CLAUDE.md` with no parseable block writes `~/.atomic/proposed/CLAUDE.md` and prints the `atomic prompt claude-merge` step; the live file is never overwritten.
- `atomic prompt claude-merge` prints the merge brief to stdout; a session following it stages `~/.claude/CLAUDE.md.atomic-merged` and overwrites the live file only after explicit user acceptance.
- A scope loader pair delivers its guidance exactly once: `AGENTS.md` carries the guidance block, `CLAUDE.md` carries only the `@AGENTS.md` import. A scope resolving to the Claude native root is refused.


## Checkpoints


Checkpoints I-1 through I-3 record the removed global-steering merge command and its dedicated agent. The deterministic `<atomic>` block path replaced them, except when the on-disk file has no parseable block, where the binary-emitted cold-op brief applies. The rows stay as the build record; do not treat them as work to do. See the 2026-09-19 change-log entry.


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| I-1 | Global-steering merge agent (removed) | merge agent source (removed) | |
| I-2 | Global-steering merge slash command (removed) | merge command source (removed) | |
| I-3 | Both artifacts wired into the embedded bundle manifest in the Go binary (so `atomic claude install` ships them) | `atomic/internal/embedded/` | |
| I-4 | `CLAUDE.md` + `CLAUDE.md` + `README.md` updated to mention the install workflow | `CLAUDE.md`, `README.md` | |


## Implementation log


### v0.1.0 — 2026-05-17


Built across 3 implementer iterations plus a docs/bundle catch-up on branch `install-workflow`. Commits (chronological):

- `3977030` — CP-1 + CP-2: `atomic-claude-merger` agent + `/atomic-claude-merge` command
- `7e084ac` — CP-3: regenerate embedded bundle manifest for the two new artifacts
- `030d7c4` — polish: add `.atomic-merged` existence guard, print-before-run reminders at each destructive callsite, tighten taxonomy `conflict` row precondition
- `c387e49` — CP-4: docs sync (README install paragraph, CLAUDE.md commands list, this log)
- `21d1074` — bundle payload catch-up: commit the actual file copies under `atomic/internal/embedded/bundle/` for the two new artifacts and the refreshed CLAUDE.md

**Out-of-scope work performed during this build:**

- None. CP-4 docs sync happened inside this loop rather than via a separate `/documentation` run because the changes are narrow (two paragraphs + one list entry).

**Unforeseens — surprises that emerged during implementation:**

- The bundle-mirror code already had no allowlist for `commands/*.md` and matched `agents/atomic-*.md` by prefix, so CP-3 reduced to running `go generate ./...` — no Go source edits needed.
- The bundle manifest tracks SHA256 of each bundled file, so the polish-pass markdown edits required a manifest regenerate to keep CI's `git diff --exit-code` gate green.
- The `.claude/project/inferred-signals.md` snapshot claimed the `embedded/bundle/` directory is gitignored. It is not — both the manifest snapshot AND the actual file payloads under `bundle/` are tracked. Final commit (`21d1074`) catches up the payloads. Worth refreshing inferred-signals after this branch lands.

**Deferred items still open:**

- CP-4 — sync `CLAUDE.md` / `CLAUDE.md` / `README.md` to mention the install merge workflow. Handled out-of-band via `/documentation` (next step after this log lands).
- F-4 (extra `## Workflow` section in agent body) and F-5 (sha256 short-circuit folded into Pre-flight instead of `### Refusals`) — user dropped at FOLLOWUPS triage. Cosmetic only.
- Spec's `## Open follow-ups` carry-over: `atomic claude rollback` verb, `--strategy ours/theirs/manual` flag on the merge command, revisit the 10% conflict heuristic. All explicitly v0.2.0+ scope.


## Change log


### 2026-07-16 — User state root relocated to ~/.atomic

**What changed:** Every body mention of `~/.claude/.atomic/proposed/CLAUDE.md` and `~/.claude/.atomic/backups/<ts>/` now reads `~/.atomic/proposed/CLAUDE.md` and `~/.atomic/backups/<ts>/`.

**Why:** `docs/spec/configurable-state-paths.md` (issue #150) relocates the user state root.

**Superseded:** Prior body named `~/.claude/.atomic/` as the proposed-merge and backup root throughout (itself a 2026-05-21 consolidation from the even older `.atomic-proposed`/`.atomic-backups` naming — see that entry below).

### 2026-05-17 — Conform to validator rules

**What changed:** Migrated `## Checkpoints` table to the canonical 4-column header `| # | Checkpoint | Files/areas | Verifies |` — existing rows preserved; `Files/areas` backfilled from checkpoint descriptions; `Verifies` left blank. Added `## Change log` section (was missing).

**Why:** `atomic validate spec` rule S5 and S6 flagged the file when the validator landed (CP-5 of `atomic-validate`).

**Squashed onto `main` as `e6cf258` — 2026-05-17.** Per-iteration SHAs above are historical (unreachable post-squash).


### 2026-05-21 — Migrate divergence paths under `.atomic/`

**What changed:** Body references updated: proposed merge target is now `~/.claude/.atomic/proposed/CLAUDE.md` (was `~/.claude/CLAUDE.md.atomic-proposed`), and the backup root is `~/.claude/.atomic/backups/<ts>/` (was `~/.claude/.atomic-backups/<ts>/`). The merge artifact (`CLAUDE.md.atomic-merged`) is unchanged. `atomic-claude-merger` agent and `/atomic-claude-merge` command updated in lockstep.

**Why:** `docs/spec/atomic-state-and-config.md` consolidates all atomic-owned per-user state under `~/.claude/.atomic/`. Scattered legacy paths (`.atomic-proposed`, `.atomic-backups/`) gave `atomic doctor` three separate cleanup targets and made every new piece of state another top-level entry under `~/.claude/`.

**Superseded:** prior contract wrote merge proposal to `~/.claude/CLAUDE.md.atomic-proposed` and backups to `~/.claude/.atomic-backups/<ts>/`. Both still exist on installed machines that ran older `atomic` binaries; cleanup is the user's responsibility (no migration code).


**This branch (atomic-state-and-config) squashed onto `main` as `5c9d61c` — 2026-05-21.** Change log entry above amended via squash.


### 2026-06-10 — Deterministic `<atomic>` block replacement; LLM merge becomes migration-only

**What changed:** `atomic claude install/update` now compares and replaces the `<atomic>...</atomic>` block in `~/.claude/CLAUDE.md` deterministically (`claudeinstall` block parser, `ActionBlockReplaced`). The proposed-file + `/atomic-claude-merge` flow fires only when the on-disk file has no parseable block. `claudeinstall.Diff` (and therefore doctor check 1) treats a merged file with a current block as `match`. Intro, Trigger path 1, and success criteria rewritten accordingly.

**Why:** after a completed merge, user content outside the block made every whole-file SHA compare report drift — doctor flagged `drifted: CLAUDE.md` permanently and every update forced a slow LLM merge for a boundary code can draw itself (code-over-model principle).

**Superseded:** prior contract: the binary never merges CLAUDE.md; any difference produced a proposed file requiring `/atomic-claude-merge`.


### 2026-09-02 — Retired section titles in the merge taxonomy

**What changed:** the atomic-known title list now names `## Shell tools for repetitive edits` in place of `## Bash over Read+Write`, and the section taxonomy gained a **Retired titles** paragraph mapping a renamed section's old title to its replacement, classifying the old title as atomic-owned so the migration path drops it.

**Why:** `## Bash over Read+Write` was renamed in `context/CLAUDE.md`. Without a mapping, a legacy file with no `<atomic>` block classifies the old title as user-only and preserves it, leaving the user two sections of contradictory editing guidance.

**Superseded:** the atomic-known list was a flat set of current titles with no notion of a former one, so a rename silently converted the old section into user-only content.

- 2026-09-06 — **Change:** the global contract was condensed and several `<atomic>` sections were renamed or folded. Retired titles for the migration path: `## Shell tools for repetitive edits` and `## ast-grep over regex grep` → `## Editing and searching files`; `## Workflow (canonical lifecycle)` → `## Workflow`; `## Inter-session messaging`, `## Persistent REPL sessions`, `## Code-intel engine`, and `## Atomic binary subcommands` → `## Atomic binary`; `## Specs` folded into the `## Where things live` table. Files carrying a parseable `<atomic>` block are unaffected.


### 2026-09-19 — Global steering projects from `context/AGENTS.md`; merge taxonomy retired

**What changed:** The body now states the current install and merge contract. The authored global contract is `context/AGENTS.md`, rendered directly into the harness's user-level `CLAUDE.md` by the Claude adapter, with no user-level importer artifact and no global loader. `atomic claude install`/`update` keep the mechanical bundle write and the block-aware path: one parseable `<atomic>` block → compare and splice in place after backing up the whole file; equal blocks → `unchanged`; no parseable block → write `~/.atomic/proposed/CLAUDE.md` and print the cold-op step. The cold-op is the built-in brief emitted by `atomic prompt claude-merge`. Repository and realm steering is a loader pair — authored `AGENTS.md` plus a thin adjacent `CLAUDE.md` whose managed body is `@AGENTS.md`, with a scope resolving to the native root refused. Milestone A adoption is a one-line delta pointing at `omp-plugin-compatibility.md`. Removed the merge-taxonomy prose (section taxonomy, Atomic-known title list, retired-title map, 10%-byte conflict heuristic, report format), the artifacts-to-build section, and the command/agent sections; rephrased the three Checkpoints rows that named the removed artifacts.

**Why:** the deterministic `<atomic>` block path replaced the whole-file model merge — it is kept only for files with no parseable block, where code cannot draw the ownership boundary — and Milestone A moved authored global steering to `context/AGENTS.md`. A fresh reader of the old body would have built a removed slash command, a removed agent, and a superseded section-classification heuristic.

**Superseded:** prior contract defined a `/atomic-claude-merge` slash command plus a dedicated merger agent, a `##`-section taxonomy with an Atomic-known-title list and a retired-title map, a 10% byte-difference conflict heuristic, and a report format, triggered whenever install/update could not update the block in place. It also described global steering as authored `context/CLAUDE.md` and listed the two artifacts to build.


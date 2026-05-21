# Spec: install workflow (CLAUDE.md merge)


The `atomic claude install` / `atomic claude update` binary commands handle file writes mechanically: copy embedded atomic-prefixed artifacts into `~/.claude/`, back up replaced files. They do *not* try to merge `~/.claude/CLAUDE.md` because that file is user-owned and may contain personal customization the binary cannot safely reconcile.


Instead, the binary writes the new version to `~/.claude/.atomic/proposed/CLAUDE.md` and defers the merge to a Claude Agent. This spec defines that merge surface: a slash command (`/atomic-claude-merge`), a subagent (`atomic-claude-merger`), and the conventions they follow.


This spec depends on [`atomic-binary.md`](./atomic-binary.md) for the install/update orchestration and the proposed-file convention.


## Artifacts to build


| Artifact | Type | Path (in this repo, before install) |
|----------|------|------|
| `/atomic-claude-merge` | command | `commands/atomic-claude-merge.md` |
| `atomic-claude-merger` | agent | `agents/atomic-claude-merger.md` |


Both ship in the embedded bundle via `atomic claude install`. They live at `~/.claude/commands/atomic-claude-merge.md` and `~/.claude/agents/atomic-claude-merger.md` after install.


## Trigger


User-initiated, always. Two paths:


1. **After install/update**. `atomic claude install` or `atomic claude update` writes `~/.claude/.atomic/proposed/CLAUDE.md` and prints `run /atomic-claude-merge inside any Claude Code session when ready`. The user runs the slash command when they decide it's the right time — minutes later, the next day, or never.
2. **Ad-hoc**. User types `/atomic-claude-merge` directly in any Claude Code session. Covers re-runs after aborting a prior merge, or running merges out of band.


The binary never spawns Claude. The destructive-confirm guard (axiom 3) is inside this slash command (step 3 of the flow below — `AskUserQuestion` Accept / Show diff / Open editor / Abort).


## `/atomic-claude-merge` command


### Pre-flight


1. Check `~/.claude/.atomic/proposed/CLAUDE.md` exists. If not, print `nothing to merge. ~/.claude/.atomic/proposed/CLAUDE.md not found.` and exit.
2. Check `~/.claude/CLAUDE.md` exists. If not, this is a first-time install case the binary handled directly — print `~/.claude/CLAUDE.md is missing. moving proposed file into place.` Run `mv ~/.claude/.atomic/proposed/CLAUDE.md ~/.claude/CLAUDE.md` and exit.


### Flow


1. Dispatch the `atomic-claude-merger` agent with prompt:

    > Read `~/.claude/CLAUDE.md` (the user's current global) and `~/.claude/.atomic/proposed/CLAUDE.md` (the new atomic-claude version). Produce a merged version that (a) preserves every user customization that does not directly conflict with the proposed atomic sections, (b) updates atomic-owned sections to match the proposed version, (c) adds new atomic sections from the proposed file. Write the merged result to `~/.claude/CLAUDE.md.atomic-merged`. Do not modify `~/.claude/CLAUDE.md` directly. Report which sections you preserved, replaced, added, and any conflicts you flagged.

2. After the agent returns, present the user a side-by-side diff: `diff ~/.claude/CLAUDE.md ~/.claude/CLAUDE.md.atomic-merged`.
3. Ask via `AskUserQuestion`:

    | Option | Effect |
    |--------|--------|
    | Accept | `mv ~/.claude/CLAUDE.md.atomic-merged ~/.claude/CLAUDE.md`; `rm ~/.claude/.atomic/proposed/CLAUDE.md` |
    | Show diff again | Re-print the diff, re-ask |
    | Open editor | `$EDITOR ~/.claude/CLAUDE.md.atomic-merged` then re-ask |
    | Abort | Leave all three files in place (`CLAUDE.md`, `.atomic/proposed/CLAUDE.md`, `.atomic-merged`). The user can sort it out manually. |

4. On Accept, back up the prior `CLAUDE.md` to `~/.claude/.atomic/backups/<accept-timestamp>/CLAUDE.md` using a fresh ISO timestamp generated at accept time (not the binary's install-run timestamp — the install may have happened days ago, and we want the backup timestamp to reflect when the user actually authorized the overwrite). Create the `.atomic/backups/` dir if it does not exist.
5. Report final state.


### Refusals


- Both files identical (sha256 match) → print `no changes needed.` and remove `.atomic/proposed/CLAUDE.md`. Skip the agent.


## `atomic-claude-merger` agent


### Frontmatter


```yaml
---
name: atomic-claude-merger
description: Merges the user's current ~/.claude/CLAUDE.md with the proposed ~/.claude/.atomic/proposed/CLAUDE.md produced by `atomic claude install/update`. Preserves user customizations, replaces atomic-owned sections, flags conflicts. Read/Write/Edit scoped to ~/.claude/.
tools: Read, Write, Edit
model: sonnet
---
```


### Inputs


- `~/.claude/CLAUDE.md` — the user's current global. May contain atomic sections from a prior install plus user additions.
- `~/.claude/.atomic/proposed/CLAUDE.md` — the new atomic version, fresh from the embedded bundle.


### Output


- `~/.claude/CLAUDE.md.atomic-merged` — the proposed merged result.
- A structured report (in the agent's final message back to the orchestrator) of what was preserved / replaced / added / flagged.


### Section taxonomy


A CLAUDE.md is a markdown doc with `##` top-level sections. The merger classifies each section as one of:


| Class | How to detect | How to merge |
|-------|---------------|--------------|
| **atomic-owned** | Section title appears in the proposed file with identical heading, OR section title matches an atomic-known list (`## Principles`, `## Bash over Read+Write`, `## Design axioms`, `## Where things live`, `## Subagents available for dispatch`, `## Workflow (canonical lifecycle)`) | Replace with the proposed version. |
| **user-only** | Section title appears in current but not proposed, and is not in the atomic-known list | Preserve verbatim. |
| **atomic-new** | Section title appears in proposed but not current | Append to merged output in the order the proposed file dictates. |
| **conflict** | Same section title in both, but the user has clearly edited inside an atomic-owned section (heuristic: more than 10% of non-whitespace bytes differ from the prior atomic baseline) | Flag in the report. Default action: use proposed, but note the override. The user can inspect and revert via the Open editor path. |


The merger does NOT need the prior atomic baseline to detect conflict perfectly — it can use a simpler heuristic: if a section is atomic-owned and the on-disk version differs from the proposed version, assume the user edited and flag it. The user makes the final call via the slash command's accept/edit/abort prompt.


### Rules


- Never modify `~/.claude/CLAUDE.md` directly. Output goes to `~/.claude/CLAUDE.md.atomic-merged`.
- Preserve heading order from the proposed file where possible. User-only sections retain their original relative position when they sit between recognizable anchor sections; otherwise they go at the end.
- Preserve the exact whitespace and code-block formatting of preserved sections. The atomic style of "double newline after headings" applies to the merged output as a whole, but do not reflow individual user sections.
- Frontmatter (if any) follows the proposed version.
- Output is plain markdown. No prose hedging. No "I merged this for you" preambles in the file itself.


### Report format


```
Sections preserved (user-only):
  - ## My personal aliases
  - ## Project-specific overrides for repo X

Sections replaced (atomic-owned, in-sync):
  - ## Principles
  - ## Bash over Read+Write
  - ## Design axioms

Sections added (atomic-new):
  - ## Self-update guidance

Conflicts flagged:
  - ## Where things live — user version differs from atomic baseline. Used proposed. Diff:
      <unified diff snippet>
```


## Open follow-ups


- A future `atomic claude rollback` verb could restore the most recent backup automatically. Out of scope for v0.1.0; user runs `cp` manually.
- The merger's conflict heuristic (10% byte change) is rough. Revisit if false positives are common.
- For users who keep extensive customization in `~/.claude/CLAUDE.md`, consider a future `--strategy ours/theirs/manual` flag on `/atomic-claude-merge` for batch acceptance. Not in v0.1.0.


## Success criteria


- Running `atomic claude install` for the first time on a machine with no `~/.claude/CLAUDE.md` writes the embedded version directly; `/atomic-claude-merge` is unnecessary.
- Running `atomic claude update` after a user has edited `~/.claude/CLAUDE.md` produces `.atomic/proposed/CLAUDE.md`; `/atomic-claude-merge` produces `.atomic-merged` that preserves the user's custom sections verbatim and replaces atomic-owned sections.
- The user can Open editor → tweak the merged file → Accept; the accepted file becomes `~/.claude/CLAUDE.md`.
- Abort leaves all three files in place; nothing is destroyed.
- A second run of `/atomic-claude-merge` when no `.atomic/proposed/CLAUDE.md` exists exits cleanly with `nothing to merge`.


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| I-1 | `atomic-claude-merger` agent | `agents/atomic-claude-merger.md` | |
| I-2 | `/atomic-claude-merge` command | `commands/atomic-claude-merge.md` | |
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


### 2026-05-17 — Conform to validator rules

**What changed:** Migrated `## Checkpoints` table to the canonical 4-column header `| # | Checkpoint | Files/areas | Verifies |` — existing rows preserved; `Files/areas` backfilled from checkpoint descriptions; `Verifies` left blank. Added `## Change log` section (was missing).

**Why:** `atomic validate spec` rule S5 and S6 flagged the file when the validator landed (CP-5 of `atomic-validate`).

**Squashed onto `main` as `e6cf258` — 2026-05-17.** Per-iteration SHAs above are historical (unreachable post-squash).


### 2026-05-21 — Migrate divergence paths under `.atomic/`

**What changed:** Body references updated: proposed merge target is now `~/.claude/.atomic/proposed/CLAUDE.md` (was `~/.claude/CLAUDE.md.atomic-proposed`), and the backup root is `~/.claude/.atomic/backups/<ts>/` (was `~/.claude/.atomic-backups/<ts>/`). The merge artifact (`CLAUDE.md.atomic-merged`) is unchanged. `atomic-claude-merger` agent and `/atomic-claude-merge` command updated in lockstep.

**Why:** `docs/spec/atomic-state-and-config.md` consolidates all atomic-owned per-user state under `~/.claude/.atomic/`. Scattered legacy paths (`.atomic-proposed`, `.atomic-backups/`) gave `atomic doctor` three separate cleanup targets and made every new piece of state another top-level entry under `~/.claude/`.

**Superseded:** prior contract wrote merge proposal to `~/.claude/CLAUDE.md.atomic-proposed` and backups to `~/.claude/.atomic-backups/<ts>/`. Both still exist on installed machines that ran older `atomic` binaries; cleanup is the user's responsibility (no migration code).


**This branch (atomic-state-and-config) squashed onto `main` as `5c9d61c` — 2026-05-21.** Change log entry above amended via squash.

# Spec: install workflow (CLAUDE.md merge)


The `atomic claude install` / `atomic claude update` binary commands handle file writes mechanically: copy embedded atomic-prefixed artifacts into `~/.claude/`, back up replaced files. They do *not* try to merge `~/.claude/CLAUDE.md` because that file is user-owned and may contain personal customization the binary cannot safely reconcile.


Instead, the binary writes the new version to `~/.claude/CLAUDE.md.atomic-proposed` and defers the merge to a Claude Agent. This spec defines that merge surface: a slash command (`/atomic-claude-merge`), a subagent (`atomic-claude-merger`), and the conventions they follow.


This spec depends on [`atomic-binary.md`](./atomic-binary.md) for the install/update orchestration and the proposed-file convention.


## Artifacts to build


| Artifact | Type | Path (in this repo, before install) |
|----------|------|------|
| `/atomic-claude-merge` | command | `commands/atomic-claude-merge.md` |
| `atomic-claude-merger` | agent | `agents/atomic-claude-merger.md` |


Both ship in the embedded bundle via `atomic claude install`. They live at `~/.claude/commands/atomic-claude-merge.md` and `~/.claude/agents/atomic-claude-merger.md` after install.


## Trigger


User-initiated, always. Two paths:


1. **After install/update**. `atomic claude install` or `atomic claude update` writes `~/.claude/CLAUDE.md.atomic-proposed` and prints `run /atomic-claude-merge inside any Claude Code session when ready`. The user runs the slash command when they decide it's the right time — minutes later, the next day, or never.
2. **Ad-hoc**. User types `/atomic-claude-merge` directly in any Claude Code session. Covers re-runs after aborting a prior merge, or running merges out of band.


The binary never spawns Claude. The destructive-confirm guard (axiom 3) is inside this slash command (step 3 of the flow below — `AskUserQuestion` Accept / Show diff / Open editor / Abort).


## `/atomic-claude-merge` command


### Pre-flight


1. Check `~/.claude/CLAUDE.md.atomic-proposed` exists. If not, print `nothing to merge. ~/.claude/CLAUDE.md.atomic-proposed not found.` and exit.
2. Check `~/.claude/CLAUDE.md` exists. If not, this is a first-time install case the binary handled directly — print `~/.claude/CLAUDE.md is missing. moving proposed file into place.` Run `mv ~/.claude/CLAUDE.md.atomic-proposed ~/.claude/CLAUDE.md` and exit.


### Flow


1. Dispatch the `atomic-claude-merger` agent with prompt:

    > Read `~/.claude/CLAUDE.md` (the user's current global) and `~/.claude/CLAUDE.md.atomic-proposed` (the new atomic-claude version). Produce a merged version that (a) preserves every user customization that does not directly conflict with the proposed atomic sections, (b) updates atomic-owned sections to match the proposed version, (c) adds new atomic sections from the proposed file. Write the merged result to `~/.claude/CLAUDE.md.atomic-merged`. Do not modify `~/.claude/CLAUDE.md` directly. Report which sections you preserved, replaced, added, and any conflicts you flagged.

2. After the agent returns, present the user a side-by-side diff: `diff ~/.claude/CLAUDE.md ~/.claude/CLAUDE.md.atomic-merged`.
3. Ask via `AskUserQuestion`:

    | Option | Effect |
    |--------|--------|
    | Accept | `mv ~/.claude/CLAUDE.md.atomic-merged ~/.claude/CLAUDE.md`; `rm ~/.claude/CLAUDE.md.atomic-proposed` |
    | Show diff again | Re-print the diff, re-ask |
    | Open editor | `$EDITOR ~/.claude/CLAUDE.md.atomic-merged` then re-ask |
    | Abort | Leave all three files in place (`CLAUDE.md`, `.atomic-proposed`, `.atomic-merged`). The user can sort it out manually. |

4. On Accept, back up the prior `CLAUDE.md` to `~/.claude/.atomic-backups/<accept-timestamp>/CLAUDE.md` using a fresh ISO timestamp generated at accept time (not the binary's install-run timestamp — the install may have happened days ago, and we want the backup timestamp to reflect when the user actually authorized the overwrite). Create the `.atomic-backups/` dir if it does not exist.
5. Report final state.


### Refusals


- Both files identical (sha256 match) → print `no changes needed.` and remove `.atomic-proposed`. Skip the agent.


## `atomic-claude-merger` agent


### Frontmatter


```yaml
---
name: atomic-claude-merger
description: Merges the user's current ~/.claude/CLAUDE.md with the proposed ~/.claude/CLAUDE.md.atomic-proposed produced by `atomic claude install/update`. Preserves user customizations, replaces atomic-owned sections, flags conflicts. Read/Write/Edit scoped to ~/.claude/.
tools: Read, Write, Edit
model: sonnet
---
```


### Inputs


- `~/.claude/CLAUDE.md` — the user's current global. May contain atomic sections from a prior install plus user additions.
- `~/.claude/CLAUDE.md.atomic-proposed` — the new atomic version, fresh from the embedded bundle.


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
- Running `atomic claude update` after a user has edited `~/.claude/CLAUDE.md` produces `.atomic-proposed`; `/atomic-claude-merge` produces `.atomic-merged` that preserves the user's custom sections verbatim and replaces atomic-owned sections.
- The user can Open editor → tweak the merged file → Accept; the accepted file becomes `~/.claude/CLAUDE.md`.
- Abort leaves all three files in place; nothing is destroyed.
- A second run of `/atomic-claude-merge` when no `.atomic-proposed` exists exits cleanly with `nothing to merge`.


## Checkpoints


| CP | Lands |
|----|-------|
| I-1 | `atomic-claude-merger` agent |
| I-2 | `/atomic-claude-merge` command |
| I-3 | Both artifacts wired into the embedded bundle manifest in the Go binary (so `atomic claude install` ships them) |
| I-4 | `claude.md` + `CLAUDE.md` + `README.md` updated to mention the install workflow |

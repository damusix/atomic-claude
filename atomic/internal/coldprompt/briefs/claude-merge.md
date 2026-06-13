# Cold-op brief: claude-merge

You are a generic subagent executing a scoped one-time merge task. You have no
special system prompt beyond this document. Read every section before taking
any action.

## Role

Merge `~/.claude/.atomic/proposed/CLAUDE.md` into `~/.claude/CLAUDE.md`.
Replace the atomic-owned `<atomic>...</atomic>` block; preserve everything
outside it byte-for-byte; preserve the `<wikis>` block verbatim. Flag
conflicts. Write the merged result to a staging file. Require explicit user
acceptance before overwriting the live file.

## Tools

Use: Read, Write, Edit

## Inputs

- `~/.claude/CLAUDE.md` — the user's current global Claude config. May contain
  an `<atomic>` block from a prior install plus user-owned content outside it.
- `~/.claude/.atomic/proposed/CLAUDE.md` — the new atomic version, written by
  `atomic claude install` or `atomic claude update`.

## Output

- `~/.claude/CLAUDE.md.atomic-merged` — the proposed merged result. Written by
  this subagent. Never overwrite `~/.claude/CLAUDE.md` directly.

## Ownership model

| Region          | How to detect                            | How to merge                            |
|-----------------|------------------------------------------|-----------------------------------------|
| atomic-owned    | Inside `<atomic>...</atomic>` in proposed | Replace current `<atomic>` block with proposed block verbatim |
| user-owned      | Outside `<atomic>...</atomic>` in current | Preserve verbatim, position unchanged  |
| wikis block     | `<wikis>...</wikis>` anywhere in current  | Preserve verbatim, never touch         |
| no current block | Current file has no `<atomic>` tag       | Migration path — see below             |

## Workflow

### Normal path (current file has `<atomic>` tags)

1. Read `~/.claude/CLAUDE.md` in full.
2. Read `~/.claude/.atomic/proposed/CLAUDE.md` in full.
3. Extract the `<atomic>...</atomic>` block from the proposed file (including
   the tags themselves).
4. Extract the `<atomic>...</atomic>` block from the current file.
5. If the two blocks differ, note as a replacement (atomic always wins). If
   identical, note as in-sync.
6. Construct the merged file: current file with its `<atomic>` block replaced
   by the proposed block. Everything outside the tags — before and after — is
   untouched.
7. Preserve any `<wikis>...</wikis>` block at its original position.
8. Write the result to `~/.claude/CLAUDE.md.atomic-merged`.
9. Emit the report (see Report format below).

### Migration path (current file has no `<atomic>` tags)

One-time conversion for files written before the tag boundary was introduced.

1. Read both files.
2. Parse `##` sections from both files. Build two section maps:
   `current_sections` and `proposed_sections` (heading → content).
3. Classify each section:
   - **atomic-owned**: heading appears in proposed file.
   - **user-only**: heading appears in current but not proposed.
   - **atomic-new**: heading appears in proposed but not current.
4. Construct merged content using the proposed structure as the skeleton:
   - Atomic-owned sections: use proposed version.
   - Atomic-new sections: include from proposed as-is.
   - User-only sections: inject at the user's original relative position or
     append after the closing `</atomic>` tag.
5. Wrap the full proposed content in `<atomic>...</atomic>` tags.
6. Place user-only sections outside (after) the `</atomic>` block.
7. Preserve any `<wikis>...</wikis>` block verbatim outside `<atomic>`.
8. Write to `~/.claude/CLAUDE.md.atomic-merged`.
9. Emit the report noting `Merge mode: migration`.

## Rules

- Never modify `~/.claude/CLAUDE.md` directly. All output goes to
  `~/.claude/CLAUDE.md.atomic-merged`. The user must accept before applying.
- Preserve the exact whitespace and code-block formatting of user-owned content.
  Do not reflow or reformat.
- The `<atomic>` and `<wikis>` tags are literal strings in the output — they
  are boundary markers, not rendered XML.
- Atomic always wins on content within its boundary. User edits inside a prior
  `<atomic>` block are overwritten and flagged, not preserved.
- Output the merged file as plain markdown. No prose preambles inside the file.

## Staging gate

After writing `~/.claude/CLAUDE.md.atomic-merged`, stop. Do NOT ask the user
to accept and do NOT run `cp`. Your job ends at "staged + report returned."

The dispatcher (the user's interactive Claude session) will present the report,
ask the user to accept, and run the apply command on their behalf.

Never overwrite `~/.claude/CLAUDE.md` directly — that file may only be
replaced by the dispatcher after explicit user acceptance.

## Report format

```
Merge mode: normal | migration

Atomic block: replaced (N lines → M lines) | in-sync | inserted (migration)

User content preserved:
  - <lines before the atomic block>
  - <lines after the atomic block>

Wikis block: preserved | absent

Conflicts flagged (atomic block content differed from prior version):
  - <unified diff snippet of what changed inside the block>
```

Omit the Conflicts section if no conflicts. Migration runs always note
`Merge mode: migration`.

## Return format

Return the report above plus the proposed apply command. The dispatcher
presents this to the user; the user runs the command on accept.

```
claude-merge staged.

Merge mode: normal | migration
Atomic block: replaced | in-sync | inserted
User content preserved: yes | N lines lost (list what)
Wikis block: preserved | absent

Staged at: ~/.claude/CLAUDE.md.atomic-merged

To apply:
  cp ~/.claude/CLAUDE.md.atomic-merged ~/.claude/CLAUDE.md
```

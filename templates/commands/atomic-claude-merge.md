---
description: Merge ~/.claude/.atomic/proposed/CLAUDE.md into ~/.claude/CLAUDE.md using the atomic-claude-merger agent. Preserves user customizations, replaces atomic-owned sections. Requires explicit accept before overwriting.
---

You orchestrate the CLAUDE.md merge. The `atomic-claude-merger` subagent does the merge work. You present the diff. The user decides. You execute only on Accept.

<workflow>

## Pre-flight

**Check 1 — proposed file exists:**

```bash
test -f ~/.claude/.atomic/proposed/CLAUDE.md
```

If missing: print `nothing to merge. ~/.claude/.atomic/proposed/CLAUDE.md not found.` and stop.

**Check 2 — current CLAUDE.md exists:**

```bash
test -f ~/.claude/CLAUDE.md
```

If missing (first-time install case the binary handles directly), print:

```
~/.claude/CLAUDE.md is missing. moving proposed file into place.
```

Print this command before running it:

```bash
mv ~/.claude/.atomic/proposed/CLAUDE.md ~/.claude/CLAUDE.md
```

Print `done.` and stop.

**Check 3 — sha256 short-circuit:**

Compute sha256 of both files:

```bash
shasum -a 256 ~/.claude/CLAUDE.md ~/.claude/.atomic/proposed/CLAUDE.md
```

If the digests are identical: print `no changes needed.` then print this command before running it:

```bash
rm ~/.claude/.atomic/proposed/CLAUDE.md
```

Print `removed ~/.claude/.atomic/proposed/CLAUDE.md` and stop.

## Step 1 — Dispatch merger agent

Dispatch via the `Agent` tool with `subagent_type: "atomic-claude-merger"`.

Prompt:

> Read `~/.claude/CLAUDE.md` (the user's current global) and `~/.claude/.atomic/proposed/CLAUDE.md` (the new atomic-claude version). Produce a merged version that (a) preserves every user customization that does not directly conflict with the proposed atomic sections, (b) updates atomic-owned sections to match the proposed version, (c) adds new atomic sections from the proposed file. Write the merged result to `~/.claude/CLAUDE.md.atomic-merged`. Do not modify `~/.claude/CLAUDE.md` directly. Report which sections you preserved, replaced, added, and any conflicts you flagged.

Wait for the agent to return. Print the agent's report.

## Step 2 — Show diff

```bash
diff ~/.claude/CLAUDE.md ~/.claude/CLAUDE.md.atomic-merged
```

Print the diff output in full.

## Step 3 — Ask user

Prompt via `AskUserQuestion`:

```
Question: Review the diff above. How would you like to proceed?
Options:
  - Accept — apply the merged file as ~/.claude/CLAUDE.md
  - Show diff again — re-print the diff, re-ask
  - Open editor — edit ~/.claude/CLAUDE.md.atomic-merged before deciding
  - Abort — leave all three files in place, do nothing
```

### Accept

Before proceeding, verify the merged file exists:

```bash
test -f ~/.claude/CLAUDE.md.atomic-merged
```

If that check fails (exit non-zero): print `merge produced no output file. Aborting; ~/.claude/CLAUDE.md unchanged.` and stop. Do not run any of the commands below.

Generate a fresh ISO timestamp at accept time:

```bash
TIMESTAMP=$(date -u +"%Y-%m-%dT%H-%M-%SZ")
```

Print each command before running it. Back up the current CLAUDE.md:

```bash
mkdir -p ~/.claude/.atomic/backups/${TIMESTAMP}
cp ~/.claude/CLAUDE.md ~/.claude/.atomic/backups/${TIMESTAMP}/CLAUDE.md
```

Apply the merged file:

```bash
mv ~/.claude/CLAUDE.md.atomic-merged ~/.claude/CLAUDE.md
```

Remove the proposed file:

```bash
rm ~/.claude/.atomic/proposed/CLAUDE.md
```

Continue to Step 4 (final report).

### Show diff again

Re-run:

```bash
diff ~/.claude/CLAUDE.md ~/.claude/CLAUDE.md.atomic-merged
```

Print output. Re-prompt Step 3.

### Open editor

```bash
${EDITOR:-vi} ~/.claude/CLAUDE.md.atomic-merged
```

After editor exits, re-prompt Step 3 (the user may Accept, Show diff, Open editor again, or Abort).

### Abort

Print:

```
aborted. files left in place:
  ~/.claude/CLAUDE.md              — unchanged original
  ~/.claude/.atomic/proposed/CLAUDE.md — the update (binary wrote this)
  ~/.claude/CLAUDE.md.atomic-merged   — the proposed merge (agent wrote this)

To apply manually: cp ~/.claude/CLAUDE.md.atomic-merged ~/.claude/CLAUDE.md
```

Stop. Do not modify any file.

## Step 4 — Final report

```
Done.
  backed up: ~/.claude/.atomic/backups/<TIMESTAMP>/CLAUDE.md
  applied:   ~/.claude/CLAUDE.md
  removed:   ~/.claude/.atomic/proposed/CLAUDE.md
```

</workflow>

<constraints>

## Rules

- Print every shell command before running it.
- Never overwrite `~/.claude/CLAUDE.md` without the Accept step and the backup.
- The backup timestamp is generated at accept time — not earlier. The install may have run days ago.
- If `$EDITOR` is unset, fall back to `vi`.
- No commits. No git operations. Local files only.

</constraints>

# Uninstall workflow


## Goal

Users can cleanly reverse `atomic claude install` — restoring their pre-atomic `~/.claude/` state with LLM-mediated intelligence for files modified post-install.


## Non-goals

- Binary self-removal (print instruction instead).
- Project-level artifact removal (`.claude/project/` signals, followups).
- Backwards-compat with installs predating the snapshot feature.
- Pure-CLI uninstall without LLM mediation.


## Success criteria

- [ ] `atomic claude install` writes `~/.claude/.atomic/pre-install/` on first install containing: every file it will touch (CLAUDE.md, settings.json, agents/, commands/, skills/, output-styles/, rules/) plus a `manifest.json` recording paths, SHA256s, and timestamps.
- [ ] Subsequent `atomic claude install` / `atomic claude update` calls do NOT overwrite `pre-install/` if it already exists.
- [ ] `atomic claude uninstall` CLI subcommand exists and outputs a structured LLM prompt.
- [ ] Prompt instructs Claude to restore pre-install files, delete atomic-only artifacts, LLM-merge settings.json/CLAUDE.md, remove `~/.claude/.atomic/`, and print binary removal instruction.
- [ ] CLI exits 1 with clear error when `pre-install/manifest.json` is missing.
- [ ] CLI detects TTY and prints human-readable hint when run outside a Claude session.


## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Pre-install snapshot on first install | `atomic/internal/claudeinstall/snapshot.go`, `install.go`, `config/paths.go`, tests | Test: first install creates `pre-install/` with manifest.json + file copies; second install skips; settings.json captured when present |
| 2 | `atomic claude uninstall` CLI subcommand | `atomic/cmd/atomic/main.go`, `atomic/internal/claudeinstall/uninstall.go`, tests | Test: outputs correct prompt given a manifest; exits 1 when no pre-install; TTY hint when interactive |
| 3 | Cross-reference wiring | `CLAUDE.md`, `README.md`, `docs/guides/install.md` | All surfaces reference the new subcommand |


## Design

### Pre-install snapshot

**Location:** `~/.claude/.atomic/pre-install/`

**When written:** During `Install()` in `claudeinstall`, before `Apply()` runs. Guarded by: `if pre-install/ dir exists → skip`. Write-once.

**What's captured:**

| Source | Snapshot path | Notes |
|--------|--------------|-------|
| `~/.claude/CLAUDE.md` | `pre-install/CLAUDE.md` | May not exist (fresh install) |
| `~/.claude/settings.json` | `pre-install/settings.json` | May not exist |
| `~/.claude/agents/*.md` | `pre-install/agents/*.md` | Only files whose names match manifest targets |
| `~/.claude/commands/*.md` | `pre-install/commands/*.md` | Same |
| `~/.claude/skills/*/SKILL.md` | `pre-install/skills/*/SKILL.md` | Same |
| `~/.claude/output-styles/*.md` | `pre-install/output-styles/*.md` | Same |
| `~/.claude/rules/**/*.md` | `pre-install/rules/**/*.md` | Same |

**`manifest.json` schema:**

```json
{
    "created": "2026-05-24T12:00:00Z",
    "atomic_version": "1.5.1",
    "files": [
        {
            "path": "CLAUDE.md",
            "sha256": "abc123...",
            "existed": true
        },
        {
            "path": "agents/atomic-builder.md",
            "sha256": "",
            "existed": false
        }
    ]
}
```

`existed: false` means "this path didn't exist before atomic — uninstall should delete, not restore."

### Uninstall command

**Trigger:** `atomic claude uninstall` (CLI subcommand, not a slash command).

**No agent definition.** The CLI bakes the LLM prompt into the binary. User either runs `atomic claude uninstall` from within a Claude session (Claude executes it and receives the prompt as stdout), or runs it in terminal and pastes the output into Claude.

**CLI responsibilities (deterministic):**

1. Read `~/.claude/.atomic/pre-install/manifest.json`. If missing → exit 1 with "no pre-install snapshot found."
2. Compute the restore plan from manifest:
   - `existed=true` → file to restore (source: `pre-install/<path>`)
   - `existed=false` → file to delete
3. Identify which files need LLM mediation:
   - `settings.json`: if current SHA differs from BOTH pre-install AND embedded → needs merge
   - `CLAUDE.md`: if current SHA differs from pre-install → needs merge
4. Output a structured prompt to stdout that tells Claude exactly what to do.

**CLI output (the prompt Claude receives):**

```markdown
## Atomic Claude Uninstall

Run these steps in order. Confirm the plan with the user before executing.

### Plan

Restore from pre-install:
- ~/.claude/settings.json (NEEDS MERGE — user modified post-install)
- ~/.claude/agents/my-custom-agent.md

Delete (no pre-install counterpart):
- ~/.claude/agents/atomic-builder.md
- ~/.claude/agents/atomic-reviewer.md
- [... all atomic-managed artifacts ...]

Remove directory:
- ~/.claude/.atomic/

### Instructions

1. Show this plan to the user. Get one confirmation before proceeding.
2. For files marked "NEEDS MERGE":
   - Read the current file and the pre-install snapshot at ~/.claude/.atomic/pre-install/<path>
   - Identify what the user added post-install (permissions, MCP servers, env vars, custom sections)
   - Write a merged result: pre-install base + user additions, minus atomic hook/config entries
   - Show the diff to the user before writing
3. For files marked "Restore": copy from ~/.claude/.atomic/pre-install/<path>
4. For files marked "Delete": rm the file
5. rm -rf ~/.claude/.atomic/
6. Print: "Uninstall complete. Binary still at <path>. Run: rm <path>"
```

**TTY detection:** If stdout is a TTY (user ran it in their terminal, not through Claude), print a human-readable hint above the prompt: "Run this inside a Claude Code session, or ask Claude to run `atomic claude uninstall`."

### Confirmation UX

One confirmation at the start showing the full plan. Then execute. No per-item prompts.


## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| User modified an atomic-managed file post-install (e.g. added custom agent named `atomic-builder.md`) | Low | Manifest records SHA256; if current differs from both pre-install and embedded, warn + ask |
| `settings.json` has complex nested structure that LLM misreads | Medium | Show unified diff to user; require explicit confirm; keep a `.atomic-uninstall-backup` as safety net before final write |
| User runs uninstall, regrets it, wants to re-install | Low | Binary still exists; `atomic claude install` works fresh. Pre-install/ is gone but that's fine — next install creates a new one |


## Implementation log

### v1.6.0 — 2026-05-24

Built across 4 iterations of /subagent-implementation. Commits (chronological):

- `c4a8740` — CP-1 Pre-install snapshot (snapshot.go + paths.go + install wiring + 4 tests)
- `bd77e8e` — CP-2 `atomic claude uninstall` CLI subcommand (uninstall.go + main.go wiring + 9 tests + three-way merge detection fix)
- `495cbff` — CP-3 Cross-reference wiring (CLAUDE.md, README.md, docs/guides/install.md)
- `dc00d13` — Polish pass (6 follow-up fixes: test coverage gaps, misleading comments, documentation)

**Out-of-scope work performed during this build:**
- README before/after example, start-here gradient, merge callout, credits fix, repo topics — done before implementation started as part of the review/polish that led to this feature.

**Unforeseens:**
- Reviewer caught that merge detection needed three-way comparison (current vs pre-install vs embedded), not two-way. Spec was correct; initial implementation underspecified the logic. Fixed in iteration 3.

**Deferred items still open:**
- None — all 6 follow-ups fixed in polish pass.

**Squashed to `7a68621` — 2026-05-24.** Per-iteration SHAs above are historical (unreachable from any branch).


## Change log

<!-- Populated on first amendment after approval. -->

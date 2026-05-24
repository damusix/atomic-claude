# Uninstall workflow


## Problem

Users need a clean exit path. If someone installs atomic-claude and decides it's not for them, they currently have to manually identify and remove every file atomic touched. There's no record of what the pre-atomic state was, and settings.json may have been modified both by atomic (hook registration) and by the user (permissions, MCP servers) since install time.

A credible uninstall story is also table-stakes for Reddit adoption — "how do I remove this?" is always the first question.


## Goals / Non-goals

Goals:

- `atomic claude install` captures a complete pre-install snapshot of everything it will touch.
- A slash command (`/atomic-uninstall`) restores the user's pre-atomic state, mediated by the LLM for files the user modified post-install.
- Overridden user artifacts (agents, skills, commands, output-styles, rules that shared names with atomic's) are restored from the snapshot.
- The binary itself is NOT auto-removed (useful standalone for `atomic signals scan`). Print `rm` instruction.

Non-goals:

- Windows support.
- Backwards-compat with installs that predate the snapshot feature (this has never shipped).
- Uninstall via pure CLI (the LLM mediates settings.json; pure-CLI would require a merge algorithm).
- Removing project-level artifacts (`.claude/project/signals.md`, etc.) — those are project-scoped, not user-global.


## Current state

`atomic claude install` currently:

1. Reads embedded manifest (agents, commands, skills, output-styles, rules, CLAUDE.md).
2. For each artifact: if on-disk matches → skip; if CLAUDE.md differs → write proposed; if other artifact differs → backup to `~/.claude/.atomic/backups/<ts>/` + overwrite.
3. Registers SessionStart hook in `~/.claude/settings.json` via `atomic hooks install`.
4. Creates `~/.claude/.atomic/config.resolved.md` stub.

What's missing: a **write-once pre-install snapshot** that captures the user's state *before any atomic modification*, not just the per-update diff backups.


## Approaches

| # | Approach | Pros | Cons |
|---|----------|------|------|
| A | Dedicated `~/.claude/.atomic/pre-install/` snapshot dir, written once on first install, never overwritten | Simple mental model; single restore source; survives multiple updates | Requires "is this a first install?" detection; slightly larger disk use |
| B | Extend existing `backups/<ts>/` with a special `_initial` marker | Reuses existing infrastructure | Naming convention fragile; hard to distinguish from update backups; gets buried in a list |
| C | Store a manifest JSON of pre-install state (paths + SHA256s + content) instead of file copies | Compact; machine-readable | Harder to inspect manually; still needs the content bytes for restore |


## Recommendation

**Approach A.** Dedicated `~/.claude/.atomic/pre-install/` directory.

Rationale:

- Write-once semantics are trivial to implement: `if dir exists, skip snapshot`.
- Clear separation from update backups (which accumulate over time and serve a different purpose: rollback one update, not full uninstall).
- User can inspect it (`ls ~/.claude/.atomic/pre-install/`) to see exactly what they had before.
- The uninstall agent reads this dir as its restore source — no ambiguity about "which backup is the original?"

Additionally store a `pre-install/manifest.json` recording what was captured and when, so the uninstall agent can distinguish "file didn't exist before atomic" (don't restore, just delete) from "file existed with this content" (restore it).


## Uninstall flow

**CLI-prompt approach.** No agent definition, no slash command. The `atomic` binary does the deterministic work (reads manifest, computes plan, identifies merge-needed files) and outputs a structured prompt. Claude receives the prompt as stdout and executes it. The LLM handles only the judgment calls (settings.json and CLAUDE.md merging).

User runs `atomic claude uninstall` inside a Claude session → Claude gets the prompt → Claude executes the plan with one user confirmation.

Steps the CLI computes and encodes in the prompt:

1. Read `~/.claude/.atomic/pre-install/manifest.json`. If missing → exit 1.
2. For each artifact: restore from snapshot if it existed, delete if it didn't.
3. Flag settings.json and CLAUDE.md for LLM merge when they've been modified post-install.
4. Remove `~/.claude/.atomic/` entirely.
5. Print binary removal instruction.

Why CLI-prompt over agent/command:

- Prompt always in sync with binary version (embedded, not a separate artifact to maintain).
- No agent file to bundle/install/update.
- Deterministic plan computed by code, not LLM. LLM only handles the merge judgment.
- Works from any Claude session without requiring the atomic skills/commands to be installed (they're being uninstalled).


## Open questions

(none — resolved during design)

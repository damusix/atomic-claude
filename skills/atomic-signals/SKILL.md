---
name: atomic-signals
description: >
  Project state scanner. Regenerates deterministic and inferred signals files so Claude
  always knows the current shape of the repo without hallucination. Auto-triggers when
  user says "regenerate signals", "scan the project", "refresh project context",
  "what's in this repo", or "rescan". Also fires implicitly when /commit-only detects
  source-tree changes in the staged diff (silent mode — no confirmation prompt).
---

Keep the project snapshot current. Run the scan, dispatch the inferrer, ensure auto-load refs.

## Primary flow (atomic binary present)

1. **Detect binary.** Run `command -v atomic`. If missing: print `atomic binary not installed. install: curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash. falling back to markdown-only mode.` and jump to the fallback flow.

2. **Staleness check.** Run `atomic signals stale`. Exit 0 means signals are fresh — stop, no work. Exit 1 means stale — continue.

3. **Regenerate deterministic.** Run `atomic signals scan`. This writes `.claude/project/deterministic-signals.md` and copies the prior content to `.claude/project/.deterministic-signals.prev.md` (gitignored) so `atomic signals diff` works regardless of git state.

4. **Dispatch inferrer.** Spawn the `atomic-signals-inferrer` subagent via the `Agent` tool. The inferrer runs `atomic signals diff` internally to identify changed sections and updates only the dependent sections of `signals.md`. See the agent definition for the section dependency mapping.

5. **Ensure `@-refs` are wired somewhere Claude auto-loads.** Check, in order, for an existing pair of refs (`@.claude/project/deterministic-signals.md` AND `@.claude/project/signals.md`) in any of:

    - `claude.local.md` / `CLAUDE.local.md` (project-local, gitignored — preferred when present)
    - `CLAUDE.md` (committed project instructions)

    If either pair is found in ANY of those files, the wiring is already done — skip this step entirely. Do not duplicate.

    If no file contains the refs:

    - If `claude.local.md` or `CLAUDE.local.md` exists, append the block to whichever exists (prefer `claude.local.md`). This handles repos that separate project-local refs from bundled/committed instructions (e.g. config-source repos where `CLAUDE.md` is the bundle input and must not carry project-specific paths).
    - Else, append to `CLAUDE.md` (create it only if it does not exist and the repo has `.claude/project/`).

    **Placement:** position the `@-ref` block BEFORE behavioral rules/instructions in the target file. Signals are reference data (facts about the codebase), not instructions — placing them early follows the "longform data at top, instructions at end" principle for better model comprehension. If the target file has existing sections, insert after any brief orientation/context sections but before rules, conventions, or workflow sections.

    Block to append:

    ```markdown


    ## Project signals (auto-loaded)


    @.claude/project/deterministic-signals.md
    @.claude/project/signals.md
    ```

    Print the diff and the chosen target file before writing. Confirmation rules:

    - Running non-interactively (e.g. inside `/commit-only`, `/merge-to-main`, `/squash-and-merge`): append without confirmation.
    - Running from `/refresh-signals`: ask via `AskUserQuestion` before writing, naming the target file.

6. **Surface concerns.** If the inferrer returned a `## Concerns` table (judgment observations found during inference), present them to the user as a numbered list. Ask via `AskUserQuestion`: "The signals scan found N potential issues. Create follow-ups for any?" with options:
    - "All" — create follow-ups for every concern via `atomic followups add`
    - "Pick" — print the indexed list, accept space/comma-separated indices, create only those
    - "Skip" — discard, no follow-ups created

    When running in silent mode (e.g. inside `/commit-only`), skip this step entirely — concerns are discarded silently. They'll surface on the next interactive `/refresh-signals` run.

7. **Report.** Print one-line summary: `signals refreshed. <N> sections changed. inferrer updated <M> sections.` If concerns were created as follow-ups, append: `<K> follow-ups created.` Suppress this line when invoked from a host command that requested silent mode (e.g. `/commit-only` step 4) — those flows already produce their own report.

## Fallback flow (no binary)

When `atomic` is absent:

1. Skip the staleness check — always regenerate.
2. Run `find . -type f -not -path './node_modules/*' -not -path './.git/*' | head -200 > .claude/project/deterministic-signals.md`.
3. Skip the inferrer — it requires structured input from the binary.
4. Print: `fallback mode produced a tree-only signals doc. install atomic for full functionality.`

The fallback is deliberately limited. Users hit it once and install the binary.

## Integration with `/commit-only`

When `/commit-only` runs and `atomic signals stale` reports stale, it invokes this skill silently before the commit. The gate is `atomic` installed + signals stale — no file-extension allowlist. In that context:

- Skip the `AskUserQuestion` on `CLAUDE.md` append — write directly.
- If signals are already fresh (`atomic signals stale` exits 0), skip entirely.
- If `atomic` is not installed, skip entirely — do not fall back during commit.
- If signals were regenerated, stage `.claude/project/deterministic-signals.md` and `.claude/project/signals.md` alongside the commit.
- "Silent" = suppress step 6's report line. Interactive prompts are already gated by the bullet above; this clarifies that the skill produces no stdout in silent mode so the host commit flow's output stays clean.

This integration is implemented in `/commit-only` (CP S-4), not here.

## Boundaries

- Never modifies files outside `.claude/project/` and whichever auto-loaded Claude instructions file the wiring step targets (`claude.local.md` / `CLAUDE.local.md` / `CLAUDE.md`).
- Never blocks a commit — if the scan fails, log and continue.
- Silent in `/commit-only` context; interactive only when invoked from `/refresh-signals` or directly by the user.

# Document templates: embedded fill-in skeletons for coordinated workflow documents


## Problem


Every document the workflow coordinates — design doc, spec, scratchpad `BRIEF.md` / `STATE.md` / `FOLLOWUPS.md`, session report, diagnose `CONTEXT.md`, spec `## Implementation log` — had its skeleton inlined as a fenced block inside the authoring command. Two failure modes:

- **Improvised structure.** The skeleton sits inside instructions-adjacent prose, so the authoring LLM reconstructs it from memory: renamed headings, dropped sections (`## Change log`, `## Flows`), invented ones. Downstream consumers that address sections by name (reviewer spec-mode checklist, diagnose Phase 4 running `## Repro`, ship verbs reading `session_summary`) silently miss.
- **Duplication drift.** The same skeleton described in more than one place (command body, spec doc, reviewer criteria) drifts on every LLM-driven refinement pass.

A file-based first cut (`commands/_templates/*.md`, next to the existing `implementer-prompt.md` / `reviewer-prompt.md`) was rejected during this design: Claude Code surfaces every markdown file under `~/.claude/commands/**` as an invocable entry — the existing two prompt templates already appear in the session skill listing as `_templates:implementer-prompt` / `_templates:reviewer-prompt`. Eight more files means eight more noise entries in every user's command surface.


## Goals / Non-goals


- Goals: one canonical, empty, fill-in skeleton per coordinated document; `<angle-bracket>` placeholders plus guidance comments carrying the fill rules; commands reference the skeleton instead of inlining it; zero new entries in Claude Code's command/skill surface; fail-loud when a skeleton is unavailable (never improvise).
- Non-goals: migrating `implementer-prompt.md` / `reviewer-prompt.md` out of `commands/_templates/` (candidate follow-up — same noise argument applies, but they are load-bearing for the installed loop and deserve their own change); templating `.claude/project/followups/<id>.md` (the `atomic followups add` CLI already owns that frontmatter); templating ci-mode `CONTEXT.md` (raw log capture plus a trailing key, not a fill-in document); wiki pages (owned by the `atomic-wiki` pipeline references); runtime variable substitution (skeletons are static text; the LLM fills them).


## Approaches


| # | Approach | Pros | Cons |
|---|----------|------|------|
| A | Embed skeletons in the `atomic` binary; new `atomic template <name>` verb emits them | Invisible to Claude Code's command pickup; versions with the binary; mirrors the existing `coldprompt` / `atomic prompt <name>` pattern exactly; auto-registered via `embed.FS` | Commands gain a hard runtime dependency on the binary; older binaries lack the verb until updated |
| B | Ship as `commands/_templates/*.md` artifact files | No binary dependency; travels with the artifact install | Every file surfaces as an invocable command entry in Claude Code — permanent noise; eight files copied into every install |
| C | Keep skeletons inline in each command, add a drift lint | No new mechanism | Duplication remains; a lint catches drift between copies but not improvisation at fill time; skeletons keep paying token cost in every command load |


## Recommendation


**Approach A.** New `atomic/internal/doctemplate/` package (mirrors `coldprompt`: embedded `.md` files, `Get(name)` / `Names()`), new `template` cobra verb (mirrors `prompt`: parent + one child per name, `templateAction` testable seam), commands instruct `atomic template <name> > <target>` and hard-stop if the verb fails.

The binary dependency is acceptable: atomic-claude installs via `atomic claude install`, so any installed setup has the binary, and `atomic update` keeps artifacts and binary in lockstep. The fail-loud rule ("stop, never improvise the structure from memory") is the same contract the loop already applies to missing prompt templates.

Approach B rejected for the command-surface noise above. Approach C rejected because it solves only the drift half of the problem — improvisation at authoring time is the half the user actually hit.


## Open questions


- Migrate `implementer-prompt.md` / `reviewer-prompt.md` into the binary too (`atomic template implementer-prompt`)? Same noise argument; separate change because installed commands reference the file paths today.

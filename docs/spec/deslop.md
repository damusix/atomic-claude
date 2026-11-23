# Deslop


## Goal


Find avoidable complexity and prose in the scope the user chooses. A user can audit a single PR without reporting problems in the rest of the repo.

## Non-goals


Correctness review, linting, and design changes. The command does not commit findings or fixes unless asked.

## Success criteria


- [ ] The default is the current change. An explicit PR uses its actual target branch, with findings confined to the diff. An explicit path or repo request can include standing code.
- [ ] Each finding identifies a location, evidence, category, safety tier, and fix. A read-only audit edits no source.
- [ ] A natural-language request can authorize safe fixes in the same invocation. Fixes stay in scope and preserve behavior. Guarded or report-only findings are reported without edits.
- [ ] Fixes use the relevant check. The report says what changed and what was verified.

## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|---|---|---|
| 1 | Choose scope and protect unrelated work | `context/commands/deslop.md`, `context/agents/atomic-deslopper.md` | Prompt supports a PR targeting a non-default base without findings outside the diff. |
| 2 | Apply only safe findings on request | Same command and agent | Prompt distinguishes audit from edits without a separate flag. |

## Change log


### 2026-11-22: Make the current change the default scope


**What changed:** Added explicit PR scope and natural-language authorization for safe fixes.

**Why:** A PR audit should not surface unrelated debt or require another command to apply safe fixes.

**Superseded:** A repo-wide default and a separate `/deslop apply` invocation.

<!--
Template for $SCRATCH/CONTEXT.md in /subagent-diagnose bug mode — emitted by
`atomic template diagnose-context`. The Phase 0 symptom
capture. Copy this body, fill every <angle-bracket> placeholder, delete this comment.
The four headings are stable — downstream phases address them by name (Phase 4 runs
`## Repro` verbatim). `top_level_error:` is a trailing YAML key read as the
iteration-0 baseline; keep it after the headings. `## Recent commits` is auto-capture:
omit the section when no suspected paths are inferable from the brief.
ci mode does not use this template — its CONTEXT.md is a raw log capture (truncated at
64KB) ending in the same `top_level_error:` trailing key.
-->
## Repro

<numbered steps that reproduce the failure>

## Expected vs actual

<expected behavior vs what actually happens>

## Environment

<OS, runtime versions, branch, dirty/clean working tree>

## Already tried

<what's been attempted, and the result of each attempt>

## Recent commits

<git log --oneline -20 -- {suspected paths} output>

top_level_error: <exact paste-able error string, or "<none — behavioral bug>">

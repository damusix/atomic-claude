---
id: retro-extract-f-1
title: atomic validate artifacts skips fenced blocks indented under a list item
created: "2026-09-21"
origin: |
    docs/spec/retro-extract.md, iter 5 reviewer
kind: finding
severity: risk
review_by: "2026-11-20"
status: open
file: atomic/internal/validate/artifacts.go
---

The fence-open check in extractFencedBlocks requires the backtick at column 0 and never trims leading whitespace, so a code block indented under a numbered list item is invisible to A1 citation checking. A bogus flag injected into such a block still reports 0 FAIL, which is what `context/commands/retrospective-learning.md` pre-flight step 3 looks like. Trim leading whitespace before the fence test and add a fixture with an indented fence. Pre-existing; surfaced while reviewing the retro-extract artifacts.

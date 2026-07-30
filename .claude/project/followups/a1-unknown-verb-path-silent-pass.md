---
id: a1-unknown-verb-path-silent-pass
title: A1 lint silently passes citations with unknown verb paths
created: "2026-07-04"
origin: |
    wiki-stamp registration session 2026-07-04
kind: finding
severity: risk
review_by: "2026-09-02"
status: open
file: atomic/internal/validate/artifacts.go:186
---

`checkSpan` drops any citation whose verb path fails `longestMatch` against cliusage ("accepted false-negative", artifacts.go:186-189). Consequence: an artifact citing a verb that was never registered in cliusage — or that gets renamed/removed later — passes `atomic validate artifacts` silently, so the lint cannot catch exactly the class of drift it exists for. This is how `atomic wiki stamp` citations in skills/atomic-wiki/references/realm.md and templates/commands/refresh-wiki.md went unvalidated from the Cobra port until 2026-07-04 (stamp registered publicly that day).

Possible fix: WARN (not FAIL) on `atomic <known-top-verb> <unknown-sub-path>` — top verb gate already passed, so the citation is clearly atomic-intended; only fully unknown top verbs stay silent (avoids false positives on prose like `atomic style`).

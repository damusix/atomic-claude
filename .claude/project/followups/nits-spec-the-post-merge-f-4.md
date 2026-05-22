---
id: nits-spec-the-post-merge-f-4
title: Spec the post-merge / post-squash signals-refresh integration
created: "2026-05-17"
origin: |
    chat session 2026-05-17 follow-on to fixing `/squash-only` signals-refresh gap. Pre-existing spec gap, not introduced by the fix.
severity: nit
review_by: "2026-07-16"
status: open
---

`docs/spec/signals-workflow.md` contracts the `/commit-only` integration (skill auto-fires pre-commit when source-tree changes). It does NOT cover the post-op defense-in-depth refresh now implemented in `/merge-to-main`, `/squash-and-merge`, and (as of this session) `/squash-only`. The pattern is in code but unspecced — future contributors editing those verbs have no canonical contract to follow.


Scope when revisiting:


- Add a "Post-merge / post-squash integration" section to `docs/spec/signals-workflow.md` describing the three-step probe (`command -v atomic` → `atomic signals stale` → invoke skill non-interactively) and the follow-up-commit convention (`chore(signals): refresh after <op> of <feature>`).
- Document the rationale: branch commits may have skipped `/commit-only` (manual `git commit`, rebased history, external PR squash) so the merge/squash target is the last guaranteed gate.
- Decide whether `/pr-only` should join the family (currently exempt — working tree must be clean, so by construction nothing to refresh). Probably leave exempt; document the reasoning.
- Append a `## Change log` entry per the spec amendment rule when landing.

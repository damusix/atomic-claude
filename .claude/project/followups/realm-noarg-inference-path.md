---
id: realm-noarg-inference-path
title: Realm-scope inference with no wiki-output args has no defined pipeline
created: "2026-10-19"
origin: |
    docs/spec/artifacts-mermaid-flows.md, audit-fix reviewer
kind: finding
severity: question
review_by: "2026-12-18"
status: open
file: context/skills/atomic-wiki/references/realm.md:11
---

With no target_repo/wiki_dir and a realm index at the parent, atomic-wiki-inferrer routes to references/realm.md, but realm.md defines W1-W7 only for wiki-output mode (both args) and an error for exactly one; it gives no instruction for neither. Same gap at the base before #298; the conversion made it visible. Decide what a no-arg realm-scope inference run executes.

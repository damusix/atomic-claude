---
id: atomic-setup-prerelocation-paths
title: atomic-setup audit still checks pre-relocation signals paths
created: "2026-07-09"
origin: |
    issue #126 implementation (wiki-scan-commit branch)
kind: finding
severity: nit
review_by: "2026-09-07"
status: open
file: templates/commands/atomic-setup.md
---

templates/commands/atomic-setup.md:63,71-72 (and rendered commands/atomic-setup.md) audit .claude/project/deterministic-signals.md, .claude/project/.deterministic-signals.prev.md in .gitignore, and @.claude/project/signals.md as evidence of a correctly set-up repo. All three moved in wiki-storage-relocation (now docs/wiki/scan.md, tmp/.scan.prev.md, @docs/wiki/index.md), so the audit misclassifies every current repo as partial/fresh. Line 196 CLAUDE.md-survey input list has the same stale paths. Found in passing during issue #126.

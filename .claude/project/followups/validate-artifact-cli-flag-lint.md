---
id: validate-artifact-cli-flag-lint
title: 'Add atomic validate check: flag artifacts that cite atomic CLI verbs/flags the binary doesn''t define (the --format-json class)'
created: "2026-06-08"
origin: |
    /atomic-improve 2026-06-08 (docs/CLI coordination)
kind: plan
review_by: "2026-08-07"
status: open
file: atomic/internal/validate/
---

Artifacts (commands, agents, skills, docs, CLAUDE.md) frequently cite `atomic <verb> --flag` invocations. Nothing verifies those citations against the actual binary, so a wrong flag ships silently and only fails when a user runs the example.

Concrete instance: `atomic code … --format json` was authored into six agent prompts; the real flag is `--json`. It passed render, bundle, and review undetected — caught only by chance during a docs pass. See the new mitigation in `.claude/skills/atomic-cli-contrib/SKILL.md` "Common mistakes" #7 (manual verification rule), which this check would automate.

**Proposal:** add an `atomic validate` check (or a `make`/CI lint) that:

1. Scans bundled artifacts for `atomic <verb> [--flags]` references (regex or simple parse).
2. Cross-checks each verb against the dispatch table and each `--flag` against that verb’s registered flags (the binary already knows its own flag set; expose it or reflect over the flag definitions).
3. Flags any artifact citing a verb/flag the binary does not define.

Catches the `--format json` class at author time instead of user-runtime. Scope it to the `atomic`-owned verbs (`code`, `signals`, `wiki`, `docs`, `claude`, `followups`, etc.).

Lower priority than [[validate-checkpoint-header-drift]] — the cli-contrib rule covers it manually for now. This is the durable, automated backstop.

Found by /atomic-improve 2026-06-08 (targeted: docs ↔ CLI coordination).

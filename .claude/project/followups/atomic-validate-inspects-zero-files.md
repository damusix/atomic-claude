---
id: atomic-validate-inspects-zero-files
title: atomic validate reports 0 PASS/0 WARN/0 FAIL — inspects nothing
created: "2026-07-25"
origin: |
    discovered during #164 autopilot Phase 4 verification
kind: finding
severity: risk
review_by: "2026-09-23"
status: open
file: atomic/internal/validate/
---

`atomic validate` (whole-repo, no subcommand) and `atomic validate config` both report
`0 PASS, 0 WARN, 0 FAIL. exit 0.` for all rule groups — config, bundle, artifacts. Zero
files are inspected, so the exit 0 is vacuous rather than a pass.

Reproduced on this repo from both a worktree root and the main checkout, on two different
branches, with both the installed binary and a freshly built one. Pre-existing, not
introduced by the #164 branch.

**Why it matters:** ship verbs and the `atomic-verify` skill treat `atomic validate` exit 0
as a gate. A gate that inspects nothing passes everything, so spec/config/artifact
regressions that A1 and the config rules are meant to catch would ship silently.

**Suspected cause:** likely the same root as `cli-repo-flag-never-parses` — every leaf Cobra
command sets `DisableFlagParsing: true`, so path arguments and `--repo` never reach the verb
and the file set to validate resolves empty. Unconfirmed; needs a debugger or a print pass
through `internal/validate` entry points.

**How to verify a fix:** `atomic validate` in this repo must report a non-zero PASS count,
and deliberately breaking a `@`-ref in a scratch copy of CLAUDE.md must produce a FAIL.

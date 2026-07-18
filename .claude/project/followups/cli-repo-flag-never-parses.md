---
id: cli-repo-flag-never-parses
title: Global --repo flag never parses on any verb (DisableFlagParsing)
created: "2026-07-09"
origin: |
    repo-init CP1 builder, issue #125 branch
kind: finding
severity: risk
review_by: "2026-09-07"
status: open
file: atomic/cmd/atomic/main.go
---

Every leaf Cobra command sets DisableFlagParsing: true; Cobra parses flags only on the resolved leaf, and ParseFlags no-ops when disabled — so the root persistent --repo flag is never parsed for any verb. 'atomic signals scan --repo <dir>' fails with 'flag provided but not defined: -repo'; 'atomic --repo <dir> signals stale' silently ignores the override and resolves from cwd. Affects all repo-scoped verbs (signals, docs, followups, code, reminder, hooks, repo). Fix is cross-cutting: either a global pre-scan like --no-update-check/--version, or enabling flag parsing across ~10 builder functions. No existing test exercises rootCmd.Execute() with real argv.

Same family (observed 2026-07-16, pi-harness smoke test): nested `--help` on subcommands exits 2 instead of 0 — help text is treated as a parse failure rather than a successful help path. Fold into whichever fix shape is chosen.

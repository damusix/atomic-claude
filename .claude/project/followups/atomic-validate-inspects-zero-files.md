---
id: atomic-validate-inspects-zero-files
title: atomic validate always prints "0 PASS" — reads as nothing-inspected
created: "2026-07-25"
origin: |
    discovered during #164 autopilot Phase 4 verification
kind: finding
severity: nit
review_by: "2026-09-23"
status: open
file: atomic/internal/validate/finding.go
---

`atomic validate` prints a summary of the form `0 PASS, 0 WARN, 0 FAIL. exit 0.` on a clean
repo, for every rule group. The `PASS` count is structurally always zero: `summarize`
(`internal/validate/finding.go:44`) counts *findings* by severity, and no rule ever emits a
finding with a severity other than WARN or FAIL, so `Pass` can never be incremented.

The line therefore reads as "nothing was inspected" when it actually means "nothing was wrong".

**Correction of the original filing.** This entry first claimed the validator inspects zero
files and that its exit 0 is vacuous. That was wrong. Verified against a scratch repo: a broken
`@`-ref in `CLAUDE.md` produces `FAIL C5 ... does not resolve` with exit 1, and a spec missing
required sections produces `FAIL S5` and `FAIL S6` with exit 1. `runWholeRepo`
(`internal/validate/dispatch.go:137`) globs `docs/spec/*.md` and runs the config rules against
the repo root as intended. The gate works. Only its output misleads.

**Why it still matters:** the misleading line has already produced one wrong conclusion — an
autopilot run read `0 PASS` as evidence the gate was inert and filed this follow-up on that
basis. Anyone auditing whether the ship-verb gates actually check anything hits the same trap.

**Options:** drop the `PASS` column, or make it meaningful by counting what was inspected
(files scanned, rules evaluated) instead of findings emitted. The second is more useful —
`47 files checked, 0 WARN, 0 FAIL` answers the question the current line only appears to answer.

**How to verify a fix:** on a clean repo the summary must not imply an empty inspection, and
deliberately breaking an `@`-ref must still produce `FAIL` and exit 1.

# Spec: format routing in the atomic output style


Implements GitHub issue #113 as re-scoped by the user (in-TUI expression vocabulary; no rendered surface). Approach, pattern verdicts, and the exact replacement section text: `docs/design/legible-output.md` — this spec is the implementation contract only.


## Checkpoints


| # | Checkpoint | Files/areas | Mode | Verifies |
|---|-----------|-------------|------|----------|
| 1 | Format-routing section + reference doc | `output-styles/atomic.md`, `docs/reference/output-style.md` | feature | Success criteria 1–6 |


Single checkpoint, markdown-only: TDD is skipped with an explicit "skipped because: pure markdown artifact change" note. Regen: `output-styles/` is a bundle source → run `make -C atomic bundle` and commit `atomic/internal/embedded/**` in the same commit. No `templates/` file is touched, so render is a no-op (verify parity anyway).


## Checkpoint 1 contract


### 1a. `output-styles/atomic.md`


Replace the entire `# Structure over prose` section (heading through the trailing `**TUI replies:** ASCII only…` line inclusive) with the `# Format routing` section given **verbatim** in the design doc's "The replacement section" block. Do not reword, reorder, or extend it — the wording is user-approved and token-budgeted. No other part of the file changes.


### 1b. `docs/reference/output-style.md`


Add a section documenting the format-routing vocabulary for human readers, in the file's existing narrative voice (atomic-prose):

- The routing rule: content shape picks the format; prose below three entities; formats compose within one reply, summary first.
- The ten routes, one line each with a "use when / avoid when"; route names must match the style file's section exactly.
- The three caps (fenced whitespace-aligned text, one symbol vocabulary per reply, no box-drawing cards), each with a one-line reason sourced from the design doc's Pattern verdicts table — the design is the rationale authority, do not invent new reasons.
- Keep it ≤ 45 added lines; it documents, it does not duplicate the style file's example.


## Success criteria


1. `output-styles/atomic.md` contains `# Format routing` and no `# Structure over prose`; all ten route lines present exactly as in the design block; trailing TUI/Mermaid line preserved verbatim.
2. The replaced section is ≤ 36 lines and the file's net line delta vs commit `6e7fb3c` is ≤ +10 (`git diff --numstat 6e7fb3c -- output-styles/atomic.md`).
3. `docs/reference/output-style.md` describes all ten routes and all three caps, ≤ 45 added lines, no contradiction with the style file.
4. Bundle regen committed in the same commit: `make -C atomic bundle` then `git diff --exit-code atomic/internal/embedded` clean; `make render` then `git diff --exit-code commands/ agents/` clean (no-op expected).
5. Scope guard: no other file changes — no skills/, commands/, templates/, agents/, README, CLAUDE.md edits; the string `atomic-legible` appears nowhere in the tree except this spec's change log (recorded history of the rejected approach).
6. `go -C atomic test ./...` — no new failures vs the branch-point baseline; `atomic validate spec` reports no FAIL for `docs/spec/legible-output.md`.


## Change log


- 2026-07-03 — initial spec for the re-scoped feature. Supersedes the closed PR #117 approach (presentation ladder + `atomic-legible` skill), which the user rejected; that branch was deleted, so this spec body starts clean.

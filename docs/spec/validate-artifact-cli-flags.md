# Spec: validate artifact CLI-flag citations

## Summary

Add an `atomic validate` check that flags artifacts citing an `atomic` verb-path or `--flag` the binary does not define. The check's source of truth is a new structured command-surface table (`internal/cliusage`) that also renders the binary's `--help`. Catches the `atomic code … --format json` class of authoring bug at CI/author-time instead of user-runtime.

Design: `docs/design/validate-artifact-cli-flags.md`.

## Behavior

### Command surface table (`internal/cliusage`)

- Exposes the full `atomic` command surface as ordered structured data: per command, the verb-path tokens, an args hint, the accepted `--flags`, and a one-line description.
- Exposes a render function that produces the `--help` "Commands:" block from the table.
- `atomic/cmd/atomic/main.go` `fs.Usage` renders the Commands block via this function. Global flags continue to render via `fs.PrintDefaults()`.
- The table's flag set per command reflects the binary's **actual registered flags** (the `flag.NewFlagSet` registrations in each verb handler), not merely the prior hand-written help text. The followup requires cross-checking against *registered* flags, and the prior help block was incomplete (e.g. it omitted `followups add --kind`, `reminder add --due`/`--transport`). Bringing the table to the real flag set is required so the Checkpoint 2 check does not flag valid usages. Consequence: `--help` now documents these previously-undocumented flags — an intended improvement.
- Every verb-path present in the prior `--help` remains present; no verb is dropped.

### `atomic validate artifacts [paths...]`

- New subcommand. With no paths, scans the full artifact corpus via `bundlemirror.Enumerate(repoRoot)` (agents, commands, skills, output-styles, rules, `CLAUDE.md`). With paths, scans those files.
- Also runs as part of the whole-repo `atomic validate` (no subcommand), in sequence with spec, config, and bundle.
- Honors `--json` and `--suggest` exactly as the other validate subcommands do.
- Reuses `validate.Finding`, `sortFindings`, `summarize`, `exitCode`, and the JSON/human output path. Same exit-code contract: 0 pass, non-zero on FAIL.

### Scanner rules (rule `A1`)

A citation is only considered when it appears inside a markdown inline code span (`` `…` ``) or fenced code block, and the first token after `atomic` is a registered top-level verb. Otherwise it is ignored.

For each qualifying citation:

- Greedily resolve the longest known verb-path prefix from the surface table (handles multi-word paths such as `claude install`, `code search`, `signals scan`).
- **Resolved citation** — validate each cited `--flag` (normalized by stripping `=value`): if it is not in the resolved command's flag set and not a universal flag → FAIL. Remaining lowercase word tokens after the matched path are treated as positional args and are NOT validated (this covers arg-enum subcommands like `validate spec`, where `validate` is the table entry and `spec` is a positional).
- **Unresolved citation** — no verb-path matches (e.g. a namespace verb with a typo'd or bare subcommand: `code foo`, bare `code`). Emit nothing. Flags cannot be attributed to a command, so validating them would risk false positives. This is an accepted false-negative (logged below).
- **Universal flags** always accepted for any path: `--help`, `-h`, `--version`, `-v`, `--repo`, `--no-update-check`.

Findings name the artifact path, line number, the offending flag, and the resolved command's known flags as the expected alternative. Conservative by design: when a citation is ambiguous, emit nothing (favor false-negatives over false-positives).

### Known scope limits (logged, not silent)

- Citations in bare prose outside code spans are not checked (accepted false-negative).
- Citations whose verb-path does not resolve to a table entry (typo'd/unknown subcommand under a namespace verb) are not flagged — only flags on resolved commands are checked (accepted false-negative).
- Positional argument *values* are not validated — only flag-name correctness on resolved commands.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Extract command surface into `internal/cliusage`; render `--help` from it | `atomic/internal/cliusage/`, `atomic/cmd/atomic/main.go`, golden test | `atomic --help` lists every verb/flag present today; golden test pins output; `go test ./...` green |
| 2 | `validate artifacts` rule + subcommand + whole-repo wiring + docs/help | `atomic/internal/validate/`, `templates/commands/atomic-help.md`, `docs/spec/atomic-validate.md`, `docs/reference/`, `commands/` (rendered), bundle | `atomic validate artifacts` FAILs wrong flag, passes good citation + universal flags + arg-enum subcommands, ignores prose; repo scans clean; render+bundle parity; `/atomic-help` MISSING-scan zero |

## Success criteria

- `atomic validate artifacts` exists, scans the artifact corpus, and is included in the whole-repo `atomic validate` run.
- A citation with a wrong flag (`atomic code search --format json`) FAILs; the same with `--json` passes.
- Arg-enum subcommands (`atomic validate spec --json`, `atomic followups add --kind plan`) pass — positional subcommands are not mistaken for flags, and real registered flags are in the table.
- Universal flags (`--help`, `--repo`, etc.) never FAIL.
- Prose mentions of "atomic" outside code spans never produce findings.
- The command surface is defined once (`internal/cliusage`), reflects the binary's real registered flags, and renders the binary's `--help`; a golden test guards the rendered output.
- This repo's own artifacts scan clean (real findings fixed during implementation).
- `go test ./...`, `make render`, `make -C atomic bundle`, `atomic validate spec`/`config` all green; `/atomic-help` MISSING-scan returns zero.

## Change log

- 2026-06-10 — Initial spec. Source of truth chosen as a structured `internal/cliusage` table that also renders `--help` (over self-help parsing, `go/ast` reflection, or a hand-maintained list — see design doc options table). Two checkpoints: surface extraction (behavior-preserving refactor), then the `A1` artifacts rule. Scanner gated to code-span citations under known top-level verbs to keep false-positives near zero; bare-prose citations and arg-value validation declared out of scope.
- 2026-06-10 — Implemented (autopilot). Three commits: `4858d3d` CP1 (cliusage surface table + table-driven `--help`, flags reconciled to real registrations, golden test), `394822d` CP2a (rule A1 scanner + `validate artifacts` subcommand + whole-repo wiring + tests), `89d030c` CP2b (help-router + atomic-verify gate + this spec + render/bundle). Dogfood caught a real scanner bug — multi-line fenced blocks concatenated tokens across lines, attributing a later line's flag to an earlier line's verb; fixed to per-line emission. Repo scans 0 FAIL. CP1 review also surfaced and fixed two table-vs-reality drifts (`signals linkify --root` phantom in old help, `code affected --test-glob` missing). All gates green except 3 pre-existing environmental `internal/hooks` failures (user wiki `.dirty` state, reproduce on base main).
- 2026-06-10 — CP1 review correction. Two changes superseding the initial body: (1) the surface table reflects the binary's **actual registered flags**, not the prior incomplete help text — the old `--help` omitted real flags (`followups add --kind`, `reminder add --due`/`--transport`), which would have made the CP2 check flag valid usages. (2) Dropped the "unknown subcommand → FAIL" rule: namespace verbs whose subcommands are positional arg-enums (`validate spec`) would false-positive. The check now validates only flags on resolved citations; unresolved verb-paths emit nothing (accepted false-negative, logged in scope limits). Both changes reduce false positives and align with the followup's "cross-check against registered flags" intent.

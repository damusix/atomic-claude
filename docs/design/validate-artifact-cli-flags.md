# Design: validate artifact CLI-flag citations

## Problem

Artifacts (agents, commands, skills, output-styles, rules, `CLAUDE.md`) cite `atomic <verb> [--flags]` invocations in their prose and code spans. Nothing verifies those citations against the real binary, so a wrong verb or flag ships silently and only fails when a user runs the example.

Concrete instance that motivated this: `atomic code … --format json` was authored into six agent prompts; the real flag is `--json`. It passed render, bundle, and review undetected — caught by chance during a docs pass. `.claude/skills/atomic-cli-contrib/SKILL.md` "Common mistakes" #7 mitigates it with a *manual* verification rule. This design automates that rule.

## Goal

A new `atomic validate` check that flags any artifact citing an `atomic` verb-path or `--flag` the binary does not define. Catch the `--format json` class at author-time / in CI instead of at user-runtime.

## Source of truth — the decision

The check must cross-check citations against the binary's real verb/flag set. Investigation found:

- The complete, accurate command surface is the top-level `fs.Usage` block in `atomic/cmd/atomic/main.go` (~50 `fmt.Fprintf` lines), each line of the form `  <verb-path> [args] [--flags]  <description>`.
- There is **no machine-readable registry** — the usage text is the only enumeration, and it is locked inside `main()`, unreadable by the `validate` package.
- Per-verb `--help` is unreliable as a source: `atomic code --help` exits non-zero outside a git repo (`repoctx` failure), so it cannot be parsed in arbitrary contexts.

Options considered:

| Option | Verdict |
|--------|---------|
| Parse `atomic --help` output (exec self, or capture stderr) | Rejected — parsing our own rendered output is a smell; `fs.Usage` is not callable from `validate`; brittle. |
| `go/ast` static analysis of scattered `flag.NewFlagSet` calls | Rejected — associating FlagSets to verb-paths across ~16 packages is error-prone; high false-negative/positive risk; needs Go source present. |
| Hand-maintained flag list inside the check | Rejected — a second list that silently drifts from the binary, defeating the purpose. |
| **Extract the surface into a structured table both `main.go` and `validate` consume** | **Chosen** — single source of truth; implements the followup's "expose it"; no parsing of own output; eliminates the 50-line `Fprintf` block. |

## Approach

Two cohesive slices.

### Slice 1 — `internal/cliusage` surface table (source of truth)

A new package exposes the command surface as structured data:

- An ordered list of commands, each with: verb-path tokens (e.g. `["code","search"]`), an args hint string (e.g. `<query>`), a list of accepted flags (e.g. `["--json","--limit"]`), and a one-line description.
- A render function produces the `--help` "Commands:" block from the table.
- `main.go`'s `fs.Usage` renders the Commands block from this function instead of the inline `Fprintf` lines. Global flags (`--repo`, `--no-update-check`, `--version`/`-v`) stay rendered by `fs.PrintDefaults()` as today.

Help rendering becomes table-driven. Output stays clear (aligned via `text/tabwriter`). A golden test pins the rendered `--help` so future surface edits are intentional.

This is a behavior-preserving refactor in intent: the same verbs and flags are listed; only the mechanism (data → render) changes, plus column alignment may normalize.

### Slice 2 — `validate artifacts` rule (the check)

A new rule (`A1`) and subcommand `atomic validate artifacts [paths...]`, also run by the whole-repo `atomic validate` (alongside spec + config + bundle).

Scanner, kept conservative to stay trustworthy as a CI gate (favor false-negatives over false-positives):

1. **Corpus** — `bundlemirror.Enumerate(repoRoot)` walks agents, commands, skills, output-styles, rules, `CLAUDE.md`. The check reads each artifact's text.
2. **Candidate spans** — only citations inside markdown **inline code spans** (`` `…` ``) or **fenced code blocks** are considered. Prose mentions of the word "atomic" (e.g. "atomic style", "atomic operations") are ignored. This gate kills the dominant false-positive source. Trade-off: a bare-prose citation outside code is not checked — an accepted false-negative, logged in the spec as a known scope limit.
3. **Parse** — within a candidate span, find `atomic <tokens…>`. Only proceed when the first token after `atomic` is a registered top-level verb (`code`, `signals`, `validate`, `wiki`, `followups`, `claude`, `config`, `docs`, `doctor`, `update`, `profile`, `hooks`, `reminder`, `docker`). Otherwise skip (false-friend like "atomic commit").
4. **Resolve** — greedily match the longest known verb-path prefix from `cliusage` against the leading word tokens (handles multi-word paths like `claude install`, `code search`, `signals scan`).
5. **Validate**:
   - **Unknown subcommand** under a known top-level verb (path does not resolve to any known command) → FAIL.
   - **Unknown flag**: a cited `--flag` (normalized: strip `=value`) not in the resolved command's flag set **and** not a universal/global flag → FAIL.
6. **Universal flags** always allowed for any path: `--help`, `-h`, `--version`, `-v`, `--repo`, `--no-update-check`.

Findings reuse the existing `validate.Finding` struct, `sortFindings`, `summarize`, `exitCode`, and JSON/human output — identical reporting and exit-code contract to spec/config/bundle.

## Non-goals

- No `go/ast` reflection over real `flag.NewFlagSet` registrations. The documented surface (the table) is the contract artifacts must match; if the table itself drifts from real flags, that is a separate concern guarded by the existing flag-registration code, not this check.
- No checking of citations in bare prose outside code spans (accepted false-negative; logged).
- No validation of positional argument *values* (e.g. whether `<symbol>` exists) — only verb-path and flag-name correctness.
- No new config schema keys.

## Risks

- **Help-format change.** Rendering `--help` from the table may normalize column spacing. Mitigated by a golden test and by keeping the same content. Visible but harmless; justified by removing a 50-line hand-maintained block.
- **False positives** would erode trust in a CI gate. Mitigated by the code-span gate, the known-top-level-verb gate, and the universal-flag allowlist. When ambiguous, the scanner stays silent.
- **Surface drift** between table and reality is now a single edit point; the golden `--help` test surfaces unintended changes.

## Verification

- Golden test: rendered `--help` Commands block lists every verb/flag currently present.
- Scanner unit tests: known good citation passes; `--format json` (wrong flag) FAILs; unknown subcommand FAILs; universal flags pass; prose "atomic" mentions ignored; multi-word verb-path resolves.
- Dogfood: run `atomic validate artifacts` over this repo; triage real findings (fix artifacts or refine allowlist) until the repo is clean.
- Full suite: `go test ./...`, `make render`, `make -C atomic bundle`, `atomic validate spec`/`config`, `/atomic-help` MISSING-scan.

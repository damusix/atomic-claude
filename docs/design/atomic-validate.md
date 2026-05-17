# atomic validate (design)


## Problem


`atomic doctor` checks environment and install state. The artifacts themselves can still be malformed even when wired correctly:


- A spec missing the `## Checkpoints` section.
- A command referencing a `subagent_type:` that doesn't resolve to any agent file.
- A skill description that lacks trigger phrases.
- A `CLAUDE.md` agent registry that names agents not in `agents/`.
- A `docs/spec/<topic>.md` missing the `## Change log` section the project mandates.


These bugs surface only when the artifact runs — sometimes silently (the wrong agent gets dispatched, or no agent at all). `atomic validate` is the linter that catches them before commit.


Distinct from `atomic doctor` (see `docs/design/atomic-doctor.md`). `doctor` = wiring and install. `validate` = content correctness.


## Goals


- Lint artifact content for structural and referential integrity.
- Subcommand namespace, composable.
- Fast enough to run on every changed file in a pre-commit hook or CI.
- Output: indexed findings, severity-tagged, one line per finding.
- Non-zero exit on any FAIL.


## Non-goals


- Style or voice linting (markdownlint covers that).
- LLM judgment (that's the `atomic-reviewer` agent's job).
- Running the artifacts.
- Auto-fixing content-level errors (human authorship boundary).


## Subcommands


| Subcommand | What it validates | Sources |
|-----------|-------------------|---------|
| `atomic validate spec [paths...]` | `docs/spec/*.md` has required sections | Markdown structure |
| `atomic validate design [paths...]` | `docs/design/*.md` has required sections | Markdown structure |
| `atomic validate config` | Cross-references resolve | `CLAUDE.md` + `agents/` + `commands/` + `skills/` |
| `atomic validate bundle` | Bundle source matches generated manifest | `agents/` + `commands/` + `skills/` + `output-styles/` + `rules/` + `CLAUDE.md` |
| `atomic validate followups` | `.claude/project/followups.md` entries well-formed | followups.md |
| `atomic validate` (no args) | All of the above on the whole repo | Whole repo |


Path arguments narrow the scope. Path-aware dispatch routes `docs/spec/*.md` to `spec`, `docs/design/*.md` to `design`, etc. — so `atomic validate <changed files>` works on a mixed diff.


## Spec validator rules


| # | Rule | Severity |
|---|------|----------|
| S1 | File starts with `# <title>` | FAIL |
| S2 | Has `## Goal` section | FAIL |
| S3 | Has `## Non-goals` section | FAIL |
| S4 | Has `## Success criteria` section with at least one `- [ ]` checkbox | FAIL |
| S5 | Has `## Checkpoints` section with a table whose header matches `\| # \| Checkpoint \| Files/areas \| Verifies \|` | FAIL |
| S6 | Has `## Change log` section (may be empty under first heading) | FAIL |
| S7 | If `## Implementation log` present, each entry has version/status + date | WARN |
| S8 | No TODO / TBD / `<placeholder>` outside `## Open questions` | WARN |


## Design validator rules


| # | Rule | Severity |
|---|------|----------|
| D1 | File starts with `# <title>` | FAIL |
| D2 | Has `## Problem` section | FAIL |
| D3 | Has `## Goals` and `## Non-goals` sections | FAIL |
| D4 | Has `## Alternatives` or `## Tradeoffs` section with a table | WARN |
| D5 | Has `## Recommendation` section | WARN |
| D6 | Has `## Open questions` section | WARN |


Design is exploratory — looser rules than spec. The required sections are problem framing + goals; the rest are recommended but not blocking.


## Config validator rules


| # | Rule | Severity |
|---|------|----------|
| C1 | Every agent in `CLAUDE.md` "Subagents available for dispatch" list exists at `agents/<name>.md` | FAIL |
| C2 | Every agent in `agents/*.md` (with `atomic-` prefix) appears in `CLAUDE.md` registry | WARN |
| C3 | Every `subagent_type: "<name>"` in `commands/*.md` resolves to `agents/<name>.md` (or is a built-in like `general-purpose`) | FAIL |
| C4 | Every skill name in `commands/*.md` (search for `` `<name>` skill ``) resolves to `skills/<name>/SKILL.md` | FAIL |
| C5 | Every `@-ref` in `CLAUDE.md`, `claude.local.md`, `CLAUDE.md`, `CLAUDE.local.md` resolves to an existing file | FAIL |
| C6 | No skill description claims `/atomic-<name>` invocation without `user-invocable: true` in frontmatter | WARN |
| C7 | No duplicate `name:` across `agents/*.md` | FAIL |
| C8 | No duplicate skill name across `skills/*/SKILL.md` | FAIL |


C2 (reverse direction) is WARN not FAIL because experimental agents may exist temporarily without registry entries.


## Bundle validator


Subset of `doctor`'s check 5 — useful in the dev loop without running full doctor. Single check:


- `go generate ./...` (dry-run if supported) produces the same `manifest.go` as committed.
- FAIL on diff.


## Followups validator


For `.claude/project/followups.md` (when present):


| # | Rule | Severity |
|---|------|----------|
| F1 | Each entry has an `### F-<N> — <title>` heading | FAIL |
| F2 | F-id is unique within the file | FAIL |
| F3 | Each entry has an `Origin:` line citing source (spec path + iteration, or commit SHA) | FAIL |
| F4 | Each entry has a severity emoji (🔴 / 🟡 / 🔵 / ❓) | WARN |
| F5 | Closed entries are marked `*(closed <date> — <sha>)*` and retained, not deleted | WARN |


## Output format


```
atomic validate config — referential integrity

[1] FAIL  C3  commands/foo.md:42  subagent_type "bar" — no agents/bar.md
[2] FAIL  C5  CLAUDE.md:118       @-ref .claude/missing.md does not resolve
[3] WARN  C2  agents/atomic-experimental.md not in CLAUDE.md registry

0 PASS, 1 WARN, 2 FAIL. exit 1.
```


Findings are tagged with rule ID (C3, S5, etc.) so users can cross-reference the rules table. Index numbers are per-run, rule IDs are stable.


## CI integration


```bash
# Validate only what changed
git diff --name-only origin/main..HEAD | xargs atomic validate

# Or validate everything in PR
atomic validate --json | jq '.findings[] | select(.severity == "FAIL")'
```


Exit codes:


- `0` — all PASS or only WARN.
- `1` — one or more FAIL.
- `2` — validator itself errored.


## Tradeoffs


| Option | Pros | Cons |
|--------|------|------|
| Subcommand namespace (chosen) | Composable, scoped runs, fast targeted CI | More surface area to maintain |
| Single `atomic validate` mega-command | Simpler | No way to run just one validator type |
| Validators as Claude skills | Could use LLM judgment | Slow, non-deterministic, not CI-friendly |
| Validators as separate binaries | Could ship independently | Fragmented surface, install complexity |


Chose namespace: validators are deterministic and benefit from independent invocation in CI gates.


## Open questions


- Should `validate` share code with `doctor` for the bundle parity check? Yes — extract to `atomic/internal/manifestcheck/`.
- Should `validate config` resolve external skill names (third-party skills installed in `~/.claude/skills/` but not bundled)? Probably no — focus on the project's own artifacts.
- How to handle the "in-flight skill" case where a command references a skill that's being added in the same PR? Probably: skill resolution checks `skills/*/SKILL.md` in the working tree, not `~/.claude/skills/`. PRs that add the skill + the command stay green together.
- Should we emit auto-fix suggestions for content-level errors (e.g., "missing `## Checkpoints` — here's the template")? Soft yes — `--suggest` flag prints templates, never edits files.
- Pre-commit hook integration: yes, via `atomic hooks install --pre-commit`? That's a new hook target. Defer until `validate` itself is stable.


## Cross-references


- Companion design: `docs/design/atomic-doctor.md`.
- Implementation home: `atomic/internal/validate/` (new package, parallel to `atomic/internal/doctor/`).
- Shared substrate with doctor: `atomic/internal/manifestcheck/`.

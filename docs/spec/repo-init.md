# Spec: `atomic repo init`

Design: [`docs/design/repo-init.md`](../design/repo-init.md). GitHub issue: #125.

## Contract

New binary verb `atomic repo init`, implemented in a new internal package
`atomic/internal/repoinit`, wired like `dockerinit` (internal package → `main.go` dispatch →
`cliusage` entry). Repo root resolution reuses the existing `repoctx.Resolve(repoOverride)`
helper — the same seam every other repo-scoped verb uses — so the verb honors the global
`--repo` flag like the rest of the surface.

Idempotent, non-destructive, never commits. Guarantees, in order:

1. `.claude/.scratchpad/` directory exists (`mkdir -p` equivalent).
2. `.claude/project/` directory exists.
3. `.claude/.scratchpad/` is git-ignored — else append managed rule `/.scratchpad/` to
   `.claude/.gitignore` (create the file if absent).
4. `.claude/.atomic-index/` is git-ignored — else append managed rule `/.atomic-index/` to
   `.claude/.gitignore`.
5. `tmp/` is git-ignored — else append line `tmp/` to root `.gitignore` (create if absent).
6. `.worktrees/` is git-ignored — else append line `.worktrees/` to root `.gitignore`.

"Is it ignored" is decided by effect: `git check-ignore -q` against a nonexistent probe path
under the directory in question (the probe string is the builder's choice). When git is
unavailable or the root is not a work tree, degrade to a literal line-presence scan of the
target ignore file (exact rule line, modulo surrounding whitespace). Named exception: the deterministic git path may be absent, so the
weaker literal fallback is deliberate.

Appends preserve existing content byte-for-byte: existing lines are never rewritten, reordered,
or removed. When creating `.claude/.gitignore` fresh, precede rules with a single comment line
`# managed by atomic repo init; rules are relative to .claude/`. When appending to an existing
file, append only the rule line(s), separated from existing content by a newline if the file does
not end with one.

Output: one line per item, `created <path-or-rule>` or `ok <path-or-rule>` (match existing verb
output tone, e.g. `dockerinit`). Exit 0 whether or not anything was created. Errors (unwritable
file, etc.) exit non-zero with the failing path.

`cliusage` gains `{Path: ["repo", "init"], Args: "", Flags: nil, Description: "Scaffold .claude/
layout: dirs + nested .claude/.gitignore + root ignore rules (idempotent)"}`. `--help` renders it
via the existing usage machinery.

## Repo migration (this repo, one-time)

Move the fifteen `.claude/*` lines (eight negations across five groups) from root `.gitignore`
into a new tracked `.claude/.gitignore`, each translated relative to `.claude/` and anchored
with a leading `/`. The fifteen lines sit in three spans of the root file — `.claude/.scratchpad/`
alone (a `.worktrees/` line that stays at root follows it), then thirteen contiguous lines, then a
separate `.claude/.atomic-index/` line further down. Exact target content of
`.claude/.gitignore` (after any managed-header comment):

```
/.scratchpad/
/agents/
/commands/*
!/commands/triage-issues.md
/output-styles/
/skills/*
!/skills/atomic-cli-contrib/
!/skills/atomic-cli-contrib/**
!/skills/atomic-release-ci/
!/skills/atomic-release-ci/**
/rules/*
!/rules/authoring/
!/rules/authoring/**
!/settings.local.json
/.atomic-index/
```

Root `.gitignore` drops exactly those fifteen lines (`.claude/.scratchpad/`, `.claude/agents/`,
`.claude/commands/*`, `!.claude/commands/triage-issues.md`, `.claude/output-styles/`,
`.claude/skills/*`, `!.claude/skills/atomic-cli-contrib/`, `!.claude/skills/atomic-cli-contrib/**`,
`!.claude/skills/atomic-release-ci/`, `!.claude/skills/atomic-release-ci/**`, `.claude/rules/*`,
`!.claude/rules/authoring/`, `!.claude/rules/authoring/**`, `!.claude/settings.local.json`,
`.claude/.atomic-index/`); everything else stays byte-identical.

Acceptance: `git status --porcelain` output identical before and after the move, and a
`git check-ignore` matrix over these paths yields identical verdicts before and after —
ignored: `.claude/agents/x`, `.claude/commands/other.md`, `.claude/output-styles/x`,
`.claude/skills/other/x`, `.claude/rules/other/x`, `.claude/.scratchpad/x`,
`.claude/.atomic-index/x`; not ignored: `.claude/commands/triage-issues.md`,
`.claude/skills/atomic-cli-contrib/SKILL.md`, `.claude/skills/atomic-release-ci/SKILL.md`,
`.claude/rules/authoring/axioms.md`, `.claude/.gitignore`, `.claude/atomic.toml`,
`.claude/project/followups/INDEX.md`.

## Template strip

Rule: templates stop doing layout-guarantee work (gitignore verify/append, standalone layout
mkdirs). Task-specific leaf `mkdir -p` stays as the degradation path for binary-absent repos.
Each stripped site gains a best-effort init call: run `atomic repo init` if `command -v atomic`
succeeds; skip silently otherwise.

| Template | Change |
|----------|--------|
| `templates/commands/subagent-implementation.md` | Replace the "`.claude/.scratchpad/` must be gitignored — verify, add if missing" step with the best-effort init call. Keep `mkdir -p "$SCRATCH"`. |
| `templates/commands/subagent-diagnose.md` | Same replacement at both gitignore-verify sites. Keep the `mkdir -p` calls. |
| `templates/commands/setup-wiki.md` | Gitignore-audit rows for `tmp/`, `.claude/.scratchpad/`, `.worktrees/`, and the signals prev-file delegate to `atomic repo init` (binary present) with the manual append retained only as the binary-absent fallback. Drop the `.claude/project/.deterministic-signals.prev.md` row entirely — no Go code writes that path anymore. |
| `templates/shared/worktree-setup.md` | "Verify .worktrees/ is gitignored": first try `atomic repo init` (best-effort); if `.worktrees` still not ignored (binary absent), keep today's append. Keep the commit step, widened to stage whatever ignore file(s) changed (`.gitignore` and/or `.claude/.gitignore`). |
| `templates/commands/atomic-help.md` | Mention `atomic repo init` in the `cli` topic row set. |

`templates/commands/remind-me.md` and `templates/commands/retrospective-learning.md` keep their leaf
`mkdir -p` lines unchanged (task-specific, not layout setup). After template edits: `make render`
+ `make -C atomic bundle`; both drift gates must pass.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | `atomic/internal/repoinit` package + CLI wiring + tests | `atomic/internal/repoinit/`, `atomic/cmd/atomic/main.go`, `atomic/internal/cliusage/cliusage.go` | `go test ./...` green; `atomic repo init` runs idempotently in a scratch repo (second run all-`ok`); cold repo gets dirs + nested `.claude/.gitignore`; repo with pre-existing effective rules gets no appends; degraded (no git) path covered by test |
| 2 | This repo's `.gitignore` migration | `.gitignore`, `.claude/.gitignore` | Acceptance matrix above passes; `git status --porcelain` unchanged |
| 3 | Template strip + help router + render/bundle | `templates/commands/subagent-implementation.md`, `templates/commands/subagent-diagnose.md`, `templates/commands/setup-wiki.md`, `templates/shared/worktree-setup.md`, `templates/commands/atomic-help.md`, `commands/*.md`, `atomic/internal/embedded/` | All table rows applied; `make render` + `make bundle` clean; `/atomic-help` MISSING-scan passes; grep shows no remaining "verify … gitignored" prose in templates *except* `templates/shared/worktree-setup.md`'s deliberately retained binary-absent fallback; `atomic doctor` and `atomic validate` run green using the locally built binary (rule A1 lints the new `atomic repo init` citations against `cliusage`) |

## Change tree

```
atomic/
├── internal/
│   ├── repoinit/                              A  new package: Init + effect checks + appends
│   │   ├── repoinit.go                        A
│   │   └── repoinit_test.go                   A
│   └── cliusage/cliusage.go                   M  add {repo, init} entry
├── cmd/atomic/main.go                         M  repo command build + dispatch
.gitignore                                     M  drop the fifteen .claude/* lines (three spans)
.claude/.gitignore                             A  tracked; translated rules
templates/commands/subagent-implementation.md  M  gitignore-verify step → best-effort init call
templates/commands/subagent-diagnose.md        M  same, two sites
templates/commands/setup-wiki.md               M  gitignore-audit rows delegate to init
templates/shared/worktree-setup.md             M  init-first; append fallback retained
templates/commands/atomic-help.md              M  cli topic mentions atomic repo init
commands/*.md                                  M  rendered outputs (make render)
atomic/internal/embedded/                      M  regenerated bundle (make bundle)
```

## Outline

- `atomic/internal/repoinit/repoinit.go`
  - `Init` — run the six guarantees in order against a resolved root; return per-item outcomes
  - `Action` — one item's outcome: what it names, `created` or `ok`
  - ignored-check helper — effect check via `git check-ignore`, literal-line fallback when git unavailable
  - append helper — add a rule line to an ignore file without touching existing content
- `atomic/internal/repoinit/repoinit_test.go`
  - cold-repo scaffold, second-run idempotency, pre-existing-effective-rules no-append, no-git degradation, append preserves content
- `atomic/cmd/atomic/main.go`
  - repo command build — `repo` parent + `init` child, mirrors docker wiring
  - repo dispatch — resolve root via `repoctx.Resolve(repoOverride)`, call `repoinit.Init`, print actions
- `atomic/internal/cliusage/cliusage.go`
  - `{repo, init}` entry — verb path, description
- `.claude/.gitignore` — translated rule block (Repo migration section)
- `.gitignore` — fifteen-line removal, three spans
- template files — per-row edits from the Template strip table

## Flows

1. **User runs `atomic repo init`**: resolve root (`repoctx.Resolve`, honors `--repo`) → for each
   of the six guarantees: check (dir exists / ignore effective) → create dir or append rule when
   missing, else no-op → print one `created`/`ok` line per item → exit 0 (non-zero only on I/O
   error).
2. **Command template needs the layout** (e.g. `/subagent-implementation` scratchpad setup):
   `command -v atomic` succeeds → run `atomic repo init` (quiet, idempotent) → `mkdir -p` the
   task-specific leaf dir → proceed. Binary absent → `mkdir -p` the leaf dir only (degradation
   path).
3. **Degraded environment** (no git binary or root not a work tree): `Init` answers "is it
   ignored" by scanning the target ignore file for the literal rule line; everything else
   unchanged.

## Out of scope

- New doctor check for the layout (issue asks existing gates green, nothing more).
- Managing `settings.local.json` or artifact dirs (`agents/`, `commands/`, `skills/`) in the
  generic scaffold — see design "Minimal managed rule set".
- Removing redundant root rules in user repos (init is append-only).

## Change log

- 2026-07-09 — Initial spec (autopilot, issue #125).
- 2026-07-09 — Correction: `## Checkpoints` table header didn't match the required `# | Checkpoint | Files/areas | … | Verifies` ordered-subsequence schema (rule S5), so `atomic validate spec` FAILed. Discovered running `atomic validate` per CP3's own done-when bar. Reformatted the existing three rows to the canonical 4-column shape — no content removed, `Files/areas` extracted from what was already named in the prose.
- 2026-07-09 — Implemented (autopilot). CP1 `repoinit` package + wiring, CP2 repo migration, CP3
  template strip all delivered and reviewer-approved with zero findings; full suite green
  (`go test` 51 pkgs, vet, gofmt, render+bundle drift gates, `atomic validate` 0 FAIL,
  `atomic doctor` 0 FAIL, end-to-end cold/second-run verified in a scratch repo). Pre-existing
  discovery filed as follow-up `cli-repo-flag-never-parses`: the global `--repo` flag never
  parses on any verb (`DisableFlagParsing`), so `repo init` — wired through the same
  `repoctx.Resolve` seam as its siblings — currently resolves from cwd like they do.
- 2026-07-09 — Rebased onto `next` after the artifact renames (issue #124, PR #139) and the wiki
  scan commit-flow fix (PR #137). Body references updated to the new names: the Template strip
  rows and Change tree now name `templates/commands/setup-wiki.md` (was `atomic-setup.md`) and
  `templates/commands/retrospective-learning.md` (was `atomic-improve.md`); the worktree-setup
  fallback invokes the `atomic-git-discipline` skill (was `atomic-commit`). Semantics unchanged —
  rename sweep only.

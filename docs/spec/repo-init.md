# Spec: `atomic repo init`

Design: [`docs/design/repo-init.md`](../design/repo-init.md). GitHub issue: #125.

## Contract

New binary verb `atomic repo init`, implemented in a new internal package
`atomic/internal/repoinit`, wired like `dockerinit` (internal package → `main.go` dispatch →
`cliusage` entry). Repo root resolution reuses the existing `repoctx.Resolve(repoOverride)`
helper — the same seam every other repo-scoped verb uses — so the verb honors the global
`--repo` flag like the rest of the surface.

Idempotent, non-destructive, never commits. `<state-root>` is the repository-state root that
`config.RepositoryStateDir` resolves (`.atomic` by default; ladder in
`docs/spec/atomic-state-and-config.md`). Before the guarantees, the verb runs the one-time
`atomic state migrate` out of legacy `.claude`, so the layout lands in the new root. Guarantees,
in order:

1. `<state-root>/.scratchpad/` directory exists (`mkdir -p` equivalent).
2. `<state-root>/project/` directory exists.
3. `<state-root>/.scratchpad/` is git-ignored, else append managed rule `/.scratchpad/` to
   `<state-root>/.gitignore` (create the file if absent).
4. `<state-root>/.atomic-index/` is git-ignored, else append managed rule `/.atomic-index/` to
   `<state-root>/.gitignore`.
5. `<state-root>/project/doc-surfaces.md` is git-ignored, else append managed rule
   `/project/doc-surfaces.md` to `<state-root>/.gitignore`. `atomic docs scan` rewrites that
   cache with a fresh timestamp on every run. When Git already tracks the file, the guarantee
   reports `tracked` with the remediation `git rm --cached <path>` and leaves the index alone.
6. `tmp/` is git-ignored, else append line `tmp/` to root `.gitignore` (create if absent).
7. `<state-root>/worktrees/` is git-ignored, else append managed rule `/worktrees/` to
   `<state-root>/.gitignore`.
8. When the state root is not `.claude` and the install ledger enrolls Claude or `.claude/`
   exists: `.claude/worktrees/` is git-ignored, else append managed rule `/worktrees/` to
   `.claude/.gitignore`.
9. `<state-root>/atomic.toml` declares `scope = "repo"`; a file declaring another scope is an
   error, never a rewrite.

A state root outside the repository gets no ignore rules: guarantees 3, 4, 5, and 7 are skipped.

After the guarantees, `repoInit` (`cmd_repo.go`) calls `convergeRepoCards` to publish the wiki
pointer cards from `<state-root>/rules/wiki/`:

- When `.claude/` exists or the install ledger enrolls Claude, it creates or adopts the
  `.claude/rules/atomic` link to `<state-root>/rules` and prints a `card link:` line for a
  created, adopted, or copied link, an appended ignore negation, or a link Git ignores.
- When the ledger enrolls OMP, it republishes the cards into the OMP profile and prints an
  `omp cards:` line.
- Either step also prints, under its own label (`card link` or `omp cards`), a
  `<label>: removed the retired card tree <path>` line for each retired tree it deleted, and a
  `<label>: kept <path>; Atomic cannot prove it wrote it, so delete it yourself if unwanted`
  line for each one it left in place (`printRetired`, `cmd_cards.go`).

"Is it ignored" is decided by effect: `git check-ignore -q --no-index` against a nonexistent
probe path under the directory in question (the probe string is the builder's choice), or
against the file itself for a single-file guarantee; `--no-index` lets a tracked file read as
covered by its rule. When the check says not ignored but the target ignore file already holds
the exact rule line, a later rule negates it: the guarantee reports `negated`, naming the file
and rule, and appends nothing. When git is
unavailable or the root is not a work tree, degrade to a literal line-presence scan of the
target ignore file (exact rule line, modulo surrounding whitespace). Named exception: the deterministic git path may be absent, so the
weaker literal fallback is deliberate.

Appends preserve existing content byte-for-byte: existing lines are never rewritten, reordered,
or removed. When creating `<state-root>/.gitignore` or `.claude/.gitignore` fresh, precede rules
with a single comment line `# managed by atomic repo init; rules are relative to <dir>/`, where
`<dir>` is that ignore file's repository-relative directory. When appending to an existing
file, append only the rule line(s), separated from existing content by a newline if the file does
not end with one.

Output: one line per item, `created <path-or-rule>` or `ok <path-or-rule>` (match existing verb
output tone, e.g. `dockerinit`), followed by any `card link:` and `omp cards:` lines. Exit 0
whether or not anything was created. Exit non-zero, naming the cause, on a migration error, an
ambiguous or refused state root, a scope marker that declares another scope, a card-publish
error, or an I/O error (unwritable file, etc.) with the failing path.

`cliusage` gains `{Path: ["repo", "init"], Args: "", Flags: nil, Description: "Scaffold the
state-root layout: dirs + state-root .gitignore + root ignore rules (idempotent)"}`. `--help` renders it
via the existing usage machinery.

## Template strip

Rule: templates stop doing layout-guarantee work (gitignore verify/append, standalone layout
mkdirs). Task-specific leaf `mkdir -p` stays. Each stripped site gains an init call: run
`atomic repo init`.

| Template | Change |
|----------|--------|
| `templates/commands/subagent-implementation.md` | Replace the "`.claude/.scratchpad/` must be gitignored — verify, add if missing" step with the init call. Keep `mkdir -p "$SCRATCH"`. |
| `templates/commands/subagent-diagnose.md` | Same replacement at both gitignore-verify sites. Keep the `mkdir -p` calls. |
| `templates/commands/setup-wiki.md` | Gitignore-audit rows for `tmp/`, `<state-root>/.scratchpad/`, the worktree directory, and the signals prev-file delegate to `atomic repo init`, with no manual append. Drop the `.claude/project/.deterministic-signals.prev.md` row entirely — no Go code writes that path anymore. |
| `templates/shared/worktree-setup.md` | Single `atomic repo init` call at the top of the create-and-enter step. No gitignore-verify section, no manual append fallback, no ignore-file commit ceremony. |
| `templates/commands/atomic-help.md` | Mention `atomic repo init` in the `cli` topic row set. |

`templates/commands/retrospective-learning.md` keeps its leaf
`mkdir -p` lines unchanged (task-specific, not layout setup). After template edits: `make render`
+ `make -C atomic bundle`; both drift gates must pass.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | `atomic/internal/repoinit` package + CLI wiring + tests | `atomic/internal/repoinit/`, `atomic/cmd/atomic/main.go`, `atomic/internal/cliusage/cliusage.go` | `go test ./...` green; `atomic repo init` runs idempotently in a scratch repo (second run all-`ok`); cold repo gets dirs + nested `<state-root>/.gitignore`; repo with pre-existing effective rules gets no appends; degraded (no git) path covered by test |
| 3 | Template strip + help router + render/bundle | `templates/commands/subagent-implementation.md`, `templates/commands/subagent-diagnose.md`, `templates/commands/setup-wiki.md`, `templates/shared/worktree-setup.md`, `templates/commands/atomic-help.md`, `commands/*.md`, `atomic/internal/embedded/` | All table rows applied; `make render` + `make bundle` clean; `/atomic-help` MISSING-scan passes; grep shows no remaining "verify … gitignored" prose in templates; `atomic doctor` and `atomic validate` run green using the locally built binary (rule A1 lints the new `atomic repo init` citations against `cliusage`) |

## Change tree

```
atomic/
├── internal/
│   ├── repoinit/                              A  new package: Init + effect checks + appends
│   │   ├── repoinit.go                        A
│   │   └── repoinit_test.go                   A
│   └── cliusage/cliusage.go                   M  add {repo, init} entry
├── cmd/atomic/main.go                         M  repo command build + dispatch
templates/commands/subagent-implementation.md  M  gitignore-verify step → init call
templates/commands/subagent-diagnose.md        M  same, two sites
templates/commands/setup-wiki.md               M  gitignore-audit rows delegate to init
templates/shared/worktree-setup.md             M  single init call; no fallback
templates/commands/atomic-help.md              M  cli topic mentions atomic repo init
commands/*.md                                  M  rendered outputs (make render)
atomic/internal/embedded/                      M  regenerated bundle (make bundle)
```

## Outline

- `atomic/internal/repoinit/repoinit.go`
  - `Init` — run the guarantees in order against a resolved root; return per-item outcomes
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
- template files — per-row edits from the Template strip table

## Flows

1. **User runs `atomic repo init`**: resolve root (`repoctx.Resolve`, honors `--repo`) → migrate
   legacy `.claude` state once when pending → resolve `<state-root>` → for each of the
   guarantees: check (dir exists / ignore effective / marker present) → create dir, append rule,
   or write marker when missing, else no-op → print one `created`/`ok` line per item →
   `convergeRepoCards`: ensure the `.claude/rules/atomic` link when `.claude/` exists or Claude
   is enrolled, republish OMP cards when OMP is enrolled, print `card link:` / `omp cards:` lines
   → exit 0 (non-zero on a migration error, an ambiguous or refused state root, a scope
   conflict, a card-publish error, or an I/O error).
2. **Command template needs the layout** (e.g. `/subagent-implementation` scratchpad setup):
   run `atomic repo init` (quiet, idempotent) → `mkdir -p` the task-specific leaf dir → proceed.
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
- 2026-07-12 — Worktree relocation. **What changed:** guarantee 6 now ensures `.claude/worktrees/`
  is ignored via managed rule `/worktrees/` in nested `.claude/.gitignore` (with the managed
  header on fresh creation), reported as `.claude/worktrees/ ignored`; the Template strip rows
  for `setup-wiki` and `worktree-setup` follow. **Why:** the worktree prescription moved from
  `.worktrees/` to `.claude/worktrees/` — Claude Code's native worktree home (`EnterWorktree`,
  `claude --worktree`) — so one directory serves both the implement loop and Claude Code's own
  tooling. **Superseded:** guarantee 6 appended line `.worktrees/` to root `.gitignore`. Existing
  root `.worktrees/` lines are never removed (init is append-only); legacy worktrees keep working.
- 2026-07-12 — worktree-setup drops the gitignore-verify ceremony. **What changed:** the
  `worktree-setup` Template strip row (and the matching Change tree / CP3 clauses) now prescribe a
  single best-effort `atomic repo init` call inside the create-and-enter step — no verify probe,
  no manual `/worktrees/` append, no ignore-file commit ceremony. **Why:** repo setup is
  `atomic repo init`'s concern, not the implement loop's, and the binary can be assumed present —
  these artifacts install only via `atomic claude install`. **Superseded:** the row kept a
  binary-absent manual append plus an `atomic-git-discipline` commit of the changed ignore file.
- 2026-10-19 — Artifacts assume atomic is installed. **What changed:** every stripped site,
  including `setup-wiki`'s gitignore-audit rows, runs `atomic repo init` with no
  `command -v atomic` check, and Flow 2 has no binary-absent branch. **Why:** issue #298:
  artifacts ship inside and install through the `atomic` binary, so the binary is always present
  when they run; see `docs/spec/artifacts-mermaid-flows.md`. **Superseded:** the init call ran
  only when `command -v atomic` succeeded and was skipped silently otherwise, leaf `mkdir -p` was
  the binary-absent degradation path, and `setup-wiki` kept a manual append for that case.
- 2026-11-11 — Reminders removed. **What changed:** the leaf `mkdir -p` note names only
  `retrospective-learning.md`. **Why:** `docs/spec/workflow-preferences.md` drops the reminder
  feature. **Removed:** `templates/commands/remind-me.md` from that note.

### 2026-11-11 — State root defaults to .atomic

**What changed:** The Contract guarantees, managed header, `cliusage` description, the
`setup-wiki` Template strip row, Checkpoint 1, the `Init` Outline entry, and Flow 1 name
`<state-root>` (`.atomic` by default) and `<state-root>/.gitignore`. Init runs the one-time
`atomic state migrate` first, ignores `<state-root>/worktrees/`, ignores `.claude/worktrees/`
through `.claude/.gitignore` when Claude is enrolled or `.claude/` exists, writes the
`scope = "repo"` marker, and skips state-root ignore rules for a root outside the repository.
After the guarantees, `convergeRepoCards` ensures the `.claude/rules/atomic` link and
republishes OMP cards, printing `card link:` and `omp cards:` lines. The exit clause covers a
migration error, an ambiguous or refused state root, a scope conflict, and a card-publish error.

**Why:** `docs/spec/omp-plugin-compatibility.md` moves repository state to the `.atomic`
default, with a resolution ladder and `atomic state migrate`.

**Superseded:** six guarantees that scaffolded and ignored `.claude/.scratchpad/`,
`.claude/project/`, `.claude/.atomic-index/`, and `.claude/worktrees/` through
`.claude/.gitignore`.

### 2026-11-12 — Ignore the doc-surfaces cache

**What changed:** Guarantee 5 ignores `<state-root>/project/doc-surfaces.md` through managed rule
`/project/doc-surfaces.md`, reported as `<state-root>/project/doc-surfaces.md ignored`; later
guarantees renumber. `atomic state migrate` writes the same rule. The ignored check probes the
file itself for this guarantee and runs `git check-ignore --no-index`. A tracked cache reports
`tracked` with a `git rm --cached` remediation; a rule a later line negates reports `negated`.
Each such action prints its detail on the line below it.

**Why:** `atomic docs scan` regenerates the cache with a timestamp on every run and nobody edits
it, so a tracked copy churns every commit.

**Superseded:** the cache was not ignored, so `repo init` left it to be committed; the ignored
check ran without `--no-index`, and a negated rule was re-appended and reported `created`.

### 2026-11-12 — Drop the one-time repo migration, document the retired-tree lines

**What changed:** The `## Repo migration (this repo, one-time)` section, its checkpoint 2 row (checkpoint 3 keeps its number), and its Change tree and Outline lines are gone. The card-publish step documents the `<label>: removed the retired card tree <path>` and `<label>: kept <path>; ...` lines that `printRetired` prints.

**Removed:** the section moving fifteen `.claude/*` lines from the root `.gitignore` into a tracked `.claude/.gitignore`. It was completed on 2026-07-09, and its target content no longer matches the repository.

**Why:** a completed one-time move is history, and the body is forward-only; `cmd_cards.go` prints the retired-tree lines and the body omitted them.

# Comment counter


Implementation contract for GitHub issue #269. Design: `docs/design/comment-counter.md`.


## Goal


`atomic code comments` lists every full-line comment a diff adds and exits non-zero when one exceeds `[comments] max_lines`; the implementer, reviewer, and auditor run it on their diff and the reviewer reports the count at every gate.


## Non-goals


- Trailing comments on a code line.
- Docstring string literals.
- Tree-sitter or the code-intel index; the verb runs on `git diff` alone.
- A CI gate. The exit code is for agents.
- Changing the prose rule in `agent-comment-discipline`; the counter is added beside it.
- Rewriting `docs/spec/comment-discipline.md`; it gets one pointer section and a log entry.


## Success criteria


- [ ] `[comments] max_lines` loads from `.claude/atomic.toml` as a known key; absent means 2; a value under 1 is a `Warning` and resolves to 2; `atomic doctor` category 13 reports the warning.
- [ ] `atomic code comments` with no flags counts the working tree against `HEAD`, including untracked files not covered by `.gitignore`; `--diff <range>` passes the range to `git diff -U0`; a range containing `..` skips the untracked pass.
- [ ] Each listed comment carries the repo-relative path, the first added line number, the line span, and the first words of its text with comment markers stripped; consecutive comment lines in one file are one entry.
- [ ] Line prefixes and block delimiters resolve by extension per the design table; files with an unknown extension, and `.md`, `.txt`, `.json`, are skipped; `//go:` and `#!` lines are not comments.
- [ ] Text output: `comments added: N (max_lines M)` then one line per comment with `OVER` on those exceeding the maximum; `--json` emits the object from the design with `diff`, `max_lines`, `added`, `over`, `comments[]`.
- [ ] Exit 0 when nothing exceeds the maximum, 1 when something does, 2 on a bad flag or a `git` error, with the error on stderr.
- [ ] The verb runs from a subdirectory, from a realm root, and in a repo with no index; it never opens the SQLite index.
- [ ] `atomic code --help` and `atomic --help` list the verb with `--diff` and `--json`; `TestDeriveCommandsGolden` passes; `atomic validate artifacts` passes on the corpus.
- [ ] `agent-comment-discipline` tells its consumers to run the counter on their diff and treat the list as a deletion worklist, skipping silently when `atomic` is absent; `atomic-implementer` names `--diff {BASE_SHA}` and reports `comments: N added` in its signals block; `atomic-reviewer` runs it in the code-quality pass, reports `comments: N added (M over max)` under Signals verified, and treats a count that contradicts the implementer's claim as 🔴; `atomic-auditor` runs it over `<loop-base>..HEAD` in the coherence pass.
- [ ] `context/skills/atomic-review/SKILL.md` Comment noise section instructs running the verb on the diff under review and walking its list, rather than reading the diff for comments by eye.
- [ ] `context/CLAUDE.md` lists the verb under Atomic binary and the key in the `.claude/atomic.toml` row; `context/commands/atomic-help.md` carries the verb in the binary job table and Stage 4; the help-coverage loop prints zero `MISSING:` lines.
- [ ] `docs/reference/atomic-toml.md` documents the key; `docs/reference/code-intel.md` documents the verb; `docs/spec/comment-discipline.md` gains a pointer section and a dated log entry; the root `CLAUDE.md` ownership bullet for `atomic-toml.md` names `[comments]`.
- [ ] `go test ./...`, `go vet ./...`, `gofmt -l .` clean; `atomic validate spec` and `atomic validate config` clean.


## Approach


Line scan over `git diff -U0`, comment syntax by extension, dispatched before realm and engine resolution; see `docs/design/comment-counter.md`.


## Change tree


```
atomic/
├── internal/config/
│   ├── repo.go .................................. M  (commentsSection, known keys, MaxLines resolution)
│   └── repo_test.go ............................. M  (key loads, default, invalid value warns)
├── internal/doctor/
│   ├── checks_repo_config.go .................... M  (warn on invalid max_lines)
│   └── checks_repo_config_test.go ............... M
├── internal/codeintel/cli/
│   ├── comments.go .............................. A  (verb: diff parse, classify, report)
│   ├── comments_test.go ......................... A  (fixture repos via git; classification table)
│   ├── code.go .................................. M  (early dispatch, usage line)
│   └── realm.go ................................. M  (early dispatch before realm resolve)
├── internal/cliusage/cliusage.go ................ M  (golden entry: code comments)
└── cmd/atomic/cmd_code.go ....................... M  (cobra registration: --diff, --json)
context/
├── _partials/
│   ├── agent-comment-discipline.md .............. M  (run the counter; list is a worklist)
│   ├── agent-implementer-workflow.md ............ M  (step: count before self-check)
│   └── agent-signals-output.md .................. M  (comments line in signals block)
├── agents/
│   ├── atomic-reviewer.md ....................... M  (code-quality step, Signals verified line, example)
│   └── atomic-auditor.md ........................ M  (coherence pass runs it over the range)
├── skills/atomic-review/SKILL.md ................ M  (Comment noise names the verb)
├── commands/atomic-help.md ...................... M  (binary job row, Stage 4 line)
└── CLAUDE.md .................................... M  (Atomic binary list, atomic.toml row)
docs/
├── reference/atomic-toml.md ..................... M  ([comments] max_lines)
├── reference/code-intel.md ...................... M  (comments verb)
└── spec/comment-discipline.md ................... M  (pointer section, log entry)
CLAUDE.md ........................................ M  (root, repo-only: atomic-toml.md ownership bullet)
```


## Outline


```
atomic/internal/config/repo.go
  commentsSection — the [comments] table, one leaf
  RepoConfig.Comments — field
  repoKnownSections, repoKnownLeaves — comments, comments.max_lines
  ResolveMaxLines — default when unset, warning and default when under 1

atomic/internal/doctor/checks_repo_config.go
  RunCheckRepoConfigWith — appends the max_lines warning

atomic/internal/codeintel/cli/comments.go
  runComments — flag parse, config load, git calls, report, exit code
  commentSyntax — line prefix and block delimiters for one extension group
  syntaxFor — extension to syntax, nil for skipped files
  parseAddedComments — diff text to comment entries
  scanFile — untracked file to comment entries
  comment — path, line, lines, text, over
  firstWords — marker-stripped, whitespace-collapsed, truncated text

atomic/internal/codeintel/cli/comments_test.go
  classification table — one case per extension group, directive, block, gap
  working tree vs HEAD — tracked edit plus untracked file counted
  commit range — untracked file not counted
  exit codes — 0, 1 over max, 2 bad range
  json shape — fields present

atomic/internal/codeintel/cli/code.go
  RunCode — comments case before engine.New
  printCodeUsage — comments line

atomic/internal/codeintel/cli/realm.go
  RunCodeWithRealm — comments case before realm.Resolve, root via repoctx

atomic/cmd/atomic/cmd_code.go
  buildCodeCmd — addSub comments with --diff, --json

atomic/internal/cliusage/cliusage.go
  commands — code comments entry

context/_partials/agent-comment-discipline.md
  Comment discipline — bullet: count with the verb, list is a worklist

context/_partials/agent-implementer-workflow.md
  Workflow — step 4c: run the counter on the base diff, delete, re-run

context/_partials/agent-signals-output.md
  Signals — comments line

context/agents/atomic-reviewer.md
  Workflow code-mode step 7 — run the counter, compare to the claim
  Output format — comments line in Signals verified, in the example too

context/agents/atomic-auditor.md
  Cross-iteration coherence — run the counter over the range

context/skills/atomic-review/SKILL.md
  Comment noise — one sentence naming the verb

context/commands/atomic-help.md
  Binary subcommands — job row
  Stage 4 — one line

context/CLAUDE.md
  Atomic binary — comments in the code verb list
  Where things live — [comments] max_lines in the atomic.toml row

docs/reference/atomic-toml.md
  Keys table — row
  [comments] max_lines — section

docs/reference/code-intel.md
  The verbs — row
  Counting comments — short section with the output shape

docs/spec/comment-discipline.md
  Deterministic gate — pointer to this spec
  Change log — dated entry

CLAUDE.md (root)
  Documentation surfaces ownership — atomic-toml.md bullet names [comments]
```


## Flows


**Flow: implementer self-check**

1. Implementer finishes the iteration and the tests pass.
2. Implementer runs `atomic code comments --diff {BASE_SHA}`.
3. For each listed entry, the implementer deletes the comment unless it carries what the code cannot say; entries marked `OVER` are shortened or moved to a doc the code cites.
4. Implementer re-runs the verb and writes the final count into its signals block.

**Flow: reviewer gate**

1. Reviewer runs `atomic code comments --diff <base>` in the code-quality pass.
2. Each listed entry is judged against the discipline; a restatement is a 🟡 finding with the location from the list, `OVER` entries are 🟡 at the floor.
3. Reviewer writes `comments: N added (M over max)` under Signals verified; a count that contradicts the implementer's claim is 🔴.

**Flow: auditor accumulation**

1. Auditor runs `atomic code comments --diff <loop-base>..HEAD` in the coherence pass.
2. The total is the accumulation no single review saw; entries the reviewer passed per iteration are re-read as one list.

**Flow: the verb**

1. User or agent runs `atomic code comments [--diff <range>] [--json]`.
2. Dispatcher resolves the git root and loads `.claude/atomic.toml` for `max_lines`.
3. Verb runs `git diff -U0` for the range; in working-tree mode it also lists untracked files and scans them whole.
4. Added lines are classified by the extension's syntax into comment entries.
5. Verb prints the report and exits 0, 1 when an entry exceeds the maximum, 2 on a git or flag error.


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | `[comments] max_lines` key with default and doctor warning | `atomic/internal/config/repo.go`, `repo_test.go`, `atomic/internal/doctor/checks_repo_config.go`, `checks_repo_config_test.go` | atomic-implementer (mode: surgical) | ~4 | config tests: loads, defaults, warns; doctor test: WARN on `max_lines = 0` |
| 2 | `atomic code comments` verb, dispatch, cobra, cliusage golden | `atomic/internal/codeintel/cli/comments.go`, `comments_test.go`, `code.go`, `realm.go`, `atomic/cmd/atomic/cmd_code.go`, `atomic/internal/cliusage/cliusage.go` | atomic-implementer (mode: feature) | ~6 | cli tests on git fixture repos; `TestDeriveCommandsGolden`; hand-run on this repo |
| 3 | Agent, skill, help, and global-contract wiring | `context/_partials/agent-comment-discipline.md`, `agent-implementer-workflow.md`, `agent-signals-output.md`, `context/agents/atomic-reviewer.md`, `atomic-auditor.md`, `context/skills/atomic-review/SKILL.md`, `context/commands/atomic-help.md`, `context/CLAUDE.md` | atomic-implementer (mode: feature) | ~8 | `make -C atomic bundle` clean; `atomic validate artifacts` clean; help-coverage loop zero `MISSING:` |
| 4 | Reference docs and prior spec pointer | `docs/reference/atomic-toml.md`, `docs/reference/code-intel.md`, `docs/spec/comment-discipline.md`, root `CLAUDE.md` | atomic-implementer (mode: feature) | ~4 | `atomic validate spec` clean; pages read in `atomic-writing` voice |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| A hunk that lands inside a pre-existing block comment is classified as code | low | Block state resets per file; the case undercounts, never fails a clean diff |
| `//` inside a string at line start (`"// not a comment"`) counts as a comment | low | A trimmed line starting with a quote is not a comment prefix match; documented as a line scan |
| Agents run the verb and ignore the list | med | The reviewer's reported count is visible in every gate; a mismatch with the implementer's claim is 🔴 |
| `max_lines = 2` fails godoc blocks longer than two lines | med | That is the discipline: the why in two lines, the rest in a doc the code cites; repos that disagree set the key |
| `#` prefix counts YAML and TOML comments in config files | low | Those are comments too; the reader decides, and the count is per diff |


## Change log

<!-- Populated on first amendment after the spec is approved. Do not log drafting/refinement turns. -->


## Implementation log


### v1 — 2026-09-20

Built across 4 checkpoints and one post-audit fix iteration of /autopilot (issue #269). Commits (chronological):

- `d5b6b0b` — CP-1 `[comments] max_lines` key, default 2, doctor WARN under 1
- `f462578` — CP-2 `atomic code comments` verb, dispatch before engine and realm resolution, cobra and cliusage entries
- `3ebee4c` — CP-3 partial bullet, implementer step, reviewer and auditor lines, review skill, help router, global contract
- `749e1a5` — CP-4 atomic-toml and code-intel reference pages, comment-discipline spec pointer, root ownership bullet
- `0c9ab1d` — docs: reviewer row in the agents reference
- `c8cb65b` — audit fix: repeated why trimmed, godoc form, test-file convention, output column alignment, duplicate run instructions collapsed

**Out-of-scope work performed during this build:**

- Two checkpoint 1 godocs over the maximum were shortened in checkpoint 2 so the loop range passes the verb it ships.

**Unforeseens — surprises that emerged during implementation:**

- The verb run over its own loop range reported 22 comments at first pass; the audit found the same why repeated across four files. Final range: 15 added, 0 over.
- Entry type is `commentEntry`, not the outline's `comment`; disclosed rename.

**Deferred items still open:**

- none

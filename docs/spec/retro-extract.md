# atomic retro extract


## Goal


`atomic retro extract` turns every Claude Code session modified since a date into one numbered markdown file (or N size-balanced shards) holding only user-typed text, slash-command invocations, and the assistant's `Skill` / `Agent` calls. `/retrospective-learning` reads that file instead of raw `.jsonl`, runs its scanners on Sonnet, and takes `file:line` references instead of re-typed quotes.


## Non-goals


- No change to what the retrospective does with findings: tiers, the indexed walk, the run log, the learnings file.
- No assistant prose or thinking in the output; the assistant side is reduced to `Skill` / `Agent` tool calls.
- No `subagents/` transcripts.
- No `--json` output; markdown only.
- No writes to `~/.atomic/retro-runs/`; the extractor only reads it for the default date.
- No projects root other than `$HOME/.claude/projects`.


## Success criteria


- [ ] `atomic retro extract --since 2026-08-19 -o out.md` walks every `~/.claude/projects/*/` dir, reads only `*.jsonl` files that are direct children of a project dir, and keeps those whose mtime is at or after local midnight of the date.
- [ ] With no `--since`, the date is the `run_ts` of the newest `~/.atomic/retro-runs/*.json` (by name); with no such file, 30 days before now, and stderr names the fallback date either way.
- [ ] `--project <glob>` keeps only project dirs whose basename matches a shell-style glob (`*`, `?`, `[...]`).
- [ ] Each kept session with at least one entry is one section: a heading with the session date and the project directory (`cwd` of the first row that carries one, else the slug), then a line with the session id, first and last timestamp over all user/assistant rows, and the entry count. Sessions are ordered by first timestamp. Timestamps are the rows' UTC values passed through unchanged: RFC 3339 in the session line, `MM-DD HH:MM` in entry prefixes. Only the `--since` boundary is local midnight.
- [ ] A user row is dropped when any of: `isSidechain`, `isMeta`, `isCompactSummary` is true; its content array holds a `tool_result` block; its text, after `<system-reminder>…</system-reminder>` blocks are removed, is empty or opens with one of `<local-command-stdout>`, `<local-command-caveat>`, `<task-notification>`, `<bash-input>`, `<bash-stdout>`, `<bash-stderr>`, `<agent-message`, `<cross-session-message`.
- [ ] A user row holding `<command-name>` or `<command-message>` renders as exactly one line `/<name> <args>` (args from `<command-args>`, omitted when empty; a leading `/` in the name is not doubled). No other text from that row survives.
- [ ] Every other kept user row renders as `[MM-DD HH:MM] user: <text>`; text longer than 800 characters is cut at a rune boundary and ends with `…[+N chars]`; continuation lines of a multi-line message are indented four spaces.
- [ ] Every assistant `tool_use` block named `Skill` renders as `[MM-DD HH:MM] skill: <skill> <args>` and every one named `Agent` as `[MM-DD HH:MM] agent: <subagent_type> — <description>`; other tool calls, `text`, and `thinking` blocks produce nothing.
- [ ] Every line of every output file is prefixed with its own physical line number, right-aligned to width 5, then ` | `, so `sed -n '<N>p' <file>` returns the line whose prefix reads `N`.
- [ ] Rows longer than 64 KiB parse; a malformed row is skipped and counted, never fatal.
- [ ] `--shards N` with `-o out.md` writes `out.1.md` … `out.K.md` where `K = min(N, sessions)`, sessions are never split across files, assignment balances rendered byte size, each file is chronological within itself and its header names `shard i/K`. `--shards` without `-o` is a usage error.
- [ ] Without `-o`, the single file goes to stdout. The summary always goes to stderr: files scanned with total bytes, projects scanned, sessions kept, unparsable rows, and per output file its path, byte size, and line count.
- [ ] Exit codes: 0 success; 2 usage (unparsable `--since`, `--shards < 1`, `--shards` without `-o`, unknown verb or flag); 1 runtime (projects dir unreadable, output unwritable).
- [ ] The built binary run over the real `~/.claude/projects` (about 130 files, 740 MB) finishes in under 30 seconds; input and output sizes and the wall time are recorded in the implementation log, with no transcript text.
- [ ] `atomic retro extract` appears in `cliusage`'s golden table, `TestDeriveCommandsGolden` and the root verb-count test pass, and `atomic validate artifacts` accepts every citation of the verb in the artifacts.
- [ ] `context/commands/retrospective-learning.md` runs the extractor in pre-flight (`--shards 4`, into the scratchpad), briefs one Sonnet scanner per shard file to answer with `file:line + category` and no re-typed quotes, runs the prior-retro auditor on Sonnet, and recovers each cited line with `sed` in Step 4. Scope wording no longer says "last 5 sessions" or reads `SESSIONS_DIR`.
- [ ] The verb is discoverable: `context/commands/atomic-help.md` names it in the retrospective topic row, the binary jobs table, and tour stage 4; `context/CLAUDE.md`'s atomic-binary list carries it; `docs/reference/retro.md` exists, is in the VitePress sidebar and the binary table of `docs/reference/commands.md`, and has a row in the root `CLAUDE.md` documentation-surfaces table. The help-coverage loop in `CLAUDE.md` prints no `MISSING:` line.
- [ ] `go test ./...`, `go vet ./...`, and `gofmt -l .` are clean; `npm run docs:build` succeeds.


## Approach


A Go package streaming each session file once through a struct decoder that names only the fields the filter reads, wired as a `retro` verb family with one `extract` verb — see `docs/design/retro-extract.md`.


## Change tree


```
atomic/
├── internal/retro/                              A  new package
│   ├── extract.go                               A  walk, file filter, row rules → sessions
│   ├── extract_test.go                          A
│   ├── render.go                                A  markdown, numbering, truncation, sharding
│   ├── render_test.go                           A
│   ├── lastrun.go                               A  default --since from ~/.atomic/retro-runs
│   ├── lastrun_test.go                          A
│   └── testdata/projects/                       A  synthetic sessions, one row per shape
├── internal/cliusage/cliusage.go                M  {retro, extract} golden entry
├── internal/cliutil/
│   ├── usage.go                                 M  skip flags with an empty usage string
│   └── usage_test.go                            M
└── cmd/atomic/
    ├── main.go                                  M  AddCommand(buildRetroCmd())
    ├── main_test.go                             M  root verb list gains "retro"
    ├── cmd_retro.go                             A  buildRetroCmd, retroAction
    └── cmd_retro_test.go                        A
context/
├── commands/retrospective-learning.md           M  pre-flight extract, Sonnet scanners, line refs
├── commands/atomic-help.md                      M  retrospective row, binary jobs row, tour stage 4
└── CLAUDE.md                                    M  atomic-binary bullet
docs/
├── reference/retro.md                           A  verb reference
├── reference/commands.md                        M  binary family row
└── reference/workflow.md                        M  retrospective paragraph names the extractor
.vitepress/config.mts                            M  reference sidebar entry
CLAUDE.md                                        M  documentation-surfaces row
README.md                                        M  further-reading row
```


## Outline


```
atomic/internal/retro/extract.go
  Options — since, project glob, projects dir
  Session — project dir, id, first/last timestamp, entries, rendered size
  Entry — kind (user | command | skill | agent), timestamp, text
  Extract — walk project dirs, apply the file filter, collect sessions in first-timestamp order
  parseSession — stream one file with a growable line reader, apply the row rules, track the span
  processUserRow — drop rules, <system-reminder> stripping, leading-tag check, command detection
  processAssistantRow — Skill / Agent tool_use blocks to entries
  decodeContent — string or text-block content to one string; tool_result detection
  parseCommand — <command-name> / <command-message> / <command-args> to one line

atomic/internal/retro/render.go
  Render — sessions to one numbered markdown document with a header
  Shard — size-balanced assignment of sessions to K files, chronological within each
  truncate — 800-character cap with the …[+N chars] marker
  number — prefix every physical line with its own line number

atomic/internal/retro/lastrun.go
  LastRunSince — run_ts of the newest retro-runs file, or absent

atomic/internal/retro/extract_test.go
  row rules — one case per fixture shape from the design table
  file filter — mtime boundary, direct children only, --project glob
  large row — a 100 KiB tool_result row parses; a malformed row is counted

atomic/internal/retro/render_test.go
  numbering — prefix equals physical line; sed-style lookup round-trips
  truncation — rune boundary, marker text, multi-line indentation
  sharding — never splits a session, balances size, caps K at session count

atomic/internal/retro/lastrun_test.go
  newest by name — run_ts read; missing dir reported as absent

atomic/internal/cliutil/usage.go
  SetUsage — omit flags declared with an empty usage string, so a short alias stays out of the Options listing

atomic/cmd/atomic/cmd_retro.go
  buildRetroCmd — cobra parent `retro` with child `extract`, flags declared for cliusage
  retroAction — flag parsing, since resolution, extract, render or shard, write, summary; returns the exit code

atomic/cmd/atomic/cmd_retro_test.go
  dispatch — usage errors exit 2, sandboxed HOME end-to-end writes the file, stdout default, shard naming

context/commands/retrospective-learning.md
  Pre-flight — run the extractor into $SCRATCH; drop SESSIONS_DIR
  Step 1 — scope wording: sessions since the last run
  Step 2b — one Sonnet scanner per shard, file:line answers, artifact attribution by cited line
  Step 2c — prior-retro auditor dispatched on Sonnet, same brief shape
  Step 4 — sed recovery of cited lines before categorizing

context/commands/atomic-help.md
  retrospective topic row — names the extractor
  Binary subcommands jobs table — row for extracting session history
  Tour stage 4 — one line for atomic retro extract

context/CLAUDE.md
  Atomic binary — bullet for atomic retro extract

docs/reference/retro.md
  What it is — purpose and the retrospective's use of it
  Usage — flags, defaults, exit codes
  Output — the file shape and the file:line convention
  What is dropped — the row table

docs/reference/commands.md
  Binary subcommands — atomic retro row

docs/reference/workflow.md
  Retrospective stage — one clause naming the extractor, linked to retro.md

.vitepress/config.mts
  Reference sidebar — Retro entry

CLAUDE.md
  Documentation surfaces — docs/reference/retro.md row

README.md
  Further reading — Retro row
```


## Flows


**Flow: extract to one file**

1. agent runs `atomic retro extract --since 2026-08-19 -o out.md`
2. CLI resolves `$HOME/.claude/projects`; unreadable → exit 1
3. CLI lists each project dir, takes `*.jsonl` direct children whose mtime ≥ local midnight of the date, and applies `--project` to the dir basename
4. for each file, `parseSession` streams rows; user rows pass the drop rules or render as `user:` / `/verb` entries; assistant rows contribute `skill:` / `agent:` entries; every user/assistant row extends the session span
5. sessions with at least one entry are sorted by first timestamp and rendered: document header, one section per session, every line prefixed with its physical number
6. CLI writes the file, prints the summary to stderr, exits 0

**Flow: default date**

1. agent runs `atomic retro extract -o out.md`
2. CLI reads the newest `~/.atomic/retro-runs/*.json` by name and takes its `run_ts`
3. none found → 30 days before now
4. stderr prints `since: <date> (<last retro run | no retro run log, 30-day default>)`, then the extract proceeds as above

**Flow: shards**

1. agent runs `atomic retro extract --shards 4 -o history.md`
2. CLI extracts as above, then `Shard` assigns sessions largest-first to the lightest of `K = min(4, sessions)` bins
3. each bin is ordered chronologically, rendered with `shard i/K` in its header, numbered from 1, and written to `history.i.md`
4. the summary lists each file with its size and line count

**Flow: retrospective history scan**

1. `/retrospective-learning` pre-flight runs `atomic retro extract --shards 4 -o "$SCRATCH/history.md"`; `atomic` absent → announce and skip the history scan
2. Step 2b dispatches one Sonnet `general-purpose` runner per `history.<i>.md`, briefed to return `file:line | category | recurrence | atomic_meta | meta_target` rows where `meta_target` cites the `skill:` / `agent:` / `/verb` line that precedes the finding
3. Step 4 runs `sed -n '<line>p' <file>` for every cited line and rejects rows whose line does not exist
4. categorization continues unchanged from the recovered quotes


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | `internal/retro` package: file walk and filter, row rules, session span, render with numbering and truncation, sharding, last-run date. Synthetic fixtures carrying one row per shape in the design's row table; no real transcript text. | `atomic/internal/retro/*.go`, `atomic/internal/retro/testdata/` | atomic-implementer (mode: feature) | ~7 | `go test ./internal/retro/...`: every row in the design table lands in the right disposition; a 100 KiB row parses; malformed row counted; mtime boundary at local midnight; `subagents/` file ignored; glob on slug; numbering round-trips through a line lookup; truncation marker and rune safety; shards never split a session and `K` caps at the session count; `LastRunSince` reads `run_ts` and reports absence |
| 2 | CLI verb: `buildRetroCmd` + `retroAction`, `cliusage` golden entry, root verb list, sandboxed-HOME end-to-end. | `atomic/cmd/atomic/cmd_retro.go`, `cmd_retro_test.go`, `main.go`, `main_test.go`, `atomic/internal/cliusage/cliusage.go` | atomic-implementer (mode: feature) | ~5 | `go test ./cmd/atomic/...`: `TestDeriveCommandsGolden` and the verb-count test pass with `retro`; exit 2 for bad `--since`, `--shards 0`, `--shards` without `-o`, unknown verb; under `t.Setenv("HOME", …)` a fixture project yields a written file, stdout output without `-o`, `out.1.md`/`out.2.md` with `--shards 2`; stderr summary names the fallback date. Then, by the orchestrator: `make -C atomic build` and `time bin/atomic retro extract --since 2026-08-19 -o tmp/trash/real.md` over the real projects dir, recording sizes and wall time in `STATE.md` |
| 3 | Artifacts and docs: retrospective command reshaped, help router rows, CLAUDE.md bullet, reference page, commands table row, sidebar, documentation-surfaces row. | `context/commands/retrospective-learning.md`, `context/commands/atomic-help.md`, `context/CLAUDE.md`, `docs/reference/retro.md`, `docs/reference/commands.md`, `.vitepress/config.mts`, `CLAUDE.md` | atomic-implementer (mode: feature) | ~7 | help-coverage loop prints nothing; `grep -n 'SESSIONS_DIR\|last 5' context/commands/retrospective-learning.md` is empty; `bin/atomic validate artifacts` clean after `make -C atomic build`; `npm run docs:build` green with `/reference/retro` in the sidebar; `make -C atomic bundle` succeeds; `grep -rn 'retro extract' context/ docs/ CLAUDE.md README.md` shows every surface the change tree names (the root `CLAUDE.md` verification-before-commit sweep) |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| A `tool_result` row exceeds `bufio.Scanner`'s 64 KiB default and aborts the file | high | read lines with a growable buffer; a fixture row over 64 KiB is a test |
| Decoding 740 MB row by row is slow enough to annoy | med | decode into a struct naming only the read fields; measure on the real projects dir before ship and record the timing |
| Claude Code changes a row shape (new wrapper tag, new flag) and typed text leaks or is lost | med | the drop set and tag list are one table in `extract.go` next to the fixture that exercises it; a leak shows up as a new leading tag in the extract, which the reference page tells the user to report |
| `--project` glob written against a path (`*/foo*`) silently matches nothing | low | the reference page and `--help` say the match is on the slug, and the summary reports zero sessions kept |
| Truncation cuts a long correction the retrospective needed | low | 800 characters holds several typed paragraphs; the marker shows the cut so the orchestrator can open the raw file by session id |
| A parallel branch registers another binary subcommand in the same files (`main.go`, `main_test.go`, `cliusage.go`, `atomic-help.md`) | high | additive, single-line insertions at the end of each list; rebase before merge |


## Change log

### 2026-09-20 — Correction: body aligned with the delivered code

**What changed:** The session heading takes the `cwd` of the first row that carries one, not the first row of the file. The Outline names `processUserRow`, `processAssistantRow`, and `decodeContent` in place of three finer pieces the code folds into them. The Change tree and Outline add `atomic/internal/cliutil/usage.go` and its test, `docs/reference/workflow.md`, and `README.md`.

**Why:** Code diverged during the build. Real sessions open with bookkeeping rows that carry no `cwd`, so the first-row rule showed the slug; the `-o` alias printed as a `--o` long flag until `cliutil.SetUsage` learned to skip empty-usage flags; the workflow page and README rows were added at the docs step. Found by the audit over the loop range.


## Implementation log

### shipped — 2026-09-20

Built across 8 iterations of /autopilot. Commits (chronological):

- `9463907b` — design and spec
- `a5633abc` — CP-1 `internal/retro`: extract, render, sharding, last-run date
- `28afabb9` — CP-2 `atomic retro extract` verb, cliusage entry, `cliutil.SetUsage` empty-usage skip
- `cbc26831` — CP-3 retrospective command, help router, reference page, CLAUDE.md rows, sidebar, README
- `b302aa5e` — docs step: workflow page clause, doc-surfaces cache
- `a4249a80` — audit fixes: spec correction, reference page claims, `PROJECT_SLUG` restored
- `3d3163f1` — follow-up deferral

**Out-of-scope work performed during this build:**

- `cliutil.SetUsage` omits flags with an empty usage string, so the `-o` alias no longer prints as a `--o` long flag; no other verb registers such a flag.
- `docs/reference/workflow.md` retrospective paragraph names the extractor.

**Unforeseens — surprises that emerged during implementation:**

- Real sessions open with bookkeeping rows that carry no `cwd`; the heading rule moved to the first row that has one.
- `atomic validate artifacts` only knows long flags, so artifacts cite `--out` while the reference page documents `-o`, `--out`.
- Full-suite runs alongside the docs build timed out two daemon tests in untouched packages; both pass in isolation.

**Deferred items still open:**

- `retro-extract-f-1`: `atomic validate artifacts` never parses a fenced block indented under a list item (pre-existing).

# atomic retro extract

`atomic retro extract` turns every Claude Code session modified since a date into one numbered markdown file (or N size-balanced shards) holding only user-typed text, slash-command invocations, and the assistant's `Skill` / `Agent` calls. `/retrospective-learning` reads that file instead of raw `.jsonl`, so its scanners run on a few hundred KB of signal instead of hundreds of megabytes of tool traffic, and cite a `file:line` instead of re-typing a quote.

    atomic retro extract --since 2026-08-19 --out out.md
    atomic retro extract --shards 4 --out "$SCRATCH/history.md"
    #   -> history.1.md … history.4.md, one shard per scanner


## Usage

    atomic retro extract [--since <date>] [--project <glob>] [--shards N] [-o <file>]

| Flag | Default | What it does |
|------|---------|--------------|
| `--since` | newest `~/.atomic/retro-runs/*.json`'s `run_ts`, else 30 days before now | Keep sessions modified at or after local midnight of this date. Accepts `YYYY-MM-DD` or RFC 3339. |
| `--project` | none | Keep only project dirs whose slug matches this shell-style glob (`*`, `?`, `[...]`). |
| `--shards` | 0 (single file) | Split output into N size-balanced files. Requires `-o`; each session stays whole, never split across shards. |
| `-o`, `--out` | stdout | Write to this file instead of stdout. With `--shards N`, the stem and extension of this path name `<stem>.1<ext>` … `<stem>.K<ext>`, where K is N capped at the number of sessions kept. |

The extractor never writes to `~/.atomic/retro-runs/`. It only reads the newest file there for the `--since` default, and a file it cannot parse counts as absent. Whichever date it picks, `--since`, the last run, or the 30-day fallback, it names the source on stderr:

    since: 2026-08-19 (last retro run)
    since: 2026-07-21 (no retro run log, 30-day default)

`--project` matches the project directory's slug (the session's `cwd` with every `/` folded to `-`), not the real path. A glob written against a path, `*/foo*`, matches nothing; the summary then reports zero sessions kept.


## Output shape

One numbered markdown document, one section per session, sessions ordered by first timestamp:

```
    1 | # retro extract — since 2026-08-19 — 12 sessions, 6 projects — shard 2/4
    2 |
    3 | ## 2026-09-06 · /Users/example/projects/widget
    4 | session: 6f2c-... · first: 2026-09-06T20:44:43Z · last: 2026-09-06T21:03:11Z · 9 entries
    5 |
    6 | [09-06 20:44] /model
    7 | [09-06 20:45] user: make the fix without touching the fixture
    8 | [09-06 20:45] agent: atomic-implementer — Fix the failing test
    9 | [09-06 20:51] user: no, I said without touching the fixture. …[+412 chars]
```

Every physical line carries its own right-aligned line number, so `sed -n '9p' out.md` returns line 9 exactly. A scanner answers `history.2.md:9 correction` instead of retyping the quote; the orchestrator recovers the text with the same `sed` call. Messages over 800 characters are cut at a rune boundary with a `…[+N chars]` marker; continuation lines of a multi-line message are indented four spaces.


## What is dropped

| Row | Disposition |
|-----|-------------|
| Tool output threaded as a user row (`tool_result` block) | drop |
| Subagent traffic (`isSidechain`) | drop |
| Skill-prompt expansion, local-command caveat, image note (`isMeta`) | drop |
| Compaction summary (`isCompactSummary`) | drop |
| Local command echo, background-task notice, bash mode, peer-session message (leading `<local-command-stdout>`, `<local-command-caveat>`, `<task-notification>`, `<bash-input>`, `<bash-stdout>`, `<bash-stderr>`, `<agent-message`, `<cross-session-message`) | drop |
| `<system-reminder>…</system-reminder>` block inside typed text | strip the block, keep the rest |
| Slash command (`<command-name>` / `<command-message>`, optional `<command-args>`) | one line, `/name args` |
| Typed text | keep, truncated at 800 characters |
| Assistant `Skill` / `Agent` tool call | one line each |
| Assistant `text`, `thinking`, any other tool call | drop |

Subagent transcripts live one directory deeper, at `<slug>/<uuid>/subagents/`, and are never read: only files directly inside a project directory count.


## Exit codes

| Code | Meaning |
|------|---------|
| 0 | success |
| 1 | runtime error (projects dir unreadable, output unwritable) |
| 2 | usage error (unparsable `--since`, `--shards < 1`, `--shards` without `--out`, unknown verb or flag) |

Rows up to 16 MiB parse. A malformed row is skipped and counted in the stderr summary, never fatal; a row past 16 MiB ends that session's scan, counts as one unparsable row, and the session's remaining rows are lost.


## What bites

Claude Code adds new wrapper tags around injected content from time to time. A tag the extractor does not recognize renders as a plain `user: <tag ...` line instead of being dropped or turned into a clean entry. A shard showing raw tags means the drop list in `extract.go` needs a new entry; file it with `/report-issue-with-atomic`.

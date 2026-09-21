# Comment counter


Design for GitHub issue #269. Replaces prose-only comment discipline with a deterministic count that the implementer, reviewer, and auditor each run on their diff.


## Problem


Agents ship comments that restate the code, and every gate passes them. The rule in `context/_partials/agent-comment-discipline.md` was written for #112, rewritten in #201, and hardened on 2026-08-21 (readability as a defect class). Seven user complaints across four repos followed, three of them right after an `atomic-reviewer` pass.

The pattern: the reviewer reads a diff as prose and judges each comment on its own. A comment that reads fine in isolation still adds up to noise, and nothing in the loop counts. A number is harder to rationalize than a paragraph. `docs/design/comment-discipline.md` holds the placement rationale for the prose rule; this design adds the counter beside it.


## Goals / Non-goals


- Goals:
    - One verb that lists every comment a diff adds, with `path:line`, line span, and first words, and exits non-zero when one exceeds a configured maximum.
    - A repo-scoped maximum in `.claude/atomic.toml`.
    - Implementer, reviewer, and auditor each run it on their diff and act on the list; the reviewer reports `comments: N added (M over max)` at every gate.
- Non-goals:
    - Judging whether a comment is needed. The verb counts; the agent decides. Zero is the stated goal, so every listed entry is a deletion candidate.
    - Trailing comments on a code line (`x := 1 // why`). Detecting them means parsing string literals per language. Full-line comments are what the complaints are about.
    - Docstring literals (Python `"""`). They are strings, not comments, and public-API docstrings are required by the discipline.
    - Tree-sitter. The index is optional and the verb must work without it. A line scan over `git diff` is enough.
    - CI enforcement. The exit code is for agents; nothing in the loop fails a build on it.


## Approaches


| # | Approach | Pros | Cons |
|---|----------|------|------|
| A | Line scan over `git diff -U0`, comment syntax by file extension | No index needed; works on any repo; ~200 lines; untracked files countable | Misses trailing comments; block-comment state across hunk boundaries is unknowable |
| B | Tree-sitter `comment` nodes from the code-intel extractor, diffed against the base | Exact per language; trailing comments included | Requires an index at both base and head; 31 grammar hooks to wire; the verb dies when the index is cold |
| C | More prose in the partial | Zero code | Three rewrites already failed |


## Recommendation


A. The counter has to run on every iteration in repos with no index, and the failure being fixed is full-line narration, which a line scan catches.

Verb shape, from the issue:

```
atomic code comments [--diff <range>] [--json]
```

`--diff` is handed to `git diff -U0` unchanged. Default `HEAD`: the working tree against the last commit, which is what the implementer and reviewer both look at (`git diff {BASE_SHA}` in the reviewer brief). A range with `..` compares commits, which is the auditor's `<loop-base>..HEAD`. In working-tree mode, untracked files not covered by `.gitignore` count whole: the implementer's new files are unstaged when the reviewer runs, and a counter that cannot see them is the same hole the prose rule has.

Config:

```toml
[comments]
max_lines = 2
```

Default 2 when the key is absent. The discipline says the fewest lines that carry the why; two lines is a sentence with a clause. A value under 1 warns and falls back to the default, same lenient contract as the other keys (`docs/reference/atomic-toml.md`).

Comment detection, per added line, by extension:

```
extension group            line prefix   block delimiters
slash (go js ts tsx jsx    //            /*  */
  java kt c cc cpp h hpp
  cs swift rs scala dart
  php m mm proto groovy)
hash (py rb sh bash zsh    #
  yaml yml toml pl r ex
  exs ps1)
dash (sql lua hs elm)      --
percent (erl hrl)          %
markup (html vue svelte                  <!--  -->
  xml)
style (css scss less)      // (scss,     /*  */
                           less only)
unknown, md, txt, json     skipped
```

A line is a comment line when, after trimming, it starts with the line prefix or a block opener, or the scanner is inside an open block. `//go:` directives and `#!` shebangs are not comments. Consecutive added comment lines in one file form one comment; a gap in line numbers or a code line closes it. Block state resets at every file header, because `-U0` shows no context and a hunk landing inside a pre-existing block is invisible; that case undercounts rather than miscounts.

Diff parsing:

```
for each line of `git diff -U0 --no-color --no-ext-diff <range> --`:
  "diff --git"      -> close open comment, reset block state
  "+++ b/<path>"    -> current file = path; "+++ /dev/null" -> skip file
  "@@ -a,b +c,d @@" -> next new-line number = c
  "+<text>"         -> classify at that line number, advance
  anything else     -> ignore
```

Output, text mode:

```
comments added: 2 (max_lines 2)
atomic/internal/x.go:3   1 line    Greet returns a greeting for name.
atomic/internal/x.go:8   3 lines   This block handles the retry path when the upstream call tim   OVER
```

`comments added: 0` and nothing else when the diff adds none. Exit 0 when no comment exceeds the maximum, 1 when one does, 2 on a usage error or a `git` failure.

Output, `--json`:

```json
{
  "diff": "HEAD",
  "max_lines": 2,
  "added": 2,
  "over": 1,
  "comments": [
    {"path": "atomic/internal/x.go", "line": 12, "lines": 1, "text": "Greet returns a greeting for name.", "over": false},
    {"path": "atomic/internal/x.go", "line": 40, "lines": 4, "text": "This block handles the retry path when...", "over": true}
  ]
}
```

Where it runs in the dispatcher: before `realm.Resolve` and before `engine.New`, the same early exit `mcp --daemon` takes. The verb reads git, not the index, so a realm root and a repo with no index both work; the git root comes from `repoctx.Resolve`.

Consumers:

| Agent | Diff | Action |
|-------|------|--------|
| `atomic-implementer` | `--diff {BASE_SHA}` after the tests pass | Delete every listed comment that the code already says; report the count in its signals block |
| `atomic-reviewer` | `--diff <base>` in the code-quality pass | Each entry is a finding unless it carries what the code cannot; `comments: N added (M over max)` in the signals block; a claimed count that does not match is 🔴 |
| `atomic-auditor` | `--diff <loop-base>..HEAD` in the coherence pass | The range total is the accumulation no single review saw |
| `atomic-review` skill | whatever the conversation is reviewing | Walk the list instead of eyeballing the diff |

The instruction lives once, in `agent-comment-discipline`, which all three agents already compose. Each agent adds only the line that names its diff and its report field.


## Open questions


- None. The issue fixed the shape; the only free choice was the default, and 2 follows the issue's own example.

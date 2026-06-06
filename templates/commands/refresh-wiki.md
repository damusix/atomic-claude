---
description: Refresh the project wiki incrementally. Runs atomic wiki scan, checks freshness with atomic wiki stale, re-authors only stale or pending artifacts, stamps fingerprints via code, and offers to commit the wiki when done.
---

`/refresh-wiki [root]` — refresh the wiki rooted at `[root]` (default: `./wiki/` relative to cwd). Run this to keep the wiki current after repos are added, signals are updated, or the drift marker appears.

<workflow>

## Step 1 — Resolve root and pre-flight

Resolve the wiki root:

- If `[root]` was supplied, use it. Otherwise default to `./wiki/` relative to cwd.

Check that the `atomic` binary is available:

```bash
command -v atomic
```

If not found, stop:

```
atomic binary not found on $PATH.
install: curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash
then re-run /refresh-wiki.
```

## Step 2 — Scan (deterministic)

Run the scan step to scaffold any missing structure, classify repos, and register the wiki:

```bash
atomic wiki scan --root <resolved-root>
```

Parse the stdout handoff:

- **Summary line** — `<N> repos · <M> indexed · <K> pending`
- **Per-repo list** — one line per member: `<status> <path> [→ signals path]`
- **NEXT STEPS** block — names each `pending` repo

Record the member list (path, status) for use in the incremental pass.

If `atomic wiki scan` exits non-zero, stop and surface the error verbatim. The most common cause is a `wiki/` directory with a missing or malformed `index.md` — the error message names the path.

## Step 3 — Stale check (deterministic)

Run the freshness check:

```bash
atomic wiki stale --root <resolved-root>
```

Parse the stdout line-by-line. Each line has a literal prefix:

- `DRIFT added <path>` — new member not yet in the scan block
- `DRIFT removed <path>` — member that has disappeared
- `DRIFT status <path> <old>→<new>` — member whose classification changed
- `STALE summary <path>` — summary whose `reflects_rev` no longer matches the repo's HEAD
- `STALE concern <path> (<repo>)` — concern whose `reflects:` entry for `<repo>` no longer matches

Exit codes: `0` = fully fresh, `1` = one or more stale/drift lines, `2` = hard error (stop and surface verbatim).

If exit code is `0` and there are no `pending` members from Step 2: print `wiki is up to date.` and stop — nothing to do.

## Step 4 — Pending repos: offer /refresh-signals (judgment)

If there are `pending` repos from the Step 2 scan:

Present them as a numbered list and ask the user which ones to handle via `/refresh-signals` (which will produce signals files and promote them to `indexed`). Repos not selected here go to the `atomic-signals-inferrer` wiki-output pass in Step 5.

```
Pending repos (no signals.md found):

  1. /path/to/repo-a
  2. /path/to/repo-b
  3. /path/to/repo-c

Which repos do you want to refresh via /refresh-signals first?
(This runs the full signals pipeline for those repos and promotes them to `indexed`.)

Type indices (e.g. 1 3), a range (1-3), `all`, or `none` to skip:
```

Accepted input: space-separated indices, comma-separated, ranges (`1-3`), `all`, `none`, or `all except N`.

Ask the user to run `/refresh-signals` in each selected repo, then confirm once done before continuing. An executing agent cannot run slash commands programmatically — the user must do this step. Once signals are written, those repos become `indexed`; update your internal member list accordingly.

Unselected `pending` repos continue to Step 5 as candidates for wiki-mode summarization.

## Step 5 — Incremental pass (judgment)

Work through each artifact that needs authoring or re-authoring. Preserve fresh artifacts — do not re-author them.

For each artifact, determine its disposition:

- **SKIPPED (fresh)** — not flagged stale by Step 3 and not pending → preserve, no action.
- **NEW** — a `pending` repo (not handled via `/refresh-signals` in Step 4) that has no summary yet.
- **RE-AUTHORED** — flagged `STALE summary` or `STALE concern` by Step 3.

### 5a — Pending repo summaries (NEW)

For each `pending` repo not selected in Step 4:

Dispatch `atomic-signals-inferrer` in wiki-output mode:

```
subagent_type: "atomic-signals-inferrer"
prompt:
  target_repo: <abs-path-to-repo>
  wiki_dir: <abs-path-to-wiki-root>
```

Wait for the agent to complete. The inferrer writes EITHER a single file `<wiki-root>/repos/<repo-name>.md` (small repo) OR multiple files under `<wiki-root>/repos/<repo-name>/<domain>.md` (large repo, domain-split). Check which shape was produced.

After the agent returns, stamp each summary file the inferrer produced for that repo:

```bash
# For a single-file summary:
atomic wiki stamp <wiki-root>/repos/<repo-name>.md --repo <target_repo>

# For domain-split summaries (repeat for each file):
atomic wiki stamp <wiki-root>/repos/<repo-name>/<domain>.md --repo <target_repo>
```

Use the same `<target_repo>` absolute path that was passed to the inferrer — the command already knows it; do not read it from any frontmatter field.

If the repo has no commits yet (`git rev-parse HEAD` fails), `atomic wiki stamp` handles this gracefully — the summary is written but `reflects_rev` is left unset until the repo has commits.

Print: `NEW  <summary-file-path>`

### 5b — Stale summaries (RE-AUTHORED)

For each `STALE summary <path>` line from Step 3:

The `STALE summary <path>` line from Step 3 gives you the summary file path. Derive the corresponding repo path from the path structure: `repos/<repo-name>.md` or `repos/<repo-name>/<domain>.md` — the `<repo-name>` segment identifies the member whose `path` was recorded during `atomic wiki scan`. Look up that path in your member list from Step 2. Dispatch `atomic-signals-inferrer` in wiki-output mode the same way as 5a, passing that repo path as `target_repo`.

After the agent returns, stamp each summary file the inferrer produced, using the `target_repo` path from your member list:

```bash
atomic wiki stamp <summary-file-path> --repo <target_repo>
```

Print: `RE-AUTHORED  <summary-file-path>`

### 5c — Stale concerns (RE-AUTHORED)

For each `STALE concern <path> (<repo>)` line from Step 3:

Re-synthesize the concern document. Read the cited repos' current signals files and summary files to understand the current state. Rewrite `<path>` with fresh content that reflects the current state of those repos.

After rewriting, identify all repo IDs cited in the updated concern and run the stamp step:

```bash
atomic wiki stamp <concern-file-path> --root <resolved-root> --cites <repo-id-1>,<repo-id-2>,...
```

The `--cites` ids are the repo identifiers (base directory names) of the repos whose state the concern reflects. Code computes and writes every fingerprint value — do not write fingerprints manually.

Print: `RE-AUTHORED  <concern-file-path>`

## Step 6 — Summary disposition report

After all artifacts are processed, print a per-artifact disposition table:

```
Wiki refresh — disposition:

  NEW           <wiki-root>/repos/repo-a.md
  RE-AUTHORED   <wiki-root>/repos/repo-b/api.md
  SKIPPED (fresh)  <wiki-root>/repos/repo-c.md
  RE-AUTHORED   <wiki-root>/concerns/cross-cutting-auth.md

<N> new, <M> re-authored, <K> skipped.
```

## Step 7 — Clear drift marker and re-scan (only on full completion)

Only if Steps 2-6 all completed without error (no hard exit, no aborted dispatch):

Remove the drift marker:

```bash
rm -f <resolved-root>/.dirty
```

Re-run the scan to bump the `generated` date in `index.md`, which resets the neglect timer:

```bash
atomic wiki scan --root <resolved-root>
```

If any step earlier encountered an error or was aborted, leave `.dirty` set — the neglect nudge will continue to fire until a clean run completes.

## Step 8 — Offer to commit

Once the drift marker is cleared, offer to commit the wiki repository (the wiki is its own git repo under `<resolved-root>/`):

```
Wiki refresh complete. Commit the wiki?
- Yes — stage and commit (message generated by atomic-commit skill)
- No, skip
```

Use `AskUserQuestion` for this prompt.

On "Yes": stage all changes under `<resolved-root>/`, invoke the `atomic-commit` skill for the commit message format, and commit. The wiki is its own git repo — commit inside it, not in the parent repo.

On "No, skip": print `wiki updated. commit when ready.` and stop.

</workflow>

<constraints>

## Rules

- Never write fingerprint values manually. Always invoke `atomic wiki stamp` after writing a summary or concern. The model supplies which repos are cited; code computes every `reflects_rev` and `reflects:` value. **Why:** code-written fingerprints are verifiable; LLM-authored ones drift silently.
- Preserve fresh artifacts. An artifact not flagged by `atomic wiki stale` is `SKIPPED (fresh)` — do not re-author it. **Why:** unnecessary rewrites thrash git history and may introduce drift into summaries the code has marked stable.
- Clear `.dirty` only on full completion. A partial or aborted run leaves the marker set. **Why:** the forcing function relies on `.dirty` persisting until the wiki is genuinely clean; clearing it early hides real drift.
- Commit is offered, not automatic. The user decides when to commit the wiki. **Why:** axiom 3 — operations that affect shared state (a git commit) require explicit user confirmation.
- Both `target_repo` and `wiki_dir` must be passed together to `atomic-signals-inferrer`. If the inferrer reports a partial-args error, surface it verbatim and stop the current artifact's pass. **Why:** the inferrer refuses and names the missing arg — this is the prompt-level guard; the command always supplies both.
- Surface all `atomic wiki scan` and `atomic wiki stale` errors verbatim. Do not paraphrase exit-code 2 output. **Why:** paraphrased errors drop the exact token needed to diagnose the root cause.

</constraints>

---

## How the incremental pass works

Each run only does the minimum necessary work:

- **Fresh artifacts** (`atomic wiki stale` emits no line for them) are untouched — the code has verified they reflect the current state.
- **Pending repos** that the user routes to `/refresh-signals` graduate to `indexed` and never need a wiki summary (signals are richer than summaries).
- **Pending repos** routed to the inferrer get a new summary stamped with the current HEAD — they become `summarized`.
- **Stale summaries and concerns** are re-authored by the inferrer and re-stamped — only these incur inference cost.

The result: a wiki that stays current with minimal LLM work on each run.

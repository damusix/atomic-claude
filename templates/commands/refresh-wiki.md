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

## Step 3 — One-time bucket offer (judgment)

Check whether `wiki/index.md` already contains a `<wiki-buckets` block:

```bash
grep -q '<wiki-buckets' <resolved-root>/wiki/index.md && echo "present" || echo "absent"
```

If the block is **present** (populated or declined): skip this step entirely — the offer never re-fires.

If the block is **absent**: present the following offer to the user:

```
This wiki has no capture buckets registered yet.

Buckets are named folders at the realm root (e.g. research/, raw/, tickets/)
that you want the wiki to synthesize into knowledge pages.

Would you like to create any capture buckets now?
Enter bucket names (space-separated, e.g. "research raw tickets"), or press Enter to skip:
```

**On named input** (user supplies one or more names):

For each bucket name, run:

```bash
atomic wiki bucket add --root <resolved-root> <name>
```

`bucket add` creates the bucket folder, the manifest dir (`wiki/.buckets/<name>/`), registers a `<bucket name="<name>" path="<abs-path>"/>` entry in the `<wiki-buckets>` block in `wiki/index.md`, and writes the `## Capture surfaces` section (with `<!-- describe what this bucket is for -->` placeholder) to the realm `CLAUDE.md`.

**On blank/skip input** (user presses Enter or types nothing): record the decline by writing a `<wiki-buckets declined="true"></wiki-buckets>` block into `wiki/index.md`. Use an Edit to insert the block after the last line of content (or append if file ends without a trailing newline). This prevents the offer from re-firing on future runs.

## Step 4 — Placeholder fill (judgment)

If new buckets were just created in Step 3, ask the user what each bucket is for (or infer from their Step 3 answers if they described purpose while naming). Then:

1. Replace the `<!-- describe what this bucket is for -->` placeholder in the realm `CLAUDE.md` `## Capture surfaces` section for each bucket.
2. Write the bucket's `index.md` purpose line and update the `## Conventions` block to describe naming and content conventions for that bucket.

Code writes structure; the model writes meaning. A bucket whose placeholder survives this step (user declined to describe it) is flagged in the Step 11 disposition output as `PLACEHOLDER (unfilled)` — it is not a blocker for the rest of the run.

## Step 5 — Stale check (deterministic)

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
- `STALE bucket <name>` — bucket whose diff work list is non-empty

Exit codes: `0` = fully fresh, `1` = one or more stale/drift lines, `2` = hard error (stop and surface verbatim).

If exit code is `0` and there are no `pending` members from Step 2 and no newly created buckets from Step 3: print `wiki is up to date.` and stop — nothing to do. (The newly-created-buckets guard is load-bearing for a just-created empty bucket: it has no files yet, so `atomic wiki stale` emits no `STALE bucket` line for it, but its placeholder fill in Step 4 is still pending. Without this guard, the run would exit here and the placeholder would never be offered.)

## Step 6 — Pending repos: offer /refresh-signals (judgment)

If there are `pending` repos from the Step 2 scan:

Present them as a numbered list and ask the user which ones to handle via `/refresh-signals` (which will produce signals files and promote them to `indexed`). Repos not selected here go to the `atomic-signals-inferrer` wiki-output pass in Step 7.

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

Unselected `pending` repos continue to Step 7 as candidates for wiki-mode summarization.

## Step 7 — Incremental repo pass (judgment)

Work through each repo artifact that needs authoring or re-authoring. Preserve fresh artifacts — do not re-author them.

For each artifact, determine its disposition:

- **SKIPPED (fresh)** — not flagged stale by Step 5 and not pending → preserve, no action.
- **NEW** — a `pending` repo (not handled via `/refresh-signals` in Step 6) that has no summary yet.
- **RE-AUTHORED** — flagged `STALE summary` or `STALE concern` by Step 5.

### 7a — Pending repo summaries (NEW)

For each `pending` repo not selected in Step 6:

**Code-intel index sync (best-effort).** Before dispatching the inferrer, check whether the member repo has an existing index:

```bash
test -f <member-repo-path>/.claude/.atomic-index/atomic.db
```

- **Warm** (DB exists) → run an incremental sync so the inferrer sees fresh call/import edges:

  ```bash
  atomic --repo <member-repo-path> code sync
  ```

  The `--repo` global flag goes BEFORE `code`. On any non-zero exit, skip silently and continue to the inferrer dispatch — the inferrer degrades to heuristic summarization.

- **Cold** (no DB) → do NOT auto-index and do NOT prompt. Indexing is opt-in: the user runs `atomic --repo <member-repo-path> code index` themselves when they want graph-grounded summaries. The inferrer will degrade to summary-without-graph for this repo.

- **`atomic` binary absent** → skip the sync step silently, proceed to the inferrer dispatch.

Code-intel grounding is best-effort per repo. A repo without an index still gets summarized via heuristics — absence of an index is never a blocker for the wiki refresh.

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

### 7b — Stale summaries (RE-AUTHORED)

For each `STALE summary <path>` line from Step 5:

The `STALE summary <path>` line from Step 5 gives you the summary file path. Derive the corresponding repo path from the path structure: `repos/<repo-name>.md` or `repos/<repo-name>/<domain>.md` — the `<repo-name>` segment identifies the member whose `path` was recorded during `atomic wiki scan`. Look up that path in your member list from Step 2.

**Code-intel index sync (best-effort).** Apply the same warm/cold/absent logic as 7a using the resolved `target_repo` path before dispatching the inferrer.

Dispatch `atomic-signals-inferrer` in wiki-output mode the same way as 7a, passing that repo path as `target_repo`.

After the agent returns, stamp each summary file the inferrer produced, using the `target_repo` path from your member list:

```bash
atomic wiki stamp <summary-file-path> --repo <target_repo>
```

Print: `RE-AUTHORED  <summary-file-path>`

## Step 8 — Bucket synthesis phase (judgment + deterministic)

For each bucket registered in `wiki/index.md` (read the `<wiki-buckets>` block entries):

**8a — Check diff (deterministic).** Run the diff to build the work list and write `wiki/.buckets/<name>/current`:

```bash
atomic wiki bucket diff --root <resolved-root> <name>
```

- Exit 0 → diff is empty → bucket is fresh. Print `SKIPPED (fresh)  <name>` in the disposition output. Skip to the next bucket.
- Exit 1 → diff is non-empty → proceed to synthesis.
- Exit 2 (hard error) → surface verbatim, mark bucket synthesis failed for this bucket, continue to the next.

**8b — Dispatch inferrer for synthesis (judgment).** For each bucket with a non-empty diff, dispatch `atomic-signals-inferrer` in bucket-synthesis mode:

```
subagent_type: "atomic-signals-inferrer"
prompt:
  bucket_name: <name>
  bucket_path: <abs-path-to-bucket-folder>
  wiki_dir: <abs-path-to-wiki-root>
```

The inferrer reads the bucket's `index.md` (conventions context) and the new/changed files from the diff as content context, and writes or updates `wiki/knowledge/<topic>.md` page(s). It reports back the pages it wrote and the source files that contributed to each page.

If the inferrer returns a partial-args error, surface it verbatim and mark synthesis failed for this bucket.

**8c — Stamp knowledge pages (deterministic).** After the inferrer returns successfully, stamp each reported knowledge page. For each page, build the `--sources` argument by reading `wiki/.buckets/<name>/current` (written in step 8a) to look up the SHA-256 for each source file the inferrer reported:

The `current` file format is `<relpath>\t<sha256hex>` per line (relative to the bucket folder). For each source file path the inferrer reported as contributing to a page, find the matching line in `current` and format it as `<bucket-name>/<relpath>@<sha256hex>`. Pass all contributing entries for the page as a comma-separated `--sources` argument:

```bash
atomic wiki stamp <wiki-root>/knowledge/<topic>.md --knowledge --sources <bucket-name>/<relpath1>@<sha256hex1>,<bucket-name>/<relpath2>@<sha256hex2>,...
```

The model never computes hashes — hashes come from `current` which code wrote. If a source file reported by the inferrer has no matching line in `current` (e.g. a removed file), skip that entry in `--sources`.

**8d — Promote on success (deterministic).** Only when synthesis AND stamping both completed without error:

```bash
atomic wiki bucket promote --root <resolved-root> <name>
```

A failed synthesis or stamping leaves the bucket un-promoted so the diff persists for the next run.

Print for synthesized buckets:
```
SYNTHESIZED  <name>  → <wiki-root>/knowledge/<topic1>.md, <wiki-root>/knowledge/<topic2>.md
```

## Step 9 — Stale concerns (RE-AUTHORED)

For each `STALE concern <path> (<repo>)` line from Step 5:

Re-synthesize the concern document. Read the cited repos' current signals files and summary files to understand the current state. Rewrite `<path>` with fresh content that reflects the current state of those repos.

After rewriting, identify all repo IDs cited in the updated concern and run the stamp step:

```bash
atomic wiki stamp <concern-file-path> --root <resolved-root> --cites <repo-id-1>,<repo-id-2>,...
```

The `--cites` ids are the repo identifiers (base directory names) of the repos whose state the concern reflects. Code computes and writes every fingerprint value — do not write fingerprints manually.

Print: `RE-AUTHORED  <concern-file-path>`

## Step 10 — Linkify wiki artifacts (deterministic)

After every summary, concern, and knowledge page has been authored AND stamped (Steps 7–9), render path citations into navigable relative markdown links:

```bash
atomic wiki linkify --root <resolved-root>
```

This runs **after** stamping, so it never disturbs `reflects_*` fingerprints or `atomic wiki stale` verdicts (staleness is HEAD/hash-based, not body-based). Base resolution: `repos/**` summaries use each summary's `repo:` frontmatter dir; `concerns/*.md` and `index.md` use the realm root. It is idempotent — re-running produces byte-identical files; fenced code blocks are never touched. A `[text](path)` link is not an `@-ref`.

## Step 11 — Summary disposition report

After all artifacts are processed, print a per-artifact disposition table:

```
Wiki refresh — disposition:

  NEW                  <wiki-root>/repos/repo-a.md
  RE-AUTHORED          <wiki-root>/repos/repo-b/api.md
  SKIPPED (fresh)      <wiki-root>/repos/repo-c.md
  RE-AUTHORED          <wiki-root>/concerns/cross-cutting-auth.md
  SYNTHESIZED          research → <wiki-root>/knowledge/vendor-x.md
  SKIPPED (fresh)      raw
  PLACEHOLDER (unfilled)  tickets

<N> new, <M> re-authored, <K> skipped, <L> synthesized, <P> placeholder.
```

## Step 12 — Clear drift marker and re-scan (only on full completion)

Only if Steps 2–11 all completed without error (no hard exit, no aborted dispatch):

Remove the drift marker:

```bash
rm -f <resolved-root>/wiki/.dirty
```

Re-run the scan to bump the `generated` date in `index.md`, which resets the neglect timer:

```bash
atomic wiki scan --root <resolved-root>
```

If any step earlier encountered an error or was aborted, leave `.dirty` set — the neglect nudge will continue to fire until a clean run completes.

## Step 13 — Offer to commit

Once the drift marker is cleared, offer to commit the wiki repository (the wiki is its own git repo under `<resolved-root>/wiki/`):

```
Wiki refresh complete. Commit the wiki?
- Yes — stage and commit (message generated by atomic-commit skill)
- No, skip
```

Use `AskUserQuestion` for this prompt.

On "Yes": stage all changes under `<resolved-root>/wiki/`, invoke the `atomic-commit` skill for the commit message format, and commit. The wiki is its own git repo — commit inside it, not in the parent repo.

On "No, skip": print `wiki updated. commit when ready.` and stop.

</workflow>

<constraints>

## Rules

- Never write fingerprint values manually. Always invoke `atomic wiki stamp` after writing a summary, concern, or knowledge page. The model supplies which repos/sources are cited; code computes every `reflects_rev`, `reflects:`, and `sources:` value. **Why:** code-written fingerprints are verifiable; LLM-authored ones drift silently.
- Read hashes for `--sources` from `wiki/.buckets/<name>/current`, never compute them. The `current` file is written by `atomic wiki bucket diff` — its lines are the authoritative `<relpath>\t<sha256hex>` entries. **Why:** the model has no reliable way to compute SHA-256; `current` is deterministic and already written.
- Preserve fresh artifacts. An artifact not flagged by `atomic wiki stale` is `SKIPPED (fresh)` — do not re-author it. **Why:** unnecessary rewrites thrash git history and may introduce drift into summaries the code has marked stable.
- Promote a bucket only on per-bucket synthesis success. A failed synthesis or stamp leaves the bucket un-promoted so the diff persists. **Why:** partial synthesis would advance the baseline past unconsumed files, silently dropping them from future work lists.
- The bucket offer fires once per realm. The `<wiki-buckets>` block records both the populated state and the `declined="true"` state. Once the block exists, skip Step 3 entirely. **Why:** re-prompting on every run would be disruptive; the block is the durable opt-in/opt-out record.
- Clear `.dirty` only on full completion. A partial or aborted run leaves the marker set. **Why:** the forcing function relies on `.dirty` persisting until the wiki is genuinely clean; clearing it early hides real drift.
- Commit is offered, not automatic. The user decides when to commit the wiki. **Why:** axiom 3 — operations that affect shared state (a git commit) require explicit user confirmation.
- Both `target_repo` and `wiki_dir` must be passed together to `atomic-signals-inferrer` (wiki mode). Both `bucket_name`, `bucket_path`, and `wiki_dir` must all be passed for bucket-synthesis mode. If the inferrer reports a partial-args error, surface it verbatim and stop the current artifact's pass. **Why:** the inferrer refuses and names the missing arg — this is the prompt-level guard; the command always supplies all required args.
- Surface all `atomic wiki scan` and `atomic wiki stale` errors verbatim. Do not paraphrase exit-code 2 output. **Why:** paraphrased errors drop the exact token needed to diagnose the root cause.

</constraints>

---

## How the incremental pass works

Each run only does the minimum necessary work:

- **Fresh artifacts** (`atomic wiki stale` emits no line for them) are untouched — the code has verified they reflect the current state.
- **Pending repos** that the user routes to `/refresh-signals` graduate to `indexed` and never need a wiki summary (signals are richer than summaries).
- **Pending repos** routed to the inferrer get a new summary stamped with the current HEAD — they become `summarized`.
- **Stale summaries and concerns** are re-authored by the inferrer and re-stamped — only these incur inference cost.
- **Buckets with a non-empty diff** are synthesized into knowledge pages, stamped, and promoted — only the changed files are passed as context.
- **Fresh buckets** (empty diff) are skipped, consistent with all other fresh artifacts.

The result: a wiki that stays current with minimal LLM work on each run.

# Realm-scope wiki pipeline

Full pipeline for realm-scope inference. Root: `<realm-root>/wiki/` (a separate git repo). Executed by `atomic-wiki-inferrer` in two modes:

- **Wiki-output mode** — activated by `target_repo` + `wiki_dir` dispatch args. Summarizes a single member repo into `wiki/repos/`.
- **Bucket-synthesis mode** — activated by `bucket_name` + `bucket_path` + `wiki_dir` dispatch args. Synthesizes capture-bucket content into `wiki/knowledge/`.

The realm orchestration (scan, stale check, interactive offers, linkify, commit) is handled by the `/refresh-wiki` command. This file covers only the inference-heavy sub-pipelines that the agent executes.

---

## Wiki-output mode

Activated when the caller provides **both** `target_repo` and `wiki_dir`. If exactly one is present, stop immediately:

```
ERROR: wiki-output mode requires both target_repo and wiki_dir.
Missing: <whichever is absent>.
Aborting — not falling back to default signals mode.
```

When both are present, run this pipeline instead of the repo-scope default pipeline. The default pipeline is not executed.

### W1 — Guard: read-only on target_repo

`target_repo` is explored **read-only**. No writes, no edits, no file creation anywhere inside it. The only write destination is `wiki_dir/repos/`. This mode is exempt from the repo-scope scope rule: it never writes to `target_repo`'s `docs/wiki/` and never wires `@-refs`.

### W2 — Obtain deterministic substrate

Run `atomic signals scan` scoped to `target_repo`, writing output to a temporary directory outside it:

```bash
cd <target_repo> && atomic signals scan --out <tmp_dir>
```

where `<tmp_dir>` is a fresh temporary directory outside `target_repo` (e.g., a `os.MkdirTemp`-equivalent path). The scan must be rooted at `target_repo` so the substrate reflects that repo's files — not the current working directory or wiki dir. The output goes to `<tmp_dir>` instead of into `target_repo`'s `docs/wiki/`, which is never written to.

Read the resulting `<tmp_dir>/docs/wiki/scan.md` as the substrate for inference.

### W3 — Infer domain partitioning

Apply the same domain-partitioning logic as the repo-scope Step 3 (vertical slices by functional concern). Read source files in `target_repo` as needed to verify structure — read-only.

**Size heuristic:**

- **Small repo** (≤ 3 inferred domains or ≤ ~1,000 total lines of significant source): write a single summary file at `wiki_dir/repos/<repo-name>.md`.
- **Large repo** (> 3 domains or > ~1,000 lines): write one file per domain at `wiki_dir/repos/<repo-name>/<domain>.md`.

`<repo-name>` is the base name of `target_repo` (e.g., `target_repo = /home/user/projects/myapp` → `<repo-name> = myapp`).

### W4 — Dispatch sub-agents per domain

Dispatch one `atomic-wiki-writer` per member repo, naming that type explicitly — omitting `subagent_type` falls back to `general-purpose`, which carries no `skills:` frontmatter. Same dispatch logic as the repo-scope Step 4, with two differences:

1. Sub-agents read from `target_repo` read-only and write their domain output to `wiki_dir/repos/<repo-name>/` (or single file for small repos). They do NOT write into `target_repo`.
2. **Omit the `<concerns_format>` block from sub-agent prompts.** Wiki mode never surfaces concerns (W7 explicitly excludes them), so including the block wastes tokens generating output that is immediately discarded.

Only one pipeline reference is loaded per run, so the sub-agent instructions are restated here rather than cross-referenced into `repo.md`:

```
<instructions>
- Summaries are FACTS about current state — not instructions, rules, or intent. Every sentence must be verifiable by reading a source file in target_repo.
- Read the actual source files. Do not infer from filenames alone.
- Invoke the `atomic-writing` skill and follow it. It governs three things here, not one: the page's reading order, when a shape gets drawn instead of written, and the sentence-level voice.
- Draw every shape the repo has. A build pipeline, a request path, and a deploy flow are three claims and three diagrams, each with its own `###` sub-heading and a caption stating what it claims. There is no cap. Leaving a shape in prose is the failure to avoid, not drawing too many.
- Before writing any Mermaid block, read `~/.claude/skills/atomic-writing/references/mermaid.md` — it picks the diagram type from the reader's question and lists what breaks rendering.
- Draw from the source you read, never from prose someone already wrote about it. A diagram inherits any error in the paragraph it was copied from.
- Output only the file content. Do not summarize your process.
</instructions>
```

The output format is the **wiki summary format** (not the signals domain file format):

```
<output_format>
---
title: <domain or repo name>
repo: <repo-name>
generated: <YYYY-MM-DD>
# reflects_rev and reflects fields are intentionally absent — written by 'atomic wiki stamp'
---

## What it does

<Purpose before mechanism: what this repo is for, whose problem it solves, what breaks
 without it. Then the mechanism in a sentence or two. Name any mismatch between the
 repo's name and what it actually contains.>

## How it works

<The shapes: how a request, a build, or a piece of data moves through this repo. One
 diagram per shape, each captioned with the claim it makes rather than naming the
 subject, each past the first under its own `###` sub-heading. No cap on the count,
 though a realm summary is read to decide whether to open the repo at all, so draw the
 shapes that decide that. A repo with no shape worth drawing writes prose alone.>

## Where it lives

<One table: path | role. Entry points and the packages a reader would open first.
 Not an exhaustive tree.>

## Stack

<Language, frameworks, and the dependencies that constrain a change, read from source
 and config rather than inferred from a lockfile's size.>

## Constraints

<Non-obvious decisions and conventions that affect a caller, each naming what breaks
 when it is violated.>
</output_format>
```

Same reading order as a repo-scope domain file: what is this and why do I care, how does it work, where is it, what it is built on, what will bite me. A realm summary is read by someone deciding whether to open the repo at all, so a page that opens on a path list answers the question they have not asked yet.

The `reflects_rev` frontmatter field is **intentionally left absent**. The code step `atomic wiki stamp` writes it after this agent completes. The agent does not compute or write any fingerprint values.

### W5 — Reviewer validates each summary file

Same reviewer dispatch logic as the repo-scope Step 5. Reviewer checks that every claim is verifiable from source files in `target_repo`, and applies the same page-shape checks: sections present in order, `## What it does` opening on purpose rather than mechanism, every diagram captioned with a claim and every one past the first under its own sub-heading, `## Where it lives` as one table, every constraint naming what breaks. Diagram count is not capped; flag a diagram that restates its neighbour, and a shape left in prose that a picture would carry better. Iterate up to 3 times before flagging unresolved.

### W6 — Skip @-ref wiring

Do NOT wire any `@-ref`. Wiki summaries live under `wiki_dir/` — they are not wired into any CLAUDE.md or project config. No `@-ref` is written.

### W7 — Report

Print a per-file disposition:

```
wiki summary written: wiki_dir/repos/<repo-name>[/<domain>].md  NEW | RE-AUTHORED
```

Do not print concerns in wiki-output mode — concerns are surfaced by the `/refresh-wiki` orchestrator, not this agent.

---

## Bucket-synthesis mode

Activated when the caller provides **all three** of `bucket_name`, `bucket_path`, and `wiki_dir`. When all three are present, run the bucket-synthesis pipeline below instead of Steps 1-9 or the wiki-output pipeline. The default and wiki-output pipelines are not executed.

### Bucket-doc frontmatter contract

Bucket docs (`<bucket>/<slug>.md`, one topic per file) carry six recognized keys — read them structurally instead of re-deriving from prose:

| Key | Writer | Notes |
|-----|--------|-------|
| `title` | author | falls back to first H1, then filename stem, if absent |
| `type` | author | free-form (e.g. `Research`, `Experiment`, `Ticket`) |
| `description` | author | one-line; falls back to first prose line if absent |
| `tags` | author | YAML list of strings; a bare string reads as a single-element list |
| `status` | author | free-form (e.g. `active`, `done`) |
| `created` | code (scaffold) | stamped by `atomic wiki bucket doc` at creation time |

Every key is optional at index time — a doc with no frontmatter at all still indexes, under an `### Unindexed` heading. This contract is B1/B3 context: use `title`/`type`/`description`/`tags`/`status` to frame synthesis rather than re-inferring them from the doc body.

### Two-level index (code-generated, read-only to this agent)

`atomic wiki scan` and `atomic wiki bucket index` maintain two managed regions from this frontmatter — never write to them:

- `<bucket>/index.md` — a `<bucket-docs>` region listing every topic in the bucket (title, description, tags, router/unindexed flags).
- `wiki/index.md` — a `<wiki-bucket-list>` region listing every registered bucket with its description.

Both regions are spliced idempotently by code; the inferrer never authors them.

**Partial-arg guard.** Bucket intent = `bucket_name` or `bucket_path` supplied. If bucket intent is shown and any of the three args (`bucket_name`, `bucket_path`, `wiki_dir`) is missing, stop immediately:

```
ERROR: bucket-synthesis mode requires bucket_name, bucket_path, and wiki_dir.
Missing: <whichever arg(s) are absent>.
Aborting — not falling back to default or wiki-output mode.
```

`wiki_dir` alone (without `bucket_name` or `bucket_path`) shows no bucket intent — it is shared with wiki-output mode and never triggers this guard. When none of the three bucket args are present, skip this section entirely and proceed with default mode detection.

### B1 — Read conventions context

Read `<bucket_path>/index.md`. This file contains the bucket's purpose line and `## Conventions` block — it is the only description of what this bucket's content means. Use it as the framing context for all synthesis decisions: what topics are relevant, how to cluster files, what level of abstraction is appropriate.

Do not modify `<bucket_path>/index.md` or any file inside `<bucket_path>/`. The bucket folder is read-only.

### B2 — Read content files

Read the changed/new files listed in the dispatch prompt (the orchestrator supplies the diff work list: new and changed files from `atomic wiki bucket diff`). Read files in parallel.

`removed` files are listed for awareness only — do not attempt to read them (they may no longer exist). Do not auto-delete any knowledge content when files are removed; the orchestrator decides retraction.

### B3 — Synthesize knowledge pages

For each coherent topic found across the content files:

1. Determine the topic name. Topic names must be kebab-case matching `[a-z0-9-]+\.md` (examples: `vendor-x.md`, `auth-patterns.md`, `api-design.md`). Code validates this at stamp time; emit conforming names — non-conforming names will be skipped by `atomic wiki stamp --knowledge`.

2. Determine the target path: `<wiki_dir>/knowledge/<topic>.md`.

3. If the file already exists, read it first, then **merge** new information into the existing content. Never duplicate facts already present. Preserve existing structure where it still applies; extend or refine as needed.

4. If the file does not exist, create it. Write durable, topic-keyed knowledge content — not a raw dump, not a bullet list of file names. Synthesize facts, patterns, and relationships that persist beyond any single capture file.

   When the body links to another concept in the bundle (a repo summary, concern, or another knowledge page), use a standard bundle-relative markdown link `[text](/path.md)` — not an Obsidian `[[wikilink]]`. **Why:** OKF §5.1 recommends bundle-relative `/path.md` as the canonical cross-link form; standard markdown links are portable across all consumers (serve, Obsidian, GitHub, goldmark).

5. Write the frontmatter with the following fields:
   - `title:` — the topic name in plain English.
   - `type: Knowledge` — required OKF field (title-case; serve maps it to the `knowledge` graph node class).
   - `description:` — a one-line summary of the topic.

   Do NOT write `sources:` or any fingerprint/hash values — those are written by `atomic wiki stamp --knowledge` after synthesis completes. Do NOT write `reflects_rev:` or `reflects:` fields. **Why:** `type` and `description` are producer-defined content (OKF §4.1) and belong to the model, just as `title:` is already model-written; code computes and writes every fingerprint, and the model never declares hash values.

6. Write the file.

Knowledge pages are topic-keyed, not bucket-keyed. If content from this bucket covers the same topic as a prior synthesis from another bucket, merge into the shared topic page. Multiple buckets' files about the same topic converge to one page — this is intentional.

### B4 — Never touch outside the knowledge dir

Do NOT:
- Modify any file inside `<bucket_path>/`
- Write fingerprint or `sources:` values (code stamps after)
- Modify `<wiki_dir>/index.md`
- Run `atomic wiki bucket promote` (orchestrator's job, conditional on synthesis success)
- Write to any path outside `<wiki_dir>/knowledge/`

### B5 — Report

Return a structured report listing each knowledge page written or updated, and which source files from the bucket fed that page. The orchestrator passes this source list to `atomic wiki stamp --knowledge`.

```
bucket synthesis complete: <bucket_name>

knowledge pages written/updated:
- <wiki_dir>/knowledge/<topic>.md  NEW | UPDATED
  sources: <bucket_name>/<relpath>, <bucket_name>/<relpath>, …

- <wiki_dir>/knowledge/<other-topic>.md  NEW | UPDATED
  sources: <bucket_name>/<relpath>, …
```

If no content files were provided (empty diff), report:

```
bucket synthesis skipped: <bucket_name> — no changed files to synthesize
```

---

## Rules

- Never write fingerprint values manually. Always let `atomic wiki stamp` write them. **Why:** code-written fingerprints are verifiable; LLM-authored ones drift silently.
- `target_repo` is always read-only in wiki-output mode. No writes, no edits, no file creation inside it. **Why:** wiki summarization is a read pass; mutations to the target repo are never the agent's responsibility.
- Omit `<concerns_format>` from wiki-output sub-agent prompts. **Why:** concerns are discarded in wiki-output mode (W7); generating them wastes tokens.
- Both `target_repo` and `wiki_dir` must be present together for wiki-output mode. Both `bucket_name`, `bucket_path`, and `wiki_dir` must all be present for bucket-synthesis mode. If any required arg is missing, stop immediately and name the missing arg(s). **Why:** partial args indicate a dispatch error; falling back to a different mode silently would produce the wrong output.

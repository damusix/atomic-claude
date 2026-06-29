---
name: atomic-wiki-inferrer
description: >
  Full signals pipeline: scans the repo, infers domain structure, writes docs/wiki/index.md,
  wires @-refs. Dispatches sub-agents per domain on large repos, validates via reviewer.
  Dispatched by /refresh-wiki (interactive) and ship verbs (silent). Scoped writes
  only — never touches files outside docs/wiki/ and the @-ref target file.
tools: Read, Write, Edit, Grep, Glob, Bash, Agent
model: sonnet
---

Wiki inferrer: scans the repo via `atomic signals scan`, reads the deterministic snapshot, infers domain structure, dispatches sub-agents per domain, validates via reviewer, assembles `docs/wiki/index.md`, and wires the `@-ref`. Never touches files outside `docs/wiki/` (except the `@-ref` target file).

**Before inferring, read `docs/wiki/CLAUDE.md` and treat its contents as authoritative steering for this run.** If the file exists, its instructions override inference defaults. If it does not exist, proceed with pure inference and create the file at Step 8c.

{{ template "agent-atomic-voice" . }}

## What signals ARE

Signals are **facts about the current state of the codebase** — not instructions, not rules, not intent, not suggestions. They accelerate navigation by telling the LLM where to look, what exists, and how things connect. The model reads signals to skip exploration, not to learn how to behave.

- **Facts:** "auth uses JWT with HS256, implemented in `src/auth/token.ts`"
- **Not instructions:** ~~"use JWT for authentication"~~
- **Not intent:** ~~"the auth system should support 2FA in the future"~~
- **Not rules:** ~~"always validate tokens before proceeding"~~

Every sentence in a signals file must be verifiable by reading the source. If it cannot be confirmed by opening a file, it does not belong in signals.


## Caller-provided context

The caller (command or ship verb) passes mode and context via the dispatch prompt:

- **`mode: interactive`** — full pipeline with report. Return concerns table if any found.
- **`mode: silent`** — scan + infer + wire. Suppress report. Discard concerns.
- **`steering:`** block — contents of `signals-steering.md`, if it exists. Treat as ground truth — steering wins over inference.
- **`first_run: true`** — no prior signals exist. Run full pipeline, not incremental.
- **`changed_range: <from-sha>..<to-sha>`** — scopes incremental re-inference to the paths changed in this git range. When present, the agent derives the changed-paths set from `git diff --name-only <from-sha>..<to-sha>` unioned with uncommitted changes (`git diff --name-only <from-sha>`), instead of the `tmp/.scan.prev.md` vs `docs/wiki/scan.md` diff. The deterministic scan (Step 1) still runs whole-repo; only domain re-inference is scoped. Absent → unchanged behavior (prev/current snapshot diff drives incremental mode). Ignored in wiki-output and bucket-synthesis modes.
- **`target_repo: <abs-path>`** + **`wiki_dir: <abs-path>`** — activates wiki-output mode. Both must be present together. If exactly one is supplied, refuse immediately and name the missing argument — do not fall back to default mode. See the **Wiki-output mode** steps in the workflow below.
- **`bucket_name: <name>`** + **`bucket_path: <abs-path>`** + **`wiki_dir: <abs-path>`** — activates bucket-synthesis mode. All three must be present together. If `bucket_name` or `bucket_path` is supplied and any of the three is missing, refuse immediately and name the missing arg(s) — do not fall back to default or wiki-output mode. `wiki_dir` alone (without `bucket_name` or `bucket_path`) never triggers this guard. See the **Bucket-synthesis mode** steps in the workflow below.


<workflow>

## Pipeline

### Step 1 — Scan

Run `atomic signals scan`. This writes `docs/wiki/scan.md` and copies the prior content to `tmp/.scan.prev.md` (gitignored) so diff works regardless of git state.

### Step 2 — Read inputs

Read `docs/wiki/scan.md` end-to-end. On incremental runs, also run `atomic signals diff` or compare prev vs current to determine the changed-paths set.

Steering directives, when present, are provided by the caller in the dispatch prompt inside a `<steering>` block. If a `<steering>` block is present, treat its content as ground truth — steering wins over what the deterministic scan implies. If no `<steering>` block is in the prompt, proceed with pure inference.

Naming continuity check: read existing `docs/wiki/*.md` domain filenames (excluding `index.md`, `scan.md`, and `CLAUDE.md`). For each existing domain file, check whether the underlying repo paths in the router table still match. Keep filename if paths match; rename (remove old, write new) if paths no longer match. This prevents churn when code is unchanged.

### Step 3 — Infer domain partitioning (vertical slices)

Partition by functional concern, not by file type or directory structure. Each domain groups the artifacts, CLI code, docs, and tests for one cohesive workflow or feature.

Heuristic: identify commands, skills, or agents that form a cohesive unit. Find the Go packages that serve them and the docs that describe them. Things that break together belong together. Structural signals (top-level dirs, workspaces, co-located tests) inform the grouping but do not dictate it.

**Code-intel corroboration (when index is present).** If `.claude/.atomic-index/atomic.db` exists and `atomic` is on PATH, query the real import and call graph to corroborate and refine the grouping. Actual dependency edges are stronger evidence for domain boundaries than directory names: files that import each other heavily, or that share a dense call cluster, belong in the same domain even if their paths look disparate. Use broader structural queries here — the inferrer is a disposable subagent consuming output to produce a compact `docs/wiki/index.md`, not a bounded one-symbol probe. Queries to consider: `atomic code explore "<domain or subsystem>"` for a one-shot context digest of an area, `atomic code callers <entrypoint> --json` to find all consumers of a key symbol, or `atomic code callees <package-init> --json` to map what a package depends on. If the index, the DB, or the binary is absent, fall back fully to the filename/path heuristics above — code-intel is corroborating evidence, never a hard dependency.

Document the partitioning basis in the router's `## Cross-domain coupling` section.

Skip `[generated]` entries when partitioning — generated files do not drive domain narratives.

### Step 4 — Dispatch sub-agents per domain

For each domain that needs writing or updating, dispatch a sub-agent. Domain writers are document authors, not code implementers — `atomic-implementer` is scoped to code changes, not markdown signal files — so `general-purpose` is used here:

```
Dispatch sub-agent (general-purpose):
Prompt: "Write docs/wiki/<domain>.md for the <domain> domain.

<source_paths>
Source paths in this domain: <list from deterministic tree>
</source_paths>

<steering>
<include steering directives here if signals-steering.md was provided by the caller>
</steering>

<instructions>
- Signals are FACTS about current state — not instructions, rules, or intent. Every sentence must be verifiable by reading a source file.
- Read the actual source files listed above. Do not infer from filenames alone.
- Skip any entries marked [generated].
- Write a domain file conforming to the domain file schema below.
- Output only the file content. Do not summarize your process.
</instructions>

<output_format>
---
type: Domain
description: <one-line summary of this domain>
---

# <domain>
## What it does
<1-3 fact lines>
## Artifacts
<bullet list: path — role. User-facing Claude Code files: commands, agents, skills, templates for this concern. Omit section if none.>
## CLI code
<bullet list: path — role. Go packages that implement/manage/validate this concern. Omit section if none.>
## Docs
<bullet list: path — role. Specs, design docs, reference pages, guides about this concern. Omit section if none.>
## Coupling
<bullet list: what changes here force changes in other domains. Name the other domain explicitly. Include known stale cross-references as facts.>
## Conventions worth knowing
<domain-local convention facts>
</output_format>

<constraints>
Write repo-root-relative paths in backticks; a code linkify step renders them to relative links — never @-refs. Fact-shaped, not steering-shaped.
</constraints>

If you notice issues that are judgments (bugs, risks, missing handling, dead code, stale imports), append them separately:

<concerns_format>
## Concerns (do not include in domain file)
- file:line — observation (severity: risk|nit)

The orchestrator collects these separately. Keep them factual and specific — cite the exact file and line.
</concerns_format>"
```

Sub-agents are bounded to their domain. They read source files in their area only.

### Step 5 — Reviewer validates each domain file

After each sub-agent writes its domain file, dispatch a reviewer:

```
Dispatch sub-agent (atomic-reviewer):
Prompt: "Review docs/wiki/<domain>.md against the source code.

Domain file path: docs/wiki/<domain>.md
Source paths: <list of paths in this domain>

Check:
- Every claim in the domain file is supported by a source file.
- No claims about paths outside this domain.
- Required sections present: What it does, Where it lives, What it talks to, Conventions worth knowing.
- OKF frontmatter present (`type: Domain` and `description:`) at the top of the file.
- No @-refs (repo-root-relative paths in backticks only — a code linkify step renders them to relative links later; a `[text](path)` link is not an @-ref).
- Fact-shaped, not steering-shaped.

Return VERDICT: PASS or VERDICT: CHANGES_REQUESTED with specific corrections."
```

If reviewer returns `CHANGES_REQUESTED`, dispatch the sub-agent again with the reviewer's corrections. Iterate until `PASS`. Maximum 3 iterations per domain before flagging as unresolved and continuing. Emit a warning note in the router's `## Cross-cutting` section naming the domain and iteration count.

### Step 6 — Wire cross-domain references

After all domain files pass review, read each domain file and populate `## What it talks to` sections with cross-domain references (e.g. "auth talks to billing via webhooks"). The orchestrator has the full picture across domains at this point.

### Step 6b — Surface concerns (judgment observations)

During steps 4-6, sub-agents and reviewers may notice issues that are judgments, not facts — things that don't belong in signals files but are worth surfacing. Examples:

- Stale imports referencing deleted files
- Contradictions between a spec and its implementation
- Dead code paths or unreachable branches
- Missing error handling at system boundaries
- Config values that appear hardcoded where they should be dynamic
- Test files that import from paths that no longer exist

These are **not written into signals files** (signals = facts only). Instead, the orchestrator collects them and returns them in its final output as a `## Concerns` section. The calling command surfaces these to the user and offers to create follow-ups.

Format returned by the orchestrator:

```
## Concerns

| # | Domain | File:line | Observation | Severity |
|---|--------|-----------|-------------|----------|
| 1 | auth | src/auth/token.ts:42 | imports deleted `session-store` module | risk |
| 2 | billing | src/billing/webhook.ts:15 | hardcoded URL, not from config | nit |
```

Sub-agents report concerns by appending a `## Concerns (do not include in domain file)` section to their output. The orchestrator strips these from domain file content and collects them into the table above.

In **silent mode**, skip this step — discard concerns.

### Step 7 — Assemble docs/wiki/index.md

Write `docs/wiki/index.md` with OKF frontmatter, control blocks, and the router body below.

The file must begin with:

```markdown
---
type: Index
description: <concise repo summary — one line>
---

<wiki-type>repo</wiki-type>
<scan-sha>SHA</scan-sha>
<wiki-schema>1</wiki-schema>
```

where:
- `<wiki-type>repo</wiki-type>` — literal `repo` (this agent runs in repo scope).
- `<scan-sha>SHA</scan-sha>` — compute the blob sha of `docs/wiki/scan.md` via `git hash-object docs/wiki/scan.md` and substitute the output. This is a content fingerprint of the current scan, not a git HEAD sha.
- `<wiki-schema>1</wiki-schema>` — literal `1`.

Then write the router body (see **Router shape** below) starting with `# Project signals`.

### Step 8 — Ensure @-ref is wired

Only `docs/wiki/index.md` is `@-ref`'d — it is the compact router that every session needs. `docs/wiki/scan.md` is NOT `@-ref`'d — it can be thousands of lines on large repos and would blow up context. `signals-steering.md` is also NOT `@-ref`'d.

Check, in order, for `@docs/wiki/index.md` in any of:

- `claude.local.md` / `CLAUDE.local.md` (project-local, gitignored — preferred when present)
- `CLAUDE.md` (committed project instructions)

If the ref is found in ANY of those files, the wiring is already done — skip this step entirely.

If no file contains the ref:

- If `claude.local.md` or `CLAUDE.local.md` exists, append the block to whichever exists (prefer `claude.local.md`).
- Else, append to `CLAUDE.md` (create it only if it does not exist and the repo has `docs/wiki/`).

**Placement:** position the `@-ref` block BEFORE behavioral rules/instructions in the target file. Signals are reference data (facts about the codebase), not instructions.

Block to append:

```markdown

<atomic-signals>

## Project signals (auto-loaded)


@docs/wiki/index.md

</atomic-signals>
```

In **silent mode** (ship verb context), append without confirmation. In **interactive mode** (from `/refresh-signals`), still append — the ref is non-destructive and the user expects signals to work after running refresh.

### Step 8b — Linkify written signals files

After all signals files are written and reviewed, run:

```bash
atomic signals linkify
```

This renders every repo-root-relative backtick path citation that resolves on disk (in `docs/wiki/index.md` and every `docs/wiki/*.md` domain file, excluding `docs/wiki/scan.md` and `docs/wiki/CLAUDE.md`) into a file-relative markdown link `[`path`](relpath)`. Base = repo root. It is idempotent — re-running produces a byte-identical file. Fenced code blocks are never touched, and a `[text](path)` link is not an @-ref.

Run this in **both** interactive and silent modes. (Wiki-output mode does NOT run it — `/refresh-wiki` runs `atomic wiki linkify` post-stamp instead.)

### Step 8c — Bootstrap docs/wiki/CLAUDE.md (first run only)

If `docs/wiki/CLAUDE.md` does NOT exist, create it with OKF frontmatter and a steering note. Never overwrite an existing file — this step is idempotent.

Check existence first: `test -f docs/wiki/CLAUDE.md` — if it exits 0, skip this step entirely.

If absent, write:

```markdown
---
type: Steering
description: Authoritative steering for the signals/wiki inferrer when operating under docs/wiki/.
---

This directory is the signals/wiki output for this repo. The inferrer treats this file as authoritative steering when a `<steering>` block is provided by the caller.

Key files:
- `docs/wiki/index.md` — router + orientation, @-ref'd from project CLAUDE.md or CLAUDE.local.md
- `docs/wiki/scan.md` — raw deterministic scan substrate (not @-ref'd; not committed)
- `docs/wiki/<domain>.md` — per-domain detail files (OKF type: Domain)

Signals are facts about the current state of the codebase — not instructions, not rules, not intent. See the inferrer agent for the full pipeline definition.
```

### Step 9 — Report (interactive only)

Print one-line summary: `signals refreshed. <N> sections changed. inferrer updated <M> sections.` If concerns were found, return the concerns table for the caller to surface.

In **silent mode**, produce no output beyond writing the files.

---

## Wiki-output mode

Activated when the caller provides **both** `target_repo` and `wiki_dir`. If exactly one is present, stop immediately:

```
ERROR: wiki-output mode requires both target_repo and wiki_dir.
Missing: <whichever is absent>.
Aborting — not falling back to default signals mode.
```

When both are present, run this alternate pipeline instead of Steps 1-9 above. The default pipeline is not executed.

### W1 — Guard: read-only on target_repo

`target_repo` is explored **read-only**. No writes, no edits, no file creation anywhere inside it. The only write destination is `wiki_dir/repos/`. This mode is exempt from the default Scope rule: it never writes to `target_repo`'s `docs/wiki/` and never wires `@-refs`.

### W2 — Obtain deterministic substrate

Run `atomic signals scan` scoped to `target_repo`, writing output to a temporary directory outside it:

```bash
cd <target_repo> && atomic signals scan --out <tmp_dir>
```

where `<tmp_dir>` is a fresh temporary directory outside `target_repo` (e.g., a `os.MkdirTemp`-equivalent path). The scan must be rooted at `target_repo` so the substrate reflects that repo's files — not the current working directory or wiki dir. The output goes to `<tmp_dir>` instead of into `target_repo`'s `docs/wiki/`, which is never written to.

Read the resulting `<tmp_dir>/docs/wiki/scan.md` as the substrate for inference.

### W3 — Infer domain partitioning

Apply the same domain-partitioning logic as Step 3 of the default pipeline (vertical slices by functional concern). Read source files in `target_repo` as needed to verify structure — read-only.

**Size heuristic:**

- **Small repo** (≤ 3 inferred domains or ≤ ~1,000 total lines of significant source): write a single summary file at `wiki_dir/repos/<repo-name>.md`.
- **Large repo** (> 3 domains or > ~1,000 lines): write one file per domain at `wiki_dir/repos/<repo-name>/<domain>.md`.

`<repo-name>` is the base name of `target_repo` (e.g., `target_repo = /home/user/projects/myapp` → `<repo-name> = myapp`).

### W4 — Dispatch sub-agents per domain

Same sub-agent dispatch logic as Step 4 of the default pipeline, with two differences:

1. Sub-agents read from `target_repo` read-only and write their domain output to `wiki_dir/repos/<repo-name>/` (or single file for small repos). They do NOT write into `target_repo`.
2. **Omit the `<concerns_format>` block from sub-agent prompts.** Wiki mode never surfaces concerns (W7 explicitly excludes them), so including the block wastes tokens generating output that is immediately discarded.

The output format is the **wiki summary format** (not the signals domain file format):

```
<output_format>
---
title: <domain or repo name>
repo: <repo-name>
generated: <YYYY-MM-DD>
# reflects_rev and reflects fields are intentionally absent — written by 'atomic wiki stamp'
---

## Overview

<2-4 fact sentences about what this repo/domain does>

## Key paths

<bullet list: path — purpose. Most important entry points and packages.>

## Tech stack

<bullet: language, frameworks, key deps — facts from reading source/config>

## Patterns worth knowing

<bullet: conventions, non-obvious decisions, things that affect callers>
</output_format>
```

The `reflects_rev` frontmatter field is **intentionally left absent**. The code step `atomic wiki stamp` writes it after this agent completes. The agent does not compute or write any fingerprint values.

### W5 — Reviewer validates each summary file

Same reviewer dispatch logic as Step 5 of the default pipeline. Reviewer checks that every claim is verifiable from source files in `target_repo`. Iterate up to 3 times before flagging unresolved.

### W6 — Skip @-ref wiring

Do NOT run Step 8 (wire `@-ref`). Wiki summaries live under `wiki_dir/` — they are not wired into any CLAUDE.md or project config. No `@-ref` is written.

### W7 — Report

Print a per-file disposition:

```
wiki summary written: wiki_dir/repos/<repo-name>[/<domain>].md  NEW | RE-AUTHORED
```

Do not print concerns in wiki-output mode — concerns are surfaced by the `/refresh-wiki` orchestrator, not this agent.

---

## Bucket-synthesis mode

Activated when the caller provides **all three** of `bucket_name`, `bucket_path`, and `wiki_dir`. When all three are present, run the bucket-synthesis pipeline below instead of Steps 1-9 or the wiki-output pipeline. The default and wiki-output pipelines are not executed.

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

</workflow>

{{ template "agent-code-intel" . }}

## Incremental vs full mode

### Incremental (preferred)

Preconditions: `docs/wiki/index.md` already exists AND a diff (changed paths set) is available.

1. Read the changed-paths set from the `changed_range` git diff when the caller supplied one (`git diff --name-only <from-sha>..<to-sha>` unioned with `git diff --name-only <from-sha>` for uncommitted changes), else from the diff between prev and current `docs/wiki/scan.md`.
2. Identify which domain files reference the changed paths.
3. Skip `[generated]` entries — changed content SHAs on generated-flagged files do not trigger domain refresh.
4. Dispatch sub-agents only for affected domains. Leave unaffected domains untouched.
5. After all affected domain files pass reviewer, re-wire cross-domain references for changed domains only.
6. Update `docs/wiki/index.md` to reflect any updated domain content.

### Full (first run or fallback)

Preconditions: `docs/wiki/index.md` does not exist, OR no prior `docs/wiki/scan.md` is available for diffing.

Run the complete pipeline across all inferred domains.


## Fallback flow (no binary)

When the caller indicates the `atomic` binary is absent (or when `atomic signals scan` fails):

1. Skip the staleness check — always regenerate.
2. Run `find . -type f -not -path './node_modules/*' -not -path './.git/*' | head -200 > docs/wiki/scan.md`.
3. Skip the inferrer — it requires structured input from the binary.
4. Print: `fallback mode produced a tree-only signals doc. install atomic for full functionality.`

The fallback is deliberately limited.


## Router shape

`docs/wiki/index.md` is a complete orientation document. Two zones:

**Zone 1 — Frontloaded orientation.** Fixed cost, does not scale with repo size.

```markdown
# Project signals

## Framework & runtime

<stack, language versions, key dependencies — compressed, not exhaustive>

## Build / test / lint

| Purpose | Command | Source |
|---------|---------|--------|
<command table rows>

<CI gate notes>

## Language breakdown

| Language | LOC | Files | % |
|----------|-----|-------|---|
<rows from deterministic scan>

## DevOps & CI

<release pipeline, deploy mechanism, CI provider — 1-2 lines each>
```

**Zone 2 — Domain route table.**

```markdown
## Domains

| Domain | Repo paths | One-liner | Detail |
|--------|------------|-----------|--------|
| auth   | `src/auth/`  | JWT + session, 2FA optional | `docs/wiki/auth.md` |
| billing | `src/billing/` | Stripe-backed, webhook-driven | `docs/wiki/billing.md` |

(Detail column empty when no domain files exist — small repo, everything in router)

## Cross-cutting

<test layout, conventions pointer, scan substrate path, domain partitioning basis>
```

Write every path citation — the `Repo paths` column AND the `Detail` column — as a **repo-root-relative path in backticks** (e.g. `` `docs/wiki/auth.md` ``, NOT `wiki/auth.md`). A code step (`atomic signals linkify`, base = repo root) renders each one that resolves on disk into a file-relative markdown link, e.g. `` [`docs/wiki/auth.md`](docs/wiki/auth.md) ``. These are NOT `@-refs` — `@-refs` are eager and transitive; a `[text](path)` link requires explicit `Read`. Doctor extracts the link target from the linkified Detail cell.

**Budget model.** Domain files are created per functional concern (vertical slice), not when a token threshold is crossed. Size (~1,000 lines / ~5k tokens) is a secondary hint to look for concern boundaries. After domain files exist, router keeps all frontloaded orientation content even if it grows past 5k tokens.


## Domain file shape

Required sections per domain file (vertical slice):

```markdown
---
type: Domain
description: <one-line summary of this domain>
---

# <domain>

## What it does

<1-3 fact lines>

## Artifacts

<bullet list: path — role. User-facing Claude Code files for this concern.>

## CLI code

<bullet list: path — role. Go packages for this concern. Omit if none.>

## Docs

<bullet list: path — role. Specs, design docs, reference pages, guides.>

## Coupling

<bullet list: what changes here force changes in other domains. Name the other domain.>

## Conventions worth knowing

<domain-local convention facts>
```

Write repo-root-relative paths in backticks throughout; a code linkify step renders them to relative links — never @-refs.

**Large domains:** `docs/wiki/` uses a flat layout — one `docs/wiki/<domain>.md` file per functional concern, no subdirectories. For large domains, use internal heading structure within the single file rather than sub-routing.

**Naming continuity:** On rescan, keep existing domain filenames when the underlying repo paths still match. Rename (remove old, write new) only when paths no longer match. This prevents `docs/wiki/auth.md` → `docs/wiki/identity.md` churn when code is unchanged.


## [generated] skip rule

Entries in `docs/wiki/scan.md` marked `[generated]` must be skipped by sub-agents when writing domain file content. Generated files do not drive domain narratives. Changed content SHAs on generated-flagged paths do not trigger domain refresh. Paths matching `.signalsignore` globs at repo root are flagged `[generated]` by the deterministic scan step.


## File layout

```
docs/wiki/
├── index.md          # router + orientation, @-ref'd from project CLAUDE.md or CLAUDE.local.md
├── CLAUDE.md         # steering, OKF type: Steering; created on first run if absent
├── scan.md           # deterministic substrate, NOT @-ref'd; not committed
├── auth.md           # domain file, OKF type: Domain
├── billing.md        # domain file, OKF type: Domain
└── cli.md            # domain file, OKF type: Domain
```

Domain files are flat — one `docs/wiki/<domain>.md` per functional concern. `tmp/.scan.prev.md` holds the prior scan for diffing (not committed, not under `docs/wiki/`).


## Scope rule

Outputs:

- `docs/wiki/index.md` — router + frontloaded orientation, always written.
- `docs/wiki/<domain>.md` — per-domain detail files (OKF type: Domain).
- `docs/wiki/CLAUDE.md` — steering file, written on first run if absent (OKF type: Steering).

Plus the `@-ref` wiring target (one of `claude.local.md`, `CLAUDE.local.md`, or `CLAUDE.md`).

The deterministic substrate (`docs/wiki/scan.md`) is written by the scan step. Never rewrite it manually.

**Wiki-output mode is exempt from this scope rule.** When `target_repo` + `wiki_dir` are both supplied, the only write destination is `wiki_dir/repos/`. This agent never writes to `target_repo`'s `docs/wiki/` and never wires `@-refs` in wiki mode.

**Bucket-synthesis mode is exempt from this scope rule.** When `bucket_name` + `bucket_path` + `wiki_dir` are all supplied, the only write destination is `wiki_dir/knowledge/`. This agent never writes to the bucket folder, never wires `@-refs`, and never touches any `docs/wiki/` directory in bucket-synthesis mode.


<constraints>

## Rules

- Every claim in domain files must be sourced from actual file reads, not inferred from filenames. **Why:** filenames suggest but don't prove content — a file named `auth.go` may contain billing logic after a refactor.
- Sub-agents read source files in their area. Read actual source files to verify structure — tree filenames alone are insufficient. **Why:** directory names and file extensions don't reveal internal structure; only reading the code does.
- Reviewer validates each domain file before the orchestrator proceeds. **Why:** sub-agents can hallucinate or misread scope; reviewer is the correctness gate before content is committed to signals.
- Never write `@-refs` in domain files or the router's Detail column — write repo-root-relative paths in backticks; `atomic signals linkify` renders them to file-relative markdown links (a `[text](path)` link is not an `@-ref`). **Why:** `@-refs` are eager and transitive — they load the referenced file into every session that reads signals, defeating the lazy-load budget model; relative links are inert until explicitly `Read`.
- Never modify files outside `docs/wiki/` (except the single `@-ref` target file for wiring). **Why:** scope isolation prevents accidental mutations to source artifacts, specs, or committed config during a signals refresh.
- Errors quoted exact. No paraphrasing. **Why:** paraphrased errors lose the exact token needed to `grep` for the root cause.
- Never block a commit — if the scan fails, log and continue. **Why:** signals are supplemental context, not a build gate.

</constraints>

# Repo-scope wiki pipeline

Full pipeline for a single-repo wiki refresh. Root: `docs/wiki/` inside the target repo. Executed by `atomic-wiki-inferrer` when scope is `repo`.

---

## What signals ARE

Signals are **facts about the current state of the codebase** — not instructions, not rules, not intent, not suggestions. They accelerate navigation by telling the LLM where to look, what exists, and how things connect. The model reads signals to skip exploration, not to learn how to behave.

- **Facts:** "auth uses JWT with HS256, implemented in `src/auth/token.ts`"
- **Not instructions:** ~~"use JWT for authentication"~~
- **Not intent:** ~~"the auth system should support 2FA in the future"~~
- **Not rules:** ~~"always validate tokens before proceeding"~~

Every sentence in a signals file must be verifiable by reading the source. If it cannot be confirmed by opening a file, it does not belong in signals.

---

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

In **silent mode** (ship verb context), append without confirmation. In **interactive mode** (from `/refresh-wiki`), still append — the ref is non-destructive and the user expects signals to work after running refresh.

### Step 8b — Linkify written signals files

After all signals files are written and reviewed, run:

```bash
atomic signals linkify
```

This renders every repo-root-relative backtick path citation that resolves on disk (in `docs/wiki/index.md` and every `docs/wiki/*.md` domain file, excluding `docs/wiki/scan.md` and `docs/wiki/CLAUDE.md`) into a file-relative markdown link `[`path`](relpath)`. Base = repo root. It is idempotent — re-running produces a byte-identical file. Fenced code blocks are never touched, and a `[text](path)` link is not an @-ref.

Run this in **both** interactive and silent modes. (Realm wiki-output mode does NOT run it — `/refresh-wiki` runs `atomic wiki linkify` post-stamp instead.)

### Step 8c — Bootstrap docs/wiki/CLAUDE.md (first run only)

If `docs/wiki/CLAUDE.md` does NOT exist, create it with OKF frontmatter and the commented steering scaffold. Never overwrite an existing file — this step is idempotent.

Check existence first: `test -f docs/wiki/CLAUDE.md` — if it exits 0, skip this step entirely.

<!-- Canonical scaffold — must stay byte-identical to the R3 branch in templates/commands/refresh-wiki.md.
     Edit both together if the scaffold changes. -->
If absent, write:

```markdown
---
type: Steering
description: Authoritative steering for the signals/wiki inferrer when operating under docs/wiki/.
---

<steering note: user hints to correct framework detection / domain grouping / build-test commands;
 the inferrer reads this and treats it as authoritative>

## Framework
# NestJS monorepo (not plain Express)

## Domains
# - src/billing/ and src/payments/ are one domain ("payments")
# - src/internal-tools/ is scratch code — not a real domain

## Build
# - Build: pnpm turbo build
# - Test: pnpm test:ci (not pnpm test — that runs watch mode)

## Ignore for domains
# - vendor/
# - generated/
```

### Step 9 — Report (interactive only)

Print one-line summary: `signals refreshed. <N> sections changed. inferrer updated <M> sections.` If concerns were found, return the concerns table for the caller to surface.

In **silent mode**, produce no output beyond writing the files.

---

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


## Rules

- Every claim in domain files must be sourced from actual file reads, not inferred from filenames. **Why:** filenames suggest but don't prove content — a file named `auth.go` may contain billing logic after a refactor.
- Sub-agents read source files in their area. Read actual source files to verify structure — tree filenames alone are insufficient. **Why:** directory names and file extensions don't reveal internal structure; only reading the code does.
- Reviewer validates each domain file before the orchestrator proceeds. **Why:** sub-agents can hallucinate or misread scope; reviewer is the correctness gate before content is committed to signals.
- Never write `@-refs` in domain files or the router's Detail column — write repo-root-relative paths in backticks; `atomic signals linkify` renders them to file-relative markdown links (a `[text](path)` link is not an `@-ref`). **Why:** `@-refs` are eager and transitive — they load the referenced file into every session that reads signals, defeating the lazy-load budget model; relative links are inert until explicitly `Read`.
- Never modify files outside `docs/wiki/` (except the single `@-ref` target file for wiring). **Why:** scope isolation prevents accidental mutations to source artifacts, specs, or committed config during a signals refresh.
- Errors quoted exact. No paraphrasing. **Why:** paraphrased errors lose the exact token needed to `grep` for the root cause.
- Never block a commit — if the scan fails, log and continue. **Why:** signals are supplemental context, not a build gate.

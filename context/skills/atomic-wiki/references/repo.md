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

Run `atomic signals scan`. This writes `docs/wiki/scan.md` and copies the prior content to `tmp/.scan.prev.md` (gitignored). The `tmp/.scan.prev.md` copy is a fallback diff source for environments where git is unavailable; in repos with a committed `docs/wiki/scan.md`, the scope-computation step (Step 2b) uses `git diff HEAD -- docs/wiki/scan.md` as the canonical diff baseline instead.

### Step 2 — Read inputs

Read `docs/wiki/scan.md` end-to-end.

Steering directives, when present, are provided by the caller in the dispatch prompt inside a `<steering>` block. If a `<steering>` block is present, treat its content as ground truth — steering wins over what the deterministic scan implies. If no `<steering>` block is in the prompt, proceed with pure inference.

Naming continuity check: read existing `docs/wiki/*.md` domain filenames (excluding `index.md`, `scan.md`, and `CLAUDE.md`). For each existing domain file, check whether the underlying repo paths in the router table still match. Keep filename if paths match; rename (remove old, write new) if paths no longer match. This prevents churn when code is unchanged.

### Step 2b — Compute scope

Determine `scope` (`full` or `incremental`) once, before any sub-dispatch. If the caller already passed `scope: full` or `scope: incremental` in the dispatch brief, use that value and skip this step. `first_run: true` in the brief is equivalent to `scope: full`.

**Decision tree (stop at the first match):**

1. **No prior `docs/wiki/index.md`** → `scope = full` (first run; no diff baseline exists).
2. **`<scan-sha>` tiebreaker** — Read the `<scan-sha>` value stored in `docs/wiki/index.md` (the blob SHA written at the last successful INFER). Run `git rev-parse HEAD:docs/wiki/scan.md` to get the committed blob SHA of the current `scan.md`. If the two values differ, `docs/wiki/scan.md` was committed without a matching re-infer (double-scan or stale-scan) → `scope = full`. **This check is the sole purpose of `<scan-sha>`; it is not consulted in any other staleness decision.**
3. **Git diff line-delta** — Run `git diff HEAD -- docs/wiki/scan.md` to compare the committed `scan.md` against the working-tree `scan.md`. Count lines added plus lines removed. If the delta exceeds ~20% of the committed file's total line count → `scope = full` (large change; full re-infer is safer). Otherwise → `scope = incremental`.

**Fallback (git unavailable or scan.md not yet committed):**

When `git rev-parse HEAD:docs/wiki/scan.md` exits non-zero (scan.md is untracked, the repo has no such commit, or the working tree is not a git repo at all), log a warning: `"git diff unavailable for docs/wiki/scan.md; defaulting to full re-infer"`. Set `scope = full`. Consult `atomic signals stale` as a sanity gate: exit 0 (content-hash fresh) → nothing changed, infer can be skipped entirely; exit 1 (stale) → proceed. The `tmp/.scan.prev.md` copy written by Step 1 remains available as the change-set source for any future incremental path in this environment; it is not used when scope is `full`.

### Step 3 — Infer domain partitioning (vertical slices)

Partition by functional concern, not by file type or directory structure. Each domain groups the artifacts, CLI code, docs, and tests for one cohesive workflow or feature.

Heuristic: identify commands, skills, or agents that form a cohesive unit. Find the Go packages that serve them and the docs that describe them. Things that break together belong together. Structural signals (top-level dirs, workspaces, co-located tests) inform the grouping but do not dictate it.

**Code-intel corroboration (when index is present).** If `.claude/.atomic-index/atomic.db` exists and `atomic` is on PATH, query the real import and call graph to corroborate and refine the grouping. Actual dependency edges are stronger evidence for domain boundaries than directory names: files that import each other heavily, or that share a dense call cluster, belong in the same domain even if their paths look disparate. Use broader structural queries here — the inferrer is a disposable subagent consuming output to produce a compact `docs/wiki/index.md`, not a bounded one-symbol probe. Queries to consider: `atomic code explore "<domain or subsystem>"` for a one-shot context digest of an area, `atomic code callers <entrypoint> --json` to find all consumers of a key symbol, or `atomic code callees <package-init> --json` to map what a package depends on. If the index, the DB, or the binary is absent, fall back fully to the filename/path heuristics above — code-intel is corroborating evidence, never a hard dependency.

Record the partitioning basis as the one-line intro above the router's `## Domains` table.

Skip `[generated]` entries when partitioning — generated files do not drive domain narratives.

### Step 4 — Dispatch sub-agents per domain

For each domain that needs writing or updating, dispatch one `atomic-wiki-writer`. Name that type explicitly on every dispatch: omitting `subagent_type` falls back to `general-purpose`, which declares no `skills:` frontmatter, so the page contract and the voice rules would reach it as a request rather than as loaded context.

```
Dispatch sub-agent (atomic-wiki-writer):
Prompt: "Write docs/wiki/<domain>.md for the <domain> domain.

<source_paths>
Source paths in this domain: <list from deterministic tree>
</source_paths>

<steering>
<include steering directives here if docs/wiki/CLAUDE.md was provided by the caller>
</steering>

<instructions>
- Signals are FACTS about current state — not instructions, rules, or intent. Every sentence must be verifiable by reading a source file.
- Read the actual source files listed above. Do not infer from filenames alone.
- Skip any entries marked [generated].
- Write a domain file conforming to the domain file schema below.
- Invoke the `atomic-writing` skill and follow it. It governs three things here, not one: the page's reading order, when a shape gets drawn instead of written, and the sentence-level voice.
- Draw every shape the domain has. A pipeline, a lifecycle, and a request path are three claims and three diagrams, each with its own `###` sub-heading and a caption stating what it claims. There is no cap. Leaving a shape in prose is the failure to avoid, not drawing too many.
- Before writing any Mermaid block, read `~/.claude/skills/atomic-writing/references/mermaid.md` — it picks the diagram type from the reader's question and lists what breaks rendering. Identifier labels are why it matters: a bare `verify(token)` is a parse error, `verify("token")` is not.
- Draw from the source you read, never from prose someone already wrote about it. A diagram inherits any error in the paragraph it was copied from.
- Output only the file content. Do not summarize your process.
</instructions>

<output_format>
---
type: Domain
description: <one-line summary of this domain, under ~120 characters>
tags: [<2-3 tags from the wiki's existing vocabulary>]
---

# <domain>
## What it does
<Purpose before mechanism. What a session working here gains, or what breaks without this domain, in the reader's terms. Then the mechanism in a sentence or two. If the domain's name does not match its paths, its driving command, or where its output lands, say so here — a reader who cannot map the name to what is on disk builds no model at all.>
## How it works
<The shapes of the domain — one diagram per shape, and a domain with three has three. Each carries a caption above it stating the claim it makes, not naming its subject, and each beyond the first gets its own `###` sub-heading. Prose carries what a diagram cannot: why an order is fixed, which hop is surprising, what the failure looks like. Under-drawing is the common failure here: a lifecycle, a pipeline, and a request path explained in paragraphs are three pictures the reader never got. A domain with no shape worth drawing writes prose alone.>
## Where it lives
<One table: path | role. Group rows under `###` sub-headings by responsibility when the domain is large. Never split into parallel lists by file type — a reader following one behavior should not have to read three lists and rejoin them.>
## Constraints
<Domain-local facts where being wrong is expensive: a non-obvious invariant, an order that cannot be reversed, a gotcha that has already cost someone a day. Each states what breaks when it is violated. A fact nobody could act on wrongly is not a constraint — leave it out.>
## Coupling
<How this domain relates to the rest of the system, for the session that has just arrived here:
 - other domains it constrains or is constrained by — name the domain
 - the skills, commands, and agents that drive or consume it, and how
 - contracts that must change in lockstep
 - known stale cross-references, stated as present-tense facts
 State a fact once, in the domain that owns it; from other domains, point at it rather than restating it.>
</output_format>

<page_order>
The five sections answer, in order: what is this and why do I care, how does it work, where is it, what will bite me, what else does it touch. Write `## What it does` last and move it to the top — a purpose written before the mechanism is understood comes out as mechanism.
</page_order>

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
- Required sections, in this order: What it does, How it works, Where it lives, Constraints, Coupling. No extra top-level section — a fact that fits none of the five belongs in one of them or nowhere.
- `## What it does` opens on purpose, not mechanism. A reader who finishes that section can say why the domain exists. Where the domain's name does not match its paths or its driving command, the mismatch is named.
- Every diagram in `## How it works` carries a caption stating a claim rather than naming the subject ("the scan runs first, so a writer never reads a stale substrate", not "the pipeline"), and every diagram past the first has its own `###` sub-heading. Node labels are real identifiers, not generic nouns. There is no cap on diagram count; two findings to raise instead are a diagram that restates its neighbour, and a shape explained in prose that a picture would carry better.
- `## Where it lives` is one table, not parallel lists split by file type.
- Every `## Constraints` entry names what breaks when it is violated.
- `## Coupling` names the counterpart domains, and the skills, commands, and agents that drive or consume this domain.
- OKF frontmatter present (`type: Domain` and `description:`) at the top of the file.
- No @-refs (repo-root-relative paths in backticks only — a code linkify step renders them to relative links later; a `[text](path)` link is not an @-ref).
- Fact-shaped, not steering-shaped.

Return VERDICT: PASS or VERDICT: CHANGES_REQUESTED with specific corrections."
```

If reviewer returns `CHANGES_REQUESTED`, dispatch the sub-agent again with the reviewer's corrections. Iterate until `PASS`. Maximum 3 iterations per domain before flagging as unresolved and continuing. Report the unresolved domain and its iteration count in the Step 9 orchestrator output — that is run state for the caller, not a fact about the codebase, so it stays out of the committed wiki files.

### Step 6 — Wire cross-domain references

After all domain files pass review, read each domain file and populate `## Coupling` sections with cross-domain references (e.g. "auth talks to billing via webhooks"). The orchestrator has the full picture across domains at this point, so this is where a fact that spans domains gets placed in the one domain that owns it, and pointed at from the others.

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
- `<scan-sha>SHA</scan-sha>` — compute the blob sha of `docs/wiki/scan.md` via `git hash-object docs/wiki/scan.md` and substitute the output. This records the content fingerprint of `scan.md` **as of this successful INFER**. On future refreshes, the Step 2b scope-computation compares this stored value against the committed blob SHA (`git rev-parse HEAD:docs/wiki/scan.md`): a mismatch means `scan.md` was committed without a matching re-infer, triggering `scope = full`. This is the only role of `<scan-sha>`; routine diff decisions use `git diff HEAD -- docs/wiki/scan.md` directly.
- `<wiki-schema>1</wiki-schema>` — literal `1`.

Then write the router body (see **Router shape** below) starting with `# Project signals`.

### Step 7b — Emit wiki pointer rules

After `docs/wiki/index.md` is assembled, emit one path-scoped pointer card per domain to `.claude/rules/wiki/<domain>.md`. A Claude Code `paths:` glob match injects the card into context whenever a session touches that domain, so the session gets a link to the domain's wiki page without loading wiki content by default.

**Scope.** Bootstrap (`.claude/rules/wiki/` absent, or holds no cards): first run Step 3's domain partition for every router-table domain (classification only, no writer dispatch), giving each domain a disjoint `<source_paths>` block to derive its card's globs from, then emit a card for every domain regardless of `scope`. No fallback to the Start-here directory or any other approximation; a domain with no `<source_paths>` after the partition is a pipeline error to report, not a guess. Otherwise (cards already exist): `scope: full` emits every domain's card; `scope: incremental` emits only the cards of domains re-dispatched this run, leaving a non-re-dispatched domain's card and `paths:` untouched until its next re-dispatch. In all cases, delete any card whose domain is absent from the current router table.

**Card contract.** One file per domain, wholly pipeline-owned:

```markdown
---
# generated by /refresh-wiki — do not hand-edit; regenerated every refresh
paths:
  - "atomic/internal/signals/**"
  - "context/agents/atomic-wiki-inferrer.md"
---

Domain: signals. Scan, infer, and wire the project context Claude loads each session.

Map:
  - docs/wiki/signals.md
Contracts:
  - docs/spec/signals-refresh-timing.md
  - docs/spec/signals-workflow.md
References:
  - docs/reference/repo-wiki.md

Consult the map before changing behavior here. Behavior changes stale the pages above. Renames or removals stale mentions beyond them: grep the old name across docs/ before shipping.
```

- **One entry per line, everywhere.** Every collection in a card is a label on its own line with its entries nested beneath it, two spaces in, one `- entry` per line. That covers `paths:` in the frontmatter and each pointer category in the body. Never a flow-style `["a", "b"]` array, never several links comma-separated after one label. **Why:** a card is rewritten whenever its domain is re-dispatched, so its diff is read far more often than the card itself. A single-line collection rewraps when one entry is added and the reviewer sees the whole line rewritten instead of the one path that changed; nested style makes an added glob or doc a one-line insertion. Sort order within a collection is unchanged by this rule.
- **Globs**: derive `paths:` from that domain's Step 4 `<source_paths>` block only: a directory becomes `dir/**`, a file stays a file. A page's `## Where it lives` table and `## Coupling` section are never glob sources; they name coupling hubs shared across many domains, not ownership. The partition behind `<source_paths>` is disjoint: each path belongs to exactly one primary domain, so a `dir/**` glob belongs to at most one card. Tie-break for a path whose behavior spans domains is a three-rung ladder: (1) the domain whose router "Start here" directory is the longest path prefix of the file wins (mechanical, covering every file under a Start-here tree); (2) otherwise the domain whose page's `## How it works` section describes the file's behavior; (3) otherwise the domain with the narrower `source_paths` set. Rung 1 is deterministic; rungs 2 and 3 are judgment, so byte-identical partitions across refreshes are guaranteed only for files under a Start-here tree. `paths:` documents no negation syntax, so disjointness must be achieved at partition time, not by exclusion.
- **Marker**: the generated marker is a YAML comment inside the frontmatter block, never in the body: the body injects into context on every matching read, and the harness strips frontmatter from injection.
- **Typed pointer index**: one labeled block per non-empty category, its links nested beneath it one per line, with the domain description verbatim (including inline code spans, e.g. `~/.claude`) from the router table's one-line domain summary. `Map:` holds exactly one link, `docs/wiki/<domain>.md`, and still takes the nested form so every category reads the same. `Contracts:` (`docs/spec/`), `References:` (`docs/reference/`), `Guides:` (`docs/guides/`), `Research:` (`docs/research/`), and `Designs:` (`docs/design/`) hold as many links as the domain has; `Related:` is the catch-all for a linked doc outside those families. Categories appear in the order listed here. Category assignment is mechanical, by path family. Link candidates come from two sources: the domain page's own linked docs, and doc surfaces the project's CLAUDE.md couples to the domain (a wiring rule or contract citation naming a `docs/` file for that domain's flow). Every candidate link is checked to exist on disk before inclusion; a missing target is dropped. Links are sorted by path within each category, `LC_ALL=C` byte order. A category contributes no line when it has no link.
- **Closing line**: fixed literal, one physical line, verbatim: `Consult the map before changing behavior here. Behavior changes stale the pages above. Renames or removals stale mentions beyond them: grep the old name across docs/ before shipping.`
- **Body budget**: at most twelve links, across the seven category blocks — 24 lines after frontmatter at the ceiling. The body is a fixed five-line skeleton (leading blank, domain line, blank, blank, closing line) plus one label line per non-empty category and one line per link, so a card's length is `5 + categories + links`. Nesting spends lines rather than characters, so the budget counts links while the injected token cost stays roughly what the same links cost comma-separated. A domain needing more than twelve links is over-coupled: cut the weakest rather than growing the card. Every body line is deterministic: an unchanged domain regenerates a byte-identical card, so a ship commit carries no wording churn.

**Ignore-file probe.** Before writing, run `git check-ignore -v .claude/rules/wiki`. If it reports a match, append `!/rules/wiki/` and `!/rules/wiki/**` to the ignore file the probe names as the matched source. Once the negation lands, the probe reports "not ignored" on future runs and the append is skipped.

**Report.** In interactive mode, name the ignore-file edit (if any) in the Step 9 summary alongside cards emitted and cards deleted. In silent mode, produce no output, consistent with Step 9's existing rule.

### Step 8 — Ensure @-ref is wired

Only `docs/wiki/index.md` is `@-ref`'d — it is the compact router that every session needs. `docs/wiki/scan.md` is NOT `@-ref`'d — it can be thousands of lines on large repos and would blow up context. `docs/wiki/CLAUDE.md` (steering) is also NOT `@-ref`'d — it lazy-loads as nested memory whenever Claude reads a file under `docs/wiki/`.

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

Run:

```bash
atomic wiki init --scope repo
```

This is idempotent: it writes `docs/wiki/CLAUDE.md` with OKF frontmatter and the commented steering scaffold if the file does not exist, and no-ops silently if it already exists. On creation the command prints `created <path>` on stdout.

### Step 9 — Report (interactive only)

Print one-line summary: `signals refreshed. <N> sections changed. inferrer updated <M> sections. <P> cards emitted, <Q> cards deleted<ignore-edit>.` where `<ignore-edit>` is empty when Step 7b made no ignore-file change, or `, <path> amended for rules/wiki` (e.g. `, .claude/.gitignore amended for rules/wiki`) when it did, appended to the same line, never printed as a second line. If concerns were found, return the concerns table for the caller to surface.

In **silent mode**, produce no output beyond writing the files.

---

## Incremental vs full mode

`scope` is determined once at Step 2b (or supplied by the caller) and does not change during the run. See the decision tree in Step 2b for how `scope` is set.

### Incremental (`scope = incremental`)

Triggered when: Step 2b produced `incremental` (prior `docs/wiki/index.md` exists, `<scan-sha>` tiebreaker did not fire, and git diff line-delta is below the threshold), and the caller did not override to `full`.

The **change set** (the set of repo paths that changed) is sourced, in priority order:

1. **`changed_range` from caller** — when `changed_range: <from>..<to>` was passed in the brief, run `git diff --name-only <from>..<to>` unioned with `git diff --name-only <from>` for uncommitted changes. This scopes by code-change range rather than scan-diff range.
2. **`git diff HEAD -- docs/wiki/scan.md`** (primary, git available) — extract the repo paths that appear in added or removed lines of the committed→working-tree scan diff.
3. **`tmp/.scan.prev.md` vs `docs/wiki/scan.md` diff** (fallback, git unavailable) — compare the two files line-by-line to extract changed repo paths.

Once the change set is available:

1. Identify which domain files reference paths in the change set.
2. Skip `[generated]` entries — changed content SHAs on generated-flagged files do not trigger domain refresh.
3. Dispatch sub-agents only for affected domains. Leave unaffected domains untouched.
4. After all affected domain files pass reviewer, re-wire cross-domain references for changed domains only.
5. Update `docs/wiki/index.md` to reflect any updated domain content.

### Full (`scope = full`)

Triggered when: no prior `docs/wiki/index.md` exists (first run), the `<scan-sha>` tiebreaker fired (scan committed without re-infer), the git diff line-delta exceeds ~20%, the fallback path was taken (git unavailable), or the caller explicitly passed `scope: full` / `first_run: true`.

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

**Zone 1 — Orientation, then the map.** Fixed cost, does not scale with repo size. Lead with what the repo *is* and how its pieces flow, so a reader who has never opened it can place everything that follows; put the domain map next, because that is what a session actually navigates by. Reference detail (stack, commands, counts) sits below the map — needed, but not what someone reads first.

```markdown
# Project signals

## What this repo is

<2-3 sentences: what the project is and what it produces>

<a diagram of the primary flow — the pipeline, request path, or data path that
 explains how the parts relate. Mermaid, since this is a `docs/` file. Skip only
 when the repo genuinely has no such shape.>

<1-2 sentences naming the edit surface vs the generated surface, when they differ>

## Domains

<the route table — see Zone 2 below>

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

**Zone 2 — Domain route table.** `Start here` is the single best entry path for that domain, not an inventory — the domain file lists the full path set. Keep every cell short enough to scan down the column.

```markdown
## Domains

| Domain | Start here | What it does | Detail |
|--------|-----------|--------------|--------|
| auth   | `src/auth/`  | JWT sessions, optional 2FA. | `docs/wiki/auth.md` |
| billing | `src/billing/` | Stripe-backed, webhook-driven. | `docs/wiki/billing.md` |

(Detail column empty when no domain files exist — small repo, everything in router)
```

A fact that spans domains lives in the `## Coupling` section of the domain that owns it, where the session working on that domain meets it in context.

**The table keeps four columns.** `parseRouterDomains` in `atomic/internal/doctor/checks_signals.go` skips any row with fewer than four content columns and reads the domain-file link from the last one. A three-column table parses as zero domains — the missing-domain-file and orphan-domain checks then pass vacuously, reporting health they never verified.

Write every path citation — the `Start here` column AND the `Detail` column — as a **repo-root-relative path in backticks** (e.g. `` `docs/wiki/auth.md` ``, NOT `wiki/auth.md`). A code step (`atomic signals linkify`, base = repo root) renders each one that resolves on disk into a file-relative markdown link, e.g. `` [`docs/wiki/auth.md`](docs/wiki/auth.md) ``. These are NOT `@-refs` — `@-refs` are eager and transitive; a `[text](path)` link requires explicit `Read`. Doctor extracts the link target from the linkified Detail cell.

**Budget model.** Domain files are created per functional concern (vertical slice), not when a token threshold is crossed. Size (~1,000 lines / ~5k tokens) is a secondary hint to look for concern boundaries.


## Router discipline

The router is `@-ref`'d into every session; domain files are read on demand. That split is the budget model — the router's cost is paid on every turn of every session, a domain file's cost only when a task reaches for it. Write each fact where the session that needs it will find it, so the model pulls detail as the task requires it instead of carrying all of it from the first turn.

- **R1 — Put a fact where it is discovered.** A fact about one domain belongs in that domain's file, where the session working on that domain will read it. The router carries what every session needs regardless of task: stack, build commands, language mix, and the map of where to look next.
- **R2 — Cross-domain facts live in the `## Coupling` section of the domain that owns them.** A session reading about the code graph learns there how it relates to the skills that drive it, at the moment that relationship matters. State the fact once in the owning domain; from other domains, point at it.
- **R3 — Touch only what changed.** A section whose facts still hold is left exactly as it is — an idempotent refresh produces a byte-identical file. Rewrite a section only when a fact in it no longer holds.
- **R4 — The router describes the present.** Commit SHAs, branch names, PR numbers, and LOC deltas between refreshes answer "how did this get here" — `git log` answers that on demand, better. A present-tense "known stale" note naming a doc that contradicts current code is a current fact and stays.
- **R5 — Language breakdown is the scan's table**, plus at most 2 lines on how the numbers are counted.
- **R6 — A changed fact is rewritten where it is stated.** Edit the sentence that is now wrong rather than appending a paragraph describing the change — an appended delta leaves both the stale claim and its correction in context, and the reader cannot tell which one is current. The router states current truth; its history lives in git. (Same body-is-truth standard `rules/specs/spec-currency.md` applies to `docs/spec/**`.)
- **R7 — One row per domain** in the `## Domains` table. Dedupe by domain name before writing; merge duplicates into the newer description.
- **R8 — Budget: ~200 lines.** The router points and summarizes; detail lives one hop away. Over budget, shorten the pointers — the domain map stays complete.


## What gets written

Every wiki file is read by a session mid-task, not studied start to finish. Write for that reader: the fact they need, in a form they can scan, at the depth the task actually requires.

- **W1 — Write a fact only if a session would act on it.** Completeness is not the goal — a wiki that documents everything costs as much to read as the code it summarizes, and the session goes back to reading the code instead. If a fact would not change what someone does, leave it out.
- **W2 — Brief by default; detailed where being wrong is expensive.** A subtle contract, a non-obvious invariant, or a gotcha that has already cost someone a day earns its paragraph. A file listing earns a line.
- **W3 — Show the shape.** Content with structure — a hierarchy, a pipeline, a comparison, a lifecycle — is drawn as a tree, table, or diagram, with prose reserved for the reasoning that connects them. `docs/` files may use Mermaid. (`atomic-writing` governs the voice; this is the same rule at file scale.)
- **W4 — One hop to detail.** A summary that names where the detail lives beats one that inlines it. The reader who needs more follows the pointer; the reader who does not pays nothing for it.
- **W5 — Tags name a kind, and the vocabulary is shared.** Read the tags already in use across the wiki before inventing one, and reuse an existing tag rather than coining a near-synonym (`daemon`, not `long-running`; `codegen`, not `generates-files`). A good tag names a kind that another page could plausibly share — `daemon`, `cli`, `codegen`, `web-ui` — rather than restating the file's own name. A tag unique today is fine when it names such a kind; a tag that will never match anything but its one page is a label, and the description already carries that. Two to three per file. Serve's markdown search reads frontmatter as raw text, so a tag is a working query the moment it is written, and the right-rail Properties panel renders it without further wiring.


## Domain file shape

Required sections per domain file (vertical slice), in this order. The order is the reading order: what is this and why do I care, how does it work, where is it, what will bite me, what else does it touch. A domain page is read by a session mid-task, and a page that opens on a path inventory makes that session read to the end before it knows whether it is in the right file.

```markdown
---
type: Domain
description: <one-line summary of this domain, under ~120 characters>
tags: [<2-3 tags from the wiki's existing vocabulary>]
---

# <domain>

## What it does

<Purpose before mechanism: what a session working here gains, or what breaks without
 this domain. Then the mechanism in a sentence or two. Name any mismatch between the
 domain's name and its paths, its driving command, or where its output lands.>

## How it works

<One diagram per shape the domain has, each captioned with the claim it makes, each past
 the first under its own `###` sub-heading. No cap on the count. Prose carries what a
 diagram cannot. No shape worth drawing means no diagram, and prose alone is right; a
 shape left in prose that a picture would carry better is the more common mistake.>

## Where it lives

<One table: path | role. `###` sub-headings group rows by responsibility on large
 domains. Never parallel lists split by file type.>

## Constraints

<Domain-local facts where being wrong is expensive. Each states what breaks when it is
 violated. A fact nobody could act on wrongly is not a constraint.>

## Coupling

<How this domain relates to the rest of the system, for the session that has just arrived here:
 - other domains it constrains or is constrained by — name the domain
 - the skills, commands, and agents that drive or consume it, and how
 - contracts that must change in lockstep
 - known stale cross-references, stated as present-tense facts
 State a fact once, in the domain that owns it; from other domains, point at it rather than restating it.>
```

Write repo-root-relative paths in backticks throughout; a code linkify step renders them to relative links — never @-refs.

**No sixth section.** A fact that fits none of the five belongs inside one of them or nowhere. A page grows a catch-all heading ("Notes", "Other", "worth knowing") at the moment its author stops deciding where facts go, and everything after that lands in discovery order.

**Large domains:** `docs/wiki/` uses a flat layout — one `docs/wiki/<domain>.md` file per functional concern, no subdirectories. For large domains, use internal heading structure within the single file rather than sub-routing.

**Naming continuity:** On rescan, keep existing domain filenames when the underlying repo paths still match. Rename (remove old, write new) only when paths no longer match. This prevents `docs/wiki/auth.md` → `docs/wiki/identity.md` churn when code is unchanged.


## [generated] skip rule

Entries in `docs/wiki/scan.md` marked `[generated]` must be skipped by sub-agents when writing domain file content. Generated files do not drive domain narratives. Changed content SHAs on generated-flagged paths do not trigger domain refresh. Paths matching `[scan] generated` globs in `.claude/atomic.toml` (or a legacy `.signalsignore` `+` line) are flagged `[generated]` by the deterministic scan step.


## File layout

```
docs/wiki/
├── index.md          # router + orientation, @-ref'd from project CLAUDE.md or CLAUDE.local.md
├── CLAUDE.md         # steering, OKF type: Steering; created on first run if absent
├── scan.md           # deterministic substrate, NOT @-ref'd; committed with each refresh
├── auth.md           # domain file, OKF type: Domain
├── billing.md        # domain file, OKF type: Domain
└── cli.md            # domain file, OKF type: Domain
```

Domain files are flat — one `docs/wiki/<domain>.md` per functional concern. `tmp/.scan.prev.md` holds the prior scan content (written by Step 1) as a fallback diff source for environments where git is unavailable; the primary change-set source is `git diff HEAD -- docs/wiki/scan.md`.


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
- Never modify files outside `docs/wiki/`, `.claude/rules/wiki/` (per-domain pointer cards), an ignore file's rules/wiki negation append, or the single `@-ref` target file for wiring. **Why:** scope isolation prevents accidental mutations to source artifacts, specs, or committed config during a signals refresh.
- Errors quoted exact. No paraphrasing. **Why:** paraphrased errors lose the exact token needed to `grep` for the root cause.
- Never block a commit — if the scan fails, log and continue. **Why:** signals are supplemental context, not a build gate.

---
name: atomic-signals-inferrer
description: Orchestrates multi-agent inference pipeline for signals router. Dispatches sub-agents per domain, runs reviewer per domain file, wires cross-domain references, assembles signals.md. Scoped writes only — never touches files outside .claude/project/.
tools: Read, Write, Edit, Grep, Glob, Agent
model: sonnet
---

Signals inferrer orchestrator. Reads the deterministic project snapshot and diff, dispatches sub-agents per domain, validates via reviewer, then assembles `signals.md` (router + orientation). Never touches files outside `.claude/project/`.

## Scope rule

Inputs depend on mode (see below). Outputs are:

- `.claude/project/signals.md` — router + frontloaded orientation, always written.
- `.claude/project/signals/<domain>.md` or `.claude/project/signals/<domain>/index.md` — per-domain detail files, written only when router-only content would exceed ~1,000 lines / ~5k tokens.

The deterministic substrate (`.claude/project/deterministic-signals.md`) is read-only input. Never rewrite it.


## Modes

Two modes. The caller (the `atomic-signals` skill) passes mode via the prompt.

### Incremental (preferred)

Preconditions: `signals.md` already exists AND a diff (changed paths set) is provided.

1. Read the changed-paths set from the diff between prev and current `deterministic-signals.md`.
2. Identify which domain files reference the changed paths.
3. Skip `[generated]` entries — changed content SHAs on generated-flagged files do not trigger domain refresh.
4. Dispatch sub-agents only for affected domains. Leave unaffected domains untouched.
5. After all affected domain files pass reviewer, re-wire cross-domain references for changed domains only.
6. Update `signals.md` to reflect any updated domain content.

### Full (first run or fallback)

Preconditions: `signals.md` does not exist, OR no prior `deterministic-signals.md` is available for diffing.

Run the complete pipeline across all inferred domains (see Pipeline below).


## Migration

On first run when `.claude/project/inferred-signals.md` exists and `.claude/project/signals.md` does not:

1. Back up `inferred-signals.md` to `inferred-signals.md.bak` (same directory).
2. Run the full pipeline: write `signals.md` + domain files if content exceeds budget.
3. Remove `inferred-signals.md`.
4. Print: `Migrated to router shape. Review with \`git diff\`.`
5. Update the project `CLAUDE.md` `@-ref` from `inferred-signals.md` to `signals.md`.

No per-item confirm. This is a documented exception to axiom 3 — the inferrer runs non-interactively inside the `atomic-signals` skill dispatch with no TTY. The backup file and `git diff` serve as the review surface.


## Pipeline

```
Orchestrator reads deterministic diff → identifies domains → dispatches sub-agents → reviewer validates each domain file → orchestrator wires cross-refs → assembles signals.md
```

**Step 1 — Read inputs.**

Read `.claude/project/deterministic-signals.md` end-to-end. On incremental runs, also read the diff output to determine the changed-paths set.

Naming continuity check: read existing `signals/*.md` and `signals/*/index.md` filenames. For each existing domain file, check whether the underlying repo paths in the router table still match. Keep filename if paths match; rename (remove old, write new) if paths no longer match. This prevents churn when code is unchanged.

**Step 2 — Infer domain partitioning.**

Infer domains from structural signals: top-level directories under the primary source root, manifest-declared workspaces (pnpm `packages/`, cargo workspace members, go module dirs), co-located test files as cohesion signals. LLM judgment determines domain boundaries — no mechanical rules. Document the partitioning basis in the router's `## Cross-cutting` section.

Skip `[generated]` entries when partitioning — generated files do not drive domain narratives.

**Step 3 — Dispatch sub-agents per domain.**

For each domain that needs writing or updating, dispatch a sub-agent. Domain writers are document authors, not code implementers — `atomic-builder` and `atomic-surgeon` are scoped to code changes, not markdown signal files — so `general-purpose` is used here:

```
Dispatch sub-agent (general-purpose):
Prompt: "Write signals/<domain>.md for the <domain> domain.

Source paths in this domain: <list from deterministic tree>

Instructions:
- Read the actual source files listed above. Do not infer from filenames alone.
- Skip any entries marked [generated].
- Write a domain file conforming to the domain file schema below.
- Output only the file content. Do not summarize your process.

Domain file schema:
# <domain>
## What it does
<1-3 fact lines>
## Where it lives
<bullet list: path — role>
## What it talks to
<bullet list: external systems / sibling domains (leave blank if none — will be filled by orchestrator)>
## Conventions worth knowing
<domain-local convention facts>

Plain markdown paths throughout. No @-refs. Fact-shaped, not steering-shaped."
```

Sub-agents are bounded to their domain. They read source files in their area only.

**Step 4 — Reviewer validates each domain file.**

After each sub-agent writes its domain file, dispatch a reviewer:

```
Dispatch sub-agent (atomic-reviewer):
Prompt: "Review signals/<domain>.md against the source code.

Domain file path: .claude/project/signals/<domain>.md
Source paths: <list of paths in this domain>

Check:
- Every claim in the domain file is supported by a source file.
- No claims about paths outside this domain.
- Required sections present: What it does, Where it lives, What it talks to, Conventions worth knowing.
- No @-refs (plain markdown paths only).
- Fact-shaped, not steering-shaped.

Return VERDICT: PASS or VERDICT: CHANGES_REQUESTED with specific corrections."
```

If reviewer returns `CHANGES_REQUESTED`, dispatch the sub-agent again with the reviewer's corrections. Iterate until `PASS`. Maximum 3 iterations per domain before flagging as unresolved and continuing. Emit a `⚠️ unresolved` note in the router's `## Cross-cutting` section naming the domain and iteration count.

**Step 5 — Wire cross-domain references.**

After all domain files pass review, read each domain file and populate `## What it talks to` sections with cross-domain references (e.g. "auth talks to billing via webhooks"). The orchestrator has the full picture across domains at this point.

**Step 6 — Assemble signals.md.**

Write `.claude/project/signals.md` with the router shape below.


## Router shape

`signals.md` is a complete orientation document. Two zones:

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
| auth   | src/auth/  | JWT + session, 2FA optional | signals/auth/index.md |
| billing | src/billing/ | Stripe-backed, webhook-driven | signals/billing.md |

(Detail column empty when no domain files exist — small repo, everything in router)

## Cross-cutting

<test layout, conventions pointer, deterministic substrate path, domain partitioning basis>
```

Detail links are plain markdown paths, NOT `@-refs`. `@-refs` are eager and transitive; plain paths require explicit `Read`.

**Budget model.** ~1,000 lines / ~5k tokens triggers domain file creation. Threshold: `~chars_without_whitespace / 4` as token approximation. After domain files exist, router keeps all frontloaded orientation content even if it grows past 5k tokens. Budget is a split trigger, not a cap.


## Domain file shape

Required sections per domain file:

```markdown
# <domain>

## What it does

<1-3 fact lines>

## Where it lives

<bullet list: path — role>

## What it talks to

<bullet list: external systems / sibling domains. Populated by orchestrator after cross-ref wiring.>

## Conventions worth knowing

<domain-local convention facts>
```

Plain markdown paths throughout. No `@-refs`.

**Sub-routing (large domains only):** When a domain is large, write `signals/<domain>/index.md` as the entry-point. The router's Detail column points to `signals/<domain>/index.md`. The `index.md` routes to sibling files (`signals/<domain>/middleware.md`, etc.) via plain markdown links. Same pattern as the top-level router, scoped to one domain.

**naming continuity:** On rescan, keep existing domain filenames when the underlying repo paths still match. Rename (remove old, write new) only when paths no longer match. This prevents `signals/auth.md` → `signals/identity.md` churn when code is unchanged.


## [generated] skip rule

Entries in `deterministic-signals.md` marked `[generated]` must be skipped by sub-agents when writing domain file content. Generated files do not drive domain narratives. Changed content SHAs on generated-flagged paths do not trigger domain refresh. Paths matching `.signalsignore` globs at repo root are flagged `[generated]` by the deterministic scan step.


## File layout

```
.claude/project/
├── signals.md                    # router + orientation, @-ref'd from project CLAUDE.md
├── signals/                      # domain files, NOT @-ref'd
│   ├── auth/
│   │   └── index.md              # large domain: sub-routed
│   ├── billing.md                # small domain: single file
│   └── cli/
│       └── index.md
├── deterministic-signals.md      # substrate, NOT @-ref'd
└── deterministic-signals.prev.md # prev scan for diffing
```

The `signals/` directory is created only when the budget threshold is exceeded.


## Rules

- Every claim in domain files must be sourced from actual file reads, not inferred from filenames.
- Sub-agents read source files in their area. Do not hallucinate structure from the tree alone.
- Reviewer validates each domain file before the orchestrator proceeds.
- Never write `@-refs` in domain files or the router's Detail column — plain markdown paths only.
- Never modify files outside `.claude/project/`.
- `inferred-signals.md.bak` is never overwritten on subsequent runs — only created during migration.
- Errors quoted exact. No paraphrasing.

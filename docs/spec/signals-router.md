# Signals router


## Goal

Replace the flat eager-loaded `inferred-signals.md` with a router-shaped `signals.md` that auto-loads a complete project orientation (framework, build commands, language breakdown, devops, domain index), leaving per-domain detail files on disk for the LLM to `Read` on demand. Eliminates token overflow on large repos while preserving the "Claude already knows where things live" property.


## Non-goals

- Per-subtree `max_depth` override (deferred to v2).
- Backward compatibility with flat-file consumers. This is a **breaking change**.
- LFS / vendor binary handling beyond standard `.gitignore` elision.


## Success criteria

- [ ] `signals.md` is written to `.claude/project/signals.md`; it is `@-ref`'d from the user's project `CLAUDE.md`; it loads in every session.
- [ ] `signals.md` is always the output shape. No flat vs router mode split — one code path.
- [ ] `signals.md` is a complete orientation document: framework/runtime, build/test/lint commands, language breakdown, devops/CI summary, domain route table, cross-cutting conventions.
- [ ] Domain files (`signals/<domain>.md` or `signals/<domain>/index.md`) are written to `.claude/project/signals/` only when the router-only content would exceed ~1,000 lines / ~5k tokens. They are NOT `@-ref`'d; they are NOT eagerly loaded.
- [ ] `deterministic-signals.md` remains on disk at `.claude/project/deterministic-signals.md`; it is NOT `@-ref`'d; it is NOT auto-loaded.
- [ ] Deterministic tree entries include per-path metadata from a single file read: content SHA (hex, 7-char truncated), line count, character count, byte size.
- [ ] Bounded tree: entries at `≤ max_depth` are fully enumerated; entries at `max_depth + 1` show folder name + `(N files, M dirs)` only; entries `> max_depth + 1` are elided (appear only as a count in the parent summary).
- [ ] `max_depth` defaults to `3`; `output.signals.max_depth` in `~/.claude/.atomic/config.toml` overrides it.
- [ ] Change detection uses content SHA diff between prev and current `deterministic-signals.md`. No git commit SHA. No mtime fallback — content SHA works with or without git.
- [ ] Deterministic scan keeps prev version as `.deterministic-signals.prev.md`. Diff between prev and current surfaces changed paths (entries with different content SHAs).
- [ ] Inference pass uses sub-agents per domain. Orchestrator dispatches one sub-agent per domain that needs writing/updating; each reads source files in its area and writes its domain file. Reviewer validates each domain file against source code. Same implement→review pattern as `/subagent-implementation`.
- [ ] Cross-domain references (e.g. "auth talks to billing via webhooks") are wired by the orchestrator after all domain files land.
- [ ] Domain partitioning is LLM-inferred. Structural signals (top-level dirs, workspace manifests) inform the LLM; the LLM judges. No hard precedence rules.
- [ ] No micro-domain consolidation threshold. LLM judges when consolidation makes sense.
- [ ] Inferrer reads existing `signals/*.md` and `signals/*/index.md` as anchor on rescan; keeps filenames when underlying paths still match (naming continuity).
- [ ] First migration from flat `inferred-signals.md`: backs up to `.claude/project/inferred-signals.md.bak`, writes `signals.md` + domain files (if needed), prints "Migrated to router shape. Review with `git diff`." Documented exception to axiom 3 (inferrer is non-interactive inside skill dispatch — no TTY for confirm prompt; backup + `git diff` is the mitigation).
- [ ] `.signalsignore` at repo root: files matching its globs are scanned (appear in tree with metadata) but flagged as generated — inferrer skips them for domain file content. Not full omission. `/atomic-setup` generates a blank `.signalsignore` with commented explanation on repo bootstrap.
- [ ] `doctor signals` check validates: router present + `@-ref`'d; all domain files referenced in router table exist on disk; no orphan domain files under `signals/`; worktrees not cross-compared (check runs per-cwd only).
- [ ] `CLAUDE.md` (bundled global) `@-ref` switches from `inferred-signals.md` to `signals.md`; old ref removed.
- [ ] `atomic-signals-inferrer` agent prompt rewritten for orchestrator role: dispatches sub-agents per domain, runs reviewer per domain file, wires cross-domain refs.
- [ ] `atomic-signals` skill updated to write/update `signals.md` instead of `inferred-signals.md`.
- [ ] `signals-workflow.md` spec gains a change-log entry noting the breaking change and pointing to this spec.


## Approaches

| Option | Pros | Cons |
|--------|------|------|
| **A. Status quo** | Zero work | Breaks on large repos; eager-load wastes tokens on irrelevant domains |
| **B. Bound tree depth, keep eager load** | Small change | Doesn't solve eager-load of irrelevant domains; no fine-grained change detection |
| **C. Router + per-domain files + content-SHA change tracking** | Scales to monorepos; progressive disclosure; bounded auto-load; single code path | More moving parts; breaking change; domain partitioning is heuristic |


## Recommendation

Option C. Evidence from `docs/design/signals-router.md`:

- Flat files today: ~31KB / ~7-8k tokens on this repo. A 50k-file monorepo would exceed context limits.
- Router carries full orientation (~2k tokens frontloaded) + domain route table (~30 tokens/row). Practical ceiling ~7k tokens on extreme repos, ~2-3k on normal ones.
- Content SHA from file reads already happening for LOC counting — no extra I/O.
- Progressive discovery via existing `Read`/`Grep`/`Glob` primitives — no new tool surface.


## Architecture

Deterministic scan feeds content-SHA-based diffs to a multi-agent inference pipeline that produces the router and domain files.

```mermaid
flowchart TD
    A[atomic signals scan] -->|writes| B[deterministic-signals.md\nper-path: SHA + lines + chars + bytes]
    B -->|diff vs .prev| C[changed paths set]
    C -->|input to| D[inferrer orchestrator]
    D -->|dispatches| E[sub-agent: auth domain]
    D -->|dispatches| F[sub-agent: billing domain]
    D -->|dispatches| G[sub-agent: cli domain]
    E -->|writes| H[signals/auth/index.md]
    F -->|writes| I[signals/billing.md]
    G -->|writes| J[signals/cli/index.md]
    D -->|assembles| K[signals.md\nrouter + orientation]
    K -->|@-ref'd from| L[CLAUDE.md]
    H -.->|Read on demand| M[(source tree)]
    I -.->|Read on demand| M
    J -.->|Read on demand| M
```

Only `signals.md` is auto-loaded. Domain files and the deterministic substrate are on disk; the LLM reads them on demand using standard file tools.


## File layout

```
.claude/project/
├── signals.md                    # router + orientation, @-ref'd from CLAUDE.md
├── signals/                      # domain files, NOT @-ref'd (created only when router would exceed budget)
│   ├── auth/                     # large domain: sub-routed
│   │   ├── index.md              # entry-point; router points here
│   │   ├── middleware.md
│   │   └── tokens.md
│   ├── billing.md                # small domain: single file
│   └── cli/                      # large domain: sub-routed
│       ├── index.md              # entry-point
│       ├── commands.md
│       └── doctor.md
├── deterministic-signals.md      # tree + per-path metadata, NOT @-ref'd
└── deterministic-signals.prev.md # prev scan for diffing
```

`@-ref` status:

| File | Auto-loaded | Notes |
|------|-------------|-------|
| `signals.md` | YES — via `@-ref` in project `CLAUDE.md` | Router + full orientation |
| `signals/<domain>.md` or `signals/<domain>/index.md` | NO | Read on demand |
| `deterministic-signals.md` | NO | Substrate; consumed by inference pipeline |


## Router shape

The router is a complete orientation document, not a thin index. Two zones:

**Zone 1 — Frontloaded orientation (~2k tokens).** Fixed cost, does not scale with repo size.

| Section | Content |
|---------|---------|
| `# Project signals` | Header |
| `## Framework & runtime` | Stack, language versions, key dependencies (compressed, not exhaustive) |
| `## Build / test / lint` | Command table: purpose + command + source. CI gate notes. |
| `## Language breakdown` | Counts: language, LOC, file count, percentage |
| `## DevOps & CI` | Release pipeline, deploy mechanism, CI provider. 1-2 lines each. |

**Zone 2 — Domain route table.** Scales with domain count (~30 tokens/row).

| Section | Content |
|---------|---------|
| `## Domains` | Table: Domain / Repo paths / One-liner / Detail link |
| `## Cross-cutting` | Test layout, conventions pointer, deterministic-substrate path |

Domain table format:

```markdown
| Domain  | Repo paths                         | One-liner                     | Detail              |
|---------|------------------------------------|-------------------------------|---------------------|
| auth    | src/auth/, src/middleware/auth.ts  | JWT + session, 2FA optional   | signals/auth/index.md |
| billing | src/billing/, prisma/schema.prisma | Stripe-backed, webhook-driven | signals/billing.md  |
```

- Detail links are plain markdown paths, NOT `@-refs`. `@-refs` are eager and transitive; plain paths require explicit `Read`.
- Detail column is empty when no domain files exist (small repo, everything in router).

**Budget model.** The ~1,000 lines / ~5k tokens threshold triggers domain file creation, not a hard ceiling on the router. After domain files exist, the router keeps all frontloaded orientation content even if it exceeds 5k tokens. The budget is a split trigger, not a cap.

**Token estimation.** `~chars.replace(whitespace).length / 4` as approximation.


## Domain file shape

Required sections:

| Section | Content |
|---------|---------|
| `# <domain>` | Domain name |
| `## What it does` | 1-3 line fact description |
| `## Where it lives` | Bullet list: `path — role` |
| `## What it talks to` | Bullet list: external systems / sibling domains (cross-domain references) |
| `## Conventions worth knowing` | Domain-local convention facts |

Plain markdown paths throughout. No `@-refs`. Fact-shaped, not steering-shaped.

**Sub-routing (large domains only):**

- Inferrer MAY split a domain into `signals/<domain>/index.md` + sibling files.
- `index.md` is the entry-point; router still points here.
- `index.md` routes to sibling files via plain markdown links. Same pattern as the top-level router, scoped to one domain.
- Sub-routing is recursive: same shape applies if a sub-domain grows large.


## Bounded tree

`max_depth` config key: `output.signals.max_depth` (default `3`). Shell-settable per axiom-2 carve-out. No per-subtree override in v1.

| Level | Deterministic tree output |
|-------|--------------------------|
| `≤ max_depth` | Full file enumeration with per-file metadata (content SHA, line count, char count, byte size) |
| `max_depth + 1` | `<folder-name>/ (<N> files, <M> dirs)` — no contents, no per-file metadata |
| `> max_depth + 1` | Elided; contributes to parent's child count only |

Per-path metadata format: content SHA (7-char hex prefix of SHA-256 of file bytes), line count, character count, byte size. Computed from a single file read (same read used for LOC counting).


## Change detection

Content-SHA-based diff between consecutive deterministic scans. Works identically with or without git — no git-specific change detection, no mtime fallback.

1. `atomic signals scan` writes `deterministic-signals.md` with per-path content SHAs. Prev version saved as `.deterministic-signals.prev.md`.
2. `diff` between prev and current surfaces entries with changed SHAs = changed paths.
3. Inferrer orchestrator receives the changed-paths set. Identifies which domain files reference those paths. Dispatches sub-agents only for affected domains.
4. Unaffected domains are left untouched.

**`.signalsignore` handling:**

- Paths matching `.signalsignore` globs are scanned (appear in tree with full metadata) but flagged with a `[generated]` marker.
- Inferrer sub-agents skip `[generated]` entries when writing domain file content — generated files don't drive domain narratives.
- Changed content SHAs on generated files do not trigger domain file refresh.
- `.signalsignore` at repo root, one glob per line. Falls back to empty (no exclusions) if file absent.
- `/atomic-setup` generates a blank `.signalsignore` with commented explanation on repo bootstrap.


## Domain partitioning

LLM-inferred. No hard precedence rules, no consolidation threshold.

Structural signals that inform the LLM's judgment:

- Top-level directories under the primary source root.
- Manifest-declared workspaces: pnpm `packages/`, cargo workspace members, go module dirs.
- Co-located test files as domain cohesion signal.

The LLM judges: what counts as a domain, when to consolidate small related areas into one domain, when to split a large area into sub-domains. No mechanical rules override this judgment.

Inferrer documents the partitioning basis in the router's `## Cross-cutting` section.


## Inference pipeline

The inference pass is a multi-agent orchestration, not a single-agent rewrite.

```mermaid
flowchart TD
    O[Orchestrator] -->|reads| D[deterministic diff\nchanged paths set]
    O -->|dispatches per domain| S1[Sub-agent: domain A]
    O -->|dispatches per domain| S2[Sub-agent: domain B]
    S1 -->|writes| F1[signals/a/index.md]
    S2 -->|writes| F2[signals/b.md]
    F1 -->|reviewed by| R1[Reviewer: domain A]
    F2 -->|reviewed by| R2[Reviewer: domain B]
    R1 -->|PASS/CHANGES_REQUESTED| O
    R2 -->|PASS/CHANGES_REQUESTED| O
    O -->|after all domains pass| X[Wire cross-domain refs\nAssemble router]
```

**Pipeline steps:**

1. Orchestrator reads `deterministic-signals.md` (current) and the diff against prev. Identifies domains needing creation or update.
2. Dispatches one sub-agent per affected domain. Each sub-agent reads the actual source files in its area and writes (or updates) the domain file. Sub-agent scope is bounded to its domain.
3. Reviewer validates each domain file against the source code. Same implement→review loop as `/subagent-implementation`: iterate until PASS.
4. After all domain files pass review, orchestrator wires cross-domain references (`## What it talks to` sections) using the full picture across domains.
5. Orchestrator assembles `signals.md` — frontloaded orientation + domain route table.

On first scan (no prior `signals.md` or domain files): full pipeline runs for all inferred domains. On incremental scan: only affected domains dispatch sub-agents.


## Migration

On first run when `inferred-signals.md` exists and `signals.md` does not:

1. Backs up `inferred-signals.md` to `inferred-signals.md.bak` (same directory).
2. Runs full inference pipeline: writes `signals.md` + domain files (if content exceeds budget).
3. Removes `inferred-signals.md`.
4. Prints: `Migrated to router shape. Review with \`git diff\`.`
5. Updates project `CLAUDE.md` `@-ref` from `inferred-signals.md` to `signals.md`.

No per-item confirm prompt. The design doc (step 8) recommended interactive partition confirm per axiom 4; dropped because the inferrer runs non-interactively inside the `atomic-signals` skill dispatch — no TTY for a confirm prompt. Mitigation: backup file preserves the old output; `git diff` is the review surface. This is a documented exception to axiom 3.


## Naming continuity

On rescan when domain files already exist:

1. Orchestrator reads existing `signals/*.md` and `signals/*/index.md` filenames.
2. For each existing domain file, checks whether the underlying repo paths in the router table still match.
3. If paths match: keep filename. If paths no longer match: rename (old file removed, new file written under updated name).
4. Router table updated to reflect any renames.

Prevents `signals/auth.md` → `signals/identity.md` churn on reruns where code is unchanged.


## Affected artifacts

| Contract change | Artifact | Notes |
|----------------|----------|-------|
| Bounded tree + per-path metadata (content SHA, lines, chars, bytes) | `atomic/internal/signals/` | CLI scan package; writes `deterministic-signals.md` with per-path metadata from single file read |
| `max_depth` config key read | `atomic/internal/config/` | New key `output.signals.max_depth`; rendered in `config.resolved.md` |
| Multi-agent inference pipeline: orchestrator + per-domain sub-agents + reviewers | `agents/atomic-signals-inferrer.md` | Agent prompt rewritten for orchestrator role |
| `.signalsignore` read + `[generated]` flagging | `atomic/internal/signals/` | Read exclusion list; flag matching paths in tree output |
| `@-ref` wiring target switch | `skills/atomic-signals/SKILL.md` | Writes `@signals.md` not `@inferred-signals.md` |
| `@-ref` in bundled global | `CLAUDE.md` (root, bundled) | Replace `@.claude/project/inferred-signals.md` with `@.claude/project/signals.md` |
| `@-ref` in project-local config | `claude.local.md` (repo root, gitignored, not bundled) | Replace `@.claude/project/inferred-signals.md` with `@.claude/project/signals.md` |
| Doctor signals check | `atomic/internal/doctor/checks_signals.go` | Validate router + domain file integrity |
| Blank `.signalsignore` generation | `/atomic-setup` command | Generate commented blank file on repo bootstrap |
| Spec breaking-change note | `docs/spec/signals-workflow.md` | Append change-log entry pointing to this spec |


## Checkpoints

| # | Checkpoint | Files / areas | Agent | Est. files | Verifies |
|---|------------|---------------|-------|-----------|---------|
| 1 | Bounded tree + per-path metadata (content SHA, line count, char count, byte size) in deterministic scan | `atomic/internal/signals/tree.go`, `signals.go`, `signals_test.go` | atomic-builder | 3 | `go test ./internal/signals/...` passes; tree output at depth 3 shows full enum with all 4 metadata fields; depth 4 shows summary; depth 5 elided; content SHA is 7-char hex of SHA-256; metadata derived from single file read |
| 2 | `.signalsignore` read + `[generated]` flagging in scan output | `atomic/internal/signals/signals.go`, `signals_test.go` | atomic-surgeon | 2 | Matching paths appear in tree with `[generated]` marker; content SHA still computed; `.signalsignore` absent = no exclusions; `go test ./internal/signals/...` passes |
| 3 | `output.signals.max_depth` config key | `atomic/internal/config/config.go`, `config_test.go`, `render.go`, `render_test.go` | atomic-surgeon | 4 | Config loads default `3`; explicit value overrides; rendered in `config.resolved.md`; `go test ./internal/config/...` passes |
| 4 | Content-SHA diff: prev vs current deterministic scan, changed-paths extraction | `atomic/internal/signals/signals.go`, `signals_test.go` | atomic-surgeon | 2 | Prev saved as `.prev.md`; diff output lists entries with changed SHAs; `[generated]` paths excluded from changed set; works without git; `go test ./internal/signals/...` passes |
| 5 | Inferrer agent prompt: orchestrator role, sub-agent dispatch, reviewer loop, cross-domain refs, router assembly, migration, naming continuity | `agents/atomic-signals-inferrer.md` | atomic-surgeon | 1 | Prompt describes: sub-agent dispatch per domain, reviewer validation loop, cross-domain ref wiring, router orientation sections (framework, build, language, devops, domain table), domain file sections, naming-continuity rule, migration instructions, `.signalsignore`/`[generated]` skip rule; `grep -c` confirms key phrases |
| 6 | `atomic-signals` skill + bundled `CLAUDE.md` + `claude.local.md` `@-ref` switch | `skills/atomic-signals/SKILL.md`, `CLAUDE.md` (root), `claude.local.md` | atomic-surgeon | 3 | Skill references `signals.md` not `inferred-signals.md`; `CLAUDE.md` and `claude.local.md` `@-ref`s updated; bundle regen (`make -C atomic bundle`) exits 0 |
| 7 | `/atomic-setup` blank `.signalsignore` generation | `commands/atomic-setup.md` | atomic-surgeon | 1 | Command generates `.signalsignore` with commented explanation when absent; does not overwrite existing |
| 8 | Doctor `signals` check update | `atomic/internal/doctor/checks_signals.go`, `checks_signals_test.go` | atomic-surgeon | 2 | Validates: router present + `@-ref`'d; domain files in router table exist on disk; no orphan files in `signals/`; check runs per-cwd only (no worktree cross-compare); `go test ./internal/doctor/...` passes |
| 9 | Spec cross-references + signals-workflow change-log | `docs/spec/signals-workflow.md`, `docs/spec/signals-router.md` (this file) | atomic-surgeon | 2 | `signals-workflow.md` change-log entry appended noting breaking change + pointer to this spec; no body sections deleted; `atomic validate spec` exits 0 on both files |


## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Migration axiom-3 tension: auto-migrate removes `inferred-signals.md` without per-item confirm | medium | Documented exception to axiom 3. Inferrer backs up to `.bak` before removal; `git diff` is the review surface; inferrer prints review hint. Inferrer is non-interactive inside skill dispatch (no TTY). |
| Worktree cross-compare in doctor | low | Doctor `signals` check anchored to cwd via `repoctx.Toplevel()`. Each `.worktrees/<branch>/` carries independent `.claude/project/`. Check must not traverse sibling worktrees. |
| Generated-file churn thrashing domain files | medium | `.signalsignore` flags matching paths as `[generated]`; inferrer skips them for domain content; changed SHAs on generated files do not trigger domain refresh. |
| Sub-agent quality on unfamiliar codebases | medium | Each sub-agent reads actual source files in its domain (not just the tree). Reviewer validates against source code. Iterate until PASS. Low-quality output is caught by the review loop. |
| Naming continuity failure on large structural refactors | low | When paths no longer match, orchestrator writes new filename and removes old. Removes old file only after new file is fully written. |
| Scan performance on very large repos (50k+ files) | medium | Deterministic scan reads every file once (for content SHA + LOC). This is the cost of accurate metadata. No incremental scan in v1 — content SHA diff handles incremental inference. |


## Change log

### 2026-05-23 — Pressure-test amendments

**What changed:**

16 decisions from pressure-test session amended the spec:

1. Router is always `signals.md` — dropped flat vs router two-mode split.
2. Router is a complete orientation document (framework, build commands, language breakdown, devops, domain table) — not a thin 10-line index.
3. Budget model changed: ~1,000 lines / ~5k tokens is a split trigger, not a ceiling. Router can grow with frontloaded content.
4. Domain files created only when router-only exceeds budget. No separate activation threshold.
5. Domain partitioning is LLM-inferred — dropped "structural > LLM cluster" precedence.
6. Large domains sub-route recursively via `signals/<domain>/index.md` (not `<domain>.md`).
7. Change detection uses content SHA (from file reads already happening for LOC) — dropped git commit SHA and mtime fallback.
8. Per-path metadata expanded: content SHA + line count + character count + byte size.
9. Deterministic-to-deterministic diff drives incremental refresh — dropped `git diff --name-only` step.
10. `.signalsignore` = scan but flag as `[generated]`, not full omission. Inferrer skips for domain content.
11. `.signalsignore` is user-created; `/atomic-setup` generates blank with comments.
12. Inference pass uses sub-agents per domain with reviewer validation — not single-agent rewrite.
13. Cross-domain references wired by orchestrator after all domain files pass.
14. Micro-domain consolidation threshold dropped — LLM judges.
15. Activation oscillation: no anti-thrash mechanism needed — LLM recognizes no change.
16. Added `/atomic-setup` blank `.signalsignore` to checkpoints and affected artifacts.

**Why:** Pressure-test session (`/pressure-test @docs/spec/signals-router.md`) surfaced contradictions in the original spec: router budget too small for orientation content, content SHA available for free from existing file reads making git SHA redundant, single-agent inference insufficient for quality on large repos.

**Superseded:** Original spec used ~2,048 token budget, git commit SHA for change detection, mtime fallback outside git, `git diff --name-only` for incremental refresh, LLM-judged ~500-line flat-vs-router activation, structural > LLM partitioning precedence, < 3 files micro-domain consolidation threshold, single-agent inferrer, `signals/<domain>/<domain>.md` entry-point naming.

---
name: atomic-wiki-inferrer
description: >
  Scope-sensitive wiki/signals inferrer. Detects <wiki-type> from dispatch args or
  the active wiki index, loads the matching pipeline reference from the atomic-wiki
  skill (references/repo.md for repo scope, references/realm.md for realm scope),
  and executes that pipeline. Orchestrates rather than authors: it scans, classifies domains,
  dispatches one atomic-wiki-writer per domain and atomic-reviewer per page, then
  assembles the router and wires the @-ref. Runs in its own context so the scan,
  which is thousands of lines, never enters the caller's. Dispatched by
  /refresh-wiki (interactive) and ship verbs (silent). Scoped writes only — never
  touches files outside the active wiki root, `.claude/rules/wiki/` (per-domain
  pointer cards), an ignore file's rules/wiki negation append, or the @-ref
  target file.
skills: [atomic-writing]
---

Wiki inferrer: detects scope from dispatch args or the `<wiki-type>` block, reads the matching pipeline reference bundled with the `atomic-wiki` skill (`references/repo.md` or `references/realm.md`), and executes that pipeline.

You orchestrate; you do not author pages. Page writing goes to `atomic-wiki-writer`, one dispatch per domain, and page review to `atomic-reviewer`. Dispatch each by its stable identity, and never leave the agent to a default — an implicitly chosen agent carries none of the wiki contract. Your own context exists so the scan, which runs to thousands of lines, stays out of the caller's.

**Before inferring, read the scope's shared steering file — `docs/wiki/AGENTS.md`, falling back to the `CLAUDE.md` loader beside it when the shared file is absent — and treat its contents as authoritative steering for this run.** If it exists, its instructions override inference defaults. If it does not exist, the repo pipeline will create the pair (Step 8c).

## Contract

- **Intent.** Run the wiki pipeline end to end for the detected scope: scan, classify domains, delegate authoring and review, assemble the router, wire the steering reference.
- **Required capabilities.** Read and write within the wiki scope; run shell commands for the deterministic scan and git queries; dispatch subagents.
- **Write scope.** The active wiki root, `.claude/rules/wiki/` pointer cards, an ignore file's `rules/wiki` negation append, and the one steering-reference target. Nothing else.
- **Execution.** Fresh context (the scan runs to thousands of lines); `mode: interactive` or `mode: silent`; orchestrates, never authors.
- **Dependencies.** Skill `atomic-writing` (declared in `skills:` frontmatter); the `atomic-wiki` skill's repo and realm pipeline references; delegates to `atomic-wiki-writer` and `atomic-reviewer`.
- **Enforcement.** Instruction-only. Scoped writes and the no-author rule are not machine-checked; the required capabilities include writes and delegation.

{{ template "agent-atomic-voice" . }}

## Caller-provided context

The caller (command or ship verb) passes mode and context via the dispatch prompt:

- **`mode: interactive`** — full pipeline with report. Return concerns table if any found.
- **`mode: silent`** — scan + infer + wire. Suppress report. Discard concerns.
- **`steering:`** block — contents of the scope's shared steering file (`docs/wiki/AGENTS.md`, or the `CLAUDE.md` loader beside it when the shared file is absent), if it exists and is not all comments. Treat as ground truth — steering wins over inference.
- **`first_run: true`** — no prior signals exist; equivalent to `scope: full`. Run full pipeline, not incremental.
- **`scope: incremental|full`** — pre-computed refresh scope from the caller. When present, the agent uses this value directly and skips the Step 2b decision tree. `scope: full` forces complete re-infer of all domains. `scope: incremental` limits re-infer to changed domains derived from the diff. When absent (and `first_run` is also absent), the agent computes scope via the Step 2b decision tree in `references/repo.md` — full when no prior `docs/wiki/index.md`, when the `<scan-sha>` tiebreaker fires (committed scan.md blob SHA ≠ stored `<scan-sha>`), or when the git diff line-delta exceeds ~20%; incremental otherwise.
- **`changed_range: <from-sha>..<to-sha>`** — scopes incremental re-inference to the paths changed in this git range. When present, the agent derives the changed-paths set from `git diff --name-only <from-sha>..<to-sha>` unioned with uncommitted changes (`git diff --name-only <from-sha>`), instead of the `git diff HEAD -- docs/wiki/scan.md` scan diff. The deterministic scan (Step 1) still runs whole-repo; only domain re-inference is scoped. Absent → changed-paths set comes from the scan diff (Step 2b). Ignored in wiki-output and bucket-synthesis modes.
- **`target_repo: <abs-path>`** + **`wiki_dir: <abs-path>`** — activates wiki-output mode (realm scope). Both must be present together. If exactly one is supplied, refuse immediately and name the missing argument — do not fall back to default mode.
- **`bucket_name: <name>`** + **`bucket_path: <abs-path>`** + **`wiki_dir: <abs-path>`** — activates bucket-synthesis mode (realm scope). All three must be present together. If `bucket_name` or `bucket_path` is supplied and any of the three is missing, refuse immediately and name the missing arg(s). `wiki_dir` alone (without `bucket_name` or `bucket_path`) never triggers this guard.


<workflow>

## Scope detection and execution

### 1. Detect scope

Determine scope from dispatch args first (most reliable signal):

- **Realm scope** — any of these is present: `bucket_name`, `bucket_path`, or (`target_repo` AND `wiki_dir`). Load `references/realm.md` from the `atomic-wiki` skill.
- **Repo scope** — none of the above realm args are present. Load `references/repo.md` from the `atomic-wiki` skill.

If scope is ambiguous (no dispatch args, fresh repo), check:
- `docs/wiki/index.md` exists with `<wiki-type>repo</wiki-type>` → repo scope.
- `wiki/index.md` exists at a parent directory with `<wiki-type>realm</wiki-type>` → realm scope.
- Neither exists → repo scope (first run; the repo pipeline creates `docs/wiki/`).

### 2. Load the reference

The reference files ship inside the `atomic-wiki` skill at `references/repo.md` and `references/realm.md`. Resolve the skill's installed directory through the path the runtime reports for it, then read the matching scope's reference. Never inline pipeline steps from memory.

### 3. Execute the pipeline

Follow the pipeline defined in the reference file exactly. The reference is the authoritative workflow:

- **Repo scope** — execute Steps 1-9 (scan → infer → write `docs/wiki/` files → wire `@-ref`). Scope is determined at Step 2b: full when no prior `docs/wiki/index.md`, when the `<scan-sha>` tiebreaker fires (scan committed without re-infer), when git diff line-delta exceeds ~20%, or when the caller passed `scope: full` / `first_run: true`; incremental otherwise. Sub-dispatch a domain writer (`atomic-wiki-writer`) and a reviewer (`atomic-reviewer`) as described in the reference.
- **Realm scope** — execute the wiki-output pipeline (W1-W7) when `target_repo` + `wiki_dir` are present, or the bucket-synthesis pipeline (B1-B5) when `bucket_name` + `bucket_path` + `wiki_dir` are present.

</workflow>

{{ template "agent-code-intel" . }}

{{ template "agent-where" . }}

<constraints>

## Rules

- Load and follow the reference file exactly. Do not inline pipeline steps from memory — the reference is the source of truth and may have been updated since the agent was built.
- Sub-agents are bounded to their domain. They read source files in their area only.
- Reviewer validates each domain file before the orchestrator proceeds.
- Never write `@-refs` in domain files or the router's Detail column. Write repo-root-relative paths in backticks; `atomic signals linkify` renders them to file-relative markdown links.
- Never modify files outside the active wiki root, `.claude/rules/wiki/` (per-domain pointer cards), an ignore file's rules/wiki negation append, or the single `@-ref` target file for wiring.
- Errors quoted exact. No paraphrasing.
- Never block a commit — if the scan fails, log and continue.

</constraints>

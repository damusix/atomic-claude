# Spec: signals workflow


The signals workflow keeps Claude aware of the *current* shape of a project without hallucination. A Go binary (`atomic signals scan`) produces a deterministic, machine-generated snapshot. A subagent reads that snapshot and writes an inferred companion. Both files are auto-referenced from the project's `claude.md` so the harness loads them on every session.


This spec depends on [`atomic-binary.md`](./atomic-binary.md) — the `atomic` binary must be installed for the primary path. A markdown-only fallback exists for users without the binary.


## Files produced


| Path | Source | Purpose |
|------|--------|---------|
| `.claude/project/deterministic-signals.md` | `atomic signals scan` (regenerated every run) | Pure facts: tree, manifests, languages, lockfile presence |
| `.claude/project/inferred-signals.md` | `atomic-signals-inferrer` subagent (regenerated every run) | Inferred meaning: framework, build commands, test runner, deployment target, architectural style |


Both files are committed to the project (not gitignored). They travel with the repo so any future Claude session can read them without re-scanning. The inferrer gets an incremental-diff source for free via `atomic signals diff` — a thin wrapper that delegates to `git diff` in a git repo or unix `diff` against `.deterministic-signals.prev.md` (a snapshot `scan` writes before overwriting) outside one.


## Artifacts to build


| Artifact | Type | Path |
|----------|------|------|
| `atomic-signals` | skill | `skills/atomic-signals/SKILL.md` |
| `atomic-signals-inferrer` | agent | `agents/atomic-signals-inferrer.md` |
| `/initialize-signals` | command | `commands/initialize-signals.md` |
| `/commit-only` (edit) | command | `commands/commit-only.md` — invoke skill pre-commit when source changed |
| `/atomic-setup` (edit) | command | `commands/atomic-setup.md` — propose binary install + `/initialize-signals` |
| `claude.md` (edit) | bundled global | this repo's root `claude.md` (the one that ships as `~/.claude/CLAUDE.md` via the embed bundle) gets a section mentioning the signals workflow so users know it exists |


Distinct from the bundled-global `claude.md` above, the per-project `claude.md` at each user's repo root is mutated at runtime by the skill (step 5 below) to add the `@-refs` to the project's signals files. Two different files, both named `claude.md`.


## Skill: `atomic-signals`


### Trigger phrases


Auto-fires on natural language that implies "the project state changed and Claude needs to know":


- "regenerate signals"
- "scan the project"
- "refresh project context"
- "what's in this repo"
- "rescan"
- Plus explicit invocation: `Skill skill=atomic-signals`.


Also fires implicitly when `/commit-only` runs and the staged diff includes source-tree changes (see [Integration with `/commit-only`](#integration-with-commit-only)).


### Flow


1. **Detect binary**. `command -v atomic`. If missing: print "atomic binary not installed. install via [link]. falling back to markdown-only mode." and continue with the fallback flow.
2. **Staleness check (binary path)**. Run `atomic signals stale`. Exit 0 → no work. Exit 1 → regenerate.
3. **Regenerate deterministic**. Run `atomic signals scan`. Writes `.claude/project/deterministic-signals.md` and copies the prior content to `.claude/project/.deterministic-signals.prev.md` (gitignored) so `atomic signals diff` works regardless of git state.
4. **Dispatch inferrer**. Spawn `atomic-signals-inferrer` subagent via `Agent` tool. The inferrer runs `atomic signals diff` to learn what changed and updates only the dependent sections of `inferred-signals.md`. See agent spec below for details.
5. **Ensure `@-refs` in project `claude.md`**. If `claude.md` exists at repo root and does not already contain `@.claude/project/deterministic-signals.md` and `@.claude/project/inferred-signals.md`, append a section:

    ```markdown


    ## Project signals (auto-loaded)


    @.claude/project/deterministic-signals.md
    @.claude/project/inferred-signals.md
    ```

    Print the diff first. If running non-interactively (e.g. inside `/commit-only`), append without confirmation. If running from `/initialize-signals`, ask via `AskUserQuestion` before writing.

6. **Report**. Print one-line summary: `signals refreshed. <N> sections changed. inferrer updated <M> sections.`


### Fallback (no binary)


When `atomic` is absent:


1. Skip staleness check (always regenerate).
2. Run `find . -type f -not -path './node_modules/*' -not -path './.git/*' | head -200 > .claude/project/deterministic-signals.md` (very crude).
3. Skip the inferrer (it needs structured input).
4. Print: "fallback mode produced a tree-only signals doc. install atomic for full functionality."


The fallback is deliberately limited — users hit it once and install the binary.


## Agent: `atomic-signals-inferrer`


### Frontmatter


```yaml
---
name: atomic-signals-inferrer
description: Reads deterministic-signals.md and produces inferred-signals.md — framework detection, command guesses, architectural style. Read-write but scoped to .claude/project/.
tools: Read, Write, Grep, Glob
model: sonnet
---
```


### Job


Two modes:


- **Incremental (preferred)** — `atomic signals diff` exits 1 (diff present). Read its stdout. Use the unified-diff hunk headers (`## Manifests`, `## Languages`, etc.) to identify which deterministic sections changed, then use the section dependency mapping below to find which inferred sections need updates. Read `inferred-signals.md`, edit only the dependent sections in place, leave untouched sections byte-identical.
- **Full (first run or fallback)** — `inferred-signals.md` does not exist, or `atomic signals diff` exits 2 (no prior version available). Read `.claude/project/deterministic-signals.md` end-to-end. Write `inferred-signals.md` from scratch.


No custom diff format. Standard unified diff via the binary's `diff` wrapper.


### Section dependency mapping


Each deterministic section drives one or more inferred sections. The inferrer uses this table to scope updates in incremental mode:


| Deterministic section changed | Inferred sections to refresh |
|------------------------------|------------------------------|
| `Tree` | `Architectural style`, `Conventions detected` |
| `Manifests` | `Framework / runtime`, `Build / test / lint commands` |
| `Languages` | `Framework / runtime`, `Architectural style` |


If a deterministic section changes but no inferred section depends on it, leave `inferred-signals.md` untouched and report `0 sections updated`. Always refresh the frontmatter `generated_at` timestamp.


Input: `.claude/project/deterministic-signals.md` + `atomic signals diff` output (incremental mode).


Output: `.claude/project/inferred-signals.md` with these sections:


```markdown
---
generated_at: <ISO timestamp>
source: .claude/project/deterministic-signals.md
---

# Inferred signals

## Framework / runtime
<one line per detected framework with evidence>

## Build / test / lint commands
<commands extracted from manifests, with source>

## Architectural style
<monorepo? service-oriented? library? CLI? web app?>

## Conventions detected
<test layout, source layout, lint config style>

## Risks / unknowns
<things the deterministic file did not cover>
```


### Rules


- Read only the deterministic signals file plus any *manifests it cites* (package.json, Cargo.toml, etc.) for cross-reference. Do not crawl source code.
- In incremental mode, read only the `atomic signals diff` output, plus the cited manifests for changed sections. Do not re-read the full deterministic file — token discipline is the whole point of the incremental path.
- Every claim must cite evidence: a file path, a manifest key, a tree pattern. Unsourced claims are forbidden.
- "Risks / unknowns" must be non-empty — every project has gaps, and surfacing them keeps the file honest.
- Never modify files outside `.claude/project/`.
- Output is plain markdown. No prose padding, no hedging.
- Always preserve untouched sections byte-identical. The only frontmatter field that updates without a corresponding section change is `generated_at`.


## Command: `/initialize-signals`


### Purpose


One-shot bootstrap for a project that has never had signals generated. Verbose, interactive, idempotent on second run.


### Flow


1. Pre-flight: verify inside a git repo. Verify `atomic` binary is on `$PATH`; if not, print install instructions and stop.
2. Check if `.claude/project/deterministic-signals.md` already exists.
   - Yes → print "signals already initialized. running refresh instead." and delegate to the `atomic-signals` skill.
   - No → continue.
3. Run `atomic signals scan`. Print the resulting file's section headers (not full content).
4. Dispatch `atomic-signals-inferrer`. Wait for completion.
5. Detect project `claude.md`:
   - Exists → print the proposed `## Project signals (auto-loaded)` section diff. Ask via `AskUserQuestion`: "Append the auto-load section to claude.md? Yes / Show me the diff again / No, skip".
   - Missing → ask via `AskUserQuestion`: "No claude.md found. Create a starter? Yes (uses /atomic-setup template) / No, skip".
6. Report final state. Suggest committing the new files.


### Refusals


- No git repo → stop.
- No `atomic` binary → stop with install instructions.
- Existing `claude.md` already has both `@-refs` → confirm, take no action.


## Integration with `/commit-only`


Edit `/commit-only` to invoke the `atomic-signals` skill *before* the commit, gated by source-file or manifest detection:


```
1. Stage check (existing).
2. If staged diff touches a source file OR a known manifest AND atomic is installed:
   - Invoke atomic-signals skill silently.
   - If signals regenerated, stage the resulting deterministic-signals.md + inferred-signals.md.
3. Continue with existing commit flow.
```


Trigger surfaces:


- **Source extensions** — `.ts .tsx .js .jsx .py .go .rs .rb .java .c .cc .cpp .h .hpp .swift .kt .php`. Extend per language as the binary's scanner grows.
- **Manifest filenames** — `package.json`, `tsconfig.json`, `Cargo.toml`, `pyproject.toml`, `requirements.txt`, `Gemfile`, `composer.json`, `pom.xml`, `build.gradle`, `build.gradle.kts`, `go.mod`, `go.sum`. Matched by exact filename, not extension — a generic `foo.json` or `bar.toml` does NOT trigger.


Skip if:


- `atomic` binary not installed.
- Staged diff is prose-only (`.md`) or non-manifest config (generic `.yml .yaml .json .toml` not on the manifest list above).
- Signals are already fresh per `atomic signals stale`.


This keeps the commit verb fast in the common case (docs commits, single-file fixes) while ensuring source changes always carry an updated snapshot.


## Integration with `/atomic-setup`


Edit `/atomic-setup` audit table to include:


| Convention | Check |
|-----------|-------|
| `atomic` binary on PATH | `command -v atomic` |
| `.claude/project/deterministic-signals.md` | `test -f` |
| `claude.md` references signals files | grep for `@.claude/project/deterministic-signals.md` |


Proposed actions:


- Binary missing → action: print install command (`curl ... install.sh | bash`). Setup itself does not install — user runs the curl.
- Signals files missing but binary present → action: run `/initialize-signals` as a follow-up.
- `claude.md` missing `@-refs` → action: append the auto-load section (after binary + scan done).


## Open follow-ups


- The "staleness" definition in `atomic signals stale` — should it look only at file mtimes, or also at content hashes? Spec defaults to mtime; revisit if false-positives are common.
- Inferrer accuracy on multi-language repos — initial version handles one primary language well; polyglot repos may need iteration.
- When the inferrer disagrees with a prior run (different claims) on an *unchanged* deterministic section, the incremental path will not catch it — by design, untouched sections are preserved byte-identical. Surface this trade-off in the README. Workaround: user invokes the skill with `--force-full` (future flag) to re-infer from scratch.


## Success criteria


- A fresh project can run `/initialize-signals` and end with both signals files written, `claude.md` updated, and both files referenced via `@`.
- Re-running `/initialize-signals` is a no-op (the skill detects fresh state).
- A `/commit-only` that touches `src/foo.ts` regenerates signals and stages the updated docs alongside the commit.
- A `/commit-only` that only touches `README.md` does NOT regenerate signals.
- A `/commit-only` that touches `package.json` (or any other manifest on the trigger list) regenerates signals and stages the updated docs alongside the commit.
- Removing the binary and re-running the skill produces the fallback message and a degraded-but-non-empty signals file.
- When `package.json` changes its `scripts.test` value, the skill's incremental path: (a) `atomic signals diff` returns the `Manifests` hunk after `atomic signals scan`, (b) the inferrer reads only that diff + the cited manifests, and (c) `inferred-signals.md` updates only `Build / test / lint commands`. Other sections of `inferred-signals.md` are byte-identical to the previous run.


## Checkpoints


| CP | Lands |
|----|-------|
| S-1 | `atomic-signals-inferrer` agent |
| S-2 | `atomic-signals` skill |
| S-3 | `/initialize-signals` command |
| S-4 | Edit `/commit-only` to invoke skill pre-commit |
| S-5 | Edit `/atomic-setup` audit + propose flow |
| S-6 | Update `claude.md` + `CLAUDE.md` + `README.md` tables to document signals |


## Implementation log


### v0.1 — 2026-05-17


Built across 6 iterations of `/subagent-implementation` plus two follow-up polish iterations, then squash-merged into `main` as **`3feaa63`** — that one commit on `main` is the canonical record. The branch and its per-iteration commits below were rebased onto current `main` (dropping two stale predecessors that had been replaced upstream) and then collapsed; the original SHAs are kept for orientation only and are no longer reachable from any branch.


Iteration trail before squash (oldest first, all collapsed into `3feaa63`):


- `88f9bf4` → rebased to `9695f8f` — CP S-1 `atomic-signals-inferrer` agent
- `d6b21e5` → `e9b301c` — CP S-2 `atomic-signals` skill
- `3d59677` → `f0921c9` — CP S-3 `/initialize-signals` command
- `5bc18e3` → `bf9e1d3` — CP S-4 `/commit-only` invokes atomic-signals pre-commit
- `a4e6fe9` → `3a40a88` — CP S-5 `/atomic-setup` audits signals workflow
- `4d34aba` → `8793c1e` — CP S-6 docs (claude.md, CLAUDE.md mirror, README)
- `8d3e1cd` → `300b8ae` — polish: F-1 (install URL), F-2 (skip-order), F-3 (silent mode definition)
- `54c75c5` → `68c21db` — initial implementation log
- `05650ed` → `1ebb8ab` — F-4: manifest-filename trigger surface


**Out-of-scope work performed during this build:** none.


**Unforeseens — surprises that emerged during implementation:**


- `claude.md` and `CLAUDE.md` collide on macOS APFS (case-insensitive). The bundler design assumes them distinct, but git only tracks `claude.md`. Surfaced during S-6 review; tracked as F-5.
- Spec wording `Yes (uses /atomic-setup template)` for the `/initialize-signals` missing-claude.md branch implied invoking the full `/atomic-setup` flow. Implementation deviated to write a minimal starter directly — scope-creep avoidance. Spec note at line 205 should be updated to match if the deviation is endorsed.
- Rebase onto current `main` (immediately before squash) hit two conflicts because `main` had independently added rows to `commands/atomic-setup.md` (hook-install audit/propose rows) and `README.md` (slash-command table rows for `/remind-me` and `/follow-up`). Both branches' rows belong; the conflict resolution kept both sets. The redundant duplicate "atomic binary on PATH" audit row was dropped in favour of `main`'s `found/missing` wording, and `main`'s `go install` install command was replaced with the canonical curl URL (consistent with `/initialize-signals`).


**Deferred items still open:**


- ~~**F-4** — `.json` excluded from `/commit-only` source-extension list.~~ **Closed during build.** Resolved by introducing a manifest-filename trigger surface separate from the generic extension list (`package.json`, `tsconfig.json`, `Cargo.toml`, `pyproject.toml`, etc.). Spec "Integration with /commit-only" and success criteria updated.
- **F-5** — `claude.md` vs `CLAUDE.md` case-insensitive collision. Repo-design question, not signals-workflow scope. Recorded here for follow-up.


**Closed during build:** F-1, F-2, F-3 (originally `8d3e1cd`, rebased to `300b8ae`); F-4 (originally `05650ed`, rebased to `1ebb8ab`). All folded into the final squash `3feaa63`.

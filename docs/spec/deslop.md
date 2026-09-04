# /deslop: standing-codebase convention audit


## Goal


A `/deslop` command audits an existing codebase against conventions atomic already
defines, writes an indexed report into a scratchpad bundle for the user to read,
and — only on a second explicit invocation — applies accepted findings through the
surgical implementer without changing behavior.


## Non-goals


- Bug hunting. `atomic-reviewer` and `/code-review` own correctness.
- Replacing a linter. Anything the project's linter already catches is out of scope.
- Architectural refactoring. Moving or renaming a module is a design decision, not slop removal.
- A CI gate. The audit is judgment-based and will not produce byte-identical runs.
- A new style ruleset. Every category resolves to a rule atomic already carries.


## Success criteria


- [ ] `/deslop` with no arguments audits the whole repo; `/deslop <path>` scopes to a subtree.
- [ ] Phase 1 writes only inside the scratchpad bundle — `git status` shows no source file modified after an audit.
- [ ] The audit shards by wiki domain when `docs/wiki/index.md` exists, and by top-level source directory when it does not.
- [ ] Every finding in `REVIEW.md` carries an id, a `path:line`, a category, a safety tier, and a one-line fix.
- [ ] Every finding's category maps to a rule named in the design's category table; a finding with no such rule is not emitted.
- [ ] `REVIEW.md` records the audit SHA; `/deslop apply` refuses when `HEAD` has moved and the recorded findings' files have changed since.
- [ ] `/deslop apply` with no prior `REVIEW.md` refuses with a message naming `/deslop`.
- [ ] Findings on exported/public symbols, dynamic references, and generated files are emitted as `report-only` and are never auto-fixed.
- [ ] When the repo has no runnable test suite, every `guarded` finding is emitted as `report-only`, and `REVIEW.md` states why.
- [ ] `/deslop apply` establishes a green baseline before its first edit and refuses to proceed if the baseline is already red.
- [ ] `/deslop apply` re-runs the suite after each batch and stops on the first regression, leaving prior batches committed.
- [ ] `atomic-deslopper` is granted `Write` but not `Edit`, and its prompt confines that write to the scratchpad findings file and forbids any mutating command. Confinement is prompt-level: the harness has no path-scoped tool grant, so no frontmatter field enforces it.
- [ ] `/deslop` appears in `/atomic-help`'s topic table and in the maintenance tour stage.
- [ ] `atomic-deslopper` appears in `docs/reference/agents.md`; `/deslop` appears in `docs/reference/commands.md` and the README command list.
- [ ] `make -C atomic bundle` succeeds with both new artifacts present (every `{{ template }}` directive resolves).


## Approach


Two-phase command with a human gate: a parallel read-only audit sharded by wiki
domain writes an indexed, tier-annotated report, and a separate `apply`
invocation drives accepted findings through `atomic-implementer` in surgical mode
— see `docs/design/deslop.md`.


## Change tree


```
context/
├── CLAUDE.md ....................... M  (workflow section: standing-audit entry)
├── agents/
│   └── atomic-deslopper.md ......... A  (new: read-only shard auditor)
└── commands/
    ├── deslop.md ................... A  (new: two-phase orchestrator)
    └── atomic-help.md .............. M  (topic row + maintenance tour stage)
docs/
├── design/deslop.md ................ A  (design record)
├── spec/deslop.md .................. A  (this file)
└── reference/
    ├── commands.md ................. M  (/deslop row)
    └── agents.md ................... M  (atomic-deslopper row + dispatch tree)
README.md ........................... M  (command list line)
```


## Outline


```
context/agents/atomic-deslopper.md
  frontmatter — name, description, tools (Read/Grep/Glob/Bash/Write), skills, model, effort
  Categories — the eight slop categories, each bound to its source rule
  Safety tiers — how a tier is assigned to a finding
  Workflow — orient, sweep per category, prove dead code, assign tiers, write shard file
  Output format — the per-shard findings file shape
  Rules — read-only constraints, scratchpad-only writes, no-invented-rules

context/commands/deslop.md
  frontmatter — description
  Argument parsing — bare, <path>, apply
  Phase 1 Audit
    Pre-flight — git repo, index warmth, baseline suite probe
    Shard resolution — wiki domains, else top-level source dirs
    Scratchpad — bundle creation, purpose review
    Fan-out — parallel atomic-deslopper dispatch, one per shard
    Assemble — merge shard files into REVIEW.md, dedupe, count by tier
  Phase 2 Apply
    Staleness gate — recorded SHA vs HEAD
    Selection — which findings the user accepted
    Baseline — green before first edit
    Batch loop — surgical implementer per batch, re-verify, commit
  Rules — constraints for both phases
```


## Flows


**Flow: audit**

1. user runs `/deslop` (optionally with a path scope)
2. orchestrator confirms a git work tree, syncs the code-intel index when warm, and probes for a runnable test suite
3. orchestrator resolves shards from `docs/wiki/index.md` domains, falling back to top-level source directories
4. orchestrator creates the scratchpad bundle with `atomic scratchpad new deslop-<date> --purpose review` and records `HEAD`
5. orchestrator dispatches one `atomic-deslopper` per shard in parallel, each writing `findings/<shard>.md` inside the bundle
6. orchestrator merges the shard files into `REVIEW.md` — findings indexed `D-1..D-n`, grouped under a header per tier and ordered by path then line within each, with counts per tier and per category up front
7. orchestrator prints the bundle path and the per-tier counts, and stops

**Flow: apply**

1. user runs `/deslop apply`, naming accepted finding ids or a whole tier
2. orchestrator reads `REVIEW.md`, and refuses if the recorded SHA has moved and any named finding's file changed since
3. orchestrator runs the test suite for a baseline; a red baseline stops the run before any edit
4. orchestrator groups accepted findings into batches by file, and dispatches `atomic-implementer` in surgical mode per batch with the findings as scope
5. after each batch the orchestrator re-runs the suite; green commits the batch, red stops the run with prior batches left committed
6. orchestrator marks applied findings in `REVIEW.md` and reports what landed and what remains

**Flow: refusal on an untested repo**

1. Phase 1 pre-flight finds no runnable test command
2. every finding the shard agents tier as `guarded` is rewritten to `report-only` during assembly
3. `REVIEW.md` states the demotion and its reason in the header
4. `/deslop apply` therefore has only `safe` findings available to act on


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Write the `atomic-deslopper` agent: categories bound to source rules, tier assignment, per-shard output format, read-only constraints | `context/agents/atomic-deslopper.md` | atomic-implementer (mode: surgical) | 1 | `make -C atomic bundle` resolves every partial directive |
| 2 | Write the `/deslop` command: both phases, shard resolution, fan-out, assembly, staleness and baseline gates | `context/commands/deslop.md` | atomic-implementer (mode: surgical) | 1 | `make -C atomic bundle` succeeds; argument branches cover bare / `<path>` / `apply` |
| 3 | Register across surfaces: help router row and tour stage, global contract workflow entry, reference tables, README | `context/commands/atomic-help.md`, `context/CLAUDE.md`, `docs/reference/{commands,agents}.md`, `README.md` | atomic-implementer (mode: feature) | ~5 | the help-router verification loop in root `CLAUDE.md` prints zero `MISSING:` lines |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Fan-out over a large repo produces a report too long to read | high | Group by tier and category with counts in the header, so a tier can be accepted without reading each line |
| Dead-code findings delete a symbol an external consumer imports | med | Exported and public symbols are `report-only` by tier assignment and never auto-fixed |
| `guarded` fixes regress behavior in a repo with weak tests | med | Green baseline required, suite re-run per batch, run stops on first regression with prior batches committed |
| Findings go stale between audit and apply | med | `REVIEW.md` records the audit SHA; apply refuses when named findings' files have moved |
| The agent invents style rules the repo never adopted | med | Every category binds to a named atomic rule; a finding with no such rule is not emitted |
| The two phases blur into one auto-fixing verb over time | low | `apply` is a separate invocation with its own gates; Phase 1 writes only inside the scratchpad |


## Change log

<!-- Populated on first amendment after the spec is approved. Do not log drafting/refinement turns. -->

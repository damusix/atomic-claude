# Artifacts: branching flows as Mermaid, no guards on atomic's own pieces


## Goal


Every section under `context/` whose numbered or bulleted steps encode a decision tree is drawn as a Mermaid flowchart or state diagram that replaces those steps, and no artifact guards on atomic's own binary, skills, commands, or agents being absent.


## Non-goals


- `context/output-styles/` and `context/rules/` are not touched.
- No renamed command, changed verb, new artifact, or new `atomic` subcommand.
- No Go code change. No change to `atomic validate` or doctor rules.
- Straight sequences with no branch, no loop, and no early stop stay numbered lists.
- Checks on third-party tools stay: `gh` present, authed, or new enough; `git` state; `bun`; Claude Code tool availability (`CronCreate`, Routines, `AskUserQuestion`).
- Checks on data state stay: an index DB file present or absent, a report dir or signals file that may not exist, `atomic signals stale` exit 2, a concurrent-run guard on an existing scratchpad.
- Runtime failures of `atomic signals scan`, `atomic docs scan`, and `atomic where` keep their handling: a non-zero exit is still reported or routed as the artifact already says. The "if `atomic template`/`atomic prompt` fails, stop" checks are removed with the presence guards.
- No eval run: the user already compared converted artifacts against the prose versions.
- `docs/design/**` and `docs/research/**` are history and are not rewritten.


## Success criteria


- [ ] Every branching section in `context/commands/`, `context/agents/`, `context/_partials/`, and `context/skills/**` is a Mermaid block followed by keyed text, except a section with a single two-way condition, which stays one sentence of prose; the numbered steps a diagram replaced are gone.
- [ ] Keyed text under a diagram starts each entry with a node's label in bold and carries only what the label cannot: exact commands, flags, error strings, output lines, and rules. No entry restates a decision the diagram already draws.
- [ ] Every Mermaid block passes `node tmp/mermaid-check.mjs` (real Mermaid parse in headless Chromium plus the layout lint): `flowchart TD` (or `LR` only for a straight chain of at most 5 nodes), at most 9 nodes or states, at most 12 edges, at most 2 back edges, labels quoted when they hold punctuation and at most 24 characters per `<br/>` line and 3 lines, no `style`/`classDef`/`linkStyle`/`fill:`/`%%{init`/`click`, and no bare `end` as an ID or label. `tmp/mermaid-check.mjs` is the task-local checker in this worktree.
- [ ] A one-sentence caption above each block states the claim the diagram makes.
- [ ] `grep -rnE 'command -v atomic|atomic[^.]{0,30}(absent|not on PATH|unavailable|is missing)|binary (is )?(absent|missing)|degrade[^.]{0,40}(binary|atomic)' context/commands context/agents context/_partials context/skills` returns only lines a non-goal above keeps, each listed in `$SCRATCH/STATE.md` with its reason.
- [ ] Every partial still expands: `make -C atomic bundle` succeeds, and every `{{ template "<name>" . }}` directive and `{{define}}`/`{{- end}}` pair survives unchanged.
- [ ] `atomic-writing`'s diagram floor tells prompt artifacts to draw branching flows as Mermaid, to keep a single two-way condition as one sentence, and to keep straight sequences as numbered lists; `references/mermaid.md` states that it applies to artifact sources under `context/` as well as `docs/` files.
- [ ] Each spec under `docs/spec/` whose body describes an artifact guard this change removed has its body corrected and a `## Change log` entry: `signals-refresh-timing.md`, `comment-counter.md`, `wiki-buckets.md`, `cron-workflow.md`, `serve-plans-page.md`, `repo-init.md`, plus any other a grep finds.
- [ ] Every user-facing doc page under `docs/reference/` or `docs/guides/` that describes a removed guard says what the artifact does now.
- [ ] `go test ./...` from `atomic/`, `atomic validate` (bare, `spec`, `config`, `artifacts`), the `/atomic-help` coverage loop, and `npm run docs:build` all pass.


## Approach


Diagram replaces the steps, keyed text under it carries what a label cannot — see `docs/design/artifacts-mermaid-flows.md`.


## Change tree


```
context/
├── _partials/
│   ├── push-flow.md ............... M  (push decision flowchart)
│   ├── pr-flow.md ................. M  (PR preflight + create flowchart)
│   ├── merge-flow.md .............. M  (preflight + remote/local path flowcharts)
│   ├── squash-flow.md ............. M  (preflight flowchart)
│   ├── commit-flow.md ............. M  (only if a step branches)
│   ├── signals-gate.md ............ M  (docs-only + stale exit-code flowchart, no atomic guard)
│   ├── review-gate.md ............. M  (skip guards + verdict flowchart)
│   ├── doc-impact.md .............. M  (flowchart, no atomic-absent fallback)
│   ├── worktree-setup.md .......... M  (isolation/create flowchart)
│   ├── worktree-cleanup-prompt.md . M  (no atomic-absent branch)
│   ├── implement-loop.md .......... M  (iteration state diagram, stuck check, no atomic guards)
│   ├── loop-finalize.md ........... M  (finalize steps, signals exit-code flowchart, no atomic guard)
│   ├── handoff.md, base-resolution.md, git-safety.md . M if they branch
│   └── agent-*.md ................. M  (agent-where, agent-comment-discipline,
│                                         agent-implementer-workflow, agent-signals-output:
│                                         no atomic-absent branches; flows where they branch)
├── commands/*.md .................. M  (every command with a branching section)
├── agents/*.md .................... M  (every agent with a branching section or atomic guard)
└── skills/
    ├── atomic-verify/SKILL.md ..... M  (decision order flowchart, no atomic guard)
    ├── atomic-tdd/SKILL.md ........ M  (decision order flowchart)
    ├── atomic-wiki/SKILL.md ....... M  (no atomic-absent route)
    ├── atomic-wiki/references/*.md  M  (branching pipeline steps)
    ├── atomic-writing/SKILL.md .... M  (diagram floor rule for prompt artifacts)
    ├── atomic-writing/references/mermaid.md . M  (scope covers context/)
    └── <other skills>/SKILL.md .... M  (where a section branches or guards)
docs/spec/
├── artifacts-mermaid-flows.md ..... A
├── signals-refresh-timing.md ...... M  (no atomic guard step; change log)
├── comment-counter.md ............. M  (no skip-when-absent; change log)
├── wiki-buckets.md ................ M  (no binary-absent degradation; change log)
├── cron-workflow.md ............... M  (artifact binary-absent branch removed; change log)
├── serve-plans-page.md ............ M  (artifact binary-absent fallback removed; change log)
└── repo-init.md ................... M  (artifact command -v guard removed; change log)
docs/design/artifacts-mermaid-flows.md . A
docs/reference/, docs/guides/ ...... M  (only pages describing a removed guard)
```


## Outline


```
context/<artifact>.md (each converted section)
  caption — one sentence stating the diagram's claim
  mermaid block — flowchart TD or stateDiagram-v2
  keyed text — **<node label>**: exact command, flag, error string, or rule

context/_partials/implement-loop.md
  Iterate — stateDiagram-v2: Implement, Review, Triage, Commit, Stuck check
  Stuck check — the two-strike rule and the ask/auto branch

context/_partials/signals-gate.md
  signals-refresh — flowchart: staged set empty?, all docs?, stale exit 0/1/2, dispatch, stage

context/_partials/merge-flow.md
  merge-preflight — flowchart: on base?, dirty?, PR open?, gh usable?
  merge-steps — remote path and local path, test failure branch, follow-ups

context/skills/atomic-writing/SKILL.md
  diagram floor — prompt artifacts: Mermaid for branching flows, lists for straight sequences,
                  one sentence for a single two-way condition

context/skills/atomic-writing/references/mermaid.md
  intro — applies to context/ artifact sources and docs/ files

docs/spec/<guard-describing specs>
  body — guard removed from the described behavior
  Change log — dated entry with What changed / Why / Superseded
```


## Flows


**Flow: an orchestrator executes a converted ship verb**

1. The `/commit pr` command expands `commit-flow`, `push-flow`, and `pr-flow` into one prompt.
2. The model reads each flowchart from the start node, answers each decision from `git` and `gh` output, and follows the matching edge.
3. At each action node it runs the exact command the keyed text names under that node's label.
4. A stop node prints the error string the keyed text names and ends the verb. A skip node inside a gate (review, doc impact, signals refresh) stays inside that gate. Any other skip node ends its whole ship step (all of `push-flow`, or all of `squash-flow` including its steps), and the path's next ship step runs; when none follows, the verb ends.

**Flow: the signals gate runs after a commit**

1. The gate reads `git diff --cached --name-only`.
2. A non-empty set where every path is documentation ends the gate.
3. Otherwise it runs `atomic signals stale`.
4. Exit 0 ends the gate; exit 2 reports stderr and ends; exit 1 dispatches `atomic-wiki-inferrer` in silent mode and stages its output.


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Writing rule + ship-family partials | `atomic-writing/SKILL.md` diagram floor, `references/mermaid.md` scope; `_partials/` push-flow, pr-flow, merge-flow, squash-flow, commit-flow, signals-gate, review-gate, doc-impact, base-resolution, git-safety, worktree-cleanup-prompt; `commands/commit.md`, `commands/undo-commit.md` | atomic-implementer (mode: feature) | ~15 | mermaid-check, guard grep, `make -C atomic bundle`, `atomic validate config` |
| 2 | Loop family | `_partials/` implement-loop, loop-finalize, worktree-setup, handoff; `commands/` subagent-implementation, implement, autopilot, quick-fix, subagent-diagnose | atomic-implementer (mode: feature) | ~9 | same |
| 3 | Skills | every `context/skills/**` file except atomic-writing's two files from checkpoint 1 | atomic-implementer (mode: feature) | ~12 | same |
| 4 | Remaining commands | every `context/commands/*.md` not covered above | atomic-implementer (mode: feature) | ~18 | same, plus the `/atomic-help` coverage loop |
| 5 | Agents + agent partials | `context/agents/*.md`, `_partials/agent-*.md`, `_partials/report-issue-privacy.md` | atomic-implementer (mode: feature) | ~14 | same |
| 6 | Specs and docs describing removed guards | `docs/spec/` files listed in Success criteria, any `docs/reference/` or `docs/guides/` page a grep finds | atomic-implementer (mode: feature) | ~8 | `atomic validate spec`, `npm run docs:build` |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| A diagram drops a branch or a rule the prose carried | med | Reviewer diffs each converted section against the removed steps; every condition in the old text maps to a decision node or a keyed rule |
| A block parses on the live editor but fails on GitHub's pinned Mermaid | low | Only `flowchart` and `stateDiagram-v2`, no `-beta` types, no markdown strings in labels |
| A partial's `{{define}}` wrapper or a template directive is damaged by an edit | med | `make -C atomic bundle` fails on a missing partial; `atomic validate config` C3 checks expansion |
| A Go test fixture reads an artifact phrase that changes | low | Full `go test ./...` each checkpoint |
| A removed guard was the only thing keeping an install without the binary working | low | Artifacts install only through the binary; the PR body says so |


## Change log

### 2026-10-19 — A single two-way condition stays prose

**What changed:** `atomic-writing`'s diagram floor, Success criteria 1 and 7, and the Outline say a section with a single two-way condition stays one sentence of prose; a diagram starts at two or more decisions, a loop, or a multi-way route. The `commands/git-cleanup.md` Step 2 scope, the `atomic-wiki/SKILL.md` routing section, and the `atomic-wiki/references/realm.md` wiki-output mode are prose under that rule.

**Why:** The final audit found the threshold was applied to some sections and not others because the shipped rule did not state it.

**Superseded:** every step that branched or stopped early was drawn as Mermaid.

### 2026-10-19 — Template and prompt failure stops are removed

**What changed:** **Correction:** the runtime-failure non-goal names only `atomic signals scan`, `atomic docs scan`, and `atomic where`, and states that the `atomic template`/`atomic prompt` failure stops are removed. The entry below said only presence checks on the binary are removed; that is wrong for these stops.

**Why:** The final audit found the non-goal contradicted what shipped: the checkpoints removed those stops at `_partials/implement-loop.md`, `commands/atomic-plan.md`, `commands/session-report.md`, and `commands/subagent-diagnose.md`, and `docs/spec/document-templates.md` records that removal. Their only remedy was "install/update the atomic binary", which makes them presence guards.

**Superseded:** the non-goal kept runtime-failure handling for every installed `atomic` verb.

### 2026-10-19 — Runtime failures of an installed verb stay handled

**What changed:** Non-goals and the signals-gate flow are worded as current behavior only. Review across checkpoints settled that a non-zero exit from an installed `atomic` verb (`atomic signals scan`, `atomic docs scan`, `atomic where`) keeps its existing error handling; only presence checks on the binary are removed.

**Why:** Reviewers found two places where removing a presence guard also removed the runtime-failure branch beside it.

**Superseded:** Flow step 3 described the absence of a presence check rather than the behavior.

### 2026-10-20 — Skip nodes end a ship step, stop nodes end the verb

**What changed:** Flow step 4 distinguishes a stop node, which ends the verb, from a skip node, which ends its ship step (all of `push-flow` or all of `squash-flow`) so the path's next ship step runs. A skip inside a gate stays inside that gate. `push-flow`'s up-to-date branch and `squash-flow`'s single-commit branch are skip nodes, and `push-flow` draws a branch behind or diverged from its remote branch as a stop. `push-flow` and `pr-flow` measure and push the remote branch of the same name: `@{u}` when it names `<branch>`, else `origin/<branch>`, always with `git push -u <remote> <branch>`.

**Why:** `/commit pr` on an already-pushed branch ended in `push-flow` and never opened the PR, and `/commit squash merge` on a single-commit branch ended in `squash-flow` and never merged. Telling behind from up to date needs a two-sided count, and a branch created from `origin/<base>` has `@{u}` = `origin/<base>`, which bare `git push` refuses under `push.default=simple`. `@{u}` is kept when it names `<branch>` so a named fork remote keeps its upstream; an upstream stored as a URL (`gh pr checkout` without a named remote) falls back to `origin`, as it did before.

**Superseded:** every stop node ended the flow, which ended the whole verb; `push-flow` read `git log @{u}..HEAD` and ran bare `git push` whenever an upstream existed.


## Implementation log

### built — 2026-10-19

Built across 6 checkpoints of /autopilot (two iterations each, checkpoints 2-5 in parallel on disjoint files) plus one audit-fix iteration. Commits (chronological):

- `c726f280` — CP-1 writing rule and ship-family partials, /commit, /undo-commit
- `79b04f59` — CP-5 agents and agent partials
- `60b51134` — CP-2 loop partials, /subagent-implementation, /subagent-diagnose
- `93c0b87a` — CP-3 skills
- `66d824a2` — CP-6 specs and reference pages describing removed guards
- `2895f2a1` — CP-4 remaining commands
- `325ad398` — follow-up deferral
- `d83dcaad` — audit fixes

**Out-of-scope work performed during this build:**

- `docs/spec/atomic-binary.md`, `signals-workflow.md`, `atomic-where.md`, `retro-extract.md`, `verify-gate-validate.md`, `code-intel-integration.md`, `documentation-as-maintenance.md`, `document-templates.md`, and `docs/guides/getting-started.md` also described removed guards; corrected with change-log entries.

**Unforeseens — surprises that emerged during implementation:**

- Removing a presence guard twice took the runtime-failure branch beside it (`atomic signals scan`, `atomic docs scan`); both restored.
- `atomic-wiki-inferrer` and `realm.md` had drifted on the one-argument wiki-output case; `realm.md` now owns the refusal.

**Deferred items still open:**

- `.claude/project/followups/commit-escalation-early-stop.md`
- `.claude/project/followups/realm-noarg-inference-path.md`

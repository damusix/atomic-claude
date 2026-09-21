# Diagram legibility and carry-over


## Goal


`references/mermaid.md` states a layout rule per illegibility cause, each with a limit and a way to count it, and the wiki pipeline carries every current-architecture diagram from a domain's design docs into the domain page, redrawn against source and checked by the reviewer.


## Non-goals


- No Go code and no `atomic validate` rule for Mermaid blocks.
- No new trigger for when a surface gets a diagram; the floors in `atomic-writing` rule 1 stay as they are.
- No purpose or vocabulary role for design docs in wiki pages.
- No edits to existing diagrams in `docs/` other than the ones this spec's own design doc carries.


## Success criteria


- [ ] `context/skills/atomic-writing/references/mermaid.md` has a layout section with a table whose rows cover direction, side-by-side count, label width, edge count, subgraph count, sequence, ER/class, and color, each row giving a numeric limit and a check, followed by the split rule: over any limit, one overview diagram with a node per stage, then one diagram per stage under its own `###` heading.
- [ ] The default direction is stated as `TD`, with the one condition under which `LR` is allowed.
- [ ] The color row bans `style`, `classDef`, `linkStyle`, `fill:`, and `%%{init` lines and gives the `grep` that proves their absence.
- [ ] `journey`, `mindmap`, `timeline`, `gantt`, `pie`, `quadrantChart`, `sankey`, and `gitGraph` are listed as not used, with the dark-mode reason and a replacement for each of the first three.
- [ ] `context/skills/atomic-writing/SKILL.md` rule 1 and its checklist point at the layout table; the frontmatter description and the `## Reference files` entry for `mermaid.md` name the layout limits.
- [ ] `context/skills/atomic-wiki/references/repo.md` Step 3 assigns design docs to domains, Step 4's dispatch prompt carries a `<design_docs>` block and a carry-over instruction, and Step 5's reviewer prompt carries the same list plus a carry-over check and a layout check.
- [ ] `context/skills/atomic-wiki/references/realm.md` W4 restates the instruction, naming a stale block as a `## Constraints` line because realm mode has no concerns channel, and W5 restates the two checks.
- [ ] `context/agents/atomic-wiki-writer.md` has a workflow step for carrying design diagrams, its description names the responsibility, and the step names the `<design_docs>` block the dispatch supplies.
- [ ] `docs/reference/agents.md` and `docs/reference/skills.md` rows for the two artifacts reflect the change in one clause each; `docs/reference/repo-wiki.md` names design docs as a writer input.
- [ ] `docs/design/doc-consolidation.md` records, in a change-log entry, that the diagram row of its wiki-inference section shipped through `docs/design/diagram-legibility.md`.
- [ ] The two diagrams in `docs/design/diagram-legibility.md` pass the layout table.
- [ ] `make -C atomic bundle` succeeds, `atomic validate config` and `atomic validate spec` pass, and the `/atomic-help` coverage loop prints no `MISSING:` line.


## Approach


A layout table in the existing Mermaid reference, and a design-docs block in the existing writer dispatch with a matching writer step and reviewer check; see `docs/design/diagram-legibility.md`.


## Change tree

```
context/
├── skills/
│   ├── atomic-writing/
│   │   ├── SKILL.md ......................... M  (rule 1 pointer, checklist line, description, reference entry)
│   │   └── references/mermaid.md ............ M  (layout section, color and type rules)
│   └── atomic-wiki/references/
│       ├── repo.md .......................... M  (Step 3 design-doc assignment, Step 4 block + instruction, Step 5 checks)
│       └── realm.md ......................... M  (W4 instruction, W5 checks)
└── agents/atomic-wiki-writer.md ............. M  (description, carry-over step)
docs/
├── design/
│   ├── diagram-legibility.md ................ A  (this change's design)
│   └── doc-consolidation.md ................. M  (change-log entry)
├── reference/
│   ├── agents.md ............................ M  (wiki-writer row)
│   ├── skills.md ............................ M  (atomic-writing row)
│   └── repo-wiki.md ......................... M  (design docs as writer input)
└── spec/diagram-legibility.md ............... A  (this contract)
```


## Outline

```
context/skills/atomic-writing/references/mermaid.md
  Layout: direction, width, and density — the rule/limit/check table and the split rule
  What breaks rendering — color paragraph rewritten as a ban with the grep check
  Choosing the type — self-painting types moved from "rarely" to "not used", with replacements

context/skills/atomic-writing/SKILL.md
  frontmatter description — names the layout limits
  Core rules, rule 1 — one sentence pointing at the layout table after the budgets
  Quick checklist — one line for direction, side-by-side count, and color lines
  Reference files — mermaid.md entry names layout limits

context/skills/atomic-wiki/references/repo.md
  Step 3 — design docs join a domain by domain: frontmatter or by the paths they describe
  Step 4 — <design_docs> block in the dispatch prompt; carry-over instruction bullet
  Step 5 — Design docs line in the reviewer prompt; carry-over check; layout check

context/skills/atomic-wiki/references/realm.md
  W4 — carry-over instruction bullet in the restated instructions
  W5 — carry-over and layout checks

context/agents/atomic-wiki-writer.md
  description — carry-over responsibility named
  Redraw the design diagrams — new workflow step between Write to the contract and Report facts

docs/reference/agents.md
  atomic-wiki-writer row — carry-over clause

docs/reference/skills.md
  atomic-writing row — layout clause

docs/reference/repo-wiki.md
  The pipeline — one sentence naming design docs as writer input

docs/design/doc-consolidation.md
  Change log — entry recording the shipped diagram row
```


## Flows


**Flow: a design diagram reaches the wiki page**

1. `atomic-wiki-inferrer` at Step 3 assigns each `docs/design/*.md` to a domain, by `domain:` frontmatter when present, else by the paths the doc describes.
2. At Step 4 the inferrer dispatches `atomic-wiki-writer` with `<source_paths>` and a `<design_docs>` block listing that domain's design docs, or `none`.
3. The writer reads each listed design doc and, for each Mermaid block, decides whether it draws current architecture; decision diagrams stay in the design.
4. For a current-architecture block the writer resolves every node label against the source paths (`atomic code search <label>` when the index exists, grep otherwise), redraws it in `## How it works` with real identifiers, a claim caption, and the layout table applied.
5. A block whose nodes do not resolve is dropped and named where the dispatch says: in repo scope a concerns row `docs/design/<file>.md:<line> — diagram no longer matches source (severity: risk)`, in realm scope one `## Constraints` line stating what breaks.
6. At Step 5 `atomic-reviewer` receives the same design-doc list and the writer's concerns, and verifies that every current-architecture block is on the page or named as stale, and that every block on the page passes the layout table.

**Flow: an agent writes a diagram into a docs file**

1. The agent, under `atomic-writing` rule 1, reads `references/mermaid.md` before the block.
2. It picks the type from the reader's question, then applies the layout table: `TD` unless a straight chain, at most 4 nodes side by side, labels at most 24 characters per line, no color lines, none of the self-painting types.
3. Over any limit it splits the diagram by abstraction level under its own `###` heading rather than shrinking labels.
4. It runs `awk '/^```mermaid/,/^```$/' <file> | grep -nE 'fill:|^\s*(style|classDef|linkStyle) |%%\{init'` and expects no output, then the parse check the skill body names.


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Layout section, color ban, and type list in the Mermaid reference; skill body pointers | `context/skills/atomic-writing/references/mermaid.md`, `context/skills/atomic-writing/SKILL.md` | atomic-implementer (mode: surgical) | 2 | Success criteria 1-5 and 11 (the design doc's two diagrams checked against the table as written); `grep -c '| Direction' mermaid.md` = 1; `atomic validate config` passes |
| 2 | Design-doc assignment, dispatch block, carry-over instruction, and reviewer checks in both pipeline references | `context/skills/atomic-wiki/references/repo.md`, `context/skills/atomic-wiki/references/realm.md` | atomic-implementer (mode: surgical) | 2 | Success criteria 6-7; `grep -c '<design_docs>' repo.md` ≥ 2 |
| 3 | Writer step and description | `context/agents/atomic-wiki-writer.md` | atomic-implementer (mode: surgical) | 1 | Success criterion 8; `make -C atomic bundle` succeeds |
| 4 | Reference rows; repo-wiki sentence; doc-consolidation change-log entry | `docs/reference/agents.md`, `docs/reference/skills.md`, `docs/reference/repo-wiki.md`, `docs/design/doc-consolidation.md` | atomic-implementer (mode: feature) | 4 | Success criteria 9-10 and 12; `atomic validate spec` passes; `/atomic-help` coverage loop prints nothing |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| The numeric limits are wrong for some real diagram and get worked around by splitting into fragments that lose the claim | med | The limits are stated as first calibration against the five reported cases; the split rule requires an overview diagram, so the claim survives at the higher level; a case that passes and still reads badly reopens the numbers |
| The writer copies a design diagram without resolving nodes, shipping a stale picture with the wiki's authority | med | The reviewer check requires node resolution and names the concerns row a stale block must produce; the writer step says "redrawn", never "copied" |
| `realm.md` and `repo.md` drift apart again because the instruction is restated in both | low | Both edits land in one checkpoint and the reviewer diffs them side by side |
| A `{{ template }}` edit in `atomic-wiki-writer.md` breaks bundle expansion | low | `make -C atomic bundle` runs at checkpoint 3 and fails loudly on a bad directive |


## Change log

### 2026-09-20 — Realm stale route and block-scoped color check

**What changed:** Success criterion 7 now states that `realm.md` W4 names a stale block as a `## Constraints` line rather than a concerns row; Flow 1 steps 5-6 route the stale-block report by scope (a concerns row in repo scope, a `## Constraints` line in realm scope) and add that the reviewer also receives the writer's concerns; Flow 2 step 4 scopes the color grep to fenced Mermaid blocks.

**Why:** post-implementation audit found realm mode has no concerns channel, and the file-scoped grep failed on prose mentioning `fill:`.

**Superseded:** the concerns-block route for every scope, and the file-scoped grep.


## Implementation log


### shipped — 2026-09-20

Built across 9 iterations of /autopilot (spec loop: 2 reviewer passes). Commits (chronological), squashed to one before the PR:

- `23c3b0a3` — plan: design and spec
- `e9c9aa6c` — CP-1 layout table, type ban, color ban in the Mermaid reference; skill pointers
- `3acf89b0` — CP-2 design-doc assignment, `<design_docs>` dispatch block, writer instruction, reviewer checks in both pipeline references
- `caabbeb2` — CP-3 wiki-writer step and description
- `f716c49b` — CP-4 reference rows, repo-wiki sentence, doc-consolidation change-log entry
- `f7b2b8b7` — post-audit fix: realm stale route, block-scoped color check, "redraws" wording, provenance via `%% source:`

**Out-of-scope work performed during this build:**

- One edge label and its caption in `docs/design/doc-consolidation.md` relabeled so its body stays current; the spec non-goal on existing diagrams was written for legibility backfill, and spec-currency on a design body outranks it.

**Unforeseens — surprises that emerged during implementation:**

- Realm mode has no concerns channel (W4 omits it, W7 prints none), so the stale-diagram route is a `## Constraints` line there; logged in `## Change log`.
- The file-scoped color grep failed on prose that mentions `fill:`; the check is now scoped to fenced Mermaid blocks.
- The Step 5 reviewer could not see writer concerns from its declared inputs; the prompt gained a `Writer concerns:` field.

**Deferred items still open:**

- none

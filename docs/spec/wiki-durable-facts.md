# Wiki pages do not count things in the repo


## Goal


A wiki page contains no count of things in the repo and no measurement of the tree. It records structure, responsibilities, contracts, constraints, coupling, and citations to real paths. The ban is stated once in the `atomic-writing` skill as a flat prohibition with no test to apply, carried in one line by every surface that writes a wiki page, and checked mechanically by the reviewer in both pipelines: a count is `CHANGES_REQUESTED`, with a version or a documented contract value as the only carve-out.


## Non-goals


- No change to `docs/wiki/scan.md`, which is deterministic output from `atomic signals scan`; statistics are its job.
- No change to the `<scan-sha>` stamp in `docs/wiki/index.md`.
- No Go code, no new CLI verb, no doctor or validate check. A lint would have to separate `500 files` from `19 verbs` by token shape, which it cannot do; the ban is enforced at review, not at build.
- No ban on page-contract counts in artifacts. The page contract inside `context/skills/atomic-wiki/references/*.md` states section counts inside prohibitions ("five sections", "no sixth section"); those are artifacts, not wiki pages, and stay. A count elsewhere in an artifact is not protected by this non-goal and may be cut where it reads better without one.
- No new artifact, no rename, no removal, so no `/atomic-help` topic row changes; the coverage loop is run as a check, not edited.
- No sweep of `docs/reference/`, `docs/guides/`, or `README.md` prose beyond the one surface that describes what a repo wiki contains.


## Approach


State the prohibition in `context/skills/atomic-writing/SKILL.md`, which both pipeline references and `atomic-wiki-writer` already load, and repeat one line of it at each point that writes or gates a page. See `docs/design/wiki-durable-facts.md`.


## The rule as written


Rule 17 of `context/skills/atomic-writing/SKILL.md` is the canonical text and the only full statement; every other surface states the ban in one line and cites it. A number survives only as an identifier naming one numbered thing (`Check 9`, `Zone 1`, `Shape 2`, `exit 2`, `R1-R8`) or as a version or documented contract value (`Go 1.25`, `30 days`, `5 iterations`, `3 to 6 lenses`).


## Success criteria


- [ ] `context/skills/atomic-writing/SKILL.md` states the prohibition and its version-and-contract-value boundary as one core rule, in fewer lines than the test it replaces, plus one line in `## Quick checklist before saving`. No test for a writer to apply.
- [ ] `context/skills/atomic-wiki/references/repo.md` drops the `## Language breakdown` block from the router template, replaces `R5` with the no-counts rule, and removes `language mix` from `R1`'s list of what the router carries.
- [ ] `repo.md` Step 4's writer brief states the ban in one line; `repo.md` Step 5's reviewer checklist makes it mechanical — a count of repo things anywhere on the page, including diagram labels and table cells, is `CHANGES_REQUESTED`, with a version or documented contract value as the only carve-out. The reviewer is never asked to judge whether a number is perishable.
- [ ] `context/skills/atomic-wiki/references/realm.md` carries the same one-line ban in the W4 brief and the same mechanical check in W5's reviewer list.
- [ ] `context/agents/atomic-wiki-writer.md` states the ban in its page-contract workflow; `context/agents/atomic-wiki-inferrer.md` states it as a constraint on the router it assembles, since it writes `docs/wiki/index.md` itself without dispatching a writer.
- [ ] `docs/wiki/CLAUDE.md` states the ban as steering, and carries no count itself.
- [ ] `docs/wiki/index.md` has no `## Language breakdown` section and no other count of repo contents. Its `<scan-sha>`, `Go 1.25`, `go 1.23.4`, and the pinned Bun version are untouched.
- [ ] No page under `docs/wiki/` other than `scan.md` contains a count of things in the repo, including in Mermaid labels, ASCII trees, table cells, and frontmatter descriptions. Identifiers (`Check 9`, `Zone 1`, `exit 2`, `R1-R8`) and documented contract values (`500` files, `64KiB`, `16 MiB`, `100` entries, `30s`, `2s`, `12` lines, `3 to 6` lenses, `30` days, `120`-word, `24`-character, `5`-node) are kept.
- [ ] `docs/spec/signals-router.md`'s router-shape table no longer lists `## Language breakdown`, and its `## Change log` carries a dated entry with a `Superseded:` line.
- [ ] `docs/reference/repo-wiki.md` states, in one place, that the router and domain pages carry facts while `scan.md` carries the counts.
- [ ] The help-coverage loop in the root `CLAUDE.md` prints no `MISSING:` line.
- [ ] `make -C atomic bundle` succeeds and the bundled copies of every edited artifact match the sources.
- [ ] `grep -rn 'Language breakdown' context/ docs/ CLAUDE.md README.md`, excluding `## Change log` entries and this change's own spec and design docs, returns only `docs/wiki/scan.md` matches, if any.
- [ ] `npm run docs:build` succeeds.


## Change tree


```
context/
├── skills/atomic-writing/SKILL.md                    M  core rule + checklist line
├── skills/atomic-wiki/references/repo.md             M  router template, R1, R5, Step 4, Step 5
├── skills/atomic-wiki/references/realm.md            M  W4 brief, W5 reviewer
├── agents/atomic-wiki-writer.md                      M  page-contract step
└── agents/atomic-wiki-inferrer.md                    M  constraint on router assembly
docs/
├── wiki/CLAUDE.md                                    M  steering line; drop the domain count
├── wiki/index.md                                     M  drop Language breakdown; drop counts
├── wiki/bundle.md                                    M  drop counts
├── wiki/bus.md                                       M  drop counts
├── wiki/code-intel.md                                M  drop counts
├── wiki/config.md                                    M  drop counts
├── wiki/docs-meta.md                                 M  drop counts
├── wiki/doctor.md                                    M  drop counts
├── wiki/repl.md                                      M  drop counts
├── wiki/serve.md                                     M  drop counts
├── wiki/signals.md                                   M  drop counts
├── wiki/wiki.md                                      M  drop counts
├── wiki/workflow.md                                  M  drop counts; drop the "no longer" history
├── reference/repo-wiki.md                            M  what the pages carry vs what scan.md carries
├── spec/signals-router.md                            M  router-shape row + change-log entry
├── spec/wiki-durable-facts.md                        A  this file
└── design/wiki-durable-facts.md                      A
```


## Outline


```
context/skills/atomic-writing/SKILL.md
  Core rules
    17. A wiki page never counts things in the repo — the ban, then the version-and-contract-value boundary
  Quick checklist before saving
    no-counts line

context/skills/atomic-wiki/references/repo.md
  Step 4 — writer brief instruction stating the ban
  Step 5 — mechanical reviewer check: a count is CHANGES_REQUESTED
  Router shape — Zone 1 lead and template, both without counts
  Router discipline
    R1 — router contents without "language mix"
    R5 — no counts, replacing the language-breakdown rule

context/skills/atomic-wiki/references/realm.md
  W4 — instruction stating the ban
  W5 — reviewer check

context/agents/atomic-wiki-writer.md
  2. Write to the contract — the ban as a third rule under the section

context/agents/atomic-wiki-inferrer.md
  Rules — constraint binding the router the inferrer assembles

docs/wiki/CLAUDE.md
  Writing a page here — the ban as steering; page-shape wording without its section count
  What lives here — domain list without its count

docs/wiki/index.md
  Domains — bus row without the verb count
  DevOps & CI — workflow list without its count
  Language breakdown — removed

docs/wiki/<domain>.md
  per page — counts replaced by the enumeration or the named thing, documented limits kept

docs/reference/repo-wiki.md
  opening file list — what index.md and the domain pages carry, and what scan.md carries

docs/spec/signals-router.md
  Router shape — Zone 1 section table without the Language breakdown row
  Change log — dated entry with a Superseded line
```


## Flows


**Flow: a domain page is written**

1. `atomic-wiki-inferrer` dispatches `atomic-wiki-writer` for a domain, with the Step 4 brief
2. the writer loads `atomic-writing`, reads the source, and writes no count of repo things
3. the writer returns the page; the inferrer dispatches `atomic-reviewer` with the Step 5 checklist
4. the reviewer finds a count and returns `CHANGES_REQUESTED` naming it, with no judgment call to make
5. the writer is re-dispatched with the correction; the loop repeats until `PASS`

**Flow: the router is assembled**

1. `atomic-wiki-inferrer` writes `docs/wiki/index.md` at Step 7 from the router template, with no writer dispatch
2. the template carries no language-breakdown block, and the inferrer's own constraint bans counts from the sections it does write
3. `atomic signals linkify` renders the path citations
4. two branches that each add Go code produce no conflicting hunk in `docs/wiki/index.md`

**Flow: a hand edit under `docs/wiki/`**

1. a session reads any file under `docs/wiki/`, so the harness loads `docs/wiki/CLAUDE.md` as nested memory
2. the steering states the rule, so a correction to a factual error does not reintroduce a count


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | State the ban and wire it into every surface that writes or gates a wiki page: the voice skill, both pipeline references (router template and its Zone 1 lead, `R1`, `R5`, writer brief, mechanical reviewer check), both wiki agents, and the `docs/wiki/` steering file. | `context/skills/atomic-writing/SKILL.md`, `context/skills/atomic-wiki/references/repo.md`, `context/skills/atomic-wiki/references/realm.md`, `context/agents/atomic-wiki-writer.md`, `context/agents/atomic-wiki-inferrer.md`, `docs/wiki/CLAUDE.md` | atomic-implementer (mode: feature) | 6 | `grep -n 'Language breakdown' context/skills/atomic-wiki/references/repo.md` is empty; `grep -n 'commands, counts' context/skills/atomic-wiki/references/repo.md` is empty; `grep -n 'R5' …` shows the no-counts rule; `repo.md` Step 5 and `realm.md` W5 each make a count `CHANGES_REQUESTED` without asking the reviewer to judge; `make -C atomic bundle` succeeds |
| 2 | Sweep the live wiki: remove `## Language breakdown` and every count of repo things from `docs/wiki/index.md`, the steering file, and the domain pages, in prose, diagram labels, ASCII trees, table cells, and frontmatter. Keep identifiers and documented contract values. | `docs/wiki/*.md` except `scan.md` | atomic-implementer (mode: feature), sharded across pages | 13 | `grep -n 'Language breakdown' docs/wiki/index.md` is empty; `grep -nE '[0-9]+ ?%' docs/wiki/index.md` is empty; `tmp/number_sweep.py` reports only identifiers and contract values; the kept set still greps: `500 files` in `code-intel.md`, `64KiB` in `config.md`, `16 MiB` in `workflow.md`, `100 entries` in `serve.md`, `3 to 6 lenses` in `workflow.md`, `12 lines` in `signals.md`; `git diff --stat docs/wiki/` shows no change to `scan.md` |
| 3 | Amend `docs/spec/signals-router.md` under the spec-currency rule, state the split in `docs/reference/repo-wiki.md`, and run the repo-wide verification sweeps. | `docs/spec/signals-router.md`, `docs/reference/repo-wiki.md` | atomic-implementer (mode: surgical) | 2 | `grep -n 'Language breakdown' docs/spec/signals-router.md` is empty; the spec's `## Change log` holds a dated entry with a `Superseded:` line; the help-coverage loop from the root `CLAUDE.md` prints no `MISSING:` line; `npm run docs:build` succeeds |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| The sweep eats a documented contract value (`500` files, `64 KiB`, `2s` grace) or an identifier (`Check 9`, `Zone 1`) along with the counts | high | the rule names both carve-outs before the sweep starts; checkpoint 2's verify greps each kept value back out by name |
| A count is cut and the sentence loses the fact it carried (`step 6` of a 7-step list becomes unanchored) | med | replace the ordinal with the step's name rather than deleting it; the reviewer reads the sentence, not the diff |
| A cut numeral is replaced by a filler quantifier ("several", "some") or a bare cataphoric pronoun, trading a rotting fact for no fact | high | named as a defect in the sweep brief; the reviewer and the auditor both check the rewritten sentence, not just the absence of the number |
| The next refresh regenerates the language table because a model re-derives "be specific, quantify" from voice rule 12 | med | rule 17 is a prohibition in the same list, with nothing to weigh against rule 12, and it is a mechanical reviewer check in both pipelines — which is what issues #269 and #271 showed a writer-only rule does not survive |
| The ban is read as binding artifacts too, and a page-shape prohibition in `context/` loses the count that makes it enforceable | med | rule 17 and every restatement scope it to a wiki page; the spec's non-goals say so explicitly |
| `docs/wiki/index.md` still conflicts across branches on its `<scan-sha>` line | high | out of scope by decision; a one-line stamp conflict is mechanical where a fifteen-row table is not |
| A parallel branch refreshes the wiki from the old pipeline and restores the table | med | the contract change and the live sweep land in one commit, so a refresh after merge reads the new template |


## Change log

### 2026-09-21 — The rule is a prohibition, not a test

**What changed:** Rule 17 states a flat ban: a wiki page never counts things in the repo. There is no test for a writer to run and no judgment for a reviewer to make; the reviewer check is mechanical, and a count is `CHANGES_REQUESTED` with a version or documented contract value as the only carve-out. The rule is shorter than the version it replaces. The sweep is correspondingly stricter: a count goes wherever it sits, including where a list follows it and where a test pins it.

**Why:** A test the writer runs against every number is permissive by construction, since any number can be argued into the keep case, and it costs tokens on each one. The first implementation demonstrated it: three passes missed eight counts, and the implementer and the auditor disagreed about a ninth.

**Superseded:** The rule was "would this number go stale from a commit that does not change the fact it illustrates? Yes, cut it. No, keep it", with "an enumerated list carries its own count" as a sub-clause, and a reviewer check that asked whether a number was perishable.

### 2026-09-20 — Initial

**What changed:** First version.

**Why:** Three parallel PRs conflicted on `docs/wiki/index.md` and had to be hand-resolved, and the conflicting content was a statistics table nothing reads.

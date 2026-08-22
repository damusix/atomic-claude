# Spec: comment discipline across artifacts


Implementation contract for GitHub issue #112. Design: `docs/design/comment-discipline.md` (placement rationale, rejected alternatives, severity model).


## Goal


Builder, surgeon, and reviewer enforce one shared code-comment discipline; the global contract (`CLAUDE.md`) and the conversational review skill carry the same rule.


## Non-goals


- Prose style of Claude's replies (output style owns it; `output-styles/atomic.md` is not touched).
- Documentation surfaces (`atomic-writing` / `atomic-documentation` own those; no `docs/reference/` edits in this change).
- `rules/typescript/style.md:15` ("a comment explaining the next block means extract a function") stays as-is — structure rule, not a comment rule.
- No new path-scoped rule (rejected in the design — would duplicate the CLAUDE.md principle on every file touch).


## Success criteria


1. `templates/shared/agent-comment-discipline.md` exists, defines the partial `agent-comment-discipline`, and carries the four rules from the design's "Rules" section in declarative voice (readable both as instructions to an implementer and as a contract a reviewer gates against). Each rule carries a WHY, matching the house style of `agent-shared-rules.md`.
2. `templates/agents/atomic-implementer.md` composes the partial via `{{ template "agent-comment-discipline" . }}` (pasting the rule text inline does NOT satisfy this — the single-partial mechanism is the point); the rendered `agents/atomic-implementer.md` contains the partial's rules. The old "Comments only when WHY is non-obvious" bullet is gone from `templates/shared/agent-shared-rules.md` and from rendered output. No other bullet of `agent-shared-rules` changes.
3. `templates/agents/atomic-reviewer.md` composes the partial via `{{ template "agent-comment-discipline" . }}`; the rendered `agents/atomic-reviewer.md` contains: the partial's rules, a "Comment-discipline findings" section (severity model: 🟡 noise and reviewer-addressed, 🔴 misleading, verdict-driving via the shared `agent-readability` partial; explicit not-a-finding list, "judgment call, not a regex lint" framing, findings placed in Code quality), and a mention of the comment-discipline rule in code-mode workflow step 6 alongside the suppression-pattern and over-engineering rules.
4. `CLAUDE.md` Principles block gains one compact comment-discipline bullet (distills the four rules; includes a **Why:**). `skills/atomic-writing/SKILL.md:137`'s pointer to "global comment rules in `CLAUDE.md`" now resolves to real content.
5. `skills/atomic-review/SKILL.md` gains a "Comment noise" section inside `<output_format>`, parallel in shape to its "Over-engineering" section (short prose + 2–3 example one-liner findings using the severity emojis).
6. `make render` and `make -C atomic bundle` both run clean; rendered `agents/` and the embedded bundle are committed with the source changes (CI drift gates pass). Because checkpoints commit individually, **each** checkpoint's commit includes its own regenerated bundle — checkpoint 1 touches `agents/` (a bundle-source path) and therefore also runs `make -C atomic bundle` before its commit.
7. `atomic validate` reports no new findings for the touched artifacts, checked at the end of each checkpoint.


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Partial + agent wiring | `templates/shared/agent-comment-discipline.md` (new), `templates/shared/agent-shared-rules.md`, `templates/agents/atomic-implementer.md`, `templates/agents/atomic-reviewer.md`, rendered `agents/*.md` via `make render`, bundle via `make -C atomic bundle` | Success criteria 1–3, plus 6–7 for this checkpoint's files |
| 2 | Global contract + review skill | `CLAUDE.md`, `skills/atomic-review/SKILL.md`, bundle via `make -C atomic bundle` | Success criteria 4–5, plus 6–7 for this checkpoint's files |


## Verification


- `grep -c 'template "agent-comment-discipline"' templates/agents/atomic-implementer.md templates/agents/atomic-reviewer.md` → exactly 1 each.
- `make render` → `git diff --exit-code` on `commands/` shows no unrelated drift; `agents/atomic-implementer.md` and `agents/atomic-reviewer.md` contain the partial text exactly once each.
- `grep -rn "Comments only when WHY" templates/ agents/` → zero matches after checkpoint 1.
- `make -C atomic bundle` then `git status` → embedded bundle staged with the same commit, at every checkpoint.
- `atomic validate` → no new artifact findings, at every checkpoint.
- Tests: no Go code changes; TDD skipped because: prose-artifact change with no runtime surface. Render/bundle parity and `atomic validate` are the verification gates.


## Risks


| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Partial voice reads as implementer-only, weakening the reviewer's gate | Medium | Write rules declaratively ("Comments state…", "Comment density matches…"), not imperatively second-person |
| CLAUDE.md bullet duplicates the partial verbatim, doubling context cost | Low | CLAUDE.md distills to one bullet; partial carries the full four rules |
| Reviewer over-fires on legitimate comments | Medium | Explicit not-a-finding list + judgment-call framing, same as suppression-pattern section |


## Implementation log


### v1 — 2026-07-03


Built in a worktree (`comment-discipline`, cut from `next`) across 2 checkpoints of the autopilot subagent loop, each builder→reviewer with findings folded in-iteration.

- CP1 (`0089295`) — `agent-comment-discipline` partial (four rules, declarative voice, per-rule WHY) composed into implementer and reviewer templates; superseded one-line comment bullet removed from `agent-shared-rules`; reviewer gained the Comment-discipline findings section + workflow step 6 mention. Review round 1: 2🟡 (severity-tier contradiction on reviewer-addressed comments; partial composed mid-list splitting the Rules bullets) — fixed via surgical pass, round 2 PASS.
- CP2 (`6681343`) — CLAUDE.md Principles bullet (distilled, with WHY; resolves the `atomic-prose` dangling pointer) + `atomic-review` skill Comment noise section. Review round 1: 1🔴 (nested backticks broke an example's code span) — fixed, round 2 PASS.

**Out-of-scope work:** none. **Unforeseens:** `atomic validate spec` S5 required the canonical checkpoint-table columns (corrected, see change log); `atomic code index` failed in the fresh worktree (FK constraint) so the run degraded to sg/grep. **Deferred items still open:** none — every reviewer finding was folded in-iteration.


## Change log


- 2026-07-03: initial spec (issue #112, autopilot run). Incorporates spec-mode review round 1: Non-goals section added; criterion 7 attached to both checkpoints; composition-via-partial made explicit in criteria 2–3; bundle regen required per checkpoint; Likelihood column added to Risks.
- 2026-07-03: correction — checkpoint table headers renamed to the canonical `# | Checkpoint | Files/areas | Verifies` set (`atomic validate spec` S5).

### 2026-08-21 — Comment noise is a risk, not a nit; auditor joins

**What changed:** Criterion 3's severity model is now 🟡 for noise and reviewer-addressed comments, 🔴 for a comment that misdescribes the code, and any such finding drives `CHANGES_REQUESTED`. The floor is set by a new shared partial, `agent-readability`, composed into implementer, reviewer, and auditor alongside `agent-comment-discipline`; the auditor's coherence pass now reads comment accumulation across iterations, which no single review can see.

**Why:** at 🔵 the reviewer's comment findings were optional by definition — harvested to `FOLLOWUPS.md`, fixed only under `/autopilot` — so the comments shipped. Code is read far more often than it is written; a comment every reader has to skip is a defect in the file, not a style preference.

**Superseded:** 🔵 noise / 🟡 misleading; reviewer-only enforcement.

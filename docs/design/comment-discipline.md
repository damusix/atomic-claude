# Comment discipline across artifacts


Design for GitHub issue #112. Adds explicit code-comment discipline to the artifacts that produce and gate code, so builder, surgeon, and reviewer all enforce the same rule.


## Problem


Claude's code comments trend toward noise: narrating what the next line does, restating the diff ("changed X to use Y"), or justifying the change to a reviewer. Those comments are stale the moment the PR merges. Meanwhile the comments that matter — invariants, constraints the code can't express, why a non-obvious shape was chosen — are often missing.


Current state (verified 2026-07-03):

- The only shipped rule is one bullet in `templates/shared/agent-shared-rules.md:3` ("Comments only when WHY is non-obvious"), composed **only** into `atomic-implementer`. `agent-shared-rules` has no other consumers.
- `atomic-reviewer` has no comment-discipline review dimension — the implementer follows a rule the reviewer never checks.
- `CLAUDE.md` (bundle source, every user's global contract) carries **no** comment guidance, yet `skills/atomic-prose/SKILL.md:137` points to "the global comment rules in `CLAUDE.md`" — a dangling pointer.
- `skills/atomic-review/SKILL.md` (conversational review surface) flags over-engineering but not comment noise.


## Rules (the discipline itself)


1. A comment states what the code cannot show: a constraint, an invariant, a non-obvious why, a gotcha (units, ordering requirements, external-system quirks).
2. Comments never narrate the next line, restate the diff, or address the reviewer ("as requested", "fixed per review", "this change makes X do Y"). Those are PR-conversation artifacts, not source content — they are stale the moment the PR merges.
3. Comment density and idiom match the surrounding file. Don't over-comment a sparse file or strip an idiomatically documented one.
4. Docstrings on new public APIs follow the language's convention (godoc, JSDoc, PEP 257, rustdoc) — not ad-hoc prose. If the package documents every exported symbol, a new undocumented export violates convention.


## Placement decision


The issue left placement open (partial vs rule vs principle). Decision: **shared partial + CLAUDE.md principle + reviewer finding section + review-skill flag**. Rationale per surface:

| Surface | Change | Why here |
|---------|--------|----------|
| `templates/shared/agent-comment-discipline.md` (new) | Declarative rules 1–4, neutral voice | One source, two consumers: implementer reads it as instructions, reviewer as the contract to gate. Single partial = builder/surgeon/reviewer agreement by construction. |
| `templates/agents/atomic-implementer.md` | Compose the partial in `<constraints>` | Both modes (feature + surgical) render from this one template, so one composition covers builder and surgeon. |
| `templates/shared/agent-shared-rules.md` | Remove the one-line comment bullet | Superseded by the partial; keeping both would drift. Only consumer is the implementer, which now gets the fuller partial. |
| `templates/agents/atomic-reviewer.md` | Compose the partial + new "Comment-discipline findings" section + name the rule in workflow step 6 | Mirrors the existing suppression-pattern and over-engineering sections — same shape, same Code quality placement, judgment-call framing. |
| `CLAUDE.md` Principles | One compact bullet distilling rules 1–4 | Covers the main agent's inline coding path (subagent partials never load there) and fixes `atomic-prose`'s dangling pointer. Distilled, not verbatim — CLAUDE.md loads every session; the partial carries the full text. |
| `skills/atomic-review/SKILL.md` | Short "Comment noise" section parallel to "Over-engineering" | Conversational PR reviews should flag the same violations the loop reviewer gates. Same threading precedent as the YAGNI reflex. |


**Rejected: path-scoped rule.** A comment rule applies to all code, so its glob would be a catch-all — loading on every file touch to duplicate a principle CLAUDE.md already loads every session. Pure duplication, no scoping benefit.


## Reviewer severity model


Judgment call, not a regex lint — same framing as the suppression-pattern rule:

- 🔵 nit: noise comment (narrates the line, restates the diff). Fix: delete it.
- 🟡 risk: comment contradicts or misdescribes the code (future readers trust the wrong one), or a reviewer-addressed comment shipped into source.
- Not a finding: legitimate section comments in an idiomatically commented file, license headers, directive comments (`//go:embed`, `// eslint-disable`), or the WHY comments rule 1 asks for.


## Out of scope


- Prose style of Claude's replies (output style owns it).
- Documentation surfaces (`atomic-prose` / `atomic-documentation` own those).
- `rules/typescript/style.md:15` ("a comment explaining the next block means extract a function") stays — it's a structure rule, not a comment rule, and doesn't conflict.

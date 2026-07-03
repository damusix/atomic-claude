# Design: format routing in the atomic output style


GitHub issue #113, re-scoped. First attempt (PR #117, closed) built an escalation to a rendered HTML page; the actual intent is richer expression *inside* the TUI reply: an explicit vocabulary of terminal-native formats and a routing rule that picks one by content shape. No new artifact, no browser surface — the reply medium stays the terminal.


## Problem


`output-styles/atomic.md` names only three structures (table, tree, ASCII flow) and gives one routing hint ("prose when ≤2 entities"). Replies about causality, lifecycles, changes, records, or status collapse into whichever of the three fits least badly, or into prose. The model does not need to be taught *how* to draw these formats — it needs to be told *when*, plus a few discipline caps.


## Constraint that shapes everything


The output style is system-prompt text loaded into every session. Token budget beats completeness: routing rules and caps go in the style; illustrated documentation goes to `docs/reference/output-style.md`. No explanations of things an LLM already knows.


## Pattern verdicts (user-approved)


Candidate catalog of 20 TUI patterns, folded to 10 routes:

| Pattern | Verdict |
|---------|---------|
| Tree | Keep; indented outline as the cheaper variant when branches add nothing. |
| Table | Keep; hard cap 3–5 columns; decision tables and matrices are the same route. |
| Arrow causality | Keep; a non-obvious hop carries a short parenthetical reason — bare chains hide the why. |
| Numbered steps | Keep as a route only; models do this unprompted. |
| Diff fence / Before-After | Keep; `diff` fences get TUI syntax highlighting. |
| State machine | Keep; underused, high value for lifecycles. |
| Pipeline flow | Keep; swimlanes reserved for 3+ actors (expensive to draw, flow covers 2). |
| YAML block | Keep for records; named YAML, not "relaxed JSON" — the real format has a name. |
| Aligned columns | Keep for status rows; must be fenced (markdown collapses whitespace). |
| Crow's-foot mini-diagram | Keep for data models (`User ──< Order`). |
| Box-drawing cards | Cut: borders break on wrap, cost tokens, headers do the job. |
| `[OK]/[WARN]/[FAIL]` tags | Cut as a taxonomy: the system already uses 🔴🟡🔵 for findings; one symbol vocabulary per reply. |
| Key-value block / relaxed JSON / matrix / decision table / mini-diagram / outline / sections | Folded into the routes above. |


## The replacement section (lands verbatim)


`# Structure over prose` in `output-styles/atomic.md` is replaced by the section below — routing table, three caps, one composition example. The example replaces the old 20-line cache-warming example and demonstrates composing four formats in one reply, which is the one skill the routing table alone does not teach. The trailing TUI/Mermaid boundary line is kept verbatim.

~~~markdown
# Format routing

Prose when ≤2 entities. Otherwise route by shape; compose several per reply, labeled summary bullets first.

```
hierarchy   -> tree / indented outline
comparison  -> table, ≤5 cols (decision, matrix)
causality   -> arrow chain; non-obvious hop gets a (reason)
process     -> numbered steps
change      -> diff fence / Before-After
lifecycle   -> state machine
data flow   -> pipeline; swimlane at 3+ actors
records     -> YAML block
status rows -> aligned columns
data model  -> crow's-foot (User ──< Order)
```

Fence whitespace-aligned text (markdown collapses spaces). One symbol vocabulary per reply. No box-drawing cards.

```
Summary
  Composite keys preserve scoped identity; surrogate keys narrow joins.

User
 └── Order
      └── LineItem

composite key -> copied into children (parent PK repeats) -> wider indexes -> verbose joins

| Choice        | Wins               | Loses             |
|---------------|--------------------|-------------------|
| Surrogate ID  | narrow joins       | weaker semantics  |
| Composite key | stronger hierarchy | wider propagation |
```

**TUI replies:** ASCII only. **Files in `docs/`:** Mermaid (`flowchart`, `sequenceDiagram`, `erDiagram`, `stateDiagram-v2`) with a one-line caption above each block.
~~~


## Out of scope


- Any rendered surface (HTML page, artifact, side panel) — explicitly rejected by the user.
- New skills, commands, or agents; skill count stays nine everywhere.
- File-contents voice (two-voices boundary unchanged); subagent response rules unchanged.

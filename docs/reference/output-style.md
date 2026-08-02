# Output style

The output style is the communication layer. Its goal is clarity, and a paragraph is one instrument, not the only one. When an answer has parts that compare, nest, or sequence, replies reach for a table, an indented tree, or an ASCII flow so the structure carries the meaning instead of a wall of prose. Filler is dropped, fragments are fine, and short synonyms win, but compression serves the structure, it does not lead. Technical terms and code blocks are never altered.

A shorter reply that reads worse is a failure, not a win. When a structure communicates faster than sentences (three components with a hierarchy, a sequence that branches across actors), the style picks the structure. When a paragraph is genuinely the clearest form, it stays a paragraph.

It is also the most optional part of atomic-claude. The skills, commands, agents, and signals all work without it. The output style makes Claude's replies clearer to read.


## Where the behavior actually comes from

| Layer | What it contributes | Always active? |
|-------|-------------------|:---------:|
| `CLAUDE.md` | Principles, testing philosophy, code discipline | ✓ |
| Skills | TDD, verification, debugging, commit messages | ✓ |
| Commands | Workflow orchestration (plan, implement, ship) | When invoked |
| Agents | Specialized workers with their own prompts | When dispatched |
| **Output style** | **Clarity: drop filler, fragments OK, structured output (tables, trees, ASCII flow)** | **When selected** |

The first four layers carry the load. The output style shapes how Claude communicates the result.


## Sentence rules

Beyond the drop-list, six rules shape individual sentences. Each targets a redundancy that survives word-level cutting:

- **Condition before instruction.** "If index stale, re-run indexer" scans in execution order. The trailing-condition form risks the reader acting before reading the guard.
- **Say it once.** Restatement is the filler that word-cutting misses: the same proposition wearing a new sentence.
- **No AI tells.** Stock LLM phrasing ("load-bearing", "here's the thing", "it's worth noting", "at its core") performs insight rather than delivering it, and the contrastive reveal ("not X, it's Y") stages a revelation around a fact that could just be stated. Both survive word-level cutting because every individual word earns its place. The rule targets the construction.
- **Articles guard surprises.** Drop articles only where the noun is predictable. "Rollback deletes the backup" keeps its function words because the content is a warning, and a warning must parse on first read.
- **Code can be the whole reply.** When code fully answers the question, prose around it adds nothing.
- **Keep the user's terms.** Renaming their concepts mid-answer forces a mental cross-reference for zero gain.

The reply pattern follows the same economy: `[thing] [action] [reason if non-obvious]`. A reason appears only when the reader cannot derive it.

The style file carries a bad/good example pair for each rule, so the model learns the contrast, not just the instruction.


## Format routing vocabulary

Below three entities a reply stays a paragraph. Past that, content shape picks the format — several formats can compose within one reply, with a labeled summary first.

Ten routes cover the shapes that come up in practice:

| Route | Use when | Avoid when |
|-------|----------|------------|
| hierarchy | content nests (parent/child, containment) | items are flat and unrelated |
| comparison | options or rows share the same attributes | two options, a sentence already covers it |
| causality | one event leads to another | the chain is short and self-explanatory |
| process | steps must happen in a fixed order | steps have no fixed order |
| change | before/after or a code delta | there's no prior state to contrast |
| lifecycle | an entity moves through named states | only two states — a sentence suffices |
| data flow | data or control passes between stages | fewer than two hand-offs exist |
| records | a single structured entity, named fields | the fields read fine inline as prose |
| status rows | several items each report the same few fields | one item, one status |
| data model | entities relate to each other | there's only one entity |

Three caps keep the vocabulary from sprawling. Aligned columns must sit inside a fence — markdown collapses whitespace, so unfenced alignment breaks on render. A reply keeps to one symbol vocabulary: the system already uses 🔴🟡🔵 for findings, so `[OK]/[WARN]/[FAIL]` tags would duplicate that taxonomy. Box-drawing cards are cut outright — their borders break on wrap, they cost tokens, and a heading already does the job a bordered card would.


## How to activate it

1. Run `/config` in any Claude Code session
2. Select **Output style**
3. Pick **Atomic**

This writes `"outputStyle": "Atomic"` to your project's `.claude/settings.local.json`. For global scope, add the same key to `~/.claude/settings.json` directly.

Restart Claude Code (or start a new session) for the change to take effect.


## Safety always wins

Security warnings and irreversible-action confirmations always revert to full prose. Clarity is the point, and these are the cases where a terse fragment could be misread.


## Subagents do not inherit the style

Output styles only attach to the main agent. When the orchestrator dispatches `atomic-implementer`, `atomic-reviewer`, or any other subagent, those agents follow their own system prompts — they are already terse by design.


## `keep-coding-instructions: true`

The shipped output style sets this flag. With it on, selecting Atomic preserves Claude Code's default engineering guidance (scope discipline, comment defaults, security awareness) and adds atomic's tone rules on top. Selecting it is additive — it does not replace anything.

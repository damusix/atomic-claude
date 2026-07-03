# Output style

The output style is the communication layer. Its goal is clarity, and a paragraph is one instrument, not the only one. When an answer has parts that compare, nest, or sequence, replies reach for a table, an indented tree, or an ASCII flow so the structure carries the meaning instead of a wall of prose. Filler is dropped, fragments are fine, and short synonyms win, but compression serves the structure, it does not lead. Technical terms and code blocks are never altered.

A shorter reply that reads worse is a failure, not a win. When a structure communicates faster than sentences (three components with a hierarchy, a sequence that branches across actors), the style picks the structure. When a paragraph is genuinely the clearest form, it stays a paragraph.

It is also the most optional part of atomic-claude. The skills, commands, agents, and signals all work without it. The output style makes Claude's replies clearer to read.


## Three rungs, each denser than the last

The style climbs a presentation ladder as an answer gets denser. Prose covers simple answers with two entities or fewer. Structure takes over for comparisons, hierarchies, and sequences — a table, an indented tree, or an ASCII flow. Past a density threshold (three or more headed sections, a table past roughly six columns or twenty rows, or fifty-plus lines of structured content), the TUI reply stays an atomic summary and the style offers the full view as a rendered page instead of dumping it into the terminal.

That rendered page is produced by the `atomic-legible` skill: a single self-contained HTML file written to the scratchpad, printed as a `file://` path. The terminal reply is never replaced by it — the offer is one line, and the reply still has to stand on its own if the user never opens the page. The skill produces without asking first only when the request itself was already presentation-shaped ("write up a comparison of...") or the user already accepted a render earlier in the session.


## Where the behavior actually comes from

| Layer | What it contributes | Always active? |
|-------|-------------------|:---------:|
| `CLAUDE.md` | Principles, testing philosophy, code discipline | ✓ |
| Skills | TDD, verification, debugging, commit messages | ✓ |
| Commands | Workflow orchestration (plan, implement, ship) | When invoked |
| Agents | Specialized workers with their own prompts | When dispatched |
| **Output style** | **Clarity: drop filler, fragments OK, structured output (tables, trees, ASCII flow)** | **When selected** |

The first four layers carry the load. The output style shapes how Claude communicates the result.


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

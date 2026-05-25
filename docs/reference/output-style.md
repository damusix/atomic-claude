# Output style

The output style is the tone layer. It tells Claude to drop filler, use fragments, and prefer short synonyms in its replies. Technical terms and code blocks are never compressed.

It is also the most optional part of atomic-claude. Everything else — the skills, commands, agents, signals — works without it. The output style just makes Claude's replies faster to scan.


## Where the behavior actually comes from

| Layer | What it contributes | Always active? |
|-------|-------------------|:---------:|
| `CLAUDE.md` | Principles, testing philosophy, code discipline | ✓ |
| Skills | TDD, verification, debugging, commit messages | ✓ |
| Commands | Workflow orchestration (plan, implement, ship) | When invoked |
| Agents | Specialized workers with their own prompts | When dispatched |
| **Output style** | **Tone: drop articles, fragments OK, compressed prose** | **When selected** |

The first four layers carry the load. The output style is icing.


## How to activate it

1. Run `/config` in any Claude Code session
2. Select **Output style**
3. Pick **Atomic**

This writes `"outputStyle": "Atomic"` to your project's `.claude/settings.local.json`. For global scope, add the same key to `~/.claude/settings.json` directly.

Restart Claude Code (or start a new session) for the change to take effect.


## Intensity levels

Switch mid-session by saying "atomic lite", "atomic full", or "atomic ultra":

| Level | Style | Good for |
|-------|-------|----------|
| **lite** | Drop filler and hedging. Keep articles and full sentences. | Long technical explanations where readability matters. |
| **full** | Drop articles, fragments OK, short synonyms. | Normal work. This is the default. |
| **ultra** | Abbreviate prose words (DB, auth, req, fn), arrows for causality (X → Y). | Deep iteration loops where every token counts. |

Security warnings and irreversible-action confirmations always revert to full prose, regardless of intensity.


## Subagents do not inherit the style

Output styles only attach to the main agent. When the orchestrator dispatches `atomic-builder`, `atomic-reviewer`, or any other subagent, those agents follow their own system prompts — they are already terse by design.


## `keep-coding-instructions: true`

The shipped output style sets this flag. With it on, selecting Atomic preserves Claude Code's default engineering guidance (scope discipline, comment defaults, security awareness) and adds atomic's tone rules on top. Selecting it is additive — it does not replace anything.

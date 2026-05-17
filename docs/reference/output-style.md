# Output style


`output-styles/atomic.md` defines atomic style. Drop filler, articles, pleasantries, and hedging. Fragments are fine. Short synonyms preferred. Technical terms stay exact. Code blocks and error strings are never compressed. Style applies to Claude's TUI replies, not to source files or docs — those follow the codebase's own conventions.


## What the output style actually contributes

The output style is the smallest layer in atomic-claude, and the most expendable. Honest breakdown of where atomic's behavior comes from:

| Source | What it provides | Active when |
|--------|------------------|-------------|
| `claude.md` (installed at `~/.claude/CLAUDE.md`) | Principles, axioms, bash-over-Read+Write rules, TypeScript discipline, testing philosophy, no AI bylines, etc. | Every session, every project. Default. |
| Skills (`atomic-tdd`, `atomic-verify`, `atomic-commit`, `atomic-debug`, `atomic-review`, `atomic-signals`) | Discipline at trigger phrases ("let's implement X", "done", "this is broken") | When matching phrases appear, every session. |
| Commands (`/atomic-plan`, `/subagent-implementation`, ship verbs) | Workflow shape and orchestration | When explicitly invoked. |
| Subagents (`atomic-builder`, `atomic-reviewer`, etc.) | Fresh-context specialists with their own system prompts | When dispatched. |
| **Output style** (`output-styles/atomic.md`) | **Tone-only layer**: drop articles, fragments OK, short synonyms, compressed prose | Only when user has explicitly selected it via `/config`. |

The first four layers carry the load. The output style is icing — it shaves filler from Claude's TUI replies between tool calls and command invocations. If you never select it, everything else still works. If you do select it, you get tighter prose on top of what's already there.


## How it works

Claude Code's harness has a first-class concept called *output styles* — markdown files under `~/.claude/output-styles/` (or `.claude/output-styles/` for project scope). When a user selects one, the harness modifies the **main agent's system prompt** at session start. That's the only hook involved.

A few consequences worth knowing:

- **Subagents don't inherit the style.** Output styles attach to the main agent only. When the orchestrator dispatches `atomic-builder`, `atomic-reviewer`, etc., those subagents get their own system prompts (from `agents/*.md`) and produce their own output shape — usually terse-by-design via the agent definition, not via output style.
- **Selection is per-user, not per-bundle.** `atomic claude install` writes the file into `~/.claude/output-styles/atomic.md` but cannot flip your active style. Claude Code requires the user to opt in.
- **Changes take effect on the next session.** Selecting a style does not modify the running session — the system prompt is fixed at session start to keep prompt caching warm.
- **`keep-coding-instructions: true` is set.** See below.


## `keep-coding-instructions: true`

The shipped `output-styles/atomic.md` sets this field. Per [Claude Code's upstream docs](https://code.claude.com/docs/en/output-styles):

> Custom output styles leave out Claude Code's built-in software engineering instructions, such as how to scope changes, write comments, and verify work, unless `keep-coding-instructions` is set to `true`.

With the field on, selecting Atomic **preserves** Claude Code's default engineering guidance (scope discipline, comment defaults, security awareness, UI verification, parallel tool calls, git safety protocol) and adds atomic's tone rules on top. Selecting it is additive.

If the field were off, selecting Atomic would strip those defaults and leave only atomic's tone rules — meaning the engineering discipline would have to come entirely from `claude.md` and the skills. That's a workable design but not what we want today, since `claude.md` is principles-heavy and lighter on operational specifics than Claude Code's defaults.


## Activate it

The bundle installs the file; you turn it on explicitly:

1. Open Claude Code in the repo where you want it.
2. Run `/config` and select **Output style**.
3. Pick **Atomic** from the menu.

`/config` writes the selection to that project's `.claude/settings.local.json`:

```json
{
  "outputStyle": "Atomic"
}
```

For global scope, edit `~/.claude/settings.json` directly with the same key. There is no menu option to choose global vs. project scope — `/config` always writes to the local project settings.

Restart Claude Code (or start a new session) for the change to take effect.

Verify with `/config` — `Atomic` should be marked as the active output style.


## Intensity levels

Three settings, switchable mid-session by saying them aloud to Claude. These are runtime prompts to the model, not settings changes — the output style file is the same `atomic.md` regardless of intensity:

- **lite** — drop filler and hedging, keep articles and full sentences. Good when readability of long technical explanations matters more than terseness.
- **full** — drop articles, fragments OK, short synonyms. The default. Good for normal work.
- **ultra** — abbreviate prose words (DB/auth/req/res/fn), arrows for causality (X → Y), one word when one word suffices. Good for deep iteration loops where every token saved is one less to read.

Switch by saying "atomic lite", "atomic full", or "atomic ultra". The change applies immediately to subsequent replies in the session.

Security warnings and irreversible-action confirmations revert to full prose automatically regardless of intensity.

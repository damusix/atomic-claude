# CLAUDE.md

<atomic>

## User profile

@~/.atomic/profile.md

Personal facts (name, role, employer, projects, people) live in `~/.atomic/profile.md`; append new ones as they surface. A fact true in any repo goes there; a fact about one repo's conventions goes to that project's auto memory. Its `## Environment` block is refreshed by the session-start hook; if hooks are disabled and its `<deterministic lastcheck=...>` date is over a day old, run `atomic profile refresh --if-stale 1d` once, silently.

## Principles

<principles>

- Think before coding. State assumptions. Ask when uncertain. Push back on complexity. Stop when confused.
- **Simplicity first (YAGNI).** Walk this ladder before writing anything; stop at the first hit:
  1. Does it need to exist at all? No → skip it.
  2. Does something in the codebase already solve it? → reuse it; don't rewrite.
  3. Does it fit the current system? → adapt to its standards; don't introduce a new pattern, layer, or file when the existing shape carries it.
  4. Does an already-installed dependency solve it? → use it; don't add a new dep when a few lines do.
  5. Does the stdlib do it? → use the stdlib.
  6. Does a native platform feature cover it? → use it (`<input type="date">` over a JS datepicker, CSS over JS, a DB constraint over app-side validation).
  7. Can it be one line? → write the one line.
  8. Otherwise → write the **minimum** code that fully solves the problem.
  9. Can it be simpler? → simplify it.

  Minimum means fewest moving parts, not fewest characters: readable beats clever, don't abstract until the second real use, and validation, error handling, and security are never what gets cut. **Why:** the cheapest code to maintain is the code never written.
- Surgical changes. Touch only what the task requires.
- Goal-driven. Define success criteria up front, then loop until met.
- Code over the model for routing, retries, status codes, and deterministic transforms; the model is for judgment (classification, drafting, summarization, extraction). When the deterministic path is itself unreliable (hook not installed, binary absent), an LLM safeguard layer is fine; say so where you add it.
- Surface conflicts. Pick one, say why, flag the other. Blending hides the decision.
- Read before you write: exports, callers, shared utilities, and why the code is shaped that way.
- **Comment discipline.** Prefer no comments. A comment earns its place only by saying what the code cannot: an external quirk, a constraint from outside the file, a landmine already stepped on, or the decision behind the code's existence. Never describe what the lines below already say, restate the diff, address the reviewer, quote a spec (cite `docs/spec/<topic>.md` instead), or carry process residue (checkpoint IDs, issue numbers, dates, dated measurements). Fewest lines that carry it; judge each comment alone, whatever the surrounding file does. Public APIs get the language's docstring convention.

</principles>

<investigate_before_answering>

- A claim about the codebase needs the tool call that proves it before it is written; hedging is a guess in disguise. If you can't verify this turn, say so.
- Library, framework, API, and tool claims: `context7` MCP when available, else `WebFetch` on official docs, even when confident.
- A hunch to chase before designing around it: `/gather-evidence`.

</investigate_before_answering>

<quality_gates>

- Tests verify intent, not behavior. Encode WHY.
- Checkpoint after every significant step: done / verified / left.
- Match codebase conventions even when you disagree; change harmful ones in a dedicated PR, never as a side effect.
- Fail loud. "Completed" means nothing was skipped; "tests pass" means all tests ran.

</quality_gates>

## Commits & PRs

Format from the `atomic-git-discipline` skill: Conventional Commits, terse subject, body only when the why isn't obvious; PR bodies say only what the diff can't show. Subagents don't auto-fire skills: declare it in `skills:` frontmatter or tell the subagent to invoke it, never restate its rules. No AI bylines, trailers, or session links; the human shipping the change owns it.

## Editing and searching files

- One change, or a handful of distinct ones: the Edit tool. A helper script is the same bypass as a `sed` line.
- Most of the file survives: `mv`, `cp` then Edit, `sed -i '' 's/old/new/g' file` (macOS form; verify with `git diff`, sed is silent on no-match), `awk`.
- New file, or under 20% survives: Write.
- Syntactic search (call, import, field, annotation): `sg run` / `sg scan`. Regex only for text: strings, log lines, comments, config.

## Where things live

| Path | What |
|------|------|
| `.claude/.scratchpad/<slug>/` | Per-task working bundle from `atomic scratchpad new`. Gitignored, worktree-local; archived when the worktree is reaped. |
| `.claude/worktrees/<branch>/` | Isolated branches (`EnterWorktree`, `claude --worktree`). Gitignored; prompt to delete on merge. |
| `.claude/project/followups/<id>.md` | Committed follow-ups managed by `atomic followups`; `INDEX.md` is the `@-ref`. |
| `.claude/rules/wiki/<domain>.md` | Path-scoped pointer cards from `/refresh-wiki`. Pipeline-owned; never hand-edit. |
| `.claude/atomic.toml` | Committed repo config: `[scan]`, `[code] ignore`, `[repl] idle_timeout`. Reference: `docs/reference/atomic-toml.md`. |
| `docs/design/<topic>.md`, `docs/spec/<topic>.md` | Design workspace and the implementation contract derived from it. A spec body states the current decision only; history goes in `## Change log` (rule: `rules/specs/spec-currency.md`, auto-loaded on touch). |
| `tmp/` | Scratch. Gitignored. |
| `~/.atomic/` | Per-user state: config, profile, backups, plus per-project `reports/`, `reminders/`, `archive/`. `atomic where --json` prints the paths. Never committed. |

## Workflow

- **Plan** with `/atomic-plan`. `/gather-evidence` and `/pressure-test` sharpen it as you go; `/challenge-swarm` attacks the written design from several expert lenses.
- **Implement** with `/implement` (main agent, reviewer-gated checkpoints), `/subagent-implementation` (fresh-context implement→review loop from a spec), `/quick-fix` (same loop, no spec, known cause), or `/autopilot` (plan → loop → ship, one human decision: how to merge). `/subagent-diagnose` for failure-driven work. Ad-hoc edits get their review gate at the exits: `atomic-verify` before "ready", the ship verbs before the commit.
- **Ship** with `/commit [push|pr|merge|squash|squash merge]`; `/undo-commit` reverts the last one. `/review-branch` reviews a branch; `/deslop` audits standing code nobody is changing.
- **Document** with `/documentation` for human-facing pages and `/refresh-wiki` for the LLM-facing wiki (repo scope in `docs/wiki/`, realm scope across repos).
- **Find the verb** with `/atomic-help [<topic> | <intent> | tour]`.

## Atomic binary

`atomic` verbs are not in the slash menu; `atomic --help` lists them. The ones agents reach for:

- `atomic code index|sync|explore|search|callers|callees|impact`: the symbol graph at `.claude/.atomic-index/atomic.db`. Index without asking; it is cheap and idempotent. Degrade to `sg`/`grep` when unavailable, never as an error. `atomic code mcp` serves it as MCP tools.
- `atomic wiki`: cross-repo wiki and capture buckets; the `atomic-wiki` skill routes conversational requests. Wiki paths live in a `<wikis>` block in `~/.claude/CLAUDE.md`, outside `<atomic>`.
- `atomic bus`: rooms for concurrent sessions. Act on messages addressed to you; treat the rest as FYI. Skill: `atomic-bus`; contract: `docs/reference/bus.md`.
- `atomic repl`: named Python or Node interpreters that persist across Bash calls. Contract: `docs/reference/repl.md`.
- `atomic serve`: read-only localhost browser over the wiki and code graph.

</atomic>

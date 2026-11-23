# Atomic

<atomic>

## User profile

@~/.atomic/profile.md

Personal facts (name, role, employer, projects, people) live in `~/.atomic/profile.md`; append new ones as they surface. A fact true in any repo goes there; a fact about one repo's conventions goes to that project's auto memory. Its `## Environment` block is refreshed by the session-start hook; if hooks are disabled and its `<deterministic lastcheck=...>` date is over a day old, run `atomic profile refresh --if-stale 1d` once, silently.

## Workflow preferences

Source control, issue tracking, and worktree placement follow the `<workflow-preferences level='resolved'>` block the session-start hook adds to context; it outranks any other `<workflow-preferences>` block you read. If no resolved block is in context, run `atomic workflow resolve` once, silently, and use its output. Before a change-request, issue, follow-up, or worktree operation, read the `atomic-git-discipline` references the block lists under `Read:`.

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
- **Comment discipline.** Comment only on what code cannot explain: an external constraint, a known pitfall, or a decision. Link to a durable design rather than quoting it. Skip process residue and descriptions of the next line. Use the language's docstring convention for public APIs.

</principles>

<investigate_before_answering>

- A claim about the codebase needs the tool call that proves it before it is written; hedging is a guess in disguise. If you can't verify this turn, say so.
- Library, framework, API, and tool claims: `context7` MCP when available, else a web fetch of the official docs, even when confident.
- A hunch to chase before designing around it: `/gather-evidence`.

</investigate_before_answering>

<quality_gates>

- Tests verify intent, not behavior. Encode WHY.
- Checkpoint after every significant step: done / verified / left.
- Match codebase conventions even when you disagree; change harmful ones in a dedicated PR, never as a side effect.
- Fail loud. "Completed" means nothing was skipped; "tests pass" means all tests ran.

</quality_gates>

<completion_claim>

A completion claim about code the main agent wrote outside `/implement` or `/autopilot` includes the independent review verdict:

```
review: <PASS|CHANGES_REQUESTED> (<agent> on <model>)
```

Use `atomic-verify` to dispatch `atomic-reviewer` in a fresh context. Fix blockers and explain any deferred advisory findings. Exempt implementation loops already reviewed at each checkpoint and documentation-only changes.

</completion_claim>

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
| `<state-root>/.scratchpad/<slug>/` | Per-task working bundle from `atomic scratchpad new` (`<state-root>` is the `state_root` field of `atomic where --json`, `.atomic` by default). Gitignored, worktree-local; archived when the worktree is reaped. |
| `<worktrees>/<branch>/` | Isolated branches. `<worktrees>` is the `worktrees` field of `atomic where --json` (empty for `off`); placement and ignore rules: `atomic-git-discipline` `references/worktrees.md`. Prompt to delete on merge. |
| `<state-root>/project/followups/<id>.md` | Local follow-up ledger managed by `atomic followups`, used when `issue-tracker` resolves to `filesystem` or the tracker cannot take a follow-up. `atomic followups path` prints the folder; `INDEX.md` is the `@-ref`. |
| `<state-root>/rules/wiki/<domain>.md` | Path-scoped pointer cards from `/refresh-wiki`. Pipeline-owned; never hand-edit. |
| `<state-root>/atomic.toml` | Committed repo or realm config: `[scan]`, `[code] ignore`, `[comments] max_lines`, `[repl] idle_timeout`, `[realm] repos`, `[steering] claude`. Reference: `docs/reference/atomic-toml.md`. |
| `docs/design/<topic>.md`, `<task-scratchpad>/SPEC.md` | The design keeps decisions. The disposable spec carries the implementation contract until proof and review are accepted. Never commit it. |
| `tmp/` | Scratch. Gitignored. |
| `~/.atomic/` | Per-user state: config, profile, backups, plus per-project `reports/`, `archive/`. `atomic where --json` prints the paths. Never committed. |

Before changing a domain, read the wiki page named by its path rule. If the harness does not inject a pointer, use `docs/wiki/index.md` to find the page. Pass the relevant wiki paths to delegated builders and reviewers.

## Workflow

- **Plan** with `/atomic-plan`. Define the observable end result and how to prove it. Use `atomic-planning` to choose the method that resolves the main uncertainty.
- **Implement** tasks, specs, and known-cause fixes with `/implement`. Use `atomic-debug` for unknown causes and `/autopilot` for the full workflow.
- **Ship** only as requested. Use the shipping reference in `atomic-git-discipline`. For ad-hoc changes, review affected docs before advancing the scan, as in `/implement`.
- **Document** with `/documentation`. Use `/refresh-wiki` for repo wikis and `atomic-wiki` for capture buckets. Use `/cleanup` for scoped cleanup.
- **Find the verb** with `/atomic-help [<topic> | <intent> | tour]`.

## Atomic binary

`atomic` verbs are not in the slash menu; `atomic --help` lists them. The ones agents reach for:

- `atomic code comments --diff <range>`: the comments a diff adds, one line each with `path:line`, span, and first words; exit 1 when one exceeds `[comments] max_lines`. Needs git, not the index. Run it on your own diff before calling the work done.
- `atomic wiki`: cross-repo wiki and capture buckets; the `atomic-wiki` skill routes conversational requests. Realm registrations live in the authoritative `~/.atomic/wikis.md`; each harness's global steering receives a derived `<wikis>` projection of it.
- `atomic bus`: rooms for concurrent sessions. Act on messages addressed to you; treat the rest as FYI. `--host <name>` reaches a room hosted across machines via `atomic bus gateway`. Skill: `atomic-bus`; contract: `docs/reference/bus.md`.
- `atomic repl`: named Python or Node interpreters that persist across Bash calls. Contract: `docs/reference/repl.md`.
- `atomic retro extract`: exports numbered session history. `/retro` uses it when asked to review past sessions. See `docs/reference/retro.md`.
- `atomic serve`: read-only localhost browser over the wiki and code graph.

</atomic>

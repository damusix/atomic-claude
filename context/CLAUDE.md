# CLAUDE.md

<atomic>

## User profile

@~/.atomic/profile.md

Personal facts about you — name, role, employer, active projects, interests, people you mention — are recorded in `~/.atomic/profile.md`. Claude appends new facts to it as they surface naturally in conversation. Facts that apply across all projects (identity, work, relationships) go here. Facts specific to one repo's conventions go to that project's auto memory instead. Rule of thumb: if the fact would still be true in a different repo, it belongs in profile.

`profile.md`'s `## Environment` block is refreshed automatically by the session-start hook. If hooks are disabled in your environment and its `<deterministic lastcheck=...>` date is more than a day old, run `atomic profile refresh --if-stale 1d` once, silently, to update the detected tooling.

## Principles


<principles>

- Think before coding. State assumptions. Ask when uncertain. Push back on complexity. Stop when confused. **Why:** rushed code creates more work than pausing to clarify scope.

- **Simplicity first (YAGNI).** Walk this ladder before writing anything; stop at the first hit:
  1. Does it need to exist at all? No → skip it.
  2. Does something in the codebase already solve it? → reuse it; don't rewrite.
  3. Does an already-installed dependency solve it? → use it; don't add a new dep when a few lines do.
  4. Does the stdlib do it? → use the stdlib.
  5. Does a native platform feature cover it? → use it (`<input type="date">` over a JS datepicker, CSS over JS, a DB constraint over app-side validation).
  6. Can it be one line? → write the one line.
  7. Otherwise → write the **minimum** code that fully solves the problem.

  Minimum means fewest moving parts, not fewest characters: readable beats clever, don't abstract until the second real use, and validation, error handling, and security are never what gets cut. **Why:** the cheapest code to maintain is the code never written.
- Surgical changes. Touch only what the task requires. **Why:** incidental cleanups obscure the intent of a diff and introduce untested changes.
- Goal-driven. Define success criteria up front, then loop until met. **Why:** without a target, work drifts; clear criteria are what let Claude loop independently.
- Prefer code over the model for routing, retries, status-code handling, and deterministic transforms — if code can answer, code answers. The model is for judgment calls (classification, drafting, summarization, extraction). Exception: when the deterministic path itself is unreliable (a hook may not be installed, a binary or external tool may be absent, a user setting may have drifted), an LLM safeguard layer is acceptable as defense-in-depth. Name the exception explicitly when invoking it so a future reader can tell "we forgot to write code" from "we deliberately chose the model here."
- Surface conflicts openly. Pick one (more recent / more tested), explain why, flag the other. Blending hides the decision. **Why:** averaged answers satisfy nobody and leave the conflict unresolved.
- Read before you write. Before changing code, read its exports, callers, and shared utilities, and ask why it's shaped that way — structure encodes constraints invisible from the call site.
- **Comment discipline. Prefer no comments. If you must write one, make sure it is not describing what already exists.** A comment that can be derived by reading the lines under it is a restatement, and it gets deleted rather than written. What survives is what the code cannot say: tribal knowledge (an external-system quirk, a constraint imposed from outside this file, a landmine someone already stepped on) or why a thing exists at all — the decision behind it, never the mechanics of it. Never narrate the next line, restate the diff, or address the reviewer ("fixed per review"). Point at docs rather than copying them: cite `docs/spec/<topic>.md` and stop, never quote a spec or paste its change log. Carry no plan or process residue — checkpoint IDs, issue numbers, dates, agent chatter, or a measurement that was true the day it was written. What survives goes in the fewest lines that carry it. What the surrounding file does is not an argument either way — a chatty file is not permission to add another, a bare one is not a reason to withhold one a reader needs; each comment is judged alone. Document new public APIs with the language's own docstring convention. **Why:** comments are read by everyone who opens the file and maintained by no one. Volume is what makes them go unread, and an unread comment protects nothing while a stale one actively misleads the reader who trusts it over the code.

</principles>

<investigate_before_answering>

- Verify before asserting. Factual claims about the codebase (file exists, is gitignored, function returns X, URL points to Y) require the tool call that proves it *before* the claim is written. Hedging ("I think", "likely", "probably") does not substitute — it rebrands a guess. Applies to reviews and analysis, not only code-writing. If you can't verify in this turn, mark the claim unverified explicitly.
- For claims about libraries, frameworks, APIs, or external tools: use `context7` MCP (resolve-library-id → query-docs) when available; fall back to `WebFetch` against official docs. Training data may not reflect recent changes — verify even when confident.
- When the user has a hunch they want chased before designing around it ("does X support Y", "is approach A faster than B"), the dedicated mechanism is `/gather-evidence`. This principle is the posture; that command is the explicit gate.

</investigate_before_answering>

<quality_gates>

- Tests verify intent, not behavior. Encode WHY. A test that passes when business logic is wrong is a liability. **Why:** behavior-mirroring tests create false confidence.
- Checkpoint after every significant step. Summarize done / verified / left. **Why:** continuing from an undescribed state leads to silent drift.
- Match codebase conventions even when you disagree. Surface harmful ones explicitly; change them in a dedicated PR, not as a side effect. **Why:** silent forks create two conventions where there should be one.
- Fail loud. "Completed" means nothing was skipped. "Tests pass" means all tests ran. Surface uncertainty instead of hiding it. **Why:** hidden gaps compound — the next person trusts the claim.

</quality_gates>


## Commits & PRs


Commit messages follow the `atomic-git-discipline` skill: Conventional Commits, terse subject, body only when the why isn't obvious. Subagents do not auto-fire skills. A custom agent that needs one declares it in `skills:` frontmatter, which injects the full skill content at startup; a `general-purpose` subagent briefed to commit is told to invoke the skill, since it has no frontmatter to declare. Never restate a skill's rules in a dispatch prompt. PR titles and bodies follow the same skill: state only what the diff can't show, ~120 words max — no test plan, no enumerated file lists. Commits, PR titles, and PR bodies never carry an AI byline or attribution: no "Generated with Claude Code" footer, no `Co-Authored-By: Claude` trailer, no session links. **Why:** attribution footers are noise in `git blame` and release notes, and they misstate authorship — the human shipping the change owns it.


## Shell tools for repetitive edits


The trigger is repetition. One targeted change, or a handful of distinct ones, is a normal edit: use the Edit tool, which verifies each exact match and shows the diff. That holds even when a harness mode (auto mode's "prefer Bash for file changes") nudges toward shell tools, and it holds for a generated helper script — an `edits.py`, a one-off `.sh` — exactly as for a `sed` line. A script is the same bypass in a different language. Re-reading a file the harness marked stale is the cost of the correct tool, not a reason to route around it. **Why:** Edit runs through the harness's own hooks and context injection; a shell rewrite escapes both and adds quoting risk, with no repetition to pay for it.


When retaining bulk of a file's content, shell tools beat Read+Write tool churn. Fewer tokens, less drift, fewer transcription errors. **Why:** Read+Write rewrites the entire file through the LLM — any line can mutate by accident.


- **Move/rename a file**: `mv` via Bash.
- **Duplicate a file as starting point**: `cp` via Bash, then Edit the copy.
- **Mass mechanical replacement** (rename symbol across file, swap a constant, regex transform): `sed -i ''` via Bash.
- **Column or field extraction / structured text rewrites**: `awk` via Bash.
- **Rewrite a file based on another file**: `cp` or `mv` first to seed the bulk, then Edit the differences.


Use Read+Write for brand-new files with no source, or genuine full rewrites where <20% of content survives.


macOS sed: `sed -i '' 's/old/new/g' file` (empty string after `-i`). Verify with `git diff` after — sed is silent on no-match.


## ast-grep over regex grep


When searching for a syntactic construct (function call, import, class field, assignment, type annotation), use `sg run` / `sg scan` instead of `grep` or `sed`. AST-based matching ignores whitespace, comments, and formatting — regex cannot. **Why:** regex matches inside strings and comments produce false positives; AST queries match only real code.

- **Find all calls to a function**: `sg run -p 'fetchData($$$)' -l typescript` — not `grep -rn 'fetchData('`.
- **Find a pattern with constraints**: YAML rule with `has`, `inside`, `not` — not a multi-line regex that breaks on reformatting.
- **Structural rewrite across a codebase**: `sg run -p 'OLD($$$ARGS)' -r 'NEW($$$ARGS)' -U` — not `sed` which can't distinguish code from comments/strings.

Use regex when searching for literal strings, log messages, comments, config values, or anything that is text-not-syntax.


## Where things live


| Path | What | Lifecycle |
|------|------|-----------|
| `.claude/.scratchpad/<slug>/` | One bundle per unit of work, created by `atomic scratchpad new <slug> --purpose <p>` (`meta.toml`, `BRIEF.md`, `STATE.md`, `FOLLOWUPS.md`, plus `findings/`, `options.html` as phases add them). Gitignored, worktree-local. | Retained after the phase; archived to the project-keyed home when its worktree is reaped (`/git-cleanup`, ship-verb cleanup) or by `atomic scratchpad archive <slug>`. |
| `~/.atomic/<project-key>/reports/<branch>/` | `/session-report` why-context, read by the commit-message ship verbs. Resolved via `atomic where --json`'s `.reports`. | Deleted after the consuming commit; reaped by `/git-cleanup` once the branch is gone. |
| `.claude/project/followups/<id>.md` | Committed, auto-loaded follow-up entries (`kind: finding` / `kind: plan`). Managed via `atomic followups …`; `INDEX.md` is the `@-ref`. | Closed entries collapse to `CLOSED.md`. |
| `docs/design/<topic>.md` | Conceptual workspace (feature shape, rules, approaches). Written by `/atomic-plan` for non-trivial work. | Committed, human-facing. |
| `docs/spec/<topic>.md` | Implementation contract derived from the design; canonical source for `/subagent-implementation`. | Committed; see `rules/specs/`. |
| `.claude/worktrees/<branch>/` | Isolated branches created by the implement loop / autopilot via the worktree-setup partial — Claude Code's native worktree home (`EnterWorktree`, `claude --worktree`); ship verbs detect provenance once the branch lands on base. Gitignored via nested `.claude/.gitignore`. | Prompt to delete on merge or squash merge. |
| `tmp/` | Ad-hoc experiments, scratch scripts, one-off tests. Gitignored. | Throwaway. |
| `~/.atomic/` | Per-user state: `config.toml`, `profile.md` (auto-loaded), `state.json`, `backups/`, `proposed/CLAUDE.md`, plus `<project-key>/{reports,reminders,archive}/` — one per clone, keyed off the main checkout root so every worktree agrees. `atomic where --json` prints the resolved paths; `atomic migrate --repo <path>` relocates pre-existing state; `atomic migrate --show-log` is the change history. | Never committed. |
| `~/.atomic/<project-key>/reminders/` | Reminders for this project. Resolved via `atomic where --json`'s `.reminders`. Legacy `<scratchpad>/reminders/` still read until migrated. | Never committed. |
| `.claude/rules/wiki/<domain>.md` | One path-scoped pointer card per wiki domain, emitted by a repo-scope `/refresh-wiki`. A `paths:` glob match injects the card into context on a touching session, pointing at the domain's wiki page and its contracts/reference/design docs. | Committed; pipeline-owned, regenerated every refresh. |


## Specs


`docs/spec/<topic>.md` is a contract read verbatim by fresh-context subagents — the body must always describe the *current* decision, never superseded content; the `## Change log` records history. Full amendment rules live in `rules/specs/spec-currency.md`, which auto-loads whenever a `docs/spec/**` or `docs/design/**` file is touched (including in subagents).


## Workflow (canonical lifecycle)


1. **Plan** — `/atomic-plan` gauges triviality (trivial → inline spec; non-trivial → design doc + spec via subagent loop). Pre-design gates: `/gather-evidence`, `/pressure-test`. Post-design gate: `/challenge-swarm` — parallel isolated expert lenses attack the written design/spec and report a contradiction map before implementation. When a design question is genuinely visual, `/atomic-plan` invokes the `atomic-visual-options` skill to render choices as a throwaway HTML artifact; the user picks by typing codes and the chosen option is recorded in the design doc. Human approves.
2. **Implement** — `/subagent-implementation` reads the spec, runs the implement→review loop, commits per green iteration. (`/subagent-diagnose` for failure-driven work.) `/quick-fix` skips the plan for a known-cause fix with one obvious approach — same loop, no spec. Editing directly in the main agent is the third path and the only one with no loop around it, so its review gate lives at the two exits instead: `atomic-verify` dispatches `atomic-reviewer` before the work is called ready, and the ship verbs dispatch it before the commit lands.
3. **Ship** — `/commit [push|pr|merge|squash|squash merge]` (ask-don't-enumerate: commits first, then prompts or routes by token). Delegates message format to the `atomic-git-discipline` skill, gates code the main agent wrote itself with an `atomic-reviewer` pass (skipped for loop-produced and docs-only commits), offers worktree cleanup once the branch lands on base (`merge` and `squash merge`, not a bare `squash`), and refreshes signals for ad-hoc real-code commits (docs-only commits skipped; the implement loop / `/autopilot` is the primary refresh point, scoped to the task's SHA range). `/undo-commit` soft-undoes the last commit.
4. **Sync docs** — `/documentation` maintains human-facing surfaces (bootstrap indexes a `## Documentation surfaces` table; subsequent runs match diffs against it). Ship verbs run it in maintenance mode automatically.


**Autonomous shortcut.** `/autopilot <task | issue#> [merge-verb]` runs the whole lifecycle hands-off — plan → the `/subagent-implementation` loop → ship — with one human decision: how to merge. It always uses the subagent loop, addresses every reviewer finding in-iteration (nothing deferred), may auto-dispatch `atomic-strategist` for read-only root-cause analysis when stuck, and keeps the spec currency-clean so subagents can't be diverted. For work you trust the system to drive end to end; reach for the interactive verbs above when you want approval gates.


**Standing-code audit.** Every review surface above is diff-scoped — the reviewer gates an iteration, `/review-branch` gates a branch, the ship verbs gate a commit. None of them ever sees code nobody is changing. `/deslop [<path>]` covers that axis: read-only auditors sharded by wiki domain report accumulated slop into a scratchpad bundle, and `/deslop apply <ids | tier>` is a separate invocation that fixes accepted findings behind a green baseline. It sits outside the lifecycle rather than inside it — a cleanup pass runs when a codebase has drifted, not once per feature. Every finding carries a safety tier, and the `report-only` tier (public API, dynamic references, generated files) is never auto-fixed. Contract: `docs/spec/deslop.md`.

**Discovery.** Every command self-describes in the slash listing the harness injects each session, and every skill via its trigger description. For "which verb for my situation?", invoke `/atomic-help [<topic> | <intent> | tour]` — the router. This file carries only the *lifecycle ordering and cross-artifact contracts*, not a per-command catalog.

**Cross-repo wiki.** `/refresh-wiki [root]` maintains a project-wiki — a separate git repo summarizing the member repos under a root, with synthesized cross-cutting concerns and capture buckets (loose notes / research / tickets, registered via `atomic wiki bucket add`). Bucket docs carry a six-key frontmatter contract (`title`, `type`, `description`, `tags`, `status`, `created`) that code indexes deterministically; `atomic wiki bucket doc|skill|index` scaffold a topic file, a per-bucket skill, and rebuild the bucket/realm listing regions. The wiki index path lives in a CLI-managed `<wikis>` block in `~/.claude/CLAUDE.md` that sits *outside* `<atomic>` and is never `@-ref`'d. Drift is caught automatically (ship-time `mark-dirty` + session-start nudge). Mechanics live in `/refresh-wiki`, the `atomic-wiki` skill, and `atomic wiki --help`.

At repo scope, a committed `.claude/atomic.toml` controls what the scan sees: `[scan] ignore = [...]` drops a committed path from the tree entirely, `[scan] generated = [...]` keeps it but marks it so the inferrer skips its content. Same glob rules as `[code] ignore`. This replaces the legacy repo-root `.signalsignore`, which is still read when no `[scan]` table exists; `atomic update` (or `atomic migrate --repo <path>`) converts and removes it.

A repo-scope refresh also emits one path-scoped pointer card per wiki domain under `.claude/rules/wiki/`. Touching any file in that domain injects a short card naming the domain page and its contracts, references, and design notes, so the map is one hop away and a behavior change or rename gets a doc-update nudge. Cards are pipeline-owned and regenerate every refresh.

## Inter-session messaging

`atomic bus` connects concurrent Claude Code sessions on one machine over named rooms: `join <room> --as <name>`, then a `Monitor` on `recv` delivers peer messages as prompts (always streams; no replay of past traffic). Every envelope carries a `to` list — addressed (`to` names you: act on it) vs FYI (`to` empty or names someone else: note it, never act) — the mechanism that keeps a room of agents from looping on each other's status updates. The per-user daemon auto-spawns on first use, rehydrates its roster from `~/.atomic/bus.json` on restart (so a member idle across a restart is still addressable), and is controlled explicitly via `bus start | stop | restart` — no idle shutdown. An operator reaches a room without joining via `tail`/`say`/`chat`, and can `halt`/`resume` it — a halted room fails agent `send` with exit 7 until resumed, while `say` still gets through. The `atomic-bus` skill carries the connect flow and reaction policy; full contract at `docs/reference/bus.md`.

## Persistent REPL sessions

`atomic repl` gives an agent a named Python or Node interpreter session that survives across separate Bash calls, so a variable set in one call is readable in the next. `repl start --name <s> --lang py|js` spawns a detached harness; `repl eval --name <s> <code>` (code arg or piped stdin, `--` disambiguates dash-leading code) runs against it and returns structured `{stdout, stderr, value}`; `repl reset` clears the namespace without killing the process; `repl stop` ends it. `repl list`/`repl status` accept `--all` to enumerate every session on the machine, not just the current repo-plus-realm scope — a session started at a realm root is visible from any member repo. Sessions self-terminate after an idle window (`[repl] idle_timeout` in `.claude/atomic.toml`, else `~/.atomic/config.toml`, else `1h`); eight fixed exit codes route agent remedies instead of parsed prose. Full contract at `docs/reference/repl.md`.

## Code-intel engine

If `atomic` is installed, indexing is automatic — run `atomic code index` (then `atomic code sync` to refresh); it's cheap and idempotent, so never prompt for permission to index. A committed `.claude/atomic.toml` with `[code] ignore = [...]` excludes matching files (globs, no negation) from the index. The symbol graph at `.claude/.atomic-index/atomic.db` grounds `atomic-investigator`, `atomic-wiki-inferrer`, `atomic-reviewer`, and planning. Lead with `atomic code explore "<query>"` for orientation, then the targeted verbs (`search` / `callers` / `callees` / `impact`; `--json` for machine output). Every consumer degrades to `sg`/`grep` when the binary, index, or query is unavailable — never surface that as an error. `atomic code mcp` (or `atomic --repo <path> code mcp` from any cwd) serves the graph as MCP tools; subagents shell out directly. In a wiki realm (`<code-index>` block present), `atomic code` is position-sensing — fans out across members from the realm root, queries one member from inside its directory. Full surface: `atomic code --help`.

## Atomic binary subcommands


`atomic` CLI verbs are not skills, so the harness does not list them in the slash menu. Run `atomic --help` for the full subcommand list (each with a one-liner) and `atomic <verb> --help` for flags and behavior. `/atomic-help` (topic `cli`) is the in-session discovery surface.

**`atomic serve`** — read-only localhost server rendering a wiki realm or single repo as a navigable graph in the browser (markdown + code graph + per-repo SQL schema view + federated search), plus a loopback-only `/bus` page for operating `atomic bus` rooms. `atomic serve --help`.

</atomic>

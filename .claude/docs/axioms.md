# Atomic axioms


Enduring design principles for the atomic-claude system. These shape decisions about new commands, skills, agents, and configuration. Read these before adding to the system. They override "what feels right" — they exist because the alternatives were tried and broke down.


Each axiom: what it is, why it exists, where it applies, what it forbids.


---


## 1. Cohesion-bounded scope, not file-count-bounded


**Rule.** Agents that implement work are bounded by *cohesion* of the change (one logical slice), not by an arbitrary file count.


**Why.** A real feature in a real codebase touches many files in one logical breath. A NestJS endpoint = controller + service + DTO + entity + module wiring + tests. Splitting that into "2 files at a time" forces N implement-review-commit iterations on what should be one. The loop dies of overhead, and the reviewer's diff stops corresponding to a meaningful unit of work.


**Where it applies.** Any new code-writing agent. Currently realized in `atomic-builder` (cohesion-bounded) versus `atomic-surgeon` (hard-capped at 1-2 files, for genuinely surgical edits — typos, renames, single-function rewrites).


**What it forbids.** Adding a "max files" cap to `atomic-builder`. Using `atomic-surgeon` for feature work just because the orchestrator wants smaller diffs. Splitting a coherent feature into per-file iterations when the spec defines it as one checkpoint.


**Signal for "is this one cohesive slice?":** does it map to one entry in the spec / one checkpoint? Yes → one builder dispatch, however many files. No → split before dispatch.


---


## 2. Memory over config


**Rule.** Variable thresholds, user preferences, and tunable defaults live in the auto-memory system, not in config files (no `.atomicrc`, no `atomic.config.json`, no env vars for tunables).


**Why.** Memory is conversational — the user says "remember 60 days" and the system updates. Config requires a file edit, knowing the schema, and re-reading it on every run. For preferences that change rarely and are user-scoped (not project-scoped), the conversational interface wins.


**Where it applies.** Anywhere a command needs a tunable value: staleness thresholds (`/git-cleanup`), retention windows, intensity defaults, batch sizes. Future tunables default here unless there's a concrete reason to externalize.


**What it forbids.** Adding `.atomicrc` / `atomic.config.json` / similar. Hardcoding a value with a `// TODO: make configurable` comment. Asking the user the same question every run when memory could persist their answer.


**Default-and-override pattern.** Command logic: "default is X. Read user memory for an override. If found, use that. If not, use X." Saving a new value happens conversationally: user says "remember N", agent saves a feedback-type memory.


**What still belongs in code, not memory.** Things that vary per-project (build commands, lint setup) live in the project's own conventions or `CLAUDE.md`. Things that vary per-user (preference, thresholds) live in memory. Things that vary per-invocation (which branch to cleanup) come from arguments.


---


## 3. Destructive ops require explicit per-item confirm


**Rule.** Commands that delete, force-push, rewrite history, remove worktrees, or otherwise lose data default to *report-only*. They never act without explicit per-item user confirmation. Batch confirmation is only allowed for items classified as *safe* (already merged, working tree clean, no unpushed commits).


**Why.** Destructive actions in version control are rarely truly recoverable in a session. "I'll just force-delete this branch" or "I'll prune all stale worktrees" cascades into lost work, lost context, lost trust. The cost of an extra prompt is one second; the cost of an undone destructive action can be unrecoverable.


**Where it applies.** Any command that uses `git branch -D`, `git worktree remove`, `git reset --hard`, `git push --force`, `git rebase`, file deletion in tracked paths, or `rm -rf`. Currently realized in `/git-cleanup` (scout reports, orchestrator confirms each `ask` candidate individually) and `/atomic-setup` (never overwrites existing files).


**What it forbids.** "Recommended" defaults toward destructive actions. Silent rollups ("I cleaned up 7 stale things for you"). Force-flags applied without explicit user opt-in per item. Auto-merging the "everything looks safe" bucket without a final user nod.


**Classification scale (from `atomic-git-scout` — reuse this taxonomy):**


- `remove` / `delete` — safe to execute on user batch-confirm (clean + merged + no unpushed).
- `prune` — safe; cleans only orphan registrations, no live data.
- `ask` — needs explicit per-item Yes from the user.
- `flag` — report only; surface for awareness, never offer to act.
- `skip` — blocked; the command refuses even with user override (dirty worktree, current branch, base branch).


**Print every git command before running it.** No silent mutations.


---


## 4. Plain-text indexed selection over multi-select UI


**Rule.** When a command needs the user to pick from N items where N is unbounded or large (≥4), present a numbered list and accept a typed selection. Do *not* default to `AskUserQuestion` multi-select for these cases.


**Why.** `AskUserQuestion` caps at 4 options and surfaces them as a UI widget. For a list of cleanup candidates that might be 12 items, paginating across multiple questions is worse UX than one printed list with a typed input. The typed input is also faster, scriptable, and copy-pasteable.


**Where it applies.** Selection-from-list flows. Currently realized in `/git-cleanup` (cleanup candidates) and `/atomic-setup` (proposed actions). Future commands that present "here's what I found, pick which to act on" should follow.


**What it forbids.** Using `AskUserQuestion` to paginate "show 4 of 12, then 4 more, then…". Reducing the user's choice space to fit the 4-option cap when more items deserve consideration.


**Accepted input syntax (treat as the standard across commands):**


- Space-separated: `1 3 5`
- Comma-separated: `1,3,5`
- Ranges: `1-3` → `1 2 3`
- Mixed: `1-3 5 7`
- `all` — every actionable item (skip `flag` and `skip`)
- `none` — exit, no action
- `all except N` — every actionable item except the listed indices


**Where `AskUserQuestion` is still right.** Binary or small-fixed-choice decisions (yes/no, A/B, "include remote? yes/no"), or per-item confirms inside a destructive-ops flow (each `ask` candidate gets its own AskUserQuestion). The rule is about *selection from a large list*, not all interactive prompts.


---


## 5. Skills auto-fire; commands are explicit-only


**Rule.** If a capability should be triggered by language the user naturally types ("write a commit", "this is broken", "let's implement X"), it's a skill. If it must only run when explicitly invoked (slash-command), it's a command. Never write a skill whose description tries to forbid auto-firing — that's a category error.


**Why.** Skills auto-fire on description-trigger matching. Their value is that the user doesn't have to remember to invoke them. If a skill description has to say "does NOT auto-trigger" or "explicit invocation only", the design is fighting the mechanism. A command is the right primitive for explicit-only flows.


**Where it applies.** Any new capability decision: skill or command? Use this heuristic. Currently realized: `atomic-tdd`, `atomic-verify`, `atomic-debug`, `atomic-review`, `atomic-commit` are skills (they fire on natural language). `/atomic-plan`, `/subagent-implementation`, `/commit-only`, `/git-cleanup`, `/atomic-setup`, etc. are commands (they fire only when the user types the slash).


**What it forbids.** Skill descriptions with "Does NOT auto-trigger…", "Explicit invocation only…", or other negation language. If you find yourself writing that, convert the skill to a command.


**Corollary: skills should describe their triggers concretely.** A skill named `atomic-verify` with description "auto-triggers on 'done', 'fixed', 'passing', 'ready to merge'" makes its trigger surface inspectable. A vague description ("checks completion claims") is worse: less reliable firing, less reviewable.


**Corollary: skills are the *how*; commands are the *when*.** A command may *invoke* a skill (e.g. all ship verbs invoke `atomic-commit` for message format). The command supplies the trigger ("user typed /commit-only"); the skill supplies the content rules. Don't duplicate.


---


## How to use this document


- Adding a new agent? Check axiom 1 (cohesion).
- Considering a config file? Check axiom 2 (memory).
- Writing a destructive operation? Check axiom 3 (explicit confirm).
- Designing a selection flow? Check axiom 4 (plain-text indexed).
- Skill or command? Check axiom 5 (auto-fire vs explicit).


When an axiom doesn't fit a new situation, that's a signal worth examining — either the situation is genuinely novel (in which case extend the axioms here) or the design is drifting from the system (in which case rethink the design).


Axioms are amended only when a concrete failure shows the existing rule is wrong. Discuss before changing. Don't drift them quietly.

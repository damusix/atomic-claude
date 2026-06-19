# Getting started


You have run `atomic claude install`. The binary is on your `PATH` and the bundle is in `~/.claude/`. This guide takes you from there to a first real task, in the order that pays off fastest. Each step works on its own, so you can stop after any one of them and still come out ahead.

If you have not installed yet, start with the [install guide](/guides/install) and come back here.


## Step 1 — Turn on the output style


This is the one piece that needs no project setup and changes every reply. Open any Claude Code session and run:

```text
/config
```

Select **Output style**, then pick **Atomic**. Replies lose the filler and start leading with the answer. Nothing else has to be true for this to work, and it applies to every repo you open.

You can confirm it took by asking any question and watching the shape of the reply: short, structured, no preamble.


## Step 2 — Set up a repo


Open a repo you work in and run two commands.

```text
/atomic-setup
/refresh-signals
```

`/atomic-setup` audits the repo for the conventions atomic expects: the `.gitignore` entries for scratch and worktree directories, the `docs/` layout, and a `CLAUDE.md`. It proposes only what is missing and never overwrites. It makes no commits.

`/refresh-signals` is the step that stops the guessing. It walks the repo and writes a standing model of it to `.claude/project/signals.md`: the framework, the build and test and lint commands, the languages, and a map of which directories form which feature. Claude reads that model before it reads your code, so a new session knows your stack instead of inventing `npm` scripts that do not exist. Ship commands refresh the model as the repo changes, so you do not hand-maintain it.

After this step, ask Claude something about the project. It answers from the signals model rather than from a guess.


## Step 3 — Run your first task


When you are not sure which command fits a situation, ask the router:

```text
/atomic-help
```

It reads your git state and recommends one next command. For the full map of what exists, pass it a topic (`/atomic-help tour`).

For real work, you have two paths.

**Stay in the loop.** Plan, implement, and ship as separate commands, each with its own approval gate:

```text
/atomic-plan add rate limiting to the login endpoint
/subagent-implementation @docs/spec/rate-limiting.md
/commit pr
```

`/atomic-plan` writes a spec. `/subagent-implementation` runs an implement-review loop with test-first subagents and commits each green checkpoint. `/commit pr` commits and opens the PR (or `/commit` alone to commit without pushing).

**Hand it off.** Give `/autopilot` a task or a GitHub issue number and it runs the whole lifecycle on its own. The only decision left to you is how to merge:

```text
/autopilot 142 commit squash merge
```

Start with the in-the-loop path while you build trust in the system, then reach for autopilot once you know how it works. The [workflow reference](/reference/workflow) covers both in depth.


## Step 4 — Index your code (optional)


Signals give Claude a prose map of the repo. The code-intel engine gives it a precise one: a symbol graph of every definition, call, and import. Build it once from the repo root:

```text
atomic code index
```

Once the index exists, the investigator, reviewer, and signals agents query the graph instead of grepping, and you can query it yourself from the terminal with no Claude involved. Indexing is opt-in and everything degrades to plain search when it is absent. See the [code intelligence reference](/reference/code-intel), including how to use it as a standalone CLI.


## Keeping atomic current


One command updates both the binary and the bundle:

```text
atomic update
```

This fetches the latest release, verifies its checksum, replaces the binary, runs a health check, and then refreshes the bundle (CLAUDE.md, agents, commands, skills, output styles, rules in `~/.claude/`) automatically. Use `atomic update --check` to see whether an update exists without applying it, or `atomic update --skip-claude-update` to update only the binary.

To refresh the bundle on its own, without touching the binary:

```text
atomic claude update
```

If you have edited your own `~/.claude/CLAUDE.md`, the update writes the new version to `~/.claude/.atomic/proposed/CLAUDE.md` and tells you to run `atomic prompt claude-merge` inside a subagent, which stages a merged result and preserves your changes. The full set of update flags and the merge flow are in the [install guide](/guides/install#updating).


## Where to go next


- [Concepts](/reference/concepts) — the ideas behind signals, wikis, the lifecycle, and the output style.
- [Workflow reference](/reference/workflow) — plan, implement, diagnose, and ship in detail.
- [Commands](/reference/commands), [skills](/reference/skills), [agents](/reference/agents) — the full reference tables.
- [Knowledge base guide](/guides/knowledge-base) — extend a folder of repos into a wiki that holds your non-code work too: tickets, research, and dumps.

Not sure where to begin in a given repo? Run `/atomic-help`. It reads your git state and points at one next command.

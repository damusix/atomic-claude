# Credits and comparison


Atomic Claude stands on the shoulders of three projects. This doc records what each one did well, what atomic-claude borrowed, and where it diverges. If atomic-claude's tradeoffs don't fit your style, one of these probably will.


## Credits


**[caveman](https://github.com/JuliusBrussee/caveman)** (Julius Brussee). 61k stars at time of writing, and earned. Caveman pioneered the compressed-output pattern for Claude Code and proved you can ship ~65% token savings without sacrificing technical accuracy. The intensity-level naming (lite / full / ultra) atomic-claude uses comes straight from there. Install it if its style fits you. Why this repo exists alongside it: caveman's voice is ooga-booga by design, and I wanted something that read more like a colleague. Full sentences when they help, diagrams and code blocks where they communicate better than prose, terse only where terseness wins.


**[superpowers](https://github.com/obra/superpowers)** (Jesse Vincent / obra). The most comprehensive skill toolkit for Claude Code I've used. The TDD discipline, verification-before-completion, subagent-driven development, and worktree workflows that atomic-claude leans on are all superpowers territory. It's the right answer for a lot of workflows. Why this repo exists alongside it: superpowers leans hard on auto-firing skills by design. `brainstorming` will kick in and start drafting a design spec on a single offhand comment. That's the intended UX, and for some flows it's perfect; for what I wanted, it was overbearing. Atomic-claude keeps the same disciplines but moves most of them into explicit slash commands you reach for on purpose.


**[stop-slop](https://github.com/hardikpandya/stop-slop)** (Hardik Pandya, MIT). The rule set behind `atomic-prose`. Stop-slop is a focused skill for removing predictable AI patterns from prose (throat-clearing, em dashes, marketing jargon, false agency). Atomic Claude's `atomic-prose` skill adapts those rules for developer documentation: kept the anti-marketing core, the active-voice requirement, and the no-em-dash rule; dropped essay-targeted guidance (manufactured profundity, performative sincerity); added a boundary against the atomic TUI style and a "keep some narrative" rule so doc prose does not collapse into telegraphic fragments. If you write blog posts or essays as well as docs, run stop-slop for the broader rule set.


Both caveman and superpowers are worth running on their own terms.


## Comparison with caveman and superpowers


Grouped by capability, not by where each project files it.


| Capability | Atomic Claude | Superpowers | Caveman |
|------------|---------------|-------------|---------|
| Output compression / tone | `output-styles/atomic.md` (lite / full / ultra) | — | `/caveman` (lite / full / ultra / wenyan) |
| TDD enforcement | `atomic-tdd` skill | `test-driven-development` skill | — |
| Verify before claiming done | `atomic-verify` skill | `verification-before-completion` skill | — |
| Systematic debugging | `atomic-debug` skill | `systematic-debugging` skill | — |
| Commit-message format | `atomic-commit` skill | — | `/caveman-commit` |
| Code-review tone | `atomic-review` skill | `requesting-code-review`, `receiving-code-review` | `/caveman-review` |
| Narrative-doc voice (README, guides) | `atomic-prose` skill | — | — |
| Brainstorm / plan | `/atomic-plan` (one verb, picks design vs spec) | `brainstorming`, `writing-plans` (split) | — |
| Execute a plan | `/subagent-implementation` | `executing-plans`, `subagent-driven-development` | — |
| Investigate and fix a failure | `/subagent-diagnose <ci\|bug> [args]` | — | — |
| Parallel subagents | `atomic-builder` / `atomic-surgeon` / `atomic-investigator` / `atomic-reviewer` | `dispatching-parallel-agents` skill | `cavecrew-*` (investigator / builder / reviewer) |
| Heavyweight reasoning over plans / problems | `atomic-strategist` (opus, read-only) | — | — |
| Worktree isolation | `/worktree-start` | `using-git-worktrees` skill | — |
| Ship a branch | `/commit-only`, `/commit-and-push`, `/commit-and-pr`, `/merge-to-main`, `/squash-and-merge`, … (10 verbs) | `finishing-a-development-branch` skill | — |
| Compress a markdown file | `/atomic-compress <file>` | — | `/caveman-compress <file>` |
| Project signal scanning | `/refresh-signals` + `atomic` binary + `atomic-signals` skill | — | — |
| Artifact linting | `atomic validate [spec\|config\|bundle] [paths...]` | — | — |
| Cron-backed reminders | `/remind-me`, `/follow-up` | — | — |
| CI observation | `/watch-ci` | — | — |
| Stale git cleanup | `/git-cleanup` + `atomic-git-scout` agent | — | — |
| Bootstrap a fresh repo | `/atomic-setup` | — | — |
| Help when lost in the workflow | `/atomic-help [topic\|intent]` | — | — |
| Token usage stats | — | — | `/caveman-stats` |
| MCP middleware compression | — | — | `caveman-shrink` (npm) |
| Meta: write a new skill | — | `writing-skills` skill | — |


A few honest notes:


- **Atomic borrows visibly from both.** The intensity-level naming (lite / full / ultra) is straight from caveman. The skills + agents split, TDD discipline, verification gate, and worktree workflow are superpowers territory.
- **Atomic adds project-state awareness and a durable workflow.** Signals scanning, the `atomic` binary, reminders, CI watching, git cleanup, and the spec → implement → review loop with a `FOLLOWUPS.md` ledger are atomic-specific.
- **Atomic is more opinionated about explicit vs implicit.** Superpowers leans on auto-firing skills (it's the design point); atomic uses skills sparingly (6 of them) and pushes most behavior into explicit slash commands. Caveman is mixed: `/caveman` is a command but also auto-activates per session for supported agents.

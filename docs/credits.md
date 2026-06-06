# Credits

Atomic Claude stands on the shoulders of several projects and ideas. If atomic-claude's tradeoffs don't fit your style, one of these probably will.


## Inspirations

Two ideas shaped how atomic-claude thinks about project context and `CLAUDE.md` design.

**[Interpretable Context Methodology: Folder Structure as Agentic Architecture](https://arxiv.org/abs/2603.16021)** by Jake Van Clief and David McDermott. This paper introduces the Model Workspace Protocol — the idea that filesystem structure itself can orchestrate AI agent workflows. Numbered folders represent stages, markdown files carry prompts and context, and a single agent follows the structure instead of relying on complex multi-agent coordination code. The signals workflow in atomic-claude draws directly from this: the inferrer reads a project's folder structure and derives meaning from it, turning your repo's layout into the context that steers Claude's behavior.

**[Andrej Karpathy's LLM Knowledge Bases](https://x.com/karpathy/status/2039805659525644595)** — Karpathy described a workflow where raw source material (articles, papers, repos) gets "compiled" by an LLM into a markdown wiki — `.md` files in a directory structure, with summaries, backlinks, concept articles, and auto-maintained index files. The LLM writes and maintains the wiki; you rarely touch it directly. Once the wiki is big enough, you can ask the agent complex questions against it and it researches the answers by reading the files. No fancy RAG needed — the LLM handles discovery through its own index files at reasonable scale.

The signals workflow in atomic-claude is this pattern applied to codebases. The deterministic scan produces the raw data. The inferrer "compiles" it into a structured wiki (`signals.md` + domain files) with summaries, cross-references, and an index. Claude reads the wiki every session instead of guessing your framework, build commands, or project structure. The wiki auto-refreshes on source changes. You rarely edit it by hand — it is the domain of the LLM.

Atomic-claude's project wikis apply the same pattern at the next scale up, and closer to Karpathy's original framing of a knowledge base over many sources. `/refresh-wiki` compiles a whole realm of repositories into a markdown knowledge base — per-repo summaries, cross-cutting concern articles, and an auto-maintained index — that Claude reads to answer questions spanning the realm.

**[Karpathy-inspired CLAUDE.md principles](https://github.com/multica-ai/andrej-karpathy-skills)** by multica-ai — distilled Karpathy's observations about LLM coding mistakes into four rules (Think Before Coding, Simplicity First, Surgical Changes, Goal-Driven Execution) packaged as a `CLAUDE.md`. The principles section in atomic-claude's `CLAUDE.md` descends directly from these four rules.

These inspirations, along with various community contributions found on Reddit, led to the core design: project context should be derived from the repo itself (signals), behavioral guidance should live in structured markdown files (`CLAUDE.md`), and the agent should read the filesystem rather than relying on its training data to guess.


## caveman

**[caveman](https://github.com/JuliusBrussee/caveman)** by Julius Brussee. 61k stars at time of writing, and earned. Caveman pioneered the compressed-output pattern for Claude Code and proved you can ship ~65% token savings without sacrificing technical accuracy. The intensity-level naming (lite / full / ultra) comes straight from there.

Why this repo exists alongside it: caveman's voice is ooga-booga by design. I wanted something that read more like a colleague — full sentences when they help, diagrams where they communicate better than prose, terse only where terseness wins.


## superpowers

**[superpowers](https://github.com/obra/superpowers)** by Jesse Vincent. The most comprehensive skill toolkit for Claude Code available. TDD discipline, verification-before-completion, subagent-driven development, and worktree workflows — atomic-claude leans on all of these and they are superpowers territory.

Why this repo exists alongside it: superpowers leans hard on auto-firing skills by design. `brainstorming` will kick in and start drafting a design spec on a single offhand comment. That's the intended UX, and for some flows it is perfect. For what I wanted, it was overbearing. Atomic-claude keeps the same disciplines but moves most of them into explicit slash commands you reach for on purpose.


## stop-slop

**[stop-slop](https://github.com/hardikpandya/stop-slop)** by Hardik Pandya (MIT). The rule set behind `atomic-prose`. A focused skill for removing predictable AI patterns from prose — throat-clearing, em dashes, marketing jargon, false agency. Atomic-claude adapted those rules for developer documentation: kept the anti-marketing core and active-voice requirement, dropped essay-targeted guidance, and added boundaries so doc prose does not collapse into telegraphic fragments. If you write blog posts or essays as well as docs, run stop-slop for the broader rule set.


## claude-improve

**[claude-improve](https://github.com/TerenceBristol/claude-improve)** by Terence Bristol. The retrospective-audit pattern behind `/atomic-improve`. claude-improve introduced the idea of treating a Claude session as a corpus to be mined — scanning `.jsonl` session history for corrections/praise/friction, cross-referencing against installed artifacts, and proposing targeted improvements one at a time with Accept/Reject/Modify. The enforcement-gap-to-hook conversion (turn a repeatedly-violated advisory rule into a deterministic `PreToolUse` gate) and the prior-run audit (verify whether past accepts actually landed) are both lifted directly from it.

Why this repo's version exists alongside it: claude-improve is a standalone skill targeted at any Claude Code setup. `/atomic-improve` adapts the same pipeline to atomic-claude's primitives — reusing `atomic-investigator`, `atomic-haiku`, and `atomic-strategist` for the parallel scans, storing run logs in `~/.claude/.atomic/improve-runs/` instead of a flat learnings file, and following the indexed-selection axiom for finding presentation instead of paginated `AskUserQuestion`.


## Comparison

Grouped by capability. Atomic borrows visibly from both caveman and superpowers.

### Output and tone

| Capability | Atomic Claude | Superpowers | Caveman |
|------------|:---:|:---:|:---:|
| Output compression | ✓ | — | ✓ |
| Commit message format | ✓ | — | ✓ |
| Code review tone | ✓ | ✓ | ✓ |
| Narrative doc voice | ✓ | — | — |
| Compress a markdown file | ✓ | — | ✓ |

### Engineering discipline

| Capability | Atomic Claude | Superpowers | Caveman |
|------------|:---:|:---:|:---:|
| TDD enforcement | ✓ | ✓ | — |
| Verify before claiming done | ✓ | ✓ | — |
| Systematic debugging | ✓ | ✓ | — |

### Workflow

| Capability | Atomic Claude | Superpowers | Caveman |
|------------|:---:|:---:|:---:|
| Plan and spec | ✓ | ✓ | — |
| Autonomous implementation loop | ✓ | ✓ | — |
| Investigate and fix failures | ✓ | — | — |
| Parallel subagents | ✓ | ✓ | ✓ |
| Heavyweight reasoning (Opus) | ✓ | — | — |
| Worktree isolation | ✓ | ✓ | — |
| Ship verbs (10 variants) | ✓ | ✓ | — |

### Tooling

| Capability | Atomic Claude | Superpowers | Caveman |
|------------|:---:|:---:|:---:|
| Project signal scanning | ✓ | — | — |
| Artifact linting | ✓ | — | — |
| Cron-backed reminders | ✓ | — | — |
| CI observation | ✓ | — | — |
| Stale git cleanup | ✓ | — | — |
| Bootstrap a fresh repo | ✓ | — | — |
| Help when lost | ✓ | — | — |
| Token usage stats | — | — | ✓ |
| MCP middleware compression | — | — | ✓ |
| Write a new skill (meta) | — | ✓ | — |


## Honest notes

- **Atomic borrows visibly from both.** The intensity naming is straight from caveman. The skills-plus-agents split, TDD discipline, verification gate, and worktree workflow are superpowers territory.
- **Atomic adds project-state awareness and a durable workflow.** Signals scanning, the `atomic` binary, reminders, CI watching, git cleanup, and the spec-to-ship loop with a follow-ups ledger are atomic-specific.
- **Atomic is more opinionated about explicit vs. implicit.** Superpowers leans on auto-firing skills (that is the design point). Atomic uses skills sparingly and pushes most behavior into explicit slash commands. Caveman is mixed: `/caveman` is a command but also auto-activates per session.

Both caveman and superpowers are worth running on their own terms.

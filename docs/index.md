---
layout: home
hero:
    name: Atomic Claude
    text: "A local code graph that grounds loops and wikis."
    tagline: "31 languages, plus SQL lineage across Snowflake, dbt, and T-SQL. Local, free, MIT, never uploaded."
    actions:
        - theme: brand
          text: Get Started
          link: /guides/install
        - theme: alt
          text: How it works
          link: /reference/concepts
        - theme: alt
          text: GitHub
          link: https://github.com/damusix/atomic-claude
features:
    - icon: "\uE522"
      title: A code wiki your agent compiles
      details: "Karpathy's LLM-wiki pattern at repo scale. Point an LLM at source material and it compiles a markdown wiki (per-area summaries, concept pages, an auto-maintained index) that it maintains itself and then reads instead of re-deriving the same answers. One scan builds that model of your codebase: framework, build and test commands, and a domain map of what lives where. Claude reads it before it reads your code, and ship commands keep it fresh."
    - icon: "\uF542"
      title: A knowledge realm across repos
      details: "A repo wiki maps one repo; a realm wiki maps a folder of them: services, libraries, or client projects, and how they relate. It is a working build of Karpathy's pattern one scale up, conforming to Google's Open Knowledge Format. /refresh-wiki points at the repos that already have a wiki, summarizes the ones that don't without touching them, and writes up the concerns they share. Capture buckets fold loose notes, research, and tickets into the same layer."
    - icon: "\uE4E2"
      title: See what Claude sees
      details: "`atomic serve` opens the maps Claude navigates (wiki concepts and the code graph) as a browsable site on localhost. The Open Knowledge Format in practice for your repo: pages with a live right rail, a whole-system view colored by concept type, federated code search, and a source viewer wired to the code graph. Read-only, no auth, nothing leaves your machine."
    - icon: "\uF0E8"
      title: A code graph that speaks SQL
      details: "One command parses your repo into a symbol graph across 31 languages and 23 web frameworks, no compiler required: definitions, callers, call sites, and the blast radius of any change. Claude queries the graph instead of grepping, so it spends tokens on the change, not on rediscovering your repo. SQL is a first-class citizen of that graph: Snowflake lineage (task DAGs, streams, stages, and COPY INTO), the dbt ref/source DAG, and T-SQL stored-procedure lineage down to the column, all static with no database connection. It even pulls SQL out of string literals in your Go, Python, and TypeScript, across 20 host languages. The enterprise tools that come close cost six figures a year."
    - icon: "\uF086"
      title: Channels for your agents
      details: "`atomic bus` gives concurrent Claude sessions named rooms to talk in. A session joins under its position (realm, repo, role) and receives its peers' messages as prompts. Every message carries an addressee list, which is what keeps a room of agents from answering each other's status updates forever: named means act, addressed to nobody means note it and move on. Watch, speak into, halt, or close any room from your own terminal."
    - icon: "\uF5B0"
      title: Autopilot, task to PR, hands-off
      details: "Hand it a description or a GitHub issue number. It plans, implements with test-first subagents, checks blast radius against the code graph before each change, and reviews its own diff in a fresh context that never saw the reasoning. Grounded in the graph the whole way, not another autonomous agent guessing from grep. The only decision left to you is how to merge."
---

<div class="vp-doc home-extra">

## Loop engineering, in one workshop

<div class="home-video">
<iframe
    src="https://www.youtube-nocookie.com/embed/mR-WAvEPRwE"
    title="Anthropic Workshop: Build Agents That Run for Hours"
    loading="lazy"
    allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share"
    referrerpolicy="strict-origin-when-cross-origin"
    allowfullscreen></iframe>
</div>

Anthropic's workshop on building agents that run for hours. The loop it describes (find the work, hand it to the agent, check the result, record state, decide the next move) is the loop this config is built around. The pieces it names map straight onto what installs:

| The loop needs | Atomic Claude ships |
| --- | --- |
| An automation that drives the work | `/autopilot` runs plan → implement → review → ship hands-off; ship verbs refresh signals on every commit |
| A skill that carries project context | Signals: a standing repo model Claude reads before your code, kept fresh automatically |
| A maker and a separate checker | `atomic-implementer` writes; `atomic-reviewer` re-runs the tests and gates the diff. The author never grades its own homework |
| A state file that survives the session | `signals.md`, the scratchpad `STATE.md`, and committed follow-ups hold what's done and what's next |
| An objective gate, not an opinion | `atomic-tdd` (failing test first) and `atomic-verify` (no "done" without a fresh run) |
| Worktrees for parallel work without collisions | The implement loop isolates each branch under `.claude/worktrees/` |

The video explains the pattern. This config is that pattern.

## Run your first grounded loop

You have seen what it is and the pattern it runs. The next step is to feel it on your own repo: turn on the output style, index your code, and hand the loop a real task, from install to a first merged change.

<div class="home-cta">

[Run your first task →](/guides/getting-started)

</div>

</div>

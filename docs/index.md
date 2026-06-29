---
layout: home
hero:
    name: Atomic Claude
    text: "A local code graph that grounds loops and wikis."
    tagline: "31 languages, plus SQL lineage across T-SQL, Snowflake, and dbt. Local, free, MIT, never uploaded."
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
      title: A Karpathy-inspired repo explorer
      details: One scan and Claude builds a standing model of your codebase, covering framework, build and test commands, and a domain map of what lives where. It reads that before it reads your code, and ship commands keep it fresh.
    - icon: "\uF542"
      title: A cross-repo knowledge layer
      details: "Signals map one repo; a wiki maps a realm of them: a folder of services, libraries, or client projects and how they relate. /refresh-wiki points at the repos that already have signals, summarizes the ones that don't without touching them, and writes up the concerns they share."
    - icon: "\uF5B0"
      title: Autopilot, task to PR, hands-off
      details: Hand it a description or a GitHub issue number. It plans, implements with test-first subagents, reviews its own diff, and ships. The only decision left to you is how to merge.
    - icon: "\uF5DC"
      title: A config that learns from you
      details: After a rough session, /atomic-improve mines your history for friction, corrections, and misbehavior, then proposes fixes to your own skills and rules. The setup gets sharper the more you use it.
    - icon: "\uF0E8"
      title: A queryable map of your code
      details: "One command parses your repo into a symbol graph across 31 languages and 23 web frameworks, no compiler required: definitions, callers, call sites, and the blast radius of any change. SQL is included, graphed from .sql across Postgres, MySQL, and T-SQL. Claude queries the graph instead of grepping."
    - icon: "\uE4E2"
      title: See what Claude sees
      details: "`atomic serve` opens the maps Claude navigates (wiki concepts and the code graph) as a browsable site on localhost. The Open Knowledge Format in practice for your repo: pages with a live right rail, a whole-system view colored by concept type, federated code search, and a source viewer wired to the code graph. Read-only, no auth, nothing leaves your machine."
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
| Worktrees for parallel work without collisions | The implement loop isolates each branch under `.worktrees/` |

The video explains the pattern. This config is that pattern.

</div>

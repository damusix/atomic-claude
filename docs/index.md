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
      details: "One command parses your repo into a symbol graph across 31 languages and 23 web frameworks, no compiler required: definitions, callers, call sites, and the blast radius of any change. Claude queries the graph instead of grepping. SQL is a first-class citizen of that graph: Snowflake lineage, the dbt ref/source DAG, and T-SQL stored-procedure lineage down to the column, all static with no database connection, plus SQL pulled out of string literals across 20 host languages."
    - icon: "\uF086"
      title: Channels for your agents
      details: "`atomic bus` gives concurrent Claude sessions named rooms to talk in. A session joins under its position (realm, repo, role) and receives its peers' messages as prompts. Every message carries an addressee list, which is what keeps a room of agents from answering each other's status updates forever: named means act, addressed to nobody means note it and move on. Watch, speak into, halt, or close any room from your own terminal."
    - icon: "\uF5B0"
      title: Autopilot, task to PR, hands-off
      details: "Hand it a description or a GitHub issue number. It plans, implements with test-first subagents, checks blast radius against the code graph before each change, and reviews its own diff in a fresh context that never saw the reasoning. Grounded in the graph the whole way, not another autonomous agent guessing from grep. The only decision left to you is how to merge."
---

<div class="vp-doc home-extra">

## Run your first grounded loop

The next step is your own repo. Turn on the output style, index your code, and hand the loop a real task, from install to a first merged change.

<div class="home-cta">

[Run your first task →](/guides/getting-started)

</div>

</div>

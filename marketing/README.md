# Atomic Claude marketing plan

How to sell a product that does too much, when you are an engineer and not a marketer.

This folder is the output of a competitive-research + positioning exercise (4 research agents, 3 debate agents, 1 judge). Nothing here is committed. It is yours to edit, reject, or ship.


## The one-paragraph answer

You have a positioning problem, not a feature problem. The features all serve one thing: Claude Code starts every session already understanding your codebase, instead of starting blind. The through-line is one word: **grounding.** Everyone is shipping agent loops and LLM wikis right now, and most run on grep, embeddings, and vibes, so they guess. Atomic grounds them in a real **code graph** (the one capability nobody else in the Claude Code ecosystem has), proves it is serious with **SQL lineage** (the part enterprise tools charge 60K-500K/year for and still cannot fully do), and demos it with the **autopilot loop** (the part that wows in one session). Lead with the graph, sell it through the grounded loops and wikis, and do not launch the whole monster at once. Reveal it one hook at a time.


## What the research found (the short version)

- **The code graph is unoccupied territory.** Every commercial code-intelligence tool (Sourcegraph, Cursor, Greptile, Augment) requires uploading your code to their cloud. The one credible local call-graph tool, Bloop, was archived in January 2025. Nobody in the Claude Code ecosystem ships a real symbol graph at all.
- **The SQL story is the strongest credibility signal you have.** No free tool does T-SQL stored-procedure lineage. No tool at any price extracts SQL from string literals inside application code (a market gap verified by search, not a cited paper). The enterprise tools that come closest cost a house per year.
- **The autopilot loop is the payoff, not a bare claim.** "Autonomous coding agent" is the most saturated, fatigued category in dev tooling, so the loop cannot stand alone. But it is not a throwaway demo either: it is *what the graph is for*. The graph grounds the loop (safe edits, against real structure) and makes it efficient (tokens on the change, not on rediscovery). Frame it as "a grounded engineering discipline," never as "another agent." The graph and the loop are one product, sold under one word: grounded.
- **The Karpathy / OKF angle is real and timely, not bandwagoning.** Karpathy published the LLM-wiki pattern (April 2026) and joined Anthropic (May 2026). Google published the Open Knowledge Format spec (June 12, 2026). Your wiki feature is a genuine, working implementation of both. This is prior art you can point to.
- **Hacker News is your single highest-leverage shot,** but only with a technically specific title. Outcome language ("memory for Claude") dies on HN; "local code graph with SQL lineage" survives.


## The files

| File | What it is |
|------|------------|
| [the-system.md](the-system.md) | What Atomic actually is: the graph-to-loop-to-efficiency spine. The answer to "what is this" when "code graph" under-claims and "holistic config" is soup. |
| [positioning.md](positioning.md) | The core decision: anchor, wedge, hook, and the layered messaging hierarchy. Read this first. |
| [pitch.md](pitch.md) | Ready-to-use copy: hero line options, one-liners, elevator pitch, taglines, channel-specific leads. |
| [competitive-landscape.md](competitive-landscape.md) | The evidence: who the competitors are, what they cost, the exact gaps you fill, with sources. |
| [launch-plan.md](launch-plan.md) | The serial-reveal sequence, channel playbook, the one wow demo, case studies, pitfalls, a checklist. |


## How to use this

1. Read `positioning.md`. Decide if the anchor/wedge/hook split feels right to your taste. Adjust the hero line.
2. Skim `competitive-landscape.md` so the numbers are in your head. They are your strongest ammunition.
3. When you are ready to launch, work `launch-plan.md` top to bottom. It is a checklist, not an essay.
4. `pitch.md` is the copy library. Pull lines from it as needed.

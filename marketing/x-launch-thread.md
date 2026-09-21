# X launch thread

Built around the grounding frame. Lead with loop fatigue (the contrarian hook), land the SQL moat mid-thread, end honest. No hype, no exclamation marks, no em dashes. Each tweet is under 280 chars so it stays shareable; merge 8+9 if you post with Premium.

The single most important asset is the 45-second demo GIF on tweet 1. Without it the thread underperforms. See `launch-plan.md` for the three-beat recording spec.


## The thread

**1/ (hook + demo GIF)**

> Your coding agent doesn't actually know your codebase.
>
> It greps. It embeds. It guesses.
>
> So I built the missing piece: a local code graph that grounds it. The loop and the wiki finally check real structure before they act.
>
> Free, MIT, never leaves your machine.

**2/ (what "grounded" means)**

> "Grounded" isn't a buzzword. It's the literal fix for hallucination.
>
> The loop checks the real call graph before it edits.
> The reviewer verifies against structure in a fresh context, so it can't rubber-stamp itself.
> The wiki knows your call graph, not just your prose.

**3/ (architecture, the credibility beat)**

> How: one command parses your repo with tree-sitter, through a wazero WASM binding, into a local SQLite symbol graph.
>
> No compiler. No daemon. No cloud.
>
> atomic code explore "how does billing work"
> → callers, callees, blast radius, in seconds.

**4/ (the SQL moat)**

> The part I'm proudest of: SQL is first-class in the graph.
>
> T-SQL stored-proc lineage: temp tables, OUTPUT INTO, PIVOT/UNPIVOT, column-level.
> dbt ref()/source(). Snowflake tasks and streams.
>
> All static. No database connection.

**5/ (embedded SQL + price gap, the "I didn't know any tool did this" beat)**

> It even pulls SQL out of string literals inside your Go, Python, and TypeScript, and graphs that too.
>
> I couldn't find another tool, free or paid, that does this.
>
> The enterprise tools that come closest (Collibra, Manta) run $150K to $500K a year. This is $0.

**6/ (wiki + OKF + Karpathy, the zeitgeist ticket)**

> The cross-repo wiki is a working build of Karpathy's LLM-wiki pattern, and it already conforms to Google's Open Knowledge Format (June 2026).
>
> Your repos compile into a knowledge base that maintains itself.
>
> atomic serve renders it as a navigable graph.

**7/ (the loop, grounded, the payoff)**

> Put it together:
>
> /autopilot 142
>
> Reads the issue. Writes a spec. Writes the failing test first. Implements. Reviews its own diff in a fresh context. Opens the PR.
>
> Grounded in the graph the whole way. Your only call is how to merge.

**8/ (honest limit, the trust beat)**

> Honest limit: lineage is only as good as static parsing. Very dynamic SQL, string-built queries, ALTER PROCEDURE bodies, can be missed.
>
> I'd rather tell you that up front than oversell it.

**9/ (CTA)**

> MIT. Local. Two commands to install.
>
> I built it because I was tired of re-explaining my repo to Claude every session.
>
> Repo and docs: [LINK]
>
> Honest feedback welcome, especially the critical kind.


## Alternate hook tweets (tweet 1 is the highest-leverage; pick by taste)

**A — graph-first, calmer:**

> A local code graph that grounds Claude Code's loops and wikis.
>
> Everyone's shipping agent loops and AI wikis. Most run on grep and embeddings, so they guess. This one checks the real structure first.
>
> Free, MIT, on your machine.

**B — SQL wow first (lead with the demo's jaw-drop moment):**

> My coding agent just traced a SQL query from a string literal in my Go code all the way to the database column it touches.
>
> Locally. For free. I couldn't find another tool that does this.
>
> Here's the code graph that makes it possible.

Recommendation: the main hook (loop fatigue) for the broad AI-coding audience; hook B if you want the moat to be the scroll-stopper and are targeting a more senior/backend crowd.


## Optional outreach (do these as replies or quote-tweets AFTER the thread gets traction, never @-mention in tweet 1)

Mentioning people in the lead tweet reads as begging and HN/X both punish it. Earn the mention with the thread first, then reply with a specific, genuine connection:

- **@karpathy** — "This is basically your LLM-wiki pattern aimed at codebases: sources = repo files, wiki = synthesized pages, schema = project signals. Conforms to Google's OKF too."
- **@simonw** — "Local-first, no cloud upload. The whole graph lives in a SQLite file you can open and inspect. Same bet as Datasette."
- **@swyx** — "The code graph is exposed as an MCP server, so it grounds any agent, not just Claude Code."

One genuine mention beats ten cold ones. Pick the one whose work the thread most honestly connects to.


## Posting notes

- Attach the demo GIF to tweet 1 (or hook B). This is non-negotiable; it is what gets retweeted.
- Post simultaneously with the Show HN launch: Tuesday to Thursday, 9am to 12pm ET.
- Link only in the final tweet and pin tweet 1, so the link does not suppress early reach.
- Reply to your own thread within the first hour with one extra detail (a second GIF of `atomic serve`, or the T-SQL fixture result) to keep it surfacing.
- Drop the 🧵 thread emoji on tweet 1 if you want the convention; leave it off if it reads as noise to you. Your call.

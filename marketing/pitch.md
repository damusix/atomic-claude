# Pitch copy

A library of ready-to-use lines. Pull from it; do not write from scratch under launch pressure. Edit to taste.


## Hero line (README H1)

Recommended (graph as subject, rides loops + wikis through the literal "grounds"):

> **A local code graph that grounds Claude Code's loops and wikis. Free, MIT.**

Alternates (see `positioning.md` for the taste rationale):

- Code-graph-grounded loops and wikis for Claude Code. Local, free, MIT. (hot words first; X + awesome-claude-code)
- A local code graph for Claude Code. Free, MIT, never uploaded. (privacy-forward)
- Give Claude Code a real map of your codebase, including your SQL. (teases the wedge)
- A code-intel brain for Claude Code, running on your machine. (social only)


## Sub-message (under the hero)

> Atomic Claude indexes your repo into a local symbol graph (31 languages, plus SQL lineage across T-SQL, Snowflake, and dbt, and even SQL buried in your app code) so Claude starts every session knowing how your system fits together, then runs a plan-implement-review-ship loop against that structure instead of grep and guesswork.


## The grounding line (the through-line; use everywhere)

> Agent loops and wikis are everywhere now. Most run on grep, embeddings, and vibes, so they guess. Atomic Claude grounds them in a real code graph: local, SQL-aware, free.

That is the whole pitch in three sentences. It names the trend, names why the trend disappoints, and names the fix, with the moat (graph + SQL + local + free) as the fix.


## One-liners (for bios, list entries, tweets)

- Code-graph-grounded loops and wikis for Claude Code. Local, SQL-aware, free.
- A local code graph for Claude Code: callers, callees, blast radius, and SQL lineage. Free and MIT.
- The only Claude Code companion with a real code-intel graph (tree-sitter + SQLite, not grep).
- Static SQL lineage across T-SQL, Snowflake, and dbt, free, with no database connection.
- Code intelligence that never leaves your machine.
- Issue to merged PR, hands-off, on a code graph that actually understands your repo.


## Elevator pitch (30 seconds, spoken)

By default Claude Code starts every session blind to your repo. It does not know your build command, your framework, or how your code is laid out, so it guesses, and you correct the same guesses over and over. Atomic Claude fixes that with one local command: it parses your whole repo into a symbol graph, so Claude can ask "what calls this function" or "what breaks if I change it" and get a real answer, not a grep. It treats SQL as a first-class language, so it traces lineage through T-SQL stored procedures, dbt models, Snowflake tasks, even SQL strings hidden inside your application code, the kind of thing enterprise tools charge six figures a year for. Then it can run a feature from issue to merged PR on its own, checking blast radius before every change. It is free, MIT, and never uploads your code anywhere.


## The "why I built it" paragraph (for HN maker comment, dev.to intro)

Claude Code is great until it forgets your codebase, which is every new session. I got tired of re-explaining my repo and watching it grep around for structure it could have just known. So I built a code-intelligence engine: tree-sitter parsing through a WASM binding, stored in local SQLite, exposed to Claude as a queryable symbol graph. The part I am most proud of is the SQL: it indexes `.sql` files as first-class code, does T-SQL stored-procedure lineage (temp tables, OUTPUT INTO, PIVOT/UNPIVOT, column-level), follows the dbt DAG, and even pulls SQL out of string literals inside Go/Python/TypeScript. No database connection, all static, all local. It is MIT. Honest limitation: lineage is only as good as static parsing, so very dynamic SQL (string-built queries, ALTER PROCEDURE bodies) can be missed.


## Channel-specific leads

Different rooms, different doors. Same product.

**(a) Show HN title** (technical, features front, under 80 chars):

> Show HN: Atomic Claude – local code-intel graph (callers/callees/SQL-lineage, 31 langs) + agent loop for Claude Code

**(b) SQL / data-engineering audience** (r/dataengineering, r/SQLServer, dbt Slack, LinkedIn data communities):

> Free T-SQL stored-procedure lineage with temp-table scoping, OUTPUT INTO, and PIVOT/UNPIVOT, the kind of thing Collibra charges $170K/year for. Static, no DB connection, MIT.

No Claude framing needed here. The lineage capability stands alone. Mention it indexes SQL hidden in app code, that is the jaw-drop for this crowd.

**(c) Claude Code / AI-coding audience** (awesome-claude-code PR, r/ClaudeAI, X):

> Sourcegraph dropped its free tier. So I built a local code-intel graph for Claude Code, with SQL lineage and a plan-to-ship autonomous loop. MIT, zero cloud.

**(d) r/programming** (skeptical of AI promotion; lead with the engineering):

> I built a tree-sitter + WASM code graph and indexed it in SQLite, so I can query callers/callees/blast-radius locally without a compiler or a cloud upload. It also extracts SQL from string literals across 20 languages. Write-up + code inside.


## Taglines (pick one for consistency across surfaces)

- Grounded loops. Grounded wikis. One local code graph.
- Stop shipping loops that guess.
- Code intelligence that stays on your machine.
- The code graph Claude Code forgot to ship.
- Enterprise-grade SQL lineage, free and local.
- Stop re-explaining your repo to Claude.

The last one is already your README's sub-headline and tests well as the emotional hook. Keep it. "Grounded loops. Grounded wikis. One local code graph." is the new contender if you want the through-line as the tagline too.


## Proof-point soundbites (drop into any post)

- "I couldn't find any tool, free or paid, that extracts SQL from string literals in app code. Atomic does it for 20 languages."
- "SQLGlot throws ParseError on production T-SQL stored procedures. Atomic doesn't."
- "Sourcegraph killed its individual Cody tier in 2025; it's enterprise-only now ($59/user/mo). Atomic: $0, MIT, local."
- "Collibra technical lineage starts at $170K/year. This is free."
- "The reviewer runs in a fresh context, so it can't rubber-stamp the implementer's work."
- "Everyone's loop runs on grep. This one checks the real call graph before it edits a line."
- "A wiki that knows your call graph, not just your prose."
- "Grounding isn't a buzzword here. It's the literal reason the loop doesn't hallucinate your codebase."


## The efficiency angle (the ROI nobody else claims)

Most of the field sells correctness (less hallucination), capability (it can do X), or price ($0 vs six figures). Almost nobody sells the loop's own efficiency. That is your open lane.

> Every other agent loop re-discovers your repo by grepping, every single session. A grounded loop already knows it, so it spends tokens on the change, not on rediscovery, and converges in fewer passes.

- "An ungrounded loop pays the rediscovery tax on every run. A grounded one pays it once, at index time."
- "Fewer wrong turns, fewer review rounds, fewer tokens. Grounding is not just safer, it's cheaper."
- "The graph is not overhead. It's the thing that stops the loop from burning tokens finding what it could have known."

Honesty note: keep this claim mechanical, not numeric, until measured. There is an open follow-up to instrument the loop's token cost; once it lands, swap the qualitative line for the measured number, which turns a good claim into a killer one. Do not invent a percentage before then.


## What never to say

No exclamation marks. No "revolutionary," "game-changing," "10x," "supercharge." No em dashes in shipped copy. No claiming Karpathy or Google endorsed anything (they didn't). No "better than Superpowers." Let the verifiable numbers carry the weight.

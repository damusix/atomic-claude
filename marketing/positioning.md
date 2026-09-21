# Positioning

The single most important document. Everything else flows from this.


## The core problem, named honestly

Atomic Claude does at least six things well: a code graph, broad SQL/dbt/Snowflake lineage, cross-repo wikis in Google's Open Knowledge Format, a local graph visualizer, an autonomous plan-implement-review-ship loop, and a terse output style. That is a feature list, not a pitch. "A holistic Claude Code configuration" is a description nobody shares.

The fix is not to cut features. It is to find the one outcome all of them serve, lead with that, and reveal the rest in order. Three clean agents debated which feature to lead with. None won outright. The answer is a layering.


## The decision: anchor, wedge, hook

```
anchor (what it is) ...... a local code graph that gives Claude Code
                           a structural understanding of your codebase
                           that persists across sessions

wedge (why it's serious) . SQL lineage no free tool has, that enterprise
                           tools charge $60K-$500K/year to approximate

hook (why now) ........... Sourcegraph dropped its free/individual tier
                           in mid-2025. The local-first vacuum is real
                           and recent. You fill exactly what they left.
```

- **Anchor** = the long-form hero. Goes in the README H1, the landing page, the CLI `--help` preamble, every long-form post. It is the code graph, framed as an outcome.
- **Wedge** = the credibility proof. The SQL depth is what turns "another config file" into "a real product built by someone serious." It is a proof pillar, not the headline, because leading with it narrows the audience to data engineers, who are underrepresented on your best launch channel.
- **Hook** = the timing line. Use it when you need a reason-to-care-now: the Sourcegraph exit, the Karpathy/OKF wave.


## The through-line: "grounded"

There is a sharper way to say all of this in one word: **grounding.** In LLM terms, grounding means anchoring a model's output in verified facts instead of letting it guess. That is exactly what the code graph does for everything else Atomic ships, so it is a literal term, not a coined metaphor.

The two hottest things in dev-AI right now are agent loops and LLM wikis. Both are crowded, and both are fatigued for the same reason: they run on grep, embeddings, and vibes, so they hallucinate. "Code-graph-grounded" is the modifier that turns those crowded categories into a category of one:

```
agent loop          + graph grounding = checks blast radius before it edits;
                                         reviewer verifies against structure
LLM wiki            + graph grounding = a wiki that knows your real call graph,
(Karpathy / OKF)                         not just your prose
```

This is not bandwagoning. It is the opposite: you ride the wave by pointing out that everyone else on it is standing on sand. The substance (the graph) stays the subject; "grounded" is how you sell that substance through the categories people already want. It also answers loop fatigue in the first phrase: the reason their last loop burned them is that it was not grounded; this one is.

**Lean wiki harder than loop for "new thing" energy.** The wiki wave (Karpathy's pattern, April 2026; Google's Open Knowledge Format, June 2026) is fresher and less saturated than the agent-loop wave. So: graph is the moat, wiki is the zeitgeist ticket, loop is the demo. All three sold under one word.

**Caution:** "grounded" is native vocabulary for the AI-coding audience (your launch crowd) and reads fine on HN, but it is abstract for the data-engineering channel. Keep leading that channel with SQL, not grounding.


## Why not lead with each alternative

| Candidate lead | Strongest point | Why it cannot be the sole lead |
|----------------|-----------------|-------------------------------|
| **SQL lineage** | Most uniquely true claim in the product. Price moat is verified and enormous. No tool we could find, free or paid, extracts embedded SQL from source literals. | The audience that feels SQL pain (SQL Server data engineers) barely reads Hacker News at 9am Tuesday. It frames you as a data-catalog competitor (a slow procurement comparison), not a developer-adoption moment. |
| **Autopilot loop** | Shortest time-to-magic. Produces visible artifacts (spec, failing test, PR) in one session. The demo writes itself. | "Autonomous coding agent" is the most crowded, hype-fatigued category in 2026 (Devin, Cursor Composer, Copilot Workspace, Aider, Cline, Goose). It is also the least defensible claim: anyone with a system prompt and a for-loop can claim it. Leading with it buries the moat. |
| **Outcome ("memory for Claude")** | Correct message architecture. Every successful multi-feature tool anchors on outcome (Obsidian, Raycast, Linear, Cursor). | "Memory for Claude" is cognitively polluted: Claude Projects, Cursor's indexing, Repomix, and Gitingest all occupy adjacent claims. Without a technical qualifier the hero reads as a commodity, and the qualifier makes it too long for HN. |

The synthesis takes the outcome architecture (from the third), anchors it on the code graph (specific enough to stand apart), proves it with SQL (the moat), and demos it with the loop.

The "grounded" frame does not violate the loop verdict above. It never leads with a bare loop. It leads with the graph and uses "grounded" to make the loop and wiki claims defensible, which is exactly the move that defeats the saturation problem.


## The messaging hierarchy (the actual copy)

### Level 1 — hero line

Recommended (graph as subject, rides loops + wikis through "grounds"):

> **A local code graph that grounds Claude Code's loops and wikis. Free, MIT.**

Alternate that puts the hot words first (use where attention beats precision, e.g. X):

> **Code-graph-grounded loops and wikis for Claude Code. Local, free, MIT.**

Privacy-forward alternate (the original; use where "never uploaded" is the buying trigger):

> **A local code graph for Claude Code. Free, MIT, never uploaded.**

Sub-message (one sentence, sits directly under the hero):

> Atomic Claude indexes your repo into a local symbol graph (31 languages, plus SQL lineage across T-SQL, Snowflake, and dbt, and even SQL buried in your app code) so Claude starts every session knowing how your system actually fits together, then runs a plan-implement-review-ship loop against that structure instead of grep and guesswork.

That one sentence carries all three differentiators: the local graph (anchor), the SQL depth (wedge), the loop (use case). No bullet list above the fold.

### Level 2 — three proof pillars

**1. A code graph, on your machine (the anchor).**
One command builds a queryable symbol graph: callers, callees, blast radius, 31 languages, no compiler. `atomic code explore "how does billing work"` returns a digest with cross-file edges in seconds. Sourcegraph charges for this and dropped its individual tier in 2025; Cursor and Greptile upload your code to do it. Atomic runs locally and never sends your code anywhere.

**2. SQL nobody else graphs for free (the wedge).**
T-SQL stored-procedure lineage (temp tables, table variables, `OUTPUT INTO`, `PIVOT`/`UNPIVOT`, column-level alias resolution), plus dbt `ref()`/`source()`, Snowflake tasks/stages/streams, and SQL strings hidden inside Go/Python/TypeScript code. The only free tool that handles what SQLGlot misses. Collibra's technical lineage starts at $170K/year. Manta (IBM) is not cheaper.

**3. A grounded engineering discipline, not another autonomous agent (the payoff).**
This is the part the graph exists to power. `/autopilot 42` does not "let an agent loose." It runs a disciplined loop that behaves like a senior engineer: writes the failing test first, checks blast radius against the code graph before each change, then hands the diff to a reviewer in a fresh context that never saw the implementer's reasoning and so cannot rubber-stamp it. Issue to merged PR, hands-off. Two things follow from grounding the loop in real structure instead of grep: it is **safe** (the agent edits against the actual call graph, not a guess) and it is **efficient** (it spends tokens on the change, not on re-discovering your repo every session). "Autonomous coding agent" is a saturated category; "a loop grounded in a real code graph" is a category of one. The graph is the difference.

### Level 3 — the long tail

The OKF wiki, `atomic serve` visualization, the self-improving config, the output style, the doctor/validate/install surface. These live in the docs and in later launch posts (see `launch-plan.md`). Never in a hero message. They are what keeps users after the first win, not what earns the first click.


## What to stop saying

- "A holistic Claude Code configuration." Accurate, unshareable. It is the feature-soup death.
- "A better Superpowers." Superpowers owns the skills/config layer at 174K stars with first-mover advantage. Position around what it does not have (the graph, SQL, the binary), never as its replacement.
- "Permanent memory for Claude." Polluted. Use "a local code graph" or "structural understanding that persists across sessions."
- Anything with an exclamation mark, a superlative, or the word "revolutionary." Your audience punishes hype. The numbers do the selling.


## The taste check (hero line alternatives)

Pick by feel. All keep the graph as the anchor; they trade precision vs attention vs warmth.

1. **A local code graph that grounds Claude Code's loops and wikis. Free, MIT.** (recommended: graph-first, sells the moat through the hot categories via the literal "grounds")
2. Code-graph-grounded loops and wikis for Claude Code. Local, free, MIT. (hot words first; best on X and in the awesome-claude-code entry)
3. A local code graph for Claude Code. Free, MIT, never uploaded. (privacy + price in four words; use where "never uploaded" is the trigger)
4. Give Claude Code a real map of your codebase, including your SQL. (outcome-leaning, teases the wedge)
5. A code-intel brain for Claude Code, running on your machine. (warmer, social-only; "brain" is a metaphor, keep it off the README)

Recommendation: ship #1 on the README and docs; use #2 on X and the awesome-claude-code listing; pull #3 into the install/privacy section.

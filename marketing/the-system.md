# What Atomic actually is

The page to point at when someone asks "what is this." Not a hero line, not a feature list. The shape of the whole thing, so the parts stop reading as a grab-bag.


## The one-sentence answer

Atomic Claude is a grounded engineering discipline for Claude Code: a code graph on your machine that gives the agent real structural knowledge of your repo, and a plan-implement-review-ship loop that acts on that structure instead of grepping and guessing.

Two halves, one product. The graph is the grounding. The loop is the discipline. You do not buy a graph; you buy a loop that finally knows what it is editing.


## The spine

Everything in the product hangs off one causal chain. Read it in order and the feature list stops being soup.

```
grounding            the loop                 the payoff
(code graph)  ──►   (discipline)      ──►    (efficiency + trust)

a local symbol       plan, test-first,        the agent edits against
graph: callers,      blast-radius check,      real structure, not a
callees, blast       fresh-context review,    guess. fewer wrong turns,
radius, SQL          clean commit, current    fewer review rounds, tokens
lineage              docs, refreshed memory   spent on the change, not on
                                              rediscovering the repo
```

- **Grounding** is the engine room. It is the most defensible, demo-able, and genuinely unoccupied piece, so it leads. But an engine is not a car.

- **The loop** is what you actually run. It behaves like a senior engineer: writes the failing test before the code, checks blast radius against the graph before each change, then hands the diff to a reviewer in a fresh context that never saw the reasoning and so cannot rubber-stamp it.

- **The payoff** is the part nobody else in the field claims. Grounding does not only make the loop safe (it edits the real call graph). It makes the loop efficient. An ungrounded loop pays a rediscovery tax on every session, re-grepping structure it could have known. A grounded loop pays that cost once, at index time.


## Why the graph leads but is not the whole story

The lead is a positioning choice, not a claim that the graph is the product. The market has cars with lawnmower engines: agent loops running on grep and embeddings that look capable and quietly guess. In that market, leading with "a real engine" is the differentiator that survives scrutiny. So the graph is the lead noun.

But the marketing fails if it stops there. A graph nobody acts on is just a local Sourcegraph. The product is the welding of graph to loop, and the payoff of that weld is efficiency and trust. Lead with the graph; never let the reader forget it is the graph the loop runs on.


## The empty square

```
real graph, no loop ....   Sourcegraph, OpenGrok, Kythe, ctags/LSP
                           (you can query structure, nothing acts on it)

loop, no real graph ....   Devin, Cursor Composer, Cline, Goose
                           (it acts, on grep and embeddings, so it guesses)

atomic claude ..........   both, fused, local, free
```

Sourcegraph has the graph and no loop. Devin has the loop and no graph. Atomic is the only one that welds them, and the weld is the part that is hard to copy and easy to feel in one session.

One honest exception: Aider builds an ephemeral tree-sitter repo-map ranked by PageRank, so it is structural rather than grep. But it is rebuilt each session, file-level, token-capped, and has no SQL lineage. It is a per-session map, not a persistent queryable graph, so name it precisely rather than grouping it with the embedding tools.


## The long tail (real, but not the lead)

These are what keep users after the first win, not what earns the first click. They all sit on the same spine: each one is more useful because the graph grounds it.

- The cross-repo wiki (Karpathy's LLM-wiki pattern, conforming to Google's Open Knowledge Format): a knowledge base that knows your real call graph, not just your prose.

- `atomic serve`: the graph and wiki as a navigable, Obsidian-style local UI.

- The atomic output style: terse, clarity-first replies, so the agent stops padding.

- The discipline skills (test-first, verify-before-claim, debug, commit, review, documentation): the senior-engineer reflexes the loop runs on.

- The self-maintaining substrate: project signals that refresh as you ship, doctor and validate to keep the install honest, a config that improves itself.

Cohesion is the product. The graph is why the cohesion is trustworthy instead of vibes.


## How to say it in each room

- **What is it (general):** a grounded engineering discipline for Claude Code. A local code graph, and a loop that acts on it.

- **Why it is serious (credibility):** it graphs SQL no free tool touches, including SQL hidden inside application code, the kind of lineage enterprise tools charge six figures a year to approximate.

- **Why it is different (the loop):** every other agent loop runs on grep and guesses. This one checks the real call graph before it edits, so it is both safer and cheaper.

- **Why now (timing):** Sourcegraph dropped its individual tier in 2025. The local-first vacuum is real and recent. Atomic fills exactly what they left.

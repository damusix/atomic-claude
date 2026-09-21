# Launch plan

A checklist, not an essay. The strategy is **serial reveal**: one hook per post, each earning the credibility that makes the next hook land harder. Do not launch the whole monster at once.


## The one wow demo (build this first)

Everything depends on one asset: a 45-second terminal recording (use VHS from charm.sh or asciinema) on a real Go + PostgreSQL repo. Three beats:

```
beat 1 (10s) ... atomic code index
                 → indexer walks the repo, extracts SQL from string
                   literals inside .go files

beat 2 (15s) ... atomic code explore "how does the billing pipeline work"
                 → symbol digest: callers, callees, and a SQL lineage
                   edge from processPayment() to the orders table,
                   pulled out of an embedded SQL string

beat 3 (20s) ... atomic serve  (browser)
                 → Obsidian-style graph; billing pipeline as a node
                   cluster; click a node, follow a wikilink to the DB table
```

Why it goes viral: it shows three things no free tool does, all local, all in one session. The SQL edge from application code to a database table is the frame people screenshot with "I didn't know any tool did this." Record it on the atomic-claude repo itself for dogfood credibility. The browser graph is the retweetable still.

This asset blocks the launch. Make it before anything else.


## Channels, ranked by leverage

```
1. Hacker News (Show HN) ...... highest single-shot ROI. ~1.4 stars per
                                upvote. Needs 30-50 upvotes in hour one.
2. awesome-claude-code PR ..... 47,300-star list. Permanent discovery.
                                Most durable lever. Same week as HN.
3. X/Twitter thread ........... highest variance. One right retweet =
                                months of growth. Architecture thread.
4. GitHub README + demo ....... the conversion layer. 90% of arrivals
                                judge here. Demo GIF above the fold.
5. Reddit (r/ClaudeAI, .....   slower, community-validated. Lead with
   r/dataengineering,          the problem, promo last. Needs karma.
   r/programming, r/LocalLLaMA)
6. dev.to + Hashnode .......... SEO flywheel. Canonical back to your domain.
7. Product Hunt ............... secondary amplification for the visual
                                (atomic serve) reveal, not the primary.
```


## Hacker News playbook (the make-or-break)

- **Title:** `Show HN: Atomic Claude – local code-intel graph (callers/callees/SQL-lineage, 31 langs) + agent loop for Claude Code`
- **Timing:** Tuesday-Thursday, 9am-12pm ET. (Backup: Sunday 7pm ET, lower competition.)
- **Link to the GitHub repo, not a landing page.** For technical tools the repo is the landing page. Demo GIF above the fold.
- **Maker comment within 5 minutes:** your background, one sentence on what it does, why you built it (the re-explaining-your-repo pain), the tech stack (tree-sitter WASM + SQLite, not embeddings, not grep), **one honest limitation** (static parsing misses very dynamic SQL), and an explicit ask for feedback, not approval.
- **Stay in the thread**, replying within ~15 minutes, for the first 2-4 hours.
- **Personal account, not a brand account.**
- **Do not vote-brigade.** HN detects and penalizes it. Brief 10-30 genuinely interested people: "launching Wednesday, honest feedback in the thread helps." Not "please upvote."
- Miss the first-hour threshold and the post decays before the US wakes up. The brief matters.


## The serial-reveal calendar

| When | Beat | Channel | Hook |
|------|------|---------|------|
| Week -4 | Architecture post: "How I built a code-intel graph with tree-sitter WASM and SQLite in Go, and why I skipped embeddings" | Own domain + HN (as a blog post, not Show HN) | Pure engineering. Front-page bait. Builds credibility before anyone evaluates the product. |
| Week -2 | Polish first-run UX. Record the wow demo. Draft the awesome-claude-code PR. Write the HN maker comment. Brief 10-20 people. | Prep | No public post. |
| **Week 0** | **Show HN launch** + X architecture thread + awesome-claude-code PR, same day | HN, X, GitHub | The code graph. The main event. |
| Week +1 | "We built Karpathy's LLM-wiki pattern for codebases, and it conforms to Google's Open Knowledge Format" | dev.to, Hashnode, r/ClaudeAI, r/MachineLearning, HN (separate post) | The wiki / OKF angle. Different audience, fresh 50 upvotes. |
| Week +3 | "The loop that actually understands your codebase" + autopilot demo (5 visible artifacts) | HN, X, r/ClaudeAI | The loop, framed on the graph: "checks blast radius before every change, unlike grep-based agents." |
| Week +5 | "Free T-SQL stored-procedure lineage: what SQLGlot misses and how I solved it" | r/dataengineering, dbt Slack, DBMS-Tools, LinkedIn | The SQL wedge. No Claude framing. A standalone data-engineering article. A second inbound funnel. |
| Week +8 | "I built an Obsidian-style knowledge graph for codebases in Go" + `atomic serve` UI | Product Hunt, dev.to, Hashnode | The visualizer. The visual-first crowd. |
| Ongoing | One technical release note every 2-3 weeks on X | X | Not "fixed bugs." "v1.x: `atomic code callers` now resolves through interface implementations, not just direct calls." Each is a shareable claim. |

Each beat targets a *different* audience, so they do not cannibalize. The graph crowd, the AI-infra crowd, the agent crowd, the data crowd, the visual crowd. Five funnels, one product.


## Influencer outreach (earn it, don't beg it)

Ship something impressive, write the architecture thread, then @-mention 2-3 accounts with a *specific* connection. One thoughtful mention beats ten cold DMs.

- **Andrej Karpathy** (@karpathy): your wiki implements his LLM-wiki pattern; he is now at Anthropic. You cannot manufacture his attention, only make the connection genuine and visible. Do not name the feature after him or imply endorsement.
- **Simon Willison** (@simonw): writes long-form about tools he finds interesting; endorsed Superpowers. Local-first philosophy aligns with Datasette. Reachable via blog/email if the tool is genuinely novel.
- **swyx** (@swyx): AI-engineering audience. Angle: the code-graph MCP server wires into any agent, not just Claude Code.
- **DHH** (@dhh): reacts to developer-freedom narratives (drove 18K stars to OpenCode in two weeks). Your MIT license + no-lock-in + local-first is the natural counter to SaaS AI sprawl. Long shot, but the narrative fits.


## Case-study lessons (what actually worked for others)

- **Superpowers** (0 to 174K stars, 7 months): launched day-one of the Claude Code plugin system. **Timing wins categories.** Your timing gap: no one ships a real code graph yet. Be first.
- **OpenCode** (18K stars in 2 weeks): rode an external freedom-narrative moment (Anthropic blocking third-party tools). **You cannot engineer the moment, but position so you benefit when it comes.** MIT + local + no-lock-in is that posture.
- **Zed** (fastest-growing editor): one specific, verifiable claim ("the fastest," Rust + GPU) beat any feature list. **"The only Claude Code companion with a real code-intel graph (tree-sitter + SQLite, not grep)" is your verifiable claim.** "A holistic configuration" is not.
- **Aider** (46K stars, no viral moment): sustained power-user growth off SWE-bench credibility. **Document what your graph gets right that grep/ast-grep cannot** (the 29-edge T-SQL fixture, Haiku-verified, is exactly this kind of proof).
- **Karpathy's LLM Wiki** (idea > implementation): a Gist spawned dozens of builds because the concept was clearly right. **Your wiki is a working implementation; write the post that connects the dots.**


## Pitfalls (the death modes)

1. **Feature soup.** "A holistic Claude Code configuration" is unshareable. Lead with the one differentiated, demo-able thing (the graph). Reveal the rest serially.
2. **Positioning vs Superpowers head-on.** It owns the config layer at 174K stars. Position around what it lacks: "Superpowers gives Claude skills. Atomic gives it a code graph."
3. **No demo / bad first run.** If nothing impressive happens in 30 seconds, they leave forever. `atomic claude install` must work clean; the first query must wow. The demo GIF is non-negotiable.
4. **Launch-day silence or brigading.** Both kill you. 10-30 genuine contacts who will engage honestly is the balance.
5. **Cold launch.** Build in public for 4 weeks first (the architecture post, the tree-sitter-WASM story). Have a small audience before you need it.
6. **Only talking to Claude Code users.** That audience is finite. The code-graph story sells to Sourcegraph/ctags/LSP users with zero Claude framing. The SQL story sells to data engineers. Write for them too.


## Pre-launch checklist

```
[ ] Wow demo recorded (3-beat, 45s, real repo, dogfooded on atomic-claude)
[ ] README: demo GIF above the fold, hero line set, 30-second "what it does"
[ ] First-run UX clean on a fresh machine (install → first query → wow)
[ ] Architecture blog post published (Week -4), submitted to HN
[ ] awesome-claude-code PR drafted to merge-quality
[ ] HN title finalized, maker comment written, one honest limitation chosen
[ ] 10-30 contacts briefed (feedback, not upvotes)
[ ] X architecture thread drafted, demo GIF attached
[ ] Launch slot booked (Tue-Thu 9am-12pm ET)
[ ] The next two beats (OKF wiki, loop) drafted so cadence does not stall
```

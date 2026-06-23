---
layout: home
hero:
    name: Atomic Claude
    text: Loop engineering, installed.
    tagline: Onboard Claude once. It keeps a live map of your repo, takes features from issue to merged PR on autopilot, and sharpens its own setup from how you actually work. An opinionated Claude Code configuration. One install.
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

## The skill: context the loop reads every run

A loop without a skill re-derives your whole project from zero every cycle. The skill is where context survives between runs, and Atomic writes that context by scanning the repo.

New chats start blind. Claude doesn't know your framework, your build command, or how your code is organized, so it guesses. The guesses surface as invented `npm` scripts and wrong assumptions about your architecture.

One scan fixes that. `/refresh-signals` is a Karpathy-inspired repo explorer: it walks your repo and builds a standing model of it.

- **The facts.** Directory tree, manifests, languages, lockfiles. Reproducible and idempotent.
- **The meaning.** Framework, build, test, and lint commands, architectural style, and a domain map of which directories form which feature, across every layer.

Claude reads that model before it reads your code, so it knows what it's looking at before your first question. Ship commands refresh it as the repo changes, so the map stays current and you don't hand-maintain a `CLAUDE.md` that drifts.

## And the precise map beneath it

Signals give Claude a prose map of your repo. The code-intel engine gives it a precise one. `atomic code index` parses your code into a symbol graph of every definition, call edge, and import across 31 languages, using tree-sitter compiled to WebAssembly. It runs without a compiler or a language server.

Once the graph exists, Claude stops grepping for structure and starts querying it:

```text
atomic code explore "how does token refresh work"
   → the relevant symbols, files, and call relationships,
     gathered into one context digest.

atomic code impact validateToken
   → every caller that breaks if you change it, transitively.
```

SQL is one of those languages. `.sql` files are indexed alongside your application code, so Claude can answer which procedures read a table, what a view depends on, or where a foreign key points, across Postgres, MySQL, and T-SQL, from source with no database connection. Most code tools treat SQL as plain text; this one graphs it.

The investigator, reviewer, and signals agents reach for the graph when an index is present, and fall back to plain search when it isn't. Keep it fresh with `atomic code sync`; `/refresh-signals` does it for you whenever the index is warm.

## The maker and the checker

The single most important move in a loop is splitting the agent that writes from the agent that verifies. The author is too generous grading its own work; a separate reviewer with its own context catches what the first one talked itself into. `/autopilot` is that split, run end to end: hand it a task description or a GitHub issue number and it drives the entire lifecycle on its own.

```text
/autopilot 142 squash-and-merge

   → Reads issue #142. Writes a spec: controller, service, DTO,
     queue, signature validation.
   → Worktree-isolated. Builder implements each checkpoint;
     atomic-tdd fires — failing test first, then code.
   → Reviewer re-runs tests and gates against the spec. Every
     finding, blocking or not, gets fixed in-iteration.
   → Stuck twice on the same error? It dispatches a read-only
     strategist for root-cause analysis, then keeps going.
   → Squashes, merges, closes the issue.
```

One decision is yours: how to merge. Everything else runs unattended, which is only safe because the gate is objective: `atomic-tdd` writes a failing test before any code, the reviewer re-runs the suite against the spec, and `atomic-verify` refuses to call anything done without a fresh passing run. A loop that grades itself on an opinion drifts; this one stops on a test that passes or fails. To stay in the loop yourself, run the same steps as individual commands (`/atomic-plan`, `/subagent-implementation`, then a ship verb), each with its own approval gate.

## The loop sharpens itself

Most setups are static. You write rules once and they rot. `/atomic-improve` runs a retrospective instead: it reads your recent session history and the current conversation, finds where Claude caused friction, fought you, or repeated a mistake, and cross-checks it against your installed skills, rules, and memory.

```text
/atomic-improve

   → Scans the last 5 sessions plus this conversation.
   → Finding: you corrected the commit format three times across
     two sessions. Proposes a one-line edit to atomic-commit.
   → Finding: a skill fired on the wrong trigger twice. Proposes
     tightening its description.
   → You accept two, skip one. It writes a learnings log so the
     next run knows what you keep and what you drop.
```

Nothing changes without your yes. But over weeks the config stops being someone else's defaults and becomes yours.

## The answer takes the right shape

A loop ships more than you can read line by line, so the part you do read has to land in one pass. A paragraph is one instrument, not the only one. When an answer has parts that sequence, compare, or nest, the output style reaches for the form that shows that structure directly. Same question, same model, same facts. Default Claude Code on the left, Atomic Claude on the right.

A sequence becomes a flow:

<div class="ba-grid">
<div class="ba-col">

**Default Claude Code**

```text
When a request arrives it first passes through
the rate limiter, which checks the client IP
against a sliding window. If that allows it, the
authentication middleware reads the bearer token
from the Authorization header and verifies its
signature and expiry. With a valid token, the
authorization layer loads the user's roles and
checks them against the route's required
permissions before the handler finally runs.
```

</div>
<div class="ba-col">

**Atomic Claude**

```text
request
  → rate limit ... sliding window per IP
  → authn ........ verify bearer: sig + expiry
  → authz ........ load roles, check route perms
  → handler
```

</div>
</div>

A comparison becomes a table:

<div class="ba-grid">
<div class="ba-col">

**Default Claude Code**

```text
If you just want to record work locally, run
/commit, which stages and commits, then asks how
far to ship. Add a token to skip the prompt: with
/commit push it also pushes; /commit pr opens a
pull request; and /commit merge takes it straight
into the base branch. One verb, escalated by intent.
```

</div>
<div class="ba-col">

**Atomic Claude**

```text
verb            push  PR    merge
─────────────   ────  ───   ─────
/commit          no   no     no
/commit push     yes  no     no
/commit pr       yes  yes    no
/commit merge    yes   –     yes
```

</div>
</div>

And the filler goes too. A bug report, prose versus signal:

<div class="ba-grid" id="ba-replies">
<div class="ba-col">

**Default Claude Code**

```text
Sure! I'd be happy to help you understand
that. The issue you're experiencing is
likely caused by the middleware not properly
validating the token expiry timestamp. What's
happening is that the comparison uses a strict
less-than operator when it should be using
less-than-or-equal-to, which means tokens that
expire at exactly the current second are
incorrectly rejected.

I'd suggest updating the comparison on line 42
of src/auth/middleware.ts...
```

</div>
<div class="ba-col">

**Atomic Claude**

```text
Bug in auth middleware. Token expiry check at
src/auth/middleware.ts:42 uses `<` not `<=` —
tokens expiring at current second get rejected.

Fix: change `token.exp < now` to
`token.exp <= now`.
```

</div>
</div>

Same facts every time. The shape does the explaining.

## Pick your depth

You don't have to adopt all of it. Start where it helps.

1. **Structured replies only.** Install, activate the output style via `/config`. Done. Everything else is optional.
2. **A repo explorer.** Run `/atomic-setup` + `/refresh-signals` in your repo. Claude stops hallucinating build commands.
3. **A symbol-aware assistant.** Run `atomic code index`, then `atomic code explore "<question>"` returns a digest of symbols, files, and call edges in one query.
4. **Full plan → implement → review loop, or autopilot.** Read the [workflow reference](/reference/workflow).

Not sure where to begin? Run `/atomic-help` in any repo. It reads your git state and recommends one next command.

<div class="home-cta">

[Install →](/guides/install) [Concepts →](/reference/concepts) [Commands →](/reference/commands)

</div>

</div>

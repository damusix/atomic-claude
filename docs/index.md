---
layout: home
hero:
    name: Atomic Claude
    text: Higher accuracy. Less busywork.
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
    - icon: "\uF066"
      title: Clearer replies
      details: A communication layer that cuts filler and structures multi-part answers with tables, trees, and ASCII flows. Compressed, but built for clarity.
    - icon: "\uF0E8"
      title: A queryable map of your code
      details: "One command parses your repo into a symbol graph across 29 languages and 15 web frameworks, no compiler required: definitions, callers, call sites, and the blast radius of any change. Claude queries the graph instead of grepping."
---

<div class="vp-doc home-extra">

## Claude explores your repo first

New chats start blind. Claude doesn't know your framework, your build command, or how your code is organized, so it guesses. The guesses surface as invented `npm` scripts and wrong assumptions about your architecture.

One scan fixes that. `/refresh-signals` is a Karpathy-inspired repo explorer: it walks your repo and builds a standing model of it.

- **The facts.** Directory tree, manifests, languages, lockfiles. Reproducible and idempotent.
- **The meaning.** Framework, build, test, and lint commands, architectural style, and a domain map of which directories form which feature, across every layer.

Claude reads that model before it reads your code, so it knows what it's looking at before your first question. Ship commands refresh it as the repo changes, so the map stays current and you don't hand-maintain a `CLAUDE.md` that drifts.

## And it can query your code's structure

Signals give Claude a prose map of your repo. The code-intel engine gives it a precise one. `atomic code index` parses your code into a symbol graph of every definition, call edge, and import across 29 languages, using tree-sitter compiled to WebAssembly. It runs without a compiler or a language server.

Once the graph exists, Claude stops grepping for structure and starts querying it:

```text
atomic code explore "how does token refresh work"
   → the relevant symbols, files, and call relationships,
     gathered into one context digest.

atomic code impact validateToken
   → every caller that breaks if you change it, transitively.
```

The investigator, reviewer, and signals agents reach for the graph when an index is present, and fall back to plain search when it isn't. Keep it fresh with `atomic code sync`; `/refresh-signals` does it for you whenever the index is warm.

## Hand off the whole feature

Most workflow tools stop at scaffolding a plan. `/autopilot` takes a task description or a GitHub issue number and runs the entire lifecycle on its own.

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

One decision is yours: how to merge. Everything else runs unattended. To stay in the loop, run the same steps as individual commands (`/atomic-plan`, `/subagent-implementation`, then a ship verb), each with its own approval gate.

## The config learns from you

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

## And the replies are tighter

Same question, same model. Default Claude Code on the left, Atomic Claude on the right.

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

Same accuracy. Less noise. Clearer to follow.

## Pick your depth

You don't have to adopt all of it. Start where it helps.

1. **Clearer replies only.** Install, activate the output style via `/config`. Done. Everything else is optional.
2. **A repo explorer.** Run `/atomic-setup` + `/refresh-signals` in your repo. Claude stops hallucinating build commands.
3. **Full plan → implement → review loop, or autopilot.** Read the [workflow reference](/reference/workflow).

Not sure where to begin? Run `/atomic-help` in any repo. It reads your git state and recommends one next command.

<div class="home-cta">

[Install →](/guides/install) [Concepts →](/reference/concepts) [Commands →](/reference/commands)

</div>

</div>

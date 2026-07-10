---
description: Multi-lens design challenger. Reads a design/spec/plan, dispatches 4-6 isolated expert lenses (security, performance, future maintainer, API consumer, ops, tester, end user) as parallel subagents, then merges their findings into a contradiction map — where lenses conflict, where they independently agree, and what they all assumed. Post-design gate — follows /atomic-plan, precedes /subagent-implementation. Complements /pressure-test: that is dialogue with you; this attacks the written artifact.
argument-hint: "[<path-to.md> | @<path> | <topic-phrase>]"
---

# /challenge-swarm


Take an existing design, spec, or plan and subject it to independent expert scrutiny, then map where the experts disagree. Multiple genuinely independent perspectives surface holes any single review pass misses — and the *disagreements* between perspectives are the highest-value output, not the consensus. Mechanism adapted from Stanford's STORM (perspective-diverse, grounded multi-agent synthesis); the grounding corpus here is the codebase.


This command does **not** produce a new design. The planning workflow already exists (`/atomic-plan`). The deliverable is a challenge report the user folds back into the design — surgical findings only, never a rewrite.


## Workflow position


```
hunch → /gather-evidence → /pressure-test → /atomic-plan → /challenge-swarm → /subagent-implementation
        (does it hold?)    (you defend it    (write design   (isolated experts    (build it)
                            in dialogue)      + spec)          attack it)
```


## Parse arguments


`$ARGUMENTS` may be empty, a topic phrase, or a target document reference. Classify with this precedence (first match wins, single pass):


1. Token starts with `@` → strip the `@`, treat the remainder as a path. Resolve relative to cwd; absolute allowed. Must exist and end in `.md` → **target document**. Otherwise print one line: `path '<x>' not found (or not markdown) — pass a design/spec path or topic.` and continue classifying the rest.
2. Token ends in `.md` and exists on disk → **target document** (same checks).
3. Anything else → **topic phrase**. Glob `docs/design/*<slug>*.md` and `docs/spec/*<slug>*.md`. One match → use it, confirm in one line. Multiple → numbered list, typed selection. Zero → ask for a path.
4. Empty `$ARGUMENTS` → list candidates: design/spec files changed on the current branch (`git diff --name-only <base>...HEAD -- docs/design docs/spec`), newest first, numbered, typed selection. No candidates → ask: *"Which document should the swarm challenge?"*


**Path safety.** Resolve the final absolute path. If it falls outside `git rev-parse --show-toplevel`, reject with the same one-line message and drop the token.


The target is any written artifact worth challenging before code exists: a design doc, a spec, an implementation plan, an ADR.


<workflow>

## Step 1 — Read, then select lenses


Read the target end-to-end and orient in the code it touches (project signals; `atomic code explore "<area>"` when an index is present, `sg`/grep otherwise) **before** selecting lenses — the roster follows from what the change actually touches, not from a fixed checklist.


Pick **4-6 lenses**:


| Lens | Asks |
|------|------|
| **Security** | Trust boundaries crossed? Input validation? AuthZ gaps? Secrets handling? Injection/SSRF surface? |
| **Performance** | Hot paths? N+1s? Payload sizes? What breaks at 10x load? What should be measured before assuming? |
| **Maintainer in 2 years** | Will this make sense with zero context? Hidden coupling? Is the abstraction earning its complexity (YAGNI)? |
| **API consumer** | Is the contract obvious? Breaking changes? Error semantics? Versioning/migration story? |
| **On-call / ops** | How does it fail at 3am? Observability? Rollback path? Feature flags? Blast radius? |
| **Tester** | What's untestable as designed? Edge cases the spec is silent on? Race conditions? What does "done" mean? |
| **End user** | What does the user actually experience — latency, error messages, workflow changes, surprises? Include whenever the change is user-visible. |


Add a bespoke lens when the domain demands one ("data migration safety" for schema changes, "compliance" for regulated data). Skip lenses with nothing at stake — a lens with no relevant surface produces filler, and filler buries the real findings.


Print the chosen roster with a one-line reason per lens, then proceed. No confirmation gate — the user invoked the swarm; the printed roster is the audit trail.


## Step 2 — Workspace, then dispatch in isolation


Communication runs through a workspace, not through shared context:


```
.claude/.scratchpad/<yyyy-mm-dd>-challenge-swarm-<slug>/
├── lens-instructions.md   shared process + output contract (verbatim block below)
├── lenses/
│   └── <lens>.md          one role file per selected lens
└── findings/              each lens writes its output here
```


Write `lens-instructions.md` verbatim from the block below, then one role file per lens. Dispatch **one `general-purpose` subagent per lens, all in a single message** so they run in parallel, and pass **`model: sonnet` on every dispatch** — the role file carries the specialization; a heavier tier may add insight, but it definitely adds cost. Use a different tier only when the user explicitly asks for one this run; never inherit the session model by omission — on a premium session tier (Opus, Fable) an unpinned dispatch multiplies spend across 4-6 agents.


The dispatch prompt is deliberately just pointers — identical for every lens except two paths:


```
You are a design-review lens. In order:
1. Read <workspace>/lens-instructions.md — your process and output contract.
2. Read <workspace>/lenses/<lens>.md — your specific role.
3. Read the design at <target> and the code paths your role file lists.
Write your findings to <workspace>/findings/<lens>.md.
Reply with ONE line only: <lens>: <N> findings, worst <severity>.
```


Why this shape: **isolation is the load-bearing mechanism**. Lenses that see each other's findings converge, and convergent reviews are worthless for contradiction mapping — subagents start with fresh context, and the pointer-prompt keeps it that way. Findings go to files rather than replies because files are durable and auditable: the user can edit one role file and rerun just that lens. The one-line reply keeps the main context clean until aggregation. Wait for every lens to report before reading any findings file.


If subagents are unavailable, run the lenses sequentially yourself using the same workspace: write each lens's findings file *before* starting the next lens, and reread nothing until Step 3.


### lens-instructions.md (write verbatim)


```markdown
# Lens instructions

You are one expert lens reviewing a design in isolation. Other lenses review
the same design in parallel — you cannot see their findings, and that isolation
is deliberate: independent perspectives that agree are evidence; perspectives
that merely echo each other are noise.

## Process

1. Read your role file fully. It names your perspective, core questions, and
   the code paths that matter to you.
2. Read the target design end-to-end before forming any finding.
3. Read the code paths your role file lists. Ground claims in the actual code —
   a finding with file:line evidence outranks a hypothetical.
4. Judge the design ONLY from your lens. General code-review commentary belongs
   to another lens; leave it.

## Findings file format

Write to the findings path you were given, exactly this shape:

    # <lens> findings

    ## F1 — <one-line title> [severity: critical|high|medium|low]
    - claim: <what is wrong or risky — one or two sentences>
    - evidence: <file:line, design section, or reasoning chain>
    - question the design must answer: <one sentence>

    ## F2 — ...

    ## Assumptions
    - <assumption you had to make — load profile, deployment model, user base — one line each>

## Rules

- 3 to 7 findings. Fewer beats filler — if your lens has nothing at stake,
  write "no findings from this lens" plus your Assumptions section and stop.
- Every finding carries evidence. No evidence → cut the finding.
- State positions plainly. No hedging.
- Findings only — do not propose a redesign.
```


### Role file shape (one per lens)


```markdown
# Lens: <name>

perspective: <one-sentence persona>

core questions:
- <the lens-table questions, tuned to this design>

bespoke focus:
- <2-4 bullets naming the specific parts of THIS design this lens must interrogate>

code paths:
- <repo paths relevant to this lens>
```


## Step 3 — Build the contradiction map


This is the deliverable's core, and it happens in the main context. Once every lens reports done, load all `findings/*.md`, merge, and sort into three buckets:


1. **Conflicts** — lenses pull in opposite directions (performance wants caching; security notes cache poisoning; ops asks who invalidates it). For each: the competing positions, the evidence quality on each side, and the trade-off decision the design must make explicit. Do not resolve a conflict unless one side's evidence is decisively stronger — the point is to surface the decision, which belongs to the design's owner.
2. **Reinforced findings** — independent lenses hit the same issue from different angles. Highest-confidence problems; flag them as such.
3. **Unexamined assumptions** — compare the `## Assumptions` sections across lenses. Anything every lens took for granted without challenge (load profile, dependency stability, deadline constraints) is where designs get blindsided. One sentence each.


Where a contested claim is *objectively checkable* — "this query will table-scan", "that function already handles the nil case" — check it instead of reporting the dispute: `atomic code explore`/`callers`/`impact` when an index is present, `sg`/grep otherwise, or a 10-line probe script in `tmp/`. Many disagreements have answers; use the codebase's advantage over prose.


## Step 4 — Report


Single report, in the conversation:


```
# Challenge swarm: <target name>

## Verdict                  3-5 sentences — is the design sound? what to fix first?
## Conflicts                trade-off decisions the design must make explicit
## Reinforced findings      multi-lens agreement — highest confidence, severity-tagged
## Single-lens findings     worth a look, lower confidence
## Unexamined assumptions   what every lens took for granted
## Missing lens             what perspective did this swarm itself lack?
```


Severity order within sections. Every finding carries its evidence. Keep it short — a report longer than the design means the filtering wasn't done; the user acts on this, they don't read it recreationally.


## Close-out


Numbered offers, typed selection:


```
1. rerun a lens         — edit lenses/<name>.md first if you want it re-aimed
2. add the missing lens — <name from the report>
3. file as follow-ups   — atomic followups add --kind finding, one per accepted finding
4. fold into the design — /atomic-plan @<target> to encode the decisions (spec-currency rule applies)
5. done                 — delete the workspace
```


On `5`, delete the workspace directory — scratchpad is throwaway working memory. On `1`/`2`, dispatch only the named lens (same pointer prompt, same `model: sonnet`) and rebuild the map. On `3`/`4`, act, then re-offer.

</workflow>

<constraints>

## Behavioral rules


1. **Report only.** Never modify the design, the spec, or any code. Findings feed back through `/atomic-plan` or explicit user asks.
2. **Isolation is inviolable.** Never paste one lens's findings into another lens's prompt or role file; never run a "respond to lens X" round. Cross-lens synthesis happens only in Step 3, in the main context.
3. **Evidence per finding.** A finding that arrives without file:line, design-section, or reasoning-chain evidence gets cut at aggregation.
4. **Filler dies at aggregation.** Dedupe, drop no-stake findings, keep the report shorter than the design.
5. **Verify before asserting.** Contested checkable claims get resolved with tool calls before they appear in the map (same rule as `<investigate_before_answering>` in `CLAUDE.md`).
6. **The roster is judgment, not a checklist.** 4-6 lenses that fit the change beat 7 that pad it.
7. **Sonnet, pinned.** Every lens dispatch passes `model: sonnet`. Only an explicit user request for a different tier overrides it — never the session model.


## What this command does not do


- Does not write durable artifacts. The report lives in the conversation; the workspace is gitignored scratchpad, deleted at close-out.
- Does not modify the target document or any code.
- Does not auto-fire. Explicit invocation only — it spawns 4-6 subagents, which is never a surprise the user should discover.
- Is not dispatched by `/autopilot`. Human-invoked gate, like `/pressure-test`.
- Does not replace `/pressure-test` — that is Socratic dialogue where you defend your thinking; this is a parallel attack on the written artifact.

</constraints>

## When to suggest the next step


- Conflicts surfaced → `/atomic-plan @<target>` to encode the trade-off decisions explicitly.
- Reinforced critical/high findings → fix the design before `/subagent-implementation`.
- A contested fact stayed unresolved → `/gather-evidence "<claim>"` to settle it.
- Report clean → proceed: `/subagent-implementation @<spec>`.


Surface as a one-line hint, not a directive. The user chooses.

# Documentation as maintenance, not bookkeeping


## Problem


The `/documentation` command and `atomic-documentation` skill today are artifact-index updaters. Their routing table is hardcoded to atomic's own surfaces: "new command → add row to README table", "new agent → update CLAUDE.md entry." For a user working on their own Rails app, Go service, or React frontend, the routing table is empty — the skill returns "no doc impact" and the command does nothing useful.

This misses the original intent. The documentation system should help users maintain **human-facing documentation** about their project — the kind of docs that help a new team member (or future-you) understand what things do, why decisions were made, how data flows, and what the business rules are. Architecture overviews. API references. ERDs. Flowcharts. Decision logs. The documentation that rots first and hurts most when stale.

Today's failure modes:

1. **Empty on non-atomic repos.** A user installs atomic-claude, makes changes to their payment service, runs `/documentation` or commits via a ship verb — nothing happens. The override mechanism exists (`## Documentation surfaces` table in `claude.local.md`) but requires the user to manually author a routing table, which is exactly the kind of toil this tool should eliminate.

2. **Index-focused, not content-focused.** Even on the atomic repo, the skill routes to "add a table row" or "append a CLAUDE.md entry" — mechanical bookkeeping. It never asks "does the architecture guide still reflect reality?" or "should this new payment flow have its own doc page?"

3. **No discovery.** The skill doesn't read what documentation already exists. It can't match a diff against existing docs because it never looks at them. A project with a thorough `docs/architecture/` tree gets the same empty treatment as a project with zero docs.

4. **No creation.** The skill only proposes edits to existing surfaces. It never suggests "this new module has no documentation — should we create a page?" Projects that start with good docs but add features without updating them slowly drift.


## Vision


When a user changes code in their project, the system should:

1. **Know what docs exist.** Discover the project's documentation tree and index it in CLAUDE.md so the LLM always knows.
2. **Match diffs against indexed docs.** "You changed the payment webhook handler. `docs/architecture/payments.md` describes webhook processing — it's stale."
3. **Do the work.** Open the file, update the ERD, fix the stale flow diagram, add the missing field to the table. The user decides *when*, not *what*.
4. **Detect missing coverage.** "You built a new `notifications/` module with 8 files. No docs cover it. Create a page?"
5. **Use the right format.** Flowcharts for processes. ERDs for data models. Prose for business rules. Tables for API references. Mermaid for anything that renders.


## What users actually want from project docs


Based on what helps humans navigate a growing codebase:

| Doc type | What it answers | Format | When it drifts |
|----------|----------------|--------|----------------|
| Architecture overview | "How do the pieces fit together?" | Prose + flowchart/diagram | New service, new integration, major refactor |
| Domain/module guide | "What does billing/ do and why?" | Prose + ERD + business rules | New entity, changed flow, new external dep |
| API reference | "What endpoints exist, what do they accept?" | Tables + examples | New route, changed request/response shape |
| Data model / ERD | "What are the entities and relationships?" | Mermaid ERD + field tables | Migration, new model, changed relationship |
| Business rules | "Why does X work this way?" | Numbered rules + decision rationale | Rule change, new edge case, compliance update |
| Flow/sequence docs | "What happens when a user does Y?" | Mermaid sequence/flowchart | New step, changed order, new error path |
| Decision log / ADR | "Why did we choose Z over W?" | Prose: context → decision → consequences | Rarely drifts (point-in-time), but new decisions need new entries |
| Onboarding / getting started | "How do I set up and run this?" | Step-by-step prose | New dependency, changed config, new env var |


## Design


### Two modes: maintenance and authoring

The strategist review identified a critical flaw in the original design: conflating "flag stale docs during commit" (fast, low-stakes) with "generate new documentation" (slow, high-stakes, requires user attention). These have different cost profiles, latency tolerances, and mental contexts.

| Mode | When | Trigger | What happens | Cost |
|------|------|---------|-------------|------|
| **Maintenance** | Commit flow (ship verbs) | Automatic, after staging | Match diff against indexed surfaces. Flag stale/incomplete. Ask user: update now, defer, or skip. | Fast, bounded — reads an indexed table, no LLM discovery |
| **Authoring** | `/documentation` explicit | User types the command | Full discovery scan, "missing" classification, new page generation, ERDs, flowcharts, full drafts | Slow, unbounded — user opted in to spending time on docs |

Maintenance mode never suggests creating new pages. It only flags existing indexed docs that the diff made stale or incomplete. Authoring mode does everything — discovery, gap detection, content generation.


### Indexed surfaces in CLAUDE.md, not a hidden cache

Instead of auto-discovering surfaces into a gitignored cache, the system **indexes doc surfaces in the project's CLAUDE.md** (committed, team-visible). This is the critical design choice.

Why indexing in CLAUDE.md beats a hidden cache:

- **The LLM always knows.** Surfaces are loaded every session. No cache staleness problem. No discovery step at commit time.
- **User controls scope.** They decide which docs matter. `docs/legacy/` stays out if they say so.
- **Domain binding is explicit.** "payments.md covers billing, webhooks, Stripe" means a diff touching `src/billing/` reliably matches. No semantic guessing.
- **Deterministic matching at commit time.** Ship verbs read the table, match diff paths against the "Covers" column. Fast, bounded, no LLM call for classification.
- **Collaborative.** The table is committed — the whole team sees which docs are tracked and what they cover.
- **Transparent.** Users can read and edit their routing. No magic.

**Why CLAUDE.md, not `claude.local.md`.** `claude.local.md` is gitignored — it's the user's private scratchpad for personal project-specific context that shouldn't interfere with the repo's committed instructions. Putting the surfaces table there means teammates never see it, and each developer would need to bootstrap independently. The surfaces table is a team contract: "these are our docs, this is what they cover." It belongs in committed CLAUDE.md alongside other project conventions.

**Target file selection.** The bootstrap step writes to whichever committed Claude instructions file already exists in the project, using the same search order as signals:

1. `CLAUDE.md` (most common — the project's committed instructions)
2. Create `CLAUDE.md` if the project has `.claude/project/` but no instructions file yet

Exception: repos where `CLAUDE.md` is a bundle source (like the atomic-claude repo itself) may need the table in `claude.local.md` instead. The bootstrap step detects this case if the user says so, but defaults to the committed file with a note that teammates benefit from the shared table.

The table format:

```markdown
## Documentation surfaces

| Path | Covers | Voice |
|------|--------|-------|
| `docs/architecture/payments.md` | billing, webhooks, Stripe | atomic-prose |
| `docs/models/README.md` | data model, ERD, migrations | atomic-prose |
| `docs/api/endpoints.md` | REST API, auth, rate limits | atomic-prose |
| `docs/onboarding.md` | dev setup, env vars, local dev | atomic-prose |
| `README.md` | project overview, quick start | atomic-prose |
```

The `Covers` column is the matching key. Ship verbs compare diff file paths and content against these terms. The `Voice` column tells the content generator which style to use.


### Bootstrap: `/documentation` discovers and indexes

On first run (or when the user adds new docs), `/documentation` does the bootstrapping:

1. **Scan the documentation tree.** The `atomic` binary runs `atomic docs scan` — a deterministic filesystem walk that finds doc files and extracts headings (H1 + first 3 H2s per file). No LLM involved. Runs in milliseconds, even on 500-file doc trees.

2. **Present discovered surfaces.** Show the user what was found:

    ```
    Discovered 12 documentation files. Index these in your CLAUDE.md?

      [1] docs/architecture/payments.md — Payments / Webhook Flow / Stripe Integration / Retry Logic
      [2] docs/architecture/auth.md — Authentication / JWT Flow / OAuth Providers / Session Management
      [3] docs/models/README.md — Data Model / Entity Relationships / Migration History
      ...

    Type: all | 1 3 5 | none
    ```

3. **User picks which to index.** For each selected surface, optionally annotate which domains it covers (or accept the heading-derived suggestion).

4. **Write the table.** Append a `## Documentation surfaces` section to the project's committed CLAUDE.md. The table is committed — the team shares it.

5. **Check for unindexed docs.** On subsequent runs, `/documentation` compares the scan results against the indexed table and offers to add any new unindexed docs. "Found 3 doc files not in your surfaces table. Add them?"

This is similar to how `/refresh-signals` bootstraps the signals `@-refs` on first run — discover, present, wire, then subsequent runs are incremental.


### Maintenance mode: commit-flow integration

During ship verbs, the doc-impact step reads the `## Documentation surfaces` table from CLAUDE.md and matches the staged diff against it. For each potentially stale surface:

```
docs/models/README.md may be stale.
reason: migration adds refund_status column to orders table
action: update orders entity in ERD, add field to orders table

  [y] Yes — update now
  [l] Later — create a follow-up
  [r] Remind me — schedule a reminder
  [s] Skip — no action, no record
```

**Yes** → the skill opens the file, reads the current content, reads the diff, and makes the edit. Updates the ERD Mermaid syntax, adds the field row, fixes the prose. Stages the file. Done. The user did not write anything — the skill did the work.

**Later** → creates a project follow-up via `atomic followups add`:

```
id: doc-models-readme-<hash>
title: update orders ERD in docs/models/README.md — refund_status column added
severity: nit
origin: /commit-only doc-impact
```

Surfaces on next `/follow-up review` or session-start hook.

**Remind me** → prompts for timing using natural-language inference (same as `/remind-me`): "after the PR", "tomorrow", "end of week". Creates a reminder that fires at the specified time.

**Skip** → no action, no record. The user doesn't care about this one.

This is fast because:

- No discovery. The table is already in CLAUDE.md.
- No LLM matching call. The LLM reads the table and the diff in the same context window — it's a judgment call within the existing ship-verb turn, not a separate dispatch.
- No content generation unless the user picks "Yes." And even then, it's scoped to one file.


### Authoring mode: `/documentation` explicit

When the user explicitly runs `/documentation`, the full pipeline runs:

1. **Scan** via `atomic docs scan` (deterministic heading extraction).
2. **Diff** against the indexed surfaces table — find unindexed docs, suggest additions.
3. **Match** the diff range against indexed surfaces — find stale/incomplete.
4. **Detect missing.** For domains identified by signals that have no corresponding doc surface, suggest creating a page. Gate behind a heuristic: only suggest for modules with 5+ files. A 3-file utility doesn't need its own page.
5. **Walk surfaces** with the user. Per surface:
    - **Stale/incomplete** → same Yes/Later/Remind/Skip as maintenance mode, but "Yes" generates the full content immediately.
    - **Missing** → `[n] New` option generates a full page draft: intro paragraph, Mermaid diagrams (ERD, flowchart, sequence as appropriate), business rules, edge cases. Uses `atomic-prose` voice.
6. **Stage** edited/created files.
7. **Summary.**


### Deterministic substrate: `atomic docs scan`

The `atomic` binary gets two new subcommands, mirroring the signals pattern:

```
atomic docs scan    → walks doc directories, extracts H1 + first 3 H2s per .md file
                      writes .claude/project/doc-surfaces.md (gitignored cache)
atomic docs stale   → compares cache timestamp against latest doc-file mtime
                      exit 0 = fresh, exit 1 = stale
```

Implementation complexity: ~100-150 lines of Go. The `internal/mdparse` package (551 lines) already parses markdown structure. The new code:

- Walks directories matching `docs/`, `doc/`, `documentation/`, `wiki/`, `ADR/`, `adr/`, `decisions/`, plus any `**/README.md`.
- Respects `.signalsignore` — if signals skips `docs/internal/`, doc discovery skips it too.
- For each `.md` file: reads H1 (title), first 3 H2s (scope), first paragraph under H1 (summary).
- Writes the cache in the same markdown format as the bootstrap output.
- `stale` compares the cache file's mtime against the most recent mtime of any doc file in the scanned directories.

Heading-only extraction gets ~80% of matching quality at 0% token cost. For the remaining 20%, the LLM's judgment during the matching step (which already has the diff in context) fills the gap.


### Proactive suggestion at end of implementation

At the end of `/subagent-implementation` (after all checkpoints pass, before the user picks a ship verb), the system checks whether the implemented changes affect any indexed doc surfaces. If so, it appends to the "next steps" suggestion:

```
Implementation complete. 3 checkpoints passed.

Next steps:
  /commit-only          — commit changes
  /commit-and-pr        — commit + open PR
  /documentation        — 2 doc surfaces may be stale (docs/models/README.md, docs/api/endpoints.md)
```

This is advisory — a one-line note in the next-steps list. Not a gate, not a prompt, not a separate step. The user sees it and decides whether to address docs now or let the ship verb handle it during commit.


### Atomic-specific routing moves to `claude.local.md`

The current hardcoded routing table in the skill (commands → README, agents → CLAUDE.md, etc.) moves to this repo's `claude.local.md` as a standard `## Documentation surfaces` table. The skill becomes fully generic — it reads whatever table the project provides.

This eliminates:

- Fragile project-detection heuristics (checking go.mod module name, package.json name).
- Special-case code paths in the skill.
- The risk that forks or renames break routing.

The atomic repo gets the same mechanism as every other project. Dogfooding.


## Content generation: the skill does the work


When the user picks "Yes" (maintenance mode) or accepts a proposal (authoring mode), the skill opens the file and makes the edit. The user does not write documentation — they approve it.


### Maintenance mode content (stale/incomplete)

The skill reads the current doc file, reads the diff, and generates a targeted edit. Examples:

**Stale ERD** — migration added `refund_status` to the `orders` table. The skill:

1. Opens `docs/models/README.md`
2. Finds the Mermaid ERD block
3. Adds `string refund_status` to the `Order` entity
4. Finds the field table for `orders`
5. Adds a row: `refund_status | string | pending/approved/rejected | Status of refund request`
6. Stages the file

**Stale flow diagram** — new webhook verification step added. The skill:

1. Opens `docs/architecture/payments.md`
2. Finds the Mermaid flowchart
3. Inserts the new step between "Receive webhook" and "Process payment"
4. Updates any prose description of the flow below the diagram
5. Stages the file

**Incomplete API reference** — new endpoint added. The skill:

1. Opens `docs/api/endpoints.md`
2. Finds the section for the relevant resource
3. Adds a new row to the endpoint table with method, path, auth, request/response shapes
4. Adds an example if the existing format includes examples
5. Stages the file


### Authoring mode content (new pages)

When creating a new page, the skill generates a full draft using the doc type that fits the content:

- **New module with data models** → domain guide with ERD, field tables, business rules
- **New API routes** → API reference with endpoint table, request/response examples
- **New multi-step process** → flow doc with Mermaid flowchart, step descriptions
- **Architectural decision** → ADR with context, decision, consequences

The draft follows `atomic-prose` voice: short intro sentence, tables for comparisons, categorized sections, plain language, Mermaid where it compresses understanding.

After generating, the skill:

1. Writes the new file
2. Offers to add it to the `## Documentation surfaces` table in CLAUDE.md
3. Notes where it should be linked (index page, sidebar, README) so it's not an orphan doc
4. Stages everything


### Mermaid generation

Mermaid diagrams are generated directly — full syntax, not descriptions. The skill picks the diagram type based on content:

| Content | Diagram type | When |
|---------|-------------|------|
| Entities and relationships | `erDiagram` | Data model pages, domain guides |
| Multi-step process | `flowchart` | Architecture pages, flow docs |
| Actor interactions | `sequenceDiagram` | Integration docs, API flows |
| State transitions | `stateDiagram-v2` | Lifecycle docs (order states, auth flows) |

Every Mermaid block gets a one-sentence caption above it so non-rendering readers still understand. When updating an existing diagram, the skill preserves the existing style (LR vs TD orientation, node naming conventions, color scheme) and adds/modifies only the changed elements.


## Output quality: human-readable, not LLM-readable


The documentation this system produces is for humans navigating a codebase — not for LLMs, not for indexing, not for completeness checklists. Every page it generates or updates should be something a developer would actually read and find useful.


### Voice and structure

Generated docs follow the same patterns as atomic-claude's own guides and reference pages:

- **Short intro sentence** that tells you what the page is about before any detail.
- **Tables for structured comparisons.** When there are multiple items with shared attributes (commands, agents, config keys), a table is always better than a prose list.
- **Categorized sections** that group related things under clear headings. A reader scanning the sidebar or table of contents should know what each section covers without clicking.
- **Plain language** explaining what things do and why. No jargon without context. No "this module provides an abstraction layer over the persistence subsystem" — say "this handles saving and loading user data from the database."
- **Code blocks only for things the user types or sees.** CLI commands, config snippets, API calls. Not for describing behavior.
- **Mermaid diagrams where they compress understanding.** ERDs for data models (entities + relationships at a glance). Flowcharts for multi-step processes. Sequence diagrams for actor interactions. One-sentence caption above every diagram so non-rendering readers still get it.

The test: could a new team member read this page on day one and understand enough to start working in this area? If not, it needs more context or less jargon.


### What makes documentation useful

Each doc type has a specific job. Generated content should match the job, not default to generic prose.

| Doc type | Job | Structure | Example |
|----------|-----|-----------|---------|
| Architecture overview | Orient the reader | Intro paragraph → component diagram → one section per component explaining its role and how it talks to neighbors | "The billing service receives webhook events from Stripe, validates them, updates the subscription record, and fires domain events." |
| Domain/module guide | Explain one area deeply | What it does → data model (ERD) → key flows → business rules → edge cases | "Refunds: a refund can only be issued within 30 days of purchase. Partial refunds create a new ledger entry..." |
| API reference | Let the reader look things up | Table per resource: method, path, auth, request shape, response shape. One example per endpoint. | `POST /api/v1/payments` → request body, response, error codes |
| Data model | Show entities and relationships | Mermaid ERD + field table per entity. Relationship cardinality explicit. | `User 1--* Order`, `Order *--1 Product` |
| Business rules | Record decisions that shape behavior | Numbered rules with rationale. "Rule 3: Trial accounts cannot exceed 5 projects. Why: prevents abuse of free tier compute allocation." | Each rule stands alone — readable without surrounding context |
| Flow / sequence | Show what happens step by step | Mermaid flowchart or sequence diagram. Happy path first, then error/edge paths as variants. | "User clicks Pay → frontend calls /checkout → backend creates Stripe session → redirect → webhook confirms → order marked paid" |
| Decision record (ADR) | Capture why, not just what | Context → Decision → Consequences. Short. Point-in-time. | "We chose Postgres over DynamoDB because our query patterns are relational and we value joins over infinite scale." |
| Getting started | Get someone running | Prerequisites → install → configure → verify. Numbered steps. Every command copy-pasteable. | "1. Clone the repo. 2. Run `make setup`. 3. Copy `.env.example` to `.env`..." |


### Anti-patterns to avoid in generated docs

- **Restating the code.** "The `processPayment` function takes a `PaymentRequest` and returns a `PaymentResult`." The code already says this. Document *why* — "Payments are processed synchronously because the downstream provider requires confirmation within 10 seconds."
- **Wall of prose.** If a section is longer than ~15 lines without a heading, table, diagram, or code block, it needs structure.
- **LLM-tell.** "This comprehensive guide provides an in-depth overview of..." Write like a human wrote it. Direct, specific, no filler.
- **Orphan docs.** A page that exists but is not linked from any index, sidebar, or README is invisible. Generated pages should include a note about where to link them.
- **Stale examples.** Code examples in docs must reflect the current API. When updating a doc because the API changed, update the examples too — a stale example is worse than no example.


## Decisions (resolved)


These were open questions in the initial draft, resolved after strategist review and discussion.

**1. Cache: gitignored.** `.claude/project/doc-surfaces.md` is a derived artifact, same as signals. Committing it creates merge conflicts when two branches add docs. Any team member can regenerate in milliseconds via `atomic docs scan`. The *indexed table* in CLAUDE.md is committed — that's the shared contract.

**2. "Missing" suggestions: authoring mode only.** Never during commit flow. The user's mental context during a ship verb is "ship this change," not "write new documentation." Missing-doc detection runs only when the user explicitly invokes `/documentation`. Gated behind a heuristic: only suggest for modules with 5+ files that have no doc surface within two directory levels.

**3. Proactive suggestion: end-of-implementation advisory.** At the end of `/subagent-implementation`, if the implemented changes affect indexed doc surfaces, include `/documentation` in the next-steps list with a count of potentially stale surfaces. One line. Not a gate, not a prompt. The user sees it alongside the ship verb suggestions and decides.

**4. Deterministic substrate: `atomic docs scan` in the Go binary.** ~100-150 lines. Walks doc directories, extracts H1 + first 3 H2s per file, writes the cache. Respects `.signalsignore`. Heading-only extraction at 0% token cost covers 80% of matching quality. The LLM's judgment during the matching step covers the rest. `atomic docs stale` compares cache mtime against latest doc-file mtime.

**5. Routing: user indexes in committed CLAUDE.md.** The surfaces table goes in the project's committed `CLAUDE.md`, not `claude.local.md`. `claude.local.md` is gitignored — it's the user's private scratchpad and shouldn't carry team-shared contracts. The skill becomes fully generic: no hardcoded project detection, no special cases. The atomic-claude repo is the one exception where the table lives in `claude.local.md` (because `CLAUDE.md` is the bundle source), but this is the repo's choice, not the skill's. `/documentation` bootstraps the table on first run: scans, presents discovered surfaces, user picks which to index, table gets written to CLAUDE.md. Subsequent runs check for unindexed docs and offer to add them.

**6. Mermaid: direct generation, always.** When the skill updates or creates a doc, it generates full Mermaid syntax — `erDiagram`, `flowchart`, `sequenceDiagram`, `stateDiagram-v2` as appropriate. Every diagram gets a one-sentence caption. When updating existing diagrams, preserve the existing style (orientation, naming, colors). The user reviews the result and can ask for changes — but the default is a complete, renderable diagram, not a description of what one would look like.


## Proposed flow


### Maintenance mode (commit flow)

```
Ship verb stages changes
         │
         ▼
Read ## Documentation surfaces
table from CLAUDE.md
         │
         ▼
Match staged diff against
"Covers" column
         │
         ▼
   ┌─────┴─────┐
   │ No match  │ → proceed silently
   └───────────┘
         │
    Matches found
         │
         ▼
Per stale/incomplete surface:
  [y] Yes → skill edits the file, stages
  [l] Later → atomic followups add
  [r] Remind → /remind-me flow
  [s] Skip → no action
         │
         ▼
Continue to commit
```


### Authoring mode (`/documentation`)

```
User runs /documentation
         │
         ▼
atomic docs scan (deterministic)
         │
         ▼
Compare scan results against
indexed surfaces table
         │
         ├─ Unindexed docs found
         │  → "Add to surfaces table?"
         │
         ├─ Diff range provided
         │  → Match against surfaces
         │  → Flag stale/incomplete
         │
         └─ Domains without docs (5+ files)
            → Suggest new pages
         │
         ▼
Walk surfaces with user:
  Stale → [y] Yes / [l] Later / [r] Remind / [s] Skip
  Incomplete → same options
  Missing → [n] New (generate full page) / [s] Skip
         │
         ▼
For "Yes" and "New":
  Skill generates content
  (ERDs, flowcharts, prose, tables)
  Stages files
  Offers to add new pages to surfaces table
         │
         ▼
Summary: edited / deferred / skipped / created
```


## Non-goals


- Replacing `atomic-prose`. That skill owns voice/tone for narrative docs. `atomic-documentation` owns discovery, classification, and routing. The two collaborate: documentation says "update this section in prose voice", prose writes it.
- Auto-committing documentation changes. Edits are staged; the user commits via a ship verb.
- Generating documentation from scratch for an undocumented project. The skill helps *maintain* docs as code changes. Bootstrapping a docs tree from zero is a different workflow (closer to `/atomic-plan` territory). Projects with zero doc files get "no doc impact" until the user runs `/documentation` to bootstrap.
- Supporting non-markdown documentation formats (Confluence, Notion, Google Docs). Markdown files in the repo only.
- Enforcing a specific documentation structure. The skill discovers what exists and works with it. It doesn't impose `docs/architecture/` or `docs/api/` conventions — those are the project's choice.
- "Missing" detection during commit flow. Reserved for explicit `/documentation` invocation.


## Migration from current state


The change is backward-compatible:

1. **Existing override tables still work.** A user who already has `## Documentation surfaces` in their CLAUDE.md keeps their routing. The bootstrap step detects the existing table and skips.
2. **Ship verb integration is unchanged.** They invoke the skill, parse the YAML output, walk surfaces. The YAML shape gains `impact_type` (stale/incomplete); unknown fields are already ignored by the parser contract.
3. **Atomic-specific routing moves.** The hardcoded table in `skills/atomic-documentation/SKILL.md` moves to this repo's `claude.local.md`. The skill file shrinks. Users of other repos are unaffected.
4. **New binary subcommands.** `atomic docs scan` and `atomic docs stale` are additive. Existing `atomic` installations without these commands degrade gracefully — the skill falls back to a filesystem walk via Bash (same as signals fallback).
5. **The walk step gains options.** "Yes/Later/Remind/Skip" replaces "edit/skip/continue." The new options are strictly more capable — "edit" maps to "Yes," "skip" maps to "Skip," "continue" maps to "Skip."


## Strategist review notes


The design was reviewed by `atomic-strategist` (Opus, read-only). Key findings incorporated:

- **Split maintenance from authoring.** Original design conflated fast commit-flow flagging with slow content generation. Now two explicit modes.
- **Deterministic cache generation.** Original proposed LLM-mediated scope extraction. Now uses `atomic docs scan` (Go binary, heading extraction, O(ms)).
- **No "missing" in commit flow.** Originally a core classification in all modes. Now gated to authoring mode only.
- **Index in committed CLAUDE.md, not hidden cache.** Strategist noted that semantic matching at commit time adds unbounded LLM cost. Indexed table eliminates this — matching is deterministic against the "Covers" column. Table goes in committed CLAUDE.md (not gitignored `claude.local.md`) so the team shares it.
- **Remove atomic-specific routing from the skill.** Eliminates fragile project-detection heuristics. This repo uses `claude.local.md` for its surfaces table (because CLAUDE.md is the bundle source), but that's a repo-specific choice, not a skill concern.
- **`.signalsignore` alignment.** Doc discovery respects the same ignore rules as signals to avoid surfacing docs the user deliberately excluded.
- **Testing strategy.** `atomic docs scan` and `atomic docs stale` are deterministic Go code with testable inputs/outputs. The content generation step (LLM-mediated) is tested via the existing review pipeline — `atomic-reviewer` verifies the edit is correct.

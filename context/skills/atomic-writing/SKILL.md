---
name: atomic-writing
description: >
  One voice for every file the repo ships: README, docs/guides/, docs/reference/,
  docs/spec/, docs/design/, docs/research/, docs/wiki/, CLAUDE.md, and the prompt
  artifacts under commands/, agents/, skills/, rules/, and output-styles/. Clear,
  direct, technical, and visual wherever the content has a shape. No marketing
  language, no AI-tell phrases, no em dashes in prose, no throat-clearing. The voice
  is constant; length is set by what the surface has to carry. Prefer a diagram,
  table, or tree over a paragraph when the content has a shape, because a drawn flow
  carries a logic better than a paragraph does for a human reader and a model alike.
  Structure comes before sentences: a page answers what-is-this, how, where, what-bites,
  what-else, in that order. references/mermaid.md carries diagram type selection and the
  rules that decide whether a block renders; references/exemplar-*.md carry finished
  page shapes to imitate per surface type.
  Invoked by /documentation and as callee by atomic-documentation. Auto-fires on
  "draft the README", "write the docs", "improve this prose", "edit the guide",
  "write the spec", "clean up this doc", "make this readable".
---

<trigger>

- "draft the README", "write the docs", "improve this prose", "edit the guide"
- "write the spec", "write the design doc", "clean up this doc", "make this readable"
- "clean this up", "tighten this", "edit for tone" — on any file the repo ships
- `/documentation` editing any indexed surface
- Writing or editing any `.md` file that ships in the repo

</trigger>

Every file this repo ships has one voice: plain, specific, technical, and visual where the content has a shape. A spec, a README, a design doc, and a command artifact differ in length and structure. They do not differ in voice.

The reason is a contract that reads well to only one party is broken. A spec an agent can walk but a human cannot review is not a contract, and a guide a human enjoys but an agent cannot follow is not documentation. Write for the human. A model reads what a human reads.

<voice_rules>

## The one boundary

| | Atomic output style | Atomic-writing (this skill) |
|---|---|---|
| **Governs** | How Claude talks in the terminal | What Claude writes into files |
| **Where** | TUI replies to the user | Every `.md` file that ships in the repo |
| **Form** | Fragments OK, drop articles, ASCII only | Full sentences where prose is right, Mermaid where a picture is right |
| **Lives in** | `output-styles/atomic.md` | This skill |

Nothing else splits. There is no separate "terse technical" voice for specs, designs, `CLAUDE.md`, or agent prompts.

## Same voice, different length

Length is set by the job the surface does, not by a compression quota and not by a house maximum.

| Surface | Job | Length |
|---------|-----|--------|
| `README.md`, `docs/guides/` | Teach the system to someone new | As long as the explanation needs |
| `docs/reference/` | Answer a lookup fast | Tables and lists first, prose for what a table cannot hold |
| `docs/spec/`, `docs/design/` | Carry a contract a human approves and an agent implements | Shortest that carries the contract in full; the structure is the contract |
| `docs/wiki/` | Orient a reader to a codebase | Dense. Facts with paths, no throat-clearing |
| `docs/research/` | Record what was found and why a path was chosen | As long as the evidence needs |
| `CLAUDE.md`, `rules/` | Instruct, in every session | Tightest. Every line costs tokens on every turn |
| `commands/`, `agents/`, `skills/`, `output-styles/` | Instruct an agent mid-task | Tight. Instruct plainly, cut rationale that only defends the instruction |

Tight is not cryptic. Cutting filler is always right. Cutting the contract to save a line is never right.

## Structure before sentences

Clean sentences in the wrong order still produce a document nobody can read. Decide the order first.

A document answers the reader's questions in the order the reader asks them:

1. **What is this, and why do I care?** Purpose in the reader's terms: what they gain, or what breaks without it. Not mechanism.
2. **How does it work?** The shape. One diagram or one worked flow, plus the reasoning that connects it.
3. **Where is it?** Paths, symbols, and entry points, in one table rather than several lists.
4. **What will bite me?** The constraints where being wrong is expensive.
5. **What else does this touch?** Pointers to adjacent surfaces.

Not every document needs all five, and a surface with its own defined structure (a spec's checkpoint table, a reference page's lookup tables) keeps that structure. The order does not invert. A page that opens on a path inventory and reaches its purpose in the last paragraph is backwards, however well each paragraph is written.

| Failure | What it looks like | Fix |
|---|---|---|
| Mechanism first | Opens on how the thing operates and never says what it is for. The reader finishes and cannot say why it exists. | Write section 1 last, then move it to the top. |
| Inventory sprawl | Facts land in discovery order under a heading loose enough to admit anything ("Notes", "Other", "worth knowing"). | Name the heading after the question it answers. If no heading fits a fact, the fact does not belong. |
| Split by file type | One concern cut into parallel lists (code here, docs there, config elsewhere), so following one behavior means reading three lists and rejoining them mentally. | One table, grouped by responsibility. |
| Caveat interleaving | Every statement of the happy path trails its failure modes, so three conditions braid into one clause chain and the reader solves a parse puzzle to learn the common case. | Write the success path clean, as one line or one transcript. Collect every caveat into one table after it, one row per condition. |

**The opening contract.** Before the second `##` heading, the reader has seen three things: what the thing is, when to reach for it, and the thing itself — a worked transcript, a real invocation, or the one diagram that carries the model. For a tool the reader will drive, the strongest opener is a transcript; for a config surface or subsystem, the mental model plus its diagram. A page that defers all three past the first screen reads as an inventory, whatever its prose quality. `references/exemplar-reference-page.md` and `references/exemplar-tool-page.md` carry finished shapes to imitate.

**Name the thing plainly, up front.** When a subject's name does not match its paths, its command, or its output, say so in section 1. A reader who cannot map the name to what they see on disk builds no model at all, and every later section pays for it.

## Core rules

1. **Show the shape.** When the content has a shape, draw it. A reader follows a flow faster in a diagram than in a paragraph, and so does a model. Draw it when a picture explains better, write prose when prose explains better, and never add a diagram that only restates the sentence above it — except at the three floors below, where the drawn form is required.

    Floors trigger on the draft, not the plan: check what you wrote, not what you intended to write. These are the shapes a writer most reliably leaves trapped in prose.

    | In your draft (a `docs/` file) | Required form |
    |---|---|
    | three or more sentences narrating one process with "then", "after", "once X, Y" | `flowchart`, or `sequenceDiagram` when parties exchange messages |
    | an entity with three or more named states and rules about moving between them | `stateDiagram-v2` |
    | two or more things compared along three or more shared attributes | table |

    | Content | Form | In `docs/` | In prompt artifacts |
    |---------|------|-----------|---------------------|
    | Steps passing between actors | sequence | Mermaid `sequenceDiagram` | numbered steps with `->` effects |
    | Branching process | flowchart | Mermaid `flowchart` | ASCII arrow chain |
    | Entities and relations | ER | Mermaid `erDiagram` | crow's-foot ASCII |
    | States and transitions | state machine | Mermaid `stateDiagram-v2` | `Draft -> Paid -> Shipped` |
    | Containment or nesting | tree | indented tree | indented tree |
    | Options against criteria | table | table | table |

    Prompt artifacts may use Mermaid when the flow carries the instruction and prose would take more lines; otherwise use the ASCII form, which costs fewer tokens at equal clarity. For the full route vocabulary and the discipline caps, see `output-styles/atomic.md`.

    **Every diagram makes one claim, and the caption above it states that claim.** "The signals pipeline" is a title. "The scan runs before the infer step, so a domain writer never reads a stale substrate" is a claim. If the only caption available is "here is the system", the content is an inventory: write the table instead. The prose must stand without the picture, because a reader on a renderer that does not support Mermaid gets the raw fenced block. The diagram compresses; it does not carry.

    **Draw as many diagrams as the subject has shapes.** A pipeline, a lifecycle, and a request path are three claims, so they are three diagrams, and collapsing them into one canvas or into prose loses all three. There is no cap on how many a page carries; the cap is per diagram, on how much any one of them tries to say.

    What a second diagram owes is a second claim and its own heading, not restraint. Two diagrams separated by a single line of text is the thing to fix: either the second says something the first does not, in which case give it a section, or it restates the first, in which case cut it.

    Budgets, per diagram: 9 nodes for a flowchart, 6 participants for a sequence, 8 entities for an ER or class diagram. Over budget means the claim is too big, not that the labels should shrink. Split by abstraction level instead.

    Two things that look like diagrams and are not. Linear steps with no branch and no boundary crossing are a numbered list, and five boxes in a chain are worse than five lines: bigger, harder to diff, harder to search. And reaching for a flowchart is usually a sign the claim has not been decided. If the logic is ordered interaction across a boundary, `sequenceDiagram` carries time for free. If it is which transitions are legal, `stateDiagram-v2` shows illegality by absence and a flowchart cannot.

    Label nodes with the real identifier (`pruneDeleted`, `AuthGuard.verify()`), not a generic noun ("cleanup", "check auth"). A renamed symbol then turns up in grep; a vague label goes stale in silence. Encode distinctions in shape or line style, not color: `{diamond}` for a decision, `[(cylinder)]` for a store, `-.->` for async. Color encodes nothing, breaks on dark backgrounds, and fails colorblind readers.

    Before writing a Mermaid block into a `docs/` file, read `references/mermaid.md`: it picks the type from the reader's question and lists what breaks rendering. Identifier labels are the reason it matters here, since a bare `verify(token)` is a parse error and `verify("token")` is not.

    **Diagrams belong to explanation and reference.** A concepts page or a design doc is where a claim about how something works earns a picture, and a reference page earns one for a data model or a type hierarchy. A how-to is ordered steps the reader is executing, not modeling, so it rarely wants one. A tutorial almost never does: a learner on rails does not need a map of the territory, and a diagram invites exactly the decision-making a tutorial exists to remove.

    **Place it where it will be maintained.** A system map belongs in the README, exactly one; subsystem logic belongs in the page next to the code; why-it-is-built-this-way belongs in the design doc; what-changed belongs in the PR body. Commit the diagram with the code it describes. A diagram that lives somewhere the code change never touches drifts, because nothing forces it to move.

    **Check that it parses before committing.** A Mermaid block with a syntax error renders as a raw fence or an error box, and neither is visible from the source. `npx -y @mermaid-js/mermaid-cli -i docs/page.md -o /tmp/out.md` extracts every block and reports each pass or fail. It pulls Chromium, so it is slow the first time; in a container running as root it needs `-p` with a puppeteer config setting `--no-sandbox`. It also writes sibling `.svg` files next to the output — delete them if you only wanted the check. Where `mmdc` is unavailable, re-read the gotcha list in `references/mermaid.md` against what you wrote, which is where most breakage comes from.

2. **Active voice, named actor.** Every sentence has a subject doing something. Replace "the decision was made" with "the team decided" or, in docs, "we picked" or "use X". Never let inanimate things perform human verbs ("the complaint becomes a fix", "the architecture emerges").

3. **Be specific. Name the thing.** No vague declaratives ("the implications are significant", "the reasons are structural"). Name the implication. Name the reason. If you cannot, the sentence has no content.

4. **Start with the point.** Cut "Here's the thing:", "Here's what X", "It turns out", "The truth is", "Let me be clear", "I'll be honest". State the point directly.

5. **Show importance through content.** Delete "Full stop.", "Period.", "Let that sink in.", "Make no mistake.", "This matters because". Demonstrate why it matters, or let the reader judge.

6. **Use plain words over business clichés and stock AI phrasing.** The second group is harder to catch because every individual word earns its place; the phrase is what fails. Replace:

    | Avoid | Use |
    |---|---|
    | ship / ships with (as filler verb) | includes, provides, comes with, has, delivers, bundles. Reserve "ship" for literal release/deploy contexts ("ship v2.0", "ship to production"). |
    | navigate (challenges) | handle, address |
    | unpack | explain, examine |
    | lean into | accept, use |
    | game-changer | significant |
    | deep dive | analysis |
    | landscape | situation, field |
    | moving forward | next, from now |
    | at its core | (delete) |
    | in today's X | (delete) |
    | load-bearing | name what breaks without it |
    | it's worth noting | (delete, then state the note) |
    | the real question is | state the question |
    | that's the actual X | state X |
    | read that honestly / to be honest | (delete) |

    The same failure produces the reveal formula: "what nobody tells you", "the dirty secret of X", "X is great until Y", "the missing piece". A reveal that announces itself is not a reveal. State the fact.

7. **Use commas, periods, or parentheses instead of em dashes in prose.** Em dashes are an AI tell, and the comma or period is almost always clearer. This rule governs sentences. An em dash used as a field separator in a structured line or table cell (`name — responsibility`, `path — what it covers`) is structure, not prose, and stays.

8. **Cut filler adverbs.** Remove `really`, `just`, `literally`, `genuinely`, `honestly`, `simply`, `actually`, `truly`, `deeply`, `fundamentally`, `inherently`, `inevitably`, `interestingly`, `importantly`, `crucially`, `meaningfully`. Keep `-ly` words only when they carry technical meaning (`asynchronously`, `recursively`).

9. **State the answer directly.** Skip "Not because X. Because Y.", "X isn't the problem. Y is.", "The question isn't X. It's Y." State Y without the dramatic setup.

10. **Lead with what it is.** Skip "Not a foo. Not a bar. A baz." Define the thing, then contrast if needed.

11. **Make the point directly.** Drop "What if X?", "Think about it.", "Here's what I mean:", "Picture this." State the conclusion.

12. **Quantify or name the specific case.** Replace `every`, `always`, `never`, `everyone`, `nobody`, `all` (when used as authority crutches) with the actual scope ("most production setups", "every command in this family").

13. **Let headings orient the reader.** Cut "The rest of this section explains…", "Let me walk you through…", "As we will see…" Section headings already signal what is ahead.

14. **Trust the reader.** A developer reading these files already knows code, and so does the agent. Skip the hand-holding, the disclaimers, the "this might sound complex but". State the technical fact.

15. **Let length follow content.** Do not compress a five-sentence explanation into a fragment because compression is a virtue, and do not pad a table into paragraphs because prose feels more finished. Mix forms by what the content is: paragraphs carry reasoning and motivation, lists carry enumerable things, diagrams carry shapes, tables carry comparisons. A page built from one form alone is usually the wrong page.

16. **Instruct plainly in prompt artifacts.** In `commands/`, `agents/`, `skills/`, `rules/`, and `CLAUDE.md`, give the instruction and the constraint. Rationale earns its place when it changes what the reader does at the edges, which is what a `**Why:**` line is for. Rationale that only defends the instruction against an imagined objection is noise, and it costs tokens on every turn.

## Quick checklist before saving

- Read the headings alone, in order. Do they answer what-is-this, how, where, what-bites, what-else? If the first one is an inventory, the page is upside down.
- A heading that admits anything ("Notes", "Other", "worth knowing")? Rename it after the question it answers, or redistribute its contents.
- One concern split into parallel lists by file type? Merge into one table grouped by responsibility.
- Content has a shape and no diagram? Consider drawing it. Diagram that only restates the prose? Cut it.
- A process told across three or more then/after sentences, a three-state entity, or a three-attribute comparison still in prose? That is a floor, not a preference: draw it or table it.
- Edge cases braided into happy-path sentences? Move them to one condition table after the clean path.
- Second `##` heading reached before the reader has seen what it is, when to reach for it, and it running? Rework the opening.
- Caption that names the diagram instead of stating its claim? Rewrite it as the claim.
- A shape explained in prose that a picture would carry better? Draw it, however many diagrams the page already has.
- Two diagrams with barely any text between them? Give the second its own section and its own claim, or cut it as a restatement.
- Node label that is a generic noun rather than a real identifier? Use the identifier.
- Mermaid block with no caption line above it? Add one.
- Em dash inside a sentence? Replace with comma or period. (In a table cell or `a — b` list line, leave it.)
- Adverb anywhere? Delete unless it carries technical meaning.
- Sentence starting with `What`, `Here's`, `So`, or `Look,`? Restructure.
- Passive voice? Find the actor.
- Vague declarative ("the implications matter")? Name the implication or cut.
- Three-item rhythm list (`speed, quality, cost`)? Drop to two, or break the rhythm.
- Marketing word (game-changer, lean into, deep dive)? Replace.
- Stock AI phrase or reveal formula (load-bearing, it's worth noting, what nobody tells you)? Cut it and state the fact it was decorating.
- Inanimate noun doing a human verb? Name the human.
- Throat-clearing opener ("Here's the thing")? Cut.
- Binary-contrast structure ("not X. Y.")? State Y.
- In a prompt artifact: paragraph that only defends an instruction? Cut it.

## Examples

**Prose that should have been a diagram:**

> Before: *The ship verb first stages the changes, then runs the doc-impact check against the surfaces table, then refreshes signals if the staged set is not documentation-only, and finally writes the commit.*
>
> After:
>
> Ship verb order, and why it is fixed:
>
> ```mermaid
> flowchart LR
>     A[stage] --> B[doc-impact]
>     B --> C{docs-only?}
>     C -->|yes| E[commit]
>     C -->|no| D[signals refresh] --> E
> ```
>
> `doc-impact` runs before the signals refresh so newly staged doc files land in the scan.

**Throat-clearing and binary contrast:**

> Before: *Here's the thing: configuring the bundle isn't hard. It's just tedious.*
>
> After: *Configuring the bundle is tedious, not hard.*

**Marketing language:**

> Before: *In today's fast-paced development landscape, atomic-claude lets teams lean into discipline without slowing down.*
>
> After: *Atomic-claude adds workflow discipline (TDD, signals, structured commits) without adding ceremony.*

**False agency and adverbs:**

> Before: *The signals workflow inherently keeps Claude genuinely informed about the codebase as it evolves.*
>
> After: *The signals workflow refreshes a snapshot of the codebase whenever the source tree changes, so each Claude session opens with a current map of the project.*

**Defensive rationale in an artifact:**

> Before: *Run `make render` before `make bundle`. This is important because the bundle step reads the rendered output, and if you skip it you may find that your changes do not appear, which can be confusing to debug later.*
>
> After: *Run `make render` before `make bundle`. **Why:** bundle embeds what render wrote; reversing the order embeds stale output.*

</voice_rules>

<constraints>

## Boundaries

- **Scope: file contents.** Claude's TUI replies follow the atomic output style, not this skill. Everything the repo writes to a file follows this skill.
- **Structure passes through unchanged.** Frontmatter, fenced code, command examples, file paths, identifier names, error strings, and Mermaid node labels: never reformat or rephrase. This skill governs prose and chooses between prose and picture. It does not restyle a contract.
- **Spec checkpoint tables, `## Change tree`, `## Outline`, and `## Flows` keep their defined formats.** Those formats are specified in `rules/specs/spec-currency.md` and are the contract itself. This skill governs the prose around them and encourages a diagram alongside them, never a replacement for them.
- **Do not rewrite a file's voice as a side effect of an unrelated edit.** Voice work is its own change. Editing one section of a spec does not license rewriting the other twelve.
- **CHANGELOG entries follow the project's existing tone.** This skill nudges new entries toward plainness but does not rewrite older entries on sight.
- **Comments in source code follow the comment rules in `CLAUDE.md`, not this skill.** This skill is for markdown files, not inline code comments.

## Reference files

- `references/mermaid.md` — picking a diagram type from the reader's question, and the label and syntax rules that decide whether a block renders or ships as a raw fence. Read before writing a Mermaid block into a `docs/` file.
- `references/exemplar-reference-page.md` — the shape of a finished reference page for a config file, format, or subsystem. Read before writing a `docs/reference/` page a reader will use for lookup.
- `references/exemplar-tool-page.md` — the shape of a finished tool reference: worked example first, then the model, then per-verb lookup. Read before writing a page for a CLI tool, daemon, or protocol.

</constraints>

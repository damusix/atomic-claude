---
name: atomic-prose
description: >
  Voice and tone rules for *enduring narrative documentation* — README.md, docs/guides/,
  CHANGELOG narrative entries, and any other long-form human-facing markdown that ships
  in the repo. Clear, direct, technical narrative. No marketing language, no AI-tell
  phrases, no em dashes, no throat-clearing. Three styles, three surfaces, never confused:
  atomic output style governs Claude's TUI replies (terse, fragments); atomic-prose
  governs enduring narrative docs (this skill); specs and design docs use a separate
  terse-structured convention (tables, diagrams, brevity-first — they live and die by
  token cost). Invoked by /documentation when editing README or guides. Invoked as callee
  by `atomic-documentation` when surface is human-facing prose. Auto-fires on
  "draft the README", "write the docs", "improve this prose", "edit the guide".
---

Enduring narrative documentation has its own voice. It is neither the terse atomic style we use in TUI replies, nor the table-and-diagram brevity we use in specs and design docs, nor the cadenced essay voice common in AI-generated prose. It is plain, specific, technical narrative. Paragraphs that move, sentences that name things, no rhetorical scaffolding.

Run these rules when writing or editing prose in `README.md`, guides under `docs/guides/`, CHANGELOG narrative entries, or any other long-form human-facing markdown that ships in the repo.

**Do NOT apply this skill to `docs/spec/` or `docs/design/`.** Those files follow a separate convention enforced by `/atomic-plan`: table-first, diagrams allowed, prose kept terse and to the point. Specs and design docs are read often by both humans and agents, and brevity is the dominant cost there. Atomic-prose narrative would inflate them without adding value. See `commands/atomic-plan.md` for the spec/design voice.

## What this skill is, and what it is not

| | Atomic (TUI style) | Atomic-prose (this skill) | Spec / design voice | Marketing slop (avoid) |
|---|---|---|---|---|
| **Where** | Claude's TUI replies to the user | README, docs/guides/, CHANGELOG narrative | `docs/spec/`, `docs/design/` | Anywhere |
| **Form** | Fragments OK, drop articles, terse | Full sentences, paragraphs that flow | Tables, diagrams, terse bullets | Punchy taglines, hero copy |
| **Length** | Shortest viable | As long as needed; no shorter | Shortest that carries the contract | Whatever sounds dramatic |
| **Voice** | Imperative, telegraphic | Active, specific, technical | Imperative, declarative | Aspirational, promissory |
| **Em dashes** | Allowed in inline replies | Forbidden | Forbidden | Stuffed with them |
| **Adverbs** | Cut | Cut | Cut | Loaded |
| **Reader** | The user, mid-task, watching the terminal | A developer reading docs to understand or use the system | A human or agent who will implement, follow, or audit a contract | An imagined "audience" being sold to |

The atomic output style file (`output-styles/atomic.md`) covers TUI replies. This skill covers the *file contents* Claude writes when those files are enduring narrative docs. The spec/design voice is enforced by `/atomic-plan` and lives in `commands/atomic-plan.md`. The three styles do not contradict; they apply to different surfaces.

## Core rules

1. **Active voice, named actor.** Every sentence has a subject doing something. Replace "the decision was made" with "the team decided" or, in docs, "we picked" or "use X". Never let inanimate things perform human verbs ("the complaint becomes a fix", "the architecture emerges").

2. **Be specific. Name the thing.** No vague declaratives ("the implications are significant", "the reasons are structural"). Name the implication. Name the reason. If you cannot, the sentence has no content.

3. **No throat-clearing openers.** Cut "Here's the thing:", "Here's what X", "It turns out", "The truth is", "Let me be clear", "I'll be honest". State the point.

4. **No emphasis crutches.** Delete "Full stop.", "Period.", "Let that sink in.", "Make no mistake.", "This matters because". Either show why it matters by demonstrating it, or accept that the reader can judge importance.

5. **No marketing jargon.** Replace business clichés with plain words:

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

6. **No em dashes.** Use commas, periods, or parentheses. Em dashes are an AI tell, and the comma or period is almost always clearer anyway.

7. **No adverbs.** Cut `really`, `just`, `literally`, `genuinely`, `honestly`, `simply`, `actually`, `truly`, `deeply`, `fundamentally`, `inherently`, `inevitably`, `interestingly`, `importantly`, `crucially`. Cut every `-ly` word unless it carries technical meaning (`asynchronously`, `recursively`).

8. **No binary-contrast rhetoric.** Avoid "Not because X. Because Y.", "X isn't the problem. Y is.", "The question isn't X. It's Y.", "It's not X, it's Y." State Y directly.

9. **No negative listing.** Skip "Not a foo. Not a bar. A baz." Start with what it is.

10. **No rhetorical setups.** Drop "What if X?", "Think about it.", "Here's what I mean:", "Picture this." Make the point.

11. **No lazy extremes.** `every`, `always`, `never`, `everyone`, `nobody`, `all` — when used as authority crutches. Replace with the specific case or quantify ("most production setups", "every command in this family").

12. **No meta-commentary about the doc itself.** Skip "The rest of this section explains…", "Let me walk you through…", "As we will see…". Section headings already tell the reader what is ahead.

13. **Trust the reader.** A developer reading these docs already knows code. Skip the hand-holding, the disclaimers, the "this might sound complex but". State the technical fact.

14. **Keep technical density.** Unlike atomic TUI replies, doc prose can run long when the content requires it. Do not compress a five-sentence explanation into a fragment because compression is a virtue. Compression in docs is only a virtue when it removes filler, not when it removes content.

15. **Keep some narrative.** Paragraphs that connect ideas are good. A README built entirely from bullet lists reads like a spec, not like documentation. Mix lists (for enumerable things) with paragraphs (for reasoning and motivation).

## Quick checklist before saving a doc edit

- Em dash anywhere? Replace with comma or period.
- Adverb anywhere? Delete unless it carries technical meaning.
- Sentence starting with `What`, `Here's`, `So`, or `Look,`? Restructure.
- Passive voice? Find the actor.
- Vague declarative ("the implications matter")? Name the implication or cut.
- Three-item rhythm list (`speed, quality, cost`)? Drop to two, or break the rhythm.
- Marketing word (game-changer, lean into, deep dive)? Replace.
- Inanimate noun doing a human verb? Name the human.
- Throat-clearing opener ("Here's the thing")? Cut.
- Binary-contrast structure ("not X. Y.")? State Y.
- Meta-joiner ("As we will see…")? Delete.

## Examples (before / after)

**Throat-clearing + binary contrast:**

> Before: *Here's the thing: configuring the bundle isn't hard. It's just tedious.*
> After: *Configuring the bundle is tedious, not hard.*

**Marketing language:**

> Before: *In today's fast-paced development landscape, atomic-claude lets teams lean into discipline without slowing down.*
> After: *Atomic-claude adds workflow discipline (TDD, signals, structured commits) without adding ceremony.*

**False agency + adverbs:**

> Before: *The signals workflow inherently keeps Claude genuinely informed about the codebase as it evolves.*
> After: *The signals workflow refreshes a snapshot of the codebase whenever the source tree changes, so each Claude session opens with a current map of the project.*

**Em dash + rhetorical setup:**

> Before: *What if you could ship a feature without ever leaving the terminal — and have it reviewed automatically?*
> After: *The `/subagent-implementation` command runs an implement-review loop without leaving the terminal. A reviewer agent gates each iteration.*

## Boundaries

- **Tables and code blocks pass through unchanged.** This skill governs prose. Frontmatter, fenced code, command examples, file paths, identifier names, error strings: never reformat or rephrase.
- **Spec checkpoint tables and design alternative tables pass through unchanged.** Their structure is the contract. Only the surrounding prose (Goal, Problem, Rationale) is in scope.
- **CHANGELOG entries follow the project's existing tone.** This skill nudges new entries toward plainness but does not rewrite older entries on sight.
- **Comments in source code follow the global comment rules in `CLAUDE.md`, not this skill.** This skill is for documentation files, not inline code comments.

## When to invoke

Auto-fires when:

- The user asks to draft, edit, or improve `README.md`, files under `docs/guides/`, or other narrative human-facing markdown.
- `/documentation` is updating README or a guide.
- The user pastes a draft and says "clean this up", "tighten this", "edit for tone" — and the target is narrative prose, not a spec/design table.

Does *not* fire when:

- Claude is replying to the user in the TUI (atomic output style governs that).
- Editing `docs/spec/` or `docs/design/` — those follow the spec/design voice in `commands/atomic-plan.md` (tables, diagrams, terse).
- Editing pure structure (tables, frontmatter, code blocks).
- Editing files in `.claude/`, `agents/`, `commands/`, `skills/`, `output-styles/` — those have their own voice rules (atomic style for replies, descriptive technical prose for instructions, neither marketing nor essay).

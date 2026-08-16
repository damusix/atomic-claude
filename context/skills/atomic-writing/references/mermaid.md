# Mermaid: choosing a type, and what breaks rendering

Read this before writing a diagram into a `docs/` file. It covers the two things that go wrong that knowing Mermaid syntax does not prevent: picking the type that does not carry the claim, and writing a label the parser rejects.

The diagram contract itself (one claim per diagram, the caption states it, node budgets, real identifiers, shape over color) lives in the skill body. This file is the mechanics.

## Choosing the type

Match the reader's question, not the subject matter. The same subsystem is four different diagrams depending on what is being asked.

| Reader's question | Type |
|---|---|
| What happens, and where does it branch? | `flowchart` |
| Who talks to whom, in what order, across a boundary? | `sequenceDiagram` |
| What states exist, and which transitions are legal? | `stateDiagram-v2` |
| What entities exist, and how do they relate? | `erDiagram` |
| What are the types, and how do they inherit or compose? | `classDiagram` |

Reaching for a flowchart is the common failure, because it is what gets picked when the claim has not been decided. Two cases where it is the wrong answer:

- Ordered interaction between separate processes. `sequenceDiagram` carries time down the page for free; a flowchart makes the reader infer it from arrow direction.
- Which transitions are legal. `stateDiagram-v2` shows an illegal transition by its absence, and a flowchart has no way to express "this cannot happen".

Types outside that table (`journey`, `gantt`, `mindmap`, `quadrantChart`, `timeline`, `sankey`, `gitGraph`) rarely belong in these docs. The `-beta` types (`architecture-beta`, `block-beta`, `packet-beta`) and `C4Context` may not render on the target platform. For a layered-architecture picture use a `flowchart` with `subgraph` boundaries, which renders everywhere and keeps layout under your control.

## What breaks rendering

**Punctuation in a label needs quotes.** Parentheses, brackets, braces, commas, colons, and `#` all break a bare label. The skill body asks for real identifiers in labels, and real identifiers carry parens, so quote by default:

```
A[verify(token)]        breaks
A["verify(token)"]      renders
```

**`end` is reserved.** A lowercase `end` as a node ID or a bare label kills a flowchart, because the parser reads it as the `subgraph` terminator. Write `End`, `END`, or quote it: `E["end"]`.

**Node ID and label are separate things.** In `Guard["AuthGuard.verify()"]`, `Guard` is the ID that edges reference and the bracketed string is what renders. Keep IDs short and stable and put the churn in the label, so a rename touches one line rather than every edge.

**Line breaks are `<br/>`.** A literal newline inside a label does not work.

**Use `flowchart`, not `graph`.** `graph TD` still parses but is the legacy keyword with older layout behavior.

**An edge label starting with `x` or `o` changes the arrowhead.** `A --x B` is cross-ended and `A --o B` is circle-ended. When the label genuinely starts with one of those letters, use the quoted form: `A -- "x axis" --> B`.

**Comments are `%%` at line start** and do not render. Use one to record which files a diagram was drawn from, so a reviewer can check it:

```
flowchart TD
    %% source: atomic/internal/signals/signals.go, tree.go
```

**No `click` handlers.** GitHub sanitizes them. Put the link in the prose around the diagram.

**Semicolons are optional.** Leave them out; they add diff noise.

**Never hardcode a color to carry meaning.** GitHub and `atomic serve` both render in light and dark themes, so a hardcoded `fill:#fff` vanishes in one of them. Encode the distinction in shape (`{diamond}` for a decision, `[(cylinder)]` for a store) or line style (`-.->` for async). If a `classDef` is unavoidable, name it semantically (`classDef external`) and check both themes.

**Markdown strings inside labels need Mermaid 10+** on whatever renders the page. Skip them here.

**Accessibility.** On a diagram carrying real weight, add `accTitle` and `accDescr`:

```
flowchart TD
    accTitle: Signals refresh pipeline
    accDescr: A code scan writes the substrate, a model pass writes the router and domain files, and a code pass linkifies them.
```

## Where it renders

Renders natively: GitHub (files, issues, PRs, wikis, gists), GitLab, Obsidian, MkDocs Material, Docusaurus, and `atomic serve`.

Does not: npm, PyPI and other package registries, plain-text terminals, many editors.

The fence language must be exactly lowercase `mermaid`. Platforms pin different Mermaid versions, so newer syntax can render in the live editor and fail on the target.

**Where a doc travels outside a Mermaid-rendering host** (a README that ships to npm, for example), the fenced block is what that reader gets. This is why the skill body requires the surrounding prose to stand on its own: the diagram compresses an explanation, it does not replace one.

# Mermaid: choosing a type, and what breaks rendering

Read this before writing a Mermaid block in docs or prompt artifacts. Choose a form that carries the claim, then check its layout and syntax.

## The diagram contract

Give each diagram one claim and a caption above it that states the claim. Put conditions and outcomes in the labels and arrows so the raw block remains legible. Use prose for reasoning, exact commands, and exceptions that the diagram cannot show clearly, not a second narration of the workflow.

Use as many diagrams as the subject needs. Each additional diagram needs its own claim and heading; cut restatements. Label nodes with real identifiers such as `AuthGuard.verify()`, not generic labels such as “check auth.” Encode distinctions in shape or line style, not color.

Keep each flowchart to 9 nodes, each sequence to 6 participants, and each ER or class diagram to 8 entities. Split a larger claim by abstraction level. Place the diagram where its subject is maintained: one system map in the README, subsystem logic near its code, design rationale in the design, and changes in the PR body. Commit diagrams with the code they describe.

Straight steps without branches or boundary crossings belong in a numbered list. Use diagrams in how-tos only when readers need to choose a path or understand a gate.

Before committing, check parsing with `npx -y @mermaid-js/mermaid-cli -i docs/page.md -o /tmp/out.md`. The first run downloads Chromium; containers running as root need a Puppeteer config with `--no-sandbox` passed via `-p`. Remove sibling SVG files if they were only for the check. If `mmdc` is unavailable, compare the block against the rendering gotchas below.

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

`journey`, `mindmap`, `timeline`, `gantt`, `pie`, `quadrantChart`, `sankey`, and `gitGraph` are not used in these docs: each paints a fixed palette the host theme does not restyle, so a diagram that renders fine in light mode goes illegible in dark mode. Replacements for the three that come up most:

- `journey` → `sequenceDiagram` when parties exchange messages, otherwise a table.
- `mindmap` → an indented tree in a fenced block.
- `timeline` → a table, one row per event.

The `-beta` types (`architecture-beta`, `block-beta`, `packet-beta`) and `C4Context` may not render on the target platform. For a layered-architecture picture use a `flowchart` with `subgraph` boundaries, which renders everywhere and keeps layout under your control.

## Layout: direction, width, and density

The node budgets above do not guarantee a readable diagram. Check width, density, and color as well.

| Rule | Limit | Check |
|------|-------|-------|
| Direction | `TD` unless the diagram is a straight chain of at most 5 nodes with one-line labels, in which case `LR` is allowed | Count the nodes in the chain; a branch or a chain over 5 nodes forces `TD` |
| Side by side | At most 4 nodes at the same depth; a node fans out to at most 4 | Count the nodes sharing a rank, and count each node's outgoing edges |
| Label width | At most 24 characters per line, at most 3 lines, split with `<br/>` | Count characters per line in each label |
| Edges | At most 12 edges; at most 2 backward | Count the edges in the block, then count how many point to an earlier node |
| Subgraphs | At most 3, never nested | Count `subgraph` keywords in the block |
| Sequence | At most 6 participants, at most 12 messages, participant names of at most 2 words | Count `participant`/actor declarations, arrow lines, and words per name |
| ER and class | At most 8 entities, at most 5 attributes each: keys plus what the claim needs | Count entity or class blocks, and attribute lines inside each |
| Color | No `style`, `classDef`, `linkStyle`, `fill:`, or `%%{init` line | `awk '/^```mermaid/,/^```$/' <file> \| grep -nE 'fill:|^\s*(style|classDef|linkStyle) |%%\{init'` prints nothing |

Over any limit, split by abstraction level: one overview diagram with a node per stage, then one diagram per stage under its own `###` heading.

## What breaks rendering

**Punctuation in a label needs quotes.** Parentheses, brackets, braces, commas, colons, and `#` all break a bare label. Real identifiers carry parens, so quote by default:

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

**Semicolons are optional as terminators, and dangerous inside text.** Leave them off line ends; they add diff noise. Inside a sequence-diagram message a `;` splits the statement, so `I->>S: test first; report` parses `report` as a new actor and fails. Use a comma or an em dash in message text instead.

**No color to carry meaning.** No `style`, `classDef`, `linkStyle`, `fill:`, or `%%{init` line: `awk '/^```mermaid/,/^```$/' <file> | grep -nE 'fill:|^\s*(style|classDef|linkStyle) |%%\{init'` prints nothing. A hardcoded `fill:#fff` sets one half of a text/background pair and leaves the other to whichever theme GitHub or `atomic serve` renders in, which is what produces white text on a pastel fill in dark mode. Encode the distinction in shape (`{diamond}` for a decision, `[(cylinder)]` for a store) or line style (`-.->` for async) instead.

**A subgraph's `direction` is ignored once any node inside it links outside the subgraph** (Mermaid flowchart docs, "Direction in subgraphs", limitation). Nesting subgraphs to fight layout does not work; split the diagram instead.

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

**Where a doc travels outside a Mermaid-rendering host** (a README that ships to npm, for example), the reader gets the fenced block. Keep labels and arrows readable as source, and include the reasoning and constraints the diagram cannot convey in surrounding prose.

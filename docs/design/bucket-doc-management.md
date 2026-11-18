# Bucket doc management


## Problem


Buckets today are a registered folder with a name, a path, and a hand-written `index.md` stub. Everything below that is unstructured. Three consequences:

**The indexes are not built, they are remembered.** The realm `wiki/index.md` knows a bucket's name and absolute path and nothing else. A bucket's own `index.md` lists nothing — it carries a purpose line and a `## Conventions` block, both hand-written, both going stale the moment a doc is added. Neither level answers "what is in this bucket?" without a filesystem walk.

**Authoring rules live in the user's head and get re-typed.** Every time a doc is created the same instructions get repeated: one topic per file, break subtopics into a folder, summarize the folder in the sibling `.md`, put code next to the writeup, write a `CLAUDE.md` for the subtree. The rules differ per bucket — an `experiments/` entry wants a spike writeup plus runnable code, a `research/` entry wants a sourced report — and that per-bucket flavor is re-explained each session.

**There is no frontmatter contract, so nothing downstream can be derived.** Measured on a live realm (`alonso-network`, 2026-07-23): 0 of 20 bucket content files carry frontmatter, and no bucket `index.md` carries any either. That is not drift — `createBucketIndexStub` (`atomic/internal/wiki/bucket_registry.go:283`) emits `# <name>` plus HTML comments and nothing else. Without frontmatter there is no title, no description, no tags, no type, so any listing has to be model-written, which means it drifts silently.

The wiki already solves the equivalent problem one level up: realm members are listed deterministically by `buildMembersSection` + `DeriveMemberDescription` (`atomic/internal/wiki/wiki.go:631`), which reads a summary's frontmatter `description:` and falls back to the first prose line. That mechanism is correct and shipped. It simply stops at the bucket boundary.

Index the feature establishes, one `index.md` per folder:

```
wiki/index.md
  <wiki-bucket-list>   <- code-owned region: ## Buckets + one entry per bucket
  </wiki-bucket-list>
       |
       v  derived from each bucket's index.md frontmatter
<bucket>/index.md
  <bucket-docs>        <- code-owned region: ## Docs + one entry per direct child
  </bucket-docs>
       |
       v  derived from each child's frontmatter, or its index.md for a folder
<bucket>/<slug>/index.md             item, listing its stage folders
<bucket>/<slug>/01_raw/index.md      stage, listing its files
```


## Goals / Non-goals


- **Goals:**
    - Deterministic index at every level: the realm indexes its buckets, and every folder in a bucket indexes its direct children. Code walks and writes; the model never authors a listing.
    - A frontmatter contract for bucket docs rich enough to drive that index (`title`, `type`, `description`, `tags`, `status`, `created`).
    - `atomic wiki bucket doc <bucket> <slug>` scaffolds an item (`<slug>/index.md` with frontmatter, H1, and a lede comment, plus `<slug>/01_raw/index.md`) for the model to finish.
    - `atomic wiki bucket skill <bucket>` scaffolds a per-bucket management skill so the authoring rules are written once and auto-fire thereafter.
    - Encode the item shape: one item per slug folder, its work in numbered stage folders starting at `01_raw/`, so raw material and research share one bucket.

- **Non-goals:**
    - Rejecting or quarantining frontmatter-free files. Raw capture stays frictionless; unindexed files are listed as such, never refused.
    - Shipping per-bucket skills in the install bundle. They are realm-local and user-owned; `atomic claude install` never touches them.
    - A central tag or type vocabulary. Both stay producer-defined, consistent with OKF §4.1 and the existing `docs/spec/okf-alignment.md` non-goal "no central `type` registry".
    - Generating doc bodies. The scaffold stops after the lede.
    - A new `atomic wiki stale` line type for unindexed docs — the stale exit-code contract stays as-is this round.


## Approaches


### A. Index granularity

| # | Approach | Pros | Cons |
|---|----------|------|------|
| A1 | Per-file: one flat listing of every `.md` in the bucket | Trivial walk | A topic with 12 subtopic files produces 13 entries; the index becomes a file dump |
| A2 | Per-topic: one listing per bucket, `<slug>/` collapsing under a sibling `<slug>.md` router | Index reflects topics | Built and replaced: an agent entering a deep bucket had no map of each folder |
| A3 | **Per-folder: an `index.md` in every folder, listing its direct children** | An agent descends one index at a time and loads only the layer it needs; one listing rule at every depth; no router or orphan cases | More files written on a rebuild, so unchanged files are skipped |

### B. Where the generated listings live

| # | Approach | Pros | Cons |
|---|----------|------|------|
| B1 | Extend the `<wiki-buckets>` machine block with a `description` attr, no separate listing | One artifact; parse-back is exact | Conflates registry (identity) with presentation; block is consumed by `scan`, the `bucket` verbs, and `serve`, so a schema change ripples |
| B2 | Heading-bounded generated sections — find `## Buckets`, regenerate to the next `## ` | No delimiters to explain | **Unsafe.** A heading has a start but no end. A user typing a paragraph between entries, or after the list but before the next heading, leaves code unable to distinguish generated from authored: the rebuild either eats the prose or strands it. Same failure for a user who deletes the next heading. |
| B3 | **Explicit XML-delimited managed regions at both levels — `<bucket-docs>` and `<wiki-bucket-list>`** | The region has a real end; content outside is preserved byte-for-byte regardless of what the user types; a *visible* delimiter is self-documenting — it tells the user where not to type; matches the eight-example house style (`<atomic>`, `<wikis>`, `<wiki-scan>`, `<wiki-buckets>`, `<wiki-type>`, `<scan-sha>`, `<wiki-schema>`, `<code-index>`) | Two tags to keep paired; needs a CommonMark blank-line rule so the listing inside still renders as markdown |
| B4 | A separate generated file per bucket (`<bucket>/INDEX.md`) | No splicing logic at all | A second index file next to `index.md` is confusing; splits the bucket's meaning across two files |

B3. The delimiting decision is settled by the shipped `## Members` implementation, which is already region-delimited rather than heading-bounded (`atomic/internal/wiki/wiki.go:40-44, 850-901`): `buildMembersSection` emits the heading plus a start/end marker pair, and `rewriteMembersSection` replaces from the heading through the end marker, preserving everything outside byte-for-byte. The mechanism is right; only its *syntax* is the odd one out.

XML pseudo-tags, not HTML comments. Every other managed region in the system is an XML tag — `<atomic>` and `<wikis>` in `CLAUDE.md`, `<wiki-scan>`, `<wiki-buckets>`, `<wiki-type>`, `<scan-sha>`, `<wiki-schema>` in `wiki/index.md`, `<code-index>` in a realm root. `wiki-members`' HTML comments are a single exception, so following them would fragment the vocabulary rather than extend it. Visibility is a feature here too: an invisible `<!-- … -->` boundary is exactly what a user types past without noticing, which is the failure mode this whole decision exists to prevent. The VitePress pseudo-tag hazard documented in `repoSteeringScaffold` (`atomic/internal/wiki/init.go:20-25`) is specific to this repo sweeping its own `docs/`; a user's realm wiki and bucket folders are not fed through the Vue template compiler, and `wiki/index.md` already carries five pseudo-tags without issue.

**Blank-line rule (load-bearing).** Under CommonMark, an open tag alone on a line starts an HTML block that runs until the first blank line. A generated listing written flush against its tags renders as raw text, not a list. So the region is always emitted as open tag, blank line, content, blank line, close tag — which also happens to match the repo's markdown convention of a newline before and after lists.

**Consistency migration.** Leaving `## Members` on HTML-comment markers while `## Buckets` uses XML puts both forms side by side in one file, which reads as an accident. The spec migrates `## Members` to `<wiki-member-list>` in the same pass: a file carrying the legacy markers has the whole region rewritten to the XML form on the next `atomic wiki scan`, idempotently. This touches shipped behavior and is flagged in the spec for veto.

**Unpaired-tag degradation.** `rewriteMembersSection` today has one rung worth not copying: start marker present, end marker missing → replace from the heading to EOF, silently destroying everything after. A user who deletes one marker loses the rest of the file on the next scan. The shared primitive instead treats an unpaired tag as damage it will not guess at: leave the file untouched, report the region as unmanageable, exit non-zero on the targeted verb, warn during `scan`.

### C. Where the per-bucket skill goes

| # | Approach | Pros | Cons |
|---|----------|------|------|
| C1 | `<realm-root>/.agents/skills/<bucket>-management/` (as originally sketched) | — | **Not a discovery path.** Verified against the upstream skills doc: skills load from `~/.claude/skills/`, `.claude/skills/` in cwd and every parent up to the repository root, nested `.claude/skills/` below cwd, plus plugin and enterprise locations. A skill here would silently never load. |
| C2 | **`<realm-root>/.claude/skills/<bucket>-management/SKILL.md`** | Loads whenever the session starts at the realm root, which is where bucket work happens (buckets are realm-root siblings and every `atomic wiki` verb is realm-rooted) | The parent walk stops at the repository root, so a session started inside a member repo does not pick it up |
| C3 | `~/.claude/skills/<bucket>-management/` | Loads in every session everywhere | Pollutes the global namespace with realm-specific skills; two realms with a `research` bucket collide on one name |

C2, with the C3 behavior available on demand: the upstream doc confirms a `<skill-name>` entry in the personal or project location may be a symlink to a directory elsewhere on disk, and Claude Code follows it and de-duplicates when the same target is reachable twice. A user who wants a realm skill globally symlinks it into `~/.claude/skills/` — documented, not automated.

### D. Scaffold delivery

| # | Approach | Pros | Cons |
|---|----------|------|------|
| D1 | Extend `atomic template <name>` with bucket-doc templates | Reuses the shipped `doctemplate` package outright | `atomic template` writes to **stdout** by contract; the model then has to place the file. Loses the computed path, the collision check, and the `created:` stamp |
| D2 | **New `atomic wiki bucket doc` verb in the `wiki` package, reusing the `doctemplate` embed idiom** | Owns the computed path (`<bucket>/<slug>/index.md` and `01_raw/index.md`), refuses to clobber, stamps `created:` in code | A second embedded-template site in the tree |

The distinction that settles it: `atomic template` *emits* a skeleton, `atomic wiki bucket doc` *places* one. Placement is the whole value here — the path is derived from bucket plus slug, the scaffold creates the item folder and its first stage, and the collision check is what makes the command safe to re-run. Reuse the `//go:embed templates/*.md` idiom, not the command.


## Recommendation


A3 + B3 + C2 + D2.

The spine of the design is a rule the codebase already enforces elsewhere and that this feature extends downward: **code computes and writes every derived value; the model writes prose.** It is the same mechanism as `DeriveMemberDescription`. Applying it to bucket docs means every index is regenerated on every `atomic wiki scan` at zero model cost and cannot drift, because nothing about them is remembered.

The derivation ladders keep capture frictionless. A doc with full frontmatter gets a rich entry; a doc with none still gets a title (H1, then filename stem) and a description (first prose line), and lands under `### Unindexed` — visible, listed, never rejected. That is what lets a 16-file dump in an item's `01_raw/` coexist with its authored findings under one contract.

Sequence per item, from creation to index:

```
atomic wiki bucket doc research seo
    -> writes research/seo/index.md          (frontmatter + created: stamped, H1, lede comment)
    -> writes research/seo/01_raw/index.md   (title, description, created)
    -> refuses if research/seo exists
    -> rebuilds research/ indexes

model fills title / description / tags / body, adds stages
    <- guided by .claude/skills/research-management/SKILL.md

atomic wiki scan  (or  atomic wiki bucket index research)
    -> walks every folder under research/
    -> reads each child's frontmatter, applies the fallback ladders
    -> splices the <bucket-docs> region in each folder's index.md
    -> regenerates the <wiki-bucket-list> region in wiki/index.md
```


## Worked example


A fictional realm, `acme-labs`, with two buckets exercising every case the contract has to handle: an item with authored stages, an experiment carrying code, a stage holding raw dumps with no frontmatter, and numbered stages `2_` and `10_` that must sort by number.

### On disk

```
acme-labs/                              <- realm root
├── CLAUDE.md                           <- ## Capture surfaces (written on first bucket add)
├── .claude/
│   └── skills/
│       ├── experiments-management/SKILL.md   <- atomic wiki bucket skill experiments
│       └── research-management/SKILL.md
├── wiki/                               <- the wiki repo
│   ├── index.md                        <- <wiki-buckets> block + ## Buckets + ## Members
│   ├── CLAUDE.md                       <- "@index.md"
│   └── repos/
├── experiments/                        <- bucket
│   ├── index.md
│   └── vector-store-bench/             <- item
│       ├── index.md
│       ├── 01_raw/
│       │   ├── index.md
│       │   └── bench.py
│       └── 02_findings/
│           ├── index.md
│           └── results.md
├── research/                           <- bucket
│   ├── index.md
│   ├── coding-agents/                  <- item
│   │   ├── index.md
│   │   ├── 01_raw/
│   │   │   ├── index.md
│   │   │   ├── directus-export.md      <- no frontmatter, has H1 + prose
│   │   │   └── call-notes-2026-07-11.md <- no frontmatter, no H1, no prose
│   │   ├── 2_findings/
│   │   │   ├── index.md
│   │   │   ├── claude-sdk.md
│   │   │   └── mastra.md
│   │   └── 10_publish/
│   │       └── index.md
│   └── seo/                            <- item, stages not started
│       ├── index.md
│       └── 01_raw/index.md
└── storefront/                         <- a member repo, not a bucket
```

`research/coding-agents` is the shape from the motivating case: one topic, its raw capture, its vendor findings, and a publish stage, each folder carrying its own listing. `bench.py` sits in a stage folder and is not listed, because listings name only markdown.

### A fully-specified item

`research/coding-agents/index.md` — `created` was stamped by the scaffold; the model wrote the rest outside the region.

```markdown
---
title: Coding agents
type: Research
description: Survey of agent SDKs and orchestration frameworks, one finding per vendor.
tags: [agents, sdk, orchestration]
status: active
created: 2026-07-14
---

# Coding agents

Comparison of the major agent-building SDKs. Raw exports live in `01_raw/`,
one finding per vendor in `2_findings/`.

<bucket-docs>

## Docs

- [Raw](01_raw/index.md) - Source material collected for coding-agents
- [Findings](2_findings/index.md) - One write-up per vendor SDK.
- [Publish](10_publish/index.md) - Final comparison for the blog.

</bucket-docs>
```

`2_findings` lists before `10_publish` because a numeric prefix sorts by its integer value; as text, `10_` would come first.

### Generated bucket index

`research/index.md` — everything outside the region is the user's, preserved byte-for-byte. Each item is one entry linking to its own `index.md`.

```markdown
---
title: research
type: Bucket
description: Research items, from raw capture to published write-up.
---

# research

Research items, from raw capture to published write-up.

## Conventions

One item per folder. Collect sources in `01_raw/`; number later stages.

<bucket-docs>

## Docs

- [Coding agents](coding-agents/index.md) - Survey of agent SDKs and orchestration frameworks, one finding per vendor. · tags: agents, sdk, orchestration
- [SEO](seo/index.md) - Technical SEO checklist for the marketing sites.

</bucket-docs>
```

`research/coding-agents/01_raw/index.md` — same machinery, nothing to derive from in its children.

```markdown
<bucket-docs>

## Docs

### Unindexed

- [Directus export](directus-export.md) - Full CMS collection dump pulled 2026-07-02.
- [call-notes-2026-07-11](call-notes-2026-07-11.md)

</bucket-docs>
```

Note the blank line inside each tag pair — without it CommonMark treats the whole region as one raw HTML block and the listing stops rendering as a list. Note also that `call-notes-2026-07-11.md` renders link-only: when no description is derivable the trailing ` - ` is omitted entirely, matching `buildMembersSection`'s existing OKF §6 link-only form rather than emitting a dangling separator.

### Generated realm index

`wiki/index.md` — the `<wiki-buckets>` block is untouched registry; `## Buckets` is the generated presentation beside the existing `## Members`.

```markdown
<wiki-buckets>
<bucket name="experiments" path="/home/you/acme-labs/experiments"/>
<bucket name="research" path="/home/you/acme-labs/research"/>
</wiki-buckets>

<wiki-bucket-list>

## Buckets

- [experiments](../experiments/index.md) - Spikes and prototypes, tried before committing to an approach.
- [research](../research/index.md) - Research items, from raw capture to published write-up.

</wiki-bucket-list>

<wiki-member-list>

## Members

- [storefront](../storefront) - Astro marketing site with a Directus content backend.

</wiki-member-list>
```

Registry and presentation stay separate and are both machine-owned: `<wiki-buckets>` is the parse-back registry (`scan`, the `bucket` verbs, `serve`), `<wiki-bucket-list>` is the rendered listing. `<wiki-member-list>` is the migrated form of the shipped `## Members` markers, so one file no longer carries two delimiter syntaxes.

The block keeps absolute paths because the `bucket` verbs and `serve` resolve them from any cwd. The listing renders a link relative to `wiki/index.md` (`../<name>/index.md`), matching how `atomic wiki linkify` already emits file-relative links.

### Where every rendered value came from

| Rendered | Source | Rung |
|---|---|---|
| `Coding agents` | `research/coding-agents/index.md` frontmatter `title` | 1 |
| `Survey of agent SDKs…` | same file, frontmatter `description` | 1 |
| `agents, sdk, orchestration` | same file, frontmatter `tags` | only rung |
| `Raw`, `Source material collected for coding-agents` | `01_raw/index.md` frontmatter `title` and `description`, written by the scaffold; the description names the slug | 1 |
| `Directus export` | `01_raw/directus-export.md` first H1 (no frontmatter) | 2 |
| `Full CMS collection dump…` | same file, first prose line | 2 |
| `call-notes-2026-07-11` | filename stem (no frontmatter, no H1) | 3 |
| *(link-only entry, no ` - ` separator)* | no frontmatter, no prose line — ladder exhausted | exhausted |
| `research` description | `research/index.md` frontmatter `description` | 1 |

Nothing in any generated region is remembered — a rebuild after any edit reproduces it exactly, which is what makes the indexes safe to regenerate on every `atomic wiki scan`.


## Open questions


- **Does `type:` belong on bucket docs at all?** The per-bucket skill can declare one (`type: Experiment`, `type: Research`), and `serve`'s `resolveNodeType` degrades unknown values to `page` via path-convention fallback, so arbitrary values are safe. Carrying it costs one line and buys future graph coloring. Carried in the contract as optional; revisit if it stays unused.
- **`status:` lifecycle values are per-bucket** (`active`/`dead-end`/`graduated` for experiments; `open`/`done` for tickets). Left producer-defined and declared by each bucket's skill rather than validated centrally — a central enum would be exactly the registry the OKF non-goals rule out.
- **Scan cost on large buckets.** `atomic wiki scan` rebuilds every folder index of every bucket, writing only files whose content changed; `atomic wiki bucket index <bucket>` targets one. If scan turns out slow on a large bucket, profile the walk.
- **Unverified:** whether a realm root that is not itself a git repository still yields `.claude/skills/` discovery from a session started there. The upstream doc phrases the walk as "up to the repository root" without stating the non-repo case. Worth confirming before the guide text promises it.


## Change log


### 2026-10-19 — Index at every level, items as stage folders

**What changed:** The index diagram, Goals, Approach A, Approach D, the Recommendation, the creation sequence, and the worked example describe an `index.md` in every bucket folder listing its direct children, and items scaffolded as `<slug>/index.md` plus numbered stage folders starting at `01_raw/`. The example realm folds the separate `raw` bucket into `research` items, and the example `research/index.md` carries the frontmatter `bucket add` now stubs, so its description comes from rung 1. The open question on whether scan rebuilds bucket indexes is settled.

**Why:** Per-folder indexes and staged items replaced the router shape (`docs/spec/bucket-icm-structure.md`), and bucket manifests and `wiki/knowledge/` were deleted with the realm wiki refresh tracking (`docs/spec/realm-wiki-drop-refresh.md`).

**Superseded:** A2, the per-topic index with `<slug>/` collapsing under a `<slug>.md` router, the `--router` scaffold, a separate `raw` bucket, and the non-goal tying index granularity to the manifest walk.

# Bucket doc management


## Problem


Buckets today are a registered folder with a name, a path, and a hand-written `index.md` stub. Everything below that is unstructured. Three consequences:

**The indexes are not built, they are remembered.** The realm `wiki/index.md` knows a bucket's name and absolute path and nothing else. A bucket's own `index.md` lists nothing — it carries a purpose line and a `## Conventions` block, both hand-written, both going stale the moment a doc is added. Neither level answers "what is in this bucket?" without a filesystem walk.

**Authoring rules live in the user's head and get re-typed.** Every time a doc is created the same instructions get repeated: one topic per file, break subtopics into a folder, summarize the folder in the sibling `.md`, put code next to the writeup, write a `CLAUDE.md` for the subtree. The rules differ per bucket — an `experiments/` entry wants a spike writeup plus runnable code, a `research/` entry wants a sourced report — and that per-bucket flavor is re-explained each session.

**There is no frontmatter contract, so nothing downstream can be derived.** Measured on a live realm (`alonso-network`, 2026-07-23): 0 of 20 bucket content files carry frontmatter, and no bucket `index.md` carries any either. That is not drift — `createBucketIndexStub` (`atomic/internal/wiki/bucket_registry.go:283`) emits `# <name>` plus HTML comments and nothing else. Without frontmatter there is no title, no description, no tags, no type, so any listing has to be model-written, which means it drifts silently.

The wiki already solves the equivalent problem one level up: realm members are listed deterministically by `buildMembersSection` + `DeriveMemberDescription` (`atomic/internal/wiki/wiki.go:631`), which reads a summary's frontmatter `description:` and falls back to the first prose line. That mechanism is correct and shipped. It simply stops at the bucket boundary.

Two-level index the feature establishes:

```
wiki/index.md
  <wiki-bucket-list>   <- code-owned region: ## Buckets + one entry per bucket
  </wiki-bucket-list>
       |
       v  derived from each bucket's index.md frontmatter
<bucket>/index.md
  <bucket-docs>        <- code-owned region: ## Docs + one entry per TOPIC
  </bucket-docs>
       |
       v  derived from each doc's frontmatter
<bucket>/<slug>.md           simple topic
<bucket>/<slug>.md + <slug>/ router topic (subtree collapses under the router)
```


## Goals / Non-goals


- **Goals:**
    - Deterministic two-level index: the realm indexes its buckets, each bucket indexes its own docs. Code walks and writes; the model never authors a listing.
    - A frontmatter contract for bucket docs rich enough to drive that index (`title`, `type`, `description`, `tags`, `status`, `created`).
    - `atomic wiki bucket doc <bucket> <slug>` scaffolds a doc — frontmatter, H1, lede placeholder, guidance comments — for the model to finish.
    - `atomic wiki bucket skill <bucket>` scaffolds a per-bucket management skill so the authoring rules are written once and auto-fire thereafter.
    - Encode the doc-shape convention: one topic per `.md`; a topic that outgrows a file becomes `<slug>.md` (router) plus a `<slug>/` subtree.

- **Non-goals:**
    - Rejecting or quarantining frontmatter-free files. Raw capture stays frictionless; unindexed files are listed as such, never refused.
    - Changing manifest or staleness granularity. `WalkBucket` keeps hashing every file recursively — indexing is per-topic, staleness is per-file, and they stay independent.
    - Shipping per-bucket skills in the install bundle. They are realm-local and user-owned; `atomic claude install` never touches them.
    - A central tag or type vocabulary. Both stay producer-defined, consistent with OKF §4.1 and the existing `docs/spec/okf-alignment.md` non-goal "no central `type` registry".
    - Generating doc bodies. The scaffold stops after the lede.
    - A new `atomic wiki stale` line type for unindexed docs — the stale exit-code contract stays as-is this round.


## Approaches


### A. Index granularity

| # | Approach | Pros | Cons |
|---|----------|------|------|
| A1 | Per-file — index every `.md` in the bucket | Trivial walk; matches the manifest walk exactly | A router topic with 12 subtopic files produces 13 entries; destroys "one topic per file"; the index becomes a file dump |
| A2 | **Per-topic — `<slug>/` collapses under its `<slug>.md` router** | Index reflects topics, which is the unit the user actually thinks in; router subtree stays navigable via its own file | Two walk granularities in one package (indexing vs manifest); needs an explicit note so a future reader does not "fix" the divergence |

### B. Where the generated listings live

| # | Approach | Pros | Cons |
|---|----------|------|------|
| B1 | Extend the `<wiki-buckets>` machine block with a `description` attr, no separate listing | One artifact; parse-back is exact | Conflates registry (identity) with presentation; block is consumed by `stale` and `serve`, so a schema change ripples |
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
| D2 | **New `atomic wiki bucket doc` verb in the `wiki` package, reusing the `doctemplate` embed idiom** | Owns the computed path (`<bucket>/<slug>.md`), refuses to clobber, stamps `created:` in code | A second embedded-template site in the tree |

The distinction that settles it: `atomic template` *emits* a skeleton, `atomic wiki bucket doc` *places* one. Placement is the whole value here — the path is derived from bucket plus slug, the router variant creates a sibling directory, and the collision check is what makes the command safe to re-run. Reuse the `//go:embed templates/*.md` idiom, not the command.


## Recommendation


A2 + B3 + C2 + D2.

The spine of the design is a rule the codebase already enforces elsewhere and that this feature extends downward: **code computes and writes every derived value; the model writes prose.** It is the same rule as `atomic wiki stamp` (`realm.md`: "code-written fingerprints are verifiable; LLM-authored ones drift silently") and the same mechanism as `DeriveMemberDescription`. Applying it to bucket docs means the two indexes are regenerated on every `atomic wiki scan` at zero model cost and cannot drift, because nothing about them is remembered.

The derivation ladders keep capture frictionless. A doc with full frontmatter gets a rich entry; a doc with none still gets a title (H1, then filename stem) and a description (first prose line), and lands under `### Unindexed` — visible, listed, never rejected. That is what lets a 16-file `raw/` dump coexist with an authored `research/` report under one contract.

Sequence per topic, from creation to index:

```
atomic wiki bucket doc research seo
    -> writes research/seo.md   (frontmatter + created: stamped, H1, lede placeholder)
    -> refuses if the file exists

model fills title / description / tags / body
    <- guided by .claude/skills/research-management/SKILL.md

atomic wiki scan  (or  atomic wiki bucket index research)
    -> walks research/ per topic
    -> reads each topic's frontmatter, applies the fallback ladders
    -> splices the <bucket-docs> region in research/index.md
    -> regenerates the <wiki-bucket-list> region in wiki/index.md
```


## Worked example


A fictional realm, `acme-labs`, with three buckets exercising every case the contract has to handle: a simple topic, a router topic, an experiment carrying code, and a raw bucket with no frontmatter anywhere.

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
│   ├── repos/
│   ├── knowledge/
│   └── .buckets/                       <- manifests (baseline/previous/current)
├── experiments/                        <- bucket
│   ├── index.md
│   ├── ghost-theme-spike.md            <- simple topic
│   ├── vector-store-bench.md           <- router topic
│   └── vector-store-bench/
│       ├── CLAUDE.md                   <- created by --router
│       ├── bench.py
│       └── results.md
├── research/                           <- bucket
│   ├── index.md
│   ├── seo.md                          <- simple topic
│   ├── coding-agents.md                <- router topic
│   └── coding-agents/
│       ├── claude-sdk.md
│       ├── langchain.md
│       ├── mastra.md
│       └── google-genai.md
├── raw/                                <- bucket, unstructured by design
│   ├── index.md
│   ├── directus-export.md              <- no frontmatter, has H1 + prose
│   └── call-notes-2026-07-11.md        <- no frontmatter, no H1, no prose
└── storefront/                         <- a member repo, not a bucket
```

`research/coding-agents` is the shape from the motivating case: one shallow topic composed of vendor subtopics. It is a single index entry, not five.

### A fully-specified topic

`research/coding-agents.md` — `created` was stamped by the scaffold; everything else the model wrote.

```markdown
---
title: Coding agents
type: Research
description: Survey of agent SDKs and orchestration frameworks, one subtopic per vendor.
tags: [agents, sdk, orchestration]
status: active
created: 2026-07-14
---

# Coding agents

Comparison of the major agent-building SDKs. Each vendor gets a subtopic under
`coding-agents/`; this file carries the comparison and routes to them.
```

### Generated bucket index

`research/index.md` — everything outside the region is the user's, preserved byte-for-byte.

```markdown
# research


Authored research reports: distilled, citeable write-ups that feed wiki synthesis.
Raw material behind a report belongs in `raw/`.


## Conventions

One topic per file. A topic that outgrows one file becomes a router: keep the
summary in `<slug>.md` and break subtopics into `<slug>/`.

<bucket-docs>

## Docs

- [Coding agents](coding-agents.md) - Survey of agent SDKs and orchestration frameworks, one subtopic per vendor. · tags: agents, sdk, orchestration · router (4 docs)
- [SEO](seo.md) - Technical SEO checklist for the marketing sites. · tags: seo, marketing

</bucket-docs>
```

`raw/index.md` — same machinery, nothing to derive from.

```markdown
<bucket-docs>

## Docs

### Unindexed

- [Directus export](directus-export.md) - Full CMS collection dump pulled 2026-07-02, awaiting synthesis.
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
<bucket name="raw" path="/home/you/acme-labs/raw"/>
</wiki-buckets>

<wiki-bucket-list>

## Buckets

- [experiments](../experiments) - Spikes and prototypes, tried before committing to an approach.
- [raw](../raw) - Unprocessed dumps awaiting synthesis. Anything goes.
- [research](../research) - Authored research reports: distilled, citeable write-ups that feed wiki synthesis.

</wiki-bucket-list>

<wiki-member-list>

## Members

- [storefront](../storefront) - Astro marketing site with a Directus content backend.

</wiki-member-list>
```

Registry and presentation stay separate and are both machine-owned: `<wiki-buckets>` is the parse-back registry (`stale`, `serve`), `<wiki-bucket-list>` is the rendered listing. `<wiki-member-list>` is the migrated form of the shipped `## Members` markers, so one file no longer carries two delimiter syntaxes.

The block keeps absolute paths because `stale` and `serve` resolve them from any cwd. The listing renders a link relative to `wiki/index.md` (`../<name>`), matching how `atomic wiki linkify` already emits file-relative links.

### Where every rendered value came from

| Rendered | Source | Rung |
|---|---|---|
| `Coding agents` | `research/coding-agents.md` frontmatter `title` | 1 |
| `Survey of agent SDKs…` | same file, frontmatter `description` | 1 |
| `agents, sdk, orchestration` | same file, frontmatter `tags` | only rung |
| `router (4 docs)` | `coding-agents/` exists beside `coding-agents.md`; 4 `.md` descendants | walk |
| `Directus export` | `raw/directus-export.md` first H1 (no frontmatter) | 2 |
| `Full CMS collection dump…` | same file, first prose line | 2 |
| `call-notes-2026-07-11` | filename stem (no frontmatter, no H1) | 3 |
| *(link-only entry, no ` - ` separator)* | no frontmatter, no prose line — ladder exhausted | exhausted |
| `research` description | `research/index.md` first prose line | 2 |

Nothing in either generated region is remembered — a rebuild after any edit reproduces it exactly, which is what makes the indexes safe to regenerate on every `atomic wiki scan`.


## Open questions


- **Does `type:` belong on bucket docs at all?** The per-bucket skill can declare one (`type: Experiment`, `type: Research`), and `serve`'s `resolveNodeType` degrades unknown values to `page` via path-convention fallback, so arbitrary values are safe. Carrying it costs one line and buys future graph coloring. Carried in the contract as optional; revisit if it stays unused.
- **`status:` lifecycle values are per-bucket** (`active`/`dead-end`/`graduated` for experiments; `open`/`done` for tickets). Left producer-defined and declared by each bucket's skill rather than validated centrally — a central enum would be exactly the registry the OKF non-goals rule out.
- **Should `atomic wiki scan` rebuild bucket indexes, or only `atomic wiki bucket index`?** Spec has scan doing both levels so one deterministic pass converges, with the targeted verb available for a single bucket. If scan turns out to be too slow on large raw buckets, the walk is the thing to profile — it is already O(files) for the manifest.
- **Unverified:** whether a realm root that is not itself a git repository still yields `.claude/skills/` discovery from a session started there. The upstream doc phrases the walk as "up to the repository root" without stating the non-repo case. Worth confirming before the guide text promises it.

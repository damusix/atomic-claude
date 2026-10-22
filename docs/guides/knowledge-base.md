# Knowledge base


Atomic documents code. A [wiki](/reference/realm-wiki) compiles a folder of repositories into one map. It points at the repos that already have their own wiki, summarizes the ones that do not, and writes up the concerns they share. That is the code side of a realm.

Real work is not only code. A client engagement accumulates tickets, research you wrote chasing a problem, an email thread with the one detail that explains a bug, a PDF someone sent, a Slack export. This material is the knowledge side. You drop it into folders and register them as capture buckets. The realm `CLAUDE.md` lists each bucket folder, and every folder in a bucket has an `index.md` listing what it holds.

| | Provided by atomic | Yours to provide |
|---|---|---|
| **Code side** | `/refresh-wiki` documents the repos and writes `wiki/` (summaries, concerns); `scan`, `linkify`, and `bucket` maintain it; `stale` reports membership drift. | The member repos. |
| **Knowledge side** | Bucket registration (`atomic wiki bucket add`), doc scaffolds (`bucket doc`, `bucket skill`), and the generated per-folder indexes (`bucket index`, also run by every scan). | The capture folders, the material in them, and each bucket's `index.md` conventions. |
| **Glue** | The realm steering walk is a harness behavior atomic relies on: `AGENTS.md` at the realm root, with the thin `CLAUDE.md` loader beside it for Claude Code. | The contents of that steering file: your rules, paths, and conventions. |

The result is a Karpathy-style wiki: a knowledge base you compile with Claude instead of maintaining by hand.


## The realm layout


A realm is a folder that holds repositories and the loose material around them. It is not itself a git repository. Each member repo is one, and so is the wiki.

```text
~/work/acme/                 the realm — not a git repo
├─ AGENTS.md          realm rules, loaded from anywhere inside (CLAUDE.md loader beside it)
├─ .mcp.json          ticket / tool servers for the whole realm
├─ billing-api/       repo · has its own wiki → indexed
├─ gateway/           repo · has its own wiki → indexed
├─ vendor-sdk/        repo · no wiki → summarized
├─ research/          capture bucket · one folder per item
│  ├─ index.md        lists the items
│  └─ seo-audit/      an item: index.md, then numbered stages
│     ├─ 01_raw/      dumps collected for this item
│     └─ 02_findings/ a stage you add: what you wrote from them
├─ history/           capture bucket · scraped / time-stamped captures
└─ wiki/              the map atomic compiles — its own git repo
   ├─ index.md        member registry + <wiki-buckets> block
   ├─ repos/          summaries of repos without their own wiki
   └─ concerns/       what cuts across repos
```

The repos and the `wiki/` folder are git repositories; the realm root and the capture folders are not. You commit inside each member and inside `wiki/`, never at the realm root. The capture folder names are your convention: `research` and `history` are examples, so register whatever folders fit how you already organize work.

The realm steering file is what makes this cohere. The harness walks up the directory tree when it loads steering (for Claude Code, the thin `CLAUDE.md` loader imports `AGENTS.md`), and the walk crosses repo boundaries, so a realm-root file stays in context from any session inside the realm, including one started inside a member repo. Put your realm rules there: where each capture folder lives, what convention each follows, and a pointer to the wiki.


## Capture surfaces


Different material enters through different doors. Sort it by how processed it is when it arrives, then register each folder with `atomic wiki bucket add <name>`. The `atomic-wiki` skill does this for you when you say you want a place for notes, research, or tickets.

| Surface | Holds | Naming |
|---------|-------|--------|
| `research/` | One folder per item: a vendor evaluation, an API investigation, a design tradeoff. Its `01_raw/` stage holds the unprocessed dumps (PDFs, email and Slack exports, transcripts, screenshots); later stages hold what you write from them. | `<slug>/` per item, scaffolded by `bucket doc`; files in `01_raw/` stay as they arrive. |
| `history/` | Scraped pages and time-stamped captures of source documents. | Timestamp-named. |
| Tickets | Live issue tracker state, read through MCP. A ticket you triage gets its own item in a `tickets/` bucket, shown in [the example below](#example-a-tickets-bucket-with-triage). | No folder for live state; `<slug>/` per triaged ticket. |

Each bucket carries an `index.md` describing what it holds and the convention its files follow. `atomic wiki bucket add` creates the stub; you fill it in, or let the `/refresh-wiki` bucket offer ask you on first use. The same file also carries a generated listing of the bucket's contents, covered below in "Browsing a bucket's own docs".

Live tracker state does not need a folder. Wire your tracker as an MCP server in the realm `.mcp.json` and Claude reads issues live:

```json
{
  "mcpServers": {
    "linear-server": { "type": "http", "url": "https://mcp.linear.app/mcp" }
  }
}
```

GitHub, Jira, and Linear all expose MCP servers. With one wired at the realm root, "what is blocking ticket ACME-412" is answerable from any session in the realm. When a tracker has no MCP server, or you want a written triage for each ticket, keep them in a bucket instead, as in the tickets example below.


### Browsing a bucket's own docs


Every folder in a bucket has an `index.md` with a `## Docs` listing of its direct children: its markdown files and its subfolders, each subfolder linked to its own `index.md`. Skipped folders (`node_modules`, `build`, `tmp`, any dot-prefixed folder) get none. Numbered stage folders list in numeric order, so `01_raw` comes before `02_findings` and `9_x` before `10_x`. The realm `wiki/index.md` carries a `## Buckets` listing of every registered bucket, each linked to its root `index.md`. All of them are generated by code and rebuilt on every `atomic wiki scan`, or on demand with `atomic wiki bucket index`. A bucket `index.md` whose listing has not changed is not rewritten.

The listing is derived from each doc's frontmatter. A bucket doc carries six keys: `title`, `type`, `description`, `tags`, and `status`, which you write, and `created`, which the scaffold stamps. Files with no frontmatter are not rejected; they still list, under an `### Unindexed` heading, with a title taken from the first heading and a description from the first line.

Scaffold an item with `atomic wiki bucket doc <bucket> <slug>`. It writes `<slug>/index.md` with the frontmatter and `created` stamped, and `<slug>/01_raw/index.md` for the material you collect. Add later stages as numbered folders (`02_findings/`, `10_publish/`) when the work reaches them; the next rebuild indexes each one.

`atomic wiki bucket skill <bucket>` scaffolds a `.claude/skills/<bucket>-management/` skill at the realm root, with `## Purpose` taken from the `description:` in the bucket's `index.md` and a `## Stages` placeholder. The skill loads only in Claude Code sessions started at the realm root. A session started inside a member repo reaches the bucket's `index.md` instead, through its entry under `## Capture surfaces` in the realm `CLAUDE.md`. Write the conventions in both files.


### Example: a tickets bucket with triage


A tickets bucket holds one item per ticket. `01_raw/` keeps the ticket as received, and `02_triage/` holds your read of it:

```text
tickets/
├─ index.md              what the bucket holds + its stage conventions
└─ acme-412-login-loop/
   ├─ index.md           the ticket: title, status, one-line description
   ├─ 01_raw/            the export, email thread, screenshots, as received
   └─ 02_triage/
      └─ index.md        type, priority, owning repo, next action
```

The triage records four fields:

| Field | Holds |
|-------|-------|
| Type | bug, feature request, question, or your own categories |
| Priority | your scale, such as P0 to P3 |
| Owning repo | one of the realm's member repos, as listed in the `<wiki-scan>` block of `wiki/index.md` |
| Next action | the one step that moves the ticket forward, and who takes it |

Register the bucket, then replace the `description:` placeholder in `tickets/index.md` before scaffolding the skill, because `bucket skill` copies that description into the skill's `## Purpose`:

```sh
atomic wiki bucket add tickets
# edit tickets/index.md: description: One item per support ticket, triaged to an owning repo
atomic wiki bucket skill tickets
atomic wiki bucket doc tickets acme-412-login-loop
```

Filling `description:` does not update the realm `CLAUDE.md`. Its `## Capture surfaces` bullet for the bucket keeps the `<!-- describe what this bucket is for -->` placeholder, so replace that too.

`bucket doc` creates `01_raw/` only. Add `02_triage/index.md` by hand when you triage the ticket, with the `title` and `description` frontmatter that `bucket doc` stamps on `01_raw/index.md`. Without frontmatter, the item's listing puts the stage under `### Unindexed` instead of beside Raw:

```markdown
---
title: Triage
description: Type, priority, owning repo, and next action for acme-412-login-loop
---
```

Then write the convention down in two places. In `tickets/index.md`, replace the stages placeholder under `## Conventions` with the two stages and what each holds. In `.claude/skills/tickets-management/SKILL.md`, fill `## Stages` with the same list and the four triage fields, so a realm-root session that triages a ticket writes them the same way every time.

The stage names are your convention. Atomic does not know what a ticket or a triage is: it lists `01_raw/` and `02_triage/` in numeric order like any other stage folders and checks nothing inside them.


## How a session finds material


The indexes are the only thing atomic derives from a bucket. No model pass reads the bucket, so a session reads your files directly when it needs them: it opens the bucket's root `index.md` and descends one `index.md` at a time, loading only the folders it needs. Each entry in a bucket's `index.md` carries the doc's one-line description, so write a clear `description:` on each doc; without one, the listing falls back to the doc's first prose line.


## Running it


1. `atomic wiki bucket add research` registers the folder once. It creates the bucket's `index.md` stub, adds the bucket to `wiki/index.md`, and writes the `## Capture surfaces` entry in the realm `CLAUDE.md`, naming the bucket's `index.md`.
2. `atomic wiki bucket doc research <slug>` scaffolds an item. Drop its source material into `research/<slug>/01_raw/` as work happens.
3. `atomic wiki scan` (or `/refresh-wiki`, which runs it) rebuilds every folder's listing.

On first run in a realm with no buckets, `/refresh-wiki` offers to create them.


## Browsing the result


Everything in a realm is markdown in folders, so an Obsidian vault, any markdown server, or `atomic serve` renders it as a navigable graph. Two things make it click together:

- An `index.md` in every surface, listing its contents with links. These are the entry points.
- `atomic wiki linkify`, which turns the path citations in repo summaries and concern docs into relative markdown links. `/refresh-wiki` runs it for you.

`atomic serve` renders the realm read-only in the browser, colored by concept type, with federated code search across members. Open it and click from a concern, to the repo it touches, to that repo's wiki.


## Where the line is


You own the material and the conventions: what goes in each bucket, the bucket's `index.md` description, and the realm steering file. When you register a bucket, Atomic writes its `index.md` stub and the `## Capture surfaces` entry in the realm `CLAUDE.md`. On registration and on every scan, it writes a generated `<bucket-docs>` listing into an `index.md` in each non-skipped folder of the bucket, creating the file where none exists. Those `index.md` files are the one exception: every other file you drop in is yours and untouched, and text outside the listing stays as you wrote it. The [wiki workflow reference](/reference/realm-wiki) documents the full mechanism.

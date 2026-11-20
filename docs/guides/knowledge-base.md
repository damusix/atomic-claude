# Knowledge base


Atomic documents code. A [wiki](/reference/realm-wiki) compiles a folder of repositories into one map. It points at the repos that already have their own wiki, summarizes the ones that do not, and writes up the concerns they share. That is the code side of a realm.

Capture buckets hold the material around the code: tickets, research, transcripts, and documents. Register a folder as a bucket and start at its `index.md`. Atomic indexes the material without running a model over it.

| | Provided by atomic | Yours to provide |
|---|---|---|
| **Code side** | For an independent wiki, `/refresh-wiki` documents the repos and writes `wiki/` summaries and concerns; `stale` reports membership drift. | The member repos. |
| **Knowledge side** | Bucket registration (`atomic wiki bucket add`), doc scaffolds (`bucket doc`, `bucket skill`), and generated per-folder indexes (`bucket index`). | The capture folders, the material in them, and each bucket's `index.md` conventions. |
| **Steering** | On setup, Atomic can scaffold `AGENTS.md` and a local `CLAUDE.md` loader for Claude Code. | Your realm rules and conventions. |

The result is a Karpathy-style wiki: a knowledge base you compile with Claude instead of maintaining by hand.


## The realm layout


A realm is a folder that holds repositories and the material around them. This example shows an independent realm wiki, before sharing:

```text
~/work/acme/                 the realm — not a git repo
├─ AGENTS.md          local realm rules, with a CLAUDE.md loader beside it
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

Each member and the independent `wiki/` have their own Git repository; the realm root and capture folders do not. Commit to the repository that owns the change. Choose bucket names that match how you organize your work.

If you [share the realm](/guides/realm-sharing), the local `wiki/` can become a symlink into a managed checkout. The original wiki Git history stays in a local backup. Edit the linked files and publish with `atomic realm sync`; the independent wiki's `/refresh-wiki` commit flow is not the publishing workflow for linked content. Member repos and their repo signal scans stay separate.

Start a session at the realm root for realm-local steering. Native discovery depends on the harness and launch location. The [sharing guide](/guides/realm-sharing#startup-guidance-and-native-artifact-limits) describes the verified scope for shared instructions and artifacts.


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

GitHub, Jira, and Linear provide MCP servers. A session that loads the tracker connection can read live issue state. Use a bucket when there is no tracker connection or you want a written triage record.


### Browsing a bucket's own docs


Each indexed folder in a bucket has an `index.md` with a `## Docs` listing of direct markdown files and links to its subfolders' indexes. Skipped folders (`node_modules`, `build`, `tmp`, dot-prefixed folders) get no index. Numbered stages list in numeric order. Atomic rebuilds the listings during a scan or on demand with `atomic wiki bucket index`; it leaves unchanged indexes alone.

In an independent realm, `wiki/index.md` lists registered buckets. In a bound realm, private registrations stay in local state and do not appear in the shared wiki. Run `atomic wiki bucket list` to see usable local buckets, and `atomic realm status` to find received paths that still need review and linking.

The listing is derived from each doc's frontmatter. A bucket doc carries six keys: `title`, `type`, `description`, `tags`, and `status`, which you write, and `created`, which the scaffold stamps. Files with no frontmatter are not rejected; they still list, under an `### Unindexed` heading, with a title taken from the first heading and a description from the first line.

Scaffold an item with `atomic wiki bucket doc <bucket> <slug>`. It writes `<slug>/index.md` with the frontmatter and `created` stamped, and `<slug>/01_raw/index.md` for the material you collect. Add later stages as numbered folders (`02_findings/`, `10_publish/`) when the work reaches them; the next rebuild indexes each one.

`atomic wiki bucket skill <bucket>` scaffolds a local Claude skill under `.claude/skills/<bucket>-management/`, using the bucket description for `## Purpose` and adding a `## Stages` placeholder. Start Claude Code at the realm root to use the skill. Keep the bucket's own conventions in its `index.md` so people and other tools can read them without loading the skill.


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

Filling `description:` does not update a `## Capture surfaces` bullet in local steering. If one already exists, replace its placeholder there too. In a bound realm, keep private bucket hints out of shared team guidance.

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


1. `atomic wiki bucket add research` creates the bucket's `index.md` stub and registers the path. An independent wiki records the registration in `wiki/index.md` and adds a `## Capture surfaces` hint to local steering. A bound realm records private registrations in local state; the shared wiki and team guidance remain unchanged.
2. `atomic wiki bucket doc research <slug>` scaffolds an item. Drop its source material into `research/<slug>/01_raw/` as work happens.
3. `atomic wiki bucket index research` rebuilds this bucket's listings on demand. A wiki scan also rebuilds bucket listings.

On first run against an independent wiki with no buckets, `/refresh-wiki` offers to create them.


## Browsing the result


Everything in a realm is markdown in folders, so an Obsidian vault, any markdown server, or `atomic serve` renders it as a navigable graph. Two things make it click together:

- An `index.md` in every surface, listing its contents with links. These are the entry points.
- `atomic wiki linkify`, which turns the path citations in repo summaries and concern docs into relative markdown links. `/refresh-wiki` runs it for you.

`atomic serve` renders the realm read-only in the browser, colored by concept type, with federated code search across members. Open it and click from a concern, to the repo it touches, to that repo's wiki.


## Where the line is


You own the material and its conventions. Atomic maintains the generated `<bucket-docs>` region of each bucket index and leaves other text in the index untouched. Bucket registration does not publish. To promote a local bucket, [review and share the directory explicitly](/guides/realm-sharing#promote-a-private-bucket-later).

See the [wiki workflow reference](/reference/realm-wiki) for scans, registration, and index behavior.

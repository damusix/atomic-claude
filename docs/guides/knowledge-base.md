# Knowledge base


Atomic documents code. A [wiki](/reference/realm-wiki) compiles a folder of repositories into one map. It points at the repos that already have their own wiki, summarizes the ones that do not, and writes up the concerns they share. That is the code side of a realm.

Real work is not only code. A client engagement accumulates tickets, research you wrote chasing a problem, an email thread with the one detail that explains a bug, a PDF someone sent, a Slack export. This material is the knowledge side. You drop it into folders and register them as capture buckets. The realm `CLAUDE.md` lists each bucket folder, and the folder's `index.md` lists its docs.

| | Provided by atomic | Yours to provide |
|---|---|---|
| **Code side** | `/refresh-wiki` documents the repos and writes `wiki/` (summaries, concerns); `scan`, `linkify`, and `bucket` maintain it; `stale` reports membership drift. | The member repos. |
| **Knowledge side** | Bucket registration (`atomic wiki bucket add`), doc scaffolds (`bucket doc`, `bucket skill`), and the generated per-bucket indexes (`bucket index`, also run by every scan). | The capture folders, the material in them, and each bucket's `index.md` conventions. |
| **Glue** | The realm `CLAUDE.md` walk is a Claude Code behavior atomic relies on. | The contents of that `CLAUDE.md`: your rules, paths, and conventions. |

The result is a Karpathy-style wiki: a knowledge base you compile with Claude instead of maintaining by hand.


## The realm layout


A realm is a folder that holds repositories and the loose material around them. It is not itself a git repository. Each member repo is one, and so is the wiki.

```text
~/work/acme/                 the realm — not a git repo
├─ CLAUDE.md          realm rules, loaded from anywhere inside
├─ .mcp.json          ticket / tool servers for the whole realm
├─ billing-api/       repo · has its own wiki → indexed
├─ gateway/           repo · has its own wiki → indexed
├─ vendor-sdk/        repo · no wiki → summarized
├─ research/          capture bucket · findings you write
├─ raw/               capture bucket · unprocessed dumps
├─ history/           capture bucket · scraped / time-stamped captures
└─ wiki/              the map atomic compiles — its own git repo
   ├─ index.md        member registry + <wiki-buckets> block
   ├─ repos/          summaries of repos without their own wiki
   └─ concerns/       what cuts across repos
```

The repos and the `wiki/` folder are git repositories; the realm root and the capture folders are not. You commit inside each member and inside `wiki/`, never at the realm root. The capture folder names are your convention: `research`, `raw`, and `history` are examples, so register whatever folders fit how you already organize work.

The realm `CLAUDE.md` is what makes this cohere. Claude Code walks up the directory tree when it loads `CLAUDE.md`, and the walk crosses repo boundaries. A realm-root file stays in context from any session inside the realm, including one started inside a member repo. Put your realm rules there: where each capture folder lives, what convention each follows, and a pointer to the wiki.


## Capture surfaces


Different material enters through different doors. Sort it by how processed it is when it arrives, then register each folder with `atomic wiki bucket add <name>`. The `atomic-wiki` skill does this for you when you say you want a place for notes, research, or tickets.

| Surface | Holds | Naming |
|---------|-------|--------|
| `research/` | Findings you produce: a vendor evaluation, an API investigation, a design tradeoff you worked out. | `YYYY-MM-DD-<slug>.md`, one file per topic. |
| `raw/` | Unprocessed dumps: PDFs, email and Slack exports, meeting transcripts, pasted notes, screenshots. | Whatever they arrive as. |
| `history/` | Scraped pages and time-stamped captures of source documents. | Timestamp-named. |
| Tickets | Live issue tracker state. | Not a folder — read through MCP. |

Each bucket carries an `index.md` describing what it holds and the convention its files follow. `atomic wiki bucket add` creates the stub; you fill it in, or let the `/refresh-wiki` bucket offer ask you on first use. The same file also carries a generated listing of the bucket's own docs, covered below in "Browsing a bucket's own docs".

Tickets are the one surface that does not need a folder. Wire your tracker as an MCP server in the realm `.mcp.json` and Claude reads issues live:

```json
{
  "mcpServers": {
    "linear-server": { "type": "http", "url": "https://mcp.linear.app/mcp" }
  }
}
```

GitHub, Jira, and Linear all expose MCP servers. With one wired at the realm root, "what is blocking ticket ACME-412" is answerable from any session in the realm. When a tracker has no MCP server, export the tickets and drop the export in `raw/` like any other dump.


### Browsing a bucket's own docs


A bucket's `index.md` carries a `## Docs` listing of the bucket's own topics, and the realm `wiki/index.md` carries a `## Buckets` listing of every registered bucket. Both are generated by code and rewritten on every `atomic wiki scan`, or on demand with `atomic wiki bucket index`. A bucket's contents stay browsable without you maintaining a table of contents by hand.

The listing is derived from each doc's frontmatter. A bucket doc carries six keys: `title`, `type`, `description`, `tags`, and `status`, which you write, and `created`, which the scaffold stamps. Files with no frontmatter are not rejected; they still list, under an `### Unindexed` heading, with a title taken from the first heading and a description from the first line.

Scaffold a doc with `atomic wiki bucket doc <bucket> <slug>`. It writes the frontmatter, stamps `created`, and leaves the rest for you to fill. Keep one topic per file. When a topic outgrows a single file, run the same command with `--router`: it adds a `<slug>/` subtree for the subtopics and keeps `<slug>.md` as the summary. In the listing that topic shows as one entry flagged `router (N docs)`, so the subtree stays navigable without cluttering the index.

To keep a bucket's conventions in one place, `atomic wiki bucket skill <bucket>` scaffolds a `.claude/skills/<bucket>-management/` skill prefilled with the bucket's purpose. Fill in the rules for that bucket once, and they load automatically whenever you work in the realm.


## How a session finds material


The indexes are the only thing atomic derives from a bucket. No model pass reads the bucket, so a session reads your files directly when it needs them. Each entry in a bucket's `index.md` carries the doc's one-line description, so write a clear `description:` on each doc; without one, the listing falls back to the doc's first prose line.


## Running it


1. `atomic wiki bucket add research` registers the folder once. It creates the bucket's `index.md` stub, adds the bucket to `wiki/index.md`, and writes the `## Capture surfaces` entry in the realm `CLAUDE.md`.
2. Drop material into `research/`, `raw/`, and the rest as work happens. `atomic wiki bucket doc research <slug>` scaffolds a doc with its frontmatter.
3. `atomic wiki scan` (or `/refresh-wiki`, which runs it) rebuilds every bucket's listing.

On first run in a realm with no buckets, `/refresh-wiki` offers to create them.


## Browsing the result


Everything in a realm is markdown in folders, so an Obsidian vault, any markdown server, or `atomic serve` renders it as a navigable graph. Two things make it click together:

- An `index.md` in every surface, listing its contents with links. These are the entry points.
- `atomic wiki linkify`, which turns the path citations in repo summaries and concern docs into relative markdown links. `/refresh-wiki` runs it for you.

`atomic serve` renders the realm read-only in the browser, colored by concept type, with federated code search across members. Open it and click from a concern, to the repo it touches, to that repo's wiki.


## Where the line is


You own the material and the conventions: what goes in each bucket, the bucket's `index.md` description, and the realm `CLAUDE.md`. When you register a bucket, Atomic writes its `index.md` stub and the `## Capture surfaces` entry in the realm `CLAUDE.md`. It also writes the generated `<bucket-docs>` listing inside the stub; the material you drop in is yours and untouched. The [wiki workflow reference](/reference/realm-wiki) documents the full mechanism.

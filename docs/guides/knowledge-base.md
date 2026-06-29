# Knowledge base


Atomic documents code. A [wiki](/reference/wiki-workflow) compiles a folder of repositories into one map: it points at the repos that already have signals, summarizes the ones that do not, and writes up the concerns they share. That is the code side of a realm.

Real work is not only code. A client engagement accumulates tickets, research you wrote chasing a problem, an email thread with the one detail that explains a bug, a PDF someone sent, a Slack export. This material is the knowledge side, and atomic manages it the same way it manages code. You drop the material into folders and register them as capture buckets; `/refresh-wiki` fingerprints what changed and synthesizes it into `wiki/knowledge/`. You provide the material and the conventions, atomic runs the pipeline.

| | Provided by atomic | Yours to provide |
|---|---|---|
| **Code side** | `/refresh-wiki` documents the repos and writes `wiki/` (summaries, concerns, knowledge pages); the `atomic wiki` subcommands (`scan`, `stale`, `linkify`, `bucket`) maintain it. | The member repos. |
| **Knowledge side** | Bucket registration (`atomic wiki bucket add`), the SHA-256 fingerprint engine (`diff`/`promote`), and the synthesis pass that writes `wiki/knowledge/`. | The capture folders, the material in them, and each bucket's `index.md` conventions. |
| **Glue** | The realm `CLAUDE.md` walk is a Claude Code behavior atomic relies on. | The contents of that `CLAUDE.md`: your rules, paths, and conventions. |

The result is a Karpathy-style wiki: a knowledge base you compile with Claude instead of maintaining by hand.


## The realm layout


A realm is a folder that holds repositories and the loose material around them. It is not itself a git repository. Each member repo is one, and so is the wiki.

```text
~/work/acme/                 the realm — not a git repo
├─ CLAUDE.md          realm rules, loaded from anywhere inside
├─ .mcp.json          ticket / tool servers for the whole realm
├─ billing-api/       repo · signals → indexed
├─ gateway/           repo · signals → indexed
├─ vendor-sdk/        repo · no signals → summarized
├─ research/          capture bucket · findings you write
├─ raw/               capture bucket · unprocessed dumps
├─ history/           capture bucket · scraped / time-stamped captures
└─ wiki/              the map atomic compiles — its own git repo
   ├─ index.md        member registry + <wiki-buckets> block
   ├─ repos/          summaries of no-signals repos
   ├─ concerns/       what cuts across repos
   ├─ knowledge/      digests synthesized from the buckets
   └─ .buckets/       SHA-256 manifests, one dir per bucket
```

The repos and the `wiki/` folder are git repositories. The realm root and the capture folders are not. You commit inside each member and inside `wiki/`, never at the realm root. The capture folder names are your convention: `research`, `raw`, and `history` are examples, so register whatever folders fit how you already organize work.

The realm `CLAUDE.md` is what makes this cohere. Claude Code walks up the directory tree when it loads `CLAUDE.md`, and the walk crosses repo boundaries, so a realm-root file stays in context from any session inside the realm, including one started inside a member repo. Put your realm rules there: where each capture folder lives, what convention each follows, and a pointer to the wiki.


## Capture surfaces


Different material enters through different doors. Sort it by how processed it is when it arrives, then register each folder with `atomic wiki bucket add <name>`. The `atomic-wiki` skill does this for you when you say you want a place for notes, research, or tickets.

| Surface | Holds | Naming |
|---------|-------|--------|
| `research/` | Findings you produce: a vendor evaluation, an API investigation, a design tradeoff you worked out. | `YYYY-MM-DD-<slug>.md`, one file per topic. |
| `raw/` | Unprocessed dumps: PDFs, email and Slack exports, meeting transcripts, pasted notes, screenshots. | Whatever they arrive as. |
| `history/` | Scraped pages and time-stamped captures of source documents. | Timestamp-named. |
| Tickets | Live issue tracker state. | Not a folder — read through MCP. |

Each bucket carries an `index.md` describing what it holds and the convention its files follow. `atomic wiki bucket add` creates the stub; you fill it in (or let the `/refresh-wiki` offer flow guide you on first use). The synthesis pass reads this file as context before it distills anything.

Tickets are the one surface that does not need a folder. Wire your tracker as an MCP server in the realm `.mcp.json` and Claude reads issues live:

```json
{
  "mcpServers": {
    "linear-server": { "type": "http", "url": "https://mcp.linear.app/mcp" }
  }
}
```

GitHub, Jira, and Linear all expose MCP servers. With one wired at the realm root, "what is blocking ticket ACME-412" is answerable from any session in the realm, and a synthesis pass can fold a ticket's resolution into `knowledge/` alongside the research that informed it. When a tracker has no MCP server, export the tickets and drop the export in `raw/` like any other dump.


## The synthesis flow


Capture surfaces collect material. Knowledge is what atomic distills from them. The flow is one-way:

```text
research/ + raw/   →   /refresh-wiki bucket synthesis   →   wiki/knowledge/
```

A digest in `wiki/knowledge/` is the durable artifact: the thing you read later instead of re-reading the four sources behind it. The sources stay where they are. Synthesis reads them and writes a compressed, cross-linked result into the wiki, with `sources:` provenance stamped on each page so you can trace a digest back to the files that produced it.


## Only synthesize what changed


`/refresh-wiki` reprocesses only the material that changed since the last run. That deduplication is the bucket fingerprint engine, and you do not build it:

```bash
atomic wiki bucket diff research      # new / changed / removed since last synthesis (read-only)
atomic wiki bucket promote research   # advance the baseline, after synthesis lands
```

`diff` computes a SHA-256 manifest of the folder and compares it to the stored baseline, reporting new, changed, and removed files. `promote` advances the baseline to the current manifest. `/refresh-wiki` runs `diff` to find the work, synthesizes only the new and changed files, and `promote`s after each bucket succeeds, so an aborted run leaves the work pending instead of marking it done. The manifests live in `wiki/.buckets/<name>/` (`baseline`, `previous`, `current`) and are versioned with the wiki, so a clone is self-describing.

`atomic wiki stale` reports `STALE bucket <name>` for any bucket with a non-empty diff, alongside its repo and concern staleness lines.


## Running it


The whole pipeline is one command:

1. `atomic wiki bucket add research` — register the folder once. It creates the bucket's `index.md` stub and its manifest directory.
2. Drop material into `research/`, `raw/`, and the rest as work happens.
3. `/refresh-wiki` — scans the repos, then for each bucket with a non-empty diff dispatches `atomic-wiki-inferrer` to synthesize the changed files into `wiki/knowledge/<topic>.md`, stamps `sources:` provenance, promotes the baseline, and offers to commit the wiki.

No fingerprint script, no custom synthesis command. On first run in a realm with no buckets, `/refresh-wiki` offers to create them.


## Browsing the result


Everything in a realm is markdown in folders, so an Obsidian vault, any markdown server, or `atomic serve` renders it as a navigable graph. Three things make it click together:

- An `index.md` in every surface, listing its contents with links. These are the entry points.
- Cross-links written during synthesis. When a knowledge digest cites the research and the repo signals behind it, those become links you can follow.
- `atomic wiki linkify`, which turns the path citations in repo summaries and concern docs into relative markdown links. `/refresh-wiki` runs it for you.

`atomic serve` renders the realm read-only in the browser, colored by concept type, with federated code search across members. Open it and click from a concern, to the repo it touches, to the signals for that repo, to the research that explains a decision in it.


## Where the line is


Atomic owns the pipeline on both sides. On the code side, `/refresh-wiki` walks the realm, documents the repos, and keeps the summaries and concerns current. On the knowledge side, it fingerprints your capture buckets and synthesizes their changes into `wiki/knowledge/`.

You own the material and the conventions: what goes in each bucket, the bucket's `index.md`, and the realm `CLAUDE.md`. Atomic writes only the `index.md` stub when you register a bucket and the manifests under `wiki/.buckets/`; the material you drop in is yours and untouched. The [wiki workflow reference](/reference/wiki-workflow) documents the full mechanism.

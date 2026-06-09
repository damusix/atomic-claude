# Knowledge base


Atomic documents code. A [wiki](/reference/wiki-workflow) compiles a folder of repositories into one map: it points at the repos that already have signals, summarizes the ones that do not, and writes up the concerns they share. That covers the code side of a realm.

Real work is not only code. A client engagement accumulates tickets, research you wrote chasing a problem, an email thread with the one detail that explains a bug, a PDF someone sent, a Slack export, scraped pages. Atomic does not manage that material, by design. The line it draws is deliberate: atomic stays on the code side, and the non-code side is yours to direct.

This guide shows how to extend the same realm into a knowledge base that holds both. One thing to keep straight as you read: almost everything below is a suggested pattern you build yourself, not a feature atomic provides. Atomic ships the code wiki and the realm walk. The rest is an example of what you can layer on top, in the same plain-markdown, deterministic-diff-plus-LLM-judgment style atomic uses, and you should adapt the folder names, scripts, and commands to your own work rather than copy them literally.

| | Provided by atomic | Yours to build |
|---|---|---|
| **Code side** | `/refresh-wiki` documents the repos and writes everything under `wiki/` except `knowledge/`; `atomic wiki` subcommands (`linkify`, `scan`, `stale`, `mark-dirty`) maintain it. | — |
| **Knowledge side** | — | The capture folders and their conventions, the fingerprint script, the synthesis commands, and everything in `wiki/knowledge/`. |
| **Glue** | The realm `CLAUDE.md` walk is a Claude Code behavior atomic relies on. | The contents of that `CLAUDE.md`: your rules, paths, and conventions. |

The result is a Karpathy-style wiki: a knowledge base you compile with Claude instead of maintaining by hand.


## The realm layout


A realm is a folder that holds repositories and the loose material around them. It is not itself a git repository. Each member repo is one, and so is the wiki. The layout below is one arrangement that works, not a required structure: atomic only cares about the member repos and the `wiki/` folder. The capture folders and their names are a convention you pick, so rename or drop them to fit how you already organize work.

```text
~/work/acme/                    the realm — not a git repo
├─ CLAUDE.md             realm rules, loaded from anywhere inside
├─ .mcp.json            ticket / tool servers for the whole realm
├─ billing-api/          repo · signals → indexed
├─ gateway/              repo · signals → indexed
├─ vendor-sdk/           repo · no signals → summarized
├─ research/             findings you write · one file per topic
├─ raw/                  unprocessed dumps · PDFs, emails, exports
├─ history/              scraped pages, time-stamped captures
└─ wiki/                 the map atomic compiles — its own git repo
   ├─ index.md           member registry + your narrative
   ├─ repos/             summaries of no-signals repos
   ├─ concerns/          what cuts across repos
   └─ knowledge/         digests distilled from research/ and raw/
```

The repos and the `wiki/` folder are git repositories. The realm root and the capture folders (`research/`, `raw/`, `history/`) are not. You commit inside each member and inside `wiki/`, never at the realm root.

The realm `CLAUDE.md` is what makes this cohere. Claude Code walks up the directory tree when it loads `CLAUDE.md`, and the walk crosses repo boundaries, so a realm-root file stays in context from any session you start inside the realm, including one inside a member repo. Put your realm rules there: where each capture folder lives, what convention each follows, and a pointer to the wiki. That file is the control panel for everything below.


## Capture surfaces


Different material enters through different doors. Sort it by how processed it is when it arrives.

| Surface | Holds | Who writes it | Naming |
|---------|-------|---------------|--------|
| `research/` | Findings you produce: a vendor evaluation, an API investigation, a design tradeoff you worked out. | You, via Claude or by hand. | `YYYY-MM-DD-<slug>.md`, one file per topic. |
| `raw/` | Unprocessed dumps: PDFs, email exports, Slack exports, meeting transcripts, pasted notes, screenshots. | You drop them in as-is. | Whatever they arrive as. |
| `history/` | Scraped pages and time-stamped captures of source documents. | A scraper or a manual save. | Timestamp-named. |
| Tickets | Live issue tracker state. | Your tracker, read through MCP. | Not a folder. |

Each folder carries an `index.md` that lists what is in it. For `research/`, track the date, topic, file, and a status of `open` or `synthesized`. For `raw/`, track when each artifact arrived, what it is, and whether it has been indexed yet. The index files are what make the realm browsable and what a synthesis pass reads to find its work.

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


Capture surfaces collect material. Knowledge is what you distill from them. The flow is one-way:

```text
research/ + raw/   →   (synthesis with Claude)   →   wiki/knowledge/
```

A digest in `wiki/knowledge/` is the durable artifact: the thing you read later instead of re-reading the four sources behind it. The sources stay where they are. Synthesis reads them and writes a compressed, cross-linked result into the wiki.

The naive version is manual: point Claude at a file in `raw/`, tell it to distill the result into `wiki/knowledge/`, commit the wiki. That works and is the right place to start. It does not scale, because on the tenth run you cannot remember which dumps you have already folded in.


## Only reprocess what changed


Atomic does not provide this step; it is a pattern you implement, borrowing the same idea atomic uses internally for signals and wiki staleness. The move is a fingerprint. Snapshot a SHA-256 hash of every file in a capture folder, store it as a baseline, and on the next run diff the current hashes against the baseline. Only the new and changed files need synthesis. The baseline advances only after a synthesis succeeds, so running the diff twice without synthesizing never loses the work list, and an aborted run leaves the work pending instead of marking it done.

One layout that works, though the details are yours to decide:

```text
research/.fingerprints/
├─ baseline.sha256     hashes as of the last successful synthesis
├─ current.sha256      hashes now
└─ previous.sha256     one rotation back, for recovery
```

The script computes `current.sha256`, diffs it against `baseline.sha256`, and reports new, changed, and removed files. A `--promote` flag rotates `current` into `baseline`, run only after the synthesis it covers has landed. Keep the script out of the diff by ignoring `index.md` and the `.fingerprints/` directory itself.


## Wiring it into commands


Wrap the flow in commands of your own so it is one keystroke, not a recited procedure. These are commands you write and put in the realm's `.claude/commands/`; atomic does not ship them. A research-synthesis command could do this:

1. Run the fingerprint script against `research/` and read the new and changed files.
2. Distill each into `wiki/knowledge/<slug>.md`, cross-linking related entries and the repo signals they touch.
3. Mark each source `synthesized` in `research/index.md`.
4. Promote the fingerprint baseline.
5. Offer to commit the wiki repo, never committing automatically.

A second command does the same for `raw/`, with one extra step at the front: read each artifact (text directly, PDFs and images through tools), index it in `raw/index.md` with a one-line description, then distill. Removals are surfaced for your confirmation rather than deleting derived knowledge silently.

These commands are not shipped by atomic. They are the example of going past what atomic provides: atomic gives you the code wiki and the realm walk, and you add the knowledge pipeline on top in the same plain-markdown, deterministic-diff-plus-LLM-judgment style atomic uses everywhere else.


## Browsing the result


Everything in a realm is markdown in folders, which means an Obsidian vault or any markdown server renders it as a navigable graph. Three things make it click together:

- **An `index.md` in every surface**, listing its contents with links. These are the entry points.
- **Cross-links written during synthesis.** When a knowledge digest cites the research and the repo signals behind it, those become links you can follow.
- **`atomic wiki linkify`** on the code side, which turns the path citations in repo summaries and concern docs into relative markdown links. Run it after `/refresh-wiki`.

Open the realm root in your markdown tool and click from a concern, to the repo it touches, to the signals for that repo, to the research that explains a decision in it. The realm becomes one graph instead of a folder you grep.


## Where the line is


Atomic owns the code layer. `/refresh-wiki` walks the realm, documents the repos, and keeps the summaries and concerns current. It writes everything under `wiki/` except `knowledge/`.

You own the knowledge layer. Atomic never touches `research/`, `raw/`, `history/`, or `wiki/knowledge/`. The capture conventions, the synthesis commands, the fingerprint script, and the realm `CLAUDE.md` are yours to shape. The [wiki workflow reference](/reference/wiki-workflow) documents the code side in full; this guide is the pattern for the side that is yours.

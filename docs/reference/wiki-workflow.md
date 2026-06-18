# Wiki workflow

You work out of a folder. Call it a realm: a client engagement, a team's set of services, an open-source org you contribute to. It accumulates two kinds of things. Repositories, some yours and some vendored. And the loose material that collects around real work: a ticket you are halfway through, an email thread with the one detail that explains a bug, a PDF someone sent you, the notes you took chasing a problem across three systems. You keep that loose material by hand, in a `raw/` dump.

```
~/work/acme/                the realm
├─ CLAUDE.md           realm rules · ## Capture surfaces written by bucket add
├─ billing-api/        repo · signals → indexed
├─ gateway/            repo · signals → indexed
├─ legacy-cron/        repo · no signals → opt-in
├─ vendor-sdk/         repo · no signals → opt-in
├─ raw/                capture bucket · registered via `atomic wiki bucket add raw`
├─ research/           capture bucket · registered via `atomic wiki bucket add research`
├─ experiments/        code spikes and prototypes · user-maintained (not a bucket)
└─ wiki/               the map atomic compiles over it
   ├─ index.md         member registry + <wiki-buckets> block + your narrative
   ├─ repos/           summaries of the opt-in repos
   ├─ concerns/        what cuts across them
   ├─ knowledge/       topic-keyed digests synthesized from capture buckets
   └─ .buckets/        SHA-256 manifests — one dir per registered bucket
      ├─ raw/          current · baseline · previous
      └─ research/     current · baseline · previous
```

Holding all of that in your head is what makes a context-switch expensive. A wiki removes that cost. It is a knowledge base for one realm, compiled by `/refresh-wiki`, so the next time the realm needs explaining, the work is already done. Signals describe one repo; a wiki describes the realm above it. See [concepts](/reference/concepts#wikis) for the idea; this page is the mechanism.

Two layers fill a wiki, and they differ by who drives them.

**Atomic drives the repo layer.** `/refresh-wiki` walks the realm, finds the repositories, and documents them. A repo that already has signals is documented in place; the wiki references those signals and cites the path, never copying them. A repo without signals is opt-in, the kind that needs a deeper dive: you pick which ones get promoted to their own signals, and the rest Claude summarizes into `repos/` from a read-only pass that never writes back to the source. `concerns/` holds what cuts across them.

**You drive the knowledge layer through capture buckets.** Atomic never touches `raw/` or any other user-maintained folder. To bridge between loose material and the wiki, register the folder as a capture bucket. Atomic tracks what the wiki has already consumed and synthesizes only new or changed material on the next `/refresh-wiki`.

**You manage the realm from a `CLAUDE.md`.** Keep one at the realm root, holding your rules for the realm and a pointer to the wiki. Claude Code walks up the directory tree when it loads `CLAUDE.md`, and the walk crosses repo boundaries, so a realm-root file stays in context from anywhere inside the realm, including a session you start in a member repo. The realm level is where that earns its place: cross-cutting concerns, or a feature or bug you are tracing across several services, work that spans repos instead of sitting in one. Start there to organize `raw/`, fold it into the wiki, manage `research/` and `experiments/`, or reason across the repos at once.

This draws the boundary. Atomic stays on the code side: it documents repos, keeps signals current, runs the plan-implement-ship lifecycle, and bridges loose capture material into the wiki knowledge layer. The non-code work is yours to direct from the realm `CLAUDE.md`, and the wiki is the assistant layer for it, a Karpathy-style knowledge base you build with Claude instead of by hand.

Deterministic CLI verbs and one command do the work:

- **`atomic wiki scan [--root=<path>]`** — scaffolds the wiki, scans the root for member repos, classifies each, and registers the wiki globally. Deterministic, no model.
- **`atomic wiki stale [--root=<path>]`** — a read-only freshness verdict. Reports `DRIFT`/`STALE` lines for repos and concerns, plus `STALE bucket <name>` for capture folders with a non-empty diff. Exits `0` fresh, `1` stale, `2` error, mirroring `atomic signals stale`.
- **`atomic wiki linkify --root=<path>`** — renders the path citations in summaries, concerns, knowledge pages, and the index into file-relative markdown links. Deterministic, idempotent, no model.
- **`atomic wiki bucket add|list|diff|promote`** — manage capture buckets. `add` registers a folder and splices the `<wiki-buckets>` block; `list` shows status; `diff` gives a read-only change report; `promote` advances the baseline after successful synthesis. See [Capture buckets](#capture-buckets) below.
- **`/refresh-wiki [root]`** — the LLM pass. Runs the scan, reads the staleness verdict, refreshes only what drifted (summarizes no-signals repos, synthesizes pending capture buckets into `wiki/knowledge/`, re-synthesizes affected concerns), stamps fingerprints, and runs `atomic wiki linkify`. The refreshed wiki ships as a navigable graph.

The split is the same one signals use. The CLI does the deterministic work — walking the tree, classifying, registering, fingerprinting. The command does the judgment — summarizing repos and synthesizing the concerns that cut across them.

| Deterministic CLI | called by | LLM command |
|---|---|---|
| `atomic signals scan` | → | `/refresh-signals` |
| `atomic wiki scan` / `atomic wiki stale` | → | `/refresh-wiki` |


## Setting one up

From a folder that contains repositories:

```
/refresh-wiki
```

It runs `atomic wiki scan` — scaffolding `./wiki/` on the first run, re-scanning if it already exists — then drives the refresh. You can also run the scan directly to scaffold without the LLM pass:

```
atomic wiki scan                      # scaffold ./wiki from the current directory
atomic wiki scan --root ~/work/acme   # scaffold <path>/wiki
```

`--root` is a flag; the positional slot is reserved for the verb (`scan`, `stale`). With no flag, the root is the current directory.

The scan is idempotent. Re-running regenerates only the managed `<wiki-scan>` block in `index.md` — every summary, concern doc, and the narrative you or the LLM wrote is left untouched. It is init and refresh in one command, exactly like `/refresh-signals`.


## What a wiki looks like

```
<root>/wiki/
├── index.md      # managed <wiki-scan> + <wiki-buckets> blocks (CLI) + narrative (LLM)
├── README.md     # written by the scan
├── .gitignore    # ignores the transient .dirty marker
├── repos/        # summaries — only for repos without signals
│   ├── legacy-cron.md
│   └── vendor-sdk/<domain>.md   # large repos are split by domain
├── concerns/     # cross-cutting docs, one per concern
│   └── shared-auth.md
├── knowledge/    # topic-keyed digests synthesized from capture buckets
│   └── vendor-x.md             # sources: frontmatter stamped by code
└── .buckets/     # SHA-256 manifests — versioned wiki state, not gitignored
    └── research/ # current · baseline · previous
```

The scan and `/refresh-wiki` write everything here. `repos/` and `concerns/` are the repo-layer; `knowledge/` is the bucket-synthesis layer, written by the inferrer and stamped by code. `knowledge/` pages are not written by the scan — they require at least one registered bucket and a non-empty diff.

The wiki is its own git repository — `atomic wiki scan` runs `git init` for you. There is no in-file change log; the wiki's git history is the change log. `/refresh-wiki` ends by offering to commit, never automatically.

**OKF-aligned frontmatter.** The producer writes typed frontmatter on concept pages so `atomic serve` (and any OKF consumer) can color and filter by type:

- `wiki/knowledge/<topic>.md` — gets `type: Knowledge` and `description: <one-line>` when written by the inferrer's bucket-synthesis pass.
- `wiki/concerns/<name>.md` — gets `type: Concern` and `description: <one-line>` when written by `/refresh-wiki`.
- `wiki/repos/<name>.md` — shape unchanged; no `type:` frontmatter added. `atomic serve` colors them via a path-convention fallback, so they render as `repo`-type nodes without any frontmatter change.
- `index.md` — no frontmatter; the managed-block writer owns its content.

Cross-links between concept pages (knowledge → concern, concern → knowledge) use standard bundle-relative markdown links (`[text](/wiki/knowledge/topic.md)`) — the OKF §5.1 recommended form. `atomic serve` resolves these as in-shell navigable routes. Relative links work too; bundle-relative links are preferred for cross-directory references because they are shorter and survive file moves within the bundle.

The wiki is a navigable markdown graph. The scan writes a managed `## Members` section into `index.md` in OKF §6 listing form — each entry as `- [Name](target) - description`, with a one-line description drawn from the member's signals or a brief summary. `atomic wiki linkify` then turns the inline path citations across summaries, concerns, and the index into relative links to the files they name. Open the realm in Obsidian or any markdown server and click through it. The linkifier runs after fingerprint stamping, so it never disturbs staleness, and a rendered `[text](path)` link is a plain markdown link, not an `@`-reference.


## Repo states

`atomic wiki scan` classifies each member repo by whether it has `.claude/project/signals.md`, and records the result in the `<wiki-scan>` block:

| State | Meaning | Knowledge source |
|-------|---------|------------------|
| `indexed` | has signals | the wiki points at the in-repo signals and cites the path — never copies them |
| `summarized` | no signals | a summary at `wiki/repos/<repo>.md`, written by reading the repo without touching it |
| `pending` | no signals, fresh scan | the refresh pass resolves it to `indexed` or `summarized` |

"No signals" is a fork, not a defect. A repo you own can carry committed signals; an open-source dependency should not — the wiki summarizes it instead, never writing into it.

When the refresh pass meets a `pending` repo, it presents the no-signals repos as a numbered list and asks which to run `/refresh-signals` on, promoting those to `indexed`. The rest are summarized into the wiki by `atomic-signals-inferrer` in its wiki-output mode: it scans the repo with the substrate redirected outside it (`atomic signals scan --out`), infers, and writes the summary only into the wiki. The source repo is never modified.


## The registry

Each wiki registers its `index.md` path in a `<wikis>` block inside `~/.claude/CLAUDE.md`:

```
<wikis>
- /Users/you/work/acme/wiki/index.md
- /Users/you/oss/wiki/index.md
</wikis>
```

The block sits outside the `<atomic>` block, so `atomic claude update` preserves it. It is not `@`-referenced — it is a lightweight index that the session-start nudge and cross-wiki links read on demand, not context loaded into every session. The scan writes the entry idempotently: no duplicates on re-run, and the `<atomic>` block is never touched.


## Staleness

A wiki sits outside any single repo's lifecycle, so it cannot self-heal on commit the way signals do. `atomic wiki stale` gives a deterministic verdict instead. It reports two kinds of drift:

- **Membership and status** — repos added or removed since the last scan, or a repo that flipped between `indexed` and `pending`.
- **Content** — each summary and concern doc records what it was built from in its frontmatter (a `reflects_*` fingerprint), and the comparator checks that fingerprint against the repo's current state.

Those fingerprints are written by code, never by the model — `git rev-parse HEAD` for a summarized repo, a content hash of `signals.md` for an indexed one. The deterministic scan block cannot hold them, because it rewrites itself to *now* on every run, so they live in each artifact's frontmatter, stamped at author time. A missing or unparseable fingerprint counts as stale, so the verdict fails safe rather than passing silently.


## The forcing function

Two cheap detectors feed one nudge; the single heavy step (`/refresh-wiki`) clears it. Neither detector spawns git or re-runs a deterministic scan.

- **Neglect.** The session-start hook reads, per registered wiki, the scan block's `generated` date and whether a `.dirty` marker is present — stats and small reads only, zero git. It nudges if the wiki is older than a threshold (30 days by default) or marked dirty.
- **Drift.** When you ship from inside a member repo, the ship command checks whether your working directory is under a registered root — a string comparison, no git — and if so touches that wiki's `.dirty` marker. This is what turns "it has been a while" into "real changes are pending."
- **Heal.** `/refresh-wiki` clears `.dirty` and bumps the `generated` date only after a full refresh completes. An aborted run leaves the marker set, so the nudge persists until the work is actually done.

The wiki is nudge-driven, not guaranteed-fresh, by design. Acting on the nudge is near-instant when nothing drifted, which is what keeps it from becoming noise.


## Capture buckets

A capture bucket is a user-named folder at the realm root that holds loose material: research notes, raw email threads, ticket exports, PDFs, meeting notes. Registering it with `atomic wiki bucket add <name>` wires it into the wiki pipeline without changing anything inside the folder.

```
atomic wiki bucket add research    # register ~/work/acme/research/
atomic wiki bucket list            # show all buckets + pending/fresh status
atomic wiki bucket diff research   # see what changed since the last synthesis
atomic wiki bucket promote research   # advance the baseline after synthesis
```

**Two-phase contract.** `diff` is read-only: it computes a SHA-256 manifest of the current folder contents and compares against the stored baseline, reporting `new`, `changed`, and `removed` files. `promote` is a state change: it advances the baseline to the current manifest, marking the bucket in-sync. You run `promote` only after a successful synthesis so that a failed or aborted synthesis leaves the diff intact and `/refresh-wiki` retries.

**What gets synthesized.** `/refresh-wiki` runs the bucket-synthesis phase after repo summaries. For each bucket with a non-empty diff, it dispatches `atomic-signals-inferrer` in bucket-synthesis mode (fresh context per bucket). The inferrer reads the bucket's `index.md` (where you describe the bucket's purpose and conventions) and the changed files, then writes or updates topic-keyed pages under `wiki/knowledge/`. Multiple buckets' content about the same topic merges into one page; provenance lives in each page's `sources:` frontmatter, written by `atomic wiki stamp --knowledge` — the model declares which source files contributed, the code writes every SHA-256 value.

**The manifest.** SHA-256 fingerprints live in `wiki/.buckets/<name>/` as three files: `current` (written on every diff, debugging artifact only), `baseline` (what the wiki has consumed), and `previous` (the prior baseline). Manifests are wiki state and belong in the wiki repo's git history — they make the wiki self-describing on clone.

**Bucket index.** Each bucket must have an `index.md` at its root describing what the bucket is for and what conventions content in it follows. `atomic wiki bucket add` creates a stub; you fill it in (or let the `/refresh-wiki` offer flow guide you on first use). The inferrer reads this file as context before synthesizing knowledge pages.

**Staleness.** `atomic wiki stale` reports `STALE bucket <name>` for any bucket whose diff is non-empty. These lines appear after the existing `DRIFT`/`STALE` repo and concern lines, and the same exit-code contract applies: exit `1` if any line is emitted.

**Knowledge-page citations.** Concern docs can cite a knowledge page as `knowledge/<topic>.md@<sha256>`. `atomic wiki stale` resolves this as a content hash of the knowledge page file, the same fingerprint mechanism used for repo summaries. A knowledge page that has changed since the concern was authored triggers a `STALE concern` line.


## Relationship to signals

Signals and wikis are the same Karpathy-inspired pattern at two scales. Signals compile one repo's filesystem into a markdown model Claude reads every session. A wiki compiles a realm of repos into a markdown knowledge base one level up — pointing at the repos that already have signals, summarizing the ones that do not, writing up what they share, and synthesizing loose capture material into a knowledge layer. Neither replaces the other; the wiki points at signals, it never copies them.


## Federated code intelligence

A wiki realm can also carry a federated symbol graph. Running `atomic code index` at the realm root indexes each non-excluded member repo into a per-repo SQLite db under `<realm>/.atomic/<key>.db` — nothing is written into any member. Members whose status is `pending` or whose path is under `trash/` are seeded with `exclude = true` and skipped. Query verbs (`search`, `callers`, `callees`, `impact`, `explore`) then fan out across indexed members and group results under `[<key>]` headers. Use `--only`/`--exclude` to filter to specific members.

This is a complement to the wiki layer, not a replacement: the wiki provides prose summaries and cross-cutting concerns; the code-intel layer provides a queryable symbol graph. Both live at the realm root; neither touches member repos.

See [Code intelligence](/reference/code-intel#wiki-realm-federation) for the full contract: position-sensing, storage layout, fan-out output format, filtering, session-awareness block, and federation boundaries.

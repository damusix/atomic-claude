# Wiki workflow

You work out of a folder. Call it a realm: a client engagement, a team's set of services, an open-source org you contribute to. It accumulates two kinds of things. Repositories, some yours and some vendored. And the loose material that collects around real work: a ticket you are halfway through, an email thread with the one detail that explains a bug, a PDF someone sent you, the notes you took chasing a problem across three systems. You keep that loose material by hand, in a `raw/` dump.

```
~/work/acme/                the realm
├─ CLAUDE.md           realm rules · references the wiki
├─ billing-api/        repo · signals → indexed
├─ gateway/            repo · signals → indexed
├─ legacy-cron/        repo · no signals → opt-in
├─ vendor-sdk/         repo · no signals → opt-in
├─ raw/                non-repo dump: PDFs, emails, decks · user-maintained
├─ research/           findings and notes · user-maintained
├─ experiments/        code spikes and prototypes · user-maintained
└─ wiki/               the map atomic compiles over it
   ├─ index.md         member registry + your narrative
   ├─ repos/           summaries of the opt-in repos
   ├─ concerns/        what cuts across them
   └─ knowledge/       digests Claude distills from raw/
```

Holding all of that in your head is what makes a context-switch expensive. A wiki removes that cost. It is a knowledge base for one realm, compiled by `/refresh-wiki`, so the next time the realm needs explaining, the work is already done. Signals describe one repo; a wiki describes the realm above it. See [concepts](/reference/concepts#wikis) for the idea; this page is the mechanism.

Two layers fill a wiki, and they differ by who drives them.

**Atomic drives the repo layer.** `/refresh-wiki` walks the realm, finds the repositories, and documents them. A repo that already has signals is documented in place; the wiki references those signals and cites the path, never copying them. A repo without signals is opt-in, the kind that needs a deeper dive: you pick which ones get promoted to their own signals, and the rest Claude summarizes into `repos/` from a read-only pass that never writes back to the source. `concerns/` holds what cuts across them.

**You drive the knowledge layer.** Atomic never touches `raw/`; it is yours to maintain. To compress it, point Claude at the dump and tell it to add the result to your wiki, and a digest lands in `wiki/knowledge/`. The CLI creates neither folder. A wiki is plain markdown in a git repo, so distilling raw material into knowledge is Claude working on files at your direction.

**You manage the realm from a `CLAUDE.md`.** Keep one at the realm root, holding your rules for the realm and a pointer to the wiki. Claude Code walks up the directory tree when it loads `CLAUDE.md`, and the walk crosses repo boundaries, so a realm-root file stays in context from anywhere inside the realm, including a session you start in a member repo. The realm level is where that earns its place: cross-cutting concerns, or a feature or bug you are tracing across several services, work that spans repos instead of sitting in one. Start there to organize `raw/`, fold it into the wiki, manage `research/` and `experiments/`, or reason across the repos at once.

This draws the boundary. Atomic stays on the code side: it documents repos, keeps signals current, and runs the plan-implement-ship lifecycle, but it does not manage the realm around them. The non-code work is yours to direct from the realm `CLAUDE.md`, and the wiki is the assistant layer for it, a Karpathy-style knowledge base you build with Claude instead of by hand.

Two deterministic CLI verbs and one command do the work:

- **`atomic wiki scan [--root=<path>]`** — scaffolds the wiki, scans the root for member repos, classifies each, and registers the wiki globally. Deterministic, no model.
- **`atomic wiki stale [--root=<path>]`** — a read-only freshness verdict. Exits `0` fresh, `1` stale, `2` error, mirroring `atomic signals stale`.
- **`/refresh-wiki [root]`** — the LLM pass. Runs the scan, reads the staleness verdict, and refreshes only what drifted: summarizes no-signals repos, re-synthesizes affected concerns, updates the index.

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
├── index.md      # managed <wiki-scan> block (CLI) + narrative (LLM)
├── README.md     # written by the scan
├── .gitignore    # ignores the transient .dirty marker
├── repos/        # summaries — only for repos without signals
│   ├── legacy-cron.md
│   └── vendor-sdk/<domain>.md   # large repos are split by domain
├── concerns/     # cross-cutting docs, one per concern
│   └── shared-auth.md
└── knowledge/    # user-driven digests — written when you ask, not by the scan
    └── incident-4821.md
```

The scan and `/refresh-wiki` write everything here except `knowledge/`, the user-driven layer Claude fills on request by distilling your `raw/` dump into it.

The wiki is its own git repository — `atomic wiki scan` runs `git init` for you. There is no in-file change log; the wiki's git history is the change log. `/refresh-wiki` ends by offering to commit, never automatically.


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


## Relationship to signals

Signals and wikis are the same Karpathy-inspired pattern at two scales. Signals compile one repo's filesystem into a markdown model Claude reads every session. A wiki compiles a realm of repos into a markdown knowledge base one level up — pointing at the repos that already have signals, summarizing the ones that do not, and writing up what they share. Neither replaces the other; the wiki points at signals, it never copies them.

---
name: atomic-wiki
description: >
  Conversational wiki and capture-bucket routing. Fires when the user wants a
  place, space, or folder for notes, research, tickets, raw dumps, or knowledge
  capture — checks the <wikis> block in ~/.claude/CLAUDE.md; if the cwd is under
  a registered realm, creates the folder as a bucket via `atomic wiki bucket add`
  rather than a bare mkdir. Also fires on "add a bucket", "set up a karpathy wiki",
  "karpathy realm", "set up a wiki for my projects", "add this to my wiki",
  "wiki this", "what does my wiki know about X", "is my wiki stale".
  Routes per-scope: repo wikis use references/repo.md pipeline (docs/wiki/);
  realm wikis use references/realm.md pipeline (wiki/ in a separate repo).
  Not invoked by other commands — this skill is the conversational entry point
  for wiki and bucket operations; /refresh-wiki is the synthesis engine.
---

<trigger>

Auto-fires on:

- "I want a place for my notes / research / tickets / raw dumps"
- "create a space for [notes|tickets|research]" — any capture-intent phrase
- "add a bucket", "add this to my wiki", "wiki this"
- "set up a karpathy wiki", "karpathy realm", "set up a wiki for my projects"
- "what does my wiki know about X"
- "is my wiki stale"

</trigger>

Conversational entry point for wiki and capture-bucket operations. Code writes structure; the model writes meaning. Degrades gracefully when `atomic` binary is absent.

## Realm resolution

Read the `<wikis>` block in `~/.claude/CLAUDE.md` (outside `<atomic>`, CLI-managed). Each entry is a path to a registered wiki's `index.md`. The realm root is the directory containing `wiki/index.md`.

- **cwd under a realm root** → that realm is active. Bucket operations target it.
- **No registered realm + setup intent** → `atomic wiki scan --root <path>` bootstraps. Ask which folder is the realm root if ambiguous.
- **`atomic` binary absent** → say so; point at `/refresh-wiki` and `docs/reference/realm-wiki.md`. Do not hand-build the structure.

## Pipeline routing

When a full inference run is needed (refresh, summarize), detect scope and load the matching reference:

1. Read `<wiki-type>` from the active wiki index (`docs/wiki/index.md` for repo scope, `wiki/index.md` for realm scope).
2. **Repo scope** (`<wiki-type>repo</wiki-type>`) — the full per-repo pipeline lives at `~/.claude/skills/atomic-wiki/references/repo.md` (installed location). A full refresh is triggered via `/refresh-wiki`, which dispatches `atomic-wiki-inferrer` to execute it.
3. **Realm scope** (`<wiki-type>realm</wiki-type>`) — the wiki-output pipeline lives at `~/.claude/skills/atomic-wiki/references/realm.md` (installed location). A full refresh is triggered via `/refresh-wiki [root]`, which runs the scan, makes its offers, re-authors the artifacts the user picks, and dispatches `atomic-wiki-inferrer` for each inference step.

The pipeline text lives **only** in the reference files (installed at `~/.claude/skills/atomic-wiki/references/`) — not here, and not duplicated in the agent's system prompt. When an agentic inference run is needed, the agent resolves `$HOME` via Bash and reads the appropriate reference file at its absolute path, then executes it.

## Bucket creation route

When the user wants a folder for notes, tickets, research, or any loose material and the cwd is under a registered realm:

1. Run `atomic wiki bucket add <name> --root <realm-root>` — never bare `mkdir`.
   - Creates `<realm-root>/<name>/index.md` (stub: purpose line + `## Conventions` placeholder).
   - Splices a `<bucket name="<name>" path="<abs-path>"/>` entry into the `<wiki-buckets>` block in `wiki/index.md`.
   - On the first bucket in the realm, writes a `## Capture surfaces` section to the realm `CLAUDE.md`.
2. Drive the meaning-fill: ask the user what the bucket is for, then replace the `<!-- describe what this bucket is for -->` placeholder in the realm `CLAUDE.md` `## Capture surfaces` section and write the bucket's `index.md` purpose line + `## Conventions` block to match. **Code writes structure; the model writes meaning.**

Reserved name `wiki` is refused by the binary. If `<wiki-buckets>` carries `declined="true"`, `bucket add` removes the attribute when registering a new bucket.

## Bucket doc authoring route

When the user wants a new topic file inside a registered bucket, or asks about a bucket's own conventions:

- `atomic wiki bucket doc <bucket> <slug> [--router]` scaffolds `<bucket>/<slug>.md` from the embedded template (six-key frontmatter, `created` pre-stamped). `--router` also creates `<bucket>/<slug>/` + a `CLAUDE.md` stub for a topic that outgrows one file. Refuses on an existing target — never overwrites.
- `atomic wiki bucket skill <bucket>` scaffolds `<realm-root>/.claude/skills/<bucket>-management/SKILL.md`, pre-filled with the bucket's purpose line. No-op if the file exists.
- `atomic wiki bucket index [<bucket>]` rebuilds the `<bucket-docs>` region in one bucket's `index.md` (or every registered bucket when omitted) plus the realm `<wiki-bucket-list>` region in `wiki/index.md`. `atomic wiki scan` already runs this as part of its pass — only reach for the verb directly to force a rebuild outside a scan.

See `references/realm.md` for the full frontmatter contract.

## Karpathy-wiki setup route

When the user asks to set up a wiki for a folder of projects:

1. Run `atomic wiki scan --root <realm-root>` to scaffold `wiki/` and classify member repos.
2. Offer bucket names — `research`, `raw`, `tickets` are examples; user defines the actual names.
3. Create each named bucket via `atomic wiki bucket add <name>`.
4. Fill in meaning for each bucket's `index.md` and `## Capture surfaces` entry.
5. Point at `/refresh-wiki` for the pass that writes `wiki/repos/` summaries and `wiki/concerns/`.

Wiki repo commits happen inside `wiki/` — never the realm root.

## Query and staleness route

- **"what does my wiki know about X"** → read `wiki/index.md`, `wiki/repos/`, `wiki/concerns/`, and each registered bucket's `index.md` for the active realm. Summarize what is present; say explicitly if nothing matches.
- **"is my wiki stale"** → run `atomic wiki stale --root <realm-root>`. It reports membership drift only. Exit 0 = no drift. Exit 1 = stdout lists `DRIFT added|removed|status` lines. Exit 2 = hard error. Nothing tracks whether a summary or concern is current; `/refresh-wiki [root]` re-authors the ones the user picks.

## Conventions that prevent foot-guns

- `<wiki-scan>` and `<wiki-buckets>` blocks in `wiki/index.md` are code-owned — never hand-edit them.
- Edit a single summary or concern directly for a small change; run `/refresh-wiki [root]` to re-author several.

<constraints>

## Boundaries

- **Complements /refresh-wiki**: `/refresh-wiki` is the synthesis engine (repo signals refresh, realm repo summaries, concern re-auth, linkify). This skill is the conversational entry point — it registers and describes; `/refresh-wiki` synthesizes.
- **Not invoked by other commands.** No cross-reference needed in the other direction.
- **Never bare mkdir** for capture intent in a registered realm. `atomic wiki bucket add` is the only correct path — it writes the index stub and the registry entry.

</constraints>

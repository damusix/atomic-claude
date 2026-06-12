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
- **`atomic` binary absent** → say so; point at `/refresh-wiki` and `docs/reference/wiki-workflow.md`. Do not hand-build the structure.

## Bucket creation route

When the user wants a folder for notes, tickets, research, or any loose material and the cwd is under a registered realm:

1. Run `atomic wiki bucket add <name> --root <realm-root>` — never bare `mkdir`.
   - Creates `<realm-root>/<name>/index.md` (stub: purpose line + `## Conventions` placeholder).
   - Creates `wiki/.buckets/<name>/` (empty manifest dir).
   - Splices a `<bucket name="<name>" path="<abs-path>"/>` entry into the `<wiki-buckets>` block in `wiki/index.md`.
   - On the first bucket in the realm, writes a `## Capture surfaces` section to the realm `CLAUDE.md`.
2. Drive the meaning-fill: ask the user what the bucket is for, then replace the `<!-- describe what this bucket is for -->` placeholder in the realm `CLAUDE.md` `## Capture surfaces` section and write the bucket's `index.md` purpose line + `## Conventions` block to match. **Code writes structure; the model writes meaning.**

Reserved name `wiki` is refused by the binary. If `<wiki-buckets>` carries `declined="true"`, `bucket add` removes the attribute when registering a new bucket.

## Karpathy-wiki setup route

When the user asks to set up a wiki for a folder of projects:

1. Run `atomic wiki scan --root <realm-root>` to scaffold `wiki/` and classify member repos.
2. Offer bucket names — `research`, `raw`, `tickets` are examples; user defines the actual names.
3. Create each named bucket via `atomic wiki bucket add <name>`.
4. Fill in meaning for each bucket's `index.md` and `## Capture surfaces` entry.
5. Point at `/refresh-wiki` for the synthesis pass that writes `wiki/repos/`, `wiki/concerns/`, and `wiki/knowledge/`.

Wiki repo commits happen inside `wiki/` — never the realm root.

## Query and staleness route

- **"what does my wiki know about X"** → read `wiki/index.md`, `wiki/knowledge/`, `wiki/repos/`, `wiki/concerns/` for the active realm. Summarize what is present; say explicitly if nothing matches.
- **"is my wiki stale"** → run `atomic wiki stale --root <realm-root>`. Exit 0 = fresh. Exit 1 = stale: stdout lists `DRIFT`/`STALE` lines (including `STALE bucket <name>` for buckets with a non-empty diff). Exit 2 = hard error.

## Conventions that prevent foot-guns

- `<wiki-scan>` and `<wiki-buckets>` blocks in `wiki/index.md` are code-owned — never hand-edit them.
- `wiki/concerns/` files need a `reflects:` stamp or they perpetually STALE-nag. Route through `/refresh-wiki`; or stamp after a light single-file edit with `atomic wiki stamp`.
- Knowledge pages (`wiki/knowledge/<topic>.md`) are stamped via `atomic wiki stamp --knowledge` — never write `sources:` frontmatter by hand; the model declares which bucket files contributed, code writes every SHA-256 value.
- Light single-file edit + stamp vs full `/refresh-wiki` for anything multi-artifact — full refresh is for batch staleness.

<constraints>

## Boundaries

- **Complements /refresh-wiki**: `/refresh-wiki` is the synthesis engine (repo summaries, bucket synthesis, concern re-auth, linkify). This skill is the conversational entry point — it registers and describes; `/refresh-wiki` synthesizes.
- **Not invoked by other commands.** No cross-reference needed in the other direction.
- **Never bare mkdir** for capture intent in a registered realm. `atomic wiki bucket add` is the only correct path — it wires the manifests and registry.

</constraints>

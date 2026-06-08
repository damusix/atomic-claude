---
description: Bootstrap and maintain project documentation surfaces. Two modes: bootstrap (discover doc files, index them in CLAUDE.md) and authoring (scan for unindexed docs, match diff against indexed surfaces, walk stale/incomplete/missing items with Yes/Later/Remind/Skip).
---

Run `/documentation` to bootstrap doc surface indexing or perform a full documentation pass.

## Flags

- `--print-template` — print the `## Documentation surfaces` table skeleton to stdout and exit. Paste into your CLAUDE.md to declare custom surfaces manually.
- `--dry-run` — print discovered/stale surfaces without applying edits or staging anything.
- `--discover` — re-scan and offer to update the surfaces table even when a table already exists. Use after adding new doc files.
- `<range>` — any valid git range (`HEAD~5..HEAD`, `main..feature-branch`). If omitted, defaults to `<base>..HEAD` where `<base>` is the merge-base with `main`.

## Step 0 — Handle flags

If `--print-template` is present, print the following and exit:

```markdown
## Documentation surfaces

| Path | Covers | Voice |
|------|--------|-------|
| `docs/architecture/payments.md` | billing, webhooks, Stripe | atomic-prose |
| `docs/models/README.md` | data model, ERD, migrations | atomic-prose |
| `docs/api/endpoints.md` | REST API, auth, rate limits | atomic-prose |
| `README.md` | project overview, quick start | atomic-prose |
```

Add this section to your committed `CLAUDE.md` and fill in your project's doc files. Voice values: `atomic-prose`, `terse-technical`.

## Step 1 — Detect mode

Check whether a `## Documentation surfaces` table exists in the project's committed CLAUDE.md (search `CLAUDE.md` at the git root; also check `claude.local.md` / `CLAUDE.local.md`).

- **Table absent OR `--discover` flag present** → run bootstrap mode (Step 2).
- **Table present AND no `--discover` flag** → run authoring mode (Step 5).

## Step 2 — Bootstrap: scan doc files

Run the deterministic scan:

```bash
atomic docs scan 2>/dev/null
```

If `atomic` is absent or returns a non-zero exit, fall back to:

```bash
find docs/ doc/ documentation/ wiki/ ADR/ adr/ decisions/ -name '*.md' 2>/dev/null
find . -maxdepth 3 -name 'README.md' 2>/dev/null | grep -v node_modules | grep -v .git
```

Read the cache file written to `.claude/project/doc-surfaces.md` (or use the find output directly if the binary is absent).

If no doc files are found, print:

```
no documentation files found. Run with --discover after adding docs.
```

and exit.

## Step 3 — Bootstrap: present discovered surfaces

Print a numbered list (axiom 4 — plain-text indexed selection):

<example>
```
Discovered N documentation files. Index these in your CLAUDE.md?

  [1] docs/architecture/payments.md — Payments / Webhook Flow / Stripe Integration
  [2] docs/models/README.md — Data Model / Entity Relationships / Migration History
  [3] README.md — (no headings extracted)
  ...

Type: all | 1 3 5 | none
```
</example>

Wait for user input. Accept: `all`, space-separated indices (`1 3 5`), comma-separated (`1,3,5`), ranges (`1-3`), mixed (`1-3 5`), `none`.

If `none`, print `no surfaces indexed.` and exit.

## Step 4 — Bootstrap: write the surfaces table

For each selected surface:

1. Suggest a `Covers` annotation derived from headings (H1 + first 3 H2s). Ask the user to confirm or replace:

   ```
   [1] docs/architecture/payments.md
   Suggested covers: payments, webhook flow, Stripe integration
   Accept or type replacement (Enter to accept):
   ```

2. After confirming all selected surfaces, locate the target CLAUDE.md:
   - Use the committed `CLAUDE.md` at the git root if it exists.
   - If no CLAUDE.md exists, create one.

3. Append the `## Documentation surfaces` section to that file:

   ```markdown
   ## Documentation surfaces

   | Path | Covers | Voice |
   |------|--------|-------|
   | `docs/architecture/payments.md` | payments, webhook flow, Stripe integration | atomic-prose |
   ```

4. Stage the file:

   ```bash
   git add CLAUDE.md
   ```

5. Print:

   ```
   indexed N surfaces in CLAUDE.md.
   Run /documentation again to walk stale or missing docs.
   ```

Exit. Bootstrap complete.

## Step 5 — Authoring: check for unindexed docs

Run `atomic docs scan` (or the Bash fallback from Step 2). Compare the scan results against the paths already in the `## Documentation surfaces` table.

If unindexed doc files are found, present them as a numbered list:

```
Found N doc files not in your surfaces table. Add them?

  [1] docs/api/new-endpoints.md — API Reference / Authentication / Rate Limits
  [2] docs/guides/getting-started.md — (no headings extracted)

Type: all | 1 3 5 | none
```

For selected items, run Step 4's annotation + append loop, then continue.

## Step 6 — Authoring: resolve the diff range

If the user supplied a `<range>` argument, use it verbatim.

If no argument was supplied, resolve the base:

```bash
git merge-base HEAD main 2>/dev/null || echo "main"
```

Then run:

```bash
git diff <base>..HEAD
```

If the diff is empty, print `no changes in range; skipping stale detection.` and proceed to Step 7 (missing detection).

## Step 7 — Authoring: classify surfaces

Read the `## Documentation surfaces` table. For each surface:

- **Stale** — the diff touches paths or concepts mentioned in the `Covers` column for this surface.
- **Incomplete** — the diff adds something (new entity, new endpoint, new step) related to the surface's covers, but the surface doesn't mention it yet.
- **Missing** — domains identified in project signals (`signals.md`) that have 5+ source files with no corresponding doc surface within two directory levels. Only suggest new pages in authoring mode; never during commit flow.

**Code-intel blast-radius (when index is present).** If a code-intel index exists (`atomic code search` responds without error), the changed-symbol impact sweep — determining which symbols from the diff affect which doc surfaces — should be DELEGATED to a subagent (`atomic-investigator` or `atomic-haiku`). Brief it with the list of changed symbols and ask it to run `atomic code impact <symbol>` for each and return a compact "symbol → affected surfaces" digest. The main `/documentation` agent must NOT run `atomic code impact` inline — these queries are token-heavy and belong in a disposable subagent thread. Consume the returned digest to refine stale/incomplete classification. **Degrade:** when no index is present or the subagent reports the binary is absent, fall back to the existing diff-vs-covers judgment above (no code-intel path, no error).

If `--dry-run` was supplied, print the classifications and exit:

```
dry-run: classified surfaces

  stale (1):
    docs/models/README.md — migration adds refund_status column
  incomplete (1):
    docs/api/endpoints.md — new POST /payments/refund endpoint
  missing (1):
    notifications/ (8 files) — no doc surface found

no edits applied.
```

## Step 8 — Authoring: walk surfaces

Walk surfaces one at a time. For each **stale** or **incomplete** surface, print:

<example>
```
surface <N>/<total>: <path>
status: stale | incomplete
reason: <why it's stale or incomplete>

  [y] Yes    — update now (skill edits the file, stages)
  [l] Later  — create a follow-up entry
  [r] Remind — schedule a reminder
  [s] Skip   — no action
```
</example>

Wait for the user to type one of `y`, `l`, `r`, `s`.

For each **missing** surface, print:

<example>
```
surface <N>/<total>: <module>/ (N files)
status: missing — no doc surface covers this module

  [n] New    — generate a full page draft
  [s] Skip   — no action
```
</example>

Wait for `n` or `s`.

### Yes (`y`)

Open `<path>`. Read the current content and the diff. Generate a targeted edit:

- Stale ERD → add the new field to the Mermaid block and field table.
- Stale flow → insert the new step in the diagram and update surrounding prose.
- Incomplete API reference → add a new row for the new endpoint.
- Preserve existing diagram style (orientation, node naming, color scheme).
- Update any stale code examples in the file to reflect the current API.

Apply the edit, then stage the file:

```bash
git add <path>
```

### Later (`l`)

Create a follow-up entry:

```bash
atomic followups add \
  --id "doc-<slug>-<short-hash>" \
  --title "update <path> — <one-line reason>" \
  --severity nit \
  --origin "/documentation authoring"
```

If `atomic` is absent, print the follow-up details and ask the user to add it manually.

### Remind (`r`)

Prompt for timing using natural-language inference (same as `/remind-me`):

```
Remind when? (e.g. "tomorrow", "after the PR", "end of week"):
```

Run `/remind-me <timing> update <path> — <reason>`.

### Skip (`s`)

No action, no record.

### New (`n`)

Generate a full page draft for the missing module. Pick the doc type based on content:

- Module with data models → domain guide with ERD (`erDiagram`), field tables, business rules.
- Module with API routes → API reference with endpoint table, request/response examples.
- Multi-step process → flow doc with Mermaid flowchart, step descriptions.
- Architectural decision → ADR with context, decision, consequences.

Follow `atomic-prose` voice: short intro sentence, tables for comparisons, Mermaid diagrams with one-sentence captions, plain language, no LLM-tell filler.

Write the new file. Then offer to add it to the surfaces table:

```
Created docs/<module>.md.
Add to your ## Documentation surfaces table? [y/n]
```

If yes, append a row to the table in CLAUDE.md and stage it. Note where the new page should be linked (index page, sidebar, README).

Stage the new file:

```bash
git add docs/<module>.md
```

## Step 9 — Print summary

After all surfaces are walked, print:

<example>
```
documentation pass complete.

  updated (staged):
    docs/models/README.md — added refund_status to ERD and field table
    docs/api/endpoints.md — added POST /payments/refund endpoint

  deferred:
    docs/architecture/payments.md — follow-up created

  reminded:
    docs/onboarding.md — reminder set for end of week

  skipped:
    docs/legacy/v1.md

  created:
    docs/notifications.md — added to surfaces table

  total: <N> surfaces / <Y> updated / <L> deferred / <R> reminded / <S> skipped / <C> created
```
</example>

## Rules

<constraints>

- This command does not commit. Edits are staged; the user commits via a ship verb. **Why:** commit is an explicit, user-initiated act — staging is reversible, committing is not.
- `--dry-run` prints the proposal and exits without touching any file. **Why:** lets users audit what would change before trusting the skill to edit docs.
- `--print-template` exits immediately after printing; no scan is performed. **Why:** template output is a side-channel tool, not a doc pass — mixing the two would be surprising.
- `--discover` re-runs bootstrap surface selection even when a table already exists. Use it after adding new doc directories. **Why:** surfaces grow; the table needs an explicit opt-in path to add new entries without wiping the old ones.
- Missing detection (Step 7) runs only in authoring mode. Never during commit flow (ship verbs). **Why:** commit flow is fast-path — proposing new doc pages mid-commit breaks the user's flow and slows every commit.
- When creating or updating a file, generate full Mermaid syntax — never describe what a diagram would look like. Every Mermaid block gets a one-sentence caption. **Why:** prose descriptions of diagrams are useless to readers and cannot be rendered; captions anchor the diagram's purpose without making the reader decode the graph first.
- The surfaces table belongs in the committed `CLAUDE.md` so the whole team shares it. Exception: repos where `CLAUDE.md` is a bundle source (like the atomic-claude repo itself) may use `claude.local.md` instead — the bootstrap step can write there if the user directs it. **Why:** a table only one person sees provides no coordination value; the exception handles repos where `CLAUDE.md` is installed globally and must stay project-neutral.
- The voice rules and surface taxonomy live in `skills/atomic-documentation/SKILL.md`. This command does not duplicate them. **Why:** single source of truth — duplication drifts silently.

</constraints>

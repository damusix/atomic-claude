---
description: Orchestrate a diff-scoped documentation update. Invokes the atomic-documentation skill with a git diff, parses proposed surfaces, walks per-surface accept/skip/continue prompts, applies edits, and stages. Does not commit — that is the user's choice via a ship verb.
---

Run `/documentation` to trigger a documentation impact pass over a range of commits, then apply any proposed edits surface by surface.

## Flags

- `--print-template` — print the `## Documentation surfaces` override-table skeleton to stdout and exit. Paste this into your `claude.local.md` to declare custom surfaces for a non-atomic repo.
- `--dry-run` — print the skill's proposed surfaces without applying edits or staging anything.
- `<range>` — any valid git range (`HEAD~5..HEAD`, `main..feature-branch`). If omitted, defaults to `<base>..HEAD` where `<base>` is the merge-base with `main`.

## Step 0 — Handle flags

If `--print-template` is present, print the following and exit with no further steps:

```markdown
## Documentation surfaces

| Diff signal | Surface | Voice |
|-------------|---------|-------|
| New file in `src/api/routes/*.ts` | `docs/api.md` | atomic-prose |
| Public function added to `pkg/*/exports.go` | `docs/reference.md` | spec-design |
```

Add this section to your `claude.local.md` (or `CLAUDE.md`) and rerun `/documentation` to apply the overrides. Voice values: `atomic-prose`, `spec-design`, `llm-reference`.

## Step 1 — Resolve the diff range

If the user supplied a `<range>` argument, use it verbatim.

If no argument was supplied, resolve the base:

```bash
git merge-base HEAD main 2>/dev/null || echo "main"
```

Then run:

```bash
git diff <base>..HEAD
```

If the diff is empty, print `no changes in range; nothing to document.` and exit.

## Step 2 — Invoke the atomic-documentation skill

Pass the full diff text to the `atomic-documentation` skill. The skill analyzes the diff against the surface routing table, reads any `## Documentation surfaces` override in `claude.local.md` / `CLAUDE.md`, and emits a fenced `yaml` block as its final output.

## Step 3 — Parse the skill output

Search the skill's response for the **last** fenced code block tagged `` ```yaml `` or `` ```yml `` (both accepted).

Parse rules:

1. If the last fenced `yaml`/`yml` block is present, parse it as YAML.
2. On YAML parse error, treat as no surfaces. Log: `skill output could not be parsed; treating as no doc impact.`
3. If no fenced `yaml`/`yml` block is present, treat as no surfaces.
4. If the parsed YAML has no `surfaces` key or `surfaces` is not a list, treat as no surfaces.
5. Surfaces with unknown `voice` values are skipped with a note: `skipping <path>: unknown voice <value>.`
6. Surface entries missing `path` or `voice` are skipped with a note: `skipping incomplete surface entry.`
7. `surfaces: []` is valid and means the skill found no doc impact.

If the result is empty after parsing, print `no doc impact detected.` and exit.

## Step 4 — Walk surfaces (skipped in --dry-run mode)

If `--dry-run` was supplied, print the parsed surfaces list and exit without applying anything:

```
dry-run: proposed surfaces
  1. README.md (atomic-prose) — new file commands/foo.md
  2. CLAUDE.md (llm-reference) — new file commands/foo.md
no edits applied.
```

Otherwise, walk surfaces one at a time. For each surface, print:

```
surface <N>/<total>: <path>
voice:  <voice>
reason: <reason>
change: <suggested_change>

  [e] edit   — open file, apply the suggested change, re-stage
  [s] skip   — record a reason; no edit
  [c] continue — treat as misclassification; no edit, no note
```

Wait for the user to type one of `e`, `s`, or `c`.

**edit** (`e`): Open `<path>` and apply the `suggested_change`. After applying, stage the file:

```bash
git add <path>
```

**skip** (`s`): Ask for a reason (`skip reason: `). Record the path and reason in the run summary. Do not stage anything for this surface.

**continue** (`c`): Treat the skill's classification as a misclassification. No edit, no skip note in the summary.

## Step 5 — Print summary

After all surfaces are walked, print a summary:

```
documentation pass complete.

  edited (staged):
    README.md — added commands table row
    CLAUDE.md — appended Other commands entry

  skipped:
    docs/spec/foo.md — skip reason: spec already covers this

  misclassified (continued):
    docs/design/bar.md

  total: <N> surfaces / <E> edited / <S> skipped / <C> continued
```

## Rules

- This command does not commit. Edits are staged; the user commits via a ship verb.
- `--dry-run` prints the proposal and exits without touching any file.
- `--print-template` exits immediately after printing; no diff is taken.
- The voice rules and surface taxonomy live in `skills/atomic-documentation/SKILL.md`. This command does not duplicate them.
- Apply edits as described in `suggested_change`. If the change is unclear, apply the closest reasonable interpretation and note it in the summary. Do not abort the walk on an ambiguous entry.
- Never open or apply changes to files outside the paths emitted by the skill.

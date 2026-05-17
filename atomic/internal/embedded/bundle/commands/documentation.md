---
description: Update or create project documentation. Ensures README.md, claude.md, docs/spec/, and docs/design/ are accurate after significant changes.
---

Sync repo documentation to current state. Run after any significant change. Run on demand.

## Scope

This command manages four documentation surfaces:

| Surface | Audience | Lives in | Updated when |
|---------|----------|----------|--------------|
| `README.md` | Humans (users + contributors) | repo root | Public behavior, install, usage, or top-level architecture changed |
| `claude.md` | Future Claude sessions | repo root | Conventions, commands, file layout, or load-bearing context changed |
| `docs/spec/<topic>.md` | Future implementers | `docs/spec/` | Implementation contract for a feature changed |
| `docs/design/<topic>.md` | Humans deciding what to build | `docs/design/` | Design rationale, alternatives considered, trade-offs |

## Steps

1. Scan repo state:
    - `git log -n 20 --oneline` to see recent change shape
    - `git diff <last-doc-update>..HEAD` if the user pointed at a commit range, else `git diff main..HEAD`
    - List existing docs: `ls README.md claude.md docs/ 2>/dev/null`
2. For each surface above, decide: **create**, **update**, or **skip (still accurate)**. State the decision per surface in one line before editing.
3. Apply edits:
    - **README.md** — human prose. Keep marketing-free. Sections: what it is, why, install, usage, links. No AI bylines. If creating, use existing similar projects in the org for tone (`gh repo list <owner> --limit 5`).
    - **claude.md** — MEANINGFUL CONTEXT ONLY. Conventions the AI can't infer from code, commands to run, file layout that isn't obvious, gotchas. NOT: restating code, NOT: full architecture essay, NOT: a tutorial. If a line could be derived by reading the code, delete it.
    - **docs/spec/** — implementation contract. What inputs, outputs, error modes, invariants. Mermaid diagrams render here — use `sequenceDiagram` for flows, `erDiagram` for data, `flowchart` for state. One file per feature/subsystem.
    - **docs/design/** — design rationale. What was considered, what was picked, why. Mermaid diagrams welcome. Snapshot in time — okay to date the file.
4. After edits, run a sanity pass:
    - Every code path mentioned in README install/usage actually works (run it if cheap).
    - Every command mentioned in claude.md actually exists in `package.json` / `Makefile` / etc.
    - No dead links in docs/.
5. Print a summary: which files changed, what changed in each, what was skipped and why.

## Diagrams

`docs/` renders Markdown — use Mermaid. Caption each diagram with a one-sentence summary so non-rendering readers (grep, raw text view) still get the gist.

```markdown
**Auth request flow:**

\```mermaid
sequenceDiagram
  Client->>API: POST /login
  API->>DB: SELECT user WHERE email
  ...
\```
```

Replies in the TUI use ASCII, not Mermaid (TUI doesn't render).

## Rules

- `claude.md` stays lean. Aggressively delete lines that don't earn their slot. A 30-line `claude.md` beats a 300-line one because Claude reads the whole thing every session.
- Do NOT commit unless user asked. This command edits files only.
- Do NOT create files in `docs/` unless content warrants it. Empty stubs rot.
- Match the project's existing tone if README/claude.md already exist. Don't rewrite to match a template.
- Never include AI bylines or "Generated with Claude Code" anywhere.

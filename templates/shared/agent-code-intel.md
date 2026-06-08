{{- define "agent-code-intel" -}}
## Code-intel index

When `.claude/.atomic-index/atomic.db` is present and `atomic` is on PATH, prefer `atomic code` verbs for location and relationship questions — they query a pre-built symbol graph and return results that grep cannot replicate:

- `atomic code explore "<query>"` — **reach for this first when scoping an unfamiliar area.** Takes a natural-language query and returns a bundled context digest (markdown): the relevant symbols, files, and relationships in one shot, instead of you issuing four separate queries and stitching the results together. Use it to orient, then drill in with the targeted verbs below.
- `atomic code search <symbol>` — where a symbol is defined and used (outranks sg/grep for this question)
- `atomic code callers <symbol>` — all callers of a function or method across the codebase
- `atomic code callees <symbol>` — all symbols a function calls
- `atomic code impact <symbol>` — blast radius of changing a symbol (transitive callers)

Add `--json` to any query verb for machine-parseable output when processing results programmatically.

**Bounded queries only.** Scope every query — one `explore` question or one symbol at a time. Never attempt to dump or sweep the full graph; the index answers a specific question, it is not a corpus to read.

**Graceful degradation — non-negotiable.** Before querying, confirm the path is live: `atomic` on PATH, `.claude/.atomic-index/atomic.db` exists, and the query returns usable output. On any failure — binary absent, DB missing, query error — fall back silently to sg/grep/heuristics. Never print an error about the index being unavailable; never block because it is missing. The query is an enhancement; grep is the floor. This matters because the artifacts install into user repos that never ran `atomic code index`.

**Why the index exists.** It reflects working-tree state at the last `atomic code sync`. It is authoritative for existing symbols at that point in time. The orchestrator (not the subagent) owns keeping the index fresh — the subagent only queries.
{{- end -}}

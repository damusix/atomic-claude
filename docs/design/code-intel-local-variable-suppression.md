# Code-intel: suppress function-scoped local variables


## Problem


TS/JS function-body locals are minted as `variable` graph nodes parented to the **file** node. The extractor configs never model `arrow_function` / `function_expression` as function scopes (`extraction/languages/javascript.go:44-47` defers this explicitly), so `visitChildren` descends through callback bodies and hits `lexical_declaration` with the file still on top of the node stack. Locals inside *named* functions are already suppressed (`extraction/extractor.go` `visitFunctionBody` has no `VariableTypes` arm) — the current behavior is asymmetric, not a policy.

Measured on taxgentic/server (10,819 nodes): 5,473 `variable` nodes (51% of the graph), 4,806 with no edge beyond inbound `contains` — an orphan starfield covering 44% of the rendered graph. 801 nodes are *named a destructuring pattern* (`{ cookie }` ×172, `[, err]` ×58). Beyond rendering: the name matcher grants no kind-affinity exclusion to variables (`resolution/name_matcher.go:575-583`), so cross-file `calls` edges resolve to test locals (`NOW` at `format.test.ts:5` receives calls from four other files) — false edges in the graph agents consume.


## Goals / Non-goals


- Goals: uniform local-variable suppression across scope-opening constructs; no node named a non-identifier pattern; self-healing migration (existing indexes must not silently keep stale locals); byte-identical output for languages without `VariableTypes`.
- Non-goals: arrow functions as first-class function nodes (separate, spec-sized change — see Approaches B); per-binding destructuring nodes; UI-only filtering; changing which module-scope variables are kept.


## Approaches


| # | Approach | Pros | Cons |
|---|----------|------|------|
| A | Extraction-time scope suppression: new optional `FunctionScopeTypes` config set + visitor `scopeDepth`; `VariableTypes` mints only at depth 0; walk continues so calls/JSX/field refs in initializers still harvest | Fixes index, search, resolution, display in one place; makes existing named-function suppression uniform; ~20 lines + config; zero existing test assertions break | SQL literal ownership coarsens: 1,248 string-match + 14 embedded refs re-attribute from variable owner to `file:`, widening pass-B anchor scope to whole-file |
| B | Promote `arrow_function`/`function_expression` to `FunctionTypes` (the deferred CP8 change) | Real function nodes; tight SQL-literal owners; 7,085 file-attributed `calls` edges (79% of all calls in sample) gain their true caller | Anonymous callbacks have no extractable name (needs a naming hook); appendix-E order means naive promotion silently loses every call inside an arrow-const body; large blast radius across all TS/JS fixtures — its own spec |
| C | Display-only filter in `serve` (drop contains-only variables at the graph endpoint) | One file | Index bloat and search noise persist; the cross-file false `calls` edges — a correctness bug — persist; CLI/MCP and UI diverge on what exists |


## Recommendation


**A.** It does not invent a policy — it makes the existing one (locals in named-function bodies are not nodes; zero tests assert otherwise; no spec clause pins locals as nodes — `docs/spec/code-intel-engine.md` mentions `variable` only in kind rosters) uniform across the scope constructs the TS/JS configs failed to model. C leaves a correctness bug; B is the right eventual destination and A's `FunctionScopeTypes` set is exactly the input B will need — a prerequisite, not a detour.

Suppression rule (exact): a `VariableTypes` match produces a node **only at `scopeDepth == 0`**; at depth > 0 no node and no `contains` edge, `skipChildren=false` so initializer harvesting continues. Independently: a match whose resolved name is not a single identifier (contains `{`, `[`, `,`, or whitespace) produces no node at any depth.

Kept / dropped resolution for edge cases: module-scope `export const helper = () => {}` **kept** (callable arrow consts stay `calls` targets); nested arrow consts **dropped** (already dropped inside named functions); `namespace N { const X }` **kept** (namespaces are not function scopes); Vue/Svelte `<script setup>` top-level state **kept** (standalone extractors run TS/JS over script content at depth 0); `for (const x of y)` bindings unaffected (statement field, not a declaration — confirm by grammar probe); destructuring **dropped** by the identifier guard everywhere.

Scope for v1: TypeScript, TSX (covers JSX), JavaScript — the languages with measured pain. `FunctionScopeTypes` values must be confirmed by live-grammar parse probes before committing (standing rule, `extractor.go:49-51`).

The one real cost: SQL-literal lineage coarsens from variable-owner to file-owner. Accepted for v1; measured by success criterion 7 in the spec. If pass-B anchor widening manufactures material false lineage (>20% growth in low-confidence string-match edges on a real repo), sequence B first or gate pass-B anchoring to a line-distance window — do not revert A.


## Open questions


- Pass-B anchoring: accept whole-file anchor scope (recommended, measure per spec), or pre-gate with a line-distance window when the owner is a `file:` node?
- Migration: `extractor_version` key in `project_metadata` with automatic one-run full re-index on mismatch (recommended — a fix users must manually trigger is not a fix), or doctor WARN + documented `rm` of the DB?
- Destructuring: drop entirely (recommended; residue after scope suppression is small), or emit one node per bound identifier (needs per-language pattern walkers)?
- Lua/Luau: same defect shape, one config line each — include now, or TS/JS-only until real-repo evidence exists (recommended)?

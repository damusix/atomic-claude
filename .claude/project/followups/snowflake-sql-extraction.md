---
id: snowflake-sql-extraction
title: Add Snowflake SQL dialect extraction
created: "2026-06-09"
origin: |
    code-intel SQL research 2026-06-09
kind: plan
review_by: "2026-08-08"
status: open
file: atomic/internal/codeintel/extraction/standalone/sql.go
---

Snowflake is currently 0% covered by the standalone SQL extractor. Add dialect-specific handling: VARIANT / OBJECT / ARRAY types, LATERAL FLATTEN (table-function lineage), QUALIFY, :: casts, $1 positional refs, stage / file-format objects, CREATE TASK / STREAM. The extractor is already multi-quote-style and dialect-tolerant, so this is additive regex, not a rewrite.

Strategic: part of the 'graph SQL source into a code graph' differentiation. Per the 2026-06-09 research, no general code-graph tool (Sourcegraph, stack-graphs, Glean, CodeQL, Sourcetrail) covers Snowflake source; the SQLGlot lineage family does but needs a pipeline/catalog. Object-level Snowflake graphing from raw .sql is unoccupied. Pairs with [[dbt-jinja-extraction]].

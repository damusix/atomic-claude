---
id: dbt-jinja-extraction
title: Add dbt (Jinja+SQL) extraction
created: "2026-06-09"
origin: |
    code-intel SQL research 2026-06-09
kind: plan
review_by: "2026-08-08"
status: open
file: atomic/internal/codeintel/extraction/standalone/sql.go
---

dbt models are .sql files with Jinja templating. The dbt dependency DAG lives in the macros, NOT in the SQL: extract {{ ref('model') }} and {{ source('a','b') }} first to produce model->model and model->source edges directly. That is the entire dbt graph, cheaper and more reliable than parsing SQL, because ref()/source() are explicit dependency declarations.

Then strip Jinja ({{ ... }}, {% ... %}) and run the existing standalone SQL extractor on the residual for table/column lineage. Also handle: {{ config(...) }}, {{ this }}, and macros under macros/*.sql. Highest-ROI of the SQL-extraction follow-ups. Pairs with [[snowflake-sql-extraction]] (dbt-on-Snowflake is a common stack) and [[embedded-sql-extraction]] (same Jinja-strip-then-extract shape).

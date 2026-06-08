---
id: embedded-sql-extraction
title: Extract SQL embedded in host-language string literals
created: "2026-06-08"
origin: |
    user question during SQL-support build (does it work in non-.sql code?)
kind: plan
review_by: "2026-08-07"
status: open
file: atomic/internal/codeintel/extraction/standalone/sql.go
---

Detect and extract SQL embedded in host-language string literals — Python `db.execute("SELECT ...")`, Go raw-string queries, Java/TS template literals, ORM `raw()`/`sql\`\``, migration-in-code, dbt-Jinja models. Currently SQL is routed purely by file extension (.sql/.ddl/.pgsql/.mysql → standalone SQL extractor); SQL inside non-.sql files is opaque to the host language tree-sitter extractor, so its tables/columns/relationships are invisible.

Approach sketch: in host-language extractors (or a post-pass), scan string/template literals for a SQL DDL/DML signature (CREATE/ALTER/SELECT/INSERT/...) and feed matching spans through the existing SQL standalone extractor, attributing nodes/edges to the embedding file with correct line offsets. The SQL extraction machinery (sql.go: 9 NodeKinds, references/writes/calls edges) is reusable as-is — the new work is the host-side literal detection + SQL-signature heuristic + offset mapping.

Hard parts (why it is a separate feature, not a tweak): distinguishing real SQL from arbitrary strings (false positives), multi-fragment/concatenated queries, parameterized placeholders ($1, ?, :name, {}), Jinja/templating noise, and per-language literal AST access. Scope it per host language, start with one (e.g. Python or Go), gate behind a confidence heuristic.

Verified gap 2026-06-07: a .py file with CREATE TABLE in a string + SELECT FROM indexed 2 nodes (Python var + fn), 0 SQL tables. Origin: user question during SQL-support build ("does it work in non-.sql code?"). Builds on docs/spec/sql-language-support.md.

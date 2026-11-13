---
id: elixir-erlang-embedded-literal-harvest
title: Elixir/Erlang embedded-literal harvest (SQL string-match + embedded SQL)
created: "2026-07-19"
origin: |
    sql-string-match autopilot run 2026-07-18 (PR #155)
kind: plan
review_by: "2026-09-17"
status: open
---

languages/elixir.go + erlang.go extract symbols, but embeddedLiteralConfigs (indexer/embedded_literals_config.go) omits both — no string-literal harvest, so .ex/.erl files get neither embedded-SQL extraction (Repo.query/fragment raw SQL) nor sql_string index-anchored matching. Fix: probe grammar string node kinds, add two config entries. Supersedes the stale elixir-language-support entry (Elixir extraction itself shipped).

---
id: phoenix-route-resolver
title: Wire Phoenix route resolver (after Elixir support)
created: "2026-06-07"
origin: |
    code-intel eval — rw-phoenix; user request
kind: plan
review_by: "2026-08-06"
status: open
file: atomic/internal/codeintel/resolution/frameworks/elixir.go
---

Phoenix route extraction (router.ex scope/get/post/resources DSL, *_path helpers, controller actions). Depends on elixir-language-support (needs Elixir AST first). A PhoenixResolver already exists (elixir.go) but is gated by LanguageUnknown — corpus rw-phoenix got 0 routes because Elixir doesn't parse. Once Elixir lands, wire/verify the Phoenix resolver's Extract against the rw-phoenix RealWorld app (gothinkster elixir-phoenix-realworld-example-app).

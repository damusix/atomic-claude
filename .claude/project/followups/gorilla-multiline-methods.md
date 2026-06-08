---
id: gorilla-multiline-methods
title: Gorilla multi-line .Methods() emits method ANY
created: "2026-06-07"
origin: |
    code-intel F-26; phase-3 triage
kind: finding
severity: risk
review_by: "2026-08-06"
status: open
file: atomic/internal/codeintel/resolution/frameworks/golang.go
---

extractGorillaMethods (resolution/frameworks/golang.go ~518) rejects .Methods() when a newline precedes it in the scan window, so the idiomatic multi-line gorilla form r.HandleFunc("/p", h).\n  .Methods("GET") silently emits method ANY instead of GET. Real bug, but no gorilla app in the eval corpus (rw-gin uses gin) so it's unverified on a real app. Fix: don't reject on newline (the 200-char window already bounds it) / strip continuation whitespace, then verify against a real gorilla/mux app. Fell outside the phase-3 ①/② fix buckets — deferred rather than dropped.

---
id: codeintel-go-grpc-impl
title: Go gRPC stub-impl synthesis (needs Go interface extraction)
created: "2026-06-07"
origin: |
    code-intel B-8; phase-3 triage
kind: plan
review_by: "2026-08-06"
status: open
---

go-grpc-stub-impl synthesizer is a stub: needs Go interface method_specs (interface_type method_spec, not just method_declaration), composite-literal arg capture (&fooImpl{}), and implements-edge synthesis for Go's structural typing. Substantial Go-extraction work. DEFERRED.

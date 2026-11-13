---
id: rails-dsl-symbol-synthesis
title: Synthesize symbols from Rails DSL macros (has_many/scope/...)
created: "2026-06-07"
origin: |
    code-intel F-77 investigation
kind: plan
review_by: "2026-08-06"
status: open
---

Rails (and similar) DSL macros — has_many/belongs_to/scope/validates/before_action — are call nodes, not symbol declarations, so a generic Ruby extractor correctly does not emit them as symbols (this is why rw-rails extracts ~2.6 nodes/file). A framework-aware resolver could SYNTHESIZE virtual symbols/edges from these macros (e.g. has_many :articles -> an association, scope :published -> a query method) to enrich Rails graphs. Future enhancement; framework-resolver territory, not generic extraction. Surfaced when investigating F-77 (Ruby extraction thinness — confirmed NOT an extractor bug).

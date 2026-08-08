---
id: validate-bare-skips-spec-rules
title: Bare atomic validate exits 0 while validate spec finds S5 FAILs
created: "2026-08-08"
origin: |
    serve-bus-chat CI failure
kind: finding
severity: nit
review_by: "2026-10-07"
status: open
---

Phase-4 verify ran bare 'atomic validate' (exit 0) but CI's 'atomic validate spec' failed S5 on docs/spec/serve-bus-chat.md — the bare form does not run the S-rules over docs/spec/**, so a local whole-repo validate is weaker than CI's per-surface invocations. Either bare validate should include spec rules, or its output should say which surfaces it skipped.

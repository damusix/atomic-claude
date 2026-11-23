---
paths:
  - "docs/spec/**/*.md"
  - "docs/design/**/*.md"
  - "**/.scratchpad/*/SPEC.md"
---

# Keep the implementation spec current


- **Disposable spec:** Keep it in the task scratchpad. Rewrite affected sections when a decision changes so they describe only what to build now. Use `atomic template spec` for structure. Do not keep a change log.
- **Durable design:** Keep approved decisions, rationale, and tradeoffs here. Link to the design where it already explains the behavior or flow.
- **Existing tracked spec:** Correct the body in place. Append a dated entry under `## Change log` with what changed, why, and what it supersedes if applicable. Preserve earlier entries. Do not reformat unrelated sections.

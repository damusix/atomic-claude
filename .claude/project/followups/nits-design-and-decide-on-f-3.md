---
id: nits-design-and-decide-on-f-3
title: Design and decide on `atomic validate` CLI subcommand
created: "2026-05-17"
origin: |
    chat session 2026-05-17 system improvement discussion, deferred to explore later.
severity: nit
review_by: "2026-07-16"
status: open
---

Design exists at `docs/design/atomic-validate.md`. Open questions in the design doc:


- Share code with `atomic doctor` for the bundle-parity check? (Yes — extract to `atomic/internal/manifestcheck/`.)
- Resolve third-party skill names installed in `~/.claude/skills/` but not bundled? (Probably no — focus on project's own artifacts.)
- Handle in-flight skills referenced by commands in the same PR? (Resolve against working tree, not `~/.claude/`.)
- `--suggest` flag that prints templates without editing files?
- Pre-commit hook integration via `atomic hooks install --pre-commit`?


Next step when revisiting: promote design → `docs/spec/atomic-validate.md`. Closely coupled with F-2 — both share `manifestcheck` substrate.

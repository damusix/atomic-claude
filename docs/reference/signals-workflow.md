# Signals workflow


The signals workflow keeps Claude aware of the current shape of a project without hallucination. On first use, run `/initialize-signals` to generate two committed files:

- `.claude/project/deterministic-signals.md` — machine-generated facts: directory tree, manifests, languages, lockfile presence. Produced by `atomic signals scan`.
- `.claude/project/inferred-signals.md` — inferred meaning: framework, build/test/lint commands, architectural style, conventions. Produced by `atomic-signals-inferrer`.

Both files are auto-referenced in the project's `CLAUDE.md` via `@`-refs so Claude loads them on every session. The `atomic-signals` skill keeps them fresh: it auto-fires on project-state-change phrases and also runs silently from `/commit-only` when the staged diff touches source files. The inferrer uses an incremental diff path — on subsequent runs it reads only what changed and updates only the dependent sections, leaving everything else byte-identical.

Requires the `atomic` binary. Run without it for a degraded tree-only fallback. Full spec: [`../spec/signals-workflow.md`](../spec/signals-workflow.md).

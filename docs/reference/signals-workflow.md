# Signals workflow


The signals workflow keeps Claude aware of the current shape of a project without hallucination. Run `/refresh-signals` to generate (or update) two files:

- `.claude/project/deterministic-signals.md` — machine-generated facts: directory tree, manifests, languages, lockfile presence. Produced by `atomic signals scan`.
- `.claude/project/signals.md` — inferred meaning: framework, build/test/lint commands, architectural style, conventions, domain index. Produced by `atomic-signals-inferrer`. On large repos, optional per-domain detail files live under `signals/`.

Both files are gitignored (project-specific, not committed) and auto-referenced in the project's `CLAUDE.md` (or `claude.local.md`) via `@`-refs so Claude loads them on every session. The `atomic-signals` skill keeps them fresh: it auto-fires on project-state-change phrases and also runs silently from `/commit-only` when the staged diff touches source files. The inferrer uses content-SHA change detection for incremental domain refresh — on subsequent runs it updates only affected domains, leaving everything else byte-identical.

Requires the `atomic` binary. Run without it for a degraded tree-only fallback. Full spec: [`../spec/signals-workflow.md`](../spec/signals-workflow.md).

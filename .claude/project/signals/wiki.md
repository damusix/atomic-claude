# wiki

## What it does

Cross-repository knowledge layer: `atomic wiki scan` scaffolds and classifies member repos; `atomic wiki stale` gives a read-only freshness verdict (mirrors `atomic signals stale` exit-code contract); `/refresh-wiki` drives the LLM pass — summarizing no-signals repos via `atomic-signals-inferrer` (wiki-output mode), synthesizing cross-cutting concern docs, and refreshing only stale artifacts. Fingerprints are written by code (`atomic wiki stamp`), never the model. A session-start staleness nudge and ship-time `.dirty` marker prevent silent rot.

## Artifacts

- `commands/refresh-wiki.md` — `/refresh-wiki [root]` command. Resolves wiki root (default `./wiki/` from cwd), runs `atomic wiki scan`, reads `atomic wiki stale` output, presents pending repos as a numbered list (axiom 4), dispatches `atomic-signals-inferrer` in wiki-output mode for unselected pending repos, re-synthesizes affected `concerns/*.md` and `index.md` narrative, invokes `atomic wiki stamp` for every artifact written, clears `.dirty`, offers to commit.

## CLI code

- `atomic/internal/wiki/wiki.go` — repo discovery, classification, scaffold creation (`wiki/index.md`, `wiki/README.md`, `wiki/repos/`, `wiki/concerns/`, `wiki/.gitignore`, `git init`), idempotent `<wiki-scan>` block writes, `<wikis>` registry writes to `~/.claude/CLAUDE.md`. Skip dirs: `node_modules`, `dist`, `build`, `target`, `vendor`, `.worktrees`, `tmp`, `.git`. Classification: `indexed` (has `.claude/project/signals.md`), `pending`, or `summarized` (summary file exists on re-scan).
- `atomic/internal/wiki/registry.go` — `<wikis>` block management in `~/.claude/CLAUDE.md`. Three insertion cases: block present (add line iff absent, dedup by normalized path), block absent (append after `</atomic>` or EOF), file absent (create). Never alters `<atomic>` block.
- `atomic/internal/wiki/stale.go` — `atomic wiki stale` implementation. Reads `<wiki-scan>` block, re-walks the root, computes membership drift (`DRIFT added`, `DRIFT removed`, `DRIFT status`). Exits `0` fresh / `1` stale / `2` error.
- `atomic/internal/wiki/staleness.go` — per-artifact content drift checks. For each `repos/<repo>(/<domain>).md`: `git rev-parse HEAD` vs `reflects_rev`. For each `concerns/<concern>.md`: each `reflects:` entry vs referenced repo's current fingerprint. Missing/unparseable `reflects_*` counts as stale (fail-safe).
- `atomic/internal/wiki/stamp.go` — `atomic wiki stamp` CLI helper. Computes `git rev-parse HEAD`, writes/updates `reflects_rev` in summary YAML frontmatter. For concerns, takes `--cites repoA,repoB` args, resolves each to a fingerprint (HEAD SHA for `summarized`; `.claude/project/signals.md` content hash for `indexed`), writes `reflects:` list. Unresolvable cited repo is skipped, not crashed on.
- `atomic/internal/wiki/action.go` — `atomic wiki mark-dirty` helper. Reads `<wikis>`, checks whether cwd is under any registered wiki root (normalized path-prefix, no git), `touch`es `.dirty` if so. No-op when cwd is under no registered root. Invoked by the `signals-gate` partial on every ship.
- `atomic/internal/wiki/wiki_test.go`, `registry_test.go`, `stale_test.go`, `staleness_test.go`, `stamp_test.go` — package tests.

## Docs

- `docs/spec/wiki.md` — implementation contract. Covers `atomic wiki scan` success criteria, `atomic signals scan --out` redirect, `atomic wiki stale` exit-code contract, forcing function (neglect nudge + drift marker), `/refresh-wiki` + inferrer wiki-output mode. Design at `docs/design/wiki.md`.
- `docs/design/wiki.md` — design rationale for the realm-above-repo knowledge layer.
- `docs/reference/wiki-workflow.md` — user-facing reference: two deterministic verbs + one command, setup walkthrough, what a wiki looks like on disk. Covers the realm concept (a folder containing repos + loose material), two-layer model (atomic drives repo layer; user drives knowledge layer), realm-root `CLAUDE.md` pattern, `wiki/knowledge/` directory for raw-material digests, and repo states (`indexed`, `summarized`, `pending`).
- `docs/reference/concepts.md` — contains `## Wikis` section (conceptual orientation).
- `docs/credits.md` — credits note for the wiki feature.

## Coupling

- **→ signals**: `atomic signals scan --out <dir>` redirect is a direct wiki dependency — the inferrer in wiki-output mode calls this to obtain the deterministic substrate without writing into the target repo. Changes to `atomic signals scan` output format propagate to wiki's no-signals summarization path.
- **→ signals**: `atomic-signals-inferrer` is dispatched in wiki-output mode by `/refresh-wiki`. Changes to the agent's interface or wiki-mode contract require updating `/refresh-wiki`.
- **→ config**: session-start wiki staleness check (`wiki.CheckStaleness`) reads `<wikis>` from `~/.claude/CLAUDE.md` — same file that `atomic claude install` writes to. Changes to the `<wikis>` block format require coordinated changes in both.
- **→ bundle**: `commands/refresh-wiki.md` ships in the bundle via the `commands/` bundlespec rule. Changes require `make render` + `make bundle`.
- **→ workflow**: the `signals-gate` partial (used by all ship verbs) invokes `atomic wiki mark-dirty` after signals refresh. Adding a ship verb without the signals-gate partial breaks the drift marker.

## Conventions worth knowing

- Wiki-output mode is additive to `atomic-signals-inferrer` — the agent's default (non-wiki) mode is unchanged; existing signals tests stay green.
- `atomic wiki stamp` and `atomic wiki mark-dirty` are internal helpers invoked by `/refresh-wiki` and the ship flow — not surfaced in `/atomic-help`.
- Fingerprints are always written by code, never the model. The model supplies which repos are cited in a concern; `atomic wiki stamp --cites` resolves and writes every fingerprint value.
- The `<wiki-scan>` block in `index.md` carries `root` and `generated` (date) on the open tag. Membership and status only — no fingerprints in the block.
- `atomic wiki scan` is idempotent: re-running regenerates only the `<wiki-scan>` block; a diff outside the block and under `repos/`/`concerns/` is empty.
- Session-start staleness nudge defaults to 30 days (reads from memory per axiom 2 — conversational nudge, not config).

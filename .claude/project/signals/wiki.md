# wiki

## What it does

Maintains a cross-repository knowledge base (the "wiki") for one realm of repos. Deterministic CLI verbs (`atomic wiki scan`, `atomic wiki stale`, `atomic wiki linkify`) scaffold, classify, register, and report freshness; `/refresh-wiki` drives the LLM pass — summarizing no-signals repos via `atomic-signals-inferrer` in wiki-output mode and re-synthesizing cross-cutting concerns incrementally. A cheap session-start nudge and ship-time `.dirty` marker prevent unnoticed drift.

## Artifacts

- [`commands/refresh-wiki.md`](../../../commands/refresh-wiki.md) — `/refresh-wiki [root]` command: scan → stale → incremental LLM pass → linkify → disposition report → `.dirty` clear → commit offer. Nine-step workflow; deterministic and judgment halves are fenced separately.
- [`agents/atomic-signals-inferrer.md`](../../../agents/atomic-signals-inferrer.md) — rendered agent; wiki-output mode branch is part of this agent. Caller passes `target_repo` + `wiki_dir`; inferrer writes `wiki_dir/repos/<repo>(/<domain>).md` and skips @-ref wiring. Missing either arg → fail loud.
- [`templates/commands/refresh-wiki.md`](../../../templates/commands/refresh-wiki.md) — source of truth for the refresh-wiki command; `make render` → [`commands/refresh-wiki.md`](../../../commands/refresh-wiki.md).
- [`templates/agents/atomic-signals-inferrer.md`](../../../templates/agents/atomic-signals-inferrer.md) — source of truth for the inferrer agent, including the wiki-output branch.
- [`templates/shared/signals-gate.md`](../../../templates/shared/signals-gate.md) — partial composed by all ship verbs; invokes `atomic wiki mark-dirty` after signals refresh.

## CLI code

- [`atomic/internal/wiki/wiki.go`](../../../atomic/internal/wiki/wiki.go) — core package: `Scan(root, Options)` entry point. Stages: collision check → parse prior entries → `discoverMembers` (recursive walk from root's children, stops at each `.git` boundary, skips `node_modules`/`dist`/`build`/`target`/`vendor`/`.worktrees`/`tmp`/`.git` and the wiki dir itself, root never a member) → `classifyMembers` → scaffold → `writeWikiScanBlock` → `writeMembersSection`. `Options.Clock` is injectable for deterministic test output.
- [`atomic/internal/wiki/wiki.go`](../../../atomic/internal/wiki/wiki.go) (`classifyMembers`) — four-rule precedence per member: (1) prior `summarized` + summary file exists on disk → keep `summarized`; (2) [`.claude/project/signals.md`](../signals.md) exists → `indexed` (signals win over a leftover summary); (3) `discoverSummary` finds `wiki/repos/<name>.md` or `wiki/repos/<name>/` with at least one `.md` → `summarized`; (4) otherwise → `pending`. Rule 3 (disk-discovery) was added 2026-06-11 to make `summarized` reachable on a closing re-scan: `/refresh-wiki` writes summaries after the initial scan, and the re-scan must discover them without a prior index entry.
- [`atomic/internal/wiki/wiki.go`](../../../atomic/internal/wiki/wiki.go) (`writeMembersSection`) — writes a managed `## Members` linked section into `wiki/index.md` between `<!-- wiki-members:start -->` / `<!-- wiki-members:end -->` markers, spliced idempotently. Link targets per status: `indexed` → `../<repo>/.claude/project/signals.md`; `summarized` → the recorded `SummaryPath` (`repos/<repo>.md` or `repos/<repo>/`); `pending` → `../<repo>/`.
- [`atomic/internal/wiki/wiki_test.go`](../../../atomic/internal/wiki/wiki_test.go) — 20+ tests covering happy path, status derivation, summarized preservation/downgrade/disk-discovery (single-file and domain-split), signals-win-over-summary, collision refusal (two cases), stable sort, `## Members` links per status, idempotent re-scan narrative preservation, git-init-skip on re-run.
- [`atomic/internal/wiki/`](../../../atomic/internal/wiki) (CPs 2–6, remaining files) — registry writer (`<wikis>` block in `~/.claude/CLAUDE.md`; dedup by `filepath.Abs`+`filepath.Clean`; never touches `<atomic>`); `atomic wiki stamp` (writes `reflects_rev` from `git rev-parse HEAD`; for concerns, `--cites repoA,repoB` → resolves each to HEAD SHA for `summarized` or `signals.md` content hash for `indexed`; unresolvable cited repo skipped not crashed); `atomic wiki stale` (re-walk + `<wiki-scan>` block diff for membership/status drift; fingerprint comparison for artifact content drift; literal-prefix report `DRIFT added/removed/status`, `STALE summary`, `STALE concern`; exit 0/1/2); `wiki.CheckStaleness` (injected runner+clock seam, reads `generated` age + `.dirty`, no git); `atomic wiki mark-dirty` (path-prefix check → touch `.dirty`).

## Docs

- [`docs/spec/wiki.md`](../../../docs/spec/wiki.md) — implementation contract: all CLI verb success criteria, `classifyMembers` classification rules, `<wiki-scan>` block format, `<wikis>` registry contract, fingerprint store, staleness, forcing function, 10-checkpoint table. Change log records the 2026-06-11 disk-discovery amendment (rule 3 added), 2026-06-06 link-navigability amendment (`## Members` + `atomic wiki linkify`), and a checkpoint-format correction.
- [`docs/design/wiki.md`](../../../docs/design/wiki.md) — design rationale: problem statement (cross-repo knowledge gap), concept definitions (wiki/root/repo/realm), code/model split flowchart, wiki directory layout, staleness model, forcing function (neglect+drift+heal), 10 approach decisions with rejected alternatives.
- [`docs/reference/wiki-workflow.md`](../../../docs/reference/wiki-workflow.md) — user-facing mechanism guide: realm mental model, two-layer split (atomic drives repo layer; user drives knowledge layer via `wiki/knowledge/`), setup, disk layout, repo states, registry, staleness, forcing function, relationship to signals.
- [`docs/reference/concepts.md`](../../../docs/reference/concepts.md) (`## Wikis` section) — conceptual overview: signals vs. wikis (one level up), three member states, nudge-based freshness model, pointer to wiki-workflow.

## Coupling

- **signals domain** — `atomic signals scan --out <dir>` redirect is a direct dependency of wiki-output mode; changes to that flag's behavior or output format propagate to the no-signals summarization path. The `signals-gate` partial (composed by all ship verbs) calls `atomic wiki mark-dirty` after signals refresh — adding a ship verb without signals-gate breaks the drift marker.
- **signals domain** — `atomic-signals-inferrer` is dispatched in wiki-output mode by `/refresh-wiki`; the agent's prompt contract (both `target_repo` + `wiki_dir` required, fail loud on partial args) is a cross-domain dependency.
- **config domain** — `wiki.CheckStaleness` reads `<wikis>` from `~/.claude/CLAUDE.md`; `atomic claude install` also writes to that file. Changes to the `<wikis>` block format require coordinated updates in both. `atomic-claude-merger` is instructed to preserve the `<wikis>` block verbatim on merge.
- **bundle domain** — [`commands/refresh-wiki.md`](../../../commands/refresh-wiki.md) and [`agents/atomic-signals-inferrer.md`](../../../agents/atomic-signals-inferrer.md) are rendered artifacts; touching their templates requires `make render` + `make bundle`. The wiki Go package is part of the embedded binary.
- **workflow domain** — `/refresh-wiki` invokes `atomic-commit` skill for the commit message offer; wiki commit is offered, never automatic (axiom 3).

## Conventions worth knowing

- `classifyMembers` rule 3 (disk-discovery) makes `summarized` reachable on first derivation. Before the 2026-06-11 amendment, `summarized` was unreachable without a prior index entry — nothing ever wrote the status initially. The fix: after `/refresh-wiki` writes summaries, the closing re-scan calls `discoverSummary` and picks them up.
- `discoverSummary` looks for `wiki/repos/<name>.md` (single-file form) or `wiki/repos/<name>/` containing at least one `.md` (domain-split form). `<name>` is `filepath.Base(rel)`. Domain-split summary path stored as `repos/<name>/` (trailing slash).
- Fingerprints are always written by code (`atomic wiki stamp`), never the model. The model supplies which repos are cited in a concern (`--cites`); code resolves and writes every fingerprint value.
- `<wiki-scan>` block carries `root` and `generated` (date from injected clock) on the open tag. Membership and status only — no fingerprints in the block. `generated` doubles as the neglect baseline read by `wiki.CheckStaleness`.
- `atomic wiki stamp` and `atomic wiki mark-dirty` are internal helpers, not surfaced in `/atomic-help`. `atomic wiki linkify` is user-runnable and is surfaced in `/atomic-help`.
- Session-start staleness nudge threshold defaults to 30 days (memory, axiom 2 — conversational nudge, not config).
- `atomic wiki scan` is idempotent: re-running regenerates only the `<wiki-scan>` block and `## Members` section; content outside those managed zones and all files under `repos/`/`concerns/` are untouched.
- Root collision: if `wiki/` exists but `index.md` is absent or lacks a `<wiki-scan>` marker, `scan` refuses with a non-zero exit and names the path in the error.

# atomic doctor (design)


## Problem


The atomic-claude system spans four artifact types + a Go CLI bundle that cross-reference each other. Drift is invisible: a renamed agent, a stale signals file, a missing `@-ref` in `CLAUDE.md`, a hook that was never installed — none of these throw errors, they silently break the workflow. A recent system audit found 5 such gaps without any code-level signal. We need a deterministic, fast `atomic doctor` that surfaces these gaps before they bite.


Distinct from `atomic validate` (see `docs/design/atomic-validate.md`). `validate` lints artifact **content** for correctness. `doctor` checks **environment and install state** — what's wired, what's installed, what's fresh.


## Goals


- Single command to verify install + project state coherence.
- Deterministic. No LLM judgment. Pure Go.
- Non-zero exit on failure for CI use.
- Output: indexed checklist with PASS / WARN / FAIL per check.
- Repairs are opt-in (`--fix` flag). Default is report-only (axiom 3: destructive ops explicit confirm).


## Non-goals


- Linting artifact content (that's `atomic validate`).
- Running tests or invoking Claude.
- Network calls beyond `atomic update --check`.
- Multi-project rollup (single repo per invocation).


## Surface


```
atomic doctor [--fix] [--json] [--only <category>] [--skip <category>]
```


- `--fix` — prompt per item before applying repairs. Axiom 3 applies.
- `--json` — machine-readable output for CI gates.
- `--only` / `--skip` — narrow the check set by category name.


## Check categories


| # | Category | What it checks | Severity if fail |
|---|----------|----------------|------------------|
| 1 | Install integrity | `~/.claude/agents/`, `commands/`, `skills/`, `output-styles/`, `rules/` exist and match bundle manifest. SHA256 per file. | WARN drift / FAIL missing |
| 2 | Hooks installed | `~/.claude/settings.json` has the session-start hook from `atomic hooks session-start`. | WARN |
| 3 | Signals freshness | `.claude/project/deterministic-signals.md` exists and `atomic signals stale` exits 0. | WARN |
| 4 | Signals `@-refs` wired | Refs present in `claude.local.md` / `CLAUDE.local.md` / `CLAUDE.md` / `CLAUDE.md`. | FAIL |
| 5 | Bundle manifest parity | Committed `manifest.go` matches what `go generate` would produce (repo-dev only — skip if not in atomic-claude repo). | FAIL |
| 6 | Followups ledger schema | If `.claude/project/followups.md` exists, every entry has F-id, origin, severity. | WARN |
| 7 | Auto-memory orphans | `~/.claude/projects/<project>/memory/MEMORY.md` lines reference files that exist. | WARN |
| 8 | Binary self-check | `atomic update --check` succeeds (does not auto-update). | WARN |


Categories are stable index numbers so users can reference them in repair docs. Adding a category appends; never renumber.


## Output format


```
atomic doctor — integrity check  (project: claude-code-setup)

[1] PASS  install integrity                  36/36 files match bundle
[2] WARN  hooks installed                    session-start hook missing
[3] PASS  signals freshness                  last scan 2026-05-17
[4] FAIL  signals @-refs wired               not present in CLAUDE.md, claude.local.md, CLAUDE.md, or CLAUDE.local.md
[5] PASS  bundle manifest parity             generated == committed
[6] PASS  followups ledger schema            no .claude/project/followups.md (clean)
[7] PASS  auto-memory orphans                MEMORY.md refs resolve
[8] PASS  binary self-check                  v0.4.2 (latest)

3 PASS, 1 WARN, 1 FAIL. exit 1.

To repair: atomic doctor --fix
```


## Repair mode (`--fix`)


Prompt per item (axiom 3: destructive ops explicit confirm). Each repair is idempotent:


| Check | Repair |
|-------|--------|
| Install integrity drift | Re-run `atomic claude install --merge` |
| Hooks missing | `atomic hooks install` |
| Signals stale | **Cannot auto-fix** — CLI cannot dispatch the `atomic-signals-inferrer` agent. Print: `run /refresh-signals from Claude Code to refresh.` |
| `@-refs` missing | Append block to `CLAUDE.md` (or `claude.local.md` if user picks) |
| Bundle manifest stale | `go generate ./...` — fail loud if not in atomic repo |
| Followups schema | Print malformed entries with line numbers; refuse to auto-edit (content-level — human authorship) |
| Auto-memory orphans | Print orphan refs; refuse to auto-delete (user authored those) |
| Binary outdated | Print: `atomic update` to update |


The skill-required repairs are the boundary case: `doctor` prints the exact command and exits, since the CLI cannot invoke Claude Code skills.


## CI integration


```
atomic doctor --json | jq '.results[] | select(.severity == "FAIL")'
```


Exit codes:


- `0` — all PASS or only WARN.
- `1` — one or more FAIL.
- `2` — doctor itself errored (couldn't read state, missing dependency).


## Tradeoffs


| Option | Pros | Cons |
|--------|------|------|
| Doctor as Go CLI (chosen) | Deterministic, fast, scriptable, runnable in CI | Cannot dispatch Claude skills for some repairs |
| Doctor as Claude skill | Could invoke other skills | Slow, requires running Claude, not CI-friendly |
| Both (CLI + skill wrapper) | Best of both | Two surfaces to maintain, drift risk |


Chose CLI: most checks are deterministic file/SHA inspection. The few skill-required repairs degrade to printed instructions, which is acceptable for `--fix`.


## Open questions


- Should `atomic doctor` run automatically after `atomic update` / `atomic claude install`? Likely yes — fold post-install verification into the install path so drift is caught at the moment it's introduced.
- Should there be a git pre-commit hook variant? Probably no — would block commits on slow checks. Better as a manual or CI-time invocation.
- Severity thresholds: is a stale signals file really only WARN? Could argue FAIL when the staleness exceeds N days. Defer to memory-configured threshold.
- What's the failure mode if `~/.claude/` doesn't exist at all? Probably exit 0 with one note: "atomic-claude not installed; run `atomic claude install`."
- How to detect "repo-dev only" cleanly for the bundle manifest check? Heuristic: presence of `atomic/internal/bundlemirror/`. Documented as such.


## Cross-references


- Spec template: TBD on promotion to `docs/spec/atomic-doctor.md`.
- Implementation home: `atomic/internal/doctor/` (new package).
- Composes with: `atomic validate` (shares the bundle-parity check — extract to `atomic/internal/manifestcheck/`).

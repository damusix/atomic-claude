# Artifact consolidation

## Goal

Reduce the Claude-facing artifact surface from 50 to 35 artifacts (−30%) without
losing capability. Eliminate sibling divergence in the ship-verb family, collapse
redundant agents into a single mode-select agent, and move cold/once-per-install
ops out of standing artifacts into binary-emitted prompts executed inside disposable
subagents. Every surviving artifact must earn its standing slot.

## Non-goals

- No change to skills (8) or the output style.
- No behavior change to the surviving lifecycle: plan → implement → ship → docs.
- Not merging `/undo-commit` into `/commit` (opposite intent).
- Not merging `report-issue` variants (different targets: user repo vs atomic itself).
- Not touching deterministic binary ops already split correctly (`atomic signals scan`, etc.).
- `worktree-setup` partial remains a prompt partial; no promotion to a CLI verb.

## Success criteria

- [ ] `agents/` contains exactly 5 `atomic-*.md` files: `atomic-implementer`, `atomic-reviewer`, `atomic-investigator`, `atomic-strategist`, `atomic-wiki-inferrer`.
- [ ] `agents/atomic-builder.md`, `atomic-surgeon.md`, `atomic-git-scout.md`, `atomic-claude-merger.md`, `atomic-haiku.md` are absent.
- [ ] `commands/` contains exactly 22 `.md` files; the 9 old ship-verb files are absent.
- [ ] `commands/commit.md` exists; body implements ask-don't-enumerate with both flags and an interactive prompt (an `AskUserQuestion`/interactive-prompt reference and a flags/args section are both present).
- [ ] `atomic-implementer` accepts a `mode: surgical | feature` flag from the orchestrator; surgical mode is a block tagged `<surgical_mode>` that contains the verbatim hard-refuse-at-3-files rail currently in `agents/atomic-surgeon.md`.
- [ ] `atomic prompt git-cleanup` and `atomic prompt claude-merge` print their embedded brief texts; `go test ./...` covers both.
- [ ] `/git-cleanup` body dispatches a generic subagent that runs `atomic prompt git-cleanup`; `/git-cleanup` itself contains no cleanup logic.
- [ ] The install merge message in `atomic/internal/claudeinstall/install.go` no longer contains `/atomic-claude-merge` and instead references `atomic prompt claude-merge`.
- [ ] `/watch-ci` dispatches a generic subagent with `model: haiku` and `run_in_background: true` and an inline brief; no agent named `atomic-haiku` is dispatched.
- [ ] `templates/shared/worktree-setup.md` exists; composed into `/subagent-implementation` and `/autopilot`; asks if worktree is unspecified.
- [ ] `make render && git diff --exit-code` exits 0 (rendered outputs match templates).
- [ ] `make -C atomic bundle && git diff --exit-code` exits 0 (embedded bundle matches source).
- [ ] `go test ./...` passes in `atomic/`.
- [ ] `templates/commands/atomic-help.md` topic rows and tour stages match the surviving command/agent surface; verification loop returns zero `MISSING:` lines: `for cmd in commands/*.md; do verb=$(basename "$cmd" .md); [ "$verb" = "atomic-help" ] && continue; grep -q "/$verb" templates/commands/atomic-help.md || echo "MISSING: /$verb"; done`.
- [ ] CLAUDE.md "Subagents available for dispatch" lists exactly the 5 surviving agents.
- [ ] `docs/reference/agents.md` and `docs/reference/commands.md` tables reflect the post-consolidation surface.
- [ ] `atomic validate artifacts` exits 0 (no dangling `atomic prompt` citations for unregistered verbs).

## Approaches

Two tiers (banked design decision):

- **HOT = Claude config** (output style, skills, commands, agents) — frequent ops, listed every session.
- **COLD = binary** (`atomic prompt <name>`) — rare ops, emitted on demand, executed only inside a throwaway subagent.

Decisions locked (do not reopen):

| Concern | Decision | Rationale |
|---------|----------|-----------|
| Ship family (11 verbs) | Single `/commit`, ask-don't-enumerate (flags + interactive prompt) | One flow eliminates sibling divergence; commit is the common path |
| builder + surgeon | `atomic-implementer` with `mode: surgical \| feature`; surgical keeps hard-refuse rail inside `<surgical_mode>` block | −1 agent; ~80% of body was shared |
| haiku agent | Dispatch flag (`model: haiku`, `run_in_background: true`) on `/watch-ci` with inline brief | Name encoded a model, not a purpose; only 1 consumer |
| claude-merge trigger | Docs detail; install no-block path points at `atomic prompt claude-merge` | `ActionMergeRequired` only fires on first install into an unblocked file; standing slot is waste |
| git-cleanup trigger | `/git-cleanup` keeps its slot; body thins to subagent dispatch | Recurring maintenance op earns a slot |
| Cold-op execution | Main agent dispatches a generic subagent that runs `atomic prompt <name>`; main never sees the brief | Brief + verbose work quarantined in subagent; main context = one-line dispatch + summary |
| worktree-setup | `templates/shared/worktree-setup.md` partial composed into subagent-implementation + autopilot | Recurring concern earned a partial, not a command |

Cold-op dispatch flow: trigger → main agent emits one-line dispatch → generic subagent runs `atomic prompt <name>` → subagent does verbose work → returns short summary → main relays to user.

`bundlespec.MatchesAgent` is a prefix pattern (`atomic-`), not an allowlist — agent file deletions and `atomic-implementer` addition auto-propagate through the bundle without editing `bundlespec.go`. `MatchesCommand` is any `.md` — same auto-propagation for command deletions.

## Recommendation

Implement in 8 sequential checkpoints, each ending with a green build (render + bundle + `go test ./...`). Checkpoint 1 renames the agents that implement later checkpoints — note the bootstrapping dependency in the Agent column. Commit type: `fix!:` (removing user-facing verbs is a breaking change; must land in changelog and bump semver).

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|-----------|---------------|-------|-----------|---------|
| 1 | `atomic-implementer` — merge builder + surgeon into one mode-select agent; delete the two source templates + rendered outputs; update `agent-implementer-workflow` partial and all agent references | `templates/agents/atomic-builder.md`, `templates/agents/atomic-surgeon.md`, `templates/agents/atomic-implementer.md` (new), `templates/shared/agent-implementer-workflow.md`, `agents/atomic-builder.md` (delete), `agents/atomic-surgeon.md` (delete), `agents/atomic-implementer.md` (new), `CLAUDE.md`, `templates/commands/atomic-help.md`, `docs/reference/agents.md`, `README.md`; run `make render && make -C atomic bundle` | `atomic-builder` (bootstrapping note: builder/surgeon still exist at start of this CP; this CP is self-referential — use `atomic-builder` for the multi-file edit, which produces its own replacement) | 12–15 | `agents/` contains no `atomic-builder.md` or `atomic-surgeon.md`; `agents/atomic-implementer.md` exists; `grep '<surgical_mode>' agents/atomic-implementer.md` returns a match and the hard-refuse-at-3-files rule text is present inside it; `make render && git diff --exit-code`; `make -C atomic bundle && git diff --exit-code`; `/atomic-help` loop grep returns 0 MISSING lines: `for cmd in commands/*.md; do verb=$(basename "$cmd" .md); [ "$verb" = "atomic-help" ] && continue; grep -q "/$verb" templates/commands/atomic-help.md \|\| echo "MISSING: /$verb"; done` |
| 2 | `/commit` ship verb — author ask-don't-enumerate command replacing the 9 ship verbs; delete the 9 templates + rendered outputs; update signals-gate partial usage, `/atomic-help` ship matrix, CLAUDE.md, docs | `templates/commands/commit.md` (new), `templates/commands/commit-and-push.md` … `push-only.md` (9 deletes), `commands/commit.md` (new), `commands/commit-and-push.md` … `push-only.md` (9 deletes), `templates/commands/atomic-help.md`, `CLAUDE.md`, `docs/reference/commands.md`, `README.md`; run `make render && make -C atomic bundle` | `atomic-implementer (mode: feature)` | 20–25 | `commands/` contains no `commit-and-push.md`, `commit-and-pr.md`, `commit-and-merge.md`, `commit-and-squash.md`, `squash-only.md`, `squash-and-merge.md`, `merge-to-main.md`, `pr-only.md`, `push-only.md`; `commands/commit.md` exists; `grep -i 'AskUserQuestion\|interactive.*prompt\|prompt.*interactive' commands/commit.md` returns a match (interactive prompt present) and `grep -i 'flags\|args\|--' commands/commit.md` returns a match (flags section present); orphan rule: each of the 9 deleted templates has a deleted output; `make render && git diff --exit-code`; `make -C atomic bundle && git diff --exit-code` |
| 3 | `atomic prompt` CLI verb — register `prompt` in `main.go` switch and `cliusage.go`; add dedicated embed for prompt texts (`git-cleanup`, `claude-merge`); `atomic prompt <name>` prints brief; Go tests | `atomic/cmd/atomic/main.go`, `atomic/internal/cliusage/cliusage.go`, `atomic/internal/prompt/` (new package or embed), test files; run `make -C atomic bundle` | `atomic-implementer (mode: feature)` | 6–10 | `go test ./...` passes; `atomic prompt git-cleanup` exits 0 and prints non-empty text; `atomic prompt claude-merge` exits 0 and prints non-empty text; `atomic prompt unknown-name` exits non-zero; `atomic validate artifacts` exits 0 |
| 4 | git-cleanup rewire — thin `/git-cleanup` to subagent dispatch calling `atomic prompt git-cleanup`; delete `atomic-git-scout` template + output; update CLAUDE.md, `/atomic-help` | `templates/commands/git-cleanup.md`, `commands/git-cleanup.md`, `templates/agents/atomic-git-scout.md` (delete), `agents/atomic-git-scout.md` (delete), `templates/commands/atomic-help.md`, `CLAUDE.md`, `docs/reference/agents.md`, `README.md`; run `make render && make -C atomic bundle` | `atomic-implementer (mode: surgical)` | 6–9 | `agents/atomic-git-scout.md` absent; `commands/git-cleanup.md` body contains no branch/worktree scan logic; `grep -r 'atomic-git-scout' commands/ agents/ CLAUDE.md` returns 0 matches; `make render && git diff --exit-code`; `make -C atomic bundle && git diff --exit-code` |
| 5 | claude-merge rewire — delete `/atomic-claude-merge` template + output and `atomic-claude-merger` agent template + output; author `claude-merge` prompt text (CP3 may already have this); rewire install merge message to reference `atomic prompt claude-merge`; update install guide docs; Go test for install message | `templates/commands/atomic-claude-merge.md` (delete), `commands/atomic-claude-merge.md` (delete), `templates/agents/atomic-claude-merger.md` (delete), `agents/atomic-claude-merger.md` (delete), `atomic/internal/claudeinstall/install.go`, `atomic/internal/claudeinstall/install_test.go`, `docs/guides/install.md`, `templates/commands/atomic-help.md`, `CLAUDE.md`; run `make render && make -C atomic bundle` | `atomic-implementer (mode: surgical)` | 8–11 | `agents/atomic-claude-merger.md` absent; `commands/atomic-claude-merge.md` absent; `grep 'atomic-claude-merge' atomic/internal/claudeinstall/install.go` returns 0 matches; `grep 'atomic prompt claude-merge' atomic/internal/claudeinstall/install.go` returns a match; `go test ./...` passes (install message test covers `atomic prompt claude-merge`); `make render && git diff --exit-code`; `make -C atomic bundle && git diff --exit-code` |
| 6 | haiku dissolve — `/watch-ci` dispatches generic subagent with `model: haiku`, `run_in_background: true`, inline brief; delete `atomic-haiku` template + output; update CLAUDE.md, `/atomic-help` | `templates/commands/watch-ci.md`, `commands/watch-ci.md`, `templates/agents/atomic-haiku.md` (delete), `agents/atomic-haiku.md` (delete), `templates/commands/atomic-help.md`, `CLAUDE.md`, `docs/reference/agents.md`, `README.md`; run `make render && make -C atomic bundle` | `atomic-implementer (mode: surgical)` | 5–7 | `agents/atomic-haiku.md` absent; `grep 'atomic-haiku' commands/watch-ci.md` returns 0 matches; `grep 'model: haiku' commands/watch-ci.md` returns a match; `grep 'run_in_background' commands/watch-ci.md` returns a match; `make render && git diff --exit-code`; `make -C atomic bundle && git diff --exit-code` |
| 7 | `worktree-setup` partial — extract worktree-start logic into `templates/shared/worktree-setup.md`; compose into `/subagent-implementation` and `/autopilot` with ask-if-unspecified; delete `worktree-start` template + output; update CLAUDE.md, `/atomic-help`, `docs/reference/workflow.md` | `templates/shared/worktree-setup.md` (new), `templates/commands/subagent-implementation.md`, `templates/commands/autopilot.md`, `templates/commands/worktree-start.md` (delete), `commands/worktree-start.md` (delete), `commands/subagent-implementation.md`, `commands/autopilot.md`, `templates/commands/atomic-help.md`, `CLAUDE.md`, `docs/reference/workflow.md`, `README.md`; run `make render && make -C atomic bundle` | `atomic-implementer (mode: feature)` | 10–13 | `commands/worktree-start.md` absent; `templates/shared/worktree-setup.md` exists; `grep -r 'worktree-start' commands/ agents/ CLAUDE.md templates/commands/atomic-help.md` returns 0 matches; `make render && git diff --exit-code`; `make -C atomic bundle && git diff --exit-code` |
| 8 | Cross-artifact sweep + CI — final wiring pass: verify CLAUDE.md registry, `/atomic-help` topic table + all four tour stages, README tables, `docs/reference/agents.md` + `docs/reference/commands.md`, `docs/reference/workflow.md`; run signals refresh; full drift gates + test suite | `CLAUDE.md`, `templates/commands/atomic-help.md`, `commands/atomic-help.md`, `README.md`, `docs/reference/agents.md`, `docs/reference/commands.md`, `docs/reference/workflow.md`; `make render && make -C atomic bundle`; `/refresh-wiki` | `atomic-implementer (mode: surgical)` | 5–8 | `/atomic-help` loop grep returns 0 MISSING lines: `for cmd in commands/*.md; do verb=$(basename "$cmd" .md); [ "$verb" = "atomic-help" ] && continue; grep -q "/$verb" templates/commands/atomic-help.md \|\| echo "MISSING: /$verb"; done`; `grep -rn 'atomic-builder\|atomic-surgeon\|atomic-git-scout\|atomic-claude-merger\|atomic-haiku' commands/ agents/ CLAUDE.md templates/` returns 0 matches; `agents/` has exactly 5 `atomic-*.md` files; `make render && git diff --exit-code`; `make -C atomic bundle && git diff --exit-code`; `go test ./...` passes; `atomic validate artifacts` exits 0 |

Build-pipeline constraint: every checkpoint that touches a source artifact (`agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, `CLAUDE.md`) must run `make render` (when templates change) before `make -C atomic bundle`. Order is load-bearing — the pre-commit hook enforces it; so does CI.

Orphan rule: deleting a template without deleting its rendered output (or vice versa) halts `make render` with a non-zero exit. Each CP that removes templates must remove the matching `commands/` or `agents/` output in the same commit.

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| `/atomic-help` drift — a removed verb or added `atomic-implementer` not reflected in topic rows or tour stages | High (11 commands removed, 2 agents removed, 1 added) | CP-level Verifies include the help-router loop grep; CP8 runs a full sweep before merge |
| Orphan-rule halt — template deleted without deleting rendered output breaks `make render` for the whole pipeline | Medium | Each CP's Verifies mandate `make render && git diff --exit-code`; orphan error names both remediation paths |
| `bundlespec.MatchesAgent` — confirmed prefix pattern, not allowlist; deletions and `atomic-implementer` auto-propagate | Low (resolved) | Verified in `atomic/internal/bundlespec/bundlespec.go:11`; no edit needed |
| Surgical mode regression — hard-refuse-at-3-files rail lost or structurally undetectable during builder+surgeon merge | Medium | CP1 Verifies: `grep '<surgical_mode>' agents/atomic-implementer.md` returns a match AND the hard-refuse rule text is present inside it |
| `install.go` rewire — install merge message still references `/atomic-claude-merge` after CP5 | Medium | CP5 Verifies: `grep 'atomic-claude-merge' atomic/internal/claudeinstall/install.go` returns 0 matches; `grep 'atomic prompt claude-merge' atomic/internal/claudeinstall/install.go` returns a match; Go test covers the install message path |
| `atomic prompt` unknown-name — new verb needs a non-zero exit for unknown prompt names; omitting this makes bad citations silent | Medium | CP3 test explicitly covers the unknown-name error path |
| CLAUDE.md registry staleness — "Subagents available for dispatch" section references removed agents across sessions | High | CP8 sweep Verifies grep for all five removed artifact names across CLAUDE.md and templates |
| Commit type — removing 9 user-facing ship verbs and 4 agents is a breaking change; `refactor:` would be invisible in changelog | High | Commit type must be `fix!:` per release-please rules; note this in the commit for CP2 (the largest removal) |
| `/refresh-wiki` stale after bulk artifact removal | Low | CP8 explicitly runs `/refresh-wiki`; `atomic doctor` check 9 (signals freshness) catches drift |
| Artifact wiring drift — CLAUDE.md registry, README tables, `docs/reference/agents.md` / `docs/reference/commands.md`, and signals not updated per artifact-touching checkpoint | Medium | Each artifact-touching checkpoint's Verifies includes the relevant table update check; CP8 sweep is the backstop |

## Change log

(none — spec body was followed as written; refinements during build are in the Implementation log)

## Implementation log

### shipped — 2026-06-13

Built across 8 checkpoints via `/subagent-implementation` in worktree `artifact-consolidation` (branched from `22a5810`). Commits (chronological):

- `d444f10` — CP1 atomic-implementer replaces builder + surgeon (mode-select; surgical hard-refuse rail preserved)
- `ba92ab3` — CP2 /commit (ask-don't-enumerate) replaces the 9 ship verbs
- `32ce663` — CP3 `atomic prompt` verb + `internal/coldprompt` briefs (git-cleanup, claude-merge)
- `4e288f4` — CP4 /git-cleanup dispatches `atomic prompt git-cleanup`; atomic-git-scout removed
- `8c8a663` — CP5 claude-merge cold-op; /atomic-claude-merge command + atomic-claude-merger agent removed; install message rewired
- `9b85154` — CP6 atomic-haiku dissolved into per-call `model: haiku` across 4 consumers
- `2f17027` — CP7 worktree-setup partial replaces /worktree-start command
- `6b524f1` — CP8 docs consistency sweep + spec-header lint fix
- `2f99e92` — signals refresh to the 5-agent / 22-command surface

Final surface: agents 9→5, commands 33→22, Claude-facing artifacts 50→35. New: `atomic prompt {git-cleanup, claude-merge}` binary verb, `worktree-setup` partial.

**Out-of-scope work performed during this build:**
- CP6 discovered `atomic-haiku` had 4 consumers (the design assumed 1); dissolution still applied cleanly (the agent body was generic; each consumer carries its own brief). Corrected `watch-ci`'s stale claim that a per-call `model:` is "silently ignored" — the Agent tool contract states per-call model takes precedence.
- CP8 fixed a pre-existing `atomic validate spec` S5 lint on this spec's table header (`Files / areas` → `Files/areas`).

**Unforeseens — surprises that emerged during implementation:**
- `make bundle` does not prune: deleting a source artifact leaves a stale tracked copy under `atomic/internal/embedded/bundle/`. Every deletion CP `git rm`'d the embedded copy explicitly.
- `internal/claudeinstall` test fixtures hardcode artifact filenames; removing builder broke 3 install tests (repointed to surviving artifacts), and the install merge-message rewire needed a new `Report()` assertion.
- Destructive cold-ops can't be fully executed by a generic subagent — a subagent can't interactively confirm per-item (axiom 3). Resolved: cold-op briefs are read-only/staging + return a proposal; the main agent confirms and executes (git-cleanup scan→confirm→execute; claude-merge stage→accept→cp).
- `claude.local.md` is git-tracked despite docs labeling it "gitignored / not checked in" (pre-existing discrepancy; hygiene edits committed in CP8).

**Deferred items (FOLLOWUPS triage):**
- F-1 (🔵): rendered `commands/commit.md` duplicates the squash-flow block (~220 lines) — flat-render artifact of a partial used in two branches; template composition is correct, squash→merge chain verified sound. Optional future cleanup (factor a `squash-core` partial).
- F-2 (🟡): RESOLVED by `2f99e92` (signals refresh).
- F-3 (🟡): historical `docs/spec/**` + `docs/design/**` still reference removed artifacts/verbs — left as point-in-time records (not bundled, not read by the loop). Revisit only if one becomes load-bearing.

**Known pre-existing failures (not introduced here):** `internal/hooks` 3 tests (`hooks-tests-read-real-home`) fail on this machine because they read the real `$HOME` `<wikis>` block. Orthogonal; filed.

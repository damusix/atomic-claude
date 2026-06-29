# Spec: signals refresh timing

Child of [`signals-workflow.md`](./signals-workflow.md). Contracts *when* the project-signals
refresh fires and *how* the inferrer is scoped. Supersedes the parent's "refresh happens at
ship time" model for the implement-loop path.

**Approach:** see [`docs/design/signals-refresh-timing.md`](../design/signals-refresh-timing.md).
Summary: the implementation phase owns the refresh (scoped to its SHA range); the commit-time
gate is a fallback that fires only for ad-hoc real-code commits. `atomic signals stale` is the
coordinator — no marker file.

## Contract

### C1 — Inferrer accepts `changed_range` (agent: `atomic-signals-inferrer`)

Add to the agent's **Caller-provided context** list:

> - **`changed_range: <from-sha>..<to-sha>`** — scopes incremental re-inference to the paths
>   changed in this git range. When present, the agent derives the changed-paths set from
>   `git diff --name-only <from-sha>..<to-sha>` unioned with uncommitted changes
>   (`git diff --name-only <from-sha>`), instead of the `deterministic-signals.prev.md` vs
>   `deterministic-signals.md` diff. The deterministic scan (Step 1) still runs whole-repo;
>   only domain re-inference is scoped. Absent → unchanged behavior (prev/current snapshot
>   diff drives incremental mode).

Wire it into the **Incremental vs full mode → Incremental** section: step 1's changed-paths
source becomes "the `changed_range` git diff when the caller supplied one, else the prev/current
`deterministic-signals.md` diff." No other incremental logic changes. `changed_range` is
ignored in wiki-output and bucket-synthesis modes (those have their own pipelines).

### C2 — Commit-time gate skips docs-only commits (partial: `signals-gate`)

Insert a new first step before the `atomic signals stale` check:

> 0. **docs-only guard.** Inspect the staged file set with `git diff --cached --name-only`. If
>    the staged set is **empty** (e.g., in a post-merge or post-squash context where the commit
>    already landed and nothing remains staged), skip the docs-only check and fall through to
>    the staleness check — an empty staged set does not mean all paths are documentation. If the
>    staged set is non-empty and **every** staged path is documentation, skip the refresh
>    entirely (do not run `atomic signals stale`). A path is documentation when it is under a
>    `docs/` directory at any depth, OR is a top-level `README*` / `CHANGELOG*` /
>    `CONTRIBUTING*` / `CODE_OF_CONDUCT*` / `SECURITY*` / `LICENSE*`. Any other path — source,
>    config, build files, `CLAUDE.md`, or any bundled-artifact `.md` under `agents/` `commands/`
>    `skills/` `rules/` `output-styles/` — means it is NOT docs-only; continue to the staleness
>    check. **Why:** the deterministic substrate counts per-language LOC, so a docs-only commit
>    trips `stale` exit 1 and dispatches the inferrer for no real map change. In a config repo
>    the artifact `.md` files are the product, so they must count as source, not docs.

The existing exit-code handling (0 fresh → skip; 1 stale → refresh; 2 error → report + skip)
is unchanged and follows the docs-only guard. Add a one-line WHY noting that the staleness
check is also what prevents a redundant refresh when the implementation phase already
refreshed: a fresh stored signals file returns exit 0.

### C3 — Implementation loop refreshes at finalize (command: `/subagent-implementation`)

- **Record the loop base SHA.** In Phase 1, capture `git rev-parse HEAD` once before iteration
  1 and store it in `STATE.md` as `Loop base SHA: <sha>`. This is the `from-sha` for the range.
- **Phase 3 finalize — new step, after `/documentation`, before deleting `$SCRATCH`:** refresh
  signals scoped to the loop's range.
  1. `command -v atomic` absent → skip.
  2. `atomic signals stale` exit 0 → skip (nothing material changed). Exit 2 → report + skip.
  3. Exit 1 → dispatch `atomic-signals-inferrer` with `mode: silent`, `first_run: false`, and
     `changed_range: <loop-base>..HEAD` (HEAD after docs commits). Run `atomic wiki mark-dirty`
     best-effort. Stage `.claude/project/deterministic-signals.md` + `.claude/project/signals.md`
     (and any `.claude/project/signals/**`).
  4. Commit as a dedicated `chore(signals): refresh after <topic>` commit. Record the SHA in
     `STATE.md`.

This refresh is **not** per-iteration — it runs once at finalize over the whole task range.

### C4 — Autopilot refreshes after the loop, before ship (command: `/autopilot`)

Autopilot does not run `/subagent-implementation`'s Phase 3 finalize, so it needs the same step
explicitly. In Phase 4 (Verify), after the suite is green and before the Phase 5 ship gate:

- Compute the range `from-sha` = the worktree branch point (HEAD captured at Phase 2 / the loop's
  first base SHA in `STATE.md`); `to-sha` = current HEAD.
- Run the C3 finalize refresh steps (staleness-gated, `changed_range`-scoped, committed as
  `chore(signals): refresh after <topic>`).

Because the loop already refreshed, the Phase 5 ship verb's `signals-gate` sees a fresh stored
file (`stale` exit 0) and skips — no double dispatch. State this explicitly in the autopilot
step so a reader knows the gate's no-op is intended.

### C5 — Documentation + wiring surfaces

- **`CLAUDE.md`** (bundle source) Workflow §3: change "triggers signals refresh on source
  changes" to reflect that the implementation phase owns the refresh and `/commit` refreshes
  only for ad-hoc real-code (non-docs) commits.
- **`CLAUDE.local.md`** cross-artifact wiring rules: amend the "Ship verbs must trigger signals
  refresh on source-tree changes" bullet — ship verbs refresh only when ad-hoc committing a
  real code change (docs-only skipped); the implement loop / autopilot is the primary refresh
  point, scoped to the SHA range. Keep the symmetry bullet accurate (all three flows share the
  `signals-gate` partial, so the docs-only guard propagates to all of them).
- **`docs/spec/signals-workflow.md`** (parent): amend the `signals-gate` probe section to add
  the docs-only step + a pointer to this child spec for the impl-phase timing; add a change-log
  entry. Surgical — do not rewrite the legacy verb-name sections.
- **`docs/reference/signals-workflow.md`**: update the human-facing description of when refresh
  fires.
- **`templates/commands/atomic-help.md`**: the `signals` topic row currently reads "Ship verbs
  auto-dispatch `atomic-signals-inferrer` on source-tree changes." Update it to name the
  implement-loop/autopilot finalize refresh as primary and the ship-verb refresh as the ad-hoc
  fallback (docs-only skipped). One line.

## Build pipeline (mandatory, same-commit)

This change touches **source artifacts** (`templates/` → rendered `commands/` + `agents/`, and
`CLAUDE.md`). Therefore:

- After editing any `templates/**` file: run `make render` (repo root) and stage the regenerated
  `commands/**` + `agents/**`.
- After editing any source artifact (`agents/`, `commands/`, `skills/`, `output-styles/`,
  `rules/`, `CLAUDE.md`): run `make -C atomic bundle` and stage the regenerated
  `atomic/internal/embedded/**`.
- Render runs before bundle. Both outputs go in the **same commit** as the source change. CI
  drift gates (`make render && git diff --exit-code`, `make bundle && git diff --exit-code`)
  fail otherwise.

## Success criteria

- SC1 — `atomic-signals-inferrer` documents and consumes `changed_range`; absent → identical
  prior behavior.
- SC2 — `signals-gate` skips when the committed change set is docs-only; refreshes for any
  source/artifact change; the staleness check still gates the non-docs path.
- SC3 — `/subagent-implementation` records the loop base SHA and runs a single range-scoped,
  staleness-gated signals refresh at finalize, committed as `chore(signals)`.
- SC4 — `/autopilot` runs the same range-scoped refresh after the loop and before the ship gate;
  the ship-verb gate is a documented no-op afterward.
- SC5 — `CLAUDE.md`, `CLAUDE.local.md`, `docs/spec/signals-workflow.md`,
  `docs/reference/signals-workflow.md`, and `atomic-help.md` describe the new timing; no surface
  still claims ship verbs are the primary/only refresh trigger.
- SC6 — `make render` and `make -C atomic bundle` are clean (no drift); `atomic validate`
  passes; the `/atomic-help` MISSING-scan reports zero missing commands.

## Checkpoints

- **CP1 — mechanism.** C1 (inferrer `changed_range`) + C2 (gate docs-only guard). Edit
  `templates/agents/atomic-signals-inferrer.md` + `templates/shared/signals-gate.md`; render +
  bundle.
- **CP2 — dispatch sites.** C3 (`/subagent-implementation` finalize) + C4 (`/autopilot`). Edit
  `templates/commands/subagent-implementation.md` + `templates/commands/autopilot.md`; render +
  bundle.
- **CP3 — surfaces.** C5 (`CLAUDE.md`, `CLAUDE.local.md`, parent spec, reference doc,
  `atomic-help.md`); render + bundle (CLAUDE.md + atomic-help template both feed the bundle).

## Change log

### 2026-06-29 — Initial

New child spec. Moves the primary signals refresh from commit-time to implementation-phase
finalize, scoped to the loop's SHA range via a new `changed_range` inferrer arg; adds a
docs-only skip to the commit-time `signals-gate` fallback. Coordination is the existing
content-based `atomic signals stale` exit code — no marker file. No Go change.

### 2026-06-29 — Gate uses staged set

C2 guard updated: uses `git diff --cached --name-only` (the staged set about to be committed)
instead of `git diff --name-only` (unstaged changes). An empty staged set now falls through to
the `atomic signals stale` check rather than being treated as all-docs — prevents the
post-merge or post-squash defense-in-depth gate from short-circuiting to a skip when nothing
is staged.

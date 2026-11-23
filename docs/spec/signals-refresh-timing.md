# Spec: signals refresh timing

Implementation scans before updating documentation. Autopilot uses the same workflow. Ad-hoc source and prompt commits use the same documentation and baseline rules.

## Contract

Compare the scratch scan with the accepted baseline and the task diff. Update affected documentation and review it with the implementation before accepting the new scan and wiki fingerprint. If audit fixes change scope, rescan against the original baseline and review those fixes without rerunning the audit. A blocked documentation update leaves the accepted baseline unchanged. Realm wikis and buckets are outside this workflow.

## Success criteria


- [ ] Compare a scratch scan against the accepted starting baseline. Use the task diff alongside it to identify changes that affect repo wiki pages, rules, specs, and other documentation.
- [ ] Audit the updated docs with the implementation. If audit fixes change scope, rescan against the original baseline and checkpoint-review the fixes without rerunning the audit.
- [ ] Accept the final scan and matching wiki fingerprint only after required documentation passes review. If blocked, keep the old baseline. Do not modify realm wikis or buckets.

## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|---|---|---|
| 1 | Use the scan to guide docs without losing drift | Implementation and shipping prompts | A fixture scan detects changes without overwriting the accepted baseline. |

## Change log

### 2026-11-23: Scan before documentation review

**What changed:** The docs step uses a scratch scan and task diff. Save the baseline only after affected documentation passes review.

**Why:** Saving the scan first hides changes the docs have not caught up with.

### 2026-11-22: Keep only the deterministic scan


**What changed:** Implementation finalization runs the scan without a staleness precheck, prose refresh, or realm marking.

**Why:** Verification needs the source snapshot. Wiki prose is a separate, explicit task.

**Superseded:** The range-scoped inferrer dispatch and automatic dirty marker.

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

### 2026-09-02 — Correction: paths updated to the docs/wiki/ relocation

**What changed:** Replaced `.claude/project/deterministic-signals.md`, `.claude/project/signals.md`,
and `.claude/project/signals/**` references with `docs/wiki/scan.md`, `docs/wiki/index.md`, and
`docs/wiki/*.md`. Replaced `deterministic-signals.prev.md` / `deterministic-signals.md` diff
references with the `tmp/.scan.prev.md` vs `docs/wiki/scan.md` diff.

**Why:** The signals storage relocation from `.claude/project/` to `docs/wiki/`, shipped in
`docs/spec/wiki-storage-relocation.md`, landed after this spec was written; the old paths no longer
exist on disk (verified: `.claude/project/signals.md` and `.claude/project/deterministic-signals.md`
are absent from the tree), so a fresh reader would build against a stale layout.

### 2026-10-18 — Removed: realm wiki marker step

**What changed:** The C3 finalize refresh no longer runs `atomic wiki mark-dirty`, and C1 no longer names a bucket-synthesis inferrer mode.

**Why:** Realm wikis no longer track freshness, so a ship inside a member repo has no marker to set.

**Removed:** the best-effort `atomic wiki mark-dirty` call and the bucket-synthesis mode reference. See `docs/spec/realm-wiki-drop-refresh.md`.

### 2026-10-19 — Artifacts assume atomic is installed

**What changed:** The C3 finalize refresh starts at `atomic signals stale`; it no longer checks `command -v atomic` first.

**Why:** Issue #298: artifacts ship inside and install through the `atomic` binary, so the binary is always present when they run. See `docs/spec/artifacts-mermaid-flows.md`.

**Superseded:** step 1 skipped the refresh when `command -v atomic` found no binary.

## Implementation log

### Shipped — 2026-06-29

Built via `/autopilot` (3 checkpoints, implement→review loop, fresh-context subagents). No Go
change — SHA-range scoping and docs-only classification are done by the agent/gate via `git`.
Commits (chronological):

- `fb18390` — CP1 mechanism: inferrer `changed_range` + signals-gate docs-only guard (feat)
- `d437164` — CP2 dispatch sites: `/subagent-implementation` + `/autopilot` finalize refresh (feat)
- `1f1c470` — CP3 surfaces: CLAUDE.md, parent spec, reference doc, `/atomic-help` row (docs)

**Out-of-scope work performed during this build:**
- CP1 reviewer nit folded in-iteration: gate inspects the staged set (`--cached`) with an
  empty-set fall-through (more correct for the post-merge/squash gate). Spec C2 + change log
  updated to match.

**Unforeseens:**
- `CLAUDE.local.md` is untracked (not actually gitignored) in this repo. Its C5 edit was applied
  directly to the main-repo live copy out-of-band — it cannot ride the branch (committing it
  would leak the private overlay; the worktree copy is discarded on cleanup).
- In a repo where `.claude/project/signals.md` is gitignored (this repo) and the loop runs in a
  worktree, the finalize refresh regenerates throwaway worktree signals. Pre-existing limitation
  of signals-in-worktree (the prior ship-verb refresh had the same edge), orthogonal to the
  timing change. For this run, main's signals are refreshed post-merge instead.
- Path note: `.claude/project/signals.md` above named the pre-relocation storage location; the
  2026-09-02 Correction entry moved this spec's live paths to `docs/wiki/scan.md`.

**Deferred items still open:** none.

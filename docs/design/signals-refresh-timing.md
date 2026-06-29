# Design: signals refresh timing

## Problem

Project-signals refresh is wired to **commit time only**. The `signals-gate` partial is
embedded in the `commit-flow`, `merge-flow`, and `squash-flow` ship partials; each ship
verb runs `atomic signals stale` and, on exit 1, dispatches the `atomic-signals-inferrer`
agent. The `/subagent-implementation` and `/autopilot` implement→review loops never refresh
signals themselves — they only commit per green iteration.

Two consequences:

1. **Wrong moment.** The natural point to recompute the project map is when a coherent unit
   of work lands — the end of an implementation phase, with the full set of changes visible
   as a SHA range. Tying refresh to `/commit` means it fires per-commit (the loop commits
   several times per task) or not at all (loop commits go through `git commit` directly, not
   through a ship verb, so they never trigger the gate — refresh happens only if the user
   later runs a ship verb).

2. **Docs churn triggers expensive dispatch.** `atomic signals stale` is content-based and
   the deterministic substrate counts per-language LOC and lists doc paths, so a docs-only
   commit trips exit 1 and dispatches the inferrer (a Sonnet agent that fans out sub-agents).
   That cost is not worth paying for a README tweak.

## Goal

- The **implementation phase owns the refresh**: after the implement→review loop finishes
  (post-verify, post-`/documentation`), refresh signals once, scoped to the loop's change
  range, and commit it.
- The **inferrer is told the exact change range** (`from-sha`..`to-sha`) so it re-infers only
  the domains the work touched, instead of inferring from whatever the last scan happened to
  capture.
- The **commit-time gate becomes a fallback**: it refreshes only when the user is ad-hoc
  committing a *real code change* — never for docs-only commits, and never redundantly when
  the implementation phase already refreshed.

## Key insight: staleness is the coordinator (no marker file)

The requirement "the commit section should only run signals if the implementation phase
didn't" needs **no new state**. `atomic signals stale` already assembles the deterministic
snapshot exactly as a scan would and compares it to the stored one — it returns exit 0
(fresh) only when a fresh scan would produce identical content.

So:

- Implementation phase refreshes and commits signals → the stored signals file is now
  current → a subsequent ship verb's `atomic signals stale` returns **exit 0 → skips**.
- User ad-hoc commits real code with no prior refresh → snapshot differs → **exit 1 → runs**.

This is exactly the desired "only if the impl phase didn't" behavior, delivered by existing
code. A marker file would duplicate what the content hash already proves, and could drift out
of sync with reality. Rejected.

## Approaches considered

| Approach | Coordination | Verdict |
|----------|--------------|---------|
| A — marker file written by the loop, read by the gate | explicit flag in scratchpad/state | rejected: new state that can drift; the loop's scratchpad is deleted at finalize and absent for ad-hoc commits, so the gate can't rely on it |
| **B — staleness check as coordinator** | content hash via `atomic signals stale` | **chosen**: zero new state, reuses the gate's existing exit-code contract, "prefer code over the model" |

## docs-only classification

The commit-time gate must skip *before* the staleness check for docs-only commits (staleness
would otherwise trip exit 1 on the LOC delta). "docs-only" is a deterministic path test on the
commit's changed-file set: every changed path is either

- under a `docs/` directory at any depth, or
- a conventional top-level documentation file: `README*`, `CHANGELOG*`, `CONTRIBUTING*`,
  `CODE_OF_CONDUCT*`, `SECURITY*`, `LICENSE*`.

Anything else — any source file, **any bundled-artifact `.md`** (under `agents/`, `commands/`,
`skills/`, `rules/`, `output-styles/`), `CLAUDE.md`, config, build files — counts as a real
code change. In a config repo the artifact `.md` files *are* the product, so they must not be
classified as docs. The rule errs toward "real change": a false negative only costs one
harmless staleness check (which itself returns fresh if nothing material moved).

## SHA-range scoping (no Go change)

`atomic signals scan` stays whole-repo and deterministic — the substrate always reflects the
full tree. The SHA range only scopes the **LLM re-inference** step (which domains get
rewritten). The inferrer already supports incremental mode driven by a changed-paths set; the
only change is the *source* of that set:

- Today: diff of `deterministic-signals.prev.md` vs `deterministic-signals.md`.
- New, when the caller passes `changed_range: <from>..<to>`: `git diff --name-only <from>..<to>`
  plus uncommitted (`git diff --name-only <from>`), unioned. The agent runs `git` itself
  (it has Bash), so no new CLI flag and no Go change.

When no `changed_range` is supplied (the existing `/refresh-signals` and ship-verb callers),
behavior is unchanged — the prev/current snapshot diff drives incremental mode as before.

## Where it plugs in

```
implementation phase (subagent-implementation / autopilot)
  loop: implement → review → commit-per-green
  finalize: verify → /documentation
            └─► inferrer(mode: silent, changed_range: <loop-base>..HEAD)   NEW
                └─► commit chore(signals)

ship verb (/commit and its merge/squash flows) — ad-hoc path
  signals-gate:
    0. docs-only commit?  ──► skip (NEW guard)
    1. atomic signals stale
         exit 0 (fresh — incl. "loop already refreshed") ──► skip
         exit 1 (stale real change) ──► dispatch inferrer (unchanged)
         exit 2 ──► report + skip (unchanged)
```

The loop's base SHA is the HEAD captured before iteration 1 (recorded in `STATE.md`). The
range is `loop-base..HEAD` after docs settle, so the refresh sees code + doc commits together.

## Non-goals

- No change to `atomic signals scan` / `stale` / `diff` Go code.
- No change to wiki-output or bucket-synthesis modes of the inferrer.
- No new config key or memory value (the behavior is a fixed contract, not a tunable —
  axiom 2 graduation triggers not met).

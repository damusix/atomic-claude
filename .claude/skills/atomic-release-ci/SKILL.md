---
name: atomic-release-ci
description: Diagnose and fix broken release-please CI in this repo. Auto-fires on "release-please CI is failing", "release branch is out of date", "release branch is stale", "release PR is red", "release-please broke", "fix the release CI", "release branch CI failing", "version branch is behind", "release PR changelog is wrong", "release PR missing work". Contributor-only — never bundled, never installed.
user-invocable: true
---


# atomic-release-ci


Project-local skill for fixing the recurring breakage on the `release-please--branches--main` branch and its PR. Contributor-scope only: lives under `.claude/skills/`, auto-loaded for sessions in this repo, never bundled (`atomic/internal/bundlemirror/mirror.go` ships only `skills/atomic-*/` at the repo root).


## Why this exists


release-please maintains a release branch (`release-please--branches--main`) and an open release PR. CI (`.github/workflows/ci.yml`) runs on **every** branch (`push: branches: ["**"]`), so the release branch gets its own CI runs. Those runs go red for a small set of recurring reasons that are almost never a "real" bug in the release branch itself. This skill encodes the diagnosis and the canonical fix per cause so the loop stops eating time.


The two source-of-truth workflows:

- `.github/workflows/release-please.yml` — `on: push: branches: [main]`. Regenerates the release PR + branch. Uses `secrets.RELEASE_PLEASE_TOKEN`. No `workflow_dispatch` trigger, so the lever for a manual re-trigger is `gh run rerun <id>` against the last release-please run (or an empty commit to main).
- `.github/workflows/ci.yml` — render-drift gate → bundle-drift gate → `go test` → `go vet` → `gofmt` → `atomic validate`. Any of these can fail the release branch.


## The invariant


**Never hand-edit, hand-rebase, or force-push `release-please--branches--main`.** release-please owns that branch. Manual pushes fight the action and create divergence that looks exactly like the bug you're trying to fix. The only legitimate moves are: fix main, re-trigger release-please, or delete the branch so release-please recreates it.


## Durable prevention — already in place (read before "fixing" anything)


The recurring "release branch CI is red" symptom was fixed at the root in `.github/workflows/ci.yml`: the `push` trigger uses `branches-ignore: ["release-please--branches--**"]`, so push-CI does **not** run on the bot's release branch. Rationale: the release branch tip is `old-main + release-commit` — its state in isolation is never meaningful and goes stale-red whenever a fix lands on main after the release PR was cut. The release PR is still gated by the `pull_request` trigger, which tests the **merge-into-main result** — that always reflects current main, so it cannot go stale-red.

**Consequence for diagnosis:** a red check on the release PR now almost always means the **merge result is genuinely broken** — i.e. main itself is broken (cause 2 below), or there is a real merge conflict. The old "stale-base push false-red" (cause 1) should no longer appear. If you see a `push`-event CI run on the release branch go red, the `branches-ignore` rule has regressed — check `ci.yml` first.

The branch will still show "N commits behind main" in the GitHub UI. That is cosmetic — release-please keeps the release commit stable on purpose to preserve PR review state, and the merge-result check is what gates the merge. Do not try to "sync" the branch to silence the cosmetic notice; that fights release-please and churns the PR.


## "Pushing to main doesn't update the version branch" — tip-lag vs content (verified 2026-05-30)


**The recurring confusion.** You push commits to main, the release PR still says "N commits behind main," and it looks like release-please isn't picking up your work. It is — you're conflating two different kinds of "current":


- **PR content (changelog + version) — always current.** release-please runs on *every* push to main (`release-please.yml`, `on: push: branches: [main]`) and regenerates the release commit: the changelog, the version bump, the PR body. A push of new `feat:`/`fix:`/`fix!:` work shows up in the PR body within ~60s. Verify with `gh pr view <PR#> --json body`. (A push of only filtered types — `docs:`/`chore:`/`refactor:` — correctly produces *no* visible PR change; that is not a bug.)
- **Branch git tip (parent commit) — lags by design.** release-please updates the release commit **in place** and keeps its original parent. It does **not** re-parent the branch when main advances. So the branch tip stays at `old-main + release-commit`, missing every commit that landed after the branch was last created. `git merge-base --is-ancestor origin/main origin/release-please--branches--main` returns false; the lag is `K  1` where K = commits on main since branch creation.


**Why the lagging tip is still safe to merge.** The PR's `pull_request` check tests the merge *into current main*, which always reflects current main. Whatever ship-merge style you use (merge / squash / rebase), the released main ends up containing all of main's commits plus the release commit. The release is correct even with a lagging tip.


**When to actually re-sync the tip.** Only when you want the branch to *literally contain* the latest main commit — review hygiene, or a final tidy right before merging the PR. The reliable resync is **delete-and-recreate** (below): delete the branch (closes the PR), push an **empty commit** to main, release-please recreates the branch from current main HEAD and opens a fresh PR (new number, identical version).


**Critical caveat — the resync is not permanent.** The next push to main re-lags the tip immediately (same in-place-update behavior). So delete-and-recreate is a **one-time, pre-merge step**, never a standing state. If you are still actively pushing, do **not** keep recreating — just merge the PR when ready; the merge-result is correct regardless. (This reconciles the "do not sync the cosmetic notice" rule above: don't chase the notice repeatedly, but one deliberate pre-merge resync is legitimate.)


**Verification after a resync (all must hold):**

```bash
git fetch origin
git merge-base --is-ancestor origin/main origin/release-please--branches--main && echo "branch includes current main"
git rev-list --left-right --count origin/main...origin/release-please--branches--main   # expect: 0<TAB>1
gh pr list --state open --json number,title -q '.[] | "#\(.number) \(.title)"'          # fresh PR, same version
```


## Diagnosis (run these first, always)


```bash
git fetch origin

# 1. Is the release branch based on current main HEAD, or a stale one?
git merge-base --is-ancestor origin/main origin/release-please--branches--main \
  && echo "branch includes current main" \
  || echo "STALE BASE — branch is missing commits that are on main"

# 2. Lag count (main-ahead  branch-ahead)
git rev-list --left-right --count origin/main...origin/release-please--branches--main

# 3. Is the release branch's CI red, and on what step?
gh run list --branch release-please--branches--main --limit 5 \
  --json databaseId,workflowName,status,conclusion,headSha

# 4. Is MAIN's CI also red? (decides whether the bug is real or inherited)
gh run list --branch main --limit 5 \
  --json databaseId,workflowName,status,conclusion,headSha

# 5. Is the changelog missing known work? Inspect the open release PR.
gh pr list --state open --json number,title,headRefName
gh pr view <PR#> --json body -q .body
```


## Cause → fix table


| # | Cause | Signal | Fix |
|---|-------|--------|-----|
| 1 | **Stale base** (should be rare now — see Durable prevention) | Diagnosis step 1 says STALE BASE **and** you see a red `push`-event run on the branch (which means `branches-ignore` regressed), or the PR check rollup is red while main is green | First check `ci.yml` still has `branches-ignore: ["release-please--branches--**"]`; restore it if missing. If the branch genuinely must be re-parented, the **only** reliable lever is delete-and-recreate (see below) — `gh run rerun` and empty commits do **not** re-parent it (release-please updates the release commit in place and keeps its original parent). |
| 2 | **Main is also broken** | Diagnosis step 4 shows main CI red on the same step; the PR's `pull_request` (merge-result) check is red | Fix main first. The merge-result check goes green once main is green; the release PR follows with no branch surgery. Use the normal plan/implement/ship loop or a surgical fix + push. **This is now the most common real cause** of a red release PR. |
| 3 | **Commit-type mislabel** | Release PR changelog (step 5) is missing work you know shipped; the commits used `refactor:` / `chore:` / `docs:` for real features or breaking changes | This is a **history-relabel** job and is destructive. Do NOT silently rewrite. Surface the `## Release-please conventional commit types` rule in `claude.local.md`, confirm the relabel plan with the user, then rewrite + force-push main per axiom 3. release-please regenerates afterward. |
| 4 | **Render drift** | Failing step is "Verify render is committed" | `make render` from repo root, commit the `commands/` delta, push to main. |
| 5 | **Bundle drift** | Failing step is "Verify bundle is committed" | `make -C atomic bundle` (after render), commit `atomic/internal/embedded/`, push to main. |
| 6 | **Spec / validate lint** | Failing step is "Validate" (`atomic validate`) | Build + run locally: `cd atomic && make build && ../bin/atomic validate`. Fix the flagged file (common: a `docs/spec/*.md` checkpoints table that is not the exact 4-column `\| # \| Checkpoint \| Files/areas \| Verifies \|` header). Commit to main, push. |


### Delete-and-recreate (the only reliable branch re-parent)


When the release branch genuinely must be rebuilt on current main (e.g. `branches-ignore` was not yet in place and the branch is stale-red, or the branch state is corrupt), delete it and let release-please recreate it from current main HEAD:

```bash
git push origin --delete release-please--branches--main   # AUTO-CLOSES the open release PR
git commit --allow-empty -m "chore: recreate release-please branch from current main" && git push
# release-please.yml runs on the push, recreates the branch from current main + a fresh release PR
git fetch origin
git merge-base --is-ancestor <fix-sha> origin/release-please--branches--main && echo RECREATED
```

**Cost:** the open release PR closes and a new one opens (new PR number, same release content). Confirm with the user first (axiom 3 — deleting a remote branch + closing a PR is a visible, hard-to-silently-undo action). Prefer fixing `ci.yml` (`branches-ignore`) over repeated delete-recreate; the CI fix removes the need.


## Standard procedure


1. **Diagnose** — run all five diagnosis blocks. Classify into exactly one cause row (they are usually mutually exclusive; if main is red, that dominates — fix cause 2 first).
2. **Confirm before destructive action** — cause 3 (history rewrite), delete-and-recreate, and any force-push touch published history or close a PR. Per axiom 3, get an explicit per-action yes. Causes 4, 5, 6 are safe (forward commit to main).
3. **Apply the matching fix.** With `branches-ignore` in place, the most common real cause is now cause 2 (main itself is broken) — fix main and the merge-result check follows, no branch surgery. Reach for delete-and-recreate only when the branch genuinely must be re-parented and the CI fix is not sufficient; `gh run rerun` and empty commits do not re-parent the branch.
4. **Re-watch.** Dispatch `/watch-ci release-please--branches--main` (or watch main if the fix landed there). Confirm the previously-failing step is now green and that release-please regenerated the PR at the expected version. Note: after a delete-and-recreate the PR number changes — watch the new PR.
5. **Report** — one line: what was red, which cause, what fixed it, current state.


## Notes


- **The `fix(spec):`/validate trap (cause 6).** `atomic validate` rule S5 requires the checkpoints table header to be *exactly* `| # | Checkpoint | Files/areas | Verifies |`. The `/atomic-plan` template has historically emitted a richer 6-column variant (`Agent`, `Est. files`) that fails the linter. If a freshly-planned spec breaks validate, fold the extra columns into the Checkpoint cell. (Tracked separately: the `/atomic-plan` template should stop emitting the 6-column form, or the validator should accept the 4 required columns as a prefix.)
- **Re-trigger does NOT re-parent (learned the hard way).** `gh run rerun <release-please-run-id>` and pushing empty commits to main both run release-please successfully, but neither moves the release branch's base — release-please updates the release commit in place and keeps its original parent. The branch's base only changes on **first creation**. That is why delete-and-recreate is the only reliable re-parent, and why the `branches-ignore` CI fix (which sidesteps the stale tip entirely) is the preferred permanent answer.
- **Cross-reference.** Commit-type discipline (cause 3) is owned by the `## Release-please conventional commit types — hard rules` section in `claude.local.md`. This skill detects the symptom; that section is the prevention.

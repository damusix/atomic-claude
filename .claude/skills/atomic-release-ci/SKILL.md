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


**Never hand-edit, hand-rebase, or force-push `release-please--branches--main`.** release-please owns that branch and force-pushes it on every main push. Manual pushes fight the action and create divergence that looks exactly like the bug you're trying to fix. The only legitimate moves are: fix main, or re-trigger release-please.


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
| 1 | **Stale base** | Diagnosis step 1 says STALE BASE; the branch's failing step is something already green on main | Re-trigger release-please so it rebases the branch onto current main: `gh run rerun <last-release-please-run-id>`. If the branch still doesn't advance, push an empty commit to main: `git commit --allow-empty -m "chore: retrigger release-please" && git push`. |
| 2 | **Main is also broken** | Diagnosis step 4 shows main CI red on the same step | Fix main first. Do not touch the release branch — it follows automatically on the next regeneration. Use the normal plan/implement/ship loop or a surgical fix + push. |
| 3 | **Commit-type mislabel** | Release PR changelog (step 5) is missing work you know shipped; the commits used `refactor:` / `chore:` / `docs:` for real features or breaking changes | This is a **history-relabel** job and is destructive. Do NOT silently rewrite. Surface the `## Release-please conventional commit types` rule in `claude.local.md`, confirm the relabel plan with the user, then rewrite + force-push main per axiom 3. release-please regenerates afterward. |
| 4 | **Render drift** | Failing step is "Verify render is committed" | `make render` from repo root, commit the `commands/` delta, push to main. |
| 5 | **Bundle drift** | Failing step is "Verify bundle is committed" | `make -C atomic bundle` (after render), commit `atomic/internal/embedded/`, push to main. |
| 6 | **Spec / validate lint** | Failing step is "Validate" (`atomic validate`) | Build + run locally: `cd atomic && make build && ../bin/atomic validate`. Fix the flagged file (common: a `docs/spec/*.md` checkpoints table that is not the exact 4-column `\| # \| Checkpoint \| Files/areas \| Verifies \|` header). Commit to main, push. |


## Standard procedure


1. **Diagnose** — run all five diagnosis blocks. Classify into exactly one cause row (they are usually mutually exclusive; if main is red, that dominates — fix cause 2 first).
2. **Confirm before destructive action** — causes 3 (history rewrite) and any force-push touch published history. Per axiom 3, get an explicit per-action yes. Causes 1, 4, 5, 6 are safe (re-run, or a forward commit to main).
3. **Apply the matching fix.** Most release-branch-only failures are cause 1: the branch is stale-based and just needs release-please to rebase it. `gh run rerun` is the cheapest lever.
4. **Re-watch.** Dispatch `/watch-ci release-please--branches--main` (or watch main if the fix landed there). Confirm the previously-failing step is now green and that release-please regenerated the PR at the expected version.
5. **Report** — one line: what was red, which cause, what fixed it, current state.


## Notes


- **The `fix(spec):`/validate trap (cause 6).** `atomic validate` rule S5 requires the checkpoints table header to be *exactly* `| # | Checkpoint | Files/areas | Verifies |`. The `/atomic-plan` template has historically emitted a richer 6-column variant (`Agent`, `Est. files`) that fails the linter. If a freshly-planned spec breaks validate, fold the extra columns into the Checkpoint cell. (Tracked separately: the `/atomic-plan` template should stop emitting the 6-column form, or the validator should accept the 4 required columns as a prefix.)
- **Re-trigger reliability.** `gh run rerun <id>` replays the release-please run with its original push event, which rebases the branch onto current main HEAD. This is more reliable than waiting for the next organic main push. If you find yourself re-running often, consider adding `workflow_dispatch:` to `release-please.yml` for a cleaner manual trigger — but that is an enabling change, not required by this skill.
- **Cross-reference.** Commit-type discipline (cause 3) is owned by the `## Release-please conventional commit types — hard rules` section in `claude.local.md`. This skill detects the symptom; that section is the prevention.

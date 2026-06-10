---
description: Triage open GitHub issues — suggest labels for each (you approve), and run a two-stage staleness lifecycle on issues waiting on the reporter: nudge at 14 days, close at ~30. Repo-local to atomic-claude. Never auto-acts; every label, nudge, and close needs your OK.
---

You triage this repo's open GitHub issues. The deterministic half — which issues are stale, and in what way — is computed by `make triage-scan` (script: `scripts/triage-scan.sh`); your job is the judgment half: suggest labels, draft the nudge/close comments, propose a plan, and execute only what the user approves. Nothing is applied without an explicit OK — labels, nudges, and closes are all suggestions until confirmed.

## Prereqs

- `command -v gh` — if missing: tell the user to install it (`brew install gh` / https://cli.github.com/) then `gh auth login`. Stop.
- `gh auth status` — if unauthed: tell the user `gh auth login`. Stop.
- `gh repo view --json nameWithOwner,hasIssuesEnabled` — if issues are disabled, stop.

## Staleness model (the contract)

An issue is on the **close track** only when it is *waiting on the reporter*: the last comment is from a maintainer (`authorAssociation` ∈ OWNER / MEMBER / COLLABORATOR, and not the reporter), and the reporter has gone quiet. Two stages, 14 days each:

```
maintainer replied last
        │  reporter silent 14d
        ▼
   NUDGE  ── @reporter comment ("reply within 2 weeks or this closes") + label `stale`
        │  still silent 14d more  (≈ 1 month total)
        ▼
   CLOSE  ── courtesy comment + close (reason: not planned). `stale` label stays.
```

If the reporter replies at any point, the ball is back with the maintainers: no nudge, no close, and if `stale` was set, propose removing it (`unstale`). The nudge comment carries a hidden marker `<!-- atomic-triage:nudge -->` so a later run detects the nudge deterministically instead of guessing.

## Workflow

<workflow>

### Step 1 — Classify staleness (deterministic — code, not judgment)

Run the scanner. It enumerates open issues and returns each one's staleness `action` purely from comment authorship and timestamps — do not reimplement this in the prompt or eyeball dates yourself:

```
make triage-scan
```

(From the repo root. Scope to specific issues with `make triage-scan ISSUES="43 50"`.) It prints a JSON array; an empty `[]` means no open issues — report `no open issues.` and stop. The deterministic logic lives in `scripts/triage-scan.sh`; if you need to change the staleness rules, edit that script, not this command.

Each object is `{ number, title, reporter, labels, idleDays, action }`. Action meanings: `nudge` (post stage-1 follow-up), `close` (stage-2 close), `unstale` (reporter re-engaged — drop the `stale` label), `wait-nudge` / `wait-close` (on track but under 14 days — report the day count, take no action), `none` (ball is with maintainers, nothing to do).

### Step 2 — Suggest labels (this is the judgment call)

For each issue, read its title and body and propose labels from the repo's existing set only:

```
gh label list --limit 200 --json name,description
```

Map by content. Examples: a crash/defect → `bug`; a feature ask → `enhancement`; docs → `documentation`; an open question → `question`; needs investigation before action → `research`. **Repo-specific:** this project supports macOS and Linux only — a Windows-specific issue gets `windows`, and you should note that it is out-of-scope and a candidate for `wontfix` / close, surfacing that to the user rather than acting on it. Never create new labels; suggest only from the list. Skip issues that already carry the right labels.

### Step 3 — Present one plan, get approval

Show a single table — one row per issue — covering both concerns:

| # | Title | Suggested labels | Staleness | Why |
|---|-------|------------------|-----------|-----|
| 43 | atomic doctor … Windows | `windows` (+ note: out-of-scope) | wait-nudge (0d) | OWNER replied today; reporter has the ball |

Then ask the user, via `AskUserQuestion` or an inline prompt, to approve. Make it easy to approve everything, a subset, or nothing. Treat label-applies, nudges, and closes as distinct so the user can OK labels but hold a close. Default to doing nothing on anything not explicitly approved (axiom: destructive ops — and outward-facing comments — require explicit confirmation).

### Step 4 — Execute approved actions only

- **Labels:** `gh issue edit N --add-label "a,b"`
- **Nudge:** post the comment below, then `gh issue edit N --add-label stale`
- **Close:** post the close comment below, then `gh issue close N --reason "not planned"`. Leave the `stale` label in place.
- **Unstale:** `gh issue edit N --remove-label stale`

After executing, report what was applied (issue → action) in a short list. Do not claim an action you did not run.

</workflow>

## Comment templates

Atomic, courteous, no AI bylines. Substitute `{reporter}` and `{nudgeDate}`.

**Nudge** (stage 1 — the hidden marker is load-bearing for the next run; keep it):

```
Hey @{reporter} — following up here. This has been waiting on your reply for about two weeks. If we don't hear back within the next two weeks we'll close it to keep the tracker current. No hard feelings — reopen anytime once you have the details. Thanks!

<!-- atomic-triage:nudge -->
```

**Close** (stage 2):

```
Closing for inactivity — no reply since the follow-up on {nudgeDate}. Not a dead end: reopen anytime with the requested info and we'll pick it right back up. Thanks @{reporter}.
```

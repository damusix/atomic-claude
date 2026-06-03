---
description: Commit current changes, push, then open a PR via gh. Delegates message + body format to atomic-commit and atomic-review skills.
---

## 1. Commit


<commit-flow>

Invoke the `atomic-commit` skill for message format.

1. Read the current state: `git status`, `git diff`, `git log -n 10 --oneline` (parallel).
2. **Session reports** — check for `.claude/.scratchpad/session-reports/<branch>/`. If the dir exists and has `*.md` files, read them chronologically and pass their content to `atomic-commit` as supplemental why-context.
3. **Stage files** explicitly by path. Skip secrets, build artifacts, and large binaries. **Why:** secrets in git history are irrecoverable even after rewrite; binaries bloat the repo permanently. If the intent is ambiguous, ask.
4. <doc-impact>
Check whether the staged changes affect any indexed documentation surfaces.

**Step 1 — find the surfaces table.**

Look for a `## Documentation surfaces` section in the CLAUDE instructions already loaded in context (search `CLAUDE.md`, `claude.local.md`, or any `@`-included file). If no such section exists, print exactly:

```
no documentation surfaces indexed. run /documentation to set up.
```

Then skip the remaining steps — no error, no blocking.

**Step 2 — match staged diff against surfaces.**

Run `git diff --cached` and read the output. For each row in the `## Documentation surfaces` table, use LLM judgment to decide whether the staged diff touches the domain described in that row's `Covers` column. Path overlap, changed symbols, and changed domain terms all count. This is a judgment call within the current context window — no separate dispatch.

Ignore any surface entry where `impact_type` is `missing`. Maintenance mode never suggests new pages.

If no surfaces match the diff, proceed silently. No output. No prompt.

**Step 3 — walk matched surfaces.**

For each surface that matches (stale or incomplete), print:

```
doc surface: <path> — <reason the diff makes this stale>
proposed: <one sentence describing the targeted edit>

  [y] Yes    — update now
  [l] Later  — create a follow-up
  [r] Remind — schedule a reminder
  [s] Skip   — no action
```

Wait for the user's response per surface before continuing to the next.

**Step 4 — act on response.**

- **Yes** — invoke the `atomic-documentation` skill scoped to this surface. Pass the path, the diff context, and the proposed change as the prompt. The skill opens the file, makes the edit, and stages it with `git add <path>`. Do not proceed to the next surface until the skill completes.

- **Later** — run:

  ```
  atomic followups add \
    --id "doc-<slugified-path>-<short-hash>" \
    --title "update <path> — <reason>" \
    --severity nit \
    --origin "ship-verb doc-impact"
  ```

  Use the first 6 characters of the HEAD commit SHA as `<short-hash>`. If `atomic` binary is absent, print the follow-up details as plain text and continue.

- **Remind** — prompt: `When should I remind you? (e.g. "after the PR", "tomorrow", "end of week")`. Accept natural-language input and invoke `/remind-me <timing> update <path> — <reason>`. Continue without blocking if `CronCreate` is unavailable.

- **Skip** — no action, no record.

Run doc-impact before signals refresh. **Why:** new or updated doc files appear in the signals scan only if they're staged before the scan runs.
</doc-impact>
5. <signals-refresh>
Refresh project signals so Claude's map stays current for the next session.

1. Check `command -v atomic`. If missing, skip.
2. Run `atomic signals stale` and act on the exit code:
   - **exit 0** (fresh) → skip the refresh.
   - **exit 1** (stale) → refresh is mandatory. Continue to step 3. Do NOT second-guess this with `atomic signals diff`, file counts, or a judgment that "the change was small" — exit 1 means a fresh scan would produce different deterministic content than the stored signals file, and the only correct response is to refresh. Skipping it accumulates drift. The command prints how much would change and the directive; follow it.
   - **exit 2** (error, e.g. signals file missing) → report the stderr message and skip; a refresh cannot run against a missing baseline.
3. Dispatch the `atomic-signals-inferrer` agent in silent mode:
   ```
   mode: silent
   first_run: false
   ```
   Stage `.claude/project/deterministic-signals.md` and `.claude/project/signals.md` after the agent completes.

`atomic signals stale` is content-based: it assembles the deterministic snapshot exactly as a scan would and compares it to the stored one, returning exit 1 only when they actually differ. A no-op regeneration that merely bumps file mtimes stays fresh; a real shift in the project map goes stale. Treat exit 1 as an unconditional trigger, not a hint.
</signals-refresh>
6. **Commit** using a HEREDOC message.
7. **Clean up session reports** — on successful commit, delete `.claude/.scratchpad/session-reports/<branch>/`. The reports were consumed by the commit message. If the commit failed, leave them for the next attempt.
8. `git status` to confirm.

One commit per invocation. If the diff spans unrelated concerns, ask how to split.

</commit-flow>

If nothing to commit and branch has unpushed commits, skip to push.
If nothing to commit and branch is up to date, stop.

## 2. Push


<push-flow>

1. `git branch --show-current` — record the branch.
2. `git status --porcelain` — if dirty, stop and tell the user to commit first.
3. `git log @{u}..HEAD --oneline 2>/dev/null` — show what is about to ship. If the branch has no upstream, that is expected (set in step 4).
4. Push:
    - No upstream → `git push -u origin <branch>`.
    - Upstream exists and branch is ahead → `git push`.
    - Already up to date → stop.
5. Print the resulting `<old>..<new> <branch> -> <branch>` line.

If push is rejected (non-fast-forward), stop and tell the user. Let them decide how to resolve it.

</push-flow>

## 3. PR


<pr-flow>

Invoke the `atomic-review` skill for PR title and body tone.

1. `git branch --show-current` — if on base branch, stop.
2. Determine base: `gh repo view --json defaultBranchRef -q .defaultBranchRef.name`.
3. Read what is shipping: `git log <base>..HEAD --oneline` + `git diff <base>...HEAD --stat` (parallel).
4. Check for existing PR: `gh pr view --json url 2>/dev/null` — if one exists, print its URL and stop.
5. Push if needed: `git push -u origin <branch>` (no upstream) or `git push` (behind).
6. Create the PR:
    ```
    gh pr create --title "<imperative, ≤70 chars>" --body <HEREDOC>
    ```
    Body sections: `## Summary` (1-3 bullets), `## Why` (skip if obvious), `## Test plan` (checklist).
7. Print the PR URL.

If the working tree is dirty, stop and tell the user to commit first.

</pr-flow>

<git-safety>
- Stage explicitly by name (`git add <path>`), never `git add -A`. **Why:** `-A` can accidentally include secrets or untracked binaries.
- Use relative paths for `git add` based on the current working directory. **Why:** absolute paths and `git -C` can silently stage files outside the intended scope.
- Run each `git` command as a separate Bash call. **Why:** chaining with `&&` makes it impossible to inspect intermediate state and hides partial failures.
- On pre-commit hook failure: fix the root cause, re-stage, and create a new commit — never `--amend`. **Why:** amending after a hook failure modifies the PREVIOUS commit, which may destroy unrelated work.
- Keep force-push off the base branch. If a rollback is needed, use `git revert` so the bad SHA stays in history. **Why:** force-pushing rewrites shared history, breaking every collaborator's checkout.
</git-safety>

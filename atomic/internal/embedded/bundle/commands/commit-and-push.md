---
description: Commit current changes, then push. No PR, no merge. Delegates message format to the atomic-commit skill.
---

## 1. Commit


<commit-flow>

Invoke the `atomic-commit` skill for message format.

1. Read the current state: `git status`, `git diff`, `git log -n 10 --oneline` (parallel).
2. **Session reports** — check for `.claude/.scratchpad/session-reports/<branch>/`. If the dir exists and has `*.md` files, read them chronologically and pass their content to `atomic-commit` as supplemental why-context.
3. **Stage files** explicitly by path. Skip secrets, build artifacts, and large binaries. If the intent is ambiguous, ask.
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

Run doc-impact before signals refresh so new or updated doc files are picked up by signals in the same pass.
</doc-impact>
5. <signals-refresh>
Refresh project signals so Claude's map stays current for the next session.

1. Check `command -v atomic`. If missing, skip.
2. Check `atomic signals stale`. If fresh (exit 0), skip.
3. Both pass → invoke the `atomic-signals` skill in silent mode. Stage `.claude/project/deterministic-signals.md` and `.claude/project/signals.md`.

The `atomic signals stale` command is the source of truth — it fast-fails when nothing changed and catches structural shifts that a file-extension allowlist would miss.
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

<git-safety>
- Use relative paths for `git add` based on the current working directory.
- Run each `git` command as a separate Bash call.
- On pre-commit hook failure: fix the root cause, re-stage, and create a new commit. The hook exists for a reason.
- Keep force-push off the base branch. If a rollback is needed, use `git revert` so the bad SHA stays in history.
</git-safety>

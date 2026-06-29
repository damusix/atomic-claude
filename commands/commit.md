---
description: Stage, commit, and optionally ship further. Pass an escalation token (push, pr, merge, squash, squash merge) to skip the prompt. With no token, commits then asks how far to ship. Delegates message format to the atomic-commit skill.
---

## Parse arguments

Read `$ARGUMENTS`. Scan for escalation tokens: `push`, `pr`, `merge`, `squash`, `squash merge`.

Token → escalation mapping:

| Token(s) in args | Path |
|---|---|
| _(none)_ | commit only, then ask |
| `push` | commit + push |
| `pr` | commit + push + PR |
| `merge` | commit + merge to base |
| `squash` (without `merge`) | commit + squash branch |
| `squash merge` (both tokens) | commit + squash + merge to base |

If the args contain none of the tokens above, run the commit step, then prompt (see "Interactive path" below).

## Commit


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

0. **Docs-only guard.** Inspect the staged file set with `git diff --cached --name-only`. If the staged set is **empty** (e.g., in a post-merge or post-squash context where the commit already landed and nothing remains staged), skip the docs-only check and fall through to step 1 — an empty staged set does not mean all paths are documentation. If the staged set is non-empty and **every** staged path is documentation, skip the refresh entirely — do not continue to step 1. A path is documentation when it is under a `docs/` directory at any depth, OR is a top-level `README*` / `CHANGELOG*` / `CONTRIBUTING*` / `CODE_OF_CONDUCT*` / `SECURITY*` / `LICENSE*`. Any other path — source, config, build files, `CLAUDE.md`, or any bundled-artifact `.md` under `agents/`, `commands/`, `skills/`, `rules/`, or `output-styles/` — means the commit is NOT docs-only; continue to step 1. **Why:** the deterministic substrate counts per-language LOC, so a docs-only commit trips `stale` exit 1 and dispatches the inferrer for no real map change. In a config repo the artifact `.md` files are the product, so they must count as source, not docs.
1. Check `command -v atomic`. If missing, skip.
2. Run `atomic signals stale` and act on the exit code. **Why:** the staleness check also prevents a redundant refresh when the implementation phase already refreshed — a fresh stored signals file returns exit 0 and skips dispatch.
   - **exit 0** (fresh) → skip the refresh.
   - **exit 1** (stale) → refresh is mandatory. Continue to step 3. Do NOT second-guess this with `atomic signals diff`, file counts, or a judgment that "the change was small" — exit 1 means a fresh scan would produce different deterministic content than the stored signals file, and the only correct response is to refresh. Skipping it accumulates drift. The command prints how much would change and the directive; follow it.
   - **exit 2** (error, e.g. signals file missing) → report the stderr message and skip; a refresh cannot run against a missing baseline.
3. Dispatch the `atomic-wiki-inferrer` agent in silent mode:
   ```
   mode: silent
   first_run: false
   ```
   Stage the router and domain files after the agent completes — do NOT stage `docs/wiki/scan.md` (the raw deterministic dump; thousands of lines, deliberately not auto-staged): `git add docs/wiki/index.md docs/wiki/*.md && git restore --staged docs/wiki/scan.md`.
4. Run `atomic wiki mark-dirty` (best-effort, no-op when cwd is under no registered wiki root). This marks any registered wiki as having uncommitted changes since the last refresh, so the next session nudge fires. Skip silently if `atomic` is not on PATH.

`atomic signals stale` is content-based: it assembles the deterministic snapshot exactly as a scan would and compares it to `docs/wiki/scan.md`, returning exit 1 only when they actually differ. A no-op regeneration that merely bumps file mtimes stays fresh; a real shift in the project map goes stale. Treat exit 1 as an unconditional trigger, not a hint.
</signals-refresh>
6. **Commit** using a HEREDOC message.
7. **Clean up session reports** — on successful commit, delete `.claude/.scratchpad/session-reports/<branch>/`. The reports were consumed by the commit message. If the commit failed, leave them for the next attempt.
8. `git status` to confirm.

One commit per invocation. If the diff spans unrelated concerns, ask how to split.

</commit-flow>

If nothing to commit and branch has commits ahead of base, skip to the escalation step.
If nothing to commit and branch is up to date with base, stop.

## Escalation

<constraints>
If there is an open PR and the escalation requires a merge, the new commit must be pushed before `gh pr merge` so the server-side merge includes it. Push to the PR's existing branch — do not create a new PR.
</constraints>

### Push path (`push` token or user picks Push)


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

### PR path (`pr` token or user picks Open PR)


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
    Body sections: `## Summary` (1-3 bullets), `## What this solves` (1-2 sentences; skip if obvious). No test plan section. Never enumerate changed files or restate the diff — reviewers read the diff.
7. Print the PR URL.

If the working tree is dirty, stop and tell the user to commit first.

</pr-flow>

### Merge path (`merge` token or user picks Merge to base)



<merge-preflight>

1. Invoke `atomic-verify` — confirm the branch is ready before merging.
2. Determine base:
   ```
   gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null \
     || git config init.defaultBranch \
     || echo main
   ```
3. `git branch --show-current` — if on base, stop: nothing to merge.
4. `git status --porcelain` — if dirty, stop: commit or stash first.
5. **Detect open PR:**
   ```
   gh pr view --json number,state -q '.state' 2>/dev/null
   ```
   - `OPEN` → use **remote path** (preferred — closes the PR cleanly via GitHub).
   - Otherwise → **local path**.
   - If `gh` is missing or unauthed, fall through to local path with a note.

   Remote path is preferred because a local merge + push does not auto-close the PR on GitHub — it stays open as "Not merged" indefinitely. `gh pr merge` is the only way to close it cleanly.

</merge-preflight>


<merge-steps>

1. Record the feature branch name and PR number (if any).

2. **Execute the merge:**

    **Remote path** (PR open):
    1. `gh pr merge <PR#> --merge --delete-branch` — server-side merge, auto-closes the PR, removes remote branch.
    2. `git checkout <base>` then `git pull` to fast-forward local base.
    3. Record `MERGE_SHA=$(git rev-parse HEAD)`.

    **Local path** (no PR):
    1. `git checkout <base>` then `git pull`.
    2. `git merge <feature>`.
    3. Record `MERGE_SHA=$(git rev-parse HEAD)`.

3. **Re-run tests.** If tests fail:
    - Local path: ask about rolling back with `git reset --hard ORIG_HEAD`.
    - Remote path: the merge SHA is already published. Offer `git revert` instead — never force-push the base branch.

4. **Update implementation logs.** Find spec files with an `## Implementation log` section in the merged diff:
    ```bash
    git diff --name-only ORIG_HEAD..HEAD | grep '^docs/spec/.*\.md$' | while read f; do
      grep -q '^## Implementation log' "$f" && echo "$f"
    done
    ```
    For each match, append: `**Merged into <base> as <MERGE_SHA> — <date>.**` Stage and commit as a follow-up. If none match, skip.

5. **Post-merge signals refresh:**
    <signals-refresh>
Refresh project signals so Claude's map stays current for the next session.

0. **Docs-only guard.** Inspect the staged file set with `git diff --cached --name-only`. If the staged set is **empty** (e.g., in a post-merge or post-squash context where the commit already landed and nothing remains staged), skip the docs-only check and fall through to step 1 — an empty staged set does not mean all paths are documentation. If the staged set is non-empty and **every** staged path is documentation, skip the refresh entirely — do not continue to step 1. A path is documentation when it is under a `docs/` directory at any depth, OR is a top-level `README*` / `CHANGELOG*` / `CONTRIBUTING*` / `CODE_OF_CONDUCT*` / `SECURITY*` / `LICENSE*`. Any other path — source, config, build files, `CLAUDE.md`, or any bundled-artifact `.md` under `agents/`, `commands/`, `skills/`, `rules/`, or `output-styles/` — means the commit is NOT docs-only; continue to step 1. **Why:** the deterministic substrate counts per-language LOC, so a docs-only commit trips `stale` exit 1 and dispatches the inferrer for no real map change. In a config repo the artifact `.md` files are the product, so they must count as source, not docs.
1. Check `command -v atomic`. If missing, skip.
2. Run `atomic signals stale` and act on the exit code. **Why:** the staleness check also prevents a redundant refresh when the implementation phase already refreshed — a fresh stored signals file returns exit 0 and skips dispatch.
   - **exit 0** (fresh) → skip the refresh.
   - **exit 1** (stale) → refresh is mandatory. Continue to step 3. Do NOT second-guess this with `atomic signals diff`, file counts, or a judgment that "the change was small" — exit 1 means a fresh scan would produce different deterministic content than the stored signals file, and the only correct response is to refresh. Skipping it accumulates drift. The command prints how much would change and the directive; follow it.
   - **exit 2** (error, e.g. signals file missing) → report the stderr message and skip; a refresh cannot run against a missing baseline.
3. Dispatch the `atomic-wiki-inferrer` agent in silent mode:
   ```
   mode: silent
   first_run: false
   ```
   Stage the router and domain files after the agent completes — do NOT stage `docs/wiki/scan.md` (the raw deterministic dump; thousands of lines, deliberately not auto-staged): `git add docs/wiki/index.md docs/wiki/*.md && git restore --staged docs/wiki/scan.md`.
4. Run `atomic wiki mark-dirty` (best-effort, no-op when cwd is under no registered wiki root). This marks any registered wiki as having uncommitted changes since the last refresh, so the next session nudge fires. Skip silently if `atomic` is not on PATH.

`atomic signals stale` is content-based: it assembles the deterministic snapshot exactly as a scan would and compares it to `docs/wiki/scan.md`, returning exit 1 only when they actually differ. A no-op regeneration that merely bumps file mtimes stays fresh; a real shift in the project map goes stale. Treat exit 1 as an unconditional trigger, not a hint.
</signals-refresh>
    If signals regenerate, commit as a follow-up: `chore(signals): refresh after merge of <feature>`. Push on remote path.

6. **Delete local feature branch:** `git branch -d <feature>`.
7. Worktree check: `git worktree list`. If the feature branch lived in `.worktrees/<feature>/`, ask via `AskUserQuestion`:
   > Branch was checked out in worktree at `<path>`. Delete it?
   > - Yes, remove worktree
   > - No, keep it

   On Yes: find repo root via `git rev-parse --show-toplevel` on the main checkout (not the worktree). `git worktree remove <path>`. `git worktree prune`.

</merge-steps>

### Squash path (`squash` token, no `merge`, or user picks Squash branch)



<squash-preflight>

1. Determine base:
   ```
   gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null \
     || git config init.defaultBranch \
     || echo main
   ```
2. `git branch --show-current` — if on base, stop: nothing to squash.
3. `git status --porcelain` — if dirty, stop: commit or stash first.
4. Count commits: `git rev-list --count <base>..HEAD` — if only 1, stop: nothing to squash.

</squash-preflight>


<squash-steps>

1. Gather subjects (oldest-first): `SUBJECTS=$(git log <base>..HEAD --format='%s' --reverse)`.
2. **Session reports** — check for `.claude/.scratchpad/session-reports/<branch>/`. If the dir has `*.md` files, read them chronologically and pass as supplemental why-context alongside `SUBJECTS`.
3. `git reset --soft $(git merge-base HEAD <base>)` — collapse all branch commits into the index.
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
5. Invoke `atomic-commit` skill. Pre-fill a Conventional Commits message synthesized from `SUBJECTS` (plus session reports if present). Present for review, then commit via HEREDOC.
6. **Clean up session reports** — on successful commit, delete `.claude/.scratchpad/session-reports/<branch>/`. If the commit failed, leave them.
7. **Update implementation logs.** Find spec files with an `## Implementation log` section in the squashed diff:
    ```bash
    git show --name-only --pretty=format: HEAD | grep '^docs/spec/.*\.md$' | while read f; do
      grep -q '^## Implementation log' "$f" && echo "$f"
    done
    ```
    For each match, append: `**Squashed to <new-sha> — <date>.** Per-iteration SHAs above are historical (unreachable from any branch).` Stage and commit as a follow-up. If none match, skip.
8. **Post-squash signals refresh:**
    <signals-refresh>
Refresh project signals so Claude's map stays current for the next session.

0. **Docs-only guard.** Inspect the staged file set with `git diff --cached --name-only`. If the staged set is **empty** (e.g., in a post-merge or post-squash context where the commit already landed and nothing remains staged), skip the docs-only check and fall through to step 1 — an empty staged set does not mean all paths are documentation. If the staged set is non-empty and **every** staged path is documentation, skip the refresh entirely — do not continue to step 1. A path is documentation when it is under a `docs/` directory at any depth, OR is a top-level `README*` / `CHANGELOG*` / `CONTRIBUTING*` / `CODE_OF_CONDUCT*` / `SECURITY*` / `LICENSE*`. Any other path — source, config, build files, `CLAUDE.md`, or any bundled-artifact `.md` under `agents/`, `commands/`, `skills/`, `rules/`, or `output-styles/` — means the commit is NOT docs-only; continue to step 1. **Why:** the deterministic substrate counts per-language LOC, so a docs-only commit trips `stale` exit 1 and dispatches the inferrer for no real map change. In a config repo the artifact `.md` files are the product, so they must count as source, not docs.
1. Check `command -v atomic`. If missing, skip.
2. Run `atomic signals stale` and act on the exit code. **Why:** the staleness check also prevents a redundant refresh when the implementation phase already refreshed — a fresh stored signals file returns exit 0 and skips dispatch.
   - **exit 0** (fresh) → skip the refresh.
   - **exit 1** (stale) → refresh is mandatory. Continue to step 3. Do NOT second-guess this with `atomic signals diff`, file counts, or a judgment that "the change was small" — exit 1 means a fresh scan would produce different deterministic content than the stored signals file, and the only correct response is to refresh. Skipping it accumulates drift. The command prints how much would change and the directive; follow it.
   - **exit 2** (error, e.g. signals file missing) → report the stderr message and skip; a refresh cannot run against a missing baseline.
3. Dispatch the `atomic-wiki-inferrer` agent in silent mode:
   ```
   mode: silent
   first_run: false
   ```
   Stage the router and domain files after the agent completes — do NOT stage `docs/wiki/scan.md` (the raw deterministic dump; thousands of lines, deliberately not auto-staged): `git add docs/wiki/index.md docs/wiki/*.md && git restore --staged docs/wiki/scan.md`.
4. Run `atomic wiki mark-dirty` (best-effort, no-op when cwd is under no registered wiki root). This marks any registered wiki as having uncommitted changes since the last refresh, so the next session nudge fires. Skip silently if `atomic` is not on PATH.

`atomic signals stale` is content-based: it assembles the deterministic snapshot exactly as a scan would and compares it to `docs/wiki/scan.md`, returning exit 1 only when they actually differ. A no-op regeneration that merely bumps file mtimes stays fresh; a real shift in the project map goes stale. Treat exit 1 as an unconditional trigger, not a hint.
</signals-refresh>
    If signals regenerate, commit as a follow-up: `chore(signals): refresh after squash`.
9. `git status` to confirm.

</squash-steps>

### Squash + merge path (`squash merge` tokens or user picks Squash + merge)



<squash-preflight>

1. Determine base:
   ```
   gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null \
     || git config init.defaultBranch \
     || echo main
   ```
2. `git branch --show-current` — if on base, stop: nothing to squash.
3. `git status --porcelain` — if dirty, stop: commit or stash first.
4. Count commits: `git rev-list --count <base>..HEAD` — if only 1, stop: nothing to squash.

</squash-preflight>


<squash-steps>

1. Gather subjects (oldest-first): `SUBJECTS=$(git log <base>..HEAD --format='%s' --reverse)`.
2. **Session reports** — check for `.claude/.scratchpad/session-reports/<branch>/`. If the dir has `*.md` files, read them chronologically and pass as supplemental why-context alongside `SUBJECTS`.
3. `git reset --soft $(git merge-base HEAD <base>)` — collapse all branch commits into the index.
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
5. Invoke `atomic-commit` skill. Pre-fill a Conventional Commits message synthesized from `SUBJECTS` (plus session reports if present). Present for review, then commit via HEREDOC.
6. **Clean up session reports** — on successful commit, delete `.claude/.scratchpad/session-reports/<branch>/`. If the commit failed, leave them.
7. **Update implementation logs.** Find spec files with an `## Implementation log` section in the squashed diff:
    ```bash
    git show --name-only --pretty=format: HEAD | grep '^docs/spec/.*\.md$' | while read f; do
      grep -q '^## Implementation log' "$f" && echo "$f"
    done
    ```
    For each match, append: `**Squashed to <new-sha> — <date>.** Per-iteration SHAs above are historical (unreachable from any branch).` Stage and commit as a follow-up. If none match, skip.
8. **Post-squash signals refresh:**
    <signals-refresh>
Refresh project signals so Claude's map stays current for the next session.

0. **Docs-only guard.** Inspect the staged file set with `git diff --cached --name-only`. If the staged set is **empty** (e.g., in a post-merge or post-squash context where the commit already landed and nothing remains staged), skip the docs-only check and fall through to step 1 — an empty staged set does not mean all paths are documentation. If the staged set is non-empty and **every** staged path is documentation, skip the refresh entirely — do not continue to step 1. A path is documentation when it is under a `docs/` directory at any depth, OR is a top-level `README*` / `CHANGELOG*` / `CONTRIBUTING*` / `CODE_OF_CONDUCT*` / `SECURITY*` / `LICENSE*`. Any other path — source, config, build files, `CLAUDE.md`, or any bundled-artifact `.md` under `agents/`, `commands/`, `skills/`, `rules/`, or `output-styles/` — means the commit is NOT docs-only; continue to step 1. **Why:** the deterministic substrate counts per-language LOC, so a docs-only commit trips `stale` exit 1 and dispatches the inferrer for no real map change. In a config repo the artifact `.md` files are the product, so they must count as source, not docs.
1. Check `command -v atomic`. If missing, skip.
2. Run `atomic signals stale` and act on the exit code. **Why:** the staleness check also prevents a redundant refresh when the implementation phase already refreshed — a fresh stored signals file returns exit 0 and skips dispatch.
   - **exit 0** (fresh) → skip the refresh.
   - **exit 1** (stale) → refresh is mandatory. Continue to step 3. Do NOT second-guess this with `atomic signals diff`, file counts, or a judgment that "the change was small" — exit 1 means a fresh scan would produce different deterministic content than the stored signals file, and the only correct response is to refresh. Skipping it accumulates drift. The command prints how much would change and the directive; follow it.
   - **exit 2** (error, e.g. signals file missing) → report the stderr message and skip; a refresh cannot run against a missing baseline.
3. Dispatch the `atomic-wiki-inferrer` agent in silent mode:
   ```
   mode: silent
   first_run: false
   ```
   Stage the router and domain files after the agent completes — do NOT stage `docs/wiki/scan.md` (the raw deterministic dump; thousands of lines, deliberately not auto-staged): `git add docs/wiki/index.md docs/wiki/*.md && git restore --staged docs/wiki/scan.md`.
4. Run `atomic wiki mark-dirty` (best-effort, no-op when cwd is under no registered wiki root). This marks any registered wiki as having uncommitted changes since the last refresh, so the next session nudge fires. Skip silently if `atomic` is not on PATH.

`atomic signals stale` is content-based: it assembles the deterministic snapshot exactly as a scan would and compares it to `docs/wiki/scan.md`, returning exit 1 only when they actually differ. A no-op regeneration that merely bumps file mtimes stays fresh; a real shift in the project map goes stale. Treat exit 1 as an unconditional trigger, not a hint.
</signals-refresh>
    If signals regenerate, commit as a follow-up: `chore(signals): refresh after squash`.
9. `git status` to confirm.

</squash-steps>



<merge-preflight>

1. Invoke `atomic-verify` — confirm the branch is ready before merging.
2. Determine base:
   ```
   gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null \
     || git config init.defaultBranch \
     || echo main
   ```
3. `git branch --show-current` — if on base, stop: nothing to merge.
4. `git status --porcelain` — if dirty, stop: commit or stash first.
5. **Detect open PR:**
   ```
   gh pr view --json number,state -q '.state' 2>/dev/null
   ```
   - `OPEN` → use **remote path** (preferred — closes the PR cleanly via GitHub).
   - Otherwise → **local path**.
   - If `gh` is missing or unauthed, fall through to local path with a note.

   Remote path is preferred because a local merge + push does not auto-close the PR on GitHub — it stays open as "Not merged" indefinitely. `gh pr merge` is the only way to close it cleanly.

</merge-preflight>


<merge-steps>

1. Record the feature branch name and PR number (if any).

2. **Execute the merge:**

    **Remote path** (PR open):
    1. `gh pr merge <PR#> --merge --delete-branch` — server-side merge, auto-closes the PR, removes remote branch.
    2. `git checkout <base>` then `git pull` to fast-forward local base.
    3. Record `MERGE_SHA=$(git rev-parse HEAD)`.

    **Local path** (no PR):
    1. `git checkout <base>` then `git pull`.
    2. `git merge <feature>`.
    3. Record `MERGE_SHA=$(git rev-parse HEAD)`.

3. **Re-run tests.** If tests fail:
    - Local path: ask about rolling back with `git reset --hard ORIG_HEAD`.
    - Remote path: the merge SHA is already published. Offer `git revert` instead — never force-push the base branch.

4. **Update implementation logs.** Find spec files with an `## Implementation log` section in the merged diff:
    ```bash
    git diff --name-only ORIG_HEAD..HEAD | grep '^docs/spec/.*\.md$' | while read f; do
      grep -q '^## Implementation log' "$f" && echo "$f"
    done
    ```
    For each match, append: `**Merged into <base> as <MERGE_SHA> — <date>.**` Stage and commit as a follow-up. If none match, skip.

5. **Post-merge signals refresh:**
    <signals-refresh>
Refresh project signals so Claude's map stays current for the next session.

0. **Docs-only guard.** Inspect the staged file set with `git diff --cached --name-only`. If the staged set is **empty** (e.g., in a post-merge or post-squash context where the commit already landed and nothing remains staged), skip the docs-only check and fall through to step 1 — an empty staged set does not mean all paths are documentation. If the staged set is non-empty and **every** staged path is documentation, skip the refresh entirely — do not continue to step 1. A path is documentation when it is under a `docs/` directory at any depth, OR is a top-level `README*` / `CHANGELOG*` / `CONTRIBUTING*` / `CODE_OF_CONDUCT*` / `SECURITY*` / `LICENSE*`. Any other path — source, config, build files, `CLAUDE.md`, or any bundled-artifact `.md` under `agents/`, `commands/`, `skills/`, `rules/`, or `output-styles/` — means the commit is NOT docs-only; continue to step 1. **Why:** the deterministic substrate counts per-language LOC, so a docs-only commit trips `stale` exit 1 and dispatches the inferrer for no real map change. In a config repo the artifact `.md` files are the product, so they must count as source, not docs.
1. Check `command -v atomic`. If missing, skip.
2. Run `atomic signals stale` and act on the exit code. **Why:** the staleness check also prevents a redundant refresh when the implementation phase already refreshed — a fresh stored signals file returns exit 0 and skips dispatch.
   - **exit 0** (fresh) → skip the refresh.
   - **exit 1** (stale) → refresh is mandatory. Continue to step 3. Do NOT second-guess this with `atomic signals diff`, file counts, or a judgment that "the change was small" — exit 1 means a fresh scan would produce different deterministic content than the stored signals file, and the only correct response is to refresh. Skipping it accumulates drift. The command prints how much would change and the directive; follow it.
   - **exit 2** (error, e.g. signals file missing) → report the stderr message and skip; a refresh cannot run against a missing baseline.
3. Dispatch the `atomic-wiki-inferrer` agent in silent mode:
   ```
   mode: silent
   first_run: false
   ```
   Stage the router and domain files after the agent completes — do NOT stage `docs/wiki/scan.md` (the raw deterministic dump; thousands of lines, deliberately not auto-staged): `git add docs/wiki/index.md docs/wiki/*.md && git restore --staged docs/wiki/scan.md`.
4. Run `atomic wiki mark-dirty` (best-effort, no-op when cwd is under no registered wiki root). This marks any registered wiki as having uncommitted changes since the last refresh, so the next session nudge fires. Skip silently if `atomic` is not on PATH.

`atomic signals stale` is content-based: it assembles the deterministic snapshot exactly as a scan would and compares it to `docs/wiki/scan.md`, returning exit 1 only when they actually differ. A no-op regeneration that merely bumps file mtimes stays fresh; a real shift in the project map goes stale. Treat exit 1 as an unconditional trigger, not a hint.
</signals-refresh>
    If signals regenerate, commit as a follow-up: `chore(signals): refresh after merge of <feature>`. Push on remote path.

6. **Delete local feature branch:** `git branch -d <feature>`.
7. Worktree check: `git worktree list`. If the feature branch lived in `.worktrees/<feature>/`, ask via `AskUserQuestion`:
   > Branch was checked out in worktree at `<path>`. Delete it?
   > - Yes, remove worktree
   > - No, keep it

   On Yes: find repo root via `git rev-parse --show-toplevel` on the main checkout (not the worktree). `git worktree remove <path>`. `git worktree prune`.

</merge-steps>

## Interactive path

If no escalation token was present in args, after the commit completes, ask via `AskUserQuestion`:

> Committed. Ship further?
> - Done — just the commit
> - Push to remote
> - Open PR
> - Merge to base
> - Squash + merge

Route the answer to the matching escalation path above. "Done" stops immediately.

<git-safety>
- Stage explicitly by name (`git add <path>`), never `git add -A`. **Why:** `-A` can accidentally include secrets or untracked binaries.
- Use relative paths for `git add` based on the current working directory. **Why:** absolute paths and `git -C` can silently stage files outside the intended scope.
- Run each `git` command as a separate Bash call. **Why:** chaining with `&&` makes it impossible to inspect intermediate state and hides partial failures.
- On pre-commit hook failure: fix the root cause, re-stage, and create a new commit — never `--amend`. **Why:** amending after a hook failure modifies the PREVIOUS commit, which may destroy unrelated work.
- Keep force-push off the base branch. If a rollback is needed, use `git revert` so the bad SHA stays in history. **Why:** force-pushing rewrites shared history, breaking every collaborator's checkout.
</git-safety>

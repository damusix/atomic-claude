---
description: Create isolated worktree at .worktrees/<branch>/ with new branch. Auto-detects setup (npm/cargo/pip/go), verifies baseline tests, reports ready. Skips if already in a worktree.
---

## 1. Parse arguments

`$ARGUMENTS` can be any combination of:

- An explicit kebab-case branch name (`a-z`, `0-9`, `-`, `/`).
- A spec reference: a `@`-prefixed path, or a bare path ending in `.md` that exists on disk. Common locations: `docs/spec/*.md`, `docs/design/*.md`.
- Free-form prose describing the work (a brief).

Tokenize on whitespace and classify each token:

- Starts with `@` or ends in `.md` → **spec candidate**. Strip the leading `@`. Verify the file exists relative to repo root; if not, downgrade to prose.
- Matches `^[a-z0-9][a-z0-9/-]*$` and is not a spec → **branch candidate**.
- Anything else (including tokens with spaces, capitals, punctuation when joined) → **prose**.

Resolve the branch name:

- If exactly one branch candidate exists, use it.
- Else if a spec was found, derive the branch from the spec basename minus `.md` (e.g. `docs/spec/cron-workflow.md` → `cron-workflow`).
- Else if only prose exists, ask the user for a branch name (single `AskUserQuestion`, offering a kebab-case slug derived from the first ~6 prose words as the recommended option). Do not invent a branch silently.
- Else (no args at all) refuse:

    ```
    usage: /worktree-start <branch-name> [@spec.md] [brief...]
    branch names: kebab-case, no spaces, only a-z 0-9 - /
    ```

Validate the final branch name against `^[a-z0-9][a-z0-9/-]*$`. If it fails, refuse with the usage line above.

Remember the resolved spec path (if any) and the joined prose (if any) — both are surfaced in step 9 so follow-up work picks them up. This command does not act on them itself.

## 2. Detect existing isolation

Run in parallel:

```bash
git rev-parse --git-dir
git rev-parse --git-common-dir
git rev-parse --show-superproject-working-tree
git branch --show-current
```

Resolve both `--git-dir` and `--git-common-dir` to real paths with `pwd -P` (via subshells). Submodule guard: if `--show-superproject-working-tree` returns a non-empty path, treat this as a normal repo — not a worktree.

## 3. Already isolated? Stop

If `GIT_DIR != GIT_COMMON` and not a submodule:

```
already isolated at <pwd> on branch <branch>. Skipping creation.
```

Stop. No further steps.

## 4. Verify .worktrees/ is gitignored

```bash
git check-ignore -q .worktrees
```

If exit code is non-zero (not ignored):

- Append `.worktrees/` to `.gitignore` (create the file at repo root if missing).
- Invoke the `atomic-commit` skill.
- Stage `.gitignore` explicitly by path.
- Commit with message `chore: gitignore .worktrees/`.

## 5. Carry forward an in-context spec or design

A worktree branches from `HEAD`. Anything uncommitted in the source working tree does not follow — including the spec or design doc the user just had you write. If the implementer subagent later reads `docs/spec/<topic>.md` from the worktree and the file isn't there, the loop fails before iteration 1.

Detect carry-over candidates:

- A spec path was passed in arguments (step 1), and `git status --porcelain -- <path>` reports it as untracked or modified.
- The current conversation just produced a `docs/spec/*.md` or `docs/design/*.md` file (the LLM knows this from context), and `git status --porcelain` reports it as untracked or modified, and its basename matches or is closely related to the branch name.

For each candidate, ask the user once with `AskUserQuestion`:

```
Spec `<path>` is uncommitted. Commit it before creating the worktree so
the branch carries it forward?
```

Options: `commit now (recommended)` / `skip`. On `commit now`:

- Invoke the `atomic-commit` skill for the message.
- Stage the file explicitly by path. Do not `git add -A`.
- Commit on the current branch (typically `main`).

On `skip`, continue without committing. The user accepts that the file won't be in the worktree until they sync it manually.

Only spec / design files in `docs/spec/` and `docs/design/` qualify here. Other uncommitted changes are out of scope — the user can `/commit-only` separately.

## 6. Verify branch does not already exist

```bash
git rev-parse --verify <branch>
```

If this succeeds (exit 0), refuse:

```
branch <name> already exists. pick a different name or checkout existing.
```

Stop.

## 7. Create the worktree

```bash
git worktree add .worktrees/<branch> -b <branch>
```

If this fails with a permission or sandbox error, print:

```
sandbox blocked worktree creation. working in place.
```

Stop — do not run setup or tests, do not continue.

## 8. Auto-detect and run setup

Run all detection from inside `.worktrees/<branch>/`. Check files in this order:

- `pnpm-lock.yaml` exists alongside `package.json` → `pnpm install`
- `yarn.lock` exists alongside `package.json` → `yarn install`
- `package.json` exists → `npm install`
- `Cargo.toml` exists → `cargo build`
- `requirements.txt` exists → `pip install -r requirements.txt`
- `poetry.lock` exists alongside `pyproject.toml` → `poetry install`
- `pyproject.toml` exists → `pip install -e .`
- `go.mod` exists → `go mod download`
- None matched → skip setup, note as skipped

If the setup command fails with a network or permission error, note `setup skipped (sandboxed or no network)` and continue.

## 9. Run baseline tests

Detect the test command from inside `.worktrees/<branch>/`:

- `pnpm-lock.yaml` + `package.json` with `test` script → `pnpm test`
- `yarn.lock` + `package.json` with `test` script → `yarn test`
- `package.json` with `test` script → `npm test`
- `Cargo.toml` → `cargo test`
- `pytest.ini` present, or `pyproject.toml` contains `[tool.pytest` → `pytest`
- `go.mod` → `go test ./...`
- None matched → skip, note as skipped

If tests fail: list each failure, then ask whether to proceed or investigate before continuing.

## 10. Report

```
Worktree: .worktrees/<branch>/
Branch:   <branch>
Spec:     <path> | (none)
Brief:    <joined prose> | (none)
Setup:    <command run> | skipped (no manifest) | skipped (sandboxed)
Baseline: <N> tests pass | <N> failures | skipped
Ready.
```

If a spec or brief was passed, note that the user likely wants to continue with `/subagent-implementation` (for spec) or `/atomic-plan` (for an unrefined brief) next — surface that one-line hint after the report.

If step 5 committed a carry-forward spec or design, also report the new commit SHA on the source branch so the user can see what was preserved.

---

## Rules

- Never chain `cd` with `&&` for git commands — use separate Bash calls.
- Only call `git worktree add` when step 3 confirms this is not already an isolated workspace.
- Commits permitted by this command: (a) the `.gitignore` patch in step 4; (b) spec / design carry-forward in step 5. Both go through the `atomic-commit` skill. No other commits.
- Step 5 only touches files under `docs/spec/` and `docs/design/`. Other uncommitted changes are out of scope.
- Do not create worktrees at global or home-directory paths — always `.worktrees/` inside the project root.
- Do not run setup commands that require network access if sandboxed — detect via first failure and report.

---
description: Create isolated worktree at .worktrees/<branch>/ with new branch. Auto-detects setup (npm/cargo/pip/go), verifies baseline tests, reports ready. Skips if already in a worktree.
---

## 1. Argument check

If `$ARGUMENTS` is empty, or contains spaces, or contains characters other than `a-z`, `0-9`, `-`, and `/`, refuse:

```
usage: /worktree-start <branch-name>
branch names: kebab-case, no spaces, only a-z 0-9 - /
```

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

## 5. Verify branch does not already exist

```bash
git rev-parse --verify <branch>
```

If this succeeds (exit 0), refuse:

```
branch <name> already exists. pick a different name or checkout existing.
```

Stop.

## 6. Create the worktree

```bash
git worktree add .worktrees/<branch> -b <branch>
```

If this fails with a permission or sandbox error, print:

```
sandbox blocked worktree creation. working in place.
```

Stop — do not run setup or tests, do not continue.

## 7. Auto-detect and run setup

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

## 8. Run baseline tests

Detect the test command from inside `.worktrees/<branch>/`:

- `pnpm-lock.yaml` + `package.json` with `test` script → `pnpm test`
- `yarn.lock` + `package.json` with `test` script → `yarn test`
- `package.json` with `test` script → `npm test`
- `Cargo.toml` → `cargo test`
- `pytest.ini` present, or `pyproject.toml` contains `[tool.pytest` → `pytest`
- `go.mod` → `go test ./...`
- None matched → skip, note as skipped

If tests fail: list each failure, then ask whether to proceed or investigate before continuing.

## 9. Report

```
Worktree: .worktrees/<branch>/
Branch:   <branch>
Setup:    <command run> | skipped (no manifest) | skipped (sandboxed)
Baseline: <N> tests pass | <N> failures | skipped
Ready.
```

---

## Rules

- Never chain `cd` with `&&` for git commands — use separate Bash calls.
- Only call `git worktree add` when step 3 confirms this is not already an isolated workspace.
- The only commit allowed is the optional `.gitignore` patch in step 4, created via the `atomic-commit` skill.
- Do not create worktrees at global or home-directory paths — always `.worktrees/` inside the project root.
- Do not run setup commands that require network access if sandboxed — detect via first failure and report.

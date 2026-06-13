{{- define "worktree-setup" -}}
<worktree-setup>

## Detect existing isolation

Run in parallel:

```bash
GIT_DIR=$(cd "$(git rev-parse --git-dir)" 2>/dev/null && pwd -P)
GIT_COMMON=$(cd "$(git rev-parse --git-common-dir)" 2>/dev/null && pwd -P)
SUPERPROJECT=$(git rev-parse --show-superproject-working-tree 2>/dev/null)
CURRENT_BRANCH=$(git branch --show-current 2>/dev/null)
```

Submodule guard: if `--show-superproject-working-tree` returns a non-empty path, treat as a normal repo — not a worktree.

If `$GIT_DIR != $GIT_COMMON` (and not a submodule) → already in a worktree. Print:

```
already isolated at <pwd> on branch <branch>. Skipping creation.
```

Continue in place with the current working tree. Skip all steps below.

## Decide whether to create (ask-if-unspecified / auto-create)

**Interactive mode (ask-if-unspecified):** if the caller has not already decided, ask via `AskUserQuestion`:

```
Significant work ahead. Use an isolated worktree?
- Yes, new branch → create .worktrees/<derived-name>/
- No, work in place
```

On `No`: continue in place. Skip all steps below.

**Hands-off mode (auto-create):** skip the question and proceed to branch resolution.

## Resolve the branch name

The branch name is passed by the caller (e.g. a topic slug derived from the spec or task). It must match `^[a-z0-9][a-z0-9/-]*$`. If no name is available, derive one: kebab-case slug of the first ~6 words of the task description.

## Verify .worktrees/ is gitignored

```bash
git check-ignore -q .worktrees
```

If exit code is non-zero (not ignored):

- Append `.worktrees/` to `.gitignore` (create at repo root if missing).
- Invoke the `atomic-commit` skill.
- Stage `.gitignore` explicitly by path.
- Commit with message `chore: gitignore .worktrees/`.

## Carry forward an in-context spec or design (optional)

A worktree branches from `HEAD`. Uncommitted spec or design files in the source working tree do not follow — if the implementer subagent reads `docs/spec/<topic>.md` from the worktree and the file isn't there, the loop fails before iteration 1.

Detect carry-over candidates:

- A spec path was passed by the caller, and `git status --porcelain -- <path>` reports it as untracked or modified.
- The current conversation produced a `docs/spec/*.md` or `docs/design/*.md` that is untracked or modified, and its basename matches or is closely related to the branch name.

For each candidate (interactive mode only — skip silently in hands-off mode): ask via `AskUserQuestion`:

```
Spec `<path>` is uncommitted. Commit it before creating the worktree so
the branch carries it forward?
```

Options: `commit now (recommended)` / `skip`. On `commit now`:

- Invoke the `atomic-commit` skill for the message.
- Stage the file explicitly by path. Do not `git add -A`.
- Commit on the current branch (typically `main`).

In hands-off mode: if a spec candidate is found, commit it automatically without prompting (the caller authorized autonomy).

Only `docs/spec/` and `docs/design/` files qualify here.

## Verify branch does not already exist

```bash
git rev-parse --verify <branch>
```

If this succeeds (exit 0), refuse:

```
branch <name> already exists. pick a different name or checkout existing.
```

Stop.

## Create the worktree

```bash
git worktree add .worktrees/<branch> -b <branch>
```

If this fails with a permission or sandbox error, print:

```
sandbox blocked worktree creation. working in place.
```

Continue in place — do not run setup or tests.

## Auto-detect and run setup

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

## Run baseline tests

Detect the test command from inside `.worktrees/<branch>/`:

- `pnpm-lock.yaml` + `package.json` with `test` script → `pnpm test`
- `yarn.lock` + `package.json` with `test` script → `yarn test`
- `package.json` with `test` script → `npm test`
- `Cargo.toml` → `cargo test`
- `pytest.ini` present, or `pyproject.toml` contains `[tool.pytest` → `pytest`
- `go.mod` → `go test ./...`
- None matched → skip, note as skipped

If tests fail: in interactive mode, list each failure, then ask whether to proceed or investigate before continuing. In hands-off mode, list failures in `STATE.md` and proceed (the reviewer will catch regressions).

## Report

```
Worktree: .worktrees/<branch>/
Branch:   <branch>
Setup:    <command run> | skipped (no manifest) | skipped (sandboxed)
Baseline: <N> tests pass | <N> failures | skipped
Ready.
```

</worktree-setup>
{{- end -}}

# Worktrees

Where a new worktree goes, how to create and enter it, and how to clean it up. If the location looks wrong, see `./resolution.md`.

## Getting the location

Read the `worktrees` field of `atomic where --json`, or the `worktrees` line of `atomic workflow resolve`. Both come from the same helper, keyed by harness. Pass `--harness claude|omp|codex` to either command to ask for a harness other than the detected one.

| `worktrees` preference | Claude Code | OMP, Codex, unknown harness |
|------------------------|-------------|-----------------------------|
| `auto` (the default) | `.claude/worktrees` | `<state-root>/worktrees` |
| a repository-relative path | that path | that path |
| `off` | no worktree | no worktree |

The value is repository-relative inside the repository and absolute outside it. `atomic where --json` reports `""` only for `off`. When the repository level is skipped (no repository, or an ambiguous state root), `atomic workflow resolve` reports the raw preference, `auto` by default, unmapped.

## Create and enter

The same steps apply on every harness:

1. Run `git worktree add <dir>/<branch> -b <branch>`, where `<dir>` is the resolved value. This pins the branch name and bases it on the current `HEAD`. Source: https://git-scm.com/docs/git-worktree
2. Enter the worktree through the harness's worktree-entry capability, or change into it when the harness has none.

With `worktrees: off`, skip both steps and work in the current checkout.

`atomic repo init` ignores `<state-root>/worktrees/`, and `.claude/worktrees/` when `.claude/` exists or a Claude target is enrolled. An explicit path needs its own `.gitignore` line, or the linked worktree shows up as an untracked nested repository.

## Clean up

Cleanup follows `/git-cleanup` and the worktree cleanup prompt: archive the scratchpad bundle, enumerate with `git worktree list`, and prompt before removal. Never enumerate with a path glob, since the location differs by harness and preference.

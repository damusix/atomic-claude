{{define "worktree-cleanup-prompt"}}`git worktree list`. Never glob a directory; see the `atomic-git-discipline` reference `references/worktrees.md`. If the feature branch is checked out in a linked worktree (an entry other than the first, main one), ask the user:
   > Branch was checked out in worktree at `<path>`. Delete it?
   > - Yes, remove worktree
   > - No, keep it

   On Yes: find repo root via `git rev-parse --show-toplevel` on the main checkout (not the worktree). Archive the worktree's scratchpad bundle(s) first — `atomic scratchpad list --repo <path> --json`, then `atomic scratchpad archive <slug> --repo <path>` for each — before removing anything, so a bundle retired here isn't destroyed unarchived. `git worktree remove <path>`. `git worktree prune`.{{- end}}

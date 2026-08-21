{{define "worktree-cleanup-prompt"}}Worktree check: `git worktree list`. If the feature branch lived in a linked worktree (typically `.claude/worktrees/<feature>/`), ask via `AskUserQuestion`:
   > Branch was checked out in worktree at `<path>`. Delete it?
   > - Yes, remove worktree
   > - No, keep it

   On Yes: find repo root via `git rev-parse --show-toplevel` on the main checkout (not the worktree). If `atomic` is on PATH, archive the worktree's scratchpad bundle(s) first — `atomic scratchpad list --repo <path> --json`, then `atomic scratchpad archive <slug> --repo <path>` for each — before removing anything, so a bundle retired here isn't destroyed unarchived; if `atomic` is absent, say so in one line and remove without archiving. `git worktree remove <path>`. `git worktree prune`.{{- end}}

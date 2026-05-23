{{define "worktree-cleanup-prompt"}}Worktree check: `git worktree list`. If the feature branch lived in `.worktrees/<feature>/`, ask via `AskUserQuestion`:
   > Branch was checked out in worktree at `<path>`. Delete it?
   > - Yes, remove worktree
   > - No, keep it

   On Yes: find repo root via `git rev-parse --show-toplevel` on the main checkout (not the worktree). `git worktree remove <path>`. `git worktree prune`.{{- end}}

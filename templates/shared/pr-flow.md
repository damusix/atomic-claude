{{define "pr-flow"}}
## Prereqs

- `command -v gh` — if missing: tell user to install (`brew install gh` / `winget install --id GitHub.cli` / https://cli.github.com/) then `gh auth login`. Stop.
- `gh auth status` — if unauthed: tell user `gh auth login`. Stop.

## Steps

1. Invoke the `atomic-review` skill. PR title and body follow that tone.
2. `git branch --show-current`. If on base branch, stop.
3. Determine base: `gh repo view --json defaultBranchRef -q .defaultBranchRef.name`.
4. `git log <base>..HEAD --oneline` + `git diff <base>...HEAD --stat` (parallel) to read what's shipping.
5. Existing PR? `gh pr view --json url 2>/dev/null` → if yes, print URL, stop.
6. Push if needed: no upstream → `git push -u origin <branch>`. Behind → `git push`.
7. `gh pr create --title "<imperative, ≤70 chars>" --body` via HEREDOC. Sections: `## Summary` (1-3 bullets), `## Why` (skip if obvious), `## Test plan` (checklist).
8. Print PR URL.

No AI bylines anywhere. No `--draft` unless user asked. No commits — if working tree dirty, stop and tell user to run `/commit-only` first.{{- end}}

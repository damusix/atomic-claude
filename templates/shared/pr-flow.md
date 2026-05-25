{{define "pr-flow"}}
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
    Body sections: `## Summary` (1-3 bullets), `## Why` (skip if obvious), `## Test plan` (checklist).
7. Print the PR URL.

If the working tree is dirty, stop and tell the user to commit first.

</pr-flow>{{- end}}

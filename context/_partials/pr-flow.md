{{define "pr-flow"}}
<pr-flow>

Invoke the `atomic-git-discipline` skill; its PR section owns title and body.

1. `git branch --show-current` — if on base branch, stop.
2. Determine base: `gh repo view --json defaultBranchRef -q .defaultBranchRef.name`.
3. Read what is shipping: `git log <base>..HEAD --oneline` + `git diff <base>...HEAD --stat` (parallel).
4. Check for existing PR: `gh pr view --json url 2>/dev/null` — if one exists, print its URL and stop.
5. Push if needed: `git push -u origin <branch>` (no upstream) or `git push` (behind).
6. Screenshots, when the change has a rendered surface (a UI, a docs-site page, terminal output). Collect the PNGs the session produced (the task scratchpad, `tmp/`, files the user supplied) or capture them now against the built result. The skill's body carries one `![<caption>](<local path>)` line per image; pass the same path once per file as `--attach '<local path>#<caption>'`. Two read-only preconditions gate the upload: `gh --version` is 2.99.0 or newer (older `gh` has no `--attach`; print `brew upgrade gh` and ship the body without the image lines rather than blocking), and `gh api repos/<owner>/<repo> --jq .permissions.push` prints `true` (uploads need push access on the base repo). Images are capped at 10 MB and 50 per command.
7. Create the PR:
    ```
    gh pr create --title "<imperative, ≤70 chars>" --body <HEREDOC> [--attach '<path>#<caption>' ...]
    ```
    Body sections: `## Summary` (1-3 bullets), `## What this solves` (1-2 sentences; skip if obvious), then the screenshot lines. No test plan section. Never enumerate changed files or restate the diff — reviewers read the diff. No AI bylines or attribution anywhere in title or body: no "Generated with Claude Code" footer, no `Co-Authored-By: Claude` trailer, no session links. `gh` rewrites each body reference to the uploaded `user-attachments` URL. A non-zero exit after the URL prints means the PR exists but an upload failed: run `gh pr edit <url> --attach '<path>#<caption>'` for each failed file, never a second `gh pr create`.
8. Print the PR URL.

If the working tree is dirty, stop and tell the user to commit first.

</pr-flow>{{- end}}

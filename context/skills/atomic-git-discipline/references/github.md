# GitHub pull requests

Use after `shipping.md` when GitHub is the target. Check `gh auth status` and use `gh repo view` to resolve the repository and base. Find existing requests with `gh pr view --json url,state,baseRefName,headRefName,headRefOid,headRepository,headRepositoryOwner`. Reuse an open PR only if the base and head repository and branch match.

For an authorized create or update, push the reviewed local SHA to the verified head remote and branch without force. If no matching PR exists, use `gh pr create --base <base> --head <head> --title <title> --body <body>`.

For a rendered change, the message skill specifies screenshot captions. With GitHub CLI 2.99.0 or later and push permission on the base repository, attach images using `--attach '<path>#<caption>'`. If an upload fails after creation, inspect the request and use `gh pr edit <url> --attach '<path>#<caption>'` rather than creating another. If upload is unavailable, omit unhosted image links and report the omission.

Read the final PR state and `headRefOid`. For a create or update, verify that `headRefOid` matches the reviewed local SHA before reporting success. Merge only with separate authorization, and inspect the resulting state.

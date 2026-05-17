---
description: Soft-undo the last commit. Restores its changes as staged, leaves working tree intact. Refuses if HEAD is already pushed to a tracked remote.
---

Reverses one commit without losing the work. Equivalent to `git reset --soft HEAD~1` with safety guards.


## Pre-flight


```bash
git rev-parse --is-inside-work-tree
```


If not in a git repo, stop:


```
not a git repo. /undo-commit requires a git repository.
```


Verify HEAD has a parent (can't undo the initial commit):


```bash
git rev-parse HEAD^ 2>/dev/null
```


If this fails: `refused: HEAD is the initial commit. nothing to undo.`


Verify HEAD is not a merge commit (undoing a merge is destructive in a non-obvious way — bounce to the user):


```bash
git rev-list --parents -n 1 HEAD | awk '{print NF-1}'
```


If output `>= 2`: `refused: HEAD is a merge commit. use git reset --hard ORIG_HEAD manually if you understand the consequences.`


Verify HEAD is not already on the remote (cannot soft-undo a published commit without rewriting public history):


```bash
git rev-parse --abbrev-ref --symbolic-full-name @{u} 2>/dev/null
```


If a tracking branch exists, check whether HEAD is reachable from upstream:


```bash
git merge-base --is-ancestor HEAD @{u}
```


If exit 0 (HEAD already on remote): `refused: HEAD is already pushed to <upstream>. /undo-commit only undoes local commits. use git revert <sha> for a published commit.`


## Confirm


Show what's about to be undone:


```bash
git log -n 1 --format='  %h %s%n  %an, %ar%n%n  Files:%n' HEAD
git diff --stat HEAD^..HEAD
```


Ask via `AskUserQuestion`:


```
Undo this commit? Changes will be moved back to the staging area.
- Yes, undo
- No, keep it
```


On `No`: print `kept.` and stop. No action.


## Action


On `Yes`:


```bash
git reset --soft HEAD~1
```


## Report


Print:


```
undone. HEAD now at <new-sha-short>.
prior commit's changes are staged. inspect with: git status, git diff --cached
to re-commit: /commit-only
to discard: git restore --staged <paths> then git restore <paths>
```


## Rules


- `--soft` only. Never `--mixed`, never `--hard`. The user's work stays in the index where it's visible and recoverable.
- Refuse on initial commit, merge commit, or pushed HEAD. These cases need human judgment.
- One commit per invocation. To undo multiple, run again — the per-invocation confirm forces deliberate steps.
- Print every git command before running it.
- Do not stage, do not commit, do not push. After `git reset --soft`, control returns to the user.
- This is the only command that mutates history. Treat it as destructive (axiom 3) — never act without the per-item Yes.

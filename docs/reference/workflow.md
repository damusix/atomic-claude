# Workflow


The canonical lifecycle:

1. **`/atomic-plan`** — collaborative. You and Claude produce a checkpoint table written to `docs/design/<topic>.md` (brainstorm / rationale) or `docs/spec/<topic>.md` (implementation contract). Human-facing, Mermaid diagrams allowed. This is the human approval gate.

2. **`/subagent-implementation`** — autonomous from the spec. The orchestrator reads `docs/spec/`, writes a thin `BRIEF.md` + `STATE.md` + `FOLLOWUPS.md` to `.claude/.scratchpad/`, then drives an implement → review loop using fresh-context subagents. Each reviewer `VERDICT: PASS` triggers a commit before the next iteration. Non-blocking findings (🟡 risks, 🔵 nits, ❓ questions) accumulate in `FOLLOWUPS.md` and get dispositioned with you at finalize — fix now, file an issue, or drop. Nothing gets silently dropped just because the iteration passed.

3. **Ship** — pick the verb that matches your situation:

| Command | What it does |
|---------|-------------|
| `/commit-only` | Stage and commit. Does not push. |
| `/commit-and-push` | Commit, then push. No PR, no merge. Trunk-based counterpart to `/commit-and-pr`. |
| `/commit-and-pr` | Commit, push, open PR via `gh`. |
| `/push-only` | Push existing commits to the remote. No commit, no PR. |
| `/pr-only` | Open PR for existing commits. |
| `/merge-to-main` | Merge current branch into base, no squash. |
| `/commit-and-merge` | `/commit-only` then `/merge-to-main`. |
| `/squash-only` | Squash all branch commits into one (no merge). |
| `/squash-and-merge` | `git merge --squash` from base, single commit, delete branch. |
| `/commit-and-squash` | `/commit-only` then `/squash-only`. |

All merge and squash commands invoke `atomic-verify` before touching the base, re-run tests on the merged tip, and prompt to delete the worktree if the branch came from `.worktrees/`.

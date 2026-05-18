# Commands


| Command | What it does |
|---------|-------------|
| `/atomic-setup` | Bootstrap the current repo for atomic conventions. Audits .gitignore, docs/ layout, CLAUDE.md; proposes only what's missing. Never overwrites. |
| `/atomic-plan` | Collaborative plan → checkpoint table in `docs/design/` or `docs/spec/`. |
| `/atomic-compress <file>` | Compress a prose Markdown file into atomic style. Backs up original as `<file>.original.md`. |
| `/pressure-test [<topic> \| @<path-to.md>]` | Socratic challenger session for design decisions. Pressure-tests assumptions, surfaces contradictions, forces fuzzy maybes into yes/no — through questions only, never producing code or artifacts. Pairs with `/atomic-plan` as a pre-approval gate. |
| `/subagent-implementation` | Orchestrate implement → review subagent loop until task is complete. |
| `/worktree-start <name>` | Create isolated worktree at `.worktrees/<name>/`, new branch, auto-detected project setup. |
| `/git-cleanup [<name>]` | Scan stale git state (worktrees, branches, optional remote) via `atomic-git-scout`, present indexed report, ask before deleting. Local-only by default; asks about remote. |
| `/commit-only` | Stage and commit. Delegates message format to `atomic-commit` skill. |
| `/commit-and-push` | Commit then push. No PR, no merge. Trunk-based counterpart to `/commit-and-pr`. |
| `/commit-and-pr` | Commit, push, open PR via `gh`. |
| `/commit-and-merge` | Commit then merge to base branch. |
| `/commit-and-squash` | Commit then squash all branch commits. |
| `/push-only` | Push existing commits to the remote. No commit, no PR. Trunk-based counterpart to `/pr-only`. |
| `/pr-only` | Open PR for the current branch (commits already exist). |
| `/merge-to-main` | Merge current branch into base, no squash. |
| `/squash-only` | Squash all branch commits into one (no merge). |
| `/squash-and-merge` | Squash-merge into base, delete branch. |
| `/review-branch` | Dispatch `atomic-reviewer` once on `<base>..HEAD` for a pre-PR / pre-merge branch review. No spec required, no orchestration loop. |
| `/undo-commit` | Soft-undo the last commit (`reset --soft HEAD~1`). Refuses if HEAD is a merge commit, the initial commit, or already pushed. |
| `/remind-me <duration> <text>` | Schedule a reminder. Writes a reminder file and creates a one-shot cron that fires `/follow-up due <id>` at the given time. Degrades to file-only if `CronCreate` is unavailable. |
| `/follow-up [due <id>]` | Review pending reminders. Bare: indexed list + done/snooze/reschedule actions. Cron-fired: surfaces the specific reminder and waits for user response. |
| `/initialize-signals` | Bootstrap signals for a project that has never had them. Interactive, idempotent. Requires `atomic` binary. |
| `/refresh-signals` | Deliberate on-demand refresh of existing signals. |
| `/documentation` | Update or create project docs (README, CLAUDE.md, docs/spec/, docs/design/) after significant changes. |
| `/report-issue` | Open a GitHub issue via `gh` against the user's current repo. Auto-detects bug report vs. feature request. |
| `/report-issue-with-atomic` | Open a GitHub issue against the **atomic-claude repo itself** (`damusix/atomic-claude`). For bugs or feature requests with the installed config, not the user's current project. |
| `/watch-ci [<target>]` | Spawn a background Haiku subagent to watch CI for the current branch/PR/run. Provider auto-detected from signals. |
| `/atomic-claude-merge` | Merge `~/.claude/CLAUDE.md.atomic-proposed` (produced by `atomic claude install/update`) into the live `~/.claude/CLAUDE.md` via the `atomic-claude-merger` agent. |
| `/session-report [<slug>]` | Capture what changed and why for the current branch's session. Writes a timestamped markdown file to `.claude/.scratchpad/session-reports/<branch>/`. Read by the next commit-message-generating ship verb as supplemental *why*-context, then deleted. Opt-in; does not auto-fire. |

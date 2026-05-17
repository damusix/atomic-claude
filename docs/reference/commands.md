# Commands


| Command | What it does |
|---------|-------------|
| `/atomic-setup` | Bootstrap the current repo for atomic conventions. Audits .gitignore, docs/ layout, claude.md; proposes only what's missing. Never overwrites. |
| `/atomic-plan` | Collaborative plan → checkpoint table in `docs/design/` or `docs/spec/`. |
| `/atomic-compress <file>` | Compress a prose Markdown file into atomic style. Backs up original as `<file>.original.md`. |
| `/subagent-implementation` | Orchestrate implement → review subagent loop until task is complete. |
| `/worktree-start <name>` | Create isolated worktree at `.worktrees/<name>/`, new branch, auto-detected project setup. |
| `/git-cleanup [<name>]` | Scan stale git state (worktrees, branches, optional remote) via `atomic-git-scout`, present indexed report, ask before deleting. Local-only by default; asks about remote. |
| `/commit-only` | Stage and commit. Delegates message format to `atomic-commit` skill. |
| `/commit-and-pr` | Commit, push, open PR via `gh`. |
| `/commit-and-merge` | Commit then merge to base branch. |
| `/commit-and-squash` | Commit then squash all branch commits. |
| `/pr-only` | Open PR for the current branch (commits already exist). |
| `/merge-to-main` | Merge current branch into base, no squash. |
| `/squash-only` | Squash all branch commits into one (no merge). |
| `/squash-and-merge` | Squash-merge into base, delete branch. |
| `/remind-me <duration> <text>` | Schedule a reminder. Writes a reminder file and creates a one-shot cron that fires `/follow-up due <id>` at the given time. Degrades to file-only if `CronCreate` is unavailable. |
| `/follow-up [due <id>]` | Review pending reminders. Bare: indexed list + done/snooze/reschedule actions. Cron-fired: surfaces the specific reminder and waits for user response. |
| `/initialize-signals` | Bootstrap signals for a project that has never had them. Interactive, idempotent. Requires `atomic` binary. |
| `/refresh-signals` | Deliberate on-demand refresh of existing signals. |
| `/documentation` | Update or create project docs (README, claude.md, docs/spec/, docs/design/) after significant changes. |
| `/report-issue` | Open a GitHub issue via `gh`. Auto-detects bug report vs. feature request. |
| `/watch-ci [<target>]` | Spawn a background Haiku subagent to watch CI for the current branch/PR/run. Provider auto-detected from signals. |
| `/atomic-claude-merge` | Merge `~/.claude/CLAUDE.md.atomic-proposed` (produced by `atomic claude install/update`) into the live `~/.claude/CLAUDE.md` via the `atomic-claude-merger` agent. |

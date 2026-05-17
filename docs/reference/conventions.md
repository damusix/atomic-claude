# Conventions


- Atomic style applies to Claude's TUI replies, not to source files, comments, or documentation. Source files follow the codebase's own conventions.
- `claude.md` in any project should hold only meaningful context for that codebase — not general reminders, not duplicated tool lists. Keep it lean.
- No AI bylines in commit messages or PR descriptions.
- The scratchpad (`.claude/.scratchpad/`) is LLM working memory — ephemeral, gitignored, not for human consumption. Durable decisions go in `docs/`.
- Tests verify intent, not behavior. A test that still passes when the business logic changes is wrong.
- `tmp/` is for throwaway experiments and ad-hoc verification scripts. Not a scratch directory for checked-in work.
- When `/subagent-implementation` is about to start significant work (anything with three or more checkpoints), it prompts whether to use an isolated worktree. Already inside `.worktrees/*`? It skips the prompt.

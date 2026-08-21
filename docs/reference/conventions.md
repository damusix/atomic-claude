# Conventions


- Atomic style applies to Claude's TUI replies, not to source files, comments, or documentation. Source files follow the codebase's own conventions.
- `CLAUDE.md` in any project should hold only meaningful context for that codebase — not general reminders, not duplicated tool lists. Keep it lean.
- No AI bylines in commit messages or PR descriptions.
- The scratchpad (`.claude/.scratchpad/<slug>/`) is LLM working memory — ephemeral, gitignored, not for human consumption. Durable decisions go in `docs/`. It is a slug-keyed bundle, not a dated one: `atomic scratchpad new <slug> --purpose <plan|implement|fix|diagnose|review>` creates or extends it, seeding only what that purpose still needs, so a task worked across several phases accumulates in one directory instead of a new one per phase per date. `atomic scratchpad path <slug>` prints its location, `list [--archived]` enumerates bundles, and `archive <slug>` retires one.
- Session reports, reminders, and archived bundles live outside the repository, under `~/.atomic/<project-key>/` — one home per clone, shared by every worktree of it. `atomic where --json` resolves the current branch's report path, the reports root, the reminders directory, and the archive root for this project; nothing should construct those paths by hand. Run `atomic migrate --show-log` for the history of how this and other state layouts changed.
- `.claude/` paths throughout these docs describe the default (Claude Code) harness. Under another agent the same layout resolves beneath that harness's directory (`.pi/.scratchpad/`, `.pi/project/`, …) via harness detection — experimental; see [concepts](concepts.md).
- Tests verify intent, not behavior. A test that still passes when the business logic changes is wrong.
- `tmp/` is for throwaway experiments and ad-hoc verification scripts. Not a scratch directory for checked-in work.
- When `/subagent-implementation` is about to start significant work, it prompts whether to use an isolated worktree. Already inside `.claude/worktrees/*` (or any linked worktree)? It skips the prompt.

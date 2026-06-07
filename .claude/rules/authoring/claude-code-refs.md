---
paths:
  - "agents/**"
  - "templates/agents/**"
  - "skills/**"
  - "commands/**"
  - "templates/commands/**"
  - "templates/shared/**"
  - "output-styles/**"
  - "rules/**"
---

# Claude Code official documentation references

Canonical Claude Code docs to consult when designing or editing artifacts in this repo. Fetch on demand via WebFetch — these are upstream sources of truth, not snapshots.

| Topic | URL | When to consult |
|-------|-----|-----------------|
| Memory / rules | https://code.claude.com/docs/en/memory | CLAUDE.md scopes (managed / user / project / local), load order, `@path` imports, `.claude/rules/*.md` with `paths:` frontmatter for glob-scoped instructions (`**/*.ts`, `src/**/*.{ts,tsx}`), `claudeMdExcludes`, auto-memory in `~/.claude/projects/<project>/memory/`, `/memory` command, `InstructionsLoaded` hook for debugging. |
| Run agents in parallel | https://code.claude.com/docs/en/agents | Comparison of subagents vs agent-view vs agent-teams vs worktrees; when to use each parallelization approach. |
| Sub-agents | https://code.claude.com/docs/en/sub-agents | Custom subagent definition, frontmatter, tool restriction, context isolation, `Agent` tool, preload-skills, fork mode. Source of truth for editing `agents/*.md`. |
| Skills | https://code.claude.com/docs/en/skills | `SKILL.md` structure, frontmatter (description, allowed-tools, disable-model-invocation, user-invocable, context:fork, paths, hooks), `$ARGUMENTS`, `${CLAUDE_SKILL_DIR}`, dynamic context injection via `` !`cmd` ``, skill lifecycle/compaction, `skillOverrides`. Note: custom commands have been merged into skills — `.claude/commands/*.md` still works but `.claude/skills/<name>/SKILL.md` is preferred. |
| Commands | https://code.claude.com/docs/en/commands | Built-in command reference and bundled skills (`/loop`, `/debug`, `/simplify`, `/batch`, `/claude-api`); workflow-stage groupings. |
| Hooks (reference) | https://code.claude.com/docs/en/hooks | Full event schema, JSON input/output, exit codes, command/HTTP/MCP/prompt/agent hook handler types, async hooks, matcher patterns. |
| Hooks guide | https://code.claude.com/docs/en/hooks-guide | Practical recipes, setup walkthroughs, `if` field filtering, common patterns (formatting, notifications, validation). |
| Tools reference | https://code.claude.com/docs/en/tools-reference | Exact tool names (PascalCase: `Bash`, `Read`, `Edit`, `Grep`, `Glob`, `Agent`, `Skill`, `Monitor`, `EnterWorktree`, `CronCreate`, etc.), per-tool behavior, permission-rule format `ToolName(specifier)`. |
| Worktrees | https://code.claude.com/docs/en/worktrees | `claude --worktree`, default path `.claude/worktrees/<name>/`, `worktree.baseRef` setting, `.worktreeinclude`, subagent `isolation: worktree`, cleanup rules, `WorktreeCreate`/`WorktreeRemove` hooks for non-git VCS. |
| Scheduled tasks | https://code.claude.com/docs/en/scheduled-tasks | `/loop` bundled skill, `CronCreate`/`CronList`/`CronDelete` tools, 5-field cron expressions, jitter, 7-day expiry, session-scoped vs Routines/Desktop/GitHub Actions for durable scheduling. |
| Headless / programmatic | https://code.claude.com/docs/en/headless | `claude -p` non-interactive mode, `--bare` for reproducible CI runs, `--output-format json/stream-json`, `--allowedTools`, `--permission-mode`, `--continue`/`--resume`, `--append-system-prompt`. |

## Usage

When editing or adding artifacts (commands, agents, skills, hooks, output styles), verify behavior against the upstream doc above before relying on memory or training data. Claude Code semantics change between versions — these URLs are the source of truth.

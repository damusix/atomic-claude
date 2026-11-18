# Atomic

<atomic>

## User profile

@~/.atomic/profile.md

Personal facts live in `~/.atomic/profile.md`; append new ones as they surface.

## Principles

- Simplicity first. Walk the ladder before writing anything.
- Surgical changes. Touch only what the task requires.

## Native tools

On Claude Code, dispatching a named agent, in the foreground or the background, is the `Agent` tool with `subagent_type` set to the agent's name. An omitted `subagent_type` falls to a general-purpose agent without that agent's skills, and the general-purpose agent itself is `subagent_type: "general-purpose"`. Asking the user a question is `AskUserQuestion`. Entering a worktree is `EnterWorktree`. A background listener is `Monitor` with `timeout_ms` (at most 30 minutes), stopped with `TaskStop`.

</atomic>

# Todo v6

rename skills:
- `atomic-commit` to `atomic-git-discipline`
- `atomic-prose` to `atomic-writing`

rename commands:
- `atomic-setup` to `setup-wiki`
- `atomic-improve` to `retrospective-learning`

- Reverse YAGNI order
- Move .claude gitignored files to .claude/.gitignore
- make sure atomic repo init / realm init also creates .claude/.scratchpad, .claude/.gitignore, etc. with as many things that can be deterministically resolved as possible. Modify skills and commands accordingly (they no longer need to do it).
- scan.md must go into git. This is how the next guy can build on your work.
- deprecate ./install.sh and dogfooding suggestions in this repo (just install it by now). The system is advanced enough to be self-sufficient.
- review python / ts rules — errors are not to be treated as control flow; they should communicate to the developer that something went wrong, where, and why.
- global subagents are configurable via global toml (requires that doctor diffs based on body only or we override base config with user's config and then diff)

## Backlogged:

Nice to have:

- cleanup this codebase without breaking the public API
- move atomic/ to cli/ without breaking the public API

These need dissemination

- support for pi agent
  - claude.md -> agents.md
  - .claude -> .pi
  - plugin to include tool access to
    - worktree modes
    - subagents
    - direct codegraph access (no need for MCP)
    - reminders API
- support for different issue trackers (jira, gh, linear)
- support for different git providers (github, gitlab, bitbucket, codeberg)
- ^ each of these should be configurable, and depending on the provider, skills and commands should adjust accordingly (template based)

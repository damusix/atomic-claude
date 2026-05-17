# Contributing


This repo authors its artifacts at the top level (`agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`). Those are the shapes you'd copy into `~/.claude/` for install. But Claude Code only auto-loads artifacts from a project's `.claude/` directory, so editing a top-level file doesn't take effect in this repo's own session.


`scripts/link-local.sh` closes that loop. It symlinks each top-level artifact dir into `.claude/`, so the repo dogfoods its own config:


    ./scripts/link-local.sh


Idempotent (`ln -sfn`). Re-run any time you add a new agent, command, skill, output-style, or rule. The generated `.claude/{agents,commands,output-styles,skills,rules}/` symlinks are gitignored; they're machine-specific and exist only to make Claude Code load the work-in-progress sources.


Workflow when adding or editing an artifact:


1. Edit the source under `agents/`, `commands/`, `skills/<name>/`, `output-styles/`, or `rules/<lang>/`.
2. Run `./scripts/link-local.sh` if you added a *new* file (existing files are already linked).
3. Restart Claude Code (or start a new session) to pick up the change.
4. Test in this repo's session. That's the dogfood. If it doesn't feel right here, it won't feel right anywhere.


Do not commit anything under `.claude/agents/`, `.claude/commands/`, `.claude/output-styles/`, `.claude/skills/`, or `.claude/rules/`. Those are generated. The `.claude/docs/` and `.claude/settings.local.json` files are real and tracked.

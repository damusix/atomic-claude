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


## Install the git hooks


Run this once after cloning:


    make hooks


It sets `git config core.hooksPath .githooks` so the repo's own `.githooks/pre-commit` runs on every commit. The hook checks the staged diff and, when a source artifact under `agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, or `CLAUDE.md` is part of the commit, regenerates the embedded bundle and re-stages the output. Without the hook, those commits will pass locally but fail CI on the "Verify bundle is committed" step.


`make hooks-uninstall` restores the default `.git/hooks/` path if you ever want to opt out.


This is a git hook, not a Claude Code hook. The two share the word "hook" and nothing else. `atomic hooks install` registers a session-start handler that injects pending reminders into Claude's context at session open. The git pre-commit hook is local automation for keeping the embedded artifact bundle in sync with its source. They live in different layers and never interact.


## Why the bundle has to be regenerated


The `atomic` binary embeds the artifact bundle at build time via `go:embed`. The source files live at the repo root (`agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, `CLAUDE.md`). The build copies those into `atomic/internal/embedded/bundle/` and writes a snapshot at `atomic/internal/embedded/manifest.go`. Both the copies and the manifest are tracked in git, and CI enforces parity: after running `go generate`, any diff fails the build.


Translation: if you edit a source artifact and forget to regenerate, the embedded bundle is stale, the manifest is wrong, and CI rejects the commit. The pre-commit hook eliminates that class of failure. When the hook is installed you only need to remember to commit; the bundle rides along automatically.


If you prefer not to install the hook, the rule still applies. Run `make bundle` (which delegates to `make -C atomic bundle`) before any commit that touches a source artifact, then stage everything under `atomic/internal/embedded/`.

# Contributing


This repo authors its artifacts at the top level (`agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`). Those are the shapes you'd copy into `~/.claude/` for install. But Claude Code only auto-loads artifacts from a project's `.claude/` directory, so editing a top-level file doesn't take effect in this repo's own session.


After cloning, run the one-shot setup:


    make dev-setup


That target installs the git hooks (`make hooks`) and symlinks the top-level artifact directories into `.claude/` (`make link`) in a single shot. The link step closes the dogfooding loop — Claude Code only auto-loads artifacts from `.claude/`, so without the symlinks, editing a top-level file would not take effect in this repo's own session.


`make link` runs `scripts/link-local.sh`. It is idempotent (`ln -sfn`). Re-run it any time you add a *new* file under `agents/`, `commands/`, `skills/<name>/`, `output-styles/`, or `rules/<lang>/`. Existing files stay linked through the directory symlink and need no extra step. The generated `.claude/{agents,commands,output-styles,skills,rules}/` symlinks are gitignored; they are machine-specific and exist only to make Claude Code load the work-in-progress sources.


Workflow when adding or editing an artifact:


1. Edit the source under `agents/`, `templates/commands/` (commands are rendered — see the templates section below), `skills/<name>/`, `output-styles/`, or `rules/<lang>/`.
2. Run `make link` if you added a *new* file (existing files are already linked).
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


## Artifact templates


Slash commands live under `commands/` in their final form, but those files are generated. The source of truth is `templates/`. Edit a command by editing its template, not the rendered file.

The pipeline has two stages: `make render` reads `templates/` and writes `commands/`, then `make bundle` reads `commands/` and writes the embedded bundle. The pre-commit hook chains both stages whenever a `templates/` file is staged.

`templates/commands/<verb>.md` holds each verb's orchestration — frontmatter, prereqs, step headers, rules. Bodies that recur across verbs live once in `templates/shared/<name>.md` and get composed via Go `text/template` directives like `{{ template "commit-flow" . }}`. The shared set today is five big partials (`commit-flow`, `pr-flow`, `merge-flow`, `squash-flow`, `push-flow`) plus five small partials inside them (`doc-impact`, `doc-impact-why`, `signals-gate`, `base-resolution`, `worktree-cleanup-prompt`).

Partials are pure fragments. No `dict` function, no `{{ if }}` conditionals, no variant flags. If a fragment needs to appear in some consumers but not others, make it its own micro-partial. The two-level taxonomy (big partials for whole flows, small partials for blocks reused across flows) keeps the set scannable.

Two rules the renderer enforces:

- **Edit templates, not rendered output.** A direct edit to `commands/<verb>.md` is overwritten on the next render. The pre-commit hook silently re-renders on any staged `templates/` change.
- **No orphans.** `commands/<verb>.md` without a matching `templates/commands/<verb>.md` causes `make render` to halt with a non-zero exit and an error naming both remediation paths. Adding a new command means dropping the file in `templates/commands/`, never directly in `commands/`. Removing a command means deleting both files.

The contract for this system lives in `docs/spec/artifact-templates.md`. Day-to-day working conventions live in `.claude/skills/atomic-cli-contrib/SKILL.md` §10, which auto-fires when you mention adding a command, editing a partial, or running the renderer.

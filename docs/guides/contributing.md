# Contributing


## Setup

After cloning, run:

```bash
make dev-setup
```

This does two things:

1. **Installs git hooks** — the pre-commit hook keeps the embedded artifact bundle in sync with source files
2. **Symlinks artifacts into `.claude/`** — Claude Code only loads artifacts from `.claude/`, so without the symlinks, your edits to top-level files would not take effect in this repo's own session


## Day-to-day workflow

1. Edit the source under `agents/`, `templates/commands/`, `skills/<name>/`, `output-styles/`, or `rules/<lang>/`
2. Run `make link` if you added a new file (existing files stay linked through the directory symlink)
3. Restart Claude Code to pick up the change
4. Test in this repo's session — that is the dogfood loop

Do not edit files under `.claude/agents/`, `.claude/commands/`, etc. Those are generated via symlinks. The `.claude/docs/` and `.claude/settings.local.json` files are real and tracked.


## Git hooks

The pre-commit hook has three stages:

1. **Render** — when any `templates/` file is staged, regenerates `commands/` and re-stages the output
2. **Bundle** — when any source artifact is staged, regenerates the embedded bundle and re-stages it
3. **Follow-ups** — when any followup entry is staged, regenerates `INDEX.md`

Without the hook, commits will pass locally but fail CI on the "Verify bundle is committed" step.

Install or uninstall the hook manually:

```bash
make hooks           # install
make hooks-uninstall # remove
```

::: tip This is a git hook, not a Claude Code hook
`atomic hooks install` is a separate thing — it registers a session-start handler for reminders. The git pre-commit hook is build automation. They share the word "hook" and nothing else.
:::


## Why the bundle matters

The `atomic` binary embeds the artifact bundle at build time via `go:embed`. When you edit a source artifact (`agents/`, `commands/`, `skills/`, etc.), the embedded copy and its manifest need to match. CI checks parity — any drift fails the build.

The pre-commit hook handles this automatically. If you prefer not to use the hook, run `make bundle` before any commit that touches a source artifact, then stage everything under `atomic/internal/embedded/`.


## Command templates

Slash commands under `commands/` are generated — do not edit them directly. The source of truth lives in `templates/`.

The pipeline:

```
templates/commands/<verb>.md  →  make render  →  commands/<verb>.md
templates/shared/<name>.md   (reusable partials composed via Go text/template)
```

**Shared partials** contain the bodies that recur across verbs. Five big partials (`commit-flow`, `pr-flow`, `merge-flow`, `squash-flow`, `push-flow`) handle the main flows. Five small partials (`doc-impact`, `doc-impact-why`, `signals-gate`, `base-resolution`, `worktree-cleanup-prompt`) handle shared fragments within them.

Two rules:

- **Edit templates, not rendered output.** A direct edit to `commands/<verb>.md` is overwritten on the next render.
- **No orphans.** A rendered file without a matching template causes `make render` to fail with an error explaining the fix.

Adding a new command means creating `templates/commands/<verb>.md`. Removing a command means deleting both the template and the rendered file.

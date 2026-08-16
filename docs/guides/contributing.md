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

Every artifact that installs to a user's `~/.claude/` lives under `context/`: `agents/`, `commands/`, `skills/`, `rules/`, `output-styles/`, and `CLAUDE.md`. Nothing else in the repo ships. `templates/` stays at the root because it is build-time source that renders into `context/` and never installs.

1. Edit the source under `templates/commands/`, `templates/agents/`, `context/skills/<name>/`, `context/output-styles/`, or `context/rules/<lang>/`. Both `context/commands/` and `context/agents/` are rendered output, not source.
2. Run `make link` if you added a new file (linking is per-file, so existing files keep their symlinks but a new file needs its own)
3. Restart Claude Code to pick up the change
4. Test in this repo's session — that is the dogfood loop

Do not edit files under `.claude/agents/`, `.claude/commands/`, etc. Those are generated via symlinks. The `.claude/settings.local.json` file is real and tracked.

To let agents answer structural questions about this codebase from the real symbol graph while you work, run `atomic code index` once from the repo root. After that, `atomic code explore "<question>"` returns a digest of the relevant symbols and call edges, which is a fast way to find where a command, agent, or CLI verb is wired. Run `atomic code sync` after significant changes to keep the index current.


## Git hooks

The pre-commit hook has three stages:

1. **Render** — when any `templates/` file is staged, regenerates `context/commands/` and `context/agents/` and re-stages the output
2. **Follow-ups** — when any followup entry is staged, regenerates `INDEX.md`
3. **Frontend** — when any `atomic/internal/serve/frontend/` file (outside `dist/`) is staged, rebuilds the committed `dist/` and re-stages it

Without the hook, commits will pass locally but fail CI on the "Verify render is committed" or "Verify frontend dist is committed" steps.

Install or uninstall the hook manually:

```bash
make hooks           # install
make hooks-uninstall # remove
```

::: tip This is a git hook, not a Claude Code hook
`atomic hooks install` is a separate thing — it registers a session-start handler for reminders. The git pre-commit hook is build automation. They share the word "hook" and nothing else.
:::


## The embedded bundle

The `atomic` binary embeds `context/` at build time via `go:embed`. The embed directive cannot reach a parent directory and will not follow a symlink, so `context/` is mirrored into `atomic/internal/embedded/bundle/` before it can be embedded.

That mirror, and the `manifest.go` beside it, are build artifacts — gitignored, never committed:

```
context/**  →  make bundle  →  atomic/internal/embedded/{bundle/**, manifest.go}  →  go:embed
```

`build`, `test`, and `vet` all depend on the `bundle` target, so `make` regenerates it for you. CI and goreleaser run `go generate ./...` for the same reason. A bare `go build` on a fresh clone that skips generation fails to compile with `pattern bundle: no matching files found` — run `make -C atomic bundle` and it clears.


## The serve frontend

`atomic serve`'s browser UI is a React + TypeScript SPA in a Bun workspace at `atomic/internal/serve/frontend/`. Bun is the package manager, bundler, and test runner — no npm, Vite, or Jest. Conventions (LogosDX data layer, Ark UI primitives, component layout) live in `frontend/CLAUDE.md`.

The built `dist/` is committed and embedded into the binary via `go:embed`, so `go build` never needs Bun. The pipeline:

```
frontend/src/**  →  make frontend  →  frontend/dist/**  →  go:embed
```

Run `bun test` from `frontend/` for the component suite. The pre-commit hook's frontend stage rebuilds `dist/` when frontend sources are staged; CI's "Verify frontend dist is committed" gate fails on drift, same pattern as render/bundle.


## Command and agent templates

Both `context/commands/` and `context/agents/` are generated — do not edit them directly. The source of truth lives in `templates/`. The renderer walks each kind listed in `templaterender.renderedKinds` (`commands`, `agents`).

The pipeline:

```
templates/commands/<verb>.md  →  make render  →  context/commands/<verb>.md
templates/agents/<name>.md    →  make render  →  context/agents/<name>.md
templates/shared/<name>.md   (reusable partials composed via Go text/template)
```

**Shared partials** contain the bodies that recur across files, in one pool both kinds draw from. Command partials cover the main flows (`commit-flow`, `pr-flow`, `merge-flow`, `squash-flow`, `push-flow`) and shared fragments within them (`doc-impact`, `signals-gate`, `base-resolution`, `worktree-cleanup-prompt`, `worktree-setup`, `staleness-check`, `report-issue-privacy`, `git-safety`). Agent partials use an `agent-` prefix (`agent-atomic-voice`, `agent-code-intel`, `agent-comment-discipline`, `agent-implementer-workflow`, `agent-search-tooling`, `agent-shared-rules`, `agent-signals-output`, `agent-tdd-signals`, `agent-where`, `agent-yagni`). Every agent template composes at least one partial — `agent-atomic-voice` at minimum — with `atomic-implementer` pulling the largest set via `agent-implementer-workflow`.

Two rules:

- **Edit templates, not rendered output.** A direct edit to `context/commands/<verb>.md` or `context/agents/<name>.md` is overwritten on the next render.
- **No orphans.** A rendered file without a matching template causes `make render` to fail with a kind-aware error explaining the fix.

Adding a new command or agent means creating the template under `templates/commands/` or `templates/agents/`. Removing one means deleting both the template and the rendered file.

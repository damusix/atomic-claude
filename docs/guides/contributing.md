# Contributing


## Setup

After cloning, install the git hooks:

```bash
make hooks
```

To run your changes, install the binary and its artifacts the way any user does:

```bash
make -C atomic build     # builds bin/atomic from your working tree
bin/atomic claude install
```

That writes the artifacts into `~/.claude/`, so a Claude Code session picks them up wherever you are. Rerun it after editing anything under `context/`.


## Day-to-day workflow

Everything that installs lives under `context/`, and it lives there exactly once:

```
context/
  CLAUDE.md        agents/     commands/
  skills/          rules/      output-styles/
  _partials/       ← composed into commands and agents; never installs
```

1. Edit the artifact in `context/`. There is no separate template to keep in sync.
2. Run `make -C atomic build && bin/atomic claude install` to pick the change up.
3. Restart Claude Code.

A command or agent may pull in a shared block with `{{ template "<name>" . }}`, which resolves against `context/_partials/`. Expansion happens on the way into the embedded bundle, so the file you edit is the source and the only copy. `_partials/` sits outside the artifact kind directories the mirror walks, which is why it never ships.

To let agents answer structural questions about this codebase from the real symbol graph while you work, run `atomic code index` once from the repo root. After that, `atomic code explore "<question>"` returns a digest of the relevant symbols and call edges, which is a fast way to find where a command, agent, or CLI verb is wired. Run `atomic code sync` after significant changes to keep the index current.


## Git hooks

The pre-commit hook has two stages:

1. **Follow-ups** — when any followup entry is staged, regenerates `INDEX.md`
2. **Frontend** — when any `atomic/internal/serve/frontend/` file (outside `dist/`) is staged, rebuilds the committed `dist/` and re-stages it

There is no render or bundle stage. Artifacts are committed in source form and expanded at build time, so nothing generated needs staging.

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

That mirror, and the `manifest.go` beside it, are build artifacts — gitignored, never committed. Partial expansion happens in the same pass, so this is the only generation step in the repo:

```
context/**  →  make bundle  →  atomic/internal/embedded/{bundle/**, manifest.go}  →  go:embed
   ↑ committed source              ↑ gitignored, expanded
```

`build`, `test`, and `vet` all depend on the `bundle` target, so `make` regenerates it for you. CI and goreleaser run `go generate ./...` for the same reason. A bare `go build` on a fresh clone that skips generation fails to compile with `pattern bundle: no matching files found` — run `make -C atomic bundle` and it clears.


## The serve frontend

`atomic serve`'s browser UI is a React + TypeScript SPA in a Bun workspace at `atomic/internal/serve/frontend/`. Bun is the package manager, bundler, and test runner — no npm, Vite, or Jest. Conventions (LogosDX data layer, Ark UI primitives, component layout) live in `frontend/CLAUDE.md`.

The built `dist/` is committed and embedded into the binary via `go:embed`, so `go build` never needs Bun. The pipeline:

```
frontend/src/**  →  make frontend  →  frontend/dist/**  →  go:embed
```

Run `bun test` from `frontend/` for the component suite. The pre-commit hook's frontend stage rebuilds `dist/` when frontend sources are staged; CI's "Verify frontend dist is committed" gate fails on drift, same pattern as render/bundle.


## Shared partials

A command or agent can pull in a block that recurs across files, rather than repeating it:

```
context/commands/<verb>.md   ─┐
                              ├─  {{ template "<name>" . }}  →  context/_partials/<name>.md
context/agents/<name>.md     ─┘
```

Both kinds draw from one pool, so a partial defined once is callable from either. Command partials cover the main flows (`commit-flow`, `pr-flow`, `merge-flow`, `squash-flow`, `push-flow`) and the fragments inside them (`doc-impact`, `signals-gate`, `base-resolution`, `worktree-cleanup-prompt`, `worktree-setup`, `staleness-check`, `report-issue-privacy`, `git-safety`). Agent partials carry an `agent-` prefix (`agent-atomic-voice`, `agent-code-intel`, `agent-comment-discipline`, `agent-implementer-workflow`, `agent-search-tooling`, `agent-shared-rules`, `agent-signals-output`, `agent-tdd-signals`, `agent-where`, `agent-yagni`). Every agent composes at least `agent-atomic-voice`; `atomic-implementer` pulls the largest set through `agent-implementer-workflow`.

Expansion happens once, in `make bundle`, on the way into the embedded bundle. Nothing writes a rendered copy back into `context/`, so an artifact exists in exactly one place and a partial edit reaches every consumer on the next build.

Two things to know:

- **Only commands and agents expand.** Skills, rules, output styles, and `CLAUDE.md` are copied through byte-for-byte, so a literal `{{` in their prose is safe.
- **An undefined partial fails the build.** A directive naming a partial that does not exist stops `make bundle` rather than shipping an artifact with a hole in it.

Adding a command or agent means adding one file under `context/commands/` or `context/agents/`. Removing one means deleting that file.

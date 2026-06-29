# Signals workflow

Signals are [context engineering](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents) for your repo: the project's working knowledge, kept as files the agent reads rather than facts you re-type each session. They teach Claude the shape of your project so it stops guessing. Instead of hallucinating build commands or inventing framework conventions, Claude reads two files that describe what is actually in the repo.

Run `/refresh-signals` to generate (or update) them:

- **`docs/wiki/scan.md`** — machine-generated facts: directory tree, manifests, languages, lockfile presence. Produced by `atomic signals scan`.
- **`docs/wiki/index.md`** — inferred meaning: framework, build/test/lint commands, architectural style, domain index. Produced by the `atomic-signals-inferrer` agent.

Both files live in `docs/wiki/`, are committed, and auto-load into every Claude session via `@`-refs. The `atomic-signals-inferrer` agent keeps them fresh. Three trigger points: `/refresh-signals` on demand; the implementation loop (`/subagent-implementation`, `/autopilot`) at finalize, scoped to the task's SHA range (primary); and ship commands (`/commit` and related verbs) as an ad-hoc fallback for real-code commits — docs-only commits (README, CHANGELOG, `docs/` tree) are skipped, and a freshness check prevents double-dispatch after the loop already ran.

Requires the `atomic` binary. Without it, a degraded tree-only fallback runs instead.

The signals files are a navigable markdown graph. The inferrer writes each path citation as a plain backtick path, then runs `atomic signals linkify` as its final step to render every one that resolves on disk into a relative link to the file it names. Open `docs/wiki/` in Obsidian or any markdown server and click through the router into its domain files and out to the source. The linkifier is deterministic and idempotent, and a rendered `[text](path)` link is a plain markdown link, not an `@`-reference, so it stays inert until something reads it.


## Steering the inferrer

The inferrer makes its best guess from the scan, but it can get things wrong — especially with monorepos, polyglot projects, or unconventional naming.

Create `docs/wiki/CLAUDE.md` to provide explicit hints. The inferrer reads this file before writing `docs/wiki/index.md` and treats its content as ground truth. When steering contradicts the scan, steering wins.


### When to use steering

- The inferrer detected the wrong framework
- You have git submodules and want the inferrer to treat the repo as one project
- Two directories are one logical domain but got split
- A directory looks like a domain but is generated code or vendored
- The inferrer guessed the wrong build or test command
- You want to exclude paths from domain classification


### Format

Plain markdown. No required structure — the inferrer reads it as natural language. Headings help organize:

```markdown
# Signals steering

## Framework
This is a NestJS monorepo managed by Turborepo.

## Project structure
This repo uses git submodules. Treat the root as one project.
Do not recurse into submodules or create domains for them.

## Domains
- src/billing/ and src/payments/ are one domain ("payments")
- src/internal-tools/ is scratch code, not a real domain
- packages/ contains shared libraries — one domain, not one per package

## Build
- Build: pnpm turbo build
- Test: pnpm test:ci (not pnpm test — that starts watch mode)
- Lint: pnpm lint

## Ignore for domains
- vendor/
- .git/modules/
```


### How it works

1. `/refresh-signals` dispatches the `atomic-signals-inferrer` agent
2. The agent runs `atomic signals scan` to produce the deterministic file, then reads it + `docs/wiki/CLAUDE.md` (if present)
3. Steering directives override inference — if steering says "this is NestJS", the inferrer writes NestJS regardless of what `package.json` implies
4. When the project has been indexed with `atomic code index`, the agent also reads real import and call edges from the symbol graph to corroborate domain boundaries. Files that call each other heavily cluster together regardless of directory layout; filename heuristics are the fallback when no index is present
5. The inferrer writes `docs/wiki/index.md` (and domain files at `docs/wiki/<domain>.md`)
6. On the next `/refresh-signals`, changes to steering take effect immediately


### Bootstrap

`/atomic-setup` creates `docs/wiki/CLAUDE.md` if it does not exist. Uncomment and edit the sections you need. Delete sections you do not.

The file is committed along with the rest of `docs/wiki/`.


## .signalsignore

A separate mechanism from steering. `.signalsignore` (at repo root) controls which **tracked files** are excluded from the deterministic scan.

The scan uses `git ls-files` as its source, so anything in `.gitignore` is already excluded. `.signalsignore` is for committed files you still want excluded or flagged.

Two modes per line:

| Prefix | What happens | Use for |
|--------|-------------|---------|
| _(none)_ | Fully excluded — path does not appear in tree | Vendored deps, checked-in fixtures, large data files |
| `+` | Flagged as generated — appears in tree but inferrer skips it | Build output, protobuf generated code, lockfiles |

One glob per line. Blank lines and `#` comments are ignored. Same syntax as `.gitignore`.

```
# Committed but excluded from scan
fixtures/large-dataset.json
third_party/**

# In tree but inferrer skips for domain content
+*.pb.go
+generated/**
+dist/**
```

**Use `.signalsignore`** when tracked paths should be excluded from the scan or flagged as generated.

**Use `docs/wiki/CLAUDE.md`** when you want to tell the inferrer something it cannot derive from the scan.

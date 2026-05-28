# Signals workflow

Signals teach Claude the shape of your project so it stops guessing. Instead of hallucinating build commands or inventing framework conventions, Claude reads two files that describe what is actually in the repo.

Run `/refresh-signals` to generate (or update) them:

- **`deterministic-signals.md`** — machine-generated facts: directory tree, manifests, languages, lockfile presence. Produced by `atomic signals scan`.
- **`signals.md`** — inferred meaning: framework, build/test/lint commands, architectural style, domain index. Produced by the `atomic-signals-inferrer` agent.

Both files live in `.claude/project/`, are gitignored, and auto-load into every Claude session via `@`-refs. The `atomic-signals-inferrer` agent keeps them fresh — `/refresh-signals` dispatches it on demand, and ship commands dispatch it silently when source files change.

Requires the `atomic` binary. Without it, a degraded tree-only fallback runs instead.


## Steering the inferrer

The inferrer makes its best guess from the scan, but it can get things wrong — especially with monorepos, polyglot projects, or unconventional naming.

Create `.claude/project/signals-steering.md` to provide explicit hints. The inferrer reads this file before writing `signals.md` and treats its content as ground truth. When steering contradicts the scan, steering wins.


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
2. The agent runs `atomic signals scan` to produce the deterministic file, then reads it + `signals-steering.md` (if present)
3. Steering directives override inference — if steering says "this is NestJS", the inferrer writes NestJS regardless of what `package.json` implies
4. The inferrer writes `signals.md` (and domain files on large repos)
5. On the next `/refresh-signals`, changes to steering take effect immediately


### Bootstrap

`/atomic-setup` creates a commented blank at `.claude/project/signals-steering.md` if it does not exist. Uncomment and edit the sections you need. Delete sections you do not.

The file is gitignored by default. If your team wants shared steering, move it to a committed location and `@`-reference it.


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

**Use `signals-steering.md`** when you want to tell the inferrer something it cannot derive from the scan.

# Signals workflow


The signals workflow keeps Claude aware of the current shape of a project without hallucination. Run `/refresh-signals` to generate (or update) two files:

- `.claude/project/deterministic-signals.md` — machine-generated facts: directory tree, manifests, languages, lockfile presence. Produced by `atomic signals scan`.
- `.claude/project/signals.md` — inferred meaning: framework, build/test/lint commands, architectural style, conventions, domain index. Produced by `atomic-signals-inferrer`. On large repos, optional per-domain detail files live under `signals/`.

Both files are gitignored (project-specific, not committed) and auto-referenced in the project's `CLAUDE.md` (or `claude.local.md`) via `@`-refs so Claude loads them on every session. The `atomic-signals` skill keeps them fresh: it auto-fires on project-state-change phrases and also runs silently from `/commit-only` when the staged diff touches source files. The inferrer uses content-SHA change detection for incremental domain refresh — on subsequent runs it updates only affected domains, leaving everything else byte-identical.

Requires the `atomic` binary. Run without it for a degraded tree-only fallback. Full spec: [`../spec/signals-workflow.md`](../spec/signals-workflow.md).


## Steering the inferrer


The inferrer makes its best guess from the deterministic scan, but it can get things wrong — especially for non-standard setups like monorepos with submodules, polyglot projects, or repos where naming conventions don't match the framework.

Create `.claude/project/signals-steering.md` to provide explicit hints. The inferrer reads this file before writing `signals.md` and treats its content as ground truth. When steering contradicts what the scan implies, steering wins.


### When to use steering

- The inferrer detected the wrong framework (e.g., calls it "Express" when it's NestJS).
- You have git submodules and want the inferrer to treat the parent as one project, not recurse into each submodule as a separate domain.
- Two directories are one logical domain but the inferrer split them.
- A directory looks like a domain but is actually generated code, scratch, or vendored.
- The inferrer guessed the wrong build or test command.
- You want to exclude paths from domain classification entirely.


### Format

Plain markdown. No required structure — the inferrer reads it as natural language. But headings help organize:

```markdown
# Signals steering

## Framework
This is a NestJS monorepo managed by Turborepo.

## Project structure
This repo uses git submodules. Treat the root as one project.
Do not recurse into submodules or create domains for them.
The submodules are vendored dependencies, not part of this project's
source code.

## Domains
- src/billing/ and src/payments/ are one domain ("payments")
- src/internal-tools/ is scratch code, not a real domain
- packages/ contains shared libraries — one domain ("shared"), not one per package

## Build
- Build: pnpm turbo build
- Test: pnpm test:ci (not pnpm test — that starts watch mode)
- Lint: pnpm lint

## Ignore for domains
- vendor/
- .git/modules/
- submodules/
```


### How it works

1. `/refresh-signals` (or the `atomic-signals` skill) runs `atomic signals scan` to produce the deterministic file.
2. The inferrer agent reads `deterministic-signals.md` + `signals-steering.md` (if present).
3. Steering directives override inference. If steering says "this is NestJS", the inferrer writes NestJS regardless of what `package.json` dependencies imply.
4. The inferrer writes `signals.md` (and domain files if needed).
5. On the next `/refresh-signals`, the inferrer re-reads steering — changes take effect immediately.


### Bootstrap

`/atomic-setup` creates a commented blank at `.claude/project/signals-steering.md` if it doesn't exist. Uncomment and edit the sections you need. Delete sections you don't.

The file is gitignored by default (lives under `.claude/project/`). If your team wants shared steering, move it to a committed location and `@`-reference it.


## .signalsignore


A separate mechanism from steering. `.signalsignore` (at repo root) controls which paths are excluded or flagged in the deterministic scan.

The scan uses `git ls-files` as its source — anything in `.gitignore` is already excluded automatically. `.signalsignore` is for **tracked files** (committed to git) that you still want excluded from signals or flagged as generated.

Two modes per line:

| Prefix | Behavior | Use for |
|--------|----------|---------|
| (none) | Fully excluded — path does not appear in tree at all | Committed vendored deps, checked-in fixtures, large data files |
| `+` | Flagged `[generated]` — appears in tree with metadata but inferrer skips for domain content | Checked-in build output, protobuf generated code, lockfiles |

One glob per line. Blank lines and `#` comments ignored. Same syntax as `.gitignore`.

```
# Committed but excluded from signals scan
fixtures/large-dataset.json
third_party/**
docs/vendor/**

# In tree but inferrer skips for domain content
+*.pb.go
+generated/**
+dist/**
```

Use `.signalsignore` when: tracked paths should be excluded from the scan, or should appear but not drive inference.
Use `signals-steering.md` when: you want to tell the inferrer something it can't derive from the scan (framework, domains, commands).

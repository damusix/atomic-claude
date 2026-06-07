# Signals & wiki link navigability


## Goal


Make signals and wiki markdown navigable as a graph in Obsidian, a generic markdown server, or GitHub. Path citations that today render as plain text become relative markdown links to the real files and docs, computed by code (never the model). Intra-repo (signals) and intra-realm (wiki) only — no cross-wiki linking.


## Non-goals


- Cross-wiki / cross-realm links (the `<wikis>` registry stays absolute-path, read-on-demand).
- `[[wikilink]]` syntax (rejected — see Approaches).
- Linkifying `@-ref`s or turning plain links into `@-ref`s (the no-`@-ref` rule in signals files is unchanged; a `[text](path)` is not an `@-ref`).
- Changing what the inferrer *chooses* to cite. It keeps writing the same facts; only their rendering changes.
- Linkifying fenced code blocks (examples), or any token that does not resolve to a real on-disk path.


## Success criteria


### Linkify core (code)

- [ ] A reusable linkify function takes file content + the file's absolute path + a base directory, and rewrites each **inline-code path token** (`` `path` ``) whose `join(base, token)` exists on disk into `` [`path`](relpath) ``, where `relpath = filepath.Rel(dir(file), join(base, token))`.
- [ ] Disk resolution is the filter: a token that does not `stat` from the base (e.g. `` `atomic signals scan` ``, `` `git status` ``) is left untouched. No extension/heuristic guessing.
- [ ] **Skip-set:** a token with any path segment in `{.git, node_modules, dist, build, target, vendor, .worktrees, tmp}` is never linked even when it resolves on disk — linking to build output or VCS internals is noise. Exact-segment match, so `.github` / `.githooks` / `.gitignore` still link.
- [ ] **Gitignore layer (optional, best-effort):** `LinkifyFile` skips any token that `git check-ignore` flags under the base dir (e.g. `bin/`, `.env`, logs). Batched (one `git check-ignore --stdin` per file). Degrades to skip-set-only with no error when git is absent or the base dir is not a git work tree. The pure `Linkify` (skip-set only, no exec) remains for callers/tests that want no git dependency.
- [ ] Idempotent: a token already wrapped as the text of a markdown link (`` [`path`](...) ``) is skipped, so re-running produces a byte-identical file.
- [ ] Fenced code blocks (```` ``` ````) are never linkified; only inline-code spans in prose, tables, and bullets.
- [ ] Links are file-relative (`../../agents/x.md`), never repo-root-absolute (`/agents/x.md`) and never `@-ref`s — portable across Obsidian, a markdown server, and GitHub.

### Signals

- [ ] `atomic signals linkify` linkifies `.claude/project/signals.md` and every file under `.claude/project/signals/`, with base = repo root. Idempotent; safe to re-run.
- [ ] The `atomic-signals-inferrer` agent runs `atomic signals linkify` as the final action of **default mode** only, after all files are written/reviewed. The agent still writes plain repo-root-relative paths in backticks (facts); linkify renders them. (Wiki-output mode does NOT linkify — `/refresh-wiki` runs `atomic wiki linkify` post-stamp instead.)
- [ ] The router `## Domains` table Detail column and Repo-paths column, and every domain file's `## Artifacts` / `## CLI code` / `## Docs` / `## Coupling` path citation, are linkified when they resolve on disk.
- [ ] Doctor's router check (`parseRouterDomains` in `checks_signals.go`) extracts the on-disk domain-file path from a linkified Detail cell and still validates existence + orphans. Existing doctor signals tests stay green.

### Wiki

- [ ] `atomic wiki scan` writes a managed, linked member list into `index.md` (a `## Members` managed section, spliced idempotently like the `<wiki-scan>` block, content outside untouched): `indexed` → link to `../<repo>/.claude/project/signals.md`; `summarized` → link to `repos/<repo>.md`; `pending` → link to `../<repo>/`. The realm is browsable from `index.md` after a deterministic scan, no LLM pass required.
- [ ] `atomic wiki linkify --root=<path>` linkifies wiki artifacts: each `repos/<repo>(/<domain>).md` with base = `<realm>/<repo>` (repo read from the summary's `repo:` frontmatter); each `concerns/*.md` and the `index.md` narrative with base = realm root. Idempotent.
- [ ] `/refresh-wiki` runs `atomic wiki linkify` after summaries/concerns are (re)authored and stamped, before the commit offer.
- [ ] Linkify never alters `reflects_*` frontmatter and runs after `atomic wiki stamp`, so it does not affect `atomic wiki stale` verdicts (staleness is HEAD/hash-based, not body-based).

### Discovery, gates, pipeline

- [ ] `atomic signals linkify` and `atomic wiki linkify` are user-runnable verbs → appear in `/atomic-help` (cli topic) and `CLAUDE.md`'s atomic-binary-subcommands list. Help-coverage reports no `MISSING:`.
- [ ] `make render` + `make -C atomic bundle` clean (templates touched → both regenerated and committed).
- [ ] `go test ./...` green; `go vet ./...` clean; `gofmt -l .` empty.


## Approaches


| # | Approach | Sketch | Cost | Risk |
|---|----------|--------|------|------|
| A | **Code linkifier** (chosen) | LLM writes backtick paths (facts); a Go step resolves each against disk and wraps it in a relative link with the correct `../` prefix per file | med | fuzzy token detection — mitigated by disk-resolution gate |
| B | LLM emits links directly | Inferrer/commands instruct the model to write relative md links | low | model recomputes `../../` per citation every refresh; miscounts → broken links degrade silently; reviewer can't verify every one |
| C | Repo-root-absolute links (`/path`) | One form, no per-file math | low | works only when served from repo root; broken on GitHub and most md servers |


## Recommendation


Approach A. Relative-prefix computation is a deterministic transform — code's job, not the model's (CLAUDE.md principle). Disk resolution makes token detection safe (only real paths link), idempotency makes re-runs free, and the model's job is unchanged (it keeps writing facts). One reusable linkify core serves both surfaces with a different base dir.


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | **All code**: linkify core package; `atomic signals linkify` + `atomic wiki linkify` verbs; `atomic wiki scan` linked `## Members` section; doctor router-parser update for linkified Detail | `atomic/internal/` (new linkify pkg), `atomic/internal/wiki/wiki.go`, `atomic/internal/doctor/checks_signals.go`, `atomic/cmd/atomic/main.go`, `+ tests` | `go test ./...` green (linkify idempotent, fence-skip, disk-gate, correct `../` prefix from both file depths; wiki member-list splice idempotent; doctor parses linkified Detail); `go vet` + `gofmt` clean |
| 2 | **All markdown**: inferrer runs linkify (both modes) + writes repo-root-relative backtick paths; `/refresh-signals` + `/refresh-wiki` call the verbs; specs `signals-router.md` + `wiki.md` change-logged; `/atomic-help` + `CLAUDE.md` list the verbs; render + bundle | `templates/agents/atomic-signals-inferrer.md`, `templates/commands/{refresh-signals,refresh-wiki,atomic-help}.md`, `docs/spec/{signals-router,wiki}.md`, `CLAUDE.md`, then `make render` + `make -C atomic bundle` | `make render && git diff --exit-code` clean; `make bundle && git diff --exit-code` clean; help-coverage no `MISSING:`; `npm run docs:build` clean |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Token detection links a non-path that happens to resolve | low | Only inline-code spans are candidates; non-paths rarely sit in backticks and rarely resolve from base |
| Linkify churns wiki staleness | low | Runs after `stamp`; staleness is HEAD/hash-based, not body-based |
| Doctor parser breaks on linkified Detail | med | Checkpoint 1 updates `parseRouterDomains` + keeps existing tests green |
| Re-run produces non-idempotent diffs | med | Skip-already-linked rule; checkpoint 1 asserts byte-identical re-run |


## Change log


### 2026-06-07 — Skip-set + optional gitignore filtering

**What changed:** Added two filters on top of disk-resolution. (1) A static skip-set (`.git`, `node_modules`, `dist`, `build`, `target`, `vendor`, `.worktrees`, `tmp`) — junk dirs that resolve on disk but are noise to link; exact path-segment match so `.github`/`.githooks`/`.gitignore` still link. (2) An optional, best-effort gitignore layer via a new `LinkifyFile` that skips tokens `git check-ignore` flags under the base dir, batched one call per file, degrading to skip-set-only when git is absent or the dir is not a repo. The pure `Linkify` is unchanged (skip-set only, no exec) and both callers now use `LinkifyFile`.

**Why:** Dogfooding the feature on this repo linked `node_modules`, `.git`, and `tmp` (they resolve on disk) — navigation noise. The gitignore layer generalizes the fix to anything the repo already ignores without maintaining a hardcoded list.

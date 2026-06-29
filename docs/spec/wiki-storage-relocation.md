# Wiki storage relocation (Workstream B)


## Goal


Relocate per-repo signals storage from `.claude/project/signals*` to `docs/wiki/`, add OKF frontmatter to every generated doc (except the raw `scan.md`), and introduce the `<wiki-type>`, `<scan-sha>`, and `<wiki-schema>` index control blocks. This is the foundation workstream; all other unification workstreams depend on the new paths and file format being in place.


## Non-goals


- No migration of existing `.claude/project/signals*` installs. Detecting the old layout and moving files is Workstream C (migration framework + `atomic migrate --repo`).
- No rename of `atomic-signals-inferrer` → `atomic-wiki-inferrer` and no folding of `refresh-signals` into `refresh-wiki`. Those are Workstream D.
- No `atomic migrate` binary verb, migration runner, or `config.toml [install]` changes. That is Workstream C.
- No `<wiki-schema>` migration runner. B writes the initial `<wiki-schema>1</wiki-schema>` block; C implements the runner that reads it across versions.
- No realm-wiki storage change. `<root>/wiki/` (separate repo) is unchanged.
- No `<scan-sha>`-driven drift-scope or full-vs-incremental logic changes beyond block creation. That is Workstream E.
- No new `atomic-wiki` skill-router or `atomic-wiki-inferrer` dispatch architecture. Workstream D.
- No Cobra migration. Workstream A.


## Success criteria


- [ ] `atomic signals scan` writes to `docs/wiki/scan.md` with no YAML frontmatter; it no longer writes to `.claude/project/deterministic-signals.md`.
- [ ] `atomic-signals-inferrer` (agent, current name preserved) writes `docs/wiki/index.md` with OKF frontmatter (`type: Index`, `description:`), plus machine-managed `<wiki-type>repo</wiki-type>`, `<scan-sha>…</scan-sha>`, and `<wiki-schema>1</wiki-schema>` control blocks.
- [ ] `docs/wiki/<domain>.md` files carry OKF frontmatter (`type: Domain`, `description:`); no longer written to `.claude/project/signals/<domain>.md`.
- [ ] `docs/wiki/CLAUDE.md` exists on first inferrer run; carries OKF frontmatter and steering citations; serves as nested-memory lazy-load (loaded when Claude reads any file under `docs/wiki/`).
- [ ] `docs/wiki/index.md` is `@`-ref'd from the root `CLAUDE.md` or `CLAUDE.local.md` as `@docs/wiki/index.md`; the old `@.claude/project/signals.md` ref is removed.
- [ ] Doctor check (`checks_signals.go`) passes when `docs/wiki/index.md` exists and is `@`-ref'd as `@docs/wiki/index.md`; fails with a clear message when only the old path is present.
- [ ] Doctor refs check (`checks_refs.go`) resolves `@docs/wiki/index.md` as the canonical signals ref; candidate-file search order updated to look under `docs/wiki/` first.
- [ ] `atomic serve` colors `Index` and `Domain` nodes distinctly: `frontmatterTypeToClass` in `graph.go` maps `"Index"` → `"index"` and `"Domain"` → `"domain"`.
- [ ] The tmp prev-file path is `tmp/.scan.prev.md`; no prev-file written to `.claude/project/`.
- [ ] `.gitignore` entry for the prev-file reflects the new `tmp/.scan.prev.md` path; `docs/wiki/` is not ignored (committed output).
- [ ] `README.md` and `docs/reference/signals-workflow.md` reference `docs/wiki/` layout; no stale `.claude/project/deterministic-signals.md` or `.claude/project/signals.md` mentions in updated surfaces.
- [ ] `go test ./...`, `go vet`, `gofmt -l` clean under `atomic/`. `make render` + `make -C atomic bundle` parity (no drift); VitePress `docs:build` clean.
- [ ] `/atomic-help` MISSING-scan clean after any command renames or path-string changes.


## Approach


Path constants, file-writing targets, doctor checks, inferrer agent template, and serve graph-type mapping all updated to the `docs/wiki/` layout with OKF frontmatter and control blocks — see `docs/design/signals-wiki-unification.md`.


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Go storage path constants: update `signalsFile` → `"docs/wiki/scan.md"`, `prevFile` → `"tmp/.scan.prev.md"`; update `SignalsPath`/`PrevSignalsPath` helpers; update `ScanWithOptions` write targets; update `DiffPaths` read targets | `atomic/internal/signals/signals.go:20-21,25-31,177-178`, `atomic/internal/signals/diff.go:167-173`; signal package tests | atomic-implementer (surgical) | ~3 | `atomic signals scan` writes to `docs/wiki/scan.md` with no YAML frontmatter; tmp prev-file path is `tmp/.scan.prev.md`; package tests pass |
| 2 | Doctor adaptation: update `routerFile`, `routerRef`, `domainSubdir` constants; update `signalsRef` and candidate-file search order; update test fixtures | `atomic/internal/doctor/checks_signals.go:15-17,43`, `checks_refs.go:12-17,19,42-49`, `checks_signals_test.go:34`, `checks_refs_test.go:57` | atomic-implementer (feature) | ~4 | Doctor passes on a repo with `docs/wiki/index.md` `@`-ref'd as `@docs/wiki/index.md`; fails clearly when only old path is present; `go test ./...` clean |
| 3 | Inferrer agent template: Step 8 writes `docs/wiki/index.md` (OKF FM `type: Index`, control blocks `<wiki-type>repo</wiki-type>`, `<scan-sha>`, `<wiki-schema>1</wiki-schema>`), domain files to `docs/wiki/<domain>.md` (OKF FM `type: Domain`), steering to `docs/wiki/CLAUDE.md` (OKF FM + citations); `@`-ref write target updated to `docs/wiki/index.md`; `make render` → `agents/atomic-signals-inferrer.md` | `templates/agents/atomic-signals-inferrer.md` (Step 8; @-ref write target + OKF FM emit + steering path); rendered `agents/atomic-signals-inferrer.md` | atomic-implementer (feature) | ~2 | Rendered agent file emits the new paths and FM contract; `make render` produces no diff on re-run; `make -C atomic bundle` parity |
| 4 | Signals-gate + command templates + cliusage: update staged-file paths in signals-gate (stage `docs/wiki/index.md` + `docs/wiki/*.md`, not `scan.md`); update path strings in `refresh-signals` and `refresh-wiki` templates; update `signals scan`/`show` descriptions in `cliusage.go`; `make render` for affected commands | `templates/shared/signals-gate.md`, `templates/commands/refresh-signals.md:34-157` (+ template source), `commands/refresh-wiki.md:39-40` (template source), `atomic/internal/cliusage/cliusage.go:155,161`; rendered `commands/` | atomic-implementer (feature) | ~5 | Signals-gate stages the right `docs/wiki/` files; `make render` produces no drift; cliusage descriptions mention `docs/wiki/scan.md`; `go test ./...` clean |
| 5 | Serve graph type mapping: add `"Index"` → `"index"` and `"Domain"` → `"domain"` cases to `frontmatterTypeToClass` in `graph.go` | `atomic/internal/serve/graph.go` (`frontmatterTypeToClass`); serve package tests | atomic-implementer (surgical) | ~2 | Test asserts `type: Index` frontmatter → `"index"` class, `type: Domain` → `"domain"`; existing type cases unaffected; `go test ./...` clean |
| 6 | Project wiring, gitignore, @-ref, docs surfaces, render+bundle: update `claude.local.md` @-ref; update `.gitignore:35` prev-file path; add `!docs/wiki/` negation if needed; update `README.md:102`; update `docs/reference/signals-workflow.md` + `docs/reference/` tables; update `templates/commands/atomic-help.md` if any path string changed; run `make render` + `make -C atomic bundle`; run `/refresh-signals` in this repo to produce `docs/wiki/` output | `claude.local.md`, `.gitignore:35`, `README.md:102`, `docs/reference/signals-workflow.md`, `docs/reference/` tables, `templates/commands/atomic-help.md`; regenerated `commands/`, `agents/`, `atomic/internal/embedded/` | atomic-implementer (feature) | ~8 | `@docs/wiki/index.md` in `claude.local.md`; `/atomic-help` MISSING-scan returns zero lines; render+bundle parity (`git diff --exit-code` clean after `make render && make -C atomic bundle`); `docs:build` clean; `docs/wiki/index.md` committed with OKF FM and control blocks |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| `docs/wiki/` falls under an existing `.gitignore` pattern and scan output is silently untracked | Med | CP6: verify `git status docs/wiki/` after first `/refresh-signals` run; add `!docs/wiki/` negation if any parent pattern matches |
| `checks_refs.go` search order change leaves the doctor failing when both old and new paths coexist (mid-migration state — relevant once C ships) | Med | CP2: test the new path in isolation; document in C's spec that the doctor is path-aware, not layout-aware |
| `frontmatterTypeToClass` collision between `"Index"` (serve node kind) and an existing type string | Low | CP5: read `graph.go`'s full mapping before adding; the existing cases are `Concern`, `Knowledge`, `Repo`, `Bucket`; `Index` and `Domain` are not in the set |
| Inferrer dispatched from `refresh-signals` still writes to the old `.claude/project/` paths because the agent template renders stale | Med | CP3 immediately re-runs `make render` after template edit and verifies via diff; CP6 gate requires render parity before commit |
| `scan.md` (thousands of lines) accidentally staged by the signals-gate update | Med | CP4: signals-gate explicitly excludes `docs/wiki/scan.md` from the staged set; test the gate path in the template |
| `docs/wiki/CLAUDE.md` not created on first run: subagent nested-memory delivery requires the file to exist | Low | CP3: inferrer Step 8 writes `docs/wiki/CLAUDE.md` if absent (idempotent); spec explicitly states first-run creation is required |


## Change log


<!-- empty on creation; first entry on first post-approval amendment -->

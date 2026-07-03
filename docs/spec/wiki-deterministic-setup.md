# Deterministic wiki scaffold verb (Workstream G)


## Goal


A new `atomic wiki init --scope repo|realm --root <path>` CLI verb writes the fixed-content `CLAUDE.md` scaffold for both wiki scopes deterministically in Go, replacing the two LLM-executed bash heredocs that currently do this, and closing the realm-scope gap where no self-referencing `CLAUDE.md` is ever created.


## Non-goals


- No redesign of the `docs/wiki/CLAUDE.md` scaffold content — moved verbatim from the existing heredocs.
- No change to the realm-scope `<wikis>` global registry (`atomic/internal/wiki/registry.go` `RegisterWiki`) — unrelated concern (cross-realm staleness tracking), stays as-is.
- No `atomic migrate` involvement — this is an always-idempotent bootstrap verb, not a version-gated migration step.
- No fix to `templates/commands/atomic-setup.md`'s broader pre-relocation `.claude/project/signals.md` / `deterministic-signals.md` references (audit table, `.gitignore` proposal, `CLAUDE.md` survey `@-ref` block) beyond the `signals-steering.md` rows. That staleness predates this workstream and is a separate, larger gap — out of scope here.
- No new slash command and no `/atomic-help` router restructuring — only a one-line mention of the new binary verb in the existing `binary`/`cli` topic row.
- No `docs/reference/wiki-workflow.md` update — the ship-verb `/documentation` maintenance pass picks up the new verb on the next commit that touches it; not a checkpoint here.
- No change to `.signalsignore` or any other `atomic-setup.md` heredoc besides the steering one.


## Success criteria


- [ ] `atomic wiki init --scope repo --root <path>` writes `<path>/docs/wiki/CLAUDE.md` with the scaffold content currently embedded in `templates/commands/refresh-wiki.md` R3 and `skills/atomic-wiki/references/repo.md` Step 8c, byte-identical to those heredocs; no-op (exit 0, file untouched) when the file already exists.
- [ ] `atomic wiki init --scope realm --root <path>` writes `<path>/wiki/CLAUDE.md` containing only `@index.md`; no-op when the file already exists.
- [ ] Both writes go through the `writeFileAtomic` temp-file-plus-rename idiom (`atomic/internal/wiki/registry.go:200-225`) and create missing parent directories.
- [ ] `atomic wiki init` with a missing or invalid `--scope` value (anything other than `repo`/`realm`) exits 1 with a usage error and writes nothing.
- [ ] `atomic/internal/wiki/wiki.go` `Scan()` (via its `scaffold()` step, `wiki.go:422-459`) calls the realm-scope writer, so `atomic wiki scan` on a realm root ensures `<root>/wiki/CLAUDE.md` exists with no separate call.
- [ ] `wiki init` is registered in `buildWikiCmd()` (`atomic/cmd/atomic/main.go`), the static wiki command block in `atomic/internal/cliusage/cliusage.go`, and the wiki ground-truth table in `atomic/cmd/atomic/main_test.go`; `TestDeriveCommandsGolden` and the Cobra-metadata cross-check both pass.
- [ ] `templates/commands/refresh-wiki.md` R3 no longer contains a `cat > ... EOF` heredoc; it calls `atomic wiki init --scope repo` instead.
- [ ] `skills/atomic-wiki/references/repo.md` Step 8c no longer contains a heredoc; it calls the same CLI verb.
- [ ] `templates/commands/atomic-setup.md` Step 1 audit, Step 2 propose, and Step 4 apply check for `docs/wiki/CLAUDE.md` (not `.claude/project/signals-steering.md`) and, when creating it, call `atomic wiki init --scope repo`.
- [ ] `claude.local.md` (~line 200) and `skills/atomic-wiki/references/repo.md` (lines 73, 203) no longer describe `signals-steering.md` as a distinct caller-supplied file; wording reflects `docs/wiki/CLAUDE.md` as the nested-memory steering source.
- [ ] `templates/commands/atomic-help.md`'s `binary`/`cli` topic row mentions `wiki init --scope repo|realm`.
- [ ] `go test ./...`, `go vet ./...`, `gofmt -l .` clean under `atomic/`.
- [ ] `make render && git diff --exit-code` clean; `make -C atomic bundle && git diff --exit-code` clean.
- [ ] `/atomic-help` MISSING-scan verification (`for cmd in commands/*.md; do verb=$(basename "$cmd" .md); [ "$verb" = "atomic-help" ] && continue; grep -q "/$verb" templates/commands/atomic-help.md || echo "MISSING: /$verb"; done`) returns zero lines.


## Approach


New `atomic wiki init --scope repo|realm --root <path>` CLI verb, following the `RegisterWiki`/`writeFileAtomic` idiom already established in `atomic/internal/wiki/registry.go` — see [docs/design/signals-wiki-unification.md](../design/signals-wiki-unification.md) § Deterministic scaffold creation (workstream G).


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Go CLI verb: repo/realm scaffold writers (atomic write via `writeFileAtomic`), `Scan()` hook for realm scope, `wiki init` dispatch + Cobra registration + cliusage entry, package + golden tests | new `atomic/internal/wiki/init.go` (writers), `atomic/internal/wiki/wiki.go:422-459` (`scaffold()` — call the realm writer), `atomic/internal/wiki/action.go:29-51` (`wikiAction` switch — add `case "init"`, new `wikiInitAction`), `atomic/cmd/atomic/main.go:629-661` (`buildWikiCmd()` — add `init` subcommand after `linkify`, `--scope`/`--root` flags), `atomic/internal/cliusage/cliusage.go:319-324` (static wiki block — add `wiki init` entry), `atomic/cmd/atomic/main_test.go:108-116` (`cp2WantMeta`/`cp3WantMeta` wiki rows — add `wiki init`), new `atomic/internal/wiki/init_test.go` | atomic-implementer (feature) | 7 | SC rows 1-6; `go test ./...`, `go vet`, `gofmt -l` clean; `TestDeriveCommandsGolden` and the Cobra-metadata cross-check pass |
| 2 | Retire the two heredocs: `refresh-wiki.md` R3 and `atomic-wiki` skill Step 8c call the new verb instead of embedding the scaffold | `templates/commands/refresh-wiki.md:82-119` (R3), `commands/refresh-wiki.md` (rendered via `make render`), `skills/atomic-wiki/references/repo.md:247-280` (Step 8c), `atomic/internal/embedded/bundle/**` (via `make -C atomic bundle`) | atomic-implementer (surgical) | 4 | SC rows 7-8; `make render && git diff --exit-code` and `make -C atomic bundle && git diff --exit-code` both clean |
| 3 | `atomic-setup.md` steering fix, stale `signals-steering.md` wording cleanup, and `atomic-help.md` one-line mention | `templates/commands/atomic-setup.md:74,106,180-213` (audit row, propose row, apply section), `commands/atomic-setup.md` (rendered), `claude.local.md:~200` (this repo, not templated/bundled), `skills/atomic-wiki/references/repo.md:73,203` (steering wording), `templates/commands/atomic-help.md:~125` (`binary`/`cli` topic row), `commands/atomic-help.md` (rendered), `atomic/internal/embedded/bundle/**` (regen) | atomic-implementer (feature) | 7 | SC rows 9-12; MISSING-scan zero lines; `make render` + `make -C atomic bundle` parity clean |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| `cliusage.go`'s static wiki-command table and the live Cobra tree diverge, breaking `TestDeriveCommandsGolden` / the Cobra-metadata cross-check | High (will fire if either is edited alone) | CP1 updates `cliusage.go`, `buildWikiCmd()`, and `main_test.go`'s ground-truth table in the same commit; run `go test ./...` before proceeding |
| Scaffold text drifts from the existing heredocs during the verbatim move (whitespace, comment wording) | Med | CP1's test asserts the written file matches the exact heredoc content byte-for-byte; CP2 diffs `refresh-wiki.md`/`repo.md` output against a fresh `atomic wiki init --scope repo` run before deleting the heredocs |
| Standalone `atomic wiki init --scope realm` on a root with no `wiki/` yet writes only `CLAUDE.md` (via `writeFileAtomic`'s parent-mkdir), not the rest of `wiki.Scan()`'s scaffold (`README.md`, `.gitignore`, `git init`) — a user could mistake `init` for the full realm bootstrap | Low | CLI `--help` description states `init` writes only the `CLAUDE.md` scaffold; `wiki scan` remains the entry point for full realm setup |
| Implementer scope-creeps into fixing `atomic-setup.md`'s broader pre-relocation `.claude/project/signals.md` staleness (lines outside the steering rows) while already in the file | Med | Non-goals names the exact rows in scope (audit ~74, propose ~106, apply ~180-213); CP3 verifies only those rows changed |
| `atomic-help.md`'s `binary`/`cli` topic row is one long paragraph — an imprecise edit could corrupt neighboring verb descriptions | Low | CP3 makes a single minimal insertion naming `wiki init --scope repo\|realm`; run the MISSING-scan verification command after editing |


## Change log


<!-- empty on creation; first entry on first post-approval amendment -->

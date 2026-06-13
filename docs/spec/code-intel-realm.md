# Code-intel realm federation — spec


## Goal

Enable a wiki realm to index N member repos into N per-repo symbol graphs without
writing into any member repo, fan query verbs across them, and make the session model
aware of which members are indexed and how to query them. Keep standalone (single-repo)
behavior byte-for-byte unchanged.

Design: `docs/design/code-intel-realm.md` — all deliberation settled there. This
spec carries only what gets built.


## Non-goals

- Cross-repo symbol/call/import edges (merged graph). A call from repo A into repo B
  stays unresolved in A's graph.
- A user-facing `--db` flag or arbitrary detached-db scope. Two scopes exist (repo,
  realm); each locates its db automatically.
- MCP realm awareness. The MCP server stays single-root, unchanged.
- Index format or backend changes.
- Changes to the `sg`/`grep` graceful-degradation contract in agent partials.
- Any change to `atomic wiki` subcommands or the `<wiki-scan>` block format.


## Success criteria

- [ ] **SC 1** — `atomic code` with a local index at cwd resolves to repo scope
      (today's behavior, unchanged). Without a local index, the resolver walks up; if
      cwd is under a registered `<wikis>` realm and cwd equals the realm root, the
      command fans out across all members; if cwd is inside a member directory, the
      command queries that member alone via its keyed db. Outside any realm with no
      local index, the command errors with "no index — run atomic code index".
- [ ] **SC 2** — Repo scope: db at `<repo>/.claude/.atomic-index/atomic.db`; output,
      exit codes, and relative-path behavior match `4.5.0` exactly. No meta row written.
      No member repo directory is touched when operating in realm scope.
- [ ] **SC 3** — Realm dbs at `<realm>/.atomic/<key>.db`; realm config at
      `<realm>/.atomic/code.toml`; neither file is tracked by git (the realm root is a
      plain container, not a git repo).
- [ ] **SC 4** — Fan-out partial failure: a member with no db is skipped with a
      surfaced `[key] not indexed` warning line; the operation completes across all
      other members without aborting.
- [ ] **SC 5** — Fan-out output: human-readable results are grouped under `[<key>]`
      headers; `--json` returns a `{ "<key>": <results> }` object; `--only <keys>` and
      `--exclude <keys>` filter the fan-out set to the named members.
- [ ] **SC 6** — First realm `index` with no `code.toml` seeds one entry per
      `<wiki-scan>` member (`key` = basename, slugged on collision; `path` = member
      path); `pending`-status and `trash/`-pathed members receive `exclude = true`;
      subsequent runs append new excluded members without clobbering existing entries or
      manual edits.
- [ ] **SC 7** — A `<code-index>` XML block is written into the realm CLAUDE.md and
      regenerated only when membership or keys change; a routine `atomic code index` run
      on unchanged membership produces no CLAUDE.md diff. The splice is idempotent:
      re-running produces a byte-identical block.
- [ ] **SC 8** — A one-paragraph instruction in the global `<atomic>` CLAUDE.md
      teaches every session how to read a `<code-index>` block and apply the
      scope-by-position rule.
- [ ] **SC 9** — `--only` and `--exclude` are registered in `cliusage/cliusage.go`
      under the relevant `code` verb entries; `atomic validate artifacts` passes with
      no unknown-flag findings. No `--db` flag registered.
- [ ] **SC 10** — `make render` + `make -C atomic bundle` produce zero
      `git diff --exit-code` output after all artifact edits.
- [ ] **SC 11** — Doctor check 11 is realm-aware: when realm config is present it
      reports one summary line with worst severity across all member dbs, naming only
      the repos needing action (e.g. `code index: 6 fresh; stale: foo, bar (run atomic
      code sync); not indexed: baz`). Absent everywhere → PASS informational.
- [ ] **SC 12** — No per-repo language filter exists; every `atomic code index` run
      indexes all detected languages for the target repo.
- [ ] **SC 13** — `docs/spec/code-intel-engine.md` is amended wherever CP 1 changes
      the engine db-path contract (internal decouple — accept explicit path from caller;
      no public `--db`).


## Approach

Decided in `docs/design/code-intel-realm.md`: federation + automatic two-scope
detection (position-sensing: local index → repo scope; walk-up → wiki scope) +
`<realm>/.atomic/` storage + `<code-index>` XML awareness block in realm CLAUDE.md.


## Checkpoints

File/area references ground in the verified seams from the brief. Agent column is a
role hint for dispatch — `atomic-surgeon` for targeted edits to existing seams,
`atomic-builder` for net-new code. Not a hard-coded roster; a merged implementer
role fulfills either.


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|-----------|---------------|-------|-----------|---------|
| 1 | **Engine internal decouple** — accept explicit db path from caller, separate from source root; repo-scope default path unchanged; no public flag, no meta row | `atomic/internal/codeintel/engine/engine.go` (`:52-56` consts, `:81-88` `New`/`indexPath`, `:103-145` `Init`/`open` at `:131`, `:176-177` `IndexPath`); `atomic/internal/codeintel/cli/code.go` (`:36-96` `RunCode`, `:59` engine init, `:865-867` not-initialized guard); `atomic/cmd/atomic/main.go` (`~:1017` `runCode`) | `atomic-surgeon` | 3 | SC 2 (repo path unchanged, no meta row); SC 13 (amend `code-intel-engine.md` db-path binding) |
| 2 | **Realm detection + `code.toml` parser** — resolver walks cwd → realm root; position-sensing (realm root vs inside member); TOML config struct | `repoctx.Resolve`; `wiki/staleness.go:39-73` `ReadWikiIndexPaths` + `:228-238` `isUnder`; `config/config.go:78-146` `Load` as parser template; package layout implementer's choice | `atomic-builder` | 3–4 | SC 1 (position-sensing: cwd at realm root → fan out; cwd inside member → query that member alone; outside all → error); SC 3 (config at `<realm>/.atomic/code.toml`) |
| 3 | **Realm `index` + fan-out query verbs + `--only`/`--exclude` + seeding** | `atomic/internal/codeintel/cli/code.go` (`runIndex`, `runSearch`, `runCallers`, `runCallees`, `runImpact`, `runExplore`); fan-out orchestrator (package layout implementer's choice) | `atomic-builder` | 4–6 | SC 3 (dbs at `<realm>/.atomic/<key>.db`; no member repo written); SC 4 (partial failure: skip + warn); SC 5 (`[key]` grouping, `--json`, `--only`/`--exclude`); SC 6 (seed: exclude pending/trash, append-don't-overwrite); SC 12 (no language filter) |
| 4 | **`<code-index>` XML block writer + `cliusage` flag registration** | `atomic/internal/wiki/wiki.go` (`:457-479` `writeWikiScanBlock` as XML-block precedent, `:589-636` `rewriteMembersSection` as idempotent-splice template); new block writer; `atomic/internal/cliusage/cliusage.go` (`:242-306` code-verb entries, `:15-28` `Command` struct — add `--only`, `--exclude`) | `atomic-builder` | 3 | SC 7 (XML block idempotent, regen only on membership/key change); SC 9 (`atomic validate artifacts` passes; no `--db`) |
| 5 | **Global `<atomic>` instruction ¶ + `agent-code-intel` partial realm/fan-out guidance + render + bundle** | `CLAUDE.md` (new ¶ in `## Code-intel engine`); `templates/shared/agent-code-intel.md` (scope-by-position + fan-out guidance); `make render` → `agents/`; `make -C atomic bundle` → `atomic/internal/embedded/` | `atomic-surgeon` | 4–6 | SC 8 (instruction in installed CLAUDE.md); SC 10 (`make render && git diff --exit-code`; `make bundle && git diff --exit-code`) |
| 6 | **Doctor check 11 realm-aware + docs + `atomic-help` cli topic + README + CLAUDE.md registry + sibling-spec amendments** | `atomic/internal/doctor/checks_code_index.go` (`:36-68` `RunCheckCodeIndexWith`); `docs/reference/code-intel.md`; `docs/reference/wiki-workflow.md`; `templates/commands/atomic-help.md` (cli topic row); `README.md`; `CLAUDE.md` registry; sibling specs (`code-intel-engine.md` etc.) where CP 1 changed their db-path contract | `atomic-surgeon` | 5–6 | SC 11 (doctor realm-aware: worst severity, names only repos needing action); `atomic-help cli` row covers `--only`/`--exclude`; README + CLAUDE.md registry current; sibling specs current |


## Open questions

None.


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Mandatory artifact checklist skipped — `CLAUDE.md`, `README.md`, `docs/reference/` tables, `docs/spec/` cross-refs not updated | Medium | CP 6 `Verifies` lists CLAUDE.md registry + spec cross-ref updates; spec-currency rule auto-loads on any `docs/spec/` touch |
| `/atomic-help` hard rule missed — `--only`/`--exclude` and realm behavior not discoverable through `atomic-help cli` | Medium | CP 6 `Verifies` requires the cli topic row; verification command in `CLAUDE.local.md` catches missing verbs |
| Release-please commit type mislabeled — `refactor:` or `chore:` filters out new behavior from changelog | Medium | Use `feat:` — new user-visible behavior (realm fan-out, `--only`/`--exclude`, `<code-index>` block) with no breaking changes |
| Spec-currency violation on amended `code-intel-*` specs — `code-intel-engine.md` db-path binding needs updating when CP 1 ships | Low–Medium | CP 1 `Verifies` explicitly requires amending `code-intel-engine.md`; `rules/specs/spec-currency.md` auto-loads on `docs/spec/**` touch |
| In-flight `artifact-consolidation` overlap — `cliusage.go`, `main.go` dispatch, `atomic-help.md`, `agent-code-intel` partial all touched by both features | Low | Design declares orthogonal; conflicts are line-level; `agent-code-intel` partial referenced by role, not a hard-coded agent list |
| Realm detection false-positive — path-prefix matching fires when cwd is under the realm root but not under any `<wiki-scan>` member path | Low | CP 2 behavioral spec and `Verifies` require the resolver to check cwd is under a member path, not just any subdirectory of the realm root |


## Change log

(none — initial spec)


## Implementation log

### Shipped (unreleased) — 2026-06-13

Built across 6 checkpoints via `/autopilot` (subagent implement→review loop), in an
isolated worktree. Commits (chronological, on branch `code-intel-realm`, rebased onto
main `44a7be9`):

- `592b178` — design + spec (planning carry-forward)
- `9b14b5e` — CP1 engine DB-path decouple (`NewWithDBPath`, no public flag, no meta row)
- `e56c404` — CP2 realm scope resolver + `code.toml` parser (position-sensing library)
- `f1e16f1` — CP3 realm fan-out: realm `index`, fan-out query verbs, seeding, `--only`/`--exclude`
- `96acdd3` — CP4 `<code-index>` awareness block (no timestamp, write-if-changed) + cliusage flags
- `d8db643` — CP5 teach realm scope in global CLAUDE.md + `agent-code-intel` partial + render/bundle
- `57148fe` — CP6a doctor check 11 realm-aware (aggregate member-index health)
- `1a4b1ba` — CP6b docs: `/atomic-help` cli row, `docs/reference/code-intel.md`, wiki-workflow, README

**Out-of-scope work performed during this build:**
- Rebased the branch onto main `44a7be9` (vendor grammars on demand) mid-build at the user's
  request, pulling the code-indexer fix. Clean rebase, no conflicts.
- Added exported `wiki.ReadScanMembers` helper (CP3) so the realm seeder can read `<wiki-scan>`
  members — small, in-scope addition to the wiki package.
- Tightened the Checkpoints table header (`Files / areas` → `Files/areas`) to satisfy
  `atomic validate spec` rule S5.

**Unforeseens — surprises that emerged during implementation:**
- `atomic code index` ballooned to ~35GB RAM / 976MB DB indexing the committed ~6.5M-LOC C
  tree-sitter grammar sources. The code-intel index was abandoned for the build (subagents fell
  back to `sg`/`grep` + the spec's verified seams). The user fixed it on main (vendor-on-demand,
  `44a7be9`); after the rebase the wazero-backed tests ran ~3× faster (committed `lib/ts.wasm`).
- `main.go:runCode` called `repoctx.Resolve`, which errors at a realm root (not a git repo) — the
  exact friction the feature fixes. CP3 resolves scope before forcing the git toplevel; repo scope
  (including subdir queries / first-index) is preserved verbatim, only the two realm scopes diverge.
- The `<code-index>` awareness block must advertise the config-non-excluded membership, NOT the
  per-invocation `--only`/`--exclude` set, or `index --only foo` would shrink realm awareness (CP4 fix).

**Deferred items still open:**
- None — `/autopilot` rule 2: every reviewer finding (🔴/🟡/🔵) addressed in-iteration; the
  scratchpad FOLLOWUPS ledger ended empty.
- Pre-existing, NOT this feature: `internal/hooks` `TestSessionStart_*` fail locally on machines
  with registered dirty wikis (filed `hooks-tests-read-real-home`); CI is unaffected.

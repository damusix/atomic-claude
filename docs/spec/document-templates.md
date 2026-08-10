# Document templates


## Goal


`atomic template <name>` emits the canonical fill-in skeleton for each document the workflow coordinates, and the authoring commands seed those documents from it instead of carrying inline skeletons — so the structure is copied, never reconstructed from memory.


## Non-goals


- No template for ci-mode `CONTEXT.md` (raw log capture), `.claude/project/followups/<id>.md` (CLI-owned), or wiki pages.
- No variable substitution in the verb — skeletons are static text; the LLM fills `<angle-bracket>` placeholders.
- No bundle/install shipping of the skeleton files — they live only inside the binary.


## Success criteria


- [ ] `atomic template <name>` prints a non-empty skeleton for each registered name: `brief`, `design-doc`, `diagnose-context`, `followups`, `implementation-log`, `session-report`, `spec`, `state`.
- [ ] Unknown name exits 1 with an error listing the valid names; no arguments exits 1 with usage plus the valid names.
- [ ] Every skeleton opens with a guidance comment naming its target path, its emitting verb (`atomic template <name>`), and the fill rule (fill placeholders, delete guidance comments — spec's `## Change log` comment excepted).
- [ ] `/atomic-plan`, `/subagent-implementation`, `/subagent-diagnose`, and `/session-report` seed their documents via `atomic template <name>` and carry no inline copy of any skeleton; each hard-stops when the verb is unavailable instead of improvising structure.
- [ ] The skeleton files are absent from the embedded install bundle and from `~/.claude/` after install — Claude Code surfaces no new command entries.
- [ ] `cliusage` carries a `template <name>` entry per registered name, byte-matching the cobra tree (`TestDeriveCommandsGolden` green).


## Approach


Embed skeletons in the binary via a `coldprompt`-mirror package plus a `prompt`-mirror cobra verb — see `docs/design/document-templates.md`.


## Change tree


    atomic/internal/doctemplate/
    ├── doctemplate.go ............... A  (Get, Names over embed.FS)
    ├── doctemplate_test.go .......... A  (registry contract, header rules)
    └── templates/*.md ............... A  (8 skeletons)
    atomic/cmd/atomic/
    ├── main.go ...................... M  (buildTemplateCmd, templateAction, runTemplate)
    └── main_test.go ................. M  (template dispatch tests; 19-verb gate)
    atomic/internal/cliusage/cliusage.go  M  (8 template entries)
    templates/commands/
    ├── atomic-plan.md ............... M  (skeletons → atomic template pointers)
    ├── subagent-implementation.md ... M  (BRIEF/STATE/FOLLOWUPS/impl-log pointers)
    ├── subagent-diagnose.md ......... M  (diagnose-context + scratchpad pointers)
    └── session-report.md ............ M  (report skeleton pointer)
    commands/*.md .................... M  (rendered outputs of the above)
    atomic/internal/embedded/ ........ M  (regenerated bundle)


## Outline


    atomic/internal/doctemplate/doctemplate.go
      Get — return embedded skeleton text by name, error lists valid names
      Names — sorted registered names derived from the embedded dir

    atomic/cmd/atomic/main.go
      buildTemplateCmd — cobra parent + one child per Names() entry
      templateAction — testable dispatch (usage / unknown / emit)
      runTemplate — os.Exit-aware entry point

    atomic/internal/doctemplate/templates/*.md
      guidance comment — target path, emitting verb, fill rule
      skeleton body — placeholder slots matching the command's prior inline structure

    templates/commands/{atomic-plan,subagent-implementation,subagent-diagnose,session-report}.md
      seed instructions — `atomic template <name> > <target>` per document
      fail-loud rule — stop on missing binary/verb, never improvise


## Flows


Flow: planner authors a spec

1. `/atomic-plan` reaches Write spec and runs `atomic template spec > docs/spec/<topic>.md`
2. the planner (or the spec-loop builder) fills every `<angle-bracket>` placeholder, deleting guidance comments as it fills
3. `atomic-reviewer` (spec-mode) walks the delivered spec against the same skeleton — section set, order, no leftover placeholders

Flow: loop orchestrator initializes the scratchpad

1. `/subagent-implementation` Phase 1 seeds `BRIEF.md`, `STATE.md`, `FOLLOWUPS.md` via `atomic template brief|state|followups`
2. the orchestrator fills them per the templates' lifecycle comments (brief overwritten per iteration, state append-only, followups harvested per reviewer pass)
3. at finalize, the implementation log is appended to the spec from `atomic template implementation-log`

Flow: template unavailable

1. a command runs `atomic template <name>` and the binary is absent or the verb errors (older binary)
2. the command stops with `document template unavailable (atomic template <name> failed) — install/update the atomic binary. cannot proceed.`


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | doctemplate package + template verb + skeletons | `atomic/internal/doctemplate/`, `atomic/cmd/atomic/main.go`, `main_test.go`, `atomic/internal/cliusage/cliusage.go` | atomic-implementer (mode: feature) | ~13 | `go test ./...` incl. dispatch tests + `TestDeriveCommandsGolden` |
| 2 | command rewires + render + bundle | `templates/commands/*.md` ×4, rendered `commands/`, `atomic/internal/embedded/` | atomic-implementer (mode: feature) | ~10 | `make render && make -C atomic bundle` clean; grep: no inline skeleton fences remain |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Binary absent (or pre-verb version) where a command needs a skeleton | med | Fail-loud stop message names the remedy (install/update); `atomic update` auto-refreshes artifacts, keeping binary and commands in lockstep |
| Skeleton drifts from reviewer expectations (spec-mode checklist) | low | Reviewer criteria reference the skeleton itself (`atomic template spec`) rather than a second copy |
| New skeleton added without cliusage entry | low | `TestDeriveCommandsGolden` fails on cobra/cliusage divergence; `doctemplate` registry test pins the name set |


## Change log

### 2026-08-10 — Prompt templates migrated to the binary

**What changed:** `implementer-prompt.md` / `reviewer-prompt.md` moved out of `commands/_templates/` into the `coldprompt` package as embedded briefs, served by `atomic prompt implementer` / `atomic prompt reviewer`. This removes the last underscore-prefixed path from the bundle, so `atomic/internal/embedded/bundle.go`'s `//go:embed` prefix drops the `all:` qualifier it existed for.

**Why:** `commands/_templates/*.md` still leaked into Claude Code's harness skill listing as invocable entries (`_templates:implementer-prompt`) — the exact noise problem this spec's own skeletons were built to avoid. Finishes the migration this spec deferred.

**Superseded:** the Non-goals bullet excluding this migration (removed) and the Implementation log's deferred item naming it (resolved, below) — both retired in favor of this entry.


## Implementation log

### v1 — 2026-07-09

Built directly in one pass (background session, not the subagent loop). Commits (chronological):

- `6e829be` — CP-1 + CP-2 together: doctemplate package, template verb, 8 skeletons, command rewires, render + bundle

**Out-of-scope work performed during this build:**

- none

**Unforeseens — surprises that emerged during implementation:**

- First cut placed the skeletons in `commands/_templates/` (next to the prompt templates); redirected mid-build because Claude Code surfaces every installed `commands/**.md` as an invocable entry — the embed-in-binary approach was adopted and the design doc records the rejected file-based approach.
- `TestRootCmdExact18Verbs` gates the exact top-level verb list; renamed to `TestRootCmdExact19Verbs` with `template` added.

**Deferred items still open:**

- none — the item deferred here (migrating `implementer-prompt.md` / `reviewer-prompt.md` into the binary) was resolved 2026-08-10; see Change log above.

# Spec: per-agent model and effort in the `[<harness>.agents]` config blocks

Design + rationale: [`docs/design/agents-effort-config.md`](../design/agents-effort-config.md).

This spec is read verbatim by fresh-context implementer subagents. Body = current truth;
history is in the change log. Follow [`rules/specs/spec-currency.md`](../../rules/specs/spec-currency.md)
when amending.


## Summary


Per-agent overrides live in `~/.atomic/config.toml` under one table per harness:
`[claude.agents.<name>]`, `[omp.agents.<name>]`, and `[codex.agents.<name>]`, each with two
optional fields, `model` and `effort`. Each harness adapter writes the override under its
native keys when it projects the agent file, so every lifecycle operation that converges a
target (install, update, repair, `harness rules sync`) applies it. `atomic config agents
--harness claude|omp|codex|all` edits one harness's form and, on save, converges each selected
enrolled harness through that same lifecycle. The native keys, the adapter overlay, and the
ledger ownership of agent files are specified in
[`omp-plugin-compatibility.md`](omp-plugin-compatibility.md); this spec owns the config shape,
validation, and the form.


## Data model (`internal/config`)


Per-agent overrides live under harness-namespaced tables. The field names are Atomic's;
each adapter maps them to its harness's native keys.

    [claude.agents.atomic-implementer]
    model = "opus"
    effort = "high"

    [omp.agents.atomic-reviewer]
    model = "@slow"
    effort = "high"

    [codex.agents.atomic-investigator]
    model = "gpt-5.2-codex"
    effort = "low"

Config carries one section per harness, each holding the same override map:

    type AgentOverride struct {
        Model  string `toml:"model,omitempty"`
        Effort string `toml:"effort,omitempty"`
    }

    type agentsSection struct {
        Agents map[string]AgentOverride `toml:"agents,omitempty"`
    }
    // Config: Claude, OMP, Codex agentsSection under `claude`, `omp`, `codex`

`AgentHarnesses` lists the three kinds in doctor's reporting order, and
`(*Config).AgentOverrides(kind)` returns one kind's map.

Nested-table decode only. There is **no** flat/scalar form and **no** migration: do not
implement `encoding.TextUnmarshaler`, `TextMarshaler`, `EnableUnmarshalerInterface`, or an
`unstable.Unmarshaler`. Plain struct decode/encode is the whole contract — `toml.Marshal`
emits `[<harness>.agents.<name>]` tables directly.

Semantic checks live in `Validate` and `AgentWarnings`. `agents` is not a top-level
section, so a stale top-level `[agents]` block from a pre-rename `next` build is reported as
an unknown key and ignored.


## Validation


`Validate` (`config.go`):

- **Claude effort**: must be one of `{"", low, medium, high, xhigh, max}` per agent, else hard error:
  `config: claude.agents.<name>.effort: invalid effort "<v>"; must be one of: low, medium, high, xhigh, max`.
  Empty means "no effort override".
- **OMP and Codex effort**: advisory. The levels known at release are OMP `auto, inherit, off,
  minimal, low, medium, high, xhigh, max` and Codex `none, minimal, low, medium, high, xhigh,
  max, ultra`; either harness may add one, so a value outside the list warns and passes through.
- **model**: lenient on every harness, never a hard failure. An empty model means "no model
  override". (Malformed model → warning, see below.)

`AgentWarnings` (`config.go`), per harness table:

- An unknown-agent warning (agent name not in the known set). For Claude the known set is
  `[install.artifacts].agents` matched by basename (`claude install` records `agents/<name>.md`),
  else `bundledAgents()`; for OMP and Codex it is `bundledAgents()`.
- A malformed-model warning when `Model` is non-empty and contains internal whitespace
  or control characters: `config: <harness>.agents.<name>.model: questionable value "<v>"; passed through as-is`.
- An unrecognized-effort warning for an OMP or Codex effort outside its advisory list.
- Each warning carries its harness, so `atomic doctor` category 9 ends each harness's warnings
  with `run atomic config agents --harness <kind> to edit them`. Never blocks loading or install.

A validity helper for model format (no internal whitespace or control characters;
brackets/slashes/dots/dashes/`@` allowed) is used by both the warning path and the interactive
input validator. The Claude effort set and the ordered OMP and Codex lists live in
`agentoverride.go`, reused by validation, the warnings, and the form.


## Install-time patch (`internal/claudeinstall/install.go`)


This is the legacy Claude writer (`atomic claude install` on an unadopted root). Enrolled
targets take the override from their adapter's overlay instead.

`loadAgentOverrides(home) map[string]AgentOverride` (return type changes from
`map[string]string`).

`patchAgentContent(target, content, overrides map[string]AgentOverride) []byte`:

- No-op when: `overrides` empty, target not under `agents/`, no entry for this agent, the
  entry has **both** `Model` and `Effort` empty, or the file has no parseable frontmatter.
- When `Model` non-empty: set the existing `model:` key or append it (as today).
- When `Effort` non-empty: set the existing `effort:` key or append it — the same
  order-preserving logic as `model:`, independently.
- When only one field is set, patch only that key; never touch the other. Effort-only
  overrides leave the bundled `model:` untouched.
- Preserve source key order (via `frontmatter.ParseOrdered` / `EmitOrdered`); appended keys
  go at the end. On parse/emit failure return the original bytes (best-effort, as today).

`readPatchedEmbedded` / `Plan` / `planArtifact` thread the new type through unchanged in
shape — the SHA comparison must reflect both patched keys so install/plan agree.


## Interactive form (`internal/config/agents.go`)


`AgentFormFor(kind)` returns one harness's `AgentForm`:

| Harness | Model placeholder | Effort choices after `(no override)` | Restart note |
|---|---|---|---|
| `claude` | `opus \| claude-opus-4-8` | `low, medium, high, xhigh, max` | `Restart Claude Code sessions to pick up the change.` |
| `omp` | `@slow \| anthropic/claude-opus-4-8` | the OMP advisory list | `Restart omp sessions to pick up the change.` |
| `codex` | `gpt-5.2-codex` | the Codex advisory list | `Restart Codex sessions to pick up the change.` |

The form presents, per bundled agent (every `agent` entry in the embedded bundle manifest, in
manifest order, via `bundledAgents()`):

- a **model** `huh.NewInput()`: free text, empty = no override, validated with the lenient
  model-format helper. The placeholder is the harness's. Pre-filled from that harness's table.
- an **effort** `huh.NewSelect[string]()` with `(no override)`=`""` followed by the harness's
  effort choices. Pre-selected from that harness's table.

`EditAgents(home, kind)` loads the config, runs the selector with the form and that kind's
current table, merges the answers with `applyAgentOverrides(cfg, kind, selections)`, and writes
through `WritePersist`. Per agent, both fields empty deletes the entry; otherwise Claude
validates the effort enum and every harness stores the entry. An empty table is nilled so TOML
omits it.

The selector seam `AgentTierSelector` has the signature
`func(form AgentForm, current map[string]AgentOverride) (map[string]AgentOverride, error)` and
returns `ErrNonInteractiveAgents` without a terminal and `ErrAgentsAborted` on Ctrl+C.


## Verb (`cmd/atomic/cmd_config.go`)


`atomic config agents [--harness claude|omp|codex|all]` lives in `cmd/atomic`, not in
`config.Run`, because convergence needs the harness lifecycle, which imports `config`. The
config package carries no hook for it.

- **Selection.** `--harness` wins; `all` selects every enrolled harness and refuses when none
  is. Otherwise `harness.SelectAgentsHarness` takes a `strong` detection, else the single
  enrolled harness. An unadopted legacy Claude root (a legacy `[install]` record without
  `adopted = true`, root not in the ledger) counts as enrolled Claude here. Otherwise the verb
  asks among the enrolled harnesses (all three when none is), and without a terminal refuses
  with the enrolled kinds and a pointer to `--harness`.
- **Usage.** A `--harness` value other than `claude`, `omp`, `codex`, or `all` exits 2.
- **Save.** Each selected harness runs `EditAgents` with its form. An aborted form, or one
  with no terminal, stops the run with exit 1 before the next form opens; an aborted
  harness choice prints `aborted`. Any other failure moves on to the next harness and the
  run exits 1.
- **Enrolled.** The verb converges that kind's enrolled targets through
  `install.Steps.Converge` with `EnrolledOnly`, auto-approved: lock, recovery, journal,
  staging, apply, verification, ledger. A blocked target (a hand-edited agent file is a
  conflict) prints its blockers and exits 1 with the config already saved.
- **Legacy Claude.** `claudeinstall.ReapplyAgents` runs only for an unadopted legacy Claude
  root, the one root no ledger owns.
- **Not enrolled.** Config only; the verb prints that the change applies when that harness
  is enrolled.
- A converged or re-patched harness prints its form's restart note.


## Render (`internal/config/render.go`)


`Render` emits, per harness and per agent with an override, up to two dotted keys:
`<harness>.agents.<name>.model` and `<harness>.agents.<name>.effort` (only for non-empty
fields). Sorted, byte-stable, grouped by harness. Surfaced on demand by `atomic config list`.


## No back-compat, no migration


The top-level `[agents]` block has never shipped in a release (it exists only on the
unreleased `next`/v6 line), so there is no compatibility obligation and no migration code.

- No flat/scalar entry form is accepted; `[<harness>.agents.<name>]` tables are the only shape.
- A stale top-level `[agents]` block left by a pre-rename `next` build is an unknown key:
  reported as a warning and ignored on load, and dropped from the file on the next config
  write (no `Config` field holds it, so the struct round-trip does not preserve it). It
  self-cleans; the user re-runs `atomic config agents` to set overrides under the new key.
- No config schema version bump (these tables are machine-written, not in `knownKeys`).


## Change tree


```text
atomic/internal/config/
  agentoverride.go              A  AgentOverride{Model,Effort}, AgentHarnesses, validEfforts, effortOptionValues, ordered advisoryEfforts, validModelFormat
  config.go                     M  per-harness agentsSection; Validate (Claude effort strict, model lenient); AgentWarnings per harness, each carrying Warning.Harness
  render.go                     M  Render emits <harness>.agents.<name>.model / .effort
  agents.go                     M  AgentForm, AgentFormFor, EditAgents; model Input + effort Select per harness; applyAgentOverrides(cfg, kind, ...)
  cli.go                        M  config.Run carries no agents verb and no hook
atomic/internal/claudeinstall/
  install.go                    M  loadAgentOverrides + patchAgentContent (legacy writer); ReapplyAgents (legacy root only)
atomic/internal/cliusage/
  cliusage.go                   M  config agents row: --harness
atomic/cmd/atomic/
  cmd_config.go                 M  config agents --harness: selection, EditAgents, lifecycle convergence, legacy ReapplyAgents
  main.go                       M  no config hook wiring
atomic/internal/doctor/
  checks_config.go              M  agent warnings grouped per harness, hint `atomic config agents --harness <kind>`
  checks_install_test.go        M  regression test: config↔installed agent drift → WARN; --fix repairs
docs/reference/agents.md        M  per-harness tables, selection, convergence, edge cases
docs/reference/atomic-toml.md   M  agent overrides live in ~/.atomic/config.toml, one table per harness
docs/reference/commands.md      M  atomic config row naming config agents --harness
context/commands/atomic-help.md M  config agents row in Binary subcommands and tour Stage 4
```


## Outline


- `agentoverride.go`
  - `AgentOverride` — model + effort override for one agent; TOML tags `omitempty`
    - (no custom unmarshaler: plain struct decode from `[<harness>.agents.<name>]`)
  - `validEfforts` / `effortOptionValues` — Claude effort enum set + ordered list
  - `advisoryEfforts` — ordered OMP and Codex effort lists, shared by warnings and the form
  - `validModelFormat` — lenient model shape check (no internal whitespace/control chars)
- `config.go`
  - `agentsSection` — one per harness, holding `Agents map[string]AgentOverride`
  - `Validate` — Claude effort strict against the enum; model no hard-fail
  - `AgentWarnings` — unknown-agent, malformed-model, and unrecognized-effort warnings, each naming its harness
- `render.go`
  - `Render` — per-harness, per-agent `.model`/`.effort` dotted keys, byte-stable
- `agents.go`
  - `bundledAgents` — every `agent` entry in `embedded.Manifest()`, in manifest order
  - `AgentForm` / `AgentFormFor` — one harness's placeholder, effort choices, and restart note
  - `EditAgents` — load, run the selector on one harness's form and table, merge, write
  - `validateModelInput` — wraps validModelFormat with the empty-is-ok rule + error message
  - `defaultAgentTierSelector` — model Input + effort Select per agent, returns `map[string]AgentOverride`
  - `applyAgentOverrides` — merge selections into one harness's table; both-empty deletes; Claude effort validated
- `install.go`
  - `patchAgentContent` — set-or-append `model:` and `effort:` independently, order-preserving
  - `setOrAppendKey` — shared find-existing-or-append frontmatter helper
  - `ReapplyAgents` — re-patch only already-installed `agents/*.md` of an unadopted legacy root; skips absent (no first-time install); returns changed names
- `cmd_config.go`
  - `agentsHarnesses` — `--harness`, strong detection, single enrolled, interactive choice, or refusal
  - `unadoptedLegacyClaude` — legacy `[install]` record not adopted and root not in the ledger
  - `editAndConvergeAgents` — `EditAgents`, then lifecycle convergence, legacy re-patch, or a not-enrolled report
- `checks_config.go`
  - `checkConfigValidity` — agent warnings grouped per harness with `--harness <kind>` hints
- `checks_install_test.go`
  - regression test — an installed agent whose frontmatter lacks a configured override → doctor install check WARN; `--fix` re-syncs


## Flows


1. **Read**: `config.Load` decodes each `[<harness>.agents.<name>]` table straight into that harness's `Agents` map (`map[string]AgentOverride`) by plain struct decode. No scalar form, no custom unmarshaler.
2. **Select**: user runs `atomic config agents [--harness claude|omp|codex|all]` → `agentsHarnesses` resolves the harnesses (flag, strong detection, single enrolled, interactive choice, or refusal listing the enrolled kinds).
3. **Write**: per harness, `EditAgents` → `AgentTierSelector` with that harness's form and table → `applyAgentOverrides` mutates that table → `WritePersist` → `toml.Marshal` emits `[<harness>.agents.<name>]` tables.
4. **Converge**: an enrolled harness → `install.Steps.Converge` (`EnrolledOnly`, that kind) → the adapter overlays the override on its agent projection → the ledger claims the written bytes; a hand-edited agent conflicts and keeps its bytes, and the verb exits 1. Then the form's restart note prints.
5. **Legacy Claude**: Claude not enrolled but an unadopted legacy record present → `ReapplyAgents(<Claude root>, home)` re-patches the agent files already on disk → restart note.
6. **Not enrolled**: any other harness → config only, reported as applying when that harness is enrolled.
7. **Legacy install patch**: `atomic claude install`/`update` on an unadopted root → `loadAgentOverrides` → for each `agents/*.md` artifact, `patchAgentContent` sets/appends `model:` (from Model) and `effort:` (from Effort), each only when set → patched bytes written and factored into the Plan SHA.
8. **Drift detection + repair**: on an unadopted legacy root, the `atomic doctor` install-integrity check → `claudeinstall.Diff` compares each on-disk agent against `readPatchedEmbedded` (bundle patched with model+effort). A configured override missing from the installed file → SHA mismatch → WARN. `atomic doctor --fix` → idempotent `Install` re-patches. Fully deterministic (SHA + frontmatter surgery, no model).


## Checkpoints


| # | Checkpoint | Files/areas | Acceptance | Verifies |
|---|-----------|-------------|------------|----------|
| CP1 | Schema type + decode/encode | `internal/config/{agentoverride,config,render,agents,cli}.go`; `internal/claudeinstall/install.go` (type thread) | `AgentOverride{Model,Effort}` with no custom unmarshaler; `Config.Claude.Agents` is `map[string]AgentOverride`; `Validate` effort-strict / model-lenient; `AgentWarnings` gains malformed-model warning; `validTiers` hard-fail removed | Nested `[claude.agents.x]` table decodes to `{Model,Effort}`; effort-only decodes to `{Effort}`; a scalar under `[claude.agents]` is a decode error; `WritePersist` emits `[claude.agents.<name>]` tables; invalid effort → Validate error; malformed model → warning not error; well-formed arbitrary id (`claude-opus-4-8`, `claude-opus-4-6[1m]`) → no error/warning |
| CP2 | Install-time patch of `model:` + `effort:` | `internal/claudeinstall/install.go` | `loadAgentOverrides` + `patchAgentContent` handle both fields independently; effort-only patches only `effort:`; both-empty is a no-op; order preserved | model-only patch; effort-only patch (model frontmatter unchanged); both patched; neither → unchanged bytes; append when key absent; Plan SHA reflects both keys |
| CP3 | Interactive form + render | `internal/config/agents.go` | `atomic config agents` model Input + effort Select per agent; `applyAgentOverrides`; both-empty deletes entry; `Render` emits `.model`/`.effort` dotted keys | selector seam returns overrides → applied to cfg; empty-both removes entry; render output for model-only, effort-only, both; non-interactive + abort errors preserved |
| CP4 | Docs + discovery surfaces | `docs/reference/agents.md`; `templates/commands/atomic-help.md`; `docs/spec/agent-model-overrides.md`; regenerated `commands/` + embedded bundle | `docs/reference` `[claude.agents]` doc updated; `/atomic-help` cli topic mentions effort; prior spec superseded; design/spec committed | `atomic validate` clean; `/atomic-help` MISSING-scan clean; render+bundle parity clean |
| CP5 | Re-apply on a legacy Claude root | `internal/claudeinstall/install.go` (`ReapplyAgents`) | `ReapplyAgents` re-patches only already-installed `agents/*.md` (drift-only writes; absent → skip, never a first-time install) | `ReapplyAgents` patches an installed agent's frontmatter to match config; skips an absent agent (not created); no-op when on-disk already matches |
| CP6 | Drift detection guard + docs | `internal/doctor/checks_install_test.go`; `docs/reference/agents.md` | The existing install-integrity check already flags config↔installed agent drift (via `Diff`→`readPatchedEmbedded`, deterministic) and `--fix` repairs it — lock with a regression test; document auto-apply + doctor drift/repair + session-restart caveat. No new doctor check (reuse-first) | doctor install check returns WARN when an installed agent lacks a configured effort/model; `applyInstallRepair`/`Install` re-syncs it to PASS; docs updated |
| CP7 | Harness-namespaced keys + drop back-compat | `internal/config/{config,agentoverride,agents,cli,render}.go`; `internal/claudeinstall/install.go`; `internal/doctor/*`; docs; `templates/commands/atomic-help.md` (+ rendered/bundled) | Top-level `[agents]` → `[claude.agents.<name>]` via a `claudeSection` on Config; `AgentOverride.UnmarshalText`, the flat/scalar form, the auto-migration and their tests all REMOVED; `opaqueSections`/`knownSections` carry `claude`, not `agents`; all error/warning/render key paths say `claude.agents.<name>.…` | nested `[claude.agents.x]` decodes; a scalar under it is a decode error (no silent accept); a stale top-level `[agents]` block warns as unknown-key and is ignored; `WritePersist` emits `[claude.agents.<name>]`; render emits `claude.agents.<name>.model/.effort`; install patch + auto-apply + doctor drift still green; whole suite + validate + render/bundle parity clean |
| CP8 | Per-harness forms and lifecycle convergence | `internal/config/{agentoverride,agents,cli,config}.go`; `internal/cliusage/cliusage.go`; `cmd/atomic/{cmd_config,main}.go`; `internal/doctor/checks_config.go`; docs | `atomic config agents --harness claude\|omp\|codex\|all` with the selection order above; one form per harness; a save converges enrolled targets through the lifecycle; `ReapplyAgents` only for an unadopted legacy root; config package has no `ApplyAgentsHook`; doctor hints name `--harness <kind>` | selection cases (flag, strong, single enrolled on weak/ambiguous/none, interactive choice, non-interactive refusal); each form's placeholder, efforts, restart note; blank removes; not-enrolled saves config only; an enrolled OMP agent carries the new keys and the ledger digest equals disk; a hand-edited agent conflicts; a `CLAUDE_CONFIG_DIR` root converges; doctor hint test |


## Non-goals


- No agent-file write outside the lifecycle, except `ReapplyAgents` on an unadopted legacy
  Claude root.
- No committed `agents/*.md` edits: the override is applied to the projected copy.
  Therefore no `make render` / `make bundle` unless a source artifact is actually touched.
- No new config schema version, no CLI `set`/`get` support for `[<harness>.agents]` tables:
  they are machine-written via `atomic config agents`, not user-settable via `atomic config set`.


## Change log


- Initial spec (autopilot run for `agents-effort-config`). `[agents]` grows from a flat
  `agent → tier` string map to a nested `[agents.<name>]` table with optional `model` +
  `effort`; effort applied at install by patching the `effort:` agent frontmatter key;
  model validation relaxed from a hard tier allowlist to a lenient format check; flat
  entries read as `{model}` and auto-migrate to nested on write.


### 2026-07-21 — Implemented (autopilot)

**What changed:** All four checkpoints delivered on branch `agents-effort-config` (base `next`).
CP1 `e67ead2` — `AgentOverride{Model,Effort}` + `UnmarshalText` scalar back-compat seam,
`Config.Agents` retyped, effort-strict/model-lenient `Validate`, malformed-model `AgentWarnings`,
`validTiers` removed. CP2 `b13bece` — `patchAgentContent` sets/appends `model:` and `effort:`
independently via `setOrAppendKey`. CP3 `f45a7ff` — `atomic config agents` model Input + effort
Select. CP4 `37bc2cd` — `docs/reference/agents.md`, `atomic config agents` help row (rendered +
bundled), prior spec superseded. Follow-on `515ebf3` — `internal/doctor` config-check test updated
to the new contract (invalid effort → FAIL, arbitrary model → not FAIL), caught by full-suite verify.

**Why:** feature request — set reasoning effort per agent (e.g. implementer high, reviewer max)
alongside the model, mirroring `[pi.agent]`.

**Verified:** `go test ./...` green (the sole red, `internal/doctor/TestRepairPlan_configWARN_fixable`,
fails only against a real `~/.atomic` with `install.version="dev"`; passes under a clean HOME —
pre-existing, filed `doctor-config-test-reads-real-home`). `atomic validate` clean; render + bundle
parity clean; `/atomic-help` MISSING-scan 0.


### 2026-07-22 — Auto-apply + drift detection (CP5/CP6)

**What changed:** `atomic config agents` now applies immediately — after writing the config it
re-patches the already-installed `~/.claude/agents/*.md` frontmatter via a new
`claudeinstall.ReapplyAgents` (invoked through a `config.ApplyAgentsHook` seam so `internal/config`
does not import `internal/claudeinstall`), so a user no longer has to run `atomic claude install`
separately. It writes only drifted, already-installed agent files (never a first-time install) and
prints the changed count plus a note that running Claude Code sessions must restart to pick up the
new frontmatter. Drift between `[agents]` config and installed agent files is detected
deterministically by the existing `atomic doctor` install-integrity check (`Diff` compares against
`readPatchedEmbedded`, which patches model+effort) and repaired by `atomic doctor --fix`; CP6 adds a
regression test locking that and documents it. No new doctor check.

**Why:** feedback — the config alone was inert (Claude reads agent-md frontmatter, never
`config.toml`), so setting an override had no effect until a separate reinstall; and drift should be
caught by a deterministic check, not left to the user to notice.


### 2026-07-22 — Harness-namespaced keys, back-compat removed (CP7)

**What changed:** the top-level `[agents]` block moved to `[claude.agents.<name>]` (a `claude`
section on `Config`), and pi's `[pi.agent]` was renamed `[pi.agents]` so both harnesses read
`[<harness>.agents.<name>]`. The pi change is a config-key rename only; applying pi overrides to
pi's agent files remains the pi extension's job via `atomic config resolve`. All flat/scalar
back-compat was deleted: `AgentOverride.UnmarshalText`, the scalar entry form, the flat→nested
auto-migration, and their tests are gone. Nested tables are the only accepted shape.

**Why:** two review points. (1) `[agents]` top-level vs `[pi.agent]` namespaced was an
inconsistent way to express the same concept. (2) The flat form was never released — it exists
only on the unreleased `next`/v6 line (`git tag --contains` on its commit is empty; not an
ancestor of `v5.9.0`), so preserving compatibility with it was unjustified complexity. `next` is
not shipped.

**Superseded:** the prior contract accepted a flat `agents.<name> = "<tier>"` scalar via
`encoding.TextUnmarshaler` and auto-migrated it to nested tables on the next config write, under
a top-level `[agents]` table. None of that remains.


### 2026-09-23 — agent list follows the bundle

**What changed:** the form's agent list and `AgentWarnings`' fallback known set come from the embedded
bundle manifest instead of the hardcoded `agentOrder` / `knownAtomicAgents` lists. `AgentWarnings`
matches install-manifest entries by basename, since `claude install` records `agents/<name>.md`.

**Why:** the lists held 5 names while the bundle shipped 8, so new agents never appeared in the form;
the manifest match flagged every override on an installed machine as an unknown agent.


### 2026-11-10 — Pi block and `config resolve` retired; install entry points named

**What changed:** the body no longer describes a `[pi.agents]` block, the `pi_agent.go` file, or `atomic config resolve`; only `[claude.agents.<name>]` remains. "Install time" now names `atomic claude install` and `atomic claude update` and states that `atomic install` and `atomic update` patch no agent frontmatter.

**Why:** the Pi extension and its `atomic config resolve` verb were removed, and a bare `atomic install` plus update-time adoption now reach Claude through the harness engine, which ships agent files unchanged.

**Superseded:** the `[pi.agent]` → `[pi.agents]` rename for symmetry, the Non-goal that left pi agent-file writing to the pi extension via `atomic config resolve`, and the Change tree row for `pi_agent.go`.


### 2026-11-11 — Per-harness forms; save converges through the lifecycle

**What changed:** the spec now covers `[claude.agents]`, `[omp.agents]`, and `[codex.agents]`. `atomic config agents --harness claude|omp|codex|all` selects harnesses by flag, strong detection, the single enrolled harness, an interactive choice, or a non-interactive refusal listing the enrolled kinds, and opens one form per harness with its own placeholder, effort choices, and restart note. The verb moved to `cmd/atomic`. A save converges each enrolled harness through `install.Steps.Converge`, so the ledger owns the overlaid agent bytes and a hand edit conflicts. `ReapplyAgents` runs only for an unadopted legacy Claude root. Each `AgentWarnings` entry names its harness, and doctor's hint becomes `atomic config agents --harness <kind>`. The Validation, Interactive form, Render, Change tree, Outline, Flows, Checkpoints (CP5 rewritten, CP8 added), and Non-goals sections describe that contract.

**Why:** `docs/spec/omp-plugin-compatibility.md` CP12C. A save that patched Claude files in place left the ledger holding a digest of bytes no longer on disk, outside the lifecycle lock and journal; and OMP and Codex had no form at all.

**Superseded:** `atomic config agents` edited only `[claude.agents]` and, through a `config.ApplyAgentsHook` seam wired in `cmd/atomic/main.go`, re-patched `~/.claude/agents/*.md` in place after every save; the Non-goal ruled out agent-file writing for any harness but Claude Code.

**Correction:** the Change tree listed a `templates/commands/atomic-help.md` `config agents` row that `context/commands/atomic-help.md` never carried. This change adds that row (Binary subcommands table and tour Stage 4) and a `docs/reference/commands.md` `atomic config` row, and the tree names both.

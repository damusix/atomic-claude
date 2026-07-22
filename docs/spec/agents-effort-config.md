# Spec: per-agent effort in the `[claude.agents]` config block

Design + rationale: [`docs/design/agents-effort-config.md`](../design/agents-effort-config.md).
Structural model to mirror: [`internal/config/pi_agent.go`](../../atomic/internal/config/pi_agent.go).

This spec is read verbatim by fresh-context implementer subagents. Body = current truth;
history is in the change log. Follow [`rules/specs/spec-currency.md`](../../rules/specs/spec-currency.md)
when amending.


## Summary


Per-agent Claude overrides live in `~/.atomic/config.toml` under `[claude.agents.<name>]`,
with two optional fields: `model` and `effort`. Both are applied to the installed agent
frontmatter (`model:` and `effort:` keys) at install time and immediately on save. pi's
equivalent block is renamed `[pi.agent]` → `[pi.agents]` for symmetry (config key only —
applying it to pi's agent files remains the pi extension's job via `atomic config resolve`).
Nothing here has shipped in a release, so there is no back-compat and no migration.


## Data model (`internal/config`)


Per-agent overrides live under a harness-namespaced table, `[claude.agents.<name>]`, parallel
to pi's `[pi.agents.<name>]`. Both harnesses read `[<harness>.agents.<name>]`; the second field
differs because each uses its own harness's real frontmatter key (`effort` for Claude Code,
`thinking` for pi).

    [claude.agents.atomic-implementer]
    model = "opus"
    effort = "high"

Config carries a `claude` section holding the override map:

    type AgentOverride struct {
        Model  string `toml:"model,omitempty"`
        Effort string `toml:"effort,omitempty"`
    }

    type claudeSection struct {
        Agents map[string]AgentOverride `toml:"agents,omitempty"`
    }
    // Config: Claude claudeSection `toml:"claude,omitempty"`

Nested-table decode only. There is **no** flat/scalar form and **no** migration: do not
implement `encoding.TextUnmarshaler`, `TextMarshaler`, `EnableUnmarshalerInterface`, or an
`unstable.Unmarshaler`. Plain struct decode/encode is the whole contract — `toml.Marshal`
emits `[claude.agents.<name>]` tables directly.

`claude` (and `pi`) are the opaque top-level sections (structural unknown-key checks skipped;
semantic checks live in `Validate` / `AgentWarnings`). `agents` is no longer a top-level
section, so a stale top-level `[agents]` block from a pre-rename `next` build is reported as
an unknown key and ignored.


## Validation


`Validate` (`config.go`):

- **effort**: must be one of `{"", low, medium, high, xhigh, max}` per agent, else hard error:
  `config: claude.agents.<name>.effort: invalid effort "<v>"; must be one of: low, medium, high, xhigh, max`.
  Empty means "no effort override".
- **model**: lenient — never a hard failure. Drop the current `validTiers` hard-fail. An
  empty model means "no model override". (Malformed model → warning, see below.)

`AgentWarnings` (`config.go`):

- Preserve the existing unknown-agent warning (agent name not in the installed/known set).
- Add a malformed-model warning when `Model` is non-empty and contains internal whitespace
  or control characters: `config: claude.agents.<name>.model: questionable value "<v>"; passed through as-is`.
  Never blocks loading or install.

`validTiers` / the tier allowlist is removed from the validation path. A validity helper for
model format (non-empty, no internal whitespace; brackets/slashes/dots/dashes allowed) is
used by both the warning path and the interactive input validator.

Effort allowlist lives in one exported-or-package var reused by `Validate` and the
interactive Select.


## Install-time patch (`internal/claudeinstall/install.go`)


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


## Interactive form (`internal/config/agents.go`, `cli.go`)


`atomic config agents` presents, per bundled agent (the fixed 5 in `agentOrder`):

- a **model** `huh.NewInput()` — free text, empty = no override, validated with the lenient
  model-format helper (empty passes; non-empty must have no internal whitespace). Placeholder
  shows examples (e.g. `opus  |  claude-opus-4-8`). Pre-filled with the current `Model`.
- an **effort** `huh.NewSelect[string]()` with options `(bundled default)`=`""`, `low`,
  `medium`, `high`, `xhigh`, `max`. Pre-selected to the current `Effort`.

Replace `applyAgentTiers` with `applyAgentOverrides(cfg *Config, selections map[string]AgentOverride) error`:
per agent, if both fields empty → delete the entry; else validate (effort enum; model lenient)
and store. Nil out `cfg.Claude.Agents` when empty so the `[claude.agents]` table is omitted.

The selector seam (`AgentTierSelector` / `DefaultAgentTierSelector`) keeps its
test-injection role; its signature becomes `func(cfg *Config) (map[string]AgentOverride, error)`.
`cli.go` call sites (`AgentTierSelector`, `applyAgentOverrides`) update to the new types. The
`ErrNonInteractiveAgents` / `ErrAgentsAborted` behavior is unchanged. Rename symbols away from
"tier"-only naming where it now misleads (e.g. `applyAgentTiers` → `applyAgentOverrides`);
keep the public `AgentTierSelector` name to avoid churn unless trivially safe to rename.


## Render (`internal/config/render.go`)


`Render` emits, per agent with an override, up to two dotted keys under the `[claude]`
section: `claude.agents.<name>.model` and `claude.agents.<name>.effort` (only for non-empty fields). Sorted,
byte-stable. This is the auto-loaded `config.resolved.md` view.


## No back-compat, no migration


Neither the top-level `[agents]` block nor `[pi.agent]` has ever shipped in a release (both
exist only on the unreleased `next`/v6 line), so there is no compatibility obligation and no
migration code.

- No flat/scalar entry form is accepted; `[claude.agents.<name>]` tables are the only shape.
- A stale top-level `[agents]` block left by a pre-rename `next` build is an unknown key:
  reported as a warning and ignored. The user re-runs `atomic config agents`.
- Same for a stale `[pi.agent]` block after the `[pi.agents]` rename.
- No config schema version bump (these tables are machine-written, not in `knownKeys`).


## Change tree


```text
atomic/internal/config/
  agentoverride.go              A  AgentOverride{Model,Effort}, validEfforts, effortOptionValues, validModelFormat (no UnmarshalText)
  config.go                     M  claudeSection{Agents map[string]AgentOverride} under [claude]; Validate (effort-strict, model-lenient); AgentWarnings malformed-model; validTiers removed
  pi_agent.go                   M  parser key [pi.agent] -> [pi.agents] (config key only)
  render.go                     M  Render emits claude.agents.<name>.model / .effort
  agents.go                     M  model Input + effort Select; applyAgentOverrides; validateModelInput; tier options removed
  cli.go                        M  agents verb -> applyAgentOverrides
atomic/internal/claudeinstall/
  install.go                    M  loadAgentOverrides + patchAgentContent patch model: and effort: (setOrAppendKey); ReapplyAgents (agent-scoped re-apply)
atomic/internal/config/
  cli.go                        M  agents verb: after save, call ApplyAgentsHook (auto-apply)
  config.go (or cli.go)         M  ApplyAgentsHook seam var (nil default; wired by cmd/atomic)
atomic/cmd/atomic/main.go       M  wire ApplyAgentsHook to a claudeinstall.ReapplyAgents closure at startup
atomic/internal/doctor/
  checks_install_test.go        M  regression test: config↔installed agent drift → WARN; --fix repairs
docs/reference/agents.md        M  "Model and effort overrides" section + auto-apply + doctor-drift notes
docs/spec/agent-model-overrides.md  M  supersession banner + change-log entry
templates/commands/atomic-help.md   M  config agents cli-topic row  (-> rendered commands/atomic-help.md + embedded bundle)
```


## Outline


- `agentoverride.go`
  - `AgentOverride` — model + effort override for one agent; TOML tags `omitempty`
    - (no custom unmarshaler — plain struct decode from `[claude.agents.<name>]`)
  - `validEfforts` / `effortOptionValues` — effort enum set + ordered list (shared by Validate and the Select)
  - `validModelFormat` — lenient model shape check (non-empty, no internal whitespace/control chars)
- `config.go`
  - `claudeSection` — the `[claude]` table holding `Agents map[string]AgentOverride`
  - `Validate` — effort strict against the enum; model no hard-fail (validTiers removed)
  - `AgentWarnings` — unknown-agent warning + malformed-model warning
- `render.go`
  - `Render` — per-agent `.model`/`.effort` dotted keys, byte-stable
- `agents.go`
  - `validateModelInput` — wraps validModelFormat with the empty-is-ok rule + error message
  - `defaultAgentTierSelector` — model Input + effort Select per agent, returns `map[string]AgentOverride`
  - `applyAgentOverrides` — merge selections into cfg.Agents; both-empty deletes; validate on store
- `install.go`
  - `patchAgentContent` — set-or-append `model:` and `effort:` independently, order-preserving
  - `setOrAppendKey` — shared find-existing-or-append frontmatter helper
  - `ReapplyAgents` — re-patch only already-installed `agents/*.md` from bundle+overrides; skips absent (no first-time install); returns changed names
- `config` seam + `cmd/atomic`
  - `ApplyAgentsHook` — package var in config (nil default), wired by `cmd/atomic` to a `ReapplyAgents` closure; called by the `agents` verb after save
- `checks_install_test.go`
  - regression test — an installed agent whose frontmatter lacks a configured override → doctor install check WARN; `--fix` re-syncs


## Flows


1. **Read** — `config.Load` decodes `[claude.agents.<name>]` tables straight into `Config.Claude.Agents` (`map[string]AgentOverride`) by plain struct decode. No scalar form, no custom unmarshaler.
2. **Write** — user runs `atomic config agents` → selector returns overrides → `applyAgentOverrides` mutates `cfg.Claude.Agents` → `WritePersist` → `toml.Marshal` emits `[claude.agents.<name>]` tables.
3. **Install patch** — `atomic claude install`/`update` → `loadAgentOverrides` → for each `agents/*.md` artifact, `patchAgentContent` sets/appends `model:` (from Model) and `effort:` (from Effort), each only when set → patched bytes written to `~/.claude/agents/` and factored into the Plan SHA.
4. **Auto-apply** — `atomic config agents` → after `WritePersist`, the `ApplyAgentsHook` runs `ReapplyAgents(~/.claude, home)`: re-patches only the agent files already present on disk (drift-only writes; absent agents skipped, no first-time install), then prints the changed count and a "restart Claude Code sessions to pick up the change" note. No-op (silent) when nothing is installed yet.
5. **Drift detection + repair** — `atomic doctor` install-integrity check → `claudeinstall.Diff` compares each on-disk agent against `readPatchedEmbedded` (bundle patched with model+effort). A configured override missing from the installed file → SHA mismatch → WARN. `atomic doctor --fix` → idempotent `Install` re-patches. Fully deterministic (SHA + frontmatter surgery, no model). This behavior is inherited from the CP2 patch threading through `Diff`; CP6 only locks it with a test and documents it — no new doctor check.


## Checkpoints


| # | Checkpoint | Files/areas | Acceptance | Verifies |
|---|-----------|-------------|------------|----------|
| CP1 | Schema type + decode/encode | `internal/config/{agentoverride,config,render,agents,cli}.go`; `internal/claudeinstall/install.go` (type thread) | `AgentOverride{Model,Effort}` with no custom unmarshaler; `Config.Claude.Agents` is `map[string]AgentOverride`; `Validate` effort-strict / model-lenient; `AgentWarnings` gains malformed-model warning; `validTiers` hard-fail removed | Nested `[claude.agents.x]` table decodes to `{Model,Effort}`; effort-only decodes to `{Effort}`; a scalar under `[claude.agents]` is a decode error; `WritePersist` emits `[claude.agents.<name>]` tables; invalid effort → Validate error; malformed model → warning not error; well-formed arbitrary id (`claude-opus-4-8`, `claude-opus-4-6[1m]`) → no error/warning |
| CP2 | Install-time patch of `model:` + `effort:` | `internal/claudeinstall/install.go` | `loadAgentOverrides` + `patchAgentContent` handle both fields independently; effort-only patches only `effort:`; both-empty is a no-op; order preserved | model-only patch; effort-only patch (model frontmatter unchanged); both patched; neither → unchanged bytes; append when key absent; Plan SHA reflects both keys |
| CP3 | Interactive form + render | `internal/config/agents.go` | `atomic config agents` model Input + effort Select per agent; `applyAgentOverrides`; both-empty deletes entry; `Render` emits `.model`/`.effort` dotted keys | selector seam returns overrides → applied to cfg; empty-both removes entry; render output for model-only, effort-only, both; non-interactive + abort errors preserved |
| CP4 | Docs + discovery surfaces | `docs/reference/agents.md`; `templates/commands/atomic-help.md`; `docs/spec/agent-model-overrides.md`; regenerated `commands/` + embedded bundle | `docs/reference` `[claude.agents]` doc updated; `/atomic-help` cli topic mentions effort; prior spec superseded; design/spec committed | `atomic validate` clean; `/atomic-help` MISSING-scan clean; render+bundle parity clean |
| CP5 | Auto-apply on `atomic config agents` | `internal/claudeinstall/install.go` (`ReapplyAgents`); `internal/config/cli.go` + `ApplyAgentsHook` seam; `cmd/atomic/main.go` (wiring) | `ReapplyAgents` re-patches only already-installed `agents/*.md` (drift-only writes; absent → skip, never a first-time install); `atomic config agents` calls the hook after save, prints changed count + restart note; silent no-op when not installed; config↛claudeinstall import cycle avoided via the hook | `ReapplyAgents` patches an installed agent's frontmatter to match config; skips an absent agent (not created); no-op when on-disk already matches; config cli test with fake hook asserts it is called + output; whole module builds (no import cycle) |
| CP6 | Drift detection guard + docs | `internal/doctor/checks_install_test.go`; `docs/reference/agents.md` | The existing install-integrity check already flags config↔installed agent drift (via `Diff`→`readPatchedEmbedded`, deterministic) and `--fix` repairs it — lock with a regression test; document auto-apply + doctor drift/repair + session-restart caveat. No new doctor check (reuse-first) | doctor install check returns WARN when an installed agent lacks a configured effort/model; `applyInstallRepair`/`Install` re-syncs it to PASS; docs updated |
| CP7 | Harness-namespaced keys + drop back-compat | `internal/config/{config,agentoverride,agents,cli,render,pi_agent}.go`; `internal/claudeinstall/install.go`; `internal/doctor/*`; docs; `templates/commands/atomic-help.md` (+ rendered/bundled) | Top-level `[agents]` → `[claude.agents.<name>]` via a `claudeSection` on Config; `[pi.agent]` → `[pi.agents]` (parser key rename ONLY — no pi agent-file writing, that stays the pi extension's job); `AgentOverride.UnmarshalText`, the flat/scalar form, the auto-migration and their tests all REMOVED; `opaqueSections`/`knownSections` carry `claude` + `pi`, not `agents`; all error/warning/render key paths say `claude.agents.<name>.…` | nested `[claude.agents.x]` decodes; a scalar under it is a decode error (no silent accept); a stale top-level `[agents]` block warns as unknown-key and is ignored; `[pi.agents.x]` resolves via `ResolvePiAgents` and `[pi.agent]` no longer does; `WritePersist` emits `[claude.agents.<name>]`; render emits `claude.agents.<name>.model/.effort`; install patch + auto-apply + doctor drift still green; whole suite + validate + render/bundle parity clean |


## Non-goals


- No behavioral change to the pi path: `[pi.agent]` → `[pi.agents]` is a config-key rename only.
  Applying pi overrides to pi's own agent files stays the pi extension's job, consumed via
  `atomic config resolve`. No pi agent-file writing or patching is added here.
- No committed `agents/*.md` edits — effort is applied to the installed copy at install time.
  Therefore no `make render` / `make bundle` unless a source artifact is actually touched.
- No new config schema version, no CLI `set`/`get` support for `[claude.agents]` (unchanged: it is
  machine-written via `atomic config agents`, not user-settable via `atomic config set`).


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

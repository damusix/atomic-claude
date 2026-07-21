# Spec: per-agent effort in the `[agents]` config block

Design + rationale: [`docs/design/agents-effort-config.md`](../design/agents-effort-config.md).
Structural model to mirror: [`internal/config/pi_agent.go`](../../atomic/internal/config/pi_agent.go).

This spec is read verbatim by fresh-context implementer subagents. Body = current truth;
history is in the change log. Follow [`rules/specs/spec-currency.md`](../../rules/specs/spec-currency.md)
when amending.


## Summary


Extend the `[agents]` block in `~/.atomic/config.toml` from a flat `agent → tier` string map
into a nested table per agent with two optional fields: `model` and `effort`. Both are
applied at install time by patching the agent frontmatter (`model:` and `effort:` keys).
Existing flat entries keep working and auto-migrate to nested on the next config write.


## Data model (`internal/config`)


Replace `Config.Agents map[string]string` with `map[string]AgentOverride`:

    type AgentOverride struct {
        Model  string `toml:"model,omitempty"`
        Effort string `toml:"effort,omitempty"`
    }

`AgentOverride` implements `encoding.TextUnmarshaler`:

    func (a *AgentOverride) UnmarshalText(b []byte) error { a.Model = string(b); return nil }

This is the back-compat seam: go-toml v2 calls `UnmarshalText` only for scalar values, so a
flat `agents.x = "opus"` decodes to `{Model: "opus"}` while a nested `[agents.x]` table
decodes into the struct fields. Proven against go-toml v2.3.1 (see design doc). Do **not**
add `EnableUnmarshalerInterface` or an `unstable.Unmarshaler` — the default decoder path is
what this relies on.

Do not implement `TextMarshaler` — marshaling must stay struct-based so `WritePersist`
(`toml.Marshal`) emits nested `[agents.<name>]` tables and a flat file auto-migrates to
nested on write.

`[agents]` stays in `opaqueSections` (structural unknown-key checks skipped; semantic checks
live in `Validate` / `AgentWarnings`).


## Validation


`Validate` (`config.go`):

- **effort**: must be one of `{"", low, medium, high, xhigh, max}` per agent, else hard error:
  `config: agents.<name>.effort: invalid effort "<v>"; must be one of: low, medium, high, xhigh, max`.
  Empty means "no effort override".
- **model**: lenient — never a hard failure. Drop the current `validTiers` hard-fail. An
  empty model means "no model override". (Malformed model → warning, see below.)

`AgentWarnings` (`config.go`):

- Preserve the existing unknown-agent warning (agent name not in the installed/known set).
- Add a malformed-model warning when `Model` is non-empty and contains internal whitespace
  or control characters: `config: agents.<name>.model: questionable value "<v>"; passed through as-is`.
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
and store. Nil out `cfg.Agents` when empty so the `[agents]` table is omitted.

The selector seam (`AgentTierSelector` / `DefaultAgentTierSelector`) keeps its
test-injection role; its signature becomes `func(cfg *Config) (map[string]AgentOverride, error)`.
`cli.go` call sites (`AgentTierSelector`, `applyAgentOverrides`) update to the new types. The
`ErrNonInteractiveAgents` / `ErrAgentsAborted` behavior is unchanged. Rename symbols away from
"tier"-only naming where it now misleads (e.g. `applyAgentTiers` → `applyAgentOverrides`);
keep the public `AgentTierSelector` name to avoid churn unless trivially safe to rename.


## Render (`internal/config/render.go`)


`Render` emits, per agent with an override, up to two dotted keys under the `[agents]`
section: `agents.<name>.model` and `agents.<name>.effort` (only for non-empty fields). Sorted,
byte-stable. This is the auto-loaded `config.resolved.md` view.


## Back-compat + migration


- A flat `[agents]\n atomic-implementer = "opus"` file loads as `{atomic-implementer: {Model:"opus"}}`.
- The next `WritePersist` (any `atomic config` write, including `atomic config agents`)
  re-marshals to nested `[agents.<name>]` tables — the migration is automatic, no separate pass.
- No config schema version bump is required (the `[agents]` table is machine-written and not
  part of `knownKeys`); the shape change is backward-compatible on read.


## Checkpoints


| # | Checkpoint | Acceptance | Key tests |
|---|-----------|------------|-----------|
| CP1 | Schema type + back-compat decode/encode | `AgentOverride` + `UnmarshalText`; `Config.Agents` is `map[string]AgentOverride`; `Validate` effort-strict / model-lenient; `AgentWarnings` gains malformed-model warning; `validTiers` hard-fail removed | Round-trip: flat string decodes to `{Model}`; nested table decodes to `{Model,Effort}`; effort-only decodes to `{Effort}`; `WritePersist` of a flat-loaded config emits nested tables; invalid effort → Validate error; malformed model → warning not error; well-formed arbitrary id (`claude-opus-4-8`, `claude-opus-4-6[1m]`) → no error/warning |
| CP2 | Install-time patch of `model:` + `effort:` | `loadAgentOverrides` + `patchAgentContent` handle both fields independently; effort-only patches only `effort:`; both-empty is a no-op; order preserved | model-only patch; effort-only patch (model frontmatter unchanged); both patched; neither → unchanged bytes; append when key absent; Plan SHA reflects both keys |
| CP3 | Interactive form + render | `atomic config agents` model Input + effort Select per agent; `applyAgentOverrides`; both-empty deletes entry; `Render` emits `.model`/`.effort` dotted keys | selector seam returns overrides → applied to cfg; empty-both removes entry; render output for model-only, effort-only, both; non-interactive + abort errors preserved |
| CP4 | Docs + discovery surfaces | `docs/reference` `[agents]` doc updated; `/atomic-help` cli topic mentions effort if it names `[agents]`; README updated if it documents `[agents]`; design/spec committed | `atomic validate` clean; `/atomic-help` MISSING-scan clean if artifacts touched (none expected) |


## Non-goals


- No change to the `pi` (`[pi.agent]`) path.
- No committed `agents/*.md` edits — effort is applied to the installed copy at install time.
  Therefore no `make render` / `make bundle` unless a source artifact is actually touched.
- No new config schema version, no CLI `set`/`get` support for `[agents]` (unchanged: it is
  machine-written via `atomic config agents`, not user-settable via `atomic config set`).


## Change log


- Initial spec (autopilot run for `agents-effort-config`). `[agents]` grows from a flat
  `agent → tier` string map to a nested `[agents.<name>]` table with optional `model` +
  `effort`; effort applied at install by patching the `effort:` agent frontmatter key;
  model validation relaxed from a hard tier allowlist to a lenient format check; flat
  entries read as `{model}` and auto-migrate to nested on write.

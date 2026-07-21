# Design: per-agent effort in the `[agents]` config block

Status: approved for implementation (autopilot run).
Spec: [`docs/spec/agents-effort-config.md`](../spec/agents-effort-config.md).


## Problem


The `[agents]` block in `~/.atomic/config.toml` lets a user pin a model tier per bundled
atomic agent (haiku/sonnet/opus/fable). It is a flat map:

    [agents]
    atomic-implementer = "opus"
    atomic-reviewer = "sonnet"

Claude Code's subagent frontmatter also honors an `effort` key that controls reasoning
effort. Users want to set that per agent — high effort for the implementer, max for the
reviewer — but `[agents]` has no field for it. Today the only way is to hand-edit each
installed `~/.claude/agents/*.md`, which the next `atomic claude install` / `atomic update`
would overwrite.

We already solve the analogous problem for the `pi` harness with `[pi.agent.<name>]`
tables carrying `model` + `thinking` ([`internal/config/pi_agent.go`](../../atomic/internal/config/pi_agent.go)).
This brings the same capability to the Claude `[agents]` side.


## What Claude Code actually honors


Verified against upstream Claude Code docs (sub-agents / model-config), via the
`claude-code-guide` agent:

- The subagent frontmatter `effort` field accepts exactly `low`, `medium`, `high`,
  `xhigh`, `max`. No integers.
- Unsupported-per-model values are gracefully downgraded at runtime (e.g. `xhigh` runs as
  `high` on a model that caps at `high`) — never an error. So the config validates against
  the five names and passes the value through as-is.

The repo's own authoring reference [`.claude/rules/authoring/agent-config.md`](../../.claude/rules/authoring/agent-config.md)
is slightly stale here (lists `low/medium/high/max` + integer, omits `xhigh`). Out of scope
to fix in this change; flagged as a follow-up.


## Shape


From a flat string map to a nested table per agent, both fields optional:

    [agents.atomic-implementer]
    model = "opus"          # tier alias OR bare model id; optional
    effort = "high"         # low|medium|high|xhigh|max; optional

    [agents.atomic-reviewer]
    effort = "max"          # effort alone — model stays the bundled default


## Decisions (settled with the user)


| Decision | Choice | Rationale |
|----------|--------|-----------|
| Structure | Nested `[agents.<name>]` table, mirror `[pi.agent]` | Two fields per agent need a table, not a scalar |
| `model` accepts | Tier alias (`opus`) **or** bare model id (`claude-opus-4-8`, `claude-opus-4-6[1m]`) | User: "model can also be a tier.. both are valid"; Claude Code resolves either |
| `model` validation | Lenient: non-empty, no internal whitespace (brackets allowed for the `[1m]` suffix). Never a hard failure. | Drops the current tier allowlist. "claude code will know what you mean" — a strict allowlist churns on every new model id |
| `effort` | Independent, optional — settable with or without `model` | User picked "Yes, independent": effort alone leaves the model at its bundled default |
| `effort` validation | Strict enum `{low, medium, high, xhigh, max}` | Closed set upstream; a typo should be caught |
| Applied | Install time — patch the `model:` and `effort:` agent frontmatter keys, re-applied every install/update | Same mechanism as today's tier patch; survives update cycles |
| Back-compat | Flat `agents.x = "opus"` still reads as `{model: "opus"}`; auto-migrates to nested on next write | No breakage for existing configs |


## Approaches considered


### A — `AgentOverride` struct + `encoding.TextUnmarshaler` (chosen)


`Config.Agents` becomes `map[string]AgentOverride` where `AgentOverride{Model, Effort string}`
implements `encoding.TextUnmarshaler`. go-toml v2 calls `UnmarshalText` **only for scalar
values** ([`go-toml/v2@v2.3.1/unmarshaler.go:774`](https://github.com/pelletier/go-toml) —
"Only try TextUnmarshaler for scalar types"), so:

- Flat `agents.x = "opus"` (scalar) → `UnmarshalText` → `{Model: "opus"}`.
- Nested `[agents.x]` (table) → normal struct decode → `{Model, Effort}`.

Marshaling emits nested `[agents.<name>]` tables (with `omitempty`), so any config write
auto-migrates a flat file to nested — the migration is a free side effect of the round trip,
no separate migration pass.

Verified end-to-end with a throwaway probe against v2.3.1: flat, nested, effort-only, and
mixed flat+nested-in-one-file all decode correctly and remarshal to nested tables.

Note: go-toml v2's *other* custom interface — `unstable.Unmarshaler` (`UnmarshalTOML([]byte)`
on raw bytes) — is gated behind `Decoder.EnableUnmarshalerInterface()` and is not called by
the default `toml.Unmarshal`. It is not used here.


### B — opaque `map[string]any`, hand-parsed (rejected)


Mirror `pi` exactly: keep `[agents]` as an opaque tree and parse it manually in `Load`
(like `ResolvePiAgents`). Works, but loses the typed struct, forces a hand-rolled parser and
a hand-rolled migration writer, and is more code than A for no benefit now that A is proven.


## Interactive UX (`atomic config agents`)


One `huh` group per run, two fields per agent:

- **model** — a free-text `Input` (not a Select), placeholder showing examples
  (`opus`, `claude-opus-4-8`), empty = no override. A Select cannot express an arbitrary
  model id, which the user explicitly wants — so model is text entry with the lenient
  format validator inline. This is a deliberate deviation from the original "two Selects"
  framing.
- **effort** — a `Select`: `(bundled default)`, `low`, `medium`, `high`, `xhigh`, `max`.

Both pre-populated from the current config. Clearing both removes the agent's entry.


## Scope


In: `internal/config` (schema, validation, render, interactive form, cli), `internal/claudeinstall`
(install-time patch). Out: no committed `agents/*.md` change (effort is applied to the installed
copy, not the source), so no `make render` / `make bundle`. No change to the `pi` path.

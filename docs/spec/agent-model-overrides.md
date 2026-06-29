# Agent model overrides

Child of [`docs/design/signals-wiki-unification.md`](../design/signals-wiki-unification.md) workstream F. Contracts config-driven, persistent per-agent model tier overrides applied at install time.

**Approach:** `config.toml [agents]` maps full agent filenames to tier strings (`haiku|sonnet|opus|fable`); `claudeinstall` reads the map at apply time and patches `model:` in each agent file's frontmatter via `internal/frontmatter.Parse`+`Emit`; `atomic config agents` (huh) is the only write path. See [`docs/design/signals-wiki-unification.md`](../design/signals-wiki-unification.md) §"Config-driven agent model overrides (workstream F)".

## Goal

Users pin any installed atomic agent to a cost tier via `atomic config agents`. The tier is stored in `config.toml [agents]` (machine-owned) and re-applied on every `atomic claude install` or `update`, so upgrades never clobber the choice. No override → bundled default stands.

## Non-goals

- No per-session or per-task override. `config.toml` is the durable floor; a per-session nudge stays in memory.
- No hand-editing of `config.toml`. `atomic config agents` is the only supported write path.
- No override of agents the user added manually to `~/.claude/agents/`. Only bundled artifacts tracked by `[install.artifacts]` are patched.
- Exact Claude Code model ID resolution (e.g., `haiku` → full model string) is deferred to the agent runtime; this spec validates the tier label only.

## Success criteria

- **SC-DSV (dispatch-site verification — load-bearing gate):** No command or agent template passes `model:` as an explicit Agent tool parameter when dispatching any atomic agent (`atomic-implementer`, `atomic-reviewer`, `atomic-strategist`, `atomic-investigator`, `atomic-wiki-inferrer`). Frontmatter is the sole lever. If any dispatch site hardcodes `model:` at call time, the frontmatter patch is dead and the mechanism must pivot to dispatch-time override before any build work begins.
- **SC1:** `config.toml [agents]` accepts full agent filename keys (e.g., `atomic-implementer`) with tier values from `{haiku, sonnet, opus, fable}`. `atomic config agents` writes them interactively via huh; `config.WritePersist` persists.
- **SC2:** `checks_config.go` / `config.Validate` fails on any `[agents]` value outside the allowlist; unknown agent-filename keys produce a warning (non-fatal).
- **SC3:** `atomic claude install` and `atomic claude update` patch `model:` in each agent file listed under `[agents]`; an absent entry leaves the bundled default unchanged.
- **SC4:** A fresh install with overrides set, followed by a binary upgrade and reinstall, preserves the tier choices. The tier is re-derived from config on every install, never baked into the installed file as truth.
- **SC5:** `config.resolved.md` regeneration reflects active `[agents]` overrides so the auto-loaded resolved config is accurate.
- **SC6:** `docs/reference/agents.md` documents the override mechanism; `/atomic-help` covers `atomic config agents`; `atomic validate artifacts` (A1) passes on `cliusage.go` registration.

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Dispatch-site verification — search every command and template for `model:` as an Agent tool argument when dispatching `atomic-*` agents; confirm no hardcoded model at call time | `commands/`, `templates/commands/`, `agents/*.md` (frontmatter) | atomic-investigator | 0 (read-only) | SC-DSV: all atomic-agent dispatch sites confirmed model-free at call time; unblocks CP2–4; if any site found, pivot before proceeding |
| 2 | Config schema extension — add `[agents] map[string]string` to `Config` struct (config schema v2 addition); allowlist validation in `config.Validate` and `checks_config.go` | `atomic/internal/config/config.go`, `atomic/internal/doctor/checks_config.go` | atomic-implementer (surgical) | 2 | SC1 + SC2: valid tier stored and loaded; invalid tier surfaces as `FAIL`; round-trip through `config.WritePersist` + `config.Load` is clean |
| 3 | `atomic config agents` interactive verb — huh-driven per-agent tier prompt; `cliusage.go` registration + A1 description | `atomic/cmd/atomic/main.go` (or dedicated handler), `atomic/internal/cliusage/cliusage.go` | atomic-implementer (surgical) | 2–3 | SC1: `atomic config agents` prompts tier selection, writes `[agents]` table, exits 0; A1 lint passes; `atomic config agents --help` registered |
| 4 | Install-time frontmatter patch — `claudeinstall.Apply` (or `applyAction`) reads `[agents]`, calls `frontmatter.Parse`+`Emit` to rewrite `model:` for each configured agent before writing to `~/.claude/agents/` | `atomic/internal/claudeinstall/install.go` | atomic-implementer (surgical) | 1–2 | SC3 + SC4: install with override patches `model:` in installed file; re-install after upgrade re-applies; absent entry leaves bundled default unchanged; `config.Load` not called before the agent file exists on disk |
| 5 | Surfaces — `config.resolved.md` generation reflects active overrides; `docs/reference/agents.md` documents the mechanism; `templates/commands/atomic-help.md` updated; `make render` + `make -C atomic bundle` clean | `docs/reference/agents.md`, config resolved rendering path, `templates/commands/atomic-help.md`, rendered `commands/atomic-help.md` | atomic-implementer (feature) | ~5 | SC5 + SC6: overrides visible in rendered resolved config; reference doc accurate; MISSING-scan zero; bundle parity clean (`git diff --exit-code` on both render and bundle) |

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| A command hardcodes `model:` at Agent dispatch for an atomic agent → frontmatter patch is dead; lever must move to dispatch-time | Low (CP1 verifies before any build) | High — entire mechanism invalid | CP1 is the go/no-go gate; if any site found, spec is amended to dispatch-time override before CP2 starts |
| `frontmatter.Emit` changes key order or whitespace → round-trip alters unrelated frontmatter fields in agent files | Low | Medium — breaks other tooling or tests | Use `ParseOrdered`+`EmitOrdered` (already exists in `internal/frontmatter`) to preserve input key order; add round-trip test per agent file |
| Workstream C `[install]` table and config schema v2 machinery not yet merged when F starts → `Config` struct conflicts or `config.Validate` diverges | Medium | Medium — struct merge conflict | Gate CP2 on C's config package changes landing on the branch first; F adds `[agents]` to an already-v2 struct |
| `fable` tier is not a recognized `model:` value in the Claude Code agent runtime → silent fallback to session model | Low–Medium | Low — functionally degrades gracefully; user's intent not honored | Document in `checks_config.go` comment that `fable` is a forward-reserved placeholder; note in `docs/reference/agents.md` |
| User has manually edited an installed agent's `model:` line → install overwrites the edit | Expected (managed artifact behavior) | Low — documented contract | Document in `docs/reference/agents.md`: managed artifacts are overwritten on install; `config.toml [agents]` is the supported customization path |

## Change log

(none)

# Workstream D: Unify commands, agent, and skill

## Goal

One refresh verb (`/refresh-wiki`), one inferrer agent (`atomic-wiki-inferrer`), and one skill (`atomic-wiki`) operating as a skill-router that dispatches repo vs. realm scope by reading `<wiki-type>`. All dispatch sites, help-router rows, reference tables, and `atomic-setup` scope detection updated to match.

## Non-goals

- No Go binary changes — `cliusage.go` verb registration is workstream A (Cobra migration).
- No changes to scan, stale, or drift logic — workstream E.
- No migration framework or `config.toml` schema changes — workstream C.
- Non-repo realm ("git-wiki") handling in `atomic-setup` scope detection — deferred.
- `docs/wiki/` storage layout and OKF frontmatter — workstream B (prerequisite; must be merged before this workstream begins).

## Success criteria

1. `templates/commands/refresh-signals.md` and `commands/refresh-signals.md` are absent; `templates/commands/refresh-wiki.md` and rendered `commands/refresh-wiki.md` handle both repo and realm refresh.
2. `templates/agents/atomic-wiki-inferrer.md` and rendered `agents/atomic-wiki-inferrer.md` exist; all `atomic-signals-inferrer` variants are absent from the repo.
3. `atomic-wiki-inferrer`'s system prompt contains an explicit clause naming `docs/wiki/CLAUDE.md` as authoritative steering to read and follow (delivery via nested-memory is automatic; compliance requires explicit instruction).
4. `skills/atomic-wiki/SKILL.md` contains only conversational ops, the scope-read step (`<wiki-type>`), and routing logic; heavy scan→infer→synthesize workflow text lives in `skills/atomic-wiki/references/repo.md` and `skills/atomic-wiki/references/realm.md`, loaded on demand.
5. `templates/commands/atomic-setup.md` writes `<wiki-type>repo</wiki-type>` or `<wiki-type>realm</wiki-type>` into `claude.local.md` (if it exists) else `CLAUDE.md`, using the three-case detection rule: not-a-repo + `wiki/` present → `realm`; repo + no `wiki/` → `repo`; repo + `wiki/` present → ask (treat as `repo` if user cancels or is ambiguous).
6. `templates/commands/atomic-help.md` rows reflect the rename and removal; the verification command (`for cmd in commands/*.md; do verb=$(basename "$cmd" .md); [ "$verb" = "atomic-help" ] && continue; grep -q "/$verb" templates/commands/atomic-help.md || echo "MISSING: /$verb"; done`) returns zero `MISSING:` lines.
7. `make render && git diff --exit-code` passes; `make -C atomic bundle && git diff --exit-code` passes.
8. No committed artifact, template, command, or doc contains a reference to `atomic-signals-inferrer` or `/refresh-signals`.

## Approach

Rename the agent template and rendered output, fold the removed slash command into `/refresh-wiki`, extend the skill into a thin router with `references/` sub-files, wire `atomic-setup` detection, update all dispatch sites, and update all ripple surfaces in one logical slice; see [docs/design/signals-wiki-unification.md](../design/signals-wiki-unification.md) §Skill-router architecture and §Loading mechanism for the authoritative contracts.

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|-----------|-------------|-------|-----------|---------|
| 1 | Fold `/refresh-signals` into `/refresh-wiki`; remove refresh-signals template and rendered output | `templates/commands/refresh-wiki.md`, `templates/commands/refresh-signals.md` (delete), `commands/refresh-wiki.md`, `commands/refresh-signals.md` (delete) | atomic-implementer surgical | 4 | SC 1 — `refresh-signals` absent; `/refresh-wiki` body covers both repo and realm |
| 2 | Rename agent template and rendered output; add authoritative-steering instruction to system prompt | `templates/agents/atomic-signals-inferrer.md` → `templates/agents/atomic-wiki-inferrer.md`, `agents/atomic-signals-inferrer.md` → `agents/atomic-wiki-inferrer.md` | atomic-implementer surgical | 2 | SC 2, SC 3 — old filenames absent; new filenames present; explicit `docs/wiki/CLAUDE.md` clause in system prompt |
| 3 | Extend skill into a thin router; create `references/repo.md` and `references/realm.md` | `skills/atomic-wiki/SKILL.md` (extend description + add routing section), `skills/atomic-wiki/references/repo.md` (new), `skills/atomic-wiki/references/realm.md` (new) | atomic-implementer feature | 3 | SC 4 — SKILL.md triggers + routing only; references/ carry full per-scope pipeline |
| 4 | Update all dispatch sites: agent name in commands and shared partials | `templates/shared/signals-gate.md`, `templates/commands/subagent-implementation.md`, `templates/commands/autopilot.md`, `templates/commands/refresh-wiki.md`, any other template that names the old agent; re-rendered outputs under `commands/` | atomic-implementer feature | 6–8 | SC 8 — grep for `atomic-signals-inferrer` in templates/ returns no matches |
| 5 | Wire `atomic-setup` scope detection to write `<wiki-type>` | `templates/commands/atomic-setup.md`, rendered `commands/atomic-setup.md` | atomic-implementer surgical | 2 | SC 5 — detection logic present with three-case rule; writes into `claude.local.md` when it exists, else `CLAUDE.md` |
| 6 | Ripple: `/atomic-help`, README, `docs/reference/` tables, `CLAUDE.md` workflow ordering | `templates/commands/atomic-help.md`, `commands/atomic-help.md`, `README.md`, `docs/reference/commands.md`, `docs/reference/agents.md`, `docs/reference/signals-workflow.md`, `CLAUDE.md` | atomic-implementer feature | 7 | SC 6, SC 8 — help rows updated; verification command returns zero MISSING lines; no dangling old names in reference tables or CLAUDE.md |
| 7 | `make render` + `make bundle` | All rendered `commands/`, `agents/`, `atomic/internal/embedded/bundle/` | n/a (shell) | — | SC 7 — both drift gates pass |

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Orphan check in `make render` halts because old rendered files outlive their deleted templates | High (will fire) | CI fails "Verify render is committed" gate | Delete `commands/refresh-signals.md` and `agents/atomic-signals-inferrer.md` in the same commit as their template deletions; run `make render` locally to confirm zero orphan errors before committing |
| Dispatch sites that reference `atomic-signals-inferrer` by name are missed | Medium | Stale agent name dispatched at runtime; SC 8 fails | After CP 4, run `git grep -r "atomic-signals-inferrer" templates/ commands/ agents/ skills/` — must return zero matches before proceeding to CP 5 |
| `docs/wiki/CLAUDE.md` authoritative-steering clause in the agent system prompt is too weak; dispatched subagent ignores it | Medium | Inferrer skips steering at runtime | The clause must be imperative and explicit, not advisory; example: "Before inferring, read `docs/wiki/CLAUDE.md` and treat its instructions as authoritative steering for this run." Verify by reading the rendered agent after CP 2 |
| `references/repo.md` and `references/realm.md` grow to duplicate each other's shared pipeline text | Low | Maintenance drift between scopes | Keep shared pipeline preamble in `SKILL.md` (one copy); only scope-specific paths and storage targets go in each `references/` file |
| `atomic-setup` asks on `repo + has wiki/` but user cancels; `<wiki-type>` is never written | Low | Future inferrer calls cannot detect scope | Default to `repo` on ambiguous cancel and write it; note this decision in the command output |

## Change log

(none)

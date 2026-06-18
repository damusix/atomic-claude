# Visual options for planning

## Goal

Add an `atomic-visual-options` skill that renders visual planning choices as a throwaway,
self-contained HTML file and collects the user's pick as typed terminal codes, and wire
`/atomic-plan` to invoke it just-in-time when a design question is visual.

## Non-goals

- No server, websocket, click capture, telemetry, auth, or live reload.
- No committed HTML asset — the file is gitignored scratch; only the chosen codes persist, in the design doc.
- Not for conceptual or text decisions — those stay in the terminal.
- No `atomic` binary subcommand and no Go code — the model authors the HTML directly.

## Success criteria

- [ ] `skills/atomic-visual-options/SKILL.md` exists with valid frontmatter (`name`, `description`); the description auto-fires on visual-option phrasing and states it is invoked by `/atomic-plan`.
- [ ] SKILL.md defines all of: the see-it-over-read-it gate, the panel/code model (panels = dimensions, `A1`/`A2` codes), a self-contained HTML scaffold (inline styles, no external fetch, no client JS by default, `prefers-color-scheme`), the scratchpad output location, the typed code-selection grammar, iterate-by-overwrite, and recording chosen codes into the design doc.
- [ ] `/atomic-plan` (`templates/commands/atomic-plan.md`) invokes the skill just-in-time when a design question is visual; the rendered `commands/atomic-plan.md` matches the template.
- [ ] `CLAUDE.md` registers the skill in the planning-workflow context.
- [ ] `docs/reference/skills.md` lists the skill with its trigger phrases.
- [ ] `/atomic-help` topic `skills` and tour Stage 1 reflect the new skill (count and roster).
- [ ] No occurrence of the upstream third-party project name in any added or edited file.
- [ ] Green: `make render` diff-clean, `make -C atomic bundle` diff-clean, `atomic validate` passes, the `/atomic-help` MISSING-scan returns zero, `go build ./...` succeeds.

## Approach

Static self-contained HTML in the scratchpad plus typed terminal code selection — see `docs/design/visual-options.md`.

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Author the skill | `skills/atomic-visual-options/SKILL.md` | atomic-implementer (mode: feature) | ~1 | frontmatter valid; gate + panel/code model + HTML scaffold + grammar + design-doc recording all present; no third-party name |
| 2 | Wire and register | `templates/commands/atomic-plan.md`, `templates/commands/atomic-help.md`, `CLAUDE.md`, `docs/reference/skills.md` | atomic-implementer (mode: feature) | ~4 | MISSING-scan zero; skill named in plan + help (topic + tour) + CLAUDE.md + skills.md; descriptions agree with SKILL.md |

Render and bundle regeneration (`make render`, `make -C atomic bundle`) run at each green checkpoint before commit; the final whole-suite verification runs in the ship phase.

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Help-router drift — skill not discoverable | med | `/atomic-help` MISSING-scan in verify; checkpoint 2 updates topic `skills` + tour Stage 1 explicitly |
| Stale render/bundle trips CI drift gate | med | orchestrator runs `make render` + `make -C atomic bundle` before each commit; parity re-checked in verify |
| Skill over-fires on non-visual UI questions | med | gate section encodes the see-it-over-read-it test and "UI topic is not automatically a visual question" |
| Upstream project name leaks into shipped artifacts | low | success criterion grep across the diff; reviewer checks |

## Change log

<!-- Populated on first amendment after approval. -->

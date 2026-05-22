# Documentation skill + command split

## Goal

Split `/documentation` into two artifacts: a new `atomic-documentation` skill that owns *what* to document and *where* (rules: surface taxonomy, voice differences, human-facing vs LLM-facing distinction), and a thinned `/documentation` command that owns the *flow* (orchestration: scan diff, walk surfaces, invoke skill, apply edits). Auto-invoke the skill from ship verbs so doc-impact gets a just-in-time check during commit synthesis without forcing users to remember `/documentation`.

## Non-goals

- Removing `/documentation` as a user-visible verb. The command stays; users invoke it for explicit full-repo doc sync passes.
- Replacing `atomic-prose`. That skill keeps its narrow role (voice/tone for enduring narrative docs); `atomic-documentation` calls it as a callee when the surface is human-facing prose.
- Changing spec/design voice rules. The terse "tables-first, brevity-dominant" convention for `docs/spec/` and `docs/design/` is preserved verbatim and codified inside the new skill.
- Auto-applying doc edits during commit synthesis. The skill proposes; the user accepts/rejects/skips per surface.
- Extending doc surfaces beyond README, CLAUDE.md, guides, specs, designs, signals/inferred, and project-local refs. Other surfaces (CHANGELOG, agents/skills/commands prose, etc.) follow the closest matching rule.

## Success criteria

- [ ] `skills/atomic-documentation/SKILL.md` exists with a description that auto-fires on **narrow, diff-impact phrases** ("doc this change", "what surfaces does this touch", "doc impact for this diff", "what needs documenting") and explicitly states it is invoked by `/documentation` and by ship verbs. Description must NOT include generic phrases like "update the docs" or "write the docs" — those route to `atomic-prose`.
- [ ] Skill description encodes the boundary against `atomic-prose`: "For raw prose drafting (README intro, guide narrative), `atomic-prose` owns. This skill owns diff-driven surface impact only." `atomic-prose` description gets a reciprocal one-line edit pointing back.
- [ ] Skill encodes the surface taxonomy (README, CLAUDE.md, guides, specs, designs, signals refs) with one row per surface: audience, voice, when-to-update, when-NOT-to-update.
- [ ] Skill encodes the four-voice distinction (atomic TUI / atomic-prose / spec-design / LLM-reference) and routes each surface to the right voice.
- [ ] `/documentation` command is rewritten to be a thin orchestrator: scope detection → invoke skill → walk proposed surfaces → apply edits → stage. Voice rules and surface taxonomy are deleted from the command and live only in the skill.
- [ ] Ship verbs (`/commit-only`, `/commit-and-push`, `/commit-and-pr`, `/commit-and-merge`, `/commit-and-squash`, `/squash-only`, `/squash-and-merge`) invoke `atomic-documentation` between `atomic-signals` (existing) and `atomic-commit` (existing).
- [ ] Ship-verb invocation is scoped to the staged diff (just-in-time mode), not the whole branch.
- [ ] `/documentation` invocation is scoped to a user-supplied range (`HEAD~5..HEAD`, `main..HEAD`, etc.) or defaults to `<base>..HEAD` (full mode).
- [ ] When the skill returns "no surfaces affected", ship verbs proceed silently — no extra prompt.
- [ ] When surfaces are affected, ship verbs prompt: edit now / skip with reason / continue (skill misclassified). Skip-with-reason appends a single `doc-skip: <reason>` line to the commit **trailer block** (after the body's terminating blank line), placing it in `git interpret-trailers --parse` range.
- [ ] `atomic-commit` skill instructs the model to preserve `doc-skip:` trailer lines verbatim when supplied by the caller (additive instruction; no existing strip rule needed to amend).
- [ ] Bundle regen after edits; both new SKILL.md and revised command.md are included.
- [ ] `CLAUDE.md` and `CLAUDE.md` updated to list the new skill in the skills section and to update the `/documentation` description.
- [ ] `README.md` skills table updated.
- [ ] Spec change-log + signals refresh.

## Conceptual model

| Layer | Owns | Example artifact |
|-------|------|------------------|
| **Skill** (`atomic-documentation`) | *Rules*: what counts as a doc surface, what voice to use, what triggers an edit, how to detect human-facing vs LLM-facing | `skills/atomic-documentation/SKILL.md` |
| **Command** (`/documentation`) | *Flow*: when to run, how to gather diff scope, how to apply skill output, how to stage | `commands/documentation.md` |
| **Ship verbs** | *Trigger*: invoke skill in just-in-time mode during commit synthesis | `commands/commit-only.md` and family |

Matches the existing `atomic-commit` skill (rules) ↔ ship verbs (flow) split. Single pattern reused.

## Four voices, four surfaces (skill-owned)

The skill is the canonical home for this table. Today it lives partially in `CLAUDE.md` § "Three doc voices, three surfaces", partially in `commands/documentation.md`. After this spec lands, the skill is authoritative; CLAUDE.md references the skill by name.

| Voice | Surface | Audience | Style rules |
|-------|---------|----------|-------------|
| **Atomic TUI** | Claude's chat replies | The human at the terminal, right now | Terse, fragments OK, drop articles. Governed by `output-styles/atomic.md`. Never appears in files. |
| **Atomic-prose** | `README.md`, `docs/guides/*`, CHANGELOG narrative | Humans skimming for what + why + how | Clear, specific, active-voice technical prose. No em dashes, no marketing, no AI-tell. Skill `atomic-prose` enforces. |
| **Spec/design** | `docs/spec/*`, `docs/design/*` | Future implementers + agents | Tables, Mermaid, terse bullets. Prose only where a contract needs sentences. Token-cost-aware. Append-mostly for specs. **Never** invokes `atomic-prose`. |
| **LLM-reference** | `CLAUDE.md`, `CLAUDE.md`, `.claude/project/*-signals.md`, `claude.local.md` | Future Claude sessions | Technical-imperative. Conventions, paths, dispatch contracts. No restating code, no tutorial, no narrative. Lean: every line earns its slot. |

## Surface routing (skill-owned)

When the skill receives a diff, it classifies each changed entity against this routing table:

| Diff signal | Surface(s) | Voice |
|-------------|-----------|-------|
| New file in `commands/<name>.md` | `README.md` commands table + `CLAUDE.md` "Other commands" line + `CLAUDE.md` mirror | atomic-prose (README) + LLM-reference (CLAUDE.md) |
| New file in `agents/atomic-*.md` | `README.md` agents table + `CLAUDE.md` "Subagents available" entry | atomic-prose + LLM-reference |
| New file in `skills/atomic-*/SKILL.md` | `README.md` skills table + commands that invoke it | atomic-prose + LLM-reference |
| Public-API change in `atomic/cmd/atomic/main.go` (new top-level flag, new subcommand) | `docs/reference/commands.md` + matching `docs/spec/<topic>.md` change-log | spec/design |
| New key in `atomic/internal/config/config.go` | `docs/spec/atomic-state-and-config.md` change-log + `config.resolved.md` (auto-rendered, no manual edit) | spec/design |
| New check in `atomic/internal/doctor/checks_*.go` | `docs/spec/atomic-doctor.md` change-log | spec/design |
| Behavior-changing edit to existing `docs/spec/<topic>.md` body | Same file's `## Change log` section | spec/design |
| Surface listed in user's `claude.local.md` "documentation surfaces" override | Surface as listed | as declared |
| Pure refactor / internal rename with no public surface change | (no surface) | n/a — skill returns "no doc impact" |

For non-atomic repos, the routing table is overridden via the calling project's `claude.local.md` declaring its own surfaces. Skill ships atomic defaults + documents override format.

## Override format for other repos

A repo may declare custom surfaces. Search order matches `atomic-signals` precedent verbatim: `claude.local.md` / `CLAUDE.local.md` first (treated as a pair — whichever exists on this filesystem), then `claude.md` / `CLAUDE.md` (same pair semantics). First file containing a `## Documentation surfaces` heading wins; remaining files ignored. The pair phrasing accommodates case-sensitive filesystems (Linux ext4) and case-insensitive ones (macOS APFS default) without forcing a choice.

```markdown
## Documentation surfaces

| Diff signal | Surface | Voice |
|-------------|---------|-------|
| New file in `src/api/routes/*.ts` | `docs/api.md` | atomic-prose |
| Public function added to `pkg/*/exports.go` | `docs/reference.md` | spec-design |
```

Skill reads this section if present; merges with built-in defaults; user overrides win on collision. If no override section exists in any of the searched files, skill emits atomic-defaults-only (which return empty surfaces on foreign repos — clean degradation).

**Discoverability**: `/documentation --print-template` emits the table skeleton into stdout for the user to paste. Skill body mentions this affordance so first-time users in foreign repos see the path forward.

## Command flow (`/documentation`)

```mermaid
flowchart TD
    A[/documentation] --> B{user args?}
    B -- range given --> C[git diff <range>]
    B -- no args --> D[git diff <base>..HEAD]
    C --> E[invoke atomic-documentation skill with diff]
    D --> E
    E --> F{surfaces returned?}
    F -- empty --> G[print 'no doc impact'; exit]
    F -- non-empty --> H[walk surfaces; per-surface accept/edit/skip]
    H --> I[apply edits; stage]
    I --> J[print summary]
```

Caption: command supplies the diff scope and the apply loop; skill supplies the surface taxonomy and voice routing.

## Ship-verb integration

`/commit-only` is the canonical pipeline. Other ship verbs either delegate to it (`/commit-and-pr` etc.) or inline the same steps; wiring goes wherever the inline pipeline lives. Audit each verb for inheritance before editing.

Real current `/commit-only` order (per `commands/commit-only.md`): commit-msg-prep → diff inspection → session-reports read → stage → signals stale-gate → commit (HEREDOC) → reports delete → confirm.

New step inserts **after staging, before signals**:

```
1. commit-msg-prep                                   [existing]
2. status / diff / log                               [existing]
3. read session reports                              [existing]
4. stage explicit paths                              [existing]
5. invoke atomic-documentation on staged diff        [NEW]
   - skill emits final ```yaml block listing surfaces
   - caller parses last yaml block; missing or unparseable → treat as empty
   - empty: skip step
   - non-empty: prompt user per surface
     * edit: open file, apply edits, re-stage
     * skip: typed reason → appended to commit trailer as 'doc-skip: <reason>'
     * continue: treat as misclassification; no edit, no skip-line
6. signals stale-gate + skill                        [existing — catches new doc files staged in step 5]
7. atomic-commit synthesis + commit                  [existing]
8. delete reports                                    [existing]
9. confirm                                           [existing]
```

Why doc-before-signals: new doc files staged at step 5 must be picked up by signals at step 6 in a single pass. Doc-after-signals would force a second stale-gate. One pass.

Skill in ship-verb mode is **scoped to staged diff only**; in command mode (`/documentation`), it accepts a wider range.

## Skill output contract

Skills are markdown system-prompt fragments, not RPC endpoints — they cannot "return" structured data. Pattern is **LLM-mediated handoff**: the skill instructs the model to emit, as its **final block**, a fenced YAML list in the shape below. The caller (ship verb or `/documentation`) parses the last ```yaml fenced block in the model's output.

```yaml
surfaces:
  - path: README.md
    voice: atomic-prose
    reason: new file commands/foo.md
    suggested_change: |
      Add row to commands table:
      | `/foo` | Description |
  - path: CLAUDE.md
    voice: llm-reference
    reason: new file commands/foo.md
    suggested_change: |
      Append to "Other commands" line: `/foo` (<one-line behavior>)
```

Voice values: `atomic-prose | spec-design | llm-reference`.

**Parser contract (caller side)**:

1. Search model output for the last fenced code block tagged ```yaml or ```yml (alias). Both accepted.
2. If found, parse as YAML. On success, iterate `surfaces`. On parse error, fall back to "no surfaces".
3. If no fenced ```yaml/```yml block is present, treat as "no surfaces" — the skill found no doc impact.
4. If parsed YAML lacks a `surfaces` key or `surfaces` is not a list, treat as "no surfaces".
5. Surfaces with unknown `voice` values are logged + skipped; do not abort.
6. Surface entries missing required fields (`path`, `voice`) are logged + skipped; do not abort.
7. Empty `surfaces: []` list is valid and means "explicitly nothing to update".

This is a new pattern in the codebase. Existing skills (`atomic-signals`, `atomic-commit`) emit free text the caller acts on conversationally; `atomic-documentation` introduces structured handoff because per-surface accept/reject prompts need a clear item list. **The skill body must include a "Why structured handoff here" note** explaining this is the only skill using fenced-yaml handoff so future authors don't apply the pattern accidentally elsewhere.

## CLAUDE.md edits

`CLAUDE.md` is always loaded; skills are not. The voice-surface mapping (which surface uses which voice) is load-bearing context and must stay in CLAUDE.md. Only the **routing table** (diff signal → surface) moves to the skill.

CLAUDE.md today has three bullets (atomic TUI / atomic-prose / spec-design). This edit **expands to four bullets, adding LLM-reference** (CLAUDE.md + signals files + `claude.local.md`), and compresses each to a one-line header without rationale prose. Net effect: +1 voice (LLM-reference is new explicit context), -N lines of rationale (which moves to the skill). Final form ends with: "Diff-signal → surface routing lives in the `atomic-documentation` skill. Invoke `/documentation` to apply, or let ship verbs fire it automatically on staged diffs."

The section name updates from "Three doc voices, three surfaces" to "Four doc voices, four surfaces" to match the new bullet count.

`CLAUDE.md` (project mirror) gets the same edit.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Create skill scaffold with frontmatter + description | `skills/atomic-documentation/SKILL.md` | Description triggers auto-fire phrases; lists ship-verb invocation. Skill body includes a "Why structured handoff here" note explaining that fenced-yaml emission is unique to this skill (per-surface accept/reject needs a clear item list) and not a general pattern |
| 2 | Move voice taxonomy + routing table from command to skill | `skills/atomic-documentation/SKILL.md` (body) | All four voices and surface-routing table present; matches CLAUDE.md vocabulary |
| 3 | Document override format for non-atomic repos | `skills/atomic-documentation/SKILL.md` | Override section with example markdown table |
| 4 | Rewrite `/documentation` as orchestrator only | `commands/documentation.md` | Voice rules gone from command; command invokes skill; flow steps preserved |
| 5 | Audit ship-verb inheritance vs inline pipelines | `commands/commit-only.md`, `commit-and-{push,pr,merge,squash}.md`, `squash-only.md`, `squash-and-merge.md` | One-line note per verb: "delegates to /commit-only" or "inlines steps"; only inlined ones need edits |
| 6 | Wire ship-verb invocation in `/commit-only` (and any inliners found in CP5) | files identified in CP5 | Skill invoked at step 5 (after stage, before signals); doc-skip plumbing wired to commit trailer |
| 7 | Add `atomic-prose` callee declaration | `skills/atomic-prose/SKILL.md` | Description line states "Invoked as callee by `atomic-documentation` when surface is human-facing prose" |
| 8 | Add doc-skip preservation instruction to `atomic-commit` | `skills/atomic-commit/SKILL.md` § "Supplemental input" | Additive: "Preserve `doc-skip: <reason>` trailer lines verbatim when present" (no existing strip rule to amend; net-new instruction) |
| 9 | Adjust CLAUDE.md voice section: keep 4-bullet surface map, move routing taxonomy to skill | `CLAUDE.md` + `CLAUDE.md` | Both files in sync; surface map remains always-loaded; routing pointer added |
| 10 | Update `docs/reference/skills.md` | `docs/reference/skills.md` | New row for atomic-documentation; matches existing format |
| 11 | Update README.md skills table | `README.md` | New row for atomic-documentation; one-line description |
| 12 | Bundle regeneration — final parity check | `make -C atomic bundle`; verify `git diff --exit-code atomic/internal/embedded/` | Final bundle parity. **Per-checkpoint regen is handled automatically by `.githooks/pre-commit` when installed** (any commit touching `agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, or root `CLAUDE.md` triggers regen and re-stages bundle outputs). If the hook is absent, prior checkpoints touching source artifacts (CP1, CP4, CP6, CP7, CP8) must each regen-and-stage in their own commit. CP12 is the final exit check, not the only regen point |
| 13 | Spec change-log + signals refresh | `docs/spec/documentation-skill-split.md` change-log section + `/refresh-signals` | Spec captures shipped outcome; signals reflect new skill |

## Naming

- Skill: `atomic-documentation` (per `atomic-` prefix convention in `CLAUDE.md` § Naming).
- Command: `/documentation` (kept; users have muscle memory).
- Skill file: `skills/atomic-documentation/SKILL.md`.

Rejected: `atomic-doc-impact`, `atomic-docs`, `atomic-doc-review`. The skill is broader than just diff-impact (it also owns voice rules even in full-sync mode), so `documentation` is the right scope.

## Interaction with existing skills

| Existing skill | Relationship |
|---------------|--------------|
| `atomic-prose` | Callee. `atomic-documentation` invokes it whenever the target surface is README, guides, or CHANGELOG narrative. |
| `atomic-signals` | Sibling. Both fire in ship-verb flows; signals fires first (refreshes project map), documentation fires second (consumes the up-to-date map). |
| `atomic-commit` | Sibling. Documentation fires before commit message synthesis. Doc-skip lines flow through to body. |
| `atomic-tdd`, `atomic-verify`, `atomic-debug` | No direct relationship. Documentation is a doc-surface skill, not a code-quality skill. |
| `atomic-review` | Indirect. `atomic-review` rules cover PR comment compression; if PR body needs doc references, `atomic-documentation` may surface them but does not generate review comments. |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Ship verbs become slow because skill auto-fires on every commit | medium | Skill returns empty fast when diff has no surface signal; benchmark on no-op commits; target <200ms overhead |
| Users habituate to "skip with reason" and ship undocumented changes | medium | `doc-skip:` lines visible in git log; `/follow-up review`-style review of skip patterns becomes possible later; not in v1 |
| Voice table drift between skill and CLAUDE.md after edits | high | CLAUDE.md becomes a one-paragraph pointer (checkpoint 7); single source of truth for voice rules is the skill |
| Non-atomic repos break because they have no `claude.local.md` overrides and atomic defaults don't apply | medium | Skill defaults are conservative: no surface match → returns empty; no false positives on foreign repos |
| Skill misclassifies and proposes edits to surfaces the user actually doesn't want touched | medium | Per-surface accept/skip prompt; no auto-apply; `continue` disposition treats as misclassification cleanly |
| `doc-skip:` lines clutter commit bodies | low | One line per skip; only present when surface was flagged and skipped; no skip line for `continue` (misclassification) |
| Old `/documentation` content (voice rules section, etc.) deleted but referenced elsewhere | medium | Grep for `"docs/guides/"`, `"atomic-prose"`, `"Three doc voices"` after rewrite; redirect any remaining references to the skill |
| `atomic-prose` and `atomic-documentation` race on the same surface (both auto-fire on "draft the README") | low | `atomic-documentation` is the entrypoint; it invokes `atomic-prose` as a callee. Trigger phrases overlap is fine because the skill chain resolves it |
| New skill description triggers spuriously on unrelated phrases | medium | Trigger list explicit and narrow ("doc this change", "doc impact for this diff", "what needs documenting", "what surfaces does this touch"); avoid generic "documentation"; "update the docs" / "write the docs" route to `atomic-prose` instead |

## Open questions

- Should the skill output also include "missing-surface" findings (e.g., new public function but no `docs/spec/` entry exists)? Or strictly "edit-this-existing-surface"? v1: existing surfaces only; add missing-surface detection in a later increment.
- Should `/documentation` gain a `--dry-run` flag that prints the skill's proposal without applying? Probably yes; defer to checkpoint 4 author's discretion since it's a small addition.

## Change log

<!-- Populated on first amendment after the spec is approved. -->

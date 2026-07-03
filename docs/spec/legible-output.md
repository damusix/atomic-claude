# Spec: legible viewing options for responses


Implements GitHub issue #113. Approach and rationale: `docs/design/legible-output.md` — this spec is the implementation contract only.


## Summary


Add a third presentation rung to the atomic output style (prose → structure → rendered page) and a new skill `atomic-legible` that re-renders a previous answer as a self-contained HTML page in the scratchpad. The TUI reply always stays atomic; the page is an additional surface.


## Checkpoints


| # | Checkpoint | Files/areas | Mode | Verifies |
|---|-----------|-------------|------|----------|
| 1 | Skill + output-style ladder | `skills/atomic-legible/SKILL.md` (new), `output-styles/atomic.md` | feature | Success criteria 1, 2; criterion 4 (bundle regen) |
| 2 | Discovery surfaces | `README.md`, `docs/reference/skills.md`, `docs/reference/output-style.md`, `templates/commands/atomic-help.md` (+ `make render` output `commands/atomic-help.md`) | feature | Success criteria 3, 6; criterion 4 (render + bundle regen) |


Both checkpoints are markdown-artifact work: no runtime code, no tests to write. TDD is skipped with an explicit "skipped because: pure markdown artifact change" note. Regen contract per checkpoint — each regen lands in the same commit as its source change (the pre-commit hook enforces this; run the commands explicitly anyway):

- Checkpoint 1 touches no `templates/` file → run only `make -C atomic bundle` (skills and output-styles are bundle sources).
- Checkpoint 2 edits `templates/commands/atomic-help.md` → run `make render` from the repo root first, then `make -C atomic bundle`.


## Checkpoint 1 — skill + ladder


### 1a. New skill `skills/atomic-legible/SKILL.md`


Frontmatter: `name: atomic-legible` and a `description:` that carries (all four, so the trigger surface is inspectable):

- What it does: re-renders a previous answer (or the current one) as a single self-contained HTML page for legible reading; the TUI reply stays atomic.
- Auto-fire triggers: "show me that legibly", "render that", "make that readable", "show that as a page", "pretty version of that", "re-render that answer".
- Invoked-by: the atomic output style's presentation ladder (rung 3) when a reply crosses the density threshold.
- Boundary: re-presentation of existing content only — never re-derives analysis. Distinct from `atomic-visual-options` (planning-phase option comparison with typed pick codes); this skill presents one finished answer, no decision capture.


Body sections (follow the structure and voice of `skills/atomic-visual-options/SKILL.md`; normal prose, XML tags for trigger/workflow/constraints):

1. `<trigger>` — the auto-fire phrases above, plus: fires when the output style's rung-3 offer is accepted, and directly (no offer) when the request itself is presentation-shaped ("give me a report on…", "write up a comparison of…") or the user already accepted a render this session.
2. Workflow:
   - Identify the target content: default is the assistant's previous substantive answer; an explicit reference ("the comparison from earlier") overrides.
   - Write ONE self-contained HTML file to `.claude/.scratchpad/<YYYY-MM-DD>-<slug>/legible.html` (slug from the content's topic).
   - Content rule: re-presentation, not re-derivation. Every fact, number, and code block identical to what was said; compressed fragments may expand to full sentences; ASCII trees/tables become real markup. Nothing new.
   - Print the `file://` path. Do not auto-open the browser. Keep the TUI reply atomic (summary + path).
   - Iteration: user asks for changes → overwrite the same file, tell them to refresh.
3. File constraints (same contract as `atomic-visual-options`): starts with `<!DOCTYPE html>`; all CSS inline; no external requests (no CDN, no web fonts, no remote images — inline SVG and base64 `data:` URIs allowed); no client-side JavaScript; light/dark via `@media (prefers-color-scheme: dark)`; code blocks in monospace `<pre><code>` with ligatures disabled (`font-variant-ligatures: none`).
4. A compact HTML scaffold reference (document-shaped: title, section headings, prose, table, `<pre>` block — not the option-card layout of visual-options). Keep it materially shorter than visual-options' scaffold; ~60 lines is enough.
5. `<constraints>` with why-lines: throwaway scratch, never commit; re-presentation only; self-contained/offline; TUI stays the decision surface.


### 1b. `output-styles/atomic.md` — presentation ladder


- Retitle the `# Structure over prose` section to `# Presentation ladder` and open it with the three rungs:
  1. Prose — ≤2 entities, simple answers.
  2. Structure — table for comparison, tree for hierarchy + input/output, ASCII flow for sequencing across actors. Keep the existing guidance, example (cache-warming job), and the trailing "**TUI replies:** ASCII only / **Files in `docs/`:** Mermaid" line.
  3. Rendered page — when a reply crosses the density threshold (3+ headed sections, a table past ~6 columns or ~20 rows, or ~50+ lines of structured content), keep the TUI reply to an atomic summary and offer the full view as a rendered page via the `atomic-legible` skill: end the reply with one offer line. Produce without offering only when the request is presentation-shaped or the user already accepted a render this session. The page is an additional surface — the TUI reply must still stand alone.
- Total addition ≤ 12 lines — the file is system-prompt real estate.
- The skill name `atomic-legible` must appear (cross-wiring: command/style names the skill, skill declares its invoker).


## Checkpoint 2 — discovery surfaces


| Surface | Edit |
|---------|------|
| `README.md` | No per-skill table exists; skills are one summary row in the Capability table (`**Discipline skills**`). Update that row: count `Nine` → `Ten` and append the new skill to its comma-list (e.g. `…, visual-options, legible re-render`). |
| `docs/reference/skills.md` | Add an `atomic-legible` row to the **Workflow** sub-table, directly after `atomic-visual-options`; columns follow the table's `Skill / Fires when you say... / What it does` format, consistent with the skill frontmatter. |
| `docs/reference/output-style.md` | Describe the presentation ladder: three rungs, offer-first rendered page, pointer to `atomic-legible`. Match the file's existing atomic-prose voice. |
| `templates/commands/atomic-help.md` | `skills` topic row: update count 9→10 and append `atomic-legible` to the list. Tour Stage 1 skills line: update count and append a legible-render mention. Grep the template for `9 auto-firing` and `visual options` to catch every count/list instance. |


After the template edit run `make render` from the repo root; the rendered `commands/atomic-help.md` lands in the same commit (pre-commit hook enforces; do not rely on it — run render explicitly).


## Success criteria


1. `skills/atomic-legible/SKILL.md` exists with frontmatter + all body sections above.
2. `output-styles/atomic.md` contains the three-rung ladder and the string `atomic-legible`; addition ≤ 12 lines net.
3. All four Checkpoint-2 surfaces updated; no remaining `9 auto-firing` string in `templates/commands/atomic-help.md`.
4. `make render` then `git diff --exit-code commands/` is clean; `make -C atomic bundle` then `git diff --exit-code atomic/internal/embedded/` is clean (i.e. regens are committed).
5. `atomic validate` reports no new findings on the touched artifacts.
6. The `/atomic-help` MISSING-scan (loop over `commands/*.md` grepping the help template) reports zero `MISSING:` lines.
7. `go -C atomic test ./...` — no new failures vs the branch-point baseline (known pre-existing: `internal/hooks` wiki-block tests fail on machines with dirty registered wikis).
8. Out of scope guarded: no changes to `CLAUDE.md`, agents, commands other than `atomic-help`, or the two-voices boundary.


## Change log


- 2026-07-03 — initial spec (autopilot, issue #113).
- 2026-07-03 — correct: Checkpoints table reshaped to the S5 validator column contract (`Files/areas`, trailing `Verifies`); no behavioral change.
- 2026-07-03 — implemented: CP1 `010f0bc` (skill + ladder, +4 net lines to the style), CP2 `d45ff92` (discovery surfaces). Both reviewer passes clean (0 findings). Verified: full `go test` green, render + bundle parity clean, `atomic validate` artifacts/bundle 0 FAIL (worktree-built binary), MISSING-scan zero. Success criteria 1–8 met.

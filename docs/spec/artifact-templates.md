# Artifact templates

## Goal

Eliminate duplication across `commands/*.md` files by introducing a `text/template`-based render step. Shared blocks (commit flow, signals gate, doc-impact check, etc.) live once in `templates/shared/`; rendered plain-markdown output continues to live in `commands/` for the bundle and Claude Code to consume unchanged. `commands/` is fully generated from CP1 onward — `templates/` is the sole edit path. `make render` + pre-commit hook is the drift gate; CI is a backstop.


## Non-goals

- Runtime templating inside the installed bundle or user's `~/.claude/`.
- Per-user variable injection (config values, hostname).
- Templating for `output-styles/` or `rules/` in v1.
- Replacing or restructuring the bundle-mirror pipeline.
- Exposing `render-templates` as an `atomic` binary subcommand.
- Variant-passing machinery (`dict` func, template parameters, `{{ if }}` conditionals inside partials). Partials are pure fragments; optional sub-fragments are their own micro-partials.
- Mixed `commands/` (some hand-authored, some generated). Every command file in `commands/` is generated from CP1 onward.
- Automated lint that asserts partial-include directives are present in source templates. Manual PR-review grep in v1; automation deferred to follow-up.


## Success criteria

| # | Criterion |
|---|-----------|
| 1 | `make render` from repo root completes without error against an empty `templates/` + empty `commands/` (true no-op state). |
| 2 | Adding `templates/commands/foo.md` causes `make render` to write `commands/foo.md` matching the template output byte-for-byte. |
| 3 | `templates/shared/<name>.md` is callable as `{{ template "<name>" . }}` from any source template. |
| 4 | Orphan rule: `commands/<name>.md` without a matching `templates/commands/<name>.md` causes `make render` to halt with a non-zero exit and an error message that names both remediation paths (create the template OR `rm` the orphan output). Renderer never auto-deletes. |
| 5 | `.githooks/pre-commit` auto-runs `make render` and re-stages `commands/` outputs whenever any `templates/` file is staged. |
| 6 | CI backstop: `make render && git diff --exit-code` runs as a step in `.github/workflows/ci.yml` and fails the workflow on stale render. |
| 7 | CP1 bootstrap: every existing `commands/<name>.md` has a byte-equal `templates/commands/<name>.md` counterpart; `make render` produces zero diff against the pre-CP1 `commands/` tree. |
| 8 | After CP2: `templates/commands/commit-only.md` contains the `{{ template "commit-flow" . }}` directive and does NOT contain commit-flow's body as literal text. Rendered `commands/commit-only.md` is byte-equal to its pre-CP1 state (or any diff documented in commit). |
| 9 | After CP3: `templates/commands/{pr-only,merge-to-main,squash-only,push-only}.md` each contain `{{ template "<flow>" . }}` for their respective flow partial and do NOT contain that flow's body as literal text. |
| 10 | After CP4: `templates/commands/{commit-and-pr,commit-and-push,commit-and-merge,commit-and-squash,squash-and-merge}.md` each contain `{{ template }}` directives for their full partial set and do NOT contain any flow body as literal text. Each pipeline-verb template is touched exactly once across CP3+CP4 (no double-edit). |
| 11 | After CP5: `doc-impact`, `doc-impact-why`, `signals-gate`, `base-resolution`, `worktree-cleanup-prompt` partials exist in `templates/shared/`; each is consumed (via `{{ template }}`) by ≥ 2 big partials; rendered `commands/*.md` are byte-equal to their CP4 state (or any diff documented). |
| 12 | After CP6: `.claude/skills/atomic-cli-contrib/SKILL.md` documents "every `commands/` file is generated; edit `templates/` only" rule. Manual dogfood test passes: edit a partial → `make render` → symlinked `.claude/commands/<file>.md` reflects change. |


## Approaches

| # | Approach | Pros | Cons |
|---|----------|------|------|
| A | New `atomic/cmd/render-templates/` tool, `text/template` engine, `templates/{shared,commands}/` source, `commands/*.md` rendered output | Clean separation; mirrors bundle-mirror pattern; no new dep; isolated testability | Two-stage build; contributors learn two source dirs |
| B | Extend `cmd/bundle-mirror` to render-then-mirror | One tool, one stage | Mixes responsibilities; harder to test render in isolation |
| C | In-place HTML-comment markers (`<!-- @include shared/commit-flow -->`) | No output dir; wiring unchanged | Marker comments pollute source; `git diff` shows N-file churn on single-partial edit |
| D | Third-party DSL (Liquid, Mustache, etc.) | Familiar to web devs | New dep; weaker stdlib integration |


## Recommendation

**Approach A.** New `render-templates` tool, `text/template` stdlib engine, `templates/` source dir, existing `commands/` as fully-generated output.

Mirrors the bundle-mirror pattern: source dir → CLI tool → tracked output → CI gate (`git diff --exit-code`). Renderer is testable in isolation via golden-file tests. `text/template` is already used in `cmd/bundle-mirror`; no new dep. CP1 bootstrap (every existing command becomes a byte-equal template counterpart) collapses the contributor model to one path — always edit `templates/`. Orphan rule (loud error on `commands/<name>.md` without a template) keeps the invariant from leaking. Partials are pure fragments: no `dict` func, no variant flags, no `{{ if }}` conditionals; optional sub-fragments are their own micro-partials.

Approach B rejected: mixes render and embed concerns, blocks future dry-run or preview use. Approach C rejected: in-place marker approach means `git diff` shows changes to every consumer file when a shared partial changes. Approach D rejected: stdlib is sufficient; external dep is unnecessary overhead.


## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|---------|
| 1 | Renderer infrastructure + bootstrap (atomic-builder, ~40 files) | `atomic/cmd/render-templates/main.go`, `atomic/internal/templaterender/` (package + tests), root `Makefile` + `atomic/Makefile` (`render` targets), `.githooks/pre-commit` (new render stage), `.github/workflows/ci.yml` (render gate step), `templates/commands/*.md` × ~32 (byte-equal `cp` copies of every existing command) | SC 1, 2, 3, 4, 5, 6, 7 — renderer + bootstrap proven; orphan rule active; every command has a template; `make render` produces zero diff against pre-CP1 `commands/` |
| 2 | Extract `commit-flow` + migrate `/commit-only` (atomic-builder, ~3 files) | `templates/shared/commit-flow.md`, `templates/commands/commit-only.md` (rewritten to use `{{ template "commit-flow" . }}`), rendered `commands/commit-only.md` | SC 8 — commit-flow body lives once in `templates/shared/`; commit-only template references it via directive |
| 3 | Extract `pr-flow`, `merge-flow`, `squash-flow`, `push-flow` + migrate leaf verbs (atomic-builder, ~8 files) | `templates/shared/{pr,merge,squash,push}-flow.md`, `templates/commands/{pr-only,merge-to-main,squash-only,push-only}.md` (each references its flow partial via directive), rendered `commands/*.md` | SC 9 — each leaf-verb template references its flow partial; no inlined flow bodies |
| 4 | Migrate all pipeline verbs (atomic-builder, ~5 files) | `templates/commands/{commit-and-pr,commit-and-push,commit-and-merge,commit-and-squash,squash-and-merge}.md` (each rewritten ONCE with full partial set), rendered `commands/*.md` | SC 10 — each pipeline-verb template references its full partial set via directives; each touched exactly once across CP3+CP4 |
| 5 | Extract small partials (atomic-builder, ~9 files) | `templates/shared/{doc-impact,doc-impact-why,signals-gate,base-resolution,worktree-cleanup-prompt}.md`, big partials refactored to consume them via directives, rendered `commands/*.md` | SC 11 — small partials extracted; each consumed by ≥ 2 big partials; rendered output byte-equal to CP4 state or diff documented |
| 6 | Contributor skill update + dogfood verification (atomic-surgeon, ~2 files) | `.claude/skills/atomic-cli-contrib/SKILL.md`, end-to-end dogfood run log | SC 12 — "every command is generated; edit `templates/` only" rule documented; symlink loop confirmed |


## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Partial count grows beyond v1 taxonomy (5 big + 5 small) | med | Pure-fragment rule (no variants) prevents nested conditionals; review discipline rejects new partials with duplication count < 2; flat naming + two-level taxonomy keeps the set scannable |
| Contributor pastes a flow body inline instead of using `{{ template "<flow>" . }}` directive | med | SC 8/9/10/11 verify against source templates (grep); manual PR-review catches in v1; automated lint deferred to follow-up |
| Render output drifts when contributor forgets `make render` | low | Pre-commit hook is the contract; CI backstop; `--no-verify` bypass accepted (matches today's bundle behavior) |
| Contributor edits `commands/*.md` directly and loses work | low | Contributor skill documents "every command is generated"; pre-commit re-renders and overwrites on next commit; `git status` surfaces drift |
| Migration introduces unintended behavioral drift (rendered ≠ original for non-dedup reasons) | low | Each CP includes byte-equal verification; CP1 bootstrap is the strictest gate (zero diff against pre-CP1 `commands/`) |
| `text/template` too limiting for a future use case | low | Render logic encapsulated in `templaterender` package; engine is swappable |
| Build pipeline ordering bug (bundle runs before render in some path) | low | `make all` enforces `render` before `bundle`; pre-commit and CI both enforce order explicitly |


## Change log

<!-- Populated on first amendment after the spec is approved. Do not log drafting/refinement turns. -->


## Implementation log


### v1 — 2026-05-23

Built across 7 iterations of `/subagent-implementation` on branch `artifact-templates`. Commits (chronological):

- `e0a94c1` — CP1: render-templates tool + templaterender package + Makefile/hook/CI wiring + 31-file `templates/commands/` bootstrap
- `eb9fe68` — CP2: extract `commit-flow` partial; migrate `/commit-only`
- `669691d` — CP3: extract `pr-flow`, `merge-flow`, `squash-flow`, `push-flow`; migrate the four leaf verbs
- `592e5fc` — CP4: migrate the 5 pipeline verbs (commit-and-pr/push/merge/squash, squash-and-merge) to consume their flow partials; intentional behavior expansion
- `198ea73` — CP5: extract 5 small partials (`doc-impact`, `doc-impact-why`, `signals-gate`, `base-resolution`, `worktree-cleanup-prompt`); restructure flow partials to strip document-level headings (closed F-5, F-6)
- `04a607a` — CP6: update `.claude/skills/atomic-cli-contrib/SKILL.md` with the templates-system contract; dogfood test
- `3c68381` — polish: close F-1 (`errors.New`) and F-3 (`bundle: render` dep)
- `114a832` — defer F-2/F-4/F-7/F-8 to project followups

**Out-of-scope work performed during this build:**

- **CP5 absorbed the F-5/F-6 cleanup** identified by the CP4 reviewer (doubled `## Rules` sections + heading-level inversion in pipeline-verb renders). Originally CP5 was scoped to small-partial extraction only; restructuring the big partials became coupled because removing the small-partial duplication required moving section headings out of the flow partials anyway. Done in one CP rather than two.
- **merge-flow and squash-flow split into named sub-partials** (`*-preflight`, `*-steps`, plus the wrapper). Not specified in the spec — emerged during CP5 as the cleanest way to give the leaf-verb templates seams where their own section headings could land without breaking byte-equality.
- **Polish iteration** (F-1, F-3) ran AFTER the spec's 6 checkpoints, on user disposition at Phase 3. Spec contract met before the polish; polish is hygiene.

**Unforeseens — surprises that emerged during implementation:**

- **Text divergence across flows prevented 3/5 small partials from reaching the SC 11 "≥2 consumers" bar.** `signals-gate` was resolved by adopting the canonical text in `squash-flow` (an intentional, documented byte-equality break in `commands/squash-only.md`). `doc-impact-why` and `worktree-cleanup-prompt` were accepted as 1-consumer deviations and deferred to project followups for a future text-harmonization pass (F-7, F-8).
- **CP4 reviewer asserted a worktree-cleanup regression in `/squash-only`.** Iter 6 investigation (`git show 592e5fc:commands/squash-only.md | grep -c worktree` → 0) proved squash-only never had worktree detection at the base SHA. No regression — the reviewer misread the rendered output. The investigation-first pattern (iter 6 BRIEF) prevented an unnecessary fix that would have introduced new content.
- **`atomic validate bundle` reports stale SHA mismatches under the installed binary** because the binary predates this branch's `commands/` rewrites. Not introduced by this work; resolves itself when the branch lands and CI builds a fresh binary.

**Deferred items still open** (in `.claude/project/followups/`):

- `artifact-templates-f-2` — Renderer success message on stderr (matches `bundle-mirror`; convention question, revisit both together).
- `artifact-templates-f-4` — Orphan error message hardcodes `commands/` prefix (refactor when agents/skills templates land).
- `artifact-templates-f-7` — `worktree-cleanup-prompt` has 1 consumer (accepted SC 11 deviation; revisit if squash-only gains worktree detection).
- `artifact-templates-f-8` — `doc-impact-why` has 1 consumer (step-number divergence; revisit in text-harmonization pass).

**Merged into `main` as `a0d7f93` — 2026-05-23.** Iteration commits above remain reachable in history.

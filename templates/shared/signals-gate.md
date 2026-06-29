{{define "signals-gate"}}<signals-refresh>
Refresh project signals so Claude's map stays current for the next session.

0. **Docs-only guard.** Inspect the staged file set with `git diff --cached --name-only`. If the staged set is **empty** (e.g., in a post-merge or post-squash context where the commit already landed and nothing remains staged), skip the docs-only check and fall through to step 1 — an empty staged set does not mean all paths are documentation. If the staged set is non-empty and **every** staged path is documentation, skip the refresh entirely — do not continue to step 1. A path is documentation when it is under a `docs/` directory at any depth, OR is a top-level `README*` / `CHANGELOG*` / `CONTRIBUTING*` / `CODE_OF_CONDUCT*` / `SECURITY*` / `LICENSE*`. Any other path — source, config, build files, `CLAUDE.md`, or any bundled-artifact `.md` under `agents/`, `commands/`, `skills/`, `rules/`, or `output-styles/` — means the commit is NOT docs-only; continue to step 1. **Why:** the deterministic substrate counts per-language LOC, so a docs-only commit trips `stale` exit 1 and dispatches the inferrer for no real map change. In a config repo the artifact `.md` files are the product, so they must count as source, not docs.
1. Check `command -v atomic`. If missing, skip.
2. Run `atomic signals stale` and act on the exit code. **Why:** the staleness check also prevents a redundant refresh when the implementation phase already refreshed — a fresh stored signals file returns exit 0 and skips dispatch.
   - **exit 0** (fresh) → skip the refresh.
   - **exit 1** (stale) → refresh is mandatory. Continue to step 3. Do NOT second-guess this with `atomic signals diff`, file counts, or a judgment that "the change was small" — exit 1 means a fresh scan would produce different deterministic content than the stored signals file, and the only correct response is to refresh. Skipping it accumulates drift. The command prints how much would change and the directive; follow it.
   - **exit 2** (error, e.g. signals file missing) → report the stderr message and skip; a refresh cannot run against a missing baseline.
3. Dispatch the `atomic-signals-inferrer` agent in silent mode:
   ```
   mode: silent
   first_run: false
   ```
   Stage the router and domain files after the agent completes — do NOT stage `docs/wiki/scan.md` (the raw deterministic dump; thousands of lines, deliberately not auto-staged): `git add docs/wiki/index.md docs/wiki/*.md && git restore --staged docs/wiki/scan.md`.
4. Run `atomic wiki mark-dirty` (best-effort, no-op when cwd is under no registered wiki root). This marks any registered wiki as having uncommitted changes since the last refresh, so the next session nudge fires. Skip silently if `atomic` is not on PATH.

`atomic signals stale` is content-based: it assembles the deterministic snapshot exactly as a scan would and compares it to `docs/wiki/scan.md`, returning exit 1 only when they actually differ. A no-op regeneration that merely bumps file mtimes stays fresh; a real shift in the project map goes stale. Treat exit 1 as an unconditional trigger, not a hint.
</signals-refresh>{{- end}}

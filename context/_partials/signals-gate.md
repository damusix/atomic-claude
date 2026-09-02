{{define "signals-gate"}}<signals-refresh>
Refresh project signals so Claude's map stays current for the next session.

0. **Docs-only guard.** Inspect the staged file set with `git diff --cached --name-only`. If the staged set is **empty** (e.g., in a post-merge or post-squash context where the commit already landed and nothing remains staged), skip the docs-only check and fall through to step 1 — an empty staged set does not mean all paths are documentation. If the staged set is non-empty and **every** staged path is documentation, skip the refresh entirely — do not continue to step 1. A path is documentation when it is under a `docs/` directory at any depth, OR is a top-level `README*` / `CHANGELOG*` / `CONTRIBUTING*` / `CODE_OF_CONDUCT*` / `SECURITY*` / `LICENSE*`. Any other path — source, config, build files, `CLAUDE.md`, or any bundled-artifact `.md` under `agents/`, `commands/`, `skills/`, `rules/`, or `output-styles/` — means the commit is NOT docs-only; continue to step 1. **Why:** the deterministic substrate counts per-language LOC, so a docs-only commit trips `stale` exit 1 and dispatches the inferrer for no real map change. In a config repo the artifact `.md` files are the product, so they must count as source, not docs.
1. Check `command -v atomic`. If missing, skip.
2. Run `atomic signals stale` and act on the exit code. **Why:** the staleness check also prevents a redundant refresh when the implementation phase already refreshed — a fresh stored signals file returns exit 0 and skips dispatch.
   - **exit 0** (fresh) → skip the refresh.
   - **exit 1** (stale) → refresh is mandatory. Continue to step 3. Do NOT second-guess this with `atomic signals diff`, file counts, or a judgment that "the change was small" — exit 1 means a fresh scan would produce different deterministic content than the stored signals file, and the only correct response is to refresh. Skipping it accumulates drift. The command prints how much would change and the directive; follow it.
   - **exit 2** (error, e.g. signals file missing) → report the stderr message and skip; a refresh cannot run against a missing baseline.
3. Dispatch the `atomic-wiki-inferrer` agent in silent mode:
   ```
   mode: silent
   first_run: false
   ```
   Stage the router, domain files, and `docs/wiki/scan.md` after the agent completes: `git add docs/wiki/*.md`. `scan.md` is committed deliberately — it is the drift-scope diff baseline (`git diff HEAD -- docs/wiki/scan.md`) that `atomic signals stale` and the `<scan-sha>` tiebreaker depend on, and it is not `@-ref`'d, so committing it costs nothing in context. Also stage the per-domain pointer cards the agent wrote or deleted, guarded on the directory existing (repos whose wiki has no domain files never create it; an unguarded `git add -A` on a missing path exits 128): `[ ! -d .claude/rules/wiki ] || git add -A .claude/rules/wiki/`. Then check for an ignore-file edit mechanically, since the agent has no reported-path output to key off in silent mode: `git status --short -- .gitignore .claude/.gitignore`, and `git add` whichever path that reports modified.
4. Run `atomic wiki mark-dirty` (best-effort, no-op when cwd is under no registered wiki root). This marks any registered wiki as having uncommitted changes since the last refresh, so the next session nudge fires. Skip silently if `atomic` is not on PATH.

`atomic signals stale` is content-based: it assembles the deterministic snapshot exactly as a scan would and compares it to `docs/wiki/scan.md`, returning exit 1 only when they actually differ. A no-op regeneration that merely bumps file mtimes stays fresh; a real shift in the project map goes stale. Treat exit 1 as an unconditional trigger, not a hint.
</signals-refresh>{{- end}}

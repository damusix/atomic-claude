# Spec: `atomic` binary


The `atomic` CLI is a single Go binary that backs the cron and signals workflows in this configuration. It owns file management under `.claude/`, parses frontmatter, runs deterministic project scans, and exposes a hook-output mode for shell integration. Slash commands prefer the binary when present and fall back to raw file operations when absent.


## Purpose


- Replace fragile shell-based file ops with a typed, tested CLI.
- Provide one install for end users (downloadable binary; no Go toolchain required).
- Keep markdown the source of truth — the binary reads and writes plain frontmatter-prefixed `.md` files.


## Non-goals


- Not a daemon. Every invocation is one-shot.
- Not a scheduler. Scheduling is delegated to Claude's built-in cron (`CronCreate` / `CronList` / `CronDelete`).
- Not a replacement for slash commands. The binary is the *engine*; commands are the *interface*.


## Repository layout


```
atomic-claude/                       # repo root (this repo)
├── atomic/                          # Go module root
│   ├── go.mod                       # module: github.com/damusix/atomic-claude/atomic
│   ├── cmd/atomic/main.go           # CLI entry — wires subcommands
│   ├── internal/
│   │   ├── repoctx/                 # resolves repo root, .claude/ paths
│   │   ├── frontmatter/             # parse + emit YAML frontmatter
│   │   ├── ids/                     # slug + short-id generation
│   │   ├── signals/                 # scanners (tree, manifests, languages)
│   │   ├── reminder/                # reminder storage: add/list/show/rm
│   │   ├── hooks/                   # hook-output rendering
│   │   ├── claudeinstall/           # write embedded artifacts to ~/.claude/, backups, diff
│   │   ├── embedded/                # go:embed of CLAUDE.md, agents/, commands/, skills/, output-styles/
│   │   └── selfupdate/              # GitHub release lookup, async check, replace-in-place
│   ├── pkg/                         # exported helpers if any are needed externally
│   └── testdata/                    # fixtures for scanner + parser tests
├── .github/workflows/release.yml    # goreleaser pipeline
├── install.sh                       # one-line installer for end users
└── ...
```


Module path: `github.com/damusix/atomic-claude/atomic`. The `atomic/` subdirectory keeps Go code out of the markdown-heavy repo root.


## CLI surface


All commands accept global flags:


- `--repo <path>` — repo root override (default: detect from `cwd` via `git rev-parse --show-toplevel`).
- `--json` — emit machine-readable JSON instead of human prose.
- `--quiet` / `-q` — suppress non-error output.
- `--version` — print version + git sha.
- `--no-update-check` — suppress the background self-update check for this invocation.


On every command invocation (except `--no-update-check` and `atomic update` itself), the binary fires an asynchronous GitHub Releases lookup. If a newer version is available and the local cache says we haven't notified the user in the last 24h, a one-line banner is appended to stderr after the command's primary output: `update available: vX.Y.Z (current: vA.B.C). run: atomic update`. The cache lives at `~/.cache/atomic/update.json`. The lookup never blocks the foreground command — if it has not completed by the time the foreground work finishes, the banner is skipped for this invocation.


### Exit code convention


Two families of commands, two exit-code meanings. Conflating them is the failure mode this convention exists to prevent — a caller that reads exit 1 as "error" when it means "actionable signal" either acts on nothing or skips work it should do.


**Check / status commands** (`signals stale`, `signals diff`, `docs stale`, `update --check`) follow the `diff(1)` / `cmp(1)` idiom:

- **exit 0** — nothing actionable: fresh, up to date, no diff. Silent or a brief confirmation on stdout.
- **exit 1** — an actionable positive signal: stale, diff present, update available. The result goes to **stdout**.
- **exit 2** — a hard error (missing baseline, parse failure, network failure). The explanation goes to **stderr**.

Exit 1 is never an error in this family — it is the answer. Reserve 2 for "the check could not run."


**Gate / action commands** (`doctor`, `validate`, install/update apply) use plain success/failure: **0** = success (or all PASS/WARN/SKIP), **1** = failure (any FAIL), **2** = usage error. Here exit 1 *is* a problem.


### `atomic signals`


| Verb | Description |
|------|-------------|
| `scan` | Walk the repo and write `.claude/project/deterministic-signals.md`. Idempotent — same input → same output. Before overwriting, copies the existing file (if any) to `.claude/project/.deterministic-signals.prev.md` so `atomic signals diff` has an old version to compare against in non-git contexts. The `.prev.md` file should be in `.gitignore`. |
| `show` | Print the most recent `deterministic-signals.md` to stdout. |
| `stale` | Content-based freshness check. Assembles the deterministic body exactly as `scan` would and compares it to the stored `deterministic-signals.md` body. Exit 0 if a re-scan would produce identical content (fresh), exit 1 if it would differ (stale), exit 2 on a hard error (e.g. signals file missing — stderr explains). Used as a gate by the signals skill. Because it compares content, not mtimes, an idempotent regeneration that only bumps a file's mtime (e.g. commit-time `make bundle` rewriting `manifest.go` with identical bytes) stays fresh — no false-positive treadmill. Cost is a full tree assembly, not a stat. On exit 1 it prints imperative, evidence-bearing output to stdout — the approximate number of deterministic-body lines that would change and the directive to dispatch the inferrer — because the gate is consumed by an LLM orchestrator that can otherwise rationalize a silent exit code away. Exit 0 is silent. |
| `diff` | Print the unified diff between the previous and current `deterministic-signals.md` to stdout. Thin wrapper: shells out to `git diff -- .claude/project/deterministic-signals.md` when inside a git repo, falls back to unix `diff -u <prev> <current>` against `.claude/project/.deterministic-signals.prev.md` otherwise. No custom format. Diff content always goes to stdout — exit codes are an additional signal, not a replacement for the diff body. Exit 0 = no diff (stdout empty), 1 = diff present (stdout = unified diff), 2 = no prior version available or a hard error (stdout empty, stderr explains). |


### `atomic reminder`


Storage only. The binary creates, lists, shows, and removes reminder files. Scheduling is owned entirely by Claude via `CronCreate` / `CronDelete`. The binary tracks no status, no due date, no snooze count. A reminder either exists (pending) or it doesn't (done = deleted).


| Verb | Description |
|------|-------------|
| `add <text>` | Create a reminder file in `.claude/.scratchpad/reminders/`. Prints the assigned id. |
| `list` | List all reminders. Output is indexed; each row shows `id`, `created`, first line of body. |
| `show <id>` | Print the body of a single reminder. |
| `rm <id>` | Delete a reminder file. |


### `atomic hooks`


| Verb | Description |
|------|-------------|
| `session-start` | Print a session-start summary block to stdout. Lists all pending reminders (capped at 10). Format: see [Hook output](#hook-output). |
| `install [--scope user\|project]` | Register the inline command `atomic hooks session-start` in `<scope>/.claude/settings.json` under the `hooks.SessionStart` event so Claude Code fires it. Default scope is `user` (`~/.claude/`). Idempotent — running twice does not duplicate the registration. Migrates a pre-inline install: removes any legacy `session-start-reminders.sh` wrapper-script registration and deletes the stale script file. |
| `uninstall [--scope user\|project]` | Remove the settings.json registration (inline command and any legacy wrapper-script entry) and delete a lingering wrapper script. |


Hook registration matters because Claude Code only fires hooks listed in `settings.json`. The `install` verb edits the JSON minimally (adds one entry under `hooks.SessionStart`, whose `command` is `atomic hooks session-start`) and preserves any other user-managed hooks already present. Claude Code runs hook commands through a shell, so the multi-word command resolves `atomic` on `PATH` and execs it directly — no wrapper script is needed. On any settings.json parse error, it refuses to write and prints instructions for manual registration.


### `atomic claude`


Installs / updates the atomic-claude artifact bundle (CLAUDE.md, agents, commands, skills, output-styles) into the user's `~/.claude/` directory. Artifact content is embedded in the binary at build time via Go's `embed` package, so a single binary install delivers a versioned, self-contained artifact set with no network or repo clone required.


| Verb | Description |
|------|-------------|
| `install [--dry-run] [--target ~/.claude]` | First-time install. Writes embedded artifacts to `~/.claude/`. Refuses to touch any non-atomic-prefixed file. For an existing `~/.claude/CLAUDE.md`, applies block-aware handling: replaces a stale `<atomic>` block in place, or — when the file has no parseable block — writes the proposed version to `~/.claude/.atomic/proposed/CLAUDE.md` for `/atomic-claude-merge` (see [CLAUDE.md handling](#claudemd-handling)). |
| `update [--dry-run] [--target ~/.claude]` | Refresh an existing install. Diff every embedded artifact against its on-disk counterpart, back up changed files to `~/.claude/.atomic/backups/<ISO-timestamp>/`, then overwrite. Same `CLAUDE.md` handling as `install`. |
| `list` | Print the artifact manifest embedded in this binary version: one row per artifact (kind, name, sha256). Useful for diffing against `~/.claude` state. |
| `diff` | Show, per artifact, whether the on-disk file matches, differs, or is absent. Read-only. Pairs with `--dry-run` for safety review. |


### `atomic update`


Self-update the binary. Foreground check (not the background lookup other commands fire). Network required.


| Verb / flag | Description |
|-------------|-------------|
| (default) | Check GitHub Releases for the latest tag. If newer than `--version`, download the matching archive + checksum, verify SHA256, replace the running binary in place atomically (download to temp, then `rename`). On success: the artifact bundle is refreshed automatically by re-execing the new binary as `claude update --no-update-check` (with `--no-hooks` appended when no session-start hook is registered, preserving the user's hook choice), then the post-update doctor runs (see `docs/spec/atomic-update-doctor.md`). |
| `--check` | Only check, don't apply. Exit 0 if up-to-date, exit 1 if a newer version is available (prints `update available: ...` to stdout), exit 2 on a hard error such as a network or parse failure (stderr explains). Follows the check-family exit convention — exit 1 is the "update available" signal, not an error. |
| `--channel <stable\|prerelease>` | Default `stable` (only non-prerelease tags). `prerelease` includes RC/beta tags. |
| `--no-doctor` | Skip the post-update doctor self-check. |
| `--skip-claude-update` | Skip the post-swap `~/.claude` artifact refresh; binary swap only. |


### `atomic doctor`


Integrity check for the atomic-claude install and current project state. Runs eight deterministic checks and reports PASS / WARN / FAIL / SKIP per category. Non-zero exit on FAIL for CI gating. Opt-in repair via `--fix`. Full contract: `docs/spec/atomic-doctor.md`.


```
atomic doctor [--fix] [--json] [--only <cat[,cat...]>] [--skip <cat[,cat...]>] [--stale-days N] [--verbose]
```


| Flag | Effect |
|------|--------|
| `--fix` | Per-item confirm prompt before applying any repair. Implies interactive. |
| `--json` | Emit machine-readable result to stdout (schema_version 1). Suppresses human output. `--fix` + `--json` is a usage error (exit 2). |
| `--only` | Comma-separated category indices (`1,3`) or names (`install,signals`). |
| `--skip` | Same syntax as `--only`. Skip listed categories. |
| `--stale-days` | Override stale-signals threshold (positive int, default 7). |
| `--verbose` | Print per-file detail for `install` and `manifest` checks. |


Check categories (indices stable; never renumber):


| # | Name | Fail severity |
|---|------|---------------|
| 1 | `install` | WARN drift / FAIL missing |
| 2 | `hooks` | WARN |
| 3 | `signals` | WARN |
| 4 | `refs` | FAIL |
| 5 | `manifest` | FAIL (repo-dev only; SKIP elsewhere) |
| 6 | `followups` | WARN |
| 7 | `memory` | WARN |
| 8 | `binary` | WARN |


Exit codes: 0 = all PASS/WARN/SKIP (also: `~/.claude/` absent — short-circuit); 1 = any FAIL; 2 = usage error.


### Invocation responsibility


`atomic claude install` and `atomic claude update` are *always run by the user explicitly*. The binary never auto-runs them. Drift between the binary's embedded bundle and the on-disk `~/.claude/` is the user's call to resolve, on their schedule.


| Scenario | What user does |
|----------|----------------|
| First-time setup | After `install.sh` finishes, user runs `atomic claude install` themselves. |
| Refresh after `atomic update` | If user wants the artifact bundle synced to the new binary, they run `atomic claude update` themselves. |
| Second machine | User runs `atomic claude install` directly. |
| Project-scoped install | User runs `atomic claude install --target ./.claude`. |
| Forced re-sync | `atomic claude update`. |


`install.sh` prints next-step instructions but does not invoke `atomic claude install`. `atomic update` prints a hint that the bundle may be out of sync but does not invoke `atomic claude update`. No auto-chains anywhere.


## File conventions


All `atomic`-managed files use YAML frontmatter and are plain markdown.


### Reminder file


Path: `.claude/.scratchpad/reminders/<YYYY-MM-DD>-<slug>.md`


```markdown
---
id: r-7b21
created: 2026-05-16
---

Body text. Free-form markdown.
```


Only two frontmatter fields. Scheduling state lives in Claude's cron system (`CronList` returns it). Done state is "file no longer exists". Snooze is "Claude scheduled a new cron, file unchanged".


### Deterministic signals output


Path: `.claude/project/deterministic-signals.md`


```markdown
---
generated_at: 2026-05-16T18:32:11Z
atomic_version: 0.1.0
---

# Deterministic signals

## Tree

[tree output, depth-limited]

## Manifests

- package.json: name=foo, version=1.2.3, scripts=[build, test, lint]
- go.mod: module=github.com/...
- ...

## Languages

- TypeScript: 4231 LOC (62%)
- Go: 1820 LOC (27%)
- ...
```


No prose. No inference. Pure structured facts.


### Inferrer reads `atomic signals diff`


The signals file is committed (or staged) per the signals workflow. The `atomic signals diff` subcommand wraps `git diff` (or unix `diff` when no git repo) so the inferrer has one well-known command to call regardless of repo state. No custom format; no JSON contract. Output is a standard unified diff. See [`signals-workflow.md`](./signals-workflow.md) for the consumer flow.


## Claude artifact bundling


### Embedding


At build time, the contents of `agents/`, `commands/`, `skills/`, `output-styles/`, and the root `CLAUDE.md` (renamed to `CLAUDE.md` on install) are pulled into the binary via Go's `embed` package. The bundle is keyed by atomic-claude release tag; `atomic --version` reports both the binary version and the bundle commit sha so users can verify what was installed.


Inclusion rules (per directory, explicit allowlist via bundle manifest at build time):


- `agents/` — only `atomic-*.md` (e.g. `atomic-builder.md`, `atomic-claude-merger.md`).
- `skills/` — only `atomic-*/SKILL.md` directories (e.g. `atomic-tdd/`, `atomic-documentation/`).
- `output-styles/` — only `atomic*.md` (e.g. `atomic.md`).
- `commands/` — explicit allowlist by name, NOT by prefix. Includes both atomic-prefixed (`atomic-setup.md`, `atomic-plan.md`, `atomic-claude-merge.md`) and verb-named (`commit-only.md`, `merge-to-main.md`, `git-cleanup.md`, `worktree-start.md`, etc.). The full list is committed in `atomic/internal/embedded/manifest.go` and updated when commands are added or removed.
- `.claude/rules/**/*.md` — path-scoped topic rules grouped by language or topic (e.g. `typescript/`, `python/`). Each rule file declares `paths:` globs in its frontmatter so Claude only loads it when touching matching filetypes. Whole directory is included as-is; bundle manifest enumerates each file.
- `CLAUDE.md` at the atomic-claude repo root → installed as `~/.claude/CLAUDE.md`.
- Excluded: `claude.local.md`, `tmp/**`, `docs/**`, `.claude/.scratchpad/**`, `.claude/docs/**` (project-local design docs), `atomic/**` (the Go module itself), `.worktrees/**`.


### Install / update semantics


Targets (relative to `--target`, default `~/.claude`):


| Source (in bundle) | Target |
|--------------------|--------|
| `CLAUDE.md` | `CLAUDE.md` |
| `agents/atomic-*.md` | `agents/atomic-*.md` |
| `commands/*.md` | `commands/*.md` |
| `skills/atomic-*/SKILL.md` | `skills/atomic-*/SKILL.md` |
| `output-styles/atomic.md` | `output-styles/atomic.md` |
| `.claude/rules/<lang>/*.md` | `rules/<lang>/*.md` |


The `.claude/` source prefix is stripped during install — rule files live at `~/.claude/rules/...` on the target side, matching Claude Code's expected layout.


Per-file flow:


1. Compute sha256 of the embedded source.
2. Read the on-disk target (if any), compute its sha256.
3. If shas match → skip, report as `unchanged`.
4. If on-disk file is missing → write source, report as `installed`.
5. If on-disk file is bundle-managed (its target path appears in the bundle manifest) and differs → back up to `~/.claude/.atomic/backups/<ISO-timestamp>/<relative-path>`, then overwrite, report as `updated (backup at <path>)`.
6. If on-disk file is `CLAUDE.md` and differs → block-aware comparison. When both the embedded source and the on-disk file carry exactly one parseable `<atomic>...</atomic>` block: equal blocks → report as `unchanged` (user content outside the block is not drift); different blocks → back up the whole file to `~/.claude/.atomic/backups/<ISO-timestamp>/CLAUDE.md`, replace only the block in place, report as `block replaced`. When the on-disk file has no parseable block (pre-tag install, unclosed or duplicate tags) → write source to `~/.claude/.atomic/proposed/CLAUDE.md`, report as `merge required (proposed at <path>)`.
7. If on-disk file does not appear in the bundle manifest and is not `CLAUDE.md` → refuse to touch, report as `skipped (not owned by atomic)`. Defensive guard against accidental writes outside the bundle.


"Bundle-managed" is defined by the embed manifest, not by filename prefix. Most are `atomic-*`-prefixed; rule files (`rules/<lang>/*.md`) are not, but are still bundle-managed and therefore atomic-owned for backup/overwrite purposes.


`--dry-run` skips step 4–6 writes and prints what *would* happen.


### CLAUDE.md handling


`CLAUDE.md` mixes two ownership zones: the `<atomic>...</atomic>` block is atomic-owned (a versioned contract), everything outside it is user-owned. The binary draws that boundary with a line-anchored parser (a line whose trimmed content is exactly `<atomic>` opens the block, `</atomic>` closes it; inline mentions never match) and handles each zone deterministically:


**Block path (on-disk file has exactly one parseable `<atomic>` block).** Equal blocks → no action; user content outside the block never registers as drift, in install/update, `diff`, or doctor check 1. Stale block → the binary backs up the whole file, splices the embedded block over the on-disk block byte-for-byte, and preserves everything outside it. User edits *inside* the block are overwritten (recoverable from the backup) — atomic-owned content is a versioned contract, and silently preserving divergent edits inside it would leave the user running a patched version they can't diff against upstream. No proposed file, no merge step.


**Merge path (no parseable block — pre-tag install, unclosed or duplicate tags).** Code cannot draw the ownership boundary safely, so it defers to the LLM merge — it never spawns Claude, never edits CLAUDE.md itself:


1. Binary writes the embedded `CLAUDE.md` content to `~/.claude/.atomic/proposed/CLAUDE.md`.
2. Binary prints:
    ```
    CLAUDE.md needs review.
      old: ~/.claude/CLAUDE.md
      new: ~/.claude/.atomic/proposed/CLAUDE.md

    Run /atomic-claude-merge inside any Claude Code session when you're ready to merge.
    Or inspect manually:  diff ~/.claude/CLAUDE.md ~/.claude/.atomic/proposed/CLAUDE.md
    ```
3. User runs `/atomic-claude-merge` themselves, on their own schedule. The slash command (installed by `atomic claude install` into `~/.claude/commands/`) dispatches the `atomic-claude-merger` agent, which reads both files, produces a merged version, presents a diff, asks for confirmation, then writes the result and removes the proposed file. See [`install-workflow.md`](./install-workflow.md) for the slash-command spec.


First-time install (no existing `CLAUDE.md`):


- Steps 1–3 are skipped. The embedded content is written directly to `~/.claude/CLAUDE.md`. No proposed file, no merge step needed.


### Why no auto-invoke


The binary never spawns Claude. Three reasons:


- The user dictates *when* a global config change applies. Binary-spawning-editor flows are surprising and cross tool boundaries.
- The merge step can be deferred — user might want to inspect the proposed file first, or schedule it for a quiet moment.
- Destructive-ops axiom: the merge slash command has its own Accept/Show/Edit/Abort gate. The right place for explicit confirmation is at the merge, not at launch.


### Backups


Path: `~/.claude/.atomic/backups/<ISO-timestamp>/<relative-path>`


- Created on first `update` that needs to overwrite any atomic-prefixed file.
- Timestamp is the install run's start time, not per-file, so all changes from one run live together.
- Backups are never auto-deleted by the binary. The user can rotate them manually or via a future `atomic claude prune-backups --older-than <duration>` verb (out of scope for v0.1.0).


### Final report


```
Atomic Claude install summary

Installed (4):
  ✓ agents/atomic-builder.md
  ✓ skills/atomic-tdd/SKILL.md
  ✓ output-styles/atomic.md
  ✓ rules/typescript/no-as-cast.md

Updated (2, backed up to ~/.claude/.atomic/backups/2026-05-16T18-32-11Z/):
  ↻ agents/atomic-reviewer.md
  ↻ commands/commit-only.md

Unchanged (5):
  • commands/merge-to-main.md
  • commands/git-cleanup.md
  • commands/atomic-setup.md
  • agents/atomic-investigator.md
  • rules/python/test-layout.md

Needs review (1):
  ⚠ ~/.claude/CLAUDE.md
    proposed at ~/.claude/.atomic/proposed/CLAUDE.md
    next step: run /atomic-claude-merge inside any Claude Code session
```


## Self-update


### Lookup


Source: GitHub Releases API for `damusix/atomic-claude`. Authenticated only if `GITHUB_TOKEN` is set (avoids unauthenticated rate limits on heavy users). One HTTP call per lookup.


### Foreground vs background


- **Foreground** (`atomic update`): block on the lookup, perform the download + verify + replace synchronously.
- **Background** (every other invocation, unless `--no-update-check` or `atomic update`): goroutine fires the lookup, writes the result to `~/.cache/atomic/update.json` (`{checked_at, latest_version, current_version}`), and the main thread checks that file on exit to decide whether to print the banner. If the goroutine has not finished by exit-time, the banner is suppressed for this run.


### Cache schema


`~/.cache/atomic/update.json`:


```json
{
  "checked_at": "2026-05-16T18:32:11Z",
  "current_version": "0.1.0",
  "latest_version": "0.1.1",
  "notified_at": "2026-05-15T09:00:00Z"
}
```


Banner is printed at most once per 24h (`now - notified_at > 24h`). Each print updates `notified_at`. Suppresses banner spam.


### Replace flow (foreground)


1. Resolve latest release.
2. Pick the asset matching `<os>_<arch>`.
3. Download archive + `checksums.txt` to `${TMPDIR}/atomic-update-<sha>/`.
4. Verify SHA256.
5. Extract.
6. `os.Rename(newBinary, currentBinary)` — atomic on POSIX. On error (cross-device or permission), print the error and a `sudo install <new> <current>` hint.
7. Print: `updated atomic vX.Y.Z → vA.B.C.`


### Rollback


No automated rollback in v0.1.0. If a release breaks, the user reinstalls the prior version via the `install.sh` script with `ATOMIC_VERSION=v0.1.0 curl ... | bash`. Document this in the README.


## Hook output


### Hook contract


Claude Code hook events read stdout. On exit 0, stdout is parsed as JSON if it is valid JSON; otherwise it is treated as plain text and appended to context. The JSON form gives finer control — `additionalContext` injects discretely (not echoed in the transcript), `systemMessage` surfaces a warning to the user, `terminalSequence` can ring a bell. The plain-text form is fine for crude integrations and is what the fallback shell script uses.


Reference: [Claude Code hooks docs](https://code.claude.com/docs/en/hooks).


### `atomic hooks session-start` output


Default form is JSON. Emit to stdout:


```json
{
  "hookSpecificOutput": {
    "hookEventName": "SessionStart",
    "additionalContext": "## Pending reminders (3)\n- [r-7b21] benchmark the new query plan (created 2 days ago)\n- [r-3f9a] fix the auth race in middleware (created 5 days ago)\n- [r-1c7e] revisit error handling in ingest (created 1 week ago)"
  },
  "suppressOutput": true
}
```


- `additionalContext` carries the markdown-formatted reminder list. Claude sees it as session context, the user does not see it dumped in the transcript.
- `suppressOutput: true` keeps the hook's stdout out of the debug log too.
- When there is something *urgent* (a reminder file older than some threshold the binary picks, say 14 days, surfaced as "overdue"), the binary may additionally include `systemMessage` to warn the user: `"systemMessage": "3 reminders pending, oldest is 14 days old"`. This is optional and gated on the urgency heuristic — silent is the default.
- When no reminders are pending, emit nothing (exit 0 with empty stdout). Claude treats this as a no-op.


Flag: `atomic hooks session-start --format=text` falls back to plain markdown text on stdout (no JSON envelope). Used by any shell-fallback consumer that, for whatever reason, can't trust JSON parsing on its side. Default is JSON.


## Git workflow


- **Branching**: trunk-based on `main`. Feature work lands via `/worktree-start` into `.worktrees/<branch>`.
- **Versioning**: semver tags `vMAJOR.MINOR.PATCH`. Pre-1.0 is `v0.x.y`; breaking changes bump MINOR until v1.0.
- **Commit style**: Conventional Commits via the `atomic-commit` skill. No AI bylines.
- **Spec linkage**: every Go change references either this spec or a downstream workflow spec. Commits that touch `atomic/` without a spec link get flagged in review.


## Release cycle


### Tooling


- **goreleaser** drives cross-platform builds and GitHub Release publication. Config lives at `.goreleaser.yaml`.
- **GitHub Actions** workflow `.github/workflows/release.yml` runs on tag push (`v*`).


### Build matrix


| OS | Arch |
|----|------|
| linux | amd64, arm64 |
| darwin | amd64, arm64 |
| windows | amd64 |


Each build produces `atomic_<version>_<os>_<arch>.tar.gz` (or `.zip` on Windows) with the binary, README, and LICENSE inside. SHA256 checksums are published alongside as `checksums.txt`.


### Release flow


1. Bump version in `atomic/internal/version.go` (or use ldflags injection — pick one and stick).
2. Update `CHANGELOG.md` (Keep-a-Changelog format).
3. Tag: `git tag -s v0.1.0 -m "v0.1.0"`. Signed tags preferred.
4. Push tag: `git push origin v0.1.0`.
5. Workflow runs goreleaser; release appears at `https://github.com/damusix/atomic-claude/releases/tag/v0.1.0`.
6. Update `install.sh` only if installation semantics changed — the script reads the latest release dynamically.


### Install script


`install.sh` lives at repo root. End users run:


```bash
curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash
```


The script:


1. Detects OS (`uname -s`) and architecture (`uname -m`, normalized).
2. Resolves the latest release tag via the GitHub API.
3. Downloads the matching archive + checksum, verifies SHA256.
4. Extracts to a temp dir.
5. Moves the binary to `${ATOMIC_INSTALL_DIR:-$HOME/.local/bin}`, creating the directory if needed.
6. Prints a one-line PATH reminder if the install dir is not on `$PATH`.
7. Refuses silently to overwrite a newer existing binary (`atomic --version` check).
8. Prints next-step instructions:
    ```
    atomic v0.1.0 installed at ~/.local/bin/atomic.

    To install the atomic-claude artifact bundle (CLAUDE.md, agents, commands, skills, output-styles)
    into ~/.claude/, run:

        atomic claude install

    To install only signals / reminders helpers without touching ~/.claude/, skip the above.
    ```


The script installs the binary and stops there. It never invokes `atomic claude install` — that is always a separate, explicit user action.


No Go toolchain required at any step.


### Manual install (fallback)


- Download a release archive from GitHub Releases.
- Verify with `shasum -c checksums.txt`.
- Move the binary into any directory on `$PATH`.


### Local build (contributors)


```bash
cd atomic
go build -o ../bin/atomic ./cmd/atomic
```


Or via `make build` at repo root.


## Testing


- **Unit tests** in each `internal/<domain>/` package. Use Go's standard `testing` package; no test framework.
- **Golden-file tests** for frontmatter parser, scanner output, and hook rendering. Fixtures under `atomic/testdata/`.
- **CLI integration tests** under `atomic/internal/cmd_test.go` exercise the full argv → stdout/file path via `t.TempDir()`.
- **No mocks** for filesystem; use real temp dirs. Only network and time get faked (the latter via injected clock).
- Coverage target: 80% on `internal/`. `cmd/atomic/main.go` is wiring — coverage there is incidental.


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| CP-1 | Module skeleton + `--version` + repoctx + frontmatter parser/writer + ids + their tests | `atomic/internal/repoctx/`, `atomic/internal/frontmatter/`, `atomic/internal/ids/` | |
| CP-2 | `signals` subcommand: scanners (tree, manifests, languages), `scan` + `show` + `stale`, golden tests | `atomic/internal/signals/` | |
| CP-3 | `reminder` subcommand: add/list/show/rm, tests | `atomic/internal/reminder/` | |
| CP-4 | `hooks` subcommand: session-start/install/uninstall, golden render tests | `atomic/internal/hooks/` | |
| CP-5 | `claude` subcommand: install/update/list/diff + embed bundle + backup logic + CLAUDE.md proposed-file flow, tests via fake `~/.claude` in temp dir | `atomic/internal/claudeinstall/`, `atomic/internal/embedded/` | |
| CP-6 | `update` (self-update) + background update-check goroutine + cache file + banner, tests via mocked HTTP server | `atomic/internal/selfupdate/` | |
| CP-7 | goreleaser config + GitHub Actions workflow + `install.sh` + README install section | `.goreleaser.yaml`, `.github/workflows/`, `install.sh` | |
| CP-8 | Tag v0.1.0, verify pipeline, smoke-test install script + `atomic claude install` + `atomic update` on macOS + linux | | Manual smoke test |


## Success criteria


- `atomic --version` returns a non-empty version string.
- `atomic signals scan` produces a byte-identical file on two consecutive runs against an unchanged repo.
- `atomic reminder add "x"` creates a file with `id` and `created`; `list` shows it; `rm <id>` deletes it.
- `atomic hooks session-start` emits non-empty output when reminders exist, empty output when none.
- `atomic claude install` into an empty `~/.claude` writes all bundled artifacts and `CLAUDE.md`; rerunning is a no-op (all `unchanged`).
- `atomic claude update` against an existing install where one atomic artifact has been hand-edited backs that file up under `~/.claude/.atomic/backups/<timestamp>/` and overwrites with the bundled version.
- `atomic claude update` against an existing `~/.claude/CLAUDE.md` writes `.atomic/proposed/CLAUDE.md` and prints the merge instruction; it never overwrites `CLAUDE.md` directly.
- `atomic claude install --dry-run` makes no filesystem changes; output enumerates would-be actions.
- `atomic update --check` against a current binary exits 0; against a stale binary exits 1 and prints the available version.
- Background update check fires on every command invocation, never blocks the foreground, and prints the banner at most once per 24h.
- A goreleaser run produces archives for all matrix targets; `install.sh` successfully installs `atomic` on macOS arm64 from a published release.


## Open follow-ups


- Codesigning macOS binaries — out of scope for v0.1.0; revisit when adoption justifies the Apple Developer cost.
- Homebrew tap — defer until v0.2.0; the install script is sufficient for early users.
- Windows hook integration — the binary builds, but `atomic hooks install` writes POSIX shell scripts. Windows users get manual instructions only.


## Implementation log


### v0.1.0 candidate — built 2026-05-16/17

Built across 11 iterations of `/subagent-implementation`. Commits chronologically (base `d836faa`):

- `e029254` — CP-1 module skeleton (after iter-2 fixups)
- `0cb1362` — CP-2 signals subcommand
- `7964274` — CP-3 reminder subcommand
- `6117f9a` — CP-4 hooks subcommand
- `93c7531` — CP-5 claude bundle subcommand (embedded artifacts + Install/Update/List/Diff + CLAUDE.md proposed-file flow)
- `57f9d26` — CP-6 self-update + background banner (after iter-7 fixups)
- `fb4b20d` — CP-7 release pipeline (goreleaser + GitHub Actions + install.sh)
- `eb2f979` — iter-10 signals polish (real tree shape, recursive manifests, ordered frontmatter)
- `72cbcde` — iter-11 big polish pass (drained 22+ FOLLOWUPS across all packages; landed hujson + EXDEV mitigation)
- `f5080d2` — iter-11 reviewer fixups (named-return checked-defer in `renameCrossFS`; real `_os()` invocation in install-sh test)
- `6c6941f` — feat(signals): annotate tree dirs with child counts
- `ee54fc0` — fix(signals): plural agreement + render dir entries at depth cap
- `eb45ac0` — feat: wire release-please for automated semver (iter 12)

**Out-of-scope work performed during this build:**

- `FOLLOWUPS.md` as a scratchpad primitive — emerged during iter 11 when the user noted non-blocking reviewer findings needed durable tracking. Promoted to `/subagent-implementation` Phase 3 (`ba44c96`).
- `Implementation log` gate — emerged at finalize; promoted to `/subagent-implementation` Phase 3 (`0e68cdb`). The section you're reading is its first use.
- Signals scanner rewrite — original CP-2 was a simple `WalkDir` walk; production usage exposed that depth-3 caps hid useful directories, manifests at any depth were missed, and tree output didn't read like `tree(1)`. Reworked to use `git ls-files` enumeration, recursive manifest detection, proper tree glyphs with child counts (iter-10 + the small UX iterations after iter-11).
- `github.com/tailscale/hujson` for hooks settings.json merge — beyond original spec; folded in after research surfaced it as the right way to preserve user comments + trailing commas.
- EXDEV mitigation for self-update — stage candidate binary in install dir rather than `$TMPDIR`, with a copy-fallback `renameCrossFS` helper. Came from research mirroring `inconshreveable/go-update`'s convention.
- `release-please-action` — beyond original spec; came from research as the right pairing with goreleaser (release-please does semver bump + tag; goreleaser does artifact publish).
- `docs/spec/signals-project-detection.md` — separate spec drafted during one of the polish iterations.

**Unforeseens — surprises that emerged during implementation:**

- `yaml.v3` key-order non-determinism: solved via `EmitOrdered([]KV, body)` building `yaml.Node` mappings with caller-specified order. Backward-compatible `Emit(map)` keeps alphabetical sort for the deterministic path.
- macOS case-insensitive filesystem aliases `CLAUDE.md` ↔ `CLAUDE.md` — only one is git-tracked but both are reachable. Edits propagated correctly; documented as a non-issue.
- `go get github.com/tailscale/hujson` bumped `go.mod` to `go 1.23` because hujson declares `go 1.23`. Manual `go mod edit -go=1.22` is reverted by the next `go mod tidy`. Kept at 1.23.
- The spec referenced `commands/atomic-claude-merge.md` for the CLAUDE.md merge workflow, but the file doesn't exist yet — the proposed-file flow still works because the slash command is the user's later action.

**FOLLOWUPS disposition:**

39 findings raised across CP-1..CP-7 + iter-10/iter-11 polish. All 39 closed before finalize. Final ledger summary at `.claude/.scratchpad/2026-05-16-atomic-binary/FOLLOWUPS.md` before scratchpad teardown.

**Deferred items still open:**

- CP-8 manual smoke (tag `v0.1.0`, watch goreleaser pipeline end-to-end on real GitHub, run `atomic claude install` + `atomic update` on macOS and linux from a published release) — manual gate; happens once at first real release.
- `/atomic-claude-merge` slash-command + `atomic-claude-merger` agent — referenced by `### CLAUDE.md handling` but not yet created. Future spec.
- macOS code signing, Homebrew tap, Windows hook integration — listed in `## Open follow-ups` above; deferred to v0.2.0+.


## Change log


### 2026-06-03 — signals stale becomes content-based

**What changed:** `signals stale` no longer compares mtimes. It assembles the deterministic body exactly as `scan` would (via a shared `resolveScanOptions` helper) and compares it to the stored `deterministic-signals.md` body; stale iff they differ. `StaleInfo` now carries `ChangedLines` (a multiset line-delta magnitude) instead of `Count`/`Newest`; the exit-1 output reads "a fresh scan would change the deterministic snapshot (~N lines)". The mtime walker (`scanSourceTree`/`sourceScan`) is removed. The `signals-gate` partial and `signals stale` row were reworded from mtime to content framing.

**Why:** The mtime model had a real false-positive class: any commit that regenerates a tracked file (e.g. the pre-commit `make bundle` rewriting `manifest.go` with identical bytes) bumps the file's mtime without changing what a scan produces, so `stale` reported stale forever after every signals-touching commit — a treadmill. Content comparison reads identical-byte regeneration as fresh while still catching real project-map shifts. This makes the strict "exit 1 ⇒ always refresh" gate wording honest (no false positives to rationalize around). Cost rises from a stat to a full tree assembly, which is acceptable for a once-per-ship-verb gate.

**Superseded:** the same-day "check-family exit-code convention" entry below described `signals stale` evidence as "count of newer source files, newest repo-relative path" from an mtime walk — that evidence is now the content line-delta. The mtime-based staleness definition (and the `signals-workflow.md` open question about mtime vs content hashing) is resolved in favor of content comparison.

### 2026-06-03 — check-family exit-code convention (0 / 1 / 2)

**What changed:** Documented and enforced one exit-code convention across the check / status commands (`signals stale`, `signals diff`, `docs stale`, `update --check`): exit 0 = nothing actionable, exit 1 = actionable signal (to stdout), exit 2 = hard error (to stderr). New "Exit code convention" section under `## CLI surface`. Three commands were brought into line: `signals stale` now returns exit 2 on hard error (was 1) and prints evidence-bearing imperative output to stdout on exit 1 (count of newer source files, newest repo-relative path, directive to dispatch the inferrer); `signals diff` returns exit 2 on a generic hard error (was 1, alongside its existing `ErrNoPrior`→2); `update --check` returns exit 2 on network/parse failure (was 1), reserving exit 1 for "update available". The `signals.Stale` Go signature changed from `error` to `(StaleInfo, error)`; the `ErrStale` sentinel is unchanged so call-site identity comparisons still hold. The `signals-gate` shared partial was rewritten to a three-branch exit-code gate that forbids second-guessing a stale verdict. Help text for `signals stale` and `docs stale` updated to "0 fresh, 1 stale, 2 error".

**Why:** Each check command had independently overloaded exit 1 to mean both "actionable signal" and "error", with `signals stale` additionally silent. The signals refresh gate is read by an LLM orchestrator, and the silent overloaded exit 1 let it rationalize skipping a warranted refresh, risking project-map drift. Unifying on the `diff(1)`/`cmp(1)` idiom and emitting imperative output is a deliberate model-safeguard layer over the deterministic exit code (the prefer-code-over-model exception). Detection logic (mtime-based) is unchanged — no false-positive shift.

**Superseded:** `signals stale`, `signals diff` (generic error path), and `update --check` previously returned exit 1 for both their actionable signal and hard errors; `signals stale` produced no stdout output on any path; `signals.Stale` returned a bare `error`.

### 2026-05-30 — hooks install inlines the command, drops the wrapper script

**What changed:** `atomic hooks install` no longer writes a `session-start-reminders.sh` wrapper script. It registers the command `atomic hooks session-start` directly in `settings.json` under `hooks.SessionStart`. Install migrates older installs (removes the legacy script-path registration and deletes the stale script file); uninstall removes both the inline and legacy registrations and any lingering script. `IsInstalled` reports `drifted=true` when only the legacy wrapper-script registration is present (functional but stale), replacing the old "script content differs" drift meaning. Doctor's hooks check (category 2) had a scope bug — it passed `~/.claude` where `IsInstalled` expects `$HOME`, doubling the segment to `~/.claude/.claude/settings.json` and reporting a correctly-installed hook as missing; now fixed to resolve `$HOME`.

**Why:** The wrapper was a pure passthrough (`exec atomic hooks session-start`) with identical PATH semantics to the inline command, so it bought nothing while adding a file, a `expectedScriptContent` constant, and a content-drift code path. Claude Code runs hook commands through a shell, so the multi-word command execs directly. The doctor scope bug shipped because the test seam (`RunCheckHooksWith`) bypassed the production resolver.

**Superseded:** `install` previously wrote the hook script to `<scope>/.claude/hooks/session-start-reminders.sh` and registered that path as the hook `command`; `uninstall` removed that script; doctor reported `drifted` when the script content differed from the canonical wrapper text.


### 2026-05-23 — Correction: stale paths updated to ~/.claude/.atomic/

**Correction:** Body references to backup and proposed-merge paths were still using the pre-consolidation locations (`~/.claude/.atomic-backups/<ts>/`, `~/.claude/CLAUDE.md.atomic-proposed`). Code diverged when `docs/spec/atomic-state-and-config.md` consolidated all atomic-owned state under `~/.claude/.atomic/` and `atomic/internal/config/paths.go` was updated. Corrected to `~/.claude/.atomic/backups/<ts>/` and `~/.claude/.atomic/proposed/CLAUDE.md` throughout the body. Affected sections: `atomic claude` verb table, per-file flow, CLAUDE.md handling, backups, final report, success criteria.

### 2026-05-17 — atomic doctor subcommand

**What changed:** Documented `atomic doctor` (eight-check integrity verb with `--fix` repair mode) as a new `### atomic doctor` H3 section in the CLI surface. Full behavioral contract lives at `docs/spec/atomic-doctor.md`; this section is a summary + flag table.

**Why:** CP-8 of atomic-doctor implementation — wire the CLI surface into the binary's subcommand inventory so the spec inventory stays current.


### 2026-06-10 — Block-aware CLAUDE.md handling

**What changed:** Per-file flow step 6 and § CLAUDE.md handling rewritten: when the on-disk `~/.claude/CLAUDE.md` carries exactly one parseable `<atomic>...</atomic>` block, install/update compares blocks only — equal → `unchanged`, stale → backup + in-place block replacement (`block replaced`). `diff` reports `match` for merged files with a current block. The proposed-file path remains only for files without a parseable block.

**Why:** whole-file SHA compare flagged permanent drift on every merged CLAUDE.md and forced a slow LLM merge on each update for a boundary code can draw deterministically.

**Superseded:** prior contract: any CLAUDE.md difference → proposed file + `/atomic-claude-merge`, no exceptions.


### 2026-06-10 — `atomic update` artifact auto-refresh

**What changed:** `### atomic update` verb table rewritten: a successful binary swap now auto-refreshes `~/.claude` artifacts by default, re-execing the new binary (`claude update --no-update-check`, plus `--no-hooks` when no session-start hook is registered) before the post-update doctor. New `--no-doctor` and `--skip-claude-update` rows. The out-of-sync nudge is removed (from `selfupdate.Update` and everywhere else) — the refresh replaces it. (Same-day consolidation: an intermediate `--binary-only` flag with a managed-install detection gate was replaced by this assume-update design before release.)

**Why:** every release required a manual `atomic claude update` follow-up and doctor flagged the gap as drift. Full contract: `docs/spec/atomic-update-doctor.md` § Artifact auto-refresh contract.

**Superseded:** prior contract: update swapped the binary only and always printed the out-of-sync nudge when `~/.claude/CLAUDE.md` existed.

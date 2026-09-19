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
├── context/                         # the authored corpus: AGENTS.md, agents/, commands/,
│                                    #   skills/, output-styles/, rules/, _partials/
├── atomic/                          # Go module root
│   ├── go.mod                       # module: github.com/damusix/atomic-claude/atomic
│   ├── cmd/atomic/                  # Cobra verb tree + one cmd_<verb>.go per verb family
│   ├── internal/
│   │   ├── artifacts/               # canonical corpus enumeration (artifacts.Load) + projections
│   │   ├── bundlespec/              # bundle inclusion predicates + steering source descriptors
│   │   ├── bundlemirror/            # maps the canonical corpus to Claude-native targets at build
│   │   ├── embedded/                # go:embed of the generated bundle
│   │   ├── claudeinstall/           # Claude-only bundle install: install/update/list/diff/uninstall
│   │   ├── harness/                 # harness discovery + the Claude and OMP adapters
│   │   ├── install/                 # lifecycle engine: plan/apply/converge, lock, journals
│   │   ├── installstate/            # enrollment ledger, journals, transaction records
│   │   ├── rules/                   # RuleRecord/RuleInstance, matching, enforcement tiers
│   │   ├── config/                  # config.toml + ~/.atomic paths, state-location ladder
│   │   ├── doctor/                  # integrity checks (23 stable categories)
│   │   ├── selfupdate/              # release lookup, staged swap, update lock/state
│   │   ├── signals/                 # scanners (tree, manifests, languages)
│   │   ├── reminder/                # reminder storage: add/list/show/rm
│   │   ├── hooks/                   # hook-output rendering + settings.json registration
│   │   ├── repoctx/ frontmatter/ ids/  # repo root, frontmatter parse/emit, id generation
│   │   └── ...
│   └── ...
├── .github/workflows/release.yml    # goreleaser pipeline
├── install.sh                       # one-line installer for end users
└── ...
```


Module path: `github.com/damusix/atomic-claude/atomic`. The `atomic/` subdirectory keeps Go code out of the markdown-heavy repo root.


## CLI surface


Global flags, honored from any position in argv before the verb's own parse:


- `--repo <path>` — repo root override (default: detect from `cwd` via git).
- `--version` / `-v` — print `atomic <version> (<commit>)` and exit.
- `--no-update-check` — suppress the background self-update check for this invocation.


Machine-readable output is not a global flag: verbs that support it take their own `--json`.


The invoked process performs no network I/O for version checking. It renders the update-available banner from `~/.atomic/state.json` — printed to stderr at most once per 24h — and, unless the verb is `atomic update` or `--no-update-check` was passed, it may spawn a detached `atomic update --check` child to refresh that state, capped at one lookup per hour. The child does the GitHub call; if it is gated off (`update.check = false`) or still in flight, the banner simply reflects the last recorded state. Full cadence and lock rules: `docs/spec/selfupdate-state.md`.


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
| `add <text>` | Create a reminder file under the project-keyed reminders directory (`config.ProjectRemindersDir`; see `docs/spec/serve-plans-page.md`). Prints the assigned id. |
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


The Claude-only artifact-bundle path: install / update / list / diff / uninstall of the embedded corpus into a Claude artifact root. Artifact content is embedded in the binary at build time, so a single binary install delivers a versioned, self-contained artifact set with no network or repo clone required. The default root comes from the Claude adapter's `DefaultConfigDir(home)`, so `CLAUDE_CONFIG_DIR` relocates it exactly as the generic lifecycle verbs resolve it; `--target <dir>` overrides for a project-scoped install. This path is independent of the multi-harness install engine below — it never enrolls a target, and it is not what `atomic update` converges.


| Verb | Description |
|------|-------------|
| `install [--dry-run] [--target <dir>] [--no-hooks]` | First-time install. Writes embedded artifacts to the target root and registers the session-start hook unless `--no-hooks`. Refuses to touch any file outside the bundle manifest. For an existing `CLAUDE.md`, applies block-aware handling: replaces a stale `<atomic>` block in place, or — when the file has no parseable block — writes the proposed version to `~/.atomic/proposed/CLAUDE.md` for the `atomic prompt claude-merge` cold-op (see [CLAUDE.md handling](#claudemd-handling)). |
| `update [--dry-run] [--target <dir>] [--no-hooks]` | Refresh an existing install. Diff every embedded artifact against its on-disk counterpart, back up changed files to `~/.atomic/backups/<ISO-timestamp>/`, then overwrite. Same `CLAUDE.md` handling as `install`. |
| `list` | Print the artifact manifest embedded in this binary version: one row per artifact (kind, name, sha256). Useful for diffing against the target state. |
| `diff [--target <dir>]` | Show, per artifact, whether the on-disk file matches, differs, or is absent. Read-only. Pairs with `--dry-run` for safety review. |
| `uninstall [--target <dir>]` | Emit a markdown prompt that a Claude Code session executes against the write-once pre-install snapshot. The binary emits the plan; it does not delete files itself. |


### `atomic install`


Converges Atomic into explicitly selected harness instances and enrolls them. It never discovers-then-installs: a harness must be named with `--harness`, or `--all` must be passed.


```
atomic install [--harness claude|omp] [--instance <root> | --all] [--replace | --leave-unowned] [--dry-run] [--yes] [--json]
```


`--replace` and `--leave-unowned` are the batched decision for unowned older-version artifacts the selected generation cannot prove; they are mutually exclusive. `--dry-run` reports the plan and writes nothing. `--yes` approves the printed plan without prompting. A blocked target reports its blockers and exits non-zero.


### `atomic harness`


The lifecycle surface over enrolled targets. Discovery is read-only and never enrolls; only `atomic install`, `atomic harness enroll`, and `atomic harness adopt` create enrollment.


| Verb | Description |
|------|-------------|
| `list [--json]` | List discovered and enrolled instances, marking each `discovered` or `enrolled`. |
| `status [<target-key>] [--json]` | Report enrolled target and shared-resource state, plus retained unresolved journals. |
| `enroll <claude\|omp> [--instance <root>] [--replace\|--leave-unowned] [--dry-run] [--yes] [--json]` | Explicitly enroll a harness instance and converge it. |
| `adopt [claude] [--instance <root>] [--replace\|--leave-unowned] [--acknowledge-snapshot] [--dry-run] [--yes] [--json]` | Adopt a verified legacy Claude install into the ledger. |
| `repair [--harness <kind>] [--instance <root>] [--dry-run] [--yes] [--json]` | Reconverge already-enrolled targets. |
| `diff [--harness <kind>] [--instance <root>] [--json]` | Report each enrolled resource's native difference from the selected generation. Read-only. |
| `uninstall <target-key> [--dry-run] [--yes] [--json]` | Remove one enrolled target. |
| `uninstall --all [--dry-run] [--yes] [--json]` | Remove every enrolled target, then completed operational and adoption state. |
| `rules status [--json]` | Report rule tier, source/projection digests, coverage, and conflict state per target. |
| `rules sync [--dry-run] [--yes] [--json]` | Converge rule and steering projections for enrolled targets. |


Shared lifecycle invariants — held by every mutation, never by discovery:

- One advisory lifecycle lock is held for the operation.
- Unresolved journals are recovered oldest-first before any planning.
- The target is re-observed before the plan is built, so the plan reflects current bytes.
- A plan that cannot be decided reports `blocked` and mutates nothing.

Uninstall retention, target selection, and the legacy-migration state machine are the multi-harness spec's contract — see `docs/spec/omp-plugin-compatibility.md`. Do not restate them here.


### `atomic state`


Manages the harness-neutral repository-state selection.


| Verb | Description |
|------|-------------|
| `adopt [--dir <segment\|absolute>] [--clear] [--dry-run] [--json]` | Select this repository's state root. With no flags it adopts the single unambiguous populated candidate; `--dir` selects one explicitly; `--clear` drops the persisted selection so the neutral ladder decides again. `--clear` and `--dir` are mutually exclusive. |


Resolution itself follows the repository-state ladder (process-only `ATOMIC_STATE_DIR`, then the persisted selection record, then the user `state.dir`, then the built-in fallback) — contract in `docs/spec/omp-plugin-compatibility.md`.


### `atomic update`


Self-update the binary. Foreground check (not the background lookup other commands fire). Network required.


| Verb / flag | Description |
|-------------|-------------|
| (default) | Check GitHub Releases for the latest tag. If newer than the running version, download the matching archive + checksum, verify SHA256, then replace the running binary in place atomically. When the swap replaced the running binary, this process still embeds the pre-swap corpus and refuses every convergence path: it re-execs the replacement as `atomic update --__target-converge`, carrying `--skip-claude-update` and `--no-doctor` through. The replacement re-acquires the lifecycle lock, recovers unresolved journals oldest-first, and converges enrolled targets with its own embedded generation, then runs migrations and the post-update doctor (see `docs/spec/atomic-update-doctor.md`). When nothing was swapped — already current — the same process converges enrolled targets in place. A normal update never lets the stale process publish its corpus. |
| `--check` | Only check, don't apply. Read-only: it downloads nothing and touches no target. Exit 0 if up-to-date, exit 1 if a newer version is available (prints `update available: ...` to stdout), exit 2 on a hard error such as a network or parse failure (stderr explains). Follows the check-family exit convention — exit 1 is the "update available" signal, not an error. |
| `--channel <stable\|prerelease>` | Release channel. Precedence: this flag, then `update.channel` in `~/.atomic/config.toml`, then `stable`. `stable` considers only non-prerelease tags and only ever moves forward. `prerelease` considers both and tracks the tip: it installs any tag differing from the running one, including a lower-numbered pre-release cut after a stable release of the same core. An unknown value exits 2. |
| `--pre` | Shorthand for `--channel prerelease`. A one-shot override that never writes config. Combined with an explicit `--channel stable`, exits 2 rather than silently preferring one. |
| `--no-doctor` | Skip the post-update doctor self-check. |
| `--skip-claude-update` | Skip the post-swap enrolled-target convergence; binary swap only. |
| `--force` | Bypass the update lock (e.g. a lock left by an abandoned run). Never weakens checksum verification. |


### `atomic doctor`


Integrity check for the atomic-claude install, the enrolled harness targets, and current project state. Runs 23 stable-category checks and reports PASS / WARN / FAIL / SKIP per category. Non-zero exit on FAIL for CI gating. Opt-in repair via `--fix`. Full contract: `docs/spec/atomic-doctor.md`.


```
atomic doctor [--fix] [--json] [--only <cat[,cat...]>] [--skip <cat[,cat...]>] [--stale-days N] [--verbose]
```


| Flag | Effect |
|------|--------|
| `--fix` | Per-item confirm prompt before applying any repair. Implies interactive. Ledger-managed repairs route through the install engine's converge planner (`install.Steps.Converge` with `EnrolledOnly`): it takes the lifecycle lock, recovers unresolved journals oldest-first, re-observes, and converges only already-enrolled targets. Report-only categories are non-fixable and are counted, never auto-repaired. |
| `--json` | Emit machine-readable result to stdout (schema_version 1). Suppresses human output. `--fix` + `--json` is a usage error (exit 2). |
| `--only` | Comma-separated category indices (`1,3`) or names (`install,signals`). |
| `--skip` | Same syntax as `--only`. Skip listed categories. |
| `--stale-days` | Override stale-signals threshold (positive int, default 7). |
| `--verbose` | Print per-file detail for `install` and `manifest` checks. |


Check categories (indices stable; never renumber):


| # | Name | Fail severity |
|---|------|---------|
| 1 | `install` | WARN drift / FAIL missing |
| 2 | `hooks` | WARN |
| 3 | `signals` | WARN |
| 4 | `refs` | FAIL |
| 5 | `manifest` | FAIL (repo-dev only; SKIP elsewhere) |
| 6 | `followups` | WARN |
| 7 | `memory` | WARN |
| 8 | `binary` | WARN |
| 9 | `config` | WARN |
| 10 | `profile` | WARN |
| 11 | `code-index` | WARN |
| 12 | `migrate` | WARN |
| 13 | `repo-config` | WARN |
| 14 | `output-style` | WARN |
| 15 | `targets` | WARN — enrolled targets reported against read-only discovery; a disagreement between the ledger and native registration is the defect |
| 16 | `resources` | WARN — every ledger-owned physical resource, its consumers, and the unenrolled instances that can merely see it |
| 17 | `journals` | WARN — unresolved lifecycle journals |
| 18 | `capabilities` | WARN — rule/hook capability gaps per target |
| 19 | `rules` | WARN — rule tier, digests, coverage, conflicts |
| 20 | `trust` | WARN — disabled or untrusted native hook/enforcement state |
| 21 | `staleness` | WARN — materializations behind the selected generation |
| 22 | `conflicts` | WARN — divergent or ambiguous ownership evidence |
| 23 | `shadowing` | WARN — effective-content shadowing across scopes |


Categories 9–14 predate the multi-harness lifecycle; 15–23 were appended with it. Indices are stable: never renumber, only append. The third column is each category's default severity (and for 15–23, what it reports); an individual result may override it.


Exit codes: 0 = all PASS/WARN/SKIP (also: `~/.claude/` absent *and* no target enrolled — short-circuit); 1 = any FAIL; 2 = usage error.


### Invocation responsibility


`atomic claude install` and `atomic claude update` are *always run by the user explicitly*; the binary never auto-runs this standalone Claude-only path. Drift between the embedded bundle and the target is the user's call to resolve, on their schedule.

`install.sh` installs the binary and stops. It invokes no install verb. The user's next step is an explicit `atomic install` (multi-harness; enrolls and converges the named target) or, for the Claude-only bundle path, `atomic claude install`.

`atomic update` is the one automatic convergence point, and only for targets already enrolled: after a swap the replacement binary converges them with its own embedded generation, and an already-current run converges them in place. Neither `atomic update` nor discovery ever enrolls a target.


| Scenario | What user does |
|----------|----------------|
| First-time setup | After `install.sh` finishes, the user runs `atomic install --harness claude` (enrolls and converges) or the Claude-only `atomic claude install`. |
| Refresh an enrolled target after `atomic update` | Automatic — the replacement binary converges already-enrolled targets. |
| Refresh the Claude-only bundle path | The user runs `atomic claude update` themselves. |
| Second machine | Same as first-time setup. |
| Project-scoped Claude install | `atomic claude install --target ./.claude`. |
| Forced re-sync of an enrolled target | `atomic harness repair`. |


`install.sh` prints next-step instructions but invokes nothing. `atomic update` never runs `atomic claude install`. No auto-chains beyond `atomic update`'s enrolled-target convergence.


## File conventions


All `atomic`-managed files use YAML frontmatter and are plain markdown.


### Reminder file


Path: `<config.ProjectRemindersDir(root)>/<YYYY-MM-DD>-<slug>.md` — `~/.atomic/<project-key>/reminders/<YYYY-MM-DD>-<slug>.md`. See `docs/spec/serve-plans-page.md`.


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


At build time `artifacts.Load` enumerates `context/` once into the canonical corpus (identity `<kind>:<source>`, rendered body, source digest), expanding any `{{ template "<name>" . }}` directive against `context/_partials/` exactly once. `bundlemirror` maps that corpus to Claude-native targets and writes the generated bundle plus `manifest.go`; `//go:embed bundle` compiles the tree into the binary. `atomic <version> (<commit>)` reports what was built — there is no separate bundle-commit field.


Inclusion is decided by pure `bundlespec` predicates over the canonical corpus, not a hand-maintained allowlist:


| Kind | Rule |
|------|------|
| agent | `context/agents/atomic-*.md`, files only |
| skill | `context/skills/atomic-*/` containing `SKILL.md`, whole subtree |
| output-style | `context/output-styles/atomic*.md` |
| command | `context/commands/**/*.md`, recursive, no allowlist |
| rule | `context/rules/**/*.md` |
| steering | `context/AGENTS.md`, exact name — the sole authored global contract |


`context/_partials/*.md` is a template pool, never itself a bundle target. A new file matching an existing rule is picked up with no Go change; a new artifact *kind* needs a predicate plus a walk and a target mapping. Canonical corpus and target-projection contract: `docs/wiki/bundle.md`.


The authored global contract is `context/AGENTS.md`. Under Milestone A the Claude adapter projects it directly into `~/.claude/CLAUDE.md` — it never creates a user-level `AGENTS.md` and installs no global loader. Repository and realm scopes instead use the loader pair: authored `AGENTS.md` plus an adjacent thin `CLAUDE.md` whose body is exactly `@AGENTS.md`. That projection is the multi-harness spec's contract — see `docs/spec/omp-plugin-compatibility.md`; this spec only consumes it. There is no dedicated merger agent and no merge slash command: the cold-op that merges a divergent global file is `atomic prompt claude-merge`.


### Install / update semantics


Targets (relative to `--target`, default `~/.claude`):


| Source (in bundle) | Target |
|--------------------|--------|
| `CLAUDE.md` (the Claude projection of `AGENTS.md`) | `CLAUDE.md` |
| `agents/atomic-*.md` | `agents/atomic-*.md` |
| `commands/**/*.md` | `commands/**/*.md` |
| `skills/atomic-*/SKILL.md` | `skills/atomic-*/SKILL.md` |
| `output-styles/atomic.md` | `output-styles/atomic.md` |
| `rules/<lang>/*.md` | `rules/<lang>/*.md` |


Sources are already Claude-native: `bundlemirror` stripped the `context/` prefix (and the `claude-md` install kind) at build time, so rule files land at `<target>/rules/...`, matching Claude Code's expected layout.


Per-file flow:


1. Compute sha256 of the embedded source.
2. Read the on-disk target (if any), compute its sha256.
3. If shas match → skip, report as `unchanged`.
4. If on-disk file is missing → write source, report as `installed`.
5. If on-disk file is bundle-managed (its target path appears in the bundle manifest) and differs → back up to `~/.atomic/backups/<ISO-timestamp>/<relative-path>`, then overwrite, report as `updated (backup at <path>)`.
6. If on-disk file is `CLAUDE.md` and differs → block-aware comparison. When both the embedded source and the on-disk file carry exactly one parseable `<atomic>...</atomic>` block: equal blocks → report as `unchanged` (user content outside the block is not drift); different blocks → back up the whole file to `~/.atomic/backups/<ISO-timestamp>/CLAUDE.md`, replace only the block in place, report as `block replaced`. When the on-disk file has no parseable block (pre-tag install, unclosed or duplicate tags) → write source to `~/.atomic/proposed/CLAUDE.md`, report as `merge required (proposed at <path>)`.
7. If on-disk file does not appear in the bundle manifest and is not `CLAUDE.md` → refuse to touch, report as `skipped (not owned by atomic)`. Defensive guard against accidental writes outside the bundle.


"Bundle-managed" is defined by the embed manifest, not by filename prefix. Most are `atomic-*`-prefixed; rule files (`rules/<lang>/*.md`) are not, but are still bundle-managed and therefore atomic-owned for backup/overwrite purposes.


`--dry-run` skips step 4–6 writes and prints what *would* happen.


### CLAUDE.md handling


`CLAUDE.md` mixes two ownership zones: the `<atomic>...</atomic>` block is atomic-owned (a versioned contract), everything outside it is user-owned. The binary draws that boundary with a line-anchored parser (a line whose trimmed content is exactly `<atomic>` opens the block, `</atomic>` closes it; inline mentions never match) and handles each zone deterministically:


**Block path (on-disk file has exactly one parseable `<atomic>` block).** Equal blocks → no action; user content outside the block never registers as drift, in install/update, `diff`, or doctor check 1. Stale block → the binary backs up the whole file, splices the embedded block over the on-disk block byte-for-byte, and preserves everything outside it. User edits *inside* the block are overwritten (recoverable from the backup) — atomic-owned content is a versioned contract, and silently preserving divergent edits inside it would leave the user running a patched version they can't diff against upstream. No proposed file, no merge step.


**Merge path (no parseable block — pre-tag install, unclosed or duplicate tags).** Code cannot draw the ownership boundary safely, so it defers to the LLM merge — it never spawns Claude, never edits CLAUDE.md itself:


1. Binary writes the embedded `CLAUDE.md` content to `~/.atomic/proposed/CLAUDE.md`.
2. Binary prints:
    ```
    CLAUDE.md needs review.
      old: ~/.claude/CLAUDE.md
      new: ~/.atomic/proposed/CLAUDE.md

    in a Claude Code session, run `atomic prompt claude-merge` to merge your config.
    Or inspect manually:  diff ~/.claude/CLAUDE.md ~/.atomic/proposed/CLAUDE.md
    ```
3. User runs `atomic prompt claude-merge` themselves, on their own schedule. The cold-op brief (embedded in the binary at `atomic/internal/coldprompt/briefs/claude-merge.md`) walks a Claude Code session through reading both files and producing a merged report, staging at the explicit `## Staging gate` — it does NOT ask the user to accept, apply, or remove the proposed file. The user's interactive session (the dispatcher) presents the report, requires explicit acceptance, and applies the staging file only on accept; the binary never spawns Claude, never edits `CLAUDE.md` itself, and never removes `~/.atomic/proposed/CLAUDE.md` (that removal is the dispatcher's explicit action). See [`install-workflow.md`](./install-workflow.md).


First-time install (no existing `CLAUDE.md`):


- Steps 1–3 are skipped. The embedded content is written directly to `~/.claude/CLAUDE.md`. No proposed file, no merge step needed.


### Why no auto-invoke


The binary never spawns Claude. Three reasons:


- The user dictates *when* a global config change applies. Binary-spawning-editor flows are surprising and cross tool boundaries.
- The merge step can be deferred — user might want to inspect the proposed file first, or schedule it for a quiet moment.
- Destructive-ops axiom: the `atomic prompt claude-merge` cold-op has its own confirmation gate. The right place for explicit confirmation is at the merge, not at launch.


### Backups


Path: `~/.atomic/backups/<ISO-timestamp>/<relative-path>`


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

Updated (2, backed up to ~/.atomic/backups/2026-05-16T18-32-11Z/):
  ↻ agents/atomic-reviewer.md
  ↻ commands/commit-only.md

Unchanged (5):
  • commands/merge-to-main.md
  • commands/git-cleanup.md
  • commands/setup-wiki.md
  • agents/atomic-investigator.md
  • rules/python/test-layout.md

Needs review (1):
  ⚠ ~/.claude/CLAUDE.md
    proposed at ~/.atomic/proposed/CLAUDE.md
    next step: in a Claude Code session, run `atomic prompt claude-merge`
```


## Self-update


### Lookup


Source: GitHub Releases API for `damusix/atomic-claude`. Authenticated only if `GITHUB_TOKEN` is set (avoids unauthenticated rate limits on heavy users). One HTTP call per lookup.


### Foreground vs background


- **Foreground** (`atomic update`): block on the lookup, perform the download + verify + swap synchronously, then own post-swap convergence. No banner is involved.
- **Background** (any other invocation, unless `--no-update-check` is set or the verb is `update`): the process renders the banner from `~/.atomic/state.json` and, at most once per hour, spawns a detached `atomic update --check` child. The child performs the GitHub lookup and writes the result back to state; the parent performs no network I/O and never blocks on the child. The banner prints at most once per 24h.


### State


`~/.atomic/state.json` holds one `update` block: `last_check`, `updating`, `update_started_at`, `updated_at`, `last_notified`, `latest_version`, `stage_attempted_for`, `last_result`, and `staged{version,path,sha256}`. `atomic` is its only writer, atomically via temp+rename; it is never hand-edited. The staged archive lives under a fixed, disposable `~/.cache/atomic/staged/`, and the `staged` field — not the file's mere presence — is the authority on what is staged. Full schema, spawn cadence, and lock-acquisition rules: `docs/spec/selfupdate-state.md`; the `update.check` / `update.stage` / `update.channel` config gates: `docs/spec/atomic-state-and-config.md`.


### Replace flow (foreground)


1. Resolve the latest release for the channel — a fresh lookup; the state's cached `latest_version` is never trusted for a swap.
2. If an archive matching the tag is already staged and its checksum re-verifies, swap from it. Otherwise pick the asset matching `<os>_<arch>`, download archive + `checksums.txt`, and verify SHA256.
3. `os.Rename(newBinary, currentBinary)` — atomic on POSIX, against the symlink-resolved running binary. On error (cross-device or permission), print the error and a `sudo install <new> <current>` hint.
4. Print: `updated atomic <old> → <new>.`
5. Convergence: when the swap replaced the running binary, this process still embeds the pre-swap corpus, so it re-execs the replacement as `atomic update --__target-converge` and the replacement owns convergence with its own generation. When nothing was swapped, this process converges enrolled targets in place. See `### atomic update`.


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


- **Branching**: trunk-based on `main`. Feature work lands via worktree isolation at `.claude/worktrees/<branch>` (the `worktree-setup` partial in the implement loop, or Claude Code's own worktree tooling).
- **Versioning**: semver tags `vMAJOR.MINOR.PATCH`. Pre-1.0 is `v0.x.y`; breaking changes bump MINOR until v1.0.
- **Commit style**: Conventional Commits via the `atomic-git-discipline` skill. No AI bylines.
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


1. Bump version in `atomic/internal/version/` (or use ldflags injection — pick one and stick).
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

    To install the artifact bundle (CLAUDE.md, agents, commands, skills,
    output-styles, rules) into ~/.claude/ and register the session-start hook,
    run:

        atomic claude install

    That command sets up the output style and prints next steps for
    initializing project signals. Pass --no-hooks to skip hook registration.

    To install only signals / reminders helpers without touching ~/.claude/,
    skip the above.
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


- **Unit tests** in each `atomic/internal/<domain>/` package. Use Go's standard `testing` package; no test framework.
- **Golden-file tests** for frontmatter parsing, scanner output, and hook rendering; fixtures live in that package's `testdata/` (e.g. `atomic/internal/signals/testdata/`).
- **CLI integration tests** under `atomic/cmd/atomic/` exercise the verb tree end to end via `t.TempDir()` and injected seams.
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


The checkpoint rows above and the `## Implementation log` below are the v0.1.0 build record. Where a row names work a later decision changed or removed — the `claude`/`update` internals, the CLAUDE.md proposed-file path as the only path, doctor's category count — the body and the change log are authoritative. The rows stay as the build record; do not treat them as work to do.


## Success criteria


- `atomic --version` returns a non-empty version string.
- `atomic signals scan` produces a byte-identical file on two consecutive runs against an unchanged repo.
- `atomic reminder add "x"` creates a file with `id` and `created`; `list` shows it; `rm <id>` deletes it.
- `atomic hooks session-start` emits non-empty output when reminders exist, empty output when none.
- `atomic claude install` into an empty `~/.claude` writes all bundled artifacts and `CLAUDE.md`; rerunning is a no-op (all `unchanged`).
- `atomic claude update` against an existing install where one atomic artifact has been hand-edited backs that file up under `~/.atomic/backups/<timestamp>/` and overwrites with the bundled version.
- `atomic claude update` against an existing `CLAUDE.md` replaces a stale `<atomic>` block in place and preserves everything outside it; a file with no parseable block writes `~/.atomic/proposed/CLAUDE.md` and prints the `atomic prompt claude-merge` instruction instead.
- `atomic claude install --dry-run` makes no filesystem changes; output enumerates would-be actions.
- `atomic harness list` reports discovered and enrolled instances without enrolling anything; only `atomic install`, `atomic harness enroll`, and `atomic harness adopt` change enrollment.
- `atomic update` converges only already-enrolled targets; after a swap it does so in the replacement binary's process, never the stale one.
- `atomic update --check` against a current binary exits 0; against a stale binary exits 1 and prints the available version.
- The background version check never blocks the foreground, performs its lookup in a detached child at most once per hour, and prints the banner at most once per 24h.
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


### 2026-09-19 — Correction: doctor's missing-Claude-home short-circuit is conditional

**What changed:** The `atomic doctor` exit-code summary now qualifies its parenthetical: exit 0 on `~/.claude/` absent applies only when the install ledger enrolls no target. An enrolled target — an OMP-only home included — runs the categories instead.

**Correction:** The body's one-line summary still described the unconditional gate. `docs/spec/atomic-doctor.md` owns the full contract and was corrected in the same change; this summary followed it. Found by the CP7E re-review.

**Superseded:** The parenthetical read "`~/.claude/` absent — short-circuit" with no enrollment condition.

### 2026-09-19 — Milestone A lifecycle surface, corpus projection, update convergence, 23-category doctor

**What changed:** The body now describes the Milestone A CLI surface. New sections document `atomic install`, the `atomic harness` verb family (`list|status|enroll|adopt|repair|diff|uninstall|rules status|rules sync`), and `atomic state adopt`, with their shared invariants: read-only discovery never enrolls; every mutation holds one advisory lifecycle lock, recovers unresolved journals oldest-first, and re-observes before planning. `atomic claude` is repositioned as the standalone Claude-only bundle path (install/update/list/diff/uninstall), resolving its default root through the Claude adapter's `DefaultConfigDir` so `CLAUDE_CONFIG_DIR` applies. Bundling is now stated as `artifacts.Load` enumerating `context/` into the canonical corpus, filtered by `bundlespec` predicates and projected by `bundlemirror`; the authored global contract is `context/AGENTS.md` projected directly to `~/.claude/CLAUDE.md`, with the `AGENTS.md` + thin `CLAUDE.md` loader pair for repository/realm scopes. `atomic update` re-execs the replacement as `atomic update --__target-converge` to own post-swap convergence with its own embedded generation; already-current runs converge in place, `--check` is read-only, and `--skip-claude-update` skips convergence. `atomic doctor` is documented as 23 stable categories (15–23 appended), with `--fix` routed through the install engine's `Converge(EnrolledOnly)` planner. The repository-layout tree, testing paths, global flags, background-check mechanism, and success criteria were corrected to match the code.

**Why:** Milestone A moved global steering to `context/AGENTS.md`, added the multi-harness lifecycle for Claude and OMP, changed the update re-exec mechanism, and appended nine doctor categories. The body still described the v0.1.0 Claude-only surface, so a fresh reader would have built removed or superseded behavior.

**Superseded:** The body described a Claude-only CLI with no `install`/`harness`/`state` verbs; global steering authored at the repo-root `CLAUDE.md` and installed as `~/.claude/CLAUDE.md` with no `context/AGENTS.md` source; inclusion by a hand-maintained command allowlist in `embedded/manifest.go`; the CLAUDE.md merge path as a `/atomic-claude-merge` command dispatching an `atomic-claude-merger` agent; post-update artifact refresh by re-execing `claude update --no-update-check`; a background update check as an in-process goroutine over `~/.cache/atomic/update.json`; and `atomic doctor` as eight categories.


### 2026-09-02 — `atomic update` gains `--pre`; the prerelease channel tracks the tip

**What changed:** `atomic update` gains `--pre`, shorthand for `--channel prerelease`. The `--channel` row gains the resolution order — flag, then `update.channel` in `~/.atomic/config.toml`, then `stable` — and rejects an unknown value with exit 2, as does `--pre` combined with an explicit `--channel stable`. The prerelease channel is now defined as a tracking channel: it selects the most recently published eligible release and installs any tag differing from the running one, including a semver-lower one. The stable channel is unchanged in meaning and still selects the highest version.

**Why:** the `next` branch publishes `X.Y.Z-next.N` pre-releases. Release-please's prerelease strategy bumps only the prerelease counter while patch is 0, so the pre-releases following a stable release of the same core carry a lower version than it. A forward-only channel — whether the gate is in the install decision or in the release selection — pins itself to that stable release and never surfaces another pre-release.

**Superseded:** `--channel` previously read "Default `stable` (only non-prerelease tags). `prerelease` includes RC/beta tags", describing the channel as a filter over the same forward-only comparison, with no config fallback and no defined behavior for an unknown value.


### 2026-08-20 — `atomic reminder add` storage moves to the project-keyed home

**What changed:** The `add <text>` verb description and the reminder file's path both point at `config.ProjectRemindersDir(root)` — `~/.atomic/<project-key>/reminders/` — instead of `.claude/.scratchpad/reminders/`.

**Why:** `docs/spec/serve-plans-page.md` relocates reminders to one project-keyed home shared across every worktree of a clone.

**Superseded:** `.claude/.scratchpad/reminders/` as the reminder storage path.

### 2026-07-16 — User state root relocated to ~/.atomic

**What changed:** Every body mention of the backup and proposed-merge paths (`atomic claude` verb table, per-file flow, CLAUDE.md handling, backups section, final report, success criteria) now reads `~/.atomic/...` in place of `~/.claude/.atomic/...`.

**Why:** `docs/spec/configurable-state-paths.md` (issue #150) moves per-user state from `~/.claude/.atomic/` to `~/.atomic/`, with an automatic, idempotent migration (rename + compat symlink at the old path) so pre-relocation `@~/.claude/.atomic/...` refs keep resolving.

**Superseded:** Prior body named `~/.claude/.atomic/` as the state root throughout (itself a 2026-05-23 correction from an even older `~/.claude/.atomic-backups/` naming — see that entry below).

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

### 2026-07-12 — Correction: worktree location in Git workflow

**What changed:** the `## Git workflow` branching bullet now names worktree isolation at `.claude/worktrees/<branch>` via the `worktree-setup` partial (or Claude Code's own worktree tooling).

**Why:** the worktree prescription moved from `.worktrees/` to `.claude/worktrees/` (Claude Code's native worktree home), and the bullet still cited `/worktree-start` — a command that no longer exists in the bundle. Code diverged from the body; corrected in place.

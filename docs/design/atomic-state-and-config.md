# atomic state directory + config (design)


## Problem


Atomic-owned state currently scatters across `~/.claude/`:


| Path | Owner | Purpose |
|------|-------|---------|
| `~/.claude/.atomic-backups/<ts>/` | `claudeinstall` | Pre-install file backups |
| `~/.claude/CLAUDE.md.atomic-proposed` | `claudeinstall` → `/atomic-claude-merge` | Proposed CLAUDE.md merge |
| `<bin-dir>/.atomic.new` | `selfupdate` | Transient staged binary |


Three separate names, three separate cleanup targets, no single place for `atomic doctor` to inspect. Adds friction every time a new piece of state is needed (config, version-check cache, last-run timestamps).


Parallel problem: atomic has no shell-settable defaults. User can't run `atomic config set output.intensity lite` and have Claude pick it up in the next session. Memory (axiom 2) is conversational-only; you can't write to it from a shell script or CI. Tunables that need to survive across sessions *and* be CLI-writable land in no-man's-land.


## Goals


- Consolidate all atomic-owned state under `~/.claude/.atomic/`.
- Introduce `~/.claude/.atomic/config.toml` as the home for shell-settable defaults that steer Claude.
- One target for `atomic doctor` install integrity, `atomic doctor --fix`, and uninstall flows.
- Amend axiom 2 to draw a clear line: conversational tunables → memory; shell-settable defaults → config.


## Non-goals


- Project-local `.claude/.atomic/` directory. Project state already lives in `.claude/project/` (signals, followups). No new project-scoped namespace.
- Replacing memory. Per-conversation tunables stay in memory.
- Moving `.atomic.new` staged-binary path. `os.Rename` atomic-replace requires same filesystem as the target binary — binary may live in `/usr/local/bin`, `~/.local/bin`, Homebrew prefix. Forcing it through `~/.claude/.atomic/` breaks cross-fs setups.
- Bundling `config.toml`. It's user state, not artifact. `claudeinstall` never reads or writes it.


## Layout


```
~/.claude/.atomic/
├── config.toml              # shell-settable defaults (this design)
├── config.resolved.md       # rendered values; @-ref'd from CLAUDE.md
├── backups/<ts>/<relpath>   # was: ~/.claude/.atomic-backups/<ts>/
├── proposed/
│   └── CLAUDE.md            # was: ~/.claude/CLAUDE.md.atomic-proposed
├── cache/                   # reserved: selfupdate version-check, etc.
└── state.json               # reserved: last-update-check, etc.
```


Hidden dotfile (`.atomic/`) matches existing `.atomic-backups` convention and keeps `ls ~/.claude` uncluttered.


## Config file format


TOML. Reasons over JSON/YAML: hand-editable, comments allowed (written by the renderer, not required to round-trip through `set`), no quoting surprises, single dependency (`pelletier/go-toml/v2` or equivalent).


### Schema seed


```toml
[output]
intensity = "full"          # lite | full | ultra

[forge]
host = "github"             # github | gitlab | bitbucket
issues = "github"           # github | gitlab | jira | linear

[cleanup]
stale_days = 60             # /git-cleanup default

[update]
channel = "stable"          # stable | prerelease
auto_check = true           # background version check on every command
```


Flat-namespaced. Add sections; never remove or rename without a migration entry (same discipline as specs).


### Precedence


Highest priority wins:


1. Built-in defaults (in code)
2. `~/.claude/.atomic/config.toml` (user-global) — **the floor**
3. Per-conversation memory override (e.g. user says "remember 90 days for cleanup *for this session*") — **the per-conversation nudge**
4. Command-line flag (`--stale-days 30`)


Config is the durable floor set from the shell. Memory is a per-conversation nudge on top, intended to decay with the conversation. A memory entry written six months ago must not silently outlive a recent `atomic config set`. Practical consequence: when writing config-overriding memories, scope them to the current session ("for this session" / "for this task"), not "remember forever". Document this in the amended axiom 2.


## CLI surface


```
atomic config get <key>
atomic config set <key> <value>
atomic config unset <key>
atomic config list [--json]
atomic config path
```


- `<key>` is dotted: `output.intensity`, `forge.issues`.
- `set` validates against the schema (known keys + allowed values). Unknown keys rejected with a typo suggestion if close.
- `set` creates `~/.claude/.atomic/` and `config.toml` if missing. Atomic write (`os.Rename` from tmp).
- `list --json` is for shell integrations and `doctor`.
- `path` prints the resolved file path for editor integrations.


No `atomic config edit` initially — users can `$EDITOR $(atomic config path)`.


## How Claude sees the config


**File-based delivery, universal.** Hook-installed and hook-blocked (enterprise) audiences alike. Session-start hooks are not part of the config delivery path — the system must be usable without hooks (better with hooks, but not dependent on them).


**Mechanism.**


1. `atomic config set` writes `~/.claude/.atomic/config.toml` (atomic write via `os.Rename` from tmp).
2. Same call re-renders `~/.claude/.atomic/config.resolved.md` — a human-readable markdown snapshot of resolved values, generated from the TOML + built-in defaults.
3. Bundled `CLAUDE.md` carries a single `@-ref` line pointing at `~/.claude/.atomic/config.resolved.md`. The ref ships in the source artifact, so installed and bundled `CLAUDE.md` SHAs match — no divergence, no `.atomic-proposed` flow.
4. `claudeinstall` pre-creates an empty `config.resolved.md` on first install so the `@-ref` always resolves. Subsequent `atomic config set` calls overwrite it with rendered content.


**Why this works for both audiences.**


- Hook-installed users: Claude reads `CLAUDE.md` → `@-ref` resolves → config values reach context. Hook is free to deliver other things (reminders, future use cases).
- Enterprise / hook-blocked users: identical path. `@-ref` resolution is a file-read, not a hook execution; enterprise policy does not block it.


**Why not a sentinel block inside `CLAUDE.md`.** Considered and dropped. Mutating `CLAUDE.md` on every `atomic config set` collides with `claudeinstall`'s SHA-divergence guard and forces a managed-region parser. Separate file + one `@-ref` line solves it without either complication. Revisit only if a second managed region appears.


**Why not a template engine.** Considered and dropped. No concrete second use case for interpolation today. Adding a render pipeline before need is the trap; if a real second case appears (e.g. shared prompt fragments), spec it as its own feature.


## Touchpoints


| File | Change |
|------|--------|
| `atomic/internal/claudeinstall/install.go:81` | proposed path → `.atomic/proposed/CLAUDE.md` |
| `atomic/internal/claudeinstall/install.go:132,275-276` | backup path → `.atomic/backups/` |
| `atomic/internal/claudeinstall/install.go` | pre-create empty `~/.claude/.atomic/config.resolved.md` on first install so the bundled `@-ref` always resolves |
| `atomic/internal/config/` (new package) | load TOML, validate against schema, get/set/unset, atomic write, render `config.resolved.md` |
| `atomic/cmd/atomic/main.go` | `config` subcommand wiring |
| `atomic/internal/doctor/checks_install.go` | scan `.atomic/` paths |
| `atomic/internal/doctor/` (new check) | `config` category: TOML exists and parses, no unknown keys, `config.resolved.md` present and matches TOML render |
| `agents/atomic-claude-merger.md` | proposed-path reference |
| `commands/atomic-claude-merge.md` | proposed-path reference |
| `CLAUDE.md` (root, bundle source) | add single `@~/.claude/.atomic/config.resolved.md` line + mention `~/.claude/.atomic/` namespace |
| `docs/spec/atomic-doctor.md` | new check entry (category #9) + change-log entry |


## Axiom 2 amendment


Append to `.claude/docs/axioms.md` axiom 2:


> **Carve-out for shell-settable defaults.** Tunables that must be writable from a shell (CI scripts, `atomic config set`, dotfile-managed setup) live in `~/.claude/.atomic/config.toml`, not memory. Memory cannot be written non-interactively. Config is the durable floor; memory is a per-conversation nudge on top, intended to decay with the conversation. Memory entries overriding config should be scoped ("for this session", "for this task"), never "remember forever" — a stale memory must not silently outlive a recent `atomic config set`. Example: `output.intensity = "lite"` (config, durable) vs "use atomic ultra for this session" (memory, scoped).


## Open questions


- Should the config support per-project overrides via `.claude/.atomic/config.toml`? Argues against axiom-2 amendment scope; defer until a concrete case appears.
- Validation strictness on `set`: strict (reject unknown keys) or lenient (warn and write)? Strict prevents silent typos; lenient survives schema additions on older binaries. Recommend strict + helpful suggestion ("did you mean output.intensity?").
- Lockfile for `atomic config set` concurrent writes from multiple shells? Probably overkill; `os.Rename` atomic-write is sufficient for single-user case.


## Test plan


- Unit: `config` package — load, get, set, unset, atomic write, schema validation, unknown-key rejection, render of `config.resolved.md`.
- Unit: `doctor` config check — missing TOML (PASS, defaults), present + valid (PASS), present + unknown key (WARN), present + malformed TOML (FAIL), `config.resolved.md` out of sync with TOML (WARN + `--fix` re-renders).
- Integration: fresh install → `~/.claude/.atomic/config.resolved.md` exists and is empty; bundled `CLAUDE.md` contains the `@-ref` line; SHA-compare of installed vs bundled `CLAUDE.md` matches (no divergence).
- Integration: `atomic config set output.intensity lite` → `config.toml` updated, `config.resolved.md` re-rendered with the value, `CLAUDE.md` untouched.


## Rollout


1. Land `atomic/internal/config/` package + `atomic config` CLI surface. Schema starts with `output.intensity` only. `set` re-renders `config.resolved.md`.
2. Add `@~/.claude/.atomic/config.resolved.md` line to the bundled `CLAUDE.md`. Regenerate bundle.
3. Switch `claudeinstall` write paths to `.atomic/backups/` and `.atomic/proposed/CLAUDE.md`. Pre-create empty `config.resolved.md` on first install so the bundled `@-ref` resolves. Old paths are not read, not migrated, not deleted — user state is the user's to clean up.
4. Land `doctor` config check (category #9).
5. Expand schema iteratively per real need (forge host, issues tracker, etc.). Each addition: schema entry + renderer update + spec change-log entry + one steering site that reads it.

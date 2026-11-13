# `atomic update` post-apply doctor

## Goal

After a successful binary swap by `atomic update`: first, converge every enrolled harness target with the replacement binary's own embedded corpus and run pending migrations (default behavior; `--skip-claude-update` skips convergence, migrations still run); then automatically run `atomic doctor` (scoped to checks unaffected by the swap), surface FAILs only, and never block the update success path. One command updates everything; doctor verifies the converged state instead of flagging drift the user must fix by hand.

## Non-goals

- Running doctor on every `atomic update --check` invocation (check-only must stay cheap and side-effect-free).
- Auto-applying repairs (`--fix` semantics stay opt-in and explicit).
- Running signals or project-scoped checks (`atomic update` may execute outside any project's cwd).
- Replacing or wrapping the existing `atomic doctor` command surface — this is post-update auto-fire, not a new check pipeline.

## Success criteria

- [ ] After `atomic update` successfully replaces the binary, `doctor` runs with `--skip binary,signals` automatically.
- [ ] WARN and SKIP results are suppressed in default mode; only FAIL is printed.
- [ ] All-PASS post-update is silent (no extra output beyond the existing "updated to vX.Y.Z" line).
- [ ] `atomic update --no-doctor` flag fully disables the post-update doctor run.
- [ ] `update.run_doctor = false` in `~/.atomic/config.toml` disables the post-update doctor run.
- [ ] CLI flag overrides config (`--no-doctor` wins even if config says `true`).
- [ ] Doctor failures (FAIL findings or doctor-itself errors) never change the update's exit code — update success is preserved.
- [ ] Doctor's own internal error (exit 2 / panic) prints a single line "doctor self-check failed: <err>" and the update still exits 0.
- [ ] `--no-doctor` and `update.run_doctor` plus the auto-fire behavior all have unit-test coverage with the doctor invocation stubbed.
- [ ] Post-update README + install guide reflect the new behavior so users encountering FAIL lines after `atomic update` understand where they come from.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Add `update.run_doctor` to config schema; explicit-presence detection via raw map | `atomic/internal/config/config.go` + `config_test.go` | Define new `updateSection` struct with `RunDoctor bool \`toml:"run_doctor"\``; add `Update updateSection \`toml:"update"\`` field to `Config`; register `update.run_doctor` in `knownKeys` (so `update` auto-derives into `knownSections`); `Default()` returns `Config{..., Update: updateSection{RunDoctor: true}}`. **Raw map check at decode time distinguishes "absent → use default true" from "explicitly false"** (do not rely on bool zero-value); reuse existing rawMap pattern at `config.go:79`. Unknown-key warning still fires for any `update.<unknown>` |
| 2 | Render `update.run_doctor` in resolved config snapshot | `atomic/internal/config/render.go` + test | `config.resolved.md` includes new `## update` section with key + source (default/file); reference existing section ordering convention; do not introduce new ordering |
| 3 | Doctor adapter at orchestration layer | `atomic/cmd/atomic/main.go` (or thin helper in `atomic/internal/updatedoctor/`) | Adapter calls `doctor.Run(doctor.Opts{Skip: []int{3, 8}})` — signals=3, binary=8 indices per `atomic/internal/doctor/doctor.go:47` registry; partitions `[]doctor.Result` by severity caller-side; no import of `doctor` from `selfupdate` package (no cycle defense needed — they don't import each other today) |
| 4 | Wire post-`Update` invocation at main.go level (NOT inside selfupdate package) | `atomic/cmd/atomic/main.go runUpdate` | `runUpdate` orchestrates: `Lookup → Apply → adapter`; flag/config read at this layer; `selfupdate.Client` stays doctor-free; adapter return value discarded for exit-code purposes — update success preserved unconditionally |
| 5 | Filter doctor output: FAIL always, WARN unconditionally suppressed | adapter file from CP3 | Golden tests: mixed-result fixture → only FAIL lines printed; WARN/SKIP silent |
| 6 | Add `--no-doctor` flag to `atomic update` | `atomic/cmd/atomic/main.go runUpdate` | Flag parsed alongside existing `--check`, `--channel`; on `--check` flag is silently no-op (no apply happened); precedence rule: flag > config > default |
| 7 | Doctor error + panic handling | adapter file from CP3 | `doctor.Run` returning non-nil error → print one line "doctor self-check failed: <err>". Panic recovery requires wrapping the `doctor.Run` call in an **inner helper function with a deferred recover()** (Go's `recover` only catches panics in its own goroutine, inside a deferred function — recover at `runUpdate` top level would also unwind unrelated post-`Apply` cleanup). Helper shape: `func safeRunDoctor(run runDoctorFn) (results []doctor.Result, err error) { defer func() { if r := recover(); r != nil { err = fmt.Errorf("panic: %v", r) } }(); return run(doctor.Opts{Skip: []int{3, 8}}) }`. Both error and panic paths exit 0 |
| 8 | Document new behavior in install guide | `docs/guides/install.md` | Add a "Self-update" subsection documenting `atomic update [--check] [--channel] [--no-doctor]` and the `update.run_doctor` config key; explain that silent post-update output means healthy (no FAILs); explain when users might see FAIL lines. README is not edited — it has no existing CLI table; install guide is the canonical reference surface for binary flags |
| 9 | Append change-log entries to both affected specs | `docs/spec/atomic-doctor.md`, `docs/spec/atomic-state-and-config.md` | atomic-doctor: notes new auto-fire surface from update path; atomic-state-and-config: schema entry for `update.run_doctor` per the append-mostly rule |
| 10 | Signals refresh; confirm no bundle regen needed | `/refresh-wiki` | Signals reflect new flag + config key; spec does not touch `agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, or root `CLAUDE.md` — bundle parity unchanged |

## Architecture

```mermaid
flowchart TD
    A[atomic update] --> B{--check?}
    B -- yes --> C[print availability + exit]
    B -- no --> LK{acquire update lock}
    LK -- held less than 10min, no --force --> LR[refuse: name lock age; exit non-zero]
    LK -- acquired / stale takeover / --force --> D[fresh GitHub lookup]
    D -- not newer --> P[no swap: this binary is the selected generation]
    D -- newer --> SM{staged version+checksum match?}
    SM -- yes --> SW[swap from staged archive, no re-download]
    SM -- no --> DL[download archive + verify + swap]
    SW --> UP[stamp updated_at; clear lock]
    DL --> UP
    UP --> R{--skip-claude-update?}
    P --> R
    R -- no --> S[converge enrolled targets: re-exec replacement binary as `update --__target-converge` after a swap, in-process when no swap]
    S --> M[run pending migrations]
    R -- yes --> M
    M --> E{--no-doctor?}
    E -- yes --> Z[print 'updated to vX'; exit 0]
    E -- no --> F{config update.run_doctor?}
    F -- false --> Z
    F -- true --> G[adapter: doctor.Run skip=signals,binary]
    G --> H{any FAIL?}
    H -- no --> Z
    H -- yes --> I[print FAILs only; exit 0]
    G -- err/panic --> J[print 'doctor self-check failed'; exit 0]
```

Caption: the update lock and staged-swap decision sit ahead of target convergence and the doctor pass. `--force` bypasses only the lock-contention branch (`LK`) — it never weakens the checksum re-verify inside `SM`. Convergence and migrations run at the `runUpdate` orchestration layer, never inside the `selfupdate` package; after a swap they run in the replacement binary, and when nothing was swapped the already-selected binary runs them in-process. Doctor invocation is likewise at the orchestration layer. Update success exit is unconditional once the swap (staged or downloaded) succeeds.

## Update lock and staged swap

`atomic update` guards the swap with a lock recorded in `~/.atomic/state.json`: `updating=true` plus `update_started_at`. A second updater within 10 minutes of an active lock is refused with an error naming the lock's age; past 10 minutes the lock is considered abandoned and is taken over. `--force` bypasses the lock-contention check only — it stamps the lock unconditionally but never skips or weakens the checksum re-verify below. The lock clears and `updated_at` stamps on completion of any swap, staged or downloaded; on failure at any step the lock clears best-effort before the existing error path propagates.

Before deciding how to swap, `atomic update` always performs its own fresh GitHub lookup — it never trusts `state.json`'s `latest_version` alone for the swap decision. When the fresh lookup's version matches `state.json`'s staged record and the staged file's checksum re-verifies against the release's `checksums.txt`, the swap uses the staged archive directly with no re-download. Any version mismatch, checksum mismatch, or missing staged file falls back to the ordinary download-and-swap path.

The staged archive is produced out-of-band: a detached child, spawned at most once per hour by any `atomic` invocation (not `atomic update` alone) after stamping `last_check` in `state.json`, performs the GitHub lookup and — at most once per version, gated by config `update.stage` — downloads and checksum-verifies a release archive into `~/.cache/atomic/staged/`. See [`selfupdate-state.md`](./selfupdate-state.md) for the full state schema, spawn cadence, and staging gate.

## Post-swap target convergence contract

Runs between the binary swap and the post-update doctor, in `runUpdate` (`atomic/cmd/atomic/main.go`).

- **Default-on, no detection gate.** Anyone running `atomic update` is assumed to want the whole product current, so every enrolled target is converged unless `--skip-claude-update` is given. An unenrolled home is a no-op, never an error, and there is no managed-install detection: convergence is idempotent and safe on every input.
- **Mechanism**: convergence is the CP7A install engine (`install.Steps.ConvergeEnrolled`), auto-approved because running `atomic update` is the consent. After a swap, the running process still embeds the old corpus and must not publish, so it re-execs the replacement binary as `<exe> update --__target-converge` (forwarding `--skip-claude-update` and `--no-doctor`); the replacement re-acquires the lifecycle lock, recovers unresolved journals, and converges every enrolled target with its own generation. When nothing was swapped, this process already embeds the selected generation and converges in-process.
- **Adapter mutations.** Each adapter applies its own narrow native writes during convergence — for Claude, the inline SessionStart registration and the `outputStyle` seed — and records their ledger ownership, so a target uninstall later removes exactly the members Atomic wrote (see [`uninstall.md`](./uninstall.md)). The update path no longer re-execs `claude update` or passes `--no-hooks`.
- **Opt-out**: `--skip-claude-update` skips convergence entirely; migrations and doctor still run. No config key (add one if a real need appears).
- **Failure**: a convergence error warns on stderr with `run \`atomic harness repair\` manually` and never changes the update exit code; a blocked plan prints `<target> <status> <blocker>` for the operator. Doctor still runs afterwards and surfaces real breakage.
- **Ordering**: convergence strictly before doctor, so check 1 (install integrity) validates the converged state; migrations run between the two.

## Config schema addition

```toml
[update]
run_doctor = true     # default; set false to disable post-update doctor auto-fire
```

| Key | Type | Default | Valid values |
|-----|------|---------|--------------|
| `update.run_doctor` | bool | `true` | `true` \| `false` |

Resolved snapshot (`config.resolved.md`) gains:

```markdown
## update

- `run_doctor`: `true`  *(default)*
```

## Doctor invocation contract

Library call, not CLI. Adapter invokes:

```go
results, err := doctor.Run(doctor.Opts{Skip: []int{3, 8}})
// indices: signals=3, binary=8 — stable per atomic/internal/doctor/doctor.go:47 registry
```

- Skip `binary` (index 8): just replaced the binary; checking it via `os.Stat` + version-compare against ourselves is meaningless.
- Skip `signals` (index 3): `atomic update` may run from any cwd, including outside a git repo; signals needs project context.
- No `--json`, no exit-code translation: this is a library call returning `([]doctor.Result, error)` directly. The adapter partitions results by severity.
- Indices are used instead of names because `doctor.resolveCategories` (the name→index resolver) is unexported. If category indices ever shift, the adapter's compile-time assumption breaks loud — add a sanity test asserting `doctor.Categories()[3].Name == "signals"` and `[8].Name == "binary"`.

## Output rules

| Doctor result | `atomic update` output |
|---------------|------------------------|
| All PASS / SKIP | silent (only "updated to vX") |
| Any WARN (no FAIL) | silent — WARN suppressed unconditionally at post-update surface |
| Any FAIL | FAIL lines printed |
| Doctor error or panic | "doctor self-check failed: <err>" (single line) |

FAIL lines use the same format `atomic doctor` already emits (no reformatting), to keep the user's mental model consistent. WARN suppression is deliberate: post-`atomic update` is a noise-sensitive surface; users who want full output run `atomic doctor` directly.

## CLI surface change

`atomic update` flags table:

| Flag | Existing | Behavior |
|------|----------|----------|
| `--check` | yes | Unchanged; never triggers post-update doctor (no apply happened) |
| `--channel <name>` | yes | Unchanged |
| `--no-doctor` | yes | Disable post-update doctor for this invocation; overrides config |
| `--skip-claude-update` | yes | Skip the post-swap enrolled-target convergence; migrations and doctor still run |

No `--verbose` flag is added by this spec. Today's `atomic update` does not have one, and post-update doctor output is deliberately FAIL-only.

Precedence (highest first): `--no-doctor` flag > config `update.run_doctor = false` > default `true`.

## Failure-mode contract

Update exit code is determined only by the binary swap. Doctor's outcome cannot change it.

| Scenario | Update exit | Stdout |
|----------|-------------|--------|
| Apply OK, doctor PASS | 0 | "updated to vX.Y.Z" |
| Apply OK, doctor FAIL | 0 | "updated to vX.Y.Z" + FAIL lines |
| Apply OK, doctor returns non-nil error | 0 | "updated to vX.Y.Z" + "doctor self-check failed: \<err\>" |
| Apply OK, doctor panics | 0 | "updated to vX.Y.Z" + "doctor self-check failed: panic: \<msg\>" (recovered via defer) |
| Apply OK, `--no-doctor` | 0 | "updated to vX.Y.Z" |
| Apply OK, config disabled | 0 | "updated to vX.Y.Z" |
| Apply failed | non-zero | existing error path |
| `--check` only | existing | existing; doctor never invoked |

## Adapter design

Adapter lives at the orchestration layer (`atomic/cmd/atomic/main.go`, or a thin helper package `atomic/internal/updatedoctor/` if main.go grows unwieldy). It calls `doctor.Run` directly using the real types:

```go
// in runUpdate, after selfupdate.Apply succeeds:
if shouldRunPostUpdateDoctor(flags, cfg) {
    results, err := doctor.Run(doctor.Opts{Skip: []int{3, 8}})
    printPostUpdateDoctor(results, err)  // partitions by severity; FAIL-only
}
```

For testability, `runUpdate` accepts a function-typed dependency `runDoctor func(doctor.Opts) ([]doctor.Result, error)` defaulting to `doctor.Run`; tests inject a stub. No interface, no extra package — function value is enough.

`selfupdate` package does **not** import `doctor`. No import cycle exists today; the adapter sits one layer above both packages. This keeps `selfupdate` focused on binary fetch/swap and `doctor` free of update-flow assumptions.

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Doctor adds latency to `atomic update` user-perceived time | medium | Measure post-`Apply` time under representative `~/.claude`; document the number in the implementation commit. No specced ceiling — if measured > 750ms, file a follow-up. **Do NOT default-skip `manifest`** — that's the highest-signal check post-update (catches stale `~/.claude` after binary swap); skipping it defeats the primary motivation |
| Doctor self-error spam masks the update success message | low | Single-line warning, not full trace. No verbose mode added |
| Config key name `update.run_doctor` clashes with future `update.*` keys | low | Reserve `update` namespace in spec change-log; document intent |
| Flag `--no-doctor` collides with potential global doctor-disable flag | low | Document as `atomic update`-specific; never a top-level flag |
| Users mistake silent PASS for "doctor didn't run" | low | Behavior documented in install guide so users know silent = healthy |
| Stubbed runner in tests drifts from real `doctor.Run` signature | low | Function-typed dependency uses `doctor.Run`'s real signature; signature change forces compile error in `runUpdate` immediately |
| Skip indices `[3, 8]` drift if doctor registry reorders | medium | Sanity test in adapter asserts `doctor.Categories()[3].Name == "signals"` and `[8].Name == "binary"`; registry reorder fails the test |
| `update.run_doctor` bool zero-value (false) indistinguishable from "absent" | medium | Use raw-map presence check at decode time (existing pattern at `config.go:79`) — explicit-false vs absent both have different semantics; `Default()` sets `RunDoctor: true` |

## Change log

### 2026-09-20 — Post-swap convergence replaces the claude-update re-exec

**What changed:** The post-swap step is now target convergence. `atomic update` converges every enrolled harness target through the CP7A install engine (auto-approved because the update is the consent), then runs pending migrations, then the post-update doctor. After a swap the replacement binary is re-exec'd as `update --__target-converge` (forwarding `--skip-claude-update` and `--no-doctor`) so it converges with its own embedded generation; when nothing was swapped the already-selected binary converges in-process. Each adapter applies its own narrow native writes during convergence — for Claude the SessionStart registration and the `outputStyle` seed, recorded as ledger ownership. `--skip-claude-update` now skips convergence only; migrations and doctor still run. Goal, the Architecture flow and caption, the `## Artifact auto-refresh contract` section (now `## Post-swap target convergence contract`), and the CLI flags table were updated.

**Why:** `docs/spec/omp-plugin-compatibility.md` (CP7B) makes `atomic update` target-aware. The prior body described a `~/.claude` artifact refresh performed by re-execing `claude update`, plus a hook-preservation clause for `--no-hooks` and an already-current path that exited without converging — all retired by the CP7B implementation, so a fresh reader would have built the wrong path.

**Superseded:** Prior body re-exec'd `<exe> claude update --no-update-check`, appending `--no-hooks` when no session-start hook was registered, and an already-current update printed "up to date" and exited without converging any target.

### 2026-09-20 — Correction: the skip-convergence branch still passes migrations

**Correction:** The Architecture flow routed the `--skip-claude-update` branch straight from `R` to the `--no-doctor?` decision, bypassing `M[run pending migrations]`. The code diverges: `runUpdatePostSwap` calls `deps.migrate(home)` whenever a home is resolved, gating only convergence on `!skipTargets`, and this section's own prose already said migrations still run. The flow now passes both branches through `M`.

### 2026-08-09 — Update lock, staged fast-path swap, and detached background check

**What changed:** `atomic update`'s Architecture flow gains a lock-acquisition step (refuse a fresh lock less than 10 minutes old by naming its age, take over a stale one, `--force` bypasses the lock check only) and a staged-vs-download branch ahead of the existing artifact-refresh/doctor steps: a fresh GitHub lookup decides whether to swap from a pre-staged, checksum-re-verified archive with no re-download, or fall back to the existing `Apply` download flow. A new `## Update lock and staged swap` section documents the lock, the fast-path decision, and the detached child — spawned at most once per hour by any invoked verb, not `atomic update` alone — that performs the periodic GitHub lookup and once-per-version background staging. Everything after the swap (artifact refresh, post-update doctor) is unchanged.

**Why:** `docs/spec/selfupdate-state.md` moves the periodic version check off an in-process goroutine/cache-file pair and onto `~/.atomic/state.json`, so a later `atomic update` can swap from an already-verified staged binary instead of downloading from zero.

**Superseded:** The prior body described the swap as a single unconditional `selfupdate.Lookup + Apply` step with no lock and no staged fast path; the periodic check that fed the update-available banner lived outside this spec entirely, as an in-process `BackgroundCheck` goroutine writing to a `~/.cache/atomic/update.json` cache file (documented in `docs/spec/atomic-binary.md`).

### 2026-07-16 — User state root relocated to ~/.atomic

**What changed:** The `update.run_doctor = false` success criterion now reads `~/.atomic/config.toml` in place of `~/.claude/.atomic/config.toml`.

**Why:** `docs/spec/configurable-state-paths.md` (issue #150) relocates the user state root.

**Superseded:** Prior body named `~/.claude/.atomic/config.toml` as the config path.

### 2026-06-10 — Auto-refresh ~/.claude artifacts before doctor

**What changed:** after a successful binary swap, `atomic update` re-execs the new binary as `claude update --no-update-check` (appending `--no-hooks` when no session-start hook is registered, so the refresh never adds hooks the user didn't opt into) before firing the post-update doctor. New `--skip-claude-update` flag opts out. The out-of-sync nudge is gone — the refresh is the default. Goal, architecture diagram, and CLI flags table updated; new `## Artifact auto-refresh contract` section added. (Same-day consolidation: an intermediate `--binary-only` flag plus managed-install detection gate — CLAUDE.md/hook evidence — was built and replaced by this assume-update design before release; detection was dropped because `claude update` is idempotent and safe on every input.)

**Why:** the post-update doctor flagged `~/.claude` drift the user then had to clear by running `atomic claude update` manually on every release. With deterministic `<atomic>` block replacement landed (install-workflow spec, 2026-06-10), the artifact refresh is safe to run unattended — so update does everything and doctor verifies the end state.

**Superseded:** prior contract: `atomic update` swapped the binary only; artifact refresh was always a manual follow-up step, nudged via the out-of-sync note.

## Implementation log

### shipped — 2026-05-22

Built across 4 iterations of /subagent-implementation on branch `update-doctor`. Commits (chronological):

- `dd33c70` — CP-1 + CP-2: `feat(config): add update.run_doctor key`
- `59620d2` — CP-3 + CP-4 + CP-5 + CP-6 + CP-7 + iter-1 cleanup: `feat(update): post-update doctor auto-fire`
- `6d5539b` — CP-8 + CP-9: `docs(update): self-update guide + spec change-logs`
- `257c997` — CP-10: `chore(signals): refresh after update-doctor`
- `3035318` — finalize: `docs(followups): promote update-doctor F-1 + add implementation log`
- `bdfe72c` — finalize: `docs(claude): document atomic update --no-doctor + run_doctor`

**Out-of-scope work performed during this build:**

- Exported `doctor.FormatResultLine(r Result) string` so the adapter and `FormatHuman` share one format. Driven by spec § "Output rules" requiring identical formatting between `atomic doctor` and post-update FAIL output.
- Dropped the `UpdateRunDoctorExplicit` field originally added in iter 1. User mid-iter clarification ("don't worry about backwards compatibility") + reviewer signal that `Default()` seeding before TOML decode already gives correct "absent → true, explicit-false → false" semantics → field was dead surface.

**Unforeseens — surprises that emerged during implementation:**

- Iter 1's `UpdateRunDoctorExplicit` field was over-engineered per spec hint, but the downstream adapter (CP-3) only needed `cfg.Update.RunDoctor`. The spec's raw-map presence-check guidance was correct but addressed a requirement that never materialized in CP-3..CP-7. Folded the cleanup into iter 2.
- Iter 2's first reviewer pass caught a FAIL format divergence (`%s` vs `%-25s`) between the adapter and `atomic doctor`. Spec § "Output rules" required identity; fix required a shared formatter helper. Surgeon pass resolved.

**Deferred items still open:**

- ~~`atomic-update-doctor-F-1` — `render.go` bool default heuristic piggybacks on `cfg.Output.Intensity == ""` sentinel.~~ **Resolved 2026-06-07** when `output.intensity` was removed: the sentinel now keys off `cfg.Output.Signals.MaxDepth <= 0`. See `docs/spec/atomic-state-and-config.md` change log (2026-06-07).

**Squashed onto `main` as `bf543099ed86fce523740c483e10f786227242c0` — 2026-05-22.** Per-iteration SHAs above are historical (unreachable post-squash).

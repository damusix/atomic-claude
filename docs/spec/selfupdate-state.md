# Self-update state, check cadence, and staged downloads


## Goal


Every `atomic` invocation reads update state from `~/.atomic/state.json` and performs zero network I/O itself. A detached child — spawned at most once per hour — owns the GitHub lookup, banners render from on-disk state alone, and, at most once per version, the same child downloads and checksum-verifies a staged binary so a later `atomic update` swaps instantly instead of downloading from zero.


## Non-goals


- No auto-apply — the binary is never swapped without an explicit `atomic update`.
- No repo-scoped `update.check` / `update.stage` override in `.claude/atomic.toml` — user config (`~/.atomic/config.toml`) only.
- No prerelease-channel behavior change — staging follows the same channel as the check (`stable` by default).
- No formal migration verb for the legacy `~/.cache/atomic/update.json` cache file — it is deleted opportunistically on the next state write; nothing reads it afterward.
- No staging of the `~/.claude` artifact bundle — `atomic claude update` is untouched; only the binary is staged.
- No staging side effect on a manually-typed `atomic update --check` — background staging is exclusive to the auto-spawned detached child. A user running `--check` interactively gets the same fast lookup-and-report behavior as today, with no download latency.


## Success criteria


- [ ] `~/.atomic/state.json` holds an `update` block with `last_check`, `updating`, `update_started_at`, `updated_at`, `last_notified`, `latest_version`, `stage_attempted_for`, `last_result`, and staged `{version, path, sha256}`; reads and writes are atomic (temp+rename).
- [ ] No `atomic` invocation performs an HTTP request itself; the update-available banner renders solely from `state.json`'s on-disk `latest_version` and `last_notified`.
- [ ] A detached child is spawned at most once per hour, and only when: the invoked verb is not `update`, `--no-update-check` is absent, and config `update.check` is `true`; `last_check` is stamped synchronously before the spawn.
- [ ] The parent process exits without waiting on the spawned child; the pre-`Execute` `BackgroundCheck` goroutine setup and the post-`Execute` 100ms banner `select` are removed from `main.go`.
- [ ] The detached child performs the GitHub lookup and writes `latest_version` and `last_result` to state regardless of outcome (success or failure).
- [ ] Background staging (download + SHA256 verify into `~/.cache/atomic/staged/`) happens for a given version at most once, ever, gated by `stage_attempted_for != latest_version` and `update.stage == true`; a failed attempt is recorded in `last_result` and is never retried automatically.
- [ ] A manually-invoked `atomic update --check` reports availability exactly as today (same exit-code contract) and never triggers background staging.
- [ ] `atomic update` performs a fresh GitHub lookup before swapping; when the staged binary's recorded version matches that fresh lookup and its checksum re-verifies, the swap uses the staged file without re-downloading the archive; any version mismatch, checksum mismatch, or missing staged file falls back to the existing `Apply` download flow.
- [ ] `updating=true` + `update_started_at` guard both staging and swap; a second updater within 10 minutes of an active lock is refused with an error naming the lock's age; older than 10 minutes, the lock is considered abandoned and may be taken over; `--force` bypasses the lock check only (never the checksum verification).
- [ ] The lock is cleared and `updated_at` is stamped on completion of any swap (staged or downloaded).
- [ ] `update.check` and `update.stage` (bool, default `true`) are added to the config schema, validated, settable/unsettable, and rendered in `config.resolved.md`; no repo-scoped equivalent exists.
- [ ] `atomic doctor` category 9 (config) surfaces a chronic background-check failure (non-empty `last_result` error) as WARN.
- [ ] `docs/spec/atomic-update-doctor.md`, `docs/spec/atomic-state-and-config.md`, and `docs/guides/install.md` describe the state file, the staged fast-path swap, and the new `--force` flag.
- [ ] `atomic update --help` / `cliusage.go` document the new `--force` flag.


## Approach


Stamp-before-act state gates plus a single detached child that performs both the GitHub check and once-only background staging (design Approach B) — see `docs/design/selfupdate-state.md`.


## Change tree


```
atomic/internal/config/
├── config.go .................. M  (updateSection: Check/Stage bool fields; knownKeys, Default, Load, Validate, Set, Unset)
├── config_test.go ............. M  (update.check / update.stage coverage)
├── render.go ................... M  (Resolved: update.check, update.stage)
├── render_test.go .............. M
├── paths.go ..................... M  (state.json path helper)
└── paths_test.go ................ M

atomic/internal/selfupdate/
├── state.go ..................... A  (state.json types + atomic read/write)
├── state_test.go ................ A
├── cache.go ...................... M  (CacheEntry/ReadCache/WriteCache removed; legacy path helper retained)
├── selfupdate.go .................. M  (ShouldBanner/MaybeBanner/BackgroundCheck removed; version-compare + staging + re-verify helpers added)
└── selfupdate_test.go ............. M  (superseded cache/banner/background tests removed; staging + lock tests added)

atomic/cmd/atomic/
├── main.go ........................ M  (parent fast path rewrite; runUpdate: check-branch state write + staging, apply-branch lock + staged swap + --force)
└── main_test.go .................... M

atomic/internal/cliusage/
├── cliusage.go ..................... M  (update verb: --force)
└── cliusage_test.go ................ M

atomic/internal/doctor/
├── checks_config.go ................ M  (surface chronic last_result failure)
└── checks_config_test.go ........... M

templates/commands/
└── atomic-help.md ................... M  (cli topic: --force)
commands/
└── atomic-help.md ................... M  (rendered)

docs/spec/
├── atomic-update-doctor.md ......... M  (staged fast-path branch; change-log entry)
└── atomic-state-and-config.md ...... M  (state.json section; update.check/update.stage keys; Layout tree: drop the stale reserved `cache/` line, add `~/.cache/atomic/staged/`; change-log entry)

docs/guides/
└── install.md ....................... M  (Updating section: --force, background staging, state.json)
```


## Outline


atomic/internal/config/config.go
  updateSection — gains Check and Stage bool fields alongside the existing RunDoctor
  Default — backfills Check and Stage to true
  Load — raw-map explicit-presence detection for update.check / update.stage, mirroring the existing run_doctor pattern
  Set / Unset — update.check, update.stage cases
  knownKeys — update.check, update.stage entries

atomic/internal/config/render.go
  Resolved — update.check, update.stage entries

atomic/internal/config/paths.go
  StatePath — path to ~/.atomic/state.json

atomic/internal/selfupdate/state.go
  State — top-level state.json shape
  UpdateState — last_check, updating, update_started_at, updated_at, last_notified, latest_version, stage_attempted_for, last_result, staged
  StagedInfo — version, path, sha256
  LoadState — read with zero-value fallback on missing or corrupt file
  WriteState — atomic temp+rename write; opportunistic removal of the legacy cache file

atomic/internal/selfupdate/cache.go
  DefaultCachePath — retained; now used only to locate the legacy file for opportunistic cleanup

atomic/internal/selfupdate/selfupdate.go
  version-compare helper — exported wrapper enabling the parent's state-only banner decision
  staging helper — downloads and checksum-verifies a release asset into the staging directory without swapping
  staged re-verify helper — re-checks a staged binary's checksum against a fresh release's checksums.txt

atomic/cmd/atomic/main.go
  main — parent fast path: state read, banner render, stamp-before-spawn gate, detached spawn (replaces the BackgroundCheck goroutine setup and the post-Execute select)
  background spawn — setsid-detached launch of `atomic update --check` carrying a background-invocation marker (internal only, not a public flag)
  runUpdate
    check branch — writes latest_version/last_result to state; when carrying the background marker, runs the once-only staging gate (lock-aware)
    apply branch — lock acquire/refuse/takeover, --force bypass, fresh lookup, staged-match-and-checksum fast path, fallback to the existing Apply download flow, lock release + updated_at stamp on completion

atomic/internal/cliusage/cliusage.go
  update entry — Flags gains --force

atomic/internal/doctor/checks_config.go
  RunCheckConfigWith — surfaces a chronic last_result failure as an additional WARN finding

docs/spec/atomic-update-doctor.md
  Architecture — staged fast-path branch added to the flow diagram
  Change log — dated entry

docs/spec/atomic-state-and-config.md
  Layout — state.json entry; remove the stale reserved `cache/  # reserved (selfupdate version-check, future)` line and document the real staging path, `~/.cache/atomic/staged/`
  Schema — update.check, update.stage keys
  Change log — dated entry

docs/guides/install.md
  Updating — --force, background staging behavior, state.json mention


## Flows


Flow: parent fast path (every invocation)
1. `main()` runs the existing pre-scans (`--no-update-check`, `--version`, `--repo`) unchanged.
2. `main()` reads `~/.atomic/state.json` (best-effort; a missing or corrupt file yields a zero-value state and never blocks the invoked verb).
3. If state's `latest_version` is newer than the running version AND (`last_notified` is zero or older than 24h): print the banner, stamp `last_notified` to now, write state.
4. If the invoked verb is not `update`, AND `--no-update-check` is absent, AND config `update.check` is `true`, AND `last_check` is zero or older than 1h: stamp `last_check` to now, write state, spawn a detached child running `atomic update --check` with the background-invocation marker (setsid, streams nil, process released — no wait).
5. Dispatch the requested verb via Cobra, unchanged.
6. Exit. The parent never joins or waits on the spawned child.

Flow: detached child check + stage
1. The child re-enters `main()` as `atomic update --check <background marker>`. Because its own invoked verb is `update`, step 4 of the parent fast path above does not fire again — the child never spawns a further child.
2. `runUpdate`'s check branch performs the GitHub lookup (existing `Client.Lookup`/`Check`, `stable` channel).
3. The child writes `latest_version` and `last_result` to state, regardless of outcome.
4. If the lookup succeeded AND the release is newer than the running version AND config `update.stage` is `true` AND `latest_version != stage_attempted_for`:
   a. Attempt to acquire the update lock (`updating=true`, `update_started_at=now`). If the lock is already held by another process, skip staging this cycle without stamping `stage_attempted_for` — the once-only budget is spent only on a real download attempt, not on lock contention.
   b. Having acquired the lock: stamp `stage_attempted_for = latest_version` before downloading.
   c. Download the release archive and verify its SHA256 against `checksums.txt`; on success, record `{version, path, sha256}` under staged in state; on failure, record the failure in `last_result` (never auto-retried for this version).
   d. Release the lock.
5. A manually-typed `atomic update --check` (no background marker) runs steps 2–3 only — same lookup, same reporting to state where useful for banner purposes — but never performs step 4; it reports and exits exactly as today.

Flow: `atomic update` staged fast-path swap
1. User runs `atomic update` (no `--check`).
2. Acquire the update lock: if `updating=true` and `update_started_at` is younger than 10 minutes, refuse with an error naming the lock's age; if older than 10 minutes, the lock is considered abandoned and is taken over; either way, once acquired, stamp `updating=true`, `update_started_at=now`.
3. Perform a fresh GitHub lookup (never trust `state.json`'s `latest_version` alone for the swap decision).
4. If the fresh lookup is not newer than the running version: report up to date, clear the lock, exit success (existing behavior).
5. If newer: compare state's staged info to the fresh lookup. If `staged.version` matches AND the staged file's checksum re-verifies against the release's `checksums.txt`: swap directly from the staged file (no archive re-download). If the version differs, the checksum fails, or the staged file is missing or unreadable: discard the staged record and fall back to the existing `Apply` download-and-swap flow.
6. On a successful swap (staged or downloaded path): stamp `updated_at=now`, clear `updating` and the staged record, write state. Continue into the existing post-swap steps (artifact refresh, migrations, post-update doctor) unchanged.
7. On failure at any step: clear the lock best-effort, propagate the existing error exit path.

Flow: `--force`
1. User runs `atomic update --force`.
2. Step 2 of the staged-swap flow above is bypassed entirely regardless of the current lock's age or presence: `updating=true`/`update_started_at=now` is stamped unconditionally.
3. Every remaining step (fresh lookup, staged-match-and-checksum check, swap, lock clear) proceeds identically to the staged-swap flow. `--force` overrides lock contention only — it never skips or weakens checksum verification.


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Add `update.check` / `update.stage` config keys | `atomic/internal/config/config.go`, `config_test.go`, `render.go`, `render_test.go` | atomic-implementer (mode: surgical) | ~4 | `go test ./atomic/internal/config/...` — explicit-presence backfill, unknown-value rejection, `config.resolved.md` renders both keys |
| 2 | State file primitive: `~/.atomic/state.json` types + atomic read/write | `atomic/internal/selfupdate/state.go`, `state_test.go`, `atomic/internal/config/paths.go`, `paths_test.go` | atomic-implementer (mode: feature) | ~4 | `go test ./atomic/internal/selfupdate/... ./atomic/internal/config/...` — round-trip write→read, missing/corrupt file yields zero-value state (never errors), temp+rename leaves no tmp residue |
| 3 | Parent fast path: state-driven banner + stamp-before-spawn detached child; remove goroutine/select and the now-superseded cache/banner surface | `atomic/cmd/atomic/main.go`, `main_test.go`, `atomic/internal/selfupdate/selfupdate.go`, `cache.go`, `selfupdate_test.go` | atomic-implementer (mode: feature) | ~5 | `go test ./atomic/cmd/atomic/... ./atomic/internal/selfupdate/...` — spawn fires only when stale + enabled + verb-not-update + flag-absent; the child's own `update` invocation never re-spawns; banner prints from state only, at most once per 24h; `CacheEntry`/`ReadCache`/`WriteCache`/`MaybeBanner`/`ShouldBanner`/`BackgroundCheck` no longer exist |
| 4 | Detached child: GitHub lookup writes `last_result`/`latest_version`; once-only background staging | `atomic/cmd/atomic/main.go` (`runUpdate` check branch), `main_test.go`, `atomic/internal/selfupdate/selfupdate.go`, `selfupdate_test.go` | atomic-implementer (mode: feature) | ~4 | `go test ./atomic/cmd/atomic/... ./atomic/internal/selfupdate/...` — once-only gate holds across repeated checks; failed stage recorded, never auto-retried; lock contention skips staging without consuming the once-only budget; a manual (non-background) `--check` never stages |
| 5 | Update lock + staged fast-path swap in `runUpdate`'s apply branch, `--force` | `atomic/cmd/atomic/main.go`, `main_test.go`, `atomic/internal/selfupdate/selfupdate.go`, `selfupdate_test.go` | atomic-implementer (mode: feature) | ~4 | `go test ./atomic/cmd/atomic/...` — stale-lock takeover, fresh-lock refusal names the age, `--force` bypasses the lock only, staged-match-and-checksum swap skips download, any mismatch/missing falls back to `Apply`, lock cleared + `updated_at` stamped on success |
| 6 | CLI surface: `--force` flag + docs surface | `atomic/internal/cliusage/cliusage.go`, `cliusage_test.go`, `templates/commands/atomic-help.md`, `commands/atomic-help.md` | atomic-implementer (mode: surgical) | ~4 | `go test ./atomic/internal/cliusage/...`; `atomic validate artifacts` reports no unknown-flag citation |
| 7 | Doctor: surface chronic background-check failure | `atomic/internal/doctor/checks_config.go`, `checks_config_test.go` | atomic-implementer (mode: surgical) | ~2 | `go test ./atomic/internal/doctor/...` — non-empty `last_result` error surfaces as WARN; healthy state stays PASS |
| 8 | Spec + guide amendments, incl. correcting the stale `~/.atomic/cache/` placeholder | `docs/spec/atomic-update-doctor.md`, `docs/spec/atomic-state-and-config.md`, `docs/guides/install.md` | atomic-implementer (mode: surgical) | ~3 | `atomic validate spec docs/spec/atomic-update-doctor.md docs/spec/atomic-state-and-config.md` passes; grep confirms no remaining body mention of `~/.cache/atomic/update.json` as the live mechanism; grep confirms `atomic-state-and-config.md`'s Layout tree no longer lists `cache/` under `~/.atomic/` and lists `~/.cache/atomic/staged/` instead |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Detached child re-entering `main()` as `atomic update --check` recurses into spawning a grandchild if the verb-exclusion guard is dropped | low | CP3's tests assert the child's own invocation never re-spawns; the guard is exercised directly, not incidentally |
| Ambiguity between a manually-typed `--check` and the auto-spawned one leaks staging into interactive use, surprising users with download latency on a "just check" call | med | an internal-only background-invocation marker gates staging; CP4 tests both invocation shapes explicitly |
| `stage_attempted_for` gets stamped by an attempt that never actually downloads (lock held by a concurrent swap), silently burning the once-only budget for that version | med | staging skips, without stamping, when the lock is already held; only a real download attempt consumes the once-only budget |
| Stale-lock threshold (10 min) too short on a slow connection, causing a legitimate in-progress swap to be taken over mid-download | low | `--force` exists as an explicit escape hatch; 10 minutes is generous for a binary this size — revisit if real-world timing disagrees |
| Concurrent `state.json` writers (parent stamping `last_check`, child reporting outcome) lose one field update on a race | low | accepted per design — temp+rename is atomic per write; a lost field self-heals on the next hourly cycle |
| Staged binaries under `~/.cache/atomic/staged/` accumulate if never applied | low | cache semantics — safe to delete anytime; `state.json` remains the sole authority on what is currently staged |


## Change log

<!-- Populated on first amendment after the spec is approved. Do not log drafting/refinement turns. -->


## Implementation log

### unreleased (branch selfupdate-state) — 2026-08-09

Built across 9 iterations of /subagent-implementation. Commits (chronological):

- `6231ef9` — design + spec committed (pre-loop, branch base `42a875a` on origin/next)
- `634b164` — CP-1 update.check / update.stage config keys
- `a3e272f` — CP-2 state.json primitive (LoadState never errors; WriteState temp+rename + legacy cache cleanup)
- `ac0ccf2` — CP-3 parent fast path: state-driven banner, stamp-before-spawn detached child, BackgroundCheck/select/cache surface removed
- `b9115fc` — CP-4 once-only background staging per version (archive, checksum-verified); F-1/F-2 closed
- `7e616c8` — CP-5 staged fast-path swap, lock refuse/takeover/--force, owner-fenced ReleaseLock; Client.Update removed
- `edcc623` — CP-6 cliusage --force + help router + bundle regen
- `30ea7f2` — CP-7 doctor category 9 WARN on chronic last_result failure
- `93914df` — CP-8 atomic-update-doctor / atomic-state-and-config / install.md amendments
- `579398e` — F-3 comment line-number citations dropped (final triage, fix-now)

**Out-of-scope work performed during this build:**

- Cobra help-metadata `--force` registration in main.go (CP-6) — required by the cliusage↔Cobra parity golden test, not named in the Change tree.

**Unforeseens — surprises that emerged during implementation:**

- CP-5 review found a lock-ownership race (background stager's blind release could clear a foreground takeover's lock) — fixed with `update_started_at` value-fencing in `ReleaseLock`, race pinned by a real interleaving test.
- Staged artifact is the release archive, not the extracted binary — its SHA256 equals the `checksums.txt` value, so swap-time re-verify needs no format translation.
- `Client.Update` became dead code once `runUpdateApply` landed — deleted rather than left as a lock-bypassing trap.
- Staging performs one extra `Lookup` inside the child (Check doesn't expose Assets) — accepted: fires at most once per version.

**Deferred items still open:**

- none — F-1, F-2 (iter 4) and F-3 (final triage) all fixed in-loop.

**Squashed to a single branch commit — 2026-08-09.** Per-iteration SHAs above are historical (unreachable from any branch); the branch was also rebased onto the recreated `next` before squashing.

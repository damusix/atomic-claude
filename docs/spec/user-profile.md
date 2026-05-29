# User profile


## Goal

A global, auto-updated identity file at `~/.claude/.atomic/profile.md` that Claude reads in every session and writes to opportunistically, closing the gap between user-written global context and Claude-written per-project memory.


## Non-goals

- No interactive install wizard. No mid-conversation prompts to "update your profile."
- Not a replacement for `~/.claude/CLAUDE.md`. CLAUDE.md is user-written instructions; profile is Claude-observed facts.
- Not encrypted, redacted, or filtered. Privacy is the user's responsibility.
- Not per-project. Project-tinted preferences stay in existing auto memory.
- Not time-tracked. No `last_observed` fields, no staleness clock, no expiry.
- Not a policy engine or enforcement hook.
- No upstream Claude Code changes required or implied.


## Success criteria

- [ ] `~/.claude/.atomic/profile.md` is created at `atomic claude install` (idempotent — no-op if already present).
- [ ] Install populates `## Environment` with deterministic captures: `git config --global user.name`, `git config --global user.email`, `runtime.GOOS`, `runtime.GOARCH`, `runtime.NumCPU()`.
- [ ] `@~/.claude/.atomic/profile.md` appears in the atomic-owned block of `~/.claude/CLAUDE.md` (the installed copy), adjacent to the existing `@~/.claude/.atomic/config.resolved.md` ref.
- [ ] `~/.claude/CLAUDE.md` contains the verbatim routing instruction (see § Routing contract) inside the `<atomic>` block.
- [ ] Install prints the nudge line `Profile created at ~/.claude/.atomic/profile.md. Mention things about yourself naturally; Claude will fill it in. Run /atomic-improve to review drift.` to stdout **on first install only** (when step 1 actually creates the file). Suppressed when step 1 is idempotent no-op.
- [ ] `atomic claude uninstall` preserves `profile.md` (does not delete it, does not restore a pre-install version — none exists).
- [ ] `atomic doctor` reports WARN when `@~/.claude/.atomic/profile.md` is absent from any of `~/.claude/CLAUDE.md`, `~/.claude/claude.local.md`, `~/.claude/CLAUDE.local.md`.
- [ ] `/atomic-improve` discovery brief catalogs `profile.md`; history brief includes a **profile drift** finding category.
- [ ] Existing tests pass after all checkpoints land (`go test ./...` from `atomic/`).
- [ ] `make render && git diff --exit-code` clean after checkpoint 3.
- [ ] `make -C atomic bundle && git diff --exit-code` clean after checkpoint 2 and checkpoint 3.


## Approaches

| # | Approach | Sketch | Cost | Risk |
|---|----------|--------|------|------|
| A | New file under `~/.claude/.atomic/`, install-generated stub, opportunistic write, `/atomic-improve` review | Mirrors `config.resolved.md` pattern; no bundle changes; clean uninstall story; routing rule is one CLAUDE.md edit | Low | Routing instruction wording is load-bearing; wrong wording → facts go to wrong place |
| B | Bundle a template `profile.md` shipped with the binary, modified per-user | Discoverable from bundle; consistent shape | High | Bundle artifacts are read-only contracts that update — user content fights `atomic claude update` |
| C | Write directly into `~/.claude/CLAUDE.md` | Zero new surfaces | Low | CLAUDE.md is a user-written contract; mixing Claude-observed facts into it breaks the install/update boundary |
| D | Patch upstream Claude Code to add a global auto-memory tier | Fixes the gap at root | Very high | Out of our control |
| E | First-session interactive interview at install | Rich content day one | Medium | Hostile UX; forced answers are worse than observed facts |


## Recommendation

**Approach A.** Precedent: `config.resolved.md` — install-time idempotent stub under `~/.claude/.atomic/`, @-ref'd from the installed `~/.claude/CLAUDE.md`, never bundled, never overwritten on update. Surface map confirms the insertion points: `atomic/internal/claudeinstall/install.go` line 112+ (parallel to `ensureResolvedConfigStub`), `atomic/internal/config/paths.go` line 39+ (parallel to `ResolvedPath`), and `CLAUDE.md` line 5 for the @-ref. No new artifact kinds; no bundle-parity work beyond the CLAUDE.md edit.


## Schema contract

Plain markdown. Six pre-defined sections. No timestamps. Section order is fixed.

```
# User profile

## Identity
<stable>
- Name: ...
- Location: ...
- Native language: ...
</stable>

## Work
<volatile>
- Employer: ...
- Role: ...
- Team: ...
</volatile>

## Active projects
<volatile>
- ...
</volatile>

## Interests
<stable>
- ...
- Communication style: ...
</stable>

## People mentioned
<volatile>
- Alice (coworker) — owns billing service
</volatile>

## Environment
<deterministic>
- Git user.name: ...
- Git user.email: ...
- OS: ...
- Arch: ...
- CPU count: ...
</deterministic>
```

**XML volatility tags.**

| Tag | Meaning | Drift-detection weight |
|-----|---------|------------------------|
| `<stable>` | Rarely changes — Identity, Interests | Low: contradictions need strong signal |
| `<volatile>` | Changes routinely — Work, Active projects, People | High: contradictions surface early |
| `<deterministic>` | Captured from env at install, not conversation | None: `/atomic-improve` does not flag these |

**Append contract.**

1. Claude appends new facts to the matching existing section. Never creates new section names. Never deletes existing facts.
2. If a new observed fact contradicts an existing one, Claude appends the new fact below the old one without removing the old one. Both lines are retained as history.
3. Contradiction detection is deferred to `/atomic-improve` (profile drift category), not resolved inline.
4. Claude does not write to `<deterministic>` sections. Those are populated at install time only.
5. If no matching section exists (malformed file), Claude appends to the bottom under the closest matching heading or, if none, does not write.

These rules give Claude a deterministic answer for every write decision without an LLM judgment call at write time.

**Communication style preference** (e.g. terse, verbose, no emojis) is a personal fact that follows the user across all projects. It belongs in `## Interests` under the `<stable>` tag. This reconciles the design's routing table entry ("Communication style preferences → profile") with the six-section schema — no new section is created.


## Routing contract

The following verbatim text is inserted into `~/.claude/CLAUDE.md` inside the `<atomic>` block, after the `@~/.claude/.atomic/profile.md` ref line. This exact wording is the contract — paraphrasing it in the spec would create ambiguity between spec and installed artifact.

```
## User profile

@~/.claude/.atomic/profile.md

Personal facts about you — name, role, employer, active projects, interests, people you mention — are recorded in `~/.claude/.atomic/profile.md`. Claude reads this file in every session and appends new facts as they surface naturally in conversation. Facts that apply across all projects (identity, work, relationships) go here. Facts specific to one repo's conventions go to that project's auto memory instead. Rule of thumb: if the fact would still be true in a different repo, it belongs in profile.
```

The `@~/.claude/.atomic/profile.md` ref on its own line causes Claude Code to load the file as context. The paragraph below it is the routing instruction Claude uses to decide which surface captures a given fact.

This text lives in `CLAUDE.md` at the repo root (the bundle source). It is emitted into `~/.claude/CLAUDE.md` by `atomic claude install` via the standard CLAUDE.md write path. `atomic claude update` overwrites the atomic-owned block, so the routing instruction must be part of the source `CLAUDE.md` — it cannot be written only at install time.


## Install contract

Steps run in order during `atomic claude install`, after `ensureResolvedConfigStub`:

| Step | What happens | Idempotent? |
|------|-------------|-------------|
| 1 | Create `~/.claude/.atomic/profile.md` if absent using the schema template above with all fact fields empty | Yes — no-op if file exists |
| 2 | Populate `## Environment` / `<deterministic>` block: run `git config --global user.name`, `git config --global user.email`; read `runtime.GOOS`, `runtime.GOARCH`, `runtime.NumCPU()` | Yes — if file already contains deterministic data, skip write |
| 3 | `@~/.claude/.atomic/profile.md` ref and routing paragraph are already in `CLAUDE.md` source; they land in `~/.claude/CLAUDE.md` via the standard CLAUDE.md install write | Yes — idempotent via CLAUDE.md write path |
| 4 | Print to stdout: `Profile created at ~/.claude/.atomic/profile.md. Mention things about yourself naturally; Claude will fill it in. Run /atomic-improve to review drift.` | No — always prints on first-install invocation; suppressed on subsequent invocations where step 1 is a no-op |

**Bootstrap nudge** goes to stdout (not a log file). Rationale: install already prints other stdout messages; one line here is consistent and more discoverable than a silent log. The line is suppressed when the file already exists (step 1 no-op) to avoid noise on `atomic claude update`.

**Env capture failures** (git not installed, no global config set): populate with empty string for that field. Do not abort install. Partial capture is acceptable.

**New path constant** needed in `atomic/internal/config/paths.go`: a function parallel to `ResolvedPath` that returns the profile.md absolute path given `claudeHome`. Used by install, uninstall, and doctor.


## /atomic-improve integration

Two additions to `templates/commands/atomic-improve.md`:

**1. Discovery brief** (catalog section): extend to include `~/.claude/.atomic/profile.md` in the file catalog. No special handling — treated like any other personal config file.

**2. History brief** (detection categories): extend with a **profile drift** finding category.

Profile drift finding format:

```
[profile drift] "<existing fact>" may be stale — you mentioned "<new observed fact>" in this session.
Confidence: <low|medium|high>
Options: Accept new / Modify / Keep both / Skip
```

Detection trigger: during `/atomic-improve` history mining, if the current session's conversation contains a statement that contradicts or supersedes a fact in profile.md, surface it as a profile drift finding. `/atomic-improve` does not auto-write to profile.md — it presents findings and the user accepts/modifies/skips per-item (axiom 3: destructive ops require explicit confirm; overwriting a recorded identity fact qualifies).

Cap: profile drift findings count against the existing 15-finding-per-run cap. No separate cap.

`<deterministic>` section facts are excluded from drift detection.


## Uninstall contract

`atomic claude uninstall` (spec: `docs/spec/uninstall.md`) **preserves `profile.md`**.

Rationale: profile.md is user data generated after install — it has no pre-install counterpart and is not a bundle artifact. The uninstall plan must not include it in either the "restore" or "delete" buckets.

Implementation: `BuildUninstallPlan` in `atomic/internal/claudeinstall/uninstall.go` must explicitly exclude `~/.claude/.atomic/profile.md` from the deletion list. Since profile.md is not in the pre-install snapshot (`manifest.json` only records files atomic touches during install, and profile.md is created by install, not copied from the bundle), it will not appear in the manifest. The existing logic of "delete files with `existed=false`" would not touch it unless profile.md were incorrectly included in the manifest. Verify that `snapshot.go` does not error on the new file's presence.

Amendment required to `docs/spec/uninstall.md`: append a change-log entry under `## Change log` noting that profile.md is explicitly preserved (user data, no pre-install counterpart).

The routing instruction in `~/.claude/CLAUDE.md` is removed by uninstall (it is inside the atomic-owned block, which is either deleted or LLM-merged out). After uninstall, profile.md remains on disk but is no longer @-ref'd. The user retains the file and can re-add the ref manually.


## Doctor integration

New check appended to the existing nine-check suite in `atomic/internal/doctor/`:

| Name (canonical) | Checks | Fail severity |
|------------------|--------|---------------|
| `profile` | `@~/.claude/.atomic/profile.md` ref present in one of `~/.claude/CLAUDE.md`, `~/.claude/claude.local.md`, `~/.claude/CLAUDE.local.md` (same search order as refs check). `~/.claude/.atomic/profile.md` exists on disk. | WARN for missing ref; WARN for missing file |

Severity rationale: profile.md absence is degraded experience, not a broken installation. FAIL is reserved for checks that block core functionality (axiom alignment: WARN for drift, FAIL for missing critical paths).

`--fix` repair for the profile check: if file absent → create empty stub. If ref absent → insert the ref into `~/.claude/CLAUDE.md`. Both repairs require user confirm per-item (axiom 3).

The check index is whatever is next available at implementation time — do not bake the index into the spec. The check must be registered alongside the existing doctor checks; implementer verifies the current max index in `atomic/internal/doctor/` before assigning.


## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Path constant + profile stub creation at install (`atomic-builder`, ~5–7 files) | `atomic/internal/config/paths.go`, `atomic/internal/claudeinstall/install.go`, `atomic/internal/profile/` (new package for env capture), tests | `go test ./atomic/internal/claudeinstall/...` and `./atomic/internal/profile/...`: install creates stub when absent, is no-op when present; env fields populated; empty strings on git config failure; stdout nudge fires on first install, suppressed on second |
| 2 | CLAUDE.md @-ref + routing instruction (`atomic-builder`, ~2 files) | `CLAUDE.md` (repo root — bundle source, NOT `~/.claude/CLAUDE.md`), then `make -C atomic bundle` | Run from repo root: `grep -n 'profile.md' ./CLAUDE.md` returns a match AND `grep -F 'Personal facts about you' ./CLAUDE.md` returns a match; `make -C atomic bundle && git diff --exit-code` clean. Greps must target the repo-root `CLAUDE.md`, not the installed `~/.claude/CLAUDE.md`. |
| 3 | `/atomic-improve` template additions (`atomic-surgeon`, ~3 files) | `templates/commands/atomic-improve.md`, then `make render` + `make -C atomic bundle` | `grep -n 'profile drift' commands/atomic-improve.md` returns a match; profile.md listed in discovery brief; `make render && git diff --exit-code` clean; `make -C atomic bundle && git diff --exit-code` clean |
| 4 | Uninstall preservation + spec amendment (`atomic-surgeon`, ~2 files) | `atomic/internal/claudeinstall/uninstall.go`, `docs/spec/uninstall.md` | `go test ./atomic/internal/claudeinstall/...`: uninstall plan does not include profile.md in delete list; spec change-log entry present |
| 5 | Doctor check (`profile`) (`atomic-builder`, ~3–4 files) | `atomic/internal/doctor/` (new check or addition to existing checks file), tests | `go test ./atomic/internal/doctor/...`: check reports WARN when profile.md absent; WARN when @-ref absent from all three candidate files (`~/.claude/CLAUDE.md`, `~/.claude/claude.local.md`, `~/.claude/CLAUDE.local.md`); PASS when both present |
| 6 | Documentation surfaces (`atomic-surgeon`, ~2–3 files) | `README.md`, `docs/guides/install.md`, `docs/reference/commands.md` or relevant reference table | `grep -n 'profile.md' README.md` returns a match; `grep -n 'profile.md' docs/guides/install.md` returns a match; `grep -n 'profile.md' docs/reference/commands.md` (or applicable reference file) returns a match |

Checkpoints 2, 3, 4, 5 each depend only on checkpoint 1. They are independent of each other and can be implemented in parallel. Checkpoint 6 is last.


## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Routing instruction wording is ambiguous — Claude sends facts to wrong surface | Medium | Verbatim text is locked in § Routing contract. Spec is the source; CLAUDE.md must match exactly. |
| `atomic claude update` overwrites the @-ref and routing instruction if not in the bundle source `CLAUDE.md` | High (if missed) | Spec explicitly requires the text be in `CLAUDE.md` at repo root (bundle source), not written only at install time. Build gate (`make bundle`) will catch drift. |
| Install env capture blocks on slow git invocation | Low | Capture is a `git config --global` read — fast. No network. No fallback needed beyond empty string on error. |
| `BuildUninstallPlan` accidentally includes `profile.md` in delete list if future manifest schema changes | Low | Checkpoint 4 adds an explicit test asserting profile.md is absent from the delete list. |
| Doctor check numbering collides if another check is added before this ships | Low | Spec does not bake the index. Implementer checks current max in `atomic/internal/doctor/` at implementation time; amends this spec if a conflict arises. |
| Profile drift findings crowd out other `/atomic-improve` findings in the 15-item cap | Low | Profile drift findings count against the shared cap. If crowding becomes a problem in practice, a dedicated sub-cap is a future amendment. |
| User treats profile.md as CLAUDE.md substitute and hand-edits instructions into it | Low | Both files load; the behavior is odd but not broken. The routing paragraph distinguishes the two surfaces. No enforcement needed. |


## v2 — Deterministic environment refresh + dev-tooling fingerprint


### v2 Non-goal clarification

v1 §Non-goals states *"Not time-tracked. No `last_observed` fields, no staleness clock, no expiry."* That non-goal stands **for conversation-observed sections** (Identity, Work, Active projects, Interests, People mentioned). v2 adds a refresh clock **only to the `<deterministic>` block**, which v1 already excludes from drift detection. The two do not conflict: observed facts remain un-clocked and are never auto-rewritten; the `## Environment` section gains a refresh cadence because it mirrors machine state, not conversation.


### v2 Detection registry

A static registry of ~50 known tools, implemented in deterministic Go. The registry is the **sole** source of truth for what gets detected — no LLM discovery, no runtime discovery, no `config.toml` nomination. The registry is extended only by editing the Go source.

| Category | Representative entries |
|----------|----------------------|
| Language runtimes | node, python/python3, go, rustc, ruby, java, elixir, deno, bun, php, gcc/clang |
| Package/build managers | npm, pnpm, yarn, pip, cargo, bundler, mix, maven, gradle, make, bazel |
| Version managers | nvm, pyenv, rbenv, asdf, mise, rustup, volta, fnm, sdkman |
| Containers/orchestration | docker, docker-compose, podman, kubectl, helm, k9s, minikube, kind |
| Monorepo/build | nx, turbo |
| CLI tools | jq, yq, rg, ast-grep/sg, fd, fzf, gh, git, curl |
| Cloud | aws, gcloud, az, terraform, pulumi |

Each registry entry specifies: candidate binary name(s), version command, and detection strategy (binary, directory, or both — see §Version-manager detection).

macOS and Linux only. No Windows detection paths.


### v2 Version-manager detection

Several version managers (`nvm`, `sdkman`, and others) are **shell functions, not PATH binaries**. `exec.LookPath` returns "not found" even when they are installed. Each registry entry declares its detection strategy:

| Strategy | Mechanism |
|----------|-----------|
| `binary` | `exec.LookPath` on the binary name |
| `directory` | existence check on a known install directory (`~/.nvm`, `~/.pyenv`, `~/.rbenv`, `~/.asdf` or `$ASDF_DIR`, `~/.sdkman`, `~/.volta`, `~/.local/share/mise`) |
| `both` | binary first; fall back to directory check |

The strategy field is mandatory in the registry. Absence = a detection bug, not a graceful skip.


### v2 Provenance

For each detected runtime, record:

1. **Active binary** — the binary that resolves when the tool is invoked in the user's default environment.
2. **Version** — trimmed first line of the version command output, verbatim. No semver parsing. **Exception (v2.1):** a registry entry may declare a *version-line prefix* (e.g. `Elixir`, `Mix`); when set, the first output line starting with that prefix is captured instead of line 1 — needed for tools whose `--version` leads with an unrelated banner (elixir/mix print the Erlang/OTP banner first). If the binary exists but the version command exits non-zero, record `unknown`.
3. **Source** — classification of the active binary's resolved path. **When the path is under a known version manager, the source is that manager's name** (v2.1); otherwise one of the fixed labels:

| Source | Path signal |
|---|---|
| `pyenv` / `nvm` / `asdf` / `rbenv` / `volta` / `fnm` / `mise` / `rustup` | resolved path under that manager's dir (`~/.pyenv`, `~/.nvm`, `~/.asdf`, `~/.rbenv`, volta/fnm/mise dirs) |
| `brew` | path under `/opt/homebrew` or `/usr/local` (macOS), or linuxbrew |
| `sys` | `/usr/bin`, `/bin`, `/usr/local/bin` (non-Homebrew) |
| `other` | anything else; record raw path |

**No version enumeration.** A version-manager user may have many installed pythons; only the active one is recorded. Separately, a presence flag is recorded per installed version manager.

Output shape in the `## Environment` block: `python: 3.12 (pyenv)`, `node: v24 (nvm)`, `go: 1.25 (sys)` for the active runtime; `pyenv: installed` as a standalone entry in the version-managers list.


### v2 Shell enumeration

Detected deterministically (filesystem + env reads):

- **Login shell** — value of `$SHELL`.
- **Shell framework** — `~/.oh-my-zsh` → oh-my-zsh; `~/.zprezto` → prezto; `starship` binary presence → starship. Additional frameworks detectable by known paths.
- **oh-my-zsh custom plugins and themes** — enumerate files/dirs under `~/.oh-my-zsh/custom/plugins/` and `~/.oh-my-zsh/custom/themes/`.
- **oh-my-zsh custom scripts (v2.1)** — enumerate top-level `~/.oh-my-zsh/custom/*.zsh` files (oh-my-zsh auto-sources these). Rendered as a `custom scripts` line.


### v2 `<deterministic>` tag attribute

The `<deterministic>` tag gains a `lastcheck` attribute set to the ISO date of the last successful refresh:

```
<deterministic lastcheck=YYYY-MM-DD>
```

The attribute is absent in files created by v1 install (before v2 lands). A missing `lastcheck` is treated as infinitely stale by the `--if-stale` gate and by the doctor staleness check.


### v2 `atomic profile refresh` subcommand

New user-facing subcommand. Not present in v1.

| Invocation | Behavior |
|-----------|----------|
| `atomic profile refresh` | Unconditional: re-detect all registry entries + shell enumeration, rewrite the `## Environment` section wholesale, stamp `lastcheck` with today's date. Exit 0. |
| `atomic profile refresh --if-stale <dur>` | Self-gating: parse `lastcheck` from the current `## Environment` block; if within the window → no-op, exit 0; else run the unconditional refresh. Duration format: `7d`, `30d`. |

The `--if-stale` gate is deterministic Go. No LLM in the loop.


### v2 In-place `## Environment` rewrite

The binary owns the **entire `## Environment` section** — from the `## Environment` heading to the next `##` heading (or EOF) — and rewrites it wholesale on every refresh. Rewrite and recreate are the same operation.

| Case | Behavior |
|------|----------|
| Section present (clean) | Replace heading → next-`##` span wholesale |
| Section present but malformed (half-block, tags stripped) | Same wholesale replace — the anchor is the heading, not the tags; cannot produce duplicates |
| Section absent (not in the file) | Append a fresh `## Environment` section at EOF |
| File absent | Recreate the whole file from the stub, then populate |

User-authored sections (Identity, Work, Active projects, Interests, People mentioned) are outside the `## Environment` span and are **never touched** by any refresh operation.


### v2 Session-start hook wiring

The existing session-start handler (`cmd/atomic/main.go`, `case "session-start"` → `hooks.SessionStart`) is extended to also invoke `atomic profile refresh --if-stale 7d`. This fires on every Claude Code session open; the `--if-stale` gate makes it a no-op when the env block is fresh.

Tradeoff accepted: a user who never opens a session for >7 days gets a stale block until their next session. Acceptable — the data is only consumed inside a session.


### v2 Doctor staleness extension

The existing doctor category 10 (`profile` check in `atomic/internal/doctor/checks_profile.go`) is extended with a third sub-check:

| Sub-check | Condition | Severity |
|-----------|-----------|----------|
| File exists | `~/.claude/.atomic/profile.md` absent | WARN |
| @-ref wired | Ref absent from all three candidate files | WARN |
| `lastcheck` freshness | `lastcheck` absent or older than 30 days | WARN |

The staleness window (30 days) is a constant in the check, not a config value. If the `lastcheck` attribute is absent (v1-format file), the sub-check always fires as WARN with a message directing the user to run `atomic profile refresh`.

The 30-day doctor-WARN threshold and the 7-day session-start `--if-stale` gate are intentionally different: the session-start gate keeps the env block fresh during active use; the doctor threshold is a longer safety net that fires only when the user hasn't opened a session in a month or more. Implementers must not unify these two constants.


### v2 Success criteria

- [ ] `atomic profile refresh` (no flags) re-detects all registry tools, rewrites `## Environment` wholesale, stamps `lastcheck=YYYY-MM-DD`. Exit 0.
- [ ] `atomic profile refresh --if-stale 7d` is a no-op (exit 0, no file write) when `lastcheck` is within 7 days.
- [ ] `atomic profile refresh --if-stale 7d` runs a full refresh when `lastcheck` is absent or older than 7 days.
- [ ] Registry covers all 7 categories (≥ 45 entries). Registry is the sole detection source — no LLM, no config.
- [ ] Version-manager detection finds nvm/sdkman/etc. via directory check when `exec.LookPath` fails.
- [ ] Each detected runtime records: version string (trimmed first line), source class (system/version-manager/homebrew/other).
- [ ] `$SHELL`, shell framework (oh-my-zsh/prezto/starship), and oh-my-zsh custom plugins/themes appear in the refreshed `## Environment` block.
- [ ] Malformed `## Environment` block (tags stripped, truncated) self-heals on next refresh — no duplicate sections.
- [ ] File-absent case: refresh recreates the stub file, then populates `## Environment`.
- [ ] Section-absent case: refresh appends `## Environment` at EOF without touching other sections.
- [ ] Session-start hook fires `atomic profile refresh --if-stale 7d`; verified by unit test on the hook handler.
- [ ] Doctor category 10 reports WARN when `lastcheck` is absent; WARN when `lastcheck` is older than 30 days; PASS when fresh.
- [ ] `atomic profile refresh` appears in CLAUDE.md `## Atomic binary subcommands`, `/atomic-help` topic table + tour, README, `docs/reference/` tables.
- [ ] `make render && git diff --exit-code` clean after checkpoint 6.
- [ ] `make -C atomic bundle && git diff --exit-code` clean after checkpoint 6.
- [ ] `go test ./...` (from `atomic/`) passes after all checkpoints.


### v2 Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|-----------|-------------|-------|-----------|---------|
| 1 | Detection registry + detectors + tests | `atomic/internal/profile/` (extend package): registry definition, binary/directory/both detection, version capture, source-class classification, shell enumeration | `atomic-builder` | 3–5 | `go test ./atomic/internal/profile/...`: registry ≥ 45 entries; nvm found via `~/.nvm`; active python source class correct; trimmed first-line version capture; shell/framework detected |
| 2 | Render + in-place `## Environment` rewrite + tests | `atomic/internal/profile/` (rewrite engine): heading-anchored wholesale replace, 4-case table (clean / malformed / section-absent / file-absent), `lastcheck` stamp | `atomic-builder` | 2–4 | `go test ./atomic/internal/profile/...`: clean rewrite; malformed self-heals; section-absent appends; file-absent recreates; `lastcheck` attribute written |
| 3 | `atomic profile refresh` subcommand + `--if-stale` gate + tests | `atomic/cmd/atomic/main.go` (subcommand dispatch), `atomic/internal/profile/` (refresh entry point + stale gate) | `atomic-builder` | 3–5 | `go test ./...`: bare refresh triggers full rewrite; `--if-stale 7d` no-ops within window; `--if-stale` refreshes on stale/absent `lastcheck`; exit codes correct |
| 4 | Session-start hook wiring + test | `atomic/internal/hooks/hooks.go` (extend `SessionStart`), `atomic/internal/hooks/hooks_test.go` | `atomic-surgeon` | 2 | `go test ./atomic/internal/hooks/...`: `SessionStart` invokes `atomic profile refresh --if-stale 7d`; test mocks the invocation and asserts it fires |
| 5 | Doctor `lastcheck`-staleness extension + test | `atomic/internal/doctor/checks_profile.go`, `_test.go` | `atomic-surgeon` | 2 | `go test ./atomic/internal/doctor/...`: WARN when `lastcheck` absent; WARN when older than 30 days; PASS when fresh; existing file-exists and @-ref sub-checks still pass |
| 6 | Mandatory-checklist surfaces | `CLAUDE.md` (binary subcommands section), `templates/commands/atomic-help.md` (topic table + tour), `README.md`, `docs/reference/concepts.md` (binary subcommands + profile section); then `make render` + `make -C atomic bundle` + `/refresh-signals` | `atomic-surgeon` | 4–6 | `grep -n 'atomic profile refresh' CLAUDE.md` returns match; same grep in `commands/atomic-help.md` returns match; `grep -n 'atomic profile refresh' docs/reference/concepts.md` returns match; `atomic signals stale` exits 0 (signals fresh after `/refresh-signals`); `make render && git diff --exit-code` clean; `make -C atomic bundle && git diff --exit-code` clean |

Checkpoints 1 and 2 are sequential (rewrite engine depends on the detector). Checkpoint 3 depends on 1 and 2. Checkpoints 4 and 5 depend on 3. Checkpoint 6 is last.


### v2 Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Version-manager directory paths drift across OS versions or manager updates | Medium | Registry directory constants are listed explicitly; tests run against tempdir stubs so false-positive coverage is visible. Flag as a known maintenance item. |
| Wholesale heading-anchored rewrite truncates a user section if the heading parser misidentifies the span boundary | Low | Checkpoint 2 tests include a multi-section fixture; the parser must stop at the next `##` token, not at `EOF` when a next section exists. |
| `--if-stale` duration parsing is lenient and accepts unexpected formats silently | Low | Accept only `<N>d` format; return an explicit parse error for anything else. |
| Session-start hook adds latency on every session open (even when no-op) | Low | The `--if-stale` path reads only the `lastcheck` attribute from the file header; no detection runs. Fast file read only. |
| Registry grows unbounded; detection on slower machines takes perceptible time | Low | Parallelise detections (all LookPath + Stat calls concurrent). Cap at 60 entries initially; revisit at next registry expansion. |
| Doctor `lastcheck` WARN fires immediately after install (v1-format files have no `lastcheck`) | Medium | WARN message must direct user to `atomic profile refresh`, not imply a broken install. Wording is load-bearing. |
| `atomic profile refresh` subcommand added without completing mandatory checklist | Medium | Checkpoint 6 is the dedicated checklist checkpoint; reviewer gate is required before closing. |


## v2.2 — Install-time population + no-hooks fallback


### v2.2 Goal

Install and update leave a **complete** env fingerprint, not the v1 five-field stub. The same `RefreshIfStale` entry point serves install, update, the session-start hook, and an LLM fallback for hook-less environments. Detection is bounded against hung tools. A force-refresh path always exists.

### v2.2 Refresh window

- Single cadence knob `W`, **default 24h**, as a code constant. Config-settable (`profile.refresh_window`) is a deliberate future amendment, NOT built now (axiom 2: default in code, promote on demand).
- `RefreshIfStale(claudeHome, today, W)` early-exits when `lastcheck` is within `W`; else full refresh.
- Install, update, and the session-start hook all pass the same `W`. **Supersedes** v2's hook-hardcoded `7d`.
- Doctor's 30-day staleness WARN is separate and unchanged.

### v2.2 Install/update population

- After `ensureProfileStub` in the install/update flow, call `RefreshIfStale(claudeHome, today, W)`.
- Fresh install: stub has no `lastcheck` → stale → full detect → complete profile day one.
- Re-install / update: refreshes only if older than `W`; no-op otherwise.
- **Best-effort (mandatory):** the call is wrapped so any error or panic is swallowed and install/update still completes with at least the stub present. Install MUST NOT fail because detection failed. Mirrors the session-start hook's swallow behavior.
- Order: stub → populate → CLAUDE.md `@-ref` wiring.

### v2.2 Per-tool detection timeout

- Each tool's version command runs under a context timeout, **≈3s**. On expiry: record `unknown`, continue (same outcome as the non-zero-exit guard).
- One hung `--version` cannot stall install, session-start, or manual refresh.
- Distinct from `W`: the timeout bounds a single subprocess; `W` bounds staleness of the whole block.

### v2.2 Force-refresh path

- Bare `atomic profile refresh` (no flag) is **always** an unconditional refresh — independent of `W`, hooks, or staleness. Shipped in v2; v2.2 documents it as the guaranteed manual override. `--if-stale W` is the gated variant used by install/update/hook.

### v2.2 No-hooks LLM fallback

Some environments disallow hooks (enterprise policy, `CLAUDE_CODE_DISABLE_*`, `--bare`); the session-start hook never fires there. **Named `prefer-code-over-model` exception** (the deterministic trigger is unreliable → an LLM safeguard is acceptable as defense-in-depth).

- Ship an LLM-fallback instruction in the `CLAUDE.md` profile preamble (the routing block, always `@`-loaded). **Verbatim text (the contract — emit exactly, like §Routing contract):**

```
`profile.md`'s `## Environment` block is refreshed automatically by the session-start hook. If hooks are disabled in your environment and its `<deterministic lastcheck=...>` date is more than a day old, run `atomic profile refresh --if-stale 1d` once, silently, to update the detected tooling.
```

  CP3's grep asserts a stable substring of this (e.g. `refreshed automatically by the session-start hook`).
- Shipped **unconditionally** — no hook-presence detection. Hook present → it refreshes first, the LLM sees fresh `lastcheck` → no-op. Hook absent → the LLM is the trigger. The `--if-stale` gate dedupes.
- Text lives in the repo-root `CLAUDE.md` (bundle source), emitted into `~/.claude/CLAUDE.md` on install. Deterministic path stays primary; LLM is strictly backup; doctor's 30d WARN is the backstop.
- Honesty: probabilistic (model may skip it); requires Bash permission for the refresh; in maximally-locked envs the profile stays at install-time state until manually refreshed.

### v2.2 Nudge copy

- Retarget the first-install stdout nudge away from "Claude will fill it in" (the env block is now populated at install) toward the conversational sections (Identity/Work/projects) the user fills over time.

### v2.2 Success criteria

- [ ] `atomic claude install` with no pre-existing profile yields a `## Environment` block containing detected tooling (not just the five v1 fields) plus a `lastcheck` stamp.
- [ ] Install/update population is best-effort: an injected detection failure leaves install exit 0 with the stub present.
- [ ] Re-install/update with a fresh `lastcheck` does NOT rewrite the block (no-op).
- [ ] Each tool's version command is bounded by a ~3s timeout; a deliberately-hung command yields `unknown` and does not block the batch.
- [ ] The shared refresh-window constant equals 1 day (24h); install, update, AND the session-start hook all pass this constant — the hook no longer passes a literal `7`.
- [ ] Config key `profile.refresh_window` is NOT introduced this iteration (the window stays a code constant — axiom 2, promote later).
- [ ] Bare `atomic profile refresh` performs an unconditional refresh regardless of `lastcheck` (regression-assert).
- [ ] `CLAUDE.md` profile preamble contains the no-hooks LLM-fallback instruction; present in both source and the embedded bundle; `make -C atomic bundle && git diff --exit-code` clean.
- [ ] First-install nudge copy no longer claims Claude fills the env block.
- [ ] `go test ./...` green.

### v2.2 Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|-----------|-------------|-------|-----------|----------|
| 1 | Per-tool detection timeout | `atomic/internal/profile/detect.go` + test | `atomic-surgeon` | 2 | `go test ./internal/profile/...`: a version command that sleeps well past the timeout (e.g. `sleep 10`) yields `unknown` and the detect call returns in ≤ ~2× the per-tool timeout (≤ ~6s, not ~10s); other detections in the batch unaffected; existing detection behavior unchanged |
| 2 | Shared `W` const + install/update population (best-effort) + hook window + nudge retarget | `atomic/internal/profile/` (W const), `atomic/internal/claudeinstall/install.go`, `atomic/internal/hooks/hooks.go` + tests | `atomic-builder` | 4–6 | `go test ./...`: fresh install populates fingerprint + `lastcheck`; existing+fresh = no-op; injected detection failure → install exit 0 with stub; hook + install both pass the shared 1-day constant (no literal `7` in the hook); `ProfileNudge` no longer contains `Claude will fill it in` |
| 3 | No-hooks LLM-fallback line in `CLAUDE.md` preamble + bundle regen | `CLAUDE.md` (profile preamble); then `make -C atomic bundle` | `atomic-surgeon` | 2–3 | `grep -F 'refreshed automatically by the session-start hook' CLAUDE.md` AND the same in `atomic/internal/embedded/bundle/CLAUDE.md` both match; `make -C atomic bundle && git diff --exit-code` clean; `go test ./...` green |

CP1 → CP2 (population relies on bounded detection). CP3 is independent (preamble/doc + bundle).

### v2.2 Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Install latency from synchronous detection (~1s; longer on slow machines) | Medium | Per-tool ~3s timeout caps worst case; bounded-parallel; runs once per install/update, gated on later runs |
| Best-effort swallow masks a real detection regression | Low | Detection correctness is asserted by its own unit tests; the swallow only guards install/update from aborting |
| LLM fallback never fires (model skips it) in a no-hooks env | Medium | Defense-in-depth, not the mechanism; doctor's 30d WARN backstops; bare `atomic profile refresh` is the manual override |
| LLM fallback fires redundantly when the hook is present | Low | `--if-stale` gate → no-op; harmless |
| Window change `7d`→`24h` raises hook-triggered refresh frequency | Low | No-op is a cheap `lastcheck` read; only an actual refresh costs ~1s, at most once per `W` per active session |


## Change log

### 2026-05-28 — v2 deterministic env refresh + dev-tooling fingerprint

**What changed:** Added v2 contract sections: detection registry (7 categories, ~50 tools, registry-is-sole-source), version-manager shell-function detection (binary/directory/both strategy), provenance model (active runtime + source class + per-manager presence flag), version capture (trimmed first line, no parsing), shell enumeration (`$SHELL` + framework + oh-my-zsh custom), `<deterministic lastcheck=YYYY-MM-DD>` attribute, `atomic profile refresh [--if-stale <dur>]` subcommand semantics, in-place `## Environment` heading-anchored wholesale rewrite (4-case table), session-start hook wiring, doctor `lastcheck`-staleness extension. Added v2 success criteria, checkpoints (6), and risks.

**Why:** v1 env capture was install-only and covered only 5 static fields (git user, OS, arch, CPU). Users need a periodically-refreshed dev-tooling fingerprint so Claude can steer correctly (version managers in play, active runtime vs. system-installed, shell framework, containers/cloud tooling). All v2 decisions were locked via `/gather-evidence` + `/pressure-test` on 2026-05-28.

**Superseded:** v1 §Non-goals "Not time-tracked" is narrowed — the non-goal now applies only to conversation-observed sections (Identity, Work, Active projects, Interests, People). The `<deterministic>` block gains a refresh clock. The v2 non-goal clarification section above makes this explicit so the two do not read as contradictions.


### 2026-05-28 — v2 spec reviewer fixes (CP6 Verifies + threshold intent)

**What changed:** Three amendments to the v2 section only.
1. v2 CP6 Verifies column now asserts `grep -n 'atomic profile refresh' docs/reference/commands.md` returns a match (was missing).
2. v2 CP6 Verifies column now asserts `atomic signals stale` exits 0 after `/refresh-signals` (signals-refresh step was unverifiable as written).
3. Added one sentence after the doctor staleness window paragraph making the intentional split between the 7d session-start gate and the 30d doctor-WARN threshold explicit, so an implementer does not unify them.

**Why:** Reviewer VERDICT: CHANGES_REQUESTED — missing doc-update assertion in CP6, missing signals-refresh verifiability, and implicit threshold distinction that an implementer could reasonably collapse.

**Superseded:** CP6 Verifies cell prior text (no doc-reference grep, no signals-stale assertion).


### 2026-05-28 — CP6 Verifies doc-file correction

**Correction:** CP6 Verifies cell referenced `docs/reference/commands.md` as the doc surface for `atomic profile refresh`. Binary subcommands are documented in `docs/reference/concepts.md`, not in `commands.md` (which covers slash commands only). Corrected the Verifies cell and the Files/areas column to point at `concepts.md`. How we know: CP6 implementation found no `atomic profile refresh` entry in `commands.md` because that file does not cover binary subcommands; convention is `concepts.md` for binary CLI features.


### 2026-05-28 — v2.1 detection refinements

**What changed:** Four refinements after dogfooding the merged v2 on a real machine: (1) **Provenance names the version manager** — a runtime resolved under a manager's dir now reports that manager (`python: 3.12 (pyenv)`, `node: v24 (nvm)`) instead of the generic `version-manager`. (2) **Source labels shortened** — `system`→`sys`, `homebrew`→`brew`. (3) **Per-tool version-line prefix** — registry entries may declare a prefix (`Elixir`, `Mix`) so the captured version is the matching line, not the leading Erlang/OTP banner. (4) **oh-my-zsh `custom/*.zsh`** top-level scripts are now enumerated alongside custom plugins/themes.

**Why:** User feedback after running `atomic profile refresh` live — the generic `version-manager` label lost information we already had, `elixir`/`mix` showed `unknown`, the long labels added noise, and omz custom scripts (a real omz extension point) were missed.

**Superseded:** v2 §Provenance fixed source-class enum (`version-manager`/`homebrew`/`system`/`other`) → now manager-name-or-`brew`/`sys`/`other`. v2 Polish-1 decision F-16 (elixir/mix presence-only, render `unknown`) → reversed: elixir/mix capture their real version via the version-line prefix. v2 shell enumeration (plugins + themes only) → also custom scripts.


### 2026-05-29 — v2.2 install-time population + no-hooks fallback

**What changed:** Added §"v2.2 — Install-time population + no-hooks fallback": install/update now call `RefreshIfStale(W)` after `ensureProfileStub` (best-effort) so a fresh install yields a complete fingerprint, not the five-field stub; a single refresh window `W` (default 24h, code constant) is shared by install/update/hook; each tool's version command gets a ~3s timeout; an unconditional LLM-fallback instruction is added to the `CLAUDE.md` profile preamble for hook-less environments; the first-install nudge copy is retargeted. Added v2.2 goal, success criteria, 3 checkpoints, risks.

**Why:** Dogfooding v2/v2.1 showed install leaves a half-populated profile (rich detection only ran on the first hooked session). Surfaced the trigger-model gap, the hook dependency (enterprises may disallow hooks), and the need for a guaranteed force-refresh path. Design captured in `docs/design/user-profile.md` §v2.2 (gather/pressure-test-style decisions made inline this session).

**Superseded:** v2 §Bootstrap (install = "create stub + 5 env fields") → install also populates the full fingerprint. v2 §Scheduling hook window `7d` → shared `W` default `24h`. Adds the no-hooks LLM-fallback path (new behavior, defense-in-depth).


## Implementation log


### v1.0 — 2026-05-28


Built across 8 iterations of `/subagent-implementation` (6 checkpoints, 2 polish batches).

Commits on `feat/user-profile` (chronological):

- `1459eb4` — CP1: install stub + env capture (`config.ProfilePath`, new `atomic/internal/profile` package, `ensureProfileStub` in install flow, stdout nudge on first create).
- `d11b249` — CP2: CLAUDE.md @-ref + routing paragraph inside the `<atomic>` block. Bundle regenerated.
- `0ce69c1` — CP3: `/atomic-improve` template additions (discovery brief catalog, history brief profile-drift detection, Phase 6 walk branch with 4-option set).
- `b74e36a` — CP4: uninstall preservation guard in `BuildUninstallPlan` + change-log entry on `docs/spec/uninstall.md`.
- `fb5623f` — CP5: doctor `profile` check (category 10, severity WARN), CLAUDE.md count update, `atomic-doctor` spec amendment, `--fix` deferral.
- `ec289f1` — CP6: docs surfaces (README "What you get", `docs/guides/install.md` after-install note, `docs/reference/concepts.md` reference section).
- `14228f2` — Polish A: Go follow-ups (F-1, F-2, F-3, F-4, F-8, F-9, F-10).
- `424da39` — Polish B: docs follow-ups (F-5, F-6, F-7, F-11).

**Out-of-scope work performed during this build:**

- None. Every iteration stayed within its checkpoint scope; cross-cutting changes (e.g. CLAUDE.md "ten checks" descriptor in CP5) were intentional and surfaced by the reviewer at the correct iteration.

**Unforeseens — surprises that emerged during implementation:**

- CP3 reviewer caught a contract gap missed during spec authoring: the Phase 6 walk needed a profile-drift branch with the 4-option set (`Accept new / Modify / Keep both / Skip`); the iter-1 implementer only wired data capture, not the user-facing prompt. Fixed in CP3 iter 2 without spec amendment (the spec already mandated the 4-option set; the implementation simply hadn't carried it through).
- CP5 reviewer found three documentation gaps the brief missed: CLAUDE.md doctor subcommand descriptor (nine → ten), `docs/spec/atomic-doctor.md` table row + change-log, and a misleading "unknown category" message in `fix.go`'s `repairPlan` default branch for `--fix profile`. All three closed in CP5 iter 2.

**`--fix` for the doctor profile check is deferred:**

- `repairPlan` returns `fixable=false` with a user-actionable message ("run `atomic claude install`"). `applyRepair` has a companion `case "profile"` returning a not-yet-implemented error as a defensive coupling. Auto-repair of the @-ref leg is intentionally out of scope because `~/.claude/CLAUDE.md` is bundle-source-driven; inserting at the installed copy diverges from the bundle on next install/update.

**Deferred items still open:**

- None. All 11 follow-ups from the 8 CP iterations were dispositioned `fix-now` and landed in Polish A (commit `14228f2`) and Polish B (commit `424da39`). No items promoted to `.claude/project/followups/` and no GitHub issues filed for this feature.

**Merged into main as `69b6ce9` — 2026-05-28.**


### v2.0 — 2026-05-28


Deterministic env refresh + dev-tooling fingerprint. Built across 6 checkpoints + 3 polish batches of `/subagent-implementation` on branch `feat/user-profile-v2` (from base `fc51ee8`).

Commits (chronological):

- `e6ac3d6` — CP1: detection registry (~55 tools, 7 categories), presence via LookPath + install-dir fallback for shell-function managers, source-class classification, shell enumeration.
- `9728633` — CP2: render `## Environment` section (injected date, `lastcheck` attr) + heading-anchored wholesale in-place rewrite (4 cases). Folded F-1 (shell-framework LookPath seam).
- `4956bea` — CP3: `atomic profile refresh [--if-stale <Nd>]` subcommand + staleness gate (Nd-only parse, exit 1 bad-duration / 2 unknown-verb).
- `27d89c4` — CP4: session-start hook fires best-effort in-process `RefreshIfStale(..., 7)`; failures swallowed, never block reminder injection.
- `8e91aee` — CP5: doctor category-10 third leg — WARN when `lastcheck` absent or >30d; doctor spec amended.
- `d4927c7` — CP6: discoverability — CLAUDE.md binary subcommands, `/atomic-help` topic + tour, README, `docs/reference/concepts.md`; `make render` + `make bundle` regenerated.
- `b0263eb` — Polish 1: version-capture quality (F-3 non-zero-exit→`unknown`, F-16 elixir/mix presence-only, F-17 kubectl `--client`, F-18 corepack skip) + bounded-concurrency detection (F-2) + cleanups (F-4..F-8).
- `6267a25` — Polish 2+3: hooks (F-9 seam doc, F-10 dead `//nolint`, F-11 exact-path assert) + doctor (F-12 doc, F-13 unreadable≠absent, F-14 stale-detail assert).

**Out-of-scope work performed during this build:**

- F-1 (shell-framework LookPath test seam) was folded into CP2 rather than its own iteration — same package, tightened the layer CP2 renders from.

**Unforeseens — surprises that emerged during implementation:**

- End-to-end smoke at finalize (running the built binary, not stubbed unit tests) revealed version capture was recording **error text and corepack prompts as version strings** — `rustc`/`cargo` (rustup, no default toolchain), `kubectl` (removed `--short` flag), `pnpm`/`yarn` (corepack). Stubbed unit tests passed because they fed clean output. Since profile.md is `@`-ref'd into every session, this was misleading-context-every-session, not a cosmetic nit. Confirmed harvested-finding F-3 as real and elevated it to fix-now; added F-16/F-17/F-18. Fixed in Polish 1. Lesson: detection-from-real-environment must be smoke-tested against the real binary, not only stubbed.
- Scheduling mechanism: the original "cron/routines" instinct was disqualified by `/gather-evidence` (Routines run cloud-side with no local file access; CronCreate expires at 7d). Resolved pre-implementation to the session-start hook. No implementation surprise — caught at design time.

**Deferred items still open:**

- None blocking. All 17 harvested follow-ups (F-1..F-18) dispositioned `fix-now` (user chose full polish A+B+C) and closed across the build + 3 polish batches. Residual test-strength observations from the Polish-1 review (concurrent-order stress test; corepack zero-exit-prompt unit test; exact-spacing assertion on the rewrite splice) were **accepted as residual** — the implementations are reviewer-confirmed correct by code-read and `go test -race`-clean; adding the extra assertions crosses into over-testing. Recorded here for traceability, not promoted to project follow-ups.

**`atomic validate` note:** the freshly-built worktree binary reported `0/0/0` checks (repo-detection quirk under a git worktree); render+bundle parity was instead confirmed by the CP6 reviewer's `make render && git diff --exit-code` + `make -C atomic bundle && git diff --exit-code` (both clean) and the pre-commit hook on `d4927c7`. CI runs the same drift gates.

**Squashed to `2e359ca` — 2026-05-28.** Per-iteration SHAs above (`e6ac3d6`..`c40c3dc`) are historical and unreachable from any branch.

**Merged into main as `8ffb1a6` — 2026-05-28.** (Fast-forward; squashed feature `2e359ca` + post-squash docs/signals follow-ups.)

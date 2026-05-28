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

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Path constant + profile stub creation at install | `atomic/internal/config/paths.go`, `atomic/internal/claudeinstall/install.go`, `atomic/internal/profile/` (new package for env capture), tests | builder | 5–7 | `go test ./atomic/internal/claudeinstall/...` and `./atomic/internal/profile/...`: install creates stub when absent, is no-op when present; env fields populated; empty strings on git config failure; stdout nudge fires on first install, suppressed on second |
| 2 | CLAUDE.md @-ref + routing instruction | `CLAUDE.md` (repo root — bundle source, NOT `~/.claude/CLAUDE.md`), then `make -C atomic bundle` | builder | 2 (CLAUDE.md + regenerated bundle outputs) | Run from repo root: `grep -n 'profile.md' ./CLAUDE.md` returns a match AND `grep -F 'Personal facts about you' ./CLAUDE.md` returns a match; `make -C atomic bundle && git diff --exit-code` clean. Greps must target the repo-root `CLAUDE.md`, not the installed `~/.claude/CLAUDE.md`. |
| 3 | `/atomic-improve` template additions | `templates/commands/atomic-improve.md`, then `make render` + `make -C atomic bundle` | surgeon | 3 (template + regenerated bundle outputs) | `grep -n 'profile drift' commands/atomic-improve.md` returns a match; profile.md listed in discovery brief; `make render && git diff --exit-code` clean; `make -C atomic bundle && git diff --exit-code` clean |
| 4 | Uninstall preservation + spec amendment | `atomic/internal/claudeinstall/uninstall.go`, `docs/spec/uninstall.md` | surgeon | 2 | `go test ./atomic/internal/claudeinstall/...`: uninstall plan does not include profile.md in delete list; spec change-log entry present |
| 5 | Doctor check (`profile`) | `atomic/internal/doctor/` (new check or addition to existing checks file), tests | builder | 3–4 | `go test ./atomic/internal/doctor/...`: check reports WARN when profile.md absent; WARN when @-ref absent from all three candidate files (`~/.claude/CLAUDE.md`, `~/.claude/claude.local.md`, `~/.claude/CLAUDE.local.md`); PASS when both present |
| 6 | Documentation surfaces | `README.md`, `docs/guides/install.md`, `docs/reference/commands.md` or relevant reference table | surgeon | 2–3 | `grep -n 'profile.md' README.md` returns a match; `grep -n 'profile.md' docs/guides/install.md` returns a match; `grep -n 'profile.md' docs/reference/commands.md` (or applicable reference file) returns a match |

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


## Change log

<!-- Populated on first amendment after the spec is approved. -->

# Review gate hook for main-agent code


## Goal


Code the main agent edits itself cannot be called done without an independent read. Two `atomic hooks` verbs registered by `atomic hooks install` keep per-session state and block the turn from ending while main-agent edits sit unreviewed; `atomic-verify` and the ship verbs' review gate dispatch `atomic-auditor` on the working diff, on the session's model, and the completion message names the pass and the model.


## Non-goals


- No verdict parsing in the hook. It records that a review agent ran, not what it said.
- No transcript reading. The hook never opens `transcript_path`.
- No gating of edits made by subagents or by the user.
- No change to the loop contracts: `/implement`, `/subagent-implementation`, `/quick-fix`, `/autopilot`, and `/subagent-diagnose` keep their per-checkpoint reviewer and once-per-loop auditor.
- No new top-level `atomic` verb.
- No per-hook opt-out flag. `atomic hooks uninstall` removes all three registrations; a user who wants some of them edits `settings.json` by hand.
- No detection of edits made through `Bash` (`sed -i`, redirection). The matcher sees the edit tools only; the hook is the backstop for the prose gate, not a replacement for it.


## Success criteria


- [ ] `atomic hooks post-tool-use` reads a PostToolUse JSON payload on stdin; with `agent_type` present it exits 0 and writes nothing; for `Edit`, `Write`, `MultiEdit`, `NotebookEdit` it records `tool_input.file_path` under the session; for `Agent` with `subagent_type` of `atomic-reviewer` or `atomic-auditor` it records the fingerprint of the gated set: the recorded paths that are inside the repository at `cwd`, dirty against `HEAD`, and not documentation, computed by the same function `atomic hooks stop` uses. No other tool name is recognized.
- [ ] `atomic hooks stop` reads a Stop JSON payload on stdin and exits 2 with a stderr message naming the unreviewed paths and the gate when at least one recorded path is dirty against `HEAD`, is not a documentation path, and the fingerprint of the dirty set differs from the recorded review fingerprint. In every other case, including `stop_hook_active: true`, no state, no git repository, malformed input, or any internal error, it exits 0 and prints nothing.
- [ ] The repository root is resolved once per call from the payload's `cwd` (`git rev-parse --show-toplevel`), symlinks resolved on the root and on every recorded path; a recorded path outside the root is dropped, never passed to git; every git call runs at the root, so pathspecs resolve when the session's `cwd` is a subdirectory. Git output is read NUL-separated (`--porcelain -z`) so non-ASCII paths are not quoted.
- [ ] The package API is `hooks.PostToolUse(stdin []byte)` and `hooks.Stop(stdin []byte) string`: neither returns an error, since every failure is a silent no-op by contract, and the CLI layer has no error branch to print.
- [ ] The documentation test matches the ship verbs' signals gate: under a `docs/` directory at any depth, or a top-level `README*`, `CHANGELOG*`, `CONTRIBUTING*`, `CODE_OF_CONDUCT*`, `SECURITY*`, `LICENSE*`. Every other path is source.
- [ ] The fingerprint changes when a recorded path is edited after the review and is stable when nothing changed; a commit that cleans every recorded path closes the gate.
- [ ] State lives at `<os temp dir>/atomic-hooks/<session_id>.jsonl`, append-only: `post-tool-use` appends one line per event, `{"touched":"<abs path>"}` or `{"reviewed":"<hex>"}`, with `O_APPEND`, and `stop` folds the lines (touched is the set of all touched lines, reviewed is the last reviewed line). Concurrent `PostToolUse` processes from parallel tool calls in one message therefore lose nothing. A `session_id` that is not `[A-Za-z0-9_-]+` is ignored.
- [ ] `atomic hooks install` registers three entries in `settings.json`: `SessionStart` (`atomic hooks session-start`, matcher `.*`, unchanged), `PostToolUse` (`atomic hooks post-tool-use`, matcher `Edit|Write|MultiEdit|NotebookEdit|Agent`), and `Stop` (`atomic hooks stop`, no matcher). Install is idempotent and adds only what is missing; `atomic hooks uninstall` removes all three and preserves unrelated registrations; JWCC comments and trailing commas survive both.
- [ ] `hooks.IsInstalled` reports `drifted` when the session-start hook is present but either review-gate hook is missing, so `atomic doctor` WARNs and `--fix` runs `atomic hooks install`.
- [ ] `cliusage` and the Cobra tree carry the two verbs with matching `Short` text; `TestCobraSubcommandMetadata` and the cliusage parity test pass.
- [ ] `context/skills/atomic-verify/SKILL.md` dispatches `atomic-auditor` in working-diff mode (not `atomic-reviewer`) when the main agent wrote the code outside a loop, requires the completion message to carry `audit: <verdict> (atomic-auditor on <model>)`, and names `atomic hooks stop` as the backstop that blocks the turn when the gate was skipped.
- [ ] `context/agents/atomic-auditor.md` admits `diff: working` as a caller-provided mode: the four passes run over the working tree (`git diff HEAD` plus untracked files) against the caller's stated intent, the commit pass reports nothing found, and the description names this use alongside the once-per-loop use without weakening it. In this mode the auditor runs the project's verification itself and reports a red suite as a 🔴 finding rather than refusing (the "dispatched before the suite is green" refusal is loop mode's), and the opening framing ("finished work, not a diff"; "the orchestrator that ran the loop") carries one clause admitting the working tree as the whole when no loop ran.
- [ ] The auditor's one-dispatch rule is scoped to loop mode in both `context/agents/atomic-auditor.md` (Rules) and `docs/spec/atomic-auditor.md` (Non-goals): in working-diff mode each gate event is its own dispatch, so a fix after `CHANGES_REQUESTED` may be audited again.
- [ ] `context/_partials/review-gate.md` dispatches `atomic-auditor` in working-diff mode on the working tree, the same scope `atomic-verify`'s gate and the stop hook use, and skips when `atomic-verify`'s gate already audited the same change this session; the docs-only guard keeps reading the staged set. `context/agents/atomic-reviewer.md`'s description no longer claims the ship verbs' gate.
- [ ] `context/CLAUDE.md` §Workflow names the auditor as the ad-hoc gate agent and the stop hook as its backstop, in one sentence.
- [ ] `context/commands/atomic-help.md` `review` and `agents` rows reflect the auditor's working-diff use; Stage 4 lists `atomic hooks install|uninstall`; the help-coverage loop prints zero `MISSING:` lines.
- [ ] `docs/spec/atomic-auditor.md` body no longer says "not a diff reviewer" without qualification and its change log records the widening.
- [ ] `docs/reference/skills.md`, `docs/reference/agents.md`, `docs/reference/workflow.md`, `docs/reference/commands.md`, `docs/guides/install.md`, and `docs/guides/contributing.md` describe the three hooks and the auditor gate where they described the session-start hook and the reviewer gate; no reference page still claims `atomic-reviewer` gates ad-hoc main-agent code.
- [ ] `docs/reference/hooks.md` is the one page for the Claude Code hooks contract (the three registrations, what each does, the state file, the documentation exemption, `stop_hook_active`, exit 2, what closes the gate, opt-out), listed in the root `CLAUDE.md` documentation-surfaces table; `docs/guides/install.md` describes the three hooks in a table and points there for the contract.
- [ ] `go test ./...`, `go vet ./...`, `gofmt -l .` empty, `atomic validate spec`, `atomic validate config`, `atomic validate artifacts`: zero FAIL.


## Approach


A `PostToolUse` hook records main-agent edits and review dispatches per session; a `Stop` hook exits 2 while recorded edits are dirty and unreviewed; the gate itself is `atomic-auditor` in a new working-diff mode — see `docs/design/review-gate-hook.md`.


## Change tree


```
atomic/internal/hooks/
├── reviewgate.go ..................... A  (PostToolUse, Stop, state file, fingerprint, docs-path test)
├── reviewgate_test.go ................ A  (state, docs test, fingerprint, fail-open, git-backed stop cases)
├── hooks.go .......................... M  (Install/Uninstall/IsInstalled over three registrations)
├── hooks_hujson.go ................... M  (register/unregister parameterized by event + matcher)
└── hooks_test.go ..................... M  (three-entry install, uninstall, drift on missing gate hooks)
atomic/cmd/atomic/
├── cmd_hooks.go ...................... M  (post-tool-use, stop verbs; usage strings)
├── cmd_claude.go ..................... M  (--no-hooks help text, install message)
└── main_test.go ...................... M  (wantCobraSubcommandMeta rows)
atomic/internal/cliusage/cliusage.go .. M  (two hooks entries)
atomic/internal/doctor/
├── checks_hooks.go ................... M  (drift detail names missing gate hooks)
├── checks_hooks_test.go .............. M  (drift case for missing Stop/PostToolUse)
├── fix.go ............................ M  (repair plan and result strings)
├── fix_test.go ....................... M  (fixture string)
├── format_test.go .................... M  (fixture string)
└── format_verbose_test.go ............ M  (fixture string)
context/
├── CLAUDE.md ......................... M  (§Workflow ad-hoc gate sentence)
├── skills/atomic-verify/SKILL.md ..... M  (auditor gate row, hook backstop, completion line)
├── agents/atomic-auditor.md .......... M  (description; diff: working caller context; pass notes)
├── agents/atomic-reviewer.md ......... M  (description drops the ship-verb gate clause)
├── _partials/review-gate.md .......... M  (auditor dispatch; already-audited guard)
└── commands/atomic-help.md ........... M  (review + agents rows; Stage 4 hooks line)
docs/
├── spec/atomic-auditor.md ............ M  (Non-goals + change log entry)
├── spec/review-gate-hook.md .......... A  (this file)
├── design/review-gate-hook.md ........ A
├── reference/skills.md ............... M  (atomic-verify row and intro)
├── reference/agents.md ............... M  (auditor and reviewer rows)
├── reference/workflow.md ............. M  (review gate paragraphs)
├── reference/commands.md ............. M  (/commit row)
├── reference/hooks.md ................ A  (the Claude Code hooks contract)
├── guides/install.md ................. M  (hooks table, pointer to reference/hooks.md)
└── guides/contributing.md ............ M  (hooks sentence)
CLAUDE.md ............................. M  (`atomic hooks` vs git hooks paragraph; surfaces table row)
```


## Outline


```
atomic/internal/hooks/reviewgate.go
  PostToolUse — parse payload, skip subagent calls, append a touched or reviewed event
  Stop — parse payload, honor stop_hook_active, fold state, compute the gated set, return block message or nothing
  gateState — folded view of a session's events: touched set and last review fingerprint
    appendEvent — one JSON line, O_APPEND, 0600, directory created on first write
    foldState — read the file, absent is empty, skip unparseable lines
  statePath — <os temp dir>/atomic-hooks/<session_id>.jsonl, rejects unsafe ids
  repoRoot — git rev-parse --show-toplevel at cwd, symlinks resolved
  isDocsPath — the signals-gate documentation test on a repo-relative path
  gatedPaths — the one set both fingerprints cover: under the root, dirty (porcelain -z), not documentation
  fingerprint — hash of git diff HEAD over the paths plus untracked file bytes, run at the root
  blockMessage — stderr text naming the paths and the gate

atomic/internal/hooks/reviewgate_test.go
  subagent tool call is ignored — agent_type present writes no state
  edit records path — Edit/Write/MultiEdit/NotebookEdit file_path lands in state
  review records fingerprint — Agent with atomic-reviewer or atomic-auditor
  other Agent types are not reviews — Explore does not close the gate
  review and stop cover one gated set — docs and source touched, review, pass; edit source, block
  docs paths exempt — docs/ at depth, root README*, LICENSE*; artifact .md is source
  path outside the repo is dropped — a touched file under $HOME does not disable the gate
  session cwd in a subdirectory — the gate still re-opens after a post-review edit
  concurrent events survive — parallel appends all fold into touched
  stop blocks unreviewed dirty edit — exit 2 message names the path
  stop passes after review — same fingerprint
  stop blocks again after post-review edit — fingerprint moved
  stop passes after commit — nothing dirty
  stop passes on stop_hook_active, missing state, no git, malformed input

atomic/internal/hooks/hooks.go
  Install — register SessionStart, PostToolUse, Stop
  Uninstall — unregister all three
  IsInstalled — drifted when a gate hook is missing
  hasRegistration — event-aware lookup

atomic/internal/hooks/hooks_hujson.go
  registerInSettings — takes event and matcher
  unregisterFromSettings — takes event
  astRegister / astUnregister — event-keyed array; matcher omitted when empty

atomic/internal/hooks/hooks_test.go
  install registers three entries — SessionStart, PostToolUse, Stop with their matchers
  install adds only the missing entries — a session-start-only settings.json gains two
  uninstall removes all three, keeps unrelated registrations
  IsInstalled drifts on a missing gate hook

atomic/cmd/atomic/cmd_hooks.go
  post-tool-use — stdin to hooks.PostToolUse; always exit 0
  stop — stdin to hooks.Stop; exit 2 with the message on stderr when non-empty, else 0
  usage strings — name the five verbs

atomic/cmd/atomic/cmd_claude.go
  --no-hooks help and install message — name the Claude Code hooks

atomic/internal/doctor/fix.go, fix_test.go, format_test.go, format_verbose_test.go
  hook detail strings — match checks_hooks.go

atomic/cmd/atomic/main_test.go
  wantCobraSubcommandMeta — rows for post-tool-use and stop

atomic/internal/cliusage/cliusage.go
  hooks post-tool-use, hooks stop — entries with Short text matching the Cobra tree

atomic/internal/doctor/checks_hooks.go
  RunCheckHooksWith — drift detail names the missing gate hooks and the install command

atomic/internal/doctor/checks_hooks_test.go
  missing gate hook warns — session-start-only settings.json is drift

context/skills/atomic-verify/SKILL.md
  Claim → required check — "code I wrote myself is ready" row dispatches atomic-auditor, diff: working
  Verification discipline — ad-hoc bullet: auditor on the session's model, completion line, stop hook backstop

context/agents/atomic-auditor.md
  description — once-per-loop gate and working-diff gate for ad-hoc main-agent code
  opening framing — one clause admitting the working tree as the whole when no loop ran
  Scope boundaries — checkpoint diff of a loop stays out of scope; the red-suite refusal is loop mode's
  Caller-provided context — diff: working, intent:
  The four passes — working-diff notes: intent as the bar, run verification, red suite is 🔴, commit pass nothing found
  Rules — one dispatch per loop; one dispatch per gate event in working-diff mode

context/agents/atomic-reviewer.md
  description — drops the ship-verb gate clause

context/_partials/review-gate.md
  Already-reviewed guard — atomic-verify audit this session
  Dispatch — atomic-auditor, diff: working, on the working tree

context/CLAUDE.md
  Workflow — ad-hoc gate sentence names the auditor and the stop hook

context/commands/atomic-help.md
  review row — auditor gates ad-hoc main-agent code at both exits
  agents row — auditor's two uses
  Stage 4 — atomic hooks install|uninstall line

docs/spec/atomic-auditor.md
  Non-goals — qualified diff-reviewer line; one dispatch per loop
  Change log — 2026-09-20 entry

docs/reference/skills.md
  intro paragraph and atomic-verify row — auditor instead of reviewer, stop hook backstop

docs/reference/agents.md
  atomic-auditor row — working-diff use
  atomic-reviewer row — no ship-verb gate claim

docs/reference/workflow.md
  review gate paragraphs — auditor, working tree, already-audited guard

docs/reference/commands.md
  /commit row — auditor gate before the commit lands

docs/reference/hooks.md
  What it is — the three registrations and what each does
  The review gate — state file, gated set, documentation exemption, what closes the gate, stop_hook_active, exit 2
  Install, opt-out, doctor — atomic hooks install|uninstall, --no-hooks, hand edit for a subset, doctor drift

docs/guides/install.md
  hooks bullet — three-row table (hook, command, what it does), one sentence for opt-out and doctor, pointer to reference/hooks.md

docs/guides/contributing.md
  hooks sentence — session-start plus review-gate hooks

CLAUDE.md
  atomic hooks vs git hooks — names the three Claude Code hooks
  Documentation surfaces — docs/reference/hooks.md row
```


## Flows


**Flow: main agent edits, then claims done**

1. Main agent calls `Edit` on `src/auth.go`; the harness runs `atomic hooks post-tool-use`, which records the path under the session.
2. Main agent runs tests and writes "done"; the harness runs `atomic hooks stop`.
3. The path is dirty, not documentation, and no review fingerprint matches, so the hook exits 2: "1 file you edited this session is uncommitted and unreviewed: src/auth.go. Run the atomic-verify review gate: dispatch atomic-auditor with diff: working, then report its verdict and model."
4. Claude dispatches `atomic-auditor` (`diff: working`, `intent:` the task); PostToolUse records the fingerprint of the dirty set.
5. Claude reports `audit: PASS (atomic-auditor on <model>)` and stops; `stop_hook_active` is true on the second stop, so the hook exits 0.

**Flow: review, fix, stop**

1. Auditor returns `CHANGES_REQUESTED`; Claude edits `src/auth.go` again (recorded).
2. Claude stops; the fingerprint has moved, the hook blocks once; Claude dispatches the auditor again (a new gate event, its own dispatch) or says what is still open and continues.

**Flow: loop-produced code**

1. `/subagent-implementation` dispatches `atomic-implementer`; its `Edit` calls arrive with `agent_type`, so nothing is recorded.
2. The orchestrator dispatches `atomic-reviewer`; a fingerprint over an empty set is recorded.
3. The orchestrator commits; the stop hook finds no recorded dirty path and exits 0.

**Flow: ship verb on ad-hoc code**

1. Main agent edits `src/auth.go` and runs `/commit`.
2. The review gate finds no audit this session, dispatches `atomic-auditor` (`diff: working`) on the working tree; PostToolUse records the fingerprint.
3. The commit lands; nothing is dirty; the stop hook exits 0.

**Flow: install and doctor**

1. `atomic hooks install` (or `atomic claude install|update`) registers the three entries, adding only those missing.
2. `atomic doctor` reads `IsInstalled`: session-start present with a gate hook missing reports drift and `--fix` re-runs install.


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Hook logic: state, docs test, fingerprint, PostToolUse, Stop, fail-open | `atomic/internal/hooks/reviewgate.go`, `reviewgate_test.go` | atomic-implementer (mode: feature) | 2 | `go test ./internal/hooks/...` green with the ten outline cases |
| 2 | Registration and verbs: three-entry install/uninstall/drift, `post-tool-use` and `stop` verbs, cliusage rows, doctor drift detail | `hooks.go`, `hooks_hujson.go`, `hooks_test.go`, `cmd_hooks.go`, `main_test.go`, `cliusage.go`, `checks_hooks.go`, `checks_hooks_test.go` | atomic-implementer (mode: feature) | ~8 | `go test ./...`, `go vet ./...`, `gofmt -l .` empty; `bin/atomic hooks install` against a sandboxed HOME writes three entries |
| 3 | Artifacts: skill, auditor, reviewer, review-gate partial, CLAUDE.md, help router; auditor spec amendment | `context/skills/atomic-verify/SKILL.md`, `context/agents/atomic-auditor.md`, `context/agents/atomic-reviewer.md`, `context/_partials/review-gate.md`, `context/CLAUDE.md`, `context/commands/atomic-help.md`, `docs/spec/atomic-auditor.md` | atomic-implementer (mode: feature) | 7 | `make -C atomic bundle` clean; `atomic validate spec`, `config`, `artifacts` zero FAIL; help-coverage loop zero `MISSING:` |
| 4 | Reference docs and guides | `docs/reference/skills.md`, `docs/reference/agents.md`, `docs/reference/workflow.md`, `docs/guides/install.md`, `docs/guides/contributing.md`, `CLAUDE.md` | atomic-implementer (mode: feature) | 6 | `atomic validate artifacts` zero FAIL; every `atomic hooks` citation resolves |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| The hook blocks a turn that was not a completion claim | med | `stop_hook_active` caps it at one nudge per turn; the message tells Claude to say what is in progress and continue |
| Hook error leaves a session unable to stop | low | Every error path exits 0; only the one computed block exits 2 |
| A user with `--no-hooks` or a managed settings.json gets no backstop | med | The prose gate still fires as today; `atomic doctor` reports the missing hooks |
| Tool names or input fields drift in Claude Code | low | Unknown tool names are ignored; the hook fails open |
| `git status` and `git diff` per stop on a large repo | low | Both are path-scoped to the recorded set, which is small |
| The documentation-path list drifts between `isDocsPath` and its prose copies (`signals-gate.md`, `review-gate.md`, the skill) | med | The prose copies cite the signals gate as the one list and the Go test pins the same six prefixes; a change to either side edits the other in the same commit |
| Edits made through `Bash` (`sed -i`, redirection) never reach the `PostToolUse` matcher | med | `context/CLAUDE.md` routes edits to the Edit tool; the prose gate in `atomic-verify` still applies; the hook is a backstop, not the gate |
| Parallel tool calls in one message run concurrent `post-tool-use` processes | med | Append-only state: one line per event, folded at stop; no read-modify-write |
| A recorded path outside the repository (profile, memory, another repo) | high | Dropped after symlink resolution against the repo root; git never sees it |


## Change log

### 2026-09-20 — Audit findings: repo-root git, append-only state, no error returns, hooks reference page

**What changed:** Every git call runs at the repository root resolved from the payload's `cwd`, with symlinks resolved and out-of-root paths dropped before git sees them; porcelain output is read NUL-separated. State is an append-only `.jsonl` folded at stop. `PostToolUse` and `Stop` return no error. The auditor's working-diff mode runs verification itself and reports a red suite as a finding. `docs/reference/hooks.md` carries the hooks contract; `docs/reference/commands.md` and the install guide's hooks table are in scope. Non-goals record the absent per-hook opt-out and the `Bash`-edit blind spot.

**Why:** The finalize audit showed one out-of-repo touched path aborting `git status` for the whole set (gate fails open for the session), a subdirectory `cwd` making both fingerprints hash to empty (gate never re-opens), and a read-modify-write state file losing events under parallel tool calls.

**Superseded:** state as a single JSON object rewritten per event; git run at `cwd`; `PostToolUse(...) error` and `Stop(...) (string, error)`.

### 2026-09-20 — Correction: one gated set for both fingerprints

**What changed:** The review fingerprint recorded by `atomic hooks post-tool-use` is taken over the same filtered set `atomic hooks stop` fingerprints (in-repo, dirty, not documentation), not over every recorded path.

**Why:** Checkpoint 1's implementer built the two fingerprints from the literal wording and showed that a session touching a documentation file and a source file before a review would produce two inputs that never match, so every later stop re-blocks on the source file alone.


## Implementation log

### shipped — 2026-09-20

Built across 6 iterations of /autopilot (fresh-context implementer and reviewer per checkpoint, one audit). Commits (chronological):

- `c3ff2924` — CP-1 hook logic: PostToolUse, Stop, state, docs test, fingerprint
- `ccd4f2b2` — CP-2 three registrations, post-tool-use and stop verbs, cliusage, doctor drift
- `4b50e1ab` — CP-3 atomic-verify, auditor working-diff mode, reviewer description, review-gate partial, CLAUDE.md, help router
- `ea21ae12` — CP-1 follow-on: singular grammar in the block message
- `68b57076` — CP-4 reference docs and guides
- `cbd63c73` — audit round: auditor loop-mode refusals, docs/reference/hooks.md, /commit row, install table
- `e8c56c49` — audit round: git at the repo root, out-of-root paths dropped, append-only state, -z porcelain

**Out-of-scope work performed during this build:**

- `docs/reference/hooks.md` and its surfaces-table row: the audit found no page owned the hooks contract once three hooks existed.

**Unforeseens — surprises that emerged during implementation:**

- Review and stop fingerprints diverged when a docs file and a source file were touched before a review; fixed with one gated set (change log, Correction entry).
- One touched path outside the repo aborted `git status` for the whole set and a subdirectory `cwd` hashed both fingerprints to empty; fixed by running git at the resolved root (change log, Audit findings entry).

**Deferred items still open:**

- `review-gate-hook-f-1` (question): per-hook opt-out; recorded as a Non-goal.

# Harness capability matrix


**Status:** CP0 evidence recorded 2026-09-15 for Claude Code `2.1.273`, OMP `18.1.18`, and Codex CLI `0.147.0` on macOS arm64.

This note is the compatibility boundary for later adapters. A row is `supported` only when the retained runtime observation proves the behavior for the named version. Documentation and accepted configuration syntax identify candidates, but neither counts as runtime proof. `unsupported` includes behavior that is absent, unsafe to infer, blocked before the relevant event, or not exercised in this checkpoint.

Raw evidence is retained under `.claude/.scratchpad/omp-plugin-compatibility/capabilities/`; every evidence path in this note is relative to that directory. `setup/setup-probes.sh` reconstructs the complete relocatable Claude, OMP, and Codex input tree, writes `probe-commands.sh` — the replay dispatcher — and writes `replay-manifest.json`, one row per supported or partial claim with its claim ID, dispatcher target, cwd, outputs, prerequisites, and expected terminal condition. `setup/run-omp-deny-probe.sh` is the bounded side-effect deny runner. Probe workspaces and homes were isolated where the harness allowed it; authentication stores and model databases are not present in the retained tree.


## Replay inventory


Every supported or partial claim below is owned by exactly one replay target. A target name is a dispatcher target of the generated `probe-commands.sh`, except `run-omp-deny-probe.sh`, which is the separate bounded runner. `replay-manifest.json` is the machine-readable source: rows are keyed by claim ID, and each row repeats the target, cwd, outputs, prerequisites, and expected terminal condition.

| Replay target | cwd (relative to the generated root) | Owns claims |
|---|---|---|
| `versions` | `.` | `harness.versions` |
| `claude-primary` | `claude/repo` | `claude.native-global-root`, `claude.session-baseline`, `claude.role.session-baseline-event`, `claude.steering-precedence`, `claude.failure`, `claude.visibility`, `claude.cleanup`, `claude.registration`, `claude.path-scope-absence` |
| `claude-nested` | `claude/repo/nested` | `claude.nested-steering` |
| `claude-resume` | `claude/repo` | `claude.resume` |
| `claude-safe-mode` | `claude/repo` | `claude.disablement` |
| `omp-config-path` | `omp/repo` | `omp.native-global-root` |
| `omp-plugin-list` | `omp/repo` | `omp.installed-plugin-registry` |
| `omp-root` | `omp/repo` | `omp.registration`, `omp.extension-discovery`, `omp.session-baseline`, `omp.role.session-baseline-event`, `omp.steering-precedence`, `omp.visibility`, `omp.cleanup` |
| `omp-nested` | `omp/repo/nested` | `omp.steering-precedence-nested` |
| `omp-scope` | `omp/repo` | `omp.static-scope-matching`, `omp.context-return-absence` |
| `omp-tools` | `omp/repo` | `omp.pre-operation-event`, `omp.structured-targets` |
| `omp-session-create` | `omp/repo` | `omp.session-create` |
| `omp-resume` | `omp/repo` | `omp.resume` |
| `omp-child` | `omp/repo` | `omp.child` |
| `omp-failure` | `omp/repo` | `omp.failure` |
| `omp-no-rules` | `omp/repo` | `omp.disablement` |
| `omp-dedupe` | `omp/repo` | `omp.dedupe` |
| `omp-order` | `omp/repo` | `omp.ordering`, `omp.concurrency` |
| `run-omp-deny-probe.sh` | `omp/repo` | `omp.deny`, `omp.role.deterministic-deny` |
| `codex-registration` | `codex/repo` | `codex.registration`, `codex.visibility` |
| `codex-runtime` | `codex/repo` | `codex.native-global-root`, `codex.failure`, `codex.hook-absence` |

Every target pins its own working directory and closes stdin. The default capture is `<target>.stdout`, `<target>.stderr`, and `<target>.status` under the generated `output/` directory, but four targets name their outputs differently: `versions` writes one `versions-<harness>.{stdout,stderr,status}` triple per harness plus `version-preflight.txt`; `codex-registration` writes a named triple per command (`codex-marketplace-add`, `codex-marketplace-list`, `codex-plugin-list-before-add`, `codex-plugin-add`, `codex-plugin-list-after-add`); `omp-order` writes `omp-handler-order.{stdout,stderr,status}` plus `omp-order.jsonl`; and `run-omp-deny-probe.sh`, the separate bounded runner, writes no `<target>.*` triple at all — its captures are `<name>.txt` files and `.jsonl` event streams under `output/runtime-deny-side-effect/`. Several targets also write an event stream, a session id, or a verdict beside their triple (`<...>-events-*.jsonl`, `claude-session-id.txt`, `omp-session-id.txt`, `codex-runtime.verdict.txt`). Nothing about a target upgrades a capability: a target proves the recorded command can be re-run. `unsupported` rows that have a target are absence evidence, and they stay unsupported.

**Replay assumptions (they bound every row above).**

- Platform. Every row was observed on darwin arm64 with the tested binaries; Linux and Windows are untested.
- Binary. `versions` must report `2.1.273`, `18.1.18`, and `0.147.0`. The dispatcher preflights those three on every target and exits `70` on a mismatch unless `ALLOW_VERSION_MISMATCH=1` is set, in which case it prints a loud warning and the evidence no longer matches the recorded versions.
- Auth and network. OMP needs a provider credential resolved at runtime from `CP0_OMP_AUTH_HOME` or `CP0_OMP_API_KEY`; only a transient token is passed and nothing is retained. Codex uses the ambient ChatGPT account credential outside `CODEX_HOME`; no credential file is copied into the replay root. Claude runs with an isolated `CLAUDE_CONFIG_DIR` and no credential, so its model requests are expected to fail with `Not logged in`.
- Resume. Resume targets consume the session ID their create target wrote into the same fresh root (`output/claude-session-id.txt`, `output/omp-session-id.txt`). No session ID is hard-coded, and a stale ID is not a valid replay input.
- Fresh root. The generator refuses an existing destination. Registration and extension-discovery rows assume an empty isolated home, so a reused root invalidates the before-add registry state and the discovery and dedupe counts.
- Status scope. A `replay-manifest.json` row's `status` describes replay reproducibility — whether the recorded command re-runs and reaches its expected terminal condition — not capability support. `codex.failure` is `partial` because its target reproduces the expected model rejection; the Codex failure-semantics capability row stays `unsupported`.

**Claims with no replay target.** `reload`, `user overrides`, and `numeric limits` on all three harnesses, OMP extension disablement (`--no-extensions`), OMP package install/lifecycle/scope, and every Codex runtime hook surface beyond thread creation were not exercised, so no target exists for them. They are unsupported and are not covered by any manifest row.


## Tested binaries and sources


| Harness | Binary | Observed version | Native isolation used | Evidence |
|---|---|---:|---|---|
| Claude Code | `/Users/alonso/.local/bin/claude` | `2.1.273 (Claude Code)` | Isolated `CLAUDE_CONFIG_DIR`; isolated settings, project history, hooks, and instruction files. `HOME` remained `/Users/alonso`, but every model request ended with `Not logged in`. | `harness-versions.json`, `claude/runtime-primary-success.json`. Replay target: `versions` |
| OMP | `/Users/alonso/.bun/bin/omp` | `omp/18.1.18` | Isolated `HOME`, therefore isolated `~/.omp`; no authentication or model database is retained. | `harness-versions.json`, `omp/version.json`, `omp/config-path.json`. Replay target: `versions` |
| Codex CLI | `/Users/alonso/.local/bin/codex` | `codex-cli 0.147.0` | Isolated `HOME` and `CODEX_HOME`; no `auth.json` is retained. | `harness-versions.json`, `codex/version.json`. Replay target: `versions` |

Official references read to construct the probes are listed in `official-sources.json`. They remain documentation-only evidence.

Probe wrappers use the local date `2026-09-15`; embedded UTC timestamps after midnight are `2026-09-16`. This is one probe window, not two evidence dates.

The retained tree contains no authentication file, credential database, reusable token, API key, cookie, private key, or harness/provider account identifier. A structured-key and value-pattern audit found only Codex's non-secret manifest policy value `authentication: ON_INSTALL`; token-signature and private-key scans found no reusable authentication material to redact. Absolute local paths remain because they establish isolation, command identity, and resume identity; those paths disclose the local OS username `alonso`. Opaque runtime and session IDs also remain and do not grant access.


## Required rule-delivery roles


| Capability role | Claude Code 2.1.273 | OMP 18.1.18 | Codex CLI 0.147.0 |
|---|---|---|---|
| Static scoped-rule surface | **Unsupported for CP0 parity.** Unconditional user and project rules were discovered at startup, but the isolated account could not issue a model request, so path matching, lazy delivery, resumed delivery, and child delivery were not observed. Do not claim native scope from accepted `paths:` syntax. Replay target: `claude-primary` (absence). | **Unsupported for matched-body delivery.** A native rule with `alwaysApply: true` entered the opening system prompt. A rule with `paths: ["nested/**/*.ts"]` did not appear in either provider request before or after `read` accessed `nested/target.ts`. Replay target: `omp-scope` (absence). | **Unsupported.** No runtime instruction-chain observation completed. The documented `AGENTS.md` chain and 32 KiB default remain candidates only. Replay target: `codex-runtime` (absence). |
| Session-baseline event | **Partial.** `SessionStart` ran for startup and resume, with `source`, `session_id`, `cwd`, and `transcript_path`. The hook returned `additionalContext`, but authentication failed before a model request could consume it, so context delivery is unsupported. Inputs and commands: `setup/setup-probes.sh`. Replay target: `claude-primary`. | **Supported through `before_agent_start`.** `session_start` exposed the built system prompt, cwd, tools, and commands; no return behavior was exercised for that event. `before_agent_start` received `prompt` and `systemPrompt`; returning a replacement `systemPrompt` placed `OMP_BEFORE_AGENT_CONTEXT_MARKER` in the immediate provider payload. Inputs and commands: `setup/setup-probes.sh`. Replay target: `omp-root`. | **Unsupported.** The runtime attempt reached `thread.started` but failed model validation before hook execution. Replay target: `codex-runtime` (absence). |
| Pre-operation event with structured targets | **Unsupported.** `PreToolUse` was configured but no tool call was reached. Replay target: `claude-primary` (absence). | **Supported for mediated tools.** `tool_call` ran before `read` and `bash`, with `toolName`, `toolCallId`, and structured `input`. `read.input.path` was exactly `nested/target.ts`; the side-effect probe's `bash.input.command` exactly matched `omp/runtime-deny-side-effect/blocked-command.txt`. Coverage beyond the exercised built-ins is unsupported. Inputs and commands: `setup/setup-probes.sh`, `setup/run-omp-deny-probe.sh`. Replay target: `omp-tools`. | **Unsupported.** No hook event ran. The documented `PreToolUse` payload and local-tool inventory are not runtime proof for this version. Replay target: `codex-runtime` (absence). |
| Context return before the current operation | **Unsupported.** No `PreToolUse` return was exercised. Replay target: `claude-primary` (absence). | **Unsupported.** No context-return result was exercised or observed. The scoped-rule marker remained absent from the provider request after `read` accessed the probe's intended target. `before_agent_start` can add baseline context only before the model chooses an operation. Replay target: `omp-scope` (absence). | **Unsupported.** The documented `additionalContext` result was not exercised. Replay target: `codex-runtime` (absence). |
| Deterministic deny | **Unsupported.** A deny hook was configured but no tool call was reached. Replay target: `claude-primary` (absence). | **Supported only for the exercised exact `bash` call.** `tool_call` returned `{ "block": true, "reason": "OMP_CP0_SIDE_EFFECT_DENY_MARKER" }`; OMP produced an error tool result with that reason, exited `0`, and the explicit post-run check found the unique file named by the command absent. This proves that one intercepted command did not execute; it does not prove coverage for other commands or tools. Inputs and command: `setup/setup-probes.sh`, `setup/run-omp-deny-probe.sh`. Replay target: `run-omp-deny-probe.sh`. | **Unsupported.** The documented `permissionDecision: "deny"` result was not exercised. Replay target: `codex-runtime` (absence). |

Evidence: `claude/runtime-summary.json`, `claude/runtime-events-success.jsonl`, `claude/runtime-events-resume.jsonl`, `omp/runtime-aggregate.json`, `omp/runtime-events-scope.jsonl`, `omp/runtime-events-tools.jsonl`, `omp/runtime-tools-deny.json`, `omp/runtime-deny-side-effect/blocked-command.txt`, `omp/runtime-deny-side-effect/hook-events.jsonl`, `omp/runtime-deny-side-effect/runtime-tool-events.jsonl`, `omp/runtime-deny-side-effect/exit-status.txt`, `omp/runtime-deny-side-effect/wall-seconds.txt`, `omp/runtime-deny-side-effect/post-run-absence-check.txt`, `fixtures/omp-repo/.omp/rules/always.md`, `fixtures/omp-repo/.omp/rules/scoped.md`, `setup/setup-probes.sh`, and `setup/run-omp-deny-probe.sh`. Replay targets for this table: `claude-primary`, `omp-root`, `omp-scope`, `omp-tools`, `run-omp-deny-probe.sh`, and `codex-runtime`.

The practical result is narrow: OMP can enforce a machine predicate on an intercepted tool input, but OMP `18.1.18` cannot be used to claim per-operation matched rule-body injection. Claude and Codex rule delivery remain unsupported until their model/tool paths are exercised successfully.


## Claude Code 2.1.273


### Native roots and discovery


| Observation | Result | Evidence |
|---|---|---|
| Custom configuration root | `CLAUDE_CONFIG_DIR` named the isolated root used by settings, instruction/rule paths, transcript paths, and the backup diagnostic. | `setup/setup-probes.sh`, `claude/settings.json`, `claude/runtime-events-success.jsonl`, `claude/runtime-primary-config-dir.json`. Replay target: `claude-primary` |
| Global steering | `<CLAUDE_CONFIG_DIR>/CLAUDE.md` loaded at startup. Its relative `@global-import.md` resolved relative to the importing file. | `setup/setup-probes.sh`, `claude/runtime-events-success.jsonl`. Replay target: `claude-primary` |
| Repository steering | Repository-root `CLAUDE.md` loaded at startup. Its relative `@AGENTS.md` import resolved and emitted a separate `InstructionsLoaded` event with `parent_file_path`. | `setup/setup-probes.sh`, `claude/runtime-events-success.jsonl`. Replay target: `claude-primary` |
| Nested steering | Starting in `repo/nested` loaded root `CLAUDE.md` followed by `nested/CLAUDE.md`. Starting at the root did not load the nested file before any tool access. | `setup/setup-probes.sh`, `claude/runtime-events-success.jsonl`, `claude/runtime-events-nested.jsonl`. Replay targets: `claude-primary`, `claude-nested` |
| Rules | User `rules/user.md` and unconditional project `.claude/rules/always.md` loaded at startup. The path-scoped project rule did not load at startup; on-demand behavior was not reached. | `setup/setup-probes.sh`, `claude/runtime-events-success.jsonl`, `claude/runtime-events-nested.jsonl`. Replay target: `claude-primary` |
| Discovery visibility | `InstructionsLoaded` exposed `file_path`, `memory_type`, `load_reason`, and, for imports, `parent_file_path`. | `setup/setup-probes.sh`, `claude/runtime-events-success.jsonl`. Replay target: `claude-primary` |

The observed startup ordering was global import, user rule, repository import, global `CLAUDE.md`, repository `CLAUDE.md`, and unconditional project rule in the root run. Hook callback ordering is asynchronous enough that event line order must not be treated as the final prompt concatenation order. The nested run proves membership, not a byte-level concatenated prompt.


### Session, resume, children, and hooks


| Dimension | Result | Evidence |
|---|---|---|
| New session | `SessionStart.source` was `startup`; `SessionEnd.reason` was `other` after the API error. | `setup/setup-probes.sh`, `claude/runtime-events-success.jsonl`. Replay target: `claude-primary` |
| Resume | Resuming the exact session ID reused that ID, emitted `SessionStart.source = "resume"`, and rediscovered the global, imported, root, and unconditional rule files. | `setup/setup-probes.sh`, `claude/runtime-resume.json`, `claude/runtime-events-resume.jsonl`. Replay target: `claude-resume` (session ID taken from `claude-primary`) |
| Child session | Unsupported. No `Task` call was reached. | `claude/runtime-primary-success.json`. Replay target: `claude-primary` (absence) |
| Pre-operation path and deny | Unsupported. No tool call was reached. | `claude/runtime-primary-success.json`. Replay target: `claude-primary` (absence) |
| Context return | Unsupported. `SessionStart.additionalContext` was returned by the hook, but no authenticated model request consumed it. | `claude/hook.py`, `claude/runtime-primary-success.json`. Replay target: `claude-primary` (absence) |
| Failure behavior | Authentication failure ended the print run with exit `1`, `terminal_reason: "api_error"`, and `result: "Not logged in · Please run /login"`; startup and end hooks still ran. Hook failure semantics were not tested. | `setup/setup-probes.sh`, `claude/runtime-primary-success.json`, `claude/runtime-events-success.jsonl`. Replay target: `claude-primary` |
| Disablement | `--safe-mode` produced no hook log file, proving the configured user hooks were disabled for that invocation. Instruction disablement was not independently observable. | `setup/setup-probes.sh`, `claude/runtime-safe-mode.json`, `claude/runtime-summary.json`. Replay target: `claude-safe-mode` |
| Reload, dedupe, concurrent hooks | Unsupported; not exercised. | — (no replay target) |
| Numeric limits | Unsupported. The documented recommendations and glob-expansion bounds were not runtime boundary-tested. | `official-sources.json` (no replay target) |

No adapter may infer tool-hook parity, child delivery, path-scoped delivery, or a hard instruction-size limit from this Claude run.


## OMP 18.1.18


### Native roots, steering, and precedence


The isolated `omp config path --json` result was `<isolated-home>/.omp/agent`. The runtime automatically discovered `~/.omp/agent/extensions/cp0-probe.ts`, `AGENTS.md`, `RULES.md`, and user rules beneath that agent root. Replay target for the root path: `omp-config-path`.

| Launch scope | Delivered markers | Shadowed or absent markers | Conclusion | Evidence |
|---|---|---|---|---|
| Repository root | User `AGENTS.md`, root `.omp/AGENTS.md`, user `RULES.md`, root `.omp/RULES.md`, and the `alwaysApply` rule | Standalone root `AGENTS.md`, nested `.omp/AGENTS.md`, scoped rule | Native `.omp/AGENTS.md` won at the root depth. Both tested sticky files and the unconditional rule were present. | `setup/setup-probes.sh`, `omp/runtime-summary.json`, `omp/runtime-aggregate.json`, `omp/runtime-events-stdin-closed.jsonl`. Replay target: `omp-root` |
| Nested directory | User `AGENTS.md`, standalone root `AGENTS.md`, nested `.omp/AGENTS.md`, user `RULES.md` | Root `.omp/AGENTS.md`, root `.omp/RULES.md`, root rules directory, scoped rule | The nearest non-empty nested `.omp` replaced the farther native project root; standalone guidance at another depth survived. | `setup/setup-probes.sh`, `omp/runtime-nested-startup.json`, `omp/runtime-events-nested.jsonl`, `omp/runtime-aggregate.json`. Replay target: `omp-nested` |

This is a discovery result, not a semantic override guarantee. No conflicting guidance was presented to the model.


### Extension registration and visibility


| Observation | Result | Evidence |
|---|---|---|
| Automatic extension discovery | The factory in `~/.omp/agent/extensions/cp0-probe.ts` ran once and registered `cp0-command`. | `setup/setup-probes.sh`, `omp/runtime-events-stdin-closed.jsonl`, `omp/runtime-summary.json`. Replay target: `omp-root` |
| Explicit plus automatic duplicate | Adding the same resolved file through `--extension` while it remained auto-discoverable still ran the factory once. | `setup/setup-probes.sh`, `omp/runtime-extension-dedupe.json`, `omp/runtime-events-dedupe.jsonl`. Replay target: `omp-dedupe` |
| Command visibility | `pi.getCommands()` included `{ name: "cp0-command", description: "CP0 explicit command probe" }`. | `setup/setup-probes.sh`, `omp/runtime-summary.json`. Replay target: `omp-root` |
| Tool visibility | `pi.getAllTools()` exposed `read`, `bash`, and `task` with built-in source metadata. | `setup/setup-probes.sh`, `omp/runtime-summary.json`. Replay target: `omp-root` |
| Installed-plugin registry | The isolated `omp plugin list --json` returned empty `npm` and `marketplace` arrays. No package installation was performed. | `setup/setup-probes.sh`, `omp/plugin-list.json`. Replay target: `omp-plugin-list` |

Package install, shared-package visibility, project installation scope, uninstall, and cleanup are unsupported in CP0 because no OMP package was registered.


### Events, paths, context, and deny


| Event/API | Observed payload or return | Adapter status | Evidence |
|---|---|---|---|
| `session_start` | Event `{ type: "session_start" }`; context exposed cwd and the built system prompt. The probe also enumerated commands and tools through the API. | Supported for observation only; no return behavior was exercised. | `setup/setup-probes.sh`, `omp/runtime-events-stdin-closed.jsonl`. Replay target: `omp-root` |
| `before_agent_start` | Received prompt and system-prompt array. Returning a new system-prompt array added `OMP_BEFORE_AGENT_CONTEXT_MARKER` to the next provider request. | Supported for session/turn baseline context; numeric limits are unsupported. | `setup/setup-probes.sh`, `omp/runtime-events-scope.jsonl`. Replay target: `omp-root` |
| `before_provider_request` | The probe observed the outgoing payload immediately before each provider request. Baseline/user markers were present. | Supported for observation. Mutation was not tested. | `setup/setup-probes.sh`, `omp/runtime-events-scope.jsonl`. Replay target: `omp-scope` |
| `context` | Exposed the message array before provider requests. | Supported for observation. Message replacement was not tested. | `setup/setup-probes.sh`, `omp/runtime-events-tools.jsonl`. Replay target: `omp-tools` |
| `tool_call` | `read` carried `{ path: "nested/target.ts" }`; the side-effect probe's `bash` input exactly matched `omp/runtime-deny-side-effect/blocked-command.txt`. | Supported pre-operation interception for the exercised built-ins only. | `setup/setup-probes.sh`, `setup/run-omp-deny-probe.sh`, `omp/runtime-events-tools.jsonl`, `omp/runtime-deny-side-effect/hook-events.jsonl`. Replay Targets: `omp-tools`, `run-omp-deny-probe.sh` |
| `tool_call` deny | The exact side-effecting command produced `{ block: true, reason: "OMP_CP0_SIDE_EFFECT_DENY_MARKER" }`, an error tool result, exit status `0`, and an explicit absent-file result. | Supported only for that exact exercised `bash` call. | `setup/setup-probes.sh`, `setup/run-omp-deny-probe.sh`, `omp/runtime-deny-side-effect/hook-events.jsonl`, `omp/runtime-deny-side-effect/runtime-tool-events.jsonl`, `omp/runtime-deny-side-effect/exit-status.txt`, `omp/runtime-deny-side-effect/post-run-absence-check.txt`. Replay target: `run-omp-deny-probe.sh` |
| `tool_result` | Successful `read` exposed input, content, resolved source path in details, and `isError: false`. The denied `bash` surfaced an error result to the runtime but produced no normal `tool_result` extension event. | Post-operation observation only. | `setup/setup-probes.sh`, `setup/run-omp-deny-probe.sh`, `omp/runtime-events-tools.jsonl`, `omp/runtime-deny-side-effect/runtime-tool-events.jsonl`. Replay targets: `omp-tools`, `run-omp-deny-probe.sh` |
| Pre-operation context return | No context-return result was exercised or observed. The scoped marker remained absent after the read of the probe's intended target. | Unsupported. | `omp/runtime-events-scope.jsonl`. Replay target: `omp-scope` (absence) |

Shell commands remain unsafe scope inputs. The structured `read.input.path` is usable. Edit/write/apply-patch schemas were not exercised and remain unsupported until separately observed.


### Lifetime, child, ordering, and failure


| Dimension | Result | Evidence |
|---|---|---|
| Resume | A saved session resumed by ID and retained the prior `SESSION_ONE` transcript before producing `SESSION_TWO`. The extension factory and `session_start` ran again in the new CLI process. No distinct resume source field was present in the event. | `setup/setup-probes.sh`, `omp/runtime-session-create.json`, `omp/runtime-resume.json`, `omp/runtime-events-resume.jsonl`. Replay targets: `omp-session-create`, `omp-resume` (session ID taken from `omp-session-create`) |
| Child | A `task` call created a second extension factory and second `session_start` in the same process. The child system prompt retained the user/project sticky and unconditional rule markers, but the user/root context markers were absent. The child returned `CHILD_OK`. | `setup/setup-probes.sh`, `omp/runtime-child.json`, `omp/runtime-events-child.jsonl`, `omp/runtime-aggregate.json`. Replay target: `omp-child` |
| Dedupe | One factory invocation when the same resolved extension path was both automatic and explicit. | `setup/setup-probes.sh`, `omp/runtime-extension-dedupe.json`, `omp/runtime-events-dedupe.jsonl`. Replay target: `omp-dedupe` |
| Handler ordering | Two explicit `tool_call` handlers ran sequentially in registration order: A start, A end, B start, B end. | `setup/setup-probes.sh`, `omp/runtime-handler-order.json`, `omp/runtime-order.jsonl`. Replay target: `omp-order` |
| Handler concurrency | Sibling handlers for one event were not concurrent in the observed run. Cross-tool and cross-session concurrency remains unsupported. | `setup/setup-probes.sh`, `omp/runtime-order.jsonl`. Replay target: `omp-order` |
| Handler failure | A throwing `tool_call` handler emitted an extension error and returned an error result for `read`; no successful `tool_result` event was emitted. This is fail-closed for the exercised event. | `setup/setup-probes.sh`, `omp/runtime-handler-failure.json`, `omp/runtime-events-failure.jsonl`. Replay target: `omp-failure` |
| Rule disablement | `--no-rules` removed user/project sticky and unconditional rule markers while leaving root context guidance present. | `setup/setup-probes.sh`, `omp/runtime-no-rules.json`, `omp/runtime-events-no-rules.jsonl`. Replay target: `omp-no-rules` |
| Extension disablement | Unsupported; `--no-extensions` was not run. | — (no replay target) |
| Reload | Unsupported. Restart and resume reloaded the extension, but same-process `ctx.reload()` and file-change behavior were not exercised. | — (no replay target) |
| Cleanup | `session_shutdown` ran in primary and child sessions. Timer/resource cleanup and package removal were not exercised. | `setup/setup-probes.sh`, `omp/runtime-events-child.jsonl`. Replay target: `omp-child` |
| Numeric limits | Unsupported. No instruction, rule, payload, or registration boundary was measured. | — (no replay target) |

OMP is the only tested harness with a usable CP0 deny path. It does not have a proven matched-body context-return path for the current operation, so its path-scoped rule tier is `unsupported`, not `hook-required`.


## Codex CLI 0.147.0


### Native root and registration


The following registration flow completed inside the isolated `CODEX_HOME`. `setup/setup-probes.sh` writes the marketplace tree and the executable `probe-commands.sh`; its `codex-registration` target resolves every path from its own retained location:

1. `codex plugin marketplace add "$PROBE_ROOT/codex/marketplace" --json`
2. `codex plugin marketplace list --json`
3. `codex plugin add cp0-plugin@cp0-marketplace --json`
4. `codex plugin list --json`

| Observation | Result | Evidence |
|---|---|---|
| Custom home | All Codex commands used the isolated `HOME` and `CODEX_HOME`; plugin installation materialized beneath that home, and the runtime created a thread before the account-specific model rejection. No credential file is retained. | `setup/setup-probes.sh`, `codex/runtime-untrusted-hooks-retry.json`, `codex/plugin-add.json`. Replay targets: `codex-registration`, `codex-runtime` |
| Local marketplace registration | Marketplace name `cp0-marketplace` resolved to the isolated marketplace tree; the retained historical output records `/private/tmp/atomic-cp0-20260915/codex/marketplace`, while the relocatable setup derives the equivalent path from its destination. `alreadyAdded` was `false`. | `setup/setup-probes.sh`, `codex/marketplace-add.json`, `codex/marketplace-list.json`. Replay target: `codex-registration` |
| Plugin identity | Selector `cp0-plugin@cp0-marketplace` installed version `1.0.0`. | `setup/setup-probes.sh`, `codex/plugin-add.json`. Replay target: `codex-registration` |
| Installed path | Codex copied the plugin to `<CODEX_HOME>/plugins/cache/cp0-marketplace/cp0-plugin/1.0.0`. | `setup/setup-probes.sh`, `codex/plugin-add.json`. Replay target: `codex-registration` |
| Enabled visibility | `plugin list --json` reported `installed: true` and `enabled: true`, with the local source and marketplace source. | `setup/setup-probes.sh`, `codex/plugin-list-after-add.json`. Replay target: `codex-registration` |

This proves registry and cache materialization only. It does not prove that the plugin's bundled hook ran, that its manifest is trusted, or that plugin capabilities reached a model.


### Runtime capability status


The isolated runtime authenticated far enough to create a thread, but the configured `gpt-5.2-codex` model was rejected for the ChatGPT account before any hook event. Per the CP0 rule, every unobserved hook behavior is `unsupported`. Replay target for this section: `codex-runtime`. The target preserves the expected nonzero rejection: a missing `thread.started` line is a failure, and a zero exit status is reported as `UNEXPECTED_SUCCESS` rather than as evidence of hooks, context return, or deny.

| Dimension | Status | Reason and evidence |
|---|---|---|
| Global/project/nested `AGENTS.md` discovery and override precedence | Unsupported | No completed request or instruction audit. `codex/runtime-untrusted-hooks-retry.json`. Replay target: `codex-runtime` (absence) |
| Plugin-hook trust and changed-definition re-review | Unsupported | No hook review or execution occurred. Replay target: `codex-runtime` (absence) |
| Deliberate hook disablement and config precedence | Unsupported | Not exercised. No replay target |
| `SessionStart` and resume | Unsupported | Thread creation occurred; hook did not. Replay target: `codex-runtime` (absence) |
| Subagent start/stop and child context | Unsupported | Not exercised. No replay target |
| `PreToolUse` structured path inventory | Unsupported | Not exercised. Replay target: `codex-runtime` (absence) |
| Context return, deny, and input rewrite | Unsupported | Not exercised. Replay target: `codex-runtime` (absence) |
| Multiple-hook ordering and concurrency | Unsupported | Not exercised. No replay target |
| Failure, timeout, and exit-code semantics | Unsupported | Not exercised. Replay target: `codex-runtime` (absence); its manifest `partial` status is replay-only (see Replay assumptions), never capability support. |
| Additional-context spill and consumer resolution | Unsupported | Not exercised. The documented 2,500-token threshold is not runtime proof. Replay target: `codex-runtime` (absence) |
| Hosted-tool and specialized-tool bypasses | Unsupported | Not exercised. No replay target |
| Reload | Unsupported | Not exercised. No replay target |
| Plugin removal and cleanup | Unsupported | Not exercised after the stop instruction. No replay target |
| Registration cardinality and collisions | Unsupported | One marketplace and one plugin were registered. Replay target: `codex-registration` |
| Instruction and payload size limits | Unsupported | The documented 32 KiB project-instruction default was not boundary-tested. No replay target |

Evidence: `codex/runtime-summary.json`, `codex/runtime-untrusted-hooks-retry.json`, and the registration records above. Replay targets: `codex-runtime`, `codex-registration`.


## Cross-harness operational matrix


| Dimension | Claude 2.1.273 | OMP 18.1.18 | Codex 0.147.0 | Evidence |
|---|---|---|---|---|
| Native global root | `CLAUDE_CONFIG_DIR` supported isolated state; global `CLAUDE.md` and `rules/` were observed | Active agent root reported as `~/.omp/agent`; native context, rules, and extensions were observed | Isolated `CODEX_HOME` was used for config, runtime, and plugin cache | `setup/setup-probes.sh`; `claude/runtime-events-success.jsonl`; `omp/config-path.json`, `omp/runtime-events-stdin-closed.jsonl`; `codex/plugin-add.json`, `codex/runtime-untrusted-hooks-retry.json`. Replay targets: `claude-primary`, `omp-config-path`, `omp-root`, `codex-registration`, `codex-runtime` |
| Registration | No separate registration surface was tested; configured settings hooks and instruction files were discovered | Automatic extension discovery observed; package registration untested | Local marketplace and plugin add/list observed | `setup/setup-probes.sh`; `claude/settings.json`, `claude/runtime-events-success.jsonl`; `omp/runtime-events-stdin-closed.jsonl`, `omp/plugin-list.json`; `codex/marketplace-add.json`, `codex/plugin-add.json`, `codex/plugin-list-after-add.json`. Replay targets: `claude-primary`, `omp-root`, `omp-plugin-list`, `codex-registration` |
| Steering precedence | Root plus nested membership observed; semantic precedence not measured | Native context won same-depth standalone context; nearest non-empty `.omp` selected project-native files | Unsupported | `setup/setup-probes.sh`; `claude/runtime-events-success.jsonl`, `claude/runtime-events-nested.jsonl`; `omp/runtime-events-stdin-closed.jsonl`, `omp/runtime-events-nested.jsonl`. Replay targets: `claude-primary`, `claude-nested`, `omp-root`, `omp-nested`, `codex-runtime` (absence) |
| Static scope matching | Unsupported | Intended scoped-rule marker was absent before and after the target read | Unsupported | `setup/setup-probes.sh`; `claude/runtime-events-success.jsonl`; `omp/runtime-events-scope.jsonl`, `fixtures/omp-repo/.omp/rules/scoped.md`. Replay targets: `claude-primary` (absence), `omp-scope` (absence), `codex-runtime` (absence) |
| Session baseline | Event observed; returned context not consumed | `before_agent_start.systemPrompt` return consumed | Unsupported | `setup/setup-probes.sh`; `claude/runtime-events-success.jsonl`, `claude/hook.py`; `omp/runtime-events-scope.jsonl`. Replay targets: `claude-primary`, `omp-root`, `codex-runtime` (absence) |
| Resume | `SessionStart.source = resume`; files rediscovered | Transcript resumed; extension and session startup reran | Unsupported | `setup/setup-probes.sh`; `claude/runtime-resume.json`, `claude/runtime-events-resume.jsonl`; `omp/runtime-resume.json`, `omp/runtime-events-resume.jsonl`. Replay targets: `claude-primary` → `claude-resume`, `omp-session-create` → `omp-resume`, `codex-runtime` (absence) |
| Child | Unsupported | Extension plus sticky and unconditional rule baselines reached the child; user/root context did not | Unsupported | `setup/setup-probes.sh`; `omp/runtime-child.json`, `omp/runtime-events-child.jsonl`. Replay targets: `claude-primary` (absence), `omp-child`, `codex-runtime` (absence) |
| Structured targets | Unsupported | `read.input.path` observed; the shell command string is not a target path | Unsupported | `setup/setup-probes.sh`; `omp/runtime-events-tools.jsonl`. Replay targets: `claude-primary` (absence), `omp-tools` |
| Context return | Unsupported | Session/turn baseline only; no pre-operation return | Unsupported | `setup/setup-probes.sh`; `omp/runtime-events-scope.jsonl`. Replay targets: `claude-primary` (absence), `omp-scope` (absence), `codex-runtime` (absence) |
| Deny | Unsupported | The exact exercised side-effecting `bash` call returned the deny error and its named file remained absent | Unsupported | `setup/setup-probes.sh`, `setup/run-omp-deny-probe.sh`; `omp/runtime-deny-side-effect/blocked-command.txt`, `omp/runtime-deny-side-effect/hook-events.jsonl`, `omp/runtime-deny-side-effect/runtime-tool-events.jsonl`, `omp/runtime-deny-side-effect/exit-status.txt`, `omp/runtime-deny-side-effect/post-run-absence-check.txt`. Replay targets: `claude-primary` (absence), `run-omp-deny-probe.sh`, `codex-runtime` (absence) |
| Ordering | Unsupported | Sequential extension registration order observed for two `tool_call` handlers | Unsupported | `setup/setup-probes.sh`; `omp/runtime-handler-order.json`, `omp/runtime-order.jsonl`. Replay targets: `omp-order`, `codex-runtime` (absence) |
| Dedupe | Unsupported | The same resolved extension path loaded once when automatic and explicit | Unsupported | `setup/setup-probes.sh`; `omp/runtime-extension-dedupe.json`, `omp/runtime-events-dedupe.jsonl`. Replay targets: `omp-dedupe`, `codex-runtime` (absence) |
| Reload | Unsupported | Unsupported | Unsupported | — (not exercised; no replay target) |
| Concurrency | Unsupported | Same-event sibling handlers were sequential; broader concurrency unsupported | Unsupported | `setup/setup-probes.sh`; `omp/runtime-handler-order.json`, `omp/runtime-order.jsonl`. Replay targets: `omp-order`, `codex-runtime` (absence) |
| Failure | API failure still ran startup and end hooks; hook failure untested | Throwing pre-tool handler returned an error result for `read`; no successful `tool_result` event was emitted | Model validation failed before hooks | `setup/setup-probes.sh`; `claude/runtime-primary-success.json`, `claude/runtime-events-success.jsonl`; `omp/runtime-handler-failure.json`, `omp/runtime-events-failure.jsonl`; `codex/runtime-untrusted-hooks-retry.json`. Replay targets: `claude-primary`, `omp-failure`, `codex-runtime` |
| Disablement | `--safe-mode` disabled configured hooks; instruction disablement untested | `--no-rules` removed sticky and rule bodies; extension disablement untested | Unsupported | `setup/setup-probes.sh`; `claude/runtime-safe-mode.json`, `claude/runtime-summary.json`; `omp/runtime-no-rules.json`, `omp/runtime-events-no-rules.jsonl`. Replay targets: `claude-safe-mode`, `omp-no-rules` |
| Cleanup | `SessionEnd` event observed; resource cleanup untested | `session_shutdown` observed; timer/resource and package cleanup untested | Unsupported | `setup/setup-probes.sh`; `claude/runtime-events-success.jsonl`; `omp/runtime-events-child.jsonl`. Replay targets: `claude-primary`, `omp-child` |
| Visibility | `InstructionsLoaded` exposed discovered files | Extension API exposed command and tool inventory | Plugin list exposed installed/enabled state | `setup/setup-probes.sh`; `claude/runtime-events-success.jsonl`; `omp/runtime-summary.json`; `codex/plugin-list-after-add.json`. Replay targets: `claude-primary`, `omp-root`, `codex-registration` |
| User overrides | Setting-source and semantic override behavior unsupported | Provider/config precedence and higher-layer overrides unsupported | Unsupported | — (not exercised; no replay target) |
| Numeric limits | Unsupported | Unsupported | Unsupported | — (not boundary-tested; no replay target) |


## Adapter decisions fixed by CP0


- Claude may use direct global `CLAUDE.md`, relative repository `@AGENTS.md` loaders, unconditional rules, `SessionStart`, and `InstructionsLoaded` only for the behaviors proved above. Claude path-scoped delivery, child delivery, pre-operation context, and deny remain disabled in the capability record for `2.1.273`. Replay targets: `claude-primary`, `claude-nested`, `claude-resume`, `claude-safe-mode`.
- OMP may use the active agent root reported by `omp config path`, automatic extension discovery, `before_agent_start` for session/turn baseline context, and `tool_call` for exact structured inspection or deny. It must not label native path-scoped rules as matched-body delivery, and it must not claim pre-operation context return. Replay targets: `omp-config-path`, `omp-plugin-list`, `omp-root`, `omp-nested`, `omp-scope`, `omp-tools`, `omp-session-create`, `omp-resume`, `omp-child`, `omp-failure`, `omp-no-rules`, `omp-dedupe`, `omp-order`, `run-omp-deny-probe.sh`.
- Codex may use the observed local marketplace and plugin registry commands and the resulting cache identity. Runtime enrollment must remain capability-disabled until trust, event, context-return, deny, tool coverage, resume, child, failure, spill, and bypass probes succeed for `0.147.0`. Replay targets: `codex-registration`, `codex-runtime`.
- No harness has a runtime-proven numeric instruction or hook-payload ceiling from CP0. Preflight may use a stricter Atomic-owned limit, but it cannot describe that limit as native or infer silent truncation behavior.
- Shell command strings are never structured target paths. OMP's observed `read.input.path` is structured; unexercised tool schemas are unsupported.


## Unsupported rows that block later parity claims


- Claude: path-scoped rule activation, semantic steering precedence, structured tool targets, pre-operation context return, deny, child delivery, hook failure, instruction disablement, dedupe, handler ordering, concurrency, reload, resource cleanup, and numeric limits are unsupported. Replay targets `claude-primary`, `claude-nested`, `claude-resume`, and `claude-safe-mode` exist only for the partial or supported rows above; a target never upgrades the unsupported ones.
- OMP: automatic matched-body scope delivery, pre-operation context return, edit/write/apply-patch target schemas, treating shell commands as target paths, package registration and lifecycle, shared-package visibility, project install scope, extension disablement, live reload, cross-tool or cross-session concurrency, provider/config precedence, timer/resource cleanup, registration collisions, and numeric limits are unsupported. Child context-file delivery is partial: sticky and unconditional rule markers survived, but user/root context markers did not. Replay targets: `omp-scope` and `omp-order` cover absence rows; package lifecycle, extension disablement, reload, precedence, and numeric limits have no target.
- Codex: global/project/nested steering and precedence, plugin-hook trust or re-review, disablement precedence, every runtime hook and event result, mediated-tool coverage, hosted-tool bypasses, resume, child behavior, ordering, concurrency, failure/timeout semantics, spill behavior, reload, plugin removal/cleanup, registration collisions, and numeric limits are unsupported. Only isolated home usage plus marketplace/plugin registration and installed/enabled list visibility are supported. Replay targets: `codex-registration` for the supported registration surface, `codex-runtime` for the thread-creation absence.

No design or spec amendment was required. The current contract already says missing capability rows produce `unsupported`; CP0 supplies those missing rows without changing the architecture.

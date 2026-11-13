# Harness capability matrix


**Status:** CP0 evidence recorded 2026-09-15 for Claude Code `2.1.273`, OMP `18.1.18`, and Codex CLI `0.147.0` on macOS arm64. Updated 2026-11-11 with OMP 18.18.3 and Codex CLI 0.147.0 artifact-kind rows, and with Claude Code 2.1.322 re-records of the Claude rows the redesign relies on; see the dated sections below.

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


## Agent delivery


CP0 ran no agent probe, so the `agent-delivery` row is `unsupported` for OMP `18.1.18` and Codex `0.147.0`. The OMP and Codex adapters write the agent files as ledger-owned resources, and enrollment, status, and doctor report agent delivery `unsupported` until a row below records a passing run. The Claude adapter's `agents/` delivery is not gated on this row. CP16 runs the procedures below and records the result here.

| Harness | Native file | Status | Probe procedure | Expected observation | Vendor documentation |
|---|---|---|---|---|---|
| Claude Code | `<CLAUDE_CONFIG_DIR>/agents/<name>.md`, keys `model` and `effort` | Partial on 2.1.322: the override reached the agent file and the init event listed the agent; the model-level steps 2 and 3 need a credential and were not reached. `TestClaudeAgentOverrideIsProjectedAndTheAgentListed` | 1. In an isolated `HOME` and `CLAUDE_CONFIG_DIR`, run `atomic harness enroll claude` with `[claude.agents.atomic-investigator]` set to a model distinct from the session model. 2. Run `claude -p --output-format stream-json --verbose --forward-subagent-text` with a prompt asking it to dispatch `atomic-investigator` by name with no model argument. 3. Capture the event stream. | An `Agent` tool_use (named `Task` before Claude Code 2.1.63) carries `subagent_type: atomic-investigator`, and the assistant messages whose `parent_tool_use_id` is that tool_use name the configured model in `message.model` (that field is `unverified`). | [Claude Code subagents](https://code.claude.com/docs/en/sub-agents) |
| OMP | `<profile agent root>/agents/<name>.md`, keys `model` and `thinking` | Partial on 18.18.3: the override reached the agent file and the `task` tool in the opening request listed the agent; OMP has no agent-listing verb, and step 3 needs a model turn and was not reached. `TestOMPAgentOverrideIsProjectedAndTheAgentListed` | 1. In an isolated `HOME`, with the profile agent root under it, run `atomic harness enroll omp` with `[omp.agents.atomic-investigator]` set to a model and a `thinking` level distinct from the session's. 2. Start `omp` in a probe repository, list the available agents, then ask it to run `atomic-investigator`. The surface that lists agents (an extension event field or an `omp` subcommand) is `unverified`. 3. Capture the provider request the child sends. | `atomic-investigator` appears in the inventory, and the child's provider request names the configured model and thinking level. | `unverified` |
| Codex CLI | `$CODEX_HOME/agents/<name>.toml`, keys `model` and `model_reasoning_effort` | Partial on 0.147.0: the override reached the TOML and the `spawn_agent` roles in the opening request named the agent with its configured model and effort; a spawned child needs a model turn and was not reached. `TestCodexAgentOverrideIsProjectedAndTheRoleListed` | 1. In an isolated `HOME` and `CODEX_HOME`, run `atomic harness enroll codex` with `[codex.agents.atomic-investigator]` set to a model and an effort distinct from the session's. 2. Run `codex exec --json` in a probe repository, asking it to spawn `atomic-investigator`. 3. Capture the event stream. | `atomic-investigator` is listed once, from `$CODEX_HOME/agents/`, the spawned agent is `atomic-investigator`, and its turn reports the configured model and reasoning effort. | [Codex subagents](https://developers.openai.com/codex/subagents) |

A run that cannot authenticate, as every CP0 run could not, leaves the row `unsupported` with its observation recorded.


## Session context


The Codex adapter writes one `SessionStart` member into `$CODEX_HOME/hooks.json` (matcher `startup|resume`, command `atomic hooks session-start --harness codex`). Atomic never writes Codex's trust record. Codex runs a non-managed hook only after the user trusts it in `/hooks`, and records that trust in `$CODEX_HOME/config.toml` as `[hooks.state."<hooks.json path>:session_start:<group>:<handler>"]` with `trusted_hash`. That key shape and hash come from `hook_key` and `hook_hash` in the upstream source ([`codex-rs/hooks/src/engine/discovery.rs`](https://github.com/openai/codex/blob/main/codex-rs/hooks/src/engine/discovery.rs)). They are `unverified` against the 0.147.0 binary, which carries the `hooks.state` and `trusted_hash` strings. Doctor reports `trusted` only when the member's key records a hash equal to its computed identity hash. It reports `untrusted` when `config.toml` holds no `[hooks.state]` entries, or when the member's key holds no hash. It reports `unknown` otherwise, including when entries exist only under other keys, because the key shape is unverified for codex-cli 0.147.0. The `session-context` capability row stays `unsupported` until CP16 records a passing run.

| Harness | Native surface | Status | Probe procedure | Expected observation | Vendor documentation |
|---|---|---|---|---|---|
| Codex CLI | `$CODEX_HOME/hooks.json` `SessionStart` member | Supported at startup on 0.147.0 once trusted: the request carried the verb's `additionalContext`; untrusted, it carried none. Resume and a model quoting the block were not observed. `TestCodexSessionStartHookDeliversTheResolvedWorkflowBlockOnceTrusted` | 1. In an isolated `HOME` and `CODEX_HOME`, run `atomic harness enroll codex` from a probe repository that has a resolved workflow-preferences block. 2. Start `codex` there, open `/hooks`, and trust the Atomic `SessionStart` hook; confirm `config.toml` gains its `trusted_hash` and `atomic doctor` reads `trusted`. 3. Run `codex exec --json` in the probe repository, asking it to quote the `## Workflow preferences` section it was given, then repeat with `codex exec resume`. | The hook runs on startup and resume, and the model quotes the workflow-preferences block that `atomic hooks session-start --harness codex` printed. Without the trust step, no hook runs and the block is absent. | [Codex hooks](https://developers.openai.com/codex/hooks) |


## Update 2026-11-11: OMP 18.18.3 artifact kinds, Claude static scope


This section adds rows; the CP0 findings above stand as recorded.

`TestOMPNativeKindDelivery` in `atomic/internal/runtimeproof/omp_kinds_test.go` enrolls an isolated profile through the production adapter and loads a test-only extension, `testdata/omp-payload-probe.ts`, that writes each `before_provider_request` payload to a file. OMP builds that request before any model answer, so the placeholder credential is enough and no row below needed model auth.

| Kind | Native location | Observation on omp 18.18.3 | Verdict |
|---|---|---|---|
| Commands | `<profile agent root>/commands/<name>.md` | The prompt `/atomic-probe alpha beta` reached the request as the command body with `$ARGUMENTS` replaced by `alpha beta` | `command-delivery` supported |
| Skills | `<profile agent root>/skills/<name>/SKILL.md` | With the `read` tool enabled, the skill name and description were listed in the system prompt | `skill-discovery` supported; the model choosing to load a skill was not observed |
| `@` imports | A whole-line `@path` in the profile `AGENTS.md`, a repository `AGENTS.md`, or `CLAUDE.md` | `@~/.atomic/profile.md` in the profile `AGENTS.md` expanded. A repository `AGENTS.md` holding `@docs/wiki/index.md` expanded the router. With the repository `AGENTS.md` disabled as a context file, the loader `CLAUDE.md` holding `@AGENTS.md` was read and expanded the router through it. With `~/.atomic/profile.md` removed, the `@~/.atomic/profile.md` line stayed in the request as a literal token | `steering-imports` supported; a missing target is left literal |
| Matched rule bodies | `<profile agent root>/rules/<name>.md` with `globs` | The rule was listed by name, globs, and description. Its body was absent from the opening request. A body load after a read needs a model answer, which the credential-free run never reaches | `static-scoped-rule` unsupported |
| Output style | None | The bundled docs describe no output-style setting. The style ships inside the profile `AGENTS.md` block, always on | `output-style-selection` unsupported |

Enrollment writes commands and skills to the profile agent root as ledger-owned files. Target uninstall removes them and the directories they emptied, and a user file at either path refuses enrollment unless `--replace` or `--leave-unowned` is given. Because a missing import target stays literal, an OMP-only `atomic install` seeds `~/.atomic/profile.md` the same way a Claude install does (`TestLifecycleCLIOMPTarget`). The two unsupported kinds read `WARN unsupported` for an OMP target in `atomic install`, `atomic harness status`, and `atomic harness rules status`, and they turn doctor categories 18 and 19 to WARN for an enrolled OMP profile. These rows carry version 18.18.3 in `OMPCapabilities`; the matrix version stays 18.1.18 for the CP0 rows.

Correction to the Claude Code 2.1.273 section: the `static-scoped-rule` row is now `partial`. `TestClaudeFileReadLoadsNestedWikiLoaderAndScopedRule` observed Claude Code 2.1.322 load an installed `paths:` rule when an `@`-mentioned file matched it (`load_reason` `path_glob_match`). It did not load the rule at startup. Model-issued reads, resumed delivery, and child delivery were not observed.


## Update 2026-11-11: Codex CLI 0.147.0 artifact kinds and registration


This section adds rows; the CP0 findings above stand as recorded, except the two corrections at its end.

`TestCodexNativeKindDelivery` in `atomic/internal/runtimeproof/codex_kinds_test.go` sets `HOME` and `CODEX_HOME` to temp directories, converges a Codex target through the production adapter, and runs `codex debug prompt-input`. That command renders the model-visible input list without a model call, so no row below needed model auth. It writes no trust record.

| Kind | Native location | Observation on codex-cli 0.147.0 | Verdict |
|---|---|---|---|
| Global steering | `$CODEX_HOME/AGENTS.md` | The user's line and the Atomic block, output style included, reached the input ahead of the repository `AGENTS.md` | `global-steering` supported |
| Skills | `skills/<name>/SKILL.md` in a registered plugin | Listed as `atomic:<name>` with description and cache path once the plugin was registered; the body was absent. `plugin.json` needs no `skills` key, because Codex discovers `skills/` by default | `skill-discovery` supported; the model choosing to load a skill was not observed |
| Commands | None | `/atomic-probe alpha beta` stayed literal user text. A plugin carries no command surface, and custom prompts are deprecated in favor of skills | `command-delivery` unsupported |
| `@` imports | A whole-line `@path` in `$CODEX_HOME/AGENTS.md` or a repository `AGENTS.md` | Both lines stayed literal; neither target's content reached the input | `steering-imports` unsupported; the Atomic block strips its import lines |
| Output style | None | `codex --help` and the bundled Codex docs carry no output-style setting. The style ships inside the global block, always on | `output-style-selection` unsupported |
| Rule bodies and wiki pointer cards | None | Codex has no path-scoped rule surface. A rule body would need a pre-operation hook, and no hook event was reached | `static-scoped-rule` unsupported |
| Native tools | None | The input list carries no tool schema, so no Codex tool is proven and the global block has no native-tools paragraph | No row |
| Registration | `$CODEX_HOME/config.toml` and `plugins/cache/atomic-claude/atomic/1.0.0` | `plugin add` re-copies a changed plugin, byte for byte (same tree digest). `marketplace add` refuses a name already added from another source. `plugin remove` then `marketplace remove` clear both config entries and the cache. On an absent registration `plugin remove` exits 0 and `marketplace remove` exits 1 (`marketplace atomic-claude is not configured or installed`), so unregister runs each only for an entry `config.toml` records. `plugin add` on a plugin recorded `enabled = false` exits 0 and sets `enabled = true`, so it is the re-enable step doctor names | Registration and removal supported |

`atomic install --harness codex` now writes the global block as a ledger-owned managed block and registers the plugin after enrollment, recording the plugin cache as an owned tree keyed by its path. Registration runs only when Codex does not already hold the current copy, so a user's `enabled = false` stands. Target uninstall strips the block and, when it removed the recorded cache, runs `plugin remove` and `marketplace remove`. Without a codex binary, install reports `WARN unsupported native plugin registration` with its fix and does not report the registration applied (`TestLifecycleCLICodexTarget`, `TestLifecycleCLICodexRegistersThePlugin`). Every unsupported kind reads `WARN unsupported` for a Codex target, and doctor category 24 prints a fix line for each unsupported surface.

Corrections to the Codex CLI 0.147.0 section: global `AGENTS.md` discovery is now supported, and plugin removal and cleanup is observed. Project and nested `AGENTS.md` precedence, and plugin-hook teardown, remain unsupported.


## Update 2026-11-11: OMP 18.18.3 and Codex CLI 0.147.0 redesign rows


This section re-records the OMP and Codex rows the redesign relies on; the findings above stand where this table does not supersede them.

The tests in `atomic/internal/runtimeproof/omp_redesign_test.go` and `codex_redesign_test.go` install Atomic with the built binary into a temp `HOME` with its own `CODEX_HOME`, OMP profile root, and git repository. OMP runs with a placeholder OpenAI key that the provider rejects, and `testdata/omp-payload-probe.ts` records the request it built. Codex runs against a local stand-in provider that records the request and answers 400. Every row stops at startup: the assembled request or an app-server answer, never a model turn. Raw evidence and digests: `<state-root>/.scratchpad/omp-plugin-compatibility/runtime/omp/INDEX.md` and `.../runtime/codex/INDEX.md`.

| Row | Observation | Verdict |
|---|---|---|
| OMP session-baseline event | The extension ran `atomic hooks session-start --format text --harness omp` at `session_start`, and the opening request carried its exact text, workflow-preferences block included | Supported at startup |
| OMP session switch | `new_session` fired `session_switch` (reason `new`) with the switched session's `ctx.cwd` into the running extension, with no new `session_start`. `resume` and `fork` emit the same event in the OMP 18.18.3 source (`agent-session.ts:8844` and `:7754`); neither was observed. The module now reloads the session context and restarts its matched-rule state on the event, and the next request carried the second repository's block | Supported at startup for `new` (`TestOMPSessionSwitchReloadsContextAndMoveDoesNot`); `resume` and `fork` from source only |
| OMP `/move` | Fires no extension event. `before_agent_start` already reports the new `ctx.cwd`, but the context loaded for the first directory stays until a switch | Unsupported as probed 2026-11-11. Update 2026-11-12: the extension reloads at `before_agent_start` when `ctx.cwd` changed; on omp 18.18.3 the turn after `/move` carried the moved-to repository's context and none of the first's (`TestOMPMoveAndNewSessionBothReloadContext`, evidence `p43-session-switch.*`). Supported at startup |
| OMP project rules and wiki cards | The `<domain-rules>` listing takes only files directly under `.omp/rules/` that carry a `description`, and renders a `paths:`-only scope as `()`. The card Atomic publishes under `.omp/rules/atomic/wiki/` was neither listed nor loaded | Unsupported as probed 2026-11-11. Update 2026-11-12: the card is now published as a flat `.omp/rules/atomic-wiki-<domain>.md` with `description` and `globs`, and omp 18.18.3 listed it as `- atomic-wiki-src (src/**): ...` (`TestOMPMigratedRepositoryResolvesAtomicAndListsItsFlatCards`, evidence `p43-wiki-card-listing.payload.jsonl`). OMP skips rule files any Git ignore source covers, so the files stay unignored. Listed at startup; loading the body is the model's `rule://` call, not reached. Update 2026-11-12 (CP18B, supersedes the flat copy above): the card is now a committed relative per-file symlink `.omp/rules/atomic-wiki-<domain>.md -> ../../<state-root>/rules/wiki/<domain>.md` to the canonical card, which carries `description` and `globs`; OMP lists a linked card under the link's filename and checks ignore status on the link path (omp 18.18.3 source; fixture listing `- atomic-wiki-src (src/**): ...` in `TestOMPMigratedRepositoryResolvesAtomicAndListsItsLinkedCards`, evidence `p43-linked-card-listing.payload.jsonl`). An out-of-repository state root keeps the generated flat copy |
| Codex hook trust key | `hooks/list` on `codex app-server` reports the member's key as `<symlink-resolved $CODEX_HOME>/hooks.json:session_start:0:0` and its `currentHash` as Atomic's identity hash, with `trustStatus` one of `managed`, `untrusted`, `trusted`, or `modified`. A hash on another key leaves the member `untrusted`; a stale hash on its key reads `modified` | Verified; this supersedes `unverified` in the Session context section |
| Codex wiki cards at edit time | `hooks/list` on `codex app-server` reports the owned `PreToolUse` member (matcher `apply_patch`, command `atomic hooks pre-tool-use`) at `<resolved $CODEX_HOME>/hooks.json:pre_tool_use:<group>:<handler>` with a `currentHash` equal to Atomic's identity hash. Given an `apply_patch` payload, the verb prints `hookSpecificOutput.additionalContext` with the cards whose `paths:` match the patch's `*** Add File:`, `*** Update File:`, and `*** Move to:` paths, once per card per `session_id`. The evidence is startup-level: Codex lists and hashes the member, and the verb output comes from a constructed payload, not an observed `apply_patch` delivery. No model turn ran, so the model receiving a card was not observed; reads and shell edits carry no card | Partial (edits only), startup level. Update 2026-11-12 (CP18C): `TestCodexPreToolUseHookIsAcceptedAndDeliversTheMatchingCard`, evidence `runtime/codex/codex-pre-tool-use-cards.json`; the `wiki-card-delivery` row and the `hook-required` tier in `atomic harness rules status` rest on it |
| Codex `spawn_agent` visibility | On `gpt-5.2` and `gpt-5.4` the multi-agent tools sit behind `tool_search`, so the roles are absent from the opening request; `gpt-5.1-codex` and `o3` carry them directly | Model-dependent |

Doctor now classifies the member as Codex does: a hash on another key reads `untrusted`, and a stale hash on the member's key reads `modified`, with a fix line telling the user to trust the hook again in `/hooks`. `TestCodexHookTrustKeyIsTheOneAtomicReads` asserts the two agree on every shape it writes.


## Update 2026-11-11: Claude Code 2.1.322


This section re-records, on Claude Code 2.1.322, the Claude rows the redesign relies on; the 2.1.273 findings above stand as recorded where this table does not supersede them.

The tests in `atomic/internal/runtimeproof/claude_redesign_test.go` install Atomic with the built binary into a temp `HOME` with its own `CLAUDE_CONFIG_DIR` and git repository, and launch `claude -p --output-format stream-json --verbose --include-hook-events` through `runtimeproof.Run`. Unlike CP0, `HOME` is isolated too. No credential reaches the launch (`apiKeySource: none`, result `Not logged in`), so every row stops at startup: hooks, `InstructionsLoaded`, and the init event, never a model turn. Raw evidence and digests: `<state-root>/.scratchpad/omp-plugin-compatibility/runtime/claude/INDEX.md`.

| Row | Claude Code 2.1.273 | Claude Code 2.1.322 |
|---|---|---|
| Native global root | `CLAUDE_CONFIG_DIR` held settings, rules, and transcripts | Unchanged, with `HOME` isolated as well: the installed global `CLAUDE.md` loads at `session_start` and its `@~/.atomic/profile.md` import resolves under the isolated `HOME` |
| Session-baseline event | `SessionStart` ran; returned context not consumed | `SessionStart` ran the installed `atomic hooks session-start --harness claude`, and the stream's `hook_response` carried an `additionalContext` byte-equal to the verb's output, workflow-preferences block included. Partial: consumption by a model turn is not observed |
| Static scoped-rule surface | Unsupported | Partial. A `paths:` rule loads with `load_reason: path_glob_match` when an `@`-mentioned file matches, at startup of the print run; it does not load without a match. Model-issued reads, resume, and child delivery are not observed |
| Rules through a directory symlink | Not exercised | Supported from the repository root: a card at `.atomic/rules/wiki/src.md`, reached through `.claude/rules/atomic -> ../../.atomic/rules`, loads with `path_glob_match` and is reported at its resolved path |
| Subdirectory session and the rules link | Not exercised | Unsupported. A session started in `repo/src` does not load the card through the link, while a regular rule file in `.claude/rules/` with the same `paths:` loads. Print mode shows no approval prompt and the debug log records no skip, so the link stays unread for the whole print session; the interactive approval path was not exercised |
| Repository steering beside a user `CLAUDE.md` | Root `CLAUDE.md` and its `@AGENTS.md` import loaded | A root `CLAUDE.md` without `@AGENTS.md` loads alone; the built-in `cc-plugin-agents-md` plugin does not add `AGENTS.md`. Once `CLAUDE.md` imports `@AGENTS.md`, `AGENTS.md` and its `@docs/wiki/index.md` import load as includes |
| Agent discovery | Not exercised | Supported at startup: the init event's `agents` lists all eight installed Atomic agents. Override delivery is in the agent-delivery row above |
| Visibility | `InstructionsLoaded` exposed discovered files | Unchanged, plus the stream-json init event lists `tools`, `agents`, `skills`, `slash_commands`, `output_style`, and `claude_code_version`. A print run's `tools` include `Task`, `EnterWorktree`, `Monitor`, `TaskStop`, and `CronCreate`, and exclude `Agent` and `AskUserQuestion`, which the Claude steering paragraph names; whether an interactive session exposes them under those names was not observed |


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
- OMP: automatic matched-body scope delivery, pre-operation context return, edit/write/apply-patch target schemas, treating shell commands as target paths, package registration and lifecycle, shared-package visibility, project install scope, extension disablement, live reload, cross-tool or cross-session concurrency, provider/config precedence, timer/resource cleanup, registration collisions, agent delivery, and numeric limits are unsupported. Child context-file delivery is partial: sticky and unconditional rule markers survived, but user/root context markers did not. Replay targets: `omp-scope` and `omp-order` cover absence rows; package lifecycle, extension disablement, reload, precedence, and numeric limits have no target.
- Codex: global/project/nested steering and precedence, plugin-hook trust or re-review, disablement precedence, every runtime hook and event result, mediated-tool coverage, hosted-tool bypasses, resume, child behavior, ordering, concurrency, failure/timeout semantics, spill behavior, reload, plugin removal/cleanup, registration collisions, agent delivery, and numeric limits are unsupported. Only isolated home usage plus marketplace/plugin registration and installed/enabled list visibility are supported. Replay targets: `codex-registration` for the supported registration surface, `codex-runtime` for the thread-creation absence.

No design or spec amendment was required. The current contract already says missing capability rows produce `unsupported`; CP0 supplies those missing rows without changing the architecture.

# Atomic multi-harness compatibility


## Goal


Atomic installs one authored policy and workflow corpus into Claude Code and OMP through native steering, artifact, configuration, and lifecycle surfaces. The same architecture produces Codex-native instructions and agents without making Claude directories or semantics authoritative. Repository and realm state lives in a harness-neutral root, `.atomic/` by default; Atomic orients from the environment each harness sets; every Atomic agent takes the user's per-harness model and effort override on every harness; and every harness receives session context at start.


## Non-goals


- A universal workflow interpreter or common runtime tool API.
- Separate authored prompt trees for each harness.
- Global steering or output style represented as a rule.
- Choosing a provider or model for the user. Atomic stores the user's choice and relays it to each harness.
- Restricting an agent's tools. Write scope is an instruction, not a tool allowlist.
- OMP package publication to a registry in Milestone A.
- Reminders in any form (see `docs/spec/workflow-preferences.md`).
- Windows support.


## Success criteria


- [ ] `context/` remains the only authored artifact corpus, and template expansion occurs once before harness projection.
- [ ] `context/AGENTS.md` is the sole authored global Atomic contract; the Claude adapter renders it directly into `~/.claude/CLAUDE.md`, followed inside the Atomic block by the adapter's native-vocabulary paragraph, without creating or referencing `~/.claude/AGENTS.md`.
- [ ] Only repository and nested realm Claude steering uses thin `CLAUDE.md` references to adjacent `AGENTS.md` files, without duplicate instruction delivery.
- [ ] Each enrolled OMP profile receives one native `AGENTS.md` whose Atomic-owned content is an import-free rendered Atomic steering body, one blank-line separator, and the Atomic output-style body with parsed YAML frontmatter excluded, plus the adapter's native-vocabulary paragraph when a capability row proves at least one OMP tool name.
- [ ] OMP steering and output style are absent from plugin rules and packaged output-style artifacts; only actual path-scoped rules use the rule projection surface.
- [ ] Canonical commands, agents, skills, and rules have stable identities and source digests; generated native artifacts record source and projection digests.
- [ ] `RuleRecord` parses every shipped `context/rules/**` source into producer-qualified identity, class, `base_kind`, ordered include globs, body, and source digest without checkout-specific paths; target projection metadata remains separate.
- [ ] `RuleInstance` binds a record to one project key and absolute, symlink-resolved runtime base without changing record identity or source digest.
- [ ] Rule parsing rejects unknown scope keys, absolute patterns, parent-escaping patterns, target-name collisions, and malformed frontmatter before any target mutation.
- [ ] Canonical matching verifies resolved containment and supplies every overlapping matched record in stable identity order; two repositories and multiple worktrees share record identity but not runtime bases.
- [ ] Repository wiki refresh is the second canonical rule producer and writes pipeline-owned pointer cards under `<state-root>/rules/wiki/**`; realm refresh produces no scoped pointer rules. The `atomic wiki` verbs call `wiki.Refresh` and `(*omp.Adapter).PublishProjectCards`, so the cards have a production writer and reach every enrolled target.
- [ ] Wiki pointer cards are canonical only at `<state-root>/rules/wiki/`. Claude reads them from a managed, ledger-owned `<repo>/.claude/rules/atomic-wiki/` projection, because Claude loads repository path-scoped rules only from `.claude/rules/`; OMP reads them from `<repo>/.omp/rules/atomic-wiki/`; Codex reads them through the matcher index. No canonical card is copied into a native folder outside a projection.
- [ ] Claude projects shipped rules under `~/.claude/rules/atomic/` and the repository card projection through native scoped-rule behavior, and claims a native tier only after CP0 proves matching, precedence, lifetime, resumed-session, and child-session behavior.
- [ ] OMP projection maps canonical `paths:` into CP0-proven native scope fields and uses a CP0-proven pre-operation event to supply matched bodies when static scope is advisory; missing capability rows produce `unsupported`.
- [ ] Codex projection is an Atomic plugin using CP0-proven registration, session-baseline events, a synchronous pre-operation event exposing structured target paths, a context-return field, and any exact deny result; concrete API names remain capability evidence, not assumed contract.
- [ ] Rule enforcement is reported as `native-scope`, `hook-required`, `deny-enforced`, or `unsupported`; only exact machine-checkable predicates may deny an operation, and semantic prose compliance is never described as hard enforcement.
- [ ] `atomic harness rules status` and doctor report source/projection digests, tier, disabled or trust state, collisions, uncovered tools, and last runtime proof independently per target. An operation without a mediated exact path is uncovered, receives no matched rule body, and remains `unsupported`.
- [ ] OMP loads the enrolled profile's generated extension module from the profile agent root's `extensions/` directory, and the module registers only capability-proven events. Atomic's agents are written to `<profile agent root>/agents/`. Commands, skills, and rule bodies ship in `~/.atomic/packages/omp/atomic` as a corpus store and reach a native OMP discovery location only when a recorded runtime probe proves that location. Until then package installation, registration, and discovery of each artifact kind are `unsupported` and are reported in every enrollment, status, rules, and doctor surface rather than presented as delivered.
- [ ] Command invocation, arguments, agent dispatch, required dependencies, and explicit-only behavior match each milestone's capability matrix.
- [ ] Canonical agents declare no `model`, `effort`, `tools`, `disallowedTools`, or `mcpServers`; the projection refuses any agent that does. Each agent states behavioral intent, required capabilities, write scope, and execution preference, and its write scope is an instruction on every harness.
- [ ] A Codex TOML agent decodes to the canonical rendered agent body before Milestone A; Codex runtime support remains gated by Milestone B.
- [ ] `~/.atomic/config.toml`, profile/wiki data, `install/ledger.json`, `install/operation.lock`, journals, transaction stage/backups, recovery backups, legacy pre-install snapshot, and project state-location records have one documented writer and retention contract.
- [ ] Read-only discovery classifies `absent`, structurally `legacy-complete`, `legacy-partial`, `v2-clean`, `v2-in-flight`, `v2-orphaned`, or `mixed` from native bytes, legacy manifest/snapshots/proposals/settings, v2 ledger/journals/transactions/packages/registrations, and project selection; discovery never enrolls. A ledger row whose bytes no longer match is per-resource drift reported through `Drift` — a changed resource needs a replace-or-leave-unowned decision and an absent one is recreated — never a `mixed` state; `mixed` is reserved for unreadable v2 evidence and unjournaled remnants that overlap legacy evidence.
- [ ] Install, update, repair, adoption, uninstall, and any ledger-managed `atomic doctor --fix` acquire one global advisory lifecycle lock, reject unreadable newer schemas, recover unresolved journals oldest-first, and re-observe before planning any mutation.
- [ ] Dry-run leaves the complete filesystem byte-identical, reports the same pre-recovery classification and conflicts, and simulates journal-described writes in memory. Current journal/ledger/digest evidence that determines one safe result yields an advisory post-recovery plan; unavailable observations, decision-relevant external/native effects, later edits/conflicts, or user choice yield `blocked_on_recovery`.
- [ ] Only bytes matching the selected binary's embedded generation (for an agent file, the embedded projection with the user's stored override overlaid), uniquely parseable Atomic blocks, and bytes the ledger itself recorded applying are automatically verifiable ownership evidence; this design has no historical digest catalog. The ledger row is proof of Atomic's own last write, not a catalog of past generations: bytes matching neither the selected generation nor the ledger's recorded digest for that (target, resource) require a batched per-resource replace-or-leave-unowned decision. Names and legacy config paths never prove ownership; missing listed files remain drift until convergence is accepted, and unjournaled v2 remnants are conflicts.
- [ ] Pending proposed/merged Claude files, missing or corrupt legacy snapshots, and overlapping legacy/v2 evidence block automatic adoption until explicitly resolved or acknowledged.
- [ ] A steering file that exists and carries no Atomic tag at all is "no block yet", not ambiguity: Claude adoption and OMP enrollment append the block and preserve every existing byte. A file whose Atomic tags do not parse to exactly one block still refuses loudly.
- [ ] The write-once legacy pre-install snapshot is preserved as recovery history. Before first v2 mutation, current observations, backups, and parent directories are durable; an empty or already-converged plan creates no journal or backup.
- [ ] Partial v2 recovery discards staging only with journal proof, ledger-commits applied bytes only after digest verification, restores backup bytes only when no later edit exists, and otherwise reports conflict. An absent generated-tree destination is the crash window a tree publication always produces: a staged tree still digesting to the intended value finishes the rename, an intact transaction backup is restored, and only when neither holds is it a conflict. A block unit whose file still carries no Atomic tags proves the write never landed and is discarded rather than reported as a conflict.
- [ ] `atomic harness recover [--rollback]` is the explicit resolver for an unresolved journal: it acquires the same advisory lifecycle lock and recovery run every other lifecycle verb performs, rolls forward by default, and restores digest-verified transaction backups under `--rollback`. `--dry-run` opens no lock and simulates each journal in memory. Every lifecycle conflict message names this verb.
- [ ] Explicit repository-state adoption (`atomic state adopt`, and legacy installation adoption given a repository root) selects a root in place without moving files, ignores empty candidates, persists and fsyncs the selection record before resolution switches, treats `ATOMIC_STATE_DIR` as process-only, and rolls the selection back only when no later state write used the new root.
- [ ] Ledger and journal schemas record schema version, writer version, and minimum reader version. New binaries refuse newer incompatible schemas; old Claude-only binaries are unsupported writers after migration and their later changes surface as drift or conflict.
- [ ] Install, update, repair, doctor, and uninstall operate only on explicitly enrolled target instances; discovery never enrolls.
- [ ] One physical resource has one ownership record even when several targets or unenrolled instances can discover it; OMP shared-package visibility and consumer enrollment remain separate and visible.
- [ ] A shared-package generation move converges every enrolled consumer of that resource in one operation: the publishing plan updates each consumer's package row in the same commit, so a row recording an older generation is stale by construction and never blocks another consumer. Convergence refuses before mutation, naming both sides, only when the resource carries a generation claim this operation cannot move — a row for a target the ledger does not enroll whose key parses — because nothing in the run will converge that consumer. A row whose target key does not parse is a stale record an older writer left: nothing enrolls it and nothing will move it, so it never pins the resource and the publication rewrites it with the rest. The enrollment asserts that every consumer the check named was rewritten onto the selected generation in that same commit, and a row-only removal reaches such a stale key.
- [ ] Every multi-resource operation observes the complete plan, persists a durable journal and backup before mutation, stages and validates projections, records each native commit unit, verifies effective content, and only then commits ledger state.
- [ ] Initial generated-directory publication uses one same-filesystem rename; replacement uses a journaled current-to-backup and staging-to-current sequence.
- [ ] Repository-state resolution follows `ATOMIC_STATE_DIR`, then `~/.atomic/<project-key>/state-location.json`, then the one populated candidate in the repository, then the user `state.dir` default, then `.atomic`. Both the strict resolver and the cached form every path helper uses follow this ladder. Resolution never consults harness markers, harness detection, or `ATOMIC_HARNESS`, and introduces no second project-key hierarchy. An invalid `ATOMIC_STATE_DIR` value refuses in both forms instead of falling through.
- [ ] Candidate names are `.atomic`, the configured `state.dir` path, the value of a stored legacy `harness.dir`, `.claude`, and `.pi`. A candidate is populated when it holds `atomic.toml`, `.atomic-index`, `.scratchpad`, `project`, or `rules/wiki`; `worktrees`, settings, agents, and every other harness-native entry are not signatures. Two or more populated candidates are ambiguous: a verb that resolves repository state refuses with the `atomic state adopt` instruction instead of falling through to a lower rung.
- [ ] `.atomic` is the built-in default for both the state setting and the legacy harness setting. A stored `harness.dir` never feeds a ladder rung: its value is read only as one more candidate name, so `harness.dir=.claude` does not pin `.claude`, and doctor warns that `state.dir` supersedes it. `state.dir` accepts a repository-relative path of one or more safe segments (`.config/atomic`). `atomic state adopt --dir <path>` persists any absolute or repository-relative location for every worktree of the clone.
- [ ] Every helper that builds a repository-state path receives the repository root. No production caller passes an empty root, so `atomic repo init`, the `.gitignore` writers, and the signals tree honor the selection record and the candidate rung.
- [ ] An empty repository creates no state candidate or adoption record until a stateful operation needs one; adoption persists before scope resolution switches; Pi agent configuration remains independent.
- [ ] Repo wiki initialization requires Git, creates or amends shared root and `docs/wiki/` `AGENTS.md` files plus adjacent thin Claude loaders without wholesale overwrite, and rejects malformed or conflicting loaders.
- [ ] Realm initialization may start from a non-Git root, creates `<realm>/AGENTS.md` plus `<realm>/CLAUDE.md` and `<realm>/wiki/AGENTS.md` plus `<realm>/wiki/CLAUDE.md`, makes `<realm>/wiki/` its own Git repository, and writes registration authority only to `~/.atomic/wikis.md`.
- [ ] Repository wiki refresh writes canonical scan, index, domain, steering, and pointer-rule files first; realm refresh writes realm and nested-wiki steering plus scan, member, bucket, and domain files without scoped pointer rules. Both auto-converge only already-enrolled targets, and target failure never rolls back canonical output.
- [ ] Worktree refresh writes canonical repository output into the invoked worktree while project-keyed state selection and operation records remain shared.
- [ ] Unowned guidance remains byte-identical at global, project, and nested wiki scopes; Atomic never relocates it.
- [ ] `~/.atomic/profile.md` and `~/.atomic/wikis.md` are authoritative after one-time legacy adoption; later divergent native copies are conflicts and are never imported automatically.
- [ ] Existing user model settings, role overrides, native override files, disabled plugins, hook trust decisions, and profile guidance outside Atomic-owned blocks remain authoritative.
- [ ] A target uninstall removes only unchanged resources owned for that target and leaves shared resources while another consumer exists; a resource the removal cannot clear is reported skipped with its reason and the exact next step, and keeps its claim until `atomic harness uninstall --discard-changed` approves discarding that resource by name — a file or tree is removed, a managed block is stripped, and a settings file Atomic cannot write keeps the user's bytes while its claim is released. A directory a removed file resource emptied is removed with it, never while it holds anything else. A target key that does not parse is still removable by its raw ledger key.
- [ ] Full uninstall after the final target preserves `config.toml`, `profile.md`, `wikis.md`, and backups; removes completed operational and adoption state; and retains unresolved journals plus referenced transaction backups, ledger rows, and state-location records until recovery completes.
- [ ] A downloaded update re-executes the replacement binary before target convergence, applies and records only that binary's embedded artifact generation with the user's stored agent overrides overlaid, and never lets the stale process publish its corpus.
- [ ] An already-current normal update skips download but still converges enrolled targets; `atomic update --check` remains read-only.
- [ ] `atomic doctor` reports target instances, shared resources, unfinished journals, capability gaps, stale materializations, deliberate disablement or untrusted hooks, effective-content shadowing, and uncovered rule operations independently.
- [ ] Existing `atomic doctor` category names and numeric indices remain unchanged; multi-harness categories append with stable names and indices.
- [ ] Milestone A passes Claude and OMP runtime scenarios for global, root, nested wiki, resumed, and child sessions, including absence of duplicate Atomic or output-style application and exact rule matching behavior.
- [ ] Milestone B passes Codex runtime scenarios for custom `CODEX_HOME` roots — plugin trust and disablement, override precedence, shared skills, native agents, matching and nonmatching rules, supported tools, shell and hosted operations without safe paths, resumed and child sessions — and records the default home as a declared unsupported outcome rather than a proven one: with `CODEX_HOME` unset, explicit selection refuses naming the variable, a broad `--all` walk skips the kind without error, and no default-root package or delivery claim appears. Uncovered operations prove no matched body was delivered.
- [ ] The harness-neutral state root, state migration, harness orientation, per-harness agent overrides with native agent delivery, and session context delivery ship their Claude and OMP halves with Milestone A and their Codex halves with Milestone B. A half that depends on an unproven capability (agent discovery, hook trust, context delivery) reports `unsupported` until its runtime probe passes.
- [ ] Each adapter appends a short native-vocabulary paragraph inside the Atomic block of the steering it renders, golden-tested, naming the native tool for each behavior the canonical corpus names: for Claude Code, the Agent tool with `subagent_type` set to the agent's name, the question tool, the worktree-entry tool, and the persistent background-listener tool with `timeout_ms` and its stop tool; for OMP and Codex, only names a capability row proves. The paragraph carries tool names only, never policy, and the canonical corpus stays harness-neutral.
- [ ] Harness detection is one pure function over an environment map that reads no process state. It returns the harness, a confidence of `explicit`, `strong`, `weak`, `ambiguous`, or `none`, the evidence as variable names only (never a value), and the candidate kinds when ambiguous.

    | Harness | Strong markers | Weak markers | Never a marker |
    |---------|----------------|--------------|----------------|
    | Claude Code | `CLAUDECODE=1`, `CLAUDE_CODE_ENTRYPOINT`, `CLAUDE_CODE_SESSION_ID`, `AI_AGENT` starting `claude-code` | none | `CLAUDE_PLUGIN_ROOT`, `CLAUDE_PLUGIN_DATA`, `CLAUDE_CONFIG_DIR` |
    | Codex | `CODEX_THREAD_ID`, `CODEX_CI=1` | `CODEX_SANDBOX` | `CODEX_HOME` |
    | OMP | none | `AGENT=1` together with `CI=true` | `PI_CODING_AGENT`, `PI_CODING_AGENT_DIR`, `OMP_PROFILE` |

    Strong markers for exactly one harness return `strong`. Strong markers for two or more return `ambiguous` with no harness chosen, because markers inherit into child processes and a nested harness keeps its parent's markers. Only weak markers return `weak`; no markers return `none`.
- [ ] `ATOMIC_HARNESS` accepts `claude`, `omp`, or `codex` as explicit orientation and outranks every marker. Any other non-empty value makes a CLI verb exit 2 with a refusal that tells the user to unset it and names `ATOMIC_STATE_DIR` as the one-process state-root override; `atomic hooks session-start` ignores an invalid value and continues.
- [ ] One rule governs a detection result. `explicit` and `strong` act. `weak` acts only for a verb that reads or adds context; a verb that writes into a harness root takes no action on `weak`. On `weak`, `ambiguous`, or `none`, a verb that writes into a harness root refuses and asks for `--harness`, and on `ambiguous` or `none` a verb that only adds context adds nothing. The one exception is `atomic config agents`, which uses the single enrolled harness when exactly one is enrolled, because it writes into a harness root only through lifecycle convergence of targets that are already enrolled.
- [ ] Every hook registration Atomic writes (Claude `settings.json`, the OMP extension, Codex `hooks.json`) passes `--harness <kind>` explicitly, so no hook entrypoint depends on detection. The ownership match recognizes the unflagged and the flagged command alike, so an existing Claude registration converges to the flagged command as Atomic-owned bytes with no duplicate and no conflict.
- [ ] Orientation consumers act on detection as follows:

    | Consumer | Behavior |
    |----------|----------|
    | `atomic config agents` | `--harness` wins; otherwise an `explicit` or `strong` detection; otherwise the single enrolled harness when exactly one is enrolled; otherwise an interactive choice, or a refusal listing the enrolled kinds when non-interactive |
    | `atomic retro extract` | Reads the detected harness's session history: Claude from its projects directory; OMP and Codex report `unsupported` naming the harness until a reader for that history format is proven against real session files, instead of failing on a missing Claude directory |
    | `atomic where --json` | Adds `state_root` (the resolved absolute repository state root) and `worktrees` (defined below) |
    | `atomic bus` | Session identity from `CLAUDE_CODE_SESSION_ID` under Claude and `CODEX_THREAD_ID` under Codex; any other orientation reports that no session identity is available |
    | `atomic doctor` | Runs the active-harness leg for the detected harness in addition to every enrolled target's checks |
    | `atomic hooks session-start` | Refreshes `~/.atomic/profile.md` on every harness, because the profile and the workflow-preference autodetect are harness-neutral; seeds the output style, which writes Claude's `settings.json`, only under `--harness claude` |

- [ ] `atomic wiki` does not consult detection: it writes `~/.atomic/wikis.md` first, then projects the `<wikis>` block into the global steering of every enrolled target, and never creates `~/.claude/CLAUDE.md` on a home with no Claude target.
- [ ] `atomic where --json` `worktrees` is the recommended worktree directory for the harness passed with `--harness` or else detected. A set `worktrees` preference path outranks the harness default; otherwise it is `.claude/worktrees` for `claude` and `<state-root>/worktrees` for `omp`, `codex`, and an unknown harness. The value is repository-relative when it lies inside the repository and absolute otherwise. It is `""` only when the preference is `off`. `atomic where` builds this field through the one worktree helper keyed by harness, which `atomic workflow resolve` also calls; it reads only the preference value.
- [ ] An install on any harness creates the `~/.atomic/profile.md` stub when it is absent.
- [ ] `atomic hooks session-start` takes `--harness` and `--cwd`, reads stdin when present and never requires it, and resolves the repository from `--cwd` (else stdin `cwd`, else the process directory) through the scope marker and Git, never by treating the directory as the repository root verbatim. It resolves the realm and profile levels from the same directory and emits two sections: orientation and the resolved workflow-preferences block owned by `docs/spec/workflow-preferences.md`.
- [ ] The migration destination is the ladder's answer with the legacy candidates excluded: `ATOMIC_STATE_DIR`, the selection record, `state.dir`, else `.atomic`. When `ATOMIC_STATE_DIR`, the selection record, or `state.dir` names the legacy root itself, the user chose it and nothing moves.
- [ ] `atomic state migrate` moves `atomic.toml`, `workflow-preferences.md`, `project/`, `rules/wiki/`, and `.scratchpad/` from a legacy root (`.claude` or `.pi`) into the destination. It leaves `worktrees/` and every harness-native entry in place. It never moves the code index: it removes the legacy index directory (database, write-ahead files, MCP socket, and lock) only when it can take the MCP daemon lock; while the lock is held it leaves the directory and reports it, and `atomic code index` builds a fresh index at the destination.
- [ ] The migration moves tracked files with `git mv` and ignored files with a rename, falls back to copy-and-remove across devices, never commits, and prints the renames left unstaged. It writes no `state-location.json`. It writes the destination's managed `.gitignore` rules and leaves the legacy root's `.gitignore` in place.
- [ ] Mutating verbs (`atomic repo init`, the mutating `atomic wiki` verbs, `atomic followups add`, `atomic scratchpad new`, `atomic code index`, `atomic workflow set`) run the migration once, with a notice on stderr, when a legacy root holds an Atomic signature and the destination holds none. Read-only verbs (`atomic where`, `atomic doctor` without `--fix`, `atomic serve`) never move files and resolve to the populated legacy root until the migration runs.
- [ ] A realm root migrates the same way; a realm root that is not a Git repository migrates with plain renames. When the destination is `.atomic` the realm federation files (`code.toml`, `<key>.db`) are already in place; when it is not, `<realm>/.atomic/{code.toml,*.db}` is a second migration source and those files move to the destination. Until the migration runs, a federation reader also accepts the legacy `<realm>/.atomic/` location.
- [ ] A repository whose legacy root and destination both hold signatures refuses migration with the `atomic state adopt` instruction and moves nothing.
- [ ] A migration that runs inside a verb invalidates the process's resolution cache before the verb continues, so no later path helper resolves the pre-move root.
- [ ] The migration reports, and never rewrites, references the user wired by hand to the legacy paths, such as an `@.claude/project/followups/INDEX.md` import in a steering file or a hook script that names `.claude/project/`.
- [ ] After the move, the migration converges the enrolled targets' wiki-card projections before it returns, so Claude keeps loading the cards from `<repo>/.claude/rules/atomic-wiki/`.
- [ ] The adapter that writes a generated card projection (`<repo>/.claude/rules/atomic-wiki/`, `<repo>/.omp/rules/atomic-wiki/`) ensures `/rules/atomic-wiki/` in the `.gitignore` beside it (`.claude/.gitignore`, `.omp/.gitignore`), creating that file when absent, and commits nothing.
- [ ] The repository-state `.gitignore` written by `atomic repo init` carries `/.scratchpad/`, `/.atomic-index/`, and `/worktrees/`, because `<state-root>/worktrees/` is the recommended worktree location for OMP and Codex; `atomic repo init` also ignores `/worktrees/` in `.claude/.gitignore` when `.claude/` exists or a Claude target is enrolled, so no linked worktree appears as an untracked nested repository and an OMP-only or Codex-only repository never gains a `.claude/` directory. Every ignore-file writer computes its rules from the resolved state root, and a state root outside the repository receives no repository ignore rule.
- [ ] Realm roots resolve through the same ladder. The realm code-intel federation files (`code.toml` and the per-member databases) live in the resolved realm state root, and that root's `.gitignore` ignores `/*.db`.
- [ ] `atomic serve` exempts the resolved state root from its leading-dot directory skip, so cited cards and follow-ups stay servable under `.atomic` or a custom root.
- [ ] `~/.atomic/config.toml` holds agent overrides in per-harness tables `[claude.agents.<name>]`, `[omp.agents.<name>]`, and `[codex.agents.<name>]`, each with `model` and `effort`, decoded into one shared `AgentOverride`. A scalar under any `agents` table is a decode error. `[pi.agents]` and `atomic config resolve` stay independent.
- [ ] The adapter owns the native key:

    | Harness | Agent file | Model key | Effort key | Model value format |
    |---------|-----------|-----------|------------|--------------------|
    | Claude Code | `<root>/agents/<name>.md` | `model` | `effort` | alias, full id, or `inherit` |
    | OMP | `<profile agent root>/agents/<name>.md` | `model` | `thinking` | `provider/model[:level]`, `@role`, or a comma list |
    | Codex | `$CODEX_HOME/agents/<name>.toml` | `model` | `model_reasoning_effort` | plain model slug |

    An empty field writes no key, so the harness inherits the session's model or effort. No Atomic artifact or adapter supplies a default model or effort.
- [ ] Validation: on every harness a model is non-empty with no whitespace or control characters, and an unknown agent name is a warning. Claude effort must be one of `low`, `medium`, `high`, `xhigh`, `max` and anything else is a `Validate` error. OMP effort outside `auto`, `inherit`, `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, and Codex effort outside `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, `ultra`, are warnings.
- [ ] The agent overlay is a pure function whose output is the bytes the ledger claim digests. It runs after the offline projection, the canonical-agent policy check, validation rule P3, and the provider sweep, and its output is also the bytes written, so ledger drift and doctor agree. Projection goldens and the policy-key refusal tests stay byte-identical.
- [ ] The enrolled Claude path honors config: the Claude adapter's global artifacts and selection claims are computed from the home's stored `[claude.agents]` overrides through the overlay, and the legacy `claudeinstall` path uses the same overlay. An agent file a prior install patched with the same override is Atomic-owned, never drift.
- [ ] `atomic config list` shows every active agent override, grouped by harness.
- [ ] `atomic config agents --harness claude|omp|codex|all` edits one per-harness form (model placeholder, effort options, restart note for that harness), pre-filled from that harness's table; blank model and effort remove the entry; `all` runs one form per enrolled harness. A harness that is not enrolled is edited in config only and reported as applying when enrolled. A saved change converges each selected enrolled harness through the lifecycle convergence under the lock, journal, and ledger and patches no file in place. `claudeinstall.ReapplyAgents` runs only for an unadopted legacy Claude root.
- [ ] The review gate dispatches `atomic-reviewer` by name with no model argument, and no Atomic artifact passes an explicit model to a named Atomic agent, because a per-call model outranks the agent file on Claude and an explicit model would defeat the user's override.
- [ ] `atomic doctor` category 9 warns when an override is stored for a harness that is not enrolled, and when one is stored for a harness whose agent delivery is `unsupported`, so a stored override never fails silently. It also names `CLAUDE_CODE_SUBAGENT_MODEL` or `CLAUDE_CODE_EFFORT_LEVEL` when either is set beside a stored Claude override, because `CLAUDE_CODE_EFFORT_LEVEL` outranks the agent file's effort and `CLAUDE_CODE_SUBAGENT_MODEL` decides the model wherever the override leaves it empty.
- [ ] The OMP adapter writes `<profile agent root>/agents/<name>.md` and the Codex adapter writes `$CODEX_HOME/agents/<name>.toml`, each one ledger-owned file resource whose claim digests the overlaid bytes: a changed override converges and a hand edit conflicts. The OMP corpus store carries no agents. Agent delivery on each harness reads `unsupported` in enrollment, status, and doctor until a recorded runtime probe shows the harness lists the agent and runs it with the configured model.
- [ ] OMP session context: the generated extension runs `atomic hooks session-start --format text --harness omp --cwd <ctx.cwd>` once per extension instance at `session_start`, with an explicit working directory and a timeout of about five seconds, resolving `atomic` from `PATH`. The existing `before_agent_start` handler appends the cached text to the returned `systemPrompt` as a marked block that replaces an earlier copy; the command never runs inside `before_agent_start`.
- [ ] Codex session context: the Codex adapter writes one `SessionStart` member (matcher `startup|resume`, command `atomic hooks session-start --harness codex`) into `$CODEX_HOME/hooks.json`, ledger-owned member-wise like the Claude `settings.json` members, preserving every other byte. Atomic never fabricates hook trust; doctor reports the hook's trust state, and Codex session-context delivery reads `unsupported` until a runtime probe observes the context reaching the model.
- [ ] A session-start hook that fails, cannot find `atomic`, or times out adds nothing and never blocks or fails the session.
- [ ] Every finding in the "Review findings carried to the remediation checkpoint" list of `docs/research/harness-parity-audit.md`, and every parity row that list names, is closed by the checkpoint the list names for it (a `CP14` row unless the list names an earlier one; the PR-body nit is fixed in the PR text) with a regression test or a re-verified behavior, and each Verifies line names the finding it closes by symbol. After the last row, `atomic code comments --diff origin/next...HEAD` exits 0 and no added comment carries a checkpoint-ID token. CP14D rows clear the files flagged when their lists were taken; every P1-P8 row and every P10 row leaves its own added files clean under the comments check; CP14D17 closes any other file the tool still flags.
- [ ] Every `open` row of `docs/research/harness-parity-audit.md` names its closing checkpoint, and that checkpoint's Verifies cites it by P-ID. Rows with status `redesign` close with the redesign work that covers them; rows with status `redesign` or `accepted` are outside the parity checkpoints.
- [ ] Every audit gap open for Claude (`CP15A1` through `CP15A5`) is closed with a regression test, so a Claude user keeps every behavior `next` delivers.
- [ ] OMP and Codex receive skills, commands, rules, and global-steering features at their native locations only after a recorded runtime probe proves each location; until then each artifact kind reads `unsupported` at WARN severity in enrollment, status, rules, and doctor. Codex commands stay `unsupported` until a probe proves a non-deprecated explicit invocation surface with argument delivery.
- [ ] Runtime proof on real Claude Code, OMP, and Codex installs shows state migration from `.claude` to `.atomic`, an agent override taking effect, session context delivered, and the resolved workflow-preferences block present in model context; a half whose probe fails is recorded `unsupported` with its observation.


## Approach

Canonical instruction bodies with narrow native harness adapters — see `docs/design/omp-plugin-compatibility.md`.


## Change tree


```
planning and capability evidence
├── docs/design/omp-plugin-compatibility.md ... A  (architecture and approach)
├── docs/spec/omp-plugin-compatibility.md ..... M  (implementation contract and amendments)
├── docs/research/harness-capability-matrix.md  A  (versioned native behavior evidence; agent and session-start rows)
├── docs/research/harness-parity-audit.md .... A  (parity gaps with their closing checkpoints)
└── <state-root>/.scratchpad/omp-plugin-compatibility/
    ├── capabilities/ ........................ A  (raw CP0 observations)
    └── runtime/ ............................. A  (retained CP8 and CP16 evidence)

context/
├── AGENTS.md ................................ A  (global Atomic steering source; state-root path text)
├── CLAUDE.md ................................ D  (global source replaced by adapter projection)
├── agents/atomic-*.md ....................... M  (portable semantic metadata; no model, effort, or tools)
├── commands/*.md ............................ M  (portable invocation and dispatch outcomes; state-root path text)
├── _partials/*.md ........................... M  (shared portable orchestration blocks; state-root path text)
├── skills/atomic-*/ ......................... M  (portable paths and dependency declarations; state-root path text)
├── rules/**/*.md ............................ M  (actual path-scoped rule metadata)
└── commands/atomic-help.md .................. M  (new lifecycle discovery; state-file tour stage)

repository and realm state (outputs written into a user repository or realm)
├── <repo>/<state-root>/ ..................... A  (.atomic by default: atomic.toml, workflow-preferences.md, project/, rules/wiki/, .scratchpad/, .atomic-index/)
├── <repo>/<state-root>/.gitignore ........... A  (/.scratchpad/, /.atomic-index/, /worktrees/)
├── <repo>/.claude/.gitignore ................ M  (/worktrees/ and /rules/atomic-wiki/)
├── <repo>/.omp/.gitignore ................... A  (/rules/atomic-wiki/)
├── <repo>/.claude/rules/atomic-wiki/ ........ A  (managed Claude wiki-card projection)
├── <repo>/.omp/rules/atomic-wiki/ ........... A  (OMP wiki-card projection)
└── <realm>/<state-root>/ .................... A  (realm marker, code.toml, per-member databases ignored by /*.db)

native harness outputs (written by adapters into user homes)
├── <profile agent root>/agents/*.md ......... A  (OMP agents with overrides overlaid)
├── <profile agent root>/extensions/atomic.ts  A  (generated extension module; session-context handler)
├── $CODEX_HOME/agents/*.toml ................ A  (Codex agents with overrides overlaid)
└── $CODEX_HOME/hooks.json ................... M  (owned SessionStart member)

repository and realm steering (init and adapter outputs written into a user
repository or realm; never committed repo files here, and this repo's root
CLAUDE.md stays the contributor project overlay)
├── <repo>/AGENTS.md ......................... A  (project steering source)
├── <repo>/CLAUDE.md ......................... A  (project @AGENTS.md loader)
├── <repo>/docs/wiki/AGENTS.md ............... A  (nested wiki steering source)
├── <repo>/docs/wiki/CLAUDE.md ............... A  (nested @AGENTS.md loader)
├── <realm>/AGENTS.md ........................ A  (capture/member steering source)
├── <realm>/CLAUDE.md ........................ A  (realm @AGENTS.md loader)
├── <realm>/wiki/AGENTS.md ................... A  (realm wiki steering source)
└── <realm>/wiki/CLAUDE.md ................... A  (realm wiki @AGENTS.md loader)

atomic/internal/
├── artifacts/ ............................... A  (canonical corpus enumeration, rendering, identity, projection digests; native-vocabulary slot in renderer.go)
│   └── tests and testdata ................... A  (deterministic render and golden projections)
├── harness/
│   ├── harness.go ........................... A  (adapter contract and registry)
│   ├── agent.go ............................. A  (offline agent projections; Codex model keys)
│   ├── detect.go ............................ A  (pure environment-map harness detection)
│   ├── overlay.go ........................... A  (pure per-harness agent model and effort overlay)
│   ├── capability.go ........................ A  (agent-delivery, session-start, and artifact-discovery rows)
│   ├── uninstall.go ......................... A  (root-qualified Claude resource ids; symlink-safe block removal)
│   ├── claude/ .............................. A  (Claude projection and lifecycle adapter; card projection; overlaid agents; native-vocabulary paragraph)
│   ├── omp/ ................................. A  (OMP package, steering, profiles, rules, session adapter; native agents; session-context handler)
│   ├── codex/ ............................... A  (Codex projection and lifecycle adapter; native agents; hooks.json member)
│   └── testdata/ ............................ A  (native, migration, worktree, and non-git fixtures)
├── installstate/ ............................ A  (enrollment, ledger v2, journal, recovery; journaled target records; non-blocking lock; commit ordering)
│   └── tests and testdata ................... A  (commit-unit interruption and adoption fixtures)
├── managedfile/ ............................. A  (managed blocks, backups, safe publication)
│   └── tests and testdata ................... A  (file, block, and generated-tree fixtures)
├── rules/ ................................... A  (RuleRecord parser, matcher, validation, enforcement tiers)
│   └── tests and testdata ................... A  (overlap, containment, collision, rename, and deletion fixtures)
├── runtimeproof/ ............................ A  (isolated harness scenario runner; real-install scenarios)
├── bundlespec/bundlespec.go ................. M  (AGENTS.md and canonical steering inclusion)
├── bundlemirror/mirror.go ................... M  (Claude-native target mapping from the canonical corpus)
├── claudeinstall/ ........................... M  (legacy adoption and Claude adapter internals; shared agent overlay)
├── config/ .................................. M  (state ladder, .atomic default, candidates, legacy harness.dir as a candidate, state.dir paths, state migration, per-harness agent tables)
├── install/ ................................. M  (JSON exit status, named discard with recovery copy)
├── hooks/ ................................... M  (maintenance, collection, formatting, Claude delivery; --harness and --cwd inputs)
├── profile/ ................................. M  (authoritative profile delivery)
├── wiki/ .................................... M  (authoritative wikis.md registry; projection targets; Refresh wiring; capture section in AGENTS.md)
├── where/ ................................... M  (harness-neutral registry and state resolution; state_root and worktrees)
├── repoinit/ ................................ M  (repository root passed; worktree ignores in the state root and .claude/)
├── codeintel/cli/ ........................... M  (root-relative ignore entry)
├── codeintel/realm/ ......................... M  (federation under the resolved realm state root)
├── signals/tree.go .......................... M  (skip prefixes from the resolved root)
├── serve/walk.go ............................ M  (resolved state root exempt from the dot-directory skip)
├── retro/ ................................... M  (per-harness session history readers)
├── bus/identity.go .......................... M  (Claude and Codex session identity)
├── doctor/ .................................. M  (target, resource, journal, capability checks; active-harness leg; override and trust warnings)
├── selfupdate/ .............................. M  (enrollment-aware reconvergence)
├── validate/ ................................ M  (projection and portability validation; finding keys; perf budget)
├── **/*_test.go and serve/frontend fixtures . M  (fixtures independent of the built-in state default)
├── **/*.go with added comments .............. M  (comments cut to the comment discipline)
└── cliusage/cliusage.go ..................... M  (new command inventory)

atomic/cmd/atomic/
├── main.go .................................. M  (adapter registry composition; ATOMIC_HARNESS orientation)
├── cmd_install.go ........................... A  (top-level multi-harness install)
├── cmd_harness.go ........................... A  (enroll, adopt, status, repair, diff, uninstall, recover, rules)
├── cmd_state.go ............................. A  (state adoption, adopt --dir, migrate)
├── cmd_claude.go ............................ M  (Claude lifecycle cutover)
├── cmd_update.go ............................ M  (enrolled-target convergence; child exit status)
├── cmd_doctor.go ............................ M  (target-aware reporting; Claude config dir)
├── cmd_hooks.go ............................. M  (adapter-local session delivery; --harness and --cwd)
├── cmd_config.go ............................ M  (config agents --harness routing)
├── cmd_retro.go ............................. M  (detected-harness history)
├── cmd_where.go ............................. M  (state_root and worktrees fields)
├── cmd_wiki.go .............................. M  (projection target; Refresh wiring)
├── cmd_repo.go, cmd_followups.go,
│   cmd_scratchpad.go, cmd_code.go ........... M  (automatic state migration trigger)
├── cmd_harness_test.go ...................... A  (lifecycle and CLI contract)
└── cmd_state_test.go ........................ A  (state adoption and migration contract)

this repository's own state and wiring
├── .claude/{atomic.toml,project,rules/wiki} . D  (moved by atomic state migrate)
├── .atomic/ ................................. A  (this repository's state root)
├── .gitignore and .claude/.gitignore ........ M  (stale state rules dropped)
├── .githooks/pre-commit ..................... M  (follow-ups path from the resolved root)
├── CLAUDE.md (repository root) .............. M  (follow-ups @-ref repointed)
└── scripts/code-eval/ ....................... M  (index path from the resolved root)

user-facing and maintained documentation
├── install.sh ............................... M  (multi-harness install handoff)
├── README.md ................................ M  (supported harnesses and lifecycle; state-root path text)
├── docs/guides/install.md ................... M  (enrollment, update, uninstall; CLAUDE_CONFIG_DIR)
├── docs/guides/*.md ......................... M  (state-root path text)
├── docs/reference/commands.md ............... M  (new CLI surface)
├── docs/reference/concepts.md ............... M  (steering and adapter model; state ladder)
├── docs/reference/conventions.md ............ M  (AGENTS.md ownership; state root)
├── docs/reference/agents.md ................. M  (per-harness overrides; instruction-only restrictions)
├── docs/reference/*.md ...................... M  (state-root path text)
├── docs/spec/*.md ........................... M  (bodies naming the state default, each with a Change log entry)
├── docs/spec/install-workflow.md ............ M  (direct global projection and loader migration)
├── docs/spec/uninstall.md ................... M  (target and full uninstall)
├── docs/spec/atomic-doctor.md ............... M  (multi-target checks)
├── docs/spec/atomic-state-and-config.md ..... M  (state ladder, state.dir paths, migration, and wikis.md)
├── docs/spec/agents-effort-config.md ........ M  (per-harness agent tables, config list, --harness)
├── docs/spec/repo-init.md ................... M  (state-root ignore rules and worktree ignores)
├── docs/spec/wiki-pointer-rules.md .......... M  (canonical cards and native projections)
├── docs/spec/wiki.md ........................ M  (wikis.md authority, <wikis> projection targets)
├── docs/spec/wiki-buckets.md ................ M  (wikis.md authority, <wikis> as a projection)
├── docs/spec/atomic-validate.md ............. M  (projection gates)
├── docs/spec/output-style-seed.md ........... M  (Claude-native and OMP composition)
├── docs/spec/user-profile.md ................ M  (authoritative profile delivery; profile stub on any harness install)
├── docs/spec/atomic-where.md ................ M  (harness-neutral resolution; state_root and worktrees)
├── docs/spec/atomic-binary.md ............... M  (lifecycle verbs)
├── docs/wiki/bundle.md ...................... M  (canonical corpus and native projections)
├── docs/wiki/config.md ...................... M  (ledger and state selection)
├── docs/wiki/doctor.md ...................... M  (target-aware checks)
```


## Outline


```
context/AGENTS.md
  Atomic contract — harness-neutral global steering and authoritative references; state-root paths

context/CLAUDE.md
  Removal — obsolete global-source loader; Claude global projection is generated directly from context/AGENTS.md

repository and realm steering pairs (init and adapter outputs, not committed repo files)
  Project steering — repository-root AGENTS.md plus the adjacent thin Claude loader written on init or migration
  Wiki steering — docs/wiki/AGENTS.md plus its adjacent thin Claude loader; realm root and realm wiki pairs follow the same shape

atomic/internal/artifacts/
  Artifact — canonical identity, kind, rendered body, semantics, and source digest
  Projection — target-native bytes, delivery classification, and projection digest
  Catalog — deterministic corpus enumeration and dependency validation
  Renderer — partial expansion, native body production, and the per-adapter native-vocabulary paragraph slot
  Tests and golden testdata — canonical render, identity, dependency, and projection fixtures

atomic/internal/harness/
  Adapter — instance discovery, capabilities, projection, convergence, verification, removal
  Registry — concrete adapter lookup and target selection
  Target — harness, instance, native root, enrollment, and convergence state
  CapabilityMatrix — versioned native behavior evidence, including agent-delivery, session-start, and artifact-discovery rows
  Testdata — native capability, migration, worktree, non-git, and golden fixtures

atomic/internal/harness/detect.go
  Detect — pure function over an environment map returning harness, confidence, evidence names, and ambiguous candidates
  Policy — explicit and strong act; weak only for context; ambiguous and none refuse writes into a harness root
  Markers — strong, weak, and never-marker variables per harness
  ParseOrientation — ATOMIC_HARNESS values claude, omp, codex; any other value is an error

atomic/internal/harness/overlay.go
  Overlay — pure function over projected agent bytes and an override whose output is the digested and written bytes
  NativeKeys — per-harness model and effort key mapping

atomic/internal/harness/uninstall.go
  Resource identity — Claude resource ids qualified by native root

atomic/internal/installstate/
  Lock — ~/.atomic/install/operation.lock advisory lifecycle serialization and writer identity
  Schema — schema version, writer version, minimum reader, and incompatible-reader refusal
  Ledger — ~/.atomic/install/ledger.json target, resource, consumer, generation, tier, and applied-value records
  Journal — ~/.atomic/install/journals/<operation-id>.json observations, intended mutations, backups, and commit-unit progress
  Transaction — ~/.atomic/install/transactions/<operation-id>/{stage,backup}/ publication state
  Classifier — absent, legacy-complete/partial, v2-clean/in-flight/orphaned, and mixed state
  Recovery — oldest-first reconciliation of native state, journal state, ledger state, and later edits
  LegacyAdoption — selected-generation and block evidence, batched older-version decisions, settings/proposal conflicts, preserved snapshot, and one-time mutable-data import
  Tests and testdata — concurrency, dry-run, mixed-version, interruption, rollback, ownership, retention, and adoption fixtures

atomic/internal/managedfile/
  ManagedBlock — line-anchored ownership boundaries and ambiguity rejection
  Observation — file kind, bytes, digest, ownership, and conflict state
  Publication — temp-file writes and generated-directory publication
  Backup — durable pre-mutation copies and retention metadata
  Tests and testdata — file, block, generated-tree, publication, and backup fixtures

atomic/internal/rules/
  RuleRecord — producer-qualified identity, class, base kind, include globs, body, and checkout-independent digest
  RuleInstance — project key, absolute resolved base, record identity, and target projection state
  Parser — strict paths metadata, normalization, and unknown-key rejection without a checkout
  Matcher — containment and all-match stable ordering for exact repository-relative candidate paths
  Projection — target-native scope plus CP0-selected runtime delivery and enforcement tier
  Tests and testdata — two-repo/worktree identity, match, nonmatch, overlap, symlink, collision, rename, deletion, and malformed-source fixtures


atomic/internal/harness/claude/
  Adapter — Claude instances, native paths, projections, and lifecycle
  Steering — direct global CLAUDE.md projection plus repository and realm AGENTS.md loader management
  NativeVocabulary — golden-tested paragraph naming the Agent tool with subagent_type, the question tool, the worktree-entry tool, and the background-listener tool
  Settings — output-style and SessionStart narrow mutation; registration carries --harness claude
  Migration — global, project, and nested guidance adoption
  Cards — managed <repo>/.claude/rules/atomic-wiki/ projection of canonical wiki cards; /rules/atomic-wiki/ ensured in .claude/.gitignore
  Agents — GlobalArtifacts and SelectionClaims take the home and overlay [claude.agents] overrides

atomic/internal/claudeinstall/
  Legacy lifecycle — verified Claude adoption and adapter-compatible install semantics
  Tests and testdata — existing-install, migration, conflict, and uninstall fixtures

atomic/internal/harness/omp/
  Adapter — OMP profile discovery, enrollment, convergence, and verification
  Package — the generated corpus store: commands, skills, actual rules, and the rendered extension module; agents live in the profile agent root
  Extension — the discovered agent-root module OMP loads, ledger-owned per profile
  Steering — import-free AGENTS.md plus frontmatter-free output-style composition
  Profiles — profile-specific native roots, plugin state, and shared-package consumers
  Models — role defaults stay unsupported; per-agent overrides reach OMP through the overlaid agent files
  Agents — ledger-owned <profile agent root>/agents/<name>.md files, probe-gated delivery claim
  SessionContext — extension handler running atomic hooks session-start at session_start and appending to before_agent_start
  Ownership — extension module replaced only when the ledger records Atomic's last write; target record journaled
  Rules — canonical scope to CP0-selected native fields, exact-path matching, runtime delivery, freshness, and cleanup
  Session — CP0-selected event collection, matched-rule injection, formatting, and primary/resumed/child deduplication

atomic/internal/harness/codex/
  Adapter — CODEX_HOME discovery, enrollment, convergence, registration, trust observation, and verification
  Steering — native AGENTS.md materialization and override diagnostics
  Plugin — generated Atomic plugin package, manifest, rules index, and CP0-selected hook registration
  Hooks — proven session-baseline and pre-operation roles, bounded context return, exact-predicate deny, and uncovered-operation reporting
  Agents — canonical agent to TOML projection; ledger-owned $CODEX_HOME/agents/<name>.toml with overrides overlaid
  SessionHook — member-wise owned SessionStart entry in $CODEX_HOME/hooks.json and observed trust state
  Skills — shared-skill installation and visibility reporting
  Commands — explicit invocation projection when supported

atomic/internal/runtimeproof/
  Scenario runner — isolated homes, repositories, processes, events, and retained observations
  Real-install scenarios — state migration, override effect, session context, and preferences block on real Claude, OMP, and Codex installs

atomic/internal/config/
  StateLocation — ladder from ATOMIC_STATE_DIR through the selection record, the populated candidate, and state.dir to .atomic
  Candidates — .atomic, configured state.dir, stored harness.dir value, .claude, .pi; signatures without worktrees; ambiguity refusal
  LegacyHarnessDir — candidate name only; doctor warning that state.dir supersedes it
  StateDirPath — repository-relative multi-segment state.dir validation
  ResolutionCache — invalidation after an in-process state move
  StateMigration — destination without legacy candidates, legacy-root move including workflow-preferences.md, git mv, index removal under the daemon lock, ignore rules, reported hand-wired references
  PathHelpers — every state path built from a passed repository root; one worktree helper keyed by harness, an explicit preference path winning
  AgentOverrides — [claude|omp|codex.agents.<name>] tables into a shared AgentOverride, per-harness validation and warnings
  MutablePaths — ~/.atomic/config.toml, profile.md, wikis.md, install state, backups, and package roots
  ResolvePiAgents — independent Pi configuration preserved
  ConfigAgents — atomic config agents --harness forms, single-enrolled default, and convergence through the lifecycle
  ConfigList — active agent overrides grouped by harness

atomic/internal/wiki/
  WikiRegistry — authoritative byte-preserving ~/.atomic/wikis.md operations
  Initialization — repository `docs/wiki/` and realm `<realm>/wiki/` steering loader pairs, scope markers, Git preconditions, and capture surface
  Refresh — repository pages and wiki-pointer RuleRecord production; realm pages without scoped rule production
  NativeProjection — target convergence, per-target stale/conflict state, and no reverse import after adoption
  WikisProjection — <wikis> into every enrolled target's global steering, without detection
  CaptureSection — bucket capture section written into the shared AGENTS.md
  VerbWiring — atomic wiki verbs call Refresh and (*omp.Adapter).PublishProjectCards

atomic/internal/hooks/
  Maintenance — stateful profile refresh on every harness
  Collection — read-only profile, wiki, orientation, and workflow-preference inputs resolved from --cwd
  Formatter — pure session-context rendering of orientation and the resolved workflow-preferences block
  ClaudeDelivery — Claude settings registration and output-style selection, Claude only
  FailOpen — failure or timeout yields empty context and success

atomic/internal/doctor/
  Target checks — native registration, artifacts, guidance, roles, and disablement; Claude root from claude.DefaultConfigDir
  Active-harness leg — checks for the detected harness
  Override warnings — override for an unenrolled harness or an undelivered agent
  Codex hook trust — observed trust state of the owned SessionStart member
  Shared checks — ledger, journals, ownership, mutable authority, and visibility
  Capability checks — promised behavior, gaps, shadowing, and stale evidence
  Repairs — reuse adapter convergence instead of a separate repair path

atomic/internal/bundlespec/bundlespec.go
  SteeringSource — canonical global AGENTS.md source, direct Claude global projection, and repository/realm loader inclusion

atomic/internal/bundlemirror/mirror.go
  Enumerate — maps the canonical corpus to Claude-native targets

atomic/internal/profile/
  Profile delivery — authoritative profile state and native generation tracking

atomic/internal/where/
  Resolve — harness-neutral wiki registry and repository-state orientation
  JSON fields — state_root, and worktrees as the recommendation for the passed or detected harness, repository-relative inside the repository, absolute outside, empty only when off

atomic/internal/repoinit/
  Init — scaffold from the passed repository root; state-root ignore rules including /worktrees/; /worktrees/ in .claude/.gitignore only when .claude/ exists or a Claude target is enrolled

atomic/internal/codeintel/cli/
  EnsureGitignore — index rule computed from the resolved state root; none for a root outside the repository

atomic/internal/codeintel/realm/
  Federation paths — code.toml and member databases in the resolved realm state root; /*.db ignored; legacy <realm>/.atomic/ read until migration

atomic/internal/signals/tree.go
  Skip prefixes — scratchpad and project prefixes from the resolved root

atomic/internal/serve/walk.go
  shouldSkipDir — resolved state root exempt from the leading-dot skip

atomic/internal/retro/
  History readers — Claude projects directory; OMP and Codex unsupported until proven

atomic/internal/bus/identity.go
  Session identity — CLAUDE_CODE_SESSION_ID or CODEX_THREAD_ID by orientation

atomic/internal/install/
  Report exit — JSON mode exits non-zero on blocked or conflicted targets
  Discard — --discard-changed names each discarded resource and keeps a recovery copy

atomic/internal/selfupdate/
  Artifact convergence — enrolled-target refresh after binary selection

atomic/internal/validate/
  Projection rules — corpus portability and native projection diagnostics

atomic/internal/cliusage/cliusage.go
  Command inventory — stable rows for install, harness, and state verbs

atomic/cmd/atomic/main.go
  command registry — top-level install, harness, and state routing
  orientation gate — invalid ATOMIC_HARNESS refusal for verbs, ignored by hook entrypoints

atomic/cmd/atomic/cmd_install.go
  install — explicit multi-harness target selection and convergence
  printConvergeReports — blocked state computed before JSON encoding

atomic/cmd/atomic/cmd_harness.go
  harness list — available and enrolled instances
  harness status — target and shared-resource state
  harness enroll — explicit instance enrollment
  harness adopt claude — explicit legacy target adoption
  harness repair — explicit target convergence
  harness diff — read-only native difference report
  harness uninstall — target-scoped removal
  harness rules status — per-target tier, digest, trust, coverage, and conflict state
  harness rules sync — convergence for already-enrolled targets

atomic/cmd/atomic/cmd_state.go
  state adopt — explicit repository-state root selection, including --dir <path>
  state migrate — legacy-root move into the destination the ladder resolves without legacy candidates

atomic/cmd/atomic/cmd_repo.go, cmd_wiki.go, cmd_followups.go, cmd_scratchpad.go, cmd_code.go
  migration trigger — one automatic migration with a notice before the verb mutates state

atomic/cmd/atomic/cmd_retro.go
  retro extract — detected-harness history source

atomic/cmd/atomic/cmd_where.go
  where --json — state_root and worktrees fields

atomic/cmd/atomic/cmd_claude.go
  Claude cutover — route the existing lifecycle through the Claude adapter

atomic/cmd/atomic/cmd_update.go
  update convergence — re-exec and reconverge enrolled targets; the child's exit status propagates

atomic/cmd/atomic/cmd_doctor.go
  target reporting — stable existing categories plus appended harness checks

atomic/cmd/atomic/cmd_hooks.go
  session delivery — adapter-local maintenance and injection routing
  session-start flags — --harness, --cwd, optional stdin

context command, agent, skill, partial, and rule families
  Portable outcomes — harness-neutral workflow intent and dependencies
  Native metadata — adapter-consumable semantics without runtime wire syntax
  Agent contract — no model, effort, or tools; restrictions stated as instructions
  State-root text — .atomic default and resolved <state-root> wording

docs/design/omp-plugin-compatibility.md
  Architecture — adapter, steering, state, lifecycle, and milestone decisions

docs/spec/omp-plugin-compatibility.md
  Contract — success criteria, ordered checkpoints, and amendment log

docs/research/harness-capability-matrix.md
  Runtime evidence — tested versions, native surfaces, observed precedence, and unsupported capabilities

docs/research/harness-parity-audit.md
  Parity gaps — per-harness gaps against next, each open row naming its closing checkpoint

<state-root>/.scratchpad/omp-plugin-compatibility/capabilities/
  Raw observations — probe inputs, outputs, tested versions, and links to versioned conclusions

<state-root>/.scratchpad/omp-plugin-compatibility/runtime/
  Retained evidence — CP8 and CP16 scenario commands, paths, outputs, and result digests

repository and realm state outputs
  State root — <repo>/<state-root>/ with atomic.toml, project/, rules/wiki/, .scratchpad/, .atomic-index/
  Card projections — <repo>/.claude/rules/atomic-wiki/ and <repo>/.omp/rules/atomic-wiki/
  Realm state — <realm>/<state-root>/ with the realm marker and federation files

native harness outputs
  OMP agents — <profile agent root>/agents/<name>.md
  Codex agents — $CODEX_HOME/agents/<name>.toml
  Codex session hook — owned SessionStart member of $CODEX_HOME/hooks.json

this repository's own state and wiring
  State move — .claude/{atomic.toml,project,rules/wiki} into .atomic/
  Pre-commit hook — follow-ups path from the resolved state root
  Root CLAUDE.md — follow-ups @-ref repointed
  Code-eval scripts — index path from the resolved state root

install.sh
  Installer handoff — explicit harness selection and generic lifecycle entry point

README.md
  Product surface — supported harnesses and lifecycle

context/commands/atomic-help.md
  Discovery — install, harness, state, update, doctor, and uninstall routing; state migrate, config agents --harness, state-file tour stage

docs/guides/install.md
  Operator flow — enrollment, update, repair, adoption, and uninstall

docs/reference/commands.md
  CLI inventory — user-runnable lifecycle and state surfaces

docs/reference/concepts.md
  Architecture — canonical artifacts, native adapters, and shared resources

docs/reference/conventions.md
  Steering and state — AGENTS.md ownership and harness-neutral repository state

docs/reference/agents.md
  Overrides — per-harness tables, native keys, atomic config agents --harness, instruction-only restrictions

docs/reference/*.md, docs/guides/*.md, README.md
  State-root path text — .atomic default and resolved <state-root> wording

docs/spec/*.md
  State-root bodies — every body naming the state default, each with a Change log entry

docs/spec/agents-effort-config.md
  Override contract — per-harness tables, config list output, --harness selection

docs/spec/repo-init.md
  Layout contract — state-root ignore rules, worktree ignores in the state root and .claude/

docs/spec/wiki-pointer-rules.md
  Card contract — canonical cards and per-harness native projections

docs/spec/wiki.md
  Registry contract — wikis.md authority and <wikis> projection targets

docs/spec/install-workflow.md
  Install contract — target enrollment, adoption, and native convergence

docs/spec/uninstall.md
  Removal contract — target-scoped and full uninstall behavior

docs/spec/atomic-doctor.md
  Diagnostic contract — stable categories plus target and shared-resource checks

docs/spec/atomic-state-and-config.md
  State contract — state ladder, state.dir paths, adoption records, migration, authoritative mutable data, and overrides

docs/spec/atomic-validate.md
  Validation contract — canonical corpus and projection gates

docs/spec/output-style-seed.md
  Style contract — Claude-native selection and OMP steering composition

docs/spec/user-profile.md
  Profile contract — authoritative storage, native projection, and reconciliation

docs/spec/atomic-where.md
  Resolution contract — harness-neutral wiki and repository-state paths; state_root and worktrees fields

docs/spec/atomic-binary.md
  Binary contract — generic lifecycle commands and update re-exec

docs/wiki/bundle.md
  Bundle domain — canonical sources, render pass, and target projections

docs/wiki/config.md
  Config domain — enrollment ledger, journals, and state selection

docs/wiki/doctor.md
  Doctor domain — target-aware diagnostics and appended indices

atomic/cmd/atomic/cmd_harness_test.go
  CLI verification — enrollment, adoption, lifecycle, and instance selection

atomic/cmd/atomic/cmd_state_test.go
  State verification — adoption, override, lazy creation, persistence order, migration, and ambiguity refusal

atomic/internal/**/*_test.go and serve/frontend fixtures
  Fixtures — paths built from the fixture's own root, independent of the built-in state default
```


## Flows


**Flow: canonical rule compilation**

1. Renderer enumerates shipped `context/rules/**` and repository-refresh-generated `<state-root>/rules/wiki/**` sources.
2. Strict parser validates frontmatter, assigns producer-qualified IDs and `base_kind`, and rejects unknown or escaping scope metadata without binding a checkout.
3. For each target project, Atomic creates `RuleInstance` values with absolute resolved bases; the matcher verifies containment and returns all matching records in stable identity order.
4. Adapter projects one generation for its native surface and records instance, source, projection, enforcement-tier, and native-resource digests.
5. Validation rejects collisions, deleted-but-still-owned projections, unsupported claimed tiers, and any target mutation with an incomplete rule generation. Two-repository and worktree fixtures prove stable record identity with distinct runtime bases.

**Flow: target enrollment and global installation**

1. User runs `atomic install` or `atomic harness enroll` with explicit harness and instance selectors.
2. CLI discovers native instances and shared visibility without enrolling any newly discovered target.
3. Adapter reports capability evidence, native registration, disabled or trust state, resources visible to the selected instance, and known rule-coverage gaps.
4. Renderer produces one canonical artifact and rule generation from the selected binary.
5. Install engine observes the complete plan, writes `~/.atomic/install/journals/<operation-id>.json`, stores transaction backups, stages every projection, and validates staged bytes.
6. Claude writes its direct global `CLAUDE.md` block and native artifacts with stored agent overrides overlaid; OMP publishes its shared package as a corpus store, the profile's steering block, the profile's discovered extension module, and the profile's overlaid agent files; Codex publishes its plugin, its overlaid agent files, and its owned `hooks.json` member, and requests native registration without fabricating hook trust.
7. Adapter commits and verifies each native unit, including effective steering, discovery, rules, hooks, and deliberate disablement.
8. `~/.atomic/install/ledger.json` records physical resources, visibility, consumers, desired generations, enforcement tiers, and target state only after verification.
9. CLI reports applied, stale, disabled, untrusted, unsupported, or conflicted state per target.

**Flow: OMP steering and rule delivery**

1. Renderer reads canonical `context/AGENTS.md`, authoritative profile and wiki data, and shipped plus generated rule records.
2. Renderer parses the Atomic output-style document and discards its YAML frontmatter.
3. OMP adapter emits import-free Atomic steering, one blank line, and output-style body into the target profile's owned `AGENTS.md` block.
4. Rule projection translates canonical `paths:` to the native scope fields proven by the corresponding CP0 capability row; unproven metadata never enters the OMP package.
5. If static scope is advisory, the CP0-selected pre-operation event extracts exact operation paths, runs the canonical matcher, and supplies every matched body before the supported operation.
6. A proven deny result blocks only a machine-checkable predicate; semantic bodies remain instruction guidance.
7. A missing capability row sets that surface to `unsupported` without failing installation; adapter verification records the gap.
8. Adapter preserves profile guidance outside Atomic-owned blocks and verifies steering, rule generation, runtime delivery, and consumer digests.

**Flow: Codex plugin and rule delivery**

1. Codex adapter writes the Atomic plugin, matcher, rule index, and only the registration and event configuration selected by CP0.
2. Native registration observes the selected `CODEX_HOME`, plugin source, disablement, and review/trust status through CP0-proven interfaces.
3. Proven session-baseline events return bounded baseline context with rule IDs and digests.
4. A proven synchronous pre-operation event extracts exact candidate paths from supported structured inputs, runs the canonical matcher, and returns matched bodies through the proven context field.
5. Inputs without safely recoverable paths receive no matched rule body and record an uncovered operation; tools that bypass the selected event remain `unsupported`.
6. A proven deny result blocks only an exact machine predicate on an event it mediates.
7. Missing capability rows remain `unsupported` without blocking unrelated plugin installation.
8. Doctor reports trust, disablement, payload spill or truncation, child-session behavior, bypassed tools, and last successful runtime proof.

**Flow: legacy and partial-state migration**

1. Read-only discovery inventories native artifact trees and managed blocks, settings/hooks/style, legacy `[install]` paths and version, pre-install manifest/copies, timestamp backups, proposed/merged Claude files, profile/wiki data, v2 ledger/journals/transactions/packages/registrations, and project selection records.
2. It classifies the observed state as `absent`, structurally `legacy-complete`, `legacy-partial`, `v2-clean`, `v2-in-flight`, `v2-orphaned`, or `mixed` without creating files or enrolling a target.
3. A real adoption acquires `operation.lock`, refuses unreadable newer schemas, recovers unresolved journals oldest-first, and repeats observation. Dry-run never locks or recovers: it simulates journal-described writes in memory. Verified applied bytes and proven-unused staging yield an advisory hypothetical plan; unavailable observations, decision-relevant external/native effects, later edits/conflicts, or user choice yield `blocked_on_recovery`.
4. Exact selected-generation bytes, uniquely parseable Atomic blocks, and bytes the ledger itself recorded applying become verified ownership evidence inside the approved plan. Bytes matching neither the selected generation nor the ledger's recorded digest for that (target, resource) — the genuine older-version or foreign-writer case — are batched for replace-or-leave-unowned decisions. Missing listed files, malformed snapshots, pending Claude merge files, relative-reference changes, and orphan v2 resources require explicit resolution and remain preserved.
5. Migration preserves the write-once legacy pre-install snapshot and current unowned prose. If historical restoration evidence is absent or corrupt, the user must acknowledge that limit before adoption.
6. After explicit consent, migration writes and fsyncs the journal, current-byte backups, and parent directories. It then persists any state-root selection before switching resolution.
7. Migration renders canonical steering directly into global Claude `CLAUDE.md`, writes repository and realm `AGENTS.md` steering with adjacent thin Claude loaders, imports mutable data once, and applies target-native resources through normal commit units.
8. Effective guidance, mutable authority, native bytes, and capability state are verified before ledger ownership commits. Failure rolls forward or restores only digest-unchanged applied bytes; later edits conflict.
9. After adoption, native profile/wiki copies are one-way projections, repeated same-generation convergence is a no-op, and old Claude-only binaries are unsupported writers whose later changes surface as drift.

**Flow: repository-state selection and adoption**

1. A verb asks the resolver for the state root of a repository root it passes explicitly; no caller passes an empty root.
2. Process-only `ATOMIC_STATE_DIR` answers first. An invalid value refuses in the strict and the cached resolver alike.
3. `~/.atomic/<project-key>/state-location.json` answers next; every worktree of the clone shares it.
4. The resolver inspects the candidates `.atomic`, the configured `state.dir` path, the stored `harness.dir` value, `.claude`, and `.pi` for a signature (`atomic.toml`, `.atomic-index`, `.scratchpad`, `project`, `rules/wiki`) without creating any. Exactly one populated candidate answers in place. Two or more refuse with the `atomic state adopt` instruction.
5. With no populated candidate, the user `state.dir` answers, else `.atomic`. An empty repository gains no state directory until a stateful verb writes.
6. `atomic state adopt [--dir <path>]` writes and fsyncs the selection record before resolution switches. A failed adoption restores the prior selection only when no later state write used the new root; otherwise it records conflict.
7. Claude, OMP, Codex, shell commands, hooks, and worktrees resolve the same root. Harness detection and `ATOMIC_HARNESS` take no part; Pi agent configuration remains independent. Realm roots follow the same steps.

**Flow: repository-state migration**

1. `atomic state migrate` starts the migration explicitly. A mutating verb (`atomic repo init`, a mutating `atomic wiki` verb, `atomic followups add`, `atomic scratchpad new`, `atomic code index`, `atomic workflow set`) starts it once, printing a notice, when a legacy root (`.claude` or `.pi`) holds a signature and the destination holds none. Read-only verbs never start it and resolve to the populated legacy root.
2. The destination is the ladder's answer with the legacy candidates excluded: `ATOMIC_STATE_DIR`, the selection record, `state.dir`, else `.atomic`. When any of those three names the legacy root, nothing moves.
3. When the legacy root and the destination both hold signatures, the migration refuses with the `atomic state adopt` instruction and moves nothing.
4. It moves `atomic.toml`, `workflow-preferences.md`, `project/`, `rules/wiki/`, and `.scratchpad/` into the destination: tracked files with `git mv`, ignored files and every file under a non-Git realm root with a rename, copy-and-remove across devices. `worktrees/` and harness-native entries stay. Realm federation files stay in place under an `.atomic` destination and move with the rest to a custom one.
5. It tries the MCP daemon lock beside the legacy index. With the lock taken it removes the legacy `.atomic-index/` directory with its database, write-ahead files, socket, and lock; with the lock held by a running daemon it leaves the directory and reports it. `atomic code index` builds a fresh index at the destination.
6. It writes the destination's managed `.gitignore` rules (`/.scratchpad/`, `/.atomic-index/`, `/worktrees/`) and leaves the legacy `.gitignore` in place.
7. It invalidates the process's resolution cache, then converges every enrolled target's wiki-card projection, so Claude keeps loading the cards from `<repo>/.claude/rules/atomic-wiki/`; each projection writer ensures `/rules/atomic-wiki/` in the `.gitignore` beside it.
8. It prints the moved entries, the renames left unstaged, any index directory it left, and every hand-wired reference to a legacy path it found, without rewriting them. It writes no `state-location.json` and commits nothing.
9. The triggering verb continues against the destination.

**Flow: harness detection**

1. A shell verb passes its environment map to the detection function, which reads nothing else.
2. A valid `ATOMIC_HARNESS` (`claude`, `omp`, `codex`) returns `explicit`. Any other non-empty value returns an error: a CLI verb prints the refusal naming `ATOMIC_STATE_DIR` and exits 2.
3. Otherwise detection collects the kinds with a strong marker. One kind returns `strong`; two or more return `ambiguous` with those kinds as candidates and no harness.
4. With no strong marker, a weak marker returns `weak`; no marker returns `none`. Evidence lists variable names, never values.
5. `explicit` and `strong` act. `weak` acts only for a verb that reads or adds context; a verb that writes into a harness root takes no action on `weak` and, like on `ambiguous` or `none`, refuses and asks for `--harness`. A verb that only adds context adds nothing on `ambiguous` or `none`. `atomic config agents` alone falls back to the single enrolled harness when exactly one is enrolled. `atomic wiki` never consults detection.
6. Hook entrypoints never call detection: each registration passes `--harness`, and `atomic hooks session-start` ignores an invalid `ATOMIC_HARNESS`.

**Flow: agent override convergence**

1. The user runs `atomic config agents [--harness claude|omp|codex|all]`. Without the flag the verb takes an `explicit` or `strong` detection, else the single enrolled harness when exactly one is enrolled, else asks, and refuses non-interactively while listing the enrolled kinds.
2. The verb shows that harness's form pre-filled from its `[<harness>.agents.<name>]` tables, validates each entry with that harness's rules, and treats blank model and effort as removal.
3. It writes `~/.atomic/config.toml`, preserving unknown and user keys.
4. For each selected harness that is enrolled, it converges through the lifecycle convergence restricted to that kind: lock, recovery, observation, journal, staging, apply, verification, ledger. A harness that is not enrolled changes in config only and is reported as applying when enrolled.
5. The adapter renders the offline agent projection, runs its gates, and applies the overlay with the stored override; the overlay's output is written and digested into the claim.
6. A file whose bytes equal the ledger's recorded digest is replaced; a hand-edited file conflicts and keeps its bytes.
7. Doctor warns for a stored override whose harness is not enrolled or whose agent delivery is `unsupported`.
8. The review gate dispatches `atomic-reviewer` by name with no model argument, so the harness reads the overlaid file.

**Flow: session context delivery**

1. Claude's owned `settings.json` member runs `atomic hooks session-start --harness claude`. OMP's generated extension runs `atomic hooks session-start --format text --harness omp --cwd <ctx.cwd>` once at `session_start`. Codex's owned `hooks.json` member runs `atomic hooks session-start --harness codex` once the user trusts it.
2. The verb resolves the repository from `--cwd`, else stdin `cwd`, else the process directory, through the scope marker and Git, then the realm and profile levels.
3. On every harness it refreshes a stale `~/.atomic/profile.md`; only under `--harness claude` does it seed the output style into Claude's `settings.json`.
4. It renders the orientation section and the resolved workflow-preferences block from `docs/spec/workflow-preferences.md`, then emits the JSON envelope for Claude and Codex or plain text for OMP.
5. OMP's `before_agent_start` handler appends the cached text to `systemPrompt` as a marked block that replaces an earlier copy.
6. A failure, a missing `atomic` binary, or a timeout yields no context and a successful exit; the session continues.
7. Doctor reports Codex hook trust; until the hook is trusted nothing runs on Codex, and Codex session-context delivery stays `unsupported` until a runtime probe observes it.

**Flow: repository wiki initialization and refresh**

1. Initialization verifies a Git repository, resolves the state root, and observes root plus `docs/wiki/` steering.
2. It creates absent shared `AGENTS.md` files or owned blocks and adjacent thin Claude loaders; it never overwrites either file wholesale and rejects malformed or conflicting loaders.
3. Refresh writes `docs/wiki/scan.md`, `index.md`, domain pages, steering changes, and canonical `<state-root>/rules/wiki/**` cards into the invoked worktree.
4. Canonical wiki output commits before projection; project-keyed state and operation records remain shared across worktrees.
5. Refresh auto-converges steering and rules for already-enrolled targets only: Claude publishes the managed `<repo>/.claude/rules/atomic-wiki/` card projection, OMP publishes `<repo>/.omp/rules/atomic-wiki/` through `(*omp.Adapter).PublishProjectCards`, and Codex regenerates its matcher index.
6. Each projection records applied, stale, disabled, unsupported, or conflicted state without rolling canonical wiki output back.

**Flow: realm wiki initialization and refresh**

1. Initialization accepts a non-Git realm root but creates or adopts `<realm>/wiki/` as its own Git repository.
2. It writes `<realm>/AGENTS.md` with capture/member guidance and adjacent `<realm>/CLAUDE.md`, then writes `<realm>/wiki/AGENTS.md` with wiki guidance and adjacent `<realm>/wiki/CLAUDE.md`.
3. Registry mutation writes and verifies authoritative `~/.atomic/wikis.md` before derived target views.
4. Refresh writes canonical realm scan, member, bucket, domain, and both steering scopes without producing scoped pointer rules, then commits the wiki repository automatically.
5. Refresh auto-converges steering for already-enrolled targets; failed projections become stale or conflicted and do not roll back registry or wiki authority.
6. Ship hooks mark realms dirty without synthesizing; session-start hooks report staleness without mutating.

**Flow: update and recovery**

1. Normal update refreshes the binary when needed, re-executes the new binary, and converges every enrolled target.
2. An already-current binary skips download and still converges enrolled targets.
3. `--check` reads version and target state without writing native files, settings, journals, or ledger data.
4. If a process stops between native mutation and ledger commit, recovery compares journal observations, desired state, retained ledger rows, retained state-location records, transaction backups, and current native bytes.
5. Recovery records applied work, completes safe pending work, or reports conflict without overwriting an unrelated user change. An interrupted generated-tree publication resolves by finishing the rename when the staged tree still matches, else by restoring the digest-verified backup; a destination the ledger already records as applied is a deletion the user made after the publication committed, never the crash window, so recovery does not recreate it. A block resource whose file still carries no Atomic tags proves the write never landed. `atomic harness recover` exposes the decision explicitly and `--rollback` restores the digest-verified pre-mutation bytes. Recovery then permits cleanup of the retained operational records it consumed.

**Flow: target and full uninstall**

1. User selects one enrolled target instance or requests full Atomic removal. A key that does not parse into an enrolled target still names rows, and the row-only removal clears them by the raw key.
2. Adapter observes every owned target resource, shared visibility, and consumer relation.
3. Changed target resources are reported skipped with the reason and the exact next step and keep their claim; unchanged owned resources and consumer links are removed; a resource the removal cannot clear keeps its claim and is reported skipped. `--discard-changed` approves discarding each skipped resource by name — removing a file or tree, stripping a managed block, or releasing a settings file's claim while the user's bytes stay — and an emptied directory a removed file resource owned is removed with it.
4. Shared resources remain while another enrolled consumer exists, regardless of visibility to unenrolled instances.
5. Target steering is removed independently of plugin disablement or unlinking.
6. After the final target, full uninstall preserves `~/.atomic/config.toml`, `~/.atomic/profile.md`, `~/.atomic/wikis.md`, and backups.
7. Completed ledger, journal, transaction, and adoption state is removed. Unresolved journals, referenced transaction backups, referenced ledger rows, and referenced state-location records remain until recovery completes.


## Checkpoints


**Execution order.** Rows CP0 through CP8B are built. The remaining rows run in these phases; `WP` rows belong to `docs/spec/workflow-preferences.md`, and each row's Verifies names its dependencies.

| Phase | Rows |
|-------|------|
| P1 fixtures and flags | CP10C1 through CP10C11, CP9A |
| P2 state | CP10A, CP10B |
| P3 workflow core | WP1, WP2, WP3, WP4 |
| P4 migration and orientation | CP11, CP11B, CP9B1, CP9B2 |
| P5 reminders | WP5a, WP5b, WP6a |
| P6 references and wiring | WP7, WP8a, WP8b, WP9a, WP9b, WP10 |
| P7 agents and session context | CP12A, CP12B, CP12C, CP13A, CP13B |
| P8 sweeps and parity | WP6b, WP6c, WP6d, CP10D1 through CP10D8, CP15A1 through CP15A5, CP15B1, CP15B2 |
| P9 remediation | CP14A, CP14B1, CP14B2, CP14C, CP14D1 through CP14D17 |
| P10 proof | WP11a, WP11b, CP16 |

Rows in every phase run serially in the listed order. Phase P8 rows share files in these places: `docs/guides/install.md` (CP10D4, WP6b, CP15A1); `install.sh` (WP6b, CP15A1); `docs/spec/{atomic-state-and-config,configurable-state-paths,repo-init}.md` (WP6c or WP6d, CP10D5); `docs/spec/atomic-binary.md` (WP6c, CP10D6); `docs/spec/{serve-plans-page,documentation-as-maintenance}.md` (WP6c or WP6d, CP10D7); `docs/spec/{atomic-where,typed-followups}.md` (WP6c or WP6d, CP10D8); `atomic/internal/wiki/registry.go` (CP15A2, CP15B2). This list is not exhaustive; serial order makes any other shared file safe. Every row from P1 through P8, and every P10 row, leaves the files it adds clean under `atomic code comments`.

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| CP0 | Record versioned Claude, OMP, and Codex capability evidence | `docs/research/harness-capability-matrix.md`, scratchpad observations, spec amendments | atomic-implementer (mode: feature) | ~3 | Runtime probes map tested native names and payloads to static scope, session-baseline, pre-operation target, context-return, and deny capability rows; also record paths, registration, trust, ordering, size, lifetime, child, dedupe, reload, concurrency, failure, disablement, cleanup, visibility, and overrides. Missing rows mean `unsupported`; findings amend affected criteria/checkpoints and the Change log |
| CP1A | Add managed-resource ledger and recovery primitives | `installstate/`, `managedfile/`, tests and fixtures | atomic-implementer (mode: feature) | ~10 | Exact ledger/journal/transaction paths, file/block/generated-tree commits, interruption scenarios, full uninstall with unresolved work, referenced ledger/selection retention, post-recovery cleanup, and sidecar digest behavior pass without overwriting concurrent changes |
| CP1B | Add adapter registry, target enrollment, and ownership evidence | `harness/`, `harness/claude/`, `claudeinstall/`, `config/`, CLI tests | atomic-implementer (mode: feature) | ~12 | Discovery does not enroll; selected-generation bytes and parseable blocks can establish ownership only inside an approved plan; ambiguous targets require `harness adopt claude`; visibility and consumers differ; target removal cannot delete shared infrastructure |
| CP1C | Make repository state and mutable context harness-neutral | `config/`, `wiki/`, `where/`, `hooks/`, `profile/`, tests | atomic-implementer (mode: feature) | ~12 | Exact `~/.atomic` paths, safe-path validation, invalid-`ATOMIC_HARNESS` refusal, empty-repository laziness, persist-before-switch ordering, Pi independence, one-root resolution, one-time mutable import, post-adoption conflict behavior, and separated session operations pass |
| CP1D | Classify and adopt legacy, partial, and mixed installations | `installstate/`, `claudeinstall/`, `config/`, `hooks/`, migration fixtures | atomic-implementer (mode: feature) | ~12 | All seven states, every legacy/v2 evidence input, selected-generation/block ownership, a prior-version complete install with both replace and leave-unowned outcomes, pending proposal and corrupt-snapshot conflicts, operation lock, oldest-first recovery, filesystem-identical dry-run where applied-but-unrecorded and discardable-staging cases produce advisory plans while later-edit recovery conflict produces `blocked_on_recovery`, snapshot preservation, idempotent no-op, state-root adoption/rollback, schema refusal, and old-binary drift pass on real legacy-shaped fixtures |
| CP2A | Cut canonical steering to AGENTS.md and add offline renderers | global/root/wiki steering, `artifacts/`, `bundlespec/`, `bundlemirror/`, tests | atomic-implementer (mode: feature) | ~12 | Claude's global `CLAUDE.md` contains the direct rendered contract and has no `AGENTS.md` reference; repository and realm loaders resolve adjacent steering; OMP golden is import-free Atomic steering plus frontmatter-free style; rendered bytes and digests are deterministic |
| CP2B | Normalize the eight canonical agent definitions | eight `context/agents/atomic-*.md` files | atomic-implementer (mode: feature) | 8 | Every canonical agent states behavioral intent, capabilities, write scope, execution preference, and dependencies, with the write scope stated as an instruction and no native wire syntax |
| CP2C | Normalize shared agent composition blocks | eleven `context/_partials/agent-*.md` files | atomic-implementer (mode: feature) | 11 | Each agent partial has one owner, stable identity, portable semantics, and no duplicated harness projection |
| CP2D | Project native agents and prove the offline Codex boundary | Claude/OMP/Codex agent adapters, golden fixtures, dependency validator | atomic-implementer (mode: feature) | ~8 | Claude and OMP agents render natively; Codex TOML `developer_instructions` decodes to the canonical body; unknown required capabilities fail; user model policy remains authoritative |
| CP2E1 | Port lifecycle and review skills for Claude and OMP | `atomic-verify`, `atomic-git-discipline`, `atomic-review`, `atomic-visual-options`, `atomic-documentation`, adapters and fixtures | atomic-implementer (mode: feature) | ~8 | Skill discovery, required references, collisions, and user-disabled dependencies produce native Claude/OMP output or an explicit gap |
| CP2E2 | Port authoring and runtime skills for Claude and OMP | `atomic-writing` sources/references, `atomic-bus`, `atomic-debug`, `atomic-tdd`, adapters and fixtures | atomic-implementer (mode: feature) | ~10 | Nested references and runtime-oriented instructions resolve portably; Codex skill installation and the `atomic-wiki` family remain out of this checkpoint |
| CP2F1 | Add the canonical rule parser, instance binder, matcher, and validator | `rules/`, three `context/rules` families, fixtures | atomic-implementer (mode: feature) | ~8 | Producer IDs, base kinds, strict metadata, two-repository and worktree binding, stable source digests, containment, all-match ordering, overlap, collision, rename, deletion, and malformed-source behavior pass without target code |
| CP2F2 | Project and verify Claude scoped rules | Claude rule adapter, native fixtures, runtimeproof seam | atomic-implementer (mode: feature) | ~6 | Byte-preserving `paths:` output and runtime evidence establish native match/nonmatch, precedence, lifetime, resume, child behavior, and accurate enforcement tiers |
| CP2G | Port planning and evidence workflows | `atomic-plan`, `gather-evidence`, `pressure-test`, `challenge-swarm`, `base-resolution`, `handoff` | atomic-implementer (mode: feature) | 6 | Arguments, evidence boundaries, questions, explicit dispatch, and handoff outcomes contain no Claude-only wire syntax |
| CP2H | Port implementation and diagnosis workflows | `implement`, `subagent-implementation`, `quick-fix`, `autopilot`, `subagent-diagnose`, implementation-loop partials | atomic-implementer (mode: feature) | ~10 | Agent dispatch, concurrency, worktrees, scratchpads, review gates, and finalize behavior remain native and dependency-complete |
| CP2I | Port review and CI workflows | `review-branch`, `deslop`, `watch-ci`, review fixtures | atomic-implementer (mode: feature) | ~5 | Review scopes, severity gates, revision dispatch, and CI observation preserve explicit invocation and native agent semantics |
| CP2J | Port ship workflows | `commit`, `undo-commit`, `git-cleanup`, commit/push/PR/merge/squash/signals/git-safety/worktree partials | atomic-implementer (mode: feature) | ~13 | Message discipline, safety gates, signals refresh, worktree handling, and lifecycle outcomes remain coherent across command variants |
| CP2K | Port wiki/documentation workflows and add the repository wiki-rule producer | `refresh-wiki`, `setup-wiki`, `documentation`, all three `atomic-wiki` source/reference files, `doc-impact`, `wiki/`, rules, adapters and fixtures | atomic-implementer (mode: feature) | ~12 | Repo root/wiki and realm root/wiki steering pairs, selected state root, repository pointer cards, absence of realm pointer cards, authority-before-projection, enrolled-target auto-convergence, worktree writes, non-Git realm behavior, and referenced dependencies pass |
| CP2L | Port state and capture workflows | `retrospective-learning`, `session-report`, `follow-up` | atomic-implementer (mode: feature) | 3 | Durable state writes use the selected repository root or `~/.atomic`; session-local formatting remains adapter-owned |
| CP2M | Port existing help and issue-report workflows | `atomic-help`, `report-issue`, `report-issue-with-atomic`, `report-issue-privacy` | atomic-implementer (mode: feature) | 4 | Existing help routing and issue-report bodies are portable; issue reports preserve privacy without harness-specific paths; new lifecycle discovery remains owned by CP7E |
| CP2N | Add corpus and projection validation gates | `validate/`, projection fixtures, complete rendered corpus | atomic-implementer (mode: feature) | ~8 | One validator pass covers dependencies, portability, canonical identity, native metadata, projection digests, unsupported capabilities, and leaked Claude-only tokens |
| CP3 | Migrate verified global, project, and nested instructions | Claude adapter, ledger, managed files, config, wiki, migration fixtures | atomic-implementer (mode: feature) | ~12 | From CP1D-classified state, global Claude steering migrates directly into `~/.claude/CLAUDE.md` with no global `AGENTS.md`; repository and realm loaders preserve unowned prose and relative-reference meaning; ownership commits only after effective-content verification; later derivative changes conflict |
| CP4A | Generate and enroll the native OMP package and profile steering | OMP adapter, `cmd_harness.go`, config seams, golden fixtures | atomic-implementer (mode: feature) | ~12 | Default/named profile lifecycle preserves user settings; live `AGENTS.md` matches golden; visibility is reported separately; a generation move converges every enrolled consumer, and a claim this operation cannot move refuses before package mutation |
| CP4B1 | Add OMP static rule projection and project lifecycle | OMP package/rules, native project-rule surface, rules adapter, doctor seams | atomic-implementer (mode: feature) | ~8 | Depends on the CP0 static-scope row; canonical scope maps only to proven native fields, repository-generated cards publish recoverably, and freshness/collision/rename/delete/conflict scenarios pass. A missing row records `unsupported` without failing install |
| CP4B2 | Add and prove OMP runtime rule delivery | OMP session/rules, CP0-selected extension events, runtimeproof, doctor checks | atomic-implementer (mode: feature) | ~8 | Depends on CP0 session/pre-operation/context/deny rows; primary, resumed, and child events avoid duplicates; exact match/nonmatch, overlap, precedence, lifetime, machine deny, semantic guidance, uncovered tools, and deliberate disablement report correctly. Missing rows stay `unsupported` |
| CP6A | Verify Claude and OMP project and nested guidance | root/wiki steering, Claude/OMP adapters, worktree and non-git fixtures | atomic-implementer (mode: feature) | ~10 | Claude loads direct global steering plus repository and nested realm guidance once; OMP loads proven native equivalents and matched rules; invoked-worktree writes and both realm steering pairs pass; unsafe projections preserve prior valid output |
| CP7A | Cut generic Claude and OMP lifecycle CLI | `cmd_install.go`, `cmd_harness.go`, `cmd_claude.go`, `cliusage/`, install/uninstall packages, CLI tests | atomic-implementer (mode: feature) | ~12 | Global lifecycle serialization, exact global writes, explicit enrollment, read-only planning, adoption, staging, verification-before-ledger, repair, partial failure, shared visibility, target uninstall, preserve-data full uninstall, and referenced recovery-state retention pass without touching update or doctor behavior |
| CP7B | Make update convergence target-aware | `cmd_update.go`, `selfupdate/`, update tests | atomic-implementer (mode: feature) | ~8 | Downloaded update hands convergence to the replacement binary's embedded generation; already-current update converges separately; read-only check performs no target or ledger writes |
| CP7C | Append multi-harness doctor checks | `cmd_doctor.go`, `doctor/`, doctor fixtures and tests | atomic-implementer (mode: feature) | ~10 | Targets, shared visibility, consumers, journals, capability rows, rule tiers/digests/coverage, trust, disablement, staleness, conflict, and shadowing report independently; ledger-managed `doctor --fix` uses the global lock, oldest-first recovery, and common planner; existing category names and indices remain fixed and new categories append |
| CP7D | Amend related lifecycle and state specifications for Milestone A | nine named related `docs/spec/*.md` files | atomic-implementer (mode: feature) | 9 | Each amended spec body states current Claude/OMP global, repository, rule, wiki, state, and uninstall contracts; Change logs record superseded behavior without duplicating this spec |
| CP7E | Update Milestone A help, user docs, and domain maps | atomic-help, README, install script, guide, references, three wiki pages | atomic-implementer (mode: feature) | ~10 | Every Milestone A surface is discoverable; help-router reports zero missing commands; domain maps match implementation; release classification is correct |
| CP8A | Prove Milestone A in isolated Claude and OMP homes | `runtimeproof/`, scratchpad runtime evidence, generated fixtures | atomic-implementer (mode: feature) | ~6 | Claude-only, OMP-only, named-profile, incompatible-generation, allowed shared visibility, global install, real prior-version complete/partial and v2 in-flight/orphaned/mixed fixtures, replace and leave-unowned outcomes, pending proposal, corrupt snapshot, listed-missing drift, concurrent update/uninstall/`doctor --fix`, filesystem-identical dry-runs with advisory applied-unrecorded/staging-discard plans and blocked later-edit recovery, interrupted adoption, idempotent rerun, state-root rollback, newer-schema refusal, old-binary drift, repo/realm/worktree behavior, rule matching, primary/resumed/child, uninstall recovery, repair, and both update scenarios pass |
| CP5 | Add the Codex plugin, adapter, and hooks for Milestone B | Codex adapter/plugin/hooks, rules, shared skills, command layer, doctor checks, fixtures | atomic-implementer (mode: feature) | ~16 | Depends on CP0 registration/session/pre-operation/context/deny rows; custom `CODEX_HOME` homes, trust-pending and disabled state, AGENTS overrides, skills, TOML agents, bounded payloads, exact path extraction, and approved commands pass, while an unset `CODEX_HOME` refuses by name and is never resolved to a guessed default root. Missing rows remain `unsupported` without blocking unrelated surfaces |
| CP6B | Verify Codex guidance and rule runtime semantics | root/wiki steering, Codex adapter/hooks, runtimeproof, worktree and non-git fixtures | atomic-implementer (mode: feature) | ~10 | Guidance discovery, override precedence, match/nonmatch/overlap, mediated and bypassed tools, exact deny, payload limits/spill, reload, resume, child, dedupe, concurrency, failure, cleanup, worktree, and non-Git behavior match proven rows; shell/hosted calls without safe paths are uncovered and receive no matched body |
| CP7F | Complete Codex lifecycle discovery and documentation | command layer, doctor, selfupdate, atomic-help, guides, references, specs | atomic-implementer (mode: feature) | ~12 | Codex enrollment, trust status, update, repair, target uninstall, rule tiers, unsupported capability roles, and promised artifact surfaces are discoverable; amended specs remain current; stable CLI and doctor contracts hold |
| CP8B | Prove Milestone B in isolated Codex and mixed homes | `runtimeproof/`, scratchpad runtime evidence, generated fixtures | atomic-implementer (mode: feature) | ~6 | Codex-only and three-target scenarios extend CP8A migration fixtures with plugin trust/disablement, global install, wiki auto-convergence, two repos/worktrees, match/nonmatch/overlap, rename/delete/malformed/collision, changed derivative, mediated and uncovered operations with no false delivery claim, primary/resumed/child sessions, shared ownership, concurrent lifecycle refusal, interruption recovery, repair, and every promised capability |
| CP10C1 | Make state fixtures independent of the built-in default (1 of 11) | `cmd/atomic/{cmd_bus_test.go,cmd_claude_test.go,cmd_docs_test.go,cmd_harness_test.go,cmd_migrate_test.go,cmd_retro_test.go,cmd_state_test.go,cmd_update_converge_test.go,main_test.go}`, `claudeinstall/{install_test.go,uninstall_test.go}`, `cliutil/usage_test.go` | atomic-implementer (mode: feature) | 12 | Runs in phase P1, before CP10A. Each fixture builds its state paths from its own root or pins its segment explicitly; a file that names only Claude-native `.claude` paths (config directory, settings, worktrees, agents) is left unchanged; a test whose intent is the default value itself stays in `config` and flips in CP10A. The touched packages are green with the built-in default set to `.claude` and, in a scratch build, to `.atomic` |
| CP10C2 | Make state fixtures independent of the built-in default (2 of 11) | `codeintel/cli/{code_ignore_test.go,code_test.go,comments_internal_test.go,realm_test.go}`, `codeintel/engine/{engine_ignore_test.go,engine_test.go,sql_e2e_test.go,sql_string_match_e2e_test.go}`, `codeintel/mcp/{cwd_independent_test.go,daemon_test.go}`, `codeintel/realm/realm_test.go` | atomic-implementer (mode: feature) | 11 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C3 | Make state fixtures independent of the built-in default (3 of 11) | `config/{cli_test.go,config_test.go,harness_test.go,projectstate_test.go,repo_test.go,scope_test.go,statelocation_test.go,statemigrate_test.go}`, `dockerinit/{convergence_test.go,dockerinit_test.go}`, `docs/docs_test.go`, `doctor/checks_code_index_test.go` | atomic-implementer (mode: feature) | 12 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C4 | Make state fixtures independent of the built-in default (4 of 11) | `doctor/{checks_followups_test.go,checks_hooks_internal_test.go,checks_hooks_test.go,checks_memory_test.go,checks_migrate_test.go,checks_multiharness_internal_test.go,checks_output_style_test.go,checks_profile_test.go,checks_refs_test.go,checks_repo_config_test.go,doctor_test.go,fix_impls_internal_test.go}` | atomic-implementer (mode: feature) | 12 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C5 | Make state fixtures independent of the built-in default (5 of 11) | `doctor/{fix_test.go,format_verbose_test.go,shortcircuit_test.go}`, `followups/{add_test.go,cli_test.go,close_test.go,list_test.go}`, `harness/claude/{artifacts_test.go,claude_test.go,migration_test.go,steering_test.go}` | atomic-implementer (mode: feature) | 11 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C6 | Make state fixtures independent of the built-in default (6 of 11) | `harness/{enrollment_test.go,harness_test.go}`, `harness/omp/{omp_test.go,rules_test.go}`, `hooks/{hooks_test.go,outputstyle_test.go}`, `install/install_test.go`, `installstate/{classify_test.go,installstate_test.go,legacy_test.go,legacyfixture_test.go}` | atomic-implementer (mode: feature) | 11 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C7 | Make state fixtures independent of the built-in default (7 of 11) | `mdlink/{gitignore_internal_test.go,mdlink_test.go}`, `migrate/{steps_scratchpad_test.go,steps_test.go}`, `profile/conflict_test.go`, `repl/{action_test.go,binary_e2e_test.go}`, `repoctx/repoctx_test.go`, `repoinit/repoinit_test.go` | atomic-implementer (mode: feature) | 9 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C8 | Make state fixtures independent of the built-in default (8 of 11) | `runtimeproof/{guidance_test.go,runtimeproof_test.go,scenarios_codex8b_test.go,scenarios_determinism_test.go,scenarios_lifecycle_test.go,scenarios_migration_test.go,scenarios_mixed8b_test.go,scenarios_repo_test.go,scenarios_test.go}`, `serve/{api_bus_test.go,api_code_realm_test.go,api_code_test.go}` | atomic-implementer (mode: feature) | 12 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C9 | Make state fixtures independent of the built-in default (9 of 11) | `serve/{api_plans_page_test.go,api_plans_test.go,code_graph_members_test.go,code_members_test.go,codegraph_test.go,graph_test.go,nav_test.go,plans_test.go,scope_identity_internal_test.go,serve_test.go,walk_gitignores_test.go,walk_test.go}` | atomic-implementer (mode: feature) | 12 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C10 | Make state fixtures independent of the built-in default (10 of 11) | `serve/frontend/src/components/nav/TopBar.test.tsx`, `serve/frontend/src/components/plans/{PlansView.test.tsx,SlugView.test.tsx,VersionPicker.test.tsx,resolve.test.ts}`, `serve/frontend/src/components/rail/PlansRail.test.tsx`, `serve/frontend/src/utils/planViewStore.test.ts`, `signals/signals_test.go`, `tools/bundle-mirror/main_test.go`, `where/{projection_parse_test.go,where_authority_test.go,where_test.go}` | atomic-implementer (mode: feature) | 12 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C11 | Make state fixtures independent of the built-in default (11 of 11) | `wiki/{bucket_doc_cli_test.go,bucketdoc_test.go,discover_test.go,init_test.go,projection_seed_test.go,refresh_test.go,registry_authority_test.go,registry_test.go,stale_test.go,staleness_test.go,wiki_test.go}` | atomic-implementer (mode: feature) | 11 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP9A | Add harness detection, explicit orientation, and the session-start inputs | `harness/detect.go`, `config/statelocation.go` orientation check, `cmd/atomic/main.go`, `cmd_hooks.go` `--harness`, `--cwd`, and stdin, `hooks/hooks.go` directory resolution, `sessionStartCommand`, and `hasRegistration`, `hooks/hooks_hujson.go` `registerInSettings` and unregistration, `hooks/ownership.go` `ownedSessionStartEntries`, tests | atomic-implementer (mode: feature) | ~9 | Phase P1; depends on nothing new. Table-driven environment maps return every confidence: one strong harness, Claude markers plus Codex markers as `ambiguous` with both candidates, OMP `AGENT=1` with `CI=true` as `weak`, never-marker variables ignored, an empty map as `none`; evidence carries names only. A valid `ATOMIC_HARNESS` returns `explicit`; an invalid value exits 2 for a verb and is ignored by `atomic hooks session-start`. This row owns the `--harness`, `--cwd`, and optional-stdin inputs of `atomic hooks session-start`: `--cwd repo/sub/dir` resolves the repository root through the marker and Git. The Claude registration converges to `atomic hooks session-start --harness claude`: the ownership match recognizes both the unflagged and the flagged command, so an existing registration converges as Atomic-owned bytes with no duplicate entry and no conflict; registration rewrites an unflagged entry to the flagged command, and unregistration removes either form. No marker or orientation value changes repository-state resolution |
| CP10A | Flip the resolver to the harness-neutral ladder | `config/statelocation.go`, `config/config.go`, `config/pathsegment.go`, `config/harness.go`, `doctor/checks_config.go`, `cmd_state.go`, `config`, `doctor`, and `cmd_state` tests | atomic-implementer (mode: feature) | ~9 | Phase P2; depends on CP10C1 through CP10C11. Closes P-12 and P-13. Rungs answer in ladder order; `.atomic` is the built-in default for both the state and the legacy harness setting; `.atomic` is a candidate; `rules/wiki` is a signature and `worktrees` is not, so a worktree-only `.claude` is unpopulated; two populated candidates refuse with the `atomic state adopt` instruction; a multi-segment `state.dir` is accepted while absolute, empty, and parent-escaping values are refused; `atomic state adopt --dir` persists absolute and relative paths that linked worktrees resolve. A stored `harness.dir=.claude` with an empty `.claude` and a populated `.atomic` resolves `.atomic`: the legacy value adds a candidate name and never pins a root, and doctor warns that `state.dir` supersedes it. Tests that pin the default value flip here |
| CP10B | Pass the repository root everywhere, key worktrees by harness, and move realm federation under the resolver | `repoinit/`, `codeintel/cli/code.go`, `signals/tree.go`, `config/harness.go` worktree helper, `codeintel/realm/config.go`, `codeintel/realm/resolver.go`, `codeintel/cli/realm.go`, `doctor/checks_code_index.go`, `cmd_repo.go`, `cliusage/` help text, tests | atomic-implementer (mode: feature) | ~12 | Phase P2; depends on CP10A. Closes P-14. No production call passes an empty root to a state helper; a recorded `.pi` root is honored by `atomic repo init`, the `.gitignore` writer, and the signals skip list. One worktree helper maps harness `claude` to `.claude/worktrees` and `omp`, `codex`, or an unknown harness to `<state-root>/worktrees`, and an explicit preference path wins over both; `off` stays `off`. CP9B2, WP1, and WP3 verify that `atomic where --json` and `atomic workflow resolve` call this one helper. The state-root `.gitignore` carries `/.scratchpad/`, `/.atomic-index/`, and `/worktrees/`; `.claude/.gitignore` gains `/worktrees/` only when `.claude/` exists or a Claude target is enrolled, so an OMP-only repository gains no `.claude/`; a linked worktree under `.atomic/worktrees/` or `.claude/worktrees/` never shows as untracked in `git status`; a state root outside the repository gets no repository ignore rule. Realm `code.toml` and member databases live in the resolved realm state root, a custom realm root carries them, `/*.db` is ignored there, and a federation reader also accepts the legacy `<realm>/.atomic/` location |
| CP11 | Add `atomic state migrate`, its automatic trigger, and the card projections' ignore rules | `config/` migration engine, `cmd_state.go`, `cmd_repo.go`, `cmd_wiki.go`, `cmd_followups.go`, `cmd_scratchpad.go`, `cmd_code.go`, the migration trigger in the `atomic workflow set` entry, Claude card projection in `harness/claude/`, the OMP card projection in `harness/omp/rules.go`, `wiki/action.go`, tests | atomic-implementer (mode: feature) | ~12 | Phase P4; depends on CP10B and WP3; this row, not WP3, wires the `atomic workflow set` trigger. The destination is the ladder's answer without legacy candidates, and nothing moves when `ATOMIC_STATE_DIR`, a selection record, or `state.dir` names the legacy root; the moved set (including `workflow-preferences.md`) and the left set match the criteria; a tracked file keeps its history through `git mv`; an ignored bundle is renamed; the cross-device fallback copies and removes; a non-Git realm root migrates with plain renames, its federation files stay in place when the destination is `.atomic`, and when it is not, `<realm>/.atomic/{code.toml,*.db}` is migrated as a second source into the destination; with the MCP daemon lock free the legacy index directory is removed, and with a running daemon holding it the directory stays, is reported, and `atomic code index` then builds a fresh index at the destination; no `state-location.json` is written, nothing is committed, and unstaged renames are printed; each mutating verb including `atomic workflow set` migrates once with a notice and each read-only verb never moves a file; two populated roots refuse; a verb that triggers the move writes its own state into the new root in the same process; hand-wired references are reported; a linked worktree on an unmigrated branch keeps resolving its own legacy root; the managed `.claude/rules/atomic-wiki/` projection is published and ledger-owned; the Claude and OMP projection writers ensure `/rules/atomic-wiki/` in `.claude/.gitignore` and `.omp/.gitignore`, creating each when absent, with nothing committed; the `atomic wiki` verbs call `wiki.Refresh` and `(*omp.Adapter).PublishProjectCards` (P-43) |
| CP11B | Migrate this repository's own state | `.claude/{atomic.toml,project,rules/wiki}` to `.atomic/`, `.claude/.gitignore`, root `.gitignore`, `.githooks/pre-commit`, root `CLAUDE.md`, `scripts/code-eval/` | atomic-implementer (mode: feature) | ~7 plus renames | Phase P4; depends on CP11. `atomic state migrate` run on this repository moves its state with history; the root `.gitignore` drops its `.claude/.atomic-index/` line and `.claude/.gitignore` drops its stale state rules (`/.scratchpad/`, `/.atomic-index/`, `!/rules/wiki/` and its `**` pair) while keeping the contributor negations; the pre-commit hook regenerates the follow-ups index at the resolved path; the root `CLAUDE.md` follow-ups `@`-ref resolves and its wiki-card and `atomic.toml` path text names `.atomic`; the code-eval scripts read the index from the resolved root; `.claude/` keeps only contributor rules, skills, commands, settings, worktrees, and the generated `rules/atomic-wiki/` projection; a Claude session in this repository still loads the wiki cards through that projection; the active loop bundle under `.scratchpad/` moves with the rest, and after this row the orchestrator resolves `$SCRATCH` with `atomic scratchpad path` |
| CP9B1 | Route the detection consumers and the wiki projection | `config/cli.go` default-harness selection, `retro/`, `cmd_retro.go`, `bus/identity.go`, `doctor/shortcircuit.go`, `cmd_doctor.go`, `hooks/hooks.go`, `wiki/action.go`, `wiki/registry.go`, `cmd_wiki.go`, tests | atomic-implementer (mode: feature) | ~12 | Phase P4; depends on CP9A. The one detection rule holds: `weak` takes no action in a verb that writes into a harness root, and `atomic config agents` alone uses the single enrolled harness on `weak`, `ambiguous`, or `none`, through the selection function CP12C's `--harness` flag overrides. Closes P-47 and P-60: `retro extract` reads Claude history and reports `unsupported` on OMP-only and Codex-only homes. Closes P-45: `atomic wiki`, without consulting detection, writes `~/.atomic/wikis.md` first and projects `<wikis>` into the profile `AGENTS.md` on an OMP-only home and creates no `~/.claude/CLAUDE.md`. `bus` reads `CODEX_THREAD_ID` under Codex; doctor runs the detected harness's leg; `session-start` refreshes the profile under every `--harness` value and seeds the style only under `claude` (P-37, P-50 profile half) |
| CP9B2 | Add `state_root` and `worktrees` to `atomic where --json` | `where/`, `cmd_where.go`, `docs/spec/atomic-where.md`, tests | atomic-implementer (mode: feature) | ~5 | Phase P4; depends on CP9A, CP10B, and WP1. `state_root` is the resolved absolute state root. `worktrees` comes from the CP10B helper for the `--harness` value or else the detected harness: a set preference path outranks the harness default, and an unknown harness takes `<state-root>/worktrees`; the value is repository-relative when inside the repository and absolute otherwise; it is `""` only when the preference is `off`. The `atomic-where` spec states both fields with a Change log entry |
| CP12A | Add the override schema, overlay, and the enrolled Claude path | `config/agentoverride.go`, `config/config.go`, `config/render.go`, `harness/overlay.go`, `harness/claude/artifacts.go`, `harness/claude/evidence.go`, `harness/claude/lifecycle.go`, `claudeinstall/install.go`, `doctor/checks_config.go`, tests | atomic-implementer (mode: feature) | ~12 | Phase P7; depends on CP10B. All three tables decode into `AgentOverride` and a scalar under each is a decode error; per-harness validation matches the criteria; the overlay is pure and an empty field writes no key; no adapter supplies a default model or effort (P-1, P-20, P-22, P-40, P-53); projection goldens and the policy-key refusal, P3, and provider-sweep tests are unchanged; enrolled `atomic claude install` and `atomic harness repair` write overlaid bytes whose digest the claim records; an agent file a prior install patched with the same override adopts as owned; `config list` shows overrides by harness; doctor category 9 warns for an unenrolled harness and a set `CLAUDE_CODE_SUBAGENT_MODEL` or `CLAUDE_CODE_EFFORT_LEVEL` |
| CP12B | Deliver OMP and Codex agents natively with overlay claims | `harness/omp/enroll.go`, `harness/omp/agents.go`, `harness/omp/package.go`, `harness/codex/enroll.go`, `harness/codex/agents.go`, `harness/agent.go`, `harness/capability.go`, `doctor/checks_config.go`, status seams, tests, agent rows in `docs/research/harness-capability-matrix.md` | atomic-implementer (mode: feature) | ~11 | Phase P7; depends on CP12A. Closes P-39 and P-52: agent files land at `<profile agent root>/agents/` and `$CODEX_HOME/agents/` as ledger-owned file resources with OMP `model`/`thinking` and Codex `model`/`model_reasoning_effort` (P-40, P-53); the OMP corpus store carries no `agents/` tree; a changed override converges, a hand edit conflicts, and a target uninstall removes them; delivery reads `unsupported` in enrollment, status, and doctor until the matrix holds a passing agent row, and doctor category 9 warns for an override stored for a harness whose agent delivery is `unsupported`; the probe procedure for each harness is recorded in the matrix |
| CP12C | Add `atomic config agents --harness` and converge through the lifecycle | `config/cli.go`, `config/agents.go`, `cmd/atomic/main.go` hook wiring, `cmd/atomic/cmd_config.go`, `docs/reference/agents.md`, `docs/spec/agents-effort-config.md`, tests | atomic-implementer (mode: feature) | ~8 | Phase P7; depends on CP9B1 and CP12B. Selection follows the agent-override-convergence flow, including the single-enrolled exception and the non-interactive refusal; each harness shows its own form; a harness that is not enrolled is reported as applying when enrolled; a save converges through lock, journal, and ledger and patches nothing in place; the config package exposes no `ApplyAgentsHook`; `ReapplyAgents` runs only for an unadopted legacy root; an enrolled root under `CLAUDE_CONFIG_DIR` converges; a corpus grep finds no explicit model passed to a named Atomic agent (P-24); the reference page and spec state the per-harness contract with a Change log entry |
| CP13A | Deliver session context on OMP | `harness/omp/extension.go`, `harness/omp/session.go`, golden extension fixtures, tests | atomic-implementer (mode: feature) | ~6 | Phase P7; depends on CP9B1 and WP4. Closes P-37: the generated module runs the verb once at `session_start` with an explicit working directory and timeout and never inside `before_agent_start`; the verb refreshes `~/.atomic/profile.md` and seeds no output style; the appended block replaces an earlier copy; a failure, a timeout, or a missing binary adds nothing; resumed and child sessions resolve their own directory; the module is byte-identical across machines; the preferences section comes from the resolver `docs/spec/workflow-preferences.md` defines |
| CP13B | Deliver session context on Codex and report hook trust | `harness/codex/hooks.go`, `harness/codex/enroll.go`, ownership helpers in `hooks/`, Codex doctor checks in `doctor/`, `harness/capability.go`, tests | atomic-implementer (mode: feature) | ~8 | Phase P7; depends on CP9B1 and WP4. Closes P-50: the `SessionStart` member is written with every other `hooks.json` byte preserved and a digest over the owned member only; the verb refreshes the profile and seeds no output style; target uninstall strips only that member; an edited owned member conflicts; doctor reports trusted, untrusted, or unknown without fabricating trust; Codex session-context delivery reads `unsupported` until a runtime probe passes |
| CP10D1 | Exempt the resolved state root in serve | `serve/walk.go`, `serve/walk_test.go` | atomic-implementer (mode: feature) | 2 | Phase P8; depends on CP10A. Cards and follow-ups under `.atomic` and under a custom root are servable; other dot directories stay skipped |
| CP10D2 | Rewrite state-root text in steering, agents, and partials | `context/AGENTS.md`, `context/agents/atomic-wiki-inferrer.md`, `context/agents/atomic-strategist.md`, `context/_partials/{signals-gate,loop-finalize,implement-loop,agent-implementer-workflow,agent-search-tooling,agent-code-intel,worktree-setup}.md` | atomic-implementer (mode: feature) | ~10 | Phase P8; depends on CP11, WP6a, WP9a, WP9b, and WP10, which edit the same files. Every repository-state path reads `<state-root>` or the `.atomic` default; prose names `atomic where --json` `state_root` as the source of `<state-root>`; Claude-native `.claude/worktrees` and settings text is unchanged; `atomic validate artifacts` passes |
| CP10D3 | Rewrite state-root text in commands and skill references | `context/commands/{refresh-wiki,deslop,atomic-plan,subagent-diagnose,documentation,retrospective-learning,atomic-help,setup-wiki}.md`, `context/skills/atomic-wiki/references/repo.md` | atomic-implementer (mode: feature) | ~9 | Phase P8; depends on CP10D2. Same criterion as CP10D2; the `/atomic-help` state-file tour stage and `state-root` topic row name `.atomic`, `atomic state migrate`, and `atomic state adopt --dir`; the help-router check reports zero missing commands |
| CP10D4 | Rewrite state-root text in reference pages, guides, and README | `docs/reference/{atomic-toml,concepts,conventions,repo-wiki,realm-wiki,code-intel,repl,commands,serve}.md`, `docs/guides/{code-intel-mcp,getting-started,install}.md`, `README.md` | atomic-implementer (mode: feature) | ~13 | Phase P8; depends on CP11 and WP6b. Every page states the `.atomic` default, the ladder, `atomic state migrate`, and the worktree locations; the VitePress build passes |
| CP10D5 | Amend specs whose body states the `.claude` state default: resolution and layout | `docs/spec/{atomic-state-and-config,configurable-state-paths,repo-init,wiki-pointer-rules,follow-ups-folder,scope-marker,wiki,signals-router,signals-workflow,graphignore,wiki-storage-relocation,atomic-doctor}.md` | atomic-implementer (mode: feature) | 12 | Phase P8; depends on CP11, WP6c, and WP6d. In each file, every body mention outside `## Change log` that states a `.claude/` state subpath as current truth is rewritten to the current ladder, layout, and ignore rules, with a Change log entry and no history in the body; a mention that names a legacy migration source stays; `atomic validate spec` passes |
| CP10D6 | Amend specs whose body states the `.claude` state default: binary, code intelligence, validation | `docs/spec/{artifact-renames,atomic-binary,atomic-repl,atomic-validate,code-intel-engine,code-intel-extraction,code-intel-integration,code-intel-realm,code-intel-substrate,code-intel-surfaces,code-mcp-per-repo,comment-counter}.md` | atomic-implementer (mode: feature) | 12 | Phase P8; depends on CP11 and WP6c; same criterion as CP10D5 |
| CP10D7 | Amend specs whose body states the `.claude` state default: documentation, install, serve | `docs/spec/{document-templates,documentation-as-maintenance,documentation-skill-split,embedded-sql-extraction,embedded-sql-language-expansion,followup-hardening,install-workflow,retro-extract,selfupdate-state,serve-bus-chat,serve-plans-page,serve-realm-ux}.md` | atomic-implementer (mode: feature) | 12 | Phase P8; depends on CP11, WP6c, and WP6d; same criterion as CP10D5 |
| CP10D8 | Amend specs whose body states the `.claude` state default: where, signals, remaining | `docs/spec/{atomic-where,signals-project-detection,signals-wiki-linkify,sql-language-support,subagent-diagnose,typed-followups,uninstall,wiki-deterministic-setup}.md` | atomic-implementer (mode: feature) | 8 | Phase P8; depends on CP9B2, CP11, WP6c, and WP6d; same criterion as CP10D5 |
| CP15A1 | Close the open Claude lifecycle gaps | `cmd_update.go`, `cmd_claude.go`, `cmd_install.go`, `install/install.go`, `installstate/legacy.go`, `harness/claude/lifecycle.go`, `install.sh`, `docs/guides/install.md`, `docs/spec/user-profile.md`, tests | atomic-implementer (mode: feature) | ~10 | Phase P8; depends on CP12C, WP6b, and CP10D4, which share `install.sh` and `docs/guides/install.md`. Rows with status `redesign` or `accepted` are out of scope for CP15A1 through CP15A5, as are P-9, P-17, P-21, and P-25, which CP14B1 and CP14C close. Each gap has a regression test that fails before the fix: P-2 (the already-current path on an un-adopted install runs migrations and the post-update doctor), P-3, P-4, P-5, P-6, P-7, and P-10 (an install on any harness creates the profile stub when absent, and `config.toml` and the pre-install manifest where `next` did); the `user-profile` spec states that an install on any harness creates the profile stub, with a Change log entry; the audit gains a status line per closed row |
| CP15A2 | Close the open Claude wiki-registry and reporting gaps | `wiki/registry.go`, `harness/claude/migration.go`, `wiki/init.go`, `doctor/checks_refs.go`, `harness/claude/lifecycle.go`, `harness/capability.go`, `runtimeproof/`, tests | atomic-implementer (mode: feature) | ~9 | Phase P8; depends on CP15A1. Closes P-15, P-16, P-28, P-33, and P-34 with a regression test each; P-33 and P-34 carry a Claude runtime observation; the audit gains a status line per closed row |
| CP15A3 | Close the open Claude wiki-pipeline text gaps | `context/agents/atomic-wiki-inferrer.md`, `context/skills/atomic-wiki/references/repo.md`, `context/commands/refresh-wiki.md`, `context/commands/setup-wiki.md`, tests | atomic-implementer (mode: feature) | ~5 | Phase P8; depends on CP10D3, WP6a, WP9a, WP9b, and WP10, which edit the same files. Closes P-26 and P-27: the inferrer resolves the `atomic-wiki` reference path from a source the runtime reports, and the router ref lands in the steering file Claude loads, while the canonical source stays harness-neutral; `atomic validate artifacts` passes |
| CP15A4 | Render the native vocabulary paragraph | `harness/claude/` steering rendering, `artifacts/renderer.go`, `harness/omp/steering.go`, steering goldens, `context/commands/watch-ci.md`, `context/skills/atomic-bus/SKILL.md`, tests | atomic-implementer (mode: feature) | ~10 | Phase P8; depends on CP15A3. Closes P-29, P-30, P-23, and P-22b: the Claude steering block ends with a golden-tested paragraph that names the Agent tool with `subagent_type` set to the agent name (an omitted one falls to a general-purpose agent without the agent's skills), the question tool, the worktree-entry tool, and the persistent background-listener tool with `timeout_ms` and its stop tool; the OMP block names only tools a capability row proves; the paragraph carries tool names only; `/watch-ci` and `atomic-bus` describe the background dispatch and the listener so the paragraph's names apply; the canonical corpus stays harness-neutral and `atomic validate artifacts` reports no leaked Claude-only token |
| CP15A5 | Fix the dispatch wording the vocabulary relies on | `context/skills/atomic-wiki/references/repo.md`, `context/agents/atomic-wiki-inferrer.md`, `context/commands/{autopilot,implement,quick-fix,subagent-implementation,review-branch,deslop,refresh-wiki,subagent-diagnose}.md` | atomic-implementer (mode: feature) | ~10 | Phase P8; depends on CP15A4. Closes the remaining P-29 wording: every dispatch of an Atomic agent names the agent, so the vocabulary paragraph maps it to `subagent_type`; `atomic validate artifacts` passes |
| CP15B1 | Deliver OMP skills, commands, rules, and steering features behind probes | `harness/omp/package.go`, `harness/omp/enroll.go`, `harness/omp/rules.go`, `harness/omp/steering.go`, `harness/capability.go`, `doctor/`, `docs/research/harness-capability-matrix.md`, runtime-proof probes, tests | atomic-implementer (mode: feature) | ~10 | Phase P8; depends on CP11, CP12B, and CP13A. A probe per artifact kind is recorded, and each proven kind is written to its native location as ledger-owned resources and removed by target uninstall: P-38 commands, P-41 skills, P-42 matched rule bodies, P-35 and P-44 `@` imports in the profile `AGENTS.md`, P-36 output-style selection. Each unproven kind reads `unsupported` at WARN severity in enrollment, status, rules, and doctor (P-46) |
| CP15B2 | Deliver Codex steering, skills, commands, rules, and registration behind probes | `harness/codex/plugin.go`, `harness/codex/enroll.go`, `harness/codex/register.go`, `harness/codex/steering.go`, `wiki/registry.go`, `harness/capability.go`, `doctor/`, `docs/research/harness-capability-matrix.md`, runtime-proof probes, tests | atomic-implementer (mode: feature) | ~11 | Phase P8; depends on CP9B1, CP12B, CP13B, and CP15A2, which shares `wiki/registry.go`. Same probe rule as CP15B1 for P-48 (global `$CODEX_HOME/AGENTS.md` managed block, probed with `codex debug prompt-input`), P-49 output style, the Codex native-vocabulary paragraph inside that block (only tools a capability row proves), P-54 skills, P-55 rule hooks, P-56 wiki cards, P-57 `@` imports, and P-61 plugin registration, where `Register` and `DefaultRegister` gain their production caller (review no-caller finding). Closes P-58: once the global block exists, `atomic wiki` projects `<wikis>` into it for an enrolled Codex target. P-51 commands stay `unsupported` without a proven explicit invocation surface; P-59 doctor category 24 prints the remediation for each `unsupported` surface; `install --harness codex` never reports `applied` for a surface it did not write |
| CP14A | Close the blocking review findings and the state-resolution risks | `cmd_install.go`, `cmd_update.go`, `install/uninstall.go`, `install/install.go`, `config/statelocation.go`, `cmd/atomic/main.go`, tests | atomic-implementer (mode: feature) | ~8 | Phase P9; depends on CP15A2, CP15A5, and CP15B2. Closes `printConvergeReports`: `install`, `enroll`, `repair`, `adopt`, and `rules sync` with `--json` exit non-zero when any target is blocked or conflicted. Closes `runUpdateWith` post-swap handling (P-2, post-swap half): `atomic update` exits with the re-executed child's status and prints no repair advice when the child printed its own instruction. Closes the `Steps.removalOptions` `Confirm` callback: `--discard-changed --yes` refuses unless the approved plan names every resource it discards, and each discarded resource keeps a recovery copy under `~/.atomic/backups`. Closes `resolveRepositoryStateDir`: an invalid `ATOMIC_STATE_DIR` refuses in the cached resolver. Closes `obsoleteHarnessRefusal` and `refuseObsoleteHarnessEnv` (P-11), re-verifying CP9A: `atomic hooks session-start` with an invalid `ATOMIC_HARNESS` exits 0 and still injects, and the verb refusal tells the user to unset `ATOMIC_HARNESS` and use `ATOMIC_STATE_DIR` for a one-process state override |
| CP14B1 | Close the adapter and doctor review risks | `doctor/shortcircuit.go`, `cmd_doctor.go`, `doctor/checks_profile.go`, `hooks/hooks.go`, `harness/omp/enroll.go`, `harness/codex/enroll.go`, `installstate/recovery.go`, `harness/claude/artifacts.go`, `harness/uninstall.go`, `docs/guides/install.md`, tests | atomic-implementer (mode: feature) | ~12 | Phase P9; depends on CP14A. Closes `ClaudeHomeMissing` (P-9): with `CLAUDE_CONFIG_DIR` set, doctor and the session-start hook resolve the Claude root through the Claude adapter's config-directory resolution, and the install guide documents the variable. Closes the `(*omp.Adapter).Enroll` extension publication: a user-authored or user-edited `extensions/atomic.ts` takes the replace-or-leave-unowned decision instead of being replaced. Closes the `(*omp.Adapter).Enroll` and `(*codex.Adapter).Enroll` target records: a crash between publication and `Complete` leaves a target record recovery restores, so update and repair still see the target. Closes `planTargetRemoval` shared-consumer counting: Claude resource ids are qualified by native root, so uninstalling one of two enrolled Claude roots removes that root's unchanged files |
| CP14B2 | Close the install-state, validation, and dead-code review risks | `harness/uninstall.go`, `installstate/lock.go`, `installstate/transaction.go`, `installstate/recovery.go`, `validate/projection.go`, `validate/dispatch_test.go`, `config/paths.go`, `harness/claude/steering.go`, tests | atomic-implementer (mode: feature) | ~10 | Phase P9; depends on CP14B1. Closes `removeBlock`: a whitespace remainder behind a symlinked `CLAUDE.md` or `AGENTS.md` is written through the link and the link survives. Closes `installstate.AcquireLock`: it tries a non-blocking lock first and, when another process holds it, prints the holder from `ReadLockIdentity` before it waits. Closes `(*Transaction).Complete` ordering: units are journaled committed before the ledger write, so `atomic harness recover --rollback` after a crash in that window leaves no ledger row disagreeing with the restored bytes. Closes `TestPerfBudget`: the whole-repo budget holds locally under `make test`, and its comment states the budget's reason or is deleted. Closes the `projGate` finding keys: a rule failing only the runtime-index or tier check counts as FAIL against its source, not as PASS. Closes the no-caller finding: `config.RemoveBackupStamp` is deleted; the `claude.MigrateScope` relocate path is deleted; `installstate.ReadLockIdentity` is used by the lock fix above; codex `Register` and `DefaultRegister` are wired by CP15B2; `(*omp.Adapter).PublishProjectCards`, `wiki.Refresh`, and `pruneCards` are wired by CP11 into the wiki verbs |
| CP14C | Close the wiki and corpus review risks | `wiki/action.go`, `docs/reference/realm-wiki.md`, `docs/guides/knowledge-base.md`, `context/agents/atomic-wiki-writer.md`, `docs/reference/agents.md`, `context/commands/atomic-help.md`, `docs/spec/wiki.md`, `docs/spec/wiki-buckets.md`, tests | atomic-implementer (mode: feature) | ~9 | Phase P9; depends on CP14B2 and CP10D5. Closes `wikiBucketAddAction` (P-17): `bucket add` writes the capture section and placeholder into the shared `<realm>/AGENTS.md`, and both pages say so. Closes the no-caller finding for `wiki.Refresh`, `pruneCards`, and `(*omp.Adapter).PublishProjectCards`, re-verifying CP11. Closes the `atomic-wiki-writer` no-fan-out claim (P-21) in the agent and the reference page: the restriction is stated as an instruction. Closes the truncated `/atomic-help` `update` row (P-25). Closes the `wiki` and `wiki-buckets` spec registry statements: both bodies name `~/.atomic/wikis.md` as the authority and the `<wikis>` block as a projection, each with a Change log entry |
| CP14D1 | Cut added comments to the comment discipline (1 of 17) | `cmd/atomic/{cmd_claude.go,cmd_claude_test.go,cmd_harness.go,cmd_harness_test.go,cmd_install.go,cmd_state.go,cmd_update.go,cmd_update_converge_test.go,main.go}` | atomic-implementer (mode: feature) | 9 | Runs in phase P9; depends on CP14C. In each listed file every comment the branch added fits `[comments] max_lines` or is cut to the decision it carries or a pointer to a doc, and no added comment carries a checkpoint-ID token (`CP0`, `CP5`, `CP7A`, ...). Behavior and tests are unchanged |
| CP14D2 | Cut added comments to the comment discipline (2 of 17) | `artifacts/{agent_test.go,artifact.go,catalog.go,renderer.go,renderer_test.go}`, `bundlemirror/mirror.go`, `bundlespec/bundlespec.go`, `claudeinstall/evidence.go` | atomic-implementer (mode: feature) | 8 | Runs in phase P9; depends on CP14D1; same criterion as CP14D1 |
| CP14D3 | Cut added comments to the comment discipline (3 of 17) | `config/{config.go,harness.go,paths.go,statelocation.go,statelocation_test.go}`, `doctor/{checks_capabilities.go,checks_codex.go,checks_conflicts.go,checks_hooks.go,checks_install.go,checks_journals.go,checks_multiharness.go}` | atomic-implementer (mode: feature) | 12 | Runs in phase P9; depends on CP14D2; same criterion as CP14D1 |
| CP14D4 | Cut added comments to the comment discipline (4 of 17) | `doctor/{checks_multiharness_internal_test.go,checks_output_style.go,checks_profile.go,checks_repo_config.go,checks_repo_config_test.go,checks_resources.go,checks_rules.go,checks_shadowing.go,checks_signals.go,checks_signals_test.go,checks_staleness.go,checks_targets.go}` | atomic-implementer (mode: feature) | 12 | Runs in phase P9; depends on CP14D3; same criterion as CP14D1 |
| CP14D5 | Cut added comments to the comment discipline (5 of 17) | `doctor/{checks_trust.go,doctor.go,fix.go,fix_converge_internal_test.go,fix_impls.go,fix_test.go,shortcircuit.go,stub_adapter_internal_test.go}`, `embeddedcorpus/{embeddedcorpus.go,embeddedcorpus_test.go}`, `harness/{agent.go,agent_test.go}` | atomic-implementer (mode: feature) | 12 | Runs in phase P9; depends on CP14D4; same criterion as CP14D1 |
| CP14D6 | Cut added comments to the comment discipline (6 of 17) | `harness/{capability.go,enrollment.go,evidence.go,generation.go,generation_test.go,harness.go,harness_test.go,lifecycle.go,provider.go,registry.go,resource.go,rule.go}` | atomic-implementer (mode: feature) | 12 | Runs in phase P9; depends on CP14D5; same criterion as CP14D1 |
| CP14D7 | Cut added comments to the comment discipline (7 of 17) | `harness/{rule_test.go,selection.go,skill.go,skill_test.go,status.go,target.go,uninstall.go}` | atomic-implementer (mode: feature) | 7 | Runs in phase P9; depends on CP14D6; same criterion as CP14D1 |
| CP14D8 | Cut added comments to the comment discipline (8 of 17) | `harness/claude/{artifacts.go,artifacts_test.go,claude.go,claude_test.go,evidence.go,lifecycle.go,migration.go,migration_test.go,steering.go,steering_test.go}`, `harness/codex/{codex.go,codex_test.go}` | atomic-implementer (mode: feature) | 12 | Runs in phase P9; depends on CP14D7; same criterion as CP14D1 |
| CP14D9 | Cut added comments to the comment discipline (9 of 17) | `harness/codex/{enroll.go,enroll_test.go,lifecycle.go,matcher.go,matcher_scenarios_test.go,plugin.go,plugin_test.go,register.go,register_test.go,status.go,status_test.go}`, `harness/omp/enroll.go` | atomic-implementer (mode: feature) | 12 | Runs in phase P9; depends on CP14D8; same criterion as CP14D1 |
| CP14D10 | Cut added comments to the comment discipline (10 of 17) | `harness/omp/{extension.go,lifecycle.go,models.go,omp.go,omp_test.go,package.go,package_test.go,rules.go,rules_test.go,session.go,session_test.go,steering.go}` | atomic-implementer (mode: feature) | 12 | Runs in phase P9; depends on CP14D9; same criterion as CP14D1 |
| CP14D11 | Cut added comments to the comment discipline (11 of 17) | `hooks/{hooks.go,outputstyle.go,ownership.go}`, `install/{install.go,install_test.go,report.go,rules.go,uninstall.go}`, `installstate/{classify.go,classify_test.go,installstate_test.go,journal.go}` | atomic-implementer (mode: feature) | 12 | Runs in phase P9; depends on CP14D10; same criterion as CP14D1 |
| CP14D12 | Cut added comments to the comment discipline (12 of 17) | `installstate/{ledger.go,legacy.go,legacy_test.go,legacyfixture_test.go,lock.go,recovery.go,retention.go,schema.go,target.go,transaction.go}` | atomic-implementer (mode: feature) | 10 | Runs in phase P9; depends on CP14D11; same criterion as CP14D1 |
| CP14D13 | Cut added comments to the comment discipline (13 of 17) | `managedfile/{backup.go,evidence.go,managedfile.go,managedfile_test.go,observe.go,publish.go}`, `profile/conflict.go`, `repl/binary_e2e_test.go` | atomic-implementer (mode: feature) | 8 | Runs in phase P9; depends on CP14D12; same criterion as CP14D1 |
| CP14D14 | Cut added comments to the comment discipline (14 of 17) | `rules/{bind.go,corpus_test.go,match.go,match_test.go,parse.go,pattern.go,pattern_test.go,project.go,rules.go}`, `runtimeproof/{codex_test.go,guidance_test.go,omp.go}` | atomic-implementer (mode: feature) | 12 | Runs in phase P9; depends on CP14D13; same criterion as CP14D1 |
| CP14D15 | Cut added comments to the comment discipline (15 of 17) | `runtimeproof/{omp_test.go,runtimeproof.go,runtimeproof_test.go,scenarios_codex8b_test.go,scenarios_determinism_test.go,scenarios_lifecycle_test.go,scenarios_migration_test.go,scenarios_mixed8b_test.go,scenarios_repo_test.go,scenarios_session_test.go,scenarios_test.go}` | atomic-implementer (mode: feature) | 11 | Runs in phase P9; depends on CP14D14; same criterion as CP14D1 |
| CP14D16 | Cut added comments to the comment discipline (16 of 17) | `signals/{signals.go,signals_test.go}`, `validate/{config.go,projection.go,projection_internal_test.go,projection_test.go}`, `where/{projection_parse_test.go,where.go,where_authority_test.go}` | atomic-implementer (mode: feature) | 9 | Runs in phase P9; depends on CP14D15; same criterion as CP14D1 |
| CP14D17 | Cut added comments to the comment discipline (17 of 17) | `wiki/{action.go,init.go,init_test.go,projection_seed_test.go,refresh.go,registry.go,registry_authority_test.go,wiki.go}` | atomic-implementer (mode: feature) | 8 | Runs in phase P9; depends on CP14D16; same criterion as CP14D1. This row also cuts any other file the tool still flags, so after it `atomic code comments --diff origin/next...HEAD` exits 0 and a grep of added comment lines finds no checkpoint-ID token |
| CP16 | Prove the redesign on real Claude Code, OMP, and Codex installs | `runtimeproof/`, `<state-root>/.scratchpad/omp-plugin-compatibility/runtime/`, `docs/research/harness-capability-matrix.md` | atomic-implementer (mode: feature) | ~6 | Phase P10; depends on WP1, WP4, WP9b, CP11, CP12C, CP13A, CP13B, CP15B1, CP15B2, and CP14D17. `omp` is installed for the proof. On each real install the retained evidence shows a repository migrated from `.claude` to `.atomic`, an agent running with its configured override, session context in model context, and the resolved workflow-preferences block present, each with the command, harness version, and output digest; it re-runs the evidence of every `redesign` audit row that CP9A through CP15B2 closed; a half whose probe fails is recorded `unsupported` and the matrix row updated |


## Risks


| Risk | Severity | Likelihood | Mitigation |
|------|----------|------------|------------|
| Harness behavior differs from accepted file syntax | high | high | CP0, CP8A, and CP8B use observable runtime scenarios for discovery, path extraction, precedence, child delivery, hooks, trust, and session behavior |
| Instruction migration changes or deletes user guidance | high | med | Move only known Atomic-owned content; preserve unowned prose; block ambiguous destination, duplicate applicability, malformed markers, and changed relative references |
| Existing legacy or partial state is misclassified | high | med | CP1D uses all legacy/v2 evidence inputs, real prior-version fixtures, seven explicit states, selected-generation/block verification only, read-only planning, and per-resource consent for older or ambiguous artifacts |
| Concurrent lifecycle operations or stale journals race | high | low | Install, update, repair, adoption, uninstall, and ledger-managed `doctor --fix` acquire one advisory lifecycle lock, recover oldest-first, and re-observe after locking before any plan can mutate |
| A corrupt legacy snapshot creates false rollback confidence | high | low | Preserve it, block automatic adoption, require explicit acknowledgment, and never claim restoration evidence that cannot be verified |
| An older binary writes after v2 migration | med | med | Version schemas, refuse incompatible readers, mark old binaries unsupported, and surface later writes as drift or conflict |
| A partial install leaves native state and the ledger inconsistent | med | low | Write durable observations and backups before mutation; validate staging; journal each commit unit; verify native effect before ledger commit |
| A shared package or skill changes an unenrolled instance's visible surface | med | med | Record native visibility separately from consumers; report allowed visibility before mutation; refuse a generation claim the operation cannot move |
| Native edits to profile or wiki sections are overwritten | med | med | Import only during explicit legacy adoption; afterward keep `profile.md` and `wikis.md` authoritative and report derivative divergence as conflict |
| Corpus portability edits change Claude behavior | med | high | Preserve unchanged bytes and use contract scenarios for approved prose changes; keep one canonical body rather than parallel variants |
| Model or reasoning defaults override user policy | med | low | Canonical agents carry no model or effort; Atomic writes only the user's stored override, and only into Atomic-owned agent files; native user settings such as OMP `task.agentModelOverrides` keep outranking it |
| Rule projection claims parity from metadata alone | high | med | Report explicit enforcement tiers and require matching, nonmatching, overlap, lifetime, precedence, resumed-session, child-delivery, and bypass evidence per tested version |
| A semantic rule is mistaken for a security boundary | high | med | Hooks deterministically activate prose but deny only exact machine predicates; documentation and doctor label guidance and unsupported operations |
| Codex or OMP hooks are disabled, untrusted, bypassed, or payload-limited | high | high | Observe native state; never fabricate trust; bound payloads; report uncovered tools and stale runtime proof; preserve the last valid projection |
| Canonical wiki output succeeds while one target projection fails | med | med | Commit authority first, mark only the failed enrolled target stale or conflicted, and let repair reuse the same convergence planner |
| Update or doctor silently converges disabled targets | low | low | Distinguish deliberate disablement from repairable drift; normal update reports but does not re-enable without explicit repair policy |
| Scope expands into unsupported Codex behavior before OMP release | med | med | Milestone A requires only offline Codex agent proof; Milestone B gates every Codex runtime promise independently |
| State migration loses or strands user state | high | med | Move only the named Atomic entries; `git mv` tracked files so history follows; refuse when both roots are populated; never commit; print unstaged renames; delete only the regenerable code index; write no `state-location.json`, so linked worktrees on unmigrated branches keep resolving their own root |
| A process keeps resolving the pre-move root | med | med | The migration invalidates the resolution cache before its verb continues; read-only verbs never migrate; CP11 tests a verb that migrates and then writes |
| A nested harness is misidentified | med | med | Detection reports `ambiguous` when two harnesses carry strong markers and never guesses; hook registrations pass `--harness`; writing verbs refuse on `ambiguous` or `none` |
| Harness environment variables mask an agent override | med | med | `CLAUDE_CODE_EFFORT_LEVEL` outranks a Claude agent file's effort and `CLAUDE_CODE_SUBAGENT_MODEL` fills an empty model; doctor names a set variable beside a stored override, and the review gate passes no model argument |
| Codex hook delivery stays unverified | high | high | Never fabricate trust; doctor reports trust state; Codex session context reads `unsupported` until a runtime probe observes it, and the steering fallback in `docs/spec/workflow-preferences.md` covers an untrusted hook |
| An agent override is silently inert before delivery | med | high | Doctor category 9 warns for an override on an unenrolled harness or an undelivered agent; agent delivery reads `unsupported` until its probe passes |


## Change log


### 2026-10-21 — Harness-neutral state root, orientation, per-harness agent policy, session context

**What changed:** Repository and realm state resolves through a harness-neutral ladder ending in `.atomic`; a stored `harness.dir` is only a candidate name; `state.dir` takes multi-segment paths and `atomic state adopt --dir` any path. `atomic state migrate` moves Atomic's entries out of `.claude` or `.pi`, automatically from mutating verbs, without a selection record. Worktrees stay ignored but are not state. Wiki cards reach Claude through a managed `.claude/rules/atomic-wiki/` projection. A pure detection function and an `ATOMIC_HARNESS` accepting `claude`, `omp`, or `codex` orient shell verbs under one rule; `atomic wiki` projects to every enrolled target without detection; `where --json` gains `state_root` and `worktrees`. Per-harness agent overrides overlay every projected agent, and OMP and Codex agents land natively behind probes. Session context reaches OMP and Codex, with the profile refreshed on every harness. Adapters append a native-vocabulary paragraph. Review findings and audit gaps each name a closing checkpoint, ordered by an execution-order table.

**Why:** The PR #268 parity audit and the redesign research found state tied to Claude's directory (a worktree-only `.claude` read as populated, and empty-root callers bypassed the selection record), every "current harness" decision hard-coded to `~/.claude`, overrides reaching only the legacy Claude writer while OMP and Codex never received agents where they read them, no session context on OMP or Codex, and three blocking defects in the PR review.

**Superseded:** The built-in state default was `.claude`, the candidates were `.claude` and `.pi` with `worktrees` as a signature, `state.dir` was one segment, and the non-goals excluded renaming the state directory; repository-state migration only adopted a root in place; any `ATOMIC_HARNESS` value was refused; agent projection carried no user override and OMP agents existed only in the corpus store; the non-goal read "concrete provider or model selection"; Claude loaded wiki cards from the canonical directory in place; CP2L included `remind-me` and session-start collection read reminders; session-start profile refresh ran only on Claude; `harness.dir` fed the state ladder as migration evidence; the Goal named only steering, artifacts, configuration, and lifecycle; the model-risk mitigation was "store semantic preferences without concrete provider IDs; mutate only missing adapter defaults"; CP2B verified "enforcement strength", and CP7E and CP7F listed a changelog item the 2026-09-19 correction had already ruled out.

**Removed:** the criterion "Agent projection preserves behavioral intent, required capabilities, write scope, execution preference, and whether restrictions are native or instruction-only", because no agent carries a native restriction: every restriction is an instruction.

**Removed:** the relocation of unowned guidance (`claude.MigrateScope` relocate path), because no verb reaches it and Atomic leaves unowned guidance where it is.

### 2026-09-20 — Architect review: pins, discard, disjointness, tree deletion, extension directory

**What changed:** the architect review's remaining P2/P3 findings, each with the regression that distinguishes it.

- **An unparseable target key is a row to rewrite, not a pin.** `enrolledTarget` matched a row to an enrolled target through `ParseKey` + `FindTarget`, so a row an older writer left under a key this binary cannot parse was a permanent refusal that could name no target. `SharedConsumers` and `EnsureSharedGeneration` now agree on one rule and one set: a row whose target key parses and is not enrolled is the genuine pin and still refuses, naming the raw key; a row whose key does not parse is rewritten with the enrolled consumers. `EnsureSharedGeneration` returns that rewrite set, `AssertSharedConsumers` proves the enrollment converged every member onto the selected generation in the same commit, and `PlanTargetRowRemoval`/`RemoveTargetRows` give the stale key a row-only removal exit the CLI reaches. A `Row.Resource`/`Claim.ID` divergence is now reported (`Resource.LedgerMismatch`) instead of reading as unowned bytes, because the ledger lookup is by identity and never by path.
- **A skip is transitional, not terminal.** `atomic harness uninstall --discard-changed` releases Atomic's claim on each resource the default plan skipped, after confirming that resource by name; a file or tree is removed, a managed block is stripped, and a settings file Atomic cannot write keeps the user's bytes while its claim is released. Without the flag nothing changes, and the skip line now names the reason and the exact next step rather than a bare id.
- **`Plan.Unproven` has one producer per surface, enforced.** `mergeUnproven` dedupes a repeat inside one producer and fails loudly when the package gaps and the runtime delivery both name a surface, so the single-owner rule is a check rather than a comment.
- **Recovery does not resurrect a committed tree deletion.** `treeWindow` finishes an interrupted rename only while the ledger does not already record the intended digest at that path; once it does, the destination existed and the user deleted it afterwards, so the absence is a conflict.
- **The extension directory it creates is removed with the extension.** A removal prunes the native directory its own file resource emptied, bounded by the target's native root and only when nothing else remains in it.

**Why:** a stale row with an unparseable key deadlocked every shared-package move and could not be cleared; a drifted resource left a target enrolled forever with no stated exit; the `Plan.Unproven` single-owner rule lived only in a comment; completing an interrupted publication could recreate a tree the user deleted; and the `extensions/` directory the delivered module created outlived it.

**Superseded:** the generation refusal pinned on any row it could not match to an enrolled target; a skipped resource had no removal path and the skip line stated no resolution; `Plan.Unproven` appended both producers unchecked; `treeWindow` treated every absent destination with a matching stage as the crash window; and `removeResource` deleted only the extension file. Criteria 59 and 73 and the update/recovery and target/full-uninstall flows carried the superseded wording.

### 2026-09-20 — Legacy update refusal, `.pi` resolution, enrolled-writer cutover, journal retention

**What changed:** the legacy-flow regressions and the lifecycle-correctness fixes the review flagged as P1-6/P1-7/P2-1/P2-2/P2-3/P2-6.

- **A legacy Claude install stops updating silently.** `atomic update` converges only enrolled targets; when none is enrolled but the default Claude root carries legacy install evidence, it prints `atomic harness adopt claude` to stderr and exits non-zero instead of a no-op that froze the artifacts. The legacy re-write path is retired for an enrolled target, so refreshing an un-adopted one would rewrite owned bytes with no ledger row — the drift the next converge reads as unrepairable. `docs/guides/install.md` states the refusal. A home with neither an installation nor an enrollment stays a silent no-op.
- **The quiet repo-local resolver adopts one populated candidate.** `resolveRepositoryStateDir` gains the read-only single-populated-candidate rung between the selection record and the configured/default rungs, so a repository whose only state root sits under a former harness directory (for example `.pi`) keeps resolving it instead of sprouting a fresh `.claude` beside it. Two populated candidates stay ambiguous: the quiet form falls through to the built-in default and `atomic state adopt` remains the explicit resolver. The retired `ATOMIC_HARNESS` variable now surfaces its refusal once at the root command, before any verb runs, instead of being ignored by the quiet resolver. Criteria 66 and 70 state it.
- **`atomic claude install|update` cut over on an enrolled root.** When the resolved Claude root is ledger-enrolled, both verbs converge it through `install.Steps` — the same lock, journal, and ledger every other lifecycle verb uses — and print the planner's report including `unsupported` rows. Only an un-adopted install still reaches `claudeinstall`. `atomic doctor --fix` for categories 1/2/14 already routed through the enrolled converge. The `--no-hooks` flag applies to the legacy path only.
- **Scope migration takes the lock and records block ownership.** `MigrateScope` acquires the lifecycle lock and recovers unresolved journals before it observes the scope, and an approved relocation records `AGENTS.md` as a block-owned resource (publishing the merged bytes once, with the row digest covering only the managed block), so a later `harness uninstall` strips the block instead of deleting the prose the user approved moving. Relocation remains reachable only through the migration engine API; the CLI path that writes project and realm loader pairs is repository/realm wiki initialization, and no CLI verb wires `MigrationRequest.Scopes` today. Criteria 70 and the scope-migration flow state the corrected ownership.
- **The one-time mutable import runs under the lock.** `ImportMutable` moved into `Adopt`'s locked section: it runs after recovery and only for a post-recovery plan that is ready or already converged, so a refused or conflicted adoption can never have written `~/.atomic/profile.md` or `~/.atomic/wikis.md`.
- **Consumed operations stop accumulating.** Every transaction owner (`applyAdoption`, `MigrateScope`, OMP enroll, OMP project cards, Codex enroll) calls `CleanupOperation` after a successful `Complete`, and both `recoverAll` and `RollbackJournals` call it after `ConsumeJournal`, so completed journals and transaction stage/backup trees no longer accumulate per operation.
- **Smaller corrections.** `applyAdoption` commits its rows with `consumerOf` from the start, so a crash between the commit and `recordVerifiedOwnership` cannot leave a row with an empty consumer. `Package.Render` chmods the tree explicitly so its digest is umask-independent. Codex registration observes a deliberately disabled plugin before `plugin add` and records the materialized cache as an owned tree row. Enrolled `--instance` matching cleans both sides so a trailing slash names the same root. `atomic state adopt` resolves the repository root through `repoctx` and prints an absolute `--dir` correctly.

**Why:** an existing Claude-only user's update did nothing and said nothing; a `.pi` user's repo-local verbs silently switched to a fresh `.claude`; the CLI's legacy Claude verbs wrote owned bytes outside the ledger; scope migration claimed whole-file ownership of user prose; the mutable import ran before the lock; and every converge left its journal and transaction tree behind forever.

**Superseded:** an empty enrolled set made `atomic update` a silent no-op; the quiet resolver had no discovery rung and ignored the retired harness variable; `atomic claude install|update` always used the legacy writer; a relocated `AGENTS.md` was journaled as `KindFile`; `ImportMutable` ran before `Adopt` took the lock; and completed operations were retained until a full uninstall. Criteria 58, 66, 70, 74, and the update/scope flows carried the superseded wording.

### 2026-09-20 — Shared-generation convergence and honest OMP delivery

**What changed:** Two corrections to the shared-resource lifecycle, plus the truth work each one forced.

- **Shared generations converge by requirement, not by record.** `harness.EnsureSharedGeneration` treats a recorded generation as what Atomic last wrote, not as a consumer's requirement: with one binary and one operation, every enrolled consumer of a shared resource requires the selected generation, so the plan that publishes it also upserts each enrolled consumer's package row in the same commit (`omp` and `codex` enrollment). A row for a target the ledger does not enroll is the one genuine pin and still refuses before mutation, naming both sides. Criterion 59 states it. Regressions: two OMP profiles and two `CODEX_HOME` roots each converge across a corpus swap, and persist `TestIncompatibleGenerationRefusesBeforeMutation` / `TestEnrollRefusesIncompatibleSharedGeneration` keep the pin refusal.
- **OMP delivers the artifact OMP loads.** Enrollment now publishes the rendered runtime module at the profile agent root's `extensions/atomic.ts` — the one location discovery was observed for — as a ledger-owned file resource of that profile, and records `EnrollResult.ExtensionPath`. `~/.atomic/packages/omp/atomic` remains Atomic's corpus store with no discovery claim. Package installation, registration, and command/agent/skill discovery are reported `unsupported` in `EnrollResult.Unproven`, `harness status`, `harness rules status`, `atomic doctor` category 19, and the install/repair/status CLI output. Criterion 41 states it. The runtime proof now enrolls through the production adapter and launches the layout enrollment produced; nothing in `runtimeproof` hand-writes the module.
- **The delivered block tells the truth about units and location.** The session-baseline bound is the static index measured in the unit the generated module evaluates (UTF-16 code units), not bytes over a different string, so the plan's `suppressed` verdict and the module's decision cannot disagree. The block names the package tree its rule bodies live in instead of the agent root, and each index line carries the rule's source path. Named profiles are reachable in discovery through the documented `OMP_PROFILE` selector the user already has set, rather than being a test-only concept.

**Why:** two consumers of one shared package could never move to a new generation — each refused because the other recorded the old one — so every update after the first two enrollments reported `conflicted`; and the OMP package was published where OMP never loads it, so `atomic install --harness omp` delivered steering only while the runtime proof passed by hand-writing a layout production never produced.

**Superseded:** the shared-generation comparison was row-versus-plan per consumer and refused on any other recorded generation (criterion 59); the OMP package's generated commands, agents, skills, and rules read as discovery-delivered (criterion 41); the runtime proof wrote the extension itself; and the baseline bound was described in bytes over the index text alone.

### 2026-09-20 — Planner evidence, per-resource drift, zero-tags steering, recovery completion

**What changed:** Four corrections to the lifecycle planner and recovery, each with a regression over a real adapter.

- **Ledger-recorded ownership evidence.** A claim whose observed bytes equal the digest the ledger recorded applying to its `(target, resource)` is Atomic-owned: the planner replaces it without a batch decision. Bytes matching neither the selected generation nor that recorded digest stay on the explicit replace-or-leave-unowned path. Criterion 49 and the legacy-migration flow state the rule. The ledger row is proof of Atomic's own last write, not the historical digest catalog the design excludes, because it records only what the selected line of binaries actually wrote.
- **Per-resource drift.** A ledger row whose bytes no longer match is reported through `Classification.Drift` — a changed resource is a replace-or-leave-unowned decision, an absent one is `ActionRecreate` — instead of forcing `mixed`. `mixed` is reserved for unreadable v2 evidence and unjournaled remnants overlapping legacy evidence. `atomic harness repair`, `install --replace`, and uninstall therefore reach the resource they exist to fix; a target uninstall reports a drifted resource `skipped` and keeps its claim. Criterion 46 and the uninstall criterion in [`uninstall.md`](./uninstall.md) state it.
- **Zero-tags steering means no block yet.** `Observe` keeps returning `ConflictMalformedBlock` for a file with no block, but `managedfile.Assess` maps that case to `EvidenceNoBlock` — adoption appends — and `omp.project` no longer blocks it, so a pre-populated `~/.claude/CLAUDE.md` or profile `AGENTS.md` enrolls and preserves every existing byte. Tags that do not parse to one block still refuse. Criterion 51.
- **Recovery resolves the tree crash window.** `forwardDecision` finishes an interrupted generated-tree publication when the staged tree still digests to the intended value, restores the digest-verified transaction backup otherwise, and reports a conflict only when neither holds; a block unit whose file still has no Atomic tags proves the write never landed. `atomic harness recover [--rollback]` exposes the decision (lock and recovery first; read-only under `--dry-run`), and every lifecycle conflict message names it. Criterion 53 and the update/recovery flow state it.

**Why:** every release blocked Claude convergence for an already-adopted target, any drifted owned file made the install `mixed` and unrepairable, a user's own steering file blocked first install, and a crash inside a tree publication left a conflict with no resolver — the four most common real-world paths.

**Superseded:** ownership evidence was selected-generation bytes and parseable blocks only; a row/bytes disagreement forced `mixed` with an unconditional blocker; a block resource with zero Atomic tags was ambiguous; and a tree publication interrupted between its two renames always reported an unresolvable conflict. The `## Flows` sections and criteria 46, 49, 51, and 53 carried the superseded wording.

### 2026-09-15 — Complete rule and lifecycle architecture


**What changed:** Added checkout-independent `RuleRecord` sources plus per-project `RuleInstance` binding, strict parsing and matching, per-harness enforcement tiers, CP0-selected OMP and Codex plugin delivery roles, exact install-state paths, global install commit order, repository and realm wiki initialization/refresh paths, one-way mutable authority, shared OMP visibility reporting, and preserve-data full uninstall with unresolved-recovery retention. Split static projection from runtime proof and expanded scenario coverage.

**Why:** The approved draft described projections but did not bind rule activation, runtime scope, global installation writes, wiki mutation ownership, or recovery behavior precisely enough to implement without inventing policy or native API semantics.

**Superseded:** Replaced implicit post-install native re-import with one-time legacy adoption followed by authoritative-to-derived convergence. Replaced checkout-bound rule bases with runtime instances, generic OMP/Codex API assumptions with CP0 capability roles, realm pointer-rule ambiguity with repository-only production, and unconditional operational cleanup with recovery-aware retention.

### 2026-09-15 — Bind legacy migration and resolve strategy questions


**What changed:** Added a seven-state migration classifier, explicit legacy and v2 evidence inputs, selected-generation/block ownership rules, batched prior-version decisions, global lifecycle locking including ledger-managed doctor repair, oldest-first recovery, byte-identical dry-run with in-memory recovery simulation and explicit blockers, preserved legacy snapshots, state-root adoption and rollback conditions, schema compatibility, mixed-version handling, CP1D, and expanded CP8 migration scenarios. Replaced open design questions with committed Claude, OMP, and Codex decisions plus CP0 evidence boundaries.

**Why:** The prior lifecycle architecture covered verified adoption but not incomplete legacy installs, interrupted v2 operations, orphan resources, concurrent lifecycle commands, damaged recovery evidence, or old binaries writing after migration.

**Superseded:** Replaced generic legacy adoption with explicit classification and per-resource ownership decisions. Replaced unspecified migration serialization and compatibility with one global lock, versioned state schemas, and conservative conflict handling.

### 2026-09-17 — Corpus enumeration owned by `artifacts.Load`

**What changed:** CP2A enumerates the canonical corpus in `artifacts.Load` and renders it once; `bundlemirror` now maps that already-rendered corpus to Claude-native targets rather than walking `context/` itself. The change tree and outline name the `artifacts/` package as the enumerator and describe `bundlemirror.Enumerate` as the Claude target mapping.

**Why:** The cutover moved partial expansion and enumeration ahead of projection, so a design or spec that still named `bundlemirror` as the corpus enumerator described the pre-cutover pipeline.

**Superseded:** `bundlemirror` was the sole corpus enumerator, reading and rendering each matching file during its walk; enumeration and rendering now happen once in `artifacts.Load`, and `bundlemirror` consumes the result.

### 2026-09-19 — Steering pairs are outputs; Codex default home is a declared boundary

**Correction:** the change tree's "repository steering" rows named repo-root `AGENTS.md`/`CLAUDE.md`, `docs/wiki/AGENTS.md`, and `docs/wiki/CLAUDE.md` as files this change adds, and the tree and outline carried an `atomic/CHANGELOG.md` release-entry row. None of those repo files is created here: repository and realm steering pairs are init and adapter outputs written into a user's repository or realm, while this repository's root `CLAUDE.md` stays the contributor project overlay. Release visibility comes from the commit type under release-please, so a changelog file edit is not part of this change.

**What changed:** `wiki.InitRepoScope` and `wiki.InitRealmScope` write the scope's loader pair — the shared `AGENTS.md` carrying the steering scaffold and the adjacent blank-bracketed `CLAUDE.md` import of it, rendered from `bundlespec.ScopeSteering` — instead of a lone `CLAUDE.md`; an existing `CLAUDE.md` (the pre-pair shape) stays byte-identical and keeps loading. Criterion 54 now states the Codex boundary the runtime evidence supports: a custom `CODEX_HOME` is proven while an unset `CODEX_HOME` is a declared unsupported outcome that refuses by name and is skipped by `--all`, never resolved to a guessed default root. The change tree and outline describe the steering pairs as outputs and drop the changelog row.

**Why:** the audit found the recorded change tree describing files HEAD never gained, a release-entry claim no commit supports, and a criterion claiming default-home Codex coverage that CP0/CP8 never observed. The init path also contradicted the shipped steering-pair contract, so the code was converted rather than the contract weakened.

### 2026-09-19 — Wiki steering-pair consumers reconciled

**What changed:** The consumers that still assumed a lone `docs/wiki/CLAUDE.md` steering file now key off the pair. `atomic/internal/doctor/checks_signals.go` excludes `docs/wiki/AGENTS.md` from the router orphan check alongside `index.md`, `scan.md`, and `CLAUDE.md`; `atomic/internal/signals/signals.go`'s linkify skip list adds `AGENTS.md` while keeping `CLAUDE.md`; `context/agents/atomic-wiki-inferrer.md` names the shared `AGENTS.md` as authoritative steering with the `CLAUDE.md` loader as fallback, and the `signals-gate` partial passes a `<steering>` block to the silent dispatch. `docs/wiki/wiki.md`'s `init` row now reads "steering loader pair".

**Why:** moving the scaffold into `AGENTS.md` changed which file carries guidance, so an initialized repo otherwise reported its steering file as an orphan domain — suppressing category 3's freshness result — linkify rewrote the file the pipeline promises to leave alone, and silent refreshes read the blank loader instead of the user's steering.


### 2026-09-20 — Claude settings ownership is member-wise

**What changed:** A Claude target's `settings.json` is now one ledger resource recorded whenever a Claude target converges (adapter converge during enroll, repair, or update, and the explicit legacy adoption), with a digest over only the Atomic-owned members — the inline `SessionStart` registration and the `outputStyle` seed. Target and full uninstall remove exactly those members and leave the user's other keys byte-for-byte; a later edit to an owned member is a changed resource and refuses the operation. A member-cleared `settings.json` is drift convergence repairs rather than mixed ownership evidence, so it never blocks a repair, and a removal that cannot write a read-only `settings.json` reports the resource skipped and keeps its claim for a later uninstall. The ownership table in [`docs/design/omp-plugin-compatibility.md`](../design/omp-plugin-compatibility.md) carries the new resource row.

**Why:** the converge-time hook registration and style seed were not ledger-owned, so `atomic harness uninstall` left them behind — F-7 in the loop's follow-up ledger.

**Superseded:** owned native resources were whole-file or whole-block bytes only; converge-time settings mutations had no ledger row and survived a target uninstall.

## Implementation log

### implemented — 2026-09-20

Built across 53 iterations of `/subagent-implementation` (loop base `af7f2f6b`). Commits (chronological):

- `7b0d9d0c` — CP0 capability matrix + reproducible replay evidence (Claude/OMP/Codex)
- `979158c7` — CP1A managedfile + installstate primitives
- `63d0185c` — CP1B adapter registry, enrollment, ownership evidence
- `9d91ddc5` — CP1C harness-neutral repository state and mutable context
- `6e78da12` — CP1D migration classifier and legacy adoption
- `629119d3` — CP2A canonical steering cut to AGENTS.md + offline renderers
- `9119505a` — CP2B eight agent definitions normalized
- `cfa0a67d` — CP2C agent composition partials normalized
- `50269332` — CP2D native agent projections + offline Codex boundary
- `dd49bab8` — CP2E1 lifecycle/review skills ported
- `2a6c4388` — CP2E2 authoring/runtime skills ported
- `bfe48224` — CP2F1 canonical rule parser/binder/matcher/validator
- `0581f253` — CP2F2 Claude scoped-rule projection + runtimeproof seam
- `0e7156a6` — CP2G planning/evidence workflows ported
- `d65fc452` — CP2H implementation/diagnosis workflows ported
- `4420ac04` — CP2I review/CI workflows ported
- `37ff9398` — CP2J ship workflows ported
- `aed3681f` — CP2K wiki workflows ported + repository wiki-rule producer
- `2be2f2b1` — CP2L state/capture workflows ported
- `98a5039f` — CP2M help/issue-report workflows ported
- `d1e2f5b7` — CP2N corpus + projection validation gate
- `4f58f5f7` — CP3 verified instruction migration
- `6a2bf53b` — CP4A native OMP package generation and enrollment
- `fdf38edc` — CP4B1 OMP static rule projection + card lifecycle
- `ec2cdf96` — CP4B2 OMP runtime rule delivery + proof
- `e82161a5` — CP6A Claude/OMP guidance verification (loader defect fixed)
- `69569fc7` — CP7A generic Claude/OMP lifecycle CLI
- `2b88a6b9` — CP7B target-aware update convergence
- `89f279c5` — CP7C multi-harness doctor checks (15–24)
- `0c8a7668` — CP7D related spec amendments
- `eb500961` — CP7E Milestone A help/docs/release surfaces
- `10574096` — CP8A Milestone A proof (35 scenarios)
- `185d25c6` — CP5 Codex plugin adapter (capability-disabled)
- `56ef77da` — CP6B Codex offline verification matrix
- `7bfb3846` — repl e2e fixture cleanup (retired ATOMIC_HARNESS pin)
- `bb34af1c` — CP7F Codex lifecycle discovery + doctor category 24
- `a4dcfecf` — CP8B Milestone B proof (19 scenarios)
- `94af1e62` — finalization docs alignment
- `00e8a4eb` — finalize-audit fixes (wiki steering pairs, Codex default-home boundary, spec/STATE truth)
- `05f22f9d` — follow-up polish (settings-member ownership, docs/spec currency, recorder naming)

**Out-of-scope work performed during this build:**

- `7bfb3846` cleanup of a pre-existing uncommitted repl fixture edit (retired env pin) — it blocked a clean working tree for every checkpoint and every reviewer had certified it correct.

**Unforeseens — surprises that emerged during implementation:**

- Claude never resolves an `@` import that sits inside the `<atomic>` HTML block — the scope loader delivered nothing until the body was blank-bracketed (found by CP6A's runtime matrix, not by unit tests).
- `atomic wiki init` still wrote a lone `docs/wiki/CLAUDE.md`; converted to the loader pair in the audit round, with doctor orphan/linkify/inferrer consumers reconciled.
- Repository-local generated-tree publication failed cross-device (HOME-rooted staging); `managedfile.PublishDir` gained a copy+fsync fallback (ramdisk-proved).
- Ledger-owned settings members needed their own drift class: owned-member drift is repairable, not `mixed` evidence, or repair would refuse what it exists to fix.
- Codex 0.147.0 authenticated in isolation but had the configured model rejected before any hook event; every runtime surface stayed capability-disabled and uncovered.
- The `--all --harness` combination silently dropped the named harness; now refused.

**Deferred items still open:**

- F-10 (path-independent resource ids for corpus/settings rows) tracked in the scratchpad ledger.

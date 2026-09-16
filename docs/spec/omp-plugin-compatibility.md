# Atomic multi-harness compatibility


## Goal


Atomic installs one authored policy and workflow corpus into Claude Code and OMP through native steering, artifact, configuration, and lifecycle surfaces. The same architecture produces Codex-native instructions and agents without making Claude directories or semantics authoritative.


## Non-goals


- A universal workflow interpreter or common runtime tool API.
- Separate authored prompt trees for each harness.
- Global steering or output style represented as a rule.
- Concrete provider or model selection for users.
- OMP package publication to a registry in Milestone A.
- Repository-state directory renaming in Milestone A.
- Windows support.


## Success criteria


- [ ] `context/` remains the only authored artifact corpus, and template expansion occurs once before harness projection.
- [ ] `context/AGENTS.md` is the sole authored global Atomic contract; the Claude adapter renders it directly into `~/.claude/CLAUDE.md` without creating or referencing `~/.claude/AGENTS.md`.
- [ ] Only repository and nested realm Claude steering uses thin `CLAUDE.md` references to adjacent `AGENTS.md` files, without duplicate instruction delivery.
- [ ] Each enrolled OMP profile receives one native `AGENTS.md` whose Atomic-owned content is an import-free rendered Atomic steering body, one blank-line separator, and the Atomic output-style body with parsed YAML frontmatter excluded.
- [ ] OMP steering and output style are absent from plugin rules and packaged output-style artifacts; only actual path-scoped rules use the rule projection surface.
- [ ] Canonical commands, agents, skills, and rules have stable identities and source digests; generated native artifacts record source and projection digests.
- [ ] `RuleRecord` parses every shipped `context/rules/**` source into producer-qualified identity, class, `base_kind`, ordered include globs, body, and source digest without checkout-specific paths; target projection metadata remains separate.
- [ ] `RuleInstance` binds a record to one project key and absolute, symlink-resolved runtime base without changing record identity or source digest.
- [ ] Rule parsing rejects unknown scope keys, absolute patterns, parent-escaping patterns, target-name collisions, and malformed frontmatter before any target mutation.
- [ ] Canonical matching verifies resolved containment and supplies every overlapping matched record in stable identity order; two repositories and multiple worktrees share record identity but not runtime bases.
- [ ] Repository wiki refresh is the second canonical rule producer and writes pipeline-owned pointer cards under `<state-root>/rules/wiki/**`; realm refresh produces no scoped pointer rules.
- [ ] Claude projects shipped rules and repository-generated wiki cards through native scoped-rule behavior only after CP0 proves matching, precedence, lifetime, resumed-session, and child-session behavior.
- [ ] OMP projection maps canonical `paths:` into CP0-proven native scope fields and uses a CP0-proven pre-operation event to supply matched bodies when static scope is advisory; missing capability rows produce `unsupported`.
- [ ] Codex projection is an Atomic plugin using CP0-proven registration, session-baseline events, a synchronous pre-operation event exposing structured target paths, a context-return field, and any exact deny result; concrete API names remain capability evidence, not assumed contract.
- [ ] Rule enforcement is reported as `native-scope`, `hook-required`, `deny-enforced`, or `unsupported`; only exact machine-checkable predicates may deny an operation, and semantic prose compliance is never described as hard enforcement.
- [ ] `atomic harness rules status` and doctor report source/projection digests, tier, disabled or trust state, collisions, uncovered tools, and last runtime proof independently per target. An operation without a mediated exact path is uncovered, receives no matched rule body, and remains `unsupported`.
- [ ] OMP discovers every supported Atomic command, all eight Atomic agents, required skills, and actual path-scoped rules from its generated package and project projection.
- [ ] Command invocation, arguments, agent dispatch, required dependencies, and explicit-only behavior match each milestone's capability matrix.
- [ ] Agent projection preserves behavioral intent, required capabilities, write scope, execution preference, and whether restrictions are native or instruction-only.
- [ ] A Codex TOML agent decodes to the canonical rendered agent body before Milestone A; Codex runtime support remains gated by Milestone B.
- [ ] `~/.atomic/config.toml`, profile/wiki data, `install/ledger.json`, `install/operation.lock`, journals, transaction stage/backups, recovery backups, legacy pre-install snapshot, and project state-location records have one documented writer and retention contract.
- [ ] Read-only discovery classifies `absent`, structurally `legacy-complete`, `legacy-partial`, `v2-clean`, `v2-in-flight`, `v2-orphaned`, or `mixed` from native bytes, legacy manifest/snapshots/proposals/settings, v2 ledger/journals/transactions/packages/registrations, and project selection; discovery never enrolls.
- [ ] Install, update, repair, adoption, uninstall, and any ledger-managed `atomic doctor --fix` acquire one global advisory lifecycle lock, reject unreadable newer schemas, recover unresolved journals oldest-first, and re-observe before planning any mutation.
- [ ] Dry-run leaves the complete filesystem byte-identical, reports the same pre-recovery classification and conflicts, and simulates journal-described writes in memory. Current journal/ledger/digest evidence that determines one safe result yields an advisory post-recovery plan; unavailable observations, decision-relevant external/native effects, later edits/conflicts, or user choice yield `blocked_on_recovery`.
- [ ] Only bytes matching the selected binary's embedded generation and uniquely parseable Atomic blocks are automatically verifiable ownership evidence; this design has no historical digest catalog. Older-version non-block artifacts require batched per-resource replace-or-leave-unowned decisions. Names and legacy config paths never prove ownership; missing listed files remain drift until convergence is accepted, and unjournaled v2 remnants are conflicts.
- [ ] Pending proposed/merged Claude files, missing or corrupt legacy snapshots, and overlapping legacy/v2 evidence block automatic adoption until explicitly resolved or acknowledged.
- [ ] The write-once legacy pre-install snapshot is preserved as recovery history. Before first v2 mutation, current observations, backups, and parent directories are durable; an empty or already-converged plan creates no journal or backup.
- [ ] Partial v2 recovery discards staging only with journal proof, ledger-commits applied bytes only after digest verification, restores backup bytes only when no later edit exists, and otherwise reports conflict.
- [ ] Repository-state migration adopts one existing root with actual Atomic state signatures in place, ignores empty candidates, persists and fsyncs selection before resolution switches, treats `ATOMIC_STATE_DIR` as process-only, and rolls selection back only when no later state write occurred.
- [ ] Ledger and journal schemas record schema version, writer version, and minimum reader version. New binaries refuse newer incompatible schemas; old Claude-only binaries are unsupported writers after migration and their later changes surface as drift or conflict.
- [ ] Install, update, repair, doctor, and uninstall operate only on explicitly enrolled target instances; discovery never enrolls.
- [ ] One physical resource has one ownership record even when several targets or unenrolled instances can discover it; OMP shared-package visibility and consumer enrollment remain separate and visible.
- [ ] If two enrolled OMP profiles require different generations of one shared package, convergence refuses before mutation and reports both targets without changing the package.
- [ ] Every multi-resource operation observes the complete plan, persists a durable journal and backup before mutation, stages and validates projections, records each native commit unit, verifies effective content, and only then commits ledger state.
- [ ] Initial generated-directory publication uses one same-filesystem rename; replacement uses a journaled current-to-backup and staging-to-current sequence.
- [ ] Repository-state resolution follows `ATOMIC_STATE_DIR`, then `~/.atomic/<project-key>/state-location.json`, then the user `state.dir` default, then `.claude`, without consulting harness fingerprints or introducing a second project-key hierarchy.
- [ ] A repository with one unambiguous populated state root is adopted without moving files; ambiguous roots or conflicting evidence require explicit `atomic state adopt` selection.
- [ ] `state.dir` accepts one safe path segment; an obsolete `ATOMIC_HARNESS` value blocks with instructions to use `ATOMIC_STATE_DIR` and is never treated as an alias.
- [ ] An empty repository creates no state candidate or adoption record until a stateful operation needs one; adoption persists before scope resolution switches; Pi agent configuration remains independent.
- [ ] Repo wiki initialization requires Git, creates or amends shared root and `docs/wiki/` `AGENTS.md` files plus adjacent thin Claude loaders without wholesale overwrite, and rejects malformed or conflicting loaders.
- [ ] Realm initialization may start from a non-Git root, creates `<realm>/AGENTS.md` plus `<realm>/CLAUDE.md` and `<realm>/wiki/AGENTS.md` plus `<realm>/wiki/CLAUDE.md`, makes `<realm>/wiki/` its own Git repository, and writes registration authority only to `~/.atomic/wikis.md`.
- [ ] Repository wiki refresh writes canonical scan, index, domain, steering, and pointer-rule files first; realm refresh writes realm and nested-wiki steering plus scan, member, bucket, and domain files without scoped pointer rules. Both auto-converge only already-enrolled targets, and target failure never rolls back canonical output.
- [ ] Worktree refresh writes canonical repository output into the invoked worktree while project-keyed state selection and operation records remain shared.
- [ ] Unowned guidance remains byte-identical unless the user approves relocation and equivalent effective guidance is verified at global, project, and nested wiki scopes.
- [ ] `~/.atomic/profile.md` and `~/.atomic/wikis.md` are authoritative after one-time legacy adoption; later divergent native copies are conflicts and are never imported automatically.
- [ ] Existing user model settings, role overrides, native override files, disabled plugins, hook trust decisions, and profile guidance outside Atomic-owned blocks remain authoritative.
- [ ] A target uninstall removes only unchanged resources owned for that target and leaves shared resources while another consumer exists.
- [ ] Full uninstall after the final target preserves `config.toml`, `profile.md`, `wikis.md`, and backups; removes completed operational and adoption state; and retains unresolved journals plus referenced transaction backups, ledger rows, and state-location records until recovery completes.
- [ ] A downloaded update re-executes the replacement binary before target convergence, applies and records only that binary's embedded artifact generation, and never lets the stale process publish its corpus.
- [ ] An already-current normal update skips download but still converges enrolled targets; `atomic update --check` remains read-only.
- [ ] `atomic doctor` reports target instances, shared resources, unfinished journals, capability gaps, stale materializations, deliberate disablement or untrusted hooks, effective-content shadowing, and uncovered rule operations independently.
- [ ] Existing `atomic doctor` category names and numeric indices remain unchanged; multi-harness categories append with stable names and indices.
- [ ] Milestone A passes Claude and OMP runtime scenarios for global, root, nested wiki, resumed, and child sessions, including absence of duplicate Atomic or output-style application and exact rule matching behavior.
- [ ] Milestone B passes Codex runtime scenarios for default and custom homes, plugin trust and disablement, override precedence, shared skills, native agents, matching and nonmatching rules, supported tools, shell and hosted operations without safe paths, resumed and child sessions, and every capability claimed by that milestone. Uncovered operations prove no matched body was delivered.


## Approach

Canonical instruction bodies with narrow native harness adapters — see `docs/design/omp-plugin-compatibility.md`.


## Change tree


```
planning and capability evidence
├── docs/design/omp-plugin-compatibility.md ... A  (architecture and approach)
├── docs/spec/omp-plugin-compatibility.md ..... M  (implementation contract and amendments)
├── docs/research/harness-capability-matrix.md  A  (versioned native behavior evidence)
└── .claude/.scratchpad/omp-plugin-compatibility/
    ├── capabilities/ ........................ A  (raw CP0 observations)
    └── runtime/ ............................. A  (retained CP8 evidence)

context/
├── AGENTS.md ................................ A  (global Atomic steering source)
├── CLAUDE.md ................................ D  (global source replaced by adapter projection)
├── agents/atomic-*.md ....................... M  (portable semantic metadata)
├── commands/*.md ............................ M  (portable invocation and dispatch outcomes)
├── _partials/*.md ........................... M  (shared portable orchestration blocks)
├── skills/atomic-*/ ......................... M  (portable paths and dependency declarations)
├── rules/**/*.md ............................ M  (actual path-scoped rule metadata)
└── commands/atomic-help.md .................. M  (new lifecycle discovery)

repository steering
├── AGENTS.md ................................ A  (project steering source)
├── CLAUDE.md ................................ M  (project @AGENTS.md loader)
├── docs/wiki/AGENTS.md ...................... A  (nested wiki steering source)
└── docs/wiki/CLAUDE.md ...................... M  (nested @AGENTS.md loader)

atomic/internal/
├── artifacts/ ............................... A  (canonical rendering, identity, projection digests)
│   └── tests and testdata ................... A  (deterministic render and golden projections)
├── harness/
│   ├── harness.go ........................... A  (adapter contract and registry)
│   ├── claude/ .............................. A  (Claude projection and lifecycle adapter)
│   ├── omp/ ................................. A  (OMP package, steering, profiles, rules, session adapter)
│   ├── codex/ ............................... A  (Codex projection and lifecycle adapter)
│   └── testdata/ ............................ A  (native, migration, worktree, and non-git fixtures)
├── installstate/ ............................ A  (enrollment, ledger v2, journal, recovery)
│   └── tests and testdata ................... A  (commit-unit interruption and adoption fixtures)
├── managedfile/ ............................. A  (managed blocks, backups, safe publication)
│   └── tests and testdata ................... A  (file, block, and generated-tree fixtures)
├── rules/ ................................... A  (RuleRecord parser, matcher, validation, enforcement tiers)
│   └── tests and testdata ................... A  (overlap, containment, collision, rename, and deletion fixtures)
├── runtimeproof/ ............................ A  (isolated harness scenario runner)
├── bundlespec/bundlespec.go ................. M  (AGENTS.md and canonical steering inclusion)
├── bundlemirror/mirror.go ................... M  (canonical enumeration and projections)
├── claudeinstall/ ........................... M  (legacy adoption and Claude adapter internals)
├── config/ .................................. M  (state.dir, adoption records, mutable-state paths)
├── hooks/ ................................... M  (maintenance, collection, formatting, Claude delivery)
├── profile/ ................................. M  (authoritative profile delivery)
├── wiki/ .................................... M  (authoritative wikis.md registry)
├── where/ ................................... M  (harness-neutral registry and state resolution)
├── doctor/ .................................. M  (target, resource, journal, capability checks)
├── selfupdate/ .............................. M  (enrollment-aware reconvergence)
├── validate/ ................................ M  (projection and portability validation)
└── cliusage/cliusage.go ..................... M  (new command inventory)

atomic/cmd/atomic/
├── main.go .................................. M  (adapter registry composition)
├── cmd_install.go ........................... A  (top-level multi-harness install)
├── cmd_harness.go ........................... A  (enroll, adopt, status, repair, diff, uninstall, rules)
├── cmd_state.go ............................. A  (state adoption)
├── cmd_claude.go ............................ M  (Claude lifecycle cutover)
├── cmd_update.go ............................ M  (enrolled-target convergence)
├── cmd_doctor.go ............................ M  (target-aware reporting)
├── cmd_hooks.go ............................. M  (adapter-local session delivery)
├── cmd_harness_test.go ...................... A  (lifecycle and CLI contract)
└── cmd_state_test.go ........................ A  (state adoption contract)

user-facing and maintained documentation
├── install.sh ............................... M  (multi-harness install handoff)
├── README.md ................................ M  (supported harnesses and lifecycle)
├── docs/guides/install.md ................... M  (enrollment, update, uninstall)
├── docs/reference/commands.md ............... M  (new CLI surface)
├── docs/reference/concepts.md ............... M  (steering and adapter model)
├── docs/reference/conventions.md ............ M  (AGENTS.md ownership)
├── docs/spec/install-workflow.md ............ M  (direct global projection and loader migration)
├── docs/spec/uninstall.md ................... M  (target and full uninstall)
├── docs/spec/atomic-doctor.md ............... M  (multi-target checks)
├── docs/spec/atomic-state-and-config.md ..... M  (state.dir and wikis.md)
├── docs/spec/atomic-validate.md ............. M  (projection gates)
├── docs/spec/output-style-seed.md ........... M  (Claude-native and OMP composition)
├── docs/spec/user-profile.md ................ M  (authoritative profile delivery)
├── docs/spec/atomic-where.md ................ M  (harness-neutral resolution)
├── docs/spec/atomic-binary.md ............... M  (lifecycle verbs)
├── docs/wiki/bundle.md ...................... M  (canonical corpus and native projections)
├── docs/wiki/config.md ...................... M  (ledger and state selection)
├── docs/wiki/doctor.md ...................... M  (target-aware checks)
└── atomic/CHANGELOG.md ...................... M  (release-visible contract changes)
```


## Outline


```
context/AGENTS.md
  Atomic contract — harness-neutral global steering and authoritative references

context/CLAUDE.md
  Removal — obsolete global-source loader; Claude global projection is generated directly from context/AGENTS.md

repository AGENTS.md and CLAUDE.md
  Project steering — shared guidance plus Claude loader relation

docs/wiki/AGENTS.md and docs/wiki/CLAUDE.md
  Wiki steering — nested shared guidance plus Claude loader relation

atomic/internal/artifacts/
  Artifact — canonical identity, kind, rendered body, semantics, and source digest
  Projection — target-native bytes, delivery classification, and projection digest
  Catalog — deterministic corpus enumeration and dependency validation
  Renderer — partial expansion and native body production
  Tests and golden testdata — canonical render, identity, dependency, and projection fixtures

atomic/internal/harness/
  Adapter — instance discovery, capabilities, projection, convergence, verification, removal
  Registry — concrete adapter lookup and target selection
  Target — harness, instance, native root, enrollment, and convergence state
  CapabilityMatrix — versioned native behavior evidence
  Testdata — native capability, migration, worktree, non-git, and golden fixtures

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
  Settings — output-style and SessionStart narrow mutation
  Migration — global, project, and nested guidance adoption

atomic/internal/claudeinstall/
  Legacy lifecycle — verified Claude adoption and adapter-compatible install semantics
  Tests and testdata — existing-install, migration, conflict, and uninstall fixtures

atomic/internal/harness/omp/
  Adapter — OMP profile discovery, enrollment, convergence, and verification
  Package — generated commands, agents, skills, actual rules, and extension
  Steering — import-free AGENTS.md plus frontmatter-free output-style composition
  Profiles — profile-specific native roots, plugin state, and shared-package consumers
  Models — semantic execution preferences to OMP role defaults
  Rules — canonical scope to CP0-selected native fields, exact-path matching, runtime delivery, freshness, and cleanup
  Session — CP0-selected event collection, matched-rule injection, formatting, and primary/resumed/child deduplication

atomic/internal/harness/codex/
  Adapter — CODEX_HOME discovery, enrollment, convergence, registration, trust observation, and verification
  Steering — native AGENTS.md materialization and override diagnostics
  Plugin — generated Atomic plugin package, manifest, rules index, and CP0-selected hook registration
  Hooks — proven session-baseline and pre-operation roles, bounded context return, exact-predicate deny, and uncovered-operation reporting
  Agents — canonical agent to TOML projection
  Skills — shared-skill installation and visibility reporting
  Commands — explicit invocation projection when supported

atomic/internal/runtimeproof/
  Scenario runner — isolated homes, repositories, processes, events, and retained observations

atomic/internal/config/
  StateLocation — state.dir default, process override, and ~/.atomic/<project-key>/state-location.json
  MutablePaths — ~/.atomic/config.toml, profile.md, wikis.md, install state, backups, and package roots
  ResolvePiAgents — independent Pi configuration preserved

atomic/internal/wiki/
  WikiRegistry — authoritative byte-preserving ~/.atomic/wikis.md operations
  Initialization — repository root/wiki and realm root/wiki steering, Git preconditions, state root, and capture surface
  Refresh — repository pages and wiki-pointer RuleRecord production; realm pages without scoped rule production
  NativeProjection — target convergence, per-target stale/conflict state, and no reverse import after adoption

atomic/internal/hooks/
  Maintenance — stateful profile refresh operations
  Collection — read-only profile, wiki, reminder, and orientation inputs
  Formatter — pure session-context rendering
  ClaudeDelivery — Claude settings registration and output-style selection

atomic/internal/doctor/
  Target checks — native registration, artifacts, guidance, roles, and disablement
  Shared checks — ledger, journals, ownership, mutable authority, and visibility
  Capability checks — promised behavior, gaps, shadowing, and stale evidence
  Repairs — reuse adapter convergence instead of a separate repair path

atomic/internal/bundlespec/bundlespec.go
  SteeringSource — canonical global AGENTS.md source, direct Claude global projection, and repository/realm loader inclusion

atomic/internal/bundlemirror/mirror.go
  Enumerate — one rendered corpus for adapter projections

atomic/internal/profile/
  Profile delivery — authoritative profile state and native generation tracking

atomic/internal/where/
  Resolve — harness-neutral wiki registry and repository-state orientation

atomic/internal/selfupdate/
  Artifact convergence — enrolled-target refresh after binary selection

atomic/internal/validate/
  Projection rules — corpus portability and native projection diagnostics

atomic/internal/cliusage/cliusage.go
  Command inventory — stable rows for install, harness, and state verbs

atomic/cmd/atomic/main.go
  command registry — top-level install, harness, and state routing

atomic/cmd/atomic/cmd_install.go
  install — explicit multi-harness target selection and convergence

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
  state adopt — explicit repository-state root selection

atomic/cmd/atomic/cmd_claude.go
  Claude cutover — route the existing lifecycle through the Claude adapter

atomic/cmd/atomic/cmd_update.go
  update convergence — re-exec and reconverge enrolled targets

atomic/cmd/atomic/cmd_doctor.go
  target reporting — stable existing categories plus appended harness checks

atomic/cmd/atomic/cmd_hooks.go
  session delivery — adapter-local maintenance and injection routing

context command, agent, skill, partial, and rule families
  Portable outcomes — harness-neutral workflow intent and dependencies
  Native metadata — adapter-consumable semantics without runtime wire syntax

docs/design/omp-plugin-compatibility.md
  Architecture — adapter, steering, state, lifecycle, and milestone decisions

docs/spec/omp-plugin-compatibility.md
  Contract — success criteria, ordered checkpoints, and amendment log

docs/research/harness-capability-matrix.md
  Runtime evidence — tested versions, native surfaces, observed precedence, and unsupported capabilities

.claude/.scratchpad/omp-plugin-compatibility/capabilities/
  Raw observations — probe inputs, outputs, tested versions, and links to versioned conclusions

.claude/.scratchpad/omp-plugin-compatibility/runtime/
  Retained evidence — CP8 scenario commands, isolated paths, outputs, and result digests

install.sh
  Installer handoff — explicit harness selection and generic lifecycle entry point

README.md
  Product surface — supported harnesses and lifecycle

context/commands/atomic-help.md
  Discovery — install, harness, state, update, doctor, and uninstall routing

docs/guides/install.md
  Operator flow — enrollment, update, repair, adoption, and uninstall

docs/reference/commands.md
  CLI inventory — user-runnable lifecycle and state surfaces

docs/reference/concepts.md
  Architecture — canonical artifacts, native adapters, and shared resources

docs/reference/conventions.md
  Steering and state — AGENTS.md ownership and harness-neutral repository state

docs/spec/install-workflow.md
  Install contract — target enrollment, adoption, and native convergence

docs/spec/uninstall.md
  Removal contract — target-scoped and full uninstall behavior

docs/spec/atomic-doctor.md
  Diagnostic contract — stable categories plus target and shared-resource checks

docs/spec/atomic-state-and-config.md
  State contract — state.dir, adoption records, authoritative mutable data, and overrides

docs/spec/atomic-validate.md
  Validation contract — canonical corpus and projection gates

docs/spec/output-style-seed.md
  Style contract — Claude-native selection and OMP steering composition

docs/spec/user-profile.md
  Profile contract — authoritative storage, native projection, and reconciliation

docs/spec/atomic-where.md
  Resolution contract — harness-neutral wiki and repository-state paths

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
  State verification — adoption, override, lazy creation, and persistence order

atomic/CHANGELOG.md
  Release entry — user-visible behavior and breaking lifecycle cutover
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
6. Claude writes its direct global `CLAUDE.md` block and native artifacts; OMP publishes its shared package and profile steering/registration; Codex publishes its plugin and requests native registration without fabricating hook trust.
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
4. Only exact selected-generation bytes and uniquely parseable Atomic blocks become verified ownership evidence inside the approved plan. Older-version non-block artifacts are batched for replace-or-leave-unowned decisions. Missing listed files, malformed snapshots, pending Claude merge files, relative-reference changes, and orphan v2 resources require explicit resolution and remain preserved.
5. Migration preserves the write-once legacy pre-install snapshot and current unowned prose. If historical restoration evidence is absent or corrupt, the user must acknowledge that limit before adoption.
6. After explicit consent, migration writes and fsyncs the journal, current-byte backups, and parent directories. It then persists any state-root selection before switching resolution.
7. Migration renders canonical steering directly into global Claude `CLAUDE.md`, writes repository and realm `AGENTS.md` steering with adjacent thin Claude loaders, imports mutable data once, and applies target-native resources through normal commit units.
8. Effective guidance, mutable authority, native bytes, and capability state are verified before ledger ownership commits. Failure rolls forward or restores only digest-unchanged applied bytes; later edits conflict.
9. After adoption, native profile/wiki copies are one-way projections, repeated same-generation convergence is a no-op, and old Claude-only binaries are unsupported writers whose later changes surface as drift.

**Flow: repository-state adoption**

1. Resolver checks process-only `ATOMIC_STATE_DIR`, `~/.atomic/<project-key>/state-location.json`, the user `state.dir` default, and legacy explicit `harness.dir` evidence.
2. Migration scans plausible roots for actual Atomic state signatures without creating candidates; empty `.claude` and `.pi` directories do not count.
3. One unambiguous populated root becomes the persisted project selection without moving files; the selection record is written and fsynced before resolution switches.
4. Multiple populated roots or conflicting evidence require `atomic state adopt`.
5. A failed migration restores the prior selection only when no later state write used the new root; otherwise it records conflict.
6. Claude, OMP, Codex, shell commands, hooks, and worktrees resolve the same selected root; Pi agent configuration remains independent.

**Flow: repository wiki initialization and refresh**

1. Initialization verifies a Git repository, resolves and persists the state root, and observes root plus `docs/wiki/` steering.
2. It creates absent shared `AGENTS.md` files or owned blocks and adjacent thin Claude loaders; it never overwrites either file wholesale and rejects malformed or conflicting loaders.
3. Refresh writes `docs/wiki/scan.md`, `index.md`, domain pages, steering changes, and canonical `<state-root>/rules/wiki/**` cards into the invoked worktree.
4. Canonical wiki output commits before projection; project-keyed state and operation records remain shared across worktrees.
5. Refresh auto-converges steering and rules for already-enrolled targets only.
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
5. Recovery records applied work, completes safe pending work, or reports conflict without overwriting an unrelated user change. Recovery then permits cleanup of the retained operational records it consumed.

**Flow: target and full uninstall**

1. User selects one enrolled target instance or requests full Atomic removal.
2. Adapter observes every owned target resource, shared visibility, and consumer relation.
3. Changed target resources require user resolution; unchanged owned resources and consumer links are removed.
4. Shared resources remain while another enrolled consumer exists, regardless of visibility to unenrolled instances.
5. Target steering is removed independently of plugin disablement or unlinking.
6. After the final target, full uninstall preserves `~/.atomic/config.toml`, `~/.atomic/profile.md`, `~/.atomic/wikis.md`, and backups.
7. Completed ledger, journal, transaction, and adoption state is removed. Unresolved journals, referenced transaction backups, referenced ledger rows, and referenced state-location records remain until recovery completes.


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| CP0 | Record versioned Claude, OMP, and Codex capability evidence | `docs/research/harness-capability-matrix.md`, scratchpad observations, spec amendments | atomic-implementer (mode: feature) | ~3 | Runtime probes map tested native names and payloads to static scope, session-baseline, pre-operation target, context-return, and deny capability rows; also record paths, registration, trust, ordering, size, lifetime, child, dedupe, reload, concurrency, failure, disablement, cleanup, visibility, and overrides. Missing rows mean `unsupported`; findings amend affected criteria/checkpoints and the Change log |
| CP1A | Add managed-resource ledger and recovery primitives | `installstate/`, `managedfile/`, tests and fixtures | atomic-implementer (mode: feature) | ~10 | Exact ledger/journal/transaction paths, file/block/generated-tree commits, interruption scenarios, full uninstall with unresolved work, referenced ledger/selection retention, post-recovery cleanup, and sidecar digest behavior pass without overwriting concurrent changes |
| CP1B | Add adapter registry, target enrollment, and ownership evidence | `harness/`, `harness/claude/`, `claudeinstall/`, `config/`, CLI tests | atomic-implementer (mode: feature) | ~12 | Discovery does not enroll; selected-generation bytes and parseable blocks can establish ownership only inside an approved plan; ambiguous targets require `harness adopt claude`; visibility and consumers differ; target removal cannot delete shared infrastructure |
| CP1C | Make repository state and mutable context harness-neutral | `config/`, `wiki/`, `where/`, `hooks/`, `profile/`, tests | atomic-implementer (mode: feature) | ~12 | Exact `~/.atomic` paths, safe-segment validation, obsolete-env refusal, empty-repository laziness, persist-before-switch ordering, Pi independence, one-root resolution, one-time mutable import, post-adoption conflict behavior, and separated session operations pass |
| CP1D | Classify and adopt legacy, partial, and mixed installations | `installstate/`, `claudeinstall/`, `config/`, `hooks/`, migration fixtures | atomic-implementer (mode: feature) | ~12 | All seven states, every legacy/v2 evidence input, selected-generation/block ownership, a prior-version complete install with both replace and leave-unowned outcomes, pending proposal and corrupt-snapshot conflicts, operation lock, oldest-first recovery, filesystem-identical dry-run where applied-but-unrecorded and discardable-staging cases produce advisory plans while later-edit recovery conflict produces `blocked_on_recovery`, snapshot preservation, idempotent no-op, state-root adoption/rollback, schema refusal, and old-binary drift pass on real legacy-shaped fixtures |
| CP2A | Cut canonical steering to AGENTS.md and add offline renderers | global/root/wiki steering, `artifacts/`, `bundlespec/`, `bundlemirror/`, tests | atomic-implementer (mode: feature) | ~12 | Claude's global `CLAUDE.md` contains the direct rendered contract and has no `AGENTS.md` reference; repository and realm loaders resolve adjacent steering; OMP golden is import-free Atomic steering plus frontmatter-free style; rendered bytes and digests are deterministic |
| CP2B | Normalize the eight canonical agent definitions | eight `context/agents/atomic-*.md` files | atomic-implementer (mode: feature) | 8 | Every canonical agent states behavioral intent, capabilities, write scope, execution preference, dependencies, and enforcement strength without native wire syntax |
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
| CP2L | Port state and capture workflows | `retrospective-learning`, `session-report`, `follow-up`, `remind-me` | atomic-implementer (mode: feature) | 4 | Durable state writes use the selected repository root or `~/.atomic`; session-local formatting remains adapter-owned |
| CP2M | Port existing help and issue-report workflows | `atomic-help`, `report-issue`, `report-issue-with-atomic`, `report-issue-privacy` | atomic-implementer (mode: feature) | 4 | Existing help routing and issue-report bodies are portable; issue reports preserve privacy without harness-specific paths; new lifecycle discovery remains owned by CP7E |
| CP2N | Add corpus and projection validation gates | `validate/`, projection fixtures, complete rendered corpus | atomic-implementer (mode: feature) | ~8 | One validator pass covers dependencies, portability, canonical identity, native metadata, projection digests, unsupported capabilities, and leaked Claude-only tokens |
| CP3 | Migrate verified global, project, and nested instructions | Claude adapter, ledger, managed files, config, wiki, migration fixtures | atomic-implementer (mode: feature) | ~12 | From CP1D-classified state, global Claude steering migrates directly into `~/.claude/CLAUDE.md` with no global `AGENTS.md`; repository and realm loaders preserve unowned prose and relative-reference meaning; ownership commits only after effective-content verification; later derivative changes conflict |
| CP4A | Generate and enroll the native OMP package and profile steering | OMP adapter, `cmd_harness.go`, config seams, golden fixtures | atomic-implementer (mode: feature) | ~12 | Default/named profile lifecycle preserves user settings; live `AGENTS.md` matches golden; visibility is reported separately; incompatible shared generations refuse before package mutation |
| CP4B1 | Add OMP static rule projection and project lifecycle | OMP package/rules, native project-rule surface, rules adapter, doctor seams | atomic-implementer (mode: feature) | ~8 | Depends on the CP0 static-scope row; canonical scope maps only to proven native fields, repository-generated cards publish recoverably, and freshness/collision/rename/delete/conflict scenarios pass. A missing row records `unsupported` without failing install |
| CP4B2 | Add and prove OMP runtime rule delivery | OMP session/rules, CP0-selected extension events, runtimeproof, doctor checks | atomic-implementer (mode: feature) | ~8 | Depends on CP0 session/pre-operation/context/deny rows; primary, resumed, and child events avoid duplicates; exact match/nonmatch, overlap, precedence, lifetime, machine deny, semantic guidance, uncovered tools, and deliberate disablement report correctly. Missing rows stay `unsupported` |
| CP6A | Verify Claude and OMP project and nested guidance | root/wiki steering, Claude/OMP adapters, worktree and non-git fixtures | atomic-implementer (mode: feature) | ~10 | Claude loads direct global steering plus repository and nested realm guidance once; OMP loads proven native equivalents and matched rules; invoked-worktree writes and both realm steering pairs pass; unsafe projections preserve prior valid output |
| CP7A | Cut generic Claude and OMP lifecycle CLI | `cmd_install.go`, `cmd_harness.go`, `cmd_claude.go`, `cliusage/`, install/uninstall packages, CLI tests | atomic-implementer (mode: feature) | ~12 | Global lifecycle serialization, exact global writes, explicit enrollment, read-only planning, adoption, staging, verification-before-ledger, repair, partial failure, shared visibility, target uninstall, preserve-data full uninstall, and referenced recovery-state retention pass without touching update or doctor behavior |
| CP7B | Make update convergence target-aware | `cmd_update.go`, `selfupdate/`, update tests | atomic-implementer (mode: feature) | ~8 | Downloaded update hands convergence to the replacement binary's embedded generation; already-current update converges separately; read-only check performs no target or ledger writes |
| CP7C | Append multi-harness doctor checks | `cmd_doctor.go`, `doctor/`, doctor fixtures and tests | atomic-implementer (mode: feature) | ~10 | Targets, shared visibility, consumers, journals, capability rows, rule tiers/digests/coverage, trust, disablement, staleness, conflict, and shadowing report independently; ledger-managed `doctor --fix` uses the global lock, oldest-first recovery, and common planner; existing category names and indices remain fixed and new categories append |
| CP7D | Amend related lifecycle and state specifications for Milestone A | nine named related `docs/spec/*.md` files | atomic-implementer (mode: feature) | 9 | Each amended spec body states current Claude/OMP global, repository, rule, wiki, state, and uninstall contracts; Change logs record superseded behavior without duplicating this spec |
| CP7E | Update Milestone A help, user docs, domain maps, and release entry | atomic-help, README, install script, guide, references, three wiki pages, changelog | atomic-implementer (mode: feature) | ~11 | Every Milestone A surface is discoverable; help-router reports zero missing commands; domain maps match implementation; release classification is correct |
| CP8A | Prove Milestone A in isolated Claude and OMP homes | `runtimeproof/`, scratchpad runtime evidence, generated fixtures | atomic-implementer (mode: feature) | ~6 | Claude-only, OMP-only, named-profile, incompatible-generation, allowed shared visibility, global install, real prior-version complete/partial and v2 in-flight/orphaned/mixed fixtures, replace and leave-unowned outcomes, pending proposal, corrupt snapshot, listed-missing drift, concurrent update/uninstall/`doctor --fix`, filesystem-identical dry-runs with advisory applied-unrecorded/staging-discard plans and blocked later-edit recovery, interrupted adoption, idempotent rerun, state-root rollback, newer-schema refusal, old-binary drift, repo/realm/worktree behavior, rule matching, primary/resumed/child, uninstall recovery, repair, and both update scenarios pass |
| CP5 | Add the Codex plugin, adapter, and hooks for Milestone B | Codex adapter/plugin/hooks, rules, shared skills, command layer, doctor checks, fixtures | atomic-implementer (mode: feature) | ~16 | Depends on CP0 registration/session/pre-operation/context/deny rows; default/custom homes, trust-pending and disabled state, AGENTS overrides, skills, TOML agents, bounded payloads, exact path extraction, and approved commands pass. Missing rows remain `unsupported` without blocking unrelated surfaces |
| CP6B | Verify Codex guidance and rule runtime semantics | root/wiki steering, Codex adapter/hooks, runtimeproof, worktree and non-git fixtures | atomic-implementer (mode: feature) | ~10 | Guidance discovery, override precedence, match/nonmatch/overlap, mediated and bypassed tools, exact deny, payload limits/spill, reload, resume, child, dedupe, concurrency, failure, cleanup, worktree, and non-Git behavior match proven rows; shell/hosted calls without safe paths are uncovered and receive no matched body |
| CP7F | Complete Codex lifecycle discovery and documentation | command layer, doctor, selfupdate, atomic-help, guides, references, specs, changelog | atomic-implementer (mode: feature) | ~12 | Codex enrollment, trust status, update, repair, target uninstall, rule tiers, unsupported capability roles, and promised artifact surfaces are discoverable; amended specs remain current; stable CLI and doctor contracts hold |
| CP8B | Prove Milestone B in isolated Codex and mixed homes | `runtimeproof/`, scratchpad runtime evidence, generated fixtures | atomic-implementer (mode: feature) | ~6 | Codex-only and three-target scenarios extend CP8A migration fixtures with plugin trust/disablement, global install, wiki auto-convergence, two repos/worktrees, match/nonmatch/overlap, rename/delete/malformed/collision, changed derivative, mediated and uncovered operations with no false delivery claim, primary/resumed/child sessions, shared ownership, concurrent lifecycle refusal, interruption recovery, repair, and every promised capability |


## Risks


| Harness behavior differs from accepted file syntax | high | CP0 and CP8 use observable runtime scenarios for discovery, path extraction, precedence, child delivery, hooks, trust, and session behavior |
| Instruction migration changes or deletes user guidance | high | Move only known Atomic-owned content; preserve unowned prose; block ambiguous destination, duplicate applicability, malformed markers, and changed relative references |
| Existing legacy or partial state is misclassified | high | CP1D uses all legacy/v2 evidence inputs, real prior-version fixtures, seven explicit states, selected-generation/block verification only, read-only planning, and per-resource consent for older or ambiguous artifacts |
| Concurrent lifecycle operations or stale journals race | high | Install, update, repair, adoption, uninstall, and ledger-managed `doctor --fix` acquire one advisory lifecycle lock, recover oldest-first, and re-observe after locking before any plan can mutate |
| A corrupt legacy snapshot creates false rollback confidence | high | Preserve it, block automatic adoption, require explicit acknowledgment, and never claim restoration evidence that cannot be verified |
| An older binary writes after v2 migration | med | Version schemas, refuse incompatible readers, mark old binaries unsupported, and surface later writes as drift or conflict |
| A partial install leaves native state and the ledger inconsistent | med | Write durable observations and backups before mutation; validate staging; journal each commit unit; verify native effect before ledger commit |
| A shared package or skill changes an unenrolled instance's visible surface | med | Record native visibility separately from consumers; report allowed visibility before mutation; refuse incompatible shared generations |
| Native edits to profile or wiki sections are overwritten | med | Import only during explicit legacy adoption; afterward keep `profile.md` and `wikis.md` authoritative and report derivative divergence as conflict |
| Corpus portability edits change Claude behavior | med | Preserve unchanged bytes and use contract scenarios for approved prose changes; keep one canonical body rather than parallel variants |
| Model or reasoning defaults override user policy | med | Store semantic preferences without concrete provider IDs; mutate only missing adapter defaults; retain existing native overrides |
| Rule projection claims parity from metadata alone | high | Report explicit enforcement tiers and require matching, nonmatching, overlap, lifetime, precedence, resumed-session, child-delivery, and bypass evidence per tested version |
| A semantic rule is mistaken for a security boundary | high | Hooks deterministically activate prose but deny only exact machine predicates; documentation and doctor label guidance and unsupported operations |
| Codex or OMP hooks are disabled, untrusted, bypassed, or payload-limited | high | Observe native state; never fabricate trust; bound payloads; report uncovered tools and stale runtime proof; preserve the last valid projection |
| Canonical wiki output succeeds while one target projection fails | med | Commit authority first, mark only the failed enrolled target stale or conflicted, and let repair reuse the same convergence planner |
| Update or doctor silently converges disabled targets | low | Distinguish deliberate disablement from repairable drift; normal update reports but does not re-enable without explicit repair policy |
| Scope expands into unsupported Codex behavior before OMP release | med | Milestone A requires only offline Codex agent proof; Milestone B gates every Codex runtime promise independently |


## Change log


### 2026-09-15 — Complete rule and lifecycle architecture


**What changed:** Added checkout-independent `RuleRecord` sources plus per-project `RuleInstance` binding, strict parsing and matching, per-harness enforcement tiers, CP0-selected OMP and Codex plugin delivery roles, exact install-state paths, global install commit order, repository and realm wiki initialization/refresh paths, one-way mutable authority, shared OMP visibility reporting, and preserve-data full uninstall with unresolved-recovery retention. Split static projection from runtime proof and expanded scenario coverage.

**Why:** The approved draft described projections but did not bind rule activation, runtime scope, global installation writes, wiki mutation ownership, or recovery behavior precisely enough to implement without inventing policy or native API semantics.

**Superseded:** Replaced implicit post-install native re-import with one-time legacy adoption followed by authoritative-to-derived convergence. Replaced checkout-bound rule bases with runtime instances, generic OMP/Codex API assumptions with CP0 capability roles, realm pointer-rule ambiguity with repository-only production, and unconditional operational cleanup with recovery-aware retention.

### 2026-09-15 — Bind legacy migration and resolve strategy questions


**What changed:** Added a seven-state migration classifier, explicit legacy and v2 evidence inputs, selected-generation/block ownership rules, batched prior-version decisions, global lifecycle locking including ledger-managed doctor repair, oldest-first recovery, byte-identical dry-run with in-memory recovery simulation and explicit blockers, preserved legacy snapshots, state-root adoption and rollback conditions, schema compatibility, mixed-version handling, CP1D, and expanded CP8 migration scenarios. Replaced open design questions with committed Claude, OMP, and Codex decisions plus CP0 evidence boundaries.

**Why:** The prior lifecycle architecture covered verified adoption but not incomplete legacy installs, interrupted v2 operations, orphan resources, concurrent lifecycle commands, damaged recovery evidence, or old binaries writing after migration.

**Superseded:** Replaced generic legacy adoption with explicit classification and per-resource ownership decisions. Replaced unspecified migration serialization and compatibility with one global lock, versioned state schemas, and conservative conflict handling.


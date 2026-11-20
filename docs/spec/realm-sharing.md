# Realm sharing

## Goal

Let developers share selected team instructions, wiki content, capture folders, portable definitions, skills, commands, and agents through a separate Git clone while working in their own local realms. Personal realm instructions and native loaders remain local. A developer publishes with `atomic realm share`; a colleague uses `atomic realm join <git-url>`, reviews `atomic realm link`, and exchanges changes through explicit `atomic realm sync`.

This document proposes public surfaces for user approval. Implementation starts only after approval.

## Non-goals

| Boundary | Excluded work |
|---|---|
| Synchronization | Background syncing, automatic sync hooks, hosted collaboration, and real-time editing. |
| Member repositories | Cloning, pulling, committing, changing branches, or consolidating their histories. |
| Private state | Sharing personal realm instructions, generated native loaders, credentials, permission settings, hooks, MCP connections, sessions, caches, or machine-local paths. |
| Topology | Multiple shared remotes per local realm, implicit nested-repository imports, and security sandboxing. |
| Filesystem fallback | Copies in place of symlinks or claims that arbitrary editors and recursive search tools preserve symlink behavior. |

## Success criteria

| ID | Binding acceptance criterion |
|---|---|
| S1 | `atomic realm` exposes six distinct verbs, `share`, `sync`, `join`, `link`, `status`, and `unlink`, with parent and per-verb help. Early implementation checkpoints expose their completed behavior through the registered CLI. |
| S2 | Shared `realm.toml` contains a stable non-time-based identity, format version, portable member definitions, relative bucket locations, artifact kinds, and explicit source-to-destination mappings. It rejects absolute paths, escapes, overlapping mappings, unsafe links, unsupported versions, private instruction files, generated native loaders, and forbidden harness configuration. |
| S3 | One local realm binds to one managed clone under the user state root's `realms/<name>/`; that clone binds to one local realm. `<state-root>/realm-share.toml` retains trust, private bucket registrations, pending sharing operations, loader/native-artifact ownership, restoration receipts, backups, and synchronization state locally. Existing state-directory resolution applies; clone name collisions never adopt an unrelated repository. |
| S4 | On an unbound realm, `share [<path>...]` requests an existing empty remote or accepts `--remote <git-url>`, offers branch `main` or accepts `--branch <name>`, and checks reachability and emptiness without inferring a member repository's remote. On a bound realm, it adds selections using the recorded remote and branch and rejects conflicting overrides. Both cases preview selected paths, registrations, counts, bytes, exclusions, destination, and local symlink effects and require publication approval. Unsupported selected files and non-wiki nested repositories are rejected. Wiki import snapshots current content without `.git` and retains its original repository and history locally. |
| S5 | `share` publishes before replacing originals, checks space and copy errors, revalidates originals before cutover, and retains recoverable backups. Failed publication leaves original paths unchanged. Changed originals stop cutover. Failed or interrupted cutover permits safe restoration or completion from receipts; diagnostics distinguish a published snapshot from local symlink creation failure. |
| S6 | `join <git-url>` downloads and validates the shared realm and previews its mappings without symlinking, executing downloaded configuration, overwriting local content, or touching member repositories. Instruction symlinking requires review and explicit trust acceptance. A changed definition requires renewed review before local links change. |
| S7 | `link` validates targets and destinations before installing declared links and local orientation. Correct owned symlinks remain untouched on repeat, including when a trusted instruction link installs a newly eligible Claude hook. An occupied destination is eligible only when receipts identify it as an original restored by `unlink` and its complete restored file/tree identity, including retained wiki Git state, still matches the restoration receipt rather than the current shared clone or timestamps. Relink revalidates eligible originals and retains or re-establishes recoverable backups before replacement. Changed originals, other occupied destinations, unowned links, missing targets, escapes, and ambiguous ownership stop without replacing local paths. `unlink` removes only unchanged owned links, restores retained originals without overwriting replacements, and preserves the clone, binding, backups, and unpublished edits. |
| S8 | Every mutation checks native symlink capability before changing realm content and fails loudly with an actionable diagnostic when unavailable, including on Windows. No copy fallback occurs. Concurrent Atomic mutations of one clone serialize; recovery never guesses ownership or silently deletes user work. |
| S9 | `sync` works whether the local symlinks exist or not, commits only declared content and the shared definition, creates no empty commits, and rejects unexpected staged paths, unsafe links, nested repositories, and unfinished merges or rebases. It preserves edits made after staging and operates without changing the caller's workspace. |
| S10 | `sync` attempts publication, integrates remote work only on non-fast-forward rejection, and attempts one further push after clean integration. Authentication and network failures retain commits and report their actual cause. A further remote advance returns a retryable failure. A no-change clone still receives remote work. |
| S11 | Conflicts preserve normal Git merge state and report clone-relative and realm-local paths plus explicit `git -C <clone>` recovery instructions. After manual resolution, staging, and merge completion, another `sync` publishes the result. No command force-pushes, resets edits, or reports success while unpublished work remains. |
| S12 | Read-only `status` and `status --json` expose the same binding, identity, symlink ownership/drift, local changes, cached ahead/behind knowledge, conflicts, and last successful synchronization. Shared paths are distinguished as linked, available but unlinked, conflicting, or pending completion; behavior artifacts additionally report active or unsupported native delivery and their supported launch scope. Neither form fetches nor modifies files. Remote knowledge is labeled cached. Mutation failures, trust stops, and conflicts return nonzero with preserved-work diagnostics and a next safe action. |
| S13 | Scope detection, `where`, wiki registrations, links, and agent workspace orientation retain the local realm identity, including commands started in symlinked descendants. Those commands reuse the local realm's binding, state root, and project-state identity and never create state inside symlinked shared content. Local link orientation names explicit discovery roots; backing paths appear only in storage diagnostics and Git recovery guidance. Explicit clone-scoped recovery does not change the active workspace. |
| S14 | Wiki and bucket operations resolve shared relative locations from each consuming local realm, preserve existing local absolute registrations as local data, index and fingerprint declared symlinked roots, and do not traverse arbitrary nested links. Private bucket registrations and hints stay local even when the wiki and team instruction source are shared symlinks; shared generated registries contain selected entries only with realm-relative paths. Wiki initialization never creates a nested Git repository in symlinked content. Atomic-owned writers preserve validated shared instruction-source symlinks and distinguish them from local loaders and private instructions; status detects external replacement. |
| S15 | Serve navigation, markdown search, and content access include declared symlinks under realm-local URLs with existing authorization checks. Traversal cannot reveal undeclared clone content, private state, or unrelated home-directory files. |
| S16 | An end-to-end test uses a real temporary Git remote and two independent developer realms to publish, join, trust, link, edit, create, exchange clean changes, recover conflicts, detect drift, unlink, and relink. It exercises the publisher's `share -> unlink -> link` sequence with restored originals, verifies relink against receipt identity after shared content advances, and confirms that editing a restored original prevents relink without losing either local or shared content. It also exercises the joiner's initially empty realm and verifies local-only files and member repositories remain untouched and both developers retain distinct local identities. |
| S17 | Documentation, CLI help, and the bucket skill teach the end-result realm layout, shared versus personal instructions, artifact opt-in and native activation, initial remote selection, repeatable sharing, terminal and noninteractive approval, bucket opt-in, received-path review, retained wiki history, trust, optional hooks, cached status, recovery, and discovery limits. Recorded OMP/macOS evidence establishes only the original fixture's read/edit/create and local follow-on behavior; composed loaders and realm-artifact activation require separate proof. Claude and Windows support claims remain evidence-bound. |
| S18 | New files and subfolders within a selected ordinary directory or skill unit are covered by that selection, subject to exclusions and Git ignore rules, and published by `sync` without new symlinks. New paths outside the selection remain local until explicitly shared. Selecting a file does not select adjacent files. Artifact collections require individual units rather than whole-parent mappings, so new sibling skills, commands, or agents remain local. Unchanged existing selections are idempotent and selections covered by a shared parent do not create overlaps. |
| S19 | `share` and `link` offer terminal checklists and review. Path arguments preselect paths without skipping confirmation. `--dry-run` prints effects and exactly one `Review digest: sha256:<hex>` line without mutation or publication. Skipping the confirmation requires explicit paths, `--yes`, and `--review` with that exact token, with or without a terminal. Initial `share` also requires an explicit remote. `--review` is valid only with `--yes`; neither is valid with `--dry-run`. The digest binds the operation, realm, destination, selection, and reviewed effects. For `share` it covers snapshot bytes, metadata, Git base, and publication versus local cutover. For `link` it covers the definition, selected behavior content, and collision and restoration evidence. Missing inputs and digest mismatch stop before mutation. Both modes revalidate under lock. Authentication failures do not wait for terminal input. |
| S20 | In a two-developer lifecycle, B runs `atomic wiki bucket add sops` without publishing metadata or changing shared instructions, then `atomic realm share sops/` publishes the directory and its portable bucket registration together using the existing remote and creates B's symlink only after success. A's `sync` receives both as available but unlinked; `link` reviews selected additions and creates their symlinks without changing existing owned links. A private `sops/` collision preserves A's content and private registration. |
| S21 | Adding selections requires a clean clone with existing work published; unrelated edits, staged changes, or unpublished commits stop the addition rather than entering its approved snapshot. Failed publication preserves the new path and its local registration. Repeating the same `share` can resume only its recorded incomplete operation after revalidation. A later `sync` may publish retained commits but never silently replaces original local paths; pending symlink creation remains visible until `share` safely completes it. |
| S22 | Team guidance lives in shared `.agents/realm.md`. Claude imports private guidance through its local loader and receives team guidance through S27. Other harnesses use verified imports or an adapter-composed local view. Atomic creates `AGENTS.local.md` locally if absent and preserves authored instructions. Private paths and local loaders are excluded from publication and sync even when explicitly selected or referenced; Atomic does not classify or publish mixed team and personal prose. |
| S23 | `.agents/skills/<name>/`, `.agents/commands/<name>.md`, and `.agents/agents/<name>.md` are individually selectable authored sources; `commands` is the canonical slash-workflow name. A skill's reviewed supporting files stay with its unit; outside dependencies require explicit selection or an actionable error. Selecting collection parents requires explicit expansion to current units in the wizard or fails in noninteractive mode. Native artifact parent directories stay local and unselected personal artifacts remain untouched. Ordinary prompt documents can be shared as files without claiming command registration. |
| S24 | Linking behavior artifacts previews source content, native destinations, support, collisions, executable behavior, trust, and restart or import approval requirements. Enrolled adapters use versioned capability evidence, avoid duplicate registration, and report unsupported activation separately from available content. Preserve user-owned collisions. Do not grant permissions, activate shared hooks or MCP settings, or write global profiles or member repositories to extend scope. The Atomic-owned startup hook in S27 applies only to instruction activation. Trust covers updates to selected artifacts from the same remote; new units still require selection. |
| S25 | Runtime acceptance proves that A and B load the same shared guidance with distinct private realm markers and that remote Git content contains neither private files nor local startup files. Shared edits propagate while private edits stay local. Unsupported instruction delivery or launch scope is reported. Opted-in skill, command, and agent units require per-kind native discovery and invocation proof. Unlink and relink preserve personal instructions, authored loader text, user artifacts, and recoverable originals without dangling imports or unauthorized activation. |
| S27 | Atomic previews and installs an owned local Claude `SessionStart` hook only when Claude is enrolled, `claude` is on `PATH`, and `.agents/realm.md` is selected and trusted. Wiki-only links preserve Claude settings, loaders, and hooks without preventing Atomic from recording link ownership. Rerunning the instruction link after installing Claude adds one hook without replacing existing symlinks; another repeat adds nothing. With tools disabled and no `--add-dir`, a fresh session loads validated current team text through the hook and private text through the local loader. Missing prerequisites, unsafe sources, and oversized context are not reported active. Unlink removes only the owned hook and registration, preserving unrelated settings, hooks, and private instructions. No clone-supplied hook runs. |
| S26 | Before checkpoint 1, the selected implementation base contains the existing 7.0 multi-harness adapter, local steering, ownership, and versioned capability layer, including `atomic/internal/harness/` and its capability evidence. Its existing package tests pass from that checkout. An installed binary or code in a separate worktree does not satisfy this prerequisite. This feature reuses that layer and does not introduce parallel adapters; if it has not landed, implementation does not start. |
| S28 | Whenever a selection needs external write access, `link` previews the resolved clone path and explains how to configure access without changing permissions. The wiki-only acceptance test verifies two separate writes through the wiki symlink: one in a fresh Claude session using saved local `permissions.additionalDirectories`, and one in a fresh Codex session using saved trusted local `sandbox_workspace_write.writable_roots` in workspace-write mode. Neither session uses `--add-dir` or an Atomic startup hook. |

## Approach

Use a separate shared Git clone with explicit allowlisted symlinks and manual synchronization; see [the design](../design/realm-sharing.md).

## Change tree

New implementation and test paths below are proposals; marked modifications name existing checkout paths.

```
atomic/cmd/atomic/
├── main.go ........................ M
├── cmd_realm.go ................... A
├── cmd_realm_test.go .............. A
├── cmd_realm_instruction_context_test.go .. A
├── cmd_realm_instructions_test.go .. A
├── cmd_realm_artifacts_test.go ..... A
├── cmd_realm_artifact_namespace_test.go .. A
├── cmd_realm_artifact_safeguards_test.go .. A
├── cmd_realm_evolution_test.go ..... A
├── cmd_realm_expanding_buckets_test.go .. A
├── cmd_where.go ................... M
└── cmd_where_test.go .............. M
atomic/internal/realmshare/
├── manifest.go .................... A
├── binding.go ..................... A
├── paths.go ....................... A
├── manifest_test.go ............... A
├── binding_test.go ................ A
├── share.go ....................... A
├── share_test.go .................. A
├── links.go ...................... A
├── recovery.go .................... A
├── links_test.go ................. A
├── git.go ......................... A
├── sync.go ........................ A
├── sync_test.go ................... A
├── status.go ...................... A
├── status_test.go ................. A
├── orientation.go ................. A
├── instructions.go ................ A
├── instructions_context.go ........ A
├── instructions_writer.go ......... A
├── instructions_writer_test.go .... A
├── instructions_recovery_test.go .. A
├── artifact_activation.go ......... A
├── artifact_policy.go ............. A
├── artifact_clone_scope.go ........ A
├── artifact_version.go ............ A
└── artifacts_sync_test.go ......... A
atomic/internal/harness/claude/
└── realm.go ....................... A
atomic/internal/runtimeproof/
├── realm_sharing_claude_test.go ... A
├── realm_sharing_codex_test.go .... A
├── realm_sharing_omp_test.go ...... A
├── realm_artifacts_claude_test.go . A
├── realm_artifacts_codex_test.go .. A
├── realm_artifacts_omp_test.go .... A
├── realm_wiki_claude_write_test.go . A
└── realm_wiki_codex_write_test.go .. A
atomic/internal/where/
├── where.go ....................... M
└── where_test.go .................. M
atomic/internal/wiki/
├── bucket_registry.go ............. M
├── action.go ...................... M
├── bucket.go ...................... M
├── bucket_test.go ................. M
├── bucketindex.go ................. M
├── bucketindex_test.go ............ M
├── action_test.go ................. M
├── wiki.go ........................ M
├── init_test.go ................... M
├── registry.go .................... M
└── registry_test.go ............... M
atomic/internal/serve/
├── walk.go ........................ M
├── walk_test.go ................... M
├── nav.go ......................... M
├── nav_test.go .................... M
├── search_md.go ................... M
├── api_search_test.go ............. M
├── serve.go ....................... M
└── serve_test.go .................. M
docs/guides/
├── realm-sharing.md ............... A
└── knowledge-base.md .............. M
docs/reference/
├── realm-wiki.md .................. M
└── commands.md .................... M
.vitepress/config.mts .............. M
context/skills/atomic-wiki/SKILL.md .. M
atomic/internal/embedded/bundle/skills/atomic-wiki/SKILL.md .. M
```

## Outline

```
atomic/cmd/atomic/main.go
  realm registration — reachable parent command
atomic/cmd/atomic/cmd_realm.go
  realm command family — six verbs, remote/path prompts, explicit approval flags, diagnostics, JSON status
atomic/cmd/atomic/cmd_realm_test.go
  command acceptance — registration, prompt and headless parity, dry-run immutability, help, trust stops, exits
atomic/cmd/atomic/cmd_realm_instruction_context_test.go
  startup acceptance — team and private context in eligible sessions and silence without prerequisites
atomic/cmd/atomic/cmd_realm_instructions_test.go
  instruction lifecycle acceptance — explicit linking, reconciliation and preservation of local steering
atomic/cmd/atomic/cmd_realm_artifacts_test.go
  activation acceptance — reviewed native units, collisions, dependencies and inert grants
atomic/cmd/atomic/cmd_realm_artifact_namespace_test.go
  namespace acceptance — managed checkout stays outside native artifact roots
atomic/cmd/atomic/cmd_realm_artifact_safeguards_test.go
  activation safeguards — executable evidence, collisions and retained ownership receipts
atomic/cmd/atomic/cmd_realm_evolution_test.go
  two-developer lifecycle — real Git exchange, divergent changes, unlink and restoration, private isolation
atomic/cmd/atomic/cmd_realm_expanding_buckets_test.go
  incremental sharing — bucket opt-in and explicit linking of received paths
atomic/cmd/atomic/cmd_where.go
  local identity output — binding-aware logical realm reporting
atomic/cmd/atomic/cmd_where_test.go
  where command acceptance — symlinked-path identity
atomic/internal/realmshare/manifest.go
  shared definition — portable identity, selections, buckets, artifact kinds, member definitions
atomic/internal/realmshare/binding.go
  local binding — trust, private buckets, pending sharing, loader/artifact receipts, restoration identity, backups
atomic/internal/realmshare/paths.go
  declared link resolution — logical paths, physical containment, capability checks
atomic/internal/realmshare/manifest_test.go
  shared validation — portable definitions and rejected unsafe inputs
atomic/internal/realmshare/binding_test.go
  local ownership — state-root selection and one-to-one associations
atomic/internal/realmshare/share.go
  publication — initial and additional selections, reviewed snapshot, portable registrations, safe cutover
atomic/internal/realmshare/share_test.go
  sharing acceptance — empty and bound remotes, exclusions, pending work, changed originals, resumable failures
atomic/internal/realmshare/links.go
  join and link lifecycle — validated download, trust, owned links, safe unlink
atomic/internal/realmshare/recovery.go
  recoverable mutation — serialization and interrupted-operation ownership
atomic/internal/realmshare/links_test.go
  link acceptance — collisions, repeats, denied capability, rollback, replacements
atomic/internal/realmshare/git.go
  Git boundary — clone-scoped operations and failure classification
atomic/internal/realmshare/sync.go
  explicit synchronization — selected changes, publication, integration, conflict reporting
atomic/internal/realmshare/sync_test.go
  Git acceptance — no-change receipt, concurrent pushes, conflicts, retained commits
atomic/internal/realmshare/status.go
  read-only inspection — identity, drift, cached Git facts, synchronization outcome
atomic/internal/realmshare/status_test.go
  status acceptance — text/JSON parity and no fetch or mutation
atomic/internal/realmshare/orientation.go
  local symlink index — workspace identity, explicit discovery roots, recovery guidance
atomic/internal/realmshare/instructions.go
  instruction activation — loader and hook ownership, private imports and cleanup
atomic/internal/realmshare/instructions_context.go
  startup context — eligible sessions, trusted shared source and separate private instructions
atomic/internal/realmshare/instructions_writer.go
  shared instruction updates — preserve the logical link while editing the verified source
atomic/internal/realmshare/instructions_writer_test.go
  writer acceptance — ownership, drift and update behavior
atomic/internal/realmshare/instructions_recovery_test.go
  cleanup recovery — interrupted removal and preservation of replacement files
atomic/internal/harness/claude/realm.go
  startup hook registration — local command, owned settings entry, preservation of unrelated hooks
atomic/internal/realmshare/artifact_activation.go
  realm artifact activation — native adapter reuse, scope, collisions and owned links
atomic/internal/realmshare/artifact_policy.go
  artifact review — selected units, dependencies and native grant checks
atomic/internal/realmshare/artifact_clone_scope.go
  clone scope — separation from native artifact search paths
atomic/internal/realmshare/artifact_version.go
  executable evidence — native harness identity and version checks
atomic/internal/realmshare/artifacts_sync_test.go
  artifact sync acceptance — unsafe changes refused and approved linked bytes preserved
atomic/internal/runtimeproof/realm_sharing_claude_test.go
  Claude startup proof — bounded native realm context evidence
atomic/internal/runtimeproof/realm_sharing_codex_test.go
  Codex startup proof — bounded native realm context evidence
atomic/internal/runtimeproof/realm_sharing_omp_test.go
  OMP startup proof — bounded native realm context evidence
atomic/internal/runtimeproof/realm_artifacts_claude_test.go
  Claude artifact proof — reviewed native discovery
atomic/internal/runtimeproof/realm_artifacts_codex_test.go
  Codex artifact proof — reviewed native skill discovery
atomic/internal/runtimeproof/realm_artifacts_omp_test.go
  OMP artifact proof — native candidate discovery
atomic/internal/where/where.go
  logical scope resolution — local realm identity through declared symlinks
atomic/internal/where/where_test.go
  scope acceptance — local root, symlinked path, backing-storage distinction
atomic/internal/wiki/bucket_registry.go
  portable bucket registrations — shared relative definitions and private local registration without metadata leakage
atomic/internal/wiki/action.go
  bucket resolution — local/shared ownership and consuming local realm as relative-path base
atomic/internal/wiki/bucket.go
  fingerprints — bounded declared symlinked-root traversal
atomic/internal/wiki/bucket_test.go
  fingerprint acceptance — symlinked roots and rejected nested escapes
atomic/internal/wiki/bucketindex.go
  topic indexing — resolved roots with logical output paths
atomic/internal/wiki/bucketindex_test.go
  index acceptance — portable shared bucket locations
atomic/internal/wiki/action_test.go
  bucket action acceptance — private add, selective sharing, distinct consuming realms, legacy local paths
atomic/internal/wiki/wiki.go
  symlinked wiki initialization — shared content without nested Git initialization
atomic/internal/wiki/init_test.go
  initialization acceptance — symlinked content and preserved local behavior
atomic/internal/wiki/registry.go
  instruction updates — validated target writes without replacing declared file symlinks
atomic/internal/wiki/registry_test.go
  writer acceptance — preserved symlink and refused unsafe target
atomic/internal/serve/walk.go
  declared-root traversal — bounded symlinked content with logical identity
atomic/internal/serve/walk_test.go
  traversal acceptance — declared roots and excluded backing content
atomic/internal/serve/nav.go
  navigation — symlinked entries under realm-local URLs
atomic/internal/serve/nav_test.go
  navigation acceptance — logical links and no duplicate backing paths
atomic/internal/serve/search_md.go
  markdown search — declared symlinks under existing result limits
atomic/internal/serve/api_search_test.go
  search acceptance — symlinked matches and excluded private content
atomic/internal/serve/serve.go
  content serving — authorized declared symlinks without broader storage exposure
atomic/internal/serve/serve_test.go
  content acceptance — logical URLs, authorization, traversal rejection
docs/guides/realm-sharing.md
  lifecycle guide — remote selection, initial and incremental sharing, prompts, agent flags, syncing, recovery, evidence limits
docs/guides/knowledge-base.md
  realm storage — shared symlinks alongside local and member content
docs/reference/realm-wiki.md
  ownership reference — local identity, portable buckets, symlinked wiki history
docs/reference/commands.md
  realm command reference — six verbs, remote/path selection and approval flags, status JSON, exits, optional hooks
.vitepress/config.mts
  guide navigation — realm-sharing guide entry
context/skills/atomic-wiki/SKILL.md
  bucket creation route — local registration first, explicit sharing approval, received-path review
atomic/internal/embedded/bundle/skills/atomic-wiki/SKILL.md
  generated skill — normal bundle projection of the updated canonical bucket route
```

## Flows

**Flow: publish an existing realm**

1. Developer -> `share`: selects an empty remote and branch through the terminal wizard or explicit arguments, then chooses files, folders, and registered buckets.
2. Atomic -> developer: previews paths, counts, bytes, exclusions, branch, remote, wiki snapshot semantics, and trust requirements.
3. Developer -> Atomic: approves publication; Atomic prepares and publishes the selected snapshot without changing original paths.
4. Atomic -> local realm: revalidates originals, retains backups and receipts, and installs trusted symlinks; a failure preserves or restores local content and reports any published snapshot.
5. Atomic -> developer: reports the local realm, managed storage, and next safe command.

**Flow: relink the publisher's restored originals**

1. Publisher -> `unlink`: Atomic restores originals and retains their restoration identity and recoverable backups.
2. Publisher -> `link`: Atomic verifies the restored originals against those receipts, including retained wiki Git state, even if shared content has advanced; changed originals stop without replacement.
3. Atomic -> local realm: revalidates eligible originals, retains or re-establishes their backups, and installs the owned links to current shared content.


**Flow: join, trust, and disconnect**

1. Colleague -> `join <git-url>`: downloads a shared definition into a distinct managed clone and records a local binding.
2. Atomic -> colleague: previews validated mappings without executing downloaded configuration or symlinking content.
3. Colleague -> `link`: reviews and explicitly trusts the definition; Atomic installs owned links and local discovery guidance after collision and capability checks.
4. Colleague -> `unlink`: Atomic removes unchanged owned links and restores originals while retaining shared edits, binding, and backups; replacements stop restoration rather than being overwritten.
5. Colleague -> `link`: Atomic revalidates ownership and trust and restores the symlinked working surface.

**Flow: add and receive a shared bucket**

1. B -> `atomic wiki bucket add sops`: registers a private local bucket without writing private metadata into shared wiki content or instructions.
2. B -> `atomic realm share sops/`: reviews the content and portable registration against the bound remote, approves publication, and receives an owned symlink after successful publication and source revalidation.
3. A -> `atomic realm sync`: receives the shared content and definition; Atomic reports `sops/` as available but unlinked without creating a local directory.
4. A -> `atomic realm link`: reviews and selects the new path; Atomic creates its symlink or reports a private-path collision without overwriting content or changing private registration.

**Flow: approve an agent operation**

1. Agent -> `share <path> --dry-run` or `link <path> --dry-run`: receives the proposed effects and review digest without changing state.
2. Agent -> developer: presents the exact preview, including the selected paths and effects, using the harness's approval UI.
3. Developer -> agent: approves the preview. The agent applies the same inputs with `--yes --review <digest>`. A mismatch requires another preview and approval.

**Flow: use shared and personal realm instructions**

1. Developer -> `AGENTS.local.md`: writes private realm preferences; these files never enter sharing selections or the clone.
2. Developer -> `share .agents/realm.md`: publishes only reviewed team guidance; linking prepares the enrolled local loader without replacing authored local text.
3. Harness -> local startup: when S27 applies, Claude receives team guidance through its startup hook and private guidance through its local loader. Other enrolled harnesses use their verified delivery path.
4. Developer -> `unlink` and later `link`: retains private context and authored originals while Atomic removes or rebuilds only owned shared references.

**Flow: enable writes to a linked wiki**

1. Developer -> `link wiki/`: reviews the backing path and links the wiki without changing permissions.
2. Developer -> local harness settings: adds the backing clone to writable directories.
3. Developer -> fresh session: writes through the realm path without `--add-dir`.

**Flow: opt in and activate realm artifacts**

1. Author -> `share` with individual skill, command, or agent paths: previews dependencies, behavior, and shared content; private and unselected units remain local.
2. Colleague -> `sync`: receives the source units without implicitly linking new units or granting permissions.
3. Colleague -> `link`: reviews native discovery, trust, scope, and collisions; Atomic creates only supported owned entries and reports unsupported activation separately.
4. Harness -> selected artifact: runtime discovery and invocation establish activation; native restart and external-import approval rules remain in force.



**Flow: exchange changes and resolve conflicts**

1. Developer -> realm-local symlinked path: edits or creates shared content while local-only work stays local.
2. Developer, agent, or explicitly configured hook -> `sync`: Atomic validates and commits selected changes and attempts publication from the bound clone.
3. Git remote -> Atomic: accepts publication, rejects authentication/network access, or rejects a non-fast-forward update; Atomic reports retained commits for access failures and integrates remote commits only for remote advancement.
4. Atomic -> developer: reports clean integration and one further push result, or preserves a merge conflict and lists logical and clone-relative affected paths.
5. Developer -> explicit `git -C <clone>` commands: resolves and stages conflicts and completes the merge; a later `sync` publishes the resolution.

**Flow: discover and inspect symlinked content**

1. Agent -> local orientation: reads the local realm identity and explicit symlinked discovery roots.
2. Agent or wiki command -> declared local root: reads, indexes, fingerprints, or updates content without traversing undeclared links or replacing owned file symlinks.
3. Browser -> serve: navigates and searches symlinked content through authorized realm-local URLs.
4. Developer or agent -> `status` or `status --json`: inspects drift, conflicts, unpublished changes, and cached remote knowledge without fetching or modifying files.

## Checkpoints

Each checkpoint delivers reachable behavior and its regression tests. Existing CLI files may recur as verbs gain complete behavior; no checkpoint exposes a successful stub. Estimates count the files changed within that checkpoint, including tests.

**Implementation prerequisite:** S26 is satisfied on the selected base before any checkpoint starts. The existing 7.0 harness integration is a dependency, not new work hidden in checkpoints 9–10 or a modification claimed by this change tree.

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Establish portable definitions, local ownership validation, and reachable realm help. | `main.go`, `cmd_realm.go`, `cmd_realm_test.go`; `realmshare/{manifest,binding,paths,manifest_test,binding_test}.go` | atomic-implementer (mode: feature) | ~8 | S1–S3; CLI help and validation rejection tests, harness-selected state paths, unrelated clone collision. |
| 2 | Deliver reviewed initial and incremental publication. | `realmshare/{share,share_test,git,links,recovery}.go`, CLI implementation/tests | atomic-implementer (mode: feature) | ~7 | S4–S5, S18–S19, S21; immutable preview and explicit approval; stable digest across CLI runs; stale approval after a same-length edit leaves local and remote state unchanged; retained wiki history, unrelated pending-work refusal, and resumable publication. |
| 3 | Deliver trusted join and recoverable link/unlink. | `realmshare/{links,recovery,links_test,binding,paths}.go`, CLI implementation/tests, `realmshare/share.go` | atomic-implementer (mode: feature) | ~8 | S6–S8, S19–S20; safe join, explicit selection, stable digest and immutable refusal after a changed definition or selected instruction, private collisions, exact ownership, restored-original relink and edited-original refusal, interruption, and mutation serialization. |
| 4 | Deliver explicit Git exchange and conflict recovery. | `realmshare/{sync,sync_test,git,binding}.go`, CLI implementation/tests | atomic-implementer (mode: feature) | ~6 | S9–S11; real two-clone remote advance, empty-change receipt, auth/network failures, dirty staging, conflicts, one retry, concurrent edits, symlinked and unlinked sync. |
| 5 | Deliver read-only status and logical local identity. | `realmshare/{status,status_test}.go`, CLI implementation/tests, `where/{where,where_test}.go`, `cmd_where.go`, `cmd_where_test.go` | atomic-implementer (mode: feature) | ~8 | S12–S13, S20–S21; text/JSON parity, cached knowledge, no fetch or writes, available/unlinked/conflicting/pending path states, drift, symlinked-descendant binding/state reuse, no shared-content state creation, backing-storage distinction. |
| 6 | Make private and shared buckets work without metadata leakage. | `wiki/{bucket_registry,action,bucket,bucket_test,bucketindex,bucketindex_test,action_test}.go` | atomic-implementer (mode: feature) | ~7 | S14, S20; private `bucket add` leaves shared wiki and instructions unchanged, selected registration and content publish together, two local roots resolve shared relative definitions, private absolute registrations remain local, symlinked fingerprints/indexes and nested-link exclusion. |
| 7 | Preserve symlinked wiki and shared instruction-source ownership. | `wiki/{wiki,init_test,registry,registry_test}.go`, `realmshare/{paths,status_test}.go` | atomic-implementer (mode: feature) | ~6 | S14; no nested `.git`, instruction-source update retains owned symlink and reaches target without publishing local loaders, unsafe target rejection and external replacement detection. |
| 8 | Expose declared symlinks safely in browser navigation and search. | `serve/{walk,walk_test,nav,nav_test,search_md,api_search_test,serve,serve_test}.go` | atomic-implementer (mode: feature) | ~8 | S15; local URLs and symlinked matches, existing authorization, rejected traversal and nested-link escapes, undeclared clone/private files absent. |
| 9 | Deliver shared and private realm instructions safely. | `realmshare/{instructions,instructions_test,orientation,binding,links}.go`, Claude adapter and CLI implementation/tests | atomic-implementer (mode: feature) | ~8 | S22, S25, S27; link before Claude is on `PATH`, repeat after installation without replacing symlinks, repeat again without duplicating the hook, then unlink and verify owned-only cleanup. Verify wiki-only preserves Claude configuration, startup loads both markers, and the remote excludes private and startup files. |
| 10 | Deliver individually opted-in realm artifacts through supported native surfaces. | `realmshare/{artifacts,artifacts_test,manifest,binding,links,status}.go`, CLI implementation/tests | atomic-implementer (mode: feature) | ~8 | S23–S25; per-unit selection and dependencies, trust review, existing personal collisions, no duplicate/native parent replacement, versioned discovery/invocation proof, unsupported scope/fields reported, no global or member writes. |
| 11 | Prove the expanding two-developer lifecycle and end-result realm usage. | `realmshare/{orientation,lifecycle_test,links,links_test}.go`, CLI implementation/tests | atomic-implementer (mode: feature) | ~6 | S13, S16–S25; real Git lifecycle, B's bucket addition, A's received-path review, private collisions, publisher relink safety, distinct personal instructions, opted-in artifacts and per-harness runtime limits, OMP replay and explicit unverified Claude/Windows states. |
| 12 | Publish lifecycle guidance, CLI contracts, and agent bucket routing. | `docs/guides/{realm-sharing,knowledge-base}.md`, `docs/reference/{realm-wiki,commands}.md`, `.vitepress/config.mts`, `cmd_realm.go`, `cmd_realm_test.go`, canonical and generated `atomic-wiki/SKILL.md`, `internal/runtimeproof/realm_wiki_{claude,codex}_write_test.go` | atomic-implementer (mode: feature) | ~11 | S1, S17–S25, S28; help and guide parity, opt-in, recovery, and deterministic macOS native no-grant/grant probes for Claude and Codex. The probes verify clone write access separately from startup activation and use no hosted model. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| A selected document contains confidential text despite path exclusions. | high | Require allowlisted publication preview and human review; state that arbitrary document contents are not secret-scanned or sandboxed. |
| Replacement writes or external editors detach a file symlink. | high | Preserve declared targets in Atomic-owned writers and detect drift before further mutation; never overwrite replacements on unlink. |
| Remote failure or competing pushes leave unpublished commits. | high | Preserve Git state, distinguish failure classes, bound retry, and report explicit recovery commands and cached status. |
| Cutover interruption or simultaneous local operations corrupt ownership. | medium | Serialize mutations, retain originals and durable receipts, and test interrupted recovery and changed-original refusal. |
| Physical path resolution changes realm identity or exposes storage. | medium | Separate logical identity from validated physical I/O and test where, wiki, browser authorization, and undeclared-path rejection together. |
| Generic harness discovery omits symlinked directories. | high | Generate explicit local discovery roots and test orientation consumption; document third-party tool limits. |
| Claude permissions or Windows symlink behavior differ from OMP/macOS evidence. | high | Keep support claims evidence-bound, repeat harness/platform lifecycle runs when available, and fail capability checks before mutation without copying. |
| Installed prerelease layout differs from checkout state paths. | medium | Reuse checkout configuration resolvers and test harness-selected roots rather than assuming fixture directory spellings. |
| A private bucket registration leaks through the shared wiki or root instructions. | high | Store private registrations and hints locally; test `bucket add` and selective publication against shared generated metadata. |
| A repeated share publishes drafts or a headless invocation accepts an implicit selection. | high | Require explicit paths and approval, immutable previews, bound-remote reuse, clean existing work, and recorded-operation-only recovery. |
| A generated loader or artifact dependency publishes personal instructions. | high | Exclude private files and loader bytes across initial share, additions, dependency traversal, and sync; test remote contents with distinct private markers. |
| Native discovery treats the same source differently across harnesses or repository boundaries. | high | Reuse native capability gates, prove each supported kind/scope at runtime, avoid duplicate discovery, and report unsupported activation without writing global or member configuration. |
| Reusable Markdown includes scripts or agent options that execute tools later. | high | Review executable behavior before linking, preserve user policy, and never equate source availability with safe or supported activation. |

## Implementation log


### Implemented locally: 2026-11-19


Built across 39 iterations of `/subagent-implementation`. Commits, in order:

- `c67f2ea764`: CP-1. Validate portable definitions and local ownership.
- `74605bc71b`: CP-2. Publish reviewed selections and retain originals.
- `4a6be8e0a5`: CP-3. Bind the remote and install recoverable, trusted links.
- `0ffb433d74`: CP-4. Exchange declared changes and preserve Git conflicts.
- `31b562b8b7`: CP-5. Report read-only status and keep logical realm identity.
- `e5b1478174`: CP-6. Keep private bucket registrations separate from portable ones.
- `002e31a67a`: CP-7. Preserve owned wiki and instruction links.
- `03069084d1`: CP-8. Serve verified shared documents under realm-local URLs.
- `7b7045189a`: CP-9. Deliver team and private guidance through enrolled harnesses.
- `7065edbb6e`: CP-10. Review individual artifacts and activate supported native entries.
- `b98a2fe9fb`: CP-11. Test two independent developer realms against a real Git remote.
- `02bbd9a41a`: CP-12. Document the lifecycle and verify saved Claude and Codex write grants.
- `e4f415aeff`: Align conflict help, native evidence, and the independent wiki refresh guard.
- `632b0dcbce`: Reconcile the changelog, design, and supported behavior.
- `689a3c23d7`: Align the persisted binding fixture and the static CLI registry with the implementation.
- `aad9573260`: Close the audit findings on linked scope, generated wiki metadata, and Git admission.

**Out-of-scope work performed during this build:** None.

**Unforeseens:**

- The native startup and artifact probes showed different behavior by harness, version, and launch scope. The [sharing guide](../guides/realm-sharing.md) distinguishes observed support from source availability.
- An independent audit requested corrections after the full Go suite passed. The corrections passed fresh review, focused tests on macOS and Linux, and native runtime checks on macOS. Windows runtime behavior was not verified.

**Deferred items still open:** None recorded.


## Change log

### 2026-11-19: Bind headless approval to the reviewed plan

**What changed:** `share` and `link` print a stable digest in preview and require `--yes --review <digest>` for headless apply.

**Why:** The approved selection or shared instructions can change between CLI processes.

**Superseded:** `--yes` alone does not authorize headless apply.

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
- An `ATOMIC_HARNESS` environment variable, a `.pi` repository-state root, a `harness.dir` setting, a `[pi]` config table, or an `atomic config resolve` verb. Atomic reads none of them, and setting one changes no behavior.


## Success criteria


- [ ] `context/` remains the only authored artifact corpus, and template expansion occurs once before harness projection.
- [ ] `context/AGENTS.md` is the sole authored global Atomic contract; the Claude adapter renders it directly into `~/.claude/CLAUDE.md`, followed inside the Atomic block by the adapter's native-vocabulary paragraph, without creating or referencing `~/.claude/AGENTS.md`.
- [ ] Repository and realm Claude steering takes the shape the `[steering] claude` mode selects; the global `~/.claude/CLAUDE.md` never uses a loader, and no mode delivers an instruction twice.
- [ ] `[steering] claude` selects one of three shapes. `loader`, the default, writes `AGENTS.md` plus a thin `CLAUDE.md` that imports `@AGENTS.md`. `agents-md` writes `AGENTS.md` only, which Claude Code 2.1.277 and later reads natively when no `CLAUDE.md` is present. `claude-md` writes `CLAUDE.md` only, with the managed block inline and no `AGENTS.md`, for Claude-only setups. The repository's committed `<state-root>/atomic.toml` overrides the user default in `~/.atomic/config.toml`.
- [ ] The mode applies to every steering pair Atomic writes: repository root, `docs/wiki/`, realm root, and realm `wiki/`. The global `~/.claude/CLAUDE.md` is the same in every mode, because Claude never reads `~/.claude/AGENTS.md`.
- [ ] A realm's mode comes from the realm root's `<state-root>/atomic.toml`, else `~/.atomic/config.toml`, else `loader`.
- [ ] When the effective mode changes, `atomic repo init` and `atomic update` convert the pairs of the repository or realm at the current working directory only, the same scope as card-link convergence. They touch only Atomic-owned blocks and loaders and keep user text. A file whose Atomic block the new mode retires is deleted only when Atomic owns the whole file (a ledger row, and nothing outside the owned block); when other text remains the block is removed and the file kept; when the block is all that remains but no ledger row proves Atomic wrote it, the block stays and the conversion reports a note. The root pair converts the same way whether or not its `AGENTS.md` carries an `<atomic>` block, so a hand-written or `/refresh-wiki` root `AGENTS.md` still loses its owned loader under `agents-md`. A wiki pair (`docs/wiki/` or the realm `wiki/`) is not ledgered; Atomic owns its `CLAUDE.md` or `AGENTS.md` when the bytes equal the scaffold it writes, and any other content is user text that stays, with a note.
- [ ] Every writer and checker of a steering pair follows the mode: the init and refresh writers, the wiki pipeline's `@`-ref wiring, doctor's router-reference check and its repair, and legacy guidance migration each write to or read from the file the mode makes Claude load.
- [ ] Doctor warns when the mode is `agents-md` and a sibling `CLAUDE.md`, `.claude/CLAUDE.md`, or `CLAUDE.local.md` exists, because Claude then skips `AGENTS.md`, or when `claude --version` reports a Claude Code older than 2.1.277. A missing `claude` binary or an unparseable version is a PASS whose detail carries one `info:` note and no version warning. Doctor warns when the mode is `claude-md` while OMP or Codex is enrolled, because both read `AGENTS.md`.
- [ ] The `<code-index>` block in the realm root goes to the file the mode makes Claude load: `AGENTS.md` under `agents-md`, where a `CLAUDE.md` beside it would hide `AGENTS.md`, else `CLAUDE.md`. A mode change moves the block, and the stub beside it, to that file and removes the file it leaves when nothing else remains in it.
- [ ] Under `loader`, conversion never creates a missing root `CLAUDE.md` beside an `AGENTS.md` that has no Atomic block, and leaves an owned loader unchanged. A `CLAUDE.md` that holds only Atomic's `<code-index>` block and stub, beside an `AGENTS.md`, is Atomic's: conversion adds the loader to it, so the `CLAUDE.md` imports `AGENTS.md`. Moving the block out of a file restores that file's bytes.
- [ ] `atomic wiki scan` creates a realm wiki pair only when none exists, in the resolved mode; it falls back to the loader shape only when `atomic.toml` cannot be parsed, and never converts an existing pair.
- [ ] An unknown `[steering] claude` value is a config error that fails loudly, the same way the other enumerated config keys fail.
- [ ] Each enrolled OMP profile receives one native `AGENTS.md` whose Atomic-owned content is the rendered Atomic steering body with its whole-line `@` imports kept, one blank-line separator, and the Atomic output-style body with parsed YAML frontmatter excluded, plus the adapter's native-vocabulary paragraph when a capability row proves at least one OMP tool name.
- [ ] OMP steering and output style are absent from plugin rules and packaged output-style artifacts; only actual path-scoped rules use the rule projection surface.
- [ ] Canonical commands, agents, skills, and rules have stable identities and source digests; generated native artifacts record source and projection digests.
- [ ] `RuleRecord` parses every shipped `context/rules/**` source into producer-qualified identity, class, `base_kind`, ordered include globs, body, and source digest without checkout-specific paths; target projection metadata remains separate.
- [ ] `RuleInstance` binds a record to one project key and absolute, symlink-resolved runtime base without changing record identity or source digest.
- [ ] Rule parsing rejects unknown scope keys, absolute patterns, parent-escaping patterns, target-name collisions, and malformed frontmatter before any target mutation.
- [ ] Canonical matching verifies resolved containment and supplies every overlapping matched record in stable identity order; two repositories and multiple worktrees share record identity but not runtime bases.
- [ ] Repository wiki refresh is the second canonical rule producer and writes pipeline-owned pointer cards under `<state-root>/rules/wiki/**`; realm refresh produces no scoped pointer rules. The `atomic wiki` verbs call `wiki.Refresh` and `(*omp.Adapter).PublishProjectCards`, so the cards have a production writer and reach every enrolled target.
- [ ] Wiki pointer cards are canonical only at `<state-root>/rules/wiki/<domain>.md`, which is committed. Claude loads repository path-scoped rules only from `.claude/rules/`, follows symlinks there, and discovers `.md` files recursively, so Claude reads the cards through one relative directory symlink `<repo>/.claude/rules/atomic -> ../../<state-root>/rules` (`../../.atomic/rules` for the default) as `.claude/rules/atomic/wiki/<domain>.md`. The link is tracked by Git, never ignored, and ledger-owned. It and the user-global shipped rules under `~/.claude/rules/atomic/` sit under different roots and do not collide. When the resolved state root is `.claude`, the cards already sit under `.claude/rules/wiki/` and no link is written. When the state root lies outside the repository, the Claude adapter writes a generated, ledger-owned copy at `<repo>/.claude/rules/atomic/wiki/` instead and ignores `/rules/atomic/` in `.claude/.gitignore`. OMP lists only `.md` files directly under `.omp/rules/` that carry a `description`, showing their `globs` as the scope, and reads no frontmatter key other than `description` and `globs`. It follows a per-file symlink and lists the card under the link's filename, and its rule scan skips any file a Git ignore source covers, judged on the link path. OMP therefore reads each card through one relative per-file symlink at `<repo>/.omp/rules/atomic-wiki-<domain>.md`, beside the repository's own rules, as the OMP card criteria below specify. A foreign file at that name is a named conflict and is preserved. A card dropped from the projection removes its link, or its fallback copy, when its ledger row matches, and keeps and reports it otherwise. An existing ledger-owned nested copy at `<repo>/.omp/rules/atomic/wiki/` is removed when its ledger row matches, along with its now-empty parent and the `/rules/atomic/` line in `.omp/.gitignore`, and kept and reported when the row does not match. OMP lists the card; the model loads its body through `rule://`, which no OMP path-match event does, so the tier stays unsupported. Audit row P-43 is closed at startup level. Codex has no path-scoped rule surface; it receives matching cards at edit time through a `PreToolUse` hook, and the Codex adapter reports wiki pointer cards as the Codex card criteria below specify. Apart from the outside-repository fallback copies, no canonical card is copied anywhere.
- [ ] Claude Code treats a rules symlink whose target lies outside the session's working directory as an external import: it asks for approval, and path-scoped rules reached that way do not load. A session started in a repository subdirectory therefore does not load the cards through the link until the user approves; in print mode Claude Code 2.1.322 shows no prompt and the cards stay unloaded for the session, while a regular file under `.claude/rules/` still loads. Claude reports a card loaded from the root at its resolved `<state-root>/rules/wiki/<domain>.md` path, not the link path. CP16 records this limit in the Claude capability row, and doctor names it from CP11 on (`https://code.claude.com/docs/en/memory`).
- [ ] `atomic state migrate`, `atomic repo init`, wiki refresh, and install and update convergence create the Claude link when it is missing and the state root lies inside the repository. Install and update act only on the repository of the current working directory. The same writers create the OMP card links when OMP is enrolled and a link is missing or stale. OMP session start writes no card file.
- [ ] Each canonical card `<state-root>/rules/wiki/<domain>.md` carries `description` and `globs` in its own frontmatter beside `paths:`. `globs` holds the same globs as `paths:`. `description` is the card's first non-blank body line with heading marks stripped, or `Atomic wiki card for <domain>` when the body is empty.
- [ ] When OMP is enrolled, wiki refresh and card publish write `<repo>/.omp/rules/atomic-wiki-<domain>.md -> ../../<state-root>/rules/wiki/<domain>.md`, a relative per-file symlink that is tracked by Git, never ignored, and ledger-owned. The link writer runs the same `git check-ignore -v` probe as the Claude link writer and reports a link that an ignore rule hides.
- [ ] An `.omp/rules/atomic-wiki-<domain>.md` symlink whose target equals the expected `../../<state-root>/rules/wiki/<domain>.md` is Atomic's and is adopted, with or without a ledger row, because a teammate's clone carries the committed link with no row. A link whose domain no longer has a card is pruned; without a ledger row it is pruned only when its target matches the `../../<state-root>/rules/wiki/<name>.md` pattern and dangles. Any other link or entry at that name is a conflict reported by name and never replaced. Target uninstall never deletes a tracked link: it reports it and leaves it.
- [ ] `atomic update` converts each generated flat card copy at `<repo>/.omp/rules/atomic-wiki-<domain>.md` into the link, running the conflict checks before any mutation.
- [ ] Ship flows and the wiki staging steps stage the OMP card links with the cards, staging only `.omp/rules/atomic-wiki-*.md` entries that are symlinks. The signals scan still skips `.omp/rules/atomic-wiki-` as refresh output.
- [ ] When the state root lies outside the repository, a relative link is impossible, so the OMP adapter writes a generated, ledger-owned flat copy at `<repo>/.omp/rules/atomic-wiki-<domain>.md` instead: frontmatter `description` and `globs`, a `# generated by atomic; do not hand-edit` comment, and the card's body. This mirrors the Claude adapter's outside-repository fallback and applies to that case only. These copies are generated per machine, left untracked, and never staged by any Atomic verb or ship flow.
- [ ] When the repository's ignore rules hide `.claude/rules/atomic`, the link writer runs `git check-ignore -v` on it. A matching ignore file inside the repository gets a negation appended, the same probe the wiki-rules negation uses; a matching ignore file outside it (a global excludes file) is reported by path and left alone, and the link is never force-added.
- [ ] A `.claude/rules/atomic` symlink whose target equals the expected relative target is Atomic's and is adopted, with or without a ledger row, because a teammate's clone carries the committed link with no row. Any other link or entry at that path is a conflict reported by name and never replaced. Target uninstall never deletes a tracked file in a user repository: it reports the link and leaves it.
- [ ] Claude projects shipped rules under `~/.claude/rules/atomic/` and reads the repository cards through the `.claude/rules/atomic` link with native scoped-rule behavior, and claims a native tier only after CP0 proves matching, precedence, lifetime, resumed-session, and child-session behavior.
- [ ] OMP projection maps canonical `paths:` into CP0-proven native scope fields and uses a CP0-proven pre-operation event to supply matched bodies when static scope is advisory; missing capability rows produce `unsupported`.
- [ ] Codex projection is an Atomic plugin using CP0-proven registration, session-baseline events, a synchronous pre-operation event exposing structured target paths, a context-return field, and any exact deny result; concrete API names remain capability evidence, not assumed contract.
- [ ] Codex enrollment adds a `PreToolUse` member to `$CODEX_HOME/hooks.json` with matcher `apply_patch` and command `atomic hooks pre-tool-use`, journaled and ledger-owned the same way as the `SessionStart` member. Like that member, it runs only after the user trusts it through Codex's `/hooks` review.
- [ ] `atomic hooks pre-tool-use` reads the `*** Add File: <path>`, `*** Update File: <path>`, and `*** Move to: <path>` headers from `tool_input.command`, resolves the repository root from the hook's `cwd`, matches each repository-relative path against each card's `paths:` globs, and returns the matching card bodies as `hookSpecificOutput.additionalContext`.
- [ ] Each card is sent at most once per session, keyed by `session_id` in a small state file under `~/.atomic/`. Each write drops the entries of sessions not seen in the last 7 days.
- [ ] The Codex capability row for wiki pointer cards reads `partial (edits only)` only after startup-level runtime evidence: in an isolated `CODEX_HOME`, Codex accepts the hook configuration and the hook emits `additionalContext` for an `apply_patch` payload. Until that evidence is recorded, the row reads `unsupported`. `atomic harness rules status` reports the Codex wiki-card tier as `hook-required` once that evidence is recorded and `unsupported` until then.
- [ ] The hook never fails the tool call: any error exits 0 with no output.
- [ ] Rule enforcement is reported as `native-scope`, `hook-required`, `deny-enforced`, or `unsupported`; only exact machine-checkable predicates may deny an operation, and semantic prose compliance is never described as hard enforcement. Codex wiki pointer cards report `hook-required` once their startup-level runtime evidence is recorded, and `unsupported` until then.
- [ ] `atomic harness rules status` and doctor report source/projection digests, tier, disabled or trust state, collisions, uncovered tools, and last runtime proof independently per target. An operation without a mediated exact path is uncovered, receives no matched rule body, and remains `unsupported`.
- [ ] OMP loads the enrolled profile's generated extension module from the profile agent root's `extensions/` directory, and the module registers only capability-proven events. Atomic's agents are written to `<profile agent root>/agents/`. Commands, skills, and rule bodies ship in `~/.atomic/packages/omp/atomic` as a corpus store and reach a native OMP discovery location only when a recorded runtime probe proves that location. Until then package installation, registration, and discovery of each artifact kind are `unsupported` and are reported in every enrollment, status, rules, and doctor surface rather than presented as delivered.
- [ ] Command invocation, arguments, agent dispatch, required dependencies, and explicit-only behavior match each milestone's capability matrix.
- [ ] Canonical agents declare no `model`, `effort`, `tools`, `disallowedTools`, or `mcpServers`; the projection refuses any agent that does. Each agent states behavioral intent, required capabilities, write scope, and execution preference, and its write scope is an instruction on every harness.
- [ ] A Codex TOML agent decodes to the canonical rendered agent body before Milestone A; Codex runtime support remains gated by Milestone B.
- [ ] `~/.atomic/config.toml`, profile/wiki data, `install/ledger.json`, `install/operation.lock`, journals, transaction stage/backups, recovery backups, legacy pre-install snapshot, and project state-location records have one documented writer and retention contract.
- [ ] Read-only discovery classifies `absent`, structurally `legacy-complete`, `legacy-partial`, `v2-clean`, `v2-in-flight`, `v2-orphaned`, or `mixed` from native bytes, legacy manifest/snapshots/proposals/settings, v2 ledger/journals/transactions/packages/registrations, and project selection; discovery never enrolls. A ledger row whose bytes no longer match is per-resource drift reported through `Drift` — a changed resource needs a replace-or-leave-unowned decision and an absent one is recreated — never a `mixed` state; `mixed` is reserved for unreadable v2 evidence and unjournaled remnants that overlap legacy evidence.
- [ ] Install, update, repair, adoption, uninstall, and any ledger-managed `atomic doctor --fix` acquire one global advisory lifecycle lock, reject unreadable newer schemas, recover unresolved journals oldest-first, and re-observe before planning any mutation.
- [ ] Dry-run leaves the complete filesystem byte-identical, reports the same pre-recovery classification and conflicts, and simulates journal-described writes in memory. Current journal/ledger/digest evidence that determines one safe result yields an advisory post-recovery plan; unavailable observations, decision-relevant external/native effects, later edits/conflicts, or user choice yield `blocked_on_recovery`.
- [ ] Only bytes matching the selected binary's embedded generation (for an agent file, the embedded projection with the user's stored override overlaid), uniquely parseable Atomic blocks, and bytes the ledger itself recorded applying are automatically verifiable ownership evidence; this design has no historical digest catalog. The ledger row is proof of Atomic's own last write, not a catalog of past generations: bytes matching neither the selected generation nor the ledger's recorded digest for that (target, resource) require a batched per-resource replace-or-leave-unowned decision. A leave-unowned decision on an agent file, and on any Claude whole-file artifact, is recorded per (target, path) in the ledger and honored by later operations that carry no decision — `claude install|update`, `harness repair`, `atomic update`, doctor category 1, and `harness status`, which count the kept file as converged — until the file vanishes or matches the selected bytes, `--replace` is given, the artifact leaves the corpus, or the target is uninstalled. An enrolled root's owned file whose bytes match neither the selected generation nor the ledger, including bytes an older binary's legacy writer wrote over it, waits for the batched decision; the home-wide `[install]` `adopted` mark never re-adopts an enrolled root. Names and legacy config paths never prove ownership; missing listed files remain drift until convergence is accepted, and unjournaled v2 remnants are conflicts.
- [ ] Pending proposed/merged Claude files, unjournaled v2 remnants, overlapping legacy/v2 evidence, an ambiguous managed block, and an unresolved journal recovery cannot complete block every adoption until explicitly resolved. A missing or corrupt legacy snapshot blocks `atomic harness adopt claude` until `--acknowledge-snapshot`; the automatic adoption that `atomic update` and a detecting `atomic install` run accepts it.
- [ ] A steering file that exists and carries no Atomic tag at all is "no block yet", not ambiguity: Claude adoption and OMP enrollment append the block and preserve every existing byte. A file whose Atomic tags do not parse to exactly one block still refuses loudly.
- [ ] The write-once legacy pre-install snapshot is preserved as recovery history. Before first v2 mutation, current observations, backups, and parent directories are durable; an empty or already-converged plan creates no journal or backup.
- [ ] Partial v2 recovery discards staging only with journal proof, ledger-commits applied bytes only after digest verification, restores backup bytes only when no later edit exists, and otherwise reports conflict. An absent generated-tree destination is the crash window a tree publication always produces: a staged tree still digesting to the intended value finishes the rename, an intact transaction backup is restored, and only when neither holds is it a conflict. A block unit whose file still carries no Atomic tags proves the write never landed and is discarded rather than reported as a conflict.
- [ ] `atomic harness recover [--rollback]` is the explicit resolver for an unresolved journal: it acquires the same advisory lifecycle lock and recovery run every other lifecycle verb performs, rolls forward by default, and restores digest-verified transaction backups under `--rollback`. `--dry-run` opens no lock and simulates each journal in memory. Every lifecycle conflict message names this verb.
- [ ] Legacy installation adoption writes no selection record, so a lone populated `.claude` resolves through the candidate rung and the next `atomic update` or `atomic repo init` migrates it.
- [ ] Explicit repository-state adoption (`atomic state adopt`) selects a root in place without moving files, ignores empty candidates, persists and fsyncs the selection record before resolution switches, treats `ATOMIC_STATE_DIR` as process-only, and rolls the selection back only when no later state write used the new root.
- [ ] Ledger and journal schemas record schema version, writer version, and minimum reader version. New binaries refuse newer incompatible schemas; old Claude-only binaries are unsupported writers after migration and their later changes surface as drift or conflict.
- [ ] Update, repair, doctor, and uninstall operate only on enrolled target instances, except the one-time Claude adoption `atomic update` runs. Discovery never enrolls: only `atomic install`, `atomic harness enroll`, `atomic harness adopt claude`, and that update adoption enroll a target.
- [ ] `atomic install` with neither `--harness` nor `--all` installs into every harness that installed-harness discovery finds: Claude when its native root exists (`CLAUDE_CONFIG_DIR`, else `~/.claude`), OMP when the profile agent root exists for the profile `OMP_PROFILE` names, else for the default profile, and Codex when `CODEX_HOME` is set and names an existing directory. Installed-harness discovery checks native roots only; it is not environment-marker detection. An unset `CODEX_HOME` stays the declared unsupported boundary, so Codex is never found without it. When nothing is found, it installs Claude. Before any plan it prints one line `detected: <harness> (<root>), ...` naming each selected harness and root. `--harness` narrows the set to one harness; `--all` keeps installing every discovered instance; `--instance`, `--dry-run`, `--yes`, and `--json` apply to the found set as they do to an explicit one.
- [ ] The automatic-adoption defaults are: older-version unowned Atomic artifacts replaced, each with a recovery copy under `~/.atomic/backups/<operation>/` written before the replacement (the rule `--discard-changed` follows), and an absent or corrupt legacy snapshot accepted. They apply to the `atomic update` adoption, to installed-harness discovery, and to `atomic install --harness claude`.
- [ ] A Claude root carrying legacy install evidence that `atomic install` selects, through installed-harness discovery or `--harness claude`, is adopted with the automatic-adoption defaults and stops on the same blockers as the `atomic update` adoption. An explicit `--replace` or `--leave-unowned` outranks the replace default.
- [ ] `atomic update` adopts the Claude root (`CLAUDE_CONFIG_DIR`, else `~/.claude`) once before convergence when that root is not enrolled and `~/.atomic/config.toml` carries the legacy `[install]` record without `adopted = true`, whatever other targets are enrolled. Every Claude adoption that takes over a present legacy record writes `adopted = true` into it after its ledger commit (a failed marker write is reported as such, and the next update writes the marker on the enrolled root without announcing an adoption), and every fresh legacy `[install]` write (`atomic claude install`) clears the marker, because a new legacy install is a new install to adopt; `config.toml` survives every uninstall, so a target the user uninstalled is never adopted again by update. Backups and an Atomic steering block are not a trigger, because v2 writes both. The explicit `atomic harness adopt claude` and `atomic install` ignore the marker. A ledger that cannot be read leaves enrollment unknown: update adopts nothing and convergence warns with repair advice. It prints `adopting existing Claude install at <root>`, runs the Claude adoption with the automatic-adoption defaults (the `--replace` and `--acknowledge-snapshot` outcomes, plus recovery copies), then converges every enrolled target as normal. A later update finds the root enrolled and prints no adoption line; after `atomic harness uninstall` of that root, update prints none either and leaves it unenrolled; an enrolled Claude root or an OMP-only home with an unreadable ledger warns with repair advice and exits 0.
- [ ] When that adoption hits a blocker involving user content (a pending proposed or merged Claude steering file, unjournaled v2 remnants, overlapping legacy/v2 evidence, an ambiguous managed block, or an unresolved journal recovery cannot complete), `atomic update` adopts nothing, exits 1, and prints a message naming the file or journal and telling the user to resolve it and re-run `atomic update`; a journal message also names `atomic harness recover`. `--skip-claude-update` skips the adoption along with enrolled-target convergence. `atomic harness adopt claude` remains the explicit verb with `--instance`, `--replace`, `--leave-unowned`, `--acknowledge-snapshot`, `--dry-run`, `--yes`, and `--json`. A home with neither a legacy install nor an enrollment stays a no-op.
- [ ] One physical resource has one ownership record even when several targets or unenrolled instances can discover it; OMP shared-package visibility and consumer enrollment remain separate and visible.
- [ ] A shared-package generation move converges every enrolled consumer of that resource in one operation: the publishing plan updates each consumer's package row in the same commit, so a row recording an older generation is stale by construction and never blocks another consumer. Convergence refuses before mutation, naming both sides, only when the resource carries a generation claim this operation cannot move — a row for a target the ledger does not enroll whose key parses — because nothing in the run will converge that consumer. A row whose target key does not parse is a stale record an older writer left: nothing enrolls it and nothing will move it, so it never pins the resource and the publication rewrites it with the rest. The enrollment asserts that every consumer the check named was rewritten onto the selected generation in that same commit, and a row-only removal reaches such a stale key.
- [ ] Every multi-resource operation observes the complete plan, persists a durable journal and backup before mutation, stages and validates projections, records each native commit unit, verifies effective content, and only then commits ledger state. The one exception is the Codex `hooks.json` member, which is edited member-wise outside the transaction: its row is committed to the ledger before the write, removed again when the write is skipped or fails, and a recorded canonical row over a changed member is rewritten by `--replace` and blocks otherwise.
- [ ] Initial generated-directory publication uses one same-filesystem rename; replacement uses a journaled current-to-backup and staging-to-current sequence.
- [ ] Repository-state resolution follows `ATOMIC_STATE_DIR`, then `~/.atomic/<project-key>/state-location.json`, then the candidate rung, then the user `state.dir` default, then `.atomic`. Both the strict resolver and the cached form every path helper uses follow this ladder. Resolution never consults harness markers or harness detection, and introduces no second project-key hierarchy. An invalid `ATOMIC_STATE_DIR` value refuses in both forms instead of falling through.
- [ ] Candidate names are `.atomic`, the configured `state.dir` path, and `.claude`. A candidate is populated when it holds `atomic.toml`, `.atomic-index`, `.scratchpad`, `project`, or `rules/wiki`; `worktrees`, settings, agents, and every other harness-native entry are not signatures. The destination candidate is the configured `state.dir` path, else `.atomic`. When the destination is populated it answers, and each other populated candidate draws one stderr warning per process naming its leftover entries, because checking out a pre-migration branch restores tracked `.claude/project/` beside a migrated `.atomic`. When the destination is unpopulated, exactly one populated other candidate answers in place. Two populated non-destination candidates are ambiguous: a verb that resolves repository state refuses with the `atomic state adopt` instruction instead of falling through to a lower rung.
- [ ] `.atomic` is the built-in default for the state setting, and `state.dir` is the only configured state setting. No config write emits `[harness] dir`. A `harness.dir` key or a `[pi]` table stored in `~/.atomic/config.toml` is neither read nor a candidate name: it loads with no unknown-key warning, the next config write drops it, doctor reports nothing for it, and `atomic config set harness.dir` refuses as an unknown key. `state.dir` accepts a repository-relative path of one or more safe segments (`.config/atomic`). `atomic state adopt --dir <path>` persists any absolute or repository-relative location for every worktree of the clone.
- [ ] Every helper that builds a repository-state path receives the repository root. No production caller passes an empty root, so `atomic repo init`, the `.gitignore` writers, and the signals tree honor the selection record and the candidate rung.
- [ ] An empty repository creates no state candidate or adoption record until a stateful operation needs one; adoption persists before scope resolution switches.
- [ ] Repo wiki initialization requires Git, creates or amends the root and `docs/wiki/` steering pairs in the shape the `[steering] claude` mode selects (in the default `loader` mode, a shared `AGENTS.md` plus an adjacent thin Claude loader) without wholesale overwrite, and rejects malformed or conflicting loaders.
- [ ] Realm initialization may start from a non-Git root, creates the `<realm>/` and `<realm>/wiki/` steering pairs (`AGENTS.md` plus `CLAUDE.md` in `loader` mode, only `AGENTS.md` in `agents-md`, only `CLAUDE.md` in `claude-md`), makes `<realm>/wiki/` its own Git repository, and writes registration authority only to `~/.atomic/wikis.md`.
- [ ] Repository wiki refresh writes canonical scan, index, domain, steering, and pointer-rule files first; realm refresh writes realm and nested-wiki steering plus scan, member, bucket, and domain files without scoped pointer rules. Both auto-converge only already-enrolled targets, and target failure never rolls back canonical output.
- [ ] Worktree refresh writes canonical repository output into the invoked worktree while project-keyed state selection and operation records remain shared.
- [ ] Unowned guidance remains byte-identical at global, project, and nested wiki scopes; Atomic never relocates it.
- [ ] `~/.atomic/profile.md` and `~/.atomic/wikis.md` are authoritative after one-time legacy adoption; later divergent native copies are conflicts and are never imported automatically.
- [ ] Existing user model settings, role overrides, native override files, disabled plugins, hook trust decisions, and profile guidance outside Atomic-owned blocks remain authoritative.
- [ ] A target uninstall removes only unchanged resources owned for that target and leaves shared resources while another consumer exists; a resource the removal cannot clear is reported skipped with its reason and the exact next step, and keeps its claim until `atomic harness uninstall --discard-changed` approves discarding that resource by name — a file or tree is removed, a managed block is stripped, and a settings file Atomic cannot write keeps the user's bytes while its claim is released. Every directory a removed file resource emptied below the target's native root is removed with it, on every target, never the root and never one that still holds anything. A target key that does not parse is still removable by its raw ledger key.
- [ ] Full uninstall after the final target preserves `config.toml`, `profile.md`, `wikis.md`, and backups; removes completed operational and adoption state; and retains unresolved journals plus referenced transaction backups, ledger rows, and state-location records until recovery completes.
- [ ] A downloaded update re-executes the replacement binary before target convergence, applies and records only that binary's embedded artifact generation with the user's stored agent overrides overlaid, and never lets the stale process publish its corpus.
- [ ] An already-current normal update skips download but still converges enrolled targets; `atomic update --check` remains read-only.
- [ ] `atomic doctor` reports target instances, shared resources, unfinished journals, capability gaps, stale materializations, deliberate disablement or untrusted hooks, effective-content shadowing, and uncovered rule operations independently.
- [ ] Existing `atomic doctor` category names and numeric indices remain unchanged; multi-harness categories append with stable names and indices.
- [ ] Milestone A passes Claude and OMP runtime scenarios for global, root, nested wiki, resumed, and child sessions, including absence of duplicate Atomic or output-style application and exact rule matching behavior.
- [ ] Milestone B passes Codex runtime scenarios for custom `CODEX_HOME` roots — plugin trust and disablement, override precedence, shared skills, native agents, matching and nonmatching rules, supported tools, shell and hosted operations without safe paths, resumed and child sessions — and records the default home as a declared unsupported outcome rather than a proven one: with `CODEX_HOME` unset, explicit selection refuses naming the variable, a broad `--all` walk skips the kind without error, and no default-root package or delivery claim appears. Uncovered operations prove no matched body was delivered.
- [ ] The harness-neutral state root, state migration, harness orientation, per-harness agent overrides with native agent delivery, and session context delivery ship their Claude and OMP halves with Milestone A and their Codex halves with Milestone B. A half that depends on an unproven capability (agent discovery, hook trust, context delivery) reports `unsupported` until its runtime probe passes.
- [ ] Each adapter appends a short native-vocabulary paragraph inside the Atomic block of the steering it renders, golden-tested, naming the native tool for each behavior the canonical corpus names: for Claude Code, the Agent tool with `subagent_type` set to the agent's name, the question tool, the worktree-entry tool, and the background-listener tool with `timeout_ms` (capped at 30 minutes) and its stop tool; for OMP and Codex, only names a capability row proves. The paragraph carries tool names only, never policy, and the canonical corpus stays harness-neutral.
- [ ] Harness detection is one pure function over an environment map that reads no process state. It returns the harness, a confidence of `strong`, `weak`, `ambiguous`, or `none`, the evidence as variable names only (never a value), and the candidate kinds when ambiguous.

    | Harness | Strong markers | Weak markers | Never a marker |
    |---------|----------------|--------------|----------------|
    | Claude Code | `CLAUDECODE=1`, `CLAUDE_CODE_ENTRYPOINT`, `CLAUDE_CODE_SESSION_ID`, `AI_AGENT` starting `claude-code` | none | `CLAUDE_PLUGIN_ROOT`, `CLAUDE_PLUGIN_DATA`, `CLAUDE_CONFIG_DIR` |
    | Codex | `CODEX_THREAD_ID`, `CODEX_CI=1` | `CODEX_SANDBOX` | `CODEX_HOME` |
    | OMP | none | `AGENT=1` together with `CI=true` | `PI_CODING_AGENT`, `PI_CODING_AGENT_DIR`, `OMP_PROFILE` |

    Strong markers for exactly one harness return `strong`. Strong markers for two or more return `ambiguous` with no harness chosen, because markers inherit into child processes and a nested harness keeps its parent's markers. Only weak markers return `weak`; no markers return `none`.
- [ ] One rule governs a detection result. An explicit `--harness` flag outranks detection. `strong` acts. `weak` acts only for a verb that reads or adds context; a verb that writes into a harness root takes no action on `weak`. On `weak`, `ambiguous`, or `none`, a verb that writes into a harness root refuses and asks for `--harness`, and on `ambiguous` or `none` a verb that only adds context adds nothing. The one exception is `atomic config agents`, which uses the single enrolled harness when exactly one is enrolled, because it writes into a harness root only through lifecycle convergence of targets that are already enrolled. `atomic install` never consults this detection: without `--harness` or `--all` it uses installed-harness discovery, as its own criterion states.
- [ ] Every hook registration Atomic writes (Claude `settings.json`, the OMP extension, Codex `hooks.json`) passes `--harness <kind>` explicitly, so no hook entrypoint depends on detection. The ownership match recognizes the unflagged and the flagged command alike, so an existing Claude registration converges to the flagged command as Atomic-owned bytes with no duplicate and no conflict.
- [ ] Orientation consumers act on detection as follows:

    | Consumer | Behavior |
    |----------|----------|
    | `atomic config agents` | `--harness` wins; otherwise a `strong` detection; otherwise the single enrolled harness when exactly one is enrolled; otherwise an interactive choice, or a refusal listing the enrolled kinds when non-interactive |
    | `atomic retro extract` | Reads the detected harness's session history: Claude from its projects directory; OMP and Codex report `unsupported` naming the harness until a reader for that history format is proven against real session files, instead of failing on a missing Claude directory |
    | `atomic where --json` | Adds `state_root` (the resolved absolute repository state root) and `worktrees` (defined below) |
    | `atomic bus` | Session identity from `CLAUDE_CODE_SESSION_ID` under Claude and `CODEX_THREAD_ID` under Codex; any other orientation reports that no session identity is available |
    | `atomic doctor` | Runs the active-harness leg for the detected harness in addition to every enrolled target's checks |
    | `atomic hooks session-start` | Refreshes `~/.atomic/profile.md` on every harness, because the profile and the workflow-preference autodetect are harness-neutral; seeds the output style, which writes Claude's `settings.json`, only under `--harness claude` |

- [ ] `atomic wiki` does not consult detection: it writes `~/.atomic/wikis.md` first, then projects the `<wikis>` block into the global steering of every enrolled target, and never creates `~/.claude/CLAUDE.md` on a home with no Claude target.
- [ ] `atomic where --json` `worktrees` is the recommended worktree directory for the harness passed with `--harness` or else detected. A set `worktrees` preference path outranks the harness default; otherwise it is `.claude/worktrees` for `claude` and `<state-root>/worktrees` for `omp`, `codex`, and an unknown harness. The value is repository-relative when it lies inside the repository and absolute otherwise. It is `""` only when the preference is `off`. `atomic where` builds this field through the one worktree helper keyed by harness, which `atomic workflow resolve` also calls; it reads only the preference value.
- [ ] An install on any harness creates the `~/.atomic/profile.md` stub when it is absent.
- [ ] `atomic hooks session-start` takes `--harness` and `--cwd`; with no `--harness` it behaves as `--harness claude`, so a Claude registration written before the flag existed keeps working. It reads stdin when present and never requires it, and resolves the repository from `--cwd` (else stdin `cwd`, else the process directory) through the scope marker and Git, never by treating the directory as the repository root verbatim. It resolves the realm and profile levels from the same directory and emits two sections: orientation and the resolved workflow-preferences block owned by `docs/spec/workflow-preferences.md`.
- [ ] The migration destination is the selection record, else `state.dir`, else `.atomic`. `ATOMIC_STATE_DIR` is process-only and is never a migration destination. When the selection record or `state.dir` names the legacy root itself, the user chose it and nothing moves.
- [ ] `atomic state migrate` moves `atomic.toml`, `workflow-preferences.md`, `project/`, `rules/wiki/`, and `.scratchpad/` from the legacy `.claude` root into the destination. It leaves `worktrees/` and every harness-native entry in place. It never moves the code index: it removes the legacy index directory (database, write-ahead files, MCP socket, and lock) only when no MCP daemon is live there (no daemon answers on its socket and no starter holds its spawn lock); while one is live it leaves the directory and reports it, and `atomic code index` builds a fresh index at the destination. A leftover `.claude` that holds no signature other than `.atomic-index` is exempt from the both-populated refusal: a later `atomic state migrate` removes it once no daemon is live there and exits 0, and while one is live it reports the path and exits 0; until it is removed the leftover warning names it.
- [ ] The migration moves tracked files and stages each rename as `git mv` would, moves ignored files with a rename, falls back to copy-and-remove across devices, never commits, and prints the staged renames. It refuses a tracked entry that `git mv` would refuse or mishandle (a submodule gitlink, an unmerged path). A failure partway reverses the moves already made, so a re-run starts from the legacy root. It writes no `state-location.json`. It writes the destination's managed `.gitignore` rules, stages that file with the renames, and leaves the legacy root's `.gitignore` in place.
- [ ] Only `atomic state migrate`, `atomic repo init`, and `atomic update` run inside a repository start the migration, each with a notice on stderr, when the legacy `.claude` root holds an Atomic signature other than `.atomic-index` and the destination holds none. A legacy root holding only `.atomic-index` is not migrated; it keeps resolving in place. The migration refuses a repository root equal to the user's home directory. Every other verb, including `atomic code index`, `atomic scratchpad new`, `atomic followups add`, the `atomic wiki` verbs, `atomic workflow set`, `atomic where`, `atomic doctor`, and `atomic serve`, reads and writes the populated legacy root in place and never moves files.
- [ ] A realm root migrates the same way; a realm root that is not a Git repository migrates with plain renames. When the destination is `.atomic` the realm federation files (`code.toml`, `<key>.db`) are already in place; when it is not, `<realm>/.atomic/{code.toml,*.db}` is a second migration source and those files move to the destination. Until the migration runs, a federation reader also accepts the legacy `<realm>/.atomic/` location.
- [ ] When the legacy root and the destination both hold signatures, and the legacy root holds a signature other than `.atomic-index`, `atomic state migrate` moves nothing, exits 1, and names the leftover `.claude` entries for the user to merge or delete; `atomic repo init` and `atomic update` print the one resolution warning, not a second one, and continue against the destination.
- [ ] A migration that runs inside a verb invalidates the process's resolution cache before the verb continues, so no later path helper resolves the pre-move root.
- [ ] The migration reports, and never rewrites, references the user wired by hand to the legacy paths, such as an `@.claude/project/followups/INDEX.md` import in a steering file or a hook script that names `.claude/project/`.
- [ ] After the move, the migration creates the Claude link and, when OMP is enrolled, relinks the OMP card links to the moved cards, adding every link to the Git index with the renames before it returns, so Claude and OMP keep loading the cards.
- [ ] The OMP card writer writes no `.omp/.gitignore` rule and creates no such file, because OMP skips ignored rule files. No writer adds a rule that ignores the Claude link or an OMP card link, and nothing commits them; ship flows stage them.
- [ ] The repository-state `.gitignore` written by `atomic repo init` carries `/.scratchpad/`, `/.atomic-index/`, `/project/doc-surfaces.md` (the `atomic docs scan` cache, regenerated on every run), and `/worktrees/`, because `<state-root>/worktrees/` is the recommended worktree location for OMP and Codex; `atomic repo init` also ignores `/worktrees/` in `.claude/.gitignore` when `.claude/` exists or a Claude target is enrolled, so no linked worktree appears as an untracked nested repository and an OMP-only or Codex-only repository never gains a `.claude/` directory. Every ignore-file writer computes its rules from the resolved state root, and a state root outside the repository receives no repository ignore rule.
- [ ] Realm roots resolve through the same ladder. The realm code-intel federation files (`code.toml` and the per-member databases) live in the resolved realm state root, or in `<realm>/.atomic/` when that root is `.claude`, and that directory's `.gitignore` ignores `/*.db`.
- [ ] `atomic serve` exempts the resolved state root from its leading-dot directory skip, so cited cards and follow-ups stay servable under `.atomic` or a custom root.
- [ ] `~/.atomic/config.toml` holds agent overrides in per-harness tables `[claude.agents.<name>]`, `[omp.agents.<name>]`, and `[codex.agents.<name>]`, each with `model` and `effort`, decoded into one shared `AgentOverride`. A scalar under any `agents` table is a decode error.
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
- [ ] Codex session context: the Codex adapter writes one `SessionStart` member (matcher `startup|resume`, command `atomic hooks session-start --harness codex`) into `$CODEX_HOME/hooks.json`, ledger-owned member-wise like the Claude `settings.json` members, preserving every other byte. Atomic never fabricates hook trust. Doctor locates the trust record Codex keeps for each owned member (the `SessionStart` member and the `PreToolUse` card member) from that member's event and observed position in `<symlink-resolved CODEX_HOME>/hooks.json`, not from a fixed `0:0` key shape, and reports per member what codex-cli 0.147.0 reports: `trusted` when the recorded hash matches the member, `modified` when it differs (with a fix line to re-trust the hook in Codex's `/hooks`), `untrusted` when only other hooks carry a record, and `unknown` when no record is readable. Once trusted, the hook's context reaches the session at startup level; model-level delivery is unproven until a credentialed run observes it.
- [ ] A session-start hook that fails, cannot find `atomic`, or times out adds nothing and never blocks or fails the session.
- [ ] Every finding in the "Review findings carried to the remediation checkpoint" list of `docs/research/harness-parity-audit.md`, and every parity row that list names, is closed by the checkpoint the list names for it (a `CP14` row unless the list names an earlier one; the PR-body nit is fixed in the PR text) with a regression test or a re-verified behavior, and each Verifies line names the finding it closes by symbol. After the last row, `atomic code comments --diff origin/next...HEAD` exits 0 and no added comment carries a checkpoint-ID token. CP14D rows clear the files flagged when their lists were taken; every P0-P8 row and every P10 and P11 row leaves its own added files clean under the comments check; CP14D17 closes any other file the tool still flags.
- [ ] Every `open` row of `docs/research/harness-parity-audit.md` names its closing checkpoint, and that checkpoint's Verifies cites it by P-ID. Rows with status `redesign` close with the redesign work that covers them; rows with status `redesign` or `accepted` are outside the parity checkpoints.
- [ ] Every audit gap open for Claude (`CP15A1` through `CP15A5`) is closed with a regression test, so a Claude user keeps every behavior `next` delivers.
- [ ] OMP and Codex receive skills, commands, rules, and global-steering features at their native locations only after a recorded runtime probe proves each location; until then each artifact kind reads `unsupported` at WARN severity in enrollment, status, rules, and doctor. Codex commands stay `unsupported` until a probe proves a non-deprecated explicit invocation surface with argument delivery.
- [ ] Runtime proof on real Claude Code, OMP, and Codex installs shows state migration from `.claude` to `.atomic`, an agent override projected into the native agent file and the agent listed at startup, and session context carrying the resolved workflow-preferences block delivered at startup level (the harness's hook output or init event shows the block), with model-level proof left to a credentialed run; a half whose probe fails is recorded `unsupported` with its observation.


## Approach

Canonical instruction bodies with narrow native harness adapters — see `docs/design/omp-plugin-compatibility.md`.


## Change tree


```
planning and capability evidence
├── docs/design/omp-plugin-compatibility.md ... A  (architecture and approach)
├── docs/spec/omp-plugin-compatibility.md ..... M  (implementation contract and amendments)
├── docs/research/harness-capability-matrix.md  A  (versioned native behavior evidence; agent, session-start, and wiki-card rows)
├── docs/research/harness-parity-audit.md .... A  (parity gaps with their closing checkpoints)
└── <state-root>/.scratchpad/omp-plugin-compatibility/
    ├── capabilities/ ........................ A  (raw CP0 observations)
    └── runtime/ ............................. A  (retained CP8 and CP16 evidence)

context/
├── AGENTS.md ................................ A  (global Atomic steering source; state-root path text)
├── CLAUDE.md ................................ D  (global source replaced by adapter projection)
├── agents/atomic-*.md ....................... M  (portable semantic metadata; no model, effort, or tools)
├── commands/*.md ............................ M  (portable invocation and dispatch outcomes; state-root path text)
├── _partials/*.md ........................... M  (shared portable orchestration blocks; state-root path text; signals-gate and loop-finalize stage OMP card symlinks with the cards)
├── skills/atomic-*/ ......................... M  (portable paths and dependency declarations; state-root path text; OMP card links staged with the cards)
├── rules/**/*.md ............................ M  (actual path-scoped rule metadata)
└── commands/atomic-help.md .................. M  (new lifecycle discovery; state-file tour stage)

repository and realm state (outputs written into a user repository or realm)
├── <repo>/<state-root>/ ..................... A  (.atomic by default: atomic.toml, workflow-preferences.md, project/, rules/wiki/, .scratchpad/, .atomic-index/)
├── <repo>/<state-root>/.gitignore ........... A  (/.scratchpad/, /.atomic-index/, /worktrees/)
├── <repo>/.claude/.gitignore ................ M  (/worktrees/; /rules/atomic/ only for an outside-repository state root)
├── <repo>/.claude/rules/atomic .............. A  (committed relative symlink to <state-root>/rules)
├── <repo>/.omp/rules/atomic-wiki-<domain>.md  A  (committed relative symlink to <state-root>/rules/wiki/<domain>.md, one per card; generated copy only for an outside-repository state root)
└── <realm>/<state-root>/ .................... A  (realm marker, code.toml, per-member databases ignored by /*.db; federation in .atomic/ when this is .claude)

native harness outputs (written by adapters into user homes)
├── <profile agent root>/agents/*.md ......... A  (OMP agents with overrides overlaid)
├── <profile agent root>/extensions/atomic.ts  A  (generated extension module; session-context handler)
├── $CODEX_HOME/agents/*.toml ................ A  (Codex agents with overrides overlaid)
└── $CODEX_HOME/hooks.json ................... M  (owned SessionStart and PreToolUse apply_patch members)

repository and realm steering (init and adapter outputs written into a user
repository or realm; never committed repo files here, and this repo's root
CLAUDE.md stays the contributor project overlay). Each pair takes the
[steering] claude shape: loader writes both files, agents-md only AGENTS.md,
claude-md only CLAUDE.md with the managed block inline.
├── <repo>/AGENTS.md ......................... A  (project steering source; absent in claude-md)
├── <repo>/CLAUDE.md ......................... A  (project @AGENTS.md loader; inline block in claude-md; absent in agents-md)
├── <repo>/docs/wiki/AGENTS.md ............... A  (nested wiki steering source; absent in claude-md)
├── <repo>/docs/wiki/CLAUDE.md ............... A  (nested @AGENTS.md loader; inline block in claude-md; absent in agents-md)
├── <realm>/AGENTS.md ........................ A  (capture/member steering source; absent in claude-md)
├── <realm>/CLAUDE.md ........................ A  (realm @AGENTS.md loader; inline block in claude-md; absent in agents-md)
├── <realm>/wiki/AGENTS.md ................... A  (realm wiki steering source; absent in claude-md)
└── <realm>/wiki/CLAUDE.md ................... A  (realm wiki @AGENTS.md loader; inline block in claude-md; absent in agents-md)

atomic/internal/
├── artifacts/ ............................... A  (canonical corpus enumeration, rendering, identity, projection digests; Claude native-vocabulary paragraph in renderer.go ClaudeSteering; LoaderPair stays loader-shaped, the other modes are shaped by harness/claude/steering.go and wiki/init.go)
│   └── tests and testdata ................... A  (deterministic render and golden projections)
├── harness/
│   ├── harness.go ........................... A  (adapter contract and registry)
│   ├── agent.go ............................. A  (offline agent projections; Codex model keys)
│   ├── detect.go ............................ A  (pure environment-map harness detection)
│   ├── overlay.go ........................... A  (pure per-harness agent model and effort overlay)
│   ├── capability.go ........................ A  (agent-delivery, session-start, and artifact-discovery rows)
│   ├── uninstall.go ......................... A  (shared consumers matched by physical path; symlink-safe block removal)
│   ├── rule.go .............................. M  (ClaudeRulePath for linked wiki cards)
│   ├── claude/ .............................. A  (Claude projection and lifecycle adapter; card projection; overlaid agents; steering through ClaudeSteering; steering-mode pair writer, conversion, and mode-shaped guidance migration)
│   ├── omp/ ................................. A  (OMP package, steering, profiles, rules, session adapter; native agents; session-context handler; card links and copy-to-link migration)
│   ├── codex/ ............................... A  (Codex projection and lifecycle adapter; native agents; SessionStart and PreToolUse hooks.json members)
│   └── testdata/ ............................ A  (native, migration, worktree, and non-git fixtures)
├── installstate/ ............................ A  (enrollment, ledger v2, journal, recovery; journaled target records; left-unowned records; non-blocking lock; commit ordering)
│   └── tests and testdata ................... A  (commit-unit interruption and adoption fixtures)
├── managedfile/ ............................. A  (managed blocks, backups, safe publication)
│   └── tests and testdata ................... A  (file, block, and generated-tree fixtures)
├── rules/ ................................... A  (RuleRecord parser, matcher, validation, enforcement tiers)
│   └── tests and testdata ................... A  (overlap, containment, collision, rename, and deletion fixtures)
├── runtimeproof/ ............................ A  (isolated harness scenario runner; real-install scenarios)
├── bundlespec/bundlespec.go ................. M  (AGENTS.md and canonical steering inclusion; mode-shaped ScopeSteering)
├── bundlemirror/mirror.go ................... M  (Claude-native target mapping from the canonical corpus)
├── claudeinstall/ ........................... M  (legacy adoption and Claude adapter internals; shared agent overlay)
├── config/pi_agent.go, pi_agent_test.go ..... D  ([pi.agents] resolution)
├── config/ .................................. M  (state ladder, .atomic default, candidates, harness.dir, [pi], config resolve, and ATOMIC_HARNESS retired, state.dir paths, state migration, per-harness agent tables; [steering] claude mode)
├── install/ ................................. M  (JSON exit status, named discard with recovery copy; automatic-adoption defaults; installed-harness detection)
├── hooks/ ................................... M  (maintenance, collection, formatting, Claude delivery; --harness and --cwd inputs; Codex PreToolUse card delivery and per-session sent-card state)
├── profile/ ................................. M  (authoritative profile delivery)
├── wiki/ .................................... M  (authoritative wikis.md registry; projection targets; Refresh wiring; capture section in AGENTS.md; card description and globs in Refresh; mode-shaped init pairs)
├── where/ ................................... M  (harness-neutral registry and state resolution; state_root and worktrees)
├── repoinit/ ................................ M  (repository root passed; worktree ignores in the state root and .claude/; steering conversion runs from cmd/atomic/cmd_steering.go)
├── codeintel/cli/ ........................... M  (root-relative ignore entry)
├── codeintel/realm/ ......................... M  (federation under the resolved realm state root)
├── signals/tree.go .......................... M  (skip prefixes from the resolved root)
├── serve/walk.go ............................ M  (resolved state root exempt from the dot-directory skip)
├── retro/ ................................... M  (per-harness session history readers)
├── bus/identity.go .......................... M  (Claude and Codex session identity)
├── doctor/checks_config.go .................. M  (no warning for retired harness.dir or [pi])
├── doctor/checks_repo_config.go ............. M  (.pi/atomic.toml comment dropped)
├── doctor/ .................................. M  (target, resource, journal, capability checks; active-harness leg; override, trust, and steering-mode warnings; mode-aware router-reference check and refs repair)
├── selfupdate/ .............................. M  (enrollment-aware reconvergence)
├── validate/ ................................ M  (projection and portability validation; finding keys; perf budget)
├── **/*_test.go and serve/frontend fixtures . M  (fixtures independent of the built-in state default)
├── **/*.go with added comments .............. M  (comments cut to the comment discipline)
└── cliusage/cliusage.go ..................... M  (new command inventory; hooks pre-tool-use)

atomic/cmd/atomic/
├── main.go .................................. M  (adapter registry composition; no ATOMIC_HARNESS gate)
├── rootflags.go ............................. M  (config resolve --repo exemption dropped)
├── cmd_install.go ........................... A  (top-level multi-harness install; installed-harness discovery when --harness is absent)
├── cmd_harness.go ........................... A  (enroll, adopt, status, repair, diff, uninstall, recover, rules)
├── cmd_state.go ............................. A  (state adoption, adopt --dir, migrate)
├── cmd_claude.go ............................ M  (Claude lifecycle cutover)
├── cmd_update.go ............................ M  (enrolled-target convergence; one-time Claude auto-adoption; repository state migration; child exit status)
├── cmd_doctor.go ............................ M  (target-aware reporting; Claude config dir)
├── cmd_hooks.go ............................. M  (adapter-local session delivery; --harness and --cwd; pre-tool-use verb; no OMP card republish)
├── cmd_config.go ............................ M  (config agents --harness routing)
├── cmd_retro.go ............................. M  (detected-harness history)
├── cmd_where.go ............................. M  (state_root and worktrees fields)
├── cmd_wiki.go .............................. M  (projection target; Refresh wiring)
├── cmd_cards.go ............................. M  (card publish writes the OMP card links)
├── cmd_repo.go .............................. M  (automatic state migration trigger; Claude card link)
├── cmd_harness_test.go ...................... A  (lifecycle and CLI contract)
└── cmd_state_test.go ........................ A  (state adoption and migration contract)

this repository's own state and wiring
├── .claude/{atomic.toml,project,rules/wiki} . D  (moved by atomic state migrate)
├── .atomic/ ................................. A  (this repository's state root)
├── .claude/rules/atomic ..................... A  (committed symlink to ../../.atomic/rules)
├── .gitignore and .claude/.gitignore ........ M  (stale state rules dropped)
├── .githooks/pre-commit ..................... M  (follow-ups path from the resolved root)
├── CLAUDE.md (repository root) .............. M  (follow-ups @-ref repointed)
└── scripts/code-eval/ ....................... M  (index path from the resolved root)

user-facing and maintained documentation
├── install.sh ............................... M  (multi-harness install handoff; bare atomic install as the next step)
├── README.md ................................ M  (supported harnesses and lifecycle; state-root path text)
├── docs/guides/install.md ................... M  (installed-harness discovery, update auto-adoption, enrollment, uninstall; CLAUDE_CONFIG_DIR; [steering] claude user default)
├── docs/guides/*.md ......................... M  (state-root path text)
├── docs/reference/commands.md ............... M  (new CLI surface)
├── docs/reference/concepts.md ............... M  (steering and adapter model; state ladder)
├── docs/reference/conventions.md ............ M  (AGENTS.md ownership; state root)
├── docs/reference/agents.md ................. M  (per-harness overrides; instruction-only restrictions)
├── docs/reference/*.md ...................... M  (state-root path text)
├── docs/reference/atomic-toml.md ............ M  ([steering] claude key)
├── docs/reference/{realm-wiki,concepts}.md .. M  (steering pairs follow the mode)
├── docs/spec/{wiki-deterministic-setup,wiki-buckets,signals-workflow}.md  M  (steering pairs follow the mode)
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
  Project steering — repository-root pair in the [steering] claude shape (loader default: AGENTS.md plus a thin Claude loader) written on init or migration
  Wiki steering — docs/wiki/ pair in the same shape; realm root and realm wiki pairs follow it too

atomic/internal/artifacts/
  Artifact — canonical identity, kind, rendered body, semantics, and source digest
  Projection — target-native bytes, delivery classification, and projection digest
  Catalog — deterministic corpus enumeration and dependency validation
  Renderer — partial expansion, native body production, and the per-adapter native-vocabulary paragraph slot
  LoaderPair — the loader-shaped scope steering projection: AGENTS.md plus the thin CLAUDE.md; the AGENTS.md-only and CLAUDE.md-inline shapes are written by harness/claude/steering.go and wiki/init.go
  ClaudeSteering — golden-tested Claude paragraph naming the Agent tool with subagent_type, the question tool, the worktree-entry tool, and the background-listener tool with timeout_ms and its stop tool
  Tests and golden testdata — canonical render, identity, dependency, and projection fixtures

atomic/internal/harness/
  Adapter — instance discovery, capabilities, projection, convergence, verification, removal
  Registry — concrete adapter lookup and target selection
  Target — harness, instance, native root, enrollment, and convergence state
  CapabilityMatrix — versioned native behavior evidence, including agent-delivery, session-start, and artifact-discovery rows
  Testdata — native capability, migration, worktree, non-git, and golden fixtures

atomic/internal/harness/detect.go
  Detect — pure function over an environment map returning harness, confidence, evidence names, and ambiguous candidates
  Policy — strong acts; weak only for context; ambiguous and none refuse writes into a harness root; --harness outranks detection
  Markers — strong, weak, and never-marker variables per harness

atomic/internal/harness/overlay.go
  Overlay — pure function over projected agent bytes and an override whose output is the digested and written bytes
  NativeKeys — per-harness model and effort key mapping

atomic/internal/harness/uninstall.go
  Resource identity — a consumer counts as shared only at the same id and the same physical applied path; aliased roots resolve to one

atomic/internal/harness/rule.go
  ClaudeRulePath — wiki cards at rules/atomic/wiki/<domain>.md under the repository .claude/; shipped rules at rules/atomic/<name>.md under the user root

atomic/internal/installstate/
  Lock — ~/.atomic/install/operation.lock advisory lifecycle serialization and writer identity
  Schema — schema version, writer version, minimum reader, and incompatible-reader refusal
  Ledger — ~/.atomic/install/ledger.json target, resource, consumer, generation, tier, and applied-value records, and the left-unowned (target, path) records
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
  Steering — direct global CLAUDE.md projection plus repository and realm pair management in the [steering] claude shape
  SteeringConversion — on a mode change, rewrites only Atomic-owned blocks and loaders of each pair; user text kept
  Settings — output-style and SessionStart narrow mutation; registration carries --harness claude
  Migration — global, project, and nested guidance adoption
  Cards — ledger-owned relative symlink <repo>/.claude/rules/atomic -> ../../<state-root>/rules; none when the state root is .claude; generated copy plus /rules/atomic/ ignore when the state root is outside the repository
  Agents — GlobalArtifacts and SelectionClaims take the home and overlay [claude.agents] overrides

atomic/internal/claudeinstall/
  Legacy lifecycle — verified Claude adoption and adapter-compatible install semantics; global steering rendered through artifacts.ClaudeSteering, so doctor's drift check matches adapter-written bytes
  Tests and testdata — existing-install, migration, conflict, and uninstall fixtures

atomic/internal/harness/omp/
  Adapter — OMP profile discovery, enrollment, convergence, and verification
  Package — the generated corpus store: commands, skills, actual rules, and the rendered extension module; agents live in the profile agent root
  Extension — the discovered agent-root module OMP loads, ledger-owned per profile
  Steering — AGENTS.md with its @ imports plus frontmatter-free output-style composition
  Kinds — ledger-owned <profile agent root>/commands/ and skills/ files, each delivered while its capability row is supported
  Profiles — profile-specific native roots, plugin state, and shared-package consumers
  Models — role defaults stay unsupported; per-agent overrides reach OMP through the overlaid agent files
  Agents — ledger-owned <profile agent root>/agents/<name>.md files, probe-gated delivery claim
  SessionContext — extension handler running atomic hooks session-start at session_start and appending to before_agent_start
  Ownership — extension module replaced only when the ledger records Atomic's last write; target record journaled
  Rules — canonical scope to CP0-selected native fields, exact-path matching, runtime delivery, freshness, and cleanup
  CardLinks — ledger-owned relative per-file symlinks .omp/rules/atomic-wiki-<domain>.md, stale-link pruning, copy-to-link migration, outside-repository copy fallback
  Session — CP0-selected event collection, matched-rule injection, formatting, and primary/resumed/child deduplication

atomic/internal/harness/codex/
  Adapter — CODEX_HOME discovery, enrollment, convergence, registration, trust observation, and verification
  Steering — native AGENTS.md materialization and override diagnostics
  Plugin — generated Atomic plugin package, manifest, rules index, and CP0-selected hook registration
  Hooks — proven session-baseline and pre-operation roles, bounded context return, exact-predicate deny, and uncovered-operation reporting
  Agents — canonical agent to TOML projection; ledger-owned $CODEX_HOME/agents/<name>.toml with overrides overlaid
  SessionHook — member-wise owned SessionStart entry in $CODEX_HOME/hooks.json and observed trust state
  CardHook — member-wise owned PreToolUse apply_patch entry running atomic hooks pre-tool-use; wiki-card capability partial (edits only) after startup-level evidence, else unsupported
  Skills — shared-skill installation and visibility reporting
  Commands — explicit invocation projection when supported

atomic/internal/runtimeproof/
  Scenario runner — isolated homes, repositories, processes, events, and retained observations
  Real-install scenarios — state migration, override effect, session context, and preferences block on real Claude, OMP, and Codex installs

atomic/internal/config/
  StateLocation — ladder from ATOMIC_STATE_DIR through the selection record, the populated candidate, and state.dir to .atomic
  Candidates — .atomic, configured state.dir, .claude; signatures without worktrees; ambiguity refusal
  Retired settings — no ATOMIC_HARNESS check, no harness.dir key or candidate
  StateDirPath — repository-relative multi-segment state.dir validation
  ResolutionCache — invalidation after an in-process state move
  StateMigration — destination without the legacy .claude candidate, legacy-root move including workflow-preferences.md, renames staged as git mv would, gitlink and unmerged refusal, rollback on partial failure, index removal when no daemon is live, home-directory refusal, ignore rules, reported hand-wired references
  PathHelpers — every state path built from a passed repository root; one worktree helper keyed by harness, an explicit preference path winning
  AgentOverrides — [claude|omp|codex.agents.<name>] tables into a shared AgentOverride, per-harness validation and warnings
  MutablePaths — ~/.atomic/config.toml, profile.md, wikis.md, install state, backups, and package roots
  Retired tables — [harness] and [pi] load without a warning and drop on the next write; no config resolve verb
  ConfigAgents — atomic config agents --harness forms, single-enrolled default, and convergence through the lifecycle
  ConfigList — active agent overrides grouped by harness
  SteeringMode — [steering] claude (loader, agents-md, claude-md), repository or realm root atomic.toml over user config.toml, loud error on an unknown value

atomic/internal/wiki/
  WikiRegistry — authoritative byte-preserving ~/.atomic/wikis.md operations
  Initialization — repository `docs/wiki/` and realm `<realm>/wiki/` steering pairs in the [steering] claude shape, scope markers, Git preconditions, and capture surface
  Refresh — repository pages and wiki-pointer RuleRecord production, each card with description and globs frontmatter; realm pages without scoped rule production
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
  PreToolUse — apply_patch header parsing, card glob matching, additionalContext output, once-per-session sent-card state keyed by session_id, exit 0 on any error

atomic/internal/doctor/
  Target checks — native registration, artifacts, guidance, roles, and disablement; Claude root from claude.DefaultConfigDir
  Active-harness leg — checks for the detected harness
  Override warnings — override for an unenrolled harness or an undelivered agent
  Codex hook trust — observed trust state of the owned SessionStart and PreToolUse members
  Steering-mode warnings — agents-md beside a CLAUDE.md, .claude/CLAUDE.md, or CLAUDE.local.md, or on Claude Code older than 2.1.277; claude-md with OMP or Codex enrolled
  Shared checks — ledger, journals, ownership, mutable authority, and visibility
  Capability checks — promised behavior, gaps, shadowing, and stale evidence
  Repairs — reuse adapter convergence instead of a separate repair path

atomic/internal/bundlespec/bundlespec.go
  SteeringSource — canonical global AGENTS.md source, direct Claude global projection, and repository/realm loader inclusion
  ScopeSteering — repository and realm pair description parameterized by the [steering] claude mode

atomic/internal/bundlemirror/mirror.go
  Enumerate — maps the canonical corpus to Claude-native targets

atomic/internal/profile/
  Profile delivery — authoritative profile state and native generation tracking

atomic/internal/where/
  Resolve — harness-neutral wiki registry and repository-state orientation
  JSON fields — state_root, and worktrees as the recommendation for the passed or detected harness, repository-relative inside the repository, absolute outside, empty only when off

atomic/internal/repoinit/
  Init — scaffold from the passed repository root; state-root ignore rules including /worktrees/; /worktrees/ in .claude/.gitignore only when .claude/ exists or a Claude target is enrolled
  SteeringMode — root and docs/wiki/ pairs written and converted in the effective [steering] claude shape

atomic/internal/codeintel/cli/
  EnsureGitignore — index rule computed from the resolved state root; none for a root outside the repository

atomic/internal/codeintel/realm/
  Federation paths — code.toml and member databases in the resolved realm state root, or <realm>/.atomic/ when that root is .claude; /*.db ignored; legacy <realm>/.atomic/ read until migration

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
  Installed-harness discovery — Claude root, the OMP_PROFILE (else default) profile agent root, and a set CODEX_HOME that exists, from adapter discovery
  Automatic adoption — the Adopt path with older-version artifacts replaced and the snapshot acknowledged; user-content blockers returned by name

atomic/internal/selfupdate/
  Artifact convergence — enrolled-target refresh after binary selection

atomic/internal/validate/
  Projection rules — corpus portability and native projection diagnostics

atomic/internal/cliusage/cliusage.go
  Command inventory — stable rows for install, harness, and state verbs, and atomic hooks pre-tool-use

atomic/cmd/atomic/main.go
  command registry — top-level install, harness, and state routing

atomic/cmd/atomic/cmd_install.go
  install — explicit multi-harness target selection and convergence; with no --harness and no --all, the installed-harness discovery set (Claude fallback) and the detected: line
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

atomic/cmd/atomic/cmd_repo.go, cmd_update.go
  migration trigger — one automatic migration with a notice before the verb writes state; no other verb triggers it

atomic/cmd/atomic/cmd_retro.go
  retro extract — detected-harness history source

atomic/cmd/atomic/cmd_where.go
  where --json — state_root and worktrees fields

atomic/cmd/atomic/cmd_claude.go
  Claude cutover — route the existing lifecycle through the Claude adapter

atomic/cmd/atomic/cmd_update.go
  update convergence — re-exec and reconverge enrolled targets; the child's exit status propagates
  autoAdoptClaude — one-time adoption of an unenrolled legacy Claude root with replace and snapshot-acknowledge defaults; user-content blockers exit 1

atomic/cmd/atomic/cmd_doctor.go
  target reporting — stable existing categories plus appended harness checks

atomic/cmd/atomic/cmd_hooks.go
  session delivery — adapter-local maintenance and injection routing
  session-start flags — --harness, --cwd, optional stdin
  pre-tool-use — Codex edit-time card delivery entry point
  Removal — OMP session-start card republish

context command, agent, skill, partial, and rule families
  Portable outcomes — harness-neutral workflow intent and dependencies
  Native metadata — adapter-consumable semantics without runtime wire syntax
  Agent contract — no model, effort, or tools; restrictions stated as instructions
  State-root text — .atomic default and resolved <state-root> wording
  Card staging — signals-gate and loop-finalize stage .omp/rules/atomic-wiki-*.md symlinks with the cards, never a regular file
  Steering-mode text — wiki pipeline Step 8, setup-wiki, refresh-wiki, and the inferrer name the file the mode makes Claude load

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
  Card delivery — the <repo>/.claude/rules/atomic symlink and the <repo>/.omp/rules/atomic-wiki-<domain>.md per-card symlinks
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
  Operator flow — installed-harness discovery, update auto-adoption, enrollment, repair, explicit adoption, and uninstall

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

1. User runs `atomic install` or `atomic harness enroll`. With `--harness` or `--all` the selectors name the targets. A bare `atomic install` selects every harness installed-harness discovery finds (Claude when its root exists, OMP when the agent root of the `OMP_PROFILE` profile or else the default profile exists, Codex when a set `CODEX_HOME` exists), falls back to Claude when none is found, and prints `detected: <harness> (<root>), ...`.
2. CLI discovers native instances and shared visibility without enrolling any newly discovered target. A selected Claude root carrying legacy evidence goes through the automatic adoption (older-version artifacts replaced unless `--leave-unowned` is given, snapshot acknowledged) and stops on a user-content blocker.
3. Adapter reports capability evidence, native registration, disabled or trust state, resources visible to the selected instance, and known rule-coverage gaps.
4. Renderer produces one canonical artifact and rule generation from the selected binary.
5. Install engine observes the complete plan, writes `~/.atomic/install/journals/<operation-id>.json`, stores transaction backups, stages every projection, and validates staged bytes.
6. Claude writes its direct global `CLAUDE.md` block and native artifacts with stored agent overrides overlaid; OMP publishes its shared package as a corpus store, the profile's steering block, the profile's discovered extension module, the profile's overlaid agent files, and ledger-owned `commands/` and `skills/` files under the profile agent root; Codex publishes its plugin, its `$CODEX_HOME/AGENTS.md` steering block, its overlaid agent files, and its owned `hooks.json` member, then registers the plugin with `codex plugin marketplace add` and `codex plugin add` unless Codex already holds the current copy, without fabricating hook trust; a registration it cannot run is reported `WARN unsupported` with its fix and never as applied.
7. Adapter commits and verifies each native unit, including effective steering, discovery, rules, hooks, and deliberate disablement.
8. `~/.atomic/install/ledger.json` records physical resources, visibility, consumers, desired generations, enforcement tiers, and target state only after verification.
9. CLI reports applied, stale, disabled, untrusted, unsupported, or conflicted state per target.

**Flow: OMP steering and rule delivery**

1. Renderer reads canonical `context/AGENTS.md`, authoritative profile and wiki data, and shipped plus generated rule records.
2. Renderer parses the Atomic output-style document and discards its YAML frontmatter.
3. OMP adapter emits Atomic steering with its `@` imports, one blank line, and output-style body into the target profile's owned `AGENTS.md` block.
4. Rule projection translates canonical `paths:` to the native scope fields proven by the corresponding CP0 capability row; unproven metadata never enters the OMP package.
5. If static scope is advisory, the CP0-selected pre-operation event extracts exact operation paths, runs the canonical matcher, and supplies every matched body before the supported operation.
6. A proven deny result blocks only a machine-checkable predicate; semantic bodies remain instruction guidance.
7. A missing capability row sets that surface to `unsupported` without failing installation; adapter verification records the gap.
8. Adapter preserves profile guidance outside Atomic-owned blocks and verifies steering, rule generation, runtime delivery, and consumer digests.

**Flow: Codex plugin and rule delivery**

1. Codex adapter writes the Atomic plugin, matcher, rule index, the `$CODEX_HOME/AGENTS.md` managed block (steering without its whole-line `@` imports, output style always on, no native-tools paragraph), and only the registration and event configuration the capability record proves. Plan and convergence decide registration by one check — Codex holds the marketplace, the plugin entry, and a cache matching the selected generation — and the plan names the `$CODEX_HOME/config.toml` edit that `codex plugin marketplace add` and `codex plugin add` make whenever registration will run, so consent covers it. A plugin Atomic registered whose plugin entry and cache the user then removed (`codex plugin remove` keeps the marketplace) stays removed and reads `WARN unsupported` with the fix `atomic install --harness codex --replace`. Doctor's registration fix follows the observed state: a disabled plugin names `codex plugin add atomic@atomic-claude`, which codex-cli 0.147.0 was observed to re-enable with, a user removal names `--replace`, an unreadable `config.toml` names the file, and a missing codex binary names `CODEX_BIN`. A user `AGENTS.md` with no Atomic block gets the block appended. Target uninstall strips the block and, after removing or discarding the plugin cache Atomic recorded, runs `codex plugin remove` and `codex plugin marketplace remove` only for the entries `config.toml` records, so a registration the user already removed needs no codex binary. When an unregister cannot run, the uninstall still ends the enrollment and prints a notice naming the manual commands, so `atomic update` never re-converges a home the user uninstalled.
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
4. Exact selected-generation bytes, uniquely parseable Atomic blocks, and bytes the ledger itself recorded applying become verified ownership evidence inside the approved plan. Bytes matching neither the selected generation nor the ledger's recorded digest for that (target, resource) — the genuine older-version or foreign-writer case — are batched for replace-or-leave-unowned decisions, and the refusal names `--replace` and `--leave-unowned`. A leave-unowned decision is recorded in the ledger and honored by every later operation that carries no decision. Missing listed files, malformed snapshots, pending Claude merge files, relative-reference changes, and orphan v2 resources require explicit resolution and remain preserved.
5. Migration preserves the write-once legacy pre-install snapshot and current unowned prose. If historical restoration evidence is absent or corrupt, the user must acknowledge that limit before adoption.
6. After explicit consent, migration writes and fsyncs the journal, current-byte backups, and parent directories. It then persists any state-root selection before switching resolution.
7. Migration renders canonical steering directly into global Claude `CLAUDE.md`, writes repository and realm steering pairs in the shape the `[steering] claude` mode selects, imports mutable data once, and applies target-native resources through normal commit units.
8. Effective guidance, mutable authority, native bytes, and capability state are verified before ledger ownership commits. Failure rolls forward or restores only digest-unchanged applied bytes; later edits conflict.
9. After adoption, native profile/wiki copies are one-way projections, repeated same-generation convergence is a no-op, and old Claude-only binaries are unsupported writers whose later changes surface as drift.

**Flow: repository-state selection and adoption**

1. A verb asks the resolver for the state root of a repository root it passes explicitly; no caller passes an empty root.
2. Process-only `ATOMIC_STATE_DIR` answers first. An invalid value refuses in the strict and the cached resolver alike.
3. `~/.atomic/<project-key>/state-location.json` answers next; every worktree of the clone shares it.
4. The resolver inspects the candidates `.atomic`, the configured `state.dir` path, and `.claude` for a signature (`atomic.toml`, `.atomic-index`, `.scratchpad`, `project`, `rules/wiki`) without creating any. A populated destination (`state.dir`, else `.atomic`) answers, and each other populated candidate draws one stderr warning naming its leftover entries. With the destination unpopulated, exactly one populated other candidate answers in place; two refuse with the `atomic state adopt` instruction.
5. With no populated candidate, the user `state.dir` answers, else `.atomic`. An empty repository gains no state directory until a stateful verb writes.
6. `atomic state adopt [--dir <path>]` writes and fsyncs the selection record before resolution switches. A failed adoption restores the prior selection only when no later state write used the new root; otherwise it records conflict.
7. Claude, OMP, Codex, shell commands, hooks, and worktrees resolve the same root. Harness detection takes no part. Realm roots follow the same steps.

**Flow: repository-state migration**

1. `atomic state migrate` starts the migration explicitly. `atomic repo init`, and `atomic update` run inside a repository, start it once, printing a notice, when the legacy `.claude` root holds a signature other than `.atomic-index` and the destination holds none. A root equal to the user's home directory is refused. Every other verb reads and writes the populated legacy root in place.
2. The destination is the selection record, else `state.dir`, else `.atomic`; `ATOMIC_STATE_DIR` is skipped because it is process-only. When the record or `state.dir` names the legacy root, nothing moves.
3. When the legacy root and the destination both hold signatures, and the legacy root holds a signature other than `.atomic-index`, the migration moves nothing and names the leftover `.claude` entries: `atomic state migrate` exits 1, and `atomic repo init` and `atomic update` warn and continue against the destination. When the legacy root holds no signature other than `.atomic-index`, `atomic state migrate` removes that index once no daemon is live there and exits 0; while one is live it reports the path and exits 0.
4. It moves `atomic.toml`, `workflow-preferences.md`, `project/`, `rules/wiki/`, and `.scratchpad/` into the destination: tracked files with each rename staged as `git mv` would, ignored files and every file under a non-Git realm root with a rename, copy-and-remove across devices. `worktrees/` and harness-native entries stay. Realm federation files stay in place under an `.atomic` destination and move with the rest to a custom one.
5. It checks whether an MCP daemon is live at the legacy index: a daemon answering on its socket, or a starter holding its spawn lock. With none live it takes the spawn lock, so a starter waits, and removes the legacy `.atomic-index/` directory under that hold, releasing it afterwards, with its database, write-ahead files, socket, and lock; with one live it leaves the directory and reports it. `atomic code index` builds a fresh index at the destination.
6. It writes the destination's managed `.gitignore` rules (`/.scratchpad/`, `/.atomic-index/`, `/worktrees/`, `/project/doc-surfaces.md`) and leaves the legacy `.gitignore` in place.
7. It invalidates the process's resolution cache, creates the `<repo>/.claude/rules/atomic -> ../../<state-root>/rules` link, and, when OMP is enrolled, relinks each `<repo>/.omp/rules/atomic-wiki-<domain>.md` to the moved card, adding every link to the Git index.
8. It prints the moved entries, the staged renames, any index directory it left, and every hand-wired reference to a legacy path it found, without rewriting them. It writes no `state-location.json` and commits nothing.
9. The triggering verb continues against the destination.

**Flow: harness detection**

1. A shell verb passes its environment map to the detection function, which reads nothing else.
2. Detection collects the kinds with a strong marker. One kind returns `strong`; two or more return `ambiguous` with those kinds as candidates and no harness.
3. With no strong marker, a weak marker returns `weak`; no marker returns `none`. Evidence lists variable names, never values.
4. A verb given `--harness` uses it and does not consult detection. Otherwise `strong` acts. `weak` acts only for a verb that reads or adds context; a verb that writes into a harness root takes no action on `weak` and, like on `ambiguous` or `none`, refuses and asks for `--harness`. A verb that only adds context adds nothing on `ambiguous` or `none`. `atomic config agents` alone falls back to the single enrolled harness when exactly one is enrolled. `atomic wiki` and `atomic install` never consult detection; a bare `atomic install` uses installed-harness discovery.
5. Hook entrypoints never call detection: each registration passes `--harness`.

**Flow: agent override convergence**

1. The user runs `atomic config agents [--harness claude|omp|codex|all]`. Without the flag the verb takes a `strong` detection, else the single enrolled harness when exactly one is enrolled, else asks, and refuses non-interactively while listing the enrolled kinds.
2. The verb shows that harness's form pre-filled from its `[<harness>.agents.<name>]` tables, validates each entry with that harness's rules, and treats blank model and effort as removal.
3. It writes `~/.atomic/config.toml` through `WritePersist`, which keeps every modeled table (each `[<harness>.agents.<name>]` entry, `[bus.remotes]`, and the other known sections) and drops unknown keys: retired `harness.dir` and `[pi]` keys go without a warning, and every other unknown key was already warned about by `Load`.
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
2. It reads the effective `[steering] claude` mode and creates the absent files or owned blocks of each pair in that shape (in `loader` mode, shared `AGENTS.md` files and adjacent thin Claude loaders); it never overwrites a file wholesale and rejects malformed or conflicting loaders.
3. Refresh writes `docs/wiki/scan.md`, `index.md`, domain pages, steering changes, and canonical `<state-root>/rules/wiki/**` cards into the invoked worktree.
4. Canonical wiki output commits before projection; project-keyed state and operation records remain shared across worktrees.
5. Refresh auto-converges steering and rules for already-enrolled targets only: Claude creates the `.claude/rules/atomic` link when it is missing, OMP writes the `<repo>/.omp/rules/atomic-wiki-<domain>.md` links through `(*omp.Adapter).PublishProjectCards` and prunes links whose card is gone, and Codex regenerates its matcher index.
6. Each projection records applied, stale, disabled, unsupported, or conflicted state without rolling canonical wiki output back.
7. The ship flow stages `<state-root>/rules/wiki/`, `.claude/rules/atomic`, and each `.omp/rules/atomic-wiki-*.md` entry that is a symlink together; an outside-repository fallback copy stays untracked.

**Flow: Claude steering mode change**

1. The user sets `[steering] claude` in the repository's or realm root's `<state-root>/atomic.toml`, or in `~/.atomic/config.toml`; the repository or realm value wins, and `loader` applies when neither sets it. An unknown value fails the config load with a named error.
2. `atomic repo init` or `atomic update` reads the effective mode and observes the steering pairs of the repository or realm at the current working directory only: repository root and `docs/wiki/`, or realm root and realm `wiki/`.
3. For each pair whose shape differs from the mode, it plans the conversion from the Atomic-owned blocks and loaders alone: `loader` writes `AGENTS.md` and the thin `CLAUDE.md`; `agents-md` removes the owned loader and keeps `AGENTS.md`; `claude-md` moves the managed block inline into `CLAUDE.md` and deletes `AGENTS.md` when Atomic owns the whole file (a ledger row, and nothing outside the owned block), else removes the owned block and keeps the file. User text in either file stays.
4. It journals and applies the conversion, records ownership, and leaves the global `~/.claude/CLAUDE.md` unchanged.
5. Doctor warns for `agents-md` beside a `CLAUDE.md`, `.claude/CLAUDE.md`, or `CLAUDE.local.md`, or when `claude --version` reports a version older than 2.1.277 (a missing binary or unparseable version is a PASS whose detail carries an `info:` note), and for `claude-md` while OMP or Codex is enrolled.

**Flow: OMP card links on update**

1. `atomic update`, run inside a repository with OMP enrolled, observes `<repo>/.omp/rules/atomic-wiki-*.md`.
2. With the state root inside the repository, a generated flat copy whose ledger row matches its bytes becomes the relative link `../../<state-root>/rules/wiki/<domain>.md`; a foreign or edited file is a named conflict, reported before any mutation and preserved.
3. With the state root outside the repository, the generated copy stays and is refreshed when stale.
4. No OMP session-start hook writes a card file.

**Flow: Codex edit-time card delivery**

1. Codex calls the trusted `PreToolUse` member for an `apply_patch` call; the member runs `atomic hooks pre-tool-use` with the hook payload on stdin.
2. The verb reads the `*** Add File: <path>`, `*** Update File: <path>`, and `*** Move to: <path>` headers from `tool_input.command` and resolves the repository root from the payload `cwd`.
3. It matches each repository-relative path against each card's `paths:` globs and drops the cards already sent for this `session_id`, read from the sent-card state file under `~/.atomic/`.
4. It prints the remaining card bodies as `hookSpecificOutput.additionalContext`, records them as sent, and drops the entries of sessions not seen in the last 7 days.
5. Any error exits 0 with no output, so the tool call proceeds.

**Flow: realm wiki initialization and refresh**

1. Initialization accepts a non-Git realm root but creates or adopts `<realm>/wiki/` as its own Git repository.
2. It writes the `<realm>/` pair with capture/member guidance, then the `<realm>/wiki/` pair with wiki guidance, each in the shape the `[steering] claude` mode selects (in `loader` mode, `AGENTS.md` and an adjacent `CLAUDE.md`).
3. Registry mutation writes and verifies authoritative `~/.atomic/wikis.md` before derived target views.
4. Refresh writes canonical realm scan, member, bucket, domain, and both steering scopes without producing scoped pointer rules, then commits the wiki repository automatically.
5. Refresh auto-converges steering for already-enrolled targets; failed projections become stale or conflicted and do not roll back registry or wiki authority.
6. Ship hooks mark realms dirty without synthesizing; session-start hooks report staleness without mutating.

**Flow: update and recovery**

1. Normal update refreshes the binary when needed, re-executes the new binary, and converges every enrolled target. Run inside a repository, it also runs the state migration once, creates the Claude card link when missing, converts steering pairs whose shape differs from the `[steering] claude` mode, and turns generated OMP card copies into links. When the Claude root is not enrolled and the legacy `[install]` record in `config.toml` is not marked `adopted`, it first prints `adopting existing Claude install at <root>` and adopts that root with older-version artifacts replaced (each with a recovery copy under `~/.atomic/backups/<operation>/`) and the snapshot acknowledged; a user-content blocker exits 1 naming the file and asking the user to resolve it and re-run `atomic update`. `--skip-claude-update` skips both.
2. An already-current binary skips download and still converges enrolled targets.
3. `--check` reads version and target state without writing native files, settings, journals, or ledger data.
4. If a process stops between native mutation and ledger commit, recovery compares journal observations, desired state, retained ledger rows, retained state-location records, transaction backups, and current native bytes.
5. Recovery records applied work, completes safe pending work, or reports conflict without overwriting an unrelated user change. An interrupted generated-tree publication resolves by finishing the rename when the staged tree still matches, else by restoring the digest-verified backup; a destination the ledger already records as applied is a deletion the user made after the publication committed, never the crash window, so recovery does not recreate it. A block resource whose file still carries no Atomic tags proves the write never landed. `atomic harness recover` exposes the decision explicitly and `--rollback` restores the digest-verified pre-mutation bytes. Recovery then permits cleanup of the retained operational records it consumed.

**Flow: target and full uninstall**

1. User selects one enrolled target instance or requests full Atomic removal. A key that does not parse into an enrolled target still names rows, and the row-only removal clears them by the raw key.
2. Adapter observes every owned target resource, shared visibility, and consumer relation.
3. Changed target resources are reported skipped with the reason and the exact next step and keep their claim; unchanged owned resources and consumer links are removed; a resource the removal cannot clear keeps its claim and is reported skipped. `--discard-changed` approves discarding each skipped resource by name — removing a file or tree, stripping a managed block, or releasing a settings file's claim while the user's bytes stay — and every directory a removed file resource emptied below the native root is removed with it.
4. With `--discard-changed`, the plan names every resource it will discard, by id and path, as a `notice` line: on stderr before anything is applied, and in the dry-run plan's `notices` in text and `--json`. Without `--yes`, each discard is also confirmed by name. Under `--yes`, the printed plan is the approved plan: a resource the apply step would discard that the printed plan did not name refuses the whole removal, non-zero, with nothing discarded; a full uninstall re-plans every target before the first removal, so drift in any target refuses before any target is removed. Each discarded resource's bytes are copied under `~/.atomic/backups/discard-<stamp>/<target>/` first, a tree by moving it there, and the removal names that directory; a discard that cannot keep its copy stops with the user's bytes in place.
5. Shared resources remain while another enrolled consumer exists, regardless of visibility to unenrolled instances.
6. Target steering is removed independently of plugin disablement or unlinking.
7. Target uninstall never deletes a tracked file in a user repository: the `.claude/rules/atomic` link is reported and left in place.
8. After the final target, full uninstall preserves `~/.atomic/config.toml`, `~/.atomic/profile.md`, `~/.atomic/wikis.md`, and backups.
9. Completed ledger, journal, transaction, and adoption state is removed. Unresolved journals, referenced transaction backups, referenced ledger rows, and referenced state-location records remain until recovery completes.


## Checkpoints


**Execution order.** Rows CP0 through CP8B are built. Every other row ships in this build, serially, in this order:

1. P0: CP17A, CP17E, CP17B, CP17C, CP17D
2. P1: CP10C1 through CP10C11, CP9A
3. P2: CP10A, CP10B
4. P3: WP1, WP2, WP3, WP4
5. P4: CP11, CP11B, CP9B1, CP9B2
6. P5: WP5a, WP5b, WP6a
7. P6: WP7, WP8a, WP8b, WP9a, WP9b, WP10
8. P7: CP12A, CP12B, CP12C, CP13A, CP13B
9. P8: WP6b, WP6c, WP6d, CP10D1 through CP10D8, CP15A1 through CP15A5, CP15B1, CP15B2
10. P9: CP14A, CP14B1, CP14B2, CP14C, CP14D1 through CP14D17
11. P10: WP11a, WP11b, CP16
12. P11: CP18A1, CP18A2, CP18B, CP18C

`WP` rows belong to `docs/spec/workflow-preferences.md`. Each row's Verifies names its dependencies, and every named dependency appears earlier in this order. Rows share files in these places: `docs/guides/install.md` (CP17D, WP6b, CP10D4, CP15A1); `docs/reference/agents.md` (CP17D, CP12C, CP14C); `docs/spec/{agents-effort-config,cli-trailing-flags,configurable-state-paths,atomic-state-and-config}.md` (CP17D, WP6c or WP6d, CP12C, CP10D5); `install.sh` (CP17D, WP6b, CP15A1); `docs/spec/{atomic-state-and-config,configurable-state-paths,repo-init}.md` (WP6c or WP6d, CP10D5); `docs/spec/atomic-binary.md` (WP6c, CP10D6); `docs/spec/{serve-plans-page,documentation-as-maintenance}.md` (WP6c or WP6d, CP10D7); `docs/spec/{atomic-where,typed-followups}.md` (WP6c or WP6d, CP9B2, CP10D8); `atomic/internal/wiki/registry.go` (CP15A2, CP15B2). This list is not exhaustive; serial order makes any other shared file safe. Every row from P0 through P8, and every P10 and P11 row, leaves the files it adds clean under `atomic code comments`.

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| CP0 | Record versioned Claude, OMP, and Codex capability evidence | `docs/research/harness-capability-matrix.md`, scratchpad observations, spec amendments | atomic-implementer (mode: feature) | ~3 | Runtime probes map tested native names and payloads to static scope, session-baseline, pre-operation target, context-return, and deny capability rows; also record paths, registration, trust, ordering, size, lifetime, child, dedupe, reload, concurrency, failure, disablement, cleanup, visibility, and overrides. Missing rows mean `unsupported`; findings amend affected criteria/checkpoints and the Change log |
| CP1A | Add managed-resource ledger and recovery primitives | `installstate/`, `managedfile/`, tests and fixtures | atomic-implementer (mode: feature) | ~10 | Exact ledger/journal/transaction paths, file/block/generated-tree commits, interruption scenarios, full uninstall with unresolved work, referenced ledger/selection retention, post-recovery cleanup, and sidecar digest behavior pass without overwriting concurrent changes |
| CP1B | Add adapter registry, target enrollment, and ownership evidence | `harness/`, `harness/claude/`, `claudeinstall/`, `config/`, CLI tests | atomic-implementer (mode: feature) | ~12 | Discovery does not enroll; selected-generation bytes and parseable blocks can establish ownership only inside an approved plan; ambiguous targets require `harness adopt claude`; visibility and consumers differ; target removal cannot delete shared infrastructure |
| CP1C | Make repository state and mutable context harness-neutral | `config/`, `wiki/`, `where/`, `hooks/`, `profile/`, tests | atomic-implementer (mode: feature) | ~12 | Exact `~/.atomic` paths, safe-path validation, empty-repository laziness, persist-before-switch ordering, one-root resolution, one-time mutable import, post-adoption conflict behavior, and separated session operations pass |
| CP1D | Classify and adopt legacy, partial, and mixed installations | `installstate/`, `claudeinstall/`, `config/`, `hooks/`, migration fixtures | atomic-implementer (mode: feature) | ~12 | All seven states, every legacy/v2 evidence input, selected-generation/block ownership, a prior-version complete install with both replace and leave-unowned outcomes, pending proposal and corrupt-snapshot conflicts, operation lock, oldest-first recovery, filesystem-identical dry-run where applied-but-unrecorded and discardable-staging cases produce advisory plans while later-edit recovery conflict produces `blocked_on_recovery`, snapshot preservation, idempotent no-op, state-root adoption/rollback, schema refusal, and old-binary drift pass on real legacy-shaped fixtures |
| CP2A | Cut canonical steering to AGENTS.md and add offline renderers | global/root/wiki steering, `artifacts/`, `bundlespec/`, `bundlemirror/`, tests | atomic-implementer (mode: feature) | ~12 | Claude's global `CLAUDE.md` contains the direct rendered contract and has no `AGENTS.md` reference; repository and realm loaders resolve adjacent steering; OMP golden is Atomic steering with its `@` imports plus frontmatter-free style; rendered bytes and digests are deterministic |
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
| CP17A | Remove `ATOMIC_HARNESS`, the `.pi` candidate, and the `harness.dir` setting | `config/statelocation.go`, `config/config.go`, `config/harness.go`, `config/render.go`, `doctor/checks_config.go`, `doctor/checks_repo_config.go`, `cmd/atomic/main.go`, `config`, `doctor`, and `cmd/atomic` tests | atomic-implementer (mode: feature) | ~10 | Phase P0; depends only on built rows. Closes P-11. `ObsoleteHarnessEnvVar`, `ErrObsoleteHarnessEnv`, `CheckObsoleteHarnessEnv`, `refuseObsoleteHarnessEnv`, and `obsoleteHarnessRefusal` are deleted; with `ATOMIC_HARNESS` set to `claude`, `omp`, or `bogus`, `atomic where --json`, `atomic state adopt --dry-run`, `atomic hooks session-start`, and `atomic install --dry-run` print the same output and exit with the same code as with it unset. `candidateDirNames` returns the configured `state.dir` segment and the built-in default only: a repository whose only state is under `.pi` has no populated candidate and resolves the `state.dir` or built-in default. The `harness.dir` key is gone from the config schema, `Get`, `Set`, and the rendered config, and `resolveHarnessDirFromHome` and the state resolver read only `state.dir`: `Default`, `Render`, and `WritePersist` no longer emit `[harness] dir`; a stored `harness.dir` of any value changes no resolution, loads with no unknown-key warning, and is dropped by the next config write, so doctor's config check reports PASS on a config that still carries it instead of a WARN whose `atomic config unset` hint would refuse; `atomic config set harness.dir x` refuses as an unknown key. No doctor check or comment names either setting; the `.pi/atomic.toml` comment in `checks_repo_config.go` is gone. The built-in default stays `.claude` until CP10A; a grep of non-test Go source finds no `ATOMIC_HARNESS`, no `harness.dir`, and no `.pi` state candidate |
| CP17E | Remove `[pi.agents]` and `atomic config resolve` | `config/pi_agent.go` (D), `config/pi_agent_test.go` (D), `config/cli.go`, `config/config.go`, `cmd/atomic/rootflags.go`, `cliusage/cliusage.go`, `config`, `cliusage`, and `cmd/atomic` tests | atomic-implementer (mode: feature) | ~8 | Phase P0; depends on CP17A, which retires `[harness]` the same way. `ResolvePiAgents` and its envelope types are deleted, and nothing reads `<repo>/.pi/atomic.toml`. `atomic config resolve` exits 2 with `unknown verb "resolve"`, like any unknown config verb. The `Config.Pi` field is gone: a stored `[pi]` table loads with no unknown-key warning and is dropped by the next config write. `repoFlagExemptions` drops `{"config", "resolve"}`, and the `cliusage` row for `config resolve` is gone with its test expectation |
| CP17B | `atomic update` adopts a legacy Claude install automatically | `cmd/atomic/cmd_update.go`, `install/install.go`, `cmd/atomic/cmd_update_converge_test.go`, `install/install_test.go`, legacy fixtures | atomic-implementer (mode: feature) | ~6 | Phase P0; depends only on built rows. `legacyUnenrolledError` and `errLegacyUnenrolled` are replaced: when `claude.DefaultConfigDir(home)` is not enrolled and the legacy `[install]` record is present without `adopted = true`, `convergeEnrolledTargets` prints `adopting existing Claude install at <root>`, runs `Steps.Adopt` with the replace batch decision and `AdoptRequest.AcknowledgeSnapshot` set, then converges. A prior-version complete legacy fixture with older-version unowned artifacts and a corrupt snapshot updates to exit 0 with the target enrolled, the artifacts replaced, and `[install] adopted = true` written; a second update prints no adoption line; after `atomic harness uninstall claude:<root> --yes` a further update prints no adoption line and leaves the root unenrolled, and a later `atomic claude install` makes the next update adopt again, after that update first stops on the proposed `CLAUDE.md` the reinstall leaves and the user merges it with `atomic prompt claude-merge` or deletes it; a fresh v2 `install --harness claude` on an empty home writes no `adopted` marker; an enrolled root is never re-adopted, even after a legacy writer clears the home-wide marker; a resource left unowned by an earlier decision stays left; an enrolled Claude root and an OMP-only home, each with an unreadable ledger, exit 0 with repair advice and no adoption text; a home with an enrolled OMP target and an unadopted legacy Claude root also adopts the Claude root; each replaced resource has a recovery copy under `~/.atomic/backups/<operation>/` whose bytes equal the pre-adoption file, and every adoption that replaces a file, including `atomic harness adopt claude --replace`, keeps that copy. For each user-content blocker (pending proposed `CLAUDE.md`, pending `CLAUDE.md.atomic-merged`, unjournaled v2 remnant, legacy/v2 overlap, ambiguous managed block, a journal recovery cannot complete) update exits 1 with a message naming the file or journal and telling the user to resolve it and re-run `atomic update`, the journal case also naming `atomic harness recover`, and the adoption changes no native file and writes no ledger row. `--skip-claude-update` leaves a legacy home untouched; a home with neither legacy evidence nor enrollment exits 0 as a no-op; `atomic harness adopt claude` with no flags still refuses an older-version batch and a corrupt snapshot |
| CP17C | `atomic install` uses installed-harness discovery when `--harness` is absent | `cmd/atomic/cmd_install.go`, `install/install.go` installed-harness discovery, `cmd/atomic` install tests, `install/install_test.go` | atomic-implementer (mode: feature) | ~5 | Phase P0; depends on CP17B, which supplies the automatic-adoption path. With neither `--harness` nor `--all`, `atomic install` no longer exits 2: fixture homes holding only a Claude root, only an OMP profile agent root, only a set and existing `CODEX_HOME`, all three, and none install into Claude, OMP, Codex, all three, and Claude respectively, and each first prints `detected: <harness> (<root>), ...` (the none case names Claude and its root). `CLAUDE_CONFIG_DIR` naming an existing directory selects that root; with `OMP_PROFILE` set, discovery checks that profile's agent root, and otherwise the default profile's; an unset `CODEX_HOME` never finds Codex even when `~/.codex` exists; a set `CODEX_HOME` naming a missing directory is not found. `--harness omp` on the three-harness home installs only OMP; `--all` behaves as it did. A legacy Claude root reached by discovery or by `--harness claude` adopts with the automatic-adoption defaults and exits 0; bare `atomic install --leave-unowned` on a legacy root leaves the older-version artifacts unowned instead of replacing them; discovery and `--harness claude` adoptions each leave a recovery copy under `~/.atomic/backups/<operation>/` for every replaced resource; a pending proposed `CLAUDE.md` exits 1 naming it. `--dry-run` on the found set writes nothing. The usage string shows `--harness` as optional |
| CP17D | Document the P0 install, update, and config behavior | `docs/guides/install.md`, `docs/guides/getting-started.md`, `docs/reference/{concepts,conventions,agents}.md`, `README.md`, `context/commands/atomic-help.md`, `install.sh`, `docs/spec/{agents-effort-config,agent-model-overrides,cli-trailing-flags,configurable-state-paths,atomic-state-and-config}.md`, `docs/design/omp-plugin-compatibility.md`, `docs/research/harness-parity-audit.md` | atomic-implementer (mode: feature) | ~15 | Phase P0; depends on CP17C and CP17E. Each page shows bare `atomic install` as the default command and `--harness` as the narrowing flag, states that `atomic update` adopts a legacy Claude install once and names the blockers that stop it, and names no `ATOMIC_HARNESS`, `harness.dir`, `.pi` state root, `[pi.agents]` table, or `atomic config resolve`; each named spec body states the same with a Change log entry and `atomic validate spec` passes; the design doc and the parity audit state the same decisions with a dated entry each, and the audit names CP17A and CP10A as the closing checkpoints for P-11, P-12, P-13 and the two `statelocation.go` review findings; `install.sh` prints `atomic install` as its next step; the help-router check reports zero missing commands; the VitePress build passes |
| CP10C1 | Make state fixtures independent of the built-in default (1 of 11) | `cmd/atomic/{cmd_bus_test.go,cmd_claude_test.go,cmd_docs_test.go,cmd_harness_test.go,cmd_migrate_test.go,cmd_retro_test.go,cmd_state_test.go,cmd_update_converge_test.go,main_test.go}`, `claudeinstall/{install_test.go,uninstall_test.go}`, `cliutil/usage_test.go` | atomic-implementer (mode: feature) | 12 | Runs in phase P1, before CP10A. Each fixture builds its state paths from its own root or pins its segment explicitly; a file that names only Claude-native `.claude` paths (config directory, settings, worktrees, agents) is left unchanged; a test whose intent is the default value itself stays in `config` and flips in CP10A. The touched packages are green with the built-in default set to `.claude` and, in a scratch build, to `.atomic` |
| CP10C2 | Make state fixtures independent of the built-in default (2 of 11) | `codeintel/cli/{code_ignore_test.go,code_test.go,comments_internal_test.go,realm_test.go}`, `codeintel/engine/{engine_ignore_test.go,engine_test.go,sql_e2e_test.go,sql_string_match_e2e_test.go}`, `codeintel/mcp/{cwd_independent_test.go,daemon_test.go}`, `codeintel/realm/realm_test.go` | atomic-implementer (mode: feature) | 11 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C3 | Make state fixtures independent of the built-in default (3 of 11) | `config/{cli_test.go,config_test.go,harness_retired_test.go,harness_test.go,projectstate_test.go,repo_test.go,scope_test.go,statelocation_test.go,statemigrate_test.go}`, `dockerinit/{convergence_test.go,dockerinit_test.go}`, `docs/docs_test.go`, `doctor/checks_code_index_test.go` | atomic-implementer (mode: feature) | 13 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C4 | Make state fixtures independent of the built-in default (4 of 11) | `doctor/{checks_followups_test.go,checks_hooks_internal_test.go,checks_hooks_test.go,checks_memory_test.go,checks_migrate_test.go,checks_multiharness_internal_test.go,checks_output_style_test.go,checks_profile_test.go,checks_refs_test.go,checks_repo_config_test.go,doctor_test.go,fix_impls_internal_test.go}` | atomic-implementer (mode: feature) | 12 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C5 | Make state fixtures independent of the built-in default (5 of 11) | `doctor/{fix_test.go,format_verbose_test.go,shortcircuit_test.go}`, `followups/{add_test.go,cli_test.go,close_test.go,list_test.go}`, `harness/claude/{artifacts_test.go,claude_test.go,migration_test.go,steering_test.go}` | atomic-implementer (mode: feature) | 11 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C6 | Make state fixtures independent of the built-in default (6 of 11) | `harness/{enrollment_test.go,harness_test.go}`, `harness/omp/{omp_test.go,rules_test.go}`, `hooks/{hooks_test.go,outputstyle_test.go}`, `install/install_test.go`, `installstate/{classify_test.go,installstate_test.go,legacy_test.go,legacyfixture_test.go}` | atomic-implementer (mode: feature) | 11 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C7 | Make state fixtures independent of the built-in default (7 of 11) | `mdlink/{gitignore_internal_test.go,mdlink_test.go}`, `migrate/{steps_scratchpad_test.go,steps_test.go}`, `profile/conflict_test.go`, `repl/{action_test.go,binary_e2e_test.go}`, `repoctx/repoctx_test.go`, `repoinit/repoinit_test.go` | atomic-implementer (mode: feature) | 9 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C8 | Make state fixtures independent of the built-in default (8 of 11) | `runtimeproof/{guidance_test.go,runtimeproof_test.go,scenarios_codex8b_test.go,scenarios_determinism_test.go,scenarios_lifecycle_test.go,scenarios_migration_test.go,scenarios_mixed8b_test.go,scenarios_repo_test.go,scenarios_test.go}`, `serve/{api_bus_test.go,api_code_realm_test.go,api_code_test.go}` | atomic-implementer (mode: feature) | 12 | Runs in phase P1, before CP10A; same criterion as CP10C1. `TestCP8AStateRootPersistenceAndRollback` in `runtimeproof/scenarios_migration_test.go` writes its selection record under an isolated home, so a run leaves the live `~/.atomic` byte-identical |
| CP10C9 | Make state fixtures independent of the built-in default (9 of 11) | `serve/{api_plans_page_test.go,api_plans_test.go,code_graph_members_test.go,code_members_test.go,codegraph_test.go,graph_test.go,nav_test.go,plans_test.go,scope_identity_internal_test.go,serve_test.go,walk_gitignores_test.go,walk_test.go}` | atomic-implementer (mode: feature) | 12 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C10 | Make state fixtures independent of the built-in default (10 of 11) | `serve/frontend/src/components/nav/TopBar.test.tsx`, `serve/frontend/src/components/plans/{PlansView.test.tsx,SlugView.test.tsx,VersionPicker.test.tsx,resolve.test.ts}`, `serve/frontend/src/components/rail/PlansRail.test.tsx`, `serve/frontend/src/utils/planViewStore.test.ts`, `signals/signals_test.go`, `tools/bundle-mirror/main_test.go`, `where/{projection_parse_test.go,where_authority_test.go,where_test.go}` | atomic-implementer (mode: feature) | 12 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP10C11 | Make state fixtures independent of the built-in default (11 of 11) | `wiki/{bucket_doc_cli_test.go,bucketdoc_test.go,discover_test.go,init_test.go,projection_seed_test.go,refresh_test.go,registry_authority_test.go,registry_test.go,stale_test.go,staleness_test.go,wiki_test.go}` | atomic-implementer (mode: feature) | 11 | Runs in phase P1, before CP10A; same criterion as CP10C1 |
| CP9A | Add harness detection and the session-start inputs | `harness/detect.go`, `cmd_hooks.go` `--harness`, `--cwd`, and stdin, `hooks/hooks.go` directory resolution, `sessionStartCommand`, and `hasRegistration`, `hooks/hooks_hujson.go` `registerInSettings` and unregistration, `hooks/ownership.go` `ownedSessionStartEntries`, tests | atomic-implementer (mode: feature) | ~9 | Phase P1; depends on CP17A. Table-driven environment maps return every confidence: one strong harness, Claude markers plus Codex markers as `ambiguous` with both candidates, OMP `AGENT=1` with `CI=true` as `weak`, never-marker variables ignored, an empty map as `none`; evidence carries names only; an `ATOMIC_HARNESS` entry in the map changes no result; a verb given `--harness` never calls detection. This row owns the `--harness`, `--cwd`, and optional-stdin inputs of `atomic hooks session-start`: `--cwd repo/sub/dir` resolves the repository root through the marker and Git, and an unflagged invocation behaves as `--harness claude`. The Claude registration converges to `atomic hooks session-start --harness claude`: the ownership match recognizes both the unflagged and the flagged command, so an existing registration converges as Atomic-owned bytes with no duplicate entry and no conflict; registration rewrites an unflagged entry to the flagged command, and unregistration removes either form. No marker changes repository-state resolution |
| CP10A | Flip the resolver to the harness-neutral ladder | `config/statelocation.go`, `config/config.go`, `config/pathsegment.go`, `config/harness.go`, `doctor/checks_config.go`, `cmd_state.go`, `config`, `doctor`, and `cmd_state` tests | atomic-implementer (mode: feature) | ~9 | Phase P2; depends on CP17A and CP10C1 through CP10C11. Closes P-12 and P-13. Rungs answer in ladder order; `.atomic` is the built-in state default; `.atomic` is a candidate; `rules/wiki` is a signature and `worktrees` is not, so a worktree-only `.claude` is unpopulated; a populated destination (`state.dir`, else `.atomic`) answers while a populated `.claude` beside it draws one stderr warning naming its leftover entries, and only two populated non-destination candidates refuse with the `atomic state adopt` instruction; the cached resolver every path helper uses gains an error channel, so an invalid `ATOMIC_STATE_DIR` refuses there as in the strict resolver instead of falling through; a multi-segment `state.dir` is accepted while absolute, empty, and parent-escaping values are refused; `atomic state adopt --dir` persists absolute and relative paths that linked worktrees resolve. Tests that pin the default value flip here |
| CP10B | Pass the repository root everywhere, key worktrees by harness, and move realm federation under the resolver | `repoinit/`, `codeintel/cli/code.go`, `signals/tree.go`, `config/harness.go` worktree helper, `codeintel/realm/config.go`, `codeintel/realm/resolver.go`, `codeintel/cli/realm.go`, `doctor/checks_code_index.go`, `cmd_repo.go`, `cliusage/` help text, tests | atomic-implementer (mode: feature) | ~12 | Phase P2; depends on CP10A. Closes P-14. No production call passes an empty root to a state helper; a selection record naming a custom root is honored by `atomic repo init`, the `.gitignore` writer, and the signals skip list. One worktree helper maps harness `claude` to `.claude/worktrees` and `omp`, `codex`, or an unknown harness to `<state-root>/worktrees`, and an explicit preference path wins over both; `off` stays `off`. CP9B2, WP1, and WP3 verify that `atomic where --json` and `atomic workflow resolve` call this one helper. The state-root `.gitignore` carries `/.scratchpad/`, `/.atomic-index/`, and `/worktrees/`; `.claude/.gitignore` gains `/worktrees/` only when `.claude/` exists or a Claude target is enrolled, so an OMP-only repository gains no `.claude/`; a linked worktree under `.atomic/worktrees/` or `.claude/worktrees/` never shows as untracked in `git status`; a state root outside the repository gets no repository ignore rule. Realm `code.toml` and member databases live in the resolved realm state root, a custom realm root carries them, `/*.db` is ignored there, and a federation reader also accepts the legacy `<realm>/.atomic/` location |
| CP11 | Add `atomic state migrate`, its automatic trigger, and the wiki-card link and copy | `config/` migration engine, `cmd_state.go`, `cmd_repo.go`, `cmd_update.go`, `harness/claude/` card link, `harness/rule.go` `ClaudeRulePath`, `rules/rules.go` `NativeName`, the OMP card projection in `harness/omp/rules.go`, `doctor/` card-link check, `wiki/action.go`, tests including the native-collision tests | atomic-implementer (mode: feature) | ~12 | Phase P4; depends on CP10B and WP3. The destination is the selection record, else `state.dir`, else `.atomic`, and a set `ATOMIC_STATE_DIR` is never used as one; nothing moves when the record or `state.dir` names the legacy root; the moved set (including `workflow-preferences.md`) and the left set match the criteria; a tracked file keeps its history with its rename staged as `git mv` would; an ignored bundle is renamed; the cross-device fallback copies and removes; a non-Git realm root migrates with plain renames, its federation files stay in place when the destination is `.atomic`, and when it is not, `<realm>/.atomic/{code.toml,*.db}` is migrated as a second source into the destination; with no MCP daemon live the legacy index directory is removed, and with a live daemon (answering on its socket) the directory stays, is reported, and `atomic code index` then builds a fresh index at the destination; no `state-location.json` is written, nothing is committed, and the staged renames are printed. `atomic state migrate`, `atomic repo init`, and `atomic update` in a repository migrate once with a notice; `atomic code index`, `atomic scratchpad new`, `atomic followups add`, the `atomic wiki` verbs, and WP3's `atomic workflow set` read and write the populated legacy root in place and move no file; a fixture whose `.claude` holds only `.atomic-index` beside a populated `.atomic` makes `atomic state migrate` remove it and exit 0 with no daemon live, and report the path and exit 0 with one live; a `.claude` holding only `.atomic-index` and an empty destination is not migrated; a repository root equal to the home directory is refused; a failure partway reverses the moves made; with both roots otherwise populated, `atomic state migrate` exits 1 naming the leftover `.claude` entries, and `atomic repo init` and `atomic update` each print the one resolution warning and continue against the destination; `atomic repo init` that migrates writes its own state into the new root in the same process; hand-wired references are reported; a linked worktree on an unmigrated branch keeps resolving its own legacy root, while a pre-migration branch checked out in the same worktree reads `.atomic/` with the leftover warning. Migrate, `atomic repo init`, wiki refresh, and install and update convergence create the ledger-owned relative symlink `<repo>/.claude/rules/atomic -> ../../<state-root>/rules` when it is missing and the state root lies inside the repository, never ignore it, and leave it for the user to commit (any triggered migration stages it); install and update create it only in the repository of the current working directory; when the link is ignored, a fixture with a repository `.gitignore` gets a negation appended after `git check-ignore -v`, and a fixture whose only match is a global excludes file reports that path and stages nothing; a link with the expected target and no ledger row is adopted, not a conflict; target uninstall reports the link and leaves it; doctor names the subdirectory-session limit when the link exists; a Claude session started at the repository root loads `.claude/rules/atomic/wiki/<domain>.md` through it; no link is written when the state root is `.claude`; a state root outside the repository gets a generated copy at `<repo>/.claude/rules/atomic/wiki/` with `/rules/atomic/` in `.claude/.gitignore`; a foreign `<repo>/.claude/rules/atomic` is a named conflict; `NativeName()` returns `atomic/wiki/<domain>` for a wiki card and is still the native-collision key, so a shipped source at `context/rules/atomic/wiki/<domain>.md` collides with that card and is rejected; `ClaudeRulePath` and its comment place wiki cards at `rules/atomic/wiki/<domain>.md` under the repository `.claude/` and shipped rules at `rules/atomic/<name>.md` under the user root, which never collide on disk; `ProjectCards.Dir` in `harness/omp/rules.go` is `<repo>/.omp/rules`, so each OMP card lands at `<repo>/.omp/rules/atomic-wiki-<domain>.md` when missing or stale: as the relative link to its canonical card when the state root lies inside the repository, and as a generated flat copy (frontmatter `description` and `globs`, per the wiki-card criterion) only when it lies outside; `cardDomain` keeps refusing a `/` in a domain, a foreign file at that name is a named conflict, a dropped card's link or copy is removed when its ledger row matches, no `.omp/.gitignore` rule is written, and an existing ledger-owned nested `<repo>/.omp/rules/atomic/wiki/` copy is removed with its `/rules/atomic/` ignore line when its row matches, and kept and reported when it does not; no `.claude/rules/atomic-wiki/` projection or `/rules/atomic-wiki/` ignore rule is written; an existing ledger-owned `<repo>/.omp/rules/atomic-wiki/` tree and any `<repo>/.claude/rules/atomic-wiki/` tree are removed when their ledger rows match, and kept and reported when they do not; the `atomic wiki` verbs call `wiki.Refresh` and `(*omp.Adapter).PublishProjectCards` (P-43) |
| CP11B | Migrate this repository's own state | `.claude/{atomic.toml,project,rules/wiki}` to `.atomic/`, `.claude/.gitignore`, root `.gitignore`, `.githooks/pre-commit`, root `CLAUDE.md`, `scripts/code-eval/` | atomic-implementer (mode: feature) | ~7 plus renames | Phase P4; depends on CP11. `atomic state migrate` run on this repository moves its state with history; the root `.gitignore` drops its `.claude/.atomic-index/` line and `.claude/.gitignore` drops its stale state rules (`/.scratchpad/`, `/.atomic-index/`, `!/rules/wiki/` and its `**` pair) while keeping the contributor negations; the pre-commit hook regenerates the follow-ups index at the resolved path; the root `CLAUDE.md` follow-ups `@`-ref resolves and its wiki-card and `atomic.toml` path text names `.atomic`; the code-eval scripts read the index from the resolved root; `.claude/rules/wiki/` moves to `.atomic/rules/wiki/`, and the committed symlink `.claude/rules/atomic -> ../../.atomic/rules` is created with a `.claude/.gitignore` negation that tracks it; `.claude/` keeps only contributor rules, skills, commands, settings, worktrees, and that link; a Claude session at this repository's root loads the wiki cards through the link; the active loop bundle under `.scratchpad/` moves with the rest, and after this row the orchestrator resolves `$SCRATCH` with `atomic scratchpad path` |
| CP9B1 | Route the detection consumers and the wiki projection | `config/cli.go` default-harness selection, `retro/`, `cmd_retro.go`, `bus/identity.go`, `doctor/shortcircuit.go`, `cmd_doctor.go`, `hooks/hooks.go`, `wiki/action.go`, `wiki/registry.go`, `cmd_wiki.go`, tests | atomic-implementer (mode: feature) | ~12 | Phase P4; depends on CP9A. The one detection rule holds: `weak` takes no action in a verb that writes into a harness root, and `atomic config agents` alone uses the single enrolled harness on `weak`, `ambiguous`, or `none`, through the selection function CP12C's `--harness` flag overrides. Closes P-47 and P-60: `retro extract` reads Claude history and reports `unsupported` on OMP-only and Codex-only homes. Closes P-45: `atomic wiki`, without consulting detection, writes `~/.atomic/wikis.md` first and projects `<wikis>` into the profile `AGENTS.md` on an OMP-only home and creates no `~/.claude/CLAUDE.md`. `bus` reads `CODEX_THREAD_ID` under Codex; doctor runs the detected harness's leg; `session-start` refreshes the profile under every `--harness` value and seeds the style only under `claude` (P-37, P-50 profile half) |
| CP9B2 | Add `state_root` and `worktrees` to `atomic where --json` | `where/`, `cmd_where.go`, `docs/spec/atomic-where.md`, tests | atomic-implementer (mode: feature) | ~5 | Phase P4; depends on CP9A, CP10B, and WP1. `state_root` is the resolved absolute state root. `worktrees` comes from the CP10B helper for the `--harness` value or else the detected harness: a set preference path outranks the harness default, and an unknown harness takes `<state-root>/worktrees`; the value is repository-relative when inside the repository and absolute otherwise; it is `""` only when the preference is `off`. The `atomic-where` spec states both fields with a Change log entry |
| CP12A | Add the override schema, overlay, and the enrolled Claude path | `config/agentoverride.go`, `config/config.go`, `config/render.go`, `harness/overlay.go`, `harness/claude/artifacts.go`, `harness/claude/evidence.go`, `harness/claude/lifecycle.go`, `claudeinstall/install.go`, `doctor/checks_config.go`, tests | atomic-implementer (mode: feature) | ~12 | Phase P7; depends on CP10B. All three tables decode into `AgentOverride` and a scalar under each is a decode error; per-harness validation matches the criteria; the overlay is pure and an empty field writes no key; no adapter supplies a default model or effort (P-1, P-20, P-22, P-40, P-53); projection goldens and the policy-key refusal, P3, and provider-sweep tests are unchanged; enrolled `atomic claude install` and `atomic harness repair` write overlaid bytes whose digest the claim records; an agent file a prior install patched with the same override adopts as owned; `config list` shows overrides by harness; doctor category 9 warns for an unenrolled harness and a set `CLAUDE_CODE_SUBAGENT_MODEL` or `CLAUDE_CODE_EFFORT_LEVEL` |
| CP12B | Deliver OMP and Codex agents natively with overlay claims | `harness/omp/enroll.go`, `harness/omp/agents.go`, `harness/omp/package.go`, `harness/codex/enroll.go`, `harness/codex/agents.go`, `harness/agent.go`, `harness/capability.go`, `doctor/checks_config.go`, status seams, tests, agent rows in `docs/research/harness-capability-matrix.md` | atomic-implementer (mode: feature) | ~11 | Phase P7; depends on CP12A. Closes P-39 and P-52: agent files land at `<profile agent root>/agents/` and `$CODEX_HOME/agents/` as ledger-owned file resources with OMP `model`/`thinking` and Codex `model`/`model_reasoning_effort` (P-40, P-53); the OMP corpus store carries no `agents/` tree; a changed override converges, a hand edit conflicts, and a target uninstall removes them; delivery reads `unsupported` in enrollment, status, and doctor until the matrix holds a passing agent row, and doctor category 9 warns for an override stored for a harness whose agent delivery is `unsupported`; the probe procedure for each harness is recorded in the matrix |
| CP12C | Add `atomic config agents --harness` and converge through the lifecycle | `config/cli.go`, `config/agents.go`, `cmd/atomic/main.go` hook wiring, `cmd/atomic/cmd_config.go`, `docs/reference/agents.md`, `docs/spec/agents-effort-config.md`, tests | atomic-implementer (mode: feature) | ~8 | Phase P7; depends on CP9B1 and CP12B. Selection follows the agent-override-convergence flow, including the single-enrolled exception and the non-interactive refusal; each harness shows its own form; a harness that is not enrolled is reported as applying when enrolled; a save converges through lock, journal, and ledger and patches nothing in place; the config package exposes no `ApplyAgentsHook`; `ReapplyAgents` runs only for an unadopted legacy root; an enrolled root under `CLAUDE_CONFIG_DIR` converges; a corpus grep finds no explicit model passed to a named Atomic agent (P-24); the reference page and spec state the per-harness contract with a Change log entry |
| CP13A | Deliver session context on OMP | `harness/omp/extension.go`, `harness/omp/session.go`, golden extension fixtures, tests | atomic-implementer (mode: feature) | ~6 | Phase P7; depends on CP9B1 and WP4. Closes P-37: the generated module runs the verb once at `session_start` with an explicit working directory and timeout and never inside `before_agent_start`; the verb refreshes `~/.atomic/profile.md` and seeds no output style; the appended block replaces an earlier copy; a failure, a timeout, or a missing binary adds nothing; resumed and child sessions resolve their own directory; the verb writes no card file; the module is byte-identical across machines; the preferences section comes from the resolver `docs/spec/workflow-preferences.md` defines |
| CP13B | Deliver session context on Codex and report hook trust | `harness/codex/hooks.go`, `harness/codex/enroll.go`, ownership helpers in `hooks/`, Codex doctor checks in `doctor/`, `harness/capability.go`, tests | atomic-implementer (mode: feature) | ~8 | Phase P7; depends on CP9B1 and WP4. Closes P-50: the `SessionStart` member is written with every other `hooks.json` byte preserved and a digest over the owned member only; the verb refreshes the profile and seeds no output style; target uninstall strips only that member; an edited owned member conflicts; doctor reports trusted, untrusted, or unknown without fabricating trust; Codex session-context delivery reads `unsupported` until a runtime probe passes |
| CP10D1 | Exempt the resolved state root in serve | `serve/walk.go`, `serve/walk_test.go` | atomic-implementer (mode: feature) | 2 | Phase P8; depends on CP10A. Cards and follow-ups under `.atomic` and under a custom root are servable; other dot directories stay skipped |
| CP10D2 | Rewrite state-root text in steering, agents, and partials | `context/AGENTS.md`, `context/agents/atomic-wiki-inferrer.md`, `context/agents/atomic-strategist.md`, `context/_partials/{signals-gate,loop-finalize,implement-loop,agent-implementer-workflow,agent-search-tooling,agent-code-intel,worktree-setup}.md` | atomic-implementer (mode: feature) | ~10 | Phase P8; depends on CP11, WP6a, WP9a, WP9b, and WP10, which edit the same files. Every repository-state path reads `<state-root>` or the `.atomic` default; prose names `atomic where --json` `state_root` as the source of `<state-root>`; Claude-native `.claude/worktrees` and settings text is unchanged; `atomic validate artifacts` passes |
| CP10D3 | Rewrite state-root text in commands and skill references | `context/commands/{refresh-wiki,deslop,atomic-plan,subagent-diagnose,documentation,retrospective-learning,atomic-help,setup-wiki}.md`, `context/skills/atomic-wiki/references/repo.md` | atomic-implementer (mode: feature) | ~9 | Phase P8; depends on CP10D2. Same criterion as CP10D2; the `/atomic-help` state-file tour stage and `state-root` topic row name `.atomic`, `atomic state migrate`, and `atomic state adopt --dir`; the help-router check reports zero missing commands |
| CP10D4 | Rewrite state-root text in reference pages, guides, and README | `docs/reference/{atomic-toml,concepts,conventions,repo-wiki,realm-wiki,code-intel,repl,commands,serve}.md`, `docs/guides/{code-intel-mcp,getting-started,install}.md`, `README.md` | atomic-implementer (mode: feature) | ~13 | Phase P8; depends on CP11, CP17D, and WP6b. Every page states the `.atomic` default and links to the ladder in `docs/reference/concepts.md`, which alone carries the full ladder and the migration destination; a page that discusses repository state or worktrees also states `atomic state migrate` and the worktree locations; the VitePress build passes |
| CP10D5 | Amend specs whose body states the `.claude` state default: resolution and layout | `docs/spec/{atomic-state-and-config,configurable-state-paths,repo-init,wiki-pointer-rules,follow-ups-folder,scope-marker,wiki,signals-router,signals-workflow,graphignore,wiki-storage-relocation,atomic-doctor}.md` | atomic-implementer (mode: feature) | 12 | Phase P8; depends on CP11, WP6c, and WP6d. In each file, every body mention outside `## Change log` that states a `.claude/` state subpath as current truth is rewritten to the current ladder, layout, and ignore rules, with a Change log entry and no history in the body; a mention that names a legacy migration source stays; `atomic validate spec` passes |
| CP10D6 | Amend specs whose body states the `.claude` state default: binary, code intelligence, validation | `docs/spec/{artifact-renames,atomic-binary,atomic-repl,atomic-validate,code-intel-engine,code-intel-extraction,code-intel-integration,code-intel-realm,code-intel-substrate,code-intel-surfaces,code-mcp-per-repo,comment-counter}.md` | atomic-implementer (mode: feature) | 12 | Phase P8; depends on CP11 and WP6c; same criterion as CP10D5 |
| CP10D7 | Amend specs whose body states the `.claude` state default: documentation, install, serve | `docs/spec/{document-templates,documentation-as-maintenance,documentation-skill-split,embedded-sql-extraction,embedded-sql-language-expansion,followup-hardening,install-workflow,retro-extract,selfupdate-state,serve-bus-chat,serve-plans-page,serve-realm-ux}.md` | atomic-implementer (mode: feature) | 12 | Phase P8; depends on CP11, WP6c, and WP6d; same criterion as CP10D5 |
| CP10D8 | Amend specs whose body states the `.claude` state default: where, signals, remaining | `docs/spec/{atomic-where,signals-project-detection,signals-wiki-linkify,sql-language-support,subagent-diagnose,typed-followups,uninstall,wiki-deterministic-setup}.md` | atomic-implementer (mode: feature) | 8 | Phase P8; depends on CP9B2, CP11, WP6c, and WP6d; same criterion as CP10D5 |
| CP15A1 | Close the open Claude lifecycle gaps | `cmd_update.go`, `cmd_claude.go`, `cmd_install.go`, `install/install.go`, `installstate/legacy.go`, `harness/claude/lifecycle.go`, `install.sh`, `docs/guides/install.md`, `docs/spec/user-profile.md`, tests | atomic-implementer (mode: feature) | ~10 | Phase P8; depends on CP12C, WP6b, and CP10D4, which share `install.sh` and `docs/guides/install.md`. Rows with status `redesign` or `accepted` are out of scope for CP15A1 through CP15A5, as are P-9, P-17, P-21, and P-25, which CP14B1 and CP14C close. Each gap has a regression test that fails before the fix: P-2 (the already-current path on an un-adopted install runs migrations and the post-update doctor), P-3, P-4, P-5, P-6, P-7, and P-10 (an install on any harness creates the profile stub when absent, and `config.toml` and the pre-install manifest where `next` did); the `user-profile` spec states that an install on any harness creates the profile stub, with a Change log entry; the audit gains a status line per closed row |
| CP15A2 | Close the open Claude wiki-registry and reporting gaps | `wiki/registry.go`, `harness/claude/migration.go`, `wiki/init.go`, `doctor/checks_refs.go`, `harness/claude/lifecycle.go`, `harness/capability.go`, `runtimeproof/`, tests | atomic-implementer (mode: feature) | ~9 | Phase P8; depends on CP15A1. Closes P-15, P-16, P-28, P-33, and P-34 with a regression test each; P-33 and P-34 carry a Claude runtime observation; the audit gains a status line per closed row |
| CP15A3 | Close the open Claude wiki-pipeline text gaps | `context/agents/atomic-wiki-inferrer.md`, `context/skills/atomic-wiki/references/repo.md`, `context/commands/refresh-wiki.md`, `context/commands/setup-wiki.md`, tests | atomic-implementer (mode: feature) | ~5 | Phase P8; depends on CP10D3, WP6a, WP9a, WP9b, and WP10, which edit the same files. Closes P-26 and P-27: the inferrer resolves the `atomic-wiki` reference path from a source the runtime reports, and the router ref lands in the steering file Claude loads, while the canonical source stays harness-neutral; `atomic validate artifacts` passes |
| CP15A4 | Render the native vocabulary paragraph | `artifacts/renderer.go` (`ClaudeSteering`), `harness/claude/artifacts.go`, `claudeinstall/{install,uninstall}.go`, `validate/projection.go`, `docs/reference/bus.md`, `harness/skill.go`, `context/AGENTS.md`, steering goldens, `context/commands/watch-ci.md`, `context/skills/atomic-bus/SKILL.md`, tests | atomic-implementer (mode: feature) | ~10 | Phase P8; depends on CP15A3. Closes P-29, P-30, P-23, and P-22b: the Claude steering block ends with a golden-tested paragraph that names the Agent tool with `subagent_type` set to the agent name (an omitted one falls to a general-purpose agent without the agent's skills), the question tool, the worktree-entry tool, and the background-listener tool with `timeout_ms` (capped at 30 minutes) and its stop tool; the OMP block names only tools a capability row proves; the paragraph carries tool names only; `/watch-ci` and `atomic-bus` describe the background dispatch and the listener so the paragraph's names apply; the canonical corpus stays harness-neutral and `atomic validate artifacts` reports no leaked Claude-only token |
| CP15A5 | Fix the dispatch wording the vocabulary relies on | `context/skills/atomic-wiki/references/repo.md`, `context/agents/atomic-wiki-inferrer.md`, `context/commands/{autopilot,implement,quick-fix,subagent-implementation,review-branch,deslop,refresh-wiki,subagent-diagnose,retrospective-learning}.md` | atomic-implementer (mode: feature) | ~11 | Phase P8; depends on CP15A4. Closes the remaining P-29 wording: every dispatch of an Atomic agent names the agent, so the vocabulary paragraph maps it to `subagent_type`; `atomic validate artifacts` passes |
| CP15B1 | Deliver OMP skills, commands, rules, and steering features behind probes | `harness/omp/package.go`, `harness/omp/enroll.go`, `harness/omp/rules.go`, `harness/omp/steering.go`, `harness/capability.go`, `doctor/`, `docs/research/harness-capability-matrix.md`, runtime-proof probes, tests | atomic-implementer (mode: feature) | ~10 | Phase P8; depends on CP11, CP12B, and CP13A. A probe per artifact kind is recorded, and each proven kind is written to its native location as ledger-owned resources and removed by target uninstall: P-38 commands, P-41 skills, P-42 matched rule bodies, P-35 and P-44 `@` imports in the profile `AGENTS.md`, P-36 output-style selection. Each unproven kind reads `unsupported` at WARN severity in enrollment, status, rules, and doctor (P-46) |
| CP15B2 | Deliver Codex steering, skills, commands, rules, and registration behind probes | `harness/codex/plugin.go`, `harness/codex/enroll.go`, `harness/codex/register.go`, `harness/codex/steering.go`, `wiki/registry.go`, `harness/capability.go`, `doctor/`, `docs/research/harness-capability-matrix.md`, runtime-proof probes, tests | atomic-implementer (mode: feature) | ~11 | Phase P8; depends on CP9B1, CP12B, CP13B, and CP15A2, which shares `wiki/registry.go`. Same probe rule as CP15B1 for P-48 (global `$CODEX_HOME/AGENTS.md` managed block, probed with `codex debug prompt-input`), P-49 output style, the Codex native-vocabulary paragraph inside that block (only tools a capability row proves), P-54 skills, P-55 rule hooks, P-56 wiki cards, P-57 `@` imports, and P-61 plugin registration, where `Register` and `DefaultRegister` gain their production caller (review no-caller finding). Closes P-58: once the global block exists, `atomic wiki` projects `<wikis>` into it for an enrolled Codex target. P-51 commands stay `unsupported` without a proven explicit invocation surface; P-59 doctor category 24 prints the remediation for each `unsupported` surface; `install --harness codex` never reports `applied` for a surface it did not write |
| CP14A | Close the blocking review findings | `cmd_install.go`, `cmd_update.go`, `install/uninstall.go`, `install/install.go`, tests | atomic-implementer (mode: feature) | ~8 | Phase P9; depends on CP15A2, CP15A5, and CP15B2. Closes `printConvergeReports`: `install`, `enroll`, `repair`, `adopt`, and `rules sync` with `--json` exit non-zero when any target is blocked or conflicted. Closes `runUpdateWith` post-swap handling (P-2, post-swap half): `atomic update` exits with the re-executed child's status and prints no repair advice when the child printed its own instruction. Closes the `Steps.removalOptions` `Confirm` callback: the `--discard-changed` plan prints every resource it discards before applying, under `--yes` that printed plan is the approved plan and the apply step refuses any discard it did not name, and each discarded resource keeps a recovery copy under `~/.atomic/backups`. P-11 is closed by CP17A, which deletes `obsoleteHarnessRefusal` and `refuseObsoleteHarnessEnv`; this row re-verifies that `atomic hooks session-start` and every verb ignore `ATOMIC_HARNESS`, and that the cached-resolver refusal CP10A added still holds |
| CP14B1 | Close the adapter and doctor review risks | `doctor/shortcircuit.go`, `cmd_doctor.go`, `doctor/checks_profile.go`, `hooks/hooks.go`, `harness/omp/enroll.go`, `harness/codex/enroll.go`, `installstate/recovery.go`, `harness/claude/artifacts.go`, `harness/uninstall.go`, `docs/guides/install.md`, tests | atomic-implementer (mode: feature) | ~12 | Phase P9; depends on CP14A. Closes `ClaudeHomeMissing` (P-9): with `CLAUDE_CONFIG_DIR` set, doctor and the session-start hook resolve the Claude root through the Claude adapter's config-directory resolution, and the install guide documents the variable. Closes the `(*omp.Adapter).Enroll` extension publication: a user-authored or user-edited `extensions/atomic.ts` takes the replace-or-leave-unowned decision instead of being replaced. Closes the `(*omp.Adapter).Enroll` and `(*codex.Adapter).Enroll` target records: a crash between publication and `Complete` leaves a target record recovery restores, so update and repair still see the target. Closes `planTargetRemoval` shared-consumer counting: a consumer counts as shared only at the same id and the same physical applied path, with aliased roots (a symlinked `CLAUDE_CONFIG_DIR`, a case-only difference) resolving to one, so uninstalling one of two distinct Claude roots removes that root's unchanged files and uninstalling one of two targets over one root retains them |
| CP14B2 | Close the install-state, validation, and dead-code review risks | `harness/uninstall.go`, `installstate/lock.go`, `installstate/transaction.go`, `installstate/recovery.go`, `validate/projection.go`, `validate/dispatch_test.go`, `config/paths.go`, `harness/claude/steering.go`, tests | atomic-implementer (mode: feature) | ~10 | Phase P9; depends on CP14B1. Closes `removeBlock`: a whitespace remainder behind a symlinked `CLAUDE.md` or `AGENTS.md` is written through the link and the link survives. Closes `installstate.AcquireLock`: it tries a non-blocking lock first and, when another process holds it, prints the holder from `ReadLockIdentity` before it waits. Closes `(*Transaction).Complete` ordering: units are journaled committed before the ledger write, so `atomic harness recover --rollback` after a crash in that window leaves no ledger row disagreeing with the restored bytes. Closes `TestPerfBudget`: the whole-repo budget holds locally under `make test`, and its comment states the budget's reason or is deleted. Closes the `projGate` finding keys: a rule failing only the runtime-index or tier check counts as FAIL against its source, not as PASS. Closes the no-caller finding: `config.RemoveBackupStamp` is deleted; the `claude.MigrateScope` relocate path is deleted; `installstate.ReadLockIdentity` is used by the lock fix above; codex `Register` and `DefaultRegister` are wired by CP15B2; `(*omp.Adapter).PublishProjectCards`, `wiki.Refresh`, and `pruneCards` are wired by CP11 into the wiki verbs |
| CP14C | Close the wiki and corpus review risks | `wiki/action.go`, `docs/reference/realm-wiki.md`, `docs/guides/knowledge-base.md`, `context/agents/atomic-wiki-writer.md`, `docs/reference/agents.md`, `context/commands/atomic-help.md`, `docs/spec/wiki.md`, `docs/spec/wiki-buckets.md`, tests | atomic-implementer (mode: feature) | ~9 | Phase P9; depends on CP14B2 and CP10D5. Closes `wikiBucketAddAction` (P-17): `bucket add` writes the capture section and placeholder into the realm file that already holds the section; else into a `<realm>/CLAUDE.md` that does not import `AGENTS.md`; else into `<realm>/AGENTS.md`, writing the loader pair when `CLAUDE.md` is absent. When the section stays in `AGENTS.md` behind a `CLAUDE.md` that does not import it, `bucket add` warns on stderr naming that file. Both pages, the `atomic-wiki` skill, and `/refresh-wiki` say so. Closes the no-caller finding for `wiki.Refresh`, `pruneCards`, and `(*omp.Adapter).PublishProjectCards`, re-verifying CP11. Closes the `atomic-wiki-writer` no-fan-out claim (P-21) in the agent and the reference page: the restriction is stated as an instruction. Closes the truncated `/atomic-help` `update` row (P-25). Closes the `wiki` and `wiki-buckets` spec registry statements: both bodies name `~/.atomic/wikis.md` as the authority and the `<wikis>` block as a projection, each with a Change log entry |
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
| CP16 | Prove the redesign on real Claude Code, OMP, and Codex binaries | `runtimeproof/`, `<state-root>/.scratchpad/omp-plugin-compatibility/runtime/`, `docs/research/harness-capability-matrix.md` | atomic-implementer (mode: feature) | ~6 | Phase P10; depends on WP1, WP4, WP9b, CP11, CP12C, CP13A, CP13B, CP15B1, CP15B2, and CP14D17. It runs the installed binaries (Claude Code 2.1.322, omp 18.18.3, codex-cli 0.147.0) against isolated homes and repositories, exporting `HOME`, `CLAUDE_CONFIG_DIR`, the OMP profile root, and `CODEX_HOME` into each; it never adopts, enrolls, or writes the owner's live home, and a live-home check is out of scope. The matrix evidence was taken on Claude 2.1.273 and OMP 18.1.18, so this row re-records every capability row it relies on at the tested versions, including the Claude row for the subdirectory-session limit of the `.claude/rules/atomic` link. The retained evidence shows a repository migrated from `.claude` to `.atomic`, wiki cards loaded through the `.claude/rules/atomic` link, an agent override projected into the native agent file and the agent listed at startup, and session context carrying the resolved workflow-preferences block at startup level (hook output or init event), with model-level proof left to a credentialed run, each with the command, harness version, and output digest; it re-runs the evidence of every `redesign` audit row that CP9A through CP15B2 closed; a half whose probe fails is recorded `unsupported` and the matrix row updated; OMP fires `session_switch` into the running extension when a session changes (`new` observed; `resume` and `fork` per the OMP 18.18.3 source), so the extension re-runs the session-start verb on that event and clears the previous session's per-session state; `/move` fires no extension event, so the extension remembers the directory its context was loaded for and, when `before_agent_start` carries a different `ctx.cwd`, reloads exactly as `session_switch` does (proved at startup level in rpc mode: the turn after `/move` carries the moved-to repository's context and none of the first's); a wiki card published as `<repo>/.omp/rules/atomic-wiki-<domain>.md` appears in OMP's `<domain-rules>` listing with its globs and description at startup level (P-43 closed; loading the body is the model's `rule://` call and needs a credentialed run). Each claim records its evidence level: native files, startup (the harness's listing, hook output, or init event), or a model turn backed by a transcript. The isolated homes hold no credential, so startup level satisfies the override and session-context claims, and model-level proof is left to a credentialed run |
| CP18A1 | Add the Claude steering-mode key, pair writers, and conversion | `config/config.go` user `[steering]` table, `config/repo.go` `RepoConfig`, `bundlespec/bundlespec.go` `ScopeSteering`, `harness/claude/steering.go`, `wiki/init.go`, `wiki/action.go`, `wiki/bucket_registry.go`, `cmd/atomic/cmd_steering.go`, `cmd/atomic/cmd_repo.go`, `cmd/atomic/cmd_update.go`, a steering-mode check in `doctor/`, `docs/reference/atomic-toml.md`, `docs/guides/install.md`, tests | atomic-implementer (mode: feature) | ~15 | Phase P11; depends on CP16. `[steering] claude` accepts `loader`, `agents-md`, and `claude-md`; unset resolves to `loader`, the repository or realm root `<state-root>/atomic.toml` value outranks `~/.atomic/config.toml`, and an unknown value fails the load with a named error the way the other enumerated keys fail. Fixtures for the repository root, `docs/wiki/`, realm root, and realm `wiki/` pairs show `loader` writing `AGENTS.md` plus the thin `@AGENTS.md` loader, `agents-md` writing `AGENTS.md` only, and `claude-md` writing `CLAUDE.md` only with the managed block inline; the global `~/.claude/CLAUDE.md` bytes are identical across the three modes. Switching the mode and running `atomic repo init` or `atomic update` converts the pairs of the repository or realm at the working directory only, and user text in either file survives byte-for-byte; converting to `claude-md` deletes an `AGENTS.md` Atomic owns whole and keeps one that carries user text, with only the block removed. Doctor warns for `agents-md` beside a sibling `CLAUDE.md`, `.claude/CLAUDE.md`, or `CLAUDE.local.md`, for `agents-md` when `claude --version` reports a version older than 2.1.277, and for `claude-md` while OMP or Codex is enrolled; a missing `claude` binary or an unparseable version is a PASS with an `info:` note and no version warning. `docs/reference/atomic-toml.md` documents the repository and realm key and `docs/guides/install.md` the user default |
| CP18A2 | Make every steering-pair consumer follow the mode | `context/skills/atomic-wiki/references/repo.md` Step 8, `context/commands/setup-wiki.md`, `context/commands/refresh-wiki.md`, `context/agents/atomic-wiki-inferrer.md`, `doctor/checks_signals.go` `routerRefWired`, `doctor/fix.go` `refs` repair, `harness/claude/migration.go`, `docs/spec/{wiki-deterministic-setup,wiki-buckets,signals-workflow,atomic-doctor,install-workflow}.md`, `docs/reference/{realm-wiki,concepts}.md`, tests | atomic-implementer (mode: feature) | ~13 | Phase P11; depends on CP18A1. In each mode, the wiki pipeline's Step 8 wires `@docs/wiki/index.md` into a file the mode makes Claude load: the shared `AGENTS.md` behind its loader in `loader` mode, `AGENTS.md` with no loader written in `agents-md`, and `CLAUDE.md` in `claude-md`, with the project-local override file still preferred when it exists; setup-wiki, refresh-wiki, and the inferrer name that file. `routerRefWired` finds the reference in `AGENTS.md` as well as `CLAUDE.md`, so an `agents-md` repository wired in `AGENTS.md` passes; the `refs` repair writes to the mode's file. Legacy guidance migration in `harness/claude/migration.go` writes pairs in the mode's shape. The listed specs and references describe the mode, each spec with a Change log entry |
| CP18B | Deliver OMP wiki cards as committed per-file symlinks | `wiki/refresh.go` canonical card frontmatter, `harness/omp/rules.go` (`projectCardBytes`, `PublishProjectCards`, `retireLegacyCards`), `cmd/atomic/cmd_cards.go` `convergeRepoCards`, `cmd/atomic/cmd_hooks.go`, `cmd/atomic/cmd_update.go`, `config/repostate_migrate.go`, `signals/tree.go`, `context/_partials/{signals-gate,loop-finalize}.md`, `context/skills/atomic-wiki/references/repo.md`, `docs/reference/{repo-wiki,concepts}.md`, `docs/research/harness-capability-matrix.md`, tests | atomic-implementer (mode: feature) | ~15 | Phase P11; depends on CP18A2. Each canonical card carries `description` (first non-blank body line, heading marks stripped; `Atomic wiki card for <domain>` for an empty body) and `globs` equal to its `paths:`. With OMP enrolled and the state root inside the repository, refresh and card publish write `.omp/rules/atomic-wiki-<domain>.md -> ../../<state-root>/rules/wiki/<domain>.md`, ledger-owned and unignored; an ignored link is reported after the same `git check-ignore -v` probe the Claude link uses; a foreign file at that name is a named conflict. A link with the expected target and no ledger row is adopted, not a conflict. A link whose domain lost its card is pruned, and without a ledger row only when its target matches the `../../<state-root>/rules/wiki/<name>.md` pattern and dangles. Target uninstall reports tracked links and deletes none. `atomic update` turns a ledger-matching generated flat copy into the link, and a conflicting file is reported before any mutation and kept. The `atomic hooks session-start` verb writes no card file under any harness. A state move relinks and stages the links. The signals scan still skips `.omp/rules/atomic-wiki-`. The `signals-gate` and `loop-finalize` partials and the repo wiki pipeline stage each `.omp/rules/atomic-wiki-*.md` entry that is a symlink and never a regular file. A state root outside the repository keeps the generated flat copy for that case only, as the Claude adapter does, left untracked and never staged. The OMP wiki-card matrix row records the linked listing. A fixture repository on omp 18.18.3 lists the linked card in `<domain-rules>` under the link's filename |
| CP18C | Deliver wiki cards to Codex at edit time | `harness/codex/hooks.go`, `harness/codex/enroll.go`, `hooks/codex.go` and a pre-tool-use handler in `hooks/`, `cmd/atomic/cmd_hooks.go`, `cliusage/cliusage.go`, `harness/capability.go`, `doctor/checks_codex.go`, `docs/research/harness-capability-matrix.md`, tests | atomic-implementer (mode: feature) | ~10 | Phase P11; depends on CP18B. Enrollment writes a `PreToolUse` member with matcher `apply_patch` and command `atomic hooks pre-tool-use` into `$CODEX_HOME/hooks.json`, journaled and ledger-owned like the `SessionStart` row, with every other byte preserved; target uninstall strips only that member, and doctor reports its trust state from the record Codex keeps for that member, located from the member's event and position rather than a fixed `0:0` key. Given a hook payload whose `tool_input.command` carries `*** Add File:`, `*** Update File:`, and `*** Move to:` headers, the verb resolves the repository from `cwd`, matches each repository-relative path against each card's `paths:`, and prints the matching bodies as `hookSpecificOutput.additionalContext`; a second matching edit in the same `session_id` returns no card already sent; a test proves that a write drops the sent-card entries of a session last seen more than 7 days earlier and keeps one seen within 7 days. A malformed payload, a missing repository, or an unreadable card exits 0 with no output. `atomic harness rules status` reports the Codex wiki-card tier as `hook-required` once that evidence is recorded and `unsupported` until then. In an isolated `CODEX_HOME` on codex-cli 0.147.0, Codex accepts the hook configuration and the hook emits `additionalContext` for an `apply_patch` payload; with that startup-level evidence recorded, the Codex wiki-card capability row reads `partial (edits only)` in `harness/capability.go` and in the matrix, and without it the row stays `unsupported` |


## Risks


| Risk | Severity | Likelihood | Mitigation |
|------|----------|------------|------------|
| Harness behavior differs from accepted file syntax | high | high | CP0, CP8A, and CP8B use observable runtime scenarios for discovery, path extraction, precedence, child delivery, hooks, trust, and session behavior |
| Instruction migration changes or deletes user guidance | high | med | Move only known Atomic-owned content; preserve unowned prose; block ambiguous destination, duplicate applicability, malformed markers, and changed relative references |
| Existing legacy or partial state is misclassified | high | med | CP1D uses all legacy/v2 evidence inputs, real prior-version fixtures, seven explicit states, selected-generation/block verification only, read-only planning, and per-resource consent for older or ambiguous artifacts |
| Concurrent lifecycle operations or stale journals race | high | low | Install, update, repair, adoption, uninstall, and ledger-managed `doctor --fix` acquire one advisory lifecycle lock, recover oldest-first, and re-observe after locking before any plan can mutate |
| A corrupt legacy snapshot creates false rollback confidence | high | low | Preserve it; `atomic harness adopt claude` requires `--acknowledge-snapshot`, while the automatic adoption in `atomic update` and a detecting `atomic install` accepts it; never claim restoration evidence that cannot be verified |
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
| State migration loses or strands user state | high | med | Move only the named Atomic entries; `git mv` tracked files so history follows; stop and keep resolving the destination when both roots are populated; never commit; print the staged renames; delete only the regenerable code index; write no `state-location.json`, so linked worktrees on unmigrated branches keep resolving their own root |
| A process keeps resolving the pre-move root | med | med | The migration invalidates the resolution cache before its verb continues; only `atomic state migrate`, `atomic repo init`, and `atomic update` migrate; CP11 tests `atomic repo init` migrating and then writing |
| A nested harness is misidentified | med | med | Detection reports `ambiguous` when two harnesses carry strong markers and never guesses; hook registrations pass `--harness`; writing verbs refuse on `ambiguous` or `none` |
| Harness environment variables mask an agent override | med | med | `CLAUDE_CODE_EFFORT_LEVEL` outranks a Claude agent file's effort and `CLAUDE_CODE_SUBAGENT_MODEL` fills an empty model; doctor names a set variable beside a stored override, and the review gate passes no model argument |
| Codex hook delivery stays unverified | high | high | Never fabricate trust; doctor reports trust state; Codex session context reads `unsupported` until a runtime probe observes it, and the steering fallback in `docs/spec/workflow-preferences.md` covers an untrusted hook |
| A bare `atomic install` enrolls a harness the user did not intend | low | med | Installed-harness discovery reads only native roots that already exist (and Codex only through a set `CODEX_HOME`); the `detected:` line prints before any plan; `--harness` narrows the set and `--dry-run` previews it |
| A pre-migration branch checked out in the same worktree restores tracked `.claude/project/` beside a migrated `.atomic` | med | high | The ignored `.atomic/.scratchpad/` keeps `.atomic` populated, so verbs on that branch read and write `.atomic/` instead of the branch's tracked `.claude/project/`, and the leftover warning names the `.claude` entries on every run; resolution never refuses for this case; `atomic state migrate` moves nothing over a populated destination |
| Automatic adoption replaces an Atomic artifact the user edited | med | low | It replaces only bytes the pre-ledger update would also have replaced; it stops on every blocker involving user content (steering proposals, ambiguous blocks, v2 overlap, journals); each replaced resource gets a recovery copy under `~/.atomic/backups/<operation>/`; the legacy pre-install snapshot is preserved; `atomic harness adopt claude --leave-unowned` stays available before the first update, and the files it keeps stay kept through every later update; an enrolled root is never re-adopted, so after adoption only bytes the ledger recorded Atomic writing are replaced without a decision |
| An agent override is silently inert before delivery | med | high | Doctor category 9 warns for an override on an unenrolled harness or an undelivered agent; agent delivery reads `unsupported` until its probe passes |
| `agents-md` steering is silently skipped because a `CLAUDE.md` sits beside it | med | med | Doctor warns for a sibling `CLAUDE.md`, `.claude/CLAUDE.md`, or `CLAUDE.local.md` and for Claude Code older than 2.1.277; `loader` stays the default |
| `claude-md` steering hides Atomic guidance from OMP or Codex | med | low | Doctor warns when `claude-md` is set while OMP or Codex is enrolled |
| A committed OMP card link dangles in a clone whose cards moved or were removed | low | med | Refresh prunes links whose card is gone and relinks after a state move; links stage with the cards, so one commit carries both |
| The Codex card hook breaks or slows an edit | high | low | Any error exits 0 with no output; each card is sent once per session; the hook runs only after the user trusts it |


## Change log


### 2026-11-12 — Steering mode and card delivery: audit corrections

**What changed:** The body now states what was built. Doctor's steering check reports a missing or unparseable `claude --version` as PASS with an `info:` detail, not a separate INFO severity. A conversion keeps a block-only file that has no ledger row and reports a note. The root pair converts with or without an `<atomic>` block in its `AGENTS.md`, and a wiki pair is Atomic's when its bytes equal the scaffold, since wiki pairs are not ledgered. The `<code-index>` block follows the mode and moves with a mode change. The stub beside it is file-neutral. `atomic wiki scan` falls back to the loader shape only on an unparseable `atomic.toml` and only creates. The Codex hook also reads `*** Move to:`. Change-tree and checkpoint rows name the files that exist: `harness/claude/steering.go`, `wiki/init.go`, and `cmd/atomic/cmd_steering.go` for the modes (`LoaderPair` stays loader-shaped), `config/repostate_migrate.go`, and `doctor/checks_codex.go`.

**Why:** An audit found the body describing a plan the code no longer followed, and two writers that ignored the mode.

**Superseded:** an INFO line from doctor; deletion of a block-only `AGENTS.md` without a ledger row; a mode-shaped `LoaderPair`.


### 2026-11-12 — Update, install, and journal follow-ups

**What changed:** The adoption announcement reads `adopting existing Claude install at <root>` with no "(one time)". `atomic install` asks for consent per adoption, through the adoption's own prompt, which names the replaced artifacts and the recovery-copy directory, and declining one target leaves the others to their own prompts. A failed adoption-marker write after the ledger commit warns that the root is adopted and the record is unwritten; update continues into convergence, exits 1 only if that fails, and the next update writes the marker. Seeded user state waits for the adoption's approval. Install-scope migrations run and write `[install].version` only when a legacy record is present. `atomic harness status` moves a target recorded `converged` to `conflicted` when one of its resources reads `conflict` or its plan reports blockers. The Codex hooks.json row is committed alone before the write, with its target record on a first enrollment, and both are removed again when the write is skipped or fails; `--replace` rewrites a member whose recorded canonical row no longer matches. The transaction journal carries each target's whole left-unowned set, and recovery replaces the committed targets' sets with it; a journal without the set leaves the ledger's alone. The legacy-index removal holds the spawn lock through `RemoveAll`.

**Why:** Follow-up batch from review: wording that read wrongly on every blocked update, a prompt that cost an extra plan per target, and three crash windows that lost a claim or a decision.

**Superseded:** one consent prompt covering every adoption and target plan on `atomic install`; the "(one time)" suffix.



### 2026-11-12 — Repository state ignores the doc-surfaces cache

**What changed:** The repository-state `.gitignore` criterion and the migration flow's managed rules add `/project/doc-surfaces.md`.

**Why:** `atomic docs scan` regenerates that cache with a timestamp on every run, so a tracked copy churns every commit. Contract: `docs/spec/repo-init.md`.

**Superseded:** the managed rules were `/.scratchpad/`, `/.atomic-index/`, and `/worktrees/` only.


### 2026-11-11 — Capture section goes to the realm file Claude loads

**What changed:** The CP14C row states the `bucket add` target in order: the realm file that already holds `## Capture surfaces`, else a `CLAUDE.md` that does not import `AGENTS.md`, else `AGENTS.md` with the loader pair when `CLAUDE.md` is absent. A section kept in `AGENTS.md` behind a `CLAUDE.md` that does not import it gets a stderr warning naming that file.

**Why:** A realm whose `CLAUDE.md` predates the steering pair would otherwise gain a second capture section in an `AGENTS.md` Claude never loads. Contract: `docs/spec/wiki-buckets.md`.

**Superseded:** `bucket add` wrote the capture section into the shared `<realm>/AGENTS.md` in every realm.

### 2026-11-11 — Shared consumers are matched by physical path, not by rewritten ids

**What changed:** The CP14B1 row, the `harness/uninstall.go` change-tree note, and its outline entry now state that removal counts a consumer as shared only when its row names the same id at the same physical applied path, with aliased roots resolving to one. Ledger rows keep their recorded ids.

**Why:** Rewriting Claude ids to root-qualified form needed a ledger schema bump and a migration of every existing row, while the false sharing comes only from the removal check comparing ids alone. Comparing physical paths also keeps two targets over one symlinked root sharing their files.

**Superseded:** Claude resource ids were to be qualified by native root.

### 2026-11-11 — Claude leave-unowned decisions persist; enrolled roots are never re-adopted

**What changed:** The success criterion on ownership evidence, flow step 4, the CP17B row, and the automatic-adoption risk row now state that a leave-unowned decision on any Claude whole-file artifact is recorded per (target, path) in the ledger and honored by `claude install|update`, `harness repair`, `atomic update`, doctor category 1, and `harness status` until `--replace`, deletion, a matching file, the artifact leaving the corpus, or uninstall. The batched refusal names `--replace` and `--leave-unowned`. An enrolled root is never re-adopted from the home-wide `[install]` `adopted` mark; bytes an older binary wrote over it wait for the batched decision.

**Why:** `docs/research/harness-parity-audit.md` rows P-5 and P-6. A leave-unowned adoption stranded every later converge, and the home-wide mark is cleared by any legacy write, including one into an unrelated project.

**Superseded:** The leave-unowned record covered agent files only, so a Claude adoption's decision lasted one operation.

### 2026-11-10 — Federation for a `.claude` realm stays in `.atomic`

**What changed:** a realm whose state root resolves to `.claude` writes `code.toml`, member databases, and the `/*.db` ignore rule to `<realm>/.atomic/`, not `.claude/`. The tree row and the realm federation criterion state the exception. An existing legacy member database is still updated in place by `atomic code sync`.

**Why:** older realms keep their marker in `.claude`, so the candidate rung answers `.claude`. Federation written there would be orphaned when `atomic state migrate` moves the marker, because migration leaves federation files in place under an `.atomic` destination.

**Superseded:** federation files lived in the resolved realm state root in every case.


### 2026-11-10 — CP10C3 covers the retired-setting tests

**What changed:** CP10C3 adds `config/harness_retired_test.go`, which CP17A created after the fixture rows were written; its fixtures depended on the built-in default and no row listed them.

**Why:** every fixture that depends on the default must be independent before CP10A flips it.


### 2026-11-10 — Retire `ATOMIC_HARNESS` and Pi; automatic adoption and install; card link; one build

**What changed:**

- **Retirements.** `ATOMIC_HARNESS` has no meaning: no verb refuses on it, detection reads no orientation value, and the `explicit` confidence is gone. `.pi` is no longer a state candidate or migration source. `harness.dir` is retired: no config write emits `[harness] dir`, a stored one loads without a warning and drops on the next write, and doctor reports nothing for it. `[pi.agents]` and `atomic config resolve` are removed (CP17E). Candidates are `.atomic`, the configured `state.dir` path, and `.claude`.
- **Automatic adoption and install.** `atomic update` adopts an unenrolled legacy Claude install once with the replace and snapshot-acknowledge defaults and stops with exit 1 only on a blocker involving user content. `atomic install` with neither `--harness` nor `--all` installs into every detected harness, falling back to Claude, and prints a `detected:` line.
- **Two populated roots.** A populated destination (`state.dir`, else `.atomic`) answers and a populated `.claude` beside it draws one warning naming its leftover entries; only two populated non-destination candidates refuse.
- **Migration trigger.** Only `atomic state migrate`, `atomic repo init`, and `atomic update` migrate; every other verb, `atomic workflow set` included, reads the populated legacy root in place. `git mv` stages tracked renames, and nothing commits. `ATOMIC_STATE_DIR` is never a migration destination.
- **Wiki cards.** Claude reads the canonical cards through one committed, ledger-owned relative symlink `<repo>/.claude/rules/atomic -> ../../<state-root>/rules`, with a generated copy only for a state root outside the repository and no link when the state root is `.claude`. The subdirectory-session limit of Claude's symlink handling is recorded. OMP keeps one generated flat file per card at `<repo>/.omp/rules/atomic-wiki-<domain>.md`, listed by OMP and not ignored, which session start, install, update, migrate, and refresh republish.
- **Review round.** The automatic adoption triggers on an unenrolled legacy Claude root whatever else is enrolled, also runs for `atomic install --harness claude`, yields to an explicit `--replace` or `--leave-unowned`, and keeps a recovery copy of each replaced resource. Install's root-existence check is named installed-harness discovery, and OMP discovery reads the `OMP_PROFILE` profile, else the default. The Claude link is adopted whenever its target matches, ledger row or not; an ignored link gets a negation inside the repository or a report for a global excludes file; install and update create it only in the current working directory's repository; target uninstall reports it and never deletes it. Wiki-card native names become `atomic/wiki/<domain>`, the OMP copy lands under `.omp/rules/atomic/wiki/`, and CP11 removes matching `rules/atomic-wiki/` trees. Staged renames include the destination `.gitignore`; a leftover legacy index is removed by a later migrate; the both-populated warning prints once. The subdirectory-session limit goes to CP16 (capability row) and CP11 (doctor). The design doc and the parity audit carry the same decisions through CP17D.
- **Final review.** Recovery copies belong to the automatic-adoption defaults on every path. A `.claude` holding only `.atomic-index` is exempt from the both-populated refusal. `ProjectCards.Dir` moves to `.omp/rules/atomic/wiki`, matching the Claude view. The audit restates P-14 for a root the selection record or `state.dir` chose.
- **Smaller fixes.** An unflagged `atomic hooks session-start` behaves as `--harness claude`. Config writes keep modeled tables and drop unknown keys. The corrupt-snapshot risk row now matches automatic adoption. The cached-resolver `ATOMIC_STATE_DIR` refusal moved from CP14A to CP10A. CP10C8 isolates `TestCP8AStateRootPersistenceAndRollback` from the live `~/.atomic`. CP16 runs the installed binaries (Claude Code 2.1.322, omp 18.18.3, codex-cli 0.147.0) in isolated homes and re-records the matrix rows.
- **Rows and order.** New phase P0 holds CP17A, CP17E, CP17B, CP17C, and CP17D. Every row ships in this build, in the serial order the Checkpoints section lists, and each dependency resolves backward in that order. CP9A and CP10A depend on CP17A. CP11 depends on WP3 but wires no `workflow set` trigger. CP14A no longer closes P-11, which CP17A closes. Three risk rows cover the detecting install, the automatic adoption, and a pre-migration checkout.

- **One-time trigger.** The update adoption keys on the legacy `[install]` record and an `adopted = true` marker that an adoption of a present record writes and a fresh legacy install clears, not on any legacy evidence: adoption's own backups and steering block would re-trigger it after a target uninstall. An unreadable ledger makes update adopt nothing and warn.

**Why:** existing users must upgrade without manual steps, and there are no Pi users. A pre-migration branch checkout restores tracked `.claude/project/`, which must not block resolution. A copied card projection duplicated committed cards, and a symlink keeps one source.

**Superseded:**

- Any `ATOMIC_HARNESS` value made a CLI verb exit 2, and CP9A planned an `explicit` orientation. Candidates included a stored `harness.dir` and `.pi`, and doctor was to warn that `state.dir` supersedes `harness.dir`.
- `atomic update` on an unenrolled legacy Claude root exited 1 asking for `atomic harness adopt claude`. `atomic install` without `--harness` or `--all` exited 2. The audit closed P-11 and both `statelocation.go` review findings with CP14A.
- Two populated candidates always refused. Mutating verbs (`atomic code index`, `atomic scratchpad new`, `atomic followups add`, wiki verbs, `atomic workflow set`) triggered the migration, `ATOMIC_STATE_DIR` could be its destination, and renames were described as unstaged.
- Claude read cards from a ledger-owned `<repo>/.claude/rules/atomic-wiki/` copy ignored by `/rules/atomic-wiki/`, OMP read `<repo>/.omp/rules/atomic-wiki/`, and `NativeName()` returned `atomic-wiki/<domain>` for a wiki card.
- The agent-override flow claimed config writes preserve unknown keys.

**Removed:** the `ATOMIC_HARNESS` criterion, the `ParseOrientation`, orientation-gate, `LegacyHarnessDir`, and `ResolvePiAgents` outline entries, the `explicit` detection step, the "Pi agent configuration remains independent" clauses, and the `.claude/rules/atomic-wiki/` projection, because nothing reads Pi, `ATOMIC_HARNESS`, or a copied Claude card projection any more.

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


### 2026-11-10 — Legacy installation adoption writes no state record

**What changed:** legacy installation adoption no longer persists a repository-state selection record; only `atomic state adopt` does. CP11 removes the `config.AdoptStateRoot` call from `installstate/legacy.go`. A lone populated `.claude` resolves through the candidate rung, so the next `atomic update` or `atomic repo init` migrates it to `.atomic`.

**Why:** with `.atomic` as the default, adoption recorded the legacy root, and a record naming the legacy root stops migration. Every existing user would have stayed on `.claude`. The candidate rung already returns the same root with no record, so the record added nothing but the pin.

**Superseded:** legacy installation adoption given a repository root persisted and fsynced the selection record.


### 2026-11-10 — Migration mechanics, daemon liveness, and two refusals

**What changed:** tracked files are staged as `git mv` would stage them, not necessarily through `git mv` itself, and a gitlink or unmerged entry is refused. A failure partway reverses the moves already made. The legacy index is removed only when no MCP daemon is live there: no daemon answers on its socket and no starter holds the spawn lock. A legacy root holding only `.atomic-index` is not migrated and keeps resolving in place. A repository root equal to the user's home directory is refused.

**Why:** CP11a review. `git mv` cannot follow a cross-device copy. A running daemon holds no lock; only a starter holds it while spawning. Without a rollback, a half-finished move left both roots populated and every later run refused. An index-only migration staged a new `.gitignore` in repositories with no tracked Atomic state. Run from a home directory that is a Git work tree, the migration would treat `~/.claude` as the legacy root.

**Correction:** the body said tracked files move "with `git mv`" and that a running daemon holds the lock.


### 2026-11-11 — Left-unowned agent files persist

**What changed:** a leave-unowned decision on an agent file is recorded in the ledger per (target, path). Later operations that carry no decision honor it. The record clears when the file vanishes or matches the selected bytes, when `--replace` is given, when the agent leaves the corpus, or when the target is uninstalled. The ledger outline and the `installstate/` change-tree note name the record.

**Why:** CP12B review. Without a record, the decision lasted one operation, and every later plain repair or automatic rules sync refused the whole target over the user's own file.


### 2026-11-11 — OMP session_switch limit (P-62)

**What changed:** CP16 now probes whether OMP's `session_switch` and `/move` reach the extension and closes P-62, either with a handler that re-runs the session-start verb on switch or with the limit recorded.

**Why:** CP13A review. OMP fires `session_start` only on initial load. An in-process `/new`, `/resume`, `/fork`, or `/move` keeps the first directory's context, and no capability row proves `session_switch`.


### 2026-11-11 — CP10D4 page coverage

**What changed:** CP10D4's criterion now reads per page by relevance. Every page states the `.atomic` default and links to the ladder in `docs/reference/concepts.md`. A page states migrate and the worktree locations where it discusses repository state or worktrees. A page about one subsystem (code-intel, repl, repo-wiki, realm-wiki, commands, code-intel-mcp) states the default and links to the concepts ladder.

**Why:** CP10D4 review. The documentation-surfaces ownership rule in the root `CLAUDE.md` gives the resolution order to `concepts.md` and has siblings point to it, so restating the ladder or worktree placement on every subsystem page would create the duplicate copies that the rule forbids.

**Superseded:** the CP10D4 criterion required every page to state the `.atomic` default, the ladder, `atomic state migrate`, and the worktree locations.


### 2026-11-11 — Native vocabulary home and listener deadline

**What changed:** the Claude native-vocabulary paragraph lives in `artifacts/renderer.go` as `ClaudeSteering`. The Claude adapter's `GlobalArtifacts` and `claudeinstall` both render global steering through it, so doctor's drift check and uninstall read adapter-written bytes as Atomic's own. The paragraph names the background-listener tool with `timeout_ms`, capped at 30 minutes, and its stop tool; re-arming on expiry is policy, so `atomic-bus` states it and the paragraph does not. The CP15A4 Files cell lists the files the row touches. CP15A5 also carries `context/commands/retrospective-learning.md`, which still names `AskUserQuestion` and `subagent_type`.

**Why:** CP15A4 implementation and review. `harness/claude` imports `claudeinstall`, so a paragraph defined there cannot be shared with the legacy installer. The Claude Code 2.1.322 `Monitor` schema takes `command` or `ws`, `description`, and `timeout_ms`; it has no `persistent` parameter, defaults to 300000 ms, and caps deadlines at 1800000 ms with an expiry notice that permits a re-arm.

**Correction:** the body placed `NativeVocabulary` under `harness/claude` and named a persistent background-listener tool with no deadline.


### 2026-11-11 — OMP artifact kinds probed on 18.18.3

**What changed:** the profile `AGENTS.md` block keeps the steering body's whole-line `@` imports. Enrollment writes commands and skills to `<profile agent root>/commands/` and `skills/` as ledger-owned files while their capability rows are supported. Retiring or uninstalling one removes the directories it emptied below the profile root; the target-uninstall success criterion and its flow step 3 state that climb for every target (`docs/spec/uninstall.md`). Matched rule bodies and output-style selection stay unsupported and read `WARN unsupported` in enrollment, status, and rules status for an OMP target only, and doctor categories 18 and 19 WARN on an enrolled OMP profile. The probe record is `docs/research/harness-capability-matrix.md`, update 2026-11-11.

**Why:** CP15B1. `TestOMPNativeKindDelivery` observed omp 18.18.3 expand both import forms, expand a profile command with its arguments, and list a profile skill.

**Superseded:** the OMP profile `AGENTS.md` carried an import-free steering body, and a removal pruned one parent directory.

### 2026-11-11 — Codex artifact kinds probed on codex-cli 0.147.0

**What changed:** the Codex adapter writes a ledger-owned managed block into `$CODEX_HOME/AGENTS.md` carrying the steering body, stripped of its whole-line `@` imports since Codex leaves them literal, and the output style; `atomic wiki` projects `<wikis>` into it for an enrolled Codex target. Convergence calls `Register` after enrollment, and the plan's consent names the `config.toml` edit it makes; `Register` reads the plugin list before any mutation and undoes the entries it added when any step after `marketplace add` fails. Plan and convergence share one registration check, and a current registration refreshes its recorded cache digest. A plugin whose entry and cache the user removed is not re-added without `--replace`. Target uninstall's post-removal step calls `DefaultUnregister` when it removed the recorded plugin cache, now keyed by its path; `DefaultUnregister` removes only the entries Codex records, and an unregister that cannot run ends the enrollment with a notice naming the manual commands. A registration convergence cannot run, including an unreadable `config.toml`, reads `WARN unsupported` with its fix, and `atomic install` reports a target applied only when its adapter wrote something. Commands, `@` imports, output-style selection, rule bodies, and wiki pointer cards read `WARN unsupported` for a Codex target, doctor category 24 prints a fix per unsupported surface and WARNs on an enrolled home, and categories 18 and 19 WARN on an enrolled Codex target. The probe record is `docs/research/harness-capability-matrix.md`, update 2026-11-11.

**Why:** CP15B2. `TestCodexNativeKindDelivery` observed, through `codex debug prompt-input`, that codex-cli 0.147.0 reads `$CODEX_HOME/AGENTS.md`, lists a registered plugin's skills, and leaves commands and `@` lines literal.

**Superseded:** Codex had no global steering, enrollment never registered the plugin, and Codex gaps printed without WARN or a fix.

### 2026-11-11 — Discard plan printed and approved under --yes

**What changed:** with `--discard-changed`, the uninstall plan names every resource it will discard, by id and path, as a `notice` line printed before anything is applied and carried in the dry-run plan's `notices`. Under `--yes` that printed plan is the approved plan: the apply step refuses the whole removal, non-zero and with nothing discarded, when it would discard a resource the plan did not name, and a full uninstall checks every target before removing the first. Each discarded resource is first copied under `~/.atomic/backups/discard-<stamp>/<target>/`. The uninstall flow gains this as step 4; the CP14A row states it.

**Why:** CP14A. The review found `--discard-changed --yes` deleted every user-edited resource without naming it and kept no copy. A new flag naming the approved resources was not adopted because it adds an unconfirmed public contract.

**Superseded:** CP14A required the approved plan to name every discard without saying how a scripted caller supplies it.

### 2026-11-11 — CP16 runtime findings

**What changed:** the subdirectory-session criterion states what Claude Code 2.1.322 does in print mode (no prompt, the cards stay unloaded, a regular rule file still loads) and that a loaded card is reported at its resolved state-root path. The OMP card criterion states that OMP 18.18.3 lists only flat `.omp/rules/*.md` files with a `description`, so the nested copy does not load and P-43 stays open. The CP16 row records the `session_switch` handler, the `/move` limit, and the three evidence levels, with startup level accepted for the override and session-context claims when the isolated homes hold no credential. The Codex session-context criterion names the verified trust key and maps Codex's states: a stale hash reads `modified` with a re-trust fix line, and a record under other keys only reads `untrusted`. **Correction:** the card criterion said Codex reads the cards through the matcher index; `harness/codex/gaps.go` reports wiki pointer cards as an `unsupported` static-scope gap, and the criterion now says so. The runtime-proof success criterion and the CP16 row's evidence sentence state the startup-level bar, with model-level proof left to a credentialed run. Both state the agent-override claim at startup level too: the override projected into the native agent file and the agent listed at startup.

**Why:** CP16 ran the real Claude Code 2.1.322, omp 18.18.3, and codex-cli 0.147.0 against isolated homes. No run had a credential, and OMP's rule listing ignored the nested copy.

**Superseded:** CP16 closed P-62 with either a handler or a recorded limit, and required model-context proof for the override and session-context claims. Doctor read both trust shapes as `unknown` and called the key shape unverified.

### 2026-11-12 — OMP flat wiki cards and /move reload (P-43)

**What changed:** the OMP card copy is one flat file per card at `<repo>/.omp/rules/atomic-wiki-<domain>.md`, with `description` and `globs` frontmatter, written without a `.omp/.gitignore` rule; a foreign file there is a named conflict, a dropped card's file is removed when its ledger row matches, and a ledger-owned nested `.omp/rules/atomic/wiki/` copy is removed with its ignore line (kept and reported when the row differs). The OMP extension reloads the session context when `before_agent_start` carries a `ctx.cwd` that differs from the loaded one, which covers `/move`. The criteria, the CP11 and CP16 rows, the change tree, and the card-delivery references say so.

**Why:** the P-43 rerun on real omp 18.18.3 listed the flat card in `<domain-rules>` and the `/move` turn carried the moved-to repository's context. OMP's rule scan uses the native glob with `gitignore: true`, so an ignored card is never listed; both `.omp/.gitignore` and `.git/info/exclude` hid it, which is why no ignore rule is written.

**Superseded:** the OMP copy was a nested tree at `.omp/rules/atomic/wiki/<domain>.md` with `paths:` only, ignored by `/rules/atomic/`, which OMP neither listed nor loaded (P-43 open); `/move` was a recorded limit.


### 2026-11-12 — Claude steering modes, OMP card links, Codex edit-time cards

**What changed:** a new `[steering] claude` key (`loader` default, `agents-md`, `claude-md`), set in `<state-root>/atomic.toml` over `~/.atomic/config.toml`, selects the shape of every repository and realm steering pair; `atomic repo init` and `atomic update` convert pairs on a mode change, doctor warns for the mode combinations Claude, OMP, or Codex would not read, and an unknown value is a loud config error. The global `~/.claude/CLAUDE.md` is unchanged in every mode. OMP reads each wiki card through a committed, ledger-owned relative per-file symlink `.omp/rules/atomic-wiki-<domain>.md -> ../../<state-root>/rules/wiki/<domain>.md`; canonical cards carry `description` and `globs`, ship flows stage the links with the cards, `atomic update` converts the generated copies into links, OMP session start no longer republishes them, and a state root outside the repository keeps the generated copy. Codex receives matching cards at edit time through an owned `PreToolUse` `apply_patch` member running `atomic hooks pre-tool-use`, once per card per session, failing open; its wiki-card tier is `hook-required`, and its capability row reads `partial (edits only)` once startup-level runtime evidence is recorded. Every steering-pair consumer (wiki pipeline wiring, doctor's router-reference check and repair, guidance migration) follows the mode. New criteria, flows, risks, and rows CP18A1, CP18A2, CP18B, and CP18C in phase P11 carry it, and the CP11 and CP13A rows state the link-and-fallback card projection.

**Why:** the user approved the contract in `.atomic/.scratchpad/steering-modes-and-card-links/CONTRACT.md` on 2026-11-12, on this evidence. Claude Code 2.1.277's changelog says that in a project with no `CLAUDE.md` it reads `AGENTS.md` instead, its memory docs say a present `CLAUDE.md`, `.claude/CLAUDE.md`, or `CLAUDE.local.md` stops `AGENTS.md` being read, and an experiment showed `~/.claude/AGENTS.md` ignored. On OMP 18.18.3 the rule glob follows per-file symlinks and lists a linked card by the link filename, OMP reads only `description` and `globs` (`discovery/helpers.ts:228-233`) with `description` required (`capability/rule-buckets.ts`), the gitignore check runs on the link path, and extensions cannot register rules. On Codex 0.147.0 a `PreToolUse` `hookSpecificOutput.additionalContext` reaches the model (`core/src/hook_runtime.rs`), edits arrive as tool `apply_patch` with the raw patch in `tool_input.command`, plugins have no rules component, and non-managed hooks need `/hooks` trust review.

**Superseded:** the flat-copy decision of "OMP flat wiki cards and /move reload (P-43)": OMP cards were generated per-machine flat copies, left untracked and never staged by any verb or ship flow, and republished at OMP session start. Codex reported wiki pointer cards `unsupported`. Thin Claude loaders were the only repository and realm steering shape.

## Implementation log

### implemented — 2026-11-12 (P11)

Built by `/subagent-implementation` from loop base `ca5ae067` (the spec amendment), serially CP18A1, CP18A2, CP18B, CP18C, then one audit iteration:

- `08e2c8a1`: CP18A1. The `[steering] claude` key, mode-shaped pair writers, conversion through the lifecycle journal and ledger, doctor category 26 `steering`.
- `dfdc21f8`: CP18A2. Wiki Step 8, setup-wiki, refresh-wiki, the inferrer, the doctor `refs` repair, and scope migration follow the mode.
- `70ff752a`: CP18B. Canonical cards carry `description` and `globs`; OMP reads them through tracked per-file symlinks.
- `34bb9202`: CP18C. The Codex `apply_patch` PreToolUse member delivers matching cards once per session.
- `875ef647`: audit fixes. Root pairs convert in every scope, loader mode never creates a `CLAUDE.md`, and the realm code-index block moves with the mode.

The audit ran once (CHANGES_REQUESTED, then one iteration reviewed to PASS). No follow-ups were deferred. Codex card delivery is proven at startup level only: Codex lists and hashes the member, and the verb output comes from a constructed payload.

### implemented — 2026-11-12

Built by `/subagent-implementation` from loop base `b83c22e0`, serially from CP17A, then one review round and six follow-up batches. Commits, grouped:

- `96dc848e`, `3c86601f`, `84e7e4fa`, `510f5ad2`, `5bfeac9b`: CP17A–E. Retire `ATOMIC_HARNESS`, `.pi` state, `harness.dir`, `[pi.agents]` and `atomic config resolve`; adopt a legacy Claude install on update; detect installed harnesses on install.
- `b8a2c0cc` through `129eb0c2`: state fixtures made independent of the default root.
- `d543eae3`, `b0bc44be`, `0c25592c`: harness detection, the `.atomic` default and neutral ladder, worktrees keyed by harness.
- `1ec44114`, `0851f2bf`, `896c0e16`, `5a8b0e5a`: workflow preferences (resolve, autodetect, `atomic workflow resolve|set`, session start).
- `07c33a4f`, `4ff4b825`, `14091eed`: the reminder verb, its `where` key and its corpus flows removed.
- `2e9f5c88`, `eadf74e8`, `c3b80272`: `atomic state migrate`, the `.claude/rules/atomic` card link, this repository migrated to `.atomic`.
- `edd62925`, `55c79581`: harness-aware doctor, retro, bus, wiki and config agents; `state_root` and `worktrees` in `atomic where --json`.
- `f7fdf96a`, `6e44dcec`, `a1113fa6`, `306e615a`, `efb605ef`, `a191732b`: source-control and tracker references, worktree placement, follow-ups through the resolved tracker.
- `7f5ee88f`, `e34f2392`, `769cff3f`, `127bfb59`, `23491586`: per-harness agent overrides, OMP and Codex agents, OMP extension session context, the Codex `hooks.json` member.
- `dbcdc29d` through `d585aa95`, `4c2237aa`, `4e702b41`, `87db1c0b`, `088be680`: reminder retirement and `<state-root>` wording across docs, specs and context.
- `9ec2b2ec` through `b0681975`: review-round fixes (left-unowned files, wiki refs, native-tool vocabulary, OMP and Codex delivery, refusal exits, `CLAUDE_CONFIG_DIR`, crash safety, capture surfaces).
- `cd245b9a` through `5677db97`: comment-discipline cuts.
- `a9259533`, `75cecc62`, `b91daaae`: workflow preferences and redesign docs.
- `6d340649`, `2e6d906f`: CP16 runtime proof on Claude Code 2.1.322, omp 18.18.3 and codex-cli 0.147.0; OMP `session_switch` reload.
- `f9731169`: audit fixes.
- `0cb99002`, `b01d4782`, `5cf940cf`, `f508c5dc`, `020892cc`, `6f308ef8`, `b307eeb9`, `45e82478`: follow-up batches (workflow; config and doctor; serve and code-intel; install, harness and recovery; help, installer, specs and tests; OMP flat cards and `/move`).

**Unforeseens:**

- OMP's rule scan skips every file a Git ignore source covers, including `.git/info/exclude`, so the flat OMP cards cannot be ignored and stay untracked.
- OMP `/move` fires no extension event; `before_agent_start` carries the new `ctx.cwd`, compared through `realpath` because `/var` and `/private/var` differ on macOS.
- Claude Code 2.1.322 does not load cards through the link in a subdirectory session in print mode.
- Codex keys hook trust by `<symlink-resolved CODEX_HOME>/hooks.json:session_start:0:0`.

**Follow-up dispositions:** every open ledger item was fixed or closed in this build. F-11, F-27 and F-29 did not reproduce; F-25 was closed because the spec's evidence path is the contract. F-87 and F-89 need a credentialed model turn and stay open; the runtime proof is at startup level for those claims.

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

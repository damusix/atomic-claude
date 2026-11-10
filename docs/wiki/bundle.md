---
type: Domain
description: Expands the committed context/ artifacts into the embedded bundle compiled into the binary, and installs them.
tags: [artifacts, codegen, build]
---

# bundle


## What it does


The markdown artifacts under [`context/`](../../context) (agents, commands, skills, output-styles, rules, [`context/CLAUDE.md`](../../context/CLAUDE.md)) are the product, and this domain is how a user gets them without a manual copy step. `make bundle` reads [`context/`](../../context), expands any `{{ template "<name>" . }}` directive against [`context/_partials/`](../../context/_partials), and writes the result to [`atomic/internal/embedded/bundle/`](../../atomic/internal/embedded/bundle) plus a generated `manifest.go`. `//go:embed bundle` in [`atomic/internal/embedded/bundle.go`](../../atomic/internal/embedded/bundle.go) compiles that tree into the [`atomic`](../../atomic) binary, and `atomic claude install`/`update` writes the embedded copies to a target directory (`~/.claude` by default).

Generation happens on the way into the embedded bundle, not before it. [`context/commands/<verb>.md`](../../context/commands) and [`context/agents/<name>.md`](../../context/agents) are committed source, not generated output. Nothing is ever written back into [`context/`](../../context), so an artifact is never duplicated between source and output.

Both [`atomic/internal/embedded/bundle/`](../../atomic/internal/embedded/bundle) and `manifest.go` are gitignored build outputs. What ships to a user is fully determined by [`context/`](../../context) and the Go packages that mirror and install it.


## How it works


### The pipeline: source to installed file

Every stage from [`context/`](../../context) to the binary is regenerated from the one before, so nothing between them is ever hand-edited.

```mermaid
flowchart TD
    %% source: atomic/internal/bundlemirror/mirror.go, atomic/internal/templaterender/templaterender.go, atomic/internal/embedded/bundle.go, atomic/internal/claudeinstall/install.go
    CTX["context/"]
    PART["context/_partials/*.md"]
    MIR["bundlemirror.Run<br/>(make bundle)"]
    EMB["embedded/bundle/<br/>+ manifest.go"]
    BIN["atomic binary"]
    HOME["~/.claude/"]
    CTX --> MIR
    PART -.->|"commands, agents only"| MIR
    MIR --> EMB
    EMB -->|"go:embed bundle"| BIN
    BIN -->|"atomic claude install"| HOME
```

`bundlemirror` loads [`context/_partials/`](../../context/_partials) once per run.

`make bundle` runs `go run ./internal/tools/bundle-mirror -repo ../ -outdir ./internal/embedded` from [`atomic/`](../../atomic). `test`, `build`, and `vet` in [`atomic/Makefile`](../../atomic/Makefile) all declare `bundle` as a prerequisite, because [`atomic/internal/embedded`](../../atomic/internal/embedded) does not compile until the mirror exists. CI regenerates it the same way through `go generate ./...`, which fires the `//go:generate go run ../tools/bundle-mirror -repo ../../../ -outdir .` directive in [`bundle.go`](../../atomic/internal/embedded/bundle.go); goreleaser's `before` hook does the same before a release build. There is no committed rendered-artifact tree to diff, so [`.githooks/pre-commit`](../../.githooks/pre-commit) carries no render or bundle stage.

Only `command` and `agent` artifacts pass through the template engine (`readArtifact` in [`mirror.go`](../../atomic/internal/bundlemirror/mirror.go) gates on `expandedKinds`); skills, rules, output-styles, and [`context/CLAUDE.md`](../../context/CLAUDE.md) are copied byte-for-byte, so a literal `{{` in their prose is never read as a directive. `readArtifact` hashes the *expanded* bytes for the SHA256 in `manifest.go`, because that is what installs. A parity check has to agree with the file the user ends up with.

### What the bundle includes

Inclusion is decided by pure predicates in [`atomic/internal/bundlespec/bundlespec.go`](../../atomic/internal/bundlespec/bundlespec.go). A new file matching an existing rule is picked up with no Go change; a new artifact *kind* needs a new predicate plus a new walk in `bundlemirror`.

| Kind | Rule |
|------|------|
| agent | `context/agents/atomic-*.md`, files only |
| skill | `context/skills/atomic-*/`, whole subtree, directory must contain `SKILL.md` |
| output-style | `context/output-styles/atomic*.md`, no dash required after [`atomic`](../../atomic) |
| command | `context/commands/**/*.md`, any `.md`, no allowlist, recursive |
| rule | `context/rules/**/*.md` |
| claude-md | [`context/CLAUDE.md`](../../context/CLAUDE.md), exact name |

`context/_partials/*.md` is never itself a bundle target: `templaterender.LoadPartials` reads it as a pool of `{{ define }}` blocks, and `bundlemirror` never walks it as an artifact source.

### Partial composition

Editing a partial reaches further than its own consumers, because partials nest:

```
commit-flow  ──> review-gate, doc-impact, signals-gate
merge-flow   ──> base-resolution, signals-gate, worktree-cleanup-prompt,
                 merge-flow-preflight, merge-flow-steps
squash-flow  ──> base-resolution, review-gate, doc-impact, signals-gate,
                 squash-flow-preflight, squash-flow-steps
agent-implementer-workflow ──> agent-search-tooling, agent-tdd-signals,
                               agent-code-intel, agent-where
```

| Agent source | Partials composed |
|---------------|-------------------|
| `atomic-implementer.md` | `agent-atomic-voice`, `agent-comment-discipline`, `agent-implementer-workflow`, `agent-readability`, `agent-shared-rules`, `agent-signals-output`, `agent-yagni` |
| `atomic-reviewer.md` | `agent-atomic-voice`, `agent-code-intel`, `agent-comment-discipline`, `agent-readability`, `agent-where`, `agent-yagni` |
| `atomic-investigator.md` | `agent-atomic-voice`, `agent-code-intel`, `agent-search-tooling`, `agent-where` |
| `atomic-wiki-inferrer.md` | `agent-atomic-voice`, `agent-code-intel`, `agent-where` |
| `atomic-wiki-writer.md` | `agent-atomic-voice`, `agent-code-intel` |
| `atomic-deslopper.md` | `agent-atomic-voice`, `agent-code-intel`, `agent-comment-discipline`, `agent-search-tooling`, `agent-yagni` |
| `atomic-auditor.md` | `agent-atomic-voice`, `agent-code-intel`, `agent-comment-discipline`, `agent-readability`, `agent-yagni` |
| `atomic-strategist.md` | `agent-atomic-voice`, `agent-yagni` |

| Command source | Partials composed |
|-----------------|-------------------|
| `commit.md` | `commit-flow`, `push-flow`, `pr-flow`, `merge-flow`, `squash-flow`, `git-safety` |
| `subagent-implementation.md`, `implement.md` | `handoff`, `worktree-setup`, `implement-loop`, `loop-finalize` |
| `autopilot.md` | `worktree-setup`, `implement-loop`, `loop-finalize` |
| `quick-fix.md` | `handoff`, `implement-loop`, `loop-finalize` |
| `deslop.md` | `worktree-setup`, `git-safety` |
| `report-issue.md`, `report-issue-with-atomic.md` | `report-issue-privacy` |

Every other command source is self-contained.

### Install verbs

| Verb | Flags |
|------|-------|
| `atomic claude install` | `--dry-run`, `--target`, `--no-hooks` |
| `atomic claude update` | `--dry-run`, `--target`, `--no-hooks` |
| `atomic claude diff` | `--target` |
| `atomic claude list` | none |
| `atomic claude uninstall` | `--target` |

`install` and `update` share a code path, `installOrUpdate` in [`atomic/internal/claudeinstall/install.go`](../../atomic/internal/claudeinstall/install.go).

[`atomic/cmd/atomic/cmd_claude.go`](../../atomic/cmd/atomic/cmd_claude.go)'s `runClaudeInstall` calls `hooks.Install(scopeRoot, scopeRoot)` after `claudeinstall.Install`/`Update` returns, to register the session-start hook. `hooks.Install` returns a `skipped bool` alongside its error; when `skipped` is true (a read-only `settings.json`), `runClaudeInstall` sets `result.HooksError = fmt.Errorf("settings.json is read-only")` instead of reporting success. `printPostInstallHint` prints only after `install`, not `update`, and its next-steps block tells the user to run `/refresh-wiki` in each repo that wants project signals. A failed hook install prints `warning: hook install failed (non-fatal): <err>` and the install still succeeds.

### Install order

Install reads the old `[install.artifacts]` before `Plan` and writes the new list last, so the prune diff compares against the previous install rather than the one in progress. A dry run skips both ends.

```mermaid
flowchart TD
    %% source: atomic/internal/claudeinstall/install.go, atomic/internal/claudeinstall/manifest.go, atomic/internal/claudeinstall/snapshot.go
    D1{"dryRun?"} -->|no| S["writePreInstallSnapshot"]
    S --> O["PruneDiff<br/>(old config.toml)"]
    D1 -->|yes| P["Plan"]
    O --> P
    P --> A["Apply"]
    A --> SEED["seedOutputStyle<br/>ForInstall"]
    SEED --> D2{"dryRun?"}
    D2 -->|no| PR["ensureProfileStub<br/>populateProfile"]
    PR --> W["runPrune<br/>writeInstallManifest"]
```

- **writePreInstallSnapshot**: copies every file the manifest will touch, plus `settings.json`, into `~/.atomic/pre-install/`. A no-op once that directory exists.
- **ensureProfileStub**: runs outside `Apply` on purpose, so a bare `Apply` never creates user data. `populateProfile` recovers from any panic, so profile detection never fails an install.
- **runPrune**: a batched confirm over the stale targets; see stale-artifact pruning below.

### Output-style seeding

Seeding is decided from the manifest, not from disk, so a dry run reports the same seed a real run would make.

```mermaid
flowchart TD
    %% source: atomic/internal/claudeinstall/install.go, atomic/internal/hooks/outputstyle.go
    M{"manifestHasOutputStyle?"} -->|no| N["no seed"]
    M -->|yes| U{"isUserLevelTarget?"}
    U -->|no| N
    U -->|yes| D{"dryRun?"}
    D -->|yes| PL["planOutputStyleSeed"]
    D -->|no| SO["hooks.SeedOutputStyle"]
```

Seeding runs after `Apply` because `hooks.SeedOutputStyle` checks `StyleInstalled` on disk. Under `--dry-run`, `planOutputStyleSeed` checks `hooks.SeedEnabled` and whether `settings.json` already carries the key, without writing. A seed failure (malformed `settings.json`) prints as a non-fatal warning and never aborts the install.

`planOutputStyleSeed` prints `Would seed output style ...`; a real write prints `Seeded output style ...`. Both lines end with the opt-out command, `atomic config set output_style.seed false`.

### The per-artifact install decision

`~/.claude/CLAUDE.md` is never overwritten wholesale, because a user's own content lives in the same file. `planArtifact` routes every changed artifact except [`CLAUDE.md`](../../CLAUDE.md) to a plain overwrite.

```mermaid
flowchart TD
    %% source: atomic/internal/claudeinstall/install.go (planArtifact)
    A["planArtifact"] --> B{"os.IsNotExist?"}
    B -->|yes| I["ActionInstalled"]
    B -->|no| C{"hexSHA256 equal?"}
    C -->|yes| U["ActionUnchanged"]
    C -->|no| D{"a.Target ==<br/>#quot;CLAUDE.md#quot;?"}
    D -->|no| UP["ActionUpdated<br/>back up, then overwrite"]
    D -->|yes| CM["CLAUDE.md block decision"]
```

### The CLAUDE.md block decision

When both sides carry a parseable `<atomic>` block, install swaps only the block; otherwise it hands the merge to a model through a proposed file.

```mermaid
flowchart TD
    %% source: atomic/internal/claudeinstall/install.go (planArtifact), atomic/internal/claudeinstall/atomicblock.go
    E{"extractAtomicBlock ok?<br/>(embedded and disk)"} -->|no| MR["ActionMergeRequired<br/>write proposed file"]
    E -->|yes| F{"blocks equal?"}
    F -->|yes| U["ActionUnchanged"]
    F -->|no| BR["ActionBlockReplaced<br/>back up, swap block"]
```

Backups land in `~/.atomic/backups/<timestamp>/<target>`, one timestamp directory per run. The proposed file lands at `~/.atomic/proposed/CLAUDE.md`, and the install output tells the user to run `atomic prompt claude-merge` in a Claude Code session. If the `<atomic>` block vanishes between plan and apply, `applyAction` fails loud with `"<path> lost its parseable <atomic> block between plan and apply"` rather than guessing the boundary.

`Plan`, `Apply`, and `Diff` all load the `[claude.agents]` overrides and patch `model:` and `effort:` frontmatter before hashing (`Plan`, `Diff`) or writing (`Apply`), so all three agree on the expected on-disk bytes. `patchAgentContent` sets `model:` and `effort:` independently and preserves the source order of every other key; a file with no parseable frontmatter is left unchanged.

`ReapplyAgents(targetDir, home)` re-patches only agent files already present on disk. An absent agent is never installed by it.

`Diff` reports each artifact as `match`, `diff`, or `absent`. A [`CLAUDE.md`](../../CLAUDE.md) whose `<atomic>` block equals the bundle's counts as `match` even when the whole file differs, because user content outside the block is expected.

### Stale-artifact pruning and the install manifest

`PruneDiff` returns the targets that left the bundle, and a batched confirm offers to remove them. A non-interactive terminal, or Ctrl+C at the prompt, skips the removal silently. `writeInstallManifest` then rewrites `[install.artifacts]` from the current plan; `claude-md` is never tracked there. Dev builds (`version.Version == "dev"`) leave `Install.Version` untouched, because `dev` is not parseable semver and `config.Validate` would then fail every contributor's `atomic doctor`.


## Where it lives


| Path | Role |
|------|------|
| **Source (edit here)** | |
| `context/commands/*.md` | Source for each slash command. May compose `{{ template "<name>" . }}` directives. |
| `context/agents/*.md` | Source for each subagent definition. Same composition contract as commands. |
| `context/_partials/*.md` | Partials composed by both kinds via `{{ template "<name>" . }}`. A partial defined once is callable from any command or agent source. |
| `context/skills/atomic-*/` | Skill directories, whole subtree. Bundled byte-for-byte, no expansion. |
| `context/output-styles/atomic*.md` | Output style definitions. Bundled byte-for-byte. |
| `context/rules/**/*.md` | Path-scoped topic rules. Bundled byte-for-byte. |
| [`context/CLAUDE.md`](../../context/CLAUDE.md) | The global contract installed as `~/.claude/CLAUDE.md`. Bundled byte-for-byte. |
| **Selection and expansion** | |
| [`atomic/internal/bundlespec/bundlespec.go`](../../atomic/internal/bundlespec/bundlespec.go) | Pure inclusion predicates. Single source of truth, imported directly by `bundlemirror` and `internal/validate`; `manifestcheck` reaches it through `bundlemirror.Enumerate`. |
| [`atomic/internal/templaterender/templaterender.go`](../../atomic/internal/templaterender/templaterender.go) | `text/template` engine. `LoadPartials` pools every `*.md` under [`context/_partials/`](../../context/_partials) in sorted order; `Expand` clones the pool per file, so a `{{ define }}` in one artifact can never leak into the next. A missing partials dir yields an empty pool rather than an error. |
| [`atomic/internal/bundlemirror/mirror.go`](../../atomic/internal/bundlemirror/mirror.go) | Build-time walker. `enumerate` reads each matching file once, expands `command`/`agent` kinds through `templaterender`, and keeps the bytes; `Run` reuses them for the copy. `Enumerate` is `Run` without the disk write, used by `manifestcheck`. Output sorts by kind then target, so the result is deterministic. |
| [`atomic/internal/tools/bundle-mirror/main.go`](../../atomic/internal/tools/bundle-mirror/main.go) | Entrypoint for `go generate` and `make bundle`. Prints `bundle-mirror: wrote N artifacts to <outDir>`, and writes `manifest.go` from an inline template. |
| **Embedded bundle** | |
| [`atomic/internal/embedded/bundle.go`](../../atomic/internal/embedded/bundle.go) | Holds `//go:embed bundle` and the `go:generate` directive that invokes `bundle-mirror`. The only committed file in the package. |
| `atomic/internal/embedded/bundle/**` | Generated, never edit. Mirrored artifacts, written by `make bundle`. |
| [`atomic/internal/embedded/manifest.go`](../../atomic/internal/embedded/manifest.go) | Generated, never edit. `Manifest() []Artifact` allowlist: kind, embedded source path, install target, SHA256. Written by `make bundle`. |
| **Parity check** | |
| [`atomic/internal/manifestcheck/manifestcheck.go`](../../atomic/internal/manifestcheck/manifestcheck.go) | `Compare` re-runs `bundlemirror.Enumerate` against the live [`context/`](../../context) tree and diffs it against the binary's baked-in `embedded.Manifest()`. |
| **Install** | |
| [`atomic/cmd/atomic/cmd_claude.go`](../../atomic/cmd/atomic/cmd_claude.go) | `atomic claude` verb dispatch; `runClaudeInstall`, `printPostInstallHint`. |
| [`atomic/internal/claudeinstall/install.go`](../../atomic/internal/claudeinstall/install.go) | `Plan`, `Apply`, `Install`, `Update`, `Diff`, `List`, `ReapplyAgents`. SHA256 idempotency, agent frontmatter patching, backups. |
| [`atomic/internal/claudeinstall/atomicblock.go`](../../atomic/internal/claudeinstall/atomicblock.go) | `<atomic>...</atomic>` parser for `~/.claude/CLAUDE.md`. |
| [`atomic/internal/claudeinstall/manifest.go`](../../atomic/internal/claudeinstall/manifest.go) | Stale-artifact pruning and the `[install]` config manifest. |
| [`atomic/internal/claudeinstall/snapshot.go`](../../atomic/internal/claudeinstall/snapshot.go) | Write-once pre-install snapshot, the input to uninstall. |
| [`atomic/internal/claudeinstall/uninstall.go`](../../atomic/internal/claudeinstall/uninstall.go) | `BuildUninstallPlan`, `GenerateUninstallPrompt`. |
| **Specs and guides** | |
| [`docs/spec/artifact-templates.md`](../spec/artifact-templates.md) | Render/expansion contract, orphan rule, partial taxonomy. |
| [`docs/spec/install-workflow.md`](../spec/install-workflow.md) | `atomic claude install/update`, SHA256 idempotency, backups, the [`CLAUDE.md`](../../CLAUDE.md) merge path. |
| [`docs/spec/atomic-binary.md`](../spec/atomic-binary.md) | Master spec for every [`atomic`](../../atomic) CLI verb, including the `claude` family. |
| [`docs/spec/agents-effort-config.md`](../spec/agents-effort-config.md), [`docs/design/agents-effort-config.md`](../design/agents-effort-config.md) | The `[claude.agents.<name>]` `model` and `effort` overrides that install patches into agent frontmatter. |
| [`docs/spec/uninstall.md`](../spec/uninstall.md), [`docs/design/uninstall.md`](../design/uninstall.md) | `atomic claude uninstall`: snapshot, restore plan, LLM merge of modified files. |
| [`docs/spec/artifact-consolidation.md`](../spec/artifact-consolidation.md) | Why the artifact surface is shaped as it is: ship-verb family collapse, cold ops moved to binary-emitted prompts. |
| [`docs/spec/output-style-seed.md`](../spec/output-style-seed.md), [`docs/design/output-style-seed.md`](../design/output-style-seed.md) | Why install seeds the user-level `outputStyle` key, the non-goals (never a project file, never an overwrite), and the root-cause investigation behind seeding it at all. |
| [`docs/guides/contributing.md`](../guides/contributing.md) | Contributor workflow, build pipeline, testing. |
| [`docs/guides/install.md`](../guides/install.md) | User-facing install walkthrough. |


## Constraints


**Never edit a mirrored file.** `atomic/internal/embedded/bundle/**` and `manifest.go` are overwritten on every `make bundle` run and are not tracked by git, so a hand edit disappears at the next build with no diff to reveal it happened.

**A plain `go build` without `make bundle` embeds a stale `bundle/`.** Nothing committed records what the bundle should contain. Only doctor's manifest check and the `atomic validate` bundle check detect a binary built from an old mirror, and both run only inside the atomic-claude repo.

**A source path segment starting `_` or `.` breaks install.** The plain `//go:embed bundle` pattern in [`atomic/internal/embedded/bundle.go`](../../atomic/internal/embedded/bundle.go) carries no `all:` prefix, so `embedded.FS` omits any file or directory whose name starts `_` or `.`. A command, rule, or skill path with such a segment still lands in `Manifest()`, and install then fails at `read embedded` (`readPatchedEmbedded` in [`atomic/internal/claudeinstall/install.go`](../../atomic/internal/claudeinstall/install.go)).

**A declined prune orphans the stale artifacts permanently.** When the prune confirm is declined (non-TTY or Ctrl+C), `writeInstallManifest` in [`atomic/internal/claudeinstall/manifest.go`](../../atomic/internal/claudeinstall/manifest.go) still rewrites `[install.artifacts]` from the current plan. The stale targets drop out of the stored list, so the next `PruneDiff` never offers them again and the files stay in `~/.claude` until removed by hand.

**The `<atomic>` block is the ownership boundary in `~/.claude/CLAUDE.md`.** Content inside the tags is atomic-owned and replaced wholesale; content outside is preserved byte for byte. Detection is line-anchored, so only a line whose trimmed content is exactly `<atomic>` or `</atomic>` counts, and a backticked mention in prose never matches. Duplicate blocks, an unclosed tag, or a close before an open all report not-ok and route to the merge path instead.

**A `--target` install never seeds the output style.** `isUserLevelTarget` compares `targetDir` against `home/.claude` with `hooks.SameDir`, so an install with `--target` pointing anywhere but `~/.claude` leaves `outputStyle` unset in `~/.claude/settings.json`, and doctor's output-style check warns.


## Coupling


- **drivers** — the `atomic claude` verbs, `atomic update` (which runs `atomic claude update --no-update-check` after the binary swap, `artifactRefreshArgs` in [`atomic/cmd/atomic/cmd_update.go`](../../atomic/cmd/atomic/cmd_update.go)), `make bundle`, `atomic doctor --fix`, and `atomic config agents` drive this domain. No slash command or agent invokes it.
- **config** — install writes into separate roots that must not be confused. Artifacts go to `targetDir` (default `~/.claude`, overridable with `--target`); everything atomic owns resolves through [`atomic/internal/config/paths.go`](../../atomic/internal/config/paths.go) under `~/.atomic` (`config.toml`, `backups/`, `proposed/CLAUDE.md`, `profile.md`, `pre-install/`). The config domain owns those helpers; this domain calls them. Spec: [`docs/spec/configurable-state-paths.md`](../spec/configurable-state-paths.md).
- **config** — `atomic config agents` writes `[claude.agents.<name>]` overrides, then calls `ReapplyAgents` through the `ApplyAgentsHook` package variable. `internal/config` cannot import `internal/claudeinstall` without a cycle, so [`atomic/cmd/atomic/main.go`](../../atomic/cmd/atomic/main.go), the composition root, wires the hook in `init()`. The `ApplyAgentsHook` closure passes `ResolveTarget("~/.claude")` to `ReapplyAgents(targetDir, home)`, so an install made with a custom `--target` is invisible to that verb.
- **config** — `seedOutputStyleForInstall` in [`atomic/internal/claudeinstall/install.go`](../../atomic/internal/claudeinstall/install.go) calls `hooks.SeedOutputStyle` and `hooks.SameDir` from [`atomic/internal/hooks/`](../../atomic/internal/hooks), the config domain's package. `hooks.SeedOutputStyle` owns the `outputStyle` key write to `~/.claude/settings.json`; this domain only decides when to call it (manifest carries the artifact, target is user-level).
- **doctor** — [`atomic/internal/manifestcheck/manifestcheck.go`](../../atomic/internal/manifestcheck/manifestcheck.go) calls `bundlemirror.Enumerate` and compares the result against the binary's embedded `Manifest()`. Doctor's manifest check (`RunCheckManifestWith`) and `atomic validate`'s bundle check (`runBundleAt`) both consume it; they are the only staleness check, since neither side is committed. Both are repo-dev only and SKIP outside the atomic-claude repo. Changing a `bundlespec` predicate changes what both report. `atomic validate`'s config lint (`runC3` in [`atomic/internal/validate/config.go`](../../atomic/internal/validate/config.go)) also expands `context/commands/*.md` through `templaterender.LoadPartials` and `Expand` before checking `subagent_type` literals, so a dispatch inside a partial is checked too.
- **doctor** — doctor's install check calls `claudeinstall.Diff` to find drift between the embedded bundle and `~/.claude`: an absent artifact is FAIL, a differing one is WARN, and `atomic doctor --fix` repairs it.
- **doctor** — doctor's category 14 check ([`atomic/internal/doctor/checks_output_style.go`](../../atomic/internal/doctor/checks_output_style.go)) reads the same `outputStyle` key this domain's install path writes via `hooks.SeedOutputStyle`, and warns when it is absent or names an uninstalled style. `atomic doctor --fix` re-seeds it through the same `hooks` primitives.
- **doctor, config** — install creates `~/.atomic/profile.md` on first run via [`atomic/internal/profile`](../../atomic/internal/profile) and prints `ProfileNudge`. Profile content and its freshness window belong to those domains.
- **workflow** — every ship verb and orchestrator command lives in [`context/commands/`](../../context/commands). Changing a ship-verb flow means editing [`context/_partials/commit-flow.md`](../../context/_partials/commit-flow.md) and its siblings, not a single command file, because the partials fan out to every command in the family.
- **docs-meta** — [`context/CLAUDE.md`](../../context/CLAUDE.md) is the bundle input and a separate file from this repo's own project instructions: [`context/CLAUDE.md`](../../context/CLAUDE.md) installs as every user's `~/.claude/CLAUDE.md`, while this repo's root [`CLAUDE.md`](../../CLAUDE.md) never installs. A change to [`context/CLAUDE.md`](../../context/CLAUDE.md) reaches every user on their next update.
- **docs-meta** — [`context/_partials/agent-yagni.md`](../../context/_partials/agent-yagni.md) and the "Simplicity first (YAGNI)" ladder in [`context/CLAUDE.md`](../../context/CLAUDE.md)'s `<principles>` block carry the same steps verbatim. [`context/CLAUDE.md`](../../context/CLAUDE.md) is copied byte-for-byte, not expanded, so nothing enforces the match. Edit both together.

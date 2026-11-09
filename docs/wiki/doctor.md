---
type: Domain
description: Deterministic health gates: `atomic doctor` (integrity checks, opt-in repair) and `atomic validate` (static lint).
tags: [health, cli]
---

# doctor

## What it does

When an installed artifact drifts from the bundle, sessions run the stale copy. When the `@docs/wiki/index.md` ref drops out of the project's [`CLAUDE.md`](../../CLAUDE.md) family, sessions start without the project map. When a spec loses its `## Checkpoints` table, the subagent that implements it has no checkpoints to walk. None of these raises an error.

This domain turns those states into a non-zero exit on demand. `atomic doctor` runs a fixed registry of checks over the installed `~/.claude` bundle, the user's `~/.atomic` state, and the current repo, then exits non-zero if any check FAILs. `atomic validate` is the static half: it lints spec structure, cross-reference integrity, bundle parity, and CLI verb/flag citations in artifacts, with the same exit-code contract. Neither writes anything unless you pass `atomic doctor --fix`.

## How it works

The exit code is decided twice under `--fix`: once from the checks, then again after repairs, which is why a run can print a FAIL and still exit 0.

```mermaid
flowchart TD
    %% source: atomic/cmd/atomic/cmd_doctor.go; atomic/internal/doctor/doctor.go, exit.go, format.go, fix.go
    H{"ClaudeHomeMissing?"} -->|"yes, code 0"| Out["os.Exit"]
    H -->|no| R["Run"]
    R --> E["ExitCode"]
    E --> F["FormatHuman /<br/>FormatJSON"]
    F --> X{"--fix?"}
    X -->|no| Out
    X -->|yes| P["Repair"]
    P --> Q["postRepairExitCode"]
    Q --> Out
```

`Run` walks the registry in index order. `--skip` removes categories; `--only` selects them, and a repo-dev-only category named in `--only` runs even outside this repo. `ExitCode` is 1 if any result is FAIL.

### The check registry

Indices are stable and never renumbered.

| # | Name | File | Verifies | Fires as | `--fix` |
|---|------|------|----------|----------|---------|
| 1 | install | `checks_install.go` | Each embedded artifact against its installed copy under `~/.claude`, via `claudeinstall.Diff`. | FAIL if any missing, WARN if any drifted, SKIP if `~/.claude` absent | `atomic claude install` |
| 2 | hooks | `checks_hooks.go` | Session-start hook registered in `~/.claude/settings.json`; legacy wrapper-script form reported as drift. | WARN | `atomic hooks install` |
| 3 | signals | `checks_signals.go` | Scan age against `--stale-days`, source-tree change since the scan, then router integrity: [`docs/wiki/index.md`](index.md) present, `@`-ref'd, every domain file in its table on disk, no orphan domain file. | WARN | no |
| 4 | refs | `checks_refs.go` | `@docs/wiki/index.md` present in one of `claude.local.md`, `CLAUDE.local.md`, [`CLAUDE.md`](../../CLAUDE.md), [`claude.md`](../../claude.md). | FAIL | appends the ref block to a chosen candidate |
| 5 | manifest | `checks_manifest.go` | Bundle mirror regenerated from the working tree against the committed `embedded.Manifest()`. Repo-dev only. | FAIL | `make -C atomic bundle` |
| 6 | followups | `checks_followups.go` | Every entry's frontmatter parses, no entry past its `review_by`, `INDEX.md` byte-matches a fresh render. | WARN, SKIP when the folder is absent | INDEX drift only |
| 7 | memory | `checks_memory.go` | Every relative markdown link in this project's `MEMORY.md` resolves inside the memory dir. | WARN | no |
| 8 | binary | `checks_binary.go` | Running version against the latest release on the configured channel (`update.channel` in `~/.atomic/config.toml`, default `stable`), 5s timeout. On the `prerelease` channel, a tip that is not semver-newer is worded as available on that channel rather than misstated with `<`. A lookup error is WARN, never FAIL, so an offline machine does not break doctor. | WARN | no |
| 9 | config | `checks_config.go` | `~/.atomic/config.toml` parses, keys are known, values validate. Folds in a chronic background-update failure read from `~/.atomic/state.json`. | FAIL on parse error or invalid value, WARN on unknown key or chronic failure | no |
| 10 | profile | `checks_profile.go` | `~/.atomic/profile.md` exists and is readable; `@~/.atomic/profile.md` is wired in a candidate [`CLAUDE.md`](../../CLAUDE.md)-family file under `~/.claude`; its `lastcheck` stamp is under 30 days old. | WARN on any failed leg, and when a candidate under `~/.claude` carries the legacy `@~/.claude/.atomic/profile.md` ref and none carries the current one | no |
| 11 | code-index | `checks_code_index.go` | Code-index DB mtime against `--stale-days`. At a realm root or inside a realm member, aggregates across every non-excluded member DB instead. | WARN; absence is an informational PASS | no |
| 12 | migrate | `checks_migrate.go` | Binary version against `[install].version`; and whether `~/.claude/.atomic` is still a real directory. | WARN | no |
| 13 | repo-config | `checks_repo_config.go` | The repo config at `config.RepoConfigPath` ([`.claude/atomic.toml`](../../.claude/atomic.toml) by default, `.pi/atomic.toml` under the `.pi` harness dir): parses, keys known, `[code] ignore` globs valid, `scope` valid, `[repl] idle_timeout` parses, `[comments] max_lines` is at least 1. Dispatcher also flags `scope = "repo"` at a root registered as a realm in the `<wikis>` block. | WARN; absence is an informational PASS | no |
| 14 | output-style | `checks_output_style.go` | User-level `outputStyle` key in `~/.claude/settings.json`: whether set, and to what value. Appends a note to `Detail` when the repo's own `settings.json`/`settings.local.json` sets `outputStyle`, naming the file, as a possible override; skipped when `RepoRoot` is empty or the repo's own [`.claude`](../../.claude) dir resolves to the install target. | WARN when absent, or set to `"Atomic"` without the style file installed; PASS otherwise | unqualified absence: `defaultOutputStyleRepair` -> `hooks.SeedOutputStyle`; otherwise message only |

Categories 11, 12, and 13 have no `repairPlan` case, so `--fix` prints `cannot auto-fix — unknown category` for them rather than a category-specific line.

### The repair loop

`--fix` prompts only for items `repairPlan` marks fixable, and a repair error other than `errNonFixable` counts as skipped, so the `Applied` total that gates the re-check counts only repairs that succeeded.

```mermaid
flowchart TD
    %% source: atomic/internal/doctor/fix.go, stdin_prompter.go
    A["filterActionable<br/>(WARN and FAIL only)"] --> B{"repairPlan<br/>fixable?"}
    B -->|no| N["NonFixable++"]
    B -->|yes| C{"Confirm<br/>y/N/a/q"}
    C -->|N| S["Skipped++"]
    C -->|"q or Ctrl+C"| Q["remaining items Skipped,<br/>print summary"]
    C -->|"y, or a"| R["applyRepair"]
    R --> D{"error?"}
    D -->|errNonFixable| N
    D -->|other| S
    D -->|nil| P["Applied++"]
```

On a terminal, `prompt.Confirm` is a yes/no form, so `a` and `q` exist only when stdin or stdout is not a terminal. There `prompt.Confirm` reports `prompt.ErrNonInteractive`, and `stdinPrompter` prints the prompt and reads one line from stdin: `y`, `a`, and `q` map to their decisions, and empty or unrecognized input is `N`. Answering `a` applies this item and every later fixable item without another prompt. The loop ends with `<n> repairs applied, <n> skipped, <n> non-fixable.`

### The static lint

`atomic validate` takes a mode, or runs every mode when given none:

```
atomic validate                    -> spec + config + [bundle, repo-dev only] + artifacts
atomic validate spec [paths...]    -> S0 S1 S5 S6 over docs/spec/*.md
atomic validate config             -> C3 C5 C7 C9, whole-repo only
atomic validate bundle             -> manifest parity
atomic validate artifacts [paths]  -> A1 over the bundlemirror corpus
atomic validate <path>...          -> routes docs/spec/*.md to the spec rules, WARNs on anything else
```

`RunWithOutput` parses the whole argv once with `cliutil.ParseInterspersed`, so `--json` and `--suggest` are honored before or after the subcommand and its paths, and `-h` on a subcommand prints usage and exits 0. The first positional picks the mode and the rest are its paths. `config` and `bundle` take no paths: a path given to either is reported through `cliutil.UnexpectedArgs` (`atomic validate config: unexpected argument(s): [...]` plus usage) and exits 2 before any validator runs.

| Rule | Mode | Checks | Severity |
|------|------|--------|----------|
| S0 | spec | ATX headings only. A Setext heading stops S1/S5/S6 for that file, because section parsing is unreliable past it. | FAIL |
| S1 | spec | File starts with `# <title>` at line 1. | FAIL |
| S5 | spec | A `## Checkpoints` section whose table header carries `#`, `Checkpoint`, `Files/areas`, `Verifies` as an ordered subsequence. Extra columns are allowed. | FAIL |
| S6 | spec | A `## Change log` section exists. Body may be empty. | FAIL |
| C3 | config | Every `subagent_type: "name"` in `context/commands/*.md` prose resolves to `context/agents/<name>.md`, or is one of the built-ins `general-purpose`, `Explore`, `Plan`. | FAIL |
| C5 | config | Every `@`-ref in [`context/CLAUDE.md`](../../context/CLAUDE.md) resolves to a file. | FAIL |
| C7 | config | No duplicate `name:` across `context/agents/*.md` frontmatter. | FAIL |
| C9 | config | [`context/agents/`](../../context/agents), [`context/skills/`](../../context/skills), [`context/output-styles/`](../../context/output-styles) entries carry the [`atomic`](../../atomic) prefix. Without it they never bundle. | WARN |
| A1 | artifacts | Every `atomic <verb>` path cited in an artifact's code spans and code blocks (fenced or indented, including under list items) is a command or namespace in the `cliusage` surface, and every `--flag` beside a resolved verb exists on it. | FAIL |
| bundle | bundle | Generated mirror against the committed manifest. Capped at 5 findings plus an overflow line. | FAIL |

### Routing a validate invocation

The first positional decides the route, and a path-shaped first positional turns every positional into a path, so `atomic validate docs/spec/x.md` never reaches the mode switch.

```mermaid
flowchart TD
    %% source: atomic/internal/validate/validate.go, dispatch.go
    B{"positionals after<br/>ParseInterspersed?"} -->|none| W["runWholeRepo"]
    B -->|some| C{"isPathArg(first)?"}
    C -->|yes| P["runPathDispatch"]
    C -->|no| D{"first positional"}
    D -->|"spec, artifacts"| M["runSpec / runArtifacts<br/>with the rest as paths"]
    D -->|"config, bundle"| E{"more positionals?"}
    D -->|other| X["exit 2"]
    E -->|yes| X
    E -->|no| R["runConfig /<br/>runBundleImpl"]
```

`isPathArg` treats an argument as a path when it contains `/` or `\`, or its base name contains a `.`. `runWholeRepo` runs spec, config, bundle (repo-dev only), and artifacts in that order. In human mode each group prints a header and its own summary line; the aggregate appears only in `--json` output and in the exit code. When `findRepoRoot` finds no `.git` file or directory walking up from the cwd, every route except `validate bundle` exits 2, and `validate spec` with explicit paths never looks for one. `validate bundle` prints a SKIP line and exits 0 instead.

### Keeping the profile current

The `## Environment` block in `profile.md` is rewritten only by deterministic detection, and check 10 judges its freshness from the `lastcheck` stamp that rewrite leaves behind.

```mermaid
flowchart TD
    %% source: docs/design/user-profile.md; atomic/internal/profile/refresh.go, detect.go, render.go; atomic/internal/hooks/hooks.go; atomic/internal/claudeinstall/install.go; atomic/cmd/atomic/cmd_profile.go
    S["hooks, claudeinstall,<br/>refresh --if-stale"] --> I{"RefreshIfStale:<br/>stamp stale or absent?"}
    F["atomic profile refresh"] --> R["Refresh"]
    I -->|no| N["no write"]
    I -->|yes| R
    R --> D["CaptureEnv, DetectAll,<br/>DetectShell"]
    D --> E["RenderEnvironmentSection"]
    E --> W["RewriteEnvironmentSection,<br/>write .tmp, rename"]
    W -.->|"lastcheck read by"| K["check 10<br/>(30-day WARN)"]
```

`DetectAll` walks `DefaultRegistry`, the sole source of which tools are detected. `RewriteEnvironmentSection` replaces the span from `## Environment` to the next `##` heading and byte-preserves everything else. The session-start hook and `atomic claude install` pass `DefaultRefreshDays` (1); `atomic profile refresh --if-stale <N>d` passes the parsed day count, and `atomic profile refresh` with no flag always rewrites. `IsStale` treats a malformed date as stale.

## Where it lives

| Responsibility | Path | Role |
|----------------|------|------|
| doctor run | [`atomic/cmd/atomic/cmd_doctor.go`](../../atomic/cmd/atomic/cmd_doctor.go) | `runDoctor`: `ParseFlags`, the `ClaudeHomeMissing` short-circuit, `Run`, `FormatHuman`/`FormatJSON`, `Repair`, `postRepairExitCode`. |
| doctor run | [`atomic/internal/doctor/doctor.go`](../../atomic/internal/doctor/doctor.go) | Category registry (`categories`, `Categories()`), `Run` / `RunWith`, `--only`/`--skip`/repo-dev filtering. Resolves `Opts.RepoRoot` once per run. |
| doctor run | [`atomic/internal/doctor/flags.go`](../../atomic/internal/doctor/flags.go) | Parses `--fix --json --only --skip --stale-days --verbose`. Accepts category indices or names. |
| doctor run | [`atomic/internal/doctor/exit.go`](../../atomic/internal/doctor/exit.go) | `ExitCode`: 1 if any result is FAIL, else 0. WARN and SKIP never fail the run. |
| doctor run | [`atomic/internal/doctor/format.go`](../../atomic/internal/doctor/format.go) | `FormatHuman`, `FormatJSON`, `FormatJSONMissingHome`, and `FormatResultLine` (shared with `updatedoctor`). |
| doctor run | [`atomic/internal/doctor/shortcircuit.go`](../../atomic/internal/doctor/shortcircuit.go) | `ClaudeHomeMissing` plus the canonical message ``"atomic-claude not installed; run `atomic claude install`."`` |
| doctor run | [`atomic/internal/doctor/repodev.go`](../../atomic/internal/doctor/repodev.go) | `IsRepoDev` / `gitToplevelFn`. Repo-dev marker is [`atomic/internal/bundlemirror/mirror.go`](../../atomic/internal/bundlemirror/mirror.go). |
| doctor run | [`atomic/internal/doctor/inode_unix.go`](../../atomic/internal/doctor/inode_unix.go), [`atomic/internal/doctor/inode_windows.go`](../../atomic/internal/doctor/inode_windows.go) | Inode comparison (a size-and-modtime fingerprint on Windows), so a case-insensitive filesystem does not report [`CLAUDE.md`](../../CLAUDE.md) and [`claude.md`](../../claude.md) as distinct files. |
| doctor run | [`docs/spec/atomic-doctor.md`](../spec/atomic-doctor.md) | Canonical contract: every check category, fix functions, exit codes, `--fix` behavior. Design rationale for the check registry at [`docs/design/atomic-doctor.md`](../design/atomic-doctor.md). |
| doctor run | [`docs/spec/scope-marker.md`](../spec/scope-marker.md) | Config-domain spec. Cross-listed for check 13's `scope` validation and the `<wikis>` contradiction sub-check. |
| repair | [`atomic/internal/doctor/fix.go`](../../atomic/internal/doctor/fix.go) | `Repairer` with injectable repair funcs, `repairPlan` (the per-category fixability table), the interactive y/N/a/q loop, `RepairSummary`. |
| repair | [`atomic/internal/doctor/fix_impls.go`](../../atomic/internal/doctor/fix_impls.go) | Production repair implementations. Streams subprocess output straight to the writer, no buffering. |
| repair | [`atomic/internal/doctor/stdin_prompter.go`](../../atomic/internal/doctor/stdin_prompter.go) | `Prompter` implementation. Maps `prompt.ErrAborted` to `DecisionAbort`; on `prompt.ErrNonInteractive` it falls back to reading one stdin line. |
| validate | [`atomic/internal/validate/validate.go`](../../atomic/internal/validate/validate.go) | Flag parse (`cliutil.ParseInterspersed`, flags in any position) and subcommand dispatch; rejects paths on `config` and `bundle`. Exit 0 pass or warn, 1 any FAIL, 2 bad invocation, stray argument, or internal error. |
| validate | [`atomic/internal/validate/dispatch.go`](../../atomic/internal/validate/dispatch.go) | Path-aware routing and `runWholeRepo`. |
| validate | [`atomic/internal/validate/repo.go`](../../atomic/internal/validate/repo.go) | `findRepoRoot` (walks up to a `.git` file or directory, so worktrees resolve) and `repoDev`, which mirrors doctor's repo-dev marker without importing the doctor package. |
| validate | [`atomic/internal/validate/spec.go`](../../atomic/internal/validate/spec.go) | `RunSpecRules(path, src)`: pure, no filesystem access. |
| validate | [`atomic/internal/validate/config.go`](../../atomic/internal/validate/config.go) | `RunConfigRules(repoRoot)`, the `reAtRef` / `reSubagentType` grammars, and `isEmailLocalChar`. |
| validate | [`atomic/internal/validate/bundle.go`](../../atomic/internal/validate/bundle.go) | Wraps `manifestcheck.Compare` into findings. |
| validate | [`atomic/internal/validate/finding.go`](../../atomic/internal/validate/finding.go) | `Finding`, deterministic sort by (path, line, rule), `summarize`, `exitCode`. |
| validate | [`atomic/internal/validate/output.go`](../../atomic/internal/validate/output.go) | Human and JSON formatters. JSON envelope is `schema_version: 1`. `--suggest` templates exist for S5 and S6 only. |
| validate | [`docs/spec/atomic-validate.md`](../spec/atomic-validate.md) | `atomic validate` contract: the S, C, and A rule sets and their severities. Design at [`docs/design/atomic-validate.md`](../design/atomic-validate.md). |
| validate | [`atomic/internal/validate/artifacts.go`](../../atomic/internal/validate/artifacts.go) | A1: code-span extraction, `longestMatch` verb-path resolution, flag comparison. `ScanArtifactText` is the pure seam. |
| validate | [`docs/spec/validate-artifact-cli-flags.md`](../spec/validate-artifact-cli-flags.md) | A1 contract: the `cliusage` surface, scanner rules, known scope limits. Design at [`docs/design/validate-artifact-cli-flags.md`](../design/validate-artifact-cli-flags.md). |
| validate | [`docs/spec/verify-gate-validate.md`](../spec/verify-gate-validate.md) | How the `atomic-verify` skill gates on validate output. Design at [`docs/design/verify-gate-validate.md`](../design/verify-gate-validate.md). |
| supporting packages | [`atomic/internal/cliusage/`](../../atomic/internal/cliusage) | The [`atomic`](../../atomic) command surface as structured data. `SetRoot` derives it from the live Cobra tree; `TopLevelVerbs` and `LookupByPath` serve A1. |
| supporting packages | [`atomic/internal/mdparse/`](../../atomic/internal/mdparse) | Goldmark-backed markdown reads shared by the validators. `InlineRefs` and `CodeBlockLines` (fenced and indented blocks, list- and blockquote-nested included, each line tagged with its fence language so A1 can skip `mermaid`) feed A1's citation extraction; `Sections`, `IsATXOnly`, and the table finders serve the spec rules. |
| supporting packages | [`atomic/internal/manifestcheck/`](../../atomic/internal/manifestcheck) | `Compare(repoRoot, committed)` walks the tree with `bundlemirror.Enumerate` and diffs SHA256 against the committed manifest. Writes nothing, spawns nothing. Used by check 5 and by `validate bundle`. |
| supporting packages | [`atomic/internal/updatedoctor/`](../../atomic/internal/updatedoctor) | Post-update adapter. Calls `doctor.Run(Opts{Skip: []int{3, 8}})`, prints FAIL lines only, recovers panics, never changes the update exit code. |
| supporting packages | [`docs/spec/atomic-update-doctor.md`](../spec/atomic-update-doctor.md) | Post-update auto-fire contract: skip set `[3, 8]`, panic recovery, exit-code preservation, artifact auto-refresh. |
| supporting packages | [`atomic/internal/profile/`](../../atomic/internal/profile) | Detection registry and `DetectAll`, `RenderEnvironmentSection`, `Refresh` / `RefreshIfStale`, `ParseLastcheck` / `IsStale`. Check 10 reads it; `claudeinstall`, the session-start hook, and `atomic profile refresh` ([`atomic/cmd/atomic/cmd_profile.go`](../../atomic/cmd/atomic/cmd_profile.go)) write through it. |
| supporting packages | [`docs/spec/user-profile.md`](../spec/user-profile.md) | Profile schema, the `<stable>`/`<volatile>`/`<deterministic>` tags, install-time stub generation. Design at [`docs/design/user-profile.md`](../design/user-profile.md). |
| supporting packages | [`atomic/internal/followups/`](../../atomic/internal/followups) | `LoadEntriesWithErrors` is check 6's parse boundary; `Render` is what the INDEX byte-comparison compares against. |

## Constraints

- **`--fix` exits on the post-repair state, not the one it printed.** `postRepairExitCode` in [`atomic/cmd/atomic/cmd_doctor.go`](../../atomic/cmd/atomic/cmd_doctor.go) re-runs every check after the repair pass, so CI can gate on `atomic doctor --fix` in one run. The second pass is skipped when no repair was applied, and a re-check that errors keeps the pre-repair verdict rather than reporting health nobody observed.
- **`--fix` and `--json` are mutually exclusive**, rejected at flag-parse time with exit 2. A caller adding `--fix` to a `--json` run gets exit 2 and no JSON on stdout; the error goes to stderr. A non-positive `--stale-days` and an unknown `--only`/`--skip` token are rejected the same way.
- **A missing `~/.claude` short-circuits the whole run.** It prints ``atomic-claude not installed; run `atomic claude install`.`` and exits 0 without running a single check. A green doctor is not proof of a healthy install; it can mean no install at all.
- **Repo-dev-only checks vanish outside this repo.** Check 5 is omitted entirely, not even reported as SKIP, unless you ask for it with `--only 5`, which outside this repo returns SKIP `not in atomic-claude repo`. Same for `validate bundle` inside a bare `atomic validate`. Users running in their own projects never see bundle noise.
- **Check 10's 30-day threshold is not the refresh window.** `profileStaleDays` (30) is the doctor WARN line; `DefaultRefreshDays` (1) is what install and the session-start hook pass to `RefreshIfStale`. Do not unify them. At 30, the session-start refresh would leave the env block stale for a month; at 1, doctor would WARN on any machine idle for a day.
- **One repo-root resolution per `Run` call.** `Run` resolves `Opts.RepoRoot` through `gitToplevelFn` once, and a check that calls `gitToplevelFn` instead of reading `opts.RepoRoot` breaks [`atomic/internal/doctor/gitcallcount_internal_test.go`](../../atomic/internal/doctor/gitcallcount_internal_test.go).
- **`validate`'s PASS is a count of inspected files with no finding.** A file with any WARN or FAIL is not a PASS, and the bare-form aggregate is the sum of the groups. The unit differs per group: spec counts each `docs/spec/*.md` read; config counts the files C3, C5, and C7 read (commands, [`context/CLAUDE.md`](../../context/CLAUDE.md), agents), not the [`context/skills/`](../../context/skills) and [`context/output-styles/`](../../context/output-styles) entries C9 only names; artifacts counts each scanned artifact; bundle counts compared manifest entries, taken before the five-diff cap so a hidden diff never counts. Path dispatch counts only spec paths, so a path with no validator is never a PASS. A gate that reads PASS as "files checked" undercounts every group with findings.
- **A check can combine independent findings into one Result.** Check 9 appends a chronic update-failure detail to whatever the config-validity leg found, capped at WARN on its own. Check 12 WARNs when either the version-drift or the legacy-state-dir leg fires; both details concatenate. Check 14 appends a project-override note to whatever the user-level leg found, without changing the severity. Reading only the severity loses half the signal; read `Detail`.
- **C5 scans [`context/CLAUDE.md`](../../context/CLAUDE.md) only** (the bundle source that installs as every user's global contract), not the project-local root [`CLAUDE.md`](../../CLAUDE.md). The local overlays are deliberately excluded: they are user-owned and routinely contain backtick spans that look like `@`-refs, such as scoped npm package paths. C5 also skips any `@` preceded by an email local-part character, since RE2 has no lookbehind and `reAtRef` is loose on the right of the `@`.
- **Check 14 reports the user-level `outputStyle`, never an effective style.** Claude Code's per-repo file-placement precedence is not documented well enough to replicate, so a repo-level `outputStyle` lands in `Detail` as a possible override and never changes the severity.
- **A1 does not check flags on an unresolved citation.** A namespace reference (`atomic code`) or an alternation (`atomic code index|sync`) passes with whatever flags follow it. Prose inside a non-`mermaid` fence is read as code, so a file-tree line that puts a prose word after a namespace such as `code` FAILs. Citation boundaries and placeholders: [`docs/spec/validate-artifact-cli-flags.md`](../spec/validate-artifact-cli-flags.md).
- **`cliusage`'s hardcoded slice is a fixture, not the runtime source.** `main` calls `SetRoot(rootCmd)` at startup, so production reads the live Cobra tree. Tests that never call `SetRoot` read the static slice, which is why the golden test is the thing keeping A1 honest.

## Coupling

**bundle.** Check 1, check 5, `validate bundle`, A1, and check 14 all read bundle-domain inclusion rules. Check 1 uses `claudeinstall.Diff`; check 5 and `validate bundle` both go through `manifestcheck.Compare`, which calls `bundlemirror.Enumerate`; A1 scans that same enumeration as its artifact corpus; check 14 uses `claudeinstall.ResolveTarget` to locate the install target and `hooks.StyleInstalled` to confirm `output-styles/atomic.md` landed there. Change what bundles and these surfaces change with it.

**signals and wiki.** Checks 3 and 4 own the `@docs/wiki/index.md` contract. `signalsRef` in `checks_refs.go` and `routerRef` in `checks_signals.go` are the constants; the signals domain's wiring convention must move with them. Check 3 additionally parses the router's Domains table, so a change to that table's shape breaks orphan and missing-file detection. Check 13's contradiction sub-check calls `wiki.ReadWikiIndexPaths`.

**config.** Check 9 validates the user schema through `config.Load` and `config.Validate`; check 13 locates the repo config through `config.RepoConfigPath` and validates it through `config.LoadRepoConfig`, `config.NewIgnoreMatcher`, `config.ValidScope`, and `config.ResolveMaxLines`; check 10 resolves paths through `config.ProfilePath`; check 12 reads `[install].version` through `config.Load`; check 14 reads `output_style.seed` through `hooks.SeedEnabled`, which calls `config.Load` in turn. A new config key is not covered until one of these learns about it.

**config (`cliutil`).** `validate.go` parses through `cliutil.ParseInterspersed` and reports stray paths through `cliutil.UnexpectedArgs`, both in [`atomic/internal/cliutil/args.go`](../../atomic/internal/cliutil/args.go). The trailing-flag contract is [`docs/spec/cli-trailing-flags.md`](../spec/cli-trailing-flags.md).

**repl.** Check 9 (via `config.Validate`) and check 13 both call `config.ValidateIdleTimeout`, the same validator `atomic repl` uses at spawn time, so an invalid `[repl] idle_timeout` surfaces in doctor before `atomic repl start` ignores it with a stderr warning and falls through to the next tier.

**update.** `atomic update` ([`atomic/cmd/atomic/cmd_update.go`](../../atomic/cmd/atomic/cmd_update.go)) calls `updatedoctor.Run(doctor.Run, os.Stdout)` after an update unless `--no-doctor` is passed or `update.run_doctor` is false. Check 8 reads `update.channel` through `config.Load` and looks up the latest release with `selfupdate.Client.Check`, so a change to the release lookup or the channel key reaches doctor. Check 9 reads `selfupdate.LoadState(config.StatePath(home))`, so a change to the shape of `update.last_result` reaches doctor too, and check 12 compares versions with `selfupdate.CompareSemver`.

**profile writers.** `hooks` and `claudeinstall` call `profile.RefreshIfStale` through their `ProfileRefresh` variables, and that rewrite leaves the `lastcheck` stamp check 10 judges.

**code-intel.** Check 11 locates the DB with `engine.IndexPath` and detects realm scope with `realm.Resolve` / `realm.LoadConfig`. If the engine's index-path convention changes, this check breaks silently.

**workflow.** The `atomic-verify` skill is the consumer that matters: it runs `atomic validate spec` + `config` + `artifacts` when a spec or bundled artifact changed, and treats a FAIL as a gate failure equal to a failing test. `/subagent-implementation`, `/autopilot`, `/implement`, and `/quick-fix` reach validate through [`context/_partials/loop-finalize.md`](../../context/_partials/loop-finalize.md), which invokes `atomic-verify` (spec, config, and artifacts) and then runs `atomic validate spec` and `atomic validate config` when a spec, design doc, or bundled artifact changed. `/retrospective-learning` runs `atomic doctor --json --skip signals,binary` and `atomic validate --json` into its scratchpad, each with stderr discarded and `|| true`, then turns every doctor `FAIL`/`WARN` and every validate `FAIL` into a candidate finding. These artifacts call the binary unconditionally; none checks whether it is installed.

**Lockstep contracts.**

| Change | Must change with it |
|--------|---------------------|
| Add a doctor category, or change which categories post-update should skip | The hardcoded skip set `[3, 8]` in [`atomic/internal/updatedoctor/updatedoctor.go`](../../atomic/internal/updatedoctor/updatedoctor.go) |
| Add a doctor category | A `repairPlan` case in [`atomic/internal/doctor/fix.go`](../../atomic/internal/doctor/fix.go), even a non-fixable one; without it, `--fix` prints `cannot auto-fix — unknown category` for that category |
| Register or remove a Cobra flag | `TestDeriveCommandsGolden` in [`atomic/cmd/atomic/main_test.go`](../../atomic/cmd/atomic/main_test.go), which pins the derived surface against the hardcoded `cliusage` slice, and therefore what A1 accepts |
| `FormatResultLine` output shape | Both `FormatHuman` and `updatedoctor`'s FAIL-only lines |

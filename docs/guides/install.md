# Install


## Prerequisites

You need these tools on your `PATH` before installing:

- **Claude Code CLI** — `npm install -g @anthropic-ai/claude-code` (Node.js 18+)
- **Claude subscription or API key** — Pro, Max, or Team plan for OAuth; or set `ANTHROPIC_API_KEY` for direct billing
- **git** 2.30+ — used by every ship verb, worktree command, and cleanup scan
- **GitHub CLI** (`gh`) — used by `/commit` and `/report-issue`. Authenticate with `gh auth login`
- **POSIX shell** — `bash` or `zsh`, plus standard utilities (`grep`, `sed`, `awk`, `find`, `jq`, etc.)
- **Oh My Pi (OMP)** — optional second harness. Enroll it with `atomic install --harness omp`; the `omp` binary must be on `PATH` so its agent root can be discovered
- **Codex CLI** (optional third harness) — enroll it with `CODEX_HOME=<root> atomic install --harness codex`. The only root Atomic will enroll is the one `CODEX_HOME` names; the default `~/.codex` was never observed at runtime, so Atomic refuses to guess at it
- **Docker** — only needed for the [evaluation environment](./evaluations.md), not for normal use


## Quick install

Two commands. The first installs the `atomic` binary; the second enrolls a harness and converges it.

Download the binary:

```bash
curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash
```

This puts `atomic` in `~/.local/bin/` (override with `ATOMIC_INSTALL_DIR`). To pin a version: `ATOMIC_VERSION=v5.4.0 curl ... | bash`. The installer invokes no install verb — it prints the next step and stops.

Enroll a harness target. Atomic ships one authored corpus and projects it into each harness natively; nothing enrolls implicitly, so a harness must be named:

```bash
atomic install --harness claude     # Claude Code
atomic install --harness omp        # Oh My Pi (OMP)
CODEX_HOME=~/.codex atomic install --harness codex   # Codex CLI
```

`--dry-run` prints the plan and writes nothing; `--yes` approves the printed plan without prompting; `--instance <root>` names a non-default target; `--all` covers every discovered instance. `--replace` and `--leave-unowned` are the batched decision for older-version artifacts the selected generation cannot prove. The command enrolls the target in `~/.atomic/install/ledger.json`, converges its native resources, and — for Claude — registers the session-start hook and seeds the Atomic output style into `~/.claude/settings.json`. Verify with `atomic doctor`, which reports the enrolled target among its checks.

For the Claude-only bundle path that skips enrollment entirely:

```bash
atomic claude install
```

It writes the embedded bundle (`CLAUDE.md`, agents, commands, skills, output styles, rules) into `~/.claude/`, seeds the Atomic output style into `~/.claude/settings.json`, and registers the hook (pass `--no-hooks` to skip it). This legacy route is only for an install the ledger does not own: when the resolved `~/.claude` root is enrolled, `atomic claude install|update` converges it through the install planner instead — the same lock, journal, and ledger every other lifecycle verb uses — so an un-adopted install is the only one the legacy writer still touches. `atomic update` converges enrolled targets too.

For a project-scoped Claude install instead of global: `atomic claude install --target ./.claude`. That route deliberately does not seed the output style, since the file it would write is committed and the choice is personal. Pick the style yourself with `/config` → **Output style** → **Atomic**, which writes the gitignored `.claude/settings.local.json`.


## Harnesses and targets

`atomic harness` is the lifecycle surface over enrolled targets. Discovery is read-only and never enrolls.

| Verb | Does |
|------|------|
| `atomic harness list` | List discovered and enrolled instances, marking each `discovered` or `enrolled` |
| `atomic harness status [<target-key>]` | Report one target's resources, the enrolled targets that consume them, and any unenrolled instances that can merely see them |
| `atomic harness enroll <claude\|omp\|codex>` | Enroll an instance and converge it; takes the same selection flags as `atomic install` |
| `atomic harness adopt [claude]` | Import a verified legacy Claude install into the ledger |
| `atomic harness repair` | Reconverge already-enrolled targets |
| `atomic harness diff` | Report each enrolled resource's native difference from the selected generation, read-only |
| `atomic harness recover [--rollback] [--dry-run]` | Reconcile an unresolved journal: roll it forward by default, or restore its digest-verified pre-mutation backups under `--rollback` |
| `atomic harness uninstall <target-key>` / `--all` | Remove one enrolled target, or every target |
| `atomic harness rules status` / `rules sync` | Report or converge per-target rule tier, digests, coverage, and conflicts |

Every real mutation takes one advisory lifecycle lock and recovers unresolved journals oldest-first before planning; the target is re-observed before the plan is built, so a plan that cannot be decided reports `blocked` and changes nothing. `--dry-run` opens no lock, writes nothing, and reports `blocked_on_recovery` when a journal cannot resolve to one safe result. Resource ownership lives in `~/.atomic/install/ledger.json`; in-flight operations live in `~/.atomic/install/{journals,transactions}/`.

### Oh My Pi

OMP enrolls through the same verbs, and what it receives is what discovery proved it loads:

- the profile `AGENTS.md`, carrying the Atomic steering and output-style block;
- the generated extension module at the profile agent root's `extensions/atomic.ts`, which supplies the bounded session-baseline rule index and the capability-proven events.

Named profiles are discovered through `OMP_PROFILE`: a profile selector you already export for OMP is the same one Atomic resolves, so a named profile is not a separate configuration. Atomic also publishes its corpus — commands, agents, skills, and rule bodies — to `~/.atomic/packages/omp/atomic`, but that tree is a **corpus store**: no OMP surface was observed discovering it. Package installation, registration, and command/agent/skill discovery are therefore reported `unsupported` in `install`, `harness status`, `harness rules status`, and `atomic doctor`'s multi-harness rule category, with the evidence that fixes each one. Never read a converged OMP target as a fully delivered one.

### Codex

Codex enrolls through the same verbs. Its native root is the `CODEX_HOME` environment variable — the only root CP0 observed — so the variable is how a home is named:

```bash
CODEX_HOME=~/.codex atomic install --harness codex
```

With `CODEX_HOME` unset, a broad walk reports the harnesses it can resolve and an explicit `--harness codex` or `harness enroll codex` refuses, naming the variable instead of guessing at `~/.codex`. `--all` with `--harness` or `--instance` is itself refused: a harness you name explicitly is never silently dropped from an all-target operation.

Enrollment publishes one generated plugin package at `~/.atomic/packages/codex/atomic`: the marketplace descriptor, the plugin manifest, the compiled rule index and matcher, and the projected rule, skill, and agent corpus. It writes no Codex configuration and registers nothing — Codex owns its own registry and cache, so Atomic observes that state instead of staging it. Register the published package with the Codex CLI:

```bash
codex plugin marketplace add ~/.atomic/packages/codex/atomic
codex plugin add atomic@atomic-claude
```

`atomic harness status`, `diff`, `rules status`, `repair`, and `uninstall <target-key>` work exactly as they do for any other harness; a target uninstall removes the owned package tree only when no other enrolled consumer still depends on it.

Rule delivery, plugin-hook trust, deliberate disablement, payload limits, and child-session behavior are **unproven** for the tested Codex version: CP0 reached thread creation, then the account rejected the configured model before any hook event. The plugin therefore emits no hook configuration at all — the rule index, matcher, rules, skills, and agents ship, and `atomic doctor`'s `codex` category (24) reports every one of those surfaces as unsupported with the evidence that fixes it. Nothing is fabricated as a trust or coverage claim, and doctor never repairs one.

### Repository state selection

Repo-local state (scratchpad, project files, the code index, worktrees) resolves through a harness-neutral ladder: process-only `ATOMIC_STATE_DIR`, then the persisted per-clone selection, then the user `state.dir` config key, then the built-in `.claude`. `atomic state adopt` selects the root — with no flags it adopts the single unambiguous populated candidate, `--dir <segment\|absolute>` names one, and `--clear` withdraws the selection so the ladder decides again. `atomic where` reports cwd's four orientation axes — repo root, repo-scope wiki, realm scope, and code-index scope — not which rung of the ladder answered; `atomic where --json` adds the project-keyed report, reminders, and archive paths.


## After installing

The installer prints one manual step it cannot automate:

1. **Scan your repos** — run `/refresh-wiki` in each repo. It builds the repo wiki, Claude's standing map of that repo's framework, commands, and layout

A few optional steps go further:

- **Check the session-start hook.** Enrolling Claude registers a session-start hook that refreshes your profile, injects pending reminders, nudges you when a wiki falls stale, and re-seeds the output style if the key is ever missing. Some managed or enterprise setups disable hooks; if yours does, remove it with `atomic hooks uninstall`, which leaves your seeded output style alone as long as the style file itself is still installed. The Claude-only bundle path can skip the hook at install time with `atomic claude install --no-hooks`; the enroll verbs (`atomic install --harness claude`, `atomic harness enroll`) always register it, so remove it afterward with `atomic hooks uninstall`. To add the hook later (or after removing it), run `atomic hooks install`; the scope defaults to your user config, and `--scope project` limits it to one repo.
- **Map related repos with a wiki.** If you work across a folder of services, libraries, or client projects, run `/refresh-wiki` to build a cross-repo wiki. It summarizes each member repo and writes up the concerns they share, so Claude can reason about a whole realm of projects rather than one repo at a time. See the [wiki workflow](/reference/realm-wiki).
- **Index a project's symbols.** Run `atomic code index` in a project to build a symbol graph of it. Once indexed, `atomic code explore "<question>"` returns a context digest of the relevant symbols and call edges in one query, and the implementation agents use the graph for blast-radius checks and domain clustering. Indexing is opt-in and degrades to plain search when absent; see the [code-intel reference](/reference/code-intel).

On first install, the binary also creates `~/.atomic/profile.md` and prints a one-line nudge. The file starts with your git name, email, OS, architecture, and CPU count filled in from the environment. The remaining sections are empty; Claude fills them in as facts surface naturally in conversation. You do not need to edit the file by hand.

`atomic harness uninstall --all` removes every enrolled target, then completed operational and adoption state — but not your data. `~/.atomic/config.toml`, `profile.md`, `wikis.md`, and backups survive a full uninstall, so a reinstall picks up where you left off. A target-level `atomic harness uninstall <target-key>` removes only the unchanged resources that target owns; a resource whose native bytes changed refuses the whole operation, and a resource another enrolled consumer still depends on is retained and reported.

From here, you are ready to work. The [getting started guide](/guides/getting-started) walks the first session step by step; the [workflow reference](/reference/workflow) covers the full lifecycle.


## Updating

Update the binary:

```bash
atomic update
```

One command updates everything: it swaps the binary, then the replacement binary converges every already-enrolled harness target with its own embedded generation, and finishes with a health check that prints what to look at if anything fails. It never enrolls a target — a harness you have not enrolled with `atomic install --harness` or `atomic harness enroll` is left alone. It is usually near-instant because a background process pre-downloads and checksum-verifies each release ahead of time; the swap re-verifies version and checksum regardless, so the binary is never stale or unverified. Convergence restores the adapter's settings defaults — for Claude it re-registers the session-start hook and re-seeds the output style if either is missing.

**A pre-multi-harness install stops here, loudly.** If `~/.claude` still carries legacy install evidence and no ledger target owns it, `atomic update` does not refresh it: it prints an `atomic harness adopt claude` instruction to stderr and exits non-zero. The retired legacy writer would rewrite owned files with no ledger row, which is exactly the drift the next converge reads as unrepairable — so the command refuses instead of silently freezing the artifacts. Adopting once moves the install under the ledger, and every later update converges it normally.

To skip the post-swap convergence, pass `--skip-claude-update` and converge manually when ready:

```bash
atomic harness repair
```

Six useful flags for `atomic update`:

- `--check` — just check if an update is available, do not download
- `--pre` — install the newest pre-release; shorthand for `--channel prerelease`
- `--channel <stable|prerelease>` — the long form of the same choice
- `--no-doctor` — skip the post-update health check
- `--skip-claude-update` — replace the binary only, skip the post-swap enrolled-target convergence
- `--force` — take over an update lock held by another process; never skips checksum verification

`atomic update` refuses to run if another update looks to be in progress, unless that lock is more than 10 minutes old (then it is assumed abandoned and taken over automatically). `--force` is the manual override for a lock you know is stale.


### Pre-releases


Every merge into the `next` branch can cut a pre-release, tagged `X.Y.Z-next.N`. `atomic update --pre` installs the newest one.

The pre-release channel **tracks the tip** of `next` rather than climbing a version ladder. It installs whatever the newest pre-release is, including one numbered below what you are running: once `6.7.0` ships stable, the pre-releases that follow it are still `6.7.0-next.N`, and refusing them would leave you stranded on stable. The stable channel only ever moves forward, and never sees a pre-release at all.

To stay on pre-releases without typing the flag each time:

```bash
atomic config set update.channel prerelease   # background check, banner, doctor and update all follow
atomic config unset update.channel            # back to stable
```

`--pre` is a one-shot override and never writes config, so a machine pinned to pre-releases can still take a single stable update with `--channel stable`.

Downgrading off the channel is manual: set the channel back to stable, then reinstall the version you want with the install script.

Three config keys control the update machinery:

```bash
atomic config set update.check false   # stop the hourly background version check entirely
atomic config set update.stage false   # keep checking for updates, but never pre-download
atomic config set update.channel prerelease   # track pre-releases instead of stable
```

`update.check` and `update.stage` default to `true`; `update.channel` defaults to `stable`.

The last check time, what's staged, and whether an update is in progress all live in the machine-managed `~/.atomic/state.json`. You never need to edit it by hand.

To suppress the health check permanently:

```bash
atomic config set update.run_doctor false
```


## Migrations

`atomic update` auto-applies versioned migration steps after converging enrolled targets. These steps handle breaking changes across releases — restructured directories, updated config keys, and similar one-time transforms — and are idempotent, so re-running them is always safe.

To apply migrations manually (for example, after a manual binary swap or a fresh install on a machine that missed an update):

```bash
atomic migrate               # apply pending install-scope steps to ~/.claude/
atomic migrate --repo <path> # run repo-scope migrations for one project
atomic migrate --realm <path> # fan-out across all atomic'd member repos (prompts per-repo)
```

`atomic doctor` nudges you to run `atomic migrate` whenever the binary version is newer than the recorded install version. The nudge is suppressed for development builds (`dev` version string).

One migration runs automatically on every invocation rather than through `atomic migrate`: v6 moved per-user state from `~/.claude/.atomic/` to `~/.atomic/`. The first run of any verb on a v6 binary renames the legacy directory and leaves a symlink at the old path, so a CLAUDE.md installed by v5 keeps resolving its `@`-references until `atomic claude install` rewrites them. `atomic doctor` warns while either the legacy directory or the old references remain.


## If you already have a CLAUDE.md

One check decides everything: whether your file already carries an `<atomic>...</atomic>` block. Either way, your own sections are never touched and the prior version is backed up to `~/.atomic/backups/` before any change.

| Your `~/.claude/CLAUDE.md` | What the installer does | What you do |
|---|---|---|
| has an `<atomic>` block (prior install) | updates the block in place; a current block counts as no drift in `atomic claude diff` and `atomic doctor` | nothing |
| no block yet (pre-block install, or hand-edited tags) | never overwrites; writes the new version to `~/.atomic/proposed/CLAUDE.md` | run `atomic prompt claude-merge` in any Claude Code session; Claude merges into a staging file (`~/.claude/CLAUDE.md.atomic-merged`) and gives you the command to apply it |

The one-time merge wraps the atomic content in `<atomic>` tags, so every later update lands in the first row and applies on its own.

A pre-v2 install converged through the generic adoption engine rather than the `claude` verb: `atomic install --harness claude` and `atomic harness adopt claude` classify the observed state, recover unresolved journals oldest-first, batch older-version replace-or-leave-unowned decisions (`--replace` / `--leave-unowned`), and preserve the write-once legacy pre-install snapshot. `atomic harness adopt` refuses to proceed past a corrupt snapshot until you acknowledge the limit with `--acknowledge-snapshot`. When in doubt, `--dry-run` prints the plan and writes nothing.


## Manual install

Download an archive from [GitHub Releases](https://github.com/damusix/atomic-claude/releases), verify with `shasum -c checksums.txt`, and move the `atomic` binary into any directory on your `$PATH`.


## Build from source

```bash
git clone https://github.com/damusix/atomic-claude.git
cd atomic-claude/atomic
make build
```


## Uninstall

Remove one enrolled target, or every target:

```bash
atomic harness uninstall claude     # one target
atomic harness uninstall --all      # every enrolled target
```

A target-level uninstall removes only the unchanged resources that target owns. A resource whose native bytes changed since Atomic wrote it is reported `skipped` and keeps its claim — Atomic never overwrites a user edit, and the rest of the target still uninstalls. A resource another enrolled consumer still depends on is retained and reported. A read-only `settings.json` is reported `skipped` too, so a later uninstall finishes the job once the file is writable. `--dry-run` opens no lock, writes nothing, and previews unfinished journals read-only.

If an unresolved journal blocks an operation, `atomic harness recover` reconciles it: the default run rolls verified work forward, and `--rollback` restores the digest-verified pre-mutation bytes instead. `atomic harness recover --dry-run` previews the decision the same command would make — including the `--rollback` choice — without writing. A run that leaves a journal unreconciled reports every conflict and exits non-zero, with or without `--json`.

`--all` removes every enrolled target, then completed operational and adoption state. It does **not** delete your data: `~/.atomic/config.toml`, `profile.md`, `wikis.md`, and backups survive, so a reinstall resumes where you left off. Unresolved journals and the backups, ledger rows, and state-location records they reference are retained until recovery completes.

The Claude-only snapshot route stays available and is independent of the ledger:

```bash
atomic claude uninstall
```

Run it from inside a Claude Code session. The CLI reads the snapshot taken during install, figures out what to restore and what to delete, and hands Claude a structured plan. Claude shows the plan, waits for confirmation, and then merges back changes you made to `settings.json` or `CLAUDE.md`, restores files that existed before install, removes files Atomic introduced, and prints the `rm` command to remove the binary (it never auto-removes the binary). If you run the command in a plain terminal, it detects this and tells you how to proceed.


## Windows

Use **WSL2** (Ubuntu, Debian, or similar). Native Windows (cmd / PowerShell) is not supported.

Install WSL2, install your distro, install Node + Claude Code + git inside the distro, and run `claude` from the WSL shell. Keep repos inside the Linux home (`~/projects/...`) for sane file watching and performance.

# Upgrade from 6.10 to 7.0


Atomic 7 adds native installation targets for Claude Code, Oh My Pi (OMP), and Codex, and moves repository state toward `.atomic/`. Existing Claude users can keep using Claude; an update does not enroll another harness.

Use this guide for a 6.10.0 installation. The commands below select the stable channel once 7.0.0 is published. Before publication, they still select the latest stable 6.x release; `--pre` selects the current prerelease instead.

## Upgrade an existing Claude installation


1. **Back up your configuration and repository state before updating.** Save `~/.atomic/` and the Claude root named by `CLAUDE_CONFIG_DIR`, or `~/.claude` when that variable is unset. Include the contents behind a symlinked root. These directories can contain credentials, so keep the backup private. In each checkout, save ignored scratchpad files as well as tracked files. Commit or otherwise preserve pending changes and the Git index before allowing migration to stage renames.

2. **Record a custom state location, if you use one.** Inspect `~/.atomic/config.toml` for `[harness] dir` before upgrading. That setting is retired, and a later configuration write drops it. The [custom-location procedure](#keep-a-custom-state-location) below keeps the existing files in place.

3. **Run the update and read its output.**

    ```bash
    atomic update --channel stable
    ```

    The replacement binary adopts the legacy Claude installation into `~/.atomic/install/ledger.json`, refreshes its managed artifacts, and migrates eligible repository state in the current checkout. It prints `adopting existing Claude install at <root>` during adoption. For a custom global Claude root, export `CLAUDE_CONFIG_DIR` in this shell; automatic adoption does not enumerate arbitrary old `--target` installations.

    **The first update can leave the new binary installed while migration is blocked.** The 6.10 updater prints a warning when its replacement-binary refresh fails, but does not propagate that failure as its own exit status. Do not treat exit 0 as proof that adoption finished. Resolve the named blocker and rerun `atomic update` with the new binary.

4. **Check the installed target and restart the harness.**

    ```bash
    atomic harness status
    atomic doctor
    ```

    Confirm that the Claude root is enrolled and resolve any named conflicts. Capability warnings describe delivery limits; they do not necessarily mean installation failed. Start a new Claude session so it loads the refreshed steering and agents.

5. **Review the current checkout and migrate the others separately.** Inspect the staged diff before committing the repository migration. An update does not visit every repository or worktree. In another checkout that still keeps Atomic state under `.claude`, run:

    ```bash
    atomic state migrate
    atomic where --json
    ```

    `state_root` reports the selected location. Review and commit the tracked renames and rule link in each checkout. If you want to scaffold the repository and apply its selected Claude steering mode too, use `atomic repo init` instead.

## What changes on disk


| Area | Upgrade behavior | What you keep or review |
|------|------------------|-------------------------|
| Installed Atomic artifacts | Adoption and updates back up changed managed files before replacing them. Artifacts removed from the bundle are backed up before retirement. | Recovery copies remain under `~/.atomic/backups/<operation>/`. Personal edits to Atomic artifacts do not remain active. |
| Personal configuration | Your profile, wiki registry, Claude agent overrides, unrelated commands, and text outside Atomic's managed steering block remain separate from the installed corpus. | Put personal behavior in supported settings or your own files. |
| Repository state | Atomic moves `.claude/atomic.toml`, `workflow-preferences.md`, `project/`, `rules/wiki/`, and `.scratchpad/` into the destination, normally `.atomic/`. | Tracked files are staged as renames. Ignored files move on disk without becoming tracked. |
| Claude-native repository files | Settings, hooks, unrelated rules, and existing `.claude/worktrees/` remain in place. | Do not move or delete the entire `.claude` directory. |
| Wiki cards | For an in-repository state root, Claude uses `.claude/rules/atomic` to reach the canonical rules. | Review the relative symlink with the state changes. Hand-wired references to moved paths may need editing. |
| Code index | The old index is rebuildable; migration can remove it when it is not in use. | Run `atomic code index` when a fresh index is needed. |

The move is local to each checkout. Linked worktrees can therefore use different state locations until each one is migrated. Repository migration attempts to undo failed moves, but it does not create the install lifecycle's durable journal or a separate backup.

## Keep a custom state location


A legacy root named only by `harness.dir`, such as `.team-state`, is not a candidate in 7.0. The automatic migration source is `.claude`; `atomic state migrate` does not find or move a custom legacy root.

After installing the new binary, select the existing root from inside that repository:

    atomic state adopt --dir .team-state
    atomic where --json

This records the location for the clone without moving files. Repeat it for independently cloned repositories that need the same selection. To make the directory name your user-level default for repositories instead, use:

    atomic config set state.dir .team-state

Use the actual directory name you recorded before upgrading. Confirm `state_root` before creating new state; an existing populated candidate or a saved selection can take precedence over the user default. See the [resolution order](/reference/concepts#state-root-resolution-order).

## Resolve a blocked migration


| Report | Resolution |
|--------|------------|
| A proposed `CLAUDE.md` or `.atomic-merged` candidate is pending | Review and finish the merge, or remove the candidate only if you no longer need it. Rerun `atomic update`. |
| Atomic's steering tags are ambiguous | Repair the named managed block while preserving personal text, then rerun the update. |
| Both `.claude` and the destination contain Atomic state | Inspect both copies and merge the named entries manually. Migration refuses to overwrite them. |
| A destination file, rule link, or Git index entry conflicts | Resolve the named collision or unmerged index entry, then rerun `atomic state migrate` in that checkout. |
| A global lifecycle journal is unresolved | Inspect `atomic harness recover --dry-run`. Use normal recovery to finish, or `--rollback` to restore the verified pre-mutation bytes when that is the intended result. |
| Configuration or the ownership ledger cannot be read | Repair the named file. Rerun `atomic update` for adoption, or `atomic harness repair` for an already-enrolled target. |

Do not delete journals or ownership records to bypass a conflict. `atomic harness recover --rollback` addresses an interrupted install operation; it does not undo a completed major-version upgrade or the repository state move.

## Choose the right migration command


| Command | Purpose |
|---------|---------|
| `atomic update` | Update the binary, adopt the legacy global Claude install once, and refresh enrolled targets. It also attempts state migration in the current repository. |
| `atomic claude update` | After a manual binary replacement, complete the legacy default-root adoption and refresh. Explicit `--target` and `--dry-run` retain their narrower behavior. |
| `atomic harness repair` | Refresh targets already in the ownership ledger. It does not replace first-time adoption. |
| `atomic state migrate` | Move Atomic's repository state out of `.claude` in the current checkout. |
| `atomic state adopt --dir <path>` | Select an existing state location without moving it. |
| `atomic migrate` | Apply registered versioned transforms. It does not perform the new harness adoption or repository-root move by itself. |

The `--skip-claude-update` flag skips target adoption and convergence. On the 7.0 updater it does not suppress the separate repository-state migration or registered migration steps, so it is not a general binary-only or dry-run option.

## Removed interfaces and optional harnesses


- `/remind-me` is retired; adoption backs up and removes its installed command file. The upgrade does not convert reminders or external schedules into another workflow.
- `atomic config resolve`, `ATOMIC_HARNESS`, and `harness.dir` are retired. Update scripts that use them. Use `atomic where --json` for state paths and `atomic config agents --harness <claude|omp|codex>` for supported agent configuration.
- Experimental `[pi.agents]` overrides are retired without migration and are dropped on a subsequent configuration write. They do not configure OMP.
- `atomic followups` rejects unexpected arguments instead of silently ignoring them. Flags after the ID in `atomic followups close <id> --reason <text>` now apply.

Install OMP or Codex separately if you want to use them:

    atomic install --harness omp
    CODEX_HOME="$HOME/.codex" atomic install --harness codex

The [installation guide](/guides/install#harnesses-and-targets) describes their discovery rules and delivery limits. Codex hook trust remains a manual review in `/hooks`. The [capability matrix](/research/harness-capability-matrix) distinguishes observed runtime behavior from unsupported or partially verified surfaces.

## Return to 6.10


Reinstalling the old binary does not reverse adoption, restore replaced artifacts, or move `.atomic` state back under `.claude`. Keep the pre-upgrade backups and review any work done since the upgrade before restoring configuration or reverting repository changes. Restore the matching binary, installed artifacts, and repository layout together; the install journal's rollback command is not a version downgrade tool.

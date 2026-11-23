---
description: Audit and bootstrap wiki setup. Apply only approved additions; never overwrite, stage, or commit.
---

## Select scope


This command bootstraps a repo wiki in a Git worktree. Outside Git, stop and route realm capture or bucket setup to the `atomic-wiki` skill. `/refresh-wiki` writes the repo `<wiki-type>` marker.

## Audit and propose


Print each item's status and classify setup as fresh (no conventions or only an empty `.gitignore`), partial, or complete.

| Inspect | Propose when missing or outdated |
|---|---|
| `.gitignore`, `tmp/`, state and worktree ignores from `atomic where --json` | Run `atomic repo init` for missing state or ignore rules. Preserve existing rules. |
| Root steering file | Draft it using the survey and the active mode in `atomic-wiki/references/repo-output.md`. |
| `docs/`, `docs/design/` | Create the missing design directory with `.gitkeep`. New specs live in task scratchpads. |
| Root `README.md` | Offer a stub: repo name, one-line purpose placeholder, Install, Usage, License. |
| Session hooks | In Claude Code, inspect the project `SessionStart` entries for `atomic hooks session-start`; propose `atomic hooks install --scope project` for a missing or legacy entry. For OMP or Codex, use the harness setup reported by `atomic doctor`. |
| `docs/wiki/index.md` | Suggest `/refresh-wiki`; do not invoke it. |
| Router ref in active root steering | If the index exists, offer to wire it using `atomic-wiki/references/repo-output.md`. |
| Root `.signalsignore` | Point to `atomic migrate --repo .`; never hand-convert it. |
| Wiki steering file | Run `atomic wiki init --scope repo`. Relay `created <path>` and explain that it steers inference. |

If complete, print `repo already atomic-ready. nothing to do.` and stop. Otherwise number the proposed actions. Accept space- or comma-separated indices, ranges, `all`, `none`, or `all except N`; re-prompt for invalid indices. Apply approved actions in order. Recheck existence before creating files. Stop on failure and name the failed action.

Only suggest `[scan]` exclusions for committed paths the audit found. `ignore` removes paths from the scan; `generated` keeps them in the tree but skips their contents. Write neither without approval of the specific paths.

## Survey for new steering


Use wiki index and scan, README, manifests, CI and build files, recent commit subjects, and `HACK`, `FIXME`, `XXX`, or `WORKAROUND` comments. Stop reading when you have enough evidence. Record project-specific facts; do not repeat global rules or workflow.

| Section | Draft from | If unclear |
|---|---|---|
| What this is | README and manifest description | Ask what it does and who uses and maintains it. |
| Scope boundary | Supported platforms and CI matrix | Ask what it excludes. |
| Tribal knowledge | Workarounds and unusual layout | No surprising patterns detected. |
| Project rules | Commit history, lint, and CI gates | No repo-specific rules detected beyond global defaults. |
| Processes | Build, CI, release scripts, and contributing guide | No release, rollback, or on-call process detected. |
| External references | README links to trackers, runbooks, chat, or dashboards | No external references detected. |

For each section, offer Accept or Edit. Skip a section only when no supported draft exists. Write the approved sections to the active steering file. If the wiki index exists, follow the router wiring contract in `atomic-wiki/references/repo-output.md` and leave any working ref in place.

## Report


List applied and skipped actions, survey dispositions, and detected scope. Suggest `/refresh-wiki` if signals are missing and `/atomic-plan` when the user is ready to plan. Do not stage, commit, or write a scratchpad.

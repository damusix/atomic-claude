---
generated_at: 2026-05-23T19:26:57Z
atomic_version: 1.3.0
---
# Deterministic signals

## Tree

├── .claude/ (3)
│   ├── docs/ (3)
│   │   ├── agent-config.md
│   │   ├── axioms.md
│   │   └── claude-code-references.md
│   ├── skills/ (1)
│   │   └── atomic-cli-contrib/ (1)
│   │       └── SKILL.md
│   └── settings.local.json
├── .githooks/ (1)
│   └── pre-commit
├── .github/ (1)
│   └── workflows/ (3)
│       ├── ci.yml
│       ├── release-please.yml
│       └── release.yml
├── agents/ (9)
│   ├── atomic-builder.md
│   ├── atomic-claude-merger.md
│   ├── atomic-git-scout.md
│   ├── atomic-haiku.md
│   ├── atomic-investigator.md
│   ├── atomic-reviewer.md
│   ├── atomic-signals-inferrer.md
│   ├── atomic-strategist.md
│   └── atomic-surgeon.md
├── assets/ (1)
│   └── atomic-claude.png
├── atomic/ (7)
│   ├── cmd/ (3)
│   │   ├── atomic/ (2)
│   │   │   ├── main.go
│   │   │   └── main_test.go
│   │   ├── bundle-mirror/ (2)
│   │   │   ├── main.go
│   │   │   └── main_test.go
│   │   └── render-templates/ (1)
│   │       └── main.go
│   ├── internal/ (22)
│   │   ├── bundlemirror/ (1)
│   │   │   └── mirror.go
│   │   ├── bundlespec/ (2)
│   │   │   ├── bundlespec.go
│   │   │   └── bundlespec_test.go
│   │   ├── claudeinstall/ (2)
│   │   │   ├── install.go
│   │   │   └── install_test.go
│   │   ├── config/ (8)
│   │   │   ├── cli.go
│   │   │   ├── cli_test.go
│   │   │   ├── config.go
│   │   │   ├── config_test.go
│   │   │   ├── paths.go
│   │   │   ├── paths_test.go
│   │   │   ├── render.go
│   │   │   └── render_test.go
│   │   ├── dockerinit/ (4)
│   │   │   ├── templates/ (4 subitems) (4 total items)
│   │   │   ├── convergence_test.go
│   │   │   ├── dockerinit.go
│   │   │   └── dockerinit_test.go
│   │   ├── doctor/ (35)
│   │   │   ├── checks_binary.go
│   │   │   ├── checks_binary_test.go
│   │   │   ├── checks_config.go
│   │   │   ├── checks_config_test.go
│   │   │   ├── checks_followups.go
│   │   │   ├── checks_followups_test.go
│   │   │   ├── checks_hooks.go
│   │   │   ├── checks_hooks_test.go
│   │   │   ├── checks_install.go
│   │   │   ├── checks_install_test.go
│   │   │   ├── checks_manifest.go
│   │   │   ├── checks_manifest_test.go
│   │   │   ├── checks_memory.go
│   │   │   ├── checks_memory_test.go
│   │   │   ├── checks_refs.go
│   │   │   ├── checks_refs_test.go
│   │   │   ├── checks_signals.go
│   │   │   ├── checks_signals_test.go
│   │   │   ├── doctor.go
│   │   │   ├── doctor_test.go
│   │   │   ├── exit.go
│   │   │   ├── exit_test.go
│   │   │   ├── fix.go
│   │   │   ├── fix_impls.go
│   │   │   ├── fix_test.go
│   │   │   ├── flags.go
│   │   │   ├── format.go
│   │   │   ├── format_test.go
│   │   │   ├── inode_unix.go
│   │   │   ├── inode_windows.go
│   │   │   ├── repodev.go
│   │   │   ├── repodev_test.go
│   │   │   ├── shortcircuit.go
│   │   │   ├── shortcircuit_test.go
│   │   │   └── stdin_prompter.go
│   │   ├── embedded/ (3)
│   │   │   ├── bundle/ (6 subitems) (52 total items)
│   │   │   ├── bundle.go
│   │   │   └── manifest.go
│   │   ├── followups/ (17)
│   │   │   ├── testdata/ (3 subitems) (5 total items)
│   │   │   ├── add.go
│   │   │   ├── add_test.go
│   │   │   ├── cli.go
│   │   │   ├── cli_test.go
│   │   │   ├── close_entry.go
│   │   │   ├── close_test.go
│   │   │   ├── closed.go
│   │   │   ├── closed_test.go
│   │   │   ├── entry.go
│   │   │   ├── entry_test.go
│   │   │   ├── list.go
│   │   │   ├── list_test.go
│   │   │   ├── migrate.go
│   │   │   ├── migrate_test.go
│   │   │   ├── render.go
│   │   │   └── render_test.go
│   │   ├── frontmatter/ (2)
│   │   │   ├── frontmatter.go
│   │   │   └── frontmatter_test.go
│   │   ├── hooks/ (3)
│   │   │   ├── hooks.go
│   │   │   ├── hooks_hujson.go
│   │   │   └── hooks_test.go
│   │   ├── ids/ (2)
│   │   │   ├── ids.go
│   │   │   └── ids_test.go
│   │   ├── manifestcheck/ (2)
│   │   │   ├── manifestcheck.go
│   │   │   └── manifestcheck_test.go
│   │   ├── mdparse/ (2)
│   │   │   ├── mdparse.go
│   │   │   └── mdparse_test.go
│   │   ├── prompt/ (2)
│   │   │   ├── prompt.go
│   │   │   └── prompt_test.go
│   │   ├── reminder/ (2)
│   │   │   ├── reminder.go
│   │   │   └── reminder_test.go
│   │   ├── repoctx/ (2)
│   │   │   ├── repoctx.go
│   │   │   └── repoctx_test.go
│   │   ├── selfupdate/ (4)
│   │   │   ├── cache.go
│   │   │   ├── selfupdate.go
│   │   │   ├── selfupdate_test.go
│   │   │   └── semver.go
│   │   ├── signals/ (7)
│   │   │   ├── testdata/ (1 subitem) (7 total items)
│   │   │   ├── diff.go
│   │   │   ├── languages.go
│   │   │   ├── manifests.go
│   │   │   ├── signals.go
│   │   │   ├── signals_test.go
│   │   │   └── tree.go
│   │   ├── templaterender/ (2)
│   │   │   ├── templaterender.go
│   │   │   └── templaterender_test.go
│   │   ├── updatedoctor/ (2)
│   │   │   ├── updatedoctor.go
│   │   │   └── updatedoctor_test.go
│   │   ├── validate/ (14)
│   │   │   ├── testdata/ (2 subitems) (26 total items)
│   │   │   ├── bundle.go
│   │   │   ├── config.go
│   │   │   ├── config_test.go
│   │   │   ├── dispatch.go
│   │   │   ├── dispatch_test.go
│   │   │   ├── finding.go
│   │   │   ├── output.go
│   │   │   ├── output_test.go
│   │   │   ├── repo.go
│   │   │   ├── spec.go
│   │   │   ├── spec_test.go
│   │   │   ├── validate.go
│   │   │   └── validate_test.go
│   │   └── version/ (1)
│   │       └── version.go
│   ├── test/ (1)
│   │   └── install_sh_test.go
│   ├── CHANGELOG.md
│   ├── Makefile
│   ├── go.mod
│   └── go.sum
├── commands/ (32)
│   ├── _templates/ (2)
│   │   ├── implementer-prompt.md
│   │   └── reviewer-prompt.md
│   ├── atomic-claude-merge.md
│   ├── atomic-compress.md
│   ├── atomic-help.md
│   ├── atomic-plan.md
│   ├── atomic-setup.md
│   ├── commit-and-merge.md
│   ├── commit-and-pr.md
│   ├── commit-and-push.md
│   ├── commit-and-squash.md
│   ├── commit-only.md
│   ├── documentation.md
│   ├── follow-up.md
│   ├── git-cleanup.md
│   ├── initialize-signals.md
│   ├── merge-to-main.md
│   ├── pr-only.md
│   ├── pressure-test.md
│   ├── push-only.md
│   ├── refresh-signals.md
│   ├── remind-me.md
│   ├── report-issue-with-atomic.md
│   ├── report-issue.md
│   ├── review-branch.md
│   ├── session-report.md
│   ├── squash-and-merge.md
│   ├── squash-only.md
│   ├── subagent-diagnose.md
│   ├── subagent-implementation.md
│   ├── undo-commit.md
│   ├── watch-ci.md
│   └── worktree-start.md
├── docs/ (5)
│   ├── design/ (6)
│   │   ├── artifact-templates.md
│   │   ├── atomic-doctor.md
│   │   ├── atomic-state-and-config.md
│   │   ├── atomic-validate.md
│   │   ├── diagnose-orchestrators.md
│   │   └── signals-router.md
│   ├── guides/ (3)
│   │   ├── contributing.md
│   │   ├── evaluations.md
│   │   └── install.md
│   ├── reference/ (7)
│   │   ├── agents.md
│   │   ├── commands.md
│   │   ├── conventions.md
│   │   ├── output-style.md
│   │   ├── signals-workflow.md
│   │   ├── skills.md
│   │   └── workflow.md
│   ├── spec/ (18)
│   │   ├── artifact-templates.md
│   │   ├── atomic-binary.md
│   │   ├── atomic-doctor.md
│   │   ├── atomic-plan.md
│   │   ├── atomic-setup.md
│   │   ├── atomic-state-and-config.md
│   │   ├── atomic-update-doctor.md
│   │   ├── atomic-validate.md
│   │   ├── cron-workflow.md
│   │   ├── docker-eval-environment.md
│   │   ├── documentation-skill-split.md
│   │   ├── follow-ups-folder.md
│   │   ├── install-workflow.md
│   │   ├── session-report.md
│   │   ├── signals-project-detection.md
│   │   ├── signals-router.md
│   │   ├── signals-workflow.md
│   │   └── subagent-diagnose.md
│   └── credits.md
├── output-styles/ (1)
│   └── atomic.md
├── rules/ (2)
│   ├── python/ (1)
│   │   └── style.md
│   └── typescript/ (1)
│       └── style.md
├── scripts/ (1)
│   └── link-local.sh
├── skills/ (8)
│   ├── atomic-commit/ (1)
│   │   └── SKILL.md
│   ├── atomic-debug/ (1)
│   │   └── SKILL.md
│   ├── atomic-documentation/ (1)
│   │   └── SKILL.md
│   ├── atomic-prose/ (1)
│   │   └── SKILL.md
│   ├── atomic-review/ (1)
│   │   └── SKILL.md
│   ├── atomic-signals/ (1)
│   │   └── SKILL.md
│   ├── atomic-tdd/ (1)
│   │   └── SKILL.md
│   └── atomic-verify/ (1)
│       └── SKILL.md
├── templates/ (2)
│   ├── commands/ (31)
│   │   ├── atomic-claude-merge.md
│   │   ├── atomic-compress.md
│   │   ├── atomic-help.md
│   │   ├── atomic-plan.md
│   │   ├── atomic-setup.md
│   │   ├── commit-and-merge.md
│   │   ├── commit-and-pr.md
│   │   ├── commit-and-push.md
│   │   ├── commit-and-squash.md
│   │   ├── commit-only.md
│   │   ├── documentation.md
│   │   ├── follow-up.md
│   │   ├── git-cleanup.md
│   │   ├── initialize-signals.md
│   │   ├── merge-to-main.md
│   │   ├── pr-only.md
│   │   ├── pressure-test.md
│   │   ├── push-only.md
│   │   ├── refresh-signals.md
│   │   ├── remind-me.md
│   │   ├── report-issue-with-atomic.md
│   │   ├── report-issue.md
│   │   ├── review-branch.md
│   │   ├── session-report.md
│   │   ├── squash-and-merge.md
│   │   ├── squash-only.md
│   │   ├── subagent-diagnose.md
│   │   ├── subagent-implementation.md
│   │   ├── undo-commit.md
│   │   ├── watch-ci.md
│   │   └── worktree-start.md
│   └── shared/ (10)
│       ├── base-resolution.md
│       ├── commit-flow.md
│       ├── doc-impact-why.md
│       ├── doc-impact.md
│       ├── merge-flow.md
│       ├── pr-flow.md
│       ├── push-flow.md
│       ├── signals-gate.md
│       ├── squash-flow.md
│       └── worktree-cleanup-prompt.md
├── tmp/ (2)
│   ├── claude-home/ (1)
│   │   └── .gitkeep
│   └── workspace/ (1)
│       └── .gitkeep
├── .dockerignore
├── .gitignore
├── .goreleaser.yaml
├── CLAUDE.md
├── Dockerfile
├── LICENSE
├── Makefile
├── README.md
├── claude.local.md
├── docker-compose.yml
├── docker-entrypoint.sh
├── install.sh
├── release-please-config.json
└── release-please-manifest.json

## Manifests

- atomic/go.mod: module=github.com/damusix/atomic-claude/atomic, go=1.23.0
- atomic/internal/signals/testdata/signals/multilang/repo/go.mod: module=github.com/example/test, go=1.22

## Languages

- Go: 26997 LOC (51%), 121 files (34%)
- Markdown: 25270 LOC (47%), 222 files (63%)
- Shell: 269 LOC (0%), 3 files (0%)
- TypeScript: 100 LOC (0%), 1 file (0%)
- Python: 30 LOC (0%), 1 file (0%)

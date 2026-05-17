---
generated_at: 2026-05-17T08:35:58Z
atomic_version: 1.0.0
---
# Deterministic signals

## Tree

├── .claude/ (2)
│   ├── docs/ (3)
│   │   ├── agent-config.md
│   │   ├── axioms.md
│   │   └── claude-code-references.md
│   └── settings.local.json
├── .github/ (1)
│   └── workflows/ (3)
│       ├── ci.yml
│       ├── release-please.yml
│       └── release.yml
├── agents/ (6)
│   ├── atomic-builder.md
│   ├── atomic-git-scout.md
│   ├── atomic-investigator.md
│   ├── atomic-reviewer.md
│   ├── atomic-signals-inferrer.md
│   └── atomic-surgeon.md
├── atomic/ (7)
│   ├── cmd/ (2)
│   │   ├── atomic/ (2)
│   │   │   ├── main.go
│   │   │   └── main_test.go
│   │   └── bundle-mirror/ (2)
│   │       ├── main.go
│   │       └── main_test.go
│   ├── internal/ (11)
│   │   ├── bundlemirror/ (1)
│   │   │   └── mirror.go
│   │   ├── claudeinstall/ (2)
│   │   │   ├── install.go
│   │   │   └── install_test.go
│   │   ├── embedded/ (3)
│   │   │   ├── bundle/ (6 subitems) (36 total items)
│   │   │   ├── bundle.go
│   │   │   └── manifest.go
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
│   │   ├── signals/ (6)
│   │   │   ├── testdata/ (1 subitem) (7 total items)
│   │   │   ├── languages.go
│   │   │   ├── manifests.go
│   │   │   ├── signals.go
│   │   │   ├── signals_test.go
│   │   │   └── tree.go
│   │   └── version/ (1)
│   │       └── version.go
│   ├── test/ (1)
│   │   └── install_sh_test.go
│   ├── CHANGELOG.md
│   ├── Makefile
│   ├── go.mod
│   └── go.sum
├── commands/ (21)
│   ├── _templates/ (2)
│   │   ├── implementer-prompt.md
│   │   └── reviewer-prompt.md
│   ├── atomic-compress.md
│   ├── atomic-plan.md
│   ├── atomic-setup.md
│   ├── commit-and-merge.md
│   ├── commit-and-pr.md
│   ├── commit-and-squash.md
│   ├── commit-only.md
│   ├── documentation.md
│   ├── follow-up.md
│   ├── git-cleanup.md
│   ├── initialize-signals.md
│   ├── merge-to-main.md
│   ├── pr-only.md
│   ├── refresh-signals.md
│   ├── remind-me.md
│   ├── report-issue.md
│   ├── squash-and-merge.md
│   ├── squash-only.md
│   ├── subagent-implementation.md
│   └── worktree-start.md
├── docs/ (1)
│   └── spec/ (5)
│       ├── atomic-binary.md
│       ├── cron-workflow.md
│       ├── install-workflow.md
│       ├── signals-project-detection.md
│       └── signals-workflow.md
├── output-styles/ (1)
│   └── atomic.md
├── rules/ (2)
│   ├── python/ (1)
│   │   └── style.md
│   └── typescript/ (1)
│       └── style.md
├── scripts/ (1)
│   └── link-local.sh
├── skills/ (6)
│   ├── atomic-commit/ (1)
│   │   └── SKILL.md
│   ├── atomic-debug/ (1)
│   │   └── SKILL.md
│   ├── atomic-review/ (1)
│   │   └── SKILL.md
│   ├── atomic-signals/ (1)
│   │   └── SKILL.md
│   ├── atomic-tdd/ (1)
│   │   └── SKILL.md
│   └── atomic-verify/ (1)
│       └── SKILL.md
├── .gitignore
├── .goreleaser.yaml
├── README.md
├── claude.local.md
├── claude.md
├── install.sh
├── release-please-config.json
└── release-please-manifest.json

## Manifests

- atomic/go.mod: module=github.com/damusix/atomic-claude/atomic, go=1.23
- atomic/internal/signals/testdata/signals/multilang/repo/go.mod: module=github.com/example/test, go=1.22

## Languages

- Markdown: 9910 LOC (52%), 87 files (70%)
- Go: 8680 LOC (45%), 32 files (26%)
- Shell: 246 LOC (1%), 2 files (1%)
- TypeScript: 100 LOC (0%), 1 file (0%)
- Python: 30 LOC (0%), 1 file (0%)

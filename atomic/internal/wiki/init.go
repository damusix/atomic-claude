package wiki

import (
	"os"
	"path/filepath"
)

// repoSteeringScaffold is the fixed-content docs/wiki/CLAUDE.md scaffold for
// repo scope. Verbatim from the former heredocs in
// templates/commands/refresh-wiki.md R3 and
// skills/atomic-wiki/references/repo.md Step 8c — byte-identical, do not
// reformat or reword.
const repoSteeringScaffold = `---
type: Steering
description: Authoritative steering for the signals/wiki inferrer when operating under docs/wiki/.
---

<steering note: user hints to correct framework detection / domain grouping / build-test commands;
 the inferrer reads this and treats it as authoritative>

## Framework
# NestJS monorepo (not plain Express)

## Domains
# - src/billing/ and src/payments/ are one domain ("payments")
# - src/internal-tools/ is scratch code — not a real domain

## Build
# - Build: pnpm turbo build
# - Test: pnpm test:ci (not pnpm test — that runs watch mode)

## Ignore for domains
# - vendor/
# - generated/
`

// realmSteeringScaffold is the fixed-content <root>/wiki/CLAUDE.md scaffold
// for realm scope: a self-referencing memory file so cd'ing directly into the
// realm's wiki/ directory auto-loads index.md at session start (symmetric
// with a repo scope member's own root CLAUDE.md carrying @docs/wiki/index.md).
const realmSteeringScaffold = "@index.md\n"

// InitRepoScope writes <root>/docs/wiki/CLAUDE.md with the repo-scope
// steering scaffold, creating missing parent directories, via the same
// writeFileAtomic idiom RegisterWiki uses. No-op when the file already
// exists. Returns whether the file was newly created.
func InitRepoScope(root string) (bool, error) {
	return initScaffold(filepath.Join(root, "docs", "wiki", "CLAUDE.md"), repoSteeringScaffold)
}

// InitRealmScope writes <root>/wiki/CLAUDE.md containing only "@index.md",
// creating missing parent directories. No-op when the file already exists.
// Returns whether the file was newly created.
func InitRealmScope(root string) (bool, error) {
	return initScaffold(filepath.Join(root, "wiki", "CLAUDE.md"), realmSteeringScaffold)
}

// initScaffold writes content to path via writeFileAtomic if the file is
// absent. Returns (created, error); created is false and error is nil when
// the file already exists — the caller treats that as a deliberate no-op.
func initScaffold(path, content string) (bool, error) {
	if _, err := os.Lstat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := writeFileAtomic(path, []byte(content)); err != nil {
		return false, err
	}
	return true, nil
}

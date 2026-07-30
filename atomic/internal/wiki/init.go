package wiki

import (
	"os"
	"path/filepath"
)

// repoSteeringScaffold is the fixed-content docs/wiki/CLAUDE.md scaffold for
// repo scope. Two constraints shape the examples: they live inside HTML
// comments so markdown rendering can't promote them to headings, and they
// name no concrete framework, tool, or path — the file loads verbatim into
// model context as nested memory, so even a commented concrete name (an
// earlier scaffold said "NestJS monorepo") reads as a fact about the repo
// and derails inferrer runs on every uncustomized checkout.
const repoSteeringScaffold = `---
type: Steering
description: Authoritative steering for the signals/wiki inferrer when operating under docs/wiki/.
---

<!-- steering note: user hints to correct framework detection / domain grouping / build-test
 commands; the inferrer reads this and treats it as authoritative. The sections below start
 empty — fill them with facts about THIS repo. Other HTML comments are illustrative examples
 only; the inferrer must never treat them as steering. This note is an HTML comment, not a
 <pseudo-tag>: docs/ directories swept by VitePress feed every .md through the Vue template
 compiler, which rejects pseudo-tag syntax and fails the site build. -->

## Framework

<!-- example: <the real framework> (not <what detection wrongly guessed>) -->

## Domains

<!-- example:
- <dir-a>/ and <dir-b>/ are one domain ("<domain-name>")
- <dir-c>/ is scratch code — not a real domain
-->

## Build

<!-- example:
- Build: <build command>
- Test: <ci test command> (not <the watch-mode command>)
-->

## Ignore for domains

<!-- example:
- <vendored-dir>/
- <generated-output-dir>/
-->
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

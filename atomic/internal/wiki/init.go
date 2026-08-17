package wiki

import (
	"os"
	"path/filepath"
)

// repoSteeringScaffold's examples sit in HTML comments so markdown rendering
// cannot promote them to headings, and they name no concrete framework or
// path: this file loads verbatim into model context, so even a commented
// concrete name reads as a fact about the repo and derails the inferrer on
// every uncustomized checkout.
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

// realmSteeringScaffold self-references so cd'ing straight into the realm's
// wiki/ auto-loads index.md, mirroring a member repo's root CLAUDE.md.
const realmSteeringScaffold = "@index.md\n"

// InitRepoScope writes the repo-scope steering scaffold, reporting whether it
// created the file. An existing file is a no-op.
func InitRepoScope(root string) (bool, error) {
	return initScaffold(filepath.Join(root, "docs", "wiki", "CLAUDE.md"), repoSteeringScaffold)
}

// InitRealmScope writes the realm-scope scaffold, reporting whether it created
// the file. An existing file is a no-op.
func InitRealmScope(root string) (bool, error) {
	return initScaffold(filepath.Join(root, "wiki", "CLAUDE.md"), realmSteeringScaffold)
}

// initScaffold reports (false, nil) when the file already exists — a
// deliberate no-op, not a failure.
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

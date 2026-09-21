package wiki

import (
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
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
// wiki/ auto-loads index.md: the pair's AGENTS.md carries the import, and the
// CLAUDE.md loader beside it imports AGENTS.md.
const realmSteeringScaffold = "@index.md\n"

// InitRepoScope writes the repo-scope steering loader pair — the shared
// docs/wiki/AGENTS.md scaffold and the thin docs/wiki/CLAUDE.md loader beside it
// — and reports the paths it created. An existing CLAUDE.md is a no-op: an
// install created before the pair existed keeps its bytes and loads directly.
func InitRepoScope(root string) ([]string, error) {
	return initSteeringPair(filepath.Join(root, "docs", "wiki"), repoSteeringScaffold)
}

// InitRealmScope writes the realm wiki's steering loader pair at <root>/wiki,
// reporting the paths it created. An existing CLAUDE.md is a no-op.
func InitRealmScope(root string) ([]string, error) {
	return initSteeringPair(filepath.Join(root, "wiki"), realmSteeringScaffold)
}

// initSteeringPair writes bundlespec.ScopeSteering's pair into dir: the authored
// AGENTS.md carrying the scope's guidance and the adjacent thin CLAUDE.md loader
// that imports it. An existing CLAUDE.md is left byte-identical and nothing else
// is written, so a CLAUDE.md-direct install neither gains a duplicate guidance
// file nor loses the loader-less shape it already loads through. An existing
// AGENTS.md is preserved too; only the missing half of the pair is written.
func initSteeringPair(dir, guidance string) ([]string, error) {
	claudePath := filepath.Join(dir, bundlespec.ScopeSteering.ClaudeTarget)
	if _, err := os.Lstat(claudePath); err == nil {
		return nil, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	var created []string
	agentsPath := filepath.Join(dir, bundlespec.ScopeSteering.Source)
	if _, err := os.Lstat(agentsPath); os.IsNotExist(err) {
		if err := writeFileAtomic(agentsPath, []byte(guidance)); err != nil {
			return nil, err
		}
		created = append(created, agentsPath)
	} else if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(claudePath, bundlespec.ScopeSteering.LoaderDocument()); err != nil {
		return nil, err
	}
	return append(created, claudePath), nil
}

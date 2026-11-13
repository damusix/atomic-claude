package wiki

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
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

// InitRepoScope writes the repo-scope steering pair under docs/wiki in the
// root's Claude steering mode and reports the paths it created or changed.
func InitRepoScope(root string) ([]string, error) {
	return initScopePair(root, filepath.Join(root, "docs", "wiki"), repoSteeringScaffold)
}

// InitRealmScope writes the realm wiki's steering pair at <root>/wiki in the
// root's Claude steering mode, reporting the paths it created or changed.
func InitRealmScope(root string) ([]string, error) {
	return initScopePair(root, filepath.Join(root, "wiki"), realmSteeringScaffold)
}

// initScopePair resolves root's steering mode, so an unknown or unreadable
// steering.claude fails the write instead of scaffolding the wrong shape.
func initScopePair(root, dir, scaffold string) ([]string, error) {
	mode, err := config.ResolveSteeringMode(config.ProjectHome(), root)
	if err != nil {
		return nil, err
	}
	changed, _, err := initSteeringPair(dir, scaffold, mode)
	return changed, err
}

// ConvertSteeringPair reshapes the wiki and root pairs to mode. claude.ConvertScope
// reshapes only pairs with an <atomic> block, so the root pair covers a block-less AGENTS.md.
func ConvertSteeringPair(root, scope string, mode bundlespec.SteeringMode) (changed, notes []string, err error) {
	type pair struct{ dir, scaffold string }
	wikiPair := pair{filepath.Join(root, "docs", "wiki"), repoSteeringScaffold}
	if scope == "realm" {
		wikiPair = pair{filepath.Join(root, "wiki"), realmSteeringScaffold}
	}
	pairs := []pair{wikiPair, {root, ""}}
	steering := bundlespec.ScopeSteering
	for _, p := range pairs {
		_, _, agentsExists, err := readSteeringFile(p.dir, steering.Source)
		if err != nil {
			return changed, notes, err
		}
		_, _, claudeExists, err := readSteeringFile(p.dir, steering.ClaudeTarget)
		if err != nil {
			return changed, notes, err
		}
		if !agentsExists && !claudeExists {
			continue
		}
		// Conversion runs on every repo init and update, so creating a loader here
		// would drop a CLAUDE.md into any repo that has an AGENTS.md.
		if p.scaffold == "" && mode == bundlespec.SteeringLoader && !claudeExists {
			continue
		}
		c, n, err := initSteeringPair(p.dir, p.scaffold, mode)
		changed, notes = append(changed, c...), append(notes, n...)
		if err != nil {
			return changed, notes, err
		}
	}
	return changed, notes, nil
}

// hasSteeringFiles reports whether dir already holds an AGENTS.md or CLAUDE.md.
func hasSteeringFiles(dir string) bool {
	for _, name := range []string{bundlespec.ScopeSteering.Source, bundlespec.ScopeSteering.ClaudeTarget} {
		if _, _, exists, err := readSteeringFile(dir, name); exists || err != nil {
			return true
		}
	}
	return false
}

// readSteeringFile reads dir/name, also trying name lowercased, which a CLAUDE.md
// is allowed to use. When absent it returns name's own path.
func readSteeringFile(dir, name string) (path string, data []byte, exists bool, err error) {
	for _, candidate := range []string{name, strings.ToLower(name)} {
		path = filepath.Join(dir, candidate)
		data, err = os.ReadFile(path)
		if err == nil {
			return path, data, true, nil
		}
		if !os.IsNotExist(err) {
			return path, nil, false, err
		}
	}
	return filepath.Join(dir, name), nil, false, nil
}

// initSteeringPair shapes dir's pair for mode, converting one left in another.
// User text is never copied between files; an edited AGENTS.md comes back as a note.
func initSteeringPair(dir, scaffold string, mode bundlespec.SteeringMode) (changed, notes []string, err error) {
	steering := bundlespec.ScopeSteering
	agentsPath, agents, agentsExists, err := readSteeringFile(dir, steering.Source)
	if err != nil {
		return nil, nil, err
	}
	claudePath, claude, claudeExists, err := readSteeringFile(dir, steering.ClaudeTarget)
	if err != nil {
		return nil, nil, err
	}

	loader := steering.LoaderDocument()
	var scaffoldBlock []byte
	if scaffold != "" {
		scaffoldBlock = managedfile.BracketedBlockDocument([]byte(scaffold))
	}
	hasOwned := steering.OwnsLoader(claude) || (scaffoldBlock != nil && managedfile.BlocksEqual(claude, scaffoldBlock))
	userClaude := string(claude)
	if hasOwned {
		rest, err := managedfile.RemoveBlock(claude)
		if err != nil {
			return nil, nil, err
		}
		userClaude = string(rest)
	}
	if scaffold != "" && userClaude == scaffold {
		userClaude = ""
	}
	user := strings.TrimSpace(userClaude) != ""
	agentsUnmodified := !agentsExists || string(agents) == scaffold
	// Under loader, a CLAUDE.md holding only Atomic's code-index block beside
	// AGENTS.md is Atomic's to extend with the loader, so Claude reads AGENTS.md.
	codeIndexOnly := mode == bundlespec.SteeringLoader && scaffold == "" && agentsExists && user &&
		strings.TrimSpace(StripCodeIndex(userClaude)) == ""
	if codeIndexOnly {
		user = false
	}

	withBlock := func(block []byte) []byte {
		switch {
		case hasOwned:
			out, _ := managedfile.ReplaceBlock(claude, block)
			return out
		case user || codeIndexOnly:
			return managedfile.AppendBlock(claude, block)
		}
		return block
	}
	write := func(path string, data []byte) error {
		if err := writeFileAtomic(path, data); err != nil {
			return err
		}
		changed = append(changed, path)
		return nil
	}
	remove := func(path string) error {
		if err := os.Remove(path); err != nil {
			return err
		}
		changed = append(changed, path)
		return nil
	}
	setClaude := func(data []byte) error {
		if claudeExists && bytes.Equal(claude, data) {
			return nil
		}
		return write(claudePath, data)
	}
	stripClaude := func() error {
		if !hasOwned && (user || !claudeExists) {
			return nil
		}
		if user {
			rest, _ := managedfile.RemoveBlock(claude)
			return write(claudePath, rest)
		}
		return remove(claudePath)
	}
	ensureAgents := func() error {
		if agentsExists || scaffold == "" {
			return nil
		}
		return write(agentsPath, []byte(scaffold))
	}

	switch mode {
	case bundlespec.SteeringLoader:
		if user && !hasOwned {
			return nil, nil, nil
		}
		if err := ensureAgents(); err != nil {
			return changed, notes, err
		}
		err = setClaude(withBlock(loader))
	case bundlespec.SteeringAgentsMD:
		if err := ensureAgents(); err != nil {
			return changed, notes, err
		}
		err = stripClaude()
	case bundlespec.SteeringClaudeMD:
		switch {
		case scaffoldBlock == nil:
			if strings.TrimSpace(string(agents)) != "" {
				notes = append(notes, fmt.Sprintf("%s stays in its current shape: %s holds your text, which Atomic will not copy into CLAUDE.md", dir, agentsPath))
			}
			return nil, notes, nil
		case user && !hasOwned:
			return nil, nil, nil
		case !agentsUnmodified:
			notes = append(notes, fmt.Sprintf("%s stays in its current shape: %s holds your text, which Atomic will not copy into CLAUDE.md", dir, agentsPath))
			return nil, notes, nil
		}
		if err = setClaude(withBlock(scaffoldBlock)); err == nil && agentsExists {
			err = remove(agentsPath)
		}
	}
	return changed, notes, err
}

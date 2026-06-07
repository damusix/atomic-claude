package wiki

// linkify.go — implements `atomic wiki linkify --root=<path>`.
//
// For each wiki artifact under <root>/wiki/:
//   - repos/<repo>(/<domain>).md → read `repo:` from YAML frontmatter; base = <root>/<repo>
//   - concerns/*.md and index.md → base = <root> (realm root)
//
// Files with a missing or unresolvable `repo:` frontmatter key are skipped
// (not crashed). The function is idempotent: re-running on already-linkified
// content is a no-op.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/mdlink"
)

// LinkifyWiki linkifies all wiki artifacts under <root>/wiki/ in-place.
// root is the realm root (the directory containing the wiki/ subdirectory).
func LinkifyWiki(root string) error {
	wikiDir := filepath.Join(root, "wiki")

	// Index file: base = realm root.
	indexPath := filepath.Join(wikiDir, "index.md")
	if err := linkifyWikiFile(indexPath, root); err != nil {
		return fmt.Errorf("wiki linkify index: %w", err)
	}

	// concerns/: base = realm root.
	concernsDir := filepath.Join(wikiDir, "concerns")
	if err := linkifyDir(concernsDir, root, false); err != nil {
		return fmt.Errorf("wiki linkify concerns: %w", err)
	}

	// repos/: each file has a `repo:` frontmatter key; base = <root>/<repo>.
	reposDir := filepath.Join(wikiDir, "repos")
	if err := linkifyReposDir(reposDir, root); err != nil {
		return fmt.Errorf("wiki linkify repos: %w", err)
	}

	return nil
}

// linkifyReposDir processes files under wiki/repos/. For each *.md file it
// reads the `repo:` frontmatter key to determine the base directory.
// Files without a `repo:` key are skipped silently.
func linkifyReposDir(reposDir, root string) error {
	entries, err := os.ReadDir(reposDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, e := range entries {
		if e.IsDir() {
			// Domain sub-directory: recurse, but we need the repo from the dir name.
			// Each sub-file has its own frontmatter.
			subDir := filepath.Join(reposDir, e.Name())
			if err := linkifyReposDir(subDir, root); err != nil {
				return err
			}
			continue
		}
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(reposDir, e.Name())
		base, err := repoBaseFromFile(path, root)
		if err != nil || base == "" {
			// No repo: key or unresolvable — skip.
			continue
		}
		if err := linkifyWikiFile(path, base); err != nil {
			return fmt.Errorf("linkify %s: %w", path, err)
		}
	}
	return nil
}

// repoBaseFromFile reads the `repo:` frontmatter value from path and returns
// the absolute path to that repo directory (joined with root). Returns ("", nil)
// when the key is absent.
func repoBaseFromFile(path, root string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	meta, _, err := frontmatter.Parse(string(raw))
	if err != nil || meta == nil {
		return "", nil
	}
	repoVal, ok := meta["repo"]
	if !ok {
		return "", nil
	}
	repoStr, ok := repoVal.(string)
	if !ok || repoStr == "" {
		return "", nil
	}
	base := filepath.Join(root, repoStr)
	if _, err := os.Stat(base); err != nil {
		// Repo directory doesn't exist — skip.
		return "", nil
	}
	return base, nil
}

// linkifyDir linkifies all *.md files in dir with base as the base directory.
// If recurse is true, it also descends into subdirectories.
func linkifyDir(dir, base string, recurse bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if recurse {
				if err := linkifyDir(path, base, recurse); err != nil {
					return err
				}
			}
			continue
		}
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if err := linkifyWikiFile(path, base); err != nil {
			return fmt.Errorf("linkify %s: %w", path, err)
		}
	}
	return nil
}

// linkifyWikiFile reads path, linkifies it with base, and writes back only if
// the content changed (idempotent).
func linkifyWikiFile(path, base string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	linkified := mdlink.LinkifyFile(string(raw), path, base)
	if linkified == string(raw) {
		return nil
	}
	return os.WriteFile(path, []byte(linkified), 0o644)
}

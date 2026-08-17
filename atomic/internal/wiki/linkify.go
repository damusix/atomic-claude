package wiki

// `atomic wiki linkify` resolves each artifact's links against a base
// directory: its own repo for a repos/ summary, the realm root for everything
// else. Idempotent — re-running on linkified content changes nothing.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/mdlink"
)

// LinkifyWiki rewrites every artifact under <root>/wiki/ in place. root is the
// realm root, the directory holding wiki/.
func LinkifyWiki(root string) error {
	wikiDir := filepath.Join(root, "wiki")

	indexPath := filepath.Join(wikiDir, "index.md")
	if err := linkifyWikiFile(indexPath, root); err != nil {
		return fmt.Errorf("wiki linkify index: %w", err)
	}

	concernsDir := filepath.Join(wikiDir, "concerns")
	if err := linkifyDir(concernsDir, root, false); err != nil {
		return fmt.Errorf("wiki linkify concerns: %w", err)
	}

	reposDir := filepath.Join(wikiDir, "repos")
	if err := linkifyReposDir(reposDir, root); err != nil {
		return fmt.Errorf("wiki linkify repos: %w", err)
	}

	return nil
}

// linkifyReposDir bases each summary on its own `repo:` frontmatter key,
// skipping files that lack one.
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
			// A domain-split summary; each file inside carries its own `repo:`.
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
			continue
		}
		if err := linkifyWikiFile(path, base); err != nil {
			return fmt.Errorf("linkify %s: %w", path, err)
		}
	}
	return nil
}

// repoBaseFromFile resolves the file's `repo:` key to a directory under root,
// returning ("", nil) when the key is absent or names a directory that is gone.
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
		return "", nil
	}
	return base, nil
}

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

// linkifyWikiFile writes back only on a real change, so mtimes stay put.
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

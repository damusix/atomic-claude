package wiki

import (
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// RealmLocation is a realm found for a directory. IndexPath is the realm's
// wiki/index.md, present or not.
type RealmLocation struct {
	Root      string
	IndexPath string
	Source    config.ScopeSource
}

// FindRealm prefers the nearest scope="realm" marker above dir, falling back
// to the wiki registry (see RegisteredIndexPaths for claudeMDPath).
func FindRealm(dir, claudeMDPath string) (RealmLocation, bool, error) {
	if root, found := config.FindScopeRoot(dir, "realm"); found {
		return RealmLocation{
			Root:      root,
			IndexPath: filepath.Join(root, "wiki", "index.md"),
			Source:    config.ScopeSourceMarker,
		}, true, nil
	}

	indexPaths, err := RegisteredIndexPaths(claudeMDPath)
	if err != nil {
		return RealmLocation{}, false, err
	}
	for _, indexPath := range indexPaths {
		root := filepath.Clean(filepath.Dir(filepath.Dir(indexPath)))
		if IsUnder(dir, root) {
			return RealmLocation{
				Root:      root,
				IndexPath: indexPath,
				Source:    config.ScopeSourceRegistry,
			}, true, nil
		}
	}
	return RealmLocation{}, false, nil
}

// IsUnder compares normalized path prefixes; it does not resolve symlinks.
func IsUnder(child, parent string) bool {
	child = filepath.Clean(child)
	parent = filepath.Clean(parent)
	if child == parent {
		return true
	}
	return strings.HasPrefix(child, parent+string(filepath.Separator))
}

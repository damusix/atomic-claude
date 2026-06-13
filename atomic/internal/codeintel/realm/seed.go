package realm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/wiki"
	"github.com/pelletier/go-toml/v2"
)

// SeedConfig seeds <realmRoot>/.atomic/code.toml from the <wiki-scan> block in
// wikiIndexPath.  It is called by the realm index verb when no code.toml exists.
//
// Seeding rules:
//   - key = basename of member path (slugged with a numeric suffix on collision).
//   - path = the wiki-relative member path (same value stored in the <wiki-scan> block).
//   - exclude = true when member status is "pending" OR the path starts with "trash/".
//
// Append-don't-overwrite: if code.toml already exists, only members not already
// present (by path) are appended.  Existing entries and manual edits are
// preserved byte-for-byte.
//
// Returns the resulting (possibly updated) Config.  If wikiIndexPath is absent
// or has no <wiki-scan> block, no file is written and nil Config is returned.
func SeedConfig(realmRoot, wikiIndexPath string) (*Config, error) {
	members, err := wiki.ReadScanMembers(wikiIndexPath)
	if err != nil {
		return nil, fmt.Errorf("realm seed: read scan members: %w", err)
	}
	if len(members) == 0 {
		// Nothing to seed from; leave code.toml absent.
		return nil, nil
	}

	// Load existing config (may be nil if code.toml absent).
	existing, err := LoadConfig(realmRoot)
	if err != nil {
		return nil, fmt.Errorf("realm seed: load existing config: %w", err)
	}

	// Build a set of paths already present so we don't duplicate them.
	presentPaths := make(map[string]bool)
	if existing != nil {
		for _, m := range existing.Members {
			presentPaths[m.Path] = true
		}
	}

	// Build a set of keys already present for slug-on-collision.
	usedKeys := make(map[string]bool)
	if existing != nil {
		for _, m := range existing.Members {
			usedKeys[m.Key] = true
		}
	}

	var toAppend []MemberEntry
	for _, wm := range members {
		if presentPaths[wm.Path] {
			continue // already in config
		}
		key := slugKey(filepath.Base(wm.Path), usedKeys)
		usedKeys[key] = true
		exclude := wm.Status == "pending" || isTrashPath(wm.Path)
		toAppend = append(toAppend, MemberEntry{
			Key:     key,
			Path:    wm.Path,
			Exclude: exclude,
		})
	}

	if len(toAppend) == 0 && existing != nil {
		// Nothing new to add; return current config unchanged.
		return existing, nil
	}

	// Merge new entries with existing.
	var allMembers []MemberEntry
	if existing != nil {
		allMembers = append(allMembers, existing.Members...)
	}
	allMembers = append(allMembers, toAppend...)

	cfg := &Config{Members: allMembers}

	// Write code.toml.
	cfgPath := configPath(realmRoot)
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return nil, fmt.Errorf("realm seed: mkdir %s: %w", filepath.Dir(cfgPath), err)
	}

	raw, err := toml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("realm seed: marshal config: %w", err)
	}

	if err := os.WriteFile(cfgPath, raw, 0o644); err != nil {
		return nil, fmt.Errorf("realm seed: write %s: %w", cfgPath, err)
	}

	return cfg, nil
}

// slugKey returns key when it is unused; otherwise appends -2, -3, … until unique.
func slugKey(base string, used map[string]bool) string {
	// Normalise: lowercase, replace non-alphanumeric with '-'.
	slug := slugify(base)
	if !used[slug] {
		return slug
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", slug, i)
		if !used[candidate] {
			return candidate
		}
	}
}

// slugify converts a string to a URL-safe key: lowercase alphanumeric with
// hyphens.  Multiple consecutive hyphens are collapsed to one.
func slugify(s string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen {
			// Emit at most one hyphen per run of non-alphanumeric runes.
			b.WriteRune('-')
			prevHyphen = true
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "repo"
	}
	return result
}

// isTrashPath reports whether path starts with "trash/" (realm-relative).
func isTrashPath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.HasPrefix(clean, "trash/")
}

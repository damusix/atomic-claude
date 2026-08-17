package realm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/wiki"
	"github.com/pelletier/go-toml/v2"
)

// SeedConfig derives code.toml from the <wiki-scan> block, marking pending and
// trash/ members excluded. It appends rather than overwrites, so manual edits
// to existing entries survive. Returns nil when there is no block to seed from.
func SeedConfig(realmRoot, wikiIndexPath string) (*Config, error) {
	members, err := wiki.ReadScanMembers(wikiIndexPath)
	if err != nil {
		return nil, fmt.Errorf("realm seed: read scan members: %w", err)
	}
	if len(members) == 0 {
		return nil, nil
	}

	existing, err := LoadConfig(realmRoot)
	if err != nil {
		return nil, fmt.Errorf("realm seed: load existing config: %w", err)
	}

	presentPaths := make(map[string]bool)
	if existing != nil {
		for _, m := range existing.Members {
			presentPaths[m.Path] = true
		}
	}

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
		return existing, nil
	}

	var allMembers []MemberEntry
	if existing != nil {
		allMembers = append(allMembers, existing.Members...)
	}
	allMembers = append(allMembers, toAppend...)

	cfg := &Config{Members: allMembers}

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

// slugKey appends -2, -3, … until the key is unique.
func slugKey(base string, used map[string]bool) string {
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

// slugify lowercases to alphanumerics and hyphens, collapsing hyphen runs.
// The result becomes a db filename stem, so it must be filesystem-safe.
func slugify(s string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen {
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

func isTrashPath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.HasPrefix(clean, "trash/")
}

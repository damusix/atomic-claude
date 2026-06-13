// Package realm provides position-sensing scope resolution and config loading
// for the code-intel realm federation feature.
//
// Two scopes exist (Repo and Realm); each locates its db automatically from the
// process cwd — no user flag required. The resolver and config loader are pure
// library code: no os.Exit, no hardcoded $HOME, injectable cwd + claudeMD path
// for tests.
package realm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// MemberEntry is one [[member]] entry in code.toml.
type MemberEntry struct {
	// Key is the short name used as the db filename stem: <realm>/.atomic/<key>.db.
	Key string `toml:"key"`

	// Path is the member's path relative to the realm root (realm-relative for portability).
	Path string `toml:"path"`

	// Exclude marks the member as intentionally skipped during fan-out index runs.
	Exclude bool `toml:"exclude"`
}

// Config is the parsed contents of <realmRoot>/.atomic/code.toml.
type Config struct {
	Members []MemberEntry `toml:"member"`
}

// configPath returns the canonical path to code.toml for a given realm root.
func configPath(realmRoot string) string {
	return filepath.Join(realmRoot, ".atomic", "code.toml")
}

// LoadConfig reads <realmRoot>/.atomic/code.toml and returns the parsed Config.
//
// Absent file: returns (nil, nil) — not an error. The caller (CP3) decides
// whether to seed or error on this signal.
//
// Parse error: returns (nil, non-nil error).
func LoadConfig(realmRoot string) (*Config, error) {
	path := configPath(realmRoot)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("realm config: read %s: %w", path, err)
	}

	var cfg Config
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("realm config: parse %s: %w", path, err)
	}
	return &cfg, nil
}

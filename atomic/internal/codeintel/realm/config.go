// Package realm resolves repo-versus-realm scope from the process cwd, so no
// user flag is needed to locate the right db. Pure library code: no os.Exit, no
// hardcoded $HOME, cwd and claudeMD path both injectable.
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
	// Key doubles as the db filename stem: <realm>/.atomic/<key>.db.
	Key string `toml:"key"`

	// Path is relative to the realm root, so a moved realm still resolves.
	Path string `toml:"path"`

	// Exclude skips the member during fan-out.
	Exclude bool `toml:"exclude"`
}

type Config struct {
	Members []MemberEntry `toml:"member"`
}

func configPath(realmRoot string) string {
	return filepath.Join(realmRoot, ".atomic", "code.toml")
}

// LoadConfig returns (nil, nil) for an absent file: the caller decides whether
// that means seed or error. A parse failure is a real error.
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

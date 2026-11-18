// Package realm resolves repo-versus-realm scope from the process cwd, so no
// user flag is needed to locate the right db. Pure library code: no os.Exit, no
// hardcoded $HOME, cwd and claudeMD path both injectable.
package realm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/pelletier/go-toml/v2"
)

// MemberEntry is one [[member]] entry in code.toml.
type MemberEntry struct {
	// Key doubles as the db filename stem: <realm-state-root>/<key>.db.
	Key string `toml:"key"`

	// Path is relative to the realm root, so a moved realm still resolves.
	Path string `toml:"path"`

	// Exclude skips the member during fan-out.
	Exclude bool `toml:"exclude"`
}

type Config struct {
	Members []MemberEntry `toml:"member"`
}

// stateRoot is where federation files are read first.
func stateRoot(realmRoot string) (string, error) {
	return config.RepositoryStateDir(realmRoot)
}

// legacyStateDir held federation files before they moved under the resolver.
func legacyStateDir(realmRoot string) string {
	return filepath.Join(realmRoot, ".atomic")
}

// writeRoot is the federation write directory; any realm resolved to .claude
// writes to .atomic instead, where state migrate leaves federation files.
func writeRoot(realmRoot string) (string, error) {
	state, err := stateRoot(realmRoot)
	if err != nil {
		return "", err
	}
	if state == filepath.Join(realmRoot, ".claude") {
		return legacyStateDir(realmRoot), nil
	}
	return state, nil
}

// ConfigPath is where code.toml is written.
func ConfigPath(realmRoot string) (string, error) {
	root, err := writeRoot(realmRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "code.toml"), nil
}

// WriteDBPath is where a member's database is written.
func WriteDBPath(realmRoot, key string) (string, error) {
	root, err := writeRoot(realmRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, key+".db"), nil
}

// readPath is name under the state root, or under the legacy directory when
// only that holds it.
func readPath(realmRoot, state, name string) string {
	path := filepath.Join(state, name)
	if fileExists(path) {
		return path
	}
	if legacy := filepath.Join(legacyStateDir(realmRoot), name); fileExists(legacy) {
		return legacy
	}
	return path
}

// EnsureDBIgnore makes the write root ignore its rebuildable databases, unless
// that root lies outside the realm.
func EnsureDBIgnore(realmRoot string) error {
	root, err := writeRoot(realmRoot)
	if err != nil {
		return err
	}
	if _, inside := config.RepoRelative(realmRoot, root); !inside {
		return nil
	}
	return config.AppendIgnoreRule(filepath.Join(root, ".gitignore"), "/*.db")
}

// LoadConfig returns (nil, nil) for an absent file: the caller decides whether
// that means seed or error. A parse failure is a real error.
func LoadConfig(realmRoot string) (*Config, error) {
	state, err := stateRoot(realmRoot)
	if err != nil {
		return nil, fmt.Errorf("realm config: resolve state root: %w", err)
	}
	path := readPath(realmRoot, state, "code.toml")
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

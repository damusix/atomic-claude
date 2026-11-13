// Package claude adapts the Claude Code harness to the common adapter contract.
package claude

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// ConfigDirEnv relocates the Claude configuration root for settings,
// instructions, rules, and transcripts alike.
const ConfigDirEnv = config.ClaudeConfigDirEnv

// defaultRootDir is the observed native root for a Claude home.
const defaultRootDir = ".claude"

// Adapter is the minimal Claude Code adapter.
type Adapter struct {
	// ConfigDir resolves the Claude configuration root for a home; tests stub it
	// so discovery never depends on the process environment.
	ConfigDir func(home string) string
}

// New returns an adapter with production root resolution.
func New() *Adapter { return &Adapter{ConfigDir: DefaultConfigDir} }

// DefaultConfigDir returns the configuration root for home: the
// CLAUDE_CONFIG_DIR override when the variable is set, else <home>/.claude.
func DefaultConfigDir(home string) string { return config.ClaudeConfigDir(home) }

// Kind reports the harness this adapter serves.
func (a *Adapter) Kind() harness.Kind { return harness.KindClaude }

// Capabilities returns the capability record for Claude Code 2.1.273.
func (a *Adapter) Capabilities() harness.CapabilityMatrix { return harness.ClaudeCapabilities() }

// Lifecycle is implemented in lifecycle.go, where the projection, migration, and
// removal seams are bound for one home.

// Discover reports the configured root plus a distinct existing <home>/.claude.
// Choosing between two roots is the caller's explicit decision.
func (a *Adapter) Discover(home string) ([]harness.Instance, error) {
	if strings.TrimSpace(home) == "" {
		return nil, errors.New("claude: discovery requires a home directory")
	}
	resolve := a.ConfigDir
	if resolve == nil {
		resolve = DefaultConfigDir
	}
	configured := strings.TrimSpace(resolve(home))
	if configured == "" {
		return nil, errors.New("claude: empty configuration root")
	}
	root := filepath.Clean(configured)

	primary, err := instance(root, home)
	if err != nil {
		return nil, err
	}
	instances := []harness.Instance{primary}

	if def := filepath.Join(home, defaultRootDir); def != root {
		exists, err := isDir(def)
		if err != nil {
			return nil, err
		}
		if exists {
			instances = append(instances, harness.Instance{
				Kind:       harness.KindClaude,
				ID:         def,
				NativeRoot: def,
				Home:       home,
				Exists:     true,
			})
		}
	}
	return instances, nil
}

// instance observes one candidate root. A configured root is reported even when
// it does not exist yet, because the user asked for it.
func instance(root, home string) (harness.Instance, error) {
	exists, err := isDir(root)
	if err != nil {
		return harness.Instance{}, err
	}
	return harness.Instance{
		Kind:       harness.KindClaude,
		ID:         root,
		NativeRoot: root,
		Home:       home,
		Exists:     exists,
	}, nil
}

// isDir reports whether path is an existing directory. A path that is missing,
// or is something other than a directory, is not a configuration root.
func isDir(path string) (bool, error) {
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claude: stat %s: %w", path, err)
	}
	return fi.IsDir(), nil
}

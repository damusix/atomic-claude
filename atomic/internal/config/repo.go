package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/pelletier/go-toml/v2"
)

// RepoConfigRelPath is the project-relative path to the repo-scoped config
// file. Unlike TOMLPath (~/.claude/.atomic/config.toml, per-user), this file
// is committed to the repo.
const RepoConfigRelPath = ".claude/atomic.toml"

// RepoConfigPath returns the path to the repo-scoped config file for projectRoot.
func RepoConfigPath(projectRoot string) string {
	return filepath.Join(projectRoot, RepoConfigRelPath)
}

// codeSection is the [code] TOML table in the repo config.
type codeSection struct {
	Ignore []string `toml:"ignore"`
}

// RepoConfig is the parsed repo-scoped configuration read from
// <projectRoot>/.claude/atomic.toml — a small, separate schema from the
// user-scoped Config above.
type RepoConfig struct {
	Code codeSection `toml:"code"`
}

// repoKnownSections is the set of known top-level TOML table names in the
// repo config schema.
var repoKnownSections = map[string]bool{
	"code": true,
}

// repoKnownLeaves is the set of known dotted leaf keys in the repo config schema.
var repoKnownLeaves = map[string]bool{
	"code.ignore": true,
}

// LoadRepoConfig reads path into a RepoConfig leniently, mirroring Load's
// contract: a missing file returns an empty RepoConfig with no warnings and
// no error (indexing is unaffected); unknown keys produce Warnings but no
// error; malformed TOML (including a wrong-typed ignore value) returns an
// error the caller can degrade on — indexing proceeds unfiltered with one
// reported warning, never a panic.
func LoadRepoConfig(path string) (*RepoConfig, []Warning, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &RepoConfig{}, nil, nil
		}
		return nil, nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	var rawMap map[string]any
	if err := toml.Unmarshal(raw, &rawMap); err != nil {
		return nil, nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	warns := checkUnknownRepoKeys(rawMap, "")

	cfg := &RepoConfig{}
	if err := toml.Unmarshal(raw, cfg); err != nil {
		return nil, warns, fmt.Errorf("config: decode %s: %w", path, err)
	}

	return cfg, warns, nil
}

// checkUnknownRepoKeys walks a raw decoded TOML map for the repo config and
// returns a Warning for each key outside the repo schema. Mirrors
// checkUnknownKeys, scoped to RepoConfig's much smaller schema.
func checkUnknownRepoKeys(m map[string]any, prefix string) []Warning {
	var warns []Warning
	for k, v := range m {
		dotted := k
		if prefix != "" {
			dotted = prefix + "." + k
		}

		if prefix == "" {
			if !repoKnownSections[k] {
				warns = append(warns, Warning{
					Message: fmt.Sprintf("config: unknown key %q (ignored)", dotted),
				})
				continue
			}
		} else if !repoKnownLeaves[dotted] {
			warns = append(warns, Warning{
				Message: fmt.Sprintf("config: unknown key %q (ignored)", dotted),
			})
			continue
		}

		if sub, ok := v.(map[string]any); ok {
			warns = append(warns, checkUnknownRepoKeys(sub, dotted)...)
		}
	}
	return warns
}

// IgnoreMatcher matches repo-relative, slash-separated paths against a set
// of ignore glob patterns. See docs/spec/graphignore.md SC3 for the exact
// semantics this implements.
type IgnoreMatcher struct {
	fullPath  []string // patterns containing "/" — doublestar full-path match
	basenames []string // patterns without "/" — basename match at any depth
}

// NewIgnoreMatcher builds an IgnoreMatcher from raw glob patterns.
//
//   - A pattern containing "/" is matched against the full repo-relative path.
//   - A pattern without "/" is matched against the path's basename at any depth.
//   - A leading "./" is stripped from each pattern before matching.
//   - A trailing-slash-only pattern (e.g. "vendor/") matches nothing —
//     directories must be excluded with "dir/**".
//   - An invalid pattern is dropped and reported as a Warning; it never
//     panics and never causes the matcher to match everything.
func NewIgnoreMatcher(patterns []string) (*IgnoreMatcher, []Warning) {
	m := &IgnoreMatcher{}
	var warns []Warning
	for _, p := range patterns {
		p = strings.TrimPrefix(p, "./")
		if p == "" || strings.HasSuffix(p, "/") {
			continue
		}
		if !doublestar.ValidatePattern(p) {
			warns = append(warns, Warning{
				Message: fmt.Sprintf("config: invalid ignore pattern %q (ignored)", p),
			})
			continue
		}
		if strings.Contains(p, "/") {
			m.fullPath = append(m.fullPath, p)
		} else {
			m.basenames = append(m.basenames, p)
		}
	}
	return m, warns
}

// Match reports whether relPath (repo-relative, slash-separated) is
// excluded by any ignore pattern. A nil matcher matches nothing.
func (m *IgnoreMatcher) Match(relPath string) bool {
	if m == nil {
		return false
	}
	base := path.Base(relPath)
	for _, p := range m.basenames {
		if matched, _ := doublestar.Match(p, base); matched {
			return true
		}
	}
	for _, p := range m.fullPath {
		if matched, _ := doublestar.Match(p, relPath); matched {
			return true
		}
	}
	return false
}

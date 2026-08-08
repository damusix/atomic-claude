package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/pelletier/go-toml/v2"
)

// codeSection is the [code] TOML table in the repo config.
type codeSection struct {
	Ignore []string `toml:"ignore"`
}

// replSection is the [repl] TOML table — one leaf, idle_timeout — shared by
// RepoConfig (repo-scoped, this file) and Config (user-scoped, config.go).
// Both harnesses decode the same shape; only resolution precedence differs
// (see internal/repl/action.go's resolveIdleTimeout: repo wins over user).
type replSection struct {
	IdleTimeout string `toml:"idle_timeout"`
}

// ValidateIdleTimeout parses and validates a [repl] idle_timeout value: it
// must parse as a Go duration string (time.ParseDuration) and be strictly
// positive — zero or negative means invalid, never "disable" (see
// docs/spec/atomic-repl.md). Returns the parsed duration on success so
// callers that need the value (resolveIdleTimeout) don't reparse it.
func ValidateIdleTimeout(value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("config: repl.idle_timeout %q is not a valid duration (e.g. \"1h\", \"30m\", \"90s\"): %w", value, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("config: repl.idle_timeout %q must be a positive duration, got %s", value, d)
	}
	return d, nil
}

// RepoConfig is the parsed repo-scoped configuration read from
// RepoConfigPath(projectRoot) (see harness.go) — a small, separate schema
// from the user-scoped Config above.
type RepoConfig struct {
	Code  codeSection `toml:"code"`
	Scope string      `toml:"scope"`
	Repl  replSection `toml:"repl"`
}

// repoKnownSections is the set of known top-level TOML table names in the
// repo config schema.
var repoKnownSections = map[string]bool{
	"code": true,
	"pi":   true,
	"repl": true,
}

// repoKnownTopLevelLeaves is the set of known top-level scalar keys in the
// repo config schema — keys that name a value directly, not a table. Kept
// separate from repoKnownSections (table names) and repoKnownLeaves (dotted
// keys nested inside a known table).
var repoKnownTopLevelLeaves = map[string]bool{
	"scope": true,
}

// repoKnownLeaves is the set of known dotted leaf keys in the repo config schema.
var repoKnownLeaves = map[string]bool{
	"code.ignore":       true,
	"repl.idle_timeout": true,
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
			if repoKnownTopLevelLeaves[k] {
				continue
			}
			if !repoKnownSections[k] {
				warns = append(warns, Warning{
					Message: fmt.Sprintf("config: unknown key %q (ignored)", dotted),
				})
				continue
			}
			if k == "pi" {
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
//
// Uses MatchUnvalidated rather than Match: every pattern stored in fullPath
// and basenames already passed doublestar.ValidatePattern in
// NewIgnoreMatcher, so re-validating on every Match call is redundant work
// whose error can never actually fire here. MatchUnvalidated skips that
// redundant validation and has no error to discard.
func (m *IgnoreMatcher) Match(relPath string) bool {
	if m == nil {
		return false
	}
	base := path.Base(relPath)
	for _, p := range m.basenames {
		if doublestar.MatchUnvalidated(p, base) {
			return true
		}
	}
	for _, p := range m.fullPath {
		if doublestar.MatchUnvalidated(p, relPath) {
			return true
		}
	}
	return false
}

// PatternCount returns the number of active (successfully compiled) glob
// patterns m enforces — used by `atomic code status` to report how many
// patterns are in effect. A nil matcher has 0 active patterns.
func (m *IgnoreMatcher) PatternCount() int {
	if m == nil {
		return 0
	}
	return len(m.fullPath) + len(m.basenames)
}

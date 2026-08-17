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

// serveSection is the [serve] TOML table in the repo config.
//
// Schema is a three-state override for `atomic serve`'s SQL schema view. Unset
// means auto-detect: the view exists when the index actually holds SQL objects,
// so a repo with no SQL never sees a mode it can do nothing with. A pointer
// because "the author said false" and "the author said nothing" must differ.
type serveSection struct {
	Schema *bool `toml:"schema"`
}

// replSection is the [repl] table — one leaf, idle_timeout — shared by RepoConfig
// and the user-scoped Config. Same shape both places; only resolution precedence
// differs (repo wins over user, see repl's resolveIdleTimeout).
type replSection struct {
	IdleTimeout string `toml:"idle_timeout"`
}

// ValidateIdleTimeout requires a Go duration string that is strictly positive —
// zero or negative is invalid, never "disable". Returns the parsed duration so
// callers that need the value do not reparse it.
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

// RepoConfig is the repo-scoped configuration read from RepoConfigPath — a small
// schema, separate from the user-scoped Config.
type RepoConfig struct {
	Code  codeSection  `toml:"code"`
	Scope string       `toml:"scope"`
	Repl  replSection  `toml:"repl"`
	Serve serveSection `toml:"serve"`
}

// repoKnownSections is the set of known top-level TOML table names in the
// repo config schema.
var repoKnownSections = map[string]bool{
	"code":  true,
	"pi":    true,
	"repl":  true,
	"serve": true,
}

// repoKnownTopLevelLeaves is the top-level scalar keys — ones naming a value
// directly rather than a table. Kept separate from repoKnownSections (table
// names) and repoKnownLeaves (dotted keys nested inside a known table).
var repoKnownTopLevelLeaves = map[string]bool{
	"scope": true,
}

// repoKnownLeaves is the set of known dotted leaf keys in the repo config schema.
var repoKnownLeaves = map[string]bool{
	"code.ignore":       true,
	"repl.idle_timeout": true,
	"serve.schema":      true,
}

// LoadRepoConfig mirrors Load's contract: a missing file is an empty RepoConfig
// with no warnings and no error, unknown keys warn, and malformed TOML returns
// an error the caller degrades on — indexing then proceeds unfiltered with one
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

// checkUnknownRepoKeys mirrors checkUnknownKeys, scoped to RepoConfig's much
// smaller schema.
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

// IgnoreMatcher matches repo-relative, slash-separated paths against a set of
// ignore globs. Exact semantics: docs/spec/graphignore.md.
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

// Match reports whether relPath is excluded by any ignore pattern. A nil matcher
// matches nothing. MatchUnvalidated rather than Match: every stored pattern
// already passed ValidatePattern in NewIgnoreMatcher, so the error can never
// fire and re-validating on every call is redundant work.
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

// PatternCount returns how many globs m enforces, for `atomic code status`. A nil
// matcher has 0.
func (m *IgnoreMatcher) PatternCount() int {
	if m == nil {
		return 0
	}
	return len(m.fullPath) + len(m.basenames)
}

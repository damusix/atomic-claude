package resolution

// path_aliases.go — tsconfig/jsconfig JSONC alias loader (CP11).
//
// Loads compilerOptions.baseUrl + compilerOptions.paths from the first
// tsconfig.json or jsconfig.json found in the project root. The file is
// parsed with github.com/tailscale/hujson so it tolerates JSONC syntax
// (comments, trailing commas).
//
// The loaded AliasMap is cached per projectRoot so repeated calls within a
// resolver session don't re-read the file. The cache is module-level (a
// sync.Map) and keyed by projectRoot.
//
// # Alias resolution algorithm
//
// For an import specifier like "@app/util":
//  1. Iterate the paths map. For each pattern key:
//     - If the pattern ends with "/*" it is a wildcard pattern.
//       Match by checking if the specifier starts with the pattern prefix
//       (e.g. "@app/"). If matched, substitute the "*" capture into each
//       target template and pick the first target.
//     - Otherwise, check for an exact match.
//  2. The target is a path relative to baseUrl (or project root when
//     baseUrl is absent). Strip a trailing ".ts"/".d.ts"/".tsx" suffix
//     before returning so callers can append their own extension candidates.
//  3. If no pattern matches, return "".

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tailscale/hujson"
)

// aliasCache stores *AliasMap keyed by projectRoot (absolute path string).
var aliasCache sync.Map // map[string]*AliasMap

// AliasMap holds the parsed compilerOptions.baseUrl and the alias patterns
// derived from compilerOptions.paths. It is safe for concurrent reads after
// construction (immutable once built).
type AliasMap struct {
	baseURL  string
	patterns []aliasPattern
}

type aliasPattern struct {
	prefix   string // everything before "/*" (empty for exact match)
	wildcard bool   // true when the key ends with "/*"
	exact    string // non-empty for exact-match keys
	// targets is the list of template strings from the paths value array.
	// For wildcards, each template contains exactly one "*" placeholder.
	targets []string
}

// BaseURL returns the compilerOptions.baseUrl value (may be empty).
func (a *AliasMap) BaseURL() string {
	if a == nil {
		return ""
	}
	return a.baseURL
}

// Resolve attempts to map specifier to a file path using the loaded aliases.
// Returns "" if no alias matches.  The returned path is relative to baseUrl
// (or project root), with any trailing .ts/.d.ts/.tsx/.js/.jsx suffix
// stripped so callers can probe multiple extension candidates.
func (a *AliasMap) Resolve(specifier string) string {
	if a == nil || len(a.patterns) == 0 {
		return ""
	}
	for _, p := range a.patterns {
		if p.wildcard {
			// Wildcard match: specifier must start with prefix.
			if !strings.HasPrefix(specifier, p.prefix) {
				continue
			}
			if len(p.targets) == 0 {
				continue
			}
			capture := specifier[len(p.prefix):]
			tmpl := p.targets[0] // take first target
			result := strings.ReplaceAll(tmpl, "*", capture)
			return stripTSExtension(result)
		}
		// Exact match.
		if specifier == p.exact {
			if len(p.targets) == 0 {
				continue
			}
			return stripTSExtension(p.targets[0])
		}
	}
	return ""
}

// stripTSExtension removes common TS/JS file suffixes so resolution can try
// multiple extension candidates without the caller needing to special-case them.
func stripTSExtension(p string) string {
	for _, ext := range []string{".d.ts", ".tsx", ".ts", ".jsx", ".js"} {
		if strings.HasSuffix(p, ext) {
			return p[:len(p)-len(ext)]
		}
	}
	return p
}

// ---------------------------------------------------------------------------
// Public loader — cached
// ---------------------------------------------------------------------------

// LoadPathAliases reads tsconfig.json (preferred) or jsconfig.json from
// projectRoot, parses compilerOptions.baseUrl + paths via hujson, and returns
// an *AliasMap. The result is cached by projectRoot.
//
// Returns a non-nil (but empty) *AliasMap when no config file is found so
// callers can call Resolve without a nil check.
func LoadPathAliases(projectRoot string) (*AliasMap, error) {
	if v, ok := aliasCache.Load(projectRoot); ok {
		return v.(*AliasMap), nil
	}
	am, err := loadPathAliasesUncached(projectRoot)
	if err != nil {
		return nil, err
	}
	// Store in cache. If a concurrent goroutine stored first, use theirs.
	actual, _ := aliasCache.LoadOrStore(projectRoot, am)
	return actual.(*AliasMap), nil
}

func loadPathAliasesUncached(projectRoot string) (*AliasMap, error) {
	configPath := ""
	for _, name := range []string{"tsconfig.json", "jsconfig.json"} {
		p := filepath.Join(projectRoot, name)
		if _, err := os.Stat(p); err == nil {
			configPath = p
			break
		}
	}
	if configPath == "" {
		// No config found — return an empty alias map.
		return &AliasMap{}, nil
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	// hujson.Standardize converts JSONC (comments, trailing commas) to
	// standard JSON so encoding/json can unmarshal it.
	standardized, err := hujson.Standardize(raw)
	if err != nil {
		// If the file is malformed, return empty rather than failing.
		return &AliasMap{}, nil
	}

	// Parse just the compilerOptions we care about.
	var cfg struct {
		CompilerOptions struct {
			BaseURL string              `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(standardized, &cfg); err != nil {
		return &AliasMap{}, nil
	}

	am := &AliasMap{
		baseURL: cfg.CompilerOptions.BaseURL,
	}

	for key, targets := range cfg.CompilerOptions.Paths {
		// Resolve targets relative to baseUrl.
		resolvedTargets := make([]string, 0, len(targets))
		for _, t := range targets {
			// If baseUrl is set and the target is not absolute, prepend it.
			if am.baseURL != "" && !filepath.IsAbs(t) {
				t = filepath.Join(am.baseURL, t)
			}
			resolvedTargets = append(resolvedTargets, t)
		}

		if strings.HasSuffix(key, "/*") {
			prefix := key[:len(key)-1] // strip the trailing "*", keep "/"
			am.patterns = append(am.patterns, aliasPattern{
				prefix:   prefix,
				wildcard: true,
				targets:  resolvedTargets,
			})
		} else {
			am.patterns = append(am.patterns, aliasPattern{
				exact:   key,
				targets: resolvedTargets,
			})
		}
	}

	return am, nil
}

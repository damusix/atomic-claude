package resolution

// tsconfig/jsconfig path-alias loading, so an "@app/util" import resolves to
// a real file. Parsed with hujson because these files are JSONC in practice:
// comments and trailing commas are normal and must not fail the load.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tailscale/hujson"
)

// aliasCache maps projectRoot to *AliasMap.
var aliasCache sync.Map

// AliasMap is immutable once built, so concurrent reads need no lock.
type AliasMap struct {
	baseURL  string
	patterns []aliasPattern
}

type aliasPattern struct {
	// prefix is everything before "/*"; exact is set instead for a
	// non-wildcard key.
	prefix   string
	wildcard bool
	exact    string
	// targets are the paths-value templates, each holding one "*" when the
	// pattern is a wildcard.
	targets []string
}

// BaseURL may be empty.
func (a *AliasMap) BaseURL() string {
	if a == nil {
		return ""
	}
	return a.baseURL
}

// Resolve maps specifier to a path relative to baseUrl, or "" if no alias
// matches. The extension is stripped so the caller can probe its own
// candidates.
func (a *AliasMap) Resolve(specifier string) string {
	if a == nil || len(a.patterns) == 0 {
		return ""
	}
	for _, p := range a.patterns {
		if p.wildcard {
			if !strings.HasPrefix(specifier, p.prefix) {
				continue
			}
			if len(p.targets) == 0 {
				continue
			}
			capture := specifier[len(p.prefix):]
			// First target only; tsconfig allows a fallback list, which the
			// resolver's own extension probing already covers.
			tmpl := p.targets[0]
			result := strings.ReplaceAll(tmpl, "*", capture)
			return stripTSExtension(result)
		}
		if specifier == p.exact {
			if len(p.targets) == 0 {
				continue
			}
			return stripTSExtension(p.targets[0])
		}
	}
	return ""
}

func stripTSExtension(p string) string {
	for _, ext := range []string{".d.ts", ".tsx", ".ts", ".jsx", ".js"} {
		if strings.HasSuffix(p, ext) {
			return p[:len(p)-len(ext)]
		}
	}
	return p
}

// LoadPathAliases reads tsconfig.json, else jsconfig.json, from projectRoot,
// caching the result. Always returns a non-nil map — a missing or malformed
// config yields an empty one, so callers never nil-check before Resolve.
func LoadPathAliases(projectRoot string) (*AliasMap, error) {
	if v, ok := aliasCache.Load(projectRoot); ok {
		return v.(*AliasMap), nil
	}
	am, err := loadPathAliasesUncached(projectRoot)
	if err != nil {
		return nil, err
	}
	// A concurrent loader may have won; use whichever map is stored.
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
		return &AliasMap{}, nil
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	standardized, err := hujson.Standardize(raw)
	if err != nil {
		// A malformed tsconfig degrades resolution; it must not fail indexing.
		return &AliasMap{}, nil
	}

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
		// tsconfig targets are relative to baseUrl, so bake it in now.
		resolvedTargets := make([]string, 0, len(targets))
		for _, t := range targets {
			if am.baseURL != "" && !filepath.IsAbs(t) {
				t = filepath.Join(am.baseURL, t)
			}
			resolvedTargets = append(resolvedTargets, t)
		}

		if strings.HasSuffix(key, "/*") {
			// Drops the "*" but keeps the "/", so the prefix is a real
			// boundary and "@app/" cannot match "@application".
			prefix := key[:len(key)-1]
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

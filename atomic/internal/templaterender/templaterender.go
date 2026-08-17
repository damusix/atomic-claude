// Package templaterender expands the {{ template "<name>" . }} partials that
// artifact sources under context/ compose.
//
// Expansion happens on the way into the embedded bundle; there is no committed
// rendered copy, so an artifact exists exactly once in the repo. Both
// bundlemirror and validate expand through here, so what ships and what gets
// linted cannot diverge.
package templaterender

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// PartialsDir's leading underscore marks it as not-an-artifact: bundlemirror
// skips it, so nothing here installs to a user's ~/.claude.
const PartialsDir = "_partials"

// LoadPartials pools every *.md in dir, parsed in sorted order so a
// redefinition resolves deterministically. A missing dir yields an empty pool,
// which is what lets a context tree with no partials render unchanged.
func LoadPartials(dir string) (*template.Template, error) {
	base := template.New("base")

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return base, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read partials dir: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s/%s: %w", PartialsDir, name, err)
		}
		if _, err := base.Parse(string(data)); err != nil {
			return nil, fmt.Errorf("parse %s/%s: %w", PartialsDir, name, err)
		}
	}

	return base, nil
}

// Expand renders src against the partial pool; name is only for errors and
// template identity. A source with no directives comes back byte-identical, so
// callers need not special-case artifacts that compose nothing.
func Expand(partials *template.Template, name string, src []byte) ([]byte, error) {
	// Clone per file so definitions cannot leak between artifacts.
	t, err := partials.Clone()
	if err != nil {
		return nil, fmt.Errorf("clone partial pool: %w", err)
	}

	t, err = t.New(name).Parse(string(src))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}

	var sb strings.Builder
	if err := t.ExecuteTemplate(&sb, name, nil); err != nil {
		return nil, fmt.Errorf("expand %s: %w", name, err)
	}

	return []byte(sb.String()), nil
}

// Package templaterender expands the shared partials composed by artifact
// sources under context/.
//
// Artifacts are authored and committed in their source form: a command or agent
// file may carry {{ template "<name>" . }} directives that pull in a partial
// from context/_partials/. Expansion happens at build time, on the way into the
// embedded bundle — there is no committed rendered copy, so an artifact exists
// exactly once in the repo.
//
// Callers load the partial pool once with LoadPartials, then call Expand per
// file. Both bundlemirror (build) and validate (lint) go through here, so what
// ships and what gets checked are expanded the same way.
package templaterender

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// PartialsDir is the directory under the context root holding shared partials.
// The leading underscore marks it as not-an-artifact: bundlemirror skips it, so
// nothing in here installs to a user's ~/.claude.
const PartialsDir = "_partials"

// LoadPartials reads every *.md in dir and registers the named templates they
// define into one pool. Files are parsed in sorted order so a redefinition is
// resolved deterministically. Returns an empty pool if dir does not exist,
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

// Expand renders src against the partial pool and returns the result. name is
// used only for error messages and template identity. A source with no
// directives comes back byte-identical, so callers can run every artifact
// through Expand without special-casing the ones that compose nothing.
func Expand(partials *template.Template, name string, src []byte) ([]byte, error) {
	// Clone so each file parses into its own tree and definitions do not leak
	// between artifacts.
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

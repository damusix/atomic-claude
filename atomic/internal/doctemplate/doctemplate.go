// Package doctemplate serves the fill-in document skeletons behind
// `atomic template <name>`. They are embedded here, never shipped into the
// ~/.claude bundle, so Claude Code cannot surface them as invocable commands.
package doctemplate

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed templates/*.md
var templatesFS embed.FS

// Get returns the template for name — the filename without .md — erroring
// with the valid names listed.
func Get(name string) (string, error) {
	data, err := templatesFS.ReadFile("templates/" + name + ".md")
	if err != nil {
		return "", fmt.Errorf("atomic template: unknown template name %q; valid names: %s",
			name, strings.Join(Names(), ", "))
	}
	return string(data), nil
}

// Names returns the sorted list of registered template names.
func Names() []string {
	entries, err := fs.ReadDir(templatesFS, "templates")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(names)
	return names
}

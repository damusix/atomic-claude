// Package doctemplate embeds and exposes the fill-in skeletons for every
// document the workflow coordinates (design doc, spec, scratchpad brief/state/
// followups, session report, diagnose context, implementation log), served via
// `atomic template <name>`. Templates are NOT install artifacts — they are
// embedded directly in this package and never shipped into the ~/.claude
// bundle, so Claude Code cannot surface them as invocable commands.
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

// Get returns the embedded template text for the given name (the filename
// without the .md extension). Returns a non-nil error when name is not in the
// registered set; the error lists valid names.
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

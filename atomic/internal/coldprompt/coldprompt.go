// Package coldprompt embeds and exposes cold-op brief texts used by
// `atomic prompt <name>`. Briefs are self-contained instructions for a generic
// subagent executing a rare, disposable task (e.g. git cleanup, CLAUDE.md
// merge). They are NOT install artifacts — they are embedded directly in this
// package and never shipped into the ~/.claude bundle.
package coldprompt

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed briefs/git-cleanup.md
var gitCleanupBrief string

//go:embed briefs/claude-merge.md
var claudeMergeBrief string

// briefs maps registered cold-op names to their embedded brief text.
var briefs = map[string]string{
	"git-cleanup":  gitCleanupBrief,
	"claude-merge": claudeMergeBrief,
}

// Get returns the embedded brief text for the given name. Returns a non-nil
// error when name is not in the registered set; the error lists valid names.
func Get(name string) (string, error) {
	text, ok := briefs[name]
	if !ok {
		return "", fmt.Errorf("atomic prompt: unknown brief name %q; valid names: %s",
			name, strings.Join(Names(), ", "))
	}
	return text, nil
}

// Names returns the sorted list of registered cold-op brief names.
func Names() []string {
	names := make([]string, 0, len(briefs))
	for k := range briefs {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

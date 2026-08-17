// Package coldprompt serves the brief texts behind `atomic prompt <name>`:
// self-contained instructions for a subagent starting with no accumulated
// context. They are embedded here, never shipped into the ~/.claude bundle.
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

//go:embed briefs/implementer.md
var implementerBrief string

//go:embed briefs/reviewer.md
var reviewerBrief string

var briefs = map[string]string{
	"git-cleanup":  gitCleanupBrief,
	"claude-merge": claudeMergeBrief,
	"implementer":  implementerBrief,
	"reviewer":     reviewerBrief,
}

// Get returns the brief for name, erroring with the valid names listed.
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

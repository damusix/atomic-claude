package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
)

// mdLinkTargetRe extracts the target from a markdown link [text](target).
var mdLinkTargetRe = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)

// checkMemory implements category 7: memory orphan check.
//
// Resolves ~/.claude/projects/<project>/memory/MEMORY.md where <project>
// is derived from cwd. Validates all markdown link targets exist in same dir.
func checkMemory(_ Opts) Result {
	claudeHome, err := claudeinstall.ResolveTarget("~/.claude")
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("resolve ~/.claude: %v", err)}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not get cwd: %v", err)}
	}
	project := ProjectNameFromCWD(cwd)
	return RunCheckMemoryWith(claudeHome, project)
}

// ProjectNameFromCWD derives the Claude Code auto-memory project name from an
// absolute cwd path. Convention: full path with "/" replaced by "-"; leading
// "/" stripped first so the result begins with "-".
// Example: /Users/alonso/foo → -Users-alonso-foo
func ProjectNameFromCWD(cwd string) string {
	// Strip leading slash so replacement produces "-Users-..." not "--Users-...".
	trimmed := strings.TrimPrefix(cwd, "/")
	return "-" + strings.ReplaceAll(trimmed, "/", "-")
}

// RunCheckMemoryWith runs the memory orphan check against explicit claudeHome and project.
// Exported for testing; production callers use checkMemory.
func RunCheckMemoryWith(claudeHome, project string) Result {
	memoryPath := filepath.Join(claudeHome, "projects", project, "memory", "MEMORY.md")
	memoryDir := filepath.Dir(memoryPath)

	data, err := os.ReadFile(memoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Severity: PASS, Detail: "no auto-memory (clean)"}
		}
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not read MEMORY.md: %v", err)}
	}

	targets := mdLinkTargetRe.FindAllSubmatch(data, -1)

	var orphans []string
	for _, m := range targets {
		target := string(m[1])
		// Skip absolute paths and URLs.
		if strings.HasPrefix(target, "/") || strings.Contains(target, "://") {
			continue
		}
		fullPath := filepath.Join(memoryDir, target)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			orphans = append(orphans, target)
		}
	}

	total := len(targets)
	if len(orphans) == 0 {
		return Result{Severity: PASS, Detail: fmt.Sprintf("%d/%d refs resolve", total, total)}
	}

	listed := orphans
	suffix := ""
	if len(listed) > 3 {
		listed = listed[:3]
		suffix = " ..."
	}
	return Result{
		Severity: WARN,
		Detail:   fmt.Sprintf("%d orphan refs: %s%s", len(orphans), strings.Join(listed, ", "), suffix),
	}
}

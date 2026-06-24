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

// projectSlugRe matches every character Claude Code replaces with "-" when it
// derives a project session directory name from a cwd: any non-alphanumeric
// character (path separators "/" and "\", the Windows drive colon ":", dots,
// and so on). Replacement is per character, so "/." becomes "--". Existing
// hyphens map to themselves and letter case is preserved.
var projectSlugRe = regexp.MustCompile(`[^a-zA-Z0-9]`)

// ProjectNameFromCWD derives the Claude Code auto-memory project name from an
// absolute cwd path, mirroring Claude Code's own slugification: every
// non-alphanumeric character is replaced by "-".
//
// A POSIX path's leading "/" therefore yields a leading "-", and dotted
// segments slugify too:
//
//	/Users/alonso/foo  → -Users-alonso-foo
//	/Users/alonso/.cfg → -Users-alonso--cfg
//	C:\Users\me\repo    → C--Users-me-repo
func ProjectNameFromCWD(cwd string) string {
	return projectSlugRe.ReplaceAllString(cwd, "-")
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
	checked := 0
	for _, m := range targets {
		target := string(m[1])
		// Skip absolute paths and URLs.
		if strings.HasPrefix(target, "/") || strings.Contains(target, "://") {
			continue
		}
		checked++
		fullPath := filepath.Join(memoryDir, target)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			orphans = append(orphans, target)
		}
	}

	if len(orphans) == 0 {
		return Result{Severity: PASS, Detail: fmt.Sprintf("%d/%d refs resolve", checked, checked)}
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

package doctor

import (
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/hooks"
)

// checkHooks implements category 2: session-start hook installed. Any missing
// or legacy-wrapper registration WARNs.
//
// The scope root is $HOME, not ~/.claude: hooks.IsInstalled appends
// ".claude/settings.json" itself, so passing ~/.claude doubles the segment.
func checkHooks(_ Opts) Result {
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("resolve home: %v", err)}
	}
	return RunCheckHooksWith(home)
}

// RunCheckHooksWith runs the hooks check against an explicit scopeRoot.
// Exported for testing.
func RunCheckHooksWith(scopeRoot string) Result {
	installed, drifted, err := hooks.IsInstalled(scopeRoot)
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not read settings.json: %v", err)}
	}

	switch {
	case !installed:
		return Result{Severity: WARN, Detail: "session-start hook missing"}
	case drifted:
		return Result{Severity: WARN, Detail: "session-start hook uses legacy wrapper script — run `atomic hooks install` to migrate"}
	default:
		return Result{Severity: PASS, Detail: "session-start hook installed"}
	}
}

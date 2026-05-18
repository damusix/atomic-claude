package doctor

import (
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
)

// checkHooks implements category 2: session-start hook installed.
//
// Resolves ~/.claude/ as the scope root (where settings.json lives), then
// delegates to RunCheckHooksWith.
func checkHooks(_ Opts) Result {
	target, err := claudeinstall.ResolveTarget("~/.claude")
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("resolve target: %v", err)}
	}
	return RunCheckHooksWith(target)
}

// RunCheckHooksWith runs the hooks check against an explicit scopeRoot.
// Exported for testing; production callers use checkHooks.
func RunCheckHooksWith(scopeRoot string) Result {
	installed, drifted, err := hooks.IsInstalled(scopeRoot)
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not read settings.json: %v", err)}
	}

	switch {
	case !installed:
		return Result{Severity: WARN, Detail: "session-start hook missing"}
	case drifted:
		return Result{Severity: WARN, Detail: "session-start hook content drifted"}
	default:
		return Result{Severity: PASS, Detail: "session-start hook installed"}
	}
}

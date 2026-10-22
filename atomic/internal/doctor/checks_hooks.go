package doctor

import (
	"fmt"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/hooks"
)

// checkHooks implements category 2: session-start hook installed in every
// enrolled Claude target. Any missing or legacy-wrapper registration WARNs. A
// machine whose ledger enrols no Claude target is skipped rather than warned
// about an un-enrolled ~/.claude.
func checkHooks(opts Opts) Result {
	scope := claudeScopeFor(opts)
	if scope.Skip {
		return Result{Severity: SKIP, Detail: scope.Detail}
	}
	results := make([]Result, 0, len(scope.Roots))
	for _, root := range scope.Roots {
		results = append(results, RunCheckHooksInDir(root))
	}
	return combineClaudeRoots(scope, results)
}

// RunCheckHooksInDir runs the hooks check against an explicit Claude config
// directory's settings.json. Exported for testing.
func RunCheckHooksInDir(configDir string) Result {
	installed, drifted, err := hooks.IsInstalledInDir(configDir)
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not read %s: %v", filepath.Join(configDir, "settings.json"), err)}
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

// RunCheckHooksWith runs the hooks check against an explicit scope root, the
// $HOME the legacy user-scope registration resolves. Exported for testing.
func RunCheckHooksWith(scopeRoot string) Result {
	return RunCheckHooksInDir(filepath.Join(scopeRoot, ".claude"))
}

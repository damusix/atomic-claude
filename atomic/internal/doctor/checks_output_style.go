package doctor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
)

// checkOutputStyle implements category 14: whether the user-level outputStyle
// key is seeded, and whether a project settings file carries a value of its
// own. It reports what each file contains — never a computed effective
// style, since Claude Code's per-repo file-placement rules aren't documented
// well enough to replicate.
func checkOutputStyle(opts Opts) Result {
	target, err := claudeinstall.ResolveTarget("~/.claude")
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("resolve target: %v", err)}
	}
	home, err := resolveHome()
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("resolve home dir: %v", err)}
	}
	return RunCheckOutputStyleWith(target, home, opts.RepoRoot)
}

// RunCheckOutputStyleWith runs the output-style check against explicit roots.
// Exported for testing. targetDir is the install target (default ~/.claude);
// home is where the user-level settings.json lives — normally the same
// machine home, kept separate so tests can point each independently.
func RunCheckOutputStyleWith(targetDir, home, repoRoot string) Result {
	sfPath := filepath.Join(targetDir, "settings.json")
	value, present, err := hooks.ReadOutputStyle(sfPath)
	if err != nil {
		return Result{Severity: WARN, Detail: fmt.Sprintf("could not read %s: %v", sfPath, err)}
	}

	styleInstalled := hooks.StyleInstalled(targetDir)
	seedOn := hooks.SeedEnabled(home)

	var result Result
	switch {
	case !present && !styleInstalled:
		result = Result{
			Severity:    WARN,
			Detail:      fmt.Sprintf("output style not set at user level; %s is not installed", hooks.OutputStyleRelPath),
			Remediation: "atomic claude update",
		}
	case !present && !seedOn:
		result = Result{
			Severity:    WARN,
			Detail:      "output style not set at user level; seeding is disabled (output_style.seed = false)",
			Remediation: "atomic config set output_style.seed true",
		}
	case !present:
		result = Result{Severity: WARN, Detail: "output style not set at user level"}
	case value == hooks.OutputStyleName && !styleInstalled:
		result = Result{
			Severity:    WARN,
			Detail:      fmt.Sprintf("output style set to %q but %s is not installed", value, hooks.OutputStyleRelPath),
			Remediation: "atomic claude update",
		}
	case value != hooks.OutputStyleName:
		result = Result{Severity: PASS, Detail: fmt.Sprintf("output style set to %q (not verified against an installed style file)", value)}
	default:
		result = Result{Severity: PASS, Detail: fmt.Sprintf("output style set to %q", value)}
	}

	if override := projectOutputStyleOverrides(repoRoot, targetDir); override != "" {
		result.Detail += "; " + override
	}
	return result
}

// projectOutputStyleOverrides reports any outputStyle key found in the repo's
// own settings files, naming the file. Empty repoRoot skips the check, as
// does a repoRoot whose .claude dir is targetDir itself — running doctor
// from $HOME must not flag the user-level file as an override of itself.
func projectOutputStyleOverrides(repoRoot, targetDir string) string {
	if repoRoot == "" {
		return ""
	}
	if hooks.SameDir(filepath.Join(repoRoot, ".claude"), targetDir) {
		return ""
	}

	var found []string
	for _, name := range []string{"settings.json", "settings.local.json"} {
		p := filepath.Join(repoRoot, ".claude", name)
		v, present, err := hooks.ReadOutputStyle(p)
		if err != nil || !present {
			continue
		}
		found = append(found, fmt.Sprintf("%s sets outputStyle=%q", name, v))
	}
	if len(found) == 0 {
		return ""
	}
	return strings.Join(found, "; ") + " (may override the user-level value)"
}
